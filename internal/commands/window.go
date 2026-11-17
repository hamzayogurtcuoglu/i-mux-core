package commands

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/layout"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/model"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/target"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/vt"
)

// new-window, split-window, select-window and select-pane (TASKS.md C3.3, C3.6). The flags
// i-mux does not use are refused.

func notImplemented(it *Item, flags string) error {
	for _, f := range []byte(flags) {
		if it.Cmd.Args.Has(f) {
			return fmt.Errorf("%s -%c: not implemented by imux-core yet", it.Cmd.Spec.Name, f)
		}
	}
	return nil
}

// newWindow is new-window: a window at the target's index (-t), else the first free one.
func newWindow(it *Item) error {
	if err := notImplemented(it, "EkS"); err != nil {
		return err
	}
	a := it.Cmd.Args
	st, err := it.findFlag('t', target.Window, target.WindowIndex)
	if err != nil {
		return err
	}
	s, t := st.S, it.Host.Tree()
	wname, hasName := a.Get('n')
	if hasName {
		n, err := it.cleanName("window", wname, s)
		if err != nil {
			return err
		}
		wname = n
	}
	idx := st.Idx
	if a.Has('a') || a.Has('b') {
		// winlink_shuffle_up: room after (or before) the target window.
		if i := s.ShuffleUp(st.WL, a.Has('b')); i != -1 {
			idx = i
		}
	}
	sx, sy := it.Host.DefaultSize(s)
	wl, err := t.NewWindow(s, idx, int(s.Options.GetNumber("base-index")), sx, sy)
	if err != nil {
		if errors.Is(err, model.ErrIndexInUse) {
			return fmt.Errorf("create window failed: index %d in use", idx)
		}
		return fmt.Errorf("create window failed: %v", err)
	}
	w := wl.Window
	argv := it.commandArgv(s)
	if hasName {
		// A window named when made is not renamed automatically (spawn.c).
		w.Name = wname
		w.Options.SetNumber("automatic-rename", 0)
	} else {
		w.Name = defaultWindowName(argv, it.shell(s))
	}
	cwd := it.paneCwd(s)
	p := t.NewPane(w, nil, false, sx, sy)
	if err := it.Host.Spawn(p, it.spawnSpec(s, p, argv, cwd, a.All('e'))); err != nil {
		if t.RemoveWindow(w) {
			it.Host.RemoveSession(s)
		}
		return fmt.Errorf("create window failed: %v", err)
	}
	if !a.Has('d') {
		s.SelectWindow(wl)
	}
	it.printNew("#{session_name}:#{window_index}.#{pane_index}", s, wl, p)
	if !a.Has('d') || s.Current == wl {
		it.setCurrent(target.State{S: s, WL: wl, Idx: -1, W: w, P: p})
	}
	it.afterHook("after-new-window", target.State{S: s, WL: wl, Idx: wl.Index, W: w, P: w.Active})
	return nil
}

// splitSize is -l (cells, or a percentage of the pane or with -f the window) or -p.
func splitSize(it *Item, st target.State, horizontal bool) (int, error) {
	a := it.Cmd.Args
	cur := st.P.Sy
	switch {
	case a.Has('f') && horizontal:
		cur = st.W.Sx
	case a.Has('f'):
		cur = st.W.Sy
	case horizontal:
		cur = st.P.Sx
	}
	invalid := func(cause string) error { return fmt.Errorf("invalid tiled geometry %s", cause) }
	if v, ok := a.Get('l'); ok {
		if v == "" {
			return 0, invalid("empty")
		}
		if pct, ok := strings.CutSuffix(v, "%"); ok {
			n, cause := strtonum(pct, 0, 1000)
			if cause != "" {
				return 0, invalid(cause)
			}
			return cur * n / 100, nil
		}
		n, cause := strtonum(v, 0, math.MaxInt32)
		if cause != "" {
			return 0, invalid(cause)
		}
		return n, nil
	}
	if v, ok := a.Get('p'); ok {
		n, cause := strtonum(v, 0, 100)
		if cause != "" {
			return 0, invalid(cause)
		}
		return cur * n / 100, nil
	}
	return -1, nil
}

// strtonum parses a number in [lo, hi], with strtonum's causes.
func strtonum(s string, lo, hi int) (int, string) {
	n, err := strconv.ParseInt(s, 10, 64)
	switch {
	case err != nil:
		return 0, "invalid"
	case n < int64(lo):
		return 0, "too small"
	case n > int64(hi):
		return 0, "too large"
	}
	return int(n), ""
}

// splitWindow is split-window: a new pane beside (-h) or below the target pane.
func splitWindow(it *Item) error {
	if err := notImplemented(it, "BEIkmRsSTWZ"); err != nil {
		return err
	}
	a := it.Cmd.Args
	st, err := it.findFlag('t', target.Pane, 0)
	if err != nil {
		return err
	}
	size, err := splitSize(it, st, a.Has('h'))
	if err != nil {
		return err
	}
	t := it.Host.Tree()
	p, err := t.SplitPane(st.P, model.SplitOptions{
		Horizontal: a.Has('h'), Before: a.Has('b'), Full: a.Has('f'), Size: size, Detached: a.Has('d'),
	})
	if errors.Is(err, layout.ErrNoSpace) {
		return errors.New("no space for a new pane")
	}
	if err != nil {
		return err
	}
	cwd := it.paneCwd(st.S)
	if err := it.Host.Spawn(p, it.spawnSpec(st.S, p, it.commandArgv(st.S), cwd, a.All('e'))); err != nil {
		it.Host.RemovePane(p)
		return fmt.Errorf("create pane failed: %v", err)
	}
	it.printNew("#{session_name}:#{window_index}.#{pane_index}", st.S, st.WL, p)
	if !a.Has('d') {
		it.setCurrent(target.State{S: st.S, WL: st.WL, Idx: st.WL.Index, W: st.W, P: p})
	}
	it.afterHook("after-split-window", target.State{S: st.S, WL: st.WL, Idx: st.WL.Index, W: st.W, P: p})
	return nil
}

