package game

// Port of game/m_flyer.c.

import (
	. "quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/shared"
)

// statics of m_flyer.c
type flyerStatics struct {
	Nextmove     int32 // nextmove
	SoundSight   int   // sound_sight
	SoundIdle    int   // sound_idle
	SoundPain1   int   // sound_pain1
	SoundPain2   int   // sound_pain2
	SoundSlash   int   // sound_slash
	SoundSproing int   // sound_sproing
	SoundDie     int   // sound_die
}

var (
	flyer_sight       = defBlocked("flyer_sight")
	flyer_idle        = defThink("flyer_idle")
	flyer_pop_blades  = defThink("flyer_pop_blades")
	flyer_run         = defThink("flyer_run")
	flyer_walk        = defThink("flyer_walk")
	flyer_stand       = defThink("flyer_stand")
	flyer_fireleft    = defThink("flyer_fireleft")
	flyer_fireright   = defThink("flyer_fireright")
	flyer_slash_left  = defThink("flyer_slash_left")
	flyer_slash_right = defThink("flyer_slash_right")
	flyer_loop_melee  = defThink("flyer_loop_melee")
	flyer_attack      = defThink("flyer_attack")
	flyer_nextmove    = defThink("flyer_nextmove")
	flyer_melee       = defThink("flyer_melee")
	flyer_check_melee = defThink("flyer_check_melee")
	flyer_pain        = defPain("flyer_pain")
	flyer_die         = defDie("flyer_die")
)

func init() {
	flyer_sight.bind((*Game).flyer_sight)
	flyer_idle.bind((*Game).flyer_idle)
	flyer_pop_blades.bind((*Game).flyer_pop_blades)
	flyer_run.bind((*Game).flyer_run)
	flyer_walk.bind((*Game).flyer_walk)
	flyer_stand.bind((*Game).flyer_stand)
	flyer_fireleft.bind((*Game).flyer_fireleft)
	flyer_fireright.bind((*Game).flyer_fireright)
	flyer_slash_left.bind((*Game).flyer_slash_left)
	flyer_slash_right.bind((*Game).flyer_slash_right)
	flyer_loop_melee.bind((*Game).flyer_loop_melee)
	flyer_attack.bind((*Game).flyer_attack)
	flyer_nextmove.bind((*Game).flyer_nextmove)
	flyer_melee.bind((*Game).flyer_melee)
	flyer_check_melee.bind((*Game).flyer_check_melee)
	flyer_pain.bind((*Game).flyer_pain)
	flyer_die.bind((*Game).flyer_die)
}

func init() {
	RegisterMonsterStatics("m_flyer", func() any { return new(flyerStatics) })
	RegisterSpawn("monster_flyer", (*Game).SP_monster_flyer)
}

// ==============================================================================
//
// flyer
//
// ==============================================================================
//
// Used for start/stop frames
// C: game/m_flyer.c:52 flyer_sight
func (g *Game) flyer_sight(self *Edict, other *Edict) {
	s := monsterStatics[flyerStatics](g, "m_flyer")
	g.gi.Sound(self, CHAN_VOICE, s.SoundSight, 1, ATTN_NORM, 0)
}

// C: game/m_flyer.c:57 flyer_idle
func (g *Game) flyer_idle(self *Edict) {
	s := monsterStatics[flyerStatics](g, "m_flyer")
	g.gi.Sound(self, CHAN_VOICE, s.SoundIdle, 1, ATTN_IDLE, 0)
}

// C: game/m_flyer.c:62 flyer_pop_blades
func (g *Game) flyer_pop_blades(self *Edict) {
	s := monsterStatics[flyerStatics](g, "m_flyer")
	g.gi.Sound(self, CHAN_VOICE, s.SoundSproing, 1, ATTN_NORM, 0)
}

