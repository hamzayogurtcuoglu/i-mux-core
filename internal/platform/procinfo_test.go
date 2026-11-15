//go:build darwin || linux

package platform_test

import (
	"io"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/platform"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/pty"
)

func startSh(t *testing.T, script string) pty.Proc {
	t.Helper()
	p, err := pty.New().Start(pty.Spec{Argv: []string{"/bin/sh", "-c", script}, Env: []string{"PATH=/usr/bin:/bin"}, Dir: "/", Cols: 80, Rows: 24})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = syscall.Kill(-p.Pid(), syscall.SIGKILL)
		_, _ = p.Wait()
		_ = p.Close()
	})
	go io.Copy(io.Discard, p)
	return p
}

// waitFor polls the PTY's foreground command and path until they are cmd and path.
func waitFor(t *testing.T, p pty.Proc, cmd, path string) {
	t.Helper()
	fd := p.Fd()
	var gotCmd, gotPath string
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		gotCmd, gotPath = platform.ForegroundCommand(fd), platform.CurrentPath(fd)
		if gotCmd == cmd && gotPath == path {
			return
		}
	}
	t.Fatalf("foreground command %q, path %q; want %q, %q", gotCmd, gotPath, cmd, path)
}

// TestForegroundCommandAndPath is decision E11's evidence: a pane running
// sh -c 'cd /tmp && exec sleep 5' reports sleep and /tmp, resolved through symlinks.
func TestForegroundCommandAndPath(t *testing.T) {
	tmp, err := filepath.EvalSymlinks("/tmp")
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, startSh(t, "cd /tmp && exec sleep 5"), "sleep", tmp)
}

// TestForegroundIsTheGroupLeader: without exec, sleep runs in the shell's process group and
// the group's leader is the shell, so, as in tmux, the shell is reported (bash for macOS's
// /bin/sh, /bin/sh itself on Linux) with the directory it changed to.
func TestForegroundIsTheGroupLeader(t *testing.T) {
	tmp, err := filepath.EvalSymlinks("/tmp")
	if err != nil {
		t.Fatal(err)
	}
	p := startSh(t, "cd /tmp && sleep 5")
	fd := p.Fd()
	// Once the directory has changed, sleep has started (or is about to).
	for deadline := time.Now().Add(5 * time.Second); platform.CurrentPath(fd) != tmp; time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("path %q, want %q", platform.CurrentPath(fd), tmp)
		}
	}
	time.Sleep(100 * time.Millisecond) // let sleep start, so a wrong answer would be "sleep"
	if shell := platform.ForegroundCommand(fd); shell == "sleep" || shell == "" {
		t.Fatalf("foreground command %q, want the shell", shell)
	}
}
