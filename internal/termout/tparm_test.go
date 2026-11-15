package termout

import "testing"

// Expansions checked against ncurses' tiparm (macOS's ncurses).
func TestTparm(t *testing.T) {
	setaf := `\E[%?%p1%{8}%<%t3%p1%d%e%p1%{16}%<%t9%p1%{8}%-%d%e38;5;%p1%d%;m`
	unvis := func(s string) string {
		u, ok := strunvis(s)
		if !ok {
			t.Fatalf("strunvis(%q)", s)
		}
		return u
	}
	for _, tc := range []struct {
		s      string
		params []any
		want   string
	}{
		{"\033[%i%p1%d;%p2%dH", []any{4, 9}, "\033[5;10H"},
		{unvis(setaf), []any{1}, "\033[31m"},
		{unvis(setaf), []any{9}, "\033[91m"},
		{unvis(setaf), []any{200}, "\033[38;5;200m"},
		{"\033[?2026%?%p1%{1}%-%tl%eh%;", []any{1}, "\033[?2026h"},
		{"\033[?2026%?%p1%{1}%-%tl%eh%;", []any{2}, "\033[?2026l"},
		{"\033[58::2::%p1%{65536}%/%d::%p1%{256}%/%{255}%&%d::%p1%{255}%&%d%;m", []any{0x123456}, "\033[58::2::18::52::86m"},
		{"\033[38;2;%p1%d;%p2%d;%p3%dm", []any{1, 2, 3}, "\033[38;2;1;2;3m"},
		{"%p1%03d|%p1%x|%p1%X|%p1%o|%p1%:-5d|%p2%c", []any{42, 65}, "042|2a|2A|52|42   |A"},
		{"%p1%p2%*%d %p1%p2%m%d %p1%!%d %p1%~%d %'a'%d %p1%p2%>%d", []any{7, 3}, "21 1 0 -8 97 1"},
		{"%p1%Pa%ga%ga%+%d", []any{5}, "10"},
		{"%?%p1%t%?%p2%tA%eB%;%eC%;", []any{1, 0}, "B"},
		{"\033]52;%p1%s;%p2%s\a", []any{"c", "?"}, "\033]52;c;?\a"},
		{"\033]8;%?%p1%l%tid=%p1%s%;;%p2%s\033\\", []any{"", "u"}, "\033]8;;u\033\\"},
		{"\033]8;%?%p1%l%tid=%p1%s%;;%p2%s\033\\", []any{"x", "u"}, "\033]8;id=x;u\033\\"},
		{"no params", nil, "no params"},
	} {
		if got := Tparm(tc.s, tc.params...); got != tc.want {
			t.Errorf("Tparm(%q, %v) = %q, want %q", tc.s, tc.params, got, tc.want)
		}
	}
}

func TestStrunvis(t *testing.T) {
	for in, want := range map[string]string{
		`\E[1m`:  "\033[1m",
		`a\\b`:   `a\b`,
		`\033`:   "\033",
		`\7x`:    "\007x",
		`\^G`:    "\007",
		`\M-a`:   "\xe1",
		`\s\t\n`: " \t\n",
	} {
		if got, ok := strunvis(in); !ok || got != want {
			t.Errorf("strunvis(%q) = %q %v, want %q", in, got, ok, want)
		}
	}
	if _, ok := strunvis(`\e[1m`); ok {
		t.Error(`\e is not an escape of unvis`)
	}
}
