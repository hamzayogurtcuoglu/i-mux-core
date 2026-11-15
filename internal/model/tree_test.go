package model

import (
	"errors"
	"testing"
	"time"
)

var t0 = time.Unix(1000, 0)

func mustSession(t *testing.T, tr *Tree, name string, at time.Time) *Session {
	t.Helper()
	s, err := tr.NewSession(name, "/", at)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func mustWindow(t *testing.T, tr *Tree, s *Session, idx int) *Winlink {
	t.Helper()
	wl, err := tr.NewWindow(s, idx, 0, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	tr.NewPane(wl.Window, nil, false, 80, 24)
	s.SelectWindow(wl)
	return wl
}

func TestSessionIDsAndNames(t *testing.T) {
	tr := NewTree()
	a := mustSession(t, tr, "", t0)
	b := mustSession(t, tr, "work", t0)
	if a.ID != 0 || a.Name != "0" || b.ID != 1 {
		t.Fatalf("a %d %q, b %d", a.ID, a.Name, b.ID)
	}
	// A default name skips one taken by an explicit name, using up the id as tmux does.
	mustSession(t, tr, "3", t0) // id 2
	c := mustSession(t, tr, "", t0)
	if c.ID != 4 || c.Name != "4" {
		t.Fatalf("c %d %q, want 4 \"4\" (id 3 taken by the name \"3\")", c.ID, c.Name)
	}
	if _, err := tr.NewSession("work", "/", t0); !errors.Is(err, ErrDuplicateSession) {
		t.Fatalf("duplicate: %v", err)
	}
	if err := tr.RenameSession(a, "work"); !errors.Is(err, ErrDuplicateSession) {
		t.Fatalf("rename to a taken name: %v", err)
	}
	if err := tr.RenameSession(a, "home"); err != nil || tr.SessionByName("home") != a {
		t.Fatalf("rename: %v", err)
	}
	names := []string{}
	for _, s := range tr.Sessions() {
		names = append(names, s.Name)
	}
	if want := "[3 4 home work]"; fmtSlice(names) != want {
		t.Fatalf("order %v, want by name %s", names, want)
	}
	// Ids are never reused.
	tr.RemoveSession(c)
	if d := mustSession(t, tr, "", t0); d.ID != 5 {
		t.Fatalf("id after a removal: %d", d.ID)
	}
}

func fmtSlice(s []string) string {
	out := "["
	for i, x := range s {
		if i > 0 {
			out += " "
		}
		out += x
	}
	return out + "]"
}

func TestWindowIndexes(t *testing.T) {
	tr := NewTree()
	s := mustSession(t, tr, "s", t0)
	w0 := mustWindow(t, tr, s, -1)
	w1 := mustWindow(t, tr, s, -1)
	w5 := mustWindow(t, tr, s, 5)
	if w0.Index != 0 || w1.Index != 1 || w5.Index != 5 {
		t.Fatalf("indexes %d %d %d", w0.Index, w1.Index, w5.Index)
	}
	if _, err := tr.NewWindow(s, 5, 0, 80, 24); !errors.Is(err, ErrIndexInUse) {
		t.Fatalf("index in use: %v", err)
	}
	// The first free index from base-index.
	tr.RemoveWindow(w0.Window)
	if wl, _ := tr.NewWindow(s, -1, 0, 80, 24); wl.Index != 0 {
		t.Fatalf("reused index %d, want 0", wl.Index)
	}
	if wl, _ := tr.NewWindow(s, -1, 1, 80, 24); wl.Index != 2 {
		t.Fatalf("base-index 1: index %d, want 2", wl.Index)
	}
	if w0.Window.ID != 0 || w1.Window.ID != 1 || tr.Window(1) != w1.Window || w1.Window.Index() != 1 || w1.Window.Session() != s {
		t.Fatal("window ids or links")
	}
}

func TestPanesAndActive(t *testing.T) {
	tr := NewTree()
	s := mustSession(t, tr, "s", t0)
	wl := mustWindow(t, tr, s, -1)
	w := wl.Window
	p0 := w.Panes[0]
	p1 := tr.NewPane(w, p0, false, 40, 24)
	pb := tr.NewPane(w, p0, true, 40, 24) // before p0
	if w.PaneIndex(pb) != 0 || w.PaneIndex(p0) != 1 || w.PaneIndex(p1) != 2 {
		t.Fatalf("order: %d %d %d", w.PaneIndex(pb), w.PaneIndex(p0), w.PaneIndex(p1))
	}
	if w.Active != p0 {
		t.Fatal("the first pane is not active")
	}
	w.SetActivePane(p1)
	w.SetActivePane(pb)
	if w.LastPane() != p1 {
		t.Fatalf("last pane %%%d, want %%%d", w.LastPane().ID, p1.ID)
	}
	// Removing the active pane activates the last one.
	tr.RemovePane(pb)
	if w.Active != p1 || w.LastPane() != p0 {
		t.Fatalf("after removing the active pane: active %%%d", w.Active.ID)
	}
	// With no last pane, the one before it, else the one after.
	tr2 := NewTree()
	s2 := mustSession(t, tr2, "s", t0)
	w2 := mustWindow(t, tr2, s2, -1).Window
	a := w2.Panes[0]
	b := tr2.NewPane(w2, a, false, 1, 1)
	c := tr2.NewPane(w2, b, false, 1, 1)
	w2.Active = b
	tr2.RemovePane(b)
	if w2.Active != a {
		t.Fatalf("active %%%d, want the one before", w2.Active.ID)
	}
	w2.Active = a
	w2.last = nil
	tr2.RemovePane(a)
	if w2.Active != c {
		t.Fatalf("active %%%d, want the one after", w2.Active.ID)
	}
	if empty := tr2.RemovePane(c); !empty || w2.Active != nil || tr2.Pane(c.ID) != nil {
		t.Fatal("the last pane gone should leave the window empty")
	}
}

func TestRemoveWindowCurrent(t *testing.T) {
	tr := NewTree()
	s := mustSession(t, tr, "s", t0)
	w0 := mustWindow(t, tr, s, 0)
	w1 := mustWindow(t, tr, s, 1)
	w2 := mustWindow(t, tr, s, 2)
	// Current w2, last w1: removing w2 goes to the last window.
	tr.RemoveWindow(w2.Window)
	if s.Current != w1 {
		t.Fatalf("current %d, want the last window 1", s.Current.Index)
	}
	// No last window left: the previous one by index, wrapping to the highest.
	s.last = nil
	s.SelectWindow(w0)
	s.last = nil
	if empty := tr.RemoveWindow(w0.Window); empty || s.Current != w1 {
		t.Fatalf("after removing window 0: current %v (wraps to the highest)", s.Current)
	}
	if empty := tr.RemoveWindow(w1.Window); !empty {
		t.Fatal("removing the last window should leave the session empty")
	}
	tr.RemoveSession(s)
	if !tr.Empty() || len(tr.panes) != 0 || len(tr.windows) != 0 {
		t.Fatal("the tree is not empty")
	}
}

func TestSelectWindowAndLast(t *testing.T) {
	tr := NewTree()
	s := mustSession(t, tr, "s", t0)
	w0 := mustWindow(t, tr, s, 0)
	w1 := mustWindow(t, tr, s, 1)
	if s.Current != w1 || s.LastWindow() != w0 {
		t.Fatal("current or last")
	}
	s.SelectWindow(w0)
	if s.Current != w0 || s.LastWindow() != w1 {
		t.Fatal("after selecting window 0")
	}
	s.SelectWindow(w0)
	if s.LastWindow() != w1 {
		t.Fatal("selecting the current window changed the last one")
	}
}

func TestBestSession(t *testing.T) {
	tr := NewTree()
	if tr.BestSession() != nil {
		t.Fatal("best of none")
	}
	a := mustSession(t, tr, "a", t0)
	b := mustSession(t, tr, "b", t0)
	empty := mustSession(t, tr, "c", t0.Add(time.Hour)) // no window: not a target
	mustWindow(t, tr, a, -1)
	mustWindow(t, tr, b, -1)
	if got := tr.BestSession(); got != a {
		t.Fatalf("tie: %q, want the first by name", got.Name)
	}
	b.Activity = t0.Add(time.Minute)
	if got := tr.BestSession(); got != b {
		t.Fatalf("newest activity: %q", got.Name)
	}
	_ = empty
}
