package server

import (
	"errors"
	"time"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/commands"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/copymode"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/format"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/model"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/style"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/target"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/vt"
)

// Copy mode (TASKS.md C4.8): the server is copymode's Env, puts panes in and out of the mode,
// and runs its commands with the client's mouse and drag.

// errNotInMode is send -X's error for a pane in no mode.
var errNotInMode = errors.New("not in a mode")

// dragRepeat is WINDOW_COPY_DRAG_REPEAT_TIME: how often a drag at the view's edge scrolls.
const dragRepeat = 50 * time.Millisecond

// copyEnv is copymode.Env for the server.
type copyEnv struct{ s *Server }

func (e copyEnv) context(p *model.Pane) *format.Context {
	s := e.s
	ctx := &format.Context{Tree: s.tree, Server: s.ServerInfo(), Clients: s.clientFormats(), Now: s.opts.Now}
	ctx.Defaults(nil, nil, nil, p)
	ctx.Buffer = s.buffers.Top()
	return ctx
}

// Style is style_apply for a window option of the pane.
func (e copyEnv) Style(p *model.Pane, option string) vt.Cell {
	ctx := e.context(p)
	gc := vt.DefaultCell
	sy := style.Default()
	if sy.Parse(vt.DefaultCell, ctx.Expand(p.Window.Options.GetString(option))) == nil {
		sy.Add(&gc)
	}
	return gc
}

// Expand is format_expand for the pane.
func (e copyEnv) Expand(p *model.Pane, str string) string { return e.context(p).Expand(str) }

// Copy is window_copy_copy_buffer: to the clipboard of the clients showing the pane when
// set-clipboard is on or external, and to a paste buffer.
func (e copyEnv) Copy(p *model.Pane, prefix string, data []byte, paste, clip bool) {
	s := e.s
	if clip && s.tree.Options.Server.GetNumber("set-clipboard") != 0 {
		for _, c := range s.attached {
			if c.gone {
				continue
			}
			if wl := c.session.Current; wl != nil && wl.Window == p.Window && paneShown(p) {
				c.tty.SetSelection("", data)
				s.send(c)
			}
		}
		s.firePane("pane-set-clipboard", p)
	}
	if paste {
		s.buffers.Add(prefix, data, int(s.tree.Options.Server.GetNumber("buffer-limit")))
	}
}

// copyModeOf is the pane's copy mode, or nil.
func copyModeOf(p *model.Pane) *copymode.Mode {
	if p == nil {
		return nil
	}
	m, _ := p.Mode.(*copymode.Mode)
	return m
}

// CopyMode implements commands.Host: copy-mode (cmd_copy_mode_exec and window_pane_set_mode).
func (s *Server) CopyMode(p *model.Pane, spec commands.CopyModeSpec) {
	if spec.Cancel {
		s.resetMode(p)
		return
	}
	m := copyModeOf(p)
	entered := false
	if m == nil && p.Term != nil {
		m = copymode.New(p, copyEnv{s}, spec.HidePosition, spec.ScrollExit)
		p.Mode = m
		// window_pane_set_mode sets PANE_CHANGED: automatic-rename looks at the pane again.
		p.Changed = true
		m.Redraw()
		entered = true
	}
	if m == nil {
		return
	}
	if entered && spec.Mouse != nil {
		if c := s.clientByName(spec.Client); c != nil {
			if x, y, ok := target.MouseAt(p, spec.Mouse, true); ok {
				s.startCopyDrag(c)
				m.StartDrag(x, y)
				s.copyDragUpdate(c, spec.Mouse)
			}
		}
	}
	if spec.PageUp {
		m.PageUp()
	}
	if spec.PageDown && m.PageDown(spec.ScrollExit) {
		s.resetMode(p)
	}
	s.modeChanged(p)
}

// resetMode is window_pane_reset_mode: the pane shows its program again.
func (s *Server) resetMode(p *model.Pane) {
	if p == nil || p.Mode == nil {
		return
	}
	p.Mode = nil
	p.Changed = true // window_pane_reset_mode's PANE_CHANGED
	s.modeChanged(p)
}

// modeChanged redraws what shows the pane.
func (s *Server) modeChanged(*model.Pane) { s.redrawAll() }

// CopyCommand implements commands.Host: send -X, window_copy_command for the client's key.
func (s *Server) CopyCommand(client string, p *model.Pane, call copymode.Call, mouse *target.Mouse) error {
	m := copyModeOf(p)
	if m == nil {
		return errNotInMode
	}
	c := s.clientByName(client)
	started := false
	if c != nil {
		call.Dragging = c.mouse.dragFlag != 0
		call.StartDrag = func() { s.startCopyDrag(c); started = true }
	}
	if mouse != nil && !mouseWheel(mouse.B) {
		call.Mouse = true
		call.MouseX, call.MouseY, call.MouseOK = target.MouseAt(p, mouse, false)
		call.LastX, call.LastY, call.LastOK = target.MouseAt(p, mouse, true)
	}
	if c == nil {
		call.StartDrag = nil
	}
	action := m.Command(call)
	if action == copymode.ActionCancel && p.Mode == m {
		s.resetMode(p)
		return nil
	}
	if started && mouse != nil {
		// begin-selection with the mouse began a drag (window_copy_start_drag ends with an
		// update for the event itself).
		s.copyDragUpdate(c, mouse)
	}
	s.modeChanged(p)
	return nil
}

// startCopyDrag makes the client's drags go to copy mode (window_copy_start_drag's
// mouse_drag_update and mouse_drag_release).
func (s *Server) startCopyDrag(c *attachedClient) {
	c.mouse.dragUpdate = s.copyDragUpdate
	c.mouse.dragRelease = s.copyDragRelease
}

// copyDragUpdate is window_copy_drag_update for the pane under the mouse.
func (s *Server) copyDragUpdate(c *attachedClient, m *target.Mouse) {
	_, _, p := target.MousePane(s.tree, m)
	cm := copyModeOf(p)
	if cm == nil {
		return
	}
	s.stopDragTimer(c)
	x, y, ok := target.MouseAt(p, m, false)
	if cm.DragUpdate(x, y, ok) {
		s.armDragTimer(c, p, cm)
	}
	s.modeChanged(p)
}

// copyDragRelease is window_copy_drag_release.
func (s *Server) copyDragRelease(c *attachedClient, m *target.Mouse) {
	_, _, p := target.MousePane(s.tree, m)
	if cm := copyModeOf(p); cm != nil {
		cm.DragRelease()
	}
	s.stopDragTimer(c)
}

// armDragTimer is the copy mode's drag timer (window_copy_scroll_timer): while the drag holds
// the cursor at the view's edge, it scrolls on.
func (s *Server) armDragTimer(c *attachedClient, p *model.Pane, cm *copymode.Mode) {
	var t *time.Timer
	t = time.AfterFunc(dragRepeat, func() {
		s.post(func() {
			if c.copyDragTimer != t || p.Mode != cm {
				return
			}
			c.copyDragTimer = nil
			if cm.ScrollTimer() {
				s.armDragTimer(c, p, cm)
			}
			s.modeChanged(p)
		})
	})
	c.copyDragTimer = t
}

func (s *Server) stopDragTimer(c *attachedClient) {
	if c.copyDragTimer != nil {
		c.copyDragTimer.Stop()
		c.copyDragTimer = nil
	}
}
