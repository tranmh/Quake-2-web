package game

// Port of game/g_weapon.c.

import (
	"math"
	"strings"

	. "quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/shared"
)

// Function pointer handles of g_weapon.c.
var (
	blaster_touch   = defTouch("blaster_touch")
	Grenade_Explode = defThink("Grenade_Explode")
	Grenade_Touch   = defTouch("Grenade_Touch")
	rocket_touch    = defTouch("rocket_touch")
	bfg_explode     = defThink("bfg_explode")
	bfg_touch       = defTouch("bfg_touch")
	bfg_think       = defThink("bfg_think")
)

func init() {
	blaster_touch.bind((*Game).blaster_touch)
	Grenade_Explode.bind((*Game).Grenade_Explode)
	Grenade_Touch.bind((*Game).Grenade_Touch)
	rocket_touch.bind((*Game).rocket_touch)
	bfg_explode.bind((*Game).bfg_explode)
	bfg_touch.bind((*Game).bfg_touch)
	bfg_think.bind((*Game).bfg_think)
}

// weaponSurfName is tr.surface->name (the engine always sets a surface;
// a nil surface reads as the null surface "").
func weaponSurfName(s *CSurface) string {
	if s == nil {
		return ""
	}
	return s.Name
}

// weaponPlaneNormal is plane->normal where plane may be NULL: C passes the
// NULL pointer on, and MSG_WriteDir(NULL) writes the same byte (0) as a
// zero vector, so a zero normal is equivalent.
func weaponPlaneNormal(plane *CPlane) Vec3 {
	if plane == nil {
		return Vec3{}
	}
	return plane.Normal
}

// check_dodge is a support routine used when a client is firing
// a non-instant attack weapon.  It checks to see if a
// monster's dodge function should be called.
// C: game/g_weapon.c:32 check_dodge
func (g *Game) check_dodge(self *Edict, start, dir Vec3, speed int32) {
	// easy mode only ducks one quarter the time
	if g.skill.Value == 0 {
		if float64(g.random()) > 0.25 {
			return
		}
	}
	end := shared.VectorMA(start, 8192, dir)
	tr := g.gi.Trace(&start, nil, nil, &end, self, MASK_SHOT)
	if tr.Ent != nil && tr.Ent.SVFlags&SVF_MONSTER != 0 && tr.Ent.Health > 0 && tr.Ent.Monsterinfo.Dodge != nil && g.infront(tr.Ent, self) {
		v := shared.VectorSubtract(tr.EndPos, start)
		eta := (shared.VectorLength(v) - tr.Ent.Maxs[0]) / float32(speed)
		tr.Ent.Monsterinfo.Dodge.fn(g, tr.Ent, self, eta)
	}
}

// fire_hit is used for all impact (hit/punch/slash) attacks.
// C: game/g_weapon.c:63 fire_hit
func (g *Game) fire_hit(self *Edict, aim Vec3, damage, kick int32) bool {
	var forward, right, up Vec3

	//see if enemy is in range
	dir := shared.VectorSubtract(self.Enemy.S.Origin, self.S.Origin)
	rng := shared.VectorLength(dir)
	if rng > aim[0] {
		return false
	}

	if aim[1] > self.Mins[0] && aim[1] < self.Maxs[0] {
		// the hit is straight on so back the range up to the edge of their bbox
		rng -= self.Enemy.Maxs[0]
	} else {
		// this is a side hit so adjust the "right" value out to the edge of their bbox
		if aim[1] < 0 {
			aim[1] = self.Enemy.Mins[0]
		} else {
			aim[1] = self.Enemy.Maxs[0]
		}
	}

	point := shared.VectorMA(self.S.Origin, rng, dir)

	tr := g.gi.Trace(&self.S.Origin, nil, nil, &point, self, MASK_SHOT)
	if tr.Fraction < 1 {
		if tr.Ent.Takedamage == 0 {
			return false
		}
		// if it will hit any client/monster then hit the one we wanted to hit
		if tr.Ent.SVFlags&SVF_MONSTER != 0 || tr.Ent.Client != nil {
			tr.Ent = self.Enemy
		}
	}

	shared.AngleVectors(self.S.Angles, &forward, &right, &up)
	point = shared.VectorMA(self.S.Origin, rng, forward)
	point = shared.VectorMA(point, aim[1], right)
	point = shared.VectorMA(point, aim[2], up)
	dir = shared.VectorSubtract(point, self.Enemy.S.Origin)

	// do the damage
	g.T_Damage(tr.Ent, self, self, &dir, point, shared.Vec3Origin, damage, kick/2, DAMAGE_NO_KNOCKBACK, MOD_HIT)

	if tr.Ent.SVFlags&SVF_MONSTER == 0 && tr.Ent.Client == nil {
		return false
	}

	// do our special form of knockback here
	v := shared.VectorMA(self.Enemy.AbsMin, 0.5, self.Enemy.Size)
	v = shared.VectorSubtract(v, point)
	shared.VectorNormalize(&v)
	self.Enemy.Velocity = shared.VectorMA(self.Enemy.Velocity, float32(kick), v)
	if self.Enemy.Velocity[2] > 0 {
		self.Enemy.Groundentity = nil
	}
	return true
}

