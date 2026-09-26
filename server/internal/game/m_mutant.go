package game

// Port of game/m_mutant.c.

import (
	. "quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/shared"
)

// statics of m_mutant.c
type mutantStatics struct {
	SoundSwing  int // sound_swing
	SoundHit    int // sound_hit
	SoundHit2   int // sound_hit2
	SoundDeath  int // sound_death
	SoundIdle   int // sound_idle
	SoundPain1  int // sound_pain1
	SoundPain2  int // sound_pain2
	SoundSight  int // sound_sight
	SoundSearch int // sound_search
	SoundStep1  int // sound_step1
	SoundStep2  int // sound_step2
	SoundStep3  int // sound_step3
	SoundThud   int // sound_thud
}

var (
	mutant_step          = defThink("mutant_step")
	mutant_sight         = defBlocked("mutant_sight")
	mutant_search        = defThink("mutant_search")
	mutant_stand         = defThink("mutant_stand")
	mutant_idle_loop     = defThink("mutant_idle_loop")
	mutant_idle          = defThink("mutant_idle")
	mutant_walk_loop     = defThink("mutant_walk_loop")
	mutant_walk          = defThink("mutant_walk")
	mutant_run           = defThink("mutant_run")
	mutant_hit_left      = defThink("mutant_hit_left")
	mutant_hit_right     = defThink("mutant_hit_right")
	mutant_check_refire  = defThink("mutant_check_refire")
	mutant_melee         = defThink("mutant_melee")
	mutant_jump_takeoff  = defThink("mutant_jump_takeoff")
	mutant_check_landing = defThink("mutant_check_landing")
	mutant_jump          = defThink("mutant_jump")
	mutant_checkattack   = defCheckAttack("mutant_checkattack")
	mutant_pain          = defPain("mutant_pain")
	mutant_dead          = defThink("mutant_dead")
	mutant_die           = defDie("mutant_die")
	mutant_jump_touch    = defTouch("mutant_jump_touch")
)

func init() {
	mutant_step.bind((*Game).mutant_step)
	mutant_sight.bind((*Game).mutant_sight)
	mutant_search.bind((*Game).mutant_search)
	mutant_stand.bind((*Game).mutant_stand)
	mutant_idle_loop.bind((*Game).mutant_idle_loop)
	mutant_idle.bind((*Game).mutant_idle)
	mutant_walk_loop.bind((*Game).mutant_walk_loop)
	mutant_walk.bind((*Game).mutant_walk)
	mutant_run.bind((*Game).mutant_run)
	mutant_hit_left.bind((*Game).mutant_hit_left)
	mutant_hit_right.bind((*Game).mutant_hit_right)
	mutant_check_refire.bind((*Game).mutant_check_refire)
	mutant_melee.bind((*Game).mutant_melee)
	mutant_jump_takeoff.bind((*Game).mutant_jump_takeoff)
	mutant_check_landing.bind((*Game).mutant_check_landing)
	mutant_jump.bind((*Game).mutant_jump)
	mutant_checkattack.bind((*Game).mutant_checkattack)
	mutant_pain.bind((*Game).mutant_pain)
	mutant_dead.bind((*Game).mutant_dead)
	mutant_die.bind((*Game).mutant_die)
	mutant_jump_touch.bind((*Game).mutant_jump_touch)
}

func init() {
	RegisterMonsterStatics("m_mutant", func() any { return new(mutantStatics) })
	RegisterSpawn("monster_mutant", (*Game).SP_monster_mutant)
}

//
// SOUNDS
//

// C: game/m_mutant.c:50 mutant_step
func (g *Game) mutant_step(self *Edict) {
	s := monsterStatics[mutantStatics](g, "m_mutant")
	var n int32
	n = (g.rng.Rand() + 1) % 3
	if n == 0 {
		g.gi.Sound(self, CHAN_VOICE, s.SoundStep1, 1, ATTN_NORM, 0)
	} else if n == 1 {
		g.gi.Sound(self, CHAN_VOICE, s.SoundStep2, 1, ATTN_NORM, 0)
	} else {
		g.gi.Sound(self, CHAN_VOICE, s.SoundStep3, 1, ATTN_NORM, 0)
	}
}

