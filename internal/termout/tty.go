// Ported from tmux 3.8's tty.c:
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
	"bytes"
	"encoding/base64"
	"fmt"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/vt"
)

// TTY is what the server knows of an attached client's terminal and writes to it, as tmux's
// struct tty: the terminal's description, its size, and the state the terminal was last put
// in (cursor, scroll region, margins, modes), so that only changes are sent. Output collects in
// a buffer the caller sends to the client.
type TTY struct {
	Term   *Term
	Sx, Sy int
	UTF8   bool // the client is UTF-8 (tmux's CLIENT_UTF8)

	// ClearOnAttach is the clear-on-attach option (on by default).
	ClearOnAttach bool

	out     bytes.Buffer
	started bool

	cx, cy                         int // the cursor, or -1 when unknown
	rupper, rlower, rleft, rright  int // the scroll region and margins, or -1 when unknown
	mode                           int
	cstyle                         vt.CursorStyle
	ccolour                        vt.Color // the cursor colour sent, or -1
	haveDA, haveDA2, haveXDA, sync bool

	cell     vt.Cell // the attributes the terminal has (tmux's tty->cell)
	lastCell vt.Cell // the last cell Attributes was given, after fitting (tty->last_cell)
	syncing  bool    // inside SyncStart and SyncEnd
	links    LinkLookup
}

// unknown stands for tmux's UINT_MAX: a position or region the terminal has not been told.
const unknown = -1

// NewTTY returns the state of a client terminal that has not been started.
func NewTTY(term *Term, sx, sy int) *TTY {
	return &TTY{
		Term: term, Sx: sx, Sy: sy, ClearOnAttach: true, ccolour: -1,
		cx: unknown, cy: unknown, rupper: unknown, rlower: unknown, rleft: unknown, rright: unknown,
		cell: vt.DefaultCell, lastCell: vt.DefaultCell,
	}
}

// Take returns what has been written since the last Take.
func (t *TTY) Take() []byte {
	b := bytes.Clone(t.out.Bytes())
	t.out.Reset()
	return b
}

func (t *TTY) puts(s string) { t.out.WriteString(s) }

func (t *TTY) putcode(name string) { t.puts(t.Term.String(name)) }

func (t *TTY) putcodeI(name string, a int) {
	if s := t.Term.String(name); s != "" {
		t.puts(Tparm(s, a))
	}
}

func (t *TTY) putcodeII(name string, a, b int) {
	if s := t.Term.String(name); s != "" {
		t.puts(Tparm(s, a, b))
	}
}

func (t *TTY) putcodeS(name, a string) {
	if s := t.Term.String(name); s != "" {
		t.puts(Tparm(s, a))
	}
}

func (t *TTY) useMargin() bool { return t.Term.Flags&TermDECSLRM != 0 }

// acsNeeded is tmux's tty_acs_needed: line drawing uses the terminal's ACS unless the client
// is UTF-8 (or U8 is 0).
func (t *TTY) acsNeeded() bool {
	if t.Term.Has("U8") && t.Term.Number("U8") == 0 {
		return true
	}
	return !t.UTF8
}

// Start puts the terminal in the state tmux keeps it in while attached (tmux's tty_start_tty,
// less the termios changes, which the client makes).
func (t *TTY) Start() {
	if t.ClearOnAttach {
		t.putcode("smcup")
		t.putcode("clear")
	} else {
		t.putcodeII("csr", 0, t.Sy-1)
		t.putcodeII("cup", t.Sy-1, 0)
		switch {
		case t.Term.Has("indn"):
			t.putcodeI("indn", t.Sy+1)
		case t.Term.Has("ind"):
			for range t.Sy + 1 {
				t.putcode("ind")
			}
		default:
			t.putcode("clear")
		}
	}
	t.putcode("smkx")
	if t.acsNeeded() {
		t.putcode("enacs")
	}
	t.putcode("cnorm")
	if t.Term.Has("kmous") {
		t.puts("\033[?1000l\033[?1002l\033[?1003l")
		t.puts("\033[?1006l\033[?1005l")
	}
	if t.Term.Has("Enbp") {
		t.putcode("Enbp")
	}
	if t.Term.Flags&TermVT100Like != 0 {
		// Subscribe to theme changes and ask for the theme now.
		t.puts("\033[?2031h\033[?996n")
	}
	t.started = true
	t.Invalidate()
	if t.ccolour != -1 {
		t.forceCursorColour(-1)
	}
}