// fire_lead is an internal support routine used for bullet/pellet based
// weapons. aimdir is a pointer: C's T_Damage normalizes it in place, which
// fire_shotgun's following pellets observe.
// C: game/g_weapon.c:134 fire_lead
func (g *Game) fire_lead(self *Edict, start Vec3, aimdir *Vec3, damage, kick, te_impact, hspread, vspread, mod int32) {
	var tr Trace
	var dir Vec3
	var forward, right, up Vec3
	var end Vec3
	var r, u float32
	var water_start Vec3
	water := false
	content_mask := int32(MASK_SHOT | MASK_WATER)

	tr = g.gi.Trace(&self.S.Origin, nil, nil, &start, self, MASK_SHOT)
	if !(float64(tr.Fraction) < 1.0) {
		dir = vectoangles(*aimdir)
		shared.AngleVectors(dir, &forward, &right, &up)

		r = float32(g.crandom() * float64(hspread))
		u = float32(g.crandom() * float64(vspread))
		end = shared.VectorMA(start, 8192, forward)
		end = shared.VectorMA(end, r, right)
		end = shared.VectorMA(end, u, up)

		if g.gi.PointContents(&start)&MASK_WATER != 0 {
			water = true
			water_start = start
			content_mask &^= MASK_WATER
		}

		tr = g.gi.Trace(&start, nil, nil, &end, self, content_mask)

		// see if we hit water
		if tr.Contents&MASK_WATER != 0 {
			var color int

			water = true
			water_start = tr.EndPos

			if shared.VectorCompare(start, tr.EndPos) == 0 {
				if tr.Contents&CONTENTS_WATER != 0 {
					if weaponSurfName(tr.Surface) == "*brwater" {
						color = SPLASH_BROWN_WATER
					} else {
						color = SPLASH_BLUE_WATER
					}
				} else if tr.Contents&CONTENTS_SLIME != 0 {
					color = SPLASH_SLIME
				} else if tr.Contents&CONTENTS_LAVA != 0 {
					color = SPLASH_LAVA
				} else {
					color = SPLASH_UNKNOWN
				}

				if color != SPLASH_UNKNOWN {
					g.gi.WriteByteC(svc_temp_entity)
					g.gi.WriteByteC(TE_SPLASH)
					g.gi.WriteByteC(8)
					g.gi.WritePosition(&tr.EndPos)
					g.gi.WriteDir(&tr.Plane.Normal)
					g.gi.WriteByteC(color)
					g.gi.Multicast(&tr.EndPos, MULTICAST_PVS)
				}

				// change bullet's course when it enters water
				dir = shared.VectorSubtract(end, start)
				dir = vectoangles(dir)
				shared.AngleVectors(dir, &forward, &right, &up)
				r = float32(g.crandom() * float64(hspread) * 2)
				u = float32(g.crandom() * float64(vspread) * 2)
				end = shared.VectorMA(water_start, 8192, forward)
				end = shared.VectorMA(end, r, right)
				end = shared.VectorMA(end, u, up)
			}

			// re-trace ignoring water this time
			tr = g.gi.Trace(&water_start, nil, nil, &end, self, MASK_SHOT)
		}
	}

	// send gun puff / flash
	if !(tr.Surface != nil && tr.Surface.Flags&SURF_SKY != 0) {
		if float64(tr.Fraction) < 1.0 {
			if tr.Ent.Takedamage != 0 {
				g.T_Damage(tr.Ent, self, self, aimdir, tr.EndPos, tr.Plane.Normal, damage, kick, DAMAGE_BULLET, mod)
			} else {
				if !strings.HasPrefix(weaponSurfName(tr.Surface), "sky") {
					g.gi.WriteByteC(svc_temp_entity)
					g.gi.WriteByteC(int(te_impact))
					g.gi.WritePosition(&tr.EndPos)
					g.gi.WriteDir(&tr.Plane.Normal)
					g.gi.Multicast(&tr.EndPos, MULTICAST_PVS)

					if self.Client != nil {
						g.PlayerNoise(self, tr.EndPos, PNOISE_IMPACT)
					}
				}
			}
		}
	}

	// if went through water, determine where the end and make a bubble trail
	if water {
		dir = shared.VectorSubtract(tr.EndPos, water_start)
		shared.VectorNormalize(&dir)
		pos := shared.VectorMA(tr.EndPos, -2, dir)
		if g.gi.PointContents(&pos)&MASK_WATER != 0 {
			tr.EndPos = pos
		} else {
			tr = g.gi.Trace(&pos, nil, nil, &water_start, tr.Ent, MASK_WATER)
		}

		pos = shared.VectorAdd(water_start, tr.EndPos)
		pos = shared.VectorScale(pos, 0.5)

		g.gi.WriteByteC(svc_temp_entity)
		g.gi.WriteByteC(TE_BUBBLETRAIL)
		g.gi.WritePosition(&water_start)
		g.gi.WritePosition(&tr.EndPos)
		g.gi.Multicast(&pos, MULTICAST_PVS)
	}
}

