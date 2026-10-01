package mapdata

import (
	"math"

	"quake2web/server/internal/qcommon/shared"
)

// MoverKind classifies the brush entities that move or change state.
type MoverKind uint8

const (
	MoverDoor         MoverKind = iota + 1 // func_door, func_water
	MoverDoorRotating                      // func_door_rotating
	MoverDoorSecret                        // func_door_secret
	MoverPlat                              // func_plat
	MoverButton                            // func_button
	MoverTrain                             // func_train
	MoverRotating                          // func_rotating (spins, never translates)
	MoverWall                              // func_wall (static or trigger-spawned / toggled)
	MoverExplosive                         // func_explosive (destroyed when shot or used)
	MoverObject                            // func_object (falls when released)
)

// String returns the lower-case kind name.
func (k MoverKind) String() string {
	switch k {
	case MoverDoor:
		return "door"
	case MoverDoorRotating:
		return "door_rotating"
	case MoverDoorSecret:
		return "door_secret"
	case MoverPlat:
		return "plat"
	case MoverButton:
		return "button"
	case MoverTrain:
		return "train"
	case MoverRotating:
		return "rotating"
	case MoverWall:
		return "wall"
	case MoverExplosive:
		return "explosive"
	case MoverObject:
		return "object"
	}
	return "?"
}

// Activation is a set of ways an entity can be set off.
type Activation uint8

const (
	// ActTouch: a client touching it (button_touch, the door trigger box,
	// the plat center trigger, Touch_Multi). A targeted func_plat only has
	// ActUse: its center trigger works once a use has sent it down.
	ActTouch Activation = 1 << iota
	// ActShoot: damage (button_killed, door_killed, door_secret_die,
	// func_explosive_explode).
	ActShoot
	// ActUse: another entity's targets (it has a targetname and a use
	// function that does something).
	ActUse
	// ActAuto: it starts by itself (an untargeted train, a START_ON
	// func_rotating, a falling func_object).
	ActAuto
)

// String lists the flags: "touch|shoot|use|auto".
func (a Activation) String() string {
	s := ""
	for _, f := range []struct {
		bit  Activation
		name string
	}{{ActTouch, "touch"}, {ActShoot, "shoot"}, {ActUse, "use"}, {ActAuto, "auto"}} {
		if a&f.bit != 0 {
			if s != "" {
				s += "|"
			}
			s += f.name
		}
	}
	if s == "" {
		return "none"
	}
	return s
}

// Spawnflags of the movers.
// C: game/g_func.c:56 PLAT_LOW_TRIGGER (DOOR_* :63, TRAIN_* :1445, SECRET_* :1874)
const (
	PlatLowTrigger    = 1
	DoorStartOpen     = 1
	DoorReverse       = 2
	DoorCrusher       = 4
	DoorNoMonster     = 8
	DoorToggle        = 32
	DoorXAxis         = 64
	DoorYAxis         = 128
	TrainStartOn      = 1
	TrainToggle       = 2
	TrainBlockStops   = 4
	SecretAlwaysShoot = 1
	Secret1stLeft     = 2
	Secret1stDown     = 4
)

// Mover is a brush entity that moves or changes state, with the poses and
// move parameters its spawn function computes.
type Mover struct {
	// Entity is the lump index; Classname the lump classname (func_water
	// and func_door_secret are renamed "func_door" by their spawn
	// functions).
	Entity    int
	Classname string
	Kind      MoverKind
	Model     string
	// Mins, Maxs are ent->mins/maxs (the inline model bounds; func_object
	// shrinks them by 1) and Size their difference.
	Mins, Maxs, Size Vec3
	// Headnode is the inline model's collision headnode (for
	// cmodel.TransformedBoxTrace at a pose).
	Headnode int32
	// Origin and Angles are the pose the entity is in once spawned (a plat
	// without targetname sits at Pos2, a START_OPEN door at its open pose, a
	// train at its first path corner).
	Origin, Angles Vec3
	// Box is the absmin/absmax of the spawned pose.
	Box Box
	// Movedir is ent->movedir: the move direction of doors, buttons and
	// water, the rotation axis of rotating movers, zero for the others
	// (plats move straight down from Pos1 to Pos2; secret doors move
	// sideways to Pos1, then to Pos2).
	Movedir Vec3
	// Pos1 and Pos2 are the closed/open (top/bottom for plats) poses:
	// origins, or angles for func_door_rotating.
	Pos1, Pos2 Vec3
	// Distance is moveinfo.distance (doors, water, rotating doors).
	Distance float32
	// Speed, Accel and Decel are the moveinfo values the moves use, after
	// Think_CalcMoveSpeed equalized a door team. Plat values are in units
	// per frame (the spawn function scales the editor values by 0.1).
	Speed, Accel, Decel float32
	// Wait is moveinfo.wait (ent->wait for func_door_secret): seconds at the
	// end pose before returning, -1 to stay. Plats always go back down 3 s
	// after reaching the top (plat_hit_top) whatever this says.
	Wait float32
	// TravelTime is the seconds from activation to arrival at the other
	// pose (Pos1 -> Pos2), counted in server frames from the think schedule
	// of Move_Calc / AngleMove_Calc, including the one-frame start delay of
	// a move started by another entity. 0 when not applicable.
	TravelTime float32
	Lip        int32
	Dmg        int32
	Health     int32
	// Spawnflags after the spawn function (func_water gains DOOR_TOGGLE,
	// func_wall gains TRIGGER_SPAWN / TOGGLE).
	Spawnflags int32
	StartOpen  bool
	Toggle     bool
	// Solid is whether the entity blocks at spawn (false for trigger-spawned
	// func_wall/func_explosive/func_object until they are used).
	Solid      bool
	Targetname string
	Target     string
	Message    string
	Team       string
	// TeamMaster is the lump index of the team master (the mover itself
	// when not teamed). Only the master reacts to use or its trigger box.
	TeamMaster int
	Activation Activation
	// Trigger is the box of the trigger the mover spawns: the door trigger
	// around the whole team (Think_SpawnDoorTrigger, on the master only) or
	// the plat's center trigger (plat_spawn_inside_trigger).
	Trigger *Box
	// Path is the func_train route, one pose per path_corner.
	Path []PathPose
}

