package game

// Port of game/g_utils.c: misc utility functions for game module.

import (
	"fmt"
	"math"

	. "quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/shared"
)

// C: game/g_utils.c:25 G_ProjectSource
func G_ProjectSource(point, distance, forward, right Vec3) Vec3 {
	var result Vec3
	result[0] = point[0] + forward[0]*distance[0] + right[0]*distance[1]
	result[1] = point[1] + forward[1]*distance[0] + right[1]*distance[1]
	result[2] = point[2] + forward[2]*distance[0] + right[2]*distance[1] + distance[2]
	return result
}

// Fofs is the Go form of FOFS(x) for string fields searched by G_Find: an
// accessor of an edict string field ("" is NULL).
type Fofs func(e *Edict) string

// FOFS accessors used with G_Find.
var (
	FOFS_classname    Fofs = func(e *Edict) string { return e.Classname }
	FOFS_targetname   Fofs = func(e *Edict) string { return e.Targetname }
	FOFS_target       Fofs = func(e *Edict) string { return e.Target }
	FOFS_team         Fofs = func(e *Edict) string { return e.Team }
	FOFS_model        Fofs = func(e *Edict) string { return e.Model }
	FOFS_message      Fofs = func(e *Edict) string { return e.Message }
	FOFS_killtarget   Fofs = func(e *Edict) string { return e.Killtarget }
	FOFS_pathtarget   Fofs = func(e *Edict) string { return e.Pathtarget }
	FOFS_combattarget Fofs = func(e *Edict) string { return e.Combattarget }
)

// G_Find searches all active entities for the next one that holds the
// matching string at fieldofs in the structure.
//
// Searches beginning at the edict after from, or the beginning if NULL
// NULL will be returned if the end of the list is reached.
// C: game/g_utils.c:45 G_Find
func (g *Game) G_Find(from *Edict, fieldofs Fofs, match string) *Edict {
	i := 0
	if from != nil {
		i = from.Index + 1
	}
	for ; i < int(g.num_edicts); i++ {
		e := &g.edicts[i]
		if !e.InUse {
			continue
		}
		s := fieldofs(e)
		if s == "" {
			continue
		}
		if shared.Q_stricmp(s, match) == 0 {
			return e
		}
	}
	return nil
}

// findradius returns entities that have origins within a spherical area.
// C: game/g_utils.c:79 findradius
func (g *Game) findradius(from *Edict, org Vec3, rad float32) *Edict {
	var eorg Vec3
	i := 0
	if from != nil {
		i = from.Index + 1
	}
	for ; i < int(g.num_edicts); i++ {
		e := &g.edicts[i]
		if !e.InUse {
			continue
		}
		if e.Solid == SOLID_NOT {
			continue
		}
		for j := 0; j < 3; j++ {
			eorg[j] = float32(float64(org[j]) - (float64(e.S.Origin[j]) + float64(e.Mins[j]+e.Maxs[j])*0.5))
		}
		if shared.VectorLength(eorg) > rad {
			continue
		}
		return e
	}
	return nil
}

// C: game/g_utils.c:114 MAXCHOICES
const MAXCHOICES = 8

// C: game/g_utils.c:116 G_PickTarget
func (g *Game) G_PickTarget(targetname string) *Edict {
	var ent *Edict
	numChoices := 0
	var choice [MAXCHOICES]*Edict

	if targetname == "" {
		g.gi.Dprintf("G_PickTarget called with NULL targetname\n")
		return nil
	}

	for {
		ent = g.G_Find(ent, FOFS_targetname, targetname)
		if ent == nil {
			break
		}
		choice[numChoices] = ent
		numChoices++
		if numChoices == MAXCHOICES {
			break
		}
	}

	if numChoices == 0 {
		g.dprintf("G_PickTarget: target %s not found\n", targetname)
		return nil
	}

	return choice[int(g.rng.Rand())%numChoices]
}

var Think_Delay = defThink("Think_Delay")

func init() { Think_Delay.bind((*Game).Think_Delay) }

// C: game/g_utils.c:150 Think_Delay
func (g *Game) Think_Delay(ent *Edict) {
	g.G_UseTargets(ent, ent.Activator)
	g.G_FreeEdict(ent)
}

