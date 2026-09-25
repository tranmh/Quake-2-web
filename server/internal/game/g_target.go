package game

// Port of game/g_target.c.

import (
	"strings"

	. "quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/shared"
)

var (
	Use_Target_Tent                = defUse("Use_Target_Tent")
	Use_Target_Speaker             = defUse("Use_Target_Speaker")
	Use_Target_Help                = defUse("Use_Target_Help")
	use_target_secret              = defUse("use_target_secret")
	use_target_goal                = defUse("use_target_goal")
	target_explosion_explode       = defThink("target_explosion_explode")
	use_target_explosion           = defUse("use_target_explosion")
	use_target_changelevel         = defUse("use_target_changelevel")
	use_target_splash              = defUse("use_target_splash")
	use_target_spawner             = defUse("use_target_spawner")
	use_target_blaster             = defUse("use_target_blaster")
	trigger_crosslevel_trigger_use = defUse("trigger_crosslevel_trigger_use")
	target_crosslevel_target_think = defThink("target_crosslevel_target_think")
	target_laser_think             = defThink("target_laser_think")
	target_laser_use               = defUse("target_laser_use")
	target_laser_start             = defThink("target_laser_start")
	target_lightramp_think         = defThink("target_lightramp_think")
	target_lightramp_use           = defUse("target_lightramp_use")
	target_earthquake_think        = defThink("target_earthquake_think")
	target_earthquake_use          = defUse("target_earthquake_use")
)

func init() {
	Use_Target_Tent.bind((*Game).Use_Target_Tent)
	Use_Target_Speaker.bind((*Game).Use_Target_Speaker)
	Use_Target_Help.bind((*Game).Use_Target_Help)
	use_target_secret.bind((*Game).use_target_secret)
	use_target_goal.bind((*Game).use_target_goal)
	target_explosion_explode.bind((*Game).target_explosion_explode)
	use_target_explosion.bind((*Game).use_target_explosion)
	use_target_changelevel.bind((*Game).use_target_changelevel)
	use_target_splash.bind((*Game).use_target_splash)
	use_target_spawner.bind((*Game).use_target_spawner)
	use_target_blaster.bind((*Game).use_target_blaster)
	trigger_crosslevel_trigger_use.bind((*Game).trigger_crosslevel_trigger_use)
	target_crosslevel_target_think.bind((*Game).target_crosslevel_target_think)
	target_laser_think.bind((*Game).target_laser_think)
	target_laser_use.bind((*Game).target_laser_use)
	target_laser_start.bind((*Game).target_laser_start)
	target_lightramp_think.bind((*Game).target_lightramp_think)
	target_lightramp_use.bind((*Game).target_lightramp_use)
	target_earthquake_think.bind((*Game).target_earthquake_think)
	target_earthquake_use.bind((*Game).target_earthquake_use)
}

// targetLaserFlag is the C int bit 0x80000000 stored in spawnflags.
const targetLaserFlag = -0x80000000

// targetU32 reinterprets a 32-bit pattern as C int (for skinnum colors).
func targetU32(v uint32) int32 { return int32(v) }

// targetStrncpy emulates strncpy(dst, src, n-1) into a char[n] buffer.
func targetStrncpy(s string, n int) string {
	if len(s) > n-1 {
		return s[:n-1]
	}
	return s
}

// C: game/g_target.c:26 Use_Target_Tent
func (g *Game) Use_Target_Tent(ent, other, activator *Edict) {
	g.gi.WriteByteC(svc_temp_entity)
	g.gi.WriteByteC(int(ent.Style))
	g.gi.WritePosition(&ent.S.Origin)
	g.gi.Multicast(&ent.S.Origin, MULTICAST_PVS)
}

// C: game/g_target.c:34 SP_target_temp_entity
func (g *Game) SP_target_temp_entity(ent *Edict) {
	ent.Use = Use_Target_Tent
}

