package options

// EngineTable holds the engine's own options, which tmux 3.8 does not have (ARCHITECTURE.md
// E19). They come after tmux's in Table.
var EngineTable = []Entry{
	// allow-default-colours off: a program cannot set or reset the pane's default colours
	// (OSC 10, 11, 110, 111), so window-style and window-active-style stay.
	{Name: "allow-default-colours", Type: TypeFlag, Scope: ScopeWindow | ScopePane, Flags: 0, DefaultNum: 1},
}

func init() { Table = append(Table, EngineTable...) }

// IsEngine reports whether e is one of the engine's own options, not tmux's.
func IsEngine(e *Entry) bool {
	for i := range EngineTable {
		if EngineTable[i].Name == e.Name {
			return true
		}
	}
	return false
}
