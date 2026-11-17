// Ported from tmux 3.8's window-copy.c, grid-reader.c and the selection parts of screen.c,
// for the subset of copy mode in COMPATIBILITY.md §3:
//
// Copyright (c) 2007 Nicholas Marriott <nicholas.marriott@gmail.com>
// Copyright (c) 2020 Anindya Mukherjee <anindya49@hotmail.com>
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

package copymode

import (
	"strconv"
	"strings"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/model"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/status"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/termout"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/vt"
)

// whitespace is tmux's WHITESPACE.
const whitespace = "\t "

// Env is what copy mode needs from the server: styles and formats for its pane, and where a
// copy goes.
type Env interface {
	// Style is style_apply of a window option for the pane, with its formats.
	Style(p *model.Pane, option string) vt.Cell
	// Expand is format_expand for the pane, the mode's variables included.
	Expand(p *model.Pane, s string) string
	// Copy is window_copy_copy_buffer: data becomes a paste buffer (paste, with prefix) and
	// goes to the clipboard of the clients showing the pane (clip, per set-clipboard).
	Copy(p *model.Pane, prefix string, data []byte, paste, clip bool)
}

// Kinds of selection and drags (tmux's LINE_SEL_*, SEL_* and CURSORDRAG_*).
const (
	lineSelNone = iota
	lineSelLeftRight
	lineSelRightLeft
)

const (
	selChar = iota
	selWord
	selLine
)

const (
	dragNone = iota
	dragEndSel
	dragSel
)

// Action is what a command leaves to do (tmux's window_copy_cmd_action).
type Action int

// Actions.
const (
	ActionNothing Action = iota
	ActionMove
	ActionRedraw
	ActionCancel
)

// sel is screen_sel: the selection on the view, in its coordinates.
type sel struct {
	hidden    bool
	sx, sy    int
	ex, ey    int
	cell      vt.Cell
	emacsKeys bool
	rectangle bool
	clipx     int
}

// Mode is copy mode for one pane (window_copy_mode_data).
type Mode struct {
	pane    *model.Pane
	env     Env
	backing *vt.Screen // the pane's screen and history when copy mode began
	screen  *vt.Screen // what the pane shows

	cx, cy, oy     int
	lastcx, lastsx int

	hidePosition bool
	scrollExit   bool

	sel *sel
	// The selection, in backing coordinates: selx, sely to endselx, endsely; selr* and endselr*
	// are a word or line selection's first extent, dx, dy where it began.
	selx, sely         int
	endselx, endsely   int
	selrx, selry       int
	endselrx, endselry int
	dx, dy             int
	cursordrag         int
	lineflag           int
	selflag            int
	separators         string
}

// New is window_copy_init: copy mode for pane p, a copy of its screen and history.
func New(p *model.Pane, env Env, hidePosition, scrollExit bool) *Mode {
	m := &Mode{pane: p, env: env, hidePosition: hidePosition, scrollExit: scrollExit}
	p.Mu.Lock()
	base := p.Term.Screen
	m.backing = cloneScreen(base)
	p.Mu.Unlock()
	m.screen = vt.NewScreen(p.Sx, p.Sy, 0)
	hsize := m.backing.Grid.HSize
	cx, cy := m.backing.Cx, hsize+m.backing.Cy
	m.cx = cx
	if cy < hsize {
		m.cy, m.oy = 0, hsize-cy
	} else {
		m.cy, m.oy = cy-hsize, 0
	}
	return m
}

// Redraw draws the view again; the caller does it once the mode is the pane's, so the
// position indicator's formats see it.
func (m *Mode) Redraw() { m.redraw() }

// cloneScreen is window_copy_clone_screen for a pane whose size is the screen's: the lines
// of history and screen, the cursor at the bottom when it was below the copy.
func cloneScreen(src *vt.Screen) *vt.Screen {
	gd := src.Grid
	total := gd.HSize + gd.Sy
	dst := vt.NewScreen(gd.Sx, total, gd.HLimit)
	dst.Grid.SetHistory(true)
	dst.Grid.DuplicateLines(0, gd, 0, total)
	dst.Grid.Sy = total - gd.HSize
	dst.Grid.HSize = gd.HSize
	dst.Grid.HScrolled = gd.HScrolled
	if src.Cy > dst.Grid.Sy-1 {
		dst.Cx, dst.Cy = 0, dst.Grid.Sy-1
	} else {
		dst.Cx, dst.Cy = src.Cx, src.Cy
	}
	return dst
}

// Screen implements model.PaneMode.
func (m *Mode) Screen() *vt.Screen { return m.screen }

// Name implements model.PaneMode.
func (m *Mode) Name() string { return "copy-mode" }

// KeyTable is window_copy_key_table.
func (m *Mode) KeyTable() string {
	if m.viKeys() {
		return "copy-mode-vi"
	}
	return "copy-mode"
}

func (m *Mode) viKeys() bool { return m.pane.Window.Options.GetNumber("mode-keys") == 1 }

func (m *Mode) hsize() int { return m.backing.Grid.HSize }

func (m *Mode) sx() int { return m.screen.SizeX() }

func (m *Mode) sy() int { return m.screen.SizeY() }

