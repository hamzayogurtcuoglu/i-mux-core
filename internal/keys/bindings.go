// Key tables and what a key runs, ported from tmux 3.8's key-bindings.c:
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

package keys

import "sort"

// ListPrinter is what a binding runs: a command list that prints itself as tmux's
// cmd_list_print (escaped picks the \; separators; noGroups prints one separator between
// all groups). commands.List implements it, so this package needs no dependency on it.
type ListPrinter interface {
	PrintList(escaped, noGroups bool) string
}

// Binding is one key binding (tmux's key_binding): the key without its flags, the note
// bind-key -N gave, whether it repeats (-r), and what it runs.
type Binding struct {
	Key    Key
	Table  string // the table it is in (tmux's tablename)
	Note   string
	Repeat bool
	Cmds   ListPrinter // nil: bind-key with no command
}

// Table is a named table of bindings (tmux's key_table).
type Table struct {
	Name string

	defaults bool             // tmux's default_key_bindings: never removed for being empty
	b        map[Key]*Binding // keyed by the key without its flags
}

// Get returns the binding for key, its flags ignored (key_bindings_get), or nil.
func (t *Table) Get(k Key) *Binding { return t.b[k&^MaskFlags] }

// Add is key_bindings_add. A binding with no command list only sets the note and the
// repeat flag of one that already exists.
func (t *Table) Add(k Key, note string, hasNote, repeat bool, cmds ListPrinter) {
	k &= ^MaskFlags
	bd := t.b[k]
	if cmds == nil {
		if bd == nil {
			return
		}
		if hasNote {
			bd.Note = note
		}
		bd.Repeat = bd.Repeat || repeat
		return
	}
	t.b[k] = &Binding{Key: k, Table: t.Name, Note: note, Repeat: repeat, Cmds: cmds}
}

// Remove takes the binding for key out (key_bindings_remove).
func (t *Table) Remove(k Key) { delete(t.b, k&^MaskFlags) }

// Empty reports whether the table has no bindings left.
func (t *Table) Empty() bool { return len(t.b) == 0 }

// Sorted returns the table's bindings in tmux's order: by key code, the order of its
// tree.
func (t *Table) Sorted() []*Binding {
	out := make([]*Binding, 0, len(t.b))
	for _, bd := range t.b {
		out = append(out, bd)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// Tables are the server's key tables (tmux's key_tables).
type Tables struct {
	m map[string]*Table
}

// NewTables returns no tables.
func NewTables() *Tables { return &Tables{m: map[string]*Table{}} }

// Get returns the table named name, or nil when there is none.
func (ts *Tables) Get(name string) *Table { return ts.m[name] }

// GetOrCreate returns the table named name, making one when there is none
// (key_bindings_get_table with create).
func (ts *Tables) GetOrCreate(name string) *Table {
	if t := ts.m[name]; t != nil {
		return t
	}
	t := &Table{Name: name, b: map[Key]*Binding{}}
	ts.m[name] = t
	return t
}

// MarkDefaults marks the tables tmux's defaults were loaded into (key_bindings_init_done
// copies them into default_key_bindings): such a table is never removed for being empty.
func (ts *Tables) MarkDefaults() {
	for _, t := range ts.m {
		t.defaults = true
	}
}

// Sorted returns the tables in tmux's order: by name, the order of its tree.
func (ts *Tables) Sorted() []*Table {
	out := make([]*Table, 0, len(ts.m))
	for _, t := range ts.m {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// RemoveIfEmpty takes the table named name out once it has no bindings, unless tmux's
// defaults were loaded into it (key_bindings_remove): a table the user made disappears
// with its last binding, one of tmux's own stays.
func (ts *Tables) RemoveIfEmpty(name string) {
	if t := ts.m[name]; t != nil && t.Empty() && !t.defaults {
		delete(ts.m, name)
	}
}
