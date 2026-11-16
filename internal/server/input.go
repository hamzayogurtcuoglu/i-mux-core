package server

import (
	"time"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/input"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/keys"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/model"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/render"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/vt"
)

// Input from attached clients (TASKS.md C2.10), as tmux's tty-keys.c and server-client.c deal
// with it: the terminal's replies tell the server about the terminal (features, colours,
// theme), focus reports and pastes go to the active pane, and theme reports fire the client
// events. Other keys go through the client's bindings (C3.8) and what they leave goes to the
// active pane (C2.11); mouse reports are left for C5.1.

// queryTimeout is how long the server waits for the replies to its queries (tmux's
// TTY_QUERY_TIMEOUT).
const queryTimeout = 5 * time.Second

// assumePasteTime is tmux's assume-paste-time default: keys this close together are a paste
// when the terminal has no bracketed paste.
const assumePasteTime = time.Millisecond

// inputState is what the server keeps of a client's terminal and input (the loop's).
type inputState struct {
	parser   *input.Parser
	timer    bool // the escape-time timer is running
	focused  bool // tmux's CLIENT_FOCUSED
	pasting  bool // CLIENT_BRACKETPASTING
	assuming bool // CLIENT_ASSUMEPASTING
	theme    vt.Theme
	fg, bg   int // the terminal's colours, or -1
	termType string
	themes   []vt.Color // the client's theme colours, nil for the terminal's
	last     time.Time  // the input before the last (last_activity_time)
}

// startInput sets up a client's input once its terminal has been sent the queries.
func (s *Server) startInput(c *attachedClient, erase int) {
	c.in = inputState{parser: input.New(c.tty.Term, erase), focused: true, fg: -1, bg: -1}
	c.in.parser.EscapeTime = s.escapeTime()
	c.in.parser.Requested()
	time.AfterFunc(queryTimeout, func() {
		s.post(func() {
			if !c.gone {
				c.in.parser.StopWaiting()
			}
		})
	})
	s.updateFocus(c.window())
}

// window is the window the client shows.
func (c *attachedClient) window() *model.Window {
	if wl := c.session.Current; wl != nil {
		return wl.Window
	}
	return nil
}

// clientInput handles bytes the client's terminal sent (in the loop).
func (s *Server) clientInput(c *attachedClient, b []byte) {
	if c.gone {
		return
	}
	c.in.last, c.activity = c.activity, s.opts.Now()
	s.handleInput(c, c.in.parser.Feed(b))
}

// handleInput handles parsed events, then waits for the rest of a partial key.
func (s *Server) handleInput(c *attachedClient, events []input.Event) {
	for i := range events {
		if c.gone {
			return
		}
		s.inputEvent(c, &events[i])
	}
	s.sendThemeUpdates()
	if d, ok := c.in.parser.Delay(); ok && !c.in.timer && !c.gone {
		c.in.timer = true
		time.AfterFunc(d, func() {
			s.post(func() {
				c.in.timer = false
				if !c.gone {
					s.handleInput(c, c.in.parser.Expire())
				}
			})
		})
	}
}

func (s *Server) inputEvent(c *attachedClient, ev *input.Event) {
	switch ev.Kind {
	case input.EventDA, input.EventSync:
		if ev.Features != "" {
			c.features.Parse(ev.Features, ",")
		}
		if ev.Kind == input.EventDA || ev.Features != "" {
			s.updateFeatures(c)
		}
	case input.EventDA2, input.EventXDA:
		if ev.Name != "" {
			c.features.Default(ev.Name)
		}
		if ev.Kind == input.EventXDA {
			c.in.termType = ev.TermType
		}
		s.updateFeatures(c)
	case input.EventColour:
		if ev.Fg != -1 {
			c.in.fg = ev.Fg
		}
		if ev.Bg != -1 && ev.Bg != c.in.bg {
			c.in.bg = ev.Bg
			s.updateThemeColours(c)
		}
		s.updatePaneColours()
		s.themeChanged(c)
	case input.EventKey:
		s.inputKey(c, ev)
	}
}

// updateFeatures is tty_update_features: the terminal is described again and drawn again.
func (s *Server) updateFeatures(c *attachedClient) {
	c.tty.UpdateFeatures(false, false)
	c.draw.r.Invalidate()
	s.markDirty(c)
}

