//go:build difftest

package difftest

import (
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/vt/vtfixture"
)

// TestMain lets the test binary be the probe's helper in a pane, as cmd/vtfixture is for tmux.
func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == "__probe" {
		os.Exit(vtfixture.ProbeHelper(os.Args[2:]))
	}
	os.Exit(m.Run())
}

// TestReplyProbe runs the reply probe (testdata/vt/replies.txt, TASKS.md C1.8) in a pane on
// each engine: a program in an i-mux-core pane gets the answers it gets in a tmux pane, through
// the server and its PTY, and both are the answers recorded in replies.out.
func TestReplyProbe(t *testing.T) {
	ref, core := tmuxEngine(t), coreEngine(t)
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	const probe = "../../testdata/vt/replies.txt"
	want, err := vtfixture.ReadEntries("../../testdata/vt/replies.out")
	if err != nil {
		t.Fatal(err)
	}
	replies := func(eng *Binary) []vtfixture.Entry {
		out, err := vtfixture.Probe(eng.Path, self, probe, 60*time.Second)
		if err != nil {
			t.Fatalf("%s: %v", eng.Name(), err)
		}
		f, err := os.CreateTemp(t.TempDir(), "replies")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.WriteString(out); err != nil {
			t.Fatal(err)
		}
		f.Close()
		got, err := vtfixture.ReadEntries(f.Name())
		if err != nil {
			t.Fatalf("%s: %v", eng.Name(), err)
		}
		return got
	}
	a, b := replies(ref), replies(core)
	if len(a) != len(want) || len(b) != len(want) {
		t.Fatalf("%d entries in replies.out, tmux gave %d, imux-core %d", len(want), len(a), len(b))
	}
	for i, w := range want {
		if a[i] != w {
			t.Errorf("%s: tmux now answers %s, replies.out has %s (run make vt-fixtures)", w.Name, strconv.Quote(a[i].Query), strconv.Quote(w.Query))
		}
		if b[i] != a[i] {
			t.Errorf("%s: imux-core answers %s, tmux %s", w.Name, strconv.Quote(b[i].Query), strconv.Quote(a[i].Query))
		}
	}
}
