// Ported from tmux 3.8's layout.c:
//
// Copyright (c) 2009 Nicholas Marriott <nicholas.marriott@gmail.com>
// Copyright (c) 2016 Stephen Kent <smkent@smkent.net>
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

package layout

import "errors"

// A window's layout is a tree of cells: a node holds cells side by side (left to right) or
// stacked (top to bottom), a leaf holds a pane. Sizes are in cells and exclude the one-cell
// borders between siblings. Floating panes (new in tmux 3.8) are not supported: every leaf
// is tiled. Sizes are uint32, as tmux's u_int, so that the arithmetic wraps where tmux's does.

// Type is a cell's type.
type Type int

// Cell types.
const (
	LeftRight  Type = iota // a node: children side by side
	TopBottom              // a node: children stacked
	WindowPane             // a leaf: a pane
)

// Status is the window's pane-border-status: a top or bottom border line takes a line from
// the panes on that edge.
type Status int

// pane-border-status values.
const (
	StatusOff Status = iota
	StatusTop
	StatusBottom
)

// Size limits, as tmux's PANE_MINIMUM, PANE_MAXIMUM and WINDOW_MAXIMUM.
const (
	PaneMinimum   = 1
	PaneMaximum   = 10000
	WindowMaximum = 10000
)

// Cell is a node or a leaf of the layout tree.
type Cell struct {
	Type   Type
	Parent *Cell
	Sx, Sy uint32
	Xoff   int
	Yoff   int
	Pane   int // the pane's id, for a leaf; -1 when it has none yet
	Cells  []*Cell
}

// Layout is a window's layout: its tree, and the saved tree while a pane is zoomed.
type Layout struct {
	Root   *Cell
	Status Status

	leaves map[int]*Cell // by pane id, in Root

	savedRoot   *Cell
	savedLeaves map[int]*Cell
	zoomed      int // the zoomed pane, or -1
}

// ErrNoSpace is tmux's error when a pane is too small to split.
var ErrNoSpace = errors.New("no space for a new pane")

func newCell(parent *Cell) *Cell { return &Cell{Type: WindowPane, Parent: parent, Pane: -1} }

// New returns the layout of a window of sx×sy with one pane (tmux's layout_init).
func New(pane int, sx, sy int) *Layout {
	l := &Layout{leaves: map[int]*Cell{}, zoomed: -1}
	l.init(pane, sx, sy)
	return l
}

func (l *Layout) init(pane int, sx, sy int) {
	c := newCell(nil)
	c.setSize(uint32(sx), uint32(sy), 0, 0)
	l.Root = c
	l.leaves = map[int]*Cell{}
	l.makeLeaf(c, pane)
}

func (c *Cell) setSize(sx, sy uint32, xoff, yoff int) {
	c.Sx, c.Sy, c.Xoff, c.Yoff = sx, sy, xoff, yoff
}

func (l *Layout) makeLeaf(c *Cell, pane int) {
	c.Type = WindowPane
	c.Cells = nil
	c.Pane = pane
	l.leaves[pane] = c
}

func (l *Layout) makeNode(c *Cell, t Type) {
	c.Type = t
	c.Cells = nil
	if c.Pane >= 0 && l.leaves[c.Pane] == c {
		delete(l.leaves, c.Pane)
	}
	c.Pane = -1
}

// Leaf returns the cell holding pane, or nil (as while another pane is zoomed).
func (l *Layout) Leaf(pane int) *Cell { return l.leaves[pane] }

// Zoomed returns the zoomed pane, or -1.
func (l *Layout) Zoomed() int { return l.zoomed }

func isTiled(c *Cell) bool { return c.Type == WindowPane }

func hasTiledChild(c *Cell) bool {
	if c.Type == WindowPane {
		return false
	}
	for _, ch := range c.Cells {
		if isTiled(ch) || hasTiledChild(ch) {
			return true
		}
	}
	return false
}

func counts(c *Cell) bool { return isTiled(c) || hasTiledChild(c) }

