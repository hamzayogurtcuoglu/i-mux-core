//go:build unix

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
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/commands"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/ipc"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/server"
)

// Server start on Unix (ARCHITECTURE.md §2, E1), after tmux 3.8's client_connect and
// server_start.

// readyEnv names the descriptor on which a starting server says it is ready.
const readyEnv = "IMUX_CORE_SERVER_READY"

// startTimeout is how long a client waits for the server it started.
var startTimeout = 5 * time.Second

func socketPath(name string) (string, error) {
	return ipc.SocketPath(os.Getenv("TMUX_TMPDIR"), os.Getuid(), name)
}

// connectError is a failed connection, printed as tmux prints it.
type connectError struct {
	path string
	err  error
}

func (e *connectError) Error() string {
	var errno syscall.Errno
	if errors.As(e.err, &errno) {
		if errno == syscall.ECONNREFUSED {
			return "no server running on " + e.path
		}
		return fmt.Sprintf("error connecting to %s (%s)", e.path, strerror(errno))
	}
	return fmt.Sprintf("error connecting to %s (%v)", e.path, e.err)
}

// strerror is C's text for an errno: Go's, with a capital letter.
func strerror(errno syscall.Errno) string {
	s := errno.Error()
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// connect connects to the server at path. When nothing listens there and the commands start a
// server, it starts one: under the lock file, so that one of several clients does it, after
// removing a socket left by a server that is gone.
func connect(path string, g globals, cmds [][]string) (net.Conn, error) {
	start := !g.noStart && commands.StartsServer(cmds)
	var lock *os.File
	locked := false
	release := func() {
		if lock != nil {
			_ = os.Remove(lock.Name())
			_ = lock.Close()
			lock = nil
		}
	}
	defer release()
	for {
		c, err := ipc.Dial(path)
		if err == nil {
			return c, nil
		}
		if !errors.Is(err, syscall.ECONNREFUSED) && !errors.Is(err, syscall.ENOENT) || !start {
			return nil, &connectError{path, err}
		}
		if !locked {
			l, waited := getLock(path + ".lock")
			if waited {
				// Another client held the lock: it has started the server, or failed to.
				continue
			}
			// Even with the lock, connect once more: another client may have started the
			// server and let the lock go between our connect and our lock.
			lock, locked = l, true
			continue
		}
		if lock != nil {
			if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return nil, &connectError{path, err}
			}
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

// getLock is tmux's client_get_lock: the lock file locked, or nil when it cannot be opened;
// waited when another client held it, which the caller takes as a reason to connect again.
func getLock(path string) (f *os.File, waited bool) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE, 0o600)
	if err != nil {
		return nil, false
	}
	fd := int(f.Fd())
	if err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		if !errors.Is(err, unix.EWOULDBLOCK) {
			return f, false
		}
		err = unix.EINTR
		for errors.Is(err, unix.EINTR) {
			err = unix.Flock(fd, unix.LOCK_EX)
		}
		_ = f.Close()
		return nil, true
	}
	return f, false
}

// startServer runs `imux-core __server`, detached from the terminal and the client, and waits
// for it to say it listens.
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
	null, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		_ = w.Close()
		return err
	}
	args := []string{"__server", "-S", path}
	if config != "" {
		args = append(args, "-f", config)
	}
	cmd := exec.Command(exe, args...)
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, readyEnv+"=") {
			cmd.Env = append(cmd.Env, kv)
		}
	}
	cmd.Env = append(cmd.Env, readyEnv+"=3")
	cmd.Stdin, cmd.Stdout, cmd.Stderr = null, null, null
	cmd.ExtraFiles = []*os.File{w}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	err = cmd.Start()
	_ = w.Close()
	_ = null.Close()
	if err != nil {
		return err
	}
	// Reaped if it ends while this client runs; it outlives a command client.
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

// runServer is `imux-core __server -S <socket> [-f conf]`.
func runServer(g globals, stderr io.Writer) int {
	// The client that started the server reads its first words on a pipe; run by hand, it
	// writes them on stderr.
	var readyFile *os.File
	ready := stderr
	if fd, err := strconv.Atoi(os.Getenv(readyEnv)); err == nil && fd > 2 {
		// Not to be inherited by the panes the config starts.
		syscall.CloseOnExec(fd)
		readyFile = os.NewFile(uintptr(fd), "ready")
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
	// tmux's server ignores these; SIGTERM ends it. They are caught and dropped rather than
	// ignored (signal.Ignore): an ignored signal stays ignored in the programs the server
	// starts, and tmux gives its panes the default handling.
	ignored := make(chan os.Signal, 8)
	signal.Notify(ignored, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTSTP, syscall.SIGTTIN,
		syscall.SIGTTOU, syscall.SIGQUIT)
	go func() {
		for range ignored {
		}
	}()
	term := make(chan os.Signal, 1)
	signal.Notify(term, syscall.SIGTERM)

	s := server.New(server.Options{SocketPath: path, Environ: os.Environ(), HoldForClient: true})
	go s.Run()
	if g.config != "" {
		_ = s.LoadConfig(g.config) // the errors are shown to the first attached client (C3)
	}
	l, err := ipc.Listen(path)
	if err != nil {
		s.Shutdown()
		var errno syscall.Errno
		if errors.As(err, &errno) {
			return fail(fmt.Sprintf("error creating %s (%s)", path, strerror(errno)))
		}
		return fail(fmt.Sprintf("error creating %s (%v)", path, err))
	}
	if readyFile != nil {
		fmt.Fprintln(readyFile, "ready")
		_ = readyFile.Close()
	}
	go s.Serve(l)
	select {
	case <-s.Done():
	case <-term:
		s.Shutdown()
	}
	s.WaitClients(2 * time.Second)
	return 0
}

// isTerminal reports whether f is a terminal (the null device is not).
func isTerminal(f *os.File) bool {
	_, err := unix.IoctlGetWinsize(int(f.Fd()), unix.TIOCGWINSZ)
	return err == nil
}
