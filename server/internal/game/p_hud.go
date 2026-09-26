package game

// Port of game/p_hud.c: intermission, scoreboard, help computer and stats.

import (
	"strings"

	. "quake2web/server/internal/q2const"
)

// hudComSprintf emulates Com_sprintf(dest, size, fmt, ...) of q_shared.c
// including its overflow message (the game's Com_Printf goes to gi.dprintf).
// C: game/q_shared.c:1223 Com_sprintf
func (g *Game) hudComSprintf(size int, format string, args ...any) string {
	s := cfmt(format, args...)
	if len(s) >= size {
		g.dprintf("Com_sprintf: overflow of %i in %i\n", len(s), size)
		s = s[:size-1]
	}
	return s
}

/*
======================================================================

INTERMISSION

======================================================================
*/

// C: game/p_hud.c:32 MoveClientToIntermission
func (g *Game) MoveClientToIntermission(ent *Edict) {
	if g.deathmatch.Value != 0 || g.coop.Value != 0 {
		ent.Client.Showscores = true
	}
	ent.S.Origin = g.level.IntermissionOrigin
	ent.Client.PS.PMove.Origin[0] = int16(int32(g.level.IntermissionOrigin[0] * 8))
	ent.Client.PS.PMove.Origin[1] = int16(int32(g.level.IntermissionOrigin[1] * 8))
	ent.Client.PS.PMove.Origin[2] = int16(int32(g.level.IntermissionOrigin[2] * 8))
	ent.Client.PS.ViewAngles = g.level.IntermissionAngle
	ent.Client.PS.PMove.PmType = PM_FREEZE
	ent.Client.PS.GunIndex = 0
	ent.Client.PS.Blend[3] = 0
	ent.Client.PS.RDFlags &^= RDF_UNDERWATER

	// clean up powerup info
	ent.Client.QuadFramenum = 0
	ent.Client.InvincibleFramenum = 0
	ent.Client.BreatherFramenum = 0
	ent.Client.EnviroFramenum = 0
	ent.Client.GrenadeBlewUp = false
	ent.Client.GrenadeTime = 0

	ent.Viewheight = 0
	ent.S.ModelIndex = 0
	ent.S.ModelIndex2 = 0
	ent.S.ModelIndex3 = 0
	ent.S.ModelIndex = 0
	ent.S.Effects = 0
	ent.S.Sound = 0
	ent.Solid = SOLID_NOT

	// add the layout

	if g.deathmatch.Value != 0 || g.coop.Value != 0 {
		g.DeathmatchScoreboardMessage(ent, nil)
		g.gi.Unicast(ent, true)
	}
}

// C: game/p_hud.c:73 BeginIntermission
func (g *Game) BeginIntermission(targ *Edict) {
	var ent, client *Edict

	if g.level.Intermissiontime != 0 {
		return // already activated
	}

	//ZOID
	if g.deathmatch.Value != 0 && g.ctfOn() {
		g.CTFCalcScores()
	}
	//ZOID

	g.game.Autosaved = false

	// respawn any dead clients
	for i := 0; float32(i) < g.maxclients.Value; i++ {
		client = &g.edicts[1+i]
		if !client.InUse {
			continue
		}
		if client.Health <= 0 {
			g.respawn(client)
		}
	}

	g.level.Intermissiontime = g.level.Time
	g.level.Changemap = targ.Map

	if strings.Contains(g.level.Changemap, "*") {
		if g.coop.Value != 0 {
			for i := 0; float32(i) < g.maxclients.Value; i++ {
				client = &g.edicts[1+i]
				if !client.InUse {
					continue
				}
				// strip players of all keys between units
				for n := 0; n < MAX_ITEMS; n++ {
					if n < len(g.itemlist) && g.itemlist[n].Flags&IT_KEY != 0 {
						client.Client.Pers.Inventory[n] = 0
					}
				}
			}
		}
	} else {
		if g.deathmatch.Value == 0 {
			g.level.Exitintermission = 1 // go immediately to the next level
			return
		}
	}

	g.level.Exitintermission = 0

	// find an intermission spot
	ent = g.G_Find(nil, FOFS_classname, "info_player_intermission")
	if ent == nil {
		// the map creator forgot to put in an intermission point...
		ent = g.G_Find(nil, FOFS_classname, "info_player_start")
		if ent == nil {
			ent = g.G_Find(nil, FOFS_classname, "info_player_deathmatch")
		}
	} else {
		// chose one of four spots
		i := g.rng.Rand() & 3
		for ; i != 0; i-- {
			ent = g.G_Find(ent, FOFS_classname, "info_player_intermission")
			if ent == nil { // wrap around the list
				ent = g.G_Find(ent, FOFS_classname, "info_player_intermission")
			}
		}
	}

	g.level.IntermissionOrigin = ent.S.Origin
	g.level.IntermissionAngle = ent.S.Angles

	// move all clients to the intermission point
	for i := 0; float32(i) < g.maxclients.Value; i++ {
		client = &g.edicts[1+i]
		if !client.InUse {
			continue
		}
		g.MoveClientToIntermission(client)
	}
}

