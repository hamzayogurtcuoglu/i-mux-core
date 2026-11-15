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

// Writing to the screen, ported from tmux 3.8's screen-write.c. Only the effect on the grid and
// cursor is kept: drawing on terminals is the renderer's job (internal/render), which compares
// whole frames instead of following tmux's tty commands.

// setCursor moves the cursor; x may be one past the last column (a pending wrap).
func (s *Screen) setCursor(cx, cy int) {
	if cx != -1 {
		if cx > s.SizeX() {
			cx = s.SizeX() - 1
		}
		s.Cx = cx
	}
	if cy != -1 {
		if cy > s.SizeY()-1 {
			cy = s.SizeY() - 1
		}
		s.Cy = cy
	}
}

// clearCell turns a cell that is being broken up into a blank keeping its background.
func (s *Screen) clearCell(px, py int) {
	gc := s.Grid.ViewCell(px, py)
	bg := gc.Bg
	gc = DefaultCell
	gc.Bg = bg
	s.Grid.ViewSetCell(px, py, &gc)
}

// writeASCII writes printable ASCII in one style, as tmux's screen_write_collect_add and
// screen_write_collect_end do: a line's worth at a time, the cursor moving at the end, so that
// it stops one past the last column until the next character wraps.
func (s *Screen) writeASCII(gc *Cell, run []byte) {
	collect := gc.Attr&AttrCharset == 0 && s.Mode&ModeWrap != 0 && s.Mode&ModeInsert == 0
	if !collect {
		c := *gc
		for _, b := range run {
			c.Data, c.Width = asciiString(b), 1
			s.writeCell(&c)
		}
		return
	}
	sx := s.SizeX()
	for len(run) > 0 {
		if s.Cx > sx-1 {
			s.linefeed(true, 8)
			s.setCursor(0, -1)
		}
		n := min(len(run), sx-s.Cx)
		s.collectEnd(gc, run[:n])
		run = run[n:]
	}
}

// collectEnd stores collected ASCII at the cursor, erasing what is left of any wide character
// the run cuts into on either side.
func (s *Screen) collectEnd(gc *Cell, data []byte) {
	gd := s.Grid
	if s.Cx != 0 {
		xx := s.Cx
		var c Cell
		for ; xx > 0; xx-- {
			c = gd.ViewCell(xx, s.Cy)
			if c.Flags&FlagPadding == 0 {
				break
			}
			s.clearCell(xx, s.Cy)
		}
		if xx != s.Cx {
			if xx == 0 {
				c = gd.ViewCell(0, s.Cy)
			}
			if c.Width > 1 || c.Flags&FlagPadding != 0 {
				s.clearCell(xx, s.Cy)
			}
		}
	}
	gd.ViewSetCells(s.Cx, s.Cy, gc, data)
	s.setCursor(s.Cx+len(data), -1)
	for xx := s.Cx; xx < s.SizeX(); xx++ {
		if c := gd.ViewCell(xx, s.Cy); c.Flags&FlagPadding == 0 {
			break
		}
		s.clearCell(xx, s.Cy)
	}
}

// writeCell writes one cell at the cursor, as tmux's screen_write_cell does: combining zero
// width characters, wrapping, padding wide characters and breaking up what it overwrites.
func (s *Screen) writeCell(gc *Cell) {
	gd := s.Grid
	sx, sy := s.SizeX(), s.SizeY()
	width := int(gc.Width)

	if gc.Flags&FlagPadding != 0 {
		return
	}
	if s.combine(gc) {
		return
	}
	// A wide character that cannot fit without wrapping is dropped.
	if s.Mode&ModeWrap == 0 && width > 1 && (width > sx || (s.Cx != sx && s.Cx > sx-width)) {
		return
	}
	skip := true
	if s.Mode&ModeInsert != 0 {
		gd.ViewInsertCells(s.Cx, s.Cy, width, 8)
		skip = false
	}
	if s.Mode&ModeWrap != 0 && s.Cx > sx-width {
		s.linefeed(true, 8)
		s.setCursor(0, -1)
	}
	if s.Cx > sx-width || s.Cy > sy-1 {
		return
	}
	gl := gd.ViewLine(s.Cy)
	if gl.Flags&LineExtended != 0 {
		now := gd.ViewCell(s.Cx, s.Cy)
		if s.overwrite(&now, width) {
			skip = false
		}
	}
	for xx := s.Cx + 1; xx < s.Cx+width; xx++ {
		gd.ViewSetPadding(xx, s.Cy, gc.Bg)
		skip = false
	}
	// tmux does not store a cell that would not change what is there: past the line's end, a
	// blank in the default style leaves the line short.
	if skip {
		if s.Cx >= len(gl.cells) {
			skip = CellsEqual(gc, &DefaultCell)
		} else {
			gce := &gl.cells[s.Cx]
			switch {
			case gce.flags&FlagExtended != 0,
				gc.Flags != gce.flags,
				gc.Attr != uint16(gce.attr),
				gc.Fg != Color(gce.fg),
				gc.Bg != Color(gce.bg),
				gc.Width != 1,
				len(gc.Data) != 1,
				uint32(gc.Data[0]) != gce.data:
				skip = false
			}
		}
	}
	if !skip {
		gd.ViewSetCell(s.Cx, s.Cy, gc)
	}
	notWrap := 0
	if s.Mode&ModeWrap == 0 {
		notWrap = 1
	}
	if s.Cx <= sx-notWrap-width {
		s.setCursor(s.Cx+width, -1)
	} else {
		s.setCursor(sx-notWrap, -1)
	}
}

