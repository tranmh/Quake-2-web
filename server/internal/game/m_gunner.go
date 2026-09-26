package game

// Port of game/m_gunner.c.

import (
	. "quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/shared"
)

// statics of m_gunner.c
type gunnerStatics struct {
	SoundPain   int // sound_pain
	SoundPain2  int // sound_pain2
	SoundDeath  int // sound_death
	SoundIdle   int // sound_idle
	SoundOpen   int // sound_open
	SoundSearch int // sound_search
	SoundSight  int // sound_sight
}

var (
	gunner_idlesound    = defThink("gunner_idlesound")
	gunner_sight        = defBlocked("gunner_sight")
	gunner_search       = defThink("gunner_search")
	gunner_fidget       = defThink("gunner_fidget")
	gunner_stand        = defThink("gunner_stand")
	gunner_walk         = defThink("gunner_walk")
	gunner_run          = defThink("gunner_run")
	gunner_pain         = defPain("gunner_pain")
	gunner_dead         = defThink("gunner_dead")
	gunner_die          = defDie("gunner_die")
	gunner_duck_down    = defThink("gunner_duck_down")
	gunner_duck_hold    = defThink("gunner_duck_hold")
	gunner_duck_up      = defThink("gunner_duck_up")
	gunner_dodge        = defDodge("gunner_dodge")
	gunner_opengun      = defThink("gunner_opengun")
	GunnerFire          = defThink("GunnerFire")
	GunnerGrenade       = defThink("GunnerGrenade")
	gunner_attack       = defThink("gunner_attack")
	gunner_fire_chain   = defThink("gunner_fire_chain")
	gunner_refire_chain = defThink("gunner_refire_chain")
)

func init() {
	gunner_idlesound.bind((*Game).gunner_idlesound)
	gunner_sight.bind((*Game).gunner_sight)
	gunner_search.bind((*Game).gunner_search)
	gunner_fidget.bind((*Game).gunner_fidget)
	gunner_stand.bind((*Game).gunner_stand)
	gunner_walk.bind((*Game).gunner_walk)
	gunner_run.bind((*Game).gunner_run)
	gunner_pain.bind((*Game).gunner_pain)
	gunner_dead.bind((*Game).gunner_dead)
	gunner_die.bind((*Game).gunner_die)
	gunner_duck_down.bind((*Game).gunner_duck_down)
	gunner_duck_hold.bind((*Game).gunner_duck_hold)
	gunner_duck_up.bind((*Game).gunner_duck_up)
	gunner_dodge.bind((*Game).gunner_dodge)
	gunner_opengun.bind((*Game).gunner_opengun)
	GunnerFire.bind((*Game).GunnerFire)
	GunnerGrenade.bind((*Game).GunnerGrenade)
	gunner_attack.bind((*Game).gunner_attack)
	gunner_fire_chain.bind((*Game).gunner_fire_chain)
	gunner_refire_chain.bind((*Game).gunner_refire_chain)
}

func init() {
	RegisterMonsterStatics("m_gunner", func() any { return new(gunnerStatics) })
	RegisterSpawn("monster_gunner", (*Game).SP_monster_gunner)
}

// ==============================================================================
//
// # GUNNER
//
// ==============================================================================
//
// C: game/m_gunner.c:41 gunner_idlesound
func (g *Game) gunner_idlesound(self *Edict) {
	s := monsterStatics[gunnerStatics](g, "m_gunner")
	g.gi.Sound(self, CHAN_VOICE, s.SoundIdle, 1, ATTN_IDLE, 0)
}

// C: game/m_gunner.c:46 gunner_sight
func (g *Game) gunner_sight(self *Edict, other *Edict) {
	s := monsterStatics[gunnerStatics](g, "m_gunner")
	g.gi.Sound(self, CHAN_VOICE, s.SoundSight, 1, ATTN_NORM, 0)
}

// C: game/m_gunner.c:51 gunner_search
func (g *Game) gunner_search(self *Edict) {
	s := monsterStatics[gunnerStatics](g, "m_gunner")
	g.gi.Sound(self, CHAN_VOICE, s.SoundSearch, 1, ATTN_NORM, 0)
}

// C: game/m_gunner.c:66 gunner_frames_fidget
var gunner_frames_fidget = []MFrame{
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, gunner_idlesound},
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

// C: game/m_gunner.c:122 gunner_move_fidget
var gunner_move_fidget = defMMove("gunner_move_fidget", gunner_FRAME_stand31, gunner_FRAME_stand70, gunner_frames_fidget, gunner_stand)