// C: game/m_mutant.c:62 mutant_sight
func (g *Game) mutant_sight(self *Edict, other *Edict) {
	s := monsterStatics[mutantStatics](g, "m_mutant")
	g.gi.Sound(self, CHAN_VOICE, s.SoundSight, 1, ATTN_NORM, 0)
}

// C: game/m_mutant.c:67 mutant_search
func (g *Game) mutant_search(self *Edict) {
	s := monsterStatics[mutantStatics](g, "m_mutant")
	g.gi.Sound(self, CHAN_VOICE, s.SoundSearch, 1, ATTN_NORM, 0)
}

// C: game/m_mutant.c:72 mutant_swing
func (g *Game) mutant_swing(self *Edict) {
	s := monsterStatics[mutantStatics](g, "m_mutant")
	g.gi.Sound(self, CHAN_VOICE, s.SoundSwing, 1, ATTN_NORM, 0)
}

//
// STAND
//

// C: game/m_mutant.c:82 mutant_frames_stand
var mutant_frames_stand = []MFrame{
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

// C: game/m_mutant.c:141 mutant_move_stand
var mutant_move_stand = defMMove("mutant_move_stand", mutant_FRAME_stand101, mutant_FRAME_stand151, mutant_frames_stand, nil)

// C: game/m_mutant.c:143 mutant_stand
func (g *Game) mutant_stand(self *Edict) {
	self.Monsterinfo.Currentmove = mutant_move_stand
}

//
// IDLE
//

// C: game/m_mutant.c:153 mutant_idle_loop
func (g *Game) mutant_idle_loop(self *Edict) {
	if float64(g.random()) < 0.75 {
		self.Monsterinfo.Nextframe = mutant_FRAME_stand155
	}
}

// C: game/m_mutant.c:159 mutant_frames_idle
var mutant_frames_idle = []MFrame{
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, mutant_idle_loop},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
}

// C: game/m_mutant.c:175 mutant_move_idle
var mutant_move_idle = defMMove("mutant_move_idle", mutant_FRAME_stand152, mutant_FRAME_stand164, mutant_frames_idle, mutant_stand)

// C: game/m_mutant.c:177 mutant_idle
func (g *Game) mutant_idle(self *Edict) {
	s := monsterStatics[mutantStatics](g, "m_mutant")
	self.Monsterinfo.Currentmove = mutant_move_idle
	g.gi.Sound(self, CHAN_VOICE, s.SoundIdle, 1, ATTN_IDLE, 0)
}

//
// WALK
//

// C: game/m_mutant.c:190 mutant_frames_walk
var mutant_frames_walk = []MFrame{
	{ai_walk, 3, nil},
	{ai_walk, 1, nil},
	{ai_walk, 5, nil},
	{ai_walk, 10, nil},
	{ai_walk, 13, nil},
	{ai_walk, 10, nil},
	{ai_walk, 0, nil},
	{ai_walk, 5, nil},
	{ai_walk, 6, nil},
	{ai_walk, 16, nil},
	{ai_walk, 15, nil},
	{ai_walk, 6, nil},
}

// C: game/m_mutant.c:205 mutant_move_walk
var mutant_move_walk = defMMove("mutant_move_walk", mutant_FRAME_walk05, mutant_FRAME_walk16, mutant_frames_walk, nil)

// C: game/m_mutant.c:207 mutant_walk_loop
func (g *Game) mutant_walk_loop(self *Edict) {
	self.Monsterinfo.Currentmove = mutant_move_walk
}

// C: game/m_mutant.c:212 mutant_frames_start_walk
var mutant_frames_start_walk = []MFrame{
	{ai_walk, 5, nil},
	{ai_walk, 5, nil},
	{ai_walk, -2, nil},
	{ai_walk, 1, nil},
}

// C: game/m_mutant.c:219 mutant_move_start_walk
var mutant_move_start_walk = defMMove("mutant_move_start_walk", mutant_FRAME_walk01, mutant_FRAME_walk04, mutant_frames_start_walk, mutant_walk_loop)

