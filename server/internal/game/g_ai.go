package game

// Port of game/g_ai.c.

import (
	. "quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/shared"
)

// mframe_t aifuncs and the default checkattack.
var (
	ai_move       = defAI("ai_move")
	ai_stand      = defAI("ai_stand")
	ai_walk       = defAI("ai_walk")
	ai_charge     = defAI("ai_charge")
	ai_turn       = defAI("ai_turn")
	ai_run        = defAI("ai_run")
	M_CheckAttack = defCheckAttack("M_CheckAttack")
)

func init() {
	ai_move.bind((*Game).ai_move)
	ai_stand.bind((*Game).ai_stand)
	ai_walk.bind((*Game).ai_walk)
	ai_charge.bind((*Game).ai_charge)
	ai_turn.bind((*Game).ai_turn)
	ai_run.bind((*Game).ai_run)
	M_CheckAttack.bind((*Game).M_CheckAttack)
}

// AI_SetSightClient is called once each frame to set level.sight_client to
// the player to be checked for in findtarget.
//
// If all clients are either dead or in notarget, sight_client
// will be null.
//
// In coop games, sight_client will cycle between the clients.
// C: game/g_ai.c:50 AI_SetSightClient
func (g *Game) AI_SetSightClient() {
	var start, check int32

	if g.level.SightClient == nil {
		start = 1
	} else {
		start = int32(g.level.SightClient.Index)
	}

	check = start
	for {
		check++
		if check > g.game.Maxclients {
			check = 1
		}
		ent := &g.edicts[check]
		if ent.InUse &&
			ent.Health > 0 &&
			ent.Flags&FL_NOTARGET == 0 {
			g.level.SightClient = ent
			return // got one
		}
		if check == start {
			g.level.SightClient = nil
			return // nobody to see
		}
	}
}

//============================================================================

// ai_move moves the specified distance at current facing.
// This replaces the QC functions: ai_forward, ai_back, ai_pain, and ai_painforward
// C: game/g_ai.c:92 ai_move
func (g *Game) ai_move(self *Edict, dist float32) {
	g.M_walkmove(self, self.S.Angles[YAW], dist)
}

// ai_stand is used for standing around and looking for players.
// Distance is for slight position adjustments needed by the animations
// C: game/g_ai.c:106 ai_stand
func (g *Game) ai_stand(self *Edict, dist float32) {
	if dist != 0 {
		g.M_walkmove(self, self.S.Angles[YAW], dist)
	}

	if self.Monsterinfo.Aiflags&AI_STAND_GROUND != 0 {
		if self.Enemy != nil {
			v := shared.VectorSubtract(self.Enemy.S.Origin, self.S.Origin)
			self.IdealYaw = vectoyaw(v)
			if self.S.Angles[YAW] != self.IdealYaw && self.Monsterinfo.Aiflags&AI_TEMP_STAND_GROUND != 0 {
				self.Monsterinfo.Aiflags &^= (AI_STAND_GROUND | AI_TEMP_STAND_GROUND)
				self.Monsterinfo.Run.fn(g, self)
			}
			g.M_ChangeYaw(self)
			g.ai_checkattack(self, 0)
		} else {
			g.FindTarget(self)
		}
		return
	}

	if g.FindTarget(self) {
		return
	}

	if g.level.Time > self.Monsterinfo.Pausetime {
		self.Monsterinfo.Walk.fn(g, self)
		return
	}

	if self.Spawnflags&1 == 0 && self.Monsterinfo.Idle != nil && g.level.Time > self.Monsterinfo.IdleTime {
		if self.Monsterinfo.IdleTime != 0 {
			self.Monsterinfo.Idle.fn(g, self)
			self.Monsterinfo.IdleTime = g.level.Time + 15 + g.random()*15
		} else {
			self.Monsterinfo.IdleTime = g.level.Time + g.random()*15
		}
	}
}

// ai_walk: the monster is walking it's beat.
// C: game/g_ai.c:163 ai_walk
func (g *Game) ai_walk(self *Edict, dist float32) {
	g.M_MoveToGoal(self, dist)

	// check for noticing a player
	if g.FindTarget(self) {
		return
	}

	if self.Monsterinfo.Search != nil && g.level.Time > self.Monsterinfo.IdleTime {
		if self.Monsterinfo.IdleTime != 0 {
			self.Monsterinfo.Search.fn(g, self)
			self.Monsterinfo.IdleTime = g.level.Time + 15 + g.random()*15
		} else {
			self.Monsterinfo.IdleTime = g.level.Time + g.random()*15
		}
	}
}

