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
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/vt"
)

// Keys into a pane (TASKS.md C2.11): a key is encoded as the pane's program asked, by its
// modes: application cursor and keypad keys, bracketed paste, and extended keys (mode 1 or 2,
// which the pane sets with CSI > 4 ; n m when extended-keys allows it).

// ExtendedFormat is the extended-keys-format option.
type ExtendedFormat int

// extended-keys-format values; tmux's default is xterm.
const (
	FormatCSIu  ExtendedFormat = 0
	FormatXterm ExtendedFormat = 1
)

// encodeTable is tmux's input_key_defaults, with the modified keys built from their templates.
var encodeTable = sync.OnceValue(func() map[Key]string {
	m := map[Key]string{
		PasteStart: "\033[200~", PasteStart | ImpliedMeta: "\033[200~",
		PasteEnd: "\033[201~", PasteEnd | ImpliedMeta: "\033[201~",
		F1: "\033OP", F2: "\033OQ", F3: "\033OR", F4: "\033OS", F5: "\033[15~", F6: "\033[17~",
		F7: "\033[18~", F8: "\033[19~", F9: "\033[20~", F10: "\033[21~", F11: "\033[23~", F12: "\033[24~",
		IC: "\033[2~", DC: "\033[3~", Home: "\033[1~", End: "\033[4~", NPage: "\033[6~", PPage: "\033[5~",
		BTab:        "\033[Z",
		Up | Cursor: "\033OA", Down | Cursor: "\033OB", Right | Cursor: "\033OC", Left | Cursor: "\033OD",
		Up: "\033[A", Down: "\033[B", Right: "\033[C", Left: "\033[D",
		KPSlash | Keypad: "\033Oo", KPStar | Keypad: "\033Oj", KPMinus | Keypad: "\033Om",
		KPSeven | Keypad: "\033Ow", KPEight | Keypad: "\033Ox", KPNine | Keypad: "\033Oy",
		KPPlus | Keypad: "\033Ok", KPFour | Keypad: "\033Ot", KPFive | Keypad: "\033Ou",
		KPSix | Keypad: "\033Ov", KPOne | Keypad: "\033Oq", KPTwo | Keypad: "\033Or",
		KPThree | Keypad: "\033Os", KPEnter | Keypad: "\033OM", KPZero | Keypad: "\033Op",
		KPPeriod | Keypad: "\033On",
		KPSlash:           "/", KPStar: "*", KPMinus: "-", KPSeven: "7", KPEight: "8", KPNine: "9", KPPlus: "+",
		KPFour: "4", KPFive: "5", KPSix: "6", KPOne: "1", KPTwo: "2", KPThree: "3", KPEnter: "\n",
		KPZero: "0", KPPeriod: ".",
	}
	templates := map[Key]string{
		F1: "\033[1;_P", F2: "\033[1;_Q", F3: "\033[1;_R", F4: "\033[1;_S", F5: "\033[15;_~",
		F6: "\033[17;_~", F7: "\033[18;_~", F8: "\033[19;_~", F9: "\033[20;_~", F10: "\033[21;_~",
		F11: "\033[23;_~", F12: "\033[24;_~", Up: "\033[1;_A", Down: "\033[1;_B", Right: "\033[1;_C",
		Left: "\033[1;_D", Home: "\033[1;_H", End: "\033[1;_F", PPage: "\033[5;_~", NPage: "\033[6;_~",
		IC: "\033[2;_~", DC: "\033[3;_~",
	}
	modifiers := []Key{
		0, 0, Shift, Meta | ImpliedMeta, Shift | Meta | ImpliedMeta, Ctrl, Shift | Ctrl,
		Meta | ImpliedMeta | Ctrl, Shift | Meta | ImpliedMeta | Ctrl,
	}
	for k, t := range templates {
		for j := 2; j < len(modifiers); j++ {
			m[k|modifiers[j]] = strings.Replace(t, "_", strconv.Itoa(j), 1)
		}
	}
	return m
})

// extended writes a key in the extended form (tmux's input_key_extended); false when it has no
// modifier that form can carry.
func extended(b *[]byte, key Key, format ExtendedFormat) bool {
	var modifier byte
	switch key & MaskModifiers {
	case Shift:
		modifier = '2'
	case Meta:
		modifier = '3'
	case Shift | Meta:
		modifier = '4'
	case Ctrl:
		modifier = '5'
	case Shift | Ctrl:
		modifier = '6'
	case Meta | Ctrl:
		modifier = '7'
	case Shift | Meta | Ctrl:
		modifier = '8'
	default:
		return false
	}
	n := uint64(key & MaskKey)
	if key.IsUnicode() {
		n = uint64(key.Rune())
	}
	if format == FormatXterm {
		*b = append(*b, "\033[27;"+string(modifier)+";"+strconv.FormatUint(n, 10)+"~"...)
	} else {
		*b = append(*b, "\033["+strconv.FormatUint(n, 10)+";"+string(modifier)+"u"...)
	}
	return true
}

