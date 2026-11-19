//go:build windows

package cli_test

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/ipc"
)

// Server start, reuse and lifetime on Windows (TASKS.md C8.4), with the real binary: the one
// IMUX_CORE_BIN names (built elsewhere, when Windows has no Go), else one built here. Every test
// has its own directory for the socket; nothing touches the owner's servers.

var bin string

func TestMain(m *testing.M) {
	bin = os.Getenv("IMUX_CORE_BIN")
	dir := ""
	if bin == "" {
		var err error
		if dir, err = os.MkdirTemp("", "imux-core-bin-"); err != nil {
			panic(err)
		}
		bin = filepath.Join(dir, "imux-core.exe")
		cmd := exec.Command("go", "build", "-o", bin, "../../cmd/imux-core")
		cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
		if err := cmd.Run(); err != nil {
			panic("building imux-core: " + err.Error())
		}
	}
	code := m.Run()
	if dir != "" {
		_ = os.RemoveAll(dir)
	}
	os.Exit(code)
}

type world struct {
	t      *testing.T
	dir    string
	socket string
	env    []string
}

func newWorld(t *testing.T) *world {
	t.Helper()
	dir := t.TempDir()
	env := []string{"TMUX_TMPDIR=" + dir, "USERPROFILE=" + dir, "HOME=" + dir, "LANG=C"}
	for _, k := range []string{"SystemRoot", "SystemDrive", "COMSPEC", "PATH", "PATHEXT", "LOCALAPPDATA", "TEMP", "TMP", "USERNAME"} {
		env = append(env, k+"="+os.Getenv(k))
	}
	w := &world{t: t, dir: dir, socket: filepath.Join(dir, "s"), env: env}
	t.Cleanup(func() {
		w.run("kill-server")
		w.waitNoServer()
	})
	return w
}

type result struct {
	out, err string
	code     int
}