// C: game/m_flyer.c:68 flyer_frames_stand
var flyer_frames_stand = []MFrame{
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
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

// C: game/m_flyer.c:116 flyer_move_stand
var flyer_move_stand = defMMove("flyer_move_stand", flyer_FRAME_stand01, flyer_FRAME_stand45, flyer_frames_stand, nil)

// C: game/m_flyer.c:119 flyer_frames_walk
var flyer_frames_walk = []MFrame{
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

// C: game/m_flyer.c:167 flyer_move_walk
var flyer_move_walk = defMMove("flyer_move_walk", flyer_FRAME_stand01, flyer_FRAME_stand45, flyer_frames_walk, nil)

// C: game/m_flyer.c:169 flyer_frames_run
var flyer_frames_run = []MFrame{
	{ai_run, 10, nil},
	{ai_run, 10, nil},
	{ai_run, 10, nil},
	{ai_run, 10, nil},
	{ai_run, 10, nil},
	{ai_run, 10, nil},
	{ai_run, 10, nil},
	{ai_run, 10, nil},
	{ai_run, 10, nil},
	{ai_run, 10, nil},
	{ai_run, 10, nil},
	{ai_run, 10, nil},
	{ai_run, 10, nil},
	{ai_run, 10, nil},
	{ai_run, 10, nil},
	{ai_run, 10, nil},
	{ai_run, 10, nil},
	{ai_run, 10, nil},
	{ai_run, 10, nil},
	{ai_run, 10, nil},
	{ai_run, 10, nil},
	{ai_run, 10, nil},
	{ai_run, 10, nil},
	{ai_run, 10, nil},
	{ai_run, 10, nil},
	{ai_run, 10, nil},
	{ai_run, 10, nil},
	{ai_run, 10, nil},
	{ai_run, 10, nil},
	{ai_run, 10, nil},
	{ai_run, 10, nil},
	{ai_run, 10, nil},
	{ai_run, 10, nil},
	{ai_run, 10, nil},
	{ai_run, 10, nil},
	{ai_run, 10, nil},
	{ai_run, 10, nil},
	{ai_run, 10, nil},
	{ai_run, 10, nil},
	{ai_run, 10, nil},
	{ai_run, 10, nil},
	{ai_run, 10, nil},
	{ai_run, 10, nil},
	{ai_run, 10, nil},
	{ai_run, 10, nil},
}

// C: game/m_flyer.c:217 flyer_move_run
var flyer_move_run = defMMove("flyer_move_run", flyer_FRAME_stand01, flyer_FRAME_stand45, flyer_frames_run, nil)

// C: game/m_flyer.c:219 flyer_run
func (g *Game) flyer_run(self *Edict) {
	if self.Monsterinfo.Aiflags&AI_STAND_GROUND != 0 {
		self.Monsterinfo.Currentmove = flyer_move_stand
	} else {
		self.Monsterinfo.Currentmove = flyer_move_run
	}
}

// C: game/m_flyer.c:227 flyer_walk
func (g *Game) flyer_walk(self *Edict) {
	self.Monsterinfo.Currentmove = flyer_move_walk
}

// C: game/m_flyer.c:232 flyer_stand
func (g *Game) flyer_stand(self *Edict) {
	self.Monsterinfo.Currentmove = flyer_move_stand
}

// C: game/m_flyer.c:237 flyer_frames_start
var flyer_frames_start = []MFrame{
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, flyer_nextmove},
}

// C: game/m_flyer.c:246 flyer_move_start
var flyer_move_start = defMMove("flyer_move_start", flyer_FRAME_start01, flyer_FRAME_start06, flyer_frames_start, nil)

// C: game/m_flyer.c:248 flyer_frames_stop
var flyer_frames_stop = []MFrame{
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, flyer_nextmove},
}

// C: game/m_flyer.c:258 flyer_move_stop
var flyer_move_stop = defMMove("flyer_move_stop", flyer_FRAME_stop01, flyer_FRAME_stop07, flyer_frames_stop, nil)

// C: game/m_flyer.c:260 flyer_stop
func (g *Game) flyer_stop(self *Edict) {
	self.Monsterinfo.Currentmove = flyer_move_stop
}

// C: game/m_flyer.c:265 flyer_start
func (g *Game) flyer_start(self *Edict) {
	self.Monsterinfo.Currentmove = flyer_move_start
}

