package game

// Port of game/p_client.c: player spawning, death, connection and movement.

import (
	"fmt"
	"math"

	"quake2web/server/internal/pmove"
	. "quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/shared"
)

var (
	SP_FixCoopSpots    = defThink("SP_FixCoopSpots")
	SP_CreateCoopSpots = defThink("SP_CreateCoopSpots")
	player_pain        = defPain("player_pain")
	player_die         = defDie("player_die")
	body_die           = defDie("body_die")
)

func init() {
	SP_FixCoopSpots.bind((*Game).SP_FixCoopSpots)
	SP_CreateCoopSpots.bind((*Game).SP_CreateCoopSpots)
	player_pain.bind((*Game).player_pain)
	player_die.bind((*Game).player_die)
	body_die.bind((*Game).body_die)
}

// pclientStrncpy emulates strncpy(dst, src, size-1) into a zeroed char[size].
func pclientStrncpy(src string, size int) string {
	if len(src) > size-1 {
		return src[:size-1]
	}
	return src
}

//
// Gross, ugly, disgustuing hack section
//

// this function is an ugly as hell hack to fix some map flaws
//
// the coop spawn spots on some maps are SNAFU.  There are coop spots
// with the wrong targetname as well as spots with no name at all
//
// we use carnal knowledge of the maps to fix the coop spot targetnames to match
// that of the nearest named single player spot

// C: game/p_client.c:39 SP_FixCoopSpots
func (g *Game) SP_FixCoopSpots(self *Edict) {
	var spot *Edict

	for {
		spot = g.G_Find(spot, FOFS_classname, "info_player_start")
		if spot == nil {
			return
		}
		if spot.Targetname == "" {
			continue
		}
		d := shared.VectorSubtract(self.S.Origin, spot.S.Origin)
		if shared.VectorLength(d) < 384 {
			if self.Targetname == "" || shared.Q_stricmp(self.Targetname, spot.Targetname) != 0 {
				//				gi.dprintf("FixCoopSpots changed %s at %s targetname from %s to %s\n", self->classname, vtos(self->s.origin), self->targetname, spot->targetname);
				self.Targetname = spot.Targetname
			}
			return
		}
	}
}

// now if that one wasn't ugly enough for you then try this one on for size
// some maps don't have any coop spots at all, so we need to create them
// where they should have been

// C: game/p_client.c:70 SP_CreateCoopSpots
func (g *Game) SP_CreateCoopSpots(self *Edict) {
	if shared.Q_stricmp(g.level.Mapname, "security") == 0 {
		spot := g.G_Spawn()
		spot.Classname = "info_player_coop"
		spot.S.Origin[0] = 188 - 64
		spot.S.Origin[1] = -164
		spot.S.Origin[2] = 80
		spot.Targetname = "jail3"
		spot.S.Angles[1] = 90

		spot = g.G_Spawn()
		spot.Classname = "info_player_coop"
		spot.S.Origin[0] = 188 + 64
		spot.S.Origin[1] = -164
		spot.S.Origin[2] = 80
		spot.Targetname = "jail3"
		spot.S.Angles[1] = 90

		spot = g.G_Spawn()
		spot.Classname = "info_player_coop"
		spot.S.Origin[0] = 188 + 128
		spot.S.Origin[1] = -164
		spot.S.Origin[2] = 80
		spot.Targetname = "jail3"
		spot.S.Angles[1] = 90

		return
	}
}

// SP_info_player_start: the normal starting point for a level.
// C: game/p_client.c:108 SP_info_player_start
func (g *Game) SP_info_player_start(self *Edict) {
	if g.coop.Value == 0 {
		return
	}
	if shared.Q_stricmp(g.level.Mapname, "security") == 0 {
		// invoke one of our gross, ugly, disgusting hacks
		self.Think = SP_CreateCoopSpots
		self.Nextthink = float32(float64(g.level.Time) + FRAMETIME)
	}
}

// SP_info_player_deathmatch: potential spawning position for deathmatch games.
// C: game/p_client.c:123 SP_info_player_deathmatch
func (g *Game) SP_info_player_deathmatch(self *Edict) {
	if g.deathmatch.Value == 0 {
		g.G_FreeEdict(self)
		return
	}
	g.SP_misc_teleporter_dest(self)
}

// SP_info_player_coop: potential spawning position for coop games.
// C: game/p_client.c:137 SP_info_player_coop
func (g *Game) SP_info_player_coop(self *Edict) {
	if g.coop.Value == 0 {
		g.G_FreeEdict(self)
		return
	}

	m := g.level.Mapname
	if shared.Q_stricmp(m, "jail2") == 0 ||
		shared.Q_stricmp(m, "jail4") == 0 ||
		shared.Q_stricmp(m, "mine1") == 0 ||
		shared.Q_stricmp(m, "mine2") == 0 ||
		shared.Q_stricmp(m, "mine3") == 0 ||
		shared.Q_stricmp(m, "mine4") == 0 ||
		shared.Q_stricmp(m, "lab") == 0 ||
		shared.Q_stricmp(m, "boss1") == 0 ||
		shared.Q_stricmp(m, "fact3") == 0 ||
		shared.Q_stricmp(m, "biggun") == 0 ||
		shared.Q_stricmp(m, "space") == 0 ||
		shared.Q_stricmp(m, "command") == 0 ||
		shared.Q_stricmp(m, "power2") == 0 ||
		shared.Q_stricmp(m, "strike") == 0 {
		// invoke one of our gross, ugly, disgusting hacks
		self.Think = SP_FixCoopSpots
		self.Nextthink = float32(float64(g.level.Time) + FRAMETIME)
	}
}

// SP_info_player_intermission: the deathmatch intermission point will be at
// one of these. Use 'angles' instead of 'angle', so you can set pitch or
// roll as well as yaw.  'pitch yaw roll'
// C: game/p_client.c:171 SP_info_player_intermission
func (g *Game) SP_info_player_intermission(self *Edict) {
}

//=======================================================================

// C: game/p_client.c:179 player_pain
func (g *Game) player_pain(self, other *Edict, kick float32, damage int32) {
	// player pain is handled at the end of the frame in P_DamageFeedback
}

// C: game/p_client.c:185 IsFemale
func (g *Game) IsFemale(ent *Edict) bool {
	if ent.Client == nil {
		return false
	}

	key := "gender"
	if g.ctfmod {
		key = "skin" // C: ctf/p_client.c:192 (older base: no gender userinfo)
	}
	info := shared.Info_ValueForKey(ent.Client.Pers.Userinfo, key)
	if len(info) > 0 && (info[0] == 'f' || info[0] == 'F') {
		return true
	}
	return false
}

// C: game/p_client.c:198 IsNeutral
func (g *Game) IsNeutral(ent *Edict) bool {
	if ent.Client == nil || g.ctfmod { // no IsNeutral in the ctf fork's older base
		return false
	}

	info := shared.Info_ValueForKey(ent.Client.Pers.Userinfo, "gender")
	var c byte
	if len(info) > 0 {
		c = info[0]
	}
	if c != 'f' && c != 'F' && c != 'm' && c != 'M' {
		return true
	}
	return false
}

