package server

import (
	"time"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/format"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/layout"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/model"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/options"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/render"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/status"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/style"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/vt"
)

// Drawing attached clients (TASKS.md C2.8, ARCHITECTURE.md §4.4): when what a client shows may
// have changed it is marked dirty, and it is drawn at most once per FrameInterval; each draw
// composes its frame and writes what differs from the last one.

// FrameInterval is the shortest time between two frames of one client.
const FrameInterval = 16 * time.Millisecond

// drawState is a client's renderer and when it was drawn (the loop's).
type drawState struct {
	r         *render.Renderer
	dirty     bool
	lastDraw  time.Time
	scheduled bool // a draw is due on a timer
}

// view is what the client shows.
func (s *Server) view(c *attachedClient) *render.View {
	v := &render.View{
		Sx: c.tty.Sx, Sy: c.tty.Sy,
		Styles:   s.borderStyles(c),
		Mouse:    c.session.Options.GetNumber("mouse") != 0,
		NoBright: c.tty.Term.Has("Nobr"),
		// The theme colours, once the terminal has told its colours or theme.
		ThemeColours: c.in.themes,
	}
	if wl := c.session.Current; wl != nil {
		v.Window = wl.Window
	}
	// The status lines are left out of the window; what they show comes with task C4.2.
	v.StatusLines = s.statusLineSize(c)
	v.StatusTop = c.session.Options.GetNumber("status-position") == 0
	v.Status = s.statusCells(c)
	if c.prompt != nil {
		// status_prompt_set: the prompt line, the panes frozen unless -C, the cursor in it.
		lines, cx := s.promptLines(c)
		v.Message = lines
		v.Freeze = c.prompt.pr.Flags&status.PromptNoFreeze == 0
		v.PromptCursor = &[2]int{cx, s.promptScreenLine(c)}
	}
	if c.message != nil {
		// status_message_set: the message line, the panes frozen unless -C, no cursor. A
		// message shows over the prompt.
		v.Message = s.messageLines(c)
		v.Freeze = !c.message.noFreeze
		v.NoCursor = true
	}
	if md := s.windowMenu(c); md != nil {
		v.Menu = s.menuView(md)
	}
	if c.popup != nil {
		v.Popup = s.popupView(c)
	}
	return v
}

// invalidate makes the client's next frame whole: its terminal was cleared or resized.
func (s *Server) invalidate(c *attachedClient) {
	c.draw.r.Invalidate()
	c.tty.Invalidate()
	s.markDirty(c)
}

// markDirty asks for the client to be drawn: now, unless it was drawn less than a frame
// interval ago, then when the interval ends.
func (s *Server) markDirty(c *attachedClient) {
	if c.gone {
		return
	}
	c.draw.dirty = true
	if c.draw.scheduled {
		return
	}
	wait := FrameInterval - s.opts.Now().Sub(c.draw.lastDraw)
	if wait <= 0 {
		s.drawClient(c)
		return
	}
	s.drawAfter(c, wait)
}

// drawAfter draws the client after d, if it is still dirty then.
func (s *Server) drawAfter(c *attachedClient, d time.Duration) {
	c.draw.scheduled = true
	time.AfterFunc(d, func() {
		s.post(func() {
			c.draw.scheduled = false
			if c.draw.dirty {
				s.drawClient(c)
			}
		})
	})
}

// drawClient draws a client's frame now.
func (s *Server) drawClient(c *attachedClient) {
	if c.gone {
		return
	}
	now := s.opts.Now()
	c.draw.dirty = false
	c.draw.lastDraw = now
	c.draw.r.Draw(c.tty, c.draw.r.Compose(s.view(c), now))
	s.send(c)
	// A pane holding a synchronized update is drawn once it times out.
	if d, ok := c.draw.r.Deadline(); ok && !c.draw.scheduled {
		c.draw.dirty = true
		s.drawAfter(c, max(d.Sub(now), FrameInterval))
	}
}

// redrawAll marks every attached client dirty: commands may have changed what any shows.
func (s *Server) redrawAll() {
	for _, c := range s.attached {
		s.markDirty(c)
	}
}

// paneChanged marks dirty the clients that show p's window.
func (s *Server) paneChanged(p *model.Pane) {
	for _, c := range s.attached {
		if wl := c.session.Current; wl != nil && wl.Window == p.Window {
			s.markDirty(c)
		}
	}
}