// C: game/g_target.c:58 Use_Target_Speaker
func (g *Game) Use_Target_Speaker(ent, other, activator *Edict) {
	var channel int

	if ent.Spawnflags&3 != 0 { // looping sound toggles
		if ent.S.Sound != 0 {
			ent.S.Sound = 0 // turn it off
		} else {
			ent.S.Sound = ent.NoiseIndex // start it
		}
	} else { // normal sound
		if ent.Spawnflags&4 != 0 {
			channel = CHAN_VOICE | CHAN_RELIABLE
		} else {
			channel = CHAN_VOICE
		}
		// use a positioned_sound, because this entity won't normally be
		// sent to any clients because it is invisible
		g.gi.PositionedSound(&ent.S.Origin, ent, channel, int(ent.NoiseIndex), ent.Volume, ent.Attenuation, 0)
	}
}

// C: game/g_target.c:81 SP_target_speaker
func (g *Game) SP_target_speaker(ent *Edict) {
	var buffer string

	if g.st.Noise == "" {
		g.dprintf("target_speaker with no noise set at %s\n", vtos(ent.S.Origin))
		return
	}
	if !strings.Contains(g.st.Noise, ".wav") {
		buffer = targetStrncpy(g.st.Noise+".wav", MAX_QPATH)
	} else {
		buffer = targetStrncpy(g.st.Noise, MAX_QPATH)
	}
	ent.NoiseIndex = int32(g.gi.SoundIndex(buffer))

	if ent.Volume == 0 {
		ent.Volume = 1.0
	}

	if ent.Attenuation == 0 {
		ent.Attenuation = 1.0
	} else if ent.Attenuation == -1 { // use -1 so 0 defaults to 1
		ent.Attenuation = 0
	}

	// check for prestarted looping sound
	if ent.Spawnflags&1 != 0 {
		ent.S.Sound = ent.NoiseIndex
	}

	ent.Use = Use_Target_Speaker

	// must link the entity so we get areas and clusters so
	// the server can determine who to send updates to
	g.gi.LinkEntity(ent)
}

// C: game/g_target.c:118 Use_Target_Help
func (g *Game) Use_Target_Help(ent, other, activator *Edict) {
	if ent.Spawnflags&1 != 0 {
		g.game.Helpmessage1 = targetStrncpy(ent.Message, 512)
	} else {
		g.game.Helpmessage2 = targetStrncpy(ent.Message, 512)
	}

	g.game.Helpchanged++
}

// C: game/g_target.c:131 SP_target_help
func (g *Game) SP_target_help(ent *Edict) {
	if g.deathmatch.Value != 0 { // auto-remove for deathmatch
		g.G_FreeEdict(ent)
		return
	}

	if ent.Message == "" {
		g.dprintf("%s with no message at %s\n", ent.Classname, vtos(ent.S.Origin))
		g.G_FreeEdict(ent)
		return
	}
	ent.Use = Use_Target_Help
}

// C: game/g_target.c:154 use_target_secret
func (g *Game) use_target_secret(ent, other, activator *Edict) {
	g.gi.Sound(ent, CHAN_VOICE, int(ent.NoiseIndex), 1, ATTN_NORM, 0)

	g.level.FoundSecrets++

	g.G_UseTargets(ent, activator)
	g.G_FreeEdict(ent)
}

// C: game/g_target.c:164 SP_target_secret
func (g *Game) SP_target_secret(ent *Edict) {
	if g.deathmatch.Value != 0 { // auto-remove for deathmatch
		g.G_FreeEdict(ent)
		return
	}

	ent.Use = use_target_secret
	if g.st.Noise == "" {
		g.st.Noise = "misc/secret.wav"
	}
	ent.NoiseIndex = int32(g.gi.SoundIndex(g.st.Noise))
	ent.SVFlags = SVF_NOCLIENT
	g.level.TotalSecrets++
	// map bug hack
	if shared.Q_stricmp(g.level.Mapname, "mine3") == 0 && ent.S.Origin[0] == 280 && ent.S.Origin[1] == -2048 && ent.S.Origin[2] == -624 {
		ent.Message = "You have found a secret area."
	}
}

