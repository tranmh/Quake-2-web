package game

// Port of game/g_trigger.c.

import (
	. "quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/shared"
)

// C: game/g_trigger.c:390 PUSH_ONCE
const PUSH_ONCE = 1

var (
	multi_wait                = defThink("multi_wait")
	Use_Multi                 = defUse("Use_Multi")
	Touch_Multi               = defTouch("Touch_Multi")
	trigger_enable            = defUse("trigger_enable")
	trigger_relay_use         = defUse("trigger_relay_use")
	trigger_key_use           = defUse("trigger_key_use")
	trigger_counter_use       = defUse("trigger_counter_use")
	trigger_push_touch        = defTouch("trigger_push_touch")
	hurt_use                  = defUse("hurt_use")
	hurt_touch                = defTouch("hurt_touch")
	trigger_gravity_touch     = defTouch("trigger_gravity_touch")
	trigger_monsterjump_touch = defTouch("trigger_monsterjump_touch")
)

func init() {
	multi_wait.bind((*Game).multi_wait)
	Use_Multi.bind((*Game).Use_Multi)
	Touch_Multi.bind((*Game).Touch_Multi)
	trigger_enable.bind((*Game).trigger_enable)
	trigger_relay_use.bind((*Game).trigger_relay_use)
	trigger_key_use.bind((*Game).trigger_key_use)
	trigger_counter_use.bind((*Game).trigger_counter_use)
	trigger_push_touch.bind((*Game).trigger_push_touch)
	hurt_use.bind((*Game).hurt_use)
	hurt_touch.bind((*Game).hurt_touch)
	trigger_gravity_touch.bind((*Game).trigger_gravity_touch)
	trigger_monsterjump_touch.bind((*Game).trigger_monsterjump_touch)
}

// C: game/g_trigger.c:23 InitTrigger
func (g *Game) InitTrigger(self *Edict) {
	if shared.VectorCompare(self.S.Angles, shared.Vec3Origin) == 0 {
		G_SetMovedir(&self.S.Angles, &self.Movedir)
	}

	self.Solid = SOLID_TRIGGER
	self.Movetype = MOVETYPE_NONE
	g.gi.SetModel(self, self.Model)
	self.SVFlags = SVF_NOCLIENT
}

// multi_wait: the wait time has passed, so set back up for another activation
// C: game/g_trigger.c:36 multi_wait
func (g *Game) multi_wait(ent *Edict) {
	ent.Nextthink = 0
}

// multi_trigger: the trigger was just activated
// ent->activator should be set to the activator so it can be held through a delay
// so wait for the delay time before firing
// C: game/g_trigger.c:45 multi_trigger
func (g *Game) multi_trigger(ent *Edict) {
	if ent.Nextthink != 0 {
		return // already been triggered
	}

	g.G_UseTargets(ent, ent.Activator)

	if ent.Wait > 0 {
		ent.Think = multi_wait
		ent.Nextthink = g.level.Time + ent.Wait
	} else { // we can't just remove (self) here, because this is a touch function
		// called while looping through area links...
		ent.Touch = nil
		ent.Nextthink = float32(float64(g.level.Time) + FRAMETIME)
		ent.Think = G_FreeEdict
	}
}

// C: game/g_trigger.c:66 Use_Multi
func (g *Game) Use_Multi(ent, other, activator *Edict) {
	ent.Activator = activator
	g.multi_trigger(ent)
}

// C: game/g_trigger.c:72 Touch_Multi
func (g *Game) Touch_Multi(self, other *Edict, plane *CPlane, surf *CSurface) {
	if other.Client != nil {
		if self.Spawnflags&2 != 0 {
			return
		}
	} else if other.SVFlags&SVF_MONSTER != 0 {
		if self.Spawnflags&1 == 0 {
			return
		}
	} else {
		return
	}

	if shared.VectorCompare(self.Movedir, shared.Vec3Origin) == 0 {
		var forward Vec3

		shared.AngleVectors(other.S.Angles, &forward, nil, nil)
		if shared.DotProduct(forward, self.Movedir) < 0 {
			return
		}
	}

	self.Activator = other
	g.multi_trigger(self)
}

