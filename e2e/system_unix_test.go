//go:build e2e && !windows

package e2e

// exeSuffix is a program file's suffix.
const exeSuffix = ""

// suiteSkip says why the suite cannot run here, or "".
func suiteSkip() string { return "" }

// noServer is what a client says when no server listens on its socket: on Unix a socket left
// with nothing listening.
const noServer = "no server running"
