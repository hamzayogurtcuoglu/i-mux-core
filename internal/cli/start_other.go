//go:build !unix && !windows

package cli

import (
	"errors"
	"io"
	"net"
	"os"
)

// The server and its socket on Windows come with Phase 8 (task C0.7 decides the transport).

var errNoServerYet = errors.New("imux-core: the server is not supported on this system yet")

func socketPath(string) (string, error) { return "", errNoServerYet }

func connect(string, globals, [][]string) (net.Conn, error) { return nil, errNoServerYet }

func runServer(_ globals, stderr io.Writer) int {
	_, _ = io.WriteString(stderr, errNoServerYet.Error()+"\n")
	return 1
}

func isTerminal(*os.File) bool { return false }
