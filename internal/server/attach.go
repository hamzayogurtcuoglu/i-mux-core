package server

import (
	"net"
	"sort"
	"time"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/commands"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/ipc"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/model"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/render"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/termout"
)

// Attached clients (TASKS.md C2.7): a client whose commands attached it stays connected; the
// server sends its terminal everything through its output queue and reads its input and
// resizes. Fields other than queue are the loop's.

type attachedClient struct {
	name     string
	session  *model.Session
	tty      *termout.TTY
	features termout.Features
	queue    *ipc.Queue
	activity time.Time
	gone     bool // detached or exited: nothing more is sent
	draw     drawState
	in       inputState
}

// Clients implements commands.Host.
func (s *Server) Clients() []commands.ClientInfo {
	out := make([]commands.ClientInfo, 0, len(s.attached))
	for _, c := range s.attached {
		out = append(out, commands.ClientInfo{Name: c.name, Session: c.session, Activity: c.activity})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Detach implements commands.Host: the client is told it is detached from its session.
func (s *Server) Detach(name string) {
	for _, c := range s.attached {
		if c.name == name {
			s.endClient(c, ipc.TypeDetach, "detached (from session "+c.session.Name+")", 0)
		}
	}
}

// attach starts an attached client (in the loop): its terminal's description from what it
// sent, then tmux's start sequence and queries. It returns an error tmux would print.
func (s *Server) attach(hello *ipc.Hello, sess *model.Session, queue *ipc.Queue) (*attachedClient, error) {
	termName := hello.Env["TERM"]
	if termName == "" {
		termName = "unknown"
	}
	c := &attachedClient{name: hello.TTYName, session: sess, queue: queue, activity: s.opts.Now()}
	c.draw.r = render.New()
	o := termout.DefaultOptions
	o.ColorTerm = hello.Env["COLORTERM"]
	term, err := termout.NewTerm(termName, hello.Terminfo, &c.features, o)
	if err != nil {
		return nil, err
	}
	c.tty = termout.NewTTY(term, hello.Size.Cols, hello.Size.Rows)
	c.tty.UTF8 = hello.UTF8
	if f, err := ipc.Encode(ipc.TypeAttach, nil); err == nil {
		_ = queue.Push(f)
	}
	sess.Attached++
	s.attached = append(s.attached, c)
	c.tty.Start()
	c.tty.SendRequests()
	erase := -1
	if hello.Erase != nil {
		erase = *hello.Erase
	}
	s.startInput(c, erase)
	s.send(c)
	s.drawClient(c)
	return c, nil
}

// send queues what the client's terminal has been written.
func (s *Server) send(c *attachedClient) {
	b := c.tty.Take()
	if len(b) == 0 || c.gone {
		return
	}
	if err := c.queue.Push(ipc.Frame{Type: ipc.TypeOutput, Payload: b}); err != nil {
		s.endClient(c, ipc.TypeDetach, err.Error(), 1)
	}
}

// endClient gives the client's terminal back and tells it why it is done (tmux's
// server_client_detach and the exit messages); its connection then closes.
func (s *Server) endClient(c *attachedClient, t ipc.Type, reason string, code int) {
	if c.gone {
		return
	}
	c.tty.Stop(c.tty.Sy)
	s.send(c)
	c.gone = true
	if f, err := ipc.Encode(t, &ipc.End{Reason: reason, Code: code}); err == nil {
		_ = c.queue.Push(f)
	}
	c.queue.Close()
	s.removeAttached(c)
}

// removeAttached takes a client that is done out of the attached ones; the panes it showed
// may lose their focus and its colours.
func (s *Server) removeAttached(c *attachedClient) {
	c.session.Attached--
	for i, x := range s.attached {
		if x == c {
			s.attached = append(s.attached[:i], s.attached[i+1:]...)
			break
		}
	}
	s.updateFocus(c.window())
	s.updatePaneColours()
}

// sessionGone ends the clients of a session that has gone (tmux's "[exited]").
func (s *Server) sessionGone(sess *model.Session) {
	for _, c := range append([]*attachedClient(nil), s.attached...) {
		if c.session == sess {
			s.endClient(c, ipc.TypeExit, "exited", 0)
		}
	}
}

// serveAttached runs an attached client's connection: a writer for its queue, and a reader of
// its input and resizes, until either side ends.
func (s *Server) serveAttached(conn net.Conn, r *ipc.Reader, c *attachedClient) {
	written := make(chan struct{})
	go func() {
		defer close(written)
		for {
			f, ok := c.queue.Pop()
			if !ok {
				return
			}
			if err := ipc.WriteFrame(conn, f.Type, f.Payload); err != nil {
				return
			}
		}
	}()
	for {
		f, err := r.Read()
		if err != nil {
			break
		}
		switch f.Type {
		case ipc.TypeResize:
			var size ipc.Size
			if f.Decode(&size) == nil {
				s.post(func() {
					if !c.gone {
						c.tty.Sx, c.tty.Sy = size.Cols, size.Rows
						s.invalidate(c)
					}
				})
			}
		case ipc.TypeInput:
			b := f.Payload
			s.post(func() { s.clientInput(c, b) })
		}
	}
	// The client went away (or its connection was closed after it was told to go).
	s.post(func() {
		if !c.gone {
			c.gone = true
			c.queue.Close()
			s.removeAttached(c)
		}
	})
	c.queue.Close()
	<-written
}

// shutdownClients tells every attached client the server is going (kill-server); like tmux's
// client, it then exits 1.
func (s *Server) shutdownClients() {
	for _, c := range append([]*attachedClient(nil), s.attached...) {
		s.endClient(c, ipc.TypeExit, "server exited", 1)
	}
}
