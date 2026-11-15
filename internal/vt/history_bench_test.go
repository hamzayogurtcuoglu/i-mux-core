package vt

import (
	"fmt"
	"runtime"
	"strings"
	"testing"
)

// historyLines is the content of the history benchmark: 80-column lines of plain and colored
// text, as an agent's transcript.
func historyLines(n int) []byte {
	var b strings.Builder
	for i := 0; i < n; i++ {
		if i%3 == 0 {
			fmt.Fprintf(&b, "\x1b[38;5;%dm%-6d\x1b[0m the quick brown fox jumps over the lazy dog, line %07d ok\r\n", i%256, i, i)
		} else {
			fmt.Fprintf(&b, "%-6d the quick brown fox jumps over the lazy dog and the pane keeps it, %07d\r\n", i, i)
		}
	}
	return []byte(b.String())
}

// BenchmarkHistoryMemory reports the heap a full 50000-line history of 80-column lines takes
// (ARCHITECTURE.md §10's memory target).
func BenchmarkHistoryMemory(b *testing.B) {
	data := historyLines(52000)
	for range b.N {
		runtime.GC()
		var m0, m1 runtime.MemStats
		runtime.ReadMemStats(&m0)
		t := NewTerminal(80, 24, 50000)
		_, _ = t.Write(data)
		runtime.GC()
		runtime.ReadMemStats(&m1)
		lines := t.Screen.Grid.HSize + t.Screen.Grid.Sy
		b.ReportMetric(float64(m1.HeapAlloc-m0.HeapAlloc)/float64(lines), "bytes/line")
		b.ReportMetric(float64(m1.HeapAlloc-m0.HeapAlloc)/(1<<20), "MiB")
		runtime.KeepAlive(t)
	}
}
