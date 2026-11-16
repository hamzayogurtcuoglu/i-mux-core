// Ported in part from tmux 3.8's cmd-display-menu.c:
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
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/format"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/model"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/pty"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/status"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/style"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/target"
)

// Popup flags (tmux's POPUP_*).
const (
	PopupCloseExit     = 0x1 // -E: close when the program exits
	PopupCloseExitZero = 0x2 // -EE: close when it exits with status 0
	PopupCloseAnyKey   = 0x4 // -k: any key closes it once the program is gone
)

// PopupSpec is a popup to open (popup_display's arguments).
type PopupSpec struct {
	Client  string
	Session *model.Session
	Flags   int
	Lines   int // popup-border-lines' index, or -1 for the option
	X, Y    int
	W, H    int
	Title   string
	// Style and BorderStyle are -s and -S, nil when not given.
	Style, BorderStyle *string
	Proc               pty.Spec
}

// PopupChange is what display-popup changes in an open popup (popup_modify): nil and -1 for
// what it leaves.
type PopupChange struct {
	Title, Style, BorderStyle *string
	Lines                     int
	Flags                     int
}

// ClientGeometry is what places a popup or menu on a client: its size, its status lines and
// their ranges, and where the window is shown from (tty_window_offset).
type ClientGeometry struct {
	Sx, Sy int
	// StatusAt is status_at_line: the status's first line, or -1 with no status.
	StatusAt    int
	StatusLines int
	StatusTop   bool // status-position top
	// Ranges are each status line's ranges.
	Ranges [][]status.Range
	Ox, Oy int
}

// popupFormats adds cmd_display_menu_get_popup_pos's popup_* variables for a popup of w×h on a
// client of geometry g to values.
func popupFormats(values map[string]string, g ClientGeometry, st target.State, w, h int) {
	add := func(k string, n int) { values[k] = strconv.Itoa(n) }
	top := 0
	if g.StatusAt != -1 {
		if g.StatusAt == 0 {
			top = g.StatusLines
		}
		found, line, start := false, 0, 0
		for l, rs := range g.Ranges {
			for _, r := range rs {
				if r.Type == style.RangeWindow && st.WL != nil && int(r.Arg) == st.WL.Index {
					found, line, start = true, l, r.Start
					break
				}
			}
			if found {
				break
			}
		}
		if found {
			add("popup_window_status_line_x", start)
			if g.StatusTop {
				add("popup_window_status_line_y", line+1+h)
			} else {
				add("popup_window_status_line_y", g.Sy-g.StatusLines+line)
			}
		}
		if g.StatusTop {
			add("popup_status_line_y", g.StatusLines+h)
		} else {
			add("popup_status_line_y", g.Sy-g.StatusLines)
		}
	}
	add("popup_width", w)
	add("popup_height", h)
	add("popup_centre_x", max((g.Sx-1)/2-w/2, 0))
	if n := (g.Sy-1)/2 + h/2; n >= g.Sy {
		add("popup_centre_y", g.Sy-h)
	} else {
		add("popup_centre_y", n)
	}
	if p := st.P; p != nil {
		if n := top + p.Yoff - g.Oy + h; n >= g.Sy {
			add("popup_pane_top", g.Sy-h)
		} else {
			add("popup_pane_top", n)
		}
		add("popup_pane_bottom", top+p.Yoff+p.Sy-g.Oy)
		add("popup_pane_left", p.Xoff-g.Ox)
		add("popup_pane_right", max(p.Xoff+p.Sx-g.Ox-w, 0))
	}
}

// strtol is strtol(p, NULL, 10): the leading number, 0 for none.
func strtol(s string) int {
	s = strings.TrimLeft(s, " \t\n\v\f\r")
	end := 0
	if end < len(s) && (s[end] == '-' || s[end] == '+') {
		end++
	}
	for end < len(s) && s[end] >= '0' && s[end] <= '9' {
		end++
	}
	n, _ := strconv.Atoi(s[:end])
	return n
}

// popupPosition is cmd_display_menu_get_popup_pos: where a popup of w×h goes on a client,
// from -x and -y (numbers, formats, or the letters C R P M L W S); ok is false when it does
// not fit.
func (it *Item) popupPosition(ctx *format.Context, g ClientGeometry, st target.State, w, h int) (px, py int, ok bool) {
	if w > g.Sx || h > g.Sy {
		return 0, 0, false
	}
	popupFormats(ctx.Values, g, st, w, h)
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
	n := strtol(ctx.Expand(xp))
	if n+w >= g.Sx {
		n = g.Sx - w
	} else if n < 0 {
		n = 0
	}
	px = n

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
	n = strtol(ctx.Expand(yp))
	if n < h {
		n = 0
	} else {
		n -= h
	}
	if n+h >= g.Sy {
		n = g.Sy - h
	} else if n < 0 {
		n = 0
	}
	return px, n, true
}

// popupLinesNone is popup-border-lines none (BOX_LINES_NONE).
const popupLinesNone = 6