// PathPose is one stop of a func_train.
type PathPose struct {
	// Corner is the lump index of the path_corner.
	Corner     int
	Targetname string
	// Origin is the train origin at that corner (corner origin - mins).
	Origin Vec3
	Wait   float32
	// Teleport: the corner has spawnflags 1 and the train jumps to it.
	Teleport   bool
	Pathtarget string
}

// RiderBoxes returns where a player riding the mover can be when the mover
// is at origin pose p, as two candidate boxes over the mover's x/y extent:
// standing on top of it (a lift slab: 56 above its top) and standing on
// the floor of a cage whose model spans floor to roof (demo1's car *31:
// 72 above its bottom, room for a floor slab and the player hull).
func (mv *Mover) RiderBoxes(p Vec3) [2]Box {
	x0, y0 := p[0]+mv.Mins[0], p[1]+mv.Mins[1]
	x1, y1 := p[0]+mv.Maxs[0], p[1]+mv.Maxs[1]
	top, bottom := p[2]+mv.Maxs[2], p[2]+mv.Mins[2]
	return [2]Box{
		{Min: Vec3{x0, y0, top}, Max: Vec3{x1, y1, top + 56}},
		{Min: Vec3{x0, y0, bottom}, Max: Vec3{x1, y1, bottom + 72}},
	}
}

// PoseBox returns the abs box of the mover at origin pose p.
func (mv *Mover) PoseBox(p Vec3) Box { return linkBox(false, p, Vec3{}, mv.Mins, mv.Maxs) }

// Poses returns the origins the mover can come to rest at: Pos1 and Pos2
// for linear movers (a secret door also rests at its closed origin), the
// path corners for trains, the spawn origin otherwise (rotating doors only
// change angles).
func (mv *Mover) Poses() []Vec3 {
	switch mv.Kind {
	case MoverDoor, MoverPlat, MoverButton:
		return []Vec3{mv.Pos1, mv.Pos2}
	case MoverDoorSecret:
		return []Vec3{mv.Origin, mv.Pos1, mv.Pos2}
	case MoverTrain:
		out := make([]Vec3, len(mv.Path))
		for i, p := range mv.Path {
			out[i] = p.Origin
		}
		return out
	}
	return []Vec3{mv.Origin}
}

func (m *Map) buildMovers() error {
	for i := range m.Entities {
		e := &m.Entities[i]
		if !e.Present() {
			continue
		}
		var mv *Mover
		var err error
		switch e.Classname {
		case "func_door":
			mv, err = m.spDoor(e)
		case "func_door_rotating":
			mv, err = m.spDoorRotating(e)
		case "func_water":
			mv, err = m.spWater(e)
		case "func_door_secret":
			mv, err = m.spDoorSecret(e)
		case "func_plat":
			mv, err = m.spPlat(e)
		case "func_button":
			mv, err = m.spButton(e)
		case "func_train":
			mv, err = m.spTrain(e)
		case "func_rotating":
			mv, err = m.spRotating(e)
		case "func_wall":
			mv, err = m.spWall(e)
		case "func_explosive":
			mv, err = m.spExplosive(e)
		case "func_object":
			mv, err = m.spObject(e)
		default:
			continue
		}
		if err != nil {
			return err
		}
		mv.Entity, mv.Classname, mv.Model = i, e.Classname, e.Model
		mv.Targetname, mv.Target, mv.Message, mv.Team = e.Targetname, e.Target, e.Message, e.Team
		mv.TeamMaster = m.TeamMaster(i)
		if mv.TeamMaster != i && (mv.Kind == MoverDoor || mv.Kind == MoverDoorRotating) {
			// door_use ignores team slaves and only the master spawns a
			// trigger box; a slave with health still opens the team
			// (door_killed uses the master).
			mv.Activation &= ActShoot
		}
		mv.Box = linkBox(mv.Solid, mv.Origin, mv.Angles, mv.Mins, mv.Maxs)
		m.moverOf[i] = len(m.Movers)
		m.Movers = append(m.Movers, *mv)
	}
	m.spawnDoorTriggers()
	return nil
}

