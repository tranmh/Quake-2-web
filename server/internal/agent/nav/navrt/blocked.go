package navrt

import (
	"fmt"
	"sort"

	"quake2web/server/internal/agent/nav"
)

// Blocking backoff: the k-th time (from 0) an edge is marked blocked it
// is left out for BlockBase·2^k, at most BlockMax.
const (
	BlockBase = 15000  // ms
	BlockMax  = 120000 // ms
)

// EdgeKey is the stable name of edge e for the level memory
// (worldmodel.World.MarkBlocked): "<from>><to>/<kind>".
func EdgeKey(e *nav.Edge) string { return fmt.Sprintf("%d>%d/%s", e.From, e.To, e.Kind) }

type blockEntry struct {
	until int64
	count int
	// related are the blockers whose belief decides about the edge, with
	// their MapState revision when it was marked: a change of any of them
	// clears the mark.
	related []int32
	revs    []uint32
}

// Blocked is the set of edges the navigator found blocked, each left out
// for a while with exponential backoff. A mark is cleared early when the
// belief about a related blocker changes (the door that was in the way
// opened). It is not safe for concurrent use.
type Blocked struct {
	m map[int]*blockEntry
}

// NewBlocked returns an empty set.
func NewBlocked() *Blocked { return &Blocked{m: map[int]*blockEntry{}} }

// Mark marks edge i blocked from now (ms) on and returns until when. The
// related blockers' revisions are taken from ms.
func (b *Blocked) Mark(i int, related []int32, ms *MapState, now int64) int64 {
	en := b.m[i]
	if en == nil {
		en = &blockEntry{}
		b.m[i] = en
	}
	d := int64(BlockBase)
	for k := 0; k < en.count && d < BlockMax; k++ {
		d *= 2
	}
	d = min(d, BlockMax)
	en.count++
	en.until = now + d
	en.related = append(en.related[:0], related...)
	en.revs = en.revs[:0]
	for _, r := range related {
		var rev uint32
		if ms != nil {
			rev = ms.Revision(r)
		}
		en.revs = append(en.revs, rev)
	}
	return en.until
}

// Active reports whether edge i is blocked at now.
func (b *Blocked) Active(i int, now int64) bool {
	en := b.m[i]
	return en != nil && now < en.until
}

// Until returns when edge i's mark ends (0 when never marked).
func (b *Blocked) Until(i int) int64 {
	if en := b.m[i]; en != nil {
		return en.until
	}
	return 0
}

// Count returns how often edge i was marked since it was last cleared.
func (b *Blocked) Count(i int) int {
	if en := b.m[i]; en != nil {
		return en.count
	}
	return 0
}

// Refresh clears the marks whose related blockers changed in ms since
// they were set, and returns the edges cleared (sorted).
func (b *Blocked) Refresh(ms *MapState) []int {
	var out []int
	for i, en := range b.m {
		for k, r := range en.related {
			if ms.Revision(r) != en.revs[k] {
				out = append(out, i)
				break
			}
		}
	}
	for _, i := range out {
		delete(b.m, i)
	}
	sort.Ints(out)
	return out
}

// Edges returns the edges marked blocked at now (sorted).
func (b *Blocked) Edges(now int64) []int {
	var out []int
	for i, en := range b.m {
		if now < en.until {
			out = append(out, i)
		}
	}
	sort.Ints(out)
	return out
}
