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

package vt

import "time"

// The parser is ported from tmux 3.8's input.c: the same states, the same transition tables,
// the same limits and the same UTF-8 handling, so that malformed input is taken apart the way
// tmux takes it apart. It only splits bytes into actions; what they do is the Handler's job.
//
// C1 controls: only their 7-bit forms (ESC P, ESC [, ESC ], ESC X, ESC ^, ESC _, ESC \) are
// controls. As in tmux, bytes 0x80 to 0x9f in the ground state start UTF-8 (and, not being lead
// bytes, print U+FFFD); inside strings they are data.

// Handler receives the parser's actions.
type Handler interface {
	// Print gets a run of printable ASCII bytes (0x20 to 0x7e). The slice is only valid
	// during the call.
	Print(run []byte)
	// PrintUTF8 gets one complete UTF-8 sequence, well-formed in its structure. invalid is
	// set when tmux would print U+FFFD instead: a bad lead byte, a sequence broken by a byte
	// that is not a continuation, or one cut short by a printable ASCII byte.
	PrintUTF8(seq []byte, invalid bool)
	// Execute gets a C0 control (0x00 to 0x1f, except ESC).
	Execute(c byte)
	// EscDispatch gets ESC [intermediates] final.
	EscDispatch(inter []byte, final byte)
	// CsiDispatch gets CSI params [intermediates] final. Private markers (< = > ?) given
	// before the parameters are in inter, first, as tmux keeps them.
	CsiDispatch(p *Params, inter []byte, final byte)
	// OscDispatch gets an OSC string; bel is set when it ended with BEL rather than ST.
	OscDispatch(data []byte, bel bool)
	// DcsDispatch gets a DCS string: its intermediates, its parameters, and its data, which
	// starts with the DCS's final byte.
	DcsDispatch(p *Params, inter []byte, data []byte)
	// ApcDispatch gets an APC string.
	ApcDispatch(data []byte)
	// RenameDispatch gets a screen-style rename string (ESC k … ST).
	RenameDispatch(data []byte)
}

type state uint8

const (
	stGround state = iota
	stEscEnter
	stEscIntermediate
	stCsiEnter
	stCsiParameter
	stCsiIntermediate
	stCsiIgnore
	stDcsEnter
	stDcsParameter
	stDcsIntermediate
	stDcsHandler
	stDcsEscape
	stDcsIgnore
	stOscString
	stApcString
	stRenameString
	stConsumeST
	numStates
)

var stateNames = [numStates]string{
	"ground", "esc_enter", "esc_intermediate", "csi_enter", "csi_parameter", "csi_intermediate",
	"csi_ignore", "dcs_enter", "dcs_parameter", "dcs_intermediate", "dcs_handler", "dcs_escape",
	"dcs_ignore", "osc_string", "apc_string", "rename_string", "consume_st",
}

func (s state) String() string { return stateNames[s] }

type action uint8

const (
	acNone action = iota
	acC0
	acPrint
	acTopBit
	acIntermediate
	acParameter
	acEscDispatch
	acCsiDispatch
	acInput
	acEndBel
	acDcsDispatch
)

// noChange marks a transition that stays in its state.
const noChange = numStates

type transition struct {
	first, last byte
	act         action
	next        state
}

// anywhere are the transitions every state but dcs_handler and dcs_escape has.
var anywhere = []transition{
	{0x18, 0x18, acC0, stGround},
	{0x1a, 0x1a, acC0, stGround},
	{0x1b, 0x1b, acNone, stEscEnter},
}

