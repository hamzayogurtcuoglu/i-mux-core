//go:build darwin || linux

package cli_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"golang.org/x/sys/unix"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/pty"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/vt"
)

// An attach and detach cycle in a PTY (TASKS.md C2.7): the client's terminal gets tmux's
// start sequence and queries, then tmux's stop sequence and the detach message, exactly as
// tmux 3.8 sent them for the same terminal (testdata/term/xterm-256color.json, recorded by
// cmd/termfixture); every mode and the termios settings are back as they were.

type output struct {
	mu sync.Mutex
	b  strings.Builder
}

func (o *output) String() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.b.String()
}

func TestAttachDetach(t *testing.T) {
	w := newWorld(t)
	terminfo, err := filepath.Abs("../../testdata/term/terminfo")
	if err != nil {
		t.Fatal(err)
	}
	w.env = append(w.env, "TERM=xterm-256color", "TERMINFO="+terminfo)
	w.session("a")

	var rec struct{ Enter, Leave string }
	b, err := os.ReadFile("../../testdata/term/xterm-256color.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &rec); err != nil {
		t.Fatal(err)
	}

	p, err := pty.New().Start(pty.Spec{
		Path: bin, Argv: []string{"imux-core", "-u", "-S", w.socket, "attach", "-t", "a"},
		Env: w.env, Dir: w.dir, Cols: 80, Rows: 24,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Kill(); _ = p.Close() })
	before, err := unix.IoctlGetTermios(p.Fd(), ioctlGetTermios)
	if err != nil {
		t.Fatal(err)
	}
	var out output
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := p.Read(buf)
			out.mu.Lock()
			out.b.Write(buf[:n])
			out.mu.Unlock()
			if err != nil {
				return
			}
		}
	}()
	w.eventually("the start sequence", func() bool { return strings.HasSuffix(out.String(), "\033]11;?\033\\") })
	if got := out.String(); got != rec.Enter {
		t.Fatalf("attach:\ntmux:      %q\nimux-core: %q", rec.Enter, got)
	}
	w.must("detach-client")
	w.eventually("the detach message", func() bool { return strings.Contains(out.String(), "]\r\n") })
	if st, err := p.Wait(); err != nil || st.Code != 0 {
		t.Errorf("client exit %+v %v", st, err)
	}
	want := rec.Enter + rec.Leave + "[detached (from session a)]\r\n"
	if got := out.String(); got != want {
		t.Errorf("attach and detach:\nwant:      %q\nimux-core: %q", want, got)
	}

	// Every mode the client set is back: fed to an emulator, the output leaves its modes
	// and screen as they were. (cnorm's ESC [ ? 12 l leaves the emulator's note that the
	// blinking was set, which is no mode of the terminal; tmux's own bytes leave it too.)
	term := vt.NewTerminal(80, 24, 0)
	modes := term.Screen.Mode
	_, _ = term.Write([]byte(out.String()))
	if term.Screen.IsAlternate() || term.Screen.Mode&^vt.ModeCursorBlinkingSet != modes {
		t.Errorf("modes %#x (alternate %v), were %#x", term.Screen.Mode, term.Screen.IsAlternate(), modes)
	}
	after, err := unix.IoctlGetTermios(p.Fd(), ioctlGetTermios)
	if err == nil && (after.Lflag != before.Lflag || after.Iflag != before.Iflag || after.Oflag != before.Oflag) {
		t.Errorf("termios not restored: lflag %#x→%#x iflag %#x→%#x oflag %#x→%#x",
			before.Lflag, after.Lflag, before.Iflag, after.Iflag, before.Oflag, after.Oflag)
	}
	// The session is still there and the server too.
	w.must("has-session", "-t", "a")
}

// A session killed under an attached client ends it with "[exited]" (exit 0); a server killed,
// with "[server exited]" (exit 1), as tmux 3.8's client does. The stop sequence comes first.
func TestAttachEnds(t *testing.T) {
	terminfo, err := filepath.Abs("../../testdata/term/terminfo")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		kill []string
		msg  string
		code int
	}{
		{[]string{"kill-session", "-t", "a"}, "[exited]\r\n", 0},
		{[]string{"kill-server"}, "[server exited]\r\n", 1},
	} {
		w := newWorld(t)
		w.env = append(w.env, "TERM=xterm-256color", "TERMINFO="+terminfo)
		w.session("a")
		p, err := pty.New().Start(pty.Spec{
			Path: bin, Argv: []string{"imux-core", "-u", "-S", w.socket, "attach", "-t", "a"},
			Env: w.env, Dir: w.dir, Cols: 80, Rows: 24,
		})
		if err != nil {
			t.Fatal(err)
		}
		var out output
		go func() {
			buf := make([]byte, 4096)
			for {
				n, err := p.Read(buf)
				out.mu.Lock()
				out.b.Write(buf[:n])
				out.mu.Unlock()
				if err != nil {
					return
				}
			}
		}()
		w.eventually("the start sequence", func() bool { return strings.HasSuffix(out.String(), "\033]11;?\033\\") })
		w.must(tc.kill...)
		w.eventually(tc.msg, func() bool { return strings.HasSuffix(out.String(), tc.msg) })
		if !strings.Contains(out.String(), "\033[?1049l\033[23;0;0t\033[?2031l"+tc.msg) {
			t.Errorf("%v: no stop sequence before the message: %q", tc.kill, out.String())
		}
		if st, err := p.Wait(); err != nil || st.Code != tc.code {
			t.Errorf("%v: client exit %+v %v, want %d", tc.kill, st, err, tc.code)
		}
		_ = p.Close()
	}
}
