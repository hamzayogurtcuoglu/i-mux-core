// Mouse events become keys (TASKS.md C5.1), ported from tmux 3.8's server-client.c
// (server_client_check_mouse, server_client_check_mouse_in_pane, server_client_click_timer)
// and window.c (window_get_active_at), without the scrollbars and floating panes the engine
// does not have:
//
// Copyright (c) 2009 Nicholas Marriott <nicholas.marriott@gmail.com>
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

package server

import (
	"time"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/input"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/keys"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/model"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/render"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/style"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/target"
)

// clickTimeout is KEYC_CLICK_TIMEOUT: how soon a press must follow the last for a second or
// third click.
const clickTimeout = 300 * time.Millisecond

// Mouse buttons (tmux's MOUSE_BUTTON_* and MOUSE_WHEEL_*).
const (
	mouseButton2   = 1
	mouseButton6   = 66
	mouseButton7   = 67
	mouseButton8   = 128
	mouseButton9   = 129
	mouseButton10  = 130
	mouseButton11  = 131
	mouseMaskDrag  = keys.MouseMaskDrag
	mouseMaskBtns  = keys.MouseMaskButtons
	mouseReleaseID = 3
)

func mouseDrag(b int) bool    { return b&mouseMaskDrag != 0 }
func mouseRelease(b int) bool { return b&mouseMaskBtns == mouseReleaseID }
func mouseWheel(b int) bool {
	return b&mouseMaskBtns == mouseWheelUp || b&mouseMaskBtns == mouseWheelDown
}

// mouseState is a client's mouse state (the tty's mouse_* fields and the client's click
// flags and timer).
type mouseState struct {
	dragFlag     int // the dragging button plus one, 0 when not dragging
	dragStatus   bool
	dragX, dragY int
	lastPane     int // the pane the drag started in, -1 for none
	// dragUpdate and dragRelease follow a drag a command started (copy mode, border resizing).
	dragUpdate  func(c *attachedClient, m *target.Mouse)
	dragRelease func(c *attachedClient, m *target.Mouse)

	double, triple bool // CLIENT_DOUBLECLICK and CLIENT_TRIPLECLICK
	clickTimer     *time.Timer
	clickEvent     target.Mouse
	clickButton    int
	clickLoc       int
	clickPane      int
}

// statusAt is status_at_line: the client's first status line, -1 with none.
func (s *Server) statusAt(c *attachedClient) int {
	if c.session.Options.GetNumber("status") == 0 {
		return -1
	}
	if c.session.Options.GetNumber("status-position") == 0 {
		return 0
	}
	return c.tty.Sy - s.statusLineSize(c)
}

// mouseEvent turns an input mouse report into the event the mouse code works on.
func mouseEvent(m input.Mouse) target.Mouse {
	return target.Mouse{
		X: m.X, Y: m.Y, B: m.B, LX: m.LX, LY: m.LY, LB: m.LB,
		SGRType: m.SGRType, SGRB: m.SGRB, S: -1, W: -1, P: -1,
	}
}

// paneAt is window_get_active_at for tiled panes: the visible pane at x, y in the window,
// up to and including its right and bottom borders.
func paneAt(w *model.Window, x, y int) *model.Pane {
	top := w.Options.GetNumber("pane-border-status") == 1
	visible := func(p *model.Pane) bool {
		if w.Layout == nil {
			return true
		}
		if z := w.Layout.Zoomed(); z >= 0 {
			return p.ID == z
		}
		return true
	}
	if top {
		// A pane's top status line wins over the bottom border of the pane above.
		for _, p := range w.Panes {
			if !visible(p) || x < p.Xoff || x > p.Xoff+p.Sx {
				continue
			}
			if y == p.Yoff-1 {
				return p
			}
		}
	}
	for _, p := range w.Panes {
		if !visible(p) || x < p.Xoff || x > p.Xoff+p.Sx {
			continue
		}
		if top {
			if y < p.Yoff-1 || y > p.Yoff+p.Sy {
				continue
			}
		} else if y < p.Yoff || y > p.Yoff+p.Sy {
			continue
		}
		return p
	}
	return nil
}

