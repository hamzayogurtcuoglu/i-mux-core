package difftest

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// Entry is one compared result in a transcript.
type Entry struct {
	Step   Step
	Args   []string // the command words run (for a wait, the last poll's)
	Result Result
}

// Transcript is what a script gave on one engine.
type Transcript struct {
	Engine  string
	Entries []Entry
}

// SkipError says the script skips the engine.
type SkipError struct{ Reason string }

func (e *SkipError) Error() string { return "skipped: " + e.Reason }

// WaitTimeout is how long a wait step polls before it gives up.
var WaitTimeout = 10 * time.Second

// ToolsDir, when set, goes first in the engines' PATH: the scripts' programs (faketui) are
// there.
var ToolsDir = ""

// FixedEnv is the environment every engine runs in, besides HOME and TMUX_TMPDIR.
var FixedEnv = []string{
	"TERM=xterm-256color",
	"LANG=C.UTF-8",
	"TZ=UTC",
	"PATH=/usr/bin:/bin",
	"SHELL=/bin/sh",
}

// NewEnv makes a run's directory, home and socket. The directory is short (under /tmp on Unix),
// as socket paths are limited to about 100 bytes.
func NewEnv() (*Env, error) {
	base := ""
	if runtime.GOOS != "windows" {
		base = "/tmp"
	}
	dir, err := os.MkdirTemp(base, "imux-dt-")
	if err != nil {
		return nil, err
	}
	if d, err := filepath.EvalSymlinks(dir); err == nil {
		dir = d
	}
	home := filepath.Join(dir, "home")
	tmp := filepath.Join(dir, "tmp")
	for _, d := range []string{home, tmp} {
		if err := os.Mkdir(d, 0o700); err != nil {
			return nil, err
		}
	}
	vars := append([]string{"HOME=" + home, "TMUX_TMPDIR=" + tmp}, FixedEnv...)
	if ToolsDir != "" {
		for i, kv := range vars {
			if p, ok := strings.CutPrefix(kv, "PATH="); ok {
				vars[i] = "PATH=" + ToolsDir + string(os.PathListSeparator) + p
			}
		}
	}
	return &Env{Dir: dir, Socket: filepath.Join(dir, "s"), Vars: vars}, nil
}

type normalizer struct {
	re      *regexp.Regexp
	repl    string
	literal bool // repl has no $1 references (the run directory's own rule)
}

func (n normalizer) apply(s string) string {
	if n.literal {
		return n.re.ReplaceAllLiteralString(s, n.repl)
	}
	return n.re.ReplaceAllString(s, n.repl)
}

