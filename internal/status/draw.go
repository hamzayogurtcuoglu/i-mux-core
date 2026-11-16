// Ported from tmux 3.8's format-draw.c:
//
// Copyright (c) 2019 Nicholas Marriott <nicholas.marriott@gmail.com>
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

package status

import (
	"unicode/utf8"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/format"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/style"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/vt"
)

// The parts format_draw splits an expanded format into, one screen each.
const (
	partLeft = iota
	partCentre
	partRight
	partAbsoluteCentre
	partList
	partListLeft
	partListRight
	partAfter
	partTotal
)

// part is one of format_draw's one-line screens: the cells written so far (its cursor is
// len(cells)) and the colour of the cells it was cleared to.
type part struct {
	cells []vt.Cell
	size  int
	bg    vt.Color
}

// cell is the cell at x: one written, or one the screen was cleared to.
func (p *part) cell(x int) vt.Cell {
	if x < len(p.cells) {
		return p.cells[x]
	}
	gc := vt.DefaultCell
	gc.Bg = p.bg
	return gc
}

// put is screen_write_cell for a one-line screen: a wide character is followed by padding and
// a zero-width one joins the character before it.
func (p *part) put(gc vt.Cell) {
	if gc.Width == 0 {
		for i := len(p.cells) - 1; i >= 0; i-- {
			if p.cells[i].Flags&vt.FlagPadding == 0 {
				p.cells[i].Data += gc.Data
				return
			}
		}
		return
	}
	p.cells = append(p.cells, gc)
	for range int(gc.Width) - 1 {
		pad := gc
		pad.Data, pad.Width, pad.Flags = "!", 0, gc.Flags|vt.FlagPadding
		p.cells = append(p.cells, pad)
	}
}

// fRange is format_range: a range being collected, in the screen of one part.
type fRange struct {
	part       int
	start, end int
	typ        style.RangeType
	arg        uint32
	str        string
}

// isType is format_is_type: whether the range is the one sy names.
func (fr *fRange) isType(sy *style.Style) bool {
	if fr.typ != sy.RangeType {
		return false
	}
	switch fr.typ {
	case style.RangePane, style.RangeWindow, style.RangeSession:
		return fr.arg == sy.RangeArg
	case style.RangeUser:
		return fr.str == sy.RangeString
	}
	return true
}

// drawer holds format_draw's state while it lays the parts out on the target line.
type drawer struct {
	out    []vt.Cell // the target line, from the cursor (ocx) on
	parts  [partTotal]part
	ranges []*fRange
}

// updateRanges is format_update_ranges: the ranges in part p, after its cells from start for
// width have been put at offset.
func (d *drawer) updateRanges(p, offset, start, width int) {
	kept := d.ranges[:0]
	for _, fr := range d.ranges {
		if fr.part != p {
			kept = append(kept, fr)
			continue
		}
		if fr.end <= start || fr.start >= start+width {
			continue
		}
		fr.start = max(fr.start, start)
		fr.end = min(fr.end, start+width)
		if fr.start == fr.end {
			continue
		}
		fr.start += offset - start
		fr.end += offset - start
		kept = append(kept, fr)
	}
	d.ranges = kept
}

// fastCopy is screen_write_fast_copy for one line: width cells of src from start, at x.
func (d *drawer) fastCopy(src *part, x, start, width int) {
	for xx := start; xx < start+width; xx, x = xx+1, x+1 {
		gc := src.cell(xx)
		if xx+int(gc.Width) > start+width {
			break
		}
		if x < 0 {
			continue
		}
		if x >= len(d.out) {
			break
		}
		d.out[x] = gc
	}
}

// put is format_draw_put: width cells of part p from start, at offset.
func (d *drawer) put(p, offset, start, width int) {
	d.fastCopy(&d.parts[p], offset, start, width)
	d.updateRanges(p, offset, start, width)
}

// appendPart copies width cells of part src to the end of part dst (screen_write_fast_copy
// into dst's screen at its cursor).
func (d *drawer) appendPart(dst, src, width int) {
	s := &d.parts[src]
	for xx := 0; xx < width; xx++ {
		gc := s.cell(xx)
		if xx+int(gc.Width) > width {
			break
		}
		d.parts[dst].cells = append(d.parts[dst].cells, gc)
	}
}

// putList is format_draw_put_list: the list at offset in width cells, trimmed around the
// focus with the markers at the sides it was trimmed from.
func (d *drawer) putList(offset, width, focusStart, focusEnd int) {
	list := len(d.parts[partList].cells)
	if width >= list {
		d.put(partList, offset, 0, width)
		return
	}

	focusCentre := focusStart + (focusEnd-focusStart)/2
	start := 0
	if focusCentre >= width/2 {
		start = focusCentre - width/2
	}
	if start+width > list {
		start = list - width
	}

	left, right := len(d.parts[partListLeft].cells), len(d.parts[partListRight].cells)
	if start != 0 && width > left {
		d.fastCopy(&d.parts[partListLeft], offset, 0, left)
		offset += left
		start += left
		width -= left
	}
	if start+width < list && width > right {
		d.fastCopy(&d.parts[partListRight], offset+width-right, 0, right)
		width -= right
	}

	d.put(partList, offset, start, width)
}

