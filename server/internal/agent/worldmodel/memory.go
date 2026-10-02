package worldmodel

import (
	"sort"

	"quake2web/server/internal/agent/mapdata"
	"quake2web/server/internal/agent/perception"
	"quake2web/server/internal/qcommon/shared"
)

// KnownItem is an item the bot saw on the level: its class and spot (items
// come back when the level-entry save is reloaded).
type KnownItem struct {
	Class string
	Pos   Vec3
	Lump  int // -1 if not matched to the entity lump
}

// LevelMemory is what the bot keeps about one visit of a level across
// reloads of its entry save (save0): only static learnings, because a
// reload restores the level's dynamic state (monsters, doors, items) to
// how it was at entry.
type LevelMemory struct {
	Key LevelKey
	// Entries counts how often the level was entered (1 + reloads): the
	// level generations the world model saw under this key.
	Entries int
	// DeathSpots are where the bot died.
	DeathSpots []Vec3
	// Blocked are navigation edges found blocked (opaque keys).
	Blocked []string
	// Items are the items seen, in the order they were first seen.
	Items []KnownItem
	// Entry is the bot's state the first time it entered: what the
	// level-entry save restores.
	Entry EntryState

	gen int // the level generation last counted
}

// EntryState is the bot's own state at a level entry.
type EntryState struct {
	Set    bool
	Origin Vec3
	Health int
	Armor  int
	Weapon string
}

func (m *LevelMemory) addDeath(p Vec3) {
	if m.Key.Map != "" {
		m.DeathSpots = append(m.DeathSpots, p)
	}
}

func (m *LevelMemory) addBlocked(edge string) {
	i := sort.SearchStrings(m.Blocked, edge)
	if i < len(m.Blocked) && m.Blocked[i] == edge {
		return
	}
	m.Blocked = append(m.Blocked, "")
	copy(m.Blocked[i+1:], m.Blocked[i:])
	m.Blocked[i] = edge
}

func (m *LevelMemory) addItem(k KnownItem) {
	if m.Key.Map == "" {
		return
	}
	for _, it := range m.Items {
		if it.Class == k.Class && dist(it.Pos, k.Pos) < 16 {
			return
		}
	}
	m.Items = append(m.Items, k)
}

func (m *LevelMemory) view() MemoryView {
	return MemoryView{Entries: m.Entries,
		DeathSpots: append([]Vec3(nil), m.DeathSpots...), Blocked: append([]string(nil), m.Blocked...)}
}

// lumpMatch ties entity numbers to lump entities for the current level.
type lumpMatch struct {
	numToLump  map[int32]int
	lumpToNum  map[int]int32
	baselines  map[int32]Vec3 // spawn origin by entity number
	baseAngles map[int32]Vec3
}

// matchLump ties the level's lump entities to entity numbers by class and
// spawn origin. An entity's baseline is its state two frames after the
// level started: items dropped to the floor (their x and y match the lump
// exactly, z lies at most a drop below), monsters dropped too and may have
// taken their first steps. Pairs are assigned nearest first. No frame state
// is used. The baselines cover every entity, seen or not (svc_spawnbaseline),
// so a track takes its lump entity from this match only once it is seen
// (actorFor, seeActor): which of a family's monsters a sound comes from is
// not something a player can tell.
func matchLump(m *mapdata.Map, baselines []shared.EntityState, cls *perception.Classifier) lumpMatch {
	lm := lumpMatch{numToLump: map[int32]int{}, lumpToNum: map[int]int32{}}
	byClassname := map[string][]int{}
	for i := range m.Entities {
		e := &m.Entities[i]
		if e.Present() && !e.Brush() {
			byClassname[e.Classname] = append(byClassname[e.Classname], i)
		}
	}
	type pair struct {
		d    float32
		num  int32
		lump int
	}
	var pairs []pair
	for i := range baselines {
		b := &baselines[i]
		c := cls.Classify(b).Class
		if c == nil {
			continue
		}
		slack := float32(2) // items, lasers, barrels: only the drop to the floor
		if c.Kind == perception.KindMonster || c.Kind == perception.KindNeutral {
			slack = 32 // two frames of walking
		}
		for _, cn := range c.Classnames {
			for _, li := range byClassname[cn] {
				o := m.Entities[li].Origin
				dx, dy, dz := b.Origin[0]-o[0], b.Origin[1]-o[1], b.Origin[2]-o[2]
				if dx < -slack || dx > slack || dy < -slack || dy > slack || dz > 8 || dz < -264 {
					continue
				}
				pairs = append(pairs, pair{d: dist(b.Origin, o), num: b.Number, lump: li})
			}
		}
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].d != pairs[j].d {
			return pairs[i].d < pairs[j].d
		}
		if pairs[i].num != pairs[j].num {
			return pairs[i].num < pairs[j].num
		}
		return pairs[i].lump < pairs[j].lump
	})
	for _, p := range pairs {
		if _, ok := lm.numToLump[p.num]; ok {
			continue
		}
		if _, ok := lm.lumpToNum[p.lump]; ok {
			continue
		}
		lm.numToLump[p.num] = p.lump
		lm.lumpToNum[p.lump] = p.num
	}
	return lm
}

// restoreItems seeds the items remembered from earlier attempts at this
// level: after a reload they are back where they were seen.
func (w *World) restoreItems() {
	for _, k := range w.mem.Items {
		c := w.classes.ByName(k.Class)
		it := &itemTrack{idx: len(w.items)}
		it.ID = w.newID('i')
		it.Class, it.Pos, it.Lump = k.Class, k.Pos, k.Lump
		if c != nil {
			it.Kind, it.Pickup, it.Value, it.Amount = c.Item.String(), c.Pickup, c.Value, c.Amount
		}
		it.Remembered = true
		if n, ok := w.match.lumpToNum[k.Lump]; ok && k.Lump >= 0 {
			if _, taken := w.bind[n]; !taken {
				it.Num, it.bound = n, true
				w.bind[n] = binding{kind: bindItem, idx: it.idx}
			}
		}
		w.items = append(w.items, it)
	}
}

// initLasers registers the level's target_lasers from the static map data.
func (w *World) initLasers() {
	if w.level.Map == nil {
		return
	}
	for _, l := range w.level.Map.Lasers {
		las := &Laser{Lump: l.Entity, Start: l.Start, End: l.End}
		if n, ok := w.match.lumpToNum[l.Entity]; ok {
			las.Num = n
		}
		w.lasers = append(w.lasers, las)
	}
}
