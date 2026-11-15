package contract

import "strings"

// commandSpec is one tmux command's name, alias and getopt flag spec.
type commandSpec struct {
	Name, Alias, Flags string
}

// lookupCommand finds a command by name, alias or unique prefix, as tmux does.
func lookupCommand(name string) (commandSpec, bool) {
	var found []commandSpec
	for _, c := range tmuxCommands {
		if c.Name == name || (c.Alias != "" && c.Alias == name) {
			return c, true
		}
		if strings.HasPrefix(c.Name, name) {
			found = append(found, c)
		}
	}
	if len(found) == 1 {
		return found[0], true
	}
	return commandSpec{}, false
}

// takesValue reports whether flag c takes a value in spec ("c:"), and whether the value is
// optional ("c::").
func takesValue(spec string, c byte) (has, value, optional bool) {
	i := strings.IndexByte(spec, c)
	if i < 0 || c == ':' {
		return false, false, false
	}
	if i+1 < len(spec) && spec[i+1] == ':' {
		return true, true, i+2 < len(spec) && spec[i+2] == ':'
	}
	return true, false, false
}

// Flag is one parsed flag; Value is set for flags that take one.
type Flag struct {
	Name  byte
	Value string
}

// parseFlags splits words (without the command name) into flags and arguments with spec, the
// way tmux's args_parse does: flags stop at the first word that is not one, or at "--".
func parseFlags(spec string, words []string) (flags []Flag, args []string, bad []string) {
	i := 0
	for ; i < len(words); i++ {
		w := words[i]
		if w == "--" {
			i++
			break
		}
		if len(w) < 2 || w[0] != '-' {
			break
		}
		for j := 1; j < len(w); j++ {
			c := w[j]
			has, value, optional := takesValue(spec, c)
			if !has {
				bad = append(bad, "-"+string(c))
				continue
			}
			if !value {
				flags = append(flags, Flag{Name: c})
				continue
			}
			v := w[j+1:]
			if v == "" && i+1 < len(words) && (!optional || !looksLikeFlag(words[i+1])) {
				i++
				v = words[i]
			}
			flags = append(flags, Flag{Name: c, Value: v})
			break
		}
	}
	return flags, words[i:], bad
}

// looksLikeFlag reports whether a word would be read as flags rather than as an optional
// flag's value: "-" followed by "-" or a letter, as in tmux's args_parse_flag_argument.
func looksLikeFlag(w string) bool {
	return len(w) > 1 && w[0] == '-' && (w[1] == '-' || (w[1] >= 'a' && w[1] <= 'z') || (w[1] >= 'A' && w[1] <= 'Z'))
}
