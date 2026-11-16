package status

import (
	"testing"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/keys"
)

// typeKeys gives the prompt each key.
func typeKeys(pr *Prompt, ks ...keys.Key) {
	for _, k := range ks {
		pr.Key(k)
	}
}

func runes(s string) []keys.Key {
	var out []keys.Key
	for _, r := range s {
		out = append(out, keys.Key(r))
	}
	return out
}

// Emacs editing: moving, inserting, deleting, C-u, C-k, C-w and C-y, and Enter giving the
// input; Escape cancels.
func TestPromptEmacs(t *testing.T) {
	var got *string
	done := func(s *string) bool { got = s; return true }
	pr := NewPrompt("p ", "hello world", 0, PromptTypeCommand, PromptOptions{WordSeparators: "-"}, done)
	ctrl := func(c byte) keys.Key { return keys.Key(c) | keys.Ctrl }
	typeKeys(pr, ctrl('w'))
	if s := fromChars(pr.buffer); s != "hello " {
		t.Fatalf("C-w: %q", s)
	}
	typeKeys(pr, keys.Home, 'X', keys.End, ctrl('y'), keys.Left, keys.Left, keys.BSpace, keys.DC)
	if s := fromChars(pr.buffer); s != "Xhello wod" {
		t.Fatalf("editing: %q", s)
	}
	typeKeys(pr, ctrl('a'), keys.Right, ctrl('k'), '!', ctrl('e'))
	if s := fromChars(pr.buffer); s != "X!" || pr.index != 2 {
		t.Fatalf("C-k: %q at %d", s, pr.index)
	}
	if pr.Key('\r') != PromptKeyClose || got == nil || *got != "X!" || !pr.Closed() {
		t.Fatalf("Enter: %v", got)
	}
	pr = NewPrompt("p ", "x", 0, PromptTypeCommand, PromptOptions{}, done)
	got = new(string)
	if pr.Key(keys.C0ESC) != PromptKeyClose || got != nil {
		t.Fatalf("Escape: %v", got)
	}
}

// The history: Up gives older inputs, Down newer ones and then nothing.
func TestPromptHistory(t *testing.T) {
	h := &History{}
	opts := PromptOptions{History: h, HistoryLimit: 2}
	done := func(*string) bool { return true }
	for _, s := range []string{"one", "two", "three"} {
		pr := NewPrompt("", "", 0, PromptTypeCommand, opts, done)
		typeKeys(pr, runes(s)...)
		pr.Key('\r')
	}
	pr := NewPrompt("", "", 0, PromptTypeCommand, opts, done)
	typeKeys(pr, keys.Up)
	if s := fromChars(pr.buffer); s != "three" {
		t.Errorf("up: %q", s)
	}
	typeKeys(pr, keys.Up, keys.Up)
	if s := fromChars(pr.buffer); s != "two" {
		t.Errorf("up twice more (one is gone): %q", s)
	}
	typeKeys(pr, keys.Down, keys.Down)
	if s := fromChars(pr.buffer); s != "" {
		t.Errorf("down: %q", s)
	}
}

// vi keys: Escape enters command mode, where h, x, w and a work, and i goes back.
func TestPromptVi(t *testing.T) {
	pr := NewPrompt("", "ab cd", 0, PromptTypeCommand, PromptOptions{Vi: true, WordSeparators: ""}, func(*string) bool { return true })
	typeKeys(pr, keys.C0ESC, '0', 'x', 'w', 'x', 'i', 'Z')
	if s := fromChars(pr.buffer); s != "b Zd" {
		t.Errorf("vi: %q", s)
	}
	if pr.Flags&PromptCommand != 0 {
		t.Error("still in command mode")
	}
}
