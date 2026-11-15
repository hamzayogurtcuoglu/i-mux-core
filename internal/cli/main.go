package cli

import (
	"fmt"
	"io"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/version"
)

// Main runs imux-core with args (without the program name) and returns the exit code.
func Main(args []string, stdout, stderr io.Writer) int {
	for _, a := range args {
		if a == "-V" {
			fmt.Fprintln(stdout, version.Line())
			return 0
		}
	}
	fmt.Fprintln(stderr, "imux-core: not implemented yet")
	return 1
}
