//go:build soak && (darwin || linux)

package server

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"runtime"
	"runtime/pprof"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/ipc"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/model"
)

// The soak test (TASKS.md C6.5), run by the nightly job with the race detector:
//
//	IMUX_SOAK_DURATION=1h go test -race -tags soak -run TestSoak -timeout 90m ./internal/server
//
// 4 sessions × 4 panes of the fake TUI, half streaming in bursts and half redrawing its frame,
// 2 clients attaching and detaching, and a command every 100 ms. Goroutines, open files and
// memory are sampled as it runs; at the end they must be flat (memory within the history
// limit). IMUX_SOAK_LOG names a file for the samples.

// soakSample is one sample of the process.
type soakSample struct {
	At         time.Duration
	Goroutines int
	FDs        int
	HeapMiB    float64
	Commands   int64
	Attaches   int64
	Attached   int // the server's attached clients
	Queued     int // events waiting for the loop
}

// openFDs counts the process's open files (/dev/fd on macOS and Linux).
func openFDs(t *testing.T) int {
	ents, err := os.ReadDir("/dev/fd")
	if err != nil {
		t.Fatal(err)
	}
	return len(ents)
}

func soakDuration(t *testing.T) time.Duration {
	v := os.Getenv("IMUX_SOAK_DURATION")
	if v == "" {
		return time.Minute
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// soakAttach attaches a client to session name and keeps reading what it is sent, until the
// connection is closed.
func soakAttach(t *testing.T, path, name string, cols, rows int) net.Conn {
	conn, err := ipc.Dial(path)
	if err != nil {
		t.Fatal(err)
	}
	var rec struct{ Caps []string }
	b, err := os.ReadFile("../../testdata/term/xterm-256color.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &rec); err != nil {
		t.Fatal(err)
	}
	send := func(typ ipc.Type, v any) {
		f, err := ipc.Encode(typ, v)
		if err != nil {
			t.Fatal(err)
		}
		_ = ipc.WriteFrame(conn, f.Type, f.Payload)
	}
	erase := 0x7f
	send(ipc.TypeHello, &ipc.Hello{
		Protocol: ipc.ProtocolVersion, Cwd: "/", StdinTTY: true, StdoutTTY: true, UTF8: true,
		TTYName: "/dev/pts/soak-" + name, Size: ipc.Size{Cols: cols, Rows: rows},
		Env: map[string]string{"TERM": "xterm-256color"}, Terminfo: rec.Caps, Erase: &erase,
	})
	send(ipc.TypeCommand, &ipc.Command{Commands: [][]string{{"attach-session", "-t", name}}})
	rd := ipc.NewReader(conn)
	go func() {
		for {
			if _, err := rd.Read(); err != nil {
				return
			}
		}
	}()
	return conn
}

func TestSoak(t *testing.T) {
	duration := soakDuration(t)
	tui := faketui(t)
	l, path := listen(t)
	s := startServer(t, Options{SocketPath: path, Environ: env})
	go s.Serve(l)
	run := func(words ...string) {
		c, r := dial(t, path, ipc.ProtocolVersion)
		defer c.Close()
		if res := command(t, c, r, words...); res.Code != 0 {
			t.Fatalf("%v: %+v", words, res)
		}
	}
	ask := func(words ...string) {
		c, r := dial(t, path, ipc.ProtocolVersion)
		defer c.Close()
		_ = command(t, c, r, words...)
	}

	stream := "while :; do " + tui + " -mode stream -mb 1; sleep 1; done"
	frame := "exec " + tui + " -mode frame"
	sessions := []string{"s0", "s1", "s2", "s3"}
	for _, name := range sessions {
		run("new-session", "-d", "-s", name, "-x", "120", "-y", "40", stream)
		run("set", "-t", name, "history-limit", "2000")
		run("split-window", "-h", "-t", name, frame)
		run("split-window", "-v", "-t", name+":0.0", stream)
		run("split-window", "-v", "-t", name+":0.2", frame)
	}

	start := time.Now()
	stop := make(chan struct{})
	var wg sync.WaitGroup
	var commands, attaches atomic.Int64

	// Two clients attaching, staying a while, and detaching, at different sizes.
	for i := range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for n := 0; ; n++ {
				select {
				case <-stop:
					return
				default:
				}
				conn := soakAttach(t, path, sessions[(i+n)%len(sessions)], 100+20*i, 30+5*(n%3))
				attaches.Add(1)
				select {
				case <-stop:
					conn.Close()
					return
				case <-time.After(time.Duration(700+300*i) * time.Millisecond):
				}
				conn.Close()
			}
		}()
	}

	// A command every 100 ms, round the ones i-mux uses.
	wg.Add(1)
	go func() {
		defer wg.Done()
		tick := time.NewTicker(100 * time.Millisecond)
		defer tick.Stop()
		for n := 0; ; n++ {
			select {
			case <-stop:
				return
			case <-tick.C:
			}
			// One session for each round of eight, so that the window made is the one killed.
			name := sessions[(n/8)%len(sessions)]
			switch n % 8 {
			case 0:
				ask("display-message", "-p", "-t", name, "#{pane_id} #{window_name} #{client_name}")
			case 1:
				ask("list-panes", "-a", "-F", "#{pane_id} #{pane_current_command} #{pane_width}")
			case 2:
				ask("set-option", "-w", "-t", name, "@imux_tab", "tab "+strconv.Itoa(n))
			case 3:
				ask("select-pane", "-t", name+":0."+strconv.Itoa(n%4))
			case 4:
				ask("capture-pane", "-p", "-t", name+":0.1")
			case 5:
				ask("new-window", "-d", "-t", name, "-n", "tmp", "exec cat")
			case 6:
				ask("kill-window", "-t", name+":tmp")
			case 7:
				ask("set-buffer", "soak "+strconv.Itoa(n))
			}
			commands.Add(1)
		}
	}()

	// Samples every few seconds.
	var samples []soakSample
	every := max(duration/60, time.Second)
	// The server's own state, from its loop; a loop that does not answer in a minute has
	// stopped: the goroutines are written out and the test ends there, rather than at its
	// timeout.
	server := func() (attached, queued int) {
		done := make(chan struct{})
		go func() {
			_ = s.Do(func(*model.Tree) { attached, queued = len(s.attached), len(s.events) })
			close(done)
		}()
		select {
		case <-done:
			return attached, queued
		case <-time.After(time.Minute):
			_ = pprof.Lookup("goroutine").WriteTo(os.Stderr, 1)
			t.Fatalf("at %s the server's loop has not answered for a minute", time.Since(start).Round(time.Second))
			return 0, 0
		}
	}
	logSamples := func() {
		if p := os.Getenv("IMUX_SOAK_LOG"); p != "" {
			if err := os.WriteFile(p, []byte(soakTable(samples)), 0o644); err != nil {
				t.Error(err)
			}
		}
	}
	sample := func() {
		runtime.GC()
		var ms runtime.MemStats
		runtime.ReadMemStats(&ms)
		attached, queued := server()
		samples = append(samples, soakSample{
			At: time.Since(start).Round(time.Second), Goroutines: runtime.NumGoroutine(),
			FDs: openFDs(t), HeapMiB: float64(ms.HeapInuse) / (1 << 20),
			Commands: commands.Load(), Attaches: attaches.Load(), Attached: attached, Queued: queued,
		})
		// Written as it goes, so that a run that fails or is stopped still leaves its samples.
		logSamples()
	}
	tick := time.NewTicker(every)
	deadline := time.After(duration)
loop:
	for {
		select {
		case <-tick.C:
			sample()
		case <-deadline:
			break loop
		}
	}
	tick.Stop()
	close(stop)
	wg.Wait()

	logSamples()
	t.Log("\n" + soakTable(samples))
	soakCheck(t, samples)
}

