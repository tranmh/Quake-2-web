package game

// Port of game/m_chick.c.

import (
	. "quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/shared"
)

// statics of m_chick.c
type chickStatics struct {
	SoundMissilePrelaunch int // sound_missile_prelaunch
	SoundMissileLaunch    int // sound_missile_launch
	SoundMeleeSwing       int // sound_melee_swing
	SoundMeleeHit         int // sound_melee_hit
	SoundMissileReload    int // sound_missile_reload
	SoundDeath1           int // sound_death1
	SoundDeath2           int // sound_death2
	SoundFallDown         int // sound_fall_down
	SoundIdle1            int // sound_idle1
	SoundIdle2            int // sound_idle2
	SoundPain1            int // sound_pain1
	SoundPain2            int // sound_pain2
	SoundPain3            int // sound_pain3
	SoundSight            int // sound_sight
	SoundSearch           int // sound_search
}

var (
	ChickMoan        = defThink("ChickMoan")
	chick_fidget     = defThink("chick_fidget")
	chick_stand      = defThink("chick_stand")
	chick_walk       = defThink("chick_walk")
	chick_run        = defThink("chick_run")
	chick_pain       = defPain("chick_pain")
	chick_dead       = defThink("chick_dead")
	chick_die        = defDie("chick_die")
	chick_duck_down  = defThink("chick_duck_down")
	chick_duck_hold  = defThink("chick_duck_hold")
	chick_duck_up    = defThink("chick_duck_up")
	chick_dodge      = defDodge("chick_dodge")
	ChickSlash       = defThink("ChickSlash")
	ChickRocket      = defThink("ChickRocket")
	Chick_PreAttack1 = defThink("Chick_PreAttack1")
	ChickReload      = defThink("ChickReload")
	chick_rerocket   = defThink("chick_rerocket")
	chick_attack1    = defThink("chick_attack1")
	chick_reslash    = defThink("chick_reslash")
	chick_slash      = defThink("chick_slash")
	chick_melee      = defThink("chick_melee")
	chick_attack     = defThink("chick_attack")
	chick_sight      = defBlocked("chick_sight")
)

func init() {
	ChickMoan.bind((*Game).ChickMoan)
	chick_fidget.bind((*Game).chick_fidget)
	chick_stand.bind((*Game).chick_stand)
	chick_walk.bind((*Game).chick_walk)
	chick_run.bind((*Game).chick_run)
	chick_pain.bind((*Game).chick_pain)
	chick_dead.bind((*Game).chick_dead)
	chick_die.bind((*Game).chick_die)
	chick_duck_down.bind((*Game).chick_duck_down)
	chick_duck_hold.bind((*Game).chick_duck_hold)
	chick_duck_up.bind((*Game).chick_duck_up)
	chick_dodge.bind((*Game).chick_dodge)
	ChickSlash.bind((*Game).ChickSlash)
	ChickRocket.bind((*Game).ChickRocket)
	Chick_PreAttack1.bind((*Game).Chick_PreAttack1)
	ChickReload.bind((*Game).ChickReload)
	chick_rerocket.bind((*Game).chick_rerocket)
	chick_attack1.bind((*Game).chick_attack1)
	chick_reslash.bind((*Game).chick_reslash)
	chick_slash.bind((*Game).chick_slash)
	chick_melee.bind((*Game).chick_melee)
	chick_attack.bind((*Game).chick_attack)
	chick_sight.bind((*Game).chick_sight)
}

func init() {
	RegisterMonsterStatics("m_chick", func() any { return new(chickStatics) })
	RegisterSpawn("monster_chick", (*Game).SP_monster_chick)
}

// C: game/m_chick.c:56 ChickMoan
func (g *Game) ChickMoan(self *Edict) {
	s := monsterStatics[chickStatics](g, "m_chick")
	if g.random() < 0.5 {
		g.gi.Sound(self, CHAN_VOICE, s.SoundIdle1, 1, ATTN_IDLE, 0)
	} else {
		g.gi.Sound(self, CHAN_VOICE, s.SoundIdle2, 1, ATTN_IDLE, 0)
	}
}

// C: game/m_chick.c:99 chick_fidget
func (g *Game) chick_fidget(self *Edict) {
	if self.Monsterinfo.Aiflags&AI_STAND_GROUND != 0 {
		return
	}
	if float64(g.random()) <= 0.3 {
		self.Monsterinfo.Currentmove = chick_move_fidget
	}
}

