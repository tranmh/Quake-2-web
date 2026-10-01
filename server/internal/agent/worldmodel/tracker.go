package worldmodel

import (
	"math"
	"strconv"
	"strings"

	"quake2web/server/internal/agent/perception"
	"quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/shared"
)

// Tracking thresholds.
const (
	// teleportJump is the per-axis jump between consecutive frames that
	// makes the client reset an entity's interpolation (CL_DeltaEntity):
	// the entity is treated as a new one.
	teleportJump = 512
	// maxActorSpeed bounds how far an unseen actor can have moved.
	maxActorSpeed = 800
	// explosionRadius: a vanished track within this distance of a recent
	// explosion was blown up.
	explosionRadius = 160
	explosionMemory = 500
	// pickupRadius: an item this close to the bot when a pickup message
	// appears was the one taken.
	pickupRadius = 96
	// maxActors bounds the actor list: beyond it, actors that vanished
	// long ago and stand for no lump entity are forgotten.
	maxActors   = 256
	forgetAfter = 30000
)

// actor is the internal state of a Track.
type actor struct {
	Track
	idx    int
	cls    *perception.Class // nil while only heard
	family string
	bound  bool
	obsPos Vec3  // last admitted position
	obsAt  int64 // when
	hasObs bool
}

func (a *actor) setLife(l LifeState, now int64) {
	if a.Life != l {
		a.Life, a.LifeAt = l, now
	}
}

// observe folds an admitted position into the track: velocity EMA.
func (a *actor) observe(pos Vec3, now int64) {
	if a.hasObs && now > a.obsAt && now-a.obsAt <= 1000 {
		dt := float32(now-a.obsAt) / 1000
		for i := 0; i < 3; i++ {
			inst := (pos[i] - a.obsPos[i]) / dt
			a.Vel[i] = 0.5*inst + 0.5*a.Vel[i]
		}
	} else if !a.hasObs || now-a.obsAt > 1000 {
		a.Vel = Vec3{}
	}
	a.obsPos, a.obsAt, a.hasObs = pos, now, true
	a.Pos, a.PosKnown = pos, true
	a.LastUpdate = now
	a.Missing = false
}

type itemTrack struct {
	Item
	idx   int
	bound bool
}

func (it *itemTrack) setLife(l LifeState, now int64) {
	if it.Life != l {
		it.Life, it.LifeAt = l, now
	}
}

type projTrack struct {
	Projectile
	idx     int
	bound   bool
	visible bool // seen this frame
	cls     *perception.Class
	obsAt   int64
}

// familyOf names the voice family of a class (the monster's sound
// directory): all soldiers are "soldier", both tanks "tank".
func familyOf(c *perception.Class) string {
	if c == nil {
		return ""
	}
	switch {
	case strings.HasPrefix(c.Name, "soldier"):
		return "soldier"
	case strings.HasPrefix(c.Name, "tank"):
		return "tank"
	}
	return c.Name
}

// compatible reports whether an observation of class c can continue track a.
func compatible(a *actor, c *perception.Class) bool {
	switch {
	case a.cls == nil:
		return a.family == "" || a.family == familyOf(c) || c.Name == "monster"
	case a.cls == c:
		return true
	case c.Name == "monster" || a.cls.Name == "monster":
		return c.Kind == a.cls.Kind
	}
	return a.cls.Model != "" && a.cls.Model == c.Model && familyOf(a.cls) == familyOf(c)
}

// jumped reports a teleport-like displacement since the last admitted
// position.
func (w *World) jumped(a *actor, pos Vec3, event int32) bool {
	if event == q2const.EV_PLAYER_TELEPORT || event == q2const.EV_OTHER_TELEPORT {
		return true
	}
	if !a.hasObs {
		return false
	}
	dt := w.now - a.obsAt
	if dt <= 150 {
		for i := 0; i < 3; i++ {
			if float32(math.Abs(float64(pos[i]-a.obsPos[i]))) > teleportJump {
				return true
			}
		}
		return false
	}
	return dist(pos, a.obsPos) > teleportJump+maxActorSpeed*float32(dt)/1000
}

func trackPrefix(c *perception.Class) byte {
	if c != nil && c.Kind == perception.KindBarrel {
		return 'o'
	}
	return 'e'
}