// selectWindow is select-window: the target window (-t), the last (-l), the next (-n) or
// the previous (-p) becomes the session's current.
func selectWindow(it *Item) error {
	a := it.Cmd.Args
	st, err := it.findFlag('t', target.Window, 0)
	if err != nil {
		return err
	}
	s := st.S
	switch {
	case a.Has('n') || a.Has('p'):
		if err := stepWindow(s, a.Has('n'), false); err != nil {
			return err
		}
		it.setCurrent(sessionState(s))
	case a.Has('l'):
		last := s.LastWindow()
		if last == nil || last == s.Current {
			return errors.New("no last window")
		}
		s.SelectWindow(last)
		it.setCurrent(sessionState(s))
	case a.Has('T') && st.WL == s.Current:
		last := s.LastWindow()
		if last == nil || last == s.Current {
			return errors.New("no last window")
		}
		s.SelectWindow(last)
		if it.Current != nil && it.Current.S == s {
			it.setCurrent(sessionState(s))
		}
	default:
		if st.WL != s.Current {
			s.SelectWindow(st.WL)
			it.setCurrent(sessionState(s))
		}
	}
	// after-select-window, with the command list's current target.
	cur := sessionState(s)
	if it.Current != nil && it.Current.S != nil {
		cur = *it.Current
	}
	it.afterHook("after-select-window", cur)
	return nil
}

// selectPane is select-pane: the target pane (-t) or the last (-l) becomes its window's
// active pane.
func selectPane(it *Item) error {
	if err := notImplemented(it, "DdegLMmPRUZ"); err != nil {
		return err
	}
	st, err := it.findFlag('t', target.Pane, 0)
	if err != nil {
		return err
	}
	if title, ok := it.Cmd.Args.Get('T'); ok && !it.Cmd.Args.Has('l') {
		// screen_set_title: the title is a format, made safe to show.
		clean, ok := vt.CleanName(it.formatSingle(st, title), false)
		if ok && st.P.Term != nil {
			st.P.Mu.Lock()
			st.P.Term.Screen.Title = clean
			st.P.Mu.Unlock()
			it.Host.OptionChanged("")
		}
		return nil
	}
	w, t := st.W, it.Host.Tree()
	p := st.P
	if it.Cmd.Args.Has('l') {
		p = w.LastPane()
		if p == nil && len(w.Panes) == 2 {
			i := w.PaneIndex(w.Active)
			if i > 0 {
				p = w.Panes[i-1]
			} else {
				p = w.Panes[1]
			}
		}
		if p == nil {
			return errors.New("no last pane")
		}
		if p != w.Active {
			t.SetActivePane(w, p)
			it.setCurrent(target.State{S: st.S, WL: st.WL, Idx: st.WL.Index, W: w, P: w.Active})
		}
		return nil
	}
	if p != w.Active {
		t.SetActivePane(w, p)
		cur := target.State{S: st.S, WL: st.WL, Idx: st.WL.Index, W: w, P: p}
		it.setCurrent(cur)
		it.afterHook("after-select-pane", cur)
	}
	return nil
}

// stepWindow is session_next and session_previous: the session's next or previous window
// becomes its current one, wrapping round; with alert, only a window with an alert (the engine
// has no alert flags yet, task C4.5, so there is none).
func stepWindow(s *model.Session, next, alert bool) error {
	what := "no next window"
	if !next {
		what = "no previous window"
	}
	links := s.Links()
	if alert || s.Current == nil || len(links) == 0 {
		return errors.New(what)
	}
	i := 0
	for j, l := range links {
		if l == s.Current {
			i = j
		}
	}
	wl := links[(i+1)%len(links)]
	if !next {
		wl = links[(i-1+len(links))%len(links)]
	}
	if wl == s.Current {
		return errors.New(what)
	}
	s.SelectWindow(wl)
	return nil
}

// nextWindow is next-window and previous-window (cmd_select_window_exec for them): the target
// session moves to its next or previous window (-a: with an alert).
func nextWindow(next bool) func(it *Item) error {
	return func(it *Item) error {
		st, err := it.findFlag('t', target.Session, 0)
		if err != nil {
			return err
		}
		s := st.S
		if err := stepWindow(s, next, it.Cmd.Args.Has('a')); err != nil {
			return err
		}
		it.setCurrent(sessionState(s))
		it.afterHook("after-select-window", sessionState(s))
		return nil
	}
}

// swapWindow is swap-window: the windows of -s and -t change places; with -d the sessions
// show the windows now at their indexes.
func swapWindow(it *Item) error {
	dst, err := it.findFlag('t', target.Window, 0)
	if err != nil {
		return err
	}
	src, err := it.findFlag('s', target.Window, target.DefaultMarked)
	if err != nil {
		return err
	}
	if src.WL.Window == dst.WL.Window {
		return nil
	}
	it.Host.Tree().SwapWindows(src.WL, dst.WL)
	if it.Cmd.Args.Has('d') {
		dst.S.SelectWindow(dst.WL)
		if src.S != dst.S {
			src.S.SelectWindow(src.WL)
		}
	}
	return nil
}
