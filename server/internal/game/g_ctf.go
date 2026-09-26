package game

// Port of ctf/g_ctf.c and ctf/g_ctf.h: ThreeWave Capture the Flag (the ctf
// game module), run as a mode of this package (Game.ctfmod, docs/CTF.md).
//
// The C globals of g_ctf.c (ctfgame, the CTF cvars, flag1_item/flag2_item,
// the mutable menu tables) are fields of Game.ctfg. The function-level
// "static gitem_t *tech" caches of the tech functions are not kept: they
// only cache FindItemByClassname results of the immutable item table.

import (
	"fmt"
	"math"
	"strings"

	. "quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/cvar"
	"quake2web/server/internal/qcommon/shared"
)

// C: ctf/g_ctf.h:21
const CTF_STRING_VERSION = "1.09b"

// C: ctf/g_ctf.h:26
const (
	STAT_CTF_TEAM1_PIC        = 17
	STAT_CTF_TEAM1_CAPS       = 18
	STAT_CTF_TEAM2_PIC        = 19
	STAT_CTF_TEAM2_CAPS       = 20
	STAT_CTF_FLAG_PIC         = 21
	STAT_CTF_JOINED_TEAM1_PIC = 22
	STAT_CTF_JOINED_TEAM2_PIC = 23
	STAT_CTF_TEAM1_HEADER     = 24
	STAT_CTF_TEAM2_HEADER     = 25
	STAT_CTF_TECH             = 26
	STAT_CTF_ID_VIEW          = 27
	STAT_CTF_MATCH            = 28

	CONFIG_CTF_MATCH = CS_MAXCLIENTS - 1
)

// ctfteam_t
// C: ctf/g_ctf.h:41
const (
	CTF_NOTEAM = 0
	CTF_TEAM1  = 1
	CTF_TEAM2  = 2
)

// ctfgrapplestate_t
// C: ctf/g_ctf.h:47
const (
	CTF_GRAPPLE_STATE_FLY  = 0
	CTF_GRAPPLE_STATE_PULL = 1
	CTF_GRAPPLE_STATE_HANG = 2
)

// SVF_PROJECTILE: entity is simple projectile, used for network optimization.
// C: ctf/game.h:31
const SVF_PROJECTILE = 0x00000008

// C: ctf/g_ctf.h:73
const (
	CTF_TEAM1_SKIN = "ctf_r"
	CTF_TEAM2_SKIN = "ctf_b"

	DF_CTF_FORCEJOIN = 131072
	DF_ARMOR_PROTECT = 262144
	DF_CTF_NO_TECH   = 524288

	CTF_CAPTURE_BONUS      = 15 // what you get for capture
	CTF_TEAM_BONUS         = 10 // what your team gets for capture
	CTF_RECOVERY_BONUS     = 1  // what you get for recovery
	CTF_FLAG_BONUS         = 0  // what you get for picking up enemy flag
	CTF_FRAG_CARRIER_BONUS = 2  // what you get for fragging enemy flag carrier
	CTF_FLAG_RETURN_TIME   = 40 // seconds until auto return

	CTF_CARRIER_DANGER_PROTECT_BONUS = 2 // bonus for fraggin someone who has recently hurt your flag carrier
	CTF_CARRIER_PROTECT_BONUS        = 1 // bonus for fraggin someone while either you or your target are near your flag carrier
	CTF_FLAG_DEFENSE_BONUS           = 1 // bonus for fraggin someone while either you or your target are near your flag
	CTF_RETURN_FLAG_ASSIST_BONUS     = 1 // awarded for returning a flag that causes a capture to happen almost immediately
	CTF_FRAG_CARRIER_ASSIST_BONUS    = 2 // award for fragging a flag carrier if a capture happens almost immediately

	CTF_TARGET_PROTECT_RADIUS   = 400 // the radius around an object being defended where a target will be worth extra frags
	CTF_ATTACKER_PROTECT_RADIUS = 400 // the radius around an object being defended where an attacker will get extra frags when making kills

	CTF_CARRIER_DANGER_PROTECT_TIMEOUT = 8
	CTF_FRAG_CARRIER_ASSIST_TIMEOUT    = 10
	CTF_RETURN_FLAG_ASSIST_TIMEOUT     = 10

	CTF_AUTO_FLAG_RETURN_TIMEOUT = 30 // number of seconds before dropped flag auto-returns

	CTF_TECH_TIMEOUT = 60 // seconds before techs spawn again

	CTF_GRAPPLE_SPEED      = 650 // speed of grapple in flight
	CTF_GRAPPLE_PULL_SPEED = 650 // speed player is pulled at
)

// match_t
// C: ctf/g_ctf.c:23
const (
	MATCH_NONE    = 0
	MATCH_SETUP   = 1
	MATCH_PREGAME = 2
	MATCH_GAME    = 3
	MATCH_POST    = 4
)

// elect_t
// C: ctf/g_ctf.c:31
const (
	ELECT_NONE  = 0
	ELECT_MATCH = 1
	ELECT_ADMIN = 2
	ELECT_MAP   = 3
)

// Ghost is C ghost_t.
// C: ctf/g_ctf.h:53 ghost_s
type Ghost struct {
	Netname string // char[16]
	Number  int32

	// stats
	Deaths     int32
	Kills      int32
	Caps       int32
	Basedef    int32
	Carrierdef int32

	Code  int32 // ghost code
	Team  int32 // team
	Score int32 // frags at time of disconnect
	Ent   *Edict
}

// ctfgame_t
// C: ctf/g_ctf.c:38 ctfgame_s
type ctfgame_t struct {
	team1, team2      int32
	total1, total2    int32 // these are only set when going into intermission!
	last_flag_capture float32
	last_capture_team int32

	match     int32   // match state
	matchtime float32 // time for match start/end (depends on state)
	lasttime  int32   // last time update

	election  int32   // election type
	etarget   *Edict  // for admin election, who's being elected
	elevel    string  // for map election, target level (char[32])
	evotes    int32   // votes so far
	needvotes int32   // votes needed
	electtime float32 // remaining time until election times out
	emsg      string  // election name (char[256])

	ghosts [MAX_CLIENTS]Ghost // ghost codes
}

// ctfGlobals holds the globals of ctf/g_ctf.c.
type ctfGlobals struct {
	ctfgame ctfgame_t

	ctf           *cvar.Cvar
	ctf_forcejoin *cvar.Cvar

	competition     *cvar.Cvar
	matchlock       *cvar.Cvar
	electpercentage *cvar.Cvar
	matchtime       *cvar.Cvar
	matchsetuptime  *cvar.Cvar
	matchstarttime  *cvar.Cvar
	admin_password  *cvar.Cvar
	warp_list       *cvar.Cvar

	flag1_item *GItem // static gitem_t *flag1_item (ctf/g_ctf.c:293)
	flag2_item *GItem

	// the menu tables are global and modified in place by the C code
	joinmenu    []PMenu
	nochasemenu []PMenu
	adminmenu   []PMenu
}

// ctfOn is the C test `ctf->value` (only true in the ctf module).
func (g *Game) ctfOn() bool { return g.ctfmod && g.ctfg.ctf.Value != 0 }

// C: ctf/g_ctf.c:75 ctf_statusbar
const ctf_statusbar = "yb\t-24 xv\t0 hnum xv\t50 pic 0 if 2 \txv\t100 \tanum \txv\t150 \tpic 2 endif if 4 \txv\t200 \trnum \txv\t250 \tpic 4 endif if 6 \txv\t296 \tpic 6 endif yb\t-50 if 7 \txv\t0 \tpic 7 \txv\t26 \tyb\t-42 \tstat_string 8 \tyb\t-50 endif if 9 xv 246 num 2 10 xv 296 pic 9 endif if 11 xv 148 pic 11 endif xr\t-50 yt 2 num 3 14 yb -129 if 26 xr -26 pic 26 endif yb -102 if 17 xr -26 pic 17 endif xr -62 num 2 18 if 22 yb -104 xr -28 pic 22 endif yb -75 if 19 xr -26 pic 19 endif xr -62 num 2 20 if 23 yb -77 xr -28 pic 23 endif if 21 yt 26 xr -24 pic 21 endif if 27 xv 0 yb -58 string \"Viewing\" xv 64 stat_string 27 endif if 28 xl 0 yb -78 stat_string 28 endif "

// ctf_dm_statusbar is the dm_statusbar of the ctf fork (no spectator/chase
// blocks), used by the ctf module when the ctf cvar is 0.
// C: ctf/g_spawn.c:706 dm_statusbar
const ctf_dm_statusbar = "yb\t-24 xv\t0 hnum xv\t50 pic 0 if 2 \txv\t100 \tanum \txv\t150 \tpic 2 endif if 4 \txv\t200 \trnum \txv\t250 \tpic 4 endif if 6 \txv\t296 \tpic 6 endif yb\t-50 if 7 \txv\t0 \tpic 7 \txv\t26 \tyb\t-42 \tstat_string 8 \tyb\t-50 endif if 9 \txv\t246 \tnum\t2\t10 \txv\t296 \tpic\t9 endif if 11 \txv\t148 \tpic\t11 endif xr\t-50 yt 2 num 3 14"

// C: ctf/g_ctf.c:196 tnames
var tnames = []string{"item_tech1", "item_tech2", "item_tech3", "item_tech4"}

// Function pointer handles of g_ctf.c.
var (
	CTFPickup_Flag         = defPickup("CTFPickup_Flag")
	CTFDrop_Flag           = defItem("CTFDrop_Flag")
	CTFDropFlagTouch       = defTouch("CTFDropFlagTouch")
	CTFDropFlagThink       = defThink("CTFDropFlagThink")
	CTFFlagThink           = defThink("CTFFlagThink")
	CTFFlagSetup           = defThink("CTFFlagSetup")
	CTFGrappleTouch        = defTouch("CTFGrappleTouch")
	CTFWeapon_Grapple      = defThink("CTFWeapon_Grapple")
	CTFPickup_Tech         = defPickup("CTFPickup_Tech")
	CTFDrop_Tech           = defItem("CTFDrop_Tech")
	TechThink              = defThink("TechThink")
	SpawnTechs             = defThink("SpawnTechs")
	misc_ctf_banner_think  = defThink("misc_ctf_banner_think")
	old_teleporter_touch   = defTouch("old_teleporter_touch")
	CTFWeapon_Grapple_Fire = defThink("CTFWeapon_Grapple_Fire")
)

func init() {
	CTFPickup_Flag.bind((*Game).CTFPickup_Flag)
	CTFDrop_Flag.bind((*Game).CTFDrop_Flag)
	CTFDropFlagTouch.bind((*Game).CTFDropFlagTouch)
	CTFDropFlagThink.bind((*Game).CTFDropFlagThink)
	CTFFlagThink.bind((*Game).CTFFlagThink)
	CTFFlagSetup.bind((*Game).CTFFlagSetup)
	CTFGrappleTouch.bind((*Game).CTFGrappleTouch)
	CTFWeapon_Grapple.bind((*Game).CTFWeapon_Grapple)
	CTFPickup_Tech.bind((*Game).CTFPickup_Tech)
	CTFDrop_Tech.bind((*Game).CTFDrop_Tech)
	TechThink.bind((*Game).TechThink)
	SpawnTechs.bind((*Game).SpawnTechs)
	misc_ctf_banner_think.bind((*Game).misc_ctf_banner_think)
	old_teleporter_touch.bind((*Game).old_teleporter_touch)
	CTFWeapon_Grapple_Fire.bind((*Game).CTFWeapon_Grapple_Fire)
}

// ctfItemlist is the itemlist[] of the ctf module: the 3.19 table with
// weapon_grapple inserted before weapon_blaster and the flags and techs
// appended (exact order of ctf/g_items.c, so item indices match the ctf
// module).
// C: ctf/g_items.c:1195 itemlist
var ctfItemlist []GItem

// ctfSpawns is the spawns[] table of the ctf module.
// C: ctf/g_spawn.c:146 spawns
var ctfSpawns []spawn_t

// buildCTFTables fills ctfItemlist and ctfSpawns. It is called from the
// init() of g_spawn.go, after itemlist (g_items.go) and spawns exist.
func buildCTFTables() {
	grapple := GItem{Classname: "weapon_grapple", Use: Use_Weapon, Weaponthink: CTFWeapon_Grapple, PickupSound: "misc/w_pkup.wav", ViewModel: "models/weapons/grapple/tris.md2", Icon: "w_grapple", PickupName: "Grapple", Flags: IT_WEAPON, Weapmodel: WEAP_GRAPPLE, Precaches: "weapons/grapple/grfire.wav weapons/grapple/grpull.wav weapons/grapple/grhang.wav weapons/grapple/grreset.wav weapons/grapple/grhit.wav"}
	tail := []GItem{
		{Classname: "item_flag_team1", Pickup: CTFPickup_Flag, Drop: CTFDrop_Flag, PickupSound: "ctf/flagtk.wav", WorldModel: "players/male/flag1.md2", WorldModelFlags: EF_FLAG1, Icon: "i_ctf1", PickupName: "Red Flag", CountWidth: 2, Precaches: "ctf/flagcap.wav"},
		{Classname: "item_flag_team2", Pickup: CTFPickup_Flag, Drop: CTFDrop_Flag, PickupSound: "ctf/flagtk.wav", WorldModel: "players/male/flag2.md2", WorldModelFlags: EF_FLAG2, Icon: "i_ctf2", PickupName: "Blue Flag", CountWidth: 2, Precaches: "ctf/flagcap.wav"},
		{Classname: "item_tech1", Pickup: CTFPickup_Tech, Drop: CTFDrop_Tech, PickupSound: "items/pkup.wav", WorldModel: "models/ctf/resistance/tris.md2", WorldModelFlags: EF_ROTATE, Icon: "tech1", PickupName: "Disruptor Shield", CountWidth: 2, Flags: IT_TECH, Precaches: "ctf/tech1.wav"},
		{Classname: "item_tech2", Pickup: CTFPickup_Tech, Drop: CTFDrop_Tech, PickupSound: "items/pkup.wav", WorldModel: "models/ctf/strength/tris.md2", WorldModelFlags: EF_ROTATE, Icon: "tech2", PickupName: "Power Amplifier", CountWidth: 2, Flags: IT_TECH, Precaches: "ctf/tech2.wav ctf/tech2x.wav"},
		{Classname: "item_tech3", Pickup: CTFPickup_Tech, Drop: CTFDrop_Tech, PickupSound: "items/pkup.wav", WorldModel: "models/ctf/haste/tris.md2", WorldModelFlags: EF_ROTATE, Icon: "tech3", PickupName: "Time Accel", CountWidth: 2, Flags: IT_TECH, Precaches: "ctf/tech3.wav"},
		{Classname: "item_tech4", Pickup: CTFPickup_Tech, Drop: CTFDrop_Tech, PickupSound: "items/pkup.wav", WorldModel: "models/ctf/regeneration/tris.md2", WorldModelFlags: EF_ROTATE, Icon: "tech4", PickupName: "AutoDoc", CountWidth: 2, Flags: IT_TECH, Precaches: "ctf/tech4.wav"},
	}
	base := itemlist[:len(itemlist)-1] // without the end of list marker
	for _, it := range base {
		if it.Classname == "weapon_blaster" {
			ctfItemlist = append(ctfItemlist, grapple)
		}
		ctfItemlist = append(ctfItemlist, it)
	}
	ctfItemlist = append(ctfItemlist, tail...)
	ctfItemlist = append(ctfItemlist, GItem{}) // end of list marker
	for i := range ctfItemlist {
		ctfItemlist[i].index = int32(i)
	}
	if len(ctfItemlist) != len(itemlist)+7 {
		panic("game: ctf itemlist")
	}

	// ctf/g_spawn.c: the monster code is "#if 0 // remove monster code"
	removed := map[string]bool{
		"monster_commander_body": true, "turret_breach": true, "turret_base": true, "turret_driver": true,
	}
	for _, s := range spawns {
		if removed[s.name] {
			continue
		}
		ctfSpawns = append(ctfSpawns, s)
		switch s.name {
		case "info_player_intermission":
			ctfSpawns = append(ctfSpawns,
				spawn_t{"info_player_team1", (*Game).SP_info_player_team1},
				spawn_t{"info_player_team2", (*Game).SP_info_player_team2})
		case "misc_banner":
			ctfSpawns = append(ctfSpawns,
				spawn_t{"misc_ctf_banner", (*Game).SP_misc_ctf_banner},
				spawn_t{"misc_ctf_small_banner", (*Game).SP_misc_ctf_small_banner})
		case "misc_teleporter_dest":
			ctfSpawns = append(ctfSpawns,
				spawn_t{"trigger_teleport", (*Game).SP_trigger_teleport},
				spawn_t{"info_teleport_destination", (*Game).SP_info_teleport_destination})
		}
	}
}

// C: ctf/g_ctf.c:201 stuffcmd
func (g *Game) stuffcmd(ent *Edict, s string) {
	g.gi.WriteByteC(11)
	g.gi.WriteString(s)
	g.gi.Unicast(ent, true)
}

/*--------------------------------------------------------------------------*/

// loc_findradius returns entities that have origins within a spherical area.
// C: ctf/g_ctf.c:219 loc_findradius
func (g *Game) loc_findradius(from *Edict, org Vec3, rad float32) *Edict {
	var eorg Vec3
	i := 0
	if from != nil {
		i = from.Index + 1
	}
	for ; i < int(g.num_edicts); i++ {
		from = &g.edicts[i]
		if !from.InUse {
			continue
		}
		for j := 0; j < 3; j++ {
			eorg[j] = float32(float64(org[j]) - (float64(from.S.Origin[j]) + float64(from.Mins[j]+from.Maxs[j])*0.5))
		}
		if shared.VectorLength(eorg) > rad {
			continue
		}
		return from
	}

	return nil
}

// C: ctf/g_ctf.c:246 loc_buildboxpoints
func loc_buildboxpoints(p *[8]Vec3, org, mins, maxs Vec3) {
	p[0] = shared.VectorAdd(org, mins)
	p[1] = p[0]
	p[1][0] -= mins[0]
	p[2] = p[0]
	p[2][1] -= mins[1]
	p[3] = p[0]
	p[3][0] -= mins[0]
	p[3][1] -= mins[1]
	p[4] = shared.VectorAdd(org, maxs)
	p[5] = p[4]
	p[5][0] -= maxs[0]
	p[6] = p[0]
	p[6][1] -= maxs[1]
	p[7] = p[0]
	p[7][0] -= maxs[0]
	p[7][1] -= maxs[1]
}

// C: ctf/g_ctf.c:266 loc_CanSee
func (g *Game) loc_CanSee(targ, inflictor *Edict) bool {
	var targpoints [8]Vec3

	// bmodels need special checking because their origin is 0,0,0
	if targ.Movetype == MOVETYPE_PUSH {
		return false // bmodels not supported
	}

	loc_buildboxpoints(&targpoints, targ.S.Origin, targ.Mins, targ.Maxs)

	viewpoint := inflictor.S.Origin
	viewpoint[2] += float32(inflictor.Viewheight)

	for i := 0; i < 8; i++ {
		trace := g.gi.Trace(&viewpoint, &shared.Vec3Origin, &shared.Vec3Origin, &targpoints[i], inflictor, MASK_SOLID)
		if trace.Fraction == 1.0 {
			return true
		}
	}

	return false
}

/*--------------------------------------------------------------------------*/

// C: ctf/g_ctf.c:296 CTFSpawn
func (g *Game) CTFSpawn() {
	if g.ctfg.flag1_item == nil {
		g.ctfg.flag1_item = g.FindItemByClassname("item_flag_team1")
	}
	if g.ctfg.flag2_item == nil {
		g.ctfg.flag2_item = g.FindItemByClassname("item_flag_team2")
	}
	g.ctfg.ctfgame = ctfgame_t{}
	g.CTFSetupTechSpawn()

	if g.ctfg.competition.Value > 1 {
		g.ctfg.ctfgame.match = MATCH_SETUP
		g.ctfg.ctfgame.matchtime = g.level.Time + g.ctfg.matchsetuptime.Value*60
	}
}