// popupLines is -b: a popup-border-lines choice, by index.
func popupLines(v string) (int, error) {
	choices := []string{"single", "double", "heavy", "simple", "rounded", "padded", "none"}
	for i, c := range choices {
		if c == v {
			return i, nil
		}
	}
	// options_find_choice also takes a unique prefix.
	found := -1
	for i, c := range choices {
		if strings.HasPrefix(c, v) {
			if found != -1 {
				return 0, fmt.Errorf("popup-border-lines ambiguous: %s", v)
			}
			found = i
		}
	}
	if found == -1 {
		return 0, fmt.Errorf("popup-border-lines unknown: %s", v)
	}
	return found, nil
}

// displayPopup is display-popup: a popup running a program, over the client, until it closes;
// the command waits for it. With a popup already open, it changes that popup.
func displayPopup(it *Item) error {
	a := it.Cmd.Args
	st, err := it.findFlag('t', target.Pane, 0)
	if err != nil {
		return err
	}
	tc, err := it.findClient('c')
	if err != nil {
		return err
	}
	if a.Has('C') {
		it.Host.ClearOverlay(tc.Name)
		return nil
	}
	shown, modify := it.Host.Overlay(tc.Name)
	if !modify && shown {
		return nil
	}
	ci := it.clientInfo(tc.Name)
	if ci == nil {
		return nil
	}
	s := st.S

	var spec PopupSpec
	if !modify {
		h := ci.Height / 2
		if v, ok := a.Get('h'); ok {
			n, cause := percentage(v, 1, ci.Height, ci.Height)
			if cause != "" {
				return fmt.Errorf("height %s", cause)
			}
			h = n
		}
		w := ci.Width / 2
		if v, ok := a.Get('w'); ok {
			n, cause := percentage(v, 1, ci.Width, ci.Width)
			if cause != "" {
				return fmt.Errorf("width %s", cause)
			}
			w = n
		}
		w, h = min(w, ci.Width), min(h, ci.Height)
		ctx := it.formatContext(ci, st.S, st.WL, st.P)
		px, py, ok := it.popupPosition(ctx, it.Host.ClientGeometry(tc.Name), st, w, h)
		if !ok {
			return nil
		}
		spec = PopupSpec{Client: tc.Name, Session: s, X: px, Y: py, W: w, H: h}

		cwd := ""
		if v, ok := a.Get('d'); ok {
			cwd = it.formatSingle(st, v)
		} else {
			cwd = popupCwd(s, it.getenv("HOME"))
		}
		spec.Proc = it.popupProc(s, cwd)
	}

	lines := -1
	if a.Has('B') {
		lines = popupLinesNone
	} else if v, ok := a.Get('b'); ok {
		if lines, err = popupLines(v); err != nil {
			return err
		}
	}
	title := ""
	if v, ok := a.Get('T'); ok {
		title = it.formatSingle(st, v)
	}

	flags := -1
	if a.Has('N') || !modify {
		flags = 0
	}
	if a.Count('E') > 1 {
		flags = max(flags, 0) | PopupCloseExitZero
	} else if a.Has('E') {
		flags = max(flags, 0) | PopupCloseExit
	}
	if a.Has('k') {
		flags = max(flags, 0) | PopupCloseAnyKey
	}
	var sty, border *string
	if v, ok := a.Get('s'); ok {
		sty = &v
	}
	if v, ok := a.Get('S'); ok {
		border = &v
	}

	if modify {
		it.Host.ModifyPopup(tc.Name, PopupChange{Title: &title, Style: sty, BorderStyle: border, Lines: lines, Flags: flags})
		return nil
	}
	spec.Flags, spec.Lines, spec.Title, spec.Style, spec.BorderStyle = flags, lines, title, sty, border
	opened := it.Host.DisplayPopup(spec, func(status int) {
		// popup_free_cb: a command-line client exits with the popup's status.
		if it.Client == nil || it.Client.Session == nil {
			it.setCode(status)
		}
		it.resume(nil)
	})
	if !opened {
		return nil
	}
	return it.wait()
}

// popupCwd is server_client_get_cwd for the target client, which is attached: the session's
// directory, else home, else /.
func popupCwd(s *model.Session, home string) string {
	switch {
	case s != nil && s.Cwd != "":
		return s.Cwd
	case home != "":
		return home
	}
	return "/"
}

// popupProc is how job_run starts a popup's program (JOB_PTY|JOB_DEFAULTSHELL): one word or
// default-command with the shell's -c, several words with execvp, none as the shell itself; the
// session's environment with -e's variables, and PWD.
func (it *Item) popupProc(s *model.Session, cwd string) pty.Spec {
	a := it.Cmd.Args
	args := a.Strings()
	sh := it.shell(s)
	cmd, hasCmd := "", false
	switch len(args) {
	case 0:
		if s != nil {
			cmd = s.Options.GetString("default-command")
		}
		hasCmd = cmd != ""
	case 1:
		cmd, hasCmd = args[0], args[0] != ""
	}
	env := it.forSession(s, it.configLoading())
	for _, kv := range a.All('e') {
		env.put(kv)
	}
	dir := it.jobDir(cwd)
	env.set("PWD", dir)
	spec := pty.Spec{Dir: dir}
	switch {
	case hasCmd:
		env.set("SHELL", sh)
		spec.Path, spec.Argv = sh, []string{filepath.Base(sh), "-c", cmd}
	case len(args) <= 1:
		spec.Path, spec.Argv = sh, []string{sh}
	default:
		spec.Argv = append([]string(nil), args...)
	}
	spec.Env = env.list()
	return spec
}
