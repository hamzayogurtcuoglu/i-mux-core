package commands

import (
	"fmt"
	"sort"
	"strings"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/cmdlang"
)

// Building command lists from parsed text and printing them, as tmux 3.8's cmd-parse.y
// (cmd_parse_build_commands, cmd_parse_expand_alias), cmd.c (cmd_print, cmd_list_print) and
// arguments.c (args_print, args_escape) do.

// List is a list of commands; each line of the source is a group.
type List struct {
	Commands []*Command
}

// DefaultAliases is tmux's command-alias option by default.
var DefaultAliases = []string{
	"split-pane=split-window",
	"splitp=split-window",
	"server-info=show-messages -JT",
	"info=show-messages -JT",
	"choose-window=choose-tree -w",
	"choose-session=choose-tree -s",
}

// Builder builds command lists from parsed commands.
type Builder struct {
	File    string            // the file, for errors and Verbose; "" for a command string
	Aliases []string          // command-alias entries, name=command
	Parse   cmdlang.Options   // how aliases are parsed
	Verbose func(text string) // source-file -v: called with each line's commands as built

	line    int // tmux's pi->line: the line of the command being built, or of its last block
	noAlias bool
	group   int
}

// Error is a build error, printed with the file and line as tmux prints it.
type Error struct {
	File string
	Line int
	Msg  string
}

// Error prints as tmux's cmd_parse_get_error: with the file and line, or bare for a command
// line or string.
func (e *Error) Error() string {
	if e.File != "" {
		return fmt.Sprintf("%s:%d: %s", e.File, e.Line, e.Msg)
	}
	return e.Msg
}

// BuildArgv builds a command line's commands (already split at ";"), as tmux's
// cmd_parse_from_arguments does: all in one group, with aliases expanded.
func BuildArgv(cmds [][]string, aliases []string) (*List, error) {
	parsed := make([]*cmdlang.Command, 0, len(cmds))
	for _, words := range cmds {
		c := &cmdlang.Command{}
		for _, w := range words {
			c.Args = append(c.Args, cmdlang.Arg{String: w})
		}
		parsed = append(parsed, c)
	}
	b := &Builder{Aliases: aliases}
	return b.Build(parsed)
}

// Build builds cmds: one group per source line, each line's commands printed with Verbose
// once the line is done.
func (b *Builder) Build(cmds []*cmdlang.Command) (*List, error) {
	result := &List{}
	var current *List
	line := -1
	flush := func() {
		if current == nil {
			return
		}
		// The line's commands, those of an alias' expansion too, join the line's group.
		b.group++
		for _, c := range current.Commands {
			c.Group = b.group
		}
		if b.Verbose != nil {
			b.Verbose(b.location() + current.Print())
		}
		result.Commands = append(result.Commands, current.Commands...)
	}
	for _, c := range cmds {
		if c.Line != line {
			flush()
			current = &List{}
		}
		line, b.line = c.Line, c.Line
		built, err := b.buildCommand(c)
		if err != nil {
			return nil, err
		}
		current.Commands = append(current.Commands, built.Commands...)
	}
	flush()
	return result, nil
}

func (b *Builder) location() string {
	if b.File != "" {
		return fmt.Sprintf("%s:%d: ", b.File, b.line)
	}
	return fmt.Sprintf("%d: ", b.line)
}

func (b *Builder) fail(msg string) error { return &Error{File: b.File, Line: b.line, Msg: msg} }

func (b *Builder) alias(name string) (string, bool) {
	for _, a := range b.Aliases {
		if n, cmd, ok := strings.Cut(a, "="); ok && n == name {
			return cmd, true
		}
	}
	return "", false
}

