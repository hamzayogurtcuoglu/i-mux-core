package cmdlang

import (
	"reflect"
	"strings"
	"testing"
)

func words(t *testing.T, src string, o Options) [][]string {
	t.Helper()
	cmds, err := Parse(src, o)
	if err != nil {
		t.Fatalf("Parse(%q): %v", src, err)
	}
	out := [][]string{}
	for _, c := range cmds {
		out = append(out, c.Words())
	}
	return out
}

func TestParseWords(t *testing.T) {
	tests := []struct {
		name, src string
		want      [][]string
	}{
		{"plain", "set -g mouse on\n", [][]string{{"set", "-g", "mouse", "on"}}},
		{"no final newline", "set -g mouse on", [][]string{{"set", "-g", "mouse", "on"}}},
		{"semicolon", "set a b ; set c d", [][]string{{"set", "a", "b"}, {"set", "c", "d"}}},
		{"semicolon glued", "set a b; set c d;", [][]string{{"set", "a", "b"}, {"set", "c", "d"}}},
		{"escaped semicolon", `bind x send a \; send b`, [][]string{{"bind", "x", "send", "a", ";", "send", "b"}}},
		{"comment", "# a comment\nset a b # trailing\n", [][]string{{"set", "a", "b"}}},
		{"hash inside word", "set a b#c", [][]string{{"set", "a", "b#c"}}},
		{"single quotes", `set a 'x "y" \n $HOME'`, [][]string{{"set", "a", `x "y" \n $HOME`}}},
		{"double quotes", `set a "x \"y\" \t z"`, [][]string{{"set", "a", "x \"y\" \t z"}}},
		{"quote join", `set a x'y z'"w"`, [][]string{{"set", "a", "xy zw"}}},
		{"empty quotes", `display-menu "" ''`, [][]string{{"display-menu", "", ""}}},
		{"escapes", `set a \e\s\101\u00e9`, [][]string{{"set", "a", "\x1b A\u00e9"}}},
		{"line continuation", "set a \\\n  b", [][]string{{"set", "a", "b"}}},
		{
			"format in quotes", `set -g status-format[0] '#{?pane_active,#[fg=red],}#{pane_id}'`,
			[][]string{{"set", "-g", "status-format[0]", "#{?pane_active,#[fg=red],}#{pane_id}"}},
		},
		{"percent token", "send-keys %% 50%", [][]string{{"send-keys", "%%", "50%"}}},
		{"crlf", "set a b\r\nset c d\r\n", [][]string{{"set", "a", "b"}, {"set", "c", "d"}}},
		// tmux keeps the newline before the comment and the one after it (checked on tmux 3.6).
		{"quoted newline", "set a 'x\n   # gone\n   y'", [][]string{{"set", "a", "x\n\ny"}}},
		{"unknown escape", `set a \9\q`, [][]string{{"set", "a", "9q"}}},
		{"quoted newline format", "set a 'x\n   #{y}'", [][]string{{"set", "a", "x\n#{y}"}}},
		{"blank lines", "\n\n  \nset a b\n\n", [][]string{{"set", "a", "b"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := words(t, tt.src, Options{}); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseBlocks(t *testing.T) {
	src := "bind -n x {\n  display a\n  set b c ; select-pane -t =\n}\nset d e\n"
	cmds, err := Parse(src, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(cmds) != 2 {
		t.Fatalf("got %d commands", len(cmds))
	}
	b := cmds[0].Args[3]
	if !b.Block || len(b.Commands) != 3 {
		t.Fatalf("block = %+v", b)
	}
	if got := b.Commands[2].Words(); !reflect.DeepEqual(got, []string{"select-pane", "-t", "="}) {
		t.Fatalf("got %q", got)
	}
	if cmds[1].Line != 5 {
		t.Fatalf("line = %d, want 5", cmds[1].Line)
	}
	// One-line block, and a block followed by more arguments.
	got := words(t, "if -F 1 { display a } { display b }", Options{})
	want := [][]string{{"if", "-F", "1", "display a", "display b"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
	// Empty block.
	if got := words(t, "bind x {}", Options{}); !reflect.DeepEqual(got, [][]string{{"bind", "x", ""}}) {
		t.Fatalf("got %q", got)
	}
}

func TestParseConditions(t *testing.T) {
	src := `%if "#{==:#{version},3.6}"
set a 1
%elif #{b}
set a 2
%else
set a 3
%endif
set z 9
`
	var seen []string
	cond := func(v map[string]bool) Options {
		return Options{Cond: func(f string) bool {
			seen = append(seen, f)
			return v[f]
		}}
	}
	if got := words(t, src, cond(map[string]bool{"#{==:#{version},3.6}": true, "#{b}": true})); !reflect.DeepEqual(got, [][]string{{"set", "a", "1"}, {"set", "z", "9"}}) {
		t.Fatalf("if: got %q", got)
	}
	if !reflect.DeepEqual(seen, []string{"#{==:#{version},3.6}", "#{b}"}) {
		t.Fatalf("conditions seen: %q", seen)
	}
	if got := words(t, src, cond(map[string]bool{"#{b}": true})); !reflect.DeepEqual(got, [][]string{{"set", "a", "2"}, {"set", "z", "9"}}) {
		t.Fatalf("elif: got %q", got)
	}
	if got := words(t, src, cond(nil)); !reflect.DeepEqual(got, [][]string{{"set", "a", "3"}, {"set", "z", "9"}}) {
		t.Fatalf("else: got %q", got)
	}
	all := words(t, src, Options{AllBranches: true})
	if len(all) != 4 {
		t.Fatalf("all branches: got %q", all)
	}
	// Inline form, and nesting.
	got := words(t, "%if #{x} set a 1 %endif\n%if #{x}\n%if #{y}\nset b 2\n%endif\n%endif\n",
		Options{Cond: func(string) bool { return true }})
	if !reflect.DeepEqual(got, [][]string{{"set", "a", "1"}, {"set", "b", "2"}}) {
		t.Fatalf("inline/nested: got %q", got)
	}
}

func TestParseVariables(t *testing.T) {
	env := map[string]string{"HOME": "/h", "X": "x1"}
	var assigned []string
	o := Options{
		Env:  func(n string) (string, bool) { v, ok := env[n]; return v, ok },
		Home: func(u string) (string, bool) { return "/h" + u, true },
		Assign: func(n, v string, hidden bool) {
			assigned = append(assigned, n+"="+v+map[bool]string{true: " hidden"}[hidden])
		},
	}
	got := words(t, "A=1\n%hidden B=2\nsource ~/x \"$X/${X}\" '$X' $NOPE. $1 ~u/y\n", o)
	want := [][]string{{"source", "/h/x", "x1/x1", "$X", ".", "$1", "/hu/y"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
	if !reflect.DeepEqual(assigned, []string{"A=1", "B=2 hidden"}) {
		t.Fatalf("assigned %q", assigned)
	}
	// Analysis mode keeps them as written.
	if got := words(t, "source ~/x $X ${Y}", Options{}); !reflect.DeepEqual(got, [][]string{{"source", "~/x", "$X", "${Y}"}}) {
		t.Fatalf("got %q", got)
	}
}

func TestParseErrors(t *testing.T) {
	for _, src := range []string{
		"set a 'unterminated",
		"set a \"unterminated",
		"%if #{x}\nset a b\n",
		"%endif\n",
		"bind x { display a",
		"set a }",
		"%bogus\n",
		`set a \4`,
	} {
		if _, err := Parse(src, Options{}); err == nil {
			t.Errorf("Parse(%q): no error", src)
		}
	}
}

func TestFormatRoundTrip(t *testing.T) {
	srcs := []string{
		`bind -n x { display "a b" ; set c 'd;e' } ; set -g x "it's $HOME"`,
		"set a '' ; set b '#{?x,a,b}' ; set c \"tab\\there\"",
		`if -F '#{x}' { display a } { if 1 { display "b" } }`,
	}
	for _, src := range srcs {
		a, err := Parse(src, Options{})
		if err != nil {
			t.Fatal(err)
		}
		b, err := Parse(Format(a), Options{})
		if err != nil {
			t.Fatalf("reparse %q: %v", Format(a), err)
		}
		if !reflect.DeepEqual(a, b) && Format(a) != Format(b) {
			t.Fatalf("round trip of %q:\n%s\n%s", src, Format(a), Format(b))
		}
	}
}

func TestSplitArgv(t *testing.T) {
	got := SplitArgv([]string{"set", "-g", "a", "b", ";", "send", "x;", "display", `y\;`, ";"})
	want := [][]string{{"set", "-g", "a", "b"}, {"send", "x"}, {"display", "y;"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func FuzzParse(f *testing.F) {
	for _, s := range []string{
		"set -g a b\n", "bind x { display a }", "%if #{x}\nset a\n%else\nset b\n%endif",
		`"a\"b" 'c' \u00e9 $X ~`, "a;b;;c", "#{x}", "{ { } }",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		cmds, err := Parse(s, Options{AllBranches: true})
		if err != nil {
			return
		}
		// What parses formats back into something that parses.
		if _, err := Parse(Format(cmds), Options{AllBranches: true}); err != nil && !strings.Contains(s, "\x00") {
			t.Fatalf("Format(%q) = %q does not parse: %v", s, Format(cmds), err)
		}
	})
}