// C: game/g_target.c:189 use_target_goal
func (g *Game) use_target_goal(ent, other, activator *Edict) {
	g.gi.Sound(ent, CHAN_VOICE, int(ent.NoiseIndex), 1, ATTN_NORM, 0)

	g.level.FoundGoals++

	if g.level.FoundGoals == g.level.TotalGoals {
		g.gi.Configstring(CS_CDTRACK, "0")
	}

	g.G_UseTargets(ent, activator)
	g.G_FreeEdict(ent)
}

// C: game/g_target.c:202 SP_target_goal
func (g *Game) SP_target_goal(ent *Edict) {
	if g.deathmatch.Value != 0 { // auto-remove for deathmatch
		g.G_FreeEdict(ent)
		return
	}

	ent.Use = use_target_goal
	if g.st.Noise == "" {
		g.st.Noise = "misc/secret.wav"
	}
	ent.NoiseIndex = int32(g.gi.SoundIndex(g.st.Noise))
	ent.SVFlags = SVF_NOCLIENT
	g.level.TotalGoals++
}

// C: game/g_target.c:227 target_explosion_explode
func (g *Game) target_explosion_explode(self *Edict) {
	var save float32

	g.gi.WriteByteC(svc_temp_entity)
	g.gi.WriteByteC(TE_EXPLOSION1)
	g.gi.WritePosition(&self.S.Origin)
	g.gi.Multicast(&self.S.Origin, MULTICAST_PHS)

	g.T_RadiusDamage(self, self.Activator, float32(self.Dmg), nil, float32(self.Dmg+40), MOD_EXPLOSIVE)

	save = self.Delay
	self.Delay = 0
	g.G_UseTargets(self, self.Activator)
	self.Delay = save
}

// C: game/g_target.c:244 use_target_explosion
func (g *Game) use_target_explosion(self, other, activator *Edict) {
	self.Activator = activator

	if self.Delay == 0 {
		g.target_explosion_explode(self)
		return
	}

	self.Think = target_explosion_explode
	self.Nextthink = g.level.Time + self.Delay
}

// C: game/g_target.c:258 SP_target_explosion
func (g *Game) SP_target_explosion(ent *Edict) {
	ent.Use = use_target_explosion
	ent.SVFlags = SVF_NOCLIENT
}

// C: game/g_target.c:270 use_target_changelevel
func (g *Game) use_target_changelevel(self, other, activator *Edict) {
	if g.level.Intermissiontime != 0 {
		return // already activated
	}

	if g.deathmatch.Value == 0 && g.coop.Value == 0 {
		if g.edicts[1].Health <= 0 {
			return
		}
	}

	// if noexit, do a ton of damage to other
	if g.deathmatch.Value != 0 && int32(g.dmflags.Value)&DF_ALLOW_EXIT == 0 && other != g.world() {
		g.T_Damage(other, self, self, &Vec3{}, other.S.Origin, shared.Vec3Origin, 10*other.MaxHealth, 1000, 0, MOD_EXIT)
		return
	}

	// if multiplayer, let everyone know who hit the exit
	if g.deathmatch.Value != 0 {
		if activator != nil && activator.Client != nil {
			g.bprintf(PRINT_HIGH, "%s exited the level.\n", activator.Client.Pers.Netname)
		}
	}

	// if going to a new unit, clear cross triggers
	if strings.Contains(self.Map, "*") {
		g.game.Serverflags &^= SFL_CROSS_TRIGGER_MASK
	}

	g.BeginIntermission(self)
}

