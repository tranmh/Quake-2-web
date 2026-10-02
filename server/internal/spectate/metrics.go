package spectate

import (
	"sync"
	"sync/atomic"
)

// ResyncReason says why a viewer was sent a keyframe.
type ResyncReason string

// Keyframe reasons.
const (
	ResyncJoin   ResyncReason = "join"   // the first keyframe after "begin"
	ResyncDelta  ResyncReason = "delta"  // the bot's frame deltas from a frame the viewer was not sent
	ResyncDrop   ResyncReason = "drop"   // the viewer's send queue dropped a datagram
	ResyncFeed   ResyncReason = "feed"   // bot messages were lost on the hub's feed
	ResyncClient ResyncReason = "client" // the viewer reported an invalid frame (lastframe -1)
)

// DropReason says what was dropped.
type DropReason string

// Drop reasons.
const (
	DropQueue DropReason = "queue" // a datagram to a viewer: its send queue was full
	DropInbox DropReason = "inbox" // a datagram from a viewer: the hub's inbox was full
	DropRate  DropReason = "rate"  // a datagram from a viewer over its packet rate
	DropFeed  DropReason = "feed"  // a bot message: the hub's feed was full
	DropSpan  DropReason = "span"  // a command (or keyframe) too large for a datagram; a keyframe is retried
)

// LeaveReason says why a viewer left.
type LeaveReason string

// Leave reasons.
const (
	LeaveDisconnect LeaveReason = "disconnect" // the viewer sent "disconnect"
	LeaveTimeout    LeaveReason = "timeout"    // nothing valid received for HubConfig.Timeout
	LeaveSlow       LeaveReason = "slow"       // its send queue overflowed too often, or stopped draining
	LeaveProtocol   LeaveReason = "protocol"   // malformed or unexpected client data
	LeaveOverflow   LeaveReason = "overflow"   // its reliable stream overflowed
	LeaveConn       LeaveReason = "conn"       // the transport ended
	LeaveClosed     LeaveReason = "closed"     // the hub was closed
)

// Metrics receives the relay's events, for counters such as Prometheus
// ones. Implementations must be safe for concurrent use and quick: they are
// called from the hub goroutine and the connection goroutines.
type Metrics interface {
	ViewerJoined()
	ViewerLeft(reason LeaveReason)
	DatagramIn()
	DatagramOut()
	Resync(reason ResyncReason)
	Drop(reason DropReason, n int)
}

// Counters is a Metrics that counts everything; Hub.Stats reads one.
type Counters struct {
	viewers, joins, in, out atomic.Int64

	mu      sync.Mutex
	resyncs map[ResyncReason]uint64
	drops   map[DropReason]uint64
	leaves  map[LeaveReason]uint64
}

// Stats is a snapshot of Counters.
type Stats struct {
	Viewers      int // viewers currently connected
	Joins        uint64
	DatagramsIn  uint64
	DatagramsOut uint64
	Resyncs      map[ResyncReason]uint64
	Drops        map[DropReason]uint64
	Leaves       map[LeaveReason]uint64
}

// ViewerJoined implements Metrics.
func (c *Counters) ViewerJoined() { c.viewers.Add(1); c.joins.Add(1) }

// ViewerLeft implements Metrics.
func (c *Counters) ViewerLeft(r LeaveReason) {
	c.viewers.Add(-1)
	c.mu.Lock()
	if c.leaves == nil {
		c.leaves = map[LeaveReason]uint64{}
	}
	c.leaves[r]++
	c.mu.Unlock()
}

// DatagramIn implements Metrics.
func (c *Counters) DatagramIn() { c.in.Add(1) }

// DatagramOut implements Metrics.
func (c *Counters) DatagramOut() { c.out.Add(1) }

// Resync implements Metrics.
func (c *Counters) Resync(r ResyncReason) {
	c.mu.Lock()
	if c.resyncs == nil {
		c.resyncs = map[ResyncReason]uint64{}
	}
	c.resyncs[r]++
	c.mu.Unlock()
}

// Drop implements Metrics.
func (c *Counters) Drop(r DropReason, n int) {
	c.mu.Lock()
	if c.drops == nil {
		c.drops = map[DropReason]uint64{}
	}
	c.drops[r] += uint64(n)
	c.mu.Unlock()
}

// Snapshot returns the current counts.
func (c *Counters) Snapshot() Stats {
	s := Stats{
		Viewers:      int(c.viewers.Load()),
		Joins:        uint64(c.joins.Load()),
		DatagramsIn:  uint64(c.in.Load()),
		DatagramsOut: uint64(c.out.Load()),
		Resyncs:      map[ResyncReason]uint64{},
		Drops:        map[DropReason]uint64{},
		Leaves:       map[LeaveReason]uint64{},
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for k, v := range c.resyncs {
		s.Resyncs[k] = v
	}
	for k, v := range c.drops {
		s.Drops[k] = v
	}
	for k, v := range c.leaves {
		s.Leaves[k] = v
	}
	return s
}

// multiMetrics fans events out to several Metrics.
type multiMetrics []Metrics

func (m multiMetrics) ViewerJoined() {
	for _, x := range m {
		x.ViewerJoined()
	}
}

func (m multiMetrics) ViewerLeft(r LeaveReason) {
	for _, x := range m {
		x.ViewerLeft(r)
	}
}

func (m multiMetrics) DatagramIn() {
	for _, x := range m {
		x.DatagramIn()
	}
}

func (m multiMetrics) DatagramOut() {
	for _, x := range m {
		x.DatagramOut()
	}
}

func (m multiMetrics) Resync(r ResyncReason) {
	for _, x := range m {
		x.Resync(r)
	}
}

func (m multiMetrics) Drop(r DropReason, n int) {
	for _, x := range m {
		x.Drop(r, n)
	}
}

// ViewerLimit caps the number of viewers across hubs (HubConfig.Global).
// It is safe for concurrent use.
type ViewerLimit struct {
	max int64
	n   atomic.Int64
}

// NewViewerLimit returns a limit of max viewers (max <= 0: unlimited).
func NewViewerLimit(max int) *ViewerLimit { return &ViewerLimit{max: int64(max)} }

// InUse returns the number of viewer slots taken.
func (l *ViewerLimit) InUse() int { return int(l.n.Load()) }

func (l *ViewerLimit) acquire() bool {
	if l == nil {
		return true
	}
	if n := l.n.Add(1); l.max > 0 && n > l.max {
		l.n.Add(-1)
		return false
	}
	return true
}

func (l *ViewerLimit) release() {
	if l != nil {
		l.n.Add(-1)
	}
}
