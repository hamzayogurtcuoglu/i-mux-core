// Ported in part from tmux 3.8's screen-redraw.c, window-border.c and tty.c:
//
// Copyright (c) 2007 Nicholas Marriott <nicholas.marriott@gmail.com>
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
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/layout"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/model"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/termout"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/vt"
)

// Styles are the options that decide how borders look, as cells: only their colours and
// attributes are used.
type Styles struct {
	Border       vt.Cell // pane-border-style
	ActiveBorder vt.Cell // pane-active-border-style
	// TwoPaneColours is pane-border-indicators colour or both (tmux's default colour): with two
	// panes, the active pane colours half of the border between them.
	TwoPaneColours bool
	// InsideFill and OutsideFill are fill-character's cells, for the parts of the window no pane
	// covers and for the client's area outside the window: their character, attributes, and
	// any colours that are not the default, over the border style.
	InsideFill, OutsideFill vt.Cell
}

// Theme colours (tmux's COLOUR_THEME_*), used as vt.ColorFlagTheme|n.
const (
	ThemeBlack = iota
	ThemeWhite
	ThemeLightGrey
	ThemeDarkGrey
	ThemeGreen
	ThemeYellow
	ThemeRed
	ThemeBlue
	ThemeCyan
	ThemeMagenta
	ThemeCount
)

// TerminalThemeColours are the theme colours as the terminal's ANSI colours (tmux's
// colour_theme_terminal_colour). They are also what tmux gives a client before its terminal is
// known: the dark-theme-* options then choose their colours for fewer than 256 colours.
var TerminalThemeColours = []vt.Color{0, 7, 7, 0, 2, 3, 1, 4, 6, 5}

// DefaultStyles are tmux 3.8's defaults: pane-border-style fg=themelightgrey and
// pane-active-border-style fg=themegreen (for a pane not in a mode, marked or synchronized).
func DefaultStyles() Styles {
	border, active := vt.DefaultCell, vt.DefaultCell
	border.Fg = vt.ColorFlagTheme | ThemeLightGrey
	active.Fg = vt.ColorFlagTheme | ThemeGreen
	// fill-character's default: "#{?is_inside,#[bg=themedarkgrey] ,#[fg=themelightgrey]#[acs]~}".
	inside, outside := vt.DefaultCell, vt.DefaultCell
	inside.Bg = vt.ColorFlagTheme | ThemeDarkGrey
	outside.Fg = vt.ColorFlagTheme | ThemeLightGrey
	outside.Attr = vt.AttrCharset
	outside.Data = "~"
	return Styles{Border: border, ActiveBorder: active, TwoPaneColours: true, InsideFill: inside, OutsideFill: outside}
}

// View is what one client shows: a window, at the client's size, with its status lines.
type View struct {
	Window *model.Window
	Sx, Sy int // the client terminal's size

	StatusLines int  // how many lines the status takes (status, status-position)
	StatusTop   bool // the status is at the top
	// Status holds the status lines' cells, StatusLines lines of Sx cells; nil leaves them
	// blank (the status line itself comes with TASKS.md C4.2).
	Status [][]vt.Cell

	Styles Styles
	Mouse  bool // the session's mouse option (on by default in tmux 3.8)
	// NoBright is the terminal's Nobr: bold does not make the ANSI colours bright.
	NoBright bool
	// ThemeColours are the client's theme colours (tmux's theme_colours), by theme colour;
	// nil is TerminalThemeColours.
	ThemeColours []vt.Color
}

// theme maps a theme colour to the client's colour (tmux's tty_map_theme_colour).
func (v *View) theme(c vt.Color) vt.Color {
	if c&vt.ColorFlagTheme == 0 {
		return c
	}
	n := int(c & 0xff)
	tc := v.ThemeColours
	if tc == nil {
		tc = TerminalThemeColours
	}
	if n >= len(tc) {
		return 8
	}
	if m := tc[n]; m != -1 && m&vt.ColorFlagTheme == 0 {
		return m
	}
	return 8
}

