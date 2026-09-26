package game

// Port of game/g_misc.c.

import (
	"fmt"
	"time"

	. "quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/shared"
)

var (
	// G_FreeEdict is used as a think function all over the game (gibs,
	// debris, projectiles); its method lives in g_utils.go.
	G_FreeEdict = defThink("G_FreeEdict")

	Use_Areaportal            = defUse("Use_Areaportal")
	gib_think                 = defThink("gib_think")
	gib_touch                 = defTouch("gib_touch")
	gib_die                   = defDie("gib_die")
	debris_die                = defDie("debris_die")
	path_corner_touch         = defTouch("path_corner_touch")
	point_combat_touch        = defTouch("point_combat_touch")
	TH_viewthing              = defThink("TH_viewthing")
	light_use                 = defUse("light_use")
	func_wall_use             = defUse("func_wall_use")
	func_object_touch         = defTouch("func_object_touch")
	func_object_release       = defThink("func_object_release")
	func_object_use           = defUse("func_object_use")
	func_explosive_explode    = defDie("func_explosive_explode")
	func_explosive_use        = defUse("func_explosive_use")
	func_explosive_spawn      = defUse("func_explosive_spawn")
	barrel_touch              = defTouch("barrel_touch")
	barrel_explode            = defThink("barrel_explode")
	barrel_delay              = defDie("barrel_delay")
	misc_blackhole_use        = defUse("misc_blackhole_use")
	misc_blackhole_think      = defThink("misc_blackhole_think")
	misc_eastertank_think     = defThink("misc_eastertank_think")
	misc_easterchick_think    = defThink("misc_easterchick_think")
	misc_easterchick2_think   = defThink("misc_easterchick2_think")
	commander_body_think      = defThink("commander_body_think")
	commander_body_use        = defUse("commander_body_use")
	commander_body_drop       = defThink("commander_body_drop")
	misc_banner_think         = defThink("misc_banner_think")
	misc_deadsoldier_die      = defDie("misc_deadsoldier_die")
	misc_viper_use            = defUse("misc_viper_use")
	misc_viper_bomb_touch     = defTouch("misc_viper_bomb_touch")
	misc_viper_bomb_prethink  = defThink("misc_viper_bomb_prethink")
	misc_viper_bomb_use       = defUse("misc_viper_bomb_use")
	misc_strogg_ship_use      = defUse("misc_strogg_ship_use")
	misc_satellite_dish_think = defThink("misc_satellite_dish_think")
	misc_satellite_dish_use   = defUse("misc_satellite_dish_use")
	target_string_use         = defUse("target_string_use")
	func_clock_think          = defThink("func_clock_think")
	func_clock_use            = defUse("func_clock_use")
	teleporter_touch          = defTouch("teleporter_touch")
)

func init() {
	G_FreeEdict.bind((*Game).G_FreeEdict)

	Use_Areaportal.bind((*Game).Use_Areaportal)
	gib_think.bind((*Game).gib_think)
	gib_touch.bind((*Game).gib_touch)
	gib_die.bind((*Game).gib_die)
	debris_die.bind((*Game).debris_die)
	path_corner_touch.bind((*Game).path_corner_touch)
	point_combat_touch.bind((*Game).point_combat_touch)
	TH_viewthing.bind((*Game).TH_viewthing)
	light_use.bind((*Game).light_use)
	func_wall_use.bind((*Game).func_wall_use)
	func_object_touch.bind((*Game).func_object_touch)
	func_object_release.bind((*Game).func_object_release)
	func_object_use.bind((*Game).func_object_use)
	func_explosive_explode.bind((*Game).func_explosive_explode)
	func_explosive_use.bind((*Game).func_explosive_use)
	func_explosive_spawn.bind((*Game).func_explosive_spawn)
	barrel_touch.bind((*Game).barrel_touch)
	barrel_explode.bind((*Game).barrel_explode)
	barrel_delay.bind((*Game).barrel_delay)
	misc_blackhole_use.bind((*Game).misc_blackhole_use)
	misc_blackhole_think.bind((*Game).misc_blackhole_think)
	misc_eastertank_think.bind((*Game).misc_eastertank_think)
	misc_easterchick_think.bind((*Game).misc_easterchick_think)
	misc_easterchick2_think.bind((*Game).misc_easterchick2_think)
	commander_body_think.bind((*Game).commander_body_think)
	commander_body_use.bind((*Game).commander_body_use)
	commander_body_drop.bind((*Game).commander_body_drop)
	misc_banner_think.bind((*Game).misc_banner_think)
	misc_deadsoldier_die.bind((*Game).misc_deadsoldier_die)
	misc_viper_use.bind((*Game).misc_viper_use)
	misc_viper_bomb_touch.bind((*Game).misc_viper_bomb_touch)
	misc_viper_bomb_prethink.bind((*Game).misc_viper_bomb_prethink)
	misc_viper_bomb_use.bind((*Game).misc_viper_bomb_use)
	misc_strogg_ship_use.bind((*Game).misc_strogg_ship_use)
	misc_satellite_dish_think.bind((*Game).misc_satellite_dish_think)
	misc_satellite_dish_use.bind((*Game).misc_satellite_dish_use)
	target_string_use.bind((*Game).target_string_use)
	func_clock_think.bind((*Game).func_clock_think)
	func_clock_use.bind((*Game).func_clock_use)
	teleporter_touch.bind((*Game).teleporter_touch)
}

/*QUAKED func_group (0 0 0) ?
Used to group brushes together just for editor convenience.
*/

//=====================================================

// C: game/g_misc.c:31 Use_Areaportal
func (g *Game) Use_Areaportal(ent, other, activator *Edict) {
	ent.Count ^= 1 // toggle state
	//	gi.dprintf ("portalstate: %i = %i\n", ent->style, ent->count);
	g.gi.SetAreaPortalState(int(ent.Style), ent.Count != 0)
}

/*QUAKED func_areaportal (0 0 0) ?

This is a non-visible object that divides the world into
areas that are seperated when this portal is not activated.
Usually enclosed in the middle of a door.
*/
// C: game/g_misc.c:44 SP_func_areaportal
func (g *Game) SP_func_areaportal(ent *Edict) {
	ent.Use = Use_Areaportal
	ent.Count = 0 // always start closed;
}

//=====================================================

/*
=================
Misc functions
=================
*/

// C: game/g_misc.c:58 VelocityForDamage
func (g *Game) VelocityForDamage(damage int32) Vec3 {
	var v Vec3
	v[0] = float32(100.0 * g.crandom())
	v[1] = float32(100.0 * g.crandom())
	v[2] = float32(200.0 + 100.0*float64(g.random()))

	if damage < 50 {
		v = shared.VectorScale(v, 0.7)
	} else {
		v = shared.VectorScale(v, 1.2)
	}
	return v
}

// C: game/g_misc.c:70 ClipGibVelocity
func (g *Game) ClipGibVelocity(ent *Edict) {
	if ent.Velocity[0] < -300 {
		ent.Velocity[0] = -300
	} else if ent.Velocity[0] > 300 {
		ent.Velocity[0] = 300
	}
	if ent.Velocity[1] < -300 {
		ent.Velocity[1] = -300
	} else if ent.Velocity[1] > 300 {
		ent.Velocity[1] = 300
	}
	if ent.Velocity[2] < 200 {
		ent.Velocity[2] = 200 // always some upwards
	} else if ent.Velocity[2] > 500 {
		ent.Velocity[2] = 500
	}
}

/*
=================
gibs
=================
*/

// C: game/g_misc.c:92 gib_think
func (g *Game) gib_think(self *Edict) {
	self.S.Frame++
	self.Nextthink = float32(float64(g.level.Time) + FRAMETIME)

	if self.S.Frame == 10 {
		self.Think = G_FreeEdict
		self.Nextthink = g.level.Time + 8 + g.random()*10
	}
}

// C: game/g_misc.c:104 gib_touch
func (g *Game) gib_touch(self, other *Edict, plane *CPlane, surf *CSurface) {
	var right Vec3

	if self.Groundentity == nil {
		return
	}

	self.Touch = nil

	if plane != nil {
		g.gi.Sound(self, CHAN_VOICE, g.gi.SoundIndex("misc/fhit3.wav"), 1, ATTN_NORM, 0)

		normalAngles := vectoangles(plane.Normal)
		shared.AngleVectors(normalAngles, nil, &right, nil)
		self.S.Angles = vectoangles(right)

		if self.S.ModelIndex == g.sm_meat_index {
			self.S.Frame++
			self.Think = gib_think
			self.Nextthink = float32(float64(g.level.Time) + FRAMETIME)
		}
	}
}

// C: game/g_misc.c:130 gib_die
func (g *Game) gib_die(self, inflictor, attacker *Edict, damage int32, point Vec3) {
	g.G_FreeEdict(self)
}

