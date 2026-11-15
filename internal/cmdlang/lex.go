package cmdlang

// The lexer follows tmux's cmd-parse.y (yylex and its helpers) closely, so that config files and
// command strings split into the same tokens as under tmux.

import (
	"strconv"
	"strings"
	"unicode/utf8"
)

type tokKind int

const (
	tokEOF tokKind = iota
	tokNewline
	tokSemi
	tokLBrace
	tokRBrace
	tokToken
	tokEquals
	tokFormat
	tokIf
	tokElif
	tokElse
	tokEndif
	tokHidden
)

type token struct {
	kind tokKind
	text string
	line int
}

const eof = -1

type lexer struct {
	src       string
	off       int
	line      int
	escapes   int
	eofSent   bool
	eol       bool
	condition bool
	opts      *Options
	err       *Error
}

func (l *lexer) fail(msg string) {
	if l.err == nil {
		l.err = &Error{Line: l.line, Msg: msg}
	}
}

func (l *lexer) getc1() int {
	if l.off >= len(l.src) {
		l.off = len(l.src) + 1 // so ungetc of EOF is a no-op
		return eof
	}
	c := l.src[l.off]
	l.off++
	return int(c)
}

func (l *lexer) ungetc(c int) {
	if c != eof && l.off > 0 {
		l.off--
	}
	if c == eof && l.off > len(l.src) {
		l.off = len(l.src) + 1
	}
}

// getc returns the next character, joining lines ended with an odd number of backslashes.
func (l *lexer) getc() int {
	if l.escapes != 0 {
		l.escapes--
		return '\\'
	}
	for {
		c := l.getc1()
		if c == '\\' {
			l.escapes++
			continue
		}
		if c == '\n' && l.escapes%2 == 1 {
			l.line++
			l.escapes--
			continue
		}
		if l.escapes != 0 {
			l.ungetc(c)
			l.escapes--
			return '\\'
		}
		return c
	}
}

