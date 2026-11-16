//go:build darwin || linux

package server

import (
	"strings"
	"testing"
	"time"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/ipc"
)

// Key bindings (TASKS.md C3.8), driven through an attached client: a root binding, the
// prefix, switch-client -T one-key tables, repeat bindings and the prefix timeout, with
// the fake TUI echoing on its second line what reaches the pane.

// keyWorld is an input world plus a way to run commands: each on its own connection, as
// the command client runs one command list per invocation.
type keyWorld struct {
	*inputWorld
}

// newKeyWorld returns a world with the fake TUI echoing keys.
func newKeyWorld(t *testing.T) *keyWorld {
	return &keyWorld{inputWorld: newInputWorld(t, "keys")}
}

// run runs one command and fails the test when it does not.
func (w *keyWorld) run(t *testing.T, words ...string) {
	t.Helper()
	c, r := dial(t, w.s.opts.SocketPath, ipc.ProtocolVersion)
	defer c.Close()
	if res := command(t, c, r, words...); res.Code != 0 {
		t.Fatalf("%v: %+v", words, res)
	}
}

// ask runs one command and returns its stdout.
func (w *keyWorld) ask(t *testing.T, words ...string) string {
	t.Helper()
	c, r := dial(t, w.s.opts.SocketPath, ipc.ProtocolVersion)
	defer c.Close()
	return command(t, c, r, words...).Stdout
}

// option returns an option's value, as a string.
func (w *keyWorld) option(t *testing.T, name string) string {
	return strings.TrimSpace(w.ask(t, "show", "-gqv", name))
}

