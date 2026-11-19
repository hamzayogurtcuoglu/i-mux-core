//go:build windows

package ipc

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// The endpoint and peer checks on Windows (ARCHITECTURE.md §7, E13): a named pipe that only the
// user and SYSTEM may open, local clients only, and every client's token user checked.

// pipeBuffer is each pipe instance's buffer size, in and out.
const pipeBuffer = 64 << 10

// accessDACL is the protected DACL of the directory and the pipe: full access for the user and
// SYSTEM, nothing inherited.
func accessDACL(user *windows.SID) string {
	return "D:P(A;;GA;;;" + user.String() + ")(A;;GA;;;SY)"
}

// CurrentUser is this process's token user.
func CurrentUser() (*windows.SID, error) {
	u, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, err
	}
	return u.User.Sid.Copy()
}

func securityAttributes(user *windows.SID) (*windows.SecurityAttributes, error) {
	sd, err := windows.SecurityDescriptorFromString(accessDACL(user))
	if err != nil {
		return nil, err
	}
	return &windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: sd}, nil
}

// SocketDir returns the socket directory, creating it with the protected DACL if needed:
// %LOCALAPPDATA%\i-mux-core, or $TMUX_TMPDIR\i-mux-core when tmpdir is set. An existing
// directory must be the user's with no other access, as on Unix.
func SocketDir(tmpdir string, _ int) (string, error) {
	base := tmpdir
	if base == "" {
		base = os.Getenv("LOCALAPPDATA")
	}
	if base == "" {
		return "", errors.New("no suitable socket path")
	}
	dir := filepath.Join(base, "i-mux-core")
	user, err := CurrentUser()
	if err != nil {
		return "", err
	}
	sa, err := securityAttributes(user)
	if err != nil {
		return "", err
	}
	p, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return "", err
	}
	if err := windows.CreateDirectory(p, sa); err != nil && !errors.Is(err, windows.ERROR_ALREADY_EXISTS) {
		return "", fmt.Errorf("couldn't create directory %s (%v)", dir, err)
	}
	if err := checkDir(dir, user); err != nil {
		return "", err
	}
	return dir, nil
}

// checkDir is the directory check: a directory, owned by user, whose DACL lets in no one but
// user and SYSTEM.
func checkDir(dir string, user *windows.SID) error {
	fi, err := os.Lstat(dir)
	if err != nil {
		return fmt.Errorf("couldn't read directory %s (%v)", dir, err)
	}
	if !fi.IsDir() || fi.Mode()&fs.ModeSymlink != 0 {
		return fmt.Errorf("%s is not a directory", dir)
	}
	ok, err := OnlyUser(dir, user)
	if err != nil {
		return fmt.Errorf("couldn't read directory %s (%v)", dir, err)
	}
	if !ok {
		return fmt.Errorf("directory %s has unsafe permissions", dir)
	}
	return nil
}

// OnlyUser reports whether the file at path is owned by user and its DACL grants access to no
// one but user and SYSTEM.
func OnlyUser(path string, user *windows.SID) (bool, error) {
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return false, err
	}
	return securedFor(sd, user)
}

func securedFor(sd *windows.SECURITY_DESCRIPTOR, user *windows.SID) (bool, error) {
	owner, _, err := sd.Owner()
	if err != nil {
		return false, err
	}
	system, err := windows.CreateWellKnownSid(windows.WinLocalSystemSid)
	if err != nil {
		return false, err
	}
	if !windows.EqualSid(owner, user) && !windows.EqualSid(owner, system) {
		return false, nil
	}
	dacl, _, err := sd.DACL()
	if err != nil || dacl == nil {
		return false, err // a NULL DACL lets everyone in
	}
	for i := range uint32(dacl.AceCount) {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, i, &ace); err != nil {
			return false, err
		}
		if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE {
			continue // a deny takes nothing more away from the user's own checks
		}
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		if !windows.EqualSid(sid, user) && !windows.EqualSid(sid, system) {
			return false, nil
		}
	}
	return true, nil
}

// SocketPath returns the socket path for -L name (or "default") in the socket directory.
func SocketPath(tmpdir string, uid int, name string) (string, error) {
	if name == "" {
		name = "default"
	}
	dir, err := SocketDir(tmpdir, uid)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, name), nil
}

