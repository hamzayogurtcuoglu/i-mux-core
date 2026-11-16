// Ported from tmux 3.8's style.c and attributes.c:
//
// Copyright (c) 2007 Nicholas Marriott <nicholas.marriott@gmail.com>
// Copyright (c) 2014 Tiago Cunha <tcunha@users.sourceforge.net>
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

package style

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/vt"
)

// Align is style_align.
type Align int

// Alignments.
const (
	AlignDefault Align = iota
	AlignLeft
	AlignCentre
	AlignRight
	AlignAbsoluteCentre
)

// List is style_list.
type List int

// List parts.
const (
	ListOff List = iota
	ListOn
	ListFocus
	ListLeftMarker
	ListRightMarker
)

// RangeType is style_range_type.
type RangeType int

// Range types.
const (
	RangeNone RangeType = iota
	RangeLeft
	RangeRight
	RangePane
	RangeWindow
	RangeSession
	RangeUser
	RangeControl
)

// DefaultType is style_default_type.
type DefaultType int

// What a style does to the default style.
const (
	DefaultBase DefaultType = iota
	DefaultPush
	DefaultPop
	DefaultSet
)

// rangeStringSize is the size of tmux's range_string: a user range keeps 15 bytes.
const rangeStringSize = 16

// Style is tmux's struct style: a cell's colours and attributes, and the status line's
// alignment, list, range, fill, width and padding.
type Style struct {
	Cell        vt.Cell
	Ignore      bool
	Dim         int
	Fill        vt.Color
	Align       Align
	List        List
	RangeType   RangeType
	RangeArg    uint32
	RangeString string
	Width       int // -1: none
	WidthPct    bool
	Pad         int // -1: none
	Default     DefaultType
	// Link is the URI of a link= style ("" for none).
	Link string
}

// Default is style_default.
func Default() Style {
	return Style{Cell: vt.DefaultCell, Fill: 8, Width: -1, Pad: -1}
}

// FromCell is style_set: the default style with gc's colours and attributes.
func FromCell(gc vt.Cell) Style {
	sy := Default()
	sy.Cell = gc
	return sy
}

// attrNames is attributes_fromstring's table.
var attrNames = []struct {
	name string
	attr uint16
}{
	{"acs", vt.AttrCharset},
	{"bright", vt.AttrBright},
	{"bold", vt.AttrBright},
	{"dim", vt.AttrDim},
	{"underscore", vt.AttrUnderscore},
	{"blink", vt.AttrBlink},
	{"reverse", vt.AttrReverse},
	{"hidden", vt.AttrHidden},
	{"italics", vt.AttrItalics},
	{"strikethrough", vt.AttrStrikethrough},
	{"double-underscore", vt.AttrUnderscore2},
	{"curly-underscore", vt.AttrUnderscore3},
	{"dotted-underscore", vt.AttrUnderscore4},
	{"dashed-underscore", vt.AttrUnderscore5},
	{"overline", vt.AttrOverline},
}

// AttributesFromString is attributes_fromstring: names separated by space, comma or |.
func AttributesFromString(s string) (uint16, bool) {
	const delimiters = " ,|"
	if s == "" || strings.IndexAny(s[:1], delimiters) == 0 || strings.ContainsAny(s[len(s)-1:], delimiters) {
		return 0, false
	}
	if strings.EqualFold(s, "default") || strings.EqualFold(s, "none") {
		return 0, true
	}
	var attr uint16
	for s != "" {
		end := strings.IndexAny(s, delimiters)
		if end < 0 {
			end = len(s)
		}
		word := s[:end]
		found := false
		for _, a := range attrNames {
			if strings.EqualFold(word, a.name) {
				attr |= a.attr
				found = true
				break
			}
		}
		if !found {
			return 0, false
		}
		s = strings.TrimLeft(s[end:], delimiters)
	}
	return attr, true
}

// AttributesToString is attributes_tostring.
func AttributesToString(attr uint16) string {
	if attr == 0 {
		return "none"
	}
	var parts []string
	for _, a := range []struct {
		attr uint16
		name string
	}{
		{vt.AttrCharset, "acs"},
		{vt.AttrBright, "bright"},
		{vt.AttrDim, "dim"},
		{vt.AttrUnderscore, "underscore"},
		{vt.AttrBlink, "blink"},
		{vt.AttrReverse, "reverse"},
		{vt.AttrHidden, "hidden"},
		{vt.AttrItalics, "italics"},
		{vt.AttrStrikethrough, "strikethrough"},
		{vt.AttrUnderscore2, "double-underscore"},
		{vt.AttrUnderscore3, "curly-underscore"},
		{vt.AttrUnderscore4, "dotted-underscore"},
		{vt.AttrUnderscore5, "dashed-underscore"},
		{vt.AttrOverline, "overline"},
		{vt.AttrNoAttr, "noattr"},
	} {
		if attr&a.attr != 0 {
			parts = append(parts, a.name)
		}
	}
	return strings.Join(parts, ",")
}

// errBad is a style that does not parse.
var errBad = errors.New("bad style")

