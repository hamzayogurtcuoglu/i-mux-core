// Copyright (c) 2008 Nicholas Marriott <nicholas.marriott@gmail.com>
// Copyright (c) 2016 Avi Halachmi <avihpit@yahoo.com>
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

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Colour palette and X11/256-colour conversions, ported from tmux 3.8's colour.c.

// ColorSplitRGB unpacks an RGB colour into its 8-bit components (tmux's colour_split_rgb).
func ColorSplitRGB(c Color) (r, g, b uint8) {
	return uint8((c >> 16) & 0xff), uint8((c >> 8) & 0xff), uint8(c & 0xff)
}

// ColorForceRGB converts a 16-colour, 256-colour or RGB Color into an RGB Color
// (tmux's colour_force_rgb), returning -1 for default or unset colours.
func ColorForceRGB(c Color) Color {
	if c&ColorFlagRGB != 0 {
		return c
	}
	if c&ColorFlag256 != 0 {
		return Color256ToRGB(c)
	}
	if c >= 0 && c <= 7 {
		return Color256ToRGB(c)
	}
	if c >= 90 && c <= 97 {
		return Color256ToRGB(8 + c - 90)
	}
	return -1
}

// Color256ToRGB converts a 256-colour index to its xterm RGB value (tmux's colour_256toRGB).
func Color256ToRGB(c Color) Color {
	return color256Table[c&0xff] | ColorFlagRGB
}

var color256Table = [256]Color{
	0x000000, 0x800000, 0x008000, 0x808000,
	0x000080, 0x800080, 0x008080, 0xc0c0c0,
	0x808080, 0xff0000, 0x00ff00, 0xffff00,
	0x0000ff, 0xff00ff, 0x00ffff, 0xffffff,
	0x000000, 0x00005f, 0x000087, 0x0000af,
	0x0000d7, 0x0000ff, 0x005f00, 0x005f5f,
	0x005f87, 0x005faf, 0x005fd7, 0x005fff,
	0x008700, 0x00875f, 0x008787, 0x0087af,
	0x0087d7, 0x0087ff, 0x00af00, 0x00af5f,
	0x00af87, 0x00afaf, 0x00afd7, 0x00afff,
	0x00d700, 0x00d75f, 0x00d787, 0x00d7af,
	0x00d7d7, 0x00d7ff, 0x00ff00, 0x00ff5f,
	0x00ff87, 0x00ffaf, 0x00ffd7, 0x00ffff,
	0x5f0000, 0x5f005f, 0x5f0087, 0x5f00af,
	0x5f00d7, 0x5f00ff, 0x5f5f00, 0x5f5f5f,
	0x5f5f87, 0x5f5faf, 0x5f5fd7, 0x5f5fff,
	0x5f8700, 0x5f875f, 0x5f8787, 0x5f87af,
	0x5f87d7, 0x5f87ff, 0x5faf00, 0x5faf5f,
	0x5faf87, 0x5fafaf, 0x5fafd7, 0x5fafff,
	0x5fd700, 0x5fd75f, 0x5fd787, 0x5fd7af,
	0x5fd7d7, 0x5fd7ff, 0x5fff00, 0x5fff5f,
	0x5fff87, 0x5fffaf, 0x5fffd7, 0x5fffff,
	0x870000, 0x87005f, 0x870087, 0x8700af,
	0x8700d7, 0x8700ff, 0x875f00, 0x875f5f,
	0x875f87, 0x875faf, 0x875fd7, 0x875fff,
	0x878700, 0x87875f, 0x878787, 0x8787af,
	0x8787d7, 0x8787ff, 0x87af00, 0x87af5f,
	0x87af87, 0x87afaf, 0x87afd7, 0x87afff,
	0x87d700, 0x87d75f, 0x87d787, 0x87d7af,
	0x87d7d7, 0x87d7ff, 0x87ff00, 0x87ff5f,
	0x87ff87, 0x87ffaf, 0x87ffd7, 0x87ffff,
	0xaf0000, 0xaf005f, 0xaf0087, 0xaf00af,
	0xaf00d7, 0xaf00ff, 0xaf5f00, 0xaf5f5f,
	0xaf5f87, 0xaf5faf, 0xaf5fd7, 0xaf5fff,
	0xaf8700, 0xaf875f, 0xaf8787, 0xaf87af,
	0xaf87d7, 0xaf87ff, 0xafaf00, 0xafaf5f,
	0xafaf87, 0xafafaf, 0xafafd7, 0xafafff,
	0xafd700, 0xafd75f, 0xafd787, 0xafd7af,
	0xafd7d7, 0xafd7ff, 0xafff00, 0xafff5f,
	0xafff87, 0xafffaf, 0xafffd7, 0xafffff,
	0xd70000, 0xd7005f, 0xd70087, 0xd700af,
	0xd700d7, 0xd700ff, 0xd75f00, 0xd75f5f,
	0xd75f87, 0xd75faf, 0xd75fd7, 0xd75fff,
	0xd78700, 0xd7875f, 0xd78787, 0xd787af,
	0xd787d7, 0xd787ff, 0xd7af00, 0xd7af5f,
	0xd7af87, 0xd7afaf, 0xd7afd7, 0xd7afff,
	0xd7d700, 0xd7d75f, 0xd7d787, 0xd7d7af,
	0xd7d7d7, 0xd7d7ff, 0xd7ff00, 0xd7ff5f,
	0xd7ff87, 0xd7ffaf, 0xd7ffd7, 0xd7ffff,
	0xff0000, 0xff005f, 0xff0087, 0xff00af,
	0xff00d7, 0xff00ff, 0xff5f00, 0xff5f5f,
	0xff5f87, 0xff5faf, 0xff5fd7, 0xff5fff,
	0xff8700, 0xff875f, 0xff8787, 0xff87af,
	0xff87d7, 0xff87ff, 0xffaf00, 0xffaf5f,
	0xffaf87, 0xffafaf, 0xffafd7, 0xffafff,
	0xffd700, 0xffd75f, 0xffd787, 0xffd7af,
	0xffd7d7, 0xffd7ff, 0xffff00, 0xffff5f,
	0xffff87, 0xffffaf, 0xffffd7, 0xffffff,
	0x080808, 0x121212, 0x1c1c1c, 0x262626,
	0x303030, 0x3a3a3a, 0x444444, 0x4e4e4e,
	0x585858, 0x626262, 0x6c6c6c, 0x767676,
	0x808080, 0x8a8a8a, 0x949494, 0x9e9e9e,
	0xa8a8a8, 0xb2b2b2, 0xbcbcbc, 0xc6c6c6,
	0xd0d0d0, 0xdadada, 0xe4e4e4, 0xeeeeee,
}

