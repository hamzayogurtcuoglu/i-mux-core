package cmdlang

import (
	"fmt"
	"strings"
)

// Arg is one argument of a command: a string, or a { … } block of commands.
type Arg struct {
	String   string
	Block    bool
	Commands []*Command // the block's commands, when Block is set
}

// Command is one parsed command. Args[0] is the command's name.
type Command struct {
	Line int
	Args []Arg
}

// Words returns the command's arguments as strings; a block becomes its commands' text.
func (c *Command) Words() []string {
	w := make([]string, len(c.Args))
	for i, a := range c.Args {
		if a.Block {
			w[i] = Format(a.Commands)
		} else {
			w[i] = a.String
		}
	}
	return w
}

// Options control how Parse treats conditions, variables and ~.
type Options struct {
	// Cond evaluates a %if or %elif format. When it is nil, every condition is false.
	Cond func(format string) bool
	// AllBranches keeps the commands of every %if branch, for analysis. Cond is still called.
	AllBranches bool
	// Env returns an environment variable for $NAME. When it is nil, $NAME is kept as written.
	Env func(name string) (string, bool)
	// Home returns the home directory of a user ("" for the current one) for ~. When it is
	// nil, ~ is kept as written.
	Home func(user string) (string, bool)
	// Assign is called for NAME=value lines (hidden for %hidden ones) that are not skipped.
	Assign func(name, value string, hidden bool)
}

// Error is a parse error at a line (1-based).
type Error struct {
	Line int
	Msg  string
}

func (e *Error) Error() string { return fmt.Sprintf("%d: %s", e.Line, e.Msg) }

// Parse parses tmux command syntax: a config file or a command string.
func Parse(src string, o Options) ([]*Command, error) {
	p := &parser{lex: &lexer{src: src, line: 1, opts: &o}, opts: &o}
	p.advance()
	cmds := p.statements(false)
	if p.tok.kind != tokEOF {
		p.fail("syntax error") // %elif, %else or %endif without %if
	}
	if p.lex.err != nil {
		return nil, p.lex.err
	}
	if p.err != nil {
		return nil, p.err
	}
	return cmds, nil
}

type parser struct {
	lex  *lexer
	opts *Options
	tok  token
	err  *Error
	// skip counts enclosing branches that are not taken (and not kept).
	skip int
}

func (p *parser) advance() {
	if p.err != nil || p.lex.err != nil {
		p.tok = token{kind: tokEOF}
		return
	}
	p.tok = p.lex.next()
}

func (p *parser) fail(msg string) {
	if p.err == nil && p.lex.err == nil {
		p.err = &Error{Line: p.tok.line, Msg: msg}
	}
	p.tok = token{kind: tokEOF}
}

func (p *parser) failed() bool { return p.err != nil || p.lex.err != nil }

// statements reads statements up to EOF, or up to the closing } when inBlock.
func (p *parser) statements(inBlock bool) []*Command {
	var out []*Command
	for !p.failed() {
		switch p.tok.kind {
		case tokEOF:
			if inBlock {
				p.fail("syntax error")
			}
			return out
		case tokNewline:
			p.advance()
			continue
		case tokRBrace:
			if !inBlock {
				p.fail("syntax error")
				return out
			}
			return out
		case tokElif, tokElse, tokEndif:
			return out // the caller's condition handles it
		case tokHidden:
			p.advance()
			if p.tok.kind != tokEquals {
				p.fail("syntax error")
				return out
			}
			p.assign(p.tok.text, true)
			p.advance()
		case tokIf:
			out = append(out, p.condition()...)
		default:
			out = append(out, p.commands()...)
		}
		switch p.tok.kind {
		case tokNewline:
			p.advance()
		case tokEOF:
		case tokRBrace:
			if !inBlock {
				p.fail("syntax error")
			}
		default:
			p.fail("syntax error")
		}
	}
	return out
}

func (p *parser) assign(text string, hidden bool) {
	if p.skip > 0 || p.opts.Assign == nil {
		return
	}
	name, value, _ := strings.Cut(text, "=")
	p.opts.Assign(name, value, hidden)
}

// evaluate reads the format after %if or %elif and evaluates it.
func (p *parser) evaluate() bool {
	p.advance()
	if p.tok.kind != tokFormat && p.tok.kind != tokToken {
		p.fail("syntax error")
		return false
	}
	f := p.tok.text
	p.advance()
	if p.opts.Cond == nil {
		return false
	}
	return p.opts.Cond(f)
}