// actorFor returns the track an admitted observation of entity num with
// class c (nil: unknown, heard only) at pos continues, or a new one.
func (w *World) actorFor(num int32, c *perception.Class, family string, pos Vec3, posKnown bool, event int32) *actor {
	if b, ok := w.bind[num]; ok {
		if b.kind == bindActor {
			a := w.actors[b.idx]
			ok := c == nil || compatible(a, c)
			if ok && c == nil && family != "" && a.family != "" && a.family != family {
				ok = false
			}
			if ok && posKnown && w.jumped(a, pos, event) {
				ok = false
			}
			if ok {
				return a
			}
		}
		w.release(num)
	}
	a := &actor{idx: len(w.actors), cls: c, family: family, bound: true}
	if c != nil {
		a.family = familyOf(c)
	}
	a.ID = w.newID(trackPrefix(c))
	a.Num = num
	a.Lump = w.lumpFor(num, c, family)
	a.Class, a.Kind = family, perception.KindMonster.String()
	if family == "" {
		a.Class = "monster"
	}
	if family == "player" {
		a.Kind = perception.KindPlayer.String()
	}
	if c != nil {
		a.Class, a.Kind = c.Name, c.Kind.String()
		a.Mins, a.Maxs = c.Mins, c.Maxs
	}
	w.actors = append(w.actors, a)
	w.bind[num] = binding{kind: bindActor, idx: a.idx}
	return a
}

// lumpFor returns the lump entity tied to entity number num when it can be
// the thing observed (class c, or a monster voice of family), else -1: an
// entity number freed and reused later must not inherit the lump entity.
func (w *World) lumpFor(num int32, c *perception.Class, family string) int {
	l, ok := w.match.numToLump[num]
	if !ok || w.level.Map == nil || l < 0 || l >= len(w.level.Map.Entities) {
		return -1
	}
	cn := w.level.Map.Entities[l].Classname
	switch {
	case c == nil || c.Name == "monster":
		if strings.HasPrefix(cn, "monster_") {
			return l
		}
		return -1
	}
	for _, x := range c.Classnames {
		if x == cn {
			return l
		}
	}
	return -1
}

// observeSightings folds what was seen.
func (w *World) observeSightings(pc *perception.Percept) {
	for _, a := range w.actors {
		a.Visible, a.Heard = false, false
	}
	for _, it := range w.items {
		it.Visible = false
	}
	for _, p := range w.projs {
		p.visible = false
	}
	for _, m := range w.movers {
		m.Visible, m.Heard = false, false
	}
	for i := range pc.Seen {
		s := &pc.Seen[i]
		switch s.Class.Kind {
		case perception.KindMonster, perception.KindNeutral, perception.KindPlayer, perception.KindBarrel:
			w.seeActor(s)
		case perception.KindItem:
			w.seeItem(s, pc)
		case perception.KindProjectile:
			w.seeProjectile(s, pc)
		case perception.KindGib:
			w.seeGib(s)
		case perception.KindBrush:
			w.seeMover(&perception.BrushPose{Num: s.Num, Inline: s.Inline, Origin: s.State.Origin, Angles: s.State.Angles}, true)
		case perception.KindBeam:
			w.seeBeam(s)
		}
	}
}

