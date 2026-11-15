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

import "bytes"

// Terminal is a pane's emulator: the parser, tmux's input.c handlers and the screen. Output a
// program writes goes into Write; the screen holds the result.
//
// The handlers are ported from tmux 3.8's input.c.
type Terminal struct {
	Screen *Screen
	parser *Parser
	in     input
}

// inputCell is the current style and character sets (tmux's input_cell).
type inputCell struct {
	cell  Cell
	set   int // 0: G0, 1: G1 (SO and SI switch)
	g0set int // 1: the DEC line drawing set
	g1set int
}

// input is the state tmux's input_ctx keeps besides the parser's.
type input struct {
	t    *Terminal
	cell inputCell

	old          inputCell
	oldCx, oldCy int
	oldMode      int
	last         string // the last character printed, for REP
	lastWidth    uint8
	lastValid    bool
}

// NewTerminal returns an emulator with a sx×sy screen keeping hlimit lines of history.
func NewTerminal(sx, sy, hlimit int) *Terminal {
	t := &Terminal{Screen: NewScreen(sx, sy, hlimit)}
	t.in.t = t
	t.in.resetCell()
	t.parser = NewParser(&t.in)
	return t
}

// Write feeds output from the program. It never fails.
func (t *Terminal) Write(b []byte) (int, error) { return t.parser.Write(b) }

func (in *input) resetCell() {
	in.cell = inputCell{cell: DefaultCell}
	in.old = in.cell
	in.oldCx, in.oldCy = 0, 0
}

func (in *input) saveState() {
	s := in.t.Screen
	in.old = in.cell
	in.oldCx = s.Cx
	in.oldCy = s.Cy
	in.oldMode = s.Mode
}

func (in *input) restoreState() {
	s := in.t.Screen
	in.cell = in.old
	if in.oldMode&ModeOrigin != 0 {
		s.Mode |= ModeOrigin
	} else {
		s.Mode &^= ModeOrigin
	}
	s.cursorMove(in.oldCx, in.oldCy, false)
}

// Print writes printable ASCII (tmux's input_print, collected).
func (in *input) Print(run []byte) {
	s := in.t.Screen
	set := in.cell.g0set
	if in.cell.set != 0 {
		set = in.cell.g1set
	}
	if set == 1 {
		in.cell.cell.Attr |= AttrCharset
	} else {
		in.cell.cell.Attr &^= AttrCharset
	}
	gc := in.cell.cell
	gc.Width = 1
	gc.Data = " "
	s.writeASCII(&gc, run)
	in.last = asciiString(run[len(run)-1])
	in.lastWidth = 1
	in.lastValid = true
	in.cell.cell.Attr &^= AttrCharset
}

// PrintUTF8 writes one UTF-8 character (tmux's input_top_bit_set).
func (in *input) PrintUTF8(seq []byte, invalid bool) {
	gc := in.cell.cell
	r, ok := DecodeChar(seq)
	if invalid || !ok {
		gc.Data, gc.Width = replacementCh, 1
	} else {
		gc.Data, gc.Width = string(seq), uint8(RuneWidth(r))
	}
	in.t.Screen.writeCell(&gc)
	if invalid {
		in.lastValid = false
		return
	}
	in.last = gc.Data
	in.lastWidth = gc.Width
	in.lastValid = true
}

// Execute runs a C0 control (tmux's input_c0_dispatch).
func (in *input) Execute(c byte) {
	s := in.t.Screen
	switch c {
	case 0x08: // BS
		s.backspace()
	case 0x09: // HT
		in.tab()
	case 0x0a, 0x0b, 0x0c: // LF, VT, FF
		s.linefeed(false, in.cell.cell.Bg)
		if s.Mode&ModeCRLF != 0 {
			s.carriageReturn()
		}
	case 0x0d: // CR
		s.carriageReturn()
	case 0x0e: // SO
		in.cell.set = 1
	case 0x0f: // SI
		in.cell.set = 0
	}
	in.lastValid = false
}

