// Ported from tmux 3.8's tty.c and tty-draw.c:
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

package termout

import (
	"strconv"
	"strings"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/vt"
)

// Writing cells (TASKS.md C2.8): attributes and colours as the terminal supports them, the
// line drawing set, and lines of cells with the fewest sequences, as tmux's tty.c and
// tty-draw.c do. The cells given here are final: a pane's default colours, palette and theme
// have been applied by the caller (internal/render), so tmux's per-pane style context is not
// needed; what is left is fitting them to the terminal.

// LinkLookup gives a cell's hyperlink: its URI and id.
type LinkLookup func(link uint32) (uri, id string, ok bool)

// SetLinks sets where the hyperlinks of the cells written are looked up; nil writes none.
func (t *TTY) SetLinks(l LinkLookup) { t.links = l }

func (t *TTY) putcodeIII(name string, a, b, c int) {
	if s := t.Term.String(name); s != "" {
		t.puts(Tparm(s, a, b, c))
	}
}

func (t *TTY) putcodeSS(name, a, b string) {
	if s := t.Term.String(name); s != "" {
		t.puts(Tparm(s, a, b))
	}
}

// Reset sets the terminal's attributes back to the default (tmux's tty_reset).
func (t *TTY) Reset() {
	if !vt.CellsEqual(&t.cell, &vt.DefaultCell) {
		if t.cell.Link != 0 {
			t.putcodeSS("Hls", "", "")
		}
		if t.cell.Attr&vt.AttrCharset != 0 && t.acsNeeded() {
			t.putcode("rmacs")
		}
		t.putcode("sgr0")
		t.cell = vt.DefaultCell
	}
	t.lastCell = vt.DefaultCell
}

// SyncStart begins synchronized output if the terminal has it (tmux's tty_sync_start).
func (t *TTY) SyncStart() {
	if t.syncing {
		return
	}
	t.syncing = true
	if t.Term.Has("Sync") {
		t.putcodeI("Sync", 1)
	}
}

// SyncEnd ends synchronized output (tmux's tty_sync_end).
func (t *TTY) SyncEnd() {
	if !t.syncing {
		return
	}
	t.syncing = false
	if t.Term.Has("Sync") {
		t.putcodeI("Sync", 2)
	}
}

// Buffered is how many bytes have been written since the last Take.
func (t *TTY) Buffered() int { return t.out.Len() }

// Mode is the terminal's current mode bits, as UpdateMode last set them.
func (t *TTY) Mode() int { return t.mode }

// Position is where the terminal's cursor is, or -1 when that is not known.
func (t *TTY) Position() (cx, cy int) { return t.cx, t.cy }

// putc writes one byte, through the line drawing set when the attributes say so, and moves
// the cursor as the terminal does (tmux's tty_putc).
func (t *TTY) putc(ch byte) {
	if t.Term.Flags&TermNoAM != 0 && ch >= 0x20 && ch != 0x7f && t.cy == t.Sy-1 && t.cx+1 >= t.Sx {
		return
	}
	if t.cell.Attr&vt.AttrCharset != 0 {
		if s := t.acsGet(ch); s != "" {
			t.puts(s)
		} else {
			t.out.WriteByte(ch)
		}
	} else {
		t.out.WriteByte(ch)
	}
	if ch >= 0x20 && ch != 0x7f && t.cx != unknown {
		if t.cx >= t.Sx {
			t.cx = 1
			if t.cy != t.rlower {
				t.cy++
			}
			if t.Term.Flags&TermNoAM != 0 {
				t.putcodeII("cup", t.cy, t.cx)
			}
		} else {
			t.cx++
		}
	}
}

// putn writes characters of width columns (tmux's tty_putn).
func (t *TTY) putn(s string, width int) {
	if t.Term.Flags&TermNoAM != 0 && t.cy == t.Sy-1 && t.cx != unknown && t.cx+len(s) >= t.Sx {
		s = s[:max(0, min(len(s), t.Sx-t.cx-1))]
	}
	t.puts(s)
	if t.cx == unknown {
		return
	}
	if t.cx+width > t.Sx {
		t.cx = t.cx + width - t.Sx
		if t.cx <= t.Sx {
			t.cy++
		} else {
			t.cx, t.cy = unknown, unknown
		}
	} else {
		t.cx += width
	}
}

