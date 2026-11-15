//go:build darwin || linux

package vtfixture

import (
	"fmt"
	"os"
	"strings"
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
	if err := rawMode(); err != nil {
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

// ProbeHelper runs inside the tmux pane: args are the probe file, the reply file to write and a
// marker. Each query is followed by the barrier; the answer is what arrives before the
// barrier's reply.
func ProbeHelper(args []string) int {
	if len(args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: __probe <probe> <out> <marker>")
		return 2
	}
	entries, err := ReadEntries(args[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := rawMode(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	var replies []Entry
	buf := make([]byte, 4096)
	for _, e := range entries {
		if _, err := os.Stdout.Write([]byte(e.Query + Barrier)); err != nil {
			return 1
		}
		var got []byte
		for !strings.HasSuffix(string(got), BarrierReply) {
			n, err := os.Stdin.Read(buf)
			if err != nil {
				return 1
			}
			got = append(got, buf[:n]...)
		}
		replies = append(replies, Entry{Name: e.Name, Query: strings.TrimSuffix(string(got), BarrierReply)})
	}
	if err := os.WriteFile(args[1], []byte(FormatReplies(replies)), 0o600); err != nil {
		return 1
	}
	if err := os.WriteFile(args[2], nil, 0o600); err != nil {
		return 1
	}
	for {
		time.Sleep(time.Hour)
	}
}

// rawMode makes the terminal pass bytes unchanged both ways.
func rawMode() error {
	t, err := unix.IoctlGetTermios(0, ioctlGetTermios)
	if err != nil {
		return err
	}
	t.Oflag &^= unix.OPOST
	t.Lflag &^= unix.ECHO | unix.ICANON | unix.ISIG | unix.IEXTEN
	t.Iflag &^= unix.ICRNL | unix.IXON
	t.Cc[unix.VMIN], t.Cc[unix.VTIME] = 1, 0
	return unix.IoctlSetTermios(0, ioctlSetTermios, t)
}
