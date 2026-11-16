package model

import (
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/layout"
)

// A window's layout operations, as tmux's spawn.c, window.c, resize.c and the split-window,
// kill-pane, resize-pane, swap-pane and select-layout commands drive layout.c (task C2.5).

// fixPanes gives every pane with a cell its cell's position and size (tmux's
// layout_fix_panes); a pane whose size changes is resized.
func (t *Tree) fixPanes(w *Window) {
	for _, p := range w.Panes {
		g, ok := w.Layout.PaneGeometry(p.ID)
		if !ok {
			continue
		}
		p.Xoff, p.Yoff = g.Xoff, g.Yoff
		if p.Sx != g.Sx || p.Sy != g.Sy {
			p.Sx, p.Sy = g.Sx, g.Sy
			if t.Resized != nil {
				t.Resized(p)
			}
		}
	}
}

// SetBorderStatus sets the window's pane-border-status, which takes a line from the panes on
// that edge.
func (t *Tree) SetBorderStatus(w *Window, s layout.Status) {
	w.Layout.Status = s
	t.fixPanes(w)
}

// SplitOptions say how split-window splits.
type SplitOptions struct {
	Horizontal bool // -h: side by side; else stacked
	Before     bool // -b
	Full       bool // -f: the whole window's width or height
	Size       int  // -l in cells; -1 for half
	Detached   bool // -d: the new pane does not become active
}

// SplitPane makes a new pane by splitting target (or the window with Full), as split-window
// does; the caller starts its program. A zoomed window is unzoomed first.
func (t *Tree) SplitPane(target *Pane, o SplitOptions) (*Pane, error) {
	w := target.Window
	t.Unzoom(w)
	typ := layout.TopBottom
	if o.Horizontal {
		typ = layout.LeftRight
	}
	id := t.nextPane
	err := w.Layout.Split(target.ID, id, layout.SplitOptions{
		Type: typ, Size: o.Size, Before: o.Before, Full: o.Full,
	})
	if err != nil {
		return nil, err
	}
	t.nextPane++
	p := &Pane{ID: id, Window: w}
	// tmux's window_add_pane: before or after the target; with -f at the start or the end.
	i := w.PaneIndex(target)
	switch {
	case o.Before && o.Full:
		i = 0
	case o.Before:
	case o.Full:
		i = len(w.Panes)
	default:
		i++
	}
	w.Panes = append(w.Panes, nil)
	copy(w.Panes[i+1:], w.Panes[i:])
	w.Panes[i] = p
	w.ZOrder = append(w.ZOrder, p)
	t.panes[p.ID] = p
	if g, ok := w.Layout.PaneGeometry(id); ok {
		p.Xoff, p.Yoff, p.Sx, p.Sy = g.Xoff, g.Yoff, g.Sx, g.Sy
	}
	t.fixPanes(w)
	if !o.Detached || w.Active == nil {
		t.SetActivePane(w, p)
	}
	return p, nil
}

// SetActivePane makes p the active pane, unzooming the window if p is hidden by the zoom.
func (t *Tree) SetActivePane(w *Window, p *Pane) {
	if w.Active == p {
		return
	}
	if z := w.Layout.Zoomed(); z >= 0 && z != p.ID {
		t.Unzoom(w)
	}
	w.SetActivePane(p)
}

// Zoom zooms p (resize-pane -Z on an unzoomed window); it reports whether it did.
func (t *Tree) Zoom(p *Pane) bool {
	w := p.Window
	if w.Layout.Zoomed() >= 0 || len(w.Panes) == 1 {
		return false
	}
	if w.Active != p {
		w.SetActivePane(p)
	}
	if !w.Layout.Zoom(p.ID, w.Sx, w.Sy) {
		return false
	}
	t.fixPanes(w)
	return true
}

// Unzoom restores a zoomed window's layout; it reports whether it was zoomed.
func (t *Tree) Unzoom(w *Window) bool {
	if w.Layout == nil || !w.Layout.Unzoom() {
		return false
	}
	t.fixPanes(w)
	return true
}

// ResizeWindow resizes a window and its layout, as tmux's resize_window: the window is no
// smaller than its layout can shrink to, and a zoom is kept.
func (t *Tree) ResizeWindow(w *Window, sx, sy int) {
	sx = min(max(sx, layout.PaneMinimum), layout.WindowMaximum)
	sy = min(max(sy, layout.PaneMinimum), layout.WindowMaximum)
	zoomed := t.Pane(w.Layout.Zoomed())
	if zoomed != nil {
		t.Unzoom(w)
	}
	w.Layout.Resize(sx, sy)
	w.Sx = max(sx, int(w.Layout.Root.Sx))
	w.Sy = max(sy, int(w.Layout.Root.Sy))
	t.fixPanes(w)
	if zoomed != nil {
		t.Zoom(zoomed)
	}
}

// ResizeWindowManual is resize-window: the window keeps this size (window-size manual) until
// something sets another.
func (t *Tree) ResizeWindowManual(w *Window, sx, sy int) {
	w.ManualSx, w.ManualSy = sx, sy
	t.ResizeWindow(w, sx, sy)
}

