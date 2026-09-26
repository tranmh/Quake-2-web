package game

// Port of game/m_soldier.c.

import (
	"math"

	. "quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/shared"
)

// statics of m_soldier.c
type soldierStatics struct {
	SoundIdle       int // sound_idle
	SoundSight1     int // sound_sight1
	SoundSight2     int // sound_sight2
	SoundPainLight  int // sound_pain_light
	SoundPain       int // sound_pain
	SoundPainSs     int // sound_pain_ss
	SoundDeathLight int // sound_death_light
	SoundDeath      int // sound_death
	SoundDeathSs    int // sound_death_ss
	SoundCock       int // sound_cock
}

var (
	soldier_idle            = defThink("soldier_idle")
	soldier_cock            = defThink("soldier_cock")
	soldier_stand           = defThink("soldier_stand")
	soldier_walk1_random    = defThink("soldier_walk1_random")
	soldier_walk            = defThink("soldier_walk")
	soldier_run             = defThink("soldier_run")
	soldier_pain            = defPain("soldier_pain")
	soldier_fire1           = defThink("soldier_fire1")
	soldier_attack1_refire1 = defThink("soldier_attack1_refire1")
	soldier_attack1_refire2 = defThink("soldier_attack1_refire2")
	soldier_fire2           = defThink("soldier_fire2")
	soldier_attack2_refire1 = defThink("soldier_attack2_refire1")
	soldier_attack2_refire2 = defThink("soldier_attack2_refire2")
	soldier_duck_down       = defThink("soldier_duck_down")
	soldier_duck_up         = defThink("soldier_duck_up")
	soldier_fire3           = defThink("soldier_fire3")
	soldier_attack3_refire  = defThink("soldier_attack3_refire")
	soldier_fire4           = defThink("soldier_fire4")
	soldier_fire8           = defThink("soldier_fire8")
	soldier_attack6_refire  = defThink("soldier_attack6_refire")
	soldier_attack          = defThink("soldier_attack")
	soldier_sight           = defBlocked("soldier_sight")
	soldier_duck_hold       = defThink("soldier_duck_hold")
	soldier_dodge           = defDodge("soldier_dodge")
	soldier_fire6           = defThink("soldier_fire6")
	soldier_fire7           = defThink("soldier_fire7")
	soldier_dead            = defThink("soldier_dead")
	soldier_die             = defDie("soldier_die")
)

func init() {
	soldier_idle.bind((*Game).soldier_idle)
	soldier_cock.bind((*Game).soldier_cock)
	soldier_stand.bind((*Game).soldier_stand)
	soldier_walk1_random.bind((*Game).soldier_walk1_random)
	soldier_walk.bind((*Game).soldier_walk)
	soldier_run.bind((*Game).soldier_run)
	soldier_pain.bind((*Game).soldier_pain)
	soldier_fire1.bind((*Game).soldier_fire1)
	soldier_attack1_refire1.bind((*Game).soldier_attack1_refire1)
	soldier_attack1_refire2.bind((*Game).soldier_attack1_refire2)
	soldier_fire2.bind((*Game).soldier_fire2)
	soldier_attack2_refire1.bind((*Game).soldier_attack2_refire1)
	soldier_attack2_refire2.bind((*Game).soldier_attack2_refire2)
	soldier_duck_down.bind((*Game).soldier_duck_down)
	soldier_duck_up.bind((*Game).soldier_duck_up)
	soldier_fire3.bind((*Game).soldier_fire3)
	soldier_attack3_refire.bind((*Game).soldier_attack3_refire)
	soldier_fire4.bind((*Game).soldier_fire4)
	soldier_fire8.bind((*Game).soldier_fire8)
	soldier_attack6_refire.bind((*Game).soldier_attack6_refire)
	soldier_attack.bind((*Game).soldier_attack)
	soldier_sight.bind((*Game).soldier_sight)
	soldier_duck_hold.bind((*Game).soldier_duck_hold)
	soldier_dodge.bind((*Game).soldier_dodge)
	soldier_fire6.bind((*Game).soldier_fire6)
	soldier_fire7.bind((*Game).soldier_fire7)
	soldier_dead.bind((*Game).soldier_dead)
	soldier_die.bind((*Game).soldier_die)
}

func init() {
	RegisterMonsterStatics("m_soldier", func() any { return new(soldierStatics) })
	RegisterSpawn("monster_soldier_light", (*Game).SP_monster_soldier_light)
	RegisterSpawn("monster_soldier", (*Game).SP_monster_soldier)
	RegisterSpawn("monster_soldier_ss", (*Game).SP_monster_soldier_ss)
}

// ==============================================================================
//
// # SOLDIER
//
// ==============================================================================
//
// C: game/m_soldier.c:44 soldier_idle
func (g *Game) soldier_idle(self *Edict) {
	s := monsterStatics[soldierStatics](g, "m_soldier")
	if float64(g.random()) > 0.8 {
		g.gi.Sound(self, CHAN_VOICE, s.SoundIdle, 1, ATTN_IDLE, 0)
	}
}