// Formats is window_copy_formats for the variables in the subset.
func (m *Mode) Formats() map[string]string {
	v := map[string]string{
		"top_line_time":       "0",
		"scroll_position":     strconv.Itoa(m.oy),
		"copy_position":       strconv.Itoa(m.oy),
		"copy_position_limit": strconv.Itoa(m.hsize()),
		"copy_line_numbers":   "0",
		"rectangle_toggle":    "0",
		"copy_cursor_x":       strconv.Itoa(m.cx),
		"copy_cursor_y":       strconv.Itoa(m.cy),
		"search_present":      "0",
		"search_timed_out":    "0",
		"selection_active":    "0",
		"selection_present":   "0",
	}
	if m.sel != nil {
		v["selection_start_x"] = strconv.Itoa(m.selx)
		v["selection_start_y"] = strconv.Itoa(m.sely)
		v["selection_end_x"] = strconv.Itoa(m.endselx)
		v["selection_end_y"] = strconv.Itoa(m.endsely)
		if m.cursordrag != dragNone {
			v["selection_active"] = "1"
		}
		if !m.sel.hidden {
			v["selection_present"] = "1"
		}
	}
	switch m.selflag {
	case selChar:
		v["selection_mode"] = "char"
	case selWord:
		v["selection_mode"] = "word"
	case selLine:
		v["selection_mode"] = "line"
	}
	return v
}

// Resize follows the pane's size: the view is drawn again at it.
func (m *Mode) Resize(sx, sy int) {
	if sx == m.sx() && sy == m.sy() {
		return
	}
	m.screen = vt.NewScreen(sx, sy, 0)
	if m.cy > sy-1 {
		m.cy = sy - 1
	}
	if m.cx > sx {
		m.cx = sx
	}
	m.updateSelection(false)
	m.redraw()
}

// lineLength is window_copy_find_length.
func (m *Mode) lineLength(py int) int { return m.backing.Grid.LineLength(py) }

// cursorLimit is window_copy_cursor_limit.
func (m *Mode) cursorLimit(py int, onemore bool) int {
	if onemore || !m.viKeys() {
		return m.lineLength(py)
	}
	return m.backing.Grid.LineLimit(py)
}

// redraw is window_copy_redraw_screen: every line of the view drawn from the copy, with the
// selection and the position indicator.
func (m *Mode) redraw() {
	sx, sy := m.sx(), m.sy()
	gd := m.screen.Grid
	hsize := m.hsize()
	for py := range sy {
		line := m.writeLine(py, hsize-m.oy+py)
		if py == 0 && !m.hidePosition {
			if f := m.pane.Window.Options.GetString("copy-mode-position-format"); f != "" {
				if expanded := m.env.Expand(m.pane, f); expanded != "" {
					status.DrawFormat(line, m.env.Style(m.pane, "copy-mode-position-style"), sx, expanded, nil, false)
				}
			}
		}
		if py == m.cy && m.cx >= sx {
			line[sx-1] = vt.DefaultCell
			line[sx-1].Data, line[sx-1].Width = "$", 1
		}
		gd.Clear(0, py, sx, 1, 8)
		for x := 0; x < sx; x++ {
			gc := line[x]
			if gc.Flags&vt.FlagPadding != 0 {
				gd.SetPadding(x, py, 8)
				continue
			}
			gd.SetCell(x, py, &gc)
		}
	}
	m.screen.Cx, m.screen.Cy = min(m.cx, sx-1), m.cy
}

// writeLine is window_copy_write_one: line fy of the copy as the view's line py, with the
// selection's style over its cells (tty_draw_line's screen_check_selection).
func (m *Mode) writeLine(py, fy int) []vt.Cell {
	sx := m.sx()
	gd := m.backing.Grid
	line := make([]vt.Cell, sx)
	for i := range line {
		line[i] = vt.DefaultCell
	}
	x := 0
	put := func(gc vt.Cell) {
		if x < sx {
			if m.checkSelection(x, py) {
				gc = m.selectCell(gc)
			}
			line[x] = gc
		}
		x++
	}
	for fx := 0; fx < sx; fx++ {
		gc := gd.Cell(fx, fy)
		width := int(gc.Width)
		switch {
		case fx+width > sx:
			sp := vt.DefaultCell
			sp.Data, sp.Width = " ", 1
			x = fx
			put(sp)
		case gc.Flags&vt.FlagPadding != 0:
			// A wide character's padding: drawn as a space only where the character
			// itself was not.
			if x <= fx {
				gc.Flags &^= vt.FlagPadding
				gc.Data, gc.Width = " ", 1
				x = fx
				put(gc)
			}
		case gc.Flags&vt.FlagTab != 0:
			gc.Flags &^= vt.FlagTab
			gc.Data = " "
			n := max(width, 1)
			gc.Width = 1
			x = fx
			for range n {
				put(gc)
			}
		default:
			x = fx
			put(gc)
			for i := 1; i < width && x < sx; i++ {
				pad := vt.DefaultCell
				pad.Flags = vt.FlagPadding
				line[x] = pad
				x++
			}
		}
	}
	return line
}

