package game

// Port of game/m_float.c.

import (
	. "quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/shared"
)

// statics of m_float.c
type floatStatics struct {
	SoundAttack2 int // sound_attack2
	SoundAttack3 int // sound_attack3
	SoundDeath1  int // sound_death1
	SoundIdle    int // sound_idle
	SoundPain1   int // sound_pain1
	SoundPain2   int // sound_pain2
	SoundSight   int // sound_sight
}

var (
	floater_sight        = defBlocked("floater_sight")
	floater_idle         = defThink("floater_idle")
	floater_fire_blaster = defThink("floater_fire_blaster")
	floater_stand        = defThink("floater_stand")
	floater_run          = defThink("floater_run")
	floater_walk         = defThink("floater_walk")
	floater_wham         = defThink("floater_wham")
	floater_zap          = defThink("floater_zap")
	floater_attack       = defThink("floater_attack")
	floater_melee        = defThink("floater_melee")
	floater_pain         = defPain("floater_pain")
	floater_dead         = defThink("floater_dead")
	floater_die          = defDie("floater_die")
)

func init() {
	floater_sight.bind((*Game).floater_sight)
	floater_idle.bind((*Game).floater_idle)
	floater_fire_blaster.bind((*Game).floater_fire_blaster)
	floater_stand.bind((*Game).floater_stand)
	floater_run.bind((*Game).floater_run)
	floater_walk.bind((*Game).floater_walk)
	floater_wham.bind((*Game).floater_wham)
	floater_zap.bind((*Game).floater_zap)
	floater_attack.bind((*Game).floater_attack)
	floater_melee.bind((*Game).floater_melee)
	floater_pain.bind((*Game).floater_pain)
	floater_dead.bind((*Game).floater_dead)
	floater_die.bind((*Game).floater_die)
}

func init() {
	RegisterMonsterStatics("m_float", func() any { return new(floatStatics) })
	RegisterSpawn("monster_floater", (*Game).SP_monster_floater)
}