// C: game/m_gunner.c:124 gunner_fidget
func (g *Game) gunner_fidget(self *Edict) {
	if self.Monsterinfo.Aiflags&AI_STAND_GROUND != 0 {
		return
	}
	if float64(g.random()) <= 0.05 {
		self.Monsterinfo.Currentmove = gunner_move_fidget
	}
}

// C: game/m_gunner.c:132 gunner_frames_stand
var gunner_frames_stand = []MFrame{
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, gunner_fidget},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, gunner_fidget},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, gunner_fidget},
}

// C: game/m_gunner.c:167 gunner_move_stand
var gunner_move_stand = defMMove("gunner_move_stand", gunner_FRAME_stand01, gunner_FRAME_stand30, gunner_frames_stand, nil)

// C: game/m_gunner.c:169 gunner_stand
func (g *Game) gunner_stand(self *Edict) {
	self.Monsterinfo.Currentmove = gunner_move_stand
}

// C: game/m_gunner.c:175 gunner_frames_walk
var gunner_frames_walk = []MFrame{
	{ai_walk, 0, nil},
	{ai_walk, 3, nil},
	{ai_walk, 4, nil},
	{ai_walk, 5, nil},
	{ai_walk, 7, nil},
	{ai_walk, 2, nil},
	{ai_walk, 6, nil},
	{ai_walk, 4, nil},
	{ai_walk, 2, nil},
	{ai_walk, 7, nil},
	{ai_walk, 5, nil},
	{ai_walk, 7, nil},
	{ai_walk, 4, nil},
}

// C: game/m_gunner.c:191 gunner_move_walk
var gunner_move_walk = defMMove("gunner_move_walk", gunner_FRAME_walk07, gunner_FRAME_walk19, gunner_frames_walk, nil)

// C: game/m_gunner.c:193 gunner_walk
func (g *Game) gunner_walk(self *Edict) {
	self.Monsterinfo.Currentmove = gunner_move_walk
}

// C: game/m_gunner.c:198 gunner_frames_run
var gunner_frames_run = []MFrame{
	{ai_run, 26, nil},
	{ai_run, 9, nil},
	{ai_run, 9, nil},
	{ai_run, 9, nil},
	{ai_run, 15, nil},
	{ai_run, 10, nil},
	{ai_run, 13, nil},
	{ai_run, 6, nil},
}

// C: game/m_gunner.c:210 gunner_move_run
var gunner_move_run = defMMove("gunner_move_run", gunner_FRAME_run01, gunner_FRAME_run08, gunner_frames_run, nil)

// C: game/m_gunner.c:212 gunner_run
func (g *Game) gunner_run(self *Edict) {
	if self.Monsterinfo.Aiflags&AI_STAND_GROUND != 0 {
		self.Monsterinfo.Currentmove = gunner_move_stand
	} else {
		self.Monsterinfo.Currentmove = gunner_move_run
	}
}

// C: game/m_gunner.c:220 gunner_frames_runandshoot
var gunner_frames_runandshoot = []MFrame{
	{ai_run, 32, nil},
	{ai_run, 15, nil},
	{ai_run, 10, nil},
	{ai_run, 18, nil},
	{ai_run, 8, nil},
	{ai_run, 20, nil},
}

// C: game/m_gunner.c:230 gunner_move_runandshoot
var gunner_move_runandshoot = defMMove("gunner_move_runandshoot", gunner_FRAME_runs01, gunner_FRAME_runs06, gunner_frames_runandshoot, nil)

// C: game/m_gunner.c:232 gunner_runandshoot
func (g *Game) gunner_runandshoot(self *Edict) {
	self.Monsterinfo.Currentmove = gunner_move_runandshoot
}

// C: game/m_gunner.c:237 gunner_frames_pain3
var gunner_frames_pain3 = []MFrame{
	{ai_move, -3, nil},
	{ai_move, 1, nil},
	{ai_move, 1, nil},
	{ai_move, 0, nil},
	{ai_move, 1, nil},
}

// C: game/m_gunner.c:245 gunner_move_pain3
var gunner_move_pain3 = defMMove("gunner_move_pain3", gunner_FRAME_pain301, gunner_FRAME_pain305, gunner_frames_pain3, gunner_run)

// C: game/m_gunner.c:247 gunner_frames_pain2
var gunner_frames_pain2 = []MFrame{
	{ai_move, -2, nil},
	{ai_move, 11, nil},
	{ai_move, 6, nil},
	{ai_move, 2, nil},
	{ai_move, -1, nil},
	{ai_move, -7, nil},
	{ai_move, -2, nil},
	{ai_move, -7, nil},
}

