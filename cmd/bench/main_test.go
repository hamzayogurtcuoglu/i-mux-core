package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// percentile picks the value at that rank of the sorted values.
func TestPercentile(t *testing.T) {
	xs := []float64{5, 1, 4, 2, 3}
	if p := percentile(xs, 0.5); p != 3 {
		t.Errorf("p50 %g", p)
	}
	if p := percentile([]float64{9, 1, 1, 1}, 0.99); p != 1 {
		t.Errorf("p99 %g", p)
	}
}

// writeText writes 80-column lines, a third of them in colour, up to the size asked for.
func TestWriteText(t *testing.T) {
	path := filepath.Join(t.TempDir(), "text")
	writeText(path, 10000)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := bytes.Split(bytes.TrimSuffix(b, []byte("\n")), []byte("\n"))
	if len(b) < 10000 || len(lines) < 100 {
		t.Fatalf("%d bytes, %d lines", len(b), len(lines))
	}
	withColour := 0
	for _, l := range lines {
		if bytes.HasPrefix(l, []byte("\033[")) {
			withColour++
			l = bytes.TrimSuffix(l[5:], []byte("\033[0m"))
		}
		if len(l) != 79 {
			t.Fatalf("line of %d columns: %q", len(l), l)
		}
	}
	if withColour != (len(lines)+2)/3 {
		t.Errorf("%d of %d lines in colour", withColour, len(lines))
	}
}