// C: game/g_target.c:302 SP_target_changelevel
func (g *Game) SP_target_changelevel(ent *Edict) {
	if ent.Map == "" {
		g.dprintf("target_changelevel with no map at %s\n", vtos(ent.S.Origin))
		g.G_FreeEdict(ent)
		return
	}

	// ugly hack because *SOMEBODY* screwed up their map
	if shared.Q_stricmp(g.level.Mapname, "fact1") == 0 && shared.Q_stricmp(ent.Map, "fact3") == 0 {
		ent.Map = "fact3$secret1"
	}

	ent.Use = use_target_changelevel
	ent.SVFlags = SVF_NOCLIENT
}

// C: game/g_target.c:338 use_target_splash
func (g *Game) use_target_splash(self, other, activator *Edict) {
	g.gi.WriteByteC(svc_temp_entity)
	g.gi.WriteByteC(TE_SPLASH)
	g.gi.WriteByteC(int(self.Count))
	g.gi.WritePosition(&self.S.Origin)
	g.gi.WriteDir(&self.Movedir)
	g.gi.WriteByteC(int(self.Sounds))
	g.gi.Multicast(&self.S.Origin, MULTICAST_PVS)

	if self.Dmg != 0 {
		g.T_RadiusDamage(self, activator, float32(self.Dmg), nil, float32(self.Dmg+40), MOD_SPLASH)
	}
}

// C: game/g_target.c:352 SP_target_splash
func (g *Game) SP_target_splash(self *Edict) {
	self.Use = use_target_splash
	G_SetMovedir(&self.S.Angles, &self.Movedir)

	if self.Count == 0 {
		self.Count = 32
	}

	self.SVFlags = SVF_NOCLIENT
}

// C: game/g_target.c:380 use_target_spawner
func (g *Game) use_target_spawner(self, other, activator *Edict) {
	ent := g.G_Spawn()
	ent.Classname = self.Target
	ent.S.Origin = self.S.Origin
	ent.S.Angles = self.S.Angles
	g.ED_CallSpawn(ent)
	g.gi.UnlinkEntity(ent)
	g.KillBox(ent)
	g.gi.LinkEntity(ent)
	if self.Speed != 0 {
		ent.Velocity = self.Movedir
	}
}

// C: game/g_target.c:396 SP_target_spawner
func (g *Game) SP_target_spawner(self *Edict) {
	self.Use = use_target_spawner
	self.SVFlags = SVF_NOCLIENT
	if self.Speed != 0 {
		G_SetMovedir(&self.S.Angles, &self.Movedir)
		self.Movedir = shared.VectorScale(self.Movedir, self.Speed)
	}
}

// C: game/g_target.c:416 use_target_blaster
func (g *Game) use_target_blaster(self, other, activator *Edict) {
	var effect int32

	if self.Spawnflags&2 != 0 {
		effect = 0
	} else if self.Spawnflags&1 != 0 {
		effect = EF_HYPERBLASTER
	} else {
		effect = EF_BLASTER
	}
	_ = effect // computed but unused in C (EF_BLASTER is passed)

	g.fire_blaster(self, self.S.Origin, self.Movedir, self.Dmg, int32(self.Speed), EF_BLASTER, MOD_TARGET_BLASTER != 0)
	g.gi.Sound(self, CHAN_VOICE, int(self.NoiseIndex), 1, ATTN_NORM, 0)
}

// C: game/g_target.c:431 SP_target_blaster
func (g *Game) SP_target_blaster(self *Edict) {
	self.Use = use_target_blaster
	G_SetMovedir(&self.S.Angles, &self.Movedir)
	self.NoiseIndex = int32(g.gi.SoundIndex("weapons/laser2.wav"))

	if self.Dmg == 0 {
		self.Dmg = 15
	}
	if self.Speed == 0 {
		self.Speed = 1000
	}

	self.SVFlags = SVF_NOCLIENT
}

// C: game/g_target.c:451 trigger_crosslevel_trigger_use
func (g *Game) trigger_crosslevel_trigger_use(self, other, activator *Edict) {
	g.game.Serverflags |= self.Spawnflags
	g.G_FreeEdict(self)
}

