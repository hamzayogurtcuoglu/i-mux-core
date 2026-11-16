package status

import (
	"strings"
	"testing"
	"time"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/format"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/model"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/options"
)

func newOptions(t *testing.T) *options.Globals {
	t.Helper()
	return options.NewGlobals()
}

func TestDrawBlank(t *testing.T) {
	g := newOptions(t)
	g.Session.SetNumber("status", 1)
	g.Session.SetNumber("status-position", 1)
	g.Session.SetString("status-style", false, "bg=colour4,fg=colour15")
	if err := g.Session.Get("status-format").ArraySet("0", strPtr(""), false); err != nil {
		t.Fatalf("array set: %v", err)
	}

	tr := model.NewTree()
	ctx := &format.Context{Tree: tr}
	ctx.Defaults(nil, nil, nil, nil)

	lines := Draw(ctx, g.Session, 80)
	if len(lines) != 1 {
		t.Fatalf("got %d lines", len(lines))
	}
	if len(lines[0].Cells) != 80 {
		t.Fatalf("got %d cells", len(lines[0].Cells))
	}
	for i, c := range lines[0].Cells {
		if c.Data != " " {
			t.Fatalf("cell %d data %q", i, c.Data)
		}
	}
}

func TestDrawSimple(t *testing.T) {
	g := newOptions(t)
	g.Session.SetNumber("status", 1)

	tr := model.NewTree()
	tr.Options.Session.SetString("status-left", false, "hello")
	tr.Options.Session.SetString("status-style", false, "bg=colour4,fg=colour15")
	sess, _ := tr.NewSession("test", "", time.Now())
	ctx := &format.Context{Tree: tr}
	ctx.Defaults(nil, sess, nil, nil)

	lines := Draw(ctx, g.Session, 80)
	if len(lines) != 1 {
		t.Fatalf("got %d lines", len(lines))
	}
	line := lines[0].Cells
	if line[0].Data != "h" {
		t.Fatalf("first cell %q", line[0].Data)
	}
}

func strPtr(s string) *string { return &s }

func TestDrawDefault(t *testing.T) {
	g := newOptions(t)
	g.Session.SetNumber("status", 1)

	tr := model.NewTree()
	tr.Options.Session.SetString("status-left", false, "")
	tr.Options.Session.SetString("status-right", false, "")
	tr.Options.Session.SetString("status-left", false, "[left]")
	tr.Options.Session.SetString("status-right", false, "[right]")
	sess, _ := tr.NewSession("test", "", time.Now())
	wl, _ := tr.NewWindow(sess, -1, 0, 80, 24)
	sess.SelectWindow(wl)
	p := tr.NewPane(wl.Window, nil, false, 80, 24)
	wl.Window.Active = p

	ctx := &format.Context{Tree: tr}
	ctx.Defaults(nil, sess, wl, p)

	lines := Draw(ctx, g.Session, 80)
	if len(lines) != 1 {
		t.Fatalf("got %d lines", len(lines))
	}
	var sb strings.Builder
	for _, c := range lines[0].Cells {
		sb.WriteString(c.Data)
	}
	t.Logf("line: %q", sb.String())
}
