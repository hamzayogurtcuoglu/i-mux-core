// Ported from tmux 3.8's paste.c:
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

package buffers

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/vt"
)

// Buffer is a paste buffer (struct paste_buffer).
type Buffer struct {
	Name string
	Data []byte
	// Automatic is a buffer named by the server (bufferN), which buffer-limit counts and the
	// commands without -b use.
	Automatic bool
	Order     uint // when it was added: newer buffers have higher orders
	Created   time.Time
}

// Store is the server's paste buffers. Its methods are called in the server's loop.
type Store struct {
	byName       map[string]*Buffer
	nextIndex    uint
	nextOrder    uint
	numAutomatic int
	// Now is the clock buffers are stamped with; nil is time.Now.
	Now func() time.Time
}

// New returns an empty store.
func New() *Store { return &Store{byName: map[string]*Buffer{}} }

func (s *Store) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// Walk returns the buffers newest first (paste_walk's order).
func (s *Store) Walk() []*Buffer {
	out := make([]*Buffer, 0, len(s.byName))
	for _, b := range s.byName {
		out = append(out, b)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Order > out[j].Order })
	return out
}

// Empty is paste_is_empty.
func (s *Store) Empty() bool { return len(s.byName) == 0 }

// Top is paste_get_top: the newest automatic buffer, or nil.
func (s *Store) Top() *Buffer {
	for _, b := range s.Walk() {
		if b.Automatic {
			return b
		}
	}
	return nil
}

// Get is paste_get_name: the buffer of that name, or nil.
func (s *Store) Get(name string) *Buffer {
	if name == "" {
		return nil
	}
	return s.byName[name]
}

// Free is paste_free: the buffer goes.
func (s *Store) Free(b *Buffer) {
	if s.byName[b.Name] != b {
		return
	}
	delete(s.byName, b.Name)
	if b.Automatic {
		s.numAutomatic--
	}
}

// Add is paste_add: an automatic buffer named prefix (default "buffer") and the next free
// index, after the oldest automatic buffers beyond limit (buffer-limit) go. Empty data adds
// nothing.
func (s *Store) Add(prefix string, data []byte, limit int) *Buffer {
	if prefix == "" {
		prefix = "buffer"
	}
	if len(data) == 0 {
		return nil
	}
	all := s.Walk()
	for i := len(all) - 1; i >= 0; i-- {
		if s.numAutomatic < limit {
			break
		}
		if all[i].Automatic {
			s.Free(all[i])
		}
	}
	var name string
	for {
		name = prefix + strconv.FormatUint(uint64(s.nextIndex), 10)
		s.nextIndex++
		if s.Get(name) == nil {
			break
		}
	}
	b := &Buffer{Name: name, Data: data, Automatic: true, Order: s.nextOrder, Created: s.now()}
	s.nextOrder++
	s.numAutomatic++
	s.byName[name] = b
	return b
}

// Rename is paste_rename: the buffer becomes a named one (no longer automatic), replacing any
// buffer of the new name.
func (s *Store) Rename(oldName, newName string) error {
	if oldName == "" {
		return errors.New("no buffer")
	}
	if newName == "" {
		return errors.New("new name is empty")
	}
	name, ok := vt.CleanName(newName, false)
	if !ok {
		return fmt.Errorf("invalid buffer name: %s", newName)
	}
	b := s.Get(oldName)
	if b == nil {
		return fmt.Errorf("no buffer %s", oldName)
	}
	other := s.Get(name)
	if other == b {
		return nil
	}
	if other != nil {
		s.Free(other)
	}
	delete(s.byName, b.Name)
	b.Name = name
	if b.Automatic {
		s.numAutomatic--
	}
	b.Automatic = false
	s.byName[name] = b
	return nil
}

// Set is paste_set: a buffer of that name holds data, replacing any of the name; with no
// name (nil), an automatic buffer is added. Empty data sets nothing.
func (s *Store) Set(data []byte, name *string, limit int) error {
	if len(data) == 0 {
		return nil
	}
	if name == nil {
		s.Add("", data, limit)
		return nil
	}
	if *name == "" {
		return errors.New("empty buffer name")
	}
	clean, ok := vt.CleanName(*name, false)
	if !ok {
		return fmt.Errorf("invalid buffer name: %s", *name)
	}
	b := &Buffer{Name: clean, Data: data, Order: s.nextOrder, Created: s.now()}
	s.nextOrder++
	if old := s.Get(clean); old != nil {
		s.Free(old)
	}
	s.byName[clean] = b
	return nil
}

// Replace is paste_replace: the buffer holds new data.
func (s *Store) Replace(b *Buffer, data []byte) { b.Data = data }

// sampleWidth is how much of a buffer its sample shows.
const sampleWidth = 200

// Sample is paste_make_sample: the start of the buffer, escaped as utf8_strvis does, with
// "..." when it is cut.
func Sample(b *Buffer) string {
	data := b.Data
	if len(data) > sampleWidth {
		data = data[:sampleWidth]
	}
	out := vt.ArgVis(string(data), false)
	// The C code writes "..." at byte 200 of its buffer, which shows only when the escaped
	// text reaches it.
	if (len(b.Data) > sampleWidth || len(out) > sampleWidth) && len(out) >= sampleWidth {
		out = out[:sampleWidth] + "..."
	}
	return out
}
