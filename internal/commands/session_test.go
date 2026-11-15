package commands

import (
	"errors"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/cmdlang"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/model"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/pty"
)

// fakeHost runs the commands on a tree without programs.
type fakeHost struct {
	tree     *model.Tree
	specs    []pty.Spec
	spawnErr error
	killed   bool
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
func (h *fakeHost) SocketPath() string { return "/tmp/s" }
func (h *fakeHost) Pid() int           { return 42 }

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
		s, err := findSession(h.tree, target, true)
		switch {
		case want == "" && err == nil:
			t.Errorf("%q found %s", target, s.Name)
		case want != "" && (err != nil || s.Name != want):
			t.Errorf("%q: %v %v, want %s", target, s, err, want)
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
	if r := run(h, "list-windows"); r.Stderr != "list-windows: not implemented by imux-core yet\n" {
		t.Errorf("%+v", r)
	}
}

func TestRunConfig(t *testing.T) {
	h := newFakeHost()
	causes := RunConfig(h, "/c.conf", "new -s one\nhas -t zz ; new -s skipped\n\nsplitp\nnew -s two\n", cmdlang.Options{})
	if len(causes) != 2 || causes[0] != "/c.conf:2: can't find session: zz" ||
		causes[1] != "/c.conf:4: split-window: not implemented by imux-core yet" {
		t.Errorf("causes %q", causes)
	}
	// A config file has no client: new-session is detached even without -d.
	if h.tree.SessionByName("one") == nil || h.tree.SessionByName("two") == nil || h.tree.SessionByName("skipped") != nil {
		t.Errorf("sessions %v", h.tree.Sessions())
	}
}
