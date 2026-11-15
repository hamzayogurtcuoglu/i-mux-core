package commands

import (
	"fmt"
	"strings"
	"time"

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
}

// Client is the client a command list came from; nil for the config file.
type Client struct {
	Cwd string
	Env map[string]string
	TTY bool // its stdin is a terminal
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
	"new-session":  newSession,
	"has-session":  hasSession,
	"kill-session": killSession,
	"kill-server":  killServer,
	"start-server": func(*Item) error { return nil },
}

// Result is what a command list gave: its output, its errors and whether it failed.
type Result struct {
	Stdout string
	Stderr string
	Failed bool
}

// Run parses and runs a client's command list in order. A parse error runs nothing; a failing
// command stops the rest of the list, as in tmux.
func Run(h Host, c *Client, cmds [][]string) Result {
	var out, errs strings.Builder
	list, err := ParseList(cmds)
	if err != nil {
		return Result{Stderr: err.Error() + "\n", Failed: true}
	}
	for _, cmd := range list {
		if err := runOne(h, c, cmd, &out); err != nil {
			errs.WriteString(err.Error() + "\n")
			return Result{Stdout: out.String(), Stderr: errs.String(), Failed: true}
		}
	}
	return Result{Stdout: out.String()}
}

func runOne(h Host, c *Client, cmd *Command, out *strings.Builder) error {
	f := implemented[cmd.Spec.Name]
	if f == nil {
		return fmt.Errorf("%s: not implemented by imux-core yet", cmd.Spec.Name)
	}
	return f(&Item{Host: h, Client: c, Cmd: cmd, Out: out})
}

// RunConfig runs a config file's commands, one line at a time: a line whose command fails does
// not stop the next lines, as in tmux. It returns the errors, each with its line.
func RunConfig(h Host, file string, lines [][][]string, lineNos []int) []string {
	var causes []string
	var out strings.Builder
	for i, cmds := range lines {
		list, err := ParseList(cmds)
		if err == nil {
			for _, cmd := range list {
				if err = runOne(h, nil, cmd, &out); err != nil {
					break
				}
			}
		}
		if err != nil {
			causes = append(causes, fmt.Sprintf("%s:%d: %v", file, lineNos[i], err))
		}
	}
	return causes
}
