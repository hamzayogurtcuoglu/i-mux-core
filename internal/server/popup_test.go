//go:build darwin || linux

package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/ipc"
)

// popupShown reports whether the attached client shows a popup.
func (w *keyWorld) popupShown(t *testing.T) bool {
	t.Helper()
	ch := make(chan bool, 1)
	if !w.s.post(func() {
		shown := false
		for _, c := range w.s.attached {
			shown = shown || c.popup != nil
		}
		ch <- shown
	}) {
		t.Fatal("the server is gone")
	}
	return <-ch
}

func (w *keyWorld) waitPopup(t *testing.T, want bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for w.popupShown(t) != want {
		if time.Now().After(deadline) {
			t.Fatalf("popup shown %v, want %v", !want, want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// waitFile waits for a file to exist and hold want.
func waitFile(t *testing.T, path, want string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		b, err := os.ReadFile(path)
		if err == nil && strings.Contains(string(b), want) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s holds %q, want %q", path, b, want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// Popups (TASKS.md C4.6): a command-line client waits for its popup and gets its status; keys
// go to the popup's program and not to the pane; the popup closes when its client detaches.
func TestPopups(t *testing.T) {
	w := newKeyWorld(t)
	deadline := time.Now().Add(10 * time.Second)
	for strings.TrimSpace(w.ask(t, "list-clients")) == "" {
		if time.Now().After(deadline) {
			t.Fatal("no attached client")
		}
		time.Sleep(10 * time.Millisecond)
	}
	// A command-line client waits for its popup and exits with its status.
	c, r := dial(t, w.s.opts.SocketPath, ipc.ProtocolVersion)
	defer c.Close()
	if res := command(t, c, r, "display-popup", "-E", "exit 3"); res.Code != 3 {
		t.Errorf("display-popup: %+v", res)
	}

	out := filepath.Join(t.TempDir(), "keys")
	w.run(t, "bind", "-n", "F2", "display-popup", "-E", faketui(t)+" -mode keys -out "+out)
	w.typed("\x1bOQ")
	w.waitPopup(t, true)
	waitFile(t, out, "")
	w.typed("q")
	waitFile(t, out, "71")
	if strings.Contains(w.screen(), "key: 71") {
		t.Errorf("a key reached the pane under the popup:\n%s", w.screen())
	}

	// The popup goes with its client.
	w.run(t, "detach-client")
	w.waitPopup(t, false)
}

// While a popup is open, the active pane loses the focus, and gets it back when it closes
// (server_client_set_overlay and server_client_clear_overlay update the window's focus).
func TestPopupFocus(t *testing.T) {
	w := newInputWorld(t, "focus")
	w.waitPane("focus: 1b5b49 in")
	kw := &keyWorld{inputWorld: w}
	kw.run(t, "bind", "-n", "F2", "display-popup", "exec cat")
	w.typed("\x1bOQ")
	w.waitPane("focus: 1b5b4f out")
	kw.run(t, "display-popup", "-C")
	w.waitPane("focus: 1b5b49 in")
}
