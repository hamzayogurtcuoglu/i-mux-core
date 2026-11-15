package main

import (
	"errors"
	"fmt"
	"os"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Pseudo console flags (consoleapi.h). Builds that do not know one fail CreatePseudoConsole.
const (
	flagInheritCursor   = 0x1
	flagResizeQuirk     = 0x2
	flagWin32InputMode  = 0x4
	flagPassthroughMode = 0x8
)

// Console is a program running on a pseudo console, in a job object.
type Console struct {
	hpc     windows.Handle
	in      *os.File // we write the program's input here
	out     *os.File // we read the program's output here
	proc    windows.Handle
	job     windows.Handle
	pid     uint32
	mu      sync.Mutex
	buf     []byte
	readErr error
	done    chan struct{}
}

// StartConsole runs cmdline on a new cols×rows pseudo console created with flags, in a job
// object that kills the whole tree when it closes.
func StartConsole(cmdline string, cols, rows int16, flags uint32, env []string) (*Console, error) {
	var inR, inW, outR, outW windows.Handle
	if err := windows.CreatePipe(&inR, &inW, nil, 0); err != nil {
		return nil, fmt.Errorf("CreatePipe: %w", err)
	}
	if err := windows.CreatePipe(&outR, &outW, nil, 0); err != nil {
		windows.CloseHandle(inR)
		windows.CloseHandle(inW)
		return nil, fmt.Errorf("CreatePipe: %w", err)
	}
	var hpc windows.Handle
	err := windows.CreatePseudoConsole(windows.Coord{X: cols, Y: rows}, inR, outW, flags, &hpc)
	// The pseudo console holds its own references to its ends of the pipes.
	windows.CloseHandle(inR)
	windows.CloseHandle(outW)
	if err != nil {
		windows.CloseHandle(inW)
		windows.CloseHandle(outR)
		return nil, fmt.Errorf("CreatePseudoConsole(flags %#x): %w", flags, err)
	}
	c := &Console{
		hpc:  hpc,
		in:   os.NewFile(uintptr(inW), "conpty-in"),
		out:  os.NewFile(uintptr(outR), "conpty-out"),
		done: make(chan struct{}),
	}
	go c.readLoop()
	if err := c.spawn(cmdline, env); err != nil {
		c.Close()
		return nil, err
	}
	return c, nil
}

func (c *Console) spawn(cmdline string, env []string) error {
	attrs, err := windows.NewProcThreadAttributeList(1)
	if err != nil {
		return fmt.Errorf("NewProcThreadAttributeList: %w", err)
	}
	defer attrs.Delete()
	// The attribute's value is the HPCON itself, not a pointer to it.
	hpc := *(*unsafe.Pointer)(unsafe.Pointer(&c.hpc))
	if err := attrs.Update(windows.PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE, hpc, unsafe.Sizeof(c.hpc)); err != nil {
		return fmt.Errorf("attribute PSEUDOCONSOLE: %w", err)
	}
	si := &windows.StartupInfoEx{}
	si.Cb = uint32(unsafe.Sizeof(*si))
	// Without STARTF_USESTDHANDLES a child may use the parent's console handles instead of the
	// pseudo console's (a known ConPTY pitfall): give it none.
	si.Flags = windows.STARTF_USESTDHANDLES
	si.ProcThreadAttributeList = attrs.List()

	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return fmt.Errorf("CreateJobObject: %w", err)
	}
	c.job = job
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		return fmt.Errorf("SetInformationJobObject: %w", err)
	}

	var block *uint16
	if env != nil {
		block = envBlock(env)
	}
	cmd, err := windows.UTF16PtrFromString(cmdline)
	if err != nil {
		return err
	}
	pi := &windows.ProcessInformation{}
	flags := uint32(windows.EXTENDED_STARTUPINFO_PRESENT | windows.CREATE_UNICODE_ENVIRONMENT | windows.CREATE_SUSPENDED)
	if err := windows.CreateProcess(nil, cmd, nil, nil, false, flags, block, nil, &si.StartupInfo, pi); err != nil {
		return fmt.Errorf("CreateProcess %q: %w", cmdline, err)
	}
	defer windows.CloseHandle(pi.Thread)
	c.proc, c.pid = pi.Process, pi.ProcessId
	if err := windows.AssignProcessToJobObject(job, pi.Process); err != nil {
		windows.TerminateProcess(pi.Process, 1)
		return fmt.Errorf("AssignProcessToJobObject: %w", err)
	}
	if _, err := windows.ResumeThread(pi.Thread); err != nil {
		return fmt.Errorf("ResumeThread: %w", err)
	}
	return nil
}

