// Ported from tmux 3.8's layout-custom.c:
//
// Copyright (c) 2010 Nicholas Marriott <nicholas.marriott@gmail.com>
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

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Layout strings (#{window_layout} and select-layout). tmux 3.8 writes version 2, JSON:
//
//	{"V":2,"L":{"t":"h","w":80,"h":24,"x":0,"y":0,"c":[…]}}
//
// where a pane cell has "a":true (the active pane) or "l":N (its place in the last-panes list),
// "i" (its index) and "I" ("%id", ignored when read). Version 1, "csum,WxH,X,Y{…}" with [ ] for
// stacked cells and the pane id after a pane's offsets, is still written for control clients
// and read for layouts saved by older tmux.

// PaneInfo is what a version 2 layout says of a pane.
type PaneInfo struct {
	Index  int  // its index in the window (pane-base-index added)
	Active bool // the window's active pane
	Last   int  // its place in the window's last-panes list, or -1
}

// Checksum is tmux's layout checksum.
func Checksum(s string) uint16 {
	var csum uint16
	for i := 0; i < len(s); i++ {
		csum = (csum >> 1) + ((csum & 1) << 15)
		csum += uint16(s[i])
	}
	return csum
}

// DumpV1 writes the layout of root in version 1 ("0000," for none, as tmux).
func DumpV1(root *Cell) string {
	if root == nil {
		return "0000,"
	}
	var b strings.Builder
	appendV1(&b, root)
	s := b.String()
	return fmt.Sprintf("%04x,%s", Checksum(s), s)
}

func appendV1(b *strings.Builder, c *Cell) {
	if c.Type == WindowPane {
		fmt.Fprintf(b, "%dx%d,%d,%d,%d", c.Sx, c.Sy, c.Xoff, c.Yoff, c.Pane)
		return
	}
	fmt.Fprintf(b, "%dx%d,%d,%d", c.Sx, c.Sy, c.Xoff, c.Yoff)
	open, end := byte('['), byte(']')
	if c.Type == LeftRight {
		open, end = '{', '}'
	}
	b.WriteByte(open)
	for i, ch := range c.Cells {
		if i > 0 {
			b.WriteByte(',')
		}
		appendV1(b, ch)
	}
	b.WriteByte(end)
}

// DumpV2 writes the layout of root in version 2; info describes each pane. It returns "" for
// no layout (tmux gives no value).
func DumpV2(root *Cell, info func(pane int) PaneInfo) string {
	if root == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString(`{"V":2,"L":`)
	appendV2(&b, root, info)
	b.WriteString("}")
	return b.String()
}

func appendV2(b *strings.Builder, c *Cell, info func(int) PaneInfo) {
	t := map[Type]byte{TopBottom: 'v', LeftRight: 'h', WindowPane: 'p'}[c.Type]
	fmt.Fprintf(b, `{"t":"%c","w":%d,"h":%d,"x":%d,"y":%d`, t, c.Sx, c.Sy, c.Xoff, c.Yoff)
	if c.Type != WindowPane {
		b.WriteString(`,"c":[`)
		for i, ch := range c.Cells {
			if i > 0 {
				b.WriteByte(',')
			}
			appendV2(b, ch, info)
		}
		b.WriteString("]")
	} else {
		pi := info(c.Pane)
		if pi.Active {
			b.WriteString(`,"a":true`)
		} else if pi.Last >= 0 {
			fmt.Fprintf(b, `,"l":%d`, pi.Last)
		}
		fmt.Fprintf(b, `,"i":%d,"I":"%%%d"`, pi.Index, c.Pane)
	}
	b.WriteString("}")
}

// cellCtx is what a version 2 layout says of one of its pane cells.
type cellCtx struct {
	c      *Cell
	active int // 1, 0, or -1 when not given
	last   int
	index  int
}

// Parsed is a layout string read and checked, before it is applied to a window.
type Parsed struct {
	Version int
	tree    *Layout
	ctxs    []cellCtx
}

// Parse reads a layout string of either version, with tmux's errors.
func Parse(input string) (*Parsed, error) {
	input = strings.TrimLeft(input, " \t\n\v\f\r")
	p := &Parsed{tree: &Layout{leaves: map[int]*Cell{}, zoomed: -1}}
	if !strings.HasPrefix(input, "{") {
		csum, ok := parseChecksum(input)
		if !ok {
			return nil, errors.New("malformed layout header")
		}
		input = input[5:]
		if csum != Checksum(input) {
			return nil, errors.New("invalid layout checksum")
		}
		rest := input
		root := constructV1(nil, &rest, 0)
		if root == nil {
			return nil, errors.New("invalid layout")
		}
		if rest != "" {
			return nil, errors.New("trailing data")
		}
		p.Version, p.tree.Root = 1, root
		return p, nil
	}
	if err := p.parseJSON(input); err != nil {
		return nil, err
	}
	if p.Version != 2 {
		return nil, errors.New("version mismatch")
	}
	active := 0
	for _, cc := range p.ctxs {
		if cc.active == 1 {
			active++
		}
	}
	if active > 1 {
		return nil, errors.New("more than one active pane")
	}
	if len(p.ctxs) == 0 {
		return nil, errors.New("no panes")
	}
	if err := p.checkIndexes(); err != nil {
		return nil, err
	}
	return p, nil
}

// parseChecksum reads the "xxxx," header as tmux's sscanf("%hx,%n") with n == 5 does.
func parseChecksum(s string) (uint16, bool) {
	if len(s) < 5 || s[4] != ',' {
		return 0, false
	}
	h := s[:4]
	if strings.HasPrefix(h, "0x") || strings.HasPrefix(h, "0X") {
		h = h[2:]
	}
	v, err := strconv.ParseUint(h, 16, 16)
	if err != nil {
		return 0, false
	}
	return uint16(v), true
}

// v1MaxDepth bounds the nesting of a version 1 layout.
const v1MaxDepth = 1000

// digits reads a run of digits; value is its first five digits, as sscanf's %5u reads them.
func digits(s *string) (value int, n int) {
	for n < len(*s) && (*s)[n] >= '0' && (*s)[n] <= '9' {
		if n < 5 {
			value = value*10 + int((*s)[n]-'0')
		}
		n++
	}
	*s = (*s)[n:]
	return value, n
}

func constructCell(parent *Cell, s *string) *Cell {
	rest := *s
	sx, n := digits(&rest)
	if n == 0 || n > 5 || !strings.HasPrefix(rest, "x") {
		return nil
	}
	rest = rest[1:]
	sy, n := digits(&rest)
	if n == 0 || n > 5 || !strings.HasPrefix(rest, ",") {
		return nil
	}
	rest = rest[1:]
	xoff, n := digits(&rest)
	if n == 0 || n > 5 || !strings.HasPrefix(rest, ",") {
		return nil
	}
	rest = rest[1:]
	yoff, n := digits(&rest)
	if n == 0 {
		return nil
	}
	if sx < PaneMinimum || sx > PaneMaximum || sy < PaneMinimum || sy > PaneMaximum ||
		xoff > WindowMaximum || yoff > WindowMaximum {
		return nil
	}
	// An optional pane id: digits after a comma, unless they are the next cell's width.
	if strings.HasPrefix(rest, ",") {
		saved := rest
		rest = rest[1:]
		digits(&rest)
		if strings.HasPrefix(rest, "x") {
			rest = saved
		}
	}
	*s = rest
	c := newCell(parent)
	c.setSize(uint32(sx), uint32(sy), xoff, yoff)
	return c
}

func constructV1(parent *Cell, s *string, depth int) *Cell {
	if depth > v1MaxDepth {
		return nil
	}
	c := constructCell(parent, s)
	if c == nil {
		return nil
	}
	if *s == "" {
		return c
	}
	switch (*s)[0] {
	case ',', '}', ']':
		return c
	case '{':
		c.Type = LeftRight
	case '[':
		c.Type = TopBottom
	default:
		return nil
	}
	for {
		*s = (*s)[1:]
		ch := constructV1(c, s, depth+1)
		if ch == nil {
			return nil
		}
		c.Cells = append(c.Cells, ch)
		if !strings.HasPrefix(*s, ",") {
			break
		}
	}
	end := "]"
	if c.Type == LeftRight {
		end = "}"
	}
	if !strings.HasPrefix(*s, end) {
		return nil
	}
	*s = (*s)[1:]
	return c
}

// jsonObject is a JSON object read for a layout.
type jsonObject map[string]json.RawMessage

func (o jsonObject) number(key string) (int64, error) {
	raw, ok := o[key]
	if !ok {
		return 0, fmt.Errorf("key %q not found", key)
	}
	var n json.Number
	if bytes.HasPrefix(bytes.TrimSpace(raw), []byte(`"`)) || json.Unmarshal(raw, &n) != nil {
		return 0, fmt.Errorf("key %q expected a number", key)
	}
	v, err := n.Int64()
	if err != nil {
		return 0, fmt.Errorf("key %q expected a number", key)
	}
	return v, nil
}

func (o jsonObject) str(key string) (string, error) {
	raw, ok := o[key]
	if !ok {
		return "", fmt.Errorf("key %q not found", key)
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", fmt.Errorf("key %q expected a string", key)
	}
	return s, nil
}

func (o jsonObject) boolean(key string) (bool, error) {
	var v bool
	if err := json.Unmarshal(o[key], &v); err != nil {
		return false, fmt.Errorf("key %q expected a boolean", key)
	}
	return v, nil
}

func (o jsonObject) object(key string) (jsonObject, error) {
	raw, ok := o[key]
	if !ok {
		return nil, fmt.Errorf("key %q not found", key)
	}
	var v jsonObject
	if bytes.HasPrefix(bytes.TrimSpace(raw), []byte("{")) && json.Unmarshal(raw, &v) == nil {
		return v, nil
	}
	return nil, fmt.Errorf("key %q expected an object", key)
}

func (o jsonObject) array(key string) ([]json.RawMessage, error) {
	raw, ok := o[key]
	if !ok {
		return nil, fmt.Errorf("key %q not found", key)
	}
	var v []json.RawMessage
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, fmt.Errorf("key %q expected an array", key)
	}
	return v, nil
}

