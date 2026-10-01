package navbuild

import (
	"fmt"
	"sort"
	"strings"

	"quake2web/server/internal/agent/mapdata"
	"quake2web/server/internal/agent/nav"
	"quake2web/server/internal/agent/nav/navsim"
	"quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/shared"
)

type Vec3 = shared.Vec3

// The scene is everything besides the world BSP that the graph depends on,
// merged over the four skills (an entity counts once, with the mask of the
// skills it spawns at).

// blockerGeo is the collision geometry of a condition blocker.
type blockerGeo struct {
	solidID  int
	headnode int32
	mins     Vec3
	maxs     Vec3
	mask     int32 // MASK_PLAYERSOLID, or MASK_WATER for func_water
	laser    bool
	start    dvec
	end      dvec
	poseMin  []Vec3
	poseMax  []Vec3
	umin     Vec3
	umax     Vec3
	// tops: the mover is a translating brush model whose top faces can be
	// stood on (mover-top nodes per pose)
	tops bool
	// solid at a pose: it blocks movement (not water, not a laser)
	solid bool
	// travel is the mover's Pos1 -> Pos2 time (s), speed its move speed
	// (units/s for trains)
	travel, speed float32
}

// vol is a box edges report entering.
type vol struct {
	kind    nav.EffectKind
	entity  int32
	pose    int8 // item on a mover: the mover pose (-1: static)
	blocker int32
	min     Vec3
	max     Vec3
}

type spawnInfo struct {
	entity     int32
	targetname string
	origin     Vec3
	skills     uint8
}

type teleInfo struct {
	entity int32
	min    Vec3
	max    Vec3
	dest   Vec3
	angles Vec3
}

type scene struct {
	maps     [4]*mapdata.Map
	blockers []nav.Blocker
	geo      []blockerGeo
	statics  []navsim.Solid
	vols     []vol
	pushes   []navsim.Push
	teles    []teleInfo
	buttons  map[int]bool
	// touchButtons are the buttons a touch presses (button_touch)
	touchButtons map[int]bool
	ents     []nav.Ent
	spawns   []spawnInfo
	// moverByID maps a solid ID (entity) of a blocker to its index.
	blockerByID map[int]int32
}

func skillBit(s int) uint8 { return 1 << uint(s) }

