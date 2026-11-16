//go:build difftest

package difftest

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/layout/layoutfixture"
)

// TestLayoutScenarios runs C2.5's layout scenarios (testdata/layout) on both engines through
// the commands (split-window, kill-pane, resize-pane, swap-pane, select-pane, resize-window,
// select-layout) and compares every step: errors, window, panes and layouts. The old layout
// format needs a control client, which imux-core does not have yet; the model's test checks
// it against the golden files.
func TestLayoutScenarios(t *testing.T) {
	ref, core := tmuxEngine(t), coreEngine(t)
	paths, err := filepath.Glob("../../testdata/layout/*.txt")
	if err != nil || len(paths) == 0 {
		t.Fatalf("no layout scenarios: %v", err)
	}
	for _, p := range paths {
		t.Run(strings.TrimSuffix(filepath.Base(p), ".txt"), func(t *testing.T) {
			steps, err := layoutfixture.Read(p)
			if err != nil {
				t.Fatal(err)
			}
			want, err := layoutfixture.FromServer(ref.Path, steps, nil)
			if err != nil {
				t.Fatal(err)
			}
			got, err := layoutfixture.FromServer(core.Path, steps, want)
			if err != nil {
				t.Fatal(err)
			}
			for i := range want {
				w, g := want[i], got[i]
				w.V1, g.V1 = "", ""
				if !reflect.DeepEqual(w, g) {
					t.Errorf("%s:%s\n tmux:      %+v\n imux-core: %+v", p, w.Step, w, g)
				}
			}
		})
	}
}
