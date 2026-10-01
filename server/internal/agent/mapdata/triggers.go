package mapdata

import (
	"strings"

	"quake2web/server/internal/cmodel"
	"quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/shared"
)

// Trigger spawnflags.
// C: game/g_trigger.c:118 SP_trigger_multiple (MONSTER NOT_PLAYER TRIGGERED)
const (
	TriggerMonster   = 1
	TriggerNotPlayer = 2
	TriggerTriggered = 4
	HurtStartOff     = 1
	HurtToggle       = 2
	PushOnce         = 1
)

// Trigger is a trigger_* entity: a touch volume (multiple, once, push,
// hurt, gravity, monsterjump) or a point relay (relay, key, counter,
// always, elevator).
type Trigger struct {
	Entity    int
	Classname string
	// Model, Headnode and Box are the inline model, its collision headnode
	// and its linked abs box; all empty for point triggers (HasVolume
	// false).
	Model     string
	Headnode  int32
	Box       Box
	HasVolume bool
	// Movedir is set from a non-zero "angle"/"angles" (InitTrigger,
	// SP_trigger_multiple): Touch_Multi then only fires for an activator
	// whose view forward vector has a non-negative dot product with it.
	// It is the push direction of trigger_push and the jump of
	// trigger_monsterjump (with Movedir[2] = height).
	Movedir Vec3
	// Spawnflags after the spawn function (trigger_once moves an old
	// TRIGGERED bit 1 to 4).
	Spawnflags int32
	// Monster: monsters fire it; NotPlayer: clients do not
	// (trigger_multiple/once).
	Monster, NotPlayer bool
	// Triggered: it starts disabled and is enabled by being used.
	Triggered bool
	// StartsEnabled reports whether a touch volume is active at level
	// start (TRIGGERED volumes and START_OFF hurts are not). False for
	// point triggers.
	StartsEnabled bool
	// Wait is the retrigger delay of trigger_multiple; -1 for the
	// single-shot trigger_once and trigger_counter (freed after firing).
	Wait  float32
	Delay float32
	// Item is the key a trigger_key needs (an item classname).
	Item string
	// Count is the number of uses a trigger_counter needs.
	Count int32
	// Speed of trigger_push (units/s / 10) and trigger_monsterjump.
	Speed float32
	Dmg   int32

	Targetname, Target, Killtarget, Message string
}

// Directional reports whether the trigger only fires when faced
// (Touch_Multi's forward·movedir test).
func (t *Trigger) Directional() bool {
	return (t.Classname == "trigger_multiple" || t.Classname == "trigger_once") && t.Movedir != (Vec3{})
}

// ExitKind is what SV_Map spawns for a level string.
type ExitKind uint8

const (
	ExitLevel     ExitKind = iota // a BSP level (ss_game)
	ExitCinematic                 // ".cin" (ss_cinematic)
	ExitDemo                      // ".dm2" (ss_demo)
	ExitPic                       // ".pcx" (ss_pic), e.g. victory.pcx
)

// String returns "level", "cin", "demo" or "pic".
func (k ExitKind) String() string { return enumName(int(k), "level", "cin", "demo", "pic") }

// LevelString is a changelevel map string taken apart the way
// SV_GameMap_f and SV_Map do.
type LevelString struct {
	// Raw is the whole string ("*demo2$base1+next").
	Raw string
	// Map is the level name with the spawnpoint, the "+next" part and the
	// leading '*' removed ("demo2", "victory.pcx").
	Map string
	// Spawnpoint follows '$' (the targetname of the info_player_start).
	Spawnpoint string
	// Next follows '+' (it becomes "nextserver gamemap <next>").
	Next string
	// NewUnit is a leading '*': SV_GameMap_f wipes the saved levels.
	NewUnit bool
	Kind    ExitKind
}