// C: game/g_target.c:457 SP_target_crosslevel_trigger
func (g *Game) SP_target_crosslevel_trigger(self *Edict) {
	self.SVFlags = SVF_NOCLIENT
	self.Use = trigger_crosslevel_trigger_use
}

// C: game/g_target.c:469 target_crosslevel_target_think
func (g *Game) target_crosslevel_target_think(self *Edict) {
	if self.Spawnflags == (g.game.Serverflags & SFL_CROSS_TRIGGER_MASK & self.Spawnflags) {
		g.G_UseTargets(self, self)
		g.G_FreeEdict(self)
	}
}

// C: game/g_target.c:478 SP_target_crosslevel_target
func (g *Game) SP_target_crosslevel_target(self *Edict) {
	if self.Delay == 0 {
		self.Delay = 1
	}
	self.SVFlags = SVF_NOCLIENT

	self.Think = target_crosslevel_target_think
	self.Nextthink = g.level.Time + self.Delay
}

// C: game/g_target.c:495 target_laser_think
func (g *Game) target_laser_think(self *Edict) {
	var ignore *Edict
	var start, end, point, lastMovedir Vec3
	var tr Trace
	var count int32

	if self.Spawnflags&targetLaserFlag != 0 {
		count = 8
	} else {
		count = 4
	}

	if self.Enemy != nil {
		lastMovedir = self.Movedir
		point = shared.VectorMA(self.Enemy.AbsMin, 0.5, self.Enemy.Size)
		self.Movedir = shared.VectorSubtract(point, self.S.Origin)
		shared.VectorNormalize(&self.Movedir)
		if shared.VectorCompare(self.Movedir, lastMovedir) == 0 {
			self.Spawnflags |= targetLaserFlag
		}
	}

	ignore = self
	start = self.S.Origin
	end = shared.VectorMA(start, 2048, self.Movedir)
	for {
		tr = g.gi.Trace(&start, nil, nil, &end, ignore, CONTENTS_SOLID|CONTENTS_MONSTER|CONTENTS_DEADMONSTER)

		if tr.Ent == nil {
			break
		}

		// hurt it if we can
		if tr.Ent.Takedamage != 0 && tr.Ent.Flags&FL_IMMUNE_LASER == 0 {
			g.T_Damage(tr.Ent, self, self.Activator, &self.Movedir, tr.EndPos, shared.Vec3Origin, self.Dmg, 1, DAMAGE_ENERGY, MOD_TARGET_LASER)
		}

		// if we hit something that's not a monster or player or is immune to lasers, we're done
		if tr.Ent.SVFlags&SVF_MONSTER == 0 && tr.Ent.Client == nil {
			if self.Spawnflags&targetLaserFlag != 0 {
				self.Spawnflags &^= targetLaserFlag
				g.gi.WriteByteC(svc_temp_entity)
				g.gi.WriteByteC(TE_LASER_SPARKS)
				g.gi.WriteByteC(int(count))
				g.gi.WritePosition(&tr.EndPos)
				g.gi.WriteDir(&tr.Plane.Normal)
				g.gi.WriteByteC(int(self.S.SkinNum))
				g.gi.Multicast(&tr.EndPos, MULTICAST_PVS)
			}
			break
		}

		ignore = tr.Ent
		start = tr.EndPos
	}

	self.S.OldOrigin = tr.EndPos

	self.Nextthink = float32(float64(g.level.Time) + FRAMETIME)
}

// C: game/g_target.c:560 target_laser_on
func (g *Game) target_laser_on(self *Edict) {
	if self.Activator == nil {
		self.Activator = self
	}
	self.Spawnflags |= targetLaserFlag | 1
	self.SVFlags &^= SVF_NOCLIENT
	g.target_laser_think(self)
}

