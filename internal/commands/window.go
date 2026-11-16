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
)

// new-window, split-window, select-window and select-pane in their basic form (TASKS.md C3.3):
// enough to build the windows and panes targets are found in. -P and -F need the format
// engine; they and the flags i-mux does not use come with task C3.6.

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
	if err := notImplemented(it, "abEFkPS"); err != nil {
		return err
	}
	a := it.Cmd.Args
	st, err := it.findFlag('t', target.Window, target.WindowIndex)
	if err != nil {
		return err
	}
	s, t := st.S, it.Host.Tree()
	sx, sy := it.Host.DefaultSize(s)
	wl, err := t.NewWindow(s, st.Idx, 0, sx, sy)
	if err != nil {
		if errors.Is(err, model.ErrIndexInUse) {
			return fmt.Errorf("create window failed: index %d in use", st.Idx)
		}
		return fmt.Errorf("create window failed: %v", err)
	}
	w := wl.Window
	if n, ok := a.Get('n'); ok {
		w.Name = n
	} else {
		w.Name = defaultWindowName(a.Strings(), it.shell())
	}
	cwd, ok := a.Get('c')
	if !ok {
		cwd = it.clientCwd()
	}
	p := t.NewPane(w, nil, false, sx, sy)
	if err := it.Host.Spawn(p, it.spawnSpec(s, p, a.Strings(), cwd, a.All('e'))); err != nil {
		if t.RemoveWindow(w) {
			it.Host.RemoveSession(s)
		}
		return fmt.Errorf("create window failed: %v", err)
	}
	if !a.Has('d') {
		s.SelectWindow(wl)
	}
	if !a.Has('d') || s.Current == wl {
		it.setCurrent(target.State{S: s, WL: wl, Idx: -1, W: w, P: p})
	}
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
	if err := notImplemented(it, "BEFIkmPRsSTWZ"); err != nil {
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
	cwd, ok := a.Get('c')
	if !ok {
		cwd = it.clientCwd()
	}
	if err := it.Host.Spawn(p, it.spawnSpec(st.S, p, a.Strings(), cwd, a.All('e'))); err != nil {
		it.Host.RemovePane(p)
		return fmt.Errorf("create pane failed: %v", err)
	}
	if !a.Has('d') {
		it.setCurrent(target.State{S: st.S, WL: st.WL, Idx: st.WL.Index, W: st.W, P: p})
	}
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
	links := s.Links()
	switch {
	case a.Has('n') || a.Has('p'):
		i := 0
		for j, l := range links {
			if l == s.Current {
				i = j
			}
		}
		next := links[(i+1)%len(links)]
		what := "no next window"
		if !a.Has('n') {
			next = links[(i-1+len(links))%len(links)]
			what = "no previous window"
		}
		if next == s.Current {
			return errors.New(what)
		}
		s.SelectWindow(next)
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
	return nil
}

// selectPane is select-pane: the target pane (-t) or the last (-l) becomes its window's
// active pane.
func selectPane(it *Item) error {
	if err := notImplemented(it, "DdegLMmPRTUZ"); err != nil {
		return err
	}
	st, err := it.findFlag('t', target.Pane, 0)
	if err != nil {
		return err
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
		it.setCurrent(target.State{S: st.S, WL: st.WL, Idx: st.WL.Index, W: w, P: p})
	}
	return nil
}
