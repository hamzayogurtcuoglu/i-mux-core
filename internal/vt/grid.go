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

// The grid is ported from tmux 3.8's grid.c. A grid is lines of cells; lines are only
// allocated when written to. Lines 0 to HSize-1 are the history and HSize to HSize+Sy-1 the
// screen. Everything here works on absolute line numbers; the view functions (view.go) work on
// the screen's.
//
// Cells are stored as tmux stores them, to keep memory low: an 8-byte entry for a cell with
// one ASCII character, 8-bit attributes and 8 or 256-colour colours, and an extended entry for
// anything else.

// Color is a colour as tmux encodes it: 0 to 7 the ANSI colours, 8 the default, 9 the
// terminal's, 90 to 97 the bright ones; with ColorFlag256, a palette index; with ColorFlagRGB,
// 0xRRGGBB; with ColorFlagTheme, a theme colour.
type Color int32

// Colour flags.
const (
	ColorFlag256   Color = 0x01000000
	ColorFlagRGB   Color = 0x02000000
	ColorFlagTheme Color = 0x04000000
	ColorDefault   Color = 8
)

// IsDefault reports whether c is the default or the terminal's colour.
func (c Color) IsDefault() bool { return c == 8 || c == 9 }

// ColorJoinRGB packs an RGB colour (tmux's colour_join_rgb).
func ColorJoinRGB(r, g, b int) Color {
	return Color(((r&0xff)<<16)|((g&0xff)<<8)|(b&0xff)) | ColorFlagRGB
}

// Cell attributes (tmux's GRID_ATTR_*).
const (
	AttrBright        = 0x1
	AttrDim           = 0x2
	AttrUnderscore    = 0x4
	AttrBlink         = 0x8
	AttrReverse       = 0x10
	AttrHidden        = 0x20
	AttrItalics       = 0x40
	AttrCharset       = 0x80
	AttrStrikethrough = 0x100
	AttrUnderscore2   = 0x200
	AttrUnderscore3   = 0x400
	AttrUnderscore4   = 0x800
	AttrUnderscore5   = 0x1000
	AttrOverline      = 0x2000
	AttrNoAttr        = 0x4000

	AttrAllUnderscore = AttrUnderscore | AttrUnderscore2 | AttrUnderscore3 | AttrUnderscore4 | AttrUnderscore5
)

// Cell flags (tmux's GRID_FLAG_*).
const (
	FlagFg256     = 0x1
	FlagBg256     = 0x2
	FlagPadding   = 0x4
	FlagExtended  = 0x8
	FlagSelected  = 0x10
	FlagNoPalette = 0x20
	FlagCleared   = 0x40
	FlagTab       = 0x80
)

// Line flags (tmux's GRID_LINE_*).
const (
	LineWrapped       = 0x1
	LineExtended      = 0x2
	LineDead          = 0x4
	LineStartPrompt   = 0x8
	LineSecondPrompt  = 0x10
	LineStartCommand  = 0x20
	LineStartOutput   = 0x40
	LineEndOutput     = 0x80
	LineHyperlink     = 0x100
	gridHistory       = 0x1
	maxCellDataLength = 32 // tmux's UTF8_SIZE: the longest grapheme a cell keeps
)

// Cell is one cell: its character (a grapheme cluster), width, attributes and colours.
type Cell struct {
	Data  string // the character; a tab's cell holds Width spaces; padding reads back as "!"
	Width uint8
	Attr  uint16
	Flags uint8
	Fg    Color
	Bg    Color
	Us    Color // underline colour
	Link  uint32
}

// DefaultCell is an empty cell.
var DefaultCell = Cell{Data: " ", Width: 1, Fg: 8, Bg: 8, Us: 8}

var (
	// tmux's padding cell has no data (size 0) but stores '!' in its compact entry, so it
	// reads back as "!" of width 1, with FlagPadding.
	paddingCell = Cell{Data: "!", Width: 0, Flags: FlagPadding, Fg: 8, Bg: 8, Us: 8}
	clearedCell = Cell{Data: " ", Width: 1, Flags: FlagCleared, Fg: 8, Bg: 8, Us: 8}
)

// cellEntry is tmux's grid_cell_entry: flags, then either attr, fg, bg and one ASCII byte, or
// (FlagExtended) an offset into the line's extended cells.
type cellEntry struct {
	flags uint8
	attr  uint8
	fg    uint8
	bg    uint8
	data  uint32 // the character, or the extended cell's offset
}

