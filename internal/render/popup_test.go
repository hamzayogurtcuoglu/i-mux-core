package render

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/vt"
)

// Box draws each border kind's corners and sides, and the title over the top line.
func TestBox(t *testing.T) {
	tests := []struct {
		lines            int
		corner, side     string
		charset, noTitle bool
	}{
		{BoxSingle, "l", "x", true, false},
		{BoxDouble, "╔", "║", false, false},
		{BoxHeavy, "┏", "┃", false, false},
		{BoxRounded, "╭", "│", false, false},
		{BoxSimple, "+", "|", false, false},
		{BoxPadded, " ", " ", false, false},
	}
	for _, tt := range tests {
		cells := make([]vt.Cell, 8*3)
		for i := range cells {
			cells[i] = vt.DefaultCell
		}
		Box(cells, 8, 8, 3, tt.lines, vt.DefaultCell, "#[fg=red]ab")
		if cells[0].Data != tt.corner || cells[8].Data != tt.side || (cells[0].Attr&vt.AttrCharset != 0) != tt.charset {
			t.Errorf("lines %d: corner %q side %q", tt.lines, cells[0].Data, cells[8].Data)
		}
		if cells[0].Flags&vt.FlagNoPalette == 0 {
			t.Errorf("lines %d: the border takes the palette", tt.lines)
		}
		if cells[2].Data != "a" || cells[3].Data != "b" || cells[2].Fg != vt.ColorFromString("red") || cells[2].Attr&vt.AttrCharset != 0 {
			t.Errorf("lines %d: title %+v %+v", tt.lines, cells[2], cells[3])
		}
	}
}

// A popup is drawn over the frame, its screen inside its border in its default colours, and
// the cursor and modes are its screen's.
func TestPopupDrawn(t *testing.T) {
	tw := newTestWindow(t, 20, 6)
	tw.firstPane([]byte("pane"))
	term := vt.NewTerminal(6, 2, 0)
	_, _ = term.Write([]byte("hi\x1b[?1000h"))
	bg := vt.DefaultCell
	bg.Bg = 4
	v := &View{
		Window: tw.w, Sx: 20, Sy: 6, Styles: DefaultStyles(), Mouse: true,
		Popup: &Popup{X: 2, Y: 1, Sx: 8, Sy: 4, Lines: BoxSingle, Border: vt.DefaultCell, Defaults: bg, Term: term, Mu: &sync.Mutex{}},
	}
	f := New().Compose(v, time.Time{})
	if got := row(f, 0); !strings.HasPrefix(got, "pane") {
		t.Errorf("row 0 %q", got)
	}
	if got := row(f, 1); got[2:10] != "lqqqqqqk" {
		t.Errorf("row 1 %q", got)
	}
	if got := row(f, 2); got[2:10] != "xhi    x" {
		t.Errorf("row 2 %q", got)
	}
	if c := f.Line(2)[5]; c.Bg != 4 {
		t.Errorf("content bg %#x", c.Bg)
	}
	if f.Cx != 5 || f.Cy != 2 {
		t.Errorf("cursor %d,%d", f.Cx, f.Cy)
	}
	if f.Mode&vt.ModeMouseStandard == 0 {
		t.Errorf("mode %#x: no mouse", f.Mode)
	}
}
