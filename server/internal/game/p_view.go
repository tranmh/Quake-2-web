package game

// Port of game/p_view.c: player view, effects, sounds and frames computed
// at the end of each server frame.

import (
	"fmt"
	"math"

	. "quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/shared"
)

// C: game/p_view.c:42 SV_CalcRoll
func (g *Game) SV_CalcRoll(angles, velocity Vec3) float32 {
	var sign, side, value float32

	side = shared.DotProduct(velocity, g.right)
	if side < 0 {
		sign = -1
	} else {
		sign = 1
	}
	side = float32(math.Abs(float64(side)))

	value = g.sv_rollangle.Value

	if side < g.sv_rollspeed.Value {
		side = side * value / g.sv_rollspeed.Value
	} else {
		side = value
	}

	return side * sign
}

// static colors of P_DamageFeedback (never modified)
// C: game/p_view.c:78
var (
	pviewPowerColor = Vec3{0.0, 1.0, 0.0}
	pviewAColor     = Vec3{1.0, 1.0, 1.0}
	pviewBColor     = Vec3{1.0, 0.0, 0.0}
)

// P_DamageFeedback handles color blends and view kicks.
// C: game/p_view.c:71 P_DamageFeedback
func (g *Game) P_DamageFeedback(player *Edict) {
	var side float32
	var realcount, count, kick float32
	var v Vec3
	var r, l int32

	client := player.Client

	// flash the backgrounds behind the status numbers
	client.PS.Stats[STAT_FLASHES] = 0
	if client.DamageBlood != 0 {
		client.PS.Stats[STAT_FLASHES] |= 1
	}
	if client.DamageArmor != 0 && player.Flags&FL_GODMODE == 0 && client.InvincibleFramenum <= float32(g.level.Framenum) {
		client.PS.Stats[STAT_FLASHES] |= 2
	}

	// total points of damage shot at the player this frame
	count = float32(client.DamageBlood + client.DamageArmor + client.DamageParmor)
	if count == 0 {
		return // didn't take any damage
	}

	// start a pain animation if still in the player model
	if client.AnimPriority < ANIM_PAIN && player.S.ModelIndex == 255 {
		client.AnimPriority = ANIM_PAIN
		if client.PS.PMove.PmFlags&PMF_DUCKED != 0 {
			player.S.Frame = FRAME_crpain1 - 1
			client.AnimEnd = FRAME_crpain4
		} else {
			// C: static int i (function-level static)
			g.P_DamageFeedback_i = (g.P_DamageFeedback_i + 1) % 3
			switch g.P_DamageFeedback_i {
			case 0:
				player.S.Frame = FRAME_pain101 - 1
				client.AnimEnd = FRAME_pain104
			case 1:
				player.S.Frame = FRAME_pain201 - 1
				client.AnimEnd = FRAME_pain204
			case 2:
				player.S.Frame = FRAME_pain301 - 1
				client.AnimEnd = FRAME_pain304
			}
		}
	}

	realcount = count
	if count < 10 {
		count = 10 // always make a visible effect
	}

	// play an apropriate pain sound
	if g.level.Time > player.PainDebounceTime && player.Flags&FL_GODMODE == 0 && client.InvincibleFramenum <= float32(g.level.Framenum) {
		r = 1 + (g.rng.Rand() & 1)
		player.PainDebounceTime = float32(float64(g.level.Time) + 0.7)
		if player.Health < 25 {
			l = 25
		} else if player.Health < 50 {
			l = 50
		} else if player.Health < 75 {
			l = 75
		} else {
			l = 100
		}
		g.gi.Sound(player, CHAN_VOICE, g.gi.SoundIndex(fmt.Sprintf("*pain%d_%d.wav", l, r)), 1, ATTN_NORM, 0)
	}

	// the total alpha of the blend is always proportional to count
	if client.DamageAlpha < 0 {
		client.DamageAlpha = 0
	}
	client.DamageAlpha = float32(float64(client.DamageAlpha) + float64(count)*0.01)
	if float64(client.DamageAlpha) < 0.2 {
		client.DamageAlpha = 0.2
	}
	if float64(client.DamageAlpha) > 0.6 {
		client.DamageAlpha = 0.6 // don't go too saturated
	}

	// the color of the blend will vary based on how much was absorbed
	// by different armors
	v = Vec3{}
	if client.DamageParmor != 0 {
		v = shared.VectorMA(v, float32(client.DamageParmor)/realcount, pviewPowerColor)
	}
	if client.DamageArmor != 0 {
		v = shared.VectorMA(v, float32(client.DamageArmor)/realcount, pviewAColor)
	}
	if client.DamageBlood != 0 {
		v = shared.VectorMA(v, float32(client.DamageBlood)/realcount, pviewBColor)
	}
	client.DamageBlend = v

	//
	// calculate view angle kicks
	//
	kb := client.DamageKnockback
	if kb < 0 {
		kb = -kb
	}
	kick = float32(kb)
	if kick != 0 && player.Health > 0 { // kick of 0 means no view adjust at all
		kick = kick * 100 / float32(player.Health)

		if float64(kick) < float64(count)*0.5 {
			kick = float32(float64(count) * 0.5)
		}
		if kick > 50 {
			kick = 50
		}

		v = shared.VectorSubtract(client.DamageFrom, player.S.Origin)
		shared.VectorNormalize(&v)

		side = shared.DotProduct(v, g.right)
		client.VDmgRoll = float32(float64(kick*side) * 0.3)

		side = -shared.DotProduct(v, g.forward)
		client.VDmgPitch = float32(float64(kick*side) * 0.3)

		client.VDmgTime = float32(float64(g.level.Time) + DAMAGE_TIME)
	}

	//
	// clear totals
	//
	client.DamageBlood = 0
	client.DamageArmor = 0
	client.DamageParmor = 0
	client.DamageKnockback = 0
}

