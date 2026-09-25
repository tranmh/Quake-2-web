package game

// Port of game/g_monster.c, plus the registration helpers for the monster
// files (m_*.go).
//
// Pattern for porting a monster file m_<name>.c (they can be added
// independently of each other):
//
//   - Frame constants of m_<name>.h are prefixed with the monster name,
//     because the frame names of different monster headers collide in one Go
//     package: FRAME_stand101 in m_soldier.h -> soldier_FRAME_stand101.
//     (m_player.h keeps its unprefixed names in m_player.go.)
//   - The file statics (`static int sound_idle;` ...) go into one struct type
//     per file, registered from init() and fetched per game instance:
//
//         type soldierStatics struct{ SoundIdle, SoundSight1 int32 }
//         func init() {
//             RegisterMonsterStatics("m_soldier", func() any { return new(soldierStatics) })
//         }
//         ...
//         s := monsterStatics[soldierStatics](g, "m_soldier")
//         g.gi.Sound(self, CHAN_VOICE, int(s.SoundIdle), 1, ATTN_IDLE, 0)
//
//     Struct fields are the C variable names in mechanical CamelCase
//     (sound_idle -> SoundIdle), exported so the level save can marshal
//     them (the constructor registry lets the loader recreate the type).
//   - Every function stored in a pointer gets a handle (defThink, defPain,
//     defDie, defAI ..., bound in init()); mmove tables are
//     `var soldier_move_stand1 = defMMove("soldier_move_stand1", ...)`.
//   - Spawn functions register their classname from init():
//         func init() { RegisterSpawn("monster_soldier", (*Game).SP_monster_soldier) }
//     ED_CallSpawn (g_spawn.go) looks the classname up there.

import (
	"math"

	. "quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/shared"
)

// monsterStaticsNew holds the constructors of the per-file statics structs
// of the monster files, filled from init() and read-only afterwards.
var monsterStaticsNew = map[string]func() any{}

// RegisterMonsterStatics registers the constructor of the statics struct of
// a monster file (file is the C file stem, e.g. "m_soldier"). Called from init().
func RegisterMonsterStatics(file string, newFn func() any) {
	if _, dup := monsterStaticsNew[file]; dup {
		panic("game: duplicate monster statics: " + file)
	}
	monsterStaticsNew[file] = newFn
}

// monsterStatics returns the statics struct of a monster file for this game
// instance, creating it on first use with the registered constructor.
func monsterStatics[T any](g *Game, file string) *T {
	if v, ok := g.mstatics[file]; ok {
		return v.(*T)
	}
	var v any
	if newFn, ok := monsterStaticsNew[file]; ok {
		v = newFn()
	} else {
		v = new(T)
	}
	if g.mstatics == nil {
		g.mstatics = map[string]any{}
	}
	g.mstatics[file] = v
	return v.(*T)
}

// Function handles of g_monster.c.
var (
	M_FliesOff                  = defThink("M_FliesOff")
	M_FliesOn                   = defThink("M_FliesOn")
	M_droptofloor               = defThink("M_droptofloor")
	monster_think               = defThink("monster_think")
	monster_use                 = defUse("monster_use")
	monster_triggered_spawn     = defThink("monster_triggered_spawn")
	monster_triggered_spawn_use = defUse("monster_triggered_spawn_use")
	walkmonster_start_go        = defThink("walkmonster_start_go")
	flymonster_start_go         = defThink("flymonster_start_go")
	swimmonster_start_go        = defThink("swimmonster_start_go")
)

func init() {
	M_FliesOff.bind((*Game).M_FliesOff)
	M_FliesOn.bind((*Game).M_FliesOn)
	M_droptofloor.bind((*Game).M_droptofloor)
	monster_think.bind((*Game).monster_think)
	monster_use.bind((*Game).monster_use)
	monster_triggered_spawn.bind((*Game).monster_triggered_spawn)
	monster_triggered_spawn_use.bind((*Game).monster_triggered_spawn_use)
	walkmonster_start_go.bind((*Game).walkmonster_start_go)
	flymonster_start_go.bind((*Game).flymonster_start_go)
	swimmonster_start_go.bind((*Game).swimmonster_start_go)
}

