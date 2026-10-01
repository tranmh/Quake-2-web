// Package mapdata extracts the static map knowledge the agent plans with
// from a BSP file: the entity lump parsed and filtered exactly like
// SpawnEntities, mover poses derived exactly like the g_func.c spawn
// functions, trigger volumes, exits, laser segments, spawn points, items,
// monsters and the entity logic graph (who fires whom).
//
// It reads the ported packages (bsp, cmodel, qcommon/shared) but never
// imports package game: the few game helpers it needs (G_SetMovedir,
// ED_NewString, the spawn-field conversions) are copied here with their C
// references, and the cross-check test compares the result with a real game
// spawn driven by gametest.
//
// A Map is immutable after Load and safe for concurrent use.
package mapdata

import (
	"fmt"
	"math"
	"sort"

	"quake2web/server/internal/bsp"
	"quake2web/server/internal/cmodel"
	"quake2web/server/internal/qcommon/shared"
)

// Vec3 is the game's vec3_t.
type Vec3 = shared.Vec3

// FRAMETIME is the server frame length in seconds (a double in C).
// C: game/g_local.h:73 FRAMETIME
const FRAMETIME = 0.1

// Options are the server settings that change what the game spawns.
type Options struct {
	// Skill is the skill cvar; SpawnEntities clamps it to 0..3.
	Skill int
	// Deathmatch selects the deathmatch inhibit filter (NOT_DEATHMATCH only)
	// and the deathmatch-only spawn changes (door speed, removals).
	Deathmatch bool
	// Coop keeps the info_player_coop spots (it inhibits nothing: the coop
	// test is commented out in SpawnEntities). SV_InitGame clears it when
	// Deathmatch is set.
	Coop bool
}

// Map is the static knowledge of one level.
type Map struct {
	Name string
	// Checksum is what CM_LoadMap returns; CS_MAPCHECKSUM carries it as a
	// decimal int32.
	Checksum uint32
	Options  Options
	// Skill is the effective skill level (Options.Skill clamped to 0..3).
	Skill int
	// Message and NextMap are the worldspawn "message" (level title) and
	// "nextmap" keys.
	Message, NextMap string

	// Entities is the whole lump in order, including inhibited and freed
	// entities.
	Entities []Entity
	// ByTargetname maps a lower-cased targetname to the lump indexes of the
	// present entities carrying it, in lump order (the G_Find order, which
	// compares case-insensitively).
	ByTargetname map[string][]int

	Movers   []Mover
	Triggers []Trigger
	Exits    []Exit
	Lasers   []Laser
	Spawns   []Spawn
	Items    []Item
	Monsters []Monster

	// CM is the collision model the server loads for this BSP (shared,
	// immutable).
	CM *cmodel.Map

	byModel   map[string]int // "*N" -> lump index of the present entity using it
	moverOf   map[int]int    // lump index -> Movers index
	triggerOf map[int]int    // lump index -> Triggers index
	laserOf   map[int]int
	exitOf    map[int]int
	teamOf    map[int][]int // team master lump index -> team chain (master first)
	master    map[int]int   // lump index -> team master lump index
	fires     map[int][]Link
	firedBy   map[int][]Link
}

// Load parses a BSP file (the bytes of maps/<name>.bsp) and derives the map
// knowledge for the given settings. name is the level name ("demo1").
func Load(name string, raw []byte, opt Options) (*Map, error) {
	f, err := bsp.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("mapdata %s: %w", name, err)
	}
	cm, err := cmodel.LoadMap("maps/"+name+".bsp", f, raw)
	if err != nil {
		return nil, fmt.Errorf("mapdata %s: %w", name, err)
	}
	ents, err := parseEntities(cm.EntityString())
	if err != nil {
		return nil, fmt.Errorf("mapdata %s: %w", name, err)
	}
	if len(ents) == 0 {
		return nil, fmt.Errorf("mapdata %s: empty entity lump", name)
	}

	m := &Map{
		Name: name, Checksum: cm.Checksum, Options: opt, Skill: clampSkill(opt.Skill),
		Entities: ents, CM: cm,
		ByTargetname: map[string][]int{}, byModel: map[string]int{},
		moverOf: map[int]int{}, triggerOf: map[int]int{}, laserOf: map[int]int{}, exitOf: map[int]int{},
		teamOf: map[int][]int{}, master: map[int]int{},
		fires: map[int][]Link{}, firedBy: map[int][]Link{},
	}
	m.filter()
	m.index()
	m.findTeams()

	world := &m.Entities[0]
	m.Message, m.NextMap = world.Message, edNewString(world.Keys["nextmap"])

	builders := []func() error{m.buildMovers, m.buildTriggers, m.buildPoints, m.buildLasers}
	for _, b := range builders {
		if err := b(); err != nil {
			return nil, fmt.Errorf("mapdata %s: %w", name, err)
		}
	}
	m.syncTeamSpeeds()
	m.buildLogic()
	return m, nil
}

