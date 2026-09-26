package game

// Port of game/m_boss32.c (Makron -- final boss).

import (
	. "quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/shared"
)

// statics of m_boss32.c
type boss32Statics struct {
	SoundPain4        int // sound_pain4
	SoundPain5        int // sound_pain5
	SoundPain6        int // sound_pain6
	SoundDeath        int // sound_death
	SoundStepLeft     int // sound_step_left
	SoundStepRight    int // sound_step_right
	SoundAttackBfg    int // sound_attack_bfg
	SoundBrainsplorch int // sound_brainsplorch
	SoundPrerailgun   int // sound_prerailgun
	SoundPopup        int // sound_popup
	SoundTaunt1       int // sound_taunt1
	SoundTaunt2       int // sound_taunt2
	SoundTaunt3       int // sound_taunt3
	SoundHit          int // sound_hit
}

var (
	makron_taunt        = defThink("makron_taunt")
	makron_stand        = defThink("makron_stand")
	makron_hit          = defThink("makron_hit")
	makron_popup        = defThink("makron_popup")
	makron_step_left    = defThink("makron_step_left")
	makron_step_right   = defThink("makron_step_right")
	makron_brainsplorch = defThink("makron_brainsplorch")
	makron_prerailgun   = defThink("makron_prerailgun")
	makron_walk         = defThink("makron_walk")
	makron_run          = defThink("makron_run")
	makronBFG           = defThink("makronBFG")
	MakronSaveloc       = defThink("MakronSaveloc")
	MakronRailgun       = defThink("MakronRailgun")
	MakronHyperblaster  = defThink("MakronHyperblaster")
	makron_pain         = defPain("makron_pain")
	makron_sight        = defBlocked("makron_sight")
	makron_attack       = defThink("makron_attack")
	makron_torso_think  = defThink("makron_torso_think")
	makron_dead         = defThink("makron_dead")
	makron_die          = defDie("makron_die")
	Makron_CheckAttack  = defCheckAttack("Makron_CheckAttack")
	MakronSpawn         = defThink("MakronSpawn")
	MakronToss          = defThink("MakronToss")
)

func init() {
	makron_taunt.bind((*Game).makron_taunt)
	makron_stand.bind((*Game).makron_stand)
	makron_hit.bind((*Game).makron_hit)
	makron_popup.bind((*Game).makron_popup)
	makron_step_left.bind((*Game).makron_step_left)
	makron_step_right.bind((*Game).makron_step_right)
	makron_brainsplorch.bind((*Game).makron_brainsplorch)
	makron_prerailgun.bind((*Game).makron_prerailgun)
	makron_walk.bind((*Game).makron_walk)
	makron_run.bind((*Game).makron_run)
	makronBFG.bind((*Game).makronBFG)
	MakronSaveloc.bind((*Game).MakronSaveloc)
	MakronRailgun.bind((*Game).MakronRailgun)
	MakronHyperblaster.bind((*Game).MakronHyperblaster)
	makron_pain.bind((*Game).makron_pain)
	makron_sight.bind((*Game).makron_sight)
	makron_attack.bind((*Game).makron_attack)
	makron_torso_think.bind((*Game).makron_torso_think)
	makron_dead.bind((*Game).makron_dead)
	makron_die.bind((*Game).makron_die)
	Makron_CheckAttack.bind((*Game).Makron_CheckAttack)
	MakronSpawn.bind((*Game).MakronSpawn)
	MakronToss.bind((*Game).MakronToss)
}

// SP_monster_makron is not in the spawns[] table of g_spawn.c (Makron only
// appears through MakronToss / MakronSpawn), so it is not registered.
func init() {
	RegisterMonsterStatics("m_boss32", func() any { return new(boss32Statics) })
}

func boss32S(g *Game) *boss32Statics { return monsterStatics[boss32Statics](g, "m_boss32") }

