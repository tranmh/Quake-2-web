package game

// Port of game/g_cmds.c: client commands.

import (
	"fmt"
	"math"
	"sort"
	"strings"

	. "quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/shared"
)

// C: game/g_cmds.c:24 ClientTeam
func (g *Game) ClientTeam(ent *Edict) string {
	value := "" // static char value[512]

	if ent.Client == nil {
		return value
	}

	value = shared.Info_ValueForKey(ent.Client.Pers.Userinfo, "skin")
	p := strings.IndexByte(value, '/')
	if p < 0 {
		return value
	}

	if int32(g.dmflags.Value)&DF_MODELTEAMS != 0 {
		return value[:p]
	}

	// if ((int)(dmflags->value) & DF_SKINTEAMS)
	return value[p+1:]
}

// C: game/g_cmds.c:49 OnSameTeam
func (g *Game) OnSameTeam(ent1, ent2 *Edict) bool {
	if int32(g.dmflags.Value)&(DF_MODELTEAMS|DF_SKINTEAMS) == 0 {
		return false
	}

	ent1Team := g.ClientTeam(ent1)
	ent2Team := g.ClientTeam(ent2)

	return ent1Team == ent2Team
}

// cmdsInventory reads pers.inventory[index] like C, including the
// out-of-bounds read inventory[-1], which in client_persistant_t is the
// selected_item field itself.
func cmdsInventory(pers *ClientPersistant, index int32) int32 {
	if index == -1 {
		return pers.SelectedItem
	}
	return pers.Inventory[index]
}

// C: game/g_cmds.c:66 SelectNextItem
func (g *Game) SelectNextItem(ent *Edict, itflags int32) {
	cl := ent.Client

	//ZOID
	if g.ctfmod && cl.Menu != nil {
		g.PMenu_Next(ent)
		return
	} else if cl.ChaseTarget != nil {
		g.ChaseNext(ent)
		return
	}
	//ZOID

	// scan  for the next valid one
	for i := int32(1); i <= MAX_ITEMS; i++ {
		index := (cl.Pers.SelectedItem + i) % MAX_ITEMS
		if cmdsInventory(&cl.Pers, index) == 0 {
			continue
		}
		it := &g.itemlist[index]
		if it.Use == nil {
			continue
		}
		if it.Flags&itflags == 0 {
			continue
		}

		cl.Pers.SelectedItem = index
		return
	}

	cl.Pers.SelectedItem = -1
}

// C: game/g_cmds.c:98 SelectPrevItem
func (g *Game) SelectPrevItem(ent *Edict, itflags int32) {
	cl := ent.Client

	//ZOID
	if g.ctfmod && cl.Menu != nil {
		g.PMenu_Prev(ent)
		return
	} else if cl.ChaseTarget != nil {
		g.ChasePrev(ent)
		return
	}
	//ZOID

	// scan  for the next valid one
	for i := int32(1); i <= MAX_ITEMS; i++ {
		index := (cl.Pers.SelectedItem + MAX_ITEMS - i) % MAX_ITEMS
		if index < 0 {
			// selected_item == -1, last iteration: C reads inventory[-1]
			// (== selected_item, nonzero) and then itemlist[-1], out of
			// bounds. Whatever that entry holds, the result is
			// selected_item = -1, which the fall-through below also sets.
			continue
		}
		if cmdsInventory(&cl.Pers, index) == 0 {
			continue
		}
		it := &g.itemlist[index]
		if it.Use == nil {
			continue
		}
		if it.Flags&itflags == 0 {
			continue
		}

		cl.Pers.SelectedItem = index
		return
	}

	cl.Pers.SelectedItem = -1
}

// C: game/g_cmds.c:130 ValidateSelectedItem
func (g *Game) ValidateSelectedItem(ent *Edict) {
	cl := ent.Client

	if cmdsInventory(&cl.Pers, cl.Pers.SelectedItem) != 0 {
		return // valid
	}

	g.SelectNextItem(ent, -1)
}

//=================================================================================