// C: game/p_client.c:211 ClientObituary
func (g *Game) ClientObituary(self, inflictor, attacker *Edict) {
	var mod int32
	var message, message2 string
	var ff bool

	if g.coop.Value != 0 && attacker.Client != nil {
		g.meansOfDeath |= MOD_FRIENDLY_FIRE
	}

	if g.deathmatch.Value != 0 || g.coop.Value != 0 {
		ff = g.meansOfDeath&MOD_FRIENDLY_FIRE != 0
		mod = g.meansOfDeath &^ MOD_FRIENDLY_FIRE
		message = ""
		message2 = ""

		switch mod {
		case MOD_SUICIDE:
			message = "suicides"
		case MOD_FALLING:
			message = "cratered"
		case MOD_CRUSH:
			message = "was squished"
		case MOD_WATER:
			message = "sank like a rock"
		case MOD_SLIME:
			message = "melted"
		case MOD_LAVA:
			message = "does a back flip into the lava"
		case MOD_EXPLOSIVE, MOD_BARREL:
			message = "blew up"
		case MOD_EXIT:
			message = "found a way out"
		case MOD_TARGET_LASER:
			message = "saw the light"
		case MOD_TARGET_BLASTER:
			message = "got blasted"
		case MOD_BOMB, MOD_SPLASH, MOD_TRIGGER_HURT:
			message = "was in the wrong place"
		}
		if attacker == self {
			switch mod {
			case MOD_HELD_GRENADE:
				message = "tried to put the pin back in"
			case MOD_HG_SPLASH, MOD_G_SPLASH:
				if g.IsNeutral(self) {
					message = "tripped on its own grenade"
				} else if g.IsFemale(self) {
					message = "tripped on her own grenade"
				} else {
					message = "tripped on his own grenade"
				}
			case MOD_R_SPLASH:
				if g.IsNeutral(self) {
					message = "blew itself up"
				} else if g.IsFemale(self) {
					message = "blew herself up"
				} else {
					message = "blew himself up"
				}
			case MOD_BFG_BLAST:
				message = "should have used a smaller gun"
			default:
				if g.IsNeutral(self) {
					message = "killed itself"
				} else if g.IsFemale(self) {
					message = "killed herself"
				} else {
					message = "killed himself"
				}
			}
		}
		if message != "" {
			g.bprintf(PRINT_MEDIUM, "%s %s.\n", self.Client.Pers.Netname, message)
			if g.deathmatch.Value != 0 {
				self.Client.Resp.Score--
			}
			self.Enemy = nil
			return
		}

		self.Enemy = attacker
		if attacker != nil && attacker.Client != nil {
			switch mod {
			case MOD_BLASTER:
				message = "was blasted by"
			case MOD_SHOTGUN:
				message = "was gunned down by"
			case MOD_SSHOTGUN:
				message = "was blown away by"
				message2 = "'s super shotgun"
			case MOD_MACHINEGUN:
				message = "was machinegunned by"
			case MOD_CHAINGUN:
				message = "was cut in half by"
				message2 = "'s chaingun"
			case MOD_GRENADE:
				message = "was popped by"
				message2 = "'s grenade"
			case MOD_G_SPLASH:
				message = "was shredded by"
				message2 = "'s shrapnel"
			case MOD_ROCKET:
				message = "ate"
				message2 = "'s rocket"
			case MOD_R_SPLASH:
				message = "almost dodged"
				message2 = "'s rocket"
			case MOD_HYPERBLASTER:
				message = "was melted by"
				message2 = "'s hyperblaster"
			case MOD_RAILGUN:
				message = "was railed by"
			case MOD_BFG_LASER:
				message = "saw the pretty lights from"
				message2 = "'s BFG"
			case MOD_BFG_BLAST:
				message = "was disintegrated by"
				message2 = "'s BFG blast"
			case MOD_BFG_EFFECT:
				message = "couldn't hide from"
				message2 = "'s BFG"
			case MOD_HANDGRENADE:
				message = "caught"
				message2 = "'s handgrenade"
			case MOD_HG_SPLASH:
				message = "didn't see"
				message2 = "'s handgrenade"
			case MOD_HELD_GRENADE:
				message = "feels"
				message2 = "'s pain"
			case MOD_TELEFRAG:
				message = "tried to invade"
				message2 = "'s personal space"
			//ZOID
			case MOD_GRAPPLE:
				if g.ctfmod {
					message = "was caught by"
					message2 = "'s grapple"
				}
				//ZOID
			}
			if message != "" {
				g.bprintf(PRINT_MEDIUM, "%s %s %s%s\n", self.Client.Pers.Netname, message, attacker.Client.Pers.Netname, message2)
				if g.deathmatch.Value != 0 {
					if ff {
						attacker.Client.Resp.Score--
					} else {
						attacker.Client.Resp.Score++
					}
				}
				return
			}
		}
	}

	g.bprintf(PRINT_MEDIUM, "%s died.\n", self.Client.Pers.Netname)
	if g.deathmatch.Value != 0 {
		self.Client.Resp.Score--
	}
}

// C: game/p_client.c:410 TossClientWeapon
func (g *Game) TossClientWeapon(self *Edict) {
	var item *GItem
	var drop *Edict
	var quad bool
	var spread float32

	if g.deathmatch.Value == 0 {
		return
	}

	item = self.Client.Pers.Weapon
	if self.Client.Pers.Inventory[self.Client.AmmoIndex] == 0 {
		item = nil
	}
	if item != nil && item.PickupName == "Blaster" {
		item = nil
	}

	if int32(g.dmflags.Value)&DF_QUAD_DROP == 0 {
		quad = false
	} else {
		quad = self.Client.QuadFramenum > float32(g.level.Framenum+10)
	}

	if item != nil && quad {
		spread = 22.5
	} else {
		spread = 0.0
	}

	if item != nil {
		self.Client.VAngle[YAW] -= spread
		drop = g.Drop_Item(self, item)
		self.Client.VAngle[YAW] += spread
		drop.Spawnflags = DROPPED_PLAYER_ITEM
	}

	if quad {
		self.Client.VAngle[YAW] += spread
		drop = g.Drop_Item(self, g.FindItemByClassname("item_quad"))
		self.Client.VAngle[YAW] -= spread
		drop.Spawnflags |= DROPPED_PLAYER_ITEM

		drop.Touch = Touch_Item
		drop.Nextthink = float32(float64(g.level.Time) + float64(self.Client.QuadFramenum-float32(g.level.Framenum))*FRAMETIME)
		drop.Think = G_FreeEdict
	}
}

// C: game/p_client.c:463 LookAtKiller
func (g *Game) LookAtKiller(self, inflictor, attacker *Edict) {
	var dir Vec3

	if attacker != nil && attacker != g.world() && attacker != self {
		dir = shared.VectorSubtract(attacker.S.Origin, self.S.Origin)
	} else if inflictor != nil && inflictor != g.world() && inflictor != self {
		dir = shared.VectorSubtract(inflictor.S.Origin, self.S.Origin)
	} else {
		self.Client.KillerYaw = self.S.Angles[YAW]
		return
	}

	if dir[0] != 0 {
		self.Client.KillerYaw = float32(180 / shared.MPI * math.Atan2(float64(dir[1]), float64(dir[0])))
	} else {
		self.Client.KillerYaw = 0
		if dir[1] > 0 {
			self.Client.KillerYaw = 90
		} else if dir[1] < 0 {
			self.Client.KillerYaw = -90
		}
	}
	if self.Client.KillerYaw < 0 {
		self.Client.KillerYaw += 360
	}
}

