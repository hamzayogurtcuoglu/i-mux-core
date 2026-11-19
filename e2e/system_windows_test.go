//go:build e2e && windows

package e2e

import (
	"os"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/difftest"
)

// exeSuffix is a program file's suffix.
const exeSuffix = ".exe"

// suiteSkip: the scenarios' command lines are POSIX shell, run by Git for Windows' sh (or
// IMUX_E2E_SH's), which must be there.
func suiteSkip() string {
	if _, err := os.Stat(difftest.PosixShell()); err != nil {
		return "no sh for the scenarios' command lines: " + err.Error() + " (install Git for Windows, or set IMUX_E2E_SH)"
	}
	return ""
}

// noServer is what a client says when no server listens: a pipe goes with its server, so it is
// a missing endpoint (ARCHITECTURE.md §7).
const noServer = "(No such file or directory)"
