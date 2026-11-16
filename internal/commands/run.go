package commands

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/cmdlang"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/keys"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/model"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/pty"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/target"
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
	// PaneKey sends a key to a pane, encoded as the pane asked (TASKS.md C2.11); false when
	// the key has no encoding there.
	PaneKey(p *model.Pane, k keys.Key) bool
	// OptionChanged tells the server an option was set, to apply and redraw what depends on
	// it (tmux's options_push_changes).
	OptionChanged(name string)
	// SwitchClient makes an attached client show another session.
	SwitchClient(name string, s *model.Session) error
	// DefaultSize is the size of a new window of s (tmux's default_window_size).
	DefaultSize(s *model.Session) (sx, sy int)
	// RemovePane takes a pane out, killing its program, and its window and session if empty.
	RemovePane(p *model.Pane)
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
	// Cols and Rows are its terminal's size, when it has one.
	Cols, Rows int
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
	// Current is the command list's current target (tmux's cmdq state): what a command made
	// is the default target of the commands after it.
	Current *target.State
}

// targetContext is what the item's targets are looked up in.
func (it *Item) targetContext() *target.Context {
	ctx := &target.Context{Tree: it.Host.Tree(), Current: it.Current}
	for _, c := range it.Host.Clients() {
		ctx.Clients = append(ctx.Clients, target.Client{Name: c.Name, Session: c.Session, Activity: c.Activity})
	}
	if it.Client != nil {
		if it.Client.AttachTo != nil {
			tc := target.Client{Name: it.Client.Name, Session: it.Client.AttachTo, Activity: it.Host.Now()}
			ctx.Clients = append(ctx.Clients, tc)
			ctx.Client = &ctx.Clients[len(ctx.Clients)-1]
		}
		if p, ok := strings.CutPrefix(it.Client.Env["TMUX_PANE"], "%"); ok {
			if id, err := strconv.Atoi(p); err == nil {
				ctx.Inside = it.Host.Tree().Pane(id)
			}
		}
	}
	return ctx
}

// find looks up a target (tmux's cmd_find_target).
func (it *Item) find(t string, typ target.Type, flags int) (target.State, error) {
	return target.Find(it.targetContext(), t, typ, flags)
}

// findFlag looks up the target given with flag f, or the current one.
func (it *Item) findFlag(f byte, typ target.Type, flags int) (target.State, error) {
	t, _ := it.Cmd.Args.Get(f)
	return it.find(t, typ, flags)
}

// setCurrent makes st the command list's current target.
func (it *Item) setCurrent(st target.State) {
	if it.Current != nil {
		*it.Current = st
	}
}

// Print writes a line of the command's output.
func (it *Item) Print(s string) { it.Out.WriteString(s + "\n") }

type execFunc func(*Item) error

// implemented are the commands the engine runs so far; the rest of tmux's table is known (for
// lookup and argument errors) but not run yet. The command table grows in task C3.2.
var implemented = map[string]execFunc{
	"new-session":         newSession,
	"has-session":         hasSession,
	"kill-session":        killSession,
	"kill-server":         killServer,
	"start-server":        func(*Item) error { return nil },
	"attach-session":      attachSession,
	"detach-client":       detachClient,
	"send-keys":           sendKeys,
	"set-option":          setOption,
	"set-window-option":   setOption,
	"show-options":        showOptions,
	"show-window-options": showOptions,
	"switch-client":       switchClient,
	"new-window":          newWindow,
	"split-window":        splitWindow,
	"select-window":       selectWindow,
	"select-pane":         selectPane,
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
	current := &target.State{Idx: -1}
	for _, cmd := range list.Commands {
		if cmd.Group == skip {
			continue
		}
		if err := runOne(h, c, cmd, out, current); err != nil {
			// A quiet error (a target not found quietly) fails with no message.
			if err.Error() != "" {
				// A config file's errors say where they come from (tmux's cfg_add_cause).
				if cmd.File != "" {
					fmt.Fprintf(&errs, "%s:%d: ", cmd.File, cmd.Line)
				}
				errs.WriteString(err.Error() + "\n")
			}
			failed = true
			skip = cmd.Group
		}
	}
	return Result{Stdout: out.String(), Stderr: errs.String(), Failed: failed}
}

func runOne(h Host, c *Client, cmd *Command, out *strings.Builder, current *target.State) error {
	f := implemented[cmd.Spec.Name]
	if f == nil {
		return fmt.Errorf("%s: not implemented by imux-core yet", cmd.Spec.Name)
	}
	return f(&Item{Host: h, Client: c, Cmd: cmd, Out: out, Current: current})
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
