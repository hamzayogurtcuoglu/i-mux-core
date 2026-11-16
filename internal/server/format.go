package server

import (
	"os"
	"slices"
	"sort"
	"strings"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/commands"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/format"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/version"
)

// What the server gives formats (TASKS.md C3.5).

// formatClient is an attached client as formats see it.
func (s *Server) formatClient(c *attachedClient) *format.Client {
	flags := []string{"attached"}
	if c.in.focused {
		flags = append(flags, "focused")
	}
	if c.tty.UTF8 {
		flags = append(flags, "UTF-8")
	}
	term := c.tty.Term
	table := c.keyTable
	if table == "" {
		table = s.defaultKeyTable(c.session)
	}
	return &format.Client{
		Name: c.name, TTY: c.name, Session: c.session, Last: c.last,
		Activity: c.activity, Created: c.created, Pid: c.pid, UID: os.Getuid(),
		Flags: flags, UTF8: c.tty.UTF8,
		Width: c.tty.Sx, Height: c.tty.Sy, Started: true,
		TermName: c.termName, Features: c.features.String(),
		KeyTable: table, Prefix: table != s.defaultKeyTable(c.session), Colours: term.Colours(),
		Env:           c.env,
		HasCapability: term.Has,
		HasFeature: func(name string) bool {
			return slices.Contains(strings.Split(c.features.String(), ","), name)
		},
	}
}

// Clients implements commands.Host: the attached clients, by name.
func (s *Server) Clients() []*commands.ClientInfo {
	out := make([]*commands.ClientInfo, 0, len(s.attached))
	for _, c := range s.attached {
		out = append(out, s.formatClient(c))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// ServerInfo implements commands.Host.
func (s *Server) ServerInfo() *format.Server {
	return &format.Server{
		Pid: s.Pid(), Start: s.started, SocketPath: s.opts.SocketPath, Version: version.Tmux,
	}
}

// ShowMessage implements commands.Host. Messages on the status line come with task C4.2;
// until then display-message without -p shows nothing.
func (s *Server) ShowMessage(client, msg string, delay int) {}
