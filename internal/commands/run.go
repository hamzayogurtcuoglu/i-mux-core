package commands

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/buffers"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/cmdlang"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/format"
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
	Clients() []*ClientInfo
	Detach(name string)
	// PaneKey sends a key to a pane, encoded as the pane asked (TASKS.md C2.11); false when
	// the key has no encoding there.
	PaneKey(p *model.Pane, k keys.Key) bool
	// PaneMouse is window_pane_key for a mouse key: the event goes to the pane's program,
	// encoded as it asked, if it asked for it.
	PaneMouse(p *model.Pane, m *target.Mouse)
	// DragResize is resize-pane -M: the client's drag, from the event m on, moves the borders
	// under it.
	DragResize(client string, m *target.Mouse)
	// OptionChanged tells the server an option was set, to apply and redraw what depends on
	// it (tmux's options_push_changes).
	OptionChanged(name string)
	// SwitchClient makes an attached client show another session.
	SwitchClient(name string, s *model.Session) error
	// SetKeyTable puts the client named (an attached one) in a key table.
	SetKeyTable(client, table string) error
	// DefaultSize is the size of a new window of s (tmux's default_window_size).
	DefaultSize(s *model.Session) (sx, sy int)
	// RemovePane takes a pane out, killing its program, and its window and session if empty.
	RemovePane(p *model.Pane)
	// Respawn starts a pane's program again, killing the one it has.
	Respawn(p *model.Pane, spec pty.Spec) error
	// ServerInfo is what the server's format variables give (pid, start_time, …).
	ServerInfo() *format.Server
	// RunJob runs a job (run-shell, if-shell): done is called in the loop with its output
	// and how it ended, or with err when it could not start.
	RunJob(spec JobSpec, done func(out []byte, status pty.ExitStatus, err error))
	// After calls f in the loop after d (run-shell -d).
	After(d time.Duration, f func())
	// ShowMessage shows a message on a client's status line (tmux's status_message_set).
	ShowMessage(client, msg string, o MessageOptions)
	// Overlay reports whether a client shows an overlay, and whether it is a popup.
	Overlay(client string) (shown, popup bool)
	// ClearOverlay closes a client's overlay (server_client_clear_overlay).
	ClearOverlay(client string)
	// DisplayPopup opens a popup (popup_display); done gets its status when it closes. It
	// reports false when the popup could not open.
	DisplayPopup(spec PopupSpec, done func(status int)) bool
	// ModifyPopup changes a client's open popup (popup_modify).
	ModifyPopup(client string, c PopupChange)
	// ClientGeometry is what places a popup or menu on a client.
	ClientGeometry(client string) ClientGeometry
	// DisplayMenu shows a menu on a window (menu_display), replacing its menu.
	DisplayMenu(spec MenuSpec)
	// Buffers are the paste buffers.
	Buffers() *buffers.Store
	// SetSelection gives data to a client's terminal clipboard (tty_set_selection: OSC 52,
	// when the terminal has Ms).
	SetSelection(client string, data []byte)
	// PaneWrite writes bytes to a pane's program, as typed.
	PaneWrite(p *model.Pane, b []byte)
	// SetPrompt opens a command prompt on a client (status_prompt_set); false when it shows
	// one already.
	SetPrompt(spec PromptSpec) bool
	// RunCommands runs a command list for an attached client, from its current target
	// (cmdq_append for the client).
	RunCommands(client string, list *List)
}

// MessageOptions are how a message is shown (status_message_set's arguments).
type MessageOptions struct {
	// Delay is how long it shows, in milliseconds: -1 for display-time, 0 until a key.
	Delay int
	// IgnoreStyles shows its # as text; IgnoreKeys (display-message -N) keeps it up through
	// keys; NoFreeze (display-message -C) lets the panes update under it.
	IgnoreStyles, IgnoreKeys, NoFreeze bool
}

// JobSpec is a job to run: a shell command (Shell -c Command) or an argv, in Dir, with Env.
type JobSpec struct {
	Shell      string
	Command    string
	Argv       []string
	Dir        string
	Env        []string
	ShowStderr bool // stderr goes with stdout (run-shell -E), else nowhere
}

// ClientInfo is an attached client, as the commands and formats see it.
type ClientInfo = format.Client

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
	// AttachedHook is set by attach-session: the client-attached hook fires once the client
	// is attached (new-session's attach fires none, as in tmux).
	AttachedHook bool
	// Session is the session an attached client shows: such a client runs the commands of
	// the key bindings it presses (tmux's cmdq_get_client for a binding).
	Session *model.Session

	// stdoutClosed is set once a file write to "-" (show-buffer, save-buffer -) is done: the
	// client closes its stdout after it, and a second one fails.
	stdoutClosed bool
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
	// Formats are added to every format the command expands: a hook's hook_* and hook
	// (tmux's cmdq_add_formats).
	Formats map[string]string
	// Target is what the command found with its target flag (tmux's item->target), for its
	// after- hook.
	Target *target.State
	// noHooks is CMDQ_STATE_NOHOOKS: the command runs for a hook and fires none itself.
	noHooks bool
	rs      *runState
	// after are what runs right after the command (hooks, commands it inserts), in the
	// order they were added; the last added runs first, as tmux's cmdq_insert_after does.
	after []afterItem
	// cont continues the queue once a waiting command (it.wait) is done; early and
	// earlyErr keep a resume that came before the command returned.
	cont     func(error)
	early    bool
	earlyErr error
}

