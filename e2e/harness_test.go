//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/difftest"
)

// coreBin is the imux-core under test (IMUX_CORE_BIN; make e2e builds it).
var coreBin string

// TestMain finds the binary, builds the fake TUI onto the engine's PATH and points attached
// clients at testdata's terminfo, as the difftests do.
func TestMain(m *testing.M) {
	coreBin = os.Getenv("IMUX_CORE_BIN")
	if coreBin == "" {
		fmt.Fprintln(os.Stderr, "e2e: IMUX_CORE_BIN is not set (make e2e sets it)")
		os.Exit(1)
	}
	if dir, err := filepath.Abs("../testdata/term/terminfo"); err == nil {
		difftest.ClientTerminfo = dir
	}
	tools, err := os.MkdirTemp("", "imux-e2e-tools-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	name := "faketui"
	if out, err := exec.Command("go", "build", "-o", filepath.Join(tools, name), "../testdata/faketui").CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "e2e: faketui: %v: %s\n", err, out)
		os.Exit(1)
	}
	difftest.ToolsDir = tools
	code := m.Run()
	_ = os.RemoveAll(tools)
	os.Exit(code)
}

// world is one scenario's server: its own directory, home, socket and clients.
type world struct {
	t       *testing.T
	eng     *difftest.Binary
	env     *difftest.Env
	clients []*client
}

func newWorld(t *testing.T) *world {
	t.Helper()
	t.Parallel()
	eng, err := difftest.NewBinary("imux-core", coreBin)
	if err != nil {
		t.Fatal(err)
	}
	env, err := difftest.NewEnv()
	if err != nil {
		t.Fatal(err)
	}
	w := &world{t: t, eng: eng, env: env}
	t.Cleanup(func() {
		for _, c := range w.clients {
			c.Close()
		}
		eng.Stop(env)
		_ = os.RemoveAll(env.Dir)
	})
	return w
}

// config makes the scenario's servers load a config with these lines.
func (w *world) config(lines ...string) {
	w.t.Helper()
	path := filepath.Join(w.env.Dir, "e2e.conf")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		w.t.Fatal(err)
	}
	w.env.Config = path
}

// run runs a command-line client with the words of args.
func (w *world) run(args ...string) difftest.Result {
	ctx, cancel := context.WithTimeout(context.Background(), difftest.WaitTimeout)
	defer cancel()
	return w.eng.Run(ctx, w.env, args)
}

// start runs args in the background (a command that waits, such as display-popup -E); the
// result comes on the channel.
func (w *world) start(args ...string) <-chan difftest.Result {
	done := make(chan difftest.Result, 1)
	go func() { done <- w.run(args...) }()
	return done
}

// ok runs args and fails the scenario unless they work; it returns their output.
func (w *world) ok(args ...string) string {
	w.t.Helper()
	r := w.run(args...)
	if r.Code != 0 || r.Stderr != "" {
		w.t.Fatalf("%q: exit %d, stderr %q", args, r.Code, r.Stderr)
	}
	return r.Stdout
}

// fails runs args and checks they fail with want on stderr.
func (w *world) fails(want string, args ...string) {
	w.t.Helper()
	r := w.run(args...)
	if r.Code == 0 || !strings.Contains(r.Stderr, want) {
		w.t.Fatalf("%q: exit %d, stderr %q, want a failure with %q", args, r.Code, r.Stderr, want)
	}
}

// format is display-message -p for target ("" for the default).
func (w *world) format(target, f string) string {
	w.t.Helper()
	args := []string{"display-message", "-p"}
	if target != "" {
		args = append(args, "-t", target)
	}
	return strings.TrimSuffix(w.ok(append(args, f)...), "\n")
}

