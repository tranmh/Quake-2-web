package mapdata

import (
	"sort"
	"strings"
)

// The entity logic graph: which entities fire (G_UseTargets) which others,
// what a used entity does, and how a chain can start. It follows the use
// and touch functions of g_trigger.c, g_func.c, g_target.c, g_misc.c,
// g_items.c and g_monster.c.

// Response is what an entity does when another entity uses it.
type Response uint8

const (
	RespNone    Response = iota // no use function (or one that ignores the call, like a door team slave)
	RespRelay                   // fires its own targets: trigger_relay, Use_Multi, target_secret/goal/explosion, func_timer, func_clock
	RespEnable                  // a TRIGGERED trigger_multiple/once becomes touchable (trigger_enable)
	RespCounter                 // trigger_counter: fires its targets on the Count-th use
	RespKey                     // trigger_key: fires its targets if the activator holds Item
	RespExit                    // target_changelevel: ends the level
	RespMove                    // door, plat, button, train start moving (doors fire their targets as they open, buttons when they arrive)
	RespToggle                  // target_laser, light, func_rotating, func_areaportal, toggle hurt or wall, func_conveyor
	RespSpawn                   // a trigger-spawned func_wall / func_explosive / func_object / monster appears
	RespExplode                 // a targeted func_explosive explodes, fires its targets and disappears
	RespWake                    // a monster wakes up and hunts the activator
	RespOther                   // any other use function (speaker, help, spawner, earthquake, ...)
)

// String returns the lower-case response name.
func (r Response) String() string {
	return enumName(int(r), "none", "relay", "enable", "counter", "key", "exit", "move", "toggle", "spawn", "explode", "wake", "other")
}

// Source is a way a logic chain starts without anything using the entity.
type Source uint8

const (
	SrcTouch  Source = iota + 1 // a client touches the trigger volume, button or door trigger box
	SrcShoot                    // the entity is damaged
	SrcDeath                    // the monster dies
	SrcPickup                   // the item is picked up
	SrcStart                    // level start: trigger_always, START_ON func_timer, target_crosslevel_target
	SrcCorner                   // a train or monster reaches the path_corner / point_combat
)

// String returns the lower-case source name.
func (s Source) String() string {
	return enumName(int(s), "?", "touch", "shoot", "death", "pickup", "start", "corner")
}

// LinkKind is how one entity sets off another.
type LinkKind uint8

const (
	// LinkTarget: From's G_UseTargets uses To (To's targetname is From's
	// target, deathtarget or pathtarget).
	LinkTarget LinkKind = iota + 1
	// LinkKill: From's killtarget removes To.
	LinkKill
	// LinkTeam: the door team master From moves its slave To with it.
	LinkTeam
	// LinkCarry: the mover From carries a player standing on it into the
	// trigger volume To.
	LinkCarry
)

// String returns "target", "kill", "team" or "carry".
func (k LinkKind) String() string { return enumName(int(k), "?", "target", "kill", "team", "carry") }

// enumName returns names[i], or "?" when i is out of range.
func enumName(i int, names ...string) string {
	if i < 0 || i >= len(names) {
		return "?"
	}
	return names[i]
}

// Link is one edge of the logic graph.
type Link struct {
	From, To int
	Kind     LinkKind
	// Delay is From's "delay": G_UseTargets fires the targets and
	// killtargets that many seconds later.
	Delay float32
}

// Fires returns the outgoing links of entity i in a stable order.
func (m *Map) Fires(i int) []Link { return m.fires[i] }

// FiredBy returns the incoming links of entity i.
func (m *Map) FiredBy(i int) []Link { return m.firedBy[i] }

// FiredTargetname returns the targetname G_UseTargets uses when entity i
// fires: a monster's deathtarget (or its target when that is not a
// path_corner / point_combat, which monster_start_go clears), the
// pathtarget of path_corner / point_combat / func_clock, the target of the
// other firing classes, "" for entities that never fire.
// C: game/g_monster.c:511 monster_death_use
func (m *Map) FiredTargetname(i int) string {
	e := &m.Entities[i]
	switch {
	case m.isMonster(e):
		if e.Deathtarget != "" {
			return e.Deathtarget
		}
		if e.Target == "" {
			return ""
		}
		// C: game/g_monster.c:583 monster_start_go (target fixups)
		ts := m.Targets(e.Target)
		for _, t := range ts {
			if t.Classname == "point_combat" {
				return ""
			}
		}
		if len(ts) == 0 || ts[0].Classname == "path_corner" {
			return ""
		}
		return e.Target
	case e.Classname == "path_corner" || e.Classname == "point_combat" || e.Classname == "func_clock":
		return e.Pathtarget
	case m.firesOwnTargets(e):
		return e.Target
	}
	return ""
}