// repeatSpace writes n spaces (tmux's tty_repeat_space).
func (t *TTY) repeatSpace(n int) {
	const chunk = 500
	for n > chunk {
		t.putn(strings.Repeat(" ", chunk), chunk)
		n -= chunk
	}
	if n != 0 {
		t.putn(strings.Repeat(" ", n), n)
	}
}

// setItalics is tmux's tty_set_italics, with default-terminal at its default.
func (t *TTY) setItalics() {
	if t.Term.Has("sitm") {
		t.putcode("sitm")
		return
	}
	t.putcode("smso")
}

// Attributes puts the terminal in the cell's attributes and colours (tmux's tty_attributes,
// with no pane palette or default colours: the cell's are final).
func (t *TTY) Attributes(gc *vt.Cell) {
	tc := &t.cell
	gc2 := *gc
	if gc2.Attr == t.lastCell.Attr && gc2.Fg == t.lastCell.Fg && gc2.Bg == t.lastCell.Bg &&
		gc2.Us == t.lastCell.Us && gc2.Link == t.lastCell.Link {
		return
	}
	// Without setab, reverse stands in for a background colour.
	if !t.Term.Has("setab") {
		if gc2.Attr&vt.AttrReverse != 0 {
			if gc2.Fg != 7 && !gc2.Fg.IsDefault() {
				gc2.Attr &^= vt.AttrReverse
			}
		} else if gc2.Bg != 0 && !gc2.Bg.IsDefault() {
			gc2.Attr |= vt.AttrReverse
		}
	}
	t.checkFg(&gc2)
	t.checkBg(&gc2)
	t.checkUs(&gc2)

	// Clearing a bit, or making the underline colour default, needs a reset.
	if tc.Attr&^gc2.Attr != 0 || (tc.Us != gc2.Us && gc2.Us == 0) {
		t.Reset()
	}
	t.colours(&gc2)

	changed := gc2.Attr &^ tc.Attr
	tc.Attr = gc2.Attr
	if changed&vt.AttrBright != 0 {
		t.putcode("bold")
	}
	if changed&vt.AttrDim != 0 {
		t.putcode("dim")
	}
	if changed&vt.AttrItalics != 0 {
		t.setItalics()
	}
	if changed&vt.AttrAllUnderscore != 0 {
		switch {
		case changed&vt.AttrUnderscore != 0:
			t.putcode("smul")
		case changed&vt.AttrUnderscore2 != 0:
			t.putcodeI("Smulx", 2)
		case changed&vt.AttrUnderscore3 != 0:
			t.putcodeI("Smulx", 3)
		case changed&vt.AttrUnderscore4 != 0:
			t.putcodeI("Smulx", 4)
		case changed&vt.AttrUnderscore5 != 0:
			t.putcodeI("Smulx", 5)
		}
	}
	if changed&vt.AttrBlink != 0 {
		t.putcode("blink")
	}
	if changed&vt.AttrReverse != 0 {
		if t.Term.Has("rev") {
			t.putcode("rev")
		} else if t.Term.Has("smso") {
			t.putcode("smso")
		}
	}
	if changed&vt.AttrHidden != 0 {
		t.putcode("invis")
	}
	if changed&vt.AttrStrikethrough != 0 {
		t.putcode("smxx")
	}
	if changed&vt.AttrOverline != 0 {
		t.putcode("Smol")
	}
	if changed&vt.AttrCharset != 0 && t.acsNeeded() {
		t.putcode("smacs")
	}
	t.hyperlink(gc)
	t.lastCell = gc2
}

// hyperlink starts or ends the cell's hyperlink (tmux's tty_hyperlink).
func (t *TTY) hyperlink(gc *vt.Cell) {
	if gc.Link == t.cell.Link {
		return
	}
	t.cell.Link = gc.Link
	if t.links == nil {
		return
	}
	if uri, id, ok := t.links(gc.Link); gc.Link != 0 && ok {
		t.putcodeSS("Hls", id, uri)
	} else {
		t.putcodeSS("Hls", "", "")
	}
}

