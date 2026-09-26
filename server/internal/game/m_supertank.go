package game

// Port of game/m_supertank.c.

import (
	. "quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/shared"
)

// statics of m_supertank.c
type supertankStatics struct {
	SoundPain1   int // sound_pain1
	SoundPain2   int // sound_pain2
	SoundPain3   int // sound_pain3
	SoundDeath   int // sound_death
	SoundSearch1 int // sound_search1
	SoundSearch2 int // sound_search2
	TreadSound   int // tread_sound
}

var (
	TreadSound          = defThink("TreadSound")
	supertank_search    = defThink("supertank_search")
	supertank_stand     = defThink("supertank_stand")
	supertank_walk      = defThink("supertank_walk")
	supertank_run       = defThink("supertank_run")
	supertank_reattack1 = defThink("supertank_reattack1")
	supertank_pain      = defPain("supertank_pain")
	supertankRocket     = defThink("supertankRocket")
	supertankMachineGun = defThink("supertankMachineGun")
	supertank_attack    = defThink("supertank_attack")
	supertank_dead      = defThink("supertank_dead")
	BossExplode         = defThink("BossExplode")
	supertank_die       = defDie("supertank_die")
)

func init() {
	TreadSound.bind((*Game).TreadSound)
	supertank_search.bind((*Game).supertank_search)
	supertank_stand.bind((*Game).supertank_stand)
	supertank_walk.bind((*Game).supertank_walk)
	supertank_run.bind((*Game).supertank_run)
	supertank_reattack1.bind((*Game).supertank_reattack1)
	supertank_pain.bind((*Game).supertank_pain)
	supertankRocket.bind((*Game).supertankRocket)
	supertankMachineGun.bind((*Game).supertankMachineGun)
	supertank_attack.bind((*Game).supertank_attack)
	supertank_dead.bind((*Game).supertank_dead)
	BossExplode.bind((*Game).BossExplode)
	supertank_die.bind((*Game).supertank_die)
}

func init() {
	RegisterMonsterStatics("m_supertank", func() any { return new(supertankStatics) })
	RegisterSpawn("monster_supertank", (*Game).SP_monster_supertank)
}

// C: game/m_supertank.c:68 supertank_frames_stand
var supertank_frames_stand = []MFrame{
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

// C: game/m_supertank.c:139 supertank_frames_run
var supertank_frames_run = []MFrame{
	{ai_run, 12, TreadSound},
	{ai_run, 12, nil},
	{ai_run, 12, nil},
	{ai_run, 12, nil},
	{ai_run, 12, nil},
	{ai_run, 12, nil},
	{ai_run, 12, nil},
	{ai_run, 12, nil},
	{ai_run, 12, nil},
	{ai_run, 12, nil},
	{ai_run, 12, nil},
	{ai_run, 12, nil},
	{ai_run, 12, nil},
	{ai_run, 12, nil},
	{ai_run, 12, nil},
	{ai_run, 12, nil},
	{ai_run, 12, nil},
	{ai_run, 12, nil},
}

// C: game/m_supertank.c:167 supertank_frames_forward
var supertank_frames_forward = []MFrame{
	{ai_walk, 4, TreadSound},
	{ai_walk, 4, nil},
	{ai_walk, 4, nil},
	{ai_walk, 4, nil},
	{ai_walk, 4, nil},
	{ai_walk, 4, nil},
	{ai_walk, 4, nil},
	{ai_walk, 4, nil},
	{ai_walk, 4, nil},
	{ai_walk, 4, nil},
	{ai_walk, 4, nil},
	{ai_walk, 4, nil},
	{ai_walk, 4, nil},
	{ai_walk, 4, nil},
	{ai_walk, 4, nil},
	{ai_walk, 4, nil},
	{ai_walk, 4, nil},
	{ai_walk, 4, nil},
}

// C: game/m_supertank.c:208 supertank_frames_turn_right
var supertank_frames_turn_right = []MFrame{
	{ai_move, 0, TreadSound},
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
}

// C: game/m_supertank.c:231 supertank_frames_turn_left
var supertank_frames_turn_left = []MFrame{
	{ai_move, 0, TreadSound},
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
}

// C: game/m_supertank.c:255 supertank_frames_pain3
var supertank_frames_pain3 = []MFrame{
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
}

// C: game/m_supertank.c:264 supertank_frames_pain2
var supertank_frames_pain2 = []MFrame{
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
}

// C: game/m_supertank.c:273 supertank_frames_pain1
var supertank_frames_pain1 = []MFrame{
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
}

// C: game/m_supertank.c:282 supertank_frames_death1
var supertank_frames_death1 = []MFrame{
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
	{ai_move, 0, BossExplode},
}

// C: game/m_supertank.c:311 supertank_frames_backward
var supertank_frames_backward = []MFrame{
	{ai_walk, 0, TreadSound},
	{ai_walk, 0, nil},
	{ai_walk, 0, nil},
	{ai_walk, 0, nil},
	{ai_walk, 0, nil},
	{ai_walk, 0, nil},
	{ai_walk, 0, nil},
	{ai_walk, 0, nil},
	{ai_walk, 0, nil},
	{ai_walk, 0, nil},
	{ai_walk, 0, nil},
	{ai_walk, 0, nil},
	{ai_walk, 0, nil},
	{ai_walk, 0, nil},
	{ai_walk, 0, nil},
	{ai_walk, 0, nil},
	{ai_walk, 0, nil},
	{ai_walk, 0, nil},
}

// C: game/m_supertank.c:334 supertank_frames_attack4
var supertank_frames_attack4 = []MFrame{
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
}

// C: game/m_supertank.c:345 supertank_frames_attack3
var supertank_frames_attack3 = []MFrame{
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
}

// C: game/m_supertank.c:377 supertank_frames_attack2
var supertank_frames_attack2 = []MFrame{
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, supertankRocket},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, supertankRocket},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, supertankRocket},
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
}