// inputKey is server_client_handle_key and server_client_key_callback for the keys this task
// handles.
func (s *Server) inputKey(c *attachedClient, ev *input.Event) {
	key := ev.Key
	switch key {
	case keys.ReportLightTheme:
		s.reportTheme(c, vt.ThemeLight)
		return
	case keys.ReportDarkTheme:
		s.reportTheme(c, vt.ThemeDark)
		return
	case keys.FocusOut:
		c.in.focused = false
		s.updateFocus(c.window())
		s.fireClient("client-focus-out", c)
		// tmux fires focus keys as keys too: they clear a message.
		s.messageKey(c)
		return
	case keys.FocusIn:
		c.in.focused = true
		s.fireClient("client-focus-in", c)
		s.updateFocus(c.window())
		s.updateLatest(c)
		s.messageKey(c)
		return
	}
	// A key clears the message, or is dropped when the message ignores keys.
	if !s.messageKey(c) {
		return
	}
	// An overlay takes the keys while it is open, then the window's menu
	// (server_client_handle_menu_key).
	if c.popup != nil {
		if s.popupKey(c, ev) {
			s.clearOverlay(c)
		}
		return
	}
	if md := s.windowMenu(c); md != nil {
		if s.menuKey(c, md, ev) {
			s.closeMenu(md.w)
		}
		return
	}
	if c.prompt != nil && s.promptKey(c, key) {
		return
	}
	// A key makes the client its window's latest and its session active
	// (server_client_key_callback).
	c.session.Activity = c.activity
	s.updateLatest(c)
	w := c.window()
	if w == nil {
		return
	}
	p := w.Active
	// Bracketed paste: the brackets go as keys, what is between them as it came.
	switch key & keys.MaskKey {
	case keys.PasteStart:
		c.in.pasting = true
		s.pasteBracket(p, "\033[200~")
		return
	case keys.PasteEnd:
		c.in.pasting = false
		s.pasteBracket(p, "\033[201~")
		return
	}
	if c.in.pasting || (!key.IsMouse() && s.assumePaste(c)) {
		s.paste(p, ev.Raw)
		return
	}
	// Mouse reports are classified with the mouse (task C5.1); they bind nothing yet.
	if key.IsMouse() {
		return
	}
	// The key goes through the client's bindings (task C3.8); what they leave goes to
	// the pane (forward_key).
	s.dispatchKey(c, key)
}

// assumePaste is server_client_is_assume_paste: keys typed faster than assume-paste-time on a
// terminal without bracketed paste are a paste.
func (s *Server) assumePaste(c *attachedClient) bool {
	if c.in.pasting || c.tty.Term.Has("Enbp") || c.in.last.IsZero() {
		return false
	}
	if c.activity.Sub(c.in.last) < assumePasteTime {
		if c.in.assuming {
			return true
		}
		c.in.assuming = true
		return false
	}
	c.in.assuming = false
	return false
}

// paste writes pasted bytes to a pane as they came (window_pane_paste).
func (s *Server) paste(p *model.Pane, b []byte) {
	if p == nil || p.Proc == nil || p.Dead {
		return
	}
	_, _ = p.Proc.Write(b)
}

// pasteBracket writes a paste bracket to a pane that asked for bracketed paste (input_key).
func (s *Server) pasteBracket(p *model.Pane, seq string) {
	if p == nil || p.Term == nil || p.Proc == nil || p.Dead {
		return
	}
	p.Mu.Lock()
	on := p.Term.Screen.Mode&vt.ModeBracketPaste != 0
	p.Mu.Unlock()
	if on {
		_, _ = p.Proc.Write([]byte(seq))
	}
}

// updateFocus is window_update_focus: the active pane of w is focused when a focused client
// shows it, and a pane that asked for focus reports (1004) is told when that changes.
func (s *Server) updateFocus(w *model.Window) {
	if w == nil || w.Active == nil {
		return
	}
	// A window showing a menu has no focused pane.
	_, hasMenu := s.menus[w]
	for _, p := range w.Panes {
		st := s.panes[p]
		if st == nil || p.Dead || p.Term == nil {
			continue
		}
		focused := false
		if p == w.Active && !hasMenu {
			for _, c := range s.attached {
				// A client showing an overlay does not give the pane its focus.
				if !c.gone && c.in.focused && c.window() == w && c.popup == nil {
					focused = true
					break
				}
			}
		}
		if focused == st.focused {
			continue
		}
		st.focused = focused
		p.Mu.Lock()
		on := p.Term.Screen.Mode&vt.ModeFocusOn != 0
		p.Mu.Unlock()
		name := "pane-focus-out"
		seq := "\033[O"
		if focused {
			name, seq = "pane-focus-in", "\033[I"
		}
		if on {
			_, _ = p.Proc.Write([]byte(seq))
		}
		s.firePane(name, p)
	}
}

// reportTheme is server_client_report_theme: the terminal said it is light or dark.
func (s *Server) reportTheme(c *attachedClient, theme vt.Theme) {
	old := c.in.theme
	c.in.theme = theme
	if theme == vt.ThemeLight {
		s.fireClient("client-light-theme", c)
	} else {
		s.fireClient("client-dark-theme", c)
	}
	if theme != old {
		s.updateThemeColours(c)
		s.invalidate(c)
	}
	// Ask for the colours again; panes hear of the theme once they arrive.
	c.in.parser.Requested()
	c.tty.RepeatRequests()
	s.send(c)
}

