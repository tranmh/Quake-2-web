package game

// Port of game/p_weapon.c (the file header says "g_weapon.c"): player weapons.

import (
	"math"

	. "quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/shared"
)

// Handles of the functions stored in gitem_t (see itemlist in g_items.go).
var (
	Pickup_Weapon          = defPickup("Pickup_Weapon")
	Use_Weapon             = defItem("Use_Weapon")
	Drop_Weapon            = defItem("Drop_Weapon")
	Weapon_Blaster         = defThink("Weapon_Blaster")
	Weapon_Shotgun         = defThink("Weapon_Shotgun")
	Weapon_SuperShotgun    = defThink("Weapon_SuperShotgun")
	Weapon_Machinegun      = defThink("Weapon_Machinegun")
	Weapon_Chaingun        = defThink("Weapon_Chaingun")
	Weapon_HyperBlaster    = defThink("Weapon_HyperBlaster")
	Weapon_RocketLauncher  = defThink("Weapon_RocketLauncher")
	Weapon_Grenade         = defThink("Weapon_Grenade")
	Weapon_GrenadeLauncher = defThink("Weapon_GrenadeLauncher")
	Weapon_Railgun         = defThink("Weapon_Railgun")
	Weapon_BFG             = defThink("Weapon_BFG")
)

func init() {
	Pickup_Weapon.bind((*Game).Pickup_Weapon)
	Use_Weapon.bind((*Game).Use_Weapon)
	Drop_Weapon.bind((*Game).Drop_Weapon)
	Weapon_Blaster.bind((*Game).Weapon_Blaster)
	Weapon_Shotgun.bind((*Game).Weapon_Shotgun)
	Weapon_SuperShotgun.bind((*Game).Weapon_SuperShotgun)
	Weapon_Machinegun.bind((*Game).Weapon_Machinegun)
	Weapon_Chaingun.bind((*Game).Weapon_Chaingun)
	Weapon_HyperBlaster.bind((*Game).Weapon_HyperBlaster)
	Weapon_RocketLauncher.bind((*Game).Weapon_RocketLauncher)
	Weapon_Grenade.bind((*Game).Weapon_Grenade)
	Weapon_GrenadeLauncher.bind((*Game).Weapon_GrenadeLauncher)
	Weapon_Railgun.bind((*Game).Weapon_Railgun)
	Weapon_BFG.bind((*Game).Weapon_BFG)
}

// C: game/p_weapon.c:33 P_ProjectSource
func P_ProjectSource(client *GClient, point, distance, forward, right Vec3) Vec3 {
	_distance := distance
	if client.Pers.Hand == LEFT_HANDED {
		_distance[1] *= -1
	} else if client.Pers.Hand == CENTER_HANDED {
		_distance[1] = 0
	}
	return G_ProjectSource(point, _distance, forward, right)
}

// PlayerNoise: each player can have two noise objects associated with it:
// a personal noise (jumping, pain, weapon firing), and a weapon
// target noise (bullet wall impacts)
//
// Monsters that don't directly see the player can move
// to a noise in hopes of seeing the player from there.
// C: game/p_weapon.c:58 PlayerNoise
func (g *Game) PlayerNoise(who *Edict, where Vec3, type_ int32) {
	var noise *Edict

	if type_ == PNOISE_WEAPON {
		if who.Client.SilencerShots != 0 {
			who.Client.SilencerShots--
			return
		}
	}

	if g.deathmatch.Value != 0 {
		return
	}

	if who.Flags&FL_NOTARGET != 0 {
		return
	}

	if who.Mynoise == nil {
		noise = g.G_Spawn()
		noise.Classname = "player_noise"
		noise.Mins = Vec3{-8, -8, -8}
		noise.Maxs = Vec3{8, 8, 8}
		noise.Owner = who
		noise.SVFlags = SVF_NOCLIENT
		who.Mynoise = noise

		noise = g.G_Spawn()
		noise.Classname = "player_noise"
		noise.Mins = Vec3{-8, -8, -8}
		noise.Maxs = Vec3{8, 8, 8}
		noise.Owner = who
		noise.SVFlags = SVF_NOCLIENT
		who.Mynoise2 = noise
	}

	if type_ == PNOISE_SELF || type_ == PNOISE_WEAPON {
		noise = who.Mynoise
		g.level.SoundEntity = noise
		g.level.SoundEntityFramenum = g.level.Framenum
	} else { // type == PNOISE_IMPACT
		noise = who.Mynoise2
		g.level.Sound2Entity = noise
		g.level.Sound2EntityFramenum = g.level.Framenum
	}

	noise.S.Origin = where
	noise.AbsMin = shared.VectorSubtract(where, noise.Maxs)
	noise.AbsMax = shared.VectorAdd(where, noise.Maxs)
	noise.TeleportTime = g.level.Time
	g.gi.LinkEntity(noise)
}

// C: game/p_weapon.c:118 Pickup_Weapon
func (g *Game) Pickup_Weapon(ent, other *Edict) bool {
	var index int32
	var ammo *GItem

	index = ITEM_INDEX(ent.Item)

	if (int32(g.dmflags.Value)&DF_WEAPONS_STAY != 0 || g.coop.Value != 0) &&
		other.Client.Pers.Inventory[index] != 0 {
		if ent.Spawnflags&(DROPPED_ITEM|DROPPED_PLAYER_ITEM) == 0 {
			return false // leave the weapon for others to pickup
		}
	}

	other.Client.Pers.Inventory[index]++

	if ent.Spawnflags&DROPPED_ITEM == 0 {
		// give them some ammo with it
		ammo = g.FindItem(ent.Item.Ammo)
		if int32(g.dmflags.Value)&DF_INFINITE_AMMO != 0 {
			g.Add_Ammo(other, ammo, 1000)
		} else {
			g.Add_Ammo(other, ammo, ammo.Quantity)
		}

		if ent.Spawnflags&DROPPED_PLAYER_ITEM == 0 {
			if g.deathmatch.Value != 0 {
				if int32(g.dmflags.Value)&DF_WEAPONS_STAY != 0 {
					ent.Flags |= FL_RESPAWN
				} else {
					g.SetRespawn(ent, 30)
				}
			}
			if g.coop.Value != 0 {
				ent.Flags |= FL_RESPAWN
			}
		}
	}

	if other.Client.Pers.Weapon != ent.Item &&
		other.Client.Pers.Inventory[index] == 1 &&
		(g.deathmatch.Value == 0 || other.Client.Pers.Weapon == g.FindItem("blaster")) {
		other.Client.Newweapon = ent.Item
	}

	return true
}

