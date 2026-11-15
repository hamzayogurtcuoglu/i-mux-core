package vt

import (
	"fmt"
	"math/rand/v2"
	"os"
	"strings"
	"testing"
	"time"
)

// mixedStream returns a deterministic byte stream of about size bytes that mixes what agent
// TUIs send: plain and colored lines, cursor movement and erasing, synchronized frames on the
// alternate screen, wide characters and emoji, titles, links, scroll regions and tabs.
func mixedStream(size int) []byte {
	r := rand.New(rand.NewPCG(1, 2))
	words := strings.Fields("the quick brown fox jumps over a lazy dog pane window session agent build test 日本語 中文 한국어 🙂 👍🏽 ✨ │ ─ ┌ ┐ └ ┘ é ñ ü")
	var b strings.Builder
	b.Grow(size + 4096)
	line := func() string {
		var l strings.Builder
		for i := 0; i < 5+r.IntN(12); i++ {
			l.WriteString(words[r.IntN(len(words))])
			l.WriteByte(' ')
		}
		return l.String()
	}
	for b.Len() < size {
		switch k := r.IntN(100); {
		case k < 40:
			b.WriteString(line() + "\r\n")
		case k < 60:
			switch r.IntN(3) {
			case 0:
				fmt.Fprintf(&b, "\x1b[%d;%dm%s\x1b[0m\r\n", 30+r.IntN(8), 40+r.IntN(8), line())
			case 1:
				fmt.Fprintf(&b, "\x1b[1;4:3;38;5;%dm%s\x1b[0m\r\n", r.IntN(256), line())
			default:
				fmt.Fprintf(&b, "\x1b[38;2;%d;%d;%dm\x1b[48;2;%d;%d;%dm%s\x1b[m\r\n", r.IntN(256), r.IntN(256), r.IntN(256), r.IntN(256), r.IntN(256), r.IntN(256), line())
			}
		case k < 75:
			for i := 0; i < 10; i++ {
				fmt.Fprintf(&b, "\x1b[%d;%dH\x1b[K%s", 1+r.IntN(50), 1+r.IntN(150), line())
			}
			b.WriteString("\x1b[2J\x1b[H")
		case k < 90:
			b.WriteString("\x1b[?2026h\x1b[?1049h\x1b[H")
			for i := 0; i < 20; i++ {
				fmt.Fprintf(&b, "\x1b[%d;1H│ %s\x1b[0K", i+1, line())
			}
			b.WriteString("\x1b[?1049l\x1b[?2026l")
		case k < 95:
			fmt.Fprintf(&b, "\x1b]2;%s\x07\x1b]8;id=%d;https://example.com/%d\x1b\\link\x1b]8;;\x1b\\\r\n", line(), r.IntN(100), r.IntN(1000))
		default:
			fmt.Fprintf(&b, "\x1b[%d;%dr\x1b[%dS\x1b[%dT\x1b[r\ta\tb\tc\r\n", 1+r.IntN(10), 20+r.IntN(30), 1+r.IntN(3), 1+r.IntN(3))
		}
	}
	return []byte(b.String())
}

// countHandler counts actions, so the benchmark measures the parser and not a screen.
type countHandler struct{ prints, utf8, controls, esc, csi, osc, dcs, apc int }

func (h *countHandler) Print(run []byte)                  { h.prints += len(run) }
func (h *countHandler) PrintUTF8([]byte, bool)            { h.utf8++ }
func (h *countHandler) Execute(byte)                      { h.controls++ }
func (h *countHandler) EscDispatch([]byte, byte)          { h.esc++ }
func (h *countHandler) CsiDispatch(*Params, []byte, byte) { h.csi++ }
func (h *countHandler) OscDispatch([]byte, bool)          { h.osc++ }
func (h *countHandler) DcsDispatch(*Params, []byte, []byte) {
	h.dcs++
}
func (h *countHandler) ApcDispatch([]byte)    { h.apc++ }
func (h *countHandler) RenameDispatch([]byte) {}

func BenchmarkParserMixed(b *testing.B) {
	data := mixedStream(8 << 20)
	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	for range b.N {
		p := NewParser(&countHandler{})
		for i := 0; i < len(data); i += 32 << 10 {
			p.Write(data[i:min(i+32<<10, len(data))])
		}
	}
}

// TestParserFixtureThroughput measures the parser on a large fixture file, when one is given
// (IMUX_VT_FIXTURE): the spike for decision E10 used a 100 MB one.
func TestParserFixtureThroughput(t *testing.T) {
	path := os.Getenv("IMUX_VT_FIXTURE")
	if path == "" {
		t.Skip("IMUX_VT_FIXTURE not set")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	h := &countHandler{}
	p := NewParser(h)
	start := time.Now()
	for i := 0; i < len(data); i += 32 << 10 {
		p.Write(data[i:min(i+32<<10, len(data))])
	}
	d := time.Since(start)
	t.Logf("%d bytes in %v: %.1f MB/s; %+v", len(data), d, float64(len(data))/1e6/d.Seconds(), *h)
}
