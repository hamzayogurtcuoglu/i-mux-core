package vt

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"
)

func TestOSCTitlesAndStack(t *testing.T) {
	term := NewTerminal(20, 5, 10)
	s := term.Screen

	// OSC 0 and 2 set the title; OSC 1 is ignored; #( from a program becomes _(. (All
	// checked on tmux 3.8.)
	_, _ = term.Write([]byte("\x1b]0;first title\x07"))
	if s.Title != "first title" {
		t.Fatalf("after OSC 0: Title = %q, want %q", s.Title, "first title")
	}
	_, _ = term.Write([]byte("\x1b]1;icon title\x1b\\"))
	if s.Title != "first title" {
		t.Fatalf("after OSC 1: Title = %q, want it unchanged", s.Title)
	}
	_, _ = term.Write([]byte("\x1b]2;run #(id)\x07"))
	if s.Title != "run _(id)" {
		t.Fatalf("after OSC 2 with #(id): Title = %q, want %q", s.Title, "run _(id)")
	}

	// APC sets the title as well.
	_, _ = term.Write([]byte("\x1b_apc title\x1b\\"))
	if s.Title != "apc title" {
		t.Fatalf("after APC: Title = %q, want %q", s.Title, "apc title")
	}

	// Title stack push/pop via CSI 22;0t / CSI 23;0t.
	_, _ = term.Write([]byte("\x1b[22;0t\x1b]2;second\x07\x1b[22;2t\x1b]2;third\x07"))
	if s.Title != "third" {
		t.Fatalf("before pop: Title = %q, want %q", s.Title, "third")
	}
	_, _ = term.Write([]byte("\x1b[23;2t"))
	if s.Title != "second" {
		t.Fatalf("after first pop: Title = %q, want %q", s.Title, "second")
	}
	_, _ = term.Write([]byte("\x1b[23;0t"))
	if s.Title != "apc title" {
		t.Fatalf("after second pop: Title = %q, want %q", s.Title, "apc title")
	}
	_, _ = term.Write([]byte("\x1b[23;0t"))
	if s.Title != "apc title" {
		t.Fatalf("a pop with an empty stack changed the title to %q", s.Title)
	}
	// A backslash is escaped once, and a push and pop give it back as it was.
	_, _ = term.Write([]byte("\x1b]2;a\\b\x07\x1b[22;0t\x1b]2;x\x07\x1b[23;0t"))
	if s.Title != `a\\b` {
		t.Fatalf("title after push and pop = %q, want %q", s.Title, `a\\b`)
	}
	// A trusted title (select-pane -T) keeps #(.
	s.SetTitle("run #(id)", false)
	if s.Title != "run #(id)" {
		t.Fatalf("trusted title = %q", s.Title)
	}

	// DisallowSetTitle blocks untrusted OSC/APC updates while SetTitle(..., false) overrides it.
	s.DisallowSetTitle = true
	s.SetTitle("locked", false)
	_, _ = term.Write([]byte("\x1b]2;ignored\x07\x1b_ignored2\x1b\\"))
	if s.Title != "locked" {
		t.Fatalf("with DisallowSetTitle: Title = %q, want %q", s.Title, "locked")
	}

	// ESC k ... ST asks for a window name; the server decides (allow-rename).
	var renamed []string
	term.Rename = func(name string) { renamed = append(renamed, name) }
	_, _ = term.Write([]byte("\x1bkmy-window\x1b\\\x1bk\x1b\\\x1bk\xff\x1b\\"))
	if len(renamed) != 2 || renamed[0] != "my-window" || renamed[1] != "" {
		t.Fatalf("Rename calls = %q, want my-window then empty (invalid UTF-8 dropped)", renamed)
	}
}

