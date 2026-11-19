//go:build windows

package vt

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/sys/windows"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/pty"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/vt/vtfixture"
)

// The VT fixtures through ConPTY (TASKS.md C8.2): a helper (this test binary) writes each
// fixture's bytes raw into a pseudo console of its size, the emulator reads what ConPTY makes of
// them, and the screen is compared with the one tmux showed on Unix.

const fixtureEnv = "IMUX_VT_CONPTY_FIXTURE"

// TestConPTYFixtureHelper is the helper: run in the pseudo console, it writes the fixture with
// VT processing, wrapping at the end of a line, and no newline auto return (LF moves down, as in
// a raw Unix terminal), then waits to be killed.
func TestConPTYFixtureHelper(t *testing.T) {
	path := os.Getenv(fixtureEnv)
	if path == "" {
		t.Skip("the helper of TestConPTYFixtures")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		os.Exit(1)
	}
	out := windows.Handle(os.Stdout.Fd())
	_ = windows.SetConsoleOutputCP(65001)
	_ = windows.SetConsoleMode(out, windows.ENABLE_PROCESSED_OUTPUT|windows.ENABLE_WRAP_AT_EOL_OUTPUT|
		windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING|windows.DISABLE_NEWLINE_AUTO_RETURN)
	var n uint32
	_ = windows.WriteFile(out, data, &n, nil)
	for {
		time.Sleep(time.Hour)
	}
}

// conptyOutput collects a pseudo console's output and says when it has been quiet.
type conptyOutput struct {
	mu   sync.Mutex
	buf  []byte
	fed  int
	last time.Time
}

