package input

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	k "github.com/hamzayogurtcuoglu/i-mux-core/internal/keys"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/termout"
)

// newParser is a parser for xterm-256color (testdata/term), whose erase character is DEL.
func newParser(t *testing.T) *Parser {
	t.Helper()
	b, err := os.ReadFile("../../testdata/term/xterm-256color.json")
	if err != nil {
		t.Fatal(err)
	}
	var rec struct{ Caps []string }
	if err := json.Unmarshal(b, &rec); err != nil {
		t.Fatal(err)
	}
	term, err := termout.NewTerm("xterm-256color", rec.Caps, &termout.Features{}, termout.DefaultOptions)
	if err != nil {
		t.Fatal(err)
	}
	return New(term, 0x7f)
}

// names feeds in and returns the names of the keys it gives, a partial key taken as it is
// once escape-time passes.
func names(p *Parser, in string) string {
	evs := p.Feed([]byte(in))
	if _, ok := p.Delay(); ok {
		evs = append(evs, p.Expire()...)
	}
	var out []string
	for _, ev := range evs {
		if ev.Kind == EventKey {
			out = append(out, ev.Key.String())
		}
	}
	return strings.Join(out, " ")
}

// TestKeyNames reads what a terminal sends as the keys tmux names in list-keys.
func TestKeyNames(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		// Text and C0 controls.
		{"a", "a"},
		{"A", "A"},
		{"abc", "a b c"},
		{"é", "é"},
		{"日本", "日 本"},
		{"🙂", "🙂"},
		{"\x01", "C-a"},
		{"\x1a", "C-z"},
		{"\x00", "C-Space"},
		{"\t", "Tab"},
		{"\r", "Enter"},
		{"\n", "C-j"},
		{"\x08", "C-h"},
		{"\x7f", "BSpace"},
		{"\x1c", "C-\\"},
		{"\x10", "C-p"},
		{"\x16", "C-v"},
		{"\x1f", "C-_"},
		// Escape and Meta.
		{"\x1b", "Escape"},
		{"\x1ba", "M-a"},
		{"\x1bv", "M-v"},
		{"\x1b\x01", "C-M-a"},
		{"\x1b\x7f", "M-BSpace"},
		{"\x1b\x1b", "M-Escape"},
		{"\x1b[", "M-["},
		{"\x1bé", "M-é"},
		// Cursor and function keys, normal and application mode, with xterm modifiers.
		{"\x1b[A", "Up"},
		{"\x1bOA", "Up"},
		{"\x1b[B", "Down"},
		{"\x1b[C", "Right"},
		{"\x1b[D", "Left"},
		{"\x1b[1;2A", "S-Up"},
		{"\x1b[1;3A", "M-Up"},
		{"\x1b[1;5A", "C-Up"},
		{"\x1b[1;6D", "C-S-Left"},
		{"\x1b[1;2C", "S-Right"},
		{"\x1b[1;2B", "S-Down"},
		{"\x1b[1;2D", "S-Left"},
		{"\x1b\x1b[A", "M-Up"},
		{"\x1b\x1b[1;3A", "Escape M-Up"},
		{"\x1b[H", "Home"},
		{"\x1b[F", "End"},
		{"\x1bOH", "Home"},
		{"\x1b[1;5H", "C-Home"},
		{"\x1b[2~", "IC"},
		{"\x1b[3~", "DC"},
		{"\x1b[5~", "PPage"},
		{"\x1b[6~", "NPage"},
		{"\x1b[3;5~", "C-DC"},
		{"\x1b[5;2~", "S-PPage"},
		{"\x1b[Z", "BTab"},
		{"\x1bOP", "F1"},
		{"\x1bOQ", "F2"},
		{"\x1b[15~", "F5"},
		{"\x1b[24~", "F12"},
		{"\x1b[1;2P", "S-F1"},
		{"\x1b[15;5~", "C-F5"},
		{"\x1b[24;3~", "M-F12"},
		{"\x1bOo", "KP/"},
		{"\x1bOM", "KPEnter"},
		{"\x1bOp", "KP0"},
		// Extended keys: CSI u and modifyOtherKeys.
		{"\x1b[13;2u", "S-Enter"},
		{"\x1b[13;5u", "C-Enter"},
		{"\x1b[27;5;118~", "C-v"},
		{"\x1b[27;2;13~", "S-Enter"},
		{"\x1b[118;5u", "C-v"},
		{"\x1b[97;2u", "a"},
		{"\x1b[97;3u", "M-a"},
		{"\x1b[9;2u", "BTab"},
		{"\x1b[127;5u", "C-BSpace"},
		{"\x1b[233;5u", "C-é"},
		{"\x1b[1;5Z", "C-S-Tab"},
		// Focus, paste, theme reports.
		{"\x1b[I", "FocusIn"},
		{"\x1b[O", "FocusOut"},
		{"\x1b[200~hi\x1b[201~", "PasteStart h i PasteEnd"},
		{"\x1b[?997;1n", "ReportDarkTheme"},
		{"\x1b[?997;2n", "ReportLightTheme"},
		// Mouse reports are one key each.
		{"\x1b[<0;10;5M", "Mouse"},
		{"\x1b[<0;10;5m", "Mouse"},
		{"\x1b[M !!", "Mouse"},
		// A wheel release is a terminal's mistake: dropped.
		{"\x1b[<64;1;1m", ""},
	} {
		p := newParser(t)
		p.StopWaiting()
		if got := names(p, tc.in); got != tc.want {
			t.Errorf("%q: %q, want %q", tc.in, got, tc.want)
		}
	}
}