func envBlock(env []string) *uint16 {
	var b []uint16
	for _, kv := range env {
		u, _ := windows.UTF16FromString(kv)
		b = append(b, u...)
	}
	b = append(b, 0)
	return &b[0]
}

func (c *Console) readLoop() {
	defer close(c.done)
	buf := make([]byte, 32<<10)
	for {
		n, err := c.out.Read(buf)
		c.mu.Lock()
		c.buf = append(c.buf, buf[:n]...)
		if err != nil {
			c.readErr = err
			c.mu.Unlock()
			return
		}
		c.mu.Unlock()
	}
}

// Output returns everything read so far.
func (c *Console) Output() []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]byte(nil), c.buf...)
}

// WaitOutput waits until the output contains s.
func (c *Console) WaitOutput(s string, timeout time.Duration) ([]byte, error) {
	deadline := time.Now().Add(timeout)
	delay := time.Millisecond
	for {
		out := c.Output()
		if containsBytes(out, []byte(s)) {
			return out, nil
		}
		if time.Now().After(deadline) {
			return out, fmt.Errorf("timed out waiting for %q", s)
		}
		time.Sleep(delay)
		delay = min(delay*2, 50*time.Millisecond)
	}
}

func containsBytes(b, s []byte) bool {
	for i := 0; i+len(s) <= len(b); i++ {
		if string(b[i:i+len(s)]) == string(s) {
			return true
		}
	}
	return false
}

// Write sends input to the program through the pseudo console.
func (c *Console) Write(b []byte) error {
	_, err := c.in.Write(b)
	return err
}

// Resize changes the pseudo console's size.
func (c *Console) Resize(cols, rows int16) error {
	return windows.ResizePseudoConsole(c.hpc, windows.Coord{X: cols, Y: rows})
}

// Wait waits for the program to exit and returns its exit code.
func (c *Console) Wait(timeout time.Duration) (uint32, error) {
	ev, err := windows.WaitForSingleObject(c.proc, uint32(timeout.Milliseconds()))
	if err != nil {
		return 0, err
	}
	if ev != windows.WAIT_OBJECT_0 {
		return 0, errors.New("timed out")
	}
	var code uint32
	err = windows.GetExitCodeProcess(c.proc, &code)
	return code, err
}

// KillTree ends every process in the job.
func (c *Console) KillTree() error {
	return windows.TerminateJobObject(c.job, 1)
}

// Close kills the tree and closes the pseudo console. ClosePseudoConsole can block until its
// output is read, so the read loop keeps running while it closes.
func (c *Console) Close() {
	if c.job != 0 {
		windows.TerminateJobObject(c.job, 1)
	}
	closed := make(chan struct{})
	go func() {
		windows.ClosePseudoConsole(c.hpc)
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
	}
	c.in.Close()
	c.out.Close()
	if c.proc != 0 {
		windows.CloseHandle(c.proc)
	}
	if c.job != 0 {
		windows.CloseHandle(c.job)
	}
}

// windowsBuild returns the OS version, from RtlGetVersion (not affected by manifests).
func windowsBuild() string {
	v := windows.RtlGetVersion()
	return fmt.Sprintf("%d.%d.%d", v.MajorVersion, v.MinorVersion, v.BuildNumber)
}
