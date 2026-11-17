//go:build darwin || linux

package pty

import (
	"bufio"
	"io"
	"os"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

var testEnv = []string{"PATH=/usr/bin:/bin", "TERM=xterm", "LANG=C"}

func startSh(t *testing.T, u *Unix, script string) Proc {
	t.Helper()
	p, err := u.Start(Spec{Argv: []string{"/bin/sh", "-c", script}, Env: testEnv, Dir: "/", Cols: 80, Rows: 24})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = syscall.Kill(-p.Pid(), syscall.SIGKILL)
		_, _ = p.Wait()
		_ = p.Close()
	})
	return p
}

// lines reads the program's output lines into a channel until the output ends.
func lines(p Proc) <-chan string {
	ch := make(chan string, 64)
	go func() {
		defer close(ch)
		sc := bufio.NewScanner(p)
		for sc.Scan() {
			ch <- strings.TrimRight(sc.Text(), "\r")
		}
	}()
	return ch
}

// expect reads lines until one is want; the PTY echoes input, so other lines come too.
func expect(t *testing.T, ch <-chan string, want string) {
	t.Helper()
	timeout := time.After(10 * time.Second)
	for {
		select {
		case l, ok := <-ch:
			if !ok {
				t.Fatalf("output ended before %q", want)
			}
			if l == want {
				return
			}
		case <-timeout:
			t.Fatalf("no %q within 10 s", want)
		}
	}
}

func TestOutputAndInput(t *testing.T) {
	p := startSh(t, &Unix{}, `echo ready; read l; echo "got:$l"`)
	out := lines(p)
	expect(t, out, "ready")
	if _, err := p.Write([]byte("hello\n")); err != nil {
		t.Fatal(err)
	}
	expect(t, out, "got:hello")
	if st, err := p.Wait(); err != nil || st != (ExitStatus{Code: 0}) {
		t.Fatalf("status %+v, %v", st, err)
	}
}

func TestResize(t *testing.T) {
	p := startSh(t, &Unix{}, `stty size; read l; stty size`)
	out := lines(p)
	expect(t, out, "24 80")
	if err := p.Resize(132, 50); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Write([]byte("\n")); err != nil {
		t.Fatal(err)
	}
	expect(t, out, "50 132")
}

// TestResizeSignal: the program gets SIGWINCH.
func TestResizeSignal(t *testing.T) {
	p := startSh(t, &Unix{}, `trap 'echo winch' WINCH; echo ready; while :; do read l || exit; done`)
	out := lines(p)
	expect(t, out, "ready")
	if err := p.Resize(100, 30); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Write([]byte("\n")); err != nil { // wake the read, so the trap runs
		t.Fatal(err)
	}
	expect(t, out, "winch")
}

func TestExitCodeAndSignal(t *testing.T) {
	u := &Unix{}
	p := startSh(t, u, "exit 3")
	go io.Copy(io.Discard, p)
	if st, err := p.Wait(); err != nil || st != (ExitStatus{Code: 3}) {
		t.Fatalf("status %+v, %v; want code 3", st, err)
	}
	// Wait may be called again.
	if st, _ := p.Wait(); st.Code != 3 {
		t.Fatalf("second Wait: %+v", st)
	}
	p = startSh(t, u, "kill -TERM $$")
	go io.Copy(io.Discard, p)
	if st, err := p.Wait(); err != nil || st != (ExitStatus{Code: -1, Signal: int(syscall.SIGTERM)}) {
		t.Fatalf("status %+v, %v; want SIGTERM", st, err)
	}
}

