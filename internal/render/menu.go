// Ported in part from tmux 3.8's menu.c and screen-write.c:
//
// Copyright (c) 2019 Nicholas Marriott <nicholas.marriott@gmail.com>
//
// Permission to use, copy, modify, and distribute this software for any
// purpose with or without fee is hereby granted, provided that the above
// copyright notice and this permission notice appear in all copies.
//
// THE SOFTWARE IS PROVIDED "AS IS" AND THE AUTHOR DISCLAIMS ALL WARRANTIES
// WITH REGARD TO THIS SOFTWARE INCLUDING ALL IMPLIED WARRANTIES OF
// MERCHANTABILITY AND FITNESS. IN NO EVENT SHALL THE AUTHOR BE LIABLE FOR
// ANY SPECIAL, DIRECT, INDIRECT, OR CONSEQUENTIAL DAMAGES OR ANY DAMAGES
// WHATSOEVER RESULTING FROM LOSS OF MIND, USE, DATA OR PROFITS, WHETHER
// IN AN ACTION OF CONTRACT, NEGLIGENCE OR OTHER TORTIOUS ACTION, ARISING
// OUT OF OR IN CONNECTION WITH THE USE OR PERFORMANCE OF THIS SOFTWARE.

package render

import (
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/status"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/vt"
)

// Menu is a window's menu as the renderer draws it: its screen (Sx×Sy cells, from MenuCells)
// at X, Y in the window, and the screen's mode (its mouse modes).
type Menu struct {
	X, Y, Sx, Sy int
	Cells        []vt.Cell
	Mode         int
	// Choice is the chosen item, or -1: the cursor goes to it (menu_get_cursor).
	Choice int
}

// hline is screen_write_hline: a separator line of nx cells, joined to the box at both ends.
func hline(cells []vt.Cell, nx, lines int, border vt.Cell) {
	gc := border
	gc.Attr |= vt.AttrCharset
	gc = boxBorder(gc, lines, cellURD)
	cells[0] = gc
	gc = boxBorder(gc, lines, cellLR)
	for i := 1; i < nx-1; i++ {
		cells[i] = gc
	}
	gc = boxBorder(gc, lines, cellULD)
	cells[nx-1] = gc
}

// MenuCells is menu_update: a menu's screen, width+4 by len(items)+2 cells. An item is its
// name (a format; a leading - makes it disabled), or nil for a separator; choice is the chosen
// item or -1.
func MenuCells(width int, title string, items []*string, choice, lines int, menuGC, border, selected vt.Cell) []vt.Cell {
	sx, sy := width+4, len(items)+2
	cells := make([]vt.Cell, sx*sy)
	for i := range cells {
		cells[i] = vt.DefaultCell
	}
	if lines != BoxNone {
		Box(cells, sx, sx, sy, lines, border, title)
	}

	// screen_write_menu.
	def := menuGC
	Box(cells, sx, sx, sy, lines, border, title)
	gc := def
	for i, name := range items {
		row := cells[(1+i)*sx : (2+i)*sx]
		if name == nil {
			hline(row, sx, lines, border)
			continue
		}
		if choice >= 0 && i == choice && (*name == "" || (*name)[0] != '-') {
			gc = selected
		}
		blank := gc
		blank.Data, blank.Width = " ", 1
		for x := 1; x < width+3; x++ {
			row[x] = blank
		}
		if *name != "" && (*name)[0] == '-' {
			// A disabled item is never the chosen one's colour: it is drawn dim.
			dim := def
			dim.Attr |= vt.AttrDim
			status.DrawFormat(row[2:2+width], dim, width, (*name)[1:], nil, false)
			continue
		}
		status.DrawFormat(row[2:2+width], gc, width, *name, nil, false)
		gc = def
	}
	return cells
}

// menu draws the window's menu over the scene (redraw_mark_menu and redraw_draw_menu_span):
// its cells in window coordinates, as tty_draw_line shows them, without a pane's colours.
func (r *Renderer) menu(f *Frame, v *View, sc *scene, areaY int) {
	m := v.Menu
	if m == nil {
		return
	}
	for my := range m.Sy {
		for mx := range m.Sx {
			x, y, ok := sc.windowToScene(m.X+mx, m.Y+my)
			if !ok || x >= f.Sx || areaY+y >= f.Sy {
				continue
			}
			gc := m.Cells[my*m.Sx+mx]
			gc.Fg, gc.Bg, gc.Us = v.theme(gc.Fg), v.theme(gc.Bg), v.theme(gc.Us)
			gc.Flags &^= vt.FlagNoPalette
			f.Line(areaY + y)[x] = gc
		}
	}
}

// menuCursor is server_client_reset_state with a window menu: the menu's screen's mode, the
// cursor at the chosen item (hidden: the menu's screen has no cursor), the mouse modes not the
// panes', and no bracketed paste.
func (r *Renderer) menuCursor(f *Frame, v *View, sc *scene, areaY int) {
	m := v.Menu
	cx, cy := m.X+2, m.Y
	if m.Choice != -1 {
		cy = m.Y + 1 + m.Choice
	}
	mode := m.Mode
	if cx < sc.ox || cx >= sc.ox+sc.sx || cy < sc.oy || cy >= sc.oy+sc.sy {
		mode &^= vt.ModeCursor
	} else {
		f.Cx, f.Cy = cx-sc.ox, cy-sc.oy+areaY
	}
	if v.NoCursor {
		mode &^= vt.ModeCursor
	}
	if v.Mouse && mode&vt.ModeMouseAll == 0 {
		mode |= vt.ModeMouseButton
	}
	mode &^= vt.ModeBracketPaste
	f.Mode = mode
}
