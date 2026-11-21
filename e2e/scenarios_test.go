//go:build e2e

package e2e

import (
	"strings"
	"testing"
	"time"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/difftest"
)

// The engine end-to-end suite (TASKS.md C6.1): realistic scenarios against imux-core alone,
// each with its own server, home and socket, driving attached clients in PTYs and checking
// their screens, the panes and formats. Panes run cat, printf or the fake TUI.

// clientName is the name of the only attached client.
func (w *world) clientName() string {
	w.t.Helper()
	names := strings.Fields(w.ok("list-clients", "-F", "#{client_name}"))
	if len(names) != 1 {
		w.t.Fatalf("clients: %q, want one", names)
	}
	return names[0]
}

// Attaching, detaching, reattaching.

func TestAttachShowsSession(t *testing.T) {
	w := newWorld(t)
	w.ok("new-session", "-d", "-s", "a", "printf 'hello from a\\n'; exec cat")
	c := w.attach(80, 24)
	c.waitText("hello from a")
	if got := w.ok("list-clients", "-F", "#{client_session}|#{client_width}x#{client_height}"); got != "a|80x24\n" {
		t.Fatalf("list-clients: %q", got)
	}
}

func TestDetachKeepsSession(t *testing.T) {
	w := newWorld(t)
	w.ok("new-session", "-d", "-s", "a", "exec cat")
	c := w.attach(80, 24)
	w.waitFormat("a", "#{session_attached}", "1")
	w.ok("detach-client", "-s", "a")
	c.waitText("[detached")
	w.ok("has-session", "-t", "a")
	w.waitFormat("a", "#{session_attached}", "0")
}

func TestReattachShowsTheSameScreen(t *testing.T) {
	w := newWorld(t)
	w.ok("new-session", "-d", "-s", "a", "printf 'still here\\n'; exec cat")
	c := w.attach(80, 24)
	c.waitText("still here")
	w.ok("detach-client", "-s", "a")
	c.waitText("[detached")
	c2 := w.attach(80, 24)
	c2.waitText("still here")
}

func TestPrefixDDetaches(t *testing.T) {
	w := newWorld(t)
	w.ok("new-session", "-d", "-s", "a", "exec cat")
	c := w.attach(80, 24)
	w.waitFormat("a", "#{session_attached}", "1")
	c.typ("\x02d")
	c.waitText("[detached")
	w.waitFormat("a", "#{session_attached}", "0")
}

func TestUnknownSessionFails(t *testing.T) {
	w := newWorld(t)
	w.ok("new-session", "-d", "-s", "a", "exec cat")
	w.fails("can't find session", "has-session", "-t", "nope")
	w.fails("can't find session", "kill-session", "-t", "nope")
}

func TestSwitchClientBetweenSessions(t *testing.T) {
	w := newWorld(t)
	w.ok("new-session", "-d", "-s", "a", "printf 'session a\\n'; exec cat")
	w.ok("new-session", "-d", "-s", "b", "printf 'session b\\n'; exec cat")
	c := w.attach(80, 24)
	w.ok("switch-client", "-c", w.clientName(), "-t", "a")
	c.waitText("session a")
	w.ok("switch-client", "-c", w.clientName(), "-t", "b")
	c.waitText("session b")
	if got := w.ok("list-clients", "-F", "#{client_session}"); got != "b\n" {
		t.Fatalf("client session: %q", got)
	}
}

// Splits and kills.

func TestSplitWindow(t *testing.T) {
	w := newWorld(t)
	w.ok("new-session", "-d", "-s", "a", "-x", "80", "-y", "24", "exec cat")
	w.ok("split-window", "-h", "-t", "a", "exec cat")
	w.ok("split-window", "-v", "-l", "8", "-t", "a", "exec cat")
	got := w.ok("list-panes", "-t", "a", "-F", "#{pane_index}:#{pane_width}x#{pane_height}")
	if got != "0:40x24\n1:39x15\n2:39x8\n" {
		t.Fatalf("panes: %q", got)
	}
}

