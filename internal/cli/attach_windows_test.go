//go:build windows

package cli_test

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/sys/windows"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/pty"
)

// The attached client on Windows (TASKS.md C8.5), run in a pseudo console the test owns, as a
// terminal runs it: its first frame, keys to the pane (Ctrl+C one of them), a resize, detach,
// and the console's modes and code pages as they were before.

// consoleModesHelper, run inside the pseudo console, prints the console's modes and code pages.
func TestMain_consoleModes(t *testing.T) {
	if os.Getenv("IMUX_TEST_CONSOLE_MODES") == "" {
		t.Skip("a helper for TestWindowsAttach")
	}
	var in, out uint32
	_ = windows.GetConsoleMode(windows.Handle(os.Stdin.Fd()), &in)
	_ = windows.GetConsoleMode(windows.Handle(os.Stdout.Fd()), &out)
	icp, _ := windows.GetConsoleCP()
	ocp, _ := windows.GetConsoleOutputCP()
	fmt.Printf("MODES in=%#x out=%#x cp=%d/%d END\n", in, out, icp, ocp)
}

type console struct {
	p   pty.Proc
	mu  sync.Mutex
	out strings.Builder
}

var ansiRe = regexp.MustCompile("\x1b(\\[[0-9;?<>=]*[ -/]*[@-~]|\\][^\x07\x1b]*(\x07|\x1b\\\\)|[()][0-9A-Za-z]|.)")

func (c *console) text() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return strings.NewReplacer("\r", "", "\n", "").Replace(ansiRe.ReplaceAllString(c.out.String(), ""))
}

func (c *console) wait(t *testing.T, want string) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for !strings.Contains(c.text(), want) {
		if time.Now().After(deadline) {
			t.Fatalf("the console does not show %q; it shows %q", want, c.text())
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestWindowsAttach(t *testing.T) {
	w := newWorld(t)
	w.must("new-session", "-d", "-s", "a", "-x", "100", "-y", "29", "powershell.exe", "-NoLogo", "-NoProfile", "-Command",
		`Write-Output 'hello from the pane'; while ($true) { $l = [Console]::ReadLine(); Write-Output ('typed:' + $l) }`)
	exe, _ := os.Executable()
	// The client between two runs of the helper, in one pseudo console.
	line := fmt.Sprintf(`"%s" -test.run TestMain_consoleModes & "%s" -S "%s" -f NUL attach-session -t a & "%s" -test.run TestMain_consoleModes`,
		exe, bin, w.socket, exe)
	env := append(append([]string(nil), w.env...), "IMUX_TEST_CONSOLE_MODES=1", "IMUX_CORE_BIN="+bin)
	p, err := pty.New().Start(pty.Spec{
		Path: os.Getenv("COMSPEC"), Argv: []string{"cmd.exe"}, CmdLine: `cmd.exe /d /s /c "` + line + `"`,
		Env: env, Dir: w.dir, Cols: 100, Rows: 30,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Kill(); _, _ = p.Wait(); _ = p.Close() })
	c := &console{p: p}
	go func() {
		b := make([]byte, 32<<10)
		for {
			n, err := p.Read(b)
			c.mu.Lock()
			c.out.Write(b[:n])
			c.mu.Unlock()
			if err != nil {
				return
			}
		}
	}()
	c.wait(t, "hello from the pane")
	c.wait(t, "[a] 0:powershell")
	before := regexp.MustCompile(`MODES [^E]*END`).FindString(c.text())
	if before == "" {
		t.Fatalf("no modes before the client: %q", c.text())
	}

	// Keys reach the pane; Ctrl+C is one of them and leaves the client attached.
	if _, err := p.Write([]byte("abc\r")); err != nil {
		t.Fatal(err)
	}
	c.wait(t, "typed:abc")
	if _, err := p.Write([]byte("\x03")); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	if got := strings.TrimSpace(w.must("list-clients", "-F", "#{client_name} #{client_utf8}")); !strings.HasPrefix(got, "console-") || !strings.HasSuffix(got, " 1") {
		t.Fatalf("after Ctrl+C: clients %q", got)
	}

	// A resize reaches the server.
	if err := p.Resize(120, 40, 0, 0); err != nil {
		t.Fatal(err)
	}
	w.eventually("the client's new size", func() bool {
		return strings.TrimSpace(w.must("list-clients", "-F", "#{client_width}x#{client_height}")) == "120x40"
	})

	// Detach: the client says so and ends; the console's modes are as they were.
	w.must("detach-client")
	c.wait(t, "[detached (from session a)]")
	// The modes the helper prints after the client: what follows the detach message (ConPTY may
	// send the screen from before the client again, its first MODES line with it).
	modes := regexp.MustCompile(`MODES [^E]*END`)
	afterDetach := func() string {
		tx := c.text()
		return modes.FindString(tx[strings.LastIndex(tx, "[detached (from session a)]"):])
	}
	w.eventually("the modes after the client", func() bool { return afterDetach() != "" })
	if after := afterDetach(); after != before {
		t.Errorf("the console's modes: %s before, %s after", before, after)
	}
}
