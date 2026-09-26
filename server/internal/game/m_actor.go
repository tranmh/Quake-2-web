package game

// Port of game/m_actor.c (misc_actor, target_actor).

import (
	. "quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/shared"
)

var (
	actor_stand        = defThink("actor_stand")
	actor_walk         = defThink("actor_walk")
	actor_run          = defThink("actor_run")
	actor_pain         = defPain("actor_pain")
	actor_dead         = defThink("actor_dead")
	actor_die          = defDie("actor_die")
	actor_fire         = defThink("actor_fire")
	actor_attack       = defThink("actor_attack")
	actor_use          = defUse("actor_use")
	target_actor_touch = defTouch("target_actor_touch")
)

func init() {
	actor_stand.bind((*Game).actor_stand)
	actor_walk.bind((*Game).actor_walk)
	actor_run.bind((*Game).actor_run)
	actor_pain.bind((*Game).actor_pain)
	actor_dead.bind((*Game).actor_dead)
	actor_die.bind((*Game).actor_die)
	actor_fire.bind((*Game).actor_fire)
	actor_attack.bind((*Game).actor_attack)
	actor_use.bind((*Game).actor_use)
	target_actor_touch.bind((*Game).target_actor_touch)
}

func init() {
	RegisterSpawn("misc_actor", (*Game).SP_misc_actor)
	RegisterSpawn("target_actor", (*Game).SP_target_actor)
}

// C: game/m_actor.c:39 actor_frames_stand
var actor_frames_stand = []MFrame{
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
}

// C: game/m_actor.c:97 actor_frames_walk
var actor_frames_walk = []MFrame{
	{ai_walk, 0, nil},
	{ai_walk, 6, nil},
	{ai_walk, 10, nil},
	{ai_walk, 3, nil},
	{ai_walk, 2, nil},
	{ai_walk, 7, nil},
	{ai_walk, 10, nil},
	{ai_walk, 1, nil},
	{ai_walk, 4, nil},
	{ai_walk, 0, nil},
	{ai_walk, 0, nil},
}

// C: game/m_actor.c:119 actor_frames_run
var actor_frames_run = []MFrame{
	{ai_run, 4, nil},
	{ai_run, 15, nil},
	{ai_run, 15, nil},
	{ai_run, 8, nil},
	{ai_run, 20, nil},
	{ai_run, 15, nil},
	{ai_run, 8, nil},
	{ai_run, 17, nil},
	{ai_run, 12, nil},
	{ai_run, -2, nil},
	{ai_run, -2, nil},
	{ai_run, -1, nil},
}

// C: game/m_actor.c:157 actor_frames_pain1
var actor_frames_pain1 = []MFrame{
	{ai_move, -5, nil},
	{ai_move, 4, nil},
	{ai_move, 1, nil},
}

// C: game/m_actor.c:165 actor_frames_pain2
var actor_frames_pain2 = []MFrame{
	{ai_move, -4, nil},
	{ai_move, 4, nil},
	{ai_move, 0, nil},
}

// C: game/m_actor.c:173 actor_frames_pain3
var actor_frames_pain3 = []MFrame{
	{ai_move, -1, nil},
	{ai_move, 1, nil},
	{ai_move, 0, nil},
}

// C: game/m_actor.c:181 actor_frames_flipoff
var actor_frames_flipoff = []MFrame{
	{ai_turn, 0, nil},
	{ai_turn, 0, nil},
	{ai_turn, 0, nil},
	{ai_turn, 0, nil},
	{ai_turn, 0, nil},
	{ai_turn, 0, nil},
	{ai_turn, 0, nil},
	{ai_turn, 0, nil},
	{ai_turn, 0, nil},
	{ai_turn, 0, nil},
	{ai_turn, 0, nil},
	{ai_turn, 0, nil},
	{ai_turn, 0, nil},
	{ai_turn, 0, nil},
}

// C: game/m_actor.c:200 actor_frames_taunt
var actor_frames_taunt = []MFrame{
	{ai_turn, 0, nil},
	{ai_turn, 0, nil},
	{ai_turn, 0, nil},
	{ai_turn, 0, nil},
	{ai_turn, 0, nil},
	{ai_turn, 0, nil},
	{ai_turn, 0, nil},
	{ai_turn, 0, nil},
	{ai_turn, 0, nil},
	{ai_turn, 0, nil},
	{ai_turn, 0, nil},
	{ai_turn, 0, nil},
	{ai_turn, 0, nil},
	{ai_turn, 0, nil},
	{ai_turn, 0, nil},
	{ai_turn, 0, nil},
	{ai_turn, 0, nil},
}

