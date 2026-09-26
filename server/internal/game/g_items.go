package game

// Port of game/g_items.c.

import (
	"strings"

	. "quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/shared"
)

// C: game/g_items.c:39
var (
	jacketarmor_info = GItemArmor{25, 50, .30, .00, ARMOR_JACKET}
	combatarmor_info = GItemArmor{50, 100, .60, .30, ARMOR_COMBAT}
	bodyarmor_info   = GItemArmor{100, 200, .80, .60, ARMOR_BODY}
)

// C: game/g_items.c:49
const (
	HEALTH_IGNORE_MAX = 1
	HEALTH_TIMED      = 2
)

// Function pointer handles of g_items.c.
var (
	DoRespawn           = defThink("DoRespawn")
	Pickup_Powerup      = defPickup("Pickup_Powerup")
	Drop_General        = defItem("Drop_General")
	Pickup_Adrenaline   = defPickup("Pickup_Adrenaline")
	Pickup_AncientHead  = defPickup("Pickup_AncientHead")
	Pickup_Bandolier    = defPickup("Pickup_Bandolier")
	Pickup_Pack         = defPickup("Pickup_Pack")
	Use_Quad            = defItem("Use_Quad")
	Use_Breather        = defItem("Use_Breather")
	Use_Envirosuit      = defItem("Use_Envirosuit")
	Use_Invulnerability = defItem("Use_Invulnerability")
	Use_Silencer        = defItem("Use_Silencer")
	Pickup_Key          = defPickup("Pickup_Key")
	Pickup_Ammo         = defPickup("Pickup_Ammo")
	Drop_Ammo           = defItem("Drop_Ammo")
	MegaHealth_think    = defThink("MegaHealth_think")
	Pickup_Health       = defPickup("Pickup_Health")
	Pickup_Armor        = defPickup("Pickup_Armor")
	Use_PowerArmor      = defItem("Use_PowerArmor")
	Pickup_PowerArmor   = defPickup("Pickup_PowerArmor")
	Drop_PowerArmor     = defItem("Drop_PowerArmor")
	Touch_Item          = defTouch("Touch_Item")
	drop_temp_touch     = defTouch("drop_temp_touch")
	drop_make_touchable = defThink("drop_make_touchable")
	Use_Item            = defUse("Use_Item")
	droptofloor         = defThink("droptofloor")
)

func init() {
	DoRespawn.bind((*Game).DoRespawn)
	Pickup_Powerup.bind((*Game).Pickup_Powerup)
	Drop_General.bind((*Game).Drop_General)
	Pickup_Adrenaline.bind((*Game).Pickup_Adrenaline)
	Pickup_AncientHead.bind((*Game).Pickup_AncientHead)
	Pickup_Bandolier.bind((*Game).Pickup_Bandolier)
	Pickup_Pack.bind((*Game).Pickup_Pack)
	Use_Quad.bind((*Game).Use_Quad)
	Use_Breather.bind((*Game).Use_Breather)
	Use_Envirosuit.bind((*Game).Use_Envirosuit)
	Use_Invulnerability.bind((*Game).Use_Invulnerability)
	Use_Silencer.bind((*Game).Use_Silencer)
	Pickup_Key.bind((*Game).Pickup_Key)
	Pickup_Ammo.bind((*Game).Pickup_Ammo)
	Drop_Ammo.bind((*Game).Drop_Ammo)
	MegaHealth_think.bind((*Game).MegaHealth_think)
	Pickup_Health.bind((*Game).Pickup_Health)
	Pickup_Armor.bind((*Game).Pickup_Armor)
	Use_PowerArmor.bind((*Game).Use_PowerArmor)
	Pickup_PowerArmor.bind((*Game).Pickup_PowerArmor)
	Drop_PowerArmor.bind((*Game).Drop_PowerArmor)
	Touch_Item.bind((*Game).Touch_Item)
	drop_temp_touch.bind((*Game).drop_temp_touch)
	drop_make_touchable.bind((*Game).drop_make_touchable)
	Use_Item.bind((*Game).Use_Item)
	droptofloor.bind((*Game).droptofloor)
}

// ITEM_INDEX is the C macro ITEM_INDEX(x) ((x)-itemlist).
// C: game/g_local.h:633 ITEM_INDEX
func ITEM_INDEX(it *GItem) int32 { return it.index }

// itemDrop returns it->drop, honoring SpawnItem's coop `item->drop = NULL`
// (which in C mutates the global item table for the rest of the process).
func (g *Game) itemDrop(it *GItem) ItemFn {
	if g.itemDropCleared[it.index] {
		return nil
	}
	return it.Drop
}

//======================================================================

// C: game/g_items.c:62 GetItemByIndex
func (g *Game) GetItemByIndex(index int32) *GItem {
	if index == 0 || index >= g.game.NumItems {
		return nil
	}

	return &g.itemlist[index]
}

// C: game/g_items.c:77 FindItemByClassname
func (g *Game) FindItemByClassname(classname string) *GItem {
	for i := 0; i < int(g.game.NumItems); i++ {
		it := &g.itemlist[i]
		if it.Classname == "" {
			continue
		}
		if shared.Q_stricmp(it.Classname, classname) == 0 {
			return it
		}
	}

	return nil
}

// C: game/g_items.c:100 FindItem
func (g *Game) FindItem(pickup_name string) *GItem {
	for i := 0; i < int(g.game.NumItems); i++ {
		it := &g.itemlist[i]
		if it.PickupName == "" {
			continue
		}
		if shared.Q_stricmp(it.PickupName, pickup_name) == 0 {
			return it
		}
	}

	return nil
}

//======================================================================

// C: game/g_items.c:119 DoRespawn
func (g *Game) DoRespawn(ent *Edict) {
	if ent.Team != "" {
		var count int32

		master := ent.Teammaster

		//ZOID
		//in ctf, when we are weapons stay, only the master of a team of weapons
		//is spawned
		if g.ctfOn() &&
			int32(g.dmflags.Value)&DF_WEAPONS_STAY != 0 &&
			master.Item != nil && master.Item.Flags&IT_WEAPON != 0 {
			ent = master
		} else {
			//ZOID

			for count, ent = 0, master; ent != nil; ent, count = ent.Chain, count+1 {
			}

			choice := g.rng.Rand() % count

			for count, ent = 0, master; count < choice; ent, count = ent.Chain, count+1 {
			}
		}
	}

	ent.SVFlags &^= SVF_NOCLIENT
	ent.Solid = SOLID_TRIGGER
	g.gi.LinkEntity(ent)

	// send an effect
	ent.S.Event = EV_ITEM_RESPAWN
}

// C: game/g_items.c:146 SetRespawn
func (g *Game) SetRespawn(ent *Edict, delay float32) {
	ent.Flags |= FL_RESPAWN
	ent.SVFlags |= SVF_NOCLIENT
	ent.Solid = SOLID_NOT
	ent.Nextthink = g.level.Time + delay
	ent.Think = DoRespawn
	g.gi.LinkEntity(ent)
}

//======================================================================

// C: game/g_items.c:159 Pickup_Powerup
func (g *Game) Pickup_Powerup(ent, other *Edict) bool {
	quantity := other.Client.Pers.Inventory[ITEM_INDEX(ent.Item)]
	if (g.skill.Value == 1 && quantity >= 2) || (g.skill.Value >= 2 && quantity >= 1) {
		return false
	}

	if g.coop.Value != 0 && ent.Item.Flags&IT_STAY_COOP != 0 && quantity > 0 {
		return false
	}

	other.Client.Pers.Inventory[ITEM_INDEX(ent.Item)]++

	if g.deathmatch.Value != 0 {
		if ent.Spawnflags&DROPPED_ITEM == 0 {
			g.SetRespawn(ent, float32(ent.Item.Quantity))
		}
		if int32(g.dmflags.Value)&DF_INSTANT_ITEMS != 0 || (ent.Item.Use == Use_Quad && ent.Spawnflags&DROPPED_PLAYER_ITEM != 0) {
			if ent.Item.Use == Use_Quad && ent.Spawnflags&DROPPED_PLAYER_ITEM != 0 {
				g.quad_drop_timeout_hack = int32(float64(ent.Nextthink-g.level.Time) / FRAMETIME)
			}
			ent.Item.Use.fn(g, other, ent.Item)
		}
	}

	return true
}

