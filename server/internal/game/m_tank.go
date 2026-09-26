package game

// Port of game/m_tank.c.

import (
	. "quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/shared"
)

// statics of m_tank.c
type tankStatics struct {
	SoundThud   int // sound_thud
	SoundPain   int // sound_pain
	SoundIdle   int // sound_idle
	SoundDie    int // sound_die
	SoundStep   int // sound_step
	SoundSight  int // sound_sight
	SoundWindup int // sound_windup
	SoundStrike int // sound_strike
}

var (
	tank_sight            = defBlocked("tank_sight")
	tank_footstep         = defThink("tank_footstep")
	tank_thud             = defThink("tank_thud")
	tank_windup           = defThink("tank_windup")
	tank_idle             = defThink("tank_idle")
	tank_stand            = defThink("tank_stand")
	tank_walk             = defThink("tank_walk")
	tank_run              = defThink("tank_run")
	tank_pain             = defPain("tank_pain")
	TankBlaster           = defThink("TankBlaster")
	TankStrike            = defThink("TankStrike")
	TankRocket            = defThink("TankRocket")
	TankMachineGun        = defThink("TankMachineGun")
	tank_reattack_blaster = defThink("tank_reattack_blaster")
	tank_poststrike       = defThink("tank_poststrike")
	tank_refire_rocket    = defThink("tank_refire_rocket")
	tank_doattack_rocket  = defThink("tank_doattack_rocket")
	tank_attack           = defThink("tank_attack")
	tank_dead             = defThink("tank_dead")
	tank_die              = defDie("tank_die")
)

func init() {
	tank_sight.bind((*Game).tank_sight)
	tank_footstep.bind((*Game).tank_footstep)
	tank_thud.bind((*Game).tank_thud)
	tank_windup.bind((*Game).tank_windup)
	tank_idle.bind((*Game).tank_idle)
	tank_stand.bind((*Game).tank_stand)
	tank_walk.bind((*Game).tank_walk)
	tank_run.bind((*Game).tank_run)
	tank_pain.bind((*Game).tank_pain)
	TankBlaster.bind((*Game).TankBlaster)
	TankStrike.bind((*Game).TankStrike)
	TankRocket.bind((*Game).TankRocket)
	TankMachineGun.bind((*Game).TankMachineGun)
	tank_reattack_blaster.bind((*Game).tank_reattack_blaster)
	tank_poststrike.bind((*Game).tank_poststrike)
	tank_refire_rocket.bind((*Game).tank_refire_rocket)
	tank_doattack_rocket.bind((*Game).tank_doattack_rocket)
	tank_attack.bind((*Game).tank_attack)
	tank_dead.bind((*Game).tank_dead)
	tank_die.bind((*Game).tank_die)
}

func init() {
	RegisterMonsterStatics("m_tank", func() any { return new(tankStatics) })
	RegisterSpawn("monster_tank", (*Game).SP_monster_tank)
	RegisterSpawn("monster_tank_commander", (*Game).SP_monster_tank)
}

// ==============================================================================
//
// # TANK
//
// ==============================================================================
//
// misc
//
// C: game/m_tank.c:49 tank_sight
func (g *Game) tank_sight(self *Edict, other *Edict) {
	s := monsterStatics[tankStatics](g, "m_tank")
	g.gi.Sound(self, CHAN_VOICE, s.SoundSight, 1, ATTN_NORM, 0)
}

// C: game/m_tank.c:55 tank_footstep
func (g *Game) tank_footstep(self *Edict) {
	s := monsterStatics[tankStatics](g, "m_tank")
	g.gi.Sound(self, CHAN_BODY, s.SoundStep, 1, ATTN_NORM, 0)
}

// C: game/m_tank.c:60 tank_thud
func (g *Game) tank_thud(self *Edict) {
	s := monsterStatics[tankStatics](g, "m_tank")
	g.gi.Sound(self, CHAN_BODY, s.SoundThud, 1, ATTN_NORM, 0)
}

