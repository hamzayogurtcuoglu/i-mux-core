// Ported from tmux 3.8's input-keys.c:
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

package keys

import (
	"fmt"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/vt"
)

// Mouse report bits (tmux's MOUSE_MASK_* and MOUSE_PARAM_*).
const (
	MouseMaskButtons   = 195
	MouseMaskShift     = 4
	MouseMaskMeta      = 8
	MouseMaskCtrl      = 16
	MouseMaskDrag      = 32
	MouseMaskModifiers = MouseMaskShift | MouseMaskMeta | MouseMaskCtrl

	MouseButton1 = 0
	MouseButton3 = 2

	mouseParamMax     = 0xff
	mouseParamUTF8Max = 0x7ff
	mouseParamBtnOff  = 0x20
	mouseParamPosOff  = 0x21
)

// MouseEvent is a mouse report as tmux keeps it (struct mouse_event): the button and the last
// report's, and the SGR form when the terminal sent one (SGRType 'M' or 'm', else ' ').
type MouseEvent struct {
	B, LB   int
	SGRType byte
	SGRB    int
}

func mouseDrag(b int) bool    { return b&MouseMaskDrag != 0 }
func mouseRelease(b int) bool { return b&MouseMaskButtons == 3 }

// EncodeMouse is input_key_get_mouse: the report for a program whose screen is in mode, at x,
// y of its screen; nil when the program did not ask for this event.
func EncodeMouse(mode int, m MouseEvent, x, y int) []byte {
	if mouseDrag(m.B) && mode&(vt.ModeMouseButton|vt.ModeMouseAll) == 0 {
		return nil
	}
	if mode&vt.AllMouseModes == 0 {
		return nil
	}
	if m.SGRType != ' ' {
		if mouseDrag(m.SGRB) && mouseRelease(m.SGRB) && mode&vt.ModeMouseAll == 0 {
			return nil
		}
	} else if mouseDrag(m.B) && mouseRelease(m.B) && mouseRelease(m.LB) && mode&vt.ModeMouseAll == 0 {
		return nil
	}

	switch {
	case m.SGRType != ' ' && mode&vt.ModeMouseSGR != 0:
		return fmt.Appendf(nil, "\033[<%d;%d;%d%c", m.SGRB, x+1, y+1, m.SGRType)
	case mode&vt.ModeMouseUTF8 != 0:
		if m.B > mouseParamUTF8Max-mouseParamBtnOff || x > mouseParamUTF8Max-mouseParamPosOff ||
			y > mouseParamUTF8Max-mouseParamPosOff {
			return nil
		}
		buf := []byte("\033[M")
		buf = split2(buf, m.B+mouseParamBtnOff)
		buf = split2(buf, x+mouseParamPosOff)
		return split2(buf, y+mouseParamPosOff)
	default:
		if m.B+mouseParamBtnOff > mouseParamMax {
			return nil
		}
		buf := []byte("\033[M")
		buf = append(buf, byte(m.B+mouseParamBtnOff))
		buf = append(buf, byte(min(x+mouseParamPosOff, mouseParamMax)))
		return append(buf, byte(min(y+mouseParamPosOff, mouseParamMax)))
	}
}

// split2 is input_key_split2: c in one byte, or two in UTF-8 form.
func split2(buf []byte, c int) []byte {
	if c > 0x7f {
		return append(buf, byte(c>>6|0xc0), byte(c&0x3f|0x80))
	}
	return append(buf, byte(c))
}
