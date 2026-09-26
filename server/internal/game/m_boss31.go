package game

// Port of game/m_boss31.c (jorg).

import (
	. "quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/shared"
)

// statics of m_boss31.c
type boss31Statics struct {
	SoundPain1     int // sound_pain1
	SoundPain2     int // sound_pain2
	SoundPain3     int // sound_pain3
	SoundIdle      int // sound_idle
	SoundDeath     int // sound_death
	SoundSearch1   int // sound_search1
	SoundSearch2   int // sound_search2
	SoundSearch3   int // sound_search3
	SoundAttack1   int // sound_attack1
	SoundAttack2   int // sound_attack2
	SoundFiregun   int // sound_firegun
	SoundStepLeft  int // sound_step_left
	SoundStepRight int // sound_step_right
	SoundDeathHit  int // sound_death_hit
}

var (
	jorg_search      = defThink("jorg_search")
	jorg_idle        = defThink("jorg_idle")
	jorg_step_left   = defThink("jorg_step_left")
	jorg_step_right  = defThink("jorg_step_right")
	jorg_stand       = defThink("jorg_stand")
	jorg_walk        = defThink("jorg_walk")
	jorg_run         = defThink("jorg_run")
	jorg_reattack1   = defThink("jorg_reattack1")
	jorg_attack1     = defThink("jorg_attack1")
	jorg_pain        = defPain("jorg_pain")
	jorgBFG          = defThink("jorgBFG")
	jorg_firebullet  = defThink("jorg_firebullet")
	jorg_attack      = defThink("jorg_attack")
	jorg_dead        = defThink("jorg_dead")
	jorg_die         = defDie("jorg_die")
	Jorg_CheckAttack = defCheckAttack("Jorg_CheckAttack")
)

func init() {
	jorg_search.bind((*Game).jorg_search)
	jorg_idle.bind((*Game).jorg_idle)
	jorg_step_left.bind((*Game).jorg_step_left)
	jorg_step_right.bind((*Game).jorg_step_right)
	jorg_stand.bind((*Game).jorg_stand)
	jorg_walk.bind((*Game).jorg_walk)
	jorg_run.bind((*Game).jorg_run)
	jorg_reattack1.bind((*Game).jorg_reattack1)
	jorg_attack1.bind((*Game).jorg_attack1)
	jorg_pain.bind((*Game).jorg_pain)
	jorgBFG.bind((*Game).jorgBFG)
	jorg_firebullet.bind((*Game).jorg_firebullet)
	jorg_attack.bind((*Game).jorg_attack)
	jorg_dead.bind((*Game).jorg_dead)
	jorg_die.bind((*Game).jorg_die)
	Jorg_CheckAttack.bind((*Game).Jorg_CheckAttack)
}

func init() {
	RegisterMonsterStatics("m_boss31", func() any { return new(boss31Statics) })
	RegisterSpawn("monster_jorg", (*Game).SP_monster_jorg)
}

func boss31S(g *Game) *boss31Statics { return monsterStatics[boss31Statics](g, "m_boss31") }

// C: game/m_boss31.c:53 jorg_search
func (g *Game) jorg_search(self *Edict) {
	s := boss31S(g)
	r := g.random()

	if float64(r) <= 0.3 {
		g.gi.Sound(self, CHAN_VOICE, s.SoundSearch1, 1, ATTN_NORM, 0)
	} else if float64(r) <= 0.6 {
		g.gi.Sound(self, CHAN_VOICE, s.SoundSearch2, 1, ATTN_NORM, 0)
	} else {
		g.gi.Sound(self, CHAN_VOICE, s.SoundSearch3, 1, ATTN_NORM, 0)
	}
}

// C: game/m_boss31.c:139 jorg_idle
func (g *Game) jorg_idle(self *Edict) {
	g.gi.Sound(self, CHAN_VOICE, boss31S(g).SoundIdle, 1, ATTN_NORM, 0)
}

// C: game/m_boss31.c:144 jorg_death_hit
func (g *Game) jorg_death_hit(self *Edict) {
	g.gi.Sound(self, CHAN_BODY, boss31S(g).SoundDeathHit, 1, ATTN_NORM, 0)
}