// Cmd_Give_f gives items to a client.
// C: game/g_cmds.c:152 Cmd_Give_f
func (g *Game) Cmd_Give_f(ent *Edict) {
	var name string
	var it *GItem
	var index int32
	var give_all bool
	var it_ent *Edict

	if g.deathmatch.Value != 0 && g.sv_cheats.Value == 0 {
		g.gi.Cprintf(ent, PRINT_HIGH, "You must run the server with '+set cheats 1' to enable this command.\n")
		return
	}

	name = g.gi.Args()

	if shared.Q_stricmp(name, "all") == 0 {
		give_all = true
	} else {
		give_all = false
	}

	if give_all || shared.Q_stricmp(g.gi.Argv(1), "health") == 0 {
		if g.gi.Argc() == 3 {
			ent.Health = shared.Atoi(g.gi.Argv(2))
		} else {
			ent.Health = ent.MaxHealth
		}
		if !give_all {
			return
		}
	}

	if give_all || shared.Q_stricmp(name, "weapons") == 0 {
		for i := int32(0); i < g.game.NumItems; i++ {
			it = &g.itemlist[i]
			if it.Pickup == nil {
				continue
			}
			if it.Flags&IT_WEAPON == 0 {
				continue
			}
			ent.Client.Pers.Inventory[i] += 1
		}
		if !give_all {
			return
		}
	}

	if give_all || shared.Q_stricmp(name, "ammo") == 0 {
		for i := int32(0); i < g.game.NumItems; i++ {
			it = &g.itemlist[i]
			if it.Pickup == nil {
				continue
			}
			if it.Flags&IT_AMMO == 0 {
				continue
			}
			g.Add_Ammo(ent, it, 1000)
		}
		if !give_all {
			return
		}
	}

	if give_all || shared.Q_stricmp(name, "armor") == 0 {
		it = g.FindItem("Jacket Armor")
		ent.Client.Pers.Inventory[ITEM_INDEX(it)] = 0

		it = g.FindItem("Combat Armor")
		ent.Client.Pers.Inventory[ITEM_INDEX(it)] = 0

		it = g.FindItem("Body Armor")
		info := it.Info
		ent.Client.Pers.Inventory[ITEM_INDEX(it)] = info.MaxCount

		if !give_all {
			return
		}
	}

	if give_all || shared.Q_stricmp(name, "Power Shield") == 0 {
		it = g.FindItem("Power Shield")
		it_ent = g.G_Spawn()
		it_ent.Classname = it.Classname
		g.SpawnItem(it_ent, it)
		g.Touch_Item(it_ent, ent, nil, nil)
		if it_ent.InUse {
			g.G_FreeEdict(it_ent)
		}

		if !give_all {
			return
		}
	}

	if give_all {
		for i := int32(0); i < g.game.NumItems; i++ {
			it = &g.itemlist[i]
			if it.Pickup == nil {
				continue
			}
			if it.Flags&(IT_ARMOR|IT_WEAPON|IT_AMMO) != 0 {
				continue
			}
			ent.Client.Pers.Inventory[i] = 1
		}
		return
	}

	it = g.FindItem(name)
	if it == nil {
		name = g.gi.Argv(1)
		it = g.FindItem(name)
		if it == nil {
			g.gi.Cprintf(ent, PRINT_HIGH, "unknown item\n")
			return
		}
	}

	if it.Pickup == nil {
		g.gi.Cprintf(ent, PRINT_HIGH, "non-pickup item\n")
		return
	}

	index = ITEM_INDEX(it)

	if it.Flags&IT_AMMO != 0 {
		if g.gi.Argc() == 3 {
			ent.Client.Pers.Inventory[index] = shared.Atoi(g.gi.Argv(2))
		} else {
			ent.Client.Pers.Inventory[index] += it.Quantity
		}
	} else {
		it_ent = g.G_Spawn()
		it_ent.Classname = it.Classname
		g.SpawnItem(it_ent, it)
		g.Touch_Item(it_ent, ent, nil, nil)
		if it_ent.InUse {
			g.G_FreeEdict(it_ent)
		}
	}
}

// Cmd_God_f sets client to godmode
//
// argv(0) god
// C: game/g_cmds.c:308 Cmd_God_f
func (g *Game) Cmd_God_f(ent *Edict) {
	var msg string

	if g.deathmatch.Value != 0 && g.sv_cheats.Value == 0 {
		g.gi.Cprintf(ent, PRINT_HIGH, "You must run the server with '+set cheats 1' to enable this command.\n")
		return
	}

	ent.Flags ^= FL_GODMODE
	if ent.Flags&FL_GODMODE == 0 {
		msg = "godmode OFF\n"
	} else {
		msg = "godmode ON\n"
	}

	g.gi.Cprintf(ent, PRINT_HIGH, msg)
}

