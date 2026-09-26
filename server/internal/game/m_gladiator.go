package game

// Port of game/m_gladiator.c.

import (
	. "quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/shared"
)

// statics of m_gladiator.c
type gladiatorStatics struct {
	SoundPain1        int // sound_pain1
	SoundPain2        int // sound_pain2
	SoundDie          int // sound_die
	SoundGun          int // sound_gun
	SoundCleaverSwing int // sound_cleaver_swing
	SoundCleaverHit   int // sound_cleaver_hit
	SoundCleaverMiss  int // sound_cleaver_miss
	SoundIdle         int // sound_idle
	SoundSearch       int // sound_search
	SoundSight        int // sound_sight
}

var (
	gladiator_idle          = defThink("gladiator_idle")
	gladiator_sight         = defBlocked("gladiator_sight")
	gladiator_search        = defThink("gladiator_search")
	gladiator_cleaver_swing = defThink("gladiator_cleaver_swing")
	gladiator_stand         = defThink("gladiator_stand")
	gladiator_walk          = defThink("gladiator_walk")
	gladiator_run           = defThink("gladiator_run")
	GaldiatorMelee          = defThink("GaldiatorMelee")
	gladiator_melee         = defThink("gladiator_melee")
	GladiatorGun            = defThink("GladiatorGun")
	gladiator_attack        = defThink("gladiator_attack")
	gladiator_pain          = defPain("gladiator_pain")
	gladiator_dead          = defThink("gladiator_dead")
	gladiator_die           = defDie("gladiator_die")
)

func init() {
	gladiator_idle.bind((*Game).gladiator_idle)
	gladiator_sight.bind((*Game).gladiator_sight)
	gladiator_search.bind((*Game).gladiator_search)
	gladiator_cleaver_swing.bind((*Game).gladiator_cleaver_swing)
	gladiator_stand.bind((*Game).gladiator_stand)
	gladiator_walk.bind((*Game).gladiator_walk)
	gladiator_run.bind((*Game).gladiator_run)
	GaldiatorMelee.bind((*Game).GaldiatorMelee)
	gladiator_melee.bind((*Game).gladiator_melee)
	GladiatorGun.bind((*Game).GladiatorGun)
	gladiator_attack.bind((*Game).gladiator_attack)
	gladiator_pain.bind((*Game).gladiator_pain)
	gladiator_dead.bind((*Game).gladiator_dead)
	gladiator_die.bind((*Game).gladiator_die)
}

func init() {
	RegisterMonsterStatics("m_gladiator", func() any { return new(gladiatorStatics) })
	RegisterSpawn("monster_gladiator", (*Game).SP_monster_gladiator)
}

// C: game/m_gladiator.c:64 gladiator_frames_stand
var gladiator_frames_stand = []MFrame{
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
}

// C: game/m_gladiator.c:82 gladiator_frames_walk
var gladiator_frames_walk = []MFrame{
	{ai_walk, 15, nil},
	{ai_walk, 7, nil},
	{ai_walk, 6, nil},
	{ai_walk, 5, nil},
	{ai_walk, 2, nil},
	{ai_walk, 0, nil},
	{ai_walk, 2, nil},
	{ai_walk, 8, nil},
	{ai_walk, 12, nil},
	{ai_walk, 8, nil},
	{ai_walk, 5, nil},
	{ai_walk, 5, nil},
	{ai_walk, 2, nil},
	{ai_walk, 2, nil},
	{ai_walk, 1, nil},
	{ai_walk, 8, nil},
}

// C: game/m_gladiator.c:109 gladiator_frames_run
var gladiator_frames_run = []MFrame{
	{ai_run, 23, nil},
	{ai_run, 14, nil},
	{ai_run, 14, nil},
	{ai_run, 21, nil},
	{ai_run, 12, nil},
	{ai_run, 13, nil},
}