func TestOSC7AndOSC9Cwd(t *testing.T) {
	term := NewTerminal(20, 5, 10)
	s := term.Screen

	// OSC 7 keeps the path as sent (tmux's pane_path, checked on tmux 3.8); Cwd decodes it.
	_, _ = term.Write([]byte("\x1b]7;file://myhost/tmp/dir%20with%20spaces/sub\x07"))
	if s.Path != "file://myhost/tmp/dir%20with%20spaces/sub" {
		t.Fatalf("Path = %q", s.Path)
	}
	if s.Cwd != "/tmp/dir with spaces/sub" {
		t.Fatalf("Cwd = %q, want %q", s.Cwd, "/tmp/dir with spaces/sub")
	}

	// OSC 7 with Windows drive letter URI.
	_, _ = term.Write([]byte("\x1b]7;file:///C:/Users/test%20user/proj\x1b\\"))
	if s.Cwd != "C:/Users/test user/proj" {
		t.Fatalf("Windows OSC 7 Cwd = %q, want %q", s.Cwd, "C:/Users/test user/proj")
	}

	// OSC 9;9 (Windows Terminal cwd), with and without quotes: only Cwd, as tmux ignores it.
	path := s.Path
	_, _ = term.Write([]byte("\x1b]9;9;\"C:\\Users\\dev\\work\"\x07"))
	if s.Cwd != `C:\Users\dev\work` {
		t.Fatalf("OSC 9;9 quoted Cwd = %q, want %q", s.Cwd, `C:\Users\dev\work`)
	}
	_, _ = term.Write([]byte("\x1b]9;9;/var/tmp\x1b\\"))
	if s.Cwd != "/var/tmp" {
		t.Fatalf("OSC 9;9 unquoted Cwd = %q, want %q", s.Cwd, "/var/tmp")
	}
	if s.Path != path {
		t.Fatalf("OSC 9;9 changed the path to %q", s.Path)
	}
}

func TestOSC8Hyperlinks(t *testing.T) {
	term := NewTerminal(20, 4, 10)
	s := term.Screen

	_, _ = term.Write([]byte("\x1b]8;id=doc;https://example.com/docs\x1b\\DOCS\x1b]8;;\x1b\\!"))
	c0 := s.Grid.ViewCell(0, 0)
	if c0.Link == 0 {
		t.Fatal("expected non-zero link on 'D'")
	}
	uri, id, ok := s.Hyperlinks.Get(c0.Link)
	if !ok || uri != "https://example.com/docs" || id != "doc" {
		t.Fatalf("Hyperlinks.Get(%d) = (%q, %q, %v)", c0.Link, uri, id, ok)
	}
	if s.Grid.ViewLine(0).Flags&LineHyperlink == 0 {
		t.Fatal("expected LineHyperlink flag on line 0")
	}
	c4 := s.Grid.ViewCell(4, 0)
	if c4.Link != 0 {
		t.Fatalf("expected Link == 0 after closing OSC 8, got %d", c4.Link)
	}

	// Duplicate id= parameter in OSC 8 is rejected.
	_, _ = term.Write([]byte("\x1b]8;id=a:id=b;https://bad.example\x1b\\X"))
	cx := s.Grid.ViewCell(5, 0)
	if cx.Link != 0 {
		t.Fatalf("expected duplicate id= OSC 8 to be ignored, got Link=%d", cx.Link)
	}

	// Oversized URI (> MaxHyperlinkURI) is ignored.
	longURI := "https://example.com/" + strings.Repeat("a", MaxHyperlinkURI)
	if got := s.Hyperlinks.Put(longURI, ""); got != 0 {
		t.Fatalf("expected oversized URI to return 0, got %d", got)
	}
}