// ParseLevelString splits a level string like SV_GameMap_f / SV_Map.
// C: server/sv_init.c:393 SV_Map
func ParseLevelString(s string) LevelString {
	ls := LevelString{Raw: s, NewUnit: strings.HasPrefix(s, "*")}
	level := s
	// if there is a + in the map, set nextserver to the remainder
	if i := strings.IndexByte(level, '+'); i >= 0 {
		ls.Next = level[i+1:]
		level = level[:i]
	}
	// if there is a $, use the remainder as a spawnpoint
	if i := strings.IndexByte(level, '$'); i >= 0 {
		ls.Spawnpoint = level[i+1:]
		level = level[:i]
	}
	// skip the end-of-unit flag if necessary
	level = strings.TrimPrefix(level, "*")
	ls.Map = level
	if l := len(level); l > 4 {
		switch level[l-4:] {
		case ".cin":
			ls.Kind = ExitCinematic
		case ".dm2":
			ls.Kind = ExitDemo
		case ".pcx":
			ls.Kind = ExitPic
		}
	}
	return ls
}

// Exit is a target_changelevel. Quake II has no trigger_changelevel: the
// exit fires when something uses this entity (its targetname).
type Exit struct {
	Entity     int
	Targetname string
	Origin     Vec3
	// Level is the "map" key taken apart (with the fact1 map hack applied).
	Level LevelString
}

// Laser is a target_laser.
type Laser struct {
	Entity     int
	Targetname string
	Target     string
	Spawnflags int32
	// Start is the beam origin and Movedir its direction: from the angles,
	// or towards the targeted entity (see laserAim).
	Start, Movedir Vec3
	// End is where the beam stops (2048 units along Movedir unless the
	// world or a solid brush entity at its spawn pose is hit first;
	// monsters and players do not stop it).
	End Vec3
	// StartOn: spawnflags START_ON (1); target_laser_use toggles it.
	StartOn bool
	Dmg     int32
	// BadTarget: Target names no present entity, so the beam has a zero
	// Movedir and End is Start.
	BadTarget bool
}

// Spawn is an info_player_start.
type Spawn struct {
	Entity         int
	Targetname     string
	Origin, Angles Vec3
}

// Item is an item_*, weapon_*, ammo_* or key_* entity.
type Item struct {
	Entity     int
	Classname  string
	Origin     Vec3
	Targetname string
	// Target is fired when the item is picked up (Touch_Item).
	Target string
}

// Monster is a monster_* entity.
type Monster struct {
	Entity         int
	Classname      string
	Origin, Angles Vec3
	Targetname     string
	// Target is fired on death (monster_death_use) unless Deathtarget is
	// set, which then replaces it; Killtarget entities are removed then.
	Target, Deathtarget, Killtarget, Combattarget string
	// Item is dropped on death.
	Item string
	// Ambush (spawnflags 1, or 4 which monster_start turns into 1) and
	// TriggerSpawn (2: the monster appears when used).
	Ambush, TriggerSpawn bool
}

