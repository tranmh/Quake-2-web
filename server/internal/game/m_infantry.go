package game

// Port of game/m_infantry.c.

import (
	. "quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/shared"
)

// statics of m_infantry.c
type infantryStatics struct {
	SoundPain1      int // sound_pain1
	SoundPain2      int // sound_pain2
	SoundDie1       int // sound_die1
	SoundDie2       int // sound_die2
	SoundGunshot    int // sound_gunshot
	SoundWeaponCock int // sound_weapon_cock
	SoundPunchSwing int // sound_punch_swing
	SoundPunchHit   int // sound_punch_hit
	SoundSight      int // sound_sight
	SoundSearch     int // sound_search
	SoundIdle       int // sound_idle
}

var (
	infantry_stand     = defThink("infantry_stand")
	infantry_fidget    = defThink("infantry_fidget")
	infantry_walk      = defThink("infantry_walk")
	infantry_run       = defThink("infantry_run")
	infantry_pain      = defPain("infantry_pain")
	InfantryMachineGun = defThink("InfantryMachineGun")
	infantry_sight     = defBlocked("infantry_sight")
	infantry_dead      = defThink("infantry_dead")
	infantry_die       = defDie("infantry_die")
	infantry_duck_down = defThink("infantry_duck_down")
	infantry_duck_hold = defThink("infantry_duck_hold")
	infantry_duck_up   = defThink("infantry_duck_up")
	infantry_dodge     = defDodge("infantry_dodge")
	infantry_cock_gun  = defThink("infantry_cock_gun")
	infantry_fire      = defThink("infantry_fire")
	infantry_swing     = defThink("infantry_swing")
	infantry_smack     = defThink("infantry_smack")
	infantry_attack    = defThink("infantry_attack")
)

func init() {
	infantry_stand.bind((*Game).infantry_stand)
	infantry_fidget.bind((*Game).infantry_fidget)
	infantry_walk.bind((*Game).infantry_walk)
	infantry_run.bind((*Game).infantry_run)
	infantry_pain.bind((*Game).infantry_pain)
	InfantryMachineGun.bind((*Game).InfantryMachineGun)
	infantry_sight.bind((*Game).infantry_sight)
	infantry_dead.bind((*Game).infantry_dead)
	infantry_die.bind((*Game).infantry_die)
	infantry_duck_down.bind((*Game).infantry_duck_down)
	infantry_duck_hold.bind((*Game).infantry_duck_hold)
	infantry_duck_up.bind((*Game).infantry_duck_up)
	infantry_dodge.bind((*Game).infantry_dodge)
	infantry_cock_gun.bind((*Game).infantry_cock_gun)
	infantry_fire.bind((*Game).infantry_fire)
	infantry_swing.bind((*Game).infantry_swing)
	infantry_smack.bind((*Game).infantry_smack)
	infantry_attack.bind((*Game).infantry_attack)
}

func init() {
	RegisterMonsterStatics("m_infantry", func() any { return new(infantryStatics) })
	RegisterSpawn("monster_infantry", (*Game).SP_monster_infantry)
}