func TestOSC52Clipboard(t *testing.T) {
	term := NewTerminal(20, 4, 10)
	// tmux's default, set-clipboard external, ignores a program's OSC 52.
	called := false
	term.SetClipboard = func(string, []byte) { called = true }
	_, _ = term.Write([]byte("\x1b]52;c;aGk=\x07"))
	if called || term.Clipboard != nil {
		t.Fatal("OSC 52 taken with set-clipboard external")
	}
	term.ClipboardMode = ClipboardOn
	var gotClip string
	var gotData []byte
	term.SetClipboard = func(clip string, b []byte) {
		gotClip = clip
		gotData = append([]byte(nil), b...)
	}

	encoded := base64.StdEncoding.EncodeToString([]byte("hello clipboard"))
	_, _ = term.Write([]byte("\x1b]52;ccx0;" + encoded + "\x07"))
	if gotClip != "c0" {
		t.Fatalf("clip = %q, want %q", gotClip, "c0")
	}
	if string(gotData) != "hello clipboard" {
		t.Fatalf("data = %q, want %q", string(gotData), "hello clipboard")
	}
	if string(term.Clipboard) != "hello clipboard" || term.ClipboardName != "c0" {
		t.Fatalf("Terminal.Clipboard = (%q, %q)", term.ClipboardName, string(term.Clipboard))
	}

	// OSC 52 read query (?) is refused: no reply and clipboard unchanged.
	_ = term.DrainReplies()
	_, _ = term.Write([]byte("\x1b]52;c;?\x07\x1b]52;p;?\x1b\\"))
	if got := term.DrainReplies(); len(got) != 0 {
		t.Fatalf("expected no reply to OSC 52;?, got %q", string(got))
	}
	if string(term.Clipboard) != "hello clipboard" {
		t.Fatalf("clipboard changed after read query: %q", string(term.Clipboard))
	}
}

func TestOSCColoursSetQueryAndReset(t *testing.T) {
	term := NewTerminal(20, 4, 10)
	term.Palette.ClientFg = ColorJoinRGB(0xd0, 0xe0, 0xf0)
	term.Palette.ClientBg = ColorJoinRGB(0x10, 0x20, 0x30)

	// Query default fg/bg falls back to client fg/bg; BEL vs ST terminators match.
	_, _ = term.Write([]byte("\x1b]10;?\x07\x1b]11;?\x1b\\"))
	want := "\x1b]10;rgb:d0d0/e0e0/f0f0\x07\x1b]11;rgb:1010/2020/3030\x1b\\"
	if got := string(term.DrainReplies()); got != want {
		t.Fatalf("initial OSC 10/11 query = %q, want %q", got, want)
	}

	// Set OSC 10, 11, 12 using X11 forms (#RRGGBB, rgb:RR/GG/BB, colour name) and query them.
	_, _ = term.Write([]byte("\x1b]10;#123456\x07\x1b]11;rgb:aa/bb/cc\x1b\\\x1b]12;red\x07"))
	_, _ = term.Write([]byte("\x1b]10;?\x1b\\\x1b]11;?\x07\x1b]12;?\x1b\\"))
	want = "\x1b]10;rgb:1212/3434/5656\x1b\\\x1b]11;rgb:aaaa/bbbb/cccc\x07\x1b]12;rgb:ffff/0000/0000\x1b\\"
	if got := string(term.DrainReplies()); got != want {
		t.Fatalf("OSC 10/11/12 query after set = %q, want %q", got, want)
	}

	// Reset OSC 110, 111, 112.
	_, _ = term.Write([]byte("\x1b]110;\x07\x1b]111;\x07\x1b]112;\x07"))
	if term.Palette.Fg != ColorDefault || term.Palette.Bg != ColorDefault || term.Screen.CColour != -1 {
		t.Fatalf("after reset: fg=%v bg=%v ccolour=%v", term.Palette.Fg, term.Palette.Bg, term.Screen.CColour)
	}

	// OSC 4 set multiple palette entries, query them, and reset with OSC 104.
	_, _ = term.Write([]byte("\x1b]4;1;#112233;2;rgb:4444/5555/6666\x1b\\"))
	_, _ = term.Write([]byte("\x1b]4;1;?;2;?\x07"))
	want = "\x1b]4;1;rgb:1111/2222/3333\x07\x1b]4;2;rgb:4444/5555/6666\x07"
	if got := string(term.DrainReplies()); got != want {
		t.Fatalf("OSC 4 query = %q, want %q", got, want)
	}
	_, _ = term.Write([]byte("\x1b]104;1\x07"))
	if got := term.Palette.Get(1 | ColorFlag256); got != -1 {
		t.Fatalf("after OSC 104;1: palette[1] = %v, want -1", got)
	}
	if got := term.Palette.Get(2 | ColorFlag256); got == -1 {
		t.Fatal("after OSC 104;1: expected palette[2] to remain set")
	}
	_, _ = term.Write([]byte("\x1b]104;\x07"))
	if got := term.Palette.Get(2 | ColorFlag256); got != -1 {
		t.Fatalf("after OSC 104;: palette[2] = %v, want -1", got)
	}
}