// C: game/m_tank.c:65 tank_windup
func (g *Game) tank_windup(self *Edict) {
	s := monsterStatics[tankStatics](g, "m_tank")
	g.gi.Sound(self, CHAN_WEAPON, s.SoundWindup, 1, ATTN_NORM, 0)
}

// C: game/m_tank.c:70 tank_idle
func (g *Game) tank_idle(self *Edict) {
	s := monsterStatics[tankStatics](g, "m_tank")
	g.gi.Sound(self, CHAN_VOICE, s.SoundIdle, 1, ATTN_IDLE, 0)
}

// stand
//
// C: game/m_tank.c:80 tank_frames_stand
var tank_frames_stand = []MFrame{
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

// C: game/m_tank.c:113 tank_move_stand
var tank_move_stand = defMMove("tank_move_stand", tank_FRAME_stand01, tank_FRAME_stand30, tank_frames_stand, nil)

// C: game/m_tank.c:115 tank_stand
func (g *Game) tank_stand(self *Edict) {
	self.Monsterinfo.Currentmove = tank_move_stand
}

// walk
//
// C: game/m_tank.c:127 tank_frames_start_walk
var tank_frames_start_walk = []MFrame{
	{ai_walk, 0, nil},
	{ai_walk, 6, nil},
	{ai_walk, 6, nil},
	{ai_walk, 11, tank_footstep},
}

// C: game/m_tank.c:134 tank_move_start_walk
var tank_move_start_walk = defMMove("tank_move_start_walk", tank_FRAME_walk01, tank_FRAME_walk04, tank_frames_start_walk, tank_walk)

// C: game/m_tank.c:136 tank_frames_walk
var tank_frames_walk = []MFrame{
	{ai_walk, 4, nil},
	{ai_walk, 5, nil},
	{ai_walk, 3, nil},
	{ai_walk, 2, nil},
	{ai_walk, 5, nil},
	{ai_walk, 5, nil},
	{ai_walk, 4, nil},
	{ai_walk, 4, tank_footstep},
	{ai_walk, 3, nil},
	{ai_walk, 5, nil},
	{ai_walk, 4, nil},
	{ai_walk, 5, nil},
	{ai_walk, 7, nil},
	{ai_walk, 7, nil},
	{ai_walk, 6, nil},
	{ai_walk, 6, tank_footstep},
}

// C: game/m_tank.c:155 tank_move_walk
var tank_move_walk = defMMove("tank_move_walk", tank_FRAME_walk05, tank_FRAME_walk20, tank_frames_walk, nil)

// C: game/m_tank.c:157 tank_frames_stop_walk
var tank_frames_stop_walk = []MFrame{
	{ai_walk, 3, nil},
	{ai_walk, 3, nil},
	{ai_walk, 2, nil},
	{ai_walk, 2, nil},
	{ai_walk, 4, tank_footstep},
}

// C: game/m_tank.c:165 tank_move_stop_walk
var tank_move_stop_walk = defMMove("tank_move_stop_walk", tank_FRAME_walk21, tank_FRAME_walk25, tank_frames_stop_walk, tank_stand)

// C: game/m_tank.c:167 tank_walk
func (g *Game) tank_walk(self *Edict) {
	self.Monsterinfo.Currentmove = tank_move_walk
}

// run
//
// C: game/m_tank.c:179 tank_frames_start_run
var tank_frames_start_run = []MFrame{
	{ai_run, 0, nil},
	{ai_run, 6, nil},
	{ai_run, 6, nil},
	{ai_run, 11, tank_footstep},
}

// C: game/m_tank.c:186 tank_move_start_run
var tank_move_start_run = defMMove("tank_move_start_run", tank_FRAME_walk01, tank_FRAME_walk04, tank_frames_start_run, tank_run)

// C: game/m_tank.c:188 tank_frames_run
var tank_frames_run = []MFrame{
	{ai_run, 4, nil},
	{ai_run, 5, nil},
	{ai_run, 3, nil},
	{ai_run, 2, nil},
	{ai_run, 5, nil},
	{ai_run, 5, nil},
	{ai_run, 4, nil},
	{ai_run, 4, tank_footstep},
	{ai_run, 3, nil},
	{ai_run, 5, nil},
	{ai_run, 4, nil},
	{ai_run, 5, nil},
	{ai_run, 7, nil},
	{ai_run, 7, nil},
	{ai_run, 6, nil},
	{ai_run, 6, tank_footstep},
}

// C: game/m_tank.c:207 tank_move_run
var tank_move_run = defMMove("tank_move_run", tank_FRAME_walk05, tank_FRAME_walk20, tank_frames_run, nil)

// C: game/m_tank.c:209 tank_frames_stop_run
var tank_frames_stop_run = []MFrame{
	{ai_run, 3, nil},
	{ai_run, 3, nil},
	{ai_run, 2, nil},
	{ai_run, 2, nil},
	{ai_run, 4, tank_footstep},
}

// C: game/m_tank.c:217 tank_move_stop_run
var tank_move_stop_run = defMMove("tank_move_stop_run", tank_FRAME_walk21, tank_FRAME_walk25, tank_frames_stop_run, tank_walk)

// C: game/m_tank.c:219 tank_run
func (g *Game) tank_run(self *Edict) {
	if self.Enemy != nil && self.Enemy.Client != nil {
		self.Monsterinfo.Aiflags |= AI_BRUTAL
	} else {
		self.Monsterinfo.Aiflags &^= AI_BRUTAL
	}

	if self.Monsterinfo.Aiflags&AI_STAND_GROUND != 0 {
		self.Monsterinfo.Currentmove = tank_move_stand
		return
	}

	if self.Monsterinfo.Currentmove == tank_move_walk ||
		self.Monsterinfo.Currentmove == tank_move_start_run {
		self.Monsterinfo.Currentmove = tank_move_run
	} else {
		self.Monsterinfo.Currentmove = tank_move_start_run
	}
}

// pain
//
// C: game/m_tank.c:247 tank_frames_pain1
var tank_frames_pain1 = []MFrame{
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
}

// C: game/m_tank.c:254 tank_move_pain1
var tank_move_pain1 = defMMove("tank_move_pain1", tank_FRAME_pain101, tank_FRAME_pain104, tank_frames_pain1, tank_run)

// C: game/m_tank.c:256 tank_frames_pain2
var tank_frames_pain2 = []MFrame{
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
}

// C: game/m_tank.c:264 tank_move_pain2
var tank_move_pain2 = defMMove("tank_move_pain2", tank_FRAME_pain201, tank_FRAME_pain205, tank_frames_pain2, tank_run)

// C: game/m_tank.c:266 tank_frames_pain3
var tank_frames_pain3 = []MFrame{
	{ai_move, -7, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 2, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 3, nil},
	{ai_move, 0, nil},
	{ai_move, 2, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, tank_footstep},
}

// C: game/m_tank.c:285 tank_move_pain3
var tank_move_pain3 = defMMove("tank_move_pain3", tank_FRAME_pain301, tank_FRAME_pain316, tank_frames_pain3, tank_run)

// C: game/m_tank.c:288 tank_pain
func (g *Game) tank_pain(self *Edict, other *Edict, kick float32, damage int32) {
	s := monsterStatics[tankStatics](g, "m_tank")
	if self.Health < (self.MaxHealth / 2) {
		self.S.SkinNum |= 1
	}

	if damage <= 10 {
		return
	}

	if g.level.Time < self.PainDebounceTime {
		return
	}

	if damage <= 30 {
		if float64(g.random()) > 0.2 {
			return
		}
	}

	// If hard or nightmare, don't go into pain while attacking
	if g.skill.Value >= 2 {
		if (self.S.Frame >= tank_FRAME_attak301) && (self.S.Frame <= tank_FRAME_attak330) {
			return
		}
		if (self.S.Frame >= tank_FRAME_attak101) && (self.S.Frame <= tank_FRAME_attak116) {
			return
		}
	}

	self.PainDebounceTime = g.level.Time + 3
	g.gi.Sound(self, CHAN_VOICE, s.SoundPain, 1, ATTN_NORM, 0)

	if g.skill.Value == 3 {
		return // no pain anims in nightmare
	}

	if damage <= 30 {
		self.Monsterinfo.Currentmove = tank_move_pain1
	} else if damage <= 60 {
		self.Monsterinfo.Currentmove = tank_move_pain2
	} else {
		self.Monsterinfo.Currentmove = tank_move_pain3
	}
}

// attacks
//
// C: game/m_tank.c:331 TankBlaster
func (g *Game) TankBlaster(self *Edict) {
	var forward, right Vec3
	var start Vec3
	var end Vec3
	var dir Vec3
	var flash_number int32

	if self.S.Frame == tank_FRAME_attak110 {
		flash_number = MZ2_TANK_BLASTER_1
	} else if self.S.Frame == tank_FRAME_attak113 {
		flash_number = MZ2_TANK_BLASTER_2
	} else { // (self->s.frame == FRAME_attak116)
		flash_number = MZ2_TANK_BLASTER_3
	}

	shared.AngleVectors(self.S.Angles, &forward, &right, nil)
	start = G_ProjectSource(self.S.Origin, MonsterFlashOffset[flash_number], forward, right)

	end = self.Enemy.S.Origin
	end[2] += float32(self.Enemy.Viewheight)
	dir = shared.VectorSubtract(end, start)

	g.monster_fire_blaster(self, start, dir, 30, 800, flash_number, EF_BLASTER)
}

// C: game/m_tank.c:356 TankStrike
func (g *Game) TankStrike(self *Edict) {
	s := monsterStatics[tankStatics](g, "m_tank")
	g.gi.Sound(self, CHAN_WEAPON, s.SoundStrike, 1, ATTN_NORM, 0)
}

// C: game/m_tank.c:361 TankRocket
func (g *Game) TankRocket(self *Edict) {
	var forward, right Vec3
	var start Vec3
	var dir Vec3
	var vec Vec3
	var flash_number int32

	if self.S.Frame == tank_FRAME_attak324 {
		flash_number = MZ2_TANK_ROCKET_1
	} else if self.S.Frame == tank_FRAME_attak327 {
		flash_number = MZ2_TANK_ROCKET_2
	} else { // (self->s.frame == FRAME_attak330)
		flash_number = MZ2_TANK_ROCKET_3
	}

	shared.AngleVectors(self.S.Angles, &forward, &right, nil)
	start = G_ProjectSource(self.S.Origin, MonsterFlashOffset[flash_number], forward, right)

	vec = self.Enemy.S.Origin
	vec[2] += float32(self.Enemy.Viewheight)
	dir = shared.VectorSubtract(vec, start)
	shared.VectorNormalize(&dir)

	g.monster_fire_rocket(self, start, dir, 50, 550, flash_number)
}

// C: game/m_tank.c:387 TankMachineGun
func (g *Game) TankMachineGun(self *Edict) {
	var dir Vec3
	var vec Vec3
	var start Vec3
	var forward, right Vec3
	var flash_number int32

	flash_number = MZ2_TANK_MACHINEGUN_1 + (self.S.Frame - tank_FRAME_attak406)

	shared.AngleVectors(self.S.Angles, &forward, &right, nil)
	start = G_ProjectSource(self.S.Origin, MonsterFlashOffset[flash_number], forward, right)

	if self.Enemy != nil {
		vec = self.Enemy.S.Origin
		vec[2] += float32(self.Enemy.Viewheight)
		vec = shared.VectorSubtract(vec, start)
		vec = vectoangles(vec)
		dir[0] = vec[0]
	} else {
		dir[0] = 0
	}
	if self.S.Frame <= tank_FRAME_attak415 {
		dir[1] = self.S.Angles[1] - float32(8*(self.S.Frame-tank_FRAME_attak411))
	} else {
		dir[1] = self.S.Angles[1] + float32(8*(self.S.Frame-tank_FRAME_attak419))
	}
	dir[2] = 0

	shared.AngleVectors(dir, &forward, nil, nil)

	g.monster_fire_bullet(self, start, forward, 20, 4, DEFAULT_BULLET_HSPREAD, DEFAULT_BULLET_VSPREAD, flash_number)
}

// C: game/m_tank.c:424 tank_frames_attack_blast
var tank_frames_attack_blast = []MFrame{
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, -1, nil},
	{ai_charge, -2, nil},
	{ai_charge, -1, nil},
	{ai_charge, -1, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, TankBlaster},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, TankBlaster},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, TankBlaster},
}

