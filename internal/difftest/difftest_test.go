//go:build difftest

package difftest

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// The differential tests: `make difftest` runs them with IMUX_DIFFTEST_TMUX (the pinned tmux)
// and IMUX_CORE_BIN (imux-core). Without the pinned tmux they skip and say why, unless
// IMUX_DIFFTEST_REQUIRE=1 (CI), where they fail.

func tmuxEngine(t *testing.T) *Binary {
	t.Helper()
	p := os.Getenv("IMUX_DIFFTEST_TMUX")
	b, err := NewBinary("tmux", p)
	if p == "" || err != nil {
		msg := "the pinned tmux is not built (make tmux), so the differential tests did not run"
		if p != "" {
			msg += ": " + err.Error()
		}
		if os.Getenv("IMUX_DIFFTEST_REQUIRE") == "1" {
			t.Fatal(msg)
		}
		t.Skip(msg)
	}
	return b
}

func coreEngine(t *testing.T) *Binary {
	t.Helper()
	b, err := NewBinary("imux-core", os.Getenv("IMUX_CORE_BIN"))
	if err != nil {
		t.Fatalf("IMUX_CORE_BIN: %v (run through make difftest)", err)
	}
	return b
}

// toolsOnce builds the scripts' programs once: the fake TUI, as faketui on the engines' PATH.
var toolsOnce = sync.OnceValues(func() (string, error) {
	dir, err := os.MkdirTemp("", "imux-dt-tools-")
	if err != nil {
		return "", err
	}
	name := "faketui"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	tui := filepath.Join(dir, name)
	out, err := exec.Command("go", "build", "-o", tui, "../../testdata/faketui").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("faketui: %w: %s", err, out)
	}
	// A new program's first start can be slow (macOS checks it): start it once now, so that
	// the scripts' screens do not depend on it.
	_ = exec.Command(tui, "-mode", "exit").Run()
	return dir, nil
})

func scripts(t *testing.T) []string {
	t.Helper()
	dir, err := toolsOnce()
	if err != nil {
		t.Fatal(err)
	}
	ToolsDir = dir
	paths, err := filepath.Glob("../../testdata/diff/*.txt")
	if err != nil || len(paths) == 0 {
		t.Fatalf("no scripts in testdata/diff: %v", err)
	}
	return paths
}

func report(t *testing.T, diffs []Difference) {
	t.Helper()
	for _, d := range diffs {
		t.Error(d.String())
	}
}

// TestScripts compares i-mux-core with tmux on every script.
func TestScripts(t *testing.T) {
	ref, core := tmuxEngine(t), coreEngine(t)
	for _, p := range scripts(t) {
		t.Run(strings.TrimSuffix(filepath.Base(p), ".txt"), func(t *testing.T) {
			diffs, skipped, err := RunPair(context.Background(), p, ref, core)
			if err != nil {
				t.Fatal(err)
			}
			if skipped != "" {
				t.Skip(skipped)
			}
			report(t, diffs)
		})
	}
}

// TestScriptsDeterministic runs every script on tmux twice: a script whose two runs differ needs
// more normalize lines, or it would report differences that are not i-mux-core's.
func TestScriptsDeterministic(t *testing.T) {
	ref := tmuxEngine(t)
	again := &Binary{Label: "tmux again", Path: ref.Path}
	for _, p := range scripts(t) {
		t.Run(strings.TrimSuffix(filepath.Base(p), ".txt"), func(t *testing.T) {
			diffs, _, err := RunPair(context.Background(), p, ref, again)
			if err != nil {
				t.Fatal(err)
			}
			report(t, diffs)
		})
	}
}

// replay is a stub engine that answers what tmux answered in tr; plant may change an answer.
// The same words asked again get the next answer recorded for them (the last one is kept).
func replay(name string, tr *Transcript, plant func(step Step, r Result) Result) *Func {
	answers := map[string][]Result{}
	for _, e := range tr.Entries {
		r := e.Result
		if e.Step.Kind == "wait" {
			_, _, value, _ := parseWait(e.Step.Args)
			r = Result{Stdout: value + "\n"}
		}
		if plant != nil {
			r = plant(e.Step, r)
		}
		k := strings.Join(e.Args, "\x00")
		answers[k] = append(answers[k], r)
	}
	return &Func{Label: name, Fn: func(args []string) Result {
		k := strings.Join(args, "\x00")
		if rs := answers[k]; len(rs) > 0 {
			if len(rs) > 1 {
				answers[k] = rs[1:]
			}
			return rs[0]
		}
		return Result{Stderr: "stub: no answer for " + strings.Join(args, " "), Code: 1}
	}}
}