//
// monster weapons
//

// monsterMuzzleflash2 writes the svc_muzzleflash2 message shared by the monster_fire_* functions.
func (g *Game) monsterMuzzleflash2(self *Edict, start Vec3, flashtype int32) {
	g.gi.WriteByteC(svc_muzzleflash2)
	g.gi.WriteShort(self.Index)
	g.gi.WriteByteC(int(flashtype))
	g.gi.Multicast(&start, MULTICAST_PVS)
}

// FIXME mosnters should call these with a totally accurate direction
// and we can mess it up based on skill.  Spread should be for normal
// and we can tighten or loosen based on skill.  We could muck with
// the damages too, but I'm not sure that's such a good idea.
// C: game/g_monster.c:31 monster_fire_bullet
func (g *Game) monster_fire_bullet(self *Edict, start, dir Vec3, damage, kick, hspread, vspread, flashtype int32) {
	g.fire_bullet(self, start, dir, damage, kick, hspread, vspread, MOD_UNKNOWN)
	g.monsterMuzzleflash2(self, start, flashtype)
}

// C: game/g_monster.c:41 monster_fire_shotgun
func (g *Game) monster_fire_shotgun(self *Edict, start, aimdir Vec3, damage, kick, hspread, vspread, count, flashtype int32) {
	g.fire_shotgun(self, start, aimdir, damage, kick, hspread, vspread, count, MOD_UNKNOWN)
	g.monsterMuzzleflash2(self, start, flashtype)
}

// C: game/g_monster.c:51 monster_fire_blaster
func (g *Game) monster_fire_blaster(self *Edict, start, dir Vec3, damage, speed, flashtype, effect int32) {
	g.fire_blaster(self, start, dir, damage, speed, effect, false)
	g.monsterMuzzleflash2(self, start, flashtype)
}

// C: game/g_monster.c:61 monster_fire_grenade
func (g *Game) monster_fire_grenade(self *Edict, start, aimdir Vec3, damage, speed, flashtype int32) {
	g.fire_grenade(self, start, aimdir, damage, speed, 2.5, float32(damage+40))
	g.monsterMuzzleflash2(self, start, flashtype)
}

// C: game/g_monster.c:71 monster_fire_rocket
func (g *Game) monster_fire_rocket(self *Edict, start, dir Vec3, damage, speed, flashtype int32) {
	g.fire_rocket(self, start, dir, damage, speed, float32(damage+20), damage)
	g.monsterMuzzleflash2(self, start, flashtype)
}

// C: game/g_monster.c:81 monster_fire_railgun
func (g *Game) monster_fire_railgun(self *Edict, start, aimdir Vec3, damage, kick, flashtype int32) {
	g.fire_rail(self, start, aimdir, damage, kick)
	g.monsterMuzzleflash2(self, start, flashtype)
}

// C: game/g_monster.c:91 monster_fire_bfg
func (g *Game) monster_fire_bfg(self *Edict, start, aimdir Vec3, damage, speed, kick int32, damage_radius float32, flashtype int32) {
	g.fire_bfg(self, start, aimdir, damage, speed, damage_radius)
	g.monsterMuzzleflash2(self, start, flashtype)
}

//
// Monster utility functions
//

// C: game/g_monster.c:107 M_FliesOff
func (g *Game) M_FliesOff(self *Edict) {
	self.S.Effects &^= EF_FLIES
	self.S.Sound = 0
}

// C: game/g_monster.c:113 M_FliesOn
func (g *Game) M_FliesOn(self *Edict) {
	if self.Waterlevel != 0 {
		return
	}
	self.S.Effects |= EF_FLIES
	self.S.Sound = int32(g.gi.SoundIndex("infantry/inflies1.wav"))
	self.Think = M_FliesOff
	self.Nextthink = g.level.Time + 60
}