// targetContext is what the item's targets are looked up in.
func (it *Item) targetContext() *target.Context {
	ctx := &target.Context{Tree: it.Host.Tree(), Current: it.Current, Mouse: it.Mouse()}
	for _, c := range it.Host.Clients() {
		ctx.Clients = append(ctx.Clients, target.Client{Name: c.Name, Session: c.Session, Activity: c.Activity})
	}
	if it.Client != nil {
		if it.Client.Session != nil {
			for i := range ctx.Clients {
				if ctx.Clients[i].Name == it.Client.Name {
					ctx.Client = &ctx.Clients[i]
				}
			}
		}
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
	st, err := it.find(t, typ, flags)
	if err == nil && it.Cmd.Spec.Target != nil && it.Cmd.Spec.Target.Flag == f {
		found := st
		it.Target = &found
	}
	return st, err
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
var implemented map[string]execFunc

// The table is filled in init: its commands run command lists themselves (hooks).
func init() {
	implemented = map[string]execFunc{
		"new-session":         newSession,
		"has-session":         hasSession,
		"kill-session":        killSession,
		"kill-server":         killServer,
		"start-server":        func(*Item) error { return nil },
		"attach-session":      attachSession,
		"detach-client":       detachClient,
		"send-keys":           sendKeys,
		"send-prefix":         sendPrefix,
		"bind-key":            bindKey,
		"unbind-key":          unbindKey,
		"list-keys":           listKeys,
		"set-option":          setOption,
		"set-window-option":   setOption,
		"show-options":        showOptions,
		"show-window-options": showOptions,
		"show-hooks":          showOptions,
		"switch-client":       switchClient,
		"next-window":         nextWindow(true),
		"previous-window":     nextWindow(false),
		"swap-window":         swapWindow,
		"new-window":          newWindow,
		"split-window":        splitWindow,
		"select-window":       selectWindow,
		"select-pane":         selectPane,
		"display-message":     displayMessage,
		"display-popup":       displayPopup,
		"display-menu":        displayMenu,
		"set-buffer":          setBuffer,
		"delete-buffer":       setBuffer,
		"load-buffer":         loadBuffer,
		"paste-buffer":        pasteBuffer,
		"save-buffer":         saveBuffer,
		"show-buffer":         saveBuffer,
		"list-buffers":        listBuffers,
		"command-prompt":      commandPrompt,
		"list-sessions":       listSessions,
		"list-windows":        listWindows,
		"list-panes":          listPanes,
		"list-clients":        listClients,
		"rename-window":       renameWindow,
		"kill-window":         killWindow,
		"kill-pane":           killPane,
		"swap-pane":           swapPane,
		"respawn-pane":        respawnPane,
		"resize-pane":         resizePane,
		"resize-window":       resizeWindow,
		"select-layout":       selectLayout,
		"capture-pane":        capturePane,
		"set-hook":            setHook,
		"run-shell":           runShell,
		"if-shell":            ifShell,
		"source-file":         sourceFile,
	}
}

// Result is what a command list gave: its output, its errors and whether it failed.
type Result struct {
	Stdout string
	Stderr string
	Failed bool
	// Code is the exit status run-shell gives a command-line client (its job's), when not
	// zero; otherwise a failure is 1.
	Code int
}

// Run builds and runs a client's command line in order. A command line that does not build
// runs nothing; a failing command stops the rest of its group, which for a command line is
// all of it, as in tmux.
func Run(h Host, c *Client, cmds [][]string) Result {
	var res Result
	finished := false
	RunAsync(h, c, cmds, func(r Result) { res, finished = r, true })
	if !finished {
		// Only a test host without jobs runs synchronously; a waiting command needs the
		// server's loop (RunAsync).
		return Result{Stderr: "the command list is waiting\n", Failed: true}
	}
	return res
}

// RunAsync is Run for the server: done is called (in the loop) once the list has run, which
// may be after commands such as run-shell have waited for their jobs.
func RunAsync(h Host, c *Client, cmds [][]string, done func(Result)) {
	list, err := BuildArgv(cmds, DefaultAliases)
	if err != nil {
		done(Result{Stderr: err.Error() + "\n", Failed: true})
		return
	}
	newRun(h, c, &target.State{Idx: -1}).start(list, done)
}

// RunConfig parses and runs a config file, as tmux's source-file does: a file that does not
// parse or build runs nothing; a failing command skips the rest of its line, and the next lines
// still run. It returns the errors, with their file and line.
func RunConfig(h Host, file, src string, opts cmdlang.Options) []string {
	var causes []string
	finished := false
	RunConfigAsync(h, file, src, opts, func(c []string) { causes, finished = c, true })
	if !finished {
		return []string{file + ": the config is waiting"}
	}
	return causes
}

// RunConfigAsync is RunConfig for the server: done gets the errors once the file's commands
// have run, which may be after run-shell or if-shell waited for their jobs.
func RunConfigAsync(h Host, file, src string, opts cmdlang.Options, done func([]string)) {
	if opts.Cond == nil {
		opts.Cond = func(f string) bool { return format.True(ParseFormat(h, nil, f)) }
	}
	cmds, err := cmdlang.Parse(src, opts)
	if err != nil {
		var pe *cmdlang.Error
		if errors.As(err, &pe) {
			done([]string{fmt.Sprintf("%s:%d: %s", file, pe.Line, pe.Msg)})
			return
		}
		done([]string{file + ": " + err.Error()})
		return
	}
	b := &Builder{File: file, Aliases: DefaultAliases, Parse: opts}
	list, err := b.Build(cmds)
	if err != nil {
		done([]string{err.Error()})
		return
	}
	rs := newRun(h, nil, &target.State{Idx: -1})
	rs.config, rs.warned = true, map[string]bool{}
	rs.start(list, func(res Result) {
		if res.Stderr == "" {
			done(nil)
			return
		}
		done(strings.Split(strings.TrimSuffix(res.Stderr, "\n"), "\n"))
	})
}
