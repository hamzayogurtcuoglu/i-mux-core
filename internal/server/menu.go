// Ported in part from tmux 3.8's menu.c and server-client.c:
//
// Copyright (c) 2019 Nicholas Marriott <nicholas.marriott@gmail.com>
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
	"math"
	"strings"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/commands"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/format"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/input"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/keys"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/model"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/render"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/style"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/vt"
)

// Menus (TASKS.md C4.7): a window's menu (tmux 3.8's w->menu), drawn over its panes for every
// client showing the window; the keys of those clients go to it.

// Mouse wheel buttons (MOUSE_WHEEL_UP and MOUSE_WHEEL_DOWN).
const (
	mouseWheelUp   = 64
	mouseWheelDown = 65
)

// menu is tmux's menu_data.
type menu struct {
	w      *model.Window
	spec   commands.MenuSpec
	lines  int
	px, py int
	choice int
}

// DisplayMenu implements commands.Host: menu_display.
func (s *Server) DisplayMenu(spec commands.MenuSpec) {
	w := spec.Window
	if w == nil {
		return
	}
	for mw := range s.menus {
		if s.tree.Window(mw.ID) == nil {
			delete(s.menus, mw)
		}
	}
	items := spec.Items
	sx, sy := spec.Width+4, len(items)+2
	px, py := spec.X, spec.Y
	if sx >= w.Sx {
		px = 0
	} else if px+sx > w.Sx {
		px = w.Sx - sx
	}
	if sy >= w.Sy {
		py = 0
	} else if py+sy > w.Sy {
		py = w.Sy - sy
	}
	w.MenuLastX, w.MenuLastY = px, py
	lines := spec.Lines
	if lines == -1 {
		lines = int(w.Options.GetNumber("menu-border-lines"))
	}
	md := &menu{w: w, spec: spec, lines: lines, px: px, py: py, choice: -1}
	enabled := func(i int) bool {
		n := items[i].Name
		return n != nil && (*n == "" || (*n)[0] != '-')
	}
	if spec.Flags&commands.MenuNoMouse != 0 {
		count := len(items)
		switch start := spec.StartingChoice; {
		case start >= count:
			start = count - 1
			choice := start + 1
			for {
				if enabled(choice - 1) {
					md.choice = choice - 1
					break
				}
				if choice--; choice == 0 {
					choice = count
				}
				if choice == start+1 {
					break
				}
			}
		case start >= 0:
			choice := start
			for {
				if enabled(choice) {
					md.choice = choice
					break
				}
				if choice++; choice == count {
					choice = 0
				}
				if choice == start {
					break
				}
			}
		}
	}
	s.menus[w] = md
	s.updateFocus(w)
	s.redrawAll()
}

// closeMenu is menu_close: the window's menu goes and the window is drawn again.
func (s *Server) closeMenu(w *model.Window) {
	if _, ok := s.menus[w]; !ok {
		return
	}
	delete(s.menus, w)
	s.updateFocus(w)
	s.redrawAll()
}

// windowMenu is the menu of the window a client shows, nil for none.
func (s *Server) windowMenu(c *attachedClient) *menu {
	w := c.window()
	if w == nil {
		return nil
	}
	return s.menus[w]
}

// menuView is menu_update with menu_reapply_styles (and menu_resize, for a window that shrank):
// the menu as the renderer draws it.
func (s *Server) menuView(md *menu) *render.Menu {
	w := md.w
	sx, sy := md.spec.Width+4, len(md.spec.Items)+2
	if md.px+sx > w.Sx {
		md.px = max(w.Sx-sx, 0)
	}
	if md.py+sy > w.Sy {
		md.py = max(w.Sy-sy, 0)
	}
	st := md.spec.State
	ctx := &format.Context{Tree: s.tree, Server: s.ServerInfo(), Clients: s.clientFormats(), Now: s.opts.Now}
	ctx.Defaults(nil, st.S, st.WL, st.P)
	apply := func(name string, over *string) vt.Cell {
		gc := vt.DefaultCell
		sy := style.Default()
		if sy.Parse(vt.DefaultCell, ctx.Expand(w.Options.GetString(name))) == nil {
			sy.Add(&gc)
		}
		if over != nil {
			tmp := style.FromCell(vt.DefaultCell)
			if tmp.Parse(gc, *over) == nil {
				gc.Fg, gc.Bg = tmp.Cell.Fg, tmp.Cell.Bg
			}
		}
		return gc
	}
	names := make([]*string, len(md.spec.Items))
	for i, it := range md.spec.Items {
		names[i] = it.Name
	}
	cells := render.MenuCells(md.spec.Width, md.spec.Title, names, md.choice, md.lines,
		apply("menu-style", md.spec.Style), apply("menu-border-style", md.spec.Border),
		apply("menu-selected-style", md.spec.Selected))
	mode := vt.ModeWrap
	if md.spec.Flags&commands.MenuNoMouse == 0 {
		mode |= vt.ModeMouseAll | vt.ModeMouseButton
	}
	return &render.Menu{X: md.px, Y: md.py, Sx: sx, Sy: sy, Cells: cells, Mode: mode, Choice: md.choice}
}