// C: game/m_mutant.c:221 mutant_walk
func (g *Game) mutant_walk(self *Edict) {
	self.Monsterinfo.Currentmove = mutant_move_start_walk
}

//
// RUN
//

// C: game/m_mutant.c:231 mutant_frames_run
var mutant_frames_run = []MFrame{
	{ai_run, 40, nil},
	{ai_run, 40, mutant_step},
	{ai_run, 24, nil},
	{ai_run, 5, mutant_step},
	{ai_run, 17, nil},
	{ai_run, 10, nil},
}

// C: game/m_mutant.c:240 mutant_move_run
var mutant_move_run = defMMove("mutant_move_run", mutant_FRAME_run03, mutant_FRAME_run08, mutant_frames_run, nil)

// C: game/m_mutant.c:242 mutant_run
func (g *Game) mutant_run(self *Edict) {
	if self.Monsterinfo.Aiflags&AI_STAND_GROUND != 0 {
		self.Monsterinfo.Currentmove = mutant_move_stand
	} else {
		self.Monsterinfo.Currentmove = mutant_move_run
	}
}

//
// MELEE
//

// C: game/m_mutant.c:255 mutant_hit_left
func (g *Game) mutant_hit_left(self *Edict) {
	s := monsterStatics[mutantStatics](g, "m_mutant")
	aim := Vec3{MELEE_DISTANCE, self.Mins[0], 8}
	if g.fire_hit(self, aim, (10 + (g.rng.Rand() % 5)), 100) {
		g.gi.Sound(self, CHAN_WEAPON, s.SoundHit, 1, ATTN_NORM, 0)
	} else {
		g.gi.Sound(self, CHAN_WEAPON, s.SoundSwing, 1, ATTN_NORM, 0)
	}
}

// C: game/m_mutant.c:266 mutant_hit_right
func (g *Game) mutant_hit_right(self *Edict) {
	s := monsterStatics[mutantStatics](g, "m_mutant")
	aim := Vec3{MELEE_DISTANCE, self.Maxs[0], 8}
	if g.fire_hit(self, aim, (10 + (g.rng.Rand() % 5)), 100) {
		g.gi.Sound(self, CHAN_WEAPON, s.SoundHit2, 1, ATTN_NORM, 0)
	} else {
		g.gi.Sound(self, CHAN_WEAPON, s.SoundSwing, 1, ATTN_NORM, 0)
	}
}

// C: game/m_mutant.c:277 mutant_check_refire
func (g *Game) mutant_check_refire(self *Edict) {
	if self.Enemy == nil || !self.Enemy.InUse || self.Enemy.Health <= 0 {
		return
	}

	if ((g.skill.Value == 3) && (float64(g.random()) < 0.5)) || (g.range_(self, self.Enemy) == RANGE_MELEE) {
		self.Monsterinfo.Nextframe = mutant_FRAME_attack09
	}
}

// C: game/m_mutant.c:286 mutant_frames_attack
var mutant_frames_attack = []MFrame{
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, mutant_hit_left},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, mutant_hit_right},
	{ai_charge, 0, mutant_check_refire},
}

// C: game/m_mutant.c:296 mutant_move_attack
var mutant_move_attack = defMMove("mutant_move_attack", mutant_FRAME_attack09, mutant_FRAME_attack15, mutant_frames_attack, mutant_run)

// C: game/m_mutant.c:298 mutant_melee
func (g *Game) mutant_melee(self *Edict) {
	self.Monsterinfo.Currentmove = mutant_move_attack
}

//
// ATTACK
//

// C: game/m_mutant.c:308 mutant_jump_touch
func (g *Game) mutant_jump_touch(self, other *Edict, plane *CPlane, surf *CSurface) {
	if self.Health <= 0 {
		self.Touch = nil
		return
	}

	if other.Takedamage != 0 {
		if shared.VectorLength(self.Velocity) > 400 {
			var point, normal Vec3
			var damage int32

			normal = self.Velocity
			shared.VectorNormalize(&normal)
			point = shared.VectorMA(self.S.Origin, self.Maxs[0], normal)
			damage = int32(40 + 10*g.random())
			g.T_Damage(other, self, self, &self.Velocity, point, normal, damage, damage, 0, MOD_UNKNOWN)
		}
	}

	if !g.M_CheckBottom(self) {
		if self.Groundentity != nil {
			self.Monsterinfo.Nextframe = mutant_FRAME_attack02
			self.Touch = nil
		}
		return
	}

	self.Touch = nil
}

