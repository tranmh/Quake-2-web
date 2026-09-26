package game

// Port of game/m_boss2.c.

import (
	. "quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/shared"
)

// statics of m_boss2.c
type boss2Statics struct {
	SoundPain1   int // sound_pain1
	SoundPain2   int // sound_pain2
	SoundPain3   int // sound_pain3
	SoundDeath   int // sound_death
	SoundSearch1 int // sound_search1
}

var (
	boss2_search      = defThink("boss2_search")
	Boss2Rocket       = defThink("Boss2Rocket")
	Boss2MachineGun   = defThink("Boss2MachineGun")
	boss2_stand       = defThink("boss2_stand")
	boss2_run         = defThink("boss2_run")
	boss2_walk        = defThink("boss2_walk")
	boss2_attack      = defThink("boss2_attack")
	boss2_attack_mg   = defThink("boss2_attack_mg")
	boss2_reattack_mg = defThink("boss2_reattack_mg")
	boss2_pain        = defPain("boss2_pain")
	boss2_dead        = defThink("boss2_dead")
	boss2_die         = defDie("boss2_die")
	Boss2_CheckAttack = defCheckAttack("Boss2_CheckAttack")
)

func init() {
	boss2_search.bind((*Game).boss2_search)
	Boss2Rocket.bind((*Game).Boss2Rocket)
	Boss2MachineGun.bind((*Game).Boss2MachineGun)
	boss2_stand.bind((*Game).boss2_stand)
	boss2_run.bind((*Game).boss2_run)
	boss2_walk.bind((*Game).boss2_walk)
	boss2_attack.bind((*Game).boss2_attack)
	boss2_attack_mg.bind((*Game).boss2_attack_mg)
	boss2_reattack_mg.bind((*Game).boss2_reattack_mg)
	boss2_pain.bind((*Game).boss2_pain)
	boss2_dead.bind((*Game).boss2_dead)
	boss2_die.bind((*Game).boss2_die)
	Boss2_CheckAttack.bind((*Game).Boss2_CheckAttack)
}

func init() {
	RegisterMonsterStatics("m_boss2", func() any { return new(boss2Statics) })
	RegisterSpawn("monster_boss2", (*Game).SP_monster_boss2)
}

