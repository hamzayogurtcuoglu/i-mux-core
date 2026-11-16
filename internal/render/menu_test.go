package render

import (
	"testing"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/vt"
)

// MenuCells: the box, a separator joined to it, the chosen item in the selected style, a
// disabled item dim, and the key on the right.
func TestMenuCells(t *testing.T) {
	one, off, two := "one#[default] #[align=right](1)", "-off", "two"
	menu, sel := vt.DefaultCell, vt.DefaultCell
	menu.Bg, sel.Bg = 4, 1
	cells := MenuCells(7, "T", []*string{&one, nil, &off, &two}, 0, BoxSingle, menu, vt.DefaultCell, sel)
	sx := 11
	row := func(y int) string {
		s := ""
		for _, c := range cells[y*sx : (y+1)*sx] {
			s += c.Data
		}
		return s
	}
	want := []string{"lqTqqqqqqqk", "x one (1) x", "tqqqqqqqqqu", "x off     x", "x two     x", "mqqqqqqqqqj"}
	for y, w := range want {
		if got := row(y); got != w {
			t.Errorf("row %d %q, want %q", y, got, w)
		}
	}
	if c := cells[1*sx+2]; c.Bg != 1 {
		t.Errorf("chosen item bg %#x", c.Bg)
	}
	if c := cells[4*sx+2]; c.Bg != 4 {
		t.Errorf("item bg %#x", c.Bg)
	}
	if c := cells[3*sx+2]; c.Attr&vt.AttrDim == 0 || c.Bg != 4 {
		t.Errorf("disabled item %+v", c)
	}
}
