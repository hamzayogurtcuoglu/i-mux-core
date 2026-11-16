package buffers

import (
	"strings"
	"testing"
)

// Automatic buffers are named bufferN, the newest is the top, buffer-limit drops the oldest;
// a named buffer is not automatic, and renaming makes a buffer named.
func TestStore(t *testing.T) {
	s := New()
	if s.Add("", nil, 50) != nil || !s.Empty() {
		t.Fatal("empty data added a buffer")
	}
	a := s.Add("", []byte("a"), 2)
	b := s.Add("", []byte("b"), 2)
	if a.Name != "buffer0" || b.Name != "buffer1" || s.Top() != b {
		t.Fatalf("names %s %s, top %v", a.Name, b.Name, s.Top())
	}
	name := "mine"
	if err := s.Set([]byte("m"), &name, 2); err != nil {
		t.Fatal(err)
	}
	if s.Top() != b || s.Get("mine").Automatic {
		t.Error("a named buffer is the top, or automatic")
	}
	s.Add("", []byte("c"), 2)
	if s.Get("buffer0") != nil || s.Get("buffer1") == nil || s.Get("buffer2") == nil {
		t.Errorf("buffer-limit 2 kept %v", names(s))
	}
	if err := s.Rename("buffer1", "renamed"); err != nil || s.Get("renamed") == nil || s.Get("renamed").Automatic {
		t.Errorf("rename: %v", err)
	}
	if err := s.Rename("nope", "x"); err == nil || err.Error() != "no buffer nope" {
		t.Errorf("rename unknown: %v", err)
	}
	empty := ""
	if err := s.Set([]byte("x"), &empty, 2); err == nil || err.Error() != "empty buffer name" {
		t.Errorf("empty name: %v", err)
	}
	if walk := names(s); strings.Join(walk, " ") != "buffer2 mine renamed" {
		t.Errorf("walk %v", walk)
	}
}

func names(s *Store) []string {
	var out []string
	for _, b := range s.Walk() {
		out = append(out, b.Name)
	}
	return out
}

// Sample escapes what is not printable and cuts a long buffer with "...".
func TestSample(t *testing.T) {
	if got := Sample(&Buffer{Data: []byte("a\tb\nc")}); got != `a\tb\nc` {
		t.Errorf("sample %q", got)
	}
	long := strings.Repeat("x", 250)
	if got := Sample(&Buffer{Data: []byte(long)}); got != strings.Repeat("x", 200)+"..." {
		t.Errorf("long sample %q", got)
	}
	if got := Sample(&Buffer{Data: []byte(strings.Repeat("x", 150) + "\n")}); got != strings.Repeat("x", 150)+`\n` {
		t.Errorf("short sample %q", got)
	}
}
