package contract

// engineOptions are the engine's own options with their scopes (ARCHITECTURE.md E19): not
// tmux's, so not in tmuxOptions, yet known to i-mux's contract.
var engineOptions = map[string]string{
	"allow-default-colours": "window|pane",
}