// SV_CalcViewOffset: auto pitching on slopes?
//
//	fall from 128: 400 = 160000
//	fall from 256: 580 = 336400
//	fall from 384: 720 = 518400
//	fall from 512: 800 = 640000
//	fall from 640: 960 =
//
//	damage = deltavelocity*deltavelocity  * 0.0001
//
// C: game/p_view.c:222 SV_CalcViewOffset
func (g *Game) SV_CalcViewOffset(ent *Edict) {
	var bob, ratio, delta float32
	var v Vec3

	//===================================

	// base angles
	angles := &ent.Client.PS.KickAngles

	// if dead, fix the angle and don't add any kick
	if ent.Deadflag != 0 {
		*angles = Vec3{}

		ent.Client.PS.ViewAngles[ROLL] = 40
		ent.Client.PS.ViewAngles[PITCH] = -15
		ent.Client.PS.ViewAngles[YAW] = ent.Client.KillerYaw
	} else {
		// add angles based on weapon kick

		*angles = ent.Client.KickAngles

		// add angles based on damage kick

		ratio = float32(float64(ent.Client.VDmgTime-g.level.Time) / DAMAGE_TIME)
		if ratio < 0 {
			ratio = 0
			ent.Client.VDmgPitch = 0
			ent.Client.VDmgRoll = 0
		}
		angles[PITCH] += ratio * ent.Client.VDmgPitch
		angles[ROLL] += ratio * ent.Client.VDmgRoll

		// add pitch based on fall kick

		ratio = float32(float64(ent.Client.FallTime-g.level.Time) / FALL_TIME)
		if ratio < 0 {
			ratio = 0
		}
		angles[PITCH] += ratio * ent.Client.FallValue

		// add angles based on velocity

		delta = shared.DotProduct(ent.Velocity, g.forward)
		angles[PITCH] += delta * g.run_pitch.Value

		delta = shared.DotProduct(ent.Velocity, g.right)
		angles[ROLL] += delta * g.run_roll.Value

		// add angles based on bob

		delta = g.bobfracsin * g.bob_pitch.Value * g.xyspeed
		if ent.Client.PS.PMove.PmFlags&PMF_DUCKED != 0 {
			delta *= 6 // crouching
		}
		angles[PITCH] += delta
		delta = g.bobfracsin * g.bob_roll.Value * g.xyspeed
		if ent.Client.PS.PMove.PmFlags&PMF_DUCKED != 0 {
			delta *= 6 // crouching
		}
		if g.bobcycle&1 != 0 {
			delta = -delta
		}
		angles[ROLL] += delta
	}

	//===================================

	// base origin

	v = Vec3{}

	// add view height

	v[2] += float32(ent.Viewheight)

	// add fall height

	ratio = float32(float64(ent.Client.FallTime-g.level.Time) / FALL_TIME)
	if ratio < 0 {
		ratio = 0
	}
	v[2] = float32(float64(v[2]) - float64(ratio*ent.Client.FallValue)*0.4)

	// add bob height

	bob = g.bobfracsin * g.xyspeed * g.bob_up.Value
	if bob > 6 {
		bob = 6
	}
	//gi.DebugGraph (bob *2, 255);
	v[2] += bob

	// add kick offset

	v = shared.VectorAdd(v, ent.Client.KickOrigin)

	// absolutely bound offsets
	// so the view can never be outside the player box

	if v[0] < -14 {
		v[0] = -14
	} else if v[0] > 14 {
		v[0] = 14
	}
	if v[1] < -14 {
		v[1] = -14
	} else if v[1] > 14 {
		v[1] = 14
	}
	if v[2] < -22 {
		v[2] = -22
	} else if v[2] > 30 {
		v[2] = 30
	}

	ent.Client.PS.ViewOffset = v
}