// C: game/m_boss31.c:150 jorg_step_left
func (g *Game) jorg_step_left(self *Edict) {
	g.gi.Sound(self, CHAN_BODY, boss31S(g).SoundStepLeft, 1, ATTN_NORM, 0)
}

// C: game/m_boss31.c:155 jorg_step_right
func (g *Game) jorg_step_right(self *Edict) {
	g.gi.Sound(self, CHAN_BODY, boss31S(g).SoundStepRight, 1, ATTN_NORM, 0)
}

// C: game/m_boss31.c:161 jorg_stand
func (g *Game) jorg_stand(self *Edict) {
	self.Monsterinfo.Currentmove = jorg_move_stand
}

// C: game/m_boss31.c:229 jorg_walk
func (g *Game) jorg_walk(self *Edict) {
	self.Monsterinfo.Currentmove = jorg_move_walk
}

// C: game/m_boss31.c:234 jorg_run
func (g *Game) jorg_run(self *Edict) {
	if self.Monsterinfo.Aiflags&AI_STAND_GROUND != 0 {
		self.Monsterinfo.Currentmove = jorg_move_stand
	} else {
		self.Monsterinfo.Currentmove = jorg_move_run
	}
}

// C: game/m_boss31.c:394 jorg_reattack1
func (g *Game) jorg_reattack1(self *Edict) {
	if g.visible(self, self.Enemy) {
		if float64(g.random()) < 0.9 {
			self.Monsterinfo.Currentmove = jorg_move_attack1
		} else {
			self.S.Sound = 0
			self.Monsterinfo.Currentmove = jorg_move_end_attack1
		}
	} else {
		self.S.Sound = 0
		self.Monsterinfo.Currentmove = jorg_move_end_attack1
	}
}

// C: game/m_boss31.c:411 jorg_attack1
func (g *Game) jorg_attack1(self *Edict) {
	self.Monsterinfo.Currentmove = jorg_move_attack1
}

// C: game/m_boss31.c:416 jorg_pain
func (g *Game) jorg_pain(self *Edict, other *Edict, kick float32, damage int32) {
	s := boss31S(g)
	if self.Health < (self.MaxHealth / 2) {
		self.S.SkinNum = 1
	}

	self.S.Sound = 0

	if g.level.Time < self.PainDebounceTime {
		return
	}

	// Lessen the chance of him going into his pain frames if he takes little damage
	if damage <= 40 {
		if float64(g.random()) <= 0.6 {
			return
		}
	}

	// If he's entering his attack1 or using attack1, lessen the chance of him
	// going into pain

	if (self.S.Frame >= boss31_FRAME_attak101) && (self.S.Frame <= boss31_FRAME_attak108) {
		if float64(g.random()) <= 0.005 {
			return
		}
	}

	if (self.S.Frame >= boss31_FRAME_attak109) && (self.S.Frame <= boss31_FRAME_attak114) {
		if float64(g.random()) <= 0.00005 {
			return
		}
	}

	if (self.S.Frame >= boss31_FRAME_attak201) && (self.S.Frame <= boss31_FRAME_attak208) {
		if float64(g.random()) <= 0.005 {
			return
		}
	}

	self.PainDebounceTime = g.level.Time + 3
	if g.skill.Value == 3 {
		return // no pain anims in nightmare
	}

	if damage <= 50 {
		g.gi.Sound(self, CHAN_VOICE, s.SoundPain1, 1, ATTN_NORM, 0)
		self.Monsterinfo.Currentmove = jorg_move_pain1
	} else if damage <= 100 {
		g.gi.Sound(self, CHAN_VOICE, s.SoundPain2, 1, ATTN_NORM, 0)
		self.Monsterinfo.Currentmove = jorg_move_pain2
	} else {
		if float64(g.random()) <= 0.3 {
			g.gi.Sound(self, CHAN_VOICE, s.SoundPain3, 1, ATTN_NORM, 0)
			self.Monsterinfo.Currentmove = jorg_move_pain3
		}
	}
}

