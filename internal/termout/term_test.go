package termout

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// The client terminals in testdata/term, described from the same terminfo capabilities as the
// pinned tmux described them (cmd/termfixture): every capability after features, COLORTERM and
// overrides, the terminal's flags and the client's features (TASKS.md C2.7).

type termCase struct {
	Name         string   `json:"name"`
	Term         string   `json:"term"`
	ColorTerm    string   `json:"colorterm"`
	Features     []string `json:"features"`
	Overrides    []string `json:"overrides"`
	Caps         []string `json:"caps"`
	Describe     []string `json:"describe"`
	Flags        string   `json:"flags"`
	TermFeatures string   `json:"termfeatures"`
}

func TestTermFixtures(t *testing.T) {
	files, err := filepath.Glob("../../testdata/term/*.json")
	if err != nil || len(files) == 0 {
		t.Fatalf("no cases: %v", err)
	}
	for _, f := range files {
		var c termCase
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(b, &c); err != nil {
			t.Fatal(err)
		}
		t.Run(c.Name, func(t *testing.T) {
			o := Options{
				TerminalFeatures:  append(append([]string(nil), DefaultOptions.TerminalFeatures...), c.Features...),
				TerminalOverrides: append(append([]string(nil), DefaultOptions.TerminalOverrides...), c.Overrides...),
				ColorTerm:         c.ColorTerm,
			}
			feat := &Features{}
			term, err := NewTerm(c.Term, c.Caps, feat, o)
			if err != nil {
				t.Fatal(err)
			}
			got := term.Describe()
			for i := range max(len(got), len(c.Describe)) {
				var a, b string
				if i < len(c.Describe) {
					a = c.Describe[i]
				}
				if i < len(got) {
					b = got[i]
				}
				if a != b {
					t.Errorf("tmux:      %s\nimux-core: %s", a, b)
				}
			}
			if flags := fmt.Sprintf("%#x", term.Flags); flags != c.Flags {
				t.Errorf("flags %s, tmux %s", flags, c.Flags)
			}
			if names := FeatureNames(feat.Enabled); names != c.TermFeatures {
				t.Errorf("features %q, tmux %q", names, c.TermFeatures)
			}
		})
	}
}
