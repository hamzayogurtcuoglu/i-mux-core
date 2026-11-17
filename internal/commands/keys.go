package commands

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/copymode"
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
	for _, f := range []byte("cFKR") {
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
	// -X: a command for the pane's mode (window_copy_command), with the key's mouse event.
	if a.Has('X') {
		if p.Mode == nil {
			return errors.New("not in a mode")
		}
		var args []string
		for _, v := range a.Values {
			args = append(args, v.text())
		}
		sess := st.S
		call := copymode.Call{Args: args, Prefix: n, Separators: sess.Options.GetString("word-separators")}
		name := ""
		if it.Client != nil {
			name = it.Client.Name
		}
		return it.Host.CopyCommand(name, p, call, it.Mouse())
	}
	// -M: the key's mouse event goes to the pane under the mouse.
	if a.Has('M') {
		m := it.Mouse()
		var mp *model.Pane
		if m != nil {
			_, _, mp = target.MousePane(it.Host.Tree(), m)
		}
		if mp == nil {
			return errors.New("no mouse target")
		}
		it.Host.PaneMouse(mp, m)
		return nil
	}
	args := a.Strings()
	for range n {
		for _, s := range args {
			if p.Mode != nil {
				it.injectModeString(st, s, a.Has('l'), a.Has('H'))
				continue
			}
			injectString(it.Host, p, s, a.Has('l'), a.Has('H'))
		}
	}
	return nil
}

// injectModeString is cmd_send_keys_inject_key for a pane in a mode: each key runs its binding
// in the mode's key table, for the pane; a key with none does nothing.
func (it *Item) injectModeString(st target.State, s string, literal, hex bool) {
	var ks []keys.Key
	switch {
	case hex:
		if n, err := strconv.ParseUint(s, 16, 64); s != "" && err == nil && n <= 0xff {
			ks = []keys.Key{keys.Key(n)}
		}
	case !literal:
		if k := keys.Parse(s); k != keys.None && k != keys.Unknown {
			ks = []keys.Key{k}
		}
	}
	if ks == nil && !hex {
		ks = textKeys(s)
	}
	table := it.Host.Tree().Keys.Get(st.P.Mode.KeyTable())
	if table == nil {
		return
	}
	// What a command inserts runs last added first, so the keys' bindings go in backwards
	// to run in order.
	var lists []*List
	for _, k := range ks {
		if bd := table.Get(k &^ keys.MaskFlags); bd != nil {
			if list, ok := bd.Cmds.(*List); ok && list != nil {
				lists = append(lists, list)
			}
		}
	}
	for i := len(lists) - 1; i >= 0; i-- {
		cur := st
		it.after = append(it.after, afterItem{list: lists[i], current: &cur})
	}
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

// CopyModeSpec is what copy-mode asks of the server.
type CopyModeSpec struct {
	Client       string        // the command's client, for -M's drag
	Mouse        *target.Mouse // -M: the key's mouse event, a drag to start
	HidePosition bool          // -H
	ScrollExit   bool          // -e
	PageUp       bool          // -u
	PageDown     bool          // -d
	Cancel       bool          // -q
}

// copyMode is copy-mode (cmd_copy_mode_exec): the target pane, or with -M the pane under the
// mouse, goes into copy mode.
func copyMode(it *Item) error {
	a := it.Cmd.Args
	if err := notImplemented(it, "s"); err != nil {
		return err
	}
	st, err := it.findFlag('t', target.Pane, 0)
	if err != nil {
		return err
	}
	p := st.P
	if a.Has('q') {
		it.Host.CopyMode(p, CopyModeSpec{Cancel: true})
		return nil
	}
	spec := CopyModeSpec{HidePosition: a.Has('H'), ScrollExit: a.Has('e'), PageUp: a.Has('u'), PageDown: a.Has('d')}
	if it.Client != nil {
		spec.Client = it.Client.Name
	}
	if a.Has('M') {
		m := it.Mouse()
		var s *model.Session
		if m != nil {
			s, _, p = target.MousePane(it.Host.Tree(), m)
		}
		if m == nil || p == nil || it.Client == nil || it.Client.Session != s {
			return nil
		}
		spec.Mouse = m
	}
	it.Host.CopyMode(p, spec)
	return nil
}