// C: game/m_flyer.c:271 flyer_frames_rollright
var flyer_frames_rollright = []MFrame{
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

// C: game/m_flyer.c:283 flyer_move_rollright
var flyer_move_rollright = defMMove("flyer_move_rollright", flyer_FRAME_rollr01, flyer_FRAME_rollr09, flyer_frames_rollright, nil)

// C: game/m_flyer.c:285 flyer_frames_rollleft
var flyer_frames_rollleft = []MFrame{
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

// C: game/m_flyer.c:297 flyer_move_rollleft
var flyer_move_rollleft = defMMove("flyer_move_rollleft", flyer_FRAME_rollf01, flyer_FRAME_rollf09, flyer_frames_rollleft, nil)

// C: game/m_flyer.c:299 flyer_frames_pain3
var flyer_frames_pain3 = []MFrame{
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
}

// C: game/m_flyer.c:306 flyer_move_pain3
var flyer_move_pain3 = defMMove("flyer_move_pain3", flyer_FRAME_pain301, flyer_FRAME_pain304, flyer_frames_pain3, flyer_run)

// C: game/m_flyer.c:308 flyer_frames_pain2
var flyer_frames_pain2 = []MFrame{
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
}

// C: game/m_flyer.c:315 flyer_move_pain2
var flyer_move_pain2 = defMMove("flyer_move_pain2", flyer_FRAME_pain201, flyer_FRAME_pain204, flyer_frames_pain2, flyer_run)

// C: game/m_flyer.c:317 flyer_frames_pain1
var flyer_frames_pain1 = []MFrame{
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

// C: game/m_flyer.c:329 flyer_move_pain1
var flyer_move_pain1 = defMMove("flyer_move_pain1", flyer_FRAME_pain101, flyer_FRAME_pain109, flyer_frames_pain1, flyer_run)

// C: game/m_flyer.c:331 flyer_frames_defense
var flyer_frames_defense = []MFrame{
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
}

// C: game/m_flyer.c:340 flyer_move_defense
var flyer_move_defense = defMMove("flyer_move_defense", flyer_FRAME_defens01, flyer_FRAME_defens06, flyer_frames_defense, nil)

// C: game/m_flyer.c:342 flyer_frames_bankright
var flyer_frames_bankright = []MFrame{
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
}

// C: game/m_flyer.c:352 flyer_move_bankright
var flyer_move_bankright = defMMove("flyer_move_bankright", flyer_FRAME_bankr01, flyer_FRAME_bankr07, flyer_frames_bankright, nil)

// C: game/m_flyer.c:354 flyer_frames_bankleft
var flyer_frames_bankleft = []MFrame{
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
}

// C: game/m_flyer.c:364 flyer_move_bankleft
var flyer_move_bankleft = defMMove("flyer_move_bankleft", flyer_FRAME_bankl01, flyer_FRAME_bankl07, flyer_frames_bankleft, nil)

// C: game/m_flyer.c:367 flyer_fire
func (g *Game) flyer_fire(self *Edict, flash_number int32) {
	var start Vec3
	var forward, right Vec3
	var end Vec3
	var dir Vec3
	var effect int32

	if (self.S.Frame == flyer_FRAME_attak204) || (self.S.Frame == flyer_FRAME_attak207) || (self.S.Frame == flyer_FRAME_attak210) {
		effect = EF_HYPERBLASTER
	} else {
		effect = 0
	}
	shared.AngleVectors(self.S.Angles, &forward, &right, nil)
	start = G_ProjectSource(self.S.Origin, MonsterFlashOffset[flash_number], forward, right)

	end = self.Enemy.S.Origin
	end[2] += float32(self.Enemy.Viewheight)
	dir = shared.VectorSubtract(end, start)

	g.monster_fire_blaster(self, start, dir, 1, 1000, flash_number, effect)
}

// C: game/m_flyer.c:389 flyer_fireleft
func (g *Game) flyer_fireleft(self *Edict) {
	g.flyer_fire(self, MZ2_FLYER_BLASTER_1)
}

// C: game/m_flyer.c:394 flyer_fireright
func (g *Game) flyer_fireright(self *Edict) {
	g.flyer_fire(self, MZ2_FLYER_BLASTER_2)
}

// C: game/m_flyer.c:400 flyer_frames_attack2
var flyer_frames_attack2 = []MFrame{
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, -10, flyer_fireleft},
	{ai_charge, -10, flyer_fireright},
	{ai_charge, -10, flyer_fireleft},
	{ai_charge, -10, flyer_fireright},
	{ai_charge, -10, flyer_fireleft},
	{ai_charge, -10, flyer_fireright},
	{ai_charge, -10, flyer_fireleft},
	{ai_charge, -10, flyer_fireright},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
}

// C: game/m_flyer.c:420 flyer_move_attack2
var flyer_move_attack2 = defMMove("flyer_move_attack2", flyer_FRAME_attak201, flyer_FRAME_attak217, flyer_frames_attack2, flyer_run)

// C: game/m_flyer.c:423 flyer_slash_left
func (g *Game) flyer_slash_left(self *Edict) {
	s := monsterStatics[flyerStatics](g, "m_flyer")
	var aim Vec3

	aim = Vec3{MELEE_DISTANCE, self.Mins[0], 0}
	g.fire_hit(self, aim, 5, 0)
	g.gi.Sound(self, CHAN_WEAPON, s.SoundSlash, 1, ATTN_NORM, 0)
}

// C: game/m_flyer.c:432 flyer_slash_right
func (g *Game) flyer_slash_right(self *Edict) {
	s := monsterStatics[flyerStatics](g, "m_flyer")
	var aim Vec3

	aim = Vec3{MELEE_DISTANCE, self.Maxs[0], 0}
	g.fire_hit(self, aim, 5, 0)
	g.gi.Sound(self, CHAN_WEAPON, s.SoundSlash, 1, ATTN_NORM, 0)
}

// C: game/m_flyer.c:441 flyer_frames_start_melee
var flyer_frames_start_melee = []MFrame{
	{ai_charge, 0, flyer_pop_blades},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
}

// C: game/m_flyer.c:450 flyer_move_start_melee
var flyer_move_start_melee = defMMove("flyer_move_start_melee", flyer_FRAME_attak101, flyer_FRAME_attak106, flyer_frames_start_melee, flyer_loop_melee)

// C: game/m_flyer.c:452 flyer_frames_end_melee
var flyer_frames_end_melee = []MFrame{
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
}

// C: game/m_flyer.c:458 flyer_move_end_melee
var flyer_move_end_melee = defMMove("flyer_move_end_melee", flyer_FRAME_attak119, flyer_FRAME_attak121, flyer_frames_end_melee, flyer_run)

// C: game/m_flyer.c:461 flyer_frames_loop_melee
var flyer_frames_loop_melee = []MFrame{
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, flyer_slash_left},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, flyer_slash_right},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
}