func newScene(maps [4]*mapdata.Map, f *fileGeo) (*scene, error) {
	sc := &scene{maps: maps, buttons: map[int]bool{}, touchButtons: map[int]bool{}, blockerByID: map[int]int32{}}
	base := maps[1]

	// movers and lasers, union over skills in entity order
	type moverAt struct {
		mv     *mapdata.Mover
		skills uint8
		killed bool
	}
	movers := map[int]*moverAt{}
	lasers := map[int]*struct {
		l      *mapdata.Laser
		skills uint8
		killed bool
	}{}
	for s, m := range maps {
		for i := range m.Movers {
			mv := &m.Movers[i]
			if movers[mv.Entity] == nil {
				movers[mv.Entity] = &moverAt{mv: mv}
			}
			movers[mv.Entity].skills |= skillBit(s)
			if killtargeted(m, mv.Entity) {
				movers[mv.Entity].killed = true
			}
		}
		for i := range m.Lasers {
			l := &m.Lasers[i]
			if lasers[l.Entity] == nil {
				lasers[l.Entity] = &struct {
					l      *mapdata.Laser
					skills uint8
					killed bool
				}{l: l}
			}
			lasers[l.Entity].skills |= skillBit(s)
		}
	}
	ids := sortedKeys(movers)
	for _, id := range ids {
		ma := movers[id]
		if err := sc.addMover(ma.mv, ma.skills, ma.killed); err != nil {
			return nil, err
		}
	}
	for _, id := range sortedKeys(lasers) {
		l := lasers[id]
		b := nav.Blocker{Entity: int32(l.l.Entity), Class: "target_laser", Kind: nav.BlockLaser, Gone: true,
			Poses: []nav.BlockerPose{{Name: "on", Origin: l.l.Start}}, Skills: l.skills, Start: l.l.Start, End: l.l.End}
		if !l.l.StartOn {
			b.Spawn = -1
		}
		g := blockerGeo{solidID: l.l.Entity, laser: true, start: toD(l.l.Start), end: toD(l.l.End)}
		var mn, mx Vec3
		for k := 0; k < 3; k++ {
			mn[k], mx[k] = min32(l.l.Start[k], l.l.End[k])-1, max32(l.l.Start[k], l.l.End[k])+1
		}
		g.poseMin, g.poseMax, g.umin, g.umax = []Vec3{mn}, []Vec3{mx}, mn, mx
		sc.addBlocker(b, g)
	}

	// static boxes: barrels (dropped to the floor), satellite dishes,
	// teleporter pads
	sc.addPointSolids(f)

	// trigger volumes, hooks
	trig := map[int]*struct {
		t      *mapdata.Trigger
		skills uint8
	}{}
	for s, m := range maps {
		for i := range m.Triggers {
			t := &m.Triggers[i]
			if !t.HasVolume {
				continue
			}
			if trig[t.Entity] == nil {
				trig[t.Entity] = &struct {
					t      *mapdata.Trigger
					skills uint8
				}{t: t}
			}
			trig[t.Entity].skills |= skillBit(s)
		}
	}
	for _, id := range sortedKeys(trig) {
		t := trig[id].t
		switch t.Classname {
		case "trigger_monsterjump":
			continue // monsters only
		case "trigger_push":
			sc.pushes = append(sc.pushes, navsim.Push{ID: t.Entity, Min: t.Box.Min, Max: t.Box.Max,
				Velocity: shared.VectorScale(t.Movedir, float32(float64(t.Speed)*10)), Once: t.Spawnflags&mapdata.PushOnce != 0})
		}
		sc.vols = append(sc.vols, vol{kind: nav.EffTrigger, entity: int32(t.Entity), pose: -1, blocker: -1, min: t.Box.Min, max: t.Box.Max})
		sc.addEnt(t.Entity, t.Classname, t.Model, trig[id].skills)
	}
	// door and plat triggers
	for _, id := range ids {
		ma := movers[id]
		if ma.mv.Trigger == nil {
			continue
		}
		k := nav.EffDoorTrigger
		if ma.mv.Kind == mapdata.MoverPlat {
			k = nav.EffPlatTrigger
		}
		sc.vols = append(sc.vols, vol{kind: k, entity: int32(id), pose: -1, blocker: -1, min: ma.mv.Trigger.Min, max: ma.mv.Trigger.Max})
		sc.addEnt(id, ma.mv.Classname, ma.mv.Model, ma.skills)
	}
	for _, id := range ids {
		if ma := movers[id]; ma.mv.Kind == mapdata.MoverButton {
			sc.addEnt(id, ma.mv.Classname, ma.mv.Model, ma.skills)
		}
	}
	sc.addItems(f)
	sc.addTeleporters()

	// spawn points
	sp := map[int]*spawnInfo{}
	for s, m := range maps {
		for _, x := range m.Spawns {
			if sp[x.Entity] == nil {
				sp[x.Entity] = &spawnInfo{entity: int32(x.Entity), targetname: x.Targetname, origin: x.Origin}
			}
			sp[x.Entity].skills |= skillBit(s)
		}
	}
	for _, id := range sortedKeys(sp) {
		sc.spawns = append(sc.spawns, *sp[id])
	}
	sort.Slice(sc.ents, func(i, j int) bool { return sc.ents[i].Entity < sc.ents[j].Entity })
	if base == nil {
		return nil, fmt.Errorf("navbuild: no skill 1 map data")
	}
	return sc, nil
}

func killtargeted(m *mapdata.Map, ent int) bool {
	for _, l := range m.FiredBy(ent) {
		if l.Kind == mapdata.LinkKill {
			return true
		}
	}
	return false
}

func (sc *scene) addEnt(ent int, class, model string, skills uint8) {
	for i := range sc.ents {
		if int(sc.ents[i].Entity) == ent {
			sc.ents[i].Skills |= skills
			return
		}
	}
	sc.ents = append(sc.ents, nav.Ent{Entity: int32(ent), Class: class, Model: model, Skills: skills})
}