// A partial key waits for escape-time (or longer while replies are expected, or in a paste),
// and more input completes it; split UTF-8 and split sequences come whole.
func TestPartial(t *testing.T) {
	p := newParser(t)
	p.StopWaiting()
	if evs := p.Feed([]byte("\x1b")); len(evs) != 0 {
		t.Fatalf("escape alone: %v", evs)
	}
	if d, ok := p.Delay(); !ok || d != 10*time.Millisecond {
		t.Errorf("delay %v %v", d, ok)
	}
	if evs := p.Feed([]byte("[1;5")); len(evs) != 0 {
		t.Fatalf("partial: %v", evs)
	}
	if evs := p.Feed([]byte("A")); len(evs) != 1 || evs[0].Key.String() != "C-Up" || string(evs[0].Raw) != "\x1b[1;5A" {
		t.Errorf("C-Up: %+v", evs)
	}
	if _, ok := p.Delay(); ok {
		t.Errorf("still waiting")
	}
	if evs := append(p.Feed([]byte{0xe6, 0x97}), p.Feed([]byte{0xa5})...); len(evs) != 1 || evs[0].Key.String() != "日" {
		t.Errorf("split UTF-8: %+v", evs)
	}
	// Waiting for the terminal's replies, or the end of a paste, a partial key waits longer.
	q := newParser(t)
	q.Requested()
	q.Feed([]byte("\x1b"))
	if d, _ := q.Delay(); d != 500*time.Millisecond {
		t.Errorf("waiting for replies: delay %v", d)
	}
	r := newParser(t)
	r.StopWaiting()
	r.Feed([]byte("\x1b[200~x\x1b[20"))
	if d, _ := r.Delay(); d != 500*time.Millisecond || !r.Bracketed() {
		t.Errorf("partial paste end: delay %v", d)
	}
}

// Mouse reports give the position from 0, the button, and the last report.
func TestMouse(t *testing.T) {
	p := newParser(t)
	p.StopWaiting()
	evs := p.Feed([]byte("\x1b[<0;10;5M\x1b[<0;12;6m\x1b[M#!\""))
	if len(evs) != 3 {
		t.Fatalf("events %+v", evs)
	}
	if m := evs[0].Mouse; m.X != 9 || m.Y != 4 || m.B != 0 || m.SGRType != 'M' {
		t.Errorf("press %+v", m)
	}
	if m := evs[1].Mouse; m.X != 11 || m.Y != 5 || m.B != 3 || m.SGRType != 'm' || m.LX != 9 || m.LY != 4 {
		t.Errorf("release %+v", m)
	}
	if m := evs[2].Mouse; m.X != 0 || m.Y != 1 || m.B != 3 || m.SGRType != ' ' {
		t.Errorf("X10 %+v", m)
	}
}

