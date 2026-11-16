package server

import (
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/buffers"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/model"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/vt"
)

// Paste buffers and the clipboard (TASKS.md C4.9).

// Buffers implements commands.Host.
func (s *Server) Buffers() *buffers.Store { return s.buffers }

// SetSelection implements commands.Host: tty_set_selection on the client's terminal.
func (s *Server) SetSelection(client string, data []byte) {
	if c := s.clientByName(client); c != nil {
		c.tty.SetSelection("", data)
		s.send(c)
	}
}

// PaneWrite implements commands.Host.
func (s *Server) PaneWrite(p *model.Pane, b []byte) {
	if p == nil || p.Proc == nil || p.Dead || len(b) == 0 {
		return
	}
	_, _ = p.Proc.Write(b)
}

// clipboardMode is set-clipboard as the panes' terminals take it.
func (s *Server) clipboardMode() vt.ClipboardMode {
	return vt.ClipboardMode(s.tree.Options.Server.GetNumber("set-clipboard"))
}

// paneClipboard is what a program's OSC 52 does with set-clipboard on (input_osc_52): the
// data becomes a paste buffer and goes to the terminals of the clients showing its pane
// (screen_write_setselection).
func (s *Server) paneClipboard(p *model.Pane, clip string, data []byte) {
	if s.tree.Pane(p.ID) == nil {
		return
	}
	for _, c := range s.attached {
		if c.gone {
			continue
		}
		if wl := c.session.Current; wl != nil && wl.Window == p.Window && paneShown(p) {
			c.tty.SetSelection(clip, data)
			s.send(c)
		}
	}
	s.firePane("pane-set-clipboard", p)
	s.buffers.Add("", data, int(s.tree.Options.Server.GetNumber("buffer-limit")))
}

// popupClipboard is input_osc_52 for a popup's program: the data goes to its client's
// terminal and becomes a paste buffer.
func (s *Server) popupClipboard(pd *popup, clip string, data []byte) {
	if pd.gone {
		return
	}
	pd.c.tty.SetSelection(clip, data)
	s.send(pd.c)
	s.buffers.Add("", data, int(s.tree.Options.Server.GetNumber("buffer-limit")))
}

// paneShown is whether a pane is drawn: with a zoomed window, only its zoomed pane.
func paneShown(p *model.Pane) bool {
	w := p.Window
	if w.Layout == nil {
		return true
	}
	if z := w.Layout.Zoomed(); z >= 0 {
		return p.ID == z
	}
	return true
}
