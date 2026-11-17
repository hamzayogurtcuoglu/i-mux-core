// Ported from tmux 3.8's tty-acs.c:
//
// Copyright (c) 2010 Nicholas Marriott <nicholas.marriott@gmail.com>
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

import "sync"

// acsUTF8 maps the VT100 line drawing characters to UTF-8, for a terminal that draws lines
// with UTF-8 rather than its alternate character set (tmux's tty_acs_table).
var acsUTF8 = sync.OnceValue(func() map[byte]string {
	return map[byte]string{
		'+': "→", // arrow pointing right
		',': "←", // arrow pointing left
		'-': "↑", // arrow pointing up
		'.': "↓", // arrow pointing down
		'0': "▮", // solid square block
		'`': "◆", // diamond
		'a': "▒", // checker board (stipple)
		'b': "␉",
		'c': "␌",
		'd': "␍",
		'e': "␊",
		'f': "°", // degree symbol
		'g': "±", // plus/minus
		'h': "␤",
		'i': "␋",
		'j': "┘", // lower right corner
		'k': "┐", // upper right corner
		'l': "┌", // upper left corner
		'm': "└", // lower left corner
		'n': "┼", // large plus or crossover
		'o': "⎺", // scan line 1
		'p': "⎻", // scan line 3
		'q': "─", // horizontal line
		'r': "⎼", // scan line 7
		's': "⎽", // scan line 9
		't': "├", // tee pointing right
		'u': "┤", // tee pointing left
		'v': "┴", // tee pointing up
		'w': "┬", // tee pointing down
		'x': "│", // vertical line
		'y': "≤", // less-than-or-equal-to
		'z': "≥", // greater-than-or-equal-to
		'{': "π", // greek pi
		'|': "≠", // not-equal
		'}': "£", // UK pound sign
		'~': "·", // bullet
	}
})

// acsReverse maps UTF-8 line drawing back to the VT100 set, for a terminal that is not UTF-8
// (tmux's tty_acs_reverse2 and tty_acs_reverse3).
var acsReverse = sync.OnceValue(func() map[string]byte {
	return map[string]byte{
		"·": '~',
		"─": 'q', "━": 'q', "│": 'x', "┃": 'x',
		"┌": 'l', "┏": 'k', "┐": 'k', "┓": 'l',
		"└": 'm', "┗": 'm', "┘": 'j', "┛": 'j',
		"├": 't', "┣": 't', "┤": 'u', "┫": 'u',
		"┳": 'w', "┴": 'v', "┻": 'v', "┼": 'n',
		"╋": 'n', "═": 'q', "║": 'x', "╔": 'l',
		"╗": 'k', "╚": 'm', "╝": 'j', "╠": 't',
		"╣": 'u', "╦": 'w', "╩": 'v', "╬": 'n',
	}
})

// acsGet returns what to write for line drawing character ch: the terminal's own alternate
// character when it needs ACS, else UTF-8; "" when there is none (tmux's tty_acs_get).
func (t *TTY) acsGet(ch byte) string {
	if t.acsNeeded() {
		if c := t.Term.acs[ch]; c != 0 {
			return string(c)
		}
		return ""
	}
	return acsUTF8()[ch]
}

// ACSUTF8 is tty_acs_get with no terminal: the UTF-8 form of an alternate-charset character,
// or "" (copy mode copies line-drawing characters so).
func ACSUTF8(ch byte) string { return acsUTF8()[ch] }