// C: game/m_chick.c:143 chick_stand
func (g *Game) chick_stand(self *Edict) {
	self.Monsterinfo.Currentmove = chick_move_stand
}

// C: game/m_chick.c:196 chick_walk
func (g *Game) chick_walk(self *Edict) {
	self.Monsterinfo.Currentmove = chick_move_walk
}

// C: game/m_chick.c:201 chick_run
func (g *Game) chick_run(self *Edict) {
	if self.Monsterinfo.Aiflags&AI_STAND_GROUND != 0 {
		self.Monsterinfo.Currentmove = chick_move_stand
		return
	}

	if self.Monsterinfo.Currentmove == chick_move_walk ||
		self.Monsterinfo.Currentmove == chick_move_start_run {
		self.Monsterinfo.Currentmove = chick_move_run
	} else {
		self.Monsterinfo.Currentmove = chick_move_start_run
	}
}

// C: game/m_chick.c:266 chick_pain
func (g *Game) chick_pain(self *Edict, other *Edict, kick float32, damage int32) {
	s := monsterStatics[chickStatics](g, "m_chick")
	var r float32

	if self.Health < (self.MaxHealth / 2) {
		self.S.SkinNum = 1
	}

	if g.level.Time < self.PainDebounceTime {
		return
	}

	self.PainDebounceTime = g.level.Time + 3

	r = g.random()
	if float64(r) < 0.33 {
		g.gi.Sound(self, CHAN_VOICE, s.SoundPain1, 1, ATTN_NORM, 0)
	} else if float64(r) < 0.66 {
		g.gi.Sound(self, CHAN_VOICE, s.SoundPain2, 1, ATTN_NORM, 0)
	} else {
		g.gi.Sound(self, CHAN_VOICE, s.SoundPain3, 1, ATTN_NORM, 0)
	}

	if g.skill.Value == 3 {
		return // no pain anims in nightmare
	}

	if damage <= 10 {
		self.Monsterinfo.Currentmove = chick_move_pain1
	} else if damage <= 25 {
		self.Monsterinfo.Currentmove = chick_move_pain2
	} else {
		self.Monsterinfo.Currentmove = chick_move_pain3
	}
}

// C: game/m_chick.c:297 chick_dead
func (g *Game) chick_dead(self *Edict) {
	self.Mins = Vec3{-16, -16, 0}
	self.Maxs = Vec3{16, 16, 16}
	self.Movetype = MOVETYPE_TOSS
	self.SVFlags |= SVF_DEADMONSTER
	self.Nextthink = 0
	g.gi.LinkEntity(self)
}

// C: game/m_chick.c:353 chick_die
func (g *Game) chick_die(self *Edict, inflictor *Edict, attacker *Edict, damage int32, point Vec3) {
	s := monsterStatics[chickStatics](g, "m_chick")
	var n int32

	// check for gib
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

	// regular death
	self.Deadflag = DEAD_DEAD
	self.Takedamage = DAMAGE_YES

	n = g.rng.Rand() % 2
	if n == 0 {
		self.Monsterinfo.Currentmove = chick_move_death1
		g.gi.Sound(self, CHAN_VOICE, s.SoundDeath1, 1, ATTN_NORM, 0)
	} else {
		self.Monsterinfo.Currentmove = chick_move_death2
		g.gi.Sound(self, CHAN_VOICE, s.SoundDeath2, 1, ATTN_NORM, 0)
	}
}

// C: game/m_chick.c:391 chick_duck_down
func (g *Game) chick_duck_down(self *Edict) {
	if self.Monsterinfo.Aiflags&AI_DUCKED != 0 {
		return
	}
	self.Monsterinfo.Aiflags |= AI_DUCKED
	self.Maxs[2] -= 32
	self.Takedamage = DAMAGE_YES
	self.Monsterinfo.Pausetime = g.level.Time + 1
	g.gi.LinkEntity(self)
}

// C: game/m_chick.c:402 chick_duck_hold
func (g *Game) chick_duck_hold(self *Edict) {
	if g.level.Time >= self.Monsterinfo.Pausetime {
		self.Monsterinfo.Aiflags &^= AI_HOLD_FRAME
	} else {
		self.Monsterinfo.Aiflags |= AI_HOLD_FRAME
	}
}

