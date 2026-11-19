//go:build !darwin && !linux && !windows

package platform

// ForegroundCommand is not known on this system yet (Windows: task C8.6).
func ForegroundCommand(int) string { return "" }

// CurrentPath is not known on this system yet (Windows: task C8.6).
func CurrentPath(int) string { return "" }

// CwdFromScreen: the system's process cwd is not read; nor is it shown.
const CwdFromScreen = false
