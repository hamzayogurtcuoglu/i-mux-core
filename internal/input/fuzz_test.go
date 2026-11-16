package input

import "testing"

// FuzzInput feeds a terminal's input in two parts: the parser never panics, and once escape-time
// has passed every byte has been read.
func FuzzInput(f *testing.F) {
	for _, s := range []string{
		"a\x1b[A\x1b[1;5A\x1bOP", "\x1b[200~x\x1b[201~", "\x1b[<0;1;1M\x1b[M !!", "\x1b[13;5u",
		"\x1b[27;5;118~", "\x1b[?62;4c\x1b[>84;0;0c\x1bP>|tmux 3.8\x1b\\", "\x1b]11;rgb:0/0/0\x07",
		"\x1b]52;c;aGk=\x07", "\x1b[?2026;2$y", "\xe6\x97\xa5\xff\x1b", "\x1b]4;1;?\x07",
	} {
		f.Add([]byte(s), 3)
	}
	f.Fuzz(func(t *testing.T, b []byte, split int) {
		p := New(nil, 0x7f)
		p.Requested()
		if split < 0 || split > len(b) {
			split = len(b) / 2
		}
		p.Feed(b[:split])
		p.Feed(b[split:])
		for range len(b) + 1 {
			if _, ok := p.Delay(); !ok {
				break
			}
			p.Expire()
		}
		if len(p.buf) != 0 {
			t.Fatalf("%q: %q left", b, p.buf)
		}
	})
}
