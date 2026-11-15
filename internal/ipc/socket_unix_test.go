//go:build darwin || linux

package ipc

import (
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// shortTemp returns a short temporary directory: socket paths are limited to about 100 bytes.
func shortTemp(t *testing.T) string {
	t.Helper()
	d, err := os.MkdirTemp("/tmp", "ipc-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(d) })
	return d
}

func TestSocketDirCreated(t *testing.T) {
	tmp := shortTemp(t)
	uid := os.Getuid()
	dir, err := SocketDir(tmp, uid)
	if err != nil {
		t.Fatal(err)
	}
	real, _ := filepath.EvalSymlinks(tmp)
	if want := filepath.Join(real, "imux-core-"+strconv.Itoa(uid)); dir != want {
		t.Fatalf("dir %q, want %q", dir, want)
	}
	fi, err := os.Stat(dir)
	if err != nil || fi.Mode().Perm() != 0o700 {
		t.Fatalf("mode %v, %v", fi.Mode(), err)
	}
	// A second call reuses it.
	if _, err := SocketDir(tmp, uid); err != nil {
		t.Fatal(err)
	}
}

func TestSocketDirRefused(t *testing.T) {
	uid := os.Getuid()
	// Group or other access: refused (tmux allows the group's; the engine does not).
	for _, mode := range []os.FileMode{0o750, 0o707, 0o777} {
		tmp := shortTemp(t)
		dir := filepath.Join(tmp, "imux-core-"+strconv.Itoa(uid))
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(dir, mode); err != nil {
			t.Fatal(err)
		}
		_, err := SocketDir(tmp, uid)
		if err == nil || !strings.Contains(err.Error(), "has unsafe permissions") {
			t.Errorf("mode %v: %v", mode, err)
		}
	}
	// Not a directory.
	tmp := shortTemp(t)
	if err := os.WriteFile(filepath.Join(tmp, "imux-core-"+strconv.Itoa(uid)), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := SocketDir(tmp, uid); err == nil || !strings.Contains(err.Error(), "is not a directory") {
		t.Errorf("a file: %v", err)
	}
	// Another owner, through the injected check.
	old := StatDir
	StatDir = func(string) (DirInfo, error) { return DirInfo{IsDir: true, UID: uid + 1, Perm: 0o700}, nil }
	t.Cleanup(func() { StatDir = old })
	if _, err := SocketDir(shortTemp(t), uid); err == nil || !strings.Contains(err.Error(), "has unsafe permissions") {
		t.Errorf("another owner: %v", err)
	}
}

func listen(t *testing.T) (*Listener, string) {
	t.Helper()
	path := filepath.Join(shortTemp(t), "s")
	l, err := Listen(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	return l, path
}

func TestListenSocketMode(t *testing.T) {
	_, path := listen(t)
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 || fi.Mode()&os.ModeSocket == 0 {
		t.Fatalf("socket mode %v", fi.Mode())
	}
}

func TestAcceptOwnUser(t *testing.T) {
	l, path := listen(t)
	go func() {
		c, err := Dial(path)
		if err == nil {
			_ = WriteFrame(c, TypeInput, []byte("hi"))
			c.Close()
		}
	}()
	c, err := l.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	f, err := NewReader(c).Read()
	if err != nil || string(f.Payload) != "hi" {
		t.Fatalf("%+v %v", f, err)
	}
}

// TestAcceptRefusesOtherUser: a peer with another uid (injected) is closed without a reply,
// and the next one from the owner still gets in.
func TestAcceptRefusesOtherUser(t *testing.T) {
	l, path := listen(t)
	old := PeerUID
	calls := 0
	PeerUID = func(c *net.UnixConn) (int, error) {
		calls++
		if calls == 1 {
			return os.Getuid() + 1, nil
		}
		return old(c)
	}
	t.Cleanup(func() { PeerUID = old })

	refused, err := Dial(path)
	if err != nil {
		t.Fatal(err)
	}
	defer refused.Close()
	accepted := make(chan net.Conn, 1)
	go func() {
		c, err := l.Accept()
		if err == nil {
			accepted <- c
		}
	}()
	_ = refused.SetReadDeadline(time.Now().Add(5 * time.Second))
	if n, err := refused.Read(make([]byte, 1)); n != 0 || !errors.Is(err, io.EOF) {
		t.Fatalf("the other user's connection got %d bytes, %v; want it closed", n, err)
	}
	ok, err := Dial(path)
	if err != nil {
		t.Fatal(err)
	}
	defer ok.Close()
	select {
	case c := <-accepted:
		c.Close()
	case <-time.After(5 * time.Second):
		t.Fatal("the owner's connection was not accepted")
	}
}

func TestPeerUIDIsOwn(t *testing.T) {
	l, path := listen(t)
	go func() {
		if c, err := Dial(path); err == nil {
			_, _ = io.Copy(io.Discard, c) // until the server closes it
			c.Close()
		}
	}()
	c, err := l.AcceptUnix()
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if uid, err := peerUID(c); err != nil || uid != os.Getuid() {
		t.Fatalf("peer uid %d, %v; want %d", uid, err, os.Getuid())
	}
}
