package navbuild

import (
	"fmt"
	"math"
	"strings"

	"quake2web/server/internal/agent/mapdata"
	"quake2web/server/internal/agent/nav"
	"quake2web/server/internal/agent/route"
	"quake2web/server/internal/qcommon/shared"
)

// RouteProblem is a route step whose target the graph does not reach.
type RouteProblem struct {
	Table string
	Step  int
	Op    route.Op
	What  string
	Msg   string
}

func (p RouteProblem) String() string {
	return fmt.Sprintf("%s step %d (%s %s): %s", p.Table, p.Step, p.Op, p.What, p.Msg)
}

// Reach ranges of the route coverage check.
const (
	gotoRadius  = 64
	killRadius  = 512
	shootRadius = 768
)

// RouteStates returns the blocker states a route check starts from: the
// spawn state, except that movers a player sets off by walking up to them
// or that run by themselves (auto doors, touch plats and buttons, trains)
// may be in any pose.
func RouteStates(g *nav.Graph, md *mapdata.Map) []nav.StateMask {
	st := g.SpawnStates()
	for i := range g.Blockers {
		b := &g.Blockers[i]
		mv := md.Mover(md.TeamMaster(int(b.Entity)))
		if mv != nil && mv.Activation&(mapdata.ActTouch|mapdata.ActAuto) != 0 {
			st[i] = b.All()
		}
	}
	return st
}

// CheckRoute replays the steps of a route table over the graph (for the
// table's skill-resolved graph g of map md): from the arrival spawn, each
// step's target must be reachable over edges whose conditions hold in the
// blocker states the earlier steps' effects produced (doors opened,
// movers moved, lasers off, walls removed). It returns the steps that are
// not covered:
//
//   - goto a mover: a node on it, an edge through it, or its door trigger;
//     goto a position: a node within 64 units;
//   - touch a trigger / press a button / pickup an item: an edge with that
//     effect;
//   - ride: a ride edge of that mover arriving at the until pose;
//   - kill / shoot: a node within 512 / 768 units of the target.
func CheckRoute(g *nav.Graph, md *mapdata.Map, t *route.Table) ([]RouteProblem, error) {
	sp, ok := md.SpawnPoint(t.From)
	if !ok {
		return nil, fmt.Errorf("navbuild: %s: no spawn point %q", t.Name, t.From)
	}
	start := nav.NoNode
	for _, s := range g.Spawns {
		if int(s.Entity) == sp.Entity {
			start = s.Node
		}
	}
	if start == nav.NoNode {
		return []RouteProblem{{Table: t.Name, Step: -1, Op: "spawn", What: fmt.Sprintf("#%d", sp.Entity), Msg: "spawn point has no node"}}, nil
	}
	states := RouteStates(g, md)
	var probs []RouteProblem
	for si := range t.Steps {
		s := &t.Steps[si]
		seen := g.Reachable([]nav.NodeID{start}, func(e *nav.Edge) bool { return nav.Holds(e, states) })
		reached := func(e *nav.Edge) bool { return seen[e.From] && nav.Holds(e, states) }
		what := ""
		var ent *mapdata.Entity
		if s.Target != nil {
			var err error
			if ent, err = route.Resolve(*s.Target, md); err != nil {
				return nil, fmt.Errorf("navbuild: %s step %d: %w", t.Name, si, err)
			}
			what = ent.String()
		}
		fail := func(format string, args ...any) {
			probs = append(probs, RouteProblem{Table: t.Name, Step: si, Op: s.Op, What: what, Msg: fmt.Sprintf(format, args...)})
		}
		switch s.Op {
		case route.OpGoto:
			switch {
			case ent != nil:
				if !moverCovered(g, md, ent.Index, seen, reached) {
					fail("no reachable node on it, edge through it or door trigger")
				}
			case s.Pos != nil:
				r := float32(gotoRadius)
				if s.Radius > 0 {
					r = s.Radius
				}
				if !nodeNear(g, seen, toVec(*s.Pos), r) {
					fail("no reachable node within %g of %v", r, *s.Pos)
				}
			}
		case route.OpTouch, route.OpPress:
			if ent != nil && !effectReached(g, ent.Index, states, reached) {
				fail("no reachable edge sets it off")
			}
		case route.OpPickup:
			it := findItem(md, s)
			if it == nil {
				fail("no item %s", s.Class)
			} else {
				what = fmt.Sprintf("#%d %s", it.Entity, it.Classname)
				if !effectReached(g, it.Entity, states, reached) {
					fail("no reachable edge picks it up")
				}
			}
		case route.OpRide:
			if ent != nil && !rideReached(g, ent.Index, s.Until, reached) {
				fail("no reachable ride to %q", s.Until)
			}
		case route.OpKill, route.OpShoot:
			var p mapdata.Vec3
			switch {
			case s.Pos != nil:
				p = toVec(*s.Pos)
			case ent != nil:
				p = entityCenter(md, ent)
			}
			r := float32(killRadius)
			if s.Op == route.OpShoot {
				r = shootRadius
			}
			if !nodeNear(g, seen, p, r) {
				fail("no reachable node within %g of %v", r, p)
			}
		}
		applyEffects(g, md, s.Effects, states)
	}
	return probs, nil
}

