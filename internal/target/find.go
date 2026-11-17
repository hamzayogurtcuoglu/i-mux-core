// Ported from tmux 3.8's cmd-find.c and parts of window.c:
//
// Copyright (c) 2015 Nicholas Marriott <nicholas.marriott@gmail.com>
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

package target

import (
	"math"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/model"
)

// Targets (TASKS.md C3.3): what -t and -s find, as tmux's cmd_find_target does, from the
// current state (the command list's, else the command's client's, else the best session).

// Type is what a target is looked up as.
type Type int

// Target types (tmux's cmd_find_type).
const (
	Pane Type = iota
	Window
	Session
)

// Flags (tmux's CMD_FIND_*).
const (
	PreferUnattached = 0x1
	Quiet            = 0x2
	WindowIndex      = 0x4
	DefaultMarked    = 0x8
	ExactSession     = 0x10
	ExactWindow      = 0x20
	CanFail          = 0x40
)

// State is what a target found (tmux's cmd_find_state): a session, a window in it at an index,
// and a pane. Idx is -1 when no index was given; with WindowIndex, WL may be nil and Idx a
// free index.
type State struct {
	S   *model.Session
	WL  *model.Winlink
	Idx int
	W   *model.Window
	P   *model.Pane
}

// Empty reports whether nothing was found.
func (st *State) Empty() bool { return st.S == nil && st.WL == nil && st.W == nil && st.P == nil }

// Client is an attached client, as targets see it.
type Client struct {
	Name     string // its terminal's path
	Session  *model.Session
	Activity time.Time
}

// Context is what a target is looked up in.
type Context struct {
	Tree    *model.Tree
	Clients []Client // the attached clients, in the order they attached
	// Client is the command's client when it is attached, else nil.
	Client *Client
	// Inside is the pane the command's client runs in (from its TMUX_PANE), if any.
	Inside *model.Pane
	// Current is the command list's current state, if any.
	Current *State
	// Mouse is the mouse event of the key that ran the command, if any (= and {mouse}).
	Mouse *Mouse
}

// Error is a target that was not found; its message is empty when the lookup was quiet.
type Error struct{ Msg string }

func (e *Error) Error() string { return e.Msg }

type finder struct {
	ctx     *Context
	flags   int
	current *State
	st      State
}

// sessionValid is cmd_find_session_valid.
func sessionValid(s *model.Session) bool {
	return s != nil && s.Current != nil && s.Current.Window != nil && s.Current.Window.Active != nil
}

// bestSession is cmd_find_best_session over list, or every session when list is nil.
func (f *finder) bestSession(list []*model.Session) *model.Session {
	if list == nil {
		list = f.ctx.Tree.Sessions()
	}
	var best *model.Session
	for _, s := range list {
		if !sessionValid(s) {
			continue
		}
		if best == nil {
			best = s
			continue
		}
		if f.flags&PreferUnattached != 0 {
			if best.Attached != 0 && s.Attached == 0 {
				best = s
				continue
			}
			if best.Attached == 0 && s.Attached != 0 {
				continue
			}
		}
		if s.Activity.After(best.Activity) {
			best = s
		}
	}
	return best
}

// bestSessionWithWindow is cmd_find_best_session_with_window.
func (f *finder) bestSessionWithWindow() bool {
	var list []*model.Session
	for _, s := range f.ctx.Tree.Sessions() {
		if f.st.W.Session() == s {
			list = append(list, s)
		}
	}
	if len(list) == 0 {
		return false
	}
	if f.st.S = f.bestSession(list); f.st.S == nil {
		return false
	}
	return f.bestWinlinkWithWindow()
}

// bestWinlinkWithWindow is cmd_find_best_winlink_with_window.
func (f *finder) bestWinlinkWithWindow() bool {
	s, w := f.st.S, f.st.W
	var wl *model.Winlink
	if s.Current != nil && s.Current.Window == w {
		wl = s.Current
	} else {
		for _, l := range s.Links() {
			if l.Window == w {
				wl = l
				break
			}
		}
	}
	if wl == nil {
		return false
	}
	f.st.WL, f.st.Idx = wl, wl.Index
	return true
}

var windowTable = map[string]string{
	"{start}": "^", "{last}": "!", "{end}": "$", "{next}": "+", "{previous}": "-",
}