// C: game/g_items.c:187 Drop_General
func (g *Game) Drop_General(ent *Edict, item *GItem) {
	g.Drop_Item(ent, item)
	ent.Client.Pers.Inventory[ITEM_INDEX(item)]--
	g.ValidateSelectedItem(ent)
}

//======================================================================

// C: game/g_items.c:197 Pickup_Adrenaline
func (g *Game) Pickup_Adrenaline(ent, other *Edict) bool {
	if g.deathmatch.Value == 0 {
		other.MaxHealth += 1
	}

	if other.Health < other.MaxHealth {
		other.Health = other.MaxHealth
	}

	if ent.Spawnflags&DROPPED_ITEM == 0 && g.deathmatch.Value != 0 {
		g.SetRespawn(ent, float32(ent.Item.Quantity))
	}

	return true
}

// C: game/g_items.c:211 Pickup_AncientHead
func (g *Game) Pickup_AncientHead(ent, other *Edict) bool {
	other.MaxHealth += 2

	if ent.Spawnflags&DROPPED_ITEM == 0 && g.deathmatch.Value != 0 {
		g.SetRespawn(ent, float32(ent.Item.Quantity))
	}

	return true
}

// itemsAddCapped is the repeated "inventory[index] += item->quantity; clamp to max" block.
func (g *Game) itemsAddCapped(other *Edict, name string, max int32) {
	item := g.FindItem(name)
	if item != nil {
		index := ITEM_INDEX(item)
		other.Client.Pers.Inventory[index] += item.Quantity
		if other.Client.Pers.Inventory[index] > max {
			other.Client.Pers.Inventory[index] = max
		}
	}
}

// C: game/g_items.c:221 Pickup_Bandolier
func (g *Game) Pickup_Bandolier(ent, other *Edict) bool {
	pers := &other.Client.Pers
	if pers.MaxBullets < 250 {
		pers.MaxBullets = 250
	}
	if pers.MaxShells < 150 {
		pers.MaxShells = 150
	}
	if pers.MaxCells < 250 {
		pers.MaxCells = 250
	}
	if pers.MaxSlugs < 75 {
		pers.MaxSlugs = 75
	}

	g.itemsAddCapped(other, "Bullets", pers.MaxBullets)
	g.itemsAddCapped(other, "Shells", pers.MaxShells)

	if ent.Spawnflags&DROPPED_ITEM == 0 && g.deathmatch.Value != 0 {
		g.SetRespawn(ent, float32(ent.Item.Quantity))
	}

	return true
}

// C: game/g_items.c:259 Pickup_Pack
func (g *Game) Pickup_Pack(ent, other *Edict) bool {
	pers := &other.Client.Pers
	if pers.MaxBullets < 300 {
		pers.MaxBullets = 300
	}
	if pers.MaxShells < 200 {
		pers.MaxShells = 200
	}
	if pers.MaxRockets < 100 {
		pers.MaxRockets = 100
	}
	if pers.MaxGrenades < 100 {
		pers.MaxGrenades = 100
	}
	if pers.MaxCells < 300 {
		pers.MaxCells = 300
	}
	if pers.MaxSlugs < 100 {
		pers.MaxSlugs = 100
	}

	g.itemsAddCapped(other, "Bullets", pers.MaxBullets)
	g.itemsAddCapped(other, "Shells", pers.MaxShells)
	g.itemsAddCapped(other, "Cells", pers.MaxCells)
	g.itemsAddCapped(other, "Grenades", pers.MaxGrenades)
	g.itemsAddCapped(other, "Rockets", pers.MaxRockets)
	g.itemsAddCapped(other, "Slugs", pers.MaxSlugs)

	if ent.Spawnflags&DROPPED_ITEM == 0 && g.deathmatch.Value != 0 {
		g.SetRespawn(ent, float32(ent.Item.Quantity))
	}

	return true
}

//======================================================================

// C: game/g_items.c:339 Use_Quad
func (g *Game) Use_Quad(ent *Edict, item *GItem) {
	var timeout int32

	ent.Client.Pers.Inventory[ITEM_INDEX(item)]--
	g.ValidateSelectedItem(ent)

	if g.quad_drop_timeout_hack != 0 {
		timeout = g.quad_drop_timeout_hack
		g.quad_drop_timeout_hack = 0
	} else {
		timeout = 300
	}

	if ent.Client.QuadFramenum > float32(g.level.Framenum) {
		ent.Client.QuadFramenum += float32(timeout)
	} else {
		ent.Client.QuadFramenum = float32(g.level.Framenum + timeout)
	}

	g.gi.Sound(ent, CHAN_ITEM, g.gi.SoundIndex("items/damage.wav"), 1, ATTN_NORM, 0)
}

//======================================================================

// C: game/g_items.c:366 Use_Breather
func (g *Game) Use_Breather(ent *Edict, item *GItem) {
	ent.Client.Pers.Inventory[ITEM_INDEX(item)]--
	g.ValidateSelectedItem(ent)

	if ent.Client.BreatherFramenum > float32(g.level.Framenum) {
		ent.Client.BreatherFramenum += 300
	} else {
		ent.Client.BreatherFramenum = float32(g.level.Framenum + 300)
	}

	//	gi.sound(ent, CHAN_ITEM, gi.soundindex("items/damage.wav"), 1, ATTN_NORM, 0);
}

//======================================================================

// C: game/g_items.c:381 Use_Envirosuit
func (g *Game) Use_Envirosuit(ent *Edict, item *GItem) {
	ent.Client.Pers.Inventory[ITEM_INDEX(item)]--
	g.ValidateSelectedItem(ent)

	if ent.Client.EnviroFramenum > float32(g.level.Framenum) {
		ent.Client.EnviroFramenum += 300
	} else {
		ent.Client.EnviroFramenum = float32(g.level.Framenum + 300)
	}

	//	gi.sound(ent, CHAN_ITEM, gi.soundindex("items/damage.wav"), 1, ATTN_NORM, 0);
}

//======================================================================

// C: game/g_items.c:396 Use_Invulnerability
func (g *Game) Use_Invulnerability(ent *Edict, item *GItem) {
	ent.Client.Pers.Inventory[ITEM_INDEX(item)]--
	g.ValidateSelectedItem(ent)

	if ent.Client.InvincibleFramenum > float32(g.level.Framenum) {
		ent.Client.InvincibleFramenum += 300
	} else {
		ent.Client.InvincibleFramenum = float32(g.level.Framenum + 300)
	}

	g.gi.Sound(ent, CHAN_ITEM, g.gi.SoundIndex("items/protect.wav"), 1, ATTN_NORM, 0)
}

//======================================================================

// C: game/g_items.c:411 Use_Silencer
func (g *Game) Use_Silencer(ent *Edict, item *GItem) {
	ent.Client.Pers.Inventory[ITEM_INDEX(item)]--
	g.ValidateSelectedItem(ent)
	ent.Client.SilencerShots += 30

	//	gi.sound(ent, CHAN_ITEM, gi.soundindex("items/damage.wav"), 1, ATTN_NORM, 0);
}

//======================================================================