// C: game/m_chick.c:410 chick_duck_up
func (g *Game) chick_duck_up(self *Edict) {
	self.Monsterinfo.Aiflags &^= AI_DUCKED
	self.Maxs[2] += 32
	self.Takedamage = DAMAGE_AIM
	g.gi.LinkEntity(self)
}

// C: game/m_chick.c:430 chick_dodge
func (g *Game) chick_dodge(self *Edict, attacker *Edict, eta float32) {
	if g.random() > 0.25 {
		return
	}

	if self.Enemy == nil {
		self.Enemy = attacker
	}

	self.Monsterinfo.Currentmove = chick_move_duck
}

// C: game/m_chick.c:441 ChickSlash
func (g *Game) ChickSlash(self *Edict) {
	s := monsterStatics[chickStatics](g, "m_chick")
	aim := Vec3{MELEE_DISTANCE, self.Mins[0], 10}
	g.gi.Sound(self, CHAN_WEAPON, s.SoundMeleeSwing, 1, ATTN_NORM, 0)
	g.fire_hit(self, aim, (10 + (g.rng.Rand() % 6)), 100)
}

// C: game/m_chick.c:451 ChickRocket
func (g *Game) ChickRocket(self *Edict) {
	var forward, right Vec3

	shared.AngleVectors(self.S.Angles, &forward, &right, nil)
	start := G_ProjectSource(self.S.Origin, MonsterFlashOffset[MZ2_CHICK_ROCKET_1], forward, right)

	vec := self.Enemy.S.Origin
	vec[2] += float32(self.Enemy.Viewheight)
	dir := shared.VectorSubtract(vec, start)
	shared.VectorNormalize(&dir)

	g.monster_fire_rocket(self, start, dir, 50, 500, MZ2_CHICK_ROCKET_1)
}

// C: game/m_chick.c:469 Chick_PreAttack1
func (g *Game) Chick_PreAttack1(self *Edict) {
	s := monsterStatics[chickStatics](g, "m_chick")
	g.gi.Sound(self, CHAN_VOICE, s.SoundMissilePrelaunch, 1, ATTN_NORM, 0)
}

// C: game/m_chick.c:474 ChickReload
func (g *Game) ChickReload(self *Edict) {
	s := monsterStatics[chickStatics](g, "m_chick")
	g.gi.Sound(self, CHAN_VOICE, s.SoundMissileReload, 1, ATTN_NORM, 0)
}

// C: game/m_chick.c:529 chick_rerocket
func (g *Game) chick_rerocket(self *Edict) {
	if self.Enemy.Health > 0 {
		if g.range_(self, self.Enemy) > RANGE_MELEE {
			if g.visible(self, self.Enemy) {
				if float64(g.random()) <= 0.6 {
					self.Monsterinfo.Currentmove = chick_move_attack1
					return
				}
			}
		}
	}
	self.Monsterinfo.Currentmove = chick_move_end_attack1
}

// C: game/m_chick.c:544 chick_attack1
func (g *Game) chick_attack1(self *Edict) {
	self.Monsterinfo.Currentmove = chick_move_attack1
}

// C: game/m_chick.c:573 chick_reslash
func (g *Game) chick_reslash(self *Edict) {
	if self.Enemy.Health > 0 {
		if g.range_(self, self.Enemy) == RANGE_MELEE {
			if float64(g.random()) <= 0.9 {
				self.Monsterinfo.Currentmove = chick_move_slash
				return
			} else {
				self.Monsterinfo.Currentmove = chick_move_end_slash
				return
			}
		}
	}
	self.Monsterinfo.Currentmove = chick_move_end_slash
}

// C: game/m_chick.c:592 chick_slash
func (g *Game) chick_slash(self *Edict) {
	self.Monsterinfo.Currentmove = chick_move_slash
}

// C: game/m_chick.c:608 chick_melee
func (g *Game) chick_melee(self *Edict) {
	self.Monsterinfo.Currentmove = chick_move_start_slash
}

// C: game/m_chick.c:614 chick_attack
func (g *Game) chick_attack(self *Edict) {
	self.Monsterinfo.Currentmove = chick_move_start_attack1
}

// C: game/m_chick.c:619 chick_sight
func (g *Game) chick_sight(self *Edict, other *Edict) {
	s := monsterStatics[chickStatics](g, "m_chick")
	g.gi.Sound(self, CHAN_VOICE, s.SoundSight, 1, ATTN_NORM, 0)
}

