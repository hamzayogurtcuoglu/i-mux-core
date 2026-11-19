//go:build windows

package pty

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// ConPTY starts programs on Windows pseudo consoles (E12): one per pane, created with no flags,
// the program and everything it starts in a job object that ends the whole tree.
type ConPTY struct{}

// New returns this system's PTY implementation.
func New() PTY { return &ConPTY{} }

// closeTimeout bounds ClosePseudoConsole, which on some builds waits until its output is read.
const closeTimeout = 5 * time.Second

type conProc struct {
	hpc  windows.Handle
	in   *os.File // the program's input: written here
	out  *os.File // its output: read here
	proc windows.Handle
	job  windows.Handle
	pid  int

	done    chan struct{} // closed when the program has ended
	status  ExitStatus
	waitErr error

	hpcOnce   sync.Once
	closeOnce sync.Once
}

// Start implements PTY. The program runs on a new cols×rows pseudo console, in a job object
// that kills everything it started when the job closes.
func (*ConPTY) Start(spec Spec) (Proc, error) {
	if len(spec.Argv) == 0 {
		return nil, errors.New("pty: no program")
	}
	path := spec.Path
	if path == "" {
		// Windows' program lookup: PATH and PATHEXT.
		p, err := exec.LookPath(spec.Argv[0])
		if err != nil {
			return nil, err
		}
		path = p
	}
	var inR, inW, outR, outW windows.Handle
	if err := windows.CreatePipe(&inR, &inW, nil, 0); err != nil {
		return nil, fmt.Errorf("CreatePipe: %w", err)
	}
	if err := windows.CreatePipe(&outR, &outW, nil, 0); err != nil {
		_ = windows.CloseHandle(inR)
		_ = windows.CloseHandle(inW)
		return nil, fmt.Errorf("CreatePipe: %w", err)
	}
	var hpc windows.Handle
	err := windows.CreatePseudoConsole(coord(spec.Cols, spec.Rows), inR, outW, 0, &hpc)
	// The pseudo console holds its own references to its ends of the pipes.
	_ = windows.CloseHandle(inR)
	_ = windows.CloseHandle(outW)
	if err != nil {
		_ = windows.CloseHandle(inW)
		_ = windows.CloseHandle(outR)
		return nil, fmt.Errorf("CreatePseudoConsole: %w", err)
	}
	p := &conProc{
		hpc:  hpc,
		in:   os.NewFile(uintptr(inW), "conpty-in"),
		out:  os.NewFile(uintptr(outR), "conpty-out"),
		done: make(chan struct{}),
	}
	if err := p.spawn(path, spec); err != nil {
		p.closePseudoConsole()
		_ = p.in.Close()
		_ = p.out.Close()
		if p.job != 0 {
			_ = windows.CloseHandle(p.job)
		}
		return nil, err
	}
	go p.reap()
	return p, nil
}

func coord(cols, rows int) windows.Coord {
	return windows.Coord{X: int16(max(cols, 1)), Y: int16(max(rows, 1))}
}