// C: game/m_boss31.c:475 jorgBFG
func (g *Game) jorgBFG(self *Edict) {
	var forward, right Vec3

	shared.AngleVectors(self.S.Angles, &forward, &right, nil)
	start := G_ProjectSource(self.S.Origin, MonsterFlashOffset[MZ2_JORG_BFG_1], forward, right)

	vec := self.Enemy.S.Origin
	vec[2] += float32(self.Enemy.Viewheight)
	dir := shared.VectorSubtract(vec, start)
	shared.VectorNormalize(&dir)
	g.gi.Sound(self, CHAN_VOICE, boss31S(g).SoundAttack2, 1, ATTN_NORM, 0)
	g.monster_fire_bfg(self, start, dir, 50, 300, 100, 200, MZ2_JORG_BFG_1)
}

// C: game/m_boss31.c:501 jorg_firebullet_right
func (g *Game) jorg_firebullet_right(self *Edict) {
	var forward, right Vec3

	shared.AngleVectors(self.S.Angles, &forward, &right, nil)
	start := G_ProjectSource(self.S.Origin, MonsterFlashOffset[MZ2_JORG_MACHINEGUN_R1], forward, right)

	target := shared.VectorMA(self.Enemy.S.Origin, -0.2, self.Enemy.Velocity)
	target[2] += float32(self.Enemy.Viewheight)
	forward = shared.VectorSubtract(target, start)
	shared.VectorNormalize(&forward)

	g.monster_fire_bullet(self, start, forward, 6, 4, DEFAULT_BULLET_HSPREAD, DEFAULT_BULLET_VSPREAD, MZ2_JORG_MACHINEGUN_R1)
}

// C: game/m_boss31.c:517 jorg_firebullet_left
func (g *Game) jorg_firebullet_left(self *Edict) {
	var forward, right Vec3

	shared.AngleVectors(self.S.Angles, &forward, &right, nil)
	start := G_ProjectSource(self.S.Origin, MonsterFlashOffset[MZ2_JORG_MACHINEGUN_L1], forward, right)

	target := shared.VectorMA(self.Enemy.S.Origin, -0.2, self.Enemy.Velocity)
	target[2] += float32(self.Enemy.Viewheight)
	forward = shared.VectorSubtract(target, start)
	shared.VectorNormalize(&forward)

	g.monster_fire_bullet(self, start, forward, 6, 4, DEFAULT_BULLET_HSPREAD, DEFAULT_BULLET_VSPREAD, MZ2_JORG_MACHINEGUN_L1)
}

// C: game/m_boss31.c:533 jorg_firebullet
func (g *Game) jorg_firebullet(self *Edict) {
	g.jorg_firebullet_left(self)
	g.jorg_firebullet_right(self)
}

// C: game/m_boss31.c:539 jorg_attack
func (g *Game) jorg_attack(self *Edict) {
	s := boss31S(g)
	vec := shared.VectorSubtract(self.Enemy.S.Origin, self.S.Origin)
	_ = shared.VectorLength(vec) // range (unused)

	if float64(g.random()) <= 0.75 {
		g.gi.Sound(self, CHAN_VOICE, s.SoundAttack1, 1, ATTN_NORM, 0)
		self.S.Sound = int32(g.gi.SoundIndex("boss3/w_loop.wav"))
		self.Monsterinfo.Currentmove = jorg_move_start_attack1
	} else {
		g.gi.Sound(self, CHAN_VOICE, s.SoundAttack2, 1, ATTN_NORM, 0)
		self.Monsterinfo.Currentmove = jorg_move_attack2
	}
}

// jorg_dead: the C body is entirely under #if 0.
// C: game/m_boss31.c:560 jorg_dead
func (g *Game) jorg_dead(self *Edict) {
}

// C: game/m_boss31.c:589 jorg_die
func (g *Game) jorg_die(self *Edict, inflictor *Edict, attacker *Edict, damage int32, point Vec3) {
	g.gi.Sound(self, CHAN_VOICE, boss31S(g).SoundDeath, 1, ATTN_NORM, 0)
	self.Deadflag = DEAD_DEAD
	self.Takedamage = DAMAGE_NO
	self.S.Sound = 0
	self.Count = 0
	self.Monsterinfo.Currentmove = jorg_move_death
}

