package game

// Port of game/m_flipper.c.

import (
	. "quake2web/server/internal/q2const"
)

// statics of m_flipper.c
type flipperStatics struct {
	SoundChomp  int // sound_chomp
	SoundAttack int // sound_attack
	SoundPain1  int // sound_pain1
	SoundPain2  int // sound_pain2
	SoundDeath  int // sound_death
	SoundIdle   int // sound_idle
	SoundSearch int // sound_search
	SoundSight  int // sound_sight
}

var (
	flipper_stand     = defThink("flipper_stand")
	flipper_run_loop  = defThink("flipper_run_loop")
	flipper_run       = defThink("flipper_run")
	flipper_walk      = defThink("flipper_walk")
	flipper_start_run = defThink("flipper_start_run")
	flipper_bite      = defThink("flipper_bite")
	flipper_preattack = defThink("flipper_preattack")
	flipper_melee     = defThink("flipper_melee")
	flipper_pain      = defPain("flipper_pain")
	flipper_dead      = defThink("flipper_dead")
	flipper_sight     = defBlocked("flipper_sight")
	flipper_die       = defDie("flipper_die")
)

func init() {
	flipper_stand.bind((*Game).flipper_stand)
	flipper_run_loop.bind((*Game).flipper_run_loop)
	flipper_run.bind((*Game).flipper_run)
	flipper_walk.bind((*Game).flipper_walk)
	flipper_start_run.bind((*Game).flipper_start_run)
	flipper_bite.bind((*Game).flipper_bite)
	flipper_preattack.bind((*Game).flipper_preattack)
	flipper_melee.bind((*Game).flipper_melee)
	flipper_pain.bind((*Game).flipper_pain)
	flipper_dead.bind((*Game).flipper_dead)
	flipper_sight.bind((*Game).flipper_sight)
	flipper_die.bind((*Game).flipper_die)
}

func init() {
	RegisterMonsterStatics("m_flipper", func() any { return new(flipperStatics) })
	RegisterSpawn("monster_flipper", (*Game).SP_monster_flipper)
}

// C: game/m_flipper.c:44 flipper_frames_stand
var flipper_frames_stand = []MFrame{
	{ai_stand, 0, nil},
}

// C: game/m_flipper.c:49 flipper_move_stand
var flipper_move_stand = defMMove("flipper_move_stand", flipper_FRAME_flphor01, flipper_FRAME_flphor01, flipper_frames_stand, nil)

// C: game/m_flipper.c:51 flipper_stand
func (g *Game) flipper_stand(self *Edict) {
	self.Monsterinfo.Currentmove = flipper_move_stand
}

// FLIPPER_RUN_SPEED is the #define of m_flipper.c.
const FLIPPER_RUN_SPEED = 24

// C: game/m_flipper.c:58 flipper_frames_run
var flipper_frames_run = []MFrame{
	{ai_run, FLIPPER_RUN_SPEED, nil},
	{ai_run, FLIPPER_RUN_SPEED, nil},
	{ai_run, FLIPPER_RUN_SPEED, nil},
	{ai_run, FLIPPER_RUN_SPEED, nil},
	{ai_run, FLIPPER_RUN_SPEED, nil},
	{ai_run, FLIPPER_RUN_SPEED, nil},
	{ai_run, FLIPPER_RUN_SPEED, nil},
	{ai_run, FLIPPER_RUN_SPEED, nil},
	{ai_run, FLIPPER_RUN_SPEED, nil},
	{ai_run, FLIPPER_RUN_SPEED, nil},
	{ai_run, FLIPPER_RUN_SPEED, nil},
	{ai_run, FLIPPER_RUN_SPEED, nil},
	{ai_run, FLIPPER_RUN_SPEED, nil},
	{ai_run, FLIPPER_RUN_SPEED, nil},
	{ai_run, FLIPPER_RUN_SPEED, nil},
	{ai_run, FLIPPER_RUN_SPEED, nil},
	{ai_run, FLIPPER_RUN_SPEED, nil},
	{ai_run, FLIPPER_RUN_SPEED, nil},
	{ai_run, FLIPPER_RUN_SPEED, nil},
	{ai_run, FLIPPER_RUN_SPEED, nil},
	{ai_run, FLIPPER_RUN_SPEED, nil},
	{ai_run, FLIPPER_RUN_SPEED, nil},
	{ai_run, FLIPPER_RUN_SPEED, nil},
	{ai_run, FLIPPER_RUN_SPEED, nil},
}

// C: game/m_flipper.c:87 flipper_move_run_loop
var flipper_move_run_loop = defMMove("flipper_move_run_loop", flipper_FRAME_flpver06, flipper_FRAME_flpver29, flipper_frames_run, nil)

