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
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/status"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/termout"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/vt"
)

// Styles are the options that decide how borders look, as cells: only their colours and
// attributes are used.
type Styles struct {
	// Border is the window's pane-border-style, for border cells next to no pane; with
	// ActiveBorder (pane-active-border-style) it is also the style of a pane not in Panes.
	Border       vt.Cell
	ActiveBorder vt.Cell
	// Panes are each pane's border style (pane-active-border-style for the active pane,
	// pane-border-style for the others, expanded for that pane) and pane-border-format line.
	Panes map[*model.Pane]PaneBorder
	// Lines is the window's pane-border-lines (LinesSingle, …).
	Lines int
	// TwoPaneColours is pane-border-indicators colour or both (tmux's default colour): with two
	// panes, the active pane colours half of the border between them.
	TwoPaneColours bool
	// Arrows is pane-border-indicators arrows or both: arrows on the borders by the active pane.
	Arrows bool
	// InsideFill and OutsideFill are fill-character's cells, for the parts of the window no pane
	// covers and for the client's area outside the window: their character, attributes, and
	// any colours that are not the default, over the border style.
	InsideFill, OutsideFill vt.Cell
}

// PaneBorder is how one pane's borders are drawn.
type PaneBorder struct {
	Style vt.Cell // its border style
	Index int     // its index, pane-base-index added, for pane-border-lines number
	// Status is its pane-border-format, expanded, for the pane-border-status line.
	Status string
}

// pane-border-lines values (tmux's PANE_LINES_*).
const (
	LinesSingle = iota
	LinesDouble
	LinesHeavy
	LinesSimple
	LinesNumber
	LinesSpaces
	LinesNone
)

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
	// Message, when not nil, is what the status lines show while a message is shown (tmux's
	// status_message_redraw): at least one line, over the window's last (or first) line when
	// the status is off.
	Message [][]vt.Cell
	// Freeze keeps the panes as the last frame showed them (TTY_FREEZE: a message is shown);
	// NoCursor hides the cursor (TTY_NOCURSOR).
	Freeze, NoCursor bool
	// PromptCursor is where the cursor goes while the client's prompt is open (its column and
	// terminal line), nil without one.
	PromptCursor *[2]int
	// Popup is the popup the client shows over everything, if any.
	Popup *Popup
	// Menu is the window's menu, drawn over its panes and borders, if any.
	Menu *Menu

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

// simpleBorders are pane-border-lines simple's characters (tmux's SIMPLE_BORDERS).
const simpleBorders = " |-+++++++++."

// doubleBorders and heavyBorders are pane-border-lines double's and heavy's characters (tmux's
// tty_acs_double_borders and tty_acs_heavy_borders), by cell type.
var (
	doubleBorders = [...]string{"", "\u2551", "\u2550", "\u2554", "\u2557", "\u255a", "\u255d", "\u2566", "\u2569", "\u2560", "\u2563", "\u256c", "\u00b7"}
	heavyBorders  = [...]string{"", "\u2503", "\u2501", "\u250f", "\u2513", "\u2517", "\u251b", "\u2533", "\u253b", "\u2523", "\u252b", "\u254b", "\u00b7"}
)

// borderChar is window_get_border_cell: gc with the character of a border cell of type ct for
// pane-border-lines lines. index is the pane's index for number, or -1 for none.
func borderChar(gc vt.Cell, lines, ct, index int) vt.Cell {
	gc.Width = 1
	switch lines {
	case LinesNumber:
		if ct == cellNone {
			gc.Attr |= vt.AttrCharset
			gc.Data = cellBorders[cellNone : cellNone+1]
			break
		}
		gc.Attr &^= vt.AttrCharset
		if index >= 0 {
			gc.Data = string(rune('0' + index%10))
		} else {
			gc.Data = "*"
		}
	case LinesDouble:
		gc.Attr &^= vt.AttrCharset
		gc.Data = doubleBorders[ct]
	case LinesHeavy:
		gc.Attr &^= vt.AttrCharset
		gc.Data = heavyBorders[ct]
	case LinesSimple:
		gc.Attr &^= vt.AttrCharset
		gc.Data = simpleBorders[ct : ct+1]
	case LinesNone, LinesSpaces:
		gc.Attr &^= vt.AttrCharset
		gc.Data = " "
	default:
		gc.Attr |= vt.AttrCharset
		gc.Data = cellBorders[ct : ct+1]
	}
	return gc
}

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
	spanStatus // a pane-border-status line
)

