package pty

import (
	"os"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/platform"
	"golang.org/x/sys/unix"
)

// open opens a new PTY: /dev/ptmx, granted and unlocked, and its slave. The master is
// non-blocking, so that Go's poller reads it and Close stops a pending Read; fd is its
// descriptor, for ioctls (os.File.Fd would make the file blocking again).
func open() (master *os.File, fd int, slave *os.File, err error) {
	fd, err = unix.Open("/dev/ptmx", unix.O_RDWR|unix.O_NOCTTY|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, -1, nil, err
	}
	name := ""
	if err = platform.Grantpt(fd); err == nil {
		if err = platform.Unlockpt(fd); err == nil {
			name, err = platform.Ptsname(fd)
		}
	}
	if err != nil {
		unix.Close(fd)
		return nil, -1, nil, err
	}
	s, err := os.OpenFile(name, os.O_RDWR|unix.O_NOCTTY|unix.O_CLOEXEC, 0)
	if err != nil {
		unix.Close(fd)
		return nil, -1, nil, err
	}
	return os.NewFile(uintptr(fd), "/dev/ptmx"), fd, s, nil
}