func (c *Cell) index() int {
	for i, ch := range c.Parent.Cells {
		if ch == c {
			return i
		}
	}
	return -1
}

func isFirstTiled(c *Cell) bool {
	if c.Parent == nil {
		return isTiled(c)
	}
	for _, ch := range c.Parent.Cells {
		if counts(ch) {
			return ch == c
		}
	}
	return false
}

func isLastTiled(c *Cell) bool {
	if c.Parent == nil {
		return isTiled(c)
	}
	cells := c.Parent.Cells
	for i := len(cells) - 1; i >= 0; i-- {
		if counts(cells[i]) {
			return cells[i] == c
		}
	}
	return false
}

func fixOffsets1(c *Cell) {
	if c.Type == LeftRight {
		xoff := c.Xoff
		for _, ch := range c.Cells {
			if !counts(ch) {
				continue
			}
			ch.Xoff, ch.Yoff = xoff, c.Yoff
			if ch.Type != WindowPane {
				fixOffsets1(ch)
			}
			xoff += int(ch.Sx) + 1
		}
		return
	}
	yoff := c.Yoff
	for _, ch := range c.Cells {
		if !counts(ch) {
			continue
		}
		ch.Xoff, ch.Yoff = c.Xoff, yoff
		if ch.Type != WindowPane {
			fixOffsets1(ch)
		}
		yoff += int(ch.Sy) + 1
	}
}

// fixOffsets updates the cells' offsets from their sizes.
func (l *Layout) fixOffsets() {
	l.Root.Xoff, l.Root.Yoff = 0, 0
	fixOffsets1(l.Root)
}

func isTop(root, c *Cell) bool {
	for c != root {
		next := c.Parent
		if next == nil {
			return false
		}
		if next.Type == TopBottom && !isFirstTiled(c) {
			return false
		}
		c = next
	}
	return true
}

func isBottom(root, c *Cell) bool {
	for c != root {
		next := c.Parent
		if next == nil {
			return false
		}
		if next.Type == TopBottom && !isLastTiled(c) {
			return false
		}
		c = next
	}
	return true
}

// addHorizontalBorder reports whether a cell gives a line to the pane border status line:
// the top cells with a top status, the bottom ones with a bottom status.
func (l *Layout) addHorizontalBorder(c *Cell) bool {
	switch l.Status {
	case StatusTop:
		return isTop(l.Root, c)
	case StatusBottom:
		return isBottom(l.Root, c)
	}
	return false
}

// Geometry is where a pane is and its size.
type Geometry struct {
	Xoff, Yoff int
	Sx, Sy     int
}

// PaneGeometry returns a pane's position and size from its cell, less the border status line
// (tmux's layout_fix_panes). ok is false for a pane with no cell (hidden by a zoom).
func (l *Layout) PaneGeometry(pane int) (g Geometry, ok bool) {
	c := l.leaves[pane]
	if c == nil {
		return g, false
	}
	g = Geometry{Xoff: c.Xoff, Yoff: c.Yoff, Sx: int(c.Sx), Sy: int(c.Sy)}
	if l.addHorizontalBorder(c) {
		if l.Status == StatusTop {
			g.Yoff++
		}
		if g.Sy > 1 {
			g.Sy--
		}
	}
	return g, true
}

// Panes returns the panes in the tree, in tree order.
func (l *Layout) Panes() []int {
	var out []int
	var walk func(c *Cell)
	walk = func(c *Cell) {
		if c.Type == WindowPane {
			out = append(out, c.Pane)
			return
		}
		for _, ch := range c.Cells {
			walk(ch)
		}
	}
	walk(l.Root)
	return out
}

func countCells(c *Cell) int {
	if c.Type == WindowPane {
		return 1
	}
	n := 0
	for _, ch := range c.Cells {
		n += countCells(ch)
	}
	return n
}

