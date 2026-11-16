// Ported from tmux 3.8's window.c:
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

package target

import (
	"strings"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/layout"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/model"
)

// Panes by position: {top}, {bottom-left}, … and the pane next to the active one in a
// direction ({up-of}, …), as tmux's window_find_string and window_pane_find_* find them.

func paneStatus(w *model.Window) layout.Status {
	if w.Layout == nil {
		return layout.StatusOff
	}
	return w.Layout.Status
}

func visible(p *model.Pane) bool {
	if z := p.Window.Layout; z != nil && z.Zoomed() >= 0 {
		return p.ID == z.Zoomed()
	}
	return true
}

// activeAt is window_get_active_at for tiled panes: the pane at x, y, borders to its right
// and below counted as its.
func activeAt(w *model.Window, x, y int) *model.Pane {
	status := paneStatus(w)
	if status == layout.StatusTop {
		for _, p := range w.ZOrder {
			if !visible(p) {
				continue
			}
			if x < p.Xoff || x > p.Xoff+p.Sx {
				continue
			}
			if y == p.Yoff-1 {
				return p
			}
		}
	}
	for _, p := range w.ZOrder {
		if !visible(p) {
			continue
		}
		if x < p.Xoff || x > p.Xoff+p.Sx {
			continue
		}
		if status == layout.StatusTop {
			if y < p.Yoff-1 || y > p.Yoff+p.Sy {
				continue
			}
		} else if y < p.Yoff || y > p.Yoff+p.Sy {
			continue
		}
		return p
	}
	return nil
}

// findString is window_find_string.
func findString(w *model.Window, s string) *model.Pane {
	x, y := w.Sx/2, w.Sy/2
	top, bottom := 0, w.Sy-1
	switch paneStatus(w) {
	case layout.StatusTop:
		top++
	case layout.StatusBottom:
		bottom--
	}
	switch strings.ToLower(s) {
	case "top":
		y = top
	case "bottom":
		y = bottom
	case "left":
		x = 0
	case "right":
		x = w.Sx - 1
	case "top-left":
		x, y = 0, top
	case "top-right":
		x, y = w.Sx-1, top
	case "bottom-left":
		x, y = 0, bottom
	case "bottom-right":
		x, y = w.Sx-1, bottom
	default:
		return nil
	}
	return activeAt(w, x, y)
}

// chooseBest is window_pane_choose_best: the most recently active.
func chooseBest(list []*model.Pane) *model.Pane {
	var best *model.Pane
	for _, p := range list {
		if best == nil || p.ActivePoint > best.ActivePoint {
			best = p
		}
	}
	return best
}

// overlaps reports whether [start, end] meets [lo, hi] as tmux checks it.
func overlaps(start, end, lo, hi int) bool {
	return (start < lo && end > hi) || (start >= lo && start <= hi) || (end >= lo && end <= hi)
}

// findUp is window_pane_find_up.
func findUp(p *model.Pane) *model.Pane {
	if p == nil {
		return nil
	}
	w := p.Window
	edge := p.Yoff
	switch paneStatus(w) {
	case layout.StatusTop:
		if edge == 1 {
			edge = w.Sy + 1
		}
	case layout.StatusBottom:
		if edge == 0 {
			edge = w.Sy
		}
	default:
		if edge == 0 {
			edge = w.Sy + 1
		}
	}
	left, right := p.Xoff, p.Xoff+p.Sx
	var list []*model.Pane
	for _, n := range w.Panes {
		if n == p || n.Yoff+n.Sy+1 != edge {
			continue
		}
		if overlaps(n.Xoff, n.Xoff+n.Sx-1, left, right) {
			list = append(list, n)
		}
	}
	return chooseBest(list)
}

// findDown is window_pane_find_down.
func findDown(p *model.Pane) *model.Pane {
	if p == nil {
		return nil
	}
	w := p.Window
	edge := p.Yoff + p.Sy + 1
	switch paneStatus(w) {
	case layout.StatusTop:
		if edge >= w.Sy {
			edge = 1
		}
	case layout.StatusBottom:
		if edge >= w.Sy-1 {
			edge = 0
		}
	default:
		if edge >= w.Sy {
			edge = 0
		}
	}
	left, right := p.Xoff, p.Xoff+p.Sx
	var list []*model.Pane
	for _, n := range w.Panes {
		if n == p || n.Yoff != edge {
			continue
		}
		if overlaps(n.Xoff, n.Xoff+n.Sx-1, left, right) {
			list = append(list, n)
		}
	}
	return chooseBest(list)
}

// findLeft is window_pane_find_left.
func findLeft(p *model.Pane) *model.Pane {
	if p == nil {
		return nil
	}
	w := p.Window
	edge := p.Xoff
	if edge == 0 {
		edge = w.Sx + 1
	}
	top, bottom := p.Yoff, p.Yoff+p.Sy
	var list []*model.Pane
	for _, n := range w.Panes {
		if n == p || n.Xoff+n.Sx+1 != edge {
			continue
		}
		if overlaps(n.Yoff, n.Yoff+n.Sy-1, top, bottom) {
			list = append(list, n)
		}
	}
	return chooseBest(list)
}

// findRight is window_pane_find_right.
func findRight(p *model.Pane) *model.Pane {
	if p == nil {
		return nil
	}
	w := p.Window
	edge := p.Xoff + p.Sx + 1
	if edge >= w.Sx {
		edge = 0
	}
	top, bottom := p.Yoff, p.Yoff+p.Sy
	var list []*model.Pane
	for _, n := range w.Panes {
		if n == p || n.Xoff != edge {
			continue
		}
		if overlaps(n.Yoff, n.Yoff+n.Sy-1, top, bottom) {
			list = append(list, n)
		}
	}
	return chooseBest(list)
}
