// Package navsim simulates player movement for navigation: a collision
// World with the semantics of the server's SV_Trace / SV_PointContents, a
// Runner around the bit-exact pmove port, and the edge Executors (walk,
// crouch, jump, drop, ladder, swim, water jump, ride) that turn an edge into
// usercmds.
//
// The same executors run offline in the graph builder (navbuild), where a
// Runner steps pmove, and online in the bot's path follower, which turns
// each Cmd into a real usercmd with control.CmdAngles. Nothing here keeps
// package-level state; a World or Runner must not be used by two goroutines
// at once (each owns its cmodel.State), while the cmodel.Map is shared.
package navsim

import (
	"quake2web/server/internal/cmodel"
	"quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/shared"
)

// Vec3 is the game's vec3_t.
type Vec3 = shared.Vec3

// Version changes whenever a change in this package changes simulation
// results; it is part of the nav cache key.
const Version = 2 // 2: executors stop at the start, Run judges falls at every frame phase

// Player hull (PM_CheckDuck): 32 units wide, mins.z -24, maxs.z 32 standing
// and 4 ducked.
const (
	HullHalf  = 16
	HullMinZ  = -24
	StandMaxZ = 32
	DuckMaxZ  = 4
	// StepHeight is pmove's STEPSIZE.
	StepHeight = 18
)

// StandMins and friends return the player hull bounds.
func StandMins() Vec3 { return Vec3{-HullHalf, -HullHalf, HullMinZ} }

// StandMaxs is the standing hull top.
func StandMaxs() Vec3 { return Vec3{HullHalf, HullHalf, StandMaxZ} }

// DuckMaxs is the ducked hull top.
func DuckMaxs() Vec3 { return Vec3{HullHalf, HullHalf, DuckMaxZ} }

// WorldEnt is the entity id traces report for the world (the server's
// trace.ent = ge->edicts). pmove only treats an entity id < 0 as "no
// entity", so a world hit must never report cmodel's -1.
const WorldEnt = 0

// Solid is something other than the world BSP that the player collides
// with: an inline brush model at a pose (a mover) or an axis-aligned box.
type Solid struct {
	// ID is reported as Trace.Ent and in the pmove touch list. It must be
	// > 0; by convention it is the entity's lump index.
	ID int
	// Headnode is the inline model's collision headnode (BSP solids).
	Headnode int32
	// Box makes this a box solid (SV_HullForEntity's CM_HeadnodeForBox)
	// spanning Origin+Mins..Origin+Maxs; boxes never rotate.
	Box bool
	// Origin and Angles are the entity pose (s.origin, s.angles).
	Origin, Angles Vec3
	// Mins and Maxs are the model bounds (ent->mins/maxs) of a BSP solid or
	// the box bounds, relative to Origin.
	Mins, Maxs Vec3
	// Pushable marks a solid the player shoves by touching it
	// (misc_explobox's barrel_touch moves it a unit per frame); the
	// simulation treats it as static.
	Pushable bool

	absMin, absMax Vec3
}

// AbsBox returns the solid's link box (SV_LinkEdict: a rotated BSP model
// gets the cube of its largest extent; everything grows by 1).
func (s *Solid) AbsBox() (min, max Vec3) {
	return LinkBox(!s.Box, s.Origin, s.Angles, s.Mins, s.Maxs)
}

// LinkBox is the absmin/absmax SV_LinkEdict computes for an entity at
// origin/angles with the given bounds.
// C: server/sv_world.c:219 SV_LinkEdict
func LinkBox(bsp bool, origin, angles, mins, maxs Vec3) (min, max Vec3) {
	if bsp && (angles[0] != 0 || angles[1] != 0 || angles[2] != 0) {
		var m float32
		for i := 0; i < 3; i++ {
			if v := abs32(mins[i]); v > m {
				m = v
			}
			if v := abs32(maxs[i]); v > m {
				m = v
			}
		}
		for i := 0; i < 3; i++ {
			min[i] = origin[i] - m
			max[i] = origin[i] + m
		}
	} else {
		for i := 0; i < 3; i++ {
			min[i] = origin[i] + mins[i]
			max[i] = origin[i] + maxs[i]
		}
	}
	for i := 0; i < 3; i++ {
		min[i]--
		max[i]++
	}
	return min, max
}

func abs32(f float32) float32 {
	if f < 0 {
		return -f
	}
	return f
}

