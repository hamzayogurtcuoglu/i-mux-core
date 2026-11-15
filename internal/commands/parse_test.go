package commands

import (
	"reflect"
	"testing"
)

// The messages below are tmux 3.8's, checked against the pinned tmux (the same cases run in
// testdata/diff/sessions.txt).

func TestFind(t *testing.T) {
	for _, tc := range []struct{ name, want, err string }{
		{"new", "new-session", ""},
		{"new-session", "new-session", ""},
		{"new-s", "new-session", ""},
		{"has", "has-session", ""},
		{"start", "start-server", ""},
		{"kill-ser", "kill-server", ""},
		{"lscm", "list-commands", ""},
		{"nosuch", "", "unknown command: nosuch"},
		{"ne", "", "ambiguous command: ne, could be: new-pane, new-session, new-window, next-layout, next-window"},
		{"kill-s", "", "ambiguous command: kill-s, could be: kill-server, kill-session"},
	} {
		got, err := Find(tc.name)
		switch {
		case tc.err != "":
			if err == nil || err.Error() != tc.err {
				t.Errorf("Find(%q) error %v, want %q", tc.name, err, tc.err)
			}
		case err != nil:
			t.Errorf("Find(%q): %v", tc.name, err)
		case got.Name != tc.want:
			t.Errorf("Find(%q) = %s, want %s", tc.name, got.Name, tc.want)
		}
	}
	if len(tmuxTable) != 92 {
		t.Errorf("%d commands in the table, tmux 3.8 has 92", len(tmuxTable))
	}
}

func TestParse(t *testing.T) {
	c, err := Parse([]string{"new", "-ds", "a", "-x80", "-e", "A=1", "-e", "B=2", "--", "-sh", "x"})
	if err != nil {
		t.Fatal(err)
	}
	if !c.Args.Has('d') || c.Args.Has('P') {
		t.Error("flags")
	}
	if v, _ := c.Args.Get('s'); v != "a" {
		t.Errorf("-s %q", v)
	}
	if v, _ := c.Args.Get('x'); v != "80" {
		t.Errorf("-x %q", v)
	}
	if got := c.Args.All('e'); !reflect.DeepEqual(got, []string{"A=1", "B=2"}) {
		t.Errorf("-e %q", got)
	}
	if !reflect.DeepEqual(c.Args.Strings(), []string{"-sh", "x"}) {
		t.Errorf("values %q", c.Args.Strings())
	}
	// A lone "-" is an argument; the last of a repeated flag wins.
	c, err = Parse([]string{"new", "-s", "a", "-s", "b", "-"})
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := c.Args.Get('s'); v != "b" || c.Args.Count('s') != 2 || !reflect.DeepEqual(c.Args.Strings(), []string{"-"}) {
		t.Errorf("-s %q, count %d, values %q", v, c.Args.Count('s'), c.Args.Strings())
	}

	for _, tc := range []struct {
		words []string
		err   string
	}{
		{[]string{"new", "-z"}, "command new-session: unknown flag -z"},
		{[]string{"has", "-t"}, "command has-session: -t expects an argument"},
		{[]string{"has", "extra"}, "command has-session: too many arguments (need at most 0)"},
		{[]string{"kill-session", "-t", "a", "extra"}, "command kill-session: too many arguments (need at most 0)"},
		{[]string{"rename-session"}, "command rename-session: too few arguments (need at least 1)"},
		{[]string{"has", "-%"}, "command has-session: invalid flag -%"},
		{[]string{"has", "-?"}, "usage: has-session [-t target-session]"},
		{nil, "no command"},
	} {
		if _, err := Parse(tc.words); err == nil || err.Error() != tc.err {
			t.Errorf("Parse(%q) error %v, want %q", tc.words, err, tc.err)
		}
	}
}

func TestParseOptionalValue(t *testing.T) {
	// A flag with an optional value ("c::") takes the next word unless it looks like a flag.
	a, err := ParseArgs("c::d", 0, -1, Strings([]string{"x", "-c", "-d"}), nil)
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := a.Get('c'); ok || !a.Has('c') || !a.Has('d') {
		t.Errorf("-c %q %v, -d %v", v, ok, a.Has('d'))
	}
	a, err = ParseArgs("c::d", 0, -1, Strings([]string{"x", "-c", "val"}), nil)
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := a.Get('c'); v != "val" {
		t.Errorf("-c %q", v)
	}
	if a, err = ParseArgs("c::", 0, -1, Strings([]string{"x", "-c"}), nil); err != nil || !a.Has('c') {
		t.Errorf("-c at the end: %v", err)
	}
}

