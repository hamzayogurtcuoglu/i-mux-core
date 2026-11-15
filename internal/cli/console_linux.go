package cli

import "golang.org/x/sys/unix"

const (
	ioctlGetTermios = unix.TCGETS
	ioctlSetTermios = unix.TCSETS
)

func flushOutput(fd int) { _ = unix.IoctlSetInt(fd, unix.TCFLSH, unix.TCOFLUSH) }
