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

import "math"

// ParamKind says what a parameter is, as in tmux's input_param.
type ParamKind uint8

// Parameter kinds.
const (
	ParamMissing ParamKind = iota // empty: ";;"
	ParamNumber                   // digits
	ParamString                   // holds a ':' (sub-parameters, as in "4:3" or "38:2::1:2:3")
)

// Params are a sequence's parameters, split at ';' as tmux's input_split does.
type Params struct {
	n    int
	kind [maxParams]ParamKind
	num  [maxParams]int
	str  [maxParams][]byte
	raw  []byte
}

// split parses raw; it fails, and the sequence is ignored, where tmux's input_split fails: a
// number above INT_MAX or one with a byte that is not a digit, or too many parameters.
func (p *Params) split(raw []byte) bool {
	p.n = 0
	p.raw = raw
	if len(raw) == 0 {
		return true
	}
	start := 0
	for i := 0; i <= len(raw); i++ {
		if i < len(raw) && raw[i] != ';' {
			continue
		}
		field := raw[start:i]
		start = i + 1
		switch {
		case len(field) == 0:
			p.kind[p.n] = ParamMissing
		case containsColon(field):
			p.kind[p.n] = ParamString
			p.str[p.n] = field
		default:
			v, ok := atoi(field)
			if !ok {
				return false
			}
			p.kind[p.n] = ParamNumber
			p.num[p.n] = v
		}
		p.n++
		if p.n == maxParams {
			return false
		}
	}
	return true
}

func containsColon(b []byte) bool {
	for _, c := range b {
		if c == ':' {
			return true
		}
	}
	return false
}

// atoi is strtonum(s, 0, INT_MAX): digits only (the parser lets only digits, ':' and ';'
// reach here, but private markers such as '?' arrive as intermediates), and at most INT_MAX.
func atoi(b []byte) (int, bool) {
	v := 0
	for _, c := range b {
		if c < '0' || c > '9' {
			return 0, false
		}
		v = v*10 + int(c-'0')
		if v > math.MaxInt32 {
			return 0, false
		}
	}
	return v, true
}

// Len returns the number of parameters.
func (p *Params) Len() int { return p.n }

// Kind returns the kind of parameter i, or ParamMissing past the end.
func (p *Params) Kind(i int) ParamKind {
	if i >= p.n {
		return ParamMissing
	}
	return p.kind[i]
}

// Get returns parameter i as tmux's input_get does: def when it is missing or past the end,
// -1 when it is a string, else the number raised to at least min.
func (p *Params) Get(i, min, def int) int {
	if i >= p.n || p.kind[i] == ParamMissing {
		return def
	}
	if p.kind[i] == ParamString {
		return -1
	}
	if p.num[i] < min {
		return min
	}
	return p.num[i]
}

// String returns parameter i's text when it is a string ("4:3"), else nil.
func (p *Params) String(i int) []byte {
	if i >= p.n || p.kind[i] != ParamString {
		return nil
	}
	return p.str[i]
}

// Raw returns the parameter bytes as received.
func (p *Params) Raw() []byte { return p.raw }