// C: game/p_view.c:345 SV_CalcGunOffset
func (g *Game) SV_CalcGunOffset(ent *Edict) {
	var delta float32
	ps := &ent.Client.PS

	// gun angles from bobbing
	ps.GunAngles[ROLL] = float32(float64(g.xyspeed*g.bobfracsin) * 0.005)
	ps.GunAngles[YAW] = float32(float64(g.xyspeed*g.bobfracsin) * 0.01)
	if g.bobcycle&1 != 0 {
		ps.GunAngles[ROLL] = -ps.GunAngles[ROLL]
		ps.GunAngles[YAW] = -ps.GunAngles[YAW]
	}

	ps.GunAngles[PITCH] = float32(float64(g.xyspeed*g.bobfracsin) * 0.005)

	// gun angles from delta movement
	for i := 0; i < 3; i++ {
		delta = ent.Client.Oldviewangles[i] - ps.ViewAngles[i]
		if delta > 180 {
			delta -= 360
		}
		if delta < -180 {
			delta += 360
		}
		if delta > 45 {
			delta = 45
		}
		if delta < -45 {
			delta = -45
		}
		if i == YAW {
			ps.GunAngles[ROLL] = float32(float64(ps.GunAngles[ROLL]) + 0.1*float64(delta))
		}
		ps.GunAngles[i] = float32(float64(ps.GunAngles[i]) + 0.2*float64(delta))
	}

	// gun height
	ps.GunOffset = Vec3{}
	//	ent->ps->gunorigin[2] += bob;

	// gun_x / gun_y / gun_z are development tools
	for i := 0; i < 3; i++ {
		ps.GunOffset[i] += g.forward[i] * g.gun_y.Value
		ps.GunOffset[i] += g.right[i] * g.gun_x.Value
		ps.GunOffset[i] += g.up[i] * (-g.gun_z.Value)
	}
}

// C: game/p_view.c:397 SV_AddBlend
func SV_AddBlend(r, g, b, a float32, v_blend *[4]float32) {
	var a2, a3 float32

	if a <= 0 {
		return
	}
	a2 = v_blend[3] + (1-v_blend[3])*a // new total alpha
	a3 = v_blend[3] / a2               // fraction of color from old

	v_blend[0] = v_blend[0]*a3 + r*(1-a3)
	v_blend[1] = v_blend[1]*a3 + g*(1-a3)
	v_blend[2] = v_blend[2]*a3 + b*(1-a3)
	v_blend[3] = a2
}

