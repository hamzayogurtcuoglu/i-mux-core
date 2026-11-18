package engine

import (
	"io"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/cli"
)

// Main runs imux-core with args (without the program's name), as this repository's
// cmd/imux-core does, and returns the exit code.
func Main(args []string, stdout, stderr io.Writer) int {
	return cli.Main(args, stdout, stderr)
}
