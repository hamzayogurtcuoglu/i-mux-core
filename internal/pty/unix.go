//go:build darwin || linux

package pty

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// Unix starts programs on Unix PTYs (E11).
type Unix struct {
	// KillGrace is how long Kill waits after SIGHUP before SIGKILL; 0 means DefaultKillGrace.
	KillGrace time.Duration
}

// New returns this system's PTY implementation.
func New() PTY { return &Unix{} }

type unixProc struct {
	master    *os.File
	fd        int // master's descriptor; never master.Fd(), which would make it blocking
	cmd       *exec.Cmd
	grace     time.Duration
	done      chan struct{}
	status    ExitStatus
	waitErr   error
	closeOnce sync.Once
	closeErr  error
}

// Start implements PTY. The program leads a new session with the PTY as its controlling
// terminal, and so its own process group.
func (u *Unix) Start(spec Spec) (Proc, error) {
	if len(spec.Argv) == 0 {
		return nil, errors.New("pty: no program")
	}
	m, fd, s, err := open()
	if err != nil {
		return nil, err
	}
	defer s.Close()
	ws := &unix.Winsize{Col: uint16(spec.Cols), Row: uint16(spec.Rows)}
	if err := unix.IoctlSetWinsize(fd, unix.TIOCSWINSZ, ws); err != nil {
		m.Close()
		return nil, err
	}
	cmd := exec.Command(spec.Argv[0], spec.Argv[1:]...)
	if spec.Path != "" {
		cmd = exec.Command(spec.Path)
		cmd.Args = spec.Argv
	}
	cmd.Env, cmd.Dir = spec.Env, spec.Dir
	cmd.Stdin, cmd.Stdout, cmd.Stderr = s, s, s
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	if err := cmd.Start(); err != nil {
		m.Close()
		return nil, err
	}
	grace := u.KillGrace
	if grace <= 0 {
		grace = DefaultKillGrace
	}
	p := &unixProc{master: m, fd: fd, cmd: cmd, grace: grace, done: make(chan struct{})}
	go p.reap()
	return p, nil
}

// reap waits for the program, so it never stays a zombie, and records how it ended.
func (p *unixProc) reap() {
	err := p.cmd.Wait()
	var ee *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &ee):
		ws := ee.Sys().(syscall.WaitStatus)
		if ws.Signaled() {
			p.status = ExitStatus{Code: -1, Signal: int(ws.Signal())}
		} else {
			p.status = ExitStatus{Code: ws.ExitStatus()}
		}
	default:
		p.waitErr = err
	}
	close(p.done)
}

func (p *unixProc) Read(b []byte) (int, error) {
	n, err := p.master.Read(b)
	// Linux reports EIO once the program side has closed; it means the end of the output.
	if err != nil && errors.Is(err, syscall.EIO) {
		err = io.EOF
	}
	return n, err
}

func (p *unixProc) Write(b []byte) (int, error) { return p.master.Write(b) }

// SetReadDeadline makes a pending or later Read return os.ErrDeadlineExceeded at t (the zero
// time removes the deadline); the pane's reader uses it for tmux's ground timer.
func (p *unixProc) SetReadDeadline(t time.Time) error { return p.master.SetReadDeadline(t) }

func (p *unixProc) Resize(cols, rows int) error {
	return unix.IoctlSetWinsize(p.fd, unix.TIOCSWINSZ, &unix.Winsize{Col: uint16(cols), Row: uint16(rows)})
}

func (p *unixProc) Wait() (ExitStatus, error) {
	<-p.done
	return p.status, p.waitErr
}

func (p *unixProc) Pid() int { return p.cmd.Process.Pid }

func (p *unixProc) Fd() int { return p.fd }

// Kill hangs up the program's process group and the terminal's foreground group, then kills
// what is left of them after the grace period (K4).
func (p *unixProc) Kill() error {
	groups := []int{p.Pid()}
	if fg, err := unix.IoctlGetInt(p.Fd(), unix.TIOCGPGRP); err == nil && fg > 0 && fg != groups[0] {
		groups = append(groups, fg)
	}
	var first error
	for _, g := range groups {
		if err := syscall.Kill(-g, syscall.SIGHUP); err != nil && !errors.Is(err, syscall.ESRCH) && first == nil {
			first = err
		}
	}
	time.AfterFunc(p.grace, func() {
		for _, g := range groups {
			_ = syscall.Kill(-g, syscall.SIGKILL) // ESRCH: the group is gone already
		}
	})
	return first
}

func (p *unixProc) Close() error {
	p.closeOnce.Do(func() { p.closeErr = p.master.Close() })
	return p.closeErr
}
