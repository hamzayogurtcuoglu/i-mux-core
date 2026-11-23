//go:build darwin || linux

package server

import (
	"strings"
	"testing"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/model"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/vt"
)

// allow-default-colours (E19, C12.2): on, a program's OSC 11 sets the pane's background as in
// tmux; turned off, that background is forgotten and later ones are ignored, so window-style
// colours the pane.
func TestAllowDefaultColours(t *testing.T) {
	w := newInputWorld(t, "focus")
	kw := &keyWorld{inputWorld: w}
	black := vt.ColorJoinRGB(0, 0, 0)
	program := func(seq string) {
		_ = w.s.Do(func(tr *model.Tree) {
			p := tr.Pane(0)
			p.Mu.Lock()
			_, _ = p.Term.Write([]byte(seq))
			p.Mu.Unlock()
		})
	}
	bg := func() (c vt.Color) {
		_ = w.s.Do(func(tr *model.Tree) {
			p := tr.Pane(0)
			p.Mu.Lock()
			c = p.Term.Palette.Bg
			p.Mu.Unlock()
		})
		return c
	}
	program("\x1b]11;#000000\x07")
	if got := bg(); got != black {
		t.Fatalf("with allow-default-colours on, bg = %v, want black", got)
	}
	kw.run(t, "set", "-g", "allow-default-colours", "off")
	if got := bg(); got != vt.ColorDefault {
		t.Fatalf("turning it off kept the program's bg %v", got)
	}
	program("\x1b]11;#000000\x07")
	if got := bg(); got != vt.ColorDefault {
		t.Fatalf("with allow-default-colours off, the program set bg %v", got)
	}
	if got := kw.ask(t, "show", "-gv", "allow-default-colours"); got != "off\n" {
		t.Errorf("show -gv allow-default-colours = %q", got)
	}
	if got := kw.ask(t, "show", "-gw"); !strings.Contains(got, "allow-default-colours off\n") {
		t.Errorf("show -gw leaves it out when set:\n%s", got)
	}
	kw.run(t, "set", "-gu", "allow-default-colours")
	if got := kw.ask(t, "show", "-gw"); strings.Contains(got, "allow-default-colours") {
		t.Errorf("show -gw lists it at its default, unlike tmux:\n%s", got)
	}
}