// TestHarnessAgainstTmux checks the harness itself on the sample script: tmux against a stub
// that gives tmux's own answers agrees, and a planted difference is reported at its line.
func TestHarnessAgainstTmux(t *testing.T) {
	ref := tmuxEngine(t)
	p := "../../testdata/diff/sample.txt"
	s, err := ParseScript(p)
	if err != nil {
		t.Fatal(err)
	}
	tr, err := Execute(context.Background(), s, ref)
	if err != nil {
		t.Fatal(err)
	}
	if len(tr.Entries) == 0 || tr.Entries[0].Result.Code != 0 {
		t.Fatalf("tmux did not run the sample: %+v", tr.Entries)
	}
	diffs, _, err := RunPair(context.Background(), p, ref, replay("stub", tr, nil))
	if err != nil {
		t.Fatal(err)
	}
	report(t, diffs)

	planted := replay("planted", tr, func(st Step, r Result) Result {
		if st.Kind == "capture" {
			r.Stdout = strings.Replace(r.Stdout, "hello", "HELLO", 1)
		}
		return r
	})
	diffs, _, err = RunPair(context.Background(), p, ref, planted)
	if err != nil {
		t.Fatal(err)
	}
	if len(diffs) != 1 || diffs[0].Step.Kind != "capture" || diffs[0].What != "stdout" {
		t.Fatalf("planted difference: got %v", diffs)
	}
	if !strings.Contains(diffs[0].String(), "sample.txt:") || !strings.Contains(diffs[0].B, "HELLO") {
		t.Fatalf("message: %s", diffs[0])
	}
}

// TestNoServer compares the errors with no server: no socket, a socket left by a server that is
// gone (replaced when a server starts), and the socket the last session's server leaves.
func TestNoServer(t *testing.T) {
	ref, core := tmuxEngine(t), coreEngine(t)
	results := func(eng *Binary) []Result {
		env, err := NewEnv()
		if err != nil {
			t.Fatal(err)
		}
		defer os.RemoveAll(env.Dir)
		defer eng.Stop(env)
		var out []Result
		run := func(args ...string) Result {
			r := eng.Run(context.Background(), env, args)
			r.Stderr = strings.ReplaceAll(r.Stderr, env.Dir, "$DIR")
			return r
		}
		out = append(out, run("has-session"))
		l, err := net.ListenUnix("unix", &net.UnixAddr{Name: env.Socket, Net: "unix"})
		if err != nil {
			t.Fatal(err)
		}
		l.SetUnlinkOnClose(false)
		l.Close()
		out = append(out, run("has-session"), run("new-session", "-d", "-s", "a", "exec sleep 100"),
			run("kill-session", "-t", "a"))
		// The server exits soon after its last session.
		deadline := time.Now().Add(WaitTimeout)
		r := run("has-session")
		for strings.Contains(r.Stderr, "can't find session") || r.Code == 0 {
			if time.Now().After(deadline) {
				break
			}
			time.Sleep(10 * time.Millisecond)
			r = run("has-session")
		}
		return append(out, r, run("kill-server"))
	}
	a, b := results(ref), results(core)
	if !reflect.DeepEqual(a, b) {
		t.Errorf("tmux:\n%+v\nimux-core:\n%+v", a, b)
	}
	for _, r := range a {
		t.Logf("%+v", r)
	}
}

// TestEmptyCommandLine compares imux-core with no command (tmux's new-session) from a client
// whose stdin is not a terminal, with and without a server.
func TestEmptyCommandLine(t *testing.T) {
	ref, core := tmuxEngine(t), coreEngine(t)
	results := func(eng *Binary) []Result {
		env, err := NewEnv()
		if err != nil {
			t.Fatal(err)
		}
		defer os.RemoveAll(env.Dir)
		defer eng.Stop(env)
		run := func(args ...string) Result {
			r := eng.Run(context.Background(), env, args)
			r.Stderr = strings.ReplaceAll(r.Stderr, env.Dir, "$DIR")
			return r
		}
		return []Result{run(), run("new-session", "-d", "-s", "a", "exec sleep 100"), run(), run("has-session", "-t", "a")}
	}
	a, b := results(ref), results(core)
	if !reflect.DeepEqual(a, b) {
		t.Errorf("tmux:\n%+v\nimux-core:\n%+v", a, b)
	}
}

