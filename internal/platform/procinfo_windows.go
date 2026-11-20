//go:build windows

package platform

import (
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Process information on Windows (TASKS.md C8.6). A pane's program and what it starts are in
// its job object; the pane's "descriptor" (pty.Proc's Fd) is that job's handle.

// CwdFromScreen is true: another process's working directory cannot be read reliably on
// Windows, so #{pane_current_path} is the last OSC 7 or OSC 9;9 path, else where the pane
// started (COMPATIBILITY.md K1).
const CwdFromScreen = true

// maxJobProcesses bounds the process list read from a job.
const maxJobProcesses = 256

// ForegroundCommand returns the image name, without .exe, of the newest live process in the
// job whose handle is job (a pane's program, or what it runs now), or "".
func ForegroundCommand(job int) string {
	if job <= 0 {
		return ""
	}
	pids := jobProcesses(windows.Handle(job))
	newest, name := int64(-1), ""
	for _, pid := range pids {
		created, image, ok := processInfo(pid)
		if ok && created > newest {
			newest, name = created, image
		}
	}
	return name
}

// CurrentPath is not read from the process on Windows (CwdFromScreen).
func CurrentPath(int) string { return "" }

// jobProcesses lists the processes in a job (JOBOBJECT_BASIC_PROCESS_ID_LIST).
func jobProcesses(job windows.Handle) []uint32 {
	type list struct {
		assigned, listed uint32
		ids              [maxJobProcesses]uintptr
	}
	var l list
	if err := windows.QueryInformationJobObject(job, windows.JobObjectBasicProcessIdList,
		uintptr(unsafe.Pointer(&l)), uint32(unsafe.Sizeof(l)), nil); err != nil && err != windows.ERROR_MORE_DATA {
		return nil
	}
	n := min(int(l.listed), maxJobProcesses)
	pids := make([]uint32, 0, n)
	for _, id := range l.ids[:n] {
		pids = append(pids, uint32(id))
	}
	return pids
}

// processInfo is a live process's creation time and image name without .exe.
func processInfo(pid uint32) (created int64, name string, ok bool) {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return 0, "", false
	}
	defer func() { _ = windows.CloseHandle(h) }()
	var code uint32
	if windows.GetExitCodeProcess(h, &code) != nil || code != 259 { // STILL_ACTIVE
		return 0, "", false
	}
	var c, e, k, u windows.Filetime
	if windows.GetProcessTimes(h, &c, &e, &k, &u) != nil {
		return 0, "", false
	}
	buf := make([]uint16, windows.MAX_LONG_PATH)
	size := uint32(len(buf))
	if windows.QueryFullProcessImageName(h, 0, &buf[0], &size) != nil {
		return 0, "", false
	}
	base := filepath.Base(windows.UTF16ToString(buf[:size]))
	if strings.HasSuffix(strings.ToLower(base), ".exe") {
		base = base[:len(base)-4]
	}
	return c.Nanoseconds(), base, base != ""
}
