//go:build darwin || linux

package server

import (
	"strings"
	"testing"
	"time"
)

// messageText is the attached client's message, "" for none.
func (w *keyWorld) messageText(t *testing.T) string {
	t.Helper()
	ch := make(chan string, 1)
	if !w.s.post(func() {
		text := ""
		for _, c := range w.s.attached {
			if c.message != nil {
				text = c.message.text
			}
		}
		ch <- text
	}) {
		t.Fatal("the server is gone")
	}
	return <-ch
}

func (w *keyWorld) waitMessage(t *testing.T, want string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for w.messageText(t) != want {
		if time.Now().After(deadline) {
			t.Fatalf("message %q, want %q", w.messageText(t), want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// Messages (TASKS.md C4.5): one shown until a key is cleared by the key, which goes on to the
// pane; -N drops keys until the message goes; a timed message goes by itself; a binding's
// error shows capitalised.
func TestMessages(t *testing.T) {
	w := newKeyWorld(t)
	// The client attaches on its own connection: wait until it has.
	deadline := time.Now().Add(10 * time.Second)
	for strings.TrimSpace(w.ask(t, "list-clients")) == "" {
		if time.Now().After(deadline) {
			t.Fatal("no attached client")
		}
		time.Sleep(10 * time.Millisecond)
	}
	w.run(t, "display-message", "-d", "0", "hello")
	w.waitMessage(t, "hello")
	w.typed("a")
	w.waitPane("key: 61")
	w.waitMessage(t, "")

	w.run(t, "display-message", "-N", "-d", "60000", "sticky")
	w.waitMessage(t, "sticky")
	w.typed("b")
	w.run(t, "display-message", "-d", "1", "short")
	w.waitMessage(t, "")
	w.typed("c")
	w.waitPane("key: 63")
	if s := w.screen(); strings.Contains(s, "key: 62") {
		t.Errorf("a key reached the pane under -N:\n%s", s)
	}

	w.run(t, "bind", "-n", "z", "select-pane", "-t", "nope")
	w.typed("z")
	w.waitMessage(t, "Can't find pane: nope")
}
