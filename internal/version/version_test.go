package version

import (
	"runtime/debug"
	"testing"
)

func TestLine(t *testing.T) {
	if got, want := Line(), "tmux 3.8 (imux-core 0.0.0-dev)"; got != want {
		t.Fatalf("Line() = %q, want %q", got, want)
	}
}

// Built from another module's pin, the engine reports the pinned version (E16).
func TestPinned(t *testing.T) {
	dep := func(path, v string) *debug.Module { return &debug.Module{Path: path, Version: v} }
	for _, c := range []struct {
		name string
		bi   debug.BuildInfo
		want string
	}{
		{"this module", debug.BuildInfo{Main: debug.Module{Path: Module, Version: "(devel)"}}, ""},
		{"a tag", debug.BuildInfo{
			Main: debug.Module{Path: "github.com/hamzayogurtcuoglu/i-mux"},
			Deps: []*debug.Module{dep("golang.org/x/sys", "v0.49.0"), dep(Module, "v0.1.0")},
		}, "0.1.0"},
		{"a pseudo-version", debug.BuildInfo{
			Main: debug.Module{Path: "github.com/hamzayogurtcuoglu/i-mux"},
			Deps: []*debug.Module{dep(Module, "v0.0.0-20261118200000-0123456789ab")},
		}, "0.0.0-20261118200000-0123456789ab"},
		{"replaced by a directory", debug.BuildInfo{
			Main: debug.Module{Path: "github.com/hamzayogurtcuoglu/i-mux"},
			Deps: []*debug.Module{{Path: Module, Version: "v0.1.0", Replace: &debug.Module{Path: "../i-mux-core"}}},
		}, ""},
		{"not a dependency", debug.BuildInfo{Main: debug.Module{Path: "example.com/x"}}, ""},
	} {
		if got := pinned(&c.bi); got != c.want {
			t.Errorf("%s: pinned = %q, want %q", c.name, got, c.want)
		}
	}
}
