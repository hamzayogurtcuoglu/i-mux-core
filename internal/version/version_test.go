package version

import "testing"

func TestLine(t *testing.T) {
	if got, want := Line(), "imux-core 0.0.0-dev (tmux 3.6 compatible)"; got != want {
		t.Fatalf("Line() = %q, want %q", got, want)
	}
}