// waitOption waits for an option to have a value.
func (w *keyWorld) waitOption(t *testing.T, name, want string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for w.option(t, name) != want {
		if time.Now().After(deadline) {
			t.Fatalf("@%s is %q, want %q", name, w.option(t, name), want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// prefixFlag is #{client_prefix} for the attached client.
func (w *keyWorld) prefixFlag(t *testing.T) string {
	return strings.TrimSpace(w.ask(t, "display-message", "-p", "#{client_prefix}"))
}

// waitPrefix waits for the client to be in (or out of) the prefix table.
func (w *keyWorld) waitPrefix(t *testing.T, want string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for w.prefixFlag(t) != want {
		if time.Now().After(deadline) {
			t.Fatalf("client_prefix is %q, want %q", w.prefixFlag(t), want)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestRootBinding runs a binding bound in the root table: its command runs and the pane
// does not see the key; an unbound key goes to the pane, and unbinding makes a bound key
// reach the pane again.
func TestRootBinding(t *testing.T) {
	w := newKeyWorld(t)
	w.run(t, "bind", "-n", "z", "set", "-g", "@hit", "z")
	w.typed("z")
	w.waitOption(t, "@hit", "z")
	if strings.Contains(w.screen(), "key: 7a") {
		t.Errorf("the bound key reached the pane:\n%s", w.screen())
	}
	w.typed("y")
	w.waitPane("key: 79")
	w.run(t, "unbind", "-n", "z")
	w.typed("z")
	w.waitPane("key: 7a")
}

// TestPrefix checks the prefix key: it switches the client to the prefix table, where
// tmux's default C-b binding runs send-prefix (so the pane gets the prefix), a prefix
// binding runs, and a key bound nowhere is swallowed after the prefix, not sent.
func TestPrefix(t *testing.T) {
	w := newKeyWorld(t)
	// The default prefix C-b switches to the prefix table.
	w.waitPrefix(t, "0")
	w.typed("\x02")
	w.waitPrefix(t, "1")
	// C-b again: send-prefix, the default prefix binding, sends it to the pane.
	w.typed("\x02")
	w.waitPane("key: 02")
	w.waitPrefix(t, "0")
	// A prefix binding runs; the pane does not see the key, and the client is back in
	// its default table afterwards, where the key is unbound and reaches the pane.
	w.run(t, "bind", "x", "set", "-g", "@hit", "x")
	w.typed("\x02")
	w.waitPrefix(t, "1")
	w.typed("x")
	w.waitOption(t, "@hit", "x")
	w.waitPrefix(t, "0")
	w.typed("x")
	w.waitPane("key: 78")
	// A key bound nowhere is swallowed after the prefix (the client falls back to its
	// default table), and then such a key reaches the pane.
	w.typed("\x02")
	w.waitPrefix(t, "1")
	w.typed("y")
	w.waitPrefix(t, "0")
	if strings.Contains(w.screen(), "key: 79") {
		t.Errorf("a key bound nowhere reached the pane:\n%s", w.screen())
	}
	w.typed("a")
	w.waitPane("key: 61")
}

// TestSwitchClientTable checks switch-client -T: the client is in the table for one key,
// a bound key runs and the binding can enter the table again, a key unbound there is
// looked up in the default table, and one unbound there too is swallowed.
func TestSwitchClientTable(t *testing.T) {
	w := newKeyWorld(t)
	w.run(t, "bind", "-T", "imux-resize", "l", "set -gF @n '#{e|+:#{@n},1}' ; switch-client -T imux-resize")
	w.run(t, "bind", "-n", "q", "set", "-g", "@hit", "q")
	w.run(t, "switch-client", "-T", "imux-resize")
	// A binding in the table runs, and the table is entered again: a second press runs it
	// once more, and the pane never sees the key.
	w.typed("l")
	w.waitOption(t, "@n", "1")
	w.typed("l")
	w.waitOption(t, "@n", "2")
	if strings.Contains(w.screen(), "key: 6c") {
		t.Errorf("the table's key reached the pane:\n%s", w.screen())
	}
	// A key unbound in the table is looked up in root: q's binding runs, and the client
	// is back in its default table, so a key unbound there reaches the pane.
	w.typed("q")
	w.waitOption(t, "@hit", "q")
	w.typed("y")
	w.waitPane("key: 79")
	// A key unbound in the table and in root is swallowed.
	w.run(t, "switch-client", "-T", "imux-resize")
	w.typed("j")
	w.typed("b")
	w.waitPane("key: 62")
	if strings.Contains(w.screen(), "key: 6a") {
		t.Errorf("a key unbound in the table and in root reached the pane:\n%s", w.screen())
	}
}

// TestRepeatBinding checks a repeat (-r) binding in the prefix table: presses within
// repeat-time run the binding again without the prefix, and after it the client is back
// in its default table, where the key is unbound and reaches the pane.
func TestRepeatBinding(t *testing.T) {
	w := newKeyWorld(t)
	w.run(t, "bind", "-r", "Up", "set -gF @n '#{e|+:#{@n},1}'")
	// The prefix puts the client in the prefix table; a repeat binding keeps it there.
	w.typed("\x02")
	w.waitPrefix(t, "1")
	w.typed("\x1b[A")
	w.waitOption(t, "@n", "1")
	w.typed("\x1b[A")
	w.waitOption(t, "@n", "2")
	if strings.Contains(w.screen(), "key: 1b5b41") {
		t.Errorf("the repeating key reached the pane:\n%s", w.screen())
	}
	// Once repeat-time (tmux's default 500 ms) has passed, the client is back in root:
	// the key is unbound there and reaches the pane.
	w.waitPrefix(t, "0")
	w.typed("\x1b[A")
	w.waitPane("key: 1b5b41")
}

// TestPrefixTimeout checks prefix-timeout: a client in the prefix table too long before
// its key goes back to its default table, where a key bound nowhere reaches the pane.
func TestPrefixTimeout(t *testing.T) {
	w := newKeyWorld(t)
	w.run(t, "set", "-s", "prefix-timeout", "50")
	w.typed("\x02")
	w.waitPrefix(t, "1")
	// The timeout is only looked at when the next key comes: nothing to wait for but time.
	time.Sleep(100 * time.Millisecond)
	// C-a is bound nowhere (tmux binds no C-a in prefix or root).
	w.typed("\x01")
	w.waitPane("key: 01")
	w.waitPrefix(t, "0")
}

// TestBindingClient checks that a binding's commands run for the client that pressed the
// key (tmux's cmdq_get_client): its formats see that client.
func TestBindingClient(t *testing.T) {
	w := newKeyWorld(t)
	w.run(t, "bind", "-n", "z", "set -gF @who '#{client_name}:#{client_key_table}'")
	w.typed("z")
	// Under -race the key event may take a moment to be processed; poll for the client name.
	deadline := time.Now().Add(10 * time.Second)
	name := ""
	for {
		name = strings.TrimSpace(w.ask(t, "display-message", "-p", "#{client_name}"))
		if name != "" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("client_name stayed empty")
		}
		time.Sleep(10 * time.Millisecond)
	}
	w.waitOption(t, "@who", name+":root")
}