// C: game/g_misc.c:135 ThrowGib
func (g *Game) ThrowGib(self *Edict, gibname string, damage, type_ int32) {
	var vscale float32

	gib := g.G_Spawn()

	size := shared.VectorScale(self.Size, 0.5)
	origin := shared.VectorAdd(self.AbsMin, size)
	gib.S.Origin[0] = float32(float64(origin[0]) + g.crandom()*float64(size[0]))
	gib.S.Origin[1] = float32(float64(origin[1]) + g.crandom()*float64(size[1]))
	gib.S.Origin[2] = float32(float64(origin[2]) + g.crandom()*float64(size[2]))

	g.gi.SetModel(gib, gibname)
	gib.Solid = SOLID_NOT
	gib.S.Effects |= EF_GIB
	gib.Flags |= FL_NO_KNOCKBACK
	gib.Takedamage = DAMAGE_YES
	gib.Die = gib_die

	if type_ == GIB_ORGANIC {
		gib.Movetype = MOVETYPE_TOSS
		gib.Touch = gib_touch
		vscale = 0.5
	} else {
		gib.Movetype = MOVETYPE_BOUNCE
		vscale = 1.0
	}

	vd := g.VelocityForDamage(damage)
	gib.Velocity = shared.VectorMA(self.Velocity, vscale, vd)
	g.ClipGibVelocity(gib)
	gib.Avelocity[0] = g.random() * 600
	gib.Avelocity[1] = g.random() * 600
	gib.Avelocity[2] = g.random() * 600

	gib.Think = G_FreeEdict
	gib.Nextthink = g.level.Time + 10 + g.random()*10

	g.gi.LinkEntity(gib)
}

// C: game/g_misc.c:183 ThrowHead
func (g *Game) ThrowHead(self *Edict, gibname string, damage, type_ int32) {
	var vscale float32

	self.S.SkinNum = 0
	self.S.Frame = 0
	self.Mins = Vec3{}
	self.Maxs = Vec3{}

	self.S.ModelIndex2 = 0
	g.gi.SetModel(self, gibname)
	self.Solid = SOLID_NOT
	self.S.Effects |= EF_GIB
	self.S.Effects &^= EF_FLIES
	self.S.Sound = 0
	self.Flags |= FL_NO_KNOCKBACK
	self.SVFlags &^= SVF_MONSTER
	self.Takedamage = DAMAGE_YES
	self.Die = gib_die

	if type_ == GIB_ORGANIC {
		self.Movetype = MOVETYPE_TOSS
		self.Touch = gib_touch
		vscale = 0.5
	} else {
		self.Movetype = MOVETYPE_BOUNCE
		vscale = 1.0
	}

	vd := g.VelocityForDamage(damage)
	self.Velocity = shared.VectorMA(self.Velocity, vscale, vd)
	g.ClipGibVelocity(self)

	self.Avelocity[YAW] = float32(g.crandom() * 600)

	self.Think = G_FreeEdict
	self.Nextthink = g.level.Time + 10 + g.random()*10

	g.gi.LinkEntity(self)
}

// C: game/g_misc.c:229 ThrowClientHead
func (g *Game) ThrowClientHead(self *Edict, damage int32) {
	var gibname string

	if g.rng.Rand()&1 != 0 {
		gibname = "models/objects/gibs/head2/tris.md2"
		self.S.SkinNum = 1 // second skin is player
	} else {
		gibname = "models/objects/gibs/skull/tris.md2"
		self.S.SkinNum = 0
	}

	self.S.Origin[2] += 32
	self.S.Frame = 0
	g.gi.SetModel(self, gibname)
	self.Mins = Vec3{-16, -16, 0}
	self.Maxs = Vec3{16, 16, 16}

	self.Takedamage = DAMAGE_NO
	self.Solid = SOLID_NOT
	self.S.Effects = EF_GIB
	self.S.Sound = 0
	self.Flags |= FL_NO_KNOCKBACK

	self.Movetype = MOVETYPE_BOUNCE
	vd := g.VelocityForDamage(damage)
	self.Velocity = shared.VectorAdd(self.Velocity, vd)

	if self.Client != nil { // bodies in the queue don't have a client anymore
		self.Client.AnimPriority = ANIM_DEATH
		self.Client.AnimEnd = self.S.Frame
	} else if !g.ctfmod { // not in the ctf fork's older base
		self.Think = nil
		self.Nextthink = 0
	}

	g.gi.LinkEntity(self)
}

/*
=================
debris
=================
*/

// C: game/g_misc.c:281 debris_die
func (g *Game) debris_die(self, inflictor, attacker *Edict, damage int32, point Vec3) {
	g.G_FreeEdict(self)
}

// C: game/g_misc.c:286 ThrowDebris
func (g *Game) ThrowDebris(self *Edict, modelname string, speed float32, origin Vec3) {
	var v Vec3

	chunk := g.G_Spawn()
	chunk.S.Origin = origin
	g.gi.SetModel(chunk, modelname)
	v[0] = float32(100 * g.crandom())
	v[1] = float32(100 * g.crandom())
	v[2] = float32(100 + 100*g.crandom())
	chunk.Velocity = shared.VectorMA(self.Velocity, speed, v)
	chunk.Movetype = MOVETYPE_BOUNCE
	chunk.Solid = SOLID_NOT
	chunk.Avelocity[0] = g.random() * 600
	chunk.Avelocity[1] = g.random() * 600
	chunk.Avelocity[2] = g.random() * 600
	chunk.Think = G_FreeEdict
	chunk.Nextthink = g.level.Time + 5 + g.random()*5
	chunk.S.Frame = 0
	chunk.Flags = 0
	chunk.Classname = "debris"
	chunk.Takedamage = DAMAGE_YES
	chunk.Die = debris_die
	g.gi.LinkEntity(chunk)
}

// C: game/g_misc.c:314 BecomeExplosion1
func (g *Game) BecomeExplosion1(self *Edict) {
	//ZOID
	if g.ctfmod {
		//flags are important
		if self.Classname == "item_flag_team1" {
			g.CTFResetFlag(CTF_TEAM1) // this will free self!
			g.bprintf(PRINT_HIGH, "The %s flag has returned!\n",
				CTFTeamName(CTF_TEAM1))
			return
		}
		if self.Classname == "item_flag_team2" {
			g.CTFResetFlag(CTF_TEAM2) // this will free self!
			g.bprintf(PRINT_HIGH, "The %s flag has returned!\n",
				CTFTeamName(CTF_TEAM1))
			return
		}
		// techs are important too
		if self.Item != nil && self.Item.Flags&IT_TECH != 0 {
			g.CTFRespawnTech(self) // this frees self!
			return
		}
	}
	//ZOID

	g.gi.WriteByteC(svc_temp_entity)
	g.gi.WriteByteC(TE_EXPLOSION1)
	g.gi.WritePosition(&self.S.Origin)
	g.gi.Multicast(&self.S.Origin, MULTICAST_PVS)

	g.G_FreeEdict(self)
}

// C: game/g_misc.c:325 BecomeExplosion2
func (g *Game) BecomeExplosion2(self *Edict) {
	g.gi.WriteByteC(svc_temp_entity)
	g.gi.WriteByteC(TE_EXPLOSION2)
	g.gi.WritePosition(&self.S.Origin)
	g.gi.Multicast(&self.S.Origin, MULTICAST_PVS)

	g.G_FreeEdict(self)
}

/*QUAKED path_corner (.5 .3 0) (-8 -8 -8) (8 8 8) TELEPORT
Target: next path corner
Pathtarget: gets used when an entity that has
	this path_corner targeted touches it
*/

// C: game/g_misc.c:342 path_corner_touch
func (g *Game) path_corner_touch(self, other *Edict, plane *CPlane, surf *CSurface) {
	var v Vec3
	var next *Edict

	if other.Movetarget != self {
		return
	}

	if other.Enemy != nil {
		return
	}

	if self.Pathtarget != "" {
		savetarget := self.Target
		self.Target = self.Pathtarget
		g.G_UseTargets(self, other)
		self.Target = savetarget
	}

	if self.Target != "" {
		next = g.G_PickTarget(self.Target)
	} else {
		next = nil
	}

	if next != nil && next.Spawnflags&1 != 0 {
		v = next.S.Origin
		v[2] += next.Mins[2]
		v[2] -= other.Mins[2]
		other.S.Origin = v
		next = g.G_PickTarget(next.Target)
		if !g.ctfmod { // not in the ctf fork's older base
			other.S.Event = EV_OTHER_TELEPORT
		}
	}

	other.Movetarget = next
	other.Goalentity = next

	if self.Wait != 0 {
		other.Monsterinfo.Pausetime = g.level.Time + self.Wait
		other.Monsterinfo.Stand.fn(g, other)
		return
	}

	if other.Movetarget == nil {
		other.Monsterinfo.Pausetime = g.level.Time + 100000000
		other.Monsterinfo.Stand.fn(g, other)
	} else {
		v = shared.VectorSubtract(other.Goalentity.S.Origin, other.S.Origin)
		other.IdealYaw = vectoyaw(v)
	}
}

