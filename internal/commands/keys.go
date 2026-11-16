package commands

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/keys"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/model"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/target"
)

// send-keys and the server options set so far (TASKS.md C2.11, C2.12); set-option knows only
// those, the option table comes with task C3.4.

// sendKeys is send-keys: each argument is a key name, else literal text; -l makes them all
// literal text, -H hexadecimal bytes, -N repeats.
func sendKeys(it *Item) error {
	a := it.Cmd.Args
	for _, f := range []byte("cFKMRX") {
		if a.Has(f) {
			return fmt.Errorf("send-keys -%c: not implemented by imux-core yet", f)
		}
	}
	st, err := it.findFlag('t', target.Pane, 0)
	if err != nil {
		return err
	}
	p := st.P
	n := 1
	if s, ok := a.Get('N'); ok {
		v, err := strconv.Atoi(s)
		switch {
		case err != nil:
			return fmt.Errorf("repeat count %s is invalid", s)
		case v < 1:
			return fmt.Errorf("repeat count %s is too small", s)
		}
		n = v
	}
	args := a.Strings()
	for range n {
		for _, s := range args {
			injectString(it.Host, p, s, a.Has('l'), a.Has('H'))
		}
	}
	return nil
}

// injectString is cmd_send_keys_inject_string: an argument is sent as a key, or as its text
// when it names no key or the key has no encoding in the pane.
func injectString(h Host, p *model.Pane, s string, literal, hex bool) {
	if hex {
		n, err := strconv.ParseUint(s, 16, 64)
		if s != "" && err == nil && n <= 0xff {
			h.PaneKey(p, keys.Literal|keys.Key(n))
		}
		return
	}
	if !literal {
		if k := keys.Parse(s); k != keys.None && k != keys.Unknown && h.PaneKey(p, k) {
			return
		}
	}
	for _, k := range textKeys(s) {
		h.PaneKey(p, k)
	}
}

// textKeys are the keys of a text: its characters.
func textKeys(s string) []keys.Key {
	var out []keys.Key
	for s != "" {
		r, size := utf8.DecodeRuneInString(s)
		if r == utf8.RuneError && size == 1 {
			s = s[1:]
			continue
		}
		out = append(out, keys.Unicode(r))
		s = s[size:]
	}
	return out
}

// switchClient is switch-client: the client (-c, else the current client) shows the target
// session, selecting a target window and pane when given; -T only puts it in a key table.
func switchClient(it *Item) error {
	a := it.Cmd.Args
	for _, f := range []byte("FlnOprZ") {
		if a.Has(f) {
			return fmt.Errorf("switch-client -%c: not implemented by imux-core yet", f)
		}
	}
	c, err := it.findClient('c')
	if err != nil {
		return err
	}
	if name, ok := a.Get('T'); ok {
		// One key table, for one key (task C3.8): the client leaves it again with the
		// first key it presses there.
		if it.Host.Tree().Keys.Get(name) == nil {
			return fmt.Errorf("table %s doesn't exist", name)
		}
		return it.Host.SetKeyTable(c.Name, name)
	}
	tflag, _ := a.Get('t')
	// A session target prefers a session no client shows, as in tmux.
	typ, flags := target.Session, target.PreferUnattached
	if strings.ContainsAny(tflag, ":.%") || tflag == "=" {
		typ, flags = target.Pane, 0
	}
	st, err := it.find(tflag, typ, flags)
	if err != nil {
		return err
	}
	if st.WL != nil {
		if st.P != nil && st.P != st.W.Active {
			it.Host.Tree().SetActivePane(st.W, st.P)
		}
		st.S.SelectWindow(st.WL)
		it.setCurrent(sessionState(st.S))
	}
	// The session takes the client's update-environment variables (not with -E).
	if ci := it.clientInfo(c.Name); ci != nil && ci.Env != nil && !it.Cmd.Args.Has('E') {
		environUpdate(st.S.Options, ci.Env, st.S.Env)
	}
	return it.Host.SwitchClient(c.Name, st.S)
}
