package format

// The syntax helpers follow tmux's format.c (format_skip, format_build_modifiers and
// format_choose), so formats split the same way as under tmux.

import "strings"

// Skip returns the index in s of the first byte in end that is outside any #{…}, skipping the
// escapes #, ## #{ #} and #:, or -1 when there is none.
func Skip(s, end string) int {
	brackets := 0
	for i := 0; i < len(s); i++ {
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

// Modifier is one modifier of a #{…}: its name ("=", "p", "==", "e", …) and its arguments, not
// yet expanded.
type Modifier struct {
	Name string
	Args []string
}

func isEnd(s string, i int) bool { return i < len(s) && (s[i] == ';' || s[i] == ':') }

func isPunct(c byte) bool {
	isDigit := c >= '0' && c <= '9'
	isLetter := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
	return c > ' ' && c < 0x7f && !isDigit && !isLetter
}

// ParseModifiers splits the key of a #{…} (the text between the braces) into its modifiers and
// the rest. It returns no modifiers, and the whole key, when the key has none or they do not
// end with ':'.
func ParseModifiers(key string) ([]Modifier, string) {
	var list []Modifier
	i := 0
	for i < len(key) && key[i] != ':' {
		if key[i] == ';' {
			i++
		}
		if i >= len(key) {
			break
		}
		c := key[i]
		// Single character modifiers with no arguments.
		if strings.IndexByte("labcdnwETSWPL!<>", c) >= 0 && isEnd(key, i+1) {
			list = append(list, Modifier{Name: string(c)})
			i++
			continue
		}
		// Double character modifiers with no arguments.
		if i+2 <= len(key) && isEnd(key, i+2) {
			switch key[i : i+2] {
			case "||", "&&", "!!", "!=", "==", "<=", ">=":
				list = append(list, Modifier{Name: key[i : i+2]})
				i += 2
				continue
			}
		}
		// Single character modifiers with arguments.
		if strings.IndexByte("mCLNPSst=pReqW", c) < 0 {
			break
		}
		if isEnd(key, i+1) {
			list = append(list, Modifier{Name: string(c)})
			i++
			continue
		}
		if i+1 >= len(key) {
			break
		}
		// A single argument with no wrapper character.
		if !isPunct(key[i+1]) || key[i+1] == '-' {
			e := Skip(key[i+1:], ":;")
			if e < 0 {
				break
			}
			list = append(list, Modifier{Name: string(c), Args: []string{key[i+1 : i+1+e]}})
			i = i + 1 + e
			continue
		}
		// Several arguments with a wrapper character.
		wrap := key[i+1]
		last := string([]byte{wrap, ';', ':'})
		m := Modifier{Name: string(c)}
		i++
		for {
			if key[i] == wrap && isEnd(key, i+1) {
				i++
				break
			}
			e := Skip(key[i+1:], last)
			if e < 0 {
				break
			}
			m.Args = append(m.Args, key[i+1:i+1+e])
			i = i + 1 + e
			if i >= len(key) || isEnd(key, i) {
				break
			}
		}
		list = append(list, m)
	}
	if i >= len(key) || key[i] != ':' {
		return nil, key
	}
	return list, key[i+1:]
}

// Choose splits s at its first top-level comma into left and right, as tmux's format_choose
// does. ok is false when there is no comma.
func Choose(s string) (left, right string, ok bool) {
	i := Skip(s, ",")
	if i < 0 {
		return "", "", false
	}
	return s[:i], s[i+1:], true
}

// Aliases are the one-letter forms #D #F #H #I #P #S #T #W #h, and what they stand for.
var Aliases = map[byte]string{
	'D': "pane_id",
	'F': "window_flags",
	'H': "host",
	'I': "window_index",
	'P': "pane_index",
	'S': "session_name",
	'T': "pane_title",
	'W': "window_name",
	'h': "host_short",
}

// Node kinds that Walk reports.
const (
	NodeVariable = "variable" // a name looked up: Text is the name
	NodeModifier = "modifier" // a modifier: Text is its name, Args its arguments
	NodeAlias    = "alias"    // #S and the like: Text is "#S"
	NodeStyle    = "style"    // #[…]: Text is the inside
	NodeJob      = "job"      // #(…): Text is the command
)

// Walk reports every variable, modifier, alias, style and job in the format s, nested ones
// included, without expanding anything.
func Walk(s string, visit func(kind, text string, args []string)) {
	walk(s, visit, 0)
}

const maxDepth = 100

func walk(s string, visit func(kind, text string, args []string), depth int) {
	if depth > maxDepth {
		return
	}
	styleEnd := -1
	for i := 0; i < len(s); i++ {
		if s[i] != '#' || i+1 >= len(s) {
			continue
		}
		c := s[i+1]
		switch c {
		case '(':
			n := 1
			j := i + 2
			for ; j < len(s); j++ {
				if s[j] == '(' {
					n++
				}
				if s[j] == ')' {
					n--
					if n == 0 {
						break
					}
				}
			}
			if j >= len(s) {
				return
			}
			visit(NodeJob, s[i+2:j], nil)
			walk(s[i+2:j], visit, depth+1)
			i = j
		case '{':
			e := Skip(s[i:], "}")
			if e < 0 {
				return
			}
			walkKey(s[i+2:i+e], visit, depth+1)
			i += e
		case '[', '#':
			j := i + 1
			for j < len(s) && s[j] == '#' {
				j++
			}
			if j < len(s) && s[j] == '[' {
				e := Skip(s[i:], "]")
				if e < 0 {
					e = len(s) - i
				}
				visit(NodeStyle, s[j+1:i+e], nil)
				walk(s[j+1:i+e], visit, depth+1)
				styleEnd = i + e
				i += e
				continue
			}
			i++
		case '}', ',':
			i++
		default:
			if _, ok := Aliases[c]; ok && i > styleEnd {
				visit(NodeAlias, "#"+string(c), nil)
			}
			i++
		}
	}
}

func walkKey(key string, visit func(kind, text string, args []string), depth int) {
	mods, rest := ParseModifiers(key)
	has := map[string]bool{}
	for _, m := range mods {
		visit(NodeModifier, m.Name, m.Args)
		has[m.Name] = true
		for _, a := range m.Args {
			walk(a, visit, depth)
		}
	}
	both := func() {
		if l, r, ok := Choose(rest); ok {
			walk(l, visit, depth)
			walk(r, visit, depth)
		} else {
			walk(rest, visit, depth)
		}
	}
	switch {
	case has["l"]:
	case has["S"] || has["W"] || has["P"] || has["L"]:
		both()
	case has["!"] || has["!!"] || has["C"] || has["N"] || has["a"] || has["c"]:
		walk(rest, visit, depth)
	case has["||"] || has["&&"]:
		for {
			l, r, ok := Choose(rest)
			if !ok {
				walk(rest, visit, depth)
				break
			}
			walk(l, visit, depth)
			rest = r
		}
	case has["R"], has["=="], has["!="], has["<"], has[">"], has["<="], has[">="], has["m"], has["e"]:
		both()
	case strings.HasPrefix(rest, "?"):
		parts := []string{}
		cp := rest[1:]
		for {
			e := Skip(cp, ",")
			if e < 0 {
				parts = append(parts, cp)
				break
			}
			parts = append(parts, cp[:e])
			cp = cp[e+1:]
		}
		for k, p := range parts {
			if k%2 == 0 && k < len(parts)-1 && !strings.Contains(p, "#{") {
				visit(NodeVariable, p, nil) // a condition that is a name
				continue
			}
			walk(p, visit, depth)
		}
	case strings.Contains(rest, "#{"):
		walk(rest, visit, depth)
	case rest != "":
		visit(NodeVariable, rest, nil)
	}
}