// borderStyles are how the window's borders are drawn, as tmux's screen_redraw does: the
// window's pane-border-style (expanded with no pane) for borders next to no pane, each pane's
// pane-border-style or pane-active-border-style and pane-border-format expanded for that pane,
// pane-border-lines and pane-border-indicators.
func (s *Server) borderStyles(c *attachedClient) render.Styles {
	st := render.DefaultStyles()
	wl := c.session.Current
	if wl == nil || wl.Window.Active == nil {
		return st
	}
	w := wl.Window
	ctx := &format.Context{
		Tree:       s.tree,
		Server:     s.ServerInfo(),
		Clients:    s.clientFormats(),
		Now:        s.opts.Now,
		ItemClient: c.format,
	}
	apply := func(oo *options.Options, name string) vt.Cell {
		gc := vt.DefaultCell
		sy := style.Default()
		if sy.Parse(vt.DefaultCell, ctx.Expand(oo.GetString(name))) == nil {
			sy.Add(&gc)
		}
		return gc
	}
	ctx.Defaults(c.format, c.session, wl, nil)
	ctx.Buffer = s.buffers.Top() // format_defaults' top paste buffer
	st.Border = apply(w.Options, "pane-border-style")
	st.Lines = int(w.Options.GetNumber("pane-border-lines"))
	ind := w.Options.GetNumber("pane-border-indicators")
	st.TwoPaneColours = ind == 1 || ind == 3 // colour, both
	st.Arrows = ind == 2 || ind == 3         // arrows, both
	borderStatus := w.BorderStatus() != layout.StatusOff
	base := int(w.Options.GetNumber("pane-base-index"))
	st.Panes = make(map[*model.Pane]render.PaneBorder, len(w.Panes))
	for i, p := range w.Panes {
		ctx.Defaults(c.format, c.session, wl, p)
		ctx.Buffer = s.buffers.Top() // format_defaults' top paste buffer
		name := "pane-border-style"
		if p == w.Active {
			name = "pane-active-border-style"
		}
		pb := render.PaneBorder{Style: apply(p.Options, name), Index: base + i}
		if borderStatus {
			pb.Status = ctx.ExpandTime(p.Options.GetString("pane-border-format"))
		}
		st.Panes[p] = pb
	}
	st.ActiveBorder = st.Panes[w.Active].Style
	return st
}

// statusCells renders the status lines for c (task C4.2).
func (s *Server) statusCells(c *attachedClient) [][]vt.Cell {
	lines := s.statusLines(c)
	out := make([][]vt.Cell, len(lines))
	for i, ln := range lines {
		out[i] = ln.Cells
	}
	return out
}

// statusLines renders and caches the status lines for c; none while the status option is off.
func (s *Server) statusLines(c *attachedClient) []status.Line {
	if c.session.Options.GetNumber("status") == 0 {
		return nil
	}
	if c.statusLines != nil && c.statusAt.Equal(s.opts.Now()) {
		return c.statusLines
	}
	ctx := &format.Context{
		Tree:       s.tree,
		Server:     s.ServerInfo(),
		Clients:    s.clientFormats(),
		Now:        s.opts.Now,
		ItemClient: c.format,
	}
	ctx.Defaults(c.format, c.session, c.session.Current, nil)
	ctx.Buffer = s.buffers.Top() // format_defaults' top paste buffer
	c.statusLines = status.Draw(ctx, c.session.Options, c.tty.Sx)
	c.statusAt = s.opts.Now()
	s.scheduleStatusRefresh(c)
	return c.statusLines
}

// scheduleStatusRefresh starts a timer for status-interval if one is not already pending, and
// only while the status line is on: tmux's status_timer_start. Each frame would otherwise
// make a key's echo just after it wait for the next one.
func (s *Server) scheduleStatusRefresh(c *attachedClient) {
	if c.statusTimer != nil || c.session.Options.GetNumber("status") == 0 {
		return
	}
	interval := time.Duration(c.session.Options.GetNumber("status-interval")) * time.Second
	if interval <= 0 {
		return
	}
	var t *time.Timer
	t = time.AfterFunc(interval, func() {
		s.post(func() {
			if c.statusTimer != t {
				return // stopped, and maybe started again, since
			}
			c.statusTimer = nil
			c.statusLines = nil
			s.markDirty(c)
		})
	})
	c.statusTimer = t
}

// restartStatusTimers is status_timer_start_all, when status or status-interval changes: each
// client's timer is stopped, and started again by its next frame if the status line is on.
func (s *Server) restartStatusTimers() {
	for _, c := range s.attached {
		if c.statusTimer != nil {
			c.statusTimer.Stop()
			c.statusTimer = nil
		}
	}
}

// clientFormats returns the attached clients as format.Client values.
func (s *Server) clientFormats() []*format.Client {
	out := make([]*format.Client, 0, len(s.attached))
	for _, c := range s.attached {
		if !c.gone {
			out = append(out, c.format)
		}
	}
	return out
}
