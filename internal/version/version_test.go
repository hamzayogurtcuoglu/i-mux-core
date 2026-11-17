package version

import "testing"

func TestLine(t *testing.T) {
	if got, want := Line(), "tmux 3.8 (imux-core 0.0.0-dev)"; got != want {
		t.Fatalf("Line() = %q, want %q", got, want)
	}
}
