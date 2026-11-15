//go:build darwin || linux

package server

import (
	"fmt"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/model"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/pty"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/vt"
)

var env = []string{"PATH=/usr/bin:/bin", "TERM=tmux-256color", "LANG=C"}

func sh(script string) pty.Spec {
	return pty.Spec{Argv: []string{"/bin/sh", "-c", script}, Env: env, Dir: "/"}
}

func startServer(t *testing.T, o Options) *Server {
	t.Helper()
	if o.PTY == nil {
		o.PTY = &pty.Unix{KillGrace: 200 * time.Millisecond}
	}
	s := New(o)
	go s.Run()
	t.Cleanup(s.Shutdown)
	return s
}

// newPane makes a session with one window and one pane running spec.
func newPane(t *testing.T, s *Server, name string, spec pty.Spec) *model.Pane {
	t.Helper()
	var p *model.Pane
	var err error
	if derr := s.Do(func(tr *model.Tree) {
		var sess *model.Session
		if sess, err = tr.NewSession(name, "/", time.Now()); err != nil {
			return
		}
		var wl *model.Winlink
		if wl, err = tr.NewWindow(sess, -1, 0, 80, 24); err != nil {
			return
		}
		sess.SelectWindow(wl)
		p = tr.NewPane(wl.Window, nil, false, 80, 24)
		err = s.Spawn(p, spec)
	}); derr != nil {
		t.Fatal(derr)
	}
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func screen(p *model.Pane) string {
	p.Mu.Lock()
	defer p.Mu.Unlock()
	return p.Term.Screen.Capture(vt.CaptureOptions{})
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestOutputReachesTheScreen(t *testing.T) {
	changed := make(chan int, 100)
	s := startServer(t, Options{PaneChanged: func(p *model.Pane) {
		select {
		case changed <- p.ID:
		default:
		}
	}})
	p := newPane(t, s, "a", sh("printf 'hello\\n'; exec sleep 100"))
	waitFor(t, "hello", func() bool { return strings.Contains(screen(p), "hello") })
	select {
	case id := <-changed:
		if id != p.ID {
			t.Fatalf("changed pane %%%d", id)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no change notice")
	}
}

// TestPaneExitEndsServer: a pane whose program ends goes, then its window and session, and
// with no session left the server ends (exit-empty).
func TestPaneExitEndsServer(t *testing.T) {
	s := startServer(t, Options{})
	p := newPane(t, s, "a", sh("exit 3"))
	select {
	case <-s.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("the server did not end with its last session")
	}
	if !p.Dead || p.Status.Code != 3 {
		t.Fatalf("pane dead %v, status %+v", p.Dead, p.Status)
	}
	if err := s.Do(func(*model.Tree) {}); err != ErrStopped {
		t.Fatalf("Do after the end: %v", err)
	}
}

// TestOnePaneExitKeepsOthers: other sessions keep the server going.
func TestOnePaneExitKeepsOthers(t *testing.T) {
	s := startServer(t, Options{})
	newPane(t, s, "keep", sh("exec sleep 100"))
	gone := newPane(t, s, "gone", sh("exit 0"))
	waitFor(t, "the pane to go", func() bool {
		var left bool
		_ = s.Do(func(tr *model.Tree) { left = tr.Pane(gone.ID) == nil && tr.SessionByName("gone") == nil })
		return left
	})
	select {
	case <-s.Done():
		t.Fatal("the server ended with a session left")
	default:
	}
}

// TestRace20Panes: 20 panes stream while commands run on the tree (run with -race).
func TestRace20Panes(t *testing.T) {
	s := startServer(t, Options{})
	var panes []*model.Pane
	for i := 0; i < 20; i++ {
		panes = append(panes, newPane(t, s, fmt.Sprintf("s%d", i), sh("seq 1 20000; exec sleep 100")))
	}
	var wg sync.WaitGroup
	for g := 0; g < 4; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				_ = s.Do(func(tr *model.Tree) {
					for _, p := range tr.Panes() {
						if p.Term == nil {
							continue // a pane the commands added, with no program
						}
						p.Mu.Lock()
						_ = p.Term.Screen.Cy
						p.Mu.Unlock()
					}
					sess := tr.SessionByName(fmt.Sprintf("s%d", (g*50+i)%20))
					if wl, err := tr.NewWindow(sess, -1, 0, 80, 24); err == nil {
						tr.NewPane(wl.Window, nil, false, 80, 24)
						sess.SelectWindow(wl)
					}
				})
			}
		}(g)
	}
	wg.Wait()
	for _, p := range panes {
		waitFor(t, "seq's last line", func() bool { return strings.Contains(screen(p), "20000") })
	}
}

// TestShutdownNoLeaks: after Shutdown the readers are gone and the goroutine count is back.
func TestShutdownNoLeaks(t *testing.T) {
	before := runtime.NumGoroutine()
	s := New(Options{PTY: &pty.Unix{KillGrace: 100 * time.Millisecond}})
	go s.Run()
	for i := 0; i < 5; i++ {
		newPane(t, s, fmt.Sprintf("s%d", i), sh("exec sleep 100"))
	}
	s.Shutdown()
	waitFor(t, "the goroutines to end", func() bool { return runtime.NumGoroutine() <= before+1 })
}

// TestGroundTimer: an unterminated string is given up on after a quiet spell, so what follows
// shows, as with tmux's ground timer.
func TestGroundTimer(t *testing.T) {
	old := vt.GroundTimeout
	vt.GroundTimeout = 200 * time.Millisecond
	t.Cleanup(func() { vt.GroundTimeout = old })
	s := startServer(t, Options{})
	p := newPane(t, s, "a", sh("printf '\\033]2;never ends'; sleep 1; printf 'visible'; exec sleep 100"))
	waitFor(t, "text after the dropped string", func() bool { return strings.Contains(screen(p), "visible") })
}