// menuMouse is server_client_handle_menu_key's half for the mouse: the event's position in
// the window, or math.MaxInt on the status line.
func (s *Server) menuMouse(c *attachedClient, m input.Mouse) (x, y int) {
	lines := s.statusLineSize(c)
	statusAt := -1
	if lines > 0 {
		if c.session.Options.GetNumber("status-position") == 0 {
			statusAt = 0
		} else {
			statusAt = c.tty.Sy - lines
		}
	}
	ox, oy := 0, 0
	if w := c.window(); w != nil {
		ox, oy = render.WindowOffset(w, c.tty.Sx, c.tty.Sy-lines)
	}
	x = m.X + ox
	switch {
	case statusAt == 0 && m.Y < lines:
		return math.MaxInt, math.MaxInt
	case statusAt == 0:
		y = m.Y - lines + oy
	case statusAt > 0 && m.Y >= statusAt:
		return math.MaxInt, math.MaxInt
	default:
		y = m.Y + oy
	}
	return x, y
}

// menuKey is menu_key: what a key does to the window's menu. It reports whether the menu
// closes.
func (s *Server) menuKey(c *attachedClient, md *menu, ev *input.Event) bool {
	items := md.spec.Items
	n := len(items)
	old := md.choice
	redraw := func() { s.redrawAll() }
	enabled := func(i int) bool {
		name := items[i].Name
		return name != nil && (*name == "" || (*name)[0] != '-')
	}
	chosen := func() bool {
		if md.choice == -1 {
			return true
		}
		item := items[md.choice]
		if !enabled(md.choice) {
			return md.spec.Flags&commands.MenuStayOpen == 0
		}
		s.runMenuCommand(c, md, item.Command)
		return true
	}

	key := ev.Key
	if key.IsMouse() {
		m := ev.Mouse
		buttons := m.B & keys.MouseMaskButtons
		release := buttons == 3
		drag := m.B&keys.MouseMaskDrag != 0
		wheel := buttons == mouseWheelUp || buttons == mouseWheelDown
		move := drag && release
		if md.spec.Flags&commands.MenuNoMouse != 0 {
			return buttons != keys.MouseButton1
		}
		x, y := s.menuMouse(c, m)
		if x < md.px || x > md.px+4+md.spec.Width || y < md.py+1 || y > md.py+1+n-1 {
			if md.spec.Flags&commands.MenuStayOpen == 0 {
				if !move && release {
					return true
				}
			} else if !release && !wheel && !drag {
				return true
			}
			if md.choice != -1 {
				md.choice = -1
				redraw()
			}
			return false
		}
		if md.spec.Flags&commands.MenuStayOpen == 0 {
			if !move && release {
				return chosen()
			}
		} else if !wheel && !drag {
			return chosen()
		}
		md.choice = y - (md.py + 1)
		if md.choice != old {
			redraw()
		}
		return false
	}

	k := key &^ keys.MaskFlags
	for i, it := range items {
		if !enabled(i) || it.Name == nil {
			continue
		}
		if k == it.Key&^keys.MaskFlags {
			md.choice = i
			return chosen()
		}
	}
	up := func() {
		if old == -1 {
			old = 0
		}
		for {
			if md.choice == -1 || md.choice == 0 {
				md.choice = n - 1
			} else {
				md.choice--
			}
			if enabled(md.choice) || md.choice == old {
				break
			}
		}
		redraw()
	}
	down := func() {
		if old == -1 {
			old = 0
		}
		for {
			if md.choice == -1 || md.choice == n-1 {
				md.choice = 0
			} else {
				md.choice++
			}
			if enabled(md.choice) || md.choice == old {
				break
			}
		}
		redraw()
	}
	switch k {
	case keys.BTab, keys.Up, 'k':
		up()
	case keys.BSpace:
		if md.spec.Flags&commands.MenuTab != 0 {
			return true
		}
	case keys.C0HT:
		if md.spec.Flags&commands.MenuTab == 0 {
			break
		}
		if md.choice == n-1 {
			return true
		}
		down()
	case keys.Down, 'j':
		down()
	case keys.PPage, 'b' | keys.Ctrl:
		if md.choice < 6 {
			md.choice = 0
		} else {
			for i := 5; i > 0; {
				md.choice--
				if md.choice != 0 && enabled(md.choice) {
					i--
				} else if md.choice == 0 {
					break
				}
			}
		}
		redraw()
	case keys.NPage:
		if md.choice > n-6 {
			md.choice = n - 1
		} else {
			for i := 5; i > 0; {
				md.choice++
				if md.choice != n-1 && enabled(md.choice) {
					i--
				} else if md.choice == n-1 {
					break
				}
			}
		}
		for !enabled(md.choice) && md.choice != 0 {
			md.choice--
		}
		redraw()
	case 'g', keys.Home:
		md.choice = 0
		for !enabled(md.choice) && md.choice != n-1 {
			md.choice++
		}
		redraw()
	case 'G', keys.End:
		md.choice = n - 1
		for !enabled(md.choice) && md.choice != 0 {
			md.choice--
		}
		redraw()
	case keys.C0CR:
		return chosen()
	case keys.C0ESC, '[' | keys.Ctrl, 'c' | keys.Ctrl, 'g' | keys.Ctrl, 'q':
		return true
	}
	return false
}

// runMenuCommand is menu_key's choice: the item's command runs for the client, in the menu's
// target; errors show on the client's message line.
func (s *Server) runMenuCommand(c *attachedClient, md *menu, command string) {
	list, err := commands.ParseCommandString(command)
	if err != nil {
		msg := err.Error()
		s.ShowMessage(c.name, strings.ToUpper(msg[:1])+msg[1:], commands.MessageOptions{Delay: -1, IgnoreStyles: true})
		return
	}
	if list == nil {
		return
	}
	st := md.spec.State
	commands.RunList(s, &commands.Client{Name: c.name, Session: c.session, Env: c.env}, list, &st, md.spec.Mouse, func(res commands.Result) {
		s.showResult(c, res)
		s.redrawAll()
	})
}