// C: game/p_view.c:418 SV_CalcBlend
func (g *Game) SV_CalcBlend(ent *Edict) {
	var remaining int32
	blend := &ent.Client.PS.Blend

	blend[0], blend[1], blend[2], blend[3] = 0, 0, 0, 0

	// add for contents
	vieworg := shared.VectorAdd(ent.S.Origin, ent.Client.PS.ViewOffset)
	contents := g.gi.PointContents(&vieworg)
	if contents&(CONTENTS_LAVA|CONTENTS_SLIME|CONTENTS_WATER) != 0 {
		ent.Client.PS.RDFlags |= RDF_UNDERWATER
	} else {
		ent.Client.PS.RDFlags &^= RDF_UNDERWATER
	}

	if contents&(CONTENTS_SOLID|CONTENTS_LAVA) != 0 {
		SV_AddBlend(1.0, 0.3, 0.0, 0.6, blend)
	} else if contents&CONTENTS_SLIME != 0 {
		SV_AddBlend(0.0, 0.1, 0.05, 0.6, blend)
	} else if contents&CONTENTS_WATER != 0 {
		SV_AddBlend(0.5, 0.3, 0.2, 0.4, blend)
	}

	fn := float32(g.level.Framenum)
	// add for powerups
	if ent.Client.QuadFramenum > fn {
		remaining = int32(ent.Client.QuadFramenum - fn)
		if remaining == 30 { // beginning to fade
			g.gi.Sound(ent, CHAN_ITEM, g.gi.SoundIndex("items/damage2.wav"), 1, ATTN_NORM, 0)
		}
		if remaining > 30 || remaining&4 != 0 {
			SV_AddBlend(0, 0, 1, 0.08, blend)
		}
	} else if ent.Client.InvincibleFramenum > fn {
		remaining = int32(ent.Client.InvincibleFramenum - fn)
		if remaining == 30 { // beginning to fade
			g.gi.Sound(ent, CHAN_ITEM, g.gi.SoundIndex("items/protect2.wav"), 1, ATTN_NORM, 0)
		}
		if remaining > 30 || remaining&4 != 0 {
			SV_AddBlend(1, 1, 0, 0.08, blend)
		}
	} else if ent.Client.EnviroFramenum > fn {
		remaining = int32(ent.Client.EnviroFramenum - fn)
		if remaining == 30 { // beginning to fade
			g.gi.Sound(ent, CHAN_ITEM, g.gi.SoundIndex("items/airout.wav"), 1, ATTN_NORM, 0)
		}
		if remaining > 30 || remaining&4 != 0 {
			SV_AddBlend(0, 1, 0, 0.08, blend)
		}
	} else if ent.Client.BreatherFramenum > fn {
		remaining = int32(ent.Client.BreatherFramenum - fn)
		if remaining == 30 { // beginning to fade
			g.gi.Sound(ent, CHAN_ITEM, g.gi.SoundIndex("items/airout.wav"), 1, ATTN_NORM, 0)
		}
		if remaining > 30 || remaining&4 != 0 {
			SV_AddBlend(0.4, 1, 0.4, 0.04, blend)
		}
	}

	// add for damage
	if ent.Client.DamageAlpha > 0 {
		SV_AddBlend(ent.Client.DamageBlend[0], ent.Client.DamageBlend[1],
			ent.Client.DamageBlend[2], ent.Client.DamageAlpha, blend)
	}

	if ent.Client.BonusAlpha > 0 {
		SV_AddBlend(0.85, 0.7, 0.3, ent.Client.BonusAlpha, blend)
	}

	// drop the damage value
	ent.Client.DamageAlpha = float32(float64(ent.Client.DamageAlpha) - 0.06)
	if ent.Client.DamageAlpha < 0 {
		ent.Client.DamageAlpha = 0
	}

	// drop the bonus value
	ent.Client.BonusAlpha = float32(float64(ent.Client.BonusAlpha) - 0.1)
	if ent.Client.BonusAlpha < 0 {
		ent.Client.BonusAlpha = 0
	}
}

// C: game/p_view.c:501 P_FallingDamage
func (g *Game) P_FallingDamage(ent *Edict) {
	var delta float32
	var damage int32

	if ent.S.ModelIndex != 255 {
		return // not in the player model
	}

	if ent.Movetype == MOVETYPE_NOCLIP {
		return
	}

	if ent.Client.Oldvelocity[2] < 0 && ent.Velocity[2] > ent.Client.Oldvelocity[2] && ent.Groundentity == nil {
		delta = ent.Client.Oldvelocity[2]
	} else {
		if ent.Groundentity == nil {
			return
		}
		delta = ent.Velocity[2] - ent.Client.Oldvelocity[2]
	}
	delta = float32(float64(delta*delta) * 0.0001)

	//ZOID
	// never take damage if just release grapple or on grapple
	if g.ctfmod && (float64(g.level.Time-ent.Client.CtfGrapplereleasetime) <= FRAMETIME*2 ||
		(ent.Client.CtfGrapple != nil &&
			ent.Client.CtfGrapplestate > CTF_GRAPPLE_STATE_FLY)) {
		return
	}
	//ZOID

	// never take falling damage if completely underwater
	if ent.Waterlevel == 3 {
		return
	}
	if ent.Waterlevel == 2 {
		delta = float32(float64(delta) * 0.25)
	}
	if ent.Waterlevel == 1 {
		delta = float32(float64(delta) * 0.5)
	}

	if delta < 1 {
		return
	}

	if delta < 15 {
		ent.S.Event = EV_FOOTSTEP
		return
	}

	ent.Client.FallValue = float32(float64(delta) * 0.5)
	if ent.Client.FallValue > 40 {
		ent.Client.FallValue = 40
	}
	ent.Client.FallTime = float32(float64(g.level.Time) + FALL_TIME)

	if delta > 30 {
		if ent.Health > 0 {
			if delta >= 55 {
				ent.S.Event = EV_FALLFAR
			} else {
				ent.S.Event = EV_FALL
			}
		}
		ent.PainDebounceTime = g.level.Time // no normal pain sound
		damage = int32((delta - 30) / 2)
		if damage < 1 {
			damage = 1
		}
		dir := Vec3{0, 0, 1}

		if g.deathmatch.Value == 0 || int32(g.dmflags.Value)&DF_NO_FALLING == 0 {
			g.T_Damage(ent, g.world(), g.world(), &dir, ent.S.Origin, shared.Vec3Origin, damage, 0, 0, MOD_FALLING)
		}
	} else {
		ent.S.Event = EV_FALLSHORT
		return
	}
}

