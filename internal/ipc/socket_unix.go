//go:build unix

package ipc

import (
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
)

// The socket directory and peer checks on Unix (ARCHITECTURE.md §7).

// DirInfo is what the directory check reads about the socket directory; tests replace it.
type DirInfo struct {
	IsDir bool
	UID   int
	Perm  fs.FileMode
}

// StatDir reads a directory's type, owner and permissions without following a symlink.
var StatDir = func(path string) (DirInfo, error) {
	fi, err := os.Lstat(path)
	if err != nil {
		return DirInfo{}, err
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return DirInfo{}, errors.New("no owner")
	}
	return DirInfo{IsDir: fi.IsDir(), UID: int(st.Uid), Perm: fi.Mode().Perm()}, nil
}

// SocketDir returns the socket directory for uid, creating it 0700 if needed and checking it:
// a directory, owned by uid, with no group or other access. tmpdir is $TMUX_TMPDIR (or "").
func SocketDir(tmpdir string, uid int) (string, error) {
	if tmpdir == "" {
		tmpdir = "/tmp"
	}
	base, err := filepath.EvalSymlinks(tmpdir)
	if err != nil {
		return "", fmt.Errorf("no suitable socket path")
	}
	dir := filepath.Join(base, "imux-core-"+strconv.Itoa(uid))
	if err := os.Mkdir(dir, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
		return "", fmt.Errorf("couldn't create directory %s (%v)", dir, unwrapErrno(err))
	}
	info, err := StatDir(dir)
	if err != nil {
		return "", fmt.Errorf("couldn't read directory %s (%v)", dir, unwrapErrno(err))
	}
	if !info.IsDir {
		return "", fmt.Errorf("%s is not a directory", dir)
	}
	if info.UID != uid || info.Perm&0o077 != 0 {
		return "", fmt.Errorf("directory %s has unsafe permissions", dir)
	}
	return dir, nil
}

func unwrapErrno(err error) error {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return pe.Err
	}
	return err
}

// SocketPath returns the socket for -L name (or "default") in the socket directory.
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

// Listener accepts connections only from its own user.
type Listener struct {
	*net.UnixListener
	uid int
}

// PeerUID returns the uid of a connection's other end; tests replace it.
var PeerUID = peerUID

// Listen creates the socket at path, readable and writable by its owner only. Whatever is at
// path is removed first, and the socket is left when the listener closes, as tmux does: the
// client that starts a server has checked under the lock file that nothing listens there.
func Listen(path string) (*Listener, error) {
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	l, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		return nil, err
	}
	// The directory is 0700 already; the socket is tightened at once.
	if err := os.Chmod(path, 0o600); err != nil {
		_ = l.Close()
		return nil, err
	}
	l.SetUnlinkOnClose(false)
	return &Listener{UnixListener: l, uid: os.Getuid()}, nil
}

// Accept returns the next connection from the server's own user. Others are closed at once,
// without a reply.
func (l *Listener) Accept() (net.Conn, error) {
	for {
		c, err := l.AcceptUnix()
		if err != nil {
			return nil, err
		}
		uid, err := PeerUID(c)
		if err != nil || uid != l.uid {
			_ = c.Close()
			continue
		}
		return c, nil
	}
}

// Dial connects to the server at path.
func Dial(path string) (*net.UnixConn, error) {
	return net.DialUnix("unix", nil, &net.UnixAddr{Name: path, Net: "unix"})
}
