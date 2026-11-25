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

// Terminal is the terminal an Attach client says it is: its name (#{client_name}) and size.
type Terminal = cli.StreamTerminal

// Client is an attached client whose terminal is a stream (Attach).
type Client = cli.StreamClient

// ErrNotAttached means the line ran but attached nothing.
var ErrNotAttached = cli.ErrNotAttached

// Attach runs an attach line (args as Main takes them, such as "-L", "imux", "attach-session",
// "-t", "work") as an attached client of a running server inside the calling process, with a
// stream for its terminal (ARCHITECTURE.md E19): the server's terminal output (for an
// xterm-256color terminal of term's size) is written to out, and the returned client takes the
// terminal's input (keys, pastes, mouse reports) and sizes. It starts no server and needs no
// console, on every system. It returns once the server has attached the client.
func Attach(ctx context.Context, args []string, term Terminal, out io.Writer) (*Client, error) {
	return cli.Attach(ctx, args, term, out)
}