func (m *Map) buildTriggers() error {
	for i := range m.Entities {
		e := &m.Entities[i]
		if !e.Present() || !strings.HasPrefix(e.Classname, "trigger_") {
			continue
		}
		t := Trigger{
			Entity: i, Classname: e.Classname, Spawnflags: e.Spawnflags, Wait: e.Wait, Delay: e.Delay,
			Count: e.Count, Speed: e.Speed, Dmg: e.Dmg,
			Targetname: e.Targetname, Target: e.Target, Killtarget: e.Killtarget, Message: e.Message,
		}
		angles := e.Angles
		volume := true
		switch e.Classname {
		case "trigger_once":
			// C: game/g_trigger.c:168 SP_trigger_once
			if t.Spawnflags&1 != 0 {
				t.Spawnflags &^= 1
				t.Spawnflags |= TriggerTriggered
			}
			t.Wait = -1
			fallthrough
		case "trigger_multiple":
			// C: game/g_trigger.c:118 SP_trigger_multiple
			if t.Wait == 0 {
				t.Wait = 0.2
			}
			t.Triggered = t.Spawnflags&TriggerTriggered != 0
			t.StartsEnabled = !t.Triggered
			t.Monster = t.Spawnflags&TriggerMonster != 0
			t.NotPlayer = t.Spawnflags&TriggerNotPlayer != 0
		case "trigger_push":
			// C: game/g_trigger.c:424 SP_trigger_push
			if t.Speed == 0 {
				t.Speed = 1000
			}
			t.StartsEnabled = true
		case "trigger_hurt":
			// C: game/g_trigger.c:496 SP_trigger_hurt
			if t.Dmg == 0 {
				t.Dmg = 5
			}
			t.StartsEnabled = t.Spawnflags&HurtStartOff == 0
		case "trigger_gravity":
			t.StartsEnabled = true
		case "trigger_monsterjump":
			// C: game/g_trigger.c:586 SP_trigger_monsterjump
			if t.Speed == 0 {
				t.Speed = 200
			}
			if angles[q2const.YAW] == 0 {
				angles[q2const.YAW] = 360
			}
			t.StartsEnabled = true
		case "trigger_relay", "trigger_always", "trigger_elevator":
			volume = false
		case "trigger_key":
			volume = false
			t.Item = e.Item
		case "trigger_counter":
			// C: game/g_trigger.c:352 SP_trigger_counter
			volume = false
			t.Wait = -1
			if t.Count == 0 {
				t.Count = 2
			}
		default:
			continue // not a spawnable trigger class
		}
		if e.Classname == "trigger_always" && float64(t.Delay) < 0.2 {
			// we must have some delay to make sure our use targets are present
			t.Delay = 0.2
		}
		if volume {
			// C: game/g_trigger.c:23 InitTrigger
			if angles != (Vec3{}) {
				G_SetMovedir(&angles, &t.Movedir)
			}
			cm, err := m.inlineModel(e)
			if err != nil {
				return err
			}
			t.Model, t.Headnode, t.HasVolume = e.Model, cm.Headnode, true
			t.Box = linkBox(false, e.Origin, angles, cm.Mins, cm.Maxs)
			if e.Classname == "trigger_monsterjump" {
				height := e.Height
				if height == 0 {
					height = 200
				}
				t.Movedir[2] = float32(height)
			}
		}
		m.triggerOf[i] = len(m.Triggers)
		m.Triggers = append(m.Triggers, t)
	}
	return nil
}

// buildPoints collects exits, spawn points, items and monsters.
func (m *Map) buildPoints() error {
	for i := range m.Entities {
		e := &m.Entities[i]
		if !e.Present() {
			continue
		}
		c := e.Classname
		switch {
		case c == "target_changelevel":
			mp := e.Map
			// ugly hack because *SOMEBODY* screwed up their map
			if shared.Q_stricmp(m.Name, "fact1") == 0 && shared.Q_stricmp(mp, "fact3") == 0 {
				mp = "fact3$secret1"
			}
			m.exitOf[i] = len(m.Exits)
			m.Exits = append(m.Exits, Exit{Entity: i, Targetname: e.Targetname, Origin: e.Origin, Level: ParseLevelString(mp)})
		case c == "info_player_start":
			m.Spawns = append(m.Spawns, Spawn{Entity: i, Targetname: e.Targetname, Origin: e.Origin, Angles: e.Angles})
		case strings.HasPrefix(c, "item_") || strings.HasPrefix(c, "weapon_") ||
			strings.HasPrefix(c, "ammo_") || strings.HasPrefix(c, "key_"):
			m.Items = append(m.Items, Item{Entity: i, Classname: c, Origin: e.Origin, Targetname: e.Targetname, Target: e.Target})
		case strings.HasPrefix(c, "monster_") && c != "monster_commander_body":
			m.Monsters = append(m.Monsters, Monster{
				Entity: i, Classname: c, Origin: e.Origin, Angles: e.Angles, Targetname: e.Targetname,
				Target: e.Target, Deathtarget: e.Deathtarget, Killtarget: e.Killtarget, Combattarget: e.Combattarget,
				Item: e.Item, Ambush: e.Spawnflags&(1|4) != 0, TriggerSpawn: e.Spawnflags&2 != 0,
			})
		}
	}
	return nil
}