// mouseInPane is server_client_check_mouse_in_pane: whether px, py is in pane p or on a
// border.
func mouseInPane(p *model.Pane, px, py int) int {
	w := p.Window
	status := w.Options.GetNumber("pane-border-status")
	statusLine := -1
	switch status {
	case 1:
		statusLine = p.Yoff - 1
	case 2:
		statusLine = p.Yoff + p.Sy
	}
	inY := (status != 0 && py != statusLine && py != p.Yoff+p.Sy) ||
		(p.Yoff == 0 && py < p.Sy) || (py >= p.Yoff && py < p.Yoff+p.Sy)
	if inY && px < p.Xoff+p.Sx {
		return keys.LocationPane
	}
	for _, f := range w.Panes {
		if w.Layout != nil {
			if z := w.Layout.Zoomed(); z >= 0 && f.ID != z {
				continue
			}
		}
		right := f.Xoff + f.Sx
		if py >= f.Yoff-1 && py <= f.Yoff+f.Sy && px == right {
			return keys.LocationBorder
		}
		if px >= f.Xoff-1 && px <= f.Xoff+f.Sx && (py == f.Yoff+f.Sy || py == f.Yoff-1) {
			return keys.LocationBorder
		}
	}
	return keys.LocationNowhere
}

// checkMouse is server_client_check_mouse: the key for a mouse event, filling in m with what
// it hit; keys.Unknown when the event makes no key. double is set for the double click the
// click timer makes.
func (s *Server) checkMouse(c *attachedClient, m *target.Mouse, double bool) keys.Key {
	ms := &c.mouse
	sess := c.session
	w := sess.Current.Window
	var lwp *model.Pane
	if ms.lastPane != -1 {
		lwp = s.tree.Pane(ms.lastPane)
	}

	typ := keys.TypeNoType
	var x, y, b int
	ignore := false
	switch {
	case double:
		typ = keys.TypeDoubleClick
		x, y, b = m.X, m.Y, m.B
		ignore = true
	case (m.SGRType != ' ' && mouseDrag(m.SGRB) && mouseRelease(m.SGRB)) ||
		(m.SGRType == ' ' && mouseDrag(m.B) && mouseRelease(m.B) && mouseRelease(m.LB)):
		typ = keys.TypeMouseMove
		x, y, b = m.X, m.Y, 0
	case mouseDrag(m.B):
		typ = keys.TypeMouseDrag
		if ms.dragFlag != 0 {
			x, y, b = m.X, m.Y, m.B
			if x == m.LX && y == m.LY {
				return keys.Unknown
			}
			if ms.dragStatus {
				x, y = ms.dragX, ms.dragY
			}
		} else {
			x, y, b = m.LX, m.LY, m.LB
			ms.dragStatus = false
		}
	case mouseWheel(m.B):
		typ = keys.TypeWheelDown
		if m.B&mouseMaskBtns == mouseWheelUp {
			typ = keys.TypeWheelUp
		}
		x, y, b = m.X, m.Y, m.B
	case mouseRelease(m.B):
		typ = keys.TypeMouseUp
		x, y, b = m.X, m.Y, m.LB
		if m.SGRType == 'm' {
			b = m.SGRB
		}
	default:
		switch {
		case ms.double:
			s.stopClickTimer(c)
			ms.double = false
			typ = keys.TypeSecondClick
			x, y, b = m.X, m.Y, m.B
			ms.triple = true
		case ms.triple:
			s.stopClickTimer(c)
			ms.triple = false
			typ = keys.TypeTripleClick
			x, y, b = m.X, m.Y, m.B
		}
		if typ == keys.TypeNoType {
			typ = keys.TypeMouseDown
			x, y, b = m.X, m.Y, m.B
			ms.double = true
		}
	}

	m.S, m.W, m.P = sess.ID, -1, -1
	m.Ignore = ignore
	loc := keys.LocationNowhere

	// The status line.
	lines := s.statusLineSize(c)
	m.StatusAt, m.StatusLines = s.statusAt(c), lines
	m.HasStatusRange = false
	if m.StatusAt != -1 && y >= m.StatusAt && y < m.StatusAt+lines {
		if typ == keys.TypeMouseDrag && ms.dragFlag == 0 {
			ms.dragStatus = true
		}
		line := y - m.StatusAt
		var r *rangeHit
		if line < len(c.statusLines) {
			if sr := c.statusLines[line].RangeAt(x); sr != nil {
				r = &rangeHit{typ: sr.Type, arg: int(sr.Arg), str: sr.Str}
			}
		}
		if r == nil {
			loc = keys.LocationStatusDefault
		} else {
			m.StatusRange, m.HasStatusRange = r.name(), r.typ != style.RangeNone
			switch r.typ {
			case style.RangeNone:
				return keys.Unknown
			case style.RangeLeft:
				loc = keys.LocationStatusLeft
			case style.RangeRight:
				loc = keys.LocationStatusRight
			case style.RangePane:
				if s.tree.Pane(r.arg) == nil {
					return keys.Unknown
				}
				m.P = r.arg
				loc = keys.LocationStatus
			case style.RangeWindow:
				wl := sess.Link(r.arg)
				if wl == nil {
					return keys.Unknown
				}
				m.W = wl.Window.ID
				loc = keys.LocationStatus
			case style.RangeSession:
				if s.tree.Session(r.arg) == nil {
					return keys.Unknown
				}
				m.S = r.arg
				loc = keys.LocationStatus
			case style.RangeUser:
				loc = keys.LocationStatus
			case style.RangeControl:
				loc = keys.LocationControl0 + r.arg
			}
		}
	}

	// Not on the status line: a pane, a border or the empty area.
	var wp *model.Pane
	px, py := 0, 0
	if loc == keys.LocationNowhere {
		px = x
		switch {
		case m.StatusAt == 0 && y >= lines:
			py = y - lines
		case m.StatusAt > 0 && y >= m.StatusAt:
			py = m.StatusAt - 1
		default:
			py = y
		}
		ox, oy, sx, sy := render.WindowArea(w, c.tty.Sx, c.tty.Sy-lines)
		m.OX, m.OY = ox, oy
		if px > sx || py > sy {
			return keys.Unknown
		}
		px += ox
		py += oy
		if typ == keys.TypeMouseDrag && lwp != nil {
			wp = lwp
		} else {
			wp = paneAt(w, px, py)
		}
		if wp == nil {
			loc = keys.LocationEmpty
			m.W = w.ID
		} else {
			loc = mouseInPane(wp, px, py)
			m.P = wp.ID
			m.W = wp.Window.ID
		}
	}

	// Reset the click sequence, or start the click timer.
	if typ == keys.TypeMouseDown || typ == keys.TypeSecondClick || typ == keys.TypeTripleClick {
		if typ != keys.TypeMouseDown && (m.B != ms.clickButton || loc != ms.clickLoc || m.P != ms.clickPane) {
			typ = keys.TypeMouseDown
			ms.triple = false
			ms.double = true
		}
		if typ != keys.TypeTripleClick {
			ms.clickEvent = *m
			ms.clickButton = m.B
			ms.clickLoc = loc
			ms.clickPane = m.P
			s.startClickTimer(c)
		}
	}

	key := keys.Unknown

	// Stop dragging.
	if typ != keys.TypeMouseDrag && typ != keys.TypeWheelUp && typ != keys.TypeWheelDown &&
		typ != keys.TypeDoubleClick && typ != keys.TypeTripleClick && ms.dragFlag != 0 {
		if ms.dragRelease != nil {
			ms.dragRelease(c, m)
		}
		ms.dragUpdate, ms.dragRelease = nil, nil
		// The drag ends with MouseDragEnd for the button that started it.
		typ = keys.TypeMouseDragEnd
		ms.dragFlag = 0
		ms.lastPane = -1
	}

	if typ == keys.TypeMouseMove && loc == keys.LocationPane {
		key = keys.MouseKey(keys.TypeMouseMove, 0, keys.LocationPane)
		if wp != nil && wp != w.Active && sess.Options.GetNumber("focus-follows-mouse") != 0 {
			s.focusFollowsMouse(w, wp)
		}
	}
	if typ == keys.TypeMouseDrag {
		if ms.dragUpdate != nil {
			key = keys.Dragging
		}
		if ms.dragFlag == 0 {
			if ms.dragStatus {
				ms.dragX, ms.dragY = x, y
			} else {
				ms.dragX, ms.dragY = px, py
			}
		}
		ms.dragFlag = b&mouseMaskBtns + 1
		if !ms.dragStatus && lwp == nil {
			if p := paneAt(w, px, py); p != nil {
				ms.lastPane = p.ID
			}
		}
	}

	if key == keys.Unknown {
		bn := 0
		switch b & mouseMaskBtns {
		case keys.MouseButton1:
			bn = 1
		case mouseButton2:
			bn = 2
		case keys.MouseButton3:
			bn = 3
		case mouseButton6:
			bn = 6
		case mouseButton7:
			bn = 7
		case mouseButton8:
			bn = 8
		case mouseButton9:
			bn = 9
		case mouseButton10:
			bn = 10
		case mouseButton11:
			bn = 11
		}
		key = keys.MouseKey(typ, bn, loc)
	}
	if b&keys.MouseMaskMeta != 0 {
		key |= keys.Meta
	}
	if b&keys.MouseMaskCtrl != 0 {
		key |= keys.Ctrl
	}
	if b&keys.MouseMaskShift != 0 {
		key |= keys.Shift
	}
	return key
}

