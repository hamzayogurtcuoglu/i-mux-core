package status

import (
	"maps"
	"strings"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/format"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/options"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/style"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/vt"
)

// MessageLines is tmux's status_message_redraw: the status lines (at least one, blank when the
// status is off) with msg drawn over the message line through message-format, in
// message-style. With ignoreStyles, the message's # are text, not styles.
func MessageLines(ctx *format.Context, oo *options.Options, sx int, lines []Line, msg string, ignoreStyles bool) [][]vt.Cell {
	if sx <= 0 {
		return nil
	}
	out := make([][]vt.Cell, max(len(lines), 1))
	for i := range out {
		row := make([]vt.Cell, sx)
		for x := range row {
			row[x] = vt.DefaultCell
		}
		if i < len(lines) {
			copy(row, lines[i].Cells)
		}
		out[i] = row
	}

	// status_prompt_line_at: message-line, within the status lines.
	line := 0
	if len(lines) > 0 {
		line = min(int(oo.GetNumber("message-line")), len(lines)-1)
	}
	ax, aw := messageArea(oo, sx)

	fc := *ctx
	fc.Values = maps.Clone(ctx.Values)
	if fc.Values == nil {
		fc.Values = map[string]string{}
	}
	if ignoreStyles {
		msg = strings.ReplaceAll(msg, "#", "##")
	}
	fc.Values["message"] = msg
	fc.Values["command_prompt"] = "0"

	gc := vt.DefaultCell
	sy := style.Default()
	if sy.Parse(vt.DefaultCell, fc.Expand(oo.GetString("message-style"))) == nil {
		sy.Add(&gc)
	}
	expanded := fc.ExpandTime(oo.GetString("message-format"))
	DrawFormat(out[line][ax:], gc, aw, expanded, nil, false)
	return out
}

// messageArea is status_message_area: where the message goes on its line, from message-style's
// width and align (read without expanding its formats, as tmux does).
func messageArea(oo *options.Options, sx int) (x, width int) {
	width = sx
	sy := style.Default()
	ok := sy.Parse(vt.DefaultCell, oo.GetString("message-style")) == nil
	if ok && sy.Width >= 0 {
		if sy.WidthPct {
			width = sx * sy.Width / 100
		} else {
			width = sy.Width
		}
	}
	if width == 0 || width > sx {
		width = sx
	}
	if ok {
		switch sy.Align {
		case style.AlignCentre, style.AlignAbsoluteCentre:
			x = (sx - width) / 2
		case style.AlignRight:
			x = sx - width
		}
	}
	return x, width
}
