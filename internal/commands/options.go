// Ported from tmux 3.8's cmd-set-option.c, cmd-show-options.c and the scope lookups of
// options.c:
//
// Copyright (c) 2007 Nicholas Marriott <nicholas.marriott@gmail.com>
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
	"fmt"
	"strings"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/cmdlang"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/options"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/target"
)

// set-option and show-options (TASKS.md C3.4; -F with the formats of C3.5).

// scopeFromFlags is options_scope_from_flags: the options -s, -p, -w (or a window command),
// -g and -t name.
func scopeFromFlags(it *Item, window bool, st target.State) (*options.Options, int, error) {
	a := it.Cmd.Args
	g := it.Host.Tree().Options
	tflag, hasT := a.Get('t')
	no := func(what, def string) (*options.Options, int, error) {
		if hasT {
			return nil, options.ScopeNone, fmt.Errorf("no such %s: %s", what, tflag)
		}
		return nil, options.ScopeNone, errors.New(def)
	}
	switch {
	case a.Has('s'):
		return g.Server, options.ScopeServer, nil
	case a.Has('p'):
		if st.P == nil {
			return no("pane", "no current pane")
		}
		return st.P.Options, options.ScopePane, nil
	case window || a.Has('w'):
		if a.Has('g') {
			return g.Window, options.ScopeWindow, nil
		}
		if st.WL == nil {
			return no("window", "no current window")
		}
		return st.WL.Window.Options, options.ScopeWindow, nil
	}
	if a.Has('g') {
		return g.Session, options.ScopeSession, nil
	}
	if st.S == nil {
		return no("session", "no current session")
	}
	return st.S.Options, options.ScopeSession, nil
}

// scopeFromName is options_scope_from_name: the options an option of the table is set in, by
// its scope.
func scopeFromName(it *Item, window bool, name string, st target.State) (*options.Options, int, error) {
	if strings.HasPrefix(name, "@") {
		return scopeFromFlags(it, window, st)
	}
	e := options.Search(name)
	if e == nil {
		return nil, options.ScopeNone, fmt.Errorf("unknown option: %s", name)
	}
	a := it.Cmd.Args
	g := it.Host.Tree().Options
	tflag, hasT := a.Get('t')
	no := func(what, def string) (*options.Options, int, error) {
		if hasT {
			return nil, options.ScopeNone, fmt.Errorf("no such %s: %s", what, tflag)
		}
		return nil, options.ScopeNone, errors.New(def)
	}
	switch e.Scope {
	case options.ScopeServer:
		return g.Server, options.ScopeServer, nil
	case options.ScopeSession:
		switch {
		case a.Has('g'):
			return g.Session, options.ScopeSession, nil
		case st.S == nil:
			return no("session", "no current session")
		}
		return st.S.Options, options.ScopeSession, nil
	case options.ScopeWindow | options.ScopePane:
		if a.Has('p') {
			if st.P == nil {
				return no("pane", "no current pane")
			}
			return st.P.Options, options.ScopePane, nil
		}
		fallthrough
	case options.ScopeWindow:
		switch {
		case a.Has('g'):
			return g.Window, options.ScopeWindow, nil
		case st.WL == nil:
			return no("window", "no current window")
		}
		return st.WL.Window.Options, options.ScopeWindow, nil
	}
	return nil, options.ScopeNone, fmt.Errorf("unknown option: %s", name)
}

// optionTarget is the target of set-option and show-options: a pane (a window for the window
// commands), which may not be found.
func optionTarget(it *Item, window bool) (target.State, error) {
	typ := target.Pane
	if window {
		typ = target.Window
	}
	return it.findFlag('t', typ, target.CanFail)
}

