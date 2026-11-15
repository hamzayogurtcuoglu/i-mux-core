package version

// Version is the engine's version. Release builds set it with
// -ldflags "-X github.com/hamzayogurtcuoglu/i-mux-core/internal/version.Version=X.Y.Z".
var Version = "0.0.0-dev"

// Tmux is the tmux version whose behaviour the engine implements. #{version} expands to it.
const Tmux = "3.6"

// Line is what imux-core -V prints.
func Line() string {
	return "imux-core " + Version + " (tmux " + Tmux + " compatible)"
}
