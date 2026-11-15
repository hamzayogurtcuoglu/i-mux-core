// Package layoutfixture runs layout scenarios (testdata/layout/*.txt) on real tmux and records
// what each step gives, so that i-mux-core's layouts can be checked against tmux's without a
// server (task C2.5). cmd/layoutfixture writes the .golden files; internal/model replays the
// scenarios and compares.
package layoutfixture

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// A scenario is one window's story, a step per line:
//
//	new WxH                      the session and its window, one pane (%0)
//	border top|bottom|off        pane-border-status
//	split %N [-h] [-b] [-f] [-d] [-l N|N%]
//	kill %N                      kill-pane
//	resize %N -L|-R|-U|-D N      resize-pane
//	resize %N -x N|-y N
//	zoom %N                      resize-pane -Z (toggles)
//	select %N                    select-pane
//	swap %A %B [-d]              swap-pane -s %A -t %B
//	window WxH                   resize-window
//	layout <string>|$N|v1$N      select-layout: a string, or what step N showed (v2 or v1)
//
// Lines starting with # are comments.

// Step is one line of a scenario.
type Step struct {
	Line  int
	Words []string
	Text  string
}

// Read reads a scenario.
func Read(path string) ([]Step, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var steps []Step
	for i, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		words := strings.Fields(line)
		if words[0] == "layout" {
			words = []string{"layout", strings.TrimSpace(strings.TrimPrefix(line, "layout"))}
		}
		steps = append(steps, Step{Line: i + 1, Words: words, Text: line})
	}
	return steps, nil
}

// State is what a step left: an error, or the window.
type State struct {
	Step    string
	Error   string
	Window  string   // "WxH zoomed=0|1"
	Panes   []string // "%id left top width height index active"
	Layout  string   // #{window_layout}
	Visible string   // #{window_visible_layout}
	V1      string   // #{window_layout} as a control client sees it
}

// Format writes states as a .golden file.
func Format(states []State) string {
	var b strings.Builder
	for _, s := range states {
		fmt.Fprintf(&b, "> %s\n", s.Step)
		if s.Error != "" {
			fmt.Fprintf(&b, "error %s\n", s.Error)
		}
		fmt.Fprintf(&b, "window %s\n", s.Window)
		for _, p := range s.Panes {
			fmt.Fprintf(&b, "pane %s\n", p)
		}
		fmt.Fprintf(&b, "layout %s\nvisible %s\nv1 %s\n", s.Layout, s.Visible, s.V1)
	}
	return b.String()
}

// Resolve replaces a layout step's $N or v1$N by what step N (1-based) showed.
func Resolve(arg string, states []State) (string, error) {
	ref, v1 := strings.CutPrefix(arg, "v1$")
	if !v1 {
		var ok bool
		if ref, ok = strings.CutPrefix(arg, "$"); !ok {
			return arg, nil
		}
	}
	n, err := strconv.Atoi(ref)
	if err != nil || n < 1 || n > len(states) {
		return "", fmt.Errorf("bad step reference %q", arg)
	}
	if v1 {
		return states[n-1].V1, nil
	}
	return states[n-1].Layout, nil
}

// Size reads "WxH".
func Size(s string) (int, int, error) {
	w, h, ok := strings.Cut(s, "x")
	x, err1 := strconv.Atoi(w)
	y, err2 := strconv.Atoi(h)
	if !ok || err1 != nil || err2 != nil {
		return 0, 0, fmt.Errorf("bad size %q", s)
	}
	return x, y, nil
}

// tmux runs a scenario on one tmux server.
type tmux struct {
	bin, socket string
	env         []string
}

func (t *tmux) run(args ...string) (string, string, error) {
	cmd := exec.Command(t.bin, append([]string{"-S", t.socket, "-f", os.DevNull}, args...)...)
	cmd.Env = t.env
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	return out.String(), strings.TrimSpace(errb.String()), err
}

// v1 is #{window_layout} for a control client, which gets the old format.
func (t *tmux) v1() (string, error) {
	cmd := exec.Command(t.bin, "-S", t.socket, "-C", "attach", "-t", "s", "-r")
	cmd.Env = t.env
	cmd.Stdin = strings.NewReader("display -p -t @0 '#{window_layout}'\n")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(out), "\n") {
		if line != "" && !strings.HasPrefix(line, "%") {
			return line, nil
		}
	}
	return "", errors.New("no layout from the control client")
}