// C: game/m_boss32.c:56 makron_taunt
func (g *Game) makron_taunt(self *Edict) {
	s := boss32S(g)
	r := g.random()
	if float64(r) <= 0.3 {
		g.gi.Sound(self, CHAN_AUTO, s.SoundTaunt1, 1, ATTN_NONE, 0)
	} else if float64(r) <= 0.6 {
		g.gi.Sound(self, CHAN_AUTO, s.SoundTaunt2, 1, ATTN_NONE, 0)
	} else {
		g.gi.Sound(self, CHAN_AUTO, s.SoundTaunt3, 1, ATTN_NONE, 0)
	}
}

// C: game/m_boss32.c:138 makron_stand
func (g *Game) makron_stand(self *Edict) {
	self.Monsterinfo.Currentmove = makron_move_stand
}

// C: game/m_boss32.c:158 makron_hit
func (g *Game) makron_hit(self *Edict) {
	g.gi.Sound(self, CHAN_AUTO, boss32S(g).SoundHit, 1, ATTN_NONE, 0)
}

// C: game/m_boss32.c:163 makron_popup
func (g *Game) makron_popup(self *Edict) {
	g.gi.Sound(self, CHAN_BODY, boss32S(g).SoundPopup, 1, ATTN_NONE, 0)
}

// C: game/m_boss32.c:168 makron_step_left
func (g *Game) makron_step_left(self *Edict) {
	g.gi.Sound(self, CHAN_BODY, boss32S(g).SoundStepLeft, 1, ATTN_NORM, 0)
}

// C: game/m_boss32.c:173 makron_step_right
func (g *Game) makron_step_right(self *Edict) {
	g.gi.Sound(self, CHAN_BODY, boss32S(g).SoundStepRight, 1, ATTN_NORM, 0)
}

// C: game/m_boss32.c:178 makron_brainsplorch
func (g *Game) makron_brainsplorch(self *Edict) {
	g.gi.Sound(self, CHAN_VOICE, boss32S(g).SoundBrainsplorch, 1, ATTN_NORM, 0)
}

// C: game/m_boss32.c:183 makron_prerailgun
func (g *Game) makron_prerailgun(self *Edict) {
	g.gi.Sound(self, CHAN_WEAPON, boss32S(g).SoundPrerailgun, 1, ATTN_NORM, 0)
}

// C: game/m_boss32.c:204 makron_walk
func (g *Game) makron_walk(self *Edict) {
	self.Monsterinfo.Currentmove = makron_move_walk
}

// C: game/m_boss32.c:209 makron_run
func (g *Game) makron_run(self *Edict) {
	if self.Monsterinfo.Aiflags&AI_STAND_GROUND != 0 {
		self.Monsterinfo.Currentmove = makron_move_stand
	} else {
		self.Monsterinfo.Currentmove = makron_move_run
	}
}

// C: game/m_boss32.c:410 makronBFG
func (g *Game) makronBFG(self *Edict) {
	var forward, right Vec3

	shared.AngleVectors(self.S.Angles, &forward, &right, nil)
	start := G_ProjectSource(self.S.Origin, MonsterFlashOffset[MZ2_MAKRON_BFG], forward, right)

	vec := self.Enemy.S.Origin
	vec[2] += float32(self.Enemy.Viewheight)
	dir := shared.VectorSubtract(vec, start)
	shared.VectorNormalize(&dir)
	g.gi.Sound(self, CHAN_VOICE, boss32S(g).SoundAttackBfg, 1, ATTN_NORM, 0)
	g.monster_fire_bfg(self, start, dir, 50, 300, 100, 300, MZ2_MAKRON_BFG)
}

// C: game/m_boss32.c:494 MakronSaveloc
func (g *Game) MakronSaveloc(self *Edict) {
	self.Pos1 = self.Enemy.S.Origin // save for aiming the shot
	self.Pos1[2] += float32(self.Enemy.Viewheight)
}

