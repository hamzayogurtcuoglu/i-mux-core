// Ported from tmux 3.8's tty-term.c:
//
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

package termout

import (
	"errors"
	"fmt"
	"path"
	"strconv"
	"strings"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/vt"
)

// A client terminal's description: the capabilities its terminfo gave, after
// terminal-features, COLORTERM and terminal-overrides, as tmux's tty_term_create builds it.

type code struct {
	typ codeType // 0: missing
	s   string
	n   int
	f   bool
}

// Term is a client terminal's description.
type Term struct {
	Name     string
	Flags    int
	Features *Features // the client's features (shared with the client)
	applied  int       // features applied so far
	opts     Options
	codes    []code
	acs      [256]byte
}

// Options are the server options a description depends on.
type Options struct {
	TerminalFeatures  []string // terminal-features entries ("pattern:feature:…")
	TerminalOverrides []string // terminal-overrides entries ("pattern:cap=value:…")
	ColorTerm         string   // the client's $COLORTERM
}

// DefaultOptions are tmux 3.8's defaults.
var DefaultOptions = Options{
	TerminalFeatures:  []string{"xterm*:clipboard:ccolour:cstyle:focus:title", "screen*:title", "rxvt*:ignorefkeys"},
	TerminalOverrides: []string{"linux*:AX@"},
}

func codeIndex(name string) int {
	for i, c := range codes {
		if c.name == name {
			return i
		}
	}
	return -1
}

// strip removes $<…> padding, as tmux's tty_term_strip.
func strip(s string) string {
	if !strings.Contains(s, "$") {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '$' && i+1 < len(s) && s[i+1] == '<' {
			for i < len(s) && s[i] != '>' {
				i++
			}
			if i < len(s) && s[i] == '>' {
				i++
			}
			if i >= len(s) {
				break
			}
		}
		b.WriteByte(s[i])
		if b.Len() == 8191 {
			break
		}
	}
	return b.String()
}

// overrideNext reads the next ':'-separated field of s from *offset ("::" is a ':'), as tmux's
// tty_term_override_next; ok is false at the end.
func overrideNext(s string, offset *int) (string, bool) {
	at := *offset
	if at >= len(s) {
		return "", false
	}
	var b strings.Builder
	for at < len(s) {
		if s[at] == ':' {
			if at+1 < len(s) && s[at+1] == ':' {
				b.WriteByte(':')
				at += 2
				continue
			}
			break
		}
		b.WriteByte(s[at])
		at++
	}
	if at < len(s) {
		*offset = at + 1
	} else {
		*offset = at
	}
	return b.String(), true
}

// fnmatch matches a terminal name against a pattern, as fnmatch(3) with no flags.
func fnmatch(pattern, name string) bool {
	ok, err := path.Match(strings.ReplaceAll(pattern, "[!", "[^"), name)
	return err == nil && ok
}

// NewTerm builds the description of terminal name from its terminfo capabilities
// ("name=value", as Terminfo.Capabilities gives them), with tmux's errors.
func NewTerm(name string, caps []string, f *Features, o Options) (*Term, error) {
	t := &Term{Name: name, Features: f, codes: make([]code, len(codes)), opts: o}
	for _, c := range caps {
		k, v, _ := strings.Cut(c, "=")
		if k == "" {
			continue
		}
		i := codeIndex(k)
		if i < 0 {
			continue
		}
		switch codes[i].typ {
		case codeString:
			t.codes[i] = code{typ: codeString, s: strip(v)}
		case codeNumber:
			if n, err := strconv.Atoi(v); err == nil && n >= 0 {
				t.codes[i] = code{typ: codeNumber, n: n}
			} else {
				t.codes[i] = code{}
			}
		case codeFlag:
			t.codes[i] = code{typ: codeFlag, f: strings.HasPrefix(v, "1")}
		}
	}

	for _, s := range o.TerminalFeatures {
		off := 0
		if first, ok := overrideNext(s, &off); ok && fnmatch(first, name) {
			f.Parse(s[off:], ":")
		}
	}
	switch ct := strings.ToLower(o.ColorTerm); {
	case ct == "truecolor" || ct == "24bit":
		f.Parse("RGB", ",")
	case strings.Contains(o.ColorTerm, "256"):
		f.Parse("256", ",")
	}
	t.applyOverrides(o)

	if !t.Has("clear") {
		return nil, errors.New("terminal does not support clear")
	}
	if !t.Has("cup") {
		return nil, errors.New("terminal does not support cup")
	}
	if t.Flag("XT") || strings.HasPrefix(t.String("clear"), "\033[") {
		t.Flags |= TermVT100Like
		f.Parse("bpaste,focus,title", ",")
	}
	if (t.Flag("Tc") || t.Has("RGB")) && (!t.Has("setrgbf") || !t.Has("setrgbb")) {
		f.Parse("RGB", ",")
	}
	if t.ApplyFeatures() {
		t.applyOverrides(o)
	}
	return t, nil
}

// Update applies features the client has gained since, and then the overrides again (tmux's
// tty_apply_features and tty_term_apply_overrides); it reports whether anything changed.
func (t *Term) Update() bool {
	if !t.ApplyFeatures() {
		return false
	}
	t.applyOverrides(t.opts)
	return true
}