// Palette holds a pane's dynamic colours (OSC 4/10/11) and any colours known
// from an attached client terminal (tmux's colour_palette + tty fg/bg).
type Palette struct {
	Fg             Color
	Bg             Color
	ClientFg       Color
	ClientBg       Color
	ClientPalette  [256]Color
	palette        *[256]Color
	defaultPalette *[256]Color
}

// Init initializes p to default colours (tmux's colour_palette_init).
func (p *Palette) Init() {
	p.Fg = ColorDefault
	p.Bg = ColorDefault
	p.ClientFg = -1
	p.ClientBg = -1
	for i := range p.ClientPalette {
		p.ClientPalette[i] = -1
	}
	p.palette = nil
	p.defaultPalette = nil
}

// Clear resets fg, bg and all dynamic palette entries (tmux's colour_palette_clear).
func (p *Palette) Clear() {
	if p == nil {
		return
	}
	p.Fg = ColorDefault
	p.Bg = ColorDefault
	p.palette = nil
}

// Get returns palette entry n, or -1 if unset (tmux's colour_palette_get).
func (p *Palette) Get(n Color) Color {
	if p == nil {
		return -1
	}
	idx := int(n)
	switch {
	case idx >= 90 && idx <= 97:
		idx = 8 + idx - 90
	case n&ColorFlag256 != 0:
		idx = int(n &^ ColorFlag256)
	case idx >= 8 || idx < 0:
		return -1
	}
	if idx < 0 || idx >= 256 {
		return -1
	}
	if p.palette != nil && p.palette[idx] != -1 {
		return p.palette[idx]
	}
	if p.defaultPalette != nil && p.defaultPalette[idx] != -1 {
		return p.defaultPalette[idx]
	}
	return -1
}

// Set sets or clears (c == -1) palette entry n (tmux's colour_palette_set).
func (p *Palette) Set(n int, c Color) bool {
	if p == nil || n < 0 || n > 255 {
		return false
	}
	if c == -1 && p.palette == nil {
		return false
	}
	if p.palette == nil {
		var arr [256]Color
		for i := range arr {
			arr[i] = -1
		}
		p.palette = &arr
	}
	p.palette[n] = c
	return true
}

// SetDefault sets a default palette entry (from pane-colours).
func (p *Palette) SetDefault(n int, c Color) {
	if p == nil || n < 0 || n > 255 {
		return
	}
	if p.defaultPalette == nil {
		var arr [256]Color
		for i := range arr {
			arr[i] = -1
		}
		p.defaultPalette = &arr
	}
	p.defaultPalette[n] = c
}

