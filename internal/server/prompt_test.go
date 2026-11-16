//go:build darwin || linux

package server

import (
	"strings"
	"testing"
	"time"
)

// i-mux's rename-tab binding (TASKS.md C4.10): the prompt starts with the tab's name, and what
// is typed (spaces, quotes, $ and ;) becomes @imux_tab as typed; Escape changes nothing.
func TestRenameTabPrompt(t *testing.T) {
	w := newKeyWorld(t)
	deadline := time.Now().Add(10 * time.Second)
	for strings.TrimSpace(w.ask(t, "list-clients")) == "" {
		if time.Now().After(deadline) {
			t.Fatal("no attached client")
		}
		time.Sleep(10 * time.Millisecond)
	}
	w.run(t, "bind", "T", "command-prompt", "-I", "#{@imux_tab}", "-p", "Tab name:", `set-option -w @imux_tab "%%%"`)
	w.run(t, "set", "-w", "-t", "a", "@imux_tab", "old")
	tab := func() string { return strings.TrimSpace(w.ask(t, "display-message", "-p", "-t", "a", "#{@imux_tab}")) }
	waitTab := func(want string) {
		t.Helper()
		deadline := time.Now().Add(10 * time.Second)
		for tab() != want {
			if time.Now().After(deadline) {
				t.Fatalf("@imux_tab %q, want %q", tab(), want)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	promptOpen := func(want bool) {
		t.Helper()
		deadline := time.Now().Add(10 * time.Second)
		for {
			ch := make(chan bool, 1)
			w.s.post(func() {
				open := false
				for _, c := range w.s.attached {
					open = open || c.prompt != nil
				}
				ch <- open
			})
			if <-ch == want {
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("prompt open %v, want %v", !want, want)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	w.typed("\x02T")
	promptOpen(true)
	w.typed("\x01\x0b" + `my "tab" $x; y` + "\r")
	waitTab(`my "tab" $x; y`)
	promptOpen(false)

	w.typed("\x02T")
	promptOpen(true)
	w.typed("zzz\x1b")
	promptOpen(false)
	if got := tab(); got != `my "tab" $x; y` {
		t.Errorf("Escape set @imux_tab to %q", got)
	}
}
