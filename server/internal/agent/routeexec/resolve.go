package routeexec

import (
	"fmt"
	"math"
	"strings"

	"quake2web/server/internal/agent/mapdata"
	"quake2web/server/internal/agent/nav"
	"quake2web/server/internal/agent/nav/navsim"
	"quake2web/server/internal/agent/route"
	"quake2web/server/internal/qcommon/shared"
)

// Vec3 is the game's vec3_t.
type Vec3 = shared.Vec3

// plan is a step resolved against the map data and the graph once, when
// the executor is made.
type plan struct {
	step *route.Step
	op   route.Op
	desc string

	// ent is the lump entity the step is about (-1: none): the goto,
	// touch, press, shoot or ride target, the kill's monster, the pickup's
	// item.
	ent int
	// blocker is ent's graph blocker (-1: none), pose the ride's (or a
	// mover goto's) pose index (-1: any).
	blocker int32
	pose    int
	// point is where the objective is (bearings, looking at it), hasPoint
	// whether there is one.
	point    Vec3
	hasPoint bool
	// radius is a goto's arrival radius.
	radius float32
	// class is the kill's or pickup's classname, pickup the item's pickup
	// name (for the inventory).
	class string
	// yaw is a face step's (or a directional touch's) facing.
	yaw    float32
	hasYaw bool
	// directional: a touch of a trigger that only fires for a player
	// facing along its movedir (yaw).
	directional bool
	// trigger is the touch target's volume (nil: none).
	trigger *mapdata.Box
	// seconds is a wait's duration (ms).
	seconds int64
	effects []effect
	// exit: the step claims the level's exit (its last step); reaching its
	// goal does not finish it, the level change does.
	exit bool
}

// optional reports an opportunistic step (route.Step.Optional): one
// attempt, skipped when it fails.
func (p *plan) optional() bool { return p.step != nil && p.step.Optional }

// effect is a claimed effect resolved to what the belief can show.
type effect struct {
	kind  route.EffectKind
	ent   int
	model string
	// blocker and pose: a door or mover the effect moves (pose -1: any
	// pose but its spawn pose).
	blocker int32
	pose    int
	point   Vec3
	desc    string
}

// gotoRadius is the default arrival radius of a goto at a point.
const gotoRadius = 48

func resolvePlans(t *route.Table, md *mapdata.Map, g *nav.Graph) ([]plan, error) {
	out := make([]plan, len(t.Steps))
	for i := range t.Steps {
		p, err := resolveStep(&t.Steps[i], md, g)
		if err != nil {
			return nil, fmt.Errorf("routeexec: %s step %d (%s): %w", t.Name, i, t.Steps[i].Op, err)
		}
		out[i] = p
	}
	return out, nil
}