// C: game/g_monster.c:123 M_FlyCheck
func (g *Game) M_FlyCheck(self *Edict) {
	if self.Waterlevel != 0 {
		return
	}

	if float64(g.random()) > 0.5 {
		return
	}

	self.Think = M_FliesOn
	self.Nextthink = g.level.Time + 5 + 10*g.random()
}

// C: game/g_monster.c:135 AttackFinished
func (g *Game) AttackFinished(self *Edict, time float32) {
	self.Monsterinfo.AttackFinished = g.level.Time + time
}

// C: game/g_monster.c:141 M_CheckGround
func (g *Game) M_CheckGround(ent *Edict) {
	var point Vec3

	if ent.Flags&(FL_SWIM|FL_FLY) != 0 {
		return
	}

	if ent.Velocity[2] > 100 {
		ent.Groundentity = nil
		return
	}

	// if the hull point one-quarter unit down is solid the entity is on ground
	point[0] = ent.S.Origin[0]
	point[1] = ent.S.Origin[1]
	point[2] = float32(float64(ent.S.Origin[2]) - 0.25)

	trace := g.gi.Trace(&ent.S.Origin, &ent.Mins, &ent.Maxs, &point, ent, MASK_MONSTERSOLID)

	// check steepness
	if float64(trace.Plane.Normal[2]) < 0.7 && !trace.StartSolid {
		ent.Groundentity = nil
		return
	}

	//	ent->groundentity = trace.ent;
	//	ent->groundentity_linkcount = trace.ent->linkcount;
	//	if (!trace.startsolid && !trace.allsolid)
	//		VectorCopy (trace.endpos, ent->s.origin);
	if !trace.StartSolid && !trace.AllSolid {
		ent.S.Origin = trace.EndPos
		ent.Groundentity = trace.Ent
		ent.GroundentityLinkcount = trace.Ent.LinkCount
		ent.Velocity[2] = 0
	}
}

// C: game/g_monster.c:183 M_CatagorizePosition
func (g *Game) M_CatagorizePosition(ent *Edict) {
	var point Vec3

	//
	// get waterlevel
	//
	point[0] = ent.S.Origin[0]
	point[1] = ent.S.Origin[1]
	point[2] = ent.S.Origin[2] + ent.Mins[2] + 1
	cont := g.gi.PointContents(&point)

	if cont&MASK_WATER == 0 {
		ent.Waterlevel = 0
		ent.Watertype = 0
		return
	}

	ent.Watertype = cont
	ent.Waterlevel = 1
	point[2] += 26
	cont = g.gi.PointContents(&point)
	if cont&MASK_WATER == 0 {
		return
	}

	ent.Waterlevel = 2
	point[2] += 22
	cont = g.gi.PointContents(&point)
	if cont&MASK_WATER != 0 {
		ent.Waterlevel = 3
	}
}