// fire_bullet fires a single round.  Used for machinegun and chaingun.
// C: game/g_weapon.c:277 fire_bullet
func (g *Game) fire_bullet(self *Edict, start, aimdir Vec3, damage, kick, hspread, vspread, mod int32) {
	g.fire_lead(self, start, &aimdir, damage, kick, TE_GUNSHOT, hspread, vspread, mod)
}

// fire_shotgun shoots shotgun pellets.  Used by shotgun and super shotgun.
// C: game/g_weapon.c:290 fire_shotgun
func (g *Game) fire_shotgun(self *Edict, start, aimdir Vec3, damage, kick, hspread, vspread, count, mod int32) {
	for i := int32(0); i < count; i++ {
		g.fire_lead(self, start, &aimdir, damage, kick, TE_SHOTGUN, hspread, vspread, mod)
	}
}

// C: game/g_weapon.c:306 blaster_touch
func (g *Game) blaster_touch(self, other *Edict, plane *CPlane, surf *CSurface) {
	var mod int32

	if other == self.Owner {
		return
	}

	if surf != nil && surf.Flags&SURF_SKY != 0 {
		g.G_FreeEdict(self)
		return
	}

	if self.Owner.Client != nil {
		g.PlayerNoise(self.Owner, self.S.Origin, PNOISE_IMPACT)
	}

	if other.Takedamage != 0 {
		if self.Spawnflags&1 != 0 {
			mod = MOD_HYPERBLASTER
		} else {
			mod = MOD_BLASTER
		}
		g.T_Damage(other, self, self.Owner, &self.Velocity, self.S.Origin, weaponPlaneNormal(plane), self.Dmg, 1, DAMAGE_ENERGY, mod)
	} else {
		g.gi.WriteByteC(svc_temp_entity)
		g.gi.WriteByteC(TE_BLASTER)
		g.gi.WritePosition(&self.S.Origin)
		if plane == nil {
			g.gi.WriteDir(&shared.Vec3Origin)
		} else {
			g.gi.WriteDir(&plane.Normal)
		}
		g.gi.Multicast(&self.S.Origin, MULTICAST_PVS)
	}

	g.G_FreeEdict(self)
}