// C: game/m_mutant.c:345 mutant_jump_takeoff
func (g *Game) mutant_jump_takeoff(self *Edict) {
	s := monsterStatics[mutantStatics](g, "m_mutant")
	var forward Vec3

	g.gi.Sound(self, CHAN_VOICE, s.SoundSight, 1, ATTN_NORM, 0)
	shared.AngleVectors(self.S.Angles, &forward, nil, nil)
	self.S.Origin[2] += 1
	self.Velocity = shared.VectorScale(forward, 600)
	self.Velocity[2] = 250
	self.Groundentity = nil
	self.Monsterinfo.Aiflags |= AI_DUCKED
	self.Monsterinfo.AttackFinished = g.level.Time + 3
	self.Touch = mutant_jump_touch
}

// C: game/m_mutant.c:360 mutant_check_landing
func (g *Game) mutant_check_landing(self *Edict) {
	s := monsterStatics[mutantStatics](g, "m_mutant")
	if self.Groundentity != nil {
		g.gi.Sound(self, CHAN_WEAPON, s.SoundThud, 1, ATTN_NORM, 0)
		self.Monsterinfo.AttackFinished = 0
		self.Monsterinfo.Aiflags &^= AI_DUCKED
		return
	}

	if g.level.Time > self.Monsterinfo.AttackFinished {
		self.Monsterinfo.Nextframe = mutant_FRAME_attack02
	} else {
		self.Monsterinfo.Nextframe = mutant_FRAME_attack05
	}
}

// C: game/m_mutant.c:376 mutant_frames_jump
var mutant_frames_jump = []MFrame{
	{ai_charge, 0, nil},
	{ai_charge, 17, nil},
	{ai_charge, 15, mutant_jump_takeoff},
	{ai_charge, 15, nil},
	{ai_charge, 15, mutant_check_landing},
	{ai_charge, 0, nil},
	{ai_charge, 3, nil},
	{ai_charge, 0, nil},
}

// C: game/m_mutant.c:387 mutant_move_jump
var mutant_move_jump = defMMove("mutant_move_jump", mutant_FRAME_attack01, mutant_FRAME_attack08, mutant_frames_jump, mutant_run)

// C: game/m_mutant.c:389 mutant_jump
func (g *Game) mutant_jump(self *Edict) {
	self.Monsterinfo.Currentmove = mutant_move_jump
}

//
// CHECKATTACK
//

// C: game/m_mutant.c:399 mutant_check_melee
func (g *Game) mutant_check_melee(self *Edict) bool {
	if g.range_(self, self.Enemy) == RANGE_MELEE {
		return true
	}
	return false
}

// C: game/m_mutant.c:406 mutant_check_jump
func (g *Game) mutant_check_jump(self *Edict) bool {
	var v Vec3
	var distance float32

	if float64(self.AbsMin[2]) > (float64(self.Enemy.AbsMin[2]) + 0.75*float64(self.Enemy.Size[2])) {
		return false
	}

	if float64(self.AbsMax[2]) < (float64(self.Enemy.AbsMin[2]) + 0.25*float64(self.Enemy.Size[2])) {
		return false
	}

	v[0] = self.S.Origin[0] - self.Enemy.S.Origin[0]
	v[1] = self.S.Origin[1] - self.Enemy.S.Origin[1]
	v[2] = 0
	distance = shared.VectorLength(v)

	if distance < 100 {
		return false
	}
	if distance > 100 {
		if float64(g.random()) < 0.9 {
			return false
		}
	}

	return true
}

// C: game/m_mutant.c:433 mutant_checkattack
func (g *Game) mutant_checkattack(self *Edict) bool {
	if self.Enemy == nil || self.Enemy.Health <= 0 {
		return false
	}

	if g.mutant_check_melee(self) {
		self.Monsterinfo.AttackState = AS_MELEE
		return true
	}

	if g.mutant_check_jump(self) {
		self.Monsterinfo.AttackState = AS_MISSILE
		// FIXME play a jump sound here
		return true
	}

	return false
}

