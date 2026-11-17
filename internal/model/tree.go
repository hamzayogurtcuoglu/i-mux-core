package model

import (
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/keys"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/layout"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/options"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/pty"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/vt"
)

// The tree of sessions, windows and panes (ARCHITECTURE.md §4). Only the server loop touches
// it (E2); a pane's screen alone is shared with the pane's reader, under the pane's mutex.
// The rules follow tmux 3.8's session.c, window.c and cmd-find.c.

// Tree is the server's sessions, windows and panes.
type Tree struct {
	// Resized is called when a pane's size changes (its program and screen are resized).
	Resized func(p *Pane)
	// ActiveChanged is called when a window's active pane changes to p from old (which may
	// be gone): tmux's window_set_active_pane and window_lost_pane fire their events there.
	ActiveChanged func(w *Window, p, old *Pane)
	// Options are the global options, the parents of every session's, window's and pane's.
	Options *options.Globals
	// Keys are the key tables (tmux's key_tables), loaded with tmux's defaults when the
	// server starts.
	Keys *keys.Tables

	sessions map[int]*Session
	windows  map[int]*Window
	panes    map[int]*Pane

	nextSession, nextWindow, nextPane int
}

// Session is a session: windows at indexes, the current and last ones.
type Session struct {
	ID       int
	Name     string
	Cwd      string
	Created  time.Time
	Activity time.Time
	Attached int
	Options  *options.Options
	// Env is the session's environment (tmux's s->environ): a nil value removes the variable
	// from the global environment for the session's panes and jobs.
	Env map[string]*string

	links   map[int]*Winlink // by index
	Current *Winlink
	last    []*Winlink // most recent first
}

// Winlink is a window at an index in a session.
type Winlink struct {
	Index   int
	Window  *Window
	Session *Session
}

// Window is a window: its panes in order, the active and last ones, its size.
type Window struct {
	ID      int
	Name    string
	Sx, Sy  int
	Options *options.Options

	Panes  []*Pane
	Active *Pane
	last   []*Pane
	// ZOrder is the panes in tmux's z_index order: tiled panes are added at the end and keep
	// their place, swap-pane exchanges two places. Drawing goes from the end to the start, so
	// a border between panes belongs to the one nearest the start.
	ZOrder []*Pane

	Layout *layout.Layout // made with the first pane

	// NameTime is when automatic-rename last named the window (tmux's name_time).
	NameTime time.Time

	// ManualSx and ManualSy are the size resize-window set (window-size manual), or 0.
	ManualSx, ManualSy int

	// MenuLastX and MenuLastY are where the window's last menu went (menu_last_px and
	// menu_last_py, for display-menu -x L and -y L).
	MenuLastX, MenuLastY int

	link *Winlink // windows are in one session: link-window is a non-goal
	tree *Tree
}

// Pane is a pane: its program and screen. Mu guards Term, which the pane's reader writes.
type Pane struct {
	ID     int
	Window *Window
	Sx, Sy int
	Xoff   int
	Yoff   int

	// Mode is the mode the pane is in (tmux's first wp->modes entry), or nil; the loop alone
	// touches it (ARCHITECTURE.md §4.3).
	Mode PaneMode

	Options *options.Options

	Mu   sync.Mutex
	Term *vt.Terminal
	Proc pty.Proc

	Dead   bool
	Status pty.ExitStatus
	// DeadTime is when a pane that remain-on-exit kept died (#{pane_dead_time}).
	DeadTime time.Time

	// Argv, Cwd and Shell are what the pane was started with (#{pane_start_command},
	// #{pane_start_path}); Shell is the default-shell then.
	Argv  []string
	Cwd   string
	Shell string

	// Changed is tmux's PANE_CHANGED: the pane wrote, or became active, since its window's
	// name was last worked out (automatic-rename).
	Changed bool

	// ActivePoint orders when panes were last made active (tmux's active_point): the pane in
	// a direction is the most recently active of those there.
	ActivePoint uint64
}

// nextActivePoint is tmux's next_active_point, across the server.
var nextActivePoint atomic.Uint64

// Errors from tree operations, with tmux's messages.
var (
	ErrDuplicateSession = errors.New("duplicate session")
	ErrIndexInUse       = errors.New("index in use")
	ErrNoFreeIndex      = errors.New("no free window index")
)

