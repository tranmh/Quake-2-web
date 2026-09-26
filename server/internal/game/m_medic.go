package game

// Port of game/m_medic.c.

import (
	"math"

	. "quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/shared"
)

// statics of m_medic.c
type medicStatics struct {
	SoundIdle1       int // sound_idle1
	SoundPain1       int // sound_pain1
	SoundPain2       int // sound_pain2
	SoundDie         int // sound_die
	SoundSight       int // sound_sight
	SoundSearch      int // sound_search
	SoundHookLaunch  int // sound_hook_launch
	SoundHookHit     int // sound_hook_hit
	SoundHookHeal    int // sound_hook_heal
	SoundHookRetract int // sound_hook_retract
}

var (
	medic_idle         = defThink("medic_idle")
	medic_search       = defThink("medic_search")
	medic_sight        = defBlocked("medic_sight")
	medic_stand        = defThink("medic_stand")
	medic_walk         = defThink("medic_walk")
	medic_run          = defThink("medic_run")
	medic_pain         = defPain("medic_pain")
	medic_fire_blaster = defThink("medic_fire_blaster")
	medic_dead         = defThink("medic_dead")
	medic_die          = defDie("medic_die")
	medic_duck_down    = defThink("medic_duck_down")
	medic_duck_hold    = defThink("medic_duck_hold")
	medic_duck_up      = defThink("medic_duck_up")
	medic_dodge        = defDodge("medic_dodge")
	medic_continue     = defThink("medic_continue")
	medic_hook_launch  = defThink("medic_hook_launch")
	medic_cable_attack = defThink("medic_cable_attack")
	medic_hook_retract = defThink("medic_hook_retract")
	medic_attack       = defThink("medic_attack")
	medic_checkattack  = defCheckAttack("medic_checkattack")
)

func init() {
	medic_idle.bind((*Game).medic_idle)
	medic_search.bind((*Game).medic_search)
	medic_sight.bind((*Game).medic_sight)
	medic_stand.bind((*Game).medic_stand)
	medic_walk.bind((*Game).medic_walk)
	medic_run.bind((*Game).medic_run)
	medic_pain.bind((*Game).medic_pain)
	medic_fire_blaster.bind((*Game).medic_fire_blaster)
	medic_dead.bind((*Game).medic_dead)
	medic_die.bind((*Game).medic_die)
	medic_duck_down.bind((*Game).medic_duck_down)
	medic_duck_hold.bind((*Game).medic_duck_hold)
	medic_duck_up.bind((*Game).medic_duck_up)
	medic_dodge.bind((*Game).medic_dodge)
	medic_continue.bind((*Game).medic_continue)
	medic_hook_launch.bind((*Game).medic_hook_launch)
	medic_cable_attack.bind((*Game).medic_cable_attack)
	medic_hook_retract.bind((*Game).medic_hook_retract)
	medic_attack.bind((*Game).medic_attack)
	medic_checkattack.bind((*Game).medic_checkattack)
}

func init() {
	RegisterMonsterStatics("m_medic", func() any { return new(medicStatics) })
	RegisterSpawn("monster_medic", (*Game).SP_monster_medic)
}

// C: game/m_medic.c:46 medic_FindDeadMonster
func (g *Game) medic_FindDeadMonster(self *Edict) *Edict {
	var ent, best *Edict

	for {
		ent = g.findradius(ent, self.S.Origin, 1024)
		if ent == nil {
			break
		}
		if ent == self {
			continue
		}
		if ent.SVFlags&SVF_MONSTER == 0 {
			continue
		}
		if ent.Monsterinfo.Aiflags&AI_GOOD_GUY != 0 {
			continue
		}
		if ent.Owner != nil {
			continue
		}
		if ent.Health > 0 {
			continue
		}
		if ent.Nextthink != 0 {
			continue
		}
		if !g.visible(self, ent) {
			continue
		}
		if best == nil {
			best = ent
			continue
		}
		if ent.MaxHealth <= best.MaxHealth {
			continue
		}
		best = ent
	}

	return best
}

