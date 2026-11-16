// Hooks (TASKS.md C3.9), ported from tmux 3.8's hooks.c (hooks_insert, hooks_insert_event)
// and cmd-queue.c (cmdq_insert_hook):
//
// Copyright (c) 2012 Thomas Adam <thomas@xteddy.org>
// Copyright (c) 2012 Nicholas Marriott <nicholas.marriott@gmail.com>
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
	"errors"
	"sort"
	"strconv"
	"strings"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/cmdlang"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/model"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/target"
)

// Event is an event hooks can be set for (tmux's event payload): its name, the target its
// hooks run with, and its items, which hook commands see as #{hook_<name>}.
type Event struct {
	Name   string
	Target target.State
	// Client is the client the event is about, by name ("" for none).
	Client string
	// Items are the payload: "pane" is "%1", "window" "@1", "session" "$1", and so on.
	Items map[string]string
	// Names are the names of the payload's sessions and windows (#{hook_session_name}).
	Names map[string]string
}

// PaneEvent is events_fire_pane: an event about a pane, which is its target.
func PaneEvent(name string, p *model.Pane) Event {
	ev := Event{Name: name, Target: PaneState(p), Items: map[string]string{}, Names: map[string]string{}}
	ev.Items["pane"] = "%" + strconv.Itoa(p.ID)
	ev.Items["window"] = "@" + strconv.Itoa(p.Window.ID)
	ev.Names["window"] = p.Window.Name
	return ev
}

// PaneState is cmd_find_from_pane: the pane, its window and the window's session.
func PaneState(p *model.Pane) target.State {
	w := p.Window
	wl := w.Link()
	if wl == nil {
		return target.State{Idx: -1, W: w, P: p}
	}
	return target.State{S: wl.Session, WL: wl, Idx: wl.Index, W: w, P: p}
}

// formats are the event's items as hook formats (event_payload_add_formats with "hook_").
func (ev *Event) formats() map[string]string {
	f := map[string]string{"hook": ev.Name, "event": ev.Name, "hook_event": ev.Name}
	for k, v := range ev.Items {
		f["hook_"+k] = v
	}
	for k, v := range ev.Names {
		f["hook_"+k+"_name"] = v
	}
	return f
}

// validState is cmd_find_valid_state: the session, window and pane are all still there.
func validState(t *model.Tree, st *target.State) bool {
	if st == nil || st.S == nil || st.WL == nil || st.P == nil {
		return false
	}
	if t.Session(st.S.ID) != st.S || st.S.Link(st.WL.Index) != st.WL {
		return false
	}
	return t.Pane(st.P.ID) == st.P && st.P.Window == st.WL.Window
}

// RunHooks is hooks_insert for an event the server fires: each command list set for the
// hook runs, one after another, with the event's target as the current target, no client,
// and #{hook_*} set. Its commands fire no hooks of their own (CMDQ_STATE_NOHOOKS).
func RunHooks(h Host, ev Event) {
	newRun(h, nil, &target.State{Idx: -1}).runHooks(ev, func(Result) {})
}

// hookDepth counts the hook commands running now: an event fired while one runs has no
// hooks (hooks_event_cb checks the running item's CMDQ_STATE_NOHOOKS).
var hookDepth int

// InHook reports whether a hook's command is running now.
func InHook() bool { return hookDepth > 0 }

