package game

// Port of game/g_turret.c.

import (
	"math"

	. "quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/shared"
)

var (
	turret_blocked            = defBlocked("turret_blocked")
	turret_breach_think       = defThink("turret_breach_think")
	turret_breach_finish_init = defThink("turret_breach_finish_init")
	turret_driver_die         = defDie("turret_driver_die")
	turret_driver_think       = defThink("turret_driver_think")
	turret_driver_link        = defThink("turret_driver_link")
)

func init() {
	turret_blocked.bind((*Game).turret_blocked)
	turret_breach_think.bind((*Game).turret_breach_think)
	turret_breach_finish_init.bind((*Game).turret_breach_finish_init)
	turret_driver_die.bind((*Game).turret_driver_die)
	turret_driver_think.bind((*Game).turret_driver_think)
	turret_driver_link.bind((*Game).turret_driver_link)
}

// C: game/g_turret.c:25 AnglesNormalize
func AnglesNormalize(vec *Vec3) {
	for vec[0] > 360 {
		vec[0] -= 360
	}
	for vec[0] < 0 {
		vec[0] += 360
	}
	for vec[1] > 360 {
		vec[1] -= 360
	}
	for vec[1] < 0 {
		vec[1] += 360
	}
}

// C: game/g_turret.c:37 SnapToEights
func SnapToEights(x float32) float32 {
	x = float32(float64(x) * 8.0)
	if x > 0.0 {
		x = float32(float64(x) + 0.5)
	} else {
		x = float32(float64(x) - 0.5)
	}
	return float32(0.125 * float64(int32(x)))
}

// C: game/g_turret.c:48 turret_blocked
func (g *Game) turret_blocked(self, other *Edict) {
	var attacker *Edict

	if other.Takedamage != 0 {
		if self.Teammaster.Owner != nil {
			attacker = self.Teammaster.Owner
		} else {
			attacker = self.Teammaster
		}
		dir := shared.Vec3Origin
		g.T_Damage(other, self, attacker, &dir, other.S.Origin, shared.Vec3Origin, self.Teammaster.Dmg, 10, 0, MOD_CRUSH)
	}
}

/*QUAKED turret_breach (0 0 0) ?
This portion of the turret can change both pitch and yaw.
The model  should be made with a flat pitch.
It (and the associated base) need to be oriented towards 0.
Use "angle" to set the starting angle.

"speed"		default 50
"dmg"		default 10
"angle"		point this forward
"target"	point this at an info_notnull at the muzzle tip
"minpitch"	min acceptable pitch angle : default -30
"maxpitch"	max acceptable pitch angle : default 30
"minyaw"	min acceptable yaw angle   : default 0
"maxyaw"	max acceptable yaw angle   : default 360
*/

// C: game/g_turret.c:78 turret_breach_fire
func (g *Game) turret_breach_fire(self *Edict) {
	var f, r, u Vec3

	shared.AngleVectors(self.S.Angles, &f, &r, &u)
	start := shared.VectorMA(self.S.Origin, self.MoveOrigin[0], f)
	start = shared.VectorMA(start, self.MoveOrigin[1], r)
	start = shared.VectorMA(start, self.MoveOrigin[2], u)

	damage := int32(100 + g.random()*50)
	speed := int32(550 + 50*g.skill.Value)
	g.fire_rocket(self.Teammaster.Owner, start, f, damage, speed, 150, damage)
	g.gi.PositionedSound(&start, self, CHAN_WEAPON, g.gi.SoundIndex("weapons/rocklf1a.wav"), 1, ATTN_NORM, 0)
}

