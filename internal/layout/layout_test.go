package layout

import (
	"strings"
	"testing"
)

// The layout's behaviour is checked against tmux by the scenarios in testdata/layout (replayed
// in internal/model). These tests cover what the scenarios do not reach.

func TestChecksum(t *testing.T) {
	// Values tmux 3.8 printed for these layouts (testdata/layout/*.golden).
	for s, want := range map[string]uint16{
		"80x24,0,0{40x24,0,0,0,39x24,41,0,1}":                              0x8205,
		"80x24,0,0{40x24,0,0,0,39x24,41,0[39x12,41,0,1,39x11,41,13,2]}":    0xd67e,
		"100x30,0,0[100x10,0,0,0,100x19,0,11{50x19,0,11,1,49x19,51,11,2}]": 0x2094,
	} {
		if got := Checksum(s); got != want {
			t.Errorf("Checksum(%q) = %04x, want %04x", s, got, want)
		}
	}
}

func TestParseErrors(t *testing.T) {
	for in, want := range map[string]string{
		"":                    "malformed layout header",
		"8205":                "malformed layout header",
		"zzzz,80x24,0,0,0":    "malformed layout header",
		"0000,80x24,0,0,0":    "invalid layout checksum",
		"b25d,80x24,0,0,0":    "",
		" \tb25d,80x24,0,0,0": "",
		DumpV1(&Cell{Type: WindowPane, Sx: 0, Sy: 24}):     "invalid layout",
		DumpV1(&Cell{Type: WindowPane, Sx: 10001, Sy: 24}): "invalid layout",
		`{"V":2}`:                     `key "L" not found`,
		`{"V":"2","L":{}}`:            `key "V" expected a number`,
		`{"V":2,"L":[]}`:              `key "L" expected an object`,
		`{"V":2,"L":{"t":"q"}}`:       `unknown cell type "q"`,
		`{"V":2,"L":{"t":"p","w":0}}`: "invalid width 0",
		`{"V":2,"L":{"t":"p","w":1,"h":1,"x":0,"y":-10001}}`:                                                                                              "invalid y-offset -10001",
		`{"V":2,"L":{"t":"p","w":1,"h":1,"x":0,"y":0}}`:                                                                                                   `key "i" not found`,
		`{"V":2,"L":{"t":"p","w":1,"h":1,"x":0,"y":0,"i":0,"c":[]}}`:                                                                                      "panes cannot have children",
		`{"V":2,"L":{"t":"h","w":1,"h":1,"x":0,"y":0,"c":[{"t":"p","w":1,"h":1,"x":0,"y":0,"i":0}]}}`:                                                     "nodes must have more than one child",
		`{"V":2,"L":{"t":"p","w":1,"h":1,"x":0,"y":0,"i":0,"a":1}}`:                                                                                       `key "a" expected a boolean`,
		`{"V":1,"L":{"t":"p","w":1,"h":1,"x":0,"y":0,"i":0}}`:                                                                                             "version mismatch",
		`{"V":2,"L":{"t":"h","w":3,"h":1,"x":0,"y":0,"c":[{"t":"p","w":1,"h":1,"x":0,"y":0,"i":0},{"t":"p","w":1,"h":1,"x":2,"y":0,"i":0}]}}`:             "duplicate pane index",
		`{"V":2,"L":{"t":"h","w":3,"h":1,"x":0,"y":0,"c":[{"t":"p","w":1,"h":1,"x":0,"y":0,"i":0,"l":0},{"t":"p","w":1,"h":1,"x":2,"y":0,"i":1,"l":0}]}}`: "duplicate last pane index",
	} {
		_, err := Parse(in)
		got := ""
		if err != nil {
			got = err.Error()
		}
		if got != want {
			t.Errorf("Parse(%q): %q, want %q", in, got, want)
		}
	}
}

func TestDumpRoundTrip(t *testing.T) {
	l := New(0, 120, 40)
	steps := []struct {
		pane, newPane int
		o             SplitOptions
	}{
		{0, 1, SplitOptions{Type: LeftRight, Size: -1}},
		{1, 2, SplitOptions{Type: TopBottom, Size: 10}},
		{0, 3, SplitOptions{Type: LeftRight, Size: 20, Before: true, Full: true}},
	}
	for _, s := range steps {
		if err := l.Split(s.pane, s.newPane, s.o); err != nil {
			t.Fatal(err)
		}
	}
	v1 := DumpV1(l.Root)
	p, err := Parse(v1)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Fit(4); err != nil {
		t.Fatal(err)
	}
	m := New(9, 1, 1)
	m.Apply(p, []int{3, 0, 1, 2})
	if got := DumpV1(m.Root); got != v1 {
		t.Errorf("round trip:\n%s\n%s", v1, got)
	}
	if !strings.HasPrefix(DumpV2(l.Root, func(int) PaneInfo { return PaneInfo{Last: -1} }), `{"V":2,"L":{"t":"h","w":120,"h":40`) {
		t.Error("v2 dump")
	}
	if err := l.Split(3, 4, SplitOptions{Type: LeftRight, Size: -1}); err != nil {
		t.Fatal(err)
	}
	// Version 1 allows a node with one child; dropping cells from it does not crash.
	for _, in := range []string{"80x24,0,0{80x24,0,0,0}", "80x24,0,0{20x24,0,0,0,20x24,21,0,1,37x24,42,0[37x24,42,0,2]}"} {
		p, err := Parse(checksummed(in))
		if err != nil {
			t.Fatalf("%s: %v", in, err)
		}
		_ = p.Fit(1)
	}
	// A pane of width 1 cannot be split.
	l = New(0, 2, 1)
	if err := l.Split(0, 1, SplitOptions{Type: LeftRight, Size: -1}); err != ErrNoSpace {
		t.Errorf("split of a 2x1 pane: %v", err)
	}
}

func FuzzLayoutParse(f *testing.F) {
	f.Add("d67e,80x24,0,0{40x24,0,0,0,39x24,41,0[39x12,41,0,1,39x11,41,13,2]}")
	f.Add(`{"V":2,"L":{"t":"h","w":80,"h":24,"x":0,"y":0,"c":[{"t":"p","w":40,"h":24,"x":0,"y":0,"l":0,"i":0,"I":"%0"},{"t":"p","w":39,"h":24,"x":41,"y":0,"a":true,"i":1,"I":"%1"}]}}`)
	f.Fuzz(func(t *testing.T, s string) {
		p, err := Parse(s)
		if err != nil {
			return
		}
		for n := 1; n <= 4; n++ {
			q, _ := Parse(s)
			if q.Fit(n) != nil {
				continue
			}
			l := New(100, 1, 1)
			panes := make([]int, n)
			for i := range panes {
				panes[i] = i
			}
			l.Apply(q, panes)
			_ = DumpV1(l.Root)
		}
		_ = p
	})
}

func checksummed(s string) string { return strings.ToLower(hex4(Checksum(s))) + "," + s }

func hex4(v uint16) string {
	const digits = "0123456789abcdef"
	return string([]byte{digits[v>>12], digits[v>>8&15], digits[v>>4&15], digits[v&15]})
}

// The known differences K6 and K7 (COMPATIBILITY.md §13).
func TestParseKnownDifferences(t *testing.T) {
	for in, want := range map[string]string{
		`{"V":2,"L":{"t":"p","w":1,"h":1,"x":0,"y":0,"i":0,"z":0}}`: "floating panes are not supported by imux-core",
		`{"V":2,`: "invalid json",
	} {
		if _, err := Parse(in); err == nil || err.Error() != want {
			t.Errorf("Parse(%q): %v, want %q", in, err, want)
		}
	}
}
