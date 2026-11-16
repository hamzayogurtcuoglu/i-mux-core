package render

import (
	"time"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/model"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/termout"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/vt"
)

// A client's frames (TASKS.md C2.8). Compose builds the frame a client shows: its window's
// panes at their offsets, the borders between them, what lies outside the window, and the
// status lines. Draw diffs it with the last frame drawn and writes the changed runs of cells
// with termout's port of tmux's tty_draw_line, inside synchronized output when the terminal has
// it, then puts the cursor where the active pane has it, as tmux's server_client_reset_state.
// Frames are what the client's terminal is to show; how tmux reaches the same screen (its
// scenes, spans and incremental pane updates) is not copied, only the result.

// SyncTimeout is how long a pane may hold a synchronized update (DEC mode 2026) before it is
// drawn anyway, as tmux's sync timer.
const SyncTimeout = time.Second

// cursorModes are tmux's CURSOR_MODES.
const cursorModes = vt.ModeCursor | vt.ModeCursorBlinking | vt.ModeCursorVeryVisible

// Frame is one composed client screen.
type Frame struct {
	Sx, Sy int
	Cells  []vt.Cell // Sy lines of Sx cells; a wide character is followed by padding cells

	Cx, Cy int // where the cursor goes
	Mode   int // the terminal modes: the cursor's and the mouse's are used
	Cursor *termout.CursorState
	// KeepCursor leaves the cursor where it is: the active pane holds a synchronized update.
	KeepCursor bool
}

// Line returns line y of the frame.
func (f *Frame) Line(y int) []vt.Cell { return f.Cells[y*f.Sx : (y+1)*f.Sx] }

type rect struct{ x, y, sx, sy int }

// Renderer draws one client's frames.
type Renderer struct {
	last  *Frame
	spare *Frame // the frame before last, whose cells the next Compose reuses
	links linkTable
	scene []buildCell

	geom map[*model.Pane]rect      // where each pane was in the last frame
	held map[*model.Pane]time.Time // panes in a synchronized update, since when

	// Written counts the bytes Draw has produced, for tests and benchmarks.
	Written int
}

// New returns a renderer whose first frame is drawn whole.
func New() *Renderer {
	return &Renderer{geom: map[*model.Pane]rect{}, held: map[*model.Pane]time.Time{}}
}

// Invalidate makes the next frame drawn whole: the terminal was cleared or resized, or its
// contents are not known.
func (r *Renderer) Invalidate() {
	r.last, r.spare = nil, nil
	r.links.reset()
}

// Deadline is when a pane holding a synchronized update must be drawn anyway, if one does.
func (r *Renderer) Deadline() (time.Time, bool) {
	var d time.Time
	for _, since := range r.held {
		if t := since.Add(SyncTimeout); d.IsZero() || t.Before(d) {
			d = t
		}
	}
	return d, !d.IsZero()
}