// C: ctf/g_ctf.c:311 CTFInit
func (g *Game) CTFInit() {
	c := &g.ctfg
	c.ctf = g.gi.Cvar("ctf", "1", CVAR_SERVERINFO)
	c.ctf_forcejoin = g.gi.Cvar("ctf_forcejoin", "", 0)
	c.competition = g.gi.Cvar("competition", "0", CVAR_SERVERINFO)
	c.matchlock = g.gi.Cvar("matchlock", "1", CVAR_SERVERINFO)
	c.electpercentage = g.gi.Cvar("electpercentage", "66", 0)
	c.matchtime = g.gi.Cvar("matchtime", "20", CVAR_SERVERINFO)
	c.matchsetuptime = g.gi.Cvar("matchsetuptime", "10", 0)
	c.matchstarttime = g.gi.Cvar("matchstarttime", "20", 0)
	c.admin_password = g.gi.Cvar("admin_password", "", 0)
	c.warp_list = g.gi.Cvar("warp_list", "q2ctf1 q2ctf2 q2ctf3 q2ctf4 q2ctf5", 0)

	// the global menu tables (modified in place by the menu code)
	c.joinmenu = append([]PMenu(nil), joinmenuInit...)
	c.nochasemenu = append([]PMenu(nil), nochasemenuInit...)
	c.adminmenu = append([]PMenu(nil), adminmenuInit...)
}

/*--------------------------------------------------------------------------*/

// C: ctf/g_ctf.c:327 CTFTeamName
func CTFTeamName(team int32) string {
	switch team {
	case CTF_TEAM1:
		return "RED"
	case CTF_TEAM2:
		return "BLUE"
	}
	return "UKNOWN"
}

// C: ctf/g_ctf.c:338 CTFOtherTeamName
func CTFOtherTeamName(team int32) string {
	switch team {
	case CTF_TEAM1:
		return "BLUE"
	case CTF_TEAM2:
		return "RED"
	}
	return "UKNOWN"
}

// C: ctf/g_ctf.c:349 CTFOtherTeam
func CTFOtherTeam(team int32) int32 {
	switch team {
	case CTF_TEAM1:
		return CTF_TEAM2
	case CTF_TEAM2:
		return CTF_TEAM1
	}
	return -1 // invalid value
}

/*--------------------------------------------------------------------------*/

// C: ctf/g_ctf.c:366 CTFAssignSkin
func (g *Game) CTFAssignSkin(ent *Edict, s string) {
	playernum := ent.Index - 1

	t := s
	if len(t) > 63 { // Com_sprintf(t, sizeof(t), "%s", s)
		t = t[:63]
	}

	if p := strings.LastIndexByte(t, '/'); p >= 0 {
		t = t[:p+1]
	} else {
		t = "male/"
	}

	switch ent.Client.Resp.CtfTeam {
	case CTF_TEAM1:
		g.gi.Configstring(CS_PLAYERSKINS+playernum, fmt.Sprintf("%s\\%s%s",
			ent.Client.Pers.Netname, t, CTF_TEAM1_SKIN))
	case CTF_TEAM2:
		g.gi.Configstring(CS_PLAYERSKINS+playernum,
			fmt.Sprintf("%s\\%s%s", ent.Client.Pers.Netname, t, CTF_TEAM2_SKIN))
	default:
		g.gi.Configstring(CS_PLAYERSKINS+playernum,
			fmt.Sprintf("%s\\%s", ent.Client.Pers.Netname, s))
	}
	//	gi.cprintf(ent, PRINT_HIGH, "You have been assigned to %s team.\n", ent->client->pers.netname);
}

// C: ctf/g_ctf.c:396 CTFAssignTeam
func (g *Game) CTFAssignTeam(who *GClient) {
	team1count, team2count := 0, 0

	who.Resp.CtfState = 0

	if int32(g.dmflags.Value)&DF_CTF_FORCEJOIN == 0 {
		who.Resp.CtfTeam = CTF_NOTEAM
		return
	}

	for i := 1; float32(i) <= g.maxclients.Value; i++ {
		player := &g.edicts[i]

		if !player.InUse || player.Client == who {
			continue
		}

		switch player.Client.Resp.CtfTeam {
		case CTF_TEAM1:
			team1count++
		case CTF_TEAM2:
			team2count++
		}
	}
	if team1count < team2count {
		who.Resp.CtfTeam = CTF_TEAM1
	} else if team2count < team1count {
		who.Resp.CtfTeam = CTF_TEAM2
	} else if g.rng.Rand()&1 != 0 {
		who.Resp.CtfTeam = CTF_TEAM1
	} else {
		who.Resp.CtfTeam = CTF_TEAM2
	}
}

// SelectCTFSpawnPoint: go to a ctf point, but NOT the two points closest
// to other players.
// C: ctf/g_ctf.c:441 SelectCTFSpawnPoint
func (g *Game) SelectCTFSpawnPoint(ent *Edict) *Edict {
	var spot, spot1, spot2 *Edict
	var count int32
	var range1, range2 float32
	var cname string

	if ent.Client.Resp.CtfState != 0 {
		if int32(g.dmflags.Value)&DF_SPAWN_FARTHEST != 0 {
			return g.SelectFarthestDeathmatchSpawnPoint()
		}
		return g.SelectRandomDeathmatchSpawnPoint()
	}

	ent.Client.Resp.CtfState++

	switch ent.Client.Resp.CtfTeam {
	case CTF_TEAM1:
		cname = "info_player_team1"
	case CTF_TEAM2:
		cname = "info_player_team2"
	default:
		return g.SelectRandomDeathmatchSpawnPoint()
	}

	spot = nil
	range1 = 99999
	range2 = 99999
	spot1 = nil
	spot2 = nil

	for {
		spot = g.G_Find(spot, FOFS_classname, cname)
		if spot == nil {
			break
		}
		count++
		rng := g.PlayersRangeFromSpot(spot)
		if rng < range1 {
			range1 = rng
			spot1 = spot
		} else if rng < range2 {
			range2 = rng
			spot2 = spot
		}
	}

	if count == 0 {
		return g.SelectRandomDeathmatchSpawnPoint()
	}

	if count <= 2 {
		spot1 = nil
		spot2 = nil
	} else {
		count -= 2
	}

	selection := g.rng.Rand() % count

	spot = nil
	for {
		spot = g.G_Find(spot, FOFS_classname, cname)
		if spot == spot1 || spot == spot2 {
			selection++
		}
		sel := selection
		selection--
		if sel == 0 {
			break
		}
	}

	return spot
}

/*------------------------------------------------------------------------*/

// CTFFragBonuses calculates the bonuses for flag defense, flag carrier
// defense, etc. Note that bonuses are not cumaltive. You get one, they are
// in importance order.
// C: ctf/g_ctf.c:519 CTFFragBonuses
func (g *Game) CTFFragBonuses(targ, inflictor, attacker *Edict) {
	var flag_item, enemy_flag_item *GItem
	var c string
	var v1, v2 Vec3

	if targ.Client != nil && attacker.Client != nil {
		if attacker.Client.Resp.Ghost != nil {
			if attacker != targ {
				attacker.Client.Resp.Ghost.Kills++
			}
		}
		if targ.Client.Resp.Ghost != nil {
			targ.Client.Resp.Ghost.Deaths++
		}
	}

	// no bonus for fragging yourself
	if targ.Client == nil || attacker.Client == nil || targ == attacker {
		return
	}

	otherteam := CTFOtherTeam(targ.Client.Resp.CtfTeam)
	if otherteam < 0 {
		return // whoever died isn't on a team
	}

	// same team, if the flag at base, check to he has the enemy flag
	if targ.Client.Resp.CtfTeam == CTF_TEAM1 {
		flag_item = g.ctfg.flag1_item
		enemy_flag_item = g.ctfg.flag2_item
	} else {
		flag_item = g.ctfg.flag2_item
		enemy_flag_item = g.ctfg.flag1_item
	}

	// did the attacker frag the flag carrier?
	if targ.Client.Pers.Inventory[ITEM_INDEX(enemy_flag_item)] != 0 {
		attacker.Client.Resp.CtfLastfraggedcarrier = g.level.Time
		attacker.Client.Resp.Score += CTF_FRAG_CARRIER_BONUS
		g.cprintf(attacker, PRINT_MEDIUM, "BONUS: %d points for fragging enemy flag carrier.\n",
			CTF_FRAG_CARRIER_BONUS)

		// the target had the flag, clear the hurt carrier
		// field on the other team
		for i := 1; float32(i) <= g.maxclients.Value; i++ {
			ent := &g.edicts[i]
			if ent.InUse && ent.Client.Resp.CtfTeam == otherteam {
				ent.Client.Resp.CtfLasthurtcarrier = 0
			}
		}
		return
	}

	if targ.Client.Resp.CtfLasthurtcarrier != 0 &&
		g.level.Time-targ.Client.Resp.CtfLasthurtcarrier < CTF_CARRIER_DANGER_PROTECT_TIMEOUT &&
		attacker.Client.Pers.Inventory[ITEM_INDEX(flag_item)] == 0 {
		// attacker is on the same team as the flag carrier and
		// fragged a guy who hurt our flag carrier
		attacker.Client.Resp.Score += CTF_CARRIER_DANGER_PROTECT_BONUS
		g.bprintf(PRINT_MEDIUM, "%s defends %s's flag carrier against an agressive enemy\n",
			attacker.Client.Pers.Netname,
			CTFTeamName(attacker.Client.Resp.CtfTeam))
		if attacker.Client.Resp.Ghost != nil {
			attacker.Client.Resp.Ghost.Carrierdef++
		}
		return
	}

	// flag and flag carrier area defense bonuses

	// we have to find the flag and carrier entities

	// find the flag
	switch attacker.Client.Resp.CtfTeam {
	case CTF_TEAM1:
		c = "item_flag_team1"
	case CTF_TEAM2:
		c = "item_flag_team2"
	default:
		return
	}

	var flag *Edict
	for {
		flag = g.G_Find(flag, FOFS_classname, c)
		if flag == nil {
			break
		}
		if flag.Spawnflags&DROPPED_ITEM == 0 {
			break
		}
	}

	if flag == nil {
		return // can't find attacker's flag
	}

	// find attacker's team's flag carrier
	var carrier *Edict
	for i := 1; float32(i) <= g.maxclients.Value; i++ {
		carrier = &g.edicts[i]
		if carrier.InUse &&
			carrier.Client.Pers.Inventory[ITEM_INDEX(flag_item)] != 0 {
			break
		}
		carrier = nil
	}

	// ok we have the attackers flag and a pointer to the carrier

	// check to see if we are defending the base's flag
	v1 = shared.VectorSubtract(targ.S.Origin, flag.S.Origin)
	v2 = shared.VectorSubtract(attacker.S.Origin, flag.S.Origin)

	if (shared.VectorLength(v1) < CTF_TARGET_PROTECT_RADIUS ||
		shared.VectorLength(v2) < CTF_TARGET_PROTECT_RADIUS ||
		g.loc_CanSee(flag, targ) || g.loc_CanSee(flag, attacker)) &&
		attacker.Client.Resp.CtfTeam != targ.Client.Resp.CtfTeam {
		// we defended the base flag
		attacker.Client.Resp.Score += CTF_FLAG_DEFENSE_BONUS
		if flag.Solid == SOLID_NOT {
			g.bprintf(PRINT_MEDIUM, "%s defends the %s base.\n",
				attacker.Client.Pers.Netname,
				CTFTeamName(attacker.Client.Resp.CtfTeam))
		} else {
			g.bprintf(PRINT_MEDIUM, "%s defends the %s flag.\n",
				attacker.Client.Pers.Netname,
				CTFTeamName(attacker.Client.Resp.CtfTeam))
		}
		if attacker.Client.Resp.Ghost != nil {
			attacker.Client.Resp.Ghost.Basedef++
		}
		return
	}

	if carrier != nil && carrier != attacker {
		v1 = shared.VectorSubtract(targ.S.Origin, carrier.S.Origin)
		v1 = shared.VectorSubtract(attacker.S.Origin, carrier.S.Origin) // sic: v2 keeps the flag distance

		if shared.VectorLength(v1) < CTF_ATTACKER_PROTECT_RADIUS ||
			shared.VectorLength(v2) < CTF_ATTACKER_PROTECT_RADIUS ||
			g.loc_CanSee(carrier, targ) || g.loc_CanSee(carrier, attacker) {
			attacker.Client.Resp.Score += CTF_CARRIER_PROTECT_BONUS
			g.bprintf(PRINT_MEDIUM, "%s defends the %s's flag carrier.\n",
				attacker.Client.Pers.Netname,
				CTFTeamName(attacker.Client.Resp.CtfTeam))
			if attacker.Client.Resp.Ghost != nil {
				attacker.Client.Resp.Ghost.Carrierdef++
			}
			return
		}
	}
}

// C: ctf/g_ctf.c:662 CTFCheckHurtCarrier
func (g *Game) CTFCheckHurtCarrier(targ, attacker *Edict) {
	var flag_item *GItem

	if targ.Client == nil || attacker.Client == nil {
		return
	}

	if targ.Client.Resp.CtfTeam == CTF_TEAM1 {
		flag_item = g.ctfg.flag2_item
	} else {
		flag_item = g.ctfg.flag1_item
	}

	if targ.Client.Pers.Inventory[ITEM_INDEX(flag_item)] != 0 &&
		targ.Client.Resp.CtfTeam != attacker.Client.Resp.CtfTeam {
		attacker.Client.Resp.CtfLasthurtcarrier = g.level.Time
	}
}

/*------------------------------------------------------------------------*/

// C: ctf/g_ctf.c:682 CTFResetFlag
func (g *Game) CTFResetFlag(ctf_team int32) {
	var c string

	switch ctf_team {
	case CTF_TEAM1:
		c = "item_flag_team1"
	case CTF_TEAM2:
		c = "item_flag_team2"
	default:
		return
	}

	var ent *Edict
	for {
		ent = g.G_Find(ent, FOFS_classname, c)
		if ent == nil {
			break
		}
		if ent.Spawnflags&DROPPED_ITEM != 0 {
			g.G_FreeEdict(ent)
		} else {
			ent.SVFlags &^= SVF_NOCLIENT
			ent.Solid = SOLID_TRIGGER
			g.gi.LinkEntity(ent)
			ent.S.Event = EV_ITEM_RESPAWN
		}
	}
}

// C: ctf/g_ctf.c:711 CTFResetFlags
func (g *Game) CTFResetFlags() {
	g.CTFResetFlag(CTF_TEAM1)
	g.CTFResetFlag(CTF_TEAM2)
}

// C: ctf/g_ctf.c:717 CTFPickup_Flag
func (g *Game) CTFPickup_Flag(ent, other *Edict) bool {
	var ctf_team int32
	var flag_item, enemy_flag_item *GItem

	// figure out what team this flag is
	if ent.Classname == "item_flag_team1" {
		ctf_team = CTF_TEAM1
	} else if ent.Classname == "item_flag_team2" {
		ctf_team = CTF_TEAM2
	} else {
		g.gi.Cprintf(ent, PRINT_HIGH, "Don't know what team the flag is on.\n")
		return false
	}

	// same team, if the flag at base, check to he has the enemy flag
	if ctf_team == CTF_TEAM1 {
		flag_item = g.ctfg.flag1_item
		enemy_flag_item = g.ctfg.flag2_item
	} else {
		flag_item = g.ctfg.flag2_item
		enemy_flag_item = g.ctfg.flag1_item
	}

	cg := &g.ctfg.ctfgame
	if ctf_team == other.Client.Resp.CtfTeam {
		if ent.Spawnflags&DROPPED_ITEM == 0 {
			// the flag is at home base.  if the player has the enemy
			// flag, he's just won!

			if other.Client.Pers.Inventory[ITEM_INDEX(enemy_flag_item)] != 0 {
				g.bprintf(PRINT_HIGH, "%s captured the %s flag!\n",
					other.Client.Pers.Netname, CTFOtherTeamName(ctf_team))
				other.Client.Pers.Inventory[ITEM_INDEX(enemy_flag_item)] = 0

				cg.last_flag_capture = g.level.Time
				cg.last_capture_team = ctf_team
				if ctf_team == CTF_TEAM1 {
					cg.team1++
				} else {
					cg.team2++
				}

				g.gi.Sound(ent, CHAN_RELIABLE+CHAN_NO_PHS_ADD+CHAN_VOICE, g.gi.SoundIndex("ctf/flagcap.wav"), 1, ATTN_NONE, 0)

				// other gets another 10 frag bonus
				other.Client.Resp.Score += CTF_CAPTURE_BONUS
				if other.Client.Resp.Ghost != nil {
					other.Client.Resp.Ghost.Caps++
				}

				// Ok, let's do the player loop, hand out the bonuses
				for i := 1; float32(i) <= g.maxclients.Value; i++ {
					player := &g.edicts[i]
					if !player.InUse {
						continue
					}

					if player.Client.Resp.CtfTeam != other.Client.Resp.CtfTeam {
						player.Client.Resp.CtfLasthurtcarrier = -5
					} else if player.Client.Resp.CtfTeam == other.Client.Resp.CtfTeam {
						if player != other {
							player.Client.Resp.Score += CTF_TEAM_BONUS
						}
						// award extra points for capture assists
						if player.Client.Resp.CtfLastreturnedflag+CTF_RETURN_FLAG_ASSIST_TIMEOUT > g.level.Time {
							g.bprintf(PRINT_HIGH, "%s gets an assist for returning the flag!\n", player.Client.Pers.Netname)
							player.Client.Resp.Score += CTF_RETURN_FLAG_ASSIST_BONUS
						}
						if player.Client.Resp.CtfLastfraggedcarrier+CTF_FRAG_CARRIER_ASSIST_TIMEOUT > g.level.Time {
							g.bprintf(PRINT_HIGH, "%s gets an assist for fragging the flag carrier!\n", player.Client.Pers.Netname)
							player.Client.Resp.Score += CTF_FRAG_CARRIER_ASSIST_BONUS
						}
					}
				}

				g.CTFResetFlags()
				return false
			}
			return false // its at home base already
		}
		// hey, its not home.  return it by teleporting it back
		g.bprintf(PRINT_HIGH, "%s returned the %s flag!\n",
			other.Client.Pers.Netname, CTFTeamName(ctf_team))
		other.Client.Resp.Score += CTF_RECOVERY_BONUS
		other.Client.Resp.CtfLastreturnedflag = g.level.Time
		g.gi.Sound(ent, CHAN_RELIABLE+CHAN_NO_PHS_ADD+CHAN_VOICE, g.gi.SoundIndex("ctf/flagret.wav"), 1, ATTN_NONE, 0)
		//CTFResetFlag will remove this entity!  We must return false
		g.CTFResetFlag(ctf_team)
		return false
	}

	// hey, its not our flag, pick it up
	g.bprintf(PRINT_HIGH, "%s got the %s flag!\n",
		other.Client.Pers.Netname, CTFTeamName(ctf_team))
	other.Client.Resp.Score += CTF_FLAG_BONUS

	other.Client.Pers.Inventory[ITEM_INDEX(flag_item)] = 1
	other.Client.Resp.CtfFlagsince = g.level.Time

	// pick up the flag
	// if it's not a dropped flag, we just make is disappear
	// if it's dropped, it will be removed by the pickup caller
	if ent.Spawnflags&DROPPED_ITEM == 0 {
		ent.Flags |= FL_RESPAWN
		ent.SVFlags |= SVF_NOCLIENT
		ent.Solid = SOLID_NOT
	}
	return true
}

// C: ctf/g_ctf.c:826 CTFDropFlagTouch
func (g *Game) CTFDropFlagTouch(ent, other *Edict, plane *CPlane, surf *CSurface) {
	//owner (who dropped us) can't touch for two secs
	if other == ent.Owner &&
		ent.Nextthink-g.level.Time > CTF_AUTO_FLAG_RETURN_TIMEOUT-2 {
		return
	}

	g.Touch_Item(ent, other, plane, surf)
}

