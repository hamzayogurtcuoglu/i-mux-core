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

// The screen is ported from tmux 3.8's screen.c: a grid with a cursor, a scroll region, modes
// and tab stops.

// Modes (tmux's MODE_*).
const (
	ModeCursor            = 0x1
	ModeInsert            = 0x2
	ModeKCursor           = 0x4
	ModeKKeypad           = 0x8
	ModeWrap              = 0x10
	ModeMouseStandard     = 0x20
	ModeMouseButton       = 0x40
	ModeCursorBlinking    = 0x80
	ModeMouseUTF8         = 0x100
	ModeMouseSGR          = 0x200
	ModeBracketPaste      = 0x400
	ModeFocusOn           = 0x800
	ModeMouseAll          = 0x1000
	ModeOrigin            = 0x2000
	ModeCRLF              = 0x4000
	ModeKeysExtended      = 0x8000
	ModeCursorVeryVisible = 0x10000
	ModeCursorBlinkingSet = 0x20000
	ModeKeysExtended2     = 0x40000
	ModeThemeUpdates      = 0x80000
	ModeSync              = 0x100000

	AllMouseModes    = ModeMouseStandard | ModeMouseButton | ModeMouseAll
	ExtendedKeyModes = ModeKeysExtended | ModeKeysExtended2
)

// CursorStyle is the cursor's shape (DECSCUSR).
type CursorStyle int

// Cursor styles.
const (
	CursorDefault CursorStyle = iota
	CursorBlock
	CursorUnderline
	CursorBar
)

// Screen is a pane's screen: its grid and history, cursor, scroll region and modes.
type Screen struct {
	Grid *Grid

	Cx, Cy int

	CStyle         CursorStyle
	DefaultCStyle  CursorStyle
	CColour        Color
	DefaultCColour Color

	Rupper, Rlower int

	Mode        int
	DefaultMode int

	savedCx, savedCy int
	savedGrid        *Grid
	savedCell        Cell
	savedFlags       int

	tabs []bool

	Title             string
	Titles            []string
	DisallowSetTitle  bool
	Path              string
	Cwd               string
	Hyperlinks        *Hyperlinks
	ProgressState     int
	ProgressValue     int
	LastCommandStatus int

	// NoAlternateScreen disables switching to the alternate screen (tmux's alternate-screen
	// option when set to off).
	NoAlternateScreen bool

	// VS16Narrow turns off tmux's variation-selector-always-wide (on by default): a character
	// followed by U+FE0F is then not made wide.
	VS16Narrow bool
}

// noSavedCursor stands for tmux's UINT_MAX saved cursor: none saved.
const noSavedCursor = -1

// NewScreen returns a sx×sy screen keeping up to hlimit lines of history.
func NewScreen(sx, sy, hlimit int) *Screen {
	s := &Screen{
		Grid:              NewGrid(sx, sy, hlimit),
		Mode:              ModeCursor,
		CColour:           -1,
		DefaultCColour:    -1,
		LastCommandStatus: -1,
	}
	s.Reinit()
	return s
}

// SizeX and SizeY are the screen's size.
func (s *Screen) SizeX() int { return s.Grid.Sx }

// SizeY is the screen's height.
func (s *Screen) SizeY() int { return s.Grid.Sy }

// Reinit resets the screen as tmux's screen_reinit does (RIS).
func (s *Screen) Reinit() {
	s.Cx, s.Cy = 0, 0
	s.Rupper, s.Rlower = 0, s.SizeY()-1
	s.Mode = ModeCursor | ModeWrap | (s.Mode & ModeCRLF)
	if s.IsAlternate() {
		s.AlternateOff(nil, false)
	}
	s.savedCx, s.savedCy = noSavedCursor, noSavedCursor
	s.ResetTabs()
	s.Grid.ClearLines(s.Grid.HSize, s.Grid.Sy, 8)
	s.Titles = nil
	s.ProgressState, s.ProgressValue = 0, 0
	s.ResetHyperlinks()
}

// ResetHyperlinks clears or initializes the screen's hyperlink table.
func (s *Screen) ResetHyperlinks() {
	if s.Hyperlinks == nil {
		s.Hyperlinks = NewHyperlinks()
	} else {
		s.Hyperlinks.Reset()
	}
}

// ResetTabs sets a tab stop every 8 columns.
func (s *Screen) ResetTabs() {
	s.tabs = make([]bool, s.SizeX())
	for i := 8; i < s.SizeX(); i += 8 {
		s.tabs[i] = true
	}
}

// IsTab reports whether there is a tab stop at x.
func (s *Screen) IsTab(x int) bool { return x >= 0 && x < len(s.tabs) && s.tabs[x] }

