package commands

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/format"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/cmdlang"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/keys"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/model"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/pty"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/status"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/style"
	tgt "github.com/hamzayogurtcuoglu/i-mux-core/internal/target"
)

// fakeHost runs the commands on a tree without programs.
type fakeHost struct {
	tree     *model.Tree
	specs    []pty.Spec
	spawnErr error
	killed   bool
	clients  []*ClientInfo
	messages []string
	detached []string
	sent     []string // PaneKey: "%id key"
	options  []string // SetServerOption: "name=choice"
	switched []string // SwitchClient: "client session"
	tables   []string // SetKeyTable: "client table"
}

func newFakeHost() *fakeHost { return &fakeHost{tree: model.NewTree()} }

func (h *fakeHost) Tree() *model.Tree { return h.tree }
func (h *fakeHost) Now() time.Time    { return time.Unix(1e9, 0) }
func (h *fakeHost) Spawn(_ *model.Pane, spec pty.Spec) error {
	h.specs = append(h.specs, spec)
	return h.spawnErr
}
func (h *fakeHost) RemoveSession(s *model.Session) { h.tree.RemoveSession(s) }
func (h *fakeHost) KillServer()                    { h.killed = true }
func (h *fakeHost) Environ() []string {
	return []string{"HOME=/home/u", "SHELL=/bin/sh", "TMUX=old", "PATH=/bin"}
}
func (h *fakeHost) Clients() []*ClientInfo { return h.clients }
func (h *fakeHost) Detach(name string)     { h.detached = append(h.detached, name) }
func (h *fakeHost) PaneKey(p *model.Pane, k keys.Key) bool {
	h.sent = append(h.sent, fmt.Sprintf("%%%d %s", p.ID, k))
	return k != keys.Parse("C-BSpace")
}

func (h *fakeHost) OptionChanged(name string)             { h.options = append(h.options, name) }
func (h *fakeHost) DefaultSize(*model.Session) (int, int) { return 80, 24 }
func (h *fakeHost) RemovePane(p *model.Pane) {
	w := p.Window
	if h.tree.RemovePane(p) {
		s := w.Session()
		if h.tree.RemoveWindow(w) {
			h.tree.RemoveSession(s)
		}
	}
}

func (h *fakeHost) SwitchClient(name string, s *model.Session) error {
	h.switched = append(h.switched, name+" "+s.Name)
	return nil
}

func (h *fakeHost) SetKeyTable(client, table string) error {
	h.tables = append(h.tables, client+" "+table)
	return nil
}

func (h *fakeHost) SocketPath() string { return "/tmp/s" }
func (h *fakeHost) Pid() int           { return 42 }
func (h *fakeHost) ServerInfo() *format.Server {
	return &format.Server{Pid: 42, Start: time.Unix(1e9, 0), SocketPath: "/tmp/s", Version: "3.8"}
}

func (h *fakeHost) Overlay(string) (bool, bool)            { return false, false }
func (h *fakeHost) ClearOverlay(string)                    {}
func (h *fakeHost) DisplayPopup(PopupSpec, func(int)) bool { return false }
func (h *fakeHost) ModifyPopup(string, PopupChange)        {}
func (h *fakeHost) ClientGeometry(string) ClientGeometry   { return ClientGeometry{StatusAt: -1} }

func (h *fakeHost) ShowMessage(client, msg string, _ MessageOptions) {
	h.messages = append(h.messages, client+": "+msg)
}

func run(h Host, words ...string) Result {
	return Run(h, &Client{Cwd: testCwd}, [][]string{words})
}

// testCwd is the client's directory: one that exists on every system.
var testCwd = os.TempDir()