// C: game/p_hud.c:164 DeathmatchScoreboardMessage
func (g *Game) DeathmatchScoreboardMessage(ent, killer *Edict) {
	var sorted, sortedscores [MAX_CLIENTS]int32
	var j int

	//ZOID
	if g.ctfOn() {
		g.CTFScoreboardMessage(ent, killer)
		return
	}
	//ZOID

	// sort the clients by score
	total := 0
	for i := 0; i < int(g.game.Maxclients); i++ {
		clEnt := &g.edicts[1+i]
		if !clEnt.InUse || g.game.Clients[i].Resp.Spectator {
			continue
		}
		score := g.game.Clients[i].Resp.Score
		for j = 0; j < total; j++ {
			if score > sortedscores[j] {
				break
			}
		}
		for k := total; k > j; k-- {
			sorted[k] = sorted[k-1]
			sortedscores[k] = sortedscores[k-1]
		}
		sorted[j] = int32(i)
		sortedscores[j] = score
		total++
	}

	// print level name and exit rules
	var str strings.Builder
	stringlength := 0

	// add the clients in sorted order
	if total > 12 {
		total = 12
	}

	for i := 0; i < total; i++ {
		cl := &g.game.Clients[sorted[i]]
		clEnt := &g.edicts[1+sorted[i]]

		g.gi.ImageIndex("i_fixme") // picnum (unused)
		x := 0
		if i >= 6 {
			x = 160
		}
		y := 32 + 32*(i%6)

		// add a dogtag
		tag := ""
		if clEnt == ent {
			tag = "tag1"
		} else if clEnt == killer {
			tag = "tag2"
		}
		if tag != "" {
			entry := g.hudComSprintf(1024, "xv %i yv %i picn %s ", x+32, y, tag)
			j = len(entry)
			if stringlength+j > 1024 {
				break
			}
			str.WriteString(entry)
			stringlength += j
		}

		// send the layout
		entry := g.hudComSprintf(1024, "client %i %i %i %i %i %i ",
			x, y, sorted[i], cl.Resp.Score, cl.Ping, (g.level.Framenum-cl.Resp.Enterframe)/600)
		j = len(entry)
		if stringlength+j > 1024 {
			break
		}
		str.WriteString(entry)
		stringlength += j
	}

	g.gi.WriteByteC(svc_layout)
	g.gi.WriteString(str.String())
}

// DeathmatchScoreboard draws instead of help message.
// Note that it isn't that hard to overflow the 1400 byte message limit!
// C: game/p_hud.c:262 DeathmatchScoreboard
func (g *Game) DeathmatchScoreboard(ent *Edict) {
	g.DeathmatchScoreboardMessage(ent, ent.Enemy)
	g.gi.Unicast(ent, true)
}

// Cmd_Score_f displays the scoreboard.
// C: game/p_hud.c:276 Cmd_Score_f
func (g *Game) Cmd_Score_f(ent *Edict) {
	ent.Client.Showinventory = false
	ent.Client.Showhelp = false
	//ZOID
	if g.ctfmod && ent.Client.Menu != nil {
		g.PMenu_Close(ent)
	}
	//ZOID

	if g.deathmatch.Value == 0 && g.coop.Value == 0 {
		return
	}

	if ent.Client.Showscores {
		ent.Client.Showscores = false
		if g.ctfmod {
			ent.Client.UpdateChase = true
		}
		return
	}

	ent.Client.Showscores = true
	g.DeathmatchScoreboard(ent)
}