func resolveStep(s *route.Step, md *mapdata.Map, g *nav.Graph) (plan, error) {
	p := plan{step: s, op: s.Op, ent: -1, blocker: -1, pose: -1, class: s.Class}
	var ent *mapdata.Entity
	if s.Target != nil {
		e, err := route.Resolve(*s.Target, md)
		if err != nil {
			return p, err
		}
		ent = e
	}
	if s.Yaw != nil {
		p.yaw, p.hasYaw = *s.Yaw, true
	}
	if s.Pos != nil {
		p.point, p.hasPoint = toVec(*s.Pos), true
	}
	switch s.Op {
	case route.OpGoto:
		p.radius = gotoRadius
		if s.Radius > 0 {
			p.radius = s.Radius
		}
		if ent != nil {
			p.ent = ent.Index
			p.blocker = g.BlockerOf(ent.Index)
			p.point, p.hasPoint = entityCenter(md, ent), true
			p.desc = "go to " + entName(ent)
		} else if !p.hasPoint {
			return p, fmt.Errorf("goto without target or pos")
		} else {
			p.desc = fmt.Sprintf("go to (%.0f %.0f %.0f)", p.point[0], p.point[1], p.point[2])
		}
	case route.OpTouch, route.OpPress, route.OpShoot:
		if ent == nil {
			return p, fmt.Errorf("%s without target", s.Op)
		}
		p.ent = ent.Index
		p.point, p.hasPoint = entityCenter(md, ent), true
		if tr := md.Trigger(ent.Index); tr != nil {
			if tr.HasVolume {
				box := tr.Box
				p.trigger = &box
			}
			if tr.Directional() {
				p.directional = true
				if !p.hasYaw {
					if y, ok := navsim.YawTo(Vec3{}, tr.Movedir); ok {
						p.yaw, p.hasYaw = y, true
					}
				}
			}
		}
		verb := map[route.Op]string{route.OpTouch: "touch", route.OpPress: "press", route.OpShoot: "shoot"}[s.Op]
		p.desc = verb + " " + entName(ent)
	case route.OpRide:
		if ent == nil {
			return p, fmt.Errorf("ride without target")
		}
		p.ent = ent.Index
		p.blocker = g.BlockerOf(ent.Index)
		if p.blocker < 0 {
			return p, fmt.Errorf("ride: %s is not a mover of the graph", entName(ent))
		}
		p.pose = PoseIndex(&g.Blockers[p.blocker], s.Until)
		if p.pose < 0 {
			return p, fmt.Errorf("ride: %s has no pose %q", entName(ent), s.Until)
		}
		p.point, p.hasPoint = entityCenter(md, ent), true
		p.desc = fmt.Sprintf("ride %s to %s", entName(ent), g.Blockers[p.blocker].Poses[p.pose].Name)
	case route.OpWait:
		p.seconds = int64(s.Seconds * 1000)
		p.desc = fmt.Sprintf("wait %.1fs", s.Seconds)
	case route.OpFace:
		if !p.hasYaw {
			return p, fmt.Errorf("face without yaw")
		}
		p.desc = fmt.Sprintf("face %.0f", p.yaw)
	case route.OpKill:
		m, err := resolveMonster(s, ent, md)
		if err != nil {
			return p, err
		}
		p.ent, p.point, p.hasPoint = m.Entity, m.Origin, true
		p.desc = fmt.Sprintf("kill %s #%d", m.Classname, m.Entity)
	case route.OpPickup:
		it := resolveItem(s, md)
		if it == nil {
			return p, fmt.Errorf("no item %s", s.Class)
		}
		p.ent, p.point, p.hasPoint = it.Entity, it.Origin, true
		p.desc = fmt.Sprintf("pick up %s #%d", it.Classname, it.Entity)
	case route.OpConfirm:
		p.desc = "confirm"
	default:
		return p, fmt.Errorf("unknown op %q", s.Op)
	}
	for k := range s.Effects {
		f, err := resolveEffect(&s.Effects[k], md, g)
		if err != nil {
			return p, fmt.Errorf("effect %d: %w", k, err)
		}
		p.effects = append(p.effects, f)
		p.exit = p.exit || f.kind == route.EffExit
	}
	if (s.Op == route.OpConfirm || s.Op == route.OpWait) && len(p.effects) > 0 {
		var ds []string
		for _, f := range p.effects {
			ds = append(ds, f.desc)
		}
		if s.Op == route.OpConfirm {
			p.desc = "confirm " + strings.Join(ds, ", ")
		} else {
			p.desc += " for " + strings.Join(ds, ", ")
		}
	}
	return p, nil
}

func resolveEffect(f *route.Effect, md *mapdata.Map, g *nav.Graph) (effect, error) {
	ent, err := route.Resolve(f.Target, md)
	if err != nil {
		return effect{}, err
	}
	e := effect{kind: f.Kind, ent: ent.Index, model: ent.Model, blocker: g.BlockerOf(ent.Index), pose: -1, point: entityCenter(md, ent)}
	e.desc = string(f.Kind) + " " + entName(ent)
	switch f.Kind {
	case route.EffMoverAt:
		if e.blocker < 0 {
			return e, fmt.Errorf("moverAt: %s is not a mover of the graph", entName(ent))
		}
		e.pose = PoseIndex(&g.Blockers[e.blocker], f.Pose)
		if e.pose < 0 {
			return e, fmt.Errorf("moverAt: %s has no pose %q", entName(ent), f.Pose)
		}
	case route.EffLaserOff, route.EffLaserOn:
		if l := md.Laser(ent.Index); l != nil {
			e.point = mid(l.Start, l.End)
		}
	}
	return e, nil
}