// C: game/p_client.c:501 player_die
func (g *Game) player_die(self, inflictor, attacker *Edict, damage int32, point Vec3) {
	self.Avelocity = Vec3{}

	self.Takedamage = DAMAGE_YES
	self.Movetype = MOVETYPE_TOSS

	self.S.ModelIndex2 = 0 // remove linked weapon model
	//ZOID
	if g.ctfmod {
		self.S.ModelIndex3 = 0 // remove linked ctf flag
	}
	//ZOID

	self.S.Angles[0] = 0
	self.S.Angles[2] = 0

	self.S.Sound = 0
	self.Client.WeaponSound = 0

	self.Maxs[2] = -8

	//	self->solid = SOLID_NOT;
	self.SVFlags |= SVF_DEADMONSTER

	if self.Deadflag == 0 {
		self.Client.RespawnTime = float32(float64(g.level.Time) + 1.0)
		g.LookAtKiller(self, inflictor, attacker)
		self.Client.PS.PMove.PmType = PM_DEAD
		g.ClientObituary(self, inflictor, attacker)
		if g.ctfmod {
			//ZOID: C: ctf/p_client.c:520
			// if at start and same team, clear
			if g.ctfOn() && g.meansOfDeath == MOD_TELEFRAG &&
				self.Client.Resp.CtfState < 2 &&
				self.Client.Resp.CtfTeam == attacker.Client.Resp.CtfTeam {
				attacker.Client.Resp.Score--
				self.Client.Resp.CtfState = 0
			}

			g.CTFFragBonuses(self, inflictor, attacker)
			//ZOID
			g.TossClientWeapon(self)
			//ZOID
			g.CTFPlayerResetGrapple(self)
			g.CTFDeadDropFlag(self)
			g.CTFDeadDropTech(self)
			//ZOID
			if g.deathmatch.Value != 0 && !self.Client.Showscores {
				g.Cmd_Help_f(self) // show scores
			}
		} else {
			g.TossClientWeapon(self)
			if g.deathmatch.Value != 0 {
				g.Cmd_Help_f(self) // show scores
			}

			// clear inventory
			// this is kind of ugly, but it's how we want to handle keys in coop
			for n := 0; n < int(g.game.NumItems); n++ {
				if g.coop.Value != 0 && g.itemlist[n].Flags&IT_KEY != 0 {
					self.Client.Resp.CoopRespawn.Inventory[n] = self.Client.Pers.Inventory[n]
				}
				self.Client.Pers.Inventory[n] = 0
			}
		}
	}

	// remove powerups
	self.Client.QuadFramenum = 0
	self.Client.InvincibleFramenum = 0
	self.Client.BreatherFramenum = 0
	self.Client.EnviroFramenum = 0
	if g.ctfmod {
		// C: ctf/p_client.c:547 (older base: no FL_POWER_ARMOR reset;
		// the inventory is cleared on every call)
		// clear inventory
		self.Client.Pers.Inventory = [MAX_ITEMS]int32{}
	} else {
		self.Flags &^= FL_POWER_ARMOR
	}

	if self.Health < -40 {
		// gib
		g.gi.Sound(self, CHAN_BODY, g.gi.SoundIndex("misc/udeath.wav"), 1, ATTN_NORM, 0)
		for n := 0; n < 4; n++ {
			g.ThrowGib(self, "models/objects/gibs/sm_meat/tris.md2", damage, GIB_ORGANIC)
		}
		g.ThrowClientHead(self, damage)
		//ZOID
		if g.ctfmod {
			self.Client.AnimPriority = ANIM_DEATH
			self.Client.AnimEnd = 0
		}
		//ZOID
		self.Takedamage = DAMAGE_NO
	} else {
		// normal death
		if self.Deadflag == 0 {
			// C: static int i (function-level static)
			g.player_die_i = (g.player_die_i + 1) % 3
			// start a death animation
			self.Client.AnimPriority = ANIM_DEATH
			if self.Client.PS.PMove.PmFlags&PMF_DUCKED != 0 {
				self.S.Frame = FRAME_crdeath1 - 1
				self.Client.AnimEnd = FRAME_crdeath5
			} else {
				switch g.player_die_i {
				case 0:
					self.S.Frame = FRAME_death101 - 1
					self.Client.AnimEnd = FRAME_death106
				case 1:
					self.S.Frame = FRAME_death201 - 1
					self.Client.AnimEnd = FRAME_death206
				case 2:
					self.S.Frame = FRAME_death301 - 1
					self.Client.AnimEnd = FRAME_death308
				}
			}
			g.gi.Sound(self, CHAN_VOICE, g.gi.SoundIndex(fmt.Sprintf("*death%d.wav", (g.rng.Rand()%4)+1)), 1, ATTN_NORM, 0)
		}
	}

	self.Deadflag = DEAD_DEAD

	g.gi.LinkEntity(self)
}

//=======================================================================

// InitClientPersistant is only called when the game first initializes in
// single player, but is called after each death and level change in deathmatch.
// C: game/p_client.c:607 InitClientPersistant
func (g *Game) InitClientPersistant(client *GClient) {
	client.Pers = ClientPersistant{}

	item := g.FindItem("Blaster")
	client.Pers.SelectedItem = ITEM_INDEX(item)
	client.Pers.Inventory[client.Pers.SelectedItem] = 1

	client.Pers.Weapon = item
	if g.ctfmod {
		//ZOID
		client.Pers.Lastweapon = item
		//ZOID

		//ZOID
		item = g.FindItem("Grapple")
		client.Pers.Inventory[ITEM_INDEX(item)] = 1
		//ZOID
	}

	client.Pers.Health = 100
	client.Pers.MaxHealth = 100

	client.Pers.MaxBullets = 200
	client.Pers.MaxShells = 100
	client.Pers.MaxRockets = 50
	client.Pers.MaxGrenades = 50
	client.Pers.MaxCells = 200
	client.Pers.MaxSlugs = 50

	client.Pers.Connected = true
}

// C: game/p_client.c:633 InitClientResp
func (g *Game) InitClientResp(client *GClient) {
	//ZOID
	ctf_team := client.Resp.CtfTeam
	id_state := client.Resp.IdState
	//ZOID

	client.Resp = ClientRespawn{}

	//ZOID
	if g.ctfmod {
		client.Resp.CtfTeam = ctf_team
		client.Resp.IdState = id_state
	}
	//ZOID

	client.Resp.Enterframe = g.level.Framenum
	client.Resp.CoopRespawn = client.Pers

	//ZOID
	if g.ctfOn() && client.Resp.CtfTeam < CTF_TEAM1 {
		g.CTFAssignTeam(client)
	}
	//ZOID
}

// SaveClientData: some information that should be persistant, like health,
// is still stored in the edict structure, so it needs to be mirrored out to
// the client structure before all the edicts are wiped.
// C: game/p_client.c:650 SaveClientData
func (g *Game) SaveClientData() {
	for i := 0; i < int(g.game.Maxclients); i++ {
		ent := &g.edicts[1+i]
		if !ent.InUse {
			continue
		}
		g.game.Clients[i].Pers.Health = ent.Health
		g.game.Clients[i].Pers.MaxHealth = ent.MaxHealth
		if g.ctfmod {
			// C: ctf/p_client.c:689 pers.powerArmorActive = (ent->flags & FL_POWER_ARMOR)
			g.game.Clients[i].Pers.SavedFlags = ent.Flags & FL_POWER_ARMOR
		} else {
			g.game.Clients[i].Pers.SavedFlags = ent.Flags & (FL_GODMODE | FL_NOTARGET | FL_POWER_ARMOR)
		}
		if g.coop.Value != 0 {
			g.game.Clients[i].Pers.Score = ent.Client.Resp.Score
		}
	}
}

// C: game/p_client.c:668 FetchClientEntData
func (g *Game) FetchClientEntData(ent *Edict) {
	ent.Health = ent.Client.Pers.Health
	ent.MaxHealth = ent.Client.Pers.MaxHealth
	ent.Flags |= ent.Client.Pers.SavedFlags
	if g.coop.Value != 0 {
		ent.Client.Resp.Score = ent.Client.Pers.Score
	}
}

/*
=======================================================================

  SelectSpawnPoint

=======================================================================
*/

// PlayersRangeFromSpot returns the distance to the nearest player from the
// given spot.
// C: game/p_client.c:694 PlayersRangeFromSpot
func (g *Game) PlayersRangeFromSpot(spot *Edict) float32 {
	var bestplayerdistance float32 = 9999999

	for n := 1; float32(n) <= g.maxclients.Value; n++ {
		player := &g.edicts[n]

		if !player.InUse {
			continue
		}

		if player.Health <= 0 {
			continue
		}

		v := shared.VectorSubtract(spot.S.Origin, player.S.Origin)
		playerdistance := shared.VectorLength(v)

		if playerdistance < bestplayerdistance {
			bestplayerdistance = playerdistance
		}
	}

	return bestplayerdistance
}