// tab moves to the next tab stop. Over blank cells all in one style it writes a tab cell, so
// the tab is kept (copy mode and capture-pane give it back), as tmux 3.4 and later do.
func (in *input) tab() {
	s := in.t.Screen
	cx := s.Cx
	if cx >= s.SizeX()-1 {
		return
	}
	line := s.Cy + s.Grid.HSize
	first := s.Grid.Cell(cx, line)
	hasContent := false
	for {
		if !hasContent {
			gc := s.Grid.Cell(cx, line)
			if gc.Data != " " || !CellsLookEqual(&gc, &first) {
				hasContent = true
			}
		}
		cx++
		if s.IsTab(cx) || cx >= s.SizeX()-1 {
			break
		}
	}
	width := cx - s.Cx
	if hasContent || width > maxCellDataLength {
		s.Cx = cx
		return
	}
	gc := s.Grid.Cell(s.Cx, line)
	gc.SetTab(width)
	s.writeCell(&gc)
}

func (in *input) backTab(p *Params) {
	s := in.t.Screen
	cx := s.Cx
	if cx > s.SizeX()-1 {
		cx = s.SizeX() - 1
	}
	n := p.Get(0, 1, 1)
	if n == -1 {
		return
	}
	for cx > 0 && n > 0 {
		n--
		for {
			cx--
			if cx == 0 || s.IsTab(cx) {
				break
			}
		}
	}
	s.Cx = cx
}

func (in *input) repeatLast(p *Params) {
	s := in.t.Screen
	n := p.Get(0, 1, 1)
	if n == -1 {
		return
	}
	if m := s.SizeX() - s.Cx; n > m {
		n = m
	}
	if !in.lastValid || in.t.parser.utf8On {
		return
	}
	set := in.cell.g0set
	if in.cell.set != 0 {
		set = in.cell.g1set
	}
	if set == 1 {
		in.cell.cell.Attr |= AttrCharset
	} else {
		in.cell.cell.Attr &^= AttrCharset
	}
	if len(in.last) == 1 && in.last[0] < 0x7f {
		gc := in.cell.cell
		gc.Data, gc.Width = " ", 1
		s.writeASCII(&gc, bytes.Repeat([]byte{in.last[0]}, n))
		return
	}
	gc := in.cell.cell
	gc.Data, gc.Width = in.last, in.lastWidth
	for i := 0; i < n; i++ {
		s.writeCell(&gc)
	}
}

// EscDispatch runs an ESC sequence (tmux's input_esc_dispatch).
func (in *input) EscDispatch(inter []byte, final byte) {
	s := in.t.Screen
	switch string(inter) {
	case "":
		switch final {
		case '7': // DECSC
			in.saveState()
		case '8': // DECRC
			in.restoreState()
		case '=': // DECKPAM
			s.Mode |= ModeKKeypad
		case '>': // DECKPNM
			s.Mode &^= ModeKKeypad
		case 'D': // IND
			s.linefeed(false, in.cell.cell.Bg)
		case 'E': // NEL
			s.carriageReturn()
			s.linefeed(false, in.cell.cell.Bg)
		case 'H': // HTS
			if s.Cx < s.SizeX() {
				s.SetTab(s.Cx, true)
			}
		case 'M': // RI
			s.reverseIndex(in.cell.cell.Bg)
		case '\\': // ST
		case 'c': // RIS
			in.resetCell()
			s.reset()
		default:
			return
		}
	case "#":
		if final != '8' { // DECALN
			return
		}
		s.alignmentTest()
	case "(":
		switch final {
		case '0':
			in.cell.g0set = 1
		case 'B':
			in.cell.g0set = 0
		default:
			return
		}
	case ")":
		switch final {
		case '0':
			in.cell.g1set = 1
		case 'B':
			in.cell.g1set = 0
		default:
			return
		}
	default:
		return
	}
	in.lastValid = false
}