// C: game/m_flipper.c:89 flipper_run_loop
func (g *Game) flipper_run_loop(self *Edict) {
	self.Monsterinfo.Currentmove = flipper_move_run_loop
}

// C: game/m_flipper.c:94 flipper_frames_run_start
var flipper_frames_run_start = []MFrame{
	{ai_run, 8, nil},
	{ai_run, 8, nil},
	{ai_run, 8, nil},
	{ai_run, 8, nil},
	{ai_run, 8, nil},
	{ai_run, 8, nil},
}

// C: game/m_flipper.c:103 flipper_move_run_start
var flipper_move_run_start = defMMove("flipper_move_run_start", flipper_FRAME_flpver01, flipper_FRAME_flpver06, flipper_frames_run_start, flipper_run_loop)

// C: game/m_flipper.c:105 flipper_run
func (g *Game) flipper_run(self *Edict) {
	self.Monsterinfo.Currentmove = flipper_move_run_start
}

// Standard Swimming
// C: game/m_flipper.c:111 flipper_frames_walk
var flipper_frames_walk = []MFrame{
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
	{ai_walk, 4, nil},
	{ai_walk, 4, nil},
	{ai_walk, 4, nil},
	{ai_walk, 4, nil},
	{ai_walk, 4, nil},
	{ai_walk, 4, nil},
	{ai_walk, 4, nil},
}

// C: game/m_flipper.c:138 flipper_move_walk
var flipper_move_walk = defMMove("flipper_move_walk", flipper_FRAME_flphor01, flipper_FRAME_flphor24, flipper_frames_walk, nil)

// C: game/m_flipper.c:140 flipper_walk
func (g *Game) flipper_walk(self *Edict) {
	self.Monsterinfo.Currentmove = flipper_move_walk
}

// C: game/m_flipper.c:145 flipper_frames_start_run
var flipper_frames_start_run = []MFrame{
	{ai_run, 8, nil},
	{ai_run, 8, nil},
	{ai_run, 8, nil},
	{ai_run, 8, nil},
	{ai_run, 8, flipper_run},
}

// C: game/m_flipper.c:153 flipper_move_start_run
var flipper_move_start_run = defMMove("flipper_move_start_run", flipper_FRAME_flphor01, flipper_FRAME_flphor05, flipper_frames_start_run, nil)

// C: game/m_flipper.c:155 flipper_start_run
func (g *Game) flipper_start_run(self *Edict) {
	self.Monsterinfo.Currentmove = flipper_move_start_run
}

// C: game/m_flipper.c:160 flipper_frames_pain2
var flipper_frames_pain2 = []MFrame{
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
}

// C: game/m_flipper.c:168 flipper_move_pain2
var flipper_move_pain2 = defMMove("flipper_move_pain2", flipper_FRAME_flppn101, flipper_FRAME_flppn105, flipper_frames_pain2, flipper_run)

// C: game/m_flipper.c:170 flipper_frames_pain1
var flipper_frames_pain1 = []MFrame{
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
}

// C: game/m_flipper.c:178 flipper_move_pain1
var flipper_move_pain1 = defMMove("flipper_move_pain1", flipper_FRAME_flppn201, flipper_FRAME_flppn205, flipper_frames_pain1, flipper_run)

// C: game/m_flipper.c:180 flipper_bite
func (g *Game) flipper_bite(self *Edict) {
	aim := Vec3{MELEE_DISTANCE, 0, 0}
	g.fire_hit(self, aim, 5, 0)
}

// C: game/m_flipper.c:188 flipper_preattack
func (g *Game) flipper_preattack(self *Edict) {
	s := monsterStatics[flipperStatics](g, "m_flipper")
	g.gi.Sound(self, CHAN_WEAPON, s.SoundChomp, 1, ATTN_NORM, 0)
}

// C: game/m_flipper.c:193 flipper_frames_attack
var flipper_frames_attack = []MFrame{
	{ai_charge, 0, flipper_preattack},
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
	{ai_charge, 0, nil},
	{ai_charge, 0, flipper_bite},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, flipper_bite},
	{ai_charge, 0, nil},
}

// C: game/m_flipper.c:216 flipper_move_attack
var flipper_move_attack = defMMove("flipper_move_attack", flipper_FRAME_flpbit01, flipper_FRAME_flpbit20, flipper_frames_attack, flipper_run)

// C: game/m_flipper.c:218 flipper_melee
func (g *Game) flipper_melee(self *Edict) {
	self.Monsterinfo.Currentmove = flipper_move_attack
}

