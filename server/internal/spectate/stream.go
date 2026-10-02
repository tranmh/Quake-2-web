package spectate

import (
	"sync"

	"quake2web/server/internal/fakeclient"
)

// Sink receives every server message the bot accepted, synchronously in the
// bot's goroutine, with the client state as it is right after parsing (the
// fakeclient OnServerMessage contract). *demo.Recorder is a Sink. A Sink
// must be quick, must not block and must not modify payload or spans (they
// are shared with the other sinks and the hubs).
type Sink interface {
	OnServerMessage(c *fakeclient.Client, payload []byte, spans []fakeclient.Span)
}

// SinkFunc adapts a function to Sink.
type SinkFunc func(c *fakeclient.Client, payload []byte, spans []fakeclient.Span)

// OnServerMessage implements Sink.
func (f SinkFunc) OnServerMessage(c *fakeclient.Client, payload []byte, spans []fakeclient.Span) {
	f(c, payload, spans)
}

// DefaultFeedDepth is the number of bot messages a hub's feed buffers
// (about 100 s of a realtime bot, a fraction of a second in lockstep).
const DefaultFeedDepth = 1024

// Stream is the bot side of spectating: plug OnServerMessage into the bot's
// fakeclient.Options (or call it from the hook there). Each message updates
// the Mirror, is offered to every subscribed hub without blocking and is
// then passed to the synchronous sinks in the order they were added.
//
// OnServerMessage must be called from one goroutine at a time (the bot's);
// AddSink, Level, Frame and the hub subscriptions are safe for concurrent
// use.
type Stream struct {
	mu     sync.Mutex
	mirror Mirror
	seq    uint64
	sinks  []Sink
	subs   []*subscription
}

// NewStream returns a stream passing every message to sinks.
func NewStream(sinks ...Sink) *Stream {
	return &Stream{sinks: append([]Sink(nil), sinks...)}
}

// AddSink appends a synchronous sink.
func (s *Stream) AddSink(k Sink) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sinks = append(append([]Sink(nil), s.sinks...), k)
}

// OnServerMessage is the fakeclient.Options.OnServerMessage hook.
func (s *Stream) OnServerMessage(c *fakeclient.Client, payload []byte, spans []fakeclient.Span) {
	s.mu.Lock()
	m := s.mirror.Update(c, payload, spans)
	s.seq++
	m.Seq = s.seq
	for _, sub := range s.subs {
		sub.offer(m)
	}
	sinks := s.sinks
	s.mu.Unlock()
	for _, k := range sinks {
		k.OnServerMessage(c, payload, spans)
	}
}

// Level returns the mirrored level of the bot (nil before its first
// serverdata).
func (s *Stream) Level() *LevelSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.mirror.Level()
}

// Frame returns the bot's latest valid frame on the current level (nil if
// none yet).
func (s *Stream) Frame() *FrameSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.mirror.Frame()
}

// delivery is a message as one subscriber receives it: lost counts the
// messages dropped for this subscriber since the previous delivery.
type delivery struct {
	m    *Message
	lost uint64
}

// subscription is a hub's bounded feed. lost is guarded by the Stream's
// mutex.
type subscription struct {
	ch   chan delivery
	lost uint64
}

// offer queues m without blocking; a full feed drops it and counts it.
func (sub *subscription) offer(m *Message) {
	select {
	case sub.ch <- delivery{m: m, lost: sub.lost}:
		sub.lost = 0
	default:
		sub.lost++
	}
}

// subscribe returns a new feed of depth messages. Its first delivery is the
// current mirrored state (a Message without payload), so a hub attached in
// the middle of a level starts from the bot's state even while the bot is
// paused.
func (s *Stream) subscribe(depth int) *subscription {
	if depth <= 0 {
		depth = DefaultFeedDepth
	}
	sub := &subscription{ch: make(chan delivery, depth+1)}
	s.mu.Lock()
	defer s.mu.Unlock()
	sub.ch <- delivery{m: &Message{
		Seq:       s.seq,
		FrameSpan: -1,
		Level:     s.mirror.Level(),
		Frame:     s.mirror.Frame(),
	}}
	s.subs = append(append([]*subscription(nil), s.subs...), sub)
	return sub
}

// unsubscribe removes a feed; no message is offered to it afterwards.
func (s *Stream) unsubscribe(sub *subscription) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, x := range s.subs {
		if x == sub {
			s.subs = append(append([]*subscription(nil), s.subs[:i]...), s.subs[i+1:]...)
			return
		}
	}
}