// ParseX11Color parses an X11 colour string as tmux's colour_parseX11 does,
// returning -1 if unrecognized.
func ParseX11Color(p string) Color {
	var r, g, b uint
	var c, m, y, k float64
	switch {
	case len(p) == 12 && strings.HasPrefix(p, "rgb:") && p[6] == '/' && p[9] == '/':
		if r1, ok1 := parseHexUint(p[4:6]); ok1 {
			if g1, ok2 := parseHexUint(p[7:9]); ok2 {
				if b1, ok3 := parseHexUint(p[10:12]); ok3 {
					return ColorJoinRGB(r1, g1, b1)
				}
			}
		}
	case len(p) == 7 && p[0] == '#':
		if r1, ok1 := parseHexUint(p[1:3]); ok1 {
			if g1, ok2 := parseHexUint(p[3:5]); ok2 {
				if b1, ok3 := parseHexUint(p[5:7]); ok3 {
					return ColorJoinRGB(r1, g1, b1)
				}
			}
		}
	case len(p) == 18 && strings.HasPrefix(p, "rgb:") && p[8] == '/' && p[13] == '/':
		if r1, ok1 := parseHexUint(p[4:8]); ok1 {
			if g1, ok2 := parseHexUint(p[9:13]); ok2 {
				if b1, ok3 := parseHexUint(p[14:18]); ok3 {
					return ColorJoinRGB(r1>>8, g1>>8, b1>>8)
				}
			}
		}
	case len(p) == 13 && p[0] == '#':
		if r1, ok1 := parseHexUint(p[1:5]); ok1 {
			if g1, ok2 := parseHexUint(p[5:9]); ok2 {
				if b1, ok3 := parseHexUint(p[9:13]); ok3 {
					return ColorJoinRGB(r1>>8, g1>>8, b1>>8)
				}
			}
		}
	}
	if n, err := fmt.Sscanf(p, "%d,%d,%d", &r, &g, &b); err == nil && n == 3 {
		return ColorJoinRGB(int(r), int(g), int(b))
	}
	if n, err := fmt.Sscanf(p, "cmyk:%f/%f/%f/%f", &c, &m, &y, &k); err == nil && n == 4 {
		if c >= 0 && c <= 1 && m >= 0 && m <= 1 && y >= 0 && y <= 1 && k >= 0 && k <= 1 {
			return ColorJoinRGB(
				int((1-c)*(1-k)*255),
				int((1-m)*(1-k)*255),
				int((1-y)*(1-k)*255),
			)
		}
	}
	if n, err := fmt.Sscanf(p, "cmy:%f/%f/%f", &c, &m, &y); err == nil && n == 3 {
		if c >= 0 && c <= 1 && m >= 0 && m <= 1 && y >= 0 && y <= 1 {
			return ColorJoinRGB(
				int((1-c)*255),
				int((1-m)*255),
				int((1-y)*255),
			)
		}
	}
	return colorByName(strings.Trim(p, " "))
}

func parseHexUint(s string) (int, bool) {
	v, err := strconv.ParseUint(s, 16, 32)
	if err != nil {
		return 0, false
	}
	return int(v), true
}

func colorByName(name string) Color {
	if len(name) >= 4 && (strings.EqualFold(name[:4], "grey") || strings.EqualFold(name[:4], "gray")) {
		if len(name) == 4 {
			return 0xbebebe | ColorFlagRGB
		}
		v, err := strconv.Atoi(name[4:])
		if err != nil || v < 0 || v > 100 {
			return -1
		}
		c := int(math.Round(2.55 * float64(v)))
		if c < 0 || c > 255 {
			return -1
		}
		return ColorJoinRGB(c, c, c)
	}
	if c, ok := x11ColorNames[strings.ToLower(name)]; ok {
		return c | ColorFlagRGB
	}
	return -1
}

