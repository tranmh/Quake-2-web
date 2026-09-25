package game

// Port of game/g_combat.c.

import (
	"math"

	. "quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/shared"
)

// CanDamage returns true if the inflictor can directly damage the target.
// Used for explosions and melee attacks.
// C: game/g_combat.c:32 CanDamage
func (g *Game) CanDamage(targ, inflictor *Edict) bool {
	var dest Vec3
	var trace Trace
	origin := shared.Vec3Origin

	// bmodels need special checking because their origin is 0,0,0
	if targ.Movetype == MOVETYPE_PUSH {
		dest = shared.VectorAdd(targ.AbsMin, targ.AbsMax)
		dest = shared.VectorScale(dest, 0.5)
		trace = g.gi.Trace(&inflictor.S.Origin, &origin, &origin, &dest, inflictor, MASK_SOLID)
		if trace.Fraction == 1.0 {
			return true
		}
		if trace.Ent == targ {
			return true
		}
		return false
	}

	trace = g.gi.Trace(&inflictor.S.Origin, &origin, &origin, &targ.S.Origin, inflictor, MASK_SOLID)
	if trace.Fraction == 1.0 {
		return true
	}

	dest = targ.S.Origin
	dest[0] = float32(float64(dest[0]) + 15.0)
	dest[1] = float32(float64(dest[1]) + 15.0)
	trace = g.gi.Trace(&inflictor.S.Origin, &origin, &origin, &dest, inflictor, MASK_SOLID)
	if trace.Fraction == 1.0 {
		return true
	}

	dest = targ.S.Origin
	dest[0] = float32(float64(dest[0]) + 15.0)
	dest[1] = float32(float64(dest[1]) - 15.0)
	trace = g.gi.Trace(&inflictor.S.Origin, &origin, &origin, &dest, inflictor, MASK_SOLID)
	if trace.Fraction == 1.0 {
		return true
	}

	dest = targ.S.Origin
	dest[0] = float32(float64(dest[0]) - 15.0)
	dest[1] = float32(float64(dest[1]) + 15.0)
	trace = g.gi.Trace(&inflictor.S.Origin, &origin, &origin, &dest, inflictor, MASK_SOLID)
	if trace.Fraction == 1.0 {
		return true
	}

	dest = targ.S.Origin
	dest[0] = float32(float64(dest[0]) - 15.0)
	dest[1] = float32(float64(dest[1]) - 15.0)
	trace = g.gi.Trace(&inflictor.S.Origin, &origin, &origin, &dest, inflictor, MASK_SOLID)
	if trace.Fraction == 1.0 {
		return true
	}

	return false
}

// C: game/g_combat.c:92 Killed
func (g *Game) Killed(targ, inflictor, attacker *Edict, damage int32, point Vec3) {
	if targ.Health < -999 {
		targ.Health = -999
	}

	targ.Enemy = attacker

	if targ.SVFlags&SVF_MONSTER != 0 && targ.Deadflag != DEAD_DEAD {
		//		targ->svflags |= SVF_DEADMONSTER;	// now treat as a different content type
		if targ.Monsterinfo.Aiflags&AI_GOOD_GUY == 0 {
			g.level.KilledMonsters++
			if g.coop.Value != 0 && attacker.Client != nil {
				attacker.Client.Resp.Score++
			}
			// medics won't heal monsters that they kill themselves
			if attacker.Classname == "monster_medic" {
				targ.Owner = attacker
			}
		}
	}

	if targ.Movetype == MOVETYPE_PUSH || targ.Movetype == MOVETYPE_STOP || targ.Movetype == MOVETYPE_NONE {
		// doors, triggers, etc
		targ.Die.fn(g, targ, inflictor, attacker, damage, point)
		return
	}

	if targ.SVFlags&SVF_MONSTER != 0 && targ.Deadflag != DEAD_DEAD {
		targ.Touch = nil
		g.monster_death_use(targ)
	}

	targ.Die.fn(g, targ, inflictor, attacker, damage, point)
}

// C: game/g_combat.c:134 SpawnDamage
func (g *Game) SpawnDamage(type_ int32, origin, normal Vec3, damage int32) {
	if damage > 255 {
		damage = 255
	}
	g.gi.WriteByteC(svc_temp_entity)
	g.gi.WriteByteC(int(type_))
	//	gi.WriteByte (damage);
	g.gi.WritePosition(&origin)
	g.gi.WriteDir(&normal)
	g.gi.Multicast(&origin, MULTICAST_PVS)
}