func TestKillPaneGivesBackItsSpace(t *testing.T) {
	w := newWorld(t)
	w.ok("new-session", "-d", "-s", "a", "-x", "80", "-y", "24", "exec cat")
	w.ok("split-window", "-h", "-t", "a", "exec cat")
	w.ok("kill-pane", "-t", "%1")
	if got := w.ok("list-panes", "-t", "a", "-F", "#{pane_id}:#{pane_width}"); got != "%0:80\n" {
		t.Fatalf("panes: %q", got)
	}
}

func TestKillWindowMovesTheClient(t *testing.T) {
	w := newWorld(t)
	w.ok("new-session", "-d", "-s", "a", "-n", "first", "printf 'first window\\n'; exec cat")
	w.ok("new-window", "-t", "a", "-n", "second", "printf 'second window\\n'; exec cat")
	c := w.attach(80, 24)
	c.waitText("second window")
	w.ok("kill-window", "-t", "a:second")
	c.waitText("first window")
	if got := w.format("a", "#{session_windows}|#{window_name}"); got != "1|first" {
		t.Fatalf("after kill-window: %q", got)
	}
}

func TestKillSessionEndsItsClient(t *testing.T) {
	w := newWorld(t)
	w.ok("new-session", "-d", "-s", "a", "exec cat")
	w.ok("new-session", "-d", "-s", "b", "exec cat")
	c := w.attach(80, 24)
	w.waitFormat("b", "#{session_attached}", "1")
	w.ok("kill-session", "-t", "b")
	c.waitText("[exited]")
	w.ok("has-session", "-t", "a")
}

func TestRespawnPane(t *testing.T) {
	w := newWorld(t)
	w.ok("new-session", "-d", "-s", "a", "printf 'first run\\n'; exec cat")
	w.waitCapture("%0", "first run")
	w.ok("respawn-pane", "-k", "-t", "%0", "printf 'second run\\n'; exec cat")
	w.waitCapture("%0", "second run")
}

func TestZoomShowsOnePane(t *testing.T) {
	w := newWorld(t)
	w.ok("new-session", "-d", "-s", "a", "printf 'left side\\n'; exec cat")
	w.ok("split-window", "-h", "-t", "a", "printf 'right side\\n'; exec cat")
	c := w.attach(80, 24)
	c.waitText("left side")
	w.ok("resize-pane", "-Z", "-t", "%1")
	c.waitText("left side", true)
	if got := w.format("a", "#{window_zoomed_flag}|#{pane_width}"); got != "1|80" {
		t.Fatalf("zoomed: %q", got)
	}
	w.ok("resize-pane", "-Z", "-t", "%1")
	c.waitText("left side")
}

func TestSwapPane(t *testing.T) {
	w := newWorld(t)
	w.ok("new-session", "-d", "-s", "a", "exec cat")
	w.ok("split-window", "-h", "-t", "a", "exec cat")
	w.ok("swap-pane", "-s", "%0", "-t", "%1")
	if got := w.ok("list-panes", "-t", "a", "-F", "#{pane_index}:#{pane_id}"); got != "0:%1\n1:%0\n" {
		t.Fatalf("after swap-pane: %q", got)
	}
}

func TestDeadPaneStays(t *testing.T) {
	w := newWorld(t)
	w.ok("new-session", "-d", "-s", "a", "exec cat")
	w.ok("set", "-g", "remain-on-exit", "on")
	w.ok("split-window", "-t", "a", "exit 3")
	w.waitFormat("%1", "#{pane_dead}|#{pane_dead_status}", "1|3")
	c := w.attach(80, 24)
	c.waitText("Pane is dead")
}

// Popups and menus.

