// Ported from tmux 3.8's tty-keys.c:
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

package input

import (
	"strings"
	"time"
	"unicode/utf8"

	k "github.com/hamzayogurtcuoglu/i-mux-core/internal/keys"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/termout"
)

// A client terminal's input (TASKS.md C2.10), as tmux's tty-keys.c reads it: keys from a tree
// of the sequences the terminal sends, with escape-time deciding between Escape and a longer
// sequence; mouse reports; and the terminal's replies to the server's queries, which are taken
// out of the key stream.

// Mouse is a mouse report (tmux's struct mouse_event, the parts the terminal gives).
type Mouse struct {
	X, Y       int  // the position, from 0
	B          int  // the button and modifiers; 3 for a release in SGR mode
	LX, LY, LB int  // the last report's
	SGRType    byte // 'M' or 'm' for SGR reports, else ' '
	SGRB       int  // the SGR button
}

// EventKind says what an Event is.
type EventKind int

// Kinds of events.
const (
	EventKey       EventKind = iota // a key, a mouse report (Key is keys.Mouse), focus, paste, theme report
	EventDA                         // primary device attributes: Features to add
	EventDA2                        // secondary device attributes: Name's default features
	EventXDA                        // the terminal's name and version (XTVERSION): TermType
	EventSync                       // the terminal has synchronized output: Features is "sync"
	EventColour                     // OSC 10 or 11: Fg or Bg (-1 when not given)
	EventPalette                    // OSC 4: Index and Colour
	EventClipboard                  // OSC 52: Clip and Data
	EventWinsize                    // CSI 8 ; rows ; cols t (Rows, Cols) or CSI 4 ; h ; w t (YPixel, XPixel)
)

// Event is something the client terminal sent.
type Event struct {
	Kind  EventKind
	Key   k.Key
	Mouse Mouse
	Raw   []byte // the bytes of the key

	Features string // EventDA, EventSync: features to add (tmux's tty_parse_client_features)
	Name     string // EventDA2: a terminal name for tty_default_features
	TermType string // EventXDA: what the terminal says it is
	Fg, Bg   int    // EventColour: the colour (vt.Color), or -1
	Index    int    // EventPalette
	Colour   int    // EventPalette
	Clip     byte   // EventClipboard
	Data     []byte // EventClipboard

	// EventWinsize: the terminal's size in cells (Rows and Cols), or in pixels (YPixel and
	// XPixel, with Rows and Cols 0).
	Rows, Cols     int
	XPixel, YPixel int
}

type node struct {
	ch                byte
	key               k.Key
	left, right, next *node
}

// Parser reads one client terminal's input.
type Parser struct {
	tree  *node
	erase int // the terminal's VERASE, or -1

	// EscapeTime is the escape-time option (tmux's default is 10 ms).
	EscapeTime time.Duration

	buf       []byte
	timer     bool // a partial key is waiting for more input or the timer
	paste     bool // inside a bracketed paste (TTY_BRACKETPASTE)
	waiting   bool // replies to the server's queries are still expected
	haveDA    bool
	haveDA2   bool
	haveXDA   bool
	haveSync  bool
	waitFg    bool
	waitBg    bool
	lastMouse Mouse

	// WinsizeQuery is tmux's TTY_WINSIZEQUERY: the server asked the terminal for its size
	// (CSI 18 t and 14 t), so its answers are read; the pixel answer ends the query.
	WinsizeQuery bool
}

// New builds the key tree for a terminal (tmux's tty_keys_build); erase is its VERASE byte, or
// -1 when it has none.
func New(term *termout.Term, erase int) *Parser {
	p := &Parser{erase: erase, EscapeTime: 10 * time.Millisecond}
	for _, x := range xtermKeys {
		for j := 2; j < len(xtermModifiers); j++ {
			s := strings.Replace(x.s, "_", string(rune('0'+j)), 1)
			p.add(s, x.key|xtermModifiers[j])
		}
	}
	for _, r := range rawKeys {
		p.add(r.s, r.key)
	}
	if term != nil {
		for _, c := range codeKeys() {
			if s := term.String(c.cap); s != "" {
				p.add(s, c.key)
			}
		}
	}
	return p
}

// Requested says the server has asked the terminal for its attributes and colours (tmux's
// tty_send_requests); until they arrive or StopWaiting, partial keys wait longer.
func (p *Parser) Requested() {
	p.waitFg, p.waitBg = true, true
	p.waiting = true
}

