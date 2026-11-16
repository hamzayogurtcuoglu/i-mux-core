package format

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// Fnmatch is fnmatch(3) with no flags but FNM_CASEFOLD: * and ? match any character, / and a
// leading . included; [...] is a bracket expression; \ escapes the next character.
func Fnmatch(pattern, s string, fold bool) bool {
	if fold {
		pattern, s = strings.ToLower(pattern), strings.ToLower(s)
	}
	return fnmatch(pattern, s, 0)
}

func fnmatch(p, s string, depth int) bool {
	if depth > 1000 {
		return false
	}
	for len(p) > 0 {
		switch p[0] {
		case '*':
			for len(p) > 0 && p[0] == '*' {
				p = p[1:]
			}
			if p == "" {
				return true
			}
			for i := 0; i <= len(s); {
				if fnmatch(p, s[i:], depth+1) {
					return true
				}
				if i == len(s) {
					break
				}
				_, n := utf8.DecodeRuneInString(s[i:])
				i += n
			}
			return false
		case '?':
			if s == "" {
				return false
			}
			_, n := utf8.DecodeRuneInString(s)
			p, s = p[1:], s[n:]
		case '[':
			if s == "" {
				return false
			}
			r, n := utf8.DecodeRuneInString(s)
			ok, rest, valid := bracket(p, r)
			if !valid {
				// An unterminated [ matches itself.
				if s[0] != '[' {
					return false
				}
				p, s = p[1:], s[1:]
				continue
			}
			if !ok {
				return false
			}
			p, s = rest, s[n:]
		case '\\':
			if len(p) > 1 {
				p = p[1:]
			}
			fallthrough
		default:
			if s == "" || s[0] != p[0] {
				return false
			}
			p, s = p[1:], s[1:]
		}
	}
	return s == ""
}

// bracket matches r against the bracket expression at the start of p; rest is p after it.
func bracket(p string, r rune) (matched bool, rest string, valid bool) {
	i := 1
	negate := false
	if i < len(p) && (p[i] == '!' || p[i] == '^') {
		negate = true
		i++
	}
	first := true
	for i < len(p) {
		if p[i] == ']' && !first {
			return matched != negate, p[i+1:], true
		}
		first = false
		if strings.HasPrefix(p[i:], "[:") {
			if end := strings.Index(p[i+2:], ":]"); end >= 0 {
				if classMatch(p[i+2:i+2+end], r) {
					matched = true
				}
				i += 2 + end + 2
				continue
			}
		}
		lo, n := utf8.DecodeRuneInString(p[i:])
		if lo == '\\' && i+1 < len(p) {
			i++
			lo, n = utf8.DecodeRuneInString(p[i:])
		}
		i += n
		hi := lo
		if i+1 < len(p) && p[i] == '-' && p[i+1] != ']' {
			i++
			hi, n = utf8.DecodeRuneInString(p[i:])
			if hi == '\\' && i+1 < len(p) {
				i++
				hi, n = utf8.DecodeRuneInString(p[i:])
			}
			i += n
		}
		if lo <= r && r <= hi {
			matched = true
		}
	}
	return false, p, false
}

func classMatch(class string, r rune) bool {
	switch class {
	case "alnum":
		return r < 0x80 && isAlnum(byte(r))
	case "alpha":
		return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
	case "digit":
		return r >= '0' && r <= '9'
	case "lower":
		return r >= 'a' && r <= 'z'
	case "upper":
		return r >= 'A' && r <= 'Z'
	case "space":
		return r == ' ' || (r >= '\t' && r <= '\r')
	case "blank":
		return r == ' ' || r == '\t'
	case "punct":
		return r < 0x80 && isPunct(byte(r))
	case "xdigit":
		return (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')
	case "cntrl":
		return r < 0x20 || r == 0x7f
	case "print":
		return r >= 0x20 && r < 0x7f
	case "graph":
		return r > 0x20 && r < 0x7f
	}
	return false
}

// compileRegex compiles a POSIX extended regular expression (regcomp with REG_EXTENDED),
// matching leftmost-longest as POSIX does.
func compileRegex(pattern string, icase bool) (*regexp.Regexp, error) {
	if icase {
		pattern = "(?i)" + pattern
	}
	re, err := regexp.CompilePOSIX(pattern)
	if err != nil {
		if re, err = regexp.Compile(pattern); err != nil {
			return nil, err
		}
		re.Longest()
	}
	return re, nil
}

// Regsub is tmux's regsub: every match of pattern in text replaced by with, where \0 to \9
// are the match and its groups. ok is false when the pattern does not compile.
func Regsub(pattern, with, text string, icase bool) (string, bool) {
	if text == "" {
		return "", true
	}
	if pattern == "" {
		return text, true
	}
	re, err := compileRegex(pattern, icase)
	if err != nil {
		return "", false
	}
	var b strings.Builder
	start, last, end := 0, 0, len(text)
	empty := false
	for start <= end {
		m := re.FindStringSubmatchIndex(text[start:])
		if m == nil {
			b.WriteString(text[last:end])
			break
		}
		b.WriteString(text[last : m[0]+start])
		if pattern[0] == '^' {
			regsubExpand(&b, with, text[start:], m)
			last = start + m[1]
			b.WriteString(text[last:end])
			break
		}
		if empty || start+m[0] != last || m[0] != m[1] {
			regsubExpand(&b, with, text[start:], m)
			last = start + m[1]
			start += m[1]
			empty = false
		} else {
			last = start + m[1]
			start += m[1] + 1
			empty = true
		}
	}
	return b.String(), true
}

func regsubExpand(b *strings.Builder, with, text string, m []int) {
	for i := 0; i < len(with); i++ {
		if with[i] == '\\' && i+1 < len(with) {
			i++
			if c := with[i]; c >= '0' && c <= '9' {
				n := int(c - '0')
				if 2*n+1 < len(m) && m[2*n] >= 0 && m[2*n] != m[2*n+1] {
					b.WriteString(text[m[2*n]:m[2*n+1]])
					continue
				}
			}
		}
		b.WriteByte(with[i])
	}
}
