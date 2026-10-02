package runner

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"sync/atomic"

	"quake2web/server/internal/agent/trace"
)

// fileRouter is the bus sink that writes the events to the current
// episode's trace file (ep-NNN/trace.jsonl.gz).
type fileRouter struct {
	mu  sync.Mutex
	cur *trace.FileSink
}

func (r *fileRouter) Write(e trace.Event) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cur == nil {
		return nil
	}
	return r.cur.Write(e)
}

// swap makes next the current file (nil: none) and closes the previous
// one.
func (r *fileRouter) swap(next *trace.FileSink) error {
	r.mu.Lock()
	old := r.cur
	r.cur = next
	r.mu.Unlock()
	if old != nil {
		return old.Close()
	}
	return nil
}

// stamp is the envelope of the events the runner publishes itself (the
// decision layer's and the session's): the level the campaign announced
// last. It is a bus sink watching level_start; its fields are read from
// the run's goroutine.
type stamp struct {
	mu  sync.Mutex
	lvl int
	m   string
}

func (s *stamp) Write(e trace.Event) error {
	switch e.Type {
	case trace.TypeEpisodeStart:
		s.mu.Lock()
		s.lvl, s.m = 0, ""
		s.mu.Unlock()
	case trace.TypeLevelStart:
		s.mu.Lock()
		s.lvl, s.m = e.Lvl, e.Map
		s.mu.Unlock()
	}
	return nil
}

func (s *stamp) get() (int, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lvl, s.m
}

// episodeLog is an episode's log.txt: lines stamped with the session's
// game time, also forwarded to the run's Logf when verbose. It is safe for
// concurrent use (the jev client logs from its calls' goroutines).
type episodeLog struct {
	mu   sync.Mutex
	w    *bufio.Writer
	f    io.Closer
	gms  *atomic.Int64
	fwd  func(format string, args ...any)
	err  error
	tag  string
	open bool
}

func createLog(path, tag string, gms *atomic.Int64, fwd func(string, ...any)) (*episodeLog, error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	return &episodeLog{w: bufio.NewWriter(f), f: f, gms: gms, fwd: fwd, tag: tag, open: true}, nil
}

// discardLog is a log that writes nowhere (a replay without an output
// directory) but still forwards.
func discardLog(tag string, gms *atomic.Int64, fwd func(string, ...any)) *episodeLog {
	return &episodeLog{gms: gms, fwd: fwd, tag: tag}
}

func (l *episodeLog) logf(format string, args ...any) {
	msg := strings.TrimRight(fmt.Sprintf(format, args...), "\n")
	ms := l.gms.Load()
	l.mu.Lock()
	if l.open && l.err == nil {
		for _, line := range strings.Split(msg, "\n") {
			if _, err := fmt.Fprintf(l.w, "[%7.1fs] %s\n", float64(ms)/1000, line); err != nil {
				l.err = err
				break
			}
		}
	}
	l.mu.Unlock()
	if l.fwd != nil {
		l.fwd("%s %s", l.tag, msg)
	}
}

func (l *episodeLog) close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.open {
		return l.err
	}
	l.open = false
	if err := l.w.Flush(); err != nil && l.err == nil {
		l.err = err
	}
	if err := l.f.Close(); err != nil && l.err == nil {
		l.err = err
	}
	return l.err
}
