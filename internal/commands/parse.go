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
	Usage       string      // what follows the name in "usage: <name> <usage>"
	Flags       CmdFlags    // tmux's CMD_* flags
	Source      *TargetSpec // the -s the command resolves before it runs, or nil
	Target      *TargetSpec // the -t, or nil
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

// Value is an argument: a string, or a { … } block of commands.
type Value struct {
	String   string
	Commands *List // a block, when IsBlock
	IsBlock  bool
}

// Strings makes values of strings.
func Strings(words []string) []Value {
	out := make([]Value, len(words))
	for i, w := range words {
		out[i] = Value{String: w}
	}
	return out
}

// text is a value as a string: a block is printed.
func (v Value) text() string {
	if v.IsBlock {
		return v.Commands.Print()
	}
	return v.String
}

type flagEntry struct {
	count    int
	values   []Value
	optional bool // given without its optional value (tmux's ARGS_ENTRY_OPTIONAL_VALUE)
}

// Args are a command's parsed flags and arguments.
type Args struct {
	flags  map[byte]*flagEntry
	Values []Value // the arguments after the flags
}

// Has reports whether flag f was given.
func (a *Args) Has(f byte) bool { return a.flags[f] != nil }

// Count returns how many times flag f was given.
func (a *Args) Count(f byte) int {
	if e := a.flags[f]; e != nil {
		return e.count
	}
	return 0
}

// Get returns flag f's value; tmux takes the last one when it is given more than once.
func (a *Args) Get(f byte) (string, bool) {
	e := a.flags[f]
	if e == nil || len(e.values) == 0 {
		return "", false
	}
	return e.values[len(e.values)-1].text(), true
}

// All returns every value given for flag f, in order.
func (a *Args) All(f byte) []string {
	var out []string
	if e := a.flags[f]; e != nil {
		for _, v := range e.values {
			out = append(out, v.text())
		}
	}
	return out
}

// Strings returns the arguments after the flags as strings.
func (a *Args) Strings() []string {
	out := make([]string, len(a.Values))
	for i, v := range a.Values {
		out[i] = v.text()
	}
	return out
}

// errUsage is tmux's -? : the command's usage is wanted.
var errUsage = errors.New("usage")

// ArgType says what an argument may be, as tmux's per-command args_parse callbacks do.
type ArgType int

// Argument types.
const (
	ArgString           ArgType = iota // a string only
	ArgCommandsOrString                // a { … } block or a string
)

// ParseArgs parses words (words[0] is the command's name) by template and bounds, as tmux's
// args_parse does: flags until the first word that is not one, or "--". argType says what
// each argument after the flags may be (nil: strings only).
func ParseArgs(template string, lower, upper int, words []Value, argType func(a *Args, idx int) ArgType) (*Args, error) {
	a := &Args{flags: map[byte]*flagEntry{}}
	i := 1
	for i < len(words) {
		w := words[i]
		if w.IsBlock || len(w.String) < 2 || w.String[0] != '-' {
			break
		}
		i++
		if w.String == "--" {
			break
		}
		s := w.String[1:]
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
				a.set(f, nil, false)
				continue
			}
			optional := at+2 < len(template) && template[at+2] == ':'
			switch {
			case s != "":
				a.set(f, &Value{String: s}, false)
			case i == len(words):
				if !optional {
					return nil, fmt.Errorf("-%c expects an argument", f)
				}
				a.set(f, nil, true)
			case words[i].IsBlock:
				return nil, fmt.Errorf("-%c argument must be a string", f)
			case optional && looksLikeFlag(words[i].String):
				a.set(f, nil, true)
			default:
				v := words[i]
				a.set(f, &v, false)
				i++
			}
			s = ""
		}
	}
	for ; i < len(words); i++ {
		t := ArgString
		if argType != nil {
			t = argType(a, len(a.Values))
		}
		if t == ArgString && words[i].IsBlock {
			return nil, fmt.Errorf("argument %d must be \"string\"", len(a.Values)+1)
		}
		a.Values = append(a.Values, words[i])
	}
	if lower != -1 && len(a.Values) < lower {
		return nil, fmt.Errorf("too few arguments (need at least %d)", lower)
	}
	if upper != -1 && len(a.Values) > upper {
		return nil, fmt.Errorf("too many arguments (need at most %d)", upper)
	}
	return a, nil
}

