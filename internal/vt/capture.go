// Copyright (c) 2008 Nicholas Marriott <nicholas.marriott@gmail.com>
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

package vt

import (
	"strconv"
	"strings"
)

// Turning cells into text, ported from tmux 3.8's grid_string_cells and cmd-capture-pane.c.

// String flags (tmux's GRID_STRING_*).
const (
	StringWithSequences   = 0x1
	StringEscapeSequences = 0x2
	StringTrimSpaces      = 0x4
	StringUsedOnly        = 0x8
	StringEmptyCells      = 0x10
)

// HyperlinkLookup returns a hyperlink's URI and id; ok is false for none.
type HyperlinkLookup func(link uint32) (uri, id string, ok bool)

func colourFg(c Color, v []int) []int {
	switch {
	case c&ColorFlagTheme != 0:
		return append(v, 39) // themes are not supported: the default
	case c&ColorFlag256 != 0:
		return append(v, 38, 5, int(c&0xff))
	case c&ColorFlagRGB != 0:
		return append(v, 38, 2, int(c>>16&0xff), int(c>>8&0xff), int(c&0xff))
	}
	switch {
	case c >= 0 && c <= 7:
		return append(v, int(c)+30)
	case c == 8:
		return append(v, 39)
	case c >= 90 && c <= 97:
		return append(v, int(c))
	}
	return v
}

func colourBg(c Color, v []int) []int {
	switch {
	case c&ColorFlagTheme != 0:
		return append(v, 49)
	case c&ColorFlag256 != 0:
		return append(v, 48, 5, int(c&0xff))
	case c&ColorFlagRGB != 0:
		return append(v, 48, 2, int(c>>16&0xff), int(c>>8&0xff), int(c&0xff))
	}
	switch {
	case c >= 0 && c <= 7:
		return append(v, int(c)+40)
	case c == 8:
		return append(v, 49)
	case c >= 90 && c <= 97:
		return append(v, int(c)+10)
	}
	return v
}

func colourUs(c Color, v []int) []int {
	switch {
	case c&ColorFlagTheme != 0:
		return append(v, 59)
	case c&ColorFlag256 != 0:
		return append(v, 58, 5, int(c&0xff))
	case c&ColorFlagRGB != 0:
		return append(v, 58, 2, int(c>>16&0xff), int(c>>8&0xff), int(c&0xff))
	}
	return v
}

func escapeStart(flags int) string {
	if flags&StringEscapeSequences != 0 {
		return "\\033["
	}
	return "\x1b["
}