// C: ctf/g_ctf.c:836 CTFDropFlagThink
func (g *Game) CTFDropFlagThink(ent *Edict) {
	// auto return the flag
	// reset flag will remove ourselves
	if ent.Classname == "item_flag_team1" {
		g.CTFResetFlag(CTF_TEAM1)
		g.bprintf(PRINT_HIGH, "The %s flag has returned!\n",
			CTFTeamName(CTF_TEAM1))
	} else if ent.Classname == "item_flag_team2" {
		g.CTFResetFlag(CTF_TEAM2)
		g.bprintf(PRINT_HIGH, "The %s flag has returned!\n",
			CTFTeamName(CTF_TEAM2))
	}
}

// CTFDeadDropFlag is called from PlayerDie, to drop the flag from a dying player.
// C: ctf/g_ctf.c:852 CTFDeadDropFlag
func (g *Game) CTFDeadDropFlag(self *Edict) {
	var dropped *Edict
	f1, f2 := g.ctfg.flag1_item, g.ctfg.flag2_item

	if self.Client.Pers.Inventory[ITEM_INDEX(f1)] != 0 {
		dropped = g.Drop_Item(self, f1)
		self.Client.Pers.Inventory[ITEM_INDEX(f1)] = 0
		g.bprintf(PRINT_HIGH, "%s lost the %s flag!\n",
			self.Client.Pers.Netname, CTFTeamName(CTF_TEAM1))
	} else if self.Client.Pers.Inventory[ITEM_INDEX(f2)] != 0 {
		dropped = g.Drop_Item(self, f2)
		self.Client.Pers.Inventory[ITEM_INDEX(f2)] = 0
		g.bprintf(PRINT_HIGH, "%s lost the %s flag!\n",
			self.Client.Pers.Netname, CTFTeamName(CTF_TEAM2))
	}

	if dropped != nil {
		dropped.Think = CTFDropFlagThink
		dropped.Nextthink = g.level.Time + CTF_AUTO_FLAG_RETURN_TIMEOUT
		dropped.Touch = CTFDropFlagTouch
	}
}

// CTFDrop_Flag (qboolean in C, called through gitem_t.drop).
// C: ctf/g_ctf.c:875 CTFDrop_Flag
func (g *Game) CTFDrop_Flag(ent *Edict, item *GItem) {
	if g.rng.Rand()&1 != 0 {
		g.gi.Cprintf(ent, PRINT_HIGH, "Only lusers drop flags.\n")
	} else {
		g.gi.Cprintf(ent, PRINT_HIGH, "Winners don't drop flags.\n")
	}
}

// C: ctf/g_ctf.c:884 CTFFlagThink
func (g *Game) CTFFlagThink(ent *Edict) {
	if ent.Solid != SOLID_NOT {
		ent.S.Frame = 173 + (((ent.S.Frame - 173) + 1) % 16)
	}
	ent.Nextthink = float32(float64(g.level.Time) + FRAMETIME)
}

// C: ctf/g_ctf.c:892 CTFFlagSetup
func (g *Game) CTFFlagSetup(ent *Edict) {
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
		g.dprintf("CTFFlagSetup: %s startsolid at %s\n", ent.Classname, vtos(ent.S.Origin))
		g.G_FreeEdict(ent)
		return
	}

	ent.S.Origin = tr.EndPos

	g.gi.LinkEntity(ent)

	ent.Nextthink = float32(float64(g.level.Time) + FRAMETIME)
	ent.Think = CTFFlagThink
}

// C: ctf/g_ctf.c:930 CTFEffects
func (g *Game) CTFEffects(player *Edict) {
	f1, f2 := ITEM_INDEX(g.ctfg.flag1_item), ITEM_INDEX(g.ctfg.flag2_item)
	player.S.Effects &^= (EF_FLAG1 | EF_FLAG2)
	if player.Health > 0 {
		if player.Client.Pers.Inventory[f1] != 0 {
			player.S.Effects |= EF_FLAG1
		}
		if player.Client.Pers.Inventory[f2] != 0 {
			player.S.Effects |= EF_FLAG2
		}
	}

	if player.Client.Pers.Inventory[f1] != 0 {
		player.S.ModelIndex3 = int32(g.gi.ModelIndex("players/male/flag1.md2"))
	} else if player.Client.Pers.Inventory[f2] != 0 {
		player.S.ModelIndex3 = int32(g.gi.ModelIndex("players/male/flag2.md2"))
	} else {
		player.S.ModelIndex3 = 0
	}
}

// CTFCalcScores is called when we enter the intermission.
// C: ctf/g_ctf.c:951 CTFCalcScores
func (g *Game) CTFCalcScores() {
	cg := &g.ctfg.ctfgame
	cg.total1 = 0
	cg.total2 = 0
	for i := 0; float32(i) < g.maxclients.Value; i++ {
		if !g.edicts[i+1].InUse {
			continue
		}
		if g.game.Clients[i].Resp.CtfTeam == CTF_TEAM1 {
			cg.total1 += g.game.Clients[i].Resp.Score
		} else if g.game.Clients[i].Resp.CtfTeam == CTF_TEAM2 {
			cg.total2 += g.game.Clients[i].Resp.Score
		}
	}
}

// C: ctf/g_ctf.c:966 CTFID_f
func (g *Game) CTFID_f(ent *Edict) {
	if ent.Client.Resp.IdState {
		g.gi.Cprintf(ent, PRINT_HIGH, "Disabling player identication display.\n")
		ent.Client.Resp.IdState = false
	} else {
		g.gi.Cprintf(ent, PRINT_HIGH, "Activating player identication display.\n")
		ent.Client.Resp.IdState = true
	}
}

// C: ctf/g_ctf.c:977 CTFSetIDView
func (g *Game) CTFSetIDView(ent *Edict) {
	var forward Vec3
	var best *Edict
	var bd float32

	ent.Client.PS.Stats[STAT_CTF_ID_VIEW] = 0

	shared.AngleVectors(ent.Client.VAngle, &forward, nil, nil)
	forward = shared.VectorScale(forward, 1024)
	forward = shared.VectorAdd(ent.S.Origin, forward)
	tr := g.gi.Trace(&ent.S.Origin, &shared.Vec3Origin, &shared.Vec3Origin, &forward, ent, MASK_SOLID)
	if tr.Fraction < 1 && tr.Ent != nil && tr.Ent.Client != nil {
		ent.Client.PS.Stats[STAT_CTF_ID_VIEW] =
			int16(CS_PLAYERSKINS + (ent.Index - 1))
		return
	}

	shared.AngleVectors(ent.Client.VAngle, &forward, nil, nil)
	best = nil
	for i := 1; float32(i) <= g.maxclients.Value; i++ {
		who := &g.edicts[i]
		if !who.InUse || who.Solid == SOLID_NOT {
			continue
		}
		dir := shared.VectorSubtract(who.S.Origin, ent.S.Origin)
		shared.VectorNormalize(&dir)
		d := shared.DotProduct(forward, dir)
		if d > bd && g.loc_CanSee(ent, who) {
			bd = d
			best = who
		}
	}
	if float64(bd) > 0.90 {
		ent.Client.PS.Stats[STAT_CTF_ID_VIEW] =
			int16(CS_PLAYERSKINS + (best.Index - 1))
	}
}

// C: ctf/g_ctf.c:1016 SetCTFStats
func (g *Game) SetCTFStats(ent *Edict) {
	cg := &g.ctfg.ctfgame
	stats := &ent.Client.PS.Stats
	f1, f2 := ITEM_INDEX(g.ctfg.flag1_item), ITEM_INDEX(g.ctfg.flag2_item)

	if cg.match > MATCH_NONE {
		stats[STAT_CTF_MATCH] = CONFIG_CTF_MATCH
	} else {
		stats[STAT_CTF_MATCH] = 0
	}

	//ghosting
	if ent.Client.Resp.Ghost != nil {
		ent.Client.Resp.Ghost.Score = ent.Client.Resp.Score
		ent.Client.Resp.Ghost.Netname = ent.Client.Pers.Netname
		ent.Client.Resp.Ghost.Number = ent.S.Number
	}

	// logo headers for the frag display
	stats[STAT_CTF_TEAM1_HEADER] = int16(g.gi.ImageIndex("ctfsb1"))
	stats[STAT_CTF_TEAM2_HEADER] = int16(g.gi.ImageIndex("ctfsb2"))

	// if during intermission, we must blink the team header of the winning team
	if g.level.Intermissiontime != 0 && g.level.Framenum&8 != 0 { // blink 1/8th second
		// note that ctfgame.total[12] is set when we go to intermission
		if cg.team1 > cg.team2 {
			stats[STAT_CTF_TEAM1_HEADER] = 0
		} else if cg.team2 > cg.team1 {
			stats[STAT_CTF_TEAM2_HEADER] = 0
		} else if cg.total1 > cg.total2 { // frag tie breaker
			stats[STAT_CTF_TEAM1_HEADER] = 0
		} else if cg.total2 > cg.total1 {
			stats[STAT_CTF_TEAM2_HEADER] = 0
		} else { // tie game!
			stats[STAT_CTF_TEAM1_HEADER] = 0
			stats[STAT_CTF_TEAM2_HEADER] = 0
		}
	}

	// tech icon
	stats[STAT_CTF_TECH] = 0
	for _, tn := range tnames {
		if tech := g.FindItemByClassname(tn); tech != nil &&
			ent.Client.Pers.Inventory[ITEM_INDEX(tech)] != 0 {
			stats[STAT_CTF_TECH] = int16(g.gi.ImageIndex(tech.Icon))
			break
		}
	}

	// figure out what icon to display for team logos
	// three states:
	//   flag at base
	//   flag taken
	//   flag dropped
	p1 := g.gi.ImageIndex("i_ctf1")
	e := g.G_Find(nil, FOFS_classname, "item_flag_team1")
	if e != nil {
		if e.Solid == SOLID_NOT {
			// not at base
			// check if on player
			p1 = g.gi.ImageIndex("i_ctf1d") // default to dropped
			for i := 1; float32(i) <= g.maxclients.Value; i++ {
				if g.edicts[i].InUse &&
					g.edicts[i].Client.Pers.Inventory[f1] != 0 {
					// enemy has it
					p1 = g.gi.ImageIndex("i_ctf1t")
					break
				}
			}
		} else if e.Spawnflags&DROPPED_ITEM != 0 {
			p1 = g.gi.ImageIndex("i_ctf1d") // must be dropped
		}
	}
	p2 := g.gi.ImageIndex("i_ctf2")
	e = g.G_Find(nil, FOFS_classname, "item_flag_team2")
	if e != nil {
		if e.Solid == SOLID_NOT {
			// not at base
			// check if on player
			p2 = g.gi.ImageIndex("i_ctf2d") // default to dropped
			for i := 1; float32(i) <= g.maxclients.Value; i++ {
				if g.edicts[i].InUse &&
					g.edicts[i].Client.Pers.Inventory[f2] != 0 {
					// enemy has it
					p2 = g.gi.ImageIndex("i_ctf2t")
					break
				}
			}
		} else if e.Spawnflags&DROPPED_ITEM != 0 {
			p2 = g.gi.ImageIndex("i_ctf2d") // must be dropped
		}
	}

	stats[STAT_CTF_TEAM1_PIC] = int16(p1)
	stats[STAT_CTF_TEAM2_PIC] = int16(p2)

	if cg.last_flag_capture != 0 && g.level.Time-cg.last_flag_capture < 5 {
		if cg.last_capture_team == CTF_TEAM1 {
			if g.level.Framenum&8 != 0 {
				stats[STAT_CTF_TEAM1_PIC] = int16(p1)
			} else {
				stats[STAT_CTF_TEAM1_PIC] = 0
			}
		} else {
			if g.level.Framenum&8 != 0 {
				stats[STAT_CTF_TEAM2_PIC] = int16(p2)
			} else {
				stats[STAT_CTF_TEAM2_PIC] = 0
			}
		}
	}

	stats[STAT_CTF_TEAM1_CAPS] = int16(cg.team1)
	stats[STAT_CTF_TEAM2_CAPS] = int16(cg.team2)

	stats[STAT_CTF_FLAG_PIC] = 0
	if ent.Client.Resp.CtfTeam == CTF_TEAM1 &&
		ent.Client.Pers.Inventory[f2] != 0 &&
		g.level.Framenum&8 != 0 {
		stats[STAT_CTF_FLAG_PIC] = int16(g.gi.ImageIndex("i_ctf2"))
	} else if ent.Client.Resp.CtfTeam == CTF_TEAM2 &&
		ent.Client.Pers.Inventory[f1] != 0 &&
		g.level.Framenum&8 != 0 {
		stats[STAT_CTF_FLAG_PIC] = int16(g.gi.ImageIndex("i_ctf1"))
	}

	stats[STAT_CTF_JOINED_TEAM1_PIC] = 0
	stats[STAT_CTF_JOINED_TEAM2_PIC] = 0
	if ent.Client.Resp.CtfTeam == CTF_TEAM1 {
		stats[STAT_CTF_JOINED_TEAM1_PIC] = int16(g.gi.ImageIndex("i_ctfj"))
	} else if ent.Client.Resp.CtfTeam == CTF_TEAM2 {
		stats[STAT_CTF_JOINED_TEAM2_PIC] = int16(g.gi.ImageIndex("i_ctfj"))
	}

	stats[STAT_CTF_ID_VIEW] = 0
	if ent.Client.Resp.IdState {
		g.CTFSetIDView(ent)
	}
}

/*------------------------------------------------------------------------*/

/*QUAKED info_player_team1 (1 0 0) (-16 -16 -24) (16 16 32)
potential team1 spawning position for ctf games
*/
// C: ctf/g_ctf.c:1160 SP_info_player_team1
func (g *Game) SP_info_player_team1(self *Edict) {
}

/*QUAKED info_player_team2 (0 0 1) (-16 -16 -24) (16 16 32)
potential team2 spawning position for ctf games
*/
// C: ctf/g_ctf.c:1167 SP_info_player_team2
func (g *Game) SP_info_player_team2(self *Edict) {
}

/*------------------------------------------------------------------------*/
/* GRAPPLE																  */
/*------------------------------------------------------------------------*/

// CTFPlayerResetGrapple: ent is player.
// C: ctf/g_ctf.c:1177 CTFPlayerResetGrapple
func (g *Game) CTFPlayerResetGrapple(ent *Edict) {
	if ent.Client != nil && ent.Client.CtfGrapple != nil {
		g.CTFResetGrapple(ent.Client.CtfGrapple)
	}
}

// CTFResetGrapple: self is grapple, not player.
// C: ctf/g_ctf.c:1184 CTFResetGrapple
func (g *Game) CTFResetGrapple(self *Edict) {
	if self.Owner.Client.CtfGrapple != nil {
		var volume float32 = 1.0

		if self.Owner.Client.SilencerShots != 0 {
			volume = 0.2
		}

		g.gi.Sound(self.Owner, CHAN_RELIABLE+CHAN_WEAPON, g.gi.SoundIndex("weapons/grapple/grreset.wav"), volume, ATTN_NORM, 0)
		cl := self.Owner.Client
		cl.CtfGrapple = nil
		cl.CtfGrapplereleasetime = g.level.Time
		cl.CtfGrapplestate = CTF_GRAPPLE_STATE_FLY // we're firing, not on hook
		cl.PS.PMove.PmFlags &^= PMF_NO_PREDICTION
		g.G_FreeEdict(self)
	}
}

// C: ctf/g_ctf.c:1203 CTFGrappleTouch
func (g *Game) CTFGrappleTouch(self, other *Edict, plane *CPlane, surf *CSurface) {
	var volume float32 = 1.0

	if other == self.Owner {
		return
	}

	if self.Owner.Client.CtfGrapplestate != CTF_GRAPPLE_STATE_FLY {
		return
	}

	if surf != nil && surf.Flags&SURF_SKY != 0 {
		g.CTFResetGrapple(self)
		return
	}

	self.Velocity = shared.Vec3Origin

	g.PlayerNoise(self.Owner, self.S.Origin, PNOISE_IMPACT)

	if other.Takedamage != 0 {
		// plane is NULL when called from CTFFireGrapple: C passes a NULL
		// normal, which SpawnDamage writes like the zero vector
		var normal Vec3
		if plane != nil {
			normal = plane.Normal
		}
		g.T_Damage(other, self, self.Owner, &self.Velocity, self.S.Origin, normal, self.Dmg, 1, 0, MOD_GRAPPLE)
		g.CTFResetGrapple(self)
		return
	}

	self.Owner.Client.CtfGrapplestate = CTF_GRAPPLE_STATE_PULL // we're on hook
	self.Enemy = other

	self.Solid = SOLID_NOT

	if self.Owner.Client.SilencerShots != 0 {
		volume = 0.2
	}

	g.gi.Sound(self.Owner, CHAN_RELIABLE+CHAN_WEAPON, g.gi.SoundIndex("weapons/grapple/grpull.wav"), volume, ATTN_NORM, 0)
	g.gi.Sound(self, CHAN_WEAPON, g.gi.SoundIndex("weapons/grapple/grhit.wav"), volume, ATTN_NORM, 0)

	g.gi.WriteByteC(svc_temp_entity)
	g.gi.WriteByteC(TE_SPARKS)
	g.gi.WritePosition(&self.S.Origin)
	if plane == nil {
		g.gi.WriteDir(&shared.Vec3Origin)
	} else {
		g.gi.WriteDir(&plane.Normal)
	}
	g.gi.Multicast(&self.S.Origin, MULTICAST_PVS)
}

// CTFGrappleDrawCable draws a beam between grapple and self.
// C: ctf/g_ctf.c:1251 CTFGrappleDrawCable
func (g *Game) CTFGrappleDrawCable(self *Edict) {
	var f, r Vec3

	shared.AngleVectors(self.Owner.Client.VAngle, &f, &r, nil)
	offset := Vec3{16, 16, float32(self.Owner.Viewheight - 8)}
	start := P_ProjectSource(self.Owner.Client, self.Owner.S.Origin, offset, f, r)

	offset = shared.VectorSubtract(start, self.Owner.S.Origin)

	dir := shared.VectorSubtract(start, self.S.Origin)
	distance := shared.VectorLength(dir)
	// don't draw cable if close
	if distance < 64 {
		return
	}

	end := self.S.Origin

	g.gi.WriteByteC(svc_temp_entity)
	g.gi.WriteByteC(TE_GRAPPLE_CABLE)
	g.gi.WriteShort(self.Owner.Index)
	g.gi.WritePosition(&self.Owner.S.Origin)
	g.gi.WritePosition(&end)
	g.gi.WritePosition(&offset)
	g.gi.Multicast(&self.S.Origin, MULTICAST_PVS)
}

