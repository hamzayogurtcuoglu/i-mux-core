package ipc

import (
	"encoding/json"
	"fmt"
)

// ProtocolVersion is the version of the protocol below. A client and a server with different
// versions do not talk (ARCHITECTURE.md §5).
const ProtocolVersion = 1

// Type is a frame's type.
type Type uint8

// Frame types. Control messages carry JSON; Output and Input carry terminal bytes.
const (
	TypeHello        Type = 1  // client → server: Hello
	TypeCommand      Type = 2  // client → server: Command
	TypeResult       Type = 3  // server → client: Result
	TypeAttach       Type = 4  // server → client: the client is now attached
	TypeOutput       Type = 5  // server → client: terminal bytes to write
	TypeInput        Type = 6  // client → server: terminal bytes read
	TypeResize       Type = 7  // client → server: Size
	TypeDetach       Type = 8  // server → client: End
	TypeExit         Type = 9  // server → client: End
	TypeSuspend      Type = 10 // server → client: stop (SIGTSTP), Unix
	TypeVersionError Type = 11 // server → client: VersionError, then the connection closes
)

func (t Type) valid() bool { return t >= TypeHello && t <= TypeVersionError }

func (t Type) String() string {
	names := [...]string{
		"", "Hello", "Command", "Result", "Attach", "Output", "Input", "Resize",
		"Detach", "Exit", "Suspend", "VersionError",
	}
	if t.valid() {
		return names[t]
	}
	return fmt.Sprintf("Type(%d)", t)
}

// Size is a terminal's size.
type Size struct {
	Cols int `json:"cols"`
	Rows int `json:"rows"`
}

// Hello is the first message of every connection.
type Hello struct {
	Protocol      int               `json:"protocol"`
	ClientVersion string            `json:"client_version"`
	Argv          []string          `json:"argv"`
	Cwd           string            `json:"cwd"`
	Env           map[string]string `json:"env"` // the variables i-mux needs (§5)
	Size          Size              `json:"size"`
	StdinTTY      bool              `json:"stdin_tty"`
	StdoutTTY     bool              `json:"stdout_tty"`
	UTF8          bool              `json:"utf8"` // -u
	Pid           int               `json:"pid"`
}

// Command is a command list from the client: each element is one command's words.
type Command struct {
	Commands [][]string `json:"commands"`
}

// Result is what a command list gave.
type Result struct {
	Stdout string `json:"stdout,omitempty"`
	Stderr string `json:"stderr,omitempty"`
	Code   int    `json:"code"`
}

// End tells a client to detach or exit, and why.
type End struct {
	Reason string `json:"reason,omitempty"`
	Code   int    `json:"code"`
}

// VersionError is the server's answer to a Hello with another protocol version.
type VersionError struct {
	Server int `json:"server"`
}

// Encode makes a frame holding a control message.
func Encode(t Type, msg any) (Frame, error) {
	if msg == nil {
		return Frame{Type: t}, nil
	}
	b, err := json.Marshal(msg)
	if err != nil {
		return Frame{}, err
	}
	return Frame{Type: t, Payload: b}, nil
}

// Decode reads a control message from a frame's payload.
func (f Frame) Decode(msg any) error {
	if err := json.Unmarshal(f.Payload, msg); err != nil {
		return fmt.Errorf("ipc: bad %s: %w", f.Type, err)
	}
	return nil
}

// MismatchError is the error a client reports when the server speaks another protocol
// version. Its text starts "error connecting to", which i-mux classifies as no server.
type MismatchError struct {
	Socket         string
	Client, Server int
}

func (e *MismatchError) Error() string {
	return fmt.Sprintf("error connecting to %s (protocol version %d, server has %d)", e.Socket, e.Client, e.Server)
}

// CheckHello is the server's check of a client's Hello: nil when the versions agree, else the
// VersionError frame to send before closing.
func CheckHello(h *Hello) *Frame {
	if h.Protocol == ProtocolVersion {
		return nil
	}
	f, _ := Encode(TypeVersionError, &VersionError{Server: ProtocolVersion})
	return &f
}
