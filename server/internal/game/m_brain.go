package game

// Port of game/m_brain.c.

import (
	. "quake2web/server/internal/q2const"
)

// statics of m_brain.c
type brainStatics struct {
	SoundChestOpen        int // sound_chest_open
	SoundTentaclesExtend  int // sound_tentacles_extend
	SoundTentaclesRetract int // sound_tentacles_retract
	SoundDeath            int // sound_death
	SoundIdle1            int // sound_idle1
	SoundIdle2            int // sound_idle2
	SoundIdle3            int // sound_idle3
	SoundPain1            int // sound_pain1
	SoundPain2            int // sound_pain2
	SoundSight            int // sound_sight
	SoundSearch           int // sound_search
	SoundMelee1           int // sound_melee1
	SoundMelee2           int // sound_melee2
	SoundMelee3           int // sound_melee3
}

var (
	brain_sight           = defBlocked("brain_sight")
	brain_search          = defThink("brain_search")
	brain_stand           = defThink("brain_stand")
	brain_idle            = defThink("brain_idle")
	brain_walk            = defThink("brain_walk")
	brain_duck_down       = defThink("brain_duck_down")
	brain_duck_hold       = defThink("brain_duck_hold")
	brain_duck_up         = defThink("brain_duck_up")
	brain_dodge           = defDodge("brain_dodge")
	brain_swing_right     = defThink("brain_swing_right")
	brain_hit_right       = defThink("brain_hit_right")
	brain_swing_left      = defThink("brain_swing_left")
	brain_hit_left        = defThink("brain_hit_left")
	brain_chest_open      = defThink("brain_chest_open")
	brain_tentacle_attack = defThink("brain_tentacle_attack")
	brain_chest_closed    = defThink("brain_chest_closed")
	brain_melee           = defThink("brain_melee")
	brain_run             = defThink("brain_run")
	brain_pain            = defPain("brain_pain")
	brain_dead            = defThink("brain_dead")
	brain_die             = defDie("brain_die")
)

func init() {
	brain_sight.bind((*Game).brain_sight)
	brain_search.bind((*Game).brain_search)
	brain_stand.bind((*Game).brain_stand)
	brain_idle.bind((*Game).brain_idle)
	brain_walk.bind((*Game).brain_walk)
	brain_duck_down.bind((*Game).brain_duck_down)
	brain_duck_hold.bind((*Game).brain_duck_hold)
	brain_duck_up.bind((*Game).brain_duck_up)
	brain_dodge.bind((*Game).brain_dodge)
	brain_swing_right.bind((*Game).brain_swing_right)
	brain_hit_right.bind((*Game).brain_hit_right)
	brain_swing_left.bind((*Game).brain_swing_left)
	brain_hit_left.bind((*Game).brain_hit_left)
	brain_chest_open.bind((*Game).brain_chest_open)
	brain_tentacle_attack.bind((*Game).brain_tentacle_attack)
	brain_chest_closed.bind((*Game).brain_chest_closed)
	brain_melee.bind((*Game).brain_melee)
	brain_run.bind((*Game).brain_run)
	brain_pain.bind((*Game).brain_pain)
	brain_dead.bind((*Game).brain_dead)
	brain_die.bind((*Game).brain_die)
}

func init() {
	RegisterMonsterStatics("m_brain", func() any { return new(brainStatics) })
	RegisterSpawn("monster_brain", (*Game).SP_monster_brain)
}

// C: game/m_brain.c:48 brain_sight
func (g *Game) brain_sight(self *Edict, other *Edict) {
	s := monsterStatics[brainStatics](g, "m_brain")
	g.gi.Sound(self, CHAN_VOICE, s.SoundSight, 1, ATTN_NORM, 0)
}

// C: game/m_brain.c:53 brain_search
func (g *Game) brain_search(self *Edict) {
	s := monsterStatics[brainStatics](g, "m_brain")
	g.gi.Sound(self, CHAN_VOICE, s.SoundSearch, 1, ATTN_NORM, 0)
}

//
// STAND
//

