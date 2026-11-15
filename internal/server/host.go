package server

import (
	"os"
	"time"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/commands"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/model"
)

// The server as the commands' host (commands.Host). These methods run in the loop.

var _ commands.Host = (*Server)(nil)

// Tree returns the tree.
func (s *Server) Tree() *model.Tree { return s.tree }

// Now returns the time.
func (s *Server) Now() time.Time { return s.opts.Now() }

// RemoveSession takes a session out, killing its panes' programs.
func (s *Server) RemoveSession(sess *model.Session) {
	for _, wl := range sess.Links() {
		for _, p := range append([]*model.Pane(nil), wl.Window.Panes...) {
			s.RemovePane(p)
		}
	}
}

// KillServer ends the server after the current event.
func (s *Server) KillServer() {
	s.killed = true
	s.checkExit()
}

// Environ returns the global environment.
func (s *Server) Environ() []string { return s.opts.Environ }

// SocketPath returns the server's socket.
func (s *Server) SocketPath() string { return s.opts.SocketPath }

// Pid returns the server's process id.
func (s *Server) Pid() int { return os.Getpid() }
