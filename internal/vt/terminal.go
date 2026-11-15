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
	last         string // the last character printed, for REP
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

// Print writes printable ASCII (tmux's input_print, collected).
func (in *input) Print(run []byte) {
	s := in.t.Screen
	set := in.cell.g0set
	if in.cell.set != 0 {
		set = in.cell.g1set
	}
	gc := in.cell.cell
	if set == 1 {
		gc.Attr |= AttrCharset
	} else {
		gc.Attr &^= AttrCharset
	}
	gc.Width = 1
	gc.Data = " "
	s.writeASCII(&gc, run)
	in.last = asciiString(run[len(run)-1])
	in.lastValid = true
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

// The sequences come with the following tasks (C1.4 to C1.8).
func (in *input) EscDispatch(inter []byte, final byte)             {}
func (in *input) CsiDispatch(p *Params, inter []byte, final byte)  {}
func (in *input) OscDispatch(data []byte, bel bool)                {}
func (in *input) DcsDispatch(p *Params, inter []byte, data []byte) {}
func (in *input) ApcDispatch(data []byte)                          {}
func (in *input) RenameDispatch(data []byte)                       {}
