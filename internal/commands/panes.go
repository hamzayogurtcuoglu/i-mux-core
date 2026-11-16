// Ported from tmux 3.8's cmd-list-sessions.c, cmd-list-windows.c, cmd-list-panes.c,
// cmd-list-clients.c, cmd-rename-window.c, cmd-kill-window.c, cmd-kill-pane.c,
// cmd-swap-pane.c, cmd-respawn-pane.c, cmd-resize-pane.c, cmd-resize-window.c,
// cmd-select-layout.c and cmd-capture-pane.c:
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
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/format"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/model"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/target"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/vt"
)

// The session, window and pane commands of TASKS.md C3.6.

// Sort orders of -O (sort_order_from_string); sortEnd leaves the list as it is.
const (
	sortEnd = iota
	sortActivity
	sortCreation
	sortIndex
	sortModifier
	sortName
	sortOrder
	sortSize
	sortZ
)

func sortOrderFromString(s string, given bool) int {
	if !given {
		return sortEnd
	}
	switch strings.ToLower(s) {
	case "activity":
		return sortActivity
	case "creation":
		return sortCreation
	case "index", "key":
		return sortIndex
	case "modifier":
		return sortModifier
	case "name", "title":
		return sortName
	case "order":
		return sortOrder
	case "size":
		return sortSize
	case "z":
		return sortZ
	}
	return sortEnd
}

// listSort is the -O and -r of a list command; ok is false for an invalid order.
func listSort(a *Args) (order int, reversed bool, err error) {
	v, given := a.Get('O')
	order = sortOrderFromString(v, given)
	if order == sortEnd && given {
		return 0, false, errors.New("invalid sort order")
	}
	return order, a.Has('r'), nil
}

// sortList is sort_qsort: nothing for sortEnd, a reversal for sortOrder, else a sort by cmp
// (which breaks ties itself).
func sortList[T any](l []T, order int, reversed bool, cmp func(a, b T) int) {
	switch {
	case len(l) < 2 || order == sortEnd:
		return
	case order == sortOrder:
		if reversed {
			for i, j := 0, len(l)-1; i < j; i, j = i+1, j-1 {
				l[i], l[j] = l[j], l[i]
			}
		}
		return
	}
	sort.SliceStable(l, func(i, j int) bool {
		r := cmp(l[i], l[j])
		if reversed {
			r = -r
		}
		return r < 0
	})
}