// rangeHit is the status range under the mouse.
type rangeHit struct {
	typ style.RangeType
	arg int
	str string
}

// name is the range as #{mouse_status_range} gives it (format_cb_mouse_status_range).
func (r *rangeHit) name() string {
	switch r.typ {
	case style.RangeLeft:
		return "left"
	case style.RangeRight:
		return "right"
	case style.RangePane:
		return "pane"
	case style.RangeWindow:
		return "window"
	case style.RangeSession:
		return "session"
	case style.RangeUser:
		return r.str
	case style.RangeControl:
		return "control"
	}
	return ""
}

// startClickTimer is the click timer (server_client_click_timer): with no further press in
// time, a client waiting for a third click gets a double click.
func (s *Server) startClickTimer(c *attachedClient) {
	s.stopClickTimer(c)
	var t *time.Timer
	t = time.AfterFunc(clickTimeout, func() {
		s.post(func() {
			if c.mouse.clickTimer != t || c.gone {
				return
			}
			c.mouse.clickTimer = nil
			if c.mouse.triple {
				m := c.mouse.clickEvent
				s.mouseKey(c, &m, true)
			}
			c.mouse.double, c.mouse.triple = false, false
		})
	})
	c.mouse.clickTimer = t
}

func (s *Server) stopClickTimer(c *attachedClient) {
	if c.mouse.clickTimer != nil {
		c.mouse.clickTimer.Stop()
		c.mouse.clickTimer = nil
	}
}

// focusFollowsMouse makes the pane under the mouse active (focus-follows-mouse).
func (s *Server) focusFollowsMouse(w *model.Window, p *model.Pane) {
	w.SetActivePane(p)
	s.updateFocus(w)
	s.redrawAll()
}

// mouseKey is server_client_key_callback for a mouse report: the key it makes runs its
// binding, with the event, from the pane under the mouse.
func (s *Server) mouseKey(c *attachedClient, m *target.Mouse, double bool) {
	key := s.checkMouse(c, m, double)
	if key == keys.Unknown {
		return
	}
	m.Valid, m.Key = true, key
	if key&keys.MaskKey == keys.Dragging {
		if c.mouse.dragUpdate != nil {
			c.mouse.dragUpdate(c, m)
		}
		return
	}
	// Mouse keys with the mouse option off go to the pane (task C5.3).
	if c.session.Options.GetNumber("mouse") == 0 {
		return
	}
	s.dispatch(c, key, m)
}