// ai_charge turns towards target and advances.
// Use this call with a distnace of 0 to replace ai_face
// C: game/g_ai.c:194 ai_charge
func (g *Game) ai_charge(self *Edict, dist float32) {
	v := shared.VectorSubtract(self.Enemy.S.Origin, self.S.Origin)
	self.IdealYaw = vectoyaw(v)
	g.M_ChangeYaw(self)

	if dist != 0 {
		g.M_walkmove(self, self.S.Angles[YAW], dist)
	}
}

// ai_turn: don't move, but turn towards ideal_yaw.
// Distance is for slight position adjustments needed by the animations
// C: game/g_ai.c:215 ai_turn
func (g *Game) ai_turn(self *Edict, dist float32) {
	if dist != 0 {
		g.M_walkmove(self, self.S.Angles[YAW], dist)
	}

	if g.FindTarget(self) {
		return
	}

	g.M_ChangeYaw(self)
}

/*

.enemy
Will be world if not currently angry at anyone.

.movetarget
The next path spot to walk toward.  If .enemy, ignore .movetarget.
When an enemy is killed, the monster will try to return to it's path.

.hunt_time
Set to time + something when the player is in sight, but movement straight for
him is blocked.  This causes the monster to use wall following code for
movement direction instead of sighting on the player.

.ideal_yaw
A yaw angle of the intended direction, which will be turned towards at up
to 45 deg / state.  If the enemy is in view and hunt_time is not active,
this will be the exact line towards the enemy.

.pausetime
A monster will leave it's stand state and head towards it's .movetarget when
time > .pausetime.

walkmove(angle, speed) primitive is all or nothing
*/

// range_ (C range) returns the range catagorization of an entity reletive to self
// 0	melee range, will become hostile even if back is turned
// 1	visibility and infront, or visibility and show hostile
// 2	infront and show hostile
// 3	only triggered by damage
// C: game/g_ai.c:264 range
func (g *Game) range_(self, other *Edict) int32 {
	v := shared.VectorSubtract(self.S.Origin, other.S.Origin)
	len := shared.VectorLength(v)
	if len < MELEE_DISTANCE {
		return RANGE_MELEE
	}
	if len < 500 {
		return RANGE_NEAR
	}
	if len < 1000 {
		return RANGE_MID
	}
	return RANGE_FAR
}

// visible returns 1 if the entity is visible to self, even if not infront ()
// C: game/g_ai.c:287 visible
func (g *Game) visible(self, other *Edict) bool {
	spot1 := self.S.Origin
	spot1[2] += float32(self.Viewheight)
	spot2 := other.S.Origin
	spot2[2] += float32(other.Viewheight)
	origin := shared.Vec3Origin
	trace := g.gi.Trace(&spot1, &origin, &origin, &spot2, self, MASK_OPAQUE)

	if trace.Fraction == 1.0 {
		return true
	}
	return false
}

// infront returns 1 if the entity is in front (in sight) of self
// C: game/g_ai.c:312 infront
func (g *Game) infront(self, other *Edict) bool {
	var forward Vec3

	shared.AngleVectors(self.S.Angles, &forward, nil, nil)
	vec := shared.VectorSubtract(other.S.Origin, self.S.Origin)
	shared.VectorNormalize(&vec)
	dot := shared.DotProduct(vec, forward)

	if float64(dot) > 0.3 {
		return true
	}
	return false
}

//============================================================================

// C: game/g_ai.c:331 HuntTarget
func (g *Game) HuntTarget(self *Edict) {
	self.Goalentity = self.Enemy
	if self.Monsterinfo.Aiflags&AI_STAND_GROUND != 0 {
		self.Monsterinfo.Stand.fn(g, self)
	} else {
		self.Monsterinfo.Run.fn(g, self)
	}
	vec := shared.VectorSubtract(self.Enemy.S.Origin, self.S.Origin)
	self.IdealYaw = vectoyaw(vec)
	// wait a while before first attack
	if self.Monsterinfo.Aiflags&AI_STAND_GROUND == 0 {
		g.AttackFinished(self, 1)
	}
}

