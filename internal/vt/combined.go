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

import "strings"

// Combining rules, ported from tmux 3.8's utf8-combined.c.

const (
	zwj           = "\u200d"
	vs16          = "\ufe0f"
	hangulFiller  = "\u3164"
	replacementCh = "\ufffd"
)

func hasZWJ(s string) bool         { return strings.HasSuffix(s, zwj) }
func isZWJ(s string) bool          { return s == zwj }
func isVS(s string) bool           { return s == vs16 }
func isHangulFiller(s string) bool { return s == hangulFiller }

func regionalCount(s string) int {
	n := 0
	for i := 0; i+4 <= len(s); i++ {
		if s[i] == 0xf0 && s[i+1] == 0x9f && s[i+2] == 0x87 && s[i+3] >= 0xa6 && s[i+3] <= 0xbf {
			n++
		}
	}
	return n
}

func firstRune(s string) (rune, bool) { return DecodeChar([]byte(s)) }

// shouldCombine reports whether add joins with: a pair of regional indicators (a flag), or a
// skin tone modifier after an emoji that takes one.
func shouldCombine(with, add string) bool {
	w, ok1 := firstRune(with)
	a, ok2 := firstRune(add)
	if !ok1 || !ok2 {
		return false
	}
	if a >= 0x1F1E6 && a <= 0x1F1FF && w >= 0x1F1E6 && w <= 0x1F1FF {
		return regionalCount(with) == 1 && regionalCount(add) == 1
	}
	switch a {
	case 0x1F44B, 0x1F44C, 0x1F44D, 0x1F44E, 0x1F44F, 0x1F450, 0x1F466, 0x1F467, 0x1F468,
		0x1F469, 0x1F46E, 0x1F470, 0x1F471, 0x1F472, 0x1F473, 0x1F474, 0x1F475, 0x1F476,
		0x1F477, 0x1F478, 0x1F47C, 0x1F481, 0x1F482, 0x1F483, 0x1F485, 0x1F486, 0x1F487,
		0x1F4AA, 0x1F575, 0x1F57A, 0x1F590, 0x1F595, 0x1F596, 0x1F645, 0x1F646, 0x1F647,
		0x1F64B, 0x1F64C, 0x1F64D, 0x1F64E, 0x1F64F, 0x1F6B4, 0x1F6B5, 0x1F6B6, 0x1F926,
		0x1F937, 0x1F938, 0x1F939, 0x1F93D, 0x1F93E, 0x1F9B5, 0x1F9B6, 0x1F9B8, 0x1F9B9,
		0x1F9CD, 0x1F9CE, 0x1F9CF, 0x1F9D1, 0x1F9D2, 0x1F9D3, 0x1F9D4, 0x1F9D5, 0x1F9D6,
		0x1F9D7, 0x1F9D8, 0x1F9D9, 0x1F9DA, 0x1F9DB, 0x1F9DC, 0x1F9DD, 0x1F9DE, 0x1F9DF:
		return w >= 0x1F3FB && w <= 0x1F3FF
	}
	return false
}

type jamoClass int

const (
	jamoNone jamoClass = iota
	jamoChoseong
	jamoJungseong
	jamoJongseong
)

// jamoClassOf classifies the three bytes at s as tmux's hanguljamo_get_class does.
func jamoClassOf(s string) jamoClass {
	if len(s) < 3 {
		return jamoNone
	}
	a, b, c := s[0], s[1], s[2]
	switch a {
	case 0xe1:
		switch b {
		case 0x84:
			if c >= 0x80 && c <= 0xbf {
				return jamoChoseong // choseong and old choseong
			}
		case 0x85:
			switch {
			case c == 0x9f || (c >= 0x80 && c <= 0x9e):
				return jamoChoseong // filler and old choseong
			case c == 0xa0 || (c >= 0xa1 && c <= 0xbf):
				return jamoJungseong // filler, jungseong and old jungseong
			}
		case 0x86:
			if c >= 0x80 && c <= 0xa7 {
				return jamoJungseong
			}
			if c >= 0xa8 && c <= 0xbf {
				return jamoJongseong
			}
		case 0x87:
			if c >= 0x80 && c <= 0xbf {
				return jamoJongseong
			}
		}
	case 0xea:
		if b == 0xa5 && c >= 0xa0 && c <= 0xbc {
			return jamoChoseong
		}
	case 0xed:
		if b == 0x9e && c >= 0xb0 && c <= 0xbf {
			return jamoJungseong
		}
		if b == 0x9f {
			if c >= 0x80 && c <= 0x86 {
				return jamoJungseong
			}
			if c >= 0x8b && c <= 0xbb {
				return jamoJongseong
			}
		}
	}
	return jamoNone
}

type jamoState int

const (
	jamoStateNotJamo jamoState = iota
	jamoStateChoseong
	jamoStateComposable
	jamoStateNotComposable
)

// jamoCheck is tmux's hanguljamo_check_state: can add, a Hangul jamo, join prev?
func jamoCheck(prev, add string) jamoState {
	if len(add) != 3 {
		return jamoStateNotJamo
	}
	switch jamoClassOf(add) {
	case jamoChoseong:
		return jamoStateChoseong
	case jamoJungseong:
		if len(prev) < 3 {
			return jamoStateNotComposable
		}
		if jamoClassOf(prev[len(prev)-3:]) == jamoChoseong {
			return jamoStateComposable
		}
		return jamoStateNotComposable
	case jamoJongseong:
		if len(prev) < 3 {
			return jamoStateNotComposable
		}
		if jamoClassOf(prev[len(prev)-3:]) == jamoJungseong {
			return jamoStateComposable
		}
		return jamoStateNotComposable
	}
	return jamoStateNotJamo
}
