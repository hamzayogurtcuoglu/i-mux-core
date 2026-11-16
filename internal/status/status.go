// Package status renders status lines (status-format[N]), their alignment and ranges, and maps clicks to ranges.
package status

import (
	"strconv"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/format"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/options"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/style"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/vt"
)

// Range is a clickable range on a status line, tmux's style_range: cells Start to End,
// End not included.
type Range struct {
	Type  style.RangeType
	Arg   uint32
	Str   string
	Start int
	End   int
}

// Line is one status line.
type Line struct {
	Cells  []vt.Cell
	Ranges []Range
}

// RangeAt returns the range at x, or nil.
func (l *Line) RangeAt(x int) *Range {
	for i := range l.Ranges {
		if r := &l.Ranges[i]; x >= r.Start && x < r.End {
			return r
		}
	}
	return nil
}

// Draw renders the status lines for a client of width sx in context ctx, as tmux's
// status_redraw: each line is status-format[N] expanded and drawn over the status style; a
// line without a status-format entry is blank.
func Draw(ctx *format.Context, oo *options.Options, sx int) []Line {
	n := int(oo.GetNumber("status"))
	if n <= 0 || sx <= 0 {
		return nil
	}

	base := BaseCell(ctx, oo)
	fmtOpt := oo.Get("status-format")
	out := make([]Line, n)
	for i := range n {
		cells := make([]vt.Cell, sx)
		for x := range cells {
			cells[x] = base
		}
		out[i].Cells = cells
		if fmtOpt == nil {
			continue
		}
		v, ok := fmtOpt.ArrayGet(strconv.Itoa(i))
		if !ok {
			continue
		}
		DrawFormat(cells, base, sx, ctx.ExpandTime(v.Str), &out[i].Ranges, false)
	}
	return out
}

// BaseCell is the status line's cell: status-style (expanded as a format) over the default
// cell, then status-fg and status-bg when they are not the default.
func BaseCell(ctx *format.Context, oo *options.Options) vt.Cell {
	gc := vt.DefaultCell
	sy := style.Default()
	if sy.Parse(vt.DefaultCell, ctx.Expand(oo.GetString("status-style"))) == nil {
		sy.Add(&gc)
	}
	if fg := oo.Get("status-fg"); fg != nil && !defaultColour(vt.Color(fg.Value.Num)) {
		gc.Fg = vt.Color(fg.Value.Num)
	}
	if bg := oo.Get("status-bg"); bg != nil && !defaultColour(vt.Color(bg.Value.Num)) {
		gc.Bg = vt.Color(bg.Value.Num)
	}
	return gc
}

// defaultColour is COLOUR_DEFAULT: 8 (default) or 9 (terminal).
func defaultColour(c vt.Color) bool { return c == 8 || c == 9 }
