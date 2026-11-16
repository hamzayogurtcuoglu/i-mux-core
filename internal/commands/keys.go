package commands

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/keys"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/model"
)

// send-keys and the extended-keys options (TASKS.md C2.11). Pane targets here cover %N and
// session[:window][.pane] by index; the full rules come with task C3.3. set-option knows only
// the server options keys need; the option table comes with task C3.4.

// findPane resolves a pane target (-t): none is the best session's active pane.
func findPane(t *model.Tree, target string, given bool) (*model.Pane, error) {
	if given && strings.HasPrefix(target, "%") {
		if id, err := strconv.Atoi(target[1:]); err == nil {
			if p := t.Pane(id); p != nil {
				return p, nil
			}
		}
		return nil, fmt.Errorf("can't find pane: %s", target)
	}
	sessPart, rest, _ := strings.Cut(target, ":")
	s, err := findSession(t, sessPart, given)
	if err != nil {
		return nil, err
	}
	wl := s.Current
	winPart, panePart, hasPane := strings.Cut(rest, ".")
	if winPart != "" {
		idx, err := strconv.Atoi(winPart)
		if err != nil {
			return nil, fmt.Errorf("%s: not implemented by imux-core yet", target)
		}
		if wl = s.Link(idx); wl == nil {
			return nil, fmt.Errorf("can't find window: %s", winPart)
		}
	}
	if wl == nil {
		return nil, errors.New("no current target")
	}
	w := wl.Window
	if !hasPane || panePart == "" {
		return w.Active, nil
	}
	idx, err := strconv.Atoi(panePart)
	if err != nil {
		return nil, fmt.Errorf("%s: not implemented by imux-core yet", target)
	}
	if idx < 0 || idx >= len(w.Panes) {
		return nil, fmt.Errorf("can't find pane: %s", panePart)
	}
	return w.Panes[idx], nil
}

// sendKeys is send-keys: each argument is a key name, else literal text; -l makes them all
// literal text, -H hexadecimal bytes, -N repeats.
func sendKeys(it *Item) error {
	a := it.Cmd.Args
	for _, f := range []byte("cFKMRX") {
		if a.Has(f) {
			return fmt.Errorf("send-keys -%c: not implemented by imux-core yet", f)
		}
	}
	target, given := a.Get('t')
	p, err := findPane(it.Host.Tree(), target, given)
	if err != nil {
		return err
	}
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

// serverOptions are the options set-option knows so far, with their choices.
var serverOptions = map[string][]string{
	"extended-keys":        {"off", "on", "always"},
	"extended-keys-format": {"csi-u", "xterm"},
}

// setOption is set-option for the server options in serverOptions.
func setOption(it *Item) error {
	a := it.Cmd.Args
	for _, f := range []byte("aFopqtuUw") {
		if a.Has(f) {
			return fmt.Errorf("set-option -%c: not implemented by imux-core yet", f)
		}
	}
	args := a.Strings()
	choices, ok := serverOptions[args[0]]
	if !ok {
		return fmt.Errorf("set-option %s: not implemented by imux-core yet", args[0])
	}
	if len(args) < 2 {
		return fmt.Errorf("set-option %s: not implemented by imux-core yet", args[0])
	}
	for i, c := range choices {
		if c == args[1] {
			it.Host.SetServerOption(args[0], i)
			return nil
		}
	}
	return fmt.Errorf("unknown value: %s", args[1])
}