// Border cell types, tmux's CELL_*, and their line drawing characters (CELL_BORDERS).
const (
	cellUD   = 1
	cellLR   = 2
	cellRD   = 3
	cellLD   = 4
	cellRU   = 5
	cellLU   = 6
	cellLRD  = 7
	cellLRU  = 8
	cellURD  = 9
	cellULD  = 10
	cellLRUD = 11
	cellNone = 12
)

const cellBorders = " xqlkmjwvtun~"

// Border connections (tmux's REDRAW_BORDER_*).
const (
	borderL = 0x1
	borderR = 0x2
	borderU = 0x4
	borderD = 0x8
)

// cellType is redraw_get_cell_type: a border cell's character from its connections.
func cellType(mask int) int {
	switch mask {
	case borderL | borderR | borderU | borderD:
		return cellLRUD
	case borderL | borderR | borderU:
		return cellLRU
	case borderL | borderR | borderD:
		return cellLRD
	case borderL | borderR, borderL, borderR:
		return cellLR
	case borderL | borderU | borderD:
		return cellULD
	case borderL | borderU:
		return cellLU
	case borderL | borderD:
		return cellLD
	case borderR | borderU | borderD:
		return cellURD
	case borderR | borderU:
		return cellRU
	case borderR | borderD:
		return cellRD
	case borderU | borderD, borderU, borderD:
		return cellUD
	}
	return cellNone
}

type spanType uint8

// What a cell of the window area is (tmux's redraw_span_type).
const (
	spanEmpty   spanType = iota // inside the window, nothing there
	spanOutside                 // outside the window
	spanPane
	spanBorder
)

// buildCell is one cell of the window area while it is built (tmux's redraw_build_cell).
type buildCell struct {
	typ    spanType
	pane   *model.Pane // spanPane
	px, py int

	top, bottom, left, right *model.Pane // spanBorder: the panes on each side
	style                    *model.Pane // the pane chosen to colour it (two panes)
	mask                     int
}

// scene is the window area of a client (tmux's redraw_build_ctx).
type scene struct {
	w              *model.Window
	ox, oy, sx, sy int
	cells          []buildCell
}

func (sc *scene) at(x, y int) *buildCell { return &sc.cells[y*sc.sx+x] }

// windowToScene is redraw_window_to_scene.
func (sc *scene) windowToScene(wx, wy int) (x, y int, ok bool) {
	if wx < 0 || wy < 0 || wx > sc.w.Sx || wy > sc.w.Sy || wx < sc.ox || wy < sc.oy {
		return 0, 0, false
	}
	x, y = wx-sc.ox, wy-sc.oy
	if x >= sc.sx || y >= sc.sy {
		return 0, 0, false
	}
	return x, y, true
}

// paneVisible is window_pane_is_visible: with a zoomed window, only its zoomed pane.
func paneVisible(p *model.Pane) bool {
	w := p.Window
	if w.Layout == nil {
		return true
	}
	if z := w.Layout.Zoomed(); z >= 0 {
		return p.ID == z
	}
	return true
}

func (sc *scene) markInside(p *model.Pane) {
	for py := range p.Sy {
		for px := range p.Sx {
			x, y, ok := sc.windowToScene(p.Xoff+px, p.Yoff+py)
			if !ok {
				continue
			}
			*sc.at(x, y) = buildCell{typ: spanPane, pane: p, px: px, py: py}
		}
	}
}

// markBorderCell is redraw_mark_border_cell for a tiled pane.
func (sc *scene) markBorderCell(wx, wy int, p *model.Pane, topOwner, bottomOwner bool, mask int) {
	x, y, ok := sc.windowToScene(wx, wy)
	if !ok {
		return
	}
	bc := sc.at(x, y)
	switch bc.typ {
	case spanEmpty, spanOutside:
		*bc = buildCell{typ: spanBorder}
	case spanBorder:
	default:
		return
	}
	if topOwner {
		bc.top = p
	}
	if bottomOwner {
		bc.bottom = p
	}
	if mask&(borderU|borderD) != 0 {
		if wx < p.Xoff {
			bc.right = p
		} else if wx >= p.Xoff+p.Sx {
			bc.left = p
		}
	}
	bc.mask |= mask
}

