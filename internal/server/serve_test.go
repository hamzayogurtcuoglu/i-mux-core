//go:build darwin || linux

package server

import (
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/ipc"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/model"
)

// The server's clients on its socket (TASKS.md C2.3).

func listen(t *testing.T) (net.Listener, string) {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "imux-st-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	path := filepath.Join(dir, "s")
	l, err := ipc.Listen(path)
	if err != nil {
		t.Fatal(err)
	}
	return l, path
}

func dial(t *testing.T, path string, protocol int) (net.Conn, *ipc.Reader) {
	t.Helper()
	c, err := ipc.Dial(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	f, _ := ipc.Encode(ipc.TypeHello, &ipc.Hello{Protocol: protocol, Cwd: "/"})
	if err := ipc.WriteFrame(c, f.Type, f.Payload); err != nil {
		t.Fatal(err)
	}
	return c, ipc.NewReader(c)
}

func command(t *testing.T, c net.Conn, r *ipc.Reader, words ...string) ipc.Result {
	t.Helper()
	f, _ := ipc.Encode(ipc.TypeCommand, &ipc.Command{Commands: [][]string{words}})
	if err := ipc.WriteFrame(c, f.Type, f.Payload); err != nil {
		t.Fatal(err)
	}
	f, err := r.Read()
	if err != nil || f.Type != ipc.TypeResult {
		t.Fatalf("frame %v, %v", f.Type, err)
	}
	var res ipc.Result
	if err := f.Decode(&res); err != nil {
		t.Fatal(err)
	}
	return res
}

func ended(s *Server, within time.Duration) bool {
	select {
	case <-s.Done():
		return true
	case <-time.After(within):
		return false
	}
}

func TestHoldForClient(t *testing.T) {
	// No client comes: the server ends after the hold.
	s := startServer(t, Options{HoldForClient: true, HoldTimeout: 50 * time.Millisecond})
	if !ended(s, 5*time.Second) {
		t.Fatal("the server outlived its hold with no client")
	}

	// A client comes: the server lasts while it is connected, and ends when it goes.
	l, path := listen(t)
	s = startServer(t, Options{HoldForClient: true, HoldTimeout: time.Hour, SocketPath: path})
	go s.Serve(l)
	c, r := dial(t, path, ipc.ProtocolVersion)
	if ended(s, 100*time.Millisecond) {
		t.Fatal("the server ended with a client connected")
	}
	// A command client is done once it has its Result.
	if res := command(t, c, r, "has-session"); res.Code != 1 || res.Stderr != "no current target\n" {
		t.Errorf("has-session with no session: %+v", res)
	}
	if !ended(s, 5*time.Second) {
		t.Fatal("the server outlived its last client with no session")
	}
	// The socket is left, as tmux leaves it.
	if _, err := os.Stat(path); err != nil {
		t.Errorf("socket: %v", err)
	}
}

func TestServeSessions(t *testing.T) {
	l, path := listen(t)
	s := startServer(t, Options{SocketPath: path, Environ: env})
	go s.Serve(l)
	c, r := dial(t, path, ipc.ProtocolVersion)
	if res := command(t, c, r, "new-session", "-d", "-s", "a", "exec sleep 100"); res.Code != 0 {
		t.Fatalf("new-session: %+v", res)
	}
	c.Close()
	// The session keeps the server up with no client.
	if ended(s, 100*time.Millisecond) {
		t.Fatal("the server ended with a session left")
	}
	var names []string
	if err := s.Do(func(tr *model.Tree) {
		for _, sess := range tr.Sessions() {
			names = append(names, sess.Name)
		}
	}); err != nil || len(names) != 1 || names[0] != "a" {
		t.Fatalf("sessions %q %v", names, err)
	}
	// kill-server: the client gets its Result, then the server ends.
	c, r = dial(t, path, ipc.ProtocolVersion)
	if res := command(t, c, r, "kill-server"); res.Code != 0 {
		t.Errorf("kill-server: %+v", res)
	}
	if !ended(s, 5*time.Second) {
		t.Fatal("kill-server did not end the server")
	}
	s.WaitClients(5 * time.Second)
}

func TestServeVersionMismatch(t *testing.T) {
	l, path := listen(t)
	s := startServer(t, Options{SocketPath: path})
	go s.Serve(l)
	_, r := dial(t, path, ipc.ProtocolVersion+1)
	f, err := r.Read()
	if err != nil || f.Type != ipc.TypeVersionError {
		t.Fatalf("frame %v, %v", f.Type, err)
	}
	var v ipc.VersionError
	if err := f.Decode(&v); err != nil || v.Server != ipc.ProtocolVersion {
		t.Errorf("%+v %v", v, err)
	}
	if _, err := r.Read(); err == nil {
		t.Error("the connection stayed open")
	}
}

// A program that cannot run makes a pane that ends at once, as in tmux, not a failed command.
func TestServeProgramNotFound(t *testing.T) {
	l, path := listen(t)
	s := startServer(t, Options{SocketPath: path, Environ: env})
	go s.Serve(l)
	c, r := dial(t, path, ipc.ProtocolVersion)
	if res := command(t, c, r, "new-session", "-d", "-s", "keep", "exec sleep 100"); res.Code != 0 {
		t.Fatalf("new-session: %+v", res)
	}
	c, r = dial(t, path, ipc.ProtocolVersion)
	if res := command(t, c, r, "new-session", "-d", "-s", "x", "/nonexistent/program", "arg"); res.Code != 0 {
		t.Fatalf("new-session with a missing program: %+v", res)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		var gone bool
		if err := s.Do(func(tr *model.Tree) { gone = tr.SessionByName("x") == nil }); err != nil {
			t.Fatal(err)
		}
		if gone {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the session of a program that cannot run is still there")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// A client that has connected keeps the server up, even when its first event is queued behind
// the one that ends the last session (tmux's server_loop waits for its clients): it was told
// "server exited unexpectedly".
func TestConnectedClientKeepsServer(t *testing.T) {
	l, path := listen(t)
	s := startServer(t, Options{SocketPath: path, Environ: env})
	go s.Serve(l)
	c, r := dial(t, path, ipc.ProtocolVersion)
	if res := command(t, c, r, "new-session", "-d", "-s", "a", "exec cat"); res.Code != 0 {
		t.Fatalf("new-session: %+v", res)
	}
	c.Close()

	// Hold the loop; behind it, the event that ends the last session, then the new client's.
	hold := make(chan struct{})
	s.post(func() { <-hold })
	s.post(func() {
		for _, sess := range s.tree.Sessions() {
			s.RemoveSession(sess)
		}
		s.checkExit()
	})
	c2, r2 := dial(t, path, ipc.ProtocolVersion)
	defer c2.Close()
	deadline := time.Now().Add(5 * time.Second)
	for s.accepted.Load() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the connection was not accepted")
		}
		time.Sleep(5 * time.Millisecond)
	}
	close(hold)
	if res := command(t, c2, r2, "new-session", "-d", "-s", "b", "exec cat"); res.Code != 0 {
		t.Fatalf("new-session on a server whose last session went: %+v", res)
	}
}