// SelectRandomDeathmatchSpawnPoint: go to a random point, but NOT the two
// points closest to other players.
// C: game/p_client.c:733 SelectRandomDeathmatchSpawnPoint
func (g *Game) SelectRandomDeathmatchSpawnPoint() *Edict {
	var spot, spot1, spot2 *Edict
	var count int32
	var selection int32
	var rng, range1, range2 float32

	range1, range2 = 99999, 99999

	for {
		spot = g.G_Find(spot, FOFS_classname, "info_player_deathmatch")
		if spot == nil {
			break
		}
		count++
		rng = g.PlayersRangeFromSpot(spot)
		if rng < range1 {
			range1 = rng
			spot1 = spot
		} else if rng < range2 {
			range2 = rng
			spot2 = spot
		}
	}

	if count == 0 {
		return nil
	}

	if count <= 2 {
		spot1, spot2 = nil, nil
	} else {
		count -= 2
	}

	selection = g.rng.Rand() % count

	spot = nil
	for {
		spot = g.G_Find(spot, FOFS_classname, "info_player_deathmatch")
		if spot == spot1 || spot == spot2 {
			selection++
		}
		cont := selection != 0
		selection--
		if !cont {
			break
		}
	}

	return spot
}

// C: game/p_client.c:789 SelectFarthestDeathmatchSpawnPoint
func (g *Game) SelectFarthestDeathmatchSpawnPoint() *Edict {
	var bestspot, spot *Edict
	var bestdistance, bestplayerdistance float32

	for {
		spot = g.G_Find(spot, FOFS_classname, "info_player_deathmatch")
		if spot == nil {
			break
		}
		bestplayerdistance = g.PlayersRangeFromSpot(spot)

		if bestplayerdistance > bestdistance {
			bestspot = spot
			bestdistance = bestplayerdistance
		}
	}

	if bestspot != nil {
		return bestspot
	}

	// if there is a player just spawned on each and every start spot
	// we have no choice to turn one into a telefrag meltdown
	spot = g.G_Find(nil, FOFS_classname, "info_player_deathmatch")

	return spot
}

// C: game/p_client.c:822 SelectDeathmatchSpawnPoint
func (g *Game) SelectDeathmatchSpawnPoint() *Edict {
	if int32(g.dmflags.Value)&DF_SPAWN_FARTHEST != 0 {
		return g.SelectFarthestDeathmatchSpawnPoint()
	}
	return g.SelectRandomDeathmatchSpawnPoint()
}

// C: game/p_client.c:831 SelectCoopSpawnPoint
func (g *Game) SelectCoopSpawnPoint(ent *Edict) *Edict {
	var spot *Edict

	index := ent.Client.Index

	// player 0 starts in normal player spawn point
	if index == 0 {
		return nil
	}

	spot = nil

	// assume there are four coop spots at each spawnpoint
	for {
		spot = g.G_Find(spot, FOFS_classname, "info_player_coop")
		if spot == nil {
			return nil // we didn't have enough...
		}

		target := spot.Targetname
		if shared.Q_stricmp(g.game.Spawnpoint, target) == 0 {
			// this is a coop spawn point for one of the clients here
			index--
			if index == 0 {
				return spot // this is it
			}
		}
	}
}

// SelectSpawnPoint chooses a player start, deathmatch start, coop start, etc.
// C: game/p_client.c:875 SelectSpawnPoint
func (g *Game) SelectSpawnPoint(ent *Edict) (origin, angles Vec3) {
	var spot *Edict

	if g.deathmatch.Value != 0 {
		//ZOID
		if g.ctfOn() {
			spot = g.SelectCTFSpawnPoint(ent)
		} else {
			//ZOID
			spot = g.SelectDeathmatchSpawnPoint()
		}
	} else if g.coop.Value != 0 {
		spot = g.SelectCoopSpawnPoint(ent)
	}

	// find a single player start spot
	if spot == nil {
		for {
			spot = g.G_Find(spot, FOFS_classname, "info_player_start")
			if spot == nil {
				break
			}
			if g.game.Spawnpoint == "" && spot.Targetname == "" {
				break
			}

			if g.game.Spawnpoint == "" || spot.Targetname == "" {
				continue
			}

			if shared.Q_stricmp(g.game.Spawnpoint, spot.Targetname) == 0 {
				break
			}
		}

		if spot == nil {
			if g.game.Spawnpoint == "" {
				// there wasn't a spawnpoint without a target, so use any
				spot = g.G_Find(spot, FOFS_classname, "info_player_start")
			}
			if spot == nil {
				g.error("Couldn't find spawn point %s\n", g.game.Spawnpoint)
			}
		}
	}

	origin = spot.S.Origin
	origin[2] += 9
	angles = spot.S.Angles
	return origin, angles
}

//======================================================================

// C: game/p_client.c:918 InitBodyQue
func (g *Game) InitBodyQue() {
	g.level.BodyQue = 0
	for i := 0; i < BODY_QUEUE_SIZE; i++ {
		ent := g.G_Spawn()
		ent.Classname = "bodyque"
	}
}

// C: game/p_client.c:931 body_die
func (g *Game) body_die(self, inflictor, attacker *Edict, damage int32, point Vec3) {
	if self.Health < -40 {
		g.gi.Sound(self, CHAN_BODY, g.gi.SoundIndex("misc/udeath.wav"), 1, ATTN_NORM, 0)
		for n := 0; n < 4; n++ {
			g.ThrowGib(self, "models/objects/gibs/sm_meat/tris.md2", damage, GIB_ORGANIC)
		}
		self.S.Origin[2] -= 48
		g.ThrowClientHead(self, damage)
		self.Takedamage = DAMAGE_NO
	}
}

// C: game/p_client.c:946 CopyToBodyQue
func (g *Game) CopyToBodyQue(ent *Edict) {
	// grab a body que and cycle to the next one
	body := &g.edicts[int(int32(g.maxclients.Value))+int(g.level.BodyQue)+1]
	g.level.BodyQue = (g.level.BodyQue + 1) % BODY_QUEUE_SIZE

	// FIXME: send an effect on the removed body

	g.gi.UnlinkEntity(ent)

	g.gi.UnlinkEntity(body)
	body.S = ent.S
	body.S.Number = int32(body.Index)

	body.SVFlags = ent.SVFlags
	body.Mins = ent.Mins
	body.Maxs = ent.Maxs
	body.AbsMin = ent.AbsMin
	body.AbsMax = ent.AbsMax
	body.Size = ent.Size
	body.Solid = ent.Solid
	body.ClipMask = ent.ClipMask
	body.Owner = ent.Owner
	body.Movetype = ent.Movetype

	body.Die = body_die
	body.Takedamage = DAMAGE_YES

	g.gi.LinkEntity(body)
}

// C: game/p_client.c:980 respawn
func (g *Game) respawn(self *Edict) {
	if g.deathmatch.Value != 0 || g.coop.Value != 0 {
		// spectator's don't leave bodies
		if self.Movetype != MOVETYPE_NOCLIP {
			g.CopyToBodyQue(self)
		}
		self.SVFlags &^= SVF_NOCLIENT
		g.PutClientInServer(self)

		// add a teleportation effect
		self.S.Event = EV_PLAYER_TELEPORT

		// hold in place briefly
		self.Client.PS.PMove.PmFlags = PMF_TIME_TELEPORT
		self.Client.PS.PMove.PmTime = 14

		self.Client.RespawnTime = g.level.Time

		return
	}

	// restart the entire server
	g.gi.AddCommandString("menu_loadgame\n")
}