// firesOwnTargets lists the classes whose code calls G_UseTargets(self).
func (m *Map) firesOwnTargets(e *Entity) bool {
	switch e.Classname {
	case "trigger_multiple", "trigger_once", "trigger_relay", "trigger_key", "trigger_counter", "trigger_always",
		"func_button", "func_door", "func_door_rotating", "func_water", "func_timer", "func_explosive",
		"target_secret", "target_goal", "target_explosion", "target_crosslevel_target", "misc_viper_bomb",
		"path_corner", "point_combat", "func_clock":
		return true
	}
	return m.isMonster(e) || m.isItem(e)
}

func (m *Map) isMonster(e *Entity) bool {
	return strings.HasPrefix(e.Classname, "monster_") && e.Classname != "monster_commander_body"
}

func (m *Map) isItem(e *Entity) bool {
	c := e.Classname
	return strings.HasPrefix(c, "item_") || strings.HasPrefix(c, "weapon_") ||
		strings.HasPrefix(c, "ammo_") || strings.HasPrefix(c, "key_")
}

// Response returns what entity i does when used.
func (m *Map) Response(i int) Response {
	e := m.Entity(i)
	if e == nil || !e.Present() {
		return RespNone
	}
	if mv := m.Mover(i); mv != nil {
		switch mv.Kind {
		case MoverDoor, MoverDoorRotating:
			if mv.TeamMaster != i {
				return RespNone // door_use: if (self->flags & FL_TEAMSLAVE) return;
			}
			return RespMove
		case MoverDoorSecret, MoverPlat, MoverButton, MoverTrain:
			return RespMove
		case MoverRotating:
			return RespToggle
		case MoverWall:
			if mv.Spawnflags&1 == 0 {
				return RespNone
			}
			if !mv.Solid {
				return RespSpawn
			}
			return RespToggle
		case MoverExplosive:
			if mv.Spawnflags&1 != 0 {
				return RespSpawn
			}
			if e.Targetname != "" {
				return RespExplode
			}
			return RespNone
		case MoverObject:
			if e.Spawnflags != 0 {
				return RespSpawn
			}
			return RespNone
		}
	}
	if t := m.Trigger(i); t != nil {
		switch t.Classname {
		case "trigger_multiple", "trigger_once":
			if t.Triggered {
				return RespEnable
			}
			return RespRelay
		case "trigger_relay":
			return RespRelay
		case "trigger_counter":
			return RespCounter
		case "trigger_key":
			if t.Item == "" || t.Target == "" {
				return RespNone
			}
			return RespKey
		case "trigger_hurt":
			if t.Spawnflags&HurtToggle != 0 {
				return RespToggle
			}
		case "trigger_elevator":
			return RespMove
		}
		return RespNone
	}
	switch e.Classname {
	case "target_changelevel":
		return RespExit
	case "target_laser", "light", "func_areaportal", "func_conveyor":
		return RespToggle
	case "target_secret", "target_goal", "target_explosion", "func_timer", "func_clock":
		return RespRelay
	case "target_speaker", "target_help", "target_temp_entity", "target_splash", "target_spawner",
		"target_blaster", "target_lightramp", "target_earthquake", "target_crosslevel_trigger",
		"target_character", "target_string", "target_actor", "func_killbox", "misc_blackhole",
		"misc_viper", "misc_strogg_ship", "misc_bigviper", "misc_satellite_dish", "turret_breach":
		return RespOther
	}
	if m.isMonster(e) {
		if e.Spawnflags&2 != 0 {
			return RespSpawn // monster_triggered_spawn_use
		}
		return RespWake
	}
	return RespNone
}