// C: game/m_gunner.c:258 gunner_move_pain2
var gunner_move_pain2 = defMMove("gunner_move_pain2", gunner_FRAME_pain201, gunner_FRAME_pain208, gunner_frames_pain2, gunner_run)

// C: game/m_gunner.c:260 gunner_frames_pain1
var gunner_frames_pain1 = []MFrame{
	{ai_move, 2, nil},
	{ai_move, 0, nil},
	{ai_move, -5, nil},
	{ai_move, 3, nil},
	{ai_move, -1, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 1, nil},
	{ai_move, 1, nil},
	{ai_move, 2, nil},
	{ai_move, 1, nil},
	{ai_move, 0, nil},
	{ai_move, -2, nil},
	{ai_move, -2, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
}

// C: game/m_gunner.c:281 gunner_move_pain1
var gunner_move_pain1 = defMMove("gunner_move_pain1", gunner_FRAME_pain101, gunner_FRAME_pain118, gunner_frames_pain1, gunner_run)

// C: game/m_gunner.c:283 gunner_pain
func (g *Game) gunner_pain(self *Edict, other *Edict, kick float32, damage int32) {
	s := monsterStatics[gunnerStatics](g, "m_gunner")
	if self.Health < (self.MaxHealth / 2) {
		self.S.SkinNum = 1
	}

	if g.level.Time < self.PainDebounceTime {
		return
	}

	self.PainDebounceTime = g.level.Time + 3

	if g.rng.Rand()&1 != 0 {
		g.gi.Sound(self, CHAN_VOICE, s.SoundPain, 1, ATTN_NORM, 0)
	} else {
		g.gi.Sound(self, CHAN_VOICE, s.SoundPain2, 1, ATTN_NORM, 0)
	}

	if g.skill.Value == 3 {
		return // no pain anims in nightmare
	}

	if damage <= 10 {
		self.Monsterinfo.Currentmove = gunner_move_pain3
	} else if damage <= 25 {
		self.Monsterinfo.Currentmove = gunner_move_pain2
	} else {
		self.Monsterinfo.Currentmove = gunner_move_pain1
	}
}

// C: game/m_gunner.c:309 gunner_dead
func (g *Game) gunner_dead(self *Edict) {
	self.Mins = Vec3{-16, -16, -24}
	self.Maxs = Vec3{16, 16, -8}
	self.Movetype = MOVETYPE_TOSS
	self.SVFlags |= SVF_DEADMONSTER
	self.Nextthink = 0
	g.gi.LinkEntity(self)
}

// C: game/m_gunner.c:319 gunner_frames_death
var gunner_frames_death = []MFrame{
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, -7, nil},
	{ai_move, -3, nil},
	{ai_move, -5, nil},
	{ai_move, 8, nil},
	{ai_move, 6, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
}

// C: game/m_gunner.c:333 gunner_move_death
var gunner_move_death = defMMove("gunner_move_death", gunner_FRAME_death01, gunner_FRAME_death11, gunner_frames_death, gunner_dead)

// C: game/m_gunner.c:335 gunner_die
func (g *Game) gunner_die(self *Edict, inflictor *Edict, attacker *Edict, damage int32, point Vec3) {
	s := monsterStatics[gunnerStatics](g, "m_gunner")
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
	g.gi.Sound(self, CHAN_VOICE, s.SoundDeath, 1, ATTN_NORM, 0)
	self.Deadflag = DEAD_DEAD
	self.Takedamage = DAMAGE_YES
	self.Monsterinfo.Currentmove = gunner_move_death
}

// C: game/m_gunner.c:363 gunner_duck_down
func (g *Game) gunner_duck_down(self *Edict) {
	if self.Monsterinfo.Aiflags&AI_DUCKED != 0 {
		return
	}
	self.Monsterinfo.Aiflags |= AI_DUCKED
	if g.skill.Value >= 2 {
		if g.random() > 0.5 {
			g.GunnerGrenade(self)
		}
	}

	self.Maxs[2] -= 32
	self.Takedamage = DAMAGE_YES
	self.Monsterinfo.Pausetime = g.level.Time + 1
	g.gi.LinkEntity(self)
}

// C: game/m_gunner.c:380 gunner_duck_hold
func (g *Game) gunner_duck_hold(self *Edict) {
	if g.level.Time >= self.Monsterinfo.Pausetime {
		self.Monsterinfo.Aiflags &^= AI_HOLD_FRAME
	} else {
		self.Monsterinfo.Aiflags |= AI_HOLD_FRAME
	}
}

// C: game/m_gunner.c:388 gunner_duck_up
func (g *Game) gunner_duck_up(self *Edict) {
	self.Monsterinfo.Aiflags &^= AI_DUCKED
	self.Maxs[2] += 32
	self.Takedamage = DAMAGE_AIM
	g.gi.LinkEntity(self)
}

// C: game/m_gunner.c:396 gunner_frames_duck
var gunner_frames_duck = []MFrame{
	{ai_move, 1, gunner_duck_down},
	{ai_move, 1, nil},
	{ai_move, 1, gunner_duck_hold},
	{ai_move, 0, nil},
	{ai_move, -1, nil},
	{ai_move, -1, nil},
	{ai_move, 0, gunner_duck_up},
	{ai_move, -1, nil},
}

// C: game/m_gunner.c:407 gunner_move_duck
var gunner_move_duck = defMMove("gunner_move_duck", gunner_FRAME_duck01, gunner_FRAME_duck08, gunner_frames_duck, gunner_run)

// C: game/m_gunner.c:409 gunner_dodge
func (g *Game) gunner_dodge(self *Edict, attacker *Edict, eta float32) {
	if g.random() > 0.25 {
		return
	}

	if self.Enemy == nil {
		self.Enemy = attacker
	}

	self.Monsterinfo.Currentmove = gunner_move_duck
}

// C: game/m_gunner.c:421 gunner_opengun
func (g *Game) gunner_opengun(self *Edict) {
	s := monsterStatics[gunnerStatics](g, "m_gunner")
	g.gi.Sound(self, CHAN_VOICE, s.SoundOpen, 1, ATTN_IDLE, 0)
}

// C: game/m_gunner.c:426 GunnerFire
func (g *Game) GunnerFire(self *Edict) {
	var start Vec3
	var forward, right Vec3
	var target Vec3
	var aim Vec3
	var flash_number int32

	flash_number = MZ2_GUNNER_MACHINEGUN_1 + (self.S.Frame - gunner_FRAME_attak216)

	shared.AngleVectors(self.S.Angles, &forward, &right, nil)
	start = G_ProjectSource(self.S.Origin, MonsterFlashOffset[flash_number], forward, right)

	// project enemy back a bit and target there
	target = self.Enemy.S.Origin
	target = shared.VectorMA(target, -0.2, self.Enemy.Velocity)
	target[2] += float32(self.Enemy.Viewheight)

	aim = shared.VectorSubtract(target, start)
	shared.VectorNormalize(&aim)
	g.monster_fire_bullet(self, start, aim, 3, 4, DEFAULT_BULLET_HSPREAD, DEFAULT_BULLET_VSPREAD, flash_number)
}

// C: game/m_gunner.c:449 GunnerGrenade
func (g *Game) GunnerGrenade(self *Edict) {
	var start Vec3
	var forward, right Vec3
	var aim Vec3
	var flash_number int32

	if self.S.Frame == gunner_FRAME_attak105 {
		flash_number = MZ2_GUNNER_GRENADE_1
	} else if self.S.Frame == gunner_FRAME_attak108 {
		flash_number = MZ2_GUNNER_GRENADE_2
	} else if self.S.Frame == gunner_FRAME_attak111 {
		flash_number = MZ2_GUNNER_GRENADE_3
	} else { // (self.S.Frame == gunner_FRAME_attak114)
		flash_number = MZ2_GUNNER_GRENADE_4
	}

	shared.AngleVectors(self.S.Angles, &forward, &right, nil)
	start = G_ProjectSource(self.S.Origin, MonsterFlashOffset[flash_number], forward, right)

	//FIXME : do a spread -225 -75 75 225 degrees around forward
	aim = forward

	g.monster_fire_grenade(self, start, aim, 50, 600, flash_number)
}

// C: game/m_gunner.c:474 gunner_frames_attack_chain
var gunner_frames_attack_chain = []MFrame{
	{ai_charge, 0, gunner_opengun},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
}

// C: game/m_gunner.c:494 gunner_move_attack_chain
var gunner_move_attack_chain = defMMove("gunner_move_attack_chain", gunner_FRAME_attak209, gunner_FRAME_attak215, gunner_frames_attack_chain, gunner_fire_chain)

// C: game/m_gunner.c:496 gunner_frames_fire_chain
var gunner_frames_fire_chain = []MFrame{
	{ai_charge, 0, GunnerFire},
	{ai_charge, 0, GunnerFire},
	{ai_charge, 0, GunnerFire},
	{ai_charge, 0, GunnerFire},
	{ai_charge, 0, GunnerFire},
	{ai_charge, 0, GunnerFire},
	{ai_charge, 0, GunnerFire},
	{ai_charge, 0, GunnerFire},
}

// C: game/m_gunner.c:507 gunner_move_fire_chain
var gunner_move_fire_chain = defMMove("gunner_move_fire_chain", gunner_FRAME_attak216, gunner_FRAME_attak223, gunner_frames_fire_chain, gunner_refire_chain)

// C: game/m_gunner.c:509 gunner_frames_endfire_chain
var gunner_frames_endfire_chain = []MFrame{
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
}

// C: game/m_gunner.c:519 gunner_move_endfire_chain
var gunner_move_endfire_chain = defMMove("gunner_move_endfire_chain", gunner_FRAME_attak224, gunner_FRAME_attak230, gunner_frames_endfire_chain, gunner_run)

// C: game/m_gunner.c:521 gunner_frames_attack_grenade
var gunner_frames_attack_grenade = []MFrame{
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, GunnerGrenade},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, GunnerGrenade},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, GunnerGrenade},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, GunnerGrenade},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
}