// C: game/g_items.c:422 Pickup_Key
func (g *Game) Pickup_Key(ent, other *Edict) bool {
	if g.coop.Value != 0 {
		if ent.Classname == "key_power_cube" {
			if other.Client.Pers.PowerCubes&((ent.Spawnflags&0x0000ff00)>>8) != 0 {
				return false
			}
			other.Client.Pers.Inventory[ITEM_INDEX(ent.Item)]++
			other.Client.Pers.PowerCubes |= (ent.Spawnflags & 0x0000ff00) >> 8
		} else {
			if other.Client.Pers.Inventory[ITEM_INDEX(ent.Item)] != 0 {
				return false
			}
			other.Client.Pers.Inventory[ITEM_INDEX(ent.Item)] = 1
		}
		return true
	}
	other.Client.Pers.Inventory[ITEM_INDEX(ent.Item)]++
	return true
}

//======================================================================

// C: game/g_items.c:447 Add_Ammo
func (g *Game) Add_Ammo(ent *Edict, item *GItem, count int32) bool {
	var max int32

	if ent.Client == nil {
		return false
	}

	switch item.Tag {
	case AMMO_BULLETS:
		max = ent.Client.Pers.MaxBullets
	case AMMO_SHELLS:
		max = ent.Client.Pers.MaxShells
	case AMMO_ROCKETS:
		max = ent.Client.Pers.MaxRockets
	case AMMO_GRENADES:
		max = ent.Client.Pers.MaxGrenades
	case AMMO_CELLS:
		max = ent.Client.Pers.MaxCells
	case AMMO_SLUGS:
		max = ent.Client.Pers.MaxSlugs
	default:
		return false
	}

	index := ITEM_INDEX(item)

	if ent.Client.Pers.Inventory[index] == max {
		return false
	}

	ent.Client.Pers.Inventory[index] += count

	if ent.Client.Pers.Inventory[index] > max {
		ent.Client.Pers.Inventory[index] = max
	}

	return true
}

// C: game/g_items.c:483 Pickup_Ammo
func (g *Game) Pickup_Ammo(ent, other *Edict) bool {
	var count int32

	weapon := ent.Item.Flags&IT_WEAPON != 0
	if weapon && int32(g.dmflags.Value)&DF_INFINITE_AMMO != 0 {
		count = 1000
	} else if ent.Count != 0 {
		count = ent.Count
	} else {
		count = ent.Item.Quantity
	}

	oldcount := other.Client.Pers.Inventory[ITEM_INDEX(ent.Item)]

	if !g.Add_Ammo(other, ent.Item, count) {
		return false
	}

	if weapon && oldcount == 0 {
		if other.Client.Pers.Weapon != ent.Item && (g.deathmatch.Value == 0 || other.Client.Pers.Weapon == g.FindItem("blaster")) {
			other.Client.Newweapon = ent.Item
		}
	}

	if ent.Spawnflags&(DROPPED_ITEM|DROPPED_PLAYER_ITEM) == 0 && g.deathmatch.Value != 0 {
		g.SetRespawn(ent, 30)
	}
	return true
}

// C: game/g_items.c:513 Drop_Ammo
func (g *Game) Drop_Ammo(ent *Edict, item *GItem) {
	index := ITEM_INDEX(item)
	dropped := g.Drop_Item(ent, item)
	if ent.Client.Pers.Inventory[index] >= item.Quantity {
		dropped.Count = item.Quantity
	} else {
		dropped.Count = ent.Client.Pers.Inventory[index]
	}

	// the ctf fork (older base) has no "Can't drop current weapon" check
	if !g.ctfmod && ent.Client.Pers.Weapon != nil &&
		ent.Client.Pers.Weapon.Tag == AMMO_GRENADES &&
		item.Tag == AMMO_GRENADES &&
		ent.Client.Pers.Inventory[index]-dropped.Count <= 0 {
		g.gi.Cprintf(ent, PRINT_HIGH, "Can't drop current weapon\n")
		g.G_FreeEdict(dropped)
		return
	}

	ent.Client.Pers.Inventory[index] -= dropped.Count
	g.ValidateSelectedItem(ent)
}

//======================================================================

// C: game/g_items.c:541 MegaHealth_think
func (g *Game) MegaHealth_think(self *Edict) {
	if self.Owner.Health > self.Owner.MaxHealth &&
		//ZOID
		!g.CTFHasRegeneration(self.Owner) {
		//ZOID
		self.Nextthink = g.level.Time + 1
		self.Owner.Health -= 1
		return
	}

	if self.Spawnflags&DROPPED_ITEM == 0 && g.deathmatch.Value != 0 {
		g.SetRespawn(self, 20)
	} else {
		g.G_FreeEdict(self)
	}
}

// C: game/g_items.c:556 Pickup_Health
func (g *Game) Pickup_Health(ent, other *Edict) bool {
	if ent.Style&HEALTH_IGNORE_MAX == 0 {
		if other.Health >= other.MaxHealth {
			return false
		}
	}

	//ZOID
	if g.ctfmod && other.Health >= 250 && ent.Count > 25 {
		return false
	}
	//ZOID

	other.Health += ent.Count

	//ZOID
	if g.ctfmod && other.Health > 250 && ent.Count > 25 {
		other.Health = 250
	}
	//ZOID

	if ent.Style&HEALTH_IGNORE_MAX == 0 {
		if other.Health > other.MaxHealth {
			other.Health = other.MaxHealth
		}
	}

	if ent.Style&HEALTH_TIMED != 0 &&
		//ZOID
		!g.CTFHasRegeneration(other) {
		//ZOID
		ent.Think = MegaHealth_think
		ent.Nextthink = g.level.Time + 5
		ent.Owner = other
		ent.Flags |= FL_RESPAWN
		ent.SVFlags |= SVF_NOCLIENT
		ent.Solid = SOLID_NOT
	} else {
		if ent.Spawnflags&DROPPED_ITEM == 0 && g.deathmatch.Value != 0 {
			g.SetRespawn(ent, 30)
		}
	}

	return true
}

//======================================================================

// C: game/g_items.c:590 ArmorIndex
func (g *Game) ArmorIndex(ent *Edict) int32 {
	if ent.Client == nil {
		return 0
	}

	if ent.Client.Pers.Inventory[g.jacket_armor_index] > 0 {
		return g.jacket_armor_index
	}

	if ent.Client.Pers.Inventory[g.combat_armor_index] > 0 {
		return g.combat_armor_index
	}

	if ent.Client.Pers.Inventory[g.body_armor_index] > 0 {
		return g.body_armor_index
	}

	return 0
}

// C: game/g_items.c:607 Pickup_Armor
func (g *Game) Pickup_Armor(ent, other *Edict) bool {
	var oldinfo *GItemArmor
	var newcount int32
	var salvage float32
	var salvagecount int32

	// get info on new armor
	newinfo := ent.Item.Info

	old_armor_index := g.ArmorIndex(other)
	inv := &other.Client.Pers.Inventory

	// handle armor shards specially
	if ent.Item.Tag == ARMOR_SHARD {
		if old_armor_index == 0 {
			inv[g.jacket_armor_index] = 2
		} else {
			inv[old_armor_index] += 2
		}
	} else if old_armor_index == 0 {
		// if player has no armor, just use it
		inv[ITEM_INDEX(ent.Item)] = newinfo.BaseCount
	} else {
		// use the better armor

		// get info on old armor
		if old_armor_index == g.jacket_armor_index {
			oldinfo = &jacketarmor_info
		} else if old_armor_index == g.combat_armor_index {
			oldinfo = &combatarmor_info
		} else { // (old_armor_index == body_armor_index)
			oldinfo = &bodyarmor_info
		}

		if newinfo.NormalProtection > oldinfo.NormalProtection {
			// calc new armor values
			salvage = oldinfo.NormalProtection / newinfo.NormalProtection
			salvagecount = int32(salvage * float32(inv[old_armor_index]))
			newcount = newinfo.BaseCount + salvagecount
			if newcount > newinfo.MaxCount {
				newcount = newinfo.MaxCount
			}

			// zero count of old armor so it goes away
			inv[old_armor_index] = 0

			// change armor to new item with computed value
			inv[ITEM_INDEX(ent.Item)] = newcount
		} else {
			// calc new armor values
			salvage = newinfo.NormalProtection / oldinfo.NormalProtection
			salvagecount = int32(salvage * float32(newinfo.BaseCount))
			newcount = inv[old_armor_index] + salvagecount
			if newcount > oldinfo.MaxCount {
				newcount = oldinfo.MaxCount
			}

			// if we're already maxed out then we don't need the new armor
			if inv[old_armor_index] >= newcount {
				return false
			}

			// update current armor value
			inv[old_armor_index] = newcount
		}
	}

	if ent.Spawnflags&DROPPED_ITEM == 0 && g.deathmatch.Value != 0 {
		g.SetRespawn(ent, 20)
	}

	return true
}

