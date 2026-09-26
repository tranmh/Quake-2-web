package game

// Port of game/m_berserk.c.

import (
	. "quake2web/server/internal/q2const"
)

// statics of m_berserk.c
type berserkStatics struct {
	SoundPain   int // sound_pain
	SoundDie    int // sound_die
	SoundIdle   int // sound_idle
	SoundPunch  int // sound_punch
	SoundSight  int // sound_sight
	SoundSearch int // sound_search
}

var (
	berserk_sight        = defBlocked("berserk_sight")
	berserk_search       = defThink("berserk_search")
	berserk_stand        = defThink("berserk_stand")
	berserk_fidget       = defThink("berserk_fidget")
	berserk_walk         = defThink("berserk_walk")
	berserk_run          = defThink("berserk_run")
	berserk_attack_spike = defThink("berserk_attack_spike")
	berserk_swing        = defThink("berserk_swing")
	berserk_attack_club  = defThink("berserk_attack_club")
	berserk_strike       = defThink("berserk_strike")
	berserk_melee        = defThink("berserk_melee")
	berserk_pain         = defPain("berserk_pain")
	berserk_dead         = defThink("berserk_dead")
	berserk_die          = defDie("berserk_die")
)

func init() {
	berserk_sight.bind((*Game).berserk_sight)
	berserk_search.bind((*Game).berserk_search)
	berserk_stand.bind((*Game).berserk_stand)
	berserk_fidget.bind((*Game).berserk_fidget)
	berserk_walk.bind((*Game).berserk_walk)
	berserk_run.bind((*Game).berserk_run)
	berserk_attack_spike.bind((*Game).berserk_attack_spike)
	berserk_swing.bind((*Game).berserk_swing)
	berserk_attack_club.bind((*Game).berserk_attack_club)
	berserk_strike.bind((*Game).berserk_strike)
	berserk_melee.bind((*Game).berserk_melee)
	berserk_pain.bind((*Game).berserk_pain)
	berserk_dead.bind((*Game).berserk_dead)
	berserk_die.bind((*Game).berserk_die)
}

func init() {
	RegisterMonsterStatics("m_berserk", func() any { return new(berserkStatics) })
	RegisterSpawn("monster_berserk", (*Game).SP_monster_berserk)
}

// ==============================================================================
//
// # BERSERK
//
// ==============================================================================
//
// C: game/m_berserk.c:39 berserk_sight
func (g *Game) berserk_sight(self *Edict, other *Edict) {
	s := monsterStatics[berserkStatics](g, "m_berserk")
	g.gi.Sound(self, CHAN_VOICE, s.SoundSight, 1, ATTN_NORM, 0)
}

// C: game/m_berserk.c:44 berserk_search
func (g *Game) berserk_search(self *Edict) {
	s := monsterStatics[berserkStatics](g, "m_berserk")
	g.gi.Sound(self, CHAN_VOICE, s.SoundSearch, 1, ATTN_NORM, 0)
}

// C: game/m_berserk.c:51 berserk_frames_stand
var berserk_frames_stand = []MFrame{
	{ai_stand, 0, berserk_fidget},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
}

// C: game/m_berserk.c:59 berserk_move_stand
var berserk_move_stand = defMMove("berserk_move_stand", berserk_FRAME_stand1, berserk_FRAME_stand5, berserk_frames_stand, nil)

// C: game/m_berserk.c:61 berserk_stand
func (g *Game) berserk_stand(self *Edict) {
	self.Monsterinfo.Currentmove = berserk_move_stand
}