// checkSelection is screen_check_selection.
func (m *Mode) checkSelection(px, py int) bool {
	s := m.sel
	if s == nil || s.hidden || px < s.clipx {
		return false
	}
	if s.rectangle {
		return false
	}
	var xx int
	switch {
	case s.sy < s.ey:
		if py < s.sy || py > s.ey {
			return false
		}
		if py == s.sy && px < s.sx {
			return false
		}
		if s.emacsKeys {
			xx = max(s.ex-1, 0)
		} else {
			xx = s.ex
		}
		if py == s.ey && px > xx {
			return false
		}
	case s.sy > s.ey:
		if py > s.sy || py < s.ey {
			return false
		}
		if py == s.ey && px < s.ex {
			return false
		}
		if s.emacsKeys {
			xx = s.sx - 1
		} else {
			xx = s.sx
		}
		if py == s.sy && (s.sx == 0 || px > xx) {
			return false
		}
	default:
		if py != s.sy {
			return false
		}
		if s.ex < s.sx {
			if s.emacsKeys {
				xx = s.sx - 1
			} else {
				xx = s.sx
			}
			if px > xx || px < s.ex {
				return false
			}
		} else {
			if s.emacsKeys {
				xx = max(s.ex-1, 0)
			} else {
				xx = s.ex
			}
			if px < s.sx || px > xx {
				return false
			}
		}
	}
	return true
}

// selectCell is screen_select_cell: the selection's style over a cell.
func (m *Mode) selectCell(src vt.Cell) vt.Cell {
	dst := m.sel.cell
	if dst.Fg.IsDefault() {
		dst.Fg = src.Fg
	}
	if dst.Bg.IsDefault() {
		dst.Bg = src.Bg
	}
	dst.Data, dst.Width = src.Data, src.Width
	dst.Flags = src.Flags | vt.FlagNoPalette
	if dst.Attr&vt.AttrNoAttr != 0 {
		dst.Attr |= src.Attr & vt.AttrCharset
	} else {
		dst.Attr |= src.Attr
	}
	return dst
}

// updateCursor is window_copy_update_cursor.
func (m *Mode) updateCursor(cx, cy int) {
	if cy < m.sy() {
		py := m.hsize() + cy - m.oy
		if maxx := m.cursorLimit(py, false); cx > maxx {
			cx = maxx
		}
	}
	m.cx, m.cy = cx, cy
}

// scrollUp is window_copy_scroll_up: the view moves ny lines down the copy.
func (m *Mode) scrollUp(ny int) {
	ny = min(ny, m.oy)
	if ny == 0 {
		return
	}
	m.oy -= ny
	m.updateSelectionView()
}

// scrollDown is window_copy_scroll_down: the view moves ny lines up the copy.
func (m *Mode) scrollDown(ny int) {
	hsize := m.hsize()
	if ny > hsize {
		return
	}
	if m.oy > hsize-ny {
		ny = hsize - m.oy
	}
	if ny == 0 {
		return
	}
	m.oy += ny
	m.updateSelectionView()
}

// updateSelectionView is window_copy_update_selection_view: the selection follows the view.
func (m *Mode) updateSelectionView() {
	if m.sel == nil && m.lineflag == lineSelNone {
		return
	}
	m.setSelection(true)
}

// cursorUp is window_copy_cursor_up, without rectangles.
func (m *Mode) cursorUp(scrollOnly bool) {
	oy := m.hsize() + m.cy - m.oy
	if ox := m.lineLength(oy); m.cx != ox {
		m.lastcx, m.lastsx = m.cx, ox
	}
	if m.lineflag == lineSelLeftRight && oy == m.sely {
		m.otherEnd()
	}
	if scrollOnly && m.viKeys() && m.cy < m.sy()-1 {
		m.updateCursor(m.cx, m.cy+1)
	}
	if scrollOnly || m.cy == 0 {
		m.cx = m.lastcx
		m.scrollDown(1)
	} else {
		m.updateCursor(m.lastcx, m.cy-1)
		m.updateSelection(false)
	}
	m.afterVertical()
}

// cursorDown is window_copy_cursor_down, without rectangles.
func (m *Mode) cursorDown(scrollOnly bool) {
	oy := m.hsize() + m.cy - m.oy
	if ox := m.lineLength(oy); m.cx != ox {
		m.lastcx, m.lastsx = m.cx, ox
	}
	if m.lineflag == lineSelRightLeft && oy == m.endsely {
		m.otherEnd()
	}
	if scrollOnly && m.viKeys() && m.cy > 0 {
		m.updateCursor(m.cx, m.cy-1)
	}
	if scrollOnly || m.cy == m.sy()-1 {
		m.cx = m.lastcx
		m.scrollUp(1)
	} else {
		m.updateCursor(m.lastcx, m.cy+1)
		m.updateSelection(false)
	}
	m.afterVertical()
}

// afterVertical is the end of window_copy_cursor_up and _down: the cursor kept within its
// line, and at its end for a line selection.
func (m *Mode) afterVertical() {
	py := m.hsize() + m.cy - m.oy
	px := m.lineLength(py)
	if (m.cx >= m.lastsx && m.cx != px) || m.cx > px {
		m.updateCursor(px, m.cy)
		m.updateSelection(false)
	}
	switch m.lineflag {
	case lineSelLeftRight:
		m.updateCursor(m.lineLength(m.hsize()+m.cy-m.oy), m.cy)
		m.updateSelection(false)
	case lineSelRightLeft:
		m.updateCursor(0, m.cy)
		m.updateSelection(false)
	}
}