func TestPopupTakesTheKeys(t *testing.T) {
	w := newWorld(t)
	w.ok("new-session", "-d", "-s", "a", "exec cat")
	c := w.attach(80, 24)
	w.waitFormat("a", "#{session_attached}", "1")
	// A command-line client waits for its popup to close.
	done := w.start("display-popup", "-c", w.clientName(), "-E", "-w", "40", "-h", "10", "faketui -mode keys")
	c.waitText("READY")
	c.typ("q")
	c.waitText("71")
	c.typ("\x04")
	c.waitText("READY", true)
	if r := <-done; r.Code != 0 {
		t.Fatalf("display-popup: %+v", r)
	}
}

func TestPopupClosesWhenItsProgramExits(t *testing.T) {
	w := newWorld(t)
	w.ok("new-session", "-d", "-s", "a", "printf 'under the popup\\n'; exec cat")
	c := w.attach(80, 24)
	c.waitText("under the popup")
	done := w.start("display-popup", "-c", w.clientName(), "-E", "-w", "30", "-h", "6", "printf 'in the popup'; sleep 1")
	c.waitText("in the popup")
	c.waitText("in the popup", true)
	c.waitText("under the popup")
	if r := <-done; r.Code != 0 {
		t.Fatalf("display-popup: %+v", r)
	}
}

func TestMenuKeyRunsItsItem(t *testing.T) {
	w := newWorld(t)
	w.ok("new-session", "-d", "-s", "a", "exec cat")
	c := w.attach(80, 24)
	w.waitFormat("a", "#{session_attached}", "1")
	w.ok("display-menu", "-c", w.clientName(), "-x", "C", "-y", "C", "-T", "Choose", "Pick me", "p", "set -g @picked yes", "Other", "o", "set -g @picked no")
	c.waitText("Pick me")
	c.typ("p")
	w.waitFormat("", "#{@picked}", "yes")
	c.waitText("Pick me", true)
}

// A menu item chosen by its key closes the menu before its command runs, as tmux's menu_key_cb
// does: the menu is not drawn again (with the choice), and a menu the command opens stays open
// (C6.3, from i-mux's pin list).
func TestMenuClosesBeforeItsItemRuns(t *testing.T) {
	w := newWorld(t)
	w.ok("new-session", "-d", "-s", "a", "exec cat")
	c := w.attach(80, 24)
	w.waitFormat("a", "#{session_attached}", "1")
	w.ok("display-menu", "-c", w.clientName(), "-x", "C", "-y", "C", "-T", "Old", "First row", "1", "display-menu -x C -y C -T New 'Next row' n ''")
	c.waitText("First row")
	time.Sleep(100 * time.Millisecond) // the menu's frame is out
	mark := len(c.output())
	c.typ("1")
	c.waitText("Next row")
	if out := plain(c.output()[mark:]); strings.Contains(out, "First row") {
		t.Fatalf("the menu was drawn again after its choice: %q", out)
	}
}

func TestMenuEscapeCloses(t *testing.T) {
	w := newWorld(t)
	w.ok("new-session", "-d", "-s", "a", "exec cat")
	c := w.attach(80, 24)
	w.waitFormat("a", "#{session_attached}", "1")
	w.ok("display-menu", "-c", w.clientName(), "-x", "C", "-y", "C", "Close me", "c", "set -g @picked yes")
	c.waitText("Close me")
	c.typ("\x1b")
	c.waitText("Close me", true)
	if got := w.format("", "#{@picked}"); got != "" {
		t.Fatalf("@picked: %q", got)
	}
}

// Copy and paste.

func TestPasteBuffer(t *testing.T) {
	w := newWorld(t)
	w.ok("new-session", "-d", "-s", "a", "exec cat")
	w.ok("set-buffer", "pasted text")
	w.ok("paste-buffer", "-t", "%0")
	w.waitCapture("%0", "pasted text")
}

func TestBracketedPaste(t *testing.T) {
	w := newWorld(t)
	w.ok("new-session", "-d", "-s", "a", "faketui -mode paste")
	w.waitCapture("%0", "READY")
	w.ok("set-buffer", "bracketed")
	w.ok("paste-buffer", "-p", "-t", "%0")
	w.waitCapture("%0", "bracketed")
}