// FIXME: He's not firing from the proper Z
// C: game/m_boss32.c:501 MakronRailgun
func (g *Game) MakronRailgun(self *Edict) {
	var forward, right Vec3

	shared.AngleVectors(self.S.Angles, &forward, &right, nil)
	start := G_ProjectSource(self.S.Origin, MonsterFlashOffset[MZ2_MAKRON_RAILGUN_1], forward, right)

	// calc direction to where we targted
	dir := shared.VectorSubtract(self.Pos1, start)
	shared.VectorNormalize(&dir)

	g.monster_fire_railgun(self, start, dir, 50, 100, MZ2_MAKRON_RAILGUN_1)
}

// FIXME: This is all wrong. He's not firing at the proper angles.
// C: game/m_boss32.c:518 MakronHyperblaster
func (g *Game) MakronHyperblaster(self *Edict) {
	var dir, vec Vec3
	var forward, right Vec3

	flash_number := MZ2_MAKRON_BLASTER_1 + (self.S.Frame - boss32_FRAME_attak405)

	shared.AngleVectors(self.S.Angles, &forward, &right, nil)
	start := G_ProjectSource(self.S.Origin, MonsterFlashOffset[flash_number], forward, right)

	if self.Enemy != nil {
		vec = self.Enemy.S.Origin
		vec[2] += float32(self.Enemy.Viewheight)
		vec = shared.VectorSubtract(vec, start)
		vec = vectoangles(vec)
		dir[0] = vec[0]
	} else {
		dir[0] = 0
	}
	if self.S.Frame <= boss32_FRAME_attak413 {
		dir[1] = self.S.Angles[1] - float32(10*(self.S.Frame-boss32_FRAME_attak413))
	} else {
		dir[1] = self.S.Angles[1] + float32(10*(self.S.Frame-boss32_FRAME_attak421))
	}
	dir[2] = 0

	shared.AngleVectors(dir, &forward, nil, nil)

	g.monster_fire_blaster(self, start, forward, 15, 1000, MZ2_MAKRON_BLASTER_1, EF_BLASTER)
}

// C: game/m_boss32.c:555 makron_pain
func (g *Game) makron_pain(self *Edict, other *Edict, kick float32, damage int32) {
	s := boss32S(g)
	if self.Health < (self.MaxHealth / 2) {
		self.S.SkinNum = 1
	}

	if g.level.Time < self.PainDebounceTime {
		return
	}

	// Lessen the chance of him going into his pain frames
	if damage <= 25 {
		if float64(g.random()) < 0.2 {
			return
		}
	}

	self.PainDebounceTime = g.level.Time + 3
	if g.skill.Value == 3 {
		return // no pain anims in nightmare
	}

	if damage <= 40 {
		g.gi.Sound(self, CHAN_VOICE, s.SoundPain4, 1, ATTN_NONE, 0)
		self.Monsterinfo.Currentmove = makron_move_pain4
	} else if damage <= 110 {
		g.gi.Sound(self, CHAN_VOICE, s.SoundPain5, 1, ATTN_NONE, 0)
		self.Monsterinfo.Currentmove = makron_move_pain5
	} else {
		// the C "else" binds to the inner if (dangling else): nothing
		// happens for damage > 150.
		if damage <= 150 {
			if float64(g.random()) <= 0.45 {
				g.gi.Sound(self, CHAN_VOICE, s.SoundPain6, 1, ATTN_NONE, 0)
				self.Monsterinfo.Currentmove = makron_move_pain6
			} else if float64(g.random()) <= 0.35 {
				g.gi.Sound(self, CHAN_VOICE, s.SoundPain6, 1, ATTN_NONE, 0)
				self.Monsterinfo.Currentmove = makron_move_pain6
			}
		}
	}
}

// C: game/m_boss32.c:601 makron_sight
func (g *Game) makron_sight(self *Edict, other *Edict) {
	self.Monsterinfo.Currentmove = makron_move_sight
}

// C: game/m_boss32.c:606 makron_attack
func (g *Game) makron_attack(self *Edict) {
	r := g.random()

	vec := shared.VectorSubtract(self.Enemy.S.Origin, self.S.Origin)
	_ = shared.VectorLength(vec) // range (unused)

	if float64(r) <= 0.3 {
		self.Monsterinfo.Currentmove = makron_move_attack3
	} else if float64(r) <= 0.6 {
		self.Monsterinfo.Currentmove = makron_move_attack4
	} else {
		self.Monsterinfo.Currentmove = makron_move_attack5
	}
}