// vt10x is input_key_vt10x: the standard mode, with Ctrl keys as C0 controls.
func vt10x(b *[]byte, key Key) bool {
	const from, to = "1!9(0)=+;:'\",<.>/-8? 2", "119900=+;;'',,..\x1f\x1f\x7f\x7f\x00\x00"
	if key&Meta != 0 {
		*b = append(*b, '\033')
	}
	if key.IsUnicode() {
		*b = utf8.AppendRune(*b, key.Rune())
		return true
	}
	only := key & MaskKey
	if only == '\r' || only == '\n' || only == '\t' {
		key &^= Ctrl
	}
	if key&Ctrl != 0 {
		switch i := strings.IndexByte(from, byte(only)); {
		case only < 0x80 && i >= 0:
			key = Key(to[i])
		case only >= '3' && only <= '7':
			key = only - 0o30
		case only >= '@' && only <= '~':
			key = only & 0x1f
		default:
			return false
		}
	}
	*b = append(*b, byte(key&0x7f))
	return true
}

// mode1 is input_key_mode1: keys modifyOtherKeys mode 1 still sends in the standard form.
func mode1(b *[]byte, key Key) bool {
	if key&(Ctrl|Meta) == Meta {
		return vt10x(b, key)
	}
	only := key & MaskKey
	if key&Ctrl != 0 && (only == ' ' || only == '/' || only == '@' || only == '^' ||
		(only >= '2' && only <= '8') || (only >= '@' && only <= '~')) {
		return vt10x(b, key)
	}
	return false
}

// Encode returns what a pane in screen modes mode is sent for key (tmux's input_key), and
// false when the key has no encoding, which send-keys then sends as text; what was written
// before that is still sent, as in tmux. backspace is the backspace option (tmux's default is
// DEL, 0x7f).
func Encode(key Key, mode int, backspace Key, format ExtendedFormat) ([]byte, bool) {
	var b []byte
	if key.IsMouse() {
		return nil, true
	}
	if key&Literal != 0 {
		return []byte{byte(key)}, true
	}
	if key&MaskKey == BSpace {
		if key&MaskModifiers == 0 {
			c := -1
			nk := backspace
			switch nk & MaskModifiers {
			case 0:
				c = int(nk & 0xff)
			case Ctrl:
				nk &= MaskKey
				switch {
				case nk == '?':
					c = 0x7f
				case nk >= '@' && nk <= '_':
					c = int(nk - 0x40)
				case nk >= 'a' && nk <= 'z':
					c = int(nk - 0x60)
				}
			}
			if c >= 0 {
				return []byte{byte(c)}, true
			}
			return nil, true
		}
		key = backspace | key&(MaskFlags|MaskModifiers)
	}
	if key&MaskKey == BTab {
		if mode&vt.ModeKeysExtended2 != 0 {
			key = '\011' | key&^MaskKey | Shift
		} else {
			key &^= MaskModifiers
		}
	}
	if key&^MaskKey == 0 {
		if key == C0HT || key == C0CR || key == C0ESC || (key >= 0x20 && key <= 0x7f) {
			return []byte{byte(key)}, true
		}
		if key.IsUnicode() {
			return utf8.AppendRune(nil, key.Rune()), true
		}
	}
	if mode&vt.ModeKKeypad == 0 {
		key &^= Keypad
	}
	if mode&vt.ModeKCursor == 0 {
		key &^= Cursor
	}
	s, ok := encodeTable()[key]
	if !ok && key&Meta != 0 && key&ImpliedMeta == 0 {
		s, ok = encodeTable()[key&^Meta]
	}
	if !ok && key&Cursor != 0 {
		s, ok = encodeTable()[key&^Cursor]
	}
	if !ok && key&Keypad != 0 {
		s, ok = encodeTable()[key&^Keypad]
	}
	if ok {
		if key.IsPaste() && mode&vt.ModeBracketPaste == 0 {
			return nil, true
		}
		if key&Meta != 0 && key&ImpliedMeta == 0 {
			b = append(b, '\033')
		}
		return append(b, s...), true
	}
	if key.IsUser() || key.IsSpecial() || key.IsMouse() {
		return nil, true
	}
	switch mode & vt.ExtendedKeyModes {
	case vt.ModeKeysExtended2:
		return b, extended(&b, key, format)
	case vt.ModeKeysExtended:
		if mode1(&b, key) {
			return b, true
		}
		b = b[:0]
		return b, extended(&b, key, format)
	}
	// A key with no standard form still sends the escape of its Meta, as in tmux.
	return b, vt10x(&b, key)
}