// C: game/m_actor.c:309 actor_frames_death1
var actor_frames_death1 = []MFrame{
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, -13, nil},
	{ai_move, 14, nil},
	{ai_move, 3, nil},
	{ai_move, -2, nil},
	{ai_move, 1, nil},
}

// C: game/m_actor.c:321 actor_frames_death2
var actor_frames_death2 = []MFrame{
	{ai_move, 0, nil},
	{ai_move, 7, nil},
	{ai_move, -6, nil},
	{ai_move, -5, nil},
	{ai_move, 1, nil},
	{ai_move, 0, nil},
	{ai_move, -1, nil},
	{ai_move, -2, nil},
	{ai_move, -1, nil},
	{ai_move, -9, nil},
	{ai_move, -13, nil},
	{ai_move, -13, nil},
	{ai_move, 0, nil},
}

// C: game/m_actor.c:382 actor_frames_attack
var actor_frames_attack = []MFrame{
	{ai_charge, -2, actor_fire},
	{ai_charge, -2, nil},
	{ai_charge, 3, nil},
	{ai_charge, 2, nil},
}

// C: game/m_actor.c:85 actor_move_stand
var actor_move_stand = defMMove("actor_move_stand", actor_FRAME_stand101, actor_FRAME_stand140, actor_frames_stand, nil)

// C: game/m_actor.c:111 actor_move_walk
var actor_move_walk = defMMove("actor_move_walk", actor_FRAME_walk01, actor_FRAME_walk08, actor_frames_walk, nil)

// C: game/m_actor.c:134 actor_move_run
var actor_move_run = defMMove("actor_move_run", actor_FRAME_run02, actor_FRAME_run07, actor_frames_run, nil)

// C: game/m_actor.c:163 actor_move_pain1
var actor_move_pain1 = defMMove("actor_move_pain1", actor_FRAME_pain101, actor_FRAME_pain103, actor_frames_pain1, actor_run)

// C: game/m_actor.c:171 actor_move_pain2
var actor_move_pain2 = defMMove("actor_move_pain2", actor_FRAME_pain201, actor_FRAME_pain203, actor_frames_pain2, actor_run)

// C: game/m_actor.c:179 actor_move_pain3
var actor_move_pain3 = defMMove("actor_move_pain3", actor_FRAME_pain301, actor_FRAME_pain303, actor_frames_pain3, actor_run)

// C: game/m_actor.c:198 actor_move_flipoff
var actor_move_flipoff = defMMove("actor_move_flipoff", actor_FRAME_flip01, actor_FRAME_flip14, actor_frames_flipoff, actor_run)

// C: game/m_actor.c:220 actor_move_taunt
var actor_move_taunt = defMMove("actor_move_taunt", actor_FRAME_taunt01, actor_FRAME_taunt17, actor_frames_taunt, actor_run)

// C: game/m_actor.c:319 actor_move_death1
var actor_move_death1 = defMMove("actor_move_death1", actor_FRAME_death101, actor_FRAME_death107, actor_frames_death1, actor_dead)

// C: game/m_actor.c:337 actor_move_death2
var actor_move_death2 = defMMove("actor_move_death2", actor_FRAME_death201, actor_FRAME_death213, actor_frames_death2, actor_dead)

// C: game/m_actor.c:389 actor_move_attack
var actor_move_attack = defMMove("actor_move_attack", actor_FRAME_attak01, actor_FRAME_attak04, actor_frames_attack, actor_run)

// C: game/m_actor.c:25 MAX_ACTOR_NAMES
const MAX_ACTOR_NAMES = 8

// C: game/m_actor.c:26 actor_names
var actor_names = [MAX_ACTOR_NAMES]string{
	"Hellrot",
	"Tokay",
	"Killme",
	"Disruptor",
	"Adrianator",
	"Rambear",
	"Titus",
	"Bitterman",
}

// C: game/m_actor.c:87 actor_stand
func (g *Game) actor_stand(self *Edict) {
	self.Monsterinfo.Currentmove = actor_move_stand

	// randomize on startup
	if float64(g.level.Time) < 1.0 {
		self.S.Frame = self.Monsterinfo.Currentmove.Firstframe + (g.rng.Rand() % (self.Monsterinfo.Currentmove.Lastframe - self.Monsterinfo.Currentmove.Firstframe + 1))
	}
}

