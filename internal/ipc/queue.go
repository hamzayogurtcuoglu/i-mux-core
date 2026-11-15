package ipc

import (
	"errors"
	"sync"
	"time"
)

// Back-pressure for an attached client's output (ARCHITECTURE.md §5, §7).

// Default queue limits.
const (
	DefaultQueueCap   = 4 << 20
	DefaultQueueGrace = time.Second
)

// ErrTooSlow means the client stayed over its queue's cap for longer than the grace period;
// the server detaches it with the reason "too slow".
var ErrTooSlow = errors.New("too slow")

// ErrQueueClosed is returned once the queue is closed.
var ErrQueueClosed = errors.New("ipc: queue closed")

// Queue holds frames for a client's writer goroutine.
type Queue struct {
	mu     sync.Mutex
	cond   *sync.Cond
	frames []Frame
	bytes  int
	cap    int
	grace  time.Duration
	over   time.Time // when the queue went over the cap; zero while under it
	closed bool
	now    func() time.Time
}

// NewQueue returns a queue with a byte cap and a grace period (zero values: the defaults).
func NewQueue(cap int, grace time.Duration) *Queue {
	if cap <= 0 {
		cap = DefaultQueueCap
	}
	if grace <= 0 {
		grace = DefaultQueueGrace
	}
	q := &Queue{cap: cap, grace: grace, now: time.Now}
	q.cond = sync.NewCond(&q.mu)
	return q
}

// Push adds a frame. It fails with ErrTooSlow when the queue has been over its cap for longer
// than the grace period; the frame is then dropped.
func (q *Queue) Push(f Frame) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return ErrQueueClosed
	}
	if q.bytes > q.cap {
		if q.over.IsZero() {
			q.over = q.now()
		} else if q.now().Sub(q.over) > q.grace {
			return ErrTooSlow
		}
	}
	q.frames = append(q.frames, f)
	q.bytes += len(f.Payload)
	q.cond.Signal()
	return nil
}

// Pop waits for the next frame. It returns false once the queue is closed and empty.
func (q *Queue) Pop() (Frame, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for len(q.frames) == 0 && !q.closed {
		q.cond.Wait()
	}
	if len(q.frames) == 0 {
		return Frame{}, false
	}
	f := q.frames[0]
	q.frames[0] = Frame{}
	q.frames = q.frames[1:]
	q.bytes -= len(f.Payload)
	if q.bytes <= q.cap {
		q.over = time.Time{}
	}
	return f, true
}

// Len returns the bytes queued.
func (q *Queue) Len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.bytes
}

// Close ends the queue: Pop returns what is left, then false.
func (q *Queue) Close() {
	q.mu.Lock()
	q.closed = true
	q.cond.Broadcast()
	q.mu.Unlock()
}