// tables are tmux's input_state_*_table, in its order: the first range that matches wins.
var tables = [numStates][]transition{
	stGround: {
		{0x00, 0x17, acC0, noChange},
		{0x19, 0x19, acC0, noChange},
		{0x1c, 0x1f, acC0, noChange},
		{0x20, 0x7e, acPrint, noChange},
		{0x7f, 0x7f, acNone, noChange},
		{0x80, 0xff, acTopBit, noChange},
	},
	stEscEnter: {
		{0x00, 0x17, acC0, noChange},
		{0x19, 0x19, acC0, noChange},
		{0x1c, 0x1f, acC0, noChange},
		{0x20, 0x2f, acIntermediate, stEscIntermediate},
		{0x30, 0x4f, acEscDispatch, stGround},
		{0x50, 0x50, acNone, stDcsEnter},
		{0x51, 0x57, acEscDispatch, stGround},
		{0x58, 0x58, acNone, stConsumeST},
		{0x59, 0x59, acEscDispatch, stGround},
		{0x5a, 0x5a, acEscDispatch, stGround},
		{0x5b, 0x5b, acNone, stCsiEnter},
		{0x5c, 0x5c, acEscDispatch, stGround},
		{0x5d, 0x5d, acNone, stOscString},
		{0x5e, 0x5e, acNone, stConsumeST},
		{0x5f, 0x5f, acNone, stApcString},
		{0x60, 0x6a, acEscDispatch, stGround},
		{0x6b, 0x6b, acNone, stRenameString},
		{0x6c, 0x7e, acEscDispatch, stGround},
		{0x7f, 0xff, acNone, noChange},
	},
	stEscIntermediate: {
		{0x00, 0x17, acC0, noChange},
		{0x19, 0x19, acC0, noChange},
		{0x1c, 0x1f, acC0, noChange},
		{0x20, 0x2f, acIntermediate, noChange},
		{0x30, 0x7e, acEscDispatch, stGround},
		{0x7f, 0xff, acNone, noChange},
	},
	stCsiEnter: {
		{0x00, 0x17, acC0, noChange},
		{0x19, 0x19, acC0, noChange},
		{0x1c, 0x1f, acC0, noChange},
		{0x20, 0x2f, acIntermediate, stCsiIntermediate},
		{0x30, 0x39, acParameter, stCsiParameter},
		{0x3a, 0x3a, acParameter, stCsiParameter},
		{0x3b, 0x3b, acParameter, stCsiParameter},
		{0x3c, 0x3f, acIntermediate, stCsiParameter},
		{0x40, 0x7e, acCsiDispatch, stGround},
		{0x7f, 0xff, acNone, noChange},
	},
	stCsiParameter: {
		{0x00, 0x17, acC0, noChange},
		{0x19, 0x19, acC0, noChange},
		{0x1c, 0x1f, acC0, noChange},
		{0x20, 0x2f, acIntermediate, stCsiIntermediate},
		{0x30, 0x39, acParameter, noChange},
		{0x3a, 0x3a, acParameter, noChange},
		{0x3b, 0x3b, acParameter, noChange},
		{0x3c, 0x3f, acNone, stCsiIgnore},
		{0x40, 0x7e, acCsiDispatch, stGround},
		{0x7f, 0xff, acNone, noChange},
	},
	stCsiIntermediate: {
		{0x00, 0x17, acC0, noChange},
		{0x19, 0x19, acC0, noChange},
		{0x1c, 0x1f, acC0, noChange},
		{0x20, 0x2f, acIntermediate, noChange},
		{0x30, 0x3f, acNone, stCsiIgnore},
		{0x40, 0x7e, acCsiDispatch, stGround},
		{0x7f, 0xff, acNone, noChange},
	},
	stCsiIgnore: {
		{0x00, 0x17, acC0, noChange},
		{0x19, 0x19, acC0, noChange},
		{0x1c, 0x1f, acC0, noChange},
		{0x20, 0x3f, acNone, noChange},
		{0x40, 0x7e, acNone, stGround},
		{0x7f, 0xff, acNone, noChange},
	},
	stDcsEnter: {
		{0x00, 0x17, acNone, noChange},
		{0x19, 0x19, acNone, noChange},
		{0x1c, 0x1f, acNone, noChange},
		{0x20, 0x2f, acIntermediate, stDcsIntermediate},
		{0x30, 0x39, acParameter, stDcsParameter},
		{0x3a, 0x3a, acNone, stDcsIgnore},
		{0x3b, 0x3b, acParameter, stDcsParameter},
		{0x3c, 0x3f, acIntermediate, stDcsParameter},
		{0x40, 0x7e, acInput, stDcsHandler},
		{0x7f, 0xff, acNone, noChange},
	},
	stDcsParameter: {
		{0x00, 0x17, acNone, noChange},
		{0x19, 0x19, acNone, noChange},
		{0x1c, 0x1f, acNone, noChange},
		{0x20, 0x2f, acIntermediate, stDcsIntermediate},
		{0x30, 0x39, acParameter, noChange},
		{0x3a, 0x3a, acNone, stDcsIgnore},
		{0x3b, 0x3b, acParameter, noChange},
		{0x3c, 0x3f, acNone, stDcsIgnore},
		{0x40, 0x7e, acInput, stDcsHandler},
		{0x7f, 0xff, acNone, noChange},
	},
	stDcsIntermediate: {
		{0x00, 0x17, acNone, noChange},
		{0x19, 0x19, acNone, noChange},
		{0x1c, 0x1f, acNone, noChange},
		{0x20, 0x2f, acIntermediate, noChange},
		{0x30, 0x3f, acNone, stDcsIgnore},
		{0x40, 0x7e, acInput, stDcsHandler},
		{0x7f, 0xff, acNone, noChange},
	},
	stDcsHandler: {
		{0x00, 0x1a, acInput, noChange},
		{0x1b, 0x1b, acNone, stDcsEscape},
		{0x1c, 0xff, acInput, noChange},
	},
	stDcsEscape: {
		{0x00, 0x5b, acInput, stDcsHandler},
		{0x5c, 0x5c, acDcsDispatch, stGround},
		{0x5d, 0xff, acInput, stDcsHandler},
	},
	stDcsIgnore: {
		{0x00, 0x17, acNone, noChange},
		{0x19, 0x19, acNone, noChange},
		{0x1c, 0x1f, acNone, noChange},
		{0x20, 0xff, acNone, noChange},
	},
	stOscString: {
		{0x00, 0x06, acNone, noChange},
		{0x07, 0x07, acEndBel, stGround},
		{0x08, 0x17, acNone, noChange},
		{0x19, 0x19, acNone, noChange},
		{0x1c, 0x1f, acNone, noChange},
		{0x20, 0xff, acInput, noChange},
	},
	stApcString: {
		{0x00, 0x17, acNone, noChange},
		{0x19, 0x19, acNone, noChange},
		{0x1c, 0x1f, acNone, noChange},
		{0x20, 0xff, acInput, noChange},
	},
	stRenameString: {
		{0x00, 0x17, acNone, noChange},
		{0x19, 0x19, acNone, noChange},
		{0x1c, 0x1f, acNone, noChange},
		{0x20, 0xff, acInput, noChange},
	},
	stConsumeST: {
		{0x00, 0x17, acNone, noChange},
		{0x19, 0x19, acNone, noChange},
		{0x1c, 0x1f, acNone, noChange},
		{0x20, 0xff, acNone, noChange},
	},
}

