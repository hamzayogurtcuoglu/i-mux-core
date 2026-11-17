package target

import (
	"testing"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/model"
)

// TestMouseTarget checks = against a mouse event: the pane under the mouse, a window range's
// active pane, and no event at all.
func TestMouseTarget(t *testing.T) {
	tr := buildTree(t)
	s0 := tr.Session(0)
	cur := State{S: s0, WL: s0.Current, Idx: -1, W: s0.Current.Window, P: s0.Current.Window.Active}

	find := func(m *Mouse, typ Type) (State, error) {
		return Find(&Context{Tree: tr, Current: &cur, Mouse: m}, "=", typ, 0)
	}
	if _, err := find(nil, Pane); err == nil || err.Error() != "no mouse target" {
		t.Fatalf("no event: %v, want no mouse target", err)
	}

	// A pane of window @1 (index 2), not its active pane.
	m := &Mouse{Valid: true, S: 0, W: 1, P: 4}
	st, err := find(m, Pane)
	if err != nil || st.P == nil || st.P.ID != 4 || st.WL.Index != 2 {
		t.Fatalf("pane: %+v %v, want %%4 in window 2", st, err)
	}
	// As a window target, the window's active pane.
	st, err = find(m, Window)
	if err != nil || st.P != st.W.Active || st.WL.Index != 2 {
		t.Fatalf("window: %+v %v", st, err)
	}
	// A window range: no pane, so the window's active pane.
	st, err = find(&Mouse{Valid: true, S: 0, W: 0, P: -1}, Pane)
	if err != nil || st.WL.Index != 0 || st.P.ID != 0 {
		t.Fatalf("window range: %+v %v", st, err)
	}
	// A pane that is not in the window falls back to the window, as tmux's lookup falls
	// through from the pane to the window.
	st, err = find(&Mouse{Valid: true, S: 0, W: 0, P: 4}, Pane)
	if err != nil || st.WL.Index != 0 || st.P.ID != 0 {
		t.Fatalf("pane outside its window: %+v %v, want window 0's %%0", st, err)
	}
	// An event for a session that is gone is no target.
	if _, err := find(&Mouse{Valid: true, S: 9, W: -1, P: -1}, Pane); err == nil {
		t.Fatal("gone session: found")
	}

	// mouse_x and mouse_y are relative to the pane, below a top status line.
	p := &model.Pane{Xoff: 40, Yoff: 5, Sx: 20, Sy: 10}
	x, y, ok := MouseAt(p, &Mouse{X: 43, Y: 8, StatusAt: 0, StatusLines: 1}, false)
	if !ok || x != 3 || y != 2 {
		t.Fatalf("MouseAt = %d,%d %v, want 3,2", x, y, ok)
	}
	// The previous report's position, with the window shown from an offset.
	x, y, ok = MouseAt(p, &Mouse{LX: 1, LY: 1, OX: 40, OY: 5, StatusAt: -1}, true)
	if !ok || x != 1 || y != 1 {
		t.Fatalf("MouseAt last = %d,%d %v, want 1,1", x, y, ok)
	}
	if _, _, ok := MouseAt(p, &Mouse{X: 39, Y: 6, StatusAt: -1}, false); ok {
		t.Fatal("MouseAt outside the pane: inside")
	}
}
