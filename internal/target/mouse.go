package target

import "github.com/hamzayogurtcuoglu/i-mux-core/internal/model"

// Mouse is a mouse event (tmux's struct mouse_event): where it happened on the client's
// terminal and what it hit. S, W and P are ids, -1 when not set.
type Mouse struct {
	Valid  bool
	Ignore bool // a double click made by the click timer, which commands must not act on twice

	StatusAt    int // the status lines' first line, -1 with no status line
	StatusLines int

	X, Y, B    int
	LX, LY, LB int // the previous report's
	OX, OY     int // the window's offset on the terminal

	S, W, P int

	SGRType byte
	SGRB    int

	// StatusRange is the status range under the mouse, as #{mouse_status_range} gives it,
	// and HasStatusRange whether the mouse is on a status line with one.
	StatusRange    string
	HasStatusRange bool
}

// MouseWindow is cmd_mouse_window: the winlink under the mouse and its session.
func MouseWindow(tree *model.Tree, m *Mouse) (*model.Session, *model.Winlink) {
	if m == nil || !m.Valid || m.S == -1 {
		return nil, nil
	}
	s := tree.Session(m.S)
	if s == nil {
		return nil, nil
	}
	if m.W == -1 {
		return s, s.Current
	}
	w := tree.Window(m.W)
	if w == nil {
		return nil, nil
	}
	for _, wl := range s.Links() {
		if wl.Window == w {
			return s, wl
		}
	}
	return s, nil
}

// MousePane is cmd_mouse_pane: the pane under the mouse, its session and winlink.
func MousePane(tree *model.Tree, m *Mouse) (*model.Session, *model.Winlink, *model.Pane) {
	s, wl := MouseWindow(tree, m)
	if wl == nil {
		return nil, nil, nil
	}
	var p *model.Pane
	if m.P == -1 {
		p = wl.Window.Active
	} else {
		p = tree.Pane(m.P)
		if p == nil || p.Window != wl.Window {
			return nil, nil, nil
		}
	}
	return s, wl, p
}

// MouseAt is cmd_mouse_at: the mouse's position in pane p (the previous report's with last),
// ok false when it is outside it.
func MouseAt(p *model.Pane, m *Mouse, last bool) (x, y int, ok bool) {
	if last {
		x, y = m.LX+m.OX, m.LY+m.OY
	} else {
		x, y = m.X+m.OX, m.Y+m.OY
	}
	if m.StatusAt == 0 && y >= m.StatusLines {
		y -= m.StatusLines
	}
	if x < p.Xoff || x >= p.Xoff+p.Sx || y < p.Yoff || y >= p.Yoff+p.Sy {
		return 0, 0, false
	}
	return x - p.Xoff, y - p.Yoff, true
}

// fromMouse is the = and {mouse} target (cmd_find_target): the pane under the mouse, else
// the window's active pane.
func (f *finder) fromMouse(typ Type) bool {
	m := f.ctx.Mouse
	tree := f.ctx.Tree
	if typ == Pane {
		if s, wl, p := MousePane(tree, m); p != nil {
			f.st.S, f.st.WL, f.st.W, f.st.P = s, wl, wl.Window, p
			return true
		}
	}
	s, wl := MouseWindow(tree, m)
	if wl == nil && s != nil {
		wl = s.Current
	}
	if wl == nil {
		return false
	}
	f.st.S, f.st.WL, f.st.W, f.st.P = s, wl, wl.Window, wl.Window.Active
	return f.st.P != nil
}

// FromMouse is cmd_find_from_mouse: the state for the pane under the mouse.
func FromMouse(tree *model.Tree, m *Mouse) (State, bool) {
	s, wl, p := MousePane(tree, m)
	if p == nil {
		return State{Idx: -1}, false
	}
	return State{S: s, WL: wl, Idx: -1, W: wl.Window, P: p}, true
}
