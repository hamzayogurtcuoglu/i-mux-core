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

import (
	"encoding/base64"
	"math"
	"strconv"
	"strings"
)

// OSC, DCS, APC and window-rename handlers, ported from tmux 3.8's input.c and screen.c.

const maxTitleStack = 10

// SetTitle sets the screen's title after sanitizing it as tmux's screen_set_title does.
// When untrusted is true (sequences from the pane), invalid UTF-8 or control bytes are rejected.
func (s *Screen) SetTitle(title string, untrusted bool) bool {
	cleaned, ok := cleanName(title, untrusted)
	if !ok {
		return false
	}
	s.Title = cleaned
	return true
}

// PushTitle pushes the current title onto the title stack (CSI 22;0t / 22;2t).
func (s *Screen) PushTitle() {
	if len(s.Titles) >= maxTitleStack {
		s.Titles = s.Titles[:maxTitleStack-1]
	}
	s.Titles = append([]string{s.Title}, s.Titles...)
}

// PopTitle pops the top title from the title stack (CSI 23;0t / 23;2t). The stored title is
// already clean; with an empty stack nothing changes (checked on tmux 3.8).
func (s *Screen) PopTitle() {
	if len(s.Titles) == 0 {
		return
	}
	s.Title = s.Titles[0]
	s.Titles = s.Titles[1:]
}

// SetPath sets the screen's path from OSC 7 (tmux's screen_set_path) and decodes
// any file://host/path URI into s.Cwd (COMPATIBILITY.md K1).
func (s *Screen) SetPath(raw string) {
	cleaned, ok := cleanName(raw, true)
	if !ok {
		return
	}
	s.Path = cleaned
	if raw == "" {
		s.Cwd = ""
		return
	}
	if cwd, ok := parseOSC7Path(raw); ok {
		s.Cwd = cwd
	}
}

func parseOSC7Path(raw string) (string, bool) {
	if !strings.HasPrefix(raw, "file://") {
		if utf8IsValid(raw) {
			return raw, true
		}
		return "", false
	}
	rest := raw[len("file://"):]
	slash := strings.IndexByte(rest, '/')
	if slash == -1 {
		return "", false
	}
	pathPart := rest[slash:]
	decoded, ok := percentDecode(pathPart)
	if !ok || !utf8IsValid(decoded) {
		return "", false
	}
	// On Windows file:///C:/foo has a leading slash before the drive letter.
	if len(decoded) >= 3 && decoded[0] == '/' && isASCIILetter(decoded[1]) && decoded[2] == ':' {
		decoded = decoded[1:]
	}
	return decoded, true
}

func isASCIILetter(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

func percentDecode(s string) (string, bool) {
	if strings.IndexByte(s, '%') < 0 {
		return s, true
	}
	b := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '%' {
			if i+2 >= len(s) {
				return "", false
			}
			hi, ok1 := fromHex(s[i+1])
			lo, ok2 := fromHex(s[i+2])
			if !ok1 || !ok2 {
				return "", false
			}
			b = append(b, (hi<<4)|lo)
			i += 2
			continue
		}
		b = append(b, s[i])
	}
	return string(b), true
}

func fromHex(c byte) (byte, bool) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', true
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, true
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, true
	default:
		return 0, false
	}
}

func (in *input) oscColourReply(n int, idx int, c Color, bel bool) {
	if c != -1 {
		c = ColorForceRGB(c)
	}
	if c == -1 {
		return
	}
	r, g, b := ColorSplitRGB(c)
	end := "\x1b\\"
	if bel {
		end = "\x07"
	}
	if n == 4 {
		in.replyf("\x1b]%d;%d;rgb:%02x%02x/%02x%02x/%02x%02x%s", n, idx, r, r, g, g, b, b, end)
	} else {
		in.replyf("\x1b]%d;rgb:%02x%02x/%02x%02x/%02x%02x%s", n, r, r, g, g, b, b, end)
	}
}

