package status

import (
	"strings"
	"testing"
	"time"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/format"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/model"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/options"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/style"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/vt"
)

func newOptions(t *testing.T) *options.Globals {
	t.Helper()
	return options.NewGlobals()
}

func TestDrawBlank(t *testing.T) {
	g := newOptions(t)
	g.Session.SetNumber("status", 1)
	g.Session.SetNumber("status-position", 1)
	g.Session.SetString("status-style", false, "bg=colour4,fg=colour15")
	if err := g.Session.Get("status-format").ArraySet("0", strPtr(""), false); err != nil {
		t.Fatalf("array set: %v", err)
	}

	tr := model.NewTree()
	ctx := &format.Context{Tree: tr}
	ctx.Defaults(nil, nil, nil, nil)

	lines := Draw(ctx, g.Session, 80)
	if len(lines) != 1 {
		t.Fatalf("got %d lines", len(lines))
	}
	if len(lines[0].Cells) != 80 {
		t.Fatalf("got %d cells", len(lines[0].Cells))
	}
	for i, c := range lines[0].Cells {
		if c.Data != " " {
			t.Fatalf("cell %d data %q", i, c.Data)
		}
	}
}

func TestDrawSimple(t *testing.T) {
	g := newOptions(t)
	g.Session.SetNumber("status", 1)

	tr := model.NewTree()
	tr.Options.Session.SetString("status-left", false, "hello")
	tr.Options.Session.SetString("status-style", false, "bg=colour4,fg=colour15")
	sess, _ := tr.NewSession("test", "", time.Now())
	ctx := &format.Context{Tree: tr}
	ctx.Defaults(nil, sess, nil, nil)

	lines := Draw(ctx, g.Session, 80)
	if len(lines) != 1 {
		t.Fatalf("got %d lines", len(lines))
	}
	line := lines[0].Cells
	if line[0].Data != "h" {
		t.Fatalf("first cell %q", line[0].Data)
	}
}

func strPtr(s string) *string { return &s }

// line draws expanded on a line of width cells over the default cell and returns its text
// and ranges.
func line(expanded string, width int) (string, []Range) {
	cells := make([]vt.Cell, width)
	for i := range cells {
		cells[i] = vt.DefaultCell
	}
	var ranges []Range
	drawFormat(cells, vt.DefaultCell, width, expanded, &ranges, false)
	var b strings.Builder
	for _, c := range cells {
		if c.Flags&vt.FlagPadding == 0 {
			b.WriteString(c.Data)
		}
	}
	return b.String(), ranges
}

// The list markers only show on the sides the list was cut, and the list is cut around its
// focus (format_draw_put_list).
func TestDrawListMarkers(t *testing.T) {
	const list = "#[list=on align=left]#[list=left-marker]<#[list=right-marker]>#[list=on]"
	tests := []struct {
		in    string
		width int
		want  string
	}{
		{list + "abc", 5, "abc  "},
		{list + "abcdefghij", 5, "abcd>"},
		{list + "abcdefgh#[list=focus]ij#[list=on]", 5, "<ghij"},
		{list + "abcd#[list=focus]ef#[list=on]ghij", 5, "<efg>"},
		{"L" + list + "abc#[nolist]R", 7, "LabcR  "},
		{"#[align=right]R" + list + "abc", 6, "abc  R"},
	}
	for _, tt := range tests {
		if got, _ := line(tt.in, tt.width); got != tt.want {
			t.Errorf("%q at %d: got %q, want %q", tt.in, tt.width, got, tt.want)
		}
	}
}

// Left, centre, right and absolute-centre without a list, and the centre is cut first.
func TestDrawAlign(t *testing.T) {
	tests := []struct {
		in    string
		width int
		want  string
	}{
		{"#[align=left]ab#[align=right]cd", 8, "ab    cd"},
		{"#[align=left]ab#[align=centre]xy#[align=right]cd", 10, "ab  xy  cd"},
		{"#[align=left]ab#[align=centre]wxyz#[align=right]cd", 6, "abxycd"},
		{"#[align=absolute-centre]xy", 6, "  xy  "},
		// Then the right, then the left.
		{"#[align=left]abcdef#[align=right]gh", 5, "abcde"},
		{"#[align=left]abc#[align=right]defgh", 5, "abcgh"}, // the right keeps its end
	}
	for _, tt := range tests {
		if got, _ := line(tt.in, tt.width); got != tt.want {
			t.Errorf("%q at %d: got %q, want %q", tt.in, tt.width, got, tt.want)
		}
	}
}