// SendRequests asks the terminal for what tmux asks once a command has attached it: its device
// attributes, version, synchronized output support and colours (tmux's tty_send_requests).
func (t *TTY) SendRequests() {
	if !t.started || t.Term.Flags&TermVT100Like == 0 {
		return
	}
	if !t.haveDA {
		t.puts("\033[c")
	}
	if !t.haveDA2 {
		t.puts("\033[>c")
	}
	if !t.haveXDA {
		t.puts("\033[>q")
	}
	if !t.sync {
		t.puts("\033[?2026$p")
	}
	t.puts("\033]10;?\033\\\033]11;?\033\\")
}

// UpdateFeatures applies features the terminal was found to have (from its replies) and puts
// it in the modes they allow, then forgets its state so that it is drawn again (tmux's
// tty_update_features); extendedKeys and focusEvents are those options.
func (t *TTY) UpdateFeatures(extendedKeys, focusEvents bool) {
	t.Term.Update()
	if t.useMargin() {
		t.putcode("Enmg")
	}
	if extendedKeys {
		t.puts(t.Term.String("Eneks"))
	}
	if focusEvents {
		t.puts(t.Term.String("Enfcs"))
	}
	t.puts(t.Term.String("Enesc"))
	t.Invalidate()
}

// RepeatRequests asks the terminal for its colours again (tmux's tty_repeat_requests, forced).
func (t *TTY) RepeatRequests() {
	if t.started && t.Term.Flags&TermVT100Like != 0 {
		t.puts("\033]10;?\033\\\033]11;?\033\\")
	}
}

// Stop gives the terminal back as tmux's tty_stop_tty does, for a terminal of rows lines.
func (t *TTY) Stop(rows int) {
	if !t.started {
		return
	}
	t.started = false
	t.puts(Tparm(t.Term.String("csr"), 0, rows-1))
	if t.acsNeeded() {
		t.putcode("rmacs")
	}
	t.putcode("sgr0")
	t.putcode("rmkx")
	if t.ClearOnAttach {
		t.putcode("clear")
	}
	if t.cstyle != vt.CursorDefault {
		if t.Term.Has("Se") {
			t.putcode("Se")
		} else if t.Term.Has("Ss") {
			t.putcodeI("Ss", 0)
		}
	}
	if t.ccolour != -1 {
		t.putcode("Cr")
	}
	t.putcode("cnorm")
	if t.Term.Has("kmous") {
		t.puts("\033[?1000l\033[?1002l\033[?1003l")
		t.puts("\033[?1006l\033[?1005l")
	}
	if t.Term.Has("Dsbp") {
		t.putcode("Dsbp")
	}
	t.putcode("Dsesc")
	t.putcode("Dsfcs")
	t.putcode("Dseks")
	if t.useMargin() {
		t.putcode("Dsmg")
	}
	if t.ClearOnAttach {
		t.putcode("rmcup")
	} else {
		t.putcode("clear")
	}
	if t.Term.Flags&TermVT100Like != 0 {
		t.puts("\033[?2031l")
	}
}

// Invalidate forgets what the terminal shows and resets it to a known state (tmux's
// tty_invalidate).
func (t *TTY) Invalidate() {
	t.cell, t.lastCell = vt.DefaultCell, vt.DefaultCell
	t.cx, t.cy = unknown, unknown
	t.rupper, t.rleft, t.rlower, t.rright = unknown, unknown, unknown, unknown
	if !t.started {
		t.mode = vt.ModeCursor
		return
	}
	if t.useMargin() {
		t.putcode("Enmg")
	}
	t.putcode("sgr0")
	t.mode = 0xffffff
	t.UpdateMode(vt.ModeCursor, nil)
	t.Cursor(0, 0)
	t.RegionOff()
	t.MarginOff()
}

// CursorState is what a screen says of the cursor, for UpdateMode; nil keeps the terminal's.
type CursorState struct {
	Style        vt.CursorStyle
	DefaultStyle vt.CursorStyle
	DefaultMode  int
	Colour       vt.Color // -1 for the default
}

