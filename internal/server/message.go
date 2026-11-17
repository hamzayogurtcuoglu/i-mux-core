package server

import (
	"time"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/commands"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/format"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/status"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/vt"
)

// message is a client's status line message (tmux's message_string and its flags).
type message struct {
	text         string
	ignoreStyles bool
	ignoreKeys   bool
	noFreeze     bool
}

// ShowMessage implements commands.Host: tmux's status_message_set. The message replaces the
// one shown, for Delay ms (display-time for -1; until a key for 0).
func (s *Server) ShowMessage(client, msg string, o commands.MessageOptions) {
	var c *attachedClient
	for _, a := range s.attached {
		if a.name == client && !a.gone {
			c = a
		}
	}
	if c == nil {
		return
	}
	s.clearMessage(c)
	m := &message{text: msg, ignoreStyles: o.IgnoreStyles, noFreeze: o.NoFreeze}
	c.message = m
	delay := o.Delay
	if delay == -1 {
		delay = int(c.session.Options.GetNumber("display-time"))
	}
	if delay > 0 {
		c.messageTimer = time.AfterFunc(time.Duration(delay)*time.Millisecond, func() {
			s.post(func() {
				if c.message == m {
					s.clearMessage(c)
				}
			})
		})
	}
	if delay != 0 {
		m.ignoreKeys = o.IgnoreKeys
	}
	s.markDirty(c)
}

// clearMessage is status_message_clear: the message goes and the client is drawn again.
func (s *Server) clearMessage(c *attachedClient) {
	if c.message == nil {
		return
	}
	c.message = nil
	if c.messageTimer != nil {
		c.messageTimer.Stop()
		c.messageTimer = nil
	}
	s.markDirty(c)
}

// messageKey is the start of server_client_handle_key0 for a client showing a message: a key
// clears it, unless the message ignores keys, when the key is dropped. It reports whether the
// key goes on.
func (s *Server) messageKey(c *attachedClient) bool {
	if c.message == nil {
		return true
	}
	if c.message.ignoreKeys {
		return false
	}
	s.clearMessage(c)
	return true
}

// messageLines are the lines a client shows while it shows a message.
func (s *Server) messageLines(c *attachedClient) [][]vt.Cell {
	ctx := &format.Context{
		Tree:       s.tree,
		Server:     s.ServerInfo(),
		Clients:    s.clientFormats(),
		Now:        s.opts.Now,
		ItemClient: c.format,
	}
	ctx.Defaults(c.format, c.session, c.session.Current, nil)
	ctx.Buffer = s.buffers.Top() // format_defaults' top paste buffer
	var lines []status.Line
	if s.statusLineSize(c) > 0 {
		lines = s.statusLines(c)
	}
	return status.MessageLines(ctx, c.session.Options, c.tty.Sx, lines, c.message.text, c.message.ignoreStyles)
}