// PipeName is the named pipe the server for the socket path listens on: the hash of the path's
// absolute, lower-cased form (Windows paths are not case-sensitive).
func PipeName(path string) string {
	if strings.HasPrefix(path, `\\.\pipe\`) {
		return path
	}
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	sum := sha256.Sum256([]byte(strings.ToLower(filepath.Clean(path))))
	return `\\.\pipe\imux-core-` + hex.EncodeToString(sum[:12])
}

// PeerSID returns the token user of a pipe client's process, ServerSID of a server's; tests
// replace them.
var (
	PeerSID   = processSID
	ServerSID = processSID
)

// processSID returns the token user of process pid.
func processSID(pid uint32) (*windows.SID, error) {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return nil, err
	}
	defer func() { _ = windows.CloseHandle(h) }()
	var tok windows.Token
	if err := windows.OpenProcessToken(h, windows.TOKEN_QUERY, &tok); err != nil {
		return nil, err
	}
	defer func() { _ = tok.Close() }()
	u, err := tok.GetTokenUser()
	if err != nil {
		return nil, err
	}
	return u.User.Sid.Copy()
}

// pipeAddr is a named pipe's net.Addr.
type pipeAddr string

func (pipeAddr) Network() string  { return "pipe" }
func (a pipeAddr) String() string { return string(a) }

// pipeConn is one end of a connected pipe instance, through Go's poller (it is overlapped), so
// deadlines work as on a socket.
type pipeConn struct {
	*os.File
	addr pipeAddr
}

func (c *pipeConn) LocalAddr() net.Addr  { return c.addr }
func (c *pipeConn) RemoteAddr() net.Addr { return c.addr }

func (c *pipeConn) Read(b []byte) (int, error) {
	n, err := c.File.Read(b)
	// The other end going is the end of the stream.
	if err != nil && (errors.Is(err, windows.ERROR_BROKEN_PIPE) || errors.Is(err, windows.ERROR_PIPE_NOT_CONNECTED)) {
		return n, io.EOF
	}
	return n, err
}

// Listener accepts connections only from its own user, one pipe instance at a time, with the
// next instance waiting before a connection is returned.
type Listener struct {
	name string
	user *windows.SID
	sa   *windows.SecurityAttributes

	mu      sync.Mutex
	pending windows.Handle // the instance waiting for a client
	closed  bool
}

// Listen creates the server's pipe for the socket path. Its first instance claims the name,
// so a name already in use fails.
func Listen(path string) (*Listener, error) {
	user, err := CurrentUser()
	if err != nil {
		return nil, err
	}
	sa, err := securityAttributes(user)
	if err != nil {
		return nil, err
	}
	l := &Listener{name: PipeName(path), user: user, sa: sa}
	h, err := l.instance(true)
	if err != nil {
		return nil, err
	}
	l.pending = h
	return l, nil
}

func (l *Listener) instance(first bool) (windows.Handle, error) {
	n, err := windows.UTF16PtrFromString(l.name)
	if err != nil {
		return 0, err
	}
	mode := uint32(windows.PIPE_ACCESS_DUPLEX | windows.FILE_FLAG_OVERLAPPED)
	if first {
		mode |= windows.FILE_FLAG_FIRST_PIPE_INSTANCE
	}
	h, err := windows.CreateNamedPipe(n, mode,
		windows.PIPE_TYPE_BYTE|windows.PIPE_READMODE_BYTE|windows.PIPE_WAIT|windows.PIPE_REJECT_REMOTE_CLIENTS,
		windows.PIPE_UNLIMITED_INSTANCES, pipeBuffer, pipeBuffer, 0, l.sa)
	if err != nil {
		return 0, &os.PathError{Op: "listen", Path: l.name, Err: err}
	}
	return h, nil
}

// connect waits for a client on instance h, until it connects or the listener closes.
func (l *Listener) connect(h windows.Handle) error {
	ev, err := windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		return err
	}
	defer func() { _ = windows.CloseHandle(ev) }()
	ov := &windows.Overlapped{HEvent: ev}
	err = windows.ConnectNamedPipe(h, ov)
	switch {
	case err == nil, errors.Is(err, windows.ERROR_PIPE_CONNECTED):
		return nil
	case !errors.Is(err, windows.ERROR_IO_PENDING):
		return err
	}
	var n uint32
	return windows.GetOverlappedResult(h, ov, &n, true)
}

// Accept returns the next connection from the server's own user. Others are disconnected at
// once, without a reply.
func (l *Listener) Accept() (net.Conn, error) {
	for {
		l.mu.Lock()
		h, closed := l.pending, l.closed
		l.mu.Unlock()
		if closed {
			return nil, net.ErrClosed
		}
		if err := l.connect(h); err != nil {
			l.mu.Lock()
			closed := l.closed
			l.mu.Unlock()
			if closed {
				return nil, net.ErrClosed
			}
			_ = windows.CloseHandle(h)
			if h, err = l.instance(false); err != nil {
				return nil, err
			}
			l.mu.Lock()
			l.pending = h
			l.mu.Unlock()
			continue
		}
		// The next instance waits before this one is handed over, so clients are never refused
		// for want of one.
		next, err := l.instance(false)
		l.mu.Lock()
		if l.closed {
			l.mu.Unlock()
			if err == nil {
				_ = windows.CloseHandle(next)
			}
			_ = windows.CloseHandle(h)
			return nil, net.ErrClosed
		}
		if err != nil {
			l.mu.Unlock()
			_ = windows.CloseHandle(h)
			return nil, err
		}
		l.pending = next
		l.mu.Unlock()
		if !l.allowed(h) {
			_ = windows.DisconnectNamedPipe(h)
			_ = windows.CloseHandle(h)
			continue
		}
		return &pipeConn{File: os.NewFile(uintptr(h), l.name), addr: pipeAddr(l.name)}, nil
	}
}

// allowed checks the client's process: its token user must be the server's.
func (l *Listener) allowed(h windows.Handle) bool {
	var pid uint32
	if err := windows.GetNamedPipeClientProcessId(h, &pid); err != nil {
		return false
	}
	sid, err := PeerSID(pid)
	return err == nil && windows.EqualSid(sid, l.user)
}

// Close stops listening: the waiting instance is cancelled and closed.
func (l *Listener) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return nil
	}
	l.closed = true
	_ = windows.CancelIoEx(l.pending, nil)
	return windows.CloseHandle(l.pending)
}

// Addr is the pipe's name.
func (l *Listener) Addr() net.Addr { return pipeAddr(l.name) }

// errNotOwnServer is Dial's error for a pipe whose server is another user's.
var errNotOwnServer = errors.New("the server is not this user's")

// ownServer checks the pipe's server process: its token user must be this process's.
func ownServer(h windows.Handle) bool {
	var pid uint32
	if err := windows.GetNamedPipeServerProcessId(h, &pid); err != nil {
		return false
	}
	user, err := CurrentUser()
	if err != nil {
		return false
	}
	sid, err := ServerSID(pid)
	return err == nil && windows.EqualSid(sid, user)
}

// dialWait is how long Dial waits for a free instance when every one is busy.
const dialWait = 2 * time.Second

// Dial connects to the server for the socket path. With no server, the error is
// windows.ERROR_FILE_NOT_FOUND (errors.Is fs.ErrNotExist).
func Dial(path string) (net.Conn, error) {
	name := PipeName(path)
	n, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(dialWait)
	for {
		// SECURITY_IDENTIFICATION: the server may learn who the client is, not act as it.
		h, err := windows.CreateFile(n, windows.GENERIC_READ|windows.GENERIC_WRITE, 0, nil, windows.OPEN_EXISTING,
			windows.FILE_FLAG_OVERLAPPED|windows.SECURITY_SQOS_PRESENT|windows.SECURITY_IDENTIFICATION, 0)
		if err == nil {
			// The server must be the user's own: a pipe another user created first under the
			// name would otherwise get this client's commands and environment.
			if !ownServer(h) {
				_ = windows.CloseHandle(h)
				return nil, &os.PathError{Op: "dial", Path: path, Err: errNotOwnServer}
			}
			return &pipeConn{File: os.NewFile(uintptr(h), name), addr: pipeAddr(name)}, nil
		}
		if !errors.Is(err, windows.ERROR_PIPE_BUSY) || time.Now().After(deadline) {
			return nil, &os.PathError{Op: "dial", Path: path, Err: err}
		}
		time.Sleep(10 * time.Millisecond)
	}
}