// StopWaiting gives up waiting for replies (tmux's start timer, after a second).
func (p *Parser) StopWaiting() {
	p.waiting = false
	p.waitFg, p.waitBg = false, false
	p.haveDA, p.haveDA2, p.haveXDA, p.haveSync = true, true, true, true
}

// Bracketed reports whether a bracketed paste is in progress.
func (p *Parser) Bracketed() bool { return p.paste }

// add is tty_keys_add: a sequence already in the tree, or one that runs into a key of the tree,
// replaces that key, as in tmux.
func (p *Parser) add(s string, key k.Key) {
	if n, _ := p.find([]byte(s)); n != nil {
		n.key = key
		return
	}
	tp := &p.tree
	for i := 0; i < len(s); {
		if *tp == nil {
			*tp = &node{ch: s[i], key: k.Unknown}
		}
		n := *tp
		switch {
		case s[i] == n.ch:
			i++
			if i == len(s) {
				n.key = key
				return
			}
			tp = &n.next
		case s[i] < n.ch:
			tp = &n.left
		default:
			tp = &n.right
		}
	}
}

// find is tty_keys_find: the node buf leads to and how many bytes it took.
func (p *Parser) find(buf []byte) (*node, int) {
	n, size := p.tree, 0
	for len(buf) > 0 && n != nil {
		if n.ch == buf[0] {
			buf = buf[1:]
			size++
			if len(buf) == 0 || (n.next == nil && n.key != k.Unknown) {
				return n, size
			}
			n = n.next
		} else if buf[0] < n.ch {
			n = n.left
		} else {
			n = n.right
		}
	}
	return nil, size
}

// Feed adds what the client sent and returns the events it completes.
func (p *Parser) Feed(b []byte) []Event {
	p.buf = append(p.buf, b...)
	return p.run(false)
}

// Expire is called when Delay has passed with nothing more: a partial key is taken as it is.
func (p *Parser) Expire() []Event {
	if !p.timer {
		return nil
	}
	return p.run(true)
}

// Delay is how long to wait for more input before Expire, when a partial key is pending.
func (p *Parser) Delay() (time.Duration, bool) {
	if !p.timer {
		return 0, false
	}
	d := max(p.EscapeTime, time.Millisecond)
	if p.paste && partialPasteEnd(p.buf) {
		d = max(d, 500*time.Millisecond)
	}
	if p.waitFg || p.waitBg || p.waiting {
		d = max(d, 500*time.Millisecond)
	}
	return d, true
}

func partialPasteEnd(buf []byte) bool {
	const end = "\033[201~"
	return len(buf) > 0 && len(buf) < len(end) && strings.HasPrefix(end, string(buf))
}

func (p *Parser) run(expired bool) []Event {
	var out []Event
	for len(p.buf) > 0 {
		ev, size, partial := p.next(expired)
		if partial {
			p.timer = true
			return out
		}
		p.timer = false
		expired = false
		if ev != nil {
			ev.Raw = append([]byte(nil), p.buf[:size]...)
			out = append(out, *ev)
		}
		p.buf = p.buf[size:]
	}
	p.timer = false
	return out
}

// result of a sub-parser: 0 complete, -1 no, 1 partial, -2 complete but discarded.
const (
	matched = 0
	noMatch = -1
	partial = 1
	discard = -2
)

