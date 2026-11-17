//go:build linux

package main

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// cpuTime is the process's user and system CPU time, from /proc/PID/stat.
func cpuTime(pid int) time.Duration {
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		fail(err)
	}
	// The fields after the command's closing parenthesis; utime and stime are 14 and 15.
	s := string(b)
	f := strings.Fields(s[strings.LastIndexByte(s, ')')+2:])
	ut, _ := strconv.ParseInt(f[11], 10, 64)
	st, _ := strconv.ParseInt(f[12], 10, 64)
	const hz = 100 // sysconf(_SC_CLK_TCK) on Linux
	return time.Duration(ut+st) * time.Second / hz
}

// rss is the process's resident memory, in bytes.
func rss(pid int) int64 {
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/statm")
	if err != nil {
		fail(err)
	}
	f := strings.Fields(string(b))
	pages, _ := strconv.ParseInt(f[1], 10, 64)
	return pages * int64(os.Getpagesize())
}
