package termout

import (
	"fmt"
	"strconv"
	"strings"
)

// Tparm expands a parameterised capability (terminfo(5)'s % language), as ncurses' tparm
// does, with parameters that are ints or strings.
func Tparm(s string, params ...any) string {
	if !strings.Contains(s, "%") {
		return s
	}
	var p [9]any
	for i := range p {
		p[i] = 0
	}
	copy(p[:], params)
	var (
		out        strings.Builder
		stack      []any
		dynamic    [26]any
		staticVars [26]any // ncurses keeps these between calls; no capability tmux uses does
	)
	push := func(v any) { stack = append(stack, v) }
	pop := func() any {
		if len(stack) == 0 {
			return 0
		}
		v := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		return v
	}
	popInt := func() int {
		switch v := pop().(type) {
		case int:
			return v
		case string:
			n, _ := strconv.Atoi(v)
			return n
		}
		return 0
	}
	popString := func() string {
		switch v := pop().(type) {
		case string:
			return v
		case int:
			return strconv.Itoa(v)
		}
		return ""
	}
	boolInt := func(b bool) int {
		if b {
			return 1
		}
		return 0
	}

	for i := 0; i < len(s); i++ {
		c := s[i]
		if c != '%' {
			out.WriteByte(c)
			continue
		}
		i++
		if i >= len(s) {
			break
		}
		c = s[i]
		switch c {
		case '%':
			out.WriteByte('%')
		case 'c':
			out.WriteByte(byte(popInt()))
		case 's':
			out.WriteString(popString())
		case 'p':
			i++
			if i < len(s) && s[i] >= '1' && s[i] <= '9' {
				push(p[s[i]-'1'])
			}
		case 'P':
			i++
			if i < len(s) {
				if s[i] >= 'a' && s[i] <= 'z' {
					dynamic[s[i]-'a'] = pop()
				} else if s[i] >= 'A' && s[i] <= 'Z' {
					staticVars[s[i]-'A'] = pop()
				}
			}
		case 'g':
			i++
			if i < len(s) {
				var v any = 0
				if s[i] >= 'a' && s[i] <= 'z' && dynamic[s[i]-'a'] != nil {
					v = dynamic[s[i]-'a']
				} else if s[i] >= 'A' && s[i] <= 'Z' && staticVars[s[i]-'A'] != nil {
					v = staticVars[s[i]-'A']
				}
				push(v)
			}
		case '\'':
			if i+2 < len(s) {
				push(int(s[i+1]))
				i += 2 // the character and the closing quote
			}
		case '{':
			j := strings.IndexByte(s[i:], '}')
			if j < 0 {
				i = len(s)
				break
			}
			n, _ := strconv.Atoi(s[i+1 : i+j])
			push(n)
			i += j
		case 'l':
			push(len(popString()))
		case '+', '-', '*', '/', 'm', '&', '|', '^', '=', '>', '<', 'A', 'O':
			b, a := popInt(), popInt()
			switch c {
			case '+':
				push(a + b)
			case '-':
				push(a - b)
			case '*':
				push(a * b)
			case '/':
				if b != 0 {
					push(a / b)
				} else {
					push(0)
				}
			case 'm':
				if b != 0 {
					push(a % b)
				} else {
					push(0)
				}
			case '&':
				push(a & b)
			case '|':
				push(a | b)
			case '^':
				push(a ^ b)
			case '=':
				push(boolInt(a == b))
			case '>':
				push(boolInt(a > b))
			case '<':
				push(boolInt(a < b))
			case 'A':
				push(boolInt(a != 0 && b != 0))
			case 'O':
				push(boolInt(a != 0 || b != 0))
			}
		case '!':
			push(boolInt(popInt() == 0))
		case '~':
			push(^popInt())
		case 'i':
			if v, ok := p[0].(int); ok {
				p[0] = v + 1
			}
			if v, ok := p[1].(int); ok {
				p[1] = v + 1
			}
		case '?', ';':
		case 't':
			if popInt() == 0 {
				i = skip(s, i, false)
			}
		case 'e':
			i = skip(s, i, true)
		default:
			// A printf-style conversion: %[[:]flags][width[.precision]][doxXs].
			j := i
			if s[j] == ':' {
				j++
			}
			for j < len(s) && strings.IndexByte("-+# 0123456789.", s[j]) >= 0 {
				j++
			}
			if j >= len(s) || strings.IndexByte("doxXs", s[j]) < 0 {
				break
			}
			spec := strings.TrimPrefix(s[i:j], ":")
			if s[j] == 's' {
				fmt.Fprintf(&out, "%"+spec+"s", popString())
			} else {
				fmt.Fprintf(&out, "%"+spec+string(s[j]), popInt())
			}
			i = j
		}
	}
	return out.String()
}

// skip moves past the then-part (else false: to after the matching %e or %;) or the else-part
// (else true: to after the matching %;) of a %? conditional, from s[i] (the t or e).
func skip(s string, i int, toEnd bool) int {
	level := 0
	for i++; i < len(s); i++ {
		if s[i] != '%' || i+1 >= len(s) {
			continue
		}
		i++
		switch s[i] {
		case '?':
			level++
		case ';':
			if level == 0 {
				return i
			}
			level--
		case 'e':
			if level == 0 && !toEnd {
				return i
			}
		}
	}
	return i
}