// trim takes one cell at a time from the widths, from the first in order that has any.
func trim(available int, total func() int, order ...*int) {
	for total() > available {
		for i, w := range order {
			if *w > 0 || i == len(order)-1 {
				*w--
				break
			}
		}
	}
}

// drawNone is format_draw_none: no list.
func (d *drawer) drawNone(available int) {
	wl, wc := len(d.parts[partLeft].cells), len(d.parts[partCentre].cells)
	wr, wa := len(d.parts[partRight].cells), len(d.parts[partAbsoluteCentre].cells)

	// Keep as much of the left and right as possible at the expense of the centre.
	trim(available, func() int { return wl + wc + wr }, &wc, &wr, &wl)

	d.put(partLeft, 0, 0, wl)
	d.put(partRight, available-wr, len(d.parts[partRight].cells)-wr, wr)
	d.put(partCentre, wl+((available-wr)-wl)/2-wc/2, len(d.parts[partCentre].cells)/2-wc/2, wc)

	wa = min(wa, available)
	d.put(partAbsoluteCentre, (available-wa)/2, 0, wa)
}

// drawLeft is format_draw_left: the list is part of the left.
func (d *drawer) drawLeft(available, focusStart, focusEnd int) {
	wl, wc := len(d.parts[partLeft].cells), len(d.parts[partCentre].cells)
	wr, wa := len(d.parts[partRight].cells), len(d.parts[partAbsoluteCentre].cells)
	wlist, wafter := len(d.parts[partList].cells), len(d.parts[partAfter].cells)

	// Trim first the centre, then the list, then the right, then after the list, then the left.
	trim(available, func() int { return wl + wc + wr + wlist + wafter }, &wc, &wlist, &wr, &wafter, &wl)

	if wlist == 0 {
		d.appendPart(partLeft, partAfter, wafter)
		d.drawNone(available)
		return
	}

	d.put(partLeft, 0, 0, wl)
	d.put(partRight, available-wr, len(d.parts[partRight].cells)-wr, wr)
	d.put(partAfter, wl+wlist, 0, wafter)
	d.put(partCentre, (wl+wlist+wafter)+((available-wr)-(wl+wlist+wafter))/2-wc/2,
		len(d.parts[partCentre].cells)/2-wc/2, wc)

	// If there is no focus given, keep the left in focus.
	if focusStart == -1 || focusEnd == -1 {
		focusStart, focusEnd = 0, 0
	}
	d.putList(wl, wlist, focusStart, focusEnd)

	wa = min(wa, available)
	d.put(partAbsoluteCentre, (available-wa)/2, 0, wa)
}

// drawCentre is format_draw_centre: the list is part of the centre.
func (d *drawer) drawCentre(available, focusStart, focusEnd int) {
	wl, wc := len(d.parts[partLeft].cells), len(d.parts[partCentre].cells)
	wr, wa := len(d.parts[partRight].cells), len(d.parts[partAbsoluteCentre].cells)
	wlist, wafter := len(d.parts[partList].cells), len(d.parts[partAfter].cells)

	// Trim first the list, then after the list, then the centre, then the right, then the left.
	trim(available, func() int { return wl + wc + wr + wlist + wafter }, &wlist, &wafter, &wc, &wr, &wl)

	if wlist == 0 {
		d.appendPart(partCentre, partAfter, wafter)
		d.drawNone(available)
		return
	}

	d.put(partLeft, 0, 0, wl)
	d.put(partRight, available-wr, len(d.parts[partRight].cells)-wr, wr)

	// All three centre parts are placed from the middle of the available space.
	middle := wl + ((available-wr)-wl)/2
	d.put(partCentre, middle-wlist/2-wc, 0, wc)
	d.put(partAfter, middle-wlist/2+wlist, 0, wafter)

	// If there is no focus given, keep the centre in focus.
	if focusStart == -1 || focusEnd == -1 {
		focusStart = len(d.parts[partList].cells) / 2
		focusEnd = focusStart
	}
	d.putList(middle-wlist/2, wlist, focusStart, focusEnd)

	wa = min(wa, available)
	d.put(partAbsoluteCentre, (available-wa)/2, 0, wa)
}

