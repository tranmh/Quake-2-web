package perception

// Model paths the classifier treats specially.
const (
	modelSoldier = "models/monsters/soldier/tris.md2"
	modelTank    = "models/monsters/tank/tris.md2"
	modelLaser   = "models/objects/laser/tris.md2"
)

func box(x0, y0, z0, x1, y1, z1 float32) (Vec3, Vec3) {
	return Vec3{x0, y0, z0}, Vec3{x1, y1, z1}
}

// monster builds a monster class.
func monster(name, model string, classnames []string, health int, dps, rng float32, w Weapon, melee bool, mins, maxs Vec3) Class {
	return Class{Name: name, Kind: KindMonster, Model: model, Classnames: classnames,
		Health: health, DPS: dps, Range: rng, Weapon: w, Melee: melee, Mins: mins, Maxs: maxs}
}

func item(name, model, classname string, k ItemKind, pickup string, amount int, value float32) Class {
	return Class{Name: name, Kind: KindItem, Model: model, Classnames: []string{classname},
		Item: k, Pickup: pickup, Amount: amount, Value: value, Mins: Vec3{-15, -15, -15}, Maxs: Vec3{15, 15, 15}}
}

func plain(name string, k Kind, model string, classnames ...string) Class {
	return Class{Name: name, Kind: k, Model: model, Classnames: classnames}
}