// C: game/m_supertank.c:409 supertank_frames_attack1
var supertank_frames_attack1 = []MFrame{
	{ai_charge, 0, supertankMachineGun},
	{ai_charge, 0, supertankMachineGun},
	{ai_charge, 0, supertankMachineGun},
	{ai_charge, 0, supertankMachineGun},
	{ai_charge, 0, supertankMachineGun},
	{ai_charge, 0, supertankMachineGun},
}

// C: game/m_supertank.c:421 supertank_frames_end_attack1
var supertank_frames_end_attack1 = []MFrame{
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
}

// C: game/m_supertank.c:131 supertank_move_stand
var supertank_move_stand = defMMove("supertank_move_stand", supertank_FRAME_stand_1, supertank_FRAME_stand_60, supertank_frames_stand, nil)

// C: game/m_supertank.c:160 supertank_move_run
var supertank_move_run = defMMove("supertank_move_run", supertank_FRAME_forwrd_1, supertank_FRAME_forwrd_18, supertank_frames_run, nil)

// C: game/m_supertank.c:188 supertank_move_forward
var supertank_move_forward = defMMove("supertank_move_forward", supertank_FRAME_forwrd_1, supertank_FRAME_forwrd_18, supertank_frames_forward, nil)

// C: game/m_supertank.c:229 supertank_move_turn_right
var supertank_move_turn_right = defMMove("supertank_move_turn_right", supertank_FRAME_right_1, supertank_FRAME_right_18, supertank_frames_turn_right, supertank_run)

// C: game/m_supertank.c:252 supertank_move_turn_left
var supertank_move_turn_left = defMMove("supertank_move_turn_left", supertank_FRAME_left_1, supertank_FRAME_left_18, supertank_frames_turn_left, supertank_run)

// C: game/m_supertank.c:262 supertank_move_pain3
var supertank_move_pain3 = defMMove("supertank_move_pain3", supertank_FRAME_pain3_9, supertank_FRAME_pain3_12, supertank_frames_pain3, supertank_run)

// C: game/m_supertank.c:271 supertank_move_pain2
var supertank_move_pain2 = defMMove("supertank_move_pain2", supertank_FRAME_pain2_5, supertank_FRAME_pain2_8, supertank_frames_pain2, supertank_run)

// C: game/m_supertank.c:280 supertank_move_pain1
var supertank_move_pain1 = defMMove("supertank_move_pain1", supertank_FRAME_pain1_1, supertank_FRAME_pain1_4, supertank_frames_pain1, supertank_run)

// C: game/m_supertank.c:309 supertank_move_death
var supertank_move_death = defMMove("supertank_move_death", supertank_FRAME_death_1, supertank_FRAME_death_24, supertank_frames_death1, supertank_dead)