// C: game/p_view.c:579 P_WorldEffects
func (g *Game) P_WorldEffects() {
	var breather, envirosuit bool
	var waterlevel, old_waterlevel int32
	cp := g.current_player
	cc := g.current_client

	if cp.Movetype == MOVETYPE_NOCLIP {
		cp.AirFinished = g.level.Time + 12 // don't need air
		return
	}

	waterlevel = cp.Waterlevel
	old_waterlevel = cc.OldWaterlevel
	cc.OldWaterlevel = waterlevel

	fn := float32(g.level.Framenum)
	breather = cc.BreatherFramenum > fn
	envirosuit = cc.EnviroFramenum > fn

	//
	// if just entered a water volume, play a sound
	//
	if old_waterlevel == 0 && waterlevel != 0 {
		g.PlayerNoise(cp, cp.S.Origin, PNOISE_SELF)
		if cp.Watertype&CONTENTS_LAVA != 0 {
			g.gi.Sound(cp, CHAN_BODY, g.gi.SoundIndex("player/lava_in.wav"), 1, ATTN_NORM, 0)
		} else if cp.Watertype&CONTENTS_SLIME != 0 {
			g.gi.Sound(cp, CHAN_BODY, g.gi.SoundIndex("player/watr_in.wav"), 1, ATTN_NORM, 0)
		} else if cp.Watertype&CONTENTS_WATER != 0 {
			g.gi.Sound(cp, CHAN_BODY, g.gi.SoundIndex("player/watr_in.wav"), 1, ATTN_NORM, 0)
		}
		cp.Flags |= FL_INWATER

		// clear damage_debounce, so the pain sound will play immediately
		cp.DamageDebounceTime = g.level.Time - 1
	}

	//
	// if just completely exited a water volume, play a sound
	//
	if old_waterlevel != 0 && waterlevel == 0 {
		g.PlayerNoise(cp, cp.S.Origin, PNOISE_SELF)
		g.gi.Sound(cp, CHAN_BODY, g.gi.SoundIndex("player/watr_out.wav"), 1, ATTN_NORM, 0)
		cp.Flags &^= FL_INWATER
	}

	//
	// check for head just going under water
	//
	if old_waterlevel != 3 && waterlevel == 3 {
		g.gi.Sound(cp, CHAN_BODY, g.gi.SoundIndex("player/watr_un.wav"), 1, ATTN_NORM, 0)
	}

	//
	// check for head just coming out of water
	//
	if old_waterlevel == 3 && waterlevel != 3 {
		if cp.AirFinished < g.level.Time {
			// gasp for air
			g.gi.Sound(cp, CHAN_VOICE, g.gi.SoundIndex("player/gasp1.wav"), 1, ATTN_NORM, 0)
			g.PlayerNoise(cp, cp.S.Origin, PNOISE_SELF)
		} else if cp.AirFinished < g.level.Time+11 {
			// just break surface
			g.gi.Sound(cp, CHAN_VOICE, g.gi.SoundIndex("player/gasp2.wav"), 1, ATTN_NORM, 0)
		}
	}

	//
	// check for drowning
	//
	if waterlevel == 3 {
		// breather or envirosuit give air
		if breather || envirosuit {
			cp.AirFinished = g.level.Time + 10

			if int32(cc.BreatherFramenum-fn)%25 == 0 {
				if cc.BreatherSound == 0 {
					g.gi.Sound(cp, CHAN_AUTO, g.gi.SoundIndex("player/u_breath1.wav"), 1, ATTN_NORM, 0)
				} else {
					g.gi.Sound(cp, CHAN_AUTO, g.gi.SoundIndex("player/u_breath2.wav"), 1, ATTN_NORM, 0)
				}
				cc.BreatherSound ^= 1
				g.PlayerNoise(cp, cp.S.Origin, PNOISE_SELF)
				//FIXME: release a bubble?
			}
		}

		// if out of air, start drowning
		if cp.AirFinished < g.level.Time {
			// drown!
			if cp.Client.NextDrownTime < g.level.Time && cp.Health > 0 {
				cp.Client.NextDrownTime = g.level.Time + 1

				// take more damage the longer underwater
				cp.Dmg += 2
				if cp.Dmg > 15 {
					cp.Dmg = 15
				}

				// play a gurp sound instead of a normal pain sound
				if cp.Health <= cp.Dmg {
					g.gi.Sound(cp, CHAN_VOICE, g.gi.SoundIndex("player/drown1.wav"), 1, ATTN_NORM, 0)
				} else if g.rng.Rand()&1 != 0 {
					g.gi.Sound(cp, CHAN_VOICE, g.gi.SoundIndex("*gurp1.wav"), 1, ATTN_NORM, 0)
				} else {
					g.gi.Sound(cp, CHAN_VOICE, g.gi.SoundIndex("*gurp2.wav"), 1, ATTN_NORM, 0)
				}

				cp.PainDebounceTime = g.level.Time

				g.T_Damage(cp, g.world(), g.world(), &Vec3{}, cp.S.Origin, shared.Vec3Origin, cp.Dmg, 0, DAMAGE_NO_ARMOR, MOD_WATER)
			}
		}
	} else {
		cp.AirFinished = g.level.Time + 12
		cp.Dmg = 2
	}

	//
	// check for sizzle damage
	//
	if waterlevel != 0 && cp.Watertype&(CONTENTS_LAVA|CONTENTS_SLIME) != 0 {
		if cp.Watertype&CONTENTS_LAVA != 0 {
			if cp.Health > 0 && cp.PainDebounceTime <= g.level.Time && cc.InvincibleFramenum < fn {
				if g.rng.Rand()&1 != 0 {
					g.gi.Sound(cp, CHAN_VOICE, g.gi.SoundIndex("player/burn1.wav"), 1, ATTN_NORM, 0)
				} else {
					g.gi.Sound(cp, CHAN_VOICE, g.gi.SoundIndex("player/burn2.wav"), 1, ATTN_NORM, 0)
				}
				cp.PainDebounceTime = g.level.Time + 1
			}

			if envirosuit { // take 1/3 damage with envirosuit
				g.T_Damage(cp, g.world(), g.world(), &Vec3{}, cp.S.Origin, shared.Vec3Origin, 1*waterlevel, 0, 0, MOD_LAVA)
			} else {
				g.T_Damage(cp, g.world(), g.world(), &Vec3{}, cp.S.Origin, shared.Vec3Origin, 3*waterlevel, 0, 0, MOD_LAVA)
			}
		}

		if cp.Watertype&CONTENTS_SLIME != 0 {
			if !envirosuit {
				// no damage from slime with envirosuit
				g.T_Damage(cp, g.world(), g.world(), &Vec3{}, cp.S.Origin, shared.Vec3Origin, 1*waterlevel, 0, 0, MOD_SLIME)
			}
		}
	}
}