// C: game/m_soldier.c:50 soldier_cock
func (g *Game) soldier_cock(self *Edict) {
	s := monsterStatics[soldierStatics](g, "m_soldier")
	if self.S.Frame == soldier_FRAME_stand322 {
		g.gi.Sound(self, CHAN_WEAPON, s.SoundCock, 1, ATTN_IDLE, 0)
	} else {
		g.gi.Sound(self, CHAN_WEAPON, s.SoundCock, 1, ATTN_NORM, 0)
	}
}

// STAND
// C: game/m_soldier.c:63 soldier_frames_stand1
var soldier_frames_stand1 = []MFrame{
	{ai_stand, 0, soldier_idle},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
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

// C: game/m_soldier.c:98 soldier_move_stand1
var soldier_move_stand1 = defMMove("soldier_move_stand1", soldier_FRAME_stand101, soldier_FRAME_stand130, soldier_frames_stand1, soldier_stand)

// C: game/m_soldier.c:100 soldier_frames_stand3
var soldier_frames_stand3 = []MFrame{
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, soldier_cock},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
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

// C: game/m_soldier.c:145 soldier_move_stand3
var soldier_move_stand3 = defMMove("soldier_move_stand3", soldier_FRAME_stand301, soldier_FRAME_stand339, soldier_frames_stand3, soldier_stand)

// C: game/m_soldier.c:211 soldier_stand
func (g *Game) soldier_stand(self *Edict) {
	if (self.Monsterinfo.Currentmove == soldier_move_stand3) || (float64(g.random()) < 0.8) {
		self.Monsterinfo.Currentmove = soldier_move_stand1
	} else {
		self.Monsterinfo.Currentmove = soldier_move_stand3
	}
}

// WALK
//
// C: game/m_soldier.c:224 soldier_walk1_random
func (g *Game) soldier_walk1_random(self *Edict) {
	if float64(g.random()) > 0.1 {
		self.Monsterinfo.Nextframe = soldier_FRAME_walk101
	}
}

// C: game/m_soldier.c:230 soldier_frames_walk1
var soldier_frames_walk1 = []MFrame{
	{ai_walk, 3, nil},
	{ai_walk, 6, nil},
	{ai_walk, 2, nil},
	{ai_walk, 2, nil},
	{ai_walk, 2, nil},
	{ai_walk, 1, nil},
	{ai_walk, 6, nil},
	{ai_walk, 5, nil},
	{ai_walk, 3, nil},
	{ai_walk, -1, soldier_walk1_random},
	{ai_walk, 0, nil},
	{ai_walk, 0, nil},
	{ai_walk, 0, nil},
	{ai_walk, 0, nil},
	{ai_walk, 0, nil},
	{ai_walk, 0, nil},
	{ai_walk, 0, nil},
	{ai_walk, 0, nil},
	{ai_walk, 0, nil},
	{ai_walk, 0, nil},
	{ai_walk, 0, nil},
	{ai_walk, 0, nil},
	{ai_walk, 0, nil},
	{ai_walk, 0, nil},
	{ai_walk, 0, nil},
	{ai_walk, 0, nil},
	{ai_walk, 0, nil},
	{ai_walk, 0, nil},
	{ai_walk, 0, nil},
	{ai_walk, 0, nil},
	{ai_walk, 0, nil},
	{ai_walk, 0, nil},
	{ai_walk, 0, nil},
}

// C: game/m_soldier.c:266 soldier_move_walk1
var soldier_move_walk1 = defMMove("soldier_move_walk1", soldier_FRAME_walk101, soldier_FRAME_walk133, soldier_frames_walk1, nil)

// C: game/m_soldier.c:268 soldier_frames_walk2
var soldier_frames_walk2 = []MFrame{
	{ai_walk, 4, nil},
	{ai_walk, 4, nil},
	{ai_walk, 9, nil},
	{ai_walk, 8, nil},
	{ai_walk, 5, nil},
	{ai_walk, 1, nil},
	{ai_walk, 3, nil},
	{ai_walk, 7, nil},
	{ai_walk, 6, nil},
	{ai_walk, 7, nil},
}

// C: game/m_soldier.c:281 soldier_move_walk2
var soldier_move_walk2 = defMMove("soldier_move_walk2", soldier_FRAME_walk209, soldier_FRAME_walk218, soldier_frames_walk2, nil)

// C: game/m_soldier.c:283 soldier_walk
func (g *Game) soldier_walk(self *Edict) {
	if g.random() < 0.5 {
		self.Monsterinfo.Currentmove = soldier_move_walk1
	} else {
		self.Monsterinfo.Currentmove = soldier_move_walk2
	}
}

// RUN
//
// C: game/m_soldier.c:298 soldier_frames_start_run
var soldier_frames_start_run = []MFrame{
	{ai_run, 7, nil},
	{ai_run, 5, nil},
}

// C: game/m_soldier.c:303 soldier_move_start_run
var soldier_move_start_run = defMMove("soldier_move_start_run", soldier_FRAME_run01, soldier_FRAME_run02, soldier_frames_start_run, soldier_run)

// C: game/m_soldier.c:305 soldier_frames_run
var soldier_frames_run = []MFrame{
	{ai_run, 10, nil},
	{ai_run, 11, nil},
	{ai_run, 11, nil},
	{ai_run, 16, nil},
	{ai_run, 10, nil},
	{ai_run, 15, nil},
}

// C: game/m_soldier.c:314 soldier_move_run
var soldier_move_run = defMMove("soldier_move_run", soldier_FRAME_run03, soldier_FRAME_run08, soldier_frames_run, nil)

// C: game/m_soldier.c:316 soldier_run
func (g *Game) soldier_run(self *Edict) {
	if self.Monsterinfo.Aiflags&AI_STAND_GROUND != 0 {
		self.Monsterinfo.Currentmove = soldier_move_stand1
		return
	}

	if self.Monsterinfo.Currentmove == soldier_move_walk1 ||
		self.Monsterinfo.Currentmove == soldier_move_walk2 ||
		self.Monsterinfo.Currentmove == soldier_move_start_run {
		self.Monsterinfo.Currentmove = soldier_move_run
	} else {
		self.Monsterinfo.Currentmove = soldier_move_start_run
	}
}

// PAIN
//
// C: game/m_soldier.c:341 soldier_frames_pain1
var soldier_frames_pain1 = []MFrame{
	{ai_move, -3, nil},
	{ai_move, 4, nil},
	{ai_move, 1, nil},
	{ai_move, 1, nil},
	{ai_move, 0, nil},
}

// C: game/m_soldier.c:349 soldier_move_pain1
var soldier_move_pain1 = defMMove("soldier_move_pain1", soldier_FRAME_pain101, soldier_FRAME_pain105, soldier_frames_pain1, soldier_run)

// C: game/m_soldier.c:351 soldier_frames_pain2
var soldier_frames_pain2 = []MFrame{
	{ai_move, -13, nil},
	{ai_move, -1, nil},
	{ai_move, 2, nil},
	{ai_move, 4, nil},
	{ai_move, 2, nil},
	{ai_move, 3, nil},
	{ai_move, 2, nil},
}

// C: game/m_soldier.c:361 soldier_move_pain2
var soldier_move_pain2 = defMMove("soldier_move_pain2", soldier_FRAME_pain201, soldier_FRAME_pain207, soldier_frames_pain2, soldier_run)

// C: game/m_soldier.c:363 soldier_frames_pain3
var soldier_frames_pain3 = []MFrame{
	{ai_move, -8, nil},
	{ai_move, 10, nil},
	{ai_move, -4, nil},
	{ai_move, -1, nil},
	{ai_move, -3, nil},
	{ai_move, 0, nil},
	{ai_move, 3, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 1, nil},
	{ai_move, 0, nil},
	{ai_move, 1, nil},
	{ai_move, 2, nil},
	{ai_move, 4, nil},
	{ai_move, 3, nil},
	{ai_move, 2, nil},
}

// C: game/m_soldier.c:384 soldier_move_pain3
var soldier_move_pain3 = defMMove("soldier_move_pain3", soldier_FRAME_pain301, soldier_FRAME_pain318, soldier_frames_pain3, soldier_run)

// C: game/m_soldier.c:386 soldier_frames_pain4
var soldier_frames_pain4 = []MFrame{
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, -10, nil},
	{ai_move, -6, nil},
	{ai_move, 8, nil},
	{ai_move, 4, nil},
	{ai_move, 1, nil},
	{ai_move, 0, nil},
	{ai_move, 2, nil},
	{ai_move, 5, nil},
	{ai_move, 2, nil},
	{ai_move, -1, nil},
	{ai_move, -1, nil},
	{ai_move, 3, nil},
	{ai_move, 2, nil},
	{ai_move, 0, nil},
}