// C: game/m_actor.c:113 actor_walk
func (g *Game) actor_walk(self *Edict) {
	self.Monsterinfo.Currentmove = actor_move_walk
}

// C: game/m_actor.c:136 actor_run
func (g *Game) actor_run(self *Edict) {
	if (g.level.Time < self.PainDebounceTime) && (self.Enemy == nil) {
		if self.Movetarget != nil {
			g.actor_walk(self)
		} else {
			g.actor_stand(self)
		}
		return
	}

	if self.Monsterinfo.Aiflags&AI_STAND_GROUND != 0 {
		g.actor_stand(self)
		return
	}

	self.Monsterinfo.Currentmove = actor_move_run
}

// C: game/m_actor.c:222 messages
var messages = [...]string{
	"Watch it",
	"#$@*&",
	"Idiot",
	"Check your targets",
}

// C: game/m_actor.c:230 actor_pain
func (g *Game) actor_pain(self *Edict, other *Edict, kick float32, damage int32) {
	var n int32

	if self.Health < (self.MaxHealth / 2) {
		self.S.SkinNum = 1
	}

	if g.level.Time < self.PainDebounceTime {
		return
	}

	self.PainDebounceTime = g.level.Time + 3
	//	gi.sound (self, CHAN_VOICE, actor.sound_pain, 1, ATTN_NORM, 0);

	if (other.Client != nil) && (float64(g.random()) < 0.4) {
		var v Vec3
		var name string

		v = shared.VectorSubtract(other.S.Origin, self.S.Origin)
		self.IdealYaw = vectoyaw(v)
		if float64(g.random()) < 0.5 {
			self.Monsterinfo.Currentmove = actor_move_flipoff
		} else {
			self.Monsterinfo.Currentmove = actor_move_taunt
		}
		name = actor_names[self.Index%MAX_ACTOR_NAMES]
		g.cprintf(other, PRINT_CHAT, "%s: %s!\n", name, messages[g.rng.Rand()%3])
		return
	}

	n = g.rng.Rand() % 3
	if n == 0 {
		self.Monsterinfo.Currentmove = actor_move_pain1
	} else if n == 1 {
		self.Monsterinfo.Currentmove = actor_move_pain2
	} else {
		self.Monsterinfo.Currentmove = actor_move_pain3
	}
}

// C: game/m_actor.c:269 actorMachineGun
func (g *Game) actorMachineGun(self *Edict) {
	var start, target Vec3
	var forward, right Vec3

	shared.AngleVectors(self.S.Angles, &forward, &right, nil)
	start = G_ProjectSource(self.S.Origin, MonsterFlashOffset[MZ2_ACTOR_MACHINEGUN_1], forward, right)
	if self.Enemy != nil {
		if self.Enemy.Health > 0 {
			target = shared.VectorMA(self.Enemy.S.Origin, -0.2, self.Enemy.Velocity)
			target[2] += float32(self.Enemy.Viewheight)
		} else {
			target = self.Enemy.AbsMin
			target[2] += (self.Enemy.Size[2] / 2)
		}
		forward = shared.VectorSubtract(target, start)
		shared.VectorNormalize(&forward)
	} else {
		shared.AngleVectors(self.S.Angles, &forward, nil, nil)
	}
	g.monster_fire_bullet(self, start, forward, 3, 4, DEFAULT_BULLET_HSPREAD, DEFAULT_BULLET_VSPREAD, MZ2_ACTOR_MACHINEGUN_1)
}

// C: game/m_actor.c:299 actor_dead
func (g *Game) actor_dead(self *Edict) {
	self.Mins = Vec3{-16, -16, -24}
	self.Maxs = Vec3{16, 16, -8}
	self.Movetype = MOVETYPE_TOSS
	self.SVFlags |= SVF_DEADMONSTER
	self.Nextthink = 0
	g.gi.LinkEntity(self)
}

