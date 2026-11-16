// bind-key, unbind-key, list-keys and send-prefix (TASKS.md C3.8), and loading tmux's
// default bindings (keydefaults_gen.go), ported from tmux 3.8's cmd-bind-key.c,
// cmd-unbind-key.c, cmd-list-keys.c, key-bindings.c and the prefix half of cmd-send-keys.c:
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
	"strconv"
	"strings"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/cmdlang"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/format"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/keys"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/target"
)

// bindKey is bind-key: a key (its flags ignored) and what it runs. Without -T the table
// is prefix, with -n root; -N gives the binding a note and -r makes it repeat. The
// command is a { … } block, a string parsed as one line, or a command and its arguments.
func bindKey(it *Item) error {
	a := it.Cmd.Args
	if len(a.Values) == 0 {
		return errors.New("missing key")
	}
	keystr := a.Values[0].text()
	key := keys.Parse(keystr)
	if key == keys.None || key == keys.Unknown {
		return fmt.Errorf("unknown key: %s", keystr)
	}
	table := "prefix"
	if name, ok := a.Get('T'); ok {
		table = name
	} else if a.Has('n') {
		table = "root"
	}
	note, hasNote := a.Get('N')
	tbl := it.Host.Tree().Keys.GetOrCreate(table)
	rest := a.Values[1:]
	switch {
	case len(rest) == 0:
		// No command: only the note and the repeat flag of an existing binding change.
		tbl.Add(key, note, hasNote, a.Has('r'), nil)
	case len(rest) == 1 && rest[0].IsBlock:
		tbl.Add(key, note, hasNote, a.Has('r'), rest[0].Commands)
	default:
		list, err := buildBindCommands(rest)
		if err != nil {
			return err
		}
		tbl.Add(key, note, hasNote, a.Has('r'), list)
	}
	return nil
}

// buildBindCommands builds what bind-key binds its key to from the words after the key:
// one string is a command line parsed as a whole (one group, as tmux's
// cmd_parse_from_string does); more are a command and its arguments, as
// cmd_parse_from_arguments does (an argument ending in ";" ends the command there — a
// word list from a shell has none, its ";" ends the bind-key itself). A block argument
// comes only from a config file, where tmux parses it before the command sees it.
func buildBindCommands(vals []Value) (*List, error) {
	if len(vals) == 1 && !vals[0].IsBlock {
		cmds, err := cmdlang.Parse(vals[0].String, cmdlang.Options{})
		if err != nil {
			// A string from the command line has no file or line: tmux's error is bare.
			var pe *cmdlang.Error
			if errors.As(err, &pe) {
				return nil, errors.New(pe.Msg)
			}
			return nil, err
		}
		// One group, even across lines (CMD_PARSE_ONEGROUP).
		for _, c := range cmds {
			c.Line = 1
		}
		return (&Builder{Aliases: DefaultAliases}).Build(cmds)
	}
	cmd, err := ParseValues(vals)
	if err != nil {
		return nil, err
	}
	return &List{Commands: []*Command{cmd}}, nil
}

// unbindKey is unbind-key: it takes the binding for a key out of its table (-n: root,
// -T: that table, else prefix). With -q an error has no message: it stops the command's
// group, but the client does not fail.
func unbindKey(it *Item) error {
	a := it.Cmd.Args
	quiet := a.Has('q')
	if a.Has('a') {
		// Unbinding a whole table is not in i-mux's contract.
		return fmt.Errorf("unbind-key -a: not implemented by imux-core yet")
	}
	quietly := func(err error) error {
		if quiet {
			return errors.New("")
		}
		return err
	}
	if len(a.Values) == 0 {
		return quietly(errors.New("missing key"))
	}
	keystr := a.Values[0].text()
	key := keys.Parse(keystr)
	if key == keys.None || key == keys.Unknown {
		return quietly(fmt.Errorf("unknown key: %s", keystr))
	}
	table := "prefix"
	if name, ok := a.Get('T'); ok {
		table = name
	} else if a.Has('n') {
		table = "root"
	}
	ts := it.Host.Tree().Keys
	// A table given with -T must exist; the default ones need not (key_bindings_remove
	// quietly does nothing without one).
	if a.Has('T') && ts.Get(table) == nil {
		return quietly(fmt.Errorf("table %s doesn't exist", table))
	}
	if t := ts.Get(table); t != nil {
		t.Remove(key)
		ts.RemoveIfEmpty(table)
	}
	return nil
}

// listKeysTemplate is tmux's LIST_KEYS_TEMPLATE.
const listKeysTemplate = "#{?notes_only," +
	"#{key_prefix} " +
	"#{p|#{key_string_width}:key_string} " +
	"#{?key_note,#{key_note},#{key_command}}" +
	"," +
	"bind-key #{?key_has_repeat,#{?key_repeat,-r,  },} " +
	"-T #{p|#{key_table_width}:key_table} " +
	"#{p|#{key_string_width}:#{q|a:key_string}} " +
	"#{key_command}}"

