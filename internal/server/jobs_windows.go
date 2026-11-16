//go:build windows

package server

import "os/exec"

// setJobAttrs: jobs on Windows come with task C8.7.
func setJobAttrs(*exec.Cmd) {}