// C: game/g_ai.c:347 FoundTarget
func (g *Game) FoundTarget(self *Edict) {
	// let other monsters see this monster for a while
	if self.Enemy.Client != nil {
		g.level.SightEntity = self
		g.level.SightEntityFramenum = g.level.Framenum
		g.level.SightEntity.LightLevel = 128
	}

	// show_hostile is a qboolean (int) in C: the float time is truncated.
	self.ShowHostile = int32(g.level.Time + 1) // wake up other monsters

	self.Monsterinfo.LastSighting = self.Enemy.S.Origin
	self.Monsterinfo.TrailTime = g.level.Time

	if self.Combattarget == "" {
		g.HuntTarget(self)
		return
	}

	self.Movetarget = g.G_PickTarget(self.Combattarget)
	self.Goalentity = self.Movetarget
	if self.Movetarget == nil {
		self.Movetarget = self.Enemy
		self.Goalentity = self.Movetarget
		g.HuntTarget(self)
		g.dprintf("%s at %s, combattarget %s not found\n", self.Classname, vtos(self.S.Origin), self.Combattarget)
		return
	}

	// clear out our combattarget, these are a one shot deal
	self.Combattarget = ""
	self.Monsterinfo.Aiflags |= AI_COMBAT_POINT

	// clear the targetname, that point is ours!
	self.Movetarget.Targetname = ""
	self.Monsterinfo.Pausetime = 0

	// run for it
	self.Monsterinfo.Run.fn(g, self)
}

// FindTarget: self is currently not attacking anything, so try to find a target.
//
// # Returns TRUE if an enemy was sighted
//
// When a player fires a missile, the point of impact becomes a fakeplayer so
// that monsters that see the impact will respond as if they had seen the
// player.
//
// To avoid spending too much time, only a single client (or fakeclient) is
// checked each frame.  This means multi player games will have slightly
// slower noticing monsters.
// C: game/g_ai.c:407 FindTarget
func (g *Game) FindTarget(self *Edict) bool {
	var client *Edict
	var heardit bool
	var r int32

	if self.Monsterinfo.Aiflags&AI_GOOD_GUY != 0 {
		if self.Goalentity != nil && self.Goalentity.InUse && self.Goalentity.Classname != "" {
			if self.Goalentity.Classname == "target_actor" {
				return false
			}
		}

		//FIXME look for monsters?
		return false
	}

	// if we're going to a combat point, just proceed
	if self.Monsterinfo.Aiflags&AI_COMBAT_POINT != 0 {
		return false
	}

	// if the first spawnflag bit is set, the monster will only wake up on
	// really seeing the player, not another monster getting angry or hearing
	// something

	// revised behavior so they will wake up if they "see" a player make a noise
	// but not weapon impact/explosion noises

	heardit = false
	if (g.level.SightEntityFramenum >= (g.level.Framenum - 1)) && self.Spawnflags&1 == 0 {
		client = g.level.SightEntity
		if client.Enemy == self.Enemy {
			return false
		}
	} else if g.level.SoundEntityFramenum >= (g.level.Framenum - 1) {
		client = g.level.SoundEntity
		heardit = true
	} else if self.Enemy == nil && (g.level.Sound2EntityFramenum >= (g.level.Framenum - 1)) && self.Spawnflags&1 == 0 {
		client = g.level.Sound2Entity
		heardit = true
	} else {
		client = g.level.SightClient
		if client == nil {
			return false // no clients to get mad at
		}
	}

	// if the entity went away, forget it
	if !client.InUse {
		return false
	}

	if client == self.Enemy {
		return true // JDC false;
	}

	if client.Client != nil {
		if client.Flags&FL_NOTARGET != 0 {
			return false
		}
	} else if client.SVFlags&SVF_MONSTER != 0 {
		if client.Enemy == nil {
			return false
		}
		if client.Enemy.Flags&FL_NOTARGET != 0 {
			return false
		}
	} else if heardit {
		if client.Owner.Flags&FL_NOTARGET != 0 {
			return false
		}
	} else {
		return false
	}

	if !heardit {
		r = g.range_(self, client)

		if r == RANGE_FAR {
			return false
		}

		// this is where we would check invisibility

		// is client in an spot too dark to be seen?
		if client.LightLevel <= 5 {
			return false
		}

		if !g.visible(self, client) {
			return false
		}

		if r == RANGE_NEAR {
			if float32(client.ShowHostile) < g.level.Time && !g.infront(self, client) {
				return false
			}
		} else if r == RANGE_MID {
			if !g.infront(self, client) {
				return false
			}
		}

		self.Enemy = client

		if self.Enemy.Classname != "player_noise" {
			self.Monsterinfo.Aiflags &^= AI_SOUND_TARGET

			if self.Enemy.Client == nil {
				self.Enemy = self.Enemy.Enemy
				if self.Enemy.Client == nil {
					self.Enemy = nil
					return false
				}
			}
		}
	} else { // heardit
		if self.Spawnflags&1 != 0 {
			if !g.visible(self, client) {
				return false
			}
		} else {
			if !g.gi.InPHS(&self.S.Origin, &client.S.Origin) {
				return false
			}
		}

		temp := shared.VectorSubtract(client.S.Origin, self.S.Origin)

		if shared.VectorLength(temp) > 1000 { // too far to hear
			return false
		}

		// check area portals - if they are different and not connected then we can't hear it
		if client.AreaNum != self.AreaNum {
			if !g.gi.AreasConnected(int(self.AreaNum), int(client.AreaNum)) {
				return false
			}
		}

		self.IdealYaw = vectoyaw(temp)
		g.M_ChangeYaw(self)

		// hunt the sound for a bit; hopefully find the real player
		self.Monsterinfo.Aiflags |= AI_SOUND_TARGET
		self.Enemy = client
	}

	//
	// got one
	//
	g.FoundTarget(self)

	if self.Monsterinfo.Aiflags&AI_SOUND_TARGET == 0 && self.Monsterinfo.Sight != nil {
		self.Monsterinfo.Sight.fn(g, self, self.Enemy)
	}

	return true
}

