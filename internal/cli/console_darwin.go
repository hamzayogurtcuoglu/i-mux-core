package cli

import "golang.org/x/sys/unix"

const (
	ioctlGetTermios = unix.TIOCGETA
	// posixVDisable is _POSIX_VDISABLE: a control character that is turned off.
	posixVDisable   = 0xff
	ioctlSetTermios = unix.TIOCSETA
)

// fWrite is <sys/fcntl.h>'s FWRITE: TIOCFLUSH flushes the output queue.
const fWrite = 0x2

func flushOutput(fd int) { _ = unix.IoctlSetPointerInt(fd, unix.TIOCFLUSH, fWrite) }
