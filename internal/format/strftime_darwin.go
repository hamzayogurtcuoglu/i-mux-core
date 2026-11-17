//go:build darwin

package format

import "time"

// Strftime is strftime(3) as the system's C library gives it: on macOS, FreeBSD's.
func Strftime(f string, t time.Time) string { return strftimeBSD(f, t) }