// HelpComputer draws help computer.
// C: game/p_hud.c:302 HelpComputer
func (g *Game) HelpComputer(ent *Edict) {
	var sk string

	if g.skill.Value == 0 {
		sk = "easy"
	} else if g.skill.Value == 1 {
		sk = "medium"
	} else if g.skill.Value == 2 {
		sk = "hard"
	} else {
		sk = "hard+"
	}

	// send the layout
	str := g.hudComSprintf(1024,
		"xv 32 yv 8 picn help "+ // background
			"xv 202 yv 12 string2 \"%s\" "+ // skill
			"xv 0 yv 24 cstring2 \"%s\" "+ // level name
			"xv 0 yv 54 cstring2 \"%s\" "+ // help 1
			"xv 0 yv 110 cstring2 \"%s\" "+ // help 2
			"xv 50 yv 164 string2 \" kills     goals    secrets\" "+
			"xv 50 yv 172 string2 \"%3i/%3i     %i/%i       %i/%i\" ",
		sk,
		g.level.LevelName,
		g.game.Helpmessage1,
		g.game.Helpmessage2,
		g.level.KilledMonsters, g.level.TotalMonsters,
		g.level.FoundGoals, g.level.TotalGoals,
		g.level.FoundSecrets, g.level.TotalSecrets)

	g.gi.WriteByteC(svc_layout)
	g.gi.WriteString(str)
	g.gi.Unicast(ent, true)
}

// Cmd_Help_f displays the current help message.
// C: game/p_hud.c:346 Cmd_Help_f
func (g *Game) Cmd_Help_f(ent *Edict) {
	// this is for backwards compatability
	if g.deathmatch.Value != 0 {
		g.Cmd_Score_f(ent)
		return
	}

	ent.Client.Showinventory = false
	ent.Client.Showscores = false

	if g.ctfmod {
		// C: ctf/p_hud.c:376 (the fork keeps these in client_respawn_t)
		if ent.Client.Showhelp && ent.Client.Resp.GameHelpchanged == g.game.Helpchanged {
			ent.Client.Showhelp = false
			return
		}

		ent.Client.Showhelp = true
		ent.Client.Resp.Helpchanged = 0
		g.HelpComputer(ent)
		return
	}

	if ent.Client.Showhelp && ent.Client.Pers.GameHelpchanged == g.game.Helpchanged {
		ent.Client.Showhelp = false
		return
	}

	ent.Client.Showhelp = true
	ent.Client.Pers.Helpchanged = 0
	g.HelpComputer(ent)
}

//=======================================================================

