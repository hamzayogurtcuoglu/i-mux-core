package format

import (
	"strconv"
	"strings"
	"time"
)

// strftimeBSD is strftime(3) in the C locale as FreeBSD's C library gives it, which macOS's
// is (its _fmt): the flags - _ 0 change a number's padding, E and O are skipped, and an
// unknown conversion, or one of glibc's (^ # P, a width), is the character itself. tmux on
// macOS expands formats with it.
func strftimeBSD(f string, t time.Time) string {
	var b strings.Builder
	for i := 0; i < len(f); i++ {
		if f[i] != '%' {
			b.WriteByte(f[i])
			continue
		}
		pad := padDefault
	label:
		i++
		if i >= len(f) {
			// The end of the string: FreeBSD steps back and writes the character before it,
			// the % or the last flag or modifier.
			b.WriteByte(f[i-1])
			break
		}
		c := f[i]
		switch c {
		case 'E', 'O':
			goto label
		case '-':
			pad = padLess
			goto label
		case '_':
			pad = padSpace
			goto label
		case '0':
			pad = padZero
			goto label
		}
		if s, ok := bsdConversion(c, t, pad); ok {
			b.WriteString(s)
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

// FreeBSD's padding: the default for the conversion, none, spaces or zeros.
const (
	padDefault = iota
	padLess
	padSpace
	padZero
)

// bsdNum is FreeBSD's fmt_padding: n formatted for the conversion's padding kind (0: "%02d",
// 1: "%2d", 2: "%03d", 3: "%04d" by default) and the flag.
func bsdNum(n, kind, pad int) string {
	width := []int{2, 2, 3, 4}[kind]
	zero := kind != 1
	switch pad {
	case padLess:
		return strconv.Itoa(n)
	case padSpace:
		zero = false
	case padZero:
		zero = true
	}
	s := strconv.Itoa(n)
	if len(s) >= width {
		return s
	}
	if zero {
		return strings.Repeat("0", width-len(s)) + s
	}
	return strings.Repeat(" ", width-len(s)) + s
}

// bsdConversion is one conversion of strftimeBSD; false for an unknown one.
func bsdConversion(c byte, t time.Time, pad int) (string, bool) {
	hour12 := t.Hour() % 12
	if hour12 == 0 {
		hour12 = 12
	}
	yday := t.YearDay() - 1
	wday := int(t.Weekday())
	switch c {
	case 'A':
		return cDays[wday], true
	case 'a':
		return cDays[wday][:3], true
	case 'B':
		return cMonths[t.Month()-1], true
	case 'b', 'h':
		return cMonths[t.Month()-1][:3], true
	case 'C':
		return bsdNum(floorDiv(t.Year(), 100), 0, pad), true
	case 'c':
		return strftimeBSD("%a %b %e %H:%M:%S %Y", t), true
	case 'D', 'x':
		return strftimeBSD("%m/%d/%y", t), true
	case 'd':
		return bsdNum(t.Day(), 0, pad), true
	case 'e':
		return bsdNum(t.Day(), 1, pad), true
	case 'F':
		return strftimeBSD("%Y-%m-%d", t), true
	case 'G':
		y, _ := t.ISOWeek()
		return bsdNum(y, 3, pad), true
	case 'g':
		y, _ := t.ISOWeek()
		return bsdNum(((y%100)+100)%100, 0, pad), true
	case 'H':
		return bsdNum(t.Hour(), 0, pad), true
	case 'I':
		return bsdNum(hour12, 0, pad), true
	case 'j':
		return bsdNum(yday+1, 2, pad), true
	case 'k':
		return bsdNum(t.Hour(), 1, pad), true
	case 'l':
		return bsdNum(hour12, 1, pad), true
	case 'M':
		return bsdNum(t.Minute(), 0, pad), true
	case 'm':
		return bsdNum(int(t.Month()), 0, pad), true
	case 'n':
		return "\n", true
	case 'p':
		if t.Hour() < 12 {
			return "AM", true
		}
		return "PM", true
	case 'R':
		return strftimeBSD("%H:%M", t), true
	case 'r':
		return strftimeBSD("%I:%M:%S %p", t), true
	case 'S':
		return bsdNum(t.Second(), 0, pad), true
	case 's':
		return strconv.FormatInt(t.Unix(), 10), true
	case 'T', 'X':
		return strftimeBSD("%H:%M:%S", t), true
	case 't':
		return "\t", true
	case 'U':
		return bsdNum((yday+7-wday)/7, 0, pad), true
	case 'u':
		return strconv.Itoa((wday+6)%7 + 1), true
	case 'V':
		_, w := t.ISOWeek()
		return bsdNum(w, 0, pad), true
	case 'v':
		return strftimeBSD("%e-%b-%Y", t), true
	case 'W':
		return bsdNum((yday+7-(wday+6)%7)/7, 0, pad), true
	case 'w':
		return strconv.Itoa(wday), true
	case 'Y':
		return strconv.Itoa(t.Year()), true
	case 'y':
		return bsdNum(((t.Year()%100)+100)%100, 0, pad), true
	case 'Z':
		name, _ := t.Zone()
		return name, true
	case 'z':
		_, off := t.Zone()
		sign := "+"
		if off < 0 {
			sign, off = "-", -off
		}
		return sign + bsdNum(off/3600*100+off%3600/60, 3, padZero), true
	case '+':
		return strftimeBSD("%a %b %e %H:%M:%S %Z %Y", t), true
	case '%':
		return "%", true
	}
	return "", false
}