// Execute runs script on eng and returns its transcript. Each run gets a new Env, removed
// afterwards. A *SkipError says the script skips this engine.
func Execute(ctx context.Context, script *Script, eng Engine) (*Transcript, error) {
	for _, st := range script.Steps {
		if st.Kind == "skip-if-engine" && st.Args[0] == eng.Name() {
			return nil, &SkipError{Reason: strings.Join(st.Args[1:], " ")}
		}
	}
	env, err := NewEnv()
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(env.Dir)
	defer eng.Stop(env)

	tr := &Transcript{Engine: eng.Name()}
	norms := []normalizer{{regexp.MustCompile(regexp.QuoteMeta(env.Dir)), "$DIR", true}}
	normalize := func(r Result) Result {
		for _, n := range norms {
			r.Stdout = n.apply(r.Stdout)
			r.Stderr = n.apply(r.Stderr)
		}
		return r
	}
	width, height := 80, 24
	clients := map[string]Client{}
	defer func() {
		for _, c := range clients {
			c.Close()
		}
	}()
	for _, st := range script.Steps {
		switch st.Kind {
		case "conf":
			src := st.Args[0]
			if !filepath.IsAbs(src) {
				src = filepath.Join(filepath.Dir(script.Path), src)
			}
			b, err := os.ReadFile(src)
			if err != nil {
				return nil, fmt.Errorf("%s:%d: %w", script.Path, st.Line, err)
			}
			env.Config = filepath.Join(env.Dir, "test.conf")
			if err := os.WriteFile(env.Config, b, 0o600); err != nil {
				return nil, err
			}
		case "file":
			src := st.Args[1]
			if !filepath.IsAbs(src) {
				src = filepath.Join(filepath.Dir(script.Path), src)
			}
			b, err := os.ReadFile(src)
			if err != nil {
				return nil, fmt.Errorf("%s:%d: %w", script.Path, st.Line, err)
			}
			dst := filepath.Join(env.Dir, st.Args[0])
			if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
				return nil, err
			}
			if err := os.WriteFile(dst, b, 0o600); err != nil {
				return nil, err
			}
		case "size":
			width, height = parseSize(st.Args[0])
		case "normalize":
			norms = append(norms, normalizer{re: regexp.MustCompile(st.Args[0]), repl: st.Args[1]})
		case "run":
			args := withSize(st.Args, width, height)
			tr.Entries = append(tr.Entries, Entry{Step: st, Args: args, Result: normalize(eng.Run(ctx, env, args))})
		case "capture":
			args := []string{"capture-pane", "-p", "-e", "-t", st.Args[0]}
			tr.Entries = append(tr.Entries, Entry{Step: st, Args: args, Result: normalize(eng.Run(ctx, env, args))})
		case "client":
			at, ok := eng.(Attacher)
			if !ok {
				return nil, fmt.Errorf("%s:%d: engine %s cannot attach a client", script.Path, st.Line, eng.Name())
			}
			cols, rows := parseSize(st.Args[1])
			xp, yp := 0, 0
			if len(st.Args) == 3 {
				xp, yp = parseSize(st.Args[2])
			}
			c, err := at.Attach(ctx, env, st.Args[0], cols, rows, xp, yp)
			if err != nil {
				return nil, fmt.Errorf("%s:%d: %w", script.Path, st.Line, err)
			}
			clients[st.Args[0]] = c
		case "type":
			b, _ := typedText(st.Args[1])
			if err := clients[st.Args[0]].Type(b); err != nil {
				return nil, fmt.Errorf("%s:%d: %w", script.Path, st.Line, err)
			}
		case "resize":
			cols, rows := parseSize(st.Args[1])
			if err := clients[st.Args[0]].Resize(cols, rows); err != nil {
				return nil, fmt.Errorf("%s:%d: %w", script.Path, st.Line, err)
			}
		case "screen":
			if c, ok := clients[st.Args[0]]; ok {
				r := normalize(c.Screen(ctx))
				tr.Entries = append(tr.Entries, Entry{Step: st, Args: []string{"screen", st.Args[0]}, Result: r})
				break
			}
			cols, rows := parseSize(st.Args[0])
			sc, ok := eng.(Screener)
			if !ok {
				return nil, fmt.Errorf("%s:%d: engine %s cannot attach a client", script.Path, st.Line, eng.Name())
			}
			r := normalize(sc.Screen(ctx, env, cols, rows))
			tr.Entries = append(tr.Entries, Entry{Step: st, Args: screenArgs(cols, rows), Result: r})
		case "wait":
			target, format, value, _ := parseWait(st.Args)
			args := []string{"display-message", "-p"}
			if target != "" {
				args = append(args, "-t", target)
			}
			args = append(args, format)
			r := wait(ctx, func() Result { return normalize(eng.Run(ctx, env, args)) }, value)
			tr.Entries = append(tr.Entries, Entry{Step: st, Args: args, Result: r})
		case "waittext":
			args := []string{"capture-pane", "-p", "-t", st.Args[0]}
			r := waitText(ctx, func() Result { return normalize(eng.Run(ctx, env, args)) }, st.Args[1])
			tr.Entries = append(tr.Entries, Entry{Step: st, Args: args, Result: r})
		}
	}
	return tr, nil
}

// waitText polls until get's stdout holds text; the result says only whether it did.
func waitText(ctx context.Context, get func() Result, text string) Result {
	deadline := time.Now().Add(WaitTimeout)
	for {
		r := get()
		if strings.Contains(r.Stdout, text) && r.Code == 0 {
			return Result{Stdout: text}
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			return Result{Stdout: r.Stdout, Stderr: fmt.Sprintf("difftest: timed out waiting for %q", text)}
		}
		t := time.NewTimer(5 * time.Millisecond)
		select {
		case <-ctx.Done():
			t.Stop()
		case <-t.C:
		}
	}
}

