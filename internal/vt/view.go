// Copyright (c) 2008 Nicholas Marriott <nicholas.marriott@gmail.com>
//
// Permission to use, copy, modify, and distribute this software for any
// purpose with or without fee is hereby granted, provided that the above
// copyright notice and this permission notice appear in all copies.
//
// THE SOFTWARE IS PROVIDED "AS IS" AND THE AUTHOR DISCLAIMS ALL WARRANTIES
// WITH REGARD TO THIS SOFTWARE INCLUDING ALL IMPLIED WARRANTIES OF
// MERCHANTABILITY AND FITNESS. IN NO EVENT SHALL THE AUTHOR BE LIABLE FOR
// ANY SPECIAL, DIRECT, INDIRECT, OR CONSEQUENTIAL DAMAGES OR ANY DAMAGES
// WHATSOEVER RESULTING FROM LOSS OF MIND, USE, DATA OR PROFITS, WHETHER
// IN AN ACTION OF CONTRACT, NEGLIGENCE OR OTHER TORTIOUS ACTION, ARISING
// OUT OF OR IN CONNECTION WITH THE USE OR PERFORMANCE OF THIS SOFTWARE.

package vt

// The view functions are ported from tmux 3.8's grid-view.c: they take coordinates on the
// screen (y 0 is the top line of the screen, not of the history).

func (gd *Grid) viewY(y int) int { return gd.HSize + y }

// ViewCell returns the cell at px, py on the screen.
func (gd *Grid) ViewCell(px, py int) Cell { return gd.Cell(px, gd.viewY(py)) }

// ViewSetCell sets the cell at px, py on the screen.
func (gd *Grid) ViewSetCell(px, py int, gc *Cell) { gd.SetCell(px, gd.viewY(py), gc) }

// ViewSetPadding makes the cell at px, py on the screen the second half of a wide character.
func (gd *Grid) ViewSetPadding(px, py int, bg Color) { gd.SetPadding(px, gd.viewY(py), bg) }

// ViewSetCells sets cells from px, py on the screen with ASCII bytes in gc's style.
func (gd *Grid) ViewSetCells(px, py int, gc *Cell, s []byte) { gd.SetCells(px, gd.viewY(py), gc, s) }

// ViewLine returns line py of the screen.
func (gd *Grid) ViewLine(py int) *Line { return &gd.lines[gd.viewY(py)] }

// ViewClearHistory moves the screen's used lines into the history and clears the screen
// (ED 3 with ED 2's effect, as tmux's clear-history-on-clear).
func (gd *Grid) ViewClearHistory(bg Color) {
	last := 0
	for yy := 0; yy < gd.Sy; yy++ {
		if gd.lines[gd.viewY(yy)].used != 0 {
			last = yy + 1
		}
	}
	if last == 0 {
		gd.ViewClear(0, 0, gd.Sx, gd.Sy, bg)
		return
	}
	for yy := 0; yy < last; yy++ {
		gd.CollectHistory(false)
		gd.ScrollHistory(bg)
	}
	if last < gd.Sy {
		gd.ViewClear(0, 0, gd.Sx, gd.Sy-last, bg)
	}
	gd.HScrolled = 0
}

// ViewClear clears nx×ny cells from px, py on the screen.
func (gd *Grid) ViewClear(px, py, nx, ny int, bg Color) { gd.Clear(px, gd.viewY(py), nx, ny, bg) }

// ViewScrollRegionUp scrolls lines rupper to rlower up one, into the history when the grid
// keeps one.
func (gd *Grid) ViewScrollRegionUp(rupper, rlower int, bg Color) {
	if gd.History() {
		gd.CollectHistory(false)
		if rupper == 0 && rlower == gd.Sy-1 {
			gd.ScrollHistory(bg)
		} else {
			gd.ScrollHistoryRegion(gd.viewY(rupper), gd.viewY(rlower), bg)
		}
		return
	}
	rupper, rlower = gd.viewY(rupper), gd.viewY(rlower)
	gd.MoveLines(rupper, rupper+1, rlower-rupper, bg)
}

// ViewScrollRegionDown scrolls lines rupper to rlower down one.
func (gd *Grid) ViewScrollRegionDown(rupper, rlower int, bg Color) {
	rupper, rlower = gd.viewY(rupper), gd.viewY(rlower)
	gd.MoveLines(rupper+1, rupper, rlower-rupper, bg)
}

// ViewInsertLines inserts ny lines at py.
func (gd *Grid) ViewInsertLines(py, ny int, bg Color) {
	py = gd.viewY(py)
	sy := gd.viewY(gd.Sy)
	gd.MoveLines(py+ny, py, sy-py-ny, bg)
}

// ViewInsertLinesRegion inserts ny lines at py, inside a region ending at rlower.
func (gd *Grid) ViewInsertLinesRegion(rlower, py, ny int, bg Color) {
	rlower = gd.viewY(rlower)
	py = gd.viewY(py)
	ny2 := rlower + 1 - py - ny
	gd.MoveLines(rlower+1-ny2, py, ny2, bg)
	gd.Clear(0, py+ny2, gd.Sx, ny-ny2, bg)
}

// ViewDeleteLines deletes ny lines at py.
func (gd *Grid) ViewDeleteLines(py, ny int, bg Color) {
	py = gd.viewY(py)
	sy := gd.viewY(gd.Sy)
	gd.MoveLines(py, py+ny, sy-py-ny, bg)
	gd.Clear(0, sy-ny, gd.Sx, ny, bg)
}

// ViewDeleteLinesRegion deletes ny lines at py, inside a region ending at rlower.
func (gd *Grid) ViewDeleteLinesRegion(rlower, py, ny int, bg Color) {
	rlower = gd.viewY(rlower)
	py = gd.viewY(py)
	ny2 := rlower + 1 - py - ny
	gd.MoveLines(py, py+ny, ny2, bg)
	gd.Clear(0, py+ny2, gd.Sx, ny-ny2, bg)
}

// ViewInsertCells inserts nx cells at px, py.
func (gd *Grid) ViewInsertCells(px, py, nx int, bg Color) {
	py = gd.viewY(py)
	sx := gd.Sx
	if px >= sx-1 {
		gd.Clear(px, py, 1, 1, bg)
	} else {
		gd.MoveCells(px+nx, px, py, sx-px-nx, bg)
	}
}

// ViewDeleteCells deletes nx cells at px, py.
func (gd *Grid) ViewDeleteCells(px, py, nx int, bg Color) {
	py = gd.viewY(py)
	sx := gd.Sx
	gd.MoveCells(px, px+nx, py, sx-px-nx, bg)
	gd.Clear(sx-nx, py, nx, 1, bg)
}