// next is tty_keys_next: the next event, its size, or partial.
func (p *Parser) next(expired bool) (*Event, int, bool) {
	buf := p.buf
	replies := []func([]byte) (*Event, int, int){
		p.clipboard, p.syncReply, p.deviceAttributes, p.deviceAttributes2,
		p.extendedDeviceAttributes, p.colours, p.palette,
	}
	for _, f := range replies {
		ev, size, r := f(buf)
		switch r {
		case matched:
			p.updateWaiting()
			return ev, size, false
		case partial:
			if expired {
				continue
			}
			return nil, 0, true
		}
	}
	m, size, r := p.mouse(buf)
	switch r {
	case matched:
		return &Event{Kind: EventKey, Key: k.Mouse, Mouse: m}, size, false
	case discard:
		return nil, size, false
	case partial:
		if !expired {
			return nil, 0, true
		}
	}
	if key, size, r := p.extendedKey(buf); r == matched {
		return &Event{Kind: EventKey, Key: key}, size, false
	} else if r == partial && !expired {
		return nil, 0, true
	}
	if ev, size, r := p.winsize(buf); r == matched {
		return ev, size, false
	} else if r == partial && !expired {
		return nil, 0, true
	}

	key, size, r := p.next1(buf, expired)
	switch r {
	case matched:
		return p.key(key), size, false
	case partial:
		return nil, 0, true
	}
	// A key with an escape prefix: the meta modifier.
	if buf[0] == '\033' && len(buf) > 1 {
		key, size, r := p.next1(buf[1:], expired)
		switch r {
		case matched:
			if key&k.ImpliedMeta != 0 {
				// The xterm sequence includes the escape: this is Escape, then the key.
				return p.key('\033'), 1, false
			}
			return p.key(key | k.Meta), size + 1, false
		case partial:
			return nil, 0, true
		}
	}
	// Not partial: pass the bytes through.
	if buf[0] == '\033' && len(buf) >= 2 {
		key, size = k.Key(buf[1])|k.Meta, 2
	} else {
		key, size = k.Key(buf[0]), 1
	}
	if key&k.MaskKey == k.C0NUL {
		key = ' ' | k.Ctrl | key&k.Meta
	}
	if p.erase >= 0 {
		switch key {
		case k.Key(p.erase):
			key = k.BSpace
		case k.Key(p.erase) | k.Meta:
			key = k.BSpace | k.Meta
		}
	}
	// C0 controls without a key of their own are Ctrl keys: ^A is C-a.
	if only := key & k.MaskKey; only < 0x20 && only != k.C0HT && only != k.C0CR && only != k.C0ESC {
		only |= 0x40
		if only >= 'A' && only <= 'Z' {
			only |= 0x20
		}
		key = only | k.Ctrl | key&k.Meta
	}
	return p.key(key), size, false
}

// key makes a key event, keeping track of bracketed paste.
func (p *Parser) key(key k.Key) *Event {
	switch key & k.MaskKey {
	case k.PasteStart:
		p.paste = true
	case k.PasteEnd:
		p.paste = false
	}
	return &Event{Kind: EventKey, Key: key}
}

// next1 is tty_keys_next1: a key from the tree, or a UTF-8 character.
func (p *Parser) next1(buf []byte, expired bool) (k.Key, int, int) {
	if n, size := p.find(buf); n != nil && n.key != k.Unknown {
		if n.next != nil && !expired {
			return 0, 0, partial
		}
		return n.key, size, matched
	}
	c := buf[0]
	if c < 0xc2 || c > 0xf4 {
		return 0, 0, noMatch
	}
	need := 2
	switch {
	case c >= 0xf0:
		need = 4
	case c >= 0xe0:
		need = 3
	}
	if len(buf) < need {
		if !expired {
			return 0, 0, partial
		}
		return 0, 0, noMatch
	}
	r, size := utf8.DecodeRune(buf[:need])
	if r == utf8.RuneError || size != need {
		return 0, 0, noMatch
	}
	return k.Unicode(r), need, matched
}

// extendedKey is tty_keys_extended_key: CSI 27 ; m ; k ~ and CSI k ; m u.
func (p *Parser) extendedKey(buf []byte) (k.Key, int, int) {
	if r := prefix(buf, "\033["); r != matched {
		return 0, 0, r
	}
	end := 2
	for ; end < len(buf) && end != 64; end++ {
		if buf[end] == '~' || (!isDigit(buf[end]) && buf[end] != ';') {
			break
		}
	}
	if end == len(buf) {
		return 0, 0, partial
	}
	if end == 64 || (buf[end] != '~' && buf[end] != 'u') {
		return 0, 0, noMatch
	}
	args := strings.Split(string(buf[2:end]), ";")
	var number, modifiers int
	if buf[end] == '~' {
		if len(args) != 3 || args[0] != "27" || !atoi(args[1], &modifiers) || !atoi(args[2], &number) {
			return 0, 0, noMatch
		}
	} else if len(args) < 2 || !atoi(args[0], &number) || !atoi(args[1], &modifiers) {
		return 0, 0, noMatch
	}
	size := end + 1
	var nkey k.Key
	if p.erase >= 0 && number == p.erase {
		nkey = k.BSpace
	} else {
		nkey = k.Key(number)
	}
	if nkey != k.BSpace && nkey&^0x7f != 0 {
		if !utf8.ValidRune(rune(number)) {
			return 0, 0, noMatch
		}
		nkey = k.Unicode(rune(number))
	}
	if modifiers > 0 {
		modifiers--
		if modifiers&1 != 0 {
			nkey |= k.Shift
		}
		if modifiers&2 != 0 {
			nkey |= k.Meta | k.ImpliedMeta // Alt
		}
		if modifiers&4 != 0 {
			nkey |= k.Ctrl
		}
		if modifiers&8 != 0 {
			nkey |= k.Meta | k.ImpliedMeta // Meta
		}
	}
	if nkey&k.MaskKey == '\011' && nkey&k.Shift != 0 {
		nkey = k.BTab | nkey&^k.MaskKey&^k.Shift
	}
	// Shift alone on a printable character is dropped: S-a arrives as A or S-A.
	if only := nkey & k.MaskKey; ((only > 0x20 && only < 0x7f) || nkey.IsUnicode()) && nkey&k.MaskModifiers == k.Shift {
		nkey &^= k.Shift
	}
	return nkey, size, matched
}