func (p *Parsed) parseJSON(input string) error {
	var top jsonObject
	if !json.Valid([]byte(input)) {
		return errors.New("invalid json")
	}
	if err := json.Unmarshal([]byte(input), &top); err != nil {
		return errors.New("invalid layout json")
	}
	v, err := top.number("V")
	if err != nil {
		return err
	}
	p.Version = int(v)
	l, err := top.object("L")
	if err != nil {
		return err
	}
	root, err := p.parseCell(l, nil)
	if err != nil {
		return err
	}
	p.tree.Root = root
	return nil
}

func (p *Parsed) parseCell(o jsonObject, parent *Cell) (*Cell, error) {
	c := newCell(parent)
	t, err := o.str("t")
	if err != nil {
		return nil, err
	}
	switch t {
	case "p":
		c.Type = WindowPane
	case "v":
		c.Type = TopBottom
	case "h":
		c.Type = LeftRight
	default:
		return nil, fmt.Errorf("unknown cell type %q", t)
	}
	for _, f := range []struct {
		key, what string
		lo, hi    int64
		set       func(int64)
	}{
		{"w", "width", PaneMinimum, PaneMaximum, func(v int64) { c.Sx = uint32(v) }},
		{"h", "height", PaneMinimum, PaneMaximum, func(v int64) { c.Sy = uint32(v) }},
		{"x", "x-offset", -WindowMaximum, WindowMaximum, func(v int64) { c.Xoff = int(v) }},
		{"y", "y-offset", -WindowMaximum, WindowMaximum, func(v int64) { c.Yoff = int(v) }},
	} {
		v, err := o.number(f.key)
		if err != nil {
			return nil, err
		}
		if v < f.lo || v > f.hi {
			return nil, fmt.Errorf("invalid %s %d", f.what, v)
		}
		f.set(v)
	}
	if c.Type == WindowPane {
		if _, ok := o["c"]; ok {
			return nil, errors.New("panes cannot have children")
		}
		idx, err := o.number("i")
		if err != nil {
			return nil, err
		}
		if idx < 0 || idx > 1<<31-1 {
			return nil, fmt.Errorf("invalid index %d", idx)
		}
		cc := cellCtx{c: c, active: -1, last: -1, index: int(idx)}
		if _, ok := o["a"]; ok {
			a, err := o.boolean("a")
			if err != nil {
				return nil, err
			}
			cc.active = 0
			if a {
				cc.active = 1
			}
		} else if _, ok := o["l"]; ok {
			last, err := o.number("l")
			if err != nil {
				return nil, err
			}
			if last < 0 || last > 1<<31-1 {
				return nil, fmt.Errorf("invalid last %d", last)
			}
			cc.last = int(last)
		}
		if _, ok := o["z"]; ok {
			return nil, errors.New("floating panes are not supported by imux-core")
		}
		p.ctxs = append(p.ctxs, cc)
		return c, nil
	}
	children, err := o.array("c")
	if err != nil {
		return nil, err
	}
	if len(children) < 2 {
		return nil, errors.New("nodes must have more than one child")
	}
	for _, raw := range children {
		var co jsonObject
		if !bytes.HasPrefix(bytes.TrimSpace(raw), []byte("{")) || json.Unmarshal(raw, &co) != nil {
			return nil, errors.New("invalid layout json")
		}
		ch, err := p.parseCell(co, c)
		if err != nil {
			return nil, err
		}
		c.Cells = append(c.Cells, ch)
	}
	return c, nil
}