// C: game/m_boss31.c:599 Jorg_CheckAttack
func (g *Game) Jorg_CheckAttack(self *Edict) bool {
	var chance float32

	if self.Enemy.Health > 0 {
		// see if any entities are in the way of the shot
		spot1 := self.S.Origin
		spot1[2] += float32(self.Viewheight)
		spot2 := self.Enemy.S.Origin
		spot2[2] += float32(self.Enemy.Viewheight)

		tr := g.gi.Trace(&spot1, nil, nil, &spot2, self, CONTENTS_SOLID|CONTENTS_MONSTER|CONTENTS_SLIME|CONTENTS_LAVA)

		// do we have a clear shot?
		if tr.Ent != self.Enemy {
			return false
		}
	}

	_ = g.infront(self, self.Enemy) // enemy_infront (unused)
	enemy_range := g.range_(self, self.Enemy)
	temp := shared.VectorSubtract(self.Enemy.S.Origin, self.S.Origin)
	enemy_yaw := vectoyaw(temp)

	self.IdealYaw = enemy_yaw

	// melee attack
	if enemy_range == RANGE_MELEE {
		if self.Monsterinfo.Melee != nil {
			self.Monsterinfo.AttackState = AS_MELEE
		} else {
			self.Monsterinfo.AttackState = AS_MISSILE
		}
		return true
	}

	// missile attack
	if self.Monsterinfo.Attack == nil {
		return false
	}

	if g.level.Time < self.Monsterinfo.AttackFinished {
		return false
	}

	if enemy_range == RANGE_FAR {
		return false
	}

	if self.Monsterinfo.Aiflags&AI_STAND_GROUND != 0 {
		chance = 0.4
	} else if enemy_range == RANGE_MELEE {
		chance = 0.8
	} else if enemy_range == RANGE_NEAR {
		chance = 0.4
	} else if enemy_range == RANGE_MID {
		chance = 0.2
	} else {
		return false
	}

	if g.random() < chance {
		self.Monsterinfo.AttackState = AS_MISSILE
		self.Monsterinfo.AttackFinished = g.level.Time + 2*g.random()
		return true
	}

	if self.Flags&FL_FLY != 0 {
		if float64(g.random()) < 0.3 {
			self.Monsterinfo.AttackState = AS_SLIDING
		} else {
			self.Monsterinfo.AttackState = AS_STRAIGHT
		}
	}

	return false
}

// QUAKED monster_jorg (1 .5 0) (-80 -80 0) (90 90 140) Ambush Trigger_Spawn Sight
// C: game/m_boss31.c:696 SP_monster_jorg
func (g *Game) SP_monster_jorg(self *Edict) {
	if g.deathmatch.Value != 0 {
		g.G_FreeEdict(self)
		return
	}

	s := boss31S(g)
	s.SoundPain1 = g.gi.SoundIndex("boss3/bs3pain1.wav")
	s.SoundPain2 = g.gi.SoundIndex("boss3/bs3pain2.wav")
	s.SoundPain3 = g.gi.SoundIndex("boss3/bs3pain3.wav")
	s.SoundDeath = g.gi.SoundIndex("boss3/bs3deth1.wav")
	s.SoundAttack1 = g.gi.SoundIndex("boss3/bs3atck1.wav")
	s.SoundAttack2 = g.gi.SoundIndex("boss3/bs3atck2.wav")
	s.SoundSearch1 = g.gi.SoundIndex("boss3/bs3srch1.wav")
	s.SoundSearch2 = g.gi.SoundIndex("boss3/bs3srch2.wav")
	s.SoundSearch3 = g.gi.SoundIndex("boss3/bs3srch3.wav")
	s.SoundIdle = g.gi.SoundIndex("boss3/bs3idle1.wav")
	s.SoundStepLeft = g.gi.SoundIndex("boss3/step1.wav")
	s.SoundStepRight = g.gi.SoundIndex("boss3/step2.wav")
	s.SoundFiregun = g.gi.SoundIndex("boss3/xfire.wav")
	s.SoundDeathHit = g.gi.SoundIndex("boss3/d_hit.wav")

	g.MakronPrecache()

	self.Movetype = MOVETYPE_STEP
	self.Solid = SOLID_BBOX
	self.S.ModelIndex = int32(g.gi.ModelIndex("models/monsters/boss3/rider/tris.md2"))
	self.S.ModelIndex2 = int32(g.gi.ModelIndex("models/monsters/boss3/jorg/tris.md2"))
	self.Mins = Vec3{-80, -80, 0}
	self.Maxs = Vec3{80, 80, 140}

	self.Health = 3000
	self.GibHealth = -2000
	self.Mass = 1000

	self.Pain = jorg_pain
	self.Die = jorg_die
	self.Monsterinfo.Stand = jorg_stand
	self.Monsterinfo.Walk = jorg_walk
	self.Monsterinfo.Run = jorg_run
	self.Monsterinfo.Dodge = nil
	self.Monsterinfo.Attack = jorg_attack
	self.Monsterinfo.Search = jorg_search
	self.Monsterinfo.Melee = nil
	self.Monsterinfo.Sight = nil
	self.Monsterinfo.Checkattack = Jorg_CheckAttack
	g.gi.LinkEntity(self)

	self.Monsterinfo.Currentmove = jorg_move_stand
	self.Monsterinfo.Scale = boss31_MODEL_SCALE

	g.walkmonster_start(self)
}