// C: game/m_supertank.c:332 supertank_move_backward
var supertank_move_backward = defMMove("supertank_move_backward", supertank_FRAME_backwd_1, supertank_FRAME_backwd_18, supertank_frames_backward, nil)

// C: game/m_supertank.c:343 supertank_move_attack4
var supertank_move_attack4 = defMMove("supertank_move_attack4", supertank_FRAME_attak4_1, supertank_FRAME_attak4_6, supertank_frames_attack4, supertank_run)

// C: game/m_supertank.c:375 supertank_move_attack3
var supertank_move_attack3 = defMMove("supertank_move_attack3", supertank_FRAME_attak3_1, supertank_FRAME_attak3_27, supertank_frames_attack3, supertank_run)

// C: game/m_supertank.c:407 supertank_move_attack2
var supertank_move_attack2 = defMMove("supertank_move_attack2", supertank_FRAME_attak2_1, supertank_FRAME_attak2_27, supertank_frames_attack2, supertank_run)

// C: game/m_supertank.c:419 supertank_move_attack1
var supertank_move_attack1 = defMMove("supertank_move_attack1", supertank_FRAME_attak1_1, supertank_FRAME_attak1_6, supertank_frames_attack1, supertank_reattack1)

// C: game/m_supertank.c:438 supertank_move_end_attack1
var supertank_move_end_attack1 = defMMove("supertank_move_end_attack1", supertank_FRAME_attak1_7, supertank_FRAME_attak1_20, supertank_frames_end_attack1, supertank_run)

// C: game/m_supertank.c:44 TreadSound
func (g *Game) TreadSound(self *Edict) {
	s := monsterStatics[supertankStatics](g, "m_supertank")
	g.gi.Sound(self, CHAN_VOICE, s.TreadSound, 1, ATTN_NORM, 0)
}

// C: game/m_supertank.c:49 supertank_search
func (g *Game) supertank_search(self *Edict) {
	s := monsterStatics[supertankStatics](g, "m_supertank")
	if g.random() < 0.5 {
		g.gi.Sound(self, CHAN_VOICE, s.SoundSearch1, 1, ATTN_NORM, 0)
	} else {
		g.gi.Sound(self, CHAN_VOICE, s.SoundSearch2, 1, ATTN_NORM, 0)
	}
}

// C: game/m_supertank.c:133 supertank_stand
func (g *Game) supertank_stand(self *Edict) {
	self.Monsterinfo.Currentmove = supertank_move_stand
}

// C: game/m_supertank.c:190 supertank_forward
func (g *Game) supertank_forward(self *Edict) {
	self.Monsterinfo.Currentmove = supertank_move_forward
}

// C: game/m_supertank.c:195 supertank_walk
func (g *Game) supertank_walk(self *Edict) {
	self.Monsterinfo.Currentmove = supertank_move_forward
}

// C: game/m_supertank.c:200 supertank_run
func (g *Game) supertank_run(self *Edict) {
	if self.Monsterinfo.Aiflags&AI_STAND_GROUND != 0 {
		self.Monsterinfo.Currentmove = supertank_move_stand
	} else {
		self.Monsterinfo.Currentmove = supertank_move_run
	}
}

// C: game/m_supertank.c:441 supertank_reattack1
func (g *Game) supertank_reattack1(self *Edict) {
	if g.visible(self, self.Enemy) {
		if float64(g.random()) < 0.9 {
			self.Monsterinfo.Currentmove = supertank_move_attack1
		} else {
			self.Monsterinfo.Currentmove = supertank_move_end_attack1
		}
	} else {
		self.Monsterinfo.Currentmove = supertank_move_end_attack1
	}
}

