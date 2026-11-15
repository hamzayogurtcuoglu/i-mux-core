package cli

import (
	"fmt"
	"io"
	"net"
	"os"
	"strings"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/ipc"
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
	code, err := command(c, path, g, cmds, stdout, stderr)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return code
}

func command(c net.Conn, path string, g globals, cmds [][]string, stdout, stderr io.Writer) (int, error) {
	if err := send(c, ipc.TypeHello, hello(g)); err != nil {
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

// lostServer is the error when the server goes away mid-command.
func lostServer(error) error { return fmt.Errorf("server exited unexpectedly") }

// helloEnv says which variables a client passes to the server (ARCHITECTURE.md §5).
func helloEnv(name string) bool {
	switch name {
	case "TERM", "COLORTERM", "LANG", "TERM_PROGRAM", "PATH", "HOME":
		return true
	}
	return strings.HasPrefix(name, "LC_") || strings.HasPrefix(name, "IMUX_")
}

func hello(g globals) *ipc.Hello {
	h := &ipc.Hello{
		Protocol:      ipc.ProtocolVersion,
		ClientVersion: version.Version,
		Argv:          os.Args,
		Env:           map[string]string{},
		UTF8:          g.utf8,
		Pid:           os.Getpid(),
		StdinTTY:      isTerminal(os.Stdin),
		StdoutTTY:     isTerminal(os.Stdout),
	}
	h.Cwd, _ = os.Getwd()
	for _, kv := range os.Environ() {
		if k, v, ok := strings.Cut(kv, "="); ok && helloEnv(k) {
			h.Env[k] = v
		}
	}
	return h
}