// C: game/m_boss31.c:83 jorg_frames_stand
var jorg_frames_stand = []MFrame{
	{ai_stand, 0, jorg_idle},
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
	{ai_stand, 19, nil},
	{ai_stand, 11, jorg_step_left},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 6, nil},
	{ai_stand, 9, jorg_step_right},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, -2, nil},
	{ai_stand, -17, jorg_step_left},
	{ai_stand, 0, nil},
	{ai_stand, -12, nil},
	{ai_stand, -14, jorg_step_right},
}

// C: game/m_boss31.c:166 jorg_frames_run
var jorg_frames_run = []MFrame{
	{ai_run, 17, jorg_step_left},
	{ai_run, 0, nil},
	{ai_run, 0, nil},
	{ai_run, 0, nil},
	{ai_run, 12, nil},
	{ai_run, 8, nil},
	{ai_run, 10, nil},
	{ai_run, 33, jorg_step_right},
	{ai_run, 0, nil},
	{ai_run, 0, nil},
	{ai_run, 0, nil},
	{ai_run, 9, nil},
	{ai_run, 9, nil},
	{ai_run, 9, nil},
}

// C: game/m_boss31.c:189 jorg_frames_start_walk
var jorg_frames_start_walk = []MFrame{
	{ai_walk, 5, nil},
	{ai_walk, 6, nil},
	{ai_walk, 7, nil},
	{ai_walk, 9, nil},
	{ai_walk, 15, nil},
}

// C: game/m_boss31.c:199 jorg_frames_walk
var jorg_frames_walk = []MFrame{
	{ai_walk, 17, nil},
	{ai_walk, 0, nil},
	{ai_walk, 0, nil},
	{ai_walk, 0, nil},
	{ai_walk, 12, nil},
	{ai_walk, 8, nil},
	{ai_walk, 10, nil},
	{ai_walk, 33, nil},
	{ai_walk, 0, nil},
	{ai_walk, 0, nil},
	{ai_walk, 0, nil},
	{ai_walk, 9, nil},
	{ai_walk, 9, nil},
	{ai_walk, 9, nil},
}

// C: game/m_boss31.c:218 jorg_frames_end_walk
var jorg_frames_end_walk = []MFrame{
	{ai_walk, 11, nil},
	{ai_walk, 0, nil},
	{ai_walk, 0, nil},
	{ai_walk, 0, nil},
	{ai_walk, 8, nil},
	{ai_walk, -8, nil},
}

// C: game/m_boss31.c:242 jorg_frames_pain3
var jorg_frames_pain3 = []MFrame{
	{ai_move, -28, nil},
	{ai_move, -6, nil},
	{ai_move, -3, jorg_step_left},
	{ai_move, -9, nil},
	{ai_move, 0, jorg_step_right},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, -7, nil},
	{ai_move, 1, nil},
	{ai_move, -11, nil},
	{ai_move, -4, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 10, nil},
	{ai_move, 11, nil},
	{ai_move, 0, nil},
	{ai_move, 10, nil},
	{ai_move, 3, nil},
	{ai_move, 10, nil},
	{ai_move, 7, jorg_step_left},
	{ai_move, 17, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, jorg_step_right},
}

// C: game/m_boss31.c:272 jorg_frames_pain2
var jorg_frames_pain2 = []MFrame{
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
}

