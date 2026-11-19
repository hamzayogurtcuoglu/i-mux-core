//go:build !windows

package server

import (
	"os/exec"
	"syscall"
)

// setJobAttrs puts a job in its own process group, as tmux's forked child is not the
// server's.
func setJobAttrs(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} }

// setCmdLine: Unix programs get their arguments, never a command line.
func setCmdLine(*exec.Cmd, string) {}
