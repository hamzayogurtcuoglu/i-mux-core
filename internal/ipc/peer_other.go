//go:build unix && !darwin && !linux

package ipc

import (
	"errors"
	"net"
)

// peerUID is not known here: every connection is refused.
func peerUID(*net.UnixConn) (int, error) { return -1, errors.New("ipc: no peer credentials") }