// Cmd_Notarget_f sets client to notarget
//
// argv(0) notarget
// C: game/g_cmds.c:337 Cmd_Notarget_f
func (g *Game) Cmd_Notarget_f(ent *Edict) {
	var msg string

	if g.deathmatch.Value != 0 && g.sv_cheats.Value == 0 {
		g.gi.Cprintf(ent, PRINT_HIGH, "You must run the server with '+set cheats 1' to enable this command.\n")
		return
	}

	ent.Flags ^= FL_NOTARGET
	if ent.Flags&FL_NOTARGET == 0 {
		msg = "notarget OFF\n"
	} else {
		msg = "notarget ON\n"
	}

	g.gi.Cprintf(ent, PRINT_HIGH, msg)
}

// Cmd_Noclip_f
//
// argv(0) noclip
// C: game/g_cmds.c:364 Cmd_Noclip_f
func (g *Game) Cmd_Noclip_f(ent *Edict) {
	var msg string

	if g.deathmatch.Value != 0 && g.sv_cheats.Value == 0 {
		g.gi.Cprintf(ent, PRINT_HIGH, "You must run the server with '+set cheats 1' to enable this command.\n")
		return
	}

	if ent.Movetype == MOVETYPE_NOCLIP {
		ent.Movetype = MOVETYPE_WALK
		msg = "noclip OFF\n"
	} else {
		ent.Movetype = MOVETYPE_NOCLIP
		msg = "noclip ON\n"
	}

	g.gi.Cprintf(ent, PRINT_HIGH, msg)
}

// Cmd_Use_f uses an inventory item.
// C: game/g_cmds.c:396 Cmd_Use_f
func (g *Game) Cmd_Use_f(ent *Edict) {
	s := g.gi.Args()
	it := g.FindItem(s)
	if it == nil {
		g.cprintf(ent, PRINT_HIGH, "unknown item: %s\n", s)
		return
	}
	if it.Use == nil {
		g.gi.Cprintf(ent, PRINT_HIGH, "Item is not usable.\n")
		return
	}
	index := ITEM_INDEX(it)
	if ent.Client.Pers.Inventory[index] == 0 {
		g.cprintf(ent, PRINT_HIGH, "Out of item: %s\n", s)
		return
	}

	it.Use.fn(g, ent, it)
}

// Cmd_Drop_f drops an inventory item.
// C: game/g_cmds.c:432 Cmd_Drop_f
func (g *Game) Cmd_Drop_f(ent *Edict) {
	//ZOID--special case for tech powerups
	if g.ctfmod && shared.Q_stricmp(g.gi.Args(), "tech") == 0 {
		if it := g.CTFWhat_Tech(ent); it != nil {
			g.itemDrop(it).fn(g, ent, it)
			return
		}
	}
	//ZOID

	s := g.gi.Args()
	it := g.FindItem(s)
	if it == nil {
		g.cprintf(ent, PRINT_HIGH, "unknown item: %s\n", s)
		return
	}
	if g.itemDrop(it) == nil {
		g.gi.Cprintf(ent, PRINT_HIGH, "Item is not dropable.\n")
		return
	}
	index := ITEM_INDEX(it)
	if ent.Client.Pers.Inventory[index] == 0 {
		g.cprintf(ent, PRINT_HIGH, "Out of item: %s\n", s)
		return
	}

	g.itemDrop(it).fn(g, ent, it)
}

// C: game/g_cmds.c:466 Cmd_Inven_f
func (g *Game) Cmd_Inven_f(ent *Edict) {
	cl := ent.Client

	cl.Showscores = false
	cl.Showhelp = false

	//ZOID
	if g.ctfmod && ent.Client.Menu != nil {
		g.PMenu_Close(ent)
		ent.Client.UpdateChase = true
		return
	}
	//ZOID

	if cl.Showinventory {
		cl.Showinventory = false
		return
	}

	//ZOID
	if g.ctfOn() && cl.Resp.CtfTeam == CTF_NOTEAM {
		g.CTFOpenJoinMenu(ent)
		return
	}
	//ZOID

	cl.Showinventory = true

	g.gi.WriteByteC(svc_inventory)
	for i := 0; i < MAX_ITEMS; i++ {
		g.gi.WriteShort(int(cl.Pers.Inventory[i]))
	}
	g.gi.Unicast(ent, true)
}