// ChangeWeapon: the old weapon has been dropped all the way, so make the new
// one current
// C: game/p_weapon.c:174 ChangeWeapon
func (g *Game) ChangeWeapon(ent *Edict) {
	var i int32

	if ent.Client.GrenadeTime != 0 {
		ent.Client.GrenadeTime = g.level.Time
		ent.Client.WeaponSound = 0
		g.weapon_grenade_fire(ent, false)
		ent.Client.GrenadeTime = 0
	}

	ent.Client.Pers.Lastweapon = ent.Client.Pers.Weapon
	ent.Client.Pers.Weapon = ent.Client.Newweapon
	ent.Client.Newweapon = nil
	ent.Client.MachinegunShots = 0

	// set visible model
	if ent.S.ModelIndex == 255 {
		if ent.Client.Pers.Weapon != nil {
			i = (ent.Client.Pers.Weapon.Weapmodel & 0xff) << 8
		} else {
			i = 0
		}
		ent.S.SkinNum = int32(ent.Index-1) | i
	}

	if ent.Client.Pers.Weapon != nil && ent.Client.Pers.Weapon.Ammo != "" {
		ent.Client.AmmoIndex = ITEM_INDEX(g.FindItem(ent.Client.Pers.Weapon.Ammo))
	} else {
		ent.Client.AmmoIndex = 0
	}

	if ent.Client.Pers.Weapon == nil { // dead
		ent.Client.PS.GunIndex = 0
		return
	}

	ent.Client.Weaponstate = WEAPON_ACTIVATING
	ent.Client.PS.GunFrame = 0
	ent.Client.PS.GunIndex = int32(g.gi.ModelIndex(ent.Client.Pers.Weapon.ViewModel))

	ent.Client.AnimPriority = ANIM_PAIN
	if ent.Client.PS.PMove.PmFlags&PMF_DUCKED != 0 {
		ent.S.Frame = FRAME_crpain1
		ent.Client.AnimEnd = FRAME_crpain4
	} else {
		ent.S.Frame = FRAME_pain301
		ent.Client.AnimEnd = FRAME_pain304
	}
}

// C: game/p_weapon.c:234 NoAmmoWeaponChange
func (g *Game) NoAmmoWeaponChange(ent *Edict) {
	inv := &ent.Client.Pers.Inventory
	if inv[ITEM_INDEX(g.FindItem("slugs"))] != 0 &&
		inv[ITEM_INDEX(g.FindItem("railgun"))] != 0 {
		ent.Client.Newweapon = g.FindItem("railgun")
		return
	}
	if inv[ITEM_INDEX(g.FindItem("cells"))] != 0 &&
		inv[ITEM_INDEX(g.FindItem("hyperblaster"))] != 0 {
		ent.Client.Newweapon = g.FindItem("hyperblaster")
		return
	}
	if inv[ITEM_INDEX(g.FindItem("bullets"))] != 0 &&
		inv[ITEM_INDEX(g.FindItem("chaingun"))] != 0 {
		ent.Client.Newweapon = g.FindItem("chaingun")
		return
	}
	if inv[ITEM_INDEX(g.FindItem("bullets"))] != 0 &&
		inv[ITEM_INDEX(g.FindItem("machinegun"))] != 0 {
		ent.Client.Newweapon = g.FindItem("machinegun")
		return
	}
	if inv[ITEM_INDEX(g.FindItem("shells"))] > 1 &&
		inv[ITEM_INDEX(g.FindItem("super shotgun"))] != 0 {
		ent.Client.Newweapon = g.FindItem("super shotgun")
		return
	}
	if inv[ITEM_INDEX(g.FindItem("shells"))] != 0 &&
		inv[ITEM_INDEX(g.FindItem("shotgun"))] != 0 {
		ent.Client.Newweapon = g.FindItem("shotgun")
		return
	}
	ent.Client.Newweapon = g.FindItem("blaster")
}

// Think_Weapon is called by ClientBeginServerFrame and ClientThink.
// C: game/p_weapon.c:282 Think_Weapon
func (g *Game) Think_Weapon(ent *Edict) {
	// if just died, put the weapon away
	if ent.Health < 1 {
		ent.Client.Newweapon = nil
		g.ChangeWeapon(ent)
	}

	// call active weapon think routine
	if ent.Client.Pers.Weapon != nil && ent.Client.Pers.Weapon.Weaponthink != nil {
		g.is_quad = ent.Client.QuadFramenum > float32(g.level.Framenum)
		if ent.Client.SilencerShots != 0 {
			g.is_silenced = MZ_SILENCED
		} else {
			g.is_silenced = 0
		}
		ent.Client.Pers.Weapon.Weaponthink.fn(g, ent)
	}
}

// Use_Weapon: make the weapon ready if there is ammo
// C: game/p_weapon.c:311 Use_Weapon
func (g *Game) Use_Weapon(ent *Edict, item *GItem) {
	var ammo_index int32
	var ammo_item *GItem

	// see if we're already using it
	if item == ent.Client.Pers.Weapon {
		return
	}

	if item.Ammo != "" && g.g_select_empty.Value == 0 && item.Flags&IT_AMMO == 0 {
		ammo_item = g.FindItem(item.Ammo)
		ammo_index = ITEM_INDEX(ammo_item)

		if ent.Client.Pers.Inventory[ammo_index] == 0 {
			g.cprintf(ent, PRINT_HIGH, "No %s for %s.\n", ammo_item.PickupName, item.PickupName)
			return
		}

		if ent.Client.Pers.Inventory[ammo_index] < item.Quantity {
			g.cprintf(ent, PRINT_HIGH, "Not enough %s for %s.\n", ammo_item.PickupName, item.PickupName)
			return
		}
	}

	// change to this weapon when down
	ent.Client.Newweapon = item
}

// C: game/p_weapon.c:349 Drop_Weapon
func (g *Game) Drop_Weapon(ent *Edict, item *GItem) {
	var index int32

	if int32(g.dmflags.Value)&DF_WEAPONS_STAY != 0 {
		return
	}

	index = ITEM_INDEX(item)
	// see if we're already using it
	if (item == ent.Client.Pers.Weapon || item == ent.Client.Newweapon) && ent.Client.Pers.Inventory[index] == 1 {
		g.gi.Cprintf(ent, PRINT_HIGH, "Can't drop current weapon\n")
		return
	}

	g.Drop_Item(ent, item)
	ent.Client.Pers.Inventory[index]--
}

// Weapon_Generic: a generic function to handle the basics of weapon thinking.
// pause_frames and fire_frames are 0-terminated like the C arrays; a nil
// pause_frames is the C NULL.
// In the ctf module this body is Weapon_Generic2 and Weapon_Generic is the
// haste/grapple wrapper below.
// C: game/p_weapon.c:380 Weapon_Generic, ctf/p_weapon.c:380 Weapon_Generic2
func (g *Game) Weapon_Generic(ent *Edict, FRAME_ACTIVATE_LAST, FRAME_FIRE_LAST, FRAME_IDLE_LAST, FRAME_DEACTIVATE_LAST int32, pause_frames, fire_frames []int32, fire func(g *Game, ent *Edict)) {
	if g.ctfmod {
		g.ctfWeapon_Generic(ent, FRAME_ACTIVATE_LAST, FRAME_FIRE_LAST, FRAME_IDLE_LAST, FRAME_DEACTIVATE_LAST, pause_frames, fire_frames, fire)
		return
	}
	g.Weapon_Generic2(ent, FRAME_ACTIVATE_LAST, FRAME_FIRE_LAST, FRAME_IDLE_LAST, FRAME_DEACTIVATE_LAST, pause_frames, fire_frames, fire)
}

