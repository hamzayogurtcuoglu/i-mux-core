//go:build windows

package cli

import (
	"errors"
	"os"
	"strconv"

	"golang.org/x/sys/windows"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/ipc"
)

// The attached client's console on Windows (ARCHITECTURE.md §6, TASKS.md C8.5): VT input and
// output, UTF-8 code pages, Ctrl+C as a key, resizes from WINDOW_BUFFER_SIZE_EVENT.

// utf8CodePage is CP_UTF8.
const utf8CodePage = 65001

type console struct {
	in, out windows.Handle
}

func newConsole() *console {
	return &console{in: windows.Handle(os.Stdin.Fd()), out: windows.Handle(os.Stdout.Fd())}
}

// size is the console window's size (not its buffer's). A console has no size in pixels.
func (c *console) size() (ipc.Size, error) {
	var info windows.ConsoleScreenBufferInfo
	if err := windows.GetConsoleScreenBufferInfo(c.out, &info); err != nil {
		return ipc.Size{}, err
	}
	w := info.Window
	return ipc.Size{Cols: int(w.Right-w.Left) + 1, Rows: int(w.Bottom-w.Top) + 1}, nil
}

// makeRaw puts the console in the modes the engine needs and returns how to restore it:
//   - input: VT sequences for keys (ENABLE_VIRTUAL_TERMINAL_INPUT), size events, and nothing
//     processed — no line editing, no echo, Ctrl+C a key (no ENABLE_PROCESSED_INPUT) and no quick
//     edit selecting with the mouse;
//   - output: VT processing, with no newline auto return (the engine writes CR itself);
//   - UTF-8 code pages both ways.
func (c *console) makeRaw() (restore func(), err error) {
	var inMode, outMode uint32
	if err := windows.GetConsoleMode(c.in, &inMode); err != nil {
		return nil, err
	}
	if err := windows.GetConsoleMode(c.out, &outMode); err != nil {
		return nil, err
	}
	inCP, _ := windows.GetConsoleCP()
	outCP, _ := windows.GetConsoleOutputCP()
	raw := uint32(windows.ENABLE_VIRTUAL_TERMINAL_INPUT | windows.ENABLE_WINDOW_INPUT | windows.ENABLE_EXTENDED_FLAGS)
	if err := windows.SetConsoleMode(c.in, raw); err != nil {
		return nil, err
	}
	vt := outMode | windows.ENABLE_PROCESSED_OUTPUT | windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING | windows.DISABLE_NEWLINE_AUTO_RETURN
	if err := windows.SetConsoleMode(c.out, vt); err != nil {
		_ = windows.SetConsoleMode(c.in, inMode)
		return nil, errors.New("this console has no VT processing (Windows 10 1809 or later is needed)")
	}
	_ = windows.SetConsoleCP(utf8CodePage)
	_ = windows.SetConsoleOutputCP(utf8CodePage)
	return func() {
		_ = windows.SetConsoleMode(c.in, inMode)
		_ = windows.SetConsoleMode(c.out, outMode)
		if inCP != 0 {
			_ = windows.SetConsoleCP(inCP)
		}
		if outCP != 0 {
			_ = windows.SetConsoleOutputCP(outCP)
		}
	}, nil
}

// ttyName names a console client: a console has no device path, so its process id makes the
// name (#{client_name}, -c and -t) unique.
func ttyName(*os.File) string { return "console-" + strconv.Itoa(os.Getpid()) }

// eraseChar: a console sends DEL for Backspace in VT input mode, which tmux's default reads as
// BSpace anyway.
func eraseChar(*os.File) (int, bool) { return 0x7f, true }

// consoleUTF8: the client puts its console in UTF-8, so it is a UTF-8 client.
func consoleUTF8() bool { return isTerminal(os.Stdout) }
