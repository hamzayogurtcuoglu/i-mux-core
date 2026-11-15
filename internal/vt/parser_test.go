package vt

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// recorder writes every action as a line of text.
type recorder struct{ out []string }

func (r *recorder) add(format string, a ...any) { r.out = append(r.out, fmt.Sprintf(format, a...)) }

func (r *recorder) Print(run []byte) {
	// Runs may be split differently by different writes: record one action per byte.
	for _, c := range run {
		r.add("print %c", c)
	}
}
func (r *recorder) PrintUTF8(seq []byte, invalid bool)   { r.add("utf8 %x %v", seq, invalid) }
func (r *recorder) Execute(c byte)                       { r.add("exec %#x", c) }
func (r *recorder) EscDispatch(inter []byte, final byte) { r.add("esc %q %c", inter, final) }
func (r *recorder) CsiDispatch(p *Params, inter []byte, final byte) {
	r.add("csi %q %s %c", inter, describe(p), final)
}
func (r *recorder) OscDispatch(data []byte, bel bool) { r.add("osc %q bel=%v", data, bel) }
func (r *recorder) DcsDispatch(p *Params, inter []byte, data []byte) {
	r.add("dcs %q %s %q", inter, describe(p), data)
}
func (r *recorder) ApcDispatch(data []byte)    { r.add("apc %q", data) }
func (r *recorder) RenameDispatch(data []byte) { r.add("rename %q", data) }

func describe(p *Params) string {
	var parts []string
	for i := 0; i < p.Len(); i++ {
		switch p.Kind(i) {
		case ParamMissing:
			parts = append(parts, "_")
		case ParamNumber:
			parts = append(parts, fmt.Sprint(p.Get(i, 0, 0)))
		case ParamString:
			parts = append(parts, string(p.String(i)))
		}
	}
	return "[" + strings.Join(parts, ",") + "]"
}

func parse(s string) []string {
	r := &recorder{}
	NewParser(r).Write([]byte(s))
	return r.out
}

func TestParserBasics(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{"ab", []string{"print a", "print b"}},
		{"\x1b[1;2H", []string{`csi "" [1,2] H`}},
		{"\x1b[?1049h", []string{`csi "?" [1049] h`}},
		{"\x1b[>4;2m", []string{`csi ">" [4,2] m`}},
		{"\x1b[4:3m", []string{`csi "" [4:3] m`}},
		{"\x1b[;5H", []string{`csi "" [_,5] H`}},
		{"\x1b[ q", []string{`csi " " [] q`}},
		{"\x1b[1?m", nil}, // a private marker after a parameter: ignored
		{"\x1b]2;title\x07", []string{`osc "2;title" bel=true`}},
		{"\x1b]8;;http://x\x1b\\", []string{`osc "8;;http://x" bel=false`, `esc "" \`}},
		{"\x1bP+q544e\x1b\\", []string{`dcs "+" [] "q544e"`}},
		{"\x1b_hi\x1b\\", []string{`apc "hi"`, `esc "" \`}},
		{"\x1bkname\x1b\\", []string{`rename "name"`, `esc "" \`}},
		{"\x1b(B", []string{`esc "(" B`}},
		{"\x1b7\x1b8", []string{`esc "" 7`, `esc "" 8`}},
		{"a\nb", []string{"print a", "exec 0xa", "print b"}},
		{"\x1b[1\nH", []string{"exec 0xa", `csi "" [1] H`}}, // C0 inside a CSI runs at once
		{"\x1b[1\x18H", []string{"exec 0x18", "print H"}},   // CAN cancels the sequence
		{"é", []string{"utf8 c3a9 false"}},
		{"\xff", []string{"utf8 ff true"}},
		{"\xc3a", []string{"utf8 c3 true", "print a"}},
		{"\xe4\xc3\xa9", []string{"utf8 e4c3a9 true"}}, // tmux takes size bytes, then fails
		// A control stops UTF-8 (tmux's input_c0_dispatch); an escape sequence does not.
		{"\xc3\n\xa9", []string{"utf8 c3 true", "exec 0xa", "utf8 a9 true"}},
		{"\xc3\x1b[m\xa9", []string{`csi "" [] m`, "utf8 c3a9 false"}},
		{"\x1b[99999999999m", nil}, // above INT_MAX: ignored
	}
	for _, tt := range tests {
		if got := parse(tt.in); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("parse(%q) =\n  %q\nwant\n  %q", tt.in, got, tt.want)
		}
	}
}

func TestParserLimits(t *testing.T) {
	// tmux keeps 24 slots but fails when the 24th is filled: 23 parameters at most.
	if got := parse("\x1b[" + strings.Repeat("1;", 23) + "1m"); got != nil {
		t.Errorf("24 parameters: %q", got)
	}
	if got := parse("\x1b[" + strings.Repeat("1;", 22) + "1m"); len(got) != 1 {
		t.Errorf("23 parameters: %q", got)
	}
	if got := parse("\x1b[" + strings.Repeat("1", 64) + "m"); got != nil {
		t.Errorf("64 parameter bytes: %q", got)
	}
	if got := parse("\x1b ! \" #A"); got != nil {
		t.Errorf("four intermediates: %q", got)
	}
	if got := parse("\x1b]" + strings.Repeat("x", MaxStringLen) + "\x07"); got != nil {
		t.Errorf("an OSC over the limit was dispatched")
	}
}

// TestParserSplitWrites checks that writing a stream one byte at a time gives the same
// actions as writing it whole.
func TestParserSplitWrites(t *testing.T) {
	data := mixedStream(256 << 10)
	data = append(data, "\xe4\xc3\xa9\x1b[1;2;3m\x1bP+q\x1b\\\xc3\n\xa9\x1b]2;x\x07"...)
	whole := &recorder{}
	NewParser(whole).Write(data)
	split := &recorder{}
	p := NewParser(split)
	for i := range data {
		p.Write(data[i : i+1])
	}
	if !reflect.DeepEqual(whole.out, split.out) {
		for i := range whole.out {
			if i >= len(split.out) || whole.out[i] != split.out[i] {
				t.Fatalf("action %d: whole %q, split %q", i, whole.out[i], split.out[min(i, len(split.out)-1)])
			}
		}
		t.Fatalf("split has %d more actions", len(split.out)-len(whole.out))
	}
}

func FuzzParser(f *testing.F) {
	for _, s := range []string{"\x1b[1;2H", "\x1b]2;x\x07", "\x1bP+q\x1b\\", "é\xff", "\x1b[?1049h", "\x1b_\x1b\\"} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		p := NewParser(&countHandler{})
		// Write in two parts, at a point the input picks, to cover split sequences.
		cut := 0
		if len(b) > 0 {
			cut = int(b[0]) % (len(b) + 1)
		}
		p.Write(b[:cut])
		p.Write(b[cut:])
		// Bounded memory: no buffer outgrows tmux's limits.
		if len(p.input) >= MaxStringLen || p.interLen > maxIntermediate || p.paramLen > maxParamBytes {
			t.Fatalf("buffers: input %d, intermediates %d, parameters %d", len(p.input), p.interLen, p.paramLen)
		}
		if p.State() == "ground" && cap(p.input) > 32 {
			t.Fatalf("the ground state keeps a %d-byte buffer", cap(p.input))
		}
	})
}