// colours sets the colours that changed (tmux's tty_colours).
func (t *TTY) colours(gc *vt.Cell) {
	tc := &t.cell
	if gc.Fg == tc.Fg && gc.Bg == tc.Bg && gc.Us == tc.Us {
		return
	}
	if gc.Fg.IsDefault() || gc.Bg.IsDefault() {
		if !t.Term.Flag("AX") {
			t.Reset()
		} else {
			if gc.Fg.IsDefault() && !tc.Fg.IsDefault() {
				t.puts("\033[39m")
				tc.Fg = gc.Fg
			}
			if gc.Bg.IsDefault() && !tc.Bg.IsDefault() {
				t.puts("\033[49m")
				tc.Bg = gc.Bg
			}
		}
	}
	if !gc.Fg.IsDefault() && gc.Fg != tc.Fg {
		t.coloursFg(gc)
	}
	if !gc.Bg.IsDefault() && gc.Bg != tc.Bg {
		t.coloursBg(gc)
	}
	if gc.Us != tc.Us {
		t.coloursUs(gc)
	}
}

func (t *TTY) colourCount() int {
	if t.Term.Flags&Term256Colours != 0 {
		return 256
	}
	return t.Term.Number("colors")
}

// checkFg fits the foreground to the terminal's colours (tmux's tty_check_fg).
func (t *TTY) checkFg(gc *vt.Cell) {
	if gc.Fg&vt.ColorFlagRGB != 0 {
		if t.Term.Flags&TermRGBColours != 0 {
			return
		}
		r, g, b := vt.ColorSplitRGB(gc.Fg)
		gc.Fg = vt.ColorFindRGB(r, g, b)
	}
	colours := t.colourCount()
	if gc.Fg&vt.ColorFlag256 != 0 {
		if colours >= 256 {
			return
		}
		gc.Fg = vt.Color256To16(gc.Fg)
		if gc.Fg&8 == 0 {
			return
		}
		gc.Fg &= 7
		if colours >= 16 {
			gc.Fg += 90
		} else if gc.Fg == 0 && gc.Bg == 0 {
			// Black on black or white on white is no use: change the foreground.
			gc.Fg = 7
		} else if gc.Fg == 7 && gc.Bg == 7 {
			gc.Fg = 0
		}
		return
	}
	if gc.Fg >= 90 && gc.Fg <= 97 && colours < 16 {
		gc.Fg -= 90
		gc.Attr |= vt.AttrBright
	}
}

// checkBg fits the background to the terminal's colours (tmux's tty_check_bg).
func (t *TTY) checkBg(gc *vt.Cell) {
	if gc.Bg&vt.ColorFlagRGB != 0 {
		if t.Term.Flags&TermRGBColours != 0 {
			return
		}
		r, g, b := vt.ColorSplitRGB(gc.Bg)
		gc.Bg = vt.ColorFindRGB(r, g, b)
	}
	colours := t.colourCount()
	if gc.Bg&vt.ColorFlag256 != 0 {
		if colours >= 256 {
			return
		}
		gc.Bg = vt.Color256To16(gc.Bg)
		if gc.Bg&8 == 0 {
			return
		}
		gc.Bg &= 7
		if colours >= 16 {
			gc.Bg += 90
		}
		return
	}
	if gc.Bg >= 90 && gc.Bg <= 97 && colours < 16 {
		gc.Bg -= 90
	}
}

// checkUs fits the underline colour (tmux's tty_check_us).
func (t *TTY) checkUs(gc *vt.Cell) {
	if !t.Term.Has("Setulc1") {
		if c := vt.ColorForceRGB(gc.Us); c == -1 {
			gc.Us = 8
		} else {
			gc.Us = c
		}
	}
}

// coloursFg sets the foreground (tmux's tty_colours_fg).
func (t *TTY) coloursFg(gc *vt.Cell) {
	tc := &t.cell
	// Some terminals do not clear bright correctly when leaving an aixterm colour.
	if tc.Fg >= 90 && tc.Fg <= 97 && (gc.Fg < 90 || gc.Fg > 97) {
		t.Reset()
	}
	switch {
	case gc.Fg&(vt.ColorFlagRGB|vt.ColorFlag256) != 0:
		if !t.tryColour(gc.Fg, true) {
			return
		}
	case gc.Fg >= 90 && gc.Fg <= 97:
		if t.Term.Flags&Term256Colours != 0 {
			t.puts("\033[" + strconv.Itoa(int(gc.Fg)) + "m")
		} else {
			t.putcodeI("setaf", int(gc.Fg)-90+8)
		}
	default:
		t.putcodeI("setaf", int(gc.Fg))
	}
	tc.Fg = gc.Fg
}

