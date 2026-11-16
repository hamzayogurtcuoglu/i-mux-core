package commands

import (
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/format"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/model"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/target"
)

// Formats in commands (TASKS.md C3.5).

// formatContext is format_create_defaults: the format tree for client c (or none) and the
// session, winlink and pane given, the missing ones taken from c and the session.
func (it *Item) formatContext(c *ClientInfo, s *model.Session, wl *model.Winlink, p *model.Pane) *format.Context {
	ctx := &format.Context{
		Tree:    it.Host.Tree(),
		Server:  it.Host.ServerInfo(),
		Clients: it.Host.Clients(),
		Now:     it.Host.Now,
		Environ: func(s *model.Session, name string) (string, bool) {
			// The session's environment, then the global one (environ_find twice).
			if s != nil {
				if v, ok := s.Env[name]; ok {
					if v == nil {
						return "", false
					}
					return *v, true
				}
			}
			prefix := name + "="
			for _, kv := range it.Host.Environ() {
				if v, ok := strings.CutPrefix(kv, prefix); ok {
					return v, true
				}
			}
			return "", false
		},
	}
	ctx.Values = make(map[string]string, len(it.Formats))
	for k, v := range it.Formats {
		ctx.Values[k] = v
	}
	ctx.Defaults(c, s, wl, p)
	return ctx
}

// ParseFormat expands a %if or %elif condition as tmux's parser does when the file is parsed:
// for the client (or none) and its session, else the best session, without jobs or strftime.
func ParseFormat(h Host, c *Client, f string) string {
	it := &Item{Host: h, Client: c}
	ctx := it.targetContext()
	var s *model.Session
	if tc := ctx.Client; tc != nil && tc.Session != nil {
		s = tc.Session
	} else {
		s = h.Tree().BestSession()
	}
	return it.formatContext(nil, s, nil, nil).Expand(f)
}

// clientInfo is the attached client named name, or nil.
func (it *Item) clientInfo(name string) *ClientInfo {
	for _, c := range it.Host.Clients() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// formatClient is the client a command's formats are for: tc when it shows s, else the best
// client of s (display-message's choice, and cmd_find_best_client).
func (it *Item) formatClient(tc *target.Client, s *model.Session) *ClientInfo {
	if tc != nil && tc.Session == s {
		return it.clientInfo(tc.Name)
	}
	if s == nil {
		return nil
	}
	if bc := target.BestClient(it.targetContext(), s); bc != nil {
		return it.clientInfo(bc.Name)
	}
	return nil
}

// clientInfoOf is the attached client tc as formats see it, or nil.
func (it *Item) clientInfoOf(tc *target.Client) *ClientInfo {
	if tc == nil {
		return nil
	}
	return it.clientInfo(tc.Name)
}

// formatSingle is format_single_from_target: f expanded for the target client and the
// command's target.
func (it *Item) formatSingle(st target.State, f string) string {
	return it.formatContext(it.clientInfoOf(it.targetClient()), st.S, st.WL, st.P).Expand(f)
}

// targetClient is the client given with -c (or the current one); nil when there is none and
// the command may go without (CMD_CLIENT_CANFAIL).
func (it *Item) targetClient() *target.Client {
	c, err := it.findClient('c')
	if err != nil {
		return nil
	}
	return c
}

// displayMessageTemplate is tmux's DISPLAY_MESSAGE_TEMPLATE.
const displayMessageTemplate = "[#{session_name}] #{window_index}:#{window_name}, current pane #{pane_index} - (%H:%M %d-%b-%y)"

// displayMessage is display-message: a format expanded for the target, printed with -p, else
// shown on the client's status line.
func displayMessage(it *Item) error {
	if err := notImplemented(it, "aIj"); err != nil {
		return err
	}
	a := it.Cmd.Args
	st, err := it.findFlag('t', target.Pane, target.CanFail)
	if err != nil {
		return err
	}
	tc := it.targetClient()
	args := a.Strings()
	if a.Has('F') && len(args) != 0 {
		return errors.New("only one of -F or argument must be given")
	}
	delay := -1
	if v, ok := a.Get('d'); ok {
		n, cause := strtonum(v, 0, math.MaxUint32)
		if cause != "" {
			return fmt.Errorf("delay %s", cause)
		}
		delay = n
	}
	template, ok := a.Get('F')
	if len(args) != 0 {
		template, ok = args[0], true
	}
	if !ok {
		template = displayMessageTemplate
	}
	ctx := it.formatContext(it.formatClient(tc, st.S), st.S, st.WL, st.P)
	msg := template
	if !a.Has('l') {
		msg = ctx.ExpandTime(template)
	}
	switch {
	case it.Client == nil:
		return errors.New(msg)
	case a.Has('p'):
		it.Print(msg)
	case tc != nil:
		it.Host.ShowMessage(tc.Name, msg, MessageOptions{Delay: delay, IgnoreKeys: a.Has('N'), NoFreeze: a.Has('C')})
	}
	return nil
}
