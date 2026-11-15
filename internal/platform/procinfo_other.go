//go:build !darwin && !linux

package platform

// ForegroundCommand is not known on this system yet (Windows: task C8.6).
func ForegroundCommand(int) string { return "" }

// CurrentPath is not known on this system yet (Windows: task C8.6).
func CurrentPath(int) string { return "" }
