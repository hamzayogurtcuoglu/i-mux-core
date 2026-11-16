// Ported from tmux 3.8's options.c:
//
// Copyright (c) 2008 Nicholas Marriott <nicholas.marriott@gmail.com>
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

package options

import (
	"errors"
	"fmt"
	"math"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/keys"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/vt"
)

// Type is an option's type (tmux's options_table_type).
type Type int

// Option types.
const (
	TypeString Type = iota
	TypeNumber
	TypeKey
	TypeColour
	TypeFlag
	TypeChoice
	TypeCommand
)

// Scopes (tmux's OPTIONS_TABLE_*).
const (
	ScopeNone    = 0
	ScopeServer  = 0x1
	ScopeSession = 0x2
	ScopeWindow  = 0x4
	ScopePane    = 0x8
)

// Entry flags (tmux's OPTIONS_TABLE_IS_*).
const (
	IsArray  = 0x1
	IsHook   = 0x2
	IsStyle  = 0x4
	IsColour = 0x8
)

// Entry is one option of the table (tmux's options_table_entry).
type Entry struct {
	Name        string
	Alternative string
	Type        Type
	Scope       int
	Flags       int
	Minimum     uint32
	Maximum     uint32
	Choices     []string
	DefaultStr  string
	DefaultNum  int64
	DefaultArr  []string
	// Separator splits an array's value into items; HasSeparator says one is given (an empty
	// one makes the whole value one item, none splits at spaces and commas).
	HasSeparator bool
	Separator    string
	Pattern      string
	Unit         string
}

var tableByName = func() map[string]*Entry {
	m := map[string]*Entry{}
	for i := range Table {
		m[Table[i].Name] = &Table[i]
	}
	return m
}()

// Search finds a table entry by its exact name (options_search).
func Search(name string) *Entry { return tableByName[name] }

// ParseCommand parses a command option's value and returns it as it prints, or the parse error;
// the command language sets it (it cannot be imported from here).
var ParseCommand = func(s string) (string, error) { return s, nil }

// Value is an option's value, or an array item's.
type Value struct {
	Str string // TypeString, TypeCommand (as it prints), user options
	Num int64  // the number types
}

// Option is an option set in one Options (tmux's options_entry).
type Option struct {
	Name  string
	Entry *Entry // nil for a user option
	Owner *Options
	Value Value
	array map[string]Value // for an array
}

// Options is one scope's options with its parent (tmux's struct options).
type Options struct {
	Parent  *Options
	entries map[string]*Option
}

// New returns empty options inheriting from parent.
func New(parent *Options) *Options { return &Options{Parent: parent, entries: map[string]*Option{}} }

func (o *Option) isArray() bool  { return o.Entry != nil && o.Entry.Flags&IsArray != 0 }
func (o *Option) isString() bool { return o.Entry == nil || o.Entry.Type == TypeString }
func (o *Option) isCommand() bool {
	return o.Entry != nil && o.Entry.Type == TypeCommand
}

// IsArray reports whether the option is an array.
func (o *Option) IsArray() bool { return o.isArray() }

// IsString reports whether the option's value is a string (options_is_string).
func (o *Option) IsString() bool { return o.isString() }

// mapName is options_map_name.
func mapName(name string) string {
	if to, ok := OtherNames[name]; ok {
		return to
	}
	return name
}

// GetOnly is options_get_only: the option set in o itself.
func (oo *Options) GetOnly(name string) *Option {
	if o := oo.entries[name]; o != nil {
		return o
	}
	return oo.entries[mapName(name)]
}

// Get is options_get: the option in o or the nearest parent that has it.
func (oo *Options) Get(name string) *Option {
	for ; oo != nil; oo = oo.Parent {
		if o := oo.GetOnly(name); o != nil {
			return o
		}
	}
	return nil
}

