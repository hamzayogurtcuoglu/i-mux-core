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

// ClientTerminfo is the terminfo directory attached clients read (TERMINFO), so that tmux and
// i-mux-core see the same terminal everywhere; "" leaves the system's.
var ClientTerminfo = ""

// SettleTime is how long a client must write nothing before its screen is taken.
var SettleTime = 100 * time.Millisecond

// Screen implements Screener: the program attaches in a PTY, and is killed once its screen is
// taken.
func (b *Binary) Screen(ctx context.Context, env *Env, cols, rows int) Result {
	argv := []string{filepath.Base(b.Path), "-S", env.Socket}
	if env.Config != "" {
		argv = append(argv, "-f", env.Config)
	}
	argv = append(argv, "-u", "attach-session")
	vars := append([]string(nil), env.Vars...)
	if ClientTerminfo != "" {
		vars = append(vars, "TERMINFO="+ClientTerminfo)
	}
	p, err := pty.New().Start(pty.Spec{Path: b.Path, Argv: argv, Env: vars, Dir: env.Dir, Cols: cols, Rows: rows})
	if err != nil {
		return Result{Code: -1, Stderr: "difftest: attach: " + err.Error()}
	}
	defer func() {
		_ = p.Kill()
		_, _ = p.Wait()
		_ = p.Close()
	}()
	var (
		mu    sync.Mutex
		out   []byte
		last  time.Time
		ended bool
	)
	go func() {
		buf := make([]byte, 32<<10)
		for {
			n, err := p.Read(buf)
			mu.Lock()
			if n > 0 {
				out = append(out, buf[:n]...)
				last = time.Now()
			}
			if err != nil {
				ended = true
			}
			mu.Unlock()
			if err != nil {
				return
			}
		}
	}()
	term := vt.NewTerminal(cols, rows, 0)
	fed := 0
	deadline := time.Now().Add(WaitTimeout)
	for {
		mu.Lock()
		_, _ = term.Write(out[fed:])
		fed = len(out)
		quiet := fed > 0 && time.Since(last) >= SettleTime
		done := ended
		mu.Unlock()
		// Settled: nothing written for a while, and no synchronized update pending.
		if (quiet || done) && term.Screen.Mode&vt.ModeSync == 0 {
			return Result{Stdout: renderfixture.Dump(term)}
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			return Result{Stdout: renderfixture.Dump(term), Stderr: "difftest: the client did not settle"}
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// Screen implements Screener for a stub: it answers the step's words, "screen" and "WxH".
func (f *Func) Screen(_ context.Context, _ *Env, cols, rows int) Result {
	return f.Fn(screenArgs(cols, rows))
}

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