// ctfWeapon_Generic runs the weapon frame again if hasted (and for the
// grapple when not firing).
// C: ctf/p_weapon.c:548 Weapon_Generic
func (g *Game) ctfWeapon_Generic(ent *Edict, FRAME_ACTIVATE_LAST, FRAME_FIRE_LAST, FRAME_IDLE_LAST, FRAME_DEACTIVATE_LAST int32, pause_frames, fire_frames []int32, fire func(g *Game, ent *Edict)) {
	oldstate := ent.Client.Weaponstate

	g.Weapon_Generic2(ent, FRAME_ACTIVATE_LAST, FRAME_FIRE_LAST,
		FRAME_IDLE_LAST, FRAME_DEACTIVATE_LAST, pause_frames,
		fire_frames, fire)

	// run the weapon frame again if hasted
	if shared.Q_stricmp(ent.Client.Pers.Weapon.PickupName, "Grapple") == 0 &&
		ent.Client.Weaponstate == WEAPON_FIRING {
		return
	}

	if (g.CTFApplyHaste(ent) ||
		(shared.Q_stricmp(ent.Client.Pers.Weapon.PickupName, "Grapple") == 0 &&
			ent.Client.Weaponstate != WEAPON_FIRING)) &&
		oldstate == ent.Client.Weaponstate {
		g.Weapon_Generic2(ent, FRAME_ACTIVATE_LAST, FRAME_FIRE_LAST,
			FRAME_IDLE_LAST, FRAME_DEACTIVATE_LAST, pause_frames,
			fire_frames, fire)
	}
}

// Weapon_Generic2 is the generic weapon frame (Weapon_Generic of 3.19).
// C: game/p_weapon.c:380 Weapon_Generic, ctf/p_weapon.c:380 Weapon_Generic2
func (g *Game) Weapon_Generic2(ent *Edict, FRAME_ACTIVATE_LAST, FRAME_FIRE_LAST, FRAME_IDLE_LAST, FRAME_DEACTIVATE_LAST int32, pause_frames, fire_frames []int32, fire func(g *Game, ent *Edict)) {
	FRAME_FIRE_FIRST := FRAME_ACTIVATE_LAST + 1
	FRAME_IDLE_FIRST := FRAME_FIRE_LAST + 1
	FRAME_DEACTIVATE_FIRST := FRAME_IDLE_LAST + 1

	var n int

	if ent.Deadflag != 0 || ent.S.ModelIndex != 255 { // VWep animations screw up corpses
		return
	}

	if ent.Client.Weaponstate == WEAPON_DROPPING {
		if ent.Client.PS.GunFrame == FRAME_DEACTIVATE_LAST {
			g.ChangeWeapon(ent)
			return
		} else if (FRAME_DEACTIVATE_LAST - ent.Client.PS.GunFrame) == 4 {
			ent.Client.AnimPriority = ANIM_REVERSE
			if ent.Client.PS.PMove.PmFlags&PMF_DUCKED != 0 {
				ent.S.Frame = FRAME_crpain4 + 1
				ent.Client.AnimEnd = FRAME_crpain1
			} else {
				ent.S.Frame = FRAME_pain304 + 1
				ent.Client.AnimEnd = FRAME_pain301
			}
		}

		ent.Client.PS.GunFrame++
		return
	}

	if ent.Client.Weaponstate == WEAPON_ACTIVATING {
		if ent.Client.PS.GunFrame == FRAME_ACTIVATE_LAST || (g.ctfmod && g.instantweap.Value != 0) {
			ent.Client.Weaponstate = WEAPON_READY
			ent.Client.PS.GunFrame = FRAME_IDLE_FIRST
			if g.ctfmod {
				// we go recursive here to instant ready the weapon
				g.Weapon_Generic2(ent, FRAME_ACTIVATE_LAST, FRAME_FIRE_LAST,
					FRAME_IDLE_LAST, FRAME_DEACTIVATE_LAST, pause_frames,
					fire_frames, fire)
			}
			return
		}

		ent.Client.PS.GunFrame++
		return
	}

	if ent.Client.Newweapon != nil && ent.Client.Weaponstate != WEAPON_FIRING {
		ent.Client.Weaponstate = WEAPON_DROPPING
		if g.ctfmod && g.instantweap.Value != 0 {
			g.ChangeWeapon(ent)
			return
		}
		ent.Client.PS.GunFrame = FRAME_DEACTIVATE_FIRST

		if (FRAME_DEACTIVATE_LAST - FRAME_DEACTIVATE_FIRST) < 4 {
			ent.Client.AnimPriority = ANIM_REVERSE
			if ent.Client.PS.PMove.PmFlags&PMF_DUCKED != 0 {
				ent.S.Frame = FRAME_crpain4 + 1
				ent.Client.AnimEnd = FRAME_crpain1
			} else {
				ent.S.Frame = FRAME_pain304 + 1
				ent.Client.AnimEnd = FRAME_pain301
			}
		}
		return
	}

	if ent.Client.Weaponstate == WEAPON_READY {
		if (ent.Client.LatchedButtons|ent.Client.Buttons)&BUTTON_ATTACK != 0 {
			ent.Client.LatchedButtons &^= BUTTON_ATTACK
			if ent.Client.AmmoIndex == 0 ||
				ent.Client.Pers.Inventory[ent.Client.AmmoIndex] >= ent.Client.Pers.Weapon.Quantity {
				ent.Client.PS.GunFrame = FRAME_FIRE_FIRST
				ent.Client.Weaponstate = WEAPON_FIRING

				// start the animation
				ent.Client.AnimPriority = ANIM_ATTACK
				if ent.Client.PS.PMove.PmFlags&PMF_DUCKED != 0 {
					ent.S.Frame = FRAME_crattak1 - 1
					ent.Client.AnimEnd = FRAME_crattak9
				} else {
					ent.S.Frame = FRAME_attack1 - 1
					ent.Client.AnimEnd = FRAME_attack8
				}
			} else {
				if g.level.Time >= ent.PainDebounceTime {
					g.gi.Sound(ent, CHAN_VOICE, g.gi.SoundIndex("weapons/noammo.wav"), 1, ATTN_NORM, 0)
					ent.PainDebounceTime = g.level.Time + 1
				}
				g.NoAmmoWeaponChange(ent)
			}
		} else {
			if ent.Client.PS.GunFrame == FRAME_IDLE_LAST {
				ent.Client.PS.GunFrame = FRAME_IDLE_FIRST
				return
			}

			if pause_frames != nil {
				for n = 0; pause_frames[n] != 0; n++ {
					if ent.Client.PS.GunFrame == pause_frames[n] {
						if g.rng.Rand()&15 != 0 {
							return
						}
					}
				}
			}

			ent.Client.PS.GunFrame++
			return
		}
	}

	if ent.Client.Weaponstate == WEAPON_FIRING {
		for n = 0; fire_frames[n] != 0; n++ {
			if ent.Client.PS.GunFrame == fire_frames[n] {
				//ZOID
				if !(g.ctfmod && g.CTFApplyStrengthSound(ent)) {
					//ZOID
					if ent.Client.QuadFramenum > float32(g.level.Framenum) {
						g.gi.Sound(ent, CHAN_ITEM, g.gi.SoundIndex("items/damage3.wav"), 1, ATTN_NORM, 0)
					}
				}
				//ZOID
				if g.ctfmod {
					g.CTFApplyHasteSound(ent)
				}
				//ZOID

				fire(g, ent)
				break
			}
		}

		if fire_frames[n] == 0 {
			ent.Client.PS.GunFrame++
		}

		if ent.Client.PS.GunFrame == FRAME_IDLE_FIRST+1 {
			ent.Client.Weaponstate = WEAPON_READY
		}
	}
}