// clampSkill is the SpawnEntities skill clamp (floor, then 0..3).
// C: game/g_spawn.c:528 SpawnEntities
func clampSkill(s int) int {
	if s < 0 {
		return 0
	}
	if s > 3 {
		return 3
	}
	return s
}

// filter applies the SpawnEntities inhibit filter, the "command" map hack
// and the spawn-time frees.
// C: game/g_spawn.c:569 SpawnEntities
func (m *Map) filter() {
	for i := range m.Entities {
		e := &m.Entities[i]
		e.RawSpawnflags = e.Spawnflags
		if i == 0 {
			continue // the world is never inhibited
		}
		// yet another map hack
		if shared.Q_stricmp(m.Name, "command") == 0 && shared.Q_stricmp(e.Classname, "trigger_once") == 0 &&
			shared.Q_stricmp(e.Model, "*27") == 0 {
			e.Spawnflags &^= SpawnflagNotHard
		}
		if inhibited(e.Spawnflags, m.Skill, m.Options.Deathmatch) {
			e.Inhibited = true
			continue
		}
		e.Spawnflags &^= inhibitMask
		// SV_InitGame turns coop off when deathmatch is set
		if spawnFrees(e, m.Options.Deathmatch, m.Options.Coop && !m.Options.Deathmatch) {
			e.Freed = true
		}
	}
}

func (m *Map) index() {
	for i := range m.Entities {
		e := &m.Entities[i]
		if !e.Present() {
			continue
		}
		if e.Targetname != "" {
			k := asciiLower(e.Targetname)
			m.ByTargetname[k] = append(m.ByTargetname[k], i)
		}
		if e.Brush() {
			if _, dup := m.byModel[e.Model]; !dup {
				m.byModel[e.Model] = i
			}
		}
	}
}

// findTeams chains the present entities with the same team key; the first
// in lump (= edict) order is the master.
// C: game/g_spawn.c:470 G_FindTeams
func (m *Map) findTeams() {
	slave := map[int]bool{}
	for i := 1; i < len(m.Entities); i++ {
		e := &m.Entities[i]
		if !e.Present() || e.Team == "" || slave[i] {
			continue
		}
		chain := []int{i}
		m.master[i] = i
		for j := i + 1; j < len(m.Entities); j++ {
			e2 := &m.Entities[j]
			if !e2.Present() || e2.Team == "" || slave[j] {
				continue
			}
			if e.Team == e2.Team {
				chain = append(chain, j)
				m.master[j] = i
				slave[j] = true
			}
		}
		m.teamOf[i] = chain
	}
}

// Entity returns the lump entity with the given index, or nil.
func (m *Map) Entity(i int) *Entity {
	if i < 0 || i >= len(m.Entities) {
		return nil
	}
	return &m.Entities[i]
}

// Targets returns the present entities whose targetname matches name
// (case-insensitively, like G_Find), in lump order.
func (m *Map) Targets(name string) []*Entity {
	if name == "" {
		return nil
	}
	idx := m.ByTargetname[asciiLower(name)]
	out := make([]*Entity, len(idx))
	for k, i := range idx {
		out[k] = &m.Entities[i]
	}
	return out
}

// ByModel returns the present entity using inline model "*N", or nil.
func (m *Map) ByModel(model string) *Entity {
	if i, ok := m.byModel[model]; ok {
		return &m.Entities[i]
	}
	return nil
}

// Mover returns the mover spawned from lump entity i, or nil.
func (m *Map) Mover(i int) *Mover {
	if k, ok := m.moverOf[i]; ok {
		return &m.Movers[k]
	}
	return nil
}

// Trigger returns the trigger spawned from lump entity i, or nil.
func (m *Map) Trigger(i int) *Trigger {
	if k, ok := m.triggerOf[i]; ok {
		return &m.Triggers[k]
	}
	return nil
}

// Laser returns the target_laser spawned from lump entity i, or nil.
func (m *Map) Laser(i int) *Laser {
	if k, ok := m.laserOf[i]; ok {
		return &m.Lasers[k]
	}
	return nil
}

// Exit returns the target_changelevel spawned from lump entity i, or nil.
func (m *Map) Exit(i int) *Exit {
	if k, ok := m.exitOf[i]; ok {
		return &m.Exits[k]
	}
	return nil
}

// TeamMaster returns the lump index of the team master of entity i (i
// itself when it is the master or not teamed).
func (m *Map) TeamMaster(i int) int {
	if t, ok := m.master[i]; ok {
		return t
	}
	return i
}

// Team returns the team chain whose master is i (master first, then the
// slaves in lump order), or nil when i is no team master.
func (m *Map) Team(i int) []int { return m.teamOf[i] }