//
// Makron Torso. This needs to be spawned in
//

// C: game/m_boss32.c:632 makron_torso_think
func (g *Game) makron_torso_think(self *Edict) {
	self.S.Frame++
	if self.S.Frame < 365 {
		self.Nextthink = float32(float64(g.level.Time) + FRAMETIME)
	} else {
		self.S.Frame = 346
		self.Nextthink = float32(float64(g.level.Time) + FRAMETIME)
	}
}

// C: game/m_boss32.c:643 makron_torso
func (g *Game) makron_torso(ent *Edict) {
	ent.Movetype = MOVETYPE_NONE
	ent.Solid = SOLID_NOT
	ent.Mins = Vec3{-8, -8, 0}
	ent.Maxs = Vec3{8, 8, 8}
	ent.S.Frame = 346
	ent.S.ModelIndex = int32(g.gi.ModelIndex("models/monsters/boss3/rider/tris.md2"))
	ent.Think = makron_torso_think
	ent.Nextthink = float32(float64(g.level.Time) + 2*FRAMETIME)
	ent.S.Sound = int32(g.gi.SoundIndex("makron/spine.wav"))
	g.gi.LinkEntity(ent)
}

//
// death
//

// C: game/m_boss32.c:662 makron_dead
func (g *Game) makron_dead(self *Edict) {
	self.Mins = Vec3{-60, -60, 0}
	self.Maxs = Vec3{60, 60, 72}
	self.Movetype = MOVETYPE_TOSS
	self.SVFlags |= SVF_DEADMONSTER
	self.Nextthink = 0
	g.gi.LinkEntity(self)
}

// C: game/m_boss32.c:673 makron_die
func (g *Game) makron_die(self *Edict, inflictor *Edict, attacker *Edict, damage int32, point Vec3) {
	self.S.Sound = 0
	// check for gib
	if self.Health <= self.GibHealth {
		g.gi.Sound(self, CHAN_VOICE, g.gi.SoundIndex("misc/udeath.wav"), 1, ATTN_NORM, 0)
		for n := 0; n < 1; /*4*/ n++ {
			g.ThrowGib(self, "models/objects/gibs/sm_meat/tris.md2", damage, GIB_ORGANIC)
		}
		for n := 0; n < 4; n++ {
			g.ThrowGib(self, "models/objects/gibs/sm_metal/tris.md2", damage, GIB_METALLIC)
		}
		g.ThrowHead(self, "models/objects/gibs/gear/tris.md2", damage, GIB_METALLIC)
		self.Deadflag = DEAD_DEAD
		return
	}

	if self.Deadflag == DEAD_DEAD {
		return
	}

	// regular death
	g.gi.Sound(self, CHAN_VOICE, boss32S(g).SoundDeath, 1, ATTN_NONE, 0)
	self.Deadflag = DEAD_DEAD
	self.Takedamage = DAMAGE_YES

	tempent := g.G_Spawn()
	tempent.S.Origin = self.S.Origin
	tempent.S.Angles = self.S.Angles
	tempent.S.Origin[1] -= 84
	g.makron_torso(tempent)

	self.Monsterinfo.Currentmove = makron_move_death2
}

