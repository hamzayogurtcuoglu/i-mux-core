// Package renderfixture records what a client terminal shows when it attaches to real tmux, and
// formats client screens the same way for tmux and for the engine, so that internal/render's
// frames can be compared with tmux's (TASKS.md C2.8). It is used by cmd/renderfixture and by
// internal/render's tests.
//
// A case is a testdata/render/<name>.txt file, one step per line:
//
//	# a comment
//	term <name>                 the client terminal: a case of testdata/term (<name>.json)
//	size WxH                    the client's size, and the window's (the status line is off)
//	window WxH                  the window's size, when it is not the client's (window-size manual)
//	pane <file>                 the first pane, which writes <file>'s bytes unchanged
//	split [-h] [-b] [-l N] [-t %N] <file>
//	                            split-window, the new pane writing <file>
//	select %N                   select-pane -t %N
//	zoom                        resize-pane -Z on the active pane
//
// The data files are next to the case. tmux's client screen is saved as <name>.screen.
package renderfixture

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/pty"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/vt"
)

// Step is one line of a case.
type Step struct {
	Kind string
	Args []string
}

// Case is a render case.
type Case struct {
	Name       string
	Dir        string // the case's directory, where its data files are
	Term       string // a testdata/term case
	Cols, Rows int    // the client's size
	WinX, WinY int    // the window's size, if not the client's
	Steps      []Step
}

// WindowSize is the window's size.
func (c *Case) WindowSize() (sx, sy int) {
	if c.WinX > 0 {
		return c.WinX, c.WinY
	}
	return c.Cols, c.Rows
}

var reSize = regexp.MustCompile(`^([0-9]+)x([0-9]+)$`)

