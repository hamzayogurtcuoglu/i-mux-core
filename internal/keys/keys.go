// Ported from tmux 3.8's tmux.h (key codes) and key-string.c:
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
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"
)

// Key is a key as tmux encodes it (key_code): the key in the low 40 bits (its type in bits 32
// to 39), then modifiers, then flags. A Unicode character above 0x7f is held as the rune with
// UnicodeBit set, so that it differs from a single byte 0x80 to 0xff, as tmux's utf8_char
// does.
type Key uint64

// Modifiers and flags (tmux's KEYC_*).
const (
	Meta           Key = 0x00100000000000
	Ctrl           Key = 0x00200000000000
	Shift          Key = 0x00400000000000
	Literal        Key = 0x01000000000000
	Keypad         Key = 0x02000000000000
	Cursor         Key = 0x04000000000000
	ImpliedMeta    Key = 0x08000000000000
	BuildModifiers Key = 0x10000000000000
	VI             Key = 0x20000000000000
	Sent           Key = 0x40000000000000

	MaskType      Key = 0x0000ff00000000
	MaskModifiers Key = 0x00ff0000000000
	MaskFlags     Key = 0xff000000000000
	MaskKey       Key = 0x0000ffffffffff

	// UnicodeBit marks a Unicode character above 0x7f.
	UnicodeBit Key = 0x80000000

	// NUser is the number of user keys (User0 to User1000).
	NUser = 1000
)

// Key types (tmux's key_code_type).
const (
	TypeUnicode = iota
	TypeUser
	TypeFunction
	TypeMouseMove
	TypeMouseDown
	TypeMouseUp
	TypeMouseDrag
	TypeMouseDragEnd
	TypeWheelDown
	TypeWheelUp
	TypeSecondClick
	TypeDoubleClick
	TypeTripleClick
	TypeNoType
)

func shiftType(t int) Key { return Key(t) << 32 }

// Mouse locations (tmux's key_code_mouse_location).
const (
	LocationPane = iota
	LocationStatus
	LocationStatusLeft
	LocationStatusRight
	LocationStatusDefault
	LocationBorder
	LocationScrollbarUp
	LocationScrollbarSlider
	LocationScrollbarDown
	LocationEmpty
	LocationControl0 // to LocationControl0+9
	LocationNowhere  = LocationControl0 + 10
)

// MouseKey makes a mouse key (tmux's KEYC_MAKE_MOUSE_KEY).
func MouseKey(typ, button, location int) Key {
	return shiftType(typ) | Key(button)<<8 | Key(location)
}

// C0 control characters used as keys.
const (
	C0NUL Key = 0x00
	C0HT  Key = 0x09
	C0CR  Key = 0x0d
	C0ESC Key = 0x1b
)

// User is the first user key (User0).
var User = shiftType(TypeUser)

// Special keys (tmux's KEYC_NONE onwards).
const (
	None Key = 2<<32 + iota // KEYC_SHIFT_TYPE(KEYC_TYPE_FUNCTION)
	Unknown
	FocusIn
	FocusOut
	Any
	PasteStart
	PasteEnd
	BSpace
	F1
	F2
	F3
	F4
	F5
	F6
	F7
	F8
	F9
	F10
	F11
	F12
	IC
	DC
	Home
	End
	NPage
	PPage
	BTab
	Up
	Down
	Left
	Right
	KPSlash
	KPStar
	KPMinus
	KPSeven
	KPEight
	KPNine
	KPPlus
	KPFour
	KPFive
	KPSix
	KPOne
	KPTwo
	KPThree
	KPEnter
	KPZero
	KPPeriod
	ReportDarkTheme
	ReportLightTheme
	Mouse
	Dragging
	DoubleClick
)

// Unicode returns the key for character r.
func Unicode(r rune) Key {
	if r <= 0x7f {
		return Key(r)
	}
	return Key(r) | UnicodeBit
}

// IsUnicode reports whether k is a Unicode character above 0x7f (tmux's KEYC_IS_UNICODE).
func (k Key) IsUnicode() bool { return k&MaskType == 0 && k&MaskKey > 0x7f && k&UnicodeBit != 0 }

// Rune is a Unicode key's character.
func (k Key) Rune() rune { return rune(k & MaskKey &^ UnicodeBit) }

// IsUser reports whether k is a user key.
func (k Key) IsUser() bool { return k&MaskType == shiftType(TypeUser) }

// IsSpecial reports whether k is a function key.
func (k Key) IsSpecial() bool { return k&MaskType == shiftType(TypeFunction) }

// IsMouse reports whether k is a mouse key (tmux's KEYC_IS_MOUSE).
func (k Key) IsMouse() bool {
	t := k & MaskType
	return k&MaskKey == Mouse || (t >= shiftType(TypeMouseMove) && t <= shiftType(TypeTripleClick))
}

// IsPaste reports whether k is a paste bracket.
func (k Key) IsPaste() bool {
	kk := k & MaskKey
	return k&MaskType == shiftType(TypeFunction) && (kk == PasteStart || kk == PasteEnd)
}

type entry struct {
	name string
	key  Key
}