/*
======================================================================

GRENADE

======================================================================
*/

// C: game/p_weapon.c:542
const (
	GRENADE_TIMER    = 3.0 // double
	GRENADE_MINSPEED = 400
	GRENADE_MAXSPEED = 800
)

// C: game/p_weapon.c:546 weapon_grenade_fire
func (g *Game) weapon_grenade_fire(ent *Edict, held bool) {
	var offset Vec3
	var forward, right Vec3
	var start Vec3
	var damage int32 = 125
	var timer float32
	var speed int32
	var radius float32

	radius = float32(damage + 40)
	if g.is_quad {
		damage *= 4
	}

	offset = Vec3{8, 8, float32(ent.Viewheight - 8)}
	shared.AngleVectors(ent.Client.VAngle, &forward, &right, nil)
	start = P_ProjectSource(ent.Client, ent.S.Origin, offset, forward, right)

	timer = ent.Client.GrenadeTime - g.level.Time
	speed = int32(GRENADE_MINSPEED + (GRENADE_TIMER-float64(timer))*((GRENADE_MAXSPEED-GRENADE_MINSPEED)/GRENADE_TIMER))
	g.fire_grenade2(ent, start, forward, damage, speed, timer, radius, held)

	if int32(g.dmflags.Value)&DF_INFINITE_AMMO == 0 {
		ent.Client.Pers.Inventory[ent.Client.AmmoIndex]--
	}

	ent.Client.GrenadeTime = float32(float64(g.level.Time) + 1.0)

	if ent.Deadflag != 0 || ent.S.ModelIndex != 255 { // VWep animations screw up corpses
		return
	}

	if !g.ctfmod && ent.Health <= 0 { // no health check in the ctf fork's older base
		return
	}

	if ent.Client.PS.PMove.PmFlags&PMF_DUCKED != 0 {
		ent.Client.AnimPriority = ANIM_ATTACK
		ent.S.Frame = FRAME_crattak1 - 1
		ent.Client.AnimEnd = FRAME_crattak3
	} else {
		ent.Client.AnimPriority = ANIM_REVERSE
		ent.S.Frame = FRAME_wave08
		ent.Client.AnimEnd = FRAME_wave01
	}
}

// C: game/p_weapon.c:595 Weapon_Grenade
func (g *Game) Weapon_Grenade(ent *Edict) {
	if ent.Client.Newweapon != nil && ent.Client.Weaponstate == WEAPON_READY {
		g.ChangeWeapon(ent)
		return
	}

	if ent.Client.Weaponstate == WEAPON_ACTIVATING {
		ent.Client.Weaponstate = WEAPON_READY
		ent.Client.PS.GunFrame = 16
		return
	}

	if ent.Client.Weaponstate == WEAPON_READY {
		if (ent.Client.LatchedButtons|ent.Client.Buttons)&BUTTON_ATTACK != 0 {
			ent.Client.LatchedButtons &^= BUTTON_ATTACK
			if ent.Client.Pers.Inventory[ent.Client.AmmoIndex] != 0 {
				ent.Client.PS.GunFrame = 1
				ent.Client.Weaponstate = WEAPON_FIRING
				ent.Client.GrenadeTime = 0
			} else {
				if g.level.Time >= ent.PainDebounceTime {
					g.gi.Sound(ent, CHAN_VOICE, g.gi.SoundIndex("weapons/noammo.wav"), 1, ATTN_NORM, 0)
					ent.PainDebounceTime = g.level.Time + 1
				}
				g.NoAmmoWeaponChange(ent)
			}
			return
		}

		if ent.Client.PS.GunFrame == 29 || ent.Client.PS.GunFrame == 34 || ent.Client.PS.GunFrame == 39 || ent.Client.PS.GunFrame == 48 {
			if g.rng.Rand()&15 != 0 {
				return
			}
		}

		ent.Client.PS.GunFrame++
		if ent.Client.PS.GunFrame > 48 {
			ent.Client.PS.GunFrame = 16
		}
		return
	}

	if ent.Client.Weaponstate == WEAPON_FIRING {
		if ent.Client.PS.GunFrame == 5 {
			g.gi.Sound(ent, CHAN_WEAPON, g.gi.SoundIndex("weapons/hgrena1b.wav"), 1, ATTN_NORM, 0)
		}

		if ent.Client.PS.GunFrame == 11 {
			if ent.Client.GrenadeTime == 0 {
				ent.Client.GrenadeTime = float32(float64(g.level.Time) + GRENADE_TIMER + 0.2)
				ent.Client.WeaponSound = int32(g.gi.SoundIndex("weapons/hgrenc1b.wav"))
			}

			// they waited too long, detonate it in their hand
			if !ent.Client.GrenadeBlewUp && g.level.Time >= ent.Client.GrenadeTime {
				ent.Client.WeaponSound = 0
				g.weapon_grenade_fire(ent, true)
				ent.Client.GrenadeBlewUp = true
			}

			if ent.Client.Buttons&BUTTON_ATTACK != 0 {
				return
			}

			if ent.Client.GrenadeBlewUp {
				if g.level.Time >= ent.Client.GrenadeTime {
					ent.Client.PS.GunFrame = 15
					ent.Client.GrenadeBlewUp = false
				} else {
					return
				}
			}
		}

		if ent.Client.PS.GunFrame == 12 {
			ent.Client.WeaponSound = 0
			g.weapon_grenade_fire(ent, false)
		}

		if ent.Client.PS.GunFrame == 15 && g.level.Time < ent.Client.GrenadeTime {
			return
		}

		ent.Client.PS.GunFrame++

		if ent.Client.PS.GunFrame == 16 {
			ent.Client.GrenadeTime = 0
			ent.Client.Weaponstate = WEAPON_READY
		}
	}
}