func (w *world) run(args ...string) result {
	return w.runArgs(append([]string{"-S", w.socket, "-f", "NUL"}, args...)...)
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

func (w *world) must(args ...string) string {
	w.t.Helper()
	r := w.run(args...)
	if r.code != 0 {
		w.t.Fatalf("%q: exit %d: %s", args, r.code, r.err)
	}
	return r.out
}

func (w *world) eventually(what string, cond func() bool) {
	w.t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			w.t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func (w *world) listening() bool {
	c, err := ipc.Dial(w.socket)
	if err == nil {
		_ = c.Close()
		return true
	}
	return false
}

func (w *world) waitNoServer() {
	w.t.Helper()
	w.eventually("the server to exit", func() bool { return !w.listening() })
}

// session makes a detached session whose pane keeps running, and returns the server's pid.
func (w *world) session(name string) int {
	w.t.Helper()
	w.must("new-session", "-d", "-s", name, "ping.exe", "-n", "300", "127.0.0.1")
	pid, err := strconv.Atoi(strings.TrimSpace(w.must("display-message", "-p", "-t", name, "#{pid}")))
	if err != nil {
		w.t.Fatal(err)
	}
	return pid
}

func TestWindowsStartAndReuse(t *testing.T) {
	w := newWorld(t)
	if w.listening() {
		t.Fatal("a server before the first command")
	}
	a := w.session("a")
	if a == os.Getpid() {
		t.Error("the server is the test")
	}
	if b := w.session("b"); a != b {
		t.Errorf("the second client started another server: pids %d and %d", a, b)
	}
	w.must("has-session", "-t", "a")
	if got := w.must("list-sessions", "-F", "#{session_name}"); got != "a\nb\n" {
		t.Errorf("list-sessions: %q", got)
	}
	if got := strings.TrimSpace(w.must("display-message", "-p", "#{socket_path}")); got != w.socket {
		t.Errorf("socket_path %q, want %q", got, w.socket)
	}
}

func TestWindowsConcurrentStart(t *testing.T) {
	w := newWorld(t)
	const n = 6
	var wg sync.WaitGroup
	pids := make([]int, n)
	for i := range n {
		wg.Go(func() { pids[i] = w.session("s" + strconv.Itoa(i)) })
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

func TestWindowsExitWithLastSession(t *testing.T) {
	w := newWorld(t)
	w.session("a")
	w.session("b")
	w.must("kill-session", "-t", "a")
	if !w.listening() {
		t.Fatal("the server exited with a session left")
	}
	w.must("kill-session", "-t", "b")
	w.waitNoServer()
	r := w.run("has-session")
	if want := fmt.Sprintf("error connecting to %s (No such file or directory)\n", w.socket); r.err != want || r.code != 1 {
		t.Errorf("has-session: %q exit %d, want %q", r.err, r.code, want)
	}
	// A pane whose program ends ends its session, and so the server.
	w.must("new-session", "-d", "-s", "short", "cmd.exe", "/d", "/c", "exit 0")
	w.waitNoServer()
}

func TestWindowsNoServer(t *testing.T) {
	w := newWorld(t)
	want := fmt.Sprintf("error connecting to %s (No such file or directory)\n", w.socket)
	for _, cmd := range [][]string{{"has-session"}, {"kill-server"}, {"nosuch"}} {
		if r := w.run(cmd...); r.err != want || r.code != 1 {
			t.Errorf("%q: %q exit %d, want %q", cmd, r.err, r.code, want)
		}
	}
	if r := w.runArgs("-N", "-S", w.socket, "new-session", "-d"); r.code != 1 || w.listening() {
		t.Errorf("-N new-session: %+v", r)
	}
}

// -L names a socket under %LOCALAPPDATA%\i-mux-core, here TMUX_TMPDIR\i-mux-core.
func TestWindowsSocketName(t *testing.T) {
	w := newWorld(t)
	if r := w.runArgs("-L", "named", "-f", "NUL", "new-session", "-d", "-s", "x", "ping.exe", "-n", "300", "127.0.0.1"); r.code != 0 {
		t.Fatal(r.err)
	}
	defer w.runArgs("-L", "named", "kill-server")
	r := w.runArgs("-L", "named", "display-message", "-p", "#{socket_path}")
	if want := filepath.Join(w.dir, "i-mux-core", "named"); strings.TrimSpace(r.out) != want {
		t.Errorf("socket_path %q, want %q", r.out, want)
	}
}

func TestWindowsKillServer(t *testing.T) {
	w := newWorld(t)
	w.session("a")
	w.must("kill-server")
	w.waitNoServer()
}

// Shells on Windows (TASKS.md C8.7): run-shell and if-shell in cmd.exe /d /s /c, the argv form,
// and panes in PowerShell (or cmd.exe) when default-shell names none.
func TestWindowsShells(t *testing.T) {
	w := newWorld(t)
	w.session("a")
	sh := strings.ToLower(strings.TrimSpace(w.must("show-options", "-gv", "default-shell")))
	if !strings.HasSuffix(sh, `\pwsh.exe`) && !strings.HasSuffix(sh, `\powershell.exe`) && !strings.HasSuffix(sh, `\cmd.exe`) {
		t.Errorf("default-shell %q", sh)
	}
	for _, c := range []struct{ cmd, want string }{
		{"echo one&& echo two", "one\ntwo\n"},
		{`echo "a b"`, "\"a b\"\n"},
		{`["cmd.exe","/d","/c","echo","argv"]`, "argv\n"},
	} {
		if got := strings.ReplaceAll(w.must("run-shell", c.cmd), "\r", ""); got != c.want {
			t.Errorf("run-shell %q: %q, want %q", c.cmd, got, c.want)
		}
	}
	// A job knows its server, as on Unix: TMUX is the socket, the server's pid and the session.
	if got := strings.TrimSpace(w.must("run-shell", "echo %TMUX%")); !strings.HasPrefix(got, w.socket+",") {
		t.Errorf("TMUX in a job: %q", got)
	}
	w.must("if-shell", "exit 0", "set -g @yes 1", "set -g @yes 0")
	w.must("if-shell", "exit 3", "set -g @no 1", "set -g @no 0")
	if got := strings.TrimSpace(w.must("display-message", "-p", "#{@yes}#{@no}")); got != "10" {
		t.Errorf("if-shell: %q, want 10", got)
	}
	// One word: the default shell runs it.
	w.must("new-session", "-d", "-s", "p", `Write-Output ("pane-" + (40+2)); Start-Sleep 300`)
	if !strings.HasSuffix(sh, `\cmd.exe`) {
		w.eventually("the pane's output", func() bool {
			return strings.Contains(w.run("capture-pane", "-p", "-t", "p").out, "pane-42")
		})
	}
}

// Process information on Windows (TASKS.md C8.6): the command is the newest live process in the
// pane's job, without .exe; the path is the start directory until the program says another with
// OSC 9;9 (or OSC 7).
func TestWindowsPaneProcessInfo(t *testing.T) {
	w := newWorld(t)
	start := t.TempDir()
	w.must("new-session", "-d", "-s", "a", "-c", start, "powershell.exe", "-NoLogo", "-NoProfile", "-Command",
		"Start-Sleep 2; ping.exe -n 300 127.0.0.1")
	format := func(f string) string {
		return strings.TrimSpace(w.must("display-message", "-p", "-t", "a", f))
	}
	if got := format("#{pane_current_command}"); !strings.EqualFold(got, "powershell") {
		t.Errorf("before ping: %q, want powershell", got)
	}
	w.eventually("ping as the pane's command", func() bool {
		return strings.EqualFold(format("#{pane_current_command}"), "ping")
	})
	if got := format("#{pane_current_path}"); !strings.EqualFold(got, start) {
		t.Errorf("pane_current_path %q, want the start directory %q", got, start)
	}

	w.must("new-session", "-d", "-s", "b", "-c", start, "powershell.exe", "-NoLogo", "-NoProfile", "-Command",
		`[Console]::Write([char]27 + ']9;9;' + [char]34 + 'C:\Windows' + [char]34 + [char]7); Start-Sleep 300`)
	w.eventually("the OSC 9;9 path", func() bool {
		return strings.TrimSpace(w.must("display-message", "-p", "-t", "b", "#{pane_current_path}")) == `C:\Windows`
	})
}