// table is tmux's key_string_table, mouse keys made below. It is built when first used, not
// when the program starts: a command-line client seldom needs it.
var table = sync.OnceValue(func() []entry {
	t := []entry{
		{"F1", F1 | ImpliedMeta},
		{"F2", F2 | ImpliedMeta},
		{"F3", F3 | ImpliedMeta},
		{"F4", F4 | ImpliedMeta},
		{"F5", F5 | ImpliedMeta},
		{"F6", F6 | ImpliedMeta},
		{"F7", F7 | ImpliedMeta},
		{"F8", F8 | ImpliedMeta},
		{"F9", F9 | ImpliedMeta},
		{"F10", F10 | ImpliedMeta},
		{"F11", F11 | ImpliedMeta},
		{"F12", F12 | ImpliedMeta},
		{"IC", IC | ImpliedMeta},
		{"Insert", IC | ImpliedMeta},
		{"DC", DC | ImpliedMeta},
		{"Delete", DC | ImpliedMeta},
		{"Home", Home | ImpliedMeta},
		{"End", End | ImpliedMeta},
		{"NPage", NPage | ImpliedMeta},
		{"PageDown", NPage | ImpliedMeta},
		{"PgDn", NPage | ImpliedMeta},
		{"PPage", PPage | ImpliedMeta},
		{"PageUp", PPage | ImpliedMeta},
		{"PgUp", PPage | ImpliedMeta},
		{"BTab", BTab},
		{"Space", ' '},
		{"BSpace", BSpace},
	}
	c0 := []string{
		"[NUL]", "[SOH]", "[STX]", "[ETX]", "[EOT]", "[ENQ]", "[ASC]", "[BEL]", "[BS]", "Tab",
		"[LF]", "[VT]", "[FF]", "Enter", "[SO]", "[SI]", "[DLE]", "[DC1]", "[DC2]", "[DC3]",
		"[DC4]", "[NAK]", "[SYN]", "[ETB]", "[CAN]", "[EM]", "[SUB]", "Escape", "[FS]", "[GS]",
		"[RS]", "[US]",
	}
	for i, n := range c0 {
		t = append(t, entry{n, Key(i)})
	}
	t = append(t,
		entry{"Up", Up | Cursor | ImpliedMeta}, entry{"Down", Down | Cursor | ImpliedMeta},
		entry{"Left", Left | Cursor | ImpliedMeta}, entry{"Right", Right | Cursor | ImpliedMeta},
		entry{"KP/", KPSlash | Keypad}, entry{"KP*", KPStar | Keypad}, entry{"KP-", KPMinus | Keypad},
		entry{"KP7", KPSeven | Keypad}, entry{"KP8", KPEight | Keypad}, entry{"KP9", KPNine | Keypad},
		entry{"KP+", KPPlus | Keypad}, entry{"KP4", KPFour | Keypad}, entry{"KP5", KPFive | Keypad},
		entry{"KP6", KPSix | Keypad}, entry{"KP1", KPOne | Keypad}, entry{"KP2", KPTwo | Keypad},
		entry{"KP3", KPThree | Keypad}, entry{"KPEnter", KPEnter | Keypad}, entry{"KP0", KPZero | Keypad},
		entry{"KP.", KPPeriod | Keypad},
	)
	locations := []struct {
		name string
		loc  int
	}{
		{"Pane", LocationPane},
		{"Status", LocationStatus},
		{"StatusLeft", LocationStatusLeft},
		{"StatusRight", LocationStatusRight},
		{"StatusDefault", LocationStatusDefault},
		{"ScrollbarUp", LocationScrollbarUp},
		{"ScrollbarSlider", LocationScrollbarSlider},
		{"ScrollbarDown", LocationScrollbarDown},
		{"Empty", LocationEmpty},
		{"Border", LocationBorder},
	}
	for i := range 10 {
		locations = append(locations, struct {
			name string
			loc  int
		}{"Control" + strconv.Itoa(i), LocationControl0 + i})
	}
	buttons := []int{1, 2, 3, 6, 7, 8, 9, 10, 11}
	add := func(prefix string, typ, button int) {
		for _, l := range locations {
			t = append(t, entry{prefix + l.name, MouseKey(typ, button, l.loc)})
		}
	}
	for _, kind := range []struct {
		name string
		typ  int
	}{{"MouseDown", TypeMouseDown}, {"MouseUp", TypeMouseUp}, {"MouseDrag", TypeMouseDrag}, {"MouseDragEnd", TypeMouseDragEnd}} {
		for _, b := range buttons {
			add(kind.name+strconv.Itoa(b), kind.typ, b)
		}
	}
	add("WheelUp", TypeWheelUp, 0)
	add("WheelDown", TypeWheelDown, 0)
	for _, kind := range []struct {
		name string
		typ  int
	}{{"SecondClick", TypeSecondClick}, {"DoubleClick", TypeDoubleClick}, {"TripleClick", TypeTripleClick}} {
		for _, b := range buttons {
			add(kind.name+strconv.Itoa(b), kind.typ, b)
		}
	}
	return t
})

