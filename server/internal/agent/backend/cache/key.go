package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"quake2web/server/internal/agent/decide"
)

// Key returns req's key under c ("" when off). mapName is the map the
// request was asked on (see MapOf).
func (c Config) Key(req *decide.Request, mapName string) string {
	switch c.Mode {
	case Exact:
		return c.KeyOf(req.Lane, mapName, nil, nil, req.Digest())
	case Approx:
		st := req.View
		if st == nil {
			var s decide.State
			if json.Unmarshal(req.State, &s) != nil {
				return ""
			}
			st = &s
		}
		return c.KeyOf(req.Lane, mapName, st, req.Questions, "")
	}
	return ""
}

// KeyOf is Key for a request given by its parts, as a trace records them:
// its lane, map, lane state, questions and digest (Exact needs only the
// lane and the digest, Approx all but the digest). "" when the parts do
// not make a key.
func (c Config) KeyOf(lane decide.Lane, mapName string, st *decide.State, qs []decide.Question, digest string) string {
	switch c.Mode {
	case Exact:
		if digest == "" {
			return ""
		}
		return "x|" + lane.String() + "|" + digest
	case Approx:
		if st == nil {
			return ""
		}
		sum := sha256.Sum256([]byte(ApproxText(c.Granularity, lane, mapName, st, qs)))
		return "a|" + hex.EncodeToString(sum[:16])
	}
	return ""
}

// MapOf is the map a request was asked on: its snapshot's, else its slow
// lane state's ("" when neither says).
func MapOf(req *decide.Request) string {
	if req.Snapshot != nil {
		return req.Snapshot.Map
	}
	if req.View != nil && req.View.Level != nil {
		return req.View.Level.Map
	}
	return ""
}

// ApproxText is the text the approximate key digests: one line per part,
// readable (see the package documentation for what each field becomes).
func ApproxText(g Granularity, lane decide.Lane, mapName string, st *decide.State, qs []decide.Question) string {
	fine := g == Fine
	var b strings.Builder
	line := func(parts ...string) {
		b.WriteString(strings.Join(parts, " "))
		b.WriteByte('\n')
	}
	line("lane", lane.String(), "map", mapName)
	for _, q := range qs {
		keys := make([]string, len(q.Options))
		for i, o := range q.Options {
			keys[i] = o.Key
		}
		line("q", q.ID, strings.Join(keys, ","))
	}
	if st.Mode != "" {
		line("mode", st.Mode)
	}
	m := &st.Me
	me := []string{"me", "hp=" + m.HP, "armor=" + armorBand(m.Armor, fine), "weapon=" + m.Weapon, "ammo=" + m.Ammo,
		"water=" + flag(m.InWater), "moving=" + m.Moving, "dmg=" + damageBand(m.DamageLast1s, fine),
		"hitfrom=" + optSector(m.HitFromBearing, fine), "since=" + sinceBand(m.TargetSinceS, fine)}
	if fine {
		me = append(me, "ground="+flag(m.OnGround))
	}
	line(me...)
	if len(m.Weapons) > 0 || len(m.Empty) > 0 {
		line("weapons", strings.Join(m.Weapons, ","), "empty", strings.Join(m.Empty, ","))
	}
	for i := range st.Enemies {
		e := &st.Enemies[i]
		heard := e.Heard
		if !fine && heard != "" {
			heard = "yes"
		}
		parts := []string{"enemy", e.ID, e.Class, sector(e.Bearing, fine), e.Dist, "vis=" + flag(e.Visible), "heard=" + heard,
			"shoot=" + flag(e.Shootable), "aim=" + e.Aim, "state=" + e.State, "threat=" + e.Threat, "cur=" + flag(e.Current),
			"obj=" + flag(e.Objective)}
		if fine {
			parts = append(parts, "wounded="+flag(e.Wounded))
		}
		line(parts...)
	}
	for i := range st.Incoming {
		in := &st.Incoming[i]
		bearing := "-"
		if fine {
			bearing = sector(in.Bearing, true)
		}
		line("incoming", in.Kind, bearing, in.ETA, in.Dodge)
	}
	if s := st.Space; s != nil {
		sp := func(v string) string {
			if !fine && v != "open" {
				return "close"
			}
			return v
		}
		line("space", sp(s.Front), sp(s.Back), sp(s.Left), sp(s.Right))
	}
	for i := range st.Items {
		it := &st.Items[i]
		bearing := "-"
		if fine {
			bearing = sector(it.Bearing, true)
		}
		line("item", it.ID, it.Class, it.Gives, bearing, decide.DistBucket(it.Path))
	}
	if o := st.Objective; o != nil {
		path := "unknown"
		if o.Path >= 0 {
			path = decide.DistBucket(o.Path)
		}
		next := "-"
		if fine {
			next = sector(o.Next, true)
		}
		line("objective", o.Kind, strconv.Quote(o.Desc), sector(o.Bearing, fine), path, next, "stalled="+flag(o.Stalled),
			"exit="+flag(o.Exit))
	}
	if lv := st.Level; lv != nil {
		line("level", lv.Map, "deaths="+deathsBand(lv.DeathsHere, fine))
	}
	evs := make([]string, len(st.Events))
	for i, e := range st.Events {
		evs[i] = EventText(e)
	}
	if !fine {
		sort.Strings(evs)
		evs = dedup(evs)
	}
	for _, e := range evs {
		line("event", strconv.Quote(e))
	}
	return b.String()
}

