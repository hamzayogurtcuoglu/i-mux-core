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

import k "github.com/hamzayogurtcuoglu/i-mux-core/internal/keys"

// The keys a client terminal sends, as tmux knows them: fixed sequences, xterm's modified
// keys, and those named by the terminal's terminfo entry.

type rawKey struct {
	s   string
	key k.Key
}

// rawKeys is tmux's tty_default_raw_keys.
var rawKeys = []rawKey{
	{"\033O[", '\033'},

	{"\033Oo", k.KPSlash | k.Keypad},
	{"\033Oj", k.KPStar | k.Keypad},
	{"\033Om", k.KPMinus | k.Keypad},
	{"\033Ow", k.KPSeven | k.Keypad},
	{"\033Ox", k.KPEight | k.Keypad},
	{"\033Oy", k.KPNine | k.Keypad},
	{"\033Ok", k.KPPlus | k.Keypad},
	{"\033Ot", k.KPFour | k.Keypad},
	{"\033Ou", k.KPFive | k.Keypad},
	{"\033Ov", k.KPSix | k.Keypad},
	{"\033Oq", k.KPOne | k.Keypad},
	{"\033Or", k.KPTwo | k.Keypad},
	{"\033Os", k.KPThree | k.Keypad},
	{"\033OM", k.KPEnter | k.Keypad},
	{"\033Op", k.KPZero | k.Keypad},
	{"\033On", k.KPPeriod | k.Keypad},

	{"\033OA", k.Up | k.Cursor},
	{"\033OB", k.Down | k.Cursor},
	{"\033OC", k.Right | k.Cursor},
	{"\033OD", k.Left | k.Cursor},
	{"\033[A", k.Up | k.Cursor},
	{"\033[B", k.Down | k.Cursor},
	{"\033[C", k.Right | k.Cursor},
	{"\033[D", k.Left | k.Cursor},

	// Escape and an arrow stays Escape and an arrow: no IMPLIED_META.
	{"\033\033OA", k.Up | k.Cursor | k.Meta},
	{"\033\033OB", k.Down | k.Cursor | k.Meta},
	{"\033\033OC", k.Right | k.Cursor | k.Meta},
	{"\033\033OD", k.Left | k.Cursor | k.Meta},
	{"\033\033[A", k.Up | k.Cursor | k.Meta},
	{"\033\033[B", k.Down | k.Cursor | k.Meta},
	{"\033\033[C", k.Right | k.Cursor | k.Meta},
	{"\033\033[D", k.Left | k.Cursor | k.Meta},

	{"\033OH", k.Home},
	{"\033OF", k.End},
	{"\033\033OH", k.Home | k.Meta | k.ImpliedMeta},
	{"\033\033OF", k.End | k.Meta | k.ImpliedMeta},
	{"\033[H", k.Home},
	{"\033[F", k.End},
	{"\033\033[H", k.Home | k.Meta | k.ImpliedMeta},
	{"\033\033[F", k.End | k.Meta | k.ImpliedMeta},

	// rxvt arrow keys.
	{"\033Oa", k.Up | k.Ctrl},
	{"\033Ob", k.Down | k.Ctrl},
	{"\033Oc", k.Right | k.Ctrl},
	{"\033Od", k.Left | k.Ctrl},
	{"\033[a", k.Up | k.Shift},
	{"\033[b", k.Down | k.Shift},
	{"\033[c", k.Right | k.Shift},
	{"\033[d", k.Left | k.Shift},

	// rxvt function keys.
	{"\033[11~", k.F1},
	{"\033[12~", k.F2},
	{"\033[13~", k.F3},
	{"\033[14~", k.F4},
	{"\033[15~", k.F5},
	{"\033[17~", k.F6},
	{"\033[18~", k.F7},
	{"\033[19~", k.F8},
	{"\033[20~", k.F9},
	{"\033[21~", k.F10},

	{"\033[23~", k.F1 | k.Shift},
	{"\033[24~", k.F2 | k.Shift},
	{"\033[25~", k.F3 | k.Shift},
	{"\033[26~", k.F4 | k.Shift},
	{"\033[28~", k.F5 | k.Shift},
	{"\033[29~", k.F6 | k.Shift},
	{"\033[31~", k.F7 | k.Shift},
	{"\033[32~", k.F8 | k.Shift},
	{"\033[33~", k.F9 | k.Shift},
	{"\033[34~", k.F10 | k.Shift},
	{"\033[23$", k.F11 | k.Shift},
	{"\033[24$", k.F12 | k.Shift},

	{"\033[11^", k.F1 | k.Ctrl},
	{"\033[12^", k.F2 | k.Ctrl},
	{"\033[13^", k.F3 | k.Ctrl},
	{"\033[14^", k.F4 | k.Ctrl},
	{"\033[15^", k.F5 | k.Ctrl},
	{"\033[17^", k.F6 | k.Ctrl},
	{"\033[18^", k.F7 | k.Ctrl},
	{"\033[19^", k.F8 | k.Ctrl},
	{"\033[20^", k.F9 | k.Ctrl},
	{"\033[21^", k.F10 | k.Ctrl},
	{"\033[23^", k.F11 | k.Ctrl},
	{"\033[24^", k.F12 | k.Ctrl},

	{"\033[11@", k.F1 | k.Ctrl | k.Shift},
	{"\033[12@", k.F2 | k.Ctrl | k.Shift},
	{"\033[13@", k.F3 | k.Ctrl | k.Shift},
	{"\033[14@", k.F4 | k.Ctrl | k.Shift},
	{"\033[15@", k.F5 | k.Ctrl | k.Shift},
	{"\033[17@", k.F6 | k.Ctrl | k.Shift},
	{"\033[18@", k.F7 | k.Ctrl | k.Shift},
	{"\033[19@", k.F8 | k.Ctrl | k.Shift},
	{"\033[20@", k.F9 | k.Ctrl | k.Shift},
	{"\033[21@", k.F10 | k.Ctrl | k.Shift},
	{"\033[23@", k.F11 | k.Ctrl | k.Shift},
	{"\033[24@", k.F12 | k.Ctrl | k.Shift},

	{"\033[I", k.FocusIn},
	{"\033[O", k.FocusOut},

	{"\033[200~", k.PasteStart | k.ImpliedMeta},
	{"\033[201~", k.PasteEnd | k.ImpliedMeta},

	{"\033[1;5Z", '\011' | k.Ctrl | k.Shift},

	{"\033[?997;1n", k.ReportDarkTheme},
	{"\033[?997;2n", k.ReportLightTheme},
}