// C: game/m_boss32.c:711 Makron_CheckAttack
func (g *Game) Makron_CheckAttack(self *Edict) bool {
	var chance float32

	if self.Enemy.Health > 0 {
		// see if any entities are in the way of the shot
		spot1 := self.S.Origin
		spot1[2] += float32(self.Viewheight)
		spot2 := self.Enemy.S.Origin
		spot2[2] += float32(self.Enemy.Viewheight)

		tr := g.gi.Trace(&spot1, nil, nil, &spot2, self, CONTENTS_SOLID|CONTENTS_MONSTER|CONTENTS_SLIME|CONTENTS_LAVA)

		// do we have a clear shot?
		if tr.Ent != self.Enemy {
			return false
		}
	}

	_ = g.infront(self, self.Enemy) // enemy_infront (unused)
	enemy_range := g.range_(self, self.Enemy)
	temp := shared.VectorSubtract(self.Enemy.S.Origin, self.S.Origin)
	enemy_yaw := vectoyaw(temp)

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
		chance = 0.4
	} else if enemy_range == RANGE_MELEE {
		chance = 0.8
	} else if enemy_range == RANGE_NEAR {
		chance = 0.4
	} else if enemy_range == RANGE_MID {
		chance = 0.2
	} else {
		return false
	}

	if g.random() < chance {
		self.Monsterinfo.AttackState = AS_MISSILE
		self.Monsterinfo.AttackFinished = g.level.Time + 2*g.random()
		return true
	}

	if self.Flags&FL_FLY != 0 {
		if float64(g.random()) < 0.3 {
			self.Monsterinfo.AttackState = AS_SLIDING
		} else {
			self.Monsterinfo.AttackState = AS_STRAIGHT
		}
	}

	return false
}

//
// monster_makron
//

// C: game/m_boss32.c:808 MakronPrecache
func (g *Game) MakronPrecache() {
	s := boss32S(g)
	s.SoundPain4 = g.gi.SoundIndex("makron/pain3.wav")
	s.SoundPain5 = g.gi.SoundIndex("makron/pain2.wav")
	s.SoundPain6 = g.gi.SoundIndex("makron/pain1.wav")
	s.SoundDeath = g.gi.SoundIndex("makron/death.wav")
	s.SoundStepLeft = g.gi.SoundIndex("makron/step1.wav")
	s.SoundStepRight = g.gi.SoundIndex("makron/step2.wav")
	s.SoundAttackBfg = g.gi.SoundIndex("makron/bfg_fire.wav")
	s.SoundBrainsplorch = g.gi.SoundIndex("makron/brain1.wav")
	s.SoundPrerailgun = g.gi.SoundIndex("makron/rail_up.wav")
	s.SoundPopup = g.gi.SoundIndex("makron/popup.wav")
	s.SoundTaunt1 = g.gi.SoundIndex("makron/voice4.wav")
	s.SoundTaunt2 = g.gi.SoundIndex("makron/voice3.wav")
	s.SoundTaunt3 = g.gi.SoundIndex("makron/voice.wav")
	s.SoundHit = g.gi.SoundIndex("makron/bhit.wav")

	g.gi.ModelIndex("models/monsters/boss3/rider/tris.md2")
}

// QUAKED monster_makron (1 .5 0) (-30 -30 0) (30 30 90) Ambush Trigger_Spawn Sight
// C: game/m_boss32.c:830 SP_monster_makron
func (g *Game) SP_monster_makron(self *Edict) {
	if g.deathmatch.Value != 0 {
		g.G_FreeEdict(self)
		return
	}

	g.MakronPrecache()

	self.Movetype = MOVETYPE_STEP
	self.Solid = SOLID_BBOX
	self.S.ModelIndex = int32(g.gi.ModelIndex("models/monsters/boss3/rider/tris.md2"))
	self.Mins = Vec3{-30, -30, 0}
	self.Maxs = Vec3{30, 30, 90}

	self.Health = 3000
	self.GibHealth = -2000
	self.Mass = 500

	self.Pain = makron_pain
	self.Die = makron_die
	self.Monsterinfo.Stand = makron_stand
	self.Monsterinfo.Walk = makron_walk
	self.Monsterinfo.Run = makron_run
	self.Monsterinfo.Dodge = nil
	self.Monsterinfo.Attack = makron_attack
	self.Monsterinfo.Melee = nil
	self.Monsterinfo.Sight = makron_sight
	self.Monsterinfo.Checkattack = Makron_CheckAttack

	g.gi.LinkEntity(self)

	//	self->monsterinfo.currentmove = &makron_move_stand;
	self.Monsterinfo.Currentmove = makron_move_sight
	self.Monsterinfo.Scale = boss32_MODEL_SCALE

	g.walkmonster_start(self)
}

