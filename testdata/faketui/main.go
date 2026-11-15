// Command faketui stands in for an agent CLI in tests (TASKS.md C1.9): it does to a terminal
// what full-screen agent programs do, without being one. Modes:
//
//	frame   a full-screen frame on the alternate screen inside synchronized output (2026):
//	        a box, RGB colours, wide characters, emoji and an OSC 8 link; q leaves
//	keys    echo every input chunk on line 2 as hex and decoded; ^D leaves
//	mouse   turn on SGR mouse reporting (1000, 1002, 1006) and echo events; ^D leaves
//	paste   turn on bracketed paste (2004) and echo what is pasted; ^D leaves
//	resize  print the size, and again on every resize; ^D leaves
//	stream  write -mb MB of mixed output, then exit
//	title   set the title (OSC 2), the working directory (OSC 7) and copy (OSC 52), then exit
//	exit    exit with -code, or kill itself with -signal (Unix)
//
// It reads and writes the terminal raw. Every mode prints "READY" once it waits for input.
package main

import (
	"encoding/base64"
	"encoding/hex"
	"flag"
	"fmt"
	"math/rand/v2"
	"os"
	"strings"
	"time"
)

func main() {
	mode := flag.String("mode", "frame", "frame, keys, mouse, paste, resize, stream, title or exit")
	mb := flag.Int("mb", 1, "stream: megabytes to write")
	code := flag.Int("code", 0, "exit: the exit code")
	sig := flag.String("signal", "", "exit: a signal to kill itself with (Unix)")
	title := flag.String("title", "faketui", "title: the title")
	cwd := flag.String("cwd", "/tmp", "title: the working directory")
	copyText := flag.String("copy", "copied by faketui", "title: the text to copy")
	flag.Parse()

	switch *mode {
	case "stream":
		stream(*mb)
		return
	case "title":
		host, _ := os.Hostname()
		fmt.Printf("\x1b]2;%s\x07\x1b]7;file://%s%s\x07\x1b]52;c;%s\x07done\r\n",
			*title, host, *cwd, base64.StdEncoding.EncodeToString([]byte(*copyText)))
		return
	case "exit":
		if *sig != "" {
			if err := killSelf(*sig); err != nil {
				fmt.Fprintln(os.Stderr, "faketui:", err)
				os.Exit(2)
			}
			time.Sleep(time.Minute) // the signal arrives
		}
		os.Exit(*code)
	}

	restore, err := makeRaw()
	if err != nil {
		fmt.Fprintln(os.Stderr, "faketui:", err)
		os.Exit(2)
	}
	defer restore()
	switch *mode {
	case "frame":
		frame()
	case "keys":
		echo("", "key", nil)
	case "mouse":
		echo("\x1b[?1000h\x1b[?1002h\x1b[?1006h", "mouse", decodeMouse)
		os.Stdout.WriteString("\x1b[?1006l\x1b[?1002l\x1b[?1000l")
	case "paste":
		echo("\x1b[?2004h", "paste", decodePaste)
		os.Stdout.WriteString("\x1b[?2004l")
	case "resize":
		resize()
	default:
		fmt.Fprintln(os.Stderr, "faketui: unknown mode", *mode)
		restore()
		os.Exit(2)
	}
}

// frame draws a full screen and redraws it when the size changes, until q.
func frame() {
	os.Stdout.WriteString("\x1b[?1049h")
	draw := func() {
		cols, rows := size()
		var b strings.Builder
		b.WriteString("\x1b[?2026h\x1b[H\x1b[2J")
		b.WriteString("┌" + strings.Repeat("─", max(cols-2, 0)) + "┐\r\n")
		for y := 1; y < rows-1; y++ {
			b.WriteString(fmt.Sprintf("\x1b[%d;1H│\x1b[%d;%dH│", y+1, y+1, cols))
		}
		b.WriteString(fmt.Sprintf("\x1b[%d;1H└%s┘", rows, strings.Repeat("─", max(cols-2, 0))))
		b.WriteString("\x1b[2;3H\x1b[38;2;255;128;0mfaketui\x1b[0m 日本語 🙂")
		b.WriteString("\x1b[3;3H\x1b]8;id=ft;https://example.com/faketui\x1b\\link\x1b]8;;\x1b\\")
		b.WriteString(fmt.Sprintf("\x1b[4;3Hsize %dx%d", cols, rows))
		b.WriteString("\x1b[?2026l")
		os.Stdout.WriteString(b.String())
	}
	draw()
	os.Stdout.WriteString("\x1b[5;3HREADY")
	resized := onResize()
	keys := readInput()
	for {
		select {
		case <-resized:
			draw()
		case in, ok := <-keys:
			if !ok || strings.Contains(in, "q") {
				os.Stdout.WriteString("\x1b[?1049l")
				return
			}
		}
	}
}

