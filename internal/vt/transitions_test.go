package vt

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"
)

// expectedTables is tmux 3.8's input.c state tables, written out a second time, by hand and in
// another form, so the test does not just read back the parser's own tables. Each line is
// "first-last action next"; "-" keeps the state; "*" lines are INPUT_STATE_ANYWHERE.
var expectedTables = map[string]string{
	"ground": `* 00-17 c0 - | 19-19 c0 - | 1c-1f c0 - | 20-7e print - | 7f-7f none - | 80-ff topbit -`,
	"esc_enter": `* 00-17 c0 - | 19-19 c0 - | 1c-1f c0 - | 20-2f inter esc_intermediate | 30-4f esc ground |
		50-50 none dcs_enter | 51-57 esc ground | 58-58 none consume_st | 59-5a esc ground |
		5b-5b none csi_enter | 5c-5c esc ground | 5d-5d none osc_string | 5e-5e none consume_st |
		5f-5f none apc_string | 60-6a esc ground | 6b-6b none rename_string | 6c-7e esc ground | 7f-ff none -`,
	"esc_intermediate": `* 00-17 c0 - | 19-19 c0 - | 1c-1f c0 - | 20-2f inter - | 30-7e esc ground | 7f-ff none -`,
	"csi_enter": `* 00-17 c0 - | 19-19 c0 - | 1c-1f c0 - | 20-2f inter csi_intermediate | 30-3b param csi_parameter |
		3c-3f inter csi_parameter | 40-7e csi ground | 7f-ff none -`,
	"csi_parameter": `* 00-17 c0 - | 19-19 c0 - | 1c-1f c0 - | 20-2f inter csi_intermediate | 30-3b param - |
		3c-3f none csi_ignore | 40-7e csi ground | 7f-ff none -`,
	"csi_intermediate": `* 00-17 c0 - | 19-19 c0 - | 1c-1f c0 - | 20-2f inter - | 30-3f none csi_ignore | 40-7e csi ground | 7f-ff none -`,
	"csi_ignore":       `* 00-17 c0 - | 19-19 c0 - | 1c-1f c0 - | 20-3f none - | 40-7e none ground | 7f-ff none -`,
	"dcs_enter": `* 00-17 none - | 19-19 none - | 1c-1f none - | 20-2f inter dcs_intermediate | 30-39 param dcs_parameter |
		3a-3a none dcs_ignore | 3b-3b param dcs_parameter | 3c-3f inter dcs_parameter | 40-7e input dcs_handler | 7f-ff none -`,
	"dcs_parameter": `* 00-17 none - | 19-19 none - | 1c-1f none - | 20-2f inter dcs_intermediate | 30-39 param - |
		3a-3a none dcs_ignore | 3b-3b param - | 3c-3f none dcs_ignore | 40-7e input dcs_handler | 7f-ff none -`,
	"dcs_intermediate": `* 00-17 none - | 19-19 none - | 1c-1f none - | 20-2f inter - | 30-3f none dcs_ignore | 40-7e input dcs_handler | 7f-ff none -`,
	"dcs_handler":      `00-1a input - | 1b-1b none dcs_escape | 1c-ff input -`,
	"dcs_escape":       `00-5b input dcs_handler | 5c-5c dcs ground | 5d-ff input dcs_handler`,
	"dcs_ignore":       `* 00-17 none - | 19-19 none - | 1c-1f none - | 20-ff none -`,
	"osc_string":       `* 00-06 none - | 07-07 bel ground | 08-17 none - | 19-19 none - | 1c-1f none - | 20-ff input -`,
	"apc_string":       `* 00-17 none - | 19-19 none - | 1c-1f none - | 20-ff input -`,
	"rename_string":    `* 00-17 none - | 19-19 none - | 1c-1f none - | 20-ff input -`,
	"consume_st":       `* 00-17 none - | 19-19 none - | 1c-1f none - | 20-ff none -`,
}