func cmpInt(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// listLine prints one item of a list: its format tree with "line" added, through -f and the
// template.
func (it *Item) listLine(ctx *format.Context, line int, template string) {
	ctx.Values["line"] = strconv.Itoa(line)
	if filter, ok := it.Cmd.Args.Get('f'); ok && !format.True(ctx.Expand(filter)) {
		return
	}
	it.Print(ctx.Expand(template))
}

// listSessions is list-sessions.
func listSessions(it *Item) error {
	a := it.Cmd.Args
	template, ok := a.Get('F')
	if !ok {
		template = "#{session_name}: #{session_windows} windows (created #{t:session_created})" +
			"#{?session_grouped, (group ,}#{session_group}#{?session_grouped,),}" +
			"#{?session_attached, (attached),}"
	}
	order, reversed, err := listSort(a)
	if err != nil {
		return err
	}
	l := it.Host.Tree().Sessions()
	sortList(l, order, reversed, func(x, y *model.Session) int {
		r := 0
		switch order {
		case sortIndex:
			r = cmpInt(x.ID, y.ID)
		case sortCreation:
			r = x.Created.Compare(y.Created)
		case sortActivity:
			r = -x.Activity.Compare(y.Activity)
		case sortName:
			r = strings.Compare(x.Name, y.Name)
		}
		if r == 0 {
			r = strings.Compare(x.Name, y.Name)
		}
		return r
	})
	for i, s := range l {
		it.listLine(it.formatContext(nil, s, nil, nil), i, template)
	}
	return nil
}

func winlinkCmp(order int) func(x, y *model.Winlink) int {
	return func(x, y *model.Winlink) int {
		r := 0
		switch order {
		case sortIndex:
			r = cmpInt(x.Index, y.Index)
		case sortName:
			r = strings.Compare(x.Window.Name, y.Window.Name)
		case sortSize:
			r = cmpInt(x.Window.Sx*x.Window.Sy, y.Window.Sx*y.Window.Sy)
		}
		if r == 0 {
			r = strings.Compare(x.Window.Name, y.Window.Name)
		}
		return r
	}
}

// listWindows is list-windows.
func listWindows(it *Item) error {
	a := it.Cmd.Args
	st, err := it.findFlag('t', target.Session, 0)
	if err != nil {
		return err
	}
	order, reversed, err := listSort(a)
	if err != nil {
		return err
	}
	template, ok := a.Get('F')
	var l []*model.Winlink
	if a.Has('a') {
		for _, s := range it.Host.Tree().Sessions() {
			l = append(l, s.Links()...)
		}
		if !ok {
			template = "#{session_name}:#{window_index}: #{window_name}#{window_raw_flags} " +
				"(#{window_panes} panes) [#{window_width}x#{window_height}] "
		}
	} else {
		l = st.S.Links()
		if !ok {
			template = "#{window_index}: #{window_name}#{window_raw_flags} (#{window_panes} panes) " +
				"[#{window_width}x#{window_height}] [layout #{window_layout}] #{window_id}" +
				"#{?window_active, (active),}"
		}
	}
	sortList(l, order, reversed, winlinkCmp(order))
	for _, wl := range l {
		// tmux gives every line the number of windows.
		it.listLine(it.formatContext(nil, wl.Session, wl, nil), len(l), template)
	}
	return nil
}

// listPanes is list-panes: the target window's panes, with -s its session's, with -a all.
func listPanes(it *Item) error {
	a := it.Cmd.Args
	st, err := it.findFlag('t', target.Window, 0)
	if err != nil {
		return err
	}
	order, reversed, err := listSort(a)
	if err != nil {
		return err
	}
	switch {
	case a.Has('a'):
		for _, s := range it.Host.Tree().Sessions() {
			for _, wl := range s.Links() {
				it.listPanesWindow(s, wl, 2, order, reversed)
			}
		}
	case a.Has('s'):
		for _, wl := range st.S.Links() {
			it.listPanesWindow(st.S, wl, 1, order, reversed)
		}
	default:
		it.listPanesWindow(st.S, st.WL, 0, order, reversed)
	}
	return nil
}

func (it *Item) listPanesWindow(s *model.Session, wl *model.Winlink, typ, order int, reversed bool) {
	template, ok := it.Cmd.Args.Get('F')
	if !ok {
		prefix := []string{"#{pane_index}: ", "#{window_index}.#{pane_index}: ", "#{session_name}:#{window_index}.#{pane_index}: "}[typ]
		template = prefix + "[#{pane_width}x#{pane_height}#{?pane_floating_flag, #{pane_x}#,#{pane_y}#,#{pane_z}}] " +
			"[history #{history_size}/#{history_limit}, #{history_bytes} bytes] #{pane_id}" +
			"#{?pane_active, (active),}#{?pane_dead, (dead),}"
	}
	w := wl.Window
	l := append([]*model.Pane(nil), w.Panes...)
	sortList(l, order, reversed, func(x, y *model.Pane) int {
		r := 0
		switch order {
		case sortActivity:
			r = cmpInt(int(x.ActivePoint), int(y.ActivePoint))
		case sortCreation:
			r = cmpInt(x.ID, y.ID)
		case sortSize:
			r = cmpInt(x.Sx*x.Sy, y.Sx*y.Sy)
		case sortIndex:
			r = cmpInt(w.PaneIndex(x), w.PaneIndex(y))
		case sortName:
			r = strings.Compare(paneTitle(x), paneTitle(y))
		}
		if r == 0 {
			r = strings.Compare(paneTitle(x), paneTitle(y))
		}
		return r
	})
	for _, p := range l {
		it.listLine(it.formatContext(nil, s, wl, p), len(l), template)
	}
}

func paneTitle(p *model.Pane) string {
	if p.Term == nil {
		return ""
	}
	p.Mu.Lock()
	defer p.Mu.Unlock()
	return p.Term.Screen.Title
}

// listClients is list-clients: the attached clients, of the -t session only if given.
func listClients(it *Item) error {
	a := it.Cmd.Args
	st, err := it.findFlag('t', target.Session, 0)
	if err != nil {
		return err
	}
	var s *model.Session
	if a.Has('t') {
		s = st.S
	}
	template, ok := a.Get('F')
	if !ok {
		template = "#{client_name}: #{session_name} [#{client_width}x#{client_height} #{client_termname}] " +
			"#{?#{!=:#{client_uid},#{uid}},[user #{?client_user,#{client_user},#{client_uid},}] ,}" +
			"#{?client_flags,(,}#{client_flags}#{?client_flags,),}"
	}
	order, reversed, err := listSort(a)
	if err != nil {
		return err
	}
	l := it.Host.Clients()
	if order == sortEnd || order == sortOrder {
		// tmux's list of clients is in the order they connected.
		sort.SliceStable(l, func(i, j int) bool { return l[i].Created.Before(l[j].Created) })
	}
	sortList(l, order, reversed, func(x, y *ClientInfo) int {
		r := 0
		switch order {
		case sortName:
			r = strings.Compare(x.Name, y.Name)
		case sortActivity:
			r = -x.Activity.Compare(y.Activity)
		case sortCreation:
			r = x.Created.Compare(y.Created)
		}
		if r == 0 {
			r = strings.Compare(x.Name, y.Name)
		}
		return r
	})
	for i, c := range l {
		if c.Session == nil || (s != nil && c.Session != s) {
			continue
		}
		it.listLine(it.formatContext(c, nil, nil, nil), i, template)
	}
	return nil
}

// cleanName is check_name and clean_name: a name expanded as a format, valid UTF-8, with its
// control characters encoded.
func (it *Item) cleanName(what, name string, s *model.Session) (string, error) {
	expanded := it.formatContext(nil, s, nil, nil).Expand(name)
	clean, ok := vt.CleanName(expanded, false)
	if !ok {
		return "", fmt.Errorf("invalid %s name: %s", what, expanded)
	}
	return clean, nil
}

// renameWindow is rename-window.
func renameWindow(it *Item) error {
	st, err := it.findFlag('t', target.Window, 0)
	if err != nil {
		return err
	}
	name := it.formatSingle(st, it.Cmd.Args.Strings()[0])
	clean, ok := vt.CleanName(name, false)
	if !ok {
		return fmt.Errorf("invalid window name: %s", name)
	}
	st.WL.Window.Name = clean
	st.WL.Window.Options.SetNumber("automatic-rename", 0)
	it.Host.OptionChanged("automatic-rename")
	return nil
}

// killWindow is kill-window: the target window, or with -a every other window of its
// session (those -f matches).
func killWindow(it *Item) error {
	a := it.Cmd.Args
	st, err := it.findFlag('t', target.Window, 0)
	if err != nil {
		return err
	}
	filter, hasFilter := a.Get('f')
	if hasFilter && !a.Has('a') {
		return errors.New("-f only valid with -a")
	}
	if !a.Has('a') {
		it.removeWindow(st.WL.Window)
		return nil
	}
	s := st.S
	for _, wl := range s.Links() {
		if wl == st.WL {
			continue
		}
		if hasFilter && !format.True(it.formatContext(nil, s, wl, nil).Expand(filter)) {
			continue
		}
		it.removeWindow(wl.Window)
	}
	it.renumber(s)
	return nil
}

// removeWindow is server_kill_window: the window's panes go, and with the last the window.
func (it *Item) removeWindow(w *model.Window) {
	for _, p := range append([]*model.Pane(nil), w.Panes...) {
		it.Host.RemovePane(p)
	}
}

// renumber is session_renumber_windows, when renumber-windows is on.
func (it *Item) renumber(s *model.Session) {
	if it.Host.Tree().Session(s.ID) != s || s.Options.GetNumber("renumber-windows") == 0 {
		return
	}
	s.Renumber(int(s.Options.GetNumber("base-index")))
}

// killPane is kill-pane: the target pane, or with -a every other pane of its window.
func killPane(it *Item) error {
	a := it.Cmd.Args
	st, err := it.findFlag('t', target.Pane, 0)
	if err != nil {
		return err
	}
	filter, hasFilter := a.Get('f')
	if hasFilter && !a.Has('a') {
		return errors.New("-f only valid with -a")
	}
	if !a.Has('a') {
		it.Host.RemovePane(st.P)
		return nil
	}
	w := st.W
	it.Host.Tree().Unzoom(w)
	for _, p := range append([]*model.Pane(nil), w.Panes...) {
		if p == st.P {
			continue
		}
		if hasFilter && !format.True(it.formatContext(nil, st.S, st.WL, p).Expand(filter)) {
			continue
		}
		it.Host.RemovePane(p)
	}
	return nil
}

// swapPane is swap-pane: -s and -t, or with -D or -U the target and the pane after or before
// it.
func swapPane(it *Item) error {
	a := it.Cmd.Args
	dst, err := it.findFlag('t', target.Pane, 0)
	if err != nil {
		return err
	}
	src, err := it.findFlag('s', target.Pane, target.DefaultMarked)
	if err != nil {
		return err
	}
	t := it.Host.Tree()
	dw := dst.W
	srcPane := src.P
	panes := dw.Panes
	switch {
	case a.Has('D'):
		i := dw.PaneIndex(dst.P) + 1
		if i >= len(panes) {
			i = 0
		}
		srcPane = panes[i]
	case a.Has('U'):
		i := dw.PaneIndex(dst.P) - 1
		if i < 0 {
			i = len(panes) - 1
		}
		srcPane = panes[i]
	}
	sw := srcPane.Window
	dstZoomed, srcZoomed := dw.Zoomed(), sw.Zoomed()
	if srcPane != dst.P {
		t.SwapPanes(srcPane, dst.P, a.Has('d'))
	} else {
		t.Unzoom(dw)
		t.Unzoom(sw)
	}
	// window_pop_zoom: -Z zooms again what was zoomed.
	if a.Has('Z') {
		if srcZoomed {
			t.Zoom(sw.Active)
		}
		if sw != dw && dstZoomed {
			t.Zoom(dw.Active)
		}
	}
	it.Host.OptionChanged("")
	return nil
}

// respawnPane is respawn-pane: the pane's program started again, the old one killed with -k.
func respawnPane(it *Item) error {
	if err := notImplemented(it, "E"); err != nil {
		return err
	}
	a := it.Cmd.Args
	st, err := it.findFlag('t', target.Pane, 0)
	if err != nil {
		return err
	}
	p := st.P
	if !p.Dead && !a.Has('k') {
		idx := p.Window.PaneIndex(p) + int(p.Window.Options.GetNumber("pane-base-index"))
		return fmt.Errorf("respawn pane failed: pane %s:%d.%d still active", st.S.Name, st.WL.Index, idx)
	}
	argv := a.Strings()
	if len(argv) == 0 {
		argv = p.Argv
	}
	cwd := p.Cwd
	if c, ok := a.Get('c'); ok {
		cwd = it.spawnCwd(c, st.S)
	}
	shell := p.Shell
	spec := it.spawnSpec(st.S, p, argv, cwd, a.All('e'))
	if shell != "" {
		// A pane that is respawned keeps its shell.
		p.Shell = shell
	}
	if err := it.Host.Respawn(p, spec); err != nil {
		return fmt.Errorf("respawn pane failed: %v", err)
	}
	return nil
}

// percentage is args_percentage: N or N% of whole, between lo and hi.
func percentage(v string, lo, hi, whole int) (int, string) {
	if v == "" {
		return 0, "empty"
	}
	if pct, ok := strings.CutSuffix(v, "%"); ok {
		n, cause := strtonum(pct, 0, 1000)
		if cause != "" {
			return 0, cause
		}
		n = whole * n / 100
		switch {
		case n < lo:
			return 0, "too small"
		case n > hi:
			return 0, "too large"
		}
		return n, ""
	}
	return strtonum(v, lo, hi)
}

// resizePane is resize-pane: -Z toggles zoom; -x and -y set a size; -L -R -U -D move a border
// (by their value, the argument, or 1).
func resizePane(it *Item) error {
	if err := notImplemented(it, "MT"); err != nil {
		return err
	}
	a := it.Cmd.Args
	st, err := it.findFlag('t', target.Pane, 0)
	if err != nil {
		return err
	}
	t := it.Host.Tree()
	w, p := st.W, st.P
	defer it.Host.OptionChanged("")
	if a.Has('Z') {
		if w.Zoomed() {
			t.Unzoom(w)
		} else {
			t.Zoom(p)
		}
		return nil
	}
	t.Unzoom(w)
	const paneMaximum = 10000
	if v, ok := a.Get('x'); ok {
		x, cause := percentage(v, 0, paneMaximum, w.Sx)
		if cause != "" {
			return fmt.Errorf("width %s", cause)
		}
		t.ResizePaneTo(p, true, x)
	}
	if v, ok := a.Get('y'); ok {
		y, cause := percentage(v, 0, paneMaximum, w.Sy)
		if cause != "" {
			return fmt.Errorf("height %s", cause)
		}
		t.ResizePaneTo(p, false, y)
	}
	for _, f := range []byte("UDLR") {
		if !a.Has(f) {
			continue
		}
		v, ok := a.Get(f)
		if !ok {
			v = "1"
			if args := a.Strings(); len(args) != 0 {
				v = args[0]
			}
		}
		adjust, cause := strtonum(v, math.MinInt32, math.MaxInt32)
		if cause != "" {
			return fmt.Errorf("adjustment %s", cause)
		}
		if f == 'L' || f == 'U' {
			adjust = -adjust
		}
		t.ResizePane(p, f == 'L' || f == 'R', adjust)
	}
	return nil
}

// resizeWindow is resize-window: the window gets a manual size.
func resizeWindow(it *Item) error {
	if err := notImplemented(it, "aA"); err != nil {
		return err
	}
	a := it.Cmd.Args
	st, err := it.findFlag('t', target.Window, 0)
	if err != nil {
		return err
	}
	adjust := 1
	if args := a.Strings(); len(args) != 0 {
		n, cause := strtonum(args[0], 1, math.MaxInt32)
		if cause != "" {
			return fmt.Errorf("adjustment %s", cause)
		}
		adjust = n
	}
	w := st.WL.Window
	sx, sy := w.Sx, w.Sy
	const windowMaximum = 10000
	if v, ok := a.Get('x'); ok {
		n, cause := strtonum(v, 1, windowMaximum)
		if cause != "" {
			return fmt.Errorf("width %s", cause)
		}
		sx = n
	}
	if v, ok := a.Get('y'); ok {
		n, cause := strtonum(v, 1, windowMaximum)
		if cause != "" {
			return fmt.Errorf("height %s", cause)
		}
		sy = n
	}
	switch {
	case a.Has('L'):
		if sx >= adjust {
			sx -= adjust
		}
	case a.Has('R'):
		sx += adjust
	case a.Has('U'):
		if sy >= adjust {
			sy -= adjust
		}
	case a.Has('D'):
		sy += adjust
	}
	w.Options.SetNumber("window-size", 2) // manual
	it.Host.Tree().ResizeWindowManual(w, sx, sy)
	it.Host.OptionChanged("window-size")
	return nil
}

// selectLayout is select-layout with a layout string (a saved #{window_layout}, either
// version). The named layouts and -E -n -o -p come later (they are not in i-mux's contract).
func selectLayout(it *Item) error {
	if err := notImplemented(it, "Enop"); err != nil {
		return err
	}
	st, err := it.findFlag('t', target.Pane, 0)
	if err != nil {
		return err
	}
	args := it.Cmd.Args.Strings()
	if len(args) == 0 {
		return errors.New("select-layout without a layout: not implemented by imux-core yet")
	}
	name := args[0]
	switch name {
	case "even-horizontal", "even-vertical", "main-horizontal", "main-horizontal-mirrored",
		"main-vertical", "main-vertical-mirrored", "tiled":
		return fmt.Errorf("select-layout %s: not implemented by imux-core yet", name)
	}
	if err := it.Host.Tree().SelectLayout(st.W, name); err != nil {
		return fmt.Errorf("%v: %s", err, name)
	}
	it.Host.OptionChanged("")
	return nil
}

// capturePane is capture-pane -p: the pane's text printed.
func capturePane(it *Item) error {
	if err := notImplemented(it, "bFHILMPRT"); err != nil {
		return err
	}
	a := it.Cmd.Args
	if !a.Has('p') {
		return errors.New("capture-pane without -p: not implemented by imux-core yet (paste buffers come with C4.9)")
	}
	st, err := it.findFlag('t', target.Pane, 0)
	if err != nil {
		return err
	}
	p := st.P
	if p.Term == nil {
		return nil
	}
	o := vt.CaptureOptions{
		Join: a.Has('J'), Sequences: a.Has('e'), EscapeC: a.Has('C'), KeepTrailing: a.Has('N'),
		TrailingCells: a.Has('T'), Alternate: a.Has('a'),
	}
	line := func(f byte, all *bool, n **int) {
		v, ok := a.Get(f)
		switch {
		case !ok:
		case v == "-":
			*all = true
		default:
			// args_strtonum_and_expand: a format; a bad number is the screen's edge.
			x, cause := strtonum(it.formatSingle(st, v), math.MinInt32, math.MaxInt16)
			if cause == "" {
				*n = &x
			}
		}
	}
	line('S', &o.StartAll, &o.Start)
	line('E', &o.EndAll, &o.End)
	p.Mu.Lock()
	scr := p.Term.Screen
	if o.Alternate && !scr.IsAlternate() {
		p.Mu.Unlock()
		if a.Has('q') {
			it.Out.WriteString("\n")
			return nil
		}
		return errors.New("no alternate screen")
	}
	text := scr.Capture(o)
	p.Mu.Unlock()
	// The last newline is taken off and one is always printed.
	it.Out.WriteString(strings.TrimSuffix(text, "\n") + "\n")
	return nil
}
