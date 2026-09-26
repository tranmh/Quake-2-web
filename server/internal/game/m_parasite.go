package game

// Port of game/m_parasite.c.

import (
	"math"

	. "quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/shared"
)

// statics of m_parasite.c
type parasiteStatics struct {
	SoundPain1   int // sound_pain1
	SoundPain2   int // sound_pain2
	SoundDie     int // sound_die
	SoundLaunch  int // sound_launch
	SoundImpact  int // sound_impact
	SoundSuck    int // sound_suck
	SoundReelin  int // sound_reelin
	SoundSight   int // sound_sight
	SoundTap     int // sound_tap
	SoundScratch int // sound_scratch
	SoundSearch  int // sound_search
}

var (
	parasite_launch       = defThink("parasite_launch")
	parasite_reel_in      = defThink("parasite_reel_in")
	parasite_sight        = defBlocked("parasite_sight")
	parasite_tap          = defThink("parasite_tap")
	parasite_scratch      = defThink("parasite_scratch")
	parasite_do_fidget    = defThink("parasite_do_fidget")
	parasite_refidget     = defThink("parasite_refidget")
	parasite_idle         = defThink("parasite_idle")
	parasite_stand        = defThink("parasite_stand")
	parasite_start_run    = defThink("parasite_start_run")
	parasite_run          = defThink("parasite_run")
	parasite_start_walk   = defThink("parasite_start_walk")
	parasite_walk         = defThink("parasite_walk")
	parasite_pain         = defPain("parasite_pain")
	parasite_drain_attack = defThink("parasite_drain_attack")
	parasite_attack       = defThink("parasite_attack")
	parasite_dead         = defThink("parasite_dead")
	parasite_die          = defDie("parasite_die")
)

func init() {
	parasite_launch.bind((*Game).parasite_launch)
	parasite_reel_in.bind((*Game).parasite_reel_in)
	parasite_sight.bind((*Game).parasite_sight)
	parasite_tap.bind((*Game).parasite_tap)
	parasite_scratch.bind((*Game).parasite_scratch)
	parasite_do_fidget.bind((*Game).parasite_do_fidget)
	parasite_refidget.bind((*Game).parasite_refidget)
	parasite_idle.bind((*Game).parasite_idle)
	parasite_stand.bind((*Game).parasite_stand)
	parasite_start_run.bind((*Game).parasite_start_run)
	parasite_run.bind((*Game).parasite_run)
	parasite_start_walk.bind((*Game).parasite_start_walk)
	parasite_walk.bind((*Game).parasite_walk)
	parasite_pain.bind((*Game).parasite_pain)
	parasite_drain_attack.bind((*Game).parasite_drain_attack)
	parasite_attack.bind((*Game).parasite_attack)
	parasite_dead.bind((*Game).parasite_dead)
	parasite_die.bind((*Game).parasite_die)
}

func init() {
	RegisterMonsterStatics("m_parasite", func() any { return new(parasiteStatics) })
	RegisterSpawn("monster_parasite", (*Game).SP_monster_parasite)
}

// ==============================================================================
//
// parasite
//
// ==============================================================================
//
// C: game/m_parasite.c:55 parasite_launch
func (g *Game) parasite_launch(self *Edict) {
	s := monsterStatics[parasiteStatics](g, "m_parasite")
	g.gi.Sound(self, CHAN_WEAPON, s.SoundLaunch, 1, ATTN_NORM, 0)
}

// C: game/m_parasite.c:60 parasite_reel_in
func (g *Game) parasite_reel_in(self *Edict) {
	s := monsterStatics[parasiteStatics](g, "m_parasite")
	g.gi.Sound(self, CHAN_WEAPON, s.SoundReelin, 1, ATTN_NORM, 0)
}

// C: game/m_parasite.c:65 parasite_sight
func (g *Game) parasite_sight(self *Edict, other *Edict) {
	s := monsterStatics[parasiteStatics](g, "m_parasite")
	g.gi.Sound(self, CHAN_WEAPON, s.SoundSight, 1, ATTN_NORM, 0)
}

// C: game/m_parasite.c:70 parasite_tap
func (g *Game) parasite_tap(self *Edict) {
	s := monsterStatics[parasiteStatics](g, "m_parasite")
	g.gi.Sound(self, CHAN_WEAPON, s.SoundTap, 1, ATTN_IDLE, 0)
}

// C: game/m_parasite.c:75 parasite_scratch
func (g *Game) parasite_scratch(self *Edict) {
	s := monsterStatics[parasiteStatics](g, "m_parasite")
	g.gi.Sound(self, CHAN_WEAPON, s.SoundScratch, 1, ATTN_IDLE, 0)
}

