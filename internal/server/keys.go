package server

import (
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
	b, ok := keys.Encode(k, mode, backspace, s.extendedFormat)
	if len(b) > 0 {
		_, _ = p.Proc.Write(b)
	}
	return ok
}

// SetServerOption implements commands.Host for the options it knows: extended-keys (which a
// pane's request for extended keys is checked against), extended-keys-format, and status and
// window-size, which windows' sizes follow.
func (s *Server) SetServerOption(name string, choice int) {
	switch name {
	case "extended-keys":
		s.extendedKeys = vt.ExtendedKeysOption(choice)
		for _, p := range s.tree.Panes() {
			if p.Term != nil {
				p.Mu.Lock()
				p.Term.ExtendedKeys = s.extendedKeys
				p.Mu.Unlock()
			}
		}
	case "extended-keys-format":
		s.extendedFormat = keys.ExtendedFormat(choice)
	case "status":
		s.statusLines = choice
		s.recalculateSizes()
		for _, c := range s.attached {
			s.invalidate(c)
		}
	case "window-size":
		s.windowSize = choice
		s.recalculateSizes()
	}
}

// StatusLines implements commands.Host.
func (s *Server) StatusLines() int { return s.statusLines }