// C: game/m_actor.c:339 actor_die
func (g *Game) actor_die(self *Edict, inflictor *Edict, attacker *Edict, damage int32, point Vec3) {
	var n int32

	// check for gib
	if self.Health <= -80 {
		//		gi.sound (self, CHAN_VOICE, actor.sound_gib, 1, ATTN_NORM, 0);
		for n = 0; n < 2; n++ {
			g.ThrowGib(self, "models/objects/gibs/bone/tris.md2", damage, GIB_ORGANIC)
		}
		for n = 0; n < 4; n++ {
			g.ThrowGib(self, "models/objects/gibs/sm_meat/tris.md2", damage, GIB_ORGANIC)
		}
		g.ThrowHead(self, "models/objects/gibs/head2/tris.md2", damage, GIB_ORGANIC)
		self.Deadflag = DEAD_DEAD
		return
	}

	if self.Deadflag == DEAD_DEAD {
		return
	}

	// regular death
	//	gi.sound (self, CHAN_VOICE, actor.sound_die, 1, ATTN_NORM, 0);
	self.Deadflag = DEAD_DEAD
	self.Takedamage = DAMAGE_YES

	n = g.rng.Rand() % 2
	if n == 0 {
		self.Monsterinfo.Currentmove = actor_move_death1
	} else {
		self.Monsterinfo.Currentmove = actor_move_death2
	}
}

// C: game/m_actor.c:372 actor_fire
func (g *Game) actor_fire(self *Edict) {
	g.actorMachineGun(self)

	if g.level.Time >= self.Monsterinfo.Pausetime {
		self.Monsterinfo.Aiflags &^= AI_HOLD_FRAME
	} else {
		self.Monsterinfo.Aiflags |= AI_HOLD_FRAME
	}
}

// C: game/m_actor.c:391 actor_attack
func (g *Game) actor_attack(self *Edict) {
	var n int32

	self.Monsterinfo.Currentmove = actor_move_attack
	n = (g.rng.Rand() & 15) + 3 + 7
	self.Monsterinfo.Pausetime = float32(float64(g.level.Time) + float64(n)*FRAMETIME)
}

// C: game/m_actor.c:401 actor_use
func (g *Game) actor_use(self *Edict, other *Edict, activator *Edict) {
	var v Vec3

	self.Movetarget = g.G_PickTarget(self.Target)
	self.Goalentity = self.Movetarget
	if (self.Movetarget == nil) || (self.Movetarget.Classname != "target_actor") {
		g.dprintf("%s has bad target %s at %s\n", self.Classname, self.Target, vtos(self.S.Origin))
		self.Target = ""
		self.Monsterinfo.Pausetime = 100000000
		self.Monsterinfo.Stand.fn(g, self)
		return
	}

	v = shared.VectorSubtract(self.Goalentity.S.Origin, self.S.Origin)
	self.S.Angles[YAW] = vectoyaw(v)
	self.IdealYaw = self.S.Angles[YAW]
	self.Monsterinfo.Walk.fn(g, self)
	self.Target = ""
}

// QUAKED misc_actor (1 .5 0) (-16 -16 -24) (16 16 32)
//
// C: game/m_actor.c:425 SP_misc_actor
func (g *Game) SP_misc_actor(self *Edict) {
	if g.deathmatch.Value != 0 {
		g.G_FreeEdict(self)
		return
	}

	if self.Targetname == "" {
		g.dprintf("untargeted %s at %s\n", self.Classname, vtos(self.S.Origin))
		g.G_FreeEdict(self)
		return
	}

	if self.Target == "" {
		g.dprintf("%s with no target at %s\n", self.Classname, vtos(self.S.Origin))
		g.G_FreeEdict(self)
		return
	}

	self.Movetype = MOVETYPE_STEP
	self.Solid = SOLID_BBOX
	self.S.ModelIndex = int32(g.gi.ModelIndex("players/male/tris.md2"))
	self.Mins = Vec3{-16, -16, -24}
	self.Maxs = Vec3{16, 16, 32}

	if self.Health == 0 {
		self.Health = 100
	}
	self.Mass = 200

	self.Pain = actor_pain
	self.Die = actor_die

	self.Monsterinfo.Stand = actor_stand
	self.Monsterinfo.Walk = actor_walk
	self.Monsterinfo.Run = actor_run
	self.Monsterinfo.Attack = actor_attack
	self.Monsterinfo.Melee = nil
	self.Monsterinfo.Sight = nil

	self.Monsterinfo.Aiflags |= AI_GOOD_GUY

	g.gi.LinkEntity(self)

	self.Monsterinfo.Currentmove = actor_move_stand
	self.Monsterinfo.Scale = actor_MODEL_SCALE

	g.walkmonster_start(self)

	// actors always start in a dormant state, they *must* be used to get going
	self.Use = actor_use
}

