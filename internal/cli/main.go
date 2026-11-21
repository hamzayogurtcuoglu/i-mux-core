package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/cmdlang"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/version"
)

// globals are imux-core's own flags, before the command.
type globals struct {
	socketName string // -L
	socketPath string // -S
	config     string // -f
	utf8       bool   // -u
	noStart    bool   // -N: never start a server
}

// optstring is tmux 3.8's getopt string. Flags i-mux-core does not support are refused.
const optstring = "2c:CDdf:lL:NqS:T:uUvV"

const usage = "usage: imux-core [-2CDlNuVv] [-c shell-command] [-f file] [-L socket-name]\n" +
	"                 [-S socket-path] [-T features] [command [flags]]\n"

// Main runs imux-core with args (without the program name) and returns the exit code.
func Main(args []string, stdout, stderr io.Writer) int {
	server := len(args) > 0 && args[0] == "__server"
	if server {
		args = args[1:]
	}
	g, rest, code, done := parseGlobals(args, stdout, stderr)
	if done {
		return code
	}
	if server {
		// At most 2 threads at once unless the environment says (E17): more let the server
		// contend for a small machine's CPUs with the programs it serves.
		if os.Getenv("GOMAXPROCS") == "" {
			runtime.GOMAXPROCS(min(2, runtime.NumCPU()))
		}
		return runServer(g, stderr)
	}
	path, err := g.resolveSocket()
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	cmds := cmdlang.SplitArgv(rest)
	if len(rest) == 0 {
		cmds = [][]string{{"new-session"}}
	}
	return runClient(g, path, cmds, stdout, stderr)
}

// parseGlobals reads the flags as tmux's getopt does: grouped flags, a value attached or in
// the next word, until the first word that is not a flag or "--". done is set when imux-core
// should exit at once with code.
func parseGlobals(args []string, stdout, stderr io.Writer) (g globals, rest []string, code int, done bool) {
	i := 0
	for i < len(args) {
		a := args[i]
		if len(a) < 2 || a[0] != '-' {
			break
		}
		i++
		if a == "--" {
			break
		}
		for j := 1; j < len(a); j++ {
			c := a[j]
			at := strings.IndexByte(optstring, c)
			if c == ':' || at < 0 {
				fmt.Fprintf(stderr, "imux-core: unknown option -- %c\n%s", c, usage)
				return g, nil, 1, true
			}
			var val string
			if at+1 < len(optstring) && optstring[at+1] == ':' {
				switch {
				case j+1 < len(a):
					val = a[j+1:]
				case i < len(args):
					val = args[i]
					i++
				default:
					fmt.Fprintf(stderr, "imux-core: option requires an argument -- %c\n%s", c, usage)
					return g, nil, 1, true
				}
				j = len(a)
			}
			switch c {
			case 'V':
				fmt.Fprintln(stdout, version.Line())
				return g, nil, 0, true
			case 'L':
				g.socketName = val
			case 'S':
				g.socketPath = val
			case 'f':
				g.config = val
			case 'u':
				g.utf8 = true
			case 'N':
				g.noStart = true
			case '2', 'l', 'q', 'v':
				// 256 colours are assumed, -l and -q change nothing here, and there is no log.
			default:
				fmt.Fprintf(stderr, "imux-core: -%c is not supported\n", c)
				return g, nil, 1, true
			}
		}
	}
	if g.config != "" && !filepath.IsAbs(g.config) {
		// The server is told the file's absolute path; its directory may differ.
		if abs, err := filepath.Abs(g.config); err == nil {
			g.config = abs
		}
	}
	return g, args[i:], 0, false
}

// resolveSocket returns the socket path: -S as given; else -L's name in the socket directory;
// else, inside a session, the socket in $TMUX, as tmux does; else "default".
func (g *globals) resolveSocket() (string, error) {
	if g.socketPath != "" {
		return g.socketPath, nil
	}
	if g.socketName == "" {
		if t := os.Getenv("TMUX"); t != "" {
			if p, _, _ := strings.Cut(t, ","); p != "" {
				return p, nil
			}
		}
	}
	return socketPath(g.socketName)
}
