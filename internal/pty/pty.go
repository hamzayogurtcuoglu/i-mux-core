package pty

import (
	"errors"
	"io"
	"time"
)

// Spec says what to run on a new PTY.
type Spec struct {
	Argv []string // the program and its arguments; Argv[0] is looked up in PATH
	Env  []string // the program's whole environment
	Dir  string   // its working directory
	Cols int
	Rows int
}

// ExitStatus is how a program ended: with an exit code, or (Unix) killed by a signal, as
// tmux's #{pane_dead_status} and #{pane_dead_signal} report it.
type ExitStatus struct {
	Code   int // the exit code; -1 when killed by a signal
	Signal int // the signal that killed it (Unix), else 0
}

// PTY starts programs on new pseudo terminals.
type PTY interface {
	Start(spec Spec) (Proc, error)
}

// Proc is a program running on a PTY. Reading gives its output; writing gives it input.
type Proc interface {
	io.ReadWriter
	// Resize sets the PTY's size; the program gets SIGWINCH (Unix).
	Resize(cols, rows int) error
	// Wait waits for the program to end and returns how it ended. It may be called any number
	// of times, from any goroutine.
	Wait() (ExitStatus, error)
	// Kill ends the program and what it started (COMPATIBILITY.md K4). It does not wait.
	Kill() error
	// Pid is the program's process id.
	Pid() int
	// Fd is the PTY master's descriptor, for platform.ForegroundCommand and CurrentPath.
	Fd() int
	// Close releases the PTY. A Read blocked in another goroutine returns.
	Close() error
}

// DefaultKillGrace is how long Kill waits after SIGHUP before it sends SIGKILL.
const DefaultKillGrace = 2 * time.Second

// ErrUnsupported is returned where this system has no PTY implementation yet.
var ErrUnsupported = errors.New("pty: not supported on this system yet")