func (w *World) seeActor(s *perception.Sighting) {
	a := w.actorFor(s.Num, s.Class, "", s.State.Origin, true, s.State.Event)
	a.cls, a.family = s.Class, familyOf(s.Class)
	a.Class, a.Kind = s.Class.Name, s.Class.Kind.String()
	a.observe(s.State.Origin, w.now)
	a.Num = s.Num
	a.Mins, a.Maxs = s.Mins, s.Maxs
	a.Yaw = s.State.Angles[q2const.YAW]
	a.Dist = s.Dist
	a.Visible, a.Shootable, a.Wounded = true, s.Shootable, s.Wounded
	a.LastSeen = w.now
	if a.FirstSeen == 0 {
		a.FirstSeen = w.now
	}
	a.Anim = s.Anim.State.String()

	if s.Class.Kind != perception.KindMonster && s.Class.Kind != perception.KindNeutral {
		return
	}
	// A dead monster's box is not solid (SVF_DEADMONSTER, set when its
	// death animation ends: C: game/m_*.c *_dead); the death animation
	// ends on the last frame of its sequence.
	corpse := s.State.Solid == 0
	dying := s.Anim.State == perception.AnimDeath
	switch {
	case a.Life == LifeDead && !corpse && !dying:
		// a medic brought the corpse back (C: game/m_medic.c
		// medic_cable_attack ED_CallSpawn)
		a.setLife(LifeAlive, w.now)
		a.Revived++
		a.Awareness = Alert
	case corpse, dying && s.State.Frame == s.Anim.Last:
		if a.Life < LifeDead {
			a.setLife(LifeDead, w.now)
		}
	case dying:
		if a.Life == LifeAlive {
			a.setLife(LifeDying, w.now)
		}
	}
	if a.Life == LifeAlive {
		switch s.Anim.State {
		case perception.AnimAttack:
			a.Awareness, a.LastAttack = Attacking, w.now
			if a.Weapon == "" {
				a.Weapon = s.Class.Weapon.String()
			}
		case perception.AnimRun, perception.AnimPain, perception.AnimDuck:
			if a.Awareness < Alert {
				a.Awareness = Alert
			}
		}
	}
	if a.Life == LifeDead && a.Lump >= 0 && s.Class.Hostile() {
		w.effect(EffectMonsterDead, a.Lump, a.ID)
	}
}

func (w *World) seeGib(s *perception.Sighting) {
	b, ok := w.bind[s.Num]
	if !ok || b.kind != bindActor {
		return
	}
	a := w.actors[b.idx]
	if a.cls != nil && a.cls.Kind != perception.KindMonster && a.cls.Kind != perception.KindNeutral && a.cls.Kind != perception.KindPlayer {
		return
	}
	// ThrowHead turns the monster entity itself into a head gib
	// (C: game/g_misc.c ThrowHead)
	a.observe(s.State.Origin, w.now)
	a.setLife(LifeGibbed, w.now)
	a.Visible, a.LastSeen = true, w.now
	delete(w.bind, s.Num)
	a.bound = false
	if a.Lump >= 0 {
		w.effect(EffectMonsterDead, a.Lump, a.ID)
	}
}

func (w *World) seeItem(s *perception.Sighting, pc *perception.Percept) {
	var it *itemTrack
	if b, ok := w.bind[s.Num]; ok {
		if b.kind == bindItem && w.items[b.idx].Class == s.Class.Name {
			it = w.items[b.idx]
		} else {
			w.release(s.Num)
		}
	}
	if it == nil {
		// a remembered item at this spot
		for _, r := range w.items {
			if !r.bound && r.Class == s.Class.Name && dist(r.Pos, s.State.Origin) < 16 {
				it = r
				break
			}
		}
	}
	if it == nil {
		it = &itemTrack{idx: len(w.items)}
		it.ID = w.newID('i')
		it.Class = s.Class.Name
		it.Kind = s.Class.Item.String()
		it.Pickup = s.Class.Pickup
		it.Value = s.Class.Value
		it.Amount = s.Class.Amount
		it.Lump = w.lumpFor(s.Num, s.Class, "")
		w.items = append(w.items, it)
		w.mem.addItem(KnownItem{Class: it.Class, Pos: s.State.Origin, Lump: it.Lump})
	}
	it.bound = true
	w.bind[s.Num] = binding{kind: bindItem, idx: it.idx}
	it.Num = s.Num
	it.Pos = s.State.Origin
	it.Index = pc.Classifier().ItemIndex(it.Pickup)
	it.Visible, it.LastSeen, it.Remembered = true, w.now, false
	it.setLife(LifeAlive, w.now)
}

// projectile speed priors when the first sighting gives no displacement
// (C: game/g_weapon.c fire_* callers' speeds, rounded).
func projectileSpeed(c *perception.Class) float32 {
	switch c.Weapon {
	case perception.WeaponBlaster, perception.WeaponHyperblaster:
		return 1000
	case perception.WeaponRocket:
		return 650
	case perception.WeaponGrenade:
		return 600
	case perception.WeaponBFG:
		return 400
	}
	return 600
}