// C: game/m_tank.c:443 tank_move_attack_blast
var tank_move_attack_blast = defMMove("tank_move_attack_blast", tank_FRAME_attak101, tank_FRAME_attak116, tank_frames_attack_blast, tank_reattack_blaster)

// C: game/m_tank.c:445 tank_frames_reattack_blast
var tank_frames_reattack_blast = []MFrame{
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, TankBlaster},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, TankBlaster},
}

// C: game/m_tank.c:454 tank_move_reattack_blast
var tank_move_reattack_blast = defMMove("tank_move_reattack_blast", tank_FRAME_attak111, tank_FRAME_attak116, tank_frames_reattack_blast, tank_reattack_blaster)

// C: game/m_tank.c:456 tank_frames_attack_post_blast
var tank_frames_attack_post_blast = []MFrame{
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 2, nil},
	{ai_move, 3, nil},
	{ai_move, 2, nil},
	{ai_move, -2, tank_footstep},
}

// C: game/m_tank.c:465 tank_move_attack_post_blast
var tank_move_attack_post_blast = defMMove("tank_move_attack_post_blast", tank_FRAME_attak117, tank_FRAME_attak122, tank_frames_attack_post_blast, tank_run)

// C: game/m_tank.c:467 tank_reattack_blaster
func (g *Game) tank_reattack_blaster(self *Edict) {
	if g.skill.Value >= 2 {
		if g.visible(self, self.Enemy) {
			if self.Enemy.Health > 0 {
				if float64(g.random()) <= 0.6 {
					self.Monsterinfo.Currentmove = tank_move_reattack_blast
					return
				}
			}
		}
	}
	self.Monsterinfo.Currentmove = tank_move_attack_post_blast
}