var clearedEntry = cellEntry{flags: FlagCleared, attr: 0, fg: 8, bg: 8, data: ' '}

// extdEntry is tmux's grid_extd_entry.
type extdEntry struct {
	data  string
	width uint8
	attr  uint16
	flags uint8
	fg    Color
	bg    Color
	us    Color
	link  uint32
}

// Line is one line of a grid.
type Line struct {
	cells []cellEntry // len(cells) is tmux's cellsize
	used  int         // cellused: cells written to
	extd  []extdEntry
	Flags uint16
}

// Used returns how many cells of the line were written to.
func (l *Line) Used() int { return l.used }

// Size returns how many cells the line holds.
func (l *Line) Size() int { return len(l.cells) }

// Grid is a screen's cells and its history.
type Grid struct {
	flags int

	Sx, Sy int

	HScrolled int
	HSize     int
	HLimit    int

	ScrollAdded      int
	ScrollCollected  int
	ScrollGeneration int

	lines []Line
}

// NewGrid returns a sx×sy grid keeping up to hlimit lines of history.
func NewGrid(sx, sy, hlimit int) *Grid {
	gd := &Grid{Sx: sx, Sy: sy, HLimit: hlimit}
	if hlimit != 0 {
		gd.flags = gridHistory
	}
	gd.lines = make([]Line, sy)
	return gd
}

// History reports whether lines scrolled off the top go into the history.
func (gd *Grid) History() bool { return gd.flags&gridHistory != 0 }

// SetHistory turns the history on or off (the alternate screen has none).
func (gd *Grid) SetHistory(on bool) {
	if on {
		gd.flags |= gridHistory
	} else {
		gd.flags &^= gridHistory
	}
}

func storeCell(gce *cellEntry, gc *Cell, c byte) {
	gce.flags = gc.Flags &^ FlagCleared
	gce.fg = uint8(gc.Fg & 0xff)
	if gc.Fg&ColorFlag256 != 0 {
		gce.flags |= FlagFg256
	}
	gce.bg = uint8(gc.Bg & 0xff)
	if gc.Bg&ColorFlag256 != 0 {
		gce.flags |= FlagBg256
	}
	gce.attr = uint8(gc.Attr)
	gce.data = uint32(c)
}

func needExtendedCell(gce *cellEntry, gc *Cell) bool {
	switch {
	case gce.flags&FlagExtended != 0:
		return true
	case gc.Attr > 0xff:
		return true
	case len(gc.Data) > 1 || gc.Width > 1:
		return true
	case gc.Fg&(ColorFlagRGB|ColorFlagTheme) != 0 || gc.Bg&(ColorFlagRGB|ColorFlagTheme) != 0:
		return true
	case gc.Us != 8:
		return true
	case gc.Link != 0:
		return true
	case gc.Flags&FlagTab != 0:
		return true
	}
	return false
}

func (l *Line) getExtendedCell(gce *cellEntry, flags uint8) {
	l.extd = append(l.extd, extdEntry{})
	gce.data = uint32(len(l.extd) - 1)
	gce.flags = flags | FlagExtended
}

func (l *Line) extendedCell(gce *cellEntry, gc *Cell) *extdEntry {
	flags := gc.Flags &^ FlagCleared
	if gce.flags&FlagExtended == 0 {
		l.getExtendedCell(gce, flags)
	} else if int(gce.data) >= len(l.extd) {
		panic("vt: extended cell offset too big")
	}
	l.Flags |= LineExtended
	if gc.Link != 0 {
		l.Flags |= LineHyperlink
	}
	gee := &l.extd[gce.data]
	if gc.Flags&FlagTab != 0 {
		gee.data = ""
	} else {
		gee.data = gc.Data
	}
	gee.width = gc.Width
	gee.attr = gc.Attr
	gee.flags = flags
	gee.fg, gee.bg, gee.us = gc.Fg, gc.Bg, gc.Us
	gee.link = gc.Link
	return gee
}