// C: game/g_cmds.c:497 Cmd_InvUse_f
func (g *Game) Cmd_InvUse_f(ent *Edict) {
	//ZOID
	if g.ctfmod && ent.Client.Menu != nil {
		g.PMenu_Select(ent)
		return
	}
	//ZOID

	g.ValidateSelectedItem(ent)

	if ent.Client.Pers.SelectedItem == -1 {
		g.gi.Cprintf(ent, PRINT_HIGH, "No item to use.\n")
		return
	}

	it := &g.itemlist[ent.Client.Pers.SelectedItem]
	if it.Use == nil {
		g.gi.Cprintf(ent, PRINT_HIGH, "Item is not usable.\n")
		return
	}
	it.Use.fn(g, ent, it)
}

// Cmd_LastWeap_f (ctf module; not bound to a command in ClientCommand).
// C: ctf/g_cmds.c:563 Cmd_LastWeap_f
func (g *Game) Cmd_LastWeap_f(ent *Edict) {
	cl := ent.Client

	if cl.Pers.Weapon == nil || cl.Pers.Lastweapon == nil {
		return
	}

	cl.Pers.Lastweapon.Use.fn(g, ent, cl.Pers.Lastweapon)
}

// C: game/g_cmds.c:523 Cmd_WeapPrev_f
func (g *Game) Cmd_WeapPrev_f(ent *Edict) {
	cl := ent.Client

	if cl.Pers.Weapon == nil {
		return
	}

	selected_weapon := ITEM_INDEX(cl.Pers.Weapon)

	// scan  for the next valid one
	for i := int32(1); i <= MAX_ITEMS; i++ {
		index := (selected_weapon + i) % MAX_ITEMS
		if cl.Pers.Inventory[index] == 0 {
			continue
		}
		it := &g.itemlist[index]
		if it.Use == nil {
			continue
		}
		if it.Flags&IT_WEAPON == 0 {
			continue
		}
		it.Use.fn(g, ent, it)
		if cl.Pers.Weapon == it {
			return // successful
		}
	}
}

// C: game/g_cmds.c:559 Cmd_WeapNext_f
func (g *Game) Cmd_WeapNext_f(ent *Edict) {
	cl := ent.Client

	if cl.Pers.Weapon == nil {
		return
	}

	selected_weapon := ITEM_INDEX(cl.Pers.Weapon)

	// scan  for the next valid one
	for i := int32(1); i <= MAX_ITEMS; i++ {
		index := (selected_weapon + MAX_ITEMS - i) % MAX_ITEMS
		if cl.Pers.Inventory[index] == 0 {
			continue
		}
		it := &g.itemlist[index]
		if it.Use == nil {
			continue
		}
		if it.Flags&IT_WEAPON == 0 {
			continue
		}
		it.Use.fn(g, ent, it)
		if cl.Pers.Weapon == it {
			return // successful
		}
	}
}

// C: game/g_cmds.c:595 Cmd_WeapLast_f
func (g *Game) Cmd_WeapLast_f(ent *Edict) {
	cl := ent.Client

	if cl.Pers.Weapon == nil || cl.Pers.Lastweapon == nil {
		return
	}

	index := ITEM_INDEX(cl.Pers.Lastweapon)
	if cl.Pers.Inventory[index] == 0 {
		return
	}
	it := &g.itemlist[index]
	if it.Use == nil {
		return
	}
	if it.Flags&IT_WEAPON == 0 {
		return
	}
	it.Use.fn(g, ent, it)
}

// C: game/g_cmds.c:622 Cmd_InvDrop_f
func (g *Game) Cmd_InvDrop_f(ent *Edict) {
	g.ValidateSelectedItem(ent)

	if ent.Client.Pers.SelectedItem == -1 {
		g.gi.Cprintf(ent, PRINT_HIGH, "No item to drop.\n")
		return
	}

	it := &g.itemlist[ent.Client.Pers.SelectedItem]
	if g.itemDrop(it) == nil {
		g.gi.Cprintf(ent, PRINT_HIGH, "Item is not dropable.\n")
		return
	}
	g.itemDrop(it).fn(g, ent, it)
}

