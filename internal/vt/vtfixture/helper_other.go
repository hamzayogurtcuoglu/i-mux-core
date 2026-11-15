//go:build !darwin && !linux

package vtfixture

import (
	"fmt"
	"os"
)

// Helper needs a Unix terminal: tmux does not run elsewhere.
func Helper([]string) int {
	fmt.Fprintln(os.Stderr, "vtfixture: needs tmux, which needs Unix")
	return 1
}