// compact drops extended cells no entry uses any more.
func (l *Line) compact() {
	if len(l.extd) == 0 {
		return
	}
	n := 0
	for i := range l.cells {
		if l.cells[i].flags&FlagExtended != 0 {
			n++
		}
	}
	if n == 0 {
		l.extd = nil
		return
	}
	extd := make([]extdEntry, 0, n)
	for i := range l.cells {
		gce := &l.cells[i]
		if gce.flags&FlagExtended != 0 {
			extd = append(extd, l.extd[gce.data])
			gce.data = uint32(len(extd) - 1)
		}
	}
	l.extd = extd
}

// Line returns line py (absolute).
func (gd *Grid) Line(py int) *Line { return &gd.lines[py] }

func (gd *Grid) clearCell(px, py int, bg Color, moved bool) {
	gl := &gd.lines[py]
	gce := &gl.cells[px]
	oldOffset := gce.data
	hadExtd := gce.flags&FlagExtended != 0

	*gce = clearedEntry
	switch {
	case !moved && hadExtd && int(oldOffset) < len(gl.extd):
		gce.flags |= FlagExtended
		gce.data = oldOffset
		gee := gl.extendedCell(gce, &clearedCell)
		if bg != 8 {
			gee.bg = bg
		}
	case bg != 8:
		if bg&(ColorFlagRGB|ColorFlagTheme) != 0 {
			gl.getExtendedCell(gce, gce.flags)
			gee := gl.extendedCell(gce, &clearedCell)
			gee.bg = bg
		} else {
			if bg&ColorFlag256 != 0 {
				gce.flags |= FlagBg256
			}
			gce.bg = uint8(bg)
		}
	}
}

func (gd *Grid) validY(py int) bool { return py >= 0 && py < gd.HSize+gd.Sy }

// CellsLookEqual reports whether two cells have the same style.
func CellsLookEqual(a, b *Cell) bool {
	return a.Fg == b.Fg && a.Bg == b.Bg && a.Attr == b.Attr &&
		a.Flags&^FlagCleared == b.Flags&^FlagCleared && a.Link == b.Link
}

// CellsEqual reports whether two cells are the same.
func CellsEqual(a, b *Cell) bool {
	return CellsLookEqual(a, b) && a.Width == b.Width && a.Data == b.Data
}

// SetTab makes gc a tab of the given width.
func (gc *Cell) SetTab(width int) {
	gc.Flags |= FlagTab
	gc.Flags &^= FlagPadding
	gc.Width = uint8(width)
	gc.Data = spaces(width)
}

func spaces(n int) string {
	const s = "                                                                                "
	if n <= len(s) {
		return s[:n]
	}
	b := make([]byte, n)
	for i := range b {
		b[i] = ' '
	}
	return string(b)
}

func (gd *Grid) freeLine(py int) { gd.lines[py] = Line{} }

func (gd *Grid) freeLines(py, ny int) {
	for yy := py; yy < py+ny; yy++ {
		gd.freeLine(yy)
	}
}

func (gd *Grid) trimHistory(ny int) {
	n := copy(gd.lines, gd.lines[ny:gd.HSize+gd.Sy])
	for i := n; i < n+ny; i++ {
		gd.lines[i] = Line{}
	}
	gd.lines = gd.lines[:n]
}

// CollectHistory frees the oldest tenth of the history once it reaches the limit (all: down
// to the limit).
func (gd *Grid) CollectHistory(all bool) {
	if gd.HSize == 0 || gd.HSize < gd.HLimit {
		return
	}
	var ny int
	if all {
		ny = gd.HSize - gd.HLimit
	} else {
		ny = gd.HLimit / 10
	}
	ny = max(ny, 1)
	ny = min(ny, gd.HSize)
	gd.trimHistory(ny)
	gd.HSize -= ny
	gd.ScrollCollected += ny
	gd.HScrolled = min(gd.HScrolled, gd.HSize)
}

// RemoveHistory removes ny lines from the bottom of the history.
func (gd *Grid) RemoveHistory(ny int) {
	if ny > gd.HSize {
		return
	}
	start := gd.HSize + gd.Sy - ny
	for yy := 0; yy < ny; yy++ {
		gd.freeLine(start + yy)
	}
	gd.lines = gd.lines[:start]
	gd.HSize -= ny
}

// ScrollHistory scrolls the whole screen up a line, into the history.
func (gd *Grid) ScrollHistory(bg Color) {
	yy := gd.HSize + gd.Sy
	gd.lines = append(gd.lines[:yy], Line{})
	gd.emptyLine(yy, bg)
	gd.HScrolled++
	gd.lines[gd.HSize].compact()
	gd.HSize++
	gd.ScrollAdded++
}