// C: game/g_misc.c:399 SP_path_corner
func (g *Game) SP_path_corner(self *Edict) {
	if self.Targetname == "" {
		g.dprintf("path_corner with no targetname at %s\n", vtos(self.S.Origin))
		g.G_FreeEdict(self)
		return
	}

	self.Solid = SOLID_TRIGGER
	self.Touch = path_corner_touch
	self.Mins = Vec3{-8, -8, -8}
	self.Maxs = Vec3{8, 8, 8}
	self.SVFlags |= SVF_NOCLIENT
	g.gi.LinkEntity(self)
}

/*QUAKED point_combat (0.5 0.3 0) (-8 -8 -8) (8 8 8) Hold
Makes this the target of a monster and it will head here
when first activated before going after the activator.  If
hold is selected, it will stay here.
*/

// C: game/g_misc.c:422 point_combat_touch
func (g *Game) point_combat_touch(self, other *Edict, plane *CPlane, surf *CSurface) {
	var activator *Edict

	if other.Movetarget != self {
		return
	}

	if self.Target != "" {
		other.Target = self.Target
		other.Movetarget = g.G_PickTarget(other.Target)
		other.Goalentity = other.Movetarget
		if other.Goalentity == nil {
			g.dprintf("%s at %s target %s does not exist\n", self.Classname, vtos(self.S.Origin), self.Target)
			other.Movetarget = self
		}
		self.Target = ""
	} else if self.Spawnflags&1 != 0 && other.Flags&(FL_SWIM|FL_FLY) == 0 {
		other.Monsterinfo.Pausetime = g.level.Time + 100000000
		other.Monsterinfo.Aiflags |= AI_STAND_GROUND
		other.Monsterinfo.Stand.fn(g, other)
	}

	if other.Movetarget == self {
		other.Target = ""
		other.Movetarget = nil
		other.Goalentity = other.Enemy
		other.Monsterinfo.Aiflags &^= AI_COMBAT_POINT
	}

	if self.Pathtarget != "" {
		savetarget := self.Target
		self.Target = self.Pathtarget
		if other.Enemy != nil && other.Enemy.Client != nil {
			activator = other.Enemy
		} else if other.Oldenemy != nil && other.Oldenemy.Client != nil {
			activator = other.Oldenemy
		} else if other.Activator != nil && other.Activator.Client != nil {
			activator = other.Activator
		} else {
			activator = other
		}
		g.G_UseTargets(self, activator)
		self.Target = savetarget
	}
}

// C: game/g_misc.c:474 SP_point_combat
func (g *Game) SP_point_combat(self *Edict) {
	if g.deathmatch.Value != 0 {
		g.G_FreeEdict(self)
		return
	}
	self.Solid = SOLID_TRIGGER
	self.Touch = point_combat_touch
	self.Mins = Vec3{-8, -8, -16}
	self.Maxs = Vec3{8, 8, 16}
	self.SVFlags = SVF_NOCLIENT
	g.gi.LinkEntity(self)
}

/*QUAKED viewthing (0 .5 .8) (-8 -8 -8) (8 8 8)
Just for the debugging level.  Don't use
*/

// C: game/g_misc.c:493 TH_viewthing
func (g *Game) TH_viewthing(ent *Edict) {
	ent.S.Frame = (ent.S.Frame + 1) % 7
	ent.Nextthink = float32(float64(g.level.Time) + FRAMETIME)

	// C: ctf/g_misc.c:516 (static int robotron[4] is never filled in)
	if g.ctfmod && ent.Spawnflags != 0 {
		if ent.S.Frame == 0 {
			ent.Spawnflags = (ent.Spawnflags+1)%4 + 1
			ent.S.ModelIndex = 0 // robotron[ent->spawnflags - 1]
		}
	}
}

// C: game/g_misc.c:499 SP_viewthing
func (g *Game) SP_viewthing(ent *Edict) {
	g.gi.Dprintf("viewthing spawned\n")

	ent.Movetype = MOVETYPE_NONE
	ent.Solid = SOLID_BBOX
	ent.S.RenderFX = RF_FRAMELERP
	ent.Mins = Vec3{-16, -16, -24}
	ent.Maxs = Vec3{16, 16, 32}
	ent.S.ModelIndex = int32(g.gi.ModelIndex("models/objects/banner/tris.md2"))
	g.gi.LinkEntity(ent)
	ent.Nextthink = float32(float64(g.level.Time) + 0.5)
	ent.Think = TH_viewthing
}

/*QUAKED info_null (0 0.5 0) (-4 -4 -4) (4 4 4)
Used as a positional target for spotlights, etc.
*/
// C: game/g_misc.c:519 SP_info_null
func (g *Game) SP_info_null(self *Edict) {
	g.G_FreeEdict(self)
}

/*QUAKED info_notnull (0 0.5 0) (-4 -4 -4) (4 4 4)
Used as a positional target for lightning.
*/
// C: game/g_misc.c:528 SP_info_notnull
func (g *Game) SP_info_notnull(self *Edict) {
	self.AbsMin = self.S.Origin
	self.AbsMax = self.S.Origin
}

/*QUAKED light (0 1 0) (-8 -8 -8) (8 8 8) START_OFF
Non-displayed light.
Default light value is 300.
Default style is 0.
If targeted, will toggle between on and off.
Default _cone value is 10 (used to set size of light for spotlights)
*/

// C: game/g_misc.c:543 START_OFF
const START_OFF = 1

// C: game/g_misc.c:545 light_use
func (g *Game) light_use(self, other, activator *Edict) {
	if self.Spawnflags&START_OFF != 0 {
		g.gi.Configstring(CS_LIGHTS+int(self.Style), "m")
		self.Spawnflags &^= START_OFF
	} else {
		g.gi.Configstring(CS_LIGHTS+int(self.Style), "a")
		self.Spawnflags |= START_OFF
	}
}

// C: game/g_misc.c:559 SP_light
func (g *Game) SP_light(self *Edict) {
	// no targeted lights in deathmatch, because they cause global messages
	if self.Targetname == "" || g.deathmatch.Value != 0 {
		g.G_FreeEdict(self)
		return
	}

	if self.Style >= 32 {
		self.Use = light_use
		if self.Spawnflags&START_OFF != 0 {
			g.gi.Configstring(CS_LIGHTS+int(self.Style), "a")
		} else {
			g.gi.Configstring(CS_LIGHTS+int(self.Style), "m")
		}
	}
}

/*QUAKED func_wall (0 .5 .8) ? TRIGGER_SPAWN TOGGLE START_ON ANIMATED ANIMATED_FAST
This is just a solid wall if not inhibited

TRIGGER_SPAWN	the wall will not be present until triggered
				it will then blink in to existance; it will
				kill anything that was in it's way

TOGGLE			only valid for TRIGGER_SPAWN walls
				this allows the wall to be turned on and off

START_ON		only valid for TRIGGER_SPAWN walls
				the wall will initially be present
*/

// C: game/g_misc.c:593 func_wall_use
func (g *Game) func_wall_use(self, other, activator *Edict) {
	if self.Solid == SOLID_NOT {
		self.Solid = SOLID_BSP
		self.SVFlags &^= SVF_NOCLIENT
		g.KillBox(self)
	} else {
		self.Solid = SOLID_NOT
		self.SVFlags |= SVF_NOCLIENT
	}
	g.gi.LinkEntity(self)

	if self.Spawnflags&2 == 0 {
		self.Use = nil
	}
}

// C: game/g_misc.c:612 SP_func_wall
func (g *Game) SP_func_wall(self *Edict) {
	self.Movetype = MOVETYPE_PUSH
	g.gi.SetModel(self, self.Model)

	if self.Spawnflags&8 != 0 {
		self.S.Effects |= EF_ANIM_ALL
	}
	if self.Spawnflags&16 != 0 {
		self.S.Effects |= EF_ANIM_ALLFAST
	}

	// just a wall
	if self.Spawnflags&7 == 0 {
		self.Solid = SOLID_BSP
		g.gi.LinkEntity(self)
		return
	}

	// it must be TRIGGER_SPAWN
	if self.Spawnflags&1 == 0 {
		//		gi.dprintf("func_wall missing TRIGGER_SPAWN\n");
		self.Spawnflags |= 1
	}

	// yell if the spawnflags are odd
	if self.Spawnflags&4 != 0 {
		if self.Spawnflags&2 == 0 {
			g.gi.Dprintf("func_wall START_ON without TOGGLE\n")
			self.Spawnflags |= 2
		}
	}

	self.Use = func_wall_use
	if self.Spawnflags&4 != 0 {
		self.Solid = SOLID_BSP
	} else {
		self.Solid = SOLID_NOT
		self.SVFlags |= SVF_NOCLIENT
	}
	g.gi.LinkEntity(self)
}