// CTFGrapplePull pulls the player toward the grapple.
// C: ctf/g_ctf.c:1315 CTFGrapplePull
func (g *Game) CTFGrapplePull(self *Edict) {
	ocl := self.Owner.Client
	if ocl.Pers.Weapon != nil && ocl.Pers.Weapon.Classname == "weapon_grapple" &&
		ocl.Newweapon == nil &&
		ocl.Weaponstate != WEAPON_FIRING &&
		ocl.Weaponstate != WEAPON_ACTIVATING {
		g.CTFResetGrapple(self)
		return
	}

	if self.Enemy != nil {
		if self.Enemy.Solid == SOLID_NOT {
			g.CTFResetGrapple(self)
			return
		}
		if self.Enemy.Solid == SOLID_BBOX {
			v := shared.VectorScale(self.Enemy.Size, 0.5)
			v = shared.VectorAdd(v, self.Enemy.S.Origin)
			self.S.Origin = shared.VectorAdd(v, self.Enemy.Mins)
			g.gi.LinkEntity(self)
		} else {
			self.Velocity = self.Enemy.Velocity
		}
		if self.Enemy.Takedamage != 0 &&
			!g.CheckTeamDamage(self.Enemy, self.Owner) {
			var volume float32 = 1.0

			if ocl.SilencerShots != 0 {
				volume = 0.2
			}

			g.T_Damage(self.Enemy, self, self.Owner, &self.Velocity, self.S.Origin, shared.Vec3Origin, 1, 1, 0, MOD_GRAPPLE)
			g.gi.Sound(self, CHAN_WEAPON, g.gi.SoundIndex("weapons/grapple/grhurt.wav"), volume, ATTN_NORM, 0)
		}
		if self.Enemy.Deadflag != 0 { // he died
			g.CTFResetGrapple(self)
			return
		}
	}

	g.CTFGrappleDrawCable(self)

	if ocl.CtfGrapplestate > CTF_GRAPPLE_STATE_FLY {
		// pull player toward grapple
		// this causes icky stuff with prediction, we need to extend
		// the prediction layer to include two new fields in the player
		// move stuff: a point and a velocity.  The client should add
		// that velociy in the direction of the point
		var forward, up Vec3

		shared.AngleVectors(ocl.VAngle, &forward, nil, &up)
		v := self.Owner.S.Origin
		v[2] += float32(self.Owner.Viewheight)
		hookdir := shared.VectorSubtract(self.S.Origin, v)

		vlen := shared.VectorLength(hookdir)

		if ocl.CtfGrapplestate == CTF_GRAPPLE_STATE_PULL &&
			vlen < 64 {
			var volume float32 = 1.0

			if ocl.SilencerShots != 0 {
				volume = 0.2
			}

			ocl.PS.PMove.PmFlags |= PMF_NO_PREDICTION
			g.gi.Sound(self.Owner, CHAN_RELIABLE+CHAN_WEAPON, g.gi.SoundIndex("weapons/grapple/grhang.wav"), volume, ATTN_NORM, 0)
			ocl.CtfGrapplestate = CTF_GRAPPLE_STATE_HANG
		}

		shared.VectorNormalize(&hookdir)
		hookdir = shared.VectorScale(hookdir, CTF_GRAPPLE_PULL_SPEED)
		self.Owner.Velocity = hookdir
		g.SV_AddGravity(self.Owner)
	}
}

// C: ctf/g_ctf.c:1392 CTFFireGrapple
func (g *Game) CTFFireGrapple(self *Edict, start, dir Vec3, damage, speed, effect int32) {
	shared.VectorNormalize(&dir)

	grapple := g.G_Spawn()
	grapple.S.Origin = start
	grapple.S.OldOrigin = start
	grapple.S.Angles = vectoangles(dir)
	grapple.Velocity = shared.VectorScale(dir, float32(speed))
	grapple.Movetype = MOVETYPE_FLYMISSILE
	grapple.ClipMask = MASK_SHOT
	grapple.Solid = SOLID_BBOX
	grapple.S.Effects |= uint32(effect)
	grapple.Mins = Vec3{}
	grapple.Maxs = Vec3{}
	grapple.S.ModelIndex = int32(g.gi.ModelIndex("models/weapons/grapple/hook/tris.md2"))
	//	grapple->s.sound = gi.soundindex ("misc/lasfly.wav");
	grapple.Owner = self
	grapple.Touch = CTFGrappleTouch
	//	grapple->nextthink = level.time + FRAMETIME;
	//	grapple->think = CTFGrappleThink;
	grapple.Dmg = damage
	self.Client.CtfGrapple = grapple
	self.Client.CtfGrapplestate = CTF_GRAPPLE_STATE_FLY // we're firing, not on hook
	g.gi.LinkEntity(grapple)

	tr := g.gi.Trace(&self.S.Origin, &shared.Vec3Origin, &shared.Vec3Origin, &grapple.S.Origin, grapple, MASK_SHOT)
	if tr.Fraction < 1.0 {
		grapple.S.Origin = shared.VectorMA(grapple.S.Origin, -10, dir)
		grapple.Touch.fn(g, grapple, tr.Ent, nil, nil)
	}
}

// C: ctf/g_ctf.c:1429 CTFGrappleFire
func (g *Game) CTFGrappleFire(ent *Edict, g_offset Vec3, damage, effect int32) {
	var forward, right Vec3
	var volume float32 = 1.0

	if ent.Client.CtfGrapplestate > CTF_GRAPPLE_STATE_FLY {
		return // it's already out
	}

	shared.AngleVectors(ent.Client.VAngle, &forward, &right, nil)
	//	VectorSet(offset, 24, 16, ent->viewheight-8+2);
	offset := Vec3{24, 8, float32(ent.Viewheight - 8 + 2)}
	offset = shared.VectorAdd(offset, g_offset)
	start := P_ProjectSource(ent.Client, ent.S.Origin, offset, forward, right)

	ent.Client.KickOrigin = shared.VectorScale(forward, -2)
	ent.Client.KickAngles[0] = -1

	if ent.Client.SilencerShots != 0 {
		volume = 0.2
	}

	g.gi.Sound(ent, CHAN_RELIABLE+CHAN_WEAPON, g.gi.SoundIndex("weapons/grapple/grfire.wav"), volume, ATTN_NORM, 0)
	g.CTFFireGrapple(ent, start, forward, damage, CTF_GRAPPLE_SPEED, effect)

	g.PlayerNoise(ent, start, PNOISE_WEAPON)
}

// C: ctf/g_ctf.c:1466 CTFWeapon_Grapple_Fire
func (g *Game) CTFWeapon_Grapple_Fire(ent *Edict) {
	var damage int32 = 10
	g.CTFGrappleFire(ent, shared.Vec3Origin, damage, 0)
	ent.Client.PS.GunFrame++
}

var (
	grapple_pause_frames = []int32{10, 18, 27, 0}
	grapple_fire_frames  = []int32{6, 0}
)

// C: ctf/g_ctf.c:1475 CTFWeapon_Grapple
func (g *Game) CTFWeapon_Grapple(ent *Edict) {
	cl := ent.Client

	// if the the attack button is still down, stay in the firing frame
	if cl.Buttons&BUTTON_ATTACK != 0 &&
		cl.Weaponstate == WEAPON_FIRING &&
		cl.CtfGrapple != nil {
		cl.PS.GunFrame = 9
	}

	if cl.Buttons&BUTTON_ATTACK == 0 &&
		cl.CtfGrapple != nil {
		g.CTFResetGrapple(cl.CtfGrapple)
		if cl.Weaponstate == WEAPON_FIRING {
			cl.Weaponstate = WEAPON_READY
		}
	}

	if cl.Newweapon != nil &&
		cl.CtfGrapplestate > CTF_GRAPPLE_STATE_FLY &&
		cl.Weaponstate == WEAPON_FIRING {
		// he wants to change weapons while grappled
		cl.Weaponstate = WEAPON_DROPPING
		cl.PS.GunFrame = 32
	}

	prevstate := cl.Weaponstate
	g.Weapon_Generic(ent, 5, 9, 31, 36, grapple_pause_frames, grapple_fire_frames,
		(*Game).CTFWeapon_Grapple_Fire)

	// if we just switched back to grapple, immediately go to fire frame
	if prevstate == WEAPON_ACTIVATING &&
		cl.Weaponstate == WEAPON_READY &&
		cl.CtfGrapplestate > CTF_GRAPPLE_STATE_FLY {
		if cl.Buttons&BUTTON_ATTACK == 0 {
			cl.PS.GunFrame = 9
		} else {
			cl.PS.GunFrame = 5
		}
		cl.Weaponstate = WEAPON_FIRING
	}
}

// C: ctf/g_ctf.c:1519 CTFTeam_f
func (g *Game) CTFTeam_f(ent *Edict) {
	var desired_team int32

	t := g.gi.Args()
	if t == "" {
		g.cprintf(ent, PRINT_HIGH, "You are on the %s team.\n",
			CTFTeamName(ent.Client.Resp.CtfTeam))
		return
	}

	if g.ctfg.ctfgame.match > MATCH_SETUP {
		g.gi.Cprintf(ent, PRINT_HIGH, "Can't change teams in a match.\n")
		return
	}

	if shared.Q_stricmp(t, "red") == 0 {
		desired_team = CTF_TEAM1
	} else if shared.Q_stricmp(t, "blue") == 0 {
		desired_team = CTF_TEAM2
	} else {
		g.cprintf(ent, PRINT_HIGH, "Unknown team %s.\n", t)
		return
	}

	if ent.Client.Resp.CtfTeam == desired_team {
		g.cprintf(ent, PRINT_HIGH, "You are already on the %s team.\n",
			CTFTeamName(ent.Client.Resp.CtfTeam))
		return
	}

	////
	ent.SVFlags = 0
	ent.Flags &^= FL_GODMODE
	ent.Client.Resp.CtfTeam = desired_team
	ent.Client.Resp.CtfState = 0
	s := shared.Info_ValueForKey(ent.Client.Pers.Userinfo, "skin")
	g.CTFAssignSkin(ent, s)

	if ent.Solid == SOLID_NOT { // spectator
		g.PutClientInServer(ent)
		// add a teleportation effect
		ent.S.Event = EV_PLAYER_TELEPORT
		// hold in place briefly
		ent.Client.PS.PMove.PmFlags = PMF_TIME_TELEPORT
		ent.Client.PS.PMove.PmTime = 14
		g.bprintf(PRINT_HIGH, "%s joined the %s team.\n",
			ent.Client.Pers.Netname, CTFTeamName(desired_team))
		return
	}

	ent.Health = 0
	g.player_die(ent, ent, ent, 100000, shared.Vec3Origin)
	// don't even bother waiting for death frames
	ent.Deadflag = DEAD_DEAD
	g.respawn(ent)

	ent.Client.Resp.Score = 0

	g.bprintf(PRINT_HIGH, "%s changed to the %s team.\n",
		ent.Client.Pers.Netname, CTFTeamName(desired_team))
}

// ctfRoom is the C test `maxsize - len > strlen(entry)`: an int compared
// with a size_t, so a negative left side is a huge unsigned value (true).
func ctfRoom(maxsize, l int, entry string) bool {
	d := maxsize - l
	return d < 0 || d > len(entry)
}

// C: ctf/g_ctf.c:1588 CTFScoreboardMessage
func (g *Game) CTFScoreboardMessage(ent, killer *Edict) {
	var entry string
	var str string
	var sorted [2][MAX_CLIENTS]int
	var sortedscores [2][MAX_CLIENTS]int32
	var total, totalscore [2]int32
	var last [2]int32
	var team int
	maxsize := 1000

	f1, f2 := ITEM_INDEX(g.ctfg.flag1_item), ITEM_INDEX(g.ctfg.flag2_item)

	// sort the clients by team and score
	for i := 0; i < int(g.game.Maxclients); i++ {
		cl_ent := &g.edicts[1+i]
		if !cl_ent.InUse {
			continue
		}
		if g.game.Clients[i].Resp.CtfTeam == CTF_TEAM1 {
			team = 0
		} else if g.game.Clients[i].Resp.CtfTeam == CTF_TEAM2 {
			team = 1
		} else {
			continue // unknown team?
		}

		score := g.game.Clients[i].Resp.Score
		var j int32
		for j = 0; j < total[team]; j++ {
			if score > sortedscores[team][j] {
				break
			}
		}
		for k := total[team]; k > j; k-- {
			sorted[team][k] = sorted[team][k-1]
			sortedscores[team][k] = sortedscores[team][k-1]
		}
		sorted[team][j] = i
		sortedscores[team][j] = score
		totalscore[team] += score
		total[team]++
	}

	// print level name and exit rules
	// add the clients in sorted order

	// team one
	str = fmt.Sprintf("if 24 xv 8 yv 8 pic 24 endif "+
		"xv 40 yv 28 string \"%4d/%-3d\" "+
		"xv 98 yv 12 num 2 18 "+
		"if 25 xv 168 yv 8 pic 25 endif "+
		"xv 200 yv 28 string \"%4d/%-3d\" "+
		"xv 256 yv 12 num 2 20 ",
		totalscore[0], total[0],
		totalscore[1], total[1])
	l := len(str)

	pingOf := func(cl *GClient) int32 {
		if cl.Ping > 999 {
			return 999
		}
		return cl.Ping
	}

	for i := int32(0); i < 16; i++ {
		if i >= total[0] && i >= total[1] {
			break // we're done
		}

		entry = ""

		// left side
		if i < total[0] {
			cl := &g.game.Clients[sorted[0][i]]
			cl_ent := &g.edicts[1+sorted[0][i]]

			entry += fmt.Sprintf("ctf 0 %d %d %d %d ",
				42+i*8,
				sorted[0][i],
				cl.Resp.Score,
				pingOf(cl))

			if cl_ent.Client.Pers.Inventory[f2] != 0 {
				entry += fmt.Sprintf("xv 56 yv %d picn sbfctf2 ",
					42+i*8)
			}

			if ctfRoom(maxsize, l, entry) {
				str += entry
				l = len(str)
				last[0] = i
			}
		}

		// right side
		if i < total[1] {
			cl := &g.game.Clients[sorted[1][i]]
			cl_ent := &g.edicts[1+sorted[1][i]]

			entry += fmt.Sprintf("ctf 160 %d %d %d %d ",
				42+i*8,
				sorted[1][i],
				cl.Resp.Score,
				pingOf(cl))

			if cl_ent.Client.Pers.Inventory[f1] != 0 {
				entry += fmt.Sprintf("xv 216 yv %d picn sbfctf1 ",
					42+i*8)
			}
			if ctfRoom(maxsize, l, entry) {
				str += entry
				l = len(str)
				last[1] = i
			}
		}
	}

	// put in spectators if we have enough room
	var j int32
	if last[0] > last[1] {
		j = last[0]
	} else {
		j = last[1]
	}
	j = (j+2)*8 + 42

	k, n := 0, 0
	if maxsize-l > 50 {
		for i := 0; float32(i) < g.maxclients.Value; i++ {
			cl_ent := &g.edicts[1+i]
			cl := &g.game.Clients[i]
			if !cl_ent.InUse ||
				cl_ent.Solid != SOLID_NOT ||
				cl_ent.Client.Resp.CtfTeam != CTF_NOTEAM {
				continue
			}

			if k == 0 {
				k = 1
				entry = fmt.Sprintf("xv 0 yv %d string2 \"Spectators\" ", j)
				str += entry
				l = len(str)
				j += 8
			}

			x := 0
			if n&1 != 0 {
				x = 160
			}
			entry += fmt.Sprintf("ctf %d %d %d %d %d ",
				x, // x
				j, // y
				i, // playernum
				cl.Resp.Score,
				pingOf(cl))
			if ctfRoom(maxsize, l, entry) {
				str += entry
				l = len(str)
			}

			if n&1 != 0 {
				j += 8
			}
			n++
		}
	}

	if total[0]-last[0] > 1 { // couldn't fit everyone
		str += fmt.Sprintf("xv 8 yv %d string \"..and %d more\" ",
			42+(last[0]+1)*8, total[0]-last[0]-1)
	}
	if total[1]-last[1] > 1 { // couldn't fit everyone
		str += fmt.Sprintf("xv 168 yv %d string \"..and %d more\" ",
			42+(last[1]+1)*8, total[1]-last[1]-1)
	}

	g.gi.WriteByteC(svc_layout)
	g.gi.WriteString(str)
}

/*------------------------------------------------------------------------*/
/* TECH																	  */
/*------------------------------------------------------------------------*/

// C: ctf/g_ctf.c:1798 CTFHasTech
func (g *Game) CTFHasTech(who *Edict) {
	if g.level.Time-who.Client.CtfLasttechmsg > 2 {
		g.gi.Centerprintf(who, "You already have a TECH powerup.")
		who.Client.CtfLasttechmsg = g.level.Time
	}
}

// C: ctf/g_ctf.c:1806 CTFWhat_Tech
func (g *Game) CTFWhat_Tech(ent *Edict) *GItem {
	for _, tn := range tnames {
		if tech := g.FindItemByClassname(tn); tech != nil &&
			ent.Client.Pers.Inventory[ITEM_INDEX(tech)] != 0 {
			return tech
		}
	}
	return nil
}

// C: ctf/g_ctf.c:1822 CTFPickup_Tech
func (g *Game) CTFPickup_Tech(ent, other *Edict) bool {
	for _, tn := range tnames {
		if tech := g.FindItemByClassname(tn); tech != nil &&
			other.Client.Pers.Inventory[ITEM_INDEX(tech)] != 0 {
			g.CTFHasTech(other)
			return false // has this one
		}
	}

	// client only gets one tech
	other.Client.Pers.Inventory[ITEM_INDEX(ent.Item)]++
	other.Client.CtfRegentime = g.level.Time
	return true
}

// C: ctf/g_ctf.c:1845 FindTechSpawn
func (g *Game) FindTechSpawn() *Edict {
	var spot *Edict
	i := g.rng.Rand() % 16

	for ; i > 0; i-- {
		spot = g.G_Find(spot, FOFS_classname, "info_player_deathmatch")
	}
	if spot == nil {
		spot = g.G_Find(spot, FOFS_classname, "info_player_deathmatch")
	}
	return spot
}

// C: ctf/g_ctf.c:1857 TechThink
func (g *Game) TechThink(tech *Edict) {
	if spot := g.FindTechSpawn(); spot != nil {
		g.SpawnTech(tech.Item, spot)
		g.G_FreeEdict(tech)
	} else {
		tech.Nextthink = g.level.Time + CTF_TECH_TIMEOUT
		tech.Think = TechThink
	}
}

// C: ctf/g_ctf.c:1870 CTFDrop_Tech
func (g *Game) CTFDrop_Tech(ent *Edict, item *GItem) {
	tech := g.Drop_Item(ent, item)
	tech.Nextthink = g.level.Time + CTF_TECH_TIMEOUT
	tech.Think = TechThink
	ent.Client.Pers.Inventory[ITEM_INDEX(item)] = 0
}

// C: ctf/g_ctf.c:1880 CTFDeadDropTech
func (g *Game) CTFDeadDropTech(ent *Edict) {
	for _, tn := range tnames {
		if tech := g.FindItemByClassname(tn); tech != nil &&
			ent.Client.Pers.Inventory[ITEM_INDEX(tech)] != 0 {
			dropped := g.Drop_Item(ent, tech)
			// hack the velocity to make it bounce random
			dropped.Velocity[0] = float32((g.rng.Rand() % 600) - 300)
			dropped.Velocity[1] = float32((g.rng.Rand() % 600) - 300)
			dropped.Nextthink = g.level.Time + CTF_TECH_TIMEOUT
			dropped.Think = TechThink
			dropped.Owner = nil
			ent.Client.Pers.Inventory[ITEM_INDEX(tech)] = 0
		}
	}
}

// C: ctf/g_ctf.c:1903 SpawnTech
func (g *Game) SpawnTech(item *GItem, spot *Edict) {
	var forward, right Vec3
	var angles Vec3

	ent := g.G_Spawn()

	ent.Classname = item.Classname
	ent.Item = item
	ent.Spawnflags = DROPPED_ITEM
	ent.S.Effects = uint32(item.WorldModelFlags)
	ent.S.RenderFX = RF_GLOW
	ent.Mins = Vec3{-15, -15, -15}
	ent.Maxs = Vec3{15, 15, 15}
	g.gi.SetModel(ent, ent.Item.WorldModel)
	ent.Solid = SOLID_TRIGGER
	ent.Movetype = MOVETYPE_TOSS
	ent.Touch = Touch_Item
	ent.Owner = ent

	angles[0] = 0
	angles[1] = float32(g.rng.Rand() % 360)
	angles[2] = 0

	shared.AngleVectors(angles, &forward, &right, nil)
	ent.S.Origin = spot.S.Origin
	ent.S.Origin[2] += 16
	ent.Velocity = shared.VectorScale(forward, 100)
	ent.Velocity[2] = 300

	ent.Nextthink = g.level.Time + CTF_TECH_TIMEOUT
	ent.Think = TechThink

	g.gi.LinkEntity(ent)
}