func TestCopyModeSelectionToBuffer(t *testing.T) {
	w := newWorld(t)
	w.ok("new-session", "-d", "-s", "a", "printf 'copy this line\\nnext\\n'; exec cat")
	w.waitCapture("%0", "next")
	w.ok("copy-mode", "-t", "%0")
	w.waitFormat("%0", "#{pane_in_mode}", "1")
	for _, cmd := range []string{"cursor-up", "cursor-up", "start-of-line", "begin-selection", "end-of-line"} {
		w.ok("send-keys", "-X", "-t", "%0", cmd)
	}
	w.ok("send-keys", "-X", "-t", "%0", "copy-selection-and-cancel")
	if got := w.ok("show-buffer"); got != "copy this line" {
		t.Fatalf("buffer: %q", got)
	}
	w.waitFormat("%0", "#{pane_in_mode}", "0")
}

func TestWheelScrollsInAndOutOfCopyMode(t *testing.T) {
	w := newWorld(t)
	w.ok("new-session", "-d", "-s", "a", "seq 1 100; exec cat")
	w.waitCapture("%0", "100")
	c := w.attach(80, 24)
	c.waitText("100")
	c.typ("\x1b[<64;10;10M")
	w.waitFormat("%0", "#{pane_in_mode}", "1")
	c.typ("\x1b[<64;10;10M")
	w.waitFormat("%0", "#{scroll_position}", "5")
	c.typ("\x1b[<65;10;10M\x1b[<65;10;10M")
	w.waitFormat("%0", "#{pane_in_mode}", "0")
}

func TestDoubleClickCopiesTheWord(t *testing.T) {
	w := newWorld(t)
	w.ok("new-session", "-d", "-s", "a", "printf 'alpha beta gamma\\n'; exec cat")
	w.waitCapture("%0", "gamma")
	c := w.attach(80, 24)
	c.waitText("gamma")
	c.typ("\x1b[<0;9;1M\x1b[<0;9;1m\x1b[<0;9;1M\x1b[<0;9;1m")
	// The pane is in copy mode for the binding's 0.3 s only, which a poll can miss where a
	// command takes long to start (Windows): its result is waited for instead.
	deadline := time.Now().Add(difftest.WaitTimeout)
	for w.run("show-buffer").Stdout != "beta" {
		if time.Now().After(deadline) {
			t.Fatalf("buffer: %q", w.run("show-buffer").Stdout)
		}
		time.Sleep(20 * time.Millisecond)
	}
	w.waitFormat("%0", "#{pane_in_mode}", "0")
}

// The mouse.

func TestClickSelectsAPane(t *testing.T) {
	w := newWorld(t)
	w.config("set -g mouse on")
	w.ok("new-session", "-d", "-s", "a", "exec cat")
	w.ok("split-window", "-h", "-t", "a", "exec cat")
	c := w.attach(80, 24)
	w.waitFormat("a", "#{session_attached}", "1")
	c.click(0, 10, 10)
	w.waitFormat("a", "#{pane_id}", "%0")
	clickWait()
	c.click(0, 60, 10)
	w.waitFormat("a", "#{pane_id}", "%1")
}

func TestStatusClickSwitchesWindow(t *testing.T) {
	w := newWorld(t)
	w.config("set -g status-right R")
	w.ok("new-session", "-d", "-s", "a", "-n", "w0", "exec cat")
	w.ok("new-window", "-d", "-t", "a", "-n", "w1", "exec cat")
	c := w.attach(80, 24)
	c.waitText("1:w1")
	// "[a] 0:w0* 1:w1": window 1 starts at column 11.
	c.click(0, 12, 24)
	w.waitFormat("a", "#{window_index}", "1")
}