// lookup is tables flattened: lookup[state][byte].
var lookup [numStates][256]transition

func init() {
	for s := range numStates {
		t := tables[s]
		if s != stDcsHandler && s != stDcsEscape {
			t = append(append([]transition{}, anywhere...), t...)
		}
		for c := 0; c < 256; c++ {
			found := false
			for _, tr := range t {
				if byte(c) >= tr.first && byte(c) <= tr.last {
					lookup[s][c] = tr
					found = true
					break
				}
			}
			if !found {
				panic("vt: no transition from state " + s.String())
			}
		}
	}
}

// Limits, as in tmux's input_ctx.
const (
	maxIntermediate = 3       // tmux: a 4-byte intermediate buffer
	maxParamBytes   = 63      // param_buf[64]
	maxParams       = 24      // param_list[24]
	MaxStringLen    = 1 << 20 // INPUT_BUF_DEFAULT_SIZE: longer OSC, DCS and APC strings are dropped
)

// Parser splits a byte stream into actions for a Handler. Writes may split the stream at any
// byte: the parser keeps its state between them.
type Parser struct {
	h       Handler
	state   state
	discard bool
	bel     bool
	timer   bool // tmux's ground timer runs: a string (OSC, DCS, APC, …) has started

	inter    [maxIntermediate]byte
	interLen int
	param    [maxParamBytes]byte
	paramLen int
	input    []byte

	utf8     [4]byte
	utf8Have int
	utf8Size int
	utf8Bad  bool
	utf8On   bool

	params Params
}

// NewParser returns a parser in the ground state.
func NewParser(h Handler) *Parser {
	return &Parser{h: h, input: make([]byte, 0, 32)}
}

// State returns the name of the parser's state, for tests.
func (p *Parser) State() string { return p.state.String() }

// Reset returns the parser to the ground state and drops any partial sequence.
func (p *Parser) Reset() {
	p.setState(stGround)
	p.clear()
	p.utf8On = false
}

func (p *Parser) clear() {
	p.interLen = 0
	p.paramLen = 0
	p.input = p.input[:0]
	p.discard = false
	p.bel = false
	p.timer = false
}

// GroundTimeout is how long tmux lets a string (OSC, DCS, APC, a rename, SOS or PM) go
// unterminated before it gives up on it.
const GroundTimeout = 5 * time.Second

// InString reports whether a string has started and not ended: the pane's reader then calls
// Expire once GroundTimeout passes with no more output, as tmux's ground timer does.
func (p *Parser) InString() bool { return p.timer }