var (
	eventAge    = regexp.MustCompile(` \d+(\.\d+)?s ago$`)
	eventAmount = regexp.MustCompile(`^took \d+ damage`)
)

// EventText is a state event without its age and its amount of damage.
func EventText(e string) string {
	return eventAmount.ReplaceAllString(eventAge.ReplaceAllString(e, ""), "took damage")
}

func dedup(s []string) []string {
	out := s[:0]
	for i, v := range s {
		if i == 0 || v != s[i-1] {
			out = append(out, v)
		}
	}
	return out
}

func flag(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

// sector is a bearing's sector: of 8 (45° each, centered on ahead, the
// diagonals, left, right and behind) when fine, else of 4.
func sector(b int, fine bool) string {
	if fine {
		names := [8]string{"ahead", "ahead-left", "left", "behind-left", "behind", "behind-right", "right", "ahead-right"}
		i := ((b%360+360)%360*2 + 45) / 90 % 8 // nearest multiple of 45 (bearings are whole degrees)
		return names[i]
	}
	switch a := abs(b); {
	case a < 45:
		return "ahead"
	case a > 135:
		return "behind"
	case b > 0:
		return "left"
	}
	return "right"
}

func optSector(b *int, fine bool) string {
	if b == nil {
		return "-"
	}
	return sector(*b, fine)
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func armorBand(a int, fine bool) string {
	switch {
	case a <= 0:
		return "none"
	case !fine:
		return "some"
	case a < 50:
		return "low"
	case a < 100:
		return "mid"
	}
	return "high"
}

func damageBand(d int, fine bool) string {
	switch {
	case d <= 0:
		return "none"
	case !fine:
		return "hit"
	case d < 15:
		return "light"
	case d < 40:
		return "heavy"
	}
	return "severe"
}

func sinceBand(s *float64, fine bool) string {
	switch {
	case s == nil:
		return "-"
	case !fine && *s < 2:
		return "new"
	case !fine:
		return "settled"
	case *s < 1:
		return "<1s"
	case *s < 3:
		return "<3s"
	case *s < 10:
		return "<10s"
	}
	return "long"
}

func deathsBand(n int, fine bool) string {
	switch {
	case n <= 0:
		return "0"
	case !fine:
		return "1+"
	case n >= 3:
		return "3+"
	}
	return strconv.Itoa(n)
}
