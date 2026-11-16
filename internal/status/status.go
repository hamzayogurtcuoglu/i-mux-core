// Package status renders status lines (status-format[N]), their alignment and ranges, and maps clicks to ranges.
package status

import (
	"strconv"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/format"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/options"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/style"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/vt"
)

// defaultStatusFormat0 is tmux 3.8's default status-format[0].
const defaultStatusFormat0 = "#[align=left range=left #{E:status-left-style}]#[push-default]#{T;=/#{status-left-length}:status-left}#[pop-default]#[norange default]#[list=on align=#{status-justify}]#[list=left-marker]<#[list=right-marker]>#[list=on]#{W:#[range=window|#{window_index} #{E:window-status-style}#{?#{&&:#{window_last_flag},#{!=:#{E:window-status-last-style},default}}, #{E:window-status-last-style},}#{?#{&&:#{window_bell_flag},#{!=:#{E:window-status-bell-style},default}}, #{E:window-status-bell-style},#{?#{&&:#{||:#{window_activity_flag},#{window_silence_flag}},#{!=:#{E:window-status-activity-style},default}}, #{E:window-status-activity-style},}}]#[push-default]#{T:window-status-format}#[pop-default]#[norange default]#{?loop_last_flag,,#{E:window-status-separator}},#[range=window|#{window_index} list=focus #{?#{!=:#{E:window-status-current-style},default},#{E:window-status-current-style},#{E:window-status-style}}#{?#{&&:#{window_last_flag},#{!=:#{E:window-status-last-style},default}}, #{E:window-status-last-style},}#{?#{&&:#{window_bell_flag},#{!=:#{E:window-status-bell-style},default}}, #{E:window-status-bell-style},#{?#{&&:#{||:#{window_activity_flag},#{window_silence_flag}},#{!=:#{E:window-status-activity-style},default}}, #{E:window-status-activity-style},}}]#[push-default]#{T:window-status-current-format}#[pop-default]#[norange list=on default]#{?loop_last_flag,,#{E:window-status-separator}}}#[nolist align=right range=right #{E:status-right-style}]#[push-default]#{T;=/#{status-right-length}:status-right}#[pop-default]#[norange default]"

// defaultStatusFormat1 is tmux 3.8's default status-format[1].
const defaultStatusFormat1 = "#[align=left]#{R: ,#{e|-:#{w;T;=/#{status-left-length}:status-left},3}}P: #[norange default]#[list=on align=#{status-justify}]#[list=left-marker]<#[list=right-marker]>#[list=on]#{P:#[range=pane|#{pane_id} #{E:pane-status-style}]#[push-default]#{T:window-pane-status-format}#[pop-default]#[norange list=on default]#{?loop_last_flag,,#{E:window-status-separator}},#[range=pane|#{pane_id} list=focus #{?#{!=:#{E:pane-status-current-style},default},#{E:pane-status-current-style},#{E:pane-status-style}}#[push-default]#{T:window-pane-status-format}#[pop-default]#[norange list=on default]#{?loop_last_flag,,#{E:window-status-separator}}}#[nolist align=right]#{R: ,#{e|-:#{w;T;=/#{status-right-length}:status-right},3}}P: #[norange default]"

// Range is a clickable range on a status line, like tmux's status range.
type Range struct {
	Type style.RangeType
	Arg  uint32
	Str  string
	X0   int // inclusive
	X1   int // inclusive
}

// Line is one status line.
type Line struct {
	Cells  []vt.Cell
	Ranges []Range
}

// Draw renders the status lines for a client of width sx in context ctx.
func Draw(ctx *format.Context, oo *options.Options, sx int) []Line {
	n := int(oo.GetNumber("status"))
	if n <= 0 || sx <= 0 {
		return nil
	}

	base := baseStyle(oo)
	fmtOpt := oo.Get("status-format")
	out := make([]Line, n)
	for i := 0; i < n; i++ {
		s, have := "", false
		if fmtOpt != nil {
			if v, ok := fmtOpt.ArrayGet(strconv.Itoa(i)); ok {
				s, have = v.Str, true
			}
		}
		if !have {
			switch i {
			case 0:
				s = defaultStatusFormat0
			case 1:
				s = defaultStatusFormat1
			}
		}
		if s == "" {
			out[i].Cells = blankLine(sx, base)
			continue
		}
		expanded := ctx.Expand(s)
		out[i] = drawLine(expanded, oo, sx, base)
	}
	return out
}

// baseStyle is the status line's base style from status-style, status-fg and status-bg.
func baseStyle(oo *options.Options) style.Style {
	sy := style.Default()
	_ = sy.Parse(vt.DefaultCell, oo.GetString("status-style"))
	if fg := oo.Get("status-fg"); fg != nil && fg.Value.Num != int64(vt.ColorDefault) {
		sy.Cell.Fg = vt.Color(fg.Value.Num)
	}
	if bg := oo.Get("status-bg"); bg != nil && bg.Value.Num != int64(vt.ColorDefault) {
		sy.Cell.Bg = vt.Color(bg.Value.Num)
	}
	return sy
}

