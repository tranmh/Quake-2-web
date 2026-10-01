package trace

import (
	"sync"
	"sync/atomic"
	"time"
)

// Sink receives every event, in sequence order (the trace file, the
// metrics collector). Sinks are lossless, so they run synchronously: Write
// is called from Publish, in the publisher's goroutine, with the bus
// locked. It must therefore be quick (in-memory work, or a buffered write
// such as FileSink's compressor) and must not call back into the Bus
// (Publish, Subscribe, Close, Err or Subscription.Close would deadlock). A
// slow or lossy consumer belongs on a Subscription instead.
type Sink interface {
	Write(e Event) error
}

// Bus stamps events (v, run, seq, wall) and fans them out: to sinks
// synchronously (see Sink) and to subscribers through bounded channels that
// drop their oldest event when full. Publish never waits for a subscriber;
// it costs what its sinks cost. A Bus is safe for concurrent use.
type Bus struct {
	run string
	now func() time.Time

	mu      sync.Mutex
	seq     uint64
	sinks   []Sink
	subs    []*Subscription
	sinkErr error
	closed  bool
}

// NewBus returns a bus for run; now is the wall clock (nil: time.Now).
func NewBus(run string, now func() time.Time) *Bus {
	if now == nil {
		now = time.Now
	}
	return &Bus{run: run, now: now}
}

// AddSink attaches a synchronous sink.
func (b *Bus) AddSink(s Sink) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.sinks = append(b.sinks, s)
}

// Publish stamps e (Version, the bus's run unless set, the next sequence
// number, the wall clock), hands it to every sink and subscriber and
// returns it. Sinks run under the bus lock (see Sink), so concurrent
// publishers are serialized and every sink sees the events in seq order.
// A sink error is kept (Err) and does not stop the others. Events
// published after Close are dropped.
func (b *Bus) Publish(e Event) Event {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return e
	}
	b.seq++
	e.V = Version
	if e.Run == "" {
		e.Run = b.run
	}
	e.Seq = b.seq
	e.Wall = b.now().UnixMilli()
	for _, s := range b.sinks {
		if err := s.Write(e); err != nil && b.sinkErr == nil {
			b.sinkErr = err
		}
	}
	for _, s := range b.subs {
		s.offer(e)
	}
	return e
}

// Err returns the first sink error.
func (b *Bus) Err() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.sinkErr
}

// Subscribe returns a subscription buffering up to n events (n < 1: 1).
func (b *Bus) Subscribe(n int) *Subscription {
	if n < 1 {
		n = 1
	}
	s := &Subscription{bus: b, c: make(chan Event, n)}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		close(s.c)
		s.done = true
		return s
	}
	b.subs = append(b.subs, s)
	return s
}

// Close closes every subscription's channel; later events are dropped.
func (b *Bus) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	b.closed = true
	for _, s := range b.subs {
		s.done = true
		close(s.c)
	}
	b.subs = nil
}

func (b *Bus) remove(s *Subscription) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if s.done {
		return
	}
	for i, x := range b.subs {
		if x == s {
			b.subs = append(b.subs[:i], b.subs[i+1:]...)
			break
		}
	}
	s.done = true
	close(s.c)
}

// Subscription is a drop-oldest event queue of a Bus.
type Subscription struct {
	bus     *Bus
	c       chan Event
	dropped atomic.Uint64
	done    bool // guarded by bus.mu
}

// offer enqueues e, dropping the oldest queued event when full. It runs
// with the bus locked, so it is the only sender and a freed slot stays free.
func (s *Subscription) offer(e Event) {
	select {
	case s.c <- e:
		return
	default:
	}
	select {
	case <-s.c:
		s.dropped.Add(1)
	default: // the reader just made room
	}
	select {
	case s.c <- e:
	default:
		s.dropped.Add(1)
	}
}

// C returns the event channel; it is closed by Close or Bus.Close.
func (s *Subscription) C() <-chan Event { return s.c }

// Dropped returns how many events were dropped because the reader fell
// behind.
func (s *Subscription) Dropped() uint64 { return s.dropped.Load() }

// Close unsubscribes and closes the channel.
func (s *Subscription) Close() { s.bus.remove(s) }