// otherEnd is window_copy_other_end.
func (m *Mode) otherEnd() {
	if m.sel == nil && m.lineflag == lineSelNone {
		return
	}
	switch m.lineflag {
	case lineSelLeftRight:
		m.lineflag = lineSelRightLeft
	case lineSelRightLeft:
		m.lineflag = lineSelLeftRight
	}
	if m.cursordrag == dragEndSel {
		m.cursordrag = dragSel
	} else {
		m.cursordrag = dragEndSel
	}
	selx, sely := m.endselx, m.endsely
	if m.cursordrag == dragSel {
		selx, sely = m.selx, m.sely
	}
	cy := m.cy
	yy := m.hsize() + m.cy - m.oy
	m.cx = selx
	hsize := m.hsize()
	switch {
	case sely < hsize-m.oy:
		m.oy = hsize - sely
		m.cy = 0
	case sely > hsize-m.oy+m.sy():
		m.oy = hsize - sely + m.sy() - 1
		m.cy = m.sy() - 1
	default:
		m.cy = cy + sely - yy
	}
	yy = m.hsize() + m.cy - m.oy
	if limit := m.cursorLimit(yy, false); m.cx > limit {
		m.cx = limit
	}
	m.updateSelectionNoReset()
}

// acquireUp is window_copy_acquire_cursor_up: the cursor goes to px, py of the copy, the
// view scrolling up to it.
func (m *Mode) acquireUp(px, py int) {
	yy := m.hsize() - m.oy
	cy := 0
	ny := 0
	if py < yy {
		ny = yy - py
	} else {
		cy = py - yy
	}
	for ; ny > 0; ny-- {
		m.cursorUp(true)
	}
	m.updateCursor(px, cy)
	m.updateSelection(false)
}

// acquireDown is window_copy_acquire_cursor_down.
func (m *Mode) acquireDown(px, py int, noReset bool) {
	cy := py - m.hsize() + m.oy
	yy := m.sy() - 1
	for ny := cy - yy; ny > 0; ny-- {
		m.cursorDown(true)
	}
	m.updateCursor(px, min(cy, yy))
	if noReset {
		m.updateSelectionNoReset()
	} else {
		m.updateSelection(false)
	}
}

// startOfLine and endOfLine are window_copy_cursor_start_of_line and _end_of_line.
func (m *Mode) startOfLine() {
	r := m.reader()
	r.startOfLine(true)
	m.acquireUp(r.cx, r.cy)
}

func (m *Mode) endOfLine() {
	r := m.reader()
	r.endOfLine(true, false)
	m.acquireDown(m.cursorLimit(r.cy, false), r.cy, false)
}

// cursorLeft and cursorRight are window_copy_cursor_left and _right.
func (m *Mode) cursorLeft() {
	r := m.reader()
	r.left(true)
	m.acquireUp(r.cx, r.cy)
}

func (m *Mode) cursorRight() {
	r := m.reader()
	r.right(true, false, !m.viKeys())
	m.acquireDown(r.cx, r.cy, false)
}

// previousWord and nextWordEnd are window_copy_cursor_previous_word and _next_word_end.
func (m *Mode) previousWord(separators string, already bool) {
	r := m.reader()
	r.previousWord(separators, already, !m.viKeys())
	m.acquireUp(r.cx, r.cy)
}

func (m *Mode) nextWordEnd(separators string, noReset bool) {
	r := m.reader()
	if m.viKeys() {
		if !r.inSet(whitespace) {
			r.right(false, false, false)
		}
		r.nextWordEnd(separators)
		r.left(true)
	} else {
		r.nextWordEnd(separators)
	}
	m.acquireDown(r.cx, r.cy, noReset)
}

// previousWordPos and nextWordEndPos are the _pos forms: where the moves would go.
func (m *Mode) previousWordPos(separators string) (int, int) {
	r := m.reader()
	r.previousWord(separators, false, true)
	return r.cx, r.cy
}

func (m *Mode) nextWordEndPos(separators string) (int, int) {
	r := m.reader()
	if m.viKeys() {
		if !r.inSet(whitespace) {
			r.right(false, false, false)
		}
		r.nextWordEnd(separators)
		r.left(true)
	} else {
		r.nextWordEnd(separators)
	}
	return r.cx, r.cy
}

func (m *Mode) reader() *reader {
	return &reader{gd: m.backing.Grid, cx: m.cx, cy: m.hsize() + m.cy - m.oy}
}

// The selection.

// startSelection is window_copy_start_selection.
func (m *Mode) startSelection() {
	m.selx = m.cx
	m.sely = m.hsize() + m.cy - m.oy
	m.endselx, m.endsely = m.selx, m.sely
	m.cursordrag = dragEndSel
	m.setSelection(false)
}

// clearSelection is window_copy_clear_selection.
func (m *Mode) clearSelection() {
	m.sel = nil
	m.cursordrag = dragNone
	m.lineflag = lineSelNone
	m.selflag = selChar
	py := m.hsize() + m.cy - m.oy
	if px := m.cursorLimit(py, false); m.cx > px {
		m.updateCursor(px, m.cy)
	}
}