// drawRight is format_draw_right: the list is part of the right.
func (d *drawer) drawRight(available, focusStart, focusEnd int) {
	wl, wc := len(d.parts[partLeft].cells), len(d.parts[partCentre].cells)
	wr, wa := len(d.parts[partRight].cells), len(d.parts[partAbsoluteCentre].cells)
	wlist, wafter := len(d.parts[partList].cells), len(d.parts[partAfter].cells)

	// Trim first the centre, then the list, then the right, then after the list, then the left.
	trim(available, func() int { return wl + wc + wr + wlist + wafter }, &wc, &wlist, &wr, &wafter, &wl)

	if wlist == 0 {
		d.appendPart(partRight, partAfter, wafter)
		d.drawNone(available)
		return
	}

	d.put(partLeft, 0, 0, wl)
	d.put(partAfter, available-wafter, len(d.parts[partAfter].cells)-wafter, wafter)
	d.put(partRight, available-wr-wlist-wafter, 0, wr)
	d.put(partCentre, wl+((available-wr-wlist-wafter)-wl)/2-wc/2, len(d.parts[partCentre].cells)/2-wc/2, wc)

	// If there is no focus given, keep the right in focus.
	if focusStart == -1 || focusEnd == -1 {
		focusStart, focusEnd = 0, 0
	}
	d.putList(available-wlist-wafter, wlist, focusStart, focusEnd)

	wa = min(wa, available)
	d.put(partAbsoluteCentre, (available-wa)/2, 0, wa)
}

// drawAbsoluteCentre is format_draw_absolute_centre: the list is in the centre of the whole
// line.
func (d *drawer) drawAbsoluteCentre(available, focusStart, focusEnd int) {
	wl, wc := len(d.parts[partLeft].cells), len(d.parts[partCentre].cells)
	wr, wa := len(d.parts[partRight].cells), len(d.parts[partAbsoluteCentre].cells)
	wlist, wafter := len(d.parts[partList].cells), len(d.parts[partAfter].cells)

	// Trim first the centre, then the right, then the left.
	trim(available, func() int { return wl + wc + wr }, &wc, &wr, &wl)
	// The list, after it and abs_centre are drawn over the rest, so they are trimmed on their
	// own: first the list, then after the list, then abs_centre.
	trim(available, func() int { return wlist + wafter + wa }, &wlist, &wafter, &wa)

	d.put(partLeft, 0, 0, wl)
	d.put(partRight, available-wr, len(d.parts[partRight].cells)-wr, wr)

	// The centre stays at the relative centre; only the list goes in the absolute centre.
	middle := wl + ((available-wr)-wl)/2
	d.put(partCentre, middle-wc, 0, wc)

	// If there is no focus given, keep the centre in focus.
	if focusStart == -1 || focusEnd == -1 {
		focusStart = len(d.parts[partList].cells) / 2
		focusEnd = focusStart
	}

	// abs_centre and the list share the middle of the line.
	off := (available - wlist - wa) / 2
	d.put(partAbsoluteCentre, off, 0, wa)
	off += wa
	d.putList(off, wlist, focusStart, focusEnd)
	off += wlist
	d.put(partAfter, off, 0, wafter)
}