// xtermKeys is tmux's tty_default_xterm_keys: _ stands for the modifier parameter.
var xtermKeys = []rawKey{
	{"\033[1;_P", k.F1},
	{"\033O1;_P", k.F1},
	{"\033O_P", k.F1},
	{"\033[1;_Q", k.F2},
	{"\033O1;_Q", k.F2},
	{"\033O_Q", k.F2},
	{"\033[1;_R", k.F3},
	{"\033O1;_R", k.F3},
	{"\033O_R", k.F3},
	{"\033[1;_S", k.F4},
	{"\033O1;_S", k.F4},
	{"\033O_S", k.F4},
	{"\033[15;_~", k.F5},
	{"\033[17;_~", k.F6},
	{"\033[18;_~", k.F7},
	{"\033[19;_~", k.F8},
	{"\033[20;_~", k.F9},
	{"\033[21;_~", k.F10},
	{"\033[23;_~", k.F11},
	{"\033[24;_~", k.F12},
	{"\033[1;_A", k.Up},
	{"\033[1;_B", k.Down},
	{"\033[1;_C", k.Right},
	{"\033[1;_D", k.Left},
	{"\033[1;_H", k.Home},
	{"\033[1;_F", k.End},
	{"\033[5;_~", k.PPage},
	{"\033[6;_~", k.NPage},
	{"\033[2;_~", k.IC},
	{"\033[3;_~", k.DC},
}

// xtermModifiers is tmux's tty_default_xterm_modifiers, by xterm's modifier parameter.
var xtermModifiers = []k.Key{
	0,
	0,
	k.Shift,
	k.Meta | k.ImpliedMeta,
	k.Shift | k.Meta | k.ImpliedMeta,
	k.Ctrl,
	k.Shift | k.Ctrl,
	k.Meta | k.ImpliedMeta | k.Ctrl,
	k.Shift | k.Meta | k.ImpliedMeta | k.Ctrl,
	k.Meta | k.ImpliedMeta,
}

type codeKey struct {
	cap string
	key k.Key
}

// codeKeys is tmux's tty_default_code_keys: keys named by terminfo capabilities.
var codeKeys = func() []codeKey {
	m := k.Meta | k.ImpliedMeta
	var out []codeKey
	fn := []k.Key{k.F1, k.F2, k.F3, k.F4, k.F5, k.F6, k.F7, k.F8, k.F9, k.F10, k.F11, k.F12}
	mods := []k.Key{0, k.Shift, k.Ctrl, k.Shift | k.Ctrl, m, m | k.Shift}
	n := 1
	for _, mod := range mods {
		for _, f := range fn {
			if n > 63 {
				break
			}
			out = append(out, codeKey{"kf" + itoa(n), f | mod})
			n++
		}
	}
	out = append(out,
		codeKey{"kich1", k.IC}, codeKey{"kdch1", k.DC}, codeKey{"khome", k.Home}, codeKey{"kend", k.End},
		codeKey{"knp", k.NPage}, codeKey{"kpp", k.PPage}, codeKey{"kcbt", k.BTab},
		codeKey{"kcuu1", k.Up | k.Cursor}, codeKey{"kcud1", k.Down | k.Cursor},
		codeKey{"kcub1", k.Left | k.Cursor}, codeKey{"kcuf1", k.Right | k.Cursor},
	)
	// The capabilities for modified keys: base name, then 2 to 7.
	modsN := map[int]k.Key{2: k.Shift, 3: m, 4: k.Shift | m, 5: k.Ctrl, 6: k.Shift | k.Ctrl, 7: m | k.Ctrl}
	for _, b := range []struct {
		name string
		key  k.Key
	}{
		{"kDC", k.DC},
		{"kDN", k.Down},
		{"kEND", k.End},
		{"kHOM", k.Home},
		{"kIC", k.IC},
		{"kLFT", k.Left},
		{"kNXT", k.NPage},
		{"kPRV", k.PPage},
		{"kRIT", k.Right},
		{"kUP", k.Up},
	} {
		for i := 2; i <= 7; i++ {
			name := b.name
			if i > 2 {
				name += itoa(i)
			}
			out = append(out, codeKey{name, b.key | modsN[i]})
			switch {
			case b.name == "kDN" && i == 2:
				out = append(out, codeKey{"kind", k.Down | k.Shift})
			case b.name == "kUP" && i == 2:
				out = append(out, codeKey{"kri", k.Up | k.Shift})
			}
		}
	}
	return out
}()

func itoa(n int) string {
	if n < 10 {
		return string(rune('0' + n))
	}
	return itoa(n/10) + string(rune('0'+n%10))
}