// setModel applies PF_setmodel: the inline model bounds and their size.
func (m *Map) setModel(e *Entity, mv *Mover) error {
	cm, err := m.inlineModel(e)
	if err != nil {
		return err
	}
	mv.Mins, mv.Maxs, mv.Headnode = cm.Mins, cm.Maxs, cm.Headnode
	mv.Size = shared.VectorSubtract(mv.Maxs, mv.Mins)
	return nil
}

// moveDistance is |movedir|·size - lip as the door, button and water spawn
// functions compute it (every product rounded to float).
func moveDistance(movedir, size Vec3, lip int32) float32 {
	return float32(abs32(movedir[0])*size[0]) + float32(abs32(movedir[1])*size[1]) +
		float32(abs32(movedir[2])*size[2]) - float32(lip)
}

// C: game/g_func.c:1138 SP_func_door
func (m *Map) spDoor(e *Entity) (*Mover, error) {
	mv := &Mover{Kind: MoverDoor, Solid: true, Spawnflags: e.Spawnflags, Health: e.Health}
	angles := e.Angles
	G_SetMovedir(&angles, &mv.Movedir)
	if err := m.setModel(e, mv); err != nil {
		return nil, err
	}

	speed := e.Speed
	if speed == 0 {
		speed = 100
	}
	if m.Options.Deathmatch {
		speed *= 2
	}
	accel, decel := e.Accel, e.Decel
	if accel == 0 {
		accel = speed
	}
	if decel == 0 {
		decel = speed
	}
	mv.Wait = e.Wait
	if mv.Wait == 0 {
		mv.Wait = 3
	}
	mv.Lip = e.Lip
	if mv.Lip == 0 {
		mv.Lip = 8
	}
	mv.Dmg = e.Dmg
	if mv.Dmg == 0 {
		mv.Dmg = 2
	}

	// calculate second position
	mv.Pos1 = e.Origin
	mv.Distance = moveDistance(mv.Movedir, mv.Size, mv.Lip)
	mv.Pos2 = shared.VectorMA(mv.Pos1, mv.Distance, mv.Movedir)
	mv.Origin = e.Origin

	// if it starts open, switch the positions
	if e.Spawnflags&DoorStartOpen != 0 {
		mv.Origin = mv.Pos2
		mv.Pos2 = mv.Pos1
		mv.Pos1 = mv.Origin
		mv.StartOpen = true
	}
	mv.Speed, mv.Accel, mv.Decel = speed, accel, decel
	mv.Toggle = e.Spawnflags&DoorToggle != 0
	mv.Activation = doorActivation(e)
	return mv, nil
}

// doorActivation: a door with health is shot open, a targeted one is used,
// any other spawns a trigger box (Think_SpawnDoorTrigger).
// C: game/g_func.c:1225 SP_func_door (think selection)
func doorActivation(e *Entity) Activation {
	var a Activation
	if e.Health != 0 {
		a |= ActShoot
	}
	if e.Targetname != "" {
		a |= ActUse
	}
	if e.Health == 0 && e.Targetname == "" {
		a |= ActTouch
	}
	return a
}

// C: game/g_func.c:1261 SP_func_door_rotating
func (m *Map) spDoorRotating(e *Entity) (*Mover, error) {
	mv := &Mover{Kind: MoverDoorRotating, Solid: true, Spawnflags: e.Spawnflags, Health: e.Health}

	// set the axis of rotation
	switch {
	case e.Spawnflags&DoorXAxis != 0:
		mv.Movedir[2] = 1.0
	case e.Spawnflags&DoorYAxis != 0:
		mv.Movedir[0] = 1.0
	default: // Z_AXIS
		mv.Movedir[1] = 1.0
	}
	// check for reverse rotation
	if e.Spawnflags&DoorReverse != 0 {
		mv.Movedir = shared.VectorNegate(mv.Movedir)
	}

	distance := e.Distance
	if distance == 0 {
		distance = 90
	}
	mv.Pos1 = Vec3{}
	mv.Pos2 = shared.VectorMA(Vec3{}, float32(distance), mv.Movedir)
	mv.Distance = float32(distance)

	if err := m.setModel(e, mv); err != nil {
		return nil, err
	}

	speed, accel, decel := e.Speed, e.Accel, e.Decel
	if speed == 0 {
		speed = 100
	}
	if accel == 0 {
		accel = speed
	}
	if decel == 0 {
		decel = speed
	}
	mv.Wait = e.Wait
	if mv.Wait == 0 {
		mv.Wait = 3
	}
	mv.Dmg = e.Dmg
	if mv.Dmg == 0 {
		mv.Dmg = 2
	}

	mv.Origin = e.Origin
	// if it starts open, switch the positions
	if e.Spawnflags&DoorStartOpen != 0 {
		mv.Angles = mv.Pos2
		mv.Pos2 = mv.Pos1
		mv.Pos1 = mv.Angles
		mv.Movedir = shared.VectorNegate(mv.Movedir)
		mv.StartOpen = true
	}
	mv.Speed, mv.Accel, mv.Decel = speed, accel, decel
	mv.Toggle = e.Spawnflags&DoorToggle != 0
	mv.Activation = doorActivation(e)
	return mv, nil
}

