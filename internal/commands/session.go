package commands

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/format"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/model"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/pty"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/target"
)

// The session commands of task C2.3: new-session, has-session, kill-session, kill-server and
// start-server; their targets are looked up as tmux does (internal/target).

// targetSession is the session -t finds.
func targetSession(it *Item) (*model.Session, error) {
	st, err := it.findFlag('t', target.Session, 0)
	return st.S, err
}

func hasSession(it *Item) error {
	_, err := targetSession(it)
	return err
}

func killSession(it *Item) error {
	for _, f := range []byte("aCg") {
		if it.Cmd.Args.Has(f) {
			return fmt.Errorf("kill-session -%c: not implemented by imux-core yet", f)
		}
	}
	s, err := targetSession(it)
	if err != nil {
		return err
	}
	it.Host.RemoveSession(s)
	return nil
}

func killServer(it *Item) error {
	it.Host.KillServer()
	return nil
}

func newSession(it *Item) error {
	a := it.Cmd.Args
	for _, f := range []byte("ADXft") {
		if a.Has(f) {
			return fmt.Errorf("new-session -%c: not implemented by imux-core yet", f)
		}
	}
	// A command-line client attaches to the new session; an attached one (a key binding's)
	// switches to it.
	attach := !a.Has('d') && it.Client != nil && it.Client.Session == nil
	switchTo := !a.Has('d') && it.Client != nil && it.Client.Session != nil
	if attach {
		if err := it.checkAttach(); err != nil {
			return err
		}
	}
	t := it.Host.Tree()
	wname, hasName := a.Get('n')
	if hasName {
		n, err := it.cleanName("window", wname, nil)
		if err != nil {
			return err
		}
		wname = n
	}
	sname, named := a.Get('s')
	if named {
		n, err := it.cleanName("session", sname, nil)
		if err != nil {
			return err
		}
		sname = n
	}
	if named && t.SessionByName(sname) != nil {
		return fmt.Errorf("duplicate session: %s", sname)
	}
	sx, err := size(a, 'x', "width", 80)
	if err != nil {
		return err
	}
	sy, err := size(a, 'y', "height", 24)
	if err != nil {
		return err
	}
	// A session made for a client to attach takes the client's size, less a status line.
	if attach && it.Client.Cols > 0 && it.Client.Rows > 0 {
		sx, sy = it.Client.Cols, it.Client.Rows
		if sy > 0 && t.Options.Session.GetNumber("status") != 0 {
			sy--
		}
		sx, sy = max(sx, 1), max(sy, 1)
	}
	cwd := it.paneCwd(nil)
	// -x and -y give the session a default-size.
	dsx, dsy := sx, sy
	if v, err := size(a, 'x', "width", 80); err == nil && a.Has('x') {
		dsx = v
	}
	if v, err := size(a, 'y', "height", 24); err == nil && a.Has('y') {
		dsy = v
	}
	now := it.Host.Now()
	s, err := t.NewSession(sname, cwd, now)
	if err != nil {
		return err
	}
	if a.Has('x') || a.Has('y') {
		s.Options.SetString("default-size", false, fmt.Sprintf("%dx%d", dsx, dsy))
	}
	// The session's environment: update-environment from the client (not with -E), then -e.
	if env := it.clientEnv(); env != nil && !a.Has('E') {
		environUpdate(t.Options.Session, env, s.Env)
	}
	for _, kv := range a.All('e') {
		if k, v, ok := strings.Cut(kv, "="); ok {
			s.Env[k] = &v
		}
	}
	wl, err := t.NewWindow(s, -1, int(s.Options.GetNumber("base-index")), sx, sy)
	if err != nil {
		t.RemoveSession(s)
		return err
	}
	s.SelectWindow(wl)
	w := wl.Window
	argv := it.commandArgv(s)
	if hasName {
		// A window named when made is not renamed automatically (spawn.c).
		w.Name = wname
		w.Options.SetNumber("automatic-rename", 0)
	} else {
		w.Name = defaultWindowName(argv, it.shell(s))
	}
	p := t.NewPane(w, nil, false, sx, sy)
	spec := it.spawnSpec(s, p, argv, cwd, nil)
	if err := it.Host.Spawn(p, spec); err != nil {
		t.RemoveSession(s)
		return fmt.Errorf("create window failed: %v", err)
	}
	it.printNew("#{session_name}:", s, s.Current, nil)
	if attach {
		it.Client.AttachTo = s
	}
	if switchTo {
		if err := it.Host.SwitchClient(it.Client.Name, s); err != nil {
			return err
		}
	}
	if !a.Has('d') {
		it.setCurrent(sessionState(s))
	}
	it.afterHook("after-new-session", sessionState(s))
	return nil
}

// sessionState is cmd_find_from_session.
func sessionState(s *model.Session) target.State {
	return target.State{S: s, WL: s.Current, Idx: -1, W: s.Current.Window, P: s.Current.Window.Active}
}

