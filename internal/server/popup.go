// Ported in part from tmux 3.8's popup.c:
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

package server

import (
	"errors"
	"io/fs"
	"os/exec"
	"sync"
	"sync/atomic"
	"syscall"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/commands"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/format"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/input"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/keys"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/options"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/pty"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/render"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/style"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/vt"
)

// Popups (TASKS.md C4.6): a program in its own PTY and screen, drawn over a client, which
// gets the client's keys while it is open.

// Popup drag states (popup_data's dragging).
const (
	dragOff = iota
	dragMove
	dragSize
)

// popup is tmux's popup_data.
type popup struct {
	c     *attachedClient
	flags int
	title string
	// style and borderStyle are -s and -S, nil when not given.
	style, borderStyle *string
	lines              int

	// The position and size, and the ones asked for (kept through client resizes).
	px, py, sx, sy     int
	ppx, ppy, psx, psy int

	mu      sync.Mutex // guards term
	term    *vt.Terminal
	proc    pty.Proc // nil once the program has ended
	status  int
	done    func(status int)
	gone    bool // closed: what its program still does is ignored
	pending atomic.Bool

	dragging     int
	dx, dy       int
	lx, ly, lbtn int
}

// innerSize is the size of the popup's screen: inside its border, if it has one.
func (pd *popup) innerSize() (int, int) {
	if pd.lines == render.BoxNone {
		return pd.sx, pd.sy
	}
	return pd.sx - 2, pd.sy - 2
}

// clientByName is the attached client of that name, nil if none.
func (s *Server) clientByName(name string) *attachedClient {
	for _, c := range s.attached {
		if c.name == name && !c.gone {
			return c
		}
	}
	return nil
}

// Overlay implements commands.Host.
func (s *Server) Overlay(client string) (shown, isPopup bool) {
	c := s.clientByName(client)
	if c == nil || c.popup == nil {
		return false, false
	}
	return true, true
}

// ClearOverlay implements commands.Host.
func (s *Server) ClearOverlay(client string) {
	if c := s.clientByName(client); c != nil {
		s.clearOverlay(c)
	}
}

// clearOverlay is server_client_clear_overlay with popup_free_cb: the popup goes, its program
// is killed, and the command that opened it goes on with its status.
func (s *Server) clearOverlay(c *attachedClient) {
	pd := c.popup
	if pd == nil {
		return
	}
	c.popup = nil
	pd.gone = true
	if pd.proc != nil {
		_ = pd.proc.Kill()
	}
	if pd.done != nil {
		pd.done(pd.status)
	}
	if c.session != nil {
		s.updateFocus(c.window())
	}
	s.markDirty(c)
}

// popupWindowOptions are the options popup-style and the like are read from: the session's
// current window's.
func popupWindowOptions(c *attachedClient) *options.Options {
	if wl := c.session.Current; wl != nil {
		return wl.Window.Options
	}
	return c.session.Options
}

// DisplayPopup implements commands.Host: popup_display.
func (s *Server) DisplayPopup(spec commands.PopupSpec, done func(status int)) bool {
	c := s.clientByName(spec.Client)
	if c == nil {
		return false
	}
	o := popupWindowOptions(c)
	if spec.Session != nil && spec.Session.Current != nil {
		o = spec.Session.Current.Window.Options
	}
	lines := spec.Lines
	if lines == -1 {
		lines = int(o.GetNumber("popup-border-lines"))
	}
	jx, jy := spec.W, spec.H
	if lines == render.BoxNone {
		if jx < 1 || jy < 1 {
			return false
		}
	} else {
		if jx < 3 || jy < 3 {
			return false
		}
		jx, jy = jx-2, jy-2
	}
	if c.tty.Sx < spec.W || c.tty.Sy < spec.H {
		return false
	}

	proc := spec.Proc
	proc.Cols, proc.Rows = jx, jy
	p, err := s.opts.PTY.Start(proc)
	var ee *exec.Error
	var pe *fs.PathError
	if errors.As(err, &ee) || errors.As(err, &pe) {
		// tmux's job forks first: a program that cannot run ends at once with status 1.
		p, err = deadProc{}, nil
	}
	if err != nil {
		return false
	}
	pd := &popup{
		c: c, flags: spec.Flags, title: spec.Title, style: spec.Style, borderStyle: spec.BorderStyle,
		lines: lines, px: spec.X, py: spec.Y, sx: spec.W, sy: spec.H,
		ppx: spec.X, ppy: spec.Y, psx: spec.W, psy: spec.H,
		term: vt.NewTerminal(jx, jy, 0), proc: p, done: done,
		status: 128 + int(syscall.SIGHUP),
	}
	pd.term.ExtendedKeys = s.extendedKeys()
	c.popup = pd
	s.updateFocus(c.window())
	s.readers.Add(1)
	go s.readPopup(pd)
	s.markDirty(c)
	return true
}