// buildLasers derives the beam of every target_laser as target_laser_start
// and the first target_laser_think compute it.
// C: game/g_target.c:585 target_laser_start
func (m *Map) buildLasers() error {
	cs := cmodel.NewState(m.CM)
	for i := range m.Entities {
		e := &m.Entities[i]
		if !e.Present() || e.Classname != "target_laser" {
			continue
		}
		l := Laser{
			Entity: i, Targetname: e.Targetname, Target: e.Target, Spawnflags: e.Spawnflags,
			Start: e.Origin, StartOn: e.Spawnflags&1 != 0, Dmg: e.Dmg,
		}
		if l.Dmg == 0 {
			l.Dmg = 1
		}
		if e.Target != "" {
			// "%s is a bad target" is only a dprintf: without an enemy and
			// without G_SetMovedir the beam keeps a zero movedir and
			// collapses onto its origin
			if ts := m.Targets(e.Target); len(ts) > 0 {
				// C: game/g_target.c:495 target_laser_think (enemy aim)
				point := m.laserAim(ts[0])
				l.Movedir = shared.VectorSubtract(point, e.Origin)
				shared.VectorNormalize(&l.Movedir)
			} else {
				l.BadTarget = true
			}
		} else {
			angles := e.Angles
			G_SetMovedir(&angles, &l.Movedir)
		}
		l.End = m.beamEnd(cs, l.Start, l.Movedir)
		m.laserOf[i] = len(m.Lasers)
		m.Lasers = append(m.Lasers, l)
	}
	return nil
}

// laserAim is the point a targeted laser aims at, VectorMA(enemy->absmin,
// 0.5, enemy->size), for the entity kinds whose link box is known
// statically: brush movers and triggers, info_notnull (absmin = origin,
// no size) and path_corner (a linked ±8 box). Any other target is aimed at
// its origin.
func (m *Map) laserAim(t *Entity) Vec3 {
	if mv := m.Mover(t.Index); mv != nil {
		return shared.VectorMA(mv.Box.Min, 0.5, mv.Size)
	}
	if tr := m.Trigger(t.Index); tr != nil && tr.HasVolume {
		size := shared.VectorSubtract(tr.Box.Max, tr.Box.Min)
		size = shared.VectorSubtract(size, Vec3{2, 2, 2})
		return shared.VectorMA(tr.Box.Min, 0.5, size)
	}
	if t.Classname == "path_corner" {
		b := linkBox(false, t.Origin, Vec3{}, Vec3{-8, -8, -8}, Vec3{8, 8, 8})
		return shared.VectorMA(b.Min, 0.5, Vec3{16, 16, 16})
	}
	return t.Origin
}

// beamEnd traces the beam like target_laser_think: CONTENTS_SOLID |
// CONTENTS_MONSTER | CONTENTS_DEADMONSTER against the world and the solid
// brush movers in their spawn pose (monsters never stop a beam).
func (m *Map) beamEnd(cs *cmodel.State, start, dir Vec3) Vec3 {
	const mask = q2const.CONTENTS_SOLID | q2const.CONTENTS_MONSTER | q2const.CONTENTS_DEADMONSTER
	end := shared.VectorMA(start, 2048, dir)
	tr := cs.BoxTrace(start, end, Vec3{}, Vec3{}, 0, mask)
	for k := range m.Movers {
		mv := &m.Movers[k]
		if !mv.Solid || !strings.HasPrefix(mv.Model, "*") {
			continue
		}
		t := cs.TransformedBoxTrace(start, end, Vec3{}, Vec3{}, mv.Headnode, mask, mv.Origin, mv.Angles)
		if t.Fraction < tr.Fraction {
			tr = t
		}
	}
	return tr.EndPos
}