// C: game/m_parasite.c:80 parasite_search
func (g *Game) parasite_search(self *Edict) {
	s := monsterStatics[parasiteStatics](g, "m_parasite")
	g.gi.Sound(self, CHAN_WEAPON, s.SoundSearch, 1, ATTN_IDLE, 0)
}

// C: game/m_parasite.c:86 parasite_frames_start_fidget
var parasite_frames_start_fidget = []MFrame{
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
}

// C: game/m_parasite.c:93 parasite_move_start_fidget
var parasite_move_start_fidget = defMMove("parasite_move_start_fidget", parasite_FRAME_stand18, parasite_FRAME_stand21, parasite_frames_start_fidget, parasite_do_fidget)

// C: game/m_parasite.c:95 parasite_frames_fidget
var parasite_frames_fidget = []MFrame{
	{ai_stand, 0, parasite_scratch},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, parasite_scratch},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
}

// C: game/m_parasite.c:104 parasite_move_fidget
var parasite_move_fidget = defMMove("parasite_move_fidget", parasite_FRAME_stand22, parasite_FRAME_stand27, parasite_frames_fidget, parasite_refidget)

// C: game/m_parasite.c:106 parasite_frames_end_fidget
var parasite_frames_end_fidget = []MFrame{
	{ai_stand, 0, parasite_scratch},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
}

// C: game/m_parasite.c:117 parasite_move_end_fidget
var parasite_move_end_fidget = defMMove("parasite_move_end_fidget", parasite_FRAME_stand28, parasite_FRAME_stand35, parasite_frames_end_fidget, parasite_stand)

// C: game/m_parasite.c:119 parasite_end_fidget
func (g *Game) parasite_end_fidget(self *Edict) {
	self.Monsterinfo.Currentmove = parasite_move_end_fidget
}

// C: game/m_parasite.c:124 parasite_do_fidget
func (g *Game) parasite_do_fidget(self *Edict) {
	self.Monsterinfo.Currentmove = parasite_move_fidget
}

// C: game/m_parasite.c:129 parasite_refidget
func (g *Game) parasite_refidget(self *Edict) {
	if float64(g.random()) <= 0.8 {
		self.Monsterinfo.Currentmove = parasite_move_fidget
	} else {
		self.Monsterinfo.Currentmove = parasite_move_end_fidget
	}
}

// C: game/m_parasite.c:137 parasite_idle
func (g *Game) parasite_idle(self *Edict) {
	self.Monsterinfo.Currentmove = parasite_move_start_fidget
}

// C: game/m_parasite.c:143 parasite_frames_stand
var parasite_frames_stand = []MFrame{
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, parasite_tap},
	{ai_stand, 0, nil},
	{ai_stand, 0, parasite_tap},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, parasite_tap},
	{ai_stand, 0, nil},
	{ai_stand, 0, parasite_tap},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, nil},
	{ai_stand, 0, parasite_tap},
	{ai_stand, 0, nil},
	{ai_stand, 0, parasite_tap},
}

// C: game/m_parasite.c:163 parasite_move_stand
var parasite_move_stand = defMMove("parasite_move_stand", parasite_FRAME_stand01, parasite_FRAME_stand17, parasite_frames_stand, parasite_stand)

// C: game/m_parasite.c:165 parasite_stand
func (g *Game) parasite_stand(self *Edict) {
	self.Monsterinfo.Currentmove = parasite_move_stand
}

// C: game/m_parasite.c:171 parasite_frames_run
var parasite_frames_run = []MFrame{
	{ai_run, 30, nil},
	{ai_run, 30, nil},
	{ai_run, 22, nil},
	{ai_run, 19, nil},
	{ai_run, 24, nil},
	{ai_run, 28, nil},
	{ai_run, 25, nil},
}

// C: game/m_parasite.c:181 parasite_move_run
var parasite_move_run = defMMove("parasite_move_run", parasite_FRAME_run03, parasite_FRAME_run09, parasite_frames_run, nil)

// C: game/m_parasite.c:183 parasite_frames_start_run
var parasite_frames_start_run = []MFrame{
	{ai_run, 0, nil},
	{ai_run, 30, nil},
}

// C: game/m_parasite.c:188 parasite_move_start_run
var parasite_move_start_run = defMMove("parasite_move_start_run", parasite_FRAME_run01, parasite_FRAME_run02, parasite_frames_start_run, parasite_run)

