package pty

import (
	"os"
	"strconv"

	"golang.org/x/sys/unix"
)

// open opens a new PTY: /dev/ptmx, unlocked, and its slave. The master is non-blocking, so
// that Go's poller reads it and Close stops a pending Read; fd is its descriptor, for ioctls
// (os.File.Fd would make the file blocking again).
func open() (master *os.File, fd int, slave *os.File, err error) {
	fd, err = unix.Open("/dev/ptmx", unix.O_RDWR|unix.O_NOCTTY|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, -1, nil, err
	}
	if err := unix.IoctlSetPointerInt(fd, unix.TIOCSPTLCK, 0); err != nil {
		unix.Close(fd)
		return nil, -1, nil, err
	}
	n, err := unix.IoctlGetUint32(fd, unix.TIOCGPTN)
	if err != nil {
		unix.Close(fd)
		return nil, -1, nil, err
	}
	s, err := os.OpenFile("/dev/pts/"+strconv.FormatUint(uint64(n), 10), os.O_RDWR|unix.O_NOCTTY|unix.O_CLOEXEC, 0)
	if err != nil {
		unix.Close(fd)
		return nil, -1, nil, err
	}
	return os.NewFile(uintptr(fd), "/dev/ptmx"), fd, s, nil
}