// Expire gives up on an unterminated string: the parser returns to the ground state. It
// reports whether there was one; the caller then also resets its current cell (colours and
// attributes), as tmux's input_reset does.
func (p *Parser) Expire() bool {
	if !p.timer {
		return false
	}
	p.clear()
	p.state = stGround
	p.utf8On = false
	return true
}

// enter and exit run a state's enter and exit functions, as tmux's input_set_state does.
func (p *Parser) setState(next state) {
	switch p.state {
	case stOscString:
		if !p.discard {
			p.h.OscDispatch(p.input, p.bel)
		}
	case stApcString:
		if !p.discard {
			p.h.ApcDispatch(p.input)
		}
	case stRenameString:
		if !p.discard {
			p.h.RenameDispatch(p.input)
		}
	}
	p.state = next
	switch next {
	case stGround:
		p.timer = false
		if cap(p.input) > 32 {
			p.input = make([]byte, 0, 32) // input_ground gives back a large buffer
		}
	case stEscEnter, stCsiEnter:
		p.clear()
	case stDcsEnter, stOscString, stApcString, stRenameString, stConsumeST:
		p.clear()
		p.timer = true
	}
}

// stopUTF8 ends a UTF-8 sequence that cannot be completed: tmux prints U+FFFD for it.
func (p *Parser) stopUTF8() {
	if p.utf8On {
		p.h.PrintUTF8(p.utf8[:p.utf8Have], true)
	}
	p.utf8On = false
}

func (p *Parser) topBit(c byte) {
	if !p.utf8On {
		p.utf8On = true
		p.utf8Have, p.utf8Bad = 0, false
		switch {
		case c >= 0xc2 && c <= 0xdf:
			p.utf8Size = 2
		case c >= 0xe0 && c <= 0xef:
			p.utf8Size = 3
		case c >= 0xf0 && c <= 0xf4:
			p.utf8Size = 4
		default:
			p.utf8[0] = c
			p.utf8Have = 1
			p.stopUTF8()
			return
		}
		p.utf8[0] = c
		p.utf8Have = 1
		return
	}
	if c&0xc0 != 0x80 {
		p.utf8Bad = true // tmux still takes the byte, then fails the whole sequence
	}
	p.utf8[p.utf8Have] = c
	p.utf8Have++
	if p.utf8Have < p.utf8Size {
		return
	}
	p.utf8On = false
	p.h.PrintUTF8(p.utf8[:p.utf8Have], p.utf8Bad)
}

// Write parses b. It never fails.
func (p *Parser) Write(b []byte) (int, error) {
	for i := 0; i < len(b); i++ {
		c := b[i]
		// The ground state's printable ASCII is the common case: hand it over as a run.
		if p.state == stGround && c >= 0x20 && c <= 0x7e {
			p.stopUTF8()
			j := i + 1
			for j < len(b) && b[j] >= 0x20 && b[j] <= 0x7e {
				j++
			}
			p.h.Print(b[i:j])
			i = j - 1
			continue
		}
		tr := &lookup[p.state][c]
		switch tr.act {
		case acC0:
			p.h.Execute(c)
		case acPrint:
			p.stopUTF8()
			p.h.Print(b[i : i+1])
		case acTopBit:
			p.topBit(c)
		case acIntermediate:
			if p.interLen == maxIntermediate {
				p.discard = true
			} else {
				p.inter[p.interLen] = c
				p.interLen++
			}
		case acParameter:
			if p.paramLen == maxParamBytes {
				p.discard = true
			} else {
				p.param[p.paramLen] = c
				p.paramLen++
			}
		case acInput:
			if len(p.input)+1 >= MaxStringLen {
				p.discard = true
			} else {
				p.input = append(p.input, c)
			}
		case acEndBel:
			p.bel = true
		case acEscDispatch:
			if !p.discard {
				p.h.EscDispatch(p.inter[:p.interLen], c)
			}
		case acCsiDispatch:
			if !p.discard && p.params.split(p.param[:p.paramLen]) {
				p.h.CsiDispatch(&p.params, p.inter[:p.interLen], c)
			}
		case acDcsDispatch:
			// tmux dispatches a DCS whatever its parameters; only sixel images read them.
			if !p.discard {
				if !p.params.split(p.param[:p.paramLen]) {
					p.params.n = 0
				}
				p.h.DcsDispatch(&p.params, p.inter[:p.interLen], p.input)
			}
		}
		if tr.next != noChange {
			p.setState(tr.next)
		}
	}
	return len(b), nil
}
