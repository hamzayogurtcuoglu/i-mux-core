package termout

import (
	"reflect"
	"testing"
)

// The entries in testdata/term/terminfo (imux-test.src, compiled by ncurses' tic): the legacy
// format and the 32-bit one, with extended capabilities.
func TestReadTerminfo(t *testing.T) {
	dirs := []string{"../../testdata/term/terminfo"}
	ti, err := Lookup("imux-test", dirs)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(ti.Names, []string{"imux-test", "terminal for the reader test"}) {
		t.Errorf("names %q", ti.Names)
	}
	for name, want := range map[string]bool{"am": true, "xenl": true, "bw": false, "AX": true, "XT": true} {
		if ti.Bools[name] != want {
			t.Errorf("bool %s = %v", name, ti.Bools[name])
		}
	}
	if _, ok := ti.Bools["Ext"]; ok {
		t.Error("a cancelled extended flag is present")
	}
	for name, want := range map[string]int{"cols": 80, "lines": 24, "colors": 256, "Num": 7} {
		if ti.Numbers[name] != want {
			t.Errorf("number %s = %d, want %d", name, ti.Numbers[name], want)
		}
	}
	for name, want := range map[string]string{
		"clear": "\033[H\033[2J",
		"smcup": "\033[?1049h$<5>",
		"Smulx": "\033[4:%p1%dm",
		"Sync":  "\033[?2026%?%p1%{1}%-%tl%eh%;",
	} {
		if ti.Strings[name] != want {
			t.Errorf("string %s = %q, want %q", name, ti.Strings[name], want)
		}
	}

	wide, err := Lookup("imux-test-wide", dirs)
	if err != nil {
		t.Fatal(err)
	}
	if wide.Numbers["colors"] != 16777216 || wide.Numbers["pairs"] != 65536 || wide.Strings["Ss"] != "\033[%p1%d q" || !wide.Bools["AX"] {
		t.Errorf("32-bit entry: colors %d pairs %d Ss %q", wide.Numbers["colors"], wide.Numbers["pairs"], wide.Strings["Ss"])
	}

	// What tmux is sent: flags tmux knows from the entry, including standard ones that are off,
	// but no extended flag the entry does not define; padding is stripped by the server.
	caps := ti.Capabilities()
	has := func(c string) bool {
		for _, x := range caps {
			if x == c {
				return true
			}
		}
		return false
	}
	for _, c := range []string{"am=1", "bce=0", "AX=1", "XT=1", "colors=256", "smcup=\033[?1049h$<5>"} {
		if !has(c) {
			t.Errorf("capabilities lack %q", c)
		}
	}
	if has("Tc=0") || has("RGB=0") {
		t.Error("an undefined extended flag is sent")
	}

	if _, err := Lookup("nosuch", dirs); err == nil || err.Error() != "missing or unsuitable terminal: nosuch" {
		t.Errorf("missing entry: %v", err)
	}
	if _, err := Lookup("x", []string{"/nonexistent"}); err == nil || err.Error() != "can't find terminfo database" {
		t.Errorf("no database: %v", err)
	}
	if _, err := ParseTerminfo([]byte{1, 2, 3}); err == nil {
		t.Error("garbage parsed")
	}
}

func FuzzParseTerminfo(f *testing.F) {
	f.Add([]byte{0x1a, 0x01, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0})
	f.Fuzz(func(t *testing.T, b []byte) {
		if ti, err := ParseTerminfo(b); err == nil {
			_ = ti.Capabilities()
		}
	})
}

// The built-in xterm-256color (E15) is ncurses' entry, as the system's.
func TestBuiltin(t *testing.T) {
	ti, err := Builtin()
	if err != nil {
		t.Fatal(err)
	}
	if ti.Numbers["colors"] != 256 || ti.Strings["cup"] == "" || !ti.Bools["am"] {
		t.Errorf("built-in: colors %d, cup %q, am %v", ti.Numbers["colors"], ti.Strings["cup"], ti.Bools["am"])
	}
	if !NoDatabase([]string{t.TempDir() + "/none"}) || NoDatabase([]string{t.TempDir()}) {
		t.Error("NoDatabase")
	}
}
