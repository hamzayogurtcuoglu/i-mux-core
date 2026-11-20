//go:build windows

package pty

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// output collects what a program writes until its output ends.
type output struct {
	mu    sync.Mutex
	buf   bytes.Buffer
	ended chan struct{}
}

func collect(p Proc) *output {
	o := &output{ended: make(chan struct{})}
	go func() {
		defer close(o.ended)
		b := make([]byte, 4096)
		for {
			n, err := p.Read(b)
			o.mu.Lock()
			o.buf.Write(b[:n])
			o.mu.Unlock()
			if err != nil {
				return
			}
		}
	}()
	return o
}

var ansi = regexp.MustCompile("\x1b(\\[[0-9;?]*[ -/]*[@-~]|\\][^\x07\x1b]*(\x07|\x1b\\\\)|.)")

// text is the output without escape sequences and line ends.
func (o *output) text() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return strings.NewReplacer("\r", "", "\n", "").Replace(ansi.ReplaceAllString(o.buf.String(), ""))
}

func (o *output) wait(t *testing.T, want string) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for !strings.Contains(o.text(), want) {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %q; the output is %q", want, o.text())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func start(t *testing.T, argv ...string) Proc {
	t.Helper()
	p, err := New().Start(Spec{Argv: argv, Env: os.Environ(), Dir: os.TempDir(), Cols: 80, Rows: 24})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = p.Kill()
		_, _ = p.Wait()
		_ = p.Close()
	})
	return p
}

func powershell(t *testing.T, script string) Proc {
	t.Helper()
	return start(t, "powershell.exe", "-NoLogo", "-NoProfile", "-NonInteractive", "-Command", script)
}

func TestConPTYEcho(t *testing.T) {
	p := start(t, "cmd.exe", "/d", "/c", "echo hello from cmd")
	o := collect(p)
	o.wait(t, "hello from cmd")
	if st, err := p.Wait(); err != nil || st.Code != 0 {
		t.Fatalf("status %+v, %v", st, err)
	}
	select {
	case <-o.ended:
	case <-time.After(10 * time.Second):
		t.Fatal("the output did not end after the program did")
	}
}

func TestConPTYInput(t *testing.T) {
	p := powershell(t, `$l = [Console]::ReadLine(); Write-Output ("got:" + $l)`)
	o := collect(p)
	time.Sleep(500 * time.Millisecond) // powershell starts before it reads
	if _, err := p.Write([]byte("hello\r")); err != nil {
		t.Fatal(err)
	}
	o.wait(t, "got:hello")
}

func TestConPTYExitCode(t *testing.T) {
	p := start(t, "cmd.exe", "/d", "/c", "exit 3")
	collect(p)
	if st, err := p.Wait(); err != nil || st.Code != 3 {
		t.Fatalf("status %+v, %v; want code 3", st, err)
	}
}