// updateSelection is window_copy_update_selection.
func (m *Mode) updateSelection(noReset bool) bool {
	if m.sel == nil && m.lineflag == lineSelNone {
		return false
	}
	return m.setSelection(noReset)
}

func (m *Mode) updateSelectionNoReset() { m.updateSelection(true) }

// setSelection is window_copy_set_selection.
func (m *Mode) setSelection(noReset bool) bool {
	m.synchronizeCursor(noReset)
	sx, sy := m.selx, m.sely
	startPos := m.adjustSelection(&sx, &sy)
	ex, ey := m.endselx, m.endsely
	endPos := m.adjustSelection(&ex, &ey)
	if startPos == endPos && startPos != relOnScreen {
		// screen_hide_selection: a selection there is hidden, none stays none.
		if m.sel != nil {
			m.sel.hidden = true
		}
		return false
	}
	// screen_set_selection: a selection made or set again, shown.
	m.sel = &sel{
		sx: sx, sy: sy, ex: ex, ey: ey,
		cell:      m.env.Style(m.pane, "copy-mode-selection-style"),
		emacsKeys: !m.viKeys(),
	}
	return true
}

// Where a selection end is (tmux's WINDOW_COPY_REL_POS_*).
const (
	relAbove = iota
	relOnScreen
	relBelow
)

// adjustSelection is window_copy_adjust_selection: an end of the selection in view
// coordinates.
func (m *Mode) adjustSelection(selx, sely *int) int {
	sx, sy := *selx, *sely
	ty := m.hsize() - m.oy
	var rel int
	switch {
	case sy < ty:
		rel = relAbove
		sx, sy = 0, 0
	case sy > ty+m.sy()-1:
		rel = relBelow
		sx, sy = m.sx()-1, m.sy()-1
	default:
		rel = relOnScreen
		sy -= ty
	}
	*selx, *sely = sx, sy
	return rel
}

// synchronizeCursor is window_copy_synchronize_cursor.
func (m *Mode) synchronizeCursor(noReset bool) {
	switch m.cursordrag {
	case dragEndSel:
		m.synchronizeCursorEnd(false, noReset)
	case dragSel:
		m.synchronizeCursorEnd(true, noReset)
	}
}

func (m *Mode) synchronizeCursorEnd(begin, noReset bool) {
	xx := m.cx
	yy := m.hsize() + m.cy - m.oy
	switch m.selflag {
	case selWord:
		if noReset {
			break
		}
		begin = false
		if m.dy > yy || (m.dy == yy && m.dx > xx) {
			xx, yy = m.previousWordPos(m.separators)
			begin = true
			m.endselx, m.endsely = m.endselrx, m.endselry
		} else {
			if xx >= m.lineLength(yy) || !inSet(m.backing.Grid, xx+1, yy, whitespace) {
				xx, yy = m.nextWordEndPos(m.separators)
			}
			m.selx, m.sely = m.selrx, m.selry
		}
	case selLine:
		if noReset {
			break
		}
		begin = false
		if m.dy > yy {
			xx = 0
			begin = true
			m.endselx, m.endsely = m.endselrx, m.endselry
		} else {
			yy = max(yy, m.endselry)
			xx = m.lineLength(yy)
			m.selx, m.sely = m.selrx, m.selry
		}
	}
	if begin {
		m.selx, m.sely = xx, yy
	} else {
		m.endselx, m.endsely = xx, yy
	}
}

// selection is window_copy_get_selection: the selected text, or nil.
func (m *Mode) selection() []byte {
	if m.sel == nil && m.lineflag == lineSelNone {
		return nil
	}
	var sx, sy, ex, ey int
	xx, yy := m.endselx, m.endsely
	if yy < m.sely || (yy == m.sely && xx < m.selx) {
		sx, sy, ex, ey = xx, yy, m.selx, m.sely
	} else {
		sx, sy, ex, ey = m.selx, m.sely, xx, yy
	}
	eyLast := m.lineLength(ey)
	ex = min(ex, eyLast)
	emacs := !m.viKeys()
	lastex := ex + 1
	if emacs {
		lastex = ex
	}
	restex, firstsx, restsx := m.sx(), sx, 0
	var buf []byte
	for i := sy; i <= ey; i++ {
		from, to := restsx, restex
		if i == sy {
			from = firstsx
		}
		if i == ey {
			to = lastex
		}
		buf = m.copyLine(buf, i, from, to)
	}
	if len(buf) == 0 {
		return nil
	}
	if emacs || lastex <= eyLast {
		if m.backing.Grid.Line(ey).Flags&vt.LineWrapped == 0 || lastex != eyLast {
			buf = buf[:len(buf)-1]
		}
	}
	return buf
}