//=============================================================================

// C: game/g_ai.c:594 FacingIdeal
func (g *Game) FacingIdeal(self *Edict) bool {
	delta := shared.Anglemod(self.S.Angles[YAW] - self.IdealYaw)
	if delta > 45 && delta < 315 {
		return false
	}
	return true
}

//=============================================================================

// C: game/g_ai.c:607 M_CheckAttack
func (g *Game) M_CheckAttack(self *Edict) bool {
	var chance float32

	if self.Enemy.Health > 0 {
		// see if any entities are in the way of the shot
		spot1 := self.S.Origin
		spot1[2] += float32(self.Viewheight)
		spot2 := self.Enemy.S.Origin
		spot2[2] += float32(self.Enemy.Viewheight)

		tr := g.gi.Trace(&spot1, nil, nil, &spot2, self, CONTENTS_SOLID|CONTENTS_MONSTER|CONTENTS_SLIME|CONTENTS_LAVA|CONTENTS_WINDOW)

		// do we have a clear shot?
		if tr.Ent != self.Enemy {
			return false
		}
	}

	// melee attack
	if g.enemy_range == RANGE_MELEE {
		// don't always melee in easy mode
		if g.skill.Value == 0 && (g.rng.Rand()&3) != 0 {
			return false
		}
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

	if g.enemy_range == RANGE_FAR {
		return false
	}

	if self.Monsterinfo.Aiflags&AI_STAND_GROUND != 0 {
		chance = float32(0.4)
	} else if g.enemy_range == RANGE_MELEE {
		chance = float32(0.2)
	} else if g.enemy_range == RANGE_NEAR {
		chance = float32(0.1)
	} else if g.enemy_range == RANGE_MID {
		chance = float32(0.02)
	} else {
		return false
	}

	if g.skill.Value == 0 {
		chance = float32(float64(chance) * 0.5)
	} else if g.skill.Value >= 2 {
		chance *= 2
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

// ai_run_melee: turn and close until within an angle to launch a melee attack.
// C: game/g_ai.c:703 ai_run_melee
func (g *Game) ai_run_melee(self *Edict) {
	self.IdealYaw = g.enemy_yaw
	g.M_ChangeYaw(self)

	if g.FacingIdeal(self) {
		self.Monsterinfo.Melee.fn(g, self)
		self.Monsterinfo.AttackState = AS_STRAIGHT
	}
}

// ai_run_missile: turn in place until within an angle to launch a missile attack.
// C: game/g_ai.c:723 ai_run_missile
func (g *Game) ai_run_missile(self *Edict) {
	self.IdealYaw = g.enemy_yaw
	g.M_ChangeYaw(self)

	if g.FacingIdeal(self) {
		self.Monsterinfo.Attack.fn(g, self)
		self.Monsterinfo.AttackState = AS_STRAIGHT
	}
}

// ai_run_slide: strafe sideways, but stay at aproximately the same range.
// C: game/g_ai.c:743 ai_run_slide
func (g *Game) ai_run_slide(self *Edict, distance float32) {
	var ofs float32

	self.IdealYaw = g.enemy_yaw
	g.M_ChangeYaw(self)

	if self.Monsterinfo.Lefty != 0 {
		ofs = 90
	} else {
		ofs = -90
	}

	if g.M_walkmove(self, self.IdealYaw+ofs, distance) {
		return
	}

	self.Monsterinfo.Lefty = 1 - self.Monsterinfo.Lefty
	g.M_walkmove(self, self.IdealYaw-ofs, distance)
}

// ai_checkattack decides if we're going to attack or do something else.
// used by ai_run and ai_stand
// C: game/g_ai.c:771 ai_checkattack
func (g *Game) ai_checkattack(self *Edict, dist float32) bool {
	var hesDeadJim bool

	// this causes monsters to run blindly to the combat point w/o firing
	if self.Goalentity != nil {
		if self.Monsterinfo.Aiflags&AI_COMBAT_POINT != 0 {
			return false
		}

		if self.Monsterinfo.Aiflags&AI_SOUND_TARGET != 0 {
			if float64(g.level.Time-self.Enemy.TeleportTime) > 5.0 {
				if self.Goalentity == self.Enemy {
					if self.Movetarget != nil {
						self.Goalentity = self.Movetarget
					} else {
						self.Goalentity = nil
					}
				}
				self.Monsterinfo.Aiflags &^= AI_SOUND_TARGET
				if self.Monsterinfo.Aiflags&AI_TEMP_STAND_GROUND != 0 {
					self.Monsterinfo.Aiflags &^= (AI_STAND_GROUND | AI_TEMP_STAND_GROUND)
				}
			} else {
				self.ShowHostile = int32(g.level.Time + 1)
				return false
			}
		}
	}

	g.enemy_vis = false

	// see if the enemy is dead
	hesDeadJim = false
	if self.Enemy == nil || !self.Enemy.InUse {
		hesDeadJim = true
	} else if self.Monsterinfo.Aiflags&AI_MEDIC != 0 {
		if self.Enemy.Health > 0 {
			hesDeadJim = true
			self.Monsterinfo.Aiflags &^= AI_MEDIC
		}
	} else {
		if self.Monsterinfo.Aiflags&AI_BRUTAL != 0 {
			if self.Enemy.Health <= -80 {
				hesDeadJim = true
			}
		} else {
			if self.Enemy.Health <= 0 {
				hesDeadJim = true
			}
		}
	}

	if hesDeadJim {
		self.Enemy = nil
		// FIXME: look all around for other targets
		if self.Oldenemy != nil && self.Oldenemy.Health > 0 {
			self.Enemy = self.Oldenemy
			self.Oldenemy = nil
			g.HuntTarget(self)
		} else {
			if self.Movetarget != nil {
				self.Goalentity = self.Movetarget
				self.Monsterinfo.Walk.fn(g, self)
			} else {
				// we need the pausetime otherwise the stand code
				// will just revert to walking with no target and
				// the monsters will wonder around aimlessly trying
				// to hunt the world entity
				self.Monsterinfo.Pausetime = g.level.Time + 100000000
				self.Monsterinfo.Stand.fn(g, self)
			}
			return true
		}
	}

	self.ShowHostile = int32(g.level.Time + 1) // wake up other monsters

	// check knowledge of enemy
	g.enemy_vis = g.visible(self, self.Enemy)
	if g.enemy_vis {
		self.Monsterinfo.SearchTime = g.level.Time + 5
		self.Monsterinfo.LastSighting = self.Enemy.S.Origin
	}

	// look for other coop players here
	//	if (coop && self->monsterinfo.search_time < level.time)
	//	{
	//		if (FindTarget (self))
	//			return true;
	//	}

	g.enemy_infront = g.infront(self, self.Enemy)
	g.enemy_range = g.range_(self, self.Enemy)
	temp := shared.VectorSubtract(self.Enemy.S.Origin, self.S.Origin)
	g.enemy_yaw = vectoyaw(temp)

	// JDC self->ideal_yaw = enemy_yaw;

	if self.Monsterinfo.AttackState == AS_MISSILE {
		g.ai_run_missile(self)
		return true
	}
	if self.Monsterinfo.AttackState == AS_MELEE {
		g.ai_run_melee(self)
		return true
	}

	// if enemy is not currently visible, we will never attack
	if !g.enemy_vis {
		return false
	}

	return self.Monsterinfo.Checkattack.fn(g, self)
}

// ai_run: the monster has an enemy it is trying to kill.
// C: game/g_ai.c:914 ai_run
func (g *Game) ai_run(self *Edict, dist float32) {
	var v Vec3
	var new bool
	var marker *Edict
	var d1, d2 float32
	var tr Trace
	var v_forward, v_right Vec3
	var left, center, right float32
	var left_target, right_target Vec3

	// if we're going to a combat point, just proceed
	if self.Monsterinfo.Aiflags&AI_COMBAT_POINT != 0 {
		g.M_MoveToGoal(self, dist)
		return
	}

	if self.Monsterinfo.Aiflags&AI_SOUND_TARGET != 0 {
		v = shared.VectorSubtract(self.S.Origin, self.Enemy.S.Origin)
		if shared.VectorLength(v) < 64 {
			self.Monsterinfo.Aiflags |= (AI_STAND_GROUND | AI_TEMP_STAND_GROUND)
			self.Monsterinfo.Stand.fn(g, self)
			return
		}

		g.M_MoveToGoal(self, dist)

		if !g.FindTarget(self) {
			return
		}
	}

	if g.ai_checkattack(self, dist) {
		return
	}

	if self.Monsterinfo.AttackState == AS_SLIDING {
		g.ai_run_slide(self, dist)
		return
	}

	if g.enemy_vis {
		//		if (self.aiflags & AI_LOST_SIGHT)
		//			dprint("regained sight\n");
		g.M_MoveToGoal(self, dist)
		self.Monsterinfo.Aiflags &^= AI_LOST_SIGHT
		self.Monsterinfo.LastSighting = self.Enemy.S.Origin
		self.Monsterinfo.TrailTime = g.level.Time
		return
	}

	// coop will change to another enemy if visible
	if g.coop.Value != 0 { // FIXME: insane guys get mad with this, which causes crashes!
		if g.FindTarget(self) {
			return
		}
	}

	if self.Monsterinfo.SearchTime != 0 && (g.level.Time > (self.Monsterinfo.SearchTime + 20)) {
		g.M_MoveToGoal(self, dist)
		self.Monsterinfo.SearchTime = 0
		//		dprint("search timeout\n");
		return
	}

	save := self.Goalentity
	tempgoal := g.G_Spawn()
	self.Goalentity = tempgoal

	new = false

	if self.Monsterinfo.Aiflags&AI_LOST_SIGHT == 0 {
		// just lost sight of the player, decide where to go first
		//		dprint("lost sight of player, last seen at "); dprint(vtos(self.last_sighting)); dprint("\n");
		self.Monsterinfo.Aiflags |= (AI_LOST_SIGHT | AI_PURSUIT_LAST_SEEN)
		self.Monsterinfo.Aiflags &^= (AI_PURSUE_NEXT | AI_PURSUE_TEMP)
		new = true
	}

	if self.Monsterinfo.Aiflags&AI_PURSUE_NEXT != 0 {
		self.Monsterinfo.Aiflags &^= AI_PURSUE_NEXT
		//		dprint("reached current goal: "); ...

		// give ourself more time since we got this far
		self.Monsterinfo.SearchTime = g.level.Time + 5

		if self.Monsterinfo.Aiflags&AI_PURSUE_TEMP != 0 {
			//			dprint("was temp goal; retrying original\n");
			self.Monsterinfo.Aiflags &^= AI_PURSUE_TEMP
			marker = nil
			self.Monsterinfo.LastSighting = self.Monsterinfo.SavedGoal
			new = true
		} else if self.Monsterinfo.Aiflags&AI_PURSUIT_LAST_SEEN != 0 {
			self.Monsterinfo.Aiflags &^= AI_PURSUIT_LAST_SEEN
			marker = g.PlayerTrail_PickFirst(self)
		} else {
			marker = g.PlayerTrail_PickNext(self)
		}

		if marker != nil {
			self.Monsterinfo.LastSighting = marker.S.Origin
			self.Monsterinfo.TrailTime = marker.Timestamp
			self.IdealYaw = marker.S.Angles[YAW]
			self.S.Angles[YAW] = self.IdealYaw
			//			dprint("heading is "); dprint(ftos(self.ideal_yaw)); dprint("\n");

			//			debug_drawline(self.origin, self.last_sighting, 52);
			new = true
		}
	}

	v = shared.VectorSubtract(self.S.Origin, self.Monsterinfo.LastSighting)
	d1 = shared.VectorLength(v)
	if d1 <= dist {
		self.Monsterinfo.Aiflags |= AI_PURSUE_NEXT
		dist = d1
	}

	self.Goalentity.S.Origin = self.Monsterinfo.LastSighting

	if new {
		//		gi.dprintf("checking for course correction\n");

		tr = g.gi.Trace(&self.S.Origin, &self.Mins, &self.Maxs, &self.Monsterinfo.LastSighting, self, MASK_PLAYERSOLID)
		if tr.Fraction < 1 {
			v = shared.VectorSubtract(self.Goalentity.S.Origin, self.S.Origin)
			d1 = shared.VectorLength(v)
			center = tr.Fraction
			d2 = d1 * ((center + 1) / 2)
			self.IdealYaw = vectoyaw(v)
			self.S.Angles[YAW] = self.IdealYaw
			shared.AngleVectors(self.S.Angles, &v_forward, &v_right, nil)

			v = Vec3{d2, -16, 0}
			left_target = G_ProjectSource(self.S.Origin, v, v_forward, v_right)
			tr = g.gi.Trace(&self.S.Origin, &self.Mins, &self.Maxs, &left_target, self, MASK_PLAYERSOLID)
			left = tr.Fraction

			v = Vec3{d2, 16, 0}
			right_target = G_ProjectSource(self.S.Origin, v, v_forward, v_right)
			tr = g.gi.Trace(&self.S.Origin, &self.Mins, &self.Maxs, &right_target, self, MASK_PLAYERSOLID)
			right = tr.Fraction

			center = (d1 * center) / d2
			if left >= center && left > right {
				if left < 1 {
					v = Vec3{float32(float64(d2*left) * 0.5), -16, 0}
					left_target = G_ProjectSource(self.S.Origin, v, v_forward, v_right)
					//					gi.dprintf("incomplete path, go part way and adjust again\n");
				}
				self.Monsterinfo.SavedGoal = self.Monsterinfo.LastSighting
				self.Monsterinfo.Aiflags |= AI_PURSUE_TEMP
				self.Goalentity.S.Origin = left_target
				self.Monsterinfo.LastSighting = left_target
				v = shared.VectorSubtract(self.Goalentity.S.Origin, self.S.Origin)
				self.IdealYaw = vectoyaw(v)
				self.S.Angles[YAW] = self.IdealYaw
				//				gi.dprintf("adjusted left\n");
				//				debug_drawline(self.origin, self.last_sighting, 152);
			} else if right >= center && right > left {
				if right < 1 {
					v = Vec3{float32(float64(d2*right) * 0.5), 16, 0}
					right_target = G_ProjectSource(self.S.Origin, v, v_forward, v_right)
					//					gi.dprintf("incomplete path, go part way and adjust again\n");
				}
				self.Monsterinfo.SavedGoal = self.Monsterinfo.LastSighting
				self.Monsterinfo.Aiflags |= AI_PURSUE_TEMP
				self.Goalentity.S.Origin = right_target
				self.Monsterinfo.LastSighting = right_target
				v = shared.VectorSubtract(self.Goalentity.S.Origin, self.S.Origin)
				self.IdealYaw = vectoyaw(v)
				self.S.Angles[YAW] = self.IdealYaw
				//				gi.dprintf("adjusted right\n");
				//				debug_drawline(self.origin, self.last_sighting, 152);
			}
		}
		//		else gi.dprintf("course was fine\n");
	}

	g.M_MoveToGoal(self, dist)

	g.G_FreeEdict(tempgoal)

	if self != nil {
		self.Goalentity = save
	}
}
