//go:build !darwin

package format

import "time"

// Strftime is strftime(3) as the system's C library gives it: glibc's.
func Strftime(f string, t time.Time) string { return strftimeGNU(f, t) }
