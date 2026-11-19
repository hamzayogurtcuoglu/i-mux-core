//go:build !windows

package format

// programPath: on Unix only a path from / is a program's path (parse_window_name).
func programPath(string) (string, bool) { return "", false }
