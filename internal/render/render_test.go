package render

import (
	"strings"
	"testing"
	"time"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/model"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/render/renderfixture"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/vt"
)

// world is a window of two panes side by side, drawn for an xterm-256color client.
type world struct {
	tw    *testWindow
	cl    *testClient
	r     *Renderer
	view  *View
	left  *model.Pane
	right *model.Pane
	now   time.Time
}

func newWorld(t testing.TB) *world {
	t.Helper()
	tw := newTestWindow(t, 80, 24)
	left := tw.firstPane([]byte("left"))
	right := tw.split(t, left, model.SplitOptions{Horizontal: true, Size: -1}, []byte("right"))
	cl := newTestClient(t, "xterm-256color", 80, 24)
	w := &world{tw: tw, cl: cl, r: New(), left: left, right: right, now: time.Unix(1000, 0)}
	w.view = &View{Window: tw.w, Sx: 80, Sy: 24, Styles: DefaultStyles(), Mouse: true}
	w.draw()
	return w
}

// draw draws a frame and returns what was written.
func (w *world) draw() string {
	w.r.Draw(w.cl.tty, w.r.Compose(w.view, w.now))
	return string(w.cl.flush())
}

func (w *world) dump() string { return renderfixture.Dump(w.cl.screen) }

// A frame with nothing changed writes nothing; one changed cell writes a few bytes, and the
// client then shows exactly what a whole redraw shows.
func TestOneChangedCell(t *testing.T) {
	w := newWorld(t)
	if got := w.draw(); got != "" {
		t.Errorf("nothing changed, wrote %q", got)
	}
	_, _ = w.right.Term.Write([]byte("!"))
	got := w.draw()
	if len(got) > 48 || !strings.Contains(got, "!") {
		t.Errorf("one cell: wrote %d bytes: %q", len(got), got)
	}
	whole := newTestClient(t, "xterm-256color", 80, 24)
	r := New()
	r.Draw(whole.tty, r.Compose(w.view, w.now))
	whole.flush()
	if a, b := renderfixture.Dump(whole.screen), w.dump(); a != b {
		t.Errorf("after the change:\n%s", sideBySide(a, b))
	}
}

// A pane in a synchronized update keeps showing its last frame, until the update ends or
// SyncTimeout passes; the cursor stays where it was meanwhile.
func TestSyncHold(t *testing.T) {
	for _, end := range []string{"mode", "timeout"} {
		t.Run(end, func(t *testing.T) {
			w := newWorld(t)
			w.tw.tree.SetActivePane(w.tw.w, w.right)
			w.draw()
			before := w.dump()
			_, _ = w.right.Term.Write([]byte("\033[?2026h\033[2J\033[Hhalf"))
			if got := w.draw(); strings.Contains(got, "half") {
				t.Fatalf("drawn during the update: %q", got)
			}
			if d, ok := w.r.Deadline(); !ok || !d.Equal(w.now.Add(SyncTimeout)) {
				t.Errorf("deadline %v %v", d, ok)
			}
			if w.dump() != before {
				t.Errorf("the client's screen changed during the update")
			}
			// The other pane is still drawn.
			_, _ = w.left.Term.Write([]byte(" more"))
			if got := w.draw(); !strings.Contains(got, "more") {
				t.Errorf("the other pane was not drawn: %q", got)
			}
			if end == "mode" {
				_, _ = w.right.Term.Write([]byte(" done\033[?2026l"))
			} else {
				w.now = w.now.Add(SyncTimeout)
			}
			got := w.draw()
			if !strings.Contains(got, "half") {
				t.Errorf("not drawn after the update: %q", got)
			}
			if _, ok := w.r.Deadline(); ok != (end == "timeout") {
				t.Errorf("deadline still set: %v", ok)
			}
		})
	}
}

// Runs of changes are widened to whole wide characters, and far apart changes on one line
// are separate runs.
func TestDiffRuns(t *testing.T) {
	f := func(s string) *Frame {
		fr := &Frame{Sx: 20, Sy: 1, Cells: make([]vt.Cell, 20)}
		x := 0
		for _, r := range s {
			c := vt.DefaultCell
			c.Data = string(r)
			if r == '日' {
				c.Width = 2
				fr.Cells[x] = c
				pad := c
				pad.Data, pad.Width, pad.Flags = "", 0, vt.FlagPadding
				fr.Cells[x+1] = pad
				x += 2
				continue
			}
			fr.Cells[x] = c
			x++
		}
		for ; x < 20; x++ {
			fr.Cells[x] = vt.DefaultCell
		}
		return fr
	}
	a := f("ab日cdefghijklmnopq")
	b := f("ab日cdefghijklmnopq")
	b.Cells[3].Bg = 1 // the padding of the wide character
	b.Cells[16].Data = "Z"
	got := diff(a, b)
	want := []run{{0, 2, 3}, {0, 16, 16}}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("runs %v, want %v", got, want)
	}
	if got := diff(nil, b); len(got) != 1 || got[0] != (run{0, 0, 19}) {
		t.Errorf("whole frame: %v", got)
	}
}

// A window with no pane yet, and a client too small for anything but its status lines.
func TestEmpty(t *testing.T) {
	r := New()
	f := r.Compose(&View{Sx: 4, Sy: 2, StatusLines: 3}, time.Time{})
	for _, c := range f.Cells {
		if c != vt.DefaultCell {
			t.Fatalf("cell %+v", c)
		}
	}
}

// One changed cell in a large window writes a small, bounded amount.
func BenchmarkOneChangedCell(b *testing.B) {
	w := newWorld(b)
	b.ReportAllocs()
	n := 0
	for i := range b.N {
		_, _ = w.right.Term.Write([]byte{"abc"[i%3]})
		n += len(w.draw())
	}
	b.ReportMetric(float64(n)/float64(b.N), "bytes/frame")
}

// A full frame of a busy screen.
func BenchmarkWholeFrame(b *testing.B) {
	w := newWorld(b)
	line := strings.Repeat("\033[31mred\033[32mgreen\033[0m text ", 3)
	for range 24 {
		_, _ = w.left.Term.Write([]byte(line + "\r\n"))
		_, _ = w.right.Term.Write([]byte(line + "\r\n"))
	}
	b.ReportAllocs()
	for range b.N {
		w.r.Invalidate()
		w.draw()
	}
}