func TestStartsServer(t *testing.T) {
	for _, tc := range []struct {
		cmds [][]string
		want bool
	}{
		{[][]string{{"new-session", "-d"}}, true},
		{[][]string{{"has-session"}}, false},
		{[][]string{{"kill-server"}}, false},
		{[][]string{{"start-server"}}, true},
		{[][]string{{"has"}, {"new", "-d"}}, true},
		{[][]string{{"new", "-z"}}, false}, // a list that does not parse starts nothing
		{[][]string{{"lsk"}}, true},
		{[][]string{{"attach"}}, true},
	} {
		if got := StartsServer(tc.cmds); got != tc.want {
			t.Errorf("StartsServer(%q) = %v", tc.cmds, got)
		}
	}
}

// The table's targets and flags, as tmux 3.8's cmd-*.c define them.
func TestTableTargets(t *testing.T) {
	for _, tc := range []struct {
		name           string
		source, target *TargetSpec
		flags          CmdFlags
	}{
		{"kill-server", nil, nil, 0},
		{"select-pane", nil, &TargetSpec{'t', FindPane, 0}, 0},
		{"has-session", nil, &TargetSpec{'t', FindSession, 0}, 0},
		{"new-session", nil, &TargetSpec{'t', FindSession, FindCanfail}, CmdStartserver},
		{"swap-pane", &TargetSpec{'s', FindPane, FindDefaultMarked}, &TargetSpec{'t', FindPane, 0}, 0},
		{"break-pane", &TargetSpec{'s', FindPane, 0}, &TargetSpec{'t', FindWindow, FindWindowIndex}, 0},
		{"switch-client", nil, nil, CmdReadonly | CmdClientCflag},
	} {
		s, err := Find(tc.name)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(s.Source, tc.source) || !reflect.DeepEqual(s.Target, tc.target) || s.Flags != tc.flags {
			t.Errorf("%s: source %+v target %+v flags %#x", tc.name, s.Source, s.Target, s.Flags)
		}
		if s.StartServer != (s.Flags&CmdStartserver != 0) {
			t.Errorf("%s: StartServer %v with flags %#x", tc.name, s.StartServer, s.Flags)
		}
	}
}

// A command line is one group, with aliases expanded, as tmux's cmd_parse_from_arguments.
func TestBuildArgv(t *testing.T) {
	list, err := BuildArgv([][]string{{"splitp", "-h"}, {"info"}, {"has", "-t", "a b"}}, DefaultAliases)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := list.Print(), `split-window -h ; show-messages -JT ; has-session -t "a b"`; got != want {
		t.Errorf("Print() = %q, want %q", got, want)
	}
	for _, c := range list.Commands {
		if c.Group != list.Commands[0].Group {
			t.Error("a command line is more than one group")
		}
	}
	if _, err := BuildArgv([][]string{{"has"}, {"nosuch"}}, DefaultAliases); err == nil || err.Error() != "unknown command: nosuch" {
		t.Errorf("error %v", err)
	}
}

func TestEscape(t *testing.T) {
	// What tmux 3.8 printed for these (testdata/cmdlang/quotes.golden).
	for in, want := range map[string]string{
		"":        "''",
		"plain":   "plain",
		"a b":     `"a b"`,
		";":       `\;`,
		"~":       `\~`,
		"~/z":     `\~/z`,
		"a~b":     "a~b",
		"$":       `\$`,
		"$x":      `"\$x"`,
		"it's":    `"it's"`,
		`say "x"`: `"say \"x\""`,
		`"x"`:     `'"x"'`,
		"\a":      `\a`,
		"\x1b[1m": `\033[1m`,
		"é":       "é",
	} {
		if got := Escape(in); got != want {
			t.Errorf("Escape(%q) = %s, want %s", in, got, want)
		}
	}
}