//
// PAIN
//

// C: game/m_mutant.c:459 mutant_frames_pain1
var mutant_frames_pain1 = []MFrame{
	{ai_move, 4, nil},
	{ai_move, -3, nil},
	{ai_move, -8, nil},
	{ai_move, 2, nil},
	{ai_move, 5, nil},
}

// C: game/m_mutant.c:467 mutant_move_pain1
var mutant_move_pain1 = defMMove("mutant_move_pain1", mutant_FRAME_pain101, mutant_FRAME_pain105, mutant_frames_pain1, mutant_run)

// C: game/m_mutant.c:469 mutant_frames_pain2
var mutant_frames_pain2 = []MFrame{
	{ai_move, -24, nil},
	{ai_move, 11, nil},
	{ai_move, 5, nil},
	{ai_move, -2, nil},
	{ai_move, 6, nil},
	{ai_move, 4, nil},
}

// C: game/m_mutant.c:478 mutant_move_pain2
var mutant_move_pain2 = defMMove("mutant_move_pain2", mutant_FRAME_pain201, mutant_FRAME_pain206, mutant_frames_pain2, mutant_run)

// C: game/m_mutant.c:480 mutant_frames_pain3
var mutant_frames_pain3 = []MFrame{
	{ai_move, -22, nil},
	{ai_move, 3, nil},
	{ai_move, 3, nil},
	{ai_move, 2, nil},
	{ai_move, 1, nil},
	{ai_move, 1, nil},
	{ai_move, 6, nil},
	{ai_move, 3, nil},
	{ai_move, 2, nil},
	{ai_move, 0, nil},
	{ai_move, 1, nil},
}

// C: game/m_mutant.c:494 mutant_move_pain3
var mutant_move_pain3 = defMMove("mutant_move_pain3", mutant_FRAME_pain301, mutant_FRAME_pain311, mutant_frames_pain3, mutant_run)

// C: game/m_mutant.c:496 mutant_pain
func (g *Game) mutant_pain(self *Edict, other *Edict, kick float32, damage int32) {
	s := monsterStatics[mutantStatics](g, "m_mutant")
	var r float32

	if self.Health < (self.MaxHealth / 2) {
		self.S.SkinNum = 1
	}

	if g.level.Time < self.PainDebounceTime {
		return
	}

	self.PainDebounceTime = g.level.Time + 3

	if g.skill.Value == 3 {
		return // no pain anims in nightmare
	}

	r = g.random()
	if float64(r) < 0.33 {
		g.gi.Sound(self, CHAN_VOICE, s.SoundPain1, 1, ATTN_NORM, 0)
		self.Monsterinfo.Currentmove = mutant_move_pain1
	} else if float64(r) < 0.66 {
		g.gi.Sound(self, CHAN_VOICE, s.SoundPain2, 1, ATTN_NORM, 0)
		self.Monsterinfo.Currentmove = mutant_move_pain2
	} else {
		g.gi.Sound(self, CHAN_VOICE, s.SoundPain1, 1, ATTN_NORM, 0)
		self.Monsterinfo.Currentmove = mutant_move_pain3
	}
}

//
// DEATH
//

// C: game/m_mutant.c:534 mutant_dead
func (g *Game) mutant_dead(self *Edict) {
	self.Mins = Vec3{-16, -16, -24}
	self.Maxs = Vec3{16, 16, -8}
	self.Movetype = MOVETYPE_TOSS
	self.SVFlags |= SVF_DEADMONSTER
	g.gi.LinkEntity(self)

	g.M_FlyCheck(self)
}

// C: game/m_mutant.c:545 mutant_frames_death1
var mutant_frames_death1 = []MFrame{
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
}

// C: game/m_mutant.c:557 mutant_move_death1
var mutant_move_death1 = defMMove("mutant_move_death1", mutant_FRAME_death101, mutant_FRAME_death109, mutant_frames_death1, mutant_dead)