// runHooks runs the hooks for ev, for rs's client (an after- hook's command client, or nil),
// then done.
func (rs *runState) runHooks(ev Event, done func(Result)) {
	t := rs.h.Tree()
	st := ev.Target
	if !validState(t, &st) {
		// cmd_find_from_nothing: the best session's current pane.
		st = target.State{Idx: -1}
		if s := t.BestSession(); s != nil {
			st = sessionState(s)
		}
	}
	// The hook is looked up in the session's options (the global ones without a session),
	// then the pane's and the window's.
	oo := t.Options.Session
	if st.S != nil {
		oo = st.S.Options
	}
	o := oo.Get(ev.Name)
	if o == nil && st.P != nil {
		o = st.P.Options.Get(ev.Name)
	}
	if o == nil && st.WL != nil {
		o = st.WL.Window.Options.Get(ev.Name)
	}
	var lists []*List
	if o != nil {
		var values []string
		if strings.HasPrefix(ev.Name, "@") {
			values = []string{o.String("", false, false)}
		} else {
			for _, k := range o.ArrayKeys() {
				v, _ := o.ArrayGet(k)
				values = append(values, v.Str)
			}
		}
		for _, v := range values {
			if list, err := parseHook(v); err == nil && list != nil {
				lists = append(lists, list)
			}
		}
	}
	formats := ev.formats()
	var all Result
	var run func(i int)
	run = func(i int) {
		if i == len(lists) {
			done(all)
			return
		}
		cur := st
		hs := &runState{h: rs.h, c: rs.c, out: rs.out, current: &cur, formats: formats, noHooks: true}
		hs.start(lists[i], func(res Result) {
			if res.Failed {
				all.Stderr += res.Stderr
				all.Failed = true
			}
			run(i + 1)
		})
	}
	run(0)
}

// parseHook parses a hook's command list as it was stored.
func parseHook(s string) (*List, error) {
	cmds, err := cmdlang.Parse(s, cmdlang.Options{})
	if err != nil || len(cmds) == 0 {
		return nil, err
	}
	return (&Builder{Aliases: DefaultAliases}).Build(cmds)
}

// afterEvent is cmdq_insert_hook's event for after-<command>: its target is the command's
// target if still there, else the list's current target, else the client's; its items are
// the command's arguments and flags.
func (rs *runState) afterEvent(it *Item) (Event, bool) {
	t := rs.h.Tree()
	var st *target.State
	switch {
	case validState(t, it.Target):
		st = it.Target
	case validState(t, rs.current):
		st = rs.current
	default:
		if cs, err := target.Find(it.targetContext(), "", target.Pane, target.Quiet); err == nil {
			st = &cs
		}
	}
	if st == nil {
		return Event{}, false
	}
	return Event{Name: "after-" + it.Cmd.Spec.Name, Target: *st, Items: hookArguments(it.Cmd.Args)}, true
}

// hookArguments is the payload cmdq_insert_hook gives an after- hook: arguments,
// argument_N, flag_X (its value, or 1) and flag_X_N for each value.
func hookArguments(a *Args) map[string]string {
	items := map[string]string{"arguments": a.Print()}
	for i, v := range a.Strings() {
		items["argument_"+strconv.Itoa(i)] = v
	}
	flags := make([]byte, 0, len(a.flags))
	for f := range a.flags {
		flags = append(flags, f)
	}
	sort.Slice(flags, func(i, j int) bool { return flags[i] < flags[j] })
	for _, f := range flags {
		name := "flag_" + string(f)
		if v, ok := a.Get(f); ok {
			items[name] = v
		} else {
			items[name] = "1"
		}
		for i, v := range a.All(f) {
			items[name+"_"+strconv.Itoa(i)] = v
		}
	}
	return items
}

// setHook is set-hook: set-option for hooks, with -R to run a hook now.
func setHook(it *Item) error {
	if err := notImplemented(it, "BET"); err != nil {
		return err
	}
	a := it.Cmd.Args
	if a.Has('R') {
		args := a.Strings()
		if len(args) == 0 {
			return errors.New("missing argument")
		}
		st, err := optionTarget(it, false)
		if err != nil {
			return err
		}
		// hooks_run: the hook's commands run after set-hook, with its target.
		name := it.formatSingle(st, args[0])
		it.after = append(it.after, afterItem{hook: &Event{Name: name, Target: st}})
		return nil
	}
	if len(a.Strings()) == 0 {
		return errors.New("missing argument")
	}
	return setOption(it)
}