/*QUAKED func_object (0 .5 .8) ? TRIGGER_SPAWN ANIMATED ANIMATED_FAST
This is solid bmodel that will fall if it's support it removed.
*/

// C: game/g_misc.c:665 func_object_touch
func (g *Game) func_object_touch(self, other *Edict, plane *CPlane, surf *CSurface) {
	// only squash thing we fall on top of
	if plane == nil {
		return
	}
	if float64(plane.Normal[2]) < 1.0 {
		return
	}
	if other.Takedamage == DAMAGE_NO {
		return
	}
	dir := shared.Vec3Origin
	g.T_Damage(other, self, self, &dir, self.S.Origin, shared.Vec3Origin, self.Dmg, 1, 0, MOD_CRUSH)
}

// C: game/g_misc.c:677 func_object_release
func (g *Game) func_object_release(self *Edict) {
	self.Movetype = MOVETYPE_TOSS
	self.Touch = func_object_touch
}

// C: game/g_misc.c:683 func_object_use
func (g *Game) func_object_use(self, other, activator *Edict) {
	self.Solid = SOLID_BSP
	self.SVFlags &^= SVF_NOCLIENT
	self.Use = nil
	g.KillBox(self)
	g.func_object_release(self)
}

// C: game/g_misc.c:692 SP_func_object
func (g *Game) SP_func_object(self *Edict) {
	g.gi.SetModel(self, self.Model)

	self.Mins[0] += 1
	self.Mins[1] += 1
	self.Mins[2] += 1
	self.Maxs[0] -= 1
	self.Maxs[1] -= 1
	self.Maxs[2] -= 1

	if self.Dmg == 0 {
		self.Dmg = 100
	}

	if self.Spawnflags == 0 {
		self.Solid = SOLID_BSP
		self.Movetype = MOVETYPE_PUSH
		self.Think = func_object_release
		self.Nextthink = float32(float64(g.level.Time) + 2*FRAMETIME)
	} else {
		self.Solid = SOLID_NOT
		self.Movetype = MOVETYPE_PUSH
		self.Use = func_object_use
		self.SVFlags |= SVF_NOCLIENT
	}

	if self.Spawnflags&2 != 0 {
		self.S.Effects |= EF_ANIM_ALL
	}
	if self.Spawnflags&4 != 0 {
		self.S.Effects |= EF_ANIM_ALLFAST
	}

	self.ClipMask = MASK_MONSTERSOLID

	g.gi.LinkEntity(self)
}

/*QUAKED func_explosive (0 .5 .8) ? Trigger_Spawn ANIMATED ANIMATED_FAST
Any brush that you want to explode or break apart.  If you want an
ex0plosion, set dmg and it will do a radius explosion of that amount
at the center of the bursh.

If targeted it will not be shootable.

health defaults to 100.

mass defaults to 75.  This determines how much debris is emitted when
it explodes.  You get one large chunk per 100 of mass (up to 8) and
one small chunk per 25 of mass (up to 16).  So 800 gives the most.
*/

// C: game/g_misc.c:745 func_explosive_explode
func (g *Game) func_explosive_explode(self, inflictor, attacker *Edict, damage int32, point Vec3) {
	var chunkorigin Vec3
	var count, mass int32

	// bmodel origins are (0 0 0), we need to adjust that here
	size := shared.VectorScale(self.Size, 0.5)
	origin := shared.VectorAdd(self.AbsMin, size)
	self.S.Origin = origin

	self.Takedamage = DAMAGE_NO

	if self.Dmg != 0 {
		g.T_RadiusDamage(self, attacker, float32(self.Dmg), nil, float32(self.Dmg+40), MOD_EXPLOSIVE)
	}

	self.Velocity = shared.VectorSubtract(self.S.Origin, inflictor.S.Origin)
	shared.VectorNormalize(&self.Velocity)
	self.Velocity = shared.VectorScale(self.Velocity, 150)

	// start chunks towards the center
	size = shared.VectorScale(size, 0.5)

	mass = self.Mass
	if mass == 0 {
		mass = 75
	}

	// big chunks
	if mass >= 100 {
		count = mass / 100
		if count > 8 {
			count = 8
		}
		for ; count > 0; count-- {
			chunkorigin[0] = float32(float64(origin[0]) + g.crandom()*float64(size[0]))
			chunkorigin[1] = float32(float64(origin[1]) + g.crandom()*float64(size[1]))
			chunkorigin[2] = float32(float64(origin[2]) + g.crandom()*float64(size[2]))
			g.ThrowDebris(self, "models/objects/debris1/tris.md2", 1, chunkorigin)
		}
	}

	// small chunks
	count = mass / 25
	if count > 16 {
		count = 16
	}
	for ; count > 0; count-- {
		chunkorigin[0] = float32(float64(origin[0]) + g.crandom()*float64(size[0]))
		chunkorigin[1] = float32(float64(origin[1]) + g.crandom()*float64(size[1]))
		chunkorigin[2] = float32(float64(origin[2]) + g.crandom()*float64(size[2]))
		g.ThrowDebris(self, "models/objects/debris2/tris.md2", 2, chunkorigin)
	}

	g.G_UseTargets(self, attacker)

	if self.Dmg != 0 {
		g.BecomeExplosion1(self)
	} else {
		g.G_FreeEdict(self)
	}
}

// C: game/g_misc.c:809 func_explosive_use
func (g *Game) func_explosive_use(self, other, activator *Edict) {
	g.func_explosive_explode(self, self, other, self.Health, shared.Vec3Origin)
}

// C: game/g_misc.c:814 func_explosive_spawn
func (g *Game) func_explosive_spawn(self, other, activator *Edict) {
	self.Solid = SOLID_BSP
	self.SVFlags &^= SVF_NOCLIENT
	self.Use = nil
	g.KillBox(self)
	g.gi.LinkEntity(self)
}

// C: game/g_misc.c:823 SP_func_explosive
func (g *Game) SP_func_explosive(self *Edict) {
	if g.deathmatch.Value != 0 { // auto-remove for deathmatch
		g.G_FreeEdict(self)
		return
	}

	self.Movetype = MOVETYPE_PUSH

	g.gi.ModelIndex("models/objects/debris1/tris.md2")
	g.gi.ModelIndex("models/objects/debris2/tris.md2")

	g.gi.SetModel(self, self.Model)

	if self.Spawnflags&1 != 0 {
		self.SVFlags |= SVF_NOCLIENT
		self.Solid = SOLID_NOT
		self.Use = func_explosive_spawn
	} else {
		self.Solid = SOLID_BSP
		if self.Targetname != "" {
			self.Use = func_explosive_use
		}
	}

	if self.Spawnflags&2 != 0 {
		self.S.Effects |= EF_ANIM_ALL
	}
	if self.Spawnflags&4 != 0 {
		self.S.Effects |= EF_ANIM_ALLFAST
	}

	if self.Use != func_explosive_use {
		if self.Health == 0 {
			self.Health = 100
		}
		self.Die = func_explosive_explode
		self.Takedamage = DAMAGE_YES
	}

	g.gi.LinkEntity(self)
}

/*QUAKED misc_explobox (0 .5 .8) (-16 -16 0) (16 16 40)
Large exploding box.  You can override its mass (100),
health (80), and dmg (150).
*/

// C: game/g_misc.c:873 barrel_touch
func (g *Game) barrel_touch(self, other *Edict, plane *CPlane, surf *CSurface) {
	if other.Groundentity == nil || other.Groundentity == self {
		return
	}

	ratio := float32(other.Mass) / float32(self.Mass)
	v := shared.VectorSubtract(self.S.Origin, other.S.Origin)
	g.M_walkmove(self, vectoyaw(v), float32(float64(20*ratio)*FRAMETIME))
}

// barrelRandOrigin is the repeated
// org[i] = self->s.origin[i] + crandom() * self->size[i] block of barrel_explode.
func (g *Game) barrelRandOrigin(self *Edict) Vec3 {
	var org Vec3
	org[0] = float32(float64(self.S.Origin[0]) + g.crandom()*float64(self.Size[0]))
	org[1] = float32(float64(self.S.Origin[1]) + g.crandom()*float64(self.Size[1]))
	org[2] = float32(float64(self.S.Origin[2]) + g.crandom()*float64(self.Size[2]))
	return org
}