// C: game/p_view.c:745 G_SetClientEffects
func (g *Game) G_SetClientEffects(ent *Edict) {
	var pa_type, remaining int32

	ent.S.Effects = 0
	ent.S.RenderFX = 0

	if ent.Health <= 0 || g.level.Intermissiontime != 0 {
		return
	}

	if ent.PowerarmorTime > g.level.Time {
		pa_type = g.PowerArmorType(ent)
		if pa_type == POWER_ARMOR_SCREEN {
			ent.S.Effects |= EF_POWERSCREEN
		} else if pa_type == POWER_ARMOR_SHIELD {
			ent.S.Effects |= EF_COLOR_SHELL
			ent.S.RenderFX |= RF_SHELL_GREEN
		}
	}

	//ZOID
	if g.ctfmod {
		g.CTFEffects(ent)
	}
	//ZOID

	// the ctf fork blinks quad/pent every 8 frames
	blink := !g.ctfmod || g.level.Framenum&8 != 0

	fn := float32(g.level.Framenum)
	if ent.Client.QuadFramenum > fn && blink {
		remaining = int32(ent.Client.QuadFramenum - fn)
		if remaining > 30 || remaining&4 != 0 {
			ent.S.Effects |= EF_QUAD
		}
	}

	if ent.Client.InvincibleFramenum > fn && blink {
		remaining = int32(ent.Client.InvincibleFramenum - fn)
		if remaining > 30 || remaining&4 != 0 {
			ent.S.Effects |= EF_PENT
		}
	}

	// show cheaters!!!
	if ent.Flags&FL_GODMODE != 0 {
		ent.S.Effects |= EF_COLOR_SHELL
		ent.S.RenderFX |= (RF_SHELL_RED | RF_SHELL_GREEN | RF_SHELL_BLUE)
	}
}

// C: game/p_view.c:798 G_SetClientEvent
func (g *Game) G_SetClientEvent(ent *Edict) {
	if ent.S.Event != 0 {
		return
	}

	if ent.Groundentity != nil && g.xyspeed > 225 {
		if int32(g.current_client.Bobtime+g.bobmove) != g.bobcycle {
			ent.S.Event = EV_FOOTSTEP
		}
	}
}

