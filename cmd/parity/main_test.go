package main

import (
	"strings"
	"testing"
)

// TestReport turns a small go test -json stream into the table: statuses, a note's cause and
// task, and the first error line of a failure without a note.
func TestReport(t *testing.T) {
	in := strings.Join([]string{
		`{"Action":"run","Test":"TestA"}`,
		`{"Action":"pass","Test":"TestA","Elapsed":1}`,
		`{"Action":"run","Test":"TestB"}`,
		`{"Action":"output","Test":"TestB/sub","Output":"    b_test.go:12: want 1, got 2\n"}`,
		`{"Action":"fail","Test":"TestB/sub"}`,
		`{"Action":"fail","Test":"TestB","Elapsed":2}`,
		`{"Action":"run","Test":"TestC"}`,
		`{"Action":"fail","Test":"TestC","Elapsed":3}`,
		`{"Action":"skip","Test":"TestD"}`,
		`not json`,
	}, "\n")
	res, err := read(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	ns := notes{byTest: map[string]note{"TestC": {cause: "a | cause", task: "C6.3"}}}
	ns.byText = append(ns.byText, struct {
		text string
		note
	}{"got 2", note{cause: "two", task: "C9.9"}})
	write(&b, "abc123", res, ns)
	got := b.String()
	for _, want := range []string{
		"from i-mux at `abc123`",
		"4 tests: 1 pass, 2 fail, 1 skip.",
		"| `TestA` | pass |  |  |",
		"| `TestB` | fail | two | C9.9 |",
		"| `TestC` | fail | a \\| cause | C6.3 |",
		"| `TestD` | skip |  |  |",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
}