// Sorted is the options set in oo, by name (tmux's tree order).
func (oo *Options) Sorted() []*Option {
	out := make([]*Option, 0, len(oo.entries))
	for _, o := range oo.entries {
		out = append(out, o)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (oo *Options) add(name string) *Option {
	o := &Option{Name: name, Owner: oo}
	oo.entries[name] = o
	return o
}

// Empty is options_empty: the option set with no value.
func (oo *Options) Empty(e *Entry) *Option {
	o := oo.add(e.Name)
	o.Entry = e
	if e.Flags&IsArray != 0 {
		o.array = map[string]Value{}
	}
	return o
}

// Default is options_default: the option set to its default.
func (oo *Options) Default(e *Entry) *Option {
	o := oo.Empty(e)
	if e.Flags&IsArray != 0 {
		if e.DefaultArr == nil {
			_ = o.ArrayAssign(e.DefaultStr)
			return o
		}
		for i, v := range e.DefaultArr {
			_ = o.ArraySet(strconv.Itoa(i), &v, false)
		}
		return o
	}
	switch e.Type {
	case TypeString:
		o.Value.Str = e.DefaultStr
	case TypeCommand:
		if s, err := ParseCommand(e.DefaultStr); err == nil {
			o.Value.Str = s
		}
	default:
		o.Value.Num = e.DefaultNum
	}
	return o
}

// DefaultString is options_default_to_string.
func DefaultString(e *Entry) string {
	switch e.Type {
	case TypeString, TypeCommand:
		return e.DefaultStr
	case TypeNumber:
		return strconv.FormatInt(e.DefaultNum, 10)
	case TypeKey:
		return keys.Name(keys.Key(e.DefaultNum), false)
	case TypeColour:
		return vt.ColorToString(vt.Color(e.DefaultNum))
	case TypeFlag:
		if e.DefaultNum != 0 {
			return "on"
		}
		return "off"
	case TypeChoice:
		return e.Choices[e.DefaultNum]
	}
	return ""
}

// Remove takes the option out of its options (options_remove).
func (o *Option) Remove() { delete(o.Owner.entries, o.Name) }

// arrayKeyNumber is options_array_key_to_number: 1 and the index for a number, 0 for another
// key, -1 for a bad one.
func arrayKeyNumber(key string) (int, uint32) {
	if key == "" {
		return -1, 0
	}
	for i := 0; i < len(key); i++ {
		if key[i] < '0' || key[i] > '9' {
			return 0, 0
		}
	}
	n, err := strconv.ParseUint(key, 10, 64)
	if err != nil || n > math.MaxUint32 {
		return -1, 0
	}
	return 1, uint32(n)
}

// correctKey is options_array_correct_key: a number without leading zeros, else the key.
func correctKey(key string) (string, bool) {
	switch kind, n := arrayKeyNumber(key); kind {
	case -1:
		return "", false
	case 1:
		return strconv.FormatUint(uint64(n), 10), true
	}
	return key, true
}

// ArrayKeys are an array's keys in tmux's order: numbers first, by value, then the others.
func (o *Option) ArrayKeys() []string {
	out := make([]string, 0, len(o.array))
	for k := range o.array {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool {
		ni, vi := arrayKeyNumber(out[i])
		nj, vj := arrayKeyNumber(out[j])
		switch {
		case ni == 1 && nj == 1:
			return vi < vj
		case ni == 1:
			return true
		case nj == 1:
			return false
		}
		return out[i] < out[j]
	})
	return out
}

// ArrayGet is options_array_get.
func (o *Option) ArrayGet(key string) (Value, bool) {
	if !o.isArray() {
		return Value{}, false
	}
	k, ok := correctKey(key)
	if !ok {
		return Value{}, false
	}
	v, ok := o.array[k]
	return v, ok
}

// ArrayClear empties an array (options_array_clear).
func (o *Option) ArrayClear() {
	if o.isArray() {
		o.array = map[string]Value{}
	}
}

// ArraySet is options_array_set: a nil value removes the item.
func (o *Option) ArraySet(key string, value *string, appendTo bool) error {
	if !o.isArray() {
		return errors.New("not an array")
	}
	k, ok := correctKey(key)
	if !ok {
		return fmt.Errorf("bad array key: %s", key)
	}
	if value == nil {
		delete(o.array, k)
		return nil
	}
	switch {
	case o.isCommand():
		s, err := ParseCommand(*value)
		if err != nil {
			return err
		}
		o.array[k] = Value{Str: s}
	case o.isString():
		v := *value
		if old, ok := o.array[k]; ok && appendTo {
			v = old.Str + v
		}
		o.array[k] = Value{Str: v}
	case o.Entry.Type == TypeColour:
		c := vt.ColorFromString(*value)
		if c == -1 {
			return fmt.Errorf("bad colour: %s", *value)
		}
		o.array[k] = Value{Num: int64(c)}
	default:
		return errors.New("wrong array type")
	}
	return nil
}

// ArrayAssign is options_array_assign: s split at the separator, each part at the next free
// index.
func (o *Option) ArrayAssign(s string) error {
	sep := o.Entry.Separator
	if !o.Entry.HasSeparator {
		sep = " ,"
	}
	next := func() string {
		for i := uint64(0); ; i++ {
			if _, ok := o.array[strconv.FormatUint(i, 10)]; !ok {
				return strconv.FormatUint(i, 10)
			}
		}
	}
	if s == "" {
		return nil
	}
	if sep == "" {
		return o.ArraySet(next(), &s, false)
	}
	for _, part := range splitAny(s, sep) {
		if part == "" {
			continue
		}
		if err := o.ArraySet(next(), &part, false); err != nil {
			return err
		}
	}
	return nil
}

func splitAny(s, sep string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if strings.IndexByte(sep, s[i]) >= 0 {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	return append(out, s[start:])
}

// valueString is options_value_to_string.
func (o *Option) valueString(v Value, numeric bool) string {
	if o.isCommand() {
		return v.Str
	}
	if o.Entry != nil {
		switch o.Entry.Type {
		case TypeNumber:
			return strconv.FormatInt(v.Num, 10)
		case TypeKey:
			return keys.Name(keys.Key(v.Num), false)
		case TypeColour:
			return vt.ColorToString(vt.Color(v.Num))
		case TypeFlag:
			if numeric {
				return strconv.FormatInt(v.Num, 10)
			}
			if v.Num != 0 {
				return "on"
			}
			return "off"
		case TypeChoice:
			return o.Entry.Choices[v.Num]
		}
	}
	return v.Str
}

// String is options_to_string: an array's items joined by spaces, or the item with key.
func (o *Option) String(key string, hasKey, numeric bool) string {
	if o.isArray() {
		if !hasKey {
			var parts []string
			for _, k := range o.ArrayKeys() {
				parts = append(parts, o.valueString(o.array[k], numeric))
			}
			return strings.Join(parts, " ")
		}
		v, ok := o.ArrayGet(key)
		if !ok {
			return ""
		}
		return o.valueString(v, numeric)
	}
	return o.valueString(o.Value, numeric)
}

// ParseName is options_parse: a name and, for "name[key]", its array key.
func ParseName(name string) (base, key string, hasKey, ok bool) {
	if name == "" {
		return "", "", false, false
	}
	i := strings.IndexByte(name, '[')
	if i < 0 {
		return name, "", false, true
	}
	end := strings.IndexByte(name[i+1:], ']')
	if end < 0 || i+1+end != len(name)-1 || end == 0 {
		return "", "", false, false
	}
	k, good := correctKey(name[i+1 : i+1+end])
	if !good {
		return "", "", false, false
	}
	return name[:i], k, true, true
}

// Match is options_match: the full name of an option given by a unique prefix, or a user
// option; ambiguous when several match.
func Match(s string) (name, key string, hasKey, ambiguous bool) {
	parsed, key, hasKey, ok := ParseName(s)
	if !ok {
		// tmux leaves ambiguous unset here, and its callers then report the name as
		// ambiguous (seen with tmux 3.8: "set -g 'status-format[x'").
		return "", "", false, true
	}
	if strings.HasPrefix(parsed, "@") {
		return parsed, key, hasKey, false
	}
	parsed = mapName(parsed)
	var found *Entry
	for i := range Table {
		e := &Table[i]
		if e.Name == parsed {
			found = e
			break
		}
		if strings.HasPrefix(e.Name, parsed) {
			if found != nil {
				return "", "", false, true
			}
			found = e
		}
	}
	if found == nil {
		return "", "", false, false
	}
	return found.Name, key, hasKey, false
}

// GetString and GetNumber read an option that must exist (options_get_string and
// options_get_number); "" and 0 when it does not.
func (oo *Options) GetString(name string) string {
	if o := oo.Get(name); o != nil {
		return o.Value.Str
	}
	return ""
}

// GetNumber reads a number option.
func (oo *Options) GetNumber(name string) int64 {
	if o := oo.Get(name); o != nil {
		return o.Value.Num
	}
	return 0
}

// parentEntry is options_parent_table_entry.
func (oo *Options) parentEntry(name string) *Entry {
	if oo.Parent != nil {
		if o := oo.Parent.Get(name); o != nil && o.Entry != nil {
			return o.Entry
		}
	}
	return Search(mapName(name))
}

// SetString is options_set_string: with appendTo, the value follows the old one and the
// option's separator.
func (oo *Options) SetString(name string, appendTo bool, value string) *Option {
	o := oo.GetOnly(name)
	if o != nil && appendTo && o.isString() {
		sep := ""
		if !strings.HasPrefix(name, "@") {
			sep = o.Entry.Separator
		}
		value = o.Value.Str + sep + value
	}
	switch {
	case o == nil && strings.HasPrefix(name, "@"):
		o = oo.add(name)
	case o == nil:
		o = oo.Default(oo.parentEntry(name))
	}
	o.Value.Str = value
	return o
}

// SetNumber is options_set_number.
func (oo *Options) SetNumber(name string, value int64) *Option {
	o := oo.GetOnly(name)
	if o == nil {
		o = oo.Default(oo.parentEntry(name))
	}
	o.Value.Num = value
	return o
}

// Checker validates strings options_from_string_check cannot check here: a shell for
// default-shell and styles; nil accepts them.
var Checker struct {
	Shell  func(string) bool
	Style  func(string) bool
	Colour func(string) bool
}

// check is options_from_string_check.
func check(e *Entry, value string) error {
	if e == nil {
		return nil
	}
	if e.Name == "default-shell" && Checker.Shell != nil && !Checker.Shell(value) {
		return fmt.Errorf("not a suitable shell: %s", value)
	}
	if e.Pattern != "" {
		if ok, _ := path.Match(e.Pattern, value); !ok {
			return fmt.Errorf("value is invalid: %s", value)
		}
	}
	if e.Flags&IsStyle != 0 && !strings.Contains(value, "#{") && Checker.Style != nil && !Checker.Style(value) {
		return fmt.Errorf("invalid style: %s", value)
	}
	if e.Flags&IsColour != 0 && !strings.Contains(value, "#{") && Checker.Colour != nil && !Checker.Colour(value) {
		return fmt.Errorf("invalid colour: %s", value)
	}
	return nil
}

// strtonum is tmux's strtonum: the number, or "invalid", "too small" or "too large".
func strtonum(s string, lo, hi int64) (int64, string) {
	n, err := strconv.ParseInt(s, 10, 64)
	switch {
	case err != nil:
		var ne *strconv.NumError
		if errors.As(err, &ne) && errors.Is(ne.Err, strconv.ErrRange) {
			if strings.HasPrefix(s, "-") {
				return 0, "too small"
			}
			return 0, "too large"
		}
		return 0, "invalid"
	case n < lo:
		return 0, "too small"
	case n > hi:
		return 0, "too large"
	}
	return n, ""
}

// FromString is options_from_string: value (nil for none) parsed for the option's type and set
// in oo.
func (oo *Options) FromString(e *Entry, name string, value *string, appendTo bool) error {
	typ := TypeString
	if e != nil {
		if value == nil && e.Type != TypeFlag && e.Type != TypeChoice {
			return errors.New("empty value")
		}
		typ = e.Type
	} else if !strings.HasPrefix(name, "@") {
		return errors.New("bad option name")
	}
	switch typ {
	case TypeString:
		// The old value (inherited if it is not set here) comes back if the new one is bad.
		oldValue := oo.GetString(name)
		oo.SetString(name, appendTo, *value)
		if err := check(e, oo.GetString(name)); err != nil {
			oo.SetString(name, false, oldValue)
			return err
		}
	case TypeNumber:
		n, cause := strtonum(*value, int64(e.Minimum), int64(e.Maximum))
		if cause != "" {
			return fmt.Errorf("value is %s: %s", cause, *value)
		}
		oo.SetNumber(name, n)
	case TypeKey:
		k := keys.Parse(*value)
		if k == keys.Unknown {
			return fmt.Errorf("bad key: %s", *value)
		}
		oo.SetNumber(name, int64(k))
	case TypeColour:
		c := vt.ColorFromString(*value)
		if c == -1 {
			return fmt.Errorf("bad colour: %s", *value)
		}
		oo.SetNumber(name, int64(c))
	case TypeFlag:
		var flag int64
		switch {
		case value == nil || *value == "":
			if oo.GetNumber(name) == 0 {
				flag = 1
			}
		case *value == "1" || strings.EqualFold(*value, "on") || strings.EqualFold(*value, "yes"):
			flag = 1
		case *value == "0" || strings.EqualFold(*value, "off") || strings.EqualFold(*value, "no"):
			flag = 0
		default:
			return fmt.Errorf("bad value: %s", *value)
		}
		oo.SetNumber(name, flag)
	case TypeChoice:
		var choice int64
		if value == nil {
			choice = oo.GetNumber(name)
			if choice < 2 {
				choice = 1 - choice
			}
		} else {
			c, err := FindChoice(e, *value)
			if err != nil {
				return err
			}
			choice = int64(c)
		}
		oo.SetNumber(name, choice)
	case TypeCommand:
		s, err := ParseCommand(*value)
		if err != nil {
			return err
		}
		o := oo.GetOnly(name)
		if o == nil {
			o = oo.Default(oo.parentEntry(name))
		}
		o.Value.Str = s
	}
	return nil
}

// FindChoice is options_find_choice.
func FindChoice(e *Entry, value string) (int, error) {
	choice := -1
	for i, c := range e.Choices {
		if c == value {
			choice = i
		}
	}
	if choice < 0 {
		return -1, fmt.Errorf("unknown value: %s", value)
	}
	return choice, nil
}

// RemoveOrDefault is options_remove_or_default: an option of a global scope goes back to its
// default, another is unset; with a key, the array item goes.
func (o *Option) RemoveOrDefault(key string, hasKey bool, global bool) error {
	if !hasKey {
		if o.Entry != nil && global {
			o.Owner.Default(o.Entry)
		} else {
			o.Remove()
		}
		return nil
	}
	return o.ArraySet(key, nil, false)
}

// Globals are the server's global options: the server's, and the parents of every session's,
// window's and pane's (tmux's global_options, global_s_options and global_w_options).
type Globals struct {
	Server, Session, Window *Options
}

// NewGlobals returns the global options at their defaults (tmux's options_table loop in
// server start).
func NewGlobals() *Globals {
	g := &Globals{Server: New(nil), Session: New(nil), Window: New(nil)}
	for i := range Table {
		e := &Table[i]
		if e.Scope&ScopeServer != 0 {
			g.Server.Default(e)
		}
		if e.Scope&ScopeSession != 0 {
			g.Session.Default(e)
		}
		if e.Scope&(ScopeWindow|ScopePane) != 0 {
			g.Window.Default(e)
		}
	}
	return g
}

// IsGlobal reports whether oo is one of the global options.
func (g *Globals) IsGlobal(oo *Options) bool {
	return oo == g.Server || oo == g.Session || oo == g.Window
}