// condition reads %if … [%elif …]* [%else …] %endif, on several lines or inline.
func (p *parser) condition() []*Command {
	var out []*Command
	taken := false
	take := func(cond bool, body func() []*Command) {
		keep := p.opts.AllBranches || (cond && !taken)
		if !keep {
			p.skip++
		}
		cmds := body()
		if !keep {
			p.skip--
		} else {
			out = append(out, cmds...)
		}
		if cond {
			taken = true
		}
	}
	body := func() []*Command {
		if p.tok.kind == tokNewline {
			p.advance()
			return p.statements(false)
		}
		return p.commands()
	}
	take(p.evaluate(), body)
	for p.tok.kind == tokElif && !p.failed() {
		take(p.evaluate(), body)
	}
	if p.tok.kind == tokElse && !p.failed() {
		p.advance()
		take(true, body)
	}
	if p.tok.kind != tokEndif {
		p.fail("syntax error")
		return out
	}
	p.advance()
	return out
}

// commands reads commands separated by ; up to the end of the statement.
func (p *parser) commands() []*Command {
	var out []*Command
	for !p.failed() {
		switch p.tok.kind {
		case tokIf:
			out = append(out, p.condition()...)
		case tokToken, tokEquals:
			if c := p.command(); c != nil && p.skip == 0 {
				out = append(out, c)
			}
		default:
			return out
		}
		if p.tok.kind != tokSemi {
			return out
		}
		for p.tok.kind == tokSemi {
			p.advance()
		}
	}
	return out
}

// command reads [NAME=value] name args…; a lone assignment returns nil.
func (p *parser) command() *Command {
	if p.tok.kind == tokEquals {
		p.assign(p.tok.text, false)
		p.advance()
		if p.tok.kind != tokToken {
			return nil
		}
	}
	c := &Command{Line: p.tok.line, Args: []Arg{{String: p.tok.text}}}
	p.advance()
	for !p.failed() {
		switch p.tok.kind {
		case tokToken, tokEquals:
			c.Args = append(c.Args, Arg{String: p.tok.text})
			p.advance()
		case tokLBrace:
			p.advance()
			block := p.statements(true)
			if p.tok.kind != tokRBrace {
				p.fail("syntax error")
				return nil
			}
			p.advance()
			if block == nil {
				block = []*Command{}
			}
			c.Args = append(c.Args, Arg{Block: true, Commands: block})
		default:
			return c
		}
	}
	return nil
}

// Format turns commands back into command syntax that parses to the same commands.
func Format(cmds []*Command) string {
	var b strings.Builder
	for i, c := range cmds {
		if i > 0 {
			b.WriteString(" ; ")
		}
		for j, a := range c.Args {
			if j > 0 {
				b.WriteByte(' ')
			}
			if a.Block {
				b.WriteString("{ " + Format(a.Commands) + " }")
			} else {
				b.WriteString(Quote(a.String))
			}
		}
	}
	return b.String()
}

// Quote quotes s so that it parses back as one word.
func Quote(s string) string {
	if s != "" && !strings.ContainsAny(s, " \t\n\r\"'\\$~;{}#%") {
		return s
	}
	// Inside quotes a newline drops the spaces and any comment after it, so newlines are
	// written as \n in double quotes.
	if !strings.ContainsAny(s, "'\n\r") {
		return "'" + s + "'"
	}
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, `$`, `\$`, `~`, `\~`, "\n", `\n`, "\r", `\r`, "\t", `\t`)
	return `"` + r.Replace(s) + `"`
}

// SplitArgv splits command-line words into commands as tmux does: a word ";" ends a command, a
// word ending in ";" ends the command after it (without the ";"), and a word ending in "\;" is
// a literal ";".
func SplitArgv(words []string) [][]string {
	var cmds [][]string
	var cur []string
	end := func() {
		if len(cur) > 0 {
			cmds = append(cmds, cur)
		}
		cur = nil
	}
	for _, w := range words {
		switch {
		case strings.HasSuffix(w, `\;`):
			cur = append(cur, w[:len(w)-2]+";")
		case strings.HasSuffix(w, ";"):
			if w != ";" {
				cur = append(cur, w[:len(w)-1])
			}
			end()
		default:
			cur = append(cur, w)
		}
	}
	end()
	return cmds
}
