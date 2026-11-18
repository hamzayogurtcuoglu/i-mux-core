package server

import (
	"net"
	"time"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/format"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/ipc"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/keys"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/model"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/render"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/status"
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
	created  time.Time
	pid      int
	termName string
	env      map[string]string // the client's environment (tmux's c->environ)
	last     *model.Session    // the session shown before (client_last_session)
	gone     bool              // detached or exited: nothing more is sent
	// xpixel and ypixel are a cell of the client's terminal in pixels, 0 when it does not say
	// (tmux's tty->xpixel and tty->ypixel).
	xpixel, ypixel int
	draw           drawState
	in             inputState
	format         *format.Client

	// Cached status lines (task C4.2).
	statusLines []status.Line
	statusAt    time.Time
	statusTimer *time.Timer

	// The message on the status line (task C4.5), and the timer that clears it.
	message      *message
	messageTimer *time.Timer

	// The popup the client shows (its overlay, task C4.6).
	popup *popup
	// The command prompt on its status line (task C4.10).
	prompt *prompt

	// The key table the client is in (tmux's keytable), when it was set (its
	// activity_time), and the repeat state (CLIENT_REPEAT, last_key, repeat_timer).
	// mouse is the client's mouse state: drags and clicks (task C5.1).
	mouse mouseState
	// copyDragTimer scrolls copy mode while a drag holds its cursor at the view's edge.
	copyDragTimer *time.Timer

	keyTable    string
	keyTableAt  time.Time
	repeating   bool
	lastKey     keys.Key
	repeatTimer *time.Timer
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
	now := s.opts.Now()
	c := &attachedClient{
		name: hello.TTYName, session: sess, queue: queue, activity: now, created: now,
		pid: hello.Pid, termName: termName, env: hello.Env,
	}
	c.mouse.lastPane = -1
	c.draw.r = render.New()
	// tty_term_create: the server's terminal-features and terminal-overrides.
	o := termout.Options{
		TerminalFeatures:  s.arrayOption("terminal-features"),
		TerminalOverrides: s.arrayOption("terminal-overrides"),
		ColorTerm:         hello.Env["COLORTERM"],
	}
	term, err := termout.NewTerm(termName, hello.Terminfo, &c.features, o)
	if err != nil {
		return nil, err
	}
	c.tty = termout.NewTTY(term, hello.Size.Cols, hello.Size.Rows)
	c.tty.UTF8 = hello.UTF8
	// server_client_dispatch_identify's tty_resize: its output is not open yet, so the
	// terminal is not asked for a pixel size it did not give.
	s.ttyResize(c, hello.Size, false)
	c.format = s.formatClient(c)
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
	// server_client_open: the theme colours as the terminal opens (after the input state,
	// which they live in, is made).
	s.updateThemeColours(c)
	s.send(c)
	// The client starts in its session's default key table (tmux's root, or the session's
	// key-table option); the fields are set here, not through setKeyTable, so nothing is
	// drawn before the client is ready.
	c.keyTable, c.keyTableAt = s.defaultKeyTable(sess), now
	// server_client_set_session: the client is its window's latest, sizes follow, and the
	// session was active now (which the best session is chosen by).
	if w := c.window(); w != nil {
		s.latest[w] = c
	}
	sess.Activity = s.opts.Now()
	s.recalculateSizes()
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
	if c.repeatTimer != nil {
		c.repeatTimer.Stop()
		c.repeatTimer = nil
	}
	if c.statusTimer != nil {
		c.statusTimer.Stop()
		c.statusTimer = nil
	}
	if c.messageTimer != nil {
		c.messageTimer.Stop()
		c.messageTimer = nil
	}
	s.clearOverlay(c)
	s.clearPrompt(c)
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
	// server_client_lost: the client's overlay and prompt go with it.
	s.clearOverlay(c)
	s.clearPrompt(c)
	s.lostClient(c)
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
						s.ttyResize(c, size, true)
						s.send(c)
						s.popupResized(c)
						s.updateLatest(c)
						s.recalculateSizes()
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

// arrayOption is a server array option's values, in order of their indexes.
func (s *Server) arrayOption(name string) []string {
	o := s.tree.Options.Server.Get(name)
	if o == nil {
		return nil
	}
	var out []string
	for _, k := range o.ArrayKeys() {
		if v, ok := o.ArrayGet(k); ok {
			out = append(out, v.Str)
		}
	}
	return out
}