// ClearHistory drops the whole history.
func (gd *Grid) ClearHistory() {
	gd.trimHistory(gd.HSize)
	gd.HScrolled = 0
	gd.HSize = 0
	gd.ScrollGeneration++
}

// ScrollHistoryRegion scrolls the region from upper to lower (absolute lines) up a line,
// moving its top line into the history.
func (gd *Grid) ScrollHistoryRegion(upper, lower int, bg Color) {
	yy := gd.HSize + gd.Sy
	gd.lines = append(gd.lines[:yy], Line{})
	// Move the screen down a line to make room for the new history line.
	copy(gd.lines[gd.HSize+1:], gd.lines[gd.HSize:yy])
	upper++
	lower++
	gd.lines[gd.HSize] = gd.lines[upper]
	copy(gd.lines[upper:lower], gd.lines[upper+1:lower+1])
	gd.lines[lower] = Line{}
	gd.emptyLine(lower, bg)
	gd.HScrolled++
	gd.HSize++
	gd.ScrollAdded++
}

func (gd *Grid) expandLine(py, sx int, bg Color) {
	gl := &gd.lines[py]
	if sx <= len(gl.cells) {
		return
	}
	switch {
	case sx < gd.Sx/4:
		sx = gd.Sx / 4
	case sx < gd.Sx/2:
		sx = gd.Sx / 2
	case gd.Sx > sx:
		sx = gd.Sx
	}
	old := len(gl.cells)
	if cap(gl.cells) >= sx {
		gl.cells = gl.cells[:sx]
	} else {
		cells := make([]cellEntry, sx)
		copy(cells, gl.cells)
		gl.cells = cells
	}
	for xx := old; xx < sx; xx++ {
		gl.cells[xx] = cellEntry{}
		gd.clearCell(xx, py, bg, false)
	}
}

func (gd *Grid) emptyLine(py int, bg Color) {
	gd.lines[py] = Line{}
	if !bg.IsDefault() {
		gd.expandLine(py, gd.Sx, bg)
	}
}

// PeekLine returns line py, or nil when there is none.
func (gd *Grid) PeekLine(py int) *Line {
	if !gd.validY(py) {
		return nil
	}
	return &gd.lines[py]
}

func (l *Line) cell(px int, gc *Cell) {
	gce := &l.cells[px]
	if gce.flags&FlagExtended != 0 {
		if int(gce.data) >= len(l.extd) {
			*gc = DefaultCell
			return
		}
		gee := &l.extd[gce.data]
		gc.Flags = gee.flags
		gc.Attr = gee.attr
		gc.Fg, gc.Bg, gc.Us = gee.fg, gee.bg, gee.us
		gc.Link = gee.link
		if gc.Flags&FlagTab != 0 {
			gc.SetTab(int(gee.width))
		} else {
			gc.Data = gee.data
			gc.Width = gee.width
		}
		return
	}
	gc.Flags = gce.flags &^ (FlagFg256 | FlagBg256)
	gc.Attr = uint16(gce.attr)
	gc.Fg = Color(gce.fg)
	if gce.flags&FlagFg256 != 0 {
		gc.Fg |= ColorFlag256
	}
	gc.Bg = Color(gce.bg)
	if gce.flags&FlagBg256 != 0 {
		gc.Bg |= ColorFlag256
	}
	gc.Us = 8
	gc.Data = asciiString(byte(gce.data))
	gc.Width = 1
	gc.Link = 0
}

// asciiString returns a one-byte string without allocating.
func asciiString(c byte) string { return asciiTable[c : c+1] }

var asciiTable = func() string {
	b := make([]byte, 256)
	for i := range b {
		b[i] = byte(i)
	}
	return string(b)
}()

// Cell returns the cell at px, py (absolute); outside what was written, the default cell.
func (gd *Grid) Cell(px, py int) Cell {
	var gc Cell
	if !gd.validY(py) || px < 0 || px >= len(gd.lines[py].cells) {
		return DefaultCell
	}
	gd.lines[py].cell(px, &gc)
	return gc
}