// echo turns on a mode, then writes each input chunk on line 2 until ^D.
func echo(on, label string, decode func(string) string) {
	os.Stdout.WriteString(on + "\x1b[H\x1b[2J" + label + " READY")
	for in := range readInput() {
		if strings.Contains(in, "\x04") {
			return
		}
		d := decodeKeys(in)
		if decode != nil {
			d = decode(in)
		}
		fmt.Printf("\x1b[2;1H\x1b[K%s: %s %s", label, hex.EncodeToString([]byte(in)), d)
	}
}

// resize prints the size, then again after each resize, until ^D.
func resize() {
	show := func() {
		cols, rows := size()
		fmt.Printf("\x1b[H\x1b[2Jsize: %dx%d READY", cols, rows)
	}
	show()
	resized := onResize()
	keys := readInput()
	for {
		select {
		case <-resized:
			show()
		case in, ok := <-keys:
			if !ok || strings.Contains(in, "\x04") {
				return
			}
		}
	}
}

// readInput sends the input in chunks as it is read, and closes at the end of it.
func readInput() <-chan string {
	ch := make(chan string)
	go func() {
		defer close(ch)
		buf := make([]byte, 4096)
		for {
			n, err := os.Stdin.Read(buf)
			if n > 0 {
				ch <- string(buf[:n])
			}
			if err != nil {
				return
			}
		}
	}()
	return ch
}

var keyNames = map[string]string{
	"\x1b[A": "Up", "\x1b[B": "Down", "\x1b[C": "Right", "\x1b[D": "Left",
	"\x1bOA": "Up", "\x1bOB": "Down", "\x1bOC": "Right", "\x1bOD": "Left",
	"\r": "Enter", "\t": "Tab", "\x7f": "BSpace", "\x1b": "Escape", "\x1b[Z": "BTab",
	"\x1b[13;2u": "S-Enter", "\x1b[13;5u": "C-Enter", "\x1b[27;2;13~": "S-Enter", "\x1b[27;5;13~": "C-Enter",
}

// decodeKeys names a chunk of key input where it can.
func decodeKeys(in string) string {
	if n, ok := keyNames[in]; ok {
		return n
	}
	if len(in) == 1 && in[0] < 0x20 {
		return "C-" + string(rune(in[0]+'a'-1))
	}
	if len(in) == 2 && in[0] == 0x1b {
		return "M-" + in[1:]
	}
	return fmt.Sprintf("%q", in)
}

// decodeMouse names SGR mouse events: "press 0 at 10,5".
func decodeMouse(in string) string {
	var out []string
	for _, ev := range strings.Split(in, "\x1b[<")[1:] {
		var b, x, y int
		var kind byte
		if _, err := fmt.Sscanf(ev, "%d;%d;%d%c", &b, &x, &y, &kind); err != nil {
			continue
		}
		what := "press"
		if kind == 'm' {
			what = "release"
		}
		out = append(out, fmt.Sprintf("%s %d at %d,%d", what, b, x, y))
	}
	return strings.Join(out, ", ")
}

// decodePaste returns what was pasted between the brackets.
func decodePaste(in string) string {
	start := strings.Index(in, "\x1b[200~")
	end := strings.Index(in, "\x1b[201~")
	if start < 0 || end < start {
		return "not bracketed: " + fmt.Sprintf("%q", in)
	}
	return fmt.Sprintf("pasted %q", in[start+6:end])
}

// stream writes about mb megabytes of mixed output.
func stream(mb int) {
	r := rand.New(rand.NewPCG(3, 4))
	words := strings.Fields("agent build test pane 日本語 🙂 │ ─ é done ok")
	var b strings.Builder
	total := mb << 20
	for written := 0; written < total; {
		b.Reset()
		for i := 0; i < 64; i++ {
			switch r.IntN(4) {
			case 0:
				fmt.Fprintf(&b, "\x1b[38;5;%dm%s\x1b[0m ", r.IntN(256), words[r.IntN(len(words))])
			case 1:
				fmt.Fprintf(&b, "\x1b[38;2;%d;%d;%dm%s\x1b[m ", r.IntN(256), r.IntN(256), r.IntN(256), words[r.IntN(len(words))])
			default:
				b.WriteString(words[r.IntN(len(words))] + " ")
			}
		}
		b.WriteString("\r\n")
		n, err := os.Stdout.WriteString(b.String())
		if err != nil {
			os.Exit(1)
		}
		written += n
	}
	os.Stdout.WriteString("STREAM DONE\r\n")
}
