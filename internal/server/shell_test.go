//go:build darwin || linux

package server

import (
	"testing"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/ipc"
)

// The argv form of run-shell and if-shell (COMPATIBILITY.md X1): tmux has none, so it is
// checked here rather than against tmux.
func TestShellArgvForm(t *testing.T) {
	l, path := listen(t)
	s := startServer(t, Options{SocketPath: path, Environ: env})
	go s.Serve(l)
	run := func(words ...string) ipc.Result {
		t.Helper()
		c, r := dial(t, path, ipc.ProtocolVersion)
		defer c.Close()
		return command(t, c, r, words...)
	}
	if res := run("new-session", "-d", "-s", "s", "exec cat"); res.Code != 0 {
		t.Fatalf("new-session: %+v", res)
	}
	for _, tc := range []struct {
		words  []string
		stdout string
		code   int
	}{
		// Each element is expanded on its own: no quoting is needed.
		{[]string{"run-shell", `["echo", "#{session_name}", "a b", "$HOME"]`}, "s a b $HOME\n", 0},
		{[]string{"run-shell", ` ["sh", "-c", "echo out; exit 2"]`}, "out\n'sh -c echo out; exit 2' returned 2\n", 2},
		// quiet: no output and no exit status, as >/dev/null 2>&1 || true.
		{[]string{"run-shell", `{"argv": ["sh", "-c", "echo hidden; exit 4"], "quiet": true}`}, "", 0},
		{[]string{"if-shell", `["true"]`, "set -g @a yes", "set -g @a no"}, "", 0},
		{[]string{"show", "-gv", "@a"}, "yes\n", 0},
		{[]string{"if-shell", `["sh", "-c", "exit 1"]`, "set -g @a yes", "set -g @a no"}, "", 0},
		{[]string{"show", "-gv", "@a"}, "no\n", 0},
	} {
		res := run(tc.words...)
		if res.Code != tc.code || (tc.stdout != "" || tc.code == 0) && res.Stdout != tc.stdout {
			t.Errorf("%q: %+v, want stdout %q code %d", tc.words, res, tc.stdout, tc.code)
		}
	}
	// Not valid JSON: an ordinary shell command, which the shell fails on.
	if res := run("run-shell", `["echo", oops`); res.Code == 0 {
		t.Errorf("a broken argv ran: %+v", res)
	}
}
