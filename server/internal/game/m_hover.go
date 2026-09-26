package game

// Port of game/m_hover.c.

import (
	. "quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/shared"
)

// statics of m_hover.c
type hoverStatics struct {
	SoundPain1   int // sound_pain1
	SoundPain2   int // sound_pain2
	SoundDeath1  int // sound_death1
	SoundDeath2  int // sound_death2
	SoundSight   int // sound_sight
	SoundSearch1 int // sound_search1
	SoundSearch2 int // sound_search2
}

var (
	hover_sight        = defBlocked("hover_sight")
	hover_search       = defThink("hover_search")
	hover_reattack     = defThink("hover_reattack")
	hover_fire_blaster = defThink("hover_fire_blaster")
	hover_stand        = defThink("hover_stand")
	hover_run          = defThink("hover_run")
	hover_walk         = defThink("hover_walk")
	hover_start_attack = defThink("hover_start_attack")
	hover_attack       = defThink("hover_attack")
	hover_pain         = defPain("hover_pain")
	hover_deadthink    = defThink("hover_deadthink")
	hover_dead         = defThink("hover_dead")
	hover_die          = defDie("hover_die")
)

func init() {
	hover_sight.bind((*Game).hover_sight)
	hover_search.bind((*Game).hover_search)
	hover_reattack.bind((*Game).hover_reattack)
	hover_fire_blaster.bind((*Game).hover_fire_blaster)
	hover_stand.bind((*Game).hover_stand)
	hover_run.bind((*Game).hover_run)
	hover_walk.bind((*Game).hover_walk)
	hover_start_attack.bind((*Game).hover_start_attack)
	hover_attack.bind((*Game).hover_attack)
	hover_pain.bind((*Game).hover_pain)
	hover_deadthink.bind((*Game).hover_deadthink)
	hover_dead.bind((*Game).hover_dead)
	hover_die.bind((*Game).hover_die)
}

func init() {
	RegisterMonsterStatics("m_hover", func() any { return new(hoverStatics) })
	RegisterSpawn("monster_hover", (*Game).SP_monster_hover)
}

