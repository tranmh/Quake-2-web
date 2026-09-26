package game

// Port of game/m_insane.c.

import (
	"fmt"

	. "quake2web/server/internal/q2const"
)

// statics of m_insane.c
type insaneStatics struct {
	SoundFist   int    // sound_fist
	SoundShake  int    // sound_shake
	SoundMoan   int    // sound_moan
	SoundScream [8]int // sound_scream
}

var (
	insane_fist      = defThink("insane_fist")
	insane_shake     = defThink("insane_shake")
	insane_moan      = defThink("insane_moan")
	insane_scream    = defThink("insane_scream")
	insane_cross     = defThink("insane_cross")
	insane_walk      = defThink("insane_walk")
	insane_run       = defThink("insane_run")
	insane_pain      = defPain("insane_pain")
	insane_onground  = defThink("insane_onground")
	insane_checkdown = defThink("insane_checkdown")
	insane_checkup   = defThink("insane_checkup")
	insane_stand     = defThink("insane_stand")
	insane_dead      = defThink("insane_dead")
	insane_die       = defDie("insane_die")
)

func init() {
	insane_fist.bind((*Game).insane_fist)
	insane_shake.bind((*Game).insane_shake)
	insane_moan.bind((*Game).insane_moan)
	insane_scream.bind((*Game).insane_scream)
	insane_cross.bind((*Game).insane_cross)
	insane_walk.bind((*Game).insane_walk)
	insane_run.bind((*Game).insane_run)
	insane_pain.bind((*Game).insane_pain)
	insane_onground.bind((*Game).insane_onground)
	insane_checkdown.bind((*Game).insane_checkdown)
	insane_checkup.bind((*Game).insane_checkup)
	insane_stand.bind((*Game).insane_stand)
	insane_dead.bind((*Game).insane_dead)
	insane_die.bind((*Game).insane_die)
}

func init() {
	RegisterMonsterStatics("m_insane", func() any { return new(insaneStatics) })
	RegisterSpawn("misc_insane", (*Game).SP_misc_insane)
}

// C: game/m_insane.c:37 insane_fist
func (g *Game) insane_fist(self *Edict) {
	s := monsterStatics[insaneStatics](g, "m_insane")
	g.gi.Sound(self, CHAN_VOICE, s.SoundFist, 1, ATTN_IDLE, 0)
}

// C: game/m_insane.c:42 insane_shake
func (g *Game) insane_shake(self *Edict) {
	s := monsterStatics[insaneStatics](g, "m_insane")
	g.gi.Sound(self, CHAN_VOICE, s.SoundShake, 1, ATTN_IDLE, 0)
}

// C: game/m_insane.c:47 insane_moan
func (g *Game) insane_moan(self *Edict) {
	s := monsterStatics[insaneStatics](g, "m_insane")
	g.gi.Sound(self, CHAN_VOICE, s.SoundMoan, 1, ATTN_IDLE, 0)
}

// C: game/m_insane.c:52 insane_scream
func (g *Game) insane_scream(self *Edict) {
	s := monsterStatics[insaneStatics](g, "m_insane")
	g.gi.Sound(self, CHAN_VOICE, s.SoundScream[g.rng.Rand()%8], 1, ATTN_IDLE, 0)
}

// C: game/m_insane.c:68 insane_frames_stand_normal
var insane_frames_stand_normal = []MFrame{
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, insane_checkdown},
}

// C: game/m_insane.c:79 insane_frames_stand_insane
var insane_frames_stand_insane = []MFrame{
	{ai_stand, 0, insane_shake},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, insane_checkdown},
}

// C: game/m_insane.c:114 insane_frames_uptodown
var insane_frames_uptodown = []MFrame{
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, insane_moan},
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
	{ai_move, 2.7, nil},
	{ai_move, 4.1, nil},
	{ai_move, 6, nil},
	{ai_move, 7.6, nil},
	{ai_move, 3.6, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, insane_fist},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, insane_fist},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
}