//======================================================================

// C: game/g_items.c:688 PowerArmorType
func (g *Game) PowerArmorType(ent *Edict) int32 {
	if ent.Client == nil {
		return POWER_ARMOR_NONE
	}

	if ent.Flags&FL_POWER_ARMOR == 0 {
		return POWER_ARMOR_NONE
	}

	if ent.Client.Pers.Inventory[g.power_shield_index] > 0 {
		return POWER_ARMOR_SHIELD
	}

	if ent.Client.Pers.Inventory[g.power_screen_index] > 0 {
		return POWER_ARMOR_SCREEN
	}

	return POWER_ARMOR_NONE
}

// C: game/g_items.c:705 Use_PowerArmor
func (g *Game) Use_PowerArmor(ent *Edict, item *GItem) {
	if ent.Flags&FL_POWER_ARMOR != 0 {
		ent.Flags &^= FL_POWER_ARMOR
		g.gi.Sound(ent, CHAN_AUTO, g.gi.SoundIndex("misc/power2.wav"), 1, ATTN_NORM, 0)
	} else {
		index := ITEM_INDEX(g.FindItem("cells"))
		if ent.Client.Pers.Inventory[index] == 0 {
			g.gi.Cprintf(ent, PRINT_HIGH, "No cells for power armor.\n")
			return
		}
		ent.Flags |= FL_POWER_ARMOR
		g.gi.Sound(ent, CHAN_AUTO, g.gi.SoundIndex("misc/power1.wav"), 1, ATTN_NORM, 0)
	}
}

// C: game/g_items.c:727 Pickup_PowerArmor
func (g *Game) Pickup_PowerArmor(ent, other *Edict) bool {
	quantity := other.Client.Pers.Inventory[ITEM_INDEX(ent.Item)]

	other.Client.Pers.Inventory[ITEM_INDEX(ent.Item)]++

	if g.deathmatch.Value != 0 {
		if ent.Spawnflags&DROPPED_ITEM == 0 {
			g.SetRespawn(ent, float32(ent.Item.Quantity))
		}
		// auto-use for DM only if we didn't already have one
		if quantity == 0 {
			ent.Item.Use.fn(g, other, ent.Item)
		}
	}

	return true
}

// C: game/g_items.c:747 Drop_PowerArmor
func (g *Game) Drop_PowerArmor(ent *Edict, item *GItem) {
	if ent.Flags&FL_POWER_ARMOR != 0 && ent.Client.Pers.Inventory[ITEM_INDEX(item)] == 1 {
		g.Use_PowerArmor(ent, item)
	}
	g.Drop_General(ent, item)
}

//======================================================================

// C: game/g_items.c:761 Touch_Item
func (g *Game) Touch_Item(ent, other *Edict, plane *CPlane, surf *CSurface) {
	if other.Client == nil {
		return
	}
	if other.Health < 1 {
		return // dead people can't pickup
	}
	if ent.Item.Pickup == nil {
		return // not a grabbable item?
	}

	if g.CTFMatchSetup() {
		return // can't pick stuff up right now
	}

	taken := ent.Item.Pickup.fn(g, ent, other)

	if taken {
		// flash the screen
		other.Client.BonusAlpha = 0.25

		// show icon and name on status bar
		other.Client.PS.Stats[STAT_PICKUP_ICON] = int16(g.gi.ImageIndex(ent.Item.Icon))
		other.Client.PS.Stats[STAT_PICKUP_STRING] = int16(CS_ITEMS + ITEM_INDEX(ent.Item))
		other.Client.PickupMsgTime = float32(float64(g.level.Time) + 3.0)

		// change selected item
		if ent.Item.Use != nil {
			other.Client.PS.Stats[STAT_SELECTED_ITEM] = int16(ITEM_INDEX(ent.Item))
			other.Client.Pers.SelectedItem = int32(other.Client.PS.Stats[STAT_SELECTED_ITEM])
		}

		if ent.Item.Pickup == Pickup_Health {
			if ent.Count == 2 {
				g.gi.Sound(other, CHAN_ITEM, g.gi.SoundIndex("items/s_health.wav"), 1, ATTN_NORM, 0)
			} else if ent.Count == 10 {
				g.gi.Sound(other, CHAN_ITEM, g.gi.SoundIndex("items/n_health.wav"), 1, ATTN_NORM, 0)
			} else if ent.Count == 25 {
				g.gi.Sound(other, CHAN_ITEM, g.gi.SoundIndex("items/l_health.wav"), 1, ATTN_NORM, 0)
			} else { // (ent->count == 100)
				g.gi.Sound(other, CHAN_ITEM, g.gi.SoundIndex("items/m_health.wav"), 1, ATTN_NORM, 0)
			}
		} else if ent.Item.PickupSound != "" {
			g.gi.Sound(other, CHAN_ITEM, g.gi.SoundIndex(ent.Item.PickupSound), 1, ATTN_NORM, 0)
		}
	}

	if ent.Spawnflags&ITEM_TARGETS_USED == 0 {
		g.G_UseTargets(ent, other)
		ent.Spawnflags |= ITEM_TARGETS_USED
	}

	if !taken {
		return
	}

	if !(g.coop.Value != 0 && ent.Item.Flags&IT_STAY_COOP != 0) || ent.Spawnflags&(DROPPED_ITEM|DROPPED_PLAYER_ITEM) != 0 {
		if ent.Flags&FL_RESPAWN != 0 {
			ent.Flags &^= FL_RESPAWN
		} else {
			g.G_FreeEdict(ent)
		}
	}
}

//======================================================================

// C: game/g_items.c:825 drop_temp_touch
func (g *Game) drop_temp_touch(ent, other *Edict, plane *CPlane, surf *CSurface) {
	if other == ent.Owner {
		return
	}

	g.Touch_Item(ent, other, plane, surf)
}

// C: game/g_items.c:833 drop_make_touchable
func (g *Game) drop_make_touchable(ent *Edict) {
	ent.Touch = Touch_Item
	if g.deathmatch.Value != 0 {
		ent.Nextthink = g.level.Time + 29
		ent.Think = G_FreeEdict
	}
}

// C: game/g_items.c:843 Drop_Item
func (g *Game) Drop_Item(ent *Edict, item *GItem) *Edict {
	var forward, right Vec3

	dropped := g.G_Spawn()

	dropped.Classname = item.Classname
	dropped.Item = item
	dropped.Spawnflags = DROPPED_ITEM
	dropped.S.Effects = uint32(item.WorldModelFlags)
	dropped.S.RenderFX = RF_GLOW
	dropped.Mins = Vec3{-15, -15, -15}
	dropped.Maxs = Vec3{15, 15, 15}
	g.gi.SetModel(dropped, dropped.Item.WorldModel)
	dropped.Solid = SOLID_TRIGGER
	dropped.Movetype = MOVETYPE_TOSS
	dropped.Touch = drop_temp_touch
	dropped.Owner = ent

	if ent.Client != nil {
		shared.AngleVectors(ent.Client.VAngle, &forward, &right, nil)
		offset := Vec3{24, 0, -16}
		dropped.S.Origin = G_ProjectSource(ent.S.Origin, offset, forward, right)
		trace := g.gi.Trace(&ent.S.Origin, &dropped.Mins, &dropped.Maxs,
			&dropped.S.Origin, ent, CONTENTS_SOLID)
		dropped.S.Origin = trace.EndPos
	} else {
		shared.AngleVectors(ent.S.Angles, &forward, &right, nil)
		dropped.S.Origin = ent.S.Origin
	}

	dropped.Velocity = shared.VectorScale(forward, 100)
	dropped.Velocity[2] = 300

	dropped.Think = drop_make_touchable
	dropped.Nextthink = g.level.Time + 1

	g.gi.LinkEntity(dropped)

	return dropped
}