// C: game/g_trigger.c:111 trigger_enable
func (g *Game) trigger_enable(self, other, activator *Edict) {
	self.Solid = SOLID_TRIGGER
	self.Use = Use_Multi
	g.gi.LinkEntity(self)
}

// C: game/g_trigger.c:118 SP_trigger_multiple
func (g *Game) SP_trigger_multiple(ent *Edict) {
	if ent.Sounds == 1 {
		ent.NoiseIndex = int32(g.gi.SoundIndex("misc/secret.wav"))
	} else if ent.Sounds == 2 {
		ent.NoiseIndex = int32(g.gi.SoundIndex("misc/talk.wav"))
	} else if ent.Sounds == 3 {
		ent.NoiseIndex = int32(g.gi.SoundIndex("misc/trigger1.wav"))
	}

	if ent.Wait == 0 {
		ent.Wait = 0.2
	}
	ent.Touch = Touch_Multi
	ent.Movetype = MOVETYPE_NONE
	ent.SVFlags |= SVF_NOCLIENT

	if ent.Spawnflags&4 != 0 {
		ent.Solid = SOLID_NOT
		ent.Use = trigger_enable
	} else {
		ent.Solid = SOLID_TRIGGER
		ent.Use = Use_Multi
	}

	if shared.VectorCompare(ent.S.Angles, shared.Vec3Origin) == 0 {
		G_SetMovedir(&ent.S.Angles, &ent.Movedir)
	}

	g.gi.SetModel(ent, ent.Model)
	g.gi.LinkEntity(ent)
}

// C: game/g_trigger.c:168 SP_trigger_once
func (g *Game) SP_trigger_once(ent *Edict) {
	// make old maps work because I messed up on flag assignments here
	// triggered was on bit 1 when it should have been on bit 4
	if ent.Spawnflags&1 != 0 {
		v := shared.VectorMA(ent.Mins, 0.5, ent.Size)
		ent.Spawnflags &^= 1
		ent.Spawnflags |= 4
		g.dprintf("fixed TRIGGERED flag on %s at %s\n", ent.Classname, vtos(v))
	}

	ent.Wait = -1
	g.SP_trigger_multiple(ent)
}

// C: game/g_trigger.c:189 trigger_relay_use
func (g *Game) trigger_relay_use(self, other, activator *Edict) {
	g.G_UseTargets(self, activator)
}

// C: game/g_trigger.c:194 SP_trigger_relay
func (g *Game) SP_trigger_relay(self *Edict) {
	self.Use = trigger_relay_use
}

// C: game/g_trigger.c:212 trigger_key_use
func (g *Game) trigger_key_use(self, other, activator *Edict) {
	var index int32

	if self.Item == nil {
		return
	}
	if activator.Client == nil {
		return
	}

	index = ITEM_INDEX(self.Item)
	if activator.Client.Pers.Inventory[index] == 0 {
		if g.level.Time < self.TouchDebounceTime {
			return
		}
		self.TouchDebounceTime = float32(float64(g.level.Time) + 5.0)
		g.centerprintf(activator, "You need the %s", self.Item.PickupName)
		g.gi.Sound(activator, CHAN_AUTO, g.gi.SoundIndex("misc/keytry.wav"), 1, ATTN_NORM, 0)
		return
	}

	g.gi.Sound(activator, CHAN_AUTO, g.gi.SoundIndex("misc/keyuse.wav"), 1, ATTN_NORM, 0)
	if g.coop.Value != 0 {
		var ent *Edict

		if self.Item.Classname == "key_power_cube" {
			var cube uint

			for cube = 0; cube < 8; cube++ {
				if activator.Client.Pers.PowerCubes&(1<<cube) != 0 {
					break
				}
			}
			for player := int32(1); player <= g.game.Maxclients; player++ {
				ent = &g.edicts[player]
				if !ent.InUse {
					continue
				}
				if ent.Client == nil {
					continue
				}
				if ent.Client.Pers.PowerCubes&(1<<cube) != 0 {
					ent.Client.Pers.Inventory[index]--
					ent.Client.Pers.PowerCubes &^= 1 << cube
				}
			}
		} else {
			for player := int32(1); player <= g.game.Maxclients; player++ {
				ent = &g.edicts[player]
				if !ent.InUse {
					continue
				}
				if ent.Client == nil {
					continue
				}
				ent.Client.Pers.Inventory[index] = 0
			}
		}
	} else {
		activator.Client.Pers.Inventory[index]--
	}

	g.G_UseTargets(self, activator)

	self.Use = nil
}

