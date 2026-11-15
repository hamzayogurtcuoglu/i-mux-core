package vt

import "testing"

// FuzzTerminal feeds arbitrary bytes to the whole emulator, with resizes between writes, and
// checks that it never panics and its state stays consistent.
func FuzzTerminal(f *testing.F) {
	for _, s := range []string{
		"hello\r\nworld", "\x1b[2J\x1b[H\x1b[31mred\x1b[0m", "日本語🙂\u0301\u200d", "\x1b[?1049h\x1b[5;10Hx\x1b[?1049l",
		"\x1b[3;5r\x1b[4h\x1b[2L\x1b[3M\x1b[5@\x1b[2P\x1b[10b", "\x1b]8;id=a;http://x\x1b\\link\x1b]8;;\x1b\\",
		"\x1b]2;title\x07\x1b[22;0t\x1b[23;0t", "\x1b[38:2::1:2:3m\x1b[4:3m\x1b[58;5;9m", "\x1b(0lqk\x1b(B\tx\x1bH\x1b[g",
	} {
		f.Add([]byte(s), uint8(20), uint8(5))
	}
	f.Fuzz(func(t *testing.T, data []byte, w, h uint8) {
		cols, rows := 1+int(w)%120, 1+int(h)%40
		term := NewTerminal(cols, rows, 100)
		half := len(data) / 2
		_, _ = term.Write(data[:half])
		term.Resize(1+int(h)%90, 1+int(w)%30)
		_, _ = term.Write(data[half:])
		term.Resize(cols, rows)
		s := term.Screen
		gd := s.Grid
		// x may pass the edge: narrowing the alternate screen (no reflow) leaves the cursor
		// where it was, as tmux does (fixture alternate-narrow-cursor).
		if s.Cx < 0 || s.Cx > 120 || s.Cy < 0 || s.Cy >= s.SizeY() {
			t.Fatalf("cursor %d,%d outside %dx%d", s.Cx, s.Cy, s.SizeX(), s.SizeY())
		}
		// The history may pass its limit after a resize: tmux collects it only when scrolling
		// (fixture history-reflow-over-limit).
		if len(gd.lines) != gd.HSize+gd.Sy || gd.HSize < 0 {
			t.Fatalf("%d lines for history %d and screen %d", len(gd.lines), gd.HSize, gd.Sy)
		}
		if s.Rupper < 0 || s.Rlower >= s.SizeY() || s.Rupper > s.Rlower {
			t.Fatalf("scroll region %d-%d on %d rows", s.Rupper, s.Rlower, s.SizeY())
		}
		_ = s.Capture(CaptureOptions{Sequences: true, StartAll: true, EndAll: true})
		_ = s.Capture(CaptureOptions{Join: true, StartAll: true, EndAll: true})
	})
}