// CsiDispatch runs a CSI sequence (tmux's input_csi_dispatch).
func (in *input) CsiDispatch(p *Params, inter []byte, final byte) {
	s := in.t.Screen
	bg := in.cell.cell.Bg
	switch string(inter) {
	case "":
		switch final {
		case '@': // ICH
			if n := p.Get(0, 1, 1); n != -1 {
				s.insertCharacter(n, bg)
			}
		case 'A': // CUU
			if n := p.Get(0, 1, 1); n != -1 {
				s.cursorUp(n)
			}
		case 'B': // CUD
			if n := p.Get(0, 1, 1); n != -1 {
				s.cursorDown(n)
			}
		case 'C': // CUF
			if n := p.Get(0, 1, 1); n != -1 {
				s.cursorRight(n)
			}
		case 'D': // CUB
			if n := p.Get(0, 1, 1); n != -1 {
				s.cursorLeft(n)
			}
		case 'E': // CNL
			if n := p.Get(0, 1, 1); n != -1 {
				s.carriageReturn()
				s.cursorDown(n)
			}
		case 'F': // CPL
			if n := p.Get(0, 1, 1); n != -1 {
				s.carriageReturn()
				s.cursorUp(n)
			}
		case 'G', '`': // CHA / HPA
			if n := p.Get(0, 1, 1); n != -1 {
				s.cursorMove(n-1, -1, true)
			}
		case 'H', 'f': // CUP / HVP
			n := p.Get(0, 1, 1)
			m := p.Get(1, 1, 1)
			if n != -1 && m != -1 {
				s.cursorMove(m-1, n-1, true)
			}
		case 'J': // ED
			switch p.Get(0, 0, 0) {
			case 0:
				s.clearEndOfScreen(bg)
			case 1:
				s.clearStartOfScreen(bg)
			case 2:
				s.clearScreen(bg)
			case 3:
				if p.Get(1, 0, 0) == 0 {
					s.Grid.ClearHistory()
				}
			}
		case 'K': // EL
			switch p.Get(0, 0, 0) {
			case 0:
				s.clearEndOfLine(bg)
			case 1:
				s.clearStartOfLine(bg)
			case 2:
				s.clearLine(bg)
			}
		case 'L': // IL
			if n := p.Get(0, 1, 1); n != -1 {
				s.insertLine(n, bg)
			}
		case 'M': // DL
			if n := p.Get(0, 1, 1); n != -1 {
				s.deleteLine(n, bg)
			}
		case 'P': // DCH
			if n := p.Get(0, 1, 1); n != -1 {
				s.deleteCharacter(n, bg)
			}
		case 'S': // SU
			if n := p.Get(0, 1, 1); n != -1 {
				s.scrollUp(n, bg)
			}
		case 'T': // SD
			if n := p.Get(0, 1, 1); n != -1 {
				s.scrollDown(n, bg)
			}
		case 'X': // ECH
			if n := p.Get(0, 1, 1); n != -1 {
				s.clearCharacter(n, bg)
			}
		case 'Z': // CBT
			in.backTab(p)
		case 'b': // REP
			in.repeatLast(p)
		case 'c': // DA (C1.8)
		case 'd': // VPA
			if n := p.Get(0, 1, 1); n != -1 {
				s.cursorMove(-1, n-1, true)
			}
		case 'g': // TBC
			switch p.Get(0, 0, 0) {
			case 0:
				if s.Cx < s.SizeX() {
					s.SetTab(s.Cx, false)
				}
			case 3:
				s.ClearTabs()
			}
		case 'h': // SM
			in.csiSM(p)
		case 'l': // RM
			in.csiRM(p)
		case 'm': // SGR
			in.csiSGR(p)
		case 'n': // DSR (C1.8)
		case 'r': // DECSTBM
			n := p.Get(0, 1, 1)
			m := p.Get(1, 1, s.SizeY())
			if n != -1 && m != -1 {
				s.scrollRegion(n-1, m-1)
			}
		case 's': // SCP
			in.saveState()
		case 't': // WINOPS (C1.7/C1.8)
		case 'u': // RCP
			in.restoreState()
		default:
			return
		}
	case "?":
		switch final {
		case 'S': // SM_GRAPHICS
		case 'h': // DECSET
			in.csiSMPrivate(p)
		case 'l': // DECRST
			in.csiRMPrivate(p)
		case 'n': // DSR_PRIVATE
		default:
			return
		}
	case ">":
		switch final {
		case 'c', 'm', 'n', 'q':
		default:
			return
		}
	case " ":
		if final != 'q' { // DECSCUSR
			return
		}
		if n := p.Get(0, 0, 0); n != -1 {
			s.SetCursorStyle(n)
			if n == 0 {
				s.Mode &^= ModeCursorBlinkingSet
			}
		}
	case "$", "?$":
		if final != 'p' { // DECRQM (C1.6)
			return
		}
	default:
		return
	}
	in.lastValid = false
}