// C: game/g_turret.c:96 turret_breach_think
func (g *Game) turret_breach_think(self *Edict) {
	currentAngles := self.S.Angles
	AnglesNormalize(&currentAngles)

	AnglesNormalize(&self.MoveAngles)
	if self.MoveAngles[PITCH] > 180 {
		self.MoveAngles[PITCH] -= 360
	}

	// clamp angles to mins & maxs
	if self.MoveAngles[PITCH] > self.Pos1[PITCH] {
		self.MoveAngles[PITCH] = self.Pos1[PITCH]
	} else if self.MoveAngles[PITCH] < self.Pos2[PITCH] {
		self.MoveAngles[PITCH] = self.Pos2[PITCH]
	}

	if self.MoveAngles[YAW] < self.Pos1[YAW] || self.MoveAngles[YAW] > self.Pos2[YAW] {
		var dmin, dmax float32

		dmin = float32(math.Abs(float64(self.Pos1[YAW] - self.MoveAngles[YAW])))
		if dmin < -180 {
			dmin += 360
		} else if dmin > 180 {
			dmin -= 360
		}
		dmax = float32(math.Abs(float64(self.Pos2[YAW] - self.MoveAngles[YAW])))
		if dmax < -180 {
			dmax += 360
		} else if dmax > 180 {
			dmax -= 360
		}
		if math.Abs(float64(dmin)) < math.Abs(float64(dmax)) {
			self.MoveAngles[YAW] = self.Pos1[YAW]
		} else {
			self.MoveAngles[YAW] = self.Pos2[YAW]
		}
	}

	delta := shared.VectorSubtract(self.MoveAngles, currentAngles)
	if delta[0] < -180 {
		delta[0] += 360
	} else if delta[0] > 180 {
		delta[0] -= 360
	}
	if delta[1] < -180 {
		delta[1] += 360
	} else if delta[1] > 180 {
		delta[1] -= 360
	}
	delta[2] = 0

	sf := float64(self.Speed) * FRAMETIME
	nsf := float64(-1*self.Speed) * FRAMETIME
	if float64(delta[0]) > sf {
		delta[0] = float32(sf)
	}
	if float64(delta[0]) < nsf {
		delta[0] = float32(nsf)
	}
	if float64(delta[1]) > sf {
		delta[1] = float32(sf)
	}
	if float64(delta[1]) < nsf {
		delta[1] = float32(nsf)
	}

	self.Avelocity = shared.VectorScale(delta, float32(1.0/FRAMETIME))

	self.Nextthink = float32(float64(g.level.Time) + FRAMETIME)

	for ent := self.Teammaster; ent != nil; ent = ent.Teamchain {
		ent.Avelocity[1] = self.Avelocity[1]
	}

	// if we have adriver, adjust his velocities
	if self.Owner != nil {
		var angle, targetZ, diff float32
		var target Vec3

		// angular is easy, just copy ours
		self.Owner.Avelocity[0] = self.Avelocity[0]
		self.Owner.Avelocity[1] = self.Avelocity[1]

		// x & y
		angle = self.S.Angles[1] + self.Owner.MoveOrigin[1]
		angle = float32(float64(angle) * (shared.MPI * 2 / 360))
		target[0] = SnapToEights(float32(float64(self.S.Origin[0]) + math.Cos(float64(angle))*float64(self.Owner.MoveOrigin[0])))
		target[1] = SnapToEights(float32(float64(self.S.Origin[1]) + math.Sin(float64(angle))*float64(self.Owner.MoveOrigin[0])))
		target[2] = self.Owner.S.Origin[2]

		dir := shared.VectorSubtract(target, self.Owner.S.Origin)
		self.Owner.Velocity[0] = float32(float64(dir[0]) * 1.0 / FRAMETIME)
		self.Owner.Velocity[1] = float32(float64(dir[1]) * 1.0 / FRAMETIME)

		// z
		angle = float32(float64(self.S.Angles[PITCH]) * (shared.MPI * 2 / 360))
		targetZ = SnapToEights(float32(float64(self.S.Origin[2]) + float64(self.Owner.MoveOrigin[0])*math.Tan(float64(angle)) + float64(self.Owner.MoveOrigin[2])))

		diff = targetZ - self.Owner.S.Origin[2]
		self.Owner.Velocity[2] = float32(float64(diff) * 1.0 / FRAMETIME)

		if self.Spawnflags&65536 != 0 {
			g.turret_breach_fire(self)
			self.Spawnflags &^= 65536
		}
	}
}

