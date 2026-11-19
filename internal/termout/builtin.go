package termout

import (
	_ "embed"
	"os"
	"sync"
)

// The built-in xterm-256color (E15): ncurses' compiled entry, used where the system has no
// terminfo database at all (Windows), so that a client there describes its terminal as the
// modern terminals it runs in (Windows Terminal, conhost with VT processing) are described.

// BuiltinName is the terminal the built-in description is.
const BuiltinName = "xterm-256color"

//go:embed builtin/xterm-256color
var builtinXterm []byte

var builtin = sync.OnceValues(func() (*Terminfo, error) { return ParseTerminfo(builtinXterm) })

// Builtin returns the built-in xterm-256color description.
func Builtin() (*Terminfo, error) { return builtin() }

// NoDatabase reports whether none of dirs exists: the system has no terminfo to look in.
func NoDatabase(dirs []string) bool {
	for _, d := range dirs {
		if fi, err := os.Stat(d); err == nil && fi.IsDir() {
			return false
		}
	}
	return true
}