// numBool is tmux's format_add "%d" of a flag.
func numBool(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

// listKeys is list-keys: tmux's bindings, one table with -T, one key as the only
// argument, printed through tmux's own template. i-mux's harness uses -T.
func listKeys(it *Item) error {
	a := it.Cmd.Args
	for _, f := range []byte("1aFNOPr") {
		if a.Has(f) {
			return fmt.Errorf("list-keys -%c: not implemented by imux-core yet", f)
		}
	}
	only := keys.Key(keys.Unknown)
	filter := false
	if len(a.Values) > 0 {
		keystr := a.Values[0].text()
		only = keys.Parse(keystr)
		if only == keys.Unknown {
			return fmt.Errorf("invalid key: %s", keystr)
		}
		only &= keys.MaskKey | keys.MaskModifiers
		filter = true
	}
	ts := it.Host.Tree().Keys
	var list []*keys.Binding
	if name, ok := a.Get('T'); ok {
		t := ts.Get(name)
		if t == nil {
			return fmt.Errorf("table %s doesn't exist", name)
		}
		list = t.Sorted()
	} else {
		for _, t := range ts.Sorted() {
			list = append(list, t.Sorted()...)
		}
	}
	if filter {
		var out []*keys.Binding
		for _, bd := range list {
			if bd.Key&(keys.MaskKey|keys.MaskModifiers) == only {
				out = append(out, bd)
			}
		}
		if len(out) == 0 {
			return fmt.Errorf("unknown key: %s", a.Values[0].text())
		}
		list = out
	}
	// The template's widths and the -r marker are worked out over the listed bindings
	// (cmd_list_keys_get_width and friends).
	hasRepeat, keyWidth, tableWidth := false, 0, 0
	for _, bd := range list {
		if bd.Repeat {
			hasRepeat = true
		}
		if w := format.Width(keys.Name(bd.Key, false)); w > keyWidth {
			keyWidth = w
		}
		if w := format.Width(bd.Table); w > tableWidth {
			tableWidth = w
		}
	}
	prefix := ""
	if k := keys.Key(it.Host.Tree().Options.Session.GetNumber("prefix")); k != keys.None {
		prefix = keys.Name(k, false)
	}
	for _, bd := range list {
		cmd := ""
		if bd.Cmds != nil {
			cmd = bd.Cmds.PrintList(true, true)
		}
		ctx := it.formatContext(nil, nil, nil, nil)
		ctx.Values["notes_only"] = "0"
		ctx.Values["key_has_repeat"] = numBool(hasRepeat)
		ctx.Values["key_string_width"] = strconv.Itoa(keyWidth)
		ctx.Values["key_table_width"] = strconv.Itoa(tableWidth)
		ctx.Values["key_repeat"] = numBool(bd.Repeat)
		ctx.Values["key_note"] = bd.Note
		ctx.Values["key_prefix"] = prefix
		ctx.Values["key_table"] = bd.Table
		ctx.Values["key_string"] = keys.Name(bd.Key, false)
		ctx.Values["key_command"] = cmd
		if line := ctx.Expand(listKeysTemplate); line != "" {
			it.Print(line)
		}
	}
	return nil
}

// sendPrefix is send-prefix: the prefix key (prefix2 with -2, which i-mux does not use),
// sent to the target pane as send-keys would send it.
func sendPrefix(it *Item) error {
	a := it.Cmd.Args
	if a.Has('2') {
		return fmt.Errorf("send-prefix -2: not implemented by imux-core yet")
	}
	st, err := it.findFlag('t', target.Pane, 0)
	if err != nil {
		return err
	}
	name := "prefix"
	key := keys.Key(st.S.Options.GetNumber(name))
	it.Host.PaneKey(st.P, key)
	return nil
}

// LoadDefaultKeyBindings loads tmux's default key bindings (key_bindings_init): each
// default string is parsed and run as a bind-key command, and the tables it filled are
// marked as tmux's own (key_bindings_init_done), so they survive their last binding.
// A default that does not parse or run is a bug in this file: tmux fatalx's on it.
func LoadDefaultKeyBindings(h Host) {
	for _, s := range defaultKeyBindings {
		cmds, err := cmdlang.Parse(s, cmdlang.Options{})
		if err != nil {
			panic("default key binding: " + s + ": " + err.Error())
		}
		b := &Builder{Aliases: DefaultAliases}
		list, err := b.Build(cmds)
		if err != nil {
			panic("default key binding: " + s + ": " + err.Error())
		}
		var out strings.Builder
		if res := runList(h, nil, list, &out); res.Failed {
			panic("default key binding: " + s + ": " + res.Stderr)
		}
	}
	h.Tree().Keys.MarkDefaults()
}
