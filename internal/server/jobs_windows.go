//go:build windows

package server

import (
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

// setJobAttrs starts a job with no console window: the server has no console (it runs
// detached), so a console program would otherwise open one of its own.
func setJobAttrs(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.HideWindow = true
	cmd.SysProcAttr.CreationFlags |= windows.CREATE_NO_WINDOW
}

// setCmdLine gives the job its command line as it is (cmd.exe /d /s /c "…"), when there is one.
func setCmdLine(cmd *exec.Cmd, line string) {
	if line == "" {
		return
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CmdLine = line
}
