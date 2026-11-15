package vt

import "testing"

func TestRuneWidth(t *testing.T) {
	tests := []struct {
		r    rune
		want int
		why  string
	}{
		{'a', 1, "ASCII"},
		{0x07, 0, "C0 control"},
		{0x85, 0, "C1 control"},
		{0xe9, 1, "é"},
		{0x0301, 0, "combining acute"},
		{0x200b, 0, "zero width space"},
		{0x200d, 0, "zero width joiner"},
		{0xfe0f, 0, "variation selector 16"},
		{0x65e5, 2, "CJK 日"},
		{0xac00, 2, "Hangul syllable"},
		{0x1100, 2, "Hangul choseong"},
		{0x1161, 1, "Hangul jungseong: utf8proc 2.12 says 1 (wcwidth often says 0)"},
		{0x1f642, 2, "emoji 🙂"},
		{0x1f1e6, 1, "regional indicator: tmux's own table says 1"},
		{0x1f3fd, 2, "skin tone modifier: tmux's own table says 2"},
		{0x261d, 2, "☝: tmux's own table says 2"},
		{0x263a, 1, "☺ (text presentation)"},
		{0xe000, 1, "private use: tmux says 1"},
		{0xf0000, 1, "supplementary private use"},
		{0x0378, 1, "unassigned"},
		{0x2500, 1, "box drawing"},
		{0xff21, 2, "fullwidth A"},
	}
	for _, tt := range tests {
		if got := RuneWidth(tt.r); got != tt.want {
			t.Errorf("RuneWidth(%U) = %d, want %d (%s)", tt.r, got, tt.want, tt.why)
		}
	}
}

func TestWidthRangesSorted(t *testing.T) {
	for i, r := range widthRanges {
		if r.lo > r.hi {
			t.Fatalf("range %d: %U > %U", i, r.lo, r.hi)
		}
		if i > 0 && widthRanges[i-1].hi >= r.lo {
			t.Fatalf("ranges %d and %d overlap or are out of order", i-1, i)
		}
		if r.w != 0 && r.w != 2 {
			t.Fatalf("range %d: width %d", i, r.w)
		}
	}
}

func TestDecodeChar(t *testing.T) {
	for _, tt := range []struct {
		in string
		ok bool
	}{
		{"é", true},
		{"日", true},
		{"🙂", true},
		{"\xc0\x80", false},         // overlong
		{"\xed\xa0\x80", false},     // surrogate
		{"\xf4\x90\x80\x80", false}, // past U+10FFFF
		{"\xef\xbf\xbf", true},      // a noncharacter is still a code point
	} {
		if _, ok := DecodeChar([]byte(tt.in)); ok != tt.ok {
			t.Errorf("DecodeChar(%q) ok = %v, want %v", tt.in, ok, tt.ok)
		}
	}
}

func TestStringWidth(t *testing.T) {
	if got := StringWidth("a日🙂é"); got != 6 {
		t.Fatalf("StringWidth = %d, want 6", got)
	}
}
