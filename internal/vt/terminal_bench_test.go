package vt

import (
	"fmt"
	"math/rand/v2"
	"os"
	"strings"
	"testing"
	"time"
)

// Streams for the emulator benchmarks (TASKS.md C1.13), each about size bytes.

func plainStream(size int) []byte {
	var b strings.Builder
	for i := 0; b.Len() < size; i++ {
		fmt.Fprintf(&b, "%6d the quick brown fox jumps over the lazy dog while the agent works\r\n", i)
	}
	return []byte(b.String())
}

func colourStream(size int) []byte {
	r := rand.New(rand.NewPCG(5, 6))
	var b strings.Builder
	for b.Len() < size {
		for i := 0; i < 8; i++ {
			switch r.IntN(3) {
			case 0:
				fmt.Fprintf(&b, "\x1b[38;2;%d;%d;%dmword ", r.IntN(256), r.IntN(256), r.IntN(256))
			case 1:
				fmt.Fprintf(&b, "\x1b[1;38;5;%d;48;5;%dmword\x1b[0m ", r.IntN(256), r.IntN(256))
			default:
				fmt.Fprintf(&b, "\x1b[%d;4:3mword\x1b[m ", 30+r.IntN(8))
			}
		}
		b.WriteString("\r\n")
	}
	return []byte(b.String())
}

func cursorStream(size int) []byte {
	r := rand.New(rand.NewPCG(7, 8))
	var b strings.Builder
	for b.Len() < size {
		fmt.Fprintf(&b, "\x1b[%d;%dH\x1b[K%s", 1+r.IntN(50), 1+r.IntN(150), "status 42%")
		if r.IntN(20) == 0 {
			b.WriteString("\x1b[2J\x1b[H")
		}
		if r.IntN(10) == 0 {
			fmt.Fprintf(&b, "\x1b[%dL\x1b[%dM\x1b[%d@\x1b[%dP", 1+r.IntN(3), 1+r.IntN(3), 1+r.IntN(4), 1+r.IntN(4))
		}
	}
	return []byte(b.String())
}

func benchTerminal(b *testing.B, data []byte) {
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		t := NewTerminal(200, 50, 2000)
		for i := 0; i < len(data); i += 32 << 10 {
			_, _ = t.Write(data[i:min(i+32<<10, len(data))])
		}
	}
	allocs := float64(testing.AllocsPerRun(1, func() {
		t := NewTerminal(200, 50, 2000)
		_, _ = t.Write(data)
	}))
	b.ReportMetric(allocs/(float64(len(data))/1024), "allocs/KB")
}

func BenchmarkTerminalPlain(b *testing.B)  { benchTerminal(b, plainStream(8<<20)) }
func BenchmarkTerminalColour(b *testing.B) { benchTerminal(b, colourStream(8<<20)) }
func BenchmarkTerminalCursor(b *testing.B) { benchTerminal(b, cursorStream(8<<20)) }
func BenchmarkTerminalMixed(b *testing.B)  { benchTerminal(b, mixedStream(8<<20)) }

// TestTerminalFixtureThroughput measures the whole emulator on a large fixture file when one is
// given (IMUX_VT_FIXTURE), as the spike did for the parser alone.
func TestTerminalFixtureThroughput(t *testing.T) {
	path := os.Getenv("IMUX_VT_FIXTURE")
	if path == "" {
		t.Skip("IMUX_VT_FIXTURE not set")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	term := NewTerminal(200, 50, 2000)
	start := time.Now()
	for i := 0; i < len(data); i += 32 << 10 {
		_, _ = term.Write(data[i:min(i+32<<10, len(data))])
	}
	d := time.Since(start)
	t.Logf("%d bytes in %v: %.1f MB/s", len(data), d, float64(len(data))/1e6/d.Seconds())
}