// updateCursor sets the cursor's visibility, blinking and style (tmux's tty_update_cursor).
func (t *TTY) updateCursor(mode int, s *CursorState) int {
	cmode := mode
	if s != nil {
		t.forceCursorColour(s.Colour)
	}
	if cmode&vt.ModeCursor == 0 {
		if t.mode&vt.ModeCursor != 0 {
			t.putcode("civis")
		}
		return cmode
	}
	cstyle := t.cstyle
	if s != nil {
		cstyle = s.Style
		if cstyle == vt.CursorDefault {
			if cmode&vt.ModeCursorBlinkingSet == 0 {
				if s.DefaultMode&vt.ModeCursorBlinking != 0 {
					cmode |= vt.ModeCursorBlinking
				} else {
					cmode &^= vt.ModeCursorBlinking
				}
			}
			cstyle = s.DefaultStyle
		}
	}
	const cursorModes = vt.ModeCursor | vt.ModeCursorBlinking | vt.ModeCursorVeryVisible
	if (cmode^t.mode)&cursorModes == 0 && cstyle == t.cstyle {
		return cmode
	}
	t.putcode("cnorm")
	styled := func(blink, steady int) {
		switch {
		case t.Term.Has("Ss") && cmode&vt.ModeCursorBlinking != 0:
			t.putcodeI("Ss", blink)
		case t.Term.Has("Ss"):
			t.putcodeI("Ss", steady)
		case cmode&vt.ModeCursorBlinking != 0:
			t.putcode("cvvis")
		}
	}
	switch cstyle {
	case vt.CursorDefault:
		if t.cstyle != vt.CursorDefault {
			if t.Term.Has("Se") {
				t.putcode("Se")
			} else {
				t.putcodeI("Ss", 0)
			}
		}
		if cmode&(vt.ModeCursorBlinking|vt.ModeCursorVeryVisible) != 0 {
			t.putcode("cvvis")
		}
	case vt.CursorBlock:
		styled(1, 2)
	case vt.CursorUnderline:
		styled(3, 4)
	case vt.CursorBar:
		styled(5, 6)
	}
	t.cstyle = cstyle
	return cmode
}

// UpdateMode sends the changes between the terminal's modes and mode (tmux's
// tty_update_mode): the cursor, and the mouse modes.
func (t *TTY) UpdateMode(mode int, s *CursorState) {
	if t.updateCursor(mode, s)&vt.ModeCursorBlinking != 0 {
		mode |= vt.ModeCursorBlinking
	} else {
		mode &^= vt.ModeCursorBlinking
	}
	changed := mode ^ t.mode
	if changed&vt.AllMouseModes != 0 && t.Term.Has("kmous") {
		t.puts("\033[?1006l\033[?1000l\033[?1002l\033[?1003l")
		if mode&vt.AllMouseModes != 0 {
			t.puts("\033[?1006h")
		}
		switch {
		case mode&vt.ModeMouseAll != 0:
			t.puts("\033[?1000h\033[?1002h\033[?1003h")
		case mode&vt.ModeMouseButton != 0:
			t.puts("\033[?1000h\033[?1002h")
		case mode&vt.ModeMouseStandard != 0:
			t.puts("\033[?1000h")
		}
	}
	t.mode = mode
}

// forceCursorColour sets the cursor colour (-1 for the terminal's), as an rgb: spec (tmux's
// tty_force_cursor_colour).
func (t *TTY) forceCursorColour(c vt.Color) {
	if c != -1 {
		c = vt.ColorForceRGB(c)
	}
	if c == t.ccolour {
		return
	}
	if c == -1 {
		t.putcode("Cr")
	} else {
		r, g, b := vt.ColorSplitRGB(c)
		t.putcodeS("Cs", fmt.Sprintf("rgb:%02x/%02x/%02x", r, g, b))
	}
	t.ccolour = c
}