// C: game/m_parasite.c:190 parasite_frames_stop_run
var parasite_frames_stop_run = []MFrame{
	{ai_run, 20, nil},
	{ai_run, 20, nil},
	{ai_run, 12, nil},
	{ai_run, 10, nil},
	{ai_run, 0, nil},
	{ai_run, 0, nil},
}

// C: game/m_parasite.c:199 parasite_move_stop_run
var parasite_move_stop_run = defMMove("parasite_move_stop_run", parasite_FRAME_run10, parasite_FRAME_run15, parasite_frames_stop_run, nil)

// C: game/m_parasite.c:201 parasite_start_run
func (g *Game) parasite_start_run(self *Edict) {
	if self.Monsterinfo.Aiflags&AI_STAND_GROUND != 0 {
		self.Monsterinfo.Currentmove = parasite_move_stand
	} else {
		self.Monsterinfo.Currentmove = parasite_move_start_run
	}
}

// C: game/m_parasite.c:209 parasite_run
func (g *Game) parasite_run(self *Edict) {
	if self.Monsterinfo.Aiflags&AI_STAND_GROUND != 0 {
		self.Monsterinfo.Currentmove = parasite_move_stand
	} else {
		self.Monsterinfo.Currentmove = parasite_move_run
	}
}

// C: game/m_parasite.c:218 parasite_frames_walk
var parasite_frames_walk = []MFrame{
	{ai_walk, 30, nil},
	{ai_walk, 30, nil},
	{ai_walk, 22, nil},
	{ai_walk, 19, nil},
	{ai_walk, 24, nil},
	{ai_walk, 28, nil},
	{ai_walk, 25, nil},
}

// C: game/m_parasite.c:228 parasite_move_walk
var parasite_move_walk = defMMove("parasite_move_walk", parasite_FRAME_run03, parasite_FRAME_run09, parasite_frames_walk, parasite_walk)

// C: game/m_parasite.c:230 parasite_frames_start_walk
var parasite_frames_start_walk = []MFrame{
	{ai_walk, 0, nil},
	{ai_walk, 30, parasite_walk},
}

// C: game/m_parasite.c:235 parasite_move_start_walk
var parasite_move_start_walk = defMMove("parasite_move_start_walk", parasite_FRAME_run01, parasite_FRAME_run02, parasite_frames_start_walk, nil)

// C: game/m_parasite.c:237 parasite_frames_stop_walk
var parasite_frames_stop_walk = []MFrame{
	{ai_walk, 20, nil},
	{ai_walk, 20, nil},
	{ai_walk, 12, nil},
	{ai_walk, 10, nil},
	{ai_walk, 0, nil},
	{ai_walk, 0, nil},
}

// C: game/m_parasite.c:246 parasite_move_stop_walk
var parasite_move_stop_walk = defMMove("parasite_move_stop_walk", parasite_FRAME_run10, parasite_FRAME_run15, parasite_frames_stop_walk, nil)

// C: game/m_parasite.c:248 parasite_start_walk
func (g *Game) parasite_start_walk(self *Edict) {
	self.Monsterinfo.Currentmove = parasite_move_start_walk
}

// C: game/m_parasite.c:253 parasite_walk
func (g *Game) parasite_walk(self *Edict) {
	self.Monsterinfo.Currentmove = parasite_move_walk
}

// C: game/m_parasite.c:259 parasite_frames_pain1
var parasite_frames_pain1 = []MFrame{
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 6, nil},
	{ai_move, 16, nil},
	{ai_move, -6, nil},
	{ai_move, -7, nil},
	{ai_move, 0, nil},
}

// C: game/m_parasite.c:273 parasite_move_pain1
var parasite_move_pain1 = defMMove("parasite_move_pain1", parasite_FRAME_pain101, parasite_FRAME_pain111, parasite_frames_pain1, parasite_start_run)

// C: game/m_parasite.c:275 parasite_pain
func (g *Game) parasite_pain(self *Edict, other *Edict, kick float32, damage int32) {
	s := monsterStatics[parasiteStatics](g, "m_parasite")
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
		g.gi.Sound(self, CHAN_VOICE, s.SoundPain1, 1, ATTN_NORM, 0)
	} else {
		g.gi.Sound(self, CHAN_VOICE, s.SoundPain2, 1, ATTN_NORM, 0)
	}

	self.Monsterinfo.Currentmove = parasite_move_pain1
}