func isVar(c int, first bool) bool {
	if c == '=' {
		return false
	}
	if first && c >= '0' && c <= '9' {
		return false
	}
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

func (l *lexer) next() token {
	if l.eol {
		l.line++
	}
	l.eol = false
	condition := l.condition
	l.condition = false

	for {
		c := l.getc()
		switch c {
		case eof:
			if l.eofSent {
				return token{kind: tokEOF, line: l.line}
			}
			l.eofSent = true
			return token{kind: tokNewline, line: l.line}
		case ' ', '\t':
			continue
		}
		if c == '\r' {
			if n := l.getc(); n != '\n' {
				l.ungetc(n)
			} else {
				c = '\n'
			}
		}
		switch c {
		case '\n':
			l.eol = true
			return token{kind: tokNewline, line: l.line}
		case ';':
			return token{kind: tokSemi, line: l.line}
		case '{':
			return token{kind: tokLBrace, line: l.line}
		case '}':
			return token{kind: tokRBrace, line: l.line}
		case '#':
			n := l.getc()
			if condition && n == '{' {
				f, ok := l.format()
				if !ok {
					return token{kind: tokEOF, line: l.line}
				}
				return token{kind: tokFormat, text: f, line: l.line}
			}
			for n != '\n' && n != eof {
				n = l.getc()
			}
			if n == '\n' {
				l.line++
				return token{kind: tokNewline, line: l.line - 1}
			}
			continue
		case '%':
			w := l.word(c)
			if strings.Trim(w, "%0123456789") == "" {
				return token{kind: tokToken, text: w, line: l.line}
			}
			l.condition = true
			switch w {
			case "%hidden":
				return token{kind: tokHidden, line: l.line}
			case "%if":
				return token{kind: tokIf, line: l.line}
			case "%else":
				return token{kind: tokElse, line: l.line}
			case "%elif":
				return token{kind: tokElif, line: l.line}
			case "%endif":
				return token{kind: tokEndif, line: l.line}
			}
			l.fail("unknown directive: " + w)
			return token{kind: tokEOF, line: l.line}
		}
		line := l.line
		t, ok := l.token(c)
		if !ok {
			l.fail("syntax error")
			return token{kind: tokEOF, line: line}
		}
		if i := strings.IndexByte(t, '='); i > 0 && isVar(int(t[0]), true) {
			j := 1
			for j < i && isVar(int(t[j]), false) {
				j++
			}
			if j == i {
				return token{kind: tokEquals, text: t, line: line}
			}
		}
		return token{kind: tokToken, text: t, line: line}
	}
}

// format reads a #{…} format after %if or %elif; "#{" has been read.
func (l *lexer) format() (string, bool) {
	var b strings.Builder
	b.WriteString("#{")
	brackets := 1
	for {
		c := l.getc()
		if c == eof || c == '\n' {
			l.fail("unterminated format")
			return "", false
		}
		switch c {
		case '#':
			c = l.getc()
			if c == eof || c == '\n' {
				l.fail("unterminated format")
				return "", false
			}
			if c == '{' {
				brackets++
			}
			b.WriteByte('#')
		case '}':
			if brackets != 0 {
				brackets--
				if brackets == 0 {
					b.WriteByte('}')
					return b.String(), true
				}
			}
		}
		b.WriteByte(byte(c))
	}
}

func (l *lexer) word(c int) string {
	var b strings.Builder
	for {
		b.WriteByte(byte(c))
		c = l.getc()
		if c == eof || c == ' ' || c == '\t' || c == '\n' {
			break
		}
	}
	l.ungetc(c)
	return b.String()
}

const (
	stStart = iota
	stNone
	stDouble
	stSingle
)

// token reads one word, handling quotes, escapes, ~ and $VAR as tmux does.
func (l *lexer) token(c int) (string, bool) {
	var b strings.Builder
	state, last := stNone, stStart
	for c != eof {
		if state == stNone && c == '\r' {
			if n := l.getc(); n != '\n' {
				l.ungetc(n)
			} else {
				c = '\n'
			}
		}
		if c == '\n' {
			if state == stNone {
				break
			}
			l.line++
		}
		if state == stNone && (c == ' ' || c == '\t' || c == ';' || c == '}') {
			break
		}
		if c == '\n' && state != stNone {
			// Spaces and comments inside quotes after a newline are removed, the newline kept.
			b.WriteByte('\n')
			for c = l.getc(); c == ' ' || c == '\t'; c = l.getc() {
			}
			if c != '#' {
				continue
			}
			c = l.getc()
			if c != eof && strings.IndexByte(",#{}:", byte(c)) >= 0 {
				l.ungetc(c)
				c = '#'
			} else {
				for c != '\n' && c != eof {
					c = l.getc()
				}
				continue
			}
		}
		switch {
		case c == '\\' && state != stSingle:
			if !l.escape(&b) {
				return "", false
			}
			last = state
			c = l.getc()
			continue
		case c == '~' && last != state && state != stSingle:
			if !l.tilde(&b) {
				return "", false
			}
			last = state
			c = l.getc()
			continue
		case c == '$' && state != stSingle:
			if !l.variable(&b) {
				return "", false
			}
			last = state
			c = l.getc()
			continue
		case c == '\'' && state == stNone:
			state = stSingle
			c = l.getc()
			continue
		case c == '\'' && state == stSingle:
			state = stNone
			c = l.getc()
			continue
		case c == '"' && state == stNone:
			state = stDouble
			c = l.getc()
			continue
		case c == '"' && state == stDouble:
			state = stNone
			c = l.getc()
			continue
		}
		b.WriteByte(byte(c))
		last = state
		c = l.getc()
	}
	// The end of the input ends a token even inside quotes, as in tmux: an unclosed quote runs
	// to the end of the file.
	l.ungetc(c)
	return b.String(), true
}

func (l *lexer) escape(b *strings.Builder) bool {
	c := l.getc()
	if c >= '4' && c <= '7' {
		l.fail("invalid octal escape")
		return false
	}
	if c >= '0' && c <= '3' {
		o2 := l.getc()
		if o2 >= '0' && o2 <= '7' {
			o3 := l.getc()
			if o3 >= '0' && o3 <= '7' {
				b.WriteByte(byte(64*(c-'0') + 8*(o2-'0') + (o3 - '0')))
				return true
			}
		}
		l.fail("invalid octal escape")
		return false
	}
	size := 0
	switch c {
	case eof:
		return false
	case 'a':
		c = '\a'
	case 'b':
		c = '\b'
	case 'e':
		c = 0o33
	case 'f':
		c = '\f'
	case 's':
		c = ' '
	case 'v':
		c = '\v'
	case 'r':
		c = '\r'
	case 'n':
		c = '\n'
	case 't':
		c = '\t'
	case 'u':
		size = 4
	case 'U':
		size = 8
	}
	if size == 0 {
		b.WriteByte(byte(c))
		return true
	}
	kind := c
	hex := make([]byte, 0, size)
	for range size {
		h := l.getc()
		if h == eof || h == '\n' {
			return false
		}
		if !strings.ContainsRune("0123456789abcdefABCDEF", rune(h)) {
			l.fail("invalid \\" + string(rune(kind)) + " argument")
			return false
		}
		hex = append(hex, byte(h))
	}
	v, err := strconv.ParseUint(string(hex), 16, 32)
	if err != nil || !utf8.ValidRune(rune(v)) {
		l.fail("invalid \\" + string(rune(kind)) + " argument")
		return false
	}
	b.WriteRune(rune(v))
	return true
}

func (l *lexer) variable(b *strings.Builder) bool {
	c := l.getc()
	if c == eof {
		return false
	}
	brackets := false
	var name strings.Builder
	if c == '{' {
		brackets = true
	} else {
		if !isVar(c, true) {
			b.WriteByte('$')
			l.ungetc(c)
			return true
		}
		name.WriteByte(byte(c))
	}
	for {
		c = l.getc()
		if brackets && c == '}' {
			break
		}
		if c == eof || !isVar(c, false) {
			if !brackets {
				l.ungetc(c)
				break
			}
			l.fail("invalid environment variable")
			return false
		}
		name.WriteByte(byte(c))
	}
	if l.opts.Env == nil {
		// Analysis: keep the reference as written.
		if brackets {
			b.WriteString("${" + name.String() + "}")
		} else {
			b.WriteString("$" + name.String())
		}
		return true
	}
	if v, ok := l.opts.Env(name.String()); ok {
		b.WriteString(v)
	}
	return true
}

func (l *lexer) tilde(b *strings.Builder) bool {
	var name strings.Builder
	for {
		c := l.getc()
		if c == eof || strings.IndexByte("/ \t\n\"'", byte(c)) >= 0 {
			l.ungetc(c)
			break
		}
		name.WriteByte(byte(c))
	}
	if l.opts.Home == nil {
		b.WriteString("~" + name.String())
		return true
	}
	home, ok := l.opts.Home(name.String())
	if !ok {
		return false
	}
	b.WriteString(home)
	return true
}