// C: game/m_tank.c:481 tank_poststrike
func (g *Game) tank_poststrike(self *Edict) {
	self.Enemy = nil
	g.tank_run(self)
}

// C: game/m_tank.c:487 tank_frames_attack_strike
var tank_frames_attack_strike = []MFrame{
	{ai_move, 3, nil},
	{ai_move, 2, nil},
	{ai_move, 2, nil},
	{ai_move, 1, nil},
	{ai_move, 6, nil},
	{ai_move, 7, nil},
	{ai_move, 9, tank_footstep},
	{ai_move, 2, nil},
	{ai_move, 1, nil},
	{ai_move, 2, nil},
	{ai_move, 2, tank_footstep},
	{ai_move, 2, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, -2, nil},
	{ai_move, -2, nil},
	{ai_move, 0, tank_windup},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, TankStrike},
	{ai_move, 0, nil},
	{ai_move, -1, nil},
	{ai_move, -1, nil},
	{ai_move, -1, nil},
	{ai_move, -1, nil},
	{ai_move, -1, nil},
	{ai_move, -3, nil},
	{ai_move, -10, nil},
	{ai_move, -10, nil},
	{ai_move, -2, nil},
	{ai_move, -3, nil},
	{ai_move, -2, tank_footstep},
}

// C: game/m_tank.c:528 tank_move_attack_strike
var tank_move_attack_strike = defMMove("tank_move_attack_strike", tank_FRAME_attak201, tank_FRAME_attak238, tank_frames_attack_strike, tank_poststrike)