// prefixes bring a new parser into each state.
var prefixes = map[string]string{
	"ground": "", "esc_enter": "\x1b", "esc_intermediate": "\x1b ", "csi_enter": "\x1b[",
	"csi_parameter": "\x1b[1", "csi_intermediate": "\x1b[ ", "csi_ignore": "\x1b[1?",
	"dcs_enter": "\x1bP", "dcs_parameter": "\x1bP1", "dcs_intermediate": "\x1bP ",
	"dcs_handler": "\x1bPq", "dcs_escape": "\x1bPq\x1b", "dcs_ignore": "\x1bP:",
	"osc_string": "\x1b]", "apc_string": "\x1b_", "rename_string": "\x1bk", "consume_st": "\x1bX",
}

type expectation struct{ action, next string }

func parseExpected(t *testing.T, state string) [256]expectation {
	t.Helper()
	var out [256]expectation
	set := [256]bool{}
	spec := expectedTables[state]
	anywhere := strings.HasPrefix(spec, "* ")
	spec = strings.TrimPrefix(spec, "* ")
	if anywhere {
		out[0x18], set[0x18] = expectation{"c0", "ground"}, true
		out[0x1a], set[0x1a] = expectation{"c0", "ground"}, true
		out[0x1b], set[0x1b] = expectation{"none", "esc_enter"}, true
	}
	for _, part := range strings.Split(spec, "|") {
		f := strings.Fields(part)
		if len(f) != 3 {
			t.Fatalf("%s: bad spec %q", state, part)
		}
		lo, _ := strconv.ParseUint(f[0][:2], 16, 8)
		hi, _ := strconv.ParseUint(f[0][3:], 16, 8)
		for c := lo; c <= hi; c++ {
			if set[c] {
				continue // the first matching range wins, as in tmux
			}
			next := f[2]
			if next == "-" {
				next = state
			}
			out[c], set[c] = expectation{f[1], next}, true
		}
	}
	for c, ok := range set {
		if !ok {
			t.Fatalf("%s: no expectation for %#x", state, c)
		}
	}
	return out
}

// TestTransitions checks every byte in every state: the state the parser goes to, and what the
// handler sees.
func TestTransitions(t *testing.T) {
	if len(expectedTables) != int(numStates) || len(prefixes) != int(numStates) {
		t.Fatalf("%d expected tables and %d prefixes for %d states", len(expectedTables), len(prefixes), numStates)
	}
	for state := range expectedTables {
		exp := parseExpected(t, state)
		for c := 0; c < 256; c++ {
			r := &recorder{}
			p := NewParser(r)
			p.Write([]byte(prefixes[state]))
			if p.State() != state {
				t.Fatalf("prefix %q gives %s, not %s", prefixes[state], p.State(), state)
			}
			r.out = nil
			inter, param, input := p.interLen, p.paramLen, len(p.input)
			p.Write([]byte{byte(c)})
			e := exp[c]
			where := fmt.Sprintf("%s + %#02x", state, c)
			if p.State() != e.next {
				t.Errorf("%s: state %s, want %s", where, p.State(), e.next)
			}
			checkAction(t, where, e, byte(c), r.out, p, inter, param, input)
		}
	}
}