// C: game/g_misc.c:887 barrel_explode
func (g *Game) barrel_explode(self *Edict) {
	var org Vec3
	var spd float32

	g.T_RadiusDamage(self, self.Activator, float32(self.Dmg), nil, float32(self.Dmg+40), MOD_BARREL)

	save := self.S.Origin
	self.S.Origin = shared.VectorMA(self.AbsMin, 0.5, self.Size)

	// a few big chunks
	spd = float32(1.5 * float64(float32(self.Dmg)) / 200.0)
	org = g.barrelRandOrigin(self)
	g.ThrowDebris(self, "models/objects/debris1/tris.md2", spd, org)
	org = g.barrelRandOrigin(self)
	g.ThrowDebris(self, "models/objects/debris1/tris.md2", spd, org)

	// bottom corners
	spd = float32(1.75 * float64(float32(self.Dmg)) / 200.0)
	org = self.AbsMin
	g.ThrowDebris(self, "models/objects/debris3/tris.md2", spd, org)
	org = self.AbsMin
	org[0] += self.Size[0]
	g.ThrowDebris(self, "models/objects/debris3/tris.md2", spd, org)
	org = self.AbsMin
	org[1] += self.Size[1]
	g.ThrowDebris(self, "models/objects/debris3/tris.md2", spd, org)
	org = self.AbsMin
	org[0] += self.Size[0]
	org[1] += self.Size[1]
	g.ThrowDebris(self, "models/objects/debris3/tris.md2", spd, org)

	// a bunch of little chunks
	spd = float32(2 * self.Dmg / 200)
	for i := 0; i < 8; i++ {
		org = g.barrelRandOrigin(self)
		g.ThrowDebris(self, "models/objects/debris2/tris.md2", spd, org)
	}

	self.S.Origin = save
	if self.Groundentity != nil {
		g.BecomeExplosion2(self)
	} else {
		g.BecomeExplosion1(self)
	}
}

// C: game/g_misc.c:966 barrel_delay
func (g *Game) barrel_delay(self, inflictor, attacker *Edict, damage int32, point Vec3) {
	self.Takedamage = DAMAGE_NO
	self.Nextthink = float32(float64(g.level.Time) + 2*FRAMETIME)
	self.Think = barrel_explode
	self.Activator = attacker
}

// C: game/g_misc.c:974 SP_misc_explobox
func (g *Game) SP_misc_explobox(self *Edict) {
	if g.deathmatch.Value != 0 { // auto-remove for deathmatch
		g.G_FreeEdict(self)
		return
	}

	g.gi.ModelIndex("models/objects/debris1/tris.md2")
	g.gi.ModelIndex("models/objects/debris2/tris.md2")
	g.gi.ModelIndex("models/objects/debris3/tris.md2")

	self.Solid = SOLID_BBOX
	self.Movetype = MOVETYPE_STEP

	self.Model = "models/objects/barrels/tris.md2"
	self.S.ModelIndex = int32(g.gi.ModelIndex(self.Model))
	self.Mins = Vec3{-16, -16, 0}
	self.Maxs = Vec3{16, 16, 40}

	if self.Mass == 0 {
		self.Mass = 400
	}
	if self.Health == 0 {
		self.Health = 10
	}
	if self.Dmg == 0 {
		self.Dmg = 150
	}

	self.Die = barrel_delay
	self.Takedamage = DAMAGE_YES
	self.Monsterinfo.Aiflags = AI_NOSTEP

	self.Touch = barrel_touch

	self.Think = M_droptofloor
	self.Nextthink = float32(float64(g.level.Time) + 2*FRAMETIME)

	g.gi.LinkEntity(self)
}

//
// miscellaneous specialty items
//

/*QUAKED misc_blackhole (1 .5 0) (-8 -8 -8) (8 8 8)
 */

// C: game/g_misc.c:1021 misc_blackhole_use
func (g *Game) misc_blackhole_use(ent, other, activator *Edict) {
	/*
		gi.WriteByte (svc_temp_entity);
		gi.WriteByte (TE_BOSSTPORT);
		gi.WritePosition (ent->s.origin);
		gi.multicast (ent->s.origin, MULTICAST_PVS);
	*/
	g.G_FreeEdict(ent)
}

// C: game/g_misc.c:1032 misc_blackhole_think
func (g *Game) misc_blackhole_think(self *Edict) {
	self.S.Frame++
	if self.S.Frame < 19 {
		self.Nextthink = float32(float64(g.level.Time) + FRAMETIME)
	} else {
		self.S.Frame = 0
		self.Nextthink = float32(float64(g.level.Time) + FRAMETIME)
	}
}

// C: game/g_misc.c:1043 SP_misc_blackhole
func (g *Game) SP_misc_blackhole(ent *Edict) {
	ent.Movetype = MOVETYPE_NONE
	ent.Solid = SOLID_NOT
	ent.Mins = Vec3{-64, -64, 0}
	ent.Maxs = Vec3{64, 64, 8}
	ent.S.ModelIndex = int32(g.gi.ModelIndex("models/objects/black/tris.md2"))
	ent.S.RenderFX = RF_TRANSLUCENT
	ent.Use = misc_blackhole_use
	ent.Think = misc_blackhole_think
	ent.Nextthink = float32(float64(g.level.Time) + 2*FRAMETIME)
	g.gi.LinkEntity(ent)
}

/*QUAKED misc_eastertank (1 .5 0) (-32 -32 -16) (32 32 32)
 */

// C: game/g_misc.c:1060 misc_eastertank_think
func (g *Game) misc_eastertank_think(self *Edict) {
	self.S.Frame++
	if self.S.Frame < 293 {
		self.Nextthink = float32(float64(g.level.Time) + FRAMETIME)
	} else {
		self.S.Frame = 254
		self.Nextthink = float32(float64(g.level.Time) + FRAMETIME)
	}
}

// C: game/g_misc.c:1071 SP_misc_eastertank
func (g *Game) SP_misc_eastertank(ent *Edict) {
	ent.Movetype = MOVETYPE_NONE
	ent.Solid = SOLID_BBOX
	ent.Mins = Vec3{-32, -32, -16}
	ent.Maxs = Vec3{32, 32, 32}
	ent.S.ModelIndex = int32(g.gi.ModelIndex("models/monsters/tank/tris.md2"))
	ent.S.Frame = 254
	ent.Think = misc_eastertank_think
	ent.Nextthink = float32(float64(g.level.Time) + 2*FRAMETIME)
	g.gi.LinkEntity(ent)
}

/*QUAKED misc_easterchick (1 .5 0) (-32 -32 0) (32 32 32)
 */

// C: game/g_misc.c:1088 misc_easterchick_think
func (g *Game) misc_easterchick_think(self *Edict) {
	self.S.Frame++
	if self.S.Frame < 247 {
		self.Nextthink = float32(float64(g.level.Time) + FRAMETIME)
	} else {
		self.S.Frame = 208
		self.Nextthink = float32(float64(g.level.Time) + FRAMETIME)
	}
}

// C: game/g_misc.c:1099 SP_misc_easterchick
func (g *Game) SP_misc_easterchick(ent *Edict) {
	ent.Movetype = MOVETYPE_NONE
	ent.Solid = SOLID_BBOX
	ent.Mins = Vec3{-32, -32, 0}
	ent.Maxs = Vec3{32, 32, 32}
	ent.S.ModelIndex = int32(g.gi.ModelIndex("models/monsters/bitch/tris.md2"))
	ent.S.Frame = 208
	ent.Think = misc_easterchick_think
	ent.Nextthink = float32(float64(g.level.Time) + 2*FRAMETIME)
	g.gi.LinkEntity(ent)
}

/*QUAKED misc_easterchick2 (1 .5 0) (-32 -32 0) (32 32 32)
 */

// C: game/g_misc.c:1116 misc_easterchick2_think
func (g *Game) misc_easterchick2_think(self *Edict) {
	self.S.Frame++
	if self.S.Frame < 287 {
		self.Nextthink = float32(float64(g.level.Time) + FRAMETIME)
	} else {
		self.S.Frame = 248
		self.Nextthink = float32(float64(g.level.Time) + FRAMETIME)
	}
}

// C: game/g_misc.c:1127 SP_misc_easterchick2
func (g *Game) SP_misc_easterchick2(ent *Edict) {
	ent.Movetype = MOVETYPE_NONE
	ent.Solid = SOLID_BBOX
	ent.Mins = Vec3{-32, -32, 0}
	ent.Maxs = Vec3{32, 32, 32}
	ent.S.ModelIndex = int32(g.gi.ModelIndex("models/monsters/bitch/tris.md2"))
	ent.S.Frame = 248
	ent.Think = misc_easterchick2_think
	ent.Nextthink = float32(float64(g.level.Time) + 2*FRAMETIME)
	g.gi.LinkEntity(ent)
}

/*QUAKED monster_commander_body (1 .5 0) (-32 -32 0) (32 32 48)
Not really a monster, this is the Tank Commander's decapitated body.
There should be a item_commander_head that has this as it's target.
*/

