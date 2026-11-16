//go:build darwin || linux

package server

import (
	"encoding/json"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/ipc"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/model"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/vt"
)

// An attached client is drawn (TASKS.md C2.8): its first frame shows the session's pane,
// output in the pane reaches it, and after a resize it is drawn whole at its new size.
func TestAttachedClientDrawn(t *testing.T) {
	var rec struct{ Caps []string }
	b, err := os.ReadFile("../../testdata/term/xterm-256color.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &rec); err != nil {
		t.Fatal(err)
	}
	l, path := listen(t)
	s := startServer(t, Options{SocketPath: path, Environ: env})
	go s.Serve(l)
	c, r := dial(t, path, ipc.ProtocolVersion)
	if res := command(t, c, r, "new-session", "-d", "-s", "a", "-x", "40", "-y", "10",
		"printf hello; read x; printf ' world'; exec sleep 100"); res.Code != 0 {
		t.Fatalf("new-session: %+v", res)
	}
	c.Close()

	// Turn the status line off so this test checks pane drawing, not the status renderer.
	time.Sleep(50 * time.Millisecond)
	sc, sr := dial(t, path, ipc.ProtocolVersion)
	if res := command(t, sc, sr, "set", "-g", "status", "off"); res.Code != 0 {
		t.Fatalf("set status off: %+v", res)
	}
	sc.Close()

	conn, err := ipc.Dial(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	hello := &ipc.Hello{
		Protocol: ipc.ProtocolVersion, Cwd: "/", StdinTTY: true, StdoutTTY: true, UTF8: true,
		TTYName: "/dev/pts/test", Size: ipc.Size{Cols: 40, Rows: 10},
		Env: map[string]string{"TERM": "xterm-256color"}, Terminfo: rec.Caps,
	}
	for _, m := range []struct {
		t ipc.Type
		v any
	}{{ipc.TypeHello, hello}, {ipc.TypeCommand, &ipc.Command{Commands: [][]string{{"attach-session", "-t", "a"}}}}} {
		f, _ := ipc.Encode(m.t, m.v)
		if err := ipc.WriteFrame(conn, f.Type, f.Payload); err != nil {
			t.Fatal(err)
		}
	}
	var mu sync.Mutex
	screen := vt.NewTerminal(40, 10, 0)
	rd := ipc.NewReader(conn)
	go func() {
		for {
			f, err := rd.Read()
			if err != nil {
				return
			}
			if f.Type == ipc.TypeOutput {
				mu.Lock()
				_, _ = screen.Write(f.Payload)
				mu.Unlock()
			}
		}
	}()
	shown := func() string {
		mu.Lock()
		defer mu.Unlock()
		return strings.TrimRight(screen.Screen.Capture(vt.CaptureOptions{}), "\n")
	}
	waitFor := func(what, want string) {
		t.Helper()
		deadline := time.Now().Add(10 * time.Second)
		for shown() != want {
			if time.Now().After(deadline) {
				t.Fatalf("%s: the client shows %q, want %q", what, shown(), want)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
	waitFor("first frame", "hello")

	// Output in the pane is drawn.
	var p0 *model.Pane
	if err := s.Do(func(tr *model.Tree) { p0 = tr.Pane(0) }); err != nil || p0 == nil {
		t.Fatalf("pane: %v", err)
	}
	if _, err := p0.Proc.Write([]byte("\n")); err != nil {
		t.Fatal(err)
	}
	// The terminal echoes the newline, then the program writes.
	waitFor("pane output", "hello\n world")

	// A resize draws everything again at the new size.
	mu.Lock()
	screen = vt.NewTerminal(20, 5, 0)
	mu.Unlock()
	f, _ := ipc.Encode(ipc.TypeResize, &ipc.Size{Cols: 20, Rows: 5})
	if err := ipc.WriteFrame(conn, f.Type, f.Payload); err != nil {
		t.Fatal(err)
	}
	waitFor("resize", "hello\n world")
}