func checkAction(t *testing.T, where string, e expectation, c byte, out []string, p *Parser, inter, param, input int) {
	t.Helper()
	// Leaving an OSC, APC or rename string dispatches it.
	var exitDispatch []string
	for _, o := range out {
		if strings.HasPrefix(o, "osc ") || strings.HasPrefix(o, "apc ") || strings.HasPrefix(o, "rename ") {
			exitDispatch = append(exitDispatch, o)
		}
	}
	rest := len(out) - len(exitDispatch)
	switch e.action {
	case "c0":
		// The control runs, then leaving a string dispatches it, as in tmux's input_parse.
		if rest != 1 || out[0] != fmt.Sprintf("exec %#x", c) {
			t.Errorf("%s: c0: got %q", where, out)
		}
	case "print":
		if rest != 1 || out[0] != fmt.Sprintf("print %c", c) {
			t.Errorf("%s: print: got %q", where, out)
		}
	case "topbit":
		lead := c >= 0xc2 && c <= 0xf4
		if lead && (len(out) != 0 || !p.utf8On) {
			t.Errorf("%s: a lead byte should start UTF-8: got %q", where, out)
		}
		if !lead && (len(out) != 1 || !strings.HasSuffix(out[0], " true")) {
			t.Errorf("%s: a byte that cannot lead should print U+FFFD: got %q", where, out)
		}
	case "inter":
		if p.interLen != inter+1 || rest != 0 {
			t.Errorf("%s: intermediate not collected (%d → %d), %q", where, inter, p.interLen, out)
		}
	case "param":
		if p.paramLen != param+1 || rest != 0 {
			t.Errorf("%s: parameter not collected (%d → %d), %q", where, param, p.paramLen, out)
		}
	case "input":
		if e.next == p.State() && len(p.input) != input+1 {
			t.Errorf("%s: string byte not collected (%d → %d)", where, input, len(p.input))
		}
	case "esc":
		if rest != 1 || !strings.HasPrefix(out[0], "esc ") {
			t.Errorf("%s: esc dispatch: got %q", where, out)
		}
	case "csi":
		if rest != 1 || !strings.HasPrefix(out[0], "csi ") {
			t.Errorf("%s: csi dispatch: got %q", where, out)
		}
	case "dcs":
		if rest != 1 || !strings.HasPrefix(out[0], "dcs ") {
			t.Errorf("%s: dcs dispatch: got %q", where, out)
		}
	case "bel":
		if len(out) != 1 || !strings.HasSuffix(out[0], "bel=true") {
			t.Errorf("%s: OSC ended by BEL: got %q", where, out)
		}
	case "none":
		if rest != 0 {
			t.Errorf("%s: nothing should happen: got %q", where, out)
		}
	default:
		t.Fatalf("%s: unknown action %q", where, e.action)
	}
}

// TestDCSWithBadParameters: tmux dispatches a DCS whose parameters do not parse.
func TestDCSWithBadParameters(t *testing.T) {
	got := parse("\x1bP99999999999$qm\x1b\\")
	if len(got) == 0 || !strings.HasPrefix(got[0], "dcs ") {
		t.Fatalf("got %q", got)
	}
}

// TestDiscard: a sequence that overflows a buffer is dropped whole, and the next one parses.
func TestDiscard(t *testing.T) {
	got := parse("\x1b[" + strings.Repeat("9", 70) + "m\x1b[1m")
	if len(got) != 1 || got[0] != `csi "" [1] m` {
		t.Fatalf("got %q", got)
	}
	got = parse("\x1b ! \" #A\x1b(B")
	if len(got) != 1 || got[0] != `esc "(" B` {
		t.Fatalf("got %q", got)
	}
}

// TestGroundTimer: an unterminated string is given up on when the pane's reader calls Expire.
func TestGroundTimer(t *testing.T) {
	if GroundTimeout != 5*time.Second {
		t.Fatalf("GroundTimeout = %v, tmux's is 5 s", GroundTimeout)
	}
	r := &recorder{}
	p := NewParser(r)
	p.Write([]byte("\x1b]2;never ends"))
	if !p.InString() {
		t.Fatal("not in a string")
	}
	if !p.Expire() || p.State() != "ground" || p.InString() {
		t.Fatalf("after Expire: state %s, in string %v", p.State(), p.InString())
	}
	p.Write([]byte("ok"))
	if len(r.out) != 2 || r.out[0] != "print o" {
		t.Fatalf("got %q", r.out)
	}
	// A string that ends, and other sequences, stop the timer.
	for _, s := range []string{"\x1b]2;x\x07", "\x1b]2;x\x1b\\", "\x1bPq\x1b\\", "\x1b[1m", "abc"} {
		p := NewParser(&recorder{})
		p.Write([]byte(s))
		if p.InString() || p.Expire() {
			t.Errorf("%q: the timer still runs", s)
		}
	}
	// Each string kind starts it.
	for _, s := range []string{"\x1b]", "\x1bP", "\x1bP1;2", "\x1bPq", "\x1b_", "\x1bk", "\x1bX", "\x1b^"} {
		p := NewParser(&recorder{})
		p.Write([]byte(s))
		if !p.InString() {
			t.Errorf("%q: no timer", s)
		}
	}
}