// Sources returns the ways entity i starts a chain by itself.
func (m *Map) Sources(i int) []Source {
	e := m.Entity(i)
	if e == nil || !e.Present() {
		return nil
	}
	var out []Source
	if mv := m.Mover(i); mv != nil {
		if mv.Activation&ActTouch != 0 && mv.Kind != MoverPlat {
			out = append(out, SrcTouch)
		}
		if mv.Activation&ActShoot != 0 {
			out = append(out, SrcShoot)
		}
		return out
	}
	if t := m.Trigger(i); t != nil {
		switch {
		case t.Classname == "trigger_always":
			return []Source{SrcStart}
		case t.HasVolume && !t.NotPlayer && (t.Classname == "trigger_multiple" || t.Classname == "trigger_once"):
			return []Source{SrcTouch}
		}
		return nil
	}
	switch {
	case m.isMonster(e):
		return []Source{SrcDeath}
	case m.isItem(e):
		return []Source{SrcPickup}
	case e.Classname == "func_timer" && e.Spawnflags&1 != 0, e.Classname == "target_crosslevel_target":
		return []Source{SrcStart}
	case e.Classname == "path_corner" || e.Classname == "point_combat":
		return []Source{SrcCorner}
	}
	return nil
}

// buildLogic computes the target, killtarget, team and carry links.
// C: game/g_utils.c:173 G_UseTargets
func (m *Map) buildLogic() {
	add := func(l Link) {
		m.fires[l.From] = append(m.fires[l.From], l)
		m.firedBy[l.To] = append(m.firedBy[l.To], l)
	}
	for i := range m.Entities {
		e := &m.Entities[i]
		if !e.Present() || !m.firesOwnTargets(e) {
			continue
		}
		if e.Killtarget != "" {
			for _, t := range m.Targets(e.Killtarget) {
				add(Link{From: i, To: t.Index, Kind: LinkKill, Delay: e.Delay})
			}
		}
		if tn := m.FiredTargetname(i); tn != "" {
			// The exception compares the runtime classname of the entity
			// calling G_UseTargets: SP_func_water renames itself "func_door",
			// and a delayed use is fired by a "DelayedUse" temp entity,
			// which does use the area portals.
			isDoor := e.Delay == 0 &&
				(e.Classname == "func_door" || e.Classname == "func_door_rotating" || e.Classname == "func_water")
			for _, t := range m.Targets(tn) {
				// doors fire area portals in a specific way
				if isDoor && t.Classname == "func_areaportal" {
					continue
				}
				if t.Index == i {
					continue // WARNING: Entity used itself.
				}
				add(Link{From: i, To: t.Index, Kind: LinkTarget, Delay: e.Delay})
			}
		}
	}
	for master, team := range m.teamOf {
		// door_use moves the whole team chain (func_water uses it too)
		if mv := m.Mover(master); mv == nil || (mv.Kind != MoverDoor && mv.Kind != MoverDoorRotating) {
			continue
		}
		for _, s := range team[1:] {
			add(Link{From: master, To: s, Kind: LinkTeam})
		}
	}
	m.buildCarry(add)
	for _, ls := range []map[int][]Link{m.fires, m.firedBy} {
		for _, l := range ls {
			sort.SliceStable(l, func(a, b int) bool {
				if l[a].Kind != l[b].Kind {
					return l[a].Kind < l[b].Kind
				}
				if l[a].From != l[b].From {
					return l[a].From < l[b].From
				}
				return l[a].To < l[b].To
			})
		}
	}
}

// buildCarry links the movers a player can ride to the trigger volumes
// their move brings a rider into: for one of the RiderBoxes, the box swept
// from the spawn pose to another rest pose meets the trigger while the box
// at the spawn pose does not. It is a geometric estimate (it does not
// trace the mover's brushes).
func (m *Map) buildCarry(add func(Link)) {
	for k := range m.Movers {
		mv := &m.Movers[k]
		if mv.Classname == "func_water" {
			continue
		}
		switch mv.Kind {
		case MoverDoor, MoverPlat, MoverTrain:
		default:
			continue
		}
		start := mv.RiderBoxes(mv.Origin)
		for t := range m.Triggers {
			tr := &m.Triggers[t]
			if !tr.HasVolume || tr.NotPlayer || (tr.Classname != "trigger_multiple" && tr.Classname != "trigger_once") {
				continue
			}
			if carries(mv, start, tr.Box) {
				add(Link{From: mv.Entity, To: tr.Entity, Kind: LinkCarry})
			}
		}
	}
}

