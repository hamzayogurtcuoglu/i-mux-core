package ipc

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// Frames are [type u8][length u32, big-endian][payload] (ARCHITECTURE.md §5).

// MaxFrame is the largest payload a frame may carry. A frame over it closes the connection.
const MaxFrame = 1 << 20

// ErrFrameTooLarge is returned for a frame over MaxFrame; the connection must be closed.
var ErrFrameTooLarge = errors.New("ipc: frame too large")

// Frame is one frame.
type Frame struct {
	Type    Type
	Payload []byte
}

// WriteFrame writes one frame.
func WriteFrame(w io.Writer, t Type, payload []byte) error {
	if len(payload) > MaxFrame {
		return ErrFrameTooLarge
	}
	var hdr [5]byte
	hdr[0] = byte(t)
	binary.BigEndian.PutUint32(hdr[1:], uint32(len(payload)))
	// One write per frame, so frames from several goroutines never interleave on a writer that
	// serializes writes.
	buf := make([]byte, 0, len(hdr)+len(payload))
	buf = append(append(buf, hdr[:]...), payload...)
	_, err := w.Write(buf)
	return err
}

// Reader reads frames.
type Reader struct {
	r *bufio.Reader
}

// NewReader returns a frame reader on r.
func NewReader(r io.Reader) *Reader { return &Reader{r: bufio.NewReaderSize(r, 64<<10)} }

// Read reads the next frame. It returns io.EOF at a clean end between frames,
// io.ErrUnexpectedEOF inside one, and ErrFrameTooLarge for a frame over MaxFrame.
func (r *Reader) Read() (Frame, error) {
	var hdr [5]byte
	if _, err := io.ReadFull(r.r, hdr[:]); err != nil {
		return Frame{}, err
	}
	n := binary.BigEndian.Uint32(hdr[1:])
	if n > MaxFrame {
		return Frame{}, ErrFrameTooLarge
	}
	t := Type(hdr[0])
	if !t.valid() {
		return Frame{}, fmt.Errorf("ipc: unknown frame type %d", hdr[0])
	}
	payload := make([]byte, n)
	if _, err := io.ReadFull(r.r, payload); err != nil {
		if errors.Is(err, io.EOF) {
			err = io.ErrUnexpectedEOF
		}
		return Frame{}, err
	}
	return Frame{Type: t, Payload: payload}, nil
}