// C: game/m_mutant.c:559 mutant_frames_death2
var mutant_frames_death2 = []MFrame{
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
}

// C: game/m_mutant.c:572 mutant_move_death2
var mutant_move_death2 = defMMove("mutant_move_death2", mutant_FRAME_death201, mutant_FRAME_death210, mutant_frames_death2, mutant_dead)

// C: game/m_mutant.c:574 mutant_die
func (g *Game) mutant_die(self *Edict, inflictor *Edict, attacker *Edict, damage int32, point Vec3) {
	s := monsterStatics[mutantStatics](g, "m_mutant")
	var n int32

	if self.Health <= self.GibHealth {
		g.gi.Sound(self, CHAN_VOICE, g.gi.SoundIndex("misc/udeath.wav"), 1, ATTN_NORM, 0)
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

	g.gi.Sound(self, CHAN_VOICE, s.SoundDeath, 1, ATTN_NORM, 0)
	self.Deadflag = DEAD_DEAD
	self.Takedamage = DAMAGE_YES
	self.S.SkinNum = 1

	if float64(g.random()) < 0.5 {
		self.Monsterinfo.Currentmove = mutant_move_death1
	} else {
		self.Monsterinfo.Currentmove = mutant_move_death2
	}
}

//
// SPAWN
//

// QUAKED monster_mutant (1 .5 0) (-32 -32 -24) (32 32 32) Ambush Trigger_Spawn Sight
//
// C: game/m_mutant.c:611 SP_monster_mutant
func (g *Game) SP_monster_mutant(self *Edict) {
	s := monsterStatics[mutantStatics](g, "m_mutant")
	if g.deathmatch.Value != 0 {
		g.G_FreeEdict(self)
		return
	}

	s.SoundSwing = g.gi.SoundIndex("mutant/mutatck1.wav")
	s.SoundHit = g.gi.SoundIndex("mutant/mutatck2.wav")
	s.SoundHit2 = g.gi.SoundIndex("mutant/mutatck3.wav")
	s.SoundDeath = g.gi.SoundIndex("mutant/mutdeth1.wav")
	s.SoundIdle = g.gi.SoundIndex("mutant/mutidle1.wav")
	s.SoundPain1 = g.gi.SoundIndex("mutant/mutpain1.wav")
	s.SoundPain2 = g.gi.SoundIndex("mutant/mutpain2.wav")
	s.SoundSight = g.gi.SoundIndex("mutant/mutsght1.wav")
	s.SoundSearch = g.gi.SoundIndex("mutant/mutsrch1.wav")
	s.SoundStep1 = g.gi.SoundIndex("mutant/step1.wav")
	s.SoundStep2 = g.gi.SoundIndex("mutant/step2.wav")
	s.SoundStep3 = g.gi.SoundIndex("mutant/step3.wav")
	s.SoundThud = g.gi.SoundIndex("mutant/thud1.wav")

	self.Movetype = MOVETYPE_STEP
	self.Solid = SOLID_BBOX
	self.S.ModelIndex = int32(g.gi.ModelIndex("models/monsters/mutant/tris.md2"))
	self.Mins = Vec3{-32, -32, -24}
	self.Maxs = Vec3{32, 32, 48}

	self.Health = 300
	self.GibHealth = -120
	self.Mass = 300

	self.Pain = mutant_pain
	self.Die = mutant_die

	self.Monsterinfo.Stand = mutant_stand
	self.Monsterinfo.Walk = mutant_walk
	self.Monsterinfo.Run = mutant_run
	self.Monsterinfo.Dodge = nil
	self.Monsterinfo.Attack = mutant_jump
	self.Monsterinfo.Melee = mutant_melee
	self.Monsterinfo.Sight = mutant_sight
	self.Monsterinfo.Search = mutant_search
	self.Monsterinfo.Idle = mutant_idle
	self.Monsterinfo.Checkattack = mutant_checkattack

	g.gi.LinkEntity(self)

	self.Monsterinfo.Currentmove = mutant_move_stand

	self.Monsterinfo.Scale = mutant_MODEL_SCALE
	g.walkmonster_start(self)
}
