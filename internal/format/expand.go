// Ported from tmux 3.8's format.c (format_expand1, format_replace, format_build_modifiers,
// format_find and the loops) and format-draw.c (format_width, format_trim_left and
// format_trim_right):
//
// Copyright (c) 2011 Nicholas Marriott <nicholas.marriott@gmail.com>
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

package format

import (
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/cmdlang"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/model"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/options"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/vt"
)

// tmux's limits on an expansion.
const (
	maxWidth      = 10000 // FORMAT_MAX_WIDTH: =N and pN
	maxRepeat     = 10000 // FORMAT_MAX_REPEAT: R
	maxRepeatSize = 65536 // FORMAT_MAX_REPEAT_SIZE
	maxPrecision  = 100   // FORMAT_MAX_PRECISION: e|op|f|N
	loopLimit     = 100   // FORMAT_LOOP_LIMIT: nested expansions
	// TimeLimit is FORMAT_TIME_LIMIT: an expansion that takes longer gives "".
	TimeLimit      = 100 * time.Millisecond
	timeLoopCheck  = 10000 // FORMAT_TIME_LOOP_CHECK
	strftimeLimit  = 8192  // format_expand1's buffer for strftime
	timeFormatSize = 512   // format_find's buffer for t/f
)

// Modifier flags (format.c's FORMAT_*).
const (
	modTimestring uint64 = 1 << iota
	modBasename
	modDirname
	modQuoteShell
	modLiteral
	modExpand
	modExpandTime
	modSessions
	modWindows
	modPanes
	modPretty
	modLength
	modWidth
	modQuoteStyle
	modWindowName
	modSessionName
	modCharacter
	modColour
	modClients
	modNot
	modNotNot
	modRepeat
	modQuoteArguments
	modRelative
	modClientTermcap
	modClientTermfeat
	modClientEnviron
	modColourEscFg
	modColourEscBg
	modQuoteShellSQ
	modOptions
	modEnviron
	modDifference
	modCycle
)

// Sort orders of the loops (sort.c).
const (
	sortOrder = iota
	sortIndex
	sortName
	sortActivity
	sortCreation
	sortZ
)

type sortCriteria struct {
	order    int
	reversed bool
}

// state is format_expand_state: one expansion.
type state struct {
	ctx      *Context
	loop     int
	expandTm bool // FORMAT_EXPAND_TIME: strftime first
	tm       time.Time
	start    time.Time
	// timedOut is set once the expansion has taken longer than TimeLimit.
	timedOut bool
}

func (es *state) copyState(flags bool) *state {
	return &state{ctx: es.ctx, loop: es.loop, expandTm: flags, tm: es.tm, start: es.start}
}

// checkTime is format_check_time: false once the expansion has run out of time. With a
// counter it looks at the clock every timeLoopCheck calls.
func (es *state) checkTime(check *int) bool {
	if es.timedOut {
		return false
	}
	if check != nil {
		*check++
		if *check%timeLoopCheck != 0 {
			return true
		}
	}
	if time.Since(es.start) < TimeLimit {
		return true
	}
	es.timedOut = true
	return false
}

// Expand is format_expand: #{…} replaced, #[…] kept for the status renderer.
func (c *Context) Expand(f string) string {
	es := &state{ctx: c, start: time.Now()}
	return es.expand(f)
}

// ExpandTime is format_expand_time: as Expand, after strftime(3) at the context's time.
func (c *Context) ExpandTime(f string) string {
	es := &state{ctx: c, expandTm: true, start: time.Now()}
	return es.expand(f)
}