// C: game/m_medic.c:80 medic_idle
func (g *Game) medic_idle(self *Edict) {
	s := monsterStatics[medicStatics](g, "m_medic")
	g.gi.Sound(self, CHAN_VOICE, s.SoundIdle1, 1, ATTN_IDLE, 0)

	ent := g.medic_FindDeadMonster(self)
	if ent != nil {
		self.Enemy = ent
		self.Enemy.Owner = self
		self.Monsterinfo.Aiflags |= AI_MEDIC
		g.FoundTarget(self)
	}
}

// C: game/m_medic.c:96 medic_search
func (g *Game) medic_search(self *Edict) {
	s := monsterStatics[medicStatics](g, "m_medic")
	g.gi.Sound(self, CHAN_VOICE, s.SoundSearch, 1, ATTN_IDLE, 0)

	if self.Oldenemy == nil {
		ent := g.medic_FindDeadMonster(self)
		if ent != nil {
			self.Oldenemy = self.Enemy
			self.Enemy = ent
			self.Enemy.Owner = self
			self.Monsterinfo.Aiflags |= AI_MEDIC
			g.FoundTarget(self)
		}
	}
}

// C: game/m_medic.c:116 medic_sight
func (g *Game) medic_sight(self *Edict, other *Edict) {
	s := monsterStatics[medicStatics](g, "m_medic")
	g.gi.Sound(self, CHAN_VOICE, s.SoundSight, 1, ATTN_NORM, 0)
}

// C: game/m_medic.c:218 medic_stand
func (g *Game) medic_stand(self *Edict) {
	self.Monsterinfo.Currentmove = medic_move_stand
}

// C: game/m_medic.c:241 medic_walk
func (g *Game) medic_walk(self *Edict) {
	self.Monsterinfo.Currentmove = medic_move_walk
}

// C: game/m_medic.c:259 medic_run
func (g *Game) medic_run(self *Edict) {
	if self.Monsterinfo.Aiflags&AI_MEDIC == 0 {
		ent := g.medic_FindDeadMonster(self)
		if ent != nil {
			self.Oldenemy = self.Enemy
			self.Enemy = ent
			self.Enemy.Owner = self
			self.Monsterinfo.Aiflags |= AI_MEDIC
			g.FoundTarget(self)
			return
		}
	}

	if self.Monsterinfo.Aiflags&AI_STAND_GROUND != 0 {
		self.Monsterinfo.Currentmove = medic_move_stand
	} else {
		self.Monsterinfo.Currentmove = medic_move_run
	}
}

// C: game/m_medic.c:317 medic_pain
func (g *Game) medic_pain(self *Edict, other *Edict, kick float32, damage int32) {
	s := monsterStatics[medicStatics](g, "m_medic")
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

	if g.random() < 0.5 {
		self.Monsterinfo.Currentmove = medic_move_pain1
		g.gi.Sound(self, CHAN_VOICE, s.SoundPain1, 1, ATTN_NORM, 0)
	} else {
		self.Monsterinfo.Currentmove = medic_move_pain2
		g.gi.Sound(self, CHAN_VOICE, s.SoundPain2, 1, ATTN_NORM, 0)
	}
}

// C: game/m_medic.c:342 medic_fire_blaster
func (g *Game) medic_fire_blaster(self *Edict) {
	var forward, right Vec3
	var effect int32

	if (self.S.Frame == medic_FRAME_attack9) || (self.S.Frame == medic_FRAME_attack12) {
		effect = EF_BLASTER
	} else if (self.S.Frame == medic_FRAME_attack19) || (self.S.Frame == medic_FRAME_attack22) || (self.S.Frame == medic_FRAME_attack25) || (self.S.Frame == medic_FRAME_attack28) {
		effect = EF_HYPERBLASTER
	} else {
		effect = 0
	}

	shared.AngleVectors(self.S.Angles, &forward, &right, nil)
	start := G_ProjectSource(self.S.Origin, MonsterFlashOffset[MZ2_MEDIC_BLASTER_1], forward, right)

	end := self.Enemy.S.Origin
	end[2] += float32(self.Enemy.Viewheight)
	dir := shared.VectorSubtract(end, start)

	g.monster_fire_blaster(self, start, dir, 2, 1000, MZ2_MEDIC_BLASTER_1, effect)
}