func TestBorderDragResizes(t *testing.T) {
	w := newWorld(t)
	w.ok("new-session", "-d", "-s", "a", "exec cat")
	w.ok("split-window", "-h", "-l", "40", "-t", "a", "exec cat")
	c := w.attach(80, 24)
	w.waitFormat("a", "#{session_attached}", "1")
	// The client has asked its terminal for the mouse: ConPTY passes the mouse on only then.
	c.waitOutput("\x1b[?1006h", 10*time.Second)
	c.typ("\x1b[<0;40;5M\x1b[<32;45;5M\x1b[<32;50;5M\x1b[<0;50;5m")
	w.waitFormat("%0", "#{pane_width}", "49")
}

func TestMouseReachesAProgramThatAskedForIt(t *testing.T) {
	w := newWorld(t)
	// The fake TUI appends the hex of each chunk it reads to ev.out (its screen shows only the
	// last chunk, and the press and release may come in two).
	w.ok("new-session", "-d", "-s", "a", `faketui -mode keys -init "\x1b[?1000h\x1b[?1006h" -out ev.out`)
	w.waitCapture("%0", "READY")
	c := w.attach(80, 24)
	c.waitText("READY")
	c.click(0, 11, 6)
	// The program reads the press and the release at its own coordinates:
	// ESC [ < 0 ; 11 ; 6 M and ESC [ < 0 ; 11 ; 6 m.
	w.waitFile("ev.out", "1b5b3c303b31313b364d1b5b3c303b31313b366d")
}

// Two clients.

func TestTwoClientsSeeTheSameOutput(t *testing.T) {
	w := newWorld(t)
	w.ok("new-session", "-d", "-s", "a", "exec cat")
	c1 := w.attach(80, 24)
	c2 := w.attach(80, 24)
	w.waitFormat("a", "#{session_attached}", "2")
	w.ok("send-keys", "-t", "%0", "both see this", "Enter")
	c1.waitText("both see this")
	c2.waitText("both see this")
}

func TestWindowFollowsTheLatestClient(t *testing.T) {
	w := newWorld(t)
	w.ok("new-session", "-d", "-s", "a", "exec cat")
	c1 := w.attach(80, 24)
	w.waitFormat("a", "#{session_attached}", "1")
	c2 := w.attach(100, 30)
	w.waitFormat("a", "#{window_width}x#{window_height}", "100x29")
	c1.typ("x")
	w.waitFormat("a", "#{window_width}x#{window_height}", "80x23")
	_ = c2
}

func TestDetachOneOfTwoClients(t *testing.T) {
	w := newWorld(t)
	w.ok("new-session", "-d", "-s", "a", "exec cat")
	c1 := w.attach(80, 24)
	w.waitFormat("a", "#{session_attached}", "1")
	c2 := w.attach(80, 24)
	w.waitFormat("a", "#{session_attached}", "2")
	c1.typ("\x02d")
	c1.waitText("[detached")
	w.waitFormat("a", "#{session_attached}", "1")
	w.waitFormat("%0", "#{pane_current_command}", "cat") // keys sent before cat reads may be lost
	w.ok("send-keys", "-t", "%0", "still attached", "Enter")
	c2.waitText("still attached")
}

func TestKillSessionEndsBothClients(t *testing.T) {
	w := newWorld(t)
	w.ok("new-session", "-d", "-s", "a", "exec cat")
	w.ok("new-session", "-d", "-s", "b", "exec cat")
	c1 := w.attach(80, 24)
	w.waitFormat("b", "#{session_attached}", "1")
	// attach-session prefers a session nobody is attached to: the second client goes to a,
	// and is switched to b.
	c2 := w.attach(80, 24)
	w.waitFormat("a", "#{session_attached}", "1")
	for _, l := range strings.Split(strings.TrimSpace(w.ok("list-clients", "-F", "#{client_name} #{client_session}")), "\n") {
		if name, sess, _ := strings.Cut(l, " "); sess == "a" {
			w.ok("switch-client", "-c", name, "-t", "b")
		}
	}
	w.waitFormat("b", "#{session_attached}", "2")
	w.ok("kill-session", "-t", "b")
	c1.waitText("[exited]")
	c2.waitText("[exited]")
}

