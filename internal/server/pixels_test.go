//go:build darwin || linux

package server

import (
	"testing"
	"time"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/ipc"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/model"
)

// A client whose terminal gives no pixel size is asked for it after a resize (tty_resize), and
// its answers (tty_keys_winsz) give the window its cell when the window's size next changes:
// the pane is told its size in pixels from it.
func TestPixelsAskedAndAnswered(t *testing.T) {
	w := newInputWorld(t, "resize")
	w.waitPane("(960x288 pixels) READY") // 60×9 cells of 16×32
	asked := func() bool {
		var q bool
		if err := w.s.Do(func(*model.Tree) {
			for _, c := range w.s.attached {
				q = q || c.in.parser.WinsizeQuery
			}
		}); err != nil {
			t.Fatal(err)
		}
		return q
	}
	if asked() {
		t.Fatal("asked at attach, before a resize")
	}
	w.send(ipc.TypeResize, &ipc.Size{Cols: 60, Rows: 10})
	waitUntil(t, "the terminal is asked its size", asked)
	// It answers: 60×10 cells of 10×20 pixels. The cell is the client's until its next resize;
	// the window takes it when its size next changes (the status line going makes it taller).
	w.typed("\x1b[8;10;60t\x1b[4;200;600t")
	waitUntil(t, "the answers are read", func() bool { return !asked() })
	c, r := dial(t, w.s.opts.SocketPath, ipc.ProtocolVersion)
	defer c.Close()
	if res := command(t, c, r, "set", "-g", "status", "off"); res.Code != 0 {
		t.Fatalf("set status off: %+v", res)
	}
	w.waitPane("size: 60x10 (600x200 pixels) READY")
}

// waitUntil waits for cond, or fails the test after 10 s.
func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out: %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
