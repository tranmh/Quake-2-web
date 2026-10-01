package decide

import (
	"math"
	"sort"
	"time"
)

// LatencyStats keeps the latest latencies in a window and answers
// quantiles over it (nearest rank). It is not safe for concurrent use.
type LatencyStats struct {
	buf   []time.Duration
	next  int
	full  bool
	total int

	sorted []time.Duration
	dirty  bool
}

// DefaultLatencyWindow is the number of latencies a window keeps.
const DefaultLatencyWindow = 256

// NewLatencyStats returns stats over the last window latencies
// (DefaultLatencyWindow if <= 0).
func NewLatencyStats(window int) *LatencyStats {
	if window <= 0 {
		window = DefaultLatencyWindow
	}
	return &LatencyStats{buf: make([]time.Duration, window)}
}

// Add records a latency.
func (l *LatencyStats) Add(d time.Duration) {
	l.buf[l.next] = d
	l.next++
	if l.next == len(l.buf) {
		l.next, l.full = 0, true
	}
	l.total++
	l.dirty = true
}

// Count is the number of latencies in the window.
func (l *LatencyStats) Count() int {
	if l.full {
		return len(l.buf)
	}
	return l.next
}

// Total is the number of latencies ever added.
func (l *LatencyStats) Total() int { return l.total }

// Quantile returns the q-quantile (0..1) of the window, 0 when empty.
func (l *LatencyStats) Quantile(q float64) time.Duration {
	n := l.Count()
	if n == 0 {
		return 0
	}
	if l.dirty {
		l.sorted = append(l.sorted[:0], l.buf[:n]...)
		sort.Slice(l.sorted, func(i, j int) bool { return l.sorted[i] < l.sorted[j] })
		l.dirty = false
	}
	i := int(math.Ceil(q*float64(n)-1e-9)) - 1 // nearest rank
	if i < 0 {
		i = 0
	}
	if i >= n {
		i = n - 1
	}
	return l.sorted[i]
}

// LatencyModel gives the simulated latency of a request in lockstep runs.
// It must depend only on the request, so runs repeat.
type LatencyModel interface {
	Latency(req *Request) time.Duration
}

// FixedLatency is a constant simulated latency.
type FixedLatency time.Duration

// Latency implements LatencyModel.
func (f FixedLatency) Latency(*Request) time.Duration { return time.Duration(f) }

// SampledLatency draws simulated latencies from Samples (a recorded latency
// list, say): the sample of a request is chosen by a hash of Seed and its
// Seq, so it does not depend on the order of calls.
type SampledLatency struct {
	Seed    uint64
	Samples []time.Duration
}

// Latency implements LatencyModel.
func (s *SampledLatency) Latency(req *Request) time.Duration {
	if len(s.Samples) == 0 {
		return 0
	}
	return s.Samples[Mix64(s.Seed^Mix64(req.Seq))%uint64(len(s.Samples))]
}

// Mix64 is the splitmix64 finalizer: a fast, well-mixed 64-bit hash for
// deterministic seeding.
func Mix64(x uint64) uint64 {
	x += 0x9e3779b97f4a7c15
	x = (x ^ (x >> 30)) * 0xbf58476d1ce4e5b9
	x = (x ^ (x >> 27)) * 0x94d049bb133111eb
	return x ^ (x >> 31)
}