func addCode(b *strings.Builder, n int, s0 int, newc, oldc []int, flags int) {
	if len(newc) == 0 {
		return
	}
	reset := n != 0 && s0 == 0
	if !reset && equalInts(newc, oldc) {
		return
	}
	if reset && (newc[0] == 49 || newc[0] == 39) {
		return
	}
	b.WriteString(escapeStart(flags))
	for i, v := range newc {
		b.WriteString(strconv.Itoa(v))
		if i+1 < len(newc) {
			b.WriteByte(';')
		}
	}
	b.WriteByte('m')
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func addHyperlink(b *strings.Builder, id, uri string, flags int) {
	if flags&StringEscapeSequences != 0 {
		b.WriteString("\\033]8;")
	} else {
		b.WriteString("\x1b]8;")
	}
	if id != "" {
		b.WriteString("id=" + id + ";")
	} else {
		b.WriteString(";")
	}
	b.WriteString(uri)
	if flags&StringEscapeSequences != 0 {
		b.WriteString("\\033\\\\")
	} else {
		b.WriteString("\x1b\\")
	}
}

var sgrAttrs = []struct {
	mask uint16
	code int
}{
	{AttrBright, 1},
	{AttrDim, 2},
	{AttrItalics, 3},
	{AttrUnderscore, 4},
	{AttrBlink, 5},
	{AttrReverse, 7},
	{AttrHidden, 8},
	{AttrStrikethrough, 9},
	{AttrUnderscore2, 42},
	{AttrUnderscore3, 43},
	{AttrUnderscore4, 44},
	{AttrUnderscore5, 45},
	{AttrOverline, 53},
}

// cellsCode writes the sequences that take the style from last to gc.
func cellsCode(b *strings.Builder, last, gc *Cell, flags int, links HyperlinkLookup, hasLink *bool) {
	attr, lastAttr := gc.Attr, last.Attr
	var s []int
	for _, a := range sgrAttrs {
		if (attr&a.mask == 0 && lastAttr&a.mask != 0) || (last.Us != 8 && gc.Us == 8) {
			s = append(s, 0)
			lastAttr &= AttrCharset
			break
		}
	}
	for _, a := range sgrAttrs {
		if attr&a.mask != 0 && lastAttr&a.mask == 0 {
			s = append(s, a.code)
		}
	}
	if len(s) > 0 {
		b.WriteString(escapeStart(flags))
		for i, v := range s {
			if v < 10 {
				b.WriteString(strconv.Itoa(v))
			} else {
				b.WriteString(strconv.Itoa(v/10) + ":" + strconv.Itoa(v%10))
			}
			if i+1 < len(s) {
				b.WriteByte(';')
			}
		}
		b.WriteByte('m')
	}
	s0 := -1
	if len(s) > 0 {
		s0 = s[0]
	}
	var nv, ov [8]int
	addCode(b, len(s), s0, colourFg(gc.Fg, nv[:0]), colourFg(last.Fg, ov[:0]), flags)
	addCode(b, len(s), s0, colourBg(gc.Bg, nv[:0]), colourBg(last.Bg, ov[:0]), flags)
	addCode(b, len(s), s0, colourUs(gc.Us, nv[:0]), colourUs(last.Us, ov[:0]), flags)
	if attr&AttrCharset != 0 && lastAttr&AttrCharset == 0 {
		if flags&StringEscapeSequences != 0 {
			b.WriteString("\\016")
		} else {
			b.WriteString("\x0e")
		}
	}
	if attr&AttrCharset == 0 && lastAttr&AttrCharset != 0 {
		if flags&StringEscapeSequences != 0 {
			b.WriteString("\\017")
		} else {
			b.WriteString("\x0f")
		}
	}
	if links != nil && last.Link != gc.Link {
		if uri, id, ok := links(gc.Link); ok {
			addHyperlink(b, id, uri, flags)
			*hasLink = true
		} else if *hasLink {
			addHyperlink(b, "", "", flags)
			*hasLink = false
		}
	}
}

// StringCells returns nx cells of line py from px as text, as tmux's grid_string_cells does.
// last carries the style between calls when flags has StringWithSequences.
func (gd *Grid) StringCells(px, py, nx int, last *Cell, flags int, links HyperlinkLookup) string {
	gl := gd.PeekLine(py)
	if gl == nil {
		return ""
	}
	end := gl.used
	if flags&StringEmptyCells != 0 {
		end = len(gl.cells)
	}
	var b strings.Builder
	hasLink := false
	for xx := px; xx < px+nx; xx++ {
		if xx >= end {
			break
		}
		gc := gd.Cell(xx, py)
		if gc.Flags&FlagPadding != 0 {
			continue
		}
		if last != nil && flags&StringWithSequences != 0 {
			cellsCode(&b, last, &gc, flags, links, &hasLink)
			*last = gc
		}
		switch {
		case gc.Flags&FlagTab != 0:
			b.WriteByte('\t')
		case flags&StringEscapeSequences != 0 && gc.Data == "\\":
			b.WriteString("\\\\")
		default:
			b.WriteString(gc.Data)
		}
	}
	if hasLink {
		addHyperlink(&b, "", "", flags)
	}
	out := b.String()
	if flags&StringTrimSpaces != 0 {
		out = strings.TrimRight(out, " ")
	}
	return out
}

// CaptureOptions are capture-pane's flags.
type CaptureOptions struct {
	Start, End    *int // -S and -E (lines relative to the screen's top); nil: the screen
	StartAll      bool // -S -
	EndAll        bool // -E -
	Join          bool // -J
	Sequences     bool // -e
	EscapeC       bool // -C
	KeepTrailing  bool // -N
	TrailingCells bool // -T: not every cell, only those used
	Alternate     bool // -a: the saved main screen while the alternate one is on
	Hyperlinks    HyperlinkLookup
}

// Capture returns a screen's text as tmux's capture-pane -p does.
func (s *Screen) Capture(o CaptureOptions) string {
	gd := s.Grid
	if o.Alternate {
		if s.savedGrid == nil {
			return ""
		}
		gd = s.savedGrid
	}
	sx := s.SizeX()
	top := gd.HSize
	switch {
	case o.StartAll:
		top = 0
	case o.Start != nil:
		n := *o.Start
		if n < 0 && -n > gd.HSize {
			top = 0
		} else {
			top = gd.HSize + n
		}
		top = min(top, gd.HSize+gd.Sy-1)
	}
	bottom := gd.HSize + gd.Sy - 1
	switch {
	case o.EndAll:
	case o.End != nil:
		n := *o.End
		if n < 0 && -n > gd.HSize {
			bottom = 0
		} else {
			bottom = gd.HSize + n
		}
		bottom = min(bottom, gd.HSize+gd.Sy-1)
	}
	if bottom < top {
		top, bottom = bottom, top
	}
	flags := 0
	if o.Sequences {
		flags |= StringWithSequences
	}
	if o.EscapeC {
		flags |= StringEscapeSequences
	}
	if !o.Join && !o.TrailingCells {
		flags |= StringEmptyCells
	}
	if !o.Join && !o.KeepTrailing {
		flags |= StringTrimSpaces
	}
	links := o.Hyperlinks
	if links == nil && s.Hyperlinks != nil {
		links = s.Hyperlinks.Get
	}
	last := DefaultCell
	var b strings.Builder
	for i := top; i <= bottom; i++ {
		b.WriteString(gd.StringCells(0, i, sx, &last, flags, links))
		if !o.Join || gd.lines[i].Flags&LineWrapped == 0 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}
