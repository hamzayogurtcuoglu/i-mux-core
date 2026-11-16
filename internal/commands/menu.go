// Ported in part from tmux 3.8's cmd-display-menu.c and menu.c:
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

package commands

import (
	"errors"
	"fmt"
	"math"
	"strconv"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/cmdlang"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/format"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/keys"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/model"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/style"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/target"
)

// Menu flags (tmux's MENU_*).
const (
	MenuNoMouse  = 0x1 // the menu ignores the mouse (opened by a key, without -M)
	MenuTab      = 0x2
	MenuStayOpen = 0x4 // -O: a button release does not close it
)

// MenuItem is a menu's item (struct menu_item): its name as drawn (a format, with the key on
// the right; a leading - disables it) and its command, both expanded; a nil Name is a
// separator.
type MenuItem struct {
	Name    *string
	Command string
	Key     keys.Key
}

// MenuSpec is a menu to show on a window (menu_display's arguments).
type MenuSpec struct {
	Client string // the client that opened it
	Window *model.Window
	// State is the target the chosen command runs in (the menu's cmd_find_state).
	State          target.State
	Title          string
	Items          []MenuItem
	Width          int // the widest item or title, in cells
	X, Y           int // in the window
	Flags          int
	Lines          int // menu-border-lines' index, or -1 for the option
	StartingChoice int // -C: the first item chosen, or -1
	// Style, Selected and Border are -s, -H and -S, nil when not given.
	Style, Selected, Border *string
	// Key is the key that opened the menu, given to the chosen command.
	Key keys.Key
}

// addMenuItem is menu_add_item: an item, or a separator for a nil name, with its name and
// command expanded for the target client c (maxWidth is its width less 4).
func addMenuItem(items []MenuItem, width *int, name *string, key keys.Key, command string, ctx *format.Context, maxWidth int) []MenuItem {
	line := name == nil || *name == ""
	if line && len(items) == 0 {
		return items
	}
	if line && items[len(items)-1].Name == nil {
		return items
	}
	if line {
		return append(items, MenuItem{})
	}

	s := ctx.Expand(*name)
	if s == "" {
		return items
	}
	keyName, hasKey := "", false
	if s[0] != '-' && key != keys.Unknown && key != keys.None {
		keyName, hasKey = keys.Name(key, false), true
		keylen := len(keyName) + 3
		if keylen <= maxWidth/4 {
			maxWidth -= keylen
		} else if keylen >= maxWidth || len(s) >= maxWidth-keylen {
			hasKey = false
		}
	}
	suffix := ""
	if len(s) > maxWidth {
		maxWidth--
		suffix = ">"
	}
	trimmed := format.TrimRight(s, maxWidth)
	full := trimmed + suffix
	if hasKey {
		full = trimmed + suffix + "#[default] #[align=right](" + keyName + ")"
	}
	w := format.Width(full)
	if full[0] == '-' {
		w--
	}
	*width = max(*width, w)
	return append(items, MenuItem{Name: &full, Command: ctx.Expand(command), Key: key})
}

// menuFormats adds cmd_display_menu_get_menu_pos's popup_* variables for a menu of w×h on the
// window w0 to values (the mouse ones come with the mouse, task C5.1).
func menuFormats(values map[string]string, g ClientGeometry, st target.State, w0 *model.Window, w, h int) {
	add := func(k string, n int) { values[k] = strconv.Itoa(n) }
	maxY := max(w0.Sy-h, 0)
	add("popup_last_x", w0.MenuLastX)
	add("popup_last_y", w0.MenuLastY+h)
	if g.StatusAt != -1 && g.StatusLines != 0 {
		found, start := false, 0
		for _, rs := range g.Ranges {
			for _, r := range rs {
				if r.Type == style.RangeWindow && st.WL != nil && int(r.Arg) == st.WL.Index {
					found, start = true, r.Start
					break
				}
			}
			if found {
				break
			}
		}
		if found {
			add("popup_window_status_line_x", start+g.Ox)
			if g.StatusTop {
				add("popup_window_status_line_y", h)
			} else {
				add("popup_window_status_line_y", w0.Sy)
			}
		}
		if g.StatusTop {
			add("popup_status_line_y", h)
		} else {
			add("popup_status_line_y", w0.Sy)
		}
	}
	add("popup_width", w)
	add("popup_height", h)
	add("popup_centre_x", max((w0.Sx-1)/2-w/2, 0))
	if n := (w0.Sy-1)/2 + h/2; n >= w0.Sy {
		add("popup_centre_y", maxY)
	} else {
		add("popup_centre_y", n)
	}
	if p := st.P; p != nil {
		if n := p.Yoff + h; n >= w0.Sy {
			add("popup_pane_top", maxY)
		} else {
			add("popup_pane_top", n)
		}
		add("popup_pane_bottom", p.Yoff+p.Sy)
		add("popup_pane_left", p.Xoff)
		add("popup_pane_right", max(p.Xoff+p.Sx-w, 0))
	}
}