// C: game/m_brain.c:67 brain_frames_stand
var brain_frames_stand = []MFrame{
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

// C: game/m_brain.c:102 brain_move_stand
var brain_move_stand = defMMove("brain_move_stand", brain_FRAME_stand01, brain_FRAME_stand30, brain_frames_stand, nil)

// C: game/m_brain.c:104 brain_stand
func (g *Game) brain_stand(self *Edict) {
	self.Monsterinfo.Currentmove = brain_move_stand
}

//
// IDLE
//

// C: game/m_brain.c:114 brain_frames_idle
var brain_frames_idle = []MFrame{
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

// C: game/m_brain.c:149 brain_move_idle
var brain_move_idle = defMMove("brain_move_idle", brain_FRAME_stand31, brain_FRAME_stand60, brain_frames_idle, brain_stand)

// C: game/m_brain.c:151 brain_idle
func (g *Game) brain_idle(self *Edict) {
	s := monsterStatics[brainStatics](g, "m_brain")
	g.gi.Sound(self, CHAN_AUTO, s.SoundIdle3, 1, ATTN_IDLE, 0)
	self.Monsterinfo.Currentmove = brain_move_idle
}

//
// WALK
//

// C: game/m_brain.c:161 brain_frames_walk1
var brain_frames_walk1 = []MFrame{
	{ai_walk, 7, nil},
	{ai_walk, 2, nil},
	{ai_walk, 3, nil},
	{ai_walk, 3, nil},
	{ai_walk, 1, nil},
	{ai_walk, 0, nil},
	{ai_walk, 0, nil},
	{ai_walk, 9, nil},
	{ai_walk, -4, nil},
	{ai_walk, -1, nil},
	{ai_walk, 2, nil},
}

// C: game/m_brain.c:175 brain_move_walk1
var brain_move_walk1 = defMMove("brain_move_walk1", brain_FRAME_walk101, brain_FRAME_walk111, brain_frames_walk1, nil)

// walk2 is FUBAR, do not use (brain_walk2_cycle, brain_frames_walk2 and
// brain_move_walk2 are inside #if 0 in C).

// C: game/m_brain.c:234 brain_walk
func (g *Game) brain_walk(self *Edict) {
	//	if (random() <= 0.5)
	self.Monsterinfo.Currentmove = brain_move_walk1
	//	else
	//		self->monsterinfo.currentmove = &brain_move_walk2;
}

// C: game/m_brain.c:244 brain_frames_defense
var brain_frames_defense = []MFrame{
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

// C: game/m_brain.c:256 brain_move_defense
var brain_move_defense = defMMove("brain_move_defense", brain_FRAME_defens01, brain_FRAME_defens08, brain_frames_defense, nil)

// C: game/m_brain.c:258 brain_frames_pain3
var brain_frames_pain3 = []MFrame{
	{ai_move, -2, nil},
	{ai_move, 2, nil},
	{ai_move, 1, nil},
	{ai_move, 3, nil},
	{ai_move, 0, nil},
	{ai_move, -4, nil},
}

// C: game/m_brain.c:267 brain_move_pain3
var brain_move_pain3 = defMMove("brain_move_pain3", brain_FRAME_pain301, brain_FRAME_pain306, brain_frames_pain3, brain_run)

// C: game/m_brain.c:269 brain_frames_pain2
var brain_frames_pain2 = []MFrame{
	{ai_move, -2, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 3, nil},
	{ai_move, 1, nil},
	{ai_move, -2, nil},
}

// C: game/m_brain.c:280 brain_move_pain2
var brain_move_pain2 = defMMove("brain_move_pain2", brain_FRAME_pain201, brain_FRAME_pain208, brain_frames_pain2, brain_run)

// C: game/m_brain.c:282 brain_frames_pain1
var brain_frames_pain1 = []MFrame{
	{ai_move, -6, nil},
	{ai_move, -2, nil},
	{ai_move, -6, nil},
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
	{ai_move, 2, nil},
	{ai_move, 0, nil},
	{ai_move, 2, nil},
	{ai_move, 1, nil},
	{ai_move, 7, nil},
	{ai_move, 0, nil},
	{ai_move, 3, nil},
	{ai_move, -1, nil},
}

// C: game/m_brain.c:306 brain_move_pain1
var brain_move_pain1 = defMMove("brain_move_pain1", brain_FRAME_pain101, brain_FRAME_pain121, brain_frames_pain1, brain_run)

//
// DUCK
//

// C: game/m_brain.c:313 brain_duck_down
func (g *Game) brain_duck_down(self *Edict) {
	if self.Monsterinfo.Aiflags&AI_DUCKED != 0 {
		return
	}
	self.Monsterinfo.Aiflags |= AI_DUCKED
	self.Maxs[2] -= 32
	self.Takedamage = DAMAGE_YES
	g.gi.LinkEntity(self)
}

// C: game/m_brain.c:323 brain_duck_hold
func (g *Game) brain_duck_hold(self *Edict) {
	if g.level.Time >= self.Monsterinfo.Pausetime {
		self.Monsterinfo.Aiflags &^= AI_HOLD_FRAME
	} else {
		self.Monsterinfo.Aiflags |= AI_HOLD_FRAME
	}
}

// C: game/m_brain.c:331 brain_duck_up
func (g *Game) brain_duck_up(self *Edict) {
	self.Monsterinfo.Aiflags &^= AI_DUCKED
	self.Maxs[2] += 32
	self.Takedamage = DAMAGE_AIM
	g.gi.LinkEntity(self)
}

// C: game/m_brain.c:339 brain_frames_duck
var brain_frames_duck = []MFrame{
	{ai_move, 0, nil},
	{ai_move, -2, brain_duck_down},
	{ai_move, 17, brain_duck_hold},
	{ai_move, -3, nil},
	{ai_move, -1, brain_duck_up},
	{ai_move, -5, nil},
	{ai_move, -6, nil},
	{ai_move, -6, nil},
}

// C: game/m_brain.c:350 brain_move_duck
var brain_move_duck = defMMove("brain_move_duck", brain_FRAME_duck01, brain_FRAME_duck08, brain_frames_duck, brain_run)

// C: game/m_brain.c:352 brain_dodge
func (g *Game) brain_dodge(self *Edict, attacker *Edict, eta float32) {
	if float64(g.random()) > 0.25 {
		return
	}

	if self.Enemy == nil {
		self.Enemy = attacker
	}

	self.Monsterinfo.Pausetime = float32(float64(g.level.Time+eta) + 0.5)
	self.Monsterinfo.Currentmove = brain_move_duck
}

// C: game/m_brain.c:365 brain_frames_death2
var brain_frames_death2 = []MFrame{
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 9, nil},
	{ai_move, 0, nil},
}

// C: game/m_brain.c:373 brain_move_death2
var brain_move_death2 = defMMove("brain_move_death2", brain_FRAME_death201, brain_FRAME_death205, brain_frames_death2, brain_dead)

// C: game/m_brain.c:375 brain_frames_death1
var brain_frames_death1 = []MFrame{
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, -2, nil},
	{ai_move, 9, nil},
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

// C: game/m_brain.c:396 brain_move_death1
var brain_move_death1 = defMMove("brain_move_death1", brain_FRAME_death101, brain_FRAME_death118, brain_frames_death1, brain_dead)

//
// MELEE
//

// C: game/m_brain.c:403 brain_swing_right
func (g *Game) brain_swing_right(self *Edict) {
	s := monsterStatics[brainStatics](g, "m_brain")
	g.gi.Sound(self, CHAN_BODY, s.SoundMelee1, 1, ATTN_NORM, 0)
}

// C: game/m_brain.c:408 brain_hit_right
func (g *Game) brain_hit_right(self *Edict) {
	s := monsterStatics[brainStatics](g, "m_brain")
	aim := Vec3{MELEE_DISTANCE, self.Maxs[0], 8}
	if g.fire_hit(self, aim, (15 + (g.rng.Rand() % 5)), 40) {
		g.gi.Sound(self, CHAN_WEAPON, s.SoundMelee3, 1, ATTN_NORM, 0)
	}
}

// C: game/m_brain.c:417 brain_swing_left
func (g *Game) brain_swing_left(self *Edict) {
	s := monsterStatics[brainStatics](g, "m_brain")
	g.gi.Sound(self, CHAN_BODY, s.SoundMelee2, 1, ATTN_NORM, 0)
}

// C: game/m_brain.c:422 brain_hit_left
func (g *Game) brain_hit_left(self *Edict) {
	s := monsterStatics[brainStatics](g, "m_brain")
	aim := Vec3{MELEE_DISTANCE, self.Mins[0], 8}
	if g.fire_hit(self, aim, (15 + (g.rng.Rand() % 5)), 40) {
		g.gi.Sound(self, CHAN_WEAPON, s.SoundMelee3, 1, ATTN_NORM, 0)
	}
}

// C: game/m_brain.c:431 brain_frames_attack1
var brain_frames_attack1 = []MFrame{
	{ai_charge, 8, nil},
	{ai_charge, 3, nil},
	{ai_charge, 5, nil},
	{ai_charge, 0, nil},
	{ai_charge, -3, brain_swing_right},
	{ai_charge, 0, nil},
	{ai_charge, -5, nil},
	{ai_charge, -7, brain_hit_right},
	{ai_charge, 0, nil},
	{ai_charge, 6, brain_swing_left},
	{ai_charge, 1, nil},
	{ai_charge, 2, brain_hit_left},
	{ai_charge, -3, nil},
	{ai_charge, 6, nil},
	{ai_charge, -1, nil},
	{ai_charge, -3, nil},
	{ai_charge, 2, nil},
	{ai_charge, -11, nil},
}

// C: game/m_brain.c:452 brain_move_attack1
var brain_move_attack1 = defMMove("brain_move_attack1", brain_FRAME_attak101, brain_FRAME_attak118, brain_frames_attack1, brain_run)

// C: game/m_brain.c:454 brain_chest_open
func (g *Game) brain_chest_open(self *Edict) {
	s := monsterStatics[brainStatics](g, "m_brain")
	self.Spawnflags &^= 65536
	self.Monsterinfo.PowerArmorType = POWER_ARMOR_NONE
	g.gi.Sound(self, CHAN_BODY, s.SoundChestOpen, 1, ATTN_NORM, 0)
}

// C: game/m_brain.c:461 brain_tentacle_attack
func (g *Game) brain_tentacle_attack(self *Edict) {
	s := monsterStatics[brainStatics](g, "m_brain")
	aim := Vec3{MELEE_DISTANCE, 0, 8}
	if g.fire_hit(self, aim, (10+(g.rng.Rand()%5)), -600) && g.skill.Value > 0 {
		self.Spawnflags |= 65536
	}
	g.gi.Sound(self, CHAN_WEAPON, s.SoundTentaclesRetract, 1, ATTN_NORM, 0)
}

// C: game/m_brain.c:471 brain_chest_closed
func (g *Game) brain_chest_closed(self *Edict) {
	self.Monsterinfo.PowerArmorType = POWER_ARMOR_SCREEN
	if self.Spawnflags&65536 != 0 {
		self.Spawnflags &^= 65536
		self.Monsterinfo.Currentmove = brain_move_attack1
	}
}

// C: game/m_brain.c:481 brain_frames_attack2
var brain_frames_attack2 = []MFrame{
	{ai_charge, 5, nil},
	{ai_charge, -4, nil},
	{ai_charge, -4, nil},
	{ai_charge, -3, nil},
	{ai_charge, 0, brain_chest_open},
	{ai_charge, 0, nil},
	{ai_charge, 13, brain_tentacle_attack},
	{ai_charge, 0, nil},
	{ai_charge, 2, nil},
	{ai_charge, 0, nil},
	{ai_charge, -9, brain_chest_closed},
	{ai_charge, 0, nil},
	{ai_charge, 4, nil},
	{ai_charge, 3, nil},
	{ai_charge, 2, nil},
	{ai_charge, -3, nil},
	{ai_charge, -6, nil},
}

// C: game/m_brain.c:501 brain_move_attack2
var brain_move_attack2 = defMMove("brain_move_attack2", brain_FRAME_attak201, brain_FRAME_attak217, brain_frames_attack2, brain_run)

// C: game/m_brain.c:503 brain_melee
func (g *Game) brain_melee(self *Edict) {
	if float64(g.random()) <= 0.5 {
		self.Monsterinfo.Currentmove = brain_move_attack1
	} else {
		self.Monsterinfo.Currentmove = brain_move_attack2
	}
}

//
// RUN
//

// C: game/m_brain.c:516 brain_frames_run
var brain_frames_run = []MFrame{
	{ai_run, 9, nil},
	{ai_run, 2, nil},
	{ai_run, 3, nil},
	{ai_run, 3, nil},
	{ai_run, 1, nil},
	{ai_run, 0, nil},
	{ai_run, 0, nil},
	{ai_run, 10, nil},
	{ai_run, -4, nil},
	{ai_run, -1, nil},
	{ai_run, 2, nil},
}

// C: game/m_brain.c:530 brain_move_run
var brain_move_run = defMMove("brain_move_run", brain_FRAME_walk101, brain_FRAME_walk111, brain_frames_run, nil)

// C: game/m_brain.c:532 brain_run
func (g *Game) brain_run(self *Edict) {
	self.Monsterinfo.PowerArmorType = POWER_ARMOR_SCREEN
	if self.Monsterinfo.Aiflags&AI_STAND_GROUND != 0 {
		self.Monsterinfo.Currentmove = brain_move_stand
	} else {
		self.Monsterinfo.Currentmove = brain_move_run
	}
}

// C: game/m_brain.c:542 brain_pain
func (g *Game) brain_pain(self *Edict, other *Edict, kick float32, damage int32) {
	s := monsterStatics[brainStatics](g, "m_brain")
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
		self.Monsterinfo.Currentmove = brain_move_pain1
	} else if float64(r) < 0.66 {
		g.gi.Sound(self, CHAN_VOICE, s.SoundPain2, 1, ATTN_NORM, 0)
		self.Monsterinfo.Currentmove = brain_move_pain2
	} else {
		g.gi.Sound(self, CHAN_VOICE, s.SoundPain1, 1, ATTN_NORM, 0)
		self.Monsterinfo.Currentmove = brain_move_pain3
	}
}

// C: game/m_brain.c:574 brain_dead
func (g *Game) brain_dead(self *Edict) {
	self.Mins = Vec3{-16, -16, -24}
	self.Maxs = Vec3{16, 16, -8}
	self.Movetype = MOVETYPE_TOSS
	self.SVFlags |= SVF_DEADMONSTER
	self.Nextthink = 0
	g.gi.LinkEntity(self)
}

// C: game/m_brain.c:586 brain_die
func (g *Game) brain_die(self *Edict, inflictor *Edict, attacker *Edict, damage int32, point Vec3) {
	s := monsterStatics[brainStatics](g, "m_brain")
	var n int32

	self.S.Effects = 0
	self.Monsterinfo.PowerArmorType = POWER_ARMOR_NONE

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
	if float64(g.random()) <= 0.5 {
		self.Monsterinfo.Currentmove = brain_move_death1
	} else {
		self.Monsterinfo.Currentmove = brain_move_death2
	}
}

// QUAKED monster_brain (1 .5 0) (-16 -16 -24) (16 16 32) Ambush Trigger_Spawn Sight
//
// C: game/m_brain.c:621 SP_monster_brain
func (g *Game) SP_monster_brain(self *Edict) {
	s := monsterStatics[brainStatics](g, "m_brain")
	if g.deathmatch.Value != 0 {
		g.G_FreeEdict(self)
		return
	}

	s.SoundChestOpen = g.gi.SoundIndex("brain/brnatck1.wav")
	s.SoundTentaclesExtend = g.gi.SoundIndex("brain/brnatck2.wav")
	s.SoundTentaclesRetract = g.gi.SoundIndex("brain/brnatck3.wav")
	s.SoundDeath = g.gi.SoundIndex("brain/brndeth1.wav")
	s.SoundIdle1 = g.gi.SoundIndex("brain/brnidle1.wav")
	s.SoundIdle2 = g.gi.SoundIndex("brain/brnidle2.wav")
	s.SoundIdle3 = g.gi.SoundIndex("brain/brnlens1.wav")
	s.SoundPain1 = g.gi.SoundIndex("brain/brnpain1.wav")
	s.SoundPain2 = g.gi.SoundIndex("brain/brnpain2.wav")
	s.SoundSight = g.gi.SoundIndex("brain/brnsght1.wav")
	s.SoundSearch = g.gi.SoundIndex("brain/brnsrch1.wav")
	s.SoundMelee1 = g.gi.SoundIndex("brain/melee1.wav")
	s.SoundMelee2 = g.gi.SoundIndex("brain/melee2.wav")
	s.SoundMelee3 = g.gi.SoundIndex("brain/melee3.wav")

	self.Movetype = MOVETYPE_STEP
	self.Solid = SOLID_BBOX
	self.S.ModelIndex = int32(g.gi.ModelIndex("models/monsters/brain/tris.md2"))
	self.Mins = Vec3{-16, -16, -24}
	self.Maxs = Vec3{16, 16, 32}

	self.Health = 300
	self.GibHealth = -150
	self.Mass = 400

	self.Pain = brain_pain
	self.Die = brain_die

	self.Monsterinfo.Stand = brain_stand
	self.Monsterinfo.Walk = brain_walk
	self.Monsterinfo.Run = brain_run
	self.Monsterinfo.Dodge = brain_dodge
	//	self->monsterinfo.attack = brain_attack;
	self.Monsterinfo.Melee = brain_melee
	self.Monsterinfo.Sight = brain_sight
	self.Monsterinfo.Search = brain_search
	self.Monsterinfo.Idle = brain_idle

	self.Monsterinfo.PowerArmorType = POWER_ARMOR_SCREEN
	self.Monsterinfo.PowerArmorPower = 100

	g.gi.LinkEntity(self)

	self.Monsterinfo.Currentmove = brain_move_stand
	self.Monsterinfo.Scale = brain_MODEL_SCALE

	g.walkmonster_start(self)
}