// buildCell is one cell of the window area while it is built (tmux's redraw_build_cell).
type buildCell struct {
	typ    spanType
	pane   *model.Pane // spanPane
	px, py int

	top, bottom, left, right *model.Pane // spanBorder: the panes on each side
	style                    *model.Pane // the pane chosen to colour it (two panes)
	mask                     int
	arrow                    bool // an indicator arrow goes here

	// spanStatus: the pane, the offset in its status line and the border cell type beneath.
	stPane   *model.Pane
	stOffset int
	stType   int
}

// scene is the window area of a client (tmux's redraw_build_ctx).
type scene struct {
	w              *model.Window
	ox, oy, sx, sy int
	cells          []buildCell
	arrows         bool // pane-border-indicators arrows or both
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
	sc.markBorderStatus(p, status, right, top, bottom)
	sc.markBorderArrows(p, left, right, top, bottom)
}

// markBorderStatus is redraw_mark_border_status: the border cells of the pane's status line
// become status cells, keeping their cell type for the line drawn beneath the text.
func (sc *scene) markBorderStatus(p *model.Pane, status layout.Status, right, top, bottom int) {
	var wy int
	switch status {
	case layout.StatusTop:
		wy = top
	case layout.StatusBottom:
		wy = bottom
	default:
		return
	}
	off := 0
	for wx := p.Xoff + 2; wx <= right-1; wx, off = wx+1, off+1 {
		x, y, ok := sc.windowToScene(wx, wy)
		if !ok {
			continue
		}
		bc := sc.at(x, y)
		if bc.typ != spanBorder {
			continue
		}
		*bc = buildCell{typ: spanStatus, stPane: p, stOffset: off, stType: cellType(bc.mask)}
	}
}

