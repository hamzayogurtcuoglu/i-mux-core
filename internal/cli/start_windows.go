//go:build windows

package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/windows"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/commands"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/ipc"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/server"
)

// Server start on Windows (TASKS.md C8.4): as on Unix (start_unix.go), with the named pipe of
// ipc (E13), a lock file locked with LockFileEx, and a server started with no console.

// readyEnv names the inherited handle on which a starting server says it is ready.
const readyEnv = "IMUX_CORE_SERVER_READY"

// logEnv, when set, sends the server's own output (a crash, say) to a file in
// %LOCALAPPDATA%\i-mux-core\logs.
const logEnv = "IMUX_CORE_LOG"

// startTimeout is how long a client waits for the server it started.
var startTimeout = 5 * time.Second

func socketPath(name string) (string, error) {
	return ipc.SocketPath(os.Getenv("TMUX_TMPDIR"), 0, name)
}

// connectError is a failed connection, printed as tmux prints it.
type connectError struct {
	path string
	err  error
}

func (e *connectError) Error() string {
	// No pipe is what a missing socket is on Unix (a pipe goes with its server, so there is
	// never tmux's "no server running", a socket left with nothing listening).
	if errors.Is(e.err, fs.ErrNotExist) {
		return fmt.Sprintf("error connecting to %s (No such file or directory)", e.path)
	}
	var pe *os.PathError
	if errors.As(e.err, &pe) {
		return fmt.Sprintf("error connecting to %s (%v)", e.path, pe.Err)
	}
	return fmt.Sprintf("error connecting to %s (%v)", e.path, e.err)
}

// connect connects to the server at path. When none listens and the commands start a server,
// it starts one, under the lock file, so that one of several clients does it. A pipe goes with
// its server, so there is nothing stale to remove.
func connect(path string, g globals, cmds [][]string) (net.Conn, error) {
	start := !g.noStart && commands.StartsServer(cmds)
	var lock *os.File
	locked := false
	defer func() {
		if lock != nil {
			_ = lock.Close()
			_ = os.Remove(lock.Name())
		}
	}()
	for {
		c, err := ipc.Dial(path)
		if err == nil {
			return c, nil
		}
		if !errors.Is(err, fs.ErrNotExist) || !start {
			return nil, &connectError{path, err}
		}
		if !locked {
			l, waited := getLock(path + ".lock")
			if waited {
				// Another client held the lock: it has started the server, or failed to.
				continue
			}
			lock, locked = l, true
			continue
		}
		if err := startServer(path, g.config); err != nil {
			return nil, err
		}
		c, err = ipc.Dial(path)
		if err != nil {
			return nil, &connectError{path, err}
		}
		return c, nil
	}
}

// getLock is client_get_lock: the lock file locked, or nil when it cannot be opened; waited
// when another client held it.
func getLock(path string) (f *os.File, waited bool) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE, 0o600)
	if err != nil {
		return nil, false
	}
	h := windows.Handle(f.Fd())
	ol := new(windows.Overlapped)
	err = windows.LockFileEx(h, windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, ol)
	if err == nil {
		return f, false
	}
	if !errors.Is(err, windows.ERROR_LOCK_VIOLATION) {
		return f, false
	}
	// Wait for the other client, then connect again.
	_ = windows.LockFileEx(h, windows.LOCKFILE_EXCLUSIVE_LOCK, 0, 1, 0, new(windows.Overlapped))
	_ = f.Close()
	return nil, true
}