// Compose builds the frame for v at time now.
func (r *Renderer) Compose(v *View, now time.Time) *Frame {
	var f *Frame
	if sp := r.spare; sp != nil && len(sp.Cells) == v.Sx*v.Sy {
		r.spare = nil
		*sp = Frame{Sx: v.Sx, Sy: v.Sy, Cells: sp.Cells}
		f = sp
	} else {
		f = &Frame{Sx: v.Sx, Sy: v.Sy, Cells: make([]vt.Cell, v.Sx*v.Sy)}
	}
	lines := min(max(v.StatusLines, 0), v.Sy)
	areaY := 0
	if v.StatusTop {
		areaY = lines
	}
	areaSy := v.Sy - lines
	w := v.Window
	if w == nil || areaSy <= 0 || v.Sx <= 0 {
		for i := range f.Cells {
			f.Cells[i] = vt.DefaultCell
		}
		r.status(f, v, lines)
		r.popup(f, v)
		return f
	}
	sc := buildScene(v, areaSy, r.scene)
	r.scene = sc.cells
	active := w.Active
	paneStatus := v.paneStatusLines(sc, active)

	// Panes holding a synchronized update keep what the last frame showed of them.
	hold := map[*model.Pane]bool{}
	geom := map[*model.Pane]rect{}
	for _, p := range w.Panes {
		if !paneVisible(p) || p.Term == nil {
			continue
		}
		g := rect{p.Xoff - sc.ox, p.Yoff - sc.oy + areaY, p.Sx, p.Sy}
		geom[p] = g
		// A frozen client (a message is shown) keeps what it showed of the panes until a
		// whole redraw, as tmux leaves its terminal alone while TTY_FREEZE is set.
		if v.Freeze && r.last != nil && r.last.Sx == f.Sx && r.last.Sy == f.Sy && r.geom[p] == g {
			hold[p] = true
		}
		p.Mu.Lock()
		syncing := p.Term.Screen.Mode&vt.ModeSync != 0
		p.Mu.Unlock()
		if !syncing {
			delete(r.held, p)
			continue
		}
		since, ok := r.held[p]
		if !ok {
			since = now
			r.held[p] = now
		}
		if r.last != nil && r.last.Sx == f.Sx && r.last.Sy == f.Sy && r.geom[p] == g && now.Sub(since) < SyncTimeout {
			hold[p] = true
		}
	}
	for p := range r.held {
		if _, ok := geom[p]; !ok {
			delete(r.held, p)
		}
	}
	r.geom = geom

	for y := range areaSy {
		line := f.Line(areaY + y)
		for x := 0; x < sc.sx && x < f.Sx; {
			bc := sc.at(x, y)
			if bc.typ == spanStatus {
				line[x] = vt.DefaultCell
				if ps := paneStatus[bc.stPane]; bc.stOffset < len(ps) {
					line[x] = ps[bc.stOffset]
				}
				x++
				continue
			}
			if bc.typ != spanPane {
				line[x] = borderCell(v, bc, active)
				x++
				continue
			}
			// A run of one pane's line.
			p, px, py := bc.pane, bc.px, bc.py
			n := 1
			for x+n < sc.sx && x+n < f.Sx {
				nb := sc.at(x+n, y)
				if nb.typ != spanPane || nb.pane != p || nb.py != py || nb.px != px+n {
					break
				}
				n++
			}
			if hold[p] {
				copy(line[x:x+n], r.last.Line(areaY + y)[x:x+n])
			} else {
				p.Mu.Lock()
				paneLine(line[x:x+n], p, p.Term.Screen, px, py, &r.links, v)
				p.Mu.Unlock()
			}
			x += n
		}
	}
	r.menu(f, v, sc, areaY)
	r.status(f, v, lines)
	r.popup(f, v)
	switch {
	case v.Popup != nil:
		r.popupCursor(f, v)
	case v.Menu != nil:
		r.menuCursor(f, v, sc, areaY)
	case v.PromptCursor != nil:
		r.promptCursor(f, v)
	default:
		r.cursor(f, v, sc, areaY)
	}
	if r.links.full {
		// The link table started again: ids in the last frame mean other links now.
		r.Invalidate()
		return r.Compose(v, now)
	}
	return f
}

// status fills the status lines, or the message lines over them.
func (r *Renderer) status(f *Frame, v *View, lines int) {
	src := v.Status
	if v.Message != nil {
		src = v.Message
		lines = min(len(v.Message), v.Sy)
	}
	y0 := f.Sy - lines
	if v.StatusTop {
		y0 = 0
	}
	for i := range lines {
		line := f.Line(y0 + i)
		for x := range line {
			line[x] = vt.DefaultCell
		}
		if i < len(src) {
			copy(line, src[i])
		}
		// Theme colours become the client's colours, as tmux's tty does for every cell.
		for x := range line {
			gc := &line[x]
			gc.Fg, gc.Bg, gc.Us = v.theme(gc.Fg), v.theme(gc.Bg), v.theme(gc.Us)
		}
	}
}

// cursor sets where the cursor goes and the terminal's modes (tmux's
// server_client_reset_state, without overlays and prompts).
func (r *Renderer) cursor(f *Frame, v *View, sc *scene, areaY int) {
	p := v.Window.Active
	if p == nil || p.Term == nil {
		return
	}
	p.Mu.Lock()
	s := p.Term.Screen
	mode := s.Mode
	cx, cy := p.Xoff+s.Cx, p.Yoff+s.Cy
	f.Cursor = cursorState(s)
	p.Mu.Unlock()
	visible := cx >= sc.ox && cx <= sc.ox+sc.sx && cy >= sc.oy && cy <= sc.oy+sc.sy
	f.Cx, f.Cy = cx-sc.ox, cy-sc.oy+areaY
	if !visible || !paneVisible(p) || v.NoCursor {
		mode &^= vt.ModeCursor
	}
	if mode&vt.ModeSync != 0 {
		f.KeepCursor = true
	}
	if v.Mouse {
		mode &^= vt.AllMouseModes
		for _, q := range v.Window.Panes {
			q.Mu.Lock()
			all := q.Term != nil && q.Term.Screen.Mode&vt.ModeMouseAll != 0
			q.Mu.Unlock()
			if all {
				mode |= vt.ModeMouseAll
			}
		}
		if mode&vt.ModeMouseAll == 0 {
			mode |= vt.ModeMouseButton
		}
	}
	f.Mode = mode
}