// C: game/g_func.c:1378 SP_func_water
func (m *Map) spWater(e *Entity) (*Mover, error) {
	mv := &Mover{Kind: MoverDoor, Solid: true, Spawnflags: e.Spawnflags}
	angles := e.Angles
	G_SetMovedir(&angles, &mv.Movedir)
	if err := m.setModel(e, mv); err != nil {
		return nil, err
	}

	// calculate second position
	mv.Lip = e.Lip
	mv.Pos1 = e.Origin
	mv.Distance = moveDistance(mv.Movedir, mv.Size, mv.Lip)
	mv.Pos2 = shared.VectorMA(mv.Pos1, mv.Distance, mv.Movedir)
	mv.Origin = e.Origin

	// if it starts open, switch the positions
	if e.Spawnflags&DoorStartOpen != 0 {
		mv.Origin = mv.Pos2
		mv.Pos2 = mv.Pos1
		mv.Pos1 = mv.Origin
		mv.StartOpen = true
	}

	speed := e.Speed
	if speed == 0 {
		speed = 25
	}
	mv.Speed, mv.Accel, mv.Decel = speed, speed, speed
	mv.Wait = e.Wait
	if mv.Wait == 0 {
		mv.Wait = -1
	}
	if mv.Wait == -1 {
		mv.Spawnflags |= DoorToggle
	}
	mv.Toggle = mv.Spawnflags&DoorToggle != 0
	if e.Targetname != "" {
		mv.Activation = ActUse
	}
	return mv, nil
}

// C: game/g_func.c:1966 SP_func_door_secret
func (m *Map) spDoorSecret(e *Entity) (*Mover, error) {
	mv := &Mover{Kind: MoverDoorSecret, Solid: true, Spawnflags: e.Spawnflags}
	if err := m.setModel(e, mv); err != nil {
		return nil, err
	}

	health := e.Health
	if e.Targetname == "" || e.Spawnflags&SecretAlwaysShoot != 0 {
		health = 0
		mv.Activation |= ActShoot // door_secret_die
	}
	mv.Dmg = e.Dmg
	if mv.Dmg == 0 {
		mv.Dmg = 2
	}
	mv.Wait = e.Wait
	if mv.Wait == 0 {
		mv.Wait = 5
	}
	mv.Speed, mv.Accel, mv.Decel = 50, 50, 50

	// calculate positions
	var forward, right, up Vec3
	shared.AngleVectors(e.Angles, &forward, &right, &up)
	side := float32(1.0 - float64(e.Spawnflags&Secret1stLeft))
	var width float32
	if e.Spawnflags&Secret1stDown != 0 {
		width = abs32(shared.DotProduct(up, mv.Size))
	} else {
		width = abs32(shared.DotProduct(right, mv.Size))
	}
	length := abs32(shared.DotProduct(forward, mv.Size))
	if e.Spawnflags&Secret1stDown != 0 {
		mv.Pos1 = shared.VectorMA(e.Origin, -1*width, up)
	} else {
		mv.Pos1 = shared.VectorMA(e.Origin, side*width, right)
	}
	mv.Pos2 = shared.VectorMA(mv.Pos1, length, forward)
	mv.Origin = e.Origin

	if health != 0 {
		mv.Activation |= ActShoot // door_killed
	}
	mv.Health = health
	if e.Targetname != "" {
		mv.Activation |= ActUse
	}
	return mv, nil
}

// C: game/g_func.c:513 SP_func_plat
func (m *Map) spPlat(e *Entity) (*Mover, error) {
	mv := &Mover{Kind: MoverPlat, Solid: true, Spawnflags: e.Spawnflags}
	if err := m.setModel(e, mv); err != nil {
		return nil, err
	}

	if e.Speed == 0 {
		mv.Speed = 20
	} else {
		mv.Speed = float32(float64(e.Speed) * 0.1)
	}
	if e.Accel == 0 {
		mv.Accel = 5
	} else {
		mv.Accel = float32(float64(e.Accel) * 0.1)
	}
	if e.Decel == 0 {
		mv.Decel = 5
	} else {
		mv.Decel = float32(float64(e.Decel) * 0.1)
	}
	mv.Dmg = e.Dmg
	if mv.Dmg == 0 {
		mv.Dmg = 2
	}
	mv.Lip = e.Lip
	if mv.Lip == 0 {
		mv.Lip = 8
	}

	// pos1 is the top position, pos2 is the bottom
	mv.Pos1 = e.Origin
	mv.Pos2 = e.Origin
	if e.Height != 0 {
		mv.Pos2[2] -= float32(e.Height)
	} else {
		mv.Pos2[2] -= (mv.Maxs[2] - mv.Mins[2]) - float32(mv.Lip)
	}
	mv.Wait = e.Wait

	t := platInsideTrigger(mv)
	mv.Trigger = &t

	if e.Targetname != "" {
		// STATE_UP: it waits at the top until used, and Touch_Plat_Center
		// ignores it until that use has brought it to the bottom
		mv.Origin = mv.Pos1
		mv.Activation = ActUse
	} else {
		mv.Origin = mv.Pos2
		mv.Activation = ActTouch
	}
	return mv, nil
}