// C: game/m_soldier.c:406 soldier_move_pain4
var soldier_move_pain4 = defMMove("soldier_move_pain4", soldier_FRAME_pain401, soldier_FRAME_pain417, soldier_frames_pain4, soldier_run)

// C: game/m_soldier.c:409 soldier_pain
func (g *Game) soldier_pain(self *Edict, other *Edict, kick float32, damage int32) {
	s := monsterStatics[soldierStatics](g, "m_soldier")
	var r float32
	var n int32

	if self.Health < (self.MaxHealth / 2) {
		self.S.SkinNum |= 1
	}

	if g.level.Time < self.PainDebounceTime {
		if (self.Velocity[2] > 100) && ((self.Monsterinfo.Currentmove == soldier_move_pain1) || (self.Monsterinfo.Currentmove == soldier_move_pain2) || (self.Monsterinfo.Currentmove == soldier_move_pain3)) {
			self.Monsterinfo.Currentmove = soldier_move_pain4
		}
		return
	}

	self.PainDebounceTime = g.level.Time + 3

	n = self.S.SkinNum | 1
	if n == 1 {
		g.gi.Sound(self, CHAN_VOICE, s.SoundPainLight, 1, ATTN_NORM, 0)
	} else if n == 3 {
		g.gi.Sound(self, CHAN_VOICE, s.SoundPain, 1, ATTN_NORM, 0)
	} else {
		g.gi.Sound(self, CHAN_VOICE, s.SoundPainSs, 1, ATTN_NORM, 0)
	}

	if self.Velocity[2] > 100 {
		self.Monsterinfo.Currentmove = soldier_move_pain4
		return
	}

	if g.skill.Value == 3 {
		return // no pain anims in nightmare
	}

	r = g.random()

	if float64(r) < 0.33 {
		self.Monsterinfo.Currentmove = soldier_move_pain1
	} else if float64(r) < 0.66 {
		self.Monsterinfo.Currentmove = soldier_move_pain2
	} else {
		self.Monsterinfo.Currentmove = soldier_move_pain3
	}
}