// C: game/m_flyer.c:477 flyer_move_loop_melee
var flyer_move_loop_melee = defMMove("flyer_move_loop_melee", flyer_FRAME_attak107, flyer_FRAME_attak118, flyer_frames_loop_melee, flyer_check_melee)

// C: game/m_flyer.c:479 flyer_loop_melee
func (g *Game) flyer_loop_melee(self *Edict) {
	/*	if (random() <= 0.5)
		self->monsterinfo.currentmove = &flyer_move_attack1;
	else */
	self.Monsterinfo.Currentmove = flyer_move_loop_melee
}

// C: game/m_flyer.c:489 flyer_attack
func (g *Game) flyer_attack(self *Edict) {
	/*	if (random() <= 0.5)
		self->monsterinfo.currentmove = &flyer_move_attack1;
	else */
	self.Monsterinfo.Currentmove = flyer_move_attack2
}

// C: game/m_flyer.c:497 flyer_setstart
func (g *Game) flyer_setstart(self *Edict) {
	s := monsterStatics[flyerStatics](g, "m_flyer")
	s.Nextmove = flyer_ACTION_run
	self.Monsterinfo.Currentmove = flyer_move_start
}

// C: game/m_flyer.c:503 flyer_nextmove
func (g *Game) flyer_nextmove(self *Edict) {
	s := monsterStatics[flyerStatics](g, "m_flyer")
	if s.Nextmove == flyer_ACTION_attack1 {
		self.Monsterinfo.Currentmove = flyer_move_start_melee
	} else if s.Nextmove == flyer_ACTION_attack2 {
		self.Monsterinfo.Currentmove = flyer_move_attack2
	} else if s.Nextmove == flyer_ACTION_run {
		self.Monsterinfo.Currentmove = flyer_move_run
	}
}

// C: game/m_flyer.c:513 flyer_melee
func (g *Game) flyer_melee(self *Edict) {
	//	flyer.nextmove = ACTION_attack1;
	//	self->monsterinfo.currentmove = &flyer_move_stop;
	self.Monsterinfo.Currentmove = flyer_move_start_melee
}

