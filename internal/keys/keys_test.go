package keys

import "testing"

// Key names as tmux reads and prints them (key-string.c).
func TestNames(t *testing.T) {
	for _, tc := range []struct {
		in, out string
	}{
		{"a", "a"},
		{"C-a", "C-a"},
		{"^a", "C-a"},
		{"^A", "C-a"},
		{"M-v", "M-v"},
		{"C-M-x", "C-M-x"},
		{"M-C-x", "C-M-x"},
		{"S-Left", "S-Left"},
		{"C-\\", "C-\\"},
		{"Enter", "Enter"},
		{"enter", "Enter"},
		{"Escape", "Escape"},
		{"Space", "Space"},
		{"BSpace", "BSpace"},
		{"Tab", "Tab"},
		{"BTab", "BTab"},
		{"F1", "F1"},
		{"C-F12", "C-F12"},
		{"Insert", "IC"},
		{"Delete", "DC"},
		{"PageUp", "PPage"},
		{"PgDn", "NPage"},
		{"Home", "Home"},
		{"KP5", "KP5"},
		{"KPEnter", "KPEnter"},
		{"é", "é"},
		{"M-日", "M-日"},
		{"0x41", "A"},
		{"0x1", "[SOH]"},
		{"0xe9", "é"},
		{"User3", "User3"},
		{"MouseDown1Pane", "MouseDown1Pane"},
		{"SecondClick1Pane", "SecondClick1Pane"},
		{"DoubleClick1Pane", "DoubleClick1Pane"},
		{"TripleClick1Pane", "TripleClick1Pane"},
		{"MouseDown1Status", "MouseDown1Status"},
		{"MouseUp1Status", "MouseUp1Status"},
		{"MouseDown3Border", "MouseDown3Border"},
		{"MouseDrag1Border", "MouseDrag1Border"},
		{"MouseDragEnd1Pane", "MouseDragEnd1Pane"},
		{"WheelUpPane", "WheelUpPane"},
		{"WheelDownStatus", "WheelDownStatus"},
		{"MouseUp1StatusDefault", "MouseUp1StatusDefault"},
		{"None", "None"},
		{"Any", "Any"},
		{"[NUL]", "[NUL]"},
		{"bogus", "Unknown"},
		{"X-a", "Unknown"},
		{"C-", "Unknown"},
	} {
		if got := Parse(tc.in).String(); got != tc.out {
			t.Errorf("%q: %q, want %q", tc.in, got, tc.out)
		}
	}
	// Names tmux prints for keys that have no name of their own.
	for _, tc := range []struct {
		k    Key
		want string
	}{
		{0, "[NUL]"},
		{0x7f, "C-?"},
		{0xff, "\\377"},
		{'a' | Ctrl | Meta | Shift, "C-M-S-a"},
		{Up | Cursor | ImpliedMeta, "Up"},
		{PasteStart, "PasteStart"},
		{FocusIn, "FocusIn"},
		{ReportDarkTheme, "ReportDarkTheme"},
		{Mouse, "Mouse"},
		{MouseKey(TypeMouseMove, 0, LocationPane), "MouseMovePane"},
	} {
		if got := tc.k.String(); got != tc.want {
			t.Errorf("%#x: %q, want %q", uint64(tc.k), got, tc.want)
		}
	}
	if got := Name(Up|Cursor|ImpliedMeta, true); got != "Up[CI]" {
		t.Errorf("with flags: %q", got)
	}
}