// startServer runs `imux-core __server`, with no console and in a process group of its own,
// and waits for it to say it listens.
func startServer(path, config string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	r, w, err := os.Pipe()
	if err != nil {
		return err
	}
	defer r.Close()
	args := []string{"__server", "-S", path}
	if config != "" {
		args = append(args, "-f", config)
	}
	start := func(flags uint32) (*exec.Cmd, error) {
		cmd := exec.Command(exe, args...)
		for _, kv := range os.Environ() {
			if !strings.HasPrefix(kv, readyEnv+"=") {
				cmd.Env = append(cmd.Env, kv)
			}
		}
		cmd.Env = append(cmd.Env, readyEnv+"="+strconv.FormatUint(uint64(w.Fd()), 10))
		if out := serverLog(); out != nil {
			defer out.Close()
			cmd.Stdout, cmd.Stderr = out, out
		}
		cmd.SysProcAttr = &syscall.SysProcAttr{
			CreationFlags:              flags,
			HideWindow:                 true,
			AdditionalInheritedHandles: []syscall.Handle{syscall.Handle(w.Fd())},
		}
		return cmd, cmd.Start()
	}
	// No console (and so no window), a process group of its own; out of the client's job when
	// the job allows it, so that the server outlives whatever started the client.
	const detached = windows.DETACHED_PROCESS | windows.CREATE_NEW_PROCESS_GROUP
	cmd, err := start(detached | windows.CREATE_BREAKAWAY_FROM_JOB)
	if errors.Is(err, windows.ERROR_ACCESS_DENIED) {
		cmd, err = start(detached)
	}
	_ = w.Close()
	if err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()

	_ = r.SetReadDeadline(time.Now().Add(startTimeout))
	line, err := bufio.NewReader(r).ReadString('\n')
	msg := strings.TrimSpace(line)
	switch {
	case msg == "ready":
		return nil
	case msg != "":
		return errors.New(msg)
	case errors.Is(err, os.ErrDeadlineExceeded):
		return fmt.Errorf("server did not start within %v", startTimeout)
	default:
		return errors.New("server exited unexpectedly")
	}
}

// serverLog opens the server's log file when IMUX_CORE_LOG is set, else nil.
func serverLog() *os.File {
	if os.Getenv(logEnv) == "" {
		return nil
	}
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		return nil
	}
	dir := filepath.Join(base, "i-mux-core", "logs")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil
	}
	name := "server-" + time.Now().Format("20060102-150405") + ".log"
	f, err := os.OpenFile(filepath.Join(dir, name), os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return nil
	}
	return f
}

// runServer is `imux-core __server -S <socket> [-f conf]`.
func runServer(g globals, stderr io.Writer) int {
	var readyFile *os.File
	ready := stderr
	if h, err := strconv.ParseUint(os.Getenv(readyEnv), 10, 64); err == nil && h != 0 {
		// Not to be inherited by the panes the config starts.
		_ = windows.SetHandleInformation(windows.Handle(h), windows.HANDLE_FLAG_INHERIT, 0)
		readyFile = os.NewFile(uintptr(h), "ready")
		defer readyFile.Close()
		ready = readyFile
	}
	_ = os.Unsetenv(readyEnv)
	fail := func(msg string) int {
		fmt.Fprintln(ready, msg)
		return 1
	}
	path := g.socketPath
	if path == "" {
		var err error
		if path, err = socketPath(g.socketName); err != nil {
			return fail(err.Error())
		}
	}
	s := server.New(server.Options{SocketPath: path, Environ: os.Environ(), HoldForClient: true})
	go s.Run()
	if g.config != "" {
		_ = s.LoadConfig(g.config) // the server keeps the errors for the first client
	}
	l, err := ipc.Listen(path)
	if err != nil {
		s.Shutdown()
		var pe *os.PathError
		if errors.As(err, &pe) {
			return fail(fmt.Sprintf("error creating %s (%v)", path, pe.Err))
		}
		return fail(fmt.Sprintf("error creating %s (%v)", path, err))
	}
	if readyFile != nil {
		fmt.Fprintln(readyFile, "ready")
		_ = readyFile.Close()
	}
	go s.Serve(l)
	<-s.Done()
	s.WaitClients(2 * time.Second)
	return 0
}

// isTerminal reports whether f is a console.
func isTerminal(f *os.File) bool {
	var mode uint32
	return windows.GetConsoleMode(windows.Handle(f.Fd()), &mode) == nil
}