// fire_blaster fires a single blaster bolt.  Used by the blaster and hyper blaster.
// C: game/g_weapon.c:345 fire_blaster
func (g *Game) fire_blaster(self *Edict, start, dir Vec3, damage, speed, effect int32, hyper bool) {
	shared.VectorNormalize(&dir)

	bolt := g.G_Spawn()
	if g.ctfmod {
		bolt.SVFlags = SVF_PROJECTILE // special net code is used for projectiles
	} else {
		bolt.SVFlags = SVF_DEADMONSTER
	}
	// yes, I know it looks weird that projectiles are deadmonsters
	// what this means is that when prediction is used against the object
	// (blaster/hyperblaster shots), the player won't be solid clipped against
	// the object.  Right now trying to run into a firing hyperblaster
	// is very jerky since you are predicted 'against' the shots.
	bolt.S.Origin = start
	bolt.S.OldOrigin = start
	bolt.S.Angles = vectoangles(dir)
	bolt.Velocity = shared.VectorScale(dir, float32(speed))
	bolt.Movetype = MOVETYPE_FLYMISSILE
	bolt.ClipMask = MASK_SHOT
	bolt.Solid = SOLID_BBOX
	bolt.S.Effects |= uint32(effect)
	bolt.Mins = Vec3{}
	bolt.Maxs = Vec3{}
	bolt.S.ModelIndex = int32(g.gi.ModelIndex("models/objects/laser/tris.md2"))
	bolt.S.Sound = int32(g.gi.SoundIndex("misc/lasfly.wav"))
	bolt.Owner = self
	bolt.Touch = blaster_touch
	bolt.Nextthink = g.level.Time + 2
	bolt.Think = G_FreeEdict
	bolt.Dmg = damage
	bolt.Classname = "bolt"
	if hyper {
		bolt.Spawnflags = 1
	}
	g.gi.LinkEntity(bolt)

	if self.Client != nil {
		g.check_dodge(self, bolt.S.Origin, dir, speed)
	}

	tr := g.gi.Trace(&self.S.Origin, nil, nil, &bolt.S.Origin, bolt, MASK_SHOT)
	if float64(tr.Fraction) < 1.0 {
		bolt.S.Origin = shared.VectorMA(bolt.S.Origin, -10, dir)
		bolt.Touch.fn(g, bolt, tr.Ent, nil, nil)
	}
}

// C: game/g_weapon.c:398 Grenade_Explode
func (g *Game) Grenade_Explode(ent *Edict) {
	var mod int32

	if ent.Owner.Client != nil {
		g.PlayerNoise(ent.Owner, ent.S.Origin, PNOISE_IMPACT)
	}

	//FIXME: if we are onground then raise our Z just a bit since we are a point?
	if ent.Enemy != nil {
		v := shared.VectorAdd(ent.Enemy.Mins, ent.Enemy.Maxs)
		v = shared.VectorMA(ent.Enemy.S.Origin, 0.5, v)
		v = shared.VectorSubtract(ent.S.Origin, v)
		points := float32(float64(ent.Dmg) - 0.5*float64(shared.VectorLength(v)))
		dir := shared.VectorSubtract(ent.Enemy.S.Origin, ent.S.Origin)
		if ent.Spawnflags&1 != 0 {
			mod = MOD_HANDGRENADE
		} else {
			mod = MOD_GRENADE
		}
		g.T_Damage(ent.Enemy, ent, ent.Owner, &dir, ent.S.Origin, shared.Vec3Origin, int32(points), int32(points), DAMAGE_RADIUS, mod)
	}

	if ent.Spawnflags&2 != 0 {
		mod = MOD_HELD_GRENADE
	} else if ent.Spawnflags&1 != 0 {
		mod = MOD_HG_SPLASH
	} else {
		mod = MOD_G_SPLASH
	}
	g.T_RadiusDamage(ent, ent.Owner, float32(ent.Dmg), ent.Enemy, ent.DmgRadius, mod)

	origin := shared.VectorMA(ent.S.Origin, float32(-0.02), ent.Velocity)
	g.gi.WriteByteC(svc_temp_entity)
	if ent.Waterlevel != 0 {
		if ent.Groundentity != nil {
			g.gi.WriteByteC(TE_GRENADE_EXPLOSION_WATER)
		} else {
			g.gi.WriteByteC(TE_ROCKET_EXPLOSION_WATER)
		}
	} else {
		if ent.Groundentity != nil {
			g.gi.WriteByteC(TE_GRENADE_EXPLOSION)
		} else {
			g.gi.WriteByteC(TE_ROCKET_EXPLOSION)
		}
	}
	g.gi.WritePosition(&origin)
	g.gi.Multicast(&ent.S.Origin, MULTICAST_PHS)

	g.G_FreeEdict(ent)
}