func (w *World) seeProjectile(s *perception.Sighting, pc *perception.Percept) {
	var p *projTrack
	if b, ok := w.bind[s.Num]; ok {
		if b.kind == bindProj && w.projs[b.idx].cls == s.Class {
			p = w.projs[b.idx]
		} else {
			w.release(s.Num)
		}
	}
	pos := s.State.Origin
	if p != nil && w.now > p.obsAt {
		dt := float32(w.now-p.obsAt) / 1000
		if dt <= 0.5 && dist(pos, p.Pos) < 3000*dt+64 {
			for i := 0; i < 3; i++ {
				p.Vel[i] = 0.7*(pos[i]-p.Pos[i])/dt + 0.3*p.Vel[i]
			}
		} else {
			p = nil // a new projectile on the same number
			w.release(s.Num)
		}
	}
	if p == nil {
		p = &projTrack{idx: len(w.projs), cls: s.Class, bound: true}
		p.ID = w.newID('p')
		p.Class, p.Weapon = s.Class.Name, s.Class.Weapon.String()
		p.FirstSeen = w.now
		// the server sends old_origin with a new entity: one frame back
		d := shared.VectorSubtract(pos, s.State.OldOrigin)
		speed := shared.VectorLength(d) * 10
		if s.State.OldOrigin != (Vec3{}) && speed > 100 && speed < 2500 {
			p.Vel = shared.VectorScale(d, 10)
		} else {
			var fwd Vec3
			shared.AngleVectors(s.State.Angles, &fwd, nil, nil)
			p.Vel = shared.VectorScale(fwd, projectileSpeed(s.Class))
		}
		// the server clears solid on the shooter's own missiles
		// (C: server/sv_ents.c SV_BuildClientFrame); or it appeared at the
		// eye right after a shot
		p.Own = s.State.Solid == 0 ||
			(w.now-w.b.Self.LastFired <= 300 && w.b.Self.LastFired > 0 && dist(pos, pc.Eye) < 96)
		w.projs = append(w.projs, p)
		w.bind[s.Num] = binding{kind: bindProj, idx: p.idx}
	}
	p.Num, p.Pos = s.Num, pos
	p.visible, p.LastSeen, p.obsAt = true, w.now, w.now
}

// seeMover admits a brush entity's pose (seen, or heard with its sound).
func (w *World) seeMover(bp *perception.BrushPose, seen bool) {
	m := w.movers[bp.Inline]
	if m == nil {
		m = &Mover{Model: "*" + strconv.Itoa(bp.Inline), Lump: -1}
		if w.level.Map != nil {
			if e := w.level.Map.ByModel(m.Model); e != nil {
				m.Lump = e.Index
			}
		}
		w.movers[bp.Inline] = m
	} else {
		m.Moving = m.Origin != bp.Origin || m.Angles != bp.Angles
	}
	m.Num, m.Origin, m.Angles = bp.Num, bp.Origin, bp.Angles
	m.LastUpdate = w.now
	if seen {
		m.Visible = true
	} else {
		m.Heard = true
	}
	// the spawn pose is the baseline (or, without one, the lump's)
	spawnO, ok := w.match.baselines[bp.Num]
	spawnA := w.match.baseAngles[bp.Num]
	if !ok && m.Lump >= 0 && w.level.Map != nil {
		if mv := w.level.Map.Mover(m.Lump); mv != nil {
			spawnO, spawnA, ok = mv.Origin, mv.Angles, true
		}
	}
	if !ok {
		return
	}
	if dist(spawnO, bp.Origin) > 1 || dist(spawnA, bp.Angles) > 1 {
		if !m.Moved {
			w.effect(EffectMoverMoved, m.Lump, m.Model)
		}
		m.Moved = true
	} else {
		m.Moved = false
	}
}

func (w *World) seeBeam(s *perception.Sighting) {
	l := w.laserFor(s.Num, s.State.Origin)
	l.Start, l.End = s.State.Origin, s.State.OldOrigin
	if l.State != LaserOn && l.Lump >= 0 {
		w.effect(EffectLaserOn, l.Lump, "")
	}
	l.State, l.LastUpdate = LaserOn, w.now
}

