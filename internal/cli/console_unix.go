//go:build darwin || linux

package cli

import (
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/ipc"

	"golang.org/x/sys/unix"
)

// The attached client's own terminal on Unix (ARCHITECTURE.md §6, Console).

// console is the client's terminal: stdin for input and its modes, stdout for output.
type console struct {
	in, out *os.File
	saved   *unix.Termios
}

func newConsole() *console { return &console{in: os.Stdin, out: os.Stdout} }

// makeRaw puts the terminal in the mode tmux's tty_start_tty sets, and returns how to restore
// it.
func (c *console) makeRaw() (restore func(), err error) {
	fd := int(c.in.Fd())
	tio, err := unix.IoctlGetTermios(fd, ioctlGetTermios)
	if err != nil {
		return nil, err
	}
	saved := *tio
	tio.Iflag &^= unix.IXON | unix.IXOFF | unix.ICRNL | unix.INLCR | unix.IGNCR | unix.IMAXBEL | unix.ISTRIP
	tio.Iflag |= unix.IGNBRK
	tio.Oflag &^= unix.OPOST | unix.ONLCR | unix.OCRNL | unix.ONLRET
	tio.Lflag &^= unix.IEXTEN | unix.ICANON | unix.ECHO | unix.ECHOE | unix.ECHONL | unix.ECHOCTL |
		unix.ECHOPRT | unix.ECHOKE | unix.ISIG
	tio.Cc[unix.VMIN] = 1
	tio.Cc[unix.VTIME] = 0
	if err := unix.IoctlSetTermios(fd, ioctlSetTermios, tio); err != nil {
		return nil, err
	}
	flushOutput(fd)
	c.saved = &saved
	return func() { _ = unix.IoctlSetTermios(fd, ioctlSetTermios, &saved) }, nil
}

// size is the terminal's size, in cells and pixels.
func (c *console) size() (ipc.Size, error) {
	ws, err := unix.IoctlGetWinsize(int(c.out.Fd()), unix.TIOCGWINSZ)
	if err != nil {
		if ws, err = unix.IoctlGetWinsize(int(c.in.Fd()), unix.TIOCGWINSZ); err != nil {
			return ipc.Size{}, err
		}
	}
	return ipc.Size{Cols: int(ws.Col), Rows: int(ws.Row), XPixel: int(ws.Xpixel), YPixel: int(ws.Ypixel)}, nil
}

// resizes delivers a value on each SIGWINCH until stop is called.
func (c *console) resizes() (<-chan os.Signal, func()) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGWINCH)
	return ch, func() { signal.Stop(ch) }
}

// ttyName is the terminal's device path, as ttyname(3) finds it: the character device in /dev
// with the same device number. tmux names a client by it (#{client_name}, -c and -t).
func ttyName(f *os.File) string {
	var st unix.Stat_t
	if err := unix.Fstat(int(f.Fd()), &st); err != nil || st.Mode&unix.S_IFMT != unix.S_IFCHR {
		return ""
	}
	for _, dir := range []string{"/dev/pts", "/dev"} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			p := filepath.Join(dir, e.Name())
			// Lstat, as ttyname(3) looks: macOS's /dev/stderr and /dev/stdin are links to the
			// same terminal, and come before its own name.
			var dst unix.Stat_t
			if unix.Lstat(p, &dst) == nil && dst.Mode&unix.S_IFMT == unix.S_IFCHR && dst.Rdev == st.Rdev {
				return p
			}
		}
	}
	return ""
}

// eraseChar is the terminal's erase character (VERASE), which tmux reads as BSpace; none when
// it is disabled.
func eraseChar(f *os.File) (int, bool) {
	tio, err := unix.IoctlGetTermios(int(f.Fd()), ioctlGetTermios)
	if err != nil || tio.Cc[unix.VERASE] == posixVDisable {
		return 0, false
	}
	return int(tio.Cc[unix.VERASE]), true
}

// consoleUTF8: on Unix only the locale says a client is UTF-8.
func consoleUTF8() bool { return false }
