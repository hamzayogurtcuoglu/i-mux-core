// Ported from tmux 3.8's grid-reader.c and grid.c's grid_in_set (see copymode.go for the
// copyright notice).

package copymode

import (
	"strings"
	"unicode/utf8"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/vt"
)

// reader is tmux's grid_reader: a cursor over the copy's lines.
type reader struct {
	gd     *vt.Grid
	cx, cy int
}

func (r *reader) lineLength() int { return r.gd.LineLength(r.cy) }

func (r *reader) wrapped(py int) bool { return r.gd.Line(py).Flags&vt.LineWrapped != 0 }

func (r *reader) last() int { return r.gd.HSize + r.gd.Sy - 1 }

func (r *reader) padding() bool { return r.gd.Cell(r.cx, r.cy).Flags&vt.FlagPadding != 0 }

// right is grid_reader_cursor_right.
func (r *reader) right(wrap, all, onemore bool) {
	var px int
	switch {
	case all:
		px = r.gd.Sx
	case onemore:
		px = r.lineLength()
	default:
		px = r.gd.LineLimit(r.cy)
	}
	if wrap && r.cx >= px && r.cy < r.last() {
		r.startOfLine(false)
		r.down()
	} else if r.cx < px {
		r.cx++
		for r.cx < px && r.padding() {
			r.cx++
		}
	}
}

// left is grid_reader_cursor_left.
func (r *reader) left(wrap bool) {
	for r.cx > 0 && r.padding() {
		r.cx--
	}
	if r.cx == 0 && r.cy > 0 && (wrap || r.wrapped(r.cy-1)) {
		r.up()
		r.endOfLine(false, false)
	} else if r.cx > 0 {
		r.cx--
	}
}

// down and up are grid_reader_cursor_down and _up.
func (r *reader) down() {
	if r.cy < r.last() {
		r.cy++
	}
	for r.cx > 0 && r.padding() {
		r.cx--
	}
}

func (r *reader) up() {
	if r.cy > 0 {
		r.cy--
	}
	for r.cx > 0 && r.padding() {
		r.cx--
	}
}

// startOfLine and endOfLine are grid_reader_cursor_start_of_line and _end_of_line.
func (r *reader) startOfLine(wrap bool) {
	if wrap {
		for r.cy > 0 && r.wrapped(r.cy-1) {
			r.cy--
		}
	}
	r.cx = 0
}

func (r *reader) endOfLine(wrap, all bool) {
	if wrap {
		for r.cy < r.last() && r.wrapped(r.cy) {
			r.cy++
		}
	}
	if all {
		r.cx = r.gd.Sx
	} else {
		r.cx = r.lineLength()
	}
}

func (r *reader) inSet(set string) bool { return inSet(r.gd, r.cx, r.cy, set) }

// handleWrap is grid_reader_handle_wrap.
func (r *reader) handleWrap(xx, yy *int) bool {
	for r.cx > *xx {
		if r.cy == *yy {
			return false
		}
		r.startOfLine(false)
		r.down()
		if r.wrapped(r.cy) {
			*xx = r.gd.Sx - 1
		} else {
			*xx = r.lineLength()
		}
	}
	return true
}

// nextWordEnd is grid_reader_cursor_next_word_end.
func (r *reader) nextWordEnd(separators string) {
	xx := r.lineLength()
	if r.wrapped(r.cy) {
		xx = r.gd.Sx - 1
	}
	yy := r.last()
	for r.handleWrap(&xx, &yy) {
		switch {
		case r.inSet(whitespace):
			r.cx++
		case r.inSet(separators):
			for {
				r.cx++
				if r.handleWrap(&xx, &yy) && r.inSet(separators) && !r.inSet(whitespace) {
					continue
				}
				break
			}
			return
		default:
			for {
				r.cx++
				if r.handleWrap(&xx, &yy) && !r.inSet(whitespace) && !r.inSet(separators) {
					continue
				}
				break
			}
			return
		}
	}
}

// previousWord is grid_reader_cursor_previous_word.
func (r *reader) previousWord(separators string, already, stopAtEOL bool) {
	var wordIsLetters bool
	if already || r.inSet(whitespace) {
	loop:
		for {
			if r.cx > 0 {
				r.cx--
				if !r.inSet(whitespace) {
					wordIsLetters = !r.inSet(separators)
					break
				}
			} else {
				if r.cy == 0 {
					return
				}
				r.up()
				r.endOfLine(false, false)
				if stopAtEOL && r.cx > 0 {
					oldx := r.cx
					r.cx--
					atEOL := r.inSet(whitespace)
					r.cx = oldx
					if atEOL {
						wordIsLetters = false
						break loop
					}
				}
			}
		}
	} else {
		wordIsLetters = !r.inSet(separators)
	}
	var oldx, oldy int
	for {
		oldx, oldy = r.cx, r.cy
		if r.cx == 0 {
			if r.cy == 0 || !r.wrapped(r.cy-1) {
				break
			}
			r.up()
			r.endOfLine(false, true)
		}
		if r.cx > 0 {
			r.cx--
		}
		if r.inSet(whitespace) || wordIsLetters == r.inSet(separators) {
			break
		}
	}
	r.cx, r.cy = oldx, oldy
}

// inSet is grid_in_set: whether the cell at px, py is one of set's characters (tabs and any
// whitespace count when set has a tab or a space).
func inSet(gd *vt.Grid, px, py int, set string) bool {
	hasTab := strings.ContainsRune(set, '\t')
	hasSpace := strings.ContainsRune(set, ' ')
	gc := gd.Cell(px, py)
	if gc.Flags&vt.FlagPadding != 0 {
		if !hasTab && !hasSpace {
			return false
		}
		pxx := px
		var tmp vt.Cell
		for {
			pxx--
			tmp = gd.Cell(pxx, py)
			if pxx <= 0 || tmp.Flags&vt.FlagPadding == 0 {
				break
			}
		}
		if ((hasTab || hasSpace) && tmp.Flags&vt.FlagTab != 0) || (hasSpace && hasWhitespace(tmp.Data)) {
			return int(tmp.Width)-(px-pxx) != 0
		}
		return false
	}
	if (hasTab || hasSpace) && gc.Flags&vt.FlagTab != 0 {
		return true
	}
	if hasSpace && hasWhitespace(gc.Data) {
		return true
	}
	return gc.Data != "" && strings.Contains(set, gc.Data)
}

// hasWhitespace is utf8_has_whitespace.
func hasWhitespace(s string) bool {
	for _, r := range s {
		if r == utf8.RuneError {
			return false
		}
		switch r {
		case 0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x20, 0x85, 0xa0, 0x1680, 0x2000, 0x2001, 0x2002,
			0x2003, 0x2004, 0x2005, 0x2006, 0x2007, 0x2008, 0x2009, 0x200a, 0x2028, 0x2029,
			0x202f, 0x205f, 0x3000:
			return true
		}
	}
	return false
}
