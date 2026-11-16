package render

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/model"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/render/renderfixture"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/termout"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/vt"
)

// The render cases (testdata/render/*.txt): the same panes are made in a tree, drawn for a
// client of the case's terminal, and what the client's terminal shows is compared with what it
// shows on the pinned tmux (cmd/renderfixture). Each case is drawn twice: once whole after
// every step, and once a frame per step, so that the diffs between frames are drawn too.

const termDir = "../../testdata/term"

// testClient is a client terminal: what the server writes to it, and what it shows.
type testClient struct {
	tty    *termout.TTY
	screen *vt.Terminal
}

func newTestClient(t testing.TB, termCase string, sx, sy int) *testClient {
	t.Helper()
	tc, err := renderfixture.LoadTerm(termDir, termCase)
	if err != nil {
		t.Fatal(err)
	}
	o := termout.Options{
		TerminalFeatures:  append(append([]string(nil), termout.DefaultOptions.TerminalFeatures...), tc.Features...),
		TerminalOverrides: append(append([]string(nil), termout.DefaultOptions.TerminalOverrides...), tc.Overrides...),
		ColorTerm:         tc.ColorTerm,
	}
	term, err := termout.NewTerm(tc.Term, tc.Caps, &termout.Features{}, o)
	if err != nil {
		t.Fatal(err)
	}
	c := &testClient{tty: termout.NewTTY(term, sx, sy), screen: vt.NewTerminal(sx, sy, 0)}
	c.tty.UTF8 = true
	c.tty.Start()
	c.tty.SendRequests()
	c.flush()
	return c
}

func (c *testClient) flush() []byte {
	b := c.tty.Take()
	_, _ = c.screen.Write(b)
	return b
}

// testWindow is a window of a tree whose panes have screens but no programs.
type testWindow struct {
	tree *model.Tree
	w    *model.Window
}

func newTestWindow(t testing.TB, sx, sy int) *testWindow {
	t.Helper()
	tree := model.NewTree()
	tree.Resized = func(p *model.Pane) {
		if p.Term != nil {
			p.Term.Resize(p.Sx, p.Sy)
		}
	}
	s, err := tree.NewSession("a", "/", time.Unix(0, 0))
	if err != nil {
		t.Fatal(err)
	}
	wl, err := tree.NewWindow(s, -1, 0, sx, sy)
	if err != nil {
		t.Fatal(err)
	}
	s.SelectWindow(wl)
	return &testWindow{tree: tree, w: wl.Window}
}

func (tw *testWindow) firstPane(data []byte) *model.Pane {
	p := tw.tree.NewPane(tw.w, nil, false, tw.w.Sx, tw.w.Sy)
	p.Term = vt.NewTerminal(p.Sx, p.Sy, 2000)
	_, _ = p.Term.Write(data)
	return p
}

func (tw *testWindow) split(t testing.TB, target *model.Pane, o model.SplitOptions, data []byte) *model.Pane {
	t.Helper()
	p, err := tw.tree.SplitPane(target, o)
	if err != nil {
		t.Fatal(err)
	}
	p.Term = vt.NewTerminal(p.Sx, p.Sy, 2000)
	_, _ = p.Term.Write(data)
	return p
}

func pane(t testing.TB, tree *model.Tree, id string) *model.Pane {
	t.Helper()
	n, err := strconv.Atoi(strings.TrimPrefix(id, "%"))
	if err != nil || tree.Pane(n) == nil {
		t.Fatalf("no pane %s", id)
	}
	return tree.Pane(n)
}

func TestFixtures(t *testing.T) {
	cases, err := filepath.Glob("../../testdata/render/*.txt")
	if err != nil || len(cases) == 0 {
		t.Fatalf("no cases: %v", err)
	}
	for _, path := range cases {
		c, err := renderfixture.ReadCase(path)
		if err != nil {
			t.Fatal(err)
		}
		want, err := os.ReadFile(strings.TrimSuffix(path, ".txt") + ".screen")
		if err != nil {
			t.Fatalf("%v (run make render-fixtures)", err)
		}
		for _, perStep := range []bool{false, true} {
			name := c.Name
			if perStep {
				name += "/per-step"
			}
			t.Run(name, func(t *testing.T) {
				got := drawCase(t, c, perStep)
				if got != string(want) {
					t.Errorf("client screen differs from tmux's:\n%s", sideBySide(string(want), got))
				}
			})
		}
	}
}

// drawCase makes a case's panes and draws them for its client, a frame after every step when
// perStep is set, and returns what the client shows.
func drawCase(t *testing.T, c *renderfixture.Case, perStep bool) string {
	t.Helper()
	wx, wy := c.WindowSize()
	tw := newTestWindow(t, wx, wy)
	cl := newTestClient(t, c.Term, c.Cols, c.Rows)
	r := New()
	view := &View{Window: tw.w, Sx: c.Cols, Sy: c.Rows, Styles: DefaultStyles(), Mouse: true, NoBright: cl.tty.Term.Has("Nobr")}
	frame := func() {
		r.Draw(cl.tty, r.Compose(view, time.Unix(0, 0)))
		cl.flush()
	}
	read := func(file string) []byte {
		b, err := os.ReadFile(filepath.Join(c.Dir, file))
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	for _, st := range c.Steps {
		switch st.Kind {
		case "pane":
			tw.firstPane(read(st.Args[0]))
		case "split":
			sp, err := renderfixture.ParseSplit(st.Args)
			if err != nil {
				t.Fatal(err)
			}
			target := tw.w.Active
			if sp.Target != "" {
				target = pane(t, tw.tree, sp.Target)
			}
			tw.split(t, target, model.SplitOptions{Horizontal: sp.Horizontal, Before: sp.Before, Size: sp.Size, Detached: true}, read(sp.File))
		case "select":
			tw.tree.SetActivePane(tw.w, pane(t, tw.tree, st.Args[0]))
		case "zoom":
			tw.tree.Zoom(tw.w.Active)
		}
		if perStep {
			frame()
		}
	}
	frame()
	return renderfixture.Dump(cl.screen)
}

// sideBySide shows two dumps line by line, marking the lines that differ.
func sideBySide(a, b string) string {
	la, lb := strings.Split(a, "\n"), strings.Split(b, "\n")
	var out strings.Builder
	for i := range max(len(la), len(lb)) {
		var x, y string
		if i < len(la) {
			x = la[i]
		}
		if i < len(lb) {
			y = lb[i]
		}
		mark := "  "
		if x != y {
			mark = "≠ "
		}
		out.WriteString(mark + "tmux:      " + strconv.Quote(x) + "\n")
		if x != y {
			out.WriteString(mark + "imux-core: " + strconv.Quote(y) + "\n")
		}
	}
	return out.String()
}