// combine joins a zero width character, a variation selector, a joiner, a skin tone, a second
// regional indicator or a Hangul jamo onto the cell before the cursor, as tmux's
// screen_write_combine does. It reports whether gc was dealt with (joined or dropped).
func (s *Screen) combine(gc *Cell) bool {
	gd := s.Grid
	cx, cy := s.Cx, s.Cy
	if isHangulFiller(gc.Data) {
		return true
	}
	zeroWidth, forceWide := false, false
	switch {
	case isZWJ(gc.Data):
		zeroWidth = true
	case isVS(gc.Data):
		zeroWidth = true
		forceWide = !s.VS16Narrow // tmux's variation-selector-always-wide, on by default
	case gc.Width == 0:
		zeroWidth = true
	}
	if len(gc.Data) < 2 || cx == 0 {
		return zeroWidth
	}
	n := 1
	last := gd.ViewCell(cx-n, cy)
	if cx != 1 && last.Flags&FlagPadding != 0 {
		n = 2
		last = gd.ViewCell(cx-n, cy)
	}
	if n != int(last.Width) || last.Flags&FlagPadding != 0 {
		return zeroWidth
	}
	if !zeroWidth {
		switch jamoCheck(last.Data, gc.Data) {
		case jamoStateNotComposable:
			return true
		case jamoStateChoseong:
			return false
		case jamoStateComposable:
		case jamoStateNotJamo:
			switch {
			case shouldCombine(last.Data, gc.Data), shouldCombine(gc.Data, last.Data):
				forceWide = true
			case !hasZWJ(last.Data):
				return false
			}
		}
	}
	if len(last.Data)+len(gc.Data) > maxCellDataLength {
		return zeroWidth
	}
	last.Data += gc.Data
	if last.Width == 1 && forceWide {
		last.Width = 2
		n = 2
		cx++
	} else {
		forceWide = false
	}
	gd.ViewSetCell(cx-n, cy, &last)
	if forceWide {
		gd.ViewSetPadding(cx-1, cy, last.Bg)
	}
	s.setCursor(cx, cy)
	return true
}

// overwrite breaks up any wide character the new one of the given width overwrites part of.
func (s *Screen) overwrite(gc *Cell, width int) bool {
	gd := s.Grid
	done := false
	if gc.Flags&FlagPadding != 0 {
		// Clear back to the character this padding belongs to; the cursor's cell is written
		// anyway.
		xx := s.Cx + 1
		for xx--; xx > 0; xx-- {
			c := gd.ViewCell(xx, s.Cy)
			if c.Flags&FlagPadding == 0 {
				break
			}
			s.clearCell(xx, s.Cy)
		}
		s.clearCell(xx, s.Cy)
		done = true
	}
	if width != 1 || gc.Width != 1 || gc.Flags&FlagPadding != 0 {
		for xx := s.Cx + width; xx < s.SizeX(); xx++ {
			c := gd.ViewCell(xx, s.Cy)
			if c.Flags&FlagPadding == 0 {
				break
			}
			s.clearCell(xx, s.Cy)
			done = true
		}
	}
	return done
}

// linefeed moves down a line, scrolling the region at its bottom; wrapped marks the line as
// continuing on the next.
func (s *Screen) linefeed(wrapped bool, bg Color) {
	gd := s.Grid
	if wrapped {
		gd.ViewLine(s.Cy).Flags |= LineWrapped
	}
	if s.Cy != s.Rlower {
		if s.Cy < s.SizeY()-1 {
			s.setCursor(-1, s.Cy+1)
		}
		return
	}
	gd.ViewScrollRegionUp(s.Rupper, s.Rlower, bg)
}

func (s *Screen) carriageReturn() { s.setCursor(0, -1) }

func (s *Screen) backspace() {
	cx, cy := s.Cx, s.Cy
	if cx == 0 {
		if cy == 0 {
			return
		}
		if s.Grid.ViewLine(cy-1).Flags&LineWrapped != 0 {
			cy--
			cx = s.SizeX() - 1
		}
	} else {
		cx--
	}
	s.setCursor(cx, cy)
}
