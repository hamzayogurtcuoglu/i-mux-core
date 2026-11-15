//go:build darwin || linux

package cli_test

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// Server start, lifetime and stale sockets (TASKS.md C2.3), with the real binary. Every test
// has its own directory for HOME, TMUX_TMPDIR and the socket; nothing touches the owner's
// servers.

var bin string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "imux-core-bin-")
	if err != nil {
		panic(err)
	}
	bin = filepath.Join(dir, "imux-core")
	cmd := exec.Command("go", "build", "-o", bin, "../../cmd/imux-core")
	cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
	if err := cmd.Run(); err != nil {
		panic("building imux-core: " + err.Error())
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// world is one test's directory and socket.
type world struct {
	t      *testing.T
	dir    string
	socket string
	env    []string
}

func newWorld(t *testing.T) *world {
	t.Helper()
	// Short, under /tmp: socket paths are limited to about 100 bytes.
	dir, err := os.MkdirTemp("/tmp", "imux-ct-")
	if err != nil {
		t.Fatal(err)
	}
	if d, err := filepath.EvalSymlinks(dir); err == nil {
		dir = d
	}
	w := &world{t: t, dir: dir, socket: filepath.Join(dir, "s"), env: []string{
		"HOME=" + dir, "TMUX_TMPDIR=" + dir, "PATH=/usr/bin:/bin", "SHELL=/bin/sh", "LANG=C",
	}}
	t.Cleanup(func() {
		w.run("kill-server")
		w.waitNoServer()
		os.RemoveAll(dir)
	})
	return w
}

type result struct {
	out, err string
	code     int
}

// run runs imux-core -S <socket> -f /dev/null args.
func (w *world) run(args ...string) result {
	return w.runArgs(append([]string{"-S", w.socket, "-f", os.DevNull}, args...)...)
}

func (w *world) runArgs(args ...string) result {
	cmd := exec.Command(bin, args...)
	cmd.Env, cmd.Dir = w.env, w.dir
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	r := result{out: out.String(), err: errb.String()}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		r.code = ee.ExitCode()
	} else if err != nil {
		w.t.Fatal(err)
	}
	return r
}

func (w *world) must(args ...string) {
	w.t.Helper()
	if r := w.run(args...); r.code != 0 {
		w.t.Fatalf("%q: exit %d: %s", args, r.code, r.err)
	}
}