// The server's life.

func TestServerExitsWithItsLastSession(t *testing.T) {
	w := newWorld(t)
	w.ok("new-session", "-d", "-s", "a", "exec cat")
	w.ok("kill-session", "-t", "a")
	w.fails(noServer, "list-sessions")
}

func TestNewServerAfterTheLastOneExits(t *testing.T) {
	w := newWorld(t)
	w.ok("new-session", "-d", "-s", "a", "exec cat")
	w.ok("kill-server")
	w.fails(noServer, "has-session", "-t", "a")
	w.ok("new-session", "-d", "-s", "again", "exec cat")
	if got := w.ok("list-sessions", "-F", "#{session_name}"); got != "again\n" {
		t.Fatalf("sessions: %q", got)
	}
}

func TestLastPaneExitingEndsTheServer(t *testing.T) {
	w := newWorld(t)
	w.ok("new-session", "-d", "-s", "a", "exec cat")
	w.ok("send-keys", "-t", "%0", "C-d")
	deadline := time.Now().Add(difftest.WaitTimeout)
	for time.Now().Before(deadline) {
		if r := w.run("has-session"); r.Code != 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("the server did not exit when its last pane did")
}

// Keys and hooks.

func TestPrefixCMakesAWindow(t *testing.T) {
	w := newWorld(t)
	w.ok("new-session", "-d", "-s", "a", "exec cat")
	c := w.attach(80, 24)
	w.waitFormat("a", "#{session_attached}", "1")
	c.typ("\x02c")
	w.waitFormat("a", "#{session_windows}", "2")
}

func TestTypedKeysReachThePane(t *testing.T) {
	w := newWorld(t)
	w.ok("new-session", "-d", "-s", "a", "exec cat")
	c := w.attach(80, 24)
	w.waitFormat("a", "#{session_attached}", "1")
	c.typ("typed here\r")
	w.waitCapture("%0", "typed here\ntyped here")
}

func TestSendKeys(t *testing.T) {
	w := newWorld(t)
	w.ok("new-session", "-d", "-s", "a", "exec cat")
	w.ok("send-keys", "-t", "%0", "-l", "literal C-c")
	w.ok("send-keys", "-t", "%0", "Enter")
	w.waitCapture("%0", "literal C-c")
}

func TestHookRunsAfterACommand(t *testing.T) {
	w := newWorld(t)
	w.ok("new-session", "-d", "-s", "a", "exec cat")
	w.ok("set-hook", "-g", "after-new-window", "set -gF @hooked '#{window_index}'")
	w.ok("new-window", "-d", "-t", "a", "exec cat")
	w.waitFormat("", "#{@hooked}", "1")
}

func TestStatusLineShowsTheWindows(t *testing.T) {
	w := newWorld(t)
	w.config("set -g status-right R")
	w.ok("new-session", "-d", "-s", "work", "-n", "edit", "exec cat")
	w.ok("new-window", "-d", "-t", "work", "-n", "logs", "exec cat")
	c := w.attach(80, 24)
	c.waitText("[work] 0:edit* 1:logs")
}

// A terminal that never answers the device attributes query gets its features when the
// query times out, with focus events on (tty_start_timer_callback, tty_update_features): an
// Enfcs override reaches it, as i-mux's terminal background does (C6.3).
func TestFocusEventsWhenTheQueriesTimeOut(t *testing.T) {
	w := newWorld(t)
	w.config(
		"set -g focus-events on",
		`set -g terminal-overrides[90] '*:Enfcs=\E[?1004h\E]11;rgb::27/2c/36\007'`,
	)
	w.ok("new-session", "-d", "-s", "a", "exec cat")
	c := w.attach(80, 24)
	c.waitOutput("\x1b[?1004h\x1b]11;rgb:27/2c/36\x07", 10*time.Second)
}