// C: game/m_medic.c:368 medic_dead
func (g *Game) medic_dead(self *Edict) {
	self.Mins = Vec3{-16, -16, -24}
	self.Maxs = Vec3{16, 16, -8}
	self.Movetype = MOVETYPE_TOSS
	self.SVFlags |= SVF_DEADMONSTER
	self.Nextthink = 0
	g.gi.LinkEntity(self)
}

// C: game/m_medic.c:413 medic_die
func (g *Game) medic_die(self *Edict, inflictor *Edict, attacker *Edict, damage int32, point Vec3) {
	s := monsterStatics[medicStatics](g, "m_medic")
	var n int32

	// if we had a pending patient, free him up for another medic
	if (self.Enemy != nil) && (self.Enemy.Owner == self) {
		self.Enemy.Owner = nil
	}

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

	self.Monsterinfo.Currentmove = medic_move_death
}

// C: game/m_medic.c:446 medic_duck_down
func (g *Game) medic_duck_down(self *Edict) {
	if self.Monsterinfo.Aiflags&AI_DUCKED != 0 {
		return
	}
	self.Monsterinfo.Aiflags |= AI_DUCKED
	self.Maxs[2] -= 32
	self.Takedamage = DAMAGE_YES
	self.Monsterinfo.Pausetime = g.level.Time + 1
	g.gi.LinkEntity(self)
}

// C: game/m_medic.c:457 medic_duck_hold
func (g *Game) medic_duck_hold(self *Edict) {
	if g.level.Time >= self.Monsterinfo.Pausetime {
		self.Monsterinfo.Aiflags &^= AI_HOLD_FRAME
	} else {
		self.Monsterinfo.Aiflags |= AI_HOLD_FRAME
	}
}

// C: game/m_medic.c:465 medic_duck_up
func (g *Game) medic_duck_up(self *Edict) {
	self.Monsterinfo.Aiflags &^= AI_DUCKED
	self.Maxs[2] += 32
	self.Takedamage = DAMAGE_AIM
	g.gi.LinkEntity(self)
}

// C: game/m_medic.c:494 medic_dodge
func (g *Game) medic_dodge(self *Edict, attacker *Edict, eta float32) {
	if g.random() > 0.25 {
		return
	}

	if self.Enemy == nil {
		self.Enemy = attacker
	}

	self.Monsterinfo.Currentmove = medic_move_duck
}

// C: game/m_medic.c:527 medic_continue
func (g *Game) medic_continue(self *Edict) {
	if g.visible(self, self.Enemy) {
		if float64(g.random()) <= 0.95 {
			self.Monsterinfo.Currentmove = medic_move_attackHyperBlaster
		}
	}
}

// C: game/m_medic.c:555 medic_hook_launch
func (g *Game) medic_hook_launch(self *Edict) {
	s := monsterStatics[medicStatics](g, "m_medic")
	g.gi.Sound(self, CHAN_WEAPON, s.SoundHookLaunch, 1, ATTN_NORM, 0)
}

// C: game/m_medic.c:562 medic_cable_offsets
var medic_cable_offsets = [...]Vec3{
	{45.0, -9.2, 15.5},
	{48.4, -9.7, 15.2},
	{47.8, -9.8, 15.8},
	{47.3, -9.3, 14.3},
	{45.4, -10.1, 13.1},
	{41.9, -12.7, 12.0},
	{37.8, -15.8, 11.2},
	{34.3, -18.4, 10.7},
	{32.7, -19.7, 10.4},
	{32.7, -19.7, 10.4},
}