// strtonum is strtonum(3).
func strtonum(s string, lo, hi int64) (int64, bool) {
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n < lo || n > hi {
		return 0, false
	}
	return n, true
}

// Parse is style_parse: in's words (separated by space, comma or newline) are added to the
// style; on an error the style is left as it was. base is what "default" and fg=default go
// back to.
func (sy *Style) Parse(base vt.Cell, in string) error {
	if in == "" {
		return nil
	}
	saved := *sy
	if err := sy.parse(base, in); err != nil {
		*sy = saved
		return err
	}
	return nil
}

func (sy *Style) parse(base vt.Cell, in string) error {
	const delimiters = " ,\n"
	for {
		in = strings.TrimLeft(in, delimiters)
		if in == "" {
			return nil
		}
		end := strings.IndexAny(in, delimiters)
		if end < 0 {
			end = len(in)
		}
		if end > 255 {
			return errBad
		}
		tmp := in[:end]
		lower := strings.ToLower(tmp)
		switch {
		case lower == "default":
			sy.Cell.Fg, sy.Cell.Bg, sy.Cell.Us = base.Fg, base.Bg, base.Us
			sy.Cell.Attr, sy.Cell.Flags = base.Attr, base.Flags
			sy.Link = ""
		case lower == "ignore":
			sy.Ignore = true
		case lower == "noignore":
			sy.Ignore = false
		case lower == "push-default":
			sy.Default = DefaultPush
		case lower == "pop-default":
			sy.Default = DefaultPop
		case lower == "set-default":
			sy.Default = DefaultSet
		case lower == "nolist":
			sy.List = ListOff
		case strings.HasPrefix(lower, "list="):
			switch lower[5:] {
			case "on":
				sy.List = ListOn
			case "focus":
				sy.List = ListFocus
			case "left-marker":
				sy.List = ListLeftMarker
			case "right-marker":
				sy.List = ListRightMarker
			default:
				return errBad
			}
		case lower == "norange":
			sy.RangeType, sy.RangeArg, sy.RangeString = RangeNone, 0, ""
		case end > 6 && strings.HasPrefix(lower, "range="):
			if err := sy.parseRange(tmp[6:]); err != nil {
				return err
			}
		case lower == "noalign":
			sy.Align = AlignDefault
		case end > 6 && strings.HasPrefix(lower, "align="):
			switch lower[6:] {
			case "left":
				sy.Align = AlignLeft
			case "centre":
				sy.Align = AlignCentre
			case "right":
				sy.Align = AlignRight
			case "absolute-centre":
				sy.Align = AlignAbsoluteCentre
			default:
				return errBad
			}
		case end > 5 && strings.HasPrefix(lower, "fill="):
			c := vt.ColorFromString(tmp[5:])
			if c == -1 {
				return errBad
			}
			sy.Fill = c
		case end > 4 && strings.HasPrefix(lower, "dim="):
			v := strings.TrimSuffix(tmp[4:], "%")
			n, ok := strtonum(v, 0, 100)
			if !ok {
				return errBad
			}
			sy.Dim = int(n)
		case end > 3 && lower[1:3] == "g=":
			c := vt.ColorFromString(tmp[3:])
			if c == -1 {
				return errBad
			}
			switch lower[0] {
			case 'f':
				if c != 8 {
					sy.Cell.Fg = c
				} else {
					sy.Cell.Fg = base.Fg
				}
			case 'b':
				if c != 8 {
					sy.Cell.Bg = c
				} else {
					sy.Cell.Bg = base.Bg
				}
			default:
				return errBad
			}
		case end > 3 && strings.HasPrefix(lower, "us="):
			c := vt.ColorFromString(tmp[3:])
			if c == -1 {
				return errBad
			}
			if c != 8 {
				sy.Cell.Us = c
			} else {
				sy.Cell.Us = base.Us
			}
		case lower == "none":
			sy.Cell.Attr = 0
		case end > 2 && strings.HasPrefix(lower, "no"):
			switch tmp[2:] {
			case "link":
				sy.Link = ""
			case "attr":
				sy.Cell.Attr |= vt.AttrNoAttr
			default:
				a, ok := AttributesFromString(tmp[2:])
				if !ok {
					return errBad
				}
				sy.Cell.Attr &^= a
			}
		case end > 6 && strings.HasPrefix(lower, "width="):
			v := tmp[6:]
			if end > 7 && strings.HasSuffix(v, "%") {
				n, ok := strtonum(v[:len(v)-1], 0, 100)
				if !ok {
					return errBad
				}
				sy.Width, sy.WidthPct = int(n), true
			} else {
				n, ok := strtonum(v, 0, 1<<32-1)
				if !ok {
					return errBad
				}
				sy.Width, sy.WidthPct = int(n), false
			}
		case end > 4 && strings.HasPrefix(lower, "pad="):
			n, ok := strtonum(tmp[4:], 0, 1<<32-1)
			if !ok {
				return errBad
			}
			sy.Pad = int(n)
		case strings.HasPrefix(lower, "link="):
			sy.Link = tmp[5:]
		default:
			a, ok := AttributesFromString(tmp)
			if !ok {
				return errBad
			}
			sy.Cell.Attr |= a
		}
		in = in[end:]
	}
}

