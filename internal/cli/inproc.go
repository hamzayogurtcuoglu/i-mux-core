package cli

import (
	"context"
	"fmt"
	"io"
	"sync"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/cmdlang"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/commands"
)

// Command runs a command line (args as Main takes them) as a command client in this process
// (E18): the same Hello, command list and Result as the binary's client, but with no terminal,
// so it is never attached, and never starting a server. handled is false, with nothing done,
// for a line the binary must run: the server itself, no command (a new session), or a command
// that starts a server. ctx bounds the command: its deadline is the connection's, and its end
// closes the connection. A panic is reported on stderr with exit code 1.
func Command(ctx context.Context, args []string, stdout, stderr io.Writer) (code int, handled bool) {
	if len(args) > 0 && args[0] == "__server" {
		return 0, false
	}
	g, rest, code, done := parseGlobals(args, stdout, stderr)
	if done {
		return code, true
	}
	if len(rest) == 0 {
		return 0, false
	}
	cmds := cmdlang.SplitArgv(rest)
	if commands.StartsServer(cmds) {
		return 0, false
	}
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(stderr, "imux-core: %v\n", r)
			code, handled = 1, true
		}
	}()
	path, err := g.resolveSocket()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1, true
	}
	g.noStart = true
	c, err := connect(path, g, cmds)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1, true
	}
	var closeOnce sync.Once
	closeConn := func() { closeOnce.Do(func() { _ = c.Close() }) }
	defer closeConn()
	if dl, ok := ctx.Deadline(); ok {
		_ = c.SetDeadline(dl)
	}
	stop := context.AfterFunc(ctx, closeConn)
	defer stop()
	code, err = command(c, path, g, cmds, hello(g, false), stdout, stderr)
	if err != nil {
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		fmt.Fprintln(stderr, err)
		return 1, true
	}
	return code, true
}