/*
======================================================================

GRENADE LAUNCHER

======================================================================
*/

// C: game/p_weapon.c:709 weapon_grenadelauncher_fire
func (g *Game) weapon_grenadelauncher_fire(ent *Edict) {
	var offset Vec3
	var forward, right Vec3
	var start Vec3
	var damage int32 = 120
	var radius float32

	radius = float32(damage + 40)
	if g.is_quad {
		damage *= 4
	}

	offset = Vec3{8, 8, float32(ent.Viewheight - 8)}
	shared.AngleVectors(ent.Client.VAngle, &forward, &right, nil)
	start = P_ProjectSource(ent.Client, ent.S.Origin, offset, forward, right)

	ent.Client.KickOrigin = shared.VectorScale(forward, -2)
	ent.Client.KickAngles[0] = -1

	g.fire_grenade(ent, start, forward, damage, 600, 2.5, radius)

	g.gi.WriteByteC(svc_muzzleflash)
	g.gi.WriteShort(ent.Index)
	g.gi.WriteByteC(MZ_GRENADE | int(g.is_silenced))
	g.gi.Multicast(&ent.S.Origin, MULTICAST_PVS)

	ent.Client.PS.GunFrame++

	g.PlayerNoise(ent, start, PNOISE_WEAPON)

	if int32(g.dmflags.Value)&DF_INFINITE_AMMO == 0 {
		ent.Client.Pers.Inventory[ent.Client.AmmoIndex]--
	}
}

// C: game/p_weapon.c:743 Weapon_GrenadeLauncher
func (g *Game) Weapon_GrenadeLauncher(ent *Edict) {
	pause_frames := []int32{34, 51, 59, 0}
	fire_frames := []int32{6, 0}

	g.Weapon_Generic(ent, 5, 16, 59, 64, pause_frames, fire_frames, (*Game).weapon_grenadelauncher_fire)
}

/*
======================================================================

ROCKET

======================================================================
*/

// C: game/p_weapon.c:759 Weapon_RocketLauncher_Fire
func (g *Game) Weapon_RocketLauncher_Fire(ent *Edict) {
	var offset, start Vec3
	var forward, right Vec3
	var damage int32
	var damage_radius float32
	var radius_damage int32

	damage = 100 + int32(float64(g.random())*20.0)
	radius_damage = 120
	damage_radius = 120
	if g.is_quad {
		damage *= 4
		radius_damage *= 4
	}

	shared.AngleVectors(ent.Client.VAngle, &forward, &right, nil)

	ent.Client.KickOrigin = shared.VectorScale(forward, -2)
	ent.Client.KickAngles[0] = -1

	offset = Vec3{8, 8, float32(ent.Viewheight - 8)}
	start = P_ProjectSource(ent.Client, ent.S.Origin, offset, forward, right)
	g.fire_rocket(ent, start, forward, damage, 650, damage_radius, radius_damage)

	// send muzzle flash
	g.gi.WriteByteC(svc_muzzleflash)
	g.gi.WriteShort(ent.Index)
	g.gi.WriteByteC(MZ_ROCKET | int(g.is_silenced))
	g.gi.Multicast(&ent.S.Origin, MULTICAST_PVS)

	ent.Client.PS.GunFrame++

	g.PlayerNoise(ent, start, PNOISE_WEAPON)

	if int32(g.dmflags.Value)&DF_INFINITE_AMMO == 0 {
		ent.Client.Pers.Inventory[ent.Client.AmmoIndex]--
	}
}

// C: game/p_weapon.c:799 Weapon_RocketLauncher
func (g *Game) Weapon_RocketLauncher(ent *Edict) {
	pause_frames := []int32{25, 33, 42, 50, 0}
	fire_frames := []int32{5, 0}

	g.Weapon_Generic(ent, 4, 12, 50, 54, pause_frames, fire_frames, (*Game).Weapon_RocketLauncher_Fire)
}

/*
======================================================================

BLASTER / HYPERBLASTER

======================================================================
*/

// C: game/p_weapon.c:816 Blaster_Fire
func (g *Game) Blaster_Fire(ent *Edict, g_offset Vec3, damage int32, hyper bool, effect int32) {
	var forward, right Vec3
	var start Vec3
	var offset Vec3

	if g.is_quad {
		damage *= 4
	}
	shared.AngleVectors(ent.Client.VAngle, &forward, &right, nil)
	offset = Vec3{24, 8, float32(ent.Viewheight - 8)}
	offset = shared.VectorAdd(offset, g_offset)
	start = P_ProjectSource(ent.Client, ent.S.Origin, offset, forward, right)

	ent.Client.KickOrigin = shared.VectorScale(forward, -2)
	ent.Client.KickAngles[0] = -1

	g.fire_blaster(ent, start, forward, damage, 1000, effect, hyper)

	// send muzzle flash
	g.gi.WriteByteC(svc_muzzleflash)
	g.gi.WriteShort(ent.Index)
	if hyper {
		g.gi.WriteByteC(MZ_HYPERBLASTER | int(g.is_silenced))
	} else {
		g.gi.WriteByteC(MZ_BLASTER | int(g.is_silenced))
	}
	g.gi.Multicast(&ent.S.Origin, MULTICAST_PVS)

	g.PlayerNoise(ent, start, PNOISE_WEAPON)
}

// C: game/p_weapon.c:847 Weapon_Blaster_Fire
func (g *Game) Weapon_Blaster_Fire(ent *Edict) {
	var damage int32

	if g.deathmatch.Value != 0 {
		damage = 15
	} else {
		damage = 10
	}
	g.Blaster_Fire(ent, shared.Vec3Origin, damage, false, EF_BLASTER)
	ent.Client.PS.GunFrame++
}

// C: game/p_weapon.c:859 Weapon_Blaster
func (g *Game) Weapon_Blaster(ent *Edict) {
	pause_frames := []int32{19, 32, 0}
	fire_frames := []int32{5, 0}

	g.Weapon_Generic(ent, 4, 8, 52, 55, pause_frames, fire_frames, (*Game).Weapon_Blaster_Fire)
}

