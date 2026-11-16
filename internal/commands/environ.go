// The environment of panes and jobs (TASKS.md C3.13), ported from tmux 3.8's environ.c
// (environ_update, environ_for_session) and spawn.c:
//
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
	"fmt"
	"sort"
	"strings"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/format"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/model"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/options"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/version"
)

// environ is tmux's struct environ: names to values, a nil value being a variable removed.
type environ struct {
	m map[string]*string
}

func newEnviron() *environ { return &environ{m: map[string]*string{}} }

func (e *environ) set(k, v string) { e.m[k] = &v }

func (e *environ) clear(k string) { e.m[k] = nil }

// put is environ_put: NAME=value sets, a bare name is ignored here (tmux takes "-NAME" too).
func (e *environ) put(kv string) {
	if k, v, ok := strings.Cut(kv, "="); ok {
		e.set(k, v)
	}
}

// copyFrom is environ_copy: values are set, removed ones removed.
func (e *environ) copyFrom(src map[string]*string) {
	for k, v := range src {
		if v == nil {
			e.clear(k)
		} else {
			e.set(k, *v)
		}
	}
}

// list is environ_push's view: the set variables, in name order, as NAME=value.
func (e *environ) list() []string {
	names := make([]string, 0, len(e.m))
	for k, v := range e.m {
		if v != nil && k != "" {
			names = append(names, k)
		}
	}
	sort.Strings(names)
	out := make([]string, len(names))
	for i, k := range names {
		out[i] = k + "=" + *e.m[k]
	}
	return out
}

// environUpdate is environ_update: for each update-environment pattern, the variables of src
// it matches are set in dst; a pattern nothing matches removes the variable it names.
func environUpdate(oo *options.Options, src map[string]string, dst map[string]*string) {
	o := oo.Get("update-environment")
	if o == nil {
		return
	}
	for _, key := range o.ArrayKeys() {
		v, _ := o.ArrayGet(key)
		pattern := v.Str
		found := false
		for name, value := range src {
			if format.Fnmatch(pattern, name, false) {
				val := value
				dst[name] = &val
				found = true
			}
		}
		if !found {
			dst[pattern] = nil
		}
	}
}

// clientEnv is the command's client's environment, nil for none.
func (it *Item) clientEnv() map[string]string {
	if it.Client == nil {
		return nil
	}
	return it.Client.Env
}

// forSession is environ_for_session: the global environment, the session's, then TERM and
// its friends (unless the config is loading) and TMUX.
func (it *Item) forSession(s *model.Session, noTerm bool) *environ {
	env := newEnviron()
	for _, kv := range it.Host.Environ() {
		env.put(kv)
	}
	if s != nil {
		env.copyFrom(s.Env)
	}
	if !noTerm {
		env.set("TERM", it.Host.Tree().Options.Server.GetString("default-terminal"))
		env.set("TERM_PROGRAM", "tmux")
		env.set("TERM_PROGRAM_VERSION", version.Tmux)
		env.set("COLORTERM", "truecolor")
	}
	id := -1
	if s != nil {
		id = s.ID
	}
	env.set("TMUX", fmt.Sprintf("%s,%d,%d", it.Host.SocketPath(), it.Host.Pid(), id))
	return env
}

// paneEnv is spawn_pane's child environment: forSession, -e, TMUX_PANE, PATH from a
// command-line client (else the default path when there is none), SHELL and PWD.
func (it *Item) paneEnv(s *model.Session, p *model.Pane, extra []string, shell, dir string) []string {
	env := it.forSession(s, false)
	for _, kv := range extra {
		env.put(kv)
	}
	env.set("TMUX_PANE", fmt.Sprintf("%%%d", p.ID))
	if it.Client != nil && it.Client.Session == nil {
		if path, ok := it.Client.Env["PATH"]; ok {
			env.set("PATH", path)
		}
	}
	if v, ok := env.m["PATH"]; !ok || v == nil {
		env.set("PATH", "/usr/bin:/bin")
	}
	env.set("SHELL", shell)
	env.set("PWD", dir)
	return env.list()
}