// searchTable is key_string_search_table.
func searchTable(s string) Key {
	for _, e := range table() {
		if strings.EqualFold(s, e.name) {
			return e.key
		}
	}
	if strings.HasPrefix(s, "User") {
		if u, err := strconv.ParseUint(s[4:], 10, 32); err == nil && u <= NUser {
			return User + Key(u)
		}
	}
	return Unknown
}

// Parse returns the key a name stands for, or Unknown (tmux's key_string_lookup_string).
func Parse(s string) Key {
	if strings.EqualFold(s, "None") {
		return None
	}
	if strings.EqualFold(s, "Any") {
		return Any
	}
	if strings.HasPrefix(s, "0x") {
		u, err := strconv.ParseUint(s[2:], 16, 32)
		if err != nil {
			return Unknown
		}
		if u < 32 {
			return Key(u)
		}
		if !utf8.ValidRune(rune(u)) {
			return Unknown
		}
		return Unicode(rune(u))
	}
	var mods Key
	if len(s) > 1 && s[0] == '^' {
		if len(s) == 2 {
			return Key(lower(s[1])) | Ctrl
		}
		mods |= Ctrl
		s = s[1:]
	}
	for len(s) > 1 && s[1] == '-' {
		switch s[0] {
		case 'C', 'c':
			mods |= Ctrl
		case 'M', 'm':
			mods |= Meta
		case 'S', 's':
			mods |= Shift
		default:
			return Unknown
		}
		s = s[2:]
	}
	if s == "" {
		return Unknown
	}
	var k Key
	if len(s) == 1 && s[0] <= 127 {
		k = Key(s[0])
		if k < 32 {
			return Unknown
		}
	} else {
		if s[0] >= 0x80 {
			r, size := utf8.DecodeRuneInString(s)
			if r == utf8.RuneError || size != len(s) {
				return Unknown
			}
			return Unicode(r) | mods
		}
		k = searchTable(s)
		if k == Unknown {
			return Unknown
		}
		if mods&Meta == 0 {
			k &^= ImpliedMeta
		}
	}
	return k | mods
}

func lower(c byte) byte {
	if c >= 'A' && c <= 'Z' {
		return c + 32
	}
	return c
}

// String returns a key's name as tmux shows it (key_string_lookup_key).
func (k Key) String() string { return Name(k, false) }

// Name returns a key's name, with its flags in brackets when withFlags is set (tmux's
// key_string_lookup_key).
func Name(k Key, withFlags bool) string {
	saved := k
	var b strings.Builder
	flags := func() string {
		if !withFlags || saved&MaskFlags == 0 {
			return b.String()
		}
		b.WriteByte('[')
		for _, f := range []struct {
			k Key
			c byte
		}{{Literal, 'L'}, {Keypad, 'K'}, {Cursor, 'C'}, {ImpliedMeta, 'I'}, {BuildModifiers, 'B'}, {Sent, 'S'}} {
			if saved&f.k != 0 {
				b.WriteByte(f.c)
			}
		}
		b.WriteByte(']')
		return b.String()
	}
	if k&Literal != 0 {
		b.WriteByte(byte(k & 0xff))
		return flags()
	}
	if k&Ctrl != 0 {
		b.WriteString("C-")
	}
	if k&Meta != 0 {
		b.WriteString("M-")
	}
	if k&Shift != 0 {
		b.WriteString("S-")
	}
	k &= MaskKey
	special := map[Key]string{
		None: "None", Unknown: "Unknown", Any: "Any", FocusIn: "FocusIn", FocusOut: "FocusOut",
		PasteStart: "PasteStart", PasteEnd: "PasteEnd", ReportDarkTheme: "ReportDarkTheme",
		ReportLightTheme: "ReportLightTheme", Mouse: "Mouse", Dragging: "Dragging",
		MouseKey(TypeMouseMove, 0, LocationPane):        "MouseMovePane",
		MouseKey(TypeMouseMove, 0, LocationStatus):      "MouseMoveStatus",
		MouseKey(TypeMouseMove, 0, LocationStatusLeft):  "MouseMoveStatusLeft",
		MouseKey(TypeMouseMove, 0, LocationStatusRight): "MouseMoveStatusRight",
		MouseKey(TypeMouseMove, 0, LocationBorder):      "MouseMoveBorder",
	}
	if s, ok := special[k]; ok {
		b.WriteString(s)
		return flags()
	}
	if k.IsUser() {
		fmt.Fprintf(&b, "User%d", k-User)
		return flags()
	}
	for _, e := range table() {
		if k == e.key&MaskKey {
			b.WriteString(e.name)
			return flags()
		}
	}
	if k.IsUnicode() {
		b.WriteRune(k.Rune())
		return flags()
	}
	switch {
	case k > 255:
		return fmt.Sprintf("Invalid#%x", uint64(saved))
	case k > 32 && k <= 126:
		b.WriteByte(byte(k))
	case k == 127:
		b.WriteString("C-?")
	case k >= 128:
		fmt.Fprintf(&b, "\\%o", uint64(k))
	}
	return flags()
}
