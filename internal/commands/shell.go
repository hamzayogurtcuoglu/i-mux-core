// run-shell and if-shell (TASKS.md C3.10), ported from tmux 3.8's cmd-run-shell.c and
// cmd-if-shell.c, with job.c's environment, plus the argv form of COMPATIBILITY.md X1:
//
// Copyright (c) 2009 Tiago Cunha <me@tiagocunha.org>
// Copyright (c) 2009 Nicholas Marriott <nicholas.marriott@gmail.com>
//
// Permission to use, copy, modify, and distribute this software for any
// purpose with or without fee is hereby granted, provided that the above
// copyright notice and this permission notice appear in all copies.
//
// THE SOFTWARE IS PROVIDED "AS IS" AND THE AUTHOR DISCLAIMS ALL WARRANTIES
// WITH REGARD TO THIS SOFTWARE INCLUDING ALL IMPLIED WARRANTIES OF
// MERCHANTABILITY AND FITNESS. IN NO EVENT SHALL THE AUTHOR BE LIABLE FOR
// ANY SPECIAL, DIRECT, INDIRECT, OR CONSEQUENTIAL DAMAGES OR ANY DAMAGES
// WHATSOEVER RESULTING FROM LOSS OF MIND, USE, DATA OR PROFITS, WHETHER
// IN AN ACTION OF CONTRACT, NEGLIGENCE OR OTHER TORTIOUS ACTION, ARISING
// OUT OF OR IN CONNECTION WITH THE USE OR PERFORMANCE OF THIS SOFTWARE.

package commands

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/cmdlang"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/model"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/pty"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/target"
)

// argvForm is the argv form of X1: the command's text is a JSON array of strings, or an
// object with "argv" and "quiet". ok is false for an ordinary shell command.
func argvForm(s string) (argv []string, quiet, ok bool) {
	t := strings.TrimLeft(s, " ")
	switch {
	case strings.HasPrefix(t, `["`):
		if json.Unmarshal([]byte(t), &argv) == nil {
			return argv, false, true
		}
	case strings.HasPrefix(t, "{"):
		var o struct {
			Argv  []string `json:"argv"`
			Quiet bool     `json:"quiet"`
		}
		if json.Unmarshal([]byte(t), &o) == nil && o.Argv != nil {
			return o.Argv, o.Quiet, true
		}
	}
	return nil, false, false
}

// jobEnv is a job's environment (job_run): environ_for_session, without TERM and its friends
// while the config loads, so if-shell can look at the outside TERM, and PWD.
func (it *Item) jobEnv(s *model.Session, dir string) []string {
	env := it.forSession(s, it.configLoading())
	if dir != "" {
		env.set("PWD", dir)
	}
	return env.list()
}

// configLoading is !cfg_finished: the run is a config file's.
func (it *Item) configLoading() bool { return it.rs != nil && it.rs.config }

// jobDir is the job's directory: tmux's child goes to it, else home, else /.
func (it *Item) jobDir(dir string) string {
	if isDir(dir) {
		return dir
	}
	if h := it.getenv("HOME"); h != "" && isDir(h) {
		return h
	}
	return "/"
}

// jobSpec is the job for a command line: the argv form, or the job shell's (JobShell).
func (it *Item) jobSpec(cmd string, argv []string, s *model.Session, dir string, stderr bool) JobSpec {
	dir = it.jobDir(dir)
	spec := JobSpec{Dir: dir, Env: it.jobEnv(s, dir), ShowStderr: stderr}
	if argv != nil {
		spec.Argv = argv
	} else {
		spec.Shell, spec.Command = JobShell(), cmd
	}
	return spec
}

// runShell is run-shell: a shell command (or with -C tmux commands) run after -d seconds,
// its output printed; without -b the command list waits for it.
func runShell(it *Item) error {
	a := it.Cmd.Args
	if err := notImplemented(it, "s"); err != nil {
		return err
	}
	st, err := it.findFlag('t', target.Pane, target.CanFail)
	if err != nil {
		return err
	}
	var delay time.Duration
	d, hasDelay := a.Get('d')
	if hasDelay {
		secs, ok := cStrtod(d)
		if !ok {
			return fmt.Errorf("invalid delay time: %s", d)
		}
		delay = time.Duration(secs * float64(time.Second))
	} else if len(a.Values) == 0 {
		return nil
	}
	wait := !a.Has('b')
	toPane := a.Has('t') && st.P != nil
	dir := ""
	if c, ok := a.Get('c'); ok {
		dir = it.formatSingle(st, c)
	} else {
		dir = it.cwdFor(st.S)
	}

	// What runs when the delay is over.
	var run func()
	switch {
	case a.Has('C'):
		var list *List
		var perr error
		if len(a.Values) > 0 {
			list, perr = it.commandsArg(0, true, st)
		}
		run = func() {
			if perr != nil {
				it.jobDone(wait, perr)
				return
			}
			if list == nil {
				it.jobDone(wait, nil)
				return
			}
			if wait {
				it.insertAfter(list)
				it.resume(nil)
				return
			}
			newRun(it.Host, it.Client, &target.State{Idx: -1}).start(list, func(Result) {})
		}
	case len(a.Values) == 0:
		run = func() { it.jobDone(wait, nil) }
	default:
		raw := a.Values[0].text()
		ctx := it.formatContext(it.clientInfoOf(it.targetClient()), st.S, st.WL, st.P)
		for i, v := range a.Strings()[1:] {
			ctx.Values[strconv.Itoa(i+1)] = v
		}
		argv, quiet, isArgv := argvForm(raw)
		cmd := ""
		if isArgv {
			for i := range argv {
				argv[i] = ctx.Expand(argv[i])
			}
			cmd = strings.Join(argv, " ")
		} else {
			cmd = ctx.Expand(raw)
		}
		spec := it.jobSpec(cmd, argv, st.S, dir, a.Has('E'))
		run = func() {
			it.Host.RunJob(spec, func(out []byte, status pty.ExitStatus, err error) {
				if err != nil {
					it.jobPrint(wait, toPane, "failed to run command: "+cmd)
					it.jobDone(wait, nil)
					return
				}
				if !quiet {
					it.runShellOutput(cmd, out, status, wait, toPane)
				}
				it.jobDone(wait, nil)
			})
		}
	}
	if hasDelay {
		it.Host.After(delay, run)
	} else {
		run()
	}
	if !wait {
		return nil
	}
	return it.wait()
}

