// source-file (TASKS.md C3.11), ported from tmux 3.8's cmd-source-file.c and cfg.c:
//
// Copyright (c) 2008 Tiago Cunha <me@tiagocunha.org>
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
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/cmdlang"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/format"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/target"
)

// sourceDepthLimit is CMD_SOURCE_FILE_DEPTH_LIMIT.
const sourceDepthLimit = 50

// ParseOptions are what a config file is parsed with: the server's environment for $NAME and
// ~, and %if conditions expanded as formats.
func ParseOptions(h Host) cmdlang.Options {
	env := func(name string) (string, bool) {
		prefix := name + "="
		for _, kv := range h.Environ() {
			if v, ok := strings.CutPrefix(kv, prefix); ok {
				return v, true
			}
		}
		return "", false
	}
	return cmdlang.Options{
		Env: env,
		Home: func(user string) (string, bool) {
			if user != "" {
				return "", false
			}
			return env("HOME")
		},
		Cond: func(f string) bool { return format.True(ParseFormat(h, nil, f)) },
	}
}

// globQuote is cmd_source_file_quote_for_glob: a directory made literal in a pattern.
func globQuote(p string) string {
	var b strings.Builder
	for i := 0; i < len(p); i++ {
		c := p[i]
		if c < 128 && !isAlnum(c) && c != '/' {
			b.WriteByte('\\')
		}
		b.WriteByte(c)
	}
	return b.String()
}

// strerror is the C library's message for a file error.
func strerror(err error) string {
	var errno syscall.Errno
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return "No such file or directory"
	case errors.Is(err, fs.ErrPermission):
		return "Permission denied"
	case errors.As(err, &errno):
		s := errno.Error()
		if s != "" {
			return strings.ToUpper(s[:1]) + s[1:]
		}
	}
	return err.Error()
}

// sourceFile is source-file: each file matching a path (relative to the client's directory)
// is parsed and its commands run right after source-file, in order, with the target as the
// current target; -q leaves out a path that matches nothing, -n only parses, -F expands
// the paths as formats. Errors in parsing are the config's causes: printed once the files
// have run (or, while the config loads, kept with its other errors).
func sourceFile(it *Item) error {
	a := it.Cmd.Args
	if err := notImplemented(it, "v"); err != nil {
		return err
	}
	depth := 0
	if it.rs != nil {
		depth = it.rs.depth
	}
	if depth >= sourceDepthLimit {
		return errors.New("too many nested files")
	}
	st, err := it.findFlag('t', target.Pane, target.CanFail)
	if err != nil {
		return err
	}
	quiet := a.Has('q')
	cwd := globQuote(it.cwdFor(nil))
	var files []string
	var failed []string
	for _, path := range a.Strings() {
		if a.Has('F') {
			path = it.formatSingle(st, path)
		}
		if path == "-" {
			failed = append(failed, "source-file -: not implemented by imux-core yet")
			continue
		}
		pattern := path
		if !filepath.IsAbs(path) {
			pattern = cwd + "/" + path
		}
		matches, gerr := filepath.Glob(pattern)
		switch {
		case gerr != nil:
			failed = append(failed, "Invalid argument: "+path)
		case len(matches) == 0 && !quiet:
			failed = append(failed, "No such file or directory: "+path)
		}
		files = append(files, matches...)
	}

	// The files' commands, then the causes, run after source-file in order (each
	// cmdq_insert_after goes after the one before).
	var items []afterItem
	var causes []string
	// A command a file uses that is not implemented warns once (COMPATIBILITY.md §10), for
	// all the files one source-file reads.
	warned := map[string]bool{}
	if it.rs != nil && it.rs.warned != nil {
		warned = it.rs.warned
	}
	broken := false
	opts := ParseOptions(it.Host)
	for _, path := range files {
		b, rerr := os.ReadFile(path)
		if rerr != nil {
			failed = append(failed, strerror(rerr)+": "+path)
			continue
		}
		if len(b) == 0 {
			continue
		}
		cmds, perr := cmdlang.Parse(string(b), opts)
		var list *List
		if perr == nil {
			list, perr = (&Builder{File: path, Aliases: DefaultAliases, Parse: opts}).Build(cmds)
		} else {
			var pe *cmdlang.Error
			if errors.As(perr, &pe) {
				perr = fmt.Errorf("%s:%d: %s", path, pe.Line, pe.Msg)
			}
		}
		if perr != nil {
			causes = append(causes, perr.Error())
			broken = true
			continue
		}
		if a.Has('n') || list == nil {
			continue
		}
		cur := st
		formats := map[string]string{}
		for k, v := range it.Formats {
			formats[k] = v
		}
		formats["current_file"] = path
		items = append(items, afterItem{list: list, current: &cur, formats: formats, depth: 1, warned: warned})
	}
	config := it.configLoading()
	if len(causes) > 0 {
		if config {
			// cfg_add_cause: they join the config's own errors.
			for _, c := range causes {
				it.rs.errs.WriteString(c + "\n")
			}
		} else {
			cs := causes
			items = append(items, afterItem{fn: func() {
				// cfg_print_causes, after the files' commands.
				for _, c := range cs {
					it.Print(c)
				}
			}})
		}
	}
	if broken && !config && it.Client != nil && it.Client.Session == nil && it.rs != nil {
		it.rs.failed = true
	}
	// it.after runs the last added first.
	for i := len(items) - 1; i >= 0; i-- {
		it.after = append(it.after, items[i])
	}
	if len(failed) == 0 {
		return nil
	}
	msg := strings.Join(failed, "\n")
	if len(files) == 0 || it.rs == nil {
		return errors.New(msg)
	}
	// With files to read, tmux's source-file waits for them and does not fail: its errors
	// are printed, and the rest of its group still runs.
	it.rs.errs.WriteString(msg + "\n")
	it.rs.failed = true
	return nil
}
