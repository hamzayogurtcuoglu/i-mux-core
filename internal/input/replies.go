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
	"encoding/base64"
	"strconv"
	"strings"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/vt"
)

// The terminal's replies to the server's queries (tty_send_requests): they are taken out of
// the input before keys are looked for. Each returns the event, its size, and matched, noMatch
// or partial.

func (p *Parser) updateWaiting() {
	p.waiting = !p.haveDA || !p.haveDA2 || !p.haveXDA || !p.haveSync
}

// stringReply reads an OSC or DCS reply's text after head, up to BEL or ST (at most max-1
// bytes): the text, the reply's size, and the result.
func stringReply(buf []byte, head string, max int, bel bool) (string, int, int) {
	if r := prefix(buf, head); r != matched {
		return "", 0, r
	}
	n := len(head)
	i := 0
	for ; i < max-1; i++ {
		if n+i == len(buf) {
			return "", 0, partial
		}
		if buf[n+i-1] == '\033' && buf[n+i] == '\\' {
			break
		}
		if bel && buf[n+i] == '\007' {
			break
		}
	}
	if i == max-1 {
		return "", 0, noMatch
	}
	text := string(buf[n : n+i])
	text = strings.TrimSuffix(text, "\033")
	return text, n + i + 1, matched
}

// clipboard is tty_keys_clipboard: OSC 52 ; c ; base64 BEL or ST.
func (p *Parser) clipboard(buf []byte) (*Event, int, int) {
	if r := prefix(buf, "\033]52;"); r != matched {
		return nil, 0, r
	}
	end, term := 5, 0
	for ; end < len(buf); end++ {
		if buf[end] == '\007' {
			term = 1
			break
		}
		if end > 5 && buf[end-1] == '\033' && buf[end] == '\\' {
			term = 2
			break
		}
	}
	if end == len(buf) {
		return nil, 0, partial
	}
	size := end + 1
	body := string(buf[5 : end-(term-1)])
	ev := &Event{Kind: EventClipboard}
	if len(body) >= 2 && body[0] != ';' && body[1] == ';' {
		ev.Clip = body[0]
	}
	_, data, ok := strings.Cut(body, ";")
	if !ok || data == "" {
		return nil, size, matched
	}
	b, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return nil, size, matched
	}
	ev.Data = b
	return ev, size, matched
}

// attributes reads CSI <lead> params c: the parameters and the size.
func attributes(buf []byte, lead string) ([]int, int, int) {
	if r := prefix(buf, lead); r != matched {
		return nil, 0, r
	}
	n := len(lead)
	i := 0
	for ; i < 128; i++ {
		if n+i == len(buf) {
			return nil, 0, partial
		}
		if buf[n+i] >= 'a' && buf[n+i] <= 'z' {
			break
		}
	}
	if i == 128 || buf[n+i] != 'c' {
		return nil, 0, noMatch
	}
	var params []int
	for _, f := range strings.Split(string(buf[n:n+i]), ";") {
		v, err := strconv.Atoi(f)
		if err != nil {
			v = 0
		}
		params = append(params, v)
		if len(params) == 32 {
			break
		}
	}
	return params, n + i + 1, matched
}

// deviceAttributes is tty_keys_device_attributes: CSI ? params c.
func (p *Parser) deviceAttributes(buf []byte) (*Event, int, int) {
	if p.haveDA {
		return nil, 0, noMatch
	}
	params, size, r := attributes(buf, "\033[?")
	if r != matched {
		return nil, 0, r
	}
	var feat []string
	if len(params) > 0 && params[0] >= 61 && params[0] <= 65 {
		for _, n := range params[1:] {
			switch n {
			case 4:
				feat = append(feat, "sixel")
			case 21:
				feat = append(feat, "margins")
			case 28:
				feat = append(feat, "rectfill")
			case 52:
				feat = append(feat, "clipboard")
			}
		}
	}
	p.haveDA = true
	return &Event{Kind: EventDA, Features: strings.Join(feat, ",")}, size, matched
}

// deviceAttributes2 is tty_keys_device_attributes2: CSI > params c.
func (p *Parser) deviceAttributes2(buf []byte) (*Event, int, int) {
	if p.haveDA2 {
		return nil, 0, noMatch
	}
	params, size, r := attributes(buf, "\033[>")
	if r != matched {
		return nil, 0, r
	}
	ev := &Event{Kind: EventDA2}
	if len(params) > 0 {
		switch params[0] {
		case 'M':
			ev.Name = "mintty"
		case 'T':
			ev.Name = "tmux"
		case 'U':
			ev.Name = "rxvt-unicode"
		}
	}
	p.haveDA2 = true
	return ev, size, matched
}