// markBorders is redraw_mark_pane_borders for a tiled pane, without scrollbars.
func (sc *scene) markBorders(p *model.Pane) {
	status := layout.StatusOff
	if p.Window.Layout != nil {
		status = p.Window.Layout.Status
	}
	left, right := p.Xoff-1, p.Xoff+p.Sx
	top, bottom := p.Yoff-1, p.Yoff+p.Sy
	markLeft, markTop := left >= 0, top >= 0
	markRight, markBottom := right <= sc.w.Sx, bottom <= sc.w.Sy
	if status == layout.StatusTop && bottom < sc.w.Sy {
		markBottom = false
	} else if status == layout.StatusBottom {
		markTop = false
	}
	horizontal := func(wy int, topOwner, bottomOwner bool) {
		for wx := left; wx <= right; wx++ {
			mask := 0
			if wx > left {
				mask |= borderL
			}
			if wx < right {
				mask |= borderR
			}
			sc.markBorderCell(wx, wy, p, topOwner, bottomOwner, mask)
		}
	}
	vertical := func(wx int) {
		for wy := top; wy <= bottom; wy++ {
			mask := 0
			if wy > top {
				mask |= borderU
			}
			if wy < bottom {
				mask |= borderD
			}
			sc.markBorderCell(wx, wy, p, false, false, mask)
		}
	}
	if markTop {
		horizontal(top, false, true)
	}
	if markBottom {
		horizontal(bottom, true, false)
	}
	if markLeft {
		vertical(left)
	}
	if markRight {
		vertical(right)
	}
}

// markTwoPaneColours is redraw_mark_two_pane_colours: with two panes side by side, the border
// takes the left pane's style in its top half and the right's in its bottom half; stacked, the
// top pane's in its left half and the bottom pane's in its right.
func (sc *scene) markTwoPaneColours() {
	w := sc.w
	if w.Layout == nil || w.Layout.Zoomed() >= 0 || len(w.Panes) != 2 {
		return
	}
	var typ layout.Type
	for _, p := range w.Panes {
		c := w.Layout.Leaf(p.ID)
		if c == nil || c.Parent == nil {
			return
		}
		typ = c.Parent.Type
	}
	for y := range sc.sy {
		for x := range sc.sx {
			bc := sc.at(x, y)
			if bc.typ != spanBorder {
				continue
			}
			wx, wy := sc.ox+x, sc.oy+y
			switch {
			case typ == layout.LeftRight && bc.left != nil && bc.right != nil:
				if wy <= w.Sy/2 {
					bc.style = bc.left
				} else {
					bc.style = bc.right
				}
			case typ == layout.TopBottom && bc.top != nil && bc.bottom != nil:
				if wx <= w.Sx/2 {
					bc.style = bc.top
				} else {
					bc.style = bc.bottom
				}
			}
		}
	}
}

// windowOffset is tty_window_offset1: where the window area starts and its size. A window
// larger than the client is shown around the active pane's cursor.
func windowOffset(w *model.Window, sx, sy int) (ox, oy, wsx, wsy int) {
	if sx >= w.Sx && sy >= w.Sy {
		return 0, 0, w.Sx, w.Sy
	}
	wsx, wsy = sx, sy
	p := w.Active
	if p == nil || p.Term == nil {
		return 0, 0, wsx, wsy
	}
	p.Mu.Lock()
	s := p.Term.Screen
	cursor := s.Mode&vt.ModeCursor != 0
	cx, cy := p.Xoff+s.Cx, p.Yoff+s.Cy
	p.Mu.Unlock()
	if !cursor {
		return 0, 0, wsx, wsy
	}
	switch {
	case cx < wsx:
		ox = 0
	case cx > w.Sx-wsx:
		ox = w.Sx - wsx
	default:
		ox = cx - wsx/2
	}
	switch {
	case cy < wsy:
		oy = 0
	case cy > w.Sy-wsy:
		oy = w.Sy - wsy
	default:
		oy = cy - wsy + 1
	}
	return ox, oy, wsx, wsy
}