// C: game/m_berserk.c:66 berserk_frames_stand_fidget
var berserk_frames_stand_fidget = []MFrame{
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
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

// C: game/m_berserk.c:89 berserk_move_stand_fidget
var berserk_move_stand_fidget = defMMove("berserk_move_stand_fidget", berserk_FRAME_standb1, berserk_FRAME_standb20, berserk_frames_stand_fidget, berserk_stand)

// C: game/m_berserk.c:91 berserk_fidget
func (g *Game) berserk_fidget(self *Edict) {
	s := monsterStatics[berserkStatics](g, "m_berserk")
	if self.Monsterinfo.Aiflags&AI_STAND_GROUND != 0 {
		return
	}
	if float64(g.random()) > 0.15 {
		return
	}

	self.Monsterinfo.Currentmove = berserk_move_stand_fidget
	g.gi.Sound(self, CHAN_WEAPON, s.SoundIdle, 1, ATTN_IDLE, 0)
}

// C: game/m_berserk.c:103 berserk_frames_walk
var berserk_frames_walk = []MFrame{
	{ai_walk, 9.1, nil},
	{ai_walk, 6.3, nil},
	{ai_walk, 4.9, nil},
	{ai_walk, 6.7, nil},
	{ai_walk, 6.0, nil},
	{ai_walk, 8.2, nil},
	{ai_walk, 7.2, nil},
	{ai_walk, 6.1, nil},
	{ai_walk, 4.9, nil},
	{ai_walk, 4.7, nil},
	{ai_walk, 4.7, nil},
	{ai_walk, 4.8, nil},
}

// C: game/m_berserk.c:118 berserk_move_walk
var berserk_move_walk = defMMove("berserk_move_walk", berserk_FRAME_walkc1, berserk_FRAME_walkc11, berserk_frames_walk, nil)

// C: game/m_berserk.c:120 berserk_walk
func (g *Game) berserk_walk(self *Edict) {
	self.Monsterinfo.Currentmove = berserk_move_walk
}

// SKIPPED THIS FOR NOW!
//
// Running -> Arm raised in air
//
// void()	berserk_runb1	=[	$r_att1 ,	berserk_runb2	] {ai_run(21);};
// void()	berserk_runb2	=[	$r_att2 ,	berserk_runb3	] {ai_run(11);};
// void()	berserk_runb3	=[	$r_att3 ,	berserk_runb4	] {ai_run(21);};
// void()	berserk_runb4	=[	$r_att4 ,	berserk_runb5	] {ai_run(25);};
// void()	berserk_runb5	=[	$r_att5 ,	berserk_runb6	] {ai_run(18);};
// void()	berserk_runb6	=[	$r_att6 ,	berserk_runb7	] {ai_run(19);};
// running with arm in air : start loop
// void()	berserk_runb7	=[	$r_att7 ,	berserk_runb8	] {ai_run(21);};
// void()	berserk_runb8	=[	$r_att8 ,	berserk_runb9	] {ai_run(11);};
// void()	berserk_runb9	=[	$r_att9 ,	berserk_runb10	] {ai_run(21);};
// void()	berserk_runb10	=[	$r_att10 ,	berserk_runb11	] {ai_run(25);};
// void()	berserk_runb11	=[	$r_att11 ,	berserk_runb12	] {ai_run(18);};
// void()	berserk_runb12	=[	$r_att12 ,	berserk_runb7	] {ai_run(19);};
// running with arm in air : end loop
//
// C: game/m_berserk.c:150 berserk_frames_run1
var berserk_frames_run1 = []MFrame{
	{ai_run, 21, nil},
	{ai_run, 11, nil},
	{ai_run, 21, nil},
	{ai_run, 25, nil},
	{ai_run, 18, nil},
	{ai_run, 19, nil},
}

// C: game/m_berserk.c:159 berserk_move_run1
var berserk_move_run1 = defMMove("berserk_move_run1", berserk_FRAME_run1, berserk_FRAME_run6, berserk_frames_run1, nil)

// C: game/m_berserk.c:161 berserk_run
func (g *Game) berserk_run(self *Edict) {
	if self.Monsterinfo.Aiflags&AI_STAND_GROUND != 0 {
		self.Monsterinfo.Currentmove = berserk_move_stand
	} else {
		self.Monsterinfo.Currentmove = berserk_move_run1
	}
}

// C: game/m_berserk.c:170 berserk_attack_spike
func (g *Game) berserk_attack_spike(self *Edict) {
	aim := Vec3{MELEE_DISTANCE, 0, -24}                   // static in C, never modified
	g.fire_hit(self, aim, (15 + (g.rng.Rand() % 6)), 400) //	Faster attack -- upwards and backwards
}

// C: game/m_berserk.c:177 berserk_swing
func (g *Game) berserk_swing(self *Edict) {
	s := monsterStatics[berserkStatics](g, "m_berserk")
	g.gi.Sound(self, CHAN_WEAPON, s.SoundPunch, 1, ATTN_NORM, 0)
}

// C: game/m_berserk.c:182 berserk_frames_attack_spike
var berserk_frames_attack_spike = []MFrame{
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, berserk_swing},
	{ai_charge, 0, berserk_attack_spike},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
}

// C: game/m_berserk.c:193 berserk_move_attack_spike
var berserk_move_attack_spike = defMMove("berserk_move_attack_spike", berserk_FRAME_att_c1, berserk_FRAME_att_c8, berserk_frames_attack_spike, berserk_run)