const program = "sleep 100000"

// FromTmux runs a scenario on the tmux at bin, in its own temporary HOME and socket, and
// returns each step's state.
func FromTmux(bin string, steps []Step) ([]State, error) {
	dir, err := os.MkdirTemp("/tmp", "imux-lf-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	t := &tmux{bin: bin, socket: filepath.Join(dir, "s"), env: []string{
		"HOME=" + dir, "TMUX_TMPDIR=" + dir, "PATH=/usr/bin:/bin", "SHELL=/bin/sh", "TERM=xterm-256color",
	}}
	defer func() { _, _, _ = t.run("kill-server") }()

	var states []State
	for _, st := range steps {
		args, err := tmuxArgs(st, states)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", st.Line, err)
		}
		s := State{Step: st.Text}
		if _, stderr, err := t.run(args...); err != nil {
			s.Error = stderr
			if s.Error == "" {
				s.Error = err.Error()
			}
		}
		out, _, err := t.run("display", "-p", "-t", "@0", "#{window_width}x#{window_height} zoomed=#{window_zoomed_flag}")
		if err != nil {
			return nil, fmt.Errorf("line %d: no window: %w", st.Line, err)
		}
		s.Window = strings.TrimSpace(out)
		out, _, _ = t.run("list-panes", "-t", "@0", "-F",
			"#{pane_id} #{pane_left} #{pane_top} #{pane_width} #{pane_height} #{pane_index} #{pane_active}")
		s.Panes = lines(out)
		out, _, _ = t.run("display", "-p", "-t", "@0", "#{window_layout}")
		s.Layout = strings.TrimSpace(out)
		out, _, _ = t.run("display", "-p", "-t", "@0", "#{window_visible_layout}")
		s.Visible = strings.TrimSpace(out)
		if s.V1, err = t.v1(); err != nil {
			return nil, fmt.Errorf("line %d: %w", st.Line, err)
		}
		states = append(states, s)
	}
	return states, nil
}

func lines(s string) []string {
	var out []string
	sc := bufio.NewScanner(strings.NewReader(s))
	for sc.Scan() {
		out = append(out, sc.Text())
	}
	return out
}

// tmuxArgs is the tmux command for a step.
func tmuxArgs(st Step, states []State) ([]string, error) {
	w := st.Words
	need := func(n int) error {
		if len(w) < n {
			return fmt.Errorf("%s: too few words", w[0])
		}
		return nil
	}
	switch w[0] {
	case "new":
		if err := need(2); err != nil {
			return nil, err
		}
		x, y, err := Size(w[1])
		if err != nil {
			return nil, err
		}
		return []string{"new-session", "-d", "-s", "s", "-x", strconv.Itoa(x), "-y", strconv.Itoa(y), program}, nil
	case "border":
		return []string{"set-option", "-w", "-t", "@0", "pane-border-status", w[1]}, need(2)
	case "split":
		if err := need(2); err != nil {
			return nil, err
		}
		args := append([]string{"split-window", "-t", w[1]}, w[2:]...)
		return append(args, program), nil
	case "kill":
		return []string{"kill-pane", "-t", w[1]}, need(2)
	case "resize":
		return append([]string{"resize-pane", "-t", w[1]}, w[2:]...), need(3)
	case "zoom":
		return []string{"resize-pane", "-Z", "-t", w[1]}, need(2)
	case "select":
		return []string{"select-pane", "-t", w[1]}, need(2)
	case "swap":
		if err := need(3); err != nil {
			return nil, err
		}
		return append([]string{"swap-pane", "-s", w[1], "-t", w[2]}, w[3:]...), nil
	case "window":
		if err := need(2); err != nil {
			return nil, err
		}
		x, y, err := Size(w[1])
		if err != nil {
			return nil, err
		}
		return []string{"resize-window", "-t", "@0", "-x", strconv.Itoa(x), "-y", strconv.Itoa(y)}, nil
	case "layout":
		s, err := Resolve(w[1], states)
		if err != nil {
			return nil, err
		}
		return []string{"select-layout", "-t", "@0", s}, nil
	}
	return nil, fmt.Errorf("unknown step %q", w[0])
}
