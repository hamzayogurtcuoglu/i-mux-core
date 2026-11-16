package server

import (
	"time"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/keys"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/model"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/vt"
)

// Keys into panes (TASKS.md C2.11).

// backspace is tmux's backspace option default: DEL.
const backspace keys.Key = 0x7f

// PaneKey implements commands.Host: the key is encoded from the pane's modes and written to
// its program (tmux's window_pane_key and input_key_pane, for a pane in no mode). It reports
// false when the key has no encoding.
func (s *Server) PaneKey(p *model.Pane, k keys.Key) bool {
	if p == nil || p.Term == nil || p.Proc == nil || p.Dead {
		return true
	}
	p.Mu.Lock()
	mode := p.Term.Screen.Mode
	p.Mu.Unlock()
	b, ok := keys.Encode(k, mode, backspace, s.extendedFormat())
	if len(b) > 0 {
		_, _ = p.Proc.Write(b)
	}
	return ok
}

// OptionChanged implements commands.Host: what depends on the option takes its new value
// (tmux's options_push_changes, for what the engine has so far), and every client is drawn
// again.
func (s *Server) OptionChanged(name string) {
	switch name {
	case "extended-keys":
		for _, p := range s.tree.Panes() {
			if p.Term != nil {
				p.Mu.Lock()
				p.Term.ExtendedKeys = s.extendedKeys()
				p.Mu.Unlock()
			}
		}
	case "automatic-rename":
		for _, sess := range s.tree.Sessions() {
			for _, wl := range sess.Links() {
				if w := wl.Window; w.Active != nil && w.Options.GetNumber("automatic-rename") != 0 {
					w.Active.Changed = true
				}
			}
		}
	case "allow-set-title", "":
		for _, p := range s.tree.Panes() {
			s.applyPaneOptions(p)
		}
	case "set-clipboard":
		mode := s.clipboardMode()
		for _, p := range s.tree.Panes() {
			if p.Term != nil {
				p.Mu.Lock()
				p.Term.ClipboardMode = mode
				p.Mu.Unlock()
			}
		}
		for _, c := range s.attached {
			if pd := c.popup; pd != nil {
				pd.mu.Lock()
				pd.term.ClipboardMode = mode
				pd.mu.Unlock()
			}
		}
	case "escape-time":
		for _, c := range s.attached {
			if c.in.parser != nil {
				c.in.parser.EscapeTime = s.escapeTime()
			}
		}
	}
	// pane-border-status takes a line from each window's panes, or gives it back.
	for _, sess := range s.tree.Sessions() {
		for _, wl := range sess.Links() {
			if w := wl.Window; w.Layout != nil && w.Layout.Status != w.BorderStatus() {
				s.tree.SetBorderStatus(w, w.BorderStatus())
			}
		}
	}
	s.recalculateSizes()
	for _, c := range s.attached {
		s.invalidate(c)
	}
}

// The server options the engine reads.
func (s *Server) extendedKeys() vt.ExtendedKeysOption {
	return vt.ExtendedKeysOption(s.tree.Options.Server.GetNumber("extended-keys"))
}

func (s *Server) extendedFormat() keys.ExtendedFormat {
	return keys.ExtendedFormat(s.tree.Options.Server.GetNumber("extended-keys-format"))
}

func (s *Server) escapeTime() time.Duration {
	return time.Duration(s.tree.Options.Server.GetNumber("escape-time")) * time.Millisecond
}