// C: game/m_gladiator.c:140 gladiator_frames_attack_melee
var gladiator_frames_attack_melee = []MFrame{
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, gladiator_cleaver_swing},
	{ai_charge, 0, nil},
	{ai_charge, 0, GaldiatorMelee},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, gladiator_cleaver_swing},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, GaldiatorMelee},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
}

// C: game/m_gladiator.c:184 gladiator_frames_attack_gun
var gladiator_frames_attack_gun = []MFrame{
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, GladiatorGun},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
}

// C: game/m_gladiator.c:217 gladiator_frames_pain
var gladiator_frames_pain = []MFrame{
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
}

// C: game/m_gladiator.c:228 gladiator_frames_pain_air
var gladiator_frames_pain_air = []MFrame{
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
}

// C: game/m_gladiator.c:281 gladiator_frames_death
var gladiator_frames_death = []MFrame{
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

// C: game/m_gladiator.c:74 gladiator_move_stand
var gladiator_move_stand = defMMove("gladiator_move_stand", gladiator_FRAME_stand1, gladiator_FRAME_stand7, gladiator_frames_stand, nil)

// C: game/m_gladiator.c:101 gladiator_move_walk
var gladiator_move_walk = defMMove("gladiator_move_walk", gladiator_FRAME_walk1, gladiator_FRAME_walk16, gladiator_frames_walk, nil)

// C: game/m_gladiator.c:118 gladiator_move_run
var gladiator_move_run = defMMove("gladiator_move_run", gladiator_FRAME_run1, gladiator_FRAME_run6, gladiator_frames_run, nil)

// C: game/m_gladiator.c:160 gladiator_move_attack_melee
var gladiator_move_attack_melee = defMMove("gladiator_move_attack_melee", gladiator_FRAME_melee1, gladiator_FRAME_melee17, gladiator_frames_attack_melee, gladiator_run)

// C: game/m_gladiator.c:196 gladiator_move_attack_gun
var gladiator_move_attack_gun = defMMove("gladiator_move_attack_gun", gladiator_FRAME_attack1, gladiator_FRAME_attack9, gladiator_frames_attack_gun, gladiator_run)

// C: game/m_gladiator.c:226 gladiator_move_pain
var gladiator_move_pain = defMMove("gladiator_move_pain", gladiator_FRAME_pain1, gladiator_FRAME_pain6, gladiator_frames_pain, gladiator_run)

// C: game/m_gladiator.c:238 gladiator_move_pain_air
var gladiator_move_pain_air = defMMove("gladiator_move_pain_air", gladiator_FRAME_painup1, gladiator_FRAME_painup7, gladiator_frames_pain_air, gladiator_run)

// C: game/m_gladiator.c:306 gladiator_move_death
var gladiator_move_death = defMMove("gladiator_move_death", gladiator_FRAME_death1, gladiator_FRAME_death22, gladiator_frames_death, gladiator_dead)

// C: game/m_gladiator.c:44 gladiator_idle
func (g *Game) gladiator_idle(self *Edict) {
	s := monsterStatics[gladiatorStatics](g, "m_gladiator")
	g.gi.Sound(self, CHAN_VOICE, s.SoundIdle, 1, ATTN_IDLE, 0)
}

// C: game/m_gladiator.c:49 gladiator_sight
func (g *Game) gladiator_sight(self *Edict, other *Edict) {
	s := monsterStatics[gladiatorStatics](g, "m_gladiator")
	g.gi.Sound(self, CHAN_VOICE, s.SoundSight, 1, ATTN_NORM, 0)
}

// C: game/m_gladiator.c:54 gladiator_search
func (g *Game) gladiator_search(self *Edict) {
	s := monsterStatics[gladiatorStatics](g, "m_gladiator")
	g.gi.Sound(self, CHAN_VOICE, s.SoundSearch, 1, ATTN_NORM, 0)
}

// C: game/m_gladiator.c:59 gladiator_cleaver_swing
func (g *Game) gladiator_cleaver_swing(self *Edict) {
	s := monsterStatics[gladiatorStatics](g, "m_gladiator")
	g.gi.Sound(self, CHAN_WEAPON, s.SoundCleaverSwing, 1, ATTN_NORM, 0)
}

// C: game/m_gladiator.c:76 gladiator_stand
func (g *Game) gladiator_stand(self *Edict) {
	self.Monsterinfo.Currentmove = gladiator_move_stand
}

// C: game/m_gladiator.c:103 gladiator_walk
func (g *Game) gladiator_walk(self *Edict) {
	self.Monsterinfo.Currentmove = gladiator_move_walk
}

// C: game/m_gladiator.c:120 gladiator_run
func (g *Game) gladiator_run(self *Edict) {
	if self.Monsterinfo.Aiflags&AI_STAND_GROUND != 0 {
		self.Monsterinfo.Currentmove = gladiator_move_stand
	} else {
		self.Monsterinfo.Currentmove = gladiator_move_run
	}
}

// C: game/m_gladiator.c:129 GaldiatorMelee
func (g *Game) GaldiatorMelee(self *Edict) {
	s := monsterStatics[gladiatorStatics](g, "m_gladiator")
	aim := Vec3{MELEE_DISTANCE, self.Mins[0], -4}
	if g.fire_hit(self, aim, (20 + (g.rng.Rand() % 5)), 300) {
		g.gi.Sound(self, CHAN_AUTO, s.SoundCleaverHit, 1, ATTN_NORM, 0)
	} else {
		g.gi.Sound(self, CHAN_AUTO, s.SoundCleaverMiss, 1, ATTN_NORM, 0)
	}
}

// C: game/m_gladiator.c:162 gladiator_melee
func (g *Game) gladiator_melee(self *Edict) {
	self.Monsterinfo.Currentmove = gladiator_move_attack_melee
}

// C: game/m_gladiator.c:168 GladiatorGun
func (g *Game) GladiatorGun(self *Edict) {
	var forward, right Vec3

	shared.AngleVectors(self.S.Angles, &forward, &right, nil)
	start := G_ProjectSource(self.S.Origin, MonsterFlashOffset[MZ2_GLADIATOR_RAILGUN_1], forward, right)

	// calc direction to where we targted
	dir := shared.VectorSubtract(self.Pos1, start)
	shared.VectorNormalize(&dir)

	g.monster_fire_railgun(self, start, dir, 50, 100, MZ2_GLADIATOR_RAILGUN_1)
}

// C: game/m_gladiator.c:198 gladiator_attack
func (g *Game) gladiator_attack(self *Edict) {
	s := monsterStatics[gladiatorStatics](g, "m_gladiator")

	// a small safe zone
	v := shared.VectorSubtract(self.S.Origin, self.Enemy.S.Origin)
	rng := shared.VectorLength(v)
	if rng <= (MELEE_DISTANCE + 32) {
		return
	}

	// charge up the railgun
	g.gi.Sound(self, CHAN_WEAPON, s.SoundGun, 1, ATTN_NORM, 0)
	self.Pos1 = self.Enemy.S.Origin //save for aiming the shot
	self.Pos1[2] += float32(self.Enemy.Viewheight)
	self.Monsterinfo.Currentmove = gladiator_move_attack_gun
}

// C: game/m_gladiator.c:240 gladiator_pain
func (g *Game) gladiator_pain(self *Edict, other *Edict, kick float32, damage int32) {
	s := monsterStatics[gladiatorStatics](g, "m_gladiator")

	if self.Health < (self.MaxHealth / 2) {
		self.S.SkinNum = 1
	}

	if g.level.Time < self.PainDebounceTime {
		if (self.Velocity[2] > 100) && (self.Monsterinfo.Currentmove == gladiator_move_pain) {
			self.Monsterinfo.Currentmove = gladiator_move_pain_air
		}
		return
	}

	self.PainDebounceTime = g.level.Time + 3

	if g.random() < 0.5 {
		g.gi.Sound(self, CHAN_VOICE, s.SoundPain1, 1, ATTN_NORM, 0)
	} else {
		g.gi.Sound(self, CHAN_VOICE, s.SoundPain2, 1, ATTN_NORM, 0)
	}

	if g.skill.Value == 3 {
		return // no pain anims in nightmare
	}

	if self.Velocity[2] > 100 {
		self.Monsterinfo.Currentmove = gladiator_move_pain_air
	} else {
		self.Monsterinfo.Currentmove = gladiator_move_pain
	}
}

// C: game/m_gladiator.c:271 gladiator_dead
func (g *Game) gladiator_dead(self *Edict) {
	self.Mins = Vec3{-16, -16, -24}
	self.Maxs = Vec3{16, 16, -8}
	self.Movetype = MOVETYPE_TOSS
	self.SVFlags |= SVF_DEADMONSTER
	self.Nextthink = 0
	g.gi.LinkEntity(self)
}

// C: game/m_gladiator.c:308 gladiator_die
func (g *Game) gladiator_die(self *Edict, inflictor *Edict, attacker *Edict, damage int32, point Vec3) {
	s := monsterStatics[gladiatorStatics](g, "m_gladiator")
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
	g.gi.Sound(self, CHAN_VOICE, s.SoundDie, 1, ATTN_NORM, 0)
	self.Deadflag = DEAD_DEAD
	self.Takedamage = DAMAGE_YES

	self.Monsterinfo.Currentmove = gladiator_move_death
}

// QUAKED monster_gladiator (1 .5 0) (-32 -32 -24) (32 32 64) Ambush Trigger_Spawn Sight
//
// C: game/m_gladiator.c:339 SP_monster_gladiator
func (g *Game) SP_monster_gladiator(self *Edict) {
	s := monsterStatics[gladiatorStatics](g, "m_gladiator")
	if g.deathmatch.Value != 0 {
		g.G_FreeEdict(self)
		return
	}

	s.SoundPain1 = g.gi.SoundIndex("gladiator/pain.wav")
	s.SoundPain2 = g.gi.SoundIndex("gladiator/gldpain2.wav")
	s.SoundDie = g.gi.SoundIndex("gladiator/glddeth2.wav")
	s.SoundGun = g.gi.SoundIndex("gladiator/railgun.wav")
	s.SoundCleaverSwing = g.gi.SoundIndex("gladiator/melee1.wav")
	s.SoundCleaverHit = g.gi.SoundIndex("gladiator/melee2.wav")
	s.SoundCleaverMiss = g.gi.SoundIndex("gladiator/melee3.wav")
	s.SoundIdle = g.gi.SoundIndex("gladiator/gldidle1.wav")
	s.SoundSearch = g.gi.SoundIndex("gladiator/gldsrch1.wav")
	s.SoundSight = g.gi.SoundIndex("gladiator/sight.wav")

	self.Movetype = MOVETYPE_STEP
	self.Solid = SOLID_BBOX
	self.S.ModelIndex = int32(g.gi.ModelIndex("models/monsters/gladiatr/tris.md2"))
	self.Mins = Vec3{-32, -32, -24}
	self.Maxs = Vec3{32, 32, 64}

	self.Health = 400
	self.GibHealth = -175
	self.Mass = 400

	self.Pain = gladiator_pain
	self.Die = gladiator_die

	self.Monsterinfo.Stand = gladiator_stand
	self.Monsterinfo.Walk = gladiator_walk
	self.Monsterinfo.Run = gladiator_run
	self.Monsterinfo.Dodge = nil
	self.Monsterinfo.Attack = gladiator_attack
	self.Monsterinfo.Melee = gladiator_melee
	self.Monsterinfo.Sight = gladiator_sight
	self.Monsterinfo.Idle = gladiator_idle
	self.Monsterinfo.Search = gladiator_search

	g.gi.LinkEntity(self)
	self.Monsterinfo.Currentmove = gladiator_move_stand
	self.Monsterinfo.Scale = gladiator_MODEL_SCALE

	g.walkmonster_start(self)
}