// C: game/m_flipper.c:223 flipper_pain
func (g *Game) flipper_pain(self *Edict, other *Edict, kick float32, damage int32) {
	s := monsterStatics[flipperStatics](g, "m_flipper")
	var n int32

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

	n = (g.rng.Rand() + 1) % 2
	if n == 0 {
		g.gi.Sound(self, CHAN_VOICE, s.SoundPain1, 1, ATTN_NORM, 0)
		self.Monsterinfo.Currentmove = flipper_move_pain1
	} else {
		g.gi.Sound(self, CHAN_VOICE, s.SoundPain2, 1, ATTN_NORM, 0)
		self.Monsterinfo.Currentmove = flipper_move_pain2
	}
}

// C: game/m_flipper.c:251 flipper_dead
func (g *Game) flipper_dead(self *Edict) {
	self.Mins = Vec3{-16, -16, -24}
	self.Maxs = Vec3{16, 16, -8}
	self.Movetype = MOVETYPE_TOSS
	self.SVFlags |= SVF_DEADMONSTER
	self.Nextthink = 0
	g.gi.LinkEntity(self)
}

// C: game/m_flipper.c:261 flipper_frames_death
var flipper_frames_death = []MFrame{
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
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
}

// C: game/m_flipper.c:325 flipper_move_death
var flipper_move_death = defMMove("flipper_move_death", flipper_FRAME_flpdth01, flipper_FRAME_flpdth56, flipper_frames_death, flipper_dead)

// C: game/m_flipper.c:327 flipper_sight
func (g *Game) flipper_sight(self *Edict, other *Edict) {
	s := monsterStatics[flipperStatics](g, "m_flipper")
	g.gi.Sound(self, CHAN_VOICE, s.SoundSight, 1, ATTN_NORM, 0)
}

// C: game/m_flipper.c:332 flipper_die
func (g *Game) flipper_die(self *Edict, inflictor *Edict, attacker *Edict, damage int32, point Vec3) {
	s := monsterStatics[flipperStatics](g, "m_flipper")
	var n int32

	// check for gib
	if self.Health <= self.GibHealth {
		g.gi.Sound(self, CHAN_VOICE, g.gi.SoundIndex("misc/udeath.wav"), 1, ATTN_NORM, 0)
		for n = 0; n < 2; n++ {
			g.ThrowGib(self, "models/objects/gibs/bone/tris.md2", damage, GIB_ORGANIC)
		}
		for n = 0; n < 2; n++ {
			g.ThrowGib(self, "models/objects/gibs/sm_meat/tris.md2", damage, GIB_ORGANIC)
		}
		g.ThrowHead(self, "models/objects/gibs/sm_meat/tris.md2", damage, GIB_ORGANIC)
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
	self.Monsterinfo.Currentmove = flipper_move_death
}

// QUAKED monster_flipper (1 .5 0) (-16 -16 -24) (16 16 32) Ambush Trigger_Spawn Sight
//
// C: game/m_flipper.c:361 SP_monster_flipper
func (g *Game) SP_monster_flipper(self *Edict) {
	s := monsterStatics[flipperStatics](g, "m_flipper")
	if g.deathmatch.Value != 0 {
		g.G_FreeEdict(self)
		return
	}

	s.SoundPain1 = g.gi.SoundIndex("flipper/flppain1.wav")
	s.SoundPain2 = g.gi.SoundIndex("flipper/flppain2.wav")
	s.SoundDeath = g.gi.SoundIndex("flipper/flpdeth1.wav")
	s.SoundChomp = g.gi.SoundIndex("flipper/flpatck1.wav")
	s.SoundAttack = g.gi.SoundIndex("flipper/flpatck2.wav")
	s.SoundIdle = g.gi.SoundIndex("flipper/flpidle1.wav")
	s.SoundSearch = g.gi.SoundIndex("flipper/flpsrch1.wav")
	s.SoundSight = g.gi.SoundIndex("flipper/flpsght1.wav")

	self.Movetype = MOVETYPE_STEP
	self.Solid = SOLID_BBOX
	self.S.ModelIndex = int32(g.gi.ModelIndex("models/monsters/flipper/tris.md2"))
	self.Mins = Vec3{-16, -16, 0}
	self.Maxs = Vec3{16, 16, 32}

	self.Health = 50
	self.GibHealth = -30
	self.Mass = 100

	self.Pain = flipper_pain
	self.Die = flipper_die

	self.Monsterinfo.Stand = flipper_stand
	self.Monsterinfo.Walk = flipper_walk
	self.Monsterinfo.Run = flipper_start_run
	self.Monsterinfo.Melee = flipper_melee
	self.Monsterinfo.Sight = flipper_sight

	g.gi.LinkEntity(self)

	self.Monsterinfo.Currentmove = flipper_move_stand
	self.Monsterinfo.Scale = flipper_MODEL_SCALE

	g.swimmonster_start(self)
}
