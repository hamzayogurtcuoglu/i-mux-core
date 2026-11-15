package vt

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/vt/vtfixture"
)

func flag01(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

// fixtureState is the engine's side of a fixture: the same fields tmux's formats give.
func fixtureState(t *Terminal, cols, rows int) string {
	s := t.Screen
	shape := map[CursorStyle]string{CursorDefault: "default", CursorBlock: "block", CursorUnderline: "underline", CursorBar: "bar"}[s.CStyle]
	st := &vtfixture.State{
		Cols: cols, Rows: rows, CursorX: s.Cx, CursorY: s.Cy, HistorySize: s.Grid.HSize,
		Values: map[string]string{
			"cursor_flag":              flag01(s.Mode&ModeCursor != 0),
			"cursor_shape":             shape,
			"cursor_blinking":          flag01(s.Mode&ModeCursorBlinking != 0),
			"cursor_very_visible":      flag01(s.Mode&ModeCursorVeryVisible != 0),
			"alternate_on":             flag01(s.IsAlternate()),
			"insert_flag":              flag01(s.Mode&ModeInsert != 0),
			"keypad_cursor_flag":       flag01(s.Mode&ModeKCursor != 0),
			"keypad_flag":              flag01(s.Mode&ModeKKeypad != 0),
			"wrap_flag":                flag01(s.Mode&ModeWrap != 0),
			"origin_flag":              flag01(s.Mode&ModeOrigin != 0),
			"mouse_standard_flag":      flag01(s.Mode&ModeMouseStandard != 0),
			"mouse_button_flag":        flag01(s.Mode&ModeMouseButton != 0),
			"mouse_all_flag":           flag01(s.Mode&ModeMouseAll != 0),
			"mouse_sgr_flag":           flag01(s.Mode&ModeMouseSGR != 0),
			"bracket_paste_flag":       flag01(s.Mode&ModeBracketPaste != 0),
			"synchronized_output_flag": flag01(s.Mode&ModeSync != 0),
			"scroll_region_upper":      strconv.Itoa(s.Rupper),
			"scroll_region_lower":      strconv.Itoa(s.Rlower),
			"pane_title":               s.Title,
			"pane_path":                s.Path,
		},
		Screen: s.Capture(CaptureOptions{Sequences: true, KeepTrailing: true}),
		Joined: s.Capture(CaptureOptions{Join: true, StartAll: true, EndAll: true}),
	}
	if st.HistorySize > 0 {
		st.All = s.Capture(CaptureOptions{Sequences: true, KeepTrailing: true, StartAll: true, EndAll: true})
	}
	return vtfixture.Format(st)
}

// TestFixtures feeds every testdata/vt fixture to the emulator and compares the screen with
// the one tmux showed for it (cmd/vtfixture).
func TestFixtures(t *testing.T) {
	ins, err := filepath.Glob("../../testdata/vt/*.in")
	if err != nil || len(ins) == 0 {
		t.Fatalf("no fixtures: %v", err)
	}
	for _, in := range ins {
		name := strings.TrimSuffix(filepath.Base(in), ".in")
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(in)
			if err != nil {
				t.Fatal(err)
			}
			want, err := os.ReadFile(strings.TrimSuffix(in, ".in") + ".screen")
			if err != nil {
				t.Fatalf("no expected screen (run cmd/vtfixture): %v", err)
			}
			cols, rows := vtfixture.Size(in)
			term := NewTerminal(cols, rows, 2000)
			term.Screen.Title = vtfixture.InitialTitle // tmux starts a pane with the host's name
			term.Write(data)
			got := fixtureState(term, cols, rows)
			if got != string(want) {
				t.Errorf("screen differs from tmux's:\n%s", lineDiff(string(want), got))
			}
		})
	}
}

// lineDiff shows the lines that differ, quoted so escapes and spaces are visible.
func lineDiff(want, got string) string {
	w, g := strings.Split(want, "\n"), strings.Split(got, "\n")
	var b strings.Builder
	for i := 0; i < max(len(w), len(g)); i++ {
		var a, c string
		if i < len(w) {
			a = w[i]
		}
		if i < len(g) {
			c = g[i]
		}
		if a != c {
			b.WriteString("  line " + strconv.Itoa(i+1) + ":\n    tmux:   " + strconv.Quote(a) + "\n    engine: " + strconv.Quote(c) + "\n")
		}
	}
	return b.String()
}