func toVec(v route.Vec) mapdata.Vec3 { return mapdata.Vec3{v[0], v[1], v[2]} }

func nodeNear(g *nav.Graph, seen []bool, p mapdata.Vec3, r float32) bool {
	for _, c := range g.Nearby(p, r) {
		if seen[c.Node] && dist3(g.Nodes[c.Node].Origin, p) <= r {
			return true
		}
	}
	return false
}

func entityCenter(md *mapdata.Map, e *mapdata.Entity) mapdata.Vec3 {
	if mv := md.Mover(e.Index); mv != nil {
		return mv.Box.Center()
	}
	if tr := md.Trigger(e.Index); tr != nil && tr.HasVolume {
		return tr.Box.Center()
	}
	return e.Origin
}

func moverCovered(g *nav.Graph, md *mapdata.Map, ent int, seen []bool, reached func(*nav.Edge) bool) bool {
	bi := g.BlockerOf(ent)
	master := md.TeamMaster(ent)
	for i := range g.Nodes {
		if seen[i] && bi >= 0 && g.Nodes[i].Blocker == bi {
			return true
		}
	}
	for i := range g.Edges {
		e := &g.Edges[i]
		if !reached(e) {
			continue
		}
		for _, r := range e.Reqs {
			if r.Blocker == bi && bi >= 0 {
				return true
			}
		}
		for _, f := range e.Effects {
			if f.Kind == nav.EffDoorTrigger && int(f.Entity) == master {
				return true
			}
		}
	}
	if mv := md.Mover(ent); mv != nil && bi < 0 {
		// a static mover (button, wall): a node next to it
		return nodeNear(g, seen, mv.Box.Center(), gotoRadius+mv.Size[0]/2+mv.Size[1]/2)
	}
	return false
}

func effectReached(g *nav.Graph, ent int, states []nav.StateMask, reached func(*nav.Edge) bool) bool {
	for i := range g.Edges {
		e := &g.Edges[i]
		if !reached(e) {
			continue
		}
		for k := range e.Effects {
			if int(e.Effects[k].Entity) == ent && e.Effects[k].Holds(states) {
				return true
			}
		}
	}
	return false
}

func rideReached(g *nav.Graph, ent int, until string, reached func(*nav.Edge) bool) bool {
	bi := g.BlockerOf(ent)
	if bi < 0 {
		return false
	}
	want := poseIndex(&g.Blockers[bi], until)
	for i := range g.Edges {
		e := &g.Edges[i]
		if e.Kind != nav.EdgeRide || !reached(e) {
			continue
		}
		to := &g.Nodes[e.To]
		if to.Blocker == bi && (want < 0 || int(to.Pose) == want) {
			return true
		}
	}
	return false
}

// poseIndex maps a route pose name to the blocker's pose index (-1: any).
func poseIndex(b *nav.Blocker, name string) int {
	switch strings.ToLower(name) {
	case "":
		name = "pos2"
	case "top":
		name = "pos1"
	case "bottom":
		name = "pos2"
	}
	if b.Kind == nav.BlockPlat {
		switch name {
		case "pos1":
			name = "top"
		case "pos2":
			name = "bottom"
		}
	}
	for i, p := range b.Poses {
		if shared.Q_stricmp(p.Name, name) == 0 {
			return i
		}
	}
	return -1
}

func findItem(md *mapdata.Map, s *route.Step) *mapdata.Item {
	var best *mapdata.Item
	bd := math.MaxFloat64
	for k := range md.Items {
		it := &md.Items[k]
		if it.Classname != s.Class {
			continue
		}
		d := 0.0
		if s.Pos != nil {
			d = float64(dist3(it.Origin, toVec(*s.Pos)))
		}
		if d < bd {
			best, bd = it, d
		}
	}
	return best
}

// applyEffects updates the blocker states with a step's claimed effects:
// a door opens (pos2 becomes possible), a mover moves to a pose, a laser is
// switched off or on, a wall is removed.
func applyEffects(g *nav.Graph, md *mapdata.Map, effs []route.Effect, states []nav.StateMask) {
	for _, f := range effs {
		e, err := route.Resolve(f.Target, md)
		if err != nil {
			continue
		}
		var ents []int
		switch f.Kind {
		case route.EffDoorOpen:
			// the whole team opens
			if team := md.Team(md.TeamMaster(e.Index)); len(team) > 0 {
				ents = team
			} else {
				ents = []int{e.Index}
			}
		default:
			ents = []int{e.Index}
		}
		for _, ei := range ents {
			bi := g.BlockerOf(ei)
			if bi < 0 {
				continue
			}
			b := &g.Blockers[bi]
			switch f.Kind {
			case route.EffDoorOpen:
				states[bi] |= nav.Pose(len(b.Poses) - 1)
			case route.EffMoverAt:
				if k := poseIndex(b, f.Pose); k >= 0 {
					states[bi] |= nav.Pose(k)
				}
			case route.EffLaserOff, route.EffRemove:
				states[bi] = nav.StateGone
			case route.EffLaserOn:
				states[bi] = nav.Pose(0)
			}
		}
	}
}
