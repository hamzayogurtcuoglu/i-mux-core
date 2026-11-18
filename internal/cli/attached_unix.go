//go:build darwin || linux

package cli

import (
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/ipc"
)

// runAttached is the attached client (TASKS.md C2.7): the terminal in raw mode, the server's
// output written to it, its input and size sent back, until the server says the client is done
// or goes away. The terminal is restored on every way out, and the reason printed as tmux
// prints it ("[detached (from session a)]").
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

	// Signals that end the client end it cleanly: the terminal is restored first.
	ended := make(chan string, 1)
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(sigs)
	go func() {
		if sig, ok := <-sigs; ok {
			if sig == syscall.SIGHUP {
				ended <- "lost tty"
			} else {
				ended <- "terminated"
			}
			_ = c.Close()
		}
	}()

	winch, stopWinch := con.resizes()
	defer stopWinch()
	go func() {
		for range winch {
			if size, err := con.size(); err == nil {
				if f, err := ipc.Encode(ipc.TypeResize, &size); err == nil {
					send(f.Type, f.Payload)
				}
			}
		}
	}()

	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := con.in.Read(buf)
			if n > 0 {
				send(ipc.TypeInput, append([]byte(nil), buf[:n]...))
			}
			if err != nil {
				return
			}
		}
	}()

	reason, code := "server exited unexpectedly", 1
	for {
		f, err := r.Read()
		if err != nil {
			select {
			case reason = <-ended:
				code = 1
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
		fmt.Fprintf(stdout, "[%s]\n", reason)
	}
	return code, nil
}