func carries(mv *Mover, start [2]Box, trigger Box) bool {
	for c := range start {
		if start[c].Intersects(trigger) {
			continue // already there without moving
		}
		for _, p := range mv.Poses() {
			if start[c].Union(mv.RiderBoxes(p)[c]).Intersects(trigger) {
				return true
			}
		}
	}
	return false
}

// Reached is one entity a chain reaches.
type Reached struct {
	Entity int
	// Response is what the entity does: its own Response for a target link,
	// RespMove for a team slave, RespRelay (Touch_Multi fires its targets)
	// for a trigger a mover carries the rider into, RespNone for a removed
	// killtarget.
	Response Response
	// Via is the link that reached it (From is the previous entity).
	Via Link
	// Delay is the accumulated delay in seconds: the "delay" keys, the
	// travel time of buttons on the way (they fire on arrival) and the full
	// travel time of a mover carrying the rider into a trigger (an upper
	// bound: the rider may enter the volume before the mover stops).
	Delay float32
	// Requires lists the trigger_key items passed on the way.
	Requires []string
	// Counter is set when a trigger_counter is on the way (it needs several
	// uses).
	Counter bool
	// Removed is set for a killtarget.
	Removed bool
	// Disabled is set for a TRIGGERED trigger a mover carries the rider
	// into while it is still disabled: the touch does nothing and the chain
	// stops there.
	Disabled bool
}

// Reach returns everything a chain started at entity start reaches, in
// breadth-first order: start is activated by its own source (touched,
// pressed, shot, killed, picked up, or ridden for a mover), then every used
// entity reacts per its Response; an activated mover also follows its team
// and carry links. Each entity is reported once (first reach). Every
// TRIGGERED trigger counts as still disabled; see ReachEnabled.
func (m *Map) Reach(start int) []Reached { return m.ReachEnabled(start, nil) }

// ReachEnabled is Reach for a level in which enabled (nil: none) reports
// the TRIGGERED triggers something enabled earlier: a mover carrying the
// rider into one of them fires it.
func (m *Map) ReachEnabled(start int, enabled func(i int) bool) []Reached {
	type node struct {
		i        int
		delay    float32
		requires []string
		counter  bool
	}
	seen := map[int]bool{start: true}
	var out []Reached
	queue := []node{{i: start}}
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		resp := m.Response(n.i)
		delay, requires, counter := n.delay, n.requires, n.counter
		if mv := m.Mover(n.i); mv != nil && mv.Kind == MoverButton {
			delay += mv.TravelTime // button_wait fires on arrival
		}
		if resp == RespKey && n.i != start {
			requires = append(append([]string(nil), requires...), m.Trigger(n.i).Item)
		}
		counter = counter || (resp == RespCounter && n.i != start)
		// G_UseTargets frees the killtargets before it fires the targets
		links := m.fires[n.i]
		ordered := make([]Link, 0, len(links))
		for _, l := range links {
			if l.Kind == LinkKill {
				ordered = append(ordered, l)
			}
		}
		for _, l := range links {
			if l.Kind != LinkKill {
				ordered = append(ordered, l)
			}
		}
		for _, l := range ordered {
			if seen[l.To] {
				continue
			}
			seen[l.To] = true
			r := Reached{
				Entity: l.To, Response: m.Response(l.To), Via: l, Delay: delay + l.Delay,
				Requires: requires, Counter: counter,
			}
			active := false
			switch l.Kind {
			case LinkKill:
				r.Response, r.Removed = RespNone, true // removed: it reacts no further
			case LinkTeam:
				r.Response, active = RespMove, true // the slave moves (and fires) with its master
			case LinkCarry:
				r.Response, active = RespRelay, true // the rider touches the trigger
				if mv := m.Mover(l.From); mv != nil {
					r.Delay += mv.TravelTime
				}
				if tr := m.Trigger(l.To); tr != nil && tr.Triggered && (enabled == nil || !enabled(l.To)) {
					r.Disabled, active = true, false // ... unless it is still disabled
				}
			case LinkTarget:
				active = propagates(r.Response)
			}
			out = append(out, r)
			if active {
				queue = append(queue, node{i: l.To, delay: r.Delay, requires: requires, counter: counter})
			}
		}
	}
	return out
}

// propagates reports whether a used entity with this response goes on to
// fire its own targets (or, for a mover, carry a rider on).
func propagates(r Response) bool {
	switch r {
	case RespRelay, RespCounter, RespKey, RespExplode, RespMove:
		return true
	}
	return false
}
