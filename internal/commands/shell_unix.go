//go:build !windows

package commands

import (
	"errors"
	"os"
	"path/filepath"
)

// The shells of tmux's spawn.c and job.c on Unix.

// JobShell is _PATH_BSHELL: run-shell and if-shell run their commands with it.
func JobShell() string { return "/bin/sh" }

// DefaultPath is the path when a command-line client and its session give no
// PATH (paneEnv): tmux's Unix search path.
func DefaultPath() string { return "/usr/bin:/bin" }

// DefaultShell is the shell when default-shell and SHELL give none that can run.
func DefaultShell() string { return "/bin/sh" }

// ShellCommand is how shell sh runs one command line: its name, -c and the line. Unix has no
// separate command line.
func ShellCommand(sh, cmd string) (argv []string, cmdline string) {
	return []string{filepath.Base(sh), "-c", cmd}, ""
}

// loginArgv starts sh as a login shell: its name after a dash.
func loginArgv(sh string) []string { return []string{"-" + filepath.Base(sh)} }

// isExecutable is tmux's access(X_OK) check on the shell.
func isExecutable(p string) error {
	fi, err := os.Stat(p)
	if err != nil {
		return err
	}
	if fi.IsDir() || fi.Mode().Perm()&0o111 == 0 {
		return errors.New("not executable")
	}
	return nil
}
