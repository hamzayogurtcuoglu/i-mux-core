//go:build darwin || linux

package pty

import (
	"os"
	"os/exec"
	"syscall"

	"golang.org/x/sys/unix"
)

// Proc is a program running on a PTY. This is the spike's form (C0.6); task C1.1 turns it
// into the PTY and Proc interfaces of ARCHITECTURE.md §6.
type Proc struct {
	Master *os.File
	Cmd    *exec.Cmd
}

// Start runs argv on a new PTY of cols×rows, in dir with env. The child is a session leader
// with the PTY as its controlling terminal, so it leads its own process group.
func Start(argv, env []string, dir string, cols, rows int) (*Proc, error) {
	m, s, err := open()
	if err != nil {
		return nil, err
	}
	defer s.Close()
	if err := unix.IoctlSetWinsize(int(m.Fd()), unix.TIOCSWINSZ, &unix.Winsize{Col: uint16(cols), Row: uint16(rows)}); err != nil {
		m.Close()
		return nil, err
	}
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Env, cmd.Dir = env, dir
	cmd.Stdin, cmd.Stdout, cmd.Stderr = s, s, s
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	if err := cmd.Start(); err != nil {
		m.Close()
		return nil, err
	}
	return &Proc{Master: m, Cmd: cmd}, nil
}

// KillGroup sends sig to the child's whole process group.
func (p *Proc) KillGroup(sig syscall.Signal) error {
	return syscall.Kill(-p.Cmd.Process.Pid, sig)
}
