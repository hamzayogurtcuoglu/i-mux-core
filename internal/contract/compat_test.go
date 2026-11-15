package contract

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// TestCompatibilityNamesExpected checks that COMPATIBILITY.md names everything in
// contract/expected.json, so the two cannot drift apart.
func TestCompatibilityNamesExpected(t *testing.T) {
	doc, err := os.ReadFile("../../COMPATIBILITY.md")
	if err != nil {
		t.Fatal(err)
	}
	text := string(doc)
	b, err := os.ReadFile("../../contract/expected.json")
	if err != nil {
		t.Fatal(err)
	}
	var exp Report
	if err := json.Unmarshal(b, &exp); err != nil {
		t.Fatal(err)
	}
	mention := func(what, needle string) {
		t.Helper()
		if !strings.Contains(text, needle) {
			t.Errorf("COMPATIBILITY.md does not name %s (%q)", what, needle)
		}
	}
	// Commands: a row of the §3 table, with every flag and alias in it.
	for cmd, flags := range exp.Commands {
		row := ""
		for _, line := range strings.Split(text, "\n") {
			if strings.HasPrefix(line, "| `"+cmd+"`") {
				row = line
				break
			}
		}
		if row == "" {
			t.Errorf("COMPATIBILITY.md §3 has no row for %s", cmd)
			continue
		}
		cells := strings.SplitN(row, "|", 4)
		for f := range flags {
			switch {
			case strings.HasPrefix(f, "alias "):
				if !strings.Contains(cells[1], "[`"+strings.TrimPrefix(f, "alias ")+"`]") {
					t.Errorf("%s: alias %q not in its row", cmd, f)
				}
			case strings.HasPrefix(f, "-"):
				if !strings.Contains(" "+strings.NewReplacer("`", " ", ",", " ").Replace(cells[2])+" ", " "+f+" ") {
					t.Errorf("%s: flag %s not in its row: %s", cmd, f, cells[2])
				}
			}
		}
	}
	for v := range exp.Variables {
		mention("variable "+v, v)
	}
	modifiers := map[string]string{
		"==": "#{==:", "!=": "#{!=:", "||": "#{||:", "&&": "#{&&:", "m": "#{m:", "e|+": "#{e|+:",
		"e|<": "#{e|<:", "P": "#{P:", "W": "#{W:", "p/fmt/": "#{p/N/:", "E": "#{E:", "T": "#{T:",
	}
	for m := range exp.Modifiers {
		needle, ok := modifiers[m]
		if !ok {
			t.Errorf("modifier %q: add how COMPATIBILITY.md writes it to this test", m)
			continue
		}
		mention("modifier "+m, needle)
	}
	for a := range exp.Aliases {
		mention("alias "+a, a)
	}
	colours := map[string]string{
		"colourN": "colourN", "#rrggbb": "#rrggbb", "default": "default", "fmt": "#{@imux_c_",
		"name": "colour names",
	}
	for s := range exp.Styles {
		key, value, ok := strings.Cut(s, "=")
		if !ok {
			mention("style "+s, s)
			continue
		}
		if c, isColour := colours[value]; isColour {
			mention("style "+s, key)
			mention("style "+s, c)
			continue
		}
		mention("style "+s, s)
	}
	for s := range exp.Strftime {
		mention("strftime "+s, s)
	}
	for o := range exp.Options {
		mention("option "+o, o)
	}
	for o := range exp.UserOptions {
		mention("user option "+o, o)
	}
	for h := range exp.Hooks {
		mention("hook "+h, h)
	}
	for tb := range exp.Tables {
		mention("table "+tb, tb)
	}
	for k := range exp.Keys {
		mention("key "+k, k)
	}
	for k := range exp.SentKeys {
		if k == "(text)" {
			mention("text sent with send-keys", "literal text")
			continue
		}
		mention("sent key "+k, k)
	}
	for c := range exp.CopyCommands {
		mention("copy command "+c, c)
	}
	targets := map[string]string{
		"%N": "`%N`", "@N": "`@N`", "=name": "`=name`", "=session:window": "`=name:index`",
		"= (mouse)": "| `=` |", "client tty": "/dev/ttys", "tty": "/dev/ttys", "name": "| `name`",
		"session:window": "`:index`", "{last}": "`{last}`",
	}
	for f := range exp.Targets {
		needle, ok := targets[f]
		if !ok {
			t.Errorf("target form %q: add how COMPATIBILITY.md writes it to this test", f)
			continue
		}
		mention("target "+f, needle)
	}
	for f := range exp.CLI {
		mention("command-line flag "+f, "`"+f)
	}
}
