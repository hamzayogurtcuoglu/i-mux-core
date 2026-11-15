// Command imux-core is the terminal engine i-mux runs on: one binary that is the command
// client, the attached client and the server (ARCHITECTURE.md §2).
package main

import (
	"os"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/cli"
)

func main() {
	os.Exit(cli.Main(os.Args[1:], os.Stdout, os.Stderr))
}