// platInsideTrigger is the "start moving" trigger box of a plat (in world
// coordinates; it never moves with the plat).
// C: game/g_func.c:451 plat_spawn_inside_trigger
func platInsideTrigger(mv *Mover) Box {
	var tmin, tmax Vec3
	tmin[0] = mv.Mins[0] + 25
	tmin[1] = mv.Mins[1] + 25
	tmin[2] = mv.Mins[2]

	tmax[0] = mv.Maxs[0] - 25
	tmax[1] = mv.Maxs[1] - 25
	tmax[2] = mv.Maxs[2] + 8

	tmin[2] = tmax[2] - (mv.Pos1[2] - mv.Pos2[2] + float32(mv.Lip))

	if mv.Spawnflags&PlatLowTrigger != 0 {
		tmax[2] = tmin[2] + 8
	}

	if tmax[0]-tmin[0] <= 0 {
		tmin[0] = float32(float64(mv.Mins[0]+mv.Maxs[0]) * 0.5)
		tmax[0] = tmin[0] + 1
	}
	if tmax[1]-tmin[1] <= 0 {
		tmin[1] = float32(float64(mv.Mins[1]+mv.Maxs[1]) * 0.5)
		tmax[1] = tmin[1] + 1
	}
	return linkBox(false, Vec3{}, Vec3{}, tmin, tmax)
}

// C: game/g_func.c:763 SP_func_button
func (m *Map) spButton(e *Entity) (*Mover, error) {
	mv := &Mover{Kind: MoverButton, Solid: true, Spawnflags: e.Spawnflags, Health: e.Health}
	angles := e.Angles
	G_SetMovedir(&angles, &mv.Movedir)
	if err := m.setModel(e, mv); err != nil {
		return nil, err
	}

	speed := e.Speed
	if speed == 0 {
		speed = 40
	}
	accel, decel := e.Accel, e.Decel
	if accel == 0 {
		accel = speed
	}
	if decel == 0 {
		decel = speed
	}
	mv.Speed, mv.Accel, mv.Decel = speed, accel, decel
	mv.Wait = e.Wait
	if mv.Wait == 0 {
		mv.Wait = 3
	}
	mv.Lip = e.Lip
	if mv.Lip == 0 {
		mv.Lip = 4
	}

	mv.Pos1 = e.Origin
	dist := moveDistance(mv.Movedir, mv.Size, mv.Lip)
	mv.Pos2 = shared.VectorMA(mv.Pos1, dist, mv.Movedir)
	mv.Origin = e.Origin

	if e.Health != 0 {
		mv.Activation |= ActShoot
	} else if e.Targetname == "" {
		mv.Activation |= ActTouch
	}
	if e.Targetname != "" {
		mv.Activation |= ActUse
	}
	return mv, nil
}

// spTrain also resolves the path func_train_find and train_next follow
// (one pose per path_corner, origin = corner - mins). With several corners
// sharing a targetname G_PickTarget chooses at random; the first in lump
// order is used here.
// C: game/g_func.c:1654 SP_func_train
func (m *Map) spTrain(e *Entity) (*Mover, error) {
	mv := &Mover{Kind: MoverTrain, Solid: true, Spawnflags: e.Spawnflags}
	if err := m.setModel(e, mv); err != nil {
		return nil, err
	}
	if e.Spawnflags&TrainBlockStops != 0 {
		mv.Dmg = 0
	} else {
		mv.Dmg = e.Dmg
		if mv.Dmg == 0 {
			mv.Dmg = 100
		}
	}
	speed := e.Speed
	if speed == 0 {
		speed = 100
	}
	mv.Speed, mv.Accel, mv.Decel = speed, speed, speed
	mv.Origin = e.Origin
	mv.Toggle = e.Spawnflags&TrainToggle != 0

	// C: game/g_func.c:1601 func_train_find, game/g_func.c:1529 train_next
	seen := map[int]bool{}
	for target := e.Target; target != ""; {
		ts := m.Targets(target)
		if len(ts) == 0 {
			break
		}
		c := ts[0]
		if seen[c.Index] {
			break
		}
		seen[c.Index] = true
		mv.Path = append(mv.Path, PathPose{
			Corner: c.Index, Targetname: c.Targetname, Origin: shared.VectorSubtract(c.Origin, mv.Mins),
			Wait: c.Wait, Teleport: c.Spawnflags&1 != 0, Pathtarget: c.Pathtarget,
		})
		target = c.Target
	}
	if len(mv.Path) > 0 {
		mv.Origin = mv.Path[0].Origin // func_train_find, first frame
	}
	if e.Targetname == "" {
		if e.Target != "" {
			mv.Activation = ActAuto
		}
	} else {
		mv.Activation = ActUse
		if e.Spawnflags&TrainStartOn != 0 {
			mv.Activation |= ActAuto
		}
	}
	return mv, nil
}