// checkAttach is what stops a client attaching: no terminal, or already inside a session.
func (it *Item) checkAttach() error {
	if it.Client.Env["TMUX"] != "" && it.Client.TTY {
		return errors.New("sessions should be nested with care, unset $TMUX to force")
	}
	if !it.Client.TTY {
		return errors.New("open terminal failed: not a terminal")
	}
	return nil
}

// attachSession is attach-session: the client attaches to the target session (by default
// the best one), and with -d the session's other clients are detached.
func attachSession(it *Item) error {
	a := it.Cmd.Args
	for _, f := range []byte("crxf") {
		if a.Has(f) {
			return fmt.Errorf("attach-session -%c: not implemented by imux-core yet", f)
		}
	}
	t := it.Host.Tree()
	if t.Empty() {
		return errors.New("no sessions")
	}
	if it.Client == nil {
		return nil
	}
	if it.Client.Session == nil && it.Client.Env["TMUX"] != "" && it.Client.TTY {
		return errors.New("sessions should be nested with care, unset $TMUX to force")
	}
	tflag, _ := a.Get('t')
	typ, flags := target.Session, target.PreferUnattached
	if strings.ContainsAny(tflag, ":.") {
		typ, flags = target.Pane, 0
	}
	st, err := it.find(tflag, typ, flags)
	if err != nil {
		return err
	}
	s := st.S
	if st.WL != nil {
		if st.P != nil {
			t.SetActivePane(st.W, st.P)
		}
		s.SelectWindow(st.WL)
		it.setCurrent(target.State{S: s, WL: st.WL, Idx: st.WL.Index, W: st.WL.Window, P: st.WL.Window.Active})
	}
	if it.Client.Session == nil && !it.Client.TTY {
		return errors.New("open terminal failed: not a terminal")
	}
	// The session's environment takes the client's update-environment variables (not -E).
	if env := it.clientEnv(); env != nil && !a.Has('E') {
		environUpdate(s.Options, env, s.Env)
	}
	if a.Has('d') {
		for _, c := range it.Host.Clients() {
			if c.Session == s && c.Name != it.Client.Name {
				it.Host.Detach(c.Name)
			}
		}
	}
	if it.Client.Session != nil {
		// An attached client (a key binding's) switches to the session.
		return it.Host.SwitchClient(it.Client.Name, s)
	}
	it.Client.AttachTo = s
	it.Client.AttachedHook = true
	return nil
}

// refreshClient is refresh-client: -S redraws the target client's status line, no flag all of
// it. The other flags (panning, control clients, reports, flags) are not in the contract.
func refreshClient(it *Item) error {
	a := it.Cmd.Args
	for _, f := range []byte("ABcCDflLrFRU") {
		if a.Has(f) {
			return fmt.Errorf("refresh-client -%c: not implemented by imux-core yet", f)
		}
	}
	tc, err := it.findClient('t')
	if err != nil {
		return err
	}
	it.Host.RefreshClient(tc.Name, a.Has('S'))
	return nil
}

// detachClient is detach-client: -t a client (by default the most recently active), -s every
// client of a session, -a every client but the target.
func detachClient(it *Item) error {
	a := it.Cmd.Args
	for _, f := range []byte("EP") {
		if a.Has(f) {
			return fmt.Errorf("detach-client -%c: not implemented by imux-core yet", f)
		}
	}
	// The target client is found first, as tmux does for commands with CMD_CLIENT_TFLAG;
	// -s's session may be missing (CMD_FIND_CANFAIL).
	tc, err := it.findClient('t')
	if err != nil {
		return err
	}
	clients := it.Host.Clients()
	if a.Has('s') {
		st, _ := it.find(mustGet(a, 's'), target.Session, target.CanFail)
		s := st.S
		if s == nil {
			return nil
		}
		for _, c := range clients {
			if c.Session == s {
				it.Host.Detach(c.Name)
			}
		}
		return nil
	}
	if a.Has('a') {
		for _, c := range clients {
			if c.Name != tc.Name {
				it.Host.Detach(c.Name)
			}
		}
		return nil
	}
	it.Host.Detach(tc.Name)
	return nil
}

func mustGet(a *Args, f byte) string {
	v, _ := a.Get(f)
	return v
}

// findClient resolves flag ('t' or 'c') as a client: its name, or when not given the current
// client (tmux's cmd_find_client).
func (it *Item) findClient(flag byte) (*target.Client, error) {
	ctx := it.targetContext()
	name, given := it.Cmd.Args.Get(flag)
	if !given {
		c := target.CurrentClient(ctx)
		if c == nil {
			return nil, errors.New("no current client")
		}
		return c, nil
	}
	if c := target.FindClient(ctx, name); c != nil {
		return c, nil
	}
	return nil, fmt.Errorf("can't find client: %s", strings.TrimSuffix(name, ":"))
}

