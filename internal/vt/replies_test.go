package vt

import (
	"strconv"
	"testing"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/vt/vtfixture"
)

// TestRepliesMatchTmux plays testdata/vt/replies.txt on one emulator, in order, and compares
// every answer with what the pinned tmux answered (replies.out, made by cmd/vtfixture on an
// 80x24 pane with no client attached).
func TestRepliesMatchTmux(t *testing.T) {
	queries, err := vtfixture.ReadEntries("../../testdata/vt/replies.txt")
	if err != nil {
		t.Fatal(err)
	}
	want, err := vtfixture.ReadEntries("../../testdata/vt/replies.out")
	if err != nil {
		t.Fatalf("%v (run cmd/vtfixture)", err)
	}
	if len(queries) != len(want) {
		t.Fatalf("%d queries, %d recorded replies: run cmd/vtfixture", len(queries), len(want))
	}
	term := NewTerminal(vtfixture.DefaultCols, vtfixture.DefaultRows, 2000)
	for i, q := range queries {
		_, _ = term.Write([]byte(q.Query))
		got := string(term.DrainReplies())
		if got != want[i].Query {
			t.Errorf("%s: %s gives %s, tmux %s", q.Name, strconv.Quote(q.Query), strconv.Quote(got), strconv.Quote(want[i].Query))
		}
	}
}