// resizeCheck is how much a cell can shrink in the direction t.
func (l *Layout) resizeCheck(c *Cell, t Type) uint32 {
	if !counts(c) {
		return 0
	}
	var available, minimum uint32
	switch c.Type {
	case WindowPane:
		if t == LeftRight {
			available = c.Sx
			minimum = PaneMinimum
		} else {
			available = c.Sy
			minimum = PaneMinimum
			if l.addHorizontalBorder(c) {
				minimum = PaneMinimum + 1
			}
		}
		if available > minimum {
			available -= minimum
		} else {
			available = 0
		}
	case t:
		for _, ch := range c.Cells {
			available += l.resizeCheck(ch, t)
		}
	default:
		minimum = ^uint32(0)
		for _, ch := range c.Cells {
			if !counts(ch) {
				continue
			}
			if a := l.resizeCheck(ch, t); a < minimum {
				minimum = a
			}
		}
		available = minimum
	}
	return available
}

// resizeAdjust changes a cell's size, and its children's, by change (bounded already).
func (l *Layout) resizeAdjust(c *Cell, t Type, change int) {
	if t == LeftRight {
		c.Sx = uint32(int64(c.Sx) + int64(change))
	} else {
		c.Sy = uint32(int64(c.Sy) + int64(change))
	}
	if t == WindowPane {
		return
	}
	if c.Type != t {
		for _, ch := range c.Cells {
			if counts(ch) {
				l.resizeAdjust(ch, t, change)
			}
		}
		return
	}
	if !hasTiledChild(c) {
		return
	}
	for change != 0 {
		changed := false
		for _, ch := range c.Cells {
			if change == 0 {
				break
			}
			if !counts(ch) {
				continue
			}
			if change > 0 {
				l.resizeAdjust(ch, t, 1)
				change--
				changed = true
				continue
			}
			if l.resizeCheck(ch, t) > 0 {
				l.resizeAdjust(ch, t, -1)
				change++
				changed = true
			}
		}
		if !changed {
			break
		}
	}
}

// neighborDir is the next (dir true) or previous tiled sibling.
func neighborDir(c *Cell, dir bool) *Cell {
	cells := c.Parent.Cells
	i := c.index()
	for {
		if dir {
			i++
		} else {
			i--
		}
		if i < 0 || i >= len(cells) {
			return nil
		}
		if counts(cells[i]) {
			return cells[i]
		}
	}
}

// neighbor is the nearest tiled sibling, after the cell when there is one.
func neighbor(c *Cell) *Cell {
	if c.Parent == nil {
		return nil
	}
	dir := c.Parent.Cells[len(c.Parent.Cells)-1] != c
	if o := neighborDir(c, dir); o != nil {
		return o
	}
	return neighborDir(c, !dir)
}

func removeCell(parent, c *Cell) {
	i := c.index()
	parent.Cells = append(parent.Cells[:i], parent.Cells[i+1:]...)
}

func (l *Layout) forget(c *Cell) {
	if c.Type == WindowPane {
		if c.Pane >= 0 && l.leaves[c.Pane] == c {
			delete(l.leaves, c.Pane)
		}
		return
	}
	for _, ch := range c.Cells {
		l.forget(ch)
	}
}

// destroyCell removes a cell and gives its space to a neighbor (tmux's layout_destroy_cell).
// It reports whether the tree is now empty.
func (l *Layout) destroyCell(c *Cell) (empty bool) {
	parent := c.Parent
	if parent == nil {
		l.forget(c)
		l.Root = nil
		return true
	}
	if other := neighbor(c); other != nil {
		var change int
		if parent.Type == LeftRight {
			change = int(c.Sx) + 1
		} else {
			change = int(c.Sy) + 1
		}
		l.resizeAdjust(other, parent.Type, change)
	}
	removeCell(parent, c)
	l.forget(c)

	// A parent left with one cell is replaced by that cell.
	if len(parent.Cells) == 1 {
		only := parent.Cells[0]
		parent.Cells = nil
		only.Parent = parent.Parent
		if only.Parent == nil {
			if isTiled(only) {
				only.Xoff, only.Yoff = 0, 0
			}
			l.Root = only
		} else {
			only.Parent.Cells[parent.index()] = only
		}
	}
	return false
}