// buildScene is redraw_build_cells for the window area of a client of sx×sy (less its status).
// buf is reused for its cells when it is large enough.
func buildScene(v *View, sy int, buf []buildCell) *scene {
	w := v.Window
	ox, oy, wsx, wsy := windowOffset(w, v.Sx, sy)
	// The scene covers the client's whole area even when the window is smaller.
	sc := &scene{w: w, ox: ox, oy: oy, sx: max(wsx, v.Sx), sy: max(wsy, sy)}
	if n := sc.sx * sc.sy; cap(buf) >= n {
		sc.cells = buf[:n]
	} else {
		sc.cells = make([]buildCell, n)
	}
	for y := range sc.sy {
		for x := range sc.sx {
			if ox+x < w.Sx && oy+y < w.Sy {
				*sc.at(x, y) = buildCell{typ: spanEmpty}
			} else {
				*sc.at(x, y) = buildCell{typ: spanOutside}
			}
		}
	}
	// tmux marks panes from the end of the z order to the start: where borders meet, the pane
	// nearest the start owns them.
	for i := len(w.ZOrder) - 1; i >= 0; i-- {
		p := w.ZOrder[i]
		if !paneVisible(p) {
			continue
		}
		sc.markInside(p)
		sc.markBorders(p)
	}
	if v.Styles.TwoPaneColours {
		sc.markTwoPaneColours()
	}
	return sc
}

// hasPane is redraw_data_has_pane.
func (bc *buildCell) hasPane(p *model.Pane) bool {
	return bc.top == p || bc.bottom == p || bc.left == p || bc.right == p
}

// borderPane is redraw_get_pane_for_border_style: the pane whose style colours a border cell.
func (bc *buildCell) borderPane(active *model.Pane) *model.Pane {
	switch {
	case bc.style != nil:
		return bc.style
	case active != nil && bc.hasPane(active):
		return active
	case bc.top != nil:
		return bc.top
	case bc.bottom != nil:
		return bc.bottom
	case bc.left != nil:
		return bc.left
	}
	return bc.right
}

// styled is a cell of a style: the default cell with the style's colours and attributes
// (tmux's style_apply), its theme colours made the client's.
func (v *View) styled(st vt.Cell) vt.Cell {
	c := vt.DefaultCell
	c.Fg, c.Bg, c.Us, c.Attr = v.theme(st.Fg), v.theme(st.Bg), v.theme(st.Us), st.Attr
	return c
}

// borderCell is the cell drawn for a border or a cell with no pane (redraw_draw_border_span
// with window_get_border_cell and single lines, or window_get_fill_cell).
func borderCell(v *View, bc *buildCell, active *model.Pane) vt.Cell {
	if bc.typ != spanBorder {
		// window_get_fill_cell: the fill cell over the border style.
		gc := v.styled(v.Styles.Border)
		fill := v.Styles.OutsideFill
		if bc.typ == spanEmpty {
			fill = v.Styles.InsideFill
		}
		gc.Data, gc.Width = fill.Data, 1
		gc.Attr |= fill.Attr
		gc.Flags |= fill.Flags
		if fill.Fg != 8 {
			gc.Fg = v.theme(fill.Fg)
		}
		if fill.Bg != 8 {
			gc.Bg = v.theme(fill.Bg)
		}
		if fill.Us != 8 {
			gc.Us = v.theme(fill.Us)
		}
		return gc
	}
	var gc vt.Cell
	if p := bc.borderPane(active); p != nil && p == active {
		gc = v.styled(v.Styles.ActiveBorder)
	} else {
		gc = v.styled(v.Styles.Border)
	}
	gc.Attr |= vt.AttrCharset
	gc.Data = cellBorders[cellType(bc.mask) : cellType(bc.mask)+1]
	return gc
}