// C: game/g_func.c:623 SP_func_rotating
func (m *Map) spRotating(e *Entity) (*Mover, error) {
	mv := &Mover{Kind: MoverRotating, Solid: true, Spawnflags: e.Spawnflags}
	// set the axis of rotation
	switch {
	case e.Spawnflags&4 != 0:
		mv.Movedir[2] = 1.0
	case e.Spawnflags&8 != 0:
		mv.Movedir[0] = 1.0
	default: // Z_AXIS
		mv.Movedir[1] = 1.0
	}
	// check for reverse rotation
	if e.Spawnflags&2 != 0 {
		mv.Movedir = shared.VectorNegate(mv.Movedir)
	}
	mv.Speed = e.Speed
	if mv.Speed == 0 {
		mv.Speed = 100
	}
	mv.Dmg = e.Dmg
	if mv.Dmg == 0 {
		mv.Dmg = 2
	}
	if err := m.setModel(e, mv); err != nil {
		return nil, err
	}
	mv.Origin, mv.Angles = e.Origin, e.Angles
	if e.Spawnflags&1 != 0 {
		mv.Activation |= ActAuto
	}
	if e.Targetname != "" {
		mv.Activation |= ActUse
	}
	return mv, nil
}

// C: game/g_misc.c:612 SP_func_wall
func (m *Map) spWall(e *Entity) (*Mover, error) {
	mv := &Mover{Kind: MoverWall, Spawnflags: e.Spawnflags}
	if err := m.setModel(e, mv); err != nil {
		return nil, err
	}
	mv.Origin, mv.Angles = e.Origin, e.Angles
	// just a wall
	if e.Spawnflags&7 == 0 {
		mv.Solid = true
		return mv, nil
	}
	// it must be TRIGGER_SPAWN
	mv.Spawnflags |= 1
	// yell if the spawnflags are odd
	if mv.Spawnflags&4 != 0 && mv.Spawnflags&2 == 0 {
		mv.Spawnflags |= 2
	}
	mv.Solid = mv.Spawnflags&4 != 0 // START_ON
	mv.Toggle = mv.Spawnflags&2 != 0
	if e.Targetname != "" {
		mv.Activation = ActUse
	}
	return mv, nil
}

// C: game/g_misc.c:823 SP_func_explosive
func (m *Map) spExplosive(e *Entity) (*Mover, error) {
	mv := &Mover{Kind: MoverExplosive, Spawnflags: e.Spawnflags, Dmg: e.Dmg}
	if err := m.setModel(e, mv); err != nil {
		return nil, err
	}
	mv.Origin, mv.Angles = e.Origin, e.Angles
	explodeOnUse := false
	if e.Spawnflags&1 != 0 {
		mv.Solid = false // func_explosive_spawn
	} else {
		mv.Solid = true
		explodeOnUse = e.Targetname != ""
	}
	if e.Targetname != "" {
		mv.Activation |= ActUse
	}
	mv.Health = e.Health
	if !explodeOnUse {
		if mv.Health == 0 {
			mv.Health = 100
		}
		mv.Activation |= ActShoot
	}
	return mv, nil
}

// C: game/g_misc.c:692 SP_func_object
func (m *Map) spObject(e *Entity) (*Mover, error) {
	mv := &Mover{Kind: MoverObject, Spawnflags: e.Spawnflags}
	if err := m.setModel(e, mv); err != nil {
		return nil, err
	}
	mv.Mins = shared.VectorAdd(mv.Mins, Vec3{1, 1, 1})
	mv.Maxs = shared.VectorSubtract(mv.Maxs, Vec3{1, 1, 1})
	mv.Size = shared.VectorSubtract(mv.Maxs, mv.Mins)
	mv.Dmg = e.Dmg
	if mv.Dmg == 0 {
		mv.Dmg = 100
	}
	mv.Origin, mv.Angles = e.Origin, e.Angles
	if e.Spawnflags == 0 {
		mv.Solid = true
		mv.Activation = ActAuto // func_object_release after two frames
	} else if e.Targetname != "" {
		mv.Activation = ActUse
	}
	return mv, nil
}

