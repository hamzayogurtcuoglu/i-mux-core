//go:build darwin || linux

package server

import (
	"fmt"
	"testing"
	"time"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/model"
)

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

// A -O menu is chosen by a press, never by a release (E19, C12.1): the release of the click
// that chose an item in one menu reaches the menu that item opened, and must not close it.
func TestMenuStayOpenIgnoresRelease(t *testing.T) {
	w := newInputWorld(t, "focus")
	w.waitPane("focus: 1b5b49 in")
	kw := &keyWorld{inputWorld: w}
	kw.run(t, "set", "-g", "mouse", "on")
	kw.run(t, "display-menu", "-M", "-O", "-x", "C", "-y", "C", "--", "Mark", "m", "set -g @hit mark", "Other", "o", "")
	w.waitPane("focus: 1b5b4f out")
	var px, py int
	_ = w.s.Do(func(*model.Tree) {
		for _, md := range w.s.menus {
			px, py = md.px, md.py
		}
	})
	// The first item's row is py+1 (0-based); SGR reports are 1-based.
	at := fmt.Sprintf("%d;%d", px+3, py+2)
	w.typed("\x1b[<0;" + at + "m")
	time.Sleep(200 * time.Millisecond)
	if got := kw.option(t, "@hit"); got != "" {
		t.Fatalf("a release chose the item: @hit %q", got)
	}
	open := false
	_ = w.s.Do(func(*model.Tree) { open = len(w.s.menus) > 0 })
	if !open {
		t.Fatal("a release closed the menu")
	}
	w.typed("\x1b[<0;" + at + "M")
	kw.waitOption(t, "@hit", "mark")
	w.waitPane("focus: 1b5b49 in")
}

// Without -O a press moves the choice and its release chooses, as in tmux.
func TestMenuReleaseChoosesWithoutStayOpen(t *testing.T) {
	w := newInputWorld(t, "focus")
	w.waitPane("focus: 1b5b49 in")
	kw := &keyWorld{inputWorld: w}
	kw.run(t, "set", "-g", "mouse", "on")
	kw.run(t, "display-menu", "-M", "-x", "C", "-y", "C", "--", "Mark", "m", "set -g @hit mark", "Other", "o", "")
	w.waitPane("focus: 1b5b4f out")
	var px, py int
	_ = w.s.Do(func(*model.Tree) {
		for _, md := range w.s.menus {
			px, py = md.px, md.py
		}
	})
	at := fmt.Sprintf("%d;%d", px+3, py+2)
	w.typed("\x1b[<0;" + at + "M")
	time.Sleep(200 * time.Millisecond)
	if got := kw.option(t, "@hit"); got != "" {
		t.Fatalf("a press chose the item without -O: @hit %q", got)
	}
	w.typed("\x1b[<0;" + at + "m")
	kw.waitOption(t, "@hit", "mark")
}