// C: game/m_medic.c:576 medic_cable_attack
func (g *Game) medic_cable_attack(self *Edict) {
	var f, r Vec3

	if !self.Enemy.InUse {
		return
	}

	shared.AngleVectors(self.S.Angles, &f, &r, nil)
	offset := medic_cable_offsets[self.S.Frame-medic_FRAME_attack42]
	start := G_ProjectSource(self.S.Origin, offset, f, r)

	// check for max distance
	dir := shared.VectorSubtract(start, self.Enemy.S.Origin)
	distance := shared.VectorLength(dir)
	if distance > 256 {
		return
	}

	// check for min/max pitch
	angles := vectoangles(dir)
	if angles[0] < -180 {
		angles[0] += 360
	}
	if math.Abs(float64(angles[0])) > 45 {
		return
	}

	tr := g.gi.Trace(&start, nil, nil, &self.Enemy.S.Origin, self, MASK_SHOT)
	if tr.Fraction != 1.0 && tr.Ent != self.Enemy {
		return
	}

	if self.S.Frame == medic_FRAME_attack43 {
		s := monsterStatics[medicStatics](g, "m_medic")
		g.gi.Sound(self.Enemy, CHAN_AUTO, s.SoundHookHit, 1, ATTN_NORM, 0)
		self.Enemy.Monsterinfo.Aiflags |= AI_RESURRECTING
	} else if self.S.Frame == medic_FRAME_attack50 {
		self.Enemy.Spawnflags = 0
		self.Enemy.Monsterinfo.Aiflags = 0
		self.Enemy.Target = ""
		self.Enemy.Targetname = ""
		self.Enemy.Combattarget = ""
		self.Enemy.Deathtarget = ""
		self.Enemy.Owner = self
		g.ED_CallSpawn(self.Enemy)
		self.Enemy.Owner = nil
		if self.Enemy.Think != nil {
			self.Enemy.Nextthink = g.level.Time
			self.Enemy.Think.fn(g, self.Enemy)
		}
		self.Enemy.Monsterinfo.Aiflags |= AI_RESURRECTING
		if self.Oldenemy != nil && self.Oldenemy.Client != nil {
			self.Enemy.Enemy = self.Oldenemy
			g.FoundTarget(self.Enemy)
		}
	} else {
		if self.S.Frame == medic_FRAME_attack44 {
			s := monsterStatics[medicStatics](g, "m_medic")
			g.gi.Sound(self, CHAN_WEAPON, s.SoundHookHeal, 1, ATTN_NORM, 0)
		}
	}

	// adjust start for beam origin being in middle of a segment
	start = shared.VectorMA(start, 8, f)

	// adjust end z for end spot since the monster is currently dead
	end := self.Enemy.S.Origin
	end[2] = self.Enemy.AbsMin[2] + self.Enemy.Size[2]/2

	g.gi.WriteByteC(svc_temp_entity)
	g.gi.WriteByteC(TE_MEDIC_CABLE_ATTACK)
	g.gi.WriteShort(self.Index)
	g.gi.WritePosition(&start)
	g.gi.WritePosition(&end)
	g.gi.Multicast(&self.S.Origin, MULTICAST_PVS)
}

// C: game/m_medic.c:656 medic_hook_retract
func (g *Game) medic_hook_retract(self *Edict) {
	s := monsterStatics[medicStatics](g, "m_medic")
	g.gi.Sound(self, CHAN_WEAPON, s.SoundHookRetract, 1, ATTN_NORM, 0)
	self.Enemy.Monsterinfo.Aiflags &^= AI_RESURRECTING
}

// C: game/m_medic.c:696 medic_attack
func (g *Game) medic_attack(self *Edict) {
	if self.Monsterinfo.Aiflags&AI_MEDIC != 0 {
		self.Monsterinfo.Currentmove = medic_move_attackCable
	} else {
		self.Monsterinfo.Currentmove = medic_move_attackBlaster
	}
}

// C: game/m_medic.c:704 medic_checkattack
func (g *Game) medic_checkattack(self *Edict) bool {
	if self.Monsterinfo.Aiflags&AI_MEDIC != 0 {
		g.medic_attack(self)
		return true
	}

	return g.M_CheckAttack(self)
}

