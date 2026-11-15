package commands

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/model"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/pty"
)

// The session commands of task C2.3: new-session (detached), has-session, kill-session,
// kill-server and start-server. Target resolution here covers sessions only; the full rules
// come with task C3.3.

// findSession resolves a session target (-t) as tmux's cmd_find does for sessions: none is
// the best session; "$id"; "=name" exactly; else a name exactly, then a unique prefix of one,
// then a unique fnmatch pattern. A window or pane part after ':' is ignored.
func findSession(t *model.Tree, target string, given bool) (*model.Session, error) {
	if !given {
		if s := t.BestSession(); s != nil {
			return s, nil
		}
		return nil, errors.New("no current target")
	}
	name := target
	if i := strings.IndexByte(name, ':'); i >= 0 {
		name = name[:i]
	}
	fail := func(n string) error { return fmt.Errorf("can't find session: %s", n) }
	if strings.HasPrefix(name, "$") {
		if id, err := strconv.Atoi(name[1:]); err == nil {
			if s := t.Session(id); s != nil {
				return s, nil
			}
		}
		return nil, fail(name)
	}
	if exact, ok := strings.CutPrefix(name, "="); ok {
		if s := t.SessionByName(exact); s != nil {
			return s, nil
		}
		return nil, fail(exact)
	}
	if s := t.SessionByName(name); s != nil {
		return s, nil
	}
	var found *model.Session
	n := 0
	for _, s := range t.Sessions() {
		if strings.HasPrefix(s.Name, name) {
			found, n = s, n+1
		}
	}
	if n == 1 {
		return found, nil
	}
	if n == 0 {
		for _, s := range t.Sessions() {
			if ok, _ := path.Match(name, s.Name); ok {
				found, n = s, n+1
			}
		}
		if n == 1 {
			return found, nil
		}
	}
	return nil, fail(name)
}

func targetSession(it *Item) (*model.Session, error) {
	t, given := it.Cmd.Args.Get('t')
	return findSession(it.Host.Tree(), t, given)
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
	for _, f := range []byte("ADEPXFft") {
		if a.Has(f) {
			return fmt.Errorf("new-session -%c: not implemented by imux-core yet", f)
		}
	}
	attach := !a.Has('d') && it.Client != nil
	if attach {
		if err := it.checkAttach(); err != nil {
			return err
		}
	}
	t := it.Host.Tree()
	sname, named := a.Get('s')
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
	cwd, ok := a.Get('c')
	if !ok {
		cwd = it.clientCwd()
	}
	now := it.Host.Now()
	s, err := t.NewSession(sname, cwd, now)
	if err != nil {
		return err
	}
	wl, err := t.NewWindow(s, -1, 0, sx, sy)
	if err != nil {
		t.RemoveSession(s)
		return err
	}
	s.SelectWindow(wl)
	w := wl.Window
	if n, ok := a.Get('n'); ok {
		w.Name = n
	} else {
		w.Name = defaultWindowName(a.Strings(), it.shell())
	}
	p := t.NewPane(w, nil, false, sx, sy)
	spec := it.spawnSpec(s, p, a.Strings(), cwd, a.All('e'))
	if err := it.Host.Spawn(p, spec); err != nil {
		t.RemoveSession(s)
		return fmt.Errorf("create window failed: %v", err)
	}
	if attach {
		it.Client.AttachTo = s
	}
	return nil
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
	for _, f := range []byte("cErxf") {
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
	if it.Client.Env["TMUX"] != "" && it.Client.TTY {
		return errors.New("sessions should be nested with care, unset $TMUX to force")
	}
	s, err := targetSession(it)
	if err != nil {
		return err
	}
	if !it.Client.TTY {
		return errors.New("open terminal failed: not a terminal")
	}
	if a.Has('d') {
		for _, c := range it.Host.Clients() {
			if c.Session == s && c.Name != it.Client.Name {
				it.Host.Detach(c.Name)
			}
		}
	}
	it.Client.AttachTo = s
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
	clients := it.Host.Clients()
	target, err := findClient(clients, a)
	if err != nil {
		return err
	}
	if a.Has('s') {
		s, err := findSession(it.Host.Tree(), mustGet(a, 's'), true)
		if err != nil {
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
			if c.Name != target.Name {
				it.Host.Detach(c.Name)
			}
		}
		return nil
	}
	it.Host.Detach(target.Name)
	return nil
}

func mustGet(a *Args, f byte) string {
	v, _ := a.Get(f)
	return v
}

// findClient resolves -t as a client: its name, or without -t the most recently active one.
func findClient(clients []ClientInfo, a *Args) (ClientInfo, error) {
	name, given := a.Get('t')
	if !given {
		var best ClientInfo
		for _, c := range clients {
			if best.Name == "" || c.Activity.After(best.Activity) {
				best = c
			}
		}
		if best.Name == "" {
			return best, errors.New("no current client")
		}
		return best, nil
	}
	for _, c := range clients {
		if c.Name == name || strings.TrimPrefix(c.Name, "/dev/") == name {
			return c, nil
		}
	}
	return ClientInfo{}, fmt.Errorf("can't find client: %s", name)
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

func (it *Item) clientCwd() string {
	if it.Client != nil && it.Client.Cwd != "" {
		return it.Client.Cwd
	}
	if h := it.getenv("HOME"); h != "" {
		return h
	}
	return "/"
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

// shell is tmux's default-shell: $SHELL when it is an absolute path to an executable that is
// not this program, else /bin/sh.
func (it *Item) shell() string {
	sh := it.getenv("SHELL")
	if sh == "" || !filepath.IsAbs(sh) || isExecutable(sh) != nil || filepath.Base(sh) == "imux-core" {
		return "/bin/sh"
	}
	return sh
}

// spawnSpec is how tmux's spawn.c runs a pane's program: several words with execvp, one word
// with the shell's -c, none as a login shell. The environment is the server's, with TERM,
// TMUX and TMUX_PANE set.
func (it *Item) spawnSpec(s *model.Session, p *model.Pane, argv []string, cwd string, extra []string) pty.Spec {
	sh := it.shell()
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
	set := map[string]string{
		"TERM":      "tmux-256color",
		"TMUX":      fmt.Sprintf("%s,%d,%d", it.Host.SocketPath(), it.Host.Pid(), s.ID),
		"TMUX_PANE": fmt.Sprintf("%%%d", p.ID),
	}
	for _, kv := range extra {
		if k, v, ok := strings.Cut(kv, "="); ok {
			set[k] = v
		}
	}
	for _, kv := range it.Host.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		if _, ok := set[k]; !ok {
			spec.Env = append(spec.Env, kv)
		}
	}
	for k, v := range set {
		spec.Env = append(spec.Env, k+"="+v)
	}
	return spec
}

// defaultWindowName is tmux's name for a new window: the program's base name ("exec " and
// arguments dropped from a shell command), or the shell's.
func defaultWindowName(argv []string, shell string) string {
	if len(argv) == 0 {
		return filepath.Base(shell)
	}
	cmd := argv[0]
	if len(argv) == 1 {
		cmd = strings.TrimSpace(cmd)
		cmd = strings.TrimPrefix(cmd, "exec ")
		cmd = strings.TrimLeft(cmd, " ")
		if i := strings.IndexAny(cmd, " \t"); i >= 0 {
			cmd = cmd[:i]
		}
	}
	return filepath.Base(cmd)
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