// C: game/g_target.c:569 target_laser_off
func (g *Game) target_laser_off(self *Edict) {
	self.Spawnflags &^= 1
	self.SVFlags |= SVF_NOCLIENT
	self.Nextthink = 0
}

// C: game/g_target.c:576 target_laser_use
func (g *Game) target_laser_use(self, other, activator *Edict) {
	self.Activator = activator
	if self.Spawnflags&1 != 0 {
		g.target_laser_off(self)
	} else {
		g.target_laser_on(self)
	}
}

// C: game/g_target.c:585 target_laser_start
func (g *Game) target_laser_start(self *Edict) {
	self.Movetype = MOVETYPE_NONE
	self.Solid = SOLID_NOT
	self.S.RenderFX |= RF_BEAM | RF_TRANSLUCENT
	self.S.ModelIndex = 1 // must be non-zero

	// set the beam diameter
	if self.Spawnflags&64 != 0 {
		self.S.Frame = 16
	} else {
		self.S.Frame = 4
	}

	// set the color
	if self.Spawnflags&2 != 0 {
		self.S.SkinNum = targetU32(0xf2f2f0f0)
	} else if self.Spawnflags&4 != 0 {
		self.S.SkinNum = targetU32(0xd0d1d2d3)
	} else if self.Spawnflags&8 != 0 {
		self.S.SkinNum = targetU32(0xf3f3f1f1)
	} else if self.Spawnflags&16 != 0 {
		self.S.SkinNum = targetU32(0xdcdddedf)
	} else if self.Spawnflags&32 != 0 {
		self.S.SkinNum = targetU32(0xe0e1e2e3)
	}

	if self.Enemy == nil {
		if self.Target != "" {
			ent := g.G_Find(nil, FOFS_targetname, self.Target)
			if ent == nil {
				g.dprintf("%s at %s: %s is a bad target\n", self.Classname, vtos(self.S.Origin), self.Target)
			}
			self.Enemy = ent
		} else {
			G_SetMovedir(&self.S.Angles, &self.Movedir)
		}
	}
	self.Use = target_laser_use
	self.Think = target_laser_think

	if self.Dmg == 0 {
		self.Dmg = 1
	}

	self.Mins = Vec3{-8, -8, -8}
	self.Maxs = Vec3{8, 8, 8}
	g.gi.LinkEntity(self)

	if self.Spawnflags&1 != 0 {
		g.target_laser_on(self)
	} else {
		g.target_laser_off(self)
	}
}

// C: game/g_target.c:642 SP_target_laser
func (g *Game) SP_target_laser(self *Edict) {
	// let everything else get spawned before we start firing
	self.Think = target_laser_start
	self.Nextthink = g.level.Time + 1
}

// C: game/g_target.c:656 target_lightramp_think
func (g *Game) target_lightramp_think(self *Edict) {
	v := float64(float32('a')+self.Movedir[0]) + float64(g.level.Time-self.Timestamp)/FRAMETIME*float64(self.Movedir[2])
	c := byte(int8(int32(v)))
	style := ""
	if c != 0 {
		style = string([]byte{c})
	}
	g.gi.Configstring(CS_LIGHTS+int(self.Enemy.Style), style)

	if (g.level.Time - self.Timestamp) < self.Speed {
		self.Nextthink = float32(float64(g.level.Time) + FRAMETIME)
	} else if self.Spawnflags&1 != 0 {
		var temp int8

		temp = int8(int32(self.Movedir[0]))
		self.Movedir[0] = self.Movedir[1]
		self.Movedir[1] = float32(temp)
		self.Movedir[2] *= -1
	}
}

