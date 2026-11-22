package cli

import (
	"fmt"
	"io"
	"net"
	"os"
	"strings"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/ipc"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/termout"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/version"
)

// The command client: connect (starting a server when the commands need one), say Hello, send
// the command list, print the Result.

// runClient runs a command list on the server at path and returns the exit code.
func runClient(g globals, path string, cmds [][]string, stdout, stderr io.Writer) int {
	c, err := connect(path, g, cmds)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	defer c.Close()
	code, err := command(c, path, g, cmds, hello(g, true), stdout, stderr)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return code
}

// command says h, sends cmds and prints their result; a client whose Hello has no terminal is
// never attached.
func command(c net.Conn, path string, g globals, cmds [][]string, h *ipc.Hello, stdout, stderr io.Writer) (int, error) {
	if err := send(c, ipc.TypeHello, h); err != nil {
		return 1, lostServer(err)
	}
	if err := send(c, ipc.TypeCommand, &ipc.Command{Commands: cmds}); err != nil {
		return 1, lostServer(err)
	}
	r := ipc.NewReader(c)
	for {
		f, err := r.Read()
		if err != nil {
			return 1, lostServer(err)
		}
		switch f.Type {
		case ipc.TypeVersionError:
			var v ipc.VersionError
			if err := f.Decode(&v); err != nil {
				return 1, err
			}
			return 1, &ipc.MismatchError{Socket: path, Client: ipc.ProtocolVersion, Server: v.Server}
		case ipc.TypeAttach:
			if !h.StdinTTY {
				return 1, errNoTerminal
			}
			return runAttached(c, r, stdout)
		case ipc.TypeResult:
			var res ipc.Result
			if err := f.Decode(&res); err != nil {
				return 1, err
			}
			_, _ = io.WriteString(stdout, res.Stdout)
			_, _ = io.WriteString(stderr, res.Stderr)
			return res.Code, nil
		}
	}
}

func send(c net.Conn, t ipc.Type, msg any) error {
	f, err := ipc.Encode(t, msg)
	if err != nil {
		return err
	}
	return ipc.WriteFrame(c, f.Type, f.Payload)
}

// utf8Locale is tmux's test for a UTF-8 client: $TMUX set, or LC_ALL, LC_CTYPE or LANG (the
// first set) naming UTF-8.
func utf8Locale(getenv func(string) string) bool {
	if getenv("TMUX") != "" {
		return true
	}
	s := getenv("LC_ALL")
	if s == "" {
		s = getenv("LC_CTYPE")
	}
	if s == "" {
		s = getenv("LANG")
	}
	s = strings.ToUpper(s)
	return strings.Contains(s, "UTF-8") || strings.Contains(s, "UTF8")
}

// lostServer is the error when the server goes away mid-command.
func lostServer(error) error { return fmt.Errorf("server exited unexpectedly") }

// errNoTerminal is what a client without a terminal says when the server would attach it.
var errNoTerminal = fmt.Errorf("open terminal failed: not a terminal")

// hello is the client's Hello. With tty, stdin and stdout say whether there is a terminal, as
// the binary's client; without, there is none: a client inside another program's process
// (E18), whose stdin and stdout are that program's.
func hello(g globals, tty bool) *ipc.Hello {
	h := &ipc.Hello{
		Protocol:      ipc.ProtocolVersion,
		ClientVersion: version.Version,
		Argv:          os.Args,
		Env:           map[string]string{},
		UTF8:          g.utf8 || utf8Locale(os.Getenv),
		Pid:           os.Getpid(),
	}
	if tty {
		h.UTF8 = h.UTF8 || consoleUTF8()
		h.StdinTTY = isTerminal(os.Stdin)
		h.StdoutTTY = isTerminal(os.Stdout)
	}
	h.Cwd, _ = os.Getwd()
	if h.StdinTTY {
		// What an attached client needs: its terminal's name, size and capabilities (E15).
		h.TTYName = ttyName(os.Stdin)
		h.Size, _ = newConsole().size()
		dirs := termout.SearchDirs(os.Getenv)
		if ti, err := termout.Lookup(os.Getenv("TERM"), dirs); err == nil {
			h.Terminfo = ti.Capabilities()
		} else if termout.NoDatabase(dirs) {
			// No terminfo on this system (Windows): the built-in xterm-256color (E15), and TERM
			// says so when it was not set.
			if ti, err := termout.Builtin(); err == nil {
				h.Terminfo = ti.Capabilities()
				if os.Getenv("TERM") == "" {
					h.Env["TERM"] = termout.BuiltinName
				}
			}
		}
		if e, ok := eraseChar(os.Stdin); ok {
			h.Erase = &e
		}
	}
	for _, kv := range os.Environ() {
		// The whole environment, as tmux's client sends it (C3.13).
		if k, v, ok := strings.Cut(kv, "="); ok {
			h.Env[k] = v
		}
	}
	return h
}
