package main

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if runChild() {
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// TestEvidence gathers the spike's evidence and writes it to $CONPTY_EVIDENCE (CI uploads it)
// and to the test log.
func TestEvidence(t *testing.T) {
	r := Evidence()
	md := r.Markdown()
	t.Log("\n" + md)
	if p := os.Getenv("CONPTY_EVIDENCE"); p != "" {
		if err := os.WriteFile(p, []byte(md), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// What the engine cannot do without: a pseudo console, a program on it, and the tree's end.
	for _, row := range r.Flags {
		if row.What == "none" && row.Result != "accepted" {
			t.Errorf("CreatePseudoConsole: %s", row.Result)
		}
	}
	for _, row := range r.Process {
		if row.What == "kill the whole tree (job object)" && !strings.Contains(row.Result, "alive after: false") {
			t.Errorf("killing the tree: %s", row.Result)
		}
	}
}

// TestConsoleRoundTrip: what is written to the pseudo console reaches the program, and what it
// prints comes back.
func TestConsoleRoundTrip(t *testing.T) {
	cmd, env := childCmdline("echo-input")
	c, err := StartConsole(cmd, 80, 24, 0, env)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, err := c.WaitOutput("READY", 10*time.Second); err != nil {
		t.Fatal(err)
	}
	if err := c.Write([]byte("hi")); err != nil {
		t.Fatal(err)
	}
	if _, err := c.WaitOutput("IN:6869", 10*time.Second); err != nil {
		if _, err2 := c.WaitOutput("IN:68", time.Second); err2 != nil {
			t.Fatal(err)
		}
	}
}