// QUAKED monster_medic (1 .5 0) (-16 -16 -24) (16 16 32) Ambush Trigger_Spawn Sight
//
// C: game/m_medic.c:718 SP_monster_medic
func (g *Game) SP_monster_medic(self *Edict) {
	s := monsterStatics[medicStatics](g, "m_medic")
	if g.deathmatch.Value != 0 {
		g.G_FreeEdict(self)
		return
	}

	s.SoundIdle1 = g.gi.SoundIndex("medic/idle.wav")
	s.SoundPain1 = g.gi.SoundIndex("medic/medpain1.wav")
	s.SoundPain2 = g.gi.SoundIndex("medic/medpain2.wav")
	s.SoundDie = g.gi.SoundIndex("medic/meddeth1.wav")
	s.SoundSight = g.gi.SoundIndex("medic/medsght1.wav")
	s.SoundSearch = g.gi.SoundIndex("medic/medsrch1.wav")
	s.SoundHookLaunch = g.gi.SoundIndex("medic/medatck2.wav")
	s.SoundHookHit = g.gi.SoundIndex("medic/medatck3.wav")
	s.SoundHookHeal = g.gi.SoundIndex("medic/medatck4.wav")
	s.SoundHookRetract = g.gi.SoundIndex("medic/medatck5.wav")

	g.gi.SoundIndex("medic/medatck1.wav")

	self.Movetype = MOVETYPE_STEP
	self.Solid = SOLID_BBOX
	self.S.ModelIndex = int32(g.gi.ModelIndex("models/monsters/medic/tris.md2"))
	self.Mins = Vec3{-24, -24, -24}
	self.Maxs = Vec3{24, 24, 32}

	self.Health = 300
	self.GibHealth = -130
	self.Mass = 400

	self.Pain = medic_pain
	self.Die = medic_die

	self.Monsterinfo.Stand = medic_stand
	self.Monsterinfo.Walk = medic_walk
	self.Monsterinfo.Run = medic_run
	self.Monsterinfo.Dodge = medic_dodge
	self.Monsterinfo.Attack = medic_attack
	self.Monsterinfo.Melee = nil
	self.Monsterinfo.Sight = medic_sight
	self.Monsterinfo.Idle = medic_idle
	self.Monsterinfo.Search = medic_search
	self.Monsterinfo.Checkattack = medic_checkattack

	g.gi.LinkEntity(self)

	self.Monsterinfo.Currentmove = medic_move_stand
	self.Monsterinfo.Scale = medic_MODEL_SCALE

	g.walkmonster_start(self)
}

