//go:build darwin || linux

package server

import (
	"strings"
	"testing"
	"time"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/ipc"
)

// With set-clipboard on, a program's OSC 52 becomes a paste buffer (input_osc_52); with the
// default, external, it is ignored.
func TestPaneClipboard(t *testing.T) {
	l, path := listen(t)
	s := startServer(t, Options{SocketPath: path, Environ: env})
	go s.Serve(l)
	ask := func(words ...string) string {
		c, r := dial(t, path, ipc.ProtocolVersion)
		defer c.Close()
		return command(t, c, r, words...).Stdout
	}
	ask("new-session", "-d", "-s", "a", `printf '\033]52;c;aWdub3JlZA==\a'; echo first; exec cat`)
	deadline := time.Now().Add(10 * time.Second)
	for !strings.Contains(ask("capture-pane", "-p", "-t", "a"), "first") {
		if time.Now().After(deadline) {
			t.Fatal("the first program did not run")
		}
		time.Sleep(10 * time.Millisecond)
	}
	ask("set", "-s", "set-clipboard", "on")
	ask("split-window", "-t", "a", `printf '\033]52;c;dGFrZW4=\a'; exec cat`)
	for {
		got := ask("list-buffers", "-F", "#{buffer_sample}")
		if strings.Contains(got, "taken") {
			if strings.Contains(got, "ignored") {
				t.Errorf("buffers %q: the OSC 52 sent with set-clipboard external was taken", got)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("buffers %q", got)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
