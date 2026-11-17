package cli

import (
	"bytes"
	"testing"
)

func TestVersionFlag(t *testing.T) {
	var out, errb bytes.Buffer
	if code := Main([]string{"-V"}, &out, &errb); code != 0 {
		t.Fatalf("exit code %d, stderr %q", code, errb.String())
	}
	if got, want := out.String(), "tmux 3.8 (imux-core 0.0.0-dev)\n"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
}