// C: game/g_combat.c:171 CheckPowerArmor
func (g *Game) CheckPowerArmor(ent *Edict, point, normal Vec3, damage, dflags int32) int32 {
	var save int32
	var power_armor_type int32
	var index int32
	var damagePerCell int32
	var pa_te_type int32
	var power int32
	var power_used int32

	if damage == 0 {
		return 0
	}

	client := ent.Client

	if dflags&DAMAGE_NO_ARMOR != 0 {
		return 0
	}

	if client != nil {
		power_armor_type = g.PowerArmorType(ent)
		if power_armor_type != POWER_ARMOR_NONE {
			index = ITEM_INDEX(g.FindItem("Cells"))
			power = client.Pers.Inventory[index]
		}
	} else if ent.SVFlags&SVF_MONSTER != 0 {
		power_armor_type = ent.Monsterinfo.PowerArmorType
		power = ent.Monsterinfo.PowerArmorPower
	} else {
		return 0
	}

	if power_armor_type == POWER_ARMOR_NONE {
		return 0
	}
	if power == 0 {
		return 0
	}

	if power_armor_type == POWER_ARMOR_SCREEN {
		var forward Vec3

		// only works if damage point is in front
		shared.AngleVectors(ent.S.Angles, &forward, nil, nil)
		vec := shared.VectorSubtract(point, ent.S.Origin)
		shared.VectorNormalize(&vec)
		dot := shared.DotProduct(vec, forward)
		if float64(dot) <= 0.3 {
			return 0
		}

		damagePerCell = 1
		pa_te_type = TE_SCREEN_SPARKS
		damage = damage / 3
	} else {
		damagePerCell = 2
		pa_te_type = TE_SHIELD_SPARKS
		damage = (2 * damage) / 3
	}

	save = power * damagePerCell
	if save == 0 {
		return 0
	}
	if save > damage {
		save = damage
	}

	g.SpawnDamage(pa_te_type, point, normal, save)
	ent.PowerarmorTime = float32(float64(g.level.Time) + 0.2)

	power_used = save / damagePerCell

	if client != nil {
		client.Pers.Inventory[index] -= power_used
	} else {
		ent.Monsterinfo.PowerArmorPower -= power_used
	}
	return save
}

// C: game/g_combat.c:255 CheckArmor
func (g *Game) CheckArmor(ent *Edict, point, normal Vec3, damage, te_sparks, dflags int32) int32 {
	var save int32

	if damage == 0 {
		return 0
	}

	client := ent.Client

	if client == nil {
		return 0
	}

	if dflags&DAMAGE_NO_ARMOR != 0 {
		return 0
	}

	index := g.ArmorIndex(ent)
	if index == 0 {
		return 0
	}

	armor := g.GetItemByIndex(index)

	if dflags&DAMAGE_ENERGY != 0 {
		save = int32(math.Ceil(float64(armor.Info.EnergyProtection * float32(damage))))
	} else {
		save = int32(math.Ceil(float64(armor.Info.NormalProtection * float32(damage))))
	}
	if save >= client.Pers.Inventory[index] {
		save = client.Pers.Inventory[index]
	}

	if save == 0 {
		return 0
	}

	client.Pers.Inventory[index] -= save
	g.SpawnDamage(te_sparks, point, normal, save)

	return save
}