// QUAKED monster_chick (1 .5 0) (-16 -16 -24) (16 16 32) Ambush Trigger_Spawn Sight
//
// C: game/m_chick.c:626 SP_monster_chick
func (g *Game) SP_monster_chick(self *Edict) {
	s := monsterStatics[chickStatics](g, "m_chick")
	if g.deathmatch.Value != 0 {
		g.G_FreeEdict(self)
		return
	}

	s.SoundMissilePrelaunch = g.gi.SoundIndex("chick/chkatck1.wav")
	s.SoundMissileLaunch = g.gi.SoundIndex("chick/chkatck2.wav")
	s.SoundMeleeSwing = g.gi.SoundIndex("chick/chkatck3.wav")
	s.SoundMeleeHit = g.gi.SoundIndex("chick/chkatck4.wav")
	s.SoundMissileReload = g.gi.SoundIndex("chick/chkatck5.wav")
	s.SoundDeath1 = g.gi.SoundIndex("chick/chkdeth1.wav")
	s.SoundDeath2 = g.gi.SoundIndex("chick/chkdeth2.wav")
	s.SoundFallDown = g.gi.SoundIndex("chick/chkfall1.wav")
	s.SoundIdle1 = g.gi.SoundIndex("chick/chkidle1.wav")
	s.SoundIdle2 = g.gi.SoundIndex("chick/chkidle2.wav")
	s.SoundPain1 = g.gi.SoundIndex("chick/chkpain1.wav")
	s.SoundPain2 = g.gi.SoundIndex("chick/chkpain2.wav")
	s.SoundPain3 = g.gi.SoundIndex("chick/chkpain3.wav")
	s.SoundSight = g.gi.SoundIndex("chick/chksght1.wav")
	s.SoundSearch = g.gi.SoundIndex("chick/chksrch1.wav")

	self.Movetype = MOVETYPE_STEP
	self.Solid = SOLID_BBOX
	self.S.ModelIndex = int32(g.gi.ModelIndex("models/monsters/bitch/tris.md2"))
	self.Mins = Vec3{-16, -16, 0}
	self.Maxs = Vec3{16, 16, 56}

	self.Health = 175
	self.GibHealth = -70
	self.Mass = 200

	self.Pain = chick_pain
	self.Die = chick_die

	self.Monsterinfo.Stand = chick_stand
	self.Monsterinfo.Walk = chick_walk
	self.Monsterinfo.Run = chick_run
	self.Monsterinfo.Dodge = chick_dodge
	self.Monsterinfo.Attack = chick_attack
	self.Monsterinfo.Melee = chick_melee
	self.Monsterinfo.Sight = chick_sight

	g.gi.LinkEntity(self)

	self.Monsterinfo.Currentmove = chick_move_stand
	self.Monsterinfo.Scale = chick_MODEL_SCALE

	g.walkmonster_start(self)
}

// C: game/m_chick.c:64 chick_frames_fidget
var chick_frames_fidget = []MFrame{
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, ChickMoan},
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

// C: game/m_chick.c:107 chick_frames_stand
var chick_frames_stand = []MFrame{
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
	{ai_stand, 0, chick_fidget},
}

// C: game/m_chick.c:148 chick_frames_start_run
var chick_frames_start_run = []MFrame{
	{ai_run, 1, nil},
	{ai_run, 0, nil},
	{ai_run, 0, nil},
	{ai_run, -1, nil},
	{ai_run, -1, nil},
	{ai_run, 0, nil},
	{ai_run, 1, nil},
	{ai_run, 3, nil},
	{ai_run, 6, nil},
	{ai_run, 3, nil},
}

// C: game/m_chick.c:163 chick_frames_run
var chick_frames_run = []MFrame{
	{ai_run, 6, nil},
	{ai_run, 8, nil},
	{ai_run, 13, nil},
	{ai_run, 5, nil},
	{ai_run, 7, nil},
	{ai_run, 4, nil},
	{ai_run, 11, nil},
	{ai_run, 5, nil},
	{ai_run, 9, nil},
	{ai_run, 7, nil},
}