// wait polls until get's stdout is value, and returns the last result. On a timeout the result
// says so in its stderr, so a comparison with an engine that got there shows it.
func wait(ctx context.Context, get func() Result, value string) Result {
	deadline := time.Now().Add(WaitTimeout)
	delay := 5 * time.Millisecond
	for {
		r := get()
		if strings.TrimRight(r.Stdout, "\n") == value && r.Code == 0 {
			return Result{Stdout: value}
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			r.Stderr += fmt.Sprintf("difftest: timed out waiting for %q (last: %q)", value, strings.TrimRight(r.Stdout, "\n"))
			return r
		}
		t := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			t.Stop()
		case <-t.C:
		}
		delay = min(delay*2, 100*time.Millisecond)
	}
}

// withSize adds -x and -y to new-session when the script does not give them.
func withSize(args []string, w, h int) []string {
	if len(args) == 0 || (args[0] != "new-session" && args[0] != "new") {
		return args
	}
	for _, a := range args[1:] {
		if a == ";" {
			break
		}
		if a == "-x" || a == "-y" || strings.HasPrefix(a, "-x") || strings.HasPrefix(a, "-y") {
			return args
		}
	}
	out := []string{args[0], "-x", strconv.Itoa(w), "-y", strconv.Itoa(h)}
	return append(out, args[1:]...)
}

// Difference is one step where two engines disagree.
type Difference struct {
	Path string
	Step Step
	What string // "stdout", "stderr", "exit code", "steps"
	A, B string // the two values
	EngA string
	EngB string
}

func (d Difference) String() string {
	if d.Step.Kind == "screen" && d.What == "stdout" {
		return fmt.Sprintf("%s:%d: %s\n%s", d.Path, d.Step.Line, d.Step.Text, sideBySide(d.EngA, d.EngB, d.A, d.B))
	}
	return fmt.Sprintf("%s:%d: %s\n  %s differs:\n    %-10s %q\n    %-10s %q",
		d.Path, d.Step.Line, d.Step.Text, d.What, d.EngA+":", d.A, d.EngB+":", d.B)
}

// Compare lists the steps where two transcripts of one script differ.
func Compare(path string, a, b *Transcript) []Difference {
	var out []Difference
	if len(a.Entries) != len(b.Entries) {
		return []Difference{{
			Path: path, What: "steps", EngA: a.Engine, EngB: b.Engine,
			A: strconv.Itoa(len(a.Entries)), B: strconv.Itoa(len(b.Entries)),
		}}
	}
	for i := range a.Entries {
		ea, eb := a.Entries[i], b.Entries[i]
		add := func(what, x, y string) {
			out = append(out, Difference{Path: path, Step: ea.Step, What: what, A: x, B: y, EngA: a.Engine, EngB: b.Engine})
		}
		if ea.Result.Stdout != eb.Result.Stdout {
			add("stdout", ea.Result.Stdout, eb.Result.Stdout)
		}
		if ea.Result.Stderr != eb.Result.Stderr {
			add("stderr", ea.Result.Stderr, eb.Result.Stderr)
		}
		if ea.Result.Code != eb.Result.Code {
			add("exit code", strconv.Itoa(ea.Result.Code), strconv.Itoa(eb.Result.Code))
		}
	}
	return out
}

// RunPair runs a script on a reference engine and an engine under test and compares them.
// skipped is non-empty when the script skips the engine under test.
func RunPair(ctx context.Context, path string, ref, test Engine) (diffs []Difference, skipped string, err error) {
	s, err := ParseScript(path)
	if err != nil {
		return nil, "", err
	}
	tb, err := Execute(ctx, s, test)
	var skip *SkipError
	if errors.As(err, &skip) {
		return nil, skip.Reason, nil
	}
	if err != nil {
		return nil, "", err
	}
	ta, err := Execute(ctx, s, ref)
	if err != nil {
		return nil, "", err
	}
	// A wait the reference engine never got past is the script's mistake: both engines
	// timing out alike would hide it.
	for _, e := range ta.Entries {
		if e.Step.Kind == "wait" && strings.Contains(e.Result.Stderr, "difftest: timed out") {
			return nil, "", fmt.Errorf("%s:%d: %s: %s", path, e.Step.Line, ref.Name(), e.Result.Stderr)
		}
	}
	return Compare(path, ta, tb), "", nil
}
