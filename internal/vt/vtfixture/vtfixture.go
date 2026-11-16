// Package vtfixture turns a VT fixture's byte stream into the screen tmux shows for it, and
// formats screens the same way for tmux and for the engine, so they can be compared
// (TASKS.md C1.12). It is used by cmd/vtfixture and by internal/vt's tests.
package vtfixture

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// DefaultCols and DefaultRows are a fixture's size when its name gives none.
const (
	DefaultCols = 80
	DefaultRows = 24
)

var (
	reSizes = regexp.MustCompile(`@([0-9]+x[0-9]+(-[0-9]+x[0-9]+)*)\.in$`)
	reSize  = regexp.MustCompile(`^([0-9]+)x([0-9]+)$`)
)

// Sizes returns a fixture's sizes from its file name: name@WxH.in, or name@WxH-WxH….in for a
// fixture written at the first size and then resized to each of the others; 80x24 if none.
func Sizes(path string) [][2]int {
	m := reSizes.FindStringSubmatch(path)
	if m == nil {
		return [][2]int{{DefaultCols, DefaultRows}}
	}
	var out [][2]int
	for _, part := range strings.Split(m[1], "-") {
		p := reSize.FindStringSubmatch(part)
		c, _ := strconv.Atoi(p[1])
		r, _ := strconv.Atoi(p[2])
		out = append(out, [2]int{c, r})
	}
	return out
}

// Size returns the size a fixture is written at, and Final the size it ends at.
func Size(path string) (cols, rows int) {
	s := Sizes(path)
	return s[0][0], s[0][1]
}

// Final returns the size a fixture ends at.
func Final(path string) (cols, rows int) {
	s := Sizes(path)
	return s[len(s)-1][0], s[len(s)-1][1]
}

// State is what a fixture's screen shows, besides its cells.
type State struct {
	Cols, Rows  int
	CursorX     int
	CursorY     int
	Values      map[string]string // the flags of StateFormats, by name
	Screen      string            // capture-pane -p -e -N
	All         string            // capture-pane -p -e -N -S - -E -, when there is history
	Joined      string            // capture-pane -p -J -S - -E -
	Ranges      string            // with history: -p -J -S -2000, then -p -e -N -S -3 -E 1
	HistorySize int
}

// StateFormats are the tmux formats a fixture records, in order.
var StateFormats = []string{
	"cursor_flag", "cursor_shape", "cursor_blinking", "cursor_very_visible", "alternate_on",
	"insert_flag", "keypad_cursor_flag", "keypad_flag", "wrap_flag", "origin_flag",
	"mouse_standard_flag", "mouse_button_flag", "mouse_all_flag", "mouse_sgr_flag",
	"bracket_paste_flag", "synchronized_output_flag", "scroll_region_upper",
	"scroll_region_lower", "pane_title", "pane_path",
}

// InitialTitle stands for a pane's first title, which tmux sets to the host's name.
const InitialTitle = "HOST"

// Format writes a state as a .screen file.
func Format(s *State) string {
	var b strings.Builder
	fmt.Fprintf(&b, "size %dx%d\n", s.Cols, s.Rows)
	fmt.Fprintf(&b, "cursor %d,%d\n", s.CursorX, s.CursorY)
	for _, f := range StateFormats {
		fmt.Fprintf(&b, "%s %s\n", f, s.Values[f])
	}
	fmt.Fprintf(&b, "history_size %d\n", s.HistorySize)
	b.WriteString("--- screen\n")
	b.WriteString(s.Screen)
	if s.HistorySize > 0 {
		b.WriteString("--- history and screen\n")
		b.WriteString(s.All)
	}
	b.WriteString("--- joined\n")
	b.WriteString(s.Joined)
	if s.HistorySize > 0 {
		b.WriteString("--- ranges\n")
		b.WriteString(s.Ranges)
	}
	return b.String()
}

