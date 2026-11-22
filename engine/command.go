package engine

import (
	"context"
	"io"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/cli"
)

// Command runs a command line (args as Main takes them, without the program's name) in the
// calling process, as a command client of a running server (ARCHITECTURE.md E18): its stdout,
// stderr and exit code are what the imux-core binary gives for the same line, without starting
// a process. It has no terminal, so it is never attached, and it never starts a server: handled
// is false, with nothing done, for a line the binary must run (the server itself, no command,
// or a command that starts a server, such as new-session). ctx bounds the command: its deadline
// is the connection's, and its end closes the connection.
func Command(ctx context.Context, args []string, stdout, stderr io.Writer) (code int, handled bool) {
	return cli.Command(ctx, args, stdout, stderr)
}