// C: game/m_gunner.c:545 gunner_move_attack_grenade
var gunner_move_attack_grenade = defMMove("gunner_move_attack_grenade", gunner_FRAME_attak101, gunner_FRAME_attak121, gunner_frames_attack_grenade, gunner_run)

// C: game/m_gunner.c:547 gunner_attack
func (g *Game) gunner_attack(self *Edict) {
	if g.range_(self, self.Enemy) == RANGE_MELEE {
		self.Monsterinfo.Currentmove = gunner_move_attack_chain
	} else {
		if g.random() <= 0.5 {
			self.Monsterinfo.Currentmove = gunner_move_attack_grenade
		} else {
			self.Monsterinfo.Currentmove = gunner_move_attack_chain
		}
	}
}

// C: game/m_gunner.c:562 gunner_fire_chain
func (g *Game) gunner_fire_chain(self *Edict) {
	self.Monsterinfo.Currentmove = gunner_move_fire_chain
}

// C: game/m_gunner.c:567 gunner_refire_chain
func (g *Game) gunner_refire_chain(self *Edict) {
	if self.Enemy.Health > 0 {
		if g.visible(self, self.Enemy) {
			if g.random() <= 0.5 {
				self.Monsterinfo.Currentmove = gunner_move_fire_chain
				return
			}
		}
	}
	self.Monsterinfo.Currentmove = gunner_move_endfire_chain
}