// C: game/g_turret.c:201 turret_breach_finish_init
func (g *Game) turret_breach_finish_init(self *Edict) {
	// get and save info for muzzle location
	if self.Target == "" {
		g.dprintf("%s at %s needs a target\n", self.Classname, vtos(self.S.Origin))
	} else {
		self.TargetEnt = g.G_PickTarget(self.Target)
		self.MoveOrigin = shared.VectorSubtract(self.TargetEnt.S.Origin, self.S.Origin)
		g.G_FreeEdict(self.TargetEnt)
	}

	self.Teammaster.Dmg = self.Dmg
	self.Think = turret_breach_think
	self.Think.fn(g, self)
}

// C: game/g_turret.c:220 SP_turret_breach
func (g *Game) SP_turret_breach(self *Edict) {
	self.Solid = SOLID_BSP
	self.Movetype = MOVETYPE_PUSH
	g.gi.SetModel(self, self.Model)

	if self.Speed == 0 {
		self.Speed = 50
	}
	if self.Dmg == 0 {
		self.Dmg = 10
	}

	if g.st.Minpitch == 0 {
		g.st.Minpitch = -30
	}
	if g.st.Maxpitch == 0 {
		g.st.Maxpitch = 30
	}
	if g.st.Maxyaw == 0 {
		g.st.Maxyaw = 360
	}

	self.Pos1[PITCH] = -1 * g.st.Minpitch
	self.Pos1[YAW] = g.st.Minyaw
	self.Pos2[PITCH] = -1 * g.st.Maxpitch
	self.Pos2[YAW] = g.st.Maxyaw

	self.IdealYaw = self.S.Angles[YAW]
	self.MoveAngles[YAW] = self.IdealYaw

	self.Blocked = turret_blocked

	self.Think = turret_breach_finish_init
	self.Nextthink = float32(float64(g.level.Time) + FRAMETIME)
	g.gi.LinkEntity(self)
}

/*QUAKED turret_base (0 0 0) ?
This portion of the turret changes yaw only.
MUST be teamed with a turret_breach.
*/

// C: game/g_turret.c:259 SP_turret_base
func (g *Game) SP_turret_base(self *Edict) {
	self.Solid = SOLID_BSP
	self.Movetype = MOVETYPE_PUSH
	g.gi.SetModel(self, self.Model)
	self.Blocked = turret_blocked
	g.gi.LinkEntity(self)
}

/*QUAKED turret_driver (1 .5 0) (-16 -16 -24) (16 16 32)
Must NOT be on the team with the rest of the turret parts.
Instead it must target the turret_breach.
*/

// C: game/g_turret.c:278 turret_driver_die
func (g *Game) turret_driver_die(self, inflictor, attacker *Edict, damage int32, point Vec3) {
	var ent *Edict

	// level the gun
	self.TargetEnt.MoveAngles[0] = 0

	// remove the driver from the end of them team chain
	for ent = self.TargetEnt.Teammaster; ent.Teamchain != self; ent = ent.Teamchain {
	}
	ent.Teamchain = nil
	self.Teammaster = nil
	self.Flags &^= FL_TEAMSLAVE

	self.TargetEnt.Owner = nil
	self.TargetEnt.Teammaster.Owner = nil

	// infantry_die lives in m_infantry.c; C calls it with 4 arguments (point is garbage).
	if fn := dieByName("infantry_die"); fn != nil {
		fn.fn(g, self, inflictor, attacker, damage, Vec3{})
	}
}