// C: game/g_combat.c:295 M_ReactToDamage
func (g *Game) M_ReactToDamage(targ, attacker *Edict) {
	if attacker.Client == nil && attacker.SVFlags&SVF_MONSTER == 0 {
		return
	}

	if attacker == targ || attacker == targ.Enemy {
		return
	}

	// if we are a good guy monster and our attacker is a player
	// or another good guy, do not get mad at them
	if targ.Monsterinfo.Aiflags&AI_GOOD_GUY != 0 {
		if attacker.Client != nil || attacker.Monsterinfo.Aiflags&AI_GOOD_GUY != 0 {
			return
		}
	}

	// we now know that we are not both good guys

	// if attacker is a client, get mad at them because he's good and we're not
	if attacker.Client != nil {
		targ.Monsterinfo.Aiflags &^= AI_SOUND_TARGET

		// this can only happen in coop (both new and old enemies are clients)
		// only switch if can't see the current enemy
		if targ.Enemy != nil && targ.Enemy.Client != nil {
			if g.visible(targ, targ.Enemy) {
				targ.Oldenemy = attacker
				return
			}
			targ.Oldenemy = targ.Enemy
		}
		targ.Enemy = attacker
		if targ.Monsterinfo.Aiflags&AI_DUCKED == 0 {
			g.FoundTarget(targ)
		}
		return
	}

	// it's the same base (walk/swim/fly) type and a different classname and it's not a tank
	// (they spray too much), get mad at them
	if (targ.Flags&(FL_FLY|FL_SWIM)) == (attacker.Flags&(FL_FLY|FL_SWIM)) &&
		targ.Classname != attacker.Classname &&
		attacker.Classname != "monster_tank" &&
		attacker.Classname != "monster_supertank" &&
		attacker.Classname != "monster_makron" &&
		attacker.Classname != "monster_jorg" {
		if targ.Enemy != nil && targ.Enemy.Client != nil {
			targ.Oldenemy = targ.Enemy
		}
		targ.Enemy = attacker
		if targ.Monsterinfo.Aiflags&AI_DUCKED == 0 {
			g.FoundTarget(targ)
		}
	} else if attacker.Enemy == targ {
		// if they *meant* to shoot us, then shoot back
		if targ.Enemy != nil && targ.Enemy.Client != nil {
			targ.Oldenemy = targ.Enemy
		}
		targ.Enemy = attacker
		if targ.Monsterinfo.Aiflags&AI_DUCKED == 0 {
			g.FoundTarget(targ)
		}
	} else if attacker.Enemy != nil && attacker.Enemy != targ {
		// otherwise get mad at whoever they are mad at (help our buddy) unless it is us!
		if targ.Enemy != nil && targ.Enemy.Client != nil {
			targ.Oldenemy = targ.Enemy
		}
		targ.Enemy = attacker.Enemy
		if targ.Monsterinfo.Aiflags&AI_DUCKED == 0 {
			g.FoundTarget(targ)
		}
	}
}

// C: game/g_combat.c:370 CheckTeamDamage
func (g *Game) CheckTeamDamage(targ, attacker *Edict) bool {
	//FIXME make the next line real and uncomment this block
	// if ((ability to damage a teammate == OFF) && (targ's team == attacker's team))
	return false
}