// spectator_respawn is only called when pers.spectator changes.
// note that resp.spectator should be the opposite of pers.spectator here
// C: game/p_client.c:1010 spectator_respawn
func (g *Game) spectator_respawn(ent *Edict) {
	// if the user wants to become a spectator, make sure he doesn't
	// exceed max_spectators

	if ent.Client.Pers.Spectator {
		value := shared.Info_ValueForKey(ent.Client.Pers.Userinfo, "spectator")
		if g.spectator_password.String != "" &&
			g.spectator_password.String != "none" &&
			g.spectator_password.String != value {
			g.gi.Cprintf(ent, PRINT_HIGH, "Spectator password incorrect.\n")
			ent.Client.Pers.Spectator = false
			g.gi.WriteByteC(svc_stufftext)
			g.gi.WriteString("spectator 0\n")
			g.gi.Unicast(ent, true)
			return
		}

		// count spectators
		numspec := 0
		for i := 1; float32(i) <= g.maxclients.Value; i++ {
			if g.edicts[i].InUse && g.edicts[i].Client.Pers.Spectator {
				numspec++
			}
		}

		if float32(numspec) >= g.maxspectators.Value {
			g.gi.Cprintf(ent, PRINT_HIGH, "Server spectator limit is full.")
			ent.Client.Pers.Spectator = false
			// reset his spectator var
			g.gi.WriteByteC(svc_stufftext)
			g.gi.WriteString("spectator 0\n")
			g.gi.Unicast(ent, true)
			return
		}
	} else {
		// he was a spectator and wants to join the game
		// he must have the right password
		value := shared.Info_ValueForKey(ent.Client.Pers.Userinfo, "password")
		if g.password.String != "" && g.password.String != "none" &&
			g.password.String != value {
			g.gi.Cprintf(ent, PRINT_HIGH, "Password incorrect.\n")
			ent.Client.Pers.Spectator = true
			g.gi.WriteByteC(svc_stufftext)
			g.gi.WriteString("spectator 1\n")
			g.gi.Unicast(ent, true)
			return
		}
	}

	// clear score on respawn
	ent.Client.Resp.Score = 0
	ent.Client.Pers.Score = 0

	ent.SVFlags &^= SVF_NOCLIENT
	g.PutClientInServer(ent)

	// add a teleportation effect
	if !ent.Client.Pers.Spectator {
		// send effect
		g.gi.WriteByteC(svc_muzzleflash)
		g.gi.WriteShort(ent.Index)
		g.gi.WriteByteC(MZ_LOGIN)
		g.gi.Multicast(&ent.S.Origin, MULTICAST_PVS)

		// hold in place briefly
		ent.Client.PS.PMove.PmFlags = PMF_TIME_TELEPORT
		ent.Client.PS.PMove.PmTime = 14
	}

	ent.Client.RespawnTime = g.level.Time

	if ent.Client.Pers.Spectator {
		g.bprintf(PRINT_HIGH, "%s has moved to the sidelines\n", ent.Client.Pers.Netname)
	} else {
		g.bprintf(PRINT_HIGH, "%s joined the game\n", ent.Client.Pers.Netname)
	}
}

//==============================================================

// PutClientInServer is called when a player connects to a server or
// respawns in a deathmatch.
// C: game/p_client.c:1097 PutClientInServer
func (g *Game) PutClientInServer(ent *Edict) {
	mins := Vec3{-16, -16, -24}
	maxs := Vec3{16, 16, 32}
	var resp ClientRespawn

	// find a spawn point
	// do it before setting health back up, so farthest
	// ranging doesn't count this client
	spawn_origin, spawn_angles := g.SelectSpawnPoint(ent)

	index := ent.Index - 1
	client := ent.Client

	// deathmatch wipes most client data every spawn
	if g.deathmatch.Value != 0 {
		resp = client.Resp
		userinfo := client.Pers.Userinfo
		g.InitClientPersistant(client)
		g.clientUserinfoChanged(ent, userinfo)
	} else if g.coop.Value != 0 {
		resp = client.Resp
		userinfo := client.Pers.Userinfo
		// this is kind of ugly, but it's how we want to handle keys in coop
		if g.ctfmod {
			// C: ctf/p_client.c:1087 (older base; coop is forced off in ctf)
			for n := 0; n < MAX_ITEMS; n++ {
				if n < len(g.itemlist) && g.itemlist[n].Flags&IT_KEY != 0 {
					resp.CoopRespawn.Inventory[n] = client.Pers.Inventory[n]
				}
			}
		} else {
			//		for (n = 0; n < game.num_items; n++)
			//		{
			//			if (itemlist[n].flags & IT_KEY)
			//				resp.coop_respawn.inventory[n] = client->pers.inventory[n];
			//		}
			resp.CoopRespawn.GameHelpchanged = client.Pers.GameHelpchanged
			resp.CoopRespawn.Helpchanged = client.Pers.Helpchanged
		}
		client.Pers = resp.CoopRespawn
		g.clientUserinfoChanged(ent, userinfo)
		if resp.Score > client.Pers.Score {
			client.Pers.Score = resp.Score
		}
	} else {
		resp = ClientRespawn{}
	}

	// clear everything but the persistant data
	saved := client.Pers
	*client = GClient{Index: client.Index}
	client.Pers = saved
	if client.Pers.Health <= 0 {
		g.InitClientPersistant(client)
	}
	client.Resp = resp

	// copy some data from the client to the entity
	g.FetchClientEntData(ent)

	// clear entity values
	ent.Groundentity = nil
	ent.Client = &g.game.Clients[index]
	ent.Takedamage = DAMAGE_AIM
	ent.Movetype = MOVETYPE_WALK
	ent.Viewheight = 22
	ent.InUse = true
	ent.Classname = "player"
	ent.Mass = 200
	ent.Solid = SOLID_BBOX
	ent.Deadflag = DEAD_NO
	ent.AirFinished = g.level.Time + 12
	ent.ClipMask = MASK_PLAYERSOLID
	ent.Model = "players/male/tris.md2"
	ent.Pain = player_pain
	ent.Die = player_die
	ent.Waterlevel = 0
	ent.Watertype = 0
	ent.Flags &^= FL_NO_KNOCKBACK
	ent.SVFlags &^= SVF_DEADMONSTER

	ent.Mins = mins
	ent.Maxs = maxs
	ent.Velocity = Vec3{}

	// clear playerstate values
	ent.Client.PS = shared.PlayerState{}

	client.PS.PMove.Origin[0] = int16(int32(spawn_origin[0] * 8))
	client.PS.PMove.Origin[1] = int16(int32(spawn_origin[1] * 8))
	client.PS.PMove.Origin[2] = int16(int32(spawn_origin[2] * 8))

	if g.deathmatch.Value != 0 && int32(g.dmflags.Value)&DF_FIXED_FOV != 0 {
		client.PS.Fov = 90
	} else {
		client.PS.Fov = float32(shared.Atoi(shared.Info_ValueForKey(client.Pers.Userinfo, "fov")))
		if client.PS.Fov < 1 {
			client.PS.Fov = 90
		} else if client.PS.Fov > 160 {
			client.PS.Fov = 160
		}
	}

	client.PS.GunIndex = int32(g.gi.ModelIndex(client.Pers.Weapon.ViewModel))

	// clear entity state values
	ent.S.Effects = 0
	ent.S.ModelIndex = 255  // will use the skin specified model
	ent.S.ModelIndex2 = 255 // custom gun model
	// sknum is player num and weapon number
	// weapon number will be added in changeweapon
	ent.S.SkinNum = int32(ent.Index - 1)

	ent.S.Frame = 0
	ent.S.Origin = spawn_origin
	ent.S.Origin[2] += 1 // make sure off ground
	ent.S.OldOrigin = ent.S.Origin

	// set the delta angle
	for i := 0; i < 3; i++ {
		client.PS.PMove.DeltaAngles[i] = int16(shared.ANGLE2SHORT(spawn_angles[i] - client.Resp.CmdAngles[i]))
	}

	ent.S.Angles[PITCH] = 0
	ent.S.Angles[YAW] = spawn_angles[YAW]
	ent.S.Angles[ROLL] = 0
	client.PS.ViewAngles = ent.S.Angles
	client.VAngle = ent.S.Angles

	//ZOID
	if g.ctfmod {
		if g.CTFStartClient(ent) {
			return
		}
	} else if client.Pers.Spectator { // spawn a spectator
		client.ChaseTarget = nil

		client.Resp.Spectator = true

		ent.Movetype = MOVETYPE_NOCLIP
		ent.Solid = SOLID_NOT
		ent.SVFlags |= SVF_NOCLIENT
		ent.Client.PS.GunIndex = 0
		g.gi.LinkEntity(ent)
		return
	} else {
		client.Resp.Spectator = false
	}

	if !g.KillBox(ent) {
		// could't spawn in?
	}

	g.gi.LinkEntity(ent)

	// force the current weapon up
	client.Newweapon = client.Pers.Weapon
	g.ChangeWeapon(ent)
}