// NewTree returns an empty tree.
func NewTree() *Tree {
	return &Tree{
		sessions: map[int]*Session{}, windows: map[int]*Window{}, panes: map[int]*Pane{},
		Options: options.NewGlobals(),
		Keys:    keys.NewTables(),
	}
}

// Sessions returns the sessions in tmux's order: by name.
func (t *Tree) Sessions() []*Session {
	out := make([]*Session, 0, len(t.sessions))
	for _, s := range t.sessions {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Session returns the session with id $id, or nil.
func (t *Tree) Session(id int) *Session { return t.sessions[id] }

// Window returns the window with id @id, or nil.
func (t *Tree) Window(id int) *Window { return t.windows[id] }

// Pane returns the pane with id %id, or nil.
func (t *Tree) Pane(id int) *Pane { return t.panes[id] }

// Panes returns every pane, by id.
func (t *Tree) Panes() []*Pane {
	out := make([]*Pane, 0, len(t.panes))
	for _, p := range t.panes {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// SessionByName returns the session named exactly name, or nil.
func (t *Tree) SessionByName(name string) *Session {
	for _, s := range t.sessions {
		if s.Name == name {
			return s
		}
	}
	return nil
}

// NewSession creates a session. An empty name gives it its id as name, as tmux does. The name
// is used as given: the commands check and clean it (tmux's check_name and clean_name).
func (t *Tree) NewSession(name, cwd string, now time.Time) (*Session, error) {
	s := &Session{
		Cwd: cwd, Created: now, Activity: now, links: map[int]*Winlink{}, Options: options.New(t.Options.Session),
		Env: map[string]*string{},
	}
	if name != "" {
		if t.SessionByName(name) != nil {
			return nil, fmt.Errorf("%w: %s", ErrDuplicateSession, name)
		}
		s.Name = name
		s.ID = t.nextSession
		t.nextSession++
	} else {
		for {
			s.ID = t.nextSession
			t.nextSession++
			s.Name = strconv.Itoa(s.ID)
			if t.SessionByName(s.Name) == nil {
				break
			}
		}
	}
	t.sessions[s.ID] = s
	return s, nil
}

// RenameSession renames s; the name is checked and cleaned by the command.
func (t *Tree) RenameSession(s *Session, name string) error {
	if o := t.SessionByName(name); o != nil && o != s {
		return fmt.Errorf("%w: %s", ErrDuplicateSession, name)
	}
	s.Name = name
	return nil
}

// BestSession returns the session tmux targets when nothing says which: the one with the
// newest activity among those with a current window and active pane; on a tie, the first by
// name.
func (t *Tree) BestSession() *Session { return t.BestSessionFor(false) }

// BestSessionFor is cmd_find_best_session: with preferUnattached (attach-session and
// switch-client), a session no client shows comes before one that is shown.
func (t *Tree) BestSessionFor(preferUnattached bool) *Session {
	var best *Session
	for _, s := range t.Sessions() {
		if s.Current == nil || s.Current.Window.Active == nil {
			continue
		}
		if best == nil {
			best = s
			continue
		}
		if preferUnattached {
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

// Links returns the session's windows in index order.
func (s *Session) Links() []*Winlink {
	out := make([]*Winlink, 0, len(s.links))
	for _, wl := range s.links {
		out = append(out, wl)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Index < out[j].Index })
	return out
}

// Link returns the window at index idx, or nil.
func (s *Session) Link(idx int) *Winlink { return s.links[idx] }

// NextIndex is tmux's winlink_next_index: the first free index from idx up, wrapping.
func (s *Session) NextIndex(idx int) int {
	i := idx
	for {
		if s.links[i] == nil {
			return i
		}
		if i == math.MaxInt32 {
			i = 0
		} else {
			i++
		}
		if i == idx {
			return -1
		}
	}
}

// NewWindow creates a window in s at index idx; idx < 0 takes the first free index from base.
func (t *Tree) NewWindow(s *Session, idx, base, sx, sy int) (*Winlink, error) {
	if idx < 0 {
		idx = s.NextIndex(base)
		if idx < 0 {
			return nil, ErrNoFreeIndex
		}
	} else if s.links[idx] != nil {
		return nil, fmt.Errorf("%w: %d", ErrIndexInUse, idx)
	}
	w := &Window{ID: t.nextWindow, Sx: sx, Sy: sy, Options: options.New(t.Options.Window), tree: t}
	t.nextWindow++
	wl := &Winlink{Index: idx, Window: w, Session: s}
	w.link = wl
	s.links[idx] = wl
	t.windows[w.ID] = w
	return wl, nil
}

// Session returns the session a window is in.
func (w *Window) Session() *Session { return w.link.Session }

// Index returns the window's index in its session.
func (w *Window) Index() int { return w.link.Index }

// Link returns the window's winlink.
func (w *Window) Link() *Winlink { return w.link }

// SwapWindows exchanges the windows of two winlinks (swap-window): each window moves to the
// other's index and session.
func (t *Tree) SwapWindows(a, b *Winlink) {
	if a.Window == b.Window {
		return
	}
	a.Window, b.Window = b.Window, a.Window
	a.Window.link, b.Window.link = a, b
}

// NewPane adds a pane to w, after the pane at, or before it when before is set, or last
// when at is nil. The first pane of a window becomes its active pane.
func (t *Tree) NewPane(w *Window, at *Pane, before bool, sx, sy int) *Pane {
	p := &Pane{ID: t.nextPane, Window: w, Sx: sx, Sy: sy, Options: options.New(w.Options), Changed: true}
	t.nextPane++
	i := len(w.Panes)
	if at != nil {
		i = w.PaneIndex(at)
		if !before {
			i++
		}
	}
	w.Panes = append(w.Panes, nil)
	copy(w.Panes[i+1:], w.Panes[i:])
	w.Panes[i] = p
	w.ZOrder = append(w.ZOrder, p)
	if w.Active == nil {
		w.Active = p
	}
	t.panes[p.ID] = p
	if w.Layout == nil {
		w.Layout = layout.New(p.ID, w.Sx, w.Sy)
		p.Xoff, p.Yoff = 0, 0
		// The window's pane-border-status takes its line from the start.
		if st := w.BorderStatus(); st != layout.StatusOff {
			w.Layout.Status = st
			t.fixPanes(w)
		}
	}
	return p
}

// PaneIndex returns p's position in its window (tmux adds pane-base-index for #{pane_index}).
func (w *Window) PaneIndex(p *Pane) int {
	for i, q := range w.Panes {
		if q == p {
			return i
		}
	}
	return -1
}

// SetActivePane makes p the active pane; the previous one becomes the last.
func (w *Window) SetActivePane(p *Pane) {
	if w.Active == p {
		return
	}
	if w.Active != nil {
		w.last = append([]*Pane{w.Active}, removePane(w.last, w.Active)...)
	}
	w.last = removePane(w.last, p)
	old := w.Active
	w.Active = p
	p.ActivePoint = nextActivePoint.Add(1)
	p.Changed = true
	if w.tree != nil && w.tree.ActiveChanged != nil {
		w.tree.ActiveChanged(w, p, old)
	}
}

// LastPane returns the previously active pane, or nil.
func (w *Window) LastPane() *Pane {
	if len(w.last) == 0 {
		return nil
	}
	return w.last[0]
}

func removePane(l []*Pane, p *Pane) []*Pane {
	out := l[:0:0]
	for _, q := range l {
		if q != p {
			out = append(out, q)
		}
	}
	return out
}

// SelectWindow makes wl the current window; the previous one goes on the last-window stack.
func (s *Session) SelectWindow(wl *Winlink) {
	if s.Current == wl {
		return
	}
	if s.Current != nil {
		s.last = append([]*Winlink{s.Current}, removeLink(s.last, s.Current)...)
	}
	s.last = removeLink(s.last, wl)
	s.Current = wl
}

// ShuffleUp is tmux's winlink_shuffle_up: room for a window after wl (before it with before),
// moving the windows in the way up by one. It returns the index made free, or -1.
func (s *Session) ShuffleUp(wl *Winlink, before bool) int {
	if wl == nil || wl.Index == math.MaxInt32 {
		return -1
	}
	idx := wl.Index + 1
	if before {
		idx = wl.Index
	}
	last := idx
	for ; last < math.MaxInt32; last++ {
		if s.links[last] == nil {
			break
		}
	}
	if last == math.MaxInt32 {
		return -1
	}
	for ; last > idx; last-- {
		l := s.links[last-1]
		delete(s.links, last-1)
		l.Index++
		s.links[l.Index] = l
	}
	return idx
}

// Renumber is session_renumber_windows: the windows take indexes from base in order.
func (s *Session) Renumber(base int) {
	links := s.Links()
	s.links = make(map[int]*Winlink, len(links))
	for i, wl := range links {
		wl.Index = base + i
		s.links[wl.Index] = wl
	}
}

// BorderStatus is the window's pane-border-status option.
func (w *Window) BorderStatus() layout.Status {
	return layout.Status(w.Options.GetNumber("pane-border-status"))
}

// LastWindows are the windows that were current before, most recent first (tmux's lastw).
func (s *Session) LastWindows() []*Winlink { return s.last }

// NextSessionID is the id the next session will take.
func (t *Tree) NextSessionID() int { return t.nextSession }

// LastWindow returns the previously current window, or nil.
func (s *Session) LastWindow() *Winlink {
	if len(s.last) == 0 {
		return nil
	}
	return s.last[0]
}

func removeLink(l []*Winlink, wl *Winlink) []*Winlink {
	out := l[:0:0]
	for _, x := range l {
		if x != wl {
			out = append(out, x)
		}
	}
	return out
}

// RemovePane takes a pane out of the tree. It reports whether that left its window empty, in
// which case the caller removes the window. The pane's process is the caller's to end.
func (t *Tree) RemovePane(p *Pane) (windowEmpty bool) {
	w := p.Window
	if w.Layout != nil && len(w.Panes) > 1 {
		// tmux unzooms, closes the pane's cell, and gives its space to a neighbor.
		w.Layout.Unzoom()
		w.Layout.Close(p.ID)
		defer t.fixPanes(w)
	}
	i := w.PaneIndex(p)
	if i >= 0 {
		w.Panes = append(w.Panes[:i], w.Panes[i+1:]...)
	}
	w.last = removePane(w.last, p)
	w.ZOrder = removePane(w.ZOrder, p)
	active := w.Active
	if w.Active == p {
		// tmux makes the last pane active, else the one before, else the one after.
		switch {
		case w.LastPane() != nil:
			w.Active = w.LastPane()
			w.last = w.last[1:]
		case len(w.Panes) == 0:
			w.Active = nil
		case i > 0:
			w.Active = w.Panes[i-1]
		default:
			w.Active = w.Panes[0]
		}
		if w.Active != nil {
			w.Active.Changed = true
		}
	}
	delete(t.panes, p.ID)
	if w.Active != nil && w.Active != active && t.ActiveChanged != nil {
		t.ActiveChanged(w, w.Active, p)
	}
	return len(w.Panes) == 0
}

// RemoveWindow takes a window and its panes out of its session. It reports whether that left
// the session without windows, in which case the caller removes the session.
func (t *Tree) RemoveWindow(w *Window) (sessionEmpty bool) {
	for _, p := range append([]*Pane(nil), w.Panes...) {
		t.RemovePane(p)
	}
	s := w.Session()
	wl := w.link
	delete(s.links, wl.Index)
	s.last = removeLink(s.last, wl)
	if s.Current == wl {
		// tmux's session_detach: the last window, else the previous one by index, wrapping
		// round to the highest.
		switch {
		case s.LastWindow() != nil:
			s.Current = s.LastWindow()
			s.last = s.last[1:]
		default:
			s.Current = nil
			links := s.Links()
			for i := len(links) - 1; i >= 0; i-- {
				if links[i].Index < wl.Index {
					s.Current = links[i]
					break
				}
			}
			if s.Current == nil && len(links) > 0 {
				s.Current = links[len(links)-1]
			}
		}
	}
	delete(t.windows, w.ID)
	return len(s.links) == 0
}

// RemoveSession takes a session and its windows out of the tree.
func (t *Tree) RemoveSession(s *Session) {
	for _, wl := range s.Links() {
		t.RemoveWindow(wl.Window)
	}
	delete(t.sessions, s.ID)
}

// Empty reports whether the tree has no sessions left (the server then exits).
func (t *Tree) Empty() bool { return len(t.sessions) == 0 }

// PaneMode is a mode a pane can be in (tmux's window_mode_entry): while the pane is in it, its
// screen is shown instead of the program's and the client's keys go to its key table.
type PaneMode interface {
	Screen() *vt.Screen
	Name() string     // #{pane_mode}
	KeyTable() string // copy-mode or copy-mode-vi
	// Formats are the mode's format variables (the mode's formats callback).
	Formats() map[string]string
}
