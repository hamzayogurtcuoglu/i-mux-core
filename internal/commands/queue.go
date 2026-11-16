package commands

import (
	"errors"
	"fmt"
	"strings"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/target"
)

// The command queue (tmux's cmd-queue.c, as far as the engine needs it): a command list runs
// in order; a command may wait (run-shell and if-shell wait for their jobs) and the list goes
// on once it is done; what a command inserts after itself (its after- hook, the commands
// if-shell picked) runs before the next command.

// errWait is what a command that waits returns (CMD_RETURN_WAIT); it calls it.resume later.
var errWait = errors.New("waiting")

// afterItem is something inserted after a command: a hook's event, or a command list run
// with the command's state.
type afterItem struct {
	hook *Event
	list *List
	// A list may run with its own current target and formats (source-file's files).
	current *target.State
	formats map[string]string
	depth   int
	warned  map[string]bool
	// fn is a callback item (cmdq_get_callback).
	fn func()
}

// runState is a command list's run (tmux's cmdq state): its client, output, current target,
// the formats a hook adds, and whether it runs for a hook.
type runState struct {
	h       Host
	c       *Client
	out     *strings.Builder
	current *target.State
	formats map[string]string
	noHooks bool
	// config is set while a config file's commands run (tmux's !cfg_finished).
	config bool
	// depth is how many source-file levels deep the list is.
	depth int
	// warned are the commands a config has already warned about not being implemented:
	// each warns once (COMPATIBILITY.md §10).
	warned map[string]bool

	list   *List
	i      int
	skip   int
	errs   strings.Builder
	failed bool
	code   int
	done   func(Result)
}

func newRun(h Host, c *Client, st *target.State) *runState {
	return &runState{h: h, c: c, out: &strings.Builder{}, current: st}
}

// child is a run with the same state (cmdq_get_state): what a command inserts runs so.
func (rs *runState) child() *runState {
	return &runState{
		h: rs.h, c: rs.c, out: rs.out, current: rs.current, formats: rs.formats,
		noHooks: rs.noHooks, config: rs.config, depth: rs.depth, warned: rs.warned,
	}
}

// start runs list; done is called with the result once every command has run.
func (rs *runState) start(list *List, done func(Result)) {
	rs.list, rs.i, rs.skip, rs.done = list, 0, -1, done
	rs.next()
}

// next runs commands until one waits or the list is done.
func (rs *runState) next() {
	for rs.i < len(rs.list.Commands) {
		cmd := rs.list.Commands[rs.i]
		rs.i++
		if cmd.Group == rs.skip {
			continue
		}
		// k continues the loop: at once when the command and what it inserted are done
		// synchronously, else from wherever they finish.
		paused, resumed := false, false
		rs.exec(cmd, func() {
			if paused {
				rs.next()
				return
			}
			resumed = true
		})
		if !resumed {
			paused = true
			return
		}
	}
	if rs.done != nil {
		rs.done(Result{Stdout: rs.out.String(), Stderr: rs.errs.String(), Failed: rs.failed, Code: rs.code})
	}
}

// exec runs one command, then what it inserted after itself, then k.
func (rs *runState) exec(cmd *Command, k func()) {
	f := implemented[cmd.Spec.Name]
	if f == nil {
		if rs.warned != nil && rs.warned[cmd.Spec.Name] {
			// A config warns once for each command it uses that is not implemented.
			rs.skip = cmd.Group
		} else {
			if rs.warned != nil {
				rs.warned[cmd.Spec.Name] = true
			}
			rs.fail(cmd, fmt.Errorf("%s: not implemented by imux-core yet", cmd.Spec.Name))
		}
		k()
		return
	}
	it := &Item{
		Host: rs.h, Client: rs.c, Cmd: cmd, Out: rs.out, Current: rs.current,
		Formats: rs.formats, noHooks: rs.noHooks, rs: rs,
	}
	finish := func(err error) {
		if err != nil {
			rs.fail(cmd, err)
		} else if !rs.noHooks && cmd.Spec.Flags&CmdAfterhook != 0 {
			if ev, ok := rs.afterEvent(it); ok {
				it.after = append(it.after, afterItem{hook: &ev})
			}
		}
		rs.runAfter(it.after, k)
	}
	if rs.noHooks {
		hookDepth++
	}
	err := f(it)
	if rs.noHooks {
		hookDepth--
	}
	if errors.Is(err, errWait) {
		if it.early {
			// The command was done before it said it would wait (a job that could not
			// start, a host that runs jobs at once).
			finish(it.earlyErr)
			return
		}
		it.cont = finish
		return
	}
	finish(err)
}