// parseRange is range= of style_parse.
func (sy *Style) parseRange(v string) error {
	name, arg, hasArg := strings.Cut(v, "|")
	if hasArg && arg == "" {
		return errBad
	}
	set := func(t RangeType, n uint32, s string) {
		sy.RangeType, sy.RangeArg, sy.RangeString = t, n, s
	}
	switch strings.ToLower(name) {
	case "left", "right":
		if hasArg {
			return errBad
		}
		t := RangeLeft
		if strings.EqualFold(name, "right") {
			t = RangeRight
		}
		set(t, 0, "")
	case "control":
		if !hasArg {
			return errBad
		}
		n, ok := strtonum(arg, 0, 9)
		if !ok {
			return errBad
		}
		set(RangeControl, uint32(n), "")
	case "pane", "session":
		prefix := "%"
		t := RangePane
		if strings.EqualFold(name, "session") {
			prefix, t = "$", RangeSession
		}
		if !hasArg || !strings.HasPrefix(arg, prefix) || len(arg) == 1 {
			return errBad
		}
		n, ok := strtonum(arg[1:], 0, 1<<32-1)
		if !ok {
			return errBad
		}
		set(t, uint32(n), "")
	case "window":
		if !hasArg {
			return errBad
		}
		n, ok := strtonum(arg, 0, 1<<32-1)
		if !ok {
			return errBad
		}
		set(RangeWindow, uint32(n), "")
	case "user":
		if !hasArg {
			return errBad
		}
		if len(arg) > rangeStringSize-1 {
			arg = arg[:rangeStringSize-1]
		}
		set(RangeUser, 0, arg)
	}
	return nil
}

// String is style_tostring.
func (sy *Style) String() string {
	var parts []string
	add := func(s string) { parts = append(parts, s) }
	switch sy.List {
	case ListOn:
		add("list=on")
	case ListFocus:
		add("list=focus")
	case ListLeftMarker:
		add("list=left-marker")
	case ListRightMarker:
		add("list=right-marker")
	}
	switch sy.RangeType {
	case RangeNone:
	case RangeLeft:
		add("range=left")
	case RangeRight:
		add("range=right")
	case RangePane:
		add(fmt.Sprintf("range=pane|%%%d", sy.RangeArg))
	case RangeWindow:
		add(fmt.Sprintf("range=window|%d", sy.RangeArg))
	case RangeSession:
		add(fmt.Sprintf("range=session|$%d", sy.RangeArg))
	case RangeUser:
		add("range=user|" + sy.RangeString)
	case RangeControl:
		// tmux prints its tmp, left from before: nothing new.
		add("range=")
	}
	switch sy.Align {
	case AlignLeft:
		add("align=left")
	case AlignCentre:
		add("align=centre")
	case AlignRight:
		add("align=right")
	case AlignAbsoluteCentre:
		add("align=absolute-centre")
	}
	switch sy.Default {
	case DefaultPush:
		add("push-default")
	case DefaultPop:
		add("pop-default")
	case DefaultSet:
		add("set-default")
	}
	if sy.Fill != 8 {
		add("fill=" + vt.ColorToString(sy.Fill))
	}
	if sy.Dim != 0 {
		add(fmt.Sprintf("dim=%d%%", sy.Dim))
	}
	if sy.Cell.Fg != 8 {
		add("fg=" + vt.ColorToString(sy.Cell.Fg))
	}
	if sy.Cell.Bg != 8 {
		add("bg=" + vt.ColorToString(sy.Cell.Bg))
	}
	if sy.Cell.Us != 8 {
		add("us=" + vt.ColorToString(sy.Cell.Us))
	}
	if sy.Cell.Attr != 0 {
		add(AttributesToString(sy.Cell.Attr))
	}
	if sy.Width >= 0 {
		if sy.WidthPct {
			add(fmt.Sprintf("width=%d%%", sy.Width))
		} else {
			add(fmt.Sprintf("width=%d", sy.Width))
		}
	}
	if sy.Pad >= 0 {
		add(fmt.Sprintf("pad=%d", sy.Pad))
	}
	if sy.Link != "" {
		add("link=" + sy.Link)
	}
	if len(parts) == 0 {
		return "default"
	}
	return strings.Join(parts, ",")
}

// Add is style_add's last half: the style's colours (those not default) and attributes
// laid over gc.
func (sy *Style) Add(gc *vt.Cell) {
	if sy.Cell.Fg != 8 {
		gc.Fg = sy.Cell.Fg
	}
	if sy.Cell.Bg != 8 {
		gc.Bg = sy.Cell.Bg
	}
	if sy.Cell.Us != 8 {
		gc.Us = sy.Cell.Us
	}
	gc.Attr |= sy.Cell.Attr
}

// Valid reports whether s parses as a style (options_validate's check).
func Valid(s string) bool {
	sy := Default()
	return sy.Parse(vt.DefaultCell, s) == nil
}