// C: game/p_view.c:815 G_SetClientSound
func (g *Game) G_SetClientSound(ent *Edict) {
	var weap string

	// the ctf fork keeps game_helpchanged/helpchanged in client_respawn_t
	gameHelpchanged, helpchanged := &ent.Client.Pers.GameHelpchanged, &ent.Client.Pers.Helpchanged
	if g.ctfmod {
		gameHelpchanged, helpchanged = &ent.Client.Resp.GameHelpchanged, &ent.Client.Resp.Helpchanged
	}
	if *gameHelpchanged != g.game.Helpchanged {
		*gameHelpchanged = g.game.Helpchanged
		*helpchanged = 1
	}

	// help beep (no more than three times)
	if *helpchanged != 0 && *helpchanged <= 3 && g.level.Framenum&63 == 0 {
		*helpchanged++
		g.gi.Sound(ent, CHAN_VOICE, g.gi.SoundIndex("misc/pc_up.wav"), 1, ATTN_STATIC, 0)
	}

	if ent.Client.Pers.Weapon != nil {
		weap = ent.Client.Pers.Weapon.Classname
	} else {
		weap = ""
	}

	if ent.Waterlevel != 0 && ent.Watertype&(CONTENTS_LAVA|CONTENTS_SLIME) != 0 {
		ent.S.Sound = g.snd_fry
	} else if weap == "weapon_railgun" {
		ent.S.Sound = int32(g.gi.SoundIndex("weapons/rg_hum.wav"))
	} else if weap == "weapon_bfg" {
		ent.S.Sound = int32(g.gi.SoundIndex("weapons/bfg_hum.wav"))
	} else if ent.Client.WeaponSound != 0 {
		ent.S.Sound = ent.Client.WeaponSound
	} else {
		ent.S.Sound = 0
	}
}

// C: game/p_view.c:855 G_SetClientFrame
func (g *Game) G_SetClientFrame(ent *Edict) {
	var duck, run bool

	if ent.S.ModelIndex != 255 {
		return // not in the player model
	}

	client := ent.Client

	duck = client.PS.PMove.PmFlags&PMF_DUCKED != 0
	run = g.xyspeed != 0

	newanim := false
	// check for stand/duck and stop/go transitions
	if duck != client.AnimDuck && client.AnimPriority < ANIM_DEATH {
		newanim = true
	} else if run != client.AnimRun && client.AnimPriority == ANIM_BASIC {
		newanim = true
	} else if ent.Groundentity == nil && client.AnimPriority <= ANIM_WAVE {
		newanim = true
	}

	if !newanim {
		if client.AnimPriority == ANIM_REVERSE {
			if ent.S.Frame > client.AnimEnd {
				ent.S.Frame--
				return
			}
		} else if ent.S.Frame < client.AnimEnd {
			// continue an animation
			ent.S.Frame++
			return
		}

		if client.AnimPriority == ANIM_DEATH {
			return // stay there
		}
		if client.AnimPriority == ANIM_JUMP {
			if ent.Groundentity == nil {
				return // stay there
			}
			ent.Client.AnimPriority = ANIM_WAVE
			ent.S.Frame = FRAME_jump3
			ent.Client.AnimEnd = FRAME_jump6
			return
		}
	}

	// newanim:
	// return to either a running or standing frame
	client.AnimPriority = ANIM_BASIC
	client.AnimDuck = duck
	client.AnimRun = run

	if ent.Groundentity == nil {
		//ZOID: if on grapple, don't go into jump frame, go into standing
		//frame
		if g.ctfmod && client.CtfGrapple != nil {
			ent.S.Frame = FRAME_stand01
			client.AnimEnd = FRAME_stand40
		} else {
			//ZOID
			client.AnimPriority = ANIM_JUMP
			if ent.S.Frame != FRAME_jump2 {
				ent.S.Frame = FRAME_jump1
			}
			client.AnimEnd = FRAME_jump2
		}
	} else if run {
		// running
		if duck {
			ent.S.Frame = FRAME_crwalk1
			client.AnimEnd = FRAME_crwalk6
		} else {
			ent.S.Frame = FRAME_run1
			client.AnimEnd = FRAME_run6
		}
	} else {
		// standing
		if duck {
			ent.S.Frame = FRAME_crstnd01
			client.AnimEnd = FRAME_crstnd19
		} else {
			ent.S.Frame = FRAME_stand01
			client.AnimEnd = FRAME_stand40
		}
	}
}

