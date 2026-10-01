// Package worldmodel keeps the bot's beliefs about the level from the
// fair percepts of package perception: tracks of monsters and other actors
// with stable IDs, items, projectiles, mover poses, lasers, the bot's own
// state with damage events and their bearing, the inventory and the help
// computer, and a per-level memory that survives a reload.
//
// World.Update is the only entry point that sees a FrameInput, and it hands
// it straight to the Perceiver: everything else works on the Percept, which
// holds only what a player could see or hear. The belief is deterministic:
// it depends only on the inputs and the caller's clock (no map iteration
// order, no wall clock).
//
// A World is not safe for concurrent use; Snapshot returns an independent
// copy for other goroutines.
package worldmodel

import (
	"math"
	"sort"
	"strconv"

	"quake2web/server/internal/agent/mapdata"
	"quake2web/server/internal/agent/perception"
	"quake2web/server/internal/cmodel"
)

// Timing constants (milliseconds of the caller's clock).
const (
	// DecayTau is the time constant of a track's confidence after its last
	// observation.
	DecayTau = 3000
	// AttackMemory is how long a monster counts as attacking after its
	// last attack signal.
	AttackMemory = 3000
	// MoverTimeout marks a mover pose stale.
	MoverTimeout = 5000
	// ProjectileMemory drops a projectile that was not seen for this long.
	ProjectileMemory = 500
	// DamageWindow, SoundWindow: how long damage events and sounds stay in
	// the belief.
	DamageWindow = 10000
	SoundWindow  = 2000
	// MessageLimit is the number of recent messages kept.
	MessageLimit = 8
	// CombatMemory: the bot is in combat this long after damage or an
	// attack.
	CombatMemory = 3000
)

// LevelKey identifies one visit of a level: the map and its visit index
// in the campaign (demo2 is visited twice). A reload keeps the key.
type LevelKey struct {
	Map   string
	Visit int
}

// Level is what the world model knows statically about the level it
// enters.
type Level struct {
	Key LevelKey
	// CM is the collision model (nil: Map.CM; both nil: no occlusion,
	// which is only meant for tests).
	CM *cmodel.Map
	// Map is the level's static knowledge (optional): lump entities to tie
	// to entity numbers, laser segments, mover spawn poses.
	Map *mapdata.Map
}

// Config configures a World.
type Config struct {
	// ReadFile reads game data: the MD2 models whose frame names give the
	// animation states (nil: no animations).
	ReadFile func(name string) ([]byte, error)
	// Anims replaces the animation cache built from ReadFile (tests seed
	// one with AnimCache.Set).
	Anims *perception.AnimCache
	// Perception configures the observation filter (field of view).
	Perception perception.Options
	// Classes is the class table (nil: perception.NewClassTable()).
	Classes *perception.ClassTable
}

// World is the bot's world model.
type World struct {
	cfg     Config
	classes *perception.ClassTable
	anims   *perception.AnimCache

	level     Level
	gen       int // level generation the state belongs to; -1: next Update binds
	cls       *perception.Classifier
	perceiver *perception.Perceiver
	pc        *perception.Percept
	now       int64

	memories map[LevelKey]*LevelMemory
	mem      *LevelMemory
	match    lumpMatch

	actors  []*actor
	items   []*itemTrack
	projs   []*projTrack
	movers  map[int]*Mover // by inline model number
	lasers  []*Laser
	bind    map[int32]binding
	nextID  map[byte]int
	explode []explosion

	self    selfTracker
	refresh refresher

	b Belief
}

type bindKind uint8

const (
	bindActor bindKind = iota + 1
	bindItem
	bindProj
)

type binding struct {
	kind bindKind
	idx  int
}

type explosion struct {
	pos Vec3
	at  int64
}

// New returns a World with no level; call Reset before the first Update
// of a level.
func New(cfg Config) *World {
	w := &World{cfg: cfg, classes: cfg.Classes, memories: map[LevelKey]*LevelMemory{}}
	if w.classes == nil {
		w.classes = perception.NewClassTable()
	}
	w.anims = cfg.Anims
	if w.anims == nil {
		w.anims = perception.NewAnimCache(cfg.ReadFile)
	}
	w.Reset(Level{})
	return w
}

