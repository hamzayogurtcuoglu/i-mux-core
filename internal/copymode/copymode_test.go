package copymode

import (
	"strings"
	"testing"
	"time"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/model"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/vt"
)

type testEnv struct{ copied []string }

func (e *testEnv) Style(*model.Pane, string) vt.Cell { return vt.DefaultCell }

func (e *testEnv) Expand(*model.Pane, string) string { return "" }

func (e *testEnv) Copy(_ *model.Pane, _ string, data []byte, _, _ bool) {
	e.copied = append(e.copied, string(data))
}

// newPane is a pane of 20x5 showing lines, with history.
func newPane(t *testing.T, lines ...string) *model.Pane {
	t.Helper()
	tr := model.NewTree()
	s, err := tr.NewSession("a", "/", time.Unix(1, 0))
	if err != nil {
		t.Fatal(err)
	}
	wl, err := tr.NewWindow(s, 0, 0, 20, 5)
	if err != nil {
		t.Fatal(err)
	}
	p := tr.NewPane(wl.Window, nil, false, 20, 5)
	p.Term = vt.NewTerminal(20, 5, 100)
	_, _ = p.Term.Write([]byte(strings.Join(lines, "\r\n")))
	return p
}

func run(m *Mode, args ...string) Action { return m.Command(Call{Args: args, Prefix: 1}) }

// TestCopySelection selects with the cursor and copies, as copy-selection does.
func TestCopySelection(t *testing.T) {
	p := newPane(t, "one", "two", "three", "four", "alpha beta-gamma", "last")
	env := &testEnv{}
	m := New(p, env, false, false)
	p.Mode = m
	m.Redraw()
	for _, c := range []string{"cursor-up", "cursor-up", "start-of-line", "begin-selection", "cursor-right", "cursor-right", "cursor-down"} {
		run(m, c)
	}
	run(m, "copy-selection")
	if len(env.copied) != 1 || env.copied[0] != "four\nal" {
		t.Fatalf("copied %q", env.copied)
	}
	run(m, "cursor-up")
	run(m, "select-word")
	run(m, "copy-selection")
	if len(env.copied) != 2 {
		t.Fatalf("select-word copied %q", env.copied)
	}
}