// C: game/m_tank.c:530 tank_frames_attack_pre_rocket
var tank_frames_attack_pre_rocket = []MFrame{
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 1, nil},
	{ai_charge, 2, nil},
	{ai_charge, 7, nil},
	{ai_charge, 7, nil},
	{ai_charge, 7, tank_footstep},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, -3, nil},
}

// C: game/m_tank.c:556 tank_move_attack_pre_rocket
var tank_move_attack_pre_rocket = defMMove("tank_move_attack_pre_rocket", tank_FRAME_attak301, tank_FRAME_attak321, tank_frames_attack_pre_rocket, tank_doattack_rocket)

// C: game/m_tank.c:558 tank_frames_attack_fire_rocket
var tank_frames_attack_fire_rocket = []MFrame{
	{ai_charge, -3, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, TankRocket},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, TankRocket},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, -1, TankRocket},
}

// C: game/m_tank.c:570 tank_move_attack_fire_rocket
var tank_move_attack_fire_rocket = defMMove("tank_move_attack_fire_rocket", tank_FRAME_attak322, tank_FRAME_attak330, tank_frames_attack_fire_rocket, tank_refire_rocket)

// C: game/m_tank.c:572 tank_frames_attack_post_rocket
var tank_frames_attack_post_rocket = []MFrame{
	{ai_charge, 0, nil},
	{ai_charge, -1, nil},
	{ai_charge, -1, nil},
	{ai_charge, 0, nil},
	{ai_charge, 2, nil},
	{ai_charge, 3, nil},
	{ai_charge, 4, nil},
	{ai_charge, 2, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, -9, nil},
	{ai_charge, -8, nil},
	{ai_charge, -7, nil},
	{ai_charge, -1, nil},
	{ai_charge, -1, tank_footstep},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
}