func (w *World) laserFor(num int32, start Vec3) *Laser {
	for _, l := range w.lasers {
		if l.Num == num {
			return l
		}
	}
	for _, l := range w.lasers {
		if l.Num == 0 && dist(l.Start, start) < 2 {
			l.Num = num
			return l
		}
	}
	l := &Laser{Lump: -1, Num: num, Start: start}
	w.lasers = append(w.lasers, l)
	return l
}

// observeHearings folds what was heard.
func (w *World) observeHearings(pc *perception.Percept) {
	for i := range pc.Heard {
		h := &pc.Heard[i]
		ev := SoundEvent{At: w.now, Kind: h.Kind.String(), Path: h.Path, Family: h.Family, Num: h.Num,
			Pos: h.Pos, PosKnown: h.PosKnown}
		if h.Mover != nil {
			w.seeMover(h.Mover, false)
		}
		var a *actor
		if h.Num > 0 && h.Mover == nil {
			if b, ok := w.bind[h.Num]; ok && b.kind == bindActor {
				// the voice's directory is only a hint (soldiers cock
				// their guns with an infantry sound): the number decides
				a = w.actors[b.idx]
			} else if h.Family != "" && h.FromEntity {
				// a monster's voice (or buzz) at a known position
				a = w.actorFor(h.Num, nil, h.Family, h.Pos, true, 0)
			}
		}
		if a != nil {
			w.hearActor(a, h)
			ev.Track = a.ID
		}
		if !h.Loop {
			w.b.Sounds = append(w.b.Sounds, ev)
		}
	}
}

func (w *World) hearActor(a *actor, h *perception.Hearing) {
	if h.FromEntity {
		a.observe(h.Pos, w.now)
	} else {
		a.LastUpdate = w.now
	}
	a.Heard, a.LastHeard = true, w.now
	switch h.Kind {
	case perception.SoundDeath:
		if a.Life == LifeAlive {
			a.setLife(LifeDying, w.now)
		}
	case perception.SoundGib:
		a.setLife(LifeGibbed, w.now)
		if a.Lump >= 0 {
			w.effect(EffectMonsterDead, a.Lump, a.ID)
		}
	case perception.SoundSight, perception.SoundPain, perception.SoundSearch:
		if a.Life == LifeAlive && a.Awareness < Alert {
			a.Awareness = Alert
		}
	case perception.SoundAttack:
		if a.Life == LifeAlive {
			a.Awareness, a.LastAttack = Attacking, w.now
		}
	}
}

// observeFlashes folds the muzzle flashes of others.
func (w *World) observeFlashes(pc *perception.Percept) {
	for i := range pc.Flashes {
		f := &pc.Flashes[i]
		var a *actor
		if b, ok := w.bind[f.Num]; ok && b.kind == bindActor {
			a = w.actors[b.idx]
		} else if f.PosKnown {
			fam := "" // svc_muzzleflash2: some monster
			if !f.Monster {
				fam = "player"
			}
			a = w.actorFor(f.Num, nil, fam, f.Pos, true, 0)
		}
		if a == nil {
			continue
		}
		if f.PosKnown {
			a.observe(f.Pos, w.now)
		}
		a.Heard, a.LastHeard = true, w.now
		if a.Life == LifeAlive {
			a.Awareness, a.LastAttack, a.Weapon = Attacking, w.now, f.Weapon.String()
		}
	}
}

func explosive(t int32) bool {
	switch t {
	case q2const.TE_EXPLOSION1, q2const.TE_EXPLOSION2, q2const.TE_ROCKET_EXPLOSION, q2const.TE_ROCKET_EXPLOSION_WATER,
		q2const.TE_GRENADE_EXPLOSION, q2const.TE_GRENADE_EXPLOSION_WATER, q2const.TE_EXPLOSION1_BIG,
		q2const.TE_EXPLOSION1_NP, q2const.TE_PLASMA_EXPLOSION, q2const.TE_BFG_BIGEXPLOSION, q2const.TE_PLAIN_EXPLOSION:
		return true
	}
	return false
}

func (w *World) observeTempEnts(pc *perception.Percept) {
	keep := w.explode[:0]
	for _, e := range w.explode {
		if w.now-e.at <= explosionMemory {
			keep = append(keep, e)
		}
	}
	w.explode = keep
	for i := range pc.TempEnts {
		if t := &pc.TempEnts[i]; explosive(t.Type) {
			w.explode = append(w.explode, explosion{pos: t.Pos, at: w.now})
		}
	}
}

