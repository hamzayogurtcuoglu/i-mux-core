package difftest

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestSplitWords(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{"run a b", []string{"run", "a", "b"}},
		{`run 'a b' "c \"d\" \$x" e\ f`, []string{"run", "a b", `c "d" $x`, "e f"}},
		{`run '#{pane_id}' ; show`, []string{"run", "#{pane_id}", ";", "show"}},
		{`run "" ''`, []string{"run", "", ""}},
		{`run 'it''s'`, []string{"run", "its"}},
	}
	for _, tt := range tests {
		got, err := SplitWords(tt.in)
		if err != nil || !reflect.DeepEqual(got, tt.want) {
			t.Errorf("SplitWords(%q) = %q, %v; want %q", tt.in, got, err, tt.want)
		}
	}
	for _, bad := range []string{`run 'a`, `run "a`, "   "} {
		if _, err := SplitWords(bad); err == nil {
			t.Errorf("SplitWords(%q): no error", bad)
		}
	}
}

func writeScript(t *testing.T, text string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "s.txt")
	if err := os.WriteFile(p, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestParseScriptErrors(t *testing.T) {
	for _, text := range []string{
		"bogus x\n",
		"run\n",
		"run a\nconf x.conf\n",
		"size 80\n",
		"normalize '(' x\n",
		"wait '#{x}' 1\n",
		"capture\n",
		"screen\n",
		"screen 80x24 now\n",
		"skip-if-engine imux-core\n",
		"type c x\n",
		"client c 80x24\ntype c\n",
		"client c 80x24\ntype c '\\q'\n",
	} {
		if _, err := ParseScript(writeScript(t, text)); err == nil {
			t.Errorf("ParseScript(%q): no error", text)
		}
	}
	s, err := ParseScript(writeScript(t, "# c\n\nsize 10x5\nrun a 'b c'\nwait -t %0 '#{x}' == 1\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Steps) != 3 || s.Steps[1].Line != 4 || !reflect.DeepEqual(s.Steps[1].Args, []string{"a", "b c"}) {
		t.Fatalf("steps %+v", s.Steps)
	}
}

// typedText takes Go's escapes.
func TestTypedText(t *testing.T) {
	b, err := typedText(`q\r\x1b[A"`)
	if err != nil || string(b) != "q\r\x1b[A\"" {
		t.Fatalf("got %q, %v", b, err)
	}
}

func TestWithSize(t *testing.T) {
	got := withSize([]string{"new-session", "-d", ";", "set", "-x"}, 100, 30)
	if want := []string{"new-session", "-x", "100", "-y", "30", "-d", ";", "set", "-x"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q", got)
	}
	keep := []string{"new-session", "-d", "-x", "5"}
	if got := withSize(keep, 100, 30); !reflect.DeepEqual(got, keep) {
		t.Fatalf("got %q", got)
	}
	other := []string{"split-window", "-h"}
	if got := withSize(other, 100, 30); !reflect.DeepEqual(got, other) {
		t.Fatalf("got %q", got)
	}
}

// echo is a stub engine with known answers, keyed by the command's words.
func echo(name string, answers map[string]Result) *Func {
	return &Func{Label: name, Fn: func(args []string) Result {
		if r, ok := answers[strings.Join(args, " ")]; ok {
			return r
		}
		return Result{Stderr: "unknown command: " + strings.Join(args, " "), Code: 1}
	}}
}

const stubScript = `size 10x5
normalize 'pid [0-9]+' 'pid N'
run new-session -d -s x
run display-message -p 'pid #{pid}'
wait -t %0 '#{cursor_y}' == 1
capture %0
skip-if-engine other not this one
`

func stubAnswers(pid, captured string) map[string]Result {
	return map[string]Result{
		"new-session -x 10 -y 5 -d -s x":       {},
		"display-message -p pid #{pid}":        {Stdout: "pid " + pid + "\n"},
		"display-message -p -t %0 #{cursor_y}": {Stdout: "1\n"},
		"capture-pane -p -e -t %0":             {Stdout: captured},
	}
}

func TestRunPairSame(t *testing.T) {
	p := writeScript(t, stubScript)
	a := echo("a", stubAnswers("123", "hi\n"))
	b := echo("b", stubAnswers("456", "hi\n")) // the pid differs, and is normalized
	diffs, skipped, err := RunPair(context.Background(), p, a, b)
	if err != nil || skipped != "" || len(diffs) != 0 {
		t.Fatalf("diffs %v, skipped %q, err %v", diffs, skipped, err)
	}
}

func TestRunPairPlanted(t *testing.T) {
	p := writeScript(t, stubScript)
	a := echo("tmux", stubAnswers("1", "hi\n"))
	b := echo("imux-core", stubAnswers("1", "ho\n"))
	diffs, _, err := RunPair(context.Background(), p, a, b)
	if err != nil {
		t.Fatal(err)
	}
	if len(diffs) != 1 || diffs[0].Step.Line != 6 || diffs[0].What != "stdout" {
		t.Fatalf("diffs %v", diffs)
	}
	msg := diffs[0].String()
	for _, want := range []string{p + ":6: capture %0", "stdout differs", `tmux:      "hi\n"`, `imux-core: "ho\n"`} {
		if !strings.Contains(msg, want) {
			t.Errorf("message lacks %q:\n%s", want, msg)
		}
	}
}

func TestRunPairExitCodeAndWait(t *testing.T) {
	old := WaitTimeout
	WaitTimeout = 50 * 1e6 // 50 ms
	defer func() { WaitTimeout = old }()
	p := writeScript(t, stubScript)
	ans := stubAnswers("1", "hi\n")
	ans["new-session -x 10 -y 5 -d -s x"] = Result{Code: 1}
	ans["display-message -p -t %0 #{cursor_y}"] = Result{Stdout: "0\n"}
	diffs, _, err := RunPair(context.Background(), p, echo("a", stubAnswers("1", "hi\n")), echo("b", ans))
	if err != nil {
		t.Fatal(err)
	}
	var what []string
	for _, d := range diffs {
		what = append(what, d.What)
	}
	if !reflect.DeepEqual(what, []string{"exit code", "stdout", "stderr"}) {
		t.Fatalf("diffs %v", diffs)
	}
	if !strings.Contains(diffs[2].B, "timed out") {
		t.Fatalf("timeout not reported: %q", diffs[2].B)
	}
}

func TestRunPairSkip(t *testing.T) {
	p := writeScript(t, stubScript)
	_, skipped, err := RunPair(context.Background(), p, echo("a", nil), echo("other", nil))
	if err != nil || skipped != "not this one" {
		t.Fatalf("skipped %q, err %v", skipped, err)
	}
}

func TestNewEnv(t *testing.T) {
	env, err := NewEnv()
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(env.Dir)
	if len(env.Socket) > 100 {
		t.Errorf("socket path too long for a Unix socket: %d bytes", len(env.Socket))
	}
	vars := strings.Join(env.Vars, "\n")
	for _, want := range []string{"HOME=" + env.Dir, "TMUX_TMPDIR=" + env.Dir, "TERM=xterm-256color", "LANG=C.UTF-8", "TZ=UTC"} {
		if !strings.Contains(vars, want) {
			t.Errorf("environment lacks %q", want)
		}
	}
	for _, v := range env.Vars {
		if strings.HasPrefix(v, "TMUX=") || strings.HasPrefix(v, "TMUX_PANE=") {
			t.Errorf("%s leaks into the environment", v)
		}
	}
}

// dumpCells splits a Dump line into cells: styles in braces, escaped braces, wide characters
// taking two cells and combining characters joining the one before.
func TestDumpCells(t *testing.T) {
	got := dumpCells("a{fg=0x1}b\\{日e\u0301{}c")
	want := []screenCell{
		{"a", "{}"},
		{"b", "{fg=0x1}"},
		{"{", "{fg=0x1}"},
		{"日", "{fg=0x1}"},
		{"", "{fg=0x1}"},
		{"e\u0301", "{fg=0x1}"},
		{"c", "{}"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("dumpCells:\n got %q\nwant %q", got, want)
	}
}