// C: game/m_tank.c:600 tank_move_attack_post_rocket
var tank_move_attack_post_rocket = defMMove("tank_move_attack_post_rocket", tank_FRAME_attak331, tank_FRAME_attak353, tank_frames_attack_post_rocket, tank_run)

// C: game/m_tank.c:602 tank_frames_attack_chain
var tank_frames_attack_chain = []MFrame{
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{nil, 0, TankMachineGun},
	{nil, 0, TankMachineGun},
	{nil, 0, TankMachineGun},
	{nil, 0, TankMachineGun},
	{nil, 0, TankMachineGun},
	{nil, 0, TankMachineGun},
	{nil, 0, TankMachineGun},
	{nil, 0, TankMachineGun},
	{nil, 0, TankMachineGun},
	{nil, 0, TankMachineGun},
	{nil, 0, TankMachineGun},
	{nil, 0, TankMachineGun},
	{nil, 0, TankMachineGun},
	{nil, 0, TankMachineGun},
	{nil, 0, TankMachineGun},
	{nil, 0, TankMachineGun},
	{nil, 0, TankMachineGun},
	{nil, 0, TankMachineGun},
	{nil, 0, TankMachineGun},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
}

// C: game/m_tank.c:634 tank_move_attack_chain
var tank_move_attack_chain = defMMove("tank_move_attack_chain", tank_FRAME_attak401, tank_FRAME_attak429, tank_frames_attack_chain, tank_run)