// markBorderArrows is redraw_mark_border_arrows: the border cells where indicator arrows go.
func (sc *scene) markBorderArrows(p *model.Pane, left, right, top, bottom int) {
	if !sc.arrows {
		return
	}
	mark := func(wx, wy int) {
		if x, y, ok := sc.windowToScene(wx, wy); ok {
			if bc := sc.at(x, y); bc.typ == spanBorder {
				bc.arrow = true
			}
		}
	}
	if wx := p.Xoff + 1; wx >= left && wx <= right {
		mark(wx, top)
		mark(wx, bottom)
	}
	if wy := p.Yoff + 1; wy >= top && wy <= bottom {
		mark(left, wy)
		mark(right, wy)
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

// WindowOffset is tty_window_offset: where a client of sx×sy (less its status lines) shows
// the window from.
func WindowOffset(w *model.Window, sx, sy int) (ox, oy int) {
	ox, oy, _, _ = windowOffset(w, sx, sy)
	return ox, oy
}

// WindowArea is tty_window_offset with the size: where the client shows the window from and
// how much of it.
func WindowArea(w *model.Window, sx, sy int) (ox, oy, wsx, wsy int) {
	return windowOffset(w, sx, sy)
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
	s := paneScreen(p)
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
	sc := &scene{w: w, ox: ox, oy: oy, sx: max(wsx, v.Sx), sy: max(wsy, sy), arrows: v.Styles.Arrows}
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
	p := bc.borderPane(active)
	gc := v.styled(v.Styles.Border)
	if p != nil {
		gc = v.styled(v.paneBorderStyle(p, active))
	}
	gc = borderChar(gc, v.Styles.Lines, cellType(bc.mask), v.paneIndex(p))
	// redraw_draw_border_arrow: an arrow (in the line drawing set) pointing at the active pane.
	if bc.arrow && active != nil {
		ch := ""
		switch active {
		case bc.left:
			ch = ","
		case bc.right:
			ch = "+"
		case bc.top:
			ch = "-"
		case bc.bottom:
			ch = "."
		}
		if ch != "" {
			gc.Data = ch
			gc.Attr |= vt.AttrCharset
		}
	}
	return gc
}

// paneBorderStyle is window_pane_get_border_style: the style of p's borders.
func (v *View) paneBorderStyle(p, active *model.Pane) vt.Cell {
	if pb, ok := v.Styles.Panes[p]; ok {
		return pb.Style
	}
	if p == active {
		return v.Styles.ActiveBorder
	}
	return v.Styles.Border
}

// paneIndex is window_pane_index: p's index for pane-border-lines number, or -1 for no pane.
func (v *View) paneIndex(p *model.Pane) int {
	if p == nil {
		return -1
	}
	if pb, ok := v.Styles.Panes[p]; ok {
		return pb.Index
	}
	return p.Window.PaneIndex(p)
}

// paneStatusLines is window_make_pane_status for each pane with status cells in the scene: the
// border line beneath, as wide as the pane's status cells reach, with pane-border-format drawn
// over it in the pane's border style.
func (v *View) paneStatusLines(sc *scene, active *model.Pane) map[*model.Pane][]vt.Cell {
	var types map[*model.Pane]map[int]int
	for i := range sc.cells {
		bc := &sc.cells[i]
		if bc.typ != spanStatus {
			continue
		}
		if types == nil {
			types = map[*model.Pane]map[int]int{}
		}
		if types[bc.stPane] == nil {
			types[bc.stPane] = map[int]int{}
		}
		types[bc.stPane][bc.stOffset] = bc.stType
	}
	if types == nil {
		return nil
	}
	out := make(map[*model.Pane][]vt.Cell, len(types))
	for p, ts := range types {
		width := 0
		for off := range ts {
			width = max(width, off+1)
		}
		st := v.paneBorderStyle(p, active)
		gc := vt.DefaultCell
		gc.Fg, gc.Bg, gc.Us, gc.Attr = st.Fg, st.Bg, st.Us, st.Attr
		cells := make([]vt.Cell, width)
		for i := range cells {
			ct, ok := ts[i]
			if !ok {
				ct = cellLR
			}
			gc = borderChar(gc, v.Styles.Lines, ct, v.paneIndex(p))
			cells[i] = gc
		}
		gc.Attr &^= vt.AttrCharset
		status.DrawFormat(cells, gc, width, v.Styles.Panes[p].Status, nil, false)
		for i := range cells {
			c := &cells[i]
			c.Fg, c.Bg, c.Us = v.theme(c.Fg), v.theme(c.Bg), v.theme(c.Us)
		}
		out[p] = cells
	}
	return out
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
	fg, bg := p.Term.Palette.Defaults(p.Window != nil && p.Window.Active == p)
	screenLine(out, s, px, py, fg, bg, &p.Term.Palette, links, v)
}

// screenLine is tty_draw_line for line py of a screen from column px, with the default
// colours defFg and defBg and the palette pal.
func screenLine(out []vt.Cell, s *vt.Screen, px, py int, defFg, defBg vt.Color, pal *vt.Palette, links *linkTable, v *View) {
	gd := s.Grid
	n := len(out)
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

// paneScreen is wp->screen: the screen of the pane's mode while it is in one, else its
// program's.
func paneScreen(p *model.Pane) *vt.Screen {
	if p.Mode != nil {
		return p.Mode.Screen()
	}
	return p.Term.Screen
}