// C: game/m_boss2.c:154 boss2_frames_stand
var boss2_frames_stand = []MFrame{
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
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

// C: game/m_boss2.c:180 boss2_frames_fidget
var boss2_frames_fidget = []MFrame{
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
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

// C: game/m_boss2.c:215 boss2_frames_walk
var boss2_frames_walk = []MFrame{
	{ai_walk, 8, nil},
	{ai_walk, 8, nil},
	{ai_walk, 8, nil},
	{ai_walk, 8, nil},
	{ai_walk, 8, nil},
	{ai_walk, 8, nil},
	{ai_walk, 8, nil},
	{ai_walk, 8, nil},
	{ai_walk, 8, nil},
	{ai_walk, 8, nil},
	{ai_walk, 8, nil},
	{ai_walk, 8, nil},
	{ai_walk, 8, nil},
	{ai_walk, 8, nil},
	{ai_walk, 8, nil},
	{ai_walk, 8, nil},
	{ai_walk, 8, nil},
	{ai_walk, 8, nil},
	{ai_walk, 8, nil},
	{ai_walk, 8, nil},
}

// C: game/m_boss2.c:241 boss2_frames_run
var boss2_frames_run = []MFrame{
	{ai_run, 8, nil},
	{ai_run, 8, nil},
	{ai_run, 8, nil},
	{ai_run, 8, nil},
	{ai_run, 8, nil},
	{ai_run, 8, nil},
	{ai_run, 8, nil},
	{ai_run, 8, nil},
	{ai_run, 8, nil},
	{ai_run, 8, nil},
	{ai_run, 8, nil},
	{ai_run, 8, nil},
	{ai_run, 8, nil},
	{ai_run, 8, nil},
	{ai_run, 8, nil},
	{ai_run, 8, nil},
	{ai_run, 8, nil},
	{ai_run, 8, nil},
	{ai_run, 8, nil},
	{ai_run, 8, nil},
}

// C: game/m_boss2.c:266 boss2_frames_attack_pre_mg
var boss2_frames_attack_pre_mg = []MFrame{
	{ai_charge, 1, nil},
	{ai_charge, 1, nil},
	{ai_charge, 1, nil},
	{ai_charge, 1, nil},
	{ai_charge, 1, nil},
	{ai_charge, 1, nil},
	{ai_charge, 1, nil},
	{ai_charge, 1, nil},
	{ai_charge, 1, boss2_attack_mg},
}

// C: game/m_boss2.c:282 boss2_frames_attack_mg
var boss2_frames_attack_mg = []MFrame{
	{ai_charge, 1, Boss2MachineGun},
	{ai_charge, 1, Boss2MachineGun},
	{ai_charge, 1, Boss2MachineGun},
	{ai_charge, 1, Boss2MachineGun},
	{ai_charge, 1, Boss2MachineGun},
	{ai_charge, 1, boss2_reattack_mg},
}

// C: game/m_boss2.c:293 boss2_frames_attack_post_mg
var boss2_frames_attack_post_mg = []MFrame{
	{ai_charge, 1, nil},
	{ai_charge, 1, nil},
	{ai_charge, 1, nil},
	{ai_charge, 1, nil},
}

// C: game/m_boss2.c:302 boss2_frames_attack_rocket
var boss2_frames_attack_rocket = []MFrame{
	{ai_charge, 1, nil},
	{ai_charge, 1, nil},
	{ai_charge, 1, nil},
	{ai_charge, 1, nil},
	{ai_charge, 1, nil},
	{ai_charge, 1, nil},
	{ai_charge, 1, nil},
	{ai_charge, 1, nil},
	{ai_charge, 1, nil},
	{ai_charge, 1, nil},
	{ai_charge, 1, nil},
	{ai_charge, 1, nil},
	{ai_move, -20, Boss2Rocket},
	{ai_charge, 1, nil},
	{ai_charge, 1, nil},
	{ai_charge, 1, nil},
	{ai_charge, 1, nil},
	{ai_charge, 1, nil},
	{ai_charge, 1, nil},
	{ai_charge, 1, nil},
	{ai_charge, 1, nil},
}

// C: game/m_boss2.c:328 boss2_frames_pain_heavy
var boss2_frames_pain_heavy = []MFrame{
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
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

// C: game/m_boss2.c:351 boss2_frames_pain_light
var boss2_frames_pain_light = []MFrame{
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
}

// C: game/m_boss2.c:360 boss2_frames_death
var boss2_frames_death = []MFrame{
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, BossExplode},
}

// C: game/m_boss2.c:178 boss2_move_stand
var boss2_move_stand = defMMove("boss2_move_stand", boss2_FRAME_stand30, boss2_FRAME_stand50, boss2_frames_stand, nil)

// C: game/m_boss2.c:213 boss2_move_fidget
//
// In C firstframe FRAME_stand1 (21) > lastframe FRAME_stand30 (0), which
// defMMove rejects; boss2_move_fidget is never used, so it is kept as a plain
// unregistered table.
var boss2_move_fidget = &MMove{Name: "boss2_move_fidget", Firstframe: boss2_FRAME_stand1, Lastframe: boss2_FRAME_stand30, Frame: boss2_frames_fidget}

// C: game/m_boss2.c:238 boss2_move_walk
var boss2_move_walk = defMMove("boss2_move_walk", boss2_FRAME_walk1, boss2_FRAME_walk20, boss2_frames_walk, nil)

// C: game/m_boss2.c:264 boss2_move_run
var boss2_move_run = defMMove("boss2_move_run", boss2_FRAME_walk1, boss2_FRAME_walk20, boss2_frames_run, nil)

// C: game/m_boss2.c:278 boss2_move_attack_pre_mg
var boss2_move_attack_pre_mg = defMMove("boss2_move_attack_pre_mg", boss2_FRAME_attack1, boss2_FRAME_attack9, boss2_frames_attack_pre_mg, nil)

// C: game/m_boss2.c:291 boss2_move_attack_mg
var boss2_move_attack_mg = defMMove("boss2_move_attack_mg", boss2_FRAME_attack10, boss2_FRAME_attack15, boss2_frames_attack_mg, nil)

// C: game/m_boss2.c:300 boss2_move_attack_post_mg
var boss2_move_attack_post_mg = defMMove("boss2_move_attack_post_mg", boss2_FRAME_attack16, boss2_FRAME_attack19, boss2_frames_attack_post_mg, boss2_run)

// C: game/m_boss2.c:326 boss2_move_attack_rocket
var boss2_move_attack_rocket = defMMove("boss2_move_attack_rocket", boss2_FRAME_attack20, boss2_FRAME_attack40, boss2_frames_attack_rocket, boss2_run)

// C: game/m_boss2.c:349 boss2_move_pain_heavy
var boss2_move_pain_heavy = defMMove("boss2_move_pain_heavy", boss2_FRAME_pain2, boss2_FRAME_pain19, boss2_frames_pain_heavy, boss2_run)

// C: game/m_boss2.c:358 boss2_move_pain_light
var boss2_move_pain_light = defMMove("boss2_move_pain_light", boss2_FRAME_pain20, boss2_FRAME_pain23, boss2_frames_pain_light, boss2_run)

// C: game/m_boss2.c:412 boss2_move_death
var boss2_move_death = defMMove("boss2_move_death", boss2_FRAME_death2, boss2_FRAME_death50, boss2_frames_death, boss2_dead)

// C: game/m_boss2.c:41 boss2_search
func (g *Game) boss2_search(self *Edict) {
	s := monsterStatics[boss2Statics](g, "m_boss2")
	if g.random() < 0.5 {
		g.gi.Sound(self, CHAN_VOICE, s.SoundSearch1, 1, ATTN_NONE, 0)
	}
}

// boss2FireRocket is one of the four blocks of Boss2Rocket.
func (g *Game) boss2FireRocket(self *Edict, forward, right Vec3, flash int32) {
	start := G_ProjectSource(self.S.Origin, MonsterFlashOffset[flash], forward, right)
	vec := self.Enemy.S.Origin
	vec[2] += float32(self.Enemy.Viewheight)
	dir := shared.VectorSubtract(vec, start)
	shared.VectorNormalize(&dir)
	g.monster_fire_rocket(self, start, dir, 50, 500, flash)
}

// C: game/m_boss2.c:55 Boss2Rocket
func (g *Game) Boss2Rocket(self *Edict) {
	var forward, right Vec3

	shared.AngleVectors(self.S.Angles, &forward, &right, nil)

	//1
	g.boss2FireRocket(self, forward, right, MZ2_BOSS2_ROCKET_1)
	//2
	g.boss2FireRocket(self, forward, right, MZ2_BOSS2_ROCKET_2)
	//3
	g.boss2FireRocket(self, forward, right, MZ2_BOSS2_ROCKET_3)
	//4
	g.boss2FireRocket(self, forward, right, MZ2_BOSS2_ROCKET_4)
}

// C: game/m_boss2.c:97 boss2_firebullet_right
func (g *Game) boss2_firebullet_right(self *Edict) {
	var forward, right, target Vec3
	var start Vec3

	shared.AngleVectors(self.S.Angles, &forward, &right, nil)
	start = G_ProjectSource(self.S.Origin, MonsterFlashOffset[MZ2_BOSS2_MACHINEGUN_R1], forward, right)

	target = shared.VectorMA(self.Enemy.S.Origin, -0.2, self.Enemy.Velocity)
	target[2] += float32(self.Enemy.Viewheight)
	forward = shared.VectorSubtract(target, start)
	shared.VectorNormalize(&forward)

	g.monster_fire_bullet(self, start, forward, 6, 4, DEFAULT_BULLET_HSPREAD, DEFAULT_BULLET_VSPREAD, MZ2_BOSS2_MACHINEGUN_R1)
}

// C: game/m_boss2.c:113 boss2_firebullet_left
func (g *Game) boss2_firebullet_left(self *Edict) {
	var forward, right, target Vec3
	var start Vec3

	shared.AngleVectors(self.S.Angles, &forward, &right, nil)
	start = G_ProjectSource(self.S.Origin, MonsterFlashOffset[MZ2_BOSS2_MACHINEGUN_L1], forward, right)

	target = shared.VectorMA(self.Enemy.S.Origin, -0.2, self.Enemy.Velocity)

	target[2] += float32(self.Enemy.Viewheight)
	forward = shared.VectorSubtract(target, start)
	shared.VectorNormalize(&forward)

	g.monster_fire_bullet(self, start, forward, 6, 4, DEFAULT_BULLET_HSPREAD, DEFAULT_BULLET_VSPREAD, MZ2_BOSS2_MACHINEGUN_L1)
}

// C: game/m_boss2.c:130 Boss2MachineGun
func (g *Game) Boss2MachineGun(self *Edict) {
	// (the per-frame flash version is commented out in C)
	g.boss2_firebullet_left(self)
	g.boss2_firebullet_right(self)
}

// C: game/m_boss2.c:414 boss2_stand
func (g *Game) boss2_stand(self *Edict) {
	self.Monsterinfo.Currentmove = boss2_move_stand
}

// C: game/m_boss2.c:419 boss2_run
func (g *Game) boss2_run(self *Edict) {
	if self.Monsterinfo.Aiflags&AI_STAND_GROUND != 0 {
		self.Monsterinfo.Currentmove = boss2_move_stand
	} else {
		self.Monsterinfo.Currentmove = boss2_move_run
	}
}

// C: game/m_boss2.c:427 boss2_walk
func (g *Game) boss2_walk(self *Edict) {
	self.Monsterinfo.Currentmove = boss2_move_walk
}

// C: game/m_boss2.c:432 boss2_attack
func (g *Game) boss2_attack(self *Edict) {
	var vec Vec3
	var range_ float32

	vec = shared.VectorSubtract(self.Enemy.S.Origin, self.S.Origin)
	range_ = shared.VectorLength(vec)

	if range_ <= 125 {
		self.Monsterinfo.Currentmove = boss2_move_attack_pre_mg
	} else {
		if g.random() <= 0.6 {
			self.Monsterinfo.Currentmove = boss2_move_attack_pre_mg
		} else {
			self.Monsterinfo.Currentmove = boss2_move_attack_rocket
		}
	}
}

// C: game/m_boss2.c:453 boss2_attack_mg
func (g *Game) boss2_attack_mg(self *Edict) {
	self.Monsterinfo.Currentmove = boss2_move_attack_mg
}

// C: game/m_boss2.c:458 boss2_reattack_mg
func (g *Game) boss2_reattack_mg(self *Edict) {
	if g.infront(self, self.Enemy) {
		if g.random() <= 0.7 {
			self.Monsterinfo.Currentmove = boss2_move_attack_mg
		} else {
			self.Monsterinfo.Currentmove = boss2_move_attack_post_mg
		}
	} else {
		self.Monsterinfo.Currentmove = boss2_move_attack_post_mg
	}
}

// C: game/m_boss2.c:470 boss2_pain
func (g *Game) boss2_pain(self *Edict, other *Edict, kick float32, damage int32) {
	s := monsterStatics[boss2Statics](g, "m_boss2")
	if self.Health < (self.MaxHealth / 2) {
		self.S.SkinNum = 1
	}

	if g.level.Time < self.PainDebounceTime {
		return
	}

	self.PainDebounceTime = g.level.Time + 3
	// American wanted these at no attenuation
	if damage < 10 {
		g.gi.Sound(self, CHAN_VOICE, s.SoundPain3, 1, ATTN_NONE, 0)
		self.Monsterinfo.Currentmove = boss2_move_pain_light
	} else if damage < 30 {
		g.gi.Sound(self, CHAN_VOICE, s.SoundPain1, 1, ATTN_NONE, 0)
		self.Monsterinfo.Currentmove = boss2_move_pain_light
	} else {
		g.gi.Sound(self, CHAN_VOICE, s.SoundPain2, 1, ATTN_NONE, 0)
		self.Monsterinfo.Currentmove = boss2_move_pain_heavy
	}
}

// C: game/m_boss2.c:497 boss2_dead
func (g *Game) boss2_dead(self *Edict) {
	self.Mins = Vec3{-56, -56, 0}
	self.Maxs = Vec3{56, 56, 80}
	self.Movetype = MOVETYPE_TOSS
	self.SVFlags |= SVF_DEADMONSTER
	self.Nextthink = 0
	g.gi.LinkEntity(self)
}

// C: game/m_boss2.c:507 boss2_die
func (g *Game) boss2_die(self *Edict, inflictor *Edict, attacker *Edict, damage int32, point Vec3) {
	s := monsterStatics[boss2Statics](g, "m_boss2")
	g.gi.Sound(self, CHAN_VOICE, s.SoundDeath, 1, ATTN_NONE, 0)
	self.Deadflag = DEAD_DEAD
	self.Takedamage = DAMAGE_NO
	self.Count = 0
	self.Monsterinfo.Currentmove = boss2_move_death
	// (#if 0 gib code in C omitted)
}

// C: game/m_boss2.c:540 Boss2_CheckAttack
func (g *Game) Boss2_CheckAttack(self *Edict) bool {
	var spot1, spot2 Vec3
	var temp Vec3
	var chance float32
	var enemy_range int32
	var enemy_yaw float32

	if self.Enemy.Health > 0 {
		// see if any entities are in the way of the shot
		spot1 = self.S.Origin
		spot1[2] += float32(self.Viewheight)
		spot2 = self.Enemy.S.Origin
		spot2[2] += float32(self.Enemy.Viewheight)

		tr := g.gi.Trace(&spot1, nil, nil, &spot2, self, CONTENTS_SOLID|CONTENTS_MONSTER|CONTENTS_SLIME|CONTENTS_LAVA)

		// do we have a clear shot?
		if tr.Ent != self.Enemy {
			return false
		}
	}

	g.infront(self, self.Enemy) // enemy_infront (unused)
	enemy_range = g.range_(self, self.Enemy)
	temp = shared.VectorSubtract(self.Enemy.S.Origin, self.S.Origin)
	enemy_yaw = vectoyaw(temp)

	self.IdealYaw = enemy_yaw

	// melee attack
	if enemy_range == RANGE_MELEE {
		if self.Monsterinfo.Melee != nil {
			self.Monsterinfo.AttackState = AS_MELEE
		} else {
			self.Monsterinfo.AttackState = AS_MISSILE
		}
		return true
	}

	// missile attack
	if self.Monsterinfo.Attack == nil {
		return false
	}

	if g.level.Time < self.Monsterinfo.AttackFinished {
		return false
	}

	if enemy_range == RANGE_FAR {
		return false
	}

	if self.Monsterinfo.Aiflags&AI_STAND_GROUND != 0 {
		chance = float32(0.4)
	} else if enemy_range == RANGE_MELEE {
		chance = float32(0.8)
	} else if enemy_range == RANGE_NEAR {
		chance = float32(0.8)
	} else if enemy_range == RANGE_MID {
		chance = float32(0.8)
	} else {
		return false
	}

	if g.random() < chance {
		self.Monsterinfo.AttackState = AS_MISSILE
		self.Monsterinfo.AttackFinished = g.level.Time + 2*g.random()
		return true
	}

	if self.Flags&FL_FLY != 0 {
		if g.random() < 0.3 {
			self.Monsterinfo.AttackState = AS_SLIDING
		} else {
			self.Monsterinfo.AttackState = AS_STRAIGHT
		}
	}

	return false
}

// QUAKED monster_boss2 (1 .5 0) (-56 -56 0) (56 56 80) Ambush Trigger_Spawn Sight
//
// C: game/m_boss2.c:636 SP_monster_boss2
func (g *Game) SP_monster_boss2(self *Edict) {
	s := monsterStatics[boss2Statics](g, "m_boss2")
	if g.deathmatch.Value != 0 {
		g.G_FreeEdict(self)
		return
	}

	s.SoundPain1 = g.gi.SoundIndex("bosshovr/bhvpain1.wav")
	s.SoundPain2 = g.gi.SoundIndex("bosshovr/bhvpain2.wav")
	s.SoundPain3 = g.gi.SoundIndex("bosshovr/bhvpain3.wav")
	s.SoundDeath = g.gi.SoundIndex("bosshovr/bhvdeth1.wav")
	s.SoundSearch1 = g.gi.SoundIndex("bosshovr/bhvunqv1.wav")

	self.S.Sound = int32(g.gi.SoundIndex("bosshovr/bhvengn1.wav"))

	self.Movetype = MOVETYPE_STEP
	self.Solid = SOLID_BBOX
	self.S.ModelIndex = int32(g.gi.ModelIndex("models/monsters/boss2/tris.md2"))
	self.Mins = Vec3{-56, -56, 0}
	self.Maxs = Vec3{56, 56, 80}

	self.Health = 2000
	self.GibHealth = -200
	self.Mass = 1000

	self.Flags |= FL_IMMUNE_LASER

	self.Pain = boss2_pain
	self.Die = boss2_die

	self.Monsterinfo.Stand = boss2_stand
	self.Monsterinfo.Walk = boss2_walk
	self.Monsterinfo.Run = boss2_run
	self.Monsterinfo.Attack = boss2_attack
	self.Monsterinfo.Search = boss2_search
	self.Monsterinfo.Checkattack = Boss2_CheckAttack
	g.gi.LinkEntity(self)

	self.Monsterinfo.Currentmove = boss2_move_stand
	self.Monsterinfo.Scale = boss2_MODEL_SCALE

	g.flymonster_start(self)
}