func (in *input) osc4(p string, bel bool) {
	pal := &in.t.Palette
	s := p
	for s != "" {
		semi := strings.IndexByte(s, ';')
		if semi <= 0 {
			return
		}
		idx, err := strconv.Atoi(s[:semi])
		if err != nil || idx < 0 || idx >= 256 {
			return
		}
		s = s[semi+1:]
		var spec string
		if next := strings.IndexByte(s, ';'); next != -1 {
			spec = s[:next]
			s = s[next+1:]
		} else {
			spec = s
			s = ""
		}
		if spec == "?" {
			c := pal.Get(Color(idx) | ColorFlag256)
			if c == -1 {
				c = pal.ClientPalette[idx]
			}
			if c != -1 {
				in.oscColourReply(4, idx, c, bel)
			}
			continue
		}
		c := ParseX11Color(spec)
		if c == -1 {
			continue
		}
		pal.Set(idx, c)
	}
}

func (in *input) osc8(p string) {
	s := in.t.Screen
	gc := &in.cell.cell
	var id string
	hasID := false
	start := 0
	semi := -1
	for i := 0; i < len(p); i++ {
		if p[i] != ':' && p[i] != ';' {
			continue
		}
		part := p[start:i]
		if len(part) >= 4 && strings.HasPrefix(part, "id=") {
			if hasID {
				return
			}
			id = part[3:]
			hasID = true
		}
		start = i + 1
		if p[i] == ';' {
			semi = i
			break
		}
	}
	if semi == -1 {
		return
	}
	uri := p[semi+1:]
	if uri == "" {
		gc.Link = 0
		return
	}
	if s.Hyperlinks == nil {
		s.Hyperlinks = NewHyperlinks()
	}
	gc.Link = s.Hyperlinks.Put(uri, id)
}

func (in *input) osc9(p string) {
	s := in.t.Screen
	// OSC 9;9 (Windows Terminal's working directory) is not tmux's: it only sets Cwd, which
	// gives #{pane_current_path} on Windows (COMPATIBILITY.md K1). tmux ignores it.
	if strings.HasPrefix(p, "9;") {
		cwd := p[2:]
		if len(cwd) >= 2 && cwd[0] == '"' && cwd[len(cwd)-1] == '"' {
			cwd = cwd[1 : len(cwd)-1]
		}
		if utf8IsValid(cwd) {
			s.Cwd = cwd
		}
		return
	}
	if !strings.HasPrefix(p, "4") {
		return
	}
	pb := p[1:]
	if pb == "" || pb == ";" {
		return
	}
	if pb[0] != ';' {
		return
	}
	pb = pb[1:]
	if len(pb) == 0 || pb[0] < '0' || pb[0] > '4' {
		return
	}
	state := int(pb[0] - '0')
	pb = pb[1:]
	if pb == "" || pb == ";" {
		s.ProgressState = state
		s.ProgressValue = -1
		return
	}
	if pb[0] != ';' {
		return
	}
	pb = pb[1:]
	if pb == "" {
		return
	}
	progress := 0
	for i := 0; i < len(pb); i++ {
		if pb[i] < '0' || pb[i] > '9' {
			return
		}
		progress = progress*10 + int(pb[i]-'0')
		if progress > 100 {
			return
		}
	}
	s.ProgressState = state
	s.ProgressValue = progress
}

func (in *input) osc10(p string, bel bool) {
	pal := &in.t.Palette
	if p == "?" {
		// input_osc_10: the pane's default colour (window-style's when the
		// program set none), else the client terminal's.
		c, _ := pal.Defaults(false)
		if c.IsDefault() {
			c = pal.ClientFg
		}
		in.oscColourReply(10, 0, c, bel)
		return
	}
	if in.t.FixedDefaultColours {
		return
	}
	if c := ParseX11Color(p); c != -1 {
		pal.Fg = c
	}
}

func (in *input) osc11(p string, bel bool) {
	pal := &in.t.Palette
	if p == "?" {
		_, c := pal.Defaults(false)
		if c.IsDefault() {
			c = pal.ClientBg
		}
		in.oscColourReply(11, 0, c, bel)
		return
	}
	if in.t.FixedDefaultColours {
		return
	}
	if c := ParseX11Color(p); c != -1 {
		pal.Bg = c
	}
}