// coloursBg sets the background (tmux's tty_colours_bg).
func (t *TTY) coloursBg(gc *vt.Cell) {
	tc := &t.cell
	switch {
	case gc.Bg&(vt.ColorFlagRGB|vt.ColorFlag256) != 0:
		if !t.tryColour(gc.Bg, false) {
			return
		}
	case gc.Bg >= 90 && gc.Bg <= 97:
		if t.Term.Flags&Term256Colours != 0 {
			t.puts("\033[" + strconv.Itoa(int(gc.Bg)+10) + "m")
		} else {
			t.putcodeI("setab", int(gc.Bg)-90+8)
		}
	default:
		t.putcodeI("setab", int(gc.Bg))
	}
	tc.Bg = gc.Bg
}

// coloursUs sets the underline colour (tmux's tty_colours_us).
func (t *TTY) coloursUs(gc *vt.Cell) {
	tc := &t.cell
	if gc.Us.IsDefault() {
		t.putcode("ol")
		tc.Us = gc.Us
		return
	}
	if gc.Us&vt.ColorFlagRGB == 0 {
		c := gc.Us
		if c&vt.ColorFlag256 == 0 && c >= 90 && c <= 97 {
			c -= 82
		}
		t.putcodeI("Setulc1", int(c&^vt.ColorFlag256))
		return
	}
	r, g, b := vt.ColorSplitRGB(gc.Us)
	c := 65536*int(r) + 256*int(g) + int(b)
	if t.Term.Has("Setulc") {
		t.putcodeI("Setulc", c)
	} else if t.Term.Has("setal") && t.Term.Has("RGB") {
		t.putcodeI("setal", c)
	}
	tc.Us = gc.Us
}

// tryColour sets a 256 or RGB colour (tmux's tty_try_colour).
func (t *TTY) tryColour(c vt.Color, fg bool) bool {
	if c&vt.ColorFlag256 != 0 {
		if fg && t.Term.Has("setaf") {
			t.putcodeI("setaf", int(c&0xff))
		} else if t.Term.Has("setab") {
			t.putcodeI("setab", int(c&0xff))
		}
		return true
	}
	if c&vt.ColorFlagRGB != 0 {
		r, g, b := vt.ColorSplitRGB(c & 0xffffff)
		if fg && t.Term.Has("setrgbf") {
			t.putcodeIII("setrgbf", int(r), int(g), int(b))
		} else if t.Term.Has("setrgbb") {
			t.putcodeIII("setrgbb", int(r), int(g), int(b))
		}
		return true
	}
	return false
}

// checkCodeset gives what a cell can be written as: a non-UTF-8 terminal gets line drawing
// through its alternate set, and other characters as underscores (tmux's tty_check_codeset).
func (t *TTY) checkCodeset(gc *vt.Cell) *vt.Cell {
	if len(gc.Data) == 1 && gc.Data[0] < 0x7f {
		return gc
	}
	if gc.Flags&vt.FlagTab != 0 || t.UTF8 {
		return gc
	}
	n := *gc
	if c, ok := acsReverse[gc.Data]; ok {
		n.Data = string(c)
		n.Attr |= vt.AttrCharset
		return &n
	}
	n.Data = strings.Repeat("_", min(int(gc.Width), 32))
	return &n
}

// Cell writes one cell at the cursor (tmux's tty_cell).
func (t *TTY) Cell(gc *vt.Cell) {
	if t.Term.Flags&TermNoAM != 0 && t.cy == t.Sy-1 && t.cx == t.Sx-1 {
		return
	}
	if gc.Flags&vt.FlagPadding != 0 {
		return
	}
	gcp := t.checkCodeset(gc)
	t.Attributes(gcp)
	if len(gcp.Data) == 1 {
		if gcp.Data[0] < 0x20 || gcp.Data[0] == 0x7f {
			return
		}
		t.putc(gcp.Data[0])
		return
	}
	t.putn(gcp.Data, int(gcp.Width))
}

// fakeBCE reports whether clearing with a background colour must be done with spaces, as
// the terminal does not clear with the current background (tmux's tty_fake_bce).
func (t *TTY) fakeBCE(bg vt.Color) bool {
	if t.Term.Flag("bce") {
		return false
	}
	return !bg.IsDefault()
}

