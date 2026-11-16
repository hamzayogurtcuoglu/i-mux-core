package format

import (
	"strconv"
	"strings"
	"time"
)

// Strftime is strftime(3) in the C locale, as glibc gives it: the conversions, the flags
// _ - 0 ^ # and a field width. An unknown conversion is copied as written.
func Strftime(f string, t time.Time) string {
	var b strings.Builder
	for i := 0; i < len(f); i++ {
		if f[i] != '%' {
			b.WriteByte(f[i])
			continue
		}
		start := i
		i++
		// Flags.
		pad, upper, swap := byte(0), false, false
		for i < len(f) && strings.IndexByte("_-0^#", f[i]) >= 0 {
			switch f[i] {
			case '^':
				upper = true
			case '#':
				swap = true
			default:
				pad = f[i]
			}
			i++
		}
		width := -1
		for i < len(f) && f[i] >= '0' && f[i] <= '9' {
			if width < 0 {
				width = 0
			}
			width = width*10 + int(f[i]-'0')
			if width > 4096 {
				width = 4096
			}
			i++
		}
		// The E and O modifiers change nothing in the C locale.
		for i < len(f) && (f[i] == 'E' || f[i] == 'O') {
			i++
		}
		if i >= len(f) {
			b.WriteString(f[start:])
			break
		}
		s, ok := conversion(f[i], t, pad, width, upper, swap)
		if !ok {
			b.WriteString(f[start : i+1])
			continue
		}
		b.WriteString(s)
	}
	return b.String()
}

var (
	cDays   = []string{"Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"}
	cMonths = []string{
		"January", "February", "March", "April", "May", "June", "July", "August", "September",
		"October", "November", "December",
	}
)

// conversion is one %c of Strftime.
func conversion(c byte, t time.Time, pad byte, width int, upper, swap bool) (string, bool) {
	num := func(n, digits int, def byte) (string, bool) {
		return padNumber(n, digits, def, pad, width), true
	}
	str := func(s string, swapLower bool) (string, bool) {
		if upper || (swap && !swapLower) {
			s = strings.ToUpper(s)
		} else if swap && swapLower {
			s = strings.ToLower(s)
		}
		if width > len(s) {
			fill := " "
			if pad == '0' {
				fill = "0"
			}
			s = strings.Repeat(fill, width-len(s)) + s
		}
		return s, true
	}
	sub := func(f string) (string, bool) {
		s := Strftime(f, t)
		if upper {
			s = strings.ToUpper(s)
		}
		if width > len(s) {
			fill := " "
			if pad == '0' {
				fill = "0"
			}
			s = strings.Repeat(fill, width-len(s)) + s
		}
		return s, true
	}
	hour12 := t.Hour() % 12
	if hour12 == 0 {
		hour12 = 12
	}
	yday := t.YearDay() - 1
	wday := int(t.Weekday())
	switch c {
	case 'a':
		return str(cDays[wday][:3], false)
	case 'A':
		return str(cDays[wday], false)
	case 'b', 'h':
		return str(cMonths[t.Month()-1][:3], false)
	case 'B':
		return str(cMonths[t.Month()-1], false)
	case 'c':
		return sub("%a %b %e %H:%M:%S %Y")
	case 'C':
		return num(floorDiv(t.Year(), 100), 2, '0')
	case 'd':
		return num(t.Day(), 2, '0')
	case 'D', 'x':
		return sub("%m/%d/%y")
	case 'e':
		return num(t.Day(), 2, '_')
	case 'F':
		return sub("%Y-%m-%d")
	case 'G':
		y, _ := t.ISOWeek()
		return num(y, 1, '0')
	case 'g':
		y, _ := t.ISOWeek()
		return num(((y%100)+100)%100, 2, '0')
	case 'H':
		return num(t.Hour(), 2, '0')
	case 'I':
		return num(hour12, 2, '0')
	case 'j':
		return num(yday+1, 3, '0')
	case 'k':
		return num(t.Hour(), 2, '_')
	case 'l':
		return num(hour12, 2, '_')
	case 'm':
		return num(int(t.Month()), 2, '0')
	case 'M':
		return num(t.Minute(), 2, '0')
	case 'n':
		return "\n", true
	case 't':
		return "\t", true
	case 'p':
		if t.Hour() < 12 {
			return str("AM", true)
		}
		return str("PM", true)
	case 'P':
		if t.Hour() < 12 {
			return str("am", false)
		}
		return str("pm", false)
	case 'r':
		return sub("%I:%M:%S %p")
	case 'R':
		return sub("%H:%M")
	case 's':
		return num(int(t.Unix()), 1, '0')
	case 'S':
		return num(t.Second(), 2, '0')
	case 'T', 'X':
		return sub("%H:%M:%S")
	case 'u':
		return num((wday+6)%7+1, 1, '0')
	case 'U':
		return num((yday+7-wday)/7, 2, '0')
	case 'V':
		_, w := t.ISOWeek()
		return num(w, 2, '0')
	case 'w':
		return num(wday, 1, '0')
	case 'W':
		return num((yday+7-(wday+6)%7)/7, 2, '0')
	case 'y':
		return num(((t.Year()%100)+100)%100, 2, '0')
	case 'Y':
		if width < 0 {
			return strconv.Itoa(t.Year()), true
		}
		return num(t.Year(), 1, '0')
	case 'z':
		_, off := t.Zone()
		sign := "+"
		if off < 0 {
			sign, off = "-", -off
		}
		return sign + padNumber(off/3600*100+off%3600/60, 4, '0', 0, -1), true
	case 'Z':
		name, _ := t.Zone()
		return str(name, true)
	case '%':
		return "%", true
	}
	return "", false
}

// padNumber writes n with at least digits digits, padded with def ('0' or '_' for spaces)
// unless the flag says otherwise; a width replaces digits.
func padNumber(n, digits int, def, flag byte, width int) string {
	neg := n < 0
	if neg {
		n = -n
	}
	s := strconv.Itoa(n)
	fill := def
	if flag != 0 {
		fill = flag
	}
	if width >= 0 {
		digits = width
	}
	if neg {
		s = "-" + s
	}
	switch fill {
	case '-':
		return s
	case '_':
		if len(s) < digits {
			s = strings.Repeat(" ", digits-len(s)) + s
		}
	default:
		if len(s) < digits {
			z := strings.Repeat("0", digits-len(s))
			if neg {
				s = "-" + z + s[1:]
			} else {
				s = z + s
			}
		}
	}
	return s
}

func floorDiv(a, b int) int {
	q := a / b
	if (a%b != 0) && ((a < 0) != (b < 0)) {
		q--
	}
	return q
}
