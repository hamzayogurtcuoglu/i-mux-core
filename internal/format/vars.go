// The variables of tmux 3.8's format_table (format.c's format_cb_* callbacks):
//
// Copyright (c) 2011 Nicholas Marriott <nicholas.marriott@gmail.com>
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

package format

import (
	"os"
	"os/user"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/buffers"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/cmdlang"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/model"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/platform"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/vt"
)

// A variable gives its value, or false when it has none (tmux's NULL), which expands empty.
type (
	stringVar func(*Context) (string, bool)
	timeVar   func(*Context) (time.Time, bool)
)

func itoa(n int) (string, bool) { return strconv.Itoa(n), true }

func flag(b bool) (string, bool) { return boolString(b), true }

// Variables that need a session, winlink, window, pane or client.
func sv(f func(*model.Session) (string, bool)) stringVar {
	return func(c *Context) (string, bool) {
		if c.S == nil {
			return "", false
		}
		return f(c.S)
	}
}

func wlv(f func(*model.Winlink) (string, bool)) stringVar {
	return func(c *Context) (string, bool) {
		if c.WL == nil {
			return "", false
		}
		return f(c.WL)
	}
}

func wv(f func(*model.Window) (string, bool)) stringVar {
	return func(c *Context) (string, bool) {
		if c.W == nil {
			return "", false
		}
		return f(c.W)
	}
}

func pv(f func(*model.Pane) (string, bool)) stringVar {
	return func(c *Context) (string, bool) {
		if c.P == nil {
			return "", false
		}
		return f(c.P)
	}
}

// screenVar reads the pane's screen under its lock.
func screenVar(f func(*model.Pane, *vt.Screen) (string, bool)) stringVar {
	return pv(func(p *model.Pane) (string, bool) {
		if p.Term == nil {
			return "", false
		}
		p.Mu.Lock()
		defer p.Mu.Unlock()
		return f(p, p.Term.Screen)
	})
}

func bv(f func(*buffers.Buffer) (string, bool)) stringVar {
	return func(c *Context) (string, bool) {
		if c.Buffer == nil {
			return "", false
		}
		return f(c.Buffer)
	}
}

func cv(f func(*Client) (string, bool)) stringVar {
	return func(c *Context) (string, bool) {
		if c.Client == nil {
			return "", false
		}
		return f(c.Client)
	}
}

func modeFlag(mode int) stringVar {
	return screenVar(func(_ *model.Pane, s *vt.Screen) (string, bool) { return flag(s.Mode&mode != 0) })
}

// paneTitle is the pane's title (#{pane_title}).
func paneTitle(p *model.Pane) string {
	if p.Term == nil {
		return ""
	}
	p.Mu.Lock()
	defer p.Mu.Unlock()
	return p.Term.Screen.Title
}

// paneIndex is window_pane_index: the pane's place counted from pane-base-index.
func paneIndex(p *model.Pane) (int, bool) {
	w := p.Window
	i := w.PaneIndex(p)
	if i < 0 {
		return 0, false
	}
	return i + int(w.Options.GetNumber("pane-base-index")), true
}

// windowFlags is window_printable_flags.
func windowFlags(wl *model.Winlink, escape bool) string {
	s := wl.Session
	var b strings.Builder
	if s.Current == wl {
		b.WriteByte('*')
	}
	if s.LastWindow() == wl {
		b.WriteByte('-')
	}
	if wl.Window.Zoomed() {
		b.WriteByte('Z')
	}
	_ = escape // the activity flag (## when escaped) comes with alerts (task C4.5)
	return b.String()
}

// stringVars are the variables, built when first used: a command-line client seldom expands
// a format, and the table is the largest of the program's start-up work.
var stringVars map[string]stringVar

var stringVarsOnce sync.Once