// jobDone continues the list after a waiting job.
func (it *Item) jobDone(wait bool, err error) {
	if wait {
		it.resume(err)
	}
}

// runShellOutput is cmd_run_shell_callback: each line of the output printed, then how the
// job ended if it failed; a command-line client's exit status is the job's.
func (it *Item) runShellOutput(cmd string, out []byte, status pty.ExitStatus, wait, toPane bool) {
	text := string(out)
	for text != "" {
		line, rest, found := strings.Cut(text, "\n")
		it.jobPrint(wait, toPane, line)
		if !found {
			break
		}
		text = rest
	}
	code := 0
	switch {
	case status.Signal != 0:
		it.jobPrint(wait, toPane, fmt.Sprintf("'%s' terminated by signal %d", cmd, status.Signal))
		code = status.Signal + 128
	case status.Code != 0:
		it.jobPrint(wait, toPane, fmt.Sprintf("'%s' returned %d", cmd, status.Code))
		code = status.Code
	}
	if wait && it.Client != nil && it.Client.Session == nil {
		it.setCode(code)
	}
}

// jobPrint is cmd_run_shell_print: with the list waiting and no -t, a command-line client
// gets the line on its stdout; otherwise it goes to a pane's view mode, which comes with
// copy mode (task C4.8).
func (it *Item) jobPrint(wait, toPane bool, line string) {
	if wait && !toPane && it.Client != nil && it.Client.Session == nil {
		it.Print(line)
	}
}

// commandsArg is args_make_commands for argument idx: a { … } block, or a string parsed as a
// command line (expanded as a format first with expand).
func (it *Item) commandsArg(idx int, expand bool, st target.State) (*List, error) {
	v := it.Cmd.Args.Values[idx]
	if v.IsBlock {
		return v.Commands, nil
	}
	s := v.String
	if expand {
		s = it.formatSingle(st, s)
	}
	cmds, err := cmdlang.Parse(s, cmdlang.Options{})
	if err != nil {
		var pe *cmdlang.Error
		if errors.As(err, &pe) {
			return nil, errors.New(pe.Msg)
		}
		return nil, err
	}
	if len(cmds) == 0 {
		return nil, nil
	}
	return (&Builder{Aliases: DefaultAliases}).Build(cmds)
}

// ifShell is if-shell: the first command list if the shell command succeeds (with -F, if the
// format is true), else the second; without -b the list waits for the shell command.
func ifShell(it *Item) error {
	a := it.Cmd.Args
	st, err := it.findFlag('t', target.Pane, target.CanFail)
	if err != nil {
		return err
	}
	count := len(a.Values)
	if a.Has('F') {
		shellcmd := it.formatSingle(st, a.Values[0].text())
		idx := 1
		if shellcmd == "" || shellcmd[0] == '0' {
			if count < 3 {
				return nil
			}
			idx = 2
		}
		list, err := it.commandsArg(idx, false, st)
		if err != nil {
			return err
		}
		if list != nil {
			it.insertAfter(list)
		}
		return nil
	}
	raw := a.Values[0].text()
	wait := !a.Has('b')
	ctx := it.formatContext(it.clientInfoOf(it.targetClient()), st.S, st.WL, st.P)
	argv, _, isArgv := argvForm(raw)
	shellcmd := ""
	if isArgv {
		for i := range argv {
			argv[i] = ctx.Expand(argv[i])
		}
		shellcmd = strings.Join(argv, " ")
	} else {
		shellcmd = ctx.Expand(raw)
	}
	spec := it.jobSpec(shellcmd, argv, st.S, it.cwdFor(st.S), false)
	it.Host.RunJob(spec, func(_ []byte, status pty.ExitStatus, err error) {
		if err != nil {
			it.jobDone(wait, fmt.Errorf("failed to run command: %s", shellcmd))
			return
		}
		idx := 1
		if status.Signal != 0 || status.Code != 0 {
			idx = 2
		}
		if idx >= count {
			it.jobDone(wait, nil)
			return
		}
		list, perr := it.commandsArg(idx, false, st)
		switch {
		case perr != nil:
			it.jobDone(wait, perr)
		case list == nil:
			it.jobDone(wait, nil)
		case wait:
			it.insertAfter(list)
			it.resume(nil)
		default:
			newRun(it.Host, it.Client, &target.State{Idx: -1}).start(list, func(Result) {})
		}
	})
	if !wait {
		return nil
	}
	return it.wait()
}

// cStrtod is strtod(3) needing the whole string, for run-shell -d.
func cStrtod(s string) (float64, bool) {
	t := strings.TrimLeft(s, " \t\n\v\f\r")
	if t == "" {
		return 0, s == ""
	}
	f, err := strconv.ParseFloat(t, 64)
	if err != nil {
		var ne *strconv.NumError
		if !errors.As(err, &ne) || ne.Err != strconv.ErrRange {
			return 0, false
		}
	}
	return f, true
}