// G_UseTargets: the global "activator" should be set to the entity that
// initiated the firing.
//
// If self.delay is set, a DelayedUse entity will be created that will actually
// do the SUB_UseTargets after that many seconds have passed.
//
// Centerprints any self.message to the activator.
//
// Search for (string)targetname in all entities that
// match (string)self.target and call their .use function
// C: game/g_utils.c:171 G_UseTargets
func (g *Game) G_UseTargets(ent, activator *Edict) {
	var t *Edict

	g.enterCall("G_UseTargets")
	defer g.leaveCall()

	//
	// check for a delay
	//
	if ent.Delay != 0 {
		// create a temp object to fire at a later time
		t = g.G_Spawn()
		t.Classname = "DelayedUse"
		t.Nextthink = g.level.Time + ent.Delay
		t.Think = Think_Delay
		t.Activator = activator
		if activator == nil {
			g.gi.Dprintf("Think_Delay with no activator\n")
		}
		t.Message = ent.Message
		t.Target = ent.Target
		t.Killtarget = ent.Killtarget
		return
	}

	//
	// print the message
	//
	if ent.Message != "" && activator.SVFlags&SVF_MONSTER == 0 {
		g.gi.Centerprintf(activator, ent.Message)
		if ent.NoiseIndex != 0 {
			g.gi.Sound(activator, CHAN_AUTO, int(ent.NoiseIndex), 1, ATTN_NORM, 0)
		} else {
			g.gi.Sound(activator, CHAN_AUTO, g.gi.SoundIndex("misc/talk1.wav"), 1, ATTN_NORM, 0)
		}
	}

	//
	// kill killtargets
	//
	if ent.Killtarget != "" {
		t = nil
		for {
			t = g.G_Find(t, FOFS_targetname, ent.Killtarget)
			if t == nil {
				break
			}
			g.G_FreeEdict(t)
			if !ent.InUse {
				g.gi.Dprintf("entity was removed while using killtargets\n")
				return
			}
		}
	}

	//
	// fire targets
	//
	if ent.Target != "" {
		t = nil
		for {
			t = g.G_Find(t, FOFS_targetname, ent.Target)
			if t == nil {
				break
			}
			// doors fire area portals in a specific way
			if shared.Q_stricmp(t.Classname, "func_areaportal") == 0 &&
				(shared.Q_stricmp(ent.Classname, "func_door") == 0 || shared.Q_stricmp(ent.Classname, "func_door_rotating") == 0) {
				continue
			}

			if t == ent {
				g.gi.Dprintf("WARNING: Entity used itself.\n")
			} else {
				if t.Use != nil {
					t.Use.fn(g, t, ent, activator)
				}
			}
			if !ent.InUse {
				g.gi.Dprintf("entity was removed while using targets\n")
				return
			}
		}
	}
}

// tv is TempVector: a convenience function for making temporary vectors.
// C: game/g_utils.c:274 tv
func tv(x, y, z float32) Vec3 { return Vec3{x, y, z} }

// vtos is VectorToString: a convenience function for printing vectors.
// C: game/g_utils.c:300 vtos
func vtos(v Vec3) string {
	return fmt.Sprintf("(%d %d %d)", int32(v[0]), int32(v[1]), int32(v[2]))
}

// C: game/g_utils.c:309
var (
	VEC_UP       = Vec3{0, -1, 0}
	MOVEDIR_UP   = Vec3{0, 0, 1}
	VEC_DOWN     = Vec3{0, -2, 0}
	MOVEDIR_DOWN = Vec3{0, 0, -1}
)

// G_SetMovedir sets movedir from angles and clears angles.
// C: game/g_utils.c:314 G_SetMovedir
func G_SetMovedir(angles *Vec3, movedir *Vec3) {
	if shared.VectorCompare(*angles, VEC_UP) != 0 {
		*movedir = MOVEDIR_UP
	} else if shared.VectorCompare(*angles, VEC_DOWN) != 0 {
		*movedir = MOVEDIR_DOWN
	} else {
		shared.AngleVectors(*angles, movedir, nil, nil)
	}

	*angles = Vec3{}
}

// C: game/g_utils.c:333 vectoyaw
func vectoyaw(vec Vec3) float32 {
	var yaw float32

	if /*vec[YAW] == 0 &&*/ vec[PITCH] == 0 {
		yaw = 0
		if vec[YAW] > 0 {
			yaw = 90
		} else if vec[YAW] < 0 {
			yaw = -90
		}
	} else {
		yaw = float32(int32(math.Atan2(float64(vec[YAW]), float64(vec[PITCH])) * 180 / shared.MPI))
		if yaw < 0 {
			yaw += 360
		}
	}

	return yaw
}

// C: game/g_utils.c:356 vectoangles
func vectoangles(value1 Vec3) Vec3 {
	var forward float32
	var yaw, pitch float32

	if value1[1] == 0 && value1[0] == 0 {
		yaw = 0
		if value1[2] > 0 {
			pitch = 90
		} else {
			pitch = 270
		}
	} else {
		if value1[0] != 0 {
			yaw = float32(int32(math.Atan2(float64(value1[1]), float64(value1[0])) * 180 / shared.MPI))
		} else if value1[1] > 0 {
			yaw = 90
		} else {
			yaw = -90
		}
		if yaw < 0 {
			yaw += 360
		}

		forward = float32(math.Sqrt(float64(value1[0]*value1[0] + value1[1]*value1[1])))
		pitch = float32(int32(math.Atan2(float64(value1[2]), float64(forward)) * 180 / shared.MPI))
		if pitch < 0 {
			pitch += 360
		}
	}

	return Vec3{-pitch, yaw, 0}
}