// eventually polls cond for up to 10 s.
func (w *world) eventually(what string, cond func() bool) {
	w.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			w.t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// listening reports whether a server accepts connections on the socket.
func (w *world) listening() bool {
	c, err := net.Dial("unix", w.socket)
	if err == nil {
		c.Close()
		return true
	}
	return false
}

func (w *world) waitNoServer() {
	w.t.Helper()
	w.eventually("the server to exit", func() bool { return !w.listening() })
}

// session makes a detached session whose pane writes its $TMUX to a file, and returns the
// server's pid and socket from it.
func (w *world) session(name string) (pid int, socket string) {
	w.t.Helper()
	f := filepath.Join(w.dir, name+".tmux")
	w.must("new-session", "-d", "-s", name, fmt.Sprintf(`echo "$TMUX" > %s.tmp && mv %s.tmp %s; exec sleep 100`, f, f, f))
	var b []byte
	w.eventually("the pane's $TMUX", func() bool {
		var err error
		b, err = os.ReadFile(f)
		return err == nil
	})
	parts := strings.Split(strings.TrimSpace(string(b)), ",")
	if len(parts) != 3 {
		w.t.Fatalf("$TMUX = %q", b)
	}
	pid, err := strconv.Atoi(parts[1])
	if err != nil {
		w.t.Fatal(err)
	}
	return pid, parts[0]
}

func TestStartAndReuse(t *testing.T) {
	w := newWorld(t)
	if w.listening() {
		t.Fatal("a server before the first command")
	}
	pidA, sock := w.session("a")
	if sock != w.socket {
		t.Errorf("$TMUX socket %q, want %q", sock, w.socket)
	}
	if pidA == os.Getpid() {
		t.Error("the server is the test")
	}
	pidB, _ := w.session("b")
	if pidA != pidB {
		t.Errorf("the second client started another server: pids %d and %d", pidA, pidB)
	}
	w.must("has-session", "-t", "a")
	w.must("has-session", "-t", "b")
	// The server is detached: its own session, no terminal.
	if sid, err := syscall.Getpgid(pidA); err != nil || sid != pidA {
		t.Errorf("the server's process group is %d (%v), want its own", sid, err)
	}
}

func TestConcurrentStart(t *testing.T) {
	w := newWorld(t)
	const n = 8
	var wg sync.WaitGroup
	pids := make([]int, n)
	for i := range n {
		wg.Go(func() { pids[i], _ = w.session("s" + strconv.Itoa(i)) })
	}
	wg.Wait()
	for i, p := range pids {
		if p != pids[0] {
			t.Errorf("session s%d is on server %d, s0 on %d", i, p, pids[0])
		}
	}
	if _, err := os.Stat(w.socket + ".lock"); err == nil {
		t.Error("the lock file was left behind")
	}
}

func TestExitWithLastSession(t *testing.T) {
	w := newWorld(t)
	w.session("a")
	w.session("b")
	w.must("kill-session", "-t", "a")
	if !w.listening() {
		t.Fatal("the server exited with a session left")
	}
	w.must("kill-session", "-t", "b")
	w.waitNoServer()
	// Like tmux, the server leaves its socket: the next client says no server runs there.
	if _, err := os.Stat(w.socket); err != nil {
		t.Errorf("the socket is gone: %v", err)
	}
	r := w.run("has-session")
	if want := "no server running on " + w.socket + "\n"; r.err != want || r.code != 1 {
		t.Errorf("has-session: %q exit %d, want %q", r.err, r.code, want)
	}

	// A pane whose program ends ends its session, and so the server.
	w.must("new-session", "-d", "-s", "short", "exit 0")
	w.waitNoServer()
}

func TestStaleSocket(t *testing.T) {
	w := newWorld(t)
	l, err := net.ListenUnix("unix", &net.UnixAddr{Name: w.socket, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	l.SetUnlinkOnClose(false)
	l.Close()
	r := w.run("has-session")
	if want := "no server running on " + w.socket + "\n"; r.err != want || r.code != 1 {
		t.Errorf("has-session on a stale socket: %q exit %d, want %q", r.err, r.code, want)
	}
	w.session("a")
	w.must("has-session", "-t", "a")
}

func TestNoServer(t *testing.T) {
	w := newWorld(t)
	for _, cmd := range [][]string{{"has-session"}, {"kill-server"}, {"kill-session", "-t", "a"}, {"nosuch"}} {
		r := w.run(cmd...)
		want := "error connecting to " + w.socket + " (No such file or directory)\n"
		if r.err != want || r.code != 1 {
			t.Errorf("%q: %q exit %d, want %q", cmd, r.err, r.code, want)
		}
	}
	// -N never starts one.
	if r := w.runArgs("-N", "-S", w.socket, "new-session", "-d"); r.code != 1 || w.listening() {
		t.Errorf("-N new-session: %+v", r)
	}
	if _, err := os.Stat(w.socket); err == nil {
		t.Error("a socket was made")
	}
}

func TestSocketName(t *testing.T) {
	w := newWorld(t)
	r := w.runArgs("-L", "named", "-f", os.DevNull, "new-session", "-d", "-s", "x", "exec sleep 100")
	if r.code != 0 {
		t.Fatal(r.err)
	}
	sock := filepath.Join(w.dir, "imux-core-"+strconv.Itoa(os.Getuid()), "named")
	w.socket = sock // for the cleanup's kill-server
	if !w.listening() {
		t.Fatalf("no server at %s", sock)
	}
}

func TestConfigBeforeFirstCommand(t *testing.T) {
	w := newWorld(t)
	conf := filepath.Join(w.dir, "c.conf")
	if err := os.WriteFile(conf, []byte("bogus-command\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	// A config that does not parse runs nothing, and the server goes on.
	if r := w.runArgs("-S", w.socket, "-f", conf, "start-server"); r.code != 0 {
		t.Fatalf("start-server: %+v", r)
	}
	// With no session, the server ends when its client has gone.
	w.waitNoServer()

	text := "new-session -s fromconf 'exec sleep 100'\nhas-session -t nope\nnew-session -s second 'exec sleep 100'\n"
	if err := os.WriteFile(conf, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	if r := w.runArgs("-S", w.socket, "-f", conf, "has-session", "-t", "fromconf"); r.code == 0 {
		t.Fatal("has-session started a server")
	}
	// The config runs before the first command, and a failing line does not stop the next.
	// The panes it starts do not hold the client up (they once kept the server's start pipe
	// open, and the client waited out its timeout).
	start := time.Now()
	if r := w.runArgs("-S", w.socket, "-f", conf, "start-server", ";", "has-session", "-t", "fromconf"); r.code != 0 {
		t.Fatalf("start-server: %+v", r)
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Errorf("starting the server took %v", d)
	}
	w.must("has-session", "-t", "second")
}

func TestKillServerAndSignal(t *testing.T) {
	w := newWorld(t)
	w.session("a")
	if r := w.run("kill-server"); r.code != 0 || r.err != "" {
		t.Errorf("kill-server: %+v", r)
	}
	w.waitNoServer()

	pid, _ := w.session("b")
	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	w.waitNoServer()
	// SIGHUP, which a closing terminal sends, does not end it.
	pid, _ = w.session("c")
	if err := syscall.Kill(pid, syscall.SIGHUP); err != nil {
		t.Fatal(err)
	}
	w.must("has-session", "-t", "c")
}

// The signals the server ignores are not ignored by the programs in its panes, as tmux resets
// them in the child: an interrupt still ends a pane's program.
func TestPaneSignalsNotIgnored(t *testing.T) {
	w := newWorld(t)
	f := filepath.Join(w.dir, "pid")
	w.must("new-session", "-d", "-s", "a", fmt.Sprintf(`echo $$ > %s.tmp && mv %s.tmp %s; exec sleep 100`, f, f, f))
	var b []byte
	w.eventually("the pane's pid", func() bool {
		var err error
		b, err = os.ReadFile(f)
		return err == nil
	})
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Kill(pid, syscall.SIGINT); err != nil {
		t.Fatal(err)
	}
	// The program ends, then its session, then the server.
	w.waitNoServer()
}