// gone waits until no process of group g is left.
func gone(t *testing.T, g int, within time.Duration) {
	t.Helper()
	deadline := time.Now().Add(within)
	for syscall.Kill(-g, 0) == nil {
		if time.Now().After(deadline) {
			t.Fatalf("process group %d still has processes", g)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestKill: SIGHUP ends a program that keeps the default; one that ignores SIGHUP, and what it
// started, is killed after the grace period (K4).
func TestKill(t *testing.T) {
	u := &Unix{KillGrace: 200 * time.Millisecond}
	p := startSh(t, u, "echo ready; sleep 100")
	out := lines(p)
	expect(t, out, "ready")
	start := time.Now()
	if err := p.Kill(); err != nil {
		t.Fatal(err)
	}
	if st, _ := p.Wait(); st.Signal != int(syscall.SIGHUP) {
		t.Fatalf("status %+v, want SIGHUP", st)
	}
	if time.Since(start) > 150*time.Millisecond {
		t.Errorf("SIGHUP took %v: it should not wait for the grace period", time.Since(start))
	}

	p = startSh(t, u, `trap '' HUP; sleep 100 & echo "kid $!"; sleep 100`)
	out = lines(p)
	var kid int
	timeout := time.After(10 * time.Second)
	for kid == 0 {
		select {
		case l := <-out:
			if v, ok := strings.CutPrefix(l, "kid "); ok {
				kid, _ = strconv.Atoi(v)
			}
		case <-timeout:
			t.Fatal("no kid")
		}
	}
	if err := p.Kill(); err != nil {
		t.Fatal(err)
	}
	if st, _ := p.Wait(); st.Signal != int(syscall.SIGKILL) {
		t.Fatalf("status %+v, want SIGKILL", st)
	}
	gone(t, p.Pid(), 5*time.Second)
	// Killed, the child is reaped by init (launchd) once its shell is gone, a moment later on
	// macOS: until then it still answers kill(pid, 0).
	deadline := time.Now().Add(5 * time.Second)
	for syscall.Kill(kid, 0) == nil {
		if time.Now().After(deadline) {
			t.Fatalf("background child %d survived", kid)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestCloseUnblocksRead: Close releases a Read waiting in another goroutine.
func TestCloseUnblocksRead(t *testing.T) {
	p := startSh(t, &Unix{}, "sleep 100")
	done := make(chan error, 1)
	go func() {
		_, err := p.Read(make([]byte, 64))
		done <- err
	}()
	time.Sleep(50 * time.Millisecond) // let the Read block
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Read still blocked after Close")
	}
}

// openFDs counts this process's open descriptors.
func openFDs(t *testing.T) int {
	t.Helper()
	dir := "/dev/fd"
	if runtime.GOOS == "linux" {
		dir = "/proc/self/fd"
	}
	f, err := os.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	names, err := f.Readdirnames(-1)
	if err != nil {
		t.Fatal(err)
	}
	return len(names) - 1 // the directory itself
}

// TestNoLeaks: after programs run and their PTYs close, the descriptor and goroutine counts
// are back where they started.
func TestNoLeaks(t *testing.T) {
	u := &Unix{KillGrace: 50 * time.Millisecond}
	run := func() {
		p, err := u.Start(Spec{Argv: []string{"/bin/sh", "-c", "echo hi"}, Env: testEnv, Dir: "/", Cols: 80, Rows: 24})
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, p)
		_, _ = p.Wait()
		_ = p.Close()
		q, err := u.Start(Spec{Argv: []string{"/bin/sh", "-c", "sleep 100"}, Env: testEnv, Dir: "/", Cols: 80, Rows: 24})
		if err != nil {
			t.Fatal(err)
		}
		_ = q.Kill()
		_, _ = q.Wait()
		_ = q.Close()
	}
	run() // warm up: the runtime's own descriptors and goroutines
	time.Sleep(100 * time.Millisecond)
	fds, goroutines := openFDs(t), runtime.NumGoroutine()
	for range 10 {
		run()
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		f, g := openFDs(t), runtime.NumGoroutine()
		if f <= fds && g <= goroutines {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("descriptors %d → %d, goroutines %d → %d", fds, f, goroutines, g)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestStartErrors(t *testing.T) {
	u := &Unix{}
	if _, err := u.Start(Spec{}); err == nil {
		t.Error("no program: no error")
	}
	if _, err := u.Start(Spec{Argv: []string{"/nonexistent/program"}, Env: testEnv, Dir: "/", Cols: 80, Rows: 24}); err == nil {
		t.Error("missing program: no error")
	}
	if _, err := u.Start(Spec{Argv: []string{"/bin/sh"}, Env: testEnv, Dir: "/nonexistent", Cols: 80, Rows: 24}); err == nil {
		t.Error("missing directory: no error")
	}
}