// SpawnTechs is a think function (ent may be NULL when called directly).
// C: ctf/g_ctf.c:1940 SpawnTechs
func (g *Game) SpawnTechs(ent *Edict) {
	for _, tn := range tnames {
		if tech := g.FindItemByClassname(tn); tech != nil {
			if spot := g.FindTechSpawn(); spot != nil {
				g.SpawnTech(tech, spot)
			}
		}
	}
	if ent != nil {
		g.G_FreeEdict(ent)
	}
}

// CTFRespawnTech frees the passed edict!
// C: ctf/g_ctf.c:1958 CTFRespawnTech
func (g *Game) CTFRespawnTech(ent *Edict) {
	if spot := g.FindTechSpawn(); spot != nil {
		g.SpawnTech(ent.Item, spot)
	}
	g.G_FreeEdict(ent)
}

// C: ctf/g_ctf.c:1967 CTFSetupTechSpawn
func (g *Game) CTFSetupTechSpawn() {
	if int32(g.dmflags.Value)&DF_CTF_NO_TECH != 0 {
		return
	}

	ent := g.G_Spawn()
	ent.Nextthink = g.level.Time + 2
	ent.Think = SpawnTechs
}

// C: ctf/g_ctf.c:1979 CTFResetTech
func (g *Game) CTFResetTech() {
	for i := 1; i < int(g.num_edicts); i++ {
		ent := &g.edicts[i]
		if ent.InUse {
			if ent.Item != nil && ent.Item.Flags&IT_TECH != 0 {
				g.G_FreeEdict(ent)
			}
		}
	}
	g.SpawnTechs(nil)
}

// ctfHasTech reports whether ent is a client holding the tech classname.
func (g *Game) ctfHasTechItem(ent *Edict, classname string) bool {
	if !g.ctfmod {
		return false
	}
	tech := g.FindItemByClassname(classname)
	return tech != nil && ent.Client != nil && ent.Client.Pers.Inventory[ITEM_INDEX(tech)] != 0
}

// C: ctf/g_ctf.c:1992 CTFApplyResistance
func (g *Game) CTFApplyResistance(ent *Edict, dmg int32) int32 {
	var volume float32 = 1.0

	if ent.Client != nil && ent.Client.SilencerShots != 0 {
		volume = 0.2
	}

	if dmg != 0 && g.ctfHasTechItem(ent, "item_tech1") {
		// make noise
		g.gi.Sound(ent, CHAN_VOICE, g.gi.SoundIndex("ctf/tech1.wav"), volume, ATTN_NORM, 0)
		return dmg / 2
	}
	return dmg
}

// C: ctf/g_ctf.c:2010 CTFApplyStrength
func (g *Game) CTFApplyStrength(ent *Edict, dmg int32) int32 {
	if dmg != 0 && g.ctfHasTechItem(ent, "item_tech2") {
		return dmg * 2
	}
	return dmg
}

// C: ctf/g_ctf.c:2022 CTFApplyStrengthSound
func (g *Game) CTFApplyStrengthSound(ent *Edict) bool {
	var volume float32 = 1.0

	if ent.Client != nil && ent.Client.SilencerShots != 0 {
		volume = 0.2
	}

	if g.ctfHasTechItem(ent, "item_tech2") {
		if ent.Client.CtfTechsndtime < g.level.Time {
			ent.Client.CtfTechsndtime = g.level.Time + 1
			if ent.Client.QuadFramenum > float32(g.level.Framenum) {
				g.gi.Sound(ent, CHAN_VOICE, g.gi.SoundIndex("ctf/tech2x.wav"), volume, ATTN_NORM, 0)
			} else {
				g.gi.Sound(ent, CHAN_VOICE, g.gi.SoundIndex("ctf/tech2.wav"), volume, ATTN_NORM, 0)
			}
		}
		return true
	}
	return false
}

// C: ctf/g_ctf.c:2047 CTFApplyHaste
func (g *Game) CTFApplyHaste(ent *Edict) bool {
	return g.ctfHasTechItem(ent, "item_tech3")
}

// C: ctf/g_ctf.c:2059 CTFApplyHasteSound
func (g *Game) CTFApplyHasteSound(ent *Edict) {
	var volume float32 = 1.0

	if ent.Client != nil && ent.Client.SilencerShots != 0 {
		volume = 0.2
	}

	if g.ctfHasTechItem(ent, "item_tech3") &&
		ent.Client.CtfTechsndtime < g.level.Time {
		ent.Client.CtfTechsndtime = g.level.Time + 1
		g.gi.Sound(ent, CHAN_VOICE, g.gi.SoundIndex("ctf/tech3.wav"), volume, ATTN_NORM, 0)
	}
}

// C: ctf/g_ctf.c:2077 CTFApplyRegeneration
func (g *Game) CTFApplyRegeneration(ent *Edict) {
	noise := false
	var volume float32 = 1.0

	client := ent.Client
	if client == nil {
		return
	}

	if ent.Client.SilencerShots != 0 {
		volume = 0.2
	}

	if g.ctfHasTechItem(ent, "item_tech4") {
		if client.CtfRegentime < g.level.Time {
			client.CtfRegentime = g.level.Time
			if ent.Health < 150 {
				ent.Health += 5
				if ent.Health > 150 {
					ent.Health = 150
				}
				client.CtfRegentime = float32(float64(client.CtfRegentime) + 0.5)
				noise = true
			}
			index := g.ArmorIndex(ent)
			if index != 0 && client.Pers.Inventory[index] < 150 {
				client.Pers.Inventory[index] += 5
				if client.Pers.Inventory[index] > 150 {
					client.Pers.Inventory[index] = 150
				}
				client.CtfRegentime = float32(float64(client.CtfRegentime) + 0.5)
				noise = true
			}
		}
		if noise && ent.Client.CtfTechsndtime < g.level.Time {
			ent.Client.CtfTechsndtime = g.level.Time + 1
			g.gi.Sound(ent, CHAN_VOICE, g.gi.SoundIndex("ctf/tech4.wav"), volume, ATTN_NORM, 0)
		}
	}
}

// C: ctf/g_ctf.c:2120 CTFHasRegeneration
func (g *Game) CTFHasRegeneration(ent *Edict) bool {
	return g.ctfHasTechItem(ent, "item_tech4")
}

/*
======================================================================

SAY_TEAM

======================================================================
*/

// This array is in 'importance order', it indicates what items are
// more important when reporting their names.
// C: ctf/g_ctf.c:2142 loc_names
var loc_names = []struct {
	classname string
	priority  int32
}{
	{"item_flag_team1", 1},
	{"item_flag_team2", 1},
	{"item_quad", 2},
	{"item_invulnerability", 2},
	{"weapon_bfg", 3},
	{"weapon_railgun", 4},
	{"weapon_rocketlauncher", 4},
	{"weapon_hyperblaster", 4},
	{"weapon_chaingun", 4},
	{"weapon_grenadelauncher", 4},
	{"weapon_machinegun", 4},
	{"weapon_supershotgun", 4},
	{"weapon_shotgun", 4},
	{"item_power_screen", 5},
	{"item_power_shield", 5},
	{"item_armor_body", 6},
	{"item_armor_combat", 6},
	{"item_armor_jacket", 6},
	{"item_silencer", 7},
	{"item_breather", 7},
	{"item_enviro", 7},
	{"item_adrenaline", 7},
	{"item_bandolier", 8},
	{"item_pack", 8},
}

// C: ctf/g_ctf.c:2175 CTFSay_Team_Location
func (g *Game) CTFSay_Team_Location(who *Edict) string {
	var what, hot *Edict
	var hotdist, newdist float32 = 999999, 0
	var v Vec3
	var hotindex int32 = 999
	var nearteam int32 = -1
	hotsee := false
	var buf string

	for {
		what = g.loc_findradius(what, who.S.Origin, 1024)
		if what == nil {
			break
		}
		// find what in loc_classnames
		i := 0
		for i = 0; i < len(loc_names); i++ {
			if what.Classname == loc_names[i].classname {
				break
			}
		}
		if i == len(loc_names) {
			continue
		}
		// something we can see get priority over something we can't
		cansee := g.loc_CanSee(what, who)
		if cansee && !hotsee {
			hotsee = true
			hotindex = loc_names[i].priority
			hot = what
			v = shared.VectorSubtract(what.S.Origin, who.S.Origin)
			hotdist = shared.VectorLength(v)
			continue
		}
		// if we can't see this, but we have something we can see, skip it
		if hotsee && !cansee {
			continue
		}
		if hotsee && hotindex < loc_names[i].priority {
			continue
		}
		v = shared.VectorSubtract(what.S.Origin, who.S.Origin)
		newdist = shared.VectorLength(v)
		if newdist < hotdist ||
			(cansee && loc_names[i].priority < hotindex) {
			hot = what
			hotdist = newdist
			hotindex = int32(i)
			hotsee = g.loc_CanSee(hot, who)
		}
	}

	if hot == nil {
		return "nowhere"
	}

	// we now have the closest item
	// see if there's more than one in the map, if so
	// we need to determine what team is closest
	what = nil
	for {
		what = g.G_Find(what, FOFS_classname, hot.Classname)
		if what == nil {
			break
		}
		if what == hot {
			continue
		}
		// if we are here, there is more than one, find out if hot
		// is closer to red flag or blue flag
		flag1 := g.G_Find(nil, FOFS_classname, "item_flag_team1")
		if flag1 != nil {
			flag2 := g.G_Find(nil, FOFS_classname, "item_flag_team2")
			if flag2 != nil {
				v = shared.VectorSubtract(hot.S.Origin, flag1.S.Origin)
				hotdist = shared.VectorLength(v)
				v = shared.VectorSubtract(hot.S.Origin, flag2.S.Origin)
				newdist = shared.VectorLength(v)
				if hotdist < newdist {
					nearteam = CTF_TEAM1
				} else if hotdist > newdist {
					nearteam = CTF_TEAM2
				}
			}
		}
		break
	}

	item := g.FindItemByClassname(hot.Classname)
	if item == nil {
		return "nowhere"
	}

	// in water?
	if who.Waterlevel != 0 {
		buf = "in the water "
	} else {
		buf = ""
	}

	// near or above
	v = shared.VectorSubtract(who.S.Origin, hot.S.Origin)
	if math.Abs(float64(v[2])) > math.Abs(float64(v[0])) && math.Abs(float64(v[2])) > math.Abs(float64(v[1])) {
		if v[2] > 0 {
			buf += "above "
		} else {
			buf += "below "
		}
	} else {
		buf += "near "
	}

	if nearteam == CTF_TEAM1 {
		buf += "the red "
	} else if nearteam == CTF_TEAM2 {
		buf += "the blue "
	} else {
		buf += "the "
	}

	buf += item.PickupName
	return buf
}

// C: ctf/g_ctf.c:2281 CTFSay_Team_Armor
func (g *Game) CTFSay_Team_Armor(who *Edict) string {
	buf := ""

	power_armor_type := g.PowerArmorType(who)
	if power_armor_type != 0 {
		cells := who.Client.Pers.Inventory[ITEM_INDEX(g.FindItem("cells"))]
		if cells != 0 {
			name := "Power Shield"
			if power_armor_type == POWER_ARMOR_SCREEN {
				name = "Power Screen"
			}
			buf += fmt.Sprintf("%s with %d cells ", name, cells)
		}
	}

	index := g.ArmorIndex(who)
	if index != 0 {
		item := g.GetItemByIndex(index)
		if item != nil {
			if buf != "" {
				buf += "and "
			}
			buf += fmt.Sprintf("%d units of %s",
				who.Client.Pers.Inventory[index], item.PickupName)
		}
	}

	if buf == "" {
		buf = "no armor"
	}
	return buf
}

// C: ctf/g_ctf.c:2315 CTFSay_Team_Health
func (g *Game) CTFSay_Team_Health(who *Edict) string {
	if who.Health <= 0 {
		return "dead"
	}
	return fmt.Sprintf("%d health", who.Health)
}

// C: ctf/g_ctf.c:2323 CTFSay_Team_Tech
func (g *Game) CTFSay_Team_Tech(who *Edict) string {
	// see if the player has a tech powerup
	for _, tn := range tnames {
		if tech := g.FindItemByClassname(tn); tech != nil &&
			who.Client.Pers.Inventory[ITEM_INDEX(tech)] != 0 {
			return fmt.Sprintf("the %s", tech.PickupName)
		}
	}
	return "no powerup"
}

// C: ctf/g_ctf.c:2341 CTFSay_Team_Weapon
func (g *Game) CTFSay_Team_Weapon(who *Edict) string {
	if who.Client.Pers.Weapon != nil {
		return who.Client.Pers.Weapon.PickupName
	}
	return "none"
}

// C: ctf/g_ctf.c:2349 CTFSay_Team_Sight
func (g *Game) CTFSay_Team_Sight(who *Edict) string {
	n := 0
	s, s2 := "", ""

	for i := 1; float32(i) <= g.maxclients.Value; i++ {
		targ := &g.edicts[i]
		if !targ.InUse ||
			targ == who ||
			!g.loc_CanSee(targ, who) {
			continue
		}
		if s2 != "" {
			if len(s)+len(s2)+3 < 1024 {
				if n != 0 {
					s += ", "
				}
				s += s2
				s2 = ""
			}
			n++
		}
		s2 = targ.Client.Pers.Netname
	}
	if s2 != "" {
		if len(s)+len(s2)+6 < 1024 {
			if n != 0 {
				s += " and "
			}
			s += s2
		}
		return s
	}
	return "no one"
}

// C: ctf/g_ctf.c:2386 CTFSay_Team
func (g *Game) CTFSay_Team(who *Edict, msg string) {
	if g.CheckFlood(who) {
		return
	}

	if strings.HasPrefix(msg, "\"") {
		msg = msg[:len(msg)-1]
		if len(msg) > 0 {
			msg = msg[1:]
		}
	}

	out := make([]byte, 0, 1024)
	for i := 0; i < len(msg) && len(out) < 1024-1; i++ {
		if msg[i] == '%' {
			i++
			if i >= len(msg) {
				break // C: '%' at the end copies the terminating NUL
			}
			switch msg[i] {
			case 'l', 'L':
				out = append(out, g.CTFSay_Team_Location(who)...)
			case 'a', 'A':
				out = append(out, g.CTFSay_Team_Armor(who)...)
			case 'h', 'H':
				out = append(out, g.CTFSay_Team_Health(who)...)
			case 't', 'T':
				out = append(out, g.CTFSay_Team_Tech(who)...)
			case 'w', 'W':
				out = append(out, g.CTFSay_Team_Weapon(who)...)
			case 'n', 'N':
				out = append(out, g.CTFSay_Team_Sight(who)...)
			default:
				out = append(out, msg[i])
			}
		} else {
			out = append(out, msg[i])
		}
	}
	outmsg := string(out)

	for i := 0; float32(i) < g.maxclients.Value; i++ {
		cl_ent := &g.edicts[1+i]
		if !cl_ent.InUse {
			continue
		}
		if cl_ent.Client.Resp.CtfTeam == who.Client.Resp.CtfTeam {
			g.cprintf(cl_ent, PRINT_CHAT, "(%s): %s\n",
				who.Client.Pers.Netname, outmsg)
		}
	}
}

/*-----------------------------------------------------------------------*/
/*QUAKED misc_ctf_banner (1 .5 0) (-4 -64 0) (4 64 248) TEAM2
The origin is the bottom of the banner.
The banner is 248 tall.
*/
// C: ctf/g_ctf.c:2468 misc_ctf_banner_think
func (g *Game) misc_ctf_banner_think(ent *Edict) {
	ent.S.Frame = (ent.S.Frame + 1) % 16
	ent.Nextthink = float32(float64(g.level.Time) + FRAMETIME)
}

// C: ctf/g_ctf.c:2474 SP_misc_ctf_banner
func (g *Game) SP_misc_ctf_banner(ent *Edict) {
	ent.Movetype = MOVETYPE_NONE
	ent.Solid = SOLID_NOT
	ent.S.ModelIndex = int32(g.gi.ModelIndex("models/ctf/banner/tris.md2"))
	if ent.Spawnflags&1 != 0 { // team2
		ent.S.SkinNum = 1
	}

	ent.S.Frame = g.rng.Rand() % 16
	g.gi.LinkEntity(ent)

	ent.Think = misc_ctf_banner_think
	ent.Nextthink = float32(float64(g.level.Time) + FRAMETIME)
}

/*QUAKED misc_ctf_small_banner (1 .5 0) (-4 -32 0) (4 32 124) TEAM2
The origin is the bottom of the banner.
The banner is 124 tall.
*/
// C: ctf/g_ctf.c:2493 SP_misc_ctf_small_banner
func (g *Game) SP_misc_ctf_small_banner(ent *Edict) {
	ent.Movetype = MOVETYPE_NONE
	ent.Solid = SOLID_NOT
	ent.S.ModelIndex = int32(g.gi.ModelIndex("models/ctf/banner/small.md2"))
	if ent.Spawnflags&1 != 0 { // team2
		ent.S.SkinNum = 1
	}

	ent.S.Frame = g.rng.Rand() % 16
	g.gi.LinkEntity(ent)

	ent.Think = misc_ctf_banner_think
	ent.Nextthink = float32(float64(g.level.Time) + FRAMETIME)
}

/*-----------------------------------------------------------------------*/

// SetLevelName sets the menu entry to "*" + the level name (static char
// levelname[33] in C).
// C: ctf/g_ctf.c:2510 SetLevelName
func (g *Game) SetLevelName(p *PMenu) {
	name := g.edicts[0].Message
	if name == "" {
		name = g.level.Mapname
	}
	if len(name) > 31 {
		name = name[:31]
	}
	p.Text = "*" + name
}

/*-----------------------------------------------------------------------*/

/* ELECTIONS */

// C: ctf/g_ctf.c:2529 CTFBeginElection
func (g *Game) CTFBeginElection(ent *Edict, typ int32, msg string) bool {
	cg := &g.ctfg.ctfgame

	if g.ctfg.electpercentage.Value == 0 {
		g.gi.Cprintf(ent, PRINT_HIGH, "Elections are disabled, only an admin can process this action.\n")
		return false
	}

	if cg.election != ELECT_NONE {
		g.gi.Cprintf(ent, PRINT_HIGH, "Election already in progress.\n")
		return false
	}

	// clear votes
	count := 0
	for i := 1; float32(i) <= g.maxclients.Value; i++ {
		e := &g.edicts[i]
		e.Client.Resp.Voted = false
		if e.InUse {
			count++
		}
	}

	if count < 2 {
		g.gi.Cprintf(ent, PRINT_HIGH, "Not enough players for election.\n")
		return false
	}

	cg.etarget = ent
	cg.election = typ
	cg.evotes = 0
	cg.needvotes = int32((float32(count) * g.ctfg.electpercentage.Value) / 100)
	cg.electtime = g.level.Time + 20 // twenty seconds for election
	cg.emsg = msg
	if len(cg.emsg) > 255 {
		cg.emsg = cg.emsg[:255]
	}

	// tell everyone
	g.bprintf(PRINT_CHAT, "%s\n", cg.emsg)
	g.gi.Bprintf(PRINT_HIGH, "Type YES or NO to vote on this request.\n")
	g.bprintf(PRINT_HIGH, "Votes: %d  Needed: %d  Time left: %ds\n", cg.evotes, cg.needvotes,
		int32(cg.electtime-g.level.Time))

	return true
}

