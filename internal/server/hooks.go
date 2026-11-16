package server

import (
	"strconv"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/commands"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/model"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/pty"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/target"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/vt"
)

// Events and their hooks (TASKS.md C3.9), as tmux 3.8's events.c fires them: an event's
// hooks are queued (tmux appends them to the global command queue) and run once the event
// that fired them is done; events fired while a hook's command runs are dropped
// (CMDQ_STATE_NOHOOKS).

// fireEvent queues an event's hooks.
func (s *Server) fireEvent(ev commands.Event) {
	if s.opts.Event != nil {
		s.opts.Event(ev.Name, ev.Client)
	}
	if commands.InHook() {
		return
	}
	s.pendingHooks = append(s.pendingHooks, ev)
}

// runHooks runs the queued hooks, in the order their events fired.
func (s *Server) runHooks() {
	for len(s.pendingHooks) > 0 && !s.exiting {
		ev := s.pendingHooks[0]
		s.pendingHooks = s.pendingHooks[1:]
		commands.RunHooks(s, ev)
		s.redrawAll()
	}
}

// fireClient is events_fire_client: the client's session, window and pane are the target.
func (s *Server) fireClient(name string, c *attachedClient) {
	ev := commands.Event{Name: name, Client: c.name, Items: map[string]string{"client": c.name}, Names: map[string]string{}}
	if sess := c.session; sess != nil && s.tree.Session(sess.ID) == sess && sess.Current != nil {
		wl := sess.Current
		ev.Target = target.State{S: sess, WL: wl, Idx: wl.Index, W: wl.Window, P: wl.Window.Active}
		ev.Items["session"] = "$" + strconv.Itoa(sess.ID)
		ev.Names["session"] = sess.Name
		ev.Items["window"] = "@" + strconv.Itoa(wl.Window.ID)
		ev.Names["window"] = wl.Window.Name
		ev.Items["window_index"] = strconv.Itoa(wl.Index)
		if p := wl.Window.Active; p != nil {
			ev.Items["pane"] = "%" + strconv.Itoa(p.ID)
		}
	}
	s.fireEvent(ev)
}

// firePane is events_fire_pane.
func (s *Server) firePane(name string, p *model.Pane) {
	s.fireEvent(commands.PaneEvent(name, p))
}

// activeChanged is the end of window_set_active_pane and window_lost_pane: with
// focus-events on the panes' focus follows, then window-pane-changed fires.
func (s *Server) activeChanged(w *model.Window, p, old *model.Pane) {
	if s.tree.Options.Server.GetNumber("focus-events") != 0 {
		s.updateFocus(w)
	}
	ev := commands.PaneEvent("window-pane-changed", p)
	ev.Items["new_pane"] = "%" + strconv.Itoa(p.ID)
	if old != nil {
		ev.Items["old_pane"] = "%" + strconv.Itoa(old.ID)
	}
	s.fireEvent(ev)
}

// firePaneExit is server_fire_pane_exit: pane-died or pane-exited, with how the program
// ended.
func (s *Server) firePaneExit(name string, p *model.Pane, status pty.ExitStatus) {
	ev := commands.PaneEvent(name, p)
	if status.Signal != 0 {
		ev.Items["exit_signal"] = strconv.Itoa(status.Signal)
	} else {
		ev.Items["exit_status"] = strconv.Itoa(status.Code)
	}
	ev.Items["exit_success"] = "0"
	if status.Signal == 0 && status.Code == 0 {
		ev.Items["exit_success"] = "1"
	}
	s.fireEvent(ev)
}

// remainOnExit is server_destroy_pane's choice: whether a pane whose program ended stays
// (remain-on-exit on, or failed and the program failed).
func remainOnExit(p *model.Pane) bool {
	switch p.Options.GetNumber("remain-on-exit") {
	case 1, 3: // on, key
		return true
	case 2, 4: // failed, failed-key
		return p.Status.Signal != 0 || p.Status.Code != 0
	}
	return false
}

// paneDied keeps a dead pane (remain-on-exit) and fires pane-died. The line tmux writes at
// its bottom (remain-on-exit-format) comes with the pane lifecycle (task C3.12).
func (s *Server) paneDied(p *model.Pane, status pty.ExitStatus) {
	if p.Term != nil {
		p.Mu.Lock()
		p.Term.Screen.Mode &^= vt.ModeCursor
		p.Mu.Unlock()
	}
	s.firePaneExit("pane-died", p, status)
	s.paneChanged(p)
}
