//go:build darwin || linux

package vtfixture

import (
	"fmt"
	"os"
	"time"

	"golang.org/x/sys/unix"
)

// Helper runs inside the tmux pane: args are the fixture and a marker file. It puts its
// terminal in raw mode (so no byte is changed on the way, LF included), writes the fixture,
// then asks for the cursor position: tmux answers only once it has parsed everything before
// the question, so when the answer arrives the screen is complete. It then creates the marker
// and waits to be killed.
func Helper(args []string) int {
	if len(args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: __write <fixture> <marker>")
		return 2
	}
	data, err := os.ReadFile(args[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	t, err := unix.IoctlGetTermios(0, ioctlGetTermios)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	t.Oflag &^= unix.OPOST
	t.Lflag &^= unix.ECHO | unix.ICANON | unix.ISIG | unix.IEXTEN
	t.Iflag &^= unix.ICRNL | unix.IXON
	t.Cc[unix.VMIN], t.Cc[unix.VTIME] = 1, 0
	if err := unix.IoctlSetTermios(0, ioctlSetTermios, t); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if _, err := os.Stdout.Write(data); err != nil {
		return 1
	}
	if _, err := os.Stdout.Write([]byte("\x1b[6n")); err != nil {
		return 1
	}
	b := make([]byte, 1)
	for {
		if _, err := os.Stdin.Read(b); err != nil {
			return 1
		}
		if b[0] == 'R' {
			break
		}
	}
	if err := os.WriteFile(args[1], nil, 0o600); err != nil {
		return 1
	}
	for { // until killed; select{} would be taken for a deadlock and end the pane
		time.Sleep(time.Hour)
	}
}
