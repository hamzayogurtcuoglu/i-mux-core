package server

import (
	"time"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/model"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/render"
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
		Styles: render.DefaultStyles(),
		// tmux 3.8's mouse option is on by default; options come with task C3.4.
		Mouse:    true,
		NoBright: c.tty.Term.Has("Nobr"),
	}
	if wl := c.session.Current; wl != nil {
		v.Window = wl.Window
	}
	// The status line is reserved and drawn with task C4.2; until then panes take its line.
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