// C: ctf/g_ctf.c:2578 CTFResetAllPlayers
func (g *Game) CTFResetAllPlayers() {
	for i := 1; float32(i) <= g.maxclients.Value; i++ {
		ent := &g.edicts[i]
		if !ent.InUse {
			continue
		}

		if ent.Client.Menu != nil {
			g.PMenu_Close(ent)
		}

		g.CTFPlayerResetGrapple(ent)
		g.CTFDeadDropFlag(ent)
		g.CTFDeadDropTech(ent)

		ent.Client.Resp.CtfTeam = CTF_NOTEAM
		ent.Client.Resp.Ready = false

		ent.SVFlags = 0
		ent.Flags &^= FL_GODMODE
		g.PutClientInServer(ent)
	}

	// reset the level
	g.CTFResetTech()
	g.CTFResetFlags()

	for i := 1; i < int(g.num_edicts); i++ {
		ent := &g.edicts[i]
		if ent.InUse && ent.Client == nil {
			if ent.Solid == SOLID_NOT && ent.Think == DoRespawn &&
				ent.Nextthink >= g.level.Time {
				ent.Nextthink = 0
				g.DoRespawn(ent)
			}
		}
	}
	if g.ctfg.ctfgame.match == MATCH_SETUP {
		g.ctfg.ctfgame.matchtime = g.level.Time + g.ctfg.matchsetuptime.Value*60
	}
}

// C: ctf/g_ctf.c:2620 CTFAssignGhost
func (g *Game) CTFAssignGhost(ent *Edict) {
	cg := &g.ctfg.ctfgame
	var ghost int

	for ghost = 0; ghost < MAX_CLIENTS; ghost++ {
		if cg.ghosts[ghost].Code == 0 {
			break
		}
	}
	if ghost == MAX_CLIENTS {
		return
	}
	cg.ghosts[ghost].Team = ent.Client.Resp.CtfTeam
	cg.ghosts[ghost].Score = 0
	for {
		cg.ghosts[ghost].Code = 10000 + (g.rng.Rand() % 90000)
		i := 0
		for i = 0; i < MAX_CLIENTS; i++ {
			if i != ghost && cg.ghosts[i].Code == cg.ghosts[ghost].Code {
				break
			}
		}
		if i == MAX_CLIENTS {
			break
		}
	}
	cg.ghosts[ghost].Ent = ent
	cg.ghosts[ghost].Netname = ent.Client.Pers.Netname
	ent.Client.Resp.Ghost = &cg.ghosts[ghost]
	g.cprintf(ent, PRINT_CHAT, "Your ghost code is **** %d ****\n", cg.ghosts[ghost].Code)
	g.cprintf(ent, PRINT_HIGH, "If you lose connection, you can rejoin with your score "+
		"intact by typing \"ghost %d\".\n", cg.ghosts[ghost].Code)
}

// CTFStartMatch starts a match.
// C: ctf/g_ctf.c:2648 CTFStartMatch
func (g *Game) CTFStartMatch() {
	cg := &g.ctfg.ctfgame

	cg.match = MATCH_GAME
	cg.matchtime = g.level.Time + g.ctfg.matchtime.Value*60

	cg.team1 = 0
	cg.team2 = 0

	cg.ghosts = [MAX_CLIENTS]Ghost{}

	for i := 1; float32(i) <= g.maxclients.Value; i++ {
		ent := &g.edicts[i]
		if !ent.InUse {
			continue
		}

		ent.Client.Resp.Score = 0
		ent.Client.Resp.CtfState = 0
		ent.Client.Resp.Ghost = nil

		g.gi.Centerprintf(ent, "******************\n\nMATCH HAS STARTED!\n\n******************")

		if ent.Client.Resp.CtfTeam != CTF_NOTEAM {
			// make up a ghost code
			g.CTFAssignGhost(ent)
			g.CTFPlayerResetGrapple(ent)
			ent.SVFlags = SVF_NOCLIENT
			ent.Flags &^= FL_GODMODE

			ent.Client.RespawnTime = float32(float64(g.level.Time) + 1.0 + (float64(g.rng.Rand()%30) / 10.0))
			ent.Client.PS.PMove.PmType = PM_DEAD
			ent.Client.AnimPriority = ANIM_DEATH
			ent.S.Frame = FRAME_death308 - 1
			ent.Client.AnimEnd = FRAME_death308
			ent.Deadflag = DEAD_DEAD
			ent.Movetype = MOVETYPE_NOCLIP
			ent.Client.PS.GunIndex = 0
			g.gi.LinkEntity(ent)
		}
	}
}

// C: ctf/g_ctf.c:2692 CTFEndMatch
func (g *Game) CTFEndMatch() {
	cg := &g.ctfg.ctfgame

	cg.match = MATCH_POST
	g.gi.Bprintf(PRINT_CHAT, "MATCH COMPLETED!\n")

	g.CTFCalcScores()

	g.bprintf(PRINT_HIGH, "RED TEAM:  %d captures, %d points\n",
		cg.team1, cg.total1)
	g.bprintf(PRINT_HIGH, "BLUE TEAM:  %d captures, %d points\n",
		cg.team2, cg.total2)

	if cg.team1 > cg.team2 {
		g.bprintf(PRINT_CHAT, "RED team won over the BLUE team by %d CAPTURES!\n",
			cg.team1-cg.team2)
	} else if cg.team2 > cg.team1 {
		g.bprintf(PRINT_CHAT, "BLUE team won over the RED team by %d CAPTURES!\n",
			cg.team2-cg.team1)
	} else if cg.total1 > cg.total2 { // frag tie breaker
		g.bprintf(PRINT_CHAT, "RED team won over the BLUE team by %d POINTS!\n",
			cg.total1-cg.total2)
	} else if cg.total2 > cg.total1 {
		g.bprintf(PRINT_CHAT, "BLUE team won over the RED team by %d POINTS!\n",
			cg.total2-cg.total1)
	} else {
		g.gi.Bprintf(PRINT_CHAT, "TIE GAME!\n")
	}

	g.EndDMLevel()
}

// C: ctf/g_ctf.c:2722 CTFNextMap
func (g *Game) CTFNextMap() bool {
	if g.ctfg.ctfgame.match == MATCH_POST {
		g.ctfg.ctfgame.match = MATCH_SETUP
		g.CTFResetAllPlayers()
		return true
	}
	return false
}

// C: ctf/g_ctf.c:2732 CTFWinElection
func (g *Game) CTFWinElection() {
	cg := &g.ctfg.ctfgame
	switch cg.election {
	case ELECT_MATCH:
		// reset into match mode
		if g.ctfg.competition.Value < 3 {
			g.gi.CvarSet("competition", "2")
		}
		cg.match = MATCH_SETUP
		g.CTFResetAllPlayers()

	case ELECT_ADMIN:
		cg.etarget.Client.Resp.Admin = true
		g.bprintf(PRINT_HIGH, "%s has become an admin.\n", cg.etarget.Client.Pers.Netname)
		g.gi.Cprintf(cg.etarget, PRINT_HIGH, "Type 'admin' to access the adminstration menu.\n")

	case ELECT_MAP:
		g.bprintf(PRINT_HIGH, "%s is warping to level %s.\n",
			cg.etarget.Client.Pers.Netname, cg.elevel)
		g.level.Forcemap = cg.elevel
		if len(g.level.Forcemap) > MAX_QPATH-1 {
			g.level.Forcemap = g.level.Forcemap[:MAX_QPATH-1]
		}
		g.EndDMLevel()
	}
	cg.election = ELECT_NONE
}

// C: ctf/g_ctf.c:2759 CTFVoteYes
func (g *Game) CTFVoteYes(ent *Edict) {
	cg := &g.ctfg.ctfgame
	if cg.election == ELECT_NONE {
		g.gi.Cprintf(ent, PRINT_HIGH, "No election is in progress.\n")
		return
	}
	if ent.Client.Resp.Voted {
		g.gi.Cprintf(ent, PRINT_HIGH, "You already voted.\n")
		return
	}
	if cg.etarget == ent {
		g.gi.Cprintf(ent, PRINT_HIGH, "You can't vote for yourself.\n")
		return
	}

	ent.Client.Resp.Voted = true

	cg.evotes++
	if cg.evotes == cg.needvotes {
		// the election has been won
		g.CTFWinElection()
		return
	}
	g.bprintf(PRINT_HIGH, "%s\n", cg.emsg)
	g.bprintf(PRINT_CHAT, "Votes: %d  Needed: %d  Time left: %ds\n", cg.evotes, cg.needvotes,
		int32(cg.electtime-g.level.Time))
}

// C: ctf/g_ctf.c:2787 CTFVoteNo
func (g *Game) CTFVoteNo(ent *Edict) {
	cg := &g.ctfg.ctfgame
	if cg.election == ELECT_NONE {
		g.gi.Cprintf(ent, PRINT_HIGH, "No election is in progress.\n")
		return
	}
	if ent.Client.Resp.Voted {
		g.gi.Cprintf(ent, PRINT_HIGH, "You already voted.\n")
		return
	}
	if cg.etarget == ent {
		g.gi.Cprintf(ent, PRINT_HIGH, "You can't vote for yourself.\n")
		return
	}

	ent.Client.Resp.Voted = true

	g.bprintf(PRINT_HIGH, "%s\n", cg.emsg)
	g.bprintf(PRINT_CHAT, "Votes: %d  Needed: %d  Time left: %ds\n", cg.evotes, cg.needvotes,
		int32(cg.electtime-g.level.Time))
}

// C: ctf/g_ctf.c:2809 CTFReady
func (g *Game) CTFReady(ent *Edict) {
	cg := &g.ctfg.ctfgame

	if ent.Client.Resp.CtfTeam == CTF_NOTEAM {
		g.gi.Cprintf(ent, PRINT_HIGH, "Pick a team first (hit <TAB> for menu)\n")
		return
	}

	if cg.match != MATCH_SETUP {
		g.gi.Cprintf(ent, PRINT_HIGH, "A match is not being setup.\n")
		return
	}

	if ent.Client.Resp.Ready {
		g.gi.Cprintf(ent, PRINT_HIGH, "You have already commited.\n")
		return
	}

	ent.Client.Resp.Ready = true
	g.bprintf(PRINT_HIGH, "%s is ready.\n", ent.Client.Pers.Netname)

	t1, t2, j := 0, 0, 0
	for i := 1; float32(i) <= g.maxclients.Value; i++ {
		e := &g.edicts[i]
		if !e.InUse {
			continue
		}
		if e.Client.Resp.CtfTeam != CTF_NOTEAM && !e.Client.Resp.Ready {
			j++
		}
		if e.Client.Resp.CtfTeam == CTF_TEAM1 {
			t1++
		} else if e.Client.Resp.CtfTeam == CTF_TEAM2 {
			t2++
		}
	}
	if j == 0 && t1 != 0 && t2 != 0 {
		// everyone has commited
		g.gi.Bprintf(PRINT_CHAT, "All players have commited.  Match starting\n")
		cg.match = MATCH_PREGAME
		cg.matchtime = g.level.Time + g.ctfg.matchstarttime.Value
	}
}

// C: ctf/g_ctf.c:2853 CTFNotReady
func (g *Game) CTFNotReady(ent *Edict) {
	cg := &g.ctfg.ctfgame

	if ent.Client.Resp.CtfTeam == CTF_NOTEAM {
		g.gi.Cprintf(ent, PRINT_HIGH, "Pick a team first (hit <TAB> for menu)\n")
		return
	}

	if cg.match != MATCH_SETUP && cg.match != MATCH_PREGAME {
		g.gi.Cprintf(ent, PRINT_HIGH, "A match is not being setup.\n")
		return
	}

	if !ent.Client.Resp.Ready {
		g.gi.Cprintf(ent, PRINT_HIGH, "You haven't commited.\n")
		return
	}

	ent.Client.Resp.Ready = false
	g.bprintf(PRINT_HIGH, "%s is no longer ready.\n", ent.Client.Pers.Netname)

	if cg.match == MATCH_PREGAME {
		g.gi.Bprintf(PRINT_CHAT, "Match halted.\n")
		cg.match = MATCH_SETUP
		cg.matchtime = g.level.Time + g.ctfg.matchsetuptime.Value*60
	}
}

// C: ctf/g_ctf.c:2880 CTFGhost
func (g *Game) CTFGhost(ent *Edict) {
	cg := &g.ctfg.ctfgame

	if g.gi.Argc() < 2 {
		g.gi.Cprintf(ent, PRINT_HIGH, "Usage:  ghost <code>\n")
		return
	}

	if ent.Client.Resp.CtfTeam != CTF_NOTEAM {
		g.gi.Cprintf(ent, PRINT_HIGH, "You are already in the game.\n")
		return
	}
	if cg.match != MATCH_GAME {
		g.gi.Cprintf(ent, PRINT_HIGH, "No match is in progress.\n")
		return
	}

	n := shared.Atoi(g.gi.Argv(1))

	for i := 0; i < MAX_CLIENTS; i++ {
		if cg.ghosts[i].Code != 0 && cg.ghosts[i].Code == n {
			g.gi.Cprintf(ent, PRINT_HIGH, "Ghost code accepted, your position has been reinstated.\n")
			cg.ghosts[i].Ent.Client.Resp.Ghost = nil
			ent.Client.Resp.CtfTeam = cg.ghosts[i].Team
			ent.Client.Resp.Ghost = &cg.ghosts[i]
			ent.Client.Resp.Score = cg.ghosts[i].Score
			ent.Client.Resp.CtfState = 0
			cg.ghosts[i].Ent = ent
			ent.SVFlags = 0
			ent.Flags &^= FL_GODMODE
			g.PutClientInServer(ent)
			g.bprintf(PRINT_HIGH, "%s has been reinstated to %s team.\n",
				ent.Client.Pers.Netname, CTFTeamName(ent.Client.Resp.CtfTeam))
			return
		}
	}
	g.gi.Cprintf(ent, PRINT_HIGH, "Invalid ghost code.\n")
}

// C: ctf/g_ctf.c:2921 CTFMatchSetup
func (g *Game) CTFMatchSetup() bool {
	if !g.ctfmod {
		return false
	}
	m := g.ctfg.ctfgame.match
	return m == MATCH_SETUP || m == MATCH_PREGAME
}

// C: ctf/g_ctf.c:2928 CTFMatchOn
func (g *Game) CTFMatchOn() bool {
	return g.ctfmod && g.ctfg.ctfgame.match == MATCH_GAME
}

/*-----------------------------------------------------------------------*/

// C: ctf/g_ctf.c:2944 creditsmenu
var creditsmenu = []PMenu{
	{"*Quake II", PMENU_ALIGN_CENTER, nil},
	{"*ThreeWave Capture the Flag", PMENU_ALIGN_CENTER, nil},
	{"", PMENU_ALIGN_CENTER, nil},
	{"*Programming", PMENU_ALIGN_CENTER, nil},
	{"Dave 'Zoid' Kirsch", PMENU_ALIGN_CENTER, nil},
	{"*Level Design", PMENU_ALIGN_CENTER, nil},
	{"Christian Antkow", PMENU_ALIGN_CENTER, nil},
	{"Tim Willits", PMENU_ALIGN_CENTER, nil},
	{"Dave 'Zoid' Kirsch", PMENU_ALIGN_CENTER, nil},
	{"*Art", PMENU_ALIGN_CENTER, nil},
	{"Adrian Carmack Paul Steed", PMENU_ALIGN_CENTER, nil},
	{"Kevin Cloud", PMENU_ALIGN_CENTER, nil},
	{"*Sound", PMENU_ALIGN_CENTER, nil},
	{"Tom 'Bjorn' Klok", PMENU_ALIGN_CENTER, nil},
	{"*Original CTF Art Design", PMENU_ALIGN_CENTER, nil},
	{"Brian 'Whaleboy' Cozzens", PMENU_ALIGN_CENTER, nil},
	{"", PMENU_ALIGN_CENTER, nil},
	{"Return to Main Menu", PMENU_ALIGN_LEFT, (*Game).CTFReturnToMain},
}

// C: ctf/g_ctf.c:2965
const (
	jmenu_level    = 2
	jmenu_match    = 3
	jmenu_red      = 5
	jmenu_blue     = 7
	jmenu_chase    = 9
	jmenu_reqmatch = 11
)

// C: ctf/g_ctf.c:2972 joinmenu
var joinmenuInit = []PMenu{
	{"*Quake II", PMENU_ALIGN_CENTER, nil},
	{"*ThreeWave Capture the Flag", PMENU_ALIGN_CENTER, nil},
	{"", PMENU_ALIGN_CENTER, nil},
	{"", PMENU_ALIGN_CENTER, nil},
	{"", PMENU_ALIGN_CENTER, nil},
	{"Join Red Team", PMENU_ALIGN_LEFT, (*Game).CTFJoinTeam1},
	{"", PMENU_ALIGN_LEFT, nil},
	{"Join Blue Team", PMENU_ALIGN_LEFT, (*Game).CTFJoinTeam2},
	{"", PMENU_ALIGN_LEFT, nil},
	{"Chase Camera", PMENU_ALIGN_LEFT, (*Game).CTFChaseCam},
	{"Credits", PMENU_ALIGN_LEFT, (*Game).CTFCredits},
	{"", PMENU_ALIGN_LEFT, nil},
	{"", PMENU_ALIGN_LEFT, nil},
	{"Use [ and ] to move cursor", PMENU_ALIGN_LEFT, nil},
	{"ENTER to select", PMENU_ALIGN_LEFT, nil},
	{"ESC to Exit Menu", PMENU_ALIGN_LEFT, nil},
	{"(TAB to Return)", PMENU_ALIGN_LEFT, nil},
	{"v" + CTF_STRING_VERSION, PMENU_ALIGN_RIGHT, nil},
}

// C: ctf/g_ctf.c:2993 nochasemenu
var nochasemenuInit = []PMenu{
	{"*Quake II", PMENU_ALIGN_CENTER, nil},
	{"*ThreeWave Capture the Flag", PMENU_ALIGN_CENTER, nil},
	{"", PMENU_ALIGN_CENTER, nil},
	{"", PMENU_ALIGN_CENTER, nil},
	{"No one to chase", PMENU_ALIGN_LEFT, nil},
	{"", PMENU_ALIGN_CENTER, nil},
	{"Return to Main Menu", PMENU_ALIGN_LEFT, (*Game).CTFReturnToMain},
}

// C: ctf/g_ctf.c:3003 CTFJoinTeam
func (g *Game) CTFJoinTeam(ent *Edict, desired_team int32) {
	cg := &g.ctfg.ctfgame

	g.PMenu_Close(ent)

	ent.SVFlags &^= SVF_NOCLIENT
	ent.Client.Resp.CtfTeam = desired_team
	ent.Client.Resp.CtfState = 0
	s := shared.Info_ValueForKey(ent.Client.Pers.Userinfo, "skin")
	g.CTFAssignSkin(ent, s)

	// assign a ghost if we are in match mode
	if cg.match == MATCH_GAME {
		if ent.Client.Resp.Ghost != nil {
			ent.Client.Resp.Ghost.Code = 0
		}
		ent.Client.Resp.Ghost = nil
		g.CTFAssignGhost(ent)
	}

	g.PutClientInServer(ent)
	// add a teleportation effect
	ent.S.Event = EV_PLAYER_TELEPORT
	// hold in place briefly
	ent.Client.PS.PMove.PmFlags = PMF_TIME_TELEPORT
	ent.Client.PS.PMove.PmTime = 14
	g.bprintf(PRINT_HIGH, "%s joined the %s team.\n",
		ent.Client.Pers.Netname, CTFTeamName(desired_team))

	if cg.match == MATCH_SETUP {
		g.gi.Centerprintf(ent, "***********************\n"+
			"Type \"ready\" in console\n"+
			"to ready up.\n"+
			"***********************")
	}
}

// C: ctf/g_ctf.c:3040 CTFJoinTeam1
func (g *Game) CTFJoinTeam1(ent *Edict, p *PMenuHnd) {
	g.CTFJoinTeam(ent, CTF_TEAM1)
}

// C: ctf/g_ctf.c:3045 CTFJoinTeam2
func (g *Game) CTFJoinTeam2(ent *Edict, p *PMenuHnd) {
	g.CTFJoinTeam(ent, CTF_TEAM2)
}