// setOption is set-option and set-window-option (cmd_set_option_exec).
func setOption(it *Item) error {
	a := it.Cmd.Args
	window := it.Cmd.Spec.Name == "set-window-option"
	appendTo := a.Has('a')
	st, err := optionTarget(it, window)
	if err != nil {
		return err
	}
	args := a.Strings()
	argument := args[0]
	name, key, hasKey, ambiguous := options.Match(argument)
	if name == "" {
		if a.Has('q') {
			return nil
		}
		if ambiguous {
			return fmt.Errorf("ambiguous option: %s", argument)
		}
		return fmt.Errorf("invalid option: %s", argument)
	}
	var value *string
	if len(args) >= 2 {
		value = &args[1]
		if a.Has('F') {
			v := it.formatSingle(st, args[1])
			value = &v
		}
	}
	oo, scope, err := scopeFromName(it, window, name, st)
	if scope == options.ScopeNone {
		if a.Has('q') {
			return nil
		}
		return err
	}
	o := oo.GetOnly(name)
	parent := oo.Get(name)
	if hasKey && (strings.HasPrefix(name, "@") || parent == nil || !parent.IsArray()) {
		return fmt.Errorf("not an array: %s", argument)
	}
	if !a.Has('u') && a.Has('o') {
		already := o != nil
		if hasKey && o != nil {
			_, already = o.ArrayGet(key)
		}
		if already {
			if a.Has('q') {
				return nil
			}
			return fmt.Errorf("already set: %s", argument)
		}
	}
	global := it.Host.Tree().Options.IsGlobal
	if a.Has('U') && scope == options.ScopeWindow && st.W != nil {
		for _, p := range st.W.Panes {
			if po := p.Options.GetOnly(name); po != nil {
				if err := po.RemoveOrDefault(key, hasKey, global(po.Owner)); err != nil {
					return err
				}
			}
		}
	}
	switch {
	case a.Has('u') || a.Has('U'):
		if o == nil {
			return nil
		}
		if err := o.RemoveOrDefault(key, hasKey, global(oo)); err != nil {
			return err
		}
	case strings.HasPrefix(name, "@"):
		if value == nil {
			return errors.New("empty value")
		}
		oo.SetString(name, appendTo, *value)
	case !hasKey && (parent == nil || !parent.IsArray()):
		e := parent.Entry
		if err := oo.FromString(e, e.Name, value, appendTo); err != nil {
			return err
		}
	default:
		if value == nil {
			return errors.New("empty value")
		}
		if o == nil {
			o = oo.Empty(parent.Entry)
		}
		if !hasKey {
			if !appendTo {
				o.ArrayClear()
			}
			if err := o.ArrayAssign(*value); err != nil {
				return err
			}
		} else if err := o.ArraySet(key, value, appendTo); err != nil {
			return err
		}
	}
	it.Host.OptionChanged(name)
	return nil
}

// showOptions is show-options and show-window-options (cmd_show_options_exec).
func showOptions(it *Item) error {
	a := it.Cmd.Args
	window := it.Cmd.Spec.Name == "show-window-options"
	st, err := optionTarget(it, window)
	if err != nil {
		return err
	}
	args := a.Strings()
	if len(args) == 0 {
		oo, scope, err := scopeFromFlags(it, window, st)
		if scope == options.ScopeNone {
			if a.Has('q') {
				return nil
			}
			return err
		}
		showAll(it, st, scope, oo)
		return nil
	}
	argument := args[0]
	name, key, hasKey, ambiguous := options.Match(argument)
	if name == "" {
		if a.Has('q') {
			return nil
		}
		if ambiguous {
			return fmt.Errorf("ambiguous option: %s", argument)
		}
		return fmt.Errorf("invalid option: %s", argument)
	}
	oo, scope, err := scopeFromName(it, window, name, st)
	if scope == options.ScopeNone {
		if a.Has('q') {
			return nil
		}
		return err
	}
	o := oo.GetOnly(name)
	parent := false
	if a.Has('A') && o == nil {
		o, parent = oo.Get(name), true
	}
	switch {
	case o != nil:
		printParent := parent
		if !hasKey && o.IsArray() && len(o.ArrayKeys()) == 0 {
			printParent = false
		}
		showPrint(it, st, o, key, hasKey, printParent)
	case strings.HasPrefix(name, "@"):
		if a.Has('q') {
			return nil
		}
		return fmt.Errorf("invalid option: %s", argument)
	}
	return nil
}

