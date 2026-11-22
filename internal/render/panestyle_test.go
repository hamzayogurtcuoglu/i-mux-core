package render

import (
	"testing"
	"time"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/model"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/vt"
)

// tty_default_colours: a pane's cells in the default colours take window-style's colours,
// the active pane window-active-style's, a program's own OSC 11 wins over both, and
// pane-colours replaces the 16 colours.
func TestWindowStyleColours(t *testing.T) {
	tw := newTestWindow(t, 20, 4)
	left := tw.firstPane([]byte("ab\x1b[31mr"))
	right := tw.split(t, left, model.SplitOptions{Horizontal: true, Size: 10}, []byte("cd"))
	tw.w.SetActivePane(left)
	for _, p := range []*model.Pane{left, right} {
		pal := &p.Term.Palette
		pal.WindowFg, pal.WindowBg = 189, 17
		pal.ActiveBg = 18
		pal.SetDefault(1, 160)
	}
	v := &View{Window: tw.w, Sx: 20, Sy: 4, Styles: DefaultStyles()}
	line := New().Compose(v, time.Time{}).Line(0)
	if c := line[0]; c.Fg != 189 || c.Bg != 18 {
		t.Errorf("active pane: fg %d bg %d, want 189 18", c.Fg, c.Bg)
	}
	if c := line[2]; c.Fg != 160 {
		t.Errorf("colour 1: fg %d, want pane-colours' 160", c.Fg)
	}
	if c := line[5]; c.Bg != 18 {
		t.Errorf("past the line's end: bg %d, want 18", c.Bg)
	}
	if c := line[10]; c.Fg != 189 || c.Bg != 17 {
		t.Errorf("other pane: fg %d bg %d, want 189 17", c.Fg, c.Bg)
	}

	_, _ = right.Term.Write([]byte("\x1b]11;rgb:ff/00/00\x1b\\"))
	if c := New().Compose(v, time.Time{}).Line(0)[10]; c.Bg != vt.ColorJoinRGB(0xff, 0, 0) {
		t.Errorf("OSC 11 set by the program: bg %#x", c.Bg)
	}
}

// Without the styles the default colours stay the terminal's.
func TestNoWindowStyle(t *testing.T) {
	tw := newTestWindow(t, 10, 2)
	tw.firstPane([]byte("ab"))
	v := &View{Window: tw.w, Sx: 10, Sy: 2, Styles: DefaultStyles()}
	if c := New().Compose(v, time.Time{}).Line(0)[0]; c.Fg != 8 || c.Bg != 8 {
		t.Errorf("fg %d bg %d, want the defaults", c.Fg, c.Bg)
	}
}