// Colours is how many colours the terminal has, as #{client_colours} says.
func (t *Term) Colours() int {
	switch {
	case t.Flags&TermRGBColours != 0:
		return 16777216
	case t.Flags&Term256Colours != 0:
		return 256
	}
	switch n := t.Number("colors"); {
	case n < 8:
		return 2
	case n < 16:
		return 8
	}
	return 16
}

// ApplyFeatures applies the client's features not applied yet; it reports whether any were
// (then the overrides are applied again).
func (t *Term) ApplyFeatures() bool {
	feat := t.Features.Enabled &^ t.Features.Disabled
	if feat == 0 {
		return false
	}
	for i, tf := range features {
		if t.applied&(1<<i) != 0 || feat&(1<<i) == 0 {
			continue
		}
		for _, c := range tf.caps {
			t.apply(c, tf.flags)
		}
		t.Flags |= tf.flags &^ termNoReplace
	}
	if t.applied|feat == t.applied {
		return false
	}
	t.applied |= feat
	return true
}

// apply sets capabilities from "name=value:name@:name…", as tmux's tty_term_apply.
func (t *Term) apply(capabilities string, flags int) {
	off := 0
	for {
		s, ok := overrideNext(capabilities, &off)
		if !ok {
			return
		}
		if s == "" {
			continue
		}
		var value string
		remove := false
		if k, v, found := strings.Cut(s, "="); found {
			s = k
			if u, ok := strunvis(v); ok {
				value = u
			} else {
				value = v
			}
		} else if strings.HasSuffix(s, "@") {
			s = s[:len(s)-1]
			remove = true
		}
		for i, c := range codes {
			if c.name != s {
				continue
			}
			if flags&termNoReplace != 0 && t.codes[i].typ != 0 {
				continue
			}
			if remove {
				t.codes[i] = code{}
				continue
			}
			switch c.typ {
			case codeString:
				t.codes[i] = code{typ: codeString, s: value}
			case codeNumber:
				if n, err := strconv.Atoi(value); err == nil && n >= 0 {
					t.codes[i] = code{typ: codeNumber, n: n}
				}
			case codeFlag:
				t.codes[i] = code{typ: codeFlag, f: true}
			}
		}
	}
}

func (t *Term) applyOverrides(o Options) {
	for _, s := range o.TerminalOverrides {
		off := 0
		if first, ok := overrideNext(s, &off); ok && fnmatch(first, t.Name) {
			t.apply(s[off:], 0)
		}
	}
	setFlag := func(flag int, on bool) {
		if on {
			t.Flags |= flag
		} else {
			t.Flags &^= flag
		}
	}
	setFlag(TermRGBColours, t.Has("setrgbf") && t.Has("setrgbb"))
	setFlag(TermDECSLRM, t.Has("Cmg") && t.Has("Clmg"))
	setFlag(TermDECFRA, t.Has("Rect"))
	setFlag(TermNoAM, !t.Flag("am"))

	t.acs = [256]byte{}
	acs := "a#j+k+l+m+n+o-p-q-r-s-t+u+v+w+x|y<z>~."
	if t.Has("acsc") {
		acs = t.String("acsc")
	}
	for ; len(acs) >= 2; acs = acs[2:] {
		t.acs[acs[0]] = acs[1]
	}

	// tmux's tty_term_validate: an Ms that expands to nothing is dropped.
	if i := codeIndex("Ms"); t.codes[i].typ == codeString {
		if Tparm(t.codes[i].s, "c", "?") != "" {
			t.Flags &^= TermInvalidMS
		} else {
			t.Flags |= TermInvalidMS
			t.codes[i] = code{}
		}
	}
}

// Has reports whether the terminal has capability name.
func (t *Term) Has(name string) bool {
	i := codeIndex(name)
	return i >= 0 && t.codes[i].typ != 0
}

// String returns a string capability, or "".
func (t *Term) String(name string) string {
	i := codeIndex(name)
	if i < 0 || t.codes[i].typ != codeString {
		return ""
	}
	return t.codes[i].s
}

// Number returns a number capability, or 0.
func (t *Term) Number(name string) int {
	i := codeIndex(name)
	if i < 0 || t.codes[i].typ != codeNumber {
		return 0
	}
	return t.codes[i].n
}

// Flag returns a flag capability.
func (t *Term) Flag(name string) bool {
	i := codeIndex(name)
	if i < 0 || t.codes[i].typ != codeFlag {
		return false
	}
	return t.codes[i].f
}

// Describe lists the capabilities as tmux's show-messages -T does (tty_term_describe).
func (t *Term) Describe() []string {
	out := make([]string, len(codes))
	for i, c := range t.codes {
		name := codes[i].name
		switch c.typ {
		case 0:
			out[i] = fmt.Sprintf("%4d: %s: [missing]", i, name)
		case codeString:
			out[i] = fmt.Sprintf("%4d: %s: (string) %s", i, name, vt.StrnVis(c.s, 128))
		case codeNumber:
			out[i] = fmt.Sprintf("%4d: %s: (number) %d", i, name, c.n)
		case codeFlag:
			out[i] = fmt.Sprintf("%4d: %s: (flag) %v", i, name, c.f)
		}
	}
	return out
}