// C: game/p_weapon.c:868 Weapon_HyperBlaster_Fire
func (g *Game) Weapon_HyperBlaster_Fire(ent *Edict) {
	var rotation float32
	var offset Vec3
	var effect int32
	var damage int32

	ent.Client.WeaponSound = int32(g.gi.SoundIndex("weapons/hyprbl1a.wav"))

	if ent.Client.Buttons&BUTTON_ATTACK == 0 {
		ent.Client.PS.GunFrame++
	} else {
		if ent.Client.Pers.Inventory[ent.Client.AmmoIndex] == 0 {
			if g.level.Time >= ent.PainDebounceTime {
				g.gi.Sound(ent, CHAN_VOICE, g.gi.SoundIndex("weapons/noammo.wav"), 1, ATTN_NORM, 0)
				ent.PainDebounceTime = g.level.Time + 1
			}
			g.NoAmmoWeaponChange(ent)
		} else {
			// (gunframe - 5) * 2 is int; * M_PI / 6 in double
			rotation = float32(float64((ent.Client.PS.GunFrame-5)*2) * shared.MPI / 6)
			offset[0] = float32(-4 * math.Sin(float64(rotation)))
			offset[1] = 0
			offset[2] = float32(4 * math.Cos(float64(rotation)))

			if ent.Client.PS.GunFrame == 6 || ent.Client.PS.GunFrame == 9 {
				effect = EF_HYPERBLASTER
			} else {
				effect = 0
			}
			if g.deathmatch.Value != 0 {
				damage = 15
			} else {
				damage = 20
			}
			g.Blaster_Fire(ent, offset, damage, true, effect)
			if int32(g.dmflags.Value)&DF_INFINITE_AMMO == 0 {
				ent.Client.Pers.Inventory[ent.Client.AmmoIndex]--
			}

			ent.Client.AnimPriority = ANIM_ATTACK
			if ent.Client.PS.PMove.PmFlags&PMF_DUCKED != 0 {
				ent.S.Frame = FRAME_crattak1 - 1
				ent.Client.AnimEnd = FRAME_crattak9
			} else {
				ent.S.Frame = FRAME_attack1 - 1
				ent.Client.AnimEnd = FRAME_attack8
			}
		}

		ent.Client.PS.GunFrame++
		if ent.Client.PS.GunFrame == 12 && ent.Client.Pers.Inventory[ent.Client.AmmoIndex] != 0 {
			ent.Client.PS.GunFrame = 6
		}
	}

	if ent.Client.PS.GunFrame == 12 {
		g.gi.Sound(ent, CHAN_AUTO, g.gi.SoundIndex("weapons/hyprbd1a.wav"), 1, ATTN_NORM, 0)
		ent.Client.WeaponSound = 0
	}
}

// C: game/p_weapon.c:937 Weapon_HyperBlaster
func (g *Game) Weapon_HyperBlaster(ent *Edict) {
	pause_frames := []int32{0}
	fire_frames := []int32{6, 7, 8, 9, 10, 11, 0}

	g.Weapon_Generic(ent, 5, 20, 49, 53, pause_frames, fire_frames, (*Game).Weapon_HyperBlaster_Fire)
}

/*
======================================================================

MACHINEGUN / CHAINGUN

======================================================================
*/

// C: game/p_weapon.c:953 Machinegun_Fire
func (g *Game) Machinegun_Fire(ent *Edict) {
	var start Vec3
	var forward, right Vec3
	var angles Vec3
	var damage int32 = 8
	var kick int32 = 2
	var offset Vec3

	if ent.Client.Buttons&BUTTON_ATTACK == 0 {
		ent.Client.MachinegunShots = 0
		ent.Client.PS.GunFrame++
		return
	}

	if ent.Client.PS.GunFrame == 5 {
		ent.Client.PS.GunFrame = 4
	} else {
		ent.Client.PS.GunFrame = 5
	}

	if ent.Client.Pers.Inventory[ent.Client.AmmoIndex] < 1 {
		ent.Client.PS.GunFrame = 6
		if g.level.Time >= ent.PainDebounceTime {
			g.gi.Sound(ent, CHAN_VOICE, g.gi.SoundIndex("weapons/noammo.wav"), 1, ATTN_NORM, 0)
			ent.PainDebounceTime = g.level.Time + 1
		}
		g.NoAmmoWeaponChange(ent)
		return
	}

	if g.is_quad {
		damage *= 4
		kick *= 4
	}

	for i := 1; i < 3; i++ {
		ent.Client.KickOrigin[i] = float32(g.crandom() * 0.35)
		ent.Client.KickAngles[i] = float32(g.crandom() * 0.7)
	}
	ent.Client.KickOrigin[0] = float32(g.crandom() * 0.35)
	ent.Client.KickAngles[0] = float32(float64(ent.Client.MachinegunShots) * -1.5)

	// raise the gun as it is firing
	if g.deathmatch.Value == 0 {
		ent.Client.MachinegunShots++
		if ent.Client.MachinegunShots > 9 {
			ent.Client.MachinegunShots = 9
		}
	}

	// get start / end positions
	angles = shared.VectorAdd(ent.Client.VAngle, ent.Client.KickAngles)
	shared.AngleVectors(angles, &forward, &right, nil)
	offset = Vec3{0, 8, float32(ent.Viewheight - 8)}
	start = P_ProjectSource(ent.Client, ent.S.Origin, offset, forward, right)
	g.fire_bullet(ent, start, forward, damage, kick, DEFAULT_BULLET_HSPREAD, DEFAULT_BULLET_VSPREAD, MOD_MACHINEGUN)

	g.gi.WriteByteC(svc_muzzleflash)
	g.gi.WriteShort(ent.Index)
	g.gi.WriteByteC(MZ_MACHINEGUN | int(g.is_silenced))
	g.gi.Multicast(&ent.S.Origin, MULTICAST_PVS)

	g.PlayerNoise(ent, start, PNOISE_WEAPON)

	if int32(g.dmflags.Value)&DF_INFINITE_AMMO == 0 {
		ent.Client.Pers.Inventory[ent.Client.AmmoIndex]--
	}

	ent.Client.AnimPriority = ANIM_ATTACK
	if ent.Client.PS.PMove.PmFlags&PMF_DUCKED != 0 {
		ent.S.Frame = FRAME_crattak1 - int32(float64(g.random())+0.25)
		ent.Client.AnimEnd = FRAME_crattak9
	} else {
		ent.S.Frame = FRAME_attack1 - int32(float64(g.random())+0.25)
		ent.Client.AnimEnd = FRAME_attack8
	}
}

// C: game/p_weapon.c:1039 Weapon_Machinegun
func (g *Game) Weapon_Machinegun(ent *Edict) {
	pause_frames := []int32{23, 45, 0}
	fire_frames := []int32{4, 5, 0}

	g.Weapon_Generic(ent, 3, 5, 45, 49, pause_frames, fire_frames, (*Game).Machinegun_Fire)
}