func (in *input) osc12(p string, bel bool) {
	s := in.t.Screen
	if p == "?" {
		c := s.CColour
		if c == -1 {
			c = s.DefaultCColour
		}
		in.oscColourReply(12, 0, c, bel)
		return
	}
	if c := ParseX11Color(p); c != -1 {
		s.CColour = c
	}
}

func (in *input) osc52(p string) {
	// tmux takes a program's clipboard only with set-clipboard on; its default, external,
	// ignores it.
	if in.t.ClipboardMode != ClipboardOn {
		return
	}
	semi := strings.IndexByte(p, ';')
	if semi == -1 {
		return
	}
	flagsPart := p[:semi]
	end := p[semi+1:]
	const allowed = "cpqs01234567"
	var clipBuf [len(allowed)]byte
	clipLen := 0
	for i := 0; i < len(flagsPart); i++ {
		c := flagsPart[i]
		if strings.IndexByte(allowed, c) == -1 {
			continue
		}
		dup := false
		for j := 0; j < clipLen; j++ {
			if clipBuf[j] == c {
				dup = true
				break
			}
		}
		if !dup {
			clipBuf[clipLen] = c
			clipLen++
		}
	}
	if end == "?" {
		// OSC 52 read (?) is refused.
		return
	}
	if len(end) == 0 {
		return
	}
	// b64_pton in tmux skips whitespace.
	cleaned := strings.Map(func(r rune) rune {
		switch r {
		case ' ', '\t', '\r', '\n':
			return -1
		default:
			return r
		}
	}, end)
	if len(cleaned) == 0 {
		return
	}
	decoded, err := base64.StdEncoding.DecodeString(cleaned)
	if err != nil {
		return
	}
	clip := string(clipBuf[:clipLen])
	in.t.Clipboard = decoded
	in.t.ClipboardName = clip
	if in.t.SetClipboard != nil {
		in.t.SetClipboard(clip, decoded)
	}
}

func (in *input) osc104(p string) {
	pal := &in.t.Palette
	if p == "" {
		pal.Clear()
		return
	}
	s := p
	for s != "" {
		var part string
		if semi := strings.IndexByte(s, ';'); semi != -1 {
			part = s[:semi]
			s = s[semi+1:]
		} else {
			part = s
			s = ""
		}
		if part == "" {
			return
		}
		idx, err := strconv.Atoi(part)
		if err != nil || idx < 0 || idx >= 256 {
			return
		}
		pal.Set(idx, -1)
	}
}

func (in *input) osc110(p string) {
	if p == "" && !in.t.FixedDefaultColours {
		in.t.Palette.Fg = ColorDefault
	}
}

func (in *input) osc111(p string) {
	if p == "" && !in.t.FixedDefaultColours {
		in.t.Palette.Bg = ColorDefault
	}
}

func (in *input) osc112(p string) {
	if p == "" {
		in.t.Screen.CColour = -1
	}
}

func osc133ExitStatus(p string) int {
	if len(p) < 3 || p[1] != ';' || p[2] == '=' {
		return 0
	}
	rest := p[2:]
	if semi := strings.IndexByte(rest, ';'); semi != -1 {
		rest = rest[:semi]
	}
	if rest == "" || strings.IndexByte(rest, '=') >= 0 {
		return 0
	}
	v, err := strconv.Atoi(rest)
	if err != nil || v < 0 || v > 255 {
		return 255
	}
	return v
}