// C: game/g_trigger.c:282 SP_trigger_key
func (g *Game) SP_trigger_key(self *Edict) {
	if g.st.Item == "" {
		g.dprintf("no key item for trigger_key at %s\n", vtos(self.S.Origin))
		return
	}
	self.Item = g.FindItemByClassname(g.st.Item)

	if self.Item == nil {
		g.dprintf("item %s not found for trigger_key at %s\n", g.st.Item, vtos(self.S.Origin))
		return
	}

	if self.Target == "" {
		g.dprintf("%s at %s has no target\n", self.Classname, vtos(self.S.Origin))
		return
	}

	g.gi.SoundIndex("misc/keytry.wav")
	g.gi.SoundIndex("misc/keyuse.wav")

	self.Use = trigger_key_use
}

// C: game/g_trigger.c:326 trigger_counter_use
func (g *Game) trigger_counter_use(self, other, activator *Edict) {
	if self.Count == 0 {
		return
	}

	self.Count--

	if self.Count != 0 {
		if self.Spawnflags&1 == 0 {
			g.centerprintf(activator, "%i more to go...", self.Count)
			g.gi.Sound(activator, CHAN_AUTO, g.gi.SoundIndex("misc/talk1.wav"), 1, ATTN_NORM, 0)
		}
		return
	}

	if self.Spawnflags&1 == 0 {
		g.gi.Centerprintf(activator, "Sequence completed!")
		g.gi.Sound(activator, CHAN_AUTO, g.gi.SoundIndex("misc/talk1.wav"), 1, ATTN_NORM, 0)
	}
	self.Activator = activator
	g.multi_trigger(self)
}

// C: game/g_trigger.c:352 SP_trigger_counter
func (g *Game) SP_trigger_counter(self *Edict) {
	self.Wait = -1
	if self.Count == 0 {
		self.Count = 2
	}

	self.Use = trigger_counter_use
}

// C: game/g_trigger.c:373 SP_trigger_always
func (g *Game) SP_trigger_always(ent *Edict) {
	// we must have some delay to make sure our use targets are present
	if float64(ent.Delay) < 0.2 {
		ent.Delay = 0.2
	}
	g.G_UseTargets(ent, ent)
}

// C: game/g_trigger.c:394 trigger_push_touch
func (g *Game) trigger_push_touch(self, other *Edict, plane *CPlane, surf *CSurface) {
	if other.Classname == "grenade" {
		other.Velocity = shared.VectorScale(self.Movedir, self.Speed*10)
	} else if other.Health > 0 {
		other.Velocity = shared.VectorScale(self.Movedir, self.Speed*10)

		if other.Client != nil {
			// don't take falling damage immediately from this
			other.Client.Oldvelocity = other.Velocity
			if other.FlySoundDebounceTime < g.level.Time {
				other.FlySoundDebounceTime = float32(float64(g.level.Time) + 1.5)
				g.gi.Sound(other, CHAN_AUTO, int(g.windsound), 1, ATTN_NORM, 0)
			}
		}
	}
	if self.Spawnflags&PUSH_ONCE != 0 {
		g.G_FreeEdict(self)
	}
}

// C: game/g_trigger.c:424 SP_trigger_push
func (g *Game) SP_trigger_push(self *Edict) {
	g.InitTrigger(self)
	g.windsound = int32(g.gi.SoundIndex("misc/windfly.wav"))
	self.Touch = trigger_push_touch
	if self.Speed == 0 {
		self.Speed = 1000
	}
	g.gi.LinkEntity(self)
}