// copyLine is window_copy_copy_line.
func (m *Mode) copyLine(buf []byte, sy, sx, ex int) []byte {
	if sx > ex {
		return buf
	}
	gd := m.backing.Grid
	gl := gd.Line(sy)
	wrapped := gl.Flags&vt.LineWrapped != 0 && gl.Size() <= gd.Sx
	xx := m.lineLength(sy)
	if wrapped {
		xx = gl.Size()
	}
	ex, sx = min(ex, xx), min(sx, xx)
	for i := sx; i < ex; i++ {
		gc := gd.Cell(i, sy)
		if gc.Flags&vt.FlagPadding != 0 {
			continue
		}
		if gc.Flags&vt.FlagTab != 0 {
			buf = append(buf, '\t')
			continue
		}
		if len(gc.Data) == 1 && gc.Attr&vt.AttrCharset != 0 {
			if s := termout.ACSUTF8(gc.Data[0]); s != "" {
				buf = append(buf, s...)
				continue
			}
		}
		buf = append(buf, gc.Data...)
	}
	if !wrapped || ex != xx {
		buf = append(buf, '\n')
	}
	return buf
}

// The mouse.

// MoveMouse is window_copy_move_mouse: the cursor goes to the mouse at x, y of the pane.
func (m *Mode) MoveMouse(x, y int) { m.updateCursor(x, y) }

// mouseInSelection is window_copy_mouse_in_selection.
func (m *Mode) mouseInSelection(x, y int) (inside, onStart, onEnd bool) {
	if m.sel == nil {
		return false, false, false
	}
	sx, sy := m.sx(), m.sy()
	screeny := m.hsize() - m.oy
	if s := m.sely - screeny; m.sely >= screeny && s < sy && x == m.selx && y == s {
		return true, true, false
	}
	if e := m.endsely - screeny; m.endsely >= screeny && e < sy && x == m.endselx && y == e {
		return true, false, true
	}
	if (x != m.cx || y != m.cy) && !m.checkSelection(x, y) {
		return false, false, false
	}
	my := screeny + y
	abs := func(n int) int {
		if n < 0 {
			return -n
		}
		return n
	}
	mpos := my*(sx+1) + x
	spos := m.sely*(sx+1) + m.selx
	epos := m.endsely*(sx+1) + m.endselx
	if abs(mpos-spos) <= abs(mpos-epos) {
		return true, true, false
	}
	return true, false, true
}

// StartDrag is window_copy_start_drag with the drag's start at x, y of the pane; the caller
// makes DragUpdate and DragRelease the client's drag callbacks.
func (m *Mode) StartDrag(x, y int) {
	inside, onStart, onEnd := m.mouseInSelection(x, y)
	yg := m.hsize() + y - m.oy
	if onStart || onEnd || !inside || x < m.selrx || x > m.endselrx || yg != m.selry {
		m.lineflag = lineSelNone
		m.selflag = selChar
	}
	switch m.selflag {
	case selWord:
		if m.separators != "" {
			m.updateCursor(x, y)
			px, py := m.previousWordPos(m.separators)
			x, y = px, py-(m.hsize()-m.oy)
		}
		m.updateCursor(x, y)
	case selLine:
		m.updateCursor(0, y)
	default:
		m.updateCursor(x, y)
		if !inside {
			m.startSelection()
		} else {
			if onStart {
				m.cursordrag = dragSel
			} else if onEnd {
				m.cursordrag = dragEndSel
			}
			m.updateSelection(false)
		}
	}
	m.redraw()
}

// DragUpdate is window_copy_drag_update with the mouse at x, y of the pane (ok false when it
// is outside): it reports whether the drag timer runs, to scroll at the view's edge.
func (m *Mode) DragUpdate(x, y int, ok bool) bool {
	if !ok {
		return false
	}
	oldcx, oldcy := m.cx, m.cy
	m.updateCursor(x, y)
	m.updateSelection(false)
	timer := false
	if oldcy != m.cy || oldcx == m.cx {
		switch y {
		case 0:
			timer = true
			m.cursorUp(true)
		case m.sy() - 1:
			timer = true
			m.cursorDown(true)
		}
	}
	m.redraw()
	return timer
}

// DragRelease is window_copy_drag_release.
func (m *Mode) DragRelease() { m.cursordrag = dragNone }

// ScrollTimer is window_copy_scroll_timer: it reports whether to run again.
func (m *Mode) ScrollTimer() bool {
	again := false
	switch m.cy {
	case 0:
		again = true
		m.cursorUp(true)
	case m.sy() - 1:
		again = true
		m.cursorDown(true)
	}
	m.redraw()
	return again
}

// Commands.

// Call is a copy-mode command (send -X) and what it runs with.
type Call struct {
	Args   []string
	Prefix int // -N, 1 by default
	// Mouse is set for a command run by a mouse key that is not the wheel: the mouse's place
	// in the pane (MouseOK false when outside it), and Last the press a drag began with.
	Mouse          bool
	MouseX, MouseY int
	MouseOK        bool
	LastX, LastY   int
	LastOK         bool
	Dragging       bool   // the client is dragging (mouse_drag_flag)
	Separators     string // the session's word-separators
	// StartDrag makes the client's drags go to the mode (begin-selection with the mouse).
	StartDrag func()
}

