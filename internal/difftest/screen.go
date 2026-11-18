package difftest

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/pty"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/render/renderfixture"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/vt"
)

// The headless client (TASKS.md C2.9): a script's `screen WxH` step attaches a client of that
// size inside a PTY the harness owns, feeds what the client writes into an emulator until it
// settles, and compares what the client's terminal shows (renderfixture.Dump: the cells as
// they can be seen, the cursor and the terminal's modes).

// Screener is an engine that can attach a client and say what its terminal shows.
type Screener interface {
	// Screen attaches a client of cols×rows and returns the Dump of its terminal once quiet.
	Screen(ctx context.Context, env *Env, cols, rows int) Result
}

// Attacher is an engine whose clients can stay attached across steps (TASKS.md C2.12).
type Attacher interface {
	// Attach attaches a client of cols×rows, called name in the script, whose terminal's cells
	// are xpixel×ypixel pixels (0 for a terminal that does not say).
	Attach(ctx context.Context, env *Env, name string, cols, rows, xpixel, ypixel int) (Client, error)
}

// Client is an attached client the harness drives.
type Client interface {
	// Screen returns the Dump of its terminal once it has settled.
	Screen(ctx context.Context) Result
	// Resize resizes its terminal, as a terminal emulator does: its screen first, then the PTY.
	Resize(cols, rows int) error
	// Type writes b to its terminal, as keys typed into it.
	Type(b []byte) error
	Close()
}

// ClientTerminfo is the terminfo directory attached clients read (TERMINFO), so that tmux and
// i-mux-core see the same terminal everywhere; "" leaves the system's.
var ClientTerminfo = ""

// SettleTime is how long a client must write nothing before its screen is taken.
var SettleTime = 100 * time.Millisecond

// headless is a client in a PTY whose output is fed to an emulator.
type headless struct {
	p     pty.Proc
	mu    sync.Mutex
	out   []byte
	last  time.Time
	ended bool
	term  *vt.Terminal
	fed   int

	xpixel, ypixel int // a cell's size in pixels, which resizes keep
}

// Attach implements Attacher: the program runs attach-session in a PTY the harness owns.
func (b *Binary) Attach(ctx context.Context, env *Env, _ string, cols, rows, xpixel, ypixel int) (Client, error) {
	argv := []string{filepath.Base(b.Path), "-S", env.Socket}
	if env.Config != "" {
		argv = append(argv, "-f", env.Config)
	}
	argv = append(argv, "-u", "attach-session")
	vars := append([]string(nil), env.Vars...)
	if ClientTerminfo != "" {
		vars = append(vars, "TERMINFO="+ClientTerminfo)
	}
	p, err := pty.New().Start(pty.Spec{Path: b.Path, Argv: argv, Env: vars, Dir: env.Dir, Cols: cols, Rows: rows, XPixel: xpixel, YPixel: ypixel})
	if err != nil {
		return nil, err
	}
	h := &headless{p: p, term: vt.NewTerminal(cols, rows, 0), xpixel: xpixel, ypixel: ypixel}
	go func() {
		buf := make([]byte, 32<<10)
		for {
			n, err := p.Read(buf)
			h.mu.Lock()
			if n > 0 {
				h.out = append(h.out, buf[:n]...)
				h.last = time.Now()
			}
			if err != nil {
				h.ended = true
			}
			h.mu.Unlock()
			if err != nil {
				return
			}
		}
	}()
	deadline := time.Now().Add(WaitTimeout)
	for {
		h.mu.Lock()
		ready := len(h.out) > 0 || h.ended
		h.mu.Unlock()
		if ready || time.Now().After(deadline) || ctx.Err() != nil {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	return h, nil
}

// Output is everything the client's terminal was sent so far, escape sequences and all.
func (h *headless) Output() []byte {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]byte(nil), h.out...)
}

// feed gives the emulator what the client wrote since.
func (h *headless) feed() {
	_, _ = h.term.Write(h.out[h.fed:])
	h.fed = len(h.out)
}

