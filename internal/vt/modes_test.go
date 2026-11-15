package vt

import "testing"

func TestModesAndDECRQM(t *testing.T) {
	term := NewTerminal(10, 4, 10)
	s := term.Screen

	if !s.CursorVisible() || !s.AutoWrap() {
		t.Fatalf("initial modes = %#x, want cursor+wrap", s.Mode)
	}

	// Standard SM/RM 4 (IRM) and DECRQM.
	_, _ = term.Write([]byte("\x1b[4$p\x1b[4h\x1b[4$p\x1b[4l\x1b[4$p\x1b[20$p"))
	if !s.HasMode(ModeCursor) || s.InsertMode() {
		t.Fatalf("after IRM toggle: InsertMode = %v", s.InsertMode())
	}
	want := "\x1b[4;2$y\x1b[4;1$y\x1b[4;2$y\x1b[20;0$y"
	if got := string(term.DrainReplies()); got != want {
		t.Fatalf("ANSI DECRQM replies = %q, want %q", got, want)
	}

	// Keypad modes (DECKPAM / DECKPNM).
	_, _ = term.Write([]byte("\x1b="))
	if !s.ApplicationKeypad() {
		t.Fatal("ApplicationKeypad = false after DECKPAM")
	}
	_, _ = term.Write([]byte("\x1b>"))
	if s.ApplicationKeypad() {
		t.Fatal("ApplicationKeypad = true after DECKPNM")
	}

	// Private modes and mutual exclusion of mouse tracking modes (1000, 1002, 1003).
	_, _ = term.Write([]byte("\x1b[?1;6;12;1000;1004;1005;1006;2004;2026;2031h"))
	if !s.ApplicationCursor() || !s.OriginMode() || !s.MouseAny() || !s.MouseStandard() ||
		!s.FocusEvents() || !s.MouseUTF8() || !s.MouseSGR() ||
		!s.BracketedPaste() || !s.SynchronizedOutput() || !s.ThemeUpdates() {
		t.Fatalf("modes not set: %#x", s.Mode)
	}
	_, _ = term.Write([]byte("\x1b[?1002h"))
	if s.MouseStandard() || !s.MouseButton() || s.MouseAll() {
		t.Fatalf("after ?1002h: mode = %#x", s.Mode)
	}
	_, _ = term.Write([]byte("\x1b[?1003h"))
	if s.MouseStandard() || s.MouseButton() || !s.MouseAll() {
		t.Fatalf("after ?1003h: mode = %#x", s.Mode)
	}

	// Query private modes via DECRQM.
	_, _ = term.Write([]byte("\x1b[?1$p\x1b[?3$p\x1b[?6$p\x1b[?7$p\x1b[?12$p\x1b[?25$p" +
		"\x1b[?1000$p\x1b[?1002$p\x1b[?1003$p\x1b[?1004$p\x1b[?1005$p\x1b[?1006$p" +
		"\x1b[?1049$p\x1b[?2004$p\x1b[?2026$p\x1b[?2031$p\x1b[?9999$p"))
	wantPriv := "\x1b[?1;1$y\x1b[?3;4$y\x1b[?6;1$y\x1b[?7;1$y\x1b[?12;1$y\x1b[?25;1$y" +
		"\x1b[?1000;2$y\x1b[?1002;2$y\x1b[?1003;1$y\x1b[?1004;1$y\x1b[?1005;1$y\x1b[?1006;1$y" +
		"\x1b[?1049;2$y\x1b[?2004;1$y\x1b[?2026;1$y\x1b[?2031;1$y\x1b[?9999;0$y"
	if got := string(term.DrainReplies()); got != wantPriv {
		t.Fatalf("private DECRQM replies = %q, want %q", got, wantPriv)
	}

	// Reset private modes and check that 1000 clears all mouse tracking modes.
	_, _ = term.Write([]byte("\x1b[?1;6;7;12;25;1000;1004;1005;1006;2004;2026;2031l"))
	if s.ApplicationCursor() || s.OriginMode() || s.AutoWrap() || s.CursorVisible() ||
		s.MouseAny() || s.FocusEvents() || s.MouseUTF8() || s.MouseSGR() ||
		s.BracketedPaste() || s.SynchronizedOutput() || s.ThemeUpdates() {
		t.Fatalf("modes not cleared: %#x", s.Mode)
	}
}

func TestCaptureAlternateFlag(t *testing.T) {
	term := NewTerminal(10, 3, 10)
	_, _ = term.Write([]byte("primary-1\r\nprimary-2\r\nprimary-3\x1b[?1049h\x1b[1;1Halt-1\r\nalt-2"))
	if !term.Screen.IsAlternate() {
		t.Fatal("expected alternate screen to be active")
	}
	if got := term.Screen.Capture(CaptureOptions{}); got != "alt-1\nalt-2\n\n" {
		t.Fatalf("Capture() = %q", got)
	}
	if got := term.Screen.Capture(CaptureOptions{Alternate: true}); got != "primary-1\nprimary-2\nprimary-3\n" {
		t.Fatalf("Capture(Alternate: true) = %q", got)
	}
	_, _ = term.Write([]byte("\x1b[?1049l"))
	if got := term.Screen.Capture(CaptureOptions{Alternate: true}); got != "" {
		t.Fatalf("Capture(Alternate: true) outside altscreen = %q, want empty", got)
	}
}
