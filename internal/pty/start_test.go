//go:build darwin || linux

package pty

import (
	"bufio"
	"errors"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// lines reads the PTY master's output lines into a channel until it closes.
func lines(p *Proc) <-chan string {
	ch := make(chan string, 64)
	go func() {
		defer close(ch)
		sc := bufio.NewScanner(p.Master)
		for sc.Scan() {
			ch <- strings.TrimRight(sc.Text(), "\r")
		}
	}()
	return ch
}

func next(t *testing.T, ch <-chan string) string {
	t.Helper()
	select {
	case l, ok := <-ch:
		if !ok {
			t.Fatal("output ended")
		}
		return l
	case <-time.After(10 * time.Second):
		t.Fatal("no output within 10 s")
	}
	return ""
}

func start(t *testing.T, script string) *Proc {
	t.Helper()
	p, err := Start([]string{"/bin/sh", "-c", script}, []string{"PATH=/usr/bin:/bin", "TERM=xterm"}, "/", 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = p.KillGroup(syscall.SIGKILL)
		_, _ = p.Cmd.Process.Wait()
		p.Master.Close()
	})
	return p
}

// TestSessionAndControllingTerminal: the child leads its own session and process group, and
// has the PTY as its terminal (it can open /dev/tty, and stty sees the size).
func TestSessionAndControllingTerminal(t *testing.T) {
	p := start(t, `echo "$$ $(ps -o sess= -p $$) $(ps -o pgid= -p $$)"; stty size </dev/tty; exec sleep 100`)
	out := lines(p)
	f := strings.Fields(next(t, out))
	pid := strconv.Itoa(p.Cmd.Process.Pid)
	if len(f) != 3 || f[0] != pid || f[2] != pid {
		t.Fatalf("pid, session, pgid = %q; want the child's pid %s as its group", f, pid)
	}
	if got := next(t, out); got != "24 80" {
		t.Fatalf("stty size = %q, want 24 80", got)
	}
}

func TestExitCodeAndSignal(t *testing.T) {
	p := start(t, "exit 3")
	go io.Copy(io.Discard, p.Master)
	err := p.Cmd.Wait()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 3 {
		t.Fatalf("exit: %v, want code 3", err)
	}
	p = start(t, "kill -TERM $$")
	go io.Copy(io.Discard, p.Master)
	err = p.Cmd.Wait()
	if !errors.As(err, &ee) {
		t.Fatalf("exit: %v, want a signal", err)
	}
	ws := ee.Sys().(syscall.WaitStatus)
	if !ws.Signaled() || ws.Signal() != syscall.SIGTERM {
		t.Fatalf("status %v, want killed by SIGTERM", ws)
	}
}

// TestKillGroup: killing the group ends the child and what it started in the background.
func TestKillGroup(t *testing.T) {
	p := start(t, "trap '' HUP; sleep 100 & echo $!; sleep 100 & echo $!; wait")
	out := lines(p)
	var kids []int
	for range 2 {
		n, err := strconv.Atoi(next(t, out))
		if err != nil {
			t.Fatal(err)
		}
		kids = append(kids, n)
	}
	if err := p.KillGroup(syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	_ = p.Cmd.Wait()
	deadline := time.Now().Add(5 * time.Second)
	for _, k := range kids {
		for syscall.Kill(k, 0) == nil {
			if time.Now().After(deadline) {
				t.Fatalf("background child %d still runs", k)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}
}
