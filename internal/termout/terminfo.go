package termout

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// A reader of compiled terminfo entries (term(5)), in place of ncurses' setupterm and
// tigetstr (ARCHITECTURE.md E15): the legacy format (magic 0432) and the 32-bit one (01036),
// each with ncurses' extended capabilities (AX, XT, Tc, Smulx, …).

// Terminfo is one terminal's entry.
type Terminfo struct {
	Names   []string // the entry's names, the first being its primary one
	Bools   map[string]bool
	Numbers map[string]int
	Strings map[string]string
}

// Errors from Lookup, with tmux's texts.
var (
	errNoDatabase = errors.New("can't find terminfo database")
)

// SearchDirs returns the directories ncurses searches, in its order: $TERMINFO, ~/.terminfo,
// $TERMINFO_DIRS (an empty element is the system's directories), then the system's.
func SearchDirs(getenv func(string) string) []string {
	system := []string{"/etc/terminfo", "/lib/terminfo", "/usr/share/terminfo", "/usr/lib/terminfo"}
	var dirs []string
	if d := getenv("TERMINFO"); d != "" {
		dirs = append(dirs, d)
	}
	if h := getenv("HOME"); h != "" {
		dirs = append(dirs, filepath.Join(h, ".terminfo"))
	}
	if list := getenv("TERMINFO_DIRS"); list != "" {
		for _, d := range strings.Split(list, ":") {
			if d == "" {
				dirs = append(dirs, system...)
			} else {
				dirs = append(dirs, d)
			}
		}
	}
	return append(dirs, system...)
}

// Lookup finds and reads name's entry in dirs, with tmux's errors.
func Lookup(name string, dirs []string) (*Terminfo, error) {
	if name == "" || strings.ContainsAny(name, "/") || name == "." || name == ".." {
		return nil, fmt.Errorf("missing or unsuitable terminal: %s", name)
	}
	found := false
	for _, d := range dirs {
		if fi, err := os.Stat(d); err != nil || !fi.IsDir() {
			continue
		}
		found = true
		for _, sub := range []string{name[:1], strconv.FormatInt(int64(name[0]), 16)} {
			b, err := os.ReadFile(filepath.Join(d, sub, name))
			if err != nil {
				continue
			}
			ti, err := ParseTerminfo(b)
			if err != nil {
				return nil, fmt.Errorf("missing or unsuitable terminal: %s", name)
			}
			return ti, nil
		}
	}
	if !found {
		return nil, errNoDatabase
	}
	return nil, fmt.Errorf("missing or unsuitable terminal: %s", name)
}

type reader struct {
	b   []byte
	off int
	err error
}

func (r *reader) need(n int) bool {
	if r.err == nil && (n < 0 || r.off+n > len(r.b)) {
		r.err = errors.New("terminfo: truncated entry")
	}
	return r.err == nil
}

func (r *reader) short() int {
	if !r.need(2) {
		return 0
	}
	v := int(int16(binary.LittleEndian.Uint16(r.b[r.off:])))
	r.off += 2
	return v
}

func (r *reader) number(wide bool) int {
	if !wide {
		return r.short()
	}
	if !r.need(4) {
		return 0
	}
	v := int(int32(binary.LittleEndian.Uint32(r.b[r.off:])))
	r.off += 4
	return v
}

func (r *reader) bytes(n int) []byte {
	if !r.need(n) {
		return nil
	}
	b := r.b[r.off : r.off+n]
	r.off += n
	return b
}

func (r *reader) align() {
	if r.off%2 == 1 && r.off < len(r.b) {
		r.off++
	}
}

// cstring is the NUL-terminated string at off in table, or "" and false out of range.
func cstring(table []byte, off int) (string, bool) {
	if off < 0 || off >= len(table) {
		return "", false
	}
	end := off
	for end < len(table) && table[end] != 0 {
		end++
	}
	return string(table[off:end]), true
}