// QUAKED monster_gunner (1 .5 0) (-16 -16 -24) (16 16 32) Ambush Trigger_Spawn Sight
//
// C: game/m_gunner.c:581 SP_monster_gunner
func (g *Game) SP_monster_gunner(self *Edict) {
	s := monsterStatics[gunnerStatics](g, "m_gunner")
	if g.deathmatch.Value != 0 {
		g.G_FreeEdict(self)
		return
	}

	s.SoundDeath = g.gi.SoundIndex("gunner/death1.wav")
	s.SoundPain = g.gi.SoundIndex("gunner/gunpain2.wav")
	s.SoundPain2 = g.gi.SoundIndex("gunner/gunpain1.wav")
	s.SoundIdle = g.gi.SoundIndex("gunner/gunidle1.wav")
	s.SoundOpen = g.gi.SoundIndex("gunner/gunatck1.wav")
	s.SoundSearch = g.gi.SoundIndex("gunner/gunsrch1.wav")
	s.SoundSight = g.gi.SoundIndex("gunner/sight1.wav")

	g.gi.SoundIndex("gunner/gunatck2.wav")
	g.gi.SoundIndex("gunner/gunatck3.wav")

	self.Movetype = MOVETYPE_STEP
	self.Solid = SOLID_BBOX
	self.S.ModelIndex = int32(g.gi.ModelIndex("models/monsters/gunner/tris.md2"))
	self.Mins = Vec3{-16, -16, -24}
	self.Maxs = Vec3{16, 16, 32}

	self.Health = 175
	self.GibHealth = -70
	self.Mass = 200

	self.Pain = gunner_pain
	self.Die = gunner_die

	self.Monsterinfo.Stand = gunner_stand
	self.Monsterinfo.Walk = gunner_walk
	self.Monsterinfo.Run = gunner_run
	self.Monsterinfo.Dodge = gunner_dodge
	self.Monsterinfo.Attack = gunner_attack
	self.Monsterinfo.Melee = nil
	self.Monsterinfo.Sight = gunner_sight
	self.Monsterinfo.Search = gunner_search

	g.gi.LinkEntity(self)

	self.Monsterinfo.Currentmove = gunner_move_stand
	self.Monsterinfo.Scale = gunner_MODEL_SCALE

	g.walkmonster_start(self)
}