// waitFormat waits until format f for target is want.
func (w *world) waitFormat(target, f, want string) {
	w.t.Helper()
	deadline := time.Now().Add(difftest.WaitTimeout)
	last := ""
	for time.Now().Before(deadline) {
		args := []string{"display-message", "-p"}
		if target != "" {
			args = append(args, "-t", target)
		}
		r := w.run(append(args, f)...)
		last = strings.TrimSuffix(r.Stdout, "\n")
		if r.Code == 0 && last == want {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	w.t.Fatalf("%s on %q: timed out waiting for %q, last %q", f, target, want, last)
}

// capture is capture-pane -p of target, trailing blank lines dropped.
func (w *world) capture(target string) string {
	w.t.Helper()
	return strings.TrimRight(w.ok("capture-pane", "-p", "-t", target), "\n")
}

// waitCapture waits until target's pane shows sub.
func (w *world) waitCapture(target, sub string) {
	w.t.Helper()
	deadline := time.Now().Add(difftest.WaitTimeout)
	for time.Now().Before(deadline) {
		r := w.run("capture-pane", "-p", "-t", target)
		if r.Code == 0 && strings.Contains(r.Stdout, sub) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	w.t.Fatalf("pane %s: timed out waiting for %q; it shows:\n%s", target, sub, w.capture(target))
}

// waitFile waits until the scenario directory's file name, its lines joined, holds sub.
func (w *world) waitFile(name, sub string) {
	w.t.Helper()
	deadline := time.Now().Add(difftest.WaitTimeout)
	var last string
	for time.Now().Before(deadline) {
		b, _ := os.ReadFile(filepath.Join(w.env.Dir, name))
		last = strings.ReplaceAll(string(b), "\n", "")
		if strings.Contains(last, sub) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	w.t.Fatalf("%s: timed out waiting for %q; it holds %q", name, sub, last)
}

// client is an attached client in a PTY whose terminal the scenario reads.
type client struct {
	difftest.Client
	w *world
}

// attach attaches a client of cols×rows to the most recent session (attach-session).
func (w *world) attach(cols, rows int) *client {
	w.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), difftest.WaitTimeout)
	defer cancel()
	c, err := w.eng.Attach(ctx, w.env, "", cols, rows, 0, 0)
	if err != nil {
		w.t.Fatal(err)
	}
	cl := &client{Client: c, w: w}
	w.clients = append(w.clients, cl)
	return cl
}

// lines is what the client's terminal shows, as text.
func (c *client) lines() []string {
	ctx, cancel := context.WithTimeout(context.Background(), difftest.WaitTimeout)
	defer cancel()
	dump := c.Screen(ctx).Stdout
	_, screen, _ := strings.Cut(dump, "--- screen\n")
	var out []string
	for _, l := range strings.Split(strings.TrimSuffix(screen, "\n"), "\n") {
		out = append(out, plain(l))
	}
	return out
}

// plain is a Dump line without its style marks ({...}).
func plain(l string) string {
	var b strings.Builder
	depth := 0
	for _, r := range l {
		switch {
		case r == '{':
			depth++
		case r == '}' && depth > 0:
			depth--
		case depth == 0:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// text is the client's terminal as one string.
func (c *client) text() string { return strings.Join(c.lines(), "\n") }

// waitText waits until the client's terminal shows sub (or, with absent, no longer does).
func (c *client) waitText(sub string, absent ...bool) {
	c.w.t.Helper()
	gone := len(absent) > 0 && absent[0]
	deadline := time.Now().Add(difftest.WaitTimeout)
	var last string
	for time.Now().Before(deadline) {
		last = c.text()
		if strings.Contains(last, sub) != gone {
			return
		}
	}
	c.w.t.Fatalf("client: timed out waiting for %q (absent %v); it shows:\n%s", sub, gone, last)
}

// typ types s into the client's terminal (Go escapes such as \x02 for C-b work).
func (c *client) typ(s string) {
	c.w.t.Helper()
	if err := c.Type([]byte(s)); err != nil {
		c.w.t.Fatal(err)
	}
}

// click presses and releases mouse button b (0 left, 2 right) at x, y (from 1), in SGR.
func (c *client) click(b, x, y int) {
	c.typ(fmt.Sprintf("\x1b[<%d;%d;%dM\x1b[<%d;%d;%dm", b, x, y, b, x, y))
}

// clickWait waits out the click timeout, so the next press starts a new click sequence.
func clickWait() { time.Sleep(400 * time.Millisecond) }

// output is everything the client's terminal was sent, escape sequences and all.
func (c *client) output() string {
	return string(c.Client.(interface{ Output() []byte }).Output())
}

// waitOutput waits until what the client's terminal was sent holds sub.
func (c *client) waitOutput(sub string, timeout time.Duration) {
	c.w.t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if strings.Contains(c.output(), sub) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	c.w.t.Fatalf("client: timed out waiting for %q in its output", sub)
}
