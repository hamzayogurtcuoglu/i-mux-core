// Ported in part from tmux 3.8's cmd-set-buffer.c, cmd-load-buffer.c, cmd-paste-buffer.c,
// cmd-save-buffer.c and cmd-list-buffers.c:
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
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/buffers"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/format"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/model"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/target"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/vt"
)

// bufferLimit is the buffer-limit option.
func (it *Item) bufferLimit() int {
	return int(it.Host.Tree().Options.Server.GetNumber("buffer-limit"))
}

// clientFlag is a CMD_CLIENT_TFLAG|CMD_CLIENT_CANFAIL client: the one -t names (or the
// current one), nil when there is none.
func (it *Item) clientFlag(f byte) *target.Client {
	c, err := it.findClient(f)
	if err != nil {
		return nil
	}
	return c
}

// setBuffer is set-buffer and delete-buffer: a buffer's data set (-a appended), renamed
// (-n) or, for delete-buffer, the buffer deleted; -w also gives the data to the client's
// terminal clipboard.
func setBuffer(it *Item) error {
	a := it.Cmd.Args
	store := it.Host.Buffers()
	var name *string
	var pb *buffers.Buffer
	if v, ok := a.Get('b'); ok {
		name = &v
		pb = store.Get(v)
	}
	top := func() error {
		if pb == nil {
			if name != nil {
				return fmt.Errorf("unknown buffer: %s", *name)
			}
			if pb = store.Top(); pb != nil {
				n := pb.Name
				name = &n
			}
		}
		if pb == nil {
			return errors.New("no buffer")
		}
		return nil
	}

	if it.Cmd.Spec.Name == "delete-buffer" {
		if err := top(); err != nil {
			return err
		}
		store.Free(pb)
		return nil
	}
	if v, ok := a.Get('n'); ok {
		if err := top(); err != nil {
			return err
		}
		return store.Rename(*name, v)
	}
	args := a.Strings()
	if len(args) != 1 {
		return errors.New("no data specified")
	}
	if args[0] == "" {
		return nil
	}
	var data []byte
	if a.Has('a') && pb != nil {
		data = append(data, pb.Data...)
	}
	data = append(data, args[0]...)
	if err := store.Set(data, name, it.bufferLimit()); err != nil {
		return err
	}
	if a.Has('w') {
		if tc := it.clientFlag('t'); tc != nil {
			it.Host.SetSelection(tc.Name, data)
		}
	}
	return nil
}

// clientPath is file_get_path: a path relative to the client's directory.
func (it *Item) clientPath(path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(it.cwdFor(it.currentSession()), path)
}

// currentSession is the command client's session, if it is attached.
func (it *Item) currentSession() *model.Session {
	if it.Client != nil {
		return it.Client.Session
	}
	return nil
}

// loadBuffer is load-buffer: a file's contents into a buffer; -w also gives them to the
// client's terminal clipboard.
func loadBuffer(it *Item) error {
	a := it.Cmd.Args
	st, _ := it.findFlag('t', target.Pane, target.CanFail)
	path := it.formatSingle(st, a.Strings()[0])
	if path == "-" {
		return errors.New("load-buffer -: not implemented by imux-core yet")
	}
	full := it.clientPath(path)
	data, err := os.ReadFile(full)
	if err != nil {
		return fmt.Errorf("%s: %s", strerror(err), full)
	}
	if len(data) == 0 {
		return nil
	}
	var name *string
	if v, ok := a.Get('b'); ok {
		name = &v
	}
	if err := it.Host.Buffers().Set(data, name, it.bufferLimit()); err != nil {
		return err
	}
	if a.Has('w') {
		if tc := it.clientFlag('t'); tc != nil && tc.Session != nil {
			it.Host.SetSelection(tc.Name, data)
		}
	}
	return nil
}

// pasteBuffer is paste-buffer: a buffer written to a pane as if typed, its line feeds as
// carriage returns (-r keeps them, -s gives the separator), wrapped in bracketed paste with
// -p when the pane asked for it; -d deletes the buffer.
func pasteBuffer(it *Item) error {
	a := it.Cmd.Args
	st, err := it.findFlag('t', target.Pane, 0)
	if err != nil {
		return err
	}
	p := st.P
	if p.Dead {
		return errors.New("target pane has exited")
	}
	store := it.Host.Buffers()
	var pb *buffers.Buffer
	if v, ok := a.Get('b'); ok {
		if pb = store.Get(v); pb == nil {
			return fmt.Errorf("no buffer %s", v)
		}
	} else {
		pb = store.Top()
	}
	if pb != nil {
		sep, ok := a.Get('s')
		if !ok {
			sep = "\r"
			if a.Has('r') {
				sep = "\n"
			}
		}
		bracket := false
		if a.Has('p') && p.Term != nil {
			p.Mu.Lock()
			bracket = p.Term.Screen.Mode&vt.ModeBracketPaste != 0
			p.Mu.Unlock()
		}
		var out bytes.Buffer
		if bracket {
			out.WriteString("\033[200~")
		}
		data := pb.Data
		write := func(line []byte) {
			if a.Has('S') {
				out.Write(line)
			} else {
				out.WriteString(vt.PasteVis(string(line)))
			}
		}
		for {
			i := bytes.IndexByte(data, '\n')
			if i < 0 {
				break
			}
			write(data[:i])
			out.WriteString(sep)
			data = data[i+1:]
		}
		if len(data) > 0 {
			write(data)
		}
		if bracket {
			out.WriteString("\033[201~")
		}
		it.Host.PaneWrite(p, out.Bytes())
	}
	if pb != nil && a.Has('d') {
		store.Free(pb)
	}
	return nil
}