// World answers collision queries like the server does for a player: the
// world BSP (headnode 0) plus a list of solids at fixed poses. It owns its
// cmodel.State, so one World serves one goroutine.
type World struct {
	cs     *cmodel.State
	solids []Solid
}

// NewWorld returns a World over the (shared, immutable) collision map with
// no solids.
func NewWorld(m *cmodel.Map) *World {
	return &World{cs: cmodel.NewState(m)}
}

// Map returns the collision map.
func (w *World) Map() *cmodel.Map { return w.cs.Map() }

// State returns the World's own cmodel.State (for extra queries such as
// TransformedBoxTrace against a model that is not one of the solids).
func (w *World) State() *cmodel.State { return w.cs }

// SetSolids replaces the solids (the slice is copied).
func (w *World) SetSolids(solids []Solid) {
	w.solids = w.solids[:0]
	for _, s := range solids {
		w.AddSolid(s)
	}
}

// AddSolid adds one solid. IDs must be > 0.
func (w *World) AddSolid(s Solid) {
	if s.ID <= 0 {
		panic("navsim: solid id must be > 0")
	}
	s.absMin, s.absMax = s.AbsBox()
	w.solids = append(w.solids, s)
}

// Solids returns the current solids (read only).
func (w *World) Solids() []Solid { return w.solids }

// Trace sweeps the box mins/maxs from start to end like SV_Trace for a
// player (no passedict effects: the world never holds the player itself).
// Ent is WorldEnt for the world, including a trace that hits nothing, and
// the Solid's ID when a solid is hit first.
// C: server/sv_world.c:624 SV_Trace
func (w *World) Trace(start, mins, maxs, end Vec3, mask int32) shared.Trace {
	tr := w.cs.BoxTrace(start, end, mins, maxs, 0, mask)
	tr.Ent = WorldEnt
	if tr.Fraction == 0 || len(w.solids) == 0 {
		return tr // blocked by the world
	}
	var bmin, bmax Vec3
	for i := 0; i < 3; i++ {
		if end[i] > start[i] {
			bmin[i] = start[i] + mins[i] - 1
			bmax[i] = end[i] + maxs[i] + 1
		} else {
			bmin[i] = end[i] + mins[i] - 1
			bmax[i] = start[i] + maxs[i] + 1
		}
	}
	// C: server/sv_world.c:517 SV_ClipMoveToEntities
	for i := range w.solids {
		s := &w.solids[i]
		if tr.AllSolid {
			return tr
		}
		if s.absMin[0] > bmax[0] || s.absMin[1] > bmax[1] || s.absMin[2] > bmax[2] ||
			s.absMax[0] < bmin[0] || s.absMax[1] < bmin[1] || s.absMax[2] < bmin[2] {
			continue
		}
		head, angles := s.Headnode, s.Angles
		if s.Box {
			head, angles = w.cs.HeadnodeForBox(s.Mins, s.Maxs), Vec3{}
		}
		t := w.cs.TransformedBoxTrace(start, end, mins, maxs, head, mask, s.Origin, angles)
		if t.AllSolid || t.StartSolid || t.Fraction < tr.Fraction {
			t.Ent = s.ID
			if tr.StartSolid {
				tr = t
				tr.StartSolid = true
			} else {
				tr = t
			}
		} else if t.StartSolid {
			tr.StartSolid = true
		}
	}
	return tr
}

// PointContents ORs the world contents at p with those of every solid whose
// link box holds p, like SV_PointContents.
// C: server/sv_world.c:440 SV_PointContents
func (w *World) PointContents(p Vec3) int32 {
	c := w.cs.PointContents(p, 0)
	for i := range w.solids {
		s := &w.solids[i]
		if p[0] < s.absMin[0] || p[1] < s.absMin[1] || p[2] < s.absMin[2] ||
			p[0] > s.absMax[0] || p[1] > s.absMax[1] || p[2] > s.absMax[2] {
			continue
		}
		head := s.Headnode
		if s.Box {
			head = w.cs.HeadnodeForBox(s.Mins, s.Maxs)
		}
		c |= w.cs.TransformedPointContents(p, head, s.Origin, s.Angles)
	}
	return c
}

// Fits reports whether the hull mins/maxs fits at p (a position test that
// is not all solid against the player mask).
func (w *World) Fits(p, mins, maxs Vec3) bool {
	return !w.Trace(p, mins, maxs, p, q2const.MASK_PLAYERSOLID).AllSolid
}
