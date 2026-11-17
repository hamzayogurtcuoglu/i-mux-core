//go:build darwin

package main

import (
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// psField is ps -o FIELD= for the process.
func psField(pid int, field string) string {
	out, err := exec.Command("ps", "-o", field+"=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		fail(err)
	}
	return strings.TrimSpace(string(out))
}

// cpuTime is the process's CPU time, as ps gives it ([[d-]h:]m:s.cc).
func cpuTime(pid int) time.Duration {
	s := psField(pid, "time")
	var total float64
	for _, part := range strings.Split(s, ":") {
		v, err := strconv.ParseFloat(part, 64)
		if err != nil {
			fail(err)
		}
		total = total*60 + v
	}
	return time.Duration(total * float64(time.Second))
}

// rss is the process's resident memory, in bytes.
func rss(pid int) int64 {
	kb, err := strconv.ParseInt(psField(pid, "rss"), 10, 64)
	if err != nil {
		fail(err)
	}
	return kb << 10
}