// readPopup is the popup's job reader (popup_job_update_cb), until its program ends
// (popup_job_complete_cb).
func (s *Server) readPopup(pd *popup) {
	defer s.readers.Done()
	buf := make([]byte, 32<<10)
	for {
		n, err := pd.proc.Read(buf)
		if n > 0 {
			pd.mu.Lock()
			_, _ = pd.term.Write(buf[:n])
			replies := pd.term.DrainReplies()
			pd.mu.Unlock()
			if len(replies) > 0 {
				_, _ = pd.proc.Write(replies)
			}
			if !pd.pending.Swap(true) {
				s.post(func() {
					pd.pending.Store(false)
					if !pd.gone {
						s.markDirty(pd.c)
					}
				})
			}
		}
		if err != nil {
			break
		}
	}
	st, _ := pd.proc.Wait()
	s.post(func() { s.popupExited(pd, st) })
}

// popupExited is popup_job_complete_cb: the program's status is kept, and with -E (or -EE and
// status 0) the popup closes.
func (s *Server) popupExited(pd *popup, st pty.ExitStatus) {
	_ = pd.proc.Close()
	switch {
	case st.Signal != 0:
		pd.status = st.Signal
	default:
		pd.status = st.Code
	}
	pd.proc = nil
	if pd.gone {
		return
	}
	if pd.flags&commands.PopupCloseExit != 0 || (pd.flags&commands.PopupCloseExitZero != 0 && pd.status == 0) {
		s.clearOverlay(pd.c)
	}
}

// ModifyPopup implements commands.Host: popup_modify.
func (s *Server) ModifyPopup(client string, m commands.PopupChange) {
	c := s.clientByName(client)
	if c == nil || c.popup == nil {
		return
	}
	pd := c.popup
	if m.Title != nil {
		pd.title = *m.Title
	}
	if m.BorderStyle != nil {
		pd.borderStyle = m.BorderStyle
	}
	if m.Style != nil {
		pd.style = m.Style
	}
	if m.Lines != -1 {
		was := pd.lines
		pd.lines = m.Lines
		if (m.Lines == render.BoxNone) != (was == render.BoxNone) {
			s.resizePopupScreen(pd)
		}
	}
	if m.Flags != -1 {
		pd.flags = m.Flags
	}
	s.markDirty(c)
}

// resizePopupScreen makes the popup's screen and program fit its size and border.
func (s *Server) resizePopupScreen(pd *popup) {
	jx, jy := pd.innerSize()
	if jx < 1 || jy < 1 {
		return
	}
	pd.mu.Lock()
	pd.term.Resize(jx, jy)
	pd.mu.Unlock()
	if pd.proc != nil {
		_ = pd.proc.Resize(jx, jy)
	}
}

// popupResized is popup_resize_cb: after the client is resized, the popup keeps the size and
// position it was given, as far as the client allows.
func (s *Server) popupResized(c *attachedClient) {
	pd := c.popup
	if pd == nil {
		return
	}
	pd.sy = min(pd.psy, c.tty.Sy)
	pd.sx = min(pd.psx, c.tty.Sx)
	if pd.ppy+pd.sy > c.tty.Sy {
		pd.py = c.tty.Sy - pd.sy
	} else {
		pd.py = pd.ppy
	}
	if pd.ppx+pd.sx > c.tty.Sx {
		pd.px = c.tty.Sx - pd.sx
	} else {
		pd.px = pd.ppx
	}
	if pd.lines == render.BoxNone || (pd.sx > 2 && pd.sy > 2) {
		s.resizePopupScreen(pd)
	}
}

// popupView is popup_draw_cb's half that reads the options (popup_reapply_styles): the popup as
// the renderer draws it.
func (s *Server) popupView(c *attachedClient) *render.Popup {
	pd := c.popup
	o := popupWindowOptions(c)
	ctx := &format.Context{
		Tree: s.tree, Server: s.ServerInfo(), Clients: s.clientFormats(), Now: s.opts.Now,
		ItemClient: c.format,
	}
	ctx.Defaults(c.format, c.session, c.session.Current, nil)
	apply := func(name string, over *string) vt.Cell {
		gc := vt.DefaultCell
		sy := style.Default()
		if sy.Parse(vt.DefaultCell, ctx.Expand(o.GetString(name))) == nil {
			sy.Add(&gc)
		}
		if over != nil {
			tmp := style.FromCell(vt.DefaultCell)
			if tmp.Parse(gc, *over) == nil {
				gc.Fg, gc.Bg = tmp.Cell.Fg, tmp.Cell.Bg
			}
		}
		gc.Attr = 0
		return gc
	}
	return &render.Popup{
		X: pd.px, Y: pd.py, Sx: pd.sx, Sy: pd.sy, Lines: pd.lines,
		Border:   apply("popup-border-style", pd.borderStyle),
		Defaults: apply("popup-style", pd.style),
		Title:    pd.title,
		Term:     pd.term, Mu: &pd.mu,
	}
}