// ParseTerminfo reads a compiled entry.
func ParseTerminfo(b []byte) (*Terminfo, error) {
	r := &reader{b: b}
	magic := r.short()
	var wide bool
	switch magic {
	case 0o432:
	case 0o1036:
		wide = true
	default:
		return nil, errors.New("terminfo: bad magic")
	}
	nameSize, nbools, nnums, nstrs, tableSize := r.short(), r.short(), r.short(), r.short(), r.short()
	if r.err != nil || nameSize < 0 || nbools < 0 || nnums < 0 || nstrs < 0 || tableSize < 0 {
		return nil, errors.New("terminfo: bad header")
	}
	ti := &Terminfo{Bools: map[string]bool{}, Numbers: map[string]int{}, Strings: map[string]string{}}
	names, _ := cstring(r.bytes(nameSize), 0)
	ti.Names = strings.Split(names, "|")
	for i, v := range r.bytes(nbools) {
		if i < len(terminfoBools) && v == 1 {
			ti.Bools[terminfoBools[i]] = true
		}
	}
	r.align()
	for i := range nnums {
		v := r.number(wide)
		if i < len(terminfoNums) && v >= 0 {
			ti.Numbers[terminfoNums[i]] = v
		}
	}
	offsets := make([]int, nstrs)
	for i := range offsets {
		offsets[i] = r.short()
	}
	table := r.bytes(tableSize)
	if r.err != nil {
		return nil, r.err
	}
	for i, off := range offsets {
		if i >= len(terminfoStrings) || off < 0 {
			continue
		}
		if s, ok := cstring(table, off); ok {
			ti.Strings[terminfoStrings[i]] = s
		}
	}

	// The extended capabilities, if any follow.
	r.align()
	if r.off+10 > len(r.b) {
		return ti, nil
	}
	ebools, enums, estrs, _, etableSize := r.short(), r.short(), r.short(), r.short(), r.short()
	if r.err != nil || ebools < 0 || enums < 0 || estrs < 0 || etableSize < 0 {
		return ti, nil
	}
	boolValues := r.bytes(ebools)
	r.align()
	numValues := make([]int, enums)
	for i := range numValues {
		numValues[i] = r.number(wide)
	}
	strOffsets := make([]int, estrs)
	for i := range strOffsets {
		strOffsets[i] = r.short()
	}
	nameOffsets := make([]int, ebools+enums+estrs)
	for i := range nameOffsets {
		nameOffsets[i] = r.short()
	}
	etable := r.bytes(etableSize)
	if r.err != nil {
		return ti, nil // a damaged extension is ignored, as ncurses does
	}
	// The names follow the last string value in the table.
	namesBase := 0
	for _, off := range strOffsets {
		if s, ok := cstring(etable, off); ok && off+len(s)+1 > namesBase {
			namesBase = off + len(s) + 1
		}
	}
	name := func(i int) string {
		s, _ := cstring(etable, namesBase+nameOffsets[i])
		return s
	}
	for i, v := range boolValues {
		// An extended flag the entry defines is known even when false; one it does not
		// define is not (tigetflag gives -1 and tmux leaves it out).
		ti.Bools[name(i)] = v == 1
	}
	for i, v := range numValues {
		if v >= 0 {
			ti.Numbers[name(ebools+i)] = v
		}
	}
	for i, off := range strOffsets {
		if off < 0 {
			continue
		}
		if s, ok := cstring(etable, off); ok {
			ti.Strings[name(ebools+enums+i)] = s
		}
	}
	return ti, nil
}

// Capabilities lists the capabilities tmux reads from an entry, as "name=value" strings in
// tmux's order (tty_term_read_list): strings and numbers when present, every flag as 0 or 1.
func (ti *Terminfo) Capabilities() []string {
	var out []string
	for _, c := range codes {
		switch c.typ {
		case codeString:
			if s, ok := ti.Strings[c.name]; ok {
				out = append(out, c.name+"="+s)
			}
		case codeNumber:
			if n, ok := ti.Numbers[c.name]; ok {
				out = append(out, c.name+"="+strconv.Itoa(n))
			}
		case codeFlag:
			on, known := ti.Bools[c.name]
			if !known && !isStandardBool(c.name) {
				continue
			}
			v := "0"
			if on {
				v = "1"
			}
			out = append(out, c.name+"="+v)
		}
	}
	return out
}

func isStandardBool(name string) bool {
	for _, b := range terminfoBools {
		if b == name {
			return true
		}
	}
	return false
}