// C: game/m_boss32.c:877 MakronSpawn
func (g *Game) MakronSpawn(self *Edict) {
	g.SP_monster_makron(self)

	// jump at player
	player := g.level.SightClient
	if player == nil {
		return
	}

	vec := shared.VectorSubtract(player.S.Origin, self.S.Origin)
	self.S.Angles[YAW] = vectoyaw(vec)
	shared.VectorNormalize(&vec)
	self.Velocity = shared.VectorMA(shared.Vec3Origin, 400, vec)
	self.Velocity[2] = 200
	self.Groundentity = nil
}

// MakronToss: Jorg is just about dead, so set up to launch Makron out
// C: game/m_boss32.c:904 MakronToss
func (g *Game) MakronToss(self *Edict) {
	ent := g.G_Spawn()
	ent.Nextthink = float32(float64(g.level.Time) + 0.8)
	ent.Think = MakronSpawn
	ent.Target = self.Target
	ent.S.Origin = self.S.Origin
}

// C: game/m_boss32.c:73 makron_frames_stand
var makron_frames_stand = []MFrame{
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
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

// C: game/m_boss32.c:143 makron_frames_run
var makron_frames_run = []MFrame{
	{ai_run, 3, makron_step_left},
	{ai_run, 12, nil},
	{ai_run, 8, nil},
	{ai_run, 8, nil},
	{ai_run, 8, makron_step_right},
	{ai_run, 6, nil},
	{ai_run, 12, nil},
	{ai_run, 9, nil},
	{ai_run, 6, nil},
	{ai_run, 12, nil},
}

// C: game/m_boss32.c:189 makron_frames_walk
var makron_frames_walk = []MFrame{
	{ai_walk, 3, makron_step_left},
	{ai_walk, 12, nil},
	{ai_walk, 8, nil},
	{ai_walk, 8, nil},
	{ai_walk, 8, makron_step_right},
	{ai_walk, 6, nil},
	{ai_walk, 12, nil},
	{ai_walk, 9, nil},
	{ai_walk, 6, nil},
	{ai_walk, 12, nil},
}

// C: game/m_boss32.c:217 makron_frames_pain6
var makron_frames_pain6 = []MFrame{
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, makron_popup},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, makron_taunt},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
}

// C: game/m_boss32.c:249 makron_frames_pain5
var makron_frames_pain5 = []MFrame{
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
}

// C: game/m_boss32.c:258 makron_frames_pain4
var makron_frames_pain4 = []MFrame{
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
}

// C: game/m_boss32.c:267 makron_frames_death2
var makron_frames_death2 = []MFrame{
	{ai_move, -15, nil},
	{ai_move, 3, nil},
	{ai_move, -12, nil},
	{ai_move, 0, makron_step_left},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 11, nil},
	{ai_move, 12, nil},
	{ai_move, 11, makron_step_right},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 5, nil},
	{ai_move, 7, nil},
	{ai_move, 6, makron_step_left},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, -1, nil},
	{ai_move, 2, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, -6, nil},
	{ai_move, -4, nil},
	{ai_move, -6, makron_step_right},
	{ai_move, -4, nil},
	{ai_move, -4, makron_step_left},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, -2, nil},
	{ai_move, -5, nil},
	{ai_move, -3, makron_step_right},
	{ai_move, -8, nil},
	{ai_move, -3, makron_step_left},
	{ai_move, -7, nil},
	{ai_move, -4, nil},
	{ai_move, -4, makron_step_right},
	{ai_move, -6, nil},
	{ai_move, -7, nil},
	{ai_move, 0, makron_step_left},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, -2, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 2, nil},
	{ai_move, 0, nil},
	{ai_move, 27, makron_hit},
	{ai_move, 26, nil},
	{ai_move, 0, makron_brainsplorch},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
}