func (p *Parsed) checkIndexes() error {
	sort.SliceStable(p.ctxs, func(i, j int) bool { return p.ctxs[i].index < p.ctxs[j].index })
	for i := 1; i < len(p.ctxs); i++ {
		if p.ctxs[i].index == p.ctxs[i-1].index {
			return errors.New("duplicate pane index")
		}
	}
	sort.SliceStable(p.ctxs, func(i, j int) bool { return p.ctxs[i].last > p.ctxs[j].last })
	n := 0
	for n < len(p.ctxs) && p.ctxs[n].last >= 0 {
		n++
	}
	for i := 1; i < n; i++ {
		if p.ctxs[i].last == p.ctxs[i-1].last {
			return errors.New("duplicate last pane index")
		}
	}
	return nil
}

func (p *Parsed) removeCtx(c *Cell) {
	for i, cc := range p.ctxs {
		if cc.c == c {
			p.ctxs[i] = p.ctxs[len(p.ctxs)-1]
			p.ctxs = p.ctxs[:len(p.ctxs)-1]
			return
		}
	}
}

func bottomRight(c *Cell) *Cell {
	// A version 1 node may have a single child, and lose it here: tmux would then follow a
	// null pointer; this stops at the empty node.
	for c.Type != WindowPane && len(c.Cells) > 0 {
		c = c.Cells[len(c.Cells)-1]
	}
	return c
}