// soakTable is the samples as a Markdown table.
func soakTable(samples []soakSample) string {
	var b strings.Builder
	b.WriteString("| at | goroutines | fds | heap MiB | commands | attaches | attached | queued |\n")
	b.WriteString("| --- | --- | --- | --- | --- | --- | --- | --- |\n")
	for _, x := range samples {
		fmt.Fprintf(&b, "| %s | %d | %d | %.1f | %d | %d | %d | %d |\n",
			x.At, x.Goroutines, x.FDs, x.HeapMiB, x.Commands, x.Attaches, x.Attached, x.Queued)
	}
	return b.String()
}

// soakCheck fails when goroutines, open files or memory grew between the first and the last
// quarter of the run (after the first sample, which still settles).
func soakCheck(t *testing.T, samples []soakSample) {
	if len(samples) < 8 {
		t.Fatalf("%d samples: the run was too short to judge", len(samples))
	}
	samples = samples[1:]
	q := len(samples) / 4
	median := func(xs []soakSample, f func(soakSample) float64) float64 {
		v := make([]float64, len(xs))
		for i, x := range xs {
			v[i] = f(x)
		}
		sort.Float64s(v)
		return v[len(v)/2]
	}
	first, last := samples[:q], samples[len(samples)-q:]
	for _, c := range []struct {
		name  string
		f     func(soakSample) float64
		slack float64 // how much the last quarter may be above the first, absolute
		ratio float64 // and relative
	}{
		{"goroutines", func(x soakSample) float64 { return float64(x.Goroutines) }, 20, 0.10},
		{"open files", func(x soakSample) float64 { return float64(x.FDs) }, 10, 0.10},
		{"heap", func(x soakSample) float64 { return x.HeapMiB }, 16, 0.25},
		{"attached clients", func(x soakSample) float64 { return float64(x.Attached) }, 2, 0},
		{"queued events", func(x soakSample) float64 { return float64(x.Queued) }, 50, 0},
	} {
		a, b := median(first, c.f), median(last, c.f)
		if b > a*(1+c.ratio)+c.slack {
			t.Errorf("%s grew: %.1f in the first quarter, %.1f in the last", c.name, a, b)
		}
	}
}