func (p *conProc) spawn(path string, spec Spec) error {
	attrs, err := windows.NewProcThreadAttributeList(1)
	if err != nil {
		return fmt.Errorf("NewProcThreadAttributeList: %w", err)
	}
	defer attrs.Delete()
	// The attribute's value is the HPCON itself, not a pointer to it.
	hpc := *(*unsafe.Pointer)(unsafe.Pointer(&p.hpc))
	if err := attrs.Update(windows.PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE, hpc, unsafe.Sizeof(p.hpc)); err != nil {
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
	p.job = job
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		return fmt.Errorf("SetInformationJobObject: %w", err)
	}

	app, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	cmdline, err := windows.UTF16PtrFromString(windows.ComposeCommandLine(spec.Argv))
	if err != nil {
		return err
	}
	var dir *uint16
	if spec.Dir != "" {
		if dir, err = windows.UTF16PtrFromString(spec.Dir); err != nil {
			return err
		}
	}
	var block *uint16
	if spec.Env != nil {
		block = envBlock(spec.Env)
	}
	pi := &windows.ProcessInformation{}
	flags := uint32(windows.EXTENDED_STARTUPINFO_PRESENT | windows.CREATE_UNICODE_ENVIRONMENT | windows.CREATE_SUSPENDED)
	if err := windows.CreateProcess(app, cmdline, nil, nil, false, flags, block, dir, &si.StartupInfo, pi); err != nil {
		return &os.PathError{Op: "CreateProcess", Path: path, Err: err}
	}
	defer func() { _ = windows.CloseHandle(pi.Thread) }()
	p.proc, p.pid = pi.Process, int(pi.ProcessId)
	// Suspended until it is in the job, so nothing it starts escapes the job.
	if err := windows.AssignProcessToJobObject(job, pi.Process); err != nil {
		_ = windows.TerminateProcess(pi.Process, 1)
		_ = windows.CloseHandle(pi.Process)
		return fmt.Errorf("AssignProcessToJobObject: %w", err)
	}
	if _, err := windows.ResumeThread(pi.Thread); err != nil {
		_ = windows.TerminateJobObject(job, 1)
		_ = windows.CloseHandle(pi.Process)
		return fmt.Errorf("ResumeThread: %w", err)
	}
	return nil
}

// envBlock is a Unicode environment block: each "K=V" ends with a NUL, the block with another.
func envBlock(env []string) *uint16 {
	var b []uint16
	for _, kv := range env {
		u, _ := windows.UTF16FromString(kv)
		b = append(b, u...)
	}
	b = append(b, 0)
	return &b[0]
}

// reap waits for the program and records how it ended; then the pseudo console is closed, so
// that reading its output ends once what is left has been read.
func (p *conProc) reap() {
	_, err := windows.WaitForSingleObject(p.proc, windows.INFINITE)
	if err == nil {
		var code uint32
		if err = windows.GetExitCodeProcess(p.proc, &code); err == nil {
			p.status = ExitStatus{Code: int(code)}
		}
	}
	p.waitErr = err
	_ = windows.CloseHandle(p.proc)
	close(p.done)
	go p.closePseudoConsole()
}

// closePseudoConsole closes the pseudo console once. It can wait until its output has been
// read, so a reader must keep reading meanwhile; it is given closeTimeout.
func (p *conProc) closePseudoConsole() {
	p.hpcOnce.Do(func() {
		closed := make(chan struct{})
		go func() {
			windows.ClosePseudoConsole(p.hpc)
			close(closed)
		}()
		select {
		case <-closed:
		case <-time.After(closeTimeout):
		}
	})
}

func (p *conProc) Read(b []byte) (int, error) {
	n, err := p.out.Read(b)
	// The write end closing (the pseudo console closed) is the end of the output.
	if err != nil && (errors.Is(err, windows.ERROR_BROKEN_PIPE) || errors.Is(err, os.ErrClosed)) {
		err = io.EOF
	}
	return n, err
}

func (p *conProc) Write(b []byte) (int, error) { return p.in.Write(b) }

// Resize implements Proc. A pseudo console has no size in pixels.
func (p *conProc) Resize(cols, rows, _, _ int) error {
	return windows.ResizePseudoConsole(p.hpc, coord(cols, rows))
}

func (p *conProc) Wait() (ExitStatus, error) {
	<-p.done
	return p.status, p.waitErr
}

func (p *conProc) Pid() int { return p.pid }

// Fd implements Proc: a pseudo console has no descriptor (process information on Windows comes
// from the job, C8.6).
func (*conProc) Fd() int { return -1 }

// Kill ends the program and everything it started (the job, K4). It does not wait.
func (p *conProc) Kill() error {
	return windows.TerminateJobObject(p.job, 1)
}

// Close releases the pseudo console, the pipes and the job; closing the job ends what is still
// running in it. A Read blocked in another goroutine returns.
func (p *conProc) Close() error {
	p.closeOnce.Do(func() {
		p.closePseudoConsole()
		_ = p.in.Close()
		_ = p.out.Close()
		_ = windows.CloseHandle(p.job)
	})
	return nil
}