// SpawnPoint returns the info_player_start a single-player client spawns
// at when the level is entered with the given spawnpoint ("" for a plain
// "map" command), following SelectSpawnPoint: the first start whose
// targetname matches, or for an empty spawnpoint the first start without a
// targetname, else the first start at all. ok is false when the game would
// error out ("Couldn't find spawn point").
// C: game/p_client.c:875 SelectSpawnPoint
func (m *Map) SpawnPoint(spawnpoint string) (s *Spawn, ok bool) {
	for k := range m.Spawns {
		sp := &m.Spawns[k]
		if spawnpoint == "" && sp.Targetname == "" {
			return sp, true
		}
		if spawnpoint == "" || sp.Targetname == "" {
			continue
		}
		if shared.Q_stricmp(spawnpoint, sp.Targetname) == 0 {
			return sp, true
		}
	}
	if spawnpoint == "" && len(m.Spawns) > 0 {
		// there wasn't a spawnpoint without a target, so use any
		return &m.Spawns[0], true
	}
	return nil, false
}

// ClassCounts returns the number of present entities per classname, sorted
// by classname.
func (m *Map) ClassCounts() []ClassCount {
	n := map[string]int{}
	for i := range m.Entities {
		if e := &m.Entities[i]; e.Present() {
			n[e.Classname]++
		}
	}
	out := make([]ClassCount, 0, len(n))
	for c, k := range n {
		out = append(out, ClassCount{c, k})
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Classname < out[b].Classname })
	return out
}

// ClassCount is one row of ClassCounts.
type ClassCount struct {
	Classname string
	N         int
}

// Box is an axis-aligned box in world coordinates.
type Box struct {
	Min, Max Vec3
}

// Intersects reports whether the boxes overlap (touching faces count, like
// the area-link overlap test in SV_AreaEdicts).
func (b Box) Intersects(o Box) bool {
	for i := 0; i < 3; i++ {
		if b.Min[i] > o.Max[i] || b.Max[i] < o.Min[i] {
			return false
		}
	}
	return true
}

// Contains reports whether p lies inside the box (boundary included).
func (b Box) Contains(p Vec3) bool {
	for i := 0; i < 3; i++ {
		if p[i] < b.Min[i] || p[i] > b.Max[i] {
			return false
		}
	}
	return true
}

// Center returns the middle of the box.
func (b Box) Center() Vec3 {
	return Vec3{(b.Min[0] + b.Max[0]) / 2, (b.Min[1] + b.Max[1]) / 2, (b.Min[2] + b.Max[2]) / 2}
}

// Union returns the smallest box holding both.
func (b Box) Union(o Box) Box {
	for i := 0; i < 3; i++ {
		if o.Min[i] < b.Min[i] {
			b.Min[i] = o.Min[i]
		}
		if o.Max[i] > b.Max[i] {
			b.Max[i] = o.Max[i]
		}
	}
	return b
}

// linkBox is the absmin/absmax SV_LinkEdict computes for an entity at
// origin/angles with the given bounds: a rotated SOLID_BSP entity gets a
// cube of its largest extent, and every box grows by 1 on each side.
// C: server/sv_world.c:219 SV_LinkEdict (set the abs box)
func linkBox(rotatedBSP bool, origin, angles, mins, maxs Vec3) Box {
	var b Box
	if rotatedBSP && (angles[0] != 0 || angles[1] != 0 || angles[2] != 0) {
		var max float32
		for i := 0; i < 3; i++ {
			if v := abs32(mins[i]); v > max {
				max = v
			}
			if v := abs32(maxs[i]); v > max {
				max = v
			}
		}
		for i := 0; i < 3; i++ {
			b.Min[i] = origin[i] - max
			b.Max[i] = origin[i] + max
		}
	} else {
		for i := 0; i < 3; i++ {
			b.Min[i] = origin[i] + mins[i]
			b.Max[i] = origin[i] + maxs[i]
		}
	}
	for i := 0; i < 3; i++ {
		b.Min[i] -= 1
		b.Max[i] += 1
	}
	return b
}

// inlineModel returns the bounds and headnode of inline model "*N" the way
// PF_setmodel / CM_InlineModel resolve it, or an error where the server
// would drop with ERR_DROP.
// C: qcommon/cmodel.c:639 CM_InlineModel
func (m *Map) inlineModel(e *Entity) (shared.CModel, error) {
	if !e.Brush() {
		if e.Model == "" {
			return shared.CModel{}, fmt.Errorf("%v: PF_setmodel: NULL", e)
		}
		return shared.CModel{}, nil // an .md2/.sp2 model: no bounds from setmodel
	}
	if n := int(shared.Atoi(e.Model[1:])); n < 1 || n >= m.CM.NumInlineModels() {
		return shared.CModel{}, fmt.Errorf("%v: CM_InlineModel: bad number", e)
	}
	return *m.CM.InlineModel(e.Model), nil
}

// abs32 is fabs on a float, like the C code computes it (in double).
func abs32(f float32) float32 { return float32(math.Abs(float64(f))) }
