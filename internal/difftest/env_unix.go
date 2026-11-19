//go:build !windows

package difftest

// systemEnv is the part of FixedEnv that names the system's programs.
func systemEnv() []string { return []string{"PATH=/usr/bin:/bin", "SHELL=/bin/sh"} }
