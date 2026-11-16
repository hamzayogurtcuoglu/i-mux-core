package render

import (
	"testing"
	"time"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/layout"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/model"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/vt"
)

// borderWorld is a 20x6 window of two panes side by side, the left one (9 cells) active.
func borderWorld(t *testing.T) (*testWindow, *model.Pane, *model.Pane, *View) {
	t.Helper()
	tw := newTestWindow(t, 20, 6)
	left := tw.firstPane(nil)
	right := tw.split(t, left, model.SplitOptions{Horizontal: true, Size: 10}, nil)
	tw.w.SetActivePane(left)
	v := &View{Window: tw.w, Sx: 20, Sy: 6, Styles: DefaultStyles()}
	return tw, left, right, v
}

// row is line y of a frame as text: line drawing characters as their VT100 letters.
func row(f *Frame, y int) string {
	var b []byte
	for _, c := range f.Line(y) {
		b = append(b, c.Data...)
	}
	return string(b)
}

// The pane-border-status line: pane-border-format drawn over the border from two cells into
// the pane, as wide as the pane, in the pane's border style.
func TestPaneStatusLine(t *testing.T) {
	tw, left, right, v := borderWorld(t)
	tw.tree.SetBorderStatus(tw.w, layout.StatusTop)
	red, blue := DefaultStyles().Border, DefaultStyles().Border
	red.Fg, blue.Fg = 1, 4
	v.Styles.Panes = map[*model.Pane]PaneBorder{
		left:  {Style: red, Status: "L#[fg=green]x"},
		right: {Style: blue, Index: 1, Status: "#[align=right]R"},
	}
	f := New().Compose(v, time.Time{})
	if got, want := row(f, 0), "qqLxqqqqqwqqqqqqqqqR"; got != want {
		t.Fatalf("row 0 %q, want %q", got, want)
	}
	line := f.Line(0)
	for x, want := range map[int]vt.Color{0: 1, 2: 1, 3: 2, 4: 1, 12: 4, 19: 4} {
		if line[x].Fg != want {
			t.Errorf("x %d: fg %#x, want %#x", x, line[x].Fg, want)
		}
	}
	if line[2].Attr&vt.AttrCharset != 0 || line[4].Attr&vt.AttrCharset == 0 {
		t.Errorf("charset: text %#x, line %#x", line[2].Attr, line[4].Attr)
	}
}

// pane-border-lines: each choice's characters for the border between two panes.
func TestBorderLines(t *testing.T) {
	tests := []struct {
		lines   int
		want    string
		charset bool
	}{
		{LinesSingle, "x", true},
		{LinesDouble, "║", false},
		{LinesHeavy, "┃", false},
		{LinesSimple, "|", false},
		{LinesNumber, "0", false},
		{LinesSpaces, " ", false},
		{LinesNone, " ", false},
	}
	for _, tt := range tests {
		_, _, _, v := borderWorld(t)
		v.Styles.Lines = tt.lines
		gc := New().Compose(v, time.Time{}).Line(3)[9]
		if gc.Data != tt.want || (gc.Attr&vt.AttrCharset != 0) != tt.charset {
			t.Errorf("lines %d: %q charset %v, want %q %v", tt.lines, gc.Data, gc.Attr&vt.AttrCharset != 0, tt.want, tt.charset)
		}
	}
	if gc := borderChar(vt.DefaultCell, LinesNumber, cellLR, -1); gc.Data != "*" {
		t.Errorf("number with no pane: %q", gc.Data)
	}
	if gc := borderChar(vt.DefaultCell, LinesNumber, cellNone, 3); gc.Data != "~" || gc.Attr&vt.AttrCharset == 0 {
		t.Errorf("number, no cell: %q", gc.Data)
	}
}

// pane-border-indicators arrows: an arrow on the border one cell in from the pane's corner,
// pointing at the active pane.
func TestBorderArrows(t *testing.T) {
	_, _, right, v := borderWorld(t)
	v.Styles.Arrows = true
	f := New().Compose(v, time.Time{})
	if gc := f.Line(1)[9]; gc.Data != "," || gc.Attr&vt.AttrCharset == 0 {
		t.Errorf("arrow towards the left pane: %q", gc.Data)
	}
	if gc := f.Line(2)[9]; gc.Data != "x" {
		t.Errorf("no arrow: %q", gc.Data)
	}
	v.Window.SetActivePane(right)
	if gc := New().Compose(v, time.Time{}).Line(1)[9]; gc.Data != "+" {
		t.Errorf("arrow towards the right pane: %q", gc.Data)
	}
}

// A message over a window with no status line takes its last line; the cursor is hidden, and
// a frozen client keeps the panes as they were.
func TestMessage(t *testing.T) {
	tw := newTestWindow(t, 10, 3)
	p := tw.firstPane([]byte("ab"))
	v := &View{Window: tw.w, Sx: 10, Sy: 3, Styles: DefaultStyles()}
	r := New()
	cl := newTestClient(t, "xterm-256color", 10, 3)
	r.Draw(cl.tty, r.Compose(v, time.Time{}))

	msg := make([]vt.Cell, 10)
	for i := range msg {
		msg[i] = vt.DefaultCell
		msg[i].Data = "m"
	}
	v.Message, v.Freeze, v.NoCursor = [][]vt.Cell{msg}, true, true
	_, _ = p.Term.Write([]byte("cd"))
	f := r.Compose(v, time.Time{})
	r.Draw(cl.tty, f)
	if got := row(f, 2); got != "mmmmmmmmmm" {
		t.Errorf("message line %q", got)
	}
	if got := row(f, 0); got != "ab        " {
		t.Errorf("frozen pane %q", got)
	}
	if f.Mode&vt.ModeCursor != 0 {
		t.Errorf("cursor shown")
	}

	v.Message, v.Freeze, v.NoCursor = nil, false, false
	if got := row(r.Compose(v, time.Time{}), 0); got != "abcd      " {
		t.Errorf("after the message %q", got)
	}
}