// C: game/g_monster.c:218 M_WorldEffects
func (g *Game) M_WorldEffects(ent *Edict) {
	var dmg int32
	world := &g.edicts[0]

	if ent.Health > 0 {
		if ent.Flags&FL_SWIM == 0 {
			if ent.Waterlevel < 3 {
				ent.AirFinished = g.level.Time + 12
			} else if ent.AirFinished < g.level.Time { // drown!
				if ent.PainDebounceTime < g.level.Time {
					dmg = int32(2 + 2*math.Floor(float64(g.level.Time-ent.AirFinished)))
					if dmg > 15 {
						dmg = 15
					}
					g.T_Damage(ent, world, world, &Vec3{}, ent.S.Origin, shared.Vec3Origin, dmg, 0, DAMAGE_NO_ARMOR, MOD_WATER)
					ent.PainDebounceTime = g.level.Time + 1
				}
			}
		} else {
			if ent.Waterlevel > 0 {
				ent.AirFinished = g.level.Time + 9
			} else if ent.AirFinished < g.level.Time { // suffocate!
				if ent.PainDebounceTime < g.level.Time {
					dmg = int32(2 + 2*math.Floor(float64(g.level.Time-ent.AirFinished)))
					if dmg > 15 {
						dmg = 15
					}
					g.T_Damage(ent, world, world, &Vec3{}, ent.S.Origin, shared.Vec3Origin, dmg, 0, DAMAGE_NO_ARMOR, MOD_WATER)
					ent.PainDebounceTime = g.level.Time + 1
				}
			}
		}
	}

	if ent.Waterlevel == 0 {
		if ent.Flags&FL_INWATER != 0 {
			g.gi.Sound(ent, CHAN_BODY, g.gi.SoundIndex("player/watr_out.wav"), 1, ATTN_NORM, 0)
			ent.Flags &^= FL_INWATER
		}
		return
	}

	if (ent.Watertype&CONTENTS_LAVA != 0) && ent.Flags&FL_IMMUNE_LAVA == 0 {
		if ent.DamageDebounceTime < g.level.Time {
			ent.DamageDebounceTime = float32(float64(g.level.Time) + 0.2)
			g.T_Damage(ent, world, world, &Vec3{}, ent.S.Origin, shared.Vec3Origin, 10*ent.Waterlevel, 0, 0, MOD_LAVA)
		}
	}
	if (ent.Watertype&CONTENTS_SLIME != 0) && ent.Flags&FL_IMMUNE_SLIME == 0 {
		if ent.DamageDebounceTime < g.level.Time {
			ent.DamageDebounceTime = g.level.Time + 1
			g.T_Damage(ent, world, world, &Vec3{}, ent.S.Origin, shared.Vec3Origin, 4*ent.Waterlevel, 0, 0, MOD_SLIME)
		}
	}

	if ent.Flags&FL_INWATER == 0 {
		if ent.SVFlags&SVF_DEADMONSTER == 0 {
			if ent.Watertype&CONTENTS_LAVA != 0 {
				if float64(g.random()) <= 0.5 {
					g.gi.Sound(ent, CHAN_BODY, g.gi.SoundIndex("player/lava1.wav"), 1, ATTN_NORM, 0)
				} else {
					g.gi.Sound(ent, CHAN_BODY, g.gi.SoundIndex("player/lava2.wav"), 1, ATTN_NORM, 0)
				}
			} else if ent.Watertype&CONTENTS_SLIME != 0 {
				g.gi.Sound(ent, CHAN_BODY, g.gi.SoundIndex("player/watr_in.wav"), 1, ATTN_NORM, 0)
			} else if ent.Watertype&CONTENTS_WATER != 0 {
				g.gi.Sound(ent, CHAN_BODY, g.gi.SoundIndex("player/watr_in.wav"), 1, ATTN_NORM, 0)
			}
		}

		ent.Flags |= FL_INWATER
		ent.DamageDebounceTime = 0
	}
}

// C: game/g_monster.c:310 M_droptofloor
func (g *Game) M_droptofloor(ent *Edict) {
	ent.S.Origin[2] += 1
	end := ent.S.Origin
	end[2] -= 256

	trace := g.gi.Trace(&ent.S.Origin, &ent.Mins, &ent.Maxs, &end, ent, MASK_MONSTERSOLID)

	if trace.Fraction == 1 || trace.AllSolid {
		return
	}

	ent.S.Origin = trace.EndPos

	g.gi.LinkEntity(ent)
	g.M_CheckGround(ent)
	g.M_CatagorizePosition(ent)
}

// C: game/g_monster.c:332 M_SetEffects
func (g *Game) M_SetEffects(ent *Edict) {
	ent.S.Effects &^= (EF_COLOR_SHELL | EF_POWERSCREEN)
	ent.S.RenderFX &^= (RF_SHELL_RED | RF_SHELL_GREEN | RF_SHELL_BLUE)

	if ent.Monsterinfo.Aiflags&AI_RESURRECTING != 0 {
		ent.S.Effects |= EF_COLOR_SHELL
		ent.S.RenderFX |= RF_SHELL_RED
	}

	if ent.Health <= 0 {
		return
	}

	if ent.PowerarmorTime > g.level.Time {
		if ent.Monsterinfo.PowerArmorType == POWER_ARMOR_SCREEN {
			ent.S.Effects |= EF_POWERSCREEN
		} else if ent.Monsterinfo.PowerArmorType == POWER_ARMOR_SHIELD {
			ent.S.Effects |= EF_COLOR_SHELL
			ent.S.RenderFX |= RF_SHELL_GREEN
		}
	}
}

