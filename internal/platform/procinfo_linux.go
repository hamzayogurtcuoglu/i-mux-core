package platform

import (
	"bytes"
	"os"
	"strconv"

	"golang.org/x/sys/unix"
)

// processName returns the first word of /proc/<pid>/cmdline, as tmux's osdep-linux.c does.
func processName(pid int) string {
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/cmdline")
	if err != nil {
		return ""
	}
	if i := bytes.IndexByte(b, 0); i >= 0 {
		b = b[:i]
	}
	return string(b)
}

// processCwd reads /proc/<pid>/cwd, else the cwd of the terminal's session leader, as tmux's
// osdep-linux.c does.
func processCwd(pid int, fd int) string {
	if p, err := os.Readlink("/proc/" + strconv.Itoa(pid) + "/cwd"); err == nil {
		return p
	}
	sid, err := unix.IoctlGetInt(fd, unix.TIOCGSID)
	if err != nil {
		return ""
	}
	p, err := os.Readlink("/proc/" + strconv.Itoa(sid) + "/cwd")
	if err != nil {
		return ""
	}
	return p
}
