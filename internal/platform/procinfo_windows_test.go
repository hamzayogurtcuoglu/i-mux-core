//go:build windows

package platform

import (
	"testing"
	"time"
)

// ForegroundCommand reads the system (a snapshot of every process, some 8 ms on the server's
// loop) at most once per job every fgFresh: the frames of a pane switch ask it again and again.
func TestForegroundCommandIsReused(t *testing.T) {
	now := time.Unix(1000, 0)
	reads := map[int]int{}
	names := map[int]string{7: "claude", 9: "pwsh"}
	oldNow, oldRead := fgNow, readForeground
	t.Cleanup(func() {
		fgNow, readForeground = oldNow, oldRead
		fgMu.Lock()
		fgCache = map[int]fgEntry{}
		fgMu.Unlock()
	})
	fgNow = func() time.Time { return now }
	readForeground = func(job int) string { reads[job]++; return names[job] }

	for range 5 {
		if got := ForegroundCommand(7); got != "claude" {
			t.Fatalf("got %q", got)
		}
	}
	if got := ForegroundCommand(9); got != "pwsh" || reads[7] != 1 || reads[9] != 1 {
		t.Fatalf("got %q, reads %v", got, reads)
	}
	names[7] = "node"
	now = now.Add(fgFresh - time.Millisecond)
	if got := ForegroundCommand(7); got != "claude" || reads[7] != 1 {
		t.Fatalf("within fgFresh: %q, reads %v", got, reads)
	}
	now = now.Add(time.Millisecond)
	if got := ForegroundCommand(7); got != "node" || reads[7] != 2 {
		t.Fatalf("after fgFresh: %q, reads %v", got, reads)
	}
	if ForegroundCommand(0) != "" || reads[0] != 0 {
		t.Fatal("no job: no read")
	}
	fgMu.Lock()
	_, stale := fgCache[9]
	fgMu.Unlock()
	if stale {
		t.Fatal("an old job's answer is kept")
	}
}