// C: game/g_cmds.c:648 Cmd_Kill_f
func (g *Game) Cmd_Kill_f(ent *Edict) {
	//ZOID
	if g.ctfmod && ent.Solid == SOLID_NOT {
		return
	}
	//ZOID

	if (g.level.Time - ent.Client.RespawnTime) < 5 {
		return
	}
	ent.Flags &^= FL_GODMODE
	ent.Health = 0
	g.meansOfDeath = MOD_SUICIDE
	g.player_die(ent, ent, ent, 100000, shared.Vec3Origin)
}

// C: game/g_cmds.c:663 Cmd_PutAway_f
func (g *Game) Cmd_PutAway_f(ent *Edict) {
	ent.Client.Showscores = false
	ent.Client.Showhelp = false
	ent.Client.Showinventory = false
	//ZOID
	if g.ctfmod {
		if ent.Client.Menu != nil {
			g.PMenu_Close(ent)
		}
		ent.Client.UpdateChase = true
	}
	//ZOID
}

// C: game/g_cmds.c:671 PlayerSort
func (g *Game) PlayerSort(a, b int) int {
	anum := g.game.Clients[a].PS.Stats[STAT_FRAGS]
	bnum := g.game.Clients[b].PS.Stats[STAT_FRAGS]

	if anum < bnum {
		return -1
	}
	if anum > bnum {
		return 1
	}
	return 0
}

// C: game/g_cmds.c:693 Cmd_Players_f
func (g *Game) Cmd_Players_f(ent *Edict) {
	var index []int

	count := 0
	for i := 0; float32(i) < g.maxclients.Value; i++ {
		if g.game.Clients[i].Pers.Connected {
			index = append(index, i)
			count++
		}
	}

	// sort by frags
	// glibc qsort is a (stable) merge sort for arrays this small.
	sort.SliceStable(index, func(x, y int) bool { return g.PlayerSort(index[x], index[y]) < 0 })

	// print information
	large := ""

	for i := 0; i < count; i++ {
		small := fmt.Sprintf("%3d %s\n",
			g.game.Clients[index[i]].PS.Stats[STAT_FRAGS],
			g.game.Clients[index[i]].Pers.Netname)
		if len(small) > 63 { // Com_sprintf into char[64]
			small = small[:63]
		}
		if len(small)+len(large) > 1280-100 {
			// can't print all of them in one packet
			large += "...\n"
			break
		}
		large += small
	}

	g.cprintf(ent, PRINT_HIGH, "%s\n%d players\n", large, count)
}

// C: game/g_cmds.c:736 Cmd_Wave_f
func (g *Game) Cmd_Wave_f(ent *Edict) {
	i := shared.Atoi(g.gi.Argv(1))

	// can't wave when ducked
	if ent.Client.PS.PMove.PmFlags&PMF_DUCKED != 0 {
		return
	}

	if ent.Client.AnimPriority > ANIM_WAVE {
		return
	}

	ent.Client.AnimPriority = ANIM_WAVE

	switch i {
	case 0:
		g.gi.Cprintf(ent, PRINT_HIGH, "flipoff\n")
		ent.S.Frame = FRAME_flip01 - 1
		ent.Client.AnimEnd = FRAME_flip12
	case 1:
		g.gi.Cprintf(ent, PRINT_HIGH, "salute\n")
		ent.S.Frame = FRAME_salute01 - 1
		ent.Client.AnimEnd = FRAME_salute11
	case 2:
		g.gi.Cprintf(ent, PRINT_HIGH, "taunt\n")
		ent.S.Frame = FRAME_taunt01 - 1
		ent.Client.AnimEnd = FRAME_taunt17
	case 3:
		g.gi.Cprintf(ent, PRINT_HIGH, "wave\n")
		ent.S.Frame = FRAME_wave01 - 1
		ent.Client.AnimEnd = FRAME_wave11
	default: // case 4
		g.gi.Cprintf(ent, PRINT_HIGH, "point\n")
		ent.S.Frame = FRAME_point01 - 1
		ent.Client.AnimEnd = FRAME_point12
	}
}