// C: game/m_flyer.c:520 flyer_check_melee
func (g *Game) flyer_check_melee(self *Edict) {
	if g.range_(self, self.Enemy) == RANGE_MELEE {
		if float64(g.random()) <= 0.8 {
			self.Monsterinfo.Currentmove = flyer_move_loop_melee
		} else {
			self.Monsterinfo.Currentmove = flyer_move_end_melee
		}
	} else {
		self.Monsterinfo.Currentmove = flyer_move_end_melee
	}
}

// C: game/m_flyer.c:531 flyer_pain
func (g *Game) flyer_pain(self *Edict, other *Edict, kick float32, damage int32) {
	s := monsterStatics[flyerStatics](g, "m_flyer")
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

	n = g.rng.Rand() % 3
	if n == 0 {
		g.gi.Sound(self, CHAN_VOICE, s.SoundPain1, 1, ATTN_NORM, 0)
		self.Monsterinfo.Currentmove = flyer_move_pain1
	} else if n == 1 {
		g.gi.Sound(self, CHAN_VOICE, s.SoundPain2, 1, ATTN_NORM, 0)
		self.Monsterinfo.Currentmove = flyer_move_pain2
	} else {
		g.gi.Sound(self, CHAN_VOICE, s.SoundPain1, 1, ATTN_NORM, 0)
		self.Monsterinfo.Currentmove = flyer_move_pain3
	}
}

// C: game/m_flyer.c:564 flyer_die
func (g *Game) flyer_die(self *Edict, inflictor *Edict, attacker *Edict, damage int32, point Vec3) {
	s := monsterStatics[flyerStatics](g, "m_flyer")
	g.gi.Sound(self, CHAN_VOICE, s.SoundDie, 1, ATTN_NORM, 0)
	g.BecomeExplosion1(self)
}

// QUAKED monster_flyer (1 .5 0) (-16 -16 -24) (16 16 32) Ambush Trigger_Spawn Sight
//
// C: game/m_flyer.c:573 SP_monster_flyer
func (g *Game) SP_monster_flyer(self *Edict) {
	s := monsterStatics[flyerStatics](g, "m_flyer")
	if g.deathmatch.Value != 0 {
		g.G_FreeEdict(self)
		return
	}

	// fix a map bug in jail5.bsp
	if shared.Q_stricmp(g.level.Mapname, "jail5") == 0 && (self.S.Origin[2] == -104) {
		self.Targetname = self.Target
		self.Target = ""
	}

	s.SoundSight = g.gi.SoundIndex("flyer/flysght1.wav")
	s.SoundIdle = g.gi.SoundIndex("flyer/flysrch1.wav")
	s.SoundPain1 = g.gi.SoundIndex("flyer/flypain1.wav")
	s.SoundPain2 = g.gi.SoundIndex("flyer/flypain2.wav")
	s.SoundSlash = g.gi.SoundIndex("flyer/flyatck2.wav")
	s.SoundSproing = g.gi.SoundIndex("flyer/flyatck1.wav")
	s.SoundDie = g.gi.SoundIndex("flyer/flydeth1.wav")

	g.gi.SoundIndex("flyer/flyatck3.wav")

	self.S.ModelIndex = int32(g.gi.ModelIndex("models/monsters/flyer/tris.md2"))
	self.Mins = Vec3{-16, -16, -24}
	self.Maxs = Vec3{16, 16, 32}
	self.Movetype = MOVETYPE_STEP
	self.Solid = SOLID_BBOX

	self.S.Sound = int32(g.gi.SoundIndex("flyer/flyidle1.wav"))

	self.Health = 50
	self.Mass = 50

	self.Pain = flyer_pain
	self.Die = flyer_die

	self.Monsterinfo.Stand = flyer_stand
	self.Monsterinfo.Walk = flyer_walk
	self.Monsterinfo.Run = flyer_run
	self.Monsterinfo.Attack = flyer_attack
	self.Monsterinfo.Melee = flyer_melee
	self.Monsterinfo.Sight = flyer_sight
	self.Monsterinfo.Idle = flyer_idle

	g.gi.LinkEntity(self)

	self.Monsterinfo.Currentmove = flyer_move_stand
	self.Monsterinfo.Scale = flyer_MODEL_SCALE

	g.flymonster_start(self)
}
