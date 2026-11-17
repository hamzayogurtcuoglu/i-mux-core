package server

import (
	"errors"
	"io"
	"io/fs"
	"net"
	"os"
	"os/exec"
	"path"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/status"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/buffers"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/commands"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/model"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/pty"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/vt"
)

// The server loop (ARCHITECTURE.md §4, E2): one goroutine owns the tree; everything that
// changes it arrives as an event on its channel, in order. Each pane has a reader goroutine
// that feeds the pane's screen under the pane's mutex and tells the loop, at most one notice
// pending at a time, that the pane changed.

// Options configure a server.
type Options struct {
	PTY          pty.PTY          // nil: pty.New()
	HistoryLimit int              // history-limit; 0: tmux's default, 2000
	Now          func() time.Time // nil: time.Now
	// PaneChanged is called in the loop when a pane's screen has changed (coalesced).
	PaneChanged func(p *model.Pane)
	// Event is called in the loop when a client or pane event happens that hooks can be set
	// for (client-focus-in, client-dark-theme, pane-focus-in, …), with the client's name if
	// any; task C3.9 runs the hooks.
	Event func(name, client string)

	SocketPath string   // the server's socket, for $TMUX
	Environ    []string // the global environment panes start with; nil: os.Environ()
	// HoldForClient keeps the server up, though it has no session, until its first client
	// has connected or HoldTimeout has passed: the client that started it is on its way.
	HoldForClient bool
	HoldTimeout   time.Duration // 0: 5 s
}

// Server is the server's state and loop.
type Server struct {
	opts   Options
	tree   *model.Tree
	events chan func()
	stop   chan struct{}
	done   chan struct{}

	readers sync.WaitGroup
	panes   map[*model.Pane]*paneState
	menus   map[*model.Window]*menu // each window's menu (task C4.7)
	buffers *buffers.Store          // the paste buffers (task C4.9)
	// promptHistory is the command prompts' history (task C4.10).
	promptHistory *status.History
	exiting       bool

	latest map[*model.Window]*attachedClient // the client that used each window last

	nameTimers map[*model.Window]bool // windows with an automatic-rename timer pending

	pendingHooks []commands.Event // events whose hooks run once the current event is done
	cfgCauses    []string         // the startup config's errors (tmux's cfg_causes)

	started time.Time // when the server started (start_time)

	clients  int               // connected clients (in the loop)
	attached []*attachedClient // the attached ones, in the order they attached
	held     bool              // HoldForClient, until the first client
	killed   bool              // kill-server

	connMu sync.Mutex
	conns  map[net.Conn]struct{}
	connWG sync.WaitGroup
}

type paneState struct {
	pending atomic.Bool // a change notice is waiting in the loop

	// The loop's: whether the pane has the focus (PANE_FOCUSED), and the theme it was last
	// told (PANE_THEMECHANGED, last_theme).
	focused      bool
	themeChanged bool
	lastTheme    vt.Theme
}