// Reset starts a level: the next Update's frames belong to lv. Entering a
// key that was entered before (a reload of the level-entry save, or a
// repeated visit index) restores its LevelMemory: the static learnings
// (death spots, blocked edges, items known) are kept, everything dynamic is
// forgotten.
func (w *World) Reset(lv Level) {
	if lv.CM == nil && lv.Map != nil {
		lv.CM = lv.Map.CM
	}
	w.level = lv
	w.gen = -1
	w.cls = perception.NewClassifier(w.classes)
	w.perceiver = perception.NewPerceiver(lv.CM, w.cls, w.anims, w.cfg.Perception)
	w.pc = nil
	w.actors, w.items, w.projs, w.lasers = nil, nil, nil, nil
	w.movers = map[int]*Mover{}
	w.bind = map[int32]binding{}
	w.nextID = map[byte]int{}
	w.explode = nil
	w.match = lumpMatch{}
	w.self = selfTracker{}
	w.refresh = refresher{}
	w.b = Belief{Level: lv.Key, Map: lv.Key.Map}

	if lv.Key.Map == "" {
		w.mem = &LevelMemory{}
		return
	}
	mem := w.memories[lv.Key]
	if mem == nil {
		mem = &LevelMemory{Key: lv.Key}
		w.memories[lv.Key] = mem
	}
	w.mem = mem
}

// Level returns the current level.
func (w *World) Level() Level { return w.level }

// Memory returns the current level's memory (live; do not modify).
func (w *World) Memory() *LevelMemory { return w.mem }

// MemoryFor returns the memory of a level key (nil if never entered).
func (w *World) MemoryFor(k LevelKey) *LevelMemory { return w.memories[k] }

// MarkBlocked records a navigation edge (an opaque key chosen by the
// navigator) as blocked in the level memory.
func (w *World) MarkBlocked(edge string) { w.mem.addBlocked(edge) }

// Percept returns the last percept (nil before the first Update of a
// level): what the bot could perceive in the last frame.
func (w *World) Percept() *perception.Percept { return w.pc }

// Belief returns the live belief. It is updated in place by Update and must
// not be retained across Updates or shared with other goroutines.
func (w *World) Belief() *Belief { return &w.b }

// Snapshot returns a deep copy of the belief.
func (w *World) Snapshot() Belief { return w.b.Clone() }

// Update folds one frame into the belief. now is the caller's clock in
// milliseconds (session game time); it must not go backwards.
func (w *World) Update(in perception.FrameInput, now int64) {
	if in.Level == nil {
		return
	}
	if w.gen != in.Level.Gen {
		if w.gen != -1 {
			w.autoEnter(in.Level)
		}
		w.gen = in.Level.Gen
		w.bindLevel(&in)
	}
	w.now = now
	pc := w.perceiver.Perceive(&in)
	w.pc = pc
	w.b.Time, w.b.ServerFrame = now, pc.ServerFrame
	w.b.Frames++

	w.self.update(w, pc)
	w.observeSightings(pc)
	w.observeHearings(pc)
	w.observeFlashes(pc)
	w.observeTempEnts(pc)
	w.checkAbsences(pc)
	w.updateLasers(pc)
	w.updateProjectiles(pc)
	w.updateActors()
	w.updateCombat()
	w.refresh.update(w, pc)
	w.messages(pc)
	w.publish()
}

// autoEnter handles a level generation the caller did not Reset for: a
// reload of the same map keeps the level, anything else enters a level
// without static data.
func (w *World) autoEnter(ls *perception.LevelStatic) {
	lv := w.level
	if ls.MapName != lv.Key.Map {
		lv = Level{Key: LevelKey{Map: ls.MapName}}
	}
	w.Reset(lv)
}