// Command is window_copy_command for the commands in the subset: what is left to do is
// returned; unknown commands move nothing.
func (m *Mode) Command(c Call) Action {
	if len(c.Args) == 0 {
		return ActionNothing
	}
	if c.Mouse && c.MouseOK {
		m.MoveMouse(c.MouseX, c.MouseY)
	}
	np := max(c.Prefix, 1)
	action := ActionMove
	switch name := c.Args[0]; name {
	case "cancel":
		action = ActionCancel
	case "begin-selection":
		if c.Mouse {
			if c.LastOK && c.StartDrag != nil {
				c.StartDrag()
				m.StartDrag(c.LastX, c.LastY)
			}
			action = ActionMove
			break
		}
		m.lineflag, m.selflag = lineSelNone, selChar
		m.startSelection()
		action = ActionRedraw
	case "clear-selection":
		m.clearSelection()
		action = ActionRedraw
	case "scroll-up":
		action = m.cmdScrollUp(np, c.Dragging)
	case "scroll-down":
		action = m.cmdScrollDown(np, c.Dragging, false)
	case "scroll-down-and-cancel":
		action = m.cmdScrollDown(np, c.Dragging, true)
	case "page-up", "halfpage-up":
		for range np {
			m.pageUp(name == "halfpage-up")
		}
		action = ActionNothing
	case "page-down", "halfpage-down", "page-down-and-cancel", "halfpage-down-and-cancel":
		half := strings.HasPrefix(name, "half")
		exit := m.scrollExit || strings.HasSuffix(name, "-and-cancel")
		for range np {
			if m.pageDown(half, exit) {
				return ActionCancel
			}
		}
		action = ActionNothing
	case "history-top":
		oy := m.hsize() + m.cy - m.oy
		if m.lineflag == lineSelLeftRight && oy == m.sely {
			m.otherEnd()
		}
		m.cy, m.cx, m.oy = 0, 0, m.hsize()
		m.updateSelection(false)
		action = ActionRedraw
	case "history-bottom":
		oy := m.hsize() + m.cy - m.oy
		if m.lineflag == lineSelRightLeft && oy == m.endsely {
			m.otherEnd()
		}
		m.cy = m.sy() - 1
		m.cx = m.cursorLimit(m.hsize()+m.cy, false)
		m.oy = 0
		m.updateSelection(false)
		action = ActionRedraw
	case "cursor-up":
		for range np {
			m.cursorUp(false)
		}
	case "cursor-down":
		for range np {
			m.cursorDown(false)
		}
	case "cursor-left":
		for range np {
			m.cursorLeft()
		}
	case "cursor-right":
		for range np {
			m.cursorRight()
		}
	case "start-of-line":
		m.startOfLine()
	case "end-of-line":
		m.endOfLine()
	case "top-line", "middle-line", "bottom-line":
		m.cx = 0
		switch name {
		case "top-line":
			m.cy = 0
		case "middle-line":
			m.cy = (m.sy() - 1) / 2
		default:
			m.cy = m.sy() - 1
		}
		m.updateSelection(false)
		action = ActionRedraw
	case "select-word":
		m.selectWord(c.Separators)
		action = ActionRedraw
	case "select-line":
		m.selectLine(np)
		action = ActionRedraw
	case "copy-selection", "copy-selection-no-clear", "copy-selection-and-cancel":
		paste, clip, rest := copyFlags(c.Args[1:])
		prefix := ""
		if len(rest) > 0 {
			prefix = m.env.Expand(m.pane, rest[0])
		}
		m.copy(prefix, paste, clip)
		action = m.afterCopy(name)
	case "copy-pipe", "copy-pipe-no-clear", "copy-pipe-and-cancel":
		// A command to pipe the selection to is not run (not in i-mux's contract).
		paste, clip, rest := copyFlags(c.Args[1:])
		prefix := ""
		if len(rest) > 1 {
			prefix = m.env.Expand(m.pane, rest[1])
		}
		m.copy(prefix, paste, clip)
		action = m.afterCopy(name)
	}
	switch action {
	case ActionRedraw, ActionMove:
		m.redraw()
	}
	return action
}

// copyFlags parses a copy command's -P (no paste buffer) and -C (no clipboard).
func copyFlags(args []string) (paste, clip bool, rest []string) {
	paste, clip = true, true
	for len(args) > 0 && strings.HasPrefix(args[0], "-") && args[0] != "-" {
		for _, f := range args[0][1:] {
			switch f {
			case 'P':
				paste = false
			case 'C':
				clip = false
			}
		}
		args = args[1:]
	}
	return paste, clip, args
}

// afterCopy is how the copy commands end: -no-clear keeps the selection, the others clear
// it, -and-cancel leaves copy mode.
func (m *Mode) afterCopy(name string) Action {
	switch {
	case strings.HasSuffix(name, "-no-clear"):
		return ActionNothing
	case strings.HasSuffix(name, "-and-cancel"):
		m.clearSelection()
		return ActionCancel
	default:
		m.clearSelection()
		return ActionRedraw
	}
}

// copy is window_copy_copy_selection.
func (m *Mode) copy(prefix string, paste, clip bool) {
	if buf := m.selection(); buf != nil {
		m.env.Copy(m.pane, prefix, buf, paste, clip)
	}
}