func TestOSC133AndOSC9Progress(t *testing.T) {
	term := NewTerminal(20, 4, 10)
	s := term.Screen

	_, _ = term.Write([]byte("\x1b]133;A\x07$ \x1b]133;B\x07ls\r\n\x1b]133;C\x07file\r\n\x1b]133;D;42\x07"))
	if s.Grid.ViewLine(0).Flags&(LineStartPrompt|LineStartCommand) != (LineStartPrompt | LineStartCommand) {
		t.Fatalf("line 0 flags = %#x", s.Grid.ViewLine(0).Flags)
	}
	if s.Grid.ViewLine(1).Flags&LineStartOutput == 0 {
		t.Fatalf("line 1 flags = %#x, want LineStartOutput", s.Grid.ViewLine(1).Flags)
	}
	if s.Grid.ViewLine(2).Flags&LineEndOutput == 0 || s.LastCommandStatus != 42 {
		t.Fatalf("line 2 flags = %#x, status = %d", s.Grid.ViewLine(2).Flags, s.LastCommandStatus)
	}

	// OSC 9;4 progress bar.
	_, _ = term.Write([]byte("\x1b]9;4;1;75\x07"))
	if s.ProgressState != 1 || s.ProgressValue != 75 {
		t.Fatalf("progress = (%d, %d), want (1, 75)", s.ProgressState, s.ProgressValue)
	}
}

func TestDCSDECRQSSAndXTGETTCAP(t *testing.T) {
	term := NewTerminal(20, 4, 10)

	// Default cursor style -> Ps = 0.
	_, _ = term.Write([]byte("\x1bP$q q\x1b\\"))
	if got := string(term.DrainReplies()); got != "\x1bP1$r q0 q\x1b\\" {
		t.Fatalf("DECRQSS default cursor style = %q", got)
	}

	// Set steady bar (DECSCUSR 6) -> Ps = 6.
	_, _ = term.Write([]byte("\x1b[6 q\x1bP$q q\x1b\\"))
	if got := string(term.DrainReplies()); got != "\x1bP1$r q6 q\x1b\\" {
		t.Fatalf("DECRQSS cursor style 6 = %q", got)
	}

	// Unrecognized DECRQSS (e.g. m for SGR, r for DECSTBM) -> \x1bP0$r\x1b\\.
	_, _ = term.Write([]byte("\x1bP$qm\x1b\\\x1bP$qr\x1b\\"))
	if !bytes.Equal(term.DrainReplies(), []byte("\x1bP0$r\x1b\\\x1bP0$r\x1b\\")) {
		t.Fatalf("unexpected reply to unrecognized DECRQSS")
	}

	// XTGETTCAP (DCS + q ... ST) is ignored as in tmux 3.8.
	_, _ = term.Write([]byte("\x1bP+q544e\x1b\\plain"))
	if got := term.DrainReplies(); len(got) != 0 {
		t.Fatalf("expected no reply to XTGETTCAP, got %q", string(got))
	}
	if got := term.Screen.Capture(CaptureOptions{}); !strings.HasPrefix(got, "plain\n") {
		t.Fatalf("screen after XTGETTCAP = %q", got)
	}
}