// ==============================================================================
//
// # INFANTRY
//
// ==============================================================================
//
// C: game/m_infantry.c:48 infantry_frames_stand
var infantry_frames_stand = []MFrame{
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
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

// C: game/m_infantry.c:73 infantry_move_stand
var infantry_move_stand = defMMove("infantry_move_stand", infantry_FRAME_stand50, infantry_FRAME_stand71, infantry_frames_stand, nil)

// C: game/m_infantry.c:75 infantry_stand
func (g *Game) infantry_stand(self *Edict) {
	self.Monsterinfo.Currentmove = infantry_move_stand
}

// C: game/m_infantry.c:81 infantry_frames_fidget
var infantry_frames_fidget = []MFrame{
	{ai_stand, 1, nil},
	{ai_stand, 0, nil},
	{ai_stand, 1, nil},
	{ai_stand, 3, nil},
	{ai_stand, 6, nil},
	{ai_stand, 3, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 1, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 1, nil},
	{ai_stand, 0, nil},
	{ai_stand, -1, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 1, nil},
	{ai_stand, 0, nil},
	{ai_stand, -2, nil},
	{ai_stand, 1, nil},
	{ai_stand, 1, nil},
	{ai_stand, 1, nil},
	{ai_stand, -1, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, -1, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, -1, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 1, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, -1, nil},
	{ai_stand, -1, nil},
	{ai_stand, 0, nil},
	{ai_stand, -3, nil},
	{ai_stand, -2, nil},
	{ai_stand, -3, nil},
	{ai_stand, -3, nil},
	{ai_stand, -2, nil},
}

// C: game/m_infantry.c:133 infantry_move_fidget
var infantry_move_fidget = defMMove("infantry_move_fidget", infantry_FRAME_stand01, infantry_FRAME_stand49, infantry_frames_fidget, infantry_stand)

// C: game/m_infantry.c:135 infantry_fidget
func (g *Game) infantry_fidget(self *Edict) {
	s := monsterStatics[infantryStatics](g, "m_infantry")
	self.Monsterinfo.Currentmove = infantry_move_fidget
	g.gi.Sound(self, CHAN_VOICE, s.SoundIdle, 1, ATTN_IDLE, 0)
}

// C: game/m_infantry.c:141 infantry_frames_walk
var infantry_frames_walk = []MFrame{
	{ai_walk, 5, nil},
	{ai_walk, 4, nil},
	{ai_walk, 4, nil},
	{ai_walk, 5, nil},
	{ai_walk, 4, nil},
	{ai_walk, 5, nil},
	{ai_walk, 6, nil},
	{ai_walk, 4, nil},
	{ai_walk, 4, nil},
	{ai_walk, 4, nil},
	{ai_walk, 4, nil},
	{ai_walk, 5, nil},
}

// C: game/m_infantry.c:156 infantry_move_walk
var infantry_move_walk = defMMove("infantry_move_walk", infantry_FRAME_walk03, infantry_FRAME_walk14, infantry_frames_walk, nil)

// C: game/m_infantry.c:158 infantry_walk
func (g *Game) infantry_walk(self *Edict) {
	self.Monsterinfo.Currentmove = infantry_move_walk
}

// C: game/m_infantry.c:163 infantry_frames_run
var infantry_frames_run = []MFrame{
	{ai_run, 10, nil},
	{ai_run, 20, nil},
	{ai_run, 5, nil},
	{ai_run, 7, nil},
	{ai_run, 30, nil},
	{ai_run, 35, nil},
	{ai_run, 2, nil},
	{ai_run, 6, nil},
}

// C: game/m_infantry.c:174 infantry_move_run
var infantry_move_run = defMMove("infantry_move_run", infantry_FRAME_run01, infantry_FRAME_run08, infantry_frames_run, nil)

// C: game/m_infantry.c:176 infantry_run
func (g *Game) infantry_run(self *Edict) {
	if self.Monsterinfo.Aiflags&AI_STAND_GROUND != 0 {
		self.Monsterinfo.Currentmove = infantry_move_stand
	} else {
		self.Monsterinfo.Currentmove = infantry_move_run
	}
}

// C: game/m_infantry.c:185 infantry_frames_pain1
var infantry_frames_pain1 = []MFrame{
	{ai_move, -3, nil},
	{ai_move, -2, nil},
	{ai_move, -1, nil},
	{ai_move, -2, nil},
	{ai_move, -1, nil},
	{ai_move, 1, nil},
	{ai_move, -1, nil},
	{ai_move, 1, nil},
	{ai_move, 6, nil},
	{ai_move, 2, nil},
}

// C: game/m_infantry.c:198 infantry_move_pain1
var infantry_move_pain1 = defMMove("infantry_move_pain1", infantry_FRAME_pain101, infantry_FRAME_pain110, infantry_frames_pain1, infantry_run)

// C: game/m_infantry.c:200 infantry_frames_pain2
var infantry_frames_pain2 = []MFrame{
	{ai_move, -3, nil},
	{ai_move, -3, nil},
	{ai_move, 0, nil},
	{ai_move, -1, nil},
	{ai_move, -2, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 2, nil},
	{ai_move, 5, nil},
	{ai_move, 2, nil},
}

// C: game/m_infantry.c:213 infantry_move_pain2
var infantry_move_pain2 = defMMove("infantry_move_pain2", infantry_FRAME_pain201, infantry_FRAME_pain210, infantry_frames_pain2, infantry_run)

// C: game/m_infantry.c:215 infantry_pain
func (g *Game) infantry_pain(self *Edict, other *Edict, kick float32, damage int32) {
	s := monsterStatics[infantryStatics](g, "m_infantry")
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

	n = g.rng.Rand() % 2
	if n == 0 {
		self.Monsterinfo.Currentmove = infantry_move_pain1
		g.gi.Sound(self, CHAN_VOICE, s.SoundPain1, 1, ATTN_NORM, 0)
	} else {
		self.Monsterinfo.Currentmove = infantry_move_pain2
		g.gi.Sound(self, CHAN_VOICE, s.SoundPain2, 1, ATTN_NORM, 0)
	}
}

// aimangles is read-only data (C global vec3_t aimangles[]).
// C: game/m_infantry.c:244 aimangles
var aimangles = [...]Vec3{
	{0.0, 5.0, 0.0},
	{10.0, 15.0, 0.0},
	{20.0, 25.0, 0.0},
	{25.0, 35.0, 0.0},
	{30.0, 40.0, 0.0},
	{30.0, 45.0, 0.0},
	{25.0, 50.0, 0.0},
	{20.0, 40.0, 0.0},
	{15.0, 35.0, 0.0},
	{40.0, 35.0, 0.0},
	{70.0, 35.0, 0.0},
	{90.0, 35.0, 0.0},
}

// C: game/m_infantry.c:260 InfantryMachineGun
func (g *Game) InfantryMachineGun(self *Edict) {
	var start, target Vec3
	var forward, right Vec3
	var vec Vec3
	var flash_number int32

	if self.S.Frame == infantry_FRAME_attak111 {
		flash_number = MZ2_INFANTRY_MACHINEGUN_1
		shared.AngleVectors(self.S.Angles, &forward, &right, nil)
		start = G_ProjectSource(self.S.Origin, MonsterFlashOffset[flash_number], forward, right)

		if self.Enemy != nil {
			target = shared.VectorMA(self.Enemy.S.Origin, -0.2, self.Enemy.Velocity)
			target[2] += float32(self.Enemy.Viewheight)
			forward = shared.VectorSubtract(target, start)
			shared.VectorNormalize(&forward)
		} else {
			shared.AngleVectors(self.S.Angles, &forward, &right, nil)
		}
	} else {
		flash_number = MZ2_INFANTRY_MACHINEGUN_2 + (self.S.Frame - infantry_FRAME_death211)

		shared.AngleVectors(self.S.Angles, &forward, &right, nil)
		start = G_ProjectSource(self.S.Origin, MonsterFlashOffset[flash_number], forward, right)

		vec = shared.VectorSubtract(self.S.Angles, aimangles[flash_number-MZ2_INFANTRY_MACHINEGUN_2])
		shared.AngleVectors(vec, &forward, nil, nil)
	}

	g.monster_fire_bullet(self, start, forward, 3, 4, DEFAULT_BULLET_HSPREAD, DEFAULT_BULLET_VSPREAD, flash_number)
}

// C: game/m_infantry.c:299 infantry_sight
func (g *Game) infantry_sight(self *Edict, other *Edict) {
	s := monsterStatics[infantryStatics](g, "m_infantry")
	g.gi.Sound(self, CHAN_BODY, s.SoundSight, 1, ATTN_NORM, 0)
}

// C: game/m_infantry.c:304 infantry_dead
func (g *Game) infantry_dead(self *Edict) {
	self.Mins = Vec3{-16, -16, -24}
	self.Maxs = Vec3{16, 16, -8}
	self.Movetype = MOVETYPE_TOSS
	self.SVFlags |= SVF_DEADMONSTER
	g.gi.LinkEntity(self)

	g.M_FlyCheck(self)
}

// C: game/m_infantry.c:315 infantry_frames_death1
var infantry_frames_death1 = []MFrame{
	{ai_move, -4, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, -1, nil},
	{ai_move, -4, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, -1, nil},
	{ai_move, 3, nil},
	{ai_move, 1, nil},
	{ai_move, 1, nil},
	{ai_move, -2, nil},
	{ai_move, 2, nil},
	{ai_move, 2, nil},
	{ai_move, 9, nil},
	{ai_move, 9, nil},
	{ai_move, 5, nil},
	{ai_move, -3, nil},
	{ai_move, -3, nil},
}

// C: game/m_infantry.c:338 infantry_move_death1
var infantry_move_death1 = defMMove("infantry_move_death1", infantry_FRAME_death101, infantry_FRAME_death120, infantry_frames_death1, infantry_dead)

// Off with his head
// C: game/m_infantry.c:341 infantry_frames_death2
var infantry_frames_death2 = []MFrame{
	{ai_move, 0, nil},
	{ai_move, 1, nil},
	{ai_move, 5, nil},
	{ai_move, -1, nil},
	{ai_move, 0, nil},
	{ai_move, 1, nil},
	{ai_move, 1, nil},
	{ai_move, 4, nil},
	{ai_move, 3, nil},
	{ai_move, 0, nil},
	{ai_move, -2, InfantryMachineGun},
	{ai_move, -2, InfantryMachineGun},
	{ai_move, -3, InfantryMachineGun},
	{ai_move, -1, InfantryMachineGun},
	{ai_move, -2, InfantryMachineGun},
	{ai_move, 0, InfantryMachineGun},
	{ai_move, 2, InfantryMachineGun},
	{ai_move, 2, InfantryMachineGun},
	{ai_move, 3, InfantryMachineGun},
	{ai_move, -10, InfantryMachineGun},
	{ai_move, -7, InfantryMachineGun},
	{ai_move, -8, InfantryMachineGun},
	{ai_move, -6, nil},
	{ai_move, 4, nil},
	{ai_move, 0, nil},
}

// C: game/m_infantry.c:369 infantry_move_death2
var infantry_move_death2 = defMMove("infantry_move_death2", infantry_FRAME_death201, infantry_FRAME_death225, infantry_frames_death2, infantry_dead)

// C: game/m_infantry.c:371 infantry_frames_death3
var infantry_frames_death3 = []MFrame{
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, -6, nil},
	{ai_move, -11, nil},
	{ai_move, -3, nil},
	{ai_move, -11, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
}

// C: game/m_infantry.c:383 infantry_move_death3
var infantry_move_death3 = defMMove("infantry_move_death3", infantry_FRAME_death301, infantry_FRAME_death309, infantry_frames_death3, infantry_dead)

// C: game/m_infantry.c:386 infantry_die
func (g *Game) infantry_die(self *Edict, inflictor *Edict, attacker *Edict, damage int32, point Vec3) {
	s := monsterStatics[infantryStatics](g, "m_infantry")
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
	self.Deadflag = DEAD_DEAD
	self.Takedamage = DAMAGE_YES

	n = g.rng.Rand() % 3
	if n == 0 {
		self.Monsterinfo.Currentmove = infantry_move_death1
		g.gi.Sound(self, CHAN_VOICE, s.SoundDie2, 1, ATTN_NORM, 0)
	} else if n == 1 {
		self.Monsterinfo.Currentmove = infantry_move_death2
		g.gi.Sound(self, CHAN_VOICE, s.SoundDie1, 1, ATTN_NORM, 0)
	} else {
		self.Monsterinfo.Currentmove = infantry_move_death3
		g.gi.Sound(self, CHAN_VOICE, s.SoundDie2, 1, ATTN_NORM, 0)
	}
}

// C: game/m_infantry.c:429 infantry_duck_down
func (g *Game) infantry_duck_down(self *Edict) {
	if self.Monsterinfo.Aiflags&AI_DUCKED != 0 {
		return
	}
	self.Monsterinfo.Aiflags |= AI_DUCKED
	self.Maxs[2] -= 32
	self.Takedamage = DAMAGE_YES
	self.Monsterinfo.Pausetime = g.level.Time + 1
	g.gi.LinkEntity(self)
}

// C: game/m_infantry.c:440 infantry_duck_hold
func (g *Game) infantry_duck_hold(self *Edict) {
	if g.level.Time >= self.Monsterinfo.Pausetime {
		self.Monsterinfo.Aiflags &^= AI_HOLD_FRAME
	} else {
		self.Monsterinfo.Aiflags |= AI_HOLD_FRAME
	}
}

// C: game/m_infantry.c:448 infantry_duck_up
func (g *Game) infantry_duck_up(self *Edict) {
	self.Monsterinfo.Aiflags &^= AI_DUCKED
	self.Maxs[2] += 32
	self.Takedamage = DAMAGE_AIM
	g.gi.LinkEntity(self)
}

// C: game/m_infantry.c:456 infantry_frames_duck
var infantry_frames_duck = []MFrame{
	{ai_move, -2, infantry_duck_down},
	{ai_move, -5, infantry_duck_hold},
	{ai_move, 3, nil},
	{ai_move, 4, infantry_duck_up},
	{ai_move, 0, nil},
}

// C: game/m_infantry.c:464 infantry_move_duck
var infantry_move_duck = defMMove("infantry_move_duck", infantry_FRAME_duck01, infantry_FRAME_duck05, infantry_frames_duck, infantry_run)

// C: game/m_infantry.c:466 infantry_dodge
func (g *Game) infantry_dodge(self *Edict, attacker *Edict, eta float32) {
	if g.random() > 0.25 {
		return
	}

	if self.Enemy == nil {
		self.Enemy = attacker
	}

	self.Monsterinfo.Currentmove = infantry_move_duck
}

// C: game/m_infantry.c:478 infantry_cock_gun
func (g *Game) infantry_cock_gun(self *Edict) {
	s := monsterStatics[infantryStatics](g, "m_infantry")
	var n int32

	g.gi.Sound(self, CHAN_WEAPON, s.SoundWeaponCock, 1, ATTN_NORM, 0)
	n = (g.rng.Rand() & 15) + 3 + 7
	self.Monsterinfo.Pausetime = float32(float64(g.level.Time) + float64(n)*FRAMETIME)
}

// C: game/m_infantry.c:487 infantry_fire
func (g *Game) infantry_fire(self *Edict) {
	g.InfantryMachineGun(self)

	if g.level.Time >= self.Monsterinfo.Pausetime {
		self.Monsterinfo.Aiflags &^= AI_HOLD_FRAME
	} else {
		self.Monsterinfo.Aiflags |= AI_HOLD_FRAME
	}
}

// C: game/m_infantry.c:497 infantry_frames_attack1
var infantry_frames_attack1 = []MFrame{
	{ai_charge, 4, nil},
	{ai_charge, -1, nil},
	{ai_charge, -1, nil},
	{ai_charge, 0, infantry_cock_gun},
	{ai_charge, -1, nil},
	{ai_charge, 1, nil},
	{ai_charge, 1, nil},
	{ai_charge, 2, nil},
	{ai_charge, -2, nil},
	{ai_charge, -3, nil},
	{ai_charge, 1, infantry_fire},
	{ai_charge, 5, nil},
	{ai_charge, -1, nil},
	{ai_charge, -2, nil},
	{ai_charge, -3, nil},
}

// C: game/m_infantry.c:515 infantry_move_attack1
var infantry_move_attack1 = defMMove("infantry_move_attack1", infantry_FRAME_attak101, infantry_FRAME_attak115, infantry_frames_attack1, infantry_run)

// C: game/m_infantry.c:518 infantry_swing
func (g *Game) infantry_swing(self *Edict) {
	s := monsterStatics[infantryStatics](g, "m_infantry")
	g.gi.Sound(self, CHAN_WEAPON, s.SoundPunchSwing, 1, ATTN_NORM, 0)
}

// C: game/m_infantry.c:523 infantry_smack
func (g *Game) infantry_smack(self *Edict) {
	s := monsterStatics[infantryStatics](g, "m_infantry")
	var aim Vec3

	aim = Vec3{MELEE_DISTANCE, 0, 0}
	if g.fire_hit(self, aim, (5 + (g.rng.Rand() % 5)), 50) {
		g.gi.Sound(self, CHAN_WEAPON, s.SoundPunchHit, 1, ATTN_NORM, 0)
	}
}

// C: game/m_infantry.c:532 infantry_frames_attack2
var infantry_frames_attack2 = []MFrame{
	{ai_charge, 3, nil},
	{ai_charge, 6, nil},
	{ai_charge, 0, infantry_swing},
	{ai_charge, 8, nil},
	{ai_charge, 5, nil},
	{ai_charge, 8, infantry_smack},
	{ai_charge, 6, nil},
	{ai_charge, 3, nil},
}

// C: game/m_infantry.c:543 infantry_move_attack2
var infantry_move_attack2 = defMMove("infantry_move_attack2", infantry_FRAME_attak201, infantry_FRAME_attak208, infantry_frames_attack2, infantry_run)

// C: game/m_infantry.c:545 infantry_attack
func (g *Game) infantry_attack(self *Edict) {
	if g.range_(self, self.Enemy) == RANGE_MELEE {
		self.Monsterinfo.Currentmove = infantry_move_attack2
	} else {
		self.Monsterinfo.Currentmove = infantry_move_attack1
	}
}

// QUAKED monster_infantry (1 .5 0) (-16 -16 -24) (16 16 32) Ambush Trigger_Spawn Sight
//
// C: game/m_infantry.c:556 SP_monster_infantry
func (g *Game) SP_monster_infantry(self *Edict) {
	s := monsterStatics[infantryStatics](g, "m_infantry")
	if g.deathmatch.Value != 0 {
		g.G_FreeEdict(self)
		return
	}

	s.SoundPain1 = g.gi.SoundIndex("infantry/infpain1.wav")
	s.SoundPain2 = g.gi.SoundIndex("infantry/infpain2.wav")
	s.SoundDie1 = g.gi.SoundIndex("infantry/infdeth1.wav")
	s.SoundDie2 = g.gi.SoundIndex("infantry/infdeth2.wav")

	s.SoundGunshot = g.gi.SoundIndex("infantry/infatck1.wav")
	s.SoundWeaponCock = g.gi.SoundIndex("infantry/infatck3.wav")
	s.SoundPunchSwing = g.gi.SoundIndex("infantry/infatck2.wav")
	s.SoundPunchHit = g.gi.SoundIndex("infantry/melee2.wav")

	s.SoundSight = g.gi.SoundIndex("infantry/infsght1.wav")
	s.SoundSearch = g.gi.SoundIndex("infantry/infsrch1.wav")
	s.SoundIdle = g.gi.SoundIndex("infantry/infidle1.wav")

	self.Movetype = MOVETYPE_STEP
	self.Solid = SOLID_BBOX
	self.S.ModelIndex = int32(g.gi.ModelIndex("models/monsters/infantry/tris.md2"))
	self.Mins = Vec3{-16, -16, -24}
	self.Maxs = Vec3{16, 16, 32}

	self.Health = 100
	self.GibHealth = -40
	self.Mass = 200

	self.Pain = infantry_pain
	self.Die = infantry_die

	self.Monsterinfo.Stand = infantry_stand
	self.Monsterinfo.Walk = infantry_walk
	self.Monsterinfo.Run = infantry_run
	self.Monsterinfo.Dodge = infantry_dodge
	self.Monsterinfo.Attack = infantry_attack
	self.Monsterinfo.Melee = nil
	self.Monsterinfo.Sight = infantry_sight
	self.Monsterinfo.Idle = infantry_fidget

	g.gi.LinkEntity(self)

	self.Monsterinfo.Currentmove = infantry_move_stand
	self.Monsterinfo.Scale = infantry_MODEL_SCALE

	g.walkmonster_start(self)
}