// Mouse button offsets (tmux's MOUSE_PARAM_*).
const (
	mouseBtnOff = 32
	mousePosOff = 33
	// mouseMotion is MOUSE_MASK_DRAG: a report of the mouse moving.
	mouseMotion = 32
)

// mouse is tty_keys_mouse: X10 (CSI M b x y) and SGR (CSI < b ; x ; y M/m) reports.
func (p *Parser) mouse(buf []byte) (Mouse, int, int) {
	var x, y, b, sgrB int
	sgrType := byte(' ')
	if r := prefix(buf, "\033["); r != matched {
		return Mouse{}, 0, r
	}
	size := 3
	switch buf[2] {
	case 'M':
		var v [3]int
		for i := range v {
			if len(buf) <= size {
				return Mouse{}, 0, partial
			}
			v[i] = int(buf[size])
			size++
		}
		b, x, y = v[0], v[1], v[2]
		if b < mouseBtnOff || x < mousePosOff || y < mousePosOff {
			return Mouse{}, size, discard
		}
		b -= mouseBtnOff
		x -= mousePosOff
		y -= mousePosOff
	case '<':
		var ch byte
		read := func(stop string, n *int) int {
			for {
				if len(buf) <= size {
					return partial
				}
				ch = buf[size]
				size++
				if strings.IndexByte(stop, ch) >= 0 {
					return matched
				}
				if !isDigit(ch) {
					return noMatch
				}
				*n = 10**n + int(ch-'0')
			}
		}
		for _, f := range []struct {
			stop string
			n    *int
		}{{";", &sgrB}, {";", &x}, {"Mm", &y}} {
			if r := read(f.stop, f.n); r != matched {
				return Mouse{}, 0, r
			}
		}
		if x < 1 || y < 1 {
			return Mouse{}, size, discard
		}
		x--
		y--
		b = sgrB
		sgrType = ch
		// A motion is never a release: the Windows console (conhost) ends a motion with no
		// button held (ESC [ < 35 ; x ; y m) the way xterm ends a release. Read as one, it
		// chose or closed a -M menu as soon as the mouse moved over it; take it as xterm's M.
		if sgrType == 'm' && sgrB&mouseMotion != 0 && sgrB&64 == 0 {
			sgrType = 'M'
		}
		if sgrType == 'm' {
			b = 3
		}
		// A release of a wheel button is a terminal's mistake: drop it.
		if sgrType == 'm' && sgrB&64 != 0 {
			return Mouse{}, size, discard
		}
	default:
		return Mouse{}, 0, noMatch
	}
	m := Mouse{
		X: x, Y: y, B: b, LX: p.lastMouse.X, LY: p.lastMouse.Y, LB: p.lastMouse.B,
		SGRType: sgrType, SGRB: sgrB,
	}
	p.lastMouse = Mouse{X: x, Y: y, B: b}
	return m, size, matched
}

// prefix checks that buf starts with s: matched, noMatch, or partial when buf is shorter.
func prefix(buf []byte, s string) int {
	for i := 0; i < len(s); i++ {
		if i == len(buf) {
			return partial
		}
		if buf[i] != s[i] {
			return noMatch
		}
	}
	if len(buf) == len(s) {
		return partial
	}
	return matched
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func atoi(s string, n *int) bool {
	if s == "" {
		return false
	}
	v := 0
	for i := 0; i < len(s); i++ {
		if !isDigit(s[i]) {
			return false
		}
		v = v*10 + int(s[i]-'0')
		if v > 1<<31-1 {
			return false
		}
	}
	*n = v
	return true
}
