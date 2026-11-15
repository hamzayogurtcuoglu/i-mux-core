package server

import (
	"errors"
	"io"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/model"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/pty"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/vt"
)

// The server loop (ARCHITECTURE.md §4, E2): one goroutine owns the tree; everything that
// changes it arrives as an event on its channel, in order. Each pane has a reader goroutine
// that feeds the pane's screen under the pane's mutex and tells the loop, at most one notice
// pending at a time, that the pane changed.

// Options configure a server.
type Options struct {
	PTY          pty.PTY          // nil: pty.New()
	HistoryLimit int              // history-limit; 0: tmux's default, 2000
	Now          func() time.Time // nil: time.Now
	// PaneChanged is called in the loop when a pane's screen has changed (coalesced).
	PaneChanged func(p *model.Pane)
}

// Server is the server's state and loop.
type Server struct {
	opts   Options
	tree   *model.Tree
	events chan func()
	stop   chan struct{}
	done   chan struct{}

	readers sync.WaitGroup
	panes   map[*model.Pane]*paneState
	exiting bool
}

type paneState struct {
	pending atomic.Bool // a change notice is waiting in the loop
}

// New returns a server; Run starts its loop.
func New(o Options) *Server {
	if o.PTY == nil {
		o.PTY = pty.New()
	}
	if o.HistoryLimit == 0 {
		o.HistoryLimit = 2000
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	return &Server{
		opts:   o,
		tree:   model.NewTree(),
		events: make(chan func(), 256),
		stop:   make(chan struct{}),
		done:   make(chan struct{}),
		panes:  map[*model.Pane]*paneState{},
	}
}

// Run runs the loop until Shutdown, then waits for the pane readers to end.
func (s *Server) Run() {
	defer close(s.done)
	for {
		select {
		case f := <-s.events:
			f()
			if s.exiting {
				s.shutdown()
				s.finish()
				return
			}
		case <-s.stop:
			s.shutdown()
			s.finish()
			return
		}
	}
}

// finish waits for the pane readers to end, dropping the events they still post so none of
// them stays blocked.
func (s *Server) finish() {
	ended := make(chan struct{})
	go func() {
		s.readers.Wait()
		close(ended)
	}()
	for {
		select {
		case <-s.events:
		case <-ended:
			return
		}
	}
}

// Shutdown ends the server: every pane's program is killed and the loop returns.
func (s *Server) Shutdown() {
	select {
	case <-s.stop:
	default:
		close(s.stop)
	}
	<-s.done
}

// Done is closed once the loop has returned.
func (s *Server) Done() <-chan struct{} { return s.done }

// post queues an event for the loop. It returns false once the loop has ended.
func (s *Server) post(f func()) bool {
	select {
	case s.events <- f:
		return true
	case <-s.done:
		return false
	}
}

// Do runs f in the loop with the tree, and waits for it.
func (s *Server) Do(f func(t *model.Tree)) error {
	ran := make(chan struct{})
	if !s.post(func() { f(s.tree); close(ran) }) {
		return ErrStopped
	}
	select {
	case <-ran:
		return nil
	case <-s.done:
		return ErrStopped
	}
}

// ErrStopped is returned once the server has ended.
var ErrStopped = errors.New("server: stopped")

// Spawn starts a program in pane p (in the loop): a screen of the pane's size and a reader.
func (s *Server) Spawn(p *model.Pane, spec pty.Spec) error {
	spec.Cols, spec.Rows = p.Sx, p.Sy
	proc, err := s.opts.PTY.Start(spec)
	if err != nil {
		return err
	}
	p.Proc = proc
	p.Term = vt.NewTerminal(p.Sx, p.Sy, s.opts.HistoryLimit)
	st := &paneState{}
	s.panes[p] = st
	s.readers.Add(1)
	go s.read(p, st)
	return nil
}

type deadliner interface{ SetReadDeadline(time.Time) error }

// read is a pane's reader goroutine.
func (s *Server) read(p *model.Pane, st *paneState) {
	defer s.readers.Done()
	buf := make([]byte, 32<<10)
	dl, _ := p.Proc.(deadliner)
	for {
		n, err := p.Proc.Read(buf)
		if n > 0 {
			p.Mu.Lock()
			_, _ = p.Term.Write(buf[:n])
			replies := p.Term.DrainReplies()
			inString := p.Term.InString()
			p.Mu.Unlock()
			if len(replies) > 0 {
				_, _ = p.Proc.Write(replies)
			}
			if dl != nil {
				// tmux's ground timer: an unterminated string is given up on after a quiet spell.
				if inString {
					_ = dl.SetReadDeadline(time.Now().Add(vt.GroundTimeout))
				} else {
					_ = dl.SetReadDeadline(time.Time{})
				}
			}
			if !st.pending.Swap(true) {
				s.post(func() {
					st.pending.Store(false)
					if s.opts.PaneChanged != nil && s.panes[p] == st {
						s.opts.PaneChanged(p)
					}
				})
			}
		}
		if errors.Is(err, os.ErrDeadlineExceeded) {
			p.Mu.Lock()
			p.Term.ExpireString()
			p.Mu.Unlock()
			_ = dl.SetReadDeadline(time.Time{})
			continue
		}
		if err != nil {
			if !errors.Is(err, io.EOF) && !errors.Is(err, os.ErrClosed) {
				_ = err // a read error ends the pane like its end of output
			}
			break
		}
	}
	status, _ := p.Proc.Wait()
	s.post(func() { s.paneExited(p, st, status) })
}

// paneExited handles a pane whose program ended: it goes, then its window if empty, then its
// session if empty, and the server ends with the last session (exit-empty). remain-on-exit
// comes with task C3.12.
func (s *Server) paneExited(p *model.Pane, st *paneState, status pty.ExitStatus) {
	if s.panes[p] != st {
		return
	}
	delete(s.panes, p)
	p.Dead, p.Status = true, status
	_ = p.Proc.Close()
	if s.tree.Pane(p.ID) == nil {
		return
	}
	s.RemovePane(p)
}

// RemovePane takes a pane out (in the loop), killing its program, and its window and session
// when they are left empty. The server ends when no session is left.
func (s *Server) RemovePane(p *model.Pane) {
	if _, ok := s.panes[p]; ok {
		_ = p.Proc.Kill()
		_ = p.Proc.Close()
		delete(s.panes, p)
	}
	w := p.Window
	if s.tree.RemovePane(p) {
		sess := w.Session()
		if s.tree.RemoveWindow(w) {
			s.tree.RemoveSession(sess)
		}
	}
	if s.tree.Empty() {
		s.exiting = true
	}
}

// shutdown kills every pane's program (in the loop).
func (s *Server) shutdown() {
	for p := range s.panes {
		_ = p.Proc.Kill()
		_ = p.Proc.Close()
		delete(s.panes, p)
	}
}
