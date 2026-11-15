//go:build darwin || linux

package vt_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/pty"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/vt"
)

// The fake TUI (testdata/faketui) run on a real PTY, its output fed to the emulator: each of
// its modes is checked here (TASKS.md C1.9).

var faketui string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "faketui-")
	if err != nil {
		panic(err)
	}
	faketui = filepath.Join(dir, "faketui")
	cmd := exec.Command("go", "build", "-o", faketui, "../../testdata/faketui")
	cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
	if err := cmd.Run(); err != nil {
		panic("building faketui: " + err.Error())
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// pane is the fake TUI on a PTY and the emulator showing it.
type pane struct {
	t     *testing.T
	proc  pty.Proc
	mu    sync.Mutex
	term  *vt.Terminal
	bytes int
	done  chan struct{}
}

func start(t *testing.T, setup func(*vt.Terminal), args ...string) *pane {
	t.Helper()
	p := &pane{t: t, term: vt.NewTerminal(80, 24, 2000), done: make(chan struct{})}
	if setup != nil {
		setup(p.term)
	}
	proc, err := pty.New().Start(pty.Spec{
		Argv: append([]string{faketui}, args...),
		Env:  []string{"PATH=/usr/bin:/bin", "TERM=tmux-256color", "LANG=C.UTF-8"},
		Dir:  "/", Cols: 80, Rows: 24,
	})
	if err != nil {
		t.Fatal(err)
	}
	p.proc = proc
	go func() {
		defer close(p.done)
		buf := make([]byte, 32<<10)
		for {
			n, err := proc.Read(buf)
			p.mu.Lock()
			_, _ = p.term.Write(buf[:n])
			p.bytes += n
			p.mu.Unlock()
			if err != nil {
				return
			}
		}
	}()
	t.Cleanup(func() {
		_ = syscall.Kill(-proc.Pid(), syscall.SIGKILL)
		_, _ = proc.Wait()
		_ = proc.Close()
		<-p.done
	})
	return p
}

// with runs f on the emulator while no output is being fed to it.
func (p *pane) with(f func(t *vt.Terminal)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	f(p.term)
}

func (p *pane) screen() string {
	var s string
	// Joined, so that a line the screen wrapped reads whole.
	p.with(func(t *vt.Terminal) { s = t.Screen.Capture(vt.CaptureOptions{Join: true}) })
	return s
}

// wait polls until cond holds on the screen's text.
func (p *pane) wait(what string, cond func(screen string) bool) {
	p.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		s := p.screen()
		if cond(s) {
			return
		}
		if time.Now().After(deadline) {
			p.t.Fatalf("timed out waiting for %s; screen:\n%s", what, s)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func (p *pane) waitText(text string) {
	p.t.Helper()
	p.wait(strings.TrimSpace(text), func(s string) bool { return strings.Contains(s, text) })
}

func (p *pane) send(s string) {
	p.t.Helper()
	if _, err := p.proc.Write([]byte(s)); err != nil {
		p.t.Fatal(err)
	}
}

func TestFakeTUIFrame(t *testing.T) {
	p := start(t, nil, "-mode", "frame")
	p.waitText("READY")
	p.with(func(term *vt.Terminal) {
		s := term.Screen
		if !s.IsAlternate() {
			t.Error("the frame is not on the alternate screen")
		}
		if s.Mode&vt.ModeSync != 0 {
			t.Error("synchronized output still on after the frame")
		}
		capture := s.Capture(vt.CaptureOptions{})
		for _, want := range []string{"┌──", "│ faketui 日本語 🙂", "│ link", "size 80x24", "└──"} {
			if !strings.Contains(capture, want) {
				t.Errorf("frame lacks %q:\n%s", want, capture)
			}
		}
		if c := s.Grid.ViewCell(2, 1); c.Data != "f" || c.Fg != vt.ColorFlagRGB|0xff8000 {
			t.Errorf("RGB cell = %+v", c)
		}
		if c := s.Grid.ViewCell(10, 1); c.Width != 2 || c.Data != "日" {
			t.Errorf("wide cell = %+v", c)
		}
		link := s.Grid.ViewCell(2, 2)
		if uri, id, ok := s.Hyperlinks.Get(link.Link); !ok || uri != "https://example.com/faketui" || id != "ft" {
			t.Errorf("link cell %+v: %q %q %v", link, uri, id, ok)
		}
	})
	// A resize makes it draw again for the new size.
	if err := p.proc.Resize(60, 15); err != nil {
		t.Fatal(err)
	}
	p.waitText("size 60x15")
	p.send("q")
	p.wait("the main screen back", func(string) bool {
		alt := true
		p.with(func(term *vt.Terminal) { alt = term.Screen.IsAlternate() })
		return !alt
	})
}

func TestFakeTUIKeys(t *testing.T) {
	p := start(t, nil, "-mode", "keys")
	p.waitText("key READY")
	for in, want := range map[string]string{
		"\x1b[A":     "key: 1b5b41 Up",
		"\x1b[13;5u": "key: 1b5b31333b3575 C-Enter",
		"a":          `key: 61 "a"`,
		"\x01":       "key: 01 C-a",
	} {
		p.send(in)
		p.waitText(want)
	}
	p.send("\x04")
	if st, err := p.proc.Wait(); err != nil || st.Code != 0 {
		t.Fatalf("exit %+v %v", st, err)
	}
}

func TestFakeTUIMouse(t *testing.T) {
	p := start(t, nil, "-mode", "mouse")
	p.waitText("mouse READY")
	p.with(func(term *vt.Terminal) {
		// 1000, 1002 and 1003 replace one another in tmux: the last one, 1002, is left.
		m := term.Screen.Mode
		if m&vt.AllMouseModes != vt.ModeMouseButton || m&vt.ModeMouseSGR == 0 {
			t.Errorf("mouse modes %#x, want button tracking and SGR", m)
		}
	})
	p.send("\x1b[<0;10;5M\x1b[<0;10;5m")
	p.waitText("press 0 at 10,5, release 0 at 10,5")
	p.send("\x04")
	p.wait("mouse reporting off", func(string) bool {
		var m int
		p.with(func(term *vt.Terminal) { m = term.Screen.Mode })
		return m&(vt.AllMouseModes|vt.ModeMouseSGR) == 0
	})
}

func TestFakeTUIPaste(t *testing.T) {
	p := start(t, nil, "-mode", "paste")
	p.waitText("paste READY")
	p.with(func(term *vt.Terminal) {
		if term.Screen.Mode&vt.ModeBracketPaste == 0 {
			t.Error("bracketed paste not on")
		}
	})
	p.send("\x1b[200~hello world\x1b[201~")
	p.waitText(`pasted "hello world"`)
}

func TestFakeTUIResize(t *testing.T) {
	p := start(t, nil, "-mode", "resize")
	p.waitText("size: 80x24")
	if err := p.proc.Resize(100, 30); err != nil {
		t.Fatal(err)
	}
	p.waitText("size: 100x30")
}

func TestFakeTUIStream(t *testing.T) {
	p := start(t, nil, "-mode", "stream", "-mb", "2")
	p.waitText("STREAM DONE")
	<-p.done
	p.with(func(term *vt.Terminal) {
		if p.bytes < 2<<20 {
			t.Errorf("only %d bytes read", p.bytes)
		}
		if term.Screen.Grid.HSize == 0 {
			t.Error("no history after streaming")
		}
	})
}

func TestFakeTUITitle(t *testing.T) {
	var copied string
	p := start(t, func(term *vt.Terminal) {
		term.ClipboardMode = vt.ClipboardOn
		term.SetClipboard = func(_ string, b []byte) { copied = string(b) }
	}, "-mode", "title", "-title", "agent", "-cwd", "/tmp/work")
	p.waitText("done")
	<-p.done
	host, _ := os.Hostname()
	p.with(func(term *vt.Terminal) {
		s := term.Screen
		if s.Title != "agent" {
			t.Errorf("title %q", s.Title)
		}
		if s.Path != "file://"+host+"/tmp/work" || s.Cwd != "/tmp/work" {
			t.Errorf("path %q, cwd %q", s.Path, s.Cwd)
		}
		if copied != "copied by faketui" {
			t.Errorf("copied %q", copied)
		}
	})
}

func TestFakeTUIExit(t *testing.T) {
	p := start(t, nil, "-mode", "exit", "-code", "7")
	if st, err := p.proc.Wait(); err != nil || st.Code != 7 {
		t.Fatalf("exit %+v %v, want code 7", st, err)
	}
	p = start(t, nil, "-mode", "exit", "-signal", "TERM")
	if st, err := p.proc.Wait(); err != nil || st.Signal != int(syscall.SIGTERM) {
		t.Fatalf("exit %+v %v, want SIGTERM", st, err)
	}
}