// C: game/g_misc.c:1146 commander_body_think
func (g *Game) commander_body_think(self *Edict) {
	self.S.Frame++
	if self.S.Frame < 24 {
		self.Nextthink = float32(float64(g.level.Time) + FRAMETIME)
	} else {
		self.Nextthink = 0
	}

	if self.S.Frame == 22 {
		g.gi.Sound(self, CHAN_BODY, g.gi.SoundIndex("tank/thud.wav"), 1, ATTN_NORM, 0)
	}
}

// C: game/g_misc.c:1157 commander_body_use
func (g *Game) commander_body_use(self, other, activator *Edict) {
	self.Think = commander_body_think
	self.Nextthink = float32(float64(g.level.Time) + FRAMETIME)
	g.gi.Sound(self, CHAN_BODY, g.gi.SoundIndex("tank/pain.wav"), 1, ATTN_NORM, 0)
}

// C: game/g_misc.c:1164 commander_body_drop
func (g *Game) commander_body_drop(self *Edict) {
	self.Movetype = MOVETYPE_TOSS
	self.S.Origin[2] += 2
}

// C: game/g_misc.c:1170 SP_monster_commander_body
func (g *Game) SP_monster_commander_body(self *Edict) {
	self.Movetype = MOVETYPE_NONE
	self.Solid = SOLID_BBOX
	self.Model = "models/monsters/commandr/tris.md2"
	self.S.ModelIndex = int32(g.gi.ModelIndex(self.Model))
	self.Mins = Vec3{-32, -32, 0}
	self.Maxs = Vec3{32, 32, 48}
	self.Use = commander_body_use
	self.Takedamage = DAMAGE_YES
	self.Flags = FL_GODMODE
	self.S.RenderFX |= RF_FRAMELERP
	g.gi.LinkEntity(self)

	g.gi.SoundIndex("tank/thud.wav")
	g.gi.SoundIndex("tank/pain.wav")

	self.Think = commander_body_drop
	self.Nextthink = float32(float64(g.level.Time) + 5*FRAMETIME)
}

/*QUAKED misc_banner (1 .5 0) (-4 -4 -4) (4 4 4)
The origin is the bottom of the banner.
The banner is 128 tall.
*/
// C: game/g_misc.c:1196 misc_banner_think
func (g *Game) misc_banner_think(ent *Edict) {
	ent.S.Frame = (ent.S.Frame + 1) % 16
	ent.Nextthink = float32(float64(g.level.Time) + FRAMETIME)
}

// C: game/g_misc.c:1202 SP_misc_banner
func (g *Game) SP_misc_banner(ent *Edict) {
	ent.Movetype = MOVETYPE_NONE
	ent.Solid = SOLID_NOT
	ent.S.ModelIndex = int32(g.gi.ModelIndex("models/objects/banner/tris.md2"))
	ent.S.Frame = g.rng.Rand() % 16
	g.gi.LinkEntity(ent)

	ent.Think = misc_banner_think
	ent.Nextthink = float32(float64(g.level.Time) + FRAMETIME)
}

/*QUAKED misc_deadsoldier (1 .5 0) (-16 -16 0) (16 16 16) ON_BACK ON_STOMACH BACK_DECAP FETAL_POS SIT_DECAP IMPALED
This is the dead player model. Comes in 6 exciting different poses!
*/
// C: game/g_misc.c:1217 misc_deadsoldier_die
func (g *Game) misc_deadsoldier_die(self, inflictor, attacker *Edict, damage int32, point Vec3) {
	if self.Health > -80 {
		return
	}

	g.gi.Sound(self, CHAN_BODY, g.gi.SoundIndex("misc/udeath.wav"), 1, ATTN_NORM, 0)
	for n := 0; n < 4; n++ {
		g.ThrowGib(self, "models/objects/gibs/sm_meat/tris.md2", damage, GIB_ORGANIC)
	}
	g.ThrowHead(self, "models/objects/gibs/head2/tris.md2", damage, GIB_ORGANIC)
}

// C: game/g_misc.c:1230 SP_misc_deadsoldier
func (g *Game) SP_misc_deadsoldier(ent *Edict) {
	if g.deathmatch.Value != 0 { // auto-remove for deathmatch
		g.G_FreeEdict(ent)
		return
	}

	ent.Movetype = MOVETYPE_NONE
	ent.Solid = SOLID_BBOX
	ent.S.ModelIndex = int32(g.gi.ModelIndex("models/deadbods/dude/tris.md2"))

	// Defaults to frame 0
	if ent.Spawnflags&2 != 0 {
		ent.S.Frame = 1
	} else if ent.Spawnflags&4 != 0 {
		ent.S.Frame = 2
	} else if ent.Spawnflags&8 != 0 {
		ent.S.Frame = 3
	} else if ent.Spawnflags&16 != 0 {
		ent.S.Frame = 4
	} else if ent.Spawnflags&32 != 0 {
		ent.S.Frame = 5
	} else {
		ent.S.Frame = 0
	}

	ent.Mins = Vec3{-16, -16, 0}
	ent.Maxs = Vec3{16, 16, 16}
	ent.Deadflag = DEAD_DEAD
	ent.Takedamage = DAMAGE_YES
	ent.SVFlags |= SVF_MONSTER | SVF_DEADMONSTER
	ent.Die = misc_deadsoldier_die
	ent.Monsterinfo.Aiflags |= AI_GOOD_GUY

	g.gi.LinkEntity(ent)
}

/*QUAKED misc_viper (1 .5 0) (-16 -16 0) (16 16 32)
This is the Viper for the flyby bombing.
It is trigger_spawned, so you must have something use it for it to show up.
There must be a path for it to follow once it is activated.

"speed"		How fast the Viper should fly
*/

// C: game/g_misc.c:1278 misc_viper_use
func (g *Game) misc_viper_use(self, other, activator *Edict) {
	self.SVFlags &^= SVF_NOCLIENT
	self.Use = train_use
	g.train_use(self, other, activator)
}

// C: game/g_misc.c:1285 SP_misc_viper
func (g *Game) SP_misc_viper(ent *Edict) {
	if ent.Target == "" {
		g.dprintf("misc_viper without a target at %s\n", vtos(ent.AbsMin))
		g.G_FreeEdict(ent)
		return
	}

	if ent.Speed == 0 {
		ent.Speed = 300
	}

	ent.Movetype = MOVETYPE_PUSH
	ent.Solid = SOLID_NOT
	ent.S.ModelIndex = int32(g.gi.ModelIndex("models/ships/viper/tris.md2"))
	ent.Mins = Vec3{-16, -16, 0}
	ent.Maxs = Vec3{16, 16, 32}

	ent.Think = func_train_find
	ent.Nextthink = float32(float64(g.level.Time) + FRAMETIME)
	ent.Use = misc_viper_use
	ent.SVFlags |= SVF_NOCLIENT
	ent.Moveinfo.Speed = ent.Speed
	ent.Moveinfo.Decel = ent.Moveinfo.Speed
	ent.Moveinfo.Accel = ent.Moveinfo.Decel

	g.gi.LinkEntity(ent)
}

/*QUAKED misc_bigviper (1 .5 0) (-176 -120 -24) (176 120 72)
This is a large stationary viper as seen in Paul's intro
*/
// C: game/g_misc.c:1316 SP_misc_bigviper
func (g *Game) SP_misc_bigviper(ent *Edict) {
	ent.Movetype = MOVETYPE_NONE
	ent.Solid = SOLID_BBOX
	ent.Mins = Vec3{-176, -120, -24}
	ent.Maxs = Vec3{176, 120, 72}
	ent.S.ModelIndex = int32(g.gi.ModelIndex("models/ships/bigviper/tris.md2"))
	g.gi.LinkEntity(ent)
}

/*QUAKED misc_viper_bomb (1 0 0) (-8 -8 -8) (8 8 8)
"dmg"	how much boom should the bomb make?
*/
// C: game/g_misc.c:1330 misc_viper_bomb_touch
func (g *Game) misc_viper_bomb_touch(self, other *Edict, plane *CPlane, surf *CSurface) {
	g.G_UseTargets(self, self.Activator)

	self.S.Origin[2] = self.AbsMin[2] + 1
	g.T_RadiusDamage(self, self, float32(self.Dmg), nil, float32(self.Dmg+40), MOD_BOMB)
	g.BecomeExplosion2(self)
}

// C: game/g_misc.c:1339 misc_viper_bomb_prethink
func (g *Game) misc_viper_bomb_prethink(self *Edict) {
	var diff float32

	self.Groundentity = nil

	diff = self.Timestamp - g.level.Time
	if float64(diff) < -1.0 {
		diff = -1.0
	}

	v := shared.VectorScale(self.Moveinfo.Dir, float32(1.0+float64(diff)))
	v[2] = diff

	diff = self.S.Angles[2]
	self.S.Angles = vectoangles(v)
	self.S.Angles[2] = diff + 10
}