// QUAKED target_actor (.5 .3 0) (-8 -8 -8) (8 8 8) JUMP SHOOT ATTACK x HOLD BRUTAL
// JUMP			jump in set direction upon reaching this target
// SHOOT			take a single shot at the pathtarget
// ATTACK			attack pathtarget until it or actor is dead
//
// "target"		next target_actor
// "pathtarget"	target of any action to be taken at this point
// "wait"			amount of time actor should pause at this point
// "message"		actor will "say" this to the player
//
// for JUMP only:
// "speed"			speed thrown forward (default 200)
// "height"		speed thrown upwards (default 200)
//
// C: game/m_actor.c:496 target_actor_touch
func (g *Game) target_actor_touch(self *Edict, other *Edict, plane *CPlane, surf *CSurface) {
	var v Vec3

	if other.Movetarget != self {
		return
	}

	if other.Enemy != nil {
		return
	}

	other.Movetarget = nil
	other.Goalentity = nil

	if self.Message != "" {
		var n int32
		var ent *Edict

		for n = 1; n <= g.game.Maxclients; n++ {
			ent = &g.edicts[n]
			if !ent.InUse {
				continue
			}
			g.cprintf(ent, PRINT_CHAT, "%s: %s\n", actor_names[other.Index%MAX_ACTOR_NAMES], self.Message)
		}
	}

	if self.Spawnflags&1 != 0 { //jump
		other.Velocity[0] = self.Movedir[0] * self.Speed
		other.Velocity[1] = self.Movedir[1] * self.Speed

		if other.Groundentity != nil {
			other.Groundentity = nil
			other.Velocity[2] = self.Movedir[2]
			g.gi.Sound(other, CHAN_VOICE, g.gi.SoundIndex("player/male/jump1.wav"), 1, ATTN_NORM, 0)
		}
	}

	if self.Spawnflags&2 != 0 { //shoot
	} else if self.Spawnflags&4 != 0 { //attack
		other.Enemy = g.G_PickTarget(self.Pathtarget)
		if other.Enemy != nil {
			other.Goalentity = other.Enemy
			if self.Spawnflags&32 != 0 {
				other.Monsterinfo.Aiflags |= AI_BRUTAL
			}
			if self.Spawnflags&16 != 0 {
				other.Monsterinfo.Aiflags |= AI_STAND_GROUND
				g.actor_stand(other)
			} else {
				g.actor_run(other)
			}
		}
	}

	if (self.Spawnflags&6 == 0) && (self.Pathtarget != "") {
		var savetarget string

		savetarget = self.Target
		self.Target = self.Pathtarget
		g.G_UseTargets(self, other)
		self.Target = savetarget
	}

	other.Movetarget = g.G_PickTarget(self.Target)

	if other.Goalentity == nil {
		other.Goalentity = other.Movetarget
	}

	if other.Movetarget == nil && other.Enemy == nil {
		other.Monsterinfo.Pausetime = g.level.Time + 100000000
		other.Monsterinfo.Stand.fn(g, other)
	} else if other.Movetarget == other.Goalentity {
		v = shared.VectorSubtract(other.Movetarget.S.Origin, other.S.Origin)
		other.IdealYaw = vectoyaw(v)
	}
}

// C: game/m_actor.c:585 SP_target_actor
func (g *Game) SP_target_actor(self *Edict) {
	if self.Targetname == "" {
		g.dprintf("%s with no targetname at %s\n", self.Classname, vtos(self.S.Origin))
	}

	self.Solid = SOLID_TRIGGER
	self.Touch = target_actor_touch
	self.Mins = Vec3{-8, -8, -8}
	self.Maxs = Vec3{8, 8, 8}
	self.SVFlags = SVF_NOCLIENT

	if self.Spawnflags&1 != 0 {
		if self.Speed == 0 {
			self.Speed = 200
		}
		if g.st.Height == 0 {
			g.st.Height = 200
		}
		if self.S.Angles[YAW] == 0 {
			self.S.Angles[YAW] = 360
		}
		G_SetMovedir(&self.S.Angles, &self.Movedir)
		self.Movedir[2] = float32(g.st.Height)
	}

	g.gi.LinkEntity(self)
}