// saveBuffer is save-buffer and show-buffer: a buffer written to a file (-a appended), or for
// show-buffer printed.
func saveBuffer(it *Item) error {
	a := it.Cmd.Args
	store := it.Host.Buffers()
	var pb *buffers.Buffer
	if v, ok := a.Get('b'); ok {
		if pb = store.Get(v); pb == nil {
			return fmt.Errorf("no buffer %s", v)
		}
	} else if pb = store.Top(); pb == nil {
		return errors.New("no buffers")
	}
	path := "-"
	if it.Cmd.Spec.Name == "show-buffer" {
		if it.Client == nil || it.Client.Session != nil {
			// cmdq_print_data: an attached client's output.
			it.Out.Write(pb.Data)
			return nil
		}
	} else {
		st, _ := it.findFlag('t', target.Pane, target.CanFail)
		path = it.formatSingle(st, a.Strings()[0])
	}
	if path == "-" {
		// file_write to the client's stdout, which it closes once the write is done.
		if it.Client != nil {
			if it.Client.stdoutClosed {
				return errors.New("Bad file descriptor: -") //nolint:staticcheck // strerror(EBADF), as tmux prints it
			}
			it.Client.stdoutClosed = true
		}
		it.Out.Write(pb.Data)
		return nil
	}
	flags := os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	if a.Has('a') {
		flags = os.O_WRONLY | os.O_CREATE | os.O_APPEND
	}
	full := it.clientPath(path)
	f, err := os.OpenFile(full, flags, 0o644)
	if err != nil {
		return fmt.Errorf("%s: %s", strerror(err), full)
	}
	defer f.Close()
	if _, err := f.Write(pb.Data); err != nil {
		return fmt.Errorf("%s: %s", strerror(err), full)
	}
	return nil
}

// listBuffersTemplate is LIST_BUFFERS_TEMPLATE.
const listBuffersTemplate = "#{buffer_name}: #{buffer_size} bytes: \"#{buffer_sample}\""

// listBuffers is list-buffers: each buffer through -F, those -f leaves.
func listBuffers(it *Item) error {
	a := it.Cmd.Args
	template, ok := a.Get('F')
	if !ok {
		template = listBuffersTemplate
	}
	filter, hasFilter := a.Get('f')
	list := it.Host.Buffers().Walk()
	if o, ok := a.Get('O'); ok {
		// sort_get_buffers: no -O leaves them newest first, even with -r; order only reverses
		// them; the others compare, the name breaking ties, -r reversing the comparison.
		var cmp func(x, y *buffers.Buffer) int
		switch strings.ToLower(o) {
		case "name", "title":
			cmp = func(x, y *buffers.Buffer) int { return 0 }
		case "creation":
			cmp = func(x, y *buffers.Buffer) int { return int(y.Order) - int(x.Order) }
		case "size":
			cmp = func(x, y *buffers.Buffer) int { return len(x.Data) - len(y.Data) }
		case "activity", "index", "key", "modifier", "z":
			cmp = func(x, y *buffers.Buffer) int { return 0 }
		case "order":
		default:
			return errors.New("invalid sort order")
		}
		switch {
		case cmp != nil:
			sort.SliceStable(list, func(i, j int) bool {
				r := cmp(list[i], list[j])
				if r == 0 {
					r = strings.Compare(list[i].Name, list[j].Name)
				}
				if a.Has('r') {
					r = -r
				}
				return r < 0
			})
		case a.Has('r'):
			for i, j := 0, len(list)-1; i < j; i, j = i+1, j-1 {
				list[i], list[j] = list[j], list[i]
			}
		}
	}
	for _, pb := range list {
		ctx := it.formatContext(it.clientInfoOf(it.targetClient()), nil, nil, nil)
		ctx.Buffer = pb
		if hasFilter && !format.True(ctx.Expand(filter)) {
			continue
		}
		it.Print(ctx.Expand(template))
	}
	return nil
}