// Resize resizes the whole layout for a window of sx×sy (tmux's layout_resize). The layout
// does not shrink below its minimum; the window then stays larger (resize_window).
func (l *Layout) Resize(sx, sy int) {
	c := l.Root
	xchange := sx - int(c.Sx)
	xlimit := int(int32(l.resizeCheck(c, LeftRight)))
	if xchange < 0 && xchange < -xlimit {
		xchange = -xlimit
	}
	if xlimit == 0 {
		if uint32(sx) <= c.Sx {
			xchange = 0
		} else {
			xchange = sx - int(c.Sx)
		}
	}
	if xchange != 0 {
		l.resizeAdjust(c, LeftRight, xchange)
	}

	ychange := sy - int(c.Sy)
	ylimit := int(int32(l.resizeCheck(c, TopBottom)))
	if ychange < 0 && ychange < -ylimit {
		ychange = -ylimit
	}
	if ylimit == 0 {
		if uint32(sy) <= c.Sy {
			ychange = 0
		} else {
			ychange = sy - int(c.Sy)
		}
	}
	if ychange != 0 {
		l.resizeAdjust(c, TopBottom, ychange)
	}
	l.fixOffsets()
}

// ResizePaneTo sets a pane's width (LeftRight) or height (TopBottom) (layout_resize_pane_to).
func (l *Layout) ResizePaneTo(pane int, t Type, size int) {
	c := l.leaves[pane]
	if c == nil {
		return
	}
	parent := c.Parent
	for parent != nil && parent.Type != t {
		c = parent
		parent = c.Parent
	}
	if parent == nil {
		return
	}
	var cur int
	if t == LeftRight {
		cur = int(c.Sx)
	} else {
		cur = int(c.Sy)
	}
	change := size - cur
	if isLastTiled(c) {
		change = cur - size
	}
	l.ResizePane(pane, t, change, true)
}

// ResizePane grows (change > 0) or shrinks a pane in direction t (layout_resize_pane):
// against the next cell, or with opposite also the previous one.
func (l *Layout) ResizePane(pane int, t Type, change int, opposite bool) {
	c := l.leaves[pane]
	if c == nil {
		return
	}
	parent := c.Parent
	for parent != nil && parent.Type != t {
		c = parent
		parent = c.Parent
	}
	if parent == nil {
		return
	}
	if isLastTiled(c) {
		if c = neighborDir(c, false); c == nil {
			return
		}
	}
	l.resizeLayout(c, t, change, opposite)
}

// SearchByBorder is layout_search_by_border: the cell whose right or bottom border is at x, y,
// or nil.
func (l *Layout) SearchByBorder(x, y int) *Cell { return searchByBorder(l.Root, x, y) }

func searchByBorder(c *Cell, x, y int) *Cell {
	var last *Cell
	for _, child := range c.Cells {
		if x >= child.Xoff && x < child.Xoff+int(child.Sx) && y >= child.Yoff && y < child.Yoff+int(child.Sy) {
			return searchByBorder(child, x, y)
		}
		if last == nil {
			last = child
			continue
		}
		switch c.Type {
		case LeftRight:
			if x < child.Xoff && x >= last.Xoff+int(last.Sx) {
				return last
			}
		case TopBottom:
			if y < child.Yoff && y >= last.Yoff+int(last.Sy) {
				return last
			}
		}
		last = child
	}
	return nil
}

// ResizeCell is layout_resize_layout without its opposite: cell c grows or shrinks by change
// in its parent's direction.
func (l *Layout) ResizeCell(c *Cell, change int) {
	if c.Parent == nil {
		return
	}
	l.resizeLayout(c, c.Parent.Type, change, false)
}

