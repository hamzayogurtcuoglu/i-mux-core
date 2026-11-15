package contract

import (
	"encoding/json"
	"sort"
)

// Set is a set of strings, written to JSON as a sorted list.
type Set map[string]bool

// Add adds s.
func (s Set) Add(v string) { s[v] = true }

// Sorted returns the members in order.
func (s Set) Sorted() []string {
	out := make([]string, 0, len(s))
	for k := range s {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// MarshalJSON writes the set as a sorted list.
func (s Set) MarshalJSON() ([]byte, error) { return json.Marshal(s.Sorted()) }

// UnmarshalJSON reads a list.
func (s *Set) UnmarshalJSON(b []byte) error {
	var l []string
	if err := json.Unmarshal(b, &l); err != nil {
		return err
	}
	*s = Set{}
	for _, v := range l {
		(*s)[v] = true
	}
	return nil
}

// SetMap maps a name to a set, such as a command to its flags.
type SetMap map[string]Set

// Add adds v to name's set, creating it; v "" only records the name.
func (m SetMap) Add(name, v string) {
	if m[name] == nil {
		m[name] = Set{}
	}
	if v != "" {
		m[name].Add(v)
	}
}

// Report is what i-mux uses of tmux: contract/recorded.json, and the shape of
// contract/expected.json.
type Report struct {
	// CLI is the global flags tmux is started with (-L -f -u …).
	CLI Set `json:"cli"`
	// Commands maps each command (its full name) to the flags it was given.
	Commands SetMap `json:"commands"`
	// Values maps "command -flag" to the shapes of the values given (N, N%, fmt, str …).
	Values SetMap `json:"values"`
	// Variables is every format variable read (#{name}, conditions, #{@option} included).
	Variables Set `json:"variables"`
	// Modifiers is every format modifier used (==, ?, e|+, p, P, W …).
	Modifiers Set `json:"modifiers"`
	// Aliases is every one-letter format used (#S #I …).
	Aliases Set `json:"aliases"`
	// Styles is every style keyword used in #[…] or a style option (fg=, bold, align=left …).
	Styles Set `json:"styles"`
	// Strftime is every strftime conversion in a status line or message (%H %M …).
	Strftime Set `json:"strftime"`
	// Options maps each tmux option to the scopes it is set or read in.
	Options SetMap `json:"options"`
	// UserOptions maps each @option to the scopes it is set or read in.
	UserOptions SetMap `json:"user_options"`
	// Hooks maps each hook to the set-hook flags it was given.
	Hooks SetMap `json:"hooks"`
	// Tables is every key table bound in, unbound in or switched to.
	Tables Set `json:"tables"`
	// Keys is every key name bound or unbound.
	Keys Set `json:"keys"`
	// SentKeys is every key name given to send-keys (not -l text).
	SentKeys Set `json:"sent_keys"`
	// CopyCommands is every copy-mode command run with send-keys -X.
	CopyCommands Set `json:"copy_commands"`
	// Targets is every target form given to -t, -s or -c (%N, =name, …).
	Targets Set `json:"targets"`
	// Unknown is every command tmux does not have, and every parse error.
	Unknown Set `json:"unknown"`
}

// NewReport returns an empty report.
func NewReport() *Report {
	return &Report{
		CLI: Set{}, Commands: SetMap{}, Values: SetMap{}, Variables: Set{}, Modifiers: Set{},
		Aliases: Set{}, Styles: Set{}, Strftime: Set{}, Options: SetMap{}, UserOptions: SetMap{},
		Hooks: SetMap{}, Tables: Set{}, Keys: Set{}, SentKeys: Set{}, CopyCommands: Set{},
		Targets: Set{}, Unknown: Set{},
	}
}

// Items lists every item of r as "section: item" strings, for comparing reports.
func (r *Report) Items() Set {
	out := Set{}
	sets := map[string]Set{
		"cli": r.CLI, "variable": r.Variables, "modifier": r.Modifiers, "alias": r.Aliases,
		"style": r.Styles, "strftime": r.Strftime, "table": r.Tables, "key": r.Keys,
		"sent key": r.SentKeys, "copy command": r.CopyCommands, "target": r.Targets,
	}
	for name, s := range sets {
		for v := range s {
			out.Add(name + ": " + v)
		}
	}
	maps := map[string]SetMap{
		"command": r.Commands, "value": r.Values, "option": r.Options,
		"user option": r.UserOptions, "hook": r.Hooks,
	}
	for name, m := range maps {
		for k, s := range m {
			out.Add(name + ": " + k)
			for v := range s {
				out.Add(name + ": " + k + " " + v)
			}
		}
	}
	return out
}

// Recorded is contract/recorded.json: what i-mux uses, and what only i-mux's e2e harness uses.
type Recorded struct {
	From    string  `json:"from"`
	Calls   int     `json:"calls"`
	Configs int     `json:"configs"`
	Imux    *Report `json:"imux"`
	Harness *Report `json:"harness_only"`
	// Source is what i-mux's source builds that neither the e2e run nor the harness used.
	Source *Report `json:"source_only,omitempty"`
}

// AddSource sets rec.Source from a source scan, keeping only what the run did not see.
func (rec *Recorded) AddSource(src *Report) {
	seen := rec.Imux.merged(rec.Harness)
	rec.Source = src.Without(seen)
}

// merged returns a report holding the items of r and other.
func (r *Report) merged(other *Report) *Report {
	out := NewReport()
	for _, from := range []*Report{r, other} {
		if from == nil {
			continue
		}
		pairs := []struct{ from, to Set }{
			{from.CLI, out.CLI},
			{from.Variables, out.Variables},
			{from.Modifiers, out.Modifiers},
			{from.Aliases, out.Aliases},
			{from.Styles, out.Styles},
			{from.Strftime, out.Strftime},
			{from.Tables, out.Tables},
			{from.Keys, out.Keys},
			{from.SentKeys, out.SentKeys},
			{from.CopyCommands, out.CopyCommands},
			{from.Targets, out.Targets},
			{from.Unknown, out.Unknown},
		}
		for _, p := range pairs {
			for v := range p.from {
				p.to.Add(v)
			}
		}
		maps := []struct{ from, to SetMap }{
			{from.Commands, out.Commands},
			{from.Values, out.Values},
			{from.Options, out.Options},
			{from.UserOptions, out.UserOptions},
			{from.Hooks, out.Hooks},
		}
		for _, m := range maps {
			for k, s := range m.from {
				m.to.Add(k, "")
				for v := range s {
					m.to.Add(k, v)
				}
			}
		}
	}
	return out
}

// Diff compares what was recorded with what is expected. Missing items are used but not in the
// contract; unused items are in the contract but were not seen.
func Diff(rec *Recorded, expected *Report) (missing, unused []string) {
	used := rec.Imux.merged(rec.Harness).merged(rec.Source).Items()
	exp := expected.Items()
	for _, k := range used.Sorted() {
		if !exp[k] {
			missing = append(missing, k)
		}
	}
	for _, k := range exp.Sorted() {
		if !used[k] {
			unused = append(unused, k)
		}
	}
	return missing, unused
}

// Without returns the items of r that are not in other, as a report.
func (r *Report) Without(other *Report) *Report {
	out := NewReport()
	have := other.Items()
	keep := func(section, item string) bool { return !have[section+": "+item] }
	sets := []struct {
		name     string
		from, to Set
	}{
		{"cli", r.CLI, out.CLI},
		{"variable", r.Variables, out.Variables},
		{"modifier", r.Modifiers, out.Modifiers},
		{"alias", r.Aliases, out.Aliases},
		{"style", r.Styles, out.Styles},
		{"strftime", r.Strftime, out.Strftime},
		{"table", r.Tables, out.Tables},
		{"key", r.Keys, out.Keys},
		{"sent key", r.SentKeys, out.SentKeys},
		{"copy command", r.CopyCommands, out.CopyCommands},
		{"target", r.Targets, out.Targets},
		{"unknown", r.Unknown, out.Unknown},
	}
	for _, s := range sets {
		for v := range s.from {
			if keep(s.name, v) {
				s.to.Add(v)
			}
		}
	}
	maps := []struct {
		name     string
		from, to SetMap
	}{
		{"command", r.Commands, out.Commands},
		{"value", r.Values, out.Values},
		{"option", r.Options, out.Options},
		{"user option", r.UserOptions, out.UserOptions},
		{"hook", r.Hooks, out.Hooks},
	}
	for _, m := range maps {
		for k, s := range m.from {
			if keep(m.name, k) {
				m.to.Add(k, "")
			}
			for v := range s {
				if keep(m.name, k+" "+v) {
					m.to.Add(k, v)
				}
			}
		}
	}
	return out
}