// resolve gives a pane's cell its final colours, as tmux's tty_attributes and the palette
// parts of tty_check_fg, tty_check_bg and tty_check_us do: the pane's default colours, its
// palette, and the client's theme colours.
func resolve(gc *vt.Cell, defFg, defBg vt.Color, pal *vt.Palette, v *View) {
	theme := v.theme
	if gc.Flags&vt.FlagNoPalette == 0 {
		if gc.Fg == 8 {
			gc.Fg = defFg
		}
		if gc.Bg == 8 {
			gc.Bg = defBg
		}
		if c := pal.Get(gc.Fg); c != -1 {
			gc.Fg = c
		}
		if c := pal.Get(gc.Bg); c != -1 {
			gc.Bg = c
		}
	}
	gc.Fg, gc.Bg, gc.Us = theme(gc.Fg), theme(gc.Bg), theme(gc.Us)
	if gc.Flags&vt.FlagNoPalette == 0 {
		c := gc.Fg
		if c < 8 && gc.Attr&vt.AttrBright != 0 && !v.NoBright {
			c += 90
		}
		if c = pal.Get(c); c != -1 {
			gc.Fg = c
		}
		if c := pal.Get(gc.Bg); c != -1 {
			gc.Bg = c
		}
		if c := pal.Get(gc.Us); c != -1 {
			gc.Us = c
		}
	}
	gc.Fg, gc.Bg, gc.Us = theme(gc.Fg), theme(gc.Bg), theme(gc.Us)
	gc.Flags &^= vt.FlagNoPalette | vt.FlagSelected
}

// paneLine copies line py of a pane's screen, from column px, into out (n cells), as tmux's
// tty_draw_line would show it: cells past the line's end are blank in the default colours, a
// wide character that does not fit is blank, and padding at the start (a wide character cut
// off) is blank in the colour of the character it belongs to.
func paneLine(out []vt.Cell, p *model.Pane, s *vt.Screen, px, py int, links *linkTable, v *View) {
	gd := s.Grid
	n := len(out)
	defFg, defBg := p.Term.Palette.Fg, p.Term.Palette.Bg
	pal := &p.Term.Palette
	blank := func(bg vt.Color) vt.Cell {
		c := vt.DefaultCell
		c.Flags = vt.FlagCleared
		c.Bg = bg
		resolve(&c, defFg, defBg, pal, v)
		return c
	}
	ex := min(gd.ViewLine(py).Size(), s.SizeX())
	i := 0
	// Padding at the start: clear it with the colour of the character it belongs to.
	for i < n && px+i < ex && gd.ViewCell(px+i, py).Flags&vt.FlagPadding != 0 {
		i++
	}
	if i > 0 {
		bg := vt.Color(8)
		for j := px; j > 0; j-- {
			if c := gd.ViewCell(j-1, py); c.Flags&vt.FlagPadding == 0 {
				bg = c.Bg
				break
			}
		}
		for k := range i {
			out[k] = blank(bg)
		}
	}
	for i < n {
		if px+i >= ex {
			out[i] = blank(8)
			i++
			continue
		}
		gc := gd.ViewCell(px+i, py)
		w := int(gc.Width)
		if gc.Flags&vt.FlagPadding != 0 || w == 0 {
			// A stray padding cell or a zero-width cell: nothing is drawn there.
			out[i] = blank(gc.Bg)
			i++
			continue
		}
		if w > n-i {
			// A wide character (or tab) that does not fit: blank to the end.
			for ; i < n; i++ {
				out[i] = blank(gc.Bg)
			}
			break
		}
		resolve(&gc, defFg, defBg, pal, v)
		if gc.Link != 0 {
			gc.Link = links.intern(s.Hyperlinks, gc.Link)
		}
		out[i] = gc
		for k := 1; k < w; k++ {
			pad := gc
			pad.Data, pad.Width, pad.Flags = "", 0, gc.Flags|vt.FlagPadding
			out[i+k] = pad
		}
		i += w
	}
}

// cursorState is what UpdateMode is given of a screen's cursor.
func cursorState(s *vt.Screen) *termout.CursorState {
	c := s.CColour
	if c == -1 {
		c = s.DefaultCColour
	}
	return &termout.CursorState{Style: s.CStyle, DefaultStyle: s.DefaultCStyle, DefaultMode: s.DefaultMode, Colour: c}
}