// C: game/g_weapon.c:455 Grenade_Touch
func (g *Game) Grenade_Touch(ent, other *Edict, plane *CPlane, surf *CSurface) {
	if other == ent.Owner {
		return
	}

	if surf != nil && surf.Flags&SURF_SKY != 0 {
		g.G_FreeEdict(ent)
		return
	}

	if other.Takedamage == 0 {
		if ent.Spawnflags&1 != 0 {
			if float64(g.random()) > 0.5 {
				g.gi.Sound(ent, CHAN_VOICE, g.gi.SoundIndex("weapons/hgrenb1a.wav"), 1, ATTN_NORM, 0)
			} else {
				g.gi.Sound(ent, CHAN_VOICE, g.gi.SoundIndex("weapons/hgrenb2a.wav"), 1, ATTN_NORM, 0)
			}
		} else {
			g.gi.Sound(ent, CHAN_VOICE, g.gi.SoundIndex("weapons/grenlb1b.wav"), 1, ATTN_NORM, 0)
		}
		return
	}

	ent.Enemy = other
	g.Grenade_Explode(ent)
}

// C: game/g_weapon.c:486 fire_grenade
func (g *Game) fire_grenade(self *Edict, start, aimdir Vec3, damage, speed int32, timer, damage_radius float32) {
	var forward, right, up Vec3

	dir := vectoangles(aimdir)
	shared.AngleVectors(dir, &forward, &right, &up)

	grenade := g.G_Spawn()
	grenade.S.Origin = start
	grenade.Velocity = shared.VectorScale(aimdir, float32(speed))
	grenade.Velocity = shared.VectorMA(grenade.Velocity, float32(200+g.crandom()*10.0), up)
	grenade.Velocity = shared.VectorMA(grenade.Velocity, float32(g.crandom()*10.0), right)
	grenade.Avelocity = Vec3{300, 300, 300}
	grenade.Movetype = MOVETYPE_BOUNCE
	grenade.ClipMask = MASK_SHOT
	grenade.Solid = SOLID_BBOX
	grenade.S.Effects |= EF_GRENADE
	grenade.Mins = Vec3{}
	grenade.Maxs = Vec3{}
	grenade.S.ModelIndex = int32(g.gi.ModelIndex("models/objects/grenade/tris.md2"))
	grenade.Owner = self
	grenade.Touch = Grenade_Touch
	grenade.Nextthink = g.level.Time + timer
	grenade.Think = Grenade_Explode
	grenade.Dmg = damage
	grenade.DmgRadius = damage_radius
	grenade.Classname = "grenade"

	g.gi.LinkEntity(grenade)
}

// C: game/g_weapon.c:519 fire_grenade2
func (g *Game) fire_grenade2(self *Edict, start, aimdir Vec3, damage, speed int32, timer, damage_radius float32, held bool) {
	var forward, right, up Vec3

	dir := vectoangles(aimdir)
	shared.AngleVectors(dir, &forward, &right, &up)

	grenade := g.G_Spawn()
	grenade.S.Origin = start
	grenade.Velocity = shared.VectorScale(aimdir, float32(speed))
	grenade.Velocity = shared.VectorMA(grenade.Velocity, float32(200+g.crandom()*10.0), up)
	grenade.Velocity = shared.VectorMA(grenade.Velocity, float32(g.crandom()*10.0), right)
	grenade.Avelocity = Vec3{300, 300, 300}
	grenade.Movetype = MOVETYPE_BOUNCE
	grenade.ClipMask = MASK_SHOT
	grenade.Solid = SOLID_BBOX
	grenade.S.Effects |= EF_GRENADE
	grenade.Mins = Vec3{}
	grenade.Maxs = Vec3{}
	grenade.S.ModelIndex = int32(g.gi.ModelIndex("models/objects/grenade2/tris.md2"))
	grenade.Owner = self
	grenade.Touch = Grenade_Touch
	grenade.Nextthink = g.level.Time + timer
	grenade.Think = Grenade_Explode
	grenade.Dmg = damage
	grenade.DmgRadius = damage_radius
	grenade.Classname = "hgrenade"
	if held {
		grenade.Spawnflags = 3
	} else {
		grenade.Spawnflags = 1
	}
	grenade.S.Sound = int32(g.gi.SoundIndex("weapons/hgrenc1b.wav"))

	if float64(timer) <= 0.0 {
		g.Grenade_Explode(grenade)
	} else {
		g.gi.Sound(self, CHAN_WEAPON, g.gi.SoundIndex("weapons/hgrent1a.wav"), 1, ATTN_NORM, 0)
		g.gi.LinkEntity(grenade)
	}
}

