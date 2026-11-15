package ipc

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"reflect"
	"strings"
	"testing"
	"testing/iotest"
	"time"
)

func roundTrip(t *testing.T, f Frame) Frame {
	t.Helper()
	var buf bytes.Buffer
	if err := WriteFrame(&buf, f.Type, f.Payload); err != nil {
		t.Fatal(err)
	}
	// Byte at a time, as a socket may deliver it.
	got, err := NewReader(iotest.OneByteReader(&buf)).Read()
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestRoundTripEveryMessage(t *testing.T) {
	msgs := []struct {
		t   Type
		msg any
		out any
	}{
		{TypeHello, &Hello{
			Protocol: ProtocolVersion, ClientVersion: "0.0.0-dev", Argv: []string{"-L", "x", "new-session", "-d"},
			Cwd: "/tmp", Env: map[string]string{"TERM": "xterm-256color", "IMUX_HOME": "/h"}, Size: Size{120, 40},
			StdinTTY: true, StdoutTTY: true, UTF8: true, Pid: 42,
		}, &Hello{}},
		{TypeCommand, &Command{Commands: [][]string{{"new-session", "-d"}, {"set", "-g", "@x", "a;b"}}}, &Command{}},
		{TypeResult, &Result{Stdout: "%0\n", Stderr: "", Code: 0}, &Result{}},
		{TypeResult, &Result{Stderr: "can't find session: x\n", Code: 1}, &Result{}},
		{TypeResize, &Size{Cols: 80, Rows: 24}, &Size{}},
		{TypeDetach, &End{Reason: "detached (from session main)"}, &End{}},
		{TypeExit, &End{Reason: "too slow", Code: 1}, &End{}},
		{TypeVersionError, &VersionError{Server: 7}, &VersionError{}},
	}
	for _, m := range msgs {
		f, err := Encode(m.t, m.msg)
		if err != nil {
			t.Fatal(err)
		}
		got := roundTrip(t, f)
		if got.Type != m.t {
			t.Fatalf("type %s, want %s", got.Type, m.t)
		}
		if err := got.Decode(m.out); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(m.out, m.msg) {
			t.Errorf("%s: got %+v, want %+v", m.t, m.out, m.msg)
		}
	}
	// Raw and empty frames.
	for _, f := range []Frame{
		{Type: TypeOutput, Payload: []byte("\x1b[2J\x1b[Hhello\xff")},
		{Type: TypeInput, Payload: []byte("\x03")},
		{Type: TypeAttach},
		{Type: TypeSuspend},
		{Type: TypeOutput, Payload: bytes.Repeat([]byte("x"), MaxFrame)},
	} {
		got := roundTrip(t, f)
		if got.Type != f.Type || !bytes.Equal(got.Payload, f.Payload) {
			t.Errorf("%s: payload of %d bytes came back as %d", f.Type, len(f.Payload), len(got.Payload))
		}
	}
}

func TestOversizedFrame(t *testing.T) {
	if err := WriteFrame(io.Discard, TypeOutput, make([]byte, MaxFrame+1)); !errors.Is(err, ErrFrameTooLarge) {
		t.Fatalf("writing: %v", err)
	}
	var hdr [5]byte
	hdr[0] = byte(TypeOutput)
	binary.BigEndian.PutUint32(hdr[1:], MaxFrame+1)
	if _, err := NewReader(bytes.NewReader(hdr[:])).Read(); !errors.Is(err, ErrFrameTooLarge) {
		t.Fatalf("reading: %v", err)
	}
}

// TestOversizedFrameClosesConnection: a server that reads an oversized frame closes the
// connection, so the peer sees the end.
func TestOversizedFrameClosesConnection(t *testing.T) {
	a, b := net.Pipe()
	go func() {
		_, err := NewReader(a).Read()
		if errors.Is(err, ErrFrameTooLarge) {
			a.Close()
		}
	}()
	var hdr [5]byte
	hdr[0] = byte(TypeInput)
	binary.BigEndian.PutUint32(hdr[1:], 1<<30)
	if _, err := b.Write(hdr[:]); err != nil {
		t.Fatal(err)
	}
	_ = b.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := b.Read(make([]byte, 1)); !errors.Is(err, io.EOF) {
		t.Fatalf("the connection was not closed: %v", err)
	}
}

func TestReadErrors(t *testing.T) {
	if _, err := NewReader(bytes.NewReader(nil)).Read(); err != io.EOF {
		t.Errorf("empty: %v", err)
	}
	if _, err := NewReader(bytes.NewReader([]byte{byte(TypeOutput), 0, 0, 0, 5, 'a'})).Read(); err != io.ErrUnexpectedEOF {
		t.Errorf("cut short: %v", err)
	}
	if _, err := NewReader(bytes.NewReader([]byte{99, 0, 0, 0, 0})).Read(); err == nil {
		t.Error("unknown type: no error")
	}
	if err := (Frame{Type: TypeResult, Payload: []byte("{")}).Decode(&Result{}); err == nil {
		t.Error("bad JSON: no error")
	}
}

func TestVersionMismatch(t *testing.T) {
	if f := CheckHello(&Hello{Protocol: ProtocolVersion}); f != nil {
		t.Fatalf("same version refused: %+v", f)
	}
	f := CheckHello(&Hello{Protocol: ProtocolVersion + 1})
	if f == nil || f.Type != TypeVersionError {
		t.Fatalf("other version: %+v", f)
	}
	var ve VersionError
	if err := f.Decode(&ve); err != nil || ve.Server != ProtocolVersion {
		t.Fatalf("%+v %v", ve, err)
	}
	err := &MismatchError{Socket: "/tmp/imux-core-501/imux", Client: ProtocolVersion + 1, Server: ve.Server}
	// i-mux's classify: "error connecting to" means no server.
	if msg := strings.ToLower(err.Error()); !strings.Contains(msg, "error connecting to /tmp/imux-core-501/imux") {
		t.Fatalf("message %q", msg)
	}
}

func TestQueue(t *testing.T) {
	q := NewQueue(10, time.Second)
	now := time.Unix(0, 0)
	q.now = func() time.Time { return now }
	for i := 0; i < 3; i++ {
		if err := q.Push(Frame{Type: TypeOutput, Payload: []byte("123456")}); err != nil {
			t.Fatalf("push %d: %v", i, err)
		}
	}
	// Over the cap now, but within the grace period.
	now = now.Add(500 * time.Millisecond)
	if err := q.Push(Frame{Type: TypeOutput, Payload: []byte("x")}); err != nil {
		t.Fatalf("within grace: %v", err)
	}
	// Draining below the cap resets the clock.
	for q.Len() > 10 {
		q.Pop()
	}
	now = now.Add(2 * time.Second)
	if err := q.Push(Frame{Type: TypeOutput, Payload: []byte("1234567890")}); err != nil {
		t.Fatalf("after draining: %v", err)
	}
	if err := q.Push(Frame{Type: TypeOutput, Payload: []byte("y")}); err != nil {
		t.Fatalf("first push over: %v", err)
	}
	now = now.Add(1500 * time.Millisecond)
	if err := q.Push(Frame{Type: TypeOutput, Payload: []byte("z")}); !errors.Is(err, ErrTooSlow) {
		t.Fatalf("stayed over: %v, want too slow", err)
	}
	q.Close()
	n := 0
	for {
		if _, ok := q.Pop(); !ok {
			break
		}
		n++
	}
	if n == 0 || q.Len() != 0 {
		t.Fatalf("drained %d frames, %d bytes left", n, q.Len())
	}
	if err := q.Push(Frame{}); !errors.Is(err, ErrQueueClosed) {
		t.Fatalf("push after close: %v", err)
	}
}

func TestQueueWakesWriter(t *testing.T) {
	q := NewQueue(0, 0)
	got := make(chan Frame)
	go func() {
		f, _ := q.Pop()
		got <- f
	}()
	if err := q.Push(Frame{Type: TypeOutput, Payload: []byte("hi")}); err != nil {
		t.Fatal(err)
	}
	select {
	case f := <-got:
		if string(f.Payload) != "hi" {
			t.Fatalf("got %q", f.Payload)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Pop did not wake")
	}
}

func FuzzReader(f *testing.F) {
	var buf bytes.Buffer
	_ = WriteFrame(&buf, TypeHello, []byte(`{"protocol":1}`))
	_ = WriteFrame(&buf, TypeOutput, []byte("abc"))
	f.Add(buf.Bytes())
	f.Add([]byte{5, 0xff, 0xff, 0xff, 0xff})
	f.Fuzz(func(t *testing.T, b []byte) {
		r := NewReader(bytes.NewReader(b))
		for i := 0; i < 1000; i++ {
			fr, err := r.Read()
			if err != nil {
				return
			}
			if len(fr.Payload) > MaxFrame {
				t.Fatalf("payload of %d bytes", len(fr.Payload))
			}
			var h Hello
			_ = fr.Decode(&h)
		}
	})
}