// C: game/m_tank.c:636 tank_refire_rocket
func (g *Game) tank_refire_rocket(self *Edict) {
	// Only on hard or nightmare
	if g.skill.Value >= 2 {
		if self.Enemy.Health > 0 {
			if g.visible(self, self.Enemy) {
				if float64(g.random()) <= 0.4 {
					self.Monsterinfo.Currentmove = tank_move_attack_fire_rocket
					return
				}
			}
		}
	}
	self.Monsterinfo.Currentmove = tank_move_attack_post_rocket
}

// C: game/m_tank.c:650 tank_doattack_rocket
func (g *Game) tank_doattack_rocket(self *Edict) {
	self.Monsterinfo.Currentmove = tank_move_attack_fire_rocket
}

// C: game/m_tank.c:655 tank_attack
func (g *Game) tank_attack(self *Edict) {
	var vec Vec3
	var range_ float32
	var r float32

	if self.Enemy.Health < 0 {
		self.Monsterinfo.Currentmove = tank_move_attack_strike
		self.Monsterinfo.Aiflags &^= AI_BRUTAL
		return
	}

	vec = shared.VectorSubtract(self.Enemy.S.Origin, self.S.Origin)
	range_ = shared.VectorLength(vec)

	r = g.random()

	if range_ <= 125 {
		if float64(r) < 0.4 {
			self.Monsterinfo.Currentmove = tank_move_attack_chain
		} else {
			self.Monsterinfo.Currentmove = tank_move_attack_blast
		}
	} else if range_ <= 250 {
		if float64(r) < 0.5 {
			self.Monsterinfo.Currentmove = tank_move_attack_chain
		} else {
			self.Monsterinfo.Currentmove = tank_move_attack_blast
		}
	} else {
		if float64(r) < 0.33 {
			self.Monsterinfo.Currentmove = tank_move_attack_chain
		} else if float64(r) < 0.66 {
			self.Monsterinfo.Currentmove = tank_move_attack_pre_rocket
			self.PainDebounceTime = float32(float64(g.level.Time) + 5.0) // no pain for a while
		} else {
			self.Monsterinfo.Currentmove = tank_move_attack_blast
		}
	}
}

// death
//
// C: game/m_tank.c:706 tank_dead
func (g *Game) tank_dead(self *Edict) {
	self.Mins = Vec3{-16, -16, -16}
	self.Maxs = Vec3{16, 16, -0}
	self.Movetype = MOVETYPE_TOSS
	self.SVFlags |= SVF_DEADMONSTER
	self.Nextthink = 0
	g.gi.LinkEntity(self)
}