// SetCell sets the cell at px, py (absolute).
func (gd *Grid) SetCell(px, py int, gc *Cell) {
	if !gd.validY(py) {
		return
	}
	gd.expandLine(py, px+1, 8)
	gl := &gd.lines[py]
	if px+1 > gl.used {
		gl.used = px + 1
	}
	gce := &gl.cells[px]
	if needExtendedCell(gce, gc) {
		gl.extendedCell(gce, gc)
	} else {
		storeCell(gce, gc, gc.Data[0])
	}
}

// SetPadding makes the cell at px, py the second half of a wide character.
func (gd *Grid) SetPadding(px, py int, bg Color) {
	gc := paddingCell
	gc.Bg = bg
	gd.SetCell(px, py, &gc)
}

// SetCells sets the cells from px with the ASCII bytes of s, all in gc's style.
func (gd *Grid) SetCells(px, py int, gc *Cell, s []byte) {
	if !gd.validY(py) {
		return
	}
	gd.expandLine(py, px+len(s), 8)
	gl := &gd.lines[py]
	if px+len(s) > gl.used {
		gl.used = px + len(s)
	}
	for i, c := range s {
		gce := &gl.cells[px+i]
		if needExtendedCell(gce, gc) {
			gee := gl.extendedCell(gce, gc)
			gee.data = asciiString(c)
			gee.width = 1
		} else {
			storeCell(gce, gc, c)
		}
	}
}

// Clear clears nx×ny cells from px, py (absolute) to background bg.
func (gd *Grid) Clear(px, py, nx, ny int, bg Color) {
	if nx == 0 || ny == 0 {
		return
	}
	if px == 0 && nx == gd.Sx {
		gd.ClearLines(py, ny, bg)
		return
	}
	if !gd.validY(py) || !gd.validY(py+ny-1) {
		return
	}
	for yy := py; yy < py+ny; yy++ {
		gl := &gd.lines[yy]
		sx := min(gd.Sx, len(gl.cells))
		ox := nx
		if bg.IsDefault() {
			if px > sx {
				continue
			}
			if px+nx > sx {
				ox = sx - px
			}
		}
		gd.expandLine(yy, px+ox, 8) // default background first
		for xx := px; xx < px+ox; xx++ {
			gd.clearCell(xx, yy, bg, false)
		}
	}
}

// ClearLines empties ny lines from py (absolute).
func (gd *Grid) ClearLines(py, ny int, bg Color) {
	if ny == 0 || !gd.validY(py) || !gd.validY(py+ny-1) {
		return
	}
	for yy := py; yy < py+ny; yy++ {
		gd.freeLine(yy)
		gd.emptyLine(yy, bg)
	}
	if py != 0 {
		gd.lines[py-1].Flags &^= LineWrapped
	}
}

// MoveLines moves ny lines from py to dy (absolute).
func (gd *Grid) MoveLines(dy, py, ny int, bg Color) {
	if ny == 0 || py == dy {
		return
	}
	if !gd.validY(py) || !gd.validY(py+ny-1) || !gd.validY(dy) || !gd.validY(dy+ny-1) {
		return
	}
	for yy := dy; yy < dy+ny; yy++ {
		if yy >= py && yy < py+ny {
			continue
		}
		gd.freeLine(yy)
	}
	if dy != 0 {
		gd.lines[dy-1].Flags &^= LineWrapped
	}
	moved := make([]Line, ny)
	copy(moved, gd.lines[py:py+ny])
	copy(gd.lines[dy:dy+ny], moved)
	for yy := py; yy < py+ny; yy++ {
		if yy < dy || yy >= dy+ny {
			gd.emptyLine(yy, bg)
		}
	}
	if py != 0 && (py < dy || py >= dy+ny) {
		gd.lines[py-1].Flags &^= LineWrapped
	}
}

// MoveCells moves nx cells of line py from px to dx.
func (gd *Grid) MoveCells(dx, px, py, nx int, bg Color) {
	if nx == 0 || px == dx || !gd.validY(py) {
		return
	}
	gd.expandLine(py, px+nx, 8)
	gd.expandLine(py, dx+nx, 8)
	gl := &gd.lines[py]
	copy(gl.cells[dx:dx+nx], gl.cells[px:px+nx])
	if dx+nx > gl.used {
		gl.used = dx + nx
	}
	for xx := px; xx < px+nx; xx++ {
		if xx >= dx && xx < dx+nx {
			continue
		}
		gd.clearCell(xx, py, bg, true)
	}
}