func (in *input) csiSM(p *Params) {
	s := in.t.Screen
	for i := 0; i < p.Len(); i++ {
		switch p.Get(i, 0, -1) {
		case 4: // IRM
			s.Mode |= ModeInsert
		case 34:
			s.Mode &^= ModeCursorVeryVisible
		}
	}
}

func (in *input) csiRM(p *Params) {
	s := in.t.Screen
	for i := 0; i < p.Len(); i++ {
		switch p.Get(i, 0, -1) {
		case 4: // IRM
			s.Mode &^= ModeInsert
		case 34:
			s.Mode |= ModeCursorVeryVisible
		}
	}
}

func (in *input) csiSMPrivate(p *Params) {
	s := in.t.Screen
	for i := 0; i < p.Len(); i++ {
		switch p.Get(i, 0, -1) {
		case 3: // DECCOLM
			s.cursorMove(0, 0, true)
			s.clearScreen(in.cell.cell.Bg)
		case 6: // DECOM
			s.Mode |= ModeOrigin
			s.cursorMove(0, 0, true)
		case 7: // DECAWM
			s.Mode |= ModeWrap
		}
	}
}

func (in *input) csiRMPrivate(p *Params) {
	s := in.t.Screen
	for i := 0; i < p.Len(); i++ {
		switch p.Get(i, 0, -1) {
		case 3: // DECCOLM
			s.cursorMove(0, 0, true)
			s.clearScreen(in.cell.cell.Bg)
		case 6: // DECOM
			s.Mode &^= ModeOrigin
			s.cursorMove(0, 0, true)
		case 7: // DECAWM
			s.Mode &^= ModeWrap
		}
	}
}

func (in *input) csiSGR256Do(fgbg, c int) bool {
	gc := &in.cell.cell
	if c == -1 || c > 255 {
		switch fgbg {
		case 38:
			gc.Fg = ColorDefault
		case 48:
			gc.Bg = ColorDefault
		}
	} else {
		switch fgbg {
		case 38:
			gc.Fg = Color(c) | ColorFlag256
		case 48:
			gc.Bg = Color(c) | ColorFlag256
		case 58:
			gc.Us = Color(c) | ColorFlag256
		}
	}
	return true
}

func (in *input) csiSGR256(p *Params, fgbg int, i *int) {
	c := p.Get(*i+1, 0, -1)
	if in.csiSGR256Do(fgbg, c) {
		*i++
	}
}

func (in *input) csiSGRRGBDo(fgbg, r, g, b int) bool {
	if r == -1 || r > 255 || g == -1 || g > 255 || b == -1 || b > 255 {
		return false
	}
	gc := &in.cell.cell
	switch fgbg {
	case 38:
		gc.Fg = ColorJoinRGB(r, g, b)
	case 48:
		gc.Bg = ColorJoinRGB(r, g, b)
	case 58:
		gc.Us = ColorJoinRGB(r, g, b)
	}
	return true
}

func (in *input) csiSGRRGB(p *Params, fgbg int, i *int) {
	r := p.Get(*i+1, 0, -1)
	g := p.Get(*i+2, 0, -1)
	b := p.Get(*i+3, 0, -1)
	if in.csiSGRRGBDo(fgbg, r, g, b) {
		*i += 3
	}
}