// resolveMonster returns the monster a kill step means: its target when it
// names one (of the step's class), else the nearest monster of the class
// to the step's spawn origin.
func resolveMonster(s *route.Step, ent *mapdata.Entity, md *mapdata.Map) (*mapdata.Monster, error) {
	var best *mapdata.Monster
	bd := float32(math.MaxFloat32)
	for i := range md.Monsters {
		m := &md.Monsters[i]
		if s.Class != "" && m.Classname != s.Class {
			continue
		}
		if e := md.Entity(m.Entity); e == nil || !e.Present() {
			continue
		}
		if ent != nil {
			if m.Entity == ent.Index {
				return m, nil
			}
			continue
		}
		d := float32(0)
		if s.Pos != nil {
			d = dist3(m.Origin, toVec(*s.Pos))
		}
		if d < bd {
			best, bd = m, d
		}
	}
	if best == nil {
		if ent != nil {
			return nil, fmt.Errorf("%s is not a present %s", entName(ent), s.Class)
		}
		return nil, fmt.Errorf("no monster %s", s.Class)
	}
	return best, nil
}

// resolveItem returns the item a pickup step means: of its class, the one
// nearest to its pos (the first when it gives none).
func resolveItem(s *route.Step, md *mapdata.Map) *mapdata.Item {
	var best *mapdata.Item
	bd := float32(math.MaxFloat32)
	for i := range md.Items {
		it := &md.Items[i]
		if it.Classname != s.Class {
			continue
		}
		if e := md.Entity(it.Entity); e == nil || !e.Present() {
			continue
		}
		d := float32(0)
		if s.Pos != nil {
			d = dist3(it.Origin, toVec(*s.Pos))
		}
		if d < bd {
			best, bd = it, d
		}
	}
	return best
}

// PoseIndex maps a route pose name ("pos1", "pos2", "top", "bottom", a
// train corner; "" means "pos2") to the index of blocker b's pose, -1 when
// b has none of that name. For a plat "pos1" is its top and "pos2" its
// bottom, the way the game's func_plat spawn names them.
func PoseIndex(b *nav.Blocker, name string) int {
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

// entityCenter is where an entity is: a mover's or trigger's box center
// (at its spawn pose), else its origin.
func entityCenter(md *mapdata.Map, e *mapdata.Entity) Vec3 {
	if mv := md.Mover(e.Index); mv != nil {
		return mv.Box.Center()
	}
	if tr := md.Trigger(e.Index); tr != nil && tr.HasVolume {
		return tr.Box.Center()
	}
	if l := md.Laser(e.Index); l != nil {
		return mid(l.Start, l.End)
	}
	return e.Origin
}

func entName(e *mapdata.Entity) string {
	if e.Model != "" {
		return e.Classname + " " + e.Model
	}
	return fmt.Sprintf("%s #%d", e.Classname, e.Index)
}

func toVec(v route.Vec) Vec3 { return Vec3{v[0], v[1], v[2]} }

func mid(a, b Vec3) Vec3 { return Vec3{(a[0] + b[0]) / 2, (a[1] + b[1]) / 2, (a[2] + b[2]) / 2} }

func dist3(a, b Vec3) float32 {
	dx, dy, dz := float64(a[0]-b[0]), float64(a[1]-b[1]), float64(a[2]-b[2])
	return float32(math.Sqrt(dx*dx + dy*dy + dz*dz))
}

func distH(a, b Vec3) float32 {
	return float32(math.Hypot(float64(a[0]-b[0]), float64(a[1]-b[1])))
}

func add(a, b Vec3) Vec3 { return Vec3{a[0] + b[0], a[1] + b[1], a[2] + b[2]} }