// C: game/m_soldier.c:458 blaster_flash
var blaster_flash = [...]int32{MZ2_SOLDIER_BLASTER_1, MZ2_SOLDIER_BLASTER_2, MZ2_SOLDIER_BLASTER_3, MZ2_SOLDIER_BLASTER_4, MZ2_SOLDIER_BLASTER_5, MZ2_SOLDIER_BLASTER_6, MZ2_SOLDIER_BLASTER_7, MZ2_SOLDIER_BLASTER_8}

// C: game/m_soldier.c:459 shotgun_flash
var shotgun_flash = [...]int32{MZ2_SOLDIER_SHOTGUN_1, MZ2_SOLDIER_SHOTGUN_2, MZ2_SOLDIER_SHOTGUN_3, MZ2_SOLDIER_SHOTGUN_4, MZ2_SOLDIER_SHOTGUN_5, MZ2_SOLDIER_SHOTGUN_6, MZ2_SOLDIER_SHOTGUN_7, MZ2_SOLDIER_SHOTGUN_8}

// C: game/m_soldier.c:460 machinegun_flash
var machinegun_flash = [...]int32{MZ2_SOLDIER_MACHINEGUN_1, MZ2_SOLDIER_MACHINEGUN_2, MZ2_SOLDIER_MACHINEGUN_3, MZ2_SOLDIER_MACHINEGUN_4, MZ2_SOLDIER_MACHINEGUN_5, MZ2_SOLDIER_MACHINEGUN_6, MZ2_SOLDIER_MACHINEGUN_7, MZ2_SOLDIER_MACHINEGUN_8}

// ATTACK
//
// C: game/m_soldier.c:462 soldier_fire
func (g *Game) soldier_fire(self *Edict, flash_number int32) {
	var start Vec3
	var forward, right, up Vec3
	var aim Vec3
	var dir Vec3
	var end Vec3
	var r, u float32
	var flash_index int32

	if self.S.SkinNum < 2 {
		flash_index = blaster_flash[flash_number]
	} else if self.S.SkinNum < 4 {
		flash_index = shotgun_flash[flash_number]
	} else {
		flash_index = machinegun_flash[flash_number]
	}

	shared.AngleVectors(self.S.Angles, &forward, &right, nil)
	start = G_ProjectSource(self.S.Origin, MonsterFlashOffset[flash_index], forward, right)

	if flash_number == 5 || flash_number == 6 {
		aim = forward
	} else {
		end = self.Enemy.S.Origin
		end[2] += float32(self.Enemy.Viewheight)
		aim = shared.VectorSubtract(end, start)
		dir = vectoangles(aim)
		shared.AngleVectors(dir, &forward, &right, &up)

		r = float32(g.crandom() * 1000)
		u = float32(g.crandom() * 500)
		end = shared.VectorMA(start, 8192, forward)
		end = shared.VectorMA(end, r, right)
		end = shared.VectorMA(end, u, up)

		aim = shared.VectorSubtract(end, start)
		shared.VectorNormalize(&aim)
	}

	if self.S.SkinNum <= 1 {
		g.monster_fire_blaster(self, start, aim, 5, 600, flash_index, EF_BLASTER)
	} else if self.S.SkinNum <= 3 {
		g.monster_fire_shotgun(self, start, aim, 2, 1, DEFAULT_SHOTGUN_HSPREAD, DEFAULT_SHOTGUN_VSPREAD, DEFAULT_SHOTGUN_COUNT, flash_index)
	} else {
		if self.Monsterinfo.Aiflags&AI_HOLD_FRAME == 0 {
			self.Monsterinfo.Pausetime = float32(float64(g.level.Time) + float64(3+g.rng.Rand()%8)*FRAMETIME)
		}

		g.monster_fire_bullet(self, start, aim, 2, 4, DEFAULT_BULLET_HSPREAD, DEFAULT_BULLET_VSPREAD, flash_index)

		if g.level.Time >= self.Monsterinfo.Pausetime {
			self.Monsterinfo.Aiflags &^= AI_HOLD_FRAME
		} else {
			self.Monsterinfo.Aiflags |= AI_HOLD_FRAME
		}
	}
}

// ATTACK1 (blaster/shotgun)
// C: game/m_soldier.c:528 soldier_fire1
func (g *Game) soldier_fire1(self *Edict) {
	g.soldier_fire(self, 0)
}

// C: game/m_soldier.c:533 soldier_attack1_refire1
func (g *Game) soldier_attack1_refire1(self *Edict) {
	if self.S.SkinNum > 1 {
		return
	}

	if self.Enemy.Health <= 0 {
		return
	}

	if ((g.skill.Value == 3) && (g.random() < 0.5)) || (g.range_(self, self.Enemy) == RANGE_MELEE) {
		self.Monsterinfo.Nextframe = soldier_FRAME_attak102
	} else {
		self.Monsterinfo.Nextframe = soldier_FRAME_attak110
	}
}