// C: game/m_insane.c:163 insane_frames_downtoup
var insane_frames_downtoup = []MFrame{
	{ai_move, -0.7, nil},
	{ai_move, -1.2, nil},
	{ai_move, -1.5, nil},
	{ai_move, -4.5, nil},
	{ai_move, -3.5, nil},
	{ai_move, -0.2, nil},
	{ai_move, 0, nil},
	{ai_move, -1.3, nil},
	{ai_move, -3, nil},
	{ai_move, -2, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, -3.3, nil},
	{ai_move, -1.6, nil},
	{ai_move, -0.3, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
}

// C: game/m_insane.c:187 insane_frames_jumpdown
var insane_frames_jumpdown = []MFrame{
	{ai_move, 0.2, nil},
	{ai_move, 11.5, nil},
	{ai_move, 5.1, nil},
	{ai_move, 7.1, nil},
	{ai_move, 0, nil},
}

// C: game/m_insane.c:198 insane_frames_down
var insane_frames_down = []MFrame{
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
	{ai_move, -1.7, nil},
	{ai_move, -1.6, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, insane_fist},
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
	{ai_move, 0, insane_moan},
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
	{ai_move, 0.5, nil},
	{ai_move, 0, nil},
	{ai_move, -0.2, insane_scream},
	{ai_move, 0, nil},
	{ai_move, 0.2, nil},
	{ai_move, 0.4, nil},
	{ai_move, 0.6, nil},
	{ai_move, 0.8, nil},
	{ai_move, 0.7, nil},
	{ai_move, 0, insane_checkup},
}

// C: game/m_insane.c:264 insane_frames_walk_normal
var insane_frames_walk_normal = []MFrame{
	{ai_walk, 0, insane_scream},
	{ai_walk, 2.5, nil},
	{ai_walk, 3.5, nil},
	{ai_walk, 1.7, nil},
	{ai_walk, 2.3, nil},
	{ai_walk, 2.4, nil},
	{ai_walk, 2.2, nil},
	{ai_walk, 4.2, nil},
	{ai_walk, 5.6, nil},
	{ai_walk, 3.3, nil},
	{ai_walk, 2.4, nil},
	{ai_walk, 0.9, nil},
	{ai_walk, 0, nil},
}

// C: game/m_insane.c:283 insane_frames_walk_insane
var insane_frames_walk_insane = []MFrame{
	{ai_walk, 0, insane_scream},
	{ai_walk, 3.4, nil},
	{ai_walk, 3.6, nil},
	{ai_walk, 2.9, nil},
	{ai_walk, 2.2, nil},
	{ai_walk, 2.6, nil},
	{ai_walk, 0, nil},
	{ai_walk, 0.7, nil},
	{ai_walk, 4.8, nil},
	{ai_walk, 5.3, nil},
	{ai_walk, 1.1, nil},
	{ai_walk, 2, nil},
	{ai_walk, 0.5, nil},
	{ai_walk, 0, nil},
	{ai_walk, 0, nil},
	{ai_walk, 4.9, nil},
	{ai_walk, 6.7, nil},
	{ai_walk, 3.8, nil},
	{ai_walk, 2, nil},
	{ai_walk, 0.2, nil},
	{ai_walk, 0, nil},
	{ai_walk, 3.4, nil},
	{ai_walk, 6.4, nil},
	{ai_walk, 5, nil},
	{ai_walk, 1.8, nil},
	{ai_walk, 0, nil},
}

// C: game/m_insane.c:315 insane_frames_stand_pain
var insane_frames_stand_pain = []MFrame{
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

// C: game/m_insane.c:331 insane_frames_stand_death
var insane_frames_stand_death = []MFrame{
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

// C: game/m_insane.c:353 insane_frames_crawl
var insane_frames_crawl = []MFrame{
	{ai_walk, 0, insane_scream},
	{ai_walk, 1.5, nil},
	{ai_walk, 2.1, nil},
	{ai_walk, 3.6, nil},
	{ai_walk, 2, nil},
	{ai_walk, 0.9, nil},
	{ai_walk, 3, nil},
	{ai_walk, 3.4, nil},
	{ai_walk, 2.4, nil},
}

// C: game/m_insane.c:368 insane_frames_crawl_pain
var insane_frames_crawl_pain = []MFrame{
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

// C: game/m_insane.c:382 insane_frames_crawl_death
var insane_frames_crawl_death = []MFrame{
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
}

// C: game/m_insane.c:394 insane_frames_cross
var insane_frames_cross = []MFrame{
	{ai_move, 0, insane_moan},
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

// C: game/m_insane.c:414 insane_frames_struggle_cross
var insane_frames_struggle_cross = []MFrame{
	{ai_move, 0, insane_scream},
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

// C: game/m_insane.c:77 insane_move_stand_normal
var insane_move_stand_normal = defMMove("insane_move_stand_normal", insane_FRAME_stand60, insane_FRAME_stand65, insane_frames_stand_normal, insane_stand)

// C: game/m_insane.c:112 insane_move_stand_insane
var insane_move_stand_insane = defMMove("insane_move_stand_insane", insane_FRAME_stand65, insane_FRAME_stand94, insane_frames_stand_insane, insane_stand)

// C: game/m_insane.c:160 insane_move_uptodown
var insane_move_uptodown = defMMove("insane_move_uptodown", insane_FRAME_stand1, insane_FRAME_stand40, insane_frames_uptodown, insane_onground)

// C: game/m_insane.c:185 insane_move_downtoup
var insane_move_downtoup = defMMove("insane_move_downtoup", insane_FRAME_stand41, insane_FRAME_stand59, insane_frames_downtoup, insane_stand)

// C: game/m_insane.c:195 insane_move_jumpdown
var insane_move_jumpdown = defMMove("insane_move_jumpdown", insane_FRAME_stand96, insane_FRAME_stand100, insane_frames_jumpdown, insane_onground)

// C: game/m_insane.c:262 insane_move_down
var insane_move_down = defMMove("insane_move_down", insane_FRAME_stand100, insane_FRAME_stand160, insane_frames_down, insane_onground)

// C: game/m_insane.c:280 insane_move_walk_normal
var insane_move_walk_normal = defMMove("insane_move_walk_normal", insane_FRAME_walk27, insane_FRAME_walk39, insane_frames_walk_normal, insane_walk)

// C: game/m_insane.c:281 insane_move_run_normal
var insane_move_run_normal = defMMove("insane_move_run_normal", insane_FRAME_walk27, insane_FRAME_walk39, insane_frames_walk_normal, insane_run)

// C: game/m_insane.c:312 insane_move_walk_insane
var insane_move_walk_insane = defMMove("insane_move_walk_insane", insane_FRAME_walk1, insane_FRAME_walk26, insane_frames_walk_insane, insane_walk)

// C: game/m_insane.c:313 insane_move_run_insane
var insane_move_run_insane = defMMove("insane_move_run_insane", insane_FRAME_walk1, insane_FRAME_walk26, insane_frames_walk_insane, insane_run)

// C: game/m_insane.c:329 insane_move_stand_pain
var insane_move_stand_pain = defMMove("insane_move_stand_pain", insane_FRAME_st_pain2, insane_FRAME_st_pain12, insane_frames_stand_pain, insane_run)

// C: game/m_insane.c:351 insane_move_stand_death
var insane_move_stand_death = defMMove("insane_move_stand_death", insane_FRAME_st_death2, insane_FRAME_st_death18, insane_frames_stand_death, insane_dead)

// C: game/m_insane.c:365 insane_move_crawl
var insane_move_crawl = defMMove("insane_move_crawl", insane_FRAME_crawl1, insane_FRAME_crawl9, insane_frames_crawl, nil)

// C: game/m_insane.c:366 insane_move_runcrawl
var insane_move_runcrawl = defMMove("insane_move_runcrawl", insane_FRAME_crawl1, insane_FRAME_crawl9, insane_frames_crawl, nil)

// C: game/m_insane.c:380 insane_move_crawl_pain
var insane_move_crawl_pain = defMMove("insane_move_crawl_pain", insane_FRAME_cr_pain2, insane_FRAME_cr_pain10, insane_frames_crawl_pain, insane_run)

// C: game/m_insane.c:392 insane_move_crawl_death
var insane_move_crawl_death = defMMove("insane_move_crawl_death", insane_FRAME_cr_death10, insane_FRAME_cr_death16, insane_frames_crawl_death, insane_dead)

// C: game/m_insane.c:412 insane_move_cross
var insane_move_cross = defMMove("insane_move_cross", insane_FRAME_cross1, insane_FRAME_cross15, insane_frames_cross, insane_cross)

// C: game/m_insane.c:432 insane_move_struggle_cross
var insane_move_struggle_cross = defMMove("insane_move_struggle_cross", insane_FRAME_cross16, insane_FRAME_cross30, insane_frames_struggle_cross, insane_cross)

// C: game/m_insane.c:434 insane_cross
func (g *Game) insane_cross(self *Edict) {
	if float64(g.random()) < 0.8 {
		self.Monsterinfo.Currentmove = insane_move_cross
	} else {
		self.Monsterinfo.Currentmove = insane_move_struggle_cross
	}
}

// C: game/m_insane.c:442 insane_walk
func (g *Game) insane_walk(self *Edict) {
	if self.Spawnflags&16 != 0 { // Hold Ground?
		if self.S.Frame == insane_FRAME_cr_pain10 {
			self.Monsterinfo.Currentmove = insane_move_down
			return
		}
	}
	if self.Spawnflags&4 != 0 {
		self.Monsterinfo.Currentmove = insane_move_crawl
	} else if float64(g.random()) <= 0.5 {
		self.Monsterinfo.Currentmove = insane_move_walk_normal
	} else {
		self.Monsterinfo.Currentmove = insane_move_walk_insane
	}
}

// C: game/m_insane.c:459 insane_run
func (g *Game) insane_run(self *Edict) {
	if self.Spawnflags&16 != 0 { // Hold Ground?
		if self.S.Frame == insane_FRAME_cr_pain10 {
			self.Monsterinfo.Currentmove = insane_move_down
			return
		}
	}
	if self.Spawnflags&4 != 0 { // Crawling?
		self.Monsterinfo.Currentmove = insane_move_runcrawl
	} else if float64(g.random()) <= 0.5 { // Else, mix it up
		self.Monsterinfo.Currentmove = insane_move_run_normal
	} else {
		self.Monsterinfo.Currentmove = insane_move_run_insane
	}
}

// C: game/m_insane.c:477 insane_pain
func (g *Game) insane_pain(self *Edict, other *Edict, kick float32, damage int32) {
	var l, r int32

	//	if (self->health < (self->max_health / 2))
	//		self->s.skinnum = 1;

	if g.level.Time < self.PainDebounceTime {
		return
	}

	self.PainDebounceTime = g.level.Time + 3

	r = 1 + (g.rng.Rand() & 1)
	if self.Health < 25 {
		l = 25
	} else if self.Health < 50 {
		l = 50
	} else if self.Health < 75 {
		l = 75
	} else {
		l = 100
	}
	g.gi.Sound(self, CHAN_VOICE, g.gi.SoundIndex(fmt.Sprintf("player/male/pain%d_%d.wav", l, r)), 1, ATTN_IDLE, 0)

	if g.skill.Value == 3 {
		return // no pain anims in nightmare
	}

	// Don't go into pain frames if crucified.
	if self.Spawnflags&8 != 0 {
		self.Monsterinfo.Currentmove = insane_move_struggle_cross
		return
	}

	if ((self.S.Frame >= insane_FRAME_crawl1) && (self.S.Frame <= insane_FRAME_crawl9)) || ((self.S.Frame >= insane_FRAME_stand99) && (self.S.Frame <= insane_FRAME_stand160)) {
		self.Monsterinfo.Currentmove = insane_move_crawl_pain
	} else {
		self.Monsterinfo.Currentmove = insane_move_stand_pain
	}
}

// C: game/m_insane.c:519 insane_onground
func (g *Game) insane_onground(self *Edict) {
	self.Monsterinfo.Currentmove = insane_move_down
}

// C: game/m_insane.c:524 insane_checkdown
func (g *Game) insane_checkdown(self *Edict) {
	//	if ( (self->s.frame == FRAME_stand94) || (self->s.frame == FRAME_stand65) )
	if self.Spawnflags&32 != 0 { // Always stand
		return
	}
	if float64(g.random()) < 0.3 {
		if float64(g.random()) < 0.5 {
			self.Monsterinfo.Currentmove = insane_move_uptodown
		} else {
			self.Monsterinfo.Currentmove = insane_move_jumpdown
		}
	}
}

// C: game/m_insane.c:536 insane_checkup
func (g *Game) insane_checkup(self *Edict) {
	// If Hold_Ground and Crawl are set
	if (self.Spawnflags&4 != 0) && (self.Spawnflags&16 != 0) {
		return
	}
	if float64(g.random()) < 0.5 {
		self.Monsterinfo.Currentmove = insane_move_downtoup
	}
}

// C: game/m_insane.c:546 insane_stand
func (g *Game) insane_stand(self *Edict) {
	if self.Spawnflags&8 != 0 { // If crucified
		self.Monsterinfo.Currentmove = insane_move_cross
		self.Monsterinfo.Aiflags |= AI_STAND_GROUND
	} else if (self.Spawnflags&4 != 0) && (self.Spawnflags&16 != 0) {
		// If Hold_Ground and Crawl are set
		self.Monsterinfo.Currentmove = insane_move_down
	} else if float64(g.random()) < 0.5 {
		self.Monsterinfo.Currentmove = insane_move_stand_normal
	} else {
		self.Monsterinfo.Currentmove = insane_move_stand_insane
	}
}

// C: game/m_insane.c:563 insane_dead
func (g *Game) insane_dead(self *Edict) {
	if self.Spawnflags&8 != 0 {
		self.Flags |= FL_FLY
	} else {
		self.Mins = Vec3{-16, -16, -24}
		self.Maxs = Vec3{16, 16, -8}
		self.Movetype = MOVETYPE_TOSS
	}
	self.SVFlags |= SVF_DEADMONSTER
	self.Nextthink = 0
	g.gi.LinkEntity(self)
}

// C: game/m_insane.c:581 insane_die
func (g *Game) insane_die(self *Edict, inflictor *Edict, attacker *Edict, damage int32, point Vec3) {
	var n int32

	if self.Health <= self.GibHealth {
		g.gi.Sound(self, CHAN_VOICE, g.gi.SoundIndex("misc/udeath.wav"), 1, ATTN_IDLE, 0)
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

	g.gi.Sound(self, CHAN_VOICE, g.gi.SoundIndex(fmt.Sprintf("player/male/death%d.wav", (g.rng.Rand()%4)+1)), 1, ATTN_IDLE, 0)

	self.Deadflag = DEAD_DEAD
	self.Takedamage = DAMAGE_YES

	if self.Spawnflags&8 != 0 {
		g.insane_dead(self)
	} else {
		if ((self.S.Frame >= insane_FRAME_crawl1) && (self.S.Frame <= insane_FRAME_crawl9)) || ((self.S.Frame >= insane_FRAME_stand99) && (self.S.Frame <= insane_FRAME_stand160)) {
			self.Monsterinfo.Currentmove = insane_move_crawl_death
		} else {
			self.Monsterinfo.Currentmove = insane_move_stand_death
		}
	}
}

// QUAKED misc_insane (1 .5 0) (-16 -16 -24) (16 16 32) Ambush Trigger_Spawn CRAWL CRUCIFIED STAND_GROUND ALWAYS_STAND
//
// C: game/m_insane.c:621 SP_misc_insane
func (g *Game) SP_misc_insane(self *Edict) {
	s := monsterStatics[insaneStatics](g, "m_insane")
	//	static int skin = 0;	//@@

	if g.deathmatch.Value != 0 {
		g.G_FreeEdict(self)
		return
	}

	s.SoundFist = g.gi.SoundIndex("insane/insane11.wav")
	s.SoundShake = g.gi.SoundIndex("insane/insane5.wav")
	s.SoundMoan = g.gi.SoundIndex("insane/insane7.wav")
	s.SoundScream[0] = g.gi.SoundIndex("insane/insane1.wav")
	s.SoundScream[1] = g.gi.SoundIndex("insane/insane2.wav")
	s.SoundScream[2] = g.gi.SoundIndex("insane/insane3.wav")
	s.SoundScream[3] = g.gi.SoundIndex("insane/insane4.wav")
	s.SoundScream[4] = g.gi.SoundIndex("insane/insane6.wav")
	s.SoundScream[5] = g.gi.SoundIndex("insane/insane8.wav")
	s.SoundScream[6] = g.gi.SoundIndex("insane/insane9.wav")
	s.SoundScream[7] = g.gi.SoundIndex("insane/insane10.wav")

	self.Movetype = MOVETYPE_STEP
	self.Solid = SOLID_BBOX
	self.S.ModelIndex = int32(g.gi.ModelIndex("models/monsters/insane/tris.md2"))

	self.Mins = Vec3{-16, -16, -24}
	self.Maxs = Vec3{16, 16, 32}

	self.Health = 100
	self.GibHealth = -50
	self.Mass = 300

	self.Pain = insane_pain
	self.Die = insane_die

	self.Monsterinfo.Stand = insane_stand
	self.Monsterinfo.Walk = insane_walk
	self.Monsterinfo.Run = insane_run
	self.Monsterinfo.Dodge = nil
	self.Monsterinfo.Attack = nil
	self.Monsterinfo.Melee = nil
	self.Monsterinfo.Sight = nil
	self.Monsterinfo.Aiflags |= AI_GOOD_GUY

	//@@
	//	self->s.skinnum = skin;
	//	skin++;
	//	if (skin > 12)
	//		skin = 0;

	g.gi.LinkEntity(self)

	if self.Spawnflags&16 != 0 { // Stand Ground
		self.Monsterinfo.Aiflags |= AI_STAND_GROUND
	}

	self.Monsterinfo.Currentmove = insane_move_stand_normal

	self.Monsterinfo.Scale = insane_MODEL_SCALE

	if self.Spawnflags&8 != 0 { // Crucified ?
		self.Mins = Vec3{-16, 0, 0}
		self.Maxs = Vec3{16, 8, 32}
		self.Flags |= FL_NO_KNOCKBACK
		g.flymonster_start(self)
	} else {
		g.walkmonster_start(self)
		self.S.SkinNum = g.rng.Rand() % 3
	}
}