// spawnDoorTriggers computes the trigger box every door team master
// without health or targetname spawns on its first think: the abs boxes of
// the whole team, grown by 60 in x and y.
// C: game/g_func.c:1038 Think_SpawnDoorTrigger
func (m *Map) spawnDoorTriggers() {
	for k := range m.Movers {
		mv := &m.Movers[k]
		if mv.Kind != MoverDoor && mv.Kind != MoverDoorRotating || mv.Classname == "func_water" {
			continue
		}
		if mv.TeamMaster != mv.Entity || mv.Activation&ActTouch == 0 {
			continue // only the team leader spawns a trigger
		}
		mins, maxs := mv.Box.Min, mv.Box.Max
		for _, o := range m.Team(mv.Entity) {
			if o == mv.Entity {
				continue
			}
			if om := m.Mover(o); om != nil {
				shared.AddPointToBounds(om.Box.Min, &mins, &maxs)
				shared.AddPointToBounds(om.Box.Max, &mins, &maxs)
			}
		}
		// expand
		mins[0] -= 60
		mins[1] -= 60
		maxs[0] += 60
		maxs[1] += 60
		b := linkBox(false, Vec3{}, Vec3{}, mins, maxs)
		mv.Trigger = &b
	}
}

// syncTeamSpeeds equalizes the speeds of a door team so all members arrive
// together, as the master's first think does, and computes TravelTime.
// C: game/g_func.c:998 Think_CalcMoveSpeed
func (m *Map) syncTeamSpeeds() {
	for k := range m.Movers {
		mv := &m.Movers[k]
		if mv.Kind != MoverDoor && mv.Kind != MoverDoorRotating || mv.Classname == "func_water" {
			continue // only func_door / func_door_rotating think Think_CalcMoveSpeed
		}
		if mv.TeamMaster != mv.Entity {
			continue // only the team master does this
		}
		team := m.Team(mv.Entity)
		if len(team) == 0 {
			team = []int{mv.Entity}
		}
		members := make([]*Mover, 0, len(team))
		for _, i := range team {
			members = append(members, m.moverOrZero(i))
		}

		// find the smallest distance any member of the team will be moving
		min := abs32(members[0].Distance)
		for _, o := range members[1:] {
			if d := abs32(o.Distance); d < min {
				min = d
			}
		}
		time := min / members[0].Speed

		// adjust speeds so they will all complete at the same time
		for _, o := range members {
			newspeed := float32(math.Abs(float64(o.Distance)) / float64(time))
			ratio := newspeed / o.Speed
			if o.Accel == o.Speed {
				o.Accel = newspeed
			} else {
				o.Accel *= ratio
			}
			if o.Decel == o.Speed {
				o.Decel = newspeed
			} else {
				o.Decel *= ratio
			}
			o.Speed = newspeed
		}
	}
	for k := range m.Movers {
		m.Movers[k].TravelTime = travelTime(&m.Movers[k])
	}
}

// moverOrZero returns the mover of entity i, or a scratch zero mover for a
// non-mover team member (its zero distance still takes part in the C loop).
func (m *Map) moverOrZero(i int) *Mover {
	if mv := m.Mover(i); mv != nil {
		return mv
	}
	return &Mover{Entity: i}
}

// travelTime is the Pos1 -> Pos2 travel time of the linear and angular
// movers that have one.
func travelTime(mv *Mover) float32 {
	switch mv.Kind {
	case MoverDoor, MoverButton, MoverPlat, MoverDoorSecret:
		dir := shared.VectorSubtract(mv.Pos2, mv.Pos1)
		dist := shared.VectorNormalize(&dir)
		if mv.Kind == MoverDoorSecret {
			// two legs: sideways to pos1, then along forward to pos2
			first := shared.VectorSubtract(mv.Pos1, mv.Origin)
			d1 := shared.VectorNormalize(&first)
			f1, f2 := moveFrames(d1, mv.Speed, mv.Accel, mv.Decel), moveFrames(dist, mv.Speed, mv.Accel, mv.Decel)
			if f1 < 0 || f2 < 0 {
				return 0
			}
			return float32(float64(f1+f2)*FRAMETIME + 1.0) // door_secret_move1 pauses 1 s
		}
		if f := moveFrames(dist, mv.Speed, mv.Accel, mv.Decel); f >= 0 {
			return float32(float64(f) * FRAMETIME)
		}
	case MoverDoorRotating:
		if f := angleMoveFrames(shared.VectorLength(shared.VectorSubtract(mv.Pos2, mv.Pos1)), mv.Speed); f >= 0 {
			return float32(float64(f) * FRAMETIME)
		}
	}
	return 0
}