// Cursor moves the cursor to cx, cy with the cheapest sequence (tmux's tty_cursor).
func (t *TTY) Cursor(cx, cy int) {
	thisx, thisy := t.cx, t.cy
	if cx == thisx && cy == thisy && cx == t.Sx {
		return
	}
	if cx > t.Sx-1 {
		cx = t.Sx - 1
	}
	if cx == thisx && cy == thisy {
		return
	}
	done := func() { t.cx, t.cy = cx, cy }
	absolute := func() {
		t.putcodeII("cup", cy, cx)
		done()
	}
	// tmux's UINT_MAX is beyond every column: an unknown position moves absolutely.
	if thisx == unknown || thisx > t.Sx-1 {
		absolute()
		return
	}
	if cx == 0 && cy == 0 && t.Term.Has("home") {
		t.putcode("home")
		done()
		return
	}
	if cx == 0 && thisy != unknown && cy == thisy+1 && thisy != t.rlower && (!t.useMargin() || t.rleft == 0) {
		t.puts("\r\n")
		done()
		return
	}
	if cy == thisy {
		if cx == 0 && (!t.useMargin() || t.rleft == 0) {
			t.puts("\r")
			done()
			return
		}
		if cx == thisx-1 && t.Term.Has("cub1") {
			t.putcode("cub1")
			done()
			return
		}
		if cx == thisx+1 && t.Term.Has("cuf1") {
			t.putcode("cuf1")
			done()
			return
		}
		change := thisx - cx // +ve left, -ve right
		switch {
		case abs(change) > cx && t.Term.Has("hpa"):
			t.putcodeI("hpa", cx)
			done()
			return
		case change > 0 && t.Term.Has("cub") && !t.useMargin():
			if change == 2 && t.Term.Has("cub1") {
				t.putcode("cub1")
				t.putcode("cub1")
			} else {
				t.putcodeI("cub", change)
			}
			done()
			return
		case change < 0 && t.Term.Has("cuf") && !t.useMargin():
			t.putcodeI("cuf", -change)
			done()
			return
		}
	} else if cx == thisx && thisy != unknown {
		if thisy != t.rupper && cy == thisy-1 && t.Term.Has("cuu1") {
			t.putcode("cuu1")
			done()
			return
		}
		if thisy != t.rlower && cy == thisy+1 && t.Term.Has("cud1") {
			t.putcode("cud1")
			done()
			return
		}
		change := thisy - cy // +ve up, -ve down
		rlower, rupper := t.rlower, t.rupper
		if rlower == unknown {
			rlower = int(^uint(0) >> 1)
		}
		if abs(change) > cy || (change < 0 && cy-change > rlower) || (change > 0 && cy-change < rupper) {
			if t.Term.Has("vpa") {
				t.putcodeI("vpa", cy)
				done()
				return
			}
		} else if change > 0 && t.Term.Has("cuu") {
			t.putcodeI("cuu", change)
			done()
			return
		} else if change < 0 && t.Term.Has("cud") {
			t.putcodeI("cud", -change)
			done()
			return
		}
	}
	absolute()
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// RegionOff sets the scroll region to the whole terminal.
func (t *TTY) RegionOff() { t.Region(0, t.Sy-1) }

// Region sets the scroll region (tmux's tty_region).
func (t *TTY) Region(rupper, rlower int) {
	if t.rlower == rlower && t.rupper == rupper {
		return
	}
	if !t.Term.Has("csr") {
		return
	}
	t.rupper, t.rlower = rupper, rlower
	// Some terminals do not reset their wrap flag: move to the left edge first.
	if t.cx == unknown || t.cx >= t.Sx {
		if t.cy == unknown {
			t.Cursor(0, 0)
		} else {
			t.Cursor(0, t.cy)
		}
	}
	t.putcodeII("csr", t.rupper, t.rlower)
	t.cx, t.cy = unknown, unknown
}

// MarginOff sets the left and right margins to the whole terminal.
func (t *TTY) MarginOff() { t.Margin(0, t.Sx-1) }

// Margin sets the left and right margins where the terminal has them (tmux's tty_margin).
func (t *TTY) Margin(rleft, rright int) {
	if !t.useMargin() || (t.rleft == rleft && t.rright == rright) {
		return
	}
	t.putcodeII("csr", t.rupper, t.rlower)
	t.rleft, t.rright = rleft, rright
	if rleft == 0 && rright == t.Sx-1 {
		t.putcode("Clmg")
	} else {
		t.putcodeII("Cmg", rleft, rright)
	}
	t.cx, t.cy = unknown, unknown
}

// SetSelection is tty_set_selection: data given to the terminal's clipboard clip (OSC 52, the
// Ms capability), base64 encoded; nothing for a terminal without Ms or not started.
func (t *TTY) SetSelection(clip string, data []byte) {
	if !t.started || !t.Term.Has("Ms") {
		return
	}
	t.putcodeSS("Ms", clip, base64.StdEncoding.EncodeToString(data))
}