// C: game/m_soldier.c:547 soldier_attack1_refire2
func (g *Game) soldier_attack1_refire2(self *Edict) {
	if self.S.SkinNum < 2 {
		return
	}

	if self.Enemy.Health <= 0 {
		return
	}

	if ((g.skill.Value == 3) && (g.random() < 0.5)) || (g.range_(self, self.Enemy) == RANGE_MELEE) {
		self.Monsterinfo.Nextframe = soldier_FRAME_attak102
	}
}

// C: game/m_soldier.c:559 soldier_frames_attack1
var soldier_frames_attack1 = []MFrame{
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, soldier_fire1},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, soldier_attack1_refire1},
	{ai_charge, 0, nil},
	{ai_charge, 0, soldier_cock},
	{ai_charge, 0, soldier_attack1_refire2},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
}

// C: game/m_soldier.c:574 soldier_move_attack1
var soldier_move_attack1 = defMMove("soldier_move_attack1", soldier_FRAME_attak101, soldier_FRAME_attak112, soldier_frames_attack1, soldier_run)

// ATTACK2 (blaster/shotgun)
// C: game/m_soldier.c:578 soldier_fire2
func (g *Game) soldier_fire2(self *Edict) {
	g.soldier_fire(self, 1)
}

// C: game/m_soldier.c:583 soldier_attack2_refire1
func (g *Game) soldier_attack2_refire1(self *Edict) {
	if self.S.SkinNum > 1 {
		return
	}

	if self.Enemy.Health <= 0 {
		return
	}

	if ((g.skill.Value == 3) && (g.random() < 0.5)) || (g.range_(self, self.Enemy) == RANGE_MELEE) {
		self.Monsterinfo.Nextframe = soldier_FRAME_attak204
	} else {
		self.Monsterinfo.Nextframe = soldier_FRAME_attak216
	}
}

// C: game/m_soldier.c:597 soldier_attack2_refire2
func (g *Game) soldier_attack2_refire2(self *Edict) {
	if self.S.SkinNum < 2 {
		return
	}

	if self.Enemy.Health <= 0 {
		return
	}

	if ((g.skill.Value == 3) && (g.random() < 0.5)) || (g.range_(self, self.Enemy) == RANGE_MELEE) {
		self.Monsterinfo.Nextframe = soldier_FRAME_attak204
	}
}

// C: game/m_soldier.c:609 soldier_frames_attack2
var soldier_frames_attack2 = []MFrame{
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, soldier_fire2},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, soldier_attack2_refire1},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, soldier_cock},
	{ai_charge, 0, nil},
	{ai_charge, 0, soldier_attack2_refire2},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
}

// C: game/m_soldier.c:630 soldier_move_attack2
var soldier_move_attack2 = defMMove("soldier_move_attack2", soldier_FRAME_attak201, soldier_FRAME_attak218, soldier_frames_attack2, soldier_run)

// ATTACK3 (duck and shoot)
// C: game/m_soldier.c:634 soldier_duck_down
func (g *Game) soldier_duck_down(self *Edict) {
	if self.Monsterinfo.Aiflags&AI_DUCKED != 0 {
		return
	}
	self.Monsterinfo.Aiflags |= AI_DUCKED
	self.Maxs[2] -= 32
	self.Takedamage = DAMAGE_YES
	self.Monsterinfo.Pausetime = g.level.Time + 1
	g.gi.LinkEntity(self)
}

// C: game/m_soldier.c:645 soldier_duck_up
func (g *Game) soldier_duck_up(self *Edict) {
	self.Monsterinfo.Aiflags &^= AI_DUCKED
	self.Maxs[2] += 32
	self.Takedamage = DAMAGE_AIM
	g.gi.LinkEntity(self)
}

// C: game/m_soldier.c:653 soldier_fire3
func (g *Game) soldier_fire3(self *Edict) {
	g.soldier_duck_down(self)
	g.soldier_fire(self, 2)
}

// C: game/m_soldier.c:659 soldier_attack3_refire
func (g *Game) soldier_attack3_refire(self *Edict) {
	if (float64(g.level.Time) + 0.4) < float64(self.Monsterinfo.Pausetime) {
		self.Monsterinfo.Nextframe = soldier_FRAME_attak303
	}
}

// C: game/m_soldier.c:665 soldier_frames_attack3
var soldier_frames_attack3 = []MFrame{
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, soldier_fire3},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, soldier_attack3_refire},
	{ai_charge, 0, soldier_duck_up},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
}

// C: game/m_soldier.c:677 soldier_move_attack3
var soldier_move_attack3 = defMMove("soldier_move_attack3", soldier_FRAME_attak301, soldier_FRAME_attak309, soldier_frames_attack3, soldier_run)

