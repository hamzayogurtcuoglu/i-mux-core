package server

import (
	"errors"
	"net"
	"os"
	"time"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/cmdlang"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/commands"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/ipc"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/model"
)

// Clients on the socket (ARCHITECTURE.md §5): each connection says Hello, then sends a command
// list and gets its Result. A connected client keeps the server up, as in tmux.

// helloTimeout bounds how long a connection may take to say Hello and send its command.
var helloTimeout = 10 * time.Second

// Serve accepts clients on l until the server ends; then it closes l. Run must be running.
func (s *Server) Serve(l net.Listener) {
	go func() {
		<-s.done
		_ = l.Close()
		s.connMu.Lock()
		for c := range s.conns {
			// Their reads end; a Result being written still goes out.
			if uc, ok := c.(*net.UnixConn); ok {
				_ = uc.CloseRead()
			} else {
				_ = c.SetReadDeadline(time.Now())
			}
		}
		s.connMu.Unlock()
	}()
	for {
		c, err := l.Accept()
		if err != nil {
			return
		}
		s.connMu.Lock()
		select {
		case <-s.done:
			s.connMu.Unlock()
			_ = c.Close()
			continue
		default:
		}
		s.conns[c] = struct{}{}
		s.connWG.Add(1)
		s.connMu.Unlock()
		go s.handle(c)
	}
}

// WaitClients waits, at most d, for the client connections to end, so that a command's Result
// (kill-server's) is written before the process exits.
func (s *Server) WaitClients(d time.Duration) {
	ended := make(chan struct{})
	go func() {
		s.connWG.Wait()
		close(ended)
	}()
	select {
	case <-ended:
	case <-time.After(d):
	}
}

func (s *Server) handle(c net.Conn) {
	defer func() {
		_ = c.Close()
		s.connMu.Lock()
		delete(s.conns, c)
		s.connMu.Unlock()
		s.connWG.Done()
	}()
	if !s.post(func() {
		s.clients++
		s.held = false
	}) {
		return
	}
	defer s.post(func() {
		s.clients--
		s.checkExit()
	})

	_ = c.SetReadDeadline(time.Now().Add(helloTimeout))
	r := ipc.NewReader(c)
	f, err := r.Read()
	if err != nil || f.Type != ipc.TypeHello {
		return
	}
	var hello ipc.Hello
	if err := f.Decode(&hello); err != nil {
		return
	}
	if vf := ipc.CheckHello(&hello); vf != nil {
		_ = ipc.WriteFrame(c, vf.Type, vf.Payload)
		return
	}
	if f, err = r.Read(); err != nil || f.Type != ipc.TypeCommand {
		return
	}
	var cmd ipc.Command
	if err := f.Decode(&cmd); err != nil {
		return
	}
	_ = c.SetReadDeadline(time.Time{})

	client := &commands.Client{Cwd: hello.Cwd, Env: hello.Env, TTY: hello.StdinTTY, Name: hello.TTYName}
	var res commands.Result
	var att *attachedClient
	queue := ipc.NewQueue(0, 0)
	ran := make(chan struct{})
	if !s.post(func() {
		defer close(ran)
		res = commands.Run(s, client, cmd.Commands)
		if client.AttachTo == nil || res.Failed || s.tree.Session(client.AttachTo.ID) != client.AttachTo {
			return
		}
		var err error
		if att, err = s.attach(&hello, client.AttachTo, queue); err != nil {
			res = commands.Result{Stderr: "open terminal failed: " + err.Error() + "\n", Failed: true}
		}
	}) {
		return
	}
	select {
	case <-ran:
	case <-s.done:
		// The loop runs an event to its end before it returns (kill-server ends it).
		select {
		case <-ran:
		default:
			return
		}
	}
	if att != nil {
		s.serveAttached(c, r, att)
		return
	}
	out := ipc.Result{Stdout: res.Stdout, Stderr: res.Stderr}
	if res.Failed {
		out.Code = 1
	}
	rf, err := ipc.Encode(ipc.TypeResult, &out)
	if err == nil {
		_ = ipc.WriteFrame(c, rf.Type, rf.Payload)
	}
}

// LoadConfig runs a config file's commands (in the loop): tmux's -f, before the first client's
// command. A missing file is not an error. It returns the errors, which tmux shows to the first
// client that attaches (task C3).
func (s *Server) LoadConfig(path string) []string {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return []string{path + ": " + err.Error()}
	}
	env := func(name string) (string, bool) {
		for _, kv := range s.opts.Environ {
			if len(kv) > len(name) && kv[len(name)] == '=' && kv[:len(name)] == name {
				return kv[len(name)+1:], true
			}
		}
		return "", false
	}
	home := func(user string) (string, bool) {
		if user != "" {
			return "", false
		}
		return env("HOME")
	}
	var causes []string
	if err := s.Do(func(_ *model.Tree) {
		causes = commands.RunConfig(s, path, string(b), cmdlang.Options{Env: env, Home: home})
	}); err != nil {
		return []string{err.Error()}
	}
	return causes
}
