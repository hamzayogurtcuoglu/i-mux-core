// Ported from tmux 3.8's server-client.c (server_client_key_callback and its helpers) and
// key-bindings.c (key_bindings_dispatch):
//
// Copyright (c) 2009 Nicholas Marriott <nicholas.marriott@gmail.com>
//
// Permission to use, copy, modify, and distribute this software for any
// purpose with or without fee is hereby granted, provided that the above
// copyright notice and this permission notice appear in all copies.
//
// THE SOFTWARE IS PROVIDED "AS IS" AND THE AUTHOR DISCLAIMS ALL WARRANTIES
// WITH REGARD TO THIS SOFTWARE INCLUDING ALL IMPLIED WARRANTIES OF
// MERCHANTABILITY AND FITNESS. IN NO EVENT SHALL THE AUTHOR BE LIABLE FOR
// ANY SPECIAL, DIRECT, INDIRECT, OR CONSEQUENTIAL DAMAGES OR ANY DAMAGES
// WHATSOEVER RESULTING FROM LOSS OF MIND, USE, DATA OR PROFITS, WHETHER
// IN AN ACTION OF CONTRACT, NEGLIGENCE OR OTHER TORTIOUS ACTION, ARISING
// OUT OF OR IN CONNECTION WITH THE USE OR PERFORMANCE OF THIS SOFTWARE.

package server

import (
	"errors"
	"strings"
	"time"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/commands"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/keys"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/model"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/target"
)

// Key bindings (TASKS.md C3.8): a client's key table, the prefix switch and the lookup
// with tmux's fallbacks, ported from tmux 3.8's server-client.c
// (server_client_key_callback) and key-bindings.c (key_bindings_dispatch).

// defaultKeyTable is server_client_get_key_table: the session's key-table option, root
// when it is empty and with no session.
func (s *Server) defaultKeyTable(sess *model.Session) string {
	if sess == nil {
		return "root"
	}
	if name := sess.Options.GetString("key-table"); name != "" {
		return name
	}
	return "root"
}

// setKeyTable is server_client_set_key_table: "" is the client's default table. The
// status line is redrawn: it shows whether the client is in its default table.
func (s *Server) setKeyTable(c *attachedClient, name string) {
	if name == "" {
		name = s.defaultKeyTable(c.session)
	}
	c.keyTable = name
	c.keyTableAt = s.opts.Now()
	s.markDirty(c)
}

// SetKeyTable implements commands.Host: switch-client -T puts the client named in a key
// table.
func (s *Server) SetKeyTable(name, table string) error {
	for _, c := range s.attached {
		if c.name == name && !c.gone {
			s.setKeyTable(c, table)
			return nil
		}
	}
	return errors.New("can't find client: " + name)
}