func (in *input) csiSGRColon(s []byte) {
	p := [8]int{-1, -1, -1, -1, -1, -1, -1, -1}
	n := 0
	start := 0
	for j := 0; j <= len(s); j++ {
		if j < len(s) && s[j] != ':' {
			continue
		}
		out := s[start:j]
		start = j + 1
		if len(out) != 0 {
			v, ok := atoi(out)
			if !ok {
				return
			}
			p[n] = v
			n++
			if n == len(p) {
				return
			}
		} else {
			n++
			if n == len(p) {
				return
			}
		}
	}
	if n == 0 {
		return
	}
	gc := &in.cell.cell
	if p[0] == 4 {
		if n != 2 {
			return
		}
		switch p[1] {
		case 0:
			gc.Attr &^= AttrAllUnderscore
		case 1:
			gc.Attr &^= AttrAllUnderscore
			gc.Attr |= AttrUnderscore
		case 2:
			gc.Attr &^= AttrAllUnderscore
			gc.Attr |= AttrUnderscore2
		case 3:
			gc.Attr &^= AttrAllUnderscore
			gc.Attr |= AttrUnderscore3
		case 4:
			gc.Attr &^= AttrAllUnderscore
			gc.Attr |= AttrUnderscore4
		case 5:
			gc.Attr &^= AttrAllUnderscore
			gc.Attr |= AttrUnderscore5
		}
		return
	}
	if n < 2 || (p[0] != 38 && p[0] != 48 && p[0] != 58) {
		return
	}
	switch p[1] {
	case 2:
		if n < 3 {
			break
		}
		idx := 3
		if n == 5 {
			idx = 2
		}
		if n < idx+3 {
			break
		}
		in.csiSGRRGBDo(p[0], p[idx], p[idx+1], p[idx+2])
	case 5:
		if n < 3 {
			break
		}
		in.csiSGR256Do(p[0], p[2])
	}
}

func (in *input) csiSGR(p *Params) {
	gc := &in.cell.cell
	if p.Len() == 0 {
		*gc = DefaultCell
		return
	}
	for i := 0; i < p.Len(); i++ {
		if p.Kind(i) == ParamString {
			in.csiSGRColon(p.String(i))
			continue
		}
		n := p.Get(i, 0, 0)
		if n == -1 {
			continue
		}
		if n == 38 || n == 48 || n == 58 {
			i++
			switch p.Get(i, 0, -1) {
			case 2:
				in.csiSGRRGB(p, n, &i)
			case 5:
				in.csiSGR256(p, n, &i)
			}
			continue
		}
		switch n {
		case 0:
			link := gc.Link
			*gc = DefaultCell
			gc.Link = link
		case 1:
			gc.Attr |= AttrBright
		case 2:
			gc.Attr |= AttrDim
		case 3:
			gc.Attr |= AttrItalics
		case 4:
			gc.Attr &^= AttrAllUnderscore
			gc.Attr |= AttrUnderscore
		case 5, 6:
			gc.Attr |= AttrBlink
		case 7:
			gc.Attr |= AttrReverse
		case 8:
			gc.Attr |= AttrHidden
		case 9:
			gc.Attr |= AttrStrikethrough
		case 21:
			gc.Attr &^= AttrAllUnderscore
			gc.Attr |= AttrUnderscore2
		case 22:
			gc.Attr &^= AttrBright | AttrDim
		case 23:
			gc.Attr &^= AttrItalics
		case 24:
			gc.Attr &^= AttrAllUnderscore
		case 25:
			gc.Attr &^= AttrBlink
		case 27:
			gc.Attr &^= AttrReverse
		case 28:
			gc.Attr &^= AttrHidden
		case 29:
			gc.Attr &^= AttrStrikethrough
		case 30, 31, 32, 33, 34, 35, 36, 37:
			gc.Fg = Color(n - 30)
		case 39:
			gc.Fg = ColorDefault
		case 40, 41, 42, 43, 44, 45, 46, 47:
			gc.Bg = Color(n - 40)
		case 49:
			gc.Bg = ColorDefault
		case 53:
			gc.Attr |= AttrOverline
		case 55:
			gc.Attr &^= AttrOverline
		case 59:
			gc.Us = ColorDefault
		case 90, 91, 92, 93, 94, 95, 96, 97:
			gc.Fg = Color(n)
		case 100, 101, 102, 103, 104, 105, 106, 107:
			gc.Bg = Color(n - 10)
		}
	}
}

func (in *input) OscDispatch(data []byte, bel bool)                { in.lastValid = false }
func (in *input) DcsDispatch(p *Params, inter []byte, data []byte) { in.lastValid = false }
func (in *input) ApcDispatch(data []byte)                          { in.lastValid = false }
func (in *input) RenameDispatch(data []byte)                       { in.lastValid = false }
