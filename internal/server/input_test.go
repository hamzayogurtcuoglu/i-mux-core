//go:build darwin || linux

package server

import (
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/ipc"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/model"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/vt"
)

// Input from an attached client reaches the pane as tmux sends it (TASKS.md C2.10): a
// bracketed paste, focus reports and theme reports, checked with the fake TUI's echo.

var (
	faketuiOnce sync.Once
	faketuiPath string
	faketuiErr  error
)

func faketui(t *testing.T) string {
	t.Helper()
	faketuiOnce.Do(func() {
		dir, err := os.MkdirTemp("", "faketui-")
		if err != nil {
			faketuiErr = err
			return
		}
		faketuiPath = filepath.Join(dir, "faketui")
		out, err := exec.Command("go", "build", "-o", faketuiPath, "../../testdata/faketui").CombinedOutput()
		if err != nil {
			faketuiErr = &exec.Error{Name: string(out), Err: err}
		}
	})
	if faketuiErr != nil {
		t.Fatal(faketuiErr)
	}
	return faketuiPath
}

// inputWorld is a server with one session running the fake TUI in mode, and a client
// attached to it.
type inputWorld struct {
	t      *testing.T
	s      *Server
	conn   net.Conn
	mu     sync.Mutex
	events []string
}

func newInputWorld(t *testing.T, mode string) *inputWorld {
	t.Helper()
	w := &inputWorld{t: t}
	l, path := listen(t)
	w.s = startServer(t, Options{SocketPath: path, Environ: env, Event: func(name, client string) {
		w.mu.Lock()
		w.events = append(w.events, name)
		w.mu.Unlock()
	}})
	go w.s.Serve(l)
	c, r := dial(t, path, ipc.ProtocolVersion)
	if res := command(t, c, r, "new-session", "-d", "-s", "a", "-x", "60", "-y", "10",
		"exec "+faketui(t)+" -mode "+mode); res.Code != 0 {
		t.Fatalf("new-session: %+v", res)
	}
	c.Close()
	w.waitPane(" READY")

	var rec struct{ Caps []string }
	b, err := os.ReadFile("../../testdata/term/xterm-256color.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &rec); err != nil {
		t.Fatal(err)
	}
	if w.conn, err = ipc.Dial(path); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { w.conn.Close() })
	erase := 0x7f
	hello := &ipc.Hello{
		Protocol: ipc.ProtocolVersion, Cwd: "/", StdinTTY: true, StdoutTTY: true, UTF8: true,
		TTYName: "/dev/pts/test", Size: ipc.Size{Cols: 60, Rows: 10},
		Env: map[string]string{"TERM": "xterm-256color"}, Terminfo: rec.Caps, Erase: &erase,
	}
	w.send(ipc.TypeHello, hello)
	w.send(ipc.TypeCommand, &ipc.Command{Commands: [][]string{{"attach-session", "-t", "a"}}})
	rd := ipc.NewReader(w.conn)
	go func() {
		for {
			if _, err := rd.Read(); err != nil {
				return
			}
		}
	}()
	return w
}

func (w *inputWorld) send(t ipc.Type, v any) {
	w.t.Helper()
	f, err := ipc.Encode(t, v)
	if err != nil {
		w.t.Fatal(err)
	}
	if err := ipc.WriteFrame(w.conn, f.Type, f.Payload); err != nil {
		w.t.Fatal(err)
	}
}

// typed sends bytes as the client's terminal would.
func (w *inputWorld) typed(s string) {
	w.t.Helper()
	if err := ipc.WriteFrame(w.conn, ipc.TypeInput, []byte(s)); err != nil {
		w.t.Fatal(err)
	}
}

// screen is the pane's screen.
func (w *inputWorld) screen() string {
	var out string
	_ = w.s.Do(func(tr *model.Tree) {
		if p := tr.Pane(0); p != nil && p.Term != nil {
			p.Mu.Lock()
			out = p.Term.Screen.Capture(vt.CaptureOptions{})
			p.Mu.Unlock()
		}
	})
	return out
}

func (w *inputWorld) waitPane(want string) {
	w.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !strings.Contains(w.screen(), want) {
		if time.Now().After(deadline) {
			w.t.Fatalf("the pane does not show %q:\n%s", want, w.screen())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (w *inputWorld) waitEvent(name string) {
	w.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		w.mu.Lock()
		got := strings.Join(w.events, " ")
		w.mu.Unlock()
		if strings.Contains(" "+got+" ", " "+name+" ") {
			return
		}
		if time.Now().After(deadline) {
			w.t.Fatalf("no %s event; got %q", name, got)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// A bracketed paste reaches a pane that asked for bracketed paste in its brackets, its text as
// it came, keys and escapes inside it included.
func TestPasteReachesPane(t *testing.T) {
	w := newInputWorld(t, "paste")
	w.typed("\x1b[200~hello\r\x1b[Aworld\x1b[201~")
	w.waitPane(`pasted "hello\r\x1b[Aworld"`)
}

// Focus reports reach a pane that asked for them (1004): it has the focus when the client
// attaches, loses it and gets it back with the client's terminal; the client and pane events
// fire.
func TestFocusReachesPane(t *testing.T) {
	w := newInputWorld(t, "focus")
	w.waitPane("focus: 1b5b49 in")
	w.waitEvent("pane-focus-in")
	w.typed("\x1b[O")
	w.waitPane("focus: 1b5b4f out")
	w.waitEvent("client-focus-out")
	w.waitEvent("pane-focus-out")
	w.typed("\x1b[I")
	w.waitPane("focus: 1b5b49 in")
	w.waitEvent("client-focus-in")
}

// A theme report fires the client's theme event; once the terminal's colours arrive, a pane
// that asked for theme reports (2031) hears the theme, and its OSC 11 query is answered with
// the terminal's background.
func TestThemeReport(t *testing.T) {
	w := newInputWorld(t, "focus")
	w.waitPane("focus: 1b5b49 in")
	w.typed("\x1b[?997;1n")
	w.waitEvent("client-dark-theme")
	w.typed("\x1b]10;rgb:ffff/ffff/ffff\x1b\\\x1b]11;rgb:1010/1010/1010\x1b\\")
	w.waitPane("focus: 1b5b3f3939373b316e dark")
	w.typed("\x1b[?997;2n")
	w.waitEvent("client-light-theme")
	var bg vt.Color
	_ = w.s.Do(func(tr *model.Tree) {
		p := tr.Pane(0)
		p.Mu.Lock()
		bg = p.Term.Palette.ClientBg
		p.Mu.Unlock()
	})
	if bg != vt.ColorJoinRGB(0x10, 0x10, 0x10) {
		t.Errorf("the pane's client background %#x", bg)
	}
}

// Keys typed in an attached client reach its active pane, encoded for the pane (TASKS.md
// C2.11): C-Up read from the terminal goes as xterm's sequence, a Meta key with its escape.
func TestTypedKeysReachPane(t *testing.T) {
	w := newInputWorld(t, "keys")
	w.typed("\x1b[1;5A")
	w.waitPane("key: 1b5b313b3541")
	w.typed("\x1bv")
	w.waitPane("key: 1b76 M-v")
	w.typed("\x7f")
	w.waitPane("key: 7f BSpace")
}