// C: game/m_boss31.c:280 jorg_frames_pain1
var jorg_frames_pain1 = []MFrame{
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
}

// C: game/m_boss31.c:288 jorg_frames_death1
var jorg_frames_death1 = []MFrame{
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
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, MakronToss},
	{ai_move, 0, BossExplode},
}

// C: game/m_boss31.c:343 jorg_frames_attack2
var jorg_frames_attack2 = []MFrame{
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, jorgBFG},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
}

// C: game/m_boss31.c:361 jorg_frames_start_attack1
var jorg_frames_start_attack1 = []MFrame{
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
}

// C: game/m_boss31.c:374 jorg_frames_attack1
var jorg_frames_attack1 = []MFrame{
	{ai_charge, 0, jorg_firebullet},
	{ai_charge, 0, jorg_firebullet},
	{ai_charge, 0, jorg_firebullet},
	{ai_charge, 0, jorg_firebullet},
	{ai_charge, 0, jorg_firebullet},
	{ai_charge, 0, jorg_firebullet},
}

// C: game/m_boss31.c:385 jorg_frames_end_attack1
var jorg_frames_end_attack1 = []MFrame{
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
}

// C: game/m_boss31.c:137 jorg_move_stand
var jorg_move_stand = defMMove("jorg_move_stand", boss31_FRAME_stand01, boss31_FRAME_stand51, jorg_frames_stand, nil)

// C: game/m_boss31.c:183 jorg_move_run
var jorg_move_run = defMMove("jorg_move_run", boss31_FRAME_walk06, boss31_FRAME_walk19, jorg_frames_run, nil)

// C: game/m_boss31.c:197 jorg_move_start_walk
var jorg_move_start_walk = defMMove("jorg_move_start_walk", boss31_FRAME_walk01, boss31_FRAME_walk05, jorg_frames_start_walk, nil)

// C: game/m_boss31.c:216 jorg_move_walk
var jorg_move_walk = defMMove("jorg_move_walk", boss31_FRAME_walk06, boss31_FRAME_walk19, jorg_frames_walk, nil)

// C: game/m_boss31.c:227 jorg_move_end_walk
var jorg_move_end_walk = defMMove("jorg_move_end_walk", boss31_FRAME_walk20, boss31_FRAME_walk25, jorg_frames_end_walk, nil)

// C: game/m_boss31.c:270 jorg_move_pain3
var jorg_move_pain3 = defMMove("jorg_move_pain3", boss31_FRAME_pain301, boss31_FRAME_pain325, jorg_frames_pain3, jorg_run)

// C: game/m_boss31.c:278 jorg_move_pain2
var jorg_move_pain2 = defMMove("jorg_move_pain2", boss31_FRAME_pain201, boss31_FRAME_pain203, jorg_frames_pain2, jorg_run)

// C: game/m_boss31.c:286 jorg_move_pain1
var jorg_move_pain1 = defMMove("jorg_move_pain1", boss31_FRAME_pain101, boss31_FRAME_pain103, jorg_frames_pain1, jorg_run)

// C: game/m_boss31.c:341 jorg_move_death
var jorg_move_death = defMMove("jorg_move_death", boss31_FRAME_death01, boss31_FRAME_death50, jorg_frames_death1, jorg_dead)

// C: game/m_boss31.c:359 jorg_move_attack2
var jorg_move_attack2 = defMMove("jorg_move_attack2", boss31_FRAME_attak201, boss31_FRAME_attak213, jorg_frames_attack2, jorg_run)

// C: game/m_boss31.c:372 jorg_move_start_attack1
var jorg_move_start_attack1 = defMMove("jorg_move_start_attack1", boss31_FRAME_attak101, boss31_FRAME_attak108, jorg_frames_start_attack1, jorg_attack1)

// C: game/m_boss31.c:383 jorg_move_attack1
var jorg_move_attack1 = defMMove("jorg_move_attack1", boss31_FRAME_attak109, boss31_FRAME_attak114, jorg_frames_attack1, jorg_reattack1)

// C: game/m_boss31.c:392 jorg_move_end_attack1
var jorg_move_end_attack1 = defMMove("jorg_move_end_attack1", boss31_FRAME_attak115, boss31_FRAME_attak118, jorg_frames_end_attack1, jorg_run)