// C: game/g_turret.c:300 turret_driver_think
func (g *Game) turret_driver_think(self *Edict) {
	var reactionTime float32

	self.Nextthink = float32(float64(g.level.Time) + FRAMETIME)

	if self.Enemy != nil && (!self.Enemy.InUse || self.Enemy.Health <= 0) {
		self.Enemy = nil
	}

	if self.Enemy == nil {
		if !g.FindTarget(self) {
			return
		}
		self.Monsterinfo.TrailTime = g.level.Time
		self.Monsterinfo.Aiflags &^= AI_LOST_SIGHT
	} else {
		if g.visible(self, self.Enemy) {
			if self.Monsterinfo.Aiflags&AI_LOST_SIGHT != 0 {
				self.Monsterinfo.TrailTime = g.level.Time
				self.Monsterinfo.Aiflags &^= AI_LOST_SIGHT
			}
		} else {
			self.Monsterinfo.Aiflags |= AI_LOST_SIGHT
			return
		}
	}

	// let the turret know where we want it to aim
	target := self.Enemy.S.Origin
	target[2] += float32(self.Enemy.Viewheight)
	dir := shared.VectorSubtract(target, self.TargetEnt.S.Origin)
	self.TargetEnt.MoveAngles = vectoangles(dir)

	// decide if we should shoot
	if g.level.Time < self.Monsterinfo.AttackFinished {
		return
	}

	reactionTime = float32(float64(3-g.skill.Value) * 1.0)
	if (g.level.Time - self.Monsterinfo.TrailTime) < reactionTime {
		return
	}

	self.Monsterinfo.AttackFinished = float32(float64(g.level.Time+reactionTime) + 1.0)
	//FIXME how do we really want to pass this along?
	self.TargetEnt.Spawnflags |= 65536
}

// C: game/g_turret.c:354 turret_driver_link
func (g *Game) turret_driver_link(self *Edict) {
	var vec Vec3
	var ent *Edict

	self.Think = turret_driver_think
	self.Nextthink = float32(float64(g.level.Time) + FRAMETIME)

	self.TargetEnt = g.G_PickTarget(self.Target)
	self.TargetEnt.Owner = self
	self.TargetEnt.Teammaster.Owner = self
	self.S.Angles = self.TargetEnt.S.Angles

	vec[0] = self.TargetEnt.S.Origin[0] - self.S.Origin[0]
	vec[1] = self.TargetEnt.S.Origin[1] - self.S.Origin[1]
	vec[2] = 0
	self.MoveOrigin[0] = shared.VectorLength(vec)

	vec = shared.VectorSubtract(self.S.Origin, self.TargetEnt.S.Origin)
	vec = vectoangles(vec)
	AnglesNormalize(&vec)
	self.MoveOrigin[1] = vec[1]

	self.MoveOrigin[2] = self.S.Origin[2] - self.TargetEnt.S.Origin[2]

	// add the driver to the end of them team chain
	for ent = self.TargetEnt.Teammaster; ent.Teamchain != nil; ent = ent.Teamchain {
	}
	ent.Teamchain = self
	self.Teammaster = self.TargetEnt.Teammaster
	self.Flags |= FL_TEAMSLAVE
}

// C: game/g_turret.c:387 SP_turret_driver
func (g *Game) SP_turret_driver(self *Edict) {
	if g.deathmatch.Value != 0 {
		g.G_FreeEdict(self)
		return
	}

	self.Movetype = MOVETYPE_PUSH
	self.Solid = SOLID_BBOX
	self.S.ModelIndex = int32(g.gi.ModelIndex("models/monsters/infantry/tris.md2"))
	self.Mins = Vec3{-16, -16, -24}
	self.Maxs = Vec3{16, 16, 32}

	self.Health = 100
	self.GibHealth = 0
	self.Mass = 200
	self.Viewheight = 24

	self.Die = turret_driver_die
	// infantry_stand lives in m_infantry.c (nil until that file is ported).
	self.Monsterinfo.Stand = thinkByName("infantry_stand")

	self.Flags |= FL_NO_KNOCKBACK

	g.level.TotalMonsters++

	self.SVFlags |= SVF_MONSTER
	self.S.RenderFX |= RF_FRAMELERP
	self.Takedamage = DAMAGE_AIM
	self.Use = monster_use
	self.ClipMask = MASK_MONSTERSOLID
	self.S.OldOrigin = self.S.Origin
	self.Monsterinfo.Aiflags |= AI_STAND_GROUND | AI_DUCKED

	if g.st.Item != "" {
		self.Item = g.FindItemByClassname(g.st.Item)
		if self.Item == nil {
			g.dprintf("%s at %s has bad item: %s\n", self.Classname, vtos(self.S.Origin), g.st.Item)
		}
	}

	self.Think = turret_driver_link
	self.Nextthink = float32(float64(g.level.Time) + FRAMETIME)

	g.gi.LinkEntity(self)
}