// SetTab sets or clears the tab stop at x.
func (s *Screen) SetTab(x int, on bool) {
	if x >= 0 && x < len(s.tabs) {
		s.tabs[x] = on
	}
}

// ClearTabs clears every tab stop.
func (s *Screen) ClearTabs() {
	for i := range s.tabs {
		s.tabs[i] = false
	}
}

// IsAlternate reports whether the alternate screen is on.
func (s *Screen) IsAlternate() bool { return s.savedGrid != nil }

// SetCursorStyle sets the cursor style from DECSCUSR's parameter.
func (s *Screen) SetCursorStyle(style int) {
	setCursorStyle(style, &s.CStyle, &s.Mode)
}

func setCursorStyle(style int, cstyle *CursorStyle, mode *int) {
	switch style {
	case 0:
		*cstyle = CursorDefault
	case 1:
		*cstyle = CursorBlock
		*mode |= ModeCursorBlinking
	case 2:
		*cstyle = CursorBlock
		*mode &^= ModeCursorBlinking
	case 3:
		*cstyle = CursorUnderline
		*mode |= ModeCursorBlinking
	case 4:
		*cstyle = CursorUnderline
		*mode &^= ModeCursorBlinking
	case 5:
		*cstyle = CursorBar
		*mode |= ModeCursorBlinking
	case 6:
		*cstyle = CursorBar
		*mode &^= ModeCursorBlinking
	}
}

// AlternateOn enters the alternate screen, saving the visible screen and disabling history
// updates (tmux's screen_alternate_on). When cursor is true (mode 1049), the cursor position is
// saved as well.
func (s *Screen) AlternateOn(gc *Cell, cursor bool) bool {
	if s.NoAlternateScreen || s.IsAlternate() {
		return false
	}
	sx, sy := s.SizeX(), s.SizeY()
	s.savedGrid = NewGrid(sx, sy, 0)
	s.savedGrid.DuplicateLines(0, s.Grid, s.Grid.HSize, sy)
	if cursor {
		s.savedCx = s.Cx
		s.savedCy = s.Cy
	}
	if gc != nil {
		s.savedCell = *gc
	}
	s.Grid.ViewClear(0, 0, sx, sy, ColorDefault)
	s.savedFlags = s.Grid.flags
	s.Grid.flags &^= gridHistory
	return true
}

// AlternateOff leaves the alternate screen and restores the saved grid (tmux's
// screen_alternate_off). When cursor is true (mode 1049), a previously saved cursor position and
// cell style are restored even if not currently on the alternate screen.
func (s *Screen) AlternateOff(gc *Cell, cursor bool) bool {
	if s.NoAlternateScreen {
		return false
	}
	// If the size changed while in the alternate screen, go back to the saved grid's size
	// before copying it back.
	sx, sy := s.SizeX(), s.SizeY()
	if s.IsAlternate() {
		s.Resize(s.savedGrid.Sx, s.savedGrid.Sy, false)
	}
	if cursor && s.savedCx != noSavedCursor && s.savedCy != noSavedCursor {
		s.Cx = s.savedCx
		s.Cy = s.savedCy
		if gc != nil {
			*gc = s.savedCell
		}
	}
	if !s.IsAlternate() {
		if s.Cx > s.SizeX()-1 {
			s.Cx = s.SizeX() - 1
		}
		if s.Cy > s.SizeY()-1 {
			s.Cy = s.SizeY() - 1
		}
		return false
	}
	s.Grid.DuplicateLines(s.Grid.HSize, s.savedGrid, 0, s.savedGrid.Sy)
	// Turn history back on (so resize can use it), then resize back to the current size.
	if s.savedFlags&gridHistory != 0 {
		s.Grid.flags |= gridHistory
	}
	s.Resize(sx, sy, true)
	s.savedGrid = nil
	if s.Cx > s.SizeX()-1 {
		s.Cx = s.SizeX() - 1
	}
	if s.Cy > s.SizeY()-1 {
		s.Cy = s.SizeY() - 1
	}
	return true
}

// HasMode reports whether all bits in m are set in s.Mode.
func (s *Screen) HasMode(m int) bool { return s.Mode&m == m }

// CursorVisible reports whether DECTCEM (mode 25) is on.
func (s *Screen) CursorVisible() bool { return s.Mode&ModeCursor != 0 }

// InsertMode reports whether IRM (SM 4) is on.
func (s *Screen) InsertMode() bool { return s.Mode&ModeInsert != 0 }

// ApplicationCursor reports whether DECCKM (mode 1) is on.
func (s *Screen) ApplicationCursor() bool { return s.Mode&ModeKCursor != 0 }

// ApplicationKeypad reports whether DECKPAM is on.
func (s *Screen) ApplicationKeypad() bool { return s.Mode&ModeKKeypad != 0 }