// C: game/m_hover.c:65 hover_frames_stand
var hover_frames_stand = []MFrame{
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
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

// C: game/m_hover.c:100 hover_frames_stop1
var hover_frames_stop1 = []MFrame{
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

// C: game/m_hover.c:114 hover_frames_stop2
var hover_frames_stop2 = []MFrame{
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
}

// C: game/m_hover.c:127 hover_frames_takeoff
var hover_frames_takeoff = []MFrame{
	{ai_move, 0, nil},
	{ai_move, -2, nil},
	{ai_move, 5, nil},
	{ai_move, -1, nil},
	{ai_move, 1, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, -1, nil},
	{ai_move, -1, nil},
	{ai_move, -1, nil},
	{ai_move, 0, nil},
	{ai_move, 2, nil},
	{ai_move, 2, nil},
	{ai_move, 1, nil},
	{ai_move, 1, nil},
	{ai_move, -6, nil},
	{ai_move, -9, nil},
	{ai_move, 1, nil},
	{ai_move, 0, nil},
	{ai_move, 2, nil},
	{ai_move, 2, nil},
	{ai_move, 1, nil},
	{ai_move, 1, nil},
	{ai_move, 1, nil},
	{ai_move, 2, nil},
	{ai_move, 0, nil},
	{ai_move, 2, nil},
	{ai_move, 3, nil},
	{ai_move, 2, nil},
	{ai_move, 0, nil},
}

// C: game/m_hover.c:162 hover_frames_pain3
var hover_frames_pain3 = []MFrame{
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

// C: game/m_hover.c:176 hover_frames_pain2
var hover_frames_pain2 = []MFrame{
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

// C: game/m_hover.c:193 hover_frames_pain1
var hover_frames_pain1 = []MFrame{
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 2, nil},
	{ai_move, -8, nil},
	{ai_move, -4, nil},
	{ai_move, -6, nil},
	{ai_move, -4, nil},
	{ai_move, -3, nil},
	{ai_move, 1, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 3, nil},
	{ai_move, 1, nil},
	{ai_move, 0, nil},
	{ai_move, 2, nil},
	{ai_move, 3, nil},
	{ai_move, 2, nil},
	{ai_move, 7, nil},
	{ai_move, 1, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 2, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 5, nil},
	{ai_move, 3, nil},
	{ai_move, 4, nil},
}

// C: game/m_hover.c:226 hover_frames_land
var hover_frames_land = []MFrame{
	{ai_move, 0, nil},
}

// C: game/m_hover.c:232 hover_frames_forward
var hover_frames_forward = []MFrame{
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

// C: game/m_hover.c:272 hover_frames_walk
var hover_frames_walk = []MFrame{
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

// C: game/m_hover.c:312 hover_frames_run
var hover_frames_run = []MFrame{
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

// C: game/m_hover.c:352 hover_frames_death1
var hover_frames_death1 = []MFrame{
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, -10, nil},
	{ai_move, 3, nil},
	{ai_move, 5, nil},
	{ai_move, 4, nil},
	{ai_move, 7, nil},
}

// C: game/m_hover.c:368 hover_frames_backward
var hover_frames_backward = []MFrame{
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

// C: game/m_hover.c:397 hover_frames_start_attack
var hover_frames_start_attack = []MFrame{
	{ai_charge, 1, nil},
	{ai_charge, 1, nil},
	{ai_charge, 1, nil},
}

// C: game/m_hover.c:405 hover_frames_attack1
var hover_frames_attack1 = []MFrame{
	{ai_charge, -10, hover_fire_blaster},
	{ai_charge, -10, hover_fire_blaster},
	{ai_charge, 0, hover_reattack},
}

// C: game/m_hover.c:414 hover_frames_end_attack
var hover_frames_end_attack = []MFrame{
	{ai_charge, 1, nil},
	{ai_charge, 1, nil},
}

// C: game/m_hover.c:98 hover_move_stand
var hover_move_stand = defMMove("hover_move_stand", hover_FRAME_stand01, hover_FRAME_stand30, hover_frames_stand, nil)

// C: game/m_hover.c:112 hover_move_stop1
var hover_move_stop1 = defMMove("hover_move_stop1", hover_FRAME_stop101, hover_FRAME_stop109, hover_frames_stop1, nil)

// C: game/m_hover.c:125 hover_move_stop2
var hover_move_stop2 = defMMove("hover_move_stop2", hover_FRAME_stop201, hover_FRAME_stop208, hover_frames_stop2, nil)

// C: game/m_hover.c:160 hover_move_takeoff
var hover_move_takeoff = defMMove("hover_move_takeoff", hover_FRAME_takeof01, hover_FRAME_takeof30, hover_frames_takeoff, nil)

// C: game/m_hover.c:174 hover_move_pain3
var hover_move_pain3 = defMMove("hover_move_pain3", hover_FRAME_pain301, hover_FRAME_pain309, hover_frames_pain3, hover_run)

// C: game/m_hover.c:191 hover_move_pain2
var hover_move_pain2 = defMMove("hover_move_pain2", hover_FRAME_pain201, hover_FRAME_pain212, hover_frames_pain2, hover_run)

// C: game/m_hover.c:224 hover_move_pain1
var hover_move_pain1 = defMMove("hover_move_pain1", hover_FRAME_pain101, hover_FRAME_pain128, hover_frames_pain1, hover_run)

// C: game/m_hover.c:230 hover_move_land
var hover_move_land = defMMove("hover_move_land", hover_FRAME_land01, hover_FRAME_land01, hover_frames_land, nil)

// C: game/m_hover.c:270 hover_move_forward
var hover_move_forward = defMMove("hover_move_forward", hover_FRAME_forwrd01, hover_FRAME_forwrd35, hover_frames_forward, nil)

// C: game/m_hover.c:310 hover_move_walk
var hover_move_walk = defMMove("hover_move_walk", hover_FRAME_forwrd01, hover_FRAME_forwrd35, hover_frames_walk, nil)

// C: game/m_hover.c:350 hover_move_run
var hover_move_run = defMMove("hover_move_run", hover_FRAME_forwrd01, hover_FRAME_forwrd35, hover_frames_run, nil)

// C: game/m_hover.c:366 hover_move_death1
var hover_move_death1 = defMMove("hover_move_death1", hover_FRAME_death101, hover_FRAME_death111, hover_frames_death1, hover_dead)

// C: game/m_hover.c:395 hover_move_backward
var hover_move_backward = defMMove("hover_move_backward", hover_FRAME_backwd01, hover_FRAME_backwd24, hover_frames_backward, nil)

// C: game/m_hover.c:403 hover_move_start_attack
var hover_move_start_attack = defMMove("hover_move_start_attack", hover_FRAME_attak101, hover_FRAME_attak103, hover_frames_start_attack, hover_attack)

// C: game/m_hover.c:411 hover_move_attack1
var hover_move_attack1 = defMMove("hover_move_attack1", hover_FRAME_attak104, hover_FRAME_attak106, hover_frames_attack1, nil)

// C: game/m_hover.c:419 hover_move_end_attack
var hover_move_end_attack = defMMove("hover_move_end_attack", hover_FRAME_attak107, hover_FRAME_attak108, hover_frames_end_attack, hover_run)

// C: game/m_hover.c:43 hover_sight
func (g *Game) hover_sight(self *Edict, other *Edict) {
	s := monsterStatics[hoverStatics](g, "m_hover")
	g.gi.Sound(self, CHAN_VOICE, s.SoundSight, 1, ATTN_NORM, 0)
}

// C: game/m_hover.c:48 hover_search
func (g *Game) hover_search(self *Edict) {
	s := monsterStatics[hoverStatics](g, "m_hover")
	if g.random() < 0.5 {
		g.gi.Sound(self, CHAN_VOICE, s.SoundSearch1, 1, ATTN_NORM, 0)
	} else {
		g.gi.Sound(self, CHAN_VOICE, s.SoundSearch2, 1, ATTN_NORM, 0)
	}
}

// C: game/m_hover.c:421 hover_reattack
func (g *Game) hover_reattack(self *Edict) {
	if self.Enemy.Health > 0 {
		if g.visible(self, self.Enemy) {
			if g.random() <= 0.6 {
				self.Monsterinfo.Currentmove = hover_move_attack1
				return
			}
		}
	}
	self.Monsterinfo.Currentmove = hover_move_end_attack
}

// C: game/m_hover.c:434 hover_fire_blaster
func (g *Game) hover_fire_blaster(self *Edict) {
	var start Vec3
	var forward, right Vec3
	var end Vec3
	var dir Vec3
	var effect int32

	if self.S.Frame == hover_FRAME_attak104 {
		effect = EF_HYPERBLASTER
	} else {
		effect = 0
	}

	shared.AngleVectors(self.S.Angles, &forward, &right, nil)
	start = G_ProjectSource(self.S.Origin, MonsterFlashOffset[MZ2_HOVER_BLASTER_1], forward, right)

	end = self.Enemy.S.Origin
	end[2] += float32(self.Enemy.Viewheight)
	dir = shared.VectorSubtract(end, start)

	g.monster_fire_blaster(self, start, dir, 1, 1000, MZ2_HOVER_BLASTER_1, effect)
}

// C: game/m_hover.c:458 hover_stand
func (g *Game) hover_stand(self *Edict) {
	self.Monsterinfo.Currentmove = hover_move_stand
}

// C: game/m_hover.c:463 hover_run
func (g *Game) hover_run(self *Edict) {
	if self.Monsterinfo.Aiflags&AI_STAND_GROUND != 0 {
		self.Monsterinfo.Currentmove = hover_move_stand
	} else {
		self.Monsterinfo.Currentmove = hover_move_run
	}
}

// C: game/m_hover.c:471 hover_walk
func (g *Game) hover_walk(self *Edict) {
	self.Monsterinfo.Currentmove = hover_move_walk
}

// C: game/m_hover.c:476 hover_start_attack
func (g *Game) hover_start_attack(self *Edict) {
	self.Monsterinfo.Currentmove = hover_move_start_attack
}

// C: game/m_hover.c:481 hover_attack
func (g *Game) hover_attack(self *Edict) {
	self.Monsterinfo.Currentmove = hover_move_attack1
}

// C: game/m_hover.c:487 hover_pain
func (g *Game) hover_pain(self *Edict, other *Edict, kick float32, damage int32) {
	s := monsterStatics[hoverStatics](g, "m_hover")
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

	if damage <= 25 {
		if g.random() < 0.5 {
			g.gi.Sound(self, CHAN_VOICE, s.SoundPain1, 1, ATTN_NORM, 0)
			self.Monsterinfo.Currentmove = hover_move_pain3
		} else {
			g.gi.Sound(self, CHAN_VOICE, s.SoundPain2, 1, ATTN_NORM, 0)
			self.Monsterinfo.Currentmove = hover_move_pain2
		}
	} else {
		g.gi.Sound(self, CHAN_VOICE, s.SoundPain1, 1, ATTN_NORM, 0)
		self.Monsterinfo.Currentmove = hover_move_pain1
	}
}

// C: game/m_hover.c:520 hover_deadthink
func (g *Game) hover_deadthink(self *Edict) {
	if self.Groundentity == nil && g.level.Time < self.Timestamp {
		self.Nextthink = float32(float64(g.level.Time) + FRAMETIME)
		return
	}
	g.BecomeExplosion1(self)
}

// C: game/m_hover.c:530 hover_dead
func (g *Game) hover_dead(self *Edict) {
	self.Mins = Vec3{-16, -16, -24}
	self.Maxs = Vec3{16, 16, -8}
	self.Movetype = MOVETYPE_TOSS
	self.Think = hover_deadthink
	self.Nextthink = float32(float64(g.level.Time) + FRAMETIME)
	self.Timestamp = g.level.Time + 15
	g.gi.LinkEntity(self)
}

// C: game/m_hover.c:541 hover_die
func (g *Game) hover_die(self *Edict, inflictor *Edict, attacker *Edict, damage int32, point Vec3) {
	s := monsterStatics[hoverStatics](g, "m_hover")
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
	if g.random() < 0.5 {
		g.gi.Sound(self, CHAN_VOICE, s.SoundDeath1, 1, ATTN_NORM, 0)
	} else {
		g.gi.Sound(self, CHAN_VOICE, s.SoundDeath2, 1, ATTN_NORM, 0)
	}
	self.Deadflag = DEAD_DEAD
	self.Takedamage = DAMAGE_YES
	self.Monsterinfo.Currentmove = hover_move_death1
}

// QUAKED monster_hover (1 .5 0) (-16 -16 -24) (16 16 32) Ambush Trigger_Spawn Sight
//
// C: game/m_hover.c:573 SP_monster_hover
func (g *Game) SP_monster_hover(self *Edict) {
	s := monsterStatics[hoverStatics](g, "m_hover")
	if g.deathmatch.Value != 0 {
		g.G_FreeEdict(self)
		return
	}

	s.SoundPain1 = g.gi.SoundIndex("hover/hovpain1.wav")
	s.SoundPain2 = g.gi.SoundIndex("hover/hovpain2.wav")
	s.SoundDeath1 = g.gi.SoundIndex("hover/hovdeth1.wav")
	s.SoundDeath2 = g.gi.SoundIndex("hover/hovdeth2.wav")
	s.SoundSight = g.gi.SoundIndex("hover/hovsght1.wav")
	s.SoundSearch1 = g.gi.SoundIndex("hover/hovsrch1.wav")
	s.SoundSearch2 = g.gi.SoundIndex("hover/hovsrch2.wav")

	g.gi.SoundIndex("hover/hovatck1.wav")

	self.S.Sound = int32(g.gi.SoundIndex("hover/hovidle1.wav"))

	self.Movetype = MOVETYPE_STEP
	self.Solid = SOLID_BBOX
	self.S.ModelIndex = int32(g.gi.ModelIndex("models/monsters/hover/tris.md2"))
	self.Mins = Vec3{-24, -24, -24}
	self.Maxs = Vec3{24, 24, 32}

	self.Health = 240
	self.GibHealth = -100
	self.Mass = 150

	self.Pain = hover_pain
	self.Die = hover_die

	self.Monsterinfo.Stand = hover_stand
	self.Monsterinfo.Walk = hover_walk
	self.Monsterinfo.Run = hover_run
	//	self->monsterinfo.dodge = hover_dodge;
	self.Monsterinfo.Attack = hover_start_attack
	self.Monsterinfo.Sight = hover_sight
	self.Monsterinfo.Search = hover_search

	g.gi.LinkEntity(self)

	self.Monsterinfo.Currentmove = hover_move_stand
	self.Monsterinfo.Scale = hover_MODEL_SCALE

	g.flymonster_start(self)
}