// C: ctf/g_ctf.c:3050 CTFChaseCam
func (g *Game) CTFChaseCam(ent *Edict, p *PMenuHnd) {
	if ent.Client.ChaseTarget != nil {
		ent.Client.ChaseTarget = nil
		g.PMenu_Close(ent)
		return
	}

	for i := 1; float32(i) <= g.maxclients.Value; i++ {
		e := &g.edicts[i]
		if e.InUse && e.Solid != SOLID_NOT {
			ent.Client.ChaseTarget = e
			g.PMenu_Close(ent)
			ent.Client.UpdateChase = true
			return
		}
	}

	g.SetLevelName(&g.ctfg.nochasemenu[jmenu_level])

	g.PMenu_Close(ent)
	g.PMenu_Open(ent, g.ctfg.nochasemenu, -1, int32(len(g.ctfg.nochasemenu)), nil)
}

// C: ctf/g_ctf.c:3077 CTFReturnToMain
func (g *Game) CTFReturnToMain(ent *Edict, p *PMenuHnd) {
	g.PMenu_Close(ent)
	g.CTFOpenJoinMenu(ent)
}

// C: ctf/g_ctf.c:3083 CTFRequestMatch
func (g *Game) CTFRequestMatch(ent *Edict, p *PMenuHnd) {
	g.PMenu_Close(ent)

	text := fmt.Sprintf("%s has requested to switch to competition mode.",
		ent.Client.Pers.Netname)
	g.CTFBeginElection(ent, ELECT_MATCH, text)
}

// C: ctf/g_ctf.c:3096 CTFShowScores
func (g *Game) CTFShowScores(ent *Edict, p *PMenu) {
	g.PMenu_Close(ent)

	ent.Client.Showscores = true
	ent.Client.Showinventory = false
	g.DeathmatchScoreboard(ent)
}

// C: ctf/g_ctf.c:3105 CTFUpdateJoinMenu
func (g *Game) CTFUpdateJoinMenu(ent *Edict) int32 {
	cg := &g.ctfg.ctfgame
	joinmenu := g.ctfg.joinmenu

	if cg.match >= MATCH_PREGAME && g.ctfg.matchlock.Value != 0 {
		joinmenu[jmenu_red].Text = "MATCH IS LOCKED"
		joinmenu[jmenu_red].SelectFunc = nil
		joinmenu[jmenu_blue].Text = "  (entry is not permitted)"
		joinmenu[jmenu_blue].SelectFunc = nil
	} else {
		if cg.match >= MATCH_PREGAME {
			joinmenu[jmenu_red].Text = "Join Red MATCH Team"
			joinmenu[jmenu_blue].Text = "Join Blue MATCH Team"
		} else {
			joinmenu[jmenu_red].Text = "Join Red Team"
			joinmenu[jmenu_blue].Text = "Join Blue Team"
		}
		joinmenu[jmenu_red].SelectFunc = (*Game).CTFJoinTeam1
		joinmenu[jmenu_blue].SelectFunc = (*Game).CTFJoinTeam2
	}

	if fj := g.ctfg.ctf_forcejoin.String; fj != "" {
		if shared.Q_stricmp(fj, "red") == 0 {
			joinmenu[jmenu_blue].Text = ""
			joinmenu[jmenu_blue].SelectFunc = nil
		} else if shared.Q_stricmp(fj, "blue") == 0 {
			joinmenu[jmenu_red].Text = ""
			joinmenu[jmenu_red].SelectFunc = nil
		}
	}

	if ent.Client.ChaseTarget != nil {
		joinmenu[jmenu_chase].Text = "Leave Chase Camera"
	} else {
		joinmenu[jmenu_chase].Text = "Chase Camera"
	}

	g.SetLevelName(&joinmenu[jmenu_level])

	num1, num2 := 0, 0
	for i := 0; float32(i) < g.maxclients.Value; i++ {
		if !g.edicts[i+1].InUse {
			continue
		}
		if g.game.Clients[i].Resp.CtfTeam == CTF_TEAM1 {
			num1++
		} else if g.game.Clients[i].Resp.CtfTeam == CTF_TEAM2 {
			num2++
		}
	}

	team1players := fmt.Sprintf("  (%d players)", num1)
	team2players := fmt.Sprintf("  (%d players)", num2)

	switch cg.match {
	case MATCH_NONE:
		joinmenu[jmenu_match].Text = ""
	case MATCH_SETUP:
		joinmenu[jmenu_match].Text = "*MATCH SETUP IN PROGRESS"
	case MATCH_PREGAME:
		joinmenu[jmenu_match].Text = "*MATCH STARTING"
	case MATCH_GAME:
		joinmenu[jmenu_match].Text = "*MATCH IN PROGRESS"
	}

	if joinmenu[jmenu_red].Text != "" {
		joinmenu[jmenu_red+1].Text = team1players
	} else {
		joinmenu[jmenu_red+1].Text = ""
	}
	if joinmenu[jmenu_blue].Text != "" {
		joinmenu[jmenu_blue+1].Text = team2players
	} else {
		joinmenu[jmenu_blue+1].Text = ""
	}

	joinmenu[jmenu_reqmatch].Text = ""
	joinmenu[jmenu_reqmatch].SelectFunc = nil
	if g.ctfg.competition.Value != 0 && cg.match < MATCH_SETUP {
		joinmenu[jmenu_reqmatch].Text = "Request Match"
		joinmenu[jmenu_reqmatch].SelectFunc = (*Game).CTFRequestMatch
	}

	if num1 > num2 {
		return CTF_TEAM1
	} else if num2 > num1 {
		return CTF_TEAM2
	}
	if g.rng.Rand()&1 != 0 {
		return CTF_TEAM1
	}
	return CTF_TEAM2
}

// C: ctf/g_ctf.c:3199 CTFOpenJoinMenu
func (g *Game) CTFOpenJoinMenu(ent *Edict) {
	team := g.CTFUpdateJoinMenu(ent)
	if ent.Client.ChaseTarget != nil {
		team = 8
	} else if team == CTF_TEAM1 {
		team = 4
	} else {
		team = 6
	}
	g.PMenu_Open(ent, g.ctfg.joinmenu, team, int32(len(g.ctfg.joinmenu)), nil)
}

// C: ctf/g_ctf.c:3213 CTFCredits
func (g *Game) CTFCredits(ent *Edict, p *PMenuHnd) {
	g.PMenu_Close(ent)
	g.PMenu_Open(ent, creditsmenu, -1, int32(len(creditsmenu)), nil)
}

// C: ctf/g_ctf.c:3219 CTFStartClient
func (g *Game) CTFStartClient(ent *Edict) bool {
	if ent.Client.Resp.CtfTeam != CTF_NOTEAM {
		return false
	}

	if int32(g.dmflags.Value)&DF_CTF_FORCEJOIN == 0 || g.ctfg.ctfgame.match >= MATCH_SETUP {
		// start as 'observer'
		ent.Movetype = MOVETYPE_NOCLIP
		ent.Solid = SOLID_NOT
		ent.SVFlags |= SVF_NOCLIENT
		ent.Client.Resp.CtfTeam = CTF_NOTEAM
		ent.Client.PS.GunIndex = 0
		g.gi.LinkEntity(ent)

		g.CTFOpenJoinMenu(ent)
		return true
	}
	return false
}

// C: ctf/g_ctf.c:3239 CTFObserver
func (g *Game) CTFObserver(ent *Edict) {
	// start as 'observer'
	if ent.Movetype == MOVETYPE_NOCLIP {
		g.gi.Cprintf(ent, PRINT_HIGH, "You are already an observer.\n")
		return
	}

	g.CTFPlayerResetGrapple(ent)
	g.CTFDeadDropFlag(ent)
	g.CTFDeadDropTech(ent)

	ent.Movetype = MOVETYPE_NOCLIP
	ent.Solid = SOLID_NOT
	ent.SVFlags |= SVF_NOCLIENT
	ent.Client.Resp.CtfTeam = CTF_NOTEAM
	ent.Client.PS.GunIndex = 0
	ent.Client.Resp.Score = 0
	g.gi.LinkEntity(ent)
	g.CTFOpenJoinMenu(ent)
}

// C: ctf/g_ctf.c:3261 CTFInMatch
func (g *Game) CTFInMatch() bool {
	return g.ctfmod && g.ctfg.ctfgame.match > MATCH_NONE
}

// C: ctf/g_ctf.c:3268 CTFCheckRules
func (g *Game) CTFCheckRules() bool {
	cg := &g.ctfg.ctfgame

	if cg.election != ELECT_NONE && cg.electtime <= g.level.Time {
		g.gi.Bprintf(PRINT_CHAT, "Election timed out and has been cancelled.\n")
		cg.election = ELECT_NONE
	}

	if cg.match != MATCH_NONE {
		t := int32(cg.matchtime - g.level.Time)

		if t <= 0 { // time ended on something
			switch cg.match {
			case MATCH_SETUP:
				// go back to normal mode
				if g.ctfg.competition.Value < 3 {
					cg.match = MATCH_NONE
					g.gi.CvarSet("competition", "1")
					g.CTFResetAllPlayers()
				} else {
					// reset the time
					cg.matchtime = g.level.Time + g.ctfg.matchsetuptime.Value*60
				}
				return false

			case MATCH_PREGAME:
				// match started!
				g.CTFStartMatch()
				return false

			case MATCH_GAME:
				// match ended!
				g.CTFEndMatch()
				return false
			}
		}

		if t == cg.lasttime {
			return false
		}

		cg.lasttime = t

		var text string
		switch cg.match {
		case MATCH_SETUP:
			j := 0
			for i := 1; float32(i) <= g.maxclients.Value; i++ {
				ent := &g.edicts[i]
				if !ent.InUse {
					continue
				}
				if ent.Client.Resp.CtfTeam != CTF_NOTEAM &&
					!ent.Client.Resp.Ready {
					j++
				}
			}

			if g.ctfg.competition.Value < 3 {
				text = fmt.Sprintf("%02d:%02d SETUP: %d not ready",
					t/60, t%60, j)
			} else {
				text = fmt.Sprintf("SETUP: %d not ready", j)
			}

			g.gi.Configstring(CONFIG_CTF_MATCH, text)

		case MATCH_PREGAME:
			text = fmt.Sprintf("%02d:%02d UNTIL START",
				t/60, t%60)
			g.gi.Configstring(CONFIG_CTF_MATCH, text)

		case MATCH_GAME:
			text = fmt.Sprintf("%02d:%02d MATCH",
				t/60, t%60)
			g.gi.Configstring(CONFIG_CTF_MATCH, text)
		}
		return false
	}

	if g.capturelimit.Value != 0 &&
		(float32(cg.team1) >= g.capturelimit.Value ||
			float32(cg.team2) >= g.capturelimit.Value) {
		g.gi.Bprintf(PRINT_HIGH, "Capturelimit hit.\n")
		return true
	}
	return false
}

/*--------------------------------------------------------------------------
 * just here to help old map conversions
 *--------------------------------------------------------------------------*/

// C: ctf/g_ctf.c:3363 old_teleporter_touch
func (g *Game) old_teleporter_touch(self, other *Edict, plane *CPlane, surf *CSurface) {
	var forward Vec3

	if other.Client == nil {
		return
	}
	dest := g.G_Find(nil, FOFS_targetname, self.Target)
	if dest == nil {
		g.gi.Dprintf("Couldn't find destination\n")
		return
	}

	//ZOID
	g.CTFPlayerResetGrapple(other)
	//ZOID

	// unlink to make sure it can't possibly interfere with KillBox
	g.gi.UnlinkEntity(other)

	other.S.Origin = dest.S.Origin
	other.S.OldOrigin = dest.S.Origin
	//	other->s.origin[2] += 10;

	// clear the velocity and hold them in place briefly
	other.Velocity = Vec3{}
	other.Client.PS.PMove.PmTime = 160 >> 3 // hold time
	other.Client.PS.PMove.PmFlags |= PMF_TIME_TELEPORT

	// draw the teleport splash at source and on the player
	self.Enemy.S.Event = EV_PLAYER_TELEPORT
	other.S.Event = EV_PLAYER_TELEPORT

	// set angles
	for i := 0; i < 3; i++ {
		other.Client.PS.PMove.DeltaAngles[i] = int16(shared.ANGLE2SHORT(dest.S.Angles[i] - other.Client.Resp.CmdAngles[i]))
	}

	other.S.Angles[PITCH] = 0
	other.S.Angles[YAW] = dest.S.Angles[YAW]
	other.S.Angles[ROLL] = 0
	other.Client.PS.ViewAngles = dest.S.Angles
	other.Client.VAngle = dest.S.Angles

	// give a little forward velocity
	shared.AngleVectors(other.Client.VAngle, &forward, nil, nil)
	other.Velocity = shared.VectorScale(forward, 200)

	// kill anything at the destination
	if !g.KillBox(other) {
	}

	g.gi.LinkEntity(other)
}

/*QUAKED trigger_teleport (0.5 0.5 0.5) ?
Players touching this will be teleported
*/
// C: ctf/g_ctf.c:3423 SP_trigger_teleport
func (g *Game) SP_trigger_teleport(ent *Edict) {
	if ent.Target == "" {
		g.gi.Dprintf("teleporter without a target.\n")
		g.G_FreeEdict(ent)
		return
	}

	ent.SVFlags |= SVF_NOCLIENT
	ent.Solid = SOLID_TRIGGER
	ent.Touch = old_teleporter_touch
	g.gi.SetModel(ent, ent.Model)
	g.gi.LinkEntity(ent)

	// noise maker and splash effect dude
	s := g.G_Spawn()
	ent.Enemy = s
	for i := 0; i < 3; i++ {
		s.S.Origin[i] = ent.Mins[i] + (ent.Maxs[i]-ent.Mins[i])/2
	}
	s.S.Sound = int32(g.gi.SoundIndex("world/hum1.wav"))
	g.gi.LinkEntity(s)
}

/*QUAKED info_teleport_destination (0.5 0.5 0.5) (-16 -16 -24) (16 16 32)
Point trigger_teleports at these.
*/
// C: ctf/g_ctf.c:3454 SP_info_teleport_destination
func (g *Game) SP_info_teleport_destination(ent *Edict) {
	ent.S.Origin[2] += 16
}

/*----------------------------------------------------------------------------------*/
/* ADMIN */

// C: ctf/g_ctf.c:3462 admin_settings_t
type admin_settings_t struct {
	matchlen      int32
	matchsetuplen int32
	matchstartlen int32
	weaponsstay   bool
	instantitems  bool
	quaddrop      bool
	instantweap   bool
	matchlock     bool
}

// C: ctf/g_ctf.c:3478 CTFAdmin_SettingsApply
func (g *Game) CTFAdmin_SettingsApply(ent *Edict, p *PMenuHnd) {
	settings := p.Arg.(*admin_settings_t)
	cg := &g.ctfg.ctfgame
	c := &g.ctfg

	if float32(settings.matchlen) != c.matchtime.Value {
		g.bprintf(PRINT_HIGH, "%s changed the match length to %d minutes.\n",
			ent.Client.Pers.Netname, settings.matchlen)
		if cg.match == MATCH_GAME {
			// in the middle of a match, change it on the fly
			cg.matchtime = (cg.matchtime - c.matchtime.Value*60) + float32(settings.matchlen*60)
		}
		g.gi.CvarSet("matchtime", fmt.Sprintf("%d", settings.matchlen))
	}

	if float32(settings.matchsetuplen) != c.matchsetuptime.Value {
		g.bprintf(PRINT_HIGH, "%s changed the match setup time to %d minutes.\n",
			ent.Client.Pers.Netname, settings.matchsetuplen)
		if cg.match == MATCH_SETUP {
			// in the middle of a match, change it on the fly
			cg.matchtime = (cg.matchtime - c.matchsetuptime.Value*60) + float32(settings.matchsetuplen*60)
		}
		g.gi.CvarSet("matchsetuptime", fmt.Sprintf("%d", settings.matchsetuplen))
	}

	if float32(settings.matchstartlen) != c.matchstarttime.Value {
		g.bprintf(PRINT_HIGH, "%s changed the match start time to %d seconds.\n",
			ent.Client.Pers.Netname, settings.matchstartlen)
		if cg.match == MATCH_PREGAME {
			// in the middle of a match, change it on the fly
			cg.matchtime = (cg.matchtime - c.matchstarttime.Value) + float32(settings.matchstartlen)
		}
		g.gi.CvarSet("matchstarttime", fmt.Sprintf("%d", settings.matchstartlen))
	}

	onoff := func(b bool) string {
		if b {
			return "on"
		}
		return "off"
	}

	if settings.weaponsstay != (int32(g.dmflags.Value)&DF_WEAPONS_STAY != 0) {
		g.bprintf(PRINT_HIGH, "%s turned %s weapons stay.\n",
			ent.Client.Pers.Netname, onoff(settings.weaponsstay))
		i := int32(g.dmflags.Value)
		if settings.weaponsstay {
			i |= DF_WEAPONS_STAY
		} else {
			i &^= DF_WEAPONS_STAY
		}
		g.gi.CvarSet("dmflags", fmt.Sprintf("%d", i))
	}

	if settings.instantitems != (int32(g.dmflags.Value)&DF_INSTANT_ITEMS != 0) {
		g.bprintf(PRINT_HIGH, "%s turned %s instant items.\n",
			ent.Client.Pers.Netname, onoff(settings.instantitems))
		i := int32(g.dmflags.Value)
		if settings.instantitems {
			i |= DF_INSTANT_ITEMS
		} else {
			i &^= DF_INSTANT_ITEMS
		}
		g.gi.CvarSet("dmflags", fmt.Sprintf("%d", i))
	}

	if settings.quaddrop != (int32(g.dmflags.Value)&DF_QUAD_DROP != 0) {
		g.bprintf(PRINT_HIGH, "%s turned %s quad drop.\n",
			ent.Client.Pers.Netname, onoff(settings.quaddrop))
		i := int32(g.dmflags.Value)
		if settings.quaddrop {
			i |= DF_QUAD_DROP
		} else {
			i &^= DF_QUAD_DROP
		}
		g.gi.CvarSet("dmflags", fmt.Sprintf("%d", i))
	}

	if settings.instantweap != (int32(g.instantweap.Value) != 0) {
		g.bprintf(PRINT_HIGH, "%s turned %s instant weapons.\n",
			ent.Client.Pers.Netname, onoff(settings.instantweap))
		g.gi.CvarSet("instantweap", fmt.Sprintf("%d", b2i(settings.instantweap)))
	}

	if settings.matchlock != (int32(c.matchlock.Value) != 0) {
		g.bprintf(PRINT_HIGH, "%s turned %s match lock.\n",
			ent.Client.Pers.Netname, onoff(settings.matchlock))
		g.gi.CvarSet("matchlock", fmt.Sprintf("%d", b2i(settings.matchlock)))
	}

	g.PMenu_Close(ent)
	g.CTFOpenAdminMenu(ent)
}

func b2i(b bool) int32 {
	if b {
		return 1
	}
	return 0
}

// C: ctf/g_ctf.c:3571 CTFAdmin_SettingsCancel
func (g *Game) CTFAdmin_SettingsCancel(ent *Edict, p *PMenuHnd) {
	g.PMenu_Close(ent)
	g.CTFOpenAdminMenu(ent)
}

// C: ctf/g_ctf.c:3579 CTFAdmin_ChangeMatchLen
func (g *Game) CTFAdmin_ChangeMatchLen(ent *Edict, p *PMenuHnd) {
	settings := p.Arg.(*admin_settings_t)

	settings.matchlen = (settings.matchlen % 60) + 5
	if settings.matchlen < 5 {
		settings.matchlen = 5
	}

	g.CTFAdmin_UpdateSettings(ent, p)
}

// C: ctf/g_ctf.c:3590 CTFAdmin_ChangeMatchSetupLen
func (g *Game) CTFAdmin_ChangeMatchSetupLen(ent *Edict, p *PMenuHnd) {
	settings := p.Arg.(*admin_settings_t)

	settings.matchsetuplen = (settings.matchsetuplen % 60) + 5
	if settings.matchsetuplen < 5 {
		settings.matchsetuplen = 5
	}

	g.CTFAdmin_UpdateSettings(ent, p)
}