// C: game/m_float.c:83 floater_frames_stand1
var floater_frames_stand1 = []MFrame{
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

// C: game/m_float.c:140 floater_frames_stand2
var floater_frames_stand2 = []MFrame{
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

// C: game/m_float.c:205 floater_frames_activate
var floater_frames_activate = []MFrame{
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

// C: game/m_float.c:240 floater_frames_attack1
var floater_frames_attack1 = []MFrame{
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, floater_fire_blaster},
	{ai_charge, 0, floater_fire_blaster},
	{ai_charge, 0, floater_fire_blaster},
	{ai_charge, 0, floater_fire_blaster},
	{ai_charge, 0, floater_fire_blaster},
	{ai_charge, 0, floater_fire_blaster},
	{ai_charge, 0, floater_fire_blaster},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
}

// C: game/m_float.c:259 floater_frames_attack2
var floater_frames_attack2 = []MFrame{
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
	{ai_charge, 0, floater_wham},
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
	{ai_charge, 0, nil},
}

// C: game/m_float.c:289 floater_frames_attack3
var floater_frames_attack3 = []MFrame{
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, floater_zap},
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
	{ai_charge, 0, nil},
}

// C: game/m_float.c:328 floater_frames_death
var floater_frames_death = []MFrame{
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

// C: game/m_float.c:346 floater_frames_pain1
var floater_frames_pain1 = []MFrame{
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
}

// C: game/m_float.c:358 floater_frames_pain2
var floater_frames_pain2 = []MFrame{
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
}

// C: game/m_float.c:371 floater_frames_pain3
var floater_frames_pain3 = []MFrame{
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

// C: game/m_float.c:388 floater_frames_walk
var floater_frames_walk = []MFrame{
	{ai_walk, 5, nil},
	{ai_walk, 5, nil},
	{ai_walk, 5, nil},
	{ai_walk, 5, nil},
	{ai_walk, 5, nil},
	{ai_walk, 5, nil},
	{ai_walk, 5, nil},
	{ai_walk, 5, nil},
	{ai_walk, 5, nil},
	{ai_walk, 5, nil},
	{ai_walk, 5, nil},
	{ai_walk, 5, nil},
	{ai_walk, 5, nil},
	{ai_walk, 5, nil},
	{ai_walk, 5, nil},
	{ai_walk, 5, nil},
	{ai_walk, 5, nil},
	{ai_walk, 5, nil},
	{ai_walk, 5, nil},
	{ai_walk, 5, nil},
	{ai_walk, 5, nil},
	{ai_walk, 5, nil},
	{ai_walk, 5, nil},
	{ai_walk, 5, nil},
	{ai_walk, 5, nil},
	{ai_walk, 5, nil},
	{ai_walk, 5, nil},
	{ai_walk, 5, nil},
	{ai_walk, 5, nil},
	{ai_walk, 5, nil},
	{ai_walk, 5, nil},
	{ai_walk, 5, nil},
	{ai_walk, 5, nil},
	{ai_walk, 5, nil},
	{ai_walk, 5, nil},
	{ai_walk, 5, nil},
	{ai_walk, 5, nil},
	{ai_walk, 5, nil},
	{ai_walk, 5, nil},
	{ai_walk, 5, nil},
	{ai_walk, 5, nil},
	{ai_walk, 5, nil},
	{ai_walk, 5, nil},
	{ai_walk, 5, nil},
	{ai_walk, 5, nil},
	{ai_walk, 5, nil},
	{ai_walk, 5, nil},
	{ai_walk, 5, nil},
	{ai_walk, 5, nil},
	{ai_walk, 5, nil},
	{ai_walk, 5, nil},
	{ai_walk, 5, nil},
}

// C: game/m_float.c:445 floater_frames_run
var floater_frames_run = []MFrame{
	{ai_run, 13, nil},
	{ai_run, 13, nil},
	{ai_run, 13, nil},
	{ai_run, 13, nil},
	{ai_run, 13, nil},
	{ai_run, 13, nil},
	{ai_run, 13, nil},
	{ai_run, 13, nil},
	{ai_run, 13, nil},
	{ai_run, 13, nil},
	{ai_run, 13, nil},
	{ai_run, 13, nil},
	{ai_run, 13, nil},
	{ai_run, 13, nil},
	{ai_run, 13, nil},
	{ai_run, 13, nil},
	{ai_run, 13, nil},
	{ai_run, 13, nil},
	{ai_run, 13, nil},
	{ai_run, 13, nil},
	{ai_run, 13, nil},
	{ai_run, 13, nil},
	{ai_run, 13, nil},
	{ai_run, 13, nil},
	{ai_run, 13, nil},
	{ai_run, 13, nil},
	{ai_run, 13, nil},
	{ai_run, 13, nil},
	{ai_run, 13, nil},
	{ai_run, 13, nil},
	{ai_run, 13, nil},
	{ai_run, 13, nil},
	{ai_run, 13, nil},
	{ai_run, 13, nil},
	{ai_run, 13, nil},
	{ai_run, 13, nil},
	{ai_run, 13, nil},
	{ai_run, 13, nil},
	{ai_run, 13, nil},
	{ai_run, 13, nil},
	{ai_run, 13, nil},
	{ai_run, 13, nil},
	{ai_run, 13, nil},
	{ai_run, 13, nil},
	{ai_run, 13, nil},
	{ai_run, 13, nil},
	{ai_run, 13, nil},
	{ai_run, 13, nil},
	{ai_run, 13, nil},
	{ai_run, 13, nil},
	{ai_run, 13, nil},
	{ai_run, 13, nil},
}

// C: game/m_float.c:138 floater_move_stand1
var floater_move_stand1 = defMMove("floater_move_stand1", float_FRAME_stand101, float_FRAME_stand152, floater_frames_stand1, nil)

// C: game/m_float.c:195 floater_move_stand2
var floater_move_stand2 = defMMove("floater_move_stand2", float_FRAME_stand201, float_FRAME_stand252, floater_frames_stand2, nil)

// C: game/m_float.c:238 floater_move_activate
//
// The C table has 30 frames for the 31 frames actvat01..actvat31 (the last
// frame would read past the array); floater_move_activate is never used, so
// the table is padded with an empty frame to satisfy defMMove.
var floater_move_activate = defMMove("floater_move_activate", float_FRAME_actvat01, float_FRAME_actvat31, append(floater_frames_activate[:len(floater_frames_activate):len(floater_frames_activate)], MFrame{}), nil)

// C: game/m_float.c:257 floater_move_attack1
var floater_move_attack1 = defMMove("floater_move_attack1", float_FRAME_attak101, float_FRAME_attak114, floater_frames_attack1, floater_run)

// C: game/m_float.c:287 floater_move_attack2
var floater_move_attack2 = defMMove("floater_move_attack2", float_FRAME_attak201, float_FRAME_attak225, floater_frames_attack2, floater_run)

// C: game/m_float.c:326 floater_move_attack3
var floater_move_attack3 = defMMove("floater_move_attack3", float_FRAME_attak301, float_FRAME_attak334, floater_frames_attack3, floater_run)

// C: game/m_float.c:344 floater_move_death
var floater_move_death = defMMove("floater_move_death", float_FRAME_death01, float_FRAME_death13, floater_frames_death, floater_dead)

// C: game/m_float.c:356 floater_move_pain1
var floater_move_pain1 = defMMove("floater_move_pain1", float_FRAME_pain101, float_FRAME_pain107, floater_frames_pain1, floater_run)

// C: game/m_float.c:369 floater_move_pain2
var floater_move_pain2 = defMMove("floater_move_pain2", float_FRAME_pain201, float_FRAME_pain208, floater_frames_pain2, floater_run)

// C: game/m_float.c:386 floater_move_pain3
var floater_move_pain3 = defMMove("floater_move_pain3", float_FRAME_pain301, float_FRAME_pain312, floater_frames_pain3, floater_run)

// C: game/m_float.c:443 floater_move_walk
var floater_move_walk = defMMove("floater_move_walk", float_FRAME_stand101, float_FRAME_stand152, floater_frames_walk, nil)

// C: game/m_float.c:500 floater_move_run
var floater_move_run = defMMove("floater_move_run", float_FRAME_stand101, float_FRAME_stand152, floater_frames_run, nil)

// C: game/m_float.c:41 floater_sight
func (g *Game) floater_sight(self *Edict, other *Edict) {
	s := monsterStatics[floatStatics](g, "m_float")
	g.gi.Sound(self, CHAN_VOICE, s.SoundSight, 1, ATTN_NORM, 0)
}

// C: game/m_float.c:46 floater_idle
func (g *Game) floater_idle(self *Edict) {
	s := monsterStatics[floatStatics](g, "m_float")
	g.gi.Sound(self, CHAN_VOICE, s.SoundIdle, 1, ATTN_IDLE, 0)
}

// C: game/m_float.c:60 floater_fire_blaster
func (g *Game) floater_fire_blaster(self *Edict) {
	var start Vec3
	var forward, right Vec3
	var end Vec3
	var dir Vec3
	var effect int32

	if (self.S.Frame == float_FRAME_attak104) || (self.S.Frame == float_FRAME_attak107) {
		effect = EF_HYPERBLASTER
	} else {
		effect = 0
	}
	shared.AngleVectors(self.S.Angles, &forward, &right, nil)
	start = G_ProjectSource(self.S.Origin, MonsterFlashOffset[MZ2_FLOAT_BLASTER_1], forward, right)

	end = self.Enemy.S.Origin
	end[2] += float32(self.Enemy.Viewheight)
	dir = shared.VectorSubtract(end, start)

	g.monster_fire_blaster(self, start, dir, 1, 1000, MZ2_FLOAT_BLASTER_1, effect)
}

// C: game/m_float.c:197 floater_stand
func (g *Game) floater_stand(self *Edict) {
	if g.random() <= 0.5 {
		self.Monsterinfo.Currentmove = floater_move_stand1
	} else {
		self.Monsterinfo.Currentmove = floater_move_stand2
	}
}

// C: game/m_float.c:502 floater_run
func (g *Game) floater_run(self *Edict) {
	if self.Monsterinfo.Aiflags&AI_STAND_GROUND != 0 {
		self.Monsterinfo.Currentmove = floater_move_stand1
	} else {
		self.Monsterinfo.Currentmove = floater_move_run
	}
}

// C: game/m_float.c:510 floater_walk
func (g *Game) floater_walk(self *Edict) {
	self.Monsterinfo.Currentmove = floater_move_walk
}

// C: game/m_float.c:515 floater_wham
func (g *Game) floater_wham(self *Edict) {
	s := monsterStatics[floatStatics](g, "m_float")
	aim := Vec3{MELEE_DISTANCE, 0, 0}
	g.gi.Sound(self, CHAN_WEAPON, s.SoundAttack3, 1, ATTN_NORM, 0)
	g.fire_hit(self, aim, 5+g.rng.Rand()%6, -50)
}

// C: game/m_float.c:522 floater_zap
func (g *Game) floater_zap(self *Edict) {
	s := monsterStatics[floatStatics](g, "m_float")
	var forward, right Vec3
	var origin Vec3
	var dir Vec3
	var offset Vec3

	dir = shared.VectorSubtract(self.Enemy.S.Origin, self.S.Origin)

	shared.AngleVectors(self.S.Angles, &forward, &right, nil)
	//FIXME use a flash and replace these two lines with the commented one
	offset = Vec3{18.5, -0.9, 10}
	origin = G_ProjectSource(self.S.Origin, offset, forward, right)
	//	G_ProjectSource (self->s.origin, monster_flash_offset[flash_number], forward, right, origin);

	g.gi.Sound(self, CHAN_WEAPON, s.SoundAttack2, 1, ATTN_NORM, 0)

	//FIXME use the flash, Luke
	g.gi.WriteByteC(svc_temp_entity)
	g.gi.WriteByteC(TE_SPLASH)
	g.gi.WriteByteC(32)
	g.gi.WritePosition(&origin)
	g.gi.WriteDir(&dir)
	g.gi.WriteByteC(1) //sparks
	g.gi.Multicast(&origin, MULTICAST_PVS)

	g.T_Damage(self.Enemy, self, self, &dir, self.Enemy.S.Origin, shared.Vec3Origin, 5+g.rng.Rand()%6, -10, DAMAGE_ENERGY, MOD_UNKNOWN)
}

// C: game/m_float.c:551 floater_attack
func (g *Game) floater_attack(self *Edict) {
	self.Monsterinfo.Currentmove = floater_move_attack1
}

// C: game/m_float.c:557 floater_melee
func (g *Game) floater_melee(self *Edict) {
	if g.random() < 0.5 {
		self.Monsterinfo.Currentmove = floater_move_attack3
	} else {
		self.Monsterinfo.Currentmove = floater_move_attack2
	}
}

// C: game/m_float.c:566 floater_pain
func (g *Game) floater_pain(self *Edict, other *Edict, kick float32, damage int32) {
	s := monsterStatics[floatStatics](g, "m_float")
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

	n = (g.rng.Rand() + 1) % 3
	if n == 0 {
		g.gi.Sound(self, CHAN_VOICE, s.SoundPain1, 1, ATTN_NORM, 0)
		self.Monsterinfo.Currentmove = floater_move_pain1
	} else {
		g.gi.Sound(self, CHAN_VOICE, s.SoundPain2, 1, ATTN_NORM, 0)
		self.Monsterinfo.Currentmove = floater_move_pain2
	}
}

// C: game/m_float.c:593 floater_dead
func (g *Game) floater_dead(self *Edict) {
	self.Mins = Vec3{-16, -16, -24}
	self.Maxs = Vec3{16, 16, -8}
	self.Movetype = MOVETYPE_TOSS
	self.SVFlags |= SVF_DEADMONSTER
	self.Nextthink = 0
	g.gi.LinkEntity(self)
}

// C: game/m_float.c:603 floater_die
func (g *Game) floater_die(self *Edict, inflictor *Edict, attacker *Edict, damage int32, point Vec3) {
	s := monsterStatics[floatStatics](g, "m_float")
	g.gi.Sound(self, CHAN_VOICE, s.SoundDeath1, 1, ATTN_NORM, 0)
	g.BecomeExplosion1(self)
}

// QUAKED monster_floater (1 .5 0) (-16 -16 -24) (16 16 32) Ambush Trigger_Spawn Sight
//
// C: game/m_float.c:611 SP_monster_floater
func (g *Game) SP_monster_floater(self *Edict) {
	s := monsterStatics[floatStatics](g, "m_float")
	if g.deathmatch.Value != 0 {
		g.G_FreeEdict(self)
		return
	}

	s.SoundAttack2 = g.gi.SoundIndex("floater/fltatck2.wav")
	s.SoundAttack3 = g.gi.SoundIndex("floater/fltatck3.wav")
	s.SoundDeath1 = g.gi.SoundIndex("floater/fltdeth1.wav")
	s.SoundIdle = g.gi.SoundIndex("floater/fltidle1.wav")
	s.SoundPain1 = g.gi.SoundIndex("floater/fltpain1.wav")
	s.SoundPain2 = g.gi.SoundIndex("floater/fltpain2.wav")
	s.SoundSight = g.gi.SoundIndex("floater/fltsght1.wav")

	g.gi.SoundIndex("floater/fltatck1.wav")

	self.S.Sound = int32(g.gi.SoundIndex("floater/fltsrch1.wav"))

	self.Movetype = MOVETYPE_STEP
	self.Solid = SOLID_BBOX
	self.S.ModelIndex = int32(g.gi.ModelIndex("models/monsters/float/tris.md2"))
	self.Mins = Vec3{-24, -24, -24}
	self.Maxs = Vec3{24, 24, 32}

	self.Health = 200
	self.GibHealth = -80
	self.Mass = 300

	self.Pain = floater_pain
	self.Die = floater_die

	self.Monsterinfo.Stand = floater_stand
	self.Monsterinfo.Walk = floater_walk
	self.Monsterinfo.Run = floater_run
	//	self->monsterinfo.dodge = floater_dodge;
	self.Monsterinfo.Attack = floater_attack
	self.Monsterinfo.Melee = floater_melee
	self.Monsterinfo.Sight = floater_sight
	self.Monsterinfo.Idle = floater_idle

	g.gi.LinkEntity(self)

	if g.random() <= 0.5 {
		self.Monsterinfo.Currentmove = floater_move_stand1
	} else {
		self.Monsterinfo.Currentmove = floater_move_stand2
	}

	self.Monsterinfo.Scale = float_MODEL_SCALE

	g.flymonster_start(self)
}