// classDefs is the class table: every model a single-player client can be
// sent as a world entity (the game's models/ and players/ paths and BFG
// sprites), plus the model-less categories. Monster numbers are the spawn
// health (C: game/m_*.c SP_monster_*) and rough damage-per-second and range
// figures a player learns; boxes are the spawn bounding boxes.
func classDefs() []Class {
	std0, std1 := box(-16, -16, -24, 16, 16, 32)
	big0, big1 := box(-24, -24, -24, 24, 24, 32)
	defs := []Class{
		// --- model-less and generic categories
		plain("unknown", KindUnknown, ""),
		plain("monster", KindMonster, ""),
		plain("item", KindItem, ""),
		plain("gib", KindGib, ""),
		plain("projectile", KindProjectile, ""),
		plain("player", KindPlayer, "", "player"),
		plain("brush", KindBrush, ""),
		plain("laser_beam", KindBeam, "", "target_laser"),
		plain("speaker", KindSpeaker, "", "target_speaker"),
		plain("effect", KindDecor, ""),

		// --- monsters
		monster("soldier_light", modelSoldier, []string{"monster_soldier_light"}, 20, 5, 800, WeaponBlaster, false, std0, std1),
		monster("soldier", modelSoldier, []string{"monster_soldier"}, 30, 9, 600, WeaponShotgun, false, std0, std1),
		monster("soldier_ss", modelSoldier, []string{"monster_soldier_ss"}, 40, 12, 800, WeaponMachinegun, false, std0, std1),
		monster("infantry", "models/monsters/infantry/tris.md2", []string{"monster_infantry"}, 100, 12, 800, WeaponMachinegun, true, std0, std1),
		monster("gunner", "models/monsters/gunner/tris.md2", []string{"monster_gunner"}, 175, 20, 1000, WeaponChaingun, false, std0, std1),
		monster("berserk", "models/monsters/berserk/tris.md2", []string{"monster_berserk"}, 240, 20, 96, WeaponMelee, true, std0, std1),
		monster("parasite", "models/monsters/parasite/tris.md2", []string{"monster_parasite"}, 175, 15, 128, WeaponDrain, true, Vec3{-16, -16, -24}, Vec3{16, 16, 24}),
		monster("medic", "models/monsters/medic/tris.md2", []string{"monster_medic"}, 300, 15, 800, WeaponBlaster, false, big0, big1),
		monster("tank", modelTank, []string{"monster_tank"}, 750, 30, 1000, WeaponRocket, false, Vec3{-32, -32, -16}, Vec3{32, 32, 72}),
		monster("tank_commander", modelTank, []string{"monster_tank_commander"}, 1000, 30, 1000, WeaponRocket, false, Vec3{-32, -32, -16}, Vec3{32, 32, 72}),
		monster("mutant", "models/monsters/mutant/tris.md2", []string{"monster_mutant"}, 300, 25, 96, WeaponMelee, true, Vec3{-32, -32, -24}, Vec3{32, 32, 48}),
		monster("brain", "models/monsters/brain/tris.md2", []string{"monster_brain"}, 300, 15, 96, WeaponMelee, true, std0, std1),
		monster("gladiator", "models/monsters/gladiatr/tris.md2", []string{"monster_gladiator"}, 400, 25, 1000, WeaponRailgun, true, Vec3{-32, -32, -24}, Vec3{32, 32, 64}),
		monster("chick", "models/monsters/bitch/tris.md2", []string{"monster_chick"}, 175, 20, 1000, WeaponRocket, true, Vec3{-16, -16, 0}, Vec3{16, 16, 56}),
		monster("supertank", "models/monsters/boss1/tris.md2", []string{"monster_supertank"}, 1500, 50, 1000, WeaponRocket, false, Vec3{-64, -64, 0}, Vec3{64, 64, 112}),
		monster("boss2", "models/monsters/boss2/tris.md2", []string{"monster_boss2"}, 2000, 50, 1000, WeaponRocket, false, Vec3{-56, -56, 0}, Vec3{56, 56, 80}),
		monster("jorg", "models/monsters/boss3/jorg/tris.md2", []string{"monster_jorg"}, 3000, 60, 1000, WeaponChaingun, false, Vec3{-80, -80, 0}, Vec3{80, 80, 140}),
		monster("makron", "models/monsters/boss3/rider/tris.md2", []string{"monster_makron", "monster_boss3_stand"}, 3000, 50, 1000, WeaponBFG, false, Vec3{-30, -30, 0}, Vec3{30, 30, 90}),
	}
	flying := []Class{
		monster("flyer", "models/monsters/flyer/tris.md2", []string{"monster_flyer"}, 50, 6, 600, WeaponBlaster, true, std0, std1),
		monster("hover", "models/monsters/hover/tris.md2", []string{"monster_hover"}, 240, 10, 800, WeaponBlaster, false, big0, big1),
		monster("floater", "models/monsters/float/tris.md2", []string{"monster_floater"}, 200, 10, 800, WeaponBlaster, true, big0, big1),
	}
	for i := range flying {
		flying[i].Flying = true
	}
	flipper := monster("flipper", "models/monsters/flipper/tris.md2", []string{"monster_flipper"}, 50, 8, 96, WeaponMelee, true, Vec3{-16, -16, 0}, Vec3{16, 16, 32})
	flipper.Swimming = true
	defs = append(defs, flying...)
	defs = append(defs, flipper)

	// --- neutrals
	insane := Class{Name: "insane", Kind: KindNeutral, Model: "models/monsters/insane/tris.md2",
		Classnames: []string{"misc_insane"}, Health: 100, Mins: std0, Maxs: std1, NoWoundSkin: true}
	actor := Class{Name: "actor", Kind: KindNeutral, Model: "players/male/tris.md2",
		Classnames: []string{"misc_actor"}, Health: 100, Weapon: WeaponMachinegun, DPS: 10, Range: 800, Mins: std0, Maxs: std1}
	defs = append(defs, insane, actor)

	// --- items (C: game/g_items.c itemlist, SP_item_health*)
	defs = append(defs,
		item("item_health_small", "models/items/healing/stimpack/tris.md2", "item_health_small", ItemHealth, "Health", 2, 0.15),
		item("item_health", "models/items/healing/medium/tris.md2", "item_health", ItemHealth, "Health", 10, 0.4),
		item("item_health_large", "models/items/healing/large/tris.md2", "item_health_large", ItemHealth, "Health", 25, 0.6),
		item("item_health_mega", "models/items/mega_h/tris.md2", "item_health_mega", ItemHealth, "Health", 100, 0.9),
		item("item_armor_body", "models/items/armor/body/tris.md2", "item_armor_body", ItemArmor, "Body Armor", 100, 0.9),
		item("item_armor_combat", "models/items/armor/combat/tris.md2", "item_armor_combat", ItemArmor, "Combat Armor", 50, 0.75),
		item("item_armor_jacket", "models/items/armor/jacket/tris.md2", "item_armor_jacket", ItemArmor, "Jacket Armor", 25, 0.55),
		item("item_armor_shard", "models/items/armor/shard/tris.md2", "item_armor_shard", ItemArmor, "Armor Shard", 2, 0.15),
		item("item_power_screen", "models/items/armor/screen/tris.md2", "item_power_screen", ItemArmor, "Power Screen", 0, 0.7),
		item("item_power_shield", "models/items/armor/shield/tris.md2", "item_power_shield", ItemArmor, "Power Shield", 0, 0.8),
		item("ammo_shells", "models/items/ammo/shells/medium/tris.md2", "ammo_shells", ItemAmmo, "Shells", 10, 0.3),
		item("ammo_bullets", "models/items/ammo/bullets/medium/tris.md2", "ammo_bullets", ItemAmmo, "Bullets", 50, 0.3),
		item("ammo_cells", "models/items/ammo/cells/medium/tris.md2", "ammo_cells", ItemAmmo, "Cells", 50, 0.3),
		item("ammo_rockets", "models/items/ammo/rockets/medium/tris.md2", "ammo_rockets", ItemAmmo, "Rockets", 5, 0.35),
		item("ammo_slugs", "models/items/ammo/slugs/medium/tris.md2", "ammo_slugs", ItemAmmo, "Slugs", 10, 0.3),
		item("ammo_grenades", "models/items/ammo/grenades/medium/tris.md2", "ammo_grenades", ItemAmmo, "Grenades", 5, 0.3),
		item("weapon_shotgun", "models/weapons/g_shotg/tris.md2", "weapon_shotgun", ItemWeapon, "Shotgun", 1, 0.6),
		item("weapon_supershotgun", "models/weapons/g_shotg2/tris.md2", "weapon_supershotgun", ItemWeapon, "Super Shotgun", 1, 0.75),
		item("weapon_machinegun", "models/weapons/g_machn/tris.md2", "weapon_machinegun", ItemWeapon, "Machinegun", 1, 0.65),
		item("weapon_chaingun", "models/weapons/g_chain/tris.md2", "weapon_chaingun", ItemWeapon, "Chaingun", 1, 0.8),
		item("weapon_grenadelauncher", "models/weapons/g_launch/tris.md2", "weapon_grenadelauncher", ItemWeapon, "Grenade Launcher", 1, 0.7),
		item("weapon_rocketlauncher", "models/weapons/g_rocket/tris.md2", "weapon_rocketlauncher", ItemWeapon, "Rocket Launcher", 1, 0.9),
		item("weapon_hyperblaster", "models/weapons/g_hyperb/tris.md2", "weapon_hyperblaster", ItemWeapon, "HyperBlaster", 1, 0.85),
		item("weapon_railgun", "models/weapons/g_rail/tris.md2", "weapon_railgun", ItemWeapon, "Railgun", 1, 0.9),
		item("weapon_bfg", "models/weapons/g_bfg/tris.md2", "weapon_bfg", ItemWeapon, "BFG10K", 1, 1),
		item("item_quad", "models/items/quaddama/tris.md2", "item_quad", ItemPowerup, "Quad Damage", 1, 0.9),
		item("item_invulnerability", "models/items/invulner/tris.md2", "item_invulnerability", ItemPowerup, "Invulnerability", 1, 1),
		item("item_silencer", "models/items/silencer/tris.md2", "item_silencer", ItemPowerup, "Silencer", 1, 0.2),
		item("item_breather", "models/items/breather/tris.md2", "item_breather", ItemPowerup, "Rebreather", 1, 0.3),
		item("item_enviro", "models/items/enviro/tris.md2", "item_enviro", ItemPowerup, "Environment Suit", 1, 0.4),
		item("item_ancient_head", "models/items/c_head/tris.md2", "item_ancient_head", ItemPowerup, "Ancient Head", 2, 0.5),
		item("item_adrenaline", "models/items/adrenal/tris.md2", "item_adrenaline", ItemPowerup, "Adrenaline", 1, 0.5),
		item("item_bandolier", "models/items/band/tris.md2", "item_bandolier", ItemPowerup, "Bandolier", 1, 0.5),
		item("item_pack", "models/items/pack/tris.md2", "item_pack", ItemPowerup, "Ammo Pack", 1, 0.6),
		item("key_data_cd", "models/items/keys/data_cd/tris.md2", "key_data_cd", ItemKey, "Data CD", 1, 1),
		item("key_power_cube", "models/items/keys/power/tris.md2", "key_power_cube", ItemKey, "Power Cube", 1, 1),
		item("key_pyramid", "models/items/keys/pyramid/tris.md2", "key_pyramid", ItemKey, "Pyramid Key", 1, 1),
		item("key_data_spinner", "models/items/keys/spinner/tris.md2", "key_data_spinner", ItemKey, "Data Spinner", 1, 1),
		item("key_pass", "models/items/keys/pass/tris.md2", "key_pass", ItemKey, "Security Pass", 1, 1),
		item("key_blue_key", "models/items/keys/key/tris.md2", "key_blue_key", ItemKey, "Blue Key", 1, 1),
		item("key_red_key", "models/items/keys/red_key/tris.md2", "key_red_key", ItemKey, "Red Key", 1, 1),
		item("key_commander_head", "models/monsters/commandr/head/tris.md2", "key_commander_head", ItemKey, "Commander's Head", 1, 1),
		item("key_airstrike_target", "models/items/keys/target/tris.md2", "key_airstrike_target", ItemKey, "Airstrike Marker", 1, 1),
		// CTF (the ctf game mode): techs and flags
		item("item_tech1", "models/ctf/resistance/tris.md2", "item_tech1", ItemPowerup, "Disruptor Shield", 1, 0.6),
		item("item_tech2", "models/ctf/strength/tris.md2", "item_tech2", ItemPowerup, "Power Amplifier", 1, 0.6),
		item("item_tech3", "models/ctf/haste/tris.md2", "item_tech3", ItemPowerup, "Time Accel", 1, 0.6),
		item("item_tech4", "models/ctf/regeneration/tris.md2", "item_tech4", ItemPowerup, "AutoDoc", 1, 0.6),
		item("item_flag_team1", "players/male/flag1.md2", "item_flag_team1", ItemKey, "Red Flag", 1, 1),
		item("item_flag_team2", "players/male/flag2.md2", "item_flag_team2", ItemKey, "Blue Flag", 1, 1),
	)

	// --- projectiles (C: game/g_weapon.c fire_*, g_misc.c misc_viper_bomb)
	proj := func(name, model string, w Weapon) Class {
		return Class{Name: name, Kind: KindProjectile, Model: model, Weapon: w}
	}
	defs = append(defs,
		proj("blaster_bolt", modelLaser, WeaponBlaster),
		proj("hyperblaster_bolt", "", WeaponHyperblaster), // same model, EF_HYPERBLASTER
		proj("rocket", "models/objects/rocket/tris.md2", WeaponRocket),
		proj("grenade", "models/objects/grenade/tris.md2", WeaponGrenade),
		proj("hand_grenade", "models/objects/grenade2/tris.md2", WeaponGrenade),
		proj("bfg_ball", "sprites/s_bfg1.sp2", WeaponBFG),
		proj("viper_bomb", "models/objects/bomb/tris.md2", WeaponRocket),
		proj("grapple_hook", "models/weapons/grapple/hook/tris.md2", WeaponOther),
	)

	// --- gibs and debris (C: game/g_misc.c ThrowGib, ThrowHead, ThrowDebris)
	for _, g := range []string{"arm", "bone", "bone2", "chest", "gear", "head", "head2", "leg", "skull", "sm_meat", "sm_metal"} {
		defs = append(defs, plain("gib_"+g, KindGib, "models/objects/gibs/"+g+"/tris.md2"))
	}
	for _, d := range []string{"debris1", "debris2", "debris3"} {
		defs = append(defs, plain(d, KindGib, "models/objects/"+d+"/tris.md2"))
	}

	// --- barrels and props
	barrel := plain("barrel", KindBarrel, "models/objects/barrels/tris.md2", "misc_explobox")
	barrel.Health = 10
	barrel.Mins, barrel.Maxs = box(-16, -16, 0, 16, 16, 40)
	defs = append(defs, barrel,
		plain("dead_soldier", KindDecor, "models/deadbods/dude/tris.md2", "misc_deadsoldier"),
		plain("commander_body", KindDecor, "models/monsters/commandr/tris.md2", "monster_commander_body"),
		plain("banner", KindDecor, "models/objects/banner/tris.md2", "misc_banner"),
		plain("blackhole", KindDecor, "models/objects/black/tris.md2", "misc_blackhole"),
		plain("satellite_dish", KindDecor, "models/objects/satellite/tris.md2", "misc_satellite_dish"),
		plain("viper", KindDecor, "models/ships/viper/tris.md2", "misc_viper"),
		plain("bigviper", KindDecor, "models/ships/bigviper/tris.md2", "misc_bigviper"),
		plain("strogg_ship", KindDecor, "models/ships/strogg1/tris.md2", "misc_strogg_ship"),
		plain("light_mine1", KindDecor, "models/objects/minelite/light1/tris.md2", "light_mine1"),
		plain("light_mine2", KindDecor, "models/objects/minelite/light2/tris.md2", "light_mine2"),
		plain("teleporter_pad", KindDecor, "models/objects/dmspot/tris.md2", "misc_teleporter", "misc_teleporter_dest"),
		plain("ctf_banner", KindDecor, "models/ctf/banner/tris.md2", "misc_ctf_banner"),
		plain("ctf_banner_small", KindDecor, "models/ctf/banner/small.md2", "misc_ctf_small_banner"),
		plain("bfg_explosion", KindDecor, "sprites/s_bfg3.sp2"),
		plain("bfg_sprite2", KindDecor, "sprites/s_bfg2.sp2"),
		plain("grapple_cable", KindDecor, "models/weapons/grapple/tris.md2"),
	)

	// --- first-person weapon models (CS_MODELS + GunIndex; never a world entity)
	for _, v := range []struct{ model, pickup string }{
		{"v_blast", "Blaster"}, {"v_shotg", "Shotgun"}, {"v_shotg2", "Super Shotgun"},
		{"v_machn", "Machinegun"}, {"v_chain", "Chaingun"}, {"v_handgr", "Grenades"},
		{"v_launch", "Grenade Launcher"}, {"v_rocket", "Rocket Launcher"},
		{"v_hyperb", "HyperBlaster"}, {"v_rail", "Railgun"}, {"v_bfg", "BFG10K"},
	} {
		c := plain("view_"+v.model[2:], KindViewModel, "models/weapons/"+v.model+"/tris.md2")
		c.Item, c.Pickup = ItemWeapon, v.pickup
		defs = append(defs, c)
	}
	return defs
}
