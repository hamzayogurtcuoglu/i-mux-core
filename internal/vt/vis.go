// Copyright (c) 1989, 1993
//	The Regents of the University of California.  All rights reserved.
//
// Redistribution and use in source and binary forms, with or without
// modification, are permitted provided that the following conditions
// are met:
// 1. Redistributions of source code must retain the above copyright
//    notice, this list of conditions and the following disclaimer.
// 2. Redistributions in binary form must reproduce the above copyright
//    notice, this list of conditions and the following disclaimer in the
//    documentation and/or other materials provided with the distribution.
// 3. Neither the name of the University nor the names of its contributors
//    may be used to endorse or promote products derived from this software
//    without specific prior written permission.
//
// THIS SOFTWARE IS PROVIDED BY THE REGENTS AND CONTRIBUTORS ``AS IS'' AND
// ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE
// IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE
// ARE DISCLAIMED.  IN NO EVENT SHALL THE REGENTS OR CONTRIBUTORS BE LIABLE
// FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL
// DAMAGES (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS
// OR SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION)
// HOWEVER CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT
// LIABILITY, OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY
// OUT OF THE USE OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF
// SUCH DAMAGE.

package vt

import "strings"

// vis(3), ported from the copy in tmux 3.8's compat/vis.c (OpenBSD vis.c 1.24), for the
// flags tmux uses.

// Vis flags from compat.h.
const (
	visOctal  = 0x01
	visCStyle = 0x02
	visTab    = 0x08
	visNL     = 0x10
)

func isVisVisible(c byte, flag int) bool {
	if c <= 0x7f {
		if c >= 0x21 && c <= 0x7e {
			return true
		}
		if c == ' ' {
			return true
		}
		if flag&visTab == 0 && c == '\t' {
			return true
		}
		if flag&visNL == 0 && c == '\n' {
			return true
		}
	}
	return false
}

func visByte(b *strings.Builder, c byte, flag int, nextc byte) {
	if isVisVisible(c, flag) {
		if c == '\\' {
			b.WriteByte('\\')
		}
		b.WriteByte(c)
		return
	}
	if flag&visCStyle != 0 {
		switch c {
		case '\n':
			b.WriteString("\\n")
			return
		case '\r':
			b.WriteString("\\r")
			return
		case '\b':
			b.WriteString("\\b")
			return
		case '\a':
			b.WriteString("\\a")
			return
		case '\v':
			b.WriteString("\\v")
			return
		case '\t':
			b.WriteString("\\t")
			return
		case '\f':
			b.WriteString("\\f")
			return
		case ' ':
			b.WriteString("\\s")
			return
		case 0:
			b.WriteString("\\0")
			if nextc >= '0' && nextc <= '7' {
				b.WriteString("00")
			}
			return
		}
	}
	if (c&0x7f) == ' ' || flag&visOctal != 0 {
		b.WriteByte('\\')
		b.WriteByte((c>>6)&0x7 + '0')
		b.WriteByte((c>>3)&0x7 + '0')
		b.WriteByte(c&0x7 + '0')
		return
	}
	b.WriteByte('\\')
	if c&0x80 != 0 {
		c &= 0x7f
		b.WriteByte('M')
	}
	if c < 0x20 || c == 0x7f {
		b.WriteByte('^')
		if c == 0x7f {
			b.WriteByte('?')
		} else {
			b.WriteByte(c + '@')
		}
	} else {
		b.WriteByte('-')
		b.WriteByte(c)
	}
}