// C: game/m_parasite.c:297 parasite_drain_attack_ok
func (g *Game) parasite_drain_attack_ok(start Vec3, end Vec3) bool {
	var dir, angles Vec3

	// check for max distance
	dir = shared.VectorSubtract(start, end)
	if shared.VectorLength(dir) > 256 {
		return false
	}

	// check for min/max pitch
	angles = vectoangles(dir)
	if angles[0] < -180 {
		angles[0] += 360
	}
	if math.Abs(float64(angles[0])) > 30 {
		return false
	}

	return true
}

// C: game/m_parasite.c:316 parasite_drain_attack
func (g *Game) parasite_drain_attack(self *Edict) {
	s := monsterStatics[parasiteStatics](g, "m_parasite")
	var offset, start, f, r, end, dir Vec3
	var tr Trace
	var damage int32

	shared.AngleVectors(self.S.Angles, &f, &r, nil)
	offset = Vec3{24, 0, 6}
	start = G_ProjectSource(self.S.Origin, offset, f, r)

	end = self.Enemy.S.Origin
	if !g.parasite_drain_attack_ok(start, end) {
		end[2] = self.Enemy.S.Origin[2] + self.Enemy.Maxs[2] - 8
		if !g.parasite_drain_attack_ok(start, end) {
			end[2] = self.Enemy.S.Origin[2] + self.Enemy.Mins[2] + 8
			if !g.parasite_drain_attack_ok(start, end) {
				return
			}
		}
	}
	end = self.Enemy.S.Origin

	tr = g.gi.Trace(&start, nil, nil, &end, self, MASK_SHOT)
	if tr.Ent != self.Enemy {
		return
	}

	if self.S.Frame == parasite_FRAME_drain03 {
		damage = 5
		g.gi.Sound(self.Enemy, CHAN_AUTO, s.SoundImpact, 1, ATTN_NORM, 0)
	} else {
		if self.S.Frame == parasite_FRAME_drain04 {
			g.gi.Sound(self, CHAN_WEAPON, s.SoundSuck, 1, ATTN_NORM, 0)
		}
		damage = 2
	}

	g.gi.WriteByteC(svc_temp_entity)
	g.gi.WriteByteC(TE_PARASITE_ATTACK)
	g.gi.WriteShort(self.Index)
	g.gi.WritePosition(&start)
	g.gi.WritePosition(&end)
	g.gi.Multicast(&self.S.Origin, MULTICAST_PVS)

	dir = shared.VectorSubtract(start, end)
	g.T_Damage(self.Enemy, self, self, &dir, self.Enemy.S.Origin, shared.Vec3Origin, damage, 0, DAMAGE_NO_KNOCKBACK, MOD_UNKNOWN)
}

// C: game/m_parasite.c:366 parasite_frames_drain
var parasite_frames_drain = []MFrame{
	{ai_charge, 0, parasite_launch},
	{ai_charge, 0, nil},
	{ai_charge, 15, parasite_drain_attack},
	{ai_charge, 0, parasite_drain_attack},
	{ai_charge, 0, parasite_drain_attack},
	{ai_charge, 0, parasite_drain_attack},
	{ai_charge, 0, parasite_drain_attack},
	{ai_charge, -2, parasite_drain_attack},
	{ai_charge, -2, parasite_drain_attack},
	{ai_charge, -3, parasite_drain_attack},
	{ai_charge, -2, parasite_drain_attack},
	{ai_charge, 0, parasite_drain_attack},
	{ai_charge, -1, parasite_drain_attack},
	{ai_charge, 0, parasite_reel_in},
	{ai_charge, -2, nil},
	{ai_charge, -2, nil},
	{ai_charge, -3, nil},
	{ai_charge, 0, nil},
}

// C: game/m_parasite.c:387 parasite_move_drain
var parasite_move_drain = defMMove("parasite_move_drain", parasite_FRAME_drain01, parasite_FRAME_drain18, parasite_frames_drain, parasite_start_run)

// C: game/m_parasite.c:390 parasite_frames_break
var parasite_frames_break = []MFrame{
	{ai_charge, 0, nil},
	{ai_charge, -3, nil},
	{ai_charge, 1, nil},
	{ai_charge, 2, nil},
	{ai_charge, -3, nil},
	{ai_charge, 1, nil},
	{ai_charge, 1, nil},
	{ai_charge, 3, nil},
	{ai_charge, 0, nil},
	{ai_charge, -18, nil},
	{ai_charge, 3, nil},
	{ai_charge, 9, nil},
	{ai_charge, 6, nil},
	{ai_charge, 0, nil},
	{ai_charge, -18, nil},
	{ai_charge, 0, nil},
	{ai_charge, 8, nil},
	{ai_charge, 9, nil},
	{ai_charge, 0, nil},
	{ai_charge, -18, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 0, nil},
	{ai_charge, 4, nil},
	{ai_charge, 11, nil},
	{ai_charge, -2, nil},
	{ai_charge, -5, nil},
	{ai_charge, 1, nil},
}