// CheckFlood returns true (and prints) when the client is flood-locked.
// C: ctf/g_cmds.c:850 CheckFlood (inline in game/g_cmds.c Cmd_Say_f)
func (g *Game) CheckFlood(ent *Edict) bool {
	if g.flood_msgs.Value != 0 {
		cl := ent.Client

		if g.level.Time < cl.FloodLocktill {
			g.cprintf(ent, PRINT_HIGH, "You can't talk for %d more seconds\n",
				int32(cl.FloodLocktill-g.level.Time))
			return true
		}
		i := int32(float32(cl.FloodWhenhead) - g.flood_msgs.Value + 1)
		if i < 0 {
			i = int32(len(cl.FloodWhen)) + i
		}
		if when := floodWhen(cl, i); when != 0 &&
			g.level.Time-when < g.flood_persecond.Value {
			cl.FloodLocktill = g.level.Time + g.flood_waitdelay.Value
			g.cprintf(ent, PRINT_CHAT, "Flood protection:  You can't talk for %d seconds.\n",
				int32(g.flood_waitdelay.Value))
			return true
		}
		cl.FloodWhenhead = (cl.FloodWhenhead + 1) % int32(len(cl.FloodWhen))
		cl.FloodWhen[cl.FloodWhenhead] = g.level.Time
	}
	return false
}

// floodWhen reads cl->flood_when[i]. With flood_msgs outside 1..11 the C
// index leaves the array and reads the neighbouring gclient_t fields
// (pickup_msg_time, flood_locktill | flood_when[10] | flood_whenhead,
// respawn_time; same layout in both modules); further out it is undefined,
// read as 0 here.
func floodWhen(cl *GClient, i int32) float32 {
	switch {
	case i >= 0 && i < int32(len(cl.FloodWhen)):
		return cl.FloodWhen[i]
	case i == -2:
		return cl.PickupMsgTime
	case i == -1:
		return cl.FloodLocktill
	case i == 10:
		return math.Float32frombits(uint32(cl.FloodWhenhead))
	case i == 11:
		return cl.RespawnTime
	}
	return 0
}

// C: game/g_cmds.c:787 Cmd_Say_f
func (g *Game) Cmd_Say_f(ent *Edict, team, arg0 bool) {
	var text string

	if g.gi.Argc() < 2 && !arg0 {
		return
	}

	if int32(g.dmflags.Value)&(DF_MODELTEAMS|DF_SKINTEAMS) == 0 {
		team = false
	}

	if team {
		text = fmt.Sprintf("(%s): ", ent.Client.Pers.Netname)
	} else {
		text = fmt.Sprintf("%s: ", ent.Client.Pers.Netname)
	}

	if arg0 {
		text += g.gi.Argv(0)
		text += " "
		text += g.gi.Args()
	} else {
		p := g.gi.Args()

		if strings.HasPrefix(p, "\"") {
			p = p[1:]
			if len(p) > 0 {
				p = p[:len(p)-1]
			}
		}
		text += p
	}

	// don't let text be too long for malicious reasons
	if len(text) > 150 {
		text = text[:150]
	}

	text += "\n"

	// the 3.19 inline flood check is the same code as the ctf fork's
	// CheckFlood (ctf/g_cmds.c:850), so both modes share it
	if g.CheckFlood(ent) {
		return
	}

	if g.dedicated.Value != 0 {
		g.gi.Cprintf(nil, PRINT_CHAT, text)
	}

	for j := int32(1); j <= g.game.Maxclients; j++ {
		other := &g.edicts[j]
		if !other.InUse {
			continue
		}
		if other.Client == nil {
			continue
		}
		if team {
			if !g.OnSameTeam(ent, other) {
				continue
			}
		}
		g.gi.Cprintf(other, PRINT_CHAT, text)
	}
}

// C: game/g_cmds.c:872 Cmd_PlayerList_f
func (g *Game) Cmd_PlayerList_f(ent *Edict) {
	// connect time, ping, score, name
	text := ""
	for i := 0; float32(i) < g.maxclients.Value; i++ {
		e2 := &g.edicts[1+i]
		if !e2.InUse {
			continue
		}

		spec := ""
		if e2.Client.Resp.Spectator {
			spec = " (spectator)"
		}
		st := fmt.Sprintf("%02d:%02d %4d %3d %s%s\n",
			(g.level.Framenum-e2.Client.Resp.Enterframe)/600,
			((g.level.Framenum-e2.Client.Resp.Enterframe)%600)/10,
			e2.Client.Ping,
			e2.Client.Resp.Score,
			e2.Client.Pers.Netname,
			spec)
		if len(text)+len(st) > 1400-50 {
			text += "And more...\n"
			g.gi.Cprintf(ent, PRINT_HIGH, text)
			return
		}
		text += st
	}
	g.gi.Cprintf(ent, PRINT_HIGH, text)
}