// C: game/m_chick.c:180 chick_frames_walk
var chick_frames_walk = []MFrame{
	{ai_walk, 6, nil},
	{ai_walk, 8, nil},
	{ai_walk, 13, nil},
	{ai_walk, 5, nil},
	{ai_walk, 7, nil},
	{ai_walk, 4, nil},
	{ai_walk, 11, nil},
	{ai_walk, 5, nil},
	{ai_walk, 9, nil},
	{ai_walk, 7, nil},
}

// C: game/m_chick.c:220 chick_frames_pain1
var chick_frames_pain1 = []MFrame{
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
}

// C: game/m_chick.c:230 chick_frames_pain2
var chick_frames_pain2 = []MFrame{
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
}

// C: game/m_chick.c:240 chick_frames_pain3
var chick_frames_pain3 = []MFrame{
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, -6, nil},
	{ai_move, 3, nil},
	{ai_move, 11, nil},
	{ai_move, 3, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 4, nil},
	{ai_move, 1, nil},
	{ai_move, 0, nil},
	{ai_move, -3, nil},
	{ai_move, -4, nil},
	{ai_move, 5, nil},
	{ai_move, 7, nil},
	{ai_move, -2, nil},
	{ai_move, 3, nil},
	{ai_move, -5, nil},
	{ai_move, -2, nil},
	{ai_move, -8, nil},
	{ai_move, 2, nil},
}

// C: game/m_chick.c:307 chick_frames_death2
var chick_frames_death2 = []MFrame{
	{ai_move, -6, nil},
	{ai_move, 0, nil},
	{ai_move, -1, nil},
	{ai_move, -5, nil},
	{ai_move, 0, nil},
	{ai_move, -1, nil},
	{ai_move, -2, nil},
	{ai_move, 1, nil},
	{ai_move, 10, nil},
	{ai_move, 2, nil},
	{ai_move, 3, nil},
	{ai_move, 1, nil},
	{ai_move, 2, nil},
	{ai_move, 0, nil},
	{ai_move, 3, nil},
	{ai_move, 3, nil},
	{ai_move, 1, nil},
	{ai_move, -3, nil},
	{ai_move, -5, nil},
	{ai_move, 4, nil},
	{ai_move, 15, nil},
	{ai_move, 14, nil},
	{ai_move, 1, nil},
}

// C: game/m_chick.c:335 chick_frames_death1
var chick_frames_death1 = []MFrame{
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, -7, nil},
	{ai_move, 4, nil},
	{ai_move, 11, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
}

// C: game/m_chick.c:418 chick_frames_duck
var chick_frames_duck = []MFrame{
	{ai_move, 0, chick_duck_down},
	{ai_move, 1, nil},
	{ai_move, 4, chick_duck_hold},
	{ai_move, -4, nil},
	{ai_move, -5, chick_duck_up},
	{ai_move, 3, nil},
	{ai_move, 1, nil},
}

// C: game/m_chick.c:480 chick_frames_start_attack1
var chick_frames_start_attack1 = []MFrame{
	{ai_charge, 0, Chick_PreAttack1},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 4, nil},
	{ai_charge, 0, nil},
	{ai_charge, -3, nil},
	{ai_charge, 3, nil},
	{ai_charge, 5, nil},
	{ai_charge, 7, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, chick_attack1},
}

// C: game/m_chick.c:499 chick_frames_attack1
var chick_frames_attack1 = []MFrame{
	{ai_charge, 19, ChickRocket},
	{ai_charge, -6, nil},
	{ai_charge, -5, nil},
	{ai_charge, -2, nil},
	{ai_charge, -7, nil},
	{ai_charge, 0, nil},
	{ai_charge, 1, nil},
	{ai_charge, 10, ChickReload},
	{ai_charge, 4, nil},
	{ai_charge, 5, nil},
	{ai_charge, 6, nil},
	{ai_charge, 6, nil},
	{ai_charge, 4, nil},
	{ai_charge, 3, chick_rerocket},
}

// C: game/m_chick.c:519 chick_frames_end_attack1
var chick_frames_end_attack1 = []MFrame{
	{ai_charge, -3, nil},
	{ai_charge, 0, nil},
	{ai_charge, -6, nil},
	{ai_charge, -4, nil},
	{ai_charge, -2, nil},
}

// C: game/m_chick.c:549 chick_frames_slash
var chick_frames_slash = []MFrame{
	{ai_charge, 1, nil},
	{ai_charge, 7, ChickSlash},
	{ai_charge, -7, nil},
	{ai_charge, 1, nil},
	{ai_charge, -1, nil},
	{ai_charge, 1, nil},
	{ai_charge, 0, nil},
	{ai_charge, 1, nil},
	{ai_charge, -2, chick_reslash},
}