// dispatchKey runs a key a client typed through its bindings, as the binding half of
// tmux's server_client_key_callback: the prefix switch, the lookup in the client's
// table with the fallbacks tmux tries (Any, then the default table), and what is left
// going to the pane.
func (s *Server) dispatchKey(c *attachedClient, key keys.Key) {
	sess := c.session
	mask := keys.MaskKey | keys.MaskModifiers
	prefix := keys.Key(sess.Options.GetNumber("prefix")) & mask
	prefix2 := keys.Key(sess.Options.GetNumber("prefix2")) & mask
	def := s.defaultKeyTable(sess)
	timeout := time.Duration(s.tree.Options.Server.GetNumber("prefix-timeout")) * time.Millisecond

	table := c.keyTable
	if table == "" {
		table = def
	}
	first := table
	wasRepeating := c.repeating

	// table_changed and try_again are one loop: tmux jumps back to the prefix switch and
	// the lookup whenever a table changes under the key (and works the key out again,
	// after the Any retry may have replaced it).
	for {
		// The key without its flags, as tmux works it out at table_changed.
		key0 := key & mask
		// The prefix switches the client to the prefix table, unless it is there.
		if (key0 == prefix || key0 == prefix2) && table != "prefix" {
			s.setKeyTable(c, "prefix")
			return
		}
		// try_again: the binding for the key, then for Any.
		bd := s.binding(table, key0)
		if bd == nil && key0 != keys.Any {
			bd = s.binding(table, keys.Any)
		}
		// prefix-timeout: a client in the prefix table for too long before its key goes
		// back to its default table, unless a repeat binding is repeating.
		if timeout > 0 && table == "prefix" && c.activity.Sub(c.keyTableAt) > timeout &&
			(bd == nil || !c.repeating || !bd.Repeat) {
			s.setKeyTable(c, "")
			table, first = c.keyTable, c.keyTable
			continue
		}
		if bd != nil {
			// While repeating, a binding that does not repeat is tried in the default
			// table instead.
			if c.repeating && !bd.Repeat {
				s.setKeyTable(c, "")
				table, first, c.repeating = c.keyTable, c.keyTable, false
				continue
			}
			if d := s.repeatTime(c, bd); d != 0 {
				c.repeating = true
				c.lastKey = bd.Key
				s.armRepeat(c, d)
			} else {
				c.repeating = false
				s.setKeyTable(c, "")
			}
			s.runBinding(c, bd)
			return
		}
		// No binding in this table: the default table, unless that is where this started;
		// a key that fell out of another table is swallowed, not sent to the pane.
		if table != def || c.repeating {
			s.setKeyTable(c, "")
			table = c.keyTable
			if c.repeating {
				first = table
			}
			c.repeating = false
			continue
		}
		if first != table && !wasRepeating {
			s.setKeyTable(c, "")
			return
		}
		if p := c.window().Active; p != nil {
			s.PaneKey(p, key)
		}
		return
	}
}

// binding is key_bindings_get: the binding for key in the table, or nil.
func (s *Server) binding(table string, key keys.Key) *keys.Binding {
	if t := s.tree.Keys.Get(table); t != nil {
		return t.Get(key)
	}
	return nil
}

// repeatTime is server_client_repeat_time: how long a repeat binding stays repeatable.
func (s *Server) repeatTime(c *attachedClient, bd *keys.Binding) time.Duration {
	if !bd.Repeat {
		return 0
	}
	rep := c.session.Options.GetNumber("repeat-time")
	if rep == 0 {
		return 0
	}
	if !c.repeating || c.lastKey != bd.Key {
		if init := c.session.Options.GetNumber("initial-repeat-time"); init != 0 {
			rep = init
		}
	}
	return time.Duration(rep) * time.Millisecond
}

// armRepeat arms the repeat timer (tmux's repeat_timer): when it fires, the client
// leaves its table and stops repeating.
func (s *Server) armRepeat(c *attachedClient, d time.Duration) {
	if c.repeatTimer != nil {
		c.repeatTimer.Stop()
	}
	c.repeatTimer = time.AfterFunc(d, func() {
		s.post(func() {
			c.repeatTimer = nil
			if !c.gone && c.repeating {
				c.repeating = false
				s.setKeyTable(c, "")
			}
		})
	})
}

// runBinding is key_bindings_dispatch: the binding's commands run in the loop for the
// client that pressed the key, from its session, window and pane as the current target
// (cmd_find_from_client). Errors go on the client's message line, their first letter
// capitalised, as cmdq_error does; output goes there too until view mode comes (tmux shows
// it in a view mode, task C4.8).
func (s *Server) runBinding(c *attachedClient, bd *keys.Binding) {
	list, _ := bd.Cmds.(*commands.List)
	if list == nil {
		return
	}
	sess := c.session
	st := target.State{S: sess, WL: sess.Current, Idx: -1, W: sess.Current.Window, P: sess.Current.Window.Active}
	commands.RunList(s, &commands.Client{Name: c.name, Session: sess, Env: c.env}, list, &st, func(res commands.Result) {
		show := func(out string, capital bool) {
			for _, line := range strings.Split(strings.TrimSuffix(out, "\n"), "\n") {
				if line == "" {
					continue
				}
				if capital {
					line = strings.ToUpper(line[:1]) + line[1:]
				}
				s.ShowMessage(c.name, line, commands.MessageOptions{Delay: -1, IgnoreStyles: true})
			}
		}
		show(res.Stdout, false)
		show(res.Stderr, true)
		s.redrawAll()
	})
}