func (w *World) explosionNear(p Vec3) bool {
	for _, e := range w.explode {
		if dist(e.pos, p) <= explosionRadius {
			return true
		}
	}
	return false
}

// checkAbsences looks at the remembered positions that are in view now:
// whatever is not there anymore moved, vanished or was taken.
func (w *World) checkAbsences(pc *perception.Percept) {
	v := pc.Vision()
	for _, a := range w.actors {
		if a.Visible || !a.PosKnown || a.LastSeen == 0 || a.Life == LifeGone || a.Life == LifeGibbed {
			continue
		}
		mins, maxs := a.Mins, a.Maxs
		if a.Life == LifeDead || a.Life == LifeDying {
			maxs[2] = min(maxs[2], -8)
		}
		if !v.SeesBox(shared.VectorAdd(a.Pos, mins), shared.VectorAdd(a.Pos, maxs), -1) {
			continue
		}
		switch {
		case w.explosionNear(a.Pos):
			a.setLife(LifeGibbed, w.now)
			w.unbindActor(a)
			if a.Lump >= 0 && a.cls.Hostile() {
				w.effect(EffectMonsterDead, a.Lump, a.ID)
			}
		case a.Life == LifeDead || a.Life == LifeDying:
			a.setLife(LifeGone, w.now)
			w.unbindActor(a)
		default:
			a.Missing = true
		}
	}
	for _, it := range w.items {
		if it.Visible || it.Life != LifeAlive {
			continue
		}
		lo := shared.VectorAdd(it.Pos, Vec3{-8, -8, -8})
		hi := shared.VectorAdd(it.Pos, Vec3{8, 8, 8})
		if it.Remembered || !v.SeesBox(lo, hi, -1) {
			continue
		}
		if w.now-w.b.Self.PickupAt <= 1000 && dist(it.Pos, w.b.Self.Origin) <= 2*pickupRadius {
			it.setLife(LifeTaken, w.now)
			if it.Lump >= 0 {
				w.effect(EffectItemTaken, it.Lump, it.ID)
			}
		} else {
			it.setLife(LifeGone, w.now)
		}
		w.unbindItem(it)
	}
}

func (w *World) unbindActor(a *actor) {
	if a.bound {
		if b, ok := w.bind[a.Num]; ok && b.kind == bindActor && b.idx == a.idx {
			delete(w.bind, a.Num)
		}
		a.bound = false
	}
}

func (w *World) unbindItem(it *itemTrack) {
	if it.bound {
		if b, ok := w.bind[it.Num]; ok && b.kind == bindItem && b.idx == it.idx {
			delete(w.bind, it.Num)
		}
		it.bound = false
	}
}

// pickedUp marks the item the bot just took (a pickup message appeared).
func (w *World) pickedUp(name string) {
	var best *itemTrack
	bestD := float32(pickupRadius)
	for _, it := range w.items {
		if it.Life != LifeAlive {
			continue
		}
		d := dist(it.Pos, w.b.Self.Origin)
		if d > bestD || (name != "" && it.Pickup != name && best != nil && best.Pickup == name) {
			continue
		}
		if best == nil || d < bestD || (name != "" && it.Pickup == name && best.Pickup != name) {
			best, bestD = it, d
		}
	}
	if best != nil {
		best.setLife(LifeTaken, w.now)
		if best.Lump >= 0 {
			w.effect(EffectItemTaken, best.Lump, best.ID)
		}
		w.unbindItem(best)
	}
}

// updateLasers confirms lasers off: the segment is in view, the server
// would send the beam (its start is in the PHS) and no beam is seen.
func (w *World) updateLasers(pc *perception.Percept) {
	v := pc.Vision()
	for _, l := range w.lasers {
		if l.LastUpdate == w.now && l.State == LaserOn {
			continue
		}
		if l.End == (Vec3{}) && l.Start == (Vec3{}) {
			continue
		}
		if v.InPHS(l.Start) && v.SeesSegment(l.Start, l.End, 8, -1) {
			if l.State != LaserOff && l.Lump >= 0 {
				w.effect(EffectLaserOff, l.Lump, "")
			}
			l.State, l.LastUpdate = LaserOff, w.now
		}
	}
}