// C: game/m_boss32.c:367 makron_frames_death3
var makron_frames_death3 = []MFrame{
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
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

// C: game/m_boss32.c:392 makron_frames_sight
var makron_frames_sight = []MFrame{
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
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

// C: game/m_boss32.c:429 makron_frames_attack3
var makron_frames_attack3 = []MFrame{
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, makronBFG},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
}

// C: game/m_boss32.c:442 makron_frames_attack4
var makron_frames_attack4 = []MFrame{
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_move, 0, MakronHyperblaster},
	{ai_move, 0, MakronHyperblaster},
	{ai_move, 0, MakronHyperblaster},
	{ai_move, 0, MakronHyperblaster},
	{ai_move, 0, MakronHyperblaster},
	{ai_move, 0, MakronHyperblaster},
	{ai_move, 0, MakronHyperblaster},
	{ai_move, 0, MakronHyperblaster},
	{ai_move, 0, MakronHyperblaster},
	{ai_move, 0, MakronHyperblaster},
	{ai_move, 0, MakronHyperblaster},
	{ai_move, 0, MakronHyperblaster},
	{ai_move, 0, MakronHyperblaster},
	{ai_move, 0, MakronHyperblaster},
	{ai_move, 0, MakronHyperblaster},
	{ai_move, 0, MakronHyperblaster},
	{ai_move, 0, MakronHyperblaster},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
}

// C: game/m_boss32.c:473 makron_frames_attack5
var makron_frames_attack5 = []MFrame{
	{ai_charge, 0, makron_prerailgun},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, MakronSaveloc},
	{ai_move, 0, MakronRailgun},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
}

// C: game/m_boss32.c:136 makron_move_stand
var makron_move_stand = defMMove("makron_move_stand", boss32_FRAME_stand201, boss32_FRAME_stand260, makron_frames_stand, nil)

// C: game/m_boss32.c:156 makron_move_run
var makron_move_run = defMMove("makron_move_run", boss32_FRAME_walk204, boss32_FRAME_walk213, makron_frames_run, nil)

// C: game/m_boss32.c:202 makron_move_walk
var makron_move_walk = defMMove("makron_move_walk", boss32_FRAME_walk204, boss32_FRAME_walk213, makron_frames_run, nil)

// C: game/m_boss32.c:247 makron_move_pain6
var makron_move_pain6 = defMMove("makron_move_pain6", boss32_FRAME_pain601, boss32_FRAME_pain627, makron_frames_pain6, makron_run)

// C: game/m_boss32.c:256 makron_move_pain5
var makron_move_pain5 = defMMove("makron_move_pain5", boss32_FRAME_pain501, boss32_FRAME_pain504, makron_frames_pain5, makron_run)

// C: game/m_boss32.c:265 makron_move_pain4
var makron_move_pain4 = defMMove("makron_move_pain4", boss32_FRAME_pain401, boss32_FRAME_pain404, makron_frames_pain4, makron_run)

// C: game/m_boss32.c:365 makron_move_death2
var makron_move_death2 = defMMove("makron_move_death2", boss32_FRAME_death201, boss32_FRAME_death295, makron_frames_death2, makron_dead)

// C: game/m_boss32.c:390 makron_move_death3
var makron_move_death3 = defMMove("makron_move_death3", boss32_FRAME_death301, boss32_FRAME_death320, makron_frames_death3, nil)

// C: game/m_boss32.c:408 makron_move_sight
var makron_move_sight = defMMove("makron_move_sight", boss32_FRAME_active01, boss32_FRAME_active13, makron_frames_sight, makron_run)

// C: game/m_boss32.c:440 makron_move_attack3
var makron_move_attack3 = defMMove("makron_move_attack3", boss32_FRAME_attak301, boss32_FRAME_attak308, makron_frames_attack3, makron_run)

// C: game/m_boss32.c:471 makron_move_attack4
var makron_move_attack4 = defMMove("makron_move_attack4", boss32_FRAME_attak401, boss32_FRAME_attak426, makron_frames_attack4, makron_run)

// C: game/m_boss32.c:492 makron_move_attack5
var makron_move_attack5 = defMMove("makron_move_attack5", boss32_FRAME_attak501, boss32_FRAME_attak516, makron_frames_attack5, makron_run)