// ATTACK4 (machinegun)
// C: game/m_soldier.c:681 soldier_fire4
func (g *Game) soldier_fire4(self *Edict) {
	g.soldier_fire(self, 3)
	//
	//	if (self->enemy->health <= 0)
	//		return;
	//
	//	if ( ((skill->value == 3) && (random() < 0.5)) || (range(self, self->enemy) == RANGE_MELEE) )
	//		self->monsterinfo.nextframe = FRAME_attak402;
}

// C: game/m_soldier.c:692 soldier_frames_attack4
var soldier_frames_attack4 = []MFrame{
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, soldier_fire4},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
}

// C: game/m_soldier.c:701 soldier_move_attack4
var soldier_move_attack4 = defMMove("soldier_move_attack4", soldier_FRAME_attak401, soldier_FRAME_attak406, soldier_frames_attack4, soldier_run)

// ATTACK6 (run & shoot)
// C: game/m_soldier.c:736 soldier_fire8
func (g *Game) soldier_fire8(self *Edict) {
	g.soldier_fire(self, 7)
}

// C: game/m_soldier.c:741 soldier_attack6_refire
func (g *Game) soldier_attack6_refire(self *Edict) {
	if self.Enemy.Health <= 0 {
		return
	}

	if g.range_(self, self.Enemy) < RANGE_MID {
		return
	}

	if g.skill.Value == 3 {
		self.Monsterinfo.Nextframe = soldier_FRAME_runs03
	}
}

// C: game/m_soldier.c:753 soldier_frames_attack6
var soldier_frames_attack6 = []MFrame{
	{ai_charge, 10, nil},
	{ai_charge, 4, nil},
	{ai_charge, 12, nil},
	{ai_charge, 11, soldier_fire8},
	{ai_charge, 13, nil},
	{ai_charge, 18, nil},
	{ai_charge, 15, nil},
	{ai_charge, 14, nil},
	{ai_charge, 11, nil},
	{ai_charge, 8, nil},
	{ai_charge, 11, nil},
	{ai_charge, 12, nil},
	{ai_charge, 12, nil},
	{ai_charge, 17, soldier_attack6_refire},
}

// C: game/m_soldier.c:770 soldier_move_attack6
var soldier_move_attack6 = defMMove("soldier_move_attack6", soldier_FRAME_runs01, soldier_FRAME_runs14, soldier_frames_attack6, soldier_run)

// C: game/m_soldier.c:772 soldier_attack
func (g *Game) soldier_attack(self *Edict) {
	if self.S.SkinNum < 4 {
		if g.random() < 0.5 {
			self.Monsterinfo.Currentmove = soldier_move_attack1
		} else {
			self.Monsterinfo.Currentmove = soldier_move_attack2
		}
	} else {
		self.Monsterinfo.Currentmove = soldier_move_attack4
	}
}

// SIGHT
//
// C: game/m_soldier.c:792 soldier_sight
func (g *Game) soldier_sight(self *Edict, other *Edict) {
	s := monsterStatics[soldierStatics](g, "m_soldier")
	if g.random() < 0.5 {
		g.gi.Sound(self, CHAN_VOICE, s.SoundSight1, 1, ATTN_NORM, 0)
	} else {
		g.gi.Sound(self, CHAN_VOICE, s.SoundSight2, 1, ATTN_NORM, 0)
	}

	if (g.skill.Value > 0) && (g.range_(self, self.Enemy) >= RANGE_MID) {
		if g.random() > 0.5 {
			self.Monsterinfo.Currentmove = soldier_move_attack6
		}
	}
}

// DUCK
//
// C: game/m_soldier.c:810 soldier_duck_hold
func (g *Game) soldier_duck_hold(self *Edict) {
	if g.level.Time >= self.Monsterinfo.Pausetime {
		self.Monsterinfo.Aiflags &^= AI_HOLD_FRAME
	} else {
		self.Monsterinfo.Aiflags |= AI_HOLD_FRAME
	}
}

// C: game/m_soldier.c:818 soldier_frames_duck
var soldier_frames_duck = []MFrame{
	{ai_move, 5, soldier_duck_down},
	{ai_move, -1, soldier_duck_hold},
	{ai_move, 1, nil},
	{ai_move, 0, soldier_duck_up},
	{ai_move, 5, nil},
}

// C: game/m_soldier.c:826 soldier_move_duck
var soldier_move_duck = defMMove("soldier_move_duck", soldier_FRAME_duck01, soldier_FRAME_duck05, soldier_frames_duck, soldier_run)

// C: game/m_soldier.c:828 soldier_dodge
func (g *Game) soldier_dodge(self *Edict, attacker *Edict, eta float32) {
	var r float32

	r = g.random()
	if r > 0.25 {
		return
	}

	if self.Enemy == nil {
		self.Enemy = attacker
	}

	if g.skill.Value == 0 {
		self.Monsterinfo.Currentmove = soldier_move_duck
		return
	}

	self.Monsterinfo.Pausetime = float32(float64(g.level.Time+eta) + 0.3)
	r = g.random()

	if g.skill.Value == 1 {
		if float64(r) > 0.33 {
			self.Monsterinfo.Currentmove = soldier_move_duck
		} else {
			self.Monsterinfo.Currentmove = soldier_move_attack3
		}
		return
	}

	if g.skill.Value >= 2 {
		if float64(r) > 0.66 {
			self.Monsterinfo.Currentmove = soldier_move_duck
		} else {
			self.Monsterinfo.Currentmove = soldier_move_attack3
		}
		return
	}

	self.Monsterinfo.Currentmove = soldier_move_attack3
}

