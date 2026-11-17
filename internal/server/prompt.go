package server

import (
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/commands"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/format"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/keys"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/status"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/target"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/vt"
)

// The command prompt (TASKS.md C4.10): a client's prompt on its status line, which gets its
// keys while it is open.

// prompt is a client's command prompt and what opened it.
type prompt struct {
	pr   *status.Prompt
	spec commands.PromptSpec
}

// SetPrompt implements commands.Host: status_prompt_set.
func (s *Server) SetPrompt(spec commands.PromptSpec) bool {
	c := s.clientByName(spec.Client)
	if c == nil || c.prompt != nil {
		return false
	}
	s.clearOverlay(c)
	s.clearMessage(c)
	oo := c.session.Options
	p := &prompt{spec: spec}
	p.pr = status.NewPrompt(spec.Text, spec.Input, spec.Flags, spec.Type, status.PromptOptions{
		Vi:             oo.GetNumber("status-keys") == 1,
		WordSeparators: oo.GetString("word-separators"),
		History:        s.promptHistory,
		HistoryLimit:   int(s.tree.Options.Server.GetNumber("prompt-history-limit")),
		PasteTop: func() []byte {
			if b := s.buffers.Top(); b != nil {
				return b.Data
			}
			return nil
		},
	}, func(in *string) bool { return spec.Done(in, p.pr.Update) })
	c.prompt = p
	s.markDirty(c)
	return true
}

// clearPrompt is status_prompt_clear: the prompt goes; a command still waiting for it goes on.
func (s *Server) clearPrompt(c *attachedClient) {
	p := c.prompt
	if p == nil {
		return
	}
	c.prompt = nil
	if p.spec.Free != nil {
		p.spec.Free()
	}
	s.markDirty(c)
}

// promptKey is status_prompt_key: what a key does to the client's prompt. It reports whether
// the key was taken.
func (s *Server) promptKey(c *attachedClient, k keys.Key) bool {
	p := c.prompt
	if k.IsMouse() {
		// Clicks in the prompt come with the mouse (task C5.1).
		return false
	}
	result := p.pr.Key(k)
	s.markDirty(c)
	if c.prompt == p && p.pr.Closed() {
		s.clearPrompt(c)
	}
	return result == status.PromptKeyHandled || result == status.PromptKeyClose
}

// RunCommands implements commands.Host: the list runs for the client, from its current
// target, its output and errors on its message line.
func (s *Server) RunCommands(client string, list *commands.List) {
	c := s.clientByName(client)
	if c == nil {
		return
	}
	sess := c.session
	st := target.State{S: sess, WL: sess.Current, Idx: -1}
	if wl := sess.Current; wl != nil {
		st.W, st.P = wl.Window, wl.Window.Active
	}
	commands.RunList(s, &commands.Client{Name: c.name, Session: sess, Env: c.env}, list, &st, nil, func(res commands.Result) {
		s.showResult(c, res)
		s.redrawAll()
	})
}

// promptLines are the lines a client shows while its prompt is open, and the cursor's column.
func (s *Server) promptLines(c *attachedClient) ([][]vt.Cell, int) {
	ctx := &format.Context{
		Tree: s.tree, Server: s.ServerInfo(), Clients: s.clientFormats(), Now: s.opts.Now,
	}
	ctx.Defaults(nil, c.session, c.session.Current, nil)
	ctx.Buffer = s.buffers.Top() // format_defaults' top paste buffer
	var lines []status.Line
	if s.statusLineSize(c) > 0 {
		lines = s.statusLines(c)
	}
	return status.PromptLines(ctx, c.session.Options, c.tty.Sx, lines, c.prompt.pr)
}

// promptScreenLine is status_prompt_screen_line: the terminal line the prompt is on.
func (s *Server) promptScreenLine(c *attachedClient) int {
	lines := s.statusLineSize(c)
	at := 0
	if lines > 0 {
		at = min(int(c.session.Options.GetNumber("message-line")), lines-1)
	}
	if c.session.Options.GetNumber("status-position") == 0 {
		return at
	}
	n := lines - at
	if n <= c.tty.Sy {
		return min(c.tty.Sy-n, c.tty.Sy-1)
	}
	return c.tty.Sy - 1
}