// C: game/g_trigger.c:455 hurt_use
func (g *Game) hurt_use(self, other, activator *Edict) {
	if self.Solid == SOLID_NOT {
		self.Solid = SOLID_TRIGGER
	} else {
		self.Solid = SOLID_NOT
	}
	g.gi.LinkEntity(self)

	if self.Spawnflags&2 == 0 {
		self.Use = nil
	}
}

// C: game/g_trigger.c:468 hurt_touch
func (g *Game) hurt_touch(self, other *Edict, plane *CPlane, surf *CSurface) {
	var dflags int32

	if other.Takedamage == 0 {
		return
	}

	if self.Timestamp > g.level.Time {
		return
	}

	if self.Spawnflags&16 != 0 {
		self.Timestamp = g.level.Time + 1
	} else {
		self.Timestamp = float32(float64(g.level.Time) + FRAMETIME)
	}

	if self.Spawnflags&4 == 0 {
		if g.level.Framenum%10 == 0 {
			g.gi.Sound(other, CHAN_AUTO, int(self.NoiseIndex), 1, ATTN_NORM, 0)
		}
	}

	if self.Spawnflags&8 != 0 {
		dflags = DAMAGE_NO_PROTECTION
	} else {
		dflags = 0
	}
	g.T_Damage(other, self, self, &Vec3{}, other.S.Origin, shared.Vec3Origin, self.Dmg, self.Dmg, dflags, MOD_TRIGGER_HURT)
}

// C: game/g_trigger.c:496 SP_trigger_hurt
func (g *Game) SP_trigger_hurt(self *Edict) {
	g.InitTrigger(self)

	self.NoiseIndex = int32(g.gi.SoundIndex("world/electro.wav"))
	self.Touch = hurt_touch

	if self.Dmg == 0 {
		self.Dmg = 5
	}

	if self.Spawnflags&1 != 0 {
		self.Solid = SOLID_NOT
	} else {
		self.Solid = SOLID_TRIGGER
	}

	if self.Spawnflags&2 != 0 {
		self.Use = hurt_use
	}

	g.gi.LinkEntity(self)
}

// C: game/g_trigger.c:532 trigger_gravity_touch
func (g *Game) trigger_gravity_touch(self, other *Edict, plane *CPlane, surf *CSurface) {
	other.Gravity = self.Gravity
}

// C: game/g_trigger.c:537 SP_trigger_gravity
func (g *Game) SP_trigger_gravity(self *Edict) {
	if g.st.Gravity == "" {
		g.dprintf("trigger_gravity without gravity set at %s\n", vtos(self.S.Origin))
		g.G_FreeEdict(self)
		return
	}

	g.InitTrigger(self)
	self.Gravity = float32(shared.Atoi(g.st.Gravity))
	self.Touch = trigger_gravity_touch
}

// C: game/g_trigger.c:566 trigger_monsterjump_touch
func (g *Game) trigger_monsterjump_touch(self, other *Edict, plane *CPlane, surf *CSurface) {
	if other.Flags&(FL_FLY|FL_SWIM) != 0 {
		return
	}
	if other.SVFlags&SVF_DEADMONSTER != 0 {
		return
	}
	if other.SVFlags&SVF_MONSTER == 0 {
		return
	}

	// set XY even if not on ground, so the jump will clear lips
	other.Velocity[0] = self.Movedir[0] * self.Speed
	other.Velocity[1] = self.Movedir[1] * self.Speed

	if other.Groundentity == nil {
		return
	}

	other.Groundentity = nil
	other.Velocity[2] = self.Movedir[2]
}

// C: game/g_trigger.c:586 SP_trigger_monsterjump
func (g *Game) SP_trigger_monsterjump(self *Edict) {
	if self.Speed == 0 {
		self.Speed = 200
	}
	if g.st.Height == 0 {
		g.st.Height = 200
	}
	if self.S.Angles[YAW] == 0 {
		self.S.Angles[YAW] = 360
	}
	g.InitTrigger(self)
	self.Touch = trigger_monsterjump_touch
	self.Movedir[2] = float32(g.st.Height)
}