func (sc *scene) addBlocker(b nav.Blocker, g blockerGeo) {
	if len(g.poseMin) > 0 {
		g.umin, g.umax = g.poseMin[0], g.poseMax[0]
		for k := range g.poseMin {
			for i := 0; i < 3; i++ {
				g.umin[i] = min32(g.umin[i], g.poseMin[k][i])
				g.umax[i] = max32(g.umax[i], g.poseMax[k][i])
			}
		}
	}
	sc.blockerByID[g.solidID] = int32(len(sc.blockers))
	sc.blockers = append(sc.blockers, b)
	sc.geo = append(sc.geo, g)
}

// addMover classifies a mover as a condition blocker or a static solid.
func (sc *scene) addMover(mv *mapdata.Mover, skills uint8, killed bool) error {
	b := nav.Blocker{Entity: int32(mv.Entity), Class: mv.Classname, Model: mv.Model, Skills: skills, Gone: killed,
		Headnode: mv.Headnode, Mins: mv.Mins, Maxs: mv.Maxs, Solid: true}
	g := blockerGeo{solidID: mv.Entity, headnode: mv.Headnode, mins: mv.Mins, maxs: mv.Maxs, mask: q2const.MASK_PLAYERSOLID, solid: true,
		travel: mv.TravelTime, speed: mv.Speed}
	pose := func(name string, o, a Vec3) { b.Poses = append(b.Poses, nav.BlockerPose{Name: name, Origin: o, Angles: a}) }
	static := func() {
		if mv.Solid && strings.HasPrefix(mv.Model, "*") {
			sc.statics = append(sc.statics, navsim.Solid{ID: mv.Entity, Headnode: mv.Headnode, Origin: mv.Origin, Angles: mv.Angles, Mins: mv.Mins, Maxs: mv.Maxs})
		}
	}
	switch mv.Kind {
	case mapdata.MoverDoor:
		b.Kind = nav.BlockDoor
		if mv.Classname == "func_water" {
			b.Kind, g.mask, g.solid, b.Solid = nav.BlockWater, q2const.MASK_WATER, false, false
		}
		pose("pos1", mv.Pos1, mv.Angles)
		pose("pos2", mv.Pos2, mv.Angles)
		g.tops = g.solid && mv.Angles == (Vec3{})
	case mapdata.MoverDoorRotating:
		b.Kind = nav.BlockRotating
		pose("pos1", mv.Origin, mv.Pos1)
		pose("pos2", mv.Origin, mv.Pos2)
	case mapdata.MoverDoorSecret:
		b.Kind = nav.BlockSecret
		pose("closed", mv.Origin, mv.Angles)
		pose("pos1", mv.Pos1, mv.Angles)
		pose("pos2", mv.Pos2, mv.Angles)
		g.tops = mv.Angles == (Vec3{})
	case mapdata.MoverPlat:
		b.Kind = nav.BlockPlat
		pose("top", mv.Pos1, mv.Angles)
		pose("bottom", mv.Pos2, mv.Angles)
		g.tops = true
	case mapdata.MoverTrain:
		b.Kind = nav.BlockTrain
		for i, p := range mv.Path {
			if i >= nav.MaxPoses {
				break
			}
			name := p.Targetname
			if name == "" {
				name = fmt.Sprintf("corner%d", i)
			}
			pose(name, p.Origin, mv.Angles)
		}
		if len(b.Poses) == 0 {
			pose("spawn", mv.Origin, mv.Angles)
		}
		g.tops = mv.Angles == (Vec3{})
	case mapdata.MoverWall:
		if mv.Activation&mapdata.ActUse == 0 && mv.Solid && !killed {
			static()
			return nil
		}
		b.Kind, b.Gone = nav.BlockWall, true
		pose("on", mv.Origin, mv.Angles)
		if !mv.Solid {
			b.Spawn = -1
		}
	case mapdata.MoverExplosive:
		b.Kind, b.Gone = nav.BlockExplosive, true
		pose("intact", mv.Origin, mv.Angles)
		if !mv.Solid {
			b.Spawn = -1
		}
	case mapdata.MoverButton:
		sc.buttons[mv.Entity] = true
		if mv.Activation&mapdata.ActTouch != 0 {
			sc.touchButtons[mv.Entity] = true
		}
		static()
		return nil
	default: // rotating, object: solid at their spawn pose
		static()
		return nil
	}
	if b.Spawn == 0 && len(b.Poses) > 1 {
		b.Spawn = closestPose(b.Poses, mv.Origin, mv.Angles)
	}
	for _, p := range b.Poses {
		mn, mx := navsim.LinkBox(true, p.Origin, p.Angles, mv.Mins, mv.Maxs)
		g.poseMin, g.poseMax = append(g.poseMin, mn), append(g.poseMax, mx)
	}
	sc.addBlocker(b, g)
	return nil
}