// C: game/m_berserk.c:196 berserk_attack_club
func (g *Game) berserk_attack_club(self *Edict) {
	var aim Vec3

	aim = Vec3{MELEE_DISTANCE, self.Mins[0], -4}
	g.fire_hit(self, aim, (5 + (g.rng.Rand() % 6)), 400) // Slower attack
}

// C: game/m_berserk.c:204 berserk_frames_attack_club
var berserk_frames_attack_club = []MFrame{
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, berserk_swing},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, berserk_attack_club},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
}

// C: game/m_berserk.c:219 berserk_move_attack_club
var berserk_move_attack_club = defMMove("berserk_move_attack_club", berserk_FRAME_att_c9, berserk_FRAME_att_c20, berserk_frames_attack_club, berserk_run)

// C: game/m_berserk.c:222 berserk_strike
func (g *Game) berserk_strike(self *Edict) {
	//FIXME play impact sound
}

// C: game/m_berserk.c:228 berserk_frames_attack_strike
var berserk_frames_attack_strike = []MFrame{
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, berserk_swing},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, berserk_strike},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 9.7, nil},
	{ai_move, 13.6, nil},
}

// C: game/m_berserk.c:246 berserk_move_attack_strike
var berserk_move_attack_strike = defMMove("berserk_move_attack_strike", berserk_FRAME_att_c21, berserk_FRAME_att_c34, berserk_frames_attack_strike, berserk_run)

// C: game/m_berserk.c:249 berserk_melee
func (g *Game) berserk_melee(self *Edict) {
	if (g.rng.Rand() % 2) == 0 {
		self.Monsterinfo.Currentmove = berserk_move_attack_spike
	} else {
		self.Monsterinfo.Currentmove = berserk_move_attack_club
	}
}

// void() 	berserk_atke1	=[	$r_attb1,	berserk_atke2	] {ai_run(9);};
// void() 	berserk_atke2	=[	$r_attb2,	berserk_atke3	] {ai_run(6);};
// void() 	berserk_atke3	=[	$r_attb3,	berserk_atke4	] {ai_run(18.4);};
// void() 	berserk_atke4	=[	$r_attb4,	berserk_atke5	] {ai_run(25);};
// void() 	berserk_atke5	=[	$r_attb5,	berserk_atke6	] {ai_run(14);};
// void() 	berserk_atke6	=[	$r_attb6,	berserk_atke7	] {ai_run(20);};
// void() 	berserk_atke7	=[	$r_attb7,	berserk_atke8	] {ai_run(8.5);};
// void() 	berserk_atke8	=[	$r_attb8,	berserk_atke9	] {ai_run(3);};
// void() 	berserk_atke9	=[	$r_attb9,	berserk_atke10	] {ai_run(17.5);};
// void() 	berserk_atke10	=[	$r_attb10,	berserk_atke11	] {ai_run(17);};
// void() 	berserk_atke11	=[	$r_attb11,	berserk_atke12	] {ai_run(9);};
// void() 	berserk_atke12	=[	$r_attb12,	berserk_atke13	] {ai_run(25);};
// void() 	berserk_atke13	=[	$r_attb13,	berserk_atke14	] {ai_run(3.7);};
// void() 	berserk_atke14	=[	$r_attb14,	berserk_atke15	] {ai_run(2.6);};
// void() 	berserk_atke15	=[	$r_attb15,	berserk_atke16	] {ai_run(19);};
// void() 	berserk_atke16	=[	$r_attb16,	berserk_atke17	] {ai_run(25);};
// void() 	berserk_atke17	=[	$r_attb17,	berserk_atke18	] {ai_run(19.6);};
// void() 	berserk_atke18	=[	$r_attb18,	berserk_run1	] {ai_run(7.8);};
//
// C: game/m_berserk.c:280 berserk_frames_pain1
var berserk_frames_pain1 = []MFrame{
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
}

// C: game/m_berserk.c:287 berserk_move_pain1
var berserk_move_pain1 = defMMove("berserk_move_pain1", berserk_FRAME_painc1, berserk_FRAME_painc4, berserk_frames_pain1, berserk_run)

