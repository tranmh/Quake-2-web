package cache

import (
	"container/list"
	"encoding/json"
	"sync"
	"time"

	"quake2web/server/internal/agent/decide"
)

// Entry is a kept answer.
type Entry struct {
	// Raw is the backend's response body, Answers its decoded answers and
	// Model the model it named.
	Raw     json.RawMessage
	Answers map[string]decide.Answer
	Model   string
	// InputTokens and CostUSD are what the answer cost (what reusing it
	// saves).
	InputTokens int64
	CostUSD     float64
	// Ep and SnapTime are the episode and the snapshot time (session ms)
	// of the request it answered.
	Ep       int
	SnapTime int64

	key string
}

// EntryOf returns the entry to keep for resp, the answer to a request of
// episode ep asked at snap with questions qs: nil unless resp answers
// every question and is the backend's own (not a reused answer).
func EntryOf(ep int, snap int64, qs []decide.Question, resp *decide.Response) *Entry {
	if resp == nil || resp.Cache != "" || len(qs) == 0 {
		return nil
	}
	for i := range qs {
		if _, ok := resp.Answers[qs[i].ID]; !ok {
			return nil
		}
	}
	return &Entry{Raw: resp.Raw, Answers: resp.Answers, Model: resp.Model, InputTokens: resp.Usage.InputTokens, CostUSD: resp.CostUSD,
		Ep: ep, SnapTime: snap}
}

// Store keeps a run's answers by key. It is safe for concurrent use; a
// lockstep run calls it from its control loop only, in game-time order.
type Store struct {
	cfg Config

	mu      sync.Mutex
	entries map[string]*list.Element // of *Entry
	lru     *list.List               // most recently used first
	// asked counts the requests of a key asked and not answered yet
	asked   map[string]int
	evicted int
}

// NewStore returns an empty store.
func NewStore(cfg Config) *Store {
	return &Store{cfg: cfg, entries: map[string]*list.Element{}, lru: list.New(), asked: map[string]int{}}
}

// Config returns the store's configuration.
func (s *Store) Config() Config { return s.cfg }

// usable reports whether e may answer a request of episode ep at session
// time at (Config.TTL).
func (s *Store) usable(e *Entry, ep int, at int64) bool {
	if s.cfg.TTL <= 0 {
		return true
	}
	return e.Ep == ep && time.Duration(at-e.SnapTime)*time.Millisecond <= s.cfg.TTL
}

// Lookup returns the entry that answers a request with key asked in
// episode ep at session time at (nil: a miss; an entry past the TTL is
// dropped). For a miss, inflight reports another request of the key asked
// and not answered yet: its answer was not back in time.
func (s *Store) Lookup(key string, ep int, at int64) (e *Entry, inflight bool) {
	if key == "" {
		return nil, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if el, ok := s.entries[key]; ok {
		e := el.Value.(*Entry)
		if s.usable(e, ep, at) {
			s.lru.MoveToFront(el)
			return e, false
		}
		s.lru.Remove(el)
		delete(s.entries, key)
	}
	return nil, s.asked[key] > 0
}

// Asked notes a request of key sent to the backend (after a miss).
func (s *Store) Asked(key string) {
	if key == "" {
		return
	}
	s.mu.Lock()
	s.asked[key]++
	s.mu.Unlock()
}

// Answered notes the result of a request Asked: e is the entry to keep
// (EntryOf; nil: none). A newer answer replaces the key's older one.
func (s *Store) Answered(key string, e *Entry) {
	if key == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if n := s.asked[key]; n > 1 {
		s.asked[key] = n - 1
	} else {
		delete(s.asked, key)
	}
	if e == nil {
		return
	}
	e.key = key
	if el, ok := s.entries[key]; ok {
		el.Value = e
		s.lru.MoveToFront(el)
		return
	}
	s.entries[key] = s.lru.PushFront(e)
	for s.lru.Len() > s.cfg.maxEntries() {
		old := s.lru.Back()
		s.lru.Remove(old)
		delete(s.entries, old.Value.(*Entry).key)
		s.evicted++
	}
}

// Len is the number of entries kept; Evicted those dropped for room.
func (s *Store) Len() (n, evicted int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lru.Len(), s.evicted
}
