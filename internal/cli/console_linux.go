package cli

import "golang.org/x/sys/unix"

const (
	ioctlGetTermios = unix.TCGETS
	// posixVDisable is _POSIX_VDISABLE: a control character that is turned off.
	posixVDisable   = 0
	ioctlSetTermios = unix.TCSETS
)

func flushOutput(fd int) { _ = unix.IoctlSetInt(fd, unix.TCFLSH, unix.TCOFLUSH) }