// C: game/m_berserk.c:290 berserk_frames_pain2
var berserk_frames_pain2 = []MFrame{
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

// C: game/m_berserk.c:313 berserk_move_pain2
var berserk_move_pain2 = defMMove("berserk_move_pain2", berserk_FRAME_painb1, berserk_FRAME_painb20, berserk_frames_pain2, berserk_run)

// C: game/m_berserk.c:315 berserk_pain
func (g *Game) berserk_pain(self *Edict, other *Edict, kick float32, damage int32) {
	s := monsterStatics[berserkStatics](g, "m_berserk")
	if self.Health < (self.MaxHealth / 2) {
		self.S.SkinNum = 1
	}

	if g.level.Time < self.PainDebounceTime {
		return
	}

	self.PainDebounceTime = g.level.Time + 3
	g.gi.Sound(self, CHAN_VOICE, s.SoundPain, 1, ATTN_NORM, 0)

	if g.skill.Value == 3 {
		return // no pain anims in nightmare
	}

	if (damage < 20) || (g.random() < 0.5) {
		self.Monsterinfo.Currentmove = berserk_move_pain1
	} else {
		self.Monsterinfo.Currentmove = berserk_move_pain2
	}
}

// C: game/m_berserk.c:336 berserk_dead
func (g *Game) berserk_dead(self *Edict) {
	self.Mins = Vec3{-16, -16, -24}
	self.Maxs = Vec3{16, 16, -8}
	self.Movetype = MOVETYPE_TOSS
	self.SVFlags |= SVF_DEADMONSTER
	self.Nextthink = 0
	g.gi.LinkEntity(self)
}

// C: game/m_berserk.c:347 berserk_frames_death1
var berserk_frames_death1 = []MFrame{
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

// C: game/m_berserk.c:364 berserk_move_death1
var berserk_move_death1 = defMMove("berserk_move_death1", berserk_FRAME_death1, berserk_FRAME_death13, berserk_frames_death1, berserk_dead)

// C: game/m_berserk.c:367 berserk_frames_death2
var berserk_frames_death2 = []MFrame{
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
}

// C: game/m_berserk.c:378 berserk_move_death2
var berserk_move_death2 = defMMove("berserk_move_death2", berserk_FRAME_deathc1, berserk_FRAME_deathc8, berserk_frames_death2, berserk_dead)

// C: game/m_berserk.c:381 berserk_die
func (g *Game) berserk_die(self *Edict, inflictor *Edict, attacker *Edict, damage int32, point Vec3) {
	s := monsterStatics[berserkStatics](g, "m_berserk")
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

	g.gi.Sound(self, CHAN_VOICE, s.SoundDie, 1, ATTN_NORM, 0)
	self.Deadflag = DEAD_DEAD
	self.Takedamage = DAMAGE_YES

	if damage >= 50 {
		self.Monsterinfo.Currentmove = berserk_move_death1
	} else {
		self.Monsterinfo.Currentmove = berserk_move_death2
	}
}

// QUAKED monster_berserk (1 .5 0) (-16 -16 -24) (16 16 32) Ambush Trigger_Spawn Sight
//
// C: game/m_berserk.c:413 SP_monster_berserk
func (g *Game) SP_monster_berserk(self *Edict) {
	s := monsterStatics[berserkStatics](g, "m_berserk")
	if g.deathmatch.Value != 0 {
		g.G_FreeEdict(self)
		return
	}

	// pre-caches
	s.SoundPain = g.gi.SoundIndex("berserk/berpain2.wav")
	s.SoundDie = g.gi.SoundIndex("berserk/berdeth2.wav")
	s.SoundIdle = g.gi.SoundIndex("berserk/beridle1.wav")
	s.SoundPunch = g.gi.SoundIndex("berserk/attack.wav")
	s.SoundSearch = g.gi.SoundIndex("berserk/bersrch1.wav")
	s.SoundSight = g.gi.SoundIndex("berserk/sight.wav")

	self.S.ModelIndex = int32(g.gi.ModelIndex("models/monsters/berserk/tris.md2"))
	self.Mins = Vec3{-16, -16, -24}
	self.Maxs = Vec3{16, 16, 32}
	self.Movetype = MOVETYPE_STEP
	self.Solid = SOLID_BBOX

	self.Health = 240
	self.GibHealth = -60
	self.Mass = 250

	self.Pain = berserk_pain
	self.Die = berserk_die

	self.Monsterinfo.Stand = berserk_stand
	self.Monsterinfo.Walk = berserk_walk
	self.Monsterinfo.Run = berserk_run
	self.Monsterinfo.Dodge = nil
	self.Monsterinfo.Attack = nil
	self.Monsterinfo.Melee = berserk_melee
	self.Monsterinfo.Sight = berserk_sight
	self.Monsterinfo.Search = berserk_search

	self.Monsterinfo.Currentmove = berserk_move_stand
	self.Monsterinfo.Scale = berserk_MODEL_SCALE

	g.gi.LinkEntity(self)

	g.walkmonster_start(self)
}