var x11ColorNames = map[string]Color{
	"aliceblue":              0xf0f8ff,
	"alice blue":             0xf0f8ff,
	"antiquewhite":           0xfaebd7,
	"antique white":          0xfaebd7,
	"antiquewhite1":          0xffefdb,
	"antiquewhite2":          0xeedfcc,
	"antiquewhite3":          0xcdc0b0,
	"antiquewhite4":          0x8b8378,
	"aqua":                   0x00ffff,
	"aquamarine":             0x7fffd4,
	"aquamarine1":            0x7fffd4,
	"aquamarine2":            0x76eec6,
	"aquamarine3":            0x66cdaa,
	"aquamarine4":            0x458b74,
	"azure":                  0xf0ffff,
	"azure1":                 0xf0ffff,
	"azure2":                 0xe0eeee,
	"azure3":                 0xc1cdcd,
	"azure4":                 0x838b8b,
	"beige":                  0xf5f5dc,
	"bisque":                 0xffe4c4,
	"bisque1":                0xffe4c4,
	"bisque2":                0xeed5b7,
	"bisque3":                0xcdb79e,
	"bisque4":                0x8b7d6b,
	"black":                  0x000000,
	"blanchedalmond":         0xffebcd,
	"blanched almond":        0xffebcd,
	"blue":                   0x0000ff,
	"blue1":                  0x0000ff,
	"blue2":                  0x0000ee,
	"blue3":                  0x0000cd,
	"blue4":                  0x00008b,
	"blueviolet":             0x8a2be2,
	"blue violet":            0x8a2be2,
	"brown":                  0xa52a2a,
	"brown1":                 0xff4040,
	"brown2":                 0xee3b3b,
	"brown3":                 0xcd3333,
	"brown4":                 0x8b2323,
	"burlywood":              0xdeb887,
	"burlywood1":             0xffd39b,
	"burlywood2":             0xeec591,
	"burlywood3":             0xcdaa7d,
	"burlywood4":             0x8b7355,
	"cadetblue":              0x5f9ea0,
	"cadet blue":             0x5f9ea0,
	"cadetblue1":             0x98f5ff,
	"cadetblue2":             0x8ee5ee,
	"cadetblue3":             0x7ac5cd,
	"cadetblue4":             0x53868b,
	"chartreuse":             0x7fff00,
	"chartreuse1":            0x7fff00,
	"chartreuse2":            0x76ee00,
	"chartreuse3":            0x66cd00,
	"chartreuse4":            0x458b00,
	"chocolate":              0xd2691e,
	"chocolate1":             0xff7f24,
	"chocolate2":             0xee7621,
	"chocolate3":             0xcd661d,
	"chocolate4":             0x8b4513,
	"coral":                  0xff7f50,
	"coral1":                 0xff7256,
	"coral2":                 0xee6a50,
	"coral3":                 0xcd5b45,
	"coral4":                 0x8b3e2f,
	"cornflowerblue":         0x6495ed,
	"cornflower blue":        0x6495ed,
	"cornsilk":               0xfff8dc,
	"cornsilk1":              0xfff8dc,
	"cornsilk2":              0xeee8cd,
	"cornsilk3":              0xcdc8b1,
	"cornsilk4":              0x8b8878,
	"crimson":                0xdc143c,
	"cyan":                   0x00ffff,
	"cyan1":                  0x00ffff,
	"cyan2":                  0x00eeee,
	"cyan3":                  0x00cdcd,
	"cyan4":                  0x008b8b,
	"darkblue":               0x00008b,
	"dark blue":              0x00008b,
	"darkcyan":               0x008b8b,
	"dark cyan":              0x008b8b,
	"darkgoldenrod":          0xb8860b,
	"dark goldenrod":         0xb8860b,
	"darkgoldenrod1":         0xffb90f,
	"darkgoldenrod2":         0xeead0e,
	"darkgoldenrod3":         0xcd950c,
	"darkgoldenrod4":         0x8b6508,
	"darkgray":               0xa9a9a9,
	"dark gray":              0xa9a9a9,
	"darkgreen":              0x006400,
	"dark green":             0x006400,
	"darkgrey":               0xa9a9a9,
	"dark grey":              0xa9a9a9,
	"darkkhaki":              0xbdb76b,
	"dark khaki":             0xbdb76b,
	"darkmagenta":            0x8b008b,
	"dark magenta":           0x8b008b,
	"darkolivegreen":         0x556b2f,
	"dark olive green":       0x556b2f,
	"darkolivegreen1":        0xcaff70,
	"darkolivegreen2":        0xbcee68,
	"darkolivegreen3":        0xa2cd5a,
	"darkolivegreen4":        0x6e8b3d,
	"darkorange":             0xff8c00,
	"dark orange":            0xff8c00,
	"darkorange1":            0xff7f00,
	"darkorange2":            0xee7600,
	"darkorange3":            0xcd6600,
	"darkorange4":            0x8b4500,
	"darkorchid":             0x9932cc,
	"dark orchid":            0x9932cc,
	"darkorchid1":            0xbf3eff,
	"darkorchid2":            0xb23aee,
	"darkorchid3":            0x9a32cd,
	"darkorchid4":            0x68228b,
	"darkred":                0x8b0000,
	"dark red":               0x8b0000,
	"darksalmon":             0xe9967a,
	"dark salmon":            0xe9967a,
	"darkseagreen":           0x8fbc8f,
	"dark sea green":         0x8fbc8f,
	"darkseagreen1":          0xc1ffc1,
	"darkseagreen2":          0xb4eeb4,
	"darkseagreen3":          0x9bcd9b,
	"darkseagreen4":          0x698b69,
	"darkslateblue":          0x483d8b,
	"dark slate blue":        0x483d8b,
	"darkslategray":          0x2f4f4f,
	"dark slate gray":        0x2f4f4f,
	"darkslategray1":         0x97ffff,
	"darkslategray2":         0x8deeee,
	"darkslategray3":         0x79cdcd,
	"darkslategray4":         0x528b8b,
	"darkslategrey":          0x2f4f4f,
	"dark slate grey":        0x2f4f4f,
	"darkturquoise":          0x00ced1,
	"dark turquoise":         0x00ced1,
	"darkviolet":             0x9400d3,
	"dark violet":            0x9400d3,
	"deeppink":               0xff1493,
	"deep pink":              0xff1493,
	"deeppink1":              0xff1493,
	"deeppink2":              0xee1289,
	"deeppink3":              0xcd1076,
	"deeppink4":              0x8b0a50,
	"deepskyblue":            0x00bfff,
	"deep sky blue":          0x00bfff,
	"deepskyblue1":           0x00bfff,
	"deepskyblue2":           0x00b2ee,
	"deepskyblue3":           0x009acd,
	"deepskyblue4":           0x00688b,
	"dimgray":                0x696969,
	"dim gray":               0x696969,
	"dimgrey":                0x696969,
	"dim grey":               0x696969,
	"dodgerblue":             0x1e90ff,
	"dodger blue":            0x1e90ff,
	"dodgerblue1":            0x1e90ff,
	"dodgerblue2":            0x1c86ee,
	"dodgerblue3":            0x1874cd,
	"dodgerblue4":            0x104e8b,
	"firebrick":              0xb22222,
	"firebrick1":             0xff3030,
	"firebrick2":             0xee2c2c,
	"firebrick3":             0xcd2626,
	"firebrick4":             0x8b1a1a,
	"floralwhite":            0xfffaf0,
	"floral white":           0xfffaf0,
	"forestgreen":            0x228b22,
	"forest green":           0x228b22,
	"fuchsia":                0xff00ff,
	"gainsboro":              0xdcdcdc,
	"ghostwhite":             0xf8f8ff,
	"ghost white":            0xf8f8ff,
	"gold":                   0xffd700,
	"gold1":                  0xffd700,
	"gold2":                  0xeec900,
	"gold3":                  0xcdad00,
	"gold4":                  0x8b7500,
	"goldenrod":              0xdaa520,
	"goldenrod1":             0xffc125,
	"goldenrod2":             0xeeb422,
	"goldenrod3":             0xcd9b1d,
	"goldenrod4":             0x8b6914,
	"green":                  0x00ff00,
	"green1":                 0x00ff00,
	"green2":                 0x00ee00,
	"green3":                 0x00cd00,
	"green4":                 0x008b00,
	"greenyellow":            0xadff2f,
	"green yellow":           0xadff2f,
	"honeydew":               0xf0fff0,
	"honeydew1":              0xf0fff0,
	"honeydew2":              0xe0eee0,
	"honeydew3":              0xc1cdc1,
	"honeydew4":              0x838b83,
	"hotpink":                0xff69b4,
	"hot pink":               0xff69b4,
	"hotpink1":               0xff6eb4,
	"hotpink2":               0xee6aa7,
	"hotpink3":               0xcd6090,
	"hotpink4":               0x8b3a62,
	"indianred":              0xcd5c5c,
	"indian red":             0xcd5c5c,
	"indianred1":             0xff6a6a,
	"indianred2":             0xee6363,
	"indianred3":             0xcd5555,
	"indianred4":             0x8b3a3a,
	"indigo":                 0x4b0082,
	"ivory":                  0xfffff0,
	"ivory1":                 0xfffff0,
	"ivory2":                 0xeeeee0,
	"ivory3":                 0xcdcdc1,
	"ivory4":                 0x8b8b83,
	"khaki":                  0xf0e68c,
	"khaki1":                 0xfff68f,
	"khaki2":                 0xeee685,
	"khaki3":                 0xcdc673,
	"khaki4":                 0x8b864e,
	"lavender":               0xe6e6fa,
	"lavenderblush":          0xfff0f5,
	"lavender blush":         0xfff0f5,
	"lavenderblush1":         0xfff0f5,
	"lavenderblush2":         0xeee0e5,
	"lavenderblush3":         0xcdc1c5,
	"lavenderblush4":         0x8b8386,
	"lawngreen":              0x7cfc00,
	"lawn green":             0x7cfc00,
	"lemonchiffon":           0xfffacd,
	"lemon chiffon":          0xfffacd,
	"lemonchiffon1":          0xfffacd,
	"lemonchiffon2":          0xeee9bf,
	"lemonchiffon3":          0xcdc9a5,
	"lemonchiffon4":          0x8b8970,
	"lightblue":              0xadd8e6,
	"light blue":             0xadd8e6,
	"lightblue1":             0xbfefff,
	"lightblue2":             0xb2dfee,
	"lightblue3":             0x9ac0cd,
	"lightblue4":             0x68838b,
	"lightcoral":             0xf08080,
	"light coral":            0xf08080,
	"lightcyan":              0xe0ffff,
	"light cyan":             0xe0ffff,
	"lightcyan1":             0xe0ffff,
	"lightcyan2":             0xd1eeee,
	"lightcyan3":             0xb4cdcd,
	"lightcyan4":             0x7a8b8b,
	"lightgoldenrod":         0xeedd82,
	"light goldenrod":        0xeedd82,
	"lightgoldenrod1":        0xffec8b,
	"lightgoldenrod2":        0xeedc82,
	"lightgoldenrod3":        0xcdbe70,
	"lightgoldenrod4":        0x8b814c,
	"lightgoldenrodyellow":   0xfafad2,
	"light goldenrod yellow": 0xfafad2,
	"lightgray":              0xd3d3d3,
	"light gray":             0xd3d3d3,
	"lightgreen":             0x90ee90,
	"light green":            0x90ee90,
	"lightgrey":              0xd3d3d3,
	"light grey":             0xd3d3d3,
	"lightpink":              0xffb6c1,
	"light pink":             0xffb6c1,
	"lightpink1":             0xffaeb9,
	"lightpink2":             0xeea2ad,
	"lightpink3":             0xcd8c95,
	"lightpink4":             0x8b5f65,
	"lightsalmon":            0xffa07a,
	"light salmon":           0xffa07a,
	"lightsalmon1":           0xffa07a,
	"lightsalmon2":           0xee9572,
	"lightsalmon3":           0xcd8162,
	"lightsalmon4":           0x8b5742,
	"lightseagreen":          0x20b2aa,
	"light sea green":        0x20b2aa,
	"lightskyblue":           0x87cefa,
	"light sky blue":         0x87cefa,
	"lightskyblue1":          0xb0e2ff,
	"lightskyblue2":          0xa4d3ee,
	"lightskyblue3":          0x8db6cd,
	"lightskyblue4":          0x607b8b,
	"lightslateblue":         0x8470ff,
	"light slate blue":       0x8470ff,
	"lightslategray":         0x778899,
	"light slate gray":       0x778899,
	"lightslategrey":         0x778899,
	"light slate grey":       0x778899,
	"lightsteelblue":         0xb0c4de,
	"light steel blue":       0xb0c4de,
	"lightsteelblue1":        0xcae1ff,
	"lightsteelblue2":        0xbcd2ee,
	"lightsteelblue3":        0xa2b5cd,
	"lightsteelblue4":        0x6e7b8b,
	"lightyellow":            0xffffe0,
	"light yellow":           0xffffe0,
	"lightyellow1":           0xffffe0,
	"lightyellow2":           0xeeeed1,
	"lightyellow3":           0xcdcdb4,
	"lightyellow4":           0x8b8b7a,
	"lime":                   0x00ff00,
	"limegreen":              0x32cd32,
	"lime green":             0x32cd32,
	"linen":                  0xfaf0e6,
	"magenta":                0xff00ff,
	"magenta1":               0xff00ff,
	"magenta2":               0xee00ee,
	"magenta3":               0xcd00cd,
	"magenta4":               0x8b008b,
	"maroon":                 0xb03060,
	"maroon1":                0xff34b3,
	"maroon2":                0xee30a7,
	"maroon3":                0xcd2990,
	"maroon4":                0x8b1c62,
	"mediumaquamarine":       0x66cdaa,
	"medium aquamarine":      0x66cdaa,
	"mediumblue":             0x0000cd,
	"medium blue":            0x0000cd,
	"mediumorchid":           0xba55d3,
	"medium orchid":          0xba55d3,
	"mediumorchid1":          0xe066ff,
	"mediumorchid2":          0xd15fee,
	"mediumorchid3":          0xb452cd,
	"mediumorchid4":          0x7a378b,
	"mediumpurple":           0x9370db,
	"medium purple":          0x9370db,
	"mediumpurple1":          0xab82ff,
	"mediumpurple2":          0x9f79ee,
	"mediumpurple3":          0x8968cd,
	"mediumpurple4":          0x5d478b,
	"mediumseagreen":         0x3cb371,
	"medium sea green":       0x3cb371,
	"mediumslateblue":        0x7b68ee,
	"medium slate blue":      0x7b68ee,
	"mediumspringgreen":      0x00fa9a,
	"medium spring green":    0x00fa9a,
	"mediumturquoise":        0x48d1cc,
	"medium turquoise":       0x48d1cc,
	"mediumvioletred":        0xc71585,
	"medium violet red":      0xc71585,
	"midnightblue":           0x191970,
	"midnight blue":          0x191970,
	"mintcream":              0xf5fffa,
	"mint cream":             0xf5fffa,
	"mistyrose":              0xffe4e1,
	"misty rose":             0xffe4e1,
	"mistyrose1":             0xffe4e1,
	"mistyrose2":             0xeed5d2,
	"mistyrose3":             0xcdb7b5,
	"mistyrose4":             0x8b7d7b,
	"moccasin":               0xffe4b5,
	"navajowhite":            0xffdead,
	"navajo white":           0xffdead,
	"navajowhite1":           0xffdead,
	"navajowhite2":           0xeecfa1,
	"navajowhite3":           0xcdb38b,
	"navajowhite4":           0x8b795e,
	"navy":                   0x000080,
	"navyblue":               0x000080,
	"navy blue":              0x000080,
	"oldlace":                0xfdf5e6,
	"old lace":               0xfdf5e6,
	"olive":                  0x808000,
	"olivedrab":              0x6b8e23,
	"olive drab":             0x6b8e23,
	"olivedrab1":             0xc0ff3e,
	"olivedrab2":             0xb3ee3a,
	"olivedrab3":             0x9acd32,
	"olivedrab4":             0x698b22,
	"orange":                 0xffa500,
	"orange1":                0xffa500,
	"orange2":                0xee9a00,
	"orange3":                0xcd8500,
	"orange4":                0x8b5a00,
	"orangered":              0xff4500,
	"orange red":             0xff4500,
	"orangered1":             0xff4500,
	"orangered2":             0xee4000,
	"orangered3":             0xcd3700,
	"orangered4":             0x8b2500,
	"orchid":                 0xda70d6,
	"orchid1":                0xff83fa,
	"orchid2":                0xee7ae9,
	"orchid3":                0xcd69c9,
	"orchid4":                0x8b4789,
	"palegoldenrod":          0xeee8aa,
	"pale goldenrod":         0xeee8aa,
	"palegreen":              0x98fb98,
	"pale green":             0x98fb98,
	"palegreen1":             0x9aff9a,
	"palegreen2":             0x90ee90,
	"palegreen3":             0x7ccd7c,
	"palegreen4":             0x548b54,
	"paleturquoise":          0xafeeee,
	"pale turquoise":         0xafeeee,
	"paleturquoise1":         0xbbffff,
	"paleturquoise2":         0xaeeeee,
	"paleturquoise3":         0x96cdcd,
	"paleturquoise4":         0x668b8b,
	"palevioletred":          0xdb7093,
	"pale violet red":        0xdb7093,
	"palevioletred1":         0xff82ab,
	"palevioletred2":         0xee799f,
	"palevioletred3":         0xcd6889,
	"palevioletred4":         0x8b475d,
	"papayawhip":             0xffefd5,
	"papaya whip":            0xffefd5,
	"peachpuff":              0xffdab9,
	"peach puff":             0xffdab9,
	"peachpuff1":             0xffdab9,
	"peachpuff2":             0xeecbad,
	"peachpuff3":             0xcdaf95,
	"peachpuff4":             0x8b7765,
	"peru":                   0xcd853f,
	"pink":                   0xffc0cb,
	"pink1":                  0xffb5c5,
	"pink2":                  0xeea9b8,
	"pink3":                  0xcd919e,
	"pink4":                  0x8b636c,
	"plum":                   0xdda0dd,
	"plum1":                  0xffbbff,
	"plum2":                  0xeeaeee,
	"plum3":                  0xcd96cd,
	"plum4":                  0x8b668b,
	"powderblue":             0xb0e0e6,
	"powder blue":            0xb0e0e6,
	"purple":                 0xa020f0,
	"purple1":                0x9b30ff,
	"purple2":                0x912cee,
	"purple3":                0x7d26cd,
	"purple4":                0x551a8b,
	"rebeccapurple":          0x663399,
	"rebecca purple":         0x663399,
	"red":                    0xff0000,
	"red1":                   0xff0000,
	"red2":                   0xee0000,
	"red3":                   0xcd0000,
	"red4":                   0x8b0000,
	"rosybrown":              0xbc8f8f,
	"rosy brown":             0xbc8f8f,
	"rosybrown1":             0xffc1c1,
	"rosybrown2":             0xeeb4b4,
	"rosybrown3":             0xcd9b9b,
	"rosybrown4":             0x8b6969,
	"royalblue":              0x4169e1,
	"royal blue":             0x4169e1,
	"royalblue1":             0x4876ff,
	"royalblue2":             0x436eee,
	"royalblue3":             0x3a5fcd,
	"royalblue4":             0x27408b,
	"saddlebrown":            0x8b4513,
	"saddle brown":           0x8b4513,
	"salmon":                 0xfa8072,
	"salmon1":                0xff8c69,
	"salmon2":                0xee8262,
	"salmon3":                0xcd7054,
	"salmon4":                0x8b4c39,
	"sandybrown":             0xf4a460,
	"sandy brown":            0xf4a460,
	"seagreen":               0x2e8b57,
	"sea green":              0x2e8b57,
	"seagreen1":              0x54ff9f,
	"seagreen2":              0x4eee94,
	"seagreen3":              0x43cd80,
	"seagreen4":              0x2e8b57,
	"seashell":               0xfff5ee,
	"seashell1":              0xfff5ee,
	"seashell2":              0xeee5de,
	"seashell3":              0xcdc5bf,
	"seashell4":              0x8b8682,
	"sienna":                 0xa0522d,
	"sienna1":                0xff8247,
	"sienna2":                0xee7942,
	"sienna3":                0xcd6839,
	"sienna4":                0x8b4726,
	"silver":                 0xc0c0c0,
	"skyblue":                0x87ceeb,
	"sky blue":               0x87ceeb,
	"skyblue1":               0x87ceff,
	"skyblue2":               0x7ec0ee,
	"skyblue3":               0x6ca6cd,
	"skyblue4":               0x4a708b,
	"slateblue":              0x6a5acd,
	"slate blue":             0x6a5acd,
	"slateblue1":             0x836fff,
	"slateblue2":             0x7a67ee,
	"slateblue3":             0x6959cd,
	"slateblue4":             0x473c8b,
	"slategray":              0x708090,
	"slate gray":             0x708090,
	"slategray1":             0xc6e2ff,
	"slategray2":             0xb9d3ee,
	"slategray3":             0x9fb6cd,
	"slategray4":             0x6c7b8b,
	"slategrey":              0x708090,
	"slate grey":             0x708090,
	"snow":                   0xfffafa,
	"snow1":                  0xfffafa,
	"snow2":                  0xeee9e9,
	"snow3":                  0xcdc9c9,
	"snow4":                  0x8b8989,
	"springgreen":            0x00ff7f,
	"spring green":           0x00ff7f,
	"springgreen1":           0x00ff7f,
	"springgreen2":           0x00ee76,
	"springgreen3":           0x00cd66,
	"springgreen4":           0x008b45,
	"steelblue":              0x4682b4,
	"steel blue":             0x4682b4,
	"steelblue1":             0x63b8ff,
	"steelblue2":             0x5cacee,
	"steelblue3":             0x4f94cd,
	"steelblue4":             0x36648b,
	"tan":                    0xd2b48c,
	"tan1":                   0xffa54f,
	"tan2":                   0xee9a49,
	"tan3":                   0xcd853f,
	"tan4":                   0x8b5a2b,
	"teal":                   0x008080,
	"thistle":                0xd8bfd8,
	"thistle1":               0xffe1ff,
	"thistle2":               0xeed2ee,
	"thistle3":               0xcdb5cd,
	"thistle4":               0x8b7b8b,
	"tomato":                 0xff6347,
	"tomato1":                0xff6347,
	"tomato2":                0xee5c42,
	"tomato3":                0xcd4f39,
	"tomato4":                0x8b3626,
	"turquoise":              0x40e0d0,
	"turquoise1":             0x00f5ff,
	"turquoise2":             0x00e5ee,
	"turquoise3":             0x00c5cd,
	"turquoise4":             0x00868b,
	"violet":                 0xee82ee,
	"violetred":              0xd02090,
	"violet red":             0xd02090,
	"violetred1":             0xff3e96,
	"violetred2":             0xee3a8c,
	"violetred3":             0xcd3278,
	"violetred4":             0x8b2252,
	"webgray":                0x808080,
	"web gray":               0x808080,
	"webgreen":               0x008000,
	"web green":              0x008000,
	"webgrey":                0x808080,
	"web grey":               0x808080,
	"webmaroon":              0x800000,
	"web maroon":             0x800000,
	"webpurple":              0x800080,
	"web purple":             0x800080,
	"wheat":                  0xf5deb3,
	"wheat1":                 0xffe7ba,
	"wheat2":                 0xeed8ae,
	"wheat3":                 0xcdba96,
	"wheat4":                 0x8b7e66,
	"white":                  0xffffff,
	"whitesmoke":             0xf5f5f5,
	"white smoke":            0xf5f5f5,
	"x11gray":                0xbebebe,
	"x11 gray":               0xbebebe,
	"x11green":               0x00ff00,
	"x11 green":              0x00ff00,
	"x11grey":                0xbebebe,
	"x11 grey":               0xbebebe,
	"x11maroon":              0xb03060,
	"x11 maroon":             0xb03060,
	"x11purple":              0xa020f0,
	"x11 purple":             0xa020f0,
	"yellow":                 0xffff00,
	"yellow1":                0xffff00,
	"yellow2":                0xeeee00,
	"yellow3":                0xcdcd00,
	"yellow4":                0x8b8b00,
	"yellowgreen":            0x9acd32,
	"yellow green":           0x9acd32,
}