// C: game/g_utils.c:392 G_CopyString
func G_CopyString(in string) string { return in }

// C: game/g_utils.c:402 G_InitEdict
func (g *Game) G_InitEdict(e *Edict) {
	e.InUse = true
	e.Classname = "noclass"
	e.Gravity = 1.0
	e.S.Number = int32(e.Index)
}

// G_Spawn either finds a free edict, or allocates a new one.
// Try to avoid reusing an entity that was recently freed, because it
// can cause the client to think the entity morphed into something else
// instead of being removed and recreated, which can cause interpolated
// angles and bad trails.
// C: game/g_utils.c:420 G_Spawn
func (g *Game) G_Spawn() *Edict {
	i := int(int32(g.maxclients.Value + 1))
	for ; i < int(g.num_edicts); i++ {
		e := &g.edicts[i]
		// the first couple seconds of server time can involve a lot of
		// freeing and allocating, so relax the replacement policy
		if !e.InUse && (e.Freetime < 2 || float64(g.level.Time-e.Freetime) > 0.5) {
			g.G_InitEdict(e)
			return e
		}
	}

	if i == int(g.game.Maxentities) {
		g.gi.Error("ED_Alloc: no free edicts")
	}

	g.num_edicts++
	e := &g.edicts[i]
	g.G_InitEdict(e)
	return e
}

// clearEdict is memset(ed, 0, sizeof(*ed)) keeping the Go-only Index.
func clearEdict(ed *Edict) {
	idx := ed.Index
	*ed = Edict{}
	ed.Index = idx
}

// G_FreeEdict marks the edict as free.
// C: game/g_utils.c:452 G_FreeEdict
func (g *Game) G_FreeEdict(ed *Edict) {
	g.gi.UnlinkEntity(ed) // unlink from world

	if float32(ed.Index) <= g.maxclients.Value+BODY_QUEUE_SIZE {
		//		gi.dprintf("tried to free special edict\n");
		return
	}

	clearEdict(ed)
	ed.Classname = "freed"
	ed.Freetime = g.level.Time
	ed.InUse = false
}

// C: game/g_utils.c:475 G_TouchTriggers
func (g *Game) G_TouchTriggers(ent *Edict) {
	// dead things don't activate triggers!
	if (ent.Client != nil || ent.SVFlags&SVF_MONSTER != 0) && ent.Health <= 0 {
		return
	}

	touch := make([]*Edict, MAX_EDICTS)
	num := g.gi.BoxEdicts(&ent.AbsMin, &ent.AbsMax, touch, AREA_TRIGGERS)

	// be careful, it is possible to have an entity in this
	// list removed before we get to it (killtriggered)
	for i := 0; i < num; i++ {
		hit := touch[i]
		if !hit.InUse {
			continue
		}
		if hit.Touch == nil {
			continue
		}
		hit.Touch.fn(g, hit, ent, nil, nil)
	}
}

// G_TouchSolids: call after linking a new trigger in during gameplay
// to force all entities it covers to immediately touch it.
// C: game/g_utils.c:508 G_TouchSolids
func (g *Game) G_TouchSolids(ent *Edict) {
	touch := make([]*Edict, MAX_EDICTS)
	num := g.gi.BoxEdicts(&ent.AbsMin, &ent.AbsMax, touch, AREA_SOLID)

	// be careful, it is possible to have an entity in this
	// list removed before we get to it (killtriggered)
	for i := 0; i < num; i++ {
		hit := touch[i]
		if !hit.InUse {
			continue
		}
		if ent.Touch != nil {
			ent.Touch.fn(g, hit, ent, nil, nil)
		}
		if !ent.InUse {
			break
		}
	}
}

// KillBox kills all entities that would touch the proposed new positioning
// of ent.  Ent should be unlinked before calling this!
// C: game/g_utils.c:545 KillBox
func (g *Game) KillBox(ent *Edict) bool {
	for {
		tr := g.gi.Trace(&ent.S.Origin, &ent.Mins, &ent.Maxs, &ent.S.Origin, nil, MASK_PLAYERSOLID)
		if tr.Ent == nil {
			break
		}

		// nail it
		dir := shared.Vec3Origin
		g.T_Damage(tr.Ent, ent, ent, &dir, ent.S.Origin, shared.Vec3Origin, 100000, 0, DAMAGE_NO_PROTECTION, MOD_TELEFRAG)

		// if we didn't kill it, fail
		if tr.Ent.Solid != 0 {
			return false
		}
	}

	return true // all clear
}