func blankLine(sx int, sy style.Style) []vt.Cell {
	line := make([]vt.Cell, sx)
	gc := vt.DefaultCell
	sy.Add(&gc)
	for x := range sx {
		line[x] = gc
	}
	return line
}

// piece is one text run with its active style.
type piece struct {
	text string
	sy   style.Style
}

// section is a run of pieces that share an alignment.
type section struct {
	align       style.Align
	cells       []vt.Cell
	ranges      []Range
	list        bool
	listMarker  [2]string
	listJustify string
}

// drawLine renders one expanded status format string.
func drawLine(s string, oo *options.Options, sx int, base style.Style) Line {
	pieces := splitPieces(s, base)

	// Group pieces into sections by alignment.
	var sections []section
	cur := section{align: style.AlignLeft}
	var currentRange *Range

	flushRange := func(end int) {
		if currentRange == nil {
			return
		}
		currentRange.X1 = end
		if currentRange.X1 < currentRange.X0 {
			currentRange.X1 = currentRange.X0
		}
		cur.ranges = append(cur.ranges, *currentRange)
		currentRange = nil
	}

	for _, p := range pieces {
		// Alignment changes start a new section; other style changes stay in the current one.
		if p.sy.Align != style.AlignDefault && p.sy.Align != cur.align {
			flushRange(len(cur.cells) - 1)
			sections = append(sections, cur)
			cur = section{align: p.sy.Align}
		}

		// List directives.
		if cur.list && p.sy.List == style.ListOff {
			// #[nolist] closes the current list section.
			flushRange(len(cur.cells) - 1)
			sections = append(sections, cur)
			a := p.sy.Align
			if a == style.AlignDefault {
				a = style.AlignLeft
			}
			cur = section{align: a}
			continue
		}
		if p.sy.List == style.ListOn {
			// #[list=on] starts a list section if not already in one.
			if !cur.list {
				flushRange(len(cur.cells) - 1)
				sections = append(sections, cur)
				a := p.sy.Align
				if a == style.AlignDefault {
					a = style.AlignLeft
				}
				cur = section{align: a, list: true}
			}
			if cur.list && p.sy.Align != style.AlignDefault {
				cur.listJustify = alignName(p.sy.Align)
			}
			continue
		}
		if p.sy.List == style.ListLeftMarker {
			cur.listMarker[0] = p.text
			if p.text != "" {
				// text of a marker piece is the marker itself; keep it as the marker.
				cur.listMarker[0] = p.text
			}
			continue
		}
		if p.sy.List == style.ListRightMarker {
			cur.listMarker[1] = p.text
			if p.text != "" {
				cur.listMarker[1] = p.text
			}
			continue
		}
		if p.text == "" {
			continue
		}

		// Range handling.
		if p.sy.RangeType == style.RangeNone {
			flushRange(len(cur.cells) - 1)
		} else {
			flushRange(len(cur.cells) - 1)
			currentRange = &Range{
				Type: p.sy.RangeType,
				Arg:  p.sy.RangeArg,
				Str:  p.sy.RangeString,
				X0:   len(cur.cells),
			}
		}

		// Convert text to cells.
		for _, r := range p.text {
			w := vt.RuneWidth(r)
			if w < 0 {
				w = 0
			}
			if w == 0 && r < 0x20 {
				continue
			}
			if len(cur.cells)+w > sx {
				break
			}
			gc := cellForRune(r, p.sy, base)
			cur.cells = append(cur.cells, gc)
			for k := 1; k < w; k++ {
				pad := gc
				pad.Data, pad.Width, pad.Flags = "", 0, vt.FlagPadding
				cur.cells = append(cur.cells, pad)
			}
		}
	}
	flushRange(len(cur.cells) - 1)
	if len(cur.cells) > 0 || cur.align != style.AlignLeft {
		sections = append(sections, cur)
	}

	// Lay out the sections into one line.
	cells, ranges := layoutSections(sections, sx, base, oo)
	return Line{Cells: cells, Ranges: ranges}
}

func alignName(a style.Align) string {
	switch a {
	case style.AlignLeft:
		return "left"
	case style.AlignRight:
		return "right"
	case style.AlignCentre:
		return "centre"
	case style.AlignAbsoluteCentre:
		return "absolute-centre"
	}
	return "left"
}