// C: game/p_weapon.c:1047 Chaingun_Fire
func (g *Game) Chaingun_Fire(ent *Edict) {
	var shots int32
	var start Vec3
	var forward, right, up Vec3
	var r, u float32
	var offset Vec3
	var damage int32
	var kick int32 = 2

	if g.deathmatch.Value != 0 {
		damage = 6
	} else {
		damage = 8
	}

	if ent.Client.PS.GunFrame == 5 {
		g.gi.Sound(ent, CHAN_AUTO, g.gi.SoundIndex("weapons/chngnu1a.wav"), 1, ATTN_IDLE, 0)
	}

	if ent.Client.PS.GunFrame == 14 && ent.Client.Buttons&BUTTON_ATTACK == 0 {
		ent.Client.PS.GunFrame = 32
		ent.Client.WeaponSound = 0
		return
	} else if ent.Client.PS.GunFrame == 21 && ent.Client.Buttons&BUTTON_ATTACK != 0 &&
		ent.Client.Pers.Inventory[ent.Client.AmmoIndex] != 0 {
		ent.Client.PS.GunFrame = 15
	} else {
		ent.Client.PS.GunFrame++
	}

	if ent.Client.PS.GunFrame == 22 {
		ent.Client.WeaponSound = 0
		g.gi.Sound(ent, CHAN_AUTO, g.gi.SoundIndex("weapons/chngnd1a.wav"), 1, ATTN_IDLE, 0)
	} else {
		ent.Client.WeaponSound = int32(g.gi.SoundIndex("weapons/chngnl1a.wav"))
	}

	ent.Client.AnimPriority = ANIM_ATTACK
	if ent.Client.PS.PMove.PmFlags&PMF_DUCKED != 0 {
		ent.S.Frame = FRAME_crattak1 - (ent.Client.PS.GunFrame & 1)
		ent.Client.AnimEnd = FRAME_crattak9
	} else {
		ent.S.Frame = FRAME_attack1 - (ent.Client.PS.GunFrame & 1)
		ent.Client.AnimEnd = FRAME_attack8
	}

	if ent.Client.PS.GunFrame <= 9 {
		shots = 1
	} else if ent.Client.PS.GunFrame <= 14 {
		if ent.Client.Buttons&BUTTON_ATTACK != 0 {
			shots = 2
		} else {
			shots = 1
		}
	} else {
		shots = 3
	}

	if ent.Client.Pers.Inventory[ent.Client.AmmoIndex] < shots {
		shots = ent.Client.Pers.Inventory[ent.Client.AmmoIndex]
	}

	if shots == 0 {
		if g.level.Time >= ent.PainDebounceTime {
			g.gi.Sound(ent, CHAN_VOICE, g.gi.SoundIndex("weapons/noammo.wav"), 1, ATTN_NORM, 0)
			ent.PainDebounceTime = g.level.Time + 1
		}
		g.NoAmmoWeaponChange(ent)
		return
	}

	if g.is_quad {
		damage *= 4
		kick *= 4
	}

	for i := 0; i < 3; i++ {
		ent.Client.KickOrigin[i] = float32(g.crandom() * 0.35)
		ent.Client.KickAngles[i] = float32(g.crandom() * 0.7)
	}

	for i := int32(0); i < shots; i++ {
		// get start / end positions
		shared.AngleVectors(ent.Client.VAngle, &forward, &right, &up)
		r = float32(7 + g.crandom()*4)
		u = float32(g.crandom() * 4)
		offset = Vec3{0, r, u + float32(ent.Viewheight) - 8}
		start = P_ProjectSource(ent.Client, ent.S.Origin, offset, forward, right)

		g.fire_bullet(ent, start, forward, damage, kick, DEFAULT_BULLET_HSPREAD, DEFAULT_BULLET_VSPREAD, MOD_CHAINGUN)
	}

	// send muzzle flash
	g.gi.WriteByteC(svc_muzzleflash)
	g.gi.WriteShort(ent.Index)
	g.gi.WriteByteC(int(MZ_CHAINGUN1+shots-1) | int(g.is_silenced))
	g.gi.Multicast(&ent.S.Origin, MULTICAST_PVS)

	g.PlayerNoise(ent, start, PNOISE_WEAPON)

	if int32(g.dmflags.Value)&DF_INFINITE_AMMO == 0 {
		ent.Client.Pers.Inventory[ent.Client.AmmoIndex] -= shots
	}
}

// C: game/p_weapon.c:1167 Weapon_Chaingun
func (g *Game) Weapon_Chaingun(ent *Edict) {
	pause_frames := []int32{38, 43, 51, 61, 0}
	fire_frames := []int32{5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 0}

	g.Weapon_Generic(ent, 4, 31, 61, 64, pause_frames, fire_frames, (*Game).Chaingun_Fire)
}

/*
======================================================================

SHOTGUN / SUPERSHOTGUN

======================================================================
*/

// C: game/p_weapon.c:1184 weapon_shotgun_fire
func (g *Game) weapon_shotgun_fire(ent *Edict) {
	var start Vec3
	var forward, right Vec3
	var offset Vec3
	var damage int32 = 4
	var kick int32 = 8

	if ent.Client.PS.GunFrame == 9 {
		ent.Client.PS.GunFrame++
		return
	}

	shared.AngleVectors(ent.Client.VAngle, &forward, &right, nil)

	ent.Client.KickOrigin = shared.VectorScale(forward, -2)
	ent.Client.KickAngles[0] = -2

	offset = Vec3{0, 8, float32(ent.Viewheight - 8)}
	start = P_ProjectSource(ent.Client, ent.S.Origin, offset, forward, right)

	if g.is_quad {
		damage *= 4
		kick *= 4
	}

	if g.deathmatch.Value != 0 {
		g.fire_shotgun(ent, start, forward, damage, kick, 500, 500, DEFAULT_DEATHMATCH_SHOTGUN_COUNT, MOD_SHOTGUN)
	} else {
		g.fire_shotgun(ent, start, forward, damage, kick, 500, 500, DEFAULT_SHOTGUN_COUNT, MOD_SHOTGUN)
	}

	// send muzzle flash
	g.gi.WriteByteC(svc_muzzleflash)
	g.gi.WriteShort(ent.Index)
	g.gi.WriteByteC(MZ_SHOTGUN | int(g.is_silenced))
	g.gi.Multicast(&ent.S.Origin, MULTICAST_PVS)

	ent.Client.PS.GunFrame++
	g.PlayerNoise(ent, start, PNOISE_WEAPON)

	if int32(g.dmflags.Value)&DF_INFINITE_AMMO == 0 {
		ent.Client.Pers.Inventory[ent.Client.AmmoIndex]--
	}
}

// C: game/p_weapon.c:1230 Weapon_Shotgun
func (g *Game) Weapon_Shotgun(ent *Edict) {
	pause_frames := []int32{22, 28, 34, 0}
	fire_frames := []int32{8, 9, 0}

	g.Weapon_Generic(ent, 7, 18, 36, 39, pause_frames, fire_frames, (*Game).weapon_shotgun_fire)
}

