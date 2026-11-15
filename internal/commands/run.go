package commands

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/cmdlang"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/model"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/pty"
)

// Host is what the commands need of the server. Its methods run in the server loop.
type Host interface {
	Tree() *model.Tree
	Now() time.Time
	// Spawn starts a program in a new pane.
	Spawn(p *model.Pane, spec pty.Spec) error
	// RemoveSession takes a session out, killing its panes' programs.
	RemoveSession(s *model.Session)
	// KillServer ends the server once the current command list is done.
	KillServer()
	// Environ is the server's global environment; SocketPath and Pid go into $TMUX.
	Environ() []string
	SocketPath() string
	Pid() int
	// Clients lists the attached clients; Detach detaches one.
	Clients() []ClientInfo
	Detach(name string)
}

// ClientInfo is an attached client, as the commands see it.
type ClientInfo struct {
	Name     string // its terminal (#{client_name})
	Session  *model.Session
	Activity time.Time
}

// Client is the client a command list came from; nil for the config file.
type Client struct {
	Cwd  string
	Env  map[string]string
	TTY  bool   // its stdin is a terminal
	Name string // its terminal's path (#{client_name})
	// AttachTo is set by attach-session and new-session: the client attaches to it once its
	// commands are done.
	AttachTo *model.Session
}

// Item is one command being run.
type Item struct {
	Host   Host
	Client *Client
	Cmd    *Command
	Out    *strings.Builder
}

// Print writes a line of the command's output.
func (it *Item) Print(s string) { it.Out.WriteString(s + "\n") }

type execFunc func(*Item) error

// implemented are the commands the engine runs so far; the rest of tmux's table is known (for
// lookup and argument errors) but not run yet. The command table grows in task C3.2.
var implemented = map[string]execFunc{
	"new-session":    newSession,
	"has-session":    hasSession,
	"kill-session":   killSession,
	"kill-server":    killServer,
	"start-server":   func(*Item) error { return nil },
	"attach-session": attachSession,
	"detach-client":  detachClient,
}

// Result is what a command list gave: its output, its errors and whether it failed.
type Result struct {
	Stdout string
	Stderr string
	Failed bool
}

// Run builds and runs a client's command line in order. A command line that does not build
// runs nothing; a failing command stops the rest of its group, which for a command line is
// all of it, as in tmux.
func Run(h Host, c *Client, cmds [][]string) Result {
	var out strings.Builder
	list, err := BuildArgv(cmds, DefaultAliases)
	if err != nil {
		return Result{Stderr: err.Error() + "\n", Failed: true}
	}
	return runList(h, c, list, &out)
}

// runList runs a list; a failing command skips the rest of its group.
func runList(h Host, c *Client, list *List, out *strings.Builder) Result {
	var errs strings.Builder
	failed := false
	skip := -1
	for _, cmd := range list.Commands {
		if cmd.Group == skip {
			continue
		}
		if err := runOne(h, c, cmd, out); err != nil {
			// A config file's errors say where they come from (tmux's cfg_add_cause).
			if cmd.File != "" {
				fmt.Fprintf(&errs, "%s:%d: ", cmd.File, cmd.Line)
			}
			errs.WriteString(err.Error() + "\n")
			failed = true
			skip = cmd.Group
		}
	}
	return Result{Stdout: out.String(), Stderr: errs.String(), Failed: failed}
}

func runOne(h Host, c *Client, cmd *Command, out *strings.Builder) error {
	f := implemented[cmd.Spec.Name]
	if f == nil {
		return fmt.Errorf("%s: not implemented by imux-core yet", cmd.Spec.Name)
	}
	return f(&Item{Host: h, Client: c, Cmd: cmd, Out: out})
}

// RunConfig parses and runs a config file, as tmux's source-file does: a file that does not
// parse or build runs nothing; a failing command skips the rest of its line, and the next lines
// still run. It returns the errors, with their file and line.
func RunConfig(h Host, file, src string, opts cmdlang.Options) []string {
	cmds, err := cmdlang.Parse(src, opts)
	if err != nil {
		var pe *cmdlang.Error
		if errors.As(err, &pe) {
			return []string{fmt.Sprintf("%s:%d: %s", file, pe.Line, pe.Msg)}
		}
		return []string{file + ": " + err.Error()}
	}
	b := &Builder{File: file, Aliases: DefaultAliases, Parse: opts}
	list, err := b.Build(cmds)
	if err != nil {
		return []string{err.Error()}
	}
	var out strings.Builder
	res := runList(h, nil, list, &out)
	if res.Stderr == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(res.Stderr, "\n"), "\n")
}