var timeVars = map[string]timeVar{
	"client_activity": func(c *Context) (time.Time, bool) {
		if c.Client == nil {
			return time.Time{}, false
		}
		return c.Client.Activity, true
	},
	"client_created": func(c *Context) (time.Time, bool) {
		if c.Client == nil {
			return time.Time{}, false
		}
		return c.Client.Created, true
	},
	"session_activity": func(c *Context) (time.Time, bool) {
		if c.S == nil {
			return time.Time{}, false
		}
		return c.S.Activity, true
	},
	"buffer_created": func(c *Context) (time.Time, bool) {
		if c.Buffer == nil {
			return time.Time{}, false
		}
		return c.Buffer.Created, true
	},
	"session_created": func(c *Context) (time.Time, bool) {
		if c.S == nil {
			return time.Time{}, false
		}
		return c.S.Created, true
	},
	"pane_dead_time": func(c *Context) (time.Time, bool) {
		if c.P == nil || c.P.DeadTime.IsZero() {
			return time.Time{}, false
		}
		return c.P.DeadTime, true
	},
	"start_time": func(c *Context) (time.Time, bool) {
		if c.Server == nil {
			return time.Time{}, false
		}
		return c.Server.Start, true
	},
}

func buildStringVars() {
	stringVars = map[string]stringVar{
		// Paste buffers (list-buffers).
		"buffer_name":   bv(func(b *buffers.Buffer) (string, bool) { return b.Name, true }),
		"buffer_size":   bv(func(b *buffers.Buffer) (string, bool) { return itoa(len(b.Data)) }),
		"buffer_sample": bv(func(b *buffers.Buffer) (string, bool) { return buffers.Sample(b), true }),
		"buffer_full":   bv(func(b *buffers.Buffer) (string, bool) { return string(b.Data), true }),
		// Server.
		"host":       func(*Context) (string, bool) { return host(), true },
		"host_short": func(*Context) (string, bool) { h, _, _ := strings.Cut(host(), "."); return h, true },
		"pid": func(c *Context) (string, bool) {
			if c.Server == nil {
				return "", false
			}
			return itoa(c.Server.Pid)
		},
		"socket_path": func(c *Context) (string, bool) {
			if c.Server == nil {
				return "", false
			}
			return c.Server.SocketPath, true
		},
		"version": func(c *Context) (string, bool) {
			if c.Server == nil {
				return "", false
			}
			return c.Server.Version, true
		},
		"config_files": func(c *Context) (string, bool) {
			if c.Server == nil {
				return "", true
			}
			return strings.Join(c.Server.ConfigFiles, ","), true
		},
		"server_sessions": func(c *Context) (string, bool) {
			if c.Tree == nil {
				return "0", true
			}
			return itoa(len(c.Tree.Sessions()))
		},
		"sixel_support": func(*Context) (string, bool) { return "0", true },

		// Sessions.
		"session_name":    sv(func(s *model.Session) (string, bool) { return s.Name, true }),
		"session_id":      sv(func(s *model.Session) (string, bool) { return "$" + strconv.Itoa(s.ID), true }),
		"session_path":    sv(func(s *model.Session) (string, bool) { return s.Cwd, true }),
		"session_windows": sv(func(s *model.Session) (string, bool) { return itoa(len(s.Links())) }),
		"session_attached": sv(func(s *model.Session) (string, bool) {
			return itoa(s.Attached)
		}),
		"session_many_attached": sv(func(s *model.Session) (string, bool) { return flag(s.Attached > 1) }),
		"session_attached_list": func(c *Context) (string, bool) {
			if c.S == nil {
				return "", false
			}
			var names []string
			for _, cl := range c.Clients {
				if cl.Session == c.S {
					names = append(names, cl.Name)
				}
			}
			return strings.Join(names, ","), true
		},
		"session_active": func(c *Context) (string, bool) {
			if c.S == nil || c.Client == nil {
				return "", false
			}
			return flag(c.Client.Session == c.S)
		},
		"session_format":        func(c *Context) (string, bool) { return flag(c.Type == TypeSession) },
		"session_grouped":       sv(func(*model.Session) (string, bool) { return "0", true }),
		"session_group_size":    sv(func(*model.Session) (string, bool) { return "0", true }),
		"session_marked":        sv(func(*model.Session) (string, bool) { return "0", true }),
		"session_activity_flag": sv(func(*model.Session) (string, bool) { return "0", true }),
		"session_bell_flag":     sv(func(*model.Session) (string, bool) { return "0", true }),
		"session_silence_flag":  sv(func(*model.Session) (string, bool) { return "0", true }),
		"session_alerts":        sv(func(*model.Session) (string, bool) { return "", true }),
		"session_alert":         sv(func(*model.Session) (string, bool) { return "", true }),
		"session_stack": sv(func(s *model.Session) (string, bool) {
			parts := []string{strconv.Itoa(s.Current.Index)}
			for _, wl := range s.LastWindows() {
				parts = append(parts, strconv.Itoa(wl.Index))
			}
			return strings.Join(parts, ","), true
		}),
		"active_window_index": sv(func(s *model.Session) (string, bool) { return itoa(s.Current.Index) }),
		"last_window_index": sv(func(s *model.Session) (string, bool) {
			links := s.Links()
			return itoa(links[len(links)-1].Index)
		}),
		"next_session_id": func(c *Context) (string, bool) {
			if c.Tree == nil {
				return "$0", true
			}
			return "$" + strconv.Itoa(c.Tree.NextSessionID()), true
		},

		// Windows.
		"window_id":    wv(func(w *model.Window) (string, bool) { return "@" + strconv.Itoa(w.ID), true }),
		"window_name":  wv(func(w *model.Window) (string, bool) { return w.Name, true }),
		"window_width": wv(func(w *model.Window) (string, bool) { return itoa(w.Sx) }),
		"window_height": wv(func(w *model.Window) (string, bool) {
			return itoa(w.Sy)
		}),
		"window_panes": wv(func(w *model.Window) (string, bool) { return itoa(len(w.Panes)) }),
		"window_zoomed_flag": wv(func(w *model.Window) (string, bool) {
			return flag(w.Zoomed())
		}),
		"window_layout": func(c *Context) (string, bool) {
			if c.W == nil || c.W.Layout == nil {
				return "", false
			}
			return c.W.LayoutString(false, c.ItemClient != nil && c.ItemClient.Control, int(c.W.Options.GetNumber("pane-base-index"))), true
		},
		"window_visible_layout": func(c *Context) (string, bool) {
			if c.W == nil || c.W.Layout == nil {
				return "", false
			}
			return c.W.LayoutString(true, c.Client != nil && c.Client.Control, int(c.W.Options.GetNumber("pane-base-index"))), true
		},
		"window_manual_width": wv(func(w *model.Window) (string, bool) {
			if w.Options.GetNumber("window-size") != 2 {
				return "", true
			}
			return itoa(w.ManualSx)
		}),
		"window_manual_height": wv(func(w *model.Window) (string, bool) {
			if w.Options.GetNumber("window-size") != 2 {
				return "", true
			}
			return itoa(w.ManualSy)
		}),
		"window_cell_width":  wv(func(*model.Window) (string, bool) { return "0", true }),
		"window_cell_height": wv(func(*model.Window) (string, bool) { return "0", true }),
		"window_format":      func(c *Context) (string, bool) { return flag(c.Type == TypeWindow) },
		"window_index":       wlv(func(wl *model.Winlink) (string, bool) { return itoa(wl.Index) }),
		"window_active":      wlv(func(wl *model.Winlink) (string, bool) { return flag(wl.Session.Current == wl) }),
		"window_last_flag": wlv(func(wl *model.Winlink) (string, bool) {
			return flag(wl.Session.LastWindow() == wl)
		}),
		"window_flags":     wlv(func(wl *model.Winlink) (string, bool) { return windowFlags(wl, true), true }),
		"window_raw_flags": wlv(func(wl *model.Winlink) (string, bool) { return windowFlags(wl, false), true }),
		"window_start_flag": wlv(func(wl *model.Winlink) (string, bool) {
			return flag(wl.Session.Links()[0] == wl)
		}),
		"window_end_flag": wlv(func(wl *model.Winlink) (string, bool) {
			links := wl.Session.Links()
			return flag(links[len(links)-1] == wl)
		}),
		"window_activity_flag":   wlv(func(*model.Winlink) (string, bool) { return "0", true }),
		"window_bell_flag":       wlv(func(*model.Winlink) (string, bool) { return "0", true }),
		"window_silence_flag":    wlv(func(*model.Winlink) (string, bool) { return "0", true }),
		"window_marked_flag":     wlv(func(*model.Winlink) (string, bool) { return "0", true }),
		"window_linked":          wlv(func(*model.Winlink) (string, bool) { return "0", true }),
		"window_linked_sessions": wlv(func(*model.Winlink) (string, bool) { return "1", true }),
		"window_stack_index": wlv(func(wl *model.Winlink) (string, bool) {
			for i, l := range wl.Session.LastWindows() {
				if l == wl {
					return itoa(i + 1)
				}
			}
			return "0", true
		}),
		"window_bigger": func(c *Context) (string, bool) {
			if c.Client == nil {
				return "", false
			}
			_, _, bigger := windowOffset(c)
			return flag(bigger)
		},
		"window_offset_x": func(c *Context) (string, bool) {
			if c.Client == nil {
				return "", false
			}
			ox, _, bigger := windowOffset(c)
			if !bigger {
				return "", false
			}
			return itoa(ox)
		},
		"window_offset_y": func(c *Context) (string, bool) {
			if c.Client == nil {
				return "", false
			}
			_, oy, bigger := windowOffset(c)
			if !bigger {
				return "", false
			}
			return itoa(oy)
		},

		// Panes.
		"pane_id":     pv(func(p *model.Pane) (string, bool) { return "%" + strconv.Itoa(p.ID), true }),
		"pane_width":  pv(func(p *model.Pane) (string, bool) { return itoa(p.Sx) }),
		"pane_height": pv(func(p *model.Pane) (string, bool) { return itoa(p.Sy) }),
		"pane_left":   pv(func(p *model.Pane) (string, bool) { return itoa(p.Xoff) }),
		"pane_top":    pv(func(p *model.Pane) (string, bool) { return itoa(p.Yoff) }),
		"pane_right":  pv(func(p *model.Pane) (string, bool) { return itoa(p.Xoff + p.Sx - 1) }),
		"pane_bottom": pv(func(p *model.Pane) (string, bool) { return itoa(p.Yoff + p.Sy - 1) }),
		"pane_at_left": pv(func(p *model.Pane) (string, bool) {
			return flag(p.Xoff == 0)
		}),
		"pane_at_top": pv(func(p *model.Pane) (string, bool) {
			// The pane-border-status line at the top counts as part of the window.
			top := 0
			if p.Window.Options.GetNumber("pane-border-status") == 1 {
				top = 1
			}
			return flag(p.Yoff == top)
		}),
		"pane_at_right": pv(func(p *model.Pane) (string, bool) {
			return flag(p.Xoff+p.Sx == p.Window.Sx)
		}),
		"pane_at_bottom": pv(func(p *model.Pane) (string, bool) {
			bottom := p.Window.Sy
			if p.Window.Options.GetNumber("pane-border-status") == 2 {
				bottom--
			}
			return flag(p.Yoff+p.Sy == bottom)
		}),
		"pane_active": pv(func(p *model.Pane) (string, bool) { return flag(p.Window.Active == p) }),
		"pane_fg": func(c *Context) (string, bool) {
			if c.P == nil {
				return "", false
			}
			return vt.ColorToString(paneColour(c, c.P, false)), true
		},
		"pane_bg": func(c *Context) (string, bool) {
			if c.P == nil {
				return "", false
			}
			return vt.ColorToString(paneColour(c, c.P, true)), true
		},
		"pane_flags": pv(func(p *model.Pane) (string, bool) {
			// window_pane_printable_flags.
			w := p.Window
			var b strings.Builder
			if w.Active == p {
				b.WriteByte('*')
			}
			if w.LastPane() == p {
				b.WriteByte('-')
			}
			if w.Zoomed() && w.Active == p {
				b.WriteByte('Z')
			}
			return b.String(), true
		}),
		"pane_last": pv(func(p *model.Pane) (string, bool) { return flag(p.Window.LastPane() == p) }),
		"pane_index": pv(func(p *model.Pane) (string, bool) {
			i, ok := paneIndex(p)
			if !ok {
				return "", false
			}
			return itoa(i)
		}),
		"pane_format": func(c *Context) (string, bool) { return flag(c.Type == TypePane) },
		"pane_title":  pv(func(p *model.Pane) (string, bool) { return paneTitle(p), true }),
		"pane_dead":   pv(func(p *model.Pane) (string, bool) { return flag(p.Dead) }),
		"pane_dead_status": pv(func(p *model.Pane) (string, bool) {
			if !p.Dead || p.Status.Signal != 0 {
				return "", false
			}
			return itoa(p.Status.Code)
		}),
		"pane_dead_signal": pv(func(p *model.Pane) (string, bool) {
			if !p.Dead || p.Status.Signal == 0 {
				return "", false
			}
			return sig2name(p.Status.Signal), true
		}),
		"pane_pid": pv(func(p *model.Pane) (string, bool) {
			if p.Dead || p.Proc == nil {
				return "", false
			}
			return itoa(p.Proc.Pid())
		}),
		"pane_in_mode": pv(func(p *model.Pane) (string, bool) { return boolString(p.Mode != nil), true }),
		"pane_mode": pv(func(p *model.Pane) (string, bool) {
			if p.Mode == nil {
				return "", false
			}
			return p.Mode.Name(), true
		}),
		"pane_input_off": pv(func(*model.Pane) (string, bool) { return "0", true }),
		"pane_marked":    pv(func(*model.Pane) (string, bool) { return "0", true }),
		"pane_marked_set": pv(func(*model.Pane) (string, bool) {
			return "0", true
		}),
		"pane_pipe":            pv(func(*model.Pane) (string, bool) { return "0", true }),
		"pane_unseen_changes":  pv(func(*model.Pane) (string, bool) { return "0", true }),
		"pane_search_string":   pv(func(*model.Pane) (string, bool) { return "", true }),
		"pane_floating_flag":   pv(func(*model.Pane) (string, bool) { return "0", true }),
		"pane_modal_flag":      pv(func(*model.Pane) (string, bool) { return "0", true }),
		"pane_command_running": pv(func(*model.Pane) (string, bool) { return "0", true }),
		"pane_output_generation": pv(func(*model.Pane) (string, bool) {
			return "0", true
		}),
		"pane_synchronized": pv(func(p *model.Pane) (string, bool) {
			return flag(p.Options.GetNumber("synchronize-panes") != 0)
		}),
		"pane_zoomed_flag": pv(func(p *model.Pane) (string, bool) {
			return flag(p.Window.Zoomed() && p.Window.Active == p)
		}),
		"pane_start_command": pv(func(p *model.Pane) (string, bool) {
			words := make([]string, len(p.Argv))
			for i, a := range p.Argv {
				words[i] = cmdlang.Escape(a)
			}
			return strings.Join(words, " "), true
		}),
		"pane_start_path": pv(func(p *model.Pane) (string, bool) { return p.Cwd, true }),
		"pane_current_command": pv(func(p *model.Pane) (string, bool) {
			if p.Shell == "" {
				return "", false
			}
			cmd := ""
			if p.Proc != nil && !p.Dead {
				cmd = platform.ForegroundCommand(p.Proc.Fd())
			}
			if cmd == "" {
				words := make([]string, len(p.Argv))
				for i, a := range p.Argv {
					words[i] = cmdlang.Escape(a)
				}
				cmd = strings.Join(words, " ")
			}
			if cmd == "" {
				cmd = p.Shell
			}
			return ParseWindowName(cmd), true
		}),
		"pane_current_path": pv(func(p *model.Pane) (string, bool) {
			if p.Proc == nil || p.Dead {
				return "", false
			}
			if platform.CwdFromScreen {
				// K1: the last OSC 7 or OSC 9;9 path, else where the pane started.
				cwd := p.Cwd
				if p.Term != nil {
					p.Mu.Lock()
					if c := p.Term.Screen.Cwd; c != "" {
						cwd = c
					}
					p.Mu.Unlock()
				}
				return cwd, cwd != ""
			}
			cwd := platform.CurrentPath(p.Proc.Fd())
			return cwd, cwd != ""
		}),
		"pane_tabs": screenVar(func(_ *model.Pane, s *vt.Screen) (string, bool) {
			var stops []string
			for x := 0; x < s.Grid.Sx; x++ {
				if s.IsTab(x) {
					stops = append(stops, strconv.Itoa(x))
				}
			}
			return strings.Join(stops, ","), true
		}),
		"cursor_x": screenVar(func(_ *model.Pane, s *vt.Screen) (string, bool) { return itoa(s.Cx) }),
		"cursor_y": screenVar(func(_ *model.Pane, s *vt.Screen) (string, bool) { return itoa(s.Cy) }),
		"history_size": screenVar(func(_ *model.Pane, s *vt.Screen) (string, bool) {
			return itoa(s.Grid.HSize)
		}),
		"history_bytes": screenVar(func(_ *model.Pane, s *vt.Screen) (string, bool) {
			return itoa(s.Grid.Bytes())
		}),
		"history_limit": screenVar(func(_ *model.Pane, s *vt.Screen) (string, bool) {
			return itoa(s.Grid.HLimit)
		}),
		"alternate_on": screenVar(func(_ *model.Pane, s *vt.Screen) (string, bool) {
			return flag(s.IsAlternate())
		}),
		"cursor_flag":              modeFlag(vt.ModeCursor),
		"insert_flag":              modeFlag(vt.ModeInsert),
		"keypad_cursor_flag":       modeFlag(vt.ModeKCursor),
		"keypad_flag":              modeFlag(vt.ModeKKeypad),
		"wrap_flag":                modeFlag(vt.ModeWrap),
		"origin_flag":              modeFlag(vt.ModeOrigin),
		"bracket_paste_flag":       modeFlag(vt.ModeBracketPaste),
		"mouse_any_flag":           modeFlag(vt.AllMouseModes),
		"mouse_standard_flag":      modeFlag(vt.ModeMouseStandard),
		"mouse_button_flag":        modeFlag(vt.ModeMouseButton),
		"mouse_all_flag":           modeFlag(vt.ModeMouseAll),
		"mouse_sgr_flag":           modeFlag(vt.ModeMouseSGR),
		"mouse_utf8_flag":          modeFlag(vt.ModeMouseUTF8),
		"synchronized_output_flag": modeFlag(vt.ModeSync),
		"pane_key_mode": screenVar(func(_ *model.Pane, s *vt.Screen) (string, bool) {
			if s.Mode&vt.ModeKeysExtended != 0 {
				return "Ext 1", true
			}
			return "VT10x", true
		}),

		// The mouse.
		"mouse_pane": func(c *Context) (string, bool) {
			if c.Mouse == nil || !c.Mouse.Valid || c.Mouse.Pane == nil {
				return "", false
			}
			return "%" + strconv.Itoa(c.Mouse.Pane.ID), true
		},
		"mouse_x": func(c *Context) (string, bool) {
			if c.Mouse == nil || !c.Mouse.Valid || !c.Mouse.HasXY {
				return "", false
			}
			return itoa(c.Mouse.X)
		},
		"mouse_y": func(c *Context) (string, bool) {
			if c.Mouse == nil || !c.Mouse.Valid || !c.Mouse.HasXY {
				return "", false
			}
			return itoa(c.Mouse.Y)
		},
		"mouse_status_range": func(c *Context) (string, bool) {
			if c.Mouse == nil || !c.Mouse.Valid || !c.Mouse.HasRange {
				return "", false
			}
			return c.Mouse.StatusRange, true
		},
		"mouse_status_line": func(c *Context) (string, bool) {
			if c.Mouse == nil || !c.Mouse.Valid || c.Mouse.StatusLine < 0 {
				return "", false
			}
			return itoa(c.Mouse.StatusLine)
		},

		// Clients.
		"client_name": cv(func(c *Client) (string, bool) { return c.Name, c.Name != "" }),
		"client_tty":  cv(func(c *Client) (string, bool) { return c.TTY, c.TTY != "" }),
		"client_pid":  cv(func(c *Client) (string, bool) { return itoa(c.Pid) }),
		"client_session": cv(func(c *Client) (string, bool) {
			if c.Session == nil {
				return "", false
			}
			return c.Session.Name, true
		}),
		"client_last_session": cv(func(c *Client) (string, bool) {
			if c.Last == nil {
				return "", false
			}
			return c.Last.Name, true
		}),
		"client_width": cv(func(c *Client) (string, bool) { return itoa(c.Width) }),
		"client_height": cv(func(c *Client) (string, bool) {
			if !c.Started {
				return "", false
			}
			return itoa(c.Height)
		}),
		"client_cell_width": cv(func(c *Client) (string, bool) {
			if !c.Started {
				return "", false
			}
			return itoa(c.CellWidth)
		}),
		"client_cell_height": cv(func(c *Client) (string, bool) {
			if !c.Started {
				return "", false
			}
			return itoa(c.CellHeight)
		}),
		"client_colours": cv(func(c *Client) (string, bool) {
			if !c.Started {
				return "", false
			}
			return itoa(c.Colours)
		}),
		"client_control_mode": cv(func(c *Client) (string, bool) { return flag(c.Control) }),
		"client_readonly":     cv(func(c *Client) (string, bool) { return flag(c.ReadOnly) }),
		"client_utf8":         cv(func(c *Client) (string, bool) { return flag(c.UTF8) }),
		"client_flags":        cv(func(c *Client) (string, bool) { return strings.Join(c.Flags, ","), true }),
		"client_termname":     cv(func(c *Client) (string, bool) { return c.TermName, c.TermName != "" }),
		"client_termtype":     cv(func(c *Client) (string, bool) { return c.TermType, true }),
		"client_termfeatures": cv(func(c *Client) (string, bool) { return c.Features, true }),
		"client_key_table":    cv(func(c *Client) (string, bool) { return c.KeyTable, true }),
		"client_prefix":       cv(func(c *Client) (string, bool) { return flag(c.Prefix) }),
		"client_theme":        cv(func(c *Client) (string, bool) { return c.Theme, c.Theme != "" }),
		"client_discarded":    cv(func(c *Client) (string, bool) { return itoa(c.Discarded) }),
		"client_written":      cv(func(c *Client) (string, bool) { return itoa(c.Written) }),
		"client_uid":          cv(func(c *Client) (string, bool) { return itoa(c.UID) }),
		"client_user": cv(func(c *Client) (string, bool) {
			if c.User != "" {
				return c.User, true
			}
			u := userName(c.UID)
			return u, u != ""
		}),
		"uid":  func(*Context) (string, bool) { return itoa(os.Getuid()) },
		"user": func(*Context) (string, bool) { u := userName(os.Getuid()); return u, u != "" },
	}
}