func (a *Args) set(f byte, v *Value, optional bool) {
	e := a.flags[f]
	if e == nil {
		e = &flagEntry{optional: optional}
		a.flags[f] = e
	}
	e.count++
	if v != nil {
		e.values = append(e.values, *v)
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
	Group int    // commands from one line share a group; a failing one skips the rest of it
	File  string // the file it came from, for its errors ("" for a command line)
	Line  int
}

// Parse looks up and parses one command's words, with tmux's messages.
func Parse(words []string) (*Command, error) { return ParseValues(Strings(words)) }

// ParseValues is Parse for words that may be { … } blocks.
func ParseValues(words []Value) (*Command, error) {
	if len(words) == 0 || words[0].IsBlock {
		return nil, errors.New("no command")
	}
	spec, err := Find(words[0].String)
	if err != nil {
		return nil, err
	}
	args, err := ParseArgs(spec.Template, spec.Lower, spec.Upper, words, argTypes[spec.Name])
	if errors.Is(err, errUsage) {
		return nil, fmt.Errorf("usage: %s %s", spec.Name, spec.Usage)
	}
	if err != nil {
		return nil, fmt.Errorf("command %s: %w", spec.Name, err)
	}
	return &Command{Spec: spec, Args: args}, nil
}

func setOptionArgType(a *Args, idx int) ArgType {
	if a.Has('B') || idx == 1 {
		return ArgCommandsOrString
	}
	return ArgString
}

// argTypes are tmux's args_parse callbacks: which arguments may be { … } blocks.
var argTypes = map[string]func(a *Args, idx int) ArgType{
	"bind-key":       func(*Args, int) ArgType { return ArgCommandsOrString },
	"choose-tree":    func(*Args, int) ArgType { return ArgCommandsOrString },
	"choose-client":  func(*Args, int) ArgType { return ArgCommandsOrString },
	"choose-buffer":  func(*Args, int) ArgType { return ArgCommandsOrString },
	"switch-mode":    func(*Args, int) ArgType { return ArgCommandsOrString },
	"display-panes":  func(*Args, int) ArgType { return ArgCommandsOrString },
	"confirm-before": func(*Args, int) ArgType { return ArgCommandsOrString },
	"command-prompt": func(*Args, int) ArgType { return ArgCommandsOrString },
	"display-menu": func(a *Args, idx int) ArgType {
		// Menu items are a name, a key and a command; an empty name is a separator.
		i := 0
		for {
			if i == idx {
				return ArgString
			}
			if a.Values[i].text() == "" {
				i++
				continue
			}
			i++
			if i == idx {
				return ArgString
			}
			i++
			if i == idx {
				return ArgCommandsOrString
			}
			i++
		}
	},
	"if-shell": func(_ *Args, idx int) ArgType {
		if idx == 1 || idx == 2 {
			return ArgCommandsOrString
		}
		return ArgString
	},
	"set-option":        setOptionArgType,
	"set-window-option": setOptionArgType,
	"set-hook":          setOptionArgType,
	"run-shell": func(a *Args, _ int) ArgType {
		if a.Has('C') {
			return ArgCommandsOrString
		}
		return ArgString
	},
}

// StartsServer reports whether a command line starts a server when there is none: when it
// builds and any of its commands does, as tmux's client decides.
func StartsServer(cmds [][]string) bool {
	list, err := BuildArgv(cmds, DefaultAliases)
	if err != nil {
		return false
	}
	for _, c := range list.Commands {
		if c.Spec.StartServer {
			return true
		}
	}
	return false
}

// CmdFlags are a command's properties in tmux's table.
type CmdFlags int

// tmux's CMD_* flags.
const (
	CmdStartserver   CmdFlags = 0x1  // starts a server when there is none
	CmdReadonly      CmdFlags = 0x2  // allowed for a read-only client
	CmdAfterhook     CmdFlags = 0x4  // fires after-<command> hooks
	CmdClientCflag   CmdFlags = 0x8  // -c names the client
	CmdClientTflag   CmdFlags = 0x10 // -t names the client
	CmdClientCanfail CmdFlags = 0x20 // the client may be missing
)

// FindType is what a target names.
type FindType int

// tmux's cmd_find types.
const (
	FindPane FindType = iota
	FindWindow
	FindSession
)

// FindFlags change how a target is found.
type FindFlags int

// tmux's CMD_FIND_* flags.
const (
	FindPreferUnattached FindFlags = 0x1
	FindQuiet            FindFlags = 0x2
	FindWindowIndex      FindFlags = 0x4
	FindDefaultMarked    FindFlags = 0x8
	FindExactSession     FindFlags = 0x10
	FindExactWindow      FindFlags = 0x20
	FindCanfail          FindFlags = 0x40
)

// TargetSpec says which flag names a command's source or target, what it names and how it is
// found (tmux's cmd_entry_flag).
type TargetSpec struct {
	Flag  byte
	Type  FindType
	Flags FindFlags
}