func (l *Layout) resizeLayout(c *Cell, t Type, change int, opposite bool) {
	needed := change
	for needed != 0 {
		var size int
		if change > 0 {
			size = l.resizePaneGrow(c, t, needed, opposite)
			needed -= size
		} else {
			size = l.resizePaneShrink(c, t, needed)
			needed += size
		}
		if size == 0 {
			break
		}
	}
	l.fixOffsets()
}

func (l *Layout) resizePaneGrow(c *Cell, t Type, needed int, opposite bool) int {
	var size uint32
	remove := neighborDir(c, true)
	for remove != nil {
		if size = l.resizeCheck(remove, t); size > 0 {
			break
		}
		remove = neighborDir(remove, true)
	}
	if opposite && remove == nil {
		remove = neighborDir(c, false)
		for remove != nil {
			if size = l.resizeCheck(remove, t); size > 0 {
				break
			}
			remove = neighborDir(remove, false)
		}
	}
	if remove == nil {
		return 0
	}
	if size > uint32(needed) {
		size = uint32(needed)
	}
	l.resizeAdjust(c, t, int(size))
	l.resizeAdjust(remove, t, -int(size))
	return int(size)
}

func (l *Layout) resizePaneShrink(c *Cell, t Type, needed int) int {
	var size uint32
	remove := c
	for remove != nil {
		if size = l.resizeCheck(remove, t); size != 0 {
			break
		}
		remove = neighborDir(remove, false)
	}
	if remove == nil {
		return 0
	}
	add := neighborDir(c, true)
	if add == nil {
		return 0
	}
	if size > uint32(-needed) {
		size = uint32(-needed)
	}
	l.resizeAdjust(add, t, int(size))
	l.resizeAdjust(remove, t, -int(size))
	return int(size)
}

// newPaneSize is a child's new size when its parent goes from previous to size.
func (l *Layout) newPaneSize(previous uint32, c *Cell, t Type, size, countLeft, sizeLeft uint32) uint32 {
	if countLeft == 1 {
		return sizeLeft
	}
	available := l.resizeCheck(c, t)
	minimum := uint32(PaneMinimum+1) * (countLeft - 1)
	var newSize uint32
	if t == LeftRight {
		if c.Sx-available > minimum {
			minimum = c.Sx - available
		}
		newSize = uint32((uint64(c.Sx) * uint64(size)) / uint64(previous))
	} else {
		if c.Sy-available > minimum {
			minimum = c.Sy - available
		}
		newSize = uint32((uint64(c.Sy) * uint64(size)) / uint64(previous))
	}
	maximum := sizeLeft - minimum
	if newSize > maximum {
		newSize = maximum
	}
	if newSize < PaneMinimum {
		newSize = PaneMinimum
	}
	return newSize
}

// setSizeCheck reports whether a cell and its children can take size in direction t.
func (l *Layout) setSizeCheck(c *Cell, t Type, size int) bool {
	if c.Type == WindowPane {
		return size >= PaneMinimum
	}
	available := uint32(size)
	count := uint32(len(c.Cells))
	if c.Type == t {
		if available < count*2-1 {
			return false
		}
		previous := c.Sy
		if t == LeftRight {
			previous = c.Sx
		}
		for idx, ch := range c.Cells {
			newSize := l.newPaneSize(previous, ch, t, uint32(size), count-uint32(idx), available)
			if uint32(idx) == count-1 {
				if newSize > available {
					return false
				}
				available -= newSize
			} else {
				if newSize+1 > available {
					return false
				}
				available -= newSize + 1
			}
			if !l.setSizeCheck(ch, t, int(int32(newSize))) {
				return false
			}
		}
		return true
	}
	for _, ch := range c.Cells {
		if ch.Type == WindowPane {
			continue
		}
		if !l.setSizeCheck(ch, t, size) {
			return false
		}
	}
	return true
}

