//go:build windows

package cli

import (
	"fmt"
	"io"
	"net"
	"sync"
	"unicode/utf16"
	"unicode/utf8"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/ipc"
)

// runAttached is the attached client on Windows (TASKS.md C8.5), as on Unix
// (attached_unix.go): the console in raw VT mode, the server's output written to it, its keys and
// size sent back, until the server says the client is done or goes away. The console is restored
// on every way out: the end, a lost server, and the console being closed (its control handler).
func runAttached(c net.Conn, r *ipc.Reader, stdout io.Writer) (int, error) {
	con := newConsole()
	restore, err := con.makeRaw()
	if err != nil {
		return 1, fmt.Errorf("open terminal failed: %v", err)
	}
	var once sync.Once
	restoreOnce := func() { once.Do(restore) }
	defer restoreOnce()

	var sendMu sync.Mutex
	send := func(t ipc.Type, payload []byte) {
		sendMu.Lock()
		defer sendMu.Unlock()
		_ = ipc.WriteFrame(c, t, payload)
	}

	// Ctrl+C and Ctrl+Break are keys for the pane, never the client's end; closing the console,
	// logging off or shutting down end the client cleanly, the console restored first.
	ended := make(chan string, 1)
	stopCtrl := onConsoleControl(func(event uint32) bool {
		switch event {
		case windows.CTRL_C_EVENT, windows.CTRL_BREAK_EVENT:
			return true
		}
		restoreOnce()
		select {
		case ended <- "lost tty":
		default:
		}
		_ = c.Close()
		return true
	})
	defer stopCtrl()

	sendSize := func() {
		if size, err := con.size(); err == nil {
			if f, err := ipc.Encode(ipc.TypeResize, &size); err == nil {
				send(f.Type, f.Payload)
			}
		}
	}
	go con.readInput(func(b []byte) { send(ipc.TypeInput, b) }, sendSize)

	reason, code := "server exited unexpectedly", 1
	for {
		f, err := r.Read()
		if err != nil {
			select {
			case reason = <-ended:
			default:
			}
			break
		}
		if f.Type == ipc.TypeOutput {
			if _, err := stdout.Write(f.Payload); err != nil {
				reason, code = "lost tty", 1
				break
			}
			continue
		}
		if f.Type == ipc.TypeDetach || f.Type == ipc.TypeExit {
			var end ipc.End
			_ = f.Decode(&end)
			reason, code = end.Reason, end.Code
			break
		}
	}
	restoreOnce()
	if reason != "" {
		fmt.Fprintf(stdout, "[%s]\r\n", reason)
	}
	return code, nil
}

// inputRecord is INPUT_RECORD: the event type, then the event (its largest, KEY_EVENT_RECORD,
// is 16 bytes).
type inputRecord struct {
	eventType uint16
	_         uint16
	event     [16]byte
}

// keyEvent is KEY_EVENT_RECORD.
type keyEvent struct {
	keyDown     int32
	repeatCount uint16
	virtualKey  uint16
	scanCode    uint16
	char        uint16
	state       uint32
}

var (
	kernel32                  = windows.NewLazySystemDLL("kernel32.dll")
	procReadConsoleInputW     = kernel32.NewProc("ReadConsoleInputW")
	procSetConsoleCtrlHandler = kernel32.NewProc("SetConsoleCtrlHandler")
)

// readInput reads the console's input records until it fails: in VT input mode keys come as
// the characters of their sequences, which go to the server as UTF-8; a buffer size event
// sends the window's size.
func (c *console) readInput(input func([]byte), resized func()) {
	recs := make([]inputRecord, 128)
	var pending []uint16 // a high surrogate waiting for its low half
	for {
		var n uint32
		r, _, _ := procReadConsoleInputW.Call(uintptr(c.in), uintptr(unsafe.Pointer(&recs[0])), uintptr(len(recs)), uintptr(unsafe.Pointer(&n)))
		if r == 0 {
			return
		}
		var out []byte
		size := false
		for _, rec := range recs[:n] {
			switch rec.eventType {
			case windows.KEY_EVENT:
				k := (*keyEvent)(unsafe.Pointer(&rec.event[0]))
				if k.keyDown == 0 || k.char == 0 {
					continue
				}
				for range max(k.repeatCount, 1) {
					pending = append(pending, k.char)
				}
				out, pending = appendUTF16(out, pending)
			case windows.WINDOW_BUFFER_SIZE_EVENT:
				size = true
			}
		}
		if len(out) > 0 {
			input(out)
		}
		if size {
			resized()
		}
	}
}

// appendUTF16 appends the UTF-8 of units to b, keeping a trailing high surrogate for the next
// record.
func appendUTF16(b []byte, units []uint16) ([]byte, []uint16) {
	end := len(units)
	if end > 0 && utf16.IsSurrogate(rune(units[end-1])) && units[end-1] < 0xdc00 {
		end--
	}
	for _, r := range utf16.Decode(units[:end]) {
		b = utf8.AppendRune(b, r)
	}
	return b, append(units[:0], units[end:]...)
}

// onConsoleControl installs a console control handler; handle reports whether it took the
// event. The returned function removes it.
func onConsoleControl(handle func(event uint32) bool) func() {
	cb := windows.NewCallback(func(event uint32) uintptr {
		if handle(event) {
			return 1
		}
		return 0
	})
	_, _, _ = procSetConsoleCtrlHandler.Call(cb, 1)
	return func() { _, _, _ = procSetConsoleCtrlHandler.Call(cb, 0) }
}