// FromTmux runs fixture in on tmux and returns its .screen text. self is a program that runs
// Helper when given "__write" (cmd/vtfixture, or a test binary).
func FromTmux(tmux, self, in string, timeout time.Duration) (string, error) {
	cols, rows := Size(in)
	in, err := filepath.Abs(in)
	if err != nil {
		return "", err
	}
	dir, err := os.MkdirTemp("/tmp", "imux-vt-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	sock := filepath.Join(dir, "s")
	marker := filepath.Join(dir, "done")
	env := []string{"HOME=" + dir, "TMUX_TMPDIR=" + dir, "TERM=xterm-256color", "LANG=C.UTF-8", "LC_ALL=C.UTF-8", "TZ=UTC", "PATH=/usr/bin:/bin"}
	run := func(args ...string) (string, error) {
		cmd := exec.Command(tmux, append([]string{"-S", sock, "-f", "/dev/null", "-u"}, args...)...)
		cmd.Env = env
		out, err := cmd.Output()
		if err != nil {
			if ee, ok := err.(*exec.ExitError); ok {
				return "", fmt.Errorf("tmux %s: %v: %s", strings.Join(args, " "), err, ee.Stderr)
			}
			return "", err
		}
		return string(out), nil
	}
	defer func() { _, _ = run("kill-server") }()
	helper := shellQuote(self) + " __write " + shellQuote(in) + " " + shellQuote(marker)
	if _, err := run("new-session", "-d", "-x", strconv.Itoa(cols), "-y", strconv.Itoa(rows), helper); err != nil {
		return "", err
	}
	deadline := time.Now().Add(timeout)
	for {
		if _, err := os.Stat(marker); err == nil {
			break
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("the helper did not finish within %v", timeout)
		}
		time.Sleep(10 * time.Millisecond)
	}
	for _, size := range Sizes(in)[1:] {
		if _, err := run("resize-window", "-t", "@0", "-x", strconv.Itoa(size[0]), "-y", strconv.Itoa(size[1])); err != nil {
			return "", err
		}
	}
	cols, rows = Final(in)
	s := &State{Cols: cols, Rows: rows, Values: map[string]string{}}
	var formats []string
	for _, f := range append([]string{"cursor_x", "cursor_y", "history_size"}, StateFormats...) {
		formats = append(formats, "#{"+f+"}")
	}
	out, err := run("display-message", "-p", "-t", "%0", strings.Join(formats, "\t"))
	if err != nil {
		return "", err
	}
	v := strings.Split(strings.TrimRight(out, "\n"), "\t")
	if len(v) != 3+len(StateFormats) {
		return "", fmt.Errorf("unexpected state %q", out)
	}
	s.CursorX, _ = strconv.Atoi(v[0])
	s.CursorY, _ = strconv.Atoi(v[1])
	s.HistorySize, _ = strconv.Atoi(v[2])
	host, _ := os.Hostname()
	for i, f := range StateFormats {
		s.Values[f] = v[3+i]
		if f == "pane_title" && host != "" && s.Values[f] == host {
			s.Values[f] = InitialTitle
		}
	}
	if s.Screen, err = run("capture-pane", "-p", "-e", "-N", "-t", "%0"); err != nil {
		return "", err
	}
	if s.HistorySize > 0 {
		if s.All, err = run("capture-pane", "-p", "-e", "-N", "-S", "-", "-E", "-", "-t", "%0"); err != nil {
			return "", err
		}
	}
	if s.Joined, err = run("capture-pane", "-p", "-J", "-S", "-", "-E", "-", "-t", "%0"); err != nil {
		return "", err
	}
	if s.HistorySize > 0 {
		a, err := run("capture-pane", "-p", "-J", "-S", "-2000", "-t", "%0")
		if err != nil {
			return "", err
		}
		b, err := run("capture-pane", "-p", "-e", "-N", "-S", "-3", "-E", "1", "-t", "%0")
		if err != nil {
			return "", err
		}
		s.Ranges = a + "---\n" + b
	}
	return Format(s), nil
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// Probe runs a probe file on an 80x24 pane of a tmux-compatible program (tmux, imux-core), with
// self as the helper that runs in the pane (it must handle "__probe" as cmd/vtfixture does),
// and returns the replies in replies.out's format.
func Probe(tmux, self, probe string, timeout time.Duration) (string, error) {
	probe, err := filepath.Abs(probe)
	if err != nil {
		return "", err
	}
	dir, err := os.MkdirTemp("/tmp", "imux-vt-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	sock, out, marker := filepath.Join(dir, "s"), filepath.Join(dir, "out"), filepath.Join(dir, "done")
	env := []string{"HOME=" + dir, "TMUX_TMPDIR=" + dir, "TERM=xterm-256color", "LANG=C.UTF-8", "LC_ALL=C.UTF-8", "TZ=UTC", "PATH=/usr/bin:/bin"}
	run := func(args ...string) error {
		cmd := exec.Command(tmux, append([]string{"-S", sock, "-f", "/dev/null", "-u"}, args...)...)
		cmd.Env = env
		if b, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("tmux %s: %v: %s", strings.Join(args, " "), err, b)
		}
		return nil
	}
	defer func() { _ = run("kill-server") }()
	helper := shellQuote(self) + " __probe " + shellQuote(probe) + " " + shellQuote(out) + " " + shellQuote(marker)
	if err := run("new-session", "-d", "-x", strconv.Itoa(DefaultCols), "-y", strconv.Itoa(DefaultRows), helper); err != nil {
		return "", err
	}
	deadline := time.Now().Add(timeout)
	for {
		if _, err := os.Stat(marker); err == nil {
			break
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("the probe did not finish within %v", timeout)
		}
		time.Sleep(10 * time.Millisecond)
	}
	b, err := os.ReadFile(out)
	return string(b), err
}