// AutoWrap reports whether DECAWM (mode 7) is on.
func (s *Screen) AutoWrap() bool { return s.Mode&ModeWrap != 0 }

// OriginMode reports whether DECOM (mode 6) is on.
func (s *Screen) OriginMode() bool { return s.Mode&ModeOrigin != 0 }

// BracketedPaste reports whether mode 2004 is on.
func (s *Screen) BracketedPaste() bool { return s.Mode&ModeBracketPaste != 0 }

// FocusEvents reports whether mode 1004 is on.
func (s *Screen) FocusEvents() bool { return s.Mode&ModeFocusOn != 0 }

// SynchronizedOutput reports whether mode 2026 is on.
func (s *Screen) SynchronizedOutput() bool { return s.Mode&ModeSync != 0 }

// ThemeUpdates reports whether mode 2031 is on.
func (s *Screen) ThemeUpdates() bool { return s.Mode&ModeThemeUpdates != 0 }

// MouseAny reports whether any mouse tracking mode (1000, 1002, 1003) is on.
func (s *Screen) MouseAny() bool { return s.Mode&AllMouseModes != 0 }

// MouseStandard reports whether normal mouse tracking (mode 1000) is on.
func (s *Screen) MouseStandard() bool { return s.Mode&ModeMouseStandard != 0 }

// MouseButton reports whether button-event mouse tracking (mode 1002) is on.
func (s *Screen) MouseButton() bool { return s.Mode&ModeMouseButton != 0 }

// MouseAll reports whether any-event mouse tracking (mode 1003) is on.
func (s *Screen) MouseAll() bool { return s.Mode&ModeMouseAll != 0 }

// MouseUTF8 reports whether UTF-8 mouse encoding (mode 1005) is on.
func (s *Screen) MouseUTF8() bool { return s.Mode&ModeMouseUTF8 != 0 }

// MouseSGR reports whether SGR mouse encoding (mode 1006) is on.
func (s *Screen) MouseSGR() bool { return s.Mode&ModeMouseSGR != 0 }

// Resize changes the screen's size, as tmux's screen_resize: reflowing lines to a new width
// when reflow is set, and moving lines between the screen and the history for a new height,
// with the cursor following its content.
func (s *Screen) Resize(sx, sy int, reflow bool) {
	gd := s.Grid
	cx, cy := s.Cx, gd.HSize+s.Cy
	sx, sy = max(sx, 1), max(sy, 1)
	if sx != s.SizeX() {
		gd.Sx = sx
		s.ResetTabs()
	} else {
		reflow = false
	}
	if sy != s.SizeY() {
		s.resizeY(sy, true, &cy)
	}
	if reflow {
		wx, wy := gd.WrapPosition(cx, cy)
		gd.Reflow(sx)
		cx, cy = gd.UnwrapPosition(wx, wy)
	}
	if cy >= gd.HSize {
		s.Cx, s.Cy = cx, cy-gd.HSize
	} else {
		s.Cx, s.Cy = 0, 0
	}
}

// resizeY is tmux's screen_resize_y: shrinking deletes blank lines below the cursor, then
// pushes lines into the history; growing pulls lines back out of the scrolled history, then
// adds blank ones.
func (s *Screen) resizeY(sy int, eatEmpty bool, cy *int) {
	gd := s.Grid
	oldy := s.SizeY()
	if sy < oldy {
		needed := oldy - sy
		if eatEmpty {
			available := oldy - 1 - s.Cy
			if available > 0 {
				available = min(available, needed)
				gd.ViewDeleteLines(oldy-available, available, 8)
			}
			needed -= max(available, 0)
		}
		available := s.Cy
		if gd.History() {
			gd.HScrolled += needed
			gd.HSize += needed
		} else if needed > 0 && available > 0 {
			available = min(available, needed)
			gd.ViewDeleteLines(0, available, 8)
			*cy -= available
		}
	}
	gd.AdjustLines(gd.HSize + sy)
	if sy > oldy {
		needed := sy - oldy
		available := gd.HScrolled
		if gd.History() && available > 0 {
			available = min(available, needed)
			gd.HScrolled -= available
			gd.HSize -= available
		} else {
			available = 0
		}
		needed -= available
		// tmux sized its line array before pulling lines out of the history; the line past
		// the screen it leaves unused is dropped here, so the array is the history and screen.
		gd.AdjustLines(gd.HSize + sy)
		for i := gd.HSize + sy - needed; i < gd.HSize+sy; i++ {
			gd.emptyLine(i, 8)
		}
	}
	gd.Sy = sy
	s.Rupper, s.Rlower = 0, s.SizeY()-1
}