// C: game/g_items.c:892 Use_Item
func (g *Game) Use_Item(ent, other, activator *Edict) {
	ent.SVFlags &^= SVF_NOCLIENT
	ent.Use = nil

	if ent.Spawnflags&ITEM_NO_TOUCH != 0 {
		ent.Solid = SOLID_BBOX
		ent.Touch = nil
	} else {
		ent.Solid = SOLID_TRIGGER
		ent.Touch = Touch_Item
	}

	g.gi.LinkEntity(ent)
}

//======================================================================

// C: game/g_items.c:918 droptofloor
func (g *Game) droptofloor(ent *Edict) {
	ent.Mins = tv(-15, -15, -15)
	ent.Maxs = tv(15, 15, 15)

	if ent.Model != "" {
		g.gi.SetModel(ent, ent.Model)
	} else {
		g.gi.SetModel(ent, ent.Item.WorldModel)
	}
	ent.Solid = SOLID_TRIGGER
	ent.Movetype = MOVETYPE_TOSS
	ent.Touch = Touch_Item

	v := tv(0, 0, -128)
	dest := shared.VectorAdd(ent.S.Origin, v)

	tr := g.gi.Trace(&ent.S.Origin, &ent.Mins, &ent.Maxs, &dest, ent, MASK_SOLID)
	if tr.StartSolid {
		g.dprintf("droptofloor: %s startsolid at %s\n", ent.Classname, vtos(ent.S.Origin))
		g.G_FreeEdict(ent)
		return
	}

	ent.S.Origin = tr.EndPos

	if ent.Team != "" {
		ent.Flags &^= FL_TEAMSLAVE
		ent.Chain = ent.Teamchain
		ent.Teamchain = nil

		ent.SVFlags |= SVF_NOCLIENT
		ent.Solid = SOLID_NOT
		if ent == ent.Teammaster {
			ent.Nextthink = float32(float64(g.level.Time) + FRAMETIME)
			ent.Think = DoRespawn
		}
	}

	if ent.Spawnflags&ITEM_NO_TOUCH != 0 {
		ent.Solid = SOLID_BBOX
		ent.Touch = nil
		ent.S.Effects &^= EF_ROTATE
		ent.S.RenderFX &^= RF_GLOW
	}

	if ent.Spawnflags&ITEM_TRIGGER_SPAWN != 0 {
		ent.SVFlags |= SVF_NOCLIENT
		ent.Solid = SOLID_NOT
		ent.Use = Use_Item
	}

	g.gi.LinkEntity(ent)
}

// PrecacheItem precaches all data needed for a given item.
// This will be called for each item spawned in a level,
// and for each item in each client's inventory.
// C: game/g_items.c:993 PrecacheItem
func (g *Game) PrecacheItem(it *GItem) {
	if it == nil {
		return
	}

	if it.PickupSound != "" {
		g.gi.SoundIndex(it.PickupSound)
	}
	if it.WorldModel != "" {
		g.gi.ModelIndex(it.WorldModel)
	}
	if it.ViewModel != "" {
		g.gi.ModelIndex(it.ViewModel)
	}
	if it.Icon != "" {
		g.gi.ImageIndex(it.Icon)
	}

	// parse everything for its ammo
	if it.Ammo != "" {
		ammo := g.FindItem(it.Ammo)
		if ammo != it {
			g.PrecacheItem(ammo)
		}
	}

	// parse the space seperated precache string for other items
	s := it.Precaches
	if s == "" {
		return
	}

	for len(s) > 0 {
		n := strings.IndexByte(s, ' ')
		if n < 0 {
			n = len(s)
		}
		data := s[:n]
		if n >= MAX_QPATH || n < 5 {
			g.error("PrecacheItem: %s has bad precache string", it.Classname)
		}
		s = s[n:]
		if len(s) > 0 {
			s = s[1:]
		}

		// determine type based on extension
		ext := data[len(data)-3:]
		if ext == "md2" {
			g.gi.ModelIndex(data)
		} else if ext == "sp2" {
			g.gi.ModelIndex(data)
		} else if ext == "wav" {
			g.gi.SoundIndex(data)
		}
		if ext == "pcx" {
			g.gi.ImageIndex(data)
		}
	}
}

// SpawnItem sets the clipping size and plants the object on the floor.
//
// Items can't be immediately dropped to floor, because they might
// be on an entity that hasn't spawned yet.
// C: game/g_items.c:1061 SpawnItem
func (g *Game) SpawnItem(ent *Edict, item *GItem) {
	g.PrecacheItem(item)

	if ent.Spawnflags != 0 {
		if ent.Classname != "key_power_cube" {
			ent.Spawnflags = 0
			g.dprintf("%s at %s has invalid spawnflags set\n", ent.Classname, vtos(ent.S.Origin))
		}
	}

	// some items will be prevented in deathmatch
	if g.deathmatch.Value != 0 {
		dmflags := int32(g.dmflags.Value)
		if dmflags&DF_NO_ARMOR != 0 {
			if item.Pickup == Pickup_Armor || item.Pickup == Pickup_PowerArmor {
				g.G_FreeEdict(ent)
				return
			}
		}
		if dmflags&DF_NO_ITEMS != 0 {
			if item.Pickup == Pickup_Powerup {
				g.G_FreeEdict(ent)
				return
			}
		}
		if dmflags&DF_NO_HEALTH != 0 {
			if item.Pickup == Pickup_Health || item.Pickup == Pickup_Adrenaline || item.Pickup == Pickup_AncientHead {
				g.G_FreeEdict(ent)
				return
			}
		}
		if dmflags&DF_INFINITE_AMMO != 0 {
			if item.Flags == IT_AMMO || ent.Classname == "weapon_bfg" {
				g.G_FreeEdict(ent)
				return
			}
		}
	}

	if g.coop.Value != 0 && ent.Classname == "key_power_cube" {
		ent.Spawnflags |= 1 << (8 + g.level.PowerCubes)
		g.level.PowerCubes++
	}

	// don't let them drop items that stay in a coop game
	if g.coop.Value != 0 && item.Flags&IT_STAY_COOP != 0 {
		g.itemDropCleared[item.index] = true // C: item->drop = NULL;
	}

	//ZOID
	//Don't spawn the flags unless enabled
	if g.ctfmod && g.ctfg.ctf.Value == 0 &&
		(ent.Classname == "item_flag_team1" ||
			ent.Classname == "item_flag_team2") {
		g.G_FreeEdict(ent)
		return
	}
	//ZOID

	ent.Item = item
	ent.Nextthink = float32(float64(g.level.Time) + 2*FRAMETIME) // items start after other solids
	ent.Think = droptofloor
	ent.S.Effects = uint32(item.WorldModelFlags)
	ent.S.RenderFX = RF_GLOW
	if ent.Model != "" {
		g.gi.ModelIndex(ent.Model)
	}

	//ZOID
	//flags are server animated and have special handling
	if g.ctfmod && (ent.Classname == "item_flag_team1" ||
		ent.Classname == "item_flag_team2") {
		ent.Think = CTFFlagSetup
	}
	//ZOID
}

//======================================================================