// C: game/m_medic.c:122 medic_frames_stand
var medic_frames_stand = []MFrame{
	{ai_stand, 0, medic_idle},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
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

// C: game/m_medic.c:224 medic_frames_walk
var medic_frames_walk = []MFrame{
	{ai_walk, 6.2, nil},
	{ai_walk, 18.1, nil},
	{ai_walk, 1, nil},
	{ai_walk, 9, nil},
	{ai_walk, 10, nil},
	{ai_walk, 9, nil},
	{ai_walk, 11, nil},
	{ai_walk, 11.6, nil},
	{ai_walk, 2, nil},
	{ai_walk, 9.9, nil},
	{ai_walk, 14, nil},
	{ai_walk, 9.3, nil},
}

// C: game/m_medic.c:247 medic_frames_run
var medic_frames_run = []MFrame{
	{ai_run, 18, nil},
	{ai_run, 22.5, nil},
	{ai_run, 25.4, nil},
	{ai_run, 23.4, nil},
	{ai_run, 24, nil},
	{ai_run, 35.6, nil},
}

// C: game/m_medic.c:284 medic_frames_pain1
var medic_frames_pain1 = []MFrame{
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
}

// C: game/m_medic.c:297 medic_frames_pain2
var medic_frames_pain2 = []MFrame{
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
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

// C: game/m_medic.c:378 medic_frames_death
var medic_frames_death = []MFrame{
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
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

// C: game/m_medic.c:473 medic_frames_duck
var medic_frames_duck = []MFrame{
	{ai_move, -1, nil},
	{ai_move, -1, nil},
	{ai_move, -1, medic_duck_down},
	{ai_move, -1, medic_duck_hold},
	{ai_move, -1, nil},
	{ai_move, -1, nil},
	{ai_move, -1, medic_duck_up},
	{ai_move, -1, nil},
	{ai_move, -1, nil},
	{ai_move, -1, nil},
	{ai_move, -1, nil},
	{ai_move, -1, nil},
	{ai_move, -1, nil},
	{ai_move, -1, nil},
	{ai_move, -1, nil},
	{ai_move, -1, nil},
}

// C: game/m_medic.c:505 medic_frames_attackHyperBlaster
var medic_frames_attackHyperBlaster = []MFrame{
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, medic_fire_blaster},
	{ai_charge, 0, medic_fire_blaster},
	{ai_charge, 0, medic_fire_blaster},
	{ai_charge, 0, medic_fire_blaster},
	{ai_charge, 0, medic_fire_blaster},
	{ai_charge, 0, medic_fire_blaster},
	{ai_charge, 0, medic_fire_blaster},
	{ai_charge, 0, medic_fire_blaster},
	{ai_charge, 0, medic_fire_blaster},
	{ai_charge, 0, medic_fire_blaster},
	{ai_charge, 0, medic_fire_blaster},
	{ai_charge, 0, medic_fire_blaster},
}

// C: game/m_medic.c:535 medic_frames_attackBlaster
var medic_frames_attackBlaster = []MFrame{
	{ai_charge, 0, nil},
	{ai_charge, 5, nil},
	{ai_charge, 5, nil},
	{ai_charge, 3, nil},
	{ai_charge, 2, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, medic_fire_blaster},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, medic_fire_blaster},
	{ai_charge, 0, nil},
	{ai_charge, 0, medic_continue},
}

// C: game/m_medic.c:662 medic_frames_attackCable
var medic_frames_attackCable = []MFrame{
	{ai_move, 2, nil},
	{ai_move, 3, nil},
	{ai_move, 5, nil},
	{ai_move, 4.4, nil},
	{ai_charge, 4.7, nil},
	{ai_charge, 5, nil},
	{ai_charge, 6, nil},
	{ai_charge, 4, nil},
	{ai_charge, 0, nil},
	{ai_move, 0, medic_hook_launch},
	{ai_move, 0, medic_cable_attack},
	{ai_move, 0, medic_cable_attack},
	{ai_move, 0, medic_cable_attack},
	{ai_move, 0, medic_cable_attack},
	{ai_move, 0, medic_cable_attack},
	{ai_move, 0, medic_cable_attack},
	{ai_move, 0, medic_cable_attack},
	{ai_move, 0, medic_cable_attack},
	{ai_move, 0, medic_cable_attack},
	{ai_move, -15, medic_hook_retract},
	{ai_move, -1.5, nil},
	{ai_move, -1.2, nil},
	{ai_move, -3, nil},
	{ai_move, -2, nil},
	{ai_move, 0.3, nil},
	{ai_move, 0.7, nil},
	{ai_move, 1.2, nil},
	{ai_move, 1.3, nil},
}

// C: game/m_medic.c:216 medic_move_stand
var medic_move_stand = defMMove("medic_move_stand", medic_FRAME_wait1, medic_FRAME_wait90, medic_frames_stand, nil)

// C: game/m_medic.c:239 medic_move_walk
var medic_move_walk = defMMove("medic_move_walk", medic_FRAME_walk1, medic_FRAME_walk12, medic_frames_walk, nil)

// C: game/m_medic.c:257 medic_move_run
var medic_move_run = defMMove("medic_move_run", medic_FRAME_run1, medic_FRAME_run6, medic_frames_run, nil)

// C: game/m_medic.c:295 medic_move_pain1
var medic_move_pain1 = defMMove("medic_move_pain1", medic_FRAME_paina1, medic_FRAME_paina8, medic_frames_pain1, medic_run)

// C: game/m_medic.c:315 medic_move_pain2
var medic_move_pain2 = defMMove("medic_move_pain2", medic_FRAME_painb1, medic_FRAME_painb15, medic_frames_pain2, medic_run)

// C: game/m_medic.c:411 medic_move_death
var medic_move_death = defMMove("medic_move_death", medic_FRAME_death1, medic_FRAME_death30, medic_frames_death, medic_dead)

// C: game/m_medic.c:492 medic_move_duck
var medic_move_duck = defMMove("medic_move_duck", medic_FRAME_duck1, medic_FRAME_duck16, medic_frames_duck, medic_run)

// C: game/m_medic.c:524 medic_move_attackHyperBlaster
var medic_move_attackHyperBlaster = defMMove("medic_move_attackHyperBlaster", medic_FRAME_attack15, medic_FRAME_attack30, medic_frames_attackHyperBlaster, medic_run)

// C: game/m_medic.c:552 medic_move_attackBlaster
var medic_move_attackBlaster = defMMove("medic_move_attackBlaster", medic_FRAME_attack1, medic_FRAME_attack14, medic_frames_attackBlaster, medic_run)

// C: game/m_medic.c:693 medic_move_attackCable
var medic_move_attackCable = defMMove("medic_move_attackCable", medic_FRAME_attack33, medic_FRAME_attack60, medic_frames_attackCable, medic_run)