// DEATH
//
// C: game/m_soldier.c:874 soldier_fire6
func (g *Game) soldier_fire6(self *Edict) {
	g.soldier_fire(self, 5)
}

// C: game/m_soldier.c:879 soldier_fire7
func (g *Game) soldier_fire7(self *Edict) {
	g.soldier_fire(self, 6)
}

// C: game/m_soldier.c:884 soldier_dead
func (g *Game) soldier_dead(self *Edict) {
	self.Mins = Vec3{-16, -16, -24}
	self.Maxs = Vec3{16, 16, -8}
	self.Movetype = MOVETYPE_TOSS
	self.SVFlags |= SVF_DEADMONSTER
	self.Nextthink = 0
	g.gi.LinkEntity(self)
}

// C: game/m_soldier.c:894 soldier_frames_death1
var soldier_frames_death1 = []MFrame{
	{ai_move, 0, nil},
	{ai_move, -10, nil},
	{ai_move, -10, nil},
	{ai_move, -10, nil},
	{ai_move, -5, nil},
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
	{ai_move, 0, soldier_fire6},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, soldier_fire7},
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

// C: game/m_soldier.c:936 soldier_move_death1
var soldier_move_death1 = defMMove("soldier_move_death1", soldier_FRAME_death101, soldier_FRAME_death136, soldier_frames_death1, soldier_dead)