// C: game/g_monster.c:361 M_MoveFrame
func (g *Game) M_MoveFrame(self *Edict) {
	move := self.Monsterinfo.Currentmove
	self.Nextthink = float32(float64(g.level.Time) + FRAMETIME)

	if (self.Monsterinfo.Nextframe != 0) && (self.Monsterinfo.Nextframe >= move.Firstframe) && (self.Monsterinfo.Nextframe <= move.Lastframe) {
		self.S.Frame = self.Monsterinfo.Nextframe
		self.Monsterinfo.Nextframe = 0
	} else {
		if self.S.Frame == move.Lastframe {
			if move.Endfunc != nil {
				move.Endfunc.fn(g, self)

				// regrab move, endfunc is very likely to change it
				move = self.Monsterinfo.Currentmove

				// check for death
				if self.SVFlags&SVF_DEADMONSTER != 0 {
					return
				}
			}
		}

		if self.S.Frame < move.Firstframe || self.S.Frame > move.Lastframe {
			self.Monsterinfo.Aiflags &^= AI_HOLD_FRAME
			self.S.Frame = move.Firstframe
		} else {
			if self.Monsterinfo.Aiflags&AI_HOLD_FRAME == 0 {
				self.S.Frame++
				if self.S.Frame > move.Lastframe {
					self.S.Frame = move.Firstframe
				}
			}
		}
	}

	index := self.S.Frame - move.Firstframe
	if move.Frame[index].Aifunc != nil {
		if self.Monsterinfo.Aiflags&AI_HOLD_FRAME == 0 {
			move.Frame[index].Aifunc.fn(g, self, move.Frame[index].Dist*self.Monsterinfo.Scale)
		} else {
			move.Frame[index].Aifunc.fn(g, self, 0)
		}
	}

	if move.Frame[index].Thinkfunc != nil {
		move.Frame[index].Thinkfunc.fn(g, self)
	}
}

// C: game/g_monster.c:419 monster_think
func (g *Game) monster_think(self *Edict) {
	g.M_MoveFrame(self)
	if self.LinkCount != self.Monsterinfo.Linkcount {
		self.Monsterinfo.Linkcount = self.LinkCount
		g.M_CheckGround(self)
	}
	g.M_CatagorizePosition(self)
	g.M_WorldEffects(self)
	g.M_SetEffects(self)
}

// monster_use: using a monster makes it angry at the current activator.
// C: game/g_monster.c:440 monster_use
func (g *Game) monster_use(self, other, activator *Edict) {
	if self.Enemy != nil {
		return
	}
	if self.Health <= 0 {
		return
	}
	if activator.Flags&FL_NOTARGET != 0 {
		return
	}
	if activator.Client == nil && activator.Monsterinfo.Aiflags&AI_GOOD_GUY == 0 {
		return
	}

	// delay reaction so if the monster is teleported, its sound is still heard
	self.Enemy = activator
	g.FoundTarget(self)
}

// C: game/g_monster.c:460 monster_triggered_spawn
func (g *Game) monster_triggered_spawn(self *Edict) {
	self.S.Origin[2] += 1
	g.KillBox(self)

	self.Solid = SOLID_BBOX
	self.Movetype = MOVETYPE_STEP
	self.SVFlags &^= SVF_NOCLIENT
	self.AirFinished = g.level.Time + 12
	g.gi.LinkEntity(self)

	g.monster_start_go(self)

	if self.Enemy != nil && self.Spawnflags&1 == 0 && self.Enemy.Flags&FL_NOTARGET == 0 {
		g.FoundTarget(self)
	} else {
		self.Enemy = nil
	}
}