// showOptionsTemplate is tmux's SHOW_OPTIONS_TEMPLATE.
const showOptionsTemplate = "#{?option_value_only,#{option_value},#{option_name}#{?option_has_array_key,[#{option_array_key}],}#{?option_is_parent,*,}#{?option_has_value, #{?option_is_string,#{q/a:option_value},#{option_value}},}}"

// showPrint is cmd_show_options_print: one option (each item of an array) through the -F
// template or tmux's.
func showPrint(it *Item, st target.State, o *options.Option, key string, hasKey, parent bool) {
	a := it.Cmd.Args
	template, hasTemplate := a.Get('F')
	value := ""
	hasValue := true
	switch {
	case hasKey:
		value = o.String(key, true, false)
	case o.IsArray():
		if keys := o.ArrayKeys(); len(keys) > 0 {
			for _, k := range keys {
				showPrint(it, st, o, k, true, parent)
			}
			return
		}
		if !hasTemplate && a.Has('v') {
			return
		}
		hasValue = false
	default:
		value = o.String("", false, false)
	}
	if !hasTemplate {
		template = showOptionsTemplate
	}
	bit := func(v bool) string {
		if v {
			return "1"
		}
		return "0"
	}
	ctx := it.formatContext(it.clientInfoOf(it.targetClient()), st.S, st.WL, st.P)
	ctx.Values = map[string]string{
		"option_name":          o.Name,
		"option_value":         value,
		"option_value_only":    bit(a.Has('v')),
		"option_is_parent":     bit(parent),
		"option_is_array":      bit(o.IsArray()),
		"option_is_string":     bit(o.IsString()),
		"option_is_hook":       bit(o.Entry != nil && o.Entry.Flags&options.IsHook != 0),
		"option_is_user":       bit(o.Entry == nil),
		"option_has_value":     bit(hasValue),
		"option_array_key":     "",
		"option_has_array_key": "0",
	}
	if hasKey {
		ctx.Values["option_array_key"] = key
		ctx.Values["option_has_array_key"] = "1"
	}
	it.Print(ctx.Expand(template))
}

// showAll is cmd_show_options_all: the user options set, then the table's in the scope (with
// -A the inherited ones too, marked *); hooks only with -H.
func showAll(it *Item, st target.State, scope int, oo *options.Options) {
	a := it.Cmd.Args
	for _, o := range oo.Sorted() {
		if o.Entry == nil {
			showPrint(it, st, o, "", false, false)
		}
	}
	for i := range options.Table {
		e := &options.Table[i]
		if e.Scope&scope == 0 {
			continue
		}
		if !a.Has('H') && e.Flags&options.IsHook != 0 {
			continue
		}
		o := oo.GetOnly(e.Name)
		parent := false
		if o == nil {
			if !a.Has('A') {
				continue
			}
			if o = oo.Get(e.Name); o == nil {
				continue
			}
			parent = true
		}
		showPrint(it, st, o, "", false, parent)
	}
}

// The option table checks what commands and shells it is given here, where the command
// language is.
func init() {
	options.ParseCommand = func(s string) (string, error) {
		cmds, err := cmdlang.Parse(s, cmdlang.Options{})
		if err != nil {
			var pe *cmdlang.Error
			if errors.As(err, &pe) {
				return "", errors.New(pe.Msg)
			}
			return "", err
		}
		b := &Builder{Aliases: DefaultAliases}
		list, err := b.Build(cmds)
		if err != nil {
			return "", err
		}
		return list.Print(), nil
	}
	options.Checker.Shell = CheckShell
}
