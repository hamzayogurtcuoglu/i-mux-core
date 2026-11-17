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

// A popup or status line that changes is written whole, as tmux draws them (popup_draw_cb,
// screen_redraw_draw_status), so the terminal gets their text again and not only the changed
// cells; a pane's change stays a few bytes (TestOneChangedCell).
func TestWholeParts(t *testing.T) {
	w := newWorld(t)
	term := vt.NewTerminal(20, 3, 0)
	_, _ = term.Write([]byte("7 found\r\nrow two"))
	status := func(s string) [][]vt.Cell {
		line := make([]vt.Cell, 80)
		for i := range line {
			line[i] = vt.DefaultCell
		}
		for i, r := range s {
			line[i].Data = string(r)
		}
		return [][]vt.Cell{line}
	}
	w.view.Sy, w.view.StatusLines, w.view.Status = 24, 1, status("Codex  Claude")
	w.view.Popup = &Popup{X: 10, Y: 5, Sx: 22, Sy: 5, Lines: BoxSingle, Border: vt.DefaultCell, Defaults: vt.DefaultCell, Term: term, Mu: &sync.Mutex{}}
	w.draw()

	_, _ = term.Write([]byte("\x1b[H1"))
	if got := plainText(w.draw()); !strings.Contains(got, "1 found") || !strings.Contains(got, "row two") {
		t.Errorf("popup: wrote %q", got)
	}
	w.view.Status = status("Codex !15:00  Claude")
	if got := plainText(w.draw()); !strings.Contains(got, "Codex !15:00  Claude") || strings.Contains(got, "row two") {
		t.Errorf("status: wrote %q", got)
	}
	if got := w.draw(); got != "" {
		t.Errorf("nothing changed, wrote %q", got)
	}
}

// plainText is what was written without escape sequences.
func plainText(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != 0x1b {
			b.WriteByte(s[i])
			continue
		}
		// CSI: up to a final byte; others: the next byte.
		if i+1 < len(s) && s[i+1] == '[' {
			i += 2
			for i < len(s) && (s[i] < 0x40 || s[i] > 0x7e) {
				i++
			}
			continue
		}
		i++
	}
	return b.String()
}