// The terminal's replies are taken out of the input: each once, and keys around them stay.
func TestReplies(t *testing.T) {
	p := newParser(t)
	p.Requested()
	in := "a\x1b[?62;4;22;52c\x1b[>84;0;0c\x1bP>|XTerm(390)\x1b\\\x1b[?2026;2$y" +
		"\x1b]10;rgb:ffff/ffff/ffff\x1b\\\x1b]11;rgb:0000/0000/0000\x07\x1b]4;1;rgb:ff/00/00\x07" +
		"\x1b]52;c;aGk=\x07b"
	evs := p.Feed([]byte(in))
	var got []string
	for _, ev := range evs {
		switch ev.Kind {
		case EventKey:
			got = append(got, "key "+ev.Key.String())
		case EventDA:
			got = append(got, "DA "+ev.Features)
		case EventDA2:
			got = append(got, "DA2 "+ev.Name)
		case EventXDA:
			got = append(got, "XDA "+ev.Name+" "+ev.TermType)
		case EventSync:
			got = append(got, "sync "+ev.Features)
		case EventColour:
			got = append(got, "colour")
			if ev.Fg != 0x2ffffff && ev.Bg != 0x2000000 {
				t.Errorf("colour %+v", ev)
			}
		case EventPalette:
			got = append(got, "palette")
			if ev.Index != 1 || ev.Colour != 0x2ff0000 {
				t.Errorf("palette %+v", ev)
			}
		case EventClipboard:
			got = append(got, "clipboard "+string(ev.Clip)+" "+string(ev.Data))
		}
	}
	want := "key a|DA sixel,clipboard|DA2 tmux|XDA XTerm XTerm(390)|sync sync|colour|colour|palette|clipboard c hi|key b"
	if strings.Join(got, "|") != want {
		t.Errorf("events:\n got %s\nwant %s", strings.Join(got, "|"), want)
	}
	if p.waiting || p.waitFg || p.waitBg {
		t.Errorf("still waiting for replies")
	}
	// A second DA reply is not a reply any more: it is keys.
	if got := names(p, "\x1b[?1;2c"); got != "M-[ ? 1 ; 2 c" {
		t.Errorf("second DA: %q", got)
	}
}

// The keys that terminfo names come from the terminal's entry: xterm's kf1 (ESC O P) is F1,
// but without it those bytes are M-O and P, as in tmux; with no erase character DEL is C-?.
func TestTerminfoKeys(t *testing.T) {
	p := New(nil, -1)
	p.StopWaiting()
	if got := names(p, "\x1bOP"); got != "M-O P" {
		t.Errorf("without terminfo: %q", got)
	}
	if got := names(p, "\x7f"); got != "C-?" {
		t.Errorf("no erase character: %q", got)
	}
	if k.Parse("F1") == k.Unknown {
		t.Error("F1")
	}
}

// The answers to CSI 18 t and 14 t are read while the server waits for them (tty_keys_winsz);
// the pixel answer ends the wait, and other times the bytes are keys.
func TestWinsizeReplies(t *testing.T) {
	p := newParser(t)
	p.WinsizeQuery = true
	evs := p.Feed([]byte("\x1b[8;40;120t\x1b[4;800;1200tx"))
	if len(evs) != 3 || evs[0].Kind != EventWinsize || evs[0].Rows != 40 || evs[0].Cols != 120 ||
		evs[1].Kind != EventWinsize || evs[1].YPixel != 800 || evs[1].XPixel != 1200 ||
		evs[2].Kind != EventKey || evs[2].Key != 'x' {
		t.Fatalf("events %+v", evs)
	}
	if p.WinsizeQuery {
		t.Error("the pixel answer did not end the query")
	}
	// A partial answer waits for the rest.
	p.WinsizeQuery = true
	if evs := p.Feed([]byte("\x1b[4;80")); len(evs) != 0 {
		t.Fatalf("partial: %+v", evs)
	}
	if evs := p.Feed([]byte("0;1200t")); len(evs) != 1 || evs[0].Kind != EventWinsize || evs[0].XPixel != 1200 {
		t.Fatalf("rest: %+v", evs)
	}
	// Not asked: not an answer.
	for _, ev := range p.Feed([]byte("\x1b[8;40;120t")) {
		if ev.Kind == EventWinsize {
			t.Fatalf("read unasked: %+v", ev)
		}
	}
}