// ClientBeginDeathmatch: a client has just connected to the server in
// deathmatch mode, so clear everything out before starting them.
// C: game/p_client.c:1266 ClientBeginDeathmatch
func (g *Game) ClientBeginDeathmatch(ent *Edict) {
	g.G_InitEdict(ent)

	g.InitClientResp(ent.Client)

	// locate ent at a spawn point
	g.PutClientInServer(ent)

	// send effect
	g.gi.WriteByteC(svc_muzzleflash)
	g.gi.WriteShort(ent.Index)
	g.gi.WriteByteC(MZ_LOGIN)
	g.gi.Multicast(&ent.S.Origin, MULTICAST_PVS)

	g.bprintf(PRINT_HIGH, "%s entered the game\n", ent.Client.Pers.Netname)

	// make sure all view stuff is valid
	g.ClientEndServerFrame(ent)
}

// ClientBegin is called when a client has finished connecting, and is ready
// to be placed into the game.  This will happen every level load.
// C: game/p_client.c:1296 ClientBegin
func (g *Game) ClientBegin(ent *Edict) {
	ent.Client = &g.game.Clients[ent.Index-1]

	if g.deathmatch.Value != 0 {
		g.ClientBeginDeathmatch(ent)
		return
	}

	// if there is already a body waiting for us (a loadgame), just
	// take it, otherwise spawn one from scratch
	if ent.InUse {
		// the client has cleared the client side viewangles upon
		// connecting to the server, which is different than the
		// state when the game is saved, so we need to compensate
		// with deltaangles
		for i := 0; i < 3; i++ {
			ent.Client.PS.PMove.DeltaAngles[i] = int16(shared.ANGLE2SHORT(ent.Client.PS.ViewAngles[i]))
		}
	} else {
		// a spawn point will completely reinitialize the entity
		// except for the persistant data that was initialized at
		// ClientConnect() time
		g.G_InitEdict(ent)
		ent.Classname = "player"
		g.InitClientResp(ent.Client)
		g.PutClientInServer(ent)
	}

	if g.level.Intermissiontime != 0 {
		g.MoveClientToIntermission(ent)
	} else {
		// send effect if in a multiplayer game
		if g.game.Maxclients > 1 {
			g.gi.WriteByteC(svc_muzzleflash)
			g.gi.WriteShort(ent.Index)
			g.gi.WriteByteC(MZ_LOGIN)
			g.gi.Multicast(&ent.S.Origin, MULTICAST_PVS)

			g.bprintf(PRINT_HIGH, "%s entered the game\n", ent.Client.Pers.Netname)
		}
	}

	// make sure all view stuff is valid
	g.ClientEndServerFrame(ent)
}

// ClientUserinfoChanged is game_export_t.ClientUserinfoChanged.
func (g *Game) ClientUserinfoChanged(ent *Edict, userinfo string) {
	g.clientUserinfoChanged(ent, userinfo)
}

// clientUserinfoChanged is called whenever the player updates a userinfo
// variable. The game can override any of the settings in place (forcing
// skins or names, etc) before copying it off. Returns the (possibly
// modified) userinfo, as C edits the caller's buffer in place.
// C: game/p_client.c:1362 ClientUserinfoChanged
func (g *Game) clientUserinfoChanged(ent *Edict, userinfo string) string {
	// check for malformed or illegal info strings
	if !shared.Info_Validate(userinfo) {
		userinfo = "\\name\\badinfo\\skin\\male/grunt"
	}

	// set name
	s := shared.Info_ValueForKey(userinfo, "name")
	ent.Client.Pers.Netname = pclientStrncpy(s, 16)

	if !g.ctfmod { // the ctf fork has no spectator mode
		// set spectator
		s = shared.Info_ValueForKey(userinfo, "spectator")
		// spectators are only supported in deathmatch
		if g.deathmatch.Value != 0 && s != "" && s != "0" {
			ent.Client.Pers.Spectator = true
		} else {
			ent.Client.Pers.Spectator = false
		}
	}

	// set skin
	s = shared.Info_ValueForKey(userinfo, "skin")

	playernum := ent.Index - 1

	// combine name and skin into a configstring
	//ZOID
	if g.ctfOn() {
		g.CTFAssignSkin(ent, s)
	} else {
		//ZOID
		g.gi.Configstring(CS_PLAYERSKINS+playernum, fmt.Sprintf("%s\\%s", ent.Client.Pers.Netname, s))
	}

	// fov
	if g.deathmatch.Value != 0 && int32(g.dmflags.Value)&DF_FIXED_FOV != 0 {
		ent.Client.PS.Fov = 90
	} else {
		ent.Client.PS.Fov = float32(shared.Atoi(shared.Info_ValueForKey(userinfo, "fov")))
		if ent.Client.PS.Fov < 1 {
			ent.Client.PS.Fov = 90
		} else if ent.Client.PS.Fov > 160 {
			ent.Client.PS.Fov = 160
		}
	}

	// handedness
	s = shared.Info_ValueForKey(userinfo, "hand")
	if len(s) != 0 {
		ent.Client.Pers.Hand = shared.Atoi(s)
	}

	// save off the userinfo in case we want to check something later
	ent.Client.Pers.Userinfo = pclientStrncpy(userinfo, MAX_INFO_STRING)
	return userinfo
}

// pclientSetValueForKey is Info_SetValueForKey on the caller's userinfo,
// sending the C Com_Printf warning through gi.dprintf.
func (g *Game) pclientSetValueForKey(s, key, value string) string {
	r, warn := shared.Info_SetValueForKey(s, key, value)
	if warn != "" {
		g.gi.Dprintf(warn)
	}
	return r
}

