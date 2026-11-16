package server

import (
	"bytes"
	"errors"
	"os/exec"
	"syscall"
	"time"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/commands"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/pty"
)

// Jobs (TASKS.md C3.10): run-shell's and if-shell's commands, as tmux's job.c runs them:
// stdout (and with -E stderr) read whole, stderr otherwise dropped, the result handed back in
// the loop.

// RunJob implements commands.Host.
func (s *Server) RunJob(spec commands.JobSpec, done func(out []byte, status pty.ExitStatus, err error)) {
	var cmd *exec.Cmd
	if spec.Argv != nil {
		if len(spec.Argv) == 0 {
			done(nil, pty.ExitStatus{}, errors.New("empty command"))
			return
		}
		cmd = exec.Command(spec.Argv[0], spec.Argv[1:]...)
	} else {
		cmd = exec.Command(spec.Shell, "-c", spec.Command)
		cmd.Args[0] = shellArgv0(spec.Shell)
	}
	cmd.Dir = spec.Dir
	cmd.Env = spec.Env
	var out bytes.Buffer
	cmd.Stdout = &out
	if spec.ShowStderr {
		cmd.Stderr = &out
	}
	setJobAttrs(cmd)
	if err := cmd.Start(); err != nil {
		done(nil, pty.ExitStatus{}, err)
		return
	}
	go func() {
		err := cmd.Wait()
		st := pty.ExitStatus{}
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			if ws, ok := ee.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
				st = pty.ExitStatus{Code: -1, Signal: int(ws.Signal())}
			} else {
				st.Code = ee.ExitCode()
			}
		}
		s.post(func() { done(out.Bytes(), st, nil) })
	}()
}

// shellArgv0 is shell_argv0 for a command run with -c: the shell's own name.
func shellArgv0(shell string) string {
	for i := len(shell) - 1; i >= 0; i-- {
		if shell[i] == '/' {
			return shell[i+1:]
		}
	}
	return shell
}

// After implements commands.Host.
func (s *Server) After(d time.Duration, f func()) {
	time.AfterFunc(d, func() { s.post(f) })
}
