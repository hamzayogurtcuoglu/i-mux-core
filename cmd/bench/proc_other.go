//go:build !linux && !darwin

package main

import (
	"errors"
	"time"
)

// The bench measures on Linux and macOS only (TASKS.md C6.4).

func cpuTime(int) time.Duration {
	fail(errors.New("bench: not supported on this system"))
	return 0
}

func rss(int) int64 {
	fail(errors.New("bench: not supported on this system"))
	return 0
}