// C: game/m_chick.c:563 chick_frames_end_slash
var chick_frames_end_slash = []MFrame{
	{ai_charge, -6, nil},
	{ai_charge, -1, nil},
	{ai_charge, -6, nil},
	{ai_charge, 0, nil},
}

// C: game/m_chick.c:598 chick_frames_start_slash
var chick_frames_start_slash = []MFrame{
	{ai_charge, 1, nil},
	{ai_charge, 8, nil},
	{ai_charge, 3, nil},
}

// C: game/m_chick.c:97 chick_move_fidget
var chick_move_fidget = defMMove("chick_move_fidget", chick_FRAME_stand201, chick_FRAME_stand230, chick_frames_fidget, chick_stand)

// C: game/m_chick.c:141 chick_move_stand
var chick_move_stand = defMMove("chick_move_stand", chick_FRAME_stand101, chick_FRAME_stand130, chick_frames_stand, nil)

// C: game/m_chick.c:161 chick_move_start_run
var chick_move_start_run = defMMove("chick_move_start_run", chick_FRAME_walk01, chick_FRAME_walk10, chick_frames_start_run, chick_run)

// C: game/m_chick.c:178 chick_move_run
var chick_move_run = defMMove("chick_move_run", chick_FRAME_walk11, chick_FRAME_walk20, chick_frames_run, nil)

// C: game/m_chick.c:194 chick_move_walk
var chick_move_walk = defMMove("chick_move_walk", chick_FRAME_walk11, chick_FRAME_walk20, chick_frames_walk, nil)

// C: game/m_chick.c:228 chick_move_pain1
var chick_move_pain1 = defMMove("chick_move_pain1", chick_FRAME_pain101, chick_FRAME_pain105, chick_frames_pain1, chick_run)

// C: game/m_chick.c:238 chick_move_pain2
var chick_move_pain2 = defMMove("chick_move_pain2", chick_FRAME_pain201, chick_FRAME_pain205, chick_frames_pain2, chick_run)

// C: game/m_chick.c:264 chick_move_pain3
var chick_move_pain3 = defMMove("chick_move_pain3", chick_FRAME_pain301, chick_FRAME_pain321, chick_frames_pain3, chick_run)

// C: game/m_chick.c:333 chick_move_death2
var chick_move_death2 = defMMove("chick_move_death2", chick_FRAME_death201, chick_FRAME_death223, chick_frames_death2, chick_dead)

// C: game/m_chick.c:351 chick_move_death1
var chick_move_death1 = defMMove("chick_move_death1", chick_FRAME_death101, chick_FRAME_death112, chick_frames_death1, chick_dead)

// C: game/m_chick.c:428 chick_move_duck
var chick_move_duck = defMMove("chick_move_duck", chick_FRAME_duck01, chick_FRAME_duck07, chick_frames_duck, chick_run)

// C: game/m_chick.c:496 chick_move_start_attack1
var chick_move_start_attack1 = defMMove("chick_move_start_attack1", chick_FRAME_attak101, chick_FRAME_attak113, chick_frames_start_attack1, nil)

// C: game/m_chick.c:517 chick_move_attack1
var chick_move_attack1 = defMMove("chick_move_attack1", chick_FRAME_attak114, chick_FRAME_attak127, chick_frames_attack1, nil)

// C: game/m_chick.c:527 chick_move_end_attack1
var chick_move_end_attack1 = defMMove("chick_move_end_attack1", chick_FRAME_attak128, chick_FRAME_attak132, chick_frames_end_attack1, chick_run)

// C: game/m_chick.c:561 chick_move_slash
var chick_move_slash = defMMove("chick_move_slash", chick_FRAME_attak204, chick_FRAME_attak212, chick_frames_slash, nil)

// C: game/m_chick.c:570 chick_move_end_slash
var chick_move_end_slash = defMMove("chick_move_end_slash", chick_FRAME_attak213, chick_FRAME_attak216, chick_frames_end_slash, chick_run)

// C: game/m_chick.c:604 chick_move_start_slash
var chick_move_start_slash = defMMove("chick_move_start_slash", chick_FRAME_attak201, chick_FRAME_attak203, chick_frames_start_slash, chick_slash)