// C: game/g_target.c:679 target_lightramp_use
func (g *Game) target_lightramp_use(self, other, activator *Edict) {
	if self.Enemy == nil {
		var e *Edict

		// check all the targets
		e = nil
		for {
			e = g.G_Find(e, FOFS_targetname, self.Target)
			if e == nil {
				break
			}
			if e.Classname != "light" {
				g.dprintf("%s at %s ", self.Classname, vtos(self.S.Origin))
				g.dprintf("target %s (%s at %s) is not a light\n", self.Target, e.Classname, vtos(e.S.Origin))
			} else {
				self.Enemy = e
			}
		}

		if self.Enemy == nil {
			g.dprintf("%s target %s not found at %s\n", self.Classname, self.Target, vtos(self.S.Origin))
			g.G_FreeEdict(self)
			return
		}
	}

	self.Timestamp = g.level.Time
	g.target_lightramp_think(self)
}

// C: game/g_target.c:715 SP_target_lightramp
func (g *Game) SP_target_lightramp(self *Edict) {
	m := self.Message
	if m == "" || len(m) != 2 || m[0] < 'a' || m[0] > 'z' || m[1] < 'a' || m[1] > 'z' || m[0] == m[1] {
		msg := m
		if msg == "" {
			msg = "(null)" // glibc prints a NULL %s as (null)
		}
		g.dprintf("target_lightramp has bad ramp (%s) at %s\n", msg, vtos(self.S.Origin))
		g.G_FreeEdict(self)
		return
	}

	if g.deathmatch.Value != 0 {
		g.G_FreeEdict(self)
		return
	}

	if self.Target == "" {
		g.dprintf("%s with no target at %s\n", self.Classname, vtos(self.S.Origin))
		g.G_FreeEdict(self)
		return
	}

	self.SVFlags |= SVF_NOCLIENT
	self.Use = target_lightramp_use
	self.Think = target_lightramp_think

	self.Movedir[0] = float32(int32(m[0]) - 'a')
	self.Movedir[1] = float32(int32(m[1]) - 'a')
	self.Movedir[2] = float32(float64(self.Movedir[1]-self.Movedir[0]) / (float64(self.Speed) / FRAMETIME))
}

// C: game/g_target.c:755 target_earthquake_think
func (g *Game) target_earthquake_think(self *Edict) {
	if self.LastMoveTime < g.level.Time {
		g.gi.PositionedSound(&self.S.Origin, self, CHAN_AUTO, int(self.NoiseIndex), 1.0, ATTN_NONE, 0)
		self.LastMoveTime = float32(float64(g.level.Time) + 0.5)
	}

	for i := 1; i < int(g.num_edicts); i++ {
		e := &g.edicts[i]
		if !e.InUse {
			continue
		}
		if e.Client == nil {
			continue
		}
		if e.Groundentity == nil {
			continue
		}

		e.Groundentity = nil
		e.Velocity[0] = float32(float64(e.Velocity[0]) + g.crandom()*150)
		e.Velocity[1] = float32(float64(e.Velocity[1]) + g.crandom()*150)
		e.Velocity[2] = float32(float64(self.Speed) * (100.0 / float64(e.Mass)))
	}

	if g.level.Time < self.Timestamp {
		self.Nextthink = float32(float64(g.level.Time) + FRAMETIME)
	}
}

// C: game/g_target.c:785 target_earthquake_use
func (g *Game) target_earthquake_use(self, other, activator *Edict) {
	self.Timestamp = g.level.Time + float32(self.Count)
	self.Nextthink = float32(float64(g.level.Time) + FRAMETIME)
	self.Activator = activator
	self.LastMoveTime = 0
}

// C: game/g_target.c:793 SP_target_earthquake
func (g *Game) SP_target_earthquake(self *Edict) {
	if self.Targetname == "" {
		g.dprintf("untargeted %s at %s\n", self.Classname, vtos(self.S.Origin))
	}

	if self.Count == 0 {
		self.Count = 5
	}

	if self.Speed == 0 {
		self.Speed = 200
	}

	self.SVFlags |= SVF_NOCLIENT
	self.Think = target_earthquake_think
	self.Use = target_earthquake_use

	self.NoiseIndex = int32(g.gi.SoundIndex("world/quake.wav"))
}