// DuplicateLines copies ny lines from src (from sy) to gd (at dy).
func (gd *Grid) DuplicateLines(dy int, src *Grid, sy, ny int) {
	if dy+ny > gd.HSize+gd.Sy {
		ny = gd.HSize + gd.Sy - dy
	}
	if sy+ny > src.HSize+src.Sy {
		ny = src.HSize + src.Sy - sy
	}
	gd.freeLines(dy, ny)
	for yy := 0; yy < ny; yy++ {
		s := &src.lines[sy]
		d := Line{used: s.used, Flags: s.Flags}
		if len(s.cells) != 0 {
			d.cells = append([]cellEntry(nil), s.cells...)
		}
		if len(s.extd) != 0 {
			d.extd = append([]extdEntry(nil), s.extd...)
		}
		gd.lines[dy] = d
		sy++
		dy++
	}
}

// LineLength returns the length of line py without its trailing spaces.
func (gd *Grid) LineLength(py int) int {
	px := min(len(gd.lines[py].cells), gd.Sx)
	for px > 0 {
		gc := gd.Cell(px-1, py)
		if gc.Flags&FlagPadding != 0 || gc.Data != " " {
			break
		}
		px--
	}
	return px
}

// LineLimit returns the last position of line py, not counting padding.
func (gd *Grid) LineLimit(py int) int {
	px := gd.LineLength(py)
	if px == 0 {
		return 0
	}
	px--
	for px > 0 {
		if gc := gd.Cell(px, py); gc.Flags&FlagPadding == 0 {
			break
		}
		px--
	}
	return px
}

// WrapPosition converts px, py to a position counted along wrapped lines.
func (gd *Grid) WrapPosition(px, py int) (wx, wy int) {
	ax, ay := 0, 0
	yy := 0
	for ; yy < py; yy++ {
		if gd.lines[yy].Flags&LineWrapped != 0 {
			ax += gd.lines[yy].used
		} else {
			ax = 0
			ay++
		}
	}
	if px >= gd.lines[yy].used {
		ax = maxPosition
	} else {
		ax += px
	}
	return ax, ay
}

// maxPosition stands for tmux's UINT_MAX in wrapped positions: the end of the line.
const maxPosition = int(^uint32(0))

// UnwrapPosition converts a position counted along wrapped lines back.
func (gd *Grid) UnwrapPosition(wx, wy int) (px, py int) {
	ay, ey := 0, gd.HSize+gd.Sy-1
	yy := 0
	for ; yy < gd.HSize+gd.Sy-1; yy++ {
		if ay == wy {
			break
		}
		if gd.lines[yy].Flags&LineWrapped == 0 {
			ay++
		}
	}
	if wx == maxPosition {
		for yy < ey && gd.lines[yy].Flags&LineWrapped != 0 {
			yy++
		}
		wx = gd.lines[yy].used
	} else {
		for gd.lines[yy].Flags&LineWrapped != 0 {
			if wx < gd.lines[yy].used {
				break
			}
			wx -= gd.lines[yy].used
			yy++
		}
	}
	return wx, yy
}

// AdjustLines sets how many lines the grid holds (history and screen), as grid_adjust_lines.
func (gd *Grid) AdjustLines(n int) {
	switch {
	case n < len(gd.lines):
		gd.lines = gd.lines[:n]
	case n > len(gd.lines):
		gd.lines = append(gd.lines, make([]Line, n-len(gd.lines))...)
	}
}

// Reflow, ported from grid_reflow and its helpers: lines are rewrapped to width sx, joining
// wrapped lines and splitting long ones, in the history as on the screen.

func reflowDead(gl *Line) { *gl = Line{Flags: LineDead} }

// reflowAdd adds n empty lines to the target and returns the index of the first.
func (gd *Grid) reflowAdd(n int) int {
	first := gd.Sy
	gd.lines = append(gd.lines, make([]Line, n)...)
	gd.Sy += n
	return first
}

// reflowMove moves a line into the target, leaving it dead behind.
func (gd *Grid) reflowMove(from *Line) int {
	to := gd.reflowAdd(1)
	gd.lines[to] = *from
	reflowDead(from)
	return to
}