// ClientCommand is game_export_t.ClientCommand.
// C: game/g_cmds.c:908 ClientCommand
func (g *Game) ClientCommand(ent *Edict) {
	defer g.guard()
	if ent.Client == nil {
		return // not fully in game yet
	}

	cmd := g.gi.Argv(0)

	if shared.Q_stricmp(cmd, "players") == 0 {
		g.Cmd_Players_f(ent)
		return
	}
	if shared.Q_stricmp(cmd, "say") == 0 {
		g.Cmd_Say_f(ent, false, false)
		return
	}
	if g.ctfmod {
		//ZOID
		if shared.Q_stricmp(cmd, "say_team") == 0 || shared.Q_stricmp(cmd, "steam") == 0 {
			g.CTFSay_Team(ent, g.gi.Args())
			return
		}
	} else if shared.Q_stricmp(cmd, "say_team") == 0 {
		g.Cmd_Say_f(ent, true, false)
		return
	}
	if shared.Q_stricmp(cmd, "score") == 0 {
		g.Cmd_Score_f(ent)
		return
	}
	if shared.Q_stricmp(cmd, "help") == 0 {
		g.Cmd_Help_f(ent)
		return
	}

	if g.level.Intermissiontime != 0 {
		return
	}

	if shared.Q_stricmp(cmd, "use") == 0 {
		g.Cmd_Use_f(ent)
	} else if shared.Q_stricmp(cmd, "drop") == 0 {
		g.Cmd_Drop_f(ent)
	} else if shared.Q_stricmp(cmd, "give") == 0 {
		g.Cmd_Give_f(ent)
	} else if shared.Q_stricmp(cmd, "god") == 0 {
		g.Cmd_God_f(ent)
	} else if shared.Q_stricmp(cmd, "notarget") == 0 {
		g.Cmd_Notarget_f(ent)
	} else if shared.Q_stricmp(cmd, "noclip") == 0 {
		g.Cmd_Noclip_f(ent)
	} else if shared.Q_stricmp(cmd, "inven") == 0 {
		g.Cmd_Inven_f(ent)
	} else if shared.Q_stricmp(cmd, "invnext") == 0 {
		g.SelectNextItem(ent, -1)
	} else if shared.Q_stricmp(cmd, "invprev") == 0 {
		g.SelectPrevItem(ent, -1)
	} else if shared.Q_stricmp(cmd, "invnextw") == 0 {
		g.SelectNextItem(ent, IT_WEAPON)
	} else if shared.Q_stricmp(cmd, "invprevw") == 0 {
		g.SelectPrevItem(ent, IT_WEAPON)
	} else if shared.Q_stricmp(cmd, "invnextp") == 0 {
		g.SelectNextItem(ent, IT_POWERUP)
	} else if shared.Q_stricmp(cmd, "invprevp") == 0 {
		g.SelectPrevItem(ent, IT_POWERUP)
	} else if shared.Q_stricmp(cmd, "invuse") == 0 {
		g.Cmd_InvUse_f(ent)
	} else if shared.Q_stricmp(cmd, "invdrop") == 0 {
		g.Cmd_InvDrop_f(ent)
	} else if shared.Q_stricmp(cmd, "weapprev") == 0 {
		g.Cmd_WeapPrev_f(ent)
	} else if shared.Q_stricmp(cmd, "weapnext") == 0 {
		g.Cmd_WeapNext_f(ent)
	} else if shared.Q_stricmp(cmd, "weaplast") == 0 {
		g.Cmd_WeapLast_f(ent)
	} else if shared.Q_stricmp(cmd, "kill") == 0 {
		g.Cmd_Kill_f(ent)
	} else if shared.Q_stricmp(cmd, "putaway") == 0 {
		g.Cmd_PutAway_f(ent)
	} else if shared.Q_stricmp(cmd, "wave") == 0 {
		g.Cmd_Wave_f(ent)
	} else if !g.ctfmod && shared.Q_stricmp(cmd, "playerlist") == 0 {
		g.Cmd_PlayerList_f(ent)
	} else if g.ctfmod && g.ctfClientCommand(ent, cmd) {
		//ZOID: team, id, yes, no, ready, notready, ghost, admin, stats,
		// warp, boot, playerlist, observer (ctf/g_cmds.c:1034)
	} else { // anything that doesn't match a command will be a chat
		g.Cmd_Say_f(ent, false, true)
	}
}