func TestNewSession(t *testing.T) {
	h := newFakeHost()
	if r := run(h, "new", "-d", "-s", "a", "-x", "100", "-y", "30", "exec sleep 9"); r.Failed {
		t.Fatal(r.Stderr)
	}
	s := h.tree.SessionByName("a")
	if s == nil || s.Cwd != testCwd {
		t.Fatalf("session %+v", s)
	}
	w := s.Current.Window
	if w.Name != "sleep" || w.Sx != 100 || w.Sy != 30 || w.Active == nil {
		t.Errorf("window %q %dx%d", w.Name, w.Sx, w.Sy)
	}
	spec := h.specs[0]
	if spec.Path != "/bin/sh" || !slices.Equal(spec.Argv, []string{"sh", "-c", "exec sleep 9"}) || spec.Dir != testCwd {
		t.Errorf("spec %+v", spec)
	}
	env := strings.Join(spec.Env, "\n")
	for _, want := range []string{"TMUX=/tmp/s,42,0", "TMUX_PANE=%0", "TERM=tmux-256color", "PATH=/bin"} {
		if !strings.Contains(env, want) {
			t.Errorf("environment lacks %s:\n%s", want, env)
		}
	}
	if strings.Contains(env, "TMUX=old") {
		t.Error("the server's own TMUX was passed on")
	}

	// No command: a login shell. Several words: run as they are.
	run(h, "new", "-d", "-s", "b")
	if spec := h.specs[1]; spec.Path != "/bin/sh" || !slices.Equal(spec.Argv, []string{"-sh"}) {
		t.Errorf("login shell %+v", spec)
	}
	if name := h.tree.SessionByName("b").Current.Window.Name; name != "sh" {
		t.Errorf("window name %q", name)
	}
	other := t.TempDir()
	run(h, "new", "-d", "-s", "c", "-n", "win", "-c", other, "printf", "%s", "x y")
	if spec := h.specs[2]; spec.Path != "" || !slices.Equal(spec.Argv, []string{"printf", "%s", "x y"}) || spec.Dir != other {
		t.Errorf("argv %+v", spec)
	}
	if name := h.tree.SessionByName("c").Current.Window.Name; name != "win" {
		t.Errorf("window name %q", name)
	}
	// A directory that is not there: home, else / (the fake host's home is not there either).
	run(h, "new", "-d", "-s", "d", "-c", "/nonexistent", "x")
	if spec := h.specs[3]; spec.Dir != "/" {
		t.Errorf("dir %q", spec.Dir)
	}

	for _, tc := range []struct {
		words []string
		err   string
	}{
		{[]string{"new", "-d", "-s", "a"}, "duplicate session: a"},
		{[]string{"new", "-d", "-x", "abc"}, "width invalid"},
		{[]string{"new", "-d", "-y", "0"}, "height too small"},
		{[]string{"new", "-d", "-x", "65536"}, "width too large"},
	} {
		if r := run(h, tc.words...); !r.Failed || r.Stderr != tc.err+"\n" {
			t.Errorf("%q: %+v, want %q", tc.words, r, tc.err)
		}
	}

	// A program that cannot start leaves no session behind.
	h.spawnErr = errors.New("no such program")
	if r := run(h, "new", "-d", "-s", "bad", "x"); r.Stderr != "create window failed: no such program\n" {
		t.Errorf("%+v", r)
	}
	if h.tree.SessionByName("bad") != nil {
		t.Error("the failed session is still there")
	}
}

func TestSessionTargets(t *testing.T) {
	h := newFakeHost()
	for _, n := range []string{"alpha", "beta", "bezel"} {
		if r := run(h, "new", "-d", "-s", n); r.Failed {
			t.Fatal(r.Stderr)
		}
	}
	for target, want := range map[string]string{
		"alpha": "alpha", "=alpha": "alpha", "al": "alpha", "$1": "beta", "beta:0": "beta",
		"bez*": "bezel", "be": "", "=al": "", "$9": "", "nope": "",
	} {
		st, err := tgt.Find(&tgt.Context{Tree: h.tree}, target, tgt.Session, 0)
		switch {
		case want == "" && err == nil:
			t.Errorf("%q found %s", target, st.S.Name)
		case want != "" && (err != nil || st.S.Name != want):
			t.Errorf("%q: %v %v, want %s", target, st.S, err, want)
		}
	}
	if r := run(h, "has", "-t", "=al"); r.Stderr != "can't find session: al\n" {
		t.Errorf("%+v", r)
	}
	// One failing command stops the rest of the list.
	r := Run(h, nil, [][]string{{"has", "-t", "zz"}, {"kill-session", "-t", "alpha"}})
	if !r.Failed || h.tree.SessionByName("alpha") == nil {
		t.Errorf("%+v; the list went on after an error", r)
	}
	if r := run(h, "kill-session", "-t", "alpha"); r.Failed || h.tree.SessionByName("alpha") != nil {
		t.Errorf("kill-session %+v", r)
	}
	if r := run(h, "kill-server"); r.Failed || !h.killed {
		t.Errorf("kill-server %+v", r)
	}
	// list-keys works (C3.8); with no bindings it prints nothing.
	if r := run(h, "list-keys"); r.Failed || r.Stdout != "" {
		t.Errorf("list-keys %+v", r)
	}
}

