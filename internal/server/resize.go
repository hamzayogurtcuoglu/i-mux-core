// Ported in part from tmux 3.8's resize.c and server-client.c:
//
// Copyright (c) 2007 Nicholas Marriott <nicholas.marriott@gmail.com>
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
	"fmt"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/model"
)

// Window sizes from the clients that show them (TASKS.md C2.12), as tmux's resize.c: with
// window-size latest (the default), a window shown by one client takes that client's size, and
// shown by several, the size of the one that used it last; largest and smallest take the
// largest or smallest of them; manual keeps the window's size. A client's size leaves out its
// status lines.

// window-size values (tmux's WINDOW_SIZE_*).
const (
	windowSizeLargest = iota
	windowSizeSmallest
	windowSizeManual
	windowSizeLatest
)

// Window size limits (tmux's WINDOW_MINIMUM and WINDOW_MAXIMUM).
const (
	windowMinimum = 1
	windowMaximum = 10000
)

// statusLineSize is status_line_size: the lines the client's status takes, none when the
// client is too small for them.
func (s *Server) statusLineSize(c *attachedClient) int {
	lines := int(c.session.Options.GetNumber("status"))
	if c.tty.Sy <= lines {
		return 0
	}
	return lines
}

// shows reports whether a client shows a window's session (session_has).
func (c *attachedClient) shows(w *model.Window) bool {
	return !c.gone && w.Session() == c.session
}

// recalculateSizes is recalculate_sizes: every window gets the size its clients give it.
func (s *Server) recalculateSizes() {
	for _, sess := range s.tree.Sessions() {
		for _, wl := range sess.Links() {
			s.recalculateSize(wl.Window)
		}
	}
}

// recalculateSize is recalculate_size and clients_calculate_size for one window.
func (s *Server) recalculateSize(w *model.Window) {
	windowSize := w.Options.GetNumber("window-size")
	if w.Active == nil || windowSize == windowSizeManual {
		return
	}
	n := 0
	for _, c := range s.attached {
		if c.shows(w) {
			n++
		}
	}
	sx, sy, found := 0, 0, false
	for _, c := range s.attached {
		if !c.shows(w) {
			continue
		}
		if windowSize == windowSizeLatest && n > 1 && c != s.latest[w] {
			continue
		}
		cx, cy := c.tty.Sx, c.tty.Sy-s.statusLineSize(c)
		switch {
		case !found:
			sx, sy = cx, cy
		case windowSize == windowSizeLargest:
			sx, sy = max(sx, cx), max(sy, cy)
		default:
			sx, sy = min(sx, cx), min(sy, cy)
		}
		found = true
	}
	if !found {
		return
	}
	sx = min(max(sx, windowMinimum), windowMaximum)
	sy = min(max(sy, windowMinimum), windowMaximum)
	if sx == w.Sx && sy == w.Sy {
		return
	}
	s.tree.ResizeWindow(w, sx, sy)
	// server_redraw_window: the window's clients are drawn whole.
	for _, c := range s.attached {
		if c.shows(w) {
			s.invalidate(c)
		}
	}
}

// updateLatest is server_client_update_latest: the client is the one that used its window
// last, which latest window sizes follow.
func (s *Server) updateLatest(c *attachedClient) {
	w := c.window()
	if w == nil || s.latest[w] == c {
		return
	}
	s.latest[w] = c
	if w.Options.GetNumber("window-size") == windowSizeLatest {
		s.recalculateSize(w)
	}
	s.fire("client-active", c.name)
}

// lostClient is the part of server_client_lost that finds windows a new latest client: the
// most recently active of those still showing them.
func (s *Server) lostClient(c *attachedClient) {
	for w, l := range s.latest {
		if l != c {
			continue
		}
		delete(s.latest, w)
		var found *attachedClient
		for _, o := range s.attached {
			if o != c && o.window() == w && (found == nil || o.activity.After(found.activity)) {
				found = o
			}
		}
		if found != nil {
			s.updateLatest(found)
		}
	}
	s.recalculateSizes()
}

// setSession is server_client_set_session for an attached client: it shows sess now.
func (s *Server) setSession(c *attachedClient, sess *model.Session) {
	old := c.window()
	if c.session != sess {
		c.session.Attached--
		sess.Attached++
		c.session = sess
	}
	c.in.focused = true
	sess.Activity = s.opts.Now()
	s.updateFocus(old)
	if w := c.window(); w != nil {
		s.latest[w] = c
	}
	s.recalculateSizes()
	s.updateFocus(c.window())
	s.updatePaneColours()
	s.themeChanged(c)
	s.invalidate(c)
}

// SwitchClient implements commands.Host: switch-client moves an attached client to a session.
func (s *Server) SwitchClient(name string, sess *model.Session) error {
	for _, c := range s.attached {
		if c.name == name && !c.gone {
			s.setSession(c, sess)
			return nil
		}
	}
	return errors.New("can't find client: " + name)
}

// DefaultSize implements commands.Host: a new window of s takes the size the clients showing s
// give (the smallest, less their status lines), else the session's default-size (tmux's
// default_window_size with no target client).
func (s *Server) DefaultSize(sess *model.Session) (sx, sy int) {
	found := false
	for _, c := range s.attached {
		if c.gone || c.session != sess {
			continue
		}
		cx, cy := c.tty.Sx, c.tty.Sy-s.statusLineSize(c)
		if !found || cx < sx {
			sx = cx
		}
		if !found || cy < sy {
			sy = cy
		}
		found = true
	}
	if !found {
		// default-size, else 80x24.
		var x, y int
		if n, err := fmt.Sscanf(sess.Options.GetString("default-size"), "%dx%d", &x, &y); n == 2 && err == nil {
			return min(max(x, windowMinimum), windowMaximum), min(max(y, windowMinimum), windowMaximum)
		}
		return 80, 24
	}
	return min(max(sx, windowMinimum), windowMaximum), min(max(sy, windowMinimum), windowMaximum)
}