func (o *conptyOutput) quiet(d time.Duration) {
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		o.mu.Lock()
		ok := len(o.buf) > 0 && time.Since(o.last) >= d
		o.mu.Unlock()
		if ok {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// take returns what came since the last take.
func (o *conptyOutput) take() []byte {
	o.mu.Lock()
	defer o.mu.Unlock()
	b := o.buf[o.fed:]
	o.fed = len(o.buf)
	return b
}

// conptyFixture runs one fixture through ConPTY and returns the emulator's state.
func conptyFixture(t *testing.T, in string) string {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	sizes := vtfixture.Sizes(in)
	abs, _ := filepath.Abs(in)
	p, err := pty.New().Start(pty.Spec{
		Path: exe, Argv: []string{exe, "-test.run", "^TestConPTYFixtureHelper$"},
		Env: append(os.Environ(), fixtureEnv+"="+abs), Cols: sizes[0][0], Rows: sizes[0][1],
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = p.Kill(); _, _ = p.Wait(); _ = p.Close() }()
	o := &conptyOutput{}
	go func() {
		b := make([]byte, 32<<10)
		for {
			n, err := p.Read(b)
			o.mu.Lock()
			o.buf = append(o.buf, b[:n]...)
			if n > 0 {
				o.last = time.Now()
			}
			o.mu.Unlock()
			if err != nil {
				return
			}
		}
	}()
	term := NewTerminal(sizes[0][0], sizes[0][1], 2000)
	term.Screen.Title = vtfixture.InitialTitle
	o.quiet(500 * time.Millisecond)
	term.Write(o.take())
	for _, size := range sizes[1:] {
		_ = p.Resize(size[0], size[1], 0, 0)
		term.Resize(size[0], size[1])
		o.quiet(500 * time.Millisecond)
		term.Write(o.take())
	}
	cols, rows := vtfixture.Final(in)
	return fixtureState(term, cols, rows)
}

// conhostDiffers are the fixtures whose visible screen is the console host's own result, not
// tmux's (COMPATIBILITY.md K12, K13), as seen on build 26200.
var conhostDiffers = map[string]string{
	"alternate-narrow-write@10x3-5x3": "K12", "altscreen-47@12x4": "K12", "csi-ignored@12x4": "K12",
	"cursor-move@12x6": "K12", "cursor-pending-wrap@10x5": "K12", "edit-chars@12x6": "K12",
	"edit-lines@10x6": "K12", "modes-deccolm@20x4": "K12", "tabs-stops@20x6": "K12",
	"utf8-invalid@20x5": "K12", "reflow-history@10x3-5x3-15x4": "K13",
}

func TestConPTYFixtures(t *testing.T) {
	if os.Getenv(fixtureEnv) != "" {
		t.Skip("in the helper")
	}
	if v := windows.RtlGetVersion(); v.BuildNumber < 26100 {
		t.Skipf("build %d: older than 26100, ConPTY loses more (COMPATIBILITY.md K10)", v.BuildNumber)
	}
	ins, err := filepath.Glob("../../testdata/vt/*.in")
	if err != nil || len(ins) == 0 {
		t.Fatalf("no fixtures: %v", err)
	}
	for _, in := range ins {
		name := strings.TrimSuffix(filepath.Base(in), ".in")
		t.Run(name, func(t *testing.T) {
			want, err := os.ReadFile(strings.TrimSuffix(in, ".in") + ".screen")
			if err != nil {
				t.Fatal(err)
			}
			got := conptyFixture(t, in)
			w, g := visible(string(want)), visible(got)
			if k, known := conhostDiffers[name]; known {
				t.Logf("%s: the console host's own result (COMPATIBILITY.md %s); same as tmux's: %v", name, k, w == g)
				return
			}
			if w != g {
				t.Errorf("through ConPTY the visible screen differs from tmux's:\n%s", lineDiff(w, g))
			}
		})
	}
}

// decGraphics is the DEC special graphics set, as Unicode: ConPTY writes these characters where
// a program drew lines with SO and SI.
var decGraphics = map[rune]rune{
	'`': '◆', 'a': '▒', 'f': '°', 'g': '±', 'j': '┘', 'k': '┐', 'l': '┌', 'm': '└', 'n': '┼',
	'o': '⎺', 'p': '⎻', 'q': '─', 'r': '⎼', 's': '⎽', 't': '├', 'u': '┤', 'v': '┴', 'w': '┬',
	'x': '│', 'y': '≤', 'z': '≥', '{': 'π', '|': '≠', '}': '£', '~': '·',
}

var (
	sgr  = regexp.MustCompile("\x1b\\[[0-9;:]*m")
	osc8 = regexp.MustCompile("\x1b\\]8;[^\x1b\x07]*(\x1b\\\\|\x07)")
	size = regexp.MustCompile(`^size (\d+)x`)
)

// visible is what a fixture state shows on the screen: the cursor and the screen's rows as
// text, without attributes, with tabs as the spaces they cover, line drawing as its Unicode
// characters, and no trailing blanks. That is what ConPTY gives back the same; the rest of the
// state (modes, history, attributes) is conhost's (COMPATIBILITY.md K11 to K13).
func visible(state string) string {
	var out []string
	in := false
	width := 0
	for _, l := range strings.Split(state, "\n") {
		switch {
		case size.MatchString(l):
			width, _ = strconv.Atoi(size.FindStringSubmatch(l)[1])
		case strings.HasPrefix(l, "cursor "):
			out = append(out, l)
		case l == "--- screen":
			in = true
		case strings.HasPrefix(l, "--- "):
			in = false
		case in:
			l = strings.TrimSuffix(osc8.ReplaceAllString(sgr.ReplaceAllString(l, ""), ""), "$")
			var b strings.Builder
			col, shifted := 0, false
			for _, r := range l {
				switch {
				case r == 0x0e:
					shifted = true
					continue
				case r == 0x0f:
					shifted = false
					continue
				case r == '\t':
					// To the next stop at a multiple of 8, or the last column.
					n := max(min(8-col%8, width-1-col), 1)
					b.WriteString(strings.Repeat(" ", n))
					col += n
					continue
				case shifted:
					if g, ok := decGraphics[r]; ok {
						r = g
					}
				}
				b.WriteRune(r)
				col += max(RuneWidth(r), 0)
			}
			out = append(out, strings.TrimRight(b.String(), " "))
		}
	}
	return strings.Join(out, "\n")
}
