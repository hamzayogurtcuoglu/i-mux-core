package server

import (
	"strconv"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/format"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/model"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/style"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/vt"
)

// applyPaneStyles gives a pane's palette window-style, window-active-style and
// pane-colours (tmux's PANE_STYLECHANGED: cached_gc, cached_active_gc and
// colour_palette_from_option): they colour the pane's cells in the default
// colours and its 16 and 256 colours, and answer its programs' OSC 10 and 11.
func (s *Server) applyPaneStyles(p *model.Pane) {
	if p.Term == nil {
		return
	}
	expand := func(v string) string { return v }
	if p.Window != nil {
		ctx := &format.Context{Tree: s.tree, Server: s.ServerInfo(), Clients: s.clientFormats(), Now: s.opts.Now}
		ctx.Defaults(nil, nil, nil, p)
		expand = ctx.Expand
	}
	get := func(name string) vt.Cell {
		gc, sy := vt.DefaultCell, style.Default()
		if sy.Parse(vt.DefaultCell, expand(p.Options.GetString(name))) == nil {
			sy.Add(&gc)
		}
		return gc
	}
	w, a := get("window-style"), get("window-active-style")
	var colours [][2]int
	if o := p.Options.Get("pane-colours"); o != nil && o.IsArray() {
		for _, k := range o.ArrayKeys() {
			v, ok := o.ArrayGet(k)
			n, err := strconv.Atoi(k)
			if ok && err == nil {
				colours = append(colours, [2]int{n, int(v.Num)})
			}
		}
	}
	p.Mu.Lock()
	defer p.Mu.Unlock()
	pal := &p.Term.Palette
	pal.WindowFg, pal.WindowBg, pal.ActiveFg, pal.ActiveBg = w.Fg, w.Bg, a.Fg, a.Bg
	pal.ClearDefaults()
	for _, c := range colours {
		pal.SetDefault(c[0], vt.Color(c[1]))
	}
}