// sameCell reports whether the terminal shows two cells the same way.
func sameCell(a, b *vt.Cell) bool {
	return a.Data == b.Data && a.Width == b.Width && a.Attr == b.Attr && a.Fg == b.Fg && a.Bg == b.Bg &&
		a.Us == b.Us && a.Link == b.Link && a.Flags&^vt.FlagCleared == b.Flags&^vt.FlagCleared
}

// runGap is how many unchanged cells between two changed ones are written again rather than
// skipped with a cursor movement, which costs about as much.
const runGap = 6

type run struct{ y, x0, x1 int } // cells x0 to x1 (inclusive) of line y

// diff returns the runs of f that differ from last (all of f when last is nil or another size).
func diff(last, f *Frame) []run {
	var runs []run
	if last == nil || last.Sx != f.Sx || last.Sy != f.Sy {
		for y := range f.Sy {
			if f.Sx > 0 {
				runs = append(runs, run{y, 0, f.Sx - 1})
			}
		}
		return runs
	}
	for y := range f.Sy {
		a, b := last.Line(y), f.Line(y)
		start, end := -1, -1
		for x := range b {
			if sameCell(&a[x], &b[x]) {
				continue
			}
			if start >= 0 && x-end-1 > runGap {
				runs = append(runs, widen(b, y, start, end))
				start = -1
			}
			if start < 0 {
				start = x
			}
			end = x
		}
		if start >= 0 {
			runs = append(runs, widen(b, y, start, end))
		}
	}
	// Widening can make adjacent runs overlap: join them.
	out := runs[:0]
	for _, rn := range runs {
		if n := len(out); n > 0 && out[n-1].y == rn.y && rn.x0 <= out[n-1].x1+1 {
			out[n-1].x1 = max(out[n-1].x1, rn.x1)
			continue
		}
		out = append(out, rn)
	}
	return out
}

// widen extends a run to whole wide characters.
func widen(line []vt.Cell, y, x0, x1 int) run {
	for x0 > 0 && line[x0].Flags&vt.FlagPadding != 0 {
		x0--
	}
	for x1+1 < len(line) && line[x1+1].Flags&vt.FlagPadding != 0 {
		x1++
	}
	return run{y, x0, x1}
}

// Draw writes what changed since the last frame drawn to tty, and sets the cursor and modes.
func (r *Renderer) Draw(tty *termout.TTY, f *Frame) {
	runs := diff(r.last, f)
	tty.SetLinks(r.links.lookup)
	before := tty.Buffered()
	if len(runs) > 0 {
		tty.SyncStart()
		tty.UpdateMode(tty.Mode()&^cursorModes, nil)
		tty.RegionOff()
		tty.MarginOff()
		for _, rn := range runs {
			tty.DrawCells(f.Line(rn.y)[rn.x0:rn.x1+1], rn.x0, rn.y)
		}
		tty.Reset()
	}
	tty.RegionOff()
	tty.MarginOff()
	mode, cs := f.Mode, f.Cursor
	if f.KeepCursor {
		mode = mode&^cursorModes | tty.Mode()&cursorModes
		cs = nil
	} else {
		tty.Cursor(f.Cx, f.Cy)
	}
	tty.UpdateMode(mode, cs)
	tty.Reset()
	tty.SyncEnd()
	r.Written += tty.Buffered() - before
	if r.last != f {
		r.spare = r.last
	}
	r.last = f
}

// linkTable gives the hyperlinks of every pane one set of ids, so that a frame's cells can be
// compared and written without their panes.
type linkTable struct {
	ids  map[[2]string]uint32
	list [][2]string // uri, id by id-1
	full bool        // it filled up and started again during this frame
}

// maxLinks bounds the table; when it is full it starts again (and the frame is drawn whole).
const maxLinks = 1 << 16

func (lt *linkTable) reset() {
	lt.ids = nil
	lt.list = nil
	lt.full = false
}

func (lt *linkTable) intern(hl *vt.Hyperlinks, inner uint32) uint32 {
	uri, id, ok := hl.External(inner)
	if !ok {
		return 0
	}
	k := [2]string{uri, id}
	if n, ok := lt.ids[k]; ok {
		return n
	}
	if len(lt.list) >= maxLinks {
		lt.ids, lt.list, lt.full = nil, nil, true
	}
	if lt.ids == nil {
		lt.ids = map[[2]string]uint32{}
	}
	lt.list = append(lt.list, k)
	n := uint32(len(lt.list))
	lt.ids[k] = n
	return n
}

func (lt *linkTable) lookup(n uint32) (uri, id string, ok bool) {
	if n == 0 || int(n) > len(lt.list) {
		return "", "", false
	}
	k := lt.list[n-1]
	return k[0], k[1], true
}