// extendedDeviceAttributes is tty_keys_extended_device_attributes: DCS > | text ST.
func (p *Parser) extendedDeviceAttributes(buf []byte) (*Event, int, int) {
	if p.haveXDA {
		return nil, 0, noMatch
	}
	text, size, r := stringReply(buf, "\033P>|", 128, false)
	if r != matched {
		return nil, 0, r
	}
	ev := &Event{Kind: EventXDA, TermType: text}
	for _, t := range []struct{ prefix, name string }{
		{"iTerm2 ", "iTerm2"},
		{"tmux ", "tmux"},
		{"XTerm(", "XTerm"},
		{"mintty ", "mintty"},
		{"foot(", "foot"},
		{"WezTerm ", "WezTerm"},
		{"ghostty ", "ghostty"},
		{"Rio ", "Rio"},
	} {
		if strings.HasPrefix(text, t.prefix) {
			ev.Name = t.name
			break
		}
	}
	p.haveXDA = true
	return ev, size, matched
}

// syncReply is tty_keys_sync: the DECRPM reply to the query for mode 2026.
func (p *Parser) syncReply(buf []byte) (*Event, int, int) {
	if p.haveSync {
		return nil, 0, noMatch
	}
	const head = "\033[?2026;"
	if r := prefix(buf, head); r != matched {
		return nil, 0, r
	}
	i := len(head)
	if buf[i] < '0' || buf[i] > '4' {
		return nil, 0, noMatch
	}
	status := buf[i] - '0'
	i++
	for _, c := range []byte("$y") {
		if i == len(buf) {
			return nil, 0, partial
		}
		if buf[i] != c {
			return nil, 0, noMatch
		}
		i++
	}
	p.haveSync = true
	ev := &Event{Kind: EventSync}
	if status >= 1 && status <= 3 {
		ev.Features = "sync"
	}
	return ev, i, matched
}

// colours is tty_keys_colours: OSC 10 or 11 ; colour BEL or ST.
func (p *Parser) colours(buf []byte) (*Event, int, int) {
	if r := prefix(buf, "\033]1"); r != matched {
		return nil, 0, r
	}
	if buf[3] != '0' && buf[3] != '1' {
		return nil, 0, noMatch
	}
	head := "\033]1" + string(buf[3]) + ";"
	text, size, r := stringReply(buf, head, 128, true)
	if r != matched {
		return nil, 0, r
	}
	ev := &Event{Kind: EventColour, Fg: -1, Bg: -1}
	if text == "" {
		return ev, size, matched
	}
	if c := vt.ParseX11Color(text); c != -1 {
		if buf[3] == '0' {
			ev.Fg = int(c)
			p.waitFg = false
		} else {
			ev.Bg = int(c)
			p.waitBg = false
		}
	}
	return ev, size, matched
}

// palette is tty_keys_palette: OSC 4 ; index ; colour BEL or ST.
func (p *Parser) palette(buf []byte) (*Event, int, int) {
	text, size, r := stringReply(buf, "\033]4;", 128, true)
	if r != matched {
		return nil, 0, r
	}
	if text == "" {
		return nil, size, matched
	}
	idx, rest, ok := strings.Cut(text, ";")
	n, err := strconv.Atoi(idx)
	if !ok || err != nil || n < 0 || n > 255 {
		return nil, 0, noMatch
	}
	c := vt.ParseX11Color(rest)
	if c == -1 {
		return nil, size, matched
	}
	return &Event{Kind: EventPalette, Index: n, Colour: int(c)}, size, matched
}

// winsize is tty_keys_winsz: the answers to CSI 18 t and CSI 14 t, read only while the server
// waits for them. tmux reads the digits and semicolons up to t, then the two forms.
func (p *Parser) winsize(buf []byte) (*Event, int, int) {
	if !p.WinsizeQuery {
		return nil, 0, noMatch
	}
	if r := prefix(buf, "\033["); r != matched {
		return nil, 0, r
	}
	end := 2
	for ; end < len(buf) && end != 64; end++ {
		if buf[end] == 't' {
			break
		}
		if !isDigit(buf[end]) && buf[end] != ';' {
			break
		}
	}
	if end == len(buf) {
		return nil, 0, partial
	}
	if end == 64 || buf[end] != 't' {
		return nil, 0, noMatch
	}
	f := strings.Split(string(buf[2:end]), ";")
	if len(f) < 3 {
		return nil, 0, noMatch
	}
	a, err1 := strconv.Atoi(f[1])
	b, err2 := strconv.Atoi(f[2])
	if err1 != nil || err2 != nil {
		return nil, 0, noMatch
	}
	switch f[0] {
	case "8":
		return &Event{Kind: EventWinsize, Rows: a, Cols: b}, end + 1, matched
	case "4":
		p.WinsizeQuery = false
		return &Event{Kind: EventWinsize, YPixel: a, XPixel: b}, end + 1, matched
	}
	return nil, 0, noMatch
}
