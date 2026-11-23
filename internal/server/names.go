// Ported from tmux 3.8's names.c (check_window_name):
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
	"time"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/format"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/model"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/vt"
)

// Window names and pane titles (TASKS.md C3.7).

// nameInterval is NAME_INTERVAL: automatic-rename names a window at most this often.
const nameInterval = 500 * time.Millisecond

// checkNames is the end of tmux's server loop for names: check_window_name on every window.
func (s *Server) checkNames() {
	for _, sess := range s.tree.Sessions() {
		for _, wl := range sess.Links() {
			s.checkWindowName(wl.Window)
		}
	}
}

// checkWindowName is check_window_name: when the window's active pane changed and
// automatic-rename is on, the window takes the name automatic-rename-format gives, no more
// than once each nameInterval (a timer comes back for the rest).
func (s *Server) checkWindowName(w *model.Window) {
	p := w.Active
	if p == nil || w.Options.GetNumber("automatic-rename") == 0 || !p.Changed {
		return
	}
	now := s.opts.Now()
	if since := now.Sub(w.NameTime); since >= 0 && since <= nameInterval {
		if !s.nameTimers[w] {
			s.nameTimers[w] = true
			time.AfterFunc(nameInterval-since, func() {
				s.post(func() { delete(s.nameTimers, w) })
			})
		}
		return
	}
	w.NameTime = now
	p.Changed = false
	ctx := &format.Context{
		Tree: s.tree, Server: s.ServerInfo(), Clients: s.Clients(), Now: s.opts.Now,
		W: w, P: p,
	}
	name, ok := vt.CleanName(ctx.Expand(w.Options.GetString("automatic-rename-format")), true)
	if ok && name != w.Name {
		w.Name = name
		s.windowRenamed(w)
	}
}

// windowRenamed redraws what shows a window's name (its borders and the status lines).
func (s *Server) windowRenamed(w *model.Window) {
	for _, c := range s.attached {
		if c.shows(w) {
			s.markDirty(c)
		}
	}
}

// renamePane is ESC k … ST from a pane's program (input_exit_rename): with allow-rename on,
// the window takes the name and automatic-rename goes off; an empty name gives automatic
// renaming back.
func (s *Server) renamePane(p *model.Pane, name string) {
	if s.tree.Pane(p.ID) != p || p.Options.GetNumber("allow-rename") == 0 {
		return
	}
	w := p.Window
	if name == "" {
		global := s.tree.Options.IsGlobal
		if o := w.Options.GetOnly("automatic-rename"); o != nil {
			_ = o.RemoveOrDefault("", false, global(w.Options))
		}
		if w.Options.GetNumber("automatic-rename") == 0 {
			w.Name, _ = vt.CleanName("", true)
		}
	} else {
		w.Options.SetNumber("automatic-rename", 0)
		if clean, ok := vt.CleanName(name, true); ok {
			w.Name = clean
		}
	}
	s.windowRenamed(w)
}

// applyPaneOptions gives a pane's screen the options its emulator reads (allow-set-title,
// and the engine's allow-default-colours, E19: turned off, it forgets the default colours the
// program set).
func (s *Server) applyPaneOptions(p *model.Pane) {
	if p.Term == nil {
		return
	}
	p.Mu.Lock()
	p.Term.Screen.DisallowSetTitle = p.Options.GetNumber("allow-set-title") == 0
	fixed := p.Options.GetNumber("allow-default-colours") == 0
	if fixed && !p.Term.FixedDefaultColours {
		p.Term.Palette.Fg, p.Term.Palette.Bg = vt.ColorDefault, vt.ColorDefault
	}
	p.Term.FixedDefaultColours = fixed
	p.Mu.Unlock()
}
