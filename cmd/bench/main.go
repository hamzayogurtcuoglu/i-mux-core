// Command bench measures ARCHITECTURE.md §10's performance targets under tmux and i-mux-core on
// the same machine, one engine after the other, and prints them as BENCHMARKS.md's table
// (TASKS.md C6.4). scripts/bench.sh runs it.
//
//	bench -tmux PATH -core PATH [-imux PATH -fakes DIR] [-only NAME,…] [-quick]
//
// Every run has its own socket, HOME and TMUX_TMPDIR under a temporary directory, and an empty
// config: the owner's servers and files are never touched.
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/pty"
)

// tempBase is where the runs' directories go: /tmp, so that socket paths stay short (macOS
// allows 104 bytes).
const tempBase = "/tmp"

// engine is a tmux-compatible binary under test.
type engine struct {
	name string
	path string
}

// world is one engine's isolated run: its directory, socket and environment.
type world struct {
	e      engine
	dir    string
	socket string
	conf   string
	env    []string
}

func newWorld(e engine) (*world, error) {
	dir, err := os.MkdirTemp(tempBase, "ib-")
	if err != nil {
		return nil, err
	}
	if d, err := filepath.EvalSymlinks(dir); err == nil {
		dir = d
	}
	home, tmp := filepath.Join(dir, "home"), filepath.Join(dir, "tmp")
	for _, d := range []string{home, tmp} {
		if err := os.Mkdir(d, 0o700); err != nil {
			return nil, err
		}
	}
	w := &world{e: e, dir: dir, socket: filepath.Join(dir, "s"), conf: filepath.Join(dir, "empty.conf")}
	if err := os.WriteFile(w.conf, nil, 0o600); err != nil {
		return nil, err
	}
	w.env = []string{
		"HOME=" + home, "TMUX_TMPDIR=" + tmp, "TERM=xterm-256color", "LANG=C.UTF-8",
		"PATH=/usr/bin:/bin", "SHELL=/bin/sh",
	}
	return w, nil
}

// cmd runs a command on the engine's server and returns its output.
func (w *world) cmd(args ...string) (string, error) {
	c := exec.Command(w.e.path, append([]string{"-S", w.socket, "-f", w.conf}, args...)...)
	c.Env, c.Dir = w.env, w.dir
	out, err := c.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("%s %v: %w: %s", w.e.name, args, err, out)
	}
	return string(out), nil
}

func (w *world) must(args ...string) string {
	out, err := w.cmd(args...)
	if err != nil {
		fail(err)
	}
	return out
}

// close ends the server and removes the run's directory.
func (w *world) close() {
	_, _ = w.cmd("kill-server")
	_ = os.RemoveAll(w.dir)
}

// serverPid is the server's process id.
func (w *world) serverPid() int {
	n, err := strconv.Atoi(strings.TrimSpace(w.must("display-message", "-p", "#{pid}")))
	if err != nil {
		fail(err)
	}
	return n
}

