// Ported from tmux 3.8's tty-features.c:
//
// Copyright (c) 2020 Nicholas Marriott <nicholas.marriott@gmail.com>
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

import "strings"

// Terminal flags, as tmux's TERM_*.
const (
	Term256Colours = 0x1
	TermNoAM       = 0x2
	TermDECSLRM    = 0x4
	TermDECFRA     = 0x8
	TermRGBColours = 0x10
	TermVT100Like  = 0x20
	TermSixel      = 0x40
	TermInvalidMS  = 0x80
	termNoReplace  = 0x100
)

// feature is a named terminal feature: the capabilities it adds and the flags it sets.
type feature struct {
	name  string
	caps  []string
	flags int
}

// features are tmux's, in its order: a feature's bit is its index.
var features = []feature{
	{"256", []string{
		"AX",
		`setab=\E[%?%p1%{8}%<%t4%p1%d%e%p1%{16}%<%t10%p1%{8}%-%d%e48;5;%p1%d%;m`,
		`setaf=\E[%?%p1%{8}%<%t3%p1%d%e%p1%{16}%<%t9%p1%{8}%-%d%e38;5;%p1%d%;m`,
	}, Term256Colours},
	{"appesc", []string{`Enesc=\E[?7727h`, `Dsesc=\E[?7727l`}, 0},
	{"bpaste", []string{`Enbp=\E[?2004h`, `Dsbp=\E[?2004l`}, 0},
	{"ccolour", []string{`Cs=\E]12;%p1%s\a`, `Cr=\E]112\a`}, 0},
	{"clipboard", []string{`Ms=\E]52;%p1%s;%p2%s\a`}, 0},
	{"hyperlinks", []string{`Hls=\E]8;%?%p1%l%tid=%p1%s%;;%p2%s\E\\`}, 0},
	{"cstyle", []string{`Ss=\E[%p1%d q`, `Se=\E[2 q`}, termNoReplace},
	{"extkeys", []string{`Eneks=\E[>4;2m`, `Dseks=\E[>4m`}, 0},
	{"focus", []string{`Enfcs=\E[?1004h`, `Dsfcs=\E[?1004l`}, 0},
	{"ignorefkeys", ignoreFKeys(), 0},
	{"margins", []string{`Enmg=\E[?69h`, `Dsmg=\E[?69l`, `Clmg=\E[s`, `Cmg=\E[%i%p1%d;%p2%ds`}, TermDECSLRM},
	{"mouse", []string{`kmous=\E[M`}, 0},
	{"osc7", []string{`Swd=\E]7;`, `fsl=\a`}, 0},
	{"overline", []string{`Smol=\E[53m`}, 0},
	{"progressbar", []string{`Spb=\E]9;4;%p1%d;%p2%d\E\\`}, 0},
	{"rectfill", []string{"Rect"}, TermDECFRA},
	{"RGB", []string{
		"AX",
		`setrgbf=\E[38;2;%p1%d;%p2%d;%p3%dm`,
		`setrgbb=\E[48;2;%p1%d;%p2%d;%p3%dm`,
		`setab=\E[%?%p1%{8}%<%t4%p1%d%e%p1%{16}%<%t10%p1%{8}%-%d%e48;5;%p1%d%;m`,
		`setaf=\E[%?%p1%{8}%<%t3%p1%d%e%p1%{16}%<%t9%p1%{8}%-%d%e38;5;%p1%d%;m`,
	}, Term256Colours | TermRGBColours},
	{"sixel", []string{"Sxl"}, TermSixel},
	{"strikethrough", []string{`smxx=\E[9m`}, 0},
	{"sync", []string{`Sync=\E[?2026%?%p1%{1}%-%tl%eh%;`}, 0},
	{"title", []string{`tsl=\E]0;`, `fsl=\a`}, 0},
	{"usstyle", []string{
		`Smulx=\E[4::%p1%dm`,
		`Setulc=\E[58::2::%p1%{65536}%/%d::%p1%{256}%/%{255}%&%d::%p1%{255}%&%d%;m`,
		`Setulc1=\E[58::5::%p1%dm`,
		`ol=\E[59m`,
	}, 0},
	{"utf8", nil, 0},
}

func ignoreFKeys() []string {
	out := make([]string, 0, 64)
	for i := range 64 {
		out = append(out, "kf"+itoa(i)+"@")
	}
	return out
}

func itoa(i int) string {
	if i < 10 {
		return string(rune('0' + i))
	}
	return itoa(i/10) + string(rune('0'+i%10))
}

// Features is a client's set of terminal features: those enabled and those removed (a
// removed one is not enabled again), as tmux's term_features and term_nofeatures.
type Features struct {
	Enabled, Disabled int
}

// Parse adds and removes features listed in s, separated by any byte of sep ("name" adds,
// "name@" removes), as tmux's tty_parse_features: an unknown name ends the list.
func (f *Features) Parse(s, sep string) {
	for _, next := range splitAny(s, sep) {
		remove := next != "" && strings.HasSuffix(next, "@")
		if remove {
			next = next[:len(next)-1]
		}
		i := -1
		for j, tf := range features {
			if strings.EqualFold(tf.name, next) {
				i = j
				break
			}
		}
		if i < 0 {
			return
		}
		if remove {
			f.Enabled &^= 1 << i
			f.Disabled |= 1 << i
			continue
		}
		if f.Disabled&(1<<i) != 0 {
			continue
		}
		f.Enabled |= 1 << i
	}
}

// splitAny splits s at every byte in sep, keeping empty fields, as strsep does.
func splitAny(s, sep string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if strings.IndexByte(sep, s[i]) >= 0 {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	return append(out, s[start:])
}

// FeatureNames lists features as tmux's tty_get_features ("256,bpaste,…").
func FeatureNames(set int) string {
	var names []string
	for i, tf := range features {
		if set&(1<<i) != 0 {
			names = append(names, tf.name)
		}
	}
	return strings.Join(names, ",")
}

// Default adds the features tmux knows a terminal by its name (from XTVERSION or the
// secondary device attributes) has, as tmux's tty_default_features.
func (f *Features) Default(name string) {
	const modernXterm = "256,RGB,bpaste,clipboard,mouse,strikethrough,title"
	table := map[string]string{
		"mintty":       modernXterm + ",appesc,ccolour,cstyle,extkeys,margins,overline,usstyle",
		"tmux":         modernXterm + ",ccolour,cstyle,extkeys,focus,overline,usstyle,hyperlinks,progressbar",
		"rxvt-unicode": "256,bpaste,ccolour,cstyle,mouse,title,ignorefkeys",
		"iTerm2":       modernXterm + ",cstyle,extkeys,margins,usstyle,sync,osc7,hyperlinks,progressbar",
		"foot":         modernXterm + ",ccolour,cstyle,extkeys,usstyle,sync,osc7,hyperlinks",
		"WezTerm":      modernXterm + ",ccolour,cstyle,extkeys,focus,hyperlinks,margins,usstyle",
		"ghostty":      modernXterm + ",ccolour,cstyle,extkeys,focus,margins,overline,hyperlinks,osc7,sync,usstyle,progressbar",
		"Rio":          modernXterm + ",ccolour,cstyle,focus,overline,hyperlinks,osc7,sync,usstyle,progressbar",
		"XTerm":        modernXterm + ",ccolour,cstyle,extkeys,focus",
	}
	if s, ok := table[name]; ok {
		f.Parse(s, ",")
	}
}