// C: game/g_misc.c:1358 misc_viper_bomb_use
func (g *Game) misc_viper_bomb_use(self, other, activator *Edict) {
	self.Solid = SOLID_BBOX
	self.SVFlags &^= SVF_NOCLIENT
	self.S.Effects |= EF_ROCKET
	self.Use = nil
	self.Movetype = MOVETYPE_TOSS
	self.Prethink = misc_viper_bomb_prethink
	self.Touch = misc_viper_bomb_touch
	self.Activator = activator

	viper := g.G_Find(nil, FOFS_classname, "misc_viper")
	self.Velocity = shared.VectorScale(viper.Moveinfo.Dir, viper.Moveinfo.Speed)

	self.Timestamp = g.level.Time
	self.Moveinfo.Dir = viper.Moveinfo.Dir
}

// C: game/g_misc.c:1378 SP_misc_viper_bomb
func (g *Game) SP_misc_viper_bomb(self *Edict) {
	self.Movetype = MOVETYPE_NONE
	self.Solid = SOLID_NOT
	self.Mins = Vec3{-8, -8, -8}
	self.Maxs = Vec3{8, 8, 8}

	self.S.ModelIndex = int32(g.gi.ModelIndex("models/objects/bomb/tris.md2"))

	if self.Dmg == 0 {
		self.Dmg = 1000
	}

	self.Use = misc_viper_bomb_use
	self.SVFlags |= SVF_NOCLIENT

	g.gi.LinkEntity(self)
}

/*QUAKED misc_strogg_ship (1 .5 0) (-16 -16 0) (16 16 32)
This is a Storgg ship for the flybys.
It is trigger_spawned, so you must have something use it for it to show up.
There must be a path for it to follow once it is activated.

"speed"		How fast it should fly
*/

// C: game/g_misc.c:1408 misc_strogg_ship_use
func (g *Game) misc_strogg_ship_use(self, other, activator *Edict) {
	self.SVFlags &^= SVF_NOCLIENT
	self.Use = train_use
	g.train_use(self, other, activator)
}

// C: game/g_misc.c:1415 SP_misc_strogg_ship
func (g *Game) SP_misc_strogg_ship(ent *Edict) {
	if ent.Target == "" {
		g.dprintf("%s without a target at %s\n", ent.Classname, vtos(ent.AbsMin))
		g.G_FreeEdict(ent)
		return
	}

	if ent.Speed == 0 {
		ent.Speed = 300
	}

	ent.Movetype = MOVETYPE_PUSH
	ent.Solid = SOLID_NOT
	ent.S.ModelIndex = int32(g.gi.ModelIndex("models/ships/strogg1/tris.md2"))
	ent.Mins = Vec3{-16, -16, 0}
	ent.Maxs = Vec3{16, 16, 32}

	ent.Think = func_train_find
	ent.Nextthink = float32(float64(g.level.Time) + FRAMETIME)
	ent.Use = misc_strogg_ship_use
	ent.SVFlags |= SVF_NOCLIENT
	ent.Moveinfo.Speed = ent.Speed
	ent.Moveinfo.Decel = ent.Moveinfo.Speed
	ent.Moveinfo.Accel = ent.Moveinfo.Decel

	g.gi.LinkEntity(ent)
}

/*QUAKED misc_satellite_dish (1 .5 0) (-64 -64 0) (64 64 128)
 */
// C: game/g_misc.c:1445 misc_satellite_dish_think
func (g *Game) misc_satellite_dish_think(self *Edict) {
	self.S.Frame++
	if self.S.Frame < 38 {
		self.Nextthink = float32(float64(g.level.Time) + FRAMETIME)
	}
}

// C: game/g_misc.c:1452 misc_satellite_dish_use
func (g *Game) misc_satellite_dish_use(self, other, activator *Edict) {
	self.S.Frame = 0
	self.Think = misc_satellite_dish_think
	self.Nextthink = float32(float64(g.level.Time) + FRAMETIME)
}

// C: game/g_misc.c:1459 SP_misc_satellite_dish
func (g *Game) SP_misc_satellite_dish(ent *Edict) {
	ent.Movetype = MOVETYPE_NONE
	ent.Solid = SOLID_BBOX
	ent.Mins = Vec3{-64, -64, 0}
	ent.Maxs = Vec3{64, 64, 128}
	ent.S.ModelIndex = int32(g.gi.ModelIndex("models/objects/satellite/tris.md2"))
	ent.Use = misc_satellite_dish_use
	g.gi.LinkEntity(ent)
}

/*QUAKED light_mine1 (0 1 0) (-2 -2 -12) (2 2 12)
 */
// C: game/g_misc.c:1473 SP_light_mine1
func (g *Game) SP_light_mine1(ent *Edict) {
	ent.Movetype = MOVETYPE_NONE
	ent.Solid = SOLID_BBOX
	ent.S.ModelIndex = int32(g.gi.ModelIndex("models/objects/minelite/light1/tris.md2"))
	g.gi.LinkEntity(ent)
}

/*QUAKED light_mine2 (0 1 0) (-2 -2 -12) (2 2 12)
 */
// C: game/g_misc.c:1484 SP_light_mine2
func (g *Game) SP_light_mine2(ent *Edict) {
	ent.Movetype = MOVETYPE_NONE
	ent.Solid = SOLID_BBOX
	ent.S.ModelIndex = int32(g.gi.ModelIndex("models/objects/minelite/light2/tris.md2"))
	g.gi.LinkEntity(ent)
}

// miscGib is the shared body of SP_misc_gib_arm/leg/head.
func (g *Game) miscGib(ent *Edict, model string) {
	g.gi.SetModel(ent, model)
	ent.Solid = SOLID_NOT
	ent.S.Effects |= EF_GIB
	ent.Takedamage = DAMAGE_YES
	ent.Die = gib_die
	ent.Movetype = MOVETYPE_TOSS
	ent.SVFlags |= SVF_MONSTER
	ent.Deadflag = DEAD_DEAD
	ent.Avelocity[0] = g.random() * 200
	ent.Avelocity[1] = g.random() * 200
	ent.Avelocity[2] = g.random() * 200
	ent.Think = G_FreeEdict
	ent.Nextthink = g.level.Time + 30
	g.gi.LinkEntity(ent)
}

/*QUAKED misc_gib_arm (1 0 0) (-8 -8 -8) (8 8 8)
Intended for use with the target_spawner
*/
// C: game/g_misc.c:1496 SP_misc_gib_arm
func (g *Game) SP_misc_gib_arm(ent *Edict) {
	g.miscGib(ent, "models/objects/gibs/arm/tris.md2")
}

/*QUAKED misc_gib_leg (1 0 0) (-8 -8 -8) (8 8 8)
Intended for use with the target_spawner
*/
// C: game/g_misc.c:1517 SP_misc_gib_leg
func (g *Game) SP_misc_gib_leg(ent *Edict) {
	g.miscGib(ent, "models/objects/gibs/leg/tris.md2")
}

/*QUAKED misc_gib_head (1 0 0) (-8 -8 -8) (8 8 8)
Intended for use with the target_spawner
*/
// C: game/g_misc.c:1538 SP_misc_gib_head
func (g *Game) SP_misc_gib_head(ent *Edict) {
	g.miscGib(ent, "models/objects/gibs/head/tris.md2")
}

//=====================================================

/*QUAKED target_character (0 0 1) ?
used with target_string (must be on same "team")
"count" is position in the string (starts at 1)
*/

// C: game/g_misc.c:1563 SP_target_character
func (g *Game) SP_target_character(self *Edict) {
	self.Movetype = MOVETYPE_PUSH
	g.gi.SetModel(self, self.Model)
	self.Solid = SOLID_BSP
	self.S.Frame = 12
	g.gi.LinkEntity(self)
}

/*QUAKED target_string (0 0 1) (-8 -8 -8) (8 8 8)
 */

// C: game/g_misc.c:1577 target_string_use
func (g *Game) target_string_use(self, other, activator *Edict) {
	l := int32(len(self.Message))
	for e := self.Teammaster; e != nil; e = e.Teamchain {
		if e.Count == 0 {
			continue
		}
		n := e.Count - 1
		if n > l {
			e.S.Frame = 12
			continue
		}

		var c byte // message[l] is the terminating NUL
		if n < l {
			c = self.Message[n]
		}
		if c >= '0' && c <= '9' {
			e.S.Frame = int32(c - '0')
		} else if c == '-' {
			e.S.Frame = 10
		} else if c == ':' {
			e.S.Frame = 11
		} else {
			e.S.Frame = 12
		}
	}
}