// ReadCase reads a case file.
func ReadCase(path string) (*Case, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	c := &Case{Name: strings.TrimSuffix(filepath.Base(path), ".txt"), Dir: filepath.Dir(path), Cols: 80, Rows: 24}
	sc := bufio.NewScanner(f)
	n := 0
	for sc.Scan() {
		n++
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		w := strings.Fields(line)
		bad := func(msg string) error { return fmt.Errorf("%s:%d: %s: %s", path, n, w[0], msg) }
		switch w[0] {
		case "term":
			if len(w) != 2 {
				return nil, bad("want a terminal case")
			}
			c.Term = w[1]
		case "size":
			m := reSize.FindStringSubmatch(strings.Join(w[1:], " "))
			if m == nil {
				return nil, bad("want WxH")
			}
			c.Cols, _ = strconv.Atoi(m[1])
			c.Rows, _ = strconv.Atoi(m[2])
		case "window":
			m := reSize.FindStringSubmatch(strings.Join(w[1:], " "))
			if m == nil {
				return nil, bad("want WxH")
			}
			c.WinX, _ = strconv.Atoi(m[1])
			c.WinY, _ = strconv.Atoi(m[2])
		case "zoom":
			if len(w) != 1 {
				return nil, bad("takes nothing")
			}
		case "pane":
			if len(w) != 2 || len(c.Steps) != 0 {
				return nil, bad("want one file, as the first step")
			}
		case "split":
			if len(w) < 2 {
				return nil, bad("want a file")
			}
			if _, err := ParseSplit(w[1:]); err != nil {
				return nil, bad(err.Error())
			}
		case "select":
			if len(w) != 2 || !strings.HasPrefix(w[1], "%") {
				return nil, bad("want a pane")
			}
		default:
			return nil, bad("unknown step")
		}
		if w[0] != "term" && w[0] != "size" && w[0] != "window" {
			c.Steps = append(c.Steps, Step{Kind: w[0], Args: w[1:]})
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if c.Term == "" || len(c.Steps) == 0 || c.Steps[0].Kind != "pane" {
		return nil, fmt.Errorf("%s: want a term and a pane step", path)
	}
	return c, nil
}

// Split is a split step's flags.
type Split struct {
	Horizontal, Before bool
	Size               int // -l, or -1
	Target             string
	File               string
}

// ParseSplit reads a split step's words.
func ParseSplit(args []string) (Split, error) {
	s := Split{Size: -1}
	for len(args) > 1 {
		switch args[0] {
		case "-h":
			s.Horizontal = true
		case "-b":
			s.Before = true
		case "-l":
			n, err := strconv.Atoi(args[1])
			if err != nil {
				return s, fmt.Errorf("bad -l")
			}
			s.Size = n
			args = args[1:]
		case "-t":
			s.Target = args[1]
			args = args[1:]
		default:
			return s, fmt.Errorf("unknown flag %s", args[0])
		}
		args = args[1:]
	}
	if len(args) != 1 {
		return s, fmt.Errorf("want a file")
	}
	s.File = args[0]
	return s, nil
}

// Term is a recorded client terminal (testdata/term/<name>.json, made by cmd/termfixture).
type Term struct {
	Name      string   `json:"name"`
	Term      string   `json:"term"`
	ColorTerm string   `json:"colorterm"`
	Features  []string `json:"features"`
	Overrides []string `json:"overrides"`
	Caps      []string `json:"caps"`
}

// LoadTerm reads testdata/term/<name>.json from dir.
func LoadTerm(dir, name string) (*Term, error) {
	b, err := os.ReadFile(filepath.Join(dir, name+".json"))
	if err != nil {
		return nil, err
	}
	var t Term
	if err := json.Unmarshal(b, &t); err != nil {
		return nil, err
	}
	return &t, nil
}

// Dump formats what a client terminal shows: its size, the cursor, the modes an attached
// client's terminal is put in, and every line. A line is its characters, with the style of
// the cells that follow in braces wherever it changes. Only what can be seen is kept: a blank
// cell's foreground and attributes that do not show on a blank are dropped, blank cells in
// the default background at the end of a line are left out, and hyperlink ids, which number
// links across a whole server, are numbered in the order they appear.
func Dump(t *vt.Terminal) string {
	s := t.Screen
	var b strings.Builder
	fmt.Fprintf(&b, "size %dx%d\n", s.SizeX(), s.SizeY())
	fmt.Fprintf(&b, "cursor %d,%d\n", s.Cx, s.Cy)
	flag := func(name string, on bool) { fmt.Fprintf(&b, "%s %d\n", name, b2i(on)) }
	flag("cursor_flag", s.Mode&vt.ModeCursor != 0)
	fmt.Fprintf(&b, "cursor_shape %d\n", s.CStyle)
	flag("cursor_blinking", s.Mode&vt.ModeCursorBlinking != 0)
	fmt.Fprintf(&b, "cursor_colour %#x\n", s.CColour)
	flag("alternate_on", s.IsAlternate())
	flag("mouse_standard_flag", s.Mode&vt.ModeMouseStandard != 0)
	flag("mouse_button_flag", s.Mode&vt.ModeMouseButton != 0)
	flag("mouse_all_flag", s.Mode&vt.ModeMouseAll != 0)
	flag("mouse_sgr_flag", s.Mode&vt.ModeMouseSGR != 0)
	b.WriteString("--- screen\n")
	links := map[string]int{}
	for y := range s.SizeY() {
		b.WriteString(dumpLine(s, y, links))
		b.WriteByte('\n')
	}
	return b.String()
}

// Attributes that show on a blank cell.
const blankVisible = vt.AttrReverse | vt.AttrAllUnderscore | vt.AttrStrikethrough | vt.AttrOverline

// look is a cell's style as it can be seen.
func look(c *vt.Cell, link string) string {
	fg, us, attr := c.Fg, c.Us, int(c.Attr)
	if c.Data == " " || c.Flags&vt.FlagCleared != 0 {
		attr &= blankVisible
		if attr&vt.AttrReverse == 0 {
			fg = 8
		}
		if attr&vt.AttrAllUnderscore == 0 {
			us = 8
		}
	}
	var parts []string
	if fg != 8 {
		parts = append(parts, fmt.Sprintf("fg=%#x", int32(fg)))
	}
	if c.Bg != 8 {
		parts = append(parts, fmt.Sprintf("bg=%#x", int32(c.Bg)))
	}
	if us != 8 {
		parts = append(parts, fmt.Sprintf("us=%#x", int32(us)))
	}
	if attr != 0 {
		parts = append(parts, fmt.Sprintf("attr=%#x", attr))
	}
	if link != "" {
		parts = append(parts, "link="+link)
	}
	return "{" + strings.Join(parts, " ") + "}"
}

func dumpLine(s *vt.Screen, y int, links map[string]int) string {
	gd := s.Grid
	link := func(c *vt.Cell) string {
		uri, id, ok := s.Hyperlinks.Get(c.Link)
		if c.Link == 0 || !ok {
			return ""
		}
		k := id + "\x00" + uri
		if _, seen := links[k]; !seen {
			links[k] = len(links) + 1
		}
		return fmt.Sprintf("%d:%s", links[k], uri)
	}
	end := 0
	for x := range s.SizeX() {
		c := gd.ViewCell(x, y)
		blank := (c.Data == " " || c.Flags&vt.FlagCleared != 0) && c.Bg == 8 &&
			int(c.Attr)&blankVisible == 0 && c.Link == 0
		if !blank && c.Flags&vt.FlagPadding == 0 {
			end = x + max(1, int(c.Width))
		}
	}
	var b strings.Builder
	cur := look(&vt.DefaultCell, "")
	for x := 0; x < end && x < s.SizeX(); x++ {
		c := gd.ViewCell(x, y)
		if c.Flags&vt.FlagPadding != 0 {
			continue
		}
		if st := look(&c, link(&c)); st != cur {
			b.WriteString(st)
			cur = st
		}
		if c.Data == "{" || c.Data == "\\" {
			b.WriteByte('\\')
		}
		b.WriteString(c.Data)
	}
	return b.String()
}

func b2i(v bool) int {
	if v {
		return 1
	}
	return 0
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// FromTmux runs case c on tmux, attaches a client of the case's terminal and size in a PTY,
// feeds what tmux draws into an emulator once it is quiet, and returns the Dump. self is a
// program that runs vtfixture.Helper when given "__write" (cmd/renderfixture); termDir holds
// testdata/term's cases and terminfo is the terminfo directory tmux reads.
func FromTmux(tmux, self, termDir, terminfo string, c *Case, timeout time.Duration) (string, error) {
	tc, err := LoadTerm(termDir, c.Term)
	if err != nil {
		return "", err
	}
	dir, err := os.MkdirTemp("/tmp", "imux-rf-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	sock := filepath.Join(dir, "s")
	env := []string{
		"HOME=" + dir, "TMUX_TMPDIR=" + dir, "PATH=/usr/bin:/bin", "SHELL=/bin/sh",
		"LANG=C.UTF-8", "LC_ALL=C.UTF-8", "TZ=UTC", "TERM=" + tc.Term, "TERMINFO=" + terminfo,
	}
	if tc.ColorTerm != "" {
		env = append(env, "COLORTERM="+tc.ColorTerm)
	}
	var conf strings.Builder
	conf.WriteString("set -g status off\n")
	if c.WinX > 0 {
		conf.WriteString("set -g window-size manual\n")
	}
	for _, f := range tc.Features {
		fmt.Fprintf(&conf, "set -as terminal-features ',%s'\n", f)
	}
	for _, o := range tc.Overrides {
		fmt.Fprintf(&conf, "set -as terminal-overrides ',%s'\n", o)
	}
	confPath := filepath.Join(dir, "c.conf")
	if err := os.WriteFile(confPath, []byte(conf.String()), 0o600); err != nil {
		return "", err
	}
	run := func(args ...string) error {
		cmd := exec.Command(tmux, append([]string{"-u", "-S", sock, "-f", confPath}, args...)...)
		cmd.Env = env
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("tmux %s: %v: %s", strings.Join(args, " "), err, out)
		}
		return nil
	}
	defer func() { _ = run("kill-server") }()
	markers := 0
	helper := func(file string) (string, string, error) {
		abs, err := filepath.Abs(filepath.Join(c.Dir, file))
		if err != nil {
			return "", "", err
		}
		markers++
		marker := filepath.Join(dir, "done"+strconv.Itoa(markers))
		return shellQuote(self) + " __write " + shellQuote(abs) + " " + shellQuote(marker), marker, nil
	}
	wait := func(marker string) error {
		deadline := time.Now().Add(timeout)
		for {
			if _, err := os.Stat(marker); err == nil {
				return nil
			}
			if time.Now().After(deadline) {
				return fmt.Errorf("a pane did not finish writing within %v", timeout)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	for _, st := range c.Steps {
		switch st.Kind {
		case "pane":
			cmd, marker, err := helper(st.Args[0])
			if err != nil {
				return "", err
			}
			sx, sy := c.WindowSize()
			if err := run("new-session", "-d", "-s", "a", "-x", strconv.Itoa(sx), "-y", strconv.Itoa(sy), cmd); err != nil {
				return "", err
			}
			if err := wait(marker); err != nil {
				return "", err
			}
		case "split":
			sp, _ := ParseSplit(st.Args)
			cmd, marker, err := helper(sp.File)
			if err != nil {
				return "", err
			}
			args := []string{"split-window", "-d"}
			if sp.Horizontal {
				args = append(args, "-h")
			}
			if sp.Before {
				args = append(args, "-b")
			}
			if sp.Size >= 0 {
				args = append(args, "-l", strconv.Itoa(sp.Size))
			}
			if sp.Target != "" {
				args = append(args, "-t", sp.Target)
			}
			if err := run(append(args, cmd)...); err != nil {
				return "", err
			}
			if err := wait(marker); err != nil {
				return "", err
			}
		case "select":
			if err := run("select-pane", "-t", st.Args[0]); err != nil {
				return "", err
			}
		case "zoom":
			if err := run("resize-pane", "-Z"); err != nil {
				return "", err
			}
		}
	}
	attached, err := attach(tmux, sock, confPath, env, dir, c)
	if err != nil {
		return "", err
	}
	defer attached.close()
	attached.waitQuiet()
	client := vt.NewTerminal(c.Cols, c.Rows, 0)
	_, _ = client.Write(attached.out.bytes())
	return Dump(client), nil
}

type attachedClient struct {
	p   pty.Proc
	out safeBuffer
}

func attach(tmux, sock, conf string, env []string, dir string, c *Case) (*attachedClient, error) {
	p, err := pty.New().Start(pty.Spec{
		Path: tmux, Argv: []string{"tmux", "-u", "-S", sock, "-f", conf, "attach", "-t", "a"},
		Env: env, Dir: dir, Cols: c.Cols, Rows: c.Rows,
	})
	if err != nil {
		return nil, err
	}
	a := &attachedClient{p: p}
	go func() {
		b := make([]byte, 65536)
		for {
			n, err := p.Read(b)
			a.out.write(b[:n])
			if err != nil {
				return
			}
		}
	}()
	return a, nil
}

// waitQuiet waits until the client has written something and then nothing for 500 ms.
func (a *attachedClient) waitQuiet() {
	last := -1
	for {
		time.Sleep(500 * time.Millisecond)
		n := len(a.out.bytes())
		if n > 0 && n == last {
			return
		}
		last = n
	}
}

func (a *attachedClient) close() {
	_ = a.p.Kill()
	_ = a.p.Close()
}

type safeBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *safeBuffer) write(p []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.b.Write(p)
}

func (s *safeBuffer) bytes() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return bytes.Clone(s.b.Bytes())
}