// menuPosition is cmd_display_menu_get_menu_pos: where a menu of w×h goes in the window, from
// -x and -y (numbers, formats, or the letters C R P M L W S).
func (it *Item) menuPosition(ctx *format.Context, g ClientGeometry, st target.State, w0 *model.Window, w, h int) (px, py int) {
	maxX, maxY := max(w0.Sx-w, 0), max(w0.Sy-h, 0)
	menuFormats(ctx.Values, g, st, w0, w, h)
	a := it.Cmd.Args

	xp, given := a.Get('x')
	switch {
	case !given || xp == "C":
		xp = "#{popup_centre_x}"
	case xp == "R":
		xp = "#{popup_pane_right}"
	case xp == "P":
		xp = "#{popup_pane_left}"
	case xp == "M":
		xp = "#{popup_mouse_centre_x}"
	case xp == "L":
		xp = "#{popup_last_x}"
	case xp == "W":
		xp = "#{popup_window_status_line_x}"
	}
	px = min(max(strtol(ctx.Expand(xp)), 0), maxX)

	yp, given := a.Get('y')
	switch {
	case !given || yp == "C":
		yp = "#{popup_centre_y}"
	case yp == "P":
		yp = "#{popup_pane_bottom}"
	case yp == "M":
		yp = "#{popup_mouse_top}"
	case yp == "L":
		yp = "#{popup_last_y}"
	case yp == "S":
		yp = "#{popup_status_line_y}"
	case yp == "W":
		yp = "#{popup_window_status_line_y}"
	}
	n := strtol(ctx.Expand(yp))
	if n < h {
		n = 0
	} else {
		n -= h
	}
	py = min(max(n, 0), maxY)
	return px, py
}

// menuLines is -b: a menu-border-lines choice, by index.
func menuLines(v string) (int, error) {
	n, err := popupLines(v)
	if err != nil {
		return 0, fmt.Errorf("menu-border-lines %s", err.Error()[len("popup-border-lines "):])
	}
	return n, nil
}

// displayMenu is display-menu: a menu on the target window, its items a name, a key and a
// command each, an empty name a separator.
func displayMenu(it *Item) error {
	a := it.Cmd.Args
	st, err := it.findFlag('t', target.Pane, 0)
	if err != nil {
		return err
	}
	tc, err := it.findClient('c')
	if err != nil {
		return err
	}
	ci := it.clientInfo(tc.Name)
	if ci == nil {
		return nil
	}

	starting := 0
	if v, ok := a.Get('C'); ok {
		if v == "-" {
			starting = -1
		} else {
			n, cause := strtonum(v, 0, math.MaxUint32)
			if cause != "" {
				return fmt.Errorf("starting choice %s", cause)
			}
			starting = n
		}
	}
	title := ""
	if v, ok := a.Get('T'); ok {
		title = it.formatSingle(st, v)
	}
	width := format.Width(title)
	ctx := it.formatContext(ci, st.S, st.WL, st.P)
	var items []MenuItem
	vals := a.Values
	for i := 0; i < len(vals); {
		name := vals[i].text()
		i++
		if name == "" {
			items = addMenuItem(items, &width, nil, keys.None, "", ctx, ci.Width-4)
			continue
		}
		if len(vals)-i < 2 {
			return errors.New("not enough arguments")
		}
		key := keys.Parse(vals[i].text())
		command := vals[i+1].text()
		i += 2
		items = addMenuItem(items, &width, &name, key, command, ctx, ci.Width-4)
	}
	if len(items) == 0 {
		return nil
	}
	w0 := st.W
	if w0 == nil {
		return nil
	}
	px, py := it.menuPosition(it.formatContext(ci, st.S, st.WL, st.P), it.Host.ClientGeometry(tc.Name), st, w0, width+4, len(items)+2)

	lines := -1
	if v, ok := a.Get('b'); ok {
		if lines, err = menuLines(v); err != nil {
			return err
		}
	}
	flags := 0
	if a.Has('O') {
		flags |= MenuStayOpen
	}
	if !a.Has('M') {
		// The mouse event that opened the menu comes with the mouse (task C5.1).
		flags |= MenuNoMouse
	}
	var sty, sel, border *string
	if v, ok := a.Get('s'); ok {
		sty = &v
	}
	if v, ok := a.Get('H'); ok {
		sel = &v
	}
	if v, ok := a.Get('S'); ok {
		border = &v
	}
	// The key event that opened the menu (for the chosen command's mouse target) comes with
	// the mouse, task C5.1.
	key := keys.None
	it.Host.DisplayMenu(MenuSpec{
		Client: tc.Name, Window: w0, State: st, Title: title, Items: items, Width: width,
		X: px, Y: py, Flags: flags, Lines: lines, StartingChoice: starting,
		Style: sty, Selected: sel, Border: border, Key: key,
	})
	return nil
}

// ParseCommandString is cmd_parse_from_string for a command a menu or prompt runs: nil for an
// empty one; an error's message is the parser's.
func ParseCommandString(s string) (*List, error) {
	cmds, err := cmdlang.Parse(s, cmdlang.Options{})
	if err != nil {
		var pe *cmdlang.Error
		if errors.As(err, &pe) {
			return nil, errors.New(pe.Msg)
		}
		return nil, err
	}
	if len(cmds) == 0 {
		return nil, nil
	}
	return (&Builder{Aliases: DefaultAliases}).Build(cmds)
}