// C: ctf/g_ctf.c:3601 CTFAdmin_ChangeMatchStartLen
func (g *Game) CTFAdmin_ChangeMatchStartLen(ent *Edict, p *PMenuHnd) {
	settings := p.Arg.(*admin_settings_t)

	settings.matchstartlen = (settings.matchstartlen % 600) + 10
	if settings.matchstartlen < 20 {
		settings.matchstartlen = 20
	}

	g.CTFAdmin_UpdateSettings(ent, p)
}

// C: ctf/g_ctf.c:3612 CTFAdmin_ChangeWeapStay
func (g *Game) CTFAdmin_ChangeWeapStay(ent *Edict, p *PMenuHnd) {
	settings := p.Arg.(*admin_settings_t)

	settings.weaponsstay = !settings.weaponsstay
	g.CTFAdmin_UpdateSettings(ent, p)
}

// C: ctf/g_ctf.c:3620 CTFAdmin_ChangeInstantItems
func (g *Game) CTFAdmin_ChangeInstantItems(ent *Edict, p *PMenuHnd) {
	settings := p.Arg.(*admin_settings_t)

	settings.instantitems = !settings.instantitems
	g.CTFAdmin_UpdateSettings(ent, p)
}

// C: ctf/g_ctf.c:3628 CTFAdmin_ChangeQuadDrop
func (g *Game) CTFAdmin_ChangeQuadDrop(ent *Edict, p *PMenuHnd) {
	settings := p.Arg.(*admin_settings_t)

	settings.quaddrop = !settings.quaddrop
	g.CTFAdmin_UpdateSettings(ent, p)
}

// C: ctf/g_ctf.c:3636 CTFAdmin_ChangeInstantWeap
func (g *Game) CTFAdmin_ChangeInstantWeap(ent *Edict, p *PMenuHnd) {
	settings := p.Arg.(*admin_settings_t)

	settings.instantweap = !settings.instantweap
	g.CTFAdmin_UpdateSettings(ent, p)
}

// C: ctf/g_ctf.c:3644 CTFAdmin_ChangeMatchLock
func (g *Game) CTFAdmin_ChangeMatchLock(ent *Edict, p *PMenuHnd) {
	settings := p.Arg.(*admin_settings_t)

	settings.matchlock = !settings.matchlock
	g.CTFAdmin_UpdateSettings(ent, p)
}

// C: ctf/g_ctf.c:3652 CTFAdmin_UpdateSettings
func (g *Game) CTFAdmin_UpdateSettings(ent *Edict, setmenu *PMenuHnd) {
	i := 2
	settings := setmenu.Arg.(*admin_settings_t)
	yesno := func(b bool) string {
		if b {
			return "Yes"
		}
		return "No"
	}

	PMenu_UpdateEntry(&setmenu.Entries[i], fmt.Sprintf("Match Len:       %2d mins", settings.matchlen), PMENU_ALIGN_LEFT, (*Game).CTFAdmin_ChangeMatchLen)
	i++

	PMenu_UpdateEntry(&setmenu.Entries[i], fmt.Sprintf("Match Setup Len: %2d mins", settings.matchsetuplen), PMENU_ALIGN_LEFT, (*Game).CTFAdmin_ChangeMatchSetupLen)
	i++

	PMenu_UpdateEntry(&setmenu.Entries[i], fmt.Sprintf("Match Start Len: %2d secs", settings.matchstartlen), PMENU_ALIGN_LEFT, (*Game).CTFAdmin_ChangeMatchStartLen)
	i++

	PMenu_UpdateEntry(&setmenu.Entries[i], fmt.Sprintf("Weapons Stay:    %s", yesno(settings.weaponsstay)), PMENU_ALIGN_LEFT, (*Game).CTFAdmin_ChangeWeapStay)
	i++

	PMenu_UpdateEntry(&setmenu.Entries[i], fmt.Sprintf("Instant Items:   %s", yesno(settings.instantitems)), PMENU_ALIGN_LEFT, (*Game).CTFAdmin_ChangeInstantItems)
	i++

	PMenu_UpdateEntry(&setmenu.Entries[i], fmt.Sprintf("Quad Drop:       %s", yesno(settings.quaddrop)), PMENU_ALIGN_LEFT, (*Game).CTFAdmin_ChangeQuadDrop)
	i++

	PMenu_UpdateEntry(&setmenu.Entries[i], fmt.Sprintf("Instant Weapons: %s", yesno(settings.instantweap)), PMENU_ALIGN_LEFT, (*Game).CTFAdmin_ChangeInstantWeap)
	i++

	PMenu_UpdateEntry(&setmenu.Entries[i], fmt.Sprintf("Match Lock:      %s", yesno(settings.matchlock)), PMENU_ALIGN_LEFT, (*Game).CTFAdmin_ChangeMatchLock)

	g.PMenu_Update(ent)
}

// C: ctf/g_ctf.c:3693 def_setmenu
var def_setmenu = []PMenu{
	{"*Settings Menu", PMENU_ALIGN_CENTER, nil},
	{"", PMENU_ALIGN_CENTER, nil},
	{"", PMENU_ALIGN_LEFT, nil}, //int matchlen;
	{"", PMENU_ALIGN_LEFT, nil}, //int matchsetuplen;
	{"", PMENU_ALIGN_LEFT, nil}, //int matchstartlen;
	{"", PMENU_ALIGN_LEFT, nil}, //qboolean weaponsstay;
	{"", PMENU_ALIGN_LEFT, nil}, //qboolean instantitems;
	{"", PMENU_ALIGN_LEFT, nil}, //qboolean quaddrop;
	{"", PMENU_ALIGN_LEFT, nil}, //qboolean instantweap;
	{"", PMENU_ALIGN_LEFT, nil}, //qboolean matchlock;
	{"", PMENU_ALIGN_LEFT, nil},
	{"Apply", PMENU_ALIGN_LEFT, (*Game).CTFAdmin_SettingsApply},
	{"Cancel", PMENU_ALIGN_LEFT, (*Game).CTFAdmin_SettingsCancel},
}

// C: ctf/g_ctf.c:3709 CTFAdmin_Settings
func (g *Game) CTFAdmin_Settings(ent *Edict, p *PMenuHnd) {
	g.PMenu_Close(ent)

	settings := &admin_settings_t{}

	settings.matchlen = int32(g.ctfg.matchtime.Value)
	settings.matchsetuplen = int32(g.ctfg.matchsetuptime.Value)
	settings.matchstartlen = int32(g.ctfg.matchstarttime.Value)
	settings.weaponsstay = int32(g.dmflags.Value)&DF_WEAPONS_STAY != 0
	settings.instantitems = int32(g.dmflags.Value)&DF_INSTANT_ITEMS != 0
	settings.quaddrop = int32(g.dmflags.Value)&DF_QUAD_DROP != 0
	settings.instantweap = g.instantweap.Value != 0
	settings.matchlock = g.ctfg.matchlock.Value != 0

	menu := g.PMenu_Open(ent, def_setmenu, -1, int32(len(def_setmenu)), settings)
	g.CTFAdmin_UpdateSettings(ent, menu)
}

// C: ctf/g_ctf.c:3731 CTFAdmin_MatchSet
func (g *Game) CTFAdmin_MatchSet(ent *Edict, p *PMenuHnd) {
	cg := &g.ctfg.ctfgame

	g.PMenu_Close(ent)

	if cg.match == MATCH_SETUP {
		g.gi.Bprintf(PRINT_CHAT, "Match has been forced to start.\n")
		cg.match = MATCH_PREGAME
		cg.matchtime = g.level.Time + g.ctfg.matchstarttime.Value
	} else if cg.match == MATCH_GAME {
		g.gi.Bprintf(PRINT_CHAT, "Match has been forced to terminate.\n")
		cg.match = MATCH_SETUP
		cg.matchtime = g.level.Time + g.ctfg.matchsetuptime.Value*60
		g.CTFResetAllPlayers()
	}
}

// C: ctf/g_ctf.c:3747 CTFAdmin_MatchMode
func (g *Game) CTFAdmin_MatchMode(ent *Edict, p *PMenuHnd) {
	cg := &g.ctfg.ctfgame

	g.PMenu_Close(ent)

	if cg.match != MATCH_SETUP {
		if g.ctfg.competition.Value < 3 {
			g.gi.CvarSet("competition", "2")
		}
		cg.match = MATCH_SETUP
		g.CTFResetAllPlayers()
	}
}

// C: ctf/g_ctf.c:3759 CTFAdmin_Cancel
func (g *Game) CTFAdmin_Cancel(ent *Edict, p *PMenuHnd) {
	g.PMenu_Close(ent)
}

// C: ctf/g_ctf.c:3765 adminmenu
var adminmenuInit = []PMenu{
	{"*Administration Menu", PMENU_ALIGN_CENTER, nil},
	{"", PMENU_ALIGN_CENTER, nil}, // blank
	{"Settings", PMENU_ALIGN_LEFT, (*Game).CTFAdmin_Settings},
	{"", PMENU_ALIGN_LEFT, nil},
	{"", PMENU_ALIGN_LEFT, nil},
	{"Cancel", PMENU_ALIGN_LEFT, (*Game).CTFAdmin_Cancel},
	{"", PMENU_ALIGN_CENTER, nil},
}

// C: ctf/g_ctf.c:3775 CTFOpenAdminMenu
func (g *Game) CTFOpenAdminMenu(ent *Edict) {
	adminmenu := g.ctfg.adminmenu
	cg := &g.ctfg.ctfgame

	adminmenu[3].Text = ""
	adminmenu[3].SelectFunc = nil
	if cg.match == MATCH_SETUP {
		adminmenu[3].Text = "Force start match"
		adminmenu[3].SelectFunc = (*Game).CTFAdmin_MatchSet
	} else if cg.match == MATCH_GAME {
		adminmenu[3].Text = "Cancel match"
		adminmenu[3].SelectFunc = (*Game).CTFAdmin_MatchSet
	} else if cg.match == MATCH_NONE && g.ctfg.competition.Value != 0 {
		adminmenu[3].Text = "Switch to match mode"
		adminmenu[3].SelectFunc = (*Game).CTFAdmin_MatchMode
	}

	//	if (ent->client->menu)
	//		PMenu_Close(ent->client->menu);

	g.PMenu_Open(ent, adminmenu, -1, int32(len(adminmenu)), nil)
}

// C: ctf/g_ctf.c:3796 CTFAdmin
func (g *Game) CTFAdmin(ent *Edict) {
	if g.gi.Argc() > 1 && g.ctfg.admin_password.String != "" &&
		!ent.Client.Resp.Admin && g.ctfg.admin_password.String == g.gi.Argv(1) {
		ent.Client.Resp.Admin = true
		g.bprintf(PRINT_HIGH, "%s has become an admin.\n", ent.Client.Pers.Netname)
		g.gi.Cprintf(ent, PRINT_HIGH, "Type 'admin' to access the adminstration menu.\n")
	}

	if !ent.Client.Resp.Admin {
		text := fmt.Sprintf("%s has requested admin rights.",
			ent.Client.Pers.Netname)
		g.CTFBeginElection(ent, ELECT_ADMIN, text)
		return
	}

	if ent.Client.Menu != nil {
		g.PMenu_Close(ent)
	}

	g.CTFOpenAdminMenu(ent)
}

/*----------------------------------------------------------------*/

// C: ctf/g_ctf.c:3822 CTFStats
func (g *Game) CTFStats(ent *Edict) {
	cg := &g.ctfg.ctfgame
	const textSize = 1400
	text := ""

	if cg.match == MATCH_SETUP {
		for i := 1; float32(i) <= g.maxclients.Value; i++ {
			e2 := &g.edicts[i]
			if !e2.InUse {
				continue
			}
			if !e2.Client.Resp.Ready && e2.Client.Resp.CtfTeam != CTF_NOTEAM {
				st := fmt.Sprintf("%s is not ready.\n", e2.Client.Pers.Netname)
				if len(text)+len(st) < textSize-50 {
					text += st
				}
			}
		}
	}

	i := 0
	for i = 0; i < MAX_CLIENTS; i++ {
		if cg.ghosts[i].Ent != nil {
			break
		}
	}

	if i == MAX_CLIENTS {
		if text != "" {
			g.gi.Cprintf(ent, PRINT_HIGH, text)
		}
		g.gi.Cprintf(ent, PRINT_HIGH, "No statistics available.\n")
		return
	}

	text += "  #|Name            |Score|Kills|Death|BasDf|CarDf|Effcy|\n"

	for i = 0; i < MAX_CLIENTS; i++ {
		gh := &cg.ghosts[i]
		if gh.Netname == "" {
			continue
		}

		var e int32
		if gh.Deaths+gh.Kills == 0 {
			e = 50
		} else {
			e = gh.Kills * 100 / (gh.Kills + gh.Deaths)
		}
		st := fmt.Sprintf("%3d|%s|%5d|%5d|%5d|%5d|%5d|%4d%%|\n",
			gh.Number,
			cLeftPrec(gh.Netname, 16, 16),
			gh.Score,
			gh.Kills,
			gh.Deaths,
			gh.Basedef,
			gh.Carrierdef,
			e)
		if len(st) > 79 { // char st[80]
			st = st[:79]
		}
		if len(text)+len(st) > textSize-50 {
			text += "And more...\n"
			g.gi.Cprintf(ent, PRINT_HIGH, text)
			return
		}
		text += st
	}
	g.gi.Cprintf(ent, PRINT_HIGH, text)
}

// cLeftPrec is C printf "%-<width>.<prec>s": truncate to prec bytes, pad
// with spaces to width bytes (Go's fmt counts runes, which differs for UTF-8
// netnames).
func cLeftPrec(s string, width, prec int) string {
	if len(s) > prec {
		s = s[:prec]
	}
	if len(s) < width {
		s += strings.Repeat(" ", width-len(s))
	}
	return s
}

// C: ctf/g_ctf.c:3884 CTFPlayerList
func (g *Game) CTFPlayerList(ent *Edict) {
	cg := &g.ctfg.ctfgame
	const textSize = 1400

	// (the "not ready" list is built and then discarded: *text = 0 below)

	// number, name, connect time, ping, score, admin

	text := ""
	for i := 0; float32(i) < g.maxclients.Value; i++ {
		e2 := &g.edicts[1+i]
		if !e2.InUse {
			continue
		}

		ready := ""
		if cg.match == MATCH_SETUP || cg.match == MATCH_PREGAME {
			if e2.Client.Resp.Ready {
				ready = " (ready)"
			} else {
				ready = " (notready)"
			}
		}
		admin := ""
		if e2.Client.Resp.Admin {
			admin = " (admin)"
		}
		st := fmt.Sprintf("%3d %s %02d:%02d %4d %3d%s%s\n",
			i+1,
			cLeftPrec(e2.Client.Pers.Netname, 16, 16),
			(g.level.Framenum-e2.Client.Resp.Enterframe)/600,
			((g.level.Framenum-e2.Client.Resp.Enterframe)%600)/10,
			e2.Client.Ping,
			e2.Client.Resp.Score,
			ready,
			admin)
		if len(text)+len(st) > textSize-50 {
			text += "And more...\n"
			g.gi.Cprintf(ent, PRINT_HIGH, text)
			return
		}
		text += st
	}
	g.gi.Cprintf(ent, PRINT_HIGH, text)
}

// C: ctf/g_ctf.c:3933 CTFWarp
func (g *Game) CTFWarp(ent *Edict) {
	const seps = " \t\n\r"

	if g.gi.Argc() < 2 {
		g.gi.Cprintf(ent, PRINT_HIGH, "Where do you want to warp to?\n")
		g.cprintf(ent, PRINT_HIGH, "Available levels are: %s\n", g.ctfg.warp_list.String)
		return
	}

	found := false
	for _, token := range strings.FieldsFunc(g.ctfg.warp_list.String, func(r rune) bool { return strings.ContainsRune(seps, r) }) {
		if shared.Q_stricmp(token, g.gi.Argv(1)) == 0 {
			found = true
			break
		}
	}

	if !found {
		g.gi.Cprintf(ent, PRINT_HIGH, "Unknown CTF level.\n")
		g.cprintf(ent, PRINT_HIGH, "Available levels are: %s\n", g.ctfg.warp_list.String)
		return
	}

	if ent.Client.Resp.Admin {
		g.bprintf(PRINT_HIGH, "%s is warping to level %s.\n",
			ent.Client.Pers.Netname, g.gi.Argv(1))
		g.level.Forcemap = g.gi.Argv(1)
		if len(g.level.Forcemap) > MAX_QPATH-1 {
			g.level.Forcemap = g.level.Forcemap[:MAX_QPATH-1]
		}
		g.EndDMLevel()
		return
	}

	text := fmt.Sprintf("%s has requested warping to level %s.",
		ent.Client.Pers.Netname, g.gi.Argv(1))
	if g.CTFBeginElection(ent, ELECT_MAP, text) {
		lvl := g.gi.Argv(1)
		if len(lvl) > 31 {
			lvl = lvl[:31]
		}
		g.ctfg.ctfgame.elevel = lvl
	}
}

// C: ctf/g_ctf.c:3978 CTFBoot
func (g *Game) CTFBoot(ent *Edict) {
	if !ent.Client.Resp.Admin {
		g.gi.Cprintf(ent, PRINT_HIGH, "You are not an admin.\n")
		return
	}

	if g.gi.Argc() < 2 {
		g.gi.Cprintf(ent, PRINT_HIGH, "Who do you want to kick?\n")
		return
	}

	a := g.gi.Argv(1)
	if len(a) > 0 && a[0] < '0' && a[0] > '9' { // never true (sic)
		g.gi.Cprintf(ent, PRINT_HIGH, "Specify the player number to kick.\n")
		return
	}

	i := shared.Atoi(a)
	if i < 1 || float32(i) > g.maxclients.Value {
		g.gi.Cprintf(ent, PRINT_HIGH, "Invalid player number.\n")
		return
	}

	targ := &g.edicts[i]
	if !targ.InUse {
		g.gi.Cprintf(ent, PRINT_HIGH, "That player number is not connected.\n")
		return
	}

	g.gi.AddCommandString(fmt.Sprintf("kick %d\n", i-1))
}

// ctfClientCommand is the //ZOID command block of ClientCommand; returns
// false when cmd is not a CTF command.
// C: ctf/g_cmds.c:1034 ClientCommand
func (g *Game) ctfClientCommand(ent *Edict, cmd string) bool {
	switch {
	case shared.Q_stricmp(cmd, "team") == 0:
		g.CTFTeam_f(ent)
	case shared.Q_stricmp(cmd, "id") == 0:
		g.CTFID_f(ent)
	case shared.Q_stricmp(cmd, "yes") == 0:
		g.CTFVoteYes(ent)
	case shared.Q_stricmp(cmd, "no") == 0:
		g.CTFVoteNo(ent)
	case shared.Q_stricmp(cmd, "ready") == 0:
		g.CTFReady(ent)
	case shared.Q_stricmp(cmd, "notready") == 0:
		g.CTFNotReady(ent)
	case shared.Q_stricmp(cmd, "ghost") == 0:
		g.CTFGhost(ent)
	case shared.Q_stricmp(cmd, "admin") == 0:
		g.CTFAdmin(ent)
	case shared.Q_stricmp(cmd, "stats") == 0:
		g.CTFStats(ent)
	case shared.Q_stricmp(cmd, "warp") == 0:
		g.CTFWarp(ent)
	case shared.Q_stricmp(cmd, "boot") == 0:
		g.CTFBoot(ent)
	case shared.Q_stricmp(cmd, "playerlist") == 0:
		g.CTFPlayerList(ent)
	case shared.Q_stricmp(cmd, "observer") == 0:
		g.CTFObserver(ent)
	default:
		return false
	}
	return true
}