// ClientEndServerFrame is called for each player at the end of the server
// frame and right after spawning.
// C: game/p_view.c:958 ClientEndServerFrame
func (g *Game) ClientEndServerFrame(ent *Edict) {
	var bobtime float32

	g.current_player = ent
	g.current_client = ent.Client
	cc := g.current_client

	//
	// If the origin or velocity have changed since ClientThink(),
	// update the pmove values.  This will happen when the client
	// is pushed by a bmodel or kicked by an explosion.
	//
	// If it wasn't updated here, the view position would lag a frame
	// behind the body position when pushed -- "sinking into plats"
	//
	for i := 0; i < 3; i++ {
		cc.PS.PMove.Origin[i] = int16(int32(float64(ent.S.Origin[i]) * 8.0))
		cc.PS.PMove.Velocity[i] = int16(int32(float64(ent.Velocity[i]) * 8.0))
	}

	//
	// If the end of unit layout is displayed, don't give
	// the player any normal movement attributes
	//
	if g.level.Intermissiontime != 0 {
		// FIXME: add view drifting here?
		cc.PS.Blend[3] = 0
		cc.PS.Fov = 90
		g.G_SetStats(ent)
		return
	}

	shared.AngleVectors(ent.Client.VAngle, &g.forward, &g.right, &g.up)

	// burn from lava, etc
	g.P_WorldEffects()

	//
	// set model angles from view angles so other things in
	// the world can tell which direction you are looking
	//
	if ent.Client.VAngle[PITCH] > 180 {
		ent.S.Angles[PITCH] = (-360 + ent.Client.VAngle[PITCH]) / 3
	} else {
		ent.S.Angles[PITCH] = ent.Client.VAngle[PITCH] / 3
	}
	ent.S.Angles[YAW] = ent.Client.VAngle[YAW]
	ent.S.Angles[ROLL] = 0
	ent.S.Angles[ROLL] = g.SV_CalcRoll(ent.S.Angles, ent.Velocity) * 4

	//
	// calculate speed and cycle to be used for
	// all cyclic walking effects
	//
	g.xyspeed = float32(math.Sqrt(float64(ent.Velocity[0]*ent.Velocity[0] + ent.Velocity[1]*ent.Velocity[1])))

	if g.xyspeed < 5 {
		g.bobmove = 0
		cc.Bobtime = 0 // start at beginning of cycle again
	} else if ent.Groundentity != nil {
		// so bobbing only cycles when on ground
		if g.xyspeed > 210 {
			g.bobmove = 0.25
		} else if g.xyspeed > 100 {
			g.bobmove = 0.125
		} else {
			g.bobmove = 0.0625
		}
	}

	cc.Bobtime += g.bobmove
	bobtime = cc.Bobtime

	if cc.PS.PMove.PmFlags&PMF_DUCKED != 0 {
		bobtime *= 4
	}

	g.bobcycle = int32(bobtime)
	g.bobfracsin = float32(math.Abs(math.Sin(float64(bobtime) * shared.MPI)))

	// detect hitting the floor
	g.P_FallingDamage(ent)

	// apply all the damage taken this frame
	g.P_DamageFeedback(ent)

	// determine the view offsets
	g.SV_CalcViewOffset(ent)

	// determine the gun offsets
	g.SV_CalcGunOffset(ent)

	// determine the full screen color blend
	// must be after viewoffset, so eye contents can be
	// accurately determined
	// FIXME: with client prediction, the contents
	// should be determined by the client
	g.SV_CalcBlend(ent)

	if g.ctfmod {
		//ZOID
		if ent.Client.ChaseTarget == nil {
			//ZOID
			g.G_SetStats(ent)
		}

		//ZOID
		//update chasecam follower stats
		for i := 1; float32(i) <= g.maxclients.Value; i++ {
			e := &g.edicts[i]
			if !e.InUse || e.Client.ChaseTarget != ent {
				continue
			}
			e.Client.PS.Stats = ent.Client.PS.Stats
			e.Client.PS.Stats[STAT_LAYOUTS] = 1
			break
		}
		//ZOID
	} else {
		// chase cam stuff
		if ent.Client.Resp.Spectator {
			g.G_SetSpectatorStats(ent)
		} else {
			g.G_SetStats(ent)
		}
		g.G_CheckChaseStats(ent)
	}

	g.G_SetClientEvent(ent)

	g.G_SetClientEffects(ent)

	g.G_SetClientSound(ent)

	g.G_SetClientFrame(ent)

	ent.Client.Oldvelocity = ent.Velocity
	ent.Client.Oldviewangles = ent.Client.PS.ViewAngles

	// clear weapon kicks
	ent.Client.KickOrigin = Vec3{}
	ent.Client.KickAngles = Vec3{}

	// if the scoreboard is up, update it
	if ent.Client.Showscores && g.level.Framenum&31 == 0 {
		//ZOID
		if g.ctfmod && ent.Client.Menu != nil {
			g.PMenu_Do_Update(ent)
			ent.Client.Menudirty = false
			ent.Client.Menutime = g.level.Time
		} else {
			//ZOID
			g.DeathmatchScoreboardMessage(ent, ent.Enemy)
		}
		g.gi.Unicast(ent, false)
	}
}