// itemlist is C itemlist[]: the item table, in exact C order, including the
// empty entry 0 and the end of list marker. Built in init() (the entries
// refer to handles of other files) and read-only afterwards.
// C: game/g_items.c:1134 itemlist
var itemlist []GItem

func init() {
	itemlist = []GItem{
		{},
		{Classname: "item_armor_body", Pickup: Pickup_Armor, PickupSound: "misc/ar1_pkup.wav", WorldModel: "models/items/armor/body/tris.md2", WorldModelFlags: EF_ROTATE, Icon: "i_bodyarmor", PickupName: "Body Armor", CountWidth: 3, Flags: IT_ARMOR, Info: &bodyarmor_info, Tag: ARMOR_BODY, Precaches: ""},
		{Classname: "item_armor_combat", Pickup: Pickup_Armor, PickupSound: "misc/ar1_pkup.wav", WorldModel: "models/items/armor/combat/tris.md2", WorldModelFlags: EF_ROTATE, Icon: "i_combatarmor", PickupName: "Combat Armor", CountWidth: 3, Flags: IT_ARMOR, Info: &combatarmor_info, Tag: ARMOR_COMBAT, Precaches: ""},
		{Classname: "item_armor_jacket", Pickup: Pickup_Armor, PickupSound: "misc/ar1_pkup.wav", WorldModel: "models/items/armor/jacket/tris.md2", WorldModelFlags: EF_ROTATE, Icon: "i_jacketarmor", PickupName: "Jacket Armor", CountWidth: 3, Flags: IT_ARMOR, Info: &jacketarmor_info, Tag: ARMOR_JACKET, Precaches: ""},
		{Classname: "item_armor_shard", Pickup: Pickup_Armor, PickupSound: "misc/ar2_pkup.wav", WorldModel: "models/items/armor/shard/tris.md2", WorldModelFlags: EF_ROTATE, Icon: "i_jacketarmor", PickupName: "Armor Shard", CountWidth: 3, Flags: IT_ARMOR, Tag: ARMOR_SHARD, Precaches: ""},
		{Classname: "item_power_screen", Pickup: Pickup_PowerArmor, Use: Use_PowerArmor, Drop: Drop_PowerArmor, PickupSound: "misc/ar3_pkup.wav", WorldModel: "models/items/armor/screen/tris.md2", WorldModelFlags: EF_ROTATE, Icon: "i_powerscreen", PickupName: "Power Screen", Quantity: 60, Flags: IT_ARMOR, Precaches: ""},
		{Classname: "item_power_shield", Pickup: Pickup_PowerArmor, Use: Use_PowerArmor, Drop: Drop_PowerArmor, PickupSound: "misc/ar3_pkup.wav", WorldModel: "models/items/armor/shield/tris.md2", WorldModelFlags: EF_ROTATE, Icon: "i_powershield", PickupName: "Power Shield", Quantity: 60, Flags: IT_ARMOR, Precaches: "misc/power2.wav misc/power1.wav"},
		{Classname: "weapon_blaster", Use: Use_Weapon, Weaponthink: Weapon_Blaster, PickupSound: "misc/w_pkup.wav", ViewModel: "models/weapons/v_blast/tris.md2", Icon: "w_blaster", PickupName: "Blaster", Flags: IT_WEAPON | IT_STAY_COOP, Weapmodel: WEAP_BLASTER, Precaches: "weapons/blastf1a.wav misc/lasfly.wav"},
		{Classname: "weapon_shotgun", Pickup: Pickup_Weapon, Use: Use_Weapon, Drop: Drop_Weapon, Weaponthink: Weapon_Shotgun, PickupSound: "misc/w_pkup.wav", WorldModel: "models/weapons/g_shotg/tris.md2", WorldModelFlags: EF_ROTATE, ViewModel: "models/weapons/v_shotg/tris.md2", Icon: "w_shotgun", PickupName: "Shotgun", Quantity: 1, Ammo: "Shells", Flags: IT_WEAPON | IT_STAY_COOP, Weapmodel: WEAP_SHOTGUN, Precaches: "weapons/shotgf1b.wav weapons/shotgr1b.wav"},
		{Classname: "weapon_supershotgun", Pickup: Pickup_Weapon, Use: Use_Weapon, Drop: Drop_Weapon, Weaponthink: Weapon_SuperShotgun, PickupSound: "misc/w_pkup.wav", WorldModel: "models/weapons/g_shotg2/tris.md2", WorldModelFlags: EF_ROTATE, ViewModel: "models/weapons/v_shotg2/tris.md2", Icon: "w_sshotgun", PickupName: "Super Shotgun", Quantity: 2, Ammo: "Shells", Flags: IT_WEAPON | IT_STAY_COOP, Weapmodel: WEAP_SUPERSHOTGUN, Precaches: "weapons/sshotf1b.wav"},
		{Classname: "weapon_machinegun", Pickup: Pickup_Weapon, Use: Use_Weapon, Drop: Drop_Weapon, Weaponthink: Weapon_Machinegun, PickupSound: "misc/w_pkup.wav", WorldModel: "models/weapons/g_machn/tris.md2", WorldModelFlags: EF_ROTATE, ViewModel: "models/weapons/v_machn/tris.md2", Icon: "w_machinegun", PickupName: "Machinegun", Quantity: 1, Ammo: "Bullets", Flags: IT_WEAPON | IT_STAY_COOP, Weapmodel: WEAP_MACHINEGUN, Precaches: "weapons/machgf1b.wav weapons/machgf2b.wav weapons/machgf3b.wav weapons/machgf4b.wav weapons/machgf5b.wav"},
		{Classname: "weapon_chaingun", Pickup: Pickup_Weapon, Use: Use_Weapon, Drop: Drop_Weapon, Weaponthink: Weapon_Chaingun, PickupSound: "misc/w_pkup.wav", WorldModel: "models/weapons/g_chain/tris.md2", WorldModelFlags: EF_ROTATE, ViewModel: "models/weapons/v_chain/tris.md2", Icon: "w_chaingun", PickupName: "Chaingun", Quantity: 1, Ammo: "Bullets", Flags: IT_WEAPON | IT_STAY_COOP, Weapmodel: WEAP_CHAINGUN, Precaches: "weapons/chngnu1a.wav weapons/chngnl1a.wav weapons/machgf3b.wav` weapons/chngnd1a.wav"},
		{Classname: "ammo_grenades", Pickup: Pickup_Ammo, Use: Use_Weapon, Drop: Drop_Ammo, Weaponthink: Weapon_Grenade, PickupSound: "misc/am_pkup.wav", WorldModel: "models/items/ammo/grenades/medium/tris.md2", ViewModel: "models/weapons/v_handgr/tris.md2", Icon: "a_grenades", PickupName: "Grenades", CountWidth: 3, Quantity: 5, Ammo: "grenades", Flags: IT_AMMO | IT_WEAPON, Weapmodel: WEAP_GRENADES, Tag: AMMO_GRENADES, Precaches: "weapons/hgrent1a.wav weapons/hgrena1b.wav weapons/hgrenc1b.wav weapons/hgrenb1a.wav weapons/hgrenb2a.wav "},
		{Classname: "weapon_grenadelauncher", Pickup: Pickup_Weapon, Use: Use_Weapon, Drop: Drop_Weapon, Weaponthink: Weapon_GrenadeLauncher, PickupSound: "misc/w_pkup.wav", WorldModel: "models/weapons/g_launch/tris.md2", WorldModelFlags: EF_ROTATE, ViewModel: "models/weapons/v_launch/tris.md2", Icon: "w_glauncher", PickupName: "Grenade Launcher", Quantity: 1, Ammo: "Grenades", Flags: IT_WEAPON | IT_STAY_COOP, Weapmodel: WEAP_GRENADELAUNCHER, Precaches: "models/objects/grenade/tris.md2 weapons/grenlf1a.wav weapons/grenlr1b.wav weapons/grenlb1b.wav"},
		{Classname: "weapon_rocketlauncher", Pickup: Pickup_Weapon, Use: Use_Weapon, Drop: Drop_Weapon, Weaponthink: Weapon_RocketLauncher, PickupSound: "misc/w_pkup.wav", WorldModel: "models/weapons/g_rocket/tris.md2", WorldModelFlags: EF_ROTATE, ViewModel: "models/weapons/v_rocket/tris.md2", Icon: "w_rlauncher", PickupName: "Rocket Launcher", Quantity: 1, Ammo: "Rockets", Flags: IT_WEAPON | IT_STAY_COOP, Weapmodel: WEAP_ROCKETLAUNCHER, Precaches: "models/objects/rocket/tris.md2 weapons/rockfly.wav weapons/rocklf1a.wav weapons/rocklr1b.wav models/objects/debris2/tris.md2"},
		{Classname: "weapon_hyperblaster", Pickup: Pickup_Weapon, Use: Use_Weapon, Drop: Drop_Weapon, Weaponthink: Weapon_HyperBlaster, PickupSound: "misc/w_pkup.wav", WorldModel: "models/weapons/g_hyperb/tris.md2", WorldModelFlags: EF_ROTATE, ViewModel: "models/weapons/v_hyperb/tris.md2", Icon: "w_hyperblaster", PickupName: "HyperBlaster", Quantity: 1, Ammo: "Cells", Flags: IT_WEAPON | IT_STAY_COOP, Weapmodel: WEAP_HYPERBLASTER, Precaches: "weapons/hyprbu1a.wav weapons/hyprbl1a.wav weapons/hyprbf1a.wav weapons/hyprbd1a.wav misc/lasfly.wav"},
		{Classname: "weapon_railgun", Pickup: Pickup_Weapon, Use: Use_Weapon, Drop: Drop_Weapon, Weaponthink: Weapon_Railgun, PickupSound: "misc/w_pkup.wav", WorldModel: "models/weapons/g_rail/tris.md2", WorldModelFlags: EF_ROTATE, ViewModel: "models/weapons/v_rail/tris.md2", Icon: "w_railgun", PickupName: "Railgun", Quantity: 1, Ammo: "Slugs", Flags: IT_WEAPON | IT_STAY_COOP, Weapmodel: WEAP_RAILGUN, Precaches: "weapons/rg_hum.wav"},
		{Classname: "weapon_bfg", Pickup: Pickup_Weapon, Use: Use_Weapon, Drop: Drop_Weapon, Weaponthink: Weapon_BFG, PickupSound: "misc/w_pkup.wav", WorldModel: "models/weapons/g_bfg/tris.md2", WorldModelFlags: EF_ROTATE, ViewModel: "models/weapons/v_bfg/tris.md2", Icon: "w_bfg", PickupName: "BFG10K", Quantity: 50, Ammo: "Cells", Flags: IT_WEAPON | IT_STAY_COOP, Weapmodel: WEAP_BFG, Precaches: "sprites/s_bfg1.sp2 sprites/s_bfg2.sp2 sprites/s_bfg3.sp2 weapons/bfg__f1y.wav weapons/bfg__l1a.wav weapons/bfg__x1b.wav weapons/bfg_hum.wav"},
		{Classname: "ammo_shells", Pickup: Pickup_Ammo, Drop: Drop_Ammo, PickupSound: "misc/am_pkup.wav", WorldModel: "models/items/ammo/shells/medium/tris.md2", Icon: "a_shells", PickupName: "Shells", CountWidth: 3, Quantity: 10, Flags: IT_AMMO, Tag: AMMO_SHELLS, Precaches: ""},
		{Classname: "ammo_bullets", Pickup: Pickup_Ammo, Drop: Drop_Ammo, PickupSound: "misc/am_pkup.wav", WorldModel: "models/items/ammo/bullets/medium/tris.md2", Icon: "a_bullets", PickupName: "Bullets", CountWidth: 3, Quantity: 50, Flags: IT_AMMO, Tag: AMMO_BULLETS, Precaches: ""},
		{Classname: "ammo_cells", Pickup: Pickup_Ammo, Drop: Drop_Ammo, PickupSound: "misc/am_pkup.wav", WorldModel: "models/items/ammo/cells/medium/tris.md2", Icon: "a_cells", PickupName: "Cells", CountWidth: 3, Quantity: 50, Flags: IT_AMMO, Tag: AMMO_CELLS, Precaches: ""},
		{Classname: "ammo_rockets", Pickup: Pickup_Ammo, Drop: Drop_Ammo, PickupSound: "misc/am_pkup.wav", WorldModel: "models/items/ammo/rockets/medium/tris.md2", Icon: "a_rockets", PickupName: "Rockets", CountWidth: 3, Quantity: 5, Flags: IT_AMMO, Tag: AMMO_ROCKETS, Precaches: ""},
		{Classname: "ammo_slugs", Pickup: Pickup_Ammo, Drop: Drop_Ammo, PickupSound: "misc/am_pkup.wav", WorldModel: "models/items/ammo/slugs/medium/tris.md2", Icon: "a_slugs", PickupName: "Slugs", CountWidth: 3, Quantity: 10, Flags: IT_AMMO, Tag: AMMO_SLUGS, Precaches: ""},
		{Classname: "item_quad", Pickup: Pickup_Powerup, Use: Use_Quad, Drop: Drop_General, PickupSound: "items/pkup.wav", WorldModel: "models/items/quaddama/tris.md2", WorldModelFlags: EF_ROTATE, Icon: "p_quad", PickupName: "Quad Damage", CountWidth: 2, Quantity: 60, Flags: IT_POWERUP, Precaches: "items/damage.wav items/damage2.wav items/damage3.wav"},
		{Classname: "item_invulnerability", Pickup: Pickup_Powerup, Use: Use_Invulnerability, Drop: Drop_General, PickupSound: "items/pkup.wav", WorldModel: "models/items/invulner/tris.md2", WorldModelFlags: EF_ROTATE, Icon: "p_invulnerability", PickupName: "Invulnerability", CountWidth: 2, Quantity: 300, Flags: IT_POWERUP, Precaches: "items/protect.wav items/protect2.wav items/protect4.wav"},
		{Classname: "item_silencer", Pickup: Pickup_Powerup, Use: Use_Silencer, Drop: Drop_General, PickupSound: "items/pkup.wav", WorldModel: "models/items/silencer/tris.md2", WorldModelFlags: EF_ROTATE, Icon: "p_silencer", PickupName: "Silencer", CountWidth: 2, Quantity: 60, Flags: IT_POWERUP, Precaches: ""},
		{Classname: "item_breather", Pickup: Pickup_Powerup, Use: Use_Breather, Drop: Drop_General, PickupSound: "items/pkup.wav", WorldModel: "models/items/breather/tris.md2", WorldModelFlags: EF_ROTATE, Icon: "p_rebreather", PickupName: "Rebreather", CountWidth: 2, Quantity: 60, Flags: IT_STAY_COOP | IT_POWERUP, Precaches: "items/airout.wav"},
		{Classname: "item_enviro", Pickup: Pickup_Powerup, Use: Use_Envirosuit, Drop: Drop_General, PickupSound: "items/pkup.wav", WorldModel: "models/items/enviro/tris.md2", WorldModelFlags: EF_ROTATE, Icon: "p_envirosuit", PickupName: "Environment Suit", CountWidth: 2, Quantity: 60, Flags: IT_STAY_COOP | IT_POWERUP, Precaches: "items/airout.wav"},
		{Classname: "item_ancient_head", Pickup: Pickup_AncientHead, PickupSound: "items/pkup.wav", WorldModel: "models/items/c_head/tris.md2", WorldModelFlags: EF_ROTATE, Icon: "i_fixme", PickupName: "Ancient Head", CountWidth: 2, Quantity: 60, Precaches: ""},
		{Classname: "item_adrenaline", Pickup: Pickup_Adrenaline, PickupSound: "items/pkup.wav", WorldModel: "models/items/adrenal/tris.md2", WorldModelFlags: EF_ROTATE, Icon: "p_adrenaline", PickupName: "Adrenaline", CountWidth: 2, Quantity: 60, Precaches: ""},
		{Classname: "item_bandolier", Pickup: Pickup_Bandolier, PickupSound: "items/pkup.wav", WorldModel: "models/items/band/tris.md2", WorldModelFlags: EF_ROTATE, Icon: "p_bandolier", PickupName: "Bandolier", CountWidth: 2, Quantity: 60, Precaches: ""},
		{Classname: "item_pack", Pickup: Pickup_Pack, PickupSound: "items/pkup.wav", WorldModel: "models/items/pack/tris.md2", WorldModelFlags: EF_ROTATE, Icon: "i_pack", PickupName: "Ammo Pack", CountWidth: 2, Quantity: 180, Precaches: ""},
		{Classname: "key_data_cd", Pickup: Pickup_Key, Drop: Drop_General, PickupSound: "items/pkup.wav", WorldModel: "models/items/keys/data_cd/tris.md2", WorldModelFlags: EF_ROTATE, Icon: "k_datacd", PickupName: "Data CD", CountWidth: 2, Flags: IT_STAY_COOP | IT_KEY, Precaches: ""},
		{Classname: "key_power_cube", Pickup: Pickup_Key, Drop: Drop_General, PickupSound: "items/pkup.wav", WorldModel: "models/items/keys/power/tris.md2", WorldModelFlags: EF_ROTATE, Icon: "k_powercube", PickupName: "Power Cube", CountWidth: 2, Flags: IT_STAY_COOP | IT_KEY, Precaches: ""},
		{Classname: "key_pyramid", Pickup: Pickup_Key, Drop: Drop_General, PickupSound: "items/pkup.wav", WorldModel: "models/items/keys/pyramid/tris.md2", WorldModelFlags: EF_ROTATE, Icon: "k_pyramid", PickupName: "Pyramid Key", CountWidth: 2, Flags: IT_STAY_COOP | IT_KEY, Precaches: ""},
		{Classname: "key_data_spinner", Pickup: Pickup_Key, Drop: Drop_General, PickupSound: "items/pkup.wav", WorldModel: "models/items/keys/spinner/tris.md2", WorldModelFlags: EF_ROTATE, Icon: "k_dataspin", PickupName: "Data Spinner", CountWidth: 2, Flags: IT_STAY_COOP | IT_KEY, Precaches: ""},
		{Classname: "key_pass", Pickup: Pickup_Key, Drop: Drop_General, PickupSound: "items/pkup.wav", WorldModel: "models/items/keys/pass/tris.md2", WorldModelFlags: EF_ROTATE, Icon: "k_security", PickupName: "Security Pass", CountWidth: 2, Flags: IT_STAY_COOP | IT_KEY, Precaches: ""},
		{Classname: "key_blue_key", Pickup: Pickup_Key, Drop: Drop_General, PickupSound: "items/pkup.wav", WorldModel: "models/items/keys/key/tris.md2", WorldModelFlags: EF_ROTATE, Icon: "k_bluekey", PickupName: "Blue Key", CountWidth: 2, Flags: IT_STAY_COOP | IT_KEY, Precaches: ""},
		{Classname: "key_red_key", Pickup: Pickup_Key, Drop: Drop_General, PickupSound: "items/pkup.wav", WorldModel: "models/items/keys/red_key/tris.md2", WorldModelFlags: EF_ROTATE, Icon: "k_redkey", PickupName: "Red Key", CountWidth: 2, Flags: IT_STAY_COOP | IT_KEY, Precaches: ""},
		{Classname: "key_commander_head", Pickup: Pickup_Key, Drop: Drop_General, PickupSound: "items/pkup.wav", WorldModel: "models/monsters/commandr/head/tris.md2", WorldModelFlags: EF_GIB, Icon: "k_comhead", PickupName: "Commander's Head", CountWidth: 2, Flags: IT_STAY_COOP | IT_KEY, Precaches: ""},
		{Classname: "key_airstrike_target", Pickup: Pickup_Key, Drop: Drop_General, PickupSound: "items/pkup.wav", WorldModel: "models/items/keys/target/tris.md2", WorldModelFlags: EF_ROTATE, Icon: "i_airstrike", PickupName: "Airstrike Marker", CountWidth: 2, Flags: IT_STAY_COOP | IT_KEY, Precaches: ""},
		{Pickup: Pickup_Health, PickupSound: "items/pkup.wav", Icon: "i_health", PickupName: "Health", CountWidth: 3, Precaches: "items/s_health.wav items/n_health.wav items/l_health.wav items/m_health.wav"},
		{}, // end of list marker
	}
	for i := range itemlist {
		itemlist[i].index = int32(i)
	}
}