// C: game/m_tank.c:716 tank_frames_death1
var tank_frames_death1 = []MFrame{
	{ai_move, -7, nil},
	{ai_move, -2, nil},
	{ai_move, -2, nil},
	{ai_move, 1, nil},
	{ai_move, 3, nil},
	{ai_move, 6, nil},
	{ai_move, 1, nil},
	{ai_move, 1, nil},
	{ai_move, 2, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, -2, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, -3, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, -4, nil},
	{ai_move, -6, nil},
	{ai_move, -4, nil},
	{ai_move, -5, nil},
	{ai_move, -7, nil},
	{ai_move, -15, tank_thud},
	{ai_move, -5, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
}

// C: game/m_tank.c:751 tank_move_death
var tank_move_death = defMMove("tank_move_death", tank_FRAME_death101, tank_FRAME_death132, tank_frames_death1, tank_dead)

// C: game/m_tank.c:753 tank_die
func (g *Game) tank_die(self *Edict, inflictor *Edict, attacker *Edict, damage int32, point Vec3) {
	s := monsterStatics[tankStatics](g, "m_tank")
	var n int32

	// check for gib
	if self.Health <= self.GibHealth {
		g.gi.Sound(self, CHAN_VOICE, g.gi.SoundIndex("misc/udeath.wav"), 1, ATTN_NORM, 0)
		for n = 0; n < 1; /*4*/ n++ {
			g.ThrowGib(self, "models/objects/gibs/sm_meat/tris.md2", damage, GIB_ORGANIC)
		}
		for n = 0; n < 4; n++ {
			g.ThrowGib(self, "models/objects/gibs/sm_metal/tris.md2", damage, GIB_METALLIC)
		}
		g.ThrowGib(self, "models/objects/gibs/chest/tris.md2", damage, GIB_ORGANIC)
		g.ThrowHead(self, "models/objects/gibs/gear/tris.md2", damage, GIB_METALLIC)
		self.Deadflag = DEAD_DEAD
		return
	}

	if self.Deadflag == DEAD_DEAD {
		return
	}

	// regular death
	g.gi.Sound(self, CHAN_VOICE, s.SoundDie, 1, ATTN_NORM, 0)
	self.Deadflag = DEAD_DEAD
	self.Takedamage = DAMAGE_YES

	self.Monsterinfo.Currentmove = tank_move_death
}

// monster_tank
//
// QUAKED monster_tank (1 .5 0) (-32 -32 -16) (32 32 72) Ambush Trigger_Spawn Sight
//
// QUAKED monster_tank_commander (1 .5 0) (-32 -32 -16) (32 32 72) Ambush Trigger_Spawn Sight
//
// C: game/m_tank.c:792 SP_monster_tank
func (g *Game) SP_monster_tank(self *Edict) {
	s := monsterStatics[tankStatics](g, "m_tank")
	if g.deathmatch.Value != 0 {
		g.G_FreeEdict(self)
		return
	}

	self.S.ModelIndex = int32(g.gi.ModelIndex("models/monsters/tank/tris.md2"))
	self.Mins = Vec3{-32, -32, -16}
	self.Maxs = Vec3{32, 32, 72}
	self.Movetype = MOVETYPE_STEP
	self.Solid = SOLID_BBOX

	s.SoundPain = g.gi.SoundIndex("tank/tnkpain2.wav")
	s.SoundThud = g.gi.SoundIndex("tank/tnkdeth2.wav")
	s.SoundIdle = g.gi.SoundIndex("tank/tnkidle1.wav")
	s.SoundDie = g.gi.SoundIndex("tank/death.wav")
	s.SoundStep = g.gi.SoundIndex("tank/step.wav")
	s.SoundWindup = g.gi.SoundIndex("tank/tnkatck4.wav")
	s.SoundStrike = g.gi.SoundIndex("tank/tnkatck5.wav")
	s.SoundSight = g.gi.SoundIndex("tank/sight1.wav")

	g.gi.SoundIndex("tank/tnkatck1.wav")
	g.gi.SoundIndex("tank/tnkatk2a.wav")
	g.gi.SoundIndex("tank/tnkatk2b.wav")
	g.gi.SoundIndex("tank/tnkatk2c.wav")
	g.gi.SoundIndex("tank/tnkatk2d.wav")
	g.gi.SoundIndex("tank/tnkatk2e.wav")
	g.gi.SoundIndex("tank/tnkatck3.wav")

	if self.Classname == "monster_tank_commander" {
		self.Health = 1000
		self.GibHealth = -225
	} else {
		self.Health = 750
		self.GibHealth = -200
	}

	self.Mass = 500

	self.Pain = tank_pain
	self.Die = tank_die
	self.Monsterinfo.Stand = tank_stand
	self.Monsterinfo.Walk = tank_walk
	self.Monsterinfo.Run = tank_run
	self.Monsterinfo.Dodge = nil
	self.Monsterinfo.Attack = tank_attack
	self.Monsterinfo.Melee = nil
	self.Monsterinfo.Sight = tank_sight
	self.Monsterinfo.Idle = tank_idle

	g.gi.LinkEntity(self)

	self.Monsterinfo.Currentmove = tank_move_stand
	self.Monsterinfo.Scale = tank_MODEL_SCALE

	g.walkmonster_start(self)

	if self.Classname == "monster_tank_commander" {
		self.S.SkinNum = 2
	}
}