// C: game/p_weapon.c:1239 weapon_supershotgun_fire
func (g *Game) weapon_supershotgun_fire(ent *Edict) {
	var start Vec3
	var forward, right Vec3
	var offset Vec3
	var v Vec3
	var damage int32 = 6
	var kick int32 = 12

	shared.AngleVectors(ent.Client.VAngle, &forward, &right, nil)

	ent.Client.KickOrigin = shared.VectorScale(forward, -2)
	ent.Client.KickAngles[0] = -2

	offset = Vec3{0, 8, float32(ent.Viewheight - 8)}
	start = P_ProjectSource(ent.Client, ent.S.Origin, offset, forward, right)

	if g.is_quad {
		damage *= 4
		kick *= 4
	}

	v[PITCH] = ent.Client.VAngle[PITCH]
	v[YAW] = ent.Client.VAngle[YAW] - 5
	v[ROLL] = ent.Client.VAngle[ROLL]
	shared.AngleVectors(v, &forward, nil, nil)
	g.fire_shotgun(ent, start, forward, damage, kick, DEFAULT_SHOTGUN_HSPREAD, DEFAULT_SHOTGUN_VSPREAD, DEFAULT_SSHOTGUN_COUNT/2, MOD_SSHOTGUN)
	v[YAW] = ent.Client.VAngle[YAW] + 5
	shared.AngleVectors(v, &forward, nil, nil)
	g.fire_shotgun(ent, start, forward, damage, kick, DEFAULT_SHOTGUN_HSPREAD, DEFAULT_SHOTGUN_VSPREAD, DEFAULT_SSHOTGUN_COUNT/2, MOD_SSHOTGUN)

	// send muzzle flash
	g.gi.WriteByteC(svc_muzzleflash)
	g.gi.WriteShort(ent.Index)
	g.gi.WriteByteC(MZ_SSHOTGUN | int(g.is_silenced))
	g.gi.Multicast(&ent.S.Origin, MULTICAST_PVS)

	ent.Client.PS.GunFrame++
	g.PlayerNoise(ent, start, PNOISE_WEAPON)

	if int32(g.dmflags.Value)&DF_INFINITE_AMMO == 0 {
		ent.Client.Pers.Inventory[ent.Client.AmmoIndex] -= 2
	}
}

// C: game/p_weapon.c:1284 Weapon_SuperShotgun
func (g *Game) Weapon_SuperShotgun(ent *Edict) {
	pause_frames := []int32{29, 42, 57, 0}
	fire_frames := []int32{7, 0}

	g.Weapon_Generic(ent, 6, 17, 57, 61, pause_frames, fire_frames, (*Game).weapon_supershotgun_fire)
}

/*
======================================================================

RAILGUN

======================================================================
*/

// C: game/p_weapon.c:1302 weapon_railgun_fire
func (g *Game) weapon_railgun_fire(ent *Edict) {
	var start Vec3
	var forward, right Vec3
	var offset Vec3
	var damage int32
	var kick int32

	if g.deathmatch.Value != 0 { // normal damage is too extreme in dm
		damage = 100
		kick = 200
	} else {
		damage = 150
		kick = 250
	}

	if g.is_quad {
		damage *= 4
		kick *= 4
	}

	shared.AngleVectors(ent.Client.VAngle, &forward, &right, nil)

	ent.Client.KickOrigin = shared.VectorScale(forward, -3)
	ent.Client.KickAngles[0] = -3

	offset = Vec3{0, 7, float32(ent.Viewheight - 8)}
	start = P_ProjectSource(ent.Client, ent.S.Origin, offset, forward, right)
	g.fire_rail(ent, start, forward, damage, kick)

	// send muzzle flash
	g.gi.WriteByteC(svc_muzzleflash)
	g.gi.WriteShort(ent.Index)
	g.gi.WriteByteC(MZ_RAILGUN | int(g.is_silenced))
	g.gi.Multicast(&ent.S.Origin, MULTICAST_PVS)

	ent.Client.PS.GunFrame++
	g.PlayerNoise(ent, start, PNOISE_WEAPON)

	if int32(g.dmflags.Value)&DF_INFINITE_AMMO == 0 {
		ent.Client.Pers.Inventory[ent.Client.AmmoIndex]--
	}
}

// C: game/p_weapon.c:1350 Weapon_Railgun
func (g *Game) Weapon_Railgun(ent *Edict) {
	pause_frames := []int32{56, 0}
	fire_frames := []int32{4, 0}

	g.Weapon_Generic(ent, 3, 18, 56, 61, pause_frames, fire_frames, (*Game).weapon_railgun_fire)
}

/*
======================================================================

BFG10K

======================================================================
*/

// C: game/p_weapon.c:1367 weapon_bfg_fire
func (g *Game) weapon_bfg_fire(ent *Edict) {
	var offset, start Vec3
	var forward, right Vec3
	var damage int32
	var damage_radius float32 = 1000

	if g.deathmatch.Value != 0 {
		damage = 200
	} else {
		damage = 500
	}

	if ent.Client.PS.GunFrame == 9 {
		// send muzzle flash
		g.gi.WriteByteC(svc_muzzleflash)
		g.gi.WriteShort(ent.Index)
		g.gi.WriteByteC(MZ_BFG | int(g.is_silenced))
		g.gi.Multicast(&ent.S.Origin, MULTICAST_PVS)

		ent.Client.PS.GunFrame++

		g.PlayerNoise(ent, ent.S.Origin, PNOISE_WEAPON)
		return
	}

	// cells can go down during windup (from power armor hits), so
	// check again and abort firing if we don't have enough now
	if ent.Client.Pers.Inventory[ent.Client.AmmoIndex] < 50 {
		ent.Client.PS.GunFrame++
		return
	}

	if g.is_quad {
		damage *= 4
	}

	shared.AngleVectors(ent.Client.VAngle, &forward, &right, nil)

	ent.Client.KickOrigin = shared.VectorScale(forward, -2)

	// make a big pitch kick with an inverse fall
	ent.Client.VDmgPitch = -40
	ent.Client.VDmgRoll = float32(g.crandom() * 8)
	ent.Client.VDmgTime = float32(float64(g.level.Time) + DAMAGE_TIME)

	offset = Vec3{8, 8, float32(ent.Viewheight - 8)}
	start = P_ProjectSource(ent.Client, ent.S.Origin, offset, forward, right)
	g.fire_bfg(ent, start, forward, damage, 400, damage_radius)

	ent.Client.PS.GunFrame++

	g.PlayerNoise(ent, start, PNOISE_WEAPON)

	if int32(g.dmflags.Value)&DF_INFINITE_AMMO == 0 {
		ent.Client.Pers.Inventory[ent.Client.AmmoIndex] -= 50
	}
}

// C: game/p_weapon.c:1425 Weapon_BFG
func (g *Game) Weapon_BFG(ent *Edict) {
	pause_frames := []int32{39, 45, 50, 55, 0}
	fire_frames := []int32{9, 17, 0}

	g.Weapon_Generic(ent, 8, 32, 55, 58, pause_frames, fire_frames, (*Game).weapon_bfg_fire)
}