// layoutSections places left, right and centre sections into a line of width sx.
func layoutSections(sections []section, sx int, base style.Style, oo *options.Options) ([]vt.Cell, []Range) {
	if len(sections) == 0 {
		return blankLine(sx, base), nil
	}

	var lefts, centers, rights []section
	for i := range sections {
		s := sections[i]
		switch s.align {
		case style.AlignLeft:
			lefts = append(lefts, s)
		case style.AlignRight:
			rights = append(rights, s)
		case style.AlignCentre, style.AlignAbsoluteCentre:
			centers = append(centers, s)
		}
	}

	leftCells, leftRanges := buildGroup(lefts, base)
	rightCells, rightRanges := buildGroup(rights, base)
	centerCells, centerRanges := buildGroup(centers, base)

	cells := make([]vt.Cell, sx)
	gc := vt.DefaultCell
	base.Add(&gc)
	for x := range sx {
		cells[x] = gc
	}
	ranges := []Range{}

	// Place right first so left can fill the rest.
	rightX := sx
	if len(rightCells) > 0 {
		w := len(rightCells)
		if w > sx {
			w = sx
		}
		rightX = sx - w
		copy(cells[rightX:], rightCells[:w])
		for _, rng := range rightRanges {
			rng.X0, rng.X1 = rng.X0+rightX, rng.X1+rightX
			ranges = append(ranges, rng)
		}
	}

	leftW := rightX
	if len(leftCells) > 0 {
		w := len(leftCells)
		if w > leftW {
			w = leftW
		}
		copy(cells[:w], leftCells[:w])
		for _, rng := range leftRanges {
			if rng.X1 >= w {
				rng.X1 = w - 1
			}
			ranges = append(ranges, rng)
		}
	}

	if len(centerCells) > 0 {
		avail := rightX
		start := min(len(leftCells), leftW)
		avail -= start
		cw := len(centerCells)
		if cw > avail {
			cw = avail
		}
		cx := start + (avail-cw)/2
		if len(lefts) > 0 && lefts[0].align == style.AlignAbsoluteCentre {
			cx = (sx - cw) / 2
		}
		if cx < 0 {
			cx = 0
		}
		if cx+cw > sx {
			cw = sx - cx
		}
		copy(cells[cx:cx+cw], centerCells[:cw])
		for _, rng := range centerRanges {
			rng.X0, rng.X1 = rng.X0+cx, rng.X1+cx
			if rng.X1 >= sx {
				rng.X1 = sx - 1
			}
			ranges = append(ranges, rng)
		}
	}

	return cells, ranges
}

// buildGroup combines sections of the same alignment into one cell slice.
func buildGroup(list []section, base style.Style) ([]vt.Cell, []Range) {
	var cells []vt.Cell
	var ranges []Range
	for _, s := range list {
		if s.list {
			_ = s.listMarker[0]
		}
		start := len(cells)
		if s.list {
			// Add list markers around the list content.
			if s.listMarker[0] != "" {
				cells = append(cells, textCells(s.listMarker[0], base)...)
			}
		}
		cells = append(cells, s.cells...)
		if s.list {
			if s.listMarker[1] != "" {
				cells = append(cells, textCells(s.listMarker[1], base)...)
			}
		}
		for _, rng := range s.ranges {
			rng.X0 += start
			rng.X1 += start
			ranges = append(ranges, rng)
		}
	}
	return cells, ranges
}

func textCells(s string, base style.Style) []vt.Cell {
	var cells []vt.Cell
	for _, r := range s {
		w := vt.RuneWidth(r)
		gc := vt.DefaultCell
		base.Add(&gc)
		gc.Data = string(r)
		gc.Width = uint8(w)
		cells = append(cells, gc)
		for k := 1; k < w; k++ {
			pad := gc
			pad.Data, pad.Width, pad.Flags = "", 0, vt.FlagPadding
			cells = append(cells, pad)
		}
	}
	return cells
}

// splitPieces splits an expanded format string into text pieces with active styles.
func splitPieces(s string, base style.Style) []piece {
	var pieces []piece
	cur := base
	start := 0
	for i := 0; i < len(s); {
		if s[i] == '#' && i+1 < len(s) && s[i+1] == '[' {
			if i > start {
				pieces = append(pieces, piece{text: s[start:i], sy: cur})
			}
			end := findStyleEnd(s[i:])
			if end < 0 {
				break
			}
			styleText := s[i+2 : i+2+end]
			cur = applyStyle(cur, styleText)
			pieces = append(pieces, piece{text: "", sy: cur})
			i += 2 + end + 1
			start = i
		} else {
			i++
		}
	}
	if start < len(s) {
		pieces = append(pieces, piece{text: s[start:], sy: cur})
	}
	return pieces
}

// findStyleEnd returns the index of the matching ']' inside s, where s starts with "#[".
func findStyleEnd(s string) int {
	if len(s) < 2 || s[0] != '#' || s[1] != '[' {
		return -1
	}
	depth := 1
	for i := 2; i < len(s); i++ {
		switch s[i] {
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				return i - 2
			}
		}
	}
	return -1
}

// applyStyle merges a style directive into the current style.
func applyStyle(cur style.Style, text string) style.Style {
	out := cur
	_ = out.Parse(cur.Cell, text)
	return out
}

func cellForRune(r rune, sy, base style.Style) vt.Cell {
	gc := vt.DefaultCell
	base.Add(&gc)
	sy.Add(&gc)
	gc.Data = string(r)
	gc.Width = uint8(vt.RuneWidth(r))
	return gc
}
