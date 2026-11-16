package termout

import (
	"encoding/json"
	"os"
	"testing"
)

// fixtureCaps are a recorded terminal's terminfo capabilities.
func fixtureCaps(t *testing.T, name string) []string {
	t.Helper()
	var rec struct{ Caps []string }
	b, err := os.ReadFile("../../testdata/term/" + name + ".json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &rec); err != nil {
		t.Fatal(err)
	}
	return rec.Caps
}

// SetSelection writes OSC 52 (Ms) once the terminal is started and when it has Ms (xterm's
// default terminal-features give it the clipboard feature).
func TestSetSelection(t *testing.T) {
	term, err := NewTerm("xterm-256color", fixtureCaps(t, "xterm-256color"), &Features{}, DefaultOptions)
	if err != nil {
		t.Fatal(err)
	}
	tty := NewTTY(term, 80, 24)
	tty.SetSelection("", []byte("hi"))
	if got := string(tty.Take()); got != "" {
		t.Errorf("before start: %q", got)
	}
	tty.Start()
	tty.Take()
	tty.SetSelection("", []byte("hi"))
	if got := string(tty.Take()); got != "\033]52;;aGk=\a" {
		t.Errorf("selection %q", got)
	}
	none, err := NewTerm("vt100", fixtureCaps(t, "vt100"), &Features{}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	tty = NewTTY(none, 80, 24)
	tty.Start()
	tty.Take()
	tty.SetSelection("", []byte("hi"))
	if got := string(tty.Take()); got != "" {
		t.Errorf("a terminal without Ms: %q", got)
	}
}