// themeDefaults are tmux 3.8's dark-theme-* and light-theme-* options: the colour for 256 or
// more colours, else the ANSI one, by theme colour.
var themeDefaults = [render.ThemeCount]struct {
	dark, light string
	ansi        vt.Color
}{
	{"gray5", "gray10", 0},
	{"gray90", "gray95", 7},
	{"gray70", "gray80", 7},
	{"gray15", "gray45", 0},
	{"yellowgreen", "seagreen", 2},
	{"darkgoldenrod", "darkgoldenrod", 3},
	{"indianred", "indianred4", 1},
	{"skyblue3", "steelblue", 4},
	{"cadetblue", "darkcyan", 6},
	{"mediumpurple", "purple4", 5},
}

// themeOf is colour_totheme: a background colour's theme.
func themeOf(c int) vt.Theme {
	if c == -1 {
		return vt.ThemeUnknown
	}
	col := vt.Color(c)
	switch {
	case col&vt.ColorFlagRGB != 0:
		r, g, b := vt.ColorSplitRGB(col)
		if int(r)+int(g)+int(b) > 382 {
			return vt.ThemeLight
		}
		return vt.ThemeDark
	case col&vt.ColorFlag256 != 0:
		return themeOf(int(vt.Color256ToRGB(col)))
	case c == 0 || c == 90:
		return vt.ThemeDark
	case c == 7 || c == 97:
		return vt.ThemeLight
	case c >= 0 && c <= 7:
		return themeOf(int(vt.Color256ToRGB(col)))
	case c >= 90 && c <= 97:
		return themeOf(int(vt.Color256ToRGB(vt.Color(8 + c - 90))))
	}
	return vt.ThemeUnknown
}

// updateThemeColours is server_client_update_theme_colours with theme detect (the default):
// the dark or light theme's colours, chosen for how many colours the terminal has.
func (s *Server) updateThemeColours(c *attachedClient) {
	theme := c.in.theme
	if theme == vt.ThemeUnknown {
		theme = themeOf(c.in.bg)
	}
	rich := c.tty.Term.Colours() >= 256
	tc := make([]vt.Color, render.ThemeCount)
	for i, d := range themeDefaults {
		tc[i] = d.ansi
		if rich {
			name := d.dark
			if theme == vt.ThemeLight {
				name = d.light
			}
			if col := vt.ParseX11Color(name); col != -1 {
				tc[i] = col
			}
		}
	}
	c.in.themes = tc
	s.invalidate(c)
}

// updatePaneColours gives every pane the colours of the first attached client showing it
// (window_pane_get_fg and window_pane_get_bg), which answer its programs' OSC 10 and 11
// queries, and the theme they report (window_pane_get_theme).
func (s *Server) updatePaneColours() {
	for _, p := range s.tree.Panes() {
		if p.Term == nil {
			continue
		}
		fg, bg := -1, -1
		light, dark := false, false
		for _, c := range s.attached {
			if c.gone || c.window() == nil || c.session != p.Window.Session() {
				continue
			}
			if fg == -1 {
				fg = c.in.fg
			}
			if bg == -1 {
				bg = c.in.bg
			}
			switch c.in.theme {
			case vt.ThemeLight:
				light = true
			case vt.ThemeDark:
				dark = true
			}
		}
		p.Mu.Lock()
		p.Term.Palette.ClientFg, p.Term.Palette.ClientBg = vt.Color(fg), vt.Color(bg)
		var theme vt.Theme
		switch {
		case dark && !light:
			theme = vt.ThemeDark
		case light && !dark:
			theme = vt.ThemeLight
		default:
			b := int(p.Term.Palette.Bg)
			if p.Term.Palette.Bg.IsDefault() {
				b = bg
			}
			theme = themeOf(b)
		}
		p.Term.Theme = theme
		p.Mu.Unlock()
	}
}

// themeChanged is session_theme_changed: the client's session's panes are to hear of the
// theme.
func (s *Server) themeChanged(c *attachedClient) {
	for _, p := range s.tree.Panes() {
		if st := s.panes[p]; st != nil && p.Window.Session() == c.session {
			st.themeChanged = true
		}
	}
}

// sendThemeUpdates is window_pane_send_theme_update for every pane: a pane that asked for
// theme reports (2031) is sent its theme when it changed.
func (s *Server) sendThemeUpdates() {
	for p, st := range s.panes {
		if !st.themeChanged || p.Dead || p.Term == nil {
			continue
		}
		p.Mu.Lock()
		on := p.Term.Screen.Mode&vt.ModeThemeUpdates != 0
		theme := p.Term.Theme
		p.Mu.Unlock()
		if !on || theme == st.lastTheme {
			continue
		}
		st.lastTheme = theme
		st.themeChanged = false
		switch theme {
		case vt.ThemeLight:
			_, _ = p.Proc.Write([]byte("\033[?997;2n"))
		case vt.ThemeDark:
			_, _ = p.Proc.Write([]byte("\033[?997;1n"))
		}
	}
}