// C: game/g_weapon.c:569 rocket_touch
func (g *Game) rocket_touch(ent, other *Edict, plane *CPlane, surf *CSurface) {
	if other == ent.Owner {
		return
	}

	if surf != nil && surf.Flags&SURF_SKY != 0 {
		g.G_FreeEdict(ent)
		return
	}

	if ent.Owner.Client != nil {
		g.PlayerNoise(ent.Owner, ent.S.Origin, PNOISE_IMPACT)
	}

	// calculate position for the explosion entity
	origin := shared.VectorMA(ent.S.Origin, float32(-0.02), ent.Velocity)

	if other.Takedamage != 0 {
		g.T_Damage(other, ent, ent.Owner, &ent.Velocity, ent.S.Origin, weaponPlaneNormal(plane), ent.Dmg, 0, 0, MOD_ROCKET)
	} else {
		// don't throw any debris in net games
		if g.deathmatch.Value == 0 && g.coop.Value == 0 {
			if surf != nil && surf.Flags&(SURF_WARP|SURF_TRANS33|SURF_TRANS66|SURF_FLOWING) == 0 {
				n := g.rng.Rand() % 5
				for ; n > 0; n-- {
					g.ThrowDebris(ent, "models/objects/debris2/tris.md2", 2, ent.S.Origin)
				}
			}
		}
	}

	g.T_RadiusDamage(ent, ent.Owner, float32(ent.RadiusDmg), other, ent.DmgRadius, MOD_R_SPLASH)

	g.gi.WriteByteC(svc_temp_entity)
	if ent.Waterlevel != 0 {
		g.gi.WriteByteC(TE_ROCKET_EXPLOSION_WATER)
	} else {
		g.gi.WriteByteC(TE_ROCKET_EXPLOSION)
	}
	g.gi.WritePosition(&origin)
	g.gi.Multicast(&ent.S.Origin, MULTICAST_PHS)

	g.G_FreeEdict(ent)
}

// C: game/g_weapon.c:620 fire_rocket
func (g *Game) fire_rocket(self *Edict, start, dir Vec3, damage, speed int32, damage_radius float32, radius_damage int32) {
	rocket := g.G_Spawn()
	rocket.S.Origin = start
	rocket.Movedir = dir
	rocket.S.Angles = vectoangles(dir)
	rocket.Velocity = shared.VectorScale(dir, float32(speed))
	rocket.Movetype = MOVETYPE_FLYMISSILE
	rocket.ClipMask = MASK_SHOT
	rocket.Solid = SOLID_BBOX
	rocket.S.Effects |= EF_ROCKET
	rocket.Mins = Vec3{}
	rocket.Maxs = Vec3{}
	rocket.S.ModelIndex = int32(g.gi.ModelIndex("models/objects/rocket/tris.md2"))
	rocket.Owner = self
	rocket.Touch = rocket_touch
	rocket.Nextthink = g.level.Time + float32(8000/speed)
	rocket.Think = G_FreeEdict
	rocket.Dmg = damage
	rocket.RadiusDmg = radius_damage
	rocket.DmgRadius = damage_radius
	rocket.S.Sound = int32(g.gi.SoundIndex("weapons/rockfly.wav"))
	rocket.Classname = "rocket"

	if self.Client != nil {
		g.check_dodge(self, rocket.S.Origin, dir, speed)
	}

	g.gi.LinkEntity(rocket)
}

// C: game/g_weapon.c:658 fire_rail
func (g *Game) fire_rail(self *Edict, start, aimdir Vec3, damage, kick int32) {
	var tr Trace

	end := shared.VectorMA(start, 8192, aimdir)
	from := start
	ignore := self
	water := false
	mask := int32(MASK_SHOT | CONTENTS_SLIME | CONTENTS_LAVA)
	for ignore != nil {
		tr = g.gi.Trace(&from, nil, nil, &end, ignore, mask)

		if tr.Contents&(CONTENTS_SLIME|CONTENTS_LAVA) != 0 {
			mask &^= CONTENTS_SLIME | CONTENTS_LAVA
			water = true
		} else {
			if tr.Ent.SVFlags&SVF_MONSTER != 0 || tr.Ent.Client != nil {
				ignore = tr.Ent
			} else {
				ignore = nil
			}

			if tr.Ent != self && tr.Ent.Takedamage != 0 {
				g.T_Damage(tr.Ent, self, self, &aimdir, tr.EndPos, tr.Plane.Normal, damage, kick, 0, MOD_RAILGUN)
			}
		}

		from = tr.EndPos
	}

	// send gun puff / flash
	g.gi.WriteByteC(svc_temp_entity)
	g.gi.WriteByteC(TE_RAILTRAIL)
	g.gi.WritePosition(&start)
	g.gi.WritePosition(&tr.EndPos)
	g.gi.Multicast(&self.S.Origin, MULTICAST_PHS)
	//	gi.multicast (start, MULTICAST_PHS);
	if water {
		g.gi.WriteByteC(svc_temp_entity)
		g.gi.WriteByteC(TE_RAILTRAIL)
		g.gi.WritePosition(&start)
		g.gi.WritePosition(&tr.EndPos)
		g.gi.Multicast(&tr.EndPos, MULTICAST_PHS)
	}

	if self.Client != nil {
		g.PlayerNoise(self, tr.EndPos, PNOISE_IMPACT)
	}
}