// ClientConnect is called when a player begins connecting to the server.
// The game can refuse entrance to a client by returning false.
// If the client is allowed, the connection process will continue
// and eventually get to ClientBegin()
// Changing levels will NOT cause this to be called again, but
// loadgames will.
// C: game/p_client.c:1431 ClientConnect
func (g *Game) ClientConnect(ent *Edict, userinfo string) (bool, string) {
	if g.ctfmod {
		return g.ctfClientConnect(ent, userinfo)
	}
	// check to see if they are on the banned IP list
	value := shared.Info_ValueForKey(userinfo, "ip")
	if g.SV_FilterPacket(value) {
		userinfo = g.pclientSetValueForKey(userinfo, "rejmsg", "Banned.")
		return false, userinfo
	}

	// check for a spectator
	value = shared.Info_ValueForKey(userinfo, "spectator")
	if g.deathmatch.Value != 0 && value != "" && value != "0" {
		if g.spectator_password.String != "" &&
			g.spectator_password.String != "none" &&
			g.spectator_password.String != value {
			userinfo = g.pclientSetValueForKey(userinfo, "rejmsg", "Spectator password required or incorrect.")
			return false, userinfo
		}

		// count spectators
		numspec := 0
		for i := 0; float32(i) < g.maxclients.Value; i++ {
			if g.edicts[i+1].InUse && g.edicts[i+1].Client.Pers.Spectator {
				numspec++
			}
		}

		if float32(numspec) >= g.maxspectators.Value {
			userinfo = g.pclientSetValueForKey(userinfo, "rejmsg", "Server spectator limit is full.")
			return false, userinfo
		}
	} else {
		// check for a password
		value = shared.Info_ValueForKey(userinfo, "password")
		if g.password.String != "" && g.password.String != "none" &&
			g.password.String != value {
			userinfo = g.pclientSetValueForKey(userinfo, "rejmsg", "Password required or incorrect.")
			return false, userinfo
		}
	}

	// they can connect
	ent.Client = &g.game.Clients[ent.Index-1]

	// if there is already a body waiting for us (a loadgame), just
	// take it, otherwise spawn one from scratch
	if !ent.InUse {
		// clear the respawning variables
		g.InitClientResp(ent.Client)
		if !g.game.Autosaved || ent.Client.Pers.Weapon == nil {
			g.InitClientPersistant(ent.Client)
		}
	}

	userinfo = g.clientUserinfoChanged(ent, userinfo)

	if g.game.Maxclients > 1 {
		g.dprintf("%s connected\n", ent.Client.Pers.Netname)
	}

	ent.Client.Pers.Connected = true
	return true, userinfo
}

// ctfClientConnect is the ctf fork's ClientConnect: no IP filter, no
// spectators, force team join.
// C: ctf/p_client.c:1369 ClientConnect
func (g *Game) ctfClientConnect(ent *Edict, userinfo string) (bool, string) {
	// check to see if they are on the banned IP list
	_ = shared.Info_ValueForKey(userinfo, "ip")

	// check for a password
	value := shared.Info_ValueForKey(userinfo, "password")
	if g.password.String != "" && g.password.String != "none" &&
		g.password.String != value {
		userinfo = g.pclientSetValueForKey(userinfo, "rejmsg", "Password required or incorrect.")
		return false, userinfo
	}

	// they can connect
	ent.Client = &g.game.Clients[ent.Index-1]

	// if there is already a body waiting for us (a loadgame), just
	// take it, otherwise spawn one from scratch
	if !ent.InUse {
		// clear the respawning variables
		//ZOID -- force team join
		ent.Client.Resp.CtfTeam = -1
		ent.Client.Resp.IdState = false
		//ZOID
		g.InitClientResp(ent.Client)
		if !g.game.Autosaved || ent.Client.Pers.Weapon == nil {
			g.InitClientPersistant(ent.Client)
		}
	}

	userinfo = g.clientUserinfoChanged(ent, userinfo)

	if g.game.Maxclients > 1 {
		g.dprintf("%s connected\n", ent.Client.Pers.Netname)
	}

	ent.Client.Pers.Connected = true
	return true, userinfo
}

// ClientDisconnect is called when a player drops from the server.
// Will not be called between levels.
// C: game/p_client.c:1504 ClientDisconnect
func (g *Game) ClientDisconnect(ent *Edict) {
	if ent.Client == nil {
		return
	}

	g.bprintf(PRINT_HIGH, "%s disconnected\n", ent.Client.Pers.Netname)

	//ZOID
	if g.ctfmod {
		g.CTFDeadDropFlag(ent)
		g.CTFDeadDropTech(ent)
	}
	//ZOID

	// send effect
	g.gi.WriteByteC(svc_muzzleflash)
	g.gi.WriteShort(ent.Index)
	g.gi.WriteByteC(MZ_LOGOUT)
	g.gi.Multicast(&ent.S.Origin, MULTICAST_PVS)

	g.gi.UnlinkEntity(ent)
	ent.S.ModelIndex = 0
	ent.Solid = SOLID_NOT
	ent.InUse = false
	ent.Classname = "disconnected"
	ent.Client.Pers.Connected = false

	playernum := ent.Index - 1
	g.gi.Configstring(CS_PLAYERSKINS+playernum, "")
}

//==============================================================

// PM_trace: pmove doesn't need to know about passent and contentmask.
// Entity ids in the returned trace are edict indices (-1 = NULL).
// C: game/p_client.c:1537 PM_trace
func (g *Game) PM_trace(start, mins, maxs, end *Vec3) shared.Trace {
	var tr Trace
	if g.pm_passent.Health > 0 {
		tr = g.gi.Trace(start, mins, maxs, end, g.pm_passent, MASK_PLAYERSOLID)
	} else {
		tr = g.gi.Trace(start, mins, maxs, end, g.pm_passent, MASK_DEADSOLID)
	}
	st := shared.Trace{
		AllSolid:   tr.AllSolid,
		StartSolid: tr.StartSolid,
		Fraction:   tr.Fraction,
		EndPos:     tr.EndPos,
		Plane:      tr.Plane,
		Surface:    tr.Surface,
		Contents:   tr.Contents,
		Ent:        pmove.NoEnt,
	}
	if tr.Ent != nil {
		st.Ent = tr.Ent.Index
	}
	return st
}

// CheckBlock and PrintPmove (debug helpers, unused) are not ported.

// pclientEdict maps a pmove entity id to an edict (NULL for pmove.NoEnt).
func (g *Game) pclientEdict(id int) *Edict {
	if id < 0 {
		return nil
	}
	return &g.edicts[id]
}

