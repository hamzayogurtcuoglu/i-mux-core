// Ported in part from tmux 3.8's popup.c, screen-write.c and tty-acs.c:
//
// Copyright (c) 2020 Nicholas Marriott <nicholas.marriott@gmail.com>
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
	"sync"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/status"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/vt"
)

// Box lines (tmux's BOX_LINES_*, the popup-border-lines and menu-border-lines choices).
const (
	BoxSingle = iota
	BoxDouble
	BoxHeavy
	BoxSimple
	BoxRounded
	BoxPadded
	BoxNone
)

// roundedBorders are box lines rounded's characters (tmux's tty_acs_rounded_borders).
var roundedBorders = [...]string{"", "│", "─", "╭", "╮", "╰", "╯", "┳", "┻", "┤", "├", "╋", "·"}

// boxBorder is screen_write_box_border_set: gc with the character of cell type ct for lines.
func boxBorder(gc vt.Cell, lines, ct int) vt.Cell {
	gc.Width = 1
	switch lines {
	case BoxNone:
	case BoxDouble:
		gc.Attr &^= vt.AttrCharset
		gc.Data = doubleBorders[ct]
	case BoxHeavy:
		gc.Attr &^= vt.AttrCharset
		gc.Data = heavyBorders[ct]
	case BoxRounded:
		gc.Attr &^= vt.AttrCharset
		gc.Data = roundedBorders[ct]
	case BoxSimple:
		gc.Attr &^= vt.AttrCharset
		gc.Data = simpleBorders[ct : ct+1]
	case BoxPadded:
		gc.Attr &^= vt.AttrCharset
		gc.Data = " "
	default:
		gc.Attr |= vt.AttrCharset
		gc.Data = cellBorders[ct : ct+1]
	}
	return gc
}

// Box is screen_write_box: a box of sx×sy cells in cells (rows of stride sx) with the border
// cell gc and, when not empty, title drawn as a format two cells in on its top line.
func Box(cells []vt.Cell, stride, sx, sy, lines int, gc vt.Cell, title string) {
	if sx < 2 || sy < 2 {
		return
	}
	gc.Attr |= vt.AttrCharset
	gc.Flags |= vt.FlagNoPalette
	put := func(x, y int, c vt.Cell) { cells[y*stride+x] = c }
	gc = boxBorder(gc, lines, cellRD)
	put(0, 0, gc)
	gc = boxBorder(gc, lines, cellLR)
	for x := 1; x < sx-1; x++ {
		put(x, 0, gc)
	}
	gc = boxBorder(gc, lines, cellLD)
	put(sx-1, 0, gc)

	gc = boxBorder(gc, lines, cellRU)
	put(0, sy-1, gc)
	gc = boxBorder(gc, lines, cellLR)
	for x := 1; x < sx-1; x++ {
		put(x, sy-1, gc)
	}
	gc = boxBorder(gc, lines, cellLU)
	put(sx-1, sy-1, gc)

	gc = boxBorder(gc, lines, cellUD)
	for y := 1; y < sy-1; y++ {
		put(0, y, gc)
		put(sx-1, y, gc)
	}
	if title != "" && sx > 4 {
		gc.Attr &^= vt.AttrCharset
		status.DrawFormat(cells[2:sx-2], gc, sx-4, title, nil, false)
	}
}

// Popup is a popup a client shows over everything else (tmux's popup_data).
type Popup struct {
	X, Y, Sx, Sy int
	Lines        int     // popup-border-lines (BoxSingle, …)
	Border       vt.Cell // the border's colours (popup-border-style, -S)
	Defaults     vt.Cell // the content's default colours (popup-style, -s)
	Title        string  // expanded -T, drawn as a format
	// Term is the popup's program's terminal, locked with Mu.
	Term *vt.Terminal
	Mu   *sync.Mutex
}

// inner is where the popup's screen goes: inside its border, if it has one.
func (pp *Popup) inner() (x, y int) {
	if pp.Lines == BoxNone {
		return pp.X, pp.Y
	}
	return pp.X + 1, pp.Y + 1
}

// popup is popup_draw_cb: the popup drawn over the frame.
func (r *Renderer) popup(f *Frame, v *View) {
	pp := v.Popup
	if pp == nil || pp.Sx <= 0 || pp.Sy <= 0 {
		return
	}
	cells := make([]vt.Cell, pp.Sx*pp.Sy)
	for i := range cells {
		cells[i] = vt.DefaultCell
	}
	box := pp.Lines != BoxNone
	if box && pp.Sx > 2 && pp.Sy > 2 {
		Box(cells, pp.Sx, pp.Sx, pp.Sy, pp.Lines, pp.Border, pp.Title)
	}

	pp.Mu.Lock()
	defer pp.Mu.Unlock()
	pal := &pp.Term.Palette
	defFg, defBg := pp.Defaults.Fg, pp.Defaults.Bg
	if defFg == 8 {
		defFg = pal.Fg
	}
	if defBg == 8 {
		defBg = pal.Bg
	}
	for i := range cells {
		resolve(&cells[i], defFg, defBg, pal, v)
	}
	s := pp.Term.Screen
	switch {
	case !box:
		for y := range pp.Sy {
			screenLine(cells[y*pp.Sx:(y+1)*pp.Sx], s, 0, y, defFg, defBg, pal, &r.links, v)
		}
	case pp.Sx > 2 && pp.Sy > 2:
		for y := 1; y < pp.Sy-1; y++ {
			screenLine(cells[y*pp.Sx+1:(y+1)*pp.Sx-1], s, 0, y-1, defFg, defBg, pal, &r.links, v)
		}
	}

	for y := range pp.Sy {
		fy := pp.Y + y
		if fy < 0 || fy >= f.Sy {
			continue
		}
		line := f.Line(fy)
		for x := range pp.Sx {
			if fx := pp.X + x; fx >= 0 && fx < f.Sx {
				line[fx] = cells[y*pp.Sx+x]
			}
		}
	}
}

// popupCursor is server_client_reset_state with an overlay: the mode is the popup's screen's,
// and the cursor its cursor.
func (r *Renderer) popupCursor(f *Frame, v *View) {
	pp := v.Popup
	pp.Mu.Lock()
	s := pp.Term.Screen
	mode := s.Mode
	ix, iy := pp.inner()
	f.Cx, f.Cy = ix+s.Cx, iy+s.Cy
	f.Cursor = cursorState(s)
	pp.Mu.Unlock()
	if v.NoCursor {
		mode &^= vt.ModeCursor
	}
	if v.Mouse && mode&vt.ModeMouseAll == 0 {
		mode |= vt.ModeMouseButton
	}
	f.Mode = mode
}