// size reads -x or -y: 1 to 65535, or "-" for the default (there is no client size here).
func size(a *Args, f byte, what string, def int) (int, error) {
	v, ok := a.Get(f)
	if !ok || v == "-" {
		return def, nil
	}
	n, err := strconv.ParseInt(v, 10, 64)
	switch {
	case err != nil:
		return 0, fmt.Errorf("%s invalid", what)
	case n < 1:
		return 0, fmt.Errorf("%s too small", what)
	case n > 65535:
		return 0, fmt.Errorf("%s too large", what)
	}
	return int(n), nil
}

// cwdFor is server_client_get_cwd: the command client's directory, else the session's, else
// home.
func (it *Item) cwdFor(s *model.Session) string {
	if it.Client != nil && it.Client.Cwd != "" {
		return it.Client.Cwd
	}
	if s != nil && s.Cwd != "" {
		return s.Cwd
	}
	if h := it.getenv("HOME"); h != "" {
		return h
	}
	return "/"
}

// spawnCwd is spawn.c's -c: a format, relative to the client's (or session's) directory.
func (it *Item) spawnCwd(c string, s *model.Session) string {
	cwd := it.formatContext(nil, s, nil, nil).Expand(c)
	if !strings.HasPrefix(cwd, "/") {
		base := it.cwdFor(s)
		if cwd != "" {
			return base + "/" + cwd
		}
		return base
	}
	return cwd
}

// paneCwd is the directory of a new pane: -c, else the client's or session's.
func (it *Item) paneCwd(s *model.Session) string {
	if c, ok := it.Cmd.Args.Get('c'); ok {
		return it.spawnCwd(c, s)
	}
	return it.cwdFor(s)
}

// commandArgv is a new pane's command: the arguments, else default-command.
func (it *Item) commandArgv(s *model.Session) []string {
	argv := it.Cmd.Args.Strings()
	if len(argv) == 0 {
		if c := s.Options.GetString("default-command"); c != "" {
			return []string{c}
		}
	}
	return argv
}

// printNew is -P: the template (-F, else def) expanded for what was made.
func (it *Item) printNew(def string, s *model.Session, wl *model.Winlink, p *model.Pane) {
	if !it.Cmd.Args.Has('P') {
		return
	}
	template, ok := it.Cmd.Args.Get('F')
	if !ok {
		template = def
	}
	it.Print(it.formatContext(it.clientInfoOf(it.targetClient()), s, wl, p).Expand(template))
}

func (it *Item) getenv(name string) string {
	prefix := name + "="
	for _, kv := range it.Host.Environ() {
		if v, ok := strings.CutPrefix(kv, prefix); ok {
			return v
		}
	}
	return ""
}

// shell is the session's default-shell when it is a suitable shell, else /bin/sh (tmux's
// spawn.c).
func (it *Item) shell(s *model.Session) string {
	oo := it.Host.Tree().Options.Session
	if s != nil {
		oo = s.Options
	}
	if sh := oo.GetString("default-shell"); CheckShell(sh) {
		return sh
	}
	return "/bin/sh"
}

// CheckShell is tmux's checkshell: an absolute path to an executable that is not the engine.
func CheckShell(sh string) bool {
	return sh != "" && filepath.IsAbs(sh) && isExecutable(sh) == nil && filepath.Base(sh) != "imux-core"
}

// spawnSpec is how tmux's spawn.c runs a pane's program: several words with execvp, one word
// with the shell's -c, none as a login shell, with the environment paneEnv builds.
func (it *Item) spawnSpec(s *model.Session, p *model.Pane, argv []string, cwd string, extra []string) pty.Spec {
	sh := it.shell(s)
	p.Argv, p.Cwd, p.Shell = append([]string(nil), argv...), cwd, sh
	// tmux's child goes to the directory, else home, else /.
	if !isDir(cwd) {
		cwd = "/"
		if h := it.getenv("HOME"); h != "" && isDir(h) {
			cwd = h
		}
	}
	spec := pty.Spec{Dir: cwd}
	switch len(argv) {
	case 0:
		spec.Path, spec.Argv = sh, []string{"-" + filepath.Base(sh)}
	case 1:
		spec.Path, spec.Argv = sh, []string{filepath.Base(sh), "-c", argv[0]}
	default:
		spec.Argv = append([]string(nil), argv...)
	}
	spec.Env = it.paneEnv(s, p, extra, sh, cwd)
	return spec
}

// defaultWindowName is default_window_name: the command line made a name, or the shell's.
func defaultWindowName(argv []string, shell string) string {
	if len(argv) == 0 {
		return format.ParseWindowName(shell)
	}
	words := make([]string, len(argv))
	for i, a := range argv {
		words[i] = Escape(a)
	}
	return format.ParseWindowName(strings.Join(words, " "))
}

// isExecutable is tmux's access(X_OK) check on the shell.
func isExecutable(p string) error {
	fi, err := os.Stat(p)
	if err != nil {
		return err
	}
	if fi.IsDir() || fi.Mode().Perm()&0o111 == 0 {
		return errors.New("not executable")
	}
	return nil
}

func isDir(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}