// C: game/m_soldier.c:938 soldier_frames_death2
var soldier_frames_death2 = []MFrame{
	{ai_move, -5, nil},
	{ai_move, -5, nil},
	{ai_move, -5, nil},
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

// C: game/m_soldier.c:979 soldier_move_death2
var soldier_move_death2 = defMMove("soldier_move_death2", soldier_FRAME_death201, soldier_FRAME_death235, soldier_frames_death2, soldier_dead)

// C: game/m_soldier.c:981 soldier_frames_death3
var soldier_frames_death3 = []MFrame{
	{ai_move, -5, nil},
	{ai_move, -5, nil},
	{ai_move, -5, nil},
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

// C: game/m_soldier.c:1033 soldier_move_death3
var soldier_move_death3 = defMMove("soldier_move_death3", soldier_FRAME_death301, soldier_FRAME_death345, soldier_frames_death3, soldier_dead)

// C: game/m_soldier.c:1035 soldier_frames_death4
var soldier_frames_death4 = []MFrame{
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

// C: game/m_soldier.c:1096 soldier_move_death4
var soldier_move_death4 = defMMove("soldier_move_death4", soldier_FRAME_death401, soldier_FRAME_death453, soldier_frames_death4, soldier_dead)

// C: game/m_soldier.c:1098 soldier_frames_death5
var soldier_frames_death5 = []MFrame{
	{ai_move, -5, nil},
	{ai_move, -5, nil},
	{ai_move, -5, nil},
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

// C: game/m_soldier.c:1127 soldier_move_death5
var soldier_move_death5 = defMMove("soldier_move_death5", soldier_FRAME_death501, soldier_FRAME_death524, soldier_frames_death5, soldier_dead)

// C: game/m_soldier.c:1129 soldier_frames_death6
var soldier_frames_death6 = []MFrame{
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

// C: game/m_soldier.c:1142 soldier_move_death6
var soldier_move_death6 = defMMove("soldier_move_death6", soldier_FRAME_death601, soldier_FRAME_death610, soldier_frames_death6, soldier_dead)

// C: game/m_soldier.c:1144 soldier_die
func (g *Game) soldier_die(self *Edict, inflictor *Edict, attacker *Edict, damage int32, point Vec3) {
	s := monsterStatics[soldierStatics](g, "m_soldier")
	var n int32

	// check for gib
	if self.Health <= self.GibHealth {
		g.gi.Sound(self, CHAN_VOICE, g.gi.SoundIndex("misc/udeath.wav"), 1, ATTN_NORM, 0)
		for n = 0; n < 3; n++ {
			g.ThrowGib(self, "models/objects/gibs/sm_meat/tris.md2", damage, GIB_ORGANIC)
		}
		g.ThrowGib(self, "models/objects/gibs/chest/tris.md2", damage, GIB_ORGANIC)
		g.ThrowHead(self, "models/objects/gibs/head2/tris.md2", damage, GIB_ORGANIC)
		self.Deadflag = DEAD_DEAD
		return
	}

	if self.Deadflag == DEAD_DEAD {
		return
	}

	// regular death
	self.Deadflag = DEAD_DEAD
	self.Takedamage = DAMAGE_YES
	self.S.SkinNum |= 1

	if self.S.SkinNum == 1 {
		g.gi.Sound(self, CHAN_VOICE, s.SoundDeathLight, 1, ATTN_NORM, 0)
	} else if self.S.SkinNum == 3 {
		g.gi.Sound(self, CHAN_VOICE, s.SoundDeath, 1, ATTN_NORM, 0)
	} else { // (self->s.skinnum == 5)
		g.gi.Sound(self, CHAN_VOICE, s.SoundDeathSs, 1, ATTN_NORM, 0)
	}

	if math.Abs(float64((self.S.Origin[2]+float32(self.Viewheight))-point[2])) <= 4 {
		// head shot
		self.Monsterinfo.Currentmove = soldier_move_death3
		return
	}

	n = g.rng.Rand() % 5
	if n == 0 {
		self.Monsterinfo.Currentmove = soldier_move_death1
	} else if n == 1 {
		self.Monsterinfo.Currentmove = soldier_move_death2
	} else if n == 2 {
		self.Monsterinfo.Currentmove = soldier_move_death4
	} else if n == 3 {
		self.Monsterinfo.Currentmove = soldier_move_death5
	} else {
		self.Monsterinfo.Currentmove = soldier_move_death6
	}
}

// SPAWN
//
// C: game/m_soldier.c:1200 SP_monster_soldier_x
func (g *Game) SP_monster_soldier_x(self *Edict) {
	s := monsterStatics[soldierStatics](g, "m_soldier")

	self.S.ModelIndex = int32(g.gi.ModelIndex("models/monsters/soldier/tris.md2"))
	self.Monsterinfo.Scale = soldier_MODEL_SCALE
	self.Mins = Vec3{-16, -16, -24}
	self.Maxs = Vec3{16, 16, 32}
	self.Movetype = MOVETYPE_STEP
	self.Solid = SOLID_BBOX

	s.SoundIdle = g.gi.SoundIndex("soldier/solidle1.wav")
	s.SoundSight1 = g.gi.SoundIndex("soldier/solsght1.wav")
	s.SoundSight2 = g.gi.SoundIndex("soldier/solsrch1.wav")
	s.SoundCock = g.gi.SoundIndex("infantry/infatck3.wav")

	self.Mass = 100

	self.Pain = soldier_pain
	self.Die = soldier_die

	self.Monsterinfo.Stand = soldier_stand
	self.Monsterinfo.Walk = soldier_walk
	self.Monsterinfo.Run = soldier_run
	self.Monsterinfo.Dodge = soldier_dodge
	self.Monsterinfo.Attack = soldier_attack
	self.Monsterinfo.Melee = nil
	self.Monsterinfo.Sight = soldier_sight

	g.gi.LinkEntity(self)

	self.Monsterinfo.Stand.fn(g, self)

	g.walkmonster_start(self)
}

// QUAKED monster_soldier_light (1 .5 0) (-16 -16 -24) (16 16 32) Ambush Trigger_Spawn Sight
//
// C: game/m_soldier.c:1238 SP_monster_soldier_light
func (g *Game) SP_monster_soldier_light(self *Edict) {
	s := monsterStatics[soldierStatics](g, "m_soldier")
	if g.deathmatch.Value != 0 {
		g.G_FreeEdict(self)
		return
	}

	g.SP_monster_soldier_x(self)

	s.SoundPainLight = g.gi.SoundIndex("soldier/solpain2.wav")
	s.SoundDeathLight = g.gi.SoundIndex("soldier/soldeth2.wav")
	g.gi.ModelIndex("models/objects/laser/tris.md2")
	g.gi.SoundIndex("misc/lasfly.wav")
	g.gi.SoundIndex("soldier/solatck2.wav")

	self.S.SkinNum = 0
	self.Health = 20
	self.GibHealth = -30
}

// QUAKED monster_soldier (1 .5 0) (-16 -16 -24) (16 16 32) Ambush Trigger_Spawn Sight
//
// C: game/m_soldier.c:1261 SP_monster_soldier
func (g *Game) SP_monster_soldier(self *Edict) {
	s := monsterStatics[soldierStatics](g, "m_soldier")
	if g.deathmatch.Value != 0 {
		g.G_FreeEdict(self)
		return
	}

	g.SP_monster_soldier_x(self)

	s.SoundPain = g.gi.SoundIndex("soldier/solpain1.wav")
	s.SoundDeath = g.gi.SoundIndex("soldier/soldeth1.wav")
	g.gi.SoundIndex("soldier/solatck1.wav")

	self.S.SkinNum = 2
	self.Health = 30
	self.GibHealth = -30
}

// QUAKED monster_soldier_ss (1 .5 0) (-16 -16 -24) (16 16 32) Ambush Trigger_Spawn Sight
//
// C: game/m_soldier.c:1282 SP_monster_soldier_ss
func (g *Game) SP_monster_soldier_ss(self *Edict) {
	s := monsterStatics[soldierStatics](g, "m_soldier")
	if g.deathmatch.Value != 0 {
		g.G_FreeEdict(self)
		return
	}

	g.SP_monster_soldier_x(self)

	s.SoundPainSs = g.gi.SoundIndex("soldier/solpain3.wav")
	s.SoundDeathSs = g.gi.SoundIndex("soldier/soldeth3.wav")
	g.gi.SoundIndex("soldier/solatck3.wav")

	self.S.SkinNum = 4
	self.Health = 40
	self.GibHealth = -30
}
