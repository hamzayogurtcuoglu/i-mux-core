package commands

import (
	"errors"
	"fmt"
	"strings"
)

// Command lookup and argument parsing, as tmux 3.8's cmd.c (cmd_find, cmd_parse) and
// arguments.c (args_parse) do them, with tmux's error messages.

// Spec is one command of tmux's table: its name, alias, argument template (a flag letter
// followed by ':' takes a value, by '::' an optional one) and the bounds on its arguments
// (-1: no bound).
type Spec struct {
	Name, Alias  string
	Template     string
	Lower, Upper int
	// StartServer: the command starts a server when there is none (tmux's CMD_STARTSERVER).
	StartServer bool
	Usage       string // what follows the name in "usage: <name> <usage>"
}

// Find looks a command up by alias, exact name or unique prefix of its name, as tmux does.
func Find(name string) (*Spec, error) {
	var found *Spec
	ambiguous := false
	for i := range tmuxTable {
		e := &tmuxTable[i]
		if e.Alias != "" && e.Alias == name {
			return e, nil
		}
		if !strings.HasPrefix(e.Name, name) {
			continue
		}
		if found != nil {
			ambiguous = true
		}
		found = e
		if e.Name == name {
			break
		}
	}
	if ambiguous {
		var names []string
		for _, e := range tmuxTable {
			if strings.HasPrefix(e.Name, name) {
				names = append(names, e.Name)
			}
		}
		return nil, fmt.Errorf("ambiguous command: %s, could be: %s", name, strings.Join(names, ", "))
	}
	if found == nil {
		return nil, fmt.Errorf("unknown command: %s", name)
	}
	return found, nil
}

// Args are a command's parsed flags and arguments.
type Args struct {
	flags  map[byte][]string // the values of each flag given, in order
	counts map[byte]int      // how many times each flag was given
	Values []string          // the arguments after the flags
}

// Has reports whether flag f was given.
func (a *Args) Has(f byte) bool { return a.counts[f] > 0 }

// Count returns how many times flag f was given.
func (a *Args) Count(f byte) int { return a.counts[f] }

// Get returns flag f's value; tmux takes the last one when it is given more than once.
func (a *Args) Get(f byte) (string, bool) {
	v := a.flags[f]
	if len(v) == 0 {
		return "", false
	}
	return v[len(v)-1], true
}

// All returns every value given for flag f, in order.
func (a *Args) All(f byte) []string { return a.flags[f] }

// errUsage is tmux's -? : the command's usage is wanted.
var errUsage = errors.New("usage")

// ParseArgs parses words (words[0] is the command's name) by template and bounds, as tmux's
// args_parse does: flags until the first word that is not one, or "--".
func ParseArgs(template string, lower, upper int, words []string) (*Args, error) {
	a := &Args{flags: map[byte][]string{}, counts: map[byte]int{}}
	i := 1
	for i < len(words) {
		w := words[i]
		if len(w) < 2 || w[0] != '-' {
			break
		}
		i++
		if w == "--" {
			break
		}
		s := w[1:]
		for len(s) > 0 {
			f := s[0]
			s = s[1:]
			if f == '?' {
				return nil, errUsage
			}
			if !isAlnum(f) {
				return nil, fmt.Errorf("invalid flag -%c", f)
			}
			at := strings.IndexByte(template, f)
			if at < 0 {
				return nil, fmt.Errorf("unknown flag -%c", f)
			}
			if at+1 >= len(template) || template[at+1] != ':' {
				a.counts[f]++
				continue
			}
			optional := at+2 < len(template) && template[at+2] == ':'
			switch {
			case s != "":
				a.set(f, s, true)
			case i == len(words):
				if !optional {
					return nil, fmt.Errorf("-%c expects an argument", f)
				}
				a.set(f, "", false)
			case optional && looksLikeFlag(words[i]):
				a.set(f, "", false)
			default:
				a.set(f, words[i], true)
				i++
			}
			s = ""
		}
	}
	a.Values = append([]string(nil), words[i:]...)
	if lower != -1 && len(a.Values) < lower {
		return nil, fmt.Errorf("too few arguments (need at least %d)", lower)
	}
	if upper != -1 && len(a.Values) > upper {
		return nil, fmt.Errorf("too many arguments (need at most %d)", upper)
	}
	return a, nil
}

func (a *Args) set(f byte, v string, hasValue bool) {
	a.counts[f]++
	if hasValue {
		a.flags[f] = append(a.flags[f], v)
	}
}

func isAlnum(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

// looksLikeFlag is tmux's test for an optional value: a word "--…" or "-<letter>…" is the next
// flag, not the value.
func looksLikeFlag(w string) bool {
	if len(w) < 2 || w[0] != '-' {
		return false
	}
	c := w[1]
	return c == '-' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

// Command is one parsed command.
type Command struct {
	Spec  *Spec
	Args  *Args
	Words []string
}

// Parse looks up and parses one command's words, with tmux's messages.
func Parse(words []string) (*Command, error) {
	if len(words) == 0 {
		return nil, errors.New("no command")
	}
	spec, err := Find(words[0])
	if err != nil {
		return nil, err
	}
	args, err := ParseArgs(spec.Template, spec.Lower, spec.Upper, words)
	if errors.Is(err, errUsage) {
		return nil, fmt.Errorf("usage: %s %s", spec.Name, spec.Usage)
	}
	if err != nil {
		return nil, fmt.Errorf("command %s: %w", spec.Name, err)
	}
	return &Command{Spec: spec, Args: args, Words: words}, nil
}

// ParseList parses a command list; the first error fails the whole list, as in tmux.
func ParseList(cmds [][]string) ([]*Command, error) {
	out := make([]*Command, 0, len(cmds))
	for _, words := range cmds {
		c, err := Parse(words)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}

// StartsServer reports whether a command list starts a server when there is none: when it
// parses and any of its commands does, as tmux's client decides.
func StartsServer(cmds [][]string) bool {
	list, err := ParseList(cmds)
	if err != nil {
		return false
	}
	for _, c := range list {
		if c.Spec.StartServer {
			return true
		}
	}
	return false
}