func (b *Builder) buildCommand(c *cmdlang.Command) (*List, error) {
	if len(c.Args) == 0 || c.Args[0].Block {
		return &List{}, nil
	}
	if !b.noAlias {
		if text, ok := b.alias(c.Args[0].String); ok {
			cmds, err := cmdlang.Parse(text, b.Parse)
			if err != nil {
				return nil, b.fail(err.Error())
			}
			if len(cmds) == 0 {
				return &List{}, nil
			}
			last := cmds[len(cmds)-1]
			last.Args = append(last.Args, c.Args[1:]...)
			for _, x := range cmds {
				x.Line = b.line
			}
			b.noAlias = true
			defer func() { b.noAlias = false }()
			// The expansion is built like a line of its own, and printed as one with -v.
			inner := *b
			list, err := inner.Build(cmds)
			b.line, b.group = inner.line, inner.group
			return list, err
		}
	}
	words := make([]Value, len(c.Args))
	for i, a := range c.Args {
		if !a.Block {
			words[i] = Value{String: a.String}
			continue
		}
		list, err := b.Build(a.Commands)
		if err != nil {
			return nil, err
		}
		words[i] = Value{Commands: list, IsBlock: true}
	}
	cmd, err := ParseValues(words)
	if err != nil {
		return nil, b.fail(err.Error())
	}
	cmd.File, cmd.Line = b.File, b.line
	return &List{Commands: []*Command{cmd}}, nil
}

// Print prints a list as tmux's cmd_list_print does: " ; " between commands of one
// group, " ;; " between groups.
func (l *List) Print() string { return l.PrintList(false, false) }

// PrintList prints as tmux's cmd_list_print with flags does: escaped picks the \;
// separators, noGroups prints the single separator between all groups.
func (l *List) PrintList(escaped, noGroups bool) string {
	single, double := " ; ", " ;; "
	if escaped {
		single, double = " \\; ", " \\;\\; "
	}
	var s strings.Builder
	for i, c := range l.Commands {
		if i > 0 {
			if !noGroups && c.Group != l.Commands[i-1].Group {
				s.WriteString(double)
			} else {
				s.WriteString(single)
			}
		}
		s.WriteString(c.Print())
	}
	return s.String()
}

// Print prints a command as tmux's cmd_print does: its full name and its arguments.
func (c *Command) Print() string {
	if a := c.Args.Print(); a != "" {
		return c.Spec.Name + " " + a
	}
	return c.Spec.Name
}

// Print prints the arguments as tmux's args_print does: the flags without values together,
// then each flag with its value, in flag order, then the arguments.
func (a *Args) Print() string {
	var b strings.Builder
	flags := make([]byte, 0, len(a.flags))
	for f := range a.flags {
		flags = append(flags, f)
	}
	sort.Slice(flags, func(i, j int) bool { return flags[i] < flags[j] })
	for _, f := range flags {
		e := a.flags[f]
		if e.optional || len(e.values) != 0 {
			continue
		}
		if b.Len() == 0 {
			b.WriteByte('-')
		}
		for range e.count {
			b.WriteByte(f)
		}
	}
	var last *flagEntry
	for _, f := range flags {
		e := a.flags[f]
		if e.optional {
			if b.Len() != 0 {
				b.WriteByte(' ')
			}
			b.WriteByte('-')
			b.WriteByte(f)
			last = e
			continue
		}
		if len(e.values) == 0 {
			continue
		}
		for _, v := range e.values {
			if b.Len() != 0 {
				b.WriteByte(' ')
			}
			b.WriteByte('-')
			b.WriteByte(f)
			printValue(&b, v)
		}
		last = e
	}
	if last != nil && last.optional {
		b.WriteString(" --")
	}
	for _, v := range a.Values {
		printValue(&b, v)
	}
	return b.String()
}

func printValue(b *strings.Builder, v Value) {
	if b.Len() != 0 {
		b.WriteByte(' ')
	}
	if v.IsBlock {
		b.WriteString("{ " + v.Commands.Print() + " }")
		return
	}
	b.WriteString(Escape(v.String))
}

// Escape quotes an argument as tmux's args_escape does, so that it reads back the same.
func Escape(s string) string { return cmdlang.Escape(s) }
