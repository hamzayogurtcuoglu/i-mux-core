package version

import (
	"runtime/debug"
	"strings"
)

// Module is this repository's module path.
const Module = "github.com/hamzayogurtcuoglu/i-mux-core"

// dev is Version when nothing set it.
const dev = "0.0.0-dev"

// Version is the engine's version. Release builds set it with
// -ldflags "-X github.com/hamzayogurtcuoglu/i-mux-core/internal/version.Version=X.Y.Z"; built
// from another module's pin of this one (i-mux's, E16), it is the pinned version.
var Version = dev

func init() {
	if Version != dev {
		return
	}
	if bi, ok := debug.ReadBuildInfo(); ok {
		if v := pinned(bi); v != "" {
			Version = v
		}
	}
}

// pinned is this module's version as a dependency of the main module ("0.1.0" for v0.1.0, the
// pseudo-version without its v before a tag); "" when this module is the main one, or is
// replaced by a directory, which has no version.
func pinned(bi *debug.BuildInfo) string {
	if bi.Main.Path == Module {
		return ""
	}
	for _, d := range bi.Deps {
		if d.Path != Module {
			continue
		}
		if d.Replace != nil {
			d = d.Replace
		}
		if !strings.HasPrefix(d.Version, "v") {
			return ""
		}
		return strings.TrimPrefix(d.Version, "v")
	}
	return ""
}

// Tmux is the tmux version whose behaviour the engine implements. #{version} expands to it.
const Tmux = "3.8"

// Line is what imux-core -V prints (E9): tmux's form first, so i-mux reads it as Tmux, then the engine.
func Line() string {
	return "tmux " + Tmux + " (imux-core " + Version + ")"
}
