//go:build darwin || linux || windows

package cli_test

import (
	"bytes"
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/cli"
)

// An attached client whose terminal is a stream (E19, TASKS.md C11.2).

type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuf) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Len()
}

func clientsOf(w *world) string {
	return w.run("list-clients", "-F", "#{client_name} #{client_width}x#{client_height}").out
}

func TestStreamAttach(t *testing.T) {
	w := newWorld(t)
	if r := w.run("new-session", "-d", "-s", "a"); r.code != 0 {
		t.Fatalf("new-session: %+v", r)
	}
	var out syncBuf
	ctx := context.Background()
	c, err := cli.Attach(ctx, []string{"-S", w.socket, "attach-session", "-t", "a"}, cli.StreamTerminal{Name: "phone-test", Cols: 100, Rows: 30}, &out)
	if err != nil {
		t.Fatalf("Attach: %v", err)
	}
	w.eventually("the screen in the stream", func() bool { return out.Len() > 0 })
	w.eventually("the client listed with its size", func() bool { return strings.Contains(clientsOf(w), "phone-test 100x30") })

	// Keys reach the pane as a terminal's would.
	if err := c.Input([]byte("echo hi-stream\r")); err != nil {
		t.Fatal(err)
	}
	w.eventually("the typed line in the pane", func() bool {
		return strings.Count(w.run("capture-pane", "-p", "-t", "a").out, "hi-stream") >= 2
	})

	if err := c.Resize(90, 20); err != nil {
		t.Fatal(err)
	}
	w.eventually("the new size", func() bool { return strings.Contains(clientsOf(w), "phone-test 90x20") })

	// Detached by the server: Wait says so.
	if r := w.run("detach-client", "-t", "phone-test"); r.code != 0 {
		t.Fatalf("detach-client: %+v", r)
	}
	done := make(chan string, 1)
	go func() { r, _ := c.Wait(); done <- r }()
	select {
	case r := <-done:
		if !strings.Contains(r, "detached") {
			t.Errorf("reason %q", r)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Wait did not return after a detach")
	}
	if err := c.Input([]byte("x")); err == nil {
		t.Error("input after the end")
	}

	// Close ends a client from this side.
	c2, err := cli.Attach(ctx, []string{"-S", w.socket, "attach-session", "-t", "a"}, cli.StreamTerminal{Name: "phone-two", Cols: 80, Rows: 24}, &out)
	if err != nil {
		t.Fatal(err)
	}
	w.eventually("the second client", func() bool { return strings.Contains(clientsOf(w), "phone-two") })
	_ = c2.Close()
	w.eventually("the second client gone", func() bool { return !strings.Contains(clientsOf(w), "phone-two") })
}

func TestStreamAttachRefuses(t *testing.T) {
	w := newWorld(t)
	var out syncBuf
	ctx := context.Background()
	term := cli.StreamTerminal{Name: "p", Cols: 80, Rows: 24}
	if _, err := cli.Attach(ctx, []string{"-S", w.socket, "new-session"}, term, &out); err == nil {
		t.Error("new-session with no server: a server was started")
	}
	if _, err := cli.Attach(ctx, []string{"-S", w.socket, "attach-session"}, term, &out); err == nil {
		t.Error("no server: attached")
	}
	if r := w.run("new-session", "-d", "-s", "a"); r.code != 0 {
		t.Fatalf("new-session: %+v", r)
	}
	if _, err := cli.Attach(ctx, []string{"-S", w.socket, "list-sessions"}, term, &out); err == nil {
		t.Error("a command that does not attach was taken as an attach")
	}
	if _, err := cli.Attach(ctx, []string{"-S", w.socket, "attach-session", "-t", "nosuch"}, term, &out); err == nil || !strings.Contains(err.Error(), "can't find session") {
		t.Errorf("unknown session: %v", err)
	}
}