// drawFormat is format_draw: it draws expanded (a format already expanded, with its #[…]
// styles) on out, available cells wide, over base. ranges, when not nil, gets the line's
// ranges. With defaultColours, the styles' colours are replaced by base's.
//
// Hyperlinks (link= styles) are not kept: the status line has no hyperlink table yet.
func drawFormat(out []vt.Cell, base vt.Cell, available int, expanded string, ranges *[]Range, defaultColours bool) {
	available = min(available, len(out))
	baseDefault, currentDefault := base, base
	sy := style.FromCell(currentDefault)
	d := &drawer{out: out}
	for i := range d.parts {
		d.parts[i] = part{size: len(expanded), bg: currentDefault.Bg}
	}

	current := partLeft
	focusStart, focusEnd := -1, -1
	listState, fill := -1, vt.Color(-1)
	listAlign := style.AlignDefault
	alignMap := [...]int{partLeft, partLeft, partCentre, partRight, partAbsoluteCentre}
	var fr *fRange

	many := func(ch string, n int) {
		gc := sy.Cell
		gc.Data, gc.Width = ch, 1
		for range n {
			d.parts[current].put(gc)
		}
	}

	cp := expanded
	for cp != "" {
		// Sequences of #.
		if cp[0] == '#' && len(cp) > 1 && cp[1] != '[' {
			n := 1
			for n < len(cp) && cp[n] == '#' {
				n++
			}
			even := n%2 == 0
			if n >= len(cp) || cp[n] != '[' {
				cp = cp[n:]
				if even {
					n /= 2
				} else {
					n = n/2 + 1
				}
				many("#", n)
				continue
			}
			if even {
				cp = cp[n+1:]
			} else {
				cp = cp[n-1:]
			}
			if sy.Ignore {
				continue
			}
			many("#", n/2)
			if even {
				many("[", 1)
			}
			continue
		}

		// Not a style: a character.
		if cp[0] != '#' || len(cp) < 2 || cp[1] != '[' || sy.Ignore {
			r, size := utf8.DecodeRuneInString(cp)
			w := 0
			if r >= 0x80 && size > 1 {
				w = vt.RuneWidth(r)
			}
			if r < 0x80 || size <= 1 || w < 0 {
				// Not a UTF-8 character: ASCII or not valid, and only printable ASCII is drawn.
				c := cp[0]
				cp = cp[1:]
				if c < 0x20 || c > 0x7e {
					continue
				}
				r, w = rune(c), 1
			} else {
				cp = cp[size:]
			}
			gc := sy.Cell
			gc.Data, gc.Width = string(r), uint8(w)
			d.parts[current].put(gc)
			continue
		}

		// A style: find where it ends and parse it.
		end := format.Skip(cp[2:], "]")
		if end < 0 {
			d.ranges = nil
			return
		}
		saved := sy
		if sy.Parse(currentDefault, cp[2:2+end]) != nil {
			cp = cp[2+end+1:]
			continue
		}
		cp = cp[2+end+1:]
		if defaultColours {
			sy.Cell.Bg, sy.Cell.Fg = base.Bg, base.Fg
		}
		sy.Cell.Link = 0

		// A style with a fill colour: kept for later.
		if sy.Fill != 8 {
			fill = sy.Fill
		}

		// A style that pushes or pops the default.
		switch sy.Default {
		case style.DefaultPush:
			currentDefault = saved.Cell
			sy.Default = style.DefaultBase
		case style.DefaultPop:
			currentDefault = baseDefault
			sy.Default = style.DefaultBase
		case style.DefaultSet:
			baseDefault, currentDefault = saved.Cell, saved.Cell
			sy.Default = style.DefaultBase
		}

		// The list state.
		switch sy.List {
		case style.ListOn:
			// Entering the list, leaving a marker, or leaving the focus.
			if listState != 0 {
				fr = nil
				listState = 0
				listAlign = sy.Align
			}
			if focusStart != -1 && focusEnd == -1 {
				focusEnd = len(d.parts[partList].cells)
			}
			current = partList
		case style.ListFocus:
			// Entering the focus.
			if listState == 0 && focusStart == -1 {
				focusStart = len(d.parts[partList].cells)
			}
		case style.ListOff:
			// Leaving or outside the list.
			if listState == 0 {
				fr = nil
				if focusStart != -1 && focusEnd == -1 {
					focusEnd = len(d.parts[partList].cells)
				}
				alignMap[listAlign] = partAfter
				if listAlign == style.AlignLeft {
					alignMap[style.AlignDefault] = partAfter
				}
				listState = 1
			}
			current = alignMap[sy.Align]
		case style.ListLeftMarker, style.ListRightMarker:
			// Entering a marker.
			p := partListLeft
			if sy.List == style.ListRightMarker {
				p = partListRight
			}
			if listState != 0 || len(d.parts[p].cells) != 0 {
				break
			}
			fr = nil
			if focusStart != -1 && focusEnd == -1 {
				focusStart, focusEnd = -1, -1
			}
			current = p
		}

		// A change of range ends the current one and may start another.
		if ranges != nil {
			if fr != nil && !fr.isType(&sy) {
				if x := len(d.parts[current].cells); x != fr.start {
					fr.end = x
					d.ranges = append(d.ranges, fr)
				}
				fr = nil
			}
			if fr == nil && sy.RangeType != style.RangeNone {
				fr = &fRange{
					part:  current,
					start: len(d.parts[current].cells),
					typ:   sy.RangeType,
					arg:   sy.RangeArg,
					str:   sy.RangeString,
				}
			}
		}
	}

	// Clear the available area to the fill colour.
	if fill != -1 {
		gc := vt.DefaultCell
		gc.Bg = fill
		for i := range available {
			out[i] = gc
		}
	}

	// Draw the parts; how they are arranged depends on where the list is.
	switch listAlign {
	case style.AlignDefault:
		d.drawNone(available)
	case style.AlignLeft:
		d.drawLeft(available, focusStart, focusEnd)
	case style.AlignCentre:
		d.drawCentre(available, focusStart, focusEnd)
	case style.AlignRight:
		d.drawRight(available, focusStart, focusEnd)
	case style.AlignAbsoluteCentre:
		d.drawAbsoluteCentre(available, focusStart, focusEnd)
	}

	if ranges != nil {
		for _, fr := range d.ranges {
			*ranges = append(*ranges, Range{Type: fr.typ, Arg: fr.arg, Str: fr.str, Start: fr.start, End: fr.end})
		}
	}
}