// #s before [ are escapes: an even number is all text, an odd number ends with a style.
func TestDrawHashes(t *testing.T) {
	tests := []struct{ in, want string }{
		{"a#b", "a#b"},
		{"a##b", "a#b"},
		{"##[x]", "#[x]"},
		{"###[fg=red]x", "#x"},
		{"#[ignore]#[fg=red]x", "#[fg=red]x"},
	}
	for _, tt := range tests {
		if got, _ := line(tt.in, 12); strings.TrimRight(got, " ") != tt.want {
			t.Errorf("%q: got %q, want %q", tt.in, got, tt.want)
		}
	}
}

// Ranges are where their text ends up, cut with it.
func TestDrawRanges(t *testing.T) {
	in := "#[range=left]L#[norange]" +
		"#[list=on align=left]#[list=left-marker]<#[list=right-marker]>#[list=on]" +
		"#[range=window|1]one#[norange list=on] #[range=window|2 list=focus]two#[norange list=on]" +
		"#[nolist align=right range=right]R#[norange]"
	got, ranges := line(in, 12)
	if got != "Lone two   R" {
		t.Fatalf("got %q", got)
	}
	want := []Range{
		{Type: style.RangeLeft, Start: 0, End: 1},
		{Type: style.RangeWindow, Arg: 1, Start: 1, End: 4},
		{Type: style.RangeWindow, Arg: 2, Start: 5, End: 8},
		{Type: style.RangeRight, Start: 11, End: 12},
	}
	if len(ranges) != len(want) {
		t.Fatalf("ranges %+v", ranges)
	}
	for _, w := range want {
		found := false
		for _, r := range ranges {
			found = found || r == w
		}
		if !found {
			t.Errorf("no range %+v in %+v", w, ranges)
		}
	}
	l := Line{Ranges: ranges}
	if r := l.RangeAt(6); r == nil || r.Arg != 2 {
		t.Errorf("RangeAt(6) = %+v", r)
	}
	if r := l.RangeAt(9); r != nil {
		t.Errorf("RangeAt(9) = %+v", r)
	}

	// Cut to 6 cells, the focused window stays and the first is cut away.
	_, ranges = line(in, 6)
	for _, r := range ranges {
		if r.Type == style.RangeWindow && r.Arg == 1 && r.End > 2 {
			t.Errorf("window 1 not cut: %+v", r)
		}
	}
}

// push-default makes the current style the default until pop-default; fill colours the line.
func TestDrawDefaults(t *testing.T) {
	cells := make([]vt.Cell, 6)
	for i := range cells {
		cells[i] = vt.DefaultCell
	}
	drawFormat(cells, vt.DefaultCell, 6, "#[fg=red]#[push-default]a#[fg=blue]b#[default]c#[pop-default]#[default]d#[fill=colour4]", nil, false)
	red, blue := vt.ColorFromString("red"), vt.ColorFromString("blue")
	want := []vt.Color{red, blue, red, 8}
	for i, fg := range want {
		if cells[i].Fg != fg {
			t.Errorf("cell %d (%q): fg %#x, want %#x", i, cells[i].Data, cells[i].Fg, fg)
		}
	}
	if cells[5].Bg != vt.ColorFromString("colour4") {
		t.Errorf("fill: bg %#x", cells[5].Bg)
	}
}

// A wide character is not split at the edge of what is drawn.
func TestDrawWide(t *testing.T) {
	got, _ := line("ab中文", 5)
	if got != "ab中 " {
		t.Errorf("got %q", got)
	}
}

// Draw uses status-format's entries and leaves a line without one blank, in status-style.
func TestDrawLines(t *testing.T) {
	tr := model.NewTree()
	oo := tr.Options.Session
	oo.SetNumber("status", 3)
	if err := oo.Get("status-format").ArraySet("2", strPtr("x"), false); err != nil {
		t.Fatal(err)
	}
	sess, _ := tr.NewSession("s", "", time.Now())
	wl, _ := tr.NewWindow(sess, -1, 0, 80, 24)
	ctx := &format.Context{Tree: tr}
	ctx.Defaults(nil, sess, wl, nil)

	lines := Draw(ctx, oo, 20)
	if len(lines) != 3 {
		t.Fatalf("%d lines", len(lines))
	}
	if lines[0].Cells[0].Data != "[" || lines[0].Cells[0].Bg != vt.ColorFlagTheme|4 {
		t.Errorf("line 0 starts %+v", lines[0].Cells[0])
	}
	if lines[2].Cells[0].Data != "x" {
		t.Errorf("line 2 starts %q", lines[2].Cells[0].Data)
	}
	if err := oo.Get("status-format").ArraySet("1", nil, false); err != nil {
		t.Fatal(err)
	}
	if lines = Draw(ctx, oo, 20); lines[1].Cells[0].Data != " " || lines[1].Cells[0].Bg != vt.ColorFlagTheme|4 {
		t.Errorf("line 1 starts %+v", lines[1].Cells[0])
	}
}