// unescape is format_unescape: the escapes outside #{…} lose their #.
func (es *state) unescape(s string) string {
	var b strings.Builder
	brackets, check := 0, 0
	for i := 0; i < len(s); i++ {
		if !es.checkTime(&check) {
			return ""
		}
		if s[i] == '#' && i+1 < len(s) && s[i+1] == '{' {
			brackets++
		}
		if brackets == 0 && s[i] == '#' && i+1 < len(s) && strings.IndexByte(",#{}:", s[i+1]) >= 0 {
			i++
			b.WriteByte(s[i])
			continue
		}
		if s[i] == '}' {
			brackets--
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// strip is format_strip: the escapes outside #{…} are removed.
func (es *state) strip(s string) string {
	var b strings.Builder
	brackets, check := 0, 0
	for i := 0; i < len(s); i++ {
		if !es.checkTime(&check) {
			return ""
		}
		if s[i] == '#' && i+1 < len(s) && s[i+1] == '{' {
			brackets++
		}
		if s[i] == '#' && i+1 < len(s) && strings.IndexByte(",#{}:", s[i+1]) >= 0 {
			if brackets != 0 {
				b.WriteByte(s[i])
			}
			continue
		}
		if s[i] == '}' {
			brackets--
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// skip is format_skip1: the index of the first byte of end outside #{…}, or -1.
func (es *state) skip(s, end string) int {
	if !es.checkTime(nil) {
		return -1
	}
	brackets, check := 0, 0
	for i := 0; i < len(s); i++ {
		if !es.checkTime(&check) {
			return -1
		}
		if s[i] == '#' && i+1 < len(s) && s[i+1] == '{' {
			brackets++
		}
		if s[i] == '#' && i+1 < len(s) && strings.IndexByte(",#{}:", s[i+1]) >= 0 {
			i++
			continue
		}
		if s[i] == '}' {
			brackets--
		}
		if strings.IndexByte(end, s[i]) >= 0 && brackets == 0 {
			return i
		}
	}
	return -1
}

// choose is format_choose: s split at its first top-level comma, each side expanded if asked.
func (es *state) choose(s string, expand bool) (left, right string, ok bool) {
	i := es.skip(s, ",")
	if i < 0 {
		return "", "", false
	}
	left, right = s[:i], s[i+1:]
	if expand {
		left, right = es.expand(left), es.expand(right)
	}
	return left, right, true
}

// True is format_true: not empty and not "0".
func True(s string) bool { return s != "" && s != "0" }

type modifier struct {
	name string
	argv []string
}

// buildModifiers is format_build_modifiers: the modifiers before the ':' of a key, their
// arguments unescaped and expanded, and the rest of the key. ok is false when there are no
// modifiers.
func (es *state) buildModifiers(s string) ([]modifier, string, bool) {
	var list []modifier
	i := 0
	for i < len(s) && s[i] != ':' {
		if s[i] == ';' {
			i++
		}
		if i >= len(s) {
			break
		}
		c := s[i]
		// Single character modifiers with no arguments.
		if strings.IndexByte("labdnwETSWPOVL!<>A", c) >= 0 && isEnd(s, i+1) {
			list = append(list, modifier{name: string(c)})
			i++
			continue
		}
		// Double character modifiers with no arguments.
		if i+2 <= len(s) && isEnd(s, i+2) {
			switch s[i : i+2] {
			case "||", "&&", "!!", "!=", "==", "<=", ">=":
				list = append(list, modifier{name: s[i : i+2]})
				i += 2
				continue
			}
		}
		// Single character modifiers with arguments.
		if strings.IndexByte("ImCLNPSOVst=pReqWcA", c) < 0 {
			break
		}
		if isEnd(s, i+1) {
			list = append(list, modifier{name: string(c)})
			i++
			continue
		}
		if i+1 >= len(s) {
			break
		}
		// A single argument with no wrapper character.
		if !isPunct(s[i+1]) || s[i+1] == '-' {
			e := es.skip(s[i+1:], ":;")
			if e < 0 {
				break
			}
			arg := es.expand(es.unescape(s[i+1 : i+1+e]))
			list = append(list, modifier{name: string(c), argv: []string{arg}})
			i = i + 1 + e
			continue
		}
		// Several arguments with a wrapper character.
		last := string([]byte{s[i+1], ';', ':'})
		m := modifier{name: string(c)}
		i++
		for {
			if s[i] == last[0] && isEnd(s, i+1) {
				i++
				break
			}
			e := es.skip(s[i+1:], last)
			if e < 0 {
				break
			}
			m.argv = append(m.argv, es.expand(es.unescape(s[i+1:i+1+e])))
			i = i + 1 + e
			if i >= len(s) || isEnd(s, i) {
				break
			}
		}
		list = append(list, m)
	}
	if i >= len(s) || s[i] != ':' {
		return nil, s, false
	}
	return list, s[i+1:], true
}

// expand is format_expand1.
func (es *state) expand(f string) string {
	if f == "" || !es.checkTime(nil) {
		return ""
	}
	if es.loop == loopLimit {
		return ""
	}
	es.loop++
	if es.expandTm && strings.Contains(f, "%") {
		if es.tm.IsZero() {
			es.tm = es.ctx.now()
		}
		expanded := Strftime(f, es.tm)
		if len(expanded) >= strftimeLimit {
			return ""
		}
		f = expanded
	}
	var b strings.Builder
	styleEnd := -1
	i := 0
loop:
	for i < len(f) {
		if f[i] != '#' {
			b.WriteByte(f[i])
			i++
			continue
		}
		i++
		if i >= len(f) {
			break
		}
		ch := f[i]
		i++
		switch ch {
		case '(':
			brackets, j := 1, i
			for ; j < len(f); j++ {
				if f[j] == '(' {
					brackets++
				}
				if f[j] == ')' {
					brackets--
					if brackets == 0 {
						break
					}
				}
			}
			if j >= len(f) || brackets != 0 {
				break loop
			}
			b.WriteString(es.ctx.job(f[i:j]))
			i = j + 1
		case '{':
			e := es.skip(f[i-2:], "}")
			if e < 0 {
				break loop
			}
			end := i - 2 + e
			if !es.replace(f[i:end], &b) {
				break loop
			}
			i = end + 1
		case '[', '#':
			// A style (#[ or an odd run of #s before [) is kept for the status renderer;
			// otherwise ## is #.
			ptr, n := i, 2
			if ch == '[' {
				ptr, n = i-1, 1
			}
			for ptr < len(f) && f[ptr] == '#' {
				ptr++
				n++
			}
			if ptr < len(f) && f[ptr] == '[' {
				styleEnd = -1
				if e := es.skip(f[i-2:], "]"); e >= 0 {
					styleEnd = i - 2 + e
				}
				b.WriteString(f[i-2 : i-2+n+1])
				i = ptr + 1
				continue
			}
			b.WriteString(strings.Repeat("#", n/2))
			if n%2 != 0 {
				i = ptr - 1
			} else {
				i = ptr
			}
		case '}', ',':
			b.WriteByte(ch)
		default:
			name := ""
			if styleEnd < 0 || i > styleEnd { // not inside #[…]
				name = aliasFor(ch)
			}
			if name == "" {
				b.WriteByte('#')
				b.WriteByte(ch)
				continue
			}
			if !es.replace(name, &b) {
				break loop
			}
		}
	}
	es.loop--
	return b.String()
}

// aliasFor is format_upper and format_lower.
func aliasFor(c byte) string {
	if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') {
		return Aliases[c]
	}
	return ""
}

// replace is format_replace: one #{…} key, written to b. It is false on a syntax error, which
// ends the expansion there.
func (es *state) replace(key string, b *strings.Builder) bool {
	var (
		modifiers  uint64
		limit      int
		width      int
		marker     *string
		timeFormat *string
		cmp        *modifier
		search     *modifier
		subs       []*modifier
		mexp       *modifier
		boolOpN    *modifier
		loopFlags  string
		sc         = sortCriteria{order: sortOrder}
	)
	list, copy, _ := es.buildModifiers(key)
	sortFrom := func(m *modifier, def int, letters map[byte]int, fallback int) {
		if len(m.argv) < 1 {
			sc = sortCriteria{order: def}
			return
		}
		sc.order = fallback
		for _, k := range []byte("inttz") {
			if o, ok := letters[k]; ok && strings.IndexByte(m.argv[0], k) >= 0 {
				sc.order = o
				break
			}
		}
		sc.reversed = strings.IndexByte(m.argv[0], 'r') >= 0
	}
	for i := range list {
		fm := &list[i]
		has := func(c byte) bool { return len(fm.argv) >= 1 && strings.IndexByte(fm.argv[0], c) >= 0 }
		switch fm.name {
		case "m", "<", ">", "==", "!=", ">=", "<=":
			cmp = fm
		case "!":
			modifiers |= modNot
		case "!!":
			modifiers |= modNotNot
		case "||", "&&":
			boolOpN = fm
		case "C":
			search = fm
		case "s":
			if len(fm.argv) >= 2 {
				subs = append(subs, fm)
			}
		case "=":
			if len(fm.argv) < 1 {
				break
			}
			limit = strtonumOr(fm.argv[0], -maxWidth, maxWidth, 0)
			if len(fm.argv) >= 2 {
				marker = &fm.argv[1]
			}
		case "p":
			if len(fm.argv) >= 1 {
				width = strtonumOr(fm.argv[0], -maxWidth, maxWidth, 0)
			}
		case "A":
			modifiers |= modCycle
		case "w":
			modifiers |= modWidth
		case "e":
			if len(fm.argv) >= 1 && len(fm.argv) <= 3 {
				mexp = fm
			}
		case "l":
			modifiers |= modLiteral
		case "a":
			modifiers |= modCharacter
		case "b":
			modifiers |= modBasename
		case "c":
			modifiers |= modColour
			if has('f') {
				modifiers |= modColourEscFg
			}
			if has('b') {
				modifiers |= modColourEscBg
			}
		case "d":
			modifiers |= modDirname
		case "n":
			modifiers |= modLength
		case "I":
			if has('f') {
				modifiers |= modClientTermfeat
			}
			if has('c') {
				modifiers |= modClientTermcap
			}
			if has('e') {
				modifiers |= modClientEnviron
			}
		case "t":
			modifiers |= modTimestring
			switch {
			case len(fm.argv) < 1:
			case has('p'):
				modifiers |= modPretty
			case has('r'):
				modifiers |= modRelative
			case has('d'):
				modifiers |= modDifference
			case len(fm.argv) >= 2 && has('f'):
				tf := es.strip(fm.argv[1])
				timeFormat = &tf
			}
		case "q":
			switch {
			case len(fm.argv) < 1:
				modifiers |= modQuoteShell
			case has('s'):
				modifiers |= modQuoteShellSQ
			case has('e') || has('h'):
				modifiers |= modQuoteStyle
			case has('a'):
				modifiers |= modQuoteArguments
			}
		case "E":
			modifiers |= modExpand
		case "T":
			modifiers |= modExpandTime
		case "N":
			if len(fm.argv) < 1 || has('w') {
				modifiers |= modWindowName
			} else if has('s') {
				modifiers |= modSessionName
			}
		case "S":
			modifiers |= modSessions
			sortFrom(fm, sortIndex, map[byte]int{'i': sortIndex, 'n': sortName, 't': sortActivity}, sortIndex)
		case "W":
			modifiers |= modWindows
			sortFrom(fm, sortOrder, map[byte]int{'i': sortOrder, 'n': sortName, 't': sortActivity}, sortOrder)
		case "P":
			modifiers |= modPanes
			sc.order = sortCreation
			if len(fm.argv) < 1 {
				sc.reversed = false
				break
			}
			switch {
			case has('i'):
				sc.order = sortIndex
			case has('z'):
				sc.order = sortZ
			}
			sc.reversed = has('r')
		case "O":
			modifiers |= modOptions
			if len(fm.argv) == 1 {
				loopFlags = fm.argv[0]
			}
		case "V":
			modifiers |= modEnviron
			if len(fm.argv) == 1 {
				loopFlags = fm.argv[0]
			}
		case "L":
			modifiers |= modClients
			sortFrom(fm, sortOrder, map[byte]int{'i': sortOrder, 'n': sortName, 't': sortActivity}, sortOrder)
		case "R":
			modifiers |= modRepeat
		}
	}
	_ = loopFlags

	var value string
	switch {
	case modifiers&(modClientTermcap|modClientTermfeat|modClientEnviron) != 0:
		value = es.ctx.clientQuery(modifiers, copy)
	case modifiers&modCycle != 0:
		// A cycle only moves in a status format (task C4.2); elsewhere it is empty.
		value = ""
	case modifiers&modLiteral != 0:
		value = es.unescape(copy)
	case modifiers&modCharacter != 0:
		c, ok := strtonum(es.expand(copy), 32, 126)
		if ok {
			value = string(rune(c))
		}
	case modifiers&modColour != 0:
		value = es.ctx.colour(modifiers, es.expand(copy))
	default:
		var ok bool
		value, ok = es.replaceValue(copy, modifiers, sc, cmp, search, mexp, boolOpN, timeFormat)
		if !ok {
			return false
		}
	}

	// Expand again if asked.
	if modifiers&modExpand != 0 {
		value = es.expand(value)
	} else if modifiers&modExpandTime != 0 {
		value = es.copyState(true).expand(value)
	}
	// Substitutions.
	for _, m := range subs {
		value = substitute(m, value, es.expand(m.argv[0]), es.expand(m.argv[1]))
	}
	// Length limit.
	if limit > 0 {
		trimmed := TrimLeft(value, limit)
		if marker != nil && trimmed != value {
			value = trimmed + *marker
		} else {
			value = trimmed
		}
	} else if limit < 0 {
		trimmed := TrimRight(value, -limit)
		if marker != nil && trimmed != value {
			value = *marker + trimmed
		} else {
			value = trimmed
		}
	}
	// Padding.
	if width > 0 {
		if n := cstrWidth(value); n < width {
			value += strings.Repeat(" ", width-n)
		}
	} else if width < 0 {
		if n := cstrWidth(value); n < -width {
			value = strings.Repeat(" ", -width-n) + value
		}
	}
	if modifiers&modLength != 0 {
		value = strconv.Itoa(len(value))
	}
	if modifiers&modWidth != 0 {
		value = strconv.Itoa(Width(value))
	}
	b.WriteString(value)
	return true
}

// replaceValue is the middle of format_replace: a loop, operator, comparison, condition or
// variable.
func (es *state) replaceValue(copy string, modifiers uint64, sc sortCriteria, cmp, search, mexp, boolOpN *modifier, timeFormat *string) (string, bool) {
	ctx := es.ctx
	switch {
	case modifiers&modSessions != 0:
		return es.loopSessions(copy, sc), true
	case modifiers&modWindows != 0:
		if ctx.S == nil {
			return "", false
		}
		return es.loopWindows(copy, sc), true
	case modifiers&modPanes != 0:
		if ctx.W == nil {
			return "", false
		}
		return es.loopPanes(copy, sc), true
	case modifiers&modClients != 0:
		return es.loopClients(copy, sc), true
	case modifiers&(modOptions|modEnviron) != 0:
		// The option and environment loops are not in i-mux's contract.
		return "", true
	case modifiers&modWindowName != 0:
		if ctx.S == nil {
			return "", false
		}
		name := es.expand(copy)
		for _, wl := range ctx.S.Links() {
			if wl.Window.Name == name {
				return "1", true
			}
		}
		return "0", true
	case modifiers&modSessionName != 0:
		name := es.expand(copy)
		if ctx.Tree != nil && ctx.Tree.SessionByName(name) != nil {
			return "1", true
		}
		return "0", true
	case search != nil:
		// Searching a pane's history comes with copy mode (task C5.x).
		es.expand(copy)
		return "0", true
	case modifiers&modRepeat != 0:
		left, right, ok := es.choose(copy, true)
		if !ok {
			return "", false
		}
		n, ok := strtonum(right, 1, maxRepeat)
		if !ok || (len(left) != 0 && n > maxRepeatSize/len(left)) {
			return "", true
		}
		return strings.Repeat(left, n), true
	case modifiers&modNot != 0:
		return boolString(!True(es.expand(copy))), true
	case modifiers&modNotNot != 0:
		return boolString(True(es.expand(copy))), true
	case boolOpN != nil:
		and := boolOpN.name == "&&"
		result := and
		rest := copy
		for result == and {
			e := es.skip(rest, ",")
			raw := rest
			if e >= 0 {
				raw = rest[:e]
			}
			v := True(es.expand(raw))
			if and {
				result = result && v
			} else {
				result = result || v
			}
			if e < 0 {
				break
			}
			rest = rest[e+1:]
		}
		return boolString(result), true
	case cmp != nil:
		left, right, ok := es.choose(copy, true)
		if !ok {
			return "", false
		}
		switch cmp.name {
		case "==":
			return boolString(left == right), true
		case "!=":
			return boolString(left != right), true
		case "<":
			return boolString(left < right), true
		case ">":
			return boolString(left > right), true
		case "<=":
			return boolString(left <= right), true
		case ">=":
			return boolString(left >= right), true
		}
		return match(cmp, left, right), true
	case strings.HasPrefix(copy, "?"):
		return es.condition(copy[1:], modifiers, timeFormat), true
	case mexp != nil:
		v, _ := es.expression(mexp, copy)
		return v, true
	case strings.Contains(copy, "#{"):
		return es.expand(copy), true
	}
	v, _ := ctx.find(copy, modifiers, timeFormat)
	return v, true
}

// condition is #{?c1,v1,c2,v2,…,else}: the value of the first true condition.
func (es *state) condition(cp string, modifiers uint64, timeFormat *string) string {
	for {
		e := es.skip(cp, ",")
		if e < 0 {
			return es.expand(cp)
		}
		cond := cp[:e]
		found, ok := es.ctx.find(cond, modifiers, timeFormat)
		if !ok {
			// A condition that is not a variable is expanded; no change means false.
			found = es.expand(cond)
			if found == cond {
				found = ""
			}
		}
		cp = cp[e+1:]
		e = es.skip(cp, ",")
		if True(found) {
			if e < 0 {
				return es.expand(cp)
			}
			return es.expand(cp[:e])
		}
		if e < 0 {
			return ""
		}
		cp = cp[e+1:]
	}
}

func boolString(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

// expression is format_replace_expression: #{e|op|flags|precision:a,b}.
func (es *state) expression(m *modifier, copy string) (string, bool) {
	op := m.argv[0]
	switch op {
	case "+", "-", "*", "/", "%", "m", "==", "!=", ">", "<", ">=", "<=":
	default:
		return "", false
	}
	useFP, prec := false, 0
	if len(m.argv) >= 2 && strings.IndexByte(m.argv[1], 'f') >= 0 {
		useFP, prec = true, 2
	}
	if len(m.argv) >= 3 {
		p, ok := strtonum(m.argv[2], -maxPrecision, maxPrecision)
		if !ok {
			return "", false
		}
		prec = p
	}
	left, right, ok := es.choose(copy, true)
	if !ok {
		return "", false
	}
	l, ok := strtod(left)
	if !ok {
		return "", false
	}
	r, ok := strtod(right)
	if !ok {
		return "", false
	}
	if !useFP {
		l, r = float64(cLongLong(l)), float64(cLongLong(r))
	}
	var result float64
	b2f := func(b bool) float64 {
		if b {
			return 1
		}
		return 0
	}
	switch op {
	case "+":
		result = l + r
	case "-":
		result = l - r
	case "*":
		result = l * r
	case "/":
		result = l / r
	case "%", "m":
		result = math.Mod(l, r)
	case "==":
		result = b2f(math.Abs(l-r) < 1e-9)
	case "!=":
		result = b2f(math.Abs(l-r) > 1e-9)
	case ">":
		result = b2f(l > r)
	case ">=":
		result = b2f(l >= r)
	case "<":
		result = b2f(l < r)
	case "<=":
		result = b2f(l <= r)
	}
	if !useFP {
		result = float64(cLongLong(result))
	}
	return printfFloat(result, prec), true
}

// printfFloat is printf("%.*f", prec, f); a negative precision is the default, 6.
func printfFloat(f float64, prec int) string {
	if prec < 0 {
		prec = 6
	}
	switch {
	case math.IsInf(f, 1):
		return "inf"
	case math.IsInf(f, -1):
		return "-inf"
	case math.IsNaN(f):
		if math.Signbit(f) {
			return "-nan"
		}
		return "nan"
	}
	return strconv.FormatFloat(f, 'f', prec, 64)
}

// strtod is strtod(3) needing the whole string: "" is 0.
func strtod(s string) (float64, bool) {
	if s == "" {
		return 0, true
	}
	t := strings.TrimLeft(s, " \t\n\v\f\r")
	if t == "" {
		return 0, false
	}
	body := strings.TrimLeft(t, "+-")
	if len(t)-len(body) > 1 {
		return 0, false
	}
	neg := strings.HasPrefix(t, "-")
	if len(body) > 2 && body[0] == '0' && (body[1] == 'x' || body[1] == 'X') && !strings.ContainsAny(body, "pP") {
		body += "p0"
	}
	if strings.ContainsRune(body, '_') {
		return 0, false
	}
	f, err := strconv.ParseFloat(body, 64)
	if err != nil {
		if ne, ok := err.(*strconv.NumError); !ok || ne.Err != strconv.ErrRange {
			return 0, false
		}
	}
	if neg {
		f = -f
	}
	return f, true
}

// strtonum is strtonum(3).
func strtonum(s string, lo, hi int) (int, bool) {
	n, err := strconv.ParseInt(strings.TrimLeft(s, " \t\n\v\f\r"), 10, 64)
	if err != nil || n < int64(lo) || n > int64(hi) {
		return 0, false
	}
	return int(n), true
}

func strtonumOr(s string, lo, hi, def int) int {
	if n, ok := strtonum(s, lo, hi); ok {
		return n
	}
	return def
}

// match is format_match: an fnmatch(3) pattern, or with r a regular expression; i ignores
// case. The fuzzy matches (p and z) are not in i-mux's contract and do not match.
func match(m *modifier, pattern, text string) string {
	flags := ""
	if len(m.argv) >= 1 {
		flags = m.argv[0]
	}
	switch {
	case strings.ContainsAny(flags, "p"):
		return ""
	case strings.ContainsAny(flags, "z"):
		return "0"
	case !strings.ContainsRune(flags, 'r'):
		return boolString(Fnmatch(pattern, text, strings.ContainsRune(flags, 'i')))
	}
	re, err := compileRegex(pattern, strings.ContainsRune(flags, 'i'))
	if err != nil {
		return "0"
	}
	return boolString(re.MatchString(text))
}

// substitute is format_sub: regsub with s/pattern/with/flags.
func substitute(m *modifier, text, pattern, with string) string {
	icase := len(m.argv) >= 3 && strings.ContainsRune(m.argv[2], 'i')
	v, ok := Regsub(pattern, with, text, icase)
	if !ok {
		return text
	}
	return v
}

// Loops.

// loopTree is the format tree of one item of a loop (format_create and format_defaults).
func (c *Context) loopTree(s *model.Session, wl *model.Winlink, p *model.Pane, values map[string]string) *Context {
	n := &Context{
		Tree: c.Tree, Server: c.Server, Clients: c.Clients, Environ: c.Environ, Now: c.Now,
		ItemClient: c.ItemClient, Values: values,
	}
	n.Defaults(c.Client, s, wl, p)
	return n
}

func (es *state) loopSessions(f string, sc sortCriteria) string {
	ctx := es.ctx
	all, active, ok := es.choose(f, false)
	if !ok {
		all, active = f, ""
	}
	var list []*model.Session
	if ctx.Tree != nil {
		list = ctx.Tree.Sessions()
	}
	sortSessions(list, sc)
	var b strings.Builder
	for i, s := range list {
		use := all
		if ok && ctx.Client != nil && ctx.Client.Session == s {
			use = active
		}
		nft := ctx.loopTree(s, nil, nil, map[string]string{
			"loop_index": strconv.Itoa(i), "loop_last_flag": boolString(i == len(list)-1),
		})
		next := es.copyState(false)
		next.ctx = nft
		b.WriteString(next.expand(use))
	}
	return b.String()
}

func (es *state) loopWindows(f string, sc sortCriteria) string {
	ctx := es.ctx
	s := ctx.S
	all, active, ok := es.choose(f, false)
	if !ok {
		all, active = f, ""
	}
	list := s.Links()
	sortWinlinks(list, sc)
	var b strings.Builder
	for i, wl := range list {
		use := all
		if ok && wl == s.Current {
			use = active
		}
		v := map[string]string{
			"loop_index": strconv.Itoa(i), "loop_last_flag": boolString(i == len(list)-1),
			"window_after_active":  boolString(i > 0 && list[i-1] == s.Current),
			"window_before_active": boolString(i+1 < len(list) && list[i+1] == s.Current),
		}
		if i+1 < len(list) {
			addAdjacent(v, list[i+1], s, "next")
		}
		if i > 0 {
			addAdjacent(v, list[i-1], s, "prev")
		}
		nft := ctx.loopTree(s, wl, nil, v)
		next := es.copyState(false)
		next.ctx = nft
		b.WriteString(next.expand(use))
	}
	return b.String()
}

// addAdjacent adds the next_ and prev_ variables of a window loop, as tmux does.
func addAdjacent(v map[string]string, wl *model.Winlink, s *model.Session, prefix string) {
	v[prefix+"_window_index"] = strconv.Itoa(wl.Index)
	v[prefix+"_window_active"] = boolString(wl == s.Current)
	for _, o := range wl.Window.Options.Sorted() {
		if strings.HasPrefix(o.Name, "@") {
			v[prefix+"_"+o.Name] = o.String("", false, true)
		}
	}
}

func (es *state) loopPanes(f string, sc sortCriteria) string {
	ctx := es.ctx
	w := ctx.W
	all, active, ok := es.choose(f, false)
	if !ok {
		all, active = f, ""
	}
	list := append([]*model.Pane(nil), w.Panes...)
	sortPanes(w, list, sc)
	var b strings.Builder
	for i, p := range list {
		use := all
		if ok && p == w.Active {
			use = active
		}
		nft := ctx.loopTree(ctx.S, ctx.WL, p, map[string]string{
			"loop_index": strconv.Itoa(i), "loop_last_flag": boolString(i == len(list)-1),
		})
		next := es.copyState(false)
		next.ctx = nft
		b.WriteString(next.expand(use))
	}
	return b.String()
}

func (es *state) loopClients(f string, sc sortCriteria) string {
	ctx := es.ctx
	all, active, ok := es.choose(f, false)
	if !ok {
		all, active = f, ""
	}
	list := append([]*Client(nil), ctx.Clients...)
	sortClients(list, sc)
	var b strings.Builder
	for i, c := range list {
		use := all
		if ok && ctx.Client != nil && c.Name == ctx.Client.Name {
			use = active
		}
		n := &Context{
			Tree: ctx.Tree, Server: ctx.Server, Clients: ctx.Clients, Environ: ctx.Environ,
			Now: ctx.Now, ItemClient: ctx.ItemClient,
			Values: map[string]string{
				"loop_index": strconv.Itoa(i), "loop_last_flag": boolString(i == len(list)-1),
			},
		}
		n.Defaults(c, nil, nil, nil)
		next := es.copyState(false)
		next.ctx = n
		b.WriteString(next.expand(use))
	}
	return b.String()
}

func sortSessions(l []*model.Session, sc sortCriteria) {
	if sc.order == sortOrder {
		if sc.reversed {
			reverse(l)
		}
		return
	}
	sort.SliceStable(l, func(i, j int) bool {
		a, b := l[i], l[j]
		r := 0
		switch sc.order {
		case sortIndex:
			r = cmpInt(a.ID, b.ID)
		case sortCreation:
			r = a.Created.Compare(b.Created)
		case sortActivity:
			r = -a.Activity.Compare(b.Activity)
		case sortName:
			r = strings.Compare(a.Name, b.Name)
		}
		if r == 0 {
			r = strings.Compare(a.Name, b.Name)
		}
		if sc.reversed {
			r = -r
		}
		return r < 0
	})
}

func sortWinlinks(l []*model.Winlink, sc sortCriteria) {
	if sc.order == sortOrder {
		if sc.reversed {
			reverse(l)
		}
		return
	}
	sort.SliceStable(l, func(i, j int) bool {
		a, b := l[i], l[j]
		r := 0
		switch sc.order {
		case sortIndex:
			r = cmpInt(a.Index, b.Index)
		case sortName:
			r = strings.Compare(a.Window.Name, b.Window.Name)
		}
		if r == 0 {
			r = strings.Compare(a.Window.Name, b.Window.Name)
		}
		if sc.reversed {
			r = -r
		}
		return r < 0
	})
}

func sortPanes(w *model.Window, l []*model.Pane, sc sortCriteria) {
	if sc.order == sortOrder {
		if sc.reversed {
			reverse(l)
		}
		return
	}
	z := func(p *model.Pane) int {
		for i, q := range w.ZOrder {
			if q == p {
				return i
			}
		}
		return -1
	}
	sort.SliceStable(l, func(i, j int) bool {
		a, b := l[i], l[j]
		r := 0
		switch sc.order {
		case sortCreation:
			r = cmpInt(a.ID, b.ID)
		case sortIndex:
			r = cmpInt(w.PaneIndex(a), w.PaneIndex(b))
		case sortZ:
			r = cmpInt(z(a), z(b))
		}
		if r == 0 {
			r = strings.Compare(paneTitle(a), paneTitle(b))
		}
		if sc.reversed {
			r = -r
		}
		return r < 0
	})
}

func sortClients(l []*Client, sc sortCriteria) {
	if sc.order == sortOrder {
		if sc.reversed {
			reverse(l)
		}
		return
	}
	sort.SliceStable(l, func(i, j int) bool {
		a, b := l[i], l[j]
		r := 0
		switch sc.order {
		case sortName:
			r = strings.Compare(a.Name, b.Name)
		case sortActivity:
			r = -a.Activity.Compare(b.Activity)
		}
		if r == 0 {
			r = strings.Compare(a.Name, b.Name)
		}
		if sc.reversed {
			r = -r
		}
		return r < 0
	})
}

func reverse[T any](l []T) {
	for i, j := 0, len(l)-1; i < j; i, j = i+1, j-1 {
		l[i], l[j] = l[j], l[i]
	}
}

func cmpInt(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// find is format_find: an option (pane, window, session and global), a variable, an added
// value or an environment variable, with the time, path and quoting modifiers applied. ok is
// false when the key is none of them.
func (c *Context) find(key string, modifiers uint64, timeFormat *string) (string, bool) {
	var (
		found    string
		hasFound bool
		t        int64
	)
	if o, k, hasKey := c.option(key); o != nil {
		found, hasFound = o.String(k, hasKey, true), true
	} else if v, ok := lookupStringVar(key); ok {
		found, hasFound = v(c)
	} else if v, ok := timeVars[key]; ok {
		if tm, ok := v(c); ok && !tm.IsZero() {
			t = tm.Unix()
		}
	} else if v, ok := c.Values[key]; ok {
		found, hasFound = v, true
	} else if modifiers&modTimestring == 0 && c.Environ != nil {
		if v, ok := c.Environ(c.S, key); ok {
			found, hasFound = v, true
		} else {
			return "", false
		}
	} else {
		return "", false
	}

	if modifiers&modTimestring != 0 {
		if t == 0 && hasFound {
			n, err := strconv.ParseInt(found, 10, 64)
			if err == nil && n >= 0 {
				t = n
			}
		}
		if t == 0 {
			return "", false
		}
		now := c.now()
		switch {
		case modifiers&modRelative != 0:
			return relativeTime(t, now)
		case modifiers&modDifference != 0:
			return strconv.FormatInt(now.Unix()-t, 10), true
		case modifiers&modPretty != 0:
			return PrettyTime(time.Unix(t, 0), now, false), true
		case timeFormat != nil:
			s := Strftime(*timeFormat, time.Unix(t, 0))
			if len(s) >= timeFormatSize {
				s = ""
			}
			return s, true
		}
		return time.Unix(t, 0).Format("Mon Jan _2 15:04:05 2006"), true
	}
	if t != 0 {
		found = strconv.FormatInt(t, 10)
	} else if !hasFound {
		return "", false
	}
	if modifiers&modBasename != 0 {
		found = basename(found)
	}
	if modifiers&modDirname != 0 {
		found = dirname(found)
	}
	if modifiers&modQuoteShell != 0 {
		found = quoteShell(found)
	}
	if modifiers&modQuoteShellSQ != 0 {
		found = "'" + strings.ReplaceAll(found, "'", `'\''`) + "'"
	}
	if modifiers&modQuoteStyle != 0 {
		found = strings.ReplaceAll(found, "#", "##")
	}
	if modifiers&modQuoteArguments != 0 {
		found = cmdlang.Escape(found)
	}
	return found, true
}

// option is the option lookup of format_find: the server's, then the pane's, window's and
// session's with their parents.
func (c *Context) option(key string) (*options.Option, string, bool) {
	name, k, hasKey, ok := options.ParseName(key)
	if !ok {
		return nil, "", false
	}
	var lists []*options.Options
	if c.Tree != nil {
		lists = append(lists, c.Tree.Options.Server)
	}
	if c.P != nil {
		lists = append(lists, c.P.Options)
	}
	if c.W != nil {
		lists = append(lists, c.W.Options)
	}
	if c.Tree != nil {
		lists = append(lists, c.Tree.Options.Window)
	}
	if c.S != nil {
		lists = append(lists, c.S.Options)
	}
	if c.Tree != nil {
		lists = append(lists, c.Tree.Options.Session)
	}
	for _, oo := range lists {
		if o := oo.Get(name); o != nil {
			return o, k, hasKey
		}
	}
	return nil, "", false
}

// dirname is dirname(3).
func dirname(s string) string {
	if s == "" {
		return "."
	}
	i := len(s)
	for i > 1 && s[i-1] == '/' {
		i--
	}
	j := strings.LastIndexByte(s[:i], '/')
	switch {
	case j < 0:
		return "."
	case j == 0:
		return "/"
	}
	for j > 1 && s[j-1] == '/' {
		j--
	}
	if j == 0 {
		return "/"
	}
	return s[:j]
}

// quoteShell is format_quote_shell.
func quoteShell(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if strings.IndexByte("|&;<>(){}$`\\\"'*?[# =%\n\t", s[i]) >= 0 {
			b.WriteByte('\\')
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// relativeTime is format_relative_time.
func relativeTime(t int64, now time.Time) (string, bool) {
	n := now.Unix()
	if t > n {
		return "", false
	}
	if t == n {
		return "0s", true
	}
	age := n - t
	d, h, m, s := age/86400, (age%86400)/3600, (age%3600)/60, age%60
	f := strconv.FormatInt
	switch {
	case d != 0 && h != 0:
		return f(d, 10) + "d" + f(h, 10) + "h", true
	case d != 0:
		return f(d, 10) + "d", true
	case h != 0 && m != 0:
		return f(h, 10) + "h" + f(m, 10) + "m", true
	case h != 0:
		return f(h, 10) + "h", true
	case m != 0 && s != 0:
		return f(m, 10) + "m" + f(s, 10) + "s", true
	case m != 0:
		return f(m, 10) + "m", true
	}
	return f(s, 10) + "s", true
}

// PrettyTime is format_pretty_time.
func PrettyTime(t, now time.Time, seconds bool) string {
	if now.Before(t) {
		now = t
	}
	age := now.Sub(t)
	switch {
	case age < 24*time.Hour:
		if seconds {
			return Strftime("%H:%M:%S", t)
		}
		return Strftime("%H:%M", t)
	case (t.Year() == now.Year() && t.Month() == now.Month()) || age < 28*24*time.Hour:
		return Strftime("%a%d", t)
	case (t.Year() == now.Year() && t.Month() < now.Month()) ||
		(t.Year() == now.Year()-1 && t.Month() > now.Month()):
		return Strftime("%d%b", t)
	}
	return Strftime("%h%y", t)
}

// Width is format_width: the cells s takes, #[…] taking none.
func Width(s string) int {
	width := 0
	for i := 0; i < len(s); {
		if s[i] == '#' {
			end, _, lw := leadingHashes(s, i)
			width += lw
			i = end
			if i < len(s) && s[i] == '#' {
				e := Skip(s[i+2:], "]")
				if e < 0 {
					return 0
				}
				i = i + 2 + e + 1
			}
			continue
		}
		w, n, _ := charWidth(s[i:])
		width += w
		i += n
	}
	return width
}

// charWidth is the width and length of the character at the start of s, and whether it is
// kept when trimming: printable ASCII and UTF-8 characters are, other bytes are not and take
// no cells.
func charWidth(s string) (width, n int, keep bool) {
	c := s[0]
	if c < 0x80 {
		if c > 0x1f && c < 0x7f {
			return 1, 1, true
		}
		return 0, 1, false
	}
	r, n := utf8.DecodeRuneInString(s)
	if r == utf8.RuneError && n <= 1 {
		return 0, 1, false
	}
	return max(vt.RuneWidth(r), 0), n, true
}

// cstrWidth is utf8_cstrwidth: the width of s, #[…] included.
func cstrWidth(s string) int {
	width := 0
	for i := 0; i < len(s); {
		w, n, _ := charWidth(s[i:])
		width += w
		i += n
	}
	return width
}

// leadingHashes is format_leading_hashes: the index after a run of #s at i (at the # before
// [ when they start a style), how many there are, and the width they take.
func leadingHashes(s string, i int) (end, n, width int) {
	for i+n < len(s) && s[i+n] == '#' {
		n++
	}
	if n == 0 {
		return i, 0, 0
	}
	if i+n >= len(s) || s[i+n] != '[' {
		return i + n, n, (n + 1) / 2
	}
	width = n / 2
	if n%2 == 0 {
		return i + n, n, width
	}
	return i + n - 1, n, width
}

// TrimLeft is format_trim_left: s cut to limit cells, #[…] kept.
func TrimLeft(s string, limit int) string {
	var out strings.Builder
	width := 0
	for i := 0; i < len(s); {
		if width >= limit {
			break
		}
		if s[i] == '#' {
			end, n, lw := leadingHashes(s, i)
			lw = min(lw, limit-width)
			if lw != 0 {
				if n == 1 {
					out.WriteByte('#')
				} else {
					out.WriteString(strings.Repeat("#", 2*lw))
				}
				width += lw
			}
			i = end
			if i < len(s) && s[i] == '#' {
				e := Skip(s[i+2:], "]")
				if e < 0 {
					break
				}
				out.WriteString(s[i : i+2+e+1])
				i = i + 2 + e + 1
			}
			continue
		}
		w, n, keep := charWidth(s[i:])
		if width+w <= limit && keep {
			out.WriteString(s[i : i+n])
		}
		width += w
		i += n
	}
	return out.String()
}

// TrimRight is format_trim_right: s cut to its last limit cells, #[…] kept.
func TrimRight(s string, limit int) string {
	total := Width(s)
	if total <= limit {
		return s
	}
	skip := total - limit
	var out strings.Builder
	width := 0
	for i := 0; i < len(s); {
		if s[i] == '#' {
			end, n, lw := leadingHashes(s, i)
			cw := lw
			if width <= skip {
				if skip-width >= cw {
					cw = 0
				} else {
					cw -= skip - width
				}
			}
			if cw != 0 {
				if n == 1 {
					out.WriteByte('#')
				} else {
					out.WriteString(strings.Repeat("#", 2*cw))
				}
			}
			width += lw
			i = end
			if i < len(s) && s[i] == '#' {
				e := Skip(s[i+2:], "]")
				if e < 0 {
					break
				}
				out.WriteString(s[i : i+2+e+1])
				i = i + 2 + e + 1
			}
			continue
		}
		w, n, keep := charWidth(s[i:])
		if width >= skip && keep {
			out.WriteString(s[i : i+n])
		}
		width += w
		i += n
	}
	return out.String()
}