// C: game/p_hud.c:377 G_SetStats
func (g *Game) G_SetStats(ent *Edict) {
	var item *GItem
	var index, cells int32
	var power_armor_type int32
	stats := &ent.Client.PS.Stats

	//
	// health
	//
	stats[STAT_HEALTH_ICON] = int16(g.level.PicHealth)
	stats[STAT_HEALTH] = int16(ent.Health)

	//
	// ammo
	//
	if ent.Client.AmmoIndex == 0 /* || !ent->client->pers.inventory[ent->client->ammo_index] */ {
		stats[STAT_AMMO_ICON] = 0
		stats[STAT_AMMO] = 0
	} else {
		item = &g.itemlist[ent.Client.AmmoIndex]
		stats[STAT_AMMO_ICON] = int16(g.gi.ImageIndex(item.Icon))
		stats[STAT_AMMO] = int16(ent.Client.Pers.Inventory[ent.Client.AmmoIndex])
	}

	//
	// armor
	//
	power_armor_type = g.PowerArmorType(ent)
	if power_armor_type != 0 {
		cells = ent.Client.Pers.Inventory[ITEM_INDEX(g.FindItem("cells"))]
		if cells == 0 { // ran out of cells for power armor
			ent.Flags &^= FL_POWER_ARMOR
			g.gi.Sound(ent, CHAN_ITEM, g.gi.SoundIndex("misc/power2.wav"), 1, ATTN_NORM, 0)
			power_armor_type = 0
		}
	}

	index = g.ArmorIndex(ent)
	if power_armor_type != 0 && (index == 0 || g.level.Framenum&8 != 0) {
		// flash between power armor and other armor icon
		stats[STAT_ARMOR_ICON] = int16(g.gi.ImageIndex("i_powershield"))
		stats[STAT_ARMOR] = int16(cells)
	} else if index != 0 {
		item = g.GetItemByIndex(index)
		stats[STAT_ARMOR_ICON] = int16(g.gi.ImageIndex(item.Icon))
		stats[STAT_ARMOR] = int16(ent.Client.Pers.Inventory[index])
	} else {
		stats[STAT_ARMOR_ICON] = 0
		stats[STAT_ARMOR] = 0
	}

	//
	// pickup message
	//
	if g.level.Time > ent.Client.PickupMsgTime {
		stats[STAT_PICKUP_ICON] = 0
		stats[STAT_PICKUP_STRING] = 0
	}

	//
	// timers
	//
	fn := float32(g.level.Framenum)
	if ent.Client.QuadFramenum > fn {
		stats[STAT_TIMER_ICON] = int16(g.gi.ImageIndex("p_quad"))
		stats[STAT_TIMER] = int16(int32((ent.Client.QuadFramenum - fn) / 10))
	} else if ent.Client.InvincibleFramenum > fn {
		stats[STAT_TIMER_ICON] = int16(g.gi.ImageIndex("p_invulnerability"))
		stats[STAT_TIMER] = int16(int32((ent.Client.InvincibleFramenum - fn) / 10))
	} else if ent.Client.EnviroFramenum > fn {
		stats[STAT_TIMER_ICON] = int16(g.gi.ImageIndex("p_envirosuit"))
		stats[STAT_TIMER] = int16(int32((ent.Client.EnviroFramenum - fn) / 10))
	} else if ent.Client.BreatherFramenum > fn {
		stats[STAT_TIMER_ICON] = int16(g.gi.ImageIndex("p_rebreather"))
		stats[STAT_TIMER] = int16(int32((ent.Client.BreatherFramenum - fn) / 10))
	} else {
		stats[STAT_TIMER_ICON] = 0
		stats[STAT_TIMER] = 0
	}

	//
	// selected item
	//
	if ent.Client.Pers.SelectedItem == -1 {
		stats[STAT_SELECTED_ICON] = 0
	} else {
		stats[STAT_SELECTED_ICON] = int16(g.gi.ImageIndex(g.itemlist[ent.Client.Pers.SelectedItem].Icon))
	}

	stats[STAT_SELECTED_ITEM] = int16(ent.Client.Pers.SelectedItem)

	//
	// layouts
	//
	stats[STAT_LAYOUTS] = 0

	if g.deathmatch.Value != 0 {
		if ent.Client.Pers.Health <= 0 || g.level.Intermissiontime != 0 || ent.Client.Showscores {
			stats[STAT_LAYOUTS] |= 1
		}
		if ent.Client.Showinventory && ent.Client.Pers.Health > 0 {
			stats[STAT_LAYOUTS] |= 2
		}
	} else {
		if ent.Client.Showscores || ent.Client.Showhelp {
			stats[STAT_LAYOUTS] |= 1
		}
		if ent.Client.Showinventory && ent.Client.Pers.Health > 0 {
			stats[STAT_LAYOUTS] |= 2
		}
	}

	//
	// frags
	//
	stats[STAT_FRAGS] = int16(ent.Client.Resp.Score)

	//
	// help icon / current weapon if not shown
	//
	helpchanged := ent.Client.Pers.Helpchanged
	if g.ctfmod {
		helpchanged = ent.Client.Resp.Helpchanged
	}
	if helpchanged != 0 && g.level.Framenum&8 != 0 {
		stats[STAT_HELPICON] = int16(g.gi.ImageIndex("i_help"))
	} else if (ent.Client.Pers.Hand == CENTER_HANDED || ent.Client.PS.Fov > 91) && ent.Client.Pers.Weapon != nil {
		stats[STAT_HELPICON] = int16(g.gi.ImageIndex(ent.Client.Pers.Weapon.Icon))
	} else {
		stats[STAT_HELPICON] = 0
	}

	if g.ctfmod {
		//ZOID
		g.SetCTFStats(ent)
		//ZOID
		return
	}

	stats[STAT_SPECTATOR] = 0
}

// C: game/p_hud.c:530 G_CheckChaseStats
func (g *Game) G_CheckChaseStats(ent *Edict) {
	for i := 1; float32(i) <= g.maxclients.Value; i++ {
		cl := g.edicts[i].Client
		if !g.edicts[i].InUse || cl.ChaseTarget != ent {
			continue
		}
		cl.PS.Stats = ent.Client.PS.Stats
		g.G_SetSpectatorStats(&g.edicts[i])
	}
}

// C: game/p_hud.c:549 G_SetSpectatorStats
func (g *Game) G_SetSpectatorStats(ent *Edict) {
	cl := ent.Client

	if cl.ChaseTarget == nil {
		g.G_SetStats(ent)
	}

	cl.PS.Stats[STAT_SPECTATOR] = 1

	// layouts are independant in spectator
	cl.PS.Stats[STAT_LAYOUTS] = 0
	if cl.Pers.Health <= 0 || g.level.Intermissiontime != 0 || cl.Showscores {
		cl.PS.Stats[STAT_LAYOUTS] |= 1
	}
	if cl.Showinventory && cl.Pers.Health > 0 {
		cl.PS.Stats[STAT_LAYOUTS] |= 2
	}

	if cl.ChaseTarget != nil && cl.ChaseTarget.InUse {
		cl.PS.Stats[STAT_CHASE] = int16(CS_PLAYERSKINS + cl.ChaseTarget.Index - 1)
	} else {
		cl.PS.Stats[STAT_CHASE] = 0
	}
}
