package platform

import (
	"syscall"
	"unsafe"
)

// libSystem functions, called without cgo the way golang.org/x/sys/unix calls them: an
// assembly trampoline per function (libsystem_darwin_*.s) and the runtime's syscall6 (E11).

//go:cgo_import_dynamic libc_proc_pidinfo proc_pidinfo "/usr/lib/libSystem.B.dylib"
//go:cgo_import_dynamic libc_grantpt grantpt "/usr/lib/libSystem.B.dylib"
//go:cgo_import_dynamic libc_unlockpt unlockpt "/usr/lib/libSystem.B.dylib"
//go:cgo_import_dynamic libc_ptsname_r ptsname_r "/usr/lib/libSystem.B.dylib"

var (
	libc_proc_pidinfo_trampoline_addr uintptr
	libc_grantpt_trampoline_addr      uintptr
	libc_unlockpt_trampoline_addr     uintptr
	libc_ptsname_r_trampoline_addr    uintptr
)

//go:linkname syscall_syscall6 syscall.syscall6
func syscall_syscall6(fn, a1, a2, a3, a4, a5, a6 uintptr) (r1, r2 uintptr, err syscall.Errno)

// proc_pidinfo flavors and sizes, from <sys/proc_info.h>.
const (
	procPidVnodePathInfo     = 9
	procPidVnodePathInfoSize = 2352 // two vnode_info_path: vnode_info (152 bytes), path[1024]
	procPidTShortBSDInfo     = 13
	procPidTShortBSDInfoSize = 64 // pbsi_comm[16] at offset 16
)

func procPidinfo(pid, flavor int, buf []byte) int {
	r1, _, _ := syscall_syscall6(libc_proc_pidinfo_trampoline_addr, uintptr(pid), uintptr(flavor), 0,
		uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), 0)
	return int(int32(r1))
}

func cString(b []byte) string {
	for i, c := range b {
		if c == 0 {
			return string(b[:i])
		}
	}
	return string(b)
}

// processName returns pbsi_comm, as tmux's osdep-darwin.c does.
func processName(pid int) string {
	buf := make([]byte, procPidTShortBSDInfoSize)
	if procPidinfo(pid, procPidTShortBSDInfo, buf) != len(buf) {
		return ""
	}
	return cString(buf[16:32])
}

// processCwd returns pvi_cdir's path, as tmux's osdep-darwin.c does.
func processCwd(pid int, _ int) string {
	buf := make([]byte, procPidVnodePathInfoSize)
	if procPidinfo(pid, procPidVnodePathInfo, buf) != len(buf) {
		return ""
	}
	return cString(buf[152 : 152+1024])
}

// Grantpt, Unlockpt and Ptsname prepare a PTY master opened from /dev/ptmx.
func Grantpt(fd int) error {
	r1, _, e := syscall_syscall6(libc_grantpt_trampoline_addr, uintptr(fd), 0, 0, 0, 0, 0)
	if int32(r1) != 0 {
		return e
	}
	return nil
}

// Unlockpt unlocks a PTY master's slave.
func Unlockpt(fd int) error {
	r1, _, e := syscall_syscall6(libc_unlockpt_trampoline_addr, uintptr(fd), 0, 0, 0, 0, 0)
	if int32(r1) != 0 {
		return e
	}
	return nil
}

// Ptsname returns the path of a PTY master's slave.
func Ptsname(fd int) (string, error) {
	buf := make([]byte, 128)
	r1, _, e := syscall_syscall6(libc_ptsname_r_trampoline_addr, uintptr(fd), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), 0, 0, 0)
	if int32(r1) != 0 {
		return "", e
	}
	return cString(buf), nil
}