// cmdScrollUp is window_copy_cmd_scroll_up.
func (m *Mode) cmdScrollUp(np int, dragging bool) Action {
	if m.oy == m.hsize() {
		return ActionNothing
	}
	if m.sel != nil && !dragging {
		m.cursordrag, m.lineflag = dragNone, lineSelNone
		m.scrollDown(np)
		m.redraw()
		return ActionNothing
	}
	for range np {
		m.cursorUp(true)
	}
	return ActionMove
}

// cmdScrollDown is window_copy_cmd_scroll_down and _and_cancel.
func (m *Mode) cmdScrollDown(np int, dragging, cancel bool) Action {
	if !cancel && m.oy == 0 {
		if m.scrollExit && m.sel == nil {
			return ActionCancel
		}
		return ActionNothing
	}
	if m.sel != nil && !dragging {
		m.cursordrag, m.lineflag = dragNone, lineSelNone
		m.scrollUp(np)
		m.redraw()
		return ActionNothing
	}
	for range np {
		m.cursorDown(true)
	}
	if cancel && m.oy == 0 {
		return ActionCancel
	}
	if !cancel && m.scrollExit && m.oy == 0 && m.sel == nil {
		return ActionCancel
	}
	return ActionMove
}

// pageUp is window_copy_pageup1.
func (m *Mode) pageUp(half bool) {
	oy := m.hsize() + m.cy - m.oy
	if ox := m.lineLength(oy); m.cx != ox {
		m.lastcx, m.lastsx = m.cx, ox
	}
	m.cx = m.lastcx
	n := m.pageSize(half)
	if m.oy+n > m.hsize() {
		m.oy = m.hsize()
		m.cy = max(m.cy-n, 0)
	} else {
		m.oy += n
	}
	m.pageEnd()
}

// pageDown is window_copy_pagedown1: it reports whether copy mode ends (scrollExit at the
// bottom with no selection).
func (m *Mode) pageDown(half, scrollExit bool) bool {
	oy := m.hsize() + m.cy - m.oy
	if ox := m.lineLength(oy); m.cx != ox {
		m.lastcx, m.lastsx = m.cx, ox
	}
	m.cx = m.lastcx
	n := m.pageSize(half)
	if m.oy < n {
		m.oy = 0
		// As tmux: oy is 0 here, so the cursor moves down n lines.
		if m.cy+n >= m.backing.SizeY() {
			m.cy = m.backing.SizeY() - 1
		} else {
			m.cy += n
		}
	} else {
		m.oy -= n
	}
	m.pageEnd()
	if scrollExit && m.oy == 0 && m.sel == nil {
		return true
	}
	return false
}

func (m *Mode) pageSize(half bool) int {
	sy := m.sy()
	switch {
	case sy <= 2:
		return 1
	case half:
		return sy / 2
	default:
		return sy - 2
	}
}

// pageEnd is the end of a page move: the cursor kept within its line, the selection and the
// view updated.
func (m *Mode) pageEnd() {
	py := m.hsize() + m.cy - m.oy
	px := m.lineLength(py)
	if (m.cx >= m.lastsx && m.cx != px) || m.cx > px {
		m.endOfLine()
	}
	m.updateSelection(false)
	m.redraw()
}

// PageUp and PageDown are copy-mode -u and -d (window_copy_pageup and _pagedown); PageDown
// reports whether copy mode ends.
func (m *Mode) PageUp() { m.pageUp(false) }

func (m *Mode) PageDown(scrollExit bool) bool { return m.pageDown(false, scrollExit) }

// selectWord is window_copy_cmd_select_word.
func (m *Mode) selectWord(separators string) {
	m.lineflag = lineSelLeftRight
	m.selflag = selWord
	m.dx = m.cx
	m.dy = m.hsize() + m.cy - m.oy
	m.separators = separators
	m.previousWord(separators, false)
	px := m.cx
	py := m.hsize() + m.cy - m.oy
	m.selrx, m.selry = px, py
	m.startSelection()
	nextx, nexty := px+1, py
	if m.backing.Grid.Line(nexty).Flags&vt.LineWrapped != 0 && nextx > m.backing.SizeX()-1 {
		nextx, nexty = 0, nexty+1
	}
	if px >= m.lineLength(py) || !inSet(m.backing.Grid, nextx, nexty, whitespace) {
		m.nextWordEnd(separators, true)
	} else {
		m.updateCursor(px, m.cy)
		m.updateSelectionNoReset()
	}
	m.endselrx = m.cx
	m.endselry = m.hsize() + m.cy - m.oy
	if m.dy > m.endselry {
		m.dy, m.dx = m.endselry, m.endselrx
	} else if m.dx > m.endselrx {
		m.dx = m.endselrx
	}
}

// selectLine is window_copy_cmd_select_line.
func (m *Mode) selectLine(np int) {
	m.lineflag = lineSelLeftRight
	m.selflag = selLine
	m.dx = m.cx
	m.dy = m.hsize() + m.cy - m.oy
	m.startOfLine()
	m.selrx = m.cx
	m.selry = m.hsize() + m.cy - m.oy
	m.endselry = m.selry
	m.startSelection()
	m.endOfLine()
	m.endselry = m.hsize() + m.cy - m.oy
	m.endselrx = m.lineLength(m.endselry)
	for ; np > 1; np-- {
		m.cursorDown(false)
		m.endOfLine()
	}
}