// New returns a server; Run starts its loop.
func New(o Options) *Server {
	if o.PTY == nil {
		o.PTY = pty.New()
	}
	if o.HistoryLimit == 0 {
		o.HistoryLimit = 2000
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Environ == nil {
		o.Environ = os.Environ()
	}
	if o.HoldTimeout == 0 {
		o.HoldTimeout = 5 * time.Second
	}
	s := &Server{
		opts:          o,
		tree:          model.NewTree(),
		events:        make(chan func(), 256),
		stop:          make(chan struct{}),
		done:          make(chan struct{}),
		panes:         map[*model.Pane]*paneState{},
		menus:         map[*model.Window]*menu{},
		buffers:       buffers.New(),
		promptHistory: &status.History{},
		held:          o.HoldForClient,
		conns:         map[net.Conn]struct{}{},
		latest:        map[*model.Window]*attachedClient{},

		nameTimers: map[*model.Window]bool{},
	}
	s.started = o.Now()
	s.tree.Resized = s.paneResized
	s.tree.ActiveChanged = s.activeChanged
	s.startOptions()
	// tmux's default key bindings (task C3.8), before any config file changes them.
	commands.LoadDefaultKeyBindings(s)
	return s
}

// startOptions sets the options tmux sets when its server starts (tmux.c): default-shell from
// SHELL, and with VISUAL or EDITOR, editor and vi or emacs keys.
func (s *Server) startOptions() {
	g := s.tree.Options
	getenv := func(name string) (string, bool) {
		for _, kv := range s.opts.Environ {
			if v, ok := strings.CutPrefix(kv, name+"="); ok {
				return v, true
			}
		}
		return "", false
	}
	shell, _ := getenv("SHELL")
	if !commands.CheckShell(shell) {
		shell = "/bin/sh"
	}
	g.Session.SetString("default-shell", false, shell)
	ed, ok := getenv("VISUAL")
	if !ok {
		ed, ok = getenv("EDITOR")
	}
	if ok {
		g.Server.SetString("editor", false, ed)
		mode := int64(0) // emacs
		if strings.Contains(path.Base(ed), "vi") {
			mode = 1
		}
		g.Session.SetNumber("status-keys", mode)
		g.Window.SetNumber("mode-keys", mode)
	}
}

// paneResized resizes a pane's screen and its program's terminal when its layout cell changes
// size (in the loop).
func (s *Server) paneResized(p *model.Pane) {
	if p.Term != nil {
		p.Mu.Lock()
		p.Term.Resize(p.Sx, p.Sy)
		p.Mu.Unlock()
	}
	if p.Proc != nil {
		_ = p.Proc.Resize(p.Sx, p.Sy)
	}
	// The pane's mode follows (the mode's resize callback).
	if r, ok := p.Mode.(interface{ Resize(sx, sy int) }); ok {
		r.Resize(p.Sx, p.Sy)
	}
}

// Run runs the loop until Shutdown, then waits for the pane readers to end.
func (s *Server) Run() {
	defer close(s.done)
	if s.held {
		time.AfterFunc(s.opts.HoldTimeout, func() {
			s.post(func() {
				s.held = false
				s.checkExit()
			})
		})
	}
	for {
		select {
		case f := <-s.events:
			f()
			if !s.exiting {
				s.runHooks()
				s.checkNames()
			}
			if s.exiting {
				s.shutdown()
				s.finish()
				return
			}
		case <-s.stop:
			s.shutdown()
			s.finish()
			return
		}
	}
}

// finish waits for the pane readers to end, dropping the events they still post so none of
// them stays blocked.
func (s *Server) finish() {
	ended := make(chan struct{})
	go func() {
		s.readers.Wait()
		close(ended)
	}()
	for {
		select {
		case <-s.events:
		case <-ended:
			return
		}
	}
}

// Shutdown ends the server: every pane's program is killed and the loop returns.
func (s *Server) Shutdown() {
	select {
	case <-s.stop:
	default:
		close(s.stop)
	}
	<-s.done
}

// Done is closed once the loop has returned.
func (s *Server) Done() <-chan struct{} { return s.done }

// post queues an event for the loop. It returns false once the loop has ended.
func (s *Server) post(f func()) bool {
	select {
	case s.events <- f:
		return true
	case <-s.done:
		return false
	}
}

// Do runs f in the loop with the tree, and waits for it.
func (s *Server) Do(f func(t *model.Tree)) error {
	ran := make(chan struct{})
	if !s.post(func() { f(s.tree); close(ran) }) {
		return ErrStopped
	}
	select {
	case <-ran:
		return nil
	case <-s.done:
		return ErrStopped
	}
}

// ErrStopped is returned once the server has ended.
var ErrStopped = errors.New("server: stopped")

// Spawn starts a program in pane p (in the loop): a screen of the pane's size and a reader.
func (s *Server) Spawn(p *model.Pane, spec pty.Spec) error {
	spec.Cols, spec.Rows = p.Sx, p.Sy
	proc, err := s.opts.PTY.Start(spec)
	var ee *exec.Error
	var pe *fs.PathError
	if errors.As(err, &ee) || errors.As(err, &pe) {
		// tmux forks before it runs the program: one that cannot run makes a pane that ends at
		// once, with status 1, and the command succeeds.
		proc, err = deadProc{}, nil
	}
	if err != nil {
		return err
	}
	p.Proc = proc
	limit := s.opts.HistoryLimit
	if w := p.Window; w != nil && w.Session() != nil {
		limit = int(w.Session().Options.GetNumber("history-limit"))
	}
	p.Term = vt.NewTerminal(p.Sx, p.Sy, limit)
	// window_pane_create: a pane's title starts as the host name.
	if h, err := os.Hostname(); err == nil {
		p.Term.Screen.Title = h
	}
	p.Term.Rename = func(name string) {
		// Called from the pane's reader with the pane locked: the loop applies it later.
		go s.post(func() { s.renamePane(p, name) })
	}
	p.Term.Screen.DisallowSetTitle = p.Options.GetNumber("allow-set-title") == 0
	p.Term.ClipboardMode = s.clipboardMode()
	p.Term.SetClipboard = func(clip string, b []byte) {
		// Called from the pane's reader with the pane locked: the loop applies it later.
		data := append([]byte(nil), b...)
		go s.post(func() { s.paneClipboard(p, clip, data) })
	}
	p.Term.ExtendedKeys = s.extendedKeys()
	if p.Term.ExtendedKeys == vt.ExtendedKeysAlways {
		// As tmux's screen_init: the screen starts in extended keys mode 1.
		p.Term.Screen.Mode |= vt.ModeKeysExtended
	}
	st := &paneState{}
	s.panes[p] = st
	s.readers.Add(1)
	go s.read(p, st)
	return nil
}

type deadliner interface{ SetReadDeadline(time.Time) error }

// read is a pane's reader goroutine.
func (s *Server) read(p *model.Pane, st *paneState) {
	defer s.readers.Done()
	buf := make([]byte, 32<<10)
	dl, _ := p.Proc.(deadliner)
	for {
		n, err := p.Proc.Read(buf)
		if n > 0 {
			p.Mu.Lock()
			_, _ = p.Term.Write(buf[:n])
			replies := p.Term.DrainReplies()
			inString := p.Term.InString()
			p.Mu.Unlock()
			if len(replies) > 0 {
				_, _ = p.Proc.Write(replies)
			}
			if dl != nil {
				// tmux's ground timer: an unterminated string is given up on after a quiet spell.
				if inString {
					_ = dl.SetReadDeadline(time.Now().Add(vt.GroundTimeout))
				} else {
					_ = dl.SetReadDeadline(time.Time{})
				}
			}
			if !st.pending.Swap(true) {
				s.post(func() {
					st.pending.Store(false)
					if s.panes[p] != st {
						return
					}
					p.Changed = true
					s.paneChanged(p)
					if s.opts.PaneChanged != nil {
						s.opts.PaneChanged(p)
					}
				})
			}
		}
		if errors.Is(err, os.ErrDeadlineExceeded) {
			p.Mu.Lock()
			p.Term.ExpireString()
			p.Mu.Unlock()
			_ = dl.SetReadDeadline(time.Time{})
			continue
		}
		if err != nil {
			if !errors.Is(err, io.EOF) && !errors.Is(err, os.ErrClosed) {
				_ = err // a read error ends the pane like its end of output
			}
			break
		}
	}
	status, _ := p.Proc.Wait()
	s.post(func() { s.paneExited(p, st, status) })
}

// paneExited handles a pane whose program ended: it goes, then its window if empty, then its
// session if empty, and the server ends with the last session (exit-empty). remain-on-exit
// comes with task C3.12.
func (s *Server) paneExited(p *model.Pane, st *paneState, status pty.ExitStatus) {
	if s.panes[p] != st {
		return
	}
	delete(s.panes, p)
	p.Dead, p.Status = true, status
	_ = p.Proc.Close()
	if s.tree.Pane(p.ID) == nil {
		return
	}
	if remainOnExit(p) {
		s.paneDied(p, status)
		return
	}
	s.firePaneExit("pane-exited", p, status)
	s.RemovePane(p)
}

// Respawn implements commands.Host: respawn-pane's new program in an existing pane. The old
// one is killed; the screen starts again, keeping the pane's title (screen_reinit).
func (s *Server) Respawn(p *model.Pane, spec pty.Spec) error {
	if _, ok := s.panes[p]; ok {
		_ = p.Proc.Kill()
		_ = p.Proc.Close()
		delete(s.panes, p)
	}
	title := ""
	if p.Term != nil {
		p.Mu.Lock()
		title = p.Term.Screen.Title
		p.Mu.Unlock()
	}
	p.Dead, p.Status, p.DeadTime = false, pty.ExitStatus{}, time.Time{}
	if err := s.Spawn(p, spec); err != nil {
		return err
	}
	if title != "" {
		p.Term.Screen.Title = title
	}
	s.paneChanged(p)
	return nil
}

// RemovePane takes a pane out (in the loop), killing its program, and its window and session
// when they are left empty. The server ends when no session is left.
func (s *Server) RemovePane(p *model.Pane) {
	if _, ok := s.panes[p]; ok {
		_ = p.Proc.Kill()
		_ = p.Proc.Close()
		delete(s.panes, p)
	}
	w := p.Window
	if s.tree.RemovePane(p) {
		sess := w.Session()
		if s.tree.RemoveWindow(w) {
			s.tree.RemoveSession(sess)
			s.sessionGone(sess)
		}
	}
	s.redrawAll()
	s.checkExit()
}

// checkExit ends the server when it has no session and no client, as tmux's exit-empty does,
// or once kill-server has run.
func (s *Server) checkExit() {
	if s.killed || (s.tree.Empty() && s.clients == 0 && !s.held) {
		s.exiting = true
	}
}

// shutdown tells the attached clients the server is going and kills every pane's program (in
// the loop).
func (s *Server) shutdown() {
	s.shutdownClients()
	for p := range s.panes {
		_ = p.Proc.Kill()
		_ = p.Proc.Close()
		delete(s.panes, p)
	}
}

// deadProc is a pane's program that could not run: it ends at once with status 1.
type deadProc struct{}

func (deadProc) Read([]byte) (int, error)      { return 0, io.EOF }
func (deadProc) Write([]byte) (int, error)     { return 0, os.ErrClosed }
func (deadProc) Resize(int, int) error         { return nil }
func (deadProc) Wait() (pty.ExitStatus, error) { return pty.ExitStatus{Code: 1}, nil }
func (deadProc) Kill() error                   { return nil }
func (deadProc) Pid() int                      { return 0 }
func (deadProc) Fd() int                       { return -1 }
func (deadProc) Close() error                  { return nil }