// Screen implements Client: once nothing has been written for SettleTime, counted from the
// step's start too so that a change still on its way is waited for, and no synchronized
// update is pending.
func (h *headless) Screen(ctx context.Context) Result {
	start := time.Now()
	deadline := start.Add(WaitTimeout)
	for {
		h.mu.Lock()
		h.feed()
		since := h.last
		if since.Before(start) {
			since = start
		}
		quiet := h.fed > 0 && time.Since(since) >= SettleTime
		done := h.ended && time.Since(start) >= SettleTime
		h.mu.Unlock()
		if (quiet || done) && h.term.Screen.Mode&vt.ModeSync == 0 {
			return Result{Stdout: renderfixture.Dump(h.term)}
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			return Result{Stdout: renderfixture.Dump(h.term), Stderr: "difftest: the client did not settle"}
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// Resize implements Client.
func (h *headless) Resize(cols, rows int) error {
	h.mu.Lock()
	h.feed()
	h.term.Resize(cols, rows)
	h.mu.Unlock()
	return h.p.Resize(cols, rows, h.xpixel, h.ypixel)
}

// Type implements Client.
func (h *headless) Type(b []byte) error {
	_, err := h.p.Write(b)
	return err
}

// Close implements Client: the client is killed.
func (h *headless) Close() {
	_ = h.p.Kill()
	_, _ = h.p.Wait()
	_ = h.p.Close()
}

// Screen implements Screener: a client attaches, and is killed once its screen is taken.
func (b *Binary) Screen(ctx context.Context, env *Env, cols, rows int) Result {
	c, err := b.Attach(ctx, env, "", cols, rows, 0, 0)
	if err != nil {
		return Result{Code: -1, Stderr: "difftest: attach: " + err.Error()}
	}
	defer c.Close()
	return c.Screen(ctx)
}

// Screen implements Screener for a stub: it answers the step's words, "screen" and "WxH".
func (f *Func) Screen(_ context.Context, _ *Env, cols, rows int) Result {
	return f.Fn(screenArgs(cols, rows))
}

// Attach implements Attacher for a stub: its clients answer "screen NAME".
func (f *Func) Attach(_ context.Context, _ *Env, name string, _, _ int) (Client, error) {
	return &funcClient{f: f, name: name}, nil
}

type funcClient struct {
	f    *Func
	name string
}

func (c *funcClient) Screen(context.Context) Result { return c.f.Fn([]string{"screen", c.name}) }
func (c *funcClient) Resize(int, int) error         { return nil }
func (c *funcClient) Type([]byte) error             { return nil }
func (c *funcClient) Close()                        {}

func screenArgs(cols, rows int) []string {
	return []string{"screen", strconv.Itoa(cols) + "x" + strconv.Itoa(rows)}
}

// screenCell is one cell of a dumped line: its character and the style it is in.
type screenCell struct{ ch, style string }

// dumpCells splits a Dump line into cells; a wide character's second column has no character.
func dumpCells(line string) []screenCell {
	var cells []screenCell
	style := "{}"
	rs := []rune(line)
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		switch {
		case r == '{':
			j := i
			for j < len(rs) && rs[j] != '}' {
				j++
			}
			style = string(rs[i : j+1])
			i = j
			continue
		case r == '\\' && i+1 < len(rs):
			i++
			r = rs[i]
		}
		w := vt.RuneWidth(r)
		if w == 0 && len(cells) > 0 {
			cells[len(cells)-1].ch += string(r)
			continue
		}
		cells = append(cells, screenCell{string(r), style})
		for range w - 1 {
			cells = append(cells, screenCell{"", style})
		}
	}
	return cells
}

// sideBySide shows two Dumps next to each other, the lines that differ marked with ≠ and
// their differing cells with ^ below the second.
func sideBySide(nameA, nameB, a, b string) string {
	la, lb := strings.Split(strings.TrimSuffix(a, "\n"), "\n"), strings.Split(strings.TrimSuffix(b, "\n"), "\n")
	var out strings.Builder
	fmt.Fprintf(&out, "  the client's screen differs (%s | %s; ^ marks the differing cells):\n", nameA, nameB)
	screen := false
	width, row := 0, 0
	for i := range max(len(la), len(lb)) {
		var x, y string
		if i < len(la) {
			x = la[i]
		}
		if i < len(lb) {
			y = lb[i]
		}
		if !screen {
			if x == "--- screen" {
				screen = true
				continue
			}
			if strings.HasPrefix(x, "size ") {
				if w, _, ok := strings.Cut(strings.TrimPrefix(x, "size "), "x"); ok {
					width, _ = strconv.Atoi(w)
				}
			}
			if x != y {
				fmt.Fprintf(&out, "  ≠ %-30s | %s\n", x, y)
			}
			continue
		}
		ca, cb := dumpCells(x), dumpCells(y)
		text := func(cs []screenCell) string {
			var s strings.Builder
			n := 0
			for _, c := range cs {
				s.WriteString(c.ch)
				n++
			}
			s.WriteString(strings.Repeat(" ", max(0, width-n)))
			return s.String()
		}
		mark := "   "
		if x != y {
			mark = "  ≠"
		}
		fmt.Fprintf(&out, "%s %2d %s | %s\n", mark, row, text(ca), text(cb))
		row++
		if x == y {
			continue
		}
		var carets strings.Builder
		for c := range max(len(ca), len(cb), width) {
			var p, q screenCell
			if c < len(ca) {
				p = ca[c]
			}
			if c < len(cb) {
				q = cb[c]
			}
			blank := screenCell{" ", "{}"}
			if p == (screenCell{}) {
				p = blank
			}
			if q == (screenCell{}) {
				q = blank
			}
			if p != q {
				carets.WriteByte('^')
			} else {
				carets.WriteByte(' ')
			}
		}
		fmt.Fprintf(&out, "       %s   %s\n", strings.Repeat(" ", width), strings.TrimRight(carets.String(), " "))
		fmt.Fprintf(&out, "       %s: %q\n       %s: %q\n", nameA, x, nameB, y)
	}
	return out.String()
}