func (in *input) osc133(p string) {
	if len(p) == 0 {
		return
	}
	s := in.t.Screen
	gd := s.Grid
	line := s.Cy + gd.HSize
	var gl *Line
	if line >= 0 && line < gd.HSize+gd.Sy {
		gl = gd.Line(line)
	}
	switch p[0] {
	case 'A', 'N':
		if gl != nil {
			gl.Flags |= LineStartPrompt
		}
	case 'P':
		if gl != nil {
			if idx := strings.Index(p, ";k=s"); idx != -1 && (idx+4 == len(p) || p[idx+4] == ';') {
				gl.Flags |= LineSecondPrompt
			} else {
				gl.Flags |= LineStartPrompt
			}
		}
	case 'B', 'I':
		if gl != nil {
			gl.Flags |= LineStartCommand
		}
	case 'C':
		if gl != nil {
			gl.Flags |= LineStartOutput
		}
		s.LastCommandStatus = -1
	case 'D':
		status := osc133ExitStatus(p)
		s.LastCommandStatus = status
		if gl != nil {
			gl.Flags |= LineEndOutput
		}
	}
}

// OscDispatch handles an OSC sequence (tmux's input_exit_osc).
func (in *input) OscDispatch(data []byte, bel bool) {
	in.lastValid = false
	if len(data) == 0 || data[0] < '0' || data[0] > '9' {
		return
	}
	var option uint64
	i := 0
	for i < len(data) && data[i] >= '0' && data[i] <= '9' {
		option = option*10 + uint64(data[i]-'0')
		if option > math.MaxInt32 {
			return
		}
		i++
	}
	if i < len(data) && data[i] == ';' {
		i++
	} else if i != len(data) {
		return
	}
	p := string(data[i:])
	s := in.t.Screen
	switch option {
	case 0, 2: // tmux ignores OSC 1, the icon name (checked on tmux 3.8)
		if !s.DisallowSetTitle {
			s.SetTitle(p, true)
		}
	case 4:
		in.osc4(p, bel)
	case 7:
		s.SetPath(p)
	case 8:
		in.osc8(p)
	case 9:
		in.osc9(p)
	case 10:
		in.osc10(p, bel)
	case 11:
		in.osc11(p, bel)
	case 12:
		in.osc12(p, bel)
	case 52:
		in.osc52(p)
	case 104:
		in.osc104(p)
	case 110:
		in.osc110(p)
	case 111:
		in.osc111(p)
	case 112:
		in.osc112(p)
	case 133:
		in.osc133(p)
	}
}

// DcsDispatch handles a DCS sequence (tmux's input_dcs_dispatch).
func (in *input) DcsDispatch(p *Params, inter []byte, data []byte) {
	in.lastValid = false
	if len(data) >= 1 && data[0] == 'q' && len(inter) == 1 && inter[0] == '$' {
		in.handleDECRQSS(data)
	}
}

func (in *input) handleDECRQSS(buf []byte) {
	if len(buf) < 3 || buf[1] != ' ' || buf[2] != 'q' {
		in.replyf("\x1bP0$r\x1b\\")
		return
	}
	s := in.t.Screen
	var ps int
	if s.CStyle == CursorBlock || s.CStyle == CursorUnderline || s.CStyle == CursorBar {
		ps = cursorStylePs(s.CStyle, s.Mode&ModeCursorBlinking != 0)
	} else {
		ps = cursorStylePs(s.DefaultCStyle, s.DefaultMode&ModeCursorBlinking != 0)
	}
	in.replyf("\x1bP1$r q%d q\x1b\\", ps)
}

func cursorStylePs(style CursorStyle, blinking bool) int {
	switch style {
	case CursorBlock:
		if blinking {
			return 1
		}
		return 2
	case CursorUnderline:
		if blinking {
			return 3
		}
		return 4
	case CursorBar:
		if blinking {
			return 5
		}
		return 6
	default:
		return 0
	}
}

// ApcDispatch handles an APC sequence (tmux's input_exit_apc).
func (in *input) ApcDispatch(data []byte) {
	in.lastValid = false
	if !in.t.Screen.DisallowSetTitle {
		in.t.Screen.SetTitle(string(data), true)
	}
}

// RenameDispatch handles ESC k ... ST (tmux's input_exit_rename): the window's name is the
// server's, which applies it only with allow-rename on (off by default), so it is handed to
// the Rename callback; an empty name asks for automatic renaming back.
func (in *input) RenameDispatch(data []byte) {
	in.lastValid = false
	if in.t.Rename != nil && utf8IsValid(string(data)) {
		in.t.Rename(string(data))
	}
}
