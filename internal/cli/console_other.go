//go:build !darwin && !linux

package cli

import (
	"errors"
	"io"
	"net"
	"os"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/ipc"
)

// The attached client's console on Windows comes with Phase 8 (C8.x).

type console struct{}

func newConsole() *console { return &console{} }

func (*console) size() (int, int, error) { return 0, 0, errors.New("no console") }

func ttyName(*os.File) string { return "" }

func runAttached(net.Conn, *ipc.Reader, io.Writer) (int, error) {
	return 1, errors.New("imux-core: attaching is not supported on this system yet")
}

func eraseChar(*os.File) (int, bool) { return 0, false }
