//go:build darwin || linux

package server

import "testing"

// Menus (TASKS.md C4.7): an item's key runs its command in the menu's target; while the menu
// is open the window's active pane has lost the focus, and q closes it.
func TestMenus(t *testing.T) {
	w := newInputWorld(t, "focus")
	w.waitPane("focus: 1b5b49 in")
	kw := &keyWorld{inputWorld: w}
	kw.run(t, "display-menu", "-x", "C", "-y", "C", "--", "Mark", "m", "set -g @hit mark", "Other", "o", "")
	w.waitPane("focus: 1b5b4f out")
	w.typed("m")
	kw.waitOption(t, "@hit", "mark")
	w.waitPane("focus: 1b5b49 in")

	kw.run(t, "display-menu", "--", "Mark", "m", "set -g @hit again")
	w.waitPane("focus: 1b5b4f out")
	w.typed("q")
	w.waitPane("focus: 1b5b49 in")
	if got := kw.option(t, "@hit"); got != "mark" {
		t.Errorf("@hit %q after q", got)
	}
}