// clearCells clears nx cells from px on line py with the current attributes, with an escape
// sequence when one does it in fewer bytes (tmux's tty_draw_line_clear).
func (t *TTY) clearCells(px, py, nx int, bg vt.Color) {
	if nx == 0 {
		return
	}
	if nx >= 10 && !t.fakeBCE(bg) {
		if px+nx >= t.Sx && t.Term.Has("el") {
			t.Cursor(px, py)
			t.putcode("el")
			return
		}
		if px == 0 && t.Term.Has("el1") {
			t.Cursor(px+nx-1, py)
			t.putcode("el1")
			return
		}
		if t.Term.Has("ech") {
			t.Cursor(px, py)
			t.putcodeI("ech", nx)
			return
		}
	}
	t.Cursor(px, py)
	switch nx {
	case 1:
		t.putc(' ')
	case 2:
		t.putn("  ", 2)
	default:
		t.repeatSpace(nx)
	}
}

// emptyCells is how many cells from gc are blank and can be cleared rather than written
// (tmux's tty_draw_line_get_empty).
func emptyCells(gc, last *vt.Cell, nx int) int {
	switch {
	case int(gc.Width) > nx:
		return nx
	case gc.Flags&vt.FlagPadding != 0, gc.Width == 0:
		return 1
	case gc.Bg == last.Bg && gc.Attr == 0 && gc.Link == 0:
		switch {
		case gc.Flags&vt.FlagCleared != 0:
			return 1
		case gc.Flags&vt.FlagTab != 0:
			return int(gc.Width)
		case gc.Data == " ":
			return 1
		}
	}
	return 0
}

// DrawCells writes cells, a run of one line of the terminal starting at column atx of line
// aty, clearing blank stretches and writing the rest in runs of the same attributes (tmux's
// tty_draw_line, given final cells rather than a pane's screen). A cell is wide when its
// width is 2, and its second column is a padding cell.
func (t *TTY) DrawCells(cells []vt.Cell, atx, aty int) {
	nx := len(cells)
	if atx >= t.Sx {
		return
	}
	if atx+nx >= t.Sx {
		nx = t.Sx - atx
	}
	if nx == 0 {
		return
	}
	const (
		stateFirst = iota
		stateNew1
		stateNew2
		stateEmpty
		stateSame
		stateDone
	)
	last := vt.DefaultCell
	t.Attributes(&vt.DefaultCell)

	// Padding at the start is the end of a wide character cut off: clear it.
	cx := 0
	for cx < nx && cells[cx].Flags&vt.FlagPadding != 0 {
		cx++
	}
	if cx != 0 {
		t.Attributes(&last)
		t.clearCells(atx, aty, cx, last.Bg)
		if cx == nx {
			return
		}
		atx += cx
		cells = cells[cx:]
		nx -= cx
	}

	var buf strings.Builder
	width, lastI, i := 0, 0, 0
	current := stateFirst
	for {
		var next, empty int
		var gcp *vt.Cell
		if i == nx {
			next = stateDone
			gcp = &vt.DefaultCell
		} else {
			gc := &cells[i]
			empty = emptyCells(gc, &last, nx-i)
			if empty != 0 {
				gcp = gc
			} else {
				gcp = t.checkCodeset(gc)
			}
			switch {
			case empty != 0:
				next = stateEmpty
			case current == stateFirst:
				next = stateSame
			case vt.CellsLookEqual(gcp, &last):
				next = stateSame
			case current == stateNew1:
				next = stateNew2
			default:
				next = stateNew1
			}
		}
		if next != current {
			if current == stateEmpty {
				t.Attributes(&last)
				t.clearCells(atx+lastI, aty, i-lastI, last.Bg)
			} else if next != stateSame && buf.Len() != 0 {
				t.Attributes(&last)
				t.Cursor(atx+i-width, aty)
				if last.Attr&vt.AttrCharset == 0 {
					t.putn(buf.String(), width)
				} else {
					s := buf.String()
					for j := 0; j < len(s); j++ {
						t.putc(s[j])
					}
				}
				buf.Reset()
				width = 0
			}
			lastI = i
		}
		if next != stateEmpty {
			buf.WriteString(gcp.Data)
			width += int(gcp.Width)
		}
		if next == stateDone {
			break
		}
		current = next
		last = *gcp
		if empty != 0 {
			i += empty
		} else {
			i += max(1, int(gcp.Width))
		}
	}
}