// C: game/g_misc.c:1607 SP_target_string
func (g *Game) SP_target_string(self *Edict) {
	if self.Message == "" {
		self.Message = ""
	}
	self.Use = target_string_use
}

/*QUAKED func_clock (0 0 1) (-8 -8 -8) (8 8 8) TIMER_UP TIMER_DOWN START_OFF MULTI_USE
target a target_string with this

The default is to be a time of day clock

TIMER_UP and TIMER_DOWN run for "count" seconds and the fire "pathtarget"
If START_OFF, this entity must be used before it starts

"style"		0 "xx"
			1 "xx:xx"
			2 "xx:xx:xx"
*/

// C: game/g_misc.c:1628 CLOCK_MESSAGE_SIZE
const CLOCK_MESSAGE_SIZE = 16

// clockSprintf is Com_sprintf into a CLOCK_MESSAGE_SIZE buffer followed by
// the '0' fix-ups of positions 3 and 6.
func clockSprintf(format string, fix3, fix6 bool, args ...any) string {
	b := []byte(fmt.Sprintf(format, args...))
	if len(b) > CLOCK_MESSAGE_SIZE-1 {
		b = b[:CLOCK_MESSAGE_SIZE-1]
	}
	if fix3 && len(b) > 3 && b[3] == ' ' {
		b[3] = '0'
	}
	if fix6 && len(b) > 6 && b[6] == ' ' {
		b[6] = '0'
	}
	return string(b)
}

// don't let field width of any clock messages change, or it
// could cause an overwrite after a game load

// C: game/g_misc.c:1633 func_clock_reset
func (g *Game) func_clock_reset(self *Edict) {
	self.Activator = nil
	if self.Spawnflags&1 != 0 {
		self.Health = 0
		self.Wait = float32(self.Count)
	} else if self.Spawnflags&2 != 0 {
		self.Health = self.Count
		self.Wait = 0
	}
}

// C: game/g_misc.c:1648 func_clock_format_countdown
func (g *Game) func_clock_format_countdown(self *Edict) {
	if self.Style == 0 {
		self.Message = clockSprintf("%2d", false, false, self.Health)
		return
	}

	if self.Style == 1 {
		self.Message = clockSprintf("%2d:%2d", true, false, self.Health/60, self.Health%60)
		return
	}

	if self.Style == 2 {
		self.Message = clockSprintf("%2d:%2d:%2d", true, true, self.Health/3600, (self.Health-(self.Health/3600)*3600)/60, self.Health%60)
		return
	}
}

// C: game/g_misc.c:1675 func_clock_think
func (g *Game) func_clock_think(self *Edict) {
	if self.Enemy == nil {
		self.Enemy = g.G_Find(nil, FOFS_targetname, self.Target)
		if self.Enemy == nil {
			return
		}
	}

	if self.Spawnflags&1 != 0 {
		g.func_clock_format_countdown(self)
		self.Health++
	} else if self.Spawnflags&2 != 0 {
		g.func_clock_format_countdown(self)
		self.Health--
	} else {
		ltime := time.Now()
		self.Message = clockSprintf("%2d:%2d:%2d", true, true, ltime.Hour(), ltime.Minute(), ltime.Second())
	}

	self.Enemy.Message = self.Message
	self.Enemy.Use.fn(g, self.Enemy, self, self)

	if (self.Spawnflags&1 != 0 && float32(self.Health) > self.Wait) ||
		(self.Spawnflags&2 != 0 && float32(self.Health) < self.Wait) {
		if self.Pathtarget != "" {
			savetarget := self.Target
			savemessage := self.Message
			self.Target = self.Pathtarget
			self.Message = ""
			g.G_UseTargets(self, self.Activator)
			self.Target = savetarget
			self.Message = savemessage
		}

		if self.Spawnflags&8 == 0 {
			return
		}

		g.func_clock_reset(self)

		if self.Spawnflags&4 != 0 {
			return
		}
	}

	self.Nextthink = g.level.Time + 1
}

// C: game/g_misc.c:1740 func_clock_use
func (g *Game) func_clock_use(self, other, activator *Edict) {
	if self.Spawnflags&8 == 0 {
		self.Use = nil
	}
	if self.Activator != nil {
		return
	}
	self.Activator = activator
	self.Think.fn(g, self)
}

// C: game/g_misc.c:1750 SP_func_clock
func (g *Game) SP_func_clock(self *Edict) {
	if self.Target == "" {
		g.dprintf("%s with no target at %s\n", self.Classname, vtos(self.S.Origin))
		g.G_FreeEdict(self)
		return
	}

	if self.Spawnflags&2 != 0 && self.Count == 0 {
		g.dprintf("%s with no count at %s\n", self.Classname, vtos(self.S.Origin))
		g.G_FreeEdict(self)
		return
	}

	if self.Spawnflags&1 != 0 && self.Count == 0 {
		self.Count = 60 * 60
	}

	g.func_clock_reset(self)

	self.Message = "" // C: gi.TagMalloc (CLOCK_MESSAGE_SIZE, TAG_LEVEL) (zeroed)

	self.Think = func_clock_think

	if self.Spawnflags&4 != 0 {
		self.Use = func_clock_use
	} else {
		self.Nextthink = g.level.Time + 1
	}
}

//=================================================================================

// C: game/g_misc.c:1783 teleporter_touch
func (g *Game) teleporter_touch(self, other *Edict, plane *CPlane, surf *CSurface) {
	if other.Client == nil {
		return
	}
	dest := g.G_Find(nil, FOFS_targetname, self.Target)
	if dest == nil {
		g.gi.Dprintf("Couldn't find destination\n")
		return
	}

	//ZOID
	if g.ctfmod {
		g.CTFPlayerResetGrapple(other)
	}
	//ZOID

	// unlink to make sure it can't possibly interfere with KillBox
	g.gi.UnlinkEntity(other)

	other.S.Origin = dest.S.Origin
	other.S.OldOrigin = dest.S.Origin
	other.S.Origin[2] += 10

	// clear the velocity and hold them in place briefly
	other.Velocity = Vec3{}
	other.Client.PS.PMove.PmTime = 160 >> 3 // hold time
	other.Client.PS.PMove.PmFlags |= PMF_TIME_TELEPORT

	// draw the teleport splash at source and on the player
	self.Owner.S.Event = EV_PLAYER_TELEPORT
	other.S.Event = EV_PLAYER_TELEPORT

	// set angles
	for i := 0; i < 3; i++ {
		other.Client.PS.PMove.DeltaAngles[i] = int16(shared.ANGLE2SHORT(dest.S.Angles[i] - other.Client.Resp.CmdAngles[i]))
	}

	other.S.Angles = Vec3{}
	other.Client.PS.ViewAngles = Vec3{}
	other.Client.VAngle = Vec3{}

	// kill anything at the destination
	g.KillBox(other)

	g.gi.LinkEntity(other)
}

/*QUAKED misc_teleporter (1 0 0) (-32 -32 -24) (32 32 -16)
Stepping onto this disc will teleport players to the targeted misc_teleporter_dest object.
*/
// C: game/g_misc.c:1830 SP_misc_teleporter
func (g *Game) SP_misc_teleporter(ent *Edict) {
	if ent.Target == "" {
		g.gi.Dprintf("teleporter without a target.\n")
		g.G_FreeEdict(ent)
		return
	}

	g.gi.SetModel(ent, "models/objects/dmspot/tris.md2")
	ent.S.SkinNum = 1
	ent.S.Effects = EF_TELEPORTER
	ent.S.Sound = int32(g.gi.SoundIndex("world/amb10.wav"))
	ent.Solid = SOLID_BBOX

	ent.Mins = Vec3{-32, -32, -24}
	ent.Maxs = Vec3{32, 32, -16}
	g.gi.LinkEntity(ent)

	trig := g.G_Spawn()
	trig.Touch = teleporter_touch
	trig.Solid = SOLID_TRIGGER
	trig.Target = ent.Target
	trig.Owner = ent
	trig.S.Origin = ent.S.Origin
	trig.Mins = Vec3{-8, -8, 8}
	trig.Maxs = Vec3{8, 8, 24}
	g.gi.LinkEntity(trig)
}

/*QUAKED misc_teleporter_dest (1 0 0) (-32 -32 -24) (32 32 -16)
Point teleporters at these.
*/
// C: game/g_misc.c:1866 SP_misc_teleporter_dest
func (g *Game) SP_misc_teleporter_dest(ent *Edict) {
	g.gi.SetModel(ent, "models/objects/dmspot/tris.md2")
	ent.S.SkinNum = 0
	ent.Solid = SOLID_BBOX
	//	ent->s.effects |= EF_FLIES;
	ent.Mins = Vec3{-32, -32, -24}
	ent.Maxs = Vec3{32, 32, -16}
	g.gi.LinkEntity(ent)
}
