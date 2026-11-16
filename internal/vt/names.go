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

// Validating and escaping names (titles, paths), ported from tmux 3.8's utf8.c (utf8_isvalid,
// utf8_strvis) and tmux.c (clean_name).

// utf8IsValid reports whether s contains only printable ASCII (0x20..0x7e) and
// valid multi-byte UTF-8 sequences (tmux's utf8_isvalid).
func utf8IsValid(s string) bool {
	for i := 0; i < len(s); {
		b := s[i]
		if b >= 0x80 {
			size := utf8SeqSize(b)
			if size == 0 || i+size > len(s) {
				return false
			}
			for j := 1; j < size; j++ {
				if s[i+j]&0xc0 != 0x80 {
					return false
				}
			}
			if _, ok := DecodeChar([]byte(s[i : i+size])); !ok {
				return false
			}
			i += size
			continue
		}
		if b < 0x20 || b > 0x7e {
			return false
		}
		i++
	}
	return true
}

func utf8SeqSize(b byte) int {
	switch {
	case b >= 0xc2 && b <= 0xdf:
		return 2
	case b >= 0xe0 && b <= 0xef:
		return 3
	case b >= 0xf0 && b <= 0xf4:
		return 4
	default:
		return 0
	}
}

// utf8Strvis encodes s as tmux's utf8_strvis does.
func utf8Strvis(s string, flag int) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		c := s[i]
		if size := utf8SeqSize(c); size > 0 && i+size <= len(s) {
			valid := true
			for j := 1; j < size; j++ {
				if s[i+j]&0xc0 != 0x80 {
					valid = false
					break
				}
			}
			if valid {
				if _, ok := DecodeChar([]byte(s[i : i+size])); ok {
					b.WriteString(s[i : i+size])
					i += size
					continue
				}
			}
		}
		if flag&visDQ != 0 && c == '$' && i+1 < len(s) {
			if n := s[i+1]; n >= 'a' && n <= 'z' || n >= 'A' && n <= 'Z' || n == '_' || n == '{' {
				b.WriteByte('\\')
			}
			b.WriteByte('$')
			i++
			continue
		}
		var nextc byte
		if i+1 < len(s) {
			nextc = s[i+1]
		}
		visByte(&b, c, flag, nextc)
		i++
	}
	return b.String()
}

// ArgVis encodes a command argument as tmux's args_escape does before quoting it
// (utf8_stravis with VIS_OCTAL, VIS_CSTYLE, VIS_TAB and VIS_NL, and VIS_DQ inside "…").
func ArgVis(s string, doubleQuoted bool) string {
	flag := visOctal | visCStyle | visTab | visNL
	if doubleQuoted {
		flag |= visDQ
	}
	return utf8Strvis(s, flag)
}

// cleanName sanitizes a title or path as tmux's clean_name does.
func cleanName(name string, untrusted bool) (string, bool) {
	if !utf8IsValid(name) {
		return "", false
	}
	if untrusted && strings.Contains(name, "#(") {
		b := []byte(name)
		for i := 0; i+1 < len(b); i++ {
			if b[i] == '#' && b[i+1] == '(' {
				b[i] = '_'
			}
		}
		name = string(b)
	}
	return utf8Strvis(name, visOctal|visCStyle|visTab|visNL), true
}

// StrnVis encodes s byte by byte as tmux's strnvis with VIS_OCTAL, VIS_CSTYLE, VIS_TAB and
// VIS_NL does into a buffer of size bytes: it stops before an encoding that does not fit.
func StrnVis(s string, size int) string {
	var b strings.Builder
	limit := size - 1
	for i := 0; i < len(s); i++ {
		var nextc byte
		if i+1 < len(s) {
			nextc = s[i+1]
		}
		var one strings.Builder
		visByte(&one, s[i], visOctal|visCStyle|visTab|visNL, nextc)
		if b.Len()+one.Len() > limit {
			break
		}
		b.WriteString(one.String())
	}
	return b.String()
}

// CleanName is tmux's clean_name: a name made safe to show (control characters encoded), or
// false when it is not valid UTF-8.
func CleanName(name string, untrusted bool) (string, bool) { return cleanName(name, untrusted) }

// PasteVis is what paste-buffer writes of a buffer (utf8_stravisx with VIS_SAFE and
// VIS_NOSLASH): control characters other than tab, newline, backspace, BEL and CR as ^X,
// bytes that are not UTF-8 as M-x or M^X.
func PasteVis(s string) string { return utf8Strvis(s, visSafe|visNoSlash) }