func (gd *Grid) reflowJoin(target *Grid, sx, yy, width int, already bool) {
	var to int
	if !already {
		to = target.Sy
		target.reflowMove(&gd.lines[yy])
	} else {
		to = target.Sy - 1
	}
	gl := &target.lines[to]
	at := gl.used

	lines, want := 0, 0
	var from *Line
	wrapped := true
	for yy+1+lines != gd.HSize+gd.Sy {
		line := yy + 1 + lines
		if gd.lines[line].Flags&LineWrapped == 0 {
			wrapped = false
		}
		if gd.lines[line].used == 0 {
			if !wrapped {
				break
			}
			lines++
			continue
		}
		var gc Cell
		gd.lines[line].cell(0, &gc)
		if width+int(gc.Width) > sx {
			break
		}
		width += int(gc.Width)
		target.SetCell(at, to, &gc)
		at++

		from = &gd.lines[line]
		for want = 1; want < from.used; want++ {
			from.cell(want, &gc)
			if width+int(gc.Width) > sx {
				break
			}
			width += int(gc.Width)
			target.SetCell(at, to, &gc)
			at++
		}
		lines++
		if !wrapped || want != from.used || width == sx {
			break
		}
	}
	if lines == 0 || from == nil {
		return
	}
	gl = &target.lines[to]
	left := from.used - want
	if left != 0 {
		gd.MoveCells(0, want, yy+lines, left, 8)
		from.cells = from.cells[:left]
		from.used = left
		lines--
	} else if !wrapped {
		gl.Flags &^= LineWrapped
	}
	for i := yy + 1; i < yy+1+lines; i++ {
		reflowDead(&gd.lines[i])
	}
	if gd.HScrolled > to+lines {
		gd.HScrolled -= lines
	} else if gd.HScrolled > to {
		gd.HScrolled = to
	}
}

func (gd *Grid) reflowSplit(target *Grid, sx, yy, at int) {
	gl := &gd.lines[yy]
	used := gl.used
	flags := gl.Flags
	var gc Cell

	var lines int
	if gl.Flags&LineExtended == 0 {
		lines = 1 + (gl.used-1)/sx
	} else {
		lines = 2
		width := 0
		for i := at; i < used; i++ {
			gl.cell(i, &gc)
			if width+int(gc.Width) > sx {
				lines++
				width = 0
			}
			width += int(gc.Width)
		}
	}

	line := target.Sy + 1
	first := target.reflowAdd(lines)

	width, xx := 0, 0
	for i := at; i < used; i++ {
		gl.cell(i, &gc)
		if width+int(gc.Width) > sx {
			target.lines[line].Flags |= LineWrapped
			line++
			width = 0
			xx = 0
		}
		width += int(gc.Width)
		target.SetCell(xx, line, &gc)
		xx++
	}
	if flags&LineWrapped != 0 {
		target.lines[line].Flags |= LineWrapped
	}

	gl.cells = gl.cells[:at]
	gl.used = at
	gl.Flags |= LineWrapped
	target.lines[first] = *gl
	reflowDead(gl)

	if yy <= gd.HScrolled {
		gd.HScrolled += lines - 1
	}
	if width < sx && flags&LineWrapped != 0 {
		gd.reflowJoin(target, sx, yy, width, true)
	}
}

// Reflow rewraps the grid's lines to width sx.
func (gd *Grid) Reflow(sx int) {
	target := &Grid{Sx: gd.Sx}
	var gc Cell
	for yy := 0; yy < gd.HSize+gd.Sy; yy++ {
		gl := &gd.lines[yy]
		if gl.Flags&LineDead != 0 {
			continue
		}
		at, width := 0, 0
		if gl.Flags&LineExtended == 0 {
			width = gl.used
			at = min(width, sx)
		} else {
			for i := 0; i < gl.used; i++ {
				gl.cell(i, &gc)
				if at == 0 && width+int(gc.Width) > sx {
					at = i
				}
				width += int(gc.Width)
			}
		}
		switch {
		case width == sx:
			target.reflowMove(gl)
		case width > sx:
			gd.reflowSplit(target, sx, yy, at)
		case gl.Flags&LineWrapped != 0:
			gd.reflowJoin(target, sx, yy, width, false)
		default:
			target.reflowMove(gl)
		}
	}
	if target.Sy < gd.Sy {
		target.reflowAdd(gd.Sy - target.Sy)
	}
	gd.HSize = target.Sy - gd.Sy
	if gd.HScrolled > gd.HSize {
		gd.HScrolled = gd.HSize
	}
	gd.lines = target.lines
	gd.ScrollGeneration++
}