// resizeChildCells fits a node's children into its size.
func (l *Layout) resizeChildCells(c *Cell) {
	if c.Type == WindowPane {
		return
	}
	var count, prev uint32
	for _, ch := range c.Cells {
		if !counts(ch) {
			continue
		}
		count++
		if c.Type == LeftRight {
			prev += ch.Sx
		} else {
			prev += ch.Sy
		}
	}
	prev += count - 1

	available := c.Sy
	if c.Type == LeftRight {
		available = c.Sx
	}
	idx := uint32(0)
	for _, ch := range c.Cells {
		if !counts(ch) {
			continue
		}
		if c.Type == TopBottom {
			ch.Sx, ch.Xoff = c.Sx, c.Xoff
		} else {
			ch.Sx = l.newPaneSize(prev, ch, c.Type, c.Sx, count-idx, available)
			available -= ch.Sx + 1
		}
		if c.Type == LeftRight {
			ch.Sy, ch.Yoff = c.Sy, c.Yoff
		} else {
			ch.Sy = l.newPaneSize(prev, ch, c.Type, c.Sy, count-idx, available)
			available -= ch.Sy + 1
		}
		l.resizeChildCells(ch)
		idx++
	}
}

// replaceWithNode puts a new node of type t in c's place, with c as its only child.
func (l *Layout) replaceWithNode(c *Cell, t Type) *Cell {
	parent := newCell(c.Parent)
	l.makeNode(parent, t)
	parent.setSize(c.Sx, c.Sy, c.Xoff, c.Yoff)
	if c.Parent == nil {
		l.Root = parent
	} else {
		c.Parent.Cells[c.index()] = parent
	}
	c.Parent = parent
	parent.Cells = []*Cell{c}
	return parent
}

func (l *Layout) splitCheckSpace(c *Cell, t Type) bool {
	if t == LeftRight {
		return c.Sx >= PaneMinimum*2+1
	}
	minimum := uint32(PaneMinimum*2 + 1)
	if l.addHorizontalBorder(c) {
		minimum = PaneMinimum*2 + 2
	}
	return c.Sy >= minimum
}

// splitSizes are the two cells' sizes when c is split: size is the new pane's size, or -1
// for half (tmux's layout_split_sizes).
func splitSizes(c *Cell, size int, before bool, t Type) (s1, s2, ss uint32) {
	ss = c.Sy
	if t == LeftRight {
		ss = c.Sx
	}
	switch {
	case size < 0:
		s2 = ((ss + 1) / 2) - 1
	case before:
		s2 = ss - uint32(size) - 1
	default:
		s2 = uint32(size)
	}
	if s2 < PaneMinimum {
		s2 = PaneMinimum
	} else if s2 > ss-2 {
		s2 = ss - 2
	}
	s1 = ss - 1 - s2
	return s1, s2, ss
}

// SplitOptions say how to split.
type SplitOptions struct {
	Type   Type // LeftRight (-h) or TopBottom (-v)
	Size   int  // the new pane's size in cells; -1 for half
	Before bool // -b: the new pane goes before (left or above)
	Full   bool // -f: split the whole window
}