// C: game/g_items.c:2121 SP_item_health
func (g *Game) SP_item_health(self *Edict) {
	if g.deathmatch.Value != 0 && int32(g.dmflags.Value)&DF_NO_HEALTH != 0 {
		g.G_FreeEdict(self)
		return
	}

	self.Model = "models/items/healing/medium/tris.md2"
	self.Count = 10
	g.SpawnItem(self, g.FindItem("Health"))
	g.gi.SoundIndex("items/n_health.wav")
}

// C: game/g_items.c:2137 SP_item_health_small
func (g *Game) SP_item_health_small(self *Edict) {
	if g.deathmatch.Value != 0 && int32(g.dmflags.Value)&DF_NO_HEALTH != 0 {
		g.G_FreeEdict(self)
		return
	}

	self.Model = "models/items/healing/stimpack/tris.md2"
	self.Count = 2
	g.SpawnItem(self, g.FindItem("Health"))
	self.Style = HEALTH_IGNORE_MAX
	g.gi.SoundIndex("items/s_health.wav")
}

// C: game/g_items.c:2154 SP_item_health_large
func (g *Game) SP_item_health_large(self *Edict) {
	if g.deathmatch.Value != 0 && int32(g.dmflags.Value)&DF_NO_HEALTH != 0 {
		g.G_FreeEdict(self)
		return
	}

	self.Model = "models/items/healing/large/tris.md2"
	self.Count = 25
	g.SpawnItem(self, g.FindItem("Health"))
	g.gi.SoundIndex("items/l_health.wav")
}

