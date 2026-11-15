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

	tabs []bool

	Title string
	Path  string

	// VS16Narrow turns off tmux's variation-selector-always-wide (on by default): a character
	// followed by U+FE0F is then not made wide.
	VS16Narrow bool
}

// noSavedCursor stands for tmux's UINT_MAX saved cursor: none saved.
const noSavedCursor = -1

// NewScreen returns a sx×sy screen keeping up to hlimit lines of history.
func NewScreen(sx, sy, hlimit int) *Screen {
	s := &Screen{
		Grid:           NewGrid(sx, sy, hlimit),
		Mode:           ModeCursor,
		CColour:        -1,
		DefaultCColour: -1,
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

// AlternateOn and AlternateOff are filled in with the modes (C1.6).
func (s *Screen) AlternateOn(gc *Cell, cursor bool) {}

// AlternateOff leaves the alternate screen.
func (s *Screen) AlternateOff(gc *Cell, cursor bool) { s.savedGrid = nil }