func TestConPTYBreakawayChild(t *testing.T) {
	mode := os.Getenv("IMUX_TEST_BREAKAWAY")
	marker := os.Getenv("IMUX_TEST_BREAKAWAY_MARKER")
	switch mode {
	case "writer":
		time.Sleep(time.Second)
		if err := os.WriteFile(marker, []byte("alive"), 0o600); err != nil {
			t.Fatal(err)
		}
		return
	case "starter":
		self, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(self, "-test.run=^TestConPTYBreakawayChild$")
		cmd.Env = append(os.Environ(), "IMUX_TEST_BREAKAWAY=writer")
		cmd.SysProcAttr = &syscall.SysProcAttr{
			CreationFlags: windows.CREATE_NEW_PROCESS_GROUP | windows.CREATE_NO_WINDOW | windows.CREATE_BREAKAWAY_FROM_JOB,
			HideWindow:    true,
		}
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		_ = cmd.Process.Release()
		return
	case "":
		// The outer test.
	default:
		t.Fatalf("bad mode %q", mode)
	}
	marker = filepath.Join(t.TempDir(), "alive")
	t.Setenv("IMUX_TEST_BREAKAWAY", "starter")
	t.Setenv("IMUX_TEST_BREAKAWAY_MARKER", marker)
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	p := start(t, self, "-test.run=^TestConPTYBreakawayChild$")
	collect(p)
	if _, err := p.Wait(); err != nil {
		t.Fatal(err)
	}
	_ = p.Close() // closes the pane job, killing non-breakaway children
	deadline := time.Now().Add(10 * time.Second)
	for {
		if b, err := os.ReadFile(marker); err == nil && string(b) == "alive" {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("the breakaway child died with the pane job")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestConPTYResize(t *testing.T) {
	p := powershell(t, `Write-Output ("size:" + [Console]::WindowWidth + "x" + [Console]::WindowHeight); `+
		`$null = [Console]::ReadLine(); Write-Output ("size:" + [Console]::WindowWidth + "x" + [Console]::WindowHeight)`)
	o := collect(p)
	o.wait(t, "size:80x24")
	if err := p.Resize(100, 30, 0, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Write([]byte("\r")); err != nil {
		t.Fatal(err)
	}
	o.wait(t, "size:100x30")
}

// Kill ends what the program started too (the job object).
func TestConPTYKillTree(t *testing.T) {
	p := powershell(t, `$c = Start-Process -NoNewWindow -PassThru ping.exe -ArgumentList "-n","300","127.0.0.1"; `+
		`Write-Output ("child:" + $c.Id); Start-Sleep 300`)
	o := collect(p)
	o.wait(t, "child:")
	m := regexp.MustCompile(`child:(\d+)`).FindStringSubmatch(o.text())
	if m == nil {
		t.Fatalf("no child pid in %q", o.text())
	}
	pid, _ := strconv.Atoi(m[1])
	h, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		t.Fatalf("the grandchild %d: %v", pid, err)
	}
	defer func() { _ = windows.CloseHandle(h) }()
	if err := p.Kill(); err != nil {
		t.Fatal(err)
	}
	if ev, err := windows.WaitForSingleObject(h, 10000); err != nil || ev != windows.WAIT_OBJECT_0 {
		t.Fatalf("the grandchild still runs after Kill (%d, %v)", ev, err)
	}
	if _, err := p.Wait(); err != nil {
		t.Fatal(err)
	}
}

// Starting and closing programs leaves no handles behind: a leak would grow by at least one
// handle a program (the process, its job, the pipes, the pseudo console). The count still moves
// in small steps as Go's runtime starts threads for blocking reads, which stay.
func TestConPTYHandles(t *testing.T) {
	getCount := windows.NewLazySystemDLL("kernel32.dll").NewProc("GetProcessHandleCount")
	count := func() uint32 {
		var n uint32
		if r, _, err := getCount.Call(uintptr(windows.CurrentProcess()), uintptr(unsafe.Pointer(&n))); r == 0 {
			t.Fatal(err)
		}
		return n
	}
	run := func() {
		p, err := New().Start(Spec{Argv: []string{"cmd.exe", "/d", "/c", "echo x"}, Cols: 80, Rows: 24})
		if err != nil {
			t.Fatal(err)
		}
		o := collect(p)
		if _, err := p.Wait(); err != nil {
			t.Fatal(err)
		}
		<-o.ended
		_ = p.Close()
	}
	run() // the first run loads what stays loaded
	const programs = 20
	before := count()
	for range programs {
		run()
	}
	time.Sleep(200 * time.Millisecond)
	if after := count(); after >= before+programs {
		t.Fatalf("handles: %d before, %d after %d programs", before, after, programs)
	}
}

// The fake TUI (testdata/faketui) runs: its frame is drawn, and it quits on q.
func TestConPTYFakeTUI(t *testing.T) {
	bin := os.Getenv("IMUX_FAKETUI")
	if bin == "" {
		if _, err := exec.LookPath("go"); err != nil {
			t.Skip("no Go to build the fake TUI, and IMUX_FAKETUI is not set")
		}
		bin = filepath.Join(t.TempDir(), "faketui.exe")
		if out, err := exec.Command("go", "build", "-o", bin, "../../testdata/faketui").CombinedOutput(); err != nil {
			t.Fatalf("go build: %v\n%s", err, out)
		}
	}
	p := start(t, bin, "-mode", "frame")
	o := collect(p)
	o.wait(t, "faketui")
	o.wait(t, fmt.Sprintf("size %dx%d", 80, 24))
	if _, err := p.Write([]byte("q")); err != nil {
		t.Fatal(err)
	}
	if st, err := p.Wait(); err != nil || st.Code != 0 {
		t.Fatalf("status %+v, %v", st, err)
	}
}
