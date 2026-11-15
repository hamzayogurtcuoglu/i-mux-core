package vt

import "testing"

func TestGridCellStorage(t *testing.T) {
	gd := NewGrid(10, 3, 100)
	plain := Cell{Data: "a", Width: 1, Fg: 1, Bg: 8, Us: 8}
	gd.SetCell(0, 0, &plain)
	if gd.Line(0).Flags&LineExtended != 0 {
		t.Fatal("a plain ASCII cell made the line extended")
	}
	wide := Cell{Data: "日", Width: 2, Fg: 8, Bg: 8, Us: 8}
	rgb := Cell{Data: "b", Width: 1, Fg: ColorFlagRGB | 0x102030, Bg: 8, Us: 8}
	p256 := Cell{Data: "c", Width: 1, Fg: ColorFlag256 | 200, Bg: ColorFlag256 | 17, Us: 8}
	gd.SetCell(1, 0, &wide)
	gd.SetPadding(2, 0, 8)
	gd.SetCell(3, 0, &rgb)
	gd.SetCell(4, 0, &p256)
	for x, want := range []Cell{plain, wide, {}, rgb, p256} {
		if x == 2 {
			continue
		}
		if got := gd.Cell(x, 0); !CellsEqual(&got, &want) || got.Us != want.Us {
			t.Errorf("cell %d = %+v, want %+v", x, got, want)
		}
	}
	if pad := gd.Cell(2, 0); pad.Flags&FlagPadding == 0 || pad.Data != "!" || pad.Width != 1 {
		t.Errorf("padding reads back as %+v; tmux gives \"!\", width 1, padding", pad)
	}
	if got := gd.Line(0).Used(); got != 5 {
		t.Errorf("used = %d, want 5", got)
	}
	// Past what was written: the default cell.
	if got := gd.Cell(9, 2); !CellsEqual(&got, &DefaultCell) {
		t.Errorf("unwritten cell %+v", got)
	}
}

func TestGridHistory(t *testing.T) {
	gd := NewGrid(5, 2, 10)
	for i := 0; i < 25; i++ {
		c := Cell{Data: string(rune('a' + i%26)), Width: 1, Fg: 8, Bg: 8, Us: 8}
		gd.ViewSetCell(0, 1, &c)
		gd.ViewScrollRegionUp(0, 1, 8)
	}
	if gd.HSize > 10 || gd.HSize < 9 {
		t.Fatalf("history %d lines, limit 10", gd.HSize)
	}
	if len(gd.lines) != gd.HSize+gd.Sy {
		t.Fatalf("%d lines kept for %d+%d", len(gd.lines), gd.HSize, gd.Sy)
	}
	// The last character written ('y') is on the screen's top line; the newest history line
	// holds the one before it.
	if got := gd.ViewCell(0, 0).Data; got != "y" {
		t.Fatalf("top screen line starts %q, want y", got)
	}
	if got := gd.Cell(0, gd.HSize-1).Data; got != "x" {
		t.Fatalf("newest history line starts %q, want x", got)
	}
	gd.ClearHistory()
	if gd.HSize != 0 || len(gd.lines) != gd.Sy {
		t.Fatalf("after ClearHistory: %d history, %d lines", gd.HSize, len(gd.lines))
	}
}

func TestGridClearKeepsBackground(t *testing.T) {
	gd := NewGrid(10, 2, 0)
	gd.Clear(2, 0, 3, 1, 4)
	if got := gd.Cell(3, 0); got.Bg != 4 || got.Flags&FlagCleared == 0 {
		t.Fatalf("cleared cell %+v", got)
	}
	gd.Clear(2, 1, 3, 1, ColorFlagRGB|0xff0000)
	if got := gd.Cell(4, 1); got.Bg != ColorFlagRGB|0xff0000 {
		t.Fatalf("RGB background lost: %+v", got)
	}
	if got := gd.Line(0).Used(); got != 0 {
		t.Fatalf("clearing counts as writing: used %d", got)
	}
}