// windowOffset is tty_window_offset for the format's client: where the client's view of a
// window bigger than it starts. Only the top left is shown so far (C4.x follows the cursor).
func windowOffset(c *Context) (ox, oy int, bigger bool) {
	cl := c.Client
	if cl.Session == nil || cl.Session.Current == nil {
		return 0, 0, false
	}
	w := cl.Session.Current.Window
	lines := 0
	if cl.Session.Options.GetNumber("status") != 0 {
		lines = int(cl.Session.Options.GetNumber("status"))
	}
	return 0, 0, w.Sx > cl.Width || w.Sy > cl.Height-lines
}

// paneColour is tty_default_colours: the pane's default foreground or background, from
// window-active-style when the pane is active and it sets one, else window-style. Only the
// fg= and bg= of the styles are read here; the whole style parser comes with task C4.1.
func paneColour(c *Context, p *model.Pane, bg bool) vt.Color {
	get := func(name string) vt.Color {
		ctx := &Context{Tree: c.Tree, Server: c.Server, Clients: c.Clients, Now: c.Now, Environ: c.Environ}
		ctx.Defaults(nil, nil, nil, p)
		return styleColour(ctx.Expand(p.Options.GetString(name)), bg)
	}
	if p.Window.Active == p {
		if col := get("window-active-style"); col != vt.ColorDefault {
			return col
		}
	}
	return get("window-style")
}

// styleColour is the last fg= (or bg=) of a style, or the default colour.
func styleColour(style string, bg bool) vt.Color {
	key := "fg="
	if bg {
		key = "bg="
	}
	col := vt.ColorDefault
	for _, tok := range strings.FieldsFunc(style, func(r rune) bool { return r == ' ' || r == ',' }) {
		switch {
		case strings.EqualFold(tok, "default"):
			col = vt.ColorDefault
		case len(tok) > 3 && strings.EqualFold(tok[:3], key):
			if v := vt.ColorFromString(tok[3:]); v != -1 {
				col = v
			}
		}
	}
	return col
}

// userName is the name of the user with id uid (getpwuid), or "".
func userName(uid int) string {
	u, err := user.LookupId(strconv.Itoa(uid))
	if err != nil {
		return ""
	}
	return u.Username
}

// lookupStringVar is the variable called key, if there is one.
func lookupStringVar(key string) (stringVar, bool) {
	stringVarsOnce.Do(buildStringVars)
	v, ok := stringVars[key]
	return v, ok
}
