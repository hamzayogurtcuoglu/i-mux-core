package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"sync"

	"github.com/hamzayogurtcuoglu/i-mux-core/internal/cmdlang"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/ipc"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/termout"
	"github.com/hamzayogurtcuoglu/i-mux-core/internal/version"
)

// StreamTerminal is the terminal a stream client says it is (E19).
type StreamTerminal struct {
	Name       string // #{client_name}, #{client_tty}
	Cols, Rows int
}

// StreamClient is an attached client whose terminal is a stream (E19): the
// server's output goes to the writer Attach was given; input and sizes come
// from its methods.
type StreamClient struct {
	c      net.Conn
	mu     sync.Mutex
	done   chan struct{}
	reason string
	err    error
	once   sync.Once
}

// ErrNotAttached means the line ran but attached nothing (a command, not
// attach-session).
var ErrNotAttached = errors.New("imux-core: the command did not attach a client")

// Attach runs args (global flags, then an attach line) as an attached client of a running
// server, inside this process, with a stream for its terminal (E19). It returns once the server
// has attached it; the output then flows to out until the client ends (Wait).
func Attach(ctx context.Context, args []string, term StreamTerminal, out io.Writer) (*StreamClient, error) {
	var errb strings.Builder
	g, rest, code, done := parseGlobals(args, io.Discard, &errb)
	if done {
		return nil, fmt.Errorf("imux-core: %s (exit %d)", strings.TrimSpace(errb.String()), code)
	}
	if len(rest) == 0 {
		return nil, errors.New("imux-core: no attach line")
	}
	cmds := cmdlang.SplitArgv(rest)
	path, err := g.resolveSocket()
	if err != nil {
		return nil, err
	}
	g.noStart = true // never a server: a line that would start one finds none and fails
	c, err := connect(path, g, cmds)
	if err != nil {
		return nil, err
	}
	h := streamHello(g, term)
	if err := send(c, ipc.TypeHello, h); err != nil {
		_ = c.Close()
		return nil, lostServer(err)
	}
	if err := send(c, ipc.TypeCommand, &ipc.Command{Commands: cmds}); err != nil {
		_ = c.Close()
		return nil, lostServer(err)
	}
	r := ipc.NewReader(c)
	stop := context.AfterFunc(ctx, func() { _ = c.Close() })
	for {
		f, err := r.Read()
		if err != nil {
			stop()
			_ = c.Close()
			return nil, lostServer(err)
		}
		switch f.Type {
		case ipc.TypeVersionError:
			var v ipc.VersionError
			_ = f.Decode(&v)
			stop()
			_ = c.Close()
			return nil, &ipc.MismatchError{Socket: path, Client: ipc.ProtocolVersion, Server: v.Server}
		case ipc.TypeResult:
			var res ipc.Result
			_ = f.Decode(&res)
			stop()
			_ = c.Close()
			if msg := strings.TrimSpace(res.Stderr); msg != "" {
				return nil, errors.New(msg)
			}
			return nil, ErrNotAttached
		case ipc.TypeAttach:
			stop()
			s := &StreamClient{c: c, done: make(chan struct{})}
			go s.read(r, out)
			return s, nil
		}
	}
}

// streamHello is a terminal's Hello for a stream: the name and size given, the built-in
// xterm-256color, UTF-8.
func streamHello(g globals, term StreamTerminal) *ipc.Hello {
	h := &ipc.Hello{
		Protocol:      ipc.ProtocolVersion,
		ClientVersion: version.Version,
		Argv:          []string{"imux-core", "attach"},
		Env:           map[string]string{},
		UTF8:          true,
		Pid:           os.Getpid(),
		StdinTTY:      true,
		StdoutTTY:     true,
		TTYName:       term.Name,
		Size:          ipc.Size{Cols: term.Cols, Rows: term.Rows},
	}
	h.Cwd, _ = os.Getwd()
	_ = g // the stream's terminal is UTF-8 whatever -u says
	if ti, err := termout.Builtin(); err == nil {
		h.Terminfo = ti.Capabilities()
	}
	for _, kv := range os.Environ() {
		if k, v, ok := strings.Cut(kv, "="); ok {
			h.Env[k] = v
		}
	}
	h.Env["TERM"] = termout.BuiltinName
	h.Env["COLORTERM"] = "truecolor"
	delete(h.Env, "TMUX") // not a client inside a pane
	return h
}

// read copies the server's output to out until the client ends.
func (s *StreamClient) read(r *ipc.Reader, out io.Writer) {
	defer close(s.done)
	defer func() { _ = s.c.Close() }()
	for {
		f, err := r.Read()
		if err != nil {
			s.finish("", nil)
			return
		}
		switch f.Type {
		case ipc.TypeOutput:
			if _, err := out.Write(f.Payload); err != nil {
				s.finish("", err)
				return
			}
		case ipc.TypeDetach, ipc.TypeExit:
			var end ipc.End
			_ = f.Decode(&end)
			s.finish(end.Reason, nil)
			return
		}
	}
}

func (s *StreamClient) finish(reason string, err error) {
	s.once.Do(func() { s.reason, s.err = reason, err })
}

func (s *StreamClient) frame(t ipc.Type, payload []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	select {
	case <-s.done:
		return io.ErrClosedPipe
	default:
	}
	return ipc.WriteFrame(s.c, t, payload)
}

// Input sends bytes as if typed in the client's terminal (keys, pastes, mouse reports).
func (s *StreamClient) Input(b []byte) error {
	if len(b) == 0 {
		return nil
	}
	return s.frame(ipc.TypeInput, b)
}

// Resize tells the server the client's terminal has a new size.
func (s *StreamClient) Resize(cols, rows int) error {
	f, err := ipc.Encode(ipc.TypeResize, &ipc.Size{Cols: cols, Rows: rows})
	if err != nil {
		return err
	}
	return s.frame(f.Type, f.Payload)
}

// Close ends the client (as a lost terminal would).
func (s *StreamClient) Close() error {
	s.finish("closed", nil)
	return s.c.Close()
}

// Done is closed when the client has ended.
func (s *StreamClient) Done() <-chan struct{} { return s.done }

// Wait waits for the client to end and returns why: the server's reason ("detached", "server
// exited", …), or a write error from the output.
func (s *StreamClient) Wait() (string, error) {
	<-s.done
	return s.reason, s.err
}
