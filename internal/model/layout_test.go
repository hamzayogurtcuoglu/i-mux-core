package model

import (
	"testing"
	"time"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/layout"
)

// Layout operations the scenarios (testdata/layout) do not reach: swaps between windows, the
// resize notices, the last pane of a window.

func window(t *testing.T, tr *Tree, s *Session, sx, sy int) *Window {
	t.Helper()
	wl, err := tr.NewWindow(s, -1, 0, sx, sy)
	if err != nil {
		t.Fatal(err)
	}
	s.SelectWindow(wl)
	tr.NewPane(wl.Window, nil, false, sx, sy)
	return wl.Window
}

func TestSwapPanesBetweenWindows(t *testing.T) {
	tr := NewTree()
	s, _ := tr.NewSession("s", "/", time.Now())
	w1 := window(t, tr, s, 80, 24)
	w2 := window(t, tr, s, 100, 30)
	a := w1.Panes[0]
	b, err := tr.SplitPane(a, SplitOptions{Horizontal: true, Size: -1})
	if err != nil {
		t.Fatal(err)
	}
	c := w2.Panes[0]
	tr.SwapPanes(b, c, false)
	if b.Window != w2 || c.Window != w1 || w1.Panes[1] != c || w2.Panes[0] != b {
		t.Fatalf("panes not swapped: w1 %v, w2 %v", w1.Panes, w2.Panes)
	}
	if c.Sx != 39 || c.Xoff != 41 || b.Sx != 100 || b.Sy != 30 {
		t.Errorf("sizes: c %dx%d+%d, b %dx%d", c.Sx, c.Sy, c.Xoff, b.Sx, b.Sy)
	}
	if w1.Active != c || w2.Active != b {
		t.Errorf("active panes %d and %d", w1.Active.ID, w2.Active.ID)
	}
	for _, p := range w1.last {
		if p == b {
			t.Error("a pane that left w1 is still in its last-panes list")
		}
	}
	if got := w1.LayoutString(false, true, 0); got != layout.DumpV1(w1.Layout.Root) || w1.Layout.Leaf(c.ID) == nil {
		t.Errorf("w1 layout %s", got)
	}
}

func TestResizedNotices(t *testing.T) {
	tr := NewTree()
	resized := map[int]int{}
	tr.Resized = func(p *Pane) { resized[p.ID]++ }
	s, _ := tr.NewSession("s", "/", time.Now())
	w := window(t, tr, s, 80, 24)
	a := w.Panes[0]
	b, _ := tr.SplitPane(a, SplitOptions{Size: -1})
	if resized[a.ID] != 1 || resized[b.ID] != 0 {
		t.Errorf("after a split: %v", resized)
	}
	tr.ResizeWindow(w, 80, 24)
	if resized[a.ID] != 1 || resized[b.ID] != 0 {
		t.Errorf("a resize to the same size notified: %v", resized)
	}
	tr.Zoom(b)
	if resized[b.ID] != 1 || b.Sy != 24 {
		t.Errorf("after a zoom: %v, %dx%d", resized, b.Sx, b.Sy)
	}
	// Killing a pane unzooms and gives its space to its neighbor.
	tr.RemovePane(b)
	if w.Zoomed() || a.Sy != 24 || w.Active != a {
		t.Errorf("after the kill: zoomed %v, %dx%d", w.Zoomed(), a.Sx, a.Sy)
	}
	// The last pane: the window goes, its layout is not touched.
	if !tr.RemovePane(a) {
		t.Error("the window is not empty")
	}
}