// fail records a command's error: the rest of its group is skipped; a message makes the
// client fail (tmux's cmdq_error sets its retval), a quiet error does not.
func (rs *runState) fail(cmd *Command, err error) {
	rs.skip = cmd.Group
	if err.Error() == "" {
		return
	}
	// A config file's errors say where they come from (tmux's cfg_add_cause); a client's
	// errors are the message alone, even for commands from a file it sourced.
	if cmd.File != "" && rs.c == nil {
		fmt.Fprintf(&rs.errs, "%s:%d: ", cmd.File, cmd.Line)
	}
	rs.errs.WriteString(err.Error() + "\n")
	rs.failed = true
}

// runAfter runs what was inserted after a command, the last inserted first, then k.
func (rs *runState) runAfter(items []afterItem, k func()) {
	if len(items) == 0 {
		k()
		return
	}
	last := items[len(items)-1]
	rest := items[:len(items)-1]
	then := func(res Result) {
		rs.merge(res)
		rs.runAfter(rest, k)
	}
	switch {
	case last.fn != nil:
		last.fn()
		rs.runAfter(rest, k)
	case last.hook != nil:
		rs.runHooks(*last.hook, then)
	default:
		c := rs.child()
		if last.current != nil {
			cur := *last.current
			c.current = &cur
		}
		if last.formats != nil {
			c.formats = last.formats
		}
		c.depth += last.depth
		if last.warned != nil {
			c.warned = last.warned
		}
		c.start(last.list, then)
	}
}

// merge takes an inserted list's errors and exit status as the command's client's.
func (rs *runState) merge(res Result) {
	if res.Failed {
		rs.errs.WriteString(res.Stderr)
		rs.failed = true
	}
	if res.Code != 0 {
		rs.code = res.Code
	}
}

// wait is CMD_RETURN_WAIT: the command goes on later with it.resume.
func (it *Item) wait() error { return errWait }

// resume continues the queue after a waiting command, with its error (nil when it worked).
func (it *Item) resume(err error) {
	if it.cont == nil {
		it.early, it.earlyErr = true, err
		return
	}
	cont := it.cont
	it.cont = nil
	cont(err)
}

// insertAfter is cmdq_insert_after for a command list: it runs right after the command,
// with the command's state.
func (it *Item) insertAfter(list *List) {
	it.after = append(it.after, afterItem{list: list})
}

// afterHook is cmdq_insert_hook for the commands that fire their after- hook themselves
// (split-window, new-window, new-session, select-pane, select-window): st, what they made or
// selected, is its target.
func (it *Item) afterHook(name string, st target.State) {
	if it.noHooks {
		return
	}
	it.after = append(it.after, afterItem{hook: &Event{Name: name, Target: st, Items: hookArguments(it.Cmd.Args)}})
}

// setCode sets the exit status a command-line client gets (run-shell's job's).
func (it *Item) setCode(code int) {
	if it.rs != nil {
		it.rs.code = code
	}
}

// RunList runs a command list for client c (an attached client, or nil), starting from st as
// the current target: what a key binding runs. done gets its output and errors, which tmux
// shows on the client's message line.
func RunList(h Host, c *Client, list *List, st *target.State, done func(Result)) {
	cur := *st
	newRun(h, c, &cur).start(list, done)
}