// bindLevel ties the level's lump entities to entity numbers and restores
// the remembered items.
func (w *World) bindLevel(in *perception.FrameInput) {
	ls := in.Level
	if w.mem.Key.Map != "" && (w.mem.Entries == 0 || w.mem.gen != ls.Gen) {
		w.mem.Entries++
		w.mem.gen = ls.Gen
	}
	w.cls.Update(in.CS)
	if w.level.Map != nil {
		w.match = matchLump(w.level.Map, ls.Baselines, w.cls)
	}
	w.match.baselines = map[int32]Vec3{}
	w.match.baseAngles = map[int32]Vec3{}
	for _, b := range ls.Baselines {
		w.match.baselines[b.Number] = b.Origin
		w.match.baseAngles[b.Number] = b.Angles
	}
	w.restoreItems()
	w.initLasers()
}

func (w *World) newID(prefix byte) string {
	w.nextID[prefix]++
	return string(prefix) + strconv.Itoa(w.nextID[prefix])
}

// release drops the binding of an entity number (the number now belongs
// to something else, or the tracked thing is gone).
func (w *World) release(num int32) {
	b, ok := w.bind[num]
	if !ok {
		return
	}
	delete(w.bind, num)
	switch b.kind {
	case bindActor:
		a := w.actors[b.idx]
		a.bound = false
		if a.Life != LifeGibbed && a.Life != LifeGone {
			a.setLife(LifeGone, w.now)
		}
	case bindItem:
		it := w.items[b.idx]
		it.bound = false
		if it.Life == LifeAlive {
			it.setLife(LifeGone, w.now)
		}
	case bindProj:
		w.projs[b.idx].bound = false
	}
}

func (w *World) messages(pc *perception.Percept) {
	for _, s := range pc.Prints {
		w.b.Messages = append(w.b.Messages, Message{At: w.now, Text: s})
	}
	for _, s := range pc.CenterPrints {
		w.b.Messages = append(w.b.Messages, Message{At: w.now, Center: true, Text: s})
	}
	if n := len(w.b.Messages); n > MessageLimit {
		w.b.Messages = append([]Message(nil), w.b.Messages[n-MessageLimit:]...)
	}
}

// publish rebuilds the belief's slices from the internal state.
func (w *World) publish() {
	b := &w.b
	b.Tracks = b.Tracks[:0]
	for _, a := range w.actors {
		b.Tracks = append(b.Tracks, a.Track)
	}
	b.Items = b.Items[:0]
	for _, it := range w.items {
		b.Items = append(b.Items, it.Item)
	}
	b.Projectiles = b.Projectiles[:0]
	for _, p := range w.projs {
		if p.visible {
			b.Projectiles = append(b.Projectiles, p.Projectile)
		}
	}
	b.Movers = b.Movers[:0]
	keys := make([]int, 0, len(w.movers))
	for k := range w.movers {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	for _, k := range keys {
		m := w.movers[k]
		m.Stale = w.now-m.LastUpdate > MoverTimeout
		b.Movers = append(b.Movers, *m)
	}
	b.Lasers = b.Lasers[:0]
	for _, l := range w.lasers {
		b.Lasers = append(b.Lasers, *l)
	}
	b.Damage = trimDamage(b.Damage, w.now)
	b.Sounds = trimSounds(b.Sounds, w.now)
	b.Memory = w.mem.view()
}

func trimDamage(d []DamageEvent, now int64) []DamageEvent {
	i := 0
	for i < len(d) && now-d[i].At > DamageWindow {
		i++
	}
	return d[i:]
}

func trimSounds(s []SoundEvent, now int64) []SoundEvent {
	i := 0
	for i < len(s) && now-s[i].At > SoundWindow {
		i++
	}
	return s[i:]
}

// decay is the confidence after age ms.
func decay(age int64) float32 {
	if age <= 0 {
		return 1
	}
	return float32(math.Exp(-float64(age) / DecayTau))
}

func dist(a, b Vec3) float32 {
	dx, dy, dz := a[0]-b[0], a[1]-b[1], a[2]-b[2]
	return float32(math.Sqrt(float64(dx*dx + dy*dy + dz*dz)))
}
