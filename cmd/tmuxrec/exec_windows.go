//go:build windows

package main

import "errors"

// execTmux is not available on Windows, which has no tmux.
func execTmux(string, []string) error {
	return errors.New("tmux does not run on Windows")
}