// C: game/g_items.c:2170 SP_item_health_mega
func (g *Game) SP_item_health_mega(self *Edict) {
	if g.deathmatch.Value != 0 && int32(g.dmflags.Value)&DF_NO_HEALTH != 0 {
		g.G_FreeEdict(self)
		return
	}

	self.Model = "models/items/mega_h/tris.md2"
	self.Count = 100
	g.SpawnItem(self, g.FindItem("Health"))
	g.gi.SoundIndex("items/m_health.wav")
	self.Style = HEALTH_IGNORE_MAX | HEALTH_TIMED
}

// C: game/g_items.c:2186 InitItems
func (g *Game) InitItems() {
	g.game.NumItems = int32(len(g.itemlist) - 1)
}

// SetItemNames is called by worldspawn.
// C: game/g_items.c:2200 SetItemNames
func (g *Game) SetItemNames() {
	for i := 0; i < int(g.game.NumItems); i++ {
		it := &g.itemlist[i]
		g.gi.Configstring(CS_ITEMS+i, it.PickupName)
	}

	g.jacket_armor_index = ITEM_INDEX(g.FindItem("Jacket Armor"))
	g.combat_armor_index = ITEM_INDEX(g.FindItem("Combat Armor"))
	g.body_armor_index = ITEM_INDEX(g.FindItem("Body Armor"))
	g.power_screen_index = ITEM_INDEX(g.FindItem("Power Screen"))
	g.power_shield_index = ITEM_INDEX(g.FindItem("Power Shield"))
}
