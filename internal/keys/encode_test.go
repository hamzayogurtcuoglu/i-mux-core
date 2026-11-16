package keys

import (
	"strconv"
	"testing"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/vt"
)

// Keys as a pane is sent them (input_key), by the pane's modes; TestKeysIntoPanes (difftest)
// checks every key against tmux, this table the cases in short.
func TestEncode(t *testing.T) {
	app := vt.ModeKCursor | vt.ModeKKeypad
	for _, tc := range []struct {
		key  string
		mode int
		want string
		ok   bool
	}{
		{"a", 0, "a", true},
		{"Enter", 0, "\r", true},
		{"Escape", 0, "\x1b", true},
		{"C-a", 0, "\x01", true},
		{"C-Space", 0, "\x00", true},
		{"C-\\", 0, "\x1c", true},
		{"M-v", 0, "\x1bv", true},
		{"C-M-a", 0, "\x1b\x01", true},
		{"é", 0, "é", true},
		{"BSpace", 0, "\x7f", true},
		{"M-BSpace", 0, "\x1b\x7f", true},
		{"C-BSpace", 0, "", false},
		{"C-é", 0, "é", true},
		{"C-1", 0, "1", true},
		{"Up", 0, "\x1b[A", true},
		{"Up", app, "\x1bOA", true},
		{"S-Up", app, "\x1b[1;2A", true},
		{"M-Left", 0, "\x1b[1;3D", true},
		{"C-Up", 0, "\x1b[1;5A", true},
		{"F1", 0, "\x1bOP", true},
		{"C-F5", 0, "\x1b[15;5~", true},
		{"Home", 0, "\x1b[1~", true},
		{"BTab", 0, "\x1b[Z", true},
		{"KP5", 0, "5", true},
		{"KP5", app, "\x1bOu", true},
		{"KPEnter", 0, "\n", true},
		{"C-v", vt.ModeKeysExtended, "\x16", true},
		{"C-Enter", vt.ModeKeysExtended, "\x1b[27;5;13~", true},
		{"C-v", vt.ModeKeysExtended2, "\x1b[27;5;118~", true},
		{"M-a", vt.ModeKeysExtended, "\x1ba", true},
		{"BTab", vt.ModeKeysExtended2, "\x1b[27;2;9~", true},
		{"C-é", vt.ModeKeysExtended2, "\x1b[27;5;233~", true},
	} {
		got, ok := Encode(Parse(tc.key), tc.mode, 0x7f, FormatXterm)
		if string(got) != tc.want || ok != tc.ok {
			t.Errorf("%s (mode %#x): %s %v, want %s %v", tc.key, tc.mode, strconv.Quote(string(got)), ok, strconv.Quote(tc.want), tc.ok)
		}
	}
	if got, _ := Encode(Parse("C-v"), vt.ModeKeysExtended2, 0x7f, FormatCSIu); string(got) != "\x1b[118;5u" {
		t.Errorf("csi-u: %q", got)
	}
	if got, _ := Encode(PasteStart, 0, 0x7f, FormatXterm); got != nil {
		t.Errorf("a paste bracket to a pane without bracketed paste: %q", got)
	}
	if got, _ := Encode(PasteStart, vt.ModeBracketPaste, 0x7f, FormatXterm); string(got) != "\x1b[200~" {
		t.Errorf("a paste bracket: %q", got)
	}
}