// C: game/g_monster.c:483 monster_triggered_spawn_use
func (g *Game) monster_triggered_spawn_use(self, other, activator *Edict) {
	// we have a one frame delay here so we don't telefrag the guy who activated us
	self.Think = monster_triggered_spawn
	self.Nextthink = float32(float64(g.level.Time) + FRAMETIME)
	if activator.Client != nil {
		self.Enemy = activator
	}
	self.Use = monster_use
}

// C: game/g_monster.c:493 monster_triggered_start
func (g *Game) monster_triggered_start(self *Edict) {
	self.Solid = SOLID_NOT
	self.Movetype = MOVETYPE_NONE
	self.SVFlags |= SVF_NOCLIENT
	self.Nextthink = 0
	self.Use = monster_triggered_spawn_use
}

// monster_death_use: when a monster dies, it fires all of its targets with
// the current enemy as activator.
// C: game/g_monster.c:511 monster_death_use
func (g *Game) monster_death_use(self *Edict) {
	self.Flags &^= (FL_FLY | FL_SWIM)
	self.Monsterinfo.Aiflags &= AI_GOOD_GUY

	if self.Item != nil {
		g.Drop_Item(self, self.Item)
		self.Item = nil
	}

	if self.Deathtarget != "" {
		self.Target = self.Deathtarget
	}

	if self.Target == "" {
		return
	}

	g.G_UseTargets(self, self.Enemy)
}

//============================================================================

// C: game/g_monster.c:534 monster_start
func (g *Game) monster_start(self *Edict) bool {
	if g.deathmatch.Value != 0 {
		g.G_FreeEdict(self)
		return false
	}

	if (self.Spawnflags&4 != 0) && self.Monsterinfo.Aiflags&AI_GOOD_GUY == 0 {
		self.Spawnflags &^= 4
		self.Spawnflags |= 1
		//		gi.dprintf("fixed spawnflags on %s at %s\n", self->classname, vtos(self->s.origin));
	}

	if self.Monsterinfo.Aiflags&AI_GOOD_GUY == 0 {
		g.level.TotalMonsters++
	}

	self.Nextthink = float32(float64(g.level.Time) + FRAMETIME)
	self.SVFlags |= SVF_MONSTER
	self.S.RenderFX |= RF_FRAMELERP
	self.Takedamage = DAMAGE_AIM
	self.AirFinished = g.level.Time + 12
	self.Use = monster_use
	self.MaxHealth = self.Health
	self.ClipMask = MASK_MONSTERSOLID

	self.S.SkinNum = 0
	self.Deadflag = DEAD_NO
	self.SVFlags &^= SVF_DEADMONSTER

	if self.Monsterinfo.Checkattack == nil {
		self.Monsterinfo.Checkattack = M_CheckAttack
	}
	self.S.OldOrigin = self.S.Origin

	if g.st.Item != "" {
		self.Item = g.FindItemByClassname(g.st.Item)
		if self.Item == nil {
			g.dprintf("%s at %s has bad item: %s\n", self.Classname, vtos(self.S.Origin), g.st.Item)
		}
	}

	// randomize what frame they start on
	if self.Monsterinfo.Currentmove != nil {
		cm := self.Monsterinfo.Currentmove
		self.S.Frame = cm.Firstframe + (g.rng.Rand() % (cm.Lastframe - cm.Firstframe + 1))
	}

	return true
}