func closestPose(poses []nav.BlockerPose, o, a Vec3) int8 {
	best, bd := 0, float32(-1)
	for i, p := range poses {
		var d float32
		for k := 0; k < 3; k++ {
			d += abs32(p.Origin[k]-o[k]) + abs32(p.Angles[k]-a[k])
		}
		if bd < 0 || d < bd {
			best, bd = i, d
		}
	}
	return int8(best)
}

// addPointSolids adds the SOLID_BBOX point entities that block players:
// misc_explobox (after M_droptofloor), misc_satellite_dish and the
// teleporter pads. Dead soldiers are SVF_DEADMONSTER, which
// MASK_PLAYERSOLID ignores; monsters move and are left out.
func (sc *scene) addPointSolids(f *fileGeo) {
	seen := map[int]bool{}
	w := f.world(sc)
	for _, m := range sc.maps {
		for i := range m.Entities {
			e := &m.Entities[i]
			if !e.Present() || seen[i] {
				continue
			}
			var mins, maxs Vec3
			origin := e.Origin
			switch e.Classname {
			case "misc_explobox":
				mins, maxs = Vec3{-16, -16, 0}, Vec3{16, 16, 40}
				// C: game/g_monster.c:250 M_droptofloor (two frames after spawn)
				start := origin
				start[2]++
				end := start
				end[2] -= 256
				tr := w.Trace(start, mins, maxs, end, q2const.MASK_MONSTERSOLID)
				origin = start
				if tr.Fraction < 1 && !tr.AllSolid {
					origin = tr.EndPos
				}
			case "misc_satellite_dish":
				mins, maxs = Vec3{-64, -64, 0}, Vec3{64, 64, 128}
			case "misc_teleporter", "misc_teleporter_dest":
				if e.Classname == "misc_teleporter" && e.Target == "" {
					continue
				}
				mins, maxs = Vec3{-32, -32, -24}, Vec3{32, 32, -16}
			default:
				continue
			}
			seen[i] = true
			sc.statics = append(sc.statics, navsim.Solid{ID: i, Box: true, Origin: origin, Mins: mins, Maxs: maxs})
		}
	}
	sort.SliceStable(sc.statics, func(a, b int) bool { return sc.statics[a].ID < sc.statics[b].ID })
}

