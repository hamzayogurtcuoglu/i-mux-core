// Ported in part from tmux 3.8's cmd-command-prompt.c, arguments.c and cmd.c:
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

package commands

import (
	"fmt"
	"strings"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/status"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/target"
)

// PromptSpec is a command prompt to open on a client (status_prompt_set's arguments).
type PromptSpec struct {
	Client      string
	Text, Input string // the first prompt and its input, expanded
	Flags       int    // status.Prompt* flags
	Type        int    // status.PromptType*
	// Done is called with the input when a prompt is done, or nil when it is cancelled;
	// next asks for another input. It reports whether the prompt closes.
	Done func(s *string, next func(text, input string)) bool
	// Free is called when the prompt goes without being done (another prompt, a detach).
	Free func()
}

// templateReplace is cmd_template_replace: in template, %idx (or %idx% double-quoted) is s,
// and the first %% is s single-quoted (%%% double-quoted).
func templateReplace(template, s string, idx int) string {
	if !strings.Contains(template, "%") {
		return template
	}
	const (
		nq = iota
		sq
		dq
	)
	var b strings.Builder
	replaced := false
	for i := 0; i < len(template); {
		ch := template[i]
		i++
		if ch == '%' {
			quote := -1
			switch {
			case i < len(template) && template[i] >= '1' && template[i] <= '9' && int(template[i]-'0') == idx:
				i++
				quote = nq
				if i < len(template) && template[i] == '%' {
					quote = dq
					i++
				}
			case i < len(template) && template[i] == '%' && !replaced:
				replaced = true
				i++
				quote = sq
				if i < len(template) && template[i] == '%' {
					quote = dq
					i++
				}
			}
			if quote != -1 {
				for j := 0; j < len(s); j++ {
					c := s[j]
					if quote == sq && c == '\'' {
						b.WriteString(`'\''`)
						continue
					}
					if quote == dq && strings.IndexByte("\"\\$;~", c) >= 0 {
						b.WriteByte('\\')
					}
					b.WriteByte(c)
				}
				continue
			}
		}
		b.WriteByte(ch)
	}
	return b.String()
}

// commandPrompt is command-prompt: a prompt on the client (one per -p, separated by commas,
// each starting with its -I input); when done the template runs with %1, %2 … (and %%) the
// inputs. Without -b the command list waits for it.
func commandPrompt(it *Item) error {
	if err := notImplemented(it, "iP"); err != nil {
		return err
	}
	a := it.Cmd.Args
	tc, err := it.findClient('t')
	if err != nil {
		return err
	}
	// The command's target: the current one (command-prompt's -t is its client).
	st, _ := it.find("", target.Pane, target.CanFail)
	wait := !a.Has('b')

	// args_make_commands_prepare: the template (a block, or a string, -F expanded), "%1"
	// without one.
	template := "%1"
	if len(a.Values) > 0 {
		template = a.Values[0].text()
	}
	if a.Has('F') {
		template = it.formatSingle(st, template)
	}

	type prompt struct{ text, input string }
	var prompts []prompt
	text, space := "", true
	if v, ok := a.Get('p'); ok {
		text = v
	} else if len(a.Values) != 0 {
		text = "(" + a.Values[0].text() + ")"
	} else {
		text, space = ":", false
	}
	inputs, hasInputs := a.Get('I')
	if a.Has('l') {
		prompts = []prompt{{text, inputs}}
	} else {
		// strsep on the commas: each prompt (with a space after it) and its input, "" once
		// the inputs run out.
		var ins []string
		if hasInputs {
			ins = strings.Split(inputs, ",")
		}
		for i, p := range strings.Split(text, ",") {
			if space {
				p += " "
			}
			in := ""
			if i < len(ins) {
				in = ins[i]
			}
			prompts = append(prompts, prompt{p, in})
		}
	}

	typ := status.PromptTypeCommand
	if v, ok := a.Get('T'); ok {
		if typ = status.PromptTypeFromString(v); typ == -1 {
			return fmt.Errorf("unknown type: %s", v)
		}
	}
	flags := 0
	switch {
	case a.Has('1'):
		flags |= status.PromptSingle
	case a.Has('N'):
		flags |= status.PromptNumeric
	case a.Has('k'):
		flags |= status.PromptKey
	case a.Has('e'):
		flags |= status.PromptBSpaceExit
	}
	if a.Has('C') {
		flags |= status.PromptNoFreeze
	}

	// prompt_create expands each input as a format, for the command's target.
	expand := func(s string) string {
		return it.formatContext(nil, st.S, st.WL, st.P).ExpandTime(s)
	}
	finished := false
	finish := func() {
		if finished {
			return
		}
		finished = true
		if wait {
			it.resume(nil)
		}
	}
	var argv []string
	current := 0
	client := tc.Name
	done := func(s *string, next func(text, input string)) bool {
		if s == nil {
			finish()
			return true
		}
		argv = append(argv, *s)
		if current++; current != len(prompts) {
			next(prompts[current].text, expand(prompts[current].input))
			return false
		}
		cmd := template
		for i, v := range argv {
			cmd = templateReplace(cmd, v, i+1)
		}
		// tmux queues the commands: they run once the prompt has gone.
		finished = true
		it.Host.After(0, func() {
			list, perr := ParseCommandString(cmd)
			switch {
			case perr != nil:
				msg := perr.Error()
				it.Host.ShowMessage(client, strings.ToUpper(msg[:1])+msg[1:], MessageOptions{Delay: -1, IgnoreStyles: true})
			case list == nil:
			case wait:
				it.insertAfter(list)
			default:
				it.Host.RunCommands(client, list)
			}
			if wait {
				it.resume(nil)
			}
		})
		return true
	}
	if !it.Host.SetPrompt(PromptSpec{
		Client: client, Text: prompts[0].text, Input: expand(prompts[0].input),
		Flags: flags, Type: typ, Done: done, Free: finish,
	}) {
		return nil
	}
	if !wait {
		return nil
	}
	return it.wait()
}
