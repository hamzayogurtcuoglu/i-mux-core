//go:build unix

package main

import (
	"os"
	"syscall"
)

// execTmux replaces this process with the real tmux, keeping its pid, stdio and terminal.
func execTmux(bin string, args []string) error {
	return syscall.Exec(bin, append([]string{"tmux"}, args...), os.Environ())
}