// popupKey is popup_key_cb: what a key does while the popup is open. It reports whether the
// popup closes.
func (s *Server) popupKey(c *attachedClient, ev *input.Event) bool {
	pd := c.popup
	key := ev.Key
	m := ev.Mouse
	out := func() bool {
		pd.lx, pd.ly, pd.lbtn = m.X, m.Y, m.B
		return false
	}
	if key.IsMouse() {
		if pd.dragging != dragOff {
			s.popupDrag(c, pd, m)
			return out()
		}
		if m.X < pd.px || m.X > pd.px+pd.sx-1 || m.Y < pd.py || m.Y > pd.py+pd.sy-1 {
			return false
		}
		const (
			none = iota
			left
			right
			top
			bottom
		)
		border := none
		if pd.lines != render.BoxNone {
			switch {
			case m.X == pd.px:
				border = left
			case m.X == pd.px+pd.sx-1:
				border = right
			case m.Y == pd.py:
				border = top
			case m.Y == pd.py+pd.sy-1:
				border = bottom
			}
		}
		if m.B&keys.MouseMaskModifiers == 0 && m.B&keys.MouseMaskButtons == keys.MouseButton3 &&
			(border == left || border == top) {
			return out()
		}
		if m.B&keys.MouseMaskModifiers == keys.MouseMaskMeta || (border != none && m.LB&keys.MouseMaskDrag == 0) {
			if m.B&keys.MouseMaskDrag == 0 {
				return out()
			}
			switch m.LB & keys.MouseMaskButtons {
			case keys.MouseButton1:
				pd.dragging = dragMove
			case keys.MouseButton3:
				pd.dragging = dragSize
			}
			pd.dx, pd.dy = m.LX-pd.px, m.LY-pd.py
			return out()
		}
	}
	k := key & (keys.MaskKey | keys.MaskModifiers)
	if (pd.flags&(commands.PopupCloseExit|commands.PopupCloseExitZero) == 0 || pd.proc == nil) &&
		(k == 0x1b || k == 'c'|keys.Ctrl) {
		return true
	}
	if pd.proc == nil && pd.flags&commands.PopupCloseAnyKey != 0 && !key.IsMouse() && !key.IsPaste() {
		return true
	}
	if pd.proc == nil {
		return false
	}
	pd.mu.Lock()
	mode := pd.term.Screen.Mode
	pd.mu.Unlock()
	var b []byte
	if key.IsMouse() {
		x, y := m.X-pd.px, m.Y-pd.py
		if pd.lines != render.BoxNone {
			x, y = x-1, y-1
		}
		sgr := m.SGRType
		if sgr == 0 {
			sgr = ' '
		}
		b = keys.EncodeMouse(mode, keys.MouseEvent{B: m.B, LB: m.LB, SGRType: sgr, SGRB: m.SGRB}, x, y)
	} else {
		b, _ = keys.Encode(key, mode, backspace, s.extendedFormat())
	}
	if len(b) > 0 {
		_, _ = pd.proc.Write(b)
	}
	return false
}

// popupDrag is popup_handle_drag: the popup moves with button 1, or is resized with button 3.
func (s *Server) popupDrag(c *attachedClient, pd *popup, m input.Mouse) {
	switch {
	case m.B&keys.MouseMaskDrag == 0:
		pd.dragging = dragOff
	case pd.dragging == dragMove:
		px, py := m.X-pd.dx, m.Y-pd.dy
		switch {
		case m.X < pd.dx:
			px = 0
		case m.X-pd.dx+pd.sx > c.tty.Sx:
			px = c.tty.Sx - pd.sx
		}
		switch {
		case m.Y < pd.dy:
			py = 0
		case m.Y-pd.dy+pd.sy > c.tty.Sy:
			py = c.tty.Sy - pd.sy
		}
		pd.px, pd.py = px, py
		pd.dx, pd.dy = m.X-pd.px, m.Y-pd.py
		pd.ppx, pd.ppy = px, py
		s.markDirty(c)
	case pd.dragging == dragSize:
		least := 3
		if pd.lines == render.BoxNone {
			least = 1
		}
		if m.X < pd.px+least || m.Y < pd.py+least {
			return
		}
		pd.sx, pd.sy = m.X-pd.px, m.Y-pd.py
		pd.psx, pd.psy = pd.sx, pd.sy
		s.resizePopupScreen(pd)
		s.markDirty(c)
	}
}

// ClientGeometry implements commands.Host.
func (s *Server) ClientGeometry(client string) commands.ClientGeometry {
	c := s.clientByName(client)
	if c == nil {
		return commands.ClientGeometry{StatusAt: -1}
	}
	g := commands.ClientGeometry{Sx: c.tty.Sx, Sy: c.tty.Sy, StatusAt: -1}
	lines := s.statusLineSize(c)
	g.StatusLines = lines
	g.StatusTop = c.session.Options.GetNumber("status-position") == 0
	if lines > 0 {
		if g.StatusTop {
			g.StatusAt = 0
		} else {
			g.StatusAt = c.tty.Sy - lines
		}
		for _, l := range s.statusLines(c) {
			g.Ranges = append(g.Ranges, l.Ranges)
		}
	}
	if wl := c.session.Current; wl != nil {
		g.Ox, g.Oy = render.WindowOffset(wl.Window, c.tty.Sx, c.tty.Sy-lines)
	}
	return g
}