// addItems adds the item touch boxes: droptofloor's box after the drop,
// one per pose of the mover the item rests on.
// C: game/g_items.c:1050 droptofloor
func (sc *scene) addItems(f *fileGeo) {
	type itemAt struct {
		it     mapdata.Item
		skills uint8
	}
	items := map[int]*itemAt{}
	for s, m := range sc.maps {
		for _, it := range m.Items {
			if items[it.Entity] == nil {
				items[it.Entity] = &itemAt{it: it}
			}
			items[it.Entity].skills |= skillBit(s)
		}
	}
	w := f.world(sc)
	// solid movers at their spawn pose (droptofloor clips against them)
	for i := range sc.blockers {
		b, g := &sc.blockers[i], &sc.geo[i]
		if !g.solid || g.laser || b.Spawn < 0 {
			continue
		}
		p := b.Poses[b.Spawn]
		w.AddSolid(navsim.Solid{ID: g.solidID, Headnode: g.headnode, Origin: p.Origin, Angles: p.Angles, Mins: g.mins, Maxs: g.maxs})
	}
	mins, maxs := Vec3{-15, -15, -15}, Vec3{15, 15, 15}
	for _, id := range sortedKeys(items) {
		it := items[id]
		o := it.it.Origin
		end := o
		end[2] -= 128
		tr := w.Trace(o, mins, maxs, end, q2const.MASK_SOLID)
		if tr.StartSolid {
			continue // freed: "droptofloor: startsolid"
		}
		o = tr.EndPos
		mn, mx := navsim.LinkBox(false, o, Vec3{}, mins, maxs)
		sc.addEnt(id, it.it.Classname, "", it.skills)
		if bi, ok := sc.blockerByID[tr.Ent]; ok && tr.Fraction < 1 && sc.geo[bi].tops {
			b := &sc.blockers[bi]
			sp := b.Poses[max8(b.Spawn, 0)].Origin
			for k, p := range b.Poses {
				d := shared.VectorSubtract(p.Origin, sp)
				sc.vols = append(sc.vols, vol{kind: nav.EffItem, entity: int32(id), pose: int8(k), blocker: bi,
					min: shared.VectorAdd(mn, d), max: shared.VectorAdd(mx, d)})
			}
			continue
		}
		sc.vols = append(sc.vols, vol{kind: nav.EffItem, entity: int32(id), pose: -1, blocker: -1, min: mn, max: mx})
	}
}

// addTeleporters adds misc_teleporter triggers and their destinations.
// C: game/g_misc.c:1820 SP_misc_teleporter
func (sc *scene) addTeleporters() {
	m := sc.maps[1]
	for i := range m.Entities {
		e := &m.Entities[i]
		if !e.Present() || e.Classname != "misc_teleporter" || e.Target == "" {
			continue
		}
		var dest *mapdata.Entity
		for _, t := range m.Targets(e.Target) {
			if t.Classname == "misc_teleporter_dest" {
				dest = t
				break
			}
		}
		if dest == nil {
			continue
		}
		mn, mx := navsim.LinkBox(false, e.Origin, Vec3{}, Vec3{-8, -8, 8}, Vec3{8, 8, 24})
		sc.teles = append(sc.teles, teleInfo{entity: int32(i), min: mn, max: mx, dest: dest.Origin, angles: dest.Angles})
		sc.vols = append(sc.vols, vol{kind: nav.EffTrigger, entity: int32(i), pose: -1, blocker: -1, min: mn, max: mx})
		sc.addEnt(i, e.Classname, "", nav.AllSkills)
	}
}

// solidAt returns blocker bi at pose k as a navsim solid.
func (sc *scene) solidAt(bi int32, k int) navsim.Solid {
	b, g := &sc.blockers[bi], &sc.geo[bi]
	p := b.Poses[k]
	return navsim.Solid{ID: g.solidID, Headnode: g.headnode, Origin: p.Origin, Angles: p.Angles, Mins: g.mins, Maxs: g.maxs}
}

// spawnSolids returns every solid blocker in its spawn state (the level as
// it starts, at skill 1).
func (sc *scene) spawnSolids() []navsim.Solid {
	var out []navsim.Solid
	for i := range sc.blockers {
		b, g := &sc.blockers[i], &sc.geo[i]
		if g.laser || b.Spawn < 0 || b.Skills&skillBit(1) == 0 {
			continue
		}
		out = append(out, sc.solidAt(int32(i), int(b.Spawn)))
	}
	return out
}

func sortedKeys[V any](m map[int]V) []int {
	out := make([]int, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Ints(out)
	return out
}

func toD(v Vec3) dvec { return dvec{float64(v[0]), float64(v[1]), float64(v[2])} }

func min32(a, b float32) float32 {
	if a < b {
		return a
	}
	return b
}

func max32(a, b float32) float32 {
	if a > b {
		return a
	}
	return b
}

func abs32(a float32) float32 {
	if a < 0 {
		return -a
	}
	return a
}

func max8(a, b int8) int8 {
	if a > b {
		return a
	}
	return b
}