// C: game/g_monster.c:583 monster_start_go
func (g *Game) monster_start_go(self *Edict) {
	if self.Health <= 0 {
		return
	}

	// check for target to combat_point and change to combattarget
	if self.Target != "" {
		var notcombat, fixup bool
		var target *Edict

		for {
			target = g.G_Find(target, FOFS_targetname, self.Target)
			if target == nil {
				break
			}
			if target.Classname == "point_combat" {
				self.Combattarget = self.Target
				fixup = true
			} else {
				notcombat = true
			}
		}
		if notcombat && self.Combattarget != "" {
			g.dprintf("%s at %s has target with mixed types\n", self.Classname, vtos(self.S.Origin))
		}
		if fixup {
			self.Target = ""
		}
	}

	// validate combattarget
	if self.Combattarget != "" {
		var target *Edict
		for {
			target = g.G_Find(target, FOFS_targetname, self.Combattarget)
			if target == nil {
				break
			}
			if target.Classname != "point_combat" {
				g.dprintf("%s at (%i %i %i) has a bad combattarget %s : %s at (%i %i %i)\n",
					self.Classname, int32(self.S.Origin[0]), int32(self.S.Origin[1]), int32(self.S.Origin[2]),
					self.Combattarget, target.Classname, int32(target.S.Origin[0]), int32(target.S.Origin[1]),
					int32(target.S.Origin[2]))
			}
		}
	}

	if self.Target != "" {
		self.Movetarget = g.G_PickTarget(self.Target)
		self.Goalentity = self.Movetarget
		if self.Movetarget == nil {
			g.dprintf("%s can't find target %s at %s\n", self.Classname, self.Target, vtos(self.S.Origin))
			self.Target = ""
			self.Monsterinfo.Pausetime = 100000000
			self.Monsterinfo.Stand.fn(g, self)
		} else if self.Movetarget.Classname == "path_corner" {
			v := shared.VectorSubtract(self.Goalentity.S.Origin, self.S.Origin)
			self.S.Angles[YAW] = vectoyaw(v)
			self.IdealYaw = self.S.Angles[YAW]
			self.Monsterinfo.Walk.fn(g, self)
			self.Target = ""
		} else {
			self.Movetarget = nil
			self.Goalentity = nil
			self.Monsterinfo.Pausetime = 100000000
			self.Monsterinfo.Stand.fn(g, self)
		}
	} else {
		self.Monsterinfo.Pausetime = 100000000
		self.Monsterinfo.Stand.fn(g, self)
	}

	self.Think = monster_think
	self.Nextthink = float32(float64(g.level.Time) + FRAMETIME)
}

// C: game/g_monster.c:671 walkmonster_start_go
func (g *Game) walkmonster_start_go(self *Edict) {
	if self.Spawnflags&2 == 0 && g.level.Time < 1 {
		g.M_droptofloor(self)

		if self.Groundentity != nil {
			if !g.M_walkmove(self, 0, 0) {
				g.dprintf("%s in solid at %s\n", self.Classname, vtos(self.S.Origin))
			}
		}
	}

	if self.YawSpeed == 0 {
		self.YawSpeed = 20
	}
	self.Viewheight = 25

	g.monster_start_go(self)

	if self.Spawnflags&2 != 0 {
		g.monster_triggered_start(self)
	}
}

// C: game/g_monster.c:692 walkmonster_start
func (g *Game) walkmonster_start(self *Edict) {
	self.Think = walkmonster_start_go
	g.monster_start(self)
}

// C: game/g_monster.c:699 flymonster_start_go
func (g *Game) flymonster_start_go(self *Edict) {
	if !g.M_walkmove(self, 0, 0) {
		g.dprintf("%s in solid at %s\n", self.Classname, vtos(self.S.Origin))
	}

	if self.YawSpeed == 0 {
		self.YawSpeed = 10
	}
	self.Viewheight = 25

	g.monster_start_go(self)

	if self.Spawnflags&2 != 0 {
		g.monster_triggered_start(self)
	}
}

// C: game/g_monster.c:715 flymonster_start
func (g *Game) flymonster_start(self *Edict) {
	self.Flags |= FL_FLY
	self.Think = flymonster_start_go
	g.monster_start(self)
}

// C: game/g_monster.c:723 swimmonster_start_go
func (g *Game) swimmonster_start_go(self *Edict) {
	if self.YawSpeed == 0 {
		self.YawSpeed = 10
	}
	self.Viewheight = 10

	g.monster_start_go(self)

	if self.Spawnflags&2 != 0 {
		g.monster_triggered_start(self)
	}
}

// C: game/g_monster.c:735 swimmonster_start
func (g *Game) swimmonster_start(self *Edict) {
	self.Flags |= FL_SWIM
	self.Think = swimmonster_start_go
	g.monster_start(self)
}