// ClientThink will be called once for each client frame, which will
// usually be a couple times for each server frame.
// C: game/p_client.c:1570 ClientThink
func (g *Game) ClientThink(ent *Edict, ucmd *shared.UserCmd) {
	var other *Edict

	g.level.CurrentEntity = ent
	client := ent.Client

	if g.level.Intermissiontime != 0 {
		client.PS.PMove.PmType = PM_FREEZE
		// can exit intermission after five seconds
		if float64(g.level.Time) > float64(g.level.Intermissiontime)+5.0 && int32(ucmd.Buttons)&BUTTON_ANY != 0 {
			g.level.Exitintermission = 1
		}
		return
	}

	g.pm_passent = ent

	if ent.Client.ChaseTarget != nil {
		client.Resp.CmdAngles[0] = float32(shared.SHORT2ANGLE(int32(ucmd.Angles[0])))
		client.Resp.CmdAngles[1] = float32(shared.SHORT2ANGLE(int32(ucmd.Angles[1])))
		client.Resp.CmdAngles[2] = float32(shared.SHORT2ANGLE(int32(ucmd.Angles[2])))
		//ZOID
		if g.ctfmod {
			return
		}
		//ZOID
	} else {
		// set up for pmove
		var pm pmove.PmoveT

		if ent.Movetype == MOVETYPE_NOCLIP {
			client.PS.PMove.PmType = PM_SPECTATOR
		} else if ent.S.ModelIndex != 255 {
			client.PS.PMove.PmType = PM_GIB
		} else if ent.Deadflag != 0 {
			client.PS.PMove.PmType = PM_DEAD
		} else {
			client.PS.PMove.PmType = PM_NORMAL
		}

		client.PS.PMove.Gravity = int16(int32(g.sv_gravity.Value))
		pm.S = client.PS.PMove

		for i := 0; i < 3; i++ {
			pm.S.Origin[i] = int16(int32(ent.S.Origin[i] * 8))
			pm.S.Velocity[i] = int16(int32(ent.Velocity[i] * 8))
		}

		if client.OldPmove != pm.S {
			pm.SnapInitial = true
			//		gi.dprintf ("pmove changed!\n");
		}

		pm.Cmd = *ucmd

		pm.Trace = g.PM_trace // adds default parms
		pm.PointContents = func(point Vec3) int32 { return g.gi.PointContents(&point) }

		// perform a pmove
		g.gi.Pmove(&pm)

		// save results of pmove
		client.PS.PMove = pm.S
		client.OldPmove = pm.S

		for i := 0; i < 3; i++ {
			ent.S.Origin[i] = float32(float64(pm.S.Origin[i]) * 0.125)
			ent.Velocity[i] = float32(float64(pm.S.Velocity[i]) * 0.125)
		}

		ent.Mins = pm.Mins
		ent.Maxs = pm.Maxs

		client.Resp.CmdAngles[0] = float32(shared.SHORT2ANGLE(int32(ucmd.Angles[0])))
		client.Resp.CmdAngles[1] = float32(shared.SHORT2ANGLE(int32(ucmd.Angles[1])))
		client.Resp.CmdAngles[2] = float32(shared.SHORT2ANGLE(int32(ucmd.Angles[2])))

		pmGround := g.pclientEdict(pm.GroundEntity)
		if ent.Groundentity != nil && pmGround == nil && pm.Cmd.UpMove >= 10 && pm.WaterLevel == 0 {
			g.gi.Sound(ent, CHAN_VOICE, g.gi.SoundIndex("*jump1.wav"), 1, ATTN_NORM, 0)
			g.PlayerNoise(ent, ent.S.Origin, PNOISE_SELF)
		}

		ent.Viewheight = int32(pm.ViewHeight)
		ent.Waterlevel = pm.WaterLevel
		ent.Watertype = pm.WaterType
		ent.Groundentity = pmGround
		if pmGround != nil {
			ent.GroundentityLinkcount = pmGround.LinkCount
		}

		if ent.Deadflag != 0 {
			client.PS.ViewAngles[ROLL] = 40
			client.PS.ViewAngles[PITCH] = -15
			client.PS.ViewAngles[YAW] = client.KillerYaw
		} else {
			client.VAngle = pm.ViewAngles
			client.PS.ViewAngles = pm.ViewAngles
		}

		//ZOID
		if g.ctfmod && client.CtfGrapple != nil {
			g.CTFGrapplePull(client.CtfGrapple)
		}
		//ZOID

		g.gi.LinkEntity(ent)

		if ent.Movetype != MOVETYPE_NOCLIP {
			g.G_TouchTriggers(ent)
		}

		// touch other objects
		for i := 0; i < pm.NumTouch; i++ {
			other = g.pclientEdict(pm.TouchEnts[i])
			j := 0
			for j = 0; j < i; j++ {
				if pm.TouchEnts[j] == pm.TouchEnts[i] {
					break
				}
			}
			if j != i {
				continue // duplicated
			}
			if other.Touch == nil {
				continue
			}
			other.Touch.fn(g, other, ent, nil, nil)
		}
	}

	client.Oldbuttons = client.Buttons
	client.Buttons = int32(ucmd.Buttons)
	client.LatchedButtons |= client.Buttons &^ client.Oldbuttons

	// save light level the player is standing on for
	// monster sighting AI
	ent.LightLevel = int32(ucmd.LightLevel)

	if g.ctfmod {
		g.ctfClientThinkTail(ent)
		return
	}

	// fire weapon from final position if needed
	if client.LatchedButtons&BUTTON_ATTACK != 0 {
		if client.Resp.Spectator {
			client.LatchedButtons = 0

			if client.ChaseTarget != nil {
				client.ChaseTarget = nil
				client.PS.PMove.PmFlags &^= PMF_NO_PREDICTION
			} else {
				g.GetChaseTarget(ent)
			}
		} else if !client.WeaponThunk {
			client.WeaponThunk = true
			g.Think_Weapon(ent)
		}
	}

	if client.Resp.Spectator {
		if ucmd.UpMove >= 10 {
			if client.PS.PMove.PmFlags&PMF_JUMP_HELD == 0 {
				client.PS.PMove.PmFlags |= PMF_JUMP_HELD
				if client.ChaseTarget != nil {
					g.ChaseNext(ent)
				} else {
					g.GetChaseTarget(ent)
				}
			}
		} else {
			client.PS.PMove.PmFlags &^= PMF_JUMP_HELD
		}
	}

	// update chase cam if being followed
	for i := 1; float32(i) <= g.maxclients.Value; i++ {
		other = &g.edicts[i]
		if other.InUse && other.Client.ChaseTarget == ent {
			g.UpdateChaseCam(other)
		}
	}
}

// ctfClientThinkTail is the end of the ctf fork's ClientThink.
// C: ctf/p_client.c:1634
func (g *Game) ctfClientThinkTail(ent *Edict) {
	client := ent.Client

	// fire weapon from final position if needed
	if client.LatchedButtons&BUTTON_ATTACK != 0 &&
		//ZOID
		ent.Movetype != MOVETYPE_NOCLIP {
		//ZOID
		if !client.WeaponThunk {
			client.WeaponThunk = true
			g.Think_Weapon(ent)
		}
	}

	//ZOID
	//regen tech
	g.CTFApplyRegeneration(ent)
	//ZOID

	//ZOID
	for i := 1; float32(i) <= g.maxclients.Value; i++ {
		other := &g.edicts[i]
		if other.InUse && other.Client.ChaseTarget == ent {
			g.UpdateChaseCam(other)
		}
	}

	if client.Menudirty && client.Menutime <= g.level.Time {
		g.PMenu_Do_Update(ent)
		g.gi.Unicast(ent, true)
		client.Menutime = g.level.Time
		client.Menudirty = false
	}
	//ZOID
}

// ClientBeginServerFrame will be called once for each server frame, before
// running any other entities in the world.
// C: game/p_client.c:1755 ClientBeginServerFrame
func (g *Game) ClientBeginServerFrame(ent *Edict) {
	var buttonMask int32

	if g.level.Intermissiontime != 0 {
		return
	}

	client := ent.Client

	if !g.ctfmod && g.deathmatch.Value != 0 &&
		client.Pers.Spectator != client.Resp.Spectator &&
		(g.level.Time-client.RespawnTime) >= 5 {
		g.spectator_respawn(ent)
		return
	}

	// run weapon animations if it hasn't been done by a ucmd_t
	if g.ctfmod {
		if !client.WeaponThunk &&
			//ZOID
			ent.Movetype != MOVETYPE_NOCLIP {
			//ZOID
			g.Think_Weapon(ent)
		} else {
			client.WeaponThunk = false
		}
	} else if !client.WeaponThunk && !client.Resp.Spectator {
		g.Think_Weapon(ent)
	} else {
		client.WeaponThunk = false
	}

	if ent.Deadflag != 0 {
		// wait for any button just going down
		if g.level.Time > client.RespawnTime {
			// in deathmatch, only wait for attack button
			if g.deathmatch.Value != 0 {
				buttonMask = BUTTON_ATTACK
			} else {
				buttonMask = -1
			}

			if client.LatchedButtons&buttonMask != 0 ||
				(g.deathmatch.Value != 0 && int32(g.dmflags.Value)&DF_FORCE_RESPAWN != 0) ||
				g.CTFMatchOn() {
				g.respawn(ent)
				client.LatchedButtons = 0
			}
		}
		return
	}

	// add player trail so monsters can follow
	if g.deathmatch.Value == 0 {
		if !g.visible(ent, g.PlayerTrail_LastSpot()) {
			g.PlayerTrail_Add(ent.S.OldOrigin)
		}
	}

	client.LatchedButtons = 0
}
