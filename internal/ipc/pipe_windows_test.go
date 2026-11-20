//go:build windows

package ipc

import (
	"errors"
	"io"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func listenT(t *testing.T) (*Listener, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "s")
	l, err := Listen(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	return l, path
}

func accept(t *testing.T, l *Listener) <-chan net.Conn {
	ch := make(chan net.Conn, 1)
	go func() {
		c, err := l.Accept()
		if err != nil {
			close(ch)
			return
		}
		ch <- c
	}()
	return ch
}

// A client and the server talk both ways, and deadlines work as on a socket.
func TestPipeRoundTrip(t *testing.T) {
	l, path := listenT(t)
	got := accept(t, l)
	c, err := Dial(path)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	s := <-got
	if s == nil {
		t.Fatal("no connection accepted")
	}
	defer s.Close()
	if _, err := c.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 5)
	if _, err := io.ReadFull(s, buf); err != nil || string(buf) != "hello" {
		t.Fatalf("server read %q, %v", buf, err)
	}
	if _, err := s.Write([]byte("world")); err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadFull(c, buf); err != nil || string(buf) != "world" {
		t.Fatalf("client read %q, %v", buf, err)
	}
	// A deadline ends a read that waits.
	if err := s.SetReadDeadline(time.Now().Add(50 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Read(buf); !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("read after the deadline: %v", err)
	}
	// The server going is the end of the client's stream.
	_ = s.Close()
	if _, err := c.Read(buf); err != io.EOF {
		t.Fatalf("client read after the server closed: %v", err)
	}
}

// Two clients at once each get their own instance.
func TestPipeTwoClients(t *testing.T) {
	l, path := listenT(t)
	for i := range 2 {
		got := accept(t, l)
		c, err := Dial(path)
		if err != nil {
			t.Fatalf("client %d: %v", i, err)
		}
		defer c.Close()
		if s := <-got; s == nil {
			t.Fatalf("client %d not accepted", i)
		} else {
			defer s.Close()
		}
	}
}

// The pipe and the socket directory let in only the user and SYSTEM.
func TestPipeDACL(t *testing.T) {
	l, _ := listenT(t)
	user, err := CurrentUser()
	if err != nil {
		t.Fatal(err)
	}
	sd, err := windows.GetSecurityInfo(l.pending, windows.SE_KERNEL_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := securedFor(sd, user); err != nil || !ok {
		t.Fatalf("the pipe's DACL %s: %v %v", sd, ok, err)
	}
	if !strings.Contains(sd.String(), "D:P") {
		t.Errorf("the pipe's DACL is not protected: %s", sd)
	}
	// Owned by the user, even when this test runs elevated (GitHub's runners do): an
	// administrator's objects are otherwise owned by BUILTIN\Administrators (E13).
	if owner, _, err := sd.Owner(); err != nil || !windows.EqualSid(owner, user) {
		t.Errorf("the pipe's owner: %v %v, want the user", owner, err)
	}
	dir, err := SocketDir(t.TempDir(), 0)
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := OnlyUser(dir, user); err != nil || !ok {
		t.Fatalf("the socket directory: %v %v", ok, err)
	}
	// A directory others may use is refused.
	open := filepath.Join(t.TempDir(), "open")
	if err := os.Mkdir(filepath.Join(open), 0o777); err != nil {
		t.Fatal(err)
	}
	everyone, _ := windows.StringToSid("S-1-1-0")
	if err := grant(open, everyone); err != nil {
		t.Fatal(err)
	}
	if err := checkDir(open, user); err == nil || !strings.Contains(err.Error(), "unsafe permissions") {
		t.Fatalf("a directory Everyone can use: %v", err)
	}
}

// grant gives sid full access to path, besides what it has.
func grant(path string, sid *windows.SID) error {
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		return err
	}
	old, _, err := sd.DACL()
	if err != nil {
		return err
	}
	acl, err := windows.ACLFromEntries([]windows.EXPLICIT_ACCESS{{
		AccessPermissions: windows.GENERIC_ALL,
		AccessMode:        windows.GRANT_ACCESS,
		Trustee: windows.TRUSTEE{
			TrusteeForm:  windows.TRUSTEE_IS_SID,
			TrusteeType:  windows.TRUSTEE_IS_WELL_KNOWN_GROUP,
			TrusteeValue: windows.TrusteeValueFromSID(sid),
		},
	}}, old)
	if err != nil {
		return err
	}
	return windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION, nil, nil, acl, nil)
}