// check is tmux's layout_check: the children of each node fill it exactly.
func check(c *Cell) bool {
	var n uint32
	switch c.Type {
	case LeftRight:
		for _, ch := range c.Cells {
			if ch.Sy != c.Sy || !check(ch) {
				return false
			}
			n += ch.Sx + 1
		}
		if n != 0 && n-1 != c.Sx {
			return false
		}
	case TopBottom:
		for _, ch := range c.Cells {
			if ch.Sx != c.Sx || !check(ch) {
				return false
			}
			n += ch.Sy + 1
		}
		if n != 0 && n-1 != c.Sy {
			return false
		}
	}
	return true
}

// Fit drops cells from the bottom right until the layout has as many as npanes, fixes an
// old tmux's wrong top size and checks the sizes (the first half of tmux's layout_parse).
// The window is then resized to Size and the layout applied with Apply.
func (p *Parsed) Fit(npanes int) error {
	for {
		ncells := countCells(p.tree.Root)
		if npanes > ncells {
			return fmt.Errorf("have %d panes but need %d", npanes, ncells)
		}
		if npanes == ncells {
			break
		}
		c := bottomRight(p.tree.Root)
		if p.Version > 1 {
			p.removeCtx(c)
		}
		p.tree.destroyCell(c)
	}
	root := p.tree.Root
	var sx, sy uint32
	switch root.Type {
	case LeftRight:
		for _, ch := range root.Cells {
			sy = ch.Sy + 1
			sx += ch.Sx + 1
		}
	case TopBottom:
		for _, ch := range root.Cells {
			sx = ch.Sx + 1
			sy += ch.Sy + 1
		}
	}
	if root.Type != WindowPane && sx != 0 && sy != 0 && (root.Sx != sx || root.Sy != sy) {
		root.Sx, root.Sy = sx-1, sy-1
	}
	if !check(root) {
		return errors.New("size mismatch after applying layout")
	}
	return nil
}

// Size is the layout's size, which the window takes.
func (p *Parsed) Size() (sx, sy int) { return int(p.tree.Root.Sx), int(p.tree.Root.Sy) }

// Applied says what a version 2 layout set besides the cells: the active pane (or -1) and the
// last-panes list, most recent first (nil for version 1, which leaves them).
type Applied struct {
	Active int
	Last   []int
}

// Apply makes the parsed layout l's, with panes (the window's, in order) assigned to its
// cells: by index for version 2, in tree order for version 1. Fit must have succeeded.
func (l *Layout) Apply(p *Parsed, panes []int) Applied {
	l.Root = p.tree.Root
	l.leaves = map[int]*Cell{}
	l.savedRoot, l.savedLeaves, l.zoomed = nil, nil, -1
	res := Applied{Active: -1}
	if len(p.ctxs) > 0 {
		sort.SliceStable(p.ctxs, func(i, j int) bool { return p.ctxs[i].index < p.ctxs[j].index })
		for i, cc := range p.ctxs {
			l.makeLeaf(cc.c, panes[i])
		}
		for _, cc := range p.ctxs {
			if cc.active == 1 {
				res.Active = cc.c.Pane
				break
			}
		}
		sort.SliceStable(p.ctxs, func(i, j int) bool { return p.ctxs[i].last > p.ctxs[j].last })
		res.Last = []int{}
		for _, cc := range p.ctxs {
			if cc.last < 0 || cc.active == 1 {
				continue
			}
			// tmux pushes each on the head of the list: the smallest "l" ends up first.
			res.Last = append([]int{cc.c.Pane}, res.Last...)
		}
	} else {
		i := 0
		var walk func(c *Cell)
		walk = func(c *Cell) {
			if c.Type == WindowPane {
				if i < len(panes) {
					l.makeLeaf(c, panes[i])
					i++
				}
				return
			}
			for _, ch := range c.Cells {
				walk(ch)
			}
		}
		walk(l.Root)
	}
	l.fixOffsets()
	return res
}
