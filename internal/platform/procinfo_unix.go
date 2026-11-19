//go:build darwin || linux

package platform

import "golang.org/x/sys/unix"

// ForegroundCommand returns the name of the foreground process of the terminal whose PTY master
// is fd, as tmux's osdep_get_name does, or "" when it cannot tell. It is the raw name: tmux's
// parse_window_name turns it into #{pane_current_command}.
func ForegroundCommand(fd int) string {
	pgrp, err := unix.IoctlGetInt(fd, unix.TIOCGPGRP)
	if err != nil {
		return ""
	}
	return processName(pgrp)
}

// CurrentPath returns the working directory of the foreground process of the terminal whose
// PTY master is fd, as tmux's osdep_get_cwd does (#{pane_current_path}), or "".
func CurrentPath(fd int) string {
	pgrp, err := unix.IoctlGetInt(fd, unix.TIOCGPGRP)
	if err != nil {
		return ""
	}
	return processCwd(pgrp, fd)
}

// CwdFromScreen is false: #{pane_current_path} is the foreground process's directory.
const CwdFromScreen = false
