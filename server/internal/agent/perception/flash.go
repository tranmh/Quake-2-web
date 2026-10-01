package perception

import "quake2web/server/internal/q2const"

// MonsterFlashWeapon names the weapon of a svc_muzzleflash2 flash number
// (q_shared.h MZ2_*): a player learns it from the sound and the shot.
func MonsterFlashWeapon(mz2 int32) Weapon {
	switch {
	case mz2 >= q2const.MZ2_TANK_BLASTER_1 && mz2 <= q2const.MZ2_TANK_BLASTER_3:
		return WeaponBlaster
	case mz2 >= q2const.MZ2_TANK_MACHINEGUN_1 && mz2 <= q2const.MZ2_TANK_MACHINEGUN_19:
		return WeaponMachinegun
	case mz2 >= q2const.MZ2_TANK_ROCKET_1 && mz2 <= q2const.MZ2_TANK_ROCKET_3:
		return WeaponRocket
	case mz2 >= q2const.MZ2_INFANTRY_MACHINEGUN_1 && mz2 <= q2const.MZ2_INFANTRY_MACHINEGUN_13:
		return WeaponMachinegun
	case mz2 >= q2const.MZ2_SOLDIER_BLASTER_1 && mz2 <= q2const.MZ2_SOLDIER_MACHINEGUN_2,
		mz2 >= q2const.MZ2_SOLDIER_BLASTER_3 && mz2 <= q2const.MZ2_SOLDIER_MACHINEGUN_8:
		return soldierFlashWeapon(mz2)
	case mz2 >= q2const.MZ2_GUNNER_MACHINEGUN_1 && mz2 <= q2const.MZ2_GUNNER_MACHINEGUN_8:
		return WeaponChaingun
	case mz2 >= q2const.MZ2_GUNNER_GRENADE_1 && mz2 <= q2const.MZ2_GUNNER_GRENADE_4:
		return WeaponGrenade
	case mz2 == q2const.MZ2_CHICK_ROCKET_1:
		return WeaponRocket
	case mz2 == q2const.MZ2_FLYER_BLASTER_1, mz2 == q2const.MZ2_FLYER_BLASTER_2,
		mz2 == q2const.MZ2_MEDIC_BLASTER_1, mz2 == q2const.MZ2_MEDIC_BLASTER_2,
		mz2 == q2const.MZ2_HOVER_BLASTER_1, mz2 == q2const.MZ2_FLOAT_BLASTER_1:
		return WeaponBlaster
	case mz2 == q2const.MZ2_GLADIATOR_RAILGUN_1, mz2 == q2const.MZ2_MAKRON_RAILGUN_1:
		return WeaponRailgun
	case mz2 == q2const.MZ2_ACTOR_MACHINEGUN_1:
		return WeaponMachinegun
	case mz2 >= q2const.MZ2_SUPERTANK_MACHINEGUN_1 && mz2 <= q2const.MZ2_SUPERTANK_MACHINEGUN_6:
		return WeaponChaingun
	case mz2 >= q2const.MZ2_SUPERTANK_ROCKET_1 && mz2 <= q2const.MZ2_SUPERTANK_ROCKET_3:
		return WeaponRocket
	case mz2 >= q2const.MZ2_BOSS2_MACHINEGUN_L1 && mz2 <= q2const.MZ2_BOSS2_MACHINEGUN_L5,
		mz2 >= q2const.MZ2_BOSS2_MACHINEGUN_R1 && mz2 <= q2const.MZ2_BOSS2_MACHINEGUN_R5:
		return WeaponChaingun
	case mz2 >= q2const.MZ2_BOSS2_ROCKET_1 && mz2 <= q2const.MZ2_BOSS2_ROCKET_4:
		return WeaponRocket
	case mz2 == q2const.MZ2_MAKRON_BFG, mz2 == q2const.MZ2_JORG_BFG_1:
		return WeaponBFG
	case mz2 >= q2const.MZ2_MAKRON_BLASTER_1 && mz2 <= q2const.MZ2_MAKRON_BLASTER_17:
		return WeaponHyperblaster
	case mz2 >= q2const.MZ2_JORG_MACHINEGUN_L1 && mz2 <= q2const.MZ2_JORG_MACHINEGUN_R6:
		return WeaponChaingun
	}
	return WeaponOther
}

// soldierFlashWeapon: the soldier numbers interleave blaster, shotgun and
// machinegun per muzzle (MZ2_SOLDIER_BLASTER_n, _SHOTGUN_n, _MACHINEGUN_n).
func soldierFlashWeapon(mz2 int32) Weapon {
	var off int32
	if mz2 <= q2const.MZ2_SOLDIER_MACHINEGUN_2 {
		off = (mz2 - q2const.MZ2_SOLDIER_BLASTER_1) / 2 // blaster 1,2 / shotgun 1,2 / mg 1,2
	} else {
		off = (mz2 - q2const.MZ2_SOLDIER_BLASTER_3) % 3
	}
	switch off {
	case 0:
		return WeaponBlaster
	case 1:
		return WeaponShotgun
	}
	return WeaponMachinegun
}

// PlayerFlashWeapon names the weapon of a svc_muzzleflash MZ_* number
// (without MZ_SILENCED).
func PlayerFlashWeapon(mz int32) Weapon {
	switch mz {
	case q2const.MZ_BLASTER, q2const.MZ_BLASTER2:
		return WeaponBlaster
	case q2const.MZ_MACHINEGUN:
		return WeaponMachinegun
	case q2const.MZ_SHOTGUN, q2const.MZ_SSHOTGUN, q2const.MZ_SHOTGUN2:
		return WeaponShotgun
	case q2const.MZ_CHAINGUN1, q2const.MZ_CHAINGUN2, q2const.MZ_CHAINGUN3:
		return WeaponChaingun
	case q2const.MZ_RAILGUN:
		return WeaponRailgun
	case q2const.MZ_ROCKET:
		return WeaponRocket
	case q2const.MZ_GRENADE:
		return WeaponGrenade
	case q2const.MZ_BFG:
		return WeaponBFG
	case q2const.MZ_HYPERBLASTER, q2const.MZ_BLUEHYPERBLASTER:
		return WeaponHyperblaster
	case q2const.MZ_LOGIN, q2const.MZ_LOGOUT, q2const.MZ_RESPAWN, q2const.MZ_ITEMRESPAWN:
		return WeaponNone
	}
	return WeaponOther
}