// updateProjectiles drops old projectiles and computes the closest
// approach of the visible ones to the bot.
func (w *World) updateProjectiles(pc *perception.Percept) {
	keep := w.projs[:0]
	for _, p := range w.projs {
		if w.now-p.LastSeen > ProjectileMemory {
			if b, ok := w.bind[p.Num]; ok && b.kind == bindProj && w.projs[b.idx] == p {
				delete(w.bind, p.Num)
			}
			continue
		}
		keep = append(keep, p)
	}
	w.projs = keep
	for i, p := range w.projs {
		p.idx = i
		if p.bound {
			w.bind[p.Num] = binding{kind: bindProj, idx: i}
		}
	}
	self := &w.b.Self
	center := shared.VectorAdd(self.Origin, Vec3{0, 0, (self.Mins[2] + self.Maxs[2]) / 2})
	_, right, _ := pc.Vision().Axes()
	for _, p := range w.projs {
		if !p.visible {
			continue
		}
		approach(&p.Projectile, center, self.Velocity, right, w.classes.ByName(p.Class))
	}
}

// updateActors refreshes confidence, awareness decay and threat.
func (w *World) updateActors() {
	if len(w.actors) > maxActors {
		w.compactActors()
	}
	eye := w.b.Self.Eye
	for _, a := range w.actors {
		a.Confidence = decay(w.now - a.LastUpdate)
		if a.Awareness == Attacking && w.now-a.LastAttack > AttackMemory {
			a.Awareness = Alert
		}
		if a.PosKnown && !a.Visible {
			a.Dist = dist(a.Pos, eye)
		}
		a.Threat = threat(a)
	}
}

// compactActors forgets actors that are gone or gibbed for a while, are
// not bound to an entity number and stand for no lump entity.
func (w *World) compactActors() {
	keep := w.actors[:0]
	for _, a := range w.actors {
		if !a.bound && a.Lump < 0 && (a.Life == LifeGone || a.Life == LifeGibbed) && w.now-a.LifeAt > forgetAfter {
			continue
		}
		keep = append(keep, a)
	}
	for i := len(keep); i < len(w.actors); i++ {
		w.actors[i] = nil
	}
	w.actors = keep
	for i, a := range w.actors {
		a.idx = i
		if a.bound {
			w.bind[a.Num] = binding{kind: bindActor, idx: i}
		}
	}
}

// threat estimates the damage per second a track deals: its class prior,
// scaled by awareness, range, line of fire and confidence.
func threat(a *actor) float32 {
	if a.Life != LifeAlive || !a.cls.Hostile() {
		if a.cls == nil && a.Life == LifeAlive && a.Kind == perception.KindMonster.String() {
			return 5 * a.Confidence // heard, unknown class
		}
		return 0
	}
	t := a.cls.DPS
	switch a.Awareness {
	case Idle:
		t *= 0.3
	case Alert:
		t *= 0.7
	}
	if r := a.cls.Range; r > 0 && a.Dist > r {
		t *= r / a.Dist
	}
	if !a.Visible || !a.Shootable {
		t *= 0.5
	}
	if a.Wounded {
		t *= 0.8
	}
	return t * a.Confidence
}

func (w *World) updateCombat() {
	s := &w.b.Self
	s.InCombat = w.now-s.LastDamage <= CombatMemory && s.LastDamage > 0
	for _, a := range w.actors {
		if a.Life != LifeAlive || !(a.cls.Hostile() || a.cls == nil && a.family != "" && a.family != "player") {
			continue
		}
		if (a.Visible && a.Dist < 2000) || (a.LastAttack > 0 && w.now-a.LastAttack <= CombatMemory) {
			s.InCombat = true
		}
	}
	for _, p := range w.projs {
		if p.visible && p.Danger && !p.Own {
			s.InCombat = true
		}
	}
}

func (w *World) effect(kind EffectKind, lump int, ref string) {
	for _, e := range w.b.Effects {
		if e.Kind == kind && e.Lump == lump && e.Ref == ref {
			return
		}
	}
	w.b.Effects = append(w.b.Effects, Effect{Kind: kind, Lump: lump, Ref: ref, At: w.now, Frame: w.b.ServerFrame})
}