// C: game/m_supertank.c:452 supertank_pain
func (g *Game) supertank_pain(self *Edict, other *Edict, kick float32, damage int32) {
	s := monsterStatics[supertankStatics](g, "m_supertank")

	if self.Health < (self.MaxHealth / 2) {
		self.S.SkinNum = 1
	}

	if g.level.Time < self.PainDebounceTime {
		return
	}

	// Lessen the chance of him going into his pain frames
	if damage <= 25 {
		if float64(g.random()) < 0.2 {
			return
		}
	}

	// Don't go into pain if he's firing his rockets
	if g.skill.Value >= 2 {
		if (self.S.Frame >= supertank_FRAME_attak2_1) && (self.S.Frame <= supertank_FRAME_attak2_14) {
			return
		}
	}

	self.PainDebounceTime = g.level.Time + 3

	if g.skill.Value == 3 {
		return // no pain anims in nightmare
	}

	if damage <= 10 {
		g.gi.Sound(self, CHAN_VOICE, s.SoundPain1, 1, ATTN_NORM, 0)
		self.Monsterinfo.Currentmove = supertank_move_pain1
	} else if damage <= 25 {
		g.gi.Sound(self, CHAN_VOICE, s.SoundPain3, 1, ATTN_NORM, 0)
		self.Monsterinfo.Currentmove = supertank_move_pain2
	} else {
		g.gi.Sound(self, CHAN_VOICE, s.SoundPain2, 1, ATTN_NORM, 0)
		self.Monsterinfo.Currentmove = supertank_move_pain3
	}
}

// C: game/m_supertank.c:494 supertankRocket
func (g *Game) supertankRocket(self *Edict) {
	var forward, right Vec3
	var flash_number int32

	if self.S.Frame == supertank_FRAME_attak2_8 {
		flash_number = MZ2_SUPERTANK_ROCKET_1
	} else if self.S.Frame == supertank_FRAME_attak2_11 {
		flash_number = MZ2_SUPERTANK_ROCKET_2
	} else { // (self->s.frame == FRAME_attak2_14)
		flash_number = MZ2_SUPERTANK_ROCKET_3
	}

	shared.AngleVectors(self.S.Angles, &forward, &right, nil)
	start := G_ProjectSource(self.S.Origin, MonsterFlashOffset[flash_number], forward, right)

	vec := self.Enemy.S.Origin
	vec[2] += float32(self.Enemy.Viewheight)
	dir := shared.VectorSubtract(vec, start)
	shared.VectorNormalize(&dir)

	g.monster_fire_rocket(self, start, dir, 50, 500, flash_number)
}

// C: game/m_supertank.c:520 supertankMachineGun
func (g *Game) supertankMachineGun(self *Edict) {
	var dir, forward, right Vec3

	flash_number := MZ2_SUPERTANK_MACHINEGUN_1 + (self.S.Frame - supertank_FRAME_attak1_1)

	//FIXME!!!
	dir[0] = 0
	dir[1] = self.S.Angles[1]
	dir[2] = 0

	shared.AngleVectors(dir, &forward, &right, nil)
	start := G_ProjectSource(self.S.Origin, MonsterFlashOffset[flash_number], forward, right)

	if self.Enemy != nil {
		vec := self.Enemy.S.Origin
		vec = shared.VectorMA(vec, 0, self.Enemy.Velocity)
		vec[2] += float32(self.Enemy.Viewheight)
		forward = shared.VectorSubtract(vec, start)
		shared.VectorNormalize(&forward)
	}

	g.monster_fire_bullet(self, start, forward, 6, 4, DEFAULT_BULLET_HSPREAD, DEFAULT_BULLET_VSPREAD, flash_number)
}

// C: game/m_supertank.c:551 supertank_attack
func (g *Game) supertank_attack(self *Edict) {
	vec := shared.VectorSubtract(self.Enemy.S.Origin, self.S.Origin)
	rng := shared.VectorLength(vec)

	// Attack 1 == Chaingun
	// Attack 2 == Rocket Launcher

	if rng <= 160 {
		self.Monsterinfo.Currentmove = supertank_move_attack1
	} else { // fire rockets more often at distance
		if float64(g.random()) < 0.3 {
			self.Monsterinfo.Currentmove = supertank_move_attack1
		} else {
			self.Monsterinfo.Currentmove = supertank_move_attack2
		}
	}
}

//
// death
//

// C: game/m_supertank.c:583 supertank_dead
func (g *Game) supertank_dead(self *Edict) {
	self.Mins = Vec3{-60, -60, 0}
	self.Maxs = Vec3{60, 60, 72}
	self.Movetype = MOVETYPE_TOSS
	self.SVFlags |= SVF_DEADMONSTER
	self.Nextthink = 0
	g.gi.LinkEntity(self)
}

