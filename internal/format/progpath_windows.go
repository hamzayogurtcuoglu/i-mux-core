//go:build windows

package format

import "strings"

// programPath names a program given by its Windows path (C:\…\pwsh.exe, \\server\…) by its
// file name without .exe, as a path from / is named by its last part on Unix.
func programPath(s string) (string, bool) {
	drive := len(s) > 2 && s[1] == ':' && (s[2] == '\\' || s[2] == '/')
	if !drive && !strings.HasPrefix(s, `\\`) {
		return "", false
	}
	if i := strings.LastIndexAny(s, `\/`); i >= 0 {
		s = s[i+1:]
	}
	if strings.HasSuffix(strings.ToLower(s), ".exe") {
		s = s[:len(s)-4]
	}
	return s, s != ""
}