// T_Damage: dir is a pointer because C normalizes the caller's vector in
// place (VectorNormalize(dir)); callers pass e.g. &self.Velocity or
// &self.Movedir and rely on that side effect (bfg_touch, target_laser,
// the shotgun pellet loop).
//
// targ		entity that is being damaged
// inflictor	entity that is causing the damage
// attacker	entity that caused the inflictor to damage targ
// dir		direction of the attack
// point	point at which the damage is being inflicted
// normal	normal vector from that point
// damage	amount of damage being inflicted
// knockback	force to be applied against targ as a result of the damage
// dflags	these flags are used to control how T_Damage works
// C: game/g_combat.c:377 T_Damage
func (g *Game) T_Damage(targ, inflictor, attacker *Edict, dir *Vec3, point, normal Vec3, damage, knockback, dflags, mod int32) {
	var take, save, asave, psave, te_sparks int32

	if targ.Takedamage == 0 {
		return
	}

	// friendly fire avoidance
	// if enabled you can't hurt teammates (but you can hurt yourself)
	// knockback still occurs
	if targ != attacker && ((g.deathmatch.Value != 0 && int32(g.dmflags.Value)&(DF_MODELTEAMS|DF_SKINTEAMS) != 0) || g.coop.Value != 0) {
		if g.OnSameTeam(targ, attacker) {
			if int32(g.dmflags.Value)&DF_NO_FRIENDLY_FIRE != 0 {
				damage = 0
			} else {
				mod |= MOD_FRIENDLY_FIRE
			}
		}
	}
	g.meansOfDeath = mod

	// easy mode takes half damage
	if g.skill.Value == 0 && g.deathmatch.Value == 0 && targ.Client != nil {
		damage = int32(float64(damage) * 0.5)
		if damage == 0 {
			damage = 1
		}
	}

	client := targ.Client

	if dflags&DAMAGE_BULLET != 0 {
		te_sparks = TE_BULLET_SPARKS
	} else {
		te_sparks = TE_SPARKS
	}

	shared.VectorNormalize(dir)

	// bonus damage for suprising a monster
	if dflags&DAMAGE_RADIUS == 0 && targ.SVFlags&SVF_MONSTER != 0 && attacker.Client != nil && targ.Enemy == nil && targ.Health > 0 {
		damage *= 2
	}

	if targ.Flags&FL_NO_KNOCKBACK != 0 {
		knockback = 0
	}

	// figure momentum add
	if dflags&DAMAGE_NO_KNOCKBACK == 0 {
		if knockback != 0 && targ.Movetype != MOVETYPE_NONE && targ.Movetype != MOVETYPE_BOUNCE && targ.Movetype != MOVETYPE_PUSH && targ.Movetype != MOVETYPE_STOP {
			var kvel Vec3
			var mass float32

			if targ.Mass < 50 {
				mass = 50
			} else {
				mass = float32(targ.Mass)
			}

			if targ.Client != nil && attacker == targ {
				kvel = shared.VectorScale(*dir, float32(1600.0*float64(float32(knockback))/float64(mass))) // the rocket jump hack...
			} else {
				kvel = shared.VectorScale(*dir, float32(500.0*float64(float32(knockback))/float64(mass)))
			}

			targ.Velocity = shared.VectorAdd(targ.Velocity, kvel)
		}
	}

	take = damage
	save = 0

	// check for godmode
	if targ.Flags&FL_GODMODE != 0 && dflags&DAMAGE_NO_PROTECTION == 0 {
		take = 0
		save = damage
		g.SpawnDamage(te_sparks, point, normal, save)
	}

	// check for invincibility
	if (client != nil && client.InvincibleFramenum > float32(g.level.Framenum)) && dflags&DAMAGE_NO_PROTECTION == 0 {
		if targ.PainDebounceTime < g.level.Time {
			g.gi.Sound(targ, CHAN_ITEM, g.gi.SoundIndex("items/protect4.wav"), 1, ATTN_NORM, 0)
			targ.PainDebounceTime = g.level.Time + 2
		}
		take = 0
		save = damage
	}

	psave = g.CheckPowerArmor(targ, point, normal, take, dflags)
	take -= psave

	asave = g.CheckArmor(targ, point, normal, take, te_sparks, dflags)
	take -= asave

	//treat cheat/powerup savings the same as armor
	asave += save

	// team damage avoidance
	if dflags&DAMAGE_NO_PROTECTION == 0 && g.CheckTeamDamage(targ, attacker) {
		return
	}

	// do the damage
	if take != 0 {
		if targ.SVFlags&SVF_MONSTER != 0 || client != nil {
			g.SpawnDamage(TE_BLOOD, point, normal, take)
		} else {
			g.SpawnDamage(te_sparks, point, normal, take)
		}

		targ.Health = targ.Health - take

		if targ.Health <= 0 {
			if targ.SVFlags&SVF_MONSTER != 0 || client != nil {
				targ.Flags |= FL_NO_KNOCKBACK
			}
			g.Killed(targ, inflictor, attacker, take, point)
			return
		}
	}

	if targ.SVFlags&SVF_MONSTER != 0 {
		g.M_ReactToDamage(targ, attacker)
		if targ.Monsterinfo.Aiflags&AI_DUCKED == 0 && take != 0 {
			targ.Pain.fn(g, targ, attacker, float32(knockback), take)
			// nightmare mode monsters don't go into pain frames often
			if g.skill.Value == 3 {
				targ.PainDebounceTime = g.level.Time + 5
			}
		}
	} else if client != nil {
		if targ.Flags&FL_GODMODE == 0 && take != 0 {
			targ.Pain.fn(g, targ, attacker, float32(knockback), take)
		}
	} else if take != 0 {
		if targ.Pain != nil {
			targ.Pain.fn(g, targ, attacker, float32(knockback), take)
		}
	}

	// add to the damage inflicted on a player this frame
	// the total will be turned into screen blends and view angle kicks
	// at the end of the frame
	if client != nil {
		client.DamageParmor += psave
		client.DamageArmor += asave
		client.DamageBlood += take
		client.DamageKnockback += knockback
		client.DamageFrom = point
	}
}

// C: game/g_combat.c:547 T_RadiusDamage
func (g *Game) T_RadiusDamage(inflictor, attacker *Edict, damage float32, ignore *Edict, radius float32, mod int32) {
	var points float32
	var ent *Edict

	for {
		ent = g.findradius(ent, inflictor.S.Origin, radius)
		if ent == nil {
			break
		}
		if ent == ignore {
			continue
		}
		if ent.Takedamage == 0 {
			continue
		}

		v := shared.VectorAdd(ent.Mins, ent.Maxs)
		v = shared.VectorMA(ent.S.Origin, 0.5, v)
		v = shared.VectorSubtract(inflictor.S.Origin, v)
		points = float32(float64(damage) - 0.5*float64(shared.VectorLength(v)))
		if ent == attacker {
			points = float32(float64(points) * 0.5)
		}
		if points > 0 {
			if g.CanDamage(ent, inflictor) {
				dir := shared.VectorSubtract(ent.S.Origin, inflictor.S.Origin)
				g.T_Damage(ent, inflictor, attacker, &dir, inflictor.S.Origin, shared.Vec3Origin, int32(points), int32(points), DAMAGE_RADIUS, mod)
			}
		}
	}
}
