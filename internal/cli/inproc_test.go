//go:build darwin || linux || windows

package cli_test

import (
	"bytes"
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/cli"
)

// Commands inside the caller's process (E18, TASKS.md C11.1): the same output, errors and exit
// codes as the binary's client, without starting a process, a server or a terminal.

// inproc runs args with cli.Command on the world's socket, as w.run does with the binary.
func inproc(ctx context.Context, w *world, args ...string) (result, bool) {
	var out, errb bytes.Buffer
	full := append([]string{"-S", w.socket, "-f", os.DevNull}, args...)
	code, handled := cli.Command(ctx, full, &out, &errb)
	return result{out: out.String(), err: errb.String(), code: code}, handled
}

func mustInproc(t *testing.T, w *world, args ...string) result {
	t.Helper()
	r, handled := inproc(context.Background(), w, args...)
	if !handled {
		t.Fatalf("%q: not handled", args)
	}
	return r
}

func TestCommandNoServer(t *testing.T) {
	w := newWorld(t)
	for _, args := range [][]string{{"has-session"}, {"list-sessions"}, {"kill-server"}} {
		got := mustInproc(t, w, args...)
		want := w.run(args...)
		if got != want {
			t.Errorf("%q: in process %+v, binary %+v", args, got, want)
		}
	}
	if w.listening() {
		t.Error("a server was started")
	}
}

func TestCommandMatchesBinary(t *testing.T) {
	w := newWorld(t)
	if r := w.run("new-session", "-d", "-s", "a"); r.code != 0 {
		t.Fatalf("new-session: %+v", r)
	}
	for _, args := range [][]string{
		{"list-sessions", "-F", "#{session_name} #{session_windows}"},
		{"display-message", "-p", "-t", "a", "#{session_name}:#{window_index}"},
		{"set-option", "-g", "@imux_test", "x y"},
		{"show-options", "-gv", "@imux_test"},
		{"has-session", "-t", "nosuch"},
		{"nosuch-command"},
		{"list-panes", "-t", "a", "-F", "#{pane_index}", ";", "display-message", "-p", "two"},
	} {
		got := mustInproc(t, w, args...)
		want := w.run(args...)
		if got != want {
			t.Errorf("%q: in process %+v, binary %+v", args, got, want)
		}
	}
}

func TestCommandLeavesStartsToBinary(t *testing.T) {
	w := newWorld(t)
	for _, args := range [][]string{{"new-session", "-d"}, {"start-server"}, {}} {
		if r, handled := inproc(context.Background(), w, args...); handled {
			t.Errorf("%q: handled: %+v", args, r)
		}
	}
	var out, errb bytes.Buffer
	if _, handled := cli.Command(context.Background(), []string{"__server", "-S", w.socket}, &out, &errb); handled {
		t.Error("__server: handled")
	}
	if w.listening() {
		t.Error("a server was started")
	}
	// The global flags still answer at once.
	if r := mustInproc(t, w, "-V"); r.code != 0 || r.out == "" {
		t.Errorf("-V: %+v", r)
	}
}

func TestCommandContextEnds(t *testing.T) {
	w := newWorld(t)
	if r := w.run("new-session", "-d", "-s", "a"); r.code != 0 {
		t.Fatalf("new-session: %+v", r)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	r, handled := inproc(ctx, w, "wait-for", "never")
	if !handled || r.code != 1 || r.err == "" {
		t.Errorf("wait-for past its context: handled %v, %+v", handled, r)
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("wait-for took %v after its context ended", d)
	}
	// The server still answers.
	if r := mustInproc(t, w, "has-session", "-t", "a"); r.code != 0 {
		t.Errorf("has-session after: %+v", r)
	}
}

func TestCommandConcurrent(t *testing.T) {
	w := newWorld(t)
	if r := w.run("new-session", "-d", "-s", "a"); r.code != 0 {
		t.Fatalf("new-session: %+v", r)
	}
	var wg sync.WaitGroup
	errs := make(chan string, 32)
	for range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, handled := inproc(context.Background(), w, "display-message", "-p", "-t", "a", "#{session_name}")
			if !handled || r.code != 0 || r.out != "a\n" {
				errs <- r.err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Errorf("a concurrent command failed: %q", e)
	}
}