// TestHarnessScreen checks the screen step on the screen script: tmux against a stub that
// gives tmux's own screens agrees, and one planted cell is reported, with both screens shown
// side by side and the cell marked.
func TestHarnessScreen(t *testing.T) {
	ref := tmuxEngine(t)
	p := "../../testdata/diff/screen.txt"
	s, err := ParseScript(p)
	if err != nil {
		t.Fatal(err)
	}
	tr, err := Execute(context.Background(), s, ref)
	if err != nil {
		t.Fatal(err)
	}
	var screens []string
	for _, e := range tr.Entries {
		if e.Step.Kind == "screen" {
			if e.Result.Stderr != "" || !strings.Contains(e.Result.Stdout, "--- screen\n") {
				t.Fatalf("screen: %+v", e.Result)
			}
			screens = append(screens, e.Result.Stdout)
		}
	}
	if len(screens) < 4 || !strings.Contains(screens[0], "one pane") || !strings.Contains(screens[1], "│") {
		t.Fatalf("screens:\n%s", strings.Join(screens, "\n"))
	}
	t.Logf("tmux's first screen:\n%s", screens[0])
	diffs, _, err := RunPair(context.Background(), p, ref, replay("stub", tr, nil))
	if err != nil {
		t.Fatal(err)
	}
	report(t, diffs)

	first := 0
	for _, e := range tr.Entries {
		if e.Step.Kind == "screen" {
			first = e.Step.Line
			break
		}
	}
	planted := replay("planted", tr, func(st Step, r Result) Result {
		if st.Kind == "screen" && st.Line == first {
			r.Stdout = strings.Replace(r.Stdout, "one pane", "one Pane", 1)
		}
		return r
	})
	diffs, _, err = RunPair(context.Background(), p, ref, planted)
	if err != nil {
		t.Fatal(err)
	}
	if len(diffs) != 1 || diffs[0].Step.Kind != "screen" || diffs[0].What != "stdout" {
		t.Fatalf("planted difference: got %v", diffs)
	}
	msg := diffs[0].String()
	t.Logf("%s", msg)
	// Row 0 is marked, and the caret is under the planted cell: 7 columns of margin, the first
	// screen (40), " | ", then column 4.
	if !strings.Contains(msg, "screen.txt:9:") || !strings.Contains(msg, "  ≠  0 one pane ") ||
		!strings.Contains(msg, "\n"+strings.Repeat(" ", 7+40+3+4)+"^\n") || strings.Count(msg, "≠") != 1 {
		t.Fatalf("message:\n%s", msg)
	}
}

// TestInsidePaneTarget checks that a command run with TMUX_PANE in its environment defaults to
// that pane's session (and its current window) rather than the most recently active session.
func TestInsidePaneTarget(t *testing.T) {
	ref, core := tmuxEngine(t), coreEngine(t)
	run := func(eng *Binary) Result {
		env, err := NewEnv()
		if err != nil {
			t.Fatal(err)
		}
		defer os.RemoveAll(env.Dir)
		defer eng.Stop(env)
		ctx := context.Background()
		for _, args := range [][]string{
			{"new-session", "-d", "-s", "alpha", "-x", "40", "-y", "10", "exec cat"},
			{"set-option", "-g", "status", "off"},
			{"new-window", "-t", "alpha", "exec cat"},
			{"new-session", "-d", "-s", "beta", "-x", "40", "-y", "10", "exec cat"},
		} {
			if r := eng.Run(ctx, env, args); r.Code != 0 {
				t.Fatalf("%s %v: %+v", eng.Name(), args, r)
			}
		}
		// Without TMUX_PANE, the default target is beta (created after alpha); with TMUX_PANE=%0
		// (a pane in alpha), the default target is alpha's current window (%1).
		inside := *env
		inside.Vars = append(append([]string(nil), env.Vars...), "TMUX_PANE=%0")
		if r := eng.Run(ctx, &inside, []string{"send-keys", "from-inside", "Enter"}); r.Code != 0 {
			t.Fatalf("%s send-keys: %+v", eng.Name(), r)
		}
		c, err := eng.Attach(ctx, env, "c", 40, 10, 0, 0)
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		_ = c.Screen(ctx)
		if r := eng.Run(ctx, env, []string{"switch-client", "-t", "alpha"}); r.Code != 0 {
			t.Fatalf("%s switch-client: %+v", eng.Name(), r)
		}
		return c.Screen(ctx)
	}
	a, b := run(ref), run(core)
	if a != b || !strings.Contains(a.Stdout, "from-inside") {
		t.Errorf("tmux:\n%s\nimux-core:\n%s", a.Stdout, b.Stdout)
	}
}