// recalculate is tmux's recalculate_sizes for one window: a window with a manual size goes back
// to it. (Sizes from attached clients come with tasks C2.7 and C2.8.)
func (t *Tree) recalculate(w *Window) {
	if w.ManualSx > 0 && (w.Sx != w.ManualSx || w.Sy != w.ManualSy) {
		t.ResizeWindow(w, w.ManualSx, w.ManualSy)
	}
}

// ResizePane grows (change > 0) or shrinks a pane's width (horizontal) or height, as
// resize-pane -L -R -U -D do; the window is unzoomed first.
func (t *Tree) ResizePane(p *Pane, horizontal bool, change int) {
	t.Unzoom(p.Window)
	typ := layout.TopBottom
	if horizontal {
		typ = layout.LeftRight
	}
	p.Window.Layout.ResizePane(p.ID, typ, change, true)
	t.fixPanes(p.Window)
}

// ResizePaneTo sets a pane's width (horizontal) or height, as resize-pane -x and -y do. A
// height counts the pane's border status line when the pane is on that edge.
func (t *Tree) ResizePaneTo(p *Pane, horizontal bool, size int) {
	w := p.Window
	t.Unzoom(w)
	typ := layout.TopBottom
	if horizontal {
		typ = layout.LeftRight
	} else {
		switch w.Layout.Status {
		case layout.StatusTop:
			if p.Yoff == 1 {
				size++
			}
		case layout.StatusBottom:
			if p.Yoff+p.Sy == w.Sy-1 {
				size++
			}
		}
	}
	p.Window.Layout.ResizePaneTo(p.ID, typ, size)
	t.fixPanes(p.Window)
}

// SwapPanes swaps two panes, in one window or two, as swap-pane -s src -t dst [-d] does.
func (t *Tree) SwapPanes(src, dst *Pane, detached bool) {
	if src == dst {
		return
	}
	sw, dw := src.Window, dst.Window
	t.Unzoom(dw)
	if sw != dw {
		t.Unzoom(sw)
	}
	// The panes' places in their windows' lists.
	si, di := sw.PaneIndex(src), dw.PaneIndex(dst)
	sw.Panes[si], dw.Panes[di] = dst, src
	// And in their windows' z orders (tmux's TAILQ_REPLACE and insert in cmd-swap-pane.c).
	zs, zd := zIndex(sw, src), zIndex(dw, dst)
	if zs >= 0 && zd >= 0 {
		sw.ZOrder[zs], dw.ZOrder[zd] = dst, src
	}
	src.Window, dst.Window = dw, sw
	// Their cells.
	if sw == dw {
		sw.Layout.SwapPanes(src.ID, dst.ID)
	} else {
		sw.Layout.Rename(src.ID, dst.ID)
		dw.Layout.Rename(dst.ID, src.ID)
	}
	if !detached {
		if sw != dw {
			t.SetActivePane(sw, dst)
			t.SetActivePane(dw, src)
		} else {
			t.SetActivePane(sw, dst)
		}
	} else {
		if sw.Active == src {
			t.SetActivePane(sw, dst)
		}
		if dw.Active == dst {
			t.SetActivePane(dw, src)
		}
	}
	if sw != dw {
		sw.last = removePane(sw.last, src)
		dw.last = removePane(dw.last, dst)
		t.fixPanes(sw)
	}
	t.fixPanes(dw)
}

// SelectLayout applies a layout string to a window, as select-layout does: the window takes
// the layout's size. The error is tmux's cause, which the command prints with the layout.
func (t *Tree) SelectLayout(w *Window, s string) error {
	t.Unzoom(w)
	p, err := layout.Parse(s)
	if err != nil {
		return err
	}
	if err := p.Fit(len(w.Panes)); err != nil {
		return err
	}
	w.Sx, w.Sy = p.Size()
	ids := make([]int, len(w.Panes))
	for i, q := range w.Panes {
		ids[i] = q.ID
	}
	res := w.Layout.Apply(p, ids)
	t.fixPanes(w)
	if res.Last != nil {
		if a := t.Pane(res.Active); a != nil {
			t.SetActivePane(w, a)
		}
		w.last = w.last[:0]
		for _, id := range res.Last {
			w.last = append(w.last, t.Pane(id))
		}
	}
	t.recalculate(w)
	return nil
}

// LayoutString is #{window_layout} (the saved layout while zoomed) or, with visible,
// #{window_visible_layout}; v1 writes the old format. base is pane-base-index.
func (w *Window) LayoutString(visible, v1 bool, base int) string {
	root := w.Layout.Saved()
	if visible {
		root = w.Layout.Root
	}
	if v1 {
		return layout.DumpV1(root)
	}
	return layout.DumpV2(root, func(id int) layout.PaneInfo {
		pi := layout.PaneInfo{Last: -1}
		for i, p := range w.Panes {
			if p.ID == id {
				pi.Index = i + base
				pi.Active = p == w.Active
			}
		}
		for i, p := range w.last {
			if p.ID == id {
				pi.Last = i
			}
		}
		return pi
	})
}

// Zoomed reports whether a pane of the window is zoomed.
func (w *Window) Zoomed() bool { return w.Layout != nil && w.Layout.Zoomed() >= 0 }

// zIndex is p's place in w's z order, or -1.
func zIndex(w *Window, p *Pane) int {
	for i, q := range w.ZOrder {
		if q == p {
			return i
		}
	}
	return -1
}