// C: game/g_weapon.c:721 bfg_explode
func (g *Game) bfg_explode(self *Edict) {
	if self.S.Frame == 0 {
		// the BFG effect
		var ent *Edict
		for {
			ent = g.findradius(ent, self.S.Origin, self.DmgRadius)
			if ent == nil {
				break
			}
			if ent.Takedamage == 0 {
				continue
			}
			if ent == self.Owner {
				continue
			}
			if !g.CanDamage(ent, self) {
				continue
			}
			if !g.CanDamage(ent, self.Owner) {
				continue
			}

			v := shared.VectorAdd(ent.Mins, ent.Maxs)
			v = shared.VectorMA(ent.S.Origin, 0.5, v)
			v = shared.VectorSubtract(self.S.Origin, v)
			dist := shared.VectorLength(v)
			points := float32(float64(self.RadiusDmg) * (1.0 - math.Sqrt(float64(dist/self.DmgRadius))))
			if ent == self.Owner {
				points = float32(float64(points) * 0.5)
			}

			g.gi.WriteByteC(svc_temp_entity)
			g.gi.WriteByteC(TE_BFG_EXPLOSION)
			g.gi.WritePosition(&ent.S.Origin)
			g.gi.Multicast(&ent.S.Origin, MULTICAST_PHS)
			g.T_Damage(ent, self, self.Owner, &self.Velocity, ent.S.Origin, shared.Vec3Origin, int32(points), 0, DAMAGE_ENERGY, MOD_BFG_EFFECT)
		}
	}

	self.Nextthink = float32(float64(g.level.Time) + FRAMETIME)
	self.S.Frame++
	if self.S.Frame == 5 {
		self.Think = G_FreeEdict
	}
}

// C: game/g_weapon.c:765 bfg_touch
func (g *Game) bfg_touch(self, other *Edict, plane *CPlane, surf *CSurface) {
	if other == self.Owner {
		return
	}

	if surf != nil && surf.Flags&SURF_SKY != 0 {
		g.G_FreeEdict(self)
		return
	}

	if self.Owner.Client != nil {
		g.PlayerNoise(self.Owner, self.S.Origin, PNOISE_IMPACT)
	}

	// core explosion - prevents firing it into the wall/floor
	if other.Takedamage != 0 {
		g.T_Damage(other, self, self.Owner, &self.Velocity, self.S.Origin, weaponPlaneNormal(plane), 200, 0, 0, MOD_BFG_BLAST)
	}
	g.T_RadiusDamage(self, self.Owner, 200, other, 100, MOD_BFG_BLAST)

	g.gi.Sound(self, CHAN_VOICE, g.gi.SoundIndex("weapons/bfg__x1b.wav"), 1, ATTN_NORM, 0)
	self.Solid = SOLID_NOT
	self.Touch = nil
	self.S.Origin = shared.VectorMA(self.S.Origin, float32(-1*FRAMETIME), self.Velocity)
	self.Velocity = Vec3{}
	self.S.ModelIndex = int32(g.gi.ModelIndex("sprites/s_bfg3.sp2"))
	self.S.Frame = 0
	self.S.Sound = 0
	self.S.Effects &^= EF_ANIM_ALLFAST
	self.Think = bfg_explode
	self.Nextthink = float32(float64(g.level.Time) + FRAMETIME)
	self.Enemy = other

	g.gi.WriteByteC(svc_temp_entity)
	g.gi.WriteByteC(TE_BFG_BIGEXPLOSION)
	g.gi.WritePosition(&self.S.Origin)
	g.gi.Multicast(&self.S.Origin, MULTICAST_PVS)
}

