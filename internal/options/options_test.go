package options

import (
	"slices"
	"testing"
)

// Arrays keep numeric keys in order before the others; assigning splits at the separator.
func TestArrays(t *testing.T) {
	g := NewGlobals()
	o := g.Server.Get("terminal-features")
	if got := o.String("", false, false); got != "xterm*:clipboard:ccolour:cstyle:focus:title screen*:title rxvt*:ignorefkeys" {
		t.Errorf("default %q", got)
	}
	for _, k := range []string{"10", "2", "x", "007"} {
		v := "v" + k
		if err := o.ArraySet(k, &v, false); err != nil {
			t.Fatal(err)
		}
	}
	if got := o.ArrayKeys(); !slices.Equal(got, []string{"0", "1", "2", "7", "10", "x"}) {
		t.Errorf("keys %q", got)
	}
	// A key that is not all digits is a name, not an index.
	if _, ok := o.ArrayGet("x"); !ok {
		t.Error("a named key")
	}
	h := g.Session.Get("status-format")
	if len(h.ArrayKeys()) != 3 {
		t.Errorf("status-format keys %q", h.ArrayKeys())
	}
	// A hook takes its whole value as one item (the command language, which prints it again,
	// is not loaded here).
	hook := g.Session.Get("client-attached")
	if err := hook.ArrayAssign("display-message 'a, b'"); err != nil {
		t.Fatal(err)
	}
	if got := hook.ArrayKeys(); len(got) != 1 || hook.String("0", true, false) != "display-message 'a, b'" {
		t.Errorf("hook %q", got)
	}
}

// Values parse by type, with tmux's errors.
func TestFromString(t *testing.T) {
	g := NewGlobals()
	set := func(name, value string) error {
		e := Search(name)
		return g.Session.FromString(e, name, &value, false)
	}
	for _, tc := range []struct{ name, value, err string }{
		{"history-limit", "-1", "value is too small: -1"},
		{"history-limit", "x", "value is invalid: x"},
		{"history-limit", "99999999999", "value is too large: 99999999999"},
		{"mouse", "maybe", "bad value: maybe"},
		{"prefix", "C-", "bad key: C-"},
		{"status-bg", "nope", "bad colour: nope"},
		{"status-justify", "nowhere", "unknown value: nowhere"},
		{"history-limit", "10", ""},
		{"prefix", "C-a", ""},
		{"status-bg", "colour12", ""},
	} {
		err := set(tc.name, tc.value)
		if (err == nil) != (tc.err == "") || (err != nil && err.Error() != tc.err) {
			t.Errorf("%s %s: %v, want %q", tc.name, tc.value, err, tc.err)
		}
	}
	if got := g.Session.Get("prefix").String("", false, false); got != "C-a" {
		t.Errorf("prefix %q", got)
	}
	// A flag with no value toggles; a choice of two too.
	if err := g.Session.FromString(Search("mouse"), "mouse", nil, false); err != nil || g.Session.GetNumber("mouse") != 0 {
		t.Errorf("toggle: %v %d", err, g.Session.GetNumber("mouse"))
	}
	// Inheritance and appending.
	s := New(g.Session)
	if s.Get("history-limit").Owner != g.Session || s.GetOnly("history-limit") != nil {
		t.Error("inherited")
	}
	s.SetString("@u", false, "a")
	s.SetString("@u", true, "b")
	if s.GetString("@u") != "ab" {
		t.Errorf("append %q", s.GetString("@u"))
	}
}

// Names match by unique prefix; others are mapped; bad array keys are ambiguous, as in tmux.
func TestMatch(t *testing.T) {
	for _, tc := range []struct {
		in, name, key string
		ambiguous     bool
	}{
		{"history-l", "history-limit", "", false},
		{"status-format[01]", "status-format", "1", false},
		{"pane-colors", "pane-colours", "", false},
		{"@x[2]", "@x", "2", false},
		{"s", "", "", true},
		{"status-format[x", "", "", true},
		{"nonsense", "", "", false},
	} {
		name, key, _, amb := Match(tc.in)
		if name != tc.name || key != tc.key || amb != tc.ambiguous {
			t.Errorf("%q: %q %q %v", tc.in, name, key, amb)
		}
	}
}
