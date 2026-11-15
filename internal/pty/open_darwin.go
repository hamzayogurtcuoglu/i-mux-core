package pty

import (
	"os"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/platform"
	"golang.org/x/sys/unix"
)

// open opens a new PTY: /dev/ptmx, granted and unlocked, and its slave.
func open() (master, slave *os.File, err error) {
	m, err := os.OpenFile("/dev/ptmx", os.O_RDWR|unix.O_NOCTTY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, nil, err
	}
	fd := int(m.Fd())
	name := ""
	if err = platform.Grantpt(fd); err == nil {
		if err = platform.Unlockpt(fd); err == nil {
			name, err = platform.Ptsname(fd)
		}
	}
	if err != nil {
		m.Close()
		return nil, nil, err
	}
	s, err := os.OpenFile(name, os.O_RDWR|unix.O_NOCTTY|unix.O_CLOEXEC, 0)
	if err != nil {
		m.Close()
		return nil, nil, err
	}
	return m, s, nil
}