func TestRunConfig(t *testing.T) {
	h := newFakeHost()
	causes := RunConfig(h, "/c.conf", "new -s one\nhas -t zz ; new -s skipped\n\nhas -t zz\nnew -s two\n", cmdlang.Options{})
	if len(causes) != 2 || causes[0] != "/c.conf:2: can't find session: zz" ||
		causes[1] != "/c.conf:4: can't find session: zz" {
		t.Errorf("causes %q", causes)
	}
	// A config file has no client: new-session is detached even without -d.
	if h.tree.SessionByName("one") == nil || h.tree.SessionByName("two") == nil || h.tree.SessionByName("skipped") != nil {
		t.Errorf("sessions %v", h.tree.Sessions())
	}
}

func TestWindowsAndPanesAndCurrentTarget(t *testing.T) {
	h := newFakeHost()
	// A command list carries its current target from new-session to new-window, split-window,
	// select-pane, select-window and send-keys.
	r := Run(h, &Client{Cwd: testCwd}, [][]string{
		{"new-session", "-d", "-s", "alpha", "cat"},
		{"new-session", "-s", "beta", "-d", "cat"},
		// Because both had -d, current is not set yet; new-window targets the best session
		// (alpha, first by name on a tie) and sets current to the new window (@2, %2).
		{"new-window", "-n", "logs", "cat"},
		{"split-window", "-h", "cat"},
		{"send-keys", "hi"},
		{"select-pane", "-l"},
		{"send-keys", "lo"},
		{"select-window", "-l"},
		{"send-keys", "w0"},
	})
	if r.Failed {
		t.Fatalf("command list failed: %+v", r)
	}
	if want := []string{"%3 h", "%3 i", "%2 l", "%2 o", "%0 w", "%0 0"}; !slices.Equal(h.sent, want) {
		t.Errorf("sent %q, want %q", h.sent, want)
	}

	// Inside a pane (TMUX_PANE=%1 in beta), the default target is beta's current window.
	h.sent = nil
	r = Run(h, &Client{Cwd: testCwd, Env: map[string]string{"TMUX_PANE": "%1"}}, [][]string{
		{"send-keys", "in"},
	})
	if r.Failed || !slices.Equal(h.sent, []string{"%1 i", "%1 n"}) {
		t.Errorf("inside pane: sent %q (%+v)", h.sent, r)
	}
}

func (h *fakeHost) Respawn(p *model.Pane, spec pty.Spec) error { return h.Spawn(p, spec) }

// A quiet error (unbind-key -q) stops the rest of its group with no message, and the client
// does not fail: tmux sets the exit status only with an error message.
func TestQuietErrorStopsGroup(t *testing.T) {
	h := newFakeHost()
	r := Run(h, nil, [][]string{{"unbind", "-q", "-T", "nope", "x"}, {"new", "-d", "-s", "after"}})
	if r.Failed || r.Stderr != "" || h.tree.SessionByName("after") != nil {
		t.Errorf("%+v, after %v", r, h.tree.SessionByName("after") != nil)
	}
	r = Run(h, nil, [][]string{{"unbind", "-T", "nope", "x"}})
	if !r.Failed || r.Stderr != "table nope doesn't exist\n" {
		t.Errorf("%+v", r)
	}
}

func (h *fakeHost) RunJob(spec JobSpec, done func(out []byte, status pty.ExitStatus, err error)) {
	done(nil, pty.ExitStatus{}, errors.New("no jobs in the fake host"))
}
func (h *fakeHost) After(time.Duration, func()) {}

// popupFormats: the popup_* variables tmux gives -x and -y.
func TestPopupFormats(t *testing.T) {
	tr := model.NewTree()
	s, _ := tr.NewSession("s", "/", time.Unix(0, 0))
	wl, _ := tr.NewWindow(s, -1, 0, 80, 23)
	p := tr.NewPane(wl.Window, nil, false, 80, 23)
	g := ClientGeometry{
		Sx: 80, Sy: 24, StatusAt: 23, StatusLines: 1,
		Ranges: [][]status.Range{{{Type: style.RangeWindow, Arg: uint32(wl.Index), Start: 5, End: 9}}},
	}
	v := map[string]string{}
	popupFormats(v, g, tgt.State{S: s, WL: wl, P: p}, 20, 10)
	want := map[string]string{
		"popup_centre_x": "29", "popup_centre_y": "16", "popup_width": "20", "popup_height": "10",
		"popup_status_line_y": "23", "popup_window_status_line_x": "5", "popup_window_status_line_y": "23",
		"popup_pane_left": "0", "popup_pane_right": "60", "popup_pane_top": "10", "popup_pane_bottom": "23",
	}
	for k, w := range want {
		if v[k] != w {
			t.Errorf("%s = %q, want %q", k, v[k], w)
		}
	}
	if strtol(" 12x") != 12 || strtol("-3") != -3 || strtol("x") != 0 {
		t.Error("strtol")
	}
}