// waitFor polls cond every few milliseconds until it holds, or fails after limit.
func waitFor(what string, limit time.Duration, cond func() bool) {
	deadline := time.Now().Add(limit)
	for !cond() {
		if time.Now().After(deadline) {
			fail(fmt.Errorf("timed out waiting for %s", what))
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// client is an attached client in a PTY the bench owns: what it writes is kept, with when.
type client struct {
	p    pty.Proc
	mu   sync.Mutex
	out  []byte
	cond *sync.Cond
}

func (w *world) attach(cols, rows int, target string) *client {
	argv := []string{"tmux", "-S", w.socket, "-f", w.conf, "-u", "attach-session", "-t", target}
	p, err := pty.New().Start(pty.Spec{Path: w.e.path, Argv: argv, Env: w.env, Dir: w.dir, Cols: cols, Rows: rows})
	if err != nil {
		fail(err)
	}
	c := &client{p: p}
	c.cond = sync.NewCond(&c.mu)
	go func() {
		buf := make([]byte, 64<<10)
		for {
			n, err := p.Read(buf)
			c.mu.Lock()
			c.out = append(c.out, buf[:n]...)
			c.cond.Broadcast()
			c.mu.Unlock()
			if err != nil {
				return
			}
		}
	}()
	waitFor("the client to attach", 10*time.Second, func() bool {
		out, _ := w.cmd("list-clients", "-F", "#{client_name}")
		return strings.TrimSpace(out) != ""
	})
	return c
}

func (c *client) mark() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.out)
}

// waitByte waits until the client writes b after offset, and returns when it saw it.
func (c *client) waitByte(offset int, b byte, limit time.Duration) (time.Time, bool) {
	deadline := time.Now().Add(limit)
	timer := time.AfterFunc(limit, func() {
		c.mu.Lock()
		c.cond.Broadcast()
		c.mu.Unlock()
	})
	defer timer.Stop()
	c.mu.Lock()
	defer c.mu.Unlock()
	for {
		if bytes.IndexByte(c.out[min(offset, len(c.out)):], b) >= 0 {
			return time.Now(), true
		}
		if time.Now().After(deadline) {
			return time.Time{}, false
		}
		c.cond.Wait()
	}
}

func (c *client) close() {
	_ = c.p.Kill()
	_ = c.p.Close()
}

// result is one measurement under one engine.
type result struct {
	value float64
	unit  string
}

func (r result) String() string {
	switch r.unit {
	case "%":
		return fmt.Sprintf("%.2f%%", r.value)
	case "ms":
		return fmt.Sprintf("%.2f ms", r.value)
	case "s":
		return fmt.Sprintf("%.2f s", r.value)
	case "MiB":
		return fmt.Sprintf("%.1f MiB", r.value)
	}
	return fmt.Sprintf("%g %s", r.value, r.unit)
}

// measure is one of §10's targets.
type measure struct {
	name   string
	what   string
	target string
	run    func(e engine) result
	// meets says whether core's result meets the target against tmux's.
	meets func(tmux, core result) bool
}

var quick bool

func percentile(xs []float64, p float64) float64 {
	sort.Float64s(xs)
	i := int(p * float64(len(xs)-1))
	return xs[i]
}

// idleCPU: 10 sessions × 4 panes of cat and one attached client, nothing happening, for 10 s.
func idleCPU(e engine) result {
	w, err := newWorld(e)
	if err != nil {
		fail(err)
	}
	defer w.close()
	for i := range 10 {
		name := "s" + strconv.Itoa(i)
		w.must("new-session", "-d", "-s", name, "-x", "80", "-y", "24", "exec cat")
		// Four panes, two by two (select-layout's named layouts are not in the engine yet).
		w.must("split-window", "-h", "-t", name+":0.0", "exec cat")
		w.must("split-window", "-v", "-t", name+":0.0", "exec cat")
		w.must("split-window", "-v", "-t", name+":0.2", "exec cat")
	}
	c := w.attach(80, 24, "s0")
	defer c.close()
	pid := w.serverPid()
	time.Sleep(2 * time.Second) // the attach's drawing settles
	span := 10 * time.Second
	if quick {
		span = 3 * time.Second
	}
	before := cpuTime(pid)
	start := time.Now()
	time.Sleep(span)
	used := cpuTime(pid) - before
	return result{100 * used.Seconds() / time.Since(start).Seconds(), "%"}
}

// latency: keys typed into an attached client, echoed by cat in raw mode, 50 ms apart; p99.
func latency(e engine) result {
	w, err := newWorld(e)
	if err != nil {
		fail(err)
	}
	defer w.close()
	w.must("new-session", "-d", "-s", "a", "-x", "80", "-y", "24", "stty raw -echo; echo READY; exec cat")
	w.must("set", "-g", "status", "off")
	waitFor("the pane's program", 10*time.Second, func() bool {
		out, _ := w.cmd("capture-pane", "-p", "-t", "a")
		return strings.Contains(out, "READY")
	})
	c := w.attach(80, 24, "a")
	defer c.close()
	time.Sleep(time.Second)
	n := 300
	if quick {
		n = 60
	}
	var ms []float64
	for i := range n {
		ch := byte('a' + i%26)
		off := c.mark()
		t0 := time.Now()
		if _, err := c.p.Write([]byte{ch}); err != nil {
			fail(err)
		}
		t1, ok := c.waitByte(off, ch, 5*time.Second)
		if !ok {
			fail(fmt.Errorf("%s: no echo of %q", e.name, ch))
		}
		ms = append(ms, float64(t1.Sub(t0).Microseconds())/1000)
		if (i+1)%80 == 0 {
			// A new line keeps the echo on the screen, not scrolling sideways.
			_, _ = c.p.Write([]byte{'\r'})
			time.Sleep(50 * time.Millisecond)
		}
		time.Sleep(50 * time.Millisecond)
	}
	return result{percentile(ms, 0.99), "ms"}
}

// writeText writes size bytes of 80-column lines, a third of them in colour, to path.
func writeText(path string, size int) {
	f, err := os.Create(path)
	if err != nil {
		fail(err)
	}
	defer f.Close()
	r := rand.New(rand.NewSource(1))
	const letters = "abcdefghijklmnopqrstuvwxyz ABCDEFGHIJKLMNOPQRSTUVWXYZ 0123456789"
	var b bytes.Buffer
	written := 0
	for i := 0; written < size; i++ {
		b.Reset()
		if i%3 == 0 {
			fmt.Fprintf(&b, "\033[3%dm", 1+i%7)
		}
		for range 79 {
			b.WriteByte(letters[r.Intn(len(letters))])
		}
		if i%3 == 0 {
			b.WriteString("\033[0m")
		}
		b.WriteByte('\n')
		n, err := f.Write(b.Bytes())
		if err != nil {
			fail(err)
		}
		written += n
	}
}

// throughput: cat of a 100 MB file into a 200×50 pane an attached client shows, until cat
// ends.
func throughput(e engine) result {
	w, err := newWorld(e)
	if err != nil {
		fail(err)
	}
	defer w.close()
	size := 100 << 20
	if quick {
		size = 10 << 20
	}
	file := filepath.Join(w.dir, "text")
	writeText(file, size)
	w.must("new-session", "-d", "-s", "a", "-x", "200", "-y", "50", "exec sleep 1000")
	w.must("set", "-g", "remain-on-exit", "on")
	c := w.attach(200, 50, "a")
	defer c.close()
	time.Sleep(time.Second)
	start := time.Now()
	w.must("respawn-pane", "-k", "-t", "a", "cat "+file)
	waitFor("cat to end", 10*time.Minute, func() bool {
		out, _ := w.cmd("display-message", "-p", "-t", "a", "#{pane_dead}")
		return strings.TrimSpace(out) == "1"
	})
	return result{time.Since(start).Seconds(), "s"}
}

// roundTrip: display-message -p '#{pane_id}' from a new client process, 200 times; p50.
func roundTrip(e engine) result {
	w, err := newWorld(e)
	if err != nil {
		fail(err)
	}
	defer w.close()
	w.must("new-session", "-d", "-s", "a", "exec cat")
	var ms []float64
	n := 200
	if quick {
		n = 50
	}
	for range n {
		t0 := time.Now()
		w.must("display-message", "-p", "#{pane_id}")
		ms = append(ms, float64(time.Since(t0).Microseconds())/1000)
	}
	return result{percentile(ms, 0.5), "ms"}
}

// memoryPerPane: the server's resident memory growth for each of 4 panes filled past
// history-limit 50000 with that text.
func memoryPerPane(e engine) result {
	w, err := newWorld(e)
	if err != nil {
		fail(err)
	}
	defer w.close()
	file := filepath.Join(w.dir, "text")
	writeText(file, 60000*81)
	w.must("new-session", "-d", "-s", "a", "-x", "80", "-y", "24", "exec sleep 1000")
	w.must("set", "-g", "history-limit", "50000")
	w.must("set", "-g", "remain-on-exit", "on")
	pid := w.serverPid()
	time.Sleep(time.Second)
	before := rss(pid)
	for range 4 {
		w.must("new-window", "-d", "-t", "a", "cat "+file)
	}
	waitFor("the panes to fill", 5*time.Minute, func() bool {
		out, _ := w.cmd("list-panes", "-s", "-t", "a", "-F", "#{pane_dead}")
		return strings.Count(out, "1") == 4
	})
	time.Sleep(2 * time.Second)
	return result{float64(rss(pid)-before) / 4 / (1 << 20), "MiB"}
}

// startup is set by -imux and -fakes: i-mux's startup to its first frame is measured.
var imuxBin, fakesDir string

// imuxStartup: imux claude in a project in a 160×40 terminal, with i-mux's fake agents, until
// the sidebar shows; the median of 5.
func imuxStartup(e engine) result {
	var secs []float64
	n := 5
	if quick {
		n = 2
	}
	for range n {
		secs = append(secs, imuxOnce(e))
	}
	return result{percentile(secs, 0.5), "s"}
}

func imuxOnce(e engine) float64 {
	dir, err := os.MkdirTemp(tempBase, "ib-")
	if err != nil {
		fail(err)
	}
	if d, err := filepath.EvalSymlinks(dir); err == nil {
		dir = d
	}
	defer os.RemoveAll(dir)
	home := filepath.Join(dir, "home")
	project := filepath.Join(home, "proj")
	bin := filepath.Join(dir, "bin")
	imuxHome := filepath.Join(home, ".imux")
	log := filepath.Join(dir, "fakelog")
	for _, d := range []string{project, bin, imuxHome, log, filepath.Join(home, ".claude", "projects"), filepath.Join(home, ".codex", "sessions"), filepath.Join(home, ".copilot", "session-state")} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			fail(err)
		}
	}
	if err := os.Symlink(e.path, filepath.Join(bin, "tmux")); err != nil {
		fail(err)
	}
	var cfg strings.Builder
	cfg.WriteString("update_check = false\n\n")
	for _, a := range []string{"claude", "antigravity", "copilot", "codex", "opencode"} {
		fmt.Fprintf(&cfg, "[agents.%s]\nbin = %q\n\n", a, filepath.Join(fakesDir, "fake-"+a))
	}
	cfg.WriteString("[tmux]\nsidebar = true\nagent_bar = false\n\n[notify]\nenabled = false\n\n[paste]\nimages = false\n\n[hooks]\nenabled = false\n")
	if err := os.WriteFile(filepath.Join(imuxHome, "config.toml"), []byte(cfg.String()), 0o600); err != nil {
		fail(err)
	}
	socket := "imux-bench-" + strconv.Itoa(os.Getpid())
	env := []string{
		"HOME=" + home, "IMUX_HOME=" + imuxHome, "CLAUDE_CONFIG_DIR=" + filepath.Join(home, ".claude"),
		"CODEX_HOME=" + filepath.Join(home, ".codex"), "COPILOT_HOME=" + filepath.Join(home, ".copilot"),
		"GEMINI_CLI_HOME=", "IMUX_LANG=en", "XDG_CONFIG_HOME=" + filepath.Join(home, ".config"),
		"IMUX_TMUX_SOCKET=" + socket, "IMUX_FAKE_LOG=" + log, "TERM=xterm-256color", "SHELL=/bin/sh",
		"PATH=" + bin + ":/usr/bin:/bin", "TMUX_TMPDIR=" + filepath.Join(dir, "tmp"), "LANG=C.UTF-8",
	}
	if err := os.Mkdir(filepath.Join(dir, "tmp"), 0o700); err != nil {
		fail(err)
	}
	defer func() {
		stop := exec.Command(imuxBin, "__watch", "stop")
		stop.Env = env
		_ = stop.Run()
		kill := exec.Command(e.path, "-L", socket, "kill-server")
		kill.Env = env
		_ = kill.Run()
	}()
	start := time.Now()
	p, err := pty.New().Start(pty.Spec{Path: imuxBin, Argv: []string{"imux", "claude"}, Env: env, Dir: project, Cols: 160, Rows: 40})
	if err != nil {
		fail(err)
	}
	defer func() { _ = p.Kill(); _ = p.Close() }()
	var mu sync.Mutex
	var out []byte
	go func() {
		buf := make([]byte, 64<<10)
		for {
			n, err := p.Read(buf)
			mu.Lock()
			out = append(out, buf[:n]...)
			mu.Unlock()
			if err != nil {
				return
			}
		}
	}()
	deadline := time.Now().Add(time.Minute)
	for {
		mu.Lock()
		shown := bytes.Contains(out, []byte("AGENTS"))
		tail := out[max(len(out)-2000, 0):]
		mu.Unlock()
		if shown {
			return time.Since(start).Seconds()
		}
		if time.Now().After(deadline) {
			fail(fmt.Errorf("%s: the sidebar did not show; imux wrote last: %q", e.name, tail))
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func main() {
	tmuxPath := flag.String("tmux", "", "the pinned tmux")
	corePath := flag.String("core", "", "imux-core")
	flag.StringVar(&imuxBin, "imux", "", "i-mux's binary, for its startup (optional)")
	flag.StringVar(&fakesDir, "fakes", "", "i-mux's e2e/fakeagents, with -imux")
	only := flag.String("only", "", "the measures to run, by name, separated by commas")
	flag.BoolVar(&quick, "quick", false, "shorter runs, for a check rather than a record")
	flag.Parse()
	if *tmuxPath == "" || *corePath == "" {
		fail(errors.New("usage: bench -tmux PATH -core PATH [-imux PATH -fakes DIR] [-only NAME,…] [-quick]"))
	}
	tmux, core := engine{"tmux", *tmuxPath}, engine{"imux-core", *corePath}

	within := func(add float64) func(t, c result) bool {
		return func(t, c result) bool { return c.value <= t.value+add }
	}
	times := func(f float64) func(t, c result) bool {
		return func(t, c result) bool { return c.value <= t.value*f }
	}
	measures := []measure{
		{
			"idle-cpu", "Idle CPU (10 sessions × 4 panes, one client, 10 s)", "≤ 0.5% of a core", idleCPU,
			func(_, c result) bool { return c.value <= 0.5 },
		},
		{"latency", "Key to screen latency (p99 of 300 keys)", "≤ tmux + 2 ms", latency, within(2)},
		{"throughput", "Throughput (100 MB into a 200×50 pane)", "≤ 1.5× tmux", throughput, times(1.5)},
		{"round-trip", "Command round trip (p50 of 200)", "≤ tmux + 1 ms", roundTrip, within(1)},
		{"memory", "Memory per pane (history-limit 50000, filled)", "≤ 2× tmux", memoryPerPane, times(2)},
	}
	if imuxBin != "" {
		measures = append(measures, measure{
			"startup", "i-mux startup to the sidebar (median of 5)",
			"≤ tmux + 10%", imuxStartup, times(1.1),
		})
	}
	want := map[string]bool{}
	for _, n := range strings.Split(*only, ",") {
		if n != "" {
			want[n] = true
		}
	}

	fmt.Println("| What | tmux 3.8 | i-mux-core | Target | Met |")
	fmt.Println("| --- | --- | --- | --- | --- |")
	failed := false
	for _, m := range measures {
		if len(want) != 0 && !want[m.name] {
			continue
		}
		fmt.Fprintf(os.Stderr, "bench: %s…\n", m.name)
		t := m.run(tmux)
		c := m.run(core)
		met := "yes"
		if !m.meets(t, c) {
			met, failed = "**no**", true
		}
		fmt.Printf("| %s | %s | %s | %s | %s |\n", m.what, t, c, m.target, met)
	}
	if failed {
		os.Exit(1)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "bench:", err)
	os.Exit(2)
}