// C: game/m_parasite.c:425 parasite_move_break
var parasite_move_break = defMMove("parasite_move_break", parasite_FRAME_break01, parasite_FRAME_break32, parasite_frames_break, parasite_start_run)

// ===
// Break Stuff Ends
// ===
//
// C: game/m_parasite.c:433 parasite_attack
func (g *Game) parasite_attack(self *Edict) {
	//	if (random() <= 0.2)
	//		self->monsterinfo.currentmove = &parasite_move_break;
	//	else
	self.Monsterinfo.Currentmove = parasite_move_drain
}

// ===
// Death Stuff Starts
// ===
//
// C: game/m_parasite.c:449 parasite_dead
func (g *Game) parasite_dead(self *Edict) {
	self.Mins = Vec3{-16, -16, -24}
	self.Maxs = Vec3{16, 16, -8}
	self.Movetype = MOVETYPE_TOSS
	self.SVFlags |= SVF_DEADMONSTER
	self.Nextthink = 0
	g.gi.LinkEntity(self)
}

// C: game/m_parasite.c:459 parasite_frames_death
var parasite_frames_death = []MFrame{
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
	{ai_move, 0, nil},
}

// C: game/m_parasite.c:469 parasite_move_death
var parasite_move_death = defMMove("parasite_move_death", parasite_FRAME_death101, parasite_FRAME_death107, parasite_frames_death, parasite_dead)

// C: game/m_parasite.c:471 parasite_die
func (g *Game) parasite_die(self *Edict, inflictor *Edict, attacker *Edict, damage int32, point Vec3) {
	s := monsterStatics[parasiteStatics](g, "m_parasite")
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
	self.Monsterinfo.Currentmove = parasite_move_death
}

// ===
// End Death Stuff
// ===
//
// QUAKED monster_parasite (1 .5 0) (-16 -16 -24) (16 16 32) Ambush Trigger_Spawn Sight
//
// C: game/m_parasite.c:506 SP_monster_parasite
func (g *Game) SP_monster_parasite(self *Edict) {
	s := monsterStatics[parasiteStatics](g, "m_parasite")
	if g.deathmatch.Value != 0 {
		g.G_FreeEdict(self)
		return
	}

	s.SoundPain1 = g.gi.SoundIndex("parasite/parpain1.wav")
	s.SoundPain2 = g.gi.SoundIndex("parasite/parpain2.wav")
	s.SoundDie = g.gi.SoundIndex("parasite/pardeth1.wav")
	s.SoundLaunch = g.gi.SoundIndex("parasite/paratck1.wav")
	s.SoundImpact = g.gi.SoundIndex("parasite/paratck2.wav")
	s.SoundSuck = g.gi.SoundIndex("parasite/paratck3.wav")
	s.SoundReelin = g.gi.SoundIndex("parasite/paratck4.wav")
	s.SoundSight = g.gi.SoundIndex("parasite/parsght1.wav")
	s.SoundTap = g.gi.SoundIndex("parasite/paridle1.wav")
	s.SoundScratch = g.gi.SoundIndex("parasite/paridle2.wav")
	s.SoundSearch = g.gi.SoundIndex("parasite/parsrch1.wav")

	self.S.ModelIndex = int32(g.gi.ModelIndex("models/monsters/parasite/tris.md2"))
	self.Mins = Vec3{-16, -16, -24}
	self.Maxs = Vec3{16, 16, 24}
	self.Movetype = MOVETYPE_STEP
	self.Solid = SOLID_BBOX

	self.Health = 175
	self.GibHealth = -50
	self.Mass = 250

	self.Pain = parasite_pain
	self.Die = parasite_die

	self.Monsterinfo.Stand = parasite_stand
	self.Monsterinfo.Walk = parasite_start_walk
	self.Monsterinfo.Run = parasite_start_run
	self.Monsterinfo.Attack = parasite_attack
	self.Monsterinfo.Sight = parasite_sight
	self.Monsterinfo.Idle = parasite_idle

	g.gi.LinkEntity(self)

	self.Monsterinfo.Currentmove = parasite_move_stand
	self.Monsterinfo.Scale = parasite_MODEL_SCALE

	g.walkmonster_start(self)
}