// C: game/g_weapon.c:804 bfg_think
func (g *Game) bfg_think(self *Edict) {
	var dmg int32
	var tr Trace

	if g.deathmatch.Value != 0 {
		dmg = 5
	} else {
		dmg = 10
	}

	var ent *Edict
	for {
		ent = g.findradius(ent, self.S.Origin, 256)
		if ent == nil {
			break
		}
		if ent == self {
			continue
		}

		if ent == self.Owner {
			continue
		}

		if ent.Takedamage == 0 {
			continue
		}

		if ent.SVFlags&SVF_MONSTER == 0 && ent.Client == nil && ent.Classname != "misc_explobox" {
			continue
		}

		//ZOID
		//don't target players in CTF
		if g.ctfOn() && ent.Client != nil &&
			self.Owner.Client != nil &&
			ent.Client.Resp.CtfTeam == self.Owner.Client.Resp.CtfTeam {
			continue
		}
		//ZOID

		point := shared.VectorMA(ent.AbsMin, 0.5, ent.Size)

		dir := shared.VectorSubtract(point, self.S.Origin)
		shared.VectorNormalize(&dir)

		ignore := self
		start := self.S.Origin
		end := shared.VectorMA(start, 2048, dir)
		for {
			tr = g.gi.Trace(&start, nil, nil, &end, ignore, CONTENTS_SOLID|CONTENTS_MONSTER|CONTENTS_DEADMONSTER)

			if tr.Ent == nil {
				break
			}

			// hurt it if we can
			if tr.Ent.Takedamage != 0 && tr.Ent.Flags&FL_IMMUNE_LASER == 0 && tr.Ent != self.Owner {
				g.T_Damage(tr.Ent, self, self.Owner, &dir, tr.EndPos, shared.Vec3Origin, dmg, 1, DAMAGE_ENERGY, MOD_BFG_LASER)
			}

			// if we hit something that's not a monster or player we're done
			if tr.Ent.SVFlags&SVF_MONSTER == 0 && tr.Ent.Client == nil {
				g.gi.WriteByteC(svc_temp_entity)
				g.gi.WriteByteC(TE_LASER_SPARKS)
				g.gi.WriteByteC(4)
				g.gi.WritePosition(&tr.EndPos)
				g.gi.WriteDir(&tr.Plane.Normal)
				g.gi.WriteByteC(int(self.S.SkinNum))
				g.gi.Multicast(&tr.EndPos, MULTICAST_PVS)
				break
			}

			ignore = tr.Ent
			start = tr.EndPos
		}

		g.gi.WriteByteC(svc_temp_entity)
		g.gi.WriteByteC(TE_BFG_LASER)
		g.gi.WritePosition(&self.S.Origin)
		g.gi.WritePosition(&tr.EndPos)
		g.gi.Multicast(&self.S.Origin, MULTICAST_PHS)
	}

	self.Nextthink = float32(float64(g.level.Time) + FRAMETIME)
}

// C: game/g_weapon.c:882 fire_bfg
func (g *Game) fire_bfg(self *Edict, start, dir Vec3, damage, speed int32, damage_radius float32) {
	bfg := g.G_Spawn()
	bfg.S.Origin = start
	bfg.Movedir = dir
	bfg.S.Angles = vectoangles(dir)
	bfg.Velocity = shared.VectorScale(dir, float32(speed))
	bfg.Movetype = MOVETYPE_FLYMISSILE
	bfg.ClipMask = MASK_SHOT
	bfg.Solid = SOLID_BBOX
	bfg.S.Effects |= EF_BFG | EF_ANIM_ALLFAST
	bfg.Mins = Vec3{}
	bfg.Maxs = Vec3{}
	bfg.S.ModelIndex = int32(g.gi.ModelIndex("sprites/s_bfg1.sp2"))
	bfg.Owner = self
	bfg.Touch = bfg_touch
	bfg.Nextthink = g.level.Time + float32(8000/speed)
	bfg.Think = G_FreeEdict
	bfg.RadiusDmg = damage
	bfg.DmgRadius = damage_radius
	bfg.Classname = "bfg blast"
	bfg.S.Sound = int32(g.gi.SoundIndex("weapons/bfg__l1a.wav"))

	bfg.Think = bfg_think
	bfg.Nextthink = float32(float64(g.level.Time) + FRAMETIME)
	bfg.Teammaster = bfg
	bfg.Teamchain = nil

	if self.Client != nil {
		g.check_dodge(self, bfg.S.Origin, dir, speed)
	}

	g.gi.LinkEntity(bfg)
}