// Split splits pane's cell, or the whole window with Full, and makes a cell for newPane
// (tmux's layout_split_pane followed by layout_assign_pane).
func (l *Layout) Split(pane, newPane int, o SplitOptions) error {
	c := l.leaves[pane]
	if o.Full {
		c = l.Root
	}
	if c == nil {
		return ErrNoSpace
	}
	t := o.Type
	sx, sy, xoff, yoff := c.Sx, c.Sy, c.Xoff, c.Yoff
	if !l.splitCheckSpace(c, t) {
		return ErrNoSpace
	}
	size1, size2, saved := splitSizes(c, o.Size, o.Before, t)
	newSize := size1
	if o.Before {
		newSize = size2
	}
	if o.Full && !l.setSizeCheck(c, t, int(int32(newSize))) {
		return ErrNoSpace
	}

	var cnew *Cell
	resizeFirst := false
	switch {
	case c.Parent != nil && c.Parent.Type == t:
		parent := c.Parent
		cnew = newCell(parent)
		i := c.index()
		if !o.Before {
			i++
		}
		parent.Cells = insertAt(parent.Cells, i, cnew)
	case o.Full && c.Parent == nil && c.Type == t:
		if c.Type == LeftRight {
			c.Sx = newSize
			l.resizeChildCells(c)
			c.Sx = saved
		} else {
			c.Sy = newSize
			l.resizeChildCells(c)
			c.Sy = saved
		}
		resizeFirst = true
		cnew = newCell(c)
		size := saved - 1 - newSize
		if c.Type == LeftRight {
			cnew.setSize(size, sy, 0, 0)
		} else {
			cnew.setSize(sx, size, 0, 0)
		}
		if o.Before {
			c.Cells = insertAt(c.Cells, 0, cnew)
		} else {
			c.Cells = append(c.Cells, cnew)
		}
	default:
		parent := l.replaceWithNode(c, t)
		cnew = newCell(parent)
		if o.Before {
			parent.Cells = insertAt(parent.Cells, 0, cnew)
		} else {
			parent.Cells = append(parent.Cells, cnew)
		}
	}
	c1, c2 := c, cnew
	if o.Before {
		c1, c2 = cnew, c
	}
	if !resizeFirst {
		if t == LeftRight {
			c1.setSize(size1, sy, xoff, yoff)
			c2.setSize(size2, sy, xoff+int(c1.Sx)+1, yoff)
		} else {
			c1.setSize(sx, size1, xoff, yoff)
			c2.setSize(sx, size2, xoff, yoff+int(c1.Sy)+1)
		}
	}
	if o.Full {
		if !resizeFirst {
			l.resizeChildCells(c)
		}
		l.fixOffsets()
	}
	l.makeLeaf(cnew, newPane)
	return nil
}

func insertAt(cells []*Cell, i int, c *Cell) []*Cell {
	cells = append(cells, nil)
	copy(cells[i+1:], cells[i:])
	cells[i] = c
	return cells
}

// Close removes a pane's cell and gives its space to a neighbor (tmux's layout_close_pane).
// It reports whether the layout is now empty.
func (l *Layout) Close(pane int) (empty bool) {
	c := l.leaves[pane]
	if c == nil {
		// A pane hidden by a zoom: its saved cell goes (the zoom ends first in tmux).
		return false
	}
	if l.destroyCell(c) {
		return true
	}
	l.fixOffsets()
	return false
}

// SwapPanes swaps the panes in two cells (swap-pane).
func (l *Layout) SwapPanes(a, b int) {
	ca, cb := l.leaves[a], l.leaves[b]
	if ca == nil || cb == nil {
		return
	}
	ca.Pane, cb.Pane = b, a
	l.leaves[a], l.leaves[b] = cb, ca
}

// Zoom makes pane fill the window of sx×sy, saving the layout (tmux's window_zoom).
func (l *Layout) Zoom(pane, sx, sy int) bool {
	if l.zoomed >= 0 || len(l.leaves) <= 1 || l.leaves[pane] == nil {
		return false
	}
	l.savedRoot, l.savedLeaves = l.Root, l.leaves
	l.init(pane, sx, sy)
	l.zoomed = pane
	return true
}

// Unzoom restores the saved layout (tmux's window_unzoom).
func (l *Layout) Unzoom() bool {
	if l.zoomed < 0 {
		return false
	}
	l.Root, l.leaves = l.savedRoot, l.savedLeaves
	l.savedRoot, l.savedLeaves = nil, nil
	l.zoomed = -1
	return true
}

// Saved returns the layout's tree as #{window_layout} shows it: the saved one while zoomed.
func (l *Layout) Saved() *Cell {
	if l.savedRoot != nil {
		return l.savedRoot
	}
	return l.Root
}

// Rename puts pane to in pane from's cell (swap-pane between windows).
func (l *Layout) Rename(from, to int) {
	c := l.leaves[from]
	if c == nil {
		return
	}
	delete(l.leaves, from)
	c.Pane = to
	l.leaves[to] = c
}