// A client whose token user is not the server's is disconnected without a reply, and the next
// one is served.
func TestPipeOtherUserRefused(t *testing.T) {
	l, path := listenT(t)
	system, err := windows.CreateWellKnownSid(windows.WinLocalSystemSid)
	if err != nil {
		t.Fatal(err)
	}
	real := PeerSID
	PeerSID = func(uint32) (*windows.SID, error) { return system, nil }
	got := accept(t, l)
	c, err := Dial(path)
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 1)
	_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := c.Read(buf); err != io.EOF {
		t.Fatalf("the refused client read: %v", err)
	}
	_ = c.Close()
	PeerSID = real
	c2, err := Dial(path)
	if err != nil {
		t.Fatal(err)
	}
	defer c2.Close()
	select {
	case s := <-got:
		if s == nil {
			t.Fatal("Accept failed")
		}
		_ = s.Close()
	case <-time.After(5 * time.Second):
		t.Fatal("the user's own client was not accepted")
	}
}

// A name already listened on cannot be taken by a second server.
func TestPipeFirstInstance(t *testing.T) {
	_, path := listenT(t)
	if l, err := Listen(path); err == nil {
		_ = l.Close()
		t.Fatal("a second Listen on the same path succeeded")
	}
}

// With no server, Dial fails as for a missing socket; after Close, Accept ends.
func TestPipeNoServer(t *testing.T) {
	path := filepath.Join(t.TempDir(), "none")
	if _, err := Dial(path); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("dial with no server: %v", err)
	}
	l, err := Listen(path)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := l.Accept(); done <- err }()
	time.Sleep(50 * time.Millisecond)
	_ = l.Close()
	select {
	case err := <-done:
		if !errors.Is(err, net.ErrClosed) {
			t.Fatalf("Accept after Close: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Accept did not end with the listener")
	}
	if _, err := Dial(path); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("dial after the server closed: %v", err)
	}
}

// A path names one pipe, whatever its case or form.
func TestPipeName(t *testing.T) {
	a := PipeName(`C:\Users\x\AppData\Local\i-mux-core\default`)
	if b := PipeName(`c:\users\X\appdata\local\i-mux-core\.\default`); a != b {
		t.Errorf("%s != %s", a, b)
	}
	if !strings.HasPrefix(a, `\\.\pipe\imux-core-`) {
		t.Errorf("name %s", a)
	}
	if PipeName(`\\.\pipe\given`) != `\\.\pipe\given` {
		t.Error("a pipe name is not kept")
	}
}

// A client does not talk to a server that is not its user's (a pipe someone else created
// first under the name).
func TestPipeOtherUsersServerRefused(t *testing.T) {
	l, path := listenT(t)
	accept(t, l)
	system, err := windows.CreateWellKnownSid(windows.WinLocalSystemSid)
	if err != nil {
		t.Fatal(err)
	}
	real := ServerSID
	t.Cleanup(func() { ServerSID = real })
	ServerSID = func(uint32) (*windows.SID, error) { return system, nil }
	if c, err := Dial(path); err == nil {
		_ = c.Close()
		t.Fatal("dialed another user's server")
	} else if !errors.Is(err, errNotOwnServer) {
		t.Fatalf("dial: %v", err)
	}
}

// An object owned by BUILTIN\Administrators (left by an elevated run) is the user's only for an
// elevated administrator (E13).
func TestAdminOwned(t *testing.T) {
	user, err := CurrentUser()
	if err != nil {
		t.Fatal(err)
	}
	sd, err := windows.SecurityDescriptorFromString("O:BAD:P(A;;GA;;;" + user.String() + ")(A;;GA;;;SY)")
	if err != nil {
		t.Fatal(err)
	}
	old := elevatedAdmin
	t.Cleanup(func() { elevatedAdmin = old })
	for _, elevated := range []bool{false, true} {
		elevatedAdmin = func() bool { return elevated }
		if ok, err := securedFor(sd, user); err != nil || ok != elevated {
			t.Errorf("elevated %v: %v %v", elevated, ok, err)
		}
	}
	other, err := windows.SecurityDescriptorFromString("O:BUD:P(A;;GA;;;" + user.String() + ")")
	if err != nil {
		t.Fatal(err)
	}
	elevatedAdmin = func() bool { return true }
	if ok, _ := securedFor(other, user); ok {
		t.Error("an object owned by BUILTIN\\Users passed")
	}
}