// C: game/m_supertank.c:594 BossExplode
func (g *Game) BossExplode(self *Edict) {
	var n int32

	self.Think = BossExplode
	org := self.S.Origin
	org[2] += float32(24 + (g.rng.Rand() & 15))
	c := self.Count
	self.Count++
	switch c {
	case 0:
		org[0] -= 24
		org[1] -= 24
	case 1:
		org[0] += 24
		org[1] += 24
	case 2:
		org[0] += 24
		org[1] -= 24
	case 3:
		org[0] -= 24
		org[1] += 24
	case 4:
		org[0] -= 48
		org[1] -= 48
	case 5:
		org[0] += 48
		org[1] += 48
	case 6:
		org[0] -= 48
		org[1] += 48
	case 7:
		org[0] += 48
		org[1] -= 48
	case 8:
		self.S.Sound = 0
		for n = 0; n < 4; n++ {
			g.ThrowGib(self, "models/objects/gibs/sm_meat/tris.md2", 500, GIB_ORGANIC)
		}
		for n = 0; n < 8; n++ {
			g.ThrowGib(self, "models/objects/gibs/sm_metal/tris.md2", 500, GIB_METALLIC)
		}
		g.ThrowGib(self, "models/objects/gibs/chest/tris.md2", 500, GIB_ORGANIC)
		g.ThrowHead(self, "models/objects/gibs/gear/tris.md2", 500, GIB_METALLIC)
		self.Deadflag = DEAD_DEAD
		return
	}

	g.gi.WriteByteC(svc_temp_entity)
	g.gi.WriteByteC(TE_EXPLOSION1)
	g.gi.WritePosition(&org)
	g.gi.Multicast(&self.S.Origin, MULTICAST_PVS)

	self.Nextthink = float32(float64(g.level.Time) + 0.1)
}

// C: game/m_supertank.c:657 supertank_die
func (g *Game) supertank_die(self *Edict, inflictor *Edict, attacker *Edict, damage int32, point Vec3) {
	s := monsterStatics[supertankStatics](g, "m_supertank")
	g.gi.Sound(self, CHAN_VOICE, s.SoundDeath, 1, ATTN_NORM, 0)
	self.Deadflag = DEAD_DEAD
	self.Takedamage = DAMAGE_NO
	self.Count = 0
	self.Monsterinfo.Currentmove = supertank_move_death
}

//
// monster_supertank
//

// QUAKED monster_supertank (1 .5 0) (-64 -64 0) (64 64 72) Ambush Trigger_Spawn Sight
//
// C: game/m_supertank.c:672 SP_monster_supertank
func (g *Game) SP_monster_supertank(self *Edict) {
	s := monsterStatics[supertankStatics](g, "m_supertank")
	if g.deathmatch.Value != 0 {
		g.G_FreeEdict(self)
		return
	}

	s.SoundPain1 = g.gi.SoundIndex("bosstank/btkpain1.wav")
	s.SoundPain2 = g.gi.SoundIndex("bosstank/btkpain2.wav")
	s.SoundPain3 = g.gi.SoundIndex("bosstank/btkpain3.wav")
	s.SoundDeath = g.gi.SoundIndex("bosstank/btkdeth1.wav")
	s.SoundSearch1 = g.gi.SoundIndex("bosstank/btkunqv1.wav")
	s.SoundSearch2 = g.gi.SoundIndex("bosstank/btkunqv2.wav")

	//	self->s.sound = gi.soundindex ("bosstank/btkengn1.wav");
	s.TreadSound = g.gi.SoundIndex("bosstank/btkengn1.wav")

	self.Movetype = MOVETYPE_STEP
	self.Solid = SOLID_BBOX
	self.S.ModelIndex = int32(g.gi.ModelIndex("models/monsters/boss1/tris.md2"))
	self.Mins = Vec3{-64, -64, 0}
	self.Maxs = Vec3{64, 64, 112}

	self.Health = 1500
	self.GibHealth = -500
	self.Mass = 800

	self.Pain = supertank_pain
	self.Die = supertank_die
	self.Monsterinfo.Stand = supertank_stand
	self.Monsterinfo.Walk = supertank_walk
	self.Monsterinfo.Run = supertank_run
	self.Monsterinfo.Dodge = nil
	self.Monsterinfo.Attack = supertank_attack
	self.Monsterinfo.Search = supertank_search
	self.Monsterinfo.Melee = nil
	self.Monsterinfo.Sight = nil

	g.gi.LinkEntity(self)

	self.Monsterinfo.Currentmove = supertank_move_stand
	self.Monsterinfo.Scale = supertank_MODEL_SCALE

	g.walkmonster_start(self)
}