// moveFrames counts the frames from Move_Calc to Move_Done for a straight
// move of dist units started by another entity (so Move_Begin or the first
// Think_AccelMove runs one frame later). It returns -1 when the move never
// completes (zero speed).
// C: game/g_func.c:114 Move_Calc
func moveFrames(dist, speed, accel, decel float32) int {
	if !finitePositive(speed) {
		return -1 // includes the NaN speed of a zero-distance door team
	}
	frames := 1
	if speed == accel && speed == decel {
		// C: game/g_func.c:96 Move_Begin
		if float64(speed)*FRAMETIME >= float64(dist) {
			if dist == 0 {
				return frames
			}
			return frames + 1 // Move_Final
		}
		f := float32(math.Floor(float64(dist/speed) / FRAMETIME))
		rem := float32(float64(dist) - float64(f*speed)*FRAMETIME)
		frames += int(f)
		if rem == 0 {
			return frames
		}
		return frames + 1
	}

	// accelerative
	mi := accelMove{remaining: dist, speed: speed, accel: accel, decel: decel}
	for ; frames < 100000; frames++ {
		// C: game/g_func.c:334 Think_AccelMove
		mi.remaining -= mi.current
		if mi.current == 0 { // starting or blocked
			mi.calc()
		}
		mi.accelerate()
		// will the entire move complete on next frame?
		if mi.remaining <= mi.current {
			if mi.remaining == 0 {
				return frames
			}
			return frames + 1
		}
	}
	return -1
}

// angleMoveFrames counts the frames from AngleMove_Calc to AngleMove_Done
// for a rotation of length degrees started by another entity. The last
// frame is assumed to carry a remainder (the float integration of avelocity
// rarely lands exactly).
// C: game/g_func.c:174 AngleMove_Begin
func angleMoveFrames(length, speed float32) int {
	if !finitePositive(speed) {
		return -1
	}
	frames := 1
	traveltime := length / speed
	if float64(traveltime) < FRAMETIME {
		if length == 0 {
			return frames
		}
		return frames + 1
	}
	return frames + int(math.Floor(float64(traveltime)/FRAMETIME)) + 1
}

func finitePositive(f float32) bool { return f > 0 && !math.IsInf(float64(f), 1) }

// accelMove is the moveinfo_t state the accelerated move uses.
type accelMove struct {
	remaining, speed, accel, decel           float32
	current, moveSpeed, nextSpeed, decelDist float32
}

// accelerationDistance is the AccelerationDistance macro.
// C: game/g_func.c:233 AccelerationDistance
func accelerationDistance(target, rate float32) float32 {
	return target * ((target / rate) + 1) / 2
}

// C: game/g_func.c:235 plat_CalcAcceleratedMove
func (mi *accelMove) calc() {
	mi.moveSpeed = mi.speed
	if mi.remaining < mi.accel {
		mi.current = mi.remaining
		return
	}
	accelDist := accelerationDistance(mi.speed, mi.accel)
	decelDist := accelerationDistance(mi.speed, mi.decel)
	if (mi.remaining - accelDist - decelDist) < 0 {
		f := (mi.accel + mi.decel) / (mi.accel * mi.decel)
		mi.moveSpeed = float32((-2 + math.Sqrt(float64(4-float32(4*f*(-2*mi.remaining))))) / float64(2*f))
		decelDist = accelerationDistance(mi.moveSpeed, mi.decel)
	}
	mi.decelDist = decelDist
}

// C: game/g_func.c:263 plat_Accelerate
func (mi *accelMove) accelerate() {
	// are we decelerating?
	if mi.remaining <= mi.decelDist {
		if mi.remaining < mi.decelDist {
			if mi.nextSpeed != 0 {
				mi.current = mi.nextSpeed
				mi.nextSpeed = 0
				return
			}
			if mi.current > mi.decel {
				mi.current -= mi.decel
			}
		}
		return
	}

	// are we at full speed and need to start decelerating during this move?
	if mi.current == mi.moveSpeed {
		if (mi.remaining - mi.current) < mi.decelDist {
			p1 := mi.remaining - mi.decelDist
			p2 := float32(float64(mi.moveSpeed) * (1.0 - float64(p1/mi.moveSpeed)))
			distance := p1 + p2
			mi.current = mi.moveSpeed
			mi.nextSpeed = mi.moveSpeed - float32(mi.decel*(p2/distance))
			return
		}
	}

	// are we accelerating?
	if mi.current < mi.speed {
		old := mi.current
		// figure simple acceleration up to move_speed
		mi.current += mi.accel
		if mi.current > mi.speed {
			mi.current = mi.speed
		}
		// are we accelerating throughout this entire move?
		if (mi.remaining - mi.current) >= mi.decelDist {
			return
		}
		// during this move we will accelerate from current_speed to
		// move_speed and cross over the decel_distance; figure the average
		// speed for the entire move
		p1 := mi.remaining - mi.decelDist
		p1Speed := float32(float64(old+mi.moveSpeed) / 2.0)
		p2 := float32(float64(mi.moveSpeed) * (1.0 - float64(p1/p1Speed)))
		distance := p1 + p2
		mi.current = float32(p1Speed*(p1/distance)) + float32(mi.moveSpeed*(p2/distance))
		mi.nextSpeed = mi.moveSpeed - float32(mi.decel*(p2/distance))
	}
	// we are at constant velocity (move_speed)
}