var paneTable = map[string]string{
	"{last}": "!", "{next}": "+", "{previous}": "-", "{top}": "top", "{bottom}": "bottom",
	"{left}": "left", "{right}": "right", "{top-left}": "top-left", "{top-right}": "top-right",
	"{bottom-left}": "bottom-left", "{bottom-right}": "bottom-right",
}

func mapTable(t map[string]string, s string) string {
	if v, ok := t[s]; ok {
		return v
	}
	return s
}

// strtonum parses a number in [lo, hi] as tmux's strtonum.
func strtonum(s string, lo, hi int) (int, bool) {
	if s == "" {
		return 0, false
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n < int64(lo) || n > int64(hi) {
		return 0, false
	}
	return int(n), true
}

// findByID parses "<prefix>N".
func findByID(s string) (int, bool) { return strtonum(s[1:], 0, math.MaxInt32) }

// getSession is cmd_find_get_session.
func (f *finder) getSession(name string) bool {
	t := f.ctx.Tree
	if strings.HasPrefix(name, "$") {
		id, ok := findByID(name)
		if !ok {
			return false
		}
		f.st.S = t.Session(id)
		return f.st.S != nil
	}
	if s := t.SessionByName(name); s != nil {
		f.st.S = s
		return true
	}
	if c := FindClient(f.ctx, name); c != nil && c.Session != nil {
		f.st.S = c.Session
		return true
	}
	if f.flags&ExactSession != 0 {
		return false
	}
	var found *model.Session
	for _, s := range t.Sessions() {
		if strings.HasPrefix(s.Name, name) {
			if found != nil {
				return false
			}
			found = s
		}
	}
	if found == nil {
		for _, s := range t.Sessions() {
			if ok, _ := path.Match(name, s.Name); ok {
				if found != nil {
					return false
				}
				found = s
			}
		}
	}
	f.st.S = found
	return found != nil
}

// getWindow is cmd_find_get_window.
func (f *finder) getWindow(window string, only bool) bool {
	if strings.HasPrefix(window, "@") {
		id, ok := findByID(window)
		if !ok {
			return false
		}
		if f.st.W = f.ctx.Tree.Window(id); f.st.W == nil {
			return false
		}
		return f.bestSessionWithWindow()
	}
	f.st.S = f.current.S
	if f.getWindowWithSession(window) {
		return true
	}
	if !only && f.getSession(window) {
		f.st.WL = f.st.S.Current
		f.st.W = f.st.WL.Window
		if f.flags&WindowIndex == 0 {
			f.st.Idx = f.st.WL.Index
		}
		return true
	}
	return false
}

// nextLink and previousLink are winlink_next_by_number and winlink_previous_by_number.
func nextLink(s *model.Session, wl *model.Winlink, n int) *model.Winlink {
	links := s.Links()
	i := linkIndex(links, wl)
	return links[(i+n)%len(links)]
}

func previousLink(s *model.Session, wl *model.Winlink, n int) *model.Winlink {
	links := s.Links()
	i := linkIndex(links, wl)
	return links[((i-n)%len(links)+len(links))%len(links)]
}

func linkIndex(links []*model.Winlink, wl *model.Winlink) int {
	for i, l := range links {
		if l == wl {
			return i
		}
	}
	return 0
}

// getWindowWithSession is cmd_find_get_window_with_session.
func (f *finder) getWindowWithSession(window string) bool {
	exact := f.flags&ExactWindow != 0
	s := f.st.S
	f.st.WL = s.Current
	f.st.W = f.st.WL.Window
	set := func(wl *model.Winlink) bool {
		if wl == nil {
			return false
		}
		f.st.WL, f.st.Idx, f.st.W = wl, wl.Index, wl.Window
		return true
	}
	if strings.HasPrefix(window, "@") {
		id, ok := findByID(window)
		if !ok {
			return false
		}
		f.st.W = f.ctx.Tree.Window(id)
		if f.st.W == nil || f.st.W.Session() != s {
			return false
		}
		return f.bestWinlinkWithWindow()
	}
	if !exact && (window[0] == '+' || window[0] == '-') {
		n := 1
		if len(window) > 1 {
			var ok bool
			if n, ok = strtonum(window[1:], 1, math.MaxInt32); !ok {
				return false
			}
		}
		if f.flags&WindowIndex != 0 {
			if window[0] == '+' {
				if math.MaxInt32-s.Current.Index < n {
					return false
				}
				f.st.Idx = s.Current.Index + n
			} else {
				if n > s.Current.Index {
					return false
				}
				f.st.Idx = s.Current.Index - n
			}
			return true
		}
		if window[0] == '+' {
			return set(nextLink(s, s.Current, n))
		}
		return set(previousLink(s, s.Current, n))
	}
	if !exact {
		links := s.Links()
		switch window {
		case "!":
			return set(s.LastWindow())
		case "^":
			if len(links) == 0 {
				return false
			}
			return set(links[0])
		case "$":
			if len(links) == 0 {
				return false
			}
			return set(links[len(links)-1])
		}
	}
	if window[0] != '+' && window[0] != '-' {
		if idx, ok := strtonum(window, 0, math.MaxInt32); ok {
			if wl := s.Link(idx); wl != nil {
				return set(wl)
			}
			if f.flags&WindowIndex != 0 {
				f.st.Idx = idx
				return true
			}
		}
	}
	match := func(m func(name string) bool) (*model.Winlink, bool) {
		var found *model.Winlink
		for _, wl := range s.Links() {
			if m(wl.Window.Name) {
				if found != nil {
					return nil, false
				}
				found = wl
			}
		}
		return found, true
	}
	f.st.WL = nil
	wl, ok := match(func(n string) bool { return n == window })
	if !ok {
		return false
	}
	if wl != nil {
		return set(wl)
	}
	if exact {
		return false
	}
	if wl, ok = match(func(n string) bool { return strings.HasPrefix(n, window) }); !ok {
		return false
	}
	if wl != nil {
		return set(wl)
	}
	wl, ok = match(func(n string) bool { m, _ := path.Match(window, n); return m })
	if !ok || wl == nil {
		return false
	}
	return set(wl)
}

// getPane is cmd_find_get_pane.
func (f *finder) getPane(pane string, only bool) bool {
	if strings.HasPrefix(pane, "%") {
		id, ok := findByID(pane)
		if !ok {
			return false
		}
		if f.st.P = f.ctx.Tree.Pane(id); f.st.P == nil {
			return false
		}
		f.st.W = f.st.P.Window
		return f.bestSessionWithWindow()
	}
	f.st.S, f.st.WL, f.st.Idx, f.st.W = f.current.S, f.current.WL, f.current.Idx, f.current.W
	if f.getPaneWithWindow(pane) {
		return true
	}
	if !only && f.getWindow(pane, false) {
		f.st.P = f.st.W.Active
		return true
	}
	return false
}

// getPaneWithSession is cmd_find_get_pane_with_session.
func (f *finder) getPaneWithSession(pane string) bool {
	if strings.HasPrefix(pane, "%") {
		id, ok := findByID(pane)
		if !ok {
			return false
		}
		if f.st.P = f.ctx.Tree.Pane(id); f.st.P == nil {
			return false
		}
		f.st.W = f.st.P.Window
		return f.bestWinlinkWithWindow()
	}
	f.st.WL = f.st.S.Current
	f.st.Idx, f.st.W = f.st.WL.Index, f.st.WL.Window
	return f.getPaneWithWindow(pane)
}

// getPaneWithWindow is cmd_find_get_pane_with_window.
func (f *finder) getPaneWithWindow(pane string) bool {
	w := f.st.W
	if strings.HasPrefix(pane, "%") {
		id, ok := findByID(pane)
		if !ok {
			return false
		}
		f.st.P = f.ctx.Tree.Pane(id)
		return f.st.P != nil && f.st.P.Window == w
	}
	set := func(p *model.Pane) bool {
		f.st.P = p
		return p != nil
	}
	switch pane {
	case "!":
		return set(w.LastPane())
	case "{up-of}":
		return set(findUp(w.Active))
	case "{down-of}":
		return set(findDown(w.Active))
	case "{left-of}":
		return set(findLeft(w.Active))
	case "{right-of}":
		return set(findRight(w.Active))
	}
	if pane[0] == '+' || pane[0] == '-' {
		n := 1
		ok := true
		if len(pane) > 1 {
			n, ok = strtonum(pane[1:], 1, math.MaxInt32)
		}
		if !ok {
			return false
		}
		i := w.PaneIndex(w.Active)
		if pane[0] == '+' {
			return set(w.Panes[(i+n)%len(w.Panes)])
		}
		return set(w.Panes[((i-n)%len(w.Panes)+len(w.Panes))%len(w.Panes)])
	}
	if idx, ok := strtonum(pane, 0, math.MaxInt32); ok {
		// window_pane_at_index: indexes start at pane-base-index.
		if i := idx - int(w.Options.GetNumber("pane-base-index")); i >= 0 && i < len(w.Panes) {
			return set(w.Panes[i])
		}
	}
	return set(findString(w, pane))
}

// Find is cmd_find_target: target looked up as typ.
func Find(ctx *Context, target string, typ Type, flags int) (State, error) {
	if flags&CanFail != 0 {
		flags |= Quiet
	}
	f := &finder{ctx: ctx, flags: flags, st: State{Idx: -1}}
	fail := func(msg string) (State, error) {
		if flags&CanFail != 0 {
			return State{Idx: -1}, nil
		}
		if flags&Quiet != 0 {
			msg = ""
		}
		return State{Idx: -1}, &Error{Msg: msg}
	}
	switch {
	case ctx.Current != nil && valid(ctx.Current):
		f.current = ctx.Current
	default:
		cur, ok := fromClient(ctx, flags)
		if !ok {
			return fail("no current target")
		}
		f.current = &cur
	}
	found := func() (State, error) { return f.st, nil }

	if target == "" {
		f.st = *f.current
		if flags&WindowIndex != 0 {
			f.st.Idx = -1
		}
		return found()
	}
	switch target {
	case "@", "{active}", "{current}":
		c := ctx.Client
		if c == nil || c.Session == nil {
			// Reported even when quiet, as tmux does.
			if flags&CanFail != 0 {
				return State{Idx: -1}, nil
			}
			return State{Idx: -1}, &Error{Msg: "no current client"}
		}
		f.st.S = c.Session
		f.st.WL = c.Session.Current
		f.st.W = f.st.WL.Window
		f.st.P = f.st.W.Active
		return found()
	case "=", "{mouse}":
		if !f.fromMouse(typ) {
			return fail("no mouse target")
		}
		return found()
	case "~", "{marked}":
		return fail("no marked target")
	}

	var session, window, pane string
	hasSession, hasWindow, hasPane := false, false, false
	windowOnly, paneOnly := false, false
	copyT := target
	before, afterColon, colon := strings.Cut(copyT, ":")
	var periodPart string
	period := false
	if colon {
		afterColon, periodPart, period = strings.Cut(afterColon, ".")
	} else {
		before, periodPart, period = strings.Cut(before, ".")
	}
	switch {
	case colon && period:
		session, window, pane = before, afterColon, periodPart
		hasSession, hasWindow, hasPane = true, true, true
		windowOnly, paneOnly = true, true
	case colon:
		session, window = before, afterColon
		hasSession, hasWindow = true, true
		windowOnly = true
	case period:
		window, pane = before, periodPart
		hasWindow, hasPane = true, true
		paneOnly = true
	default:
		switch {
		case strings.HasPrefix(before, "$"):
			session, hasSession = before, true
		case strings.HasPrefix(before, "@"):
			window, hasWindow = before, true
		case strings.HasPrefix(before, "%"):
			pane, hasPane = before, true
		default:
			switch typ {
			case Session:
				session, hasSession = before, true
			case Window:
				window, hasWindow = before, true
			case Pane:
				pane, hasPane = before, true
			}
		}
	}
	if hasSession && strings.HasPrefix(session, "=") {
		session = session[1:]
		f.flags |= ExactSession
	}
	if hasWindow && strings.HasPrefix(window, "=") {
		window = window[1:]
		f.flags |= ExactWindow
	}
	hasSession = hasSession && session != ""
	hasWindow = hasWindow && window != ""
	hasPane = hasPane && pane != ""
	if hasWindow {
		window = mapTable(windowTable, window)
	}
	if hasPane {
		pane = mapTable(paneTable, pane)
	}

	if hasPane && flags&WindowIndex != 0 {
		return fail("can't specify pane here")
	}
	noSession := func() (State, error) { return fail("can't find session: " + session) }
	noWindow := func() (State, error) { return fail("can't find window: " + window) }
	noPane := func() (State, error) { return fail("can't find pane: " + pane) }

	if hasSession {
		if !f.getSession(session) {
			return noSession()
		}
		switch {
		case !hasWindow && !hasPane:
			f.st.WL = f.st.S.Current
			f.st.Idx = -1
			f.st.W = f.st.WL.Window
			f.st.P = f.st.W.Active
		case hasWindow && !hasPane:
			if !f.getWindowWithSession(window) {
				return noWindow()
			}
			if f.st.WL != nil {
				f.st.P = f.st.WL.Window.Active
			}
		case !hasWindow && hasPane:
			if !f.getPaneWithSession(pane) {
				return noPane()
			}
		default:
			if !f.getWindowWithSession(window) {
				return noWindow()
			}
			if !f.getPaneWithWindow(pane) {
				return noPane()
			}
		}
		return found()
	}
	switch {
	case hasWindow && hasPane:
		if !f.getWindow(window, windowOnly) {
			return noWindow()
		}
		if !f.getPaneWithWindow(pane) {
			return noPane()
		}
	case hasWindow:
		if !f.getWindow(window, windowOnly) {
			return noWindow()
		}
		if f.st.WL != nil {
			f.st.P = f.st.WL.Window.Active
		}
	case hasPane:
		if !f.getPane(pane, paneOnly) {
			return noPane()
		}
	default:
		f.st = *f.current
		if flags&WindowIndex != 0 {
			f.st.Idx = -1
		}
	}
	return found()
}

// valid is cmd_find_valid_state.
func valid(st *State) bool {
	if st.S == nil || st.WL == nil || st.W == nil || st.P == nil {
		return false
	}
	if st.WL.Session != st.S || st.WL.Window != st.W || st.S.Link(st.WL.Index) != st.WL {
		return false
	}
	return st.W.PaneIndex(st.P) >= 0
}

// fromClient is cmd_find_from_client for the command's client: an attached client's session,
// else the session showing the pane it runs in, else the best session.
func fromClient(ctx *Context, flags int) (State, bool) {
	f := &finder{ctx: ctx, flags: flags, st: State{Idx: -1}}
	if c := ctx.Client; c != nil && c.Session != nil && sessionValid(c.Session) {
		s := c.Session
		return State{S: s, WL: s.Current, Idx: -1, W: s.Current.Window, P: s.Current.Window.Active}, true
	}
	if p := ctx.Inside; p != nil && p.Window != nil && ctx.Tree.Pane(p.ID) == p {
		f.st.W = p.Window
		if f.bestSessionWithWindow() {
			wl := f.st.S.Current
			return State{S: f.st.S, WL: wl, Idx: f.st.Idx, W: wl.Window, P: wl.Window.Active}, true
		}
	}
	s := f.bestSession(nil)
	if s == nil {
		return State{Idx: -1}, false
	}
	return State{S: s, WL: s.Current, Idx: s.Current.Index, W: s.Current.Window, P: s.Current.Window.Active}, true
}

// FindClient is cmd_find_client with a name: a client by name or terminal path, with or
// without /dev/ and a trailing colon; nil if there is none.
func FindClient(ctx *Context, name string) *Client {
	name = strings.TrimSuffix(name, ":")
	for i := range ctx.Clients {
		c := &ctx.Clients[i]
		if c.Session == nil {
			continue
		}
		if name == c.Name || (strings.HasPrefix(c.Name, "/dev/") && name == c.Name[len("/dev/"):]) {
			return c
		}
	}
	return nil
}

// CurrentClient is cmd_find_current_client: the command's client when attached, else the most
// recently active client of the best session (or of the session showing the pane the command
// runs in).
func CurrentClient(ctx *Context) *Client {
	if ctx.Client != nil && ctx.Client.Session != nil {
		return ctx.Client
	}
	f := &finder{ctx: ctx, flags: Quiet, st: State{Idx: -1}}
	var s *model.Session
	if p := ctx.Inside; p != nil && ctx.Tree.Pane(p.ID) == p {
		f.st.W = p.Window
		if f.bestSessionWithWindow() {
			s = f.st.S
		}
	} else {
		s = f.bestSession(nil)
	}
	if s == nil {
		return nil
	}
	return BestClient(ctx, s)
}

// BestClient is cmd_find_best_client: the most recently active client of s, or of any
// session when no client shows s.
func BestClient(ctx *Context, s *model.Session) *Client {
	if s.Attached == 0 {
		s = nil
	}
	var best *Client
	for i := range ctx.Clients {
		c := &ctx.Clients[i]
		if c.Session == nil || (s != nil && c.Session != s) {
			continue
		}
		if best == nil || c.Activity.After(best.Activity) {
			best = c
		}
	}
	return best
}
