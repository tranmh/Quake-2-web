package game

import (
	"fmt"
	"strings"

	. "quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/crand"
	"quake2web/server/internal/qcommon/cvar"
	"quake2web/server/internal/qcommon/shared"
)

// Game holds everything the C game module keeps in globals. All game
// functions are methods on *Game. One Game is one game module instance.
// C: game/g_main.c:23 (globals)
type Game struct {
	gi  Import      // C: gi
	rng *crand.Rand // shared with the server: rand() of the process

	// callDepth is the nesting depth of the recursive entity callback
	// chains bounded by enterCall (not C state).
	callDepth int32

	game       GameLocals  // C: game
	level      LevelLocals // C: level
	st         SpawnTemp   // C: st
	edicts     []Edict     // C: g_edicts / globals.edicts
	num_edicts int32       // C: globals.num_edicts

	sm_meat_index int32
	snd_fry       int32
	meansOfDeath  int32

	// cvars (C: g_main.c:35)
	deathmatch         *cvar.Cvar
	coop               *cvar.Cvar
	dmflags            *cvar.Cvar
	skill              *cvar.Cvar
	fraglimit          *cvar.Cvar
	timelimit          *cvar.Cvar
	password           *cvar.Cvar
	spectator_password *cvar.Cvar
	maxclients         *cvar.Cvar
	maxspectators      *cvar.Cvar
	maxentities        *cvar.Cvar
	g_select_empty     *cvar.Cvar
	dedicated          *cvar.Cvar
	filterban          *cvar.Cvar
	sv_maxvelocity     *cvar.Cvar
	sv_gravity         *cvar.Cvar
	sv_rollspeed       *cvar.Cvar
	sv_rollangle       *cvar.Cvar
	gun_x              *cvar.Cvar
	gun_y              *cvar.Cvar
	gun_z              *cvar.Cvar
	run_pitch          *cvar.Cvar
	run_roll           *cvar.Cvar
	bob_up             *cvar.Cvar
	bob_pitch          *cvar.Cvar
	bob_roll           *cvar.Cvar
	sv_cheats          *cvar.Cvar
	flood_msgs         *cvar.Cvar
	flood_persecond    *cvar.Cvar
	flood_waitdelay    *cvar.Cvar
	sv_maplist         *cvar.Cvar

	// ctf module (ctf/g_main.c:41, //ZOID)
	capturelimit *cvar.Cvar
	instantweap  *cvar.Cvar

	// ctfmod is true when this instance runs as the ctf game module
	// (ctf/*.c, docs/CTF.md): the per-mode item table, spawn table and every
	// //ZOID hunk and base-vs-fork branch are selected by it. Within the ctf
	// module the C `ctf->value` checks use the ctf cvar (ctfOn()).
	ctfmod bool
	// moduleForced: ctfmod was chosen by NewModule, not detected at Init.
	moduleForced bool
	// itemlist is the item table of the running module (itemlist or
	// ctfItemlist, g_items.go / g_ctf.go).
	itemlist []GItem
	// ctfg holds the globals of ctf/g_ctf.c and ctf/p_menu.c.
	ctfg ctfGlobals

	// g_ai.c
	enemy_vis     bool
	enemy_infront bool
	enemy_range   int32
	enemy_yaw     float32

	// g_trigger.c
	windsound int32

	// g_svcmds.c
	ipfilters    []ipfilter_t
	numipfilters int32

	// p_client.c
	pm_passent *Edict

	// p_trail.c
	trail        [TRAIL_LENGTH]*Edict
	trail_head   int32
	trail_active bool

	// m_move.c
	c_yes, c_no int32

	// p_weapon.c
	is_quad     bool
	is_silenced byte

	// g_phys.c
	obstacle *Edict
	pushed   [MAX_EDICTS]pushed_t
	pushed_p int // index into pushed (C: pushed_t *pushed_p)

	// g_items.c
	jacket_armor_index     int32
	combat_armor_index     int32
	body_armor_index       int32
	power_screen_index     int32
	power_shield_index     int32
	quad_drop_timeout_hack int32

	// p_view.c
	current_player     *Edict
	current_client     *GClient
	forward, right, up Vec3
	xyspeed            float32
	bobmove            float32
	bobcycle           int32
	bobfracsin         float32

	// g_items.c: C SpawnItem sets item->drop = NULL in coop (mutating the
	// global item table); the Go table is immutable, so it is recorded here.
	itemDropCleared [MAX_ITEMS]bool

	// C function-level statics
	P_DamageFeedback_i int32 // p_view.c:99 static int i
	player_die_i       int32 // p_client.c:563 static int i

	// Per-monster file statics (static int sound_* in m_*.c), see g_monster.go.
	mstatics map[string]any
}

// New creates a game module instance (C GetGameAPI). rng is the process
// rand() state shared with the server.
// C: game/g_main.c:107 GetGameAPI
//
// The module (baseq2 or ctf) is detected in Init from the "game" cvar
// (the gamedir: "ctf" selects the ctf module, like loading ctf/game.so).
func New(gi Import, rng *crand.Rand) *Game {
	return &Game{gi: gi, rng: rng, mstatics: map[string]any{}, itemlist: itemlist}
}

// NewModule creates a game module instance for an explicit module: "ctf"
// runs the ctf module (ctf/*.c), anything else the 3.19 baseq2 game.
func NewModule(gi Import, rng *crand.Rand, module string) *Game {
	g := New(gi, rng)
	g.moduleForced = true
	g.setModule(module == "ctf")
	return g
}

// setModule selects the ctf module (per-mode item table).
func (g *Game) setModule(ctf bool) {
	g.ctfmod = ctf
	if ctf {
		g.itemlist = ctfItemlist
	} else {
		g.itemlist = itemlist
	}
}

// IsCTF reports whether this instance runs the ctf module.
func (g *Game) IsCTF() bool { return g.ctfmod }

var _ Export = (*Game)(nil)

// random is the C macro random(): ((rand () & 0x7fff) / ((float)0x7fff)).
// C: game/g_local.h:513 random
func (g *Game) random() float32 { return g.rng.GRandom() }

// crandom is the C macro crandom(): (2.0 * (random() - 0.5)), a double.
// C: game/g_local.h:514 crandom
func (g *Game) crandom() float64 { return g.rng.GCrandom() }

// world is the C macro world (&g_edicts[0]).
func (g *Game) world() *Edict { return &g.edicts[0] }

// EDICT_NUM returns g_edicts + n.
func (g *Game) EDICT_NUM(n int) *Edict { return &g.edicts[n] }

// Edicts is globals.edicts.
func (g *Game) Edicts() []Edict { return g.edicts }

// NumEdicts is globals.num_edicts.
func (g *Game) NumEdicts() int { return int(g.num_edicts) }

// MaxEdicts is globals.max_edicts.
func (g *Game) MaxEdicts() int { return int(g.game.Maxentities) }

// Level exposes level_locals_t (read-only use by tests and the server).
func (g *Game) Level() *LevelLocals { return &g.level }

// Init is game_export_t.Init.
func (g *Game) Init() {
	defer g.guard()
	g.InitGame()
}

// Shutdown is game_export_t.Shutdown.
func (g *Game) Shutdown() {
	defer g.guard()
	g.ShutdownGame()
}

// RunFrame is game_export_t.RunFrame.
func (g *Game) RunFrame() {
	defer g.guard()
	g.G_RunFrame()
}

// dprintf etc. are printf-style conveniences for gi.*printf.
func (g *Game) dprintf(format string, args ...any) { g.gi.Dprintf(cfmt(format, args...)) }
func (g *Game) bprintf(level int, format string, args ...any) {
	g.gi.Bprintf(level, cfmt(format, args...))
}
func (g *Game) cprintf(ent *Edict, level int, format string, args ...any) {
	g.gi.Cprintf(ent, level, cfmt(format, args...))
}
func (g *Game) centerprintf(ent *Edict, format string, args ...any) {
	g.gi.Centerprintf(ent, cfmt(format, args...))
}
func (g *Game) error(format string, args ...any) { g.gi.Error(cfmt(format, args...)) }

// cfmt formats like C printf for the verbs used by the game: %i is %d,
// including with flags, width and precision ("%3i" in HelpComputer).
// Callers must not rely on %g (C and Go differ); use cfmtG.
func cfmt(format string, args ...any) string {
	if len(args) == 0 {
		return format
	}
	return fmt.Sprintf(cIntVerbs(format), args...)
}

// cIntVerbs rewrites every C %i conversion (with any flags, width and
// precision) to Go's %d, leaving %% and all other verbs alone.
func cIntVerbs(format string) string {
	if !strings.Contains(format, "i") {
		return format
	}
	b := []byte(format)
	for i := 0; i < len(b); i++ {
		if b[i] != '%' {
			continue
		}
		j := i + 1
		if j < len(b) && b[j] == '%' {
			i = j
			continue
		}
		for j < len(b) && strings.IndexByte("-+ #0123456789.", b[j]) >= 0 {
			j++
		}
		if j < len(b) && b[j] == 'i' {
			b[j] = 'd'
		}
		i = j
	}
	return string(b)
}

// C: game/g_main.c:91 ShutdownGame
func (g *Game) ShutdownGame() {
	g.gi.Dprintf("==== ShutdownGame ====\n")
}

// C: game/g_save.c:152 InitGame
//
// This will be called when the dll is first loaded, which
// only happens when a new game is started or a save game
// is loaded.
func (g *Game) InitGame() {
	g.gi.Dprintf("==== InitGame ====\n")

	// module selection (the gamedir decides which game.so the engine loads)
	if !g.moduleForced {
		g.setModule(g.gi.Cvar("game", "", 0).String == "ctf")
	}

	g.gun_x = g.gi.Cvar("gun_x", "0", 0)
	g.gun_y = g.gi.Cvar("gun_y", "0", 0)
	g.gun_z = g.gi.Cvar("gun_z", "0", 0)

	//FIXME: sv_ prefix is wrong for these
	g.sv_rollspeed = g.gi.Cvar("sv_rollspeed", "200", 0)
	g.sv_rollangle = g.gi.Cvar("sv_rollangle", "2", 0)
	g.sv_maxvelocity = g.gi.Cvar("sv_maxvelocity", "2000", 0)
	g.sv_gravity = g.gi.Cvar("sv_gravity", "800", 0)

	// noset vars
	g.dedicated = g.gi.Cvar("dedicated", "0", CVAR_NOSET)

	// latched vars
	g.sv_cheats = g.gi.Cvar("cheats", "0", CVAR_SERVERINFO|CVAR_LATCH)
	g.gi.Cvar("gamename", GAMEVERSION, CVAR_SERVERINFO|CVAR_LATCH)
	g.gi.Cvar("gamedate", GAMEDATE, CVAR_SERVERINFO|CVAR_LATCH)

	g.maxclients = g.gi.Cvar("maxclients", "4", CVAR_SERVERINFO|CVAR_LATCH)
	if !g.ctfmod { // the ctf fork has no spectator mode (docs/CTF.md)
		g.maxspectators = g.gi.Cvar("maxspectators", "4", CVAR_SERVERINFO)
	}
	g.deathmatch = g.gi.Cvar("deathmatch", "0", CVAR_LATCH)
	g.coop = g.gi.Cvar("coop", "0", CVAR_LATCH)
	g.skill = g.gi.Cvar("skill", "1", CVAR_LATCH)
	g.maxentities = g.gi.Cvar("maxentities", "1024", CVAR_LATCH)

	//ZOID
	if g.ctfmod {
		// C: ctf/g_save.c:175
		//This game.dll only supports deathmatch
		if g.deathmatch.Value == 0 {
			g.gi.Dprintf("Forcing deathmatch.")
			g.gi.CvarSet("deathmatch", "1")
		}
		//force coop off
		if g.coop.Value != 0 {
			g.gi.CvarSet("coop", "0")
		}
	}
	//ZOID

	// change anytime vars
	g.dmflags = g.gi.Cvar("dmflags", "0", CVAR_SERVERINFO)
	g.fraglimit = g.gi.Cvar("fraglimit", "0", CVAR_SERVERINFO)
	g.timelimit = g.gi.Cvar("timelimit", "0", CVAR_SERVERINFO)
	if g.ctfmod {
		//ZOID
		g.capturelimit = g.gi.Cvar("capturelimit", "0", CVAR_SERVERINFO)
		g.instantweap = g.gi.Cvar("instantweap", "0", CVAR_SERVERINFO)
		//ZOID
		g.password = g.gi.Cvar("password", "", CVAR_USERINFO)
	} else {
		g.password = g.gi.Cvar("password", "", CVAR_USERINFO)
		g.spectator_password = g.gi.Cvar("spectator_password", "", CVAR_USERINFO)
		g.filterban = g.gi.Cvar("filterban", "1", 0)
	}

	g.g_select_empty = g.gi.Cvar("g_select_empty", "0", CVAR_ARCHIVE)

	g.run_pitch = g.gi.Cvar("run_pitch", "0.002", 0)
	g.run_roll = g.gi.Cvar("run_roll", "0.005", 0)
	g.bob_up = g.gi.Cvar("bob_up", "0.005", 0)
	g.bob_pitch = g.gi.Cvar("bob_pitch", "0.002", 0)
	g.bob_roll = g.gi.Cvar("bob_roll", "0.002", 0)

	// flood control
	g.flood_msgs = g.gi.Cvar("flood_msgs", "4", 0)
	g.flood_persecond = g.gi.Cvar("flood_persecond", "4", 0)
	g.flood_waitdelay = g.gi.Cvar("flood_waitdelay", "10", 0)

	// dm map list
	g.sv_maplist = g.gi.Cvar("sv_maplist", "", 0)

	// items
	g.InitItems()

	g.game.Helpmessage1 = ""
	g.game.Helpmessage2 = ""

	// initialize all entities for this game
	g.game.Maxentities = int32(g.maxentities.Value)
	g.edicts = make([]Edict, g.game.Maxentities)
	for i := range g.edicts {
		g.edicts[i].Index = i
	}

	// initialize all clients for this game
	g.game.Maxclients = int32(g.maxclients.Value)
	g.game.Clients = make([]GClient, g.game.Maxclients)
	for i := range g.game.Clients {
		g.game.Clients[i].Index = i
	}
	g.num_edicts = g.game.Maxclients + 1

	//ZOID
	if g.ctfmod {
		g.CTFInit()
	}
	//ZOID
}

// GAMEDATE stands for the C __DATE__ of the "gamedate" cvar and the
// gameversion command.
const GAMEDATE = "Dec 20 2001"

// C: game/g_main.c:178 ClientEndServerFrames
func (g *Game) ClientEndServerFrames() {
	// calc the player views now that all pushing
	// and damage has been added
	for i := 0; float32(i) < g.maxclients.Value; i++ {
		ent := &g.edicts[1+i]
		if !ent.InUse || ent.Client == nil {
			continue
		}
		g.ClientEndServerFrame(ent)
	}
}

// C: game/g_main.c:200 CreateTargetChangeLevel
func (g *Game) CreateTargetChangeLevel(mapname string) *Edict {
	ent := g.G_Spawn()
	ent.Classname = "target_changelevel"
	g.level.Nextmap = truncQPath(mapname)
	ent.Map = g.level.Nextmap
	return ent
}

// truncQPath emulates Com_sprintf/strncpy into a char[MAX_QPATH] buffer.
func truncQPath(s string) string {
	if len(s) > MAX_QPATH-1 {
		return s[:MAX_QPATH-1]
	}
	return s
}

// C: game/g_main.c:218 EndDMLevel
//
// The timelimit or fraglimit has been exceeded
func (g *Game) EndDMLevel() {
	const seps = " ,\n\r"

	// stay on same level flag
	if int32(g.dmflags.Value)&DF_SAME_LEVEL != 0 {
		g.BeginIntermission(g.CreateTargetChangeLevel(g.level.Mapname))
		return
	}

	if g.ctfmod && g.level.Forcemap != "" { // C: ctf/g_main.c:231
		g.BeginIntermission(g.CreateTargetChangeLevel(g.level.Forcemap))
		return
	}

	// see if it's in the map list
	if g.sv_maplist.String != "" {
		toks := strings.FieldsFunc(g.sv_maplist.String, func(r rune) bool { return strings.ContainsRune(seps, r) })
		f := ""
		for i, t := range toks {
			if shared.Q_stricmp(t, g.level.Mapname) == 0 {
				// it's in the list, go to the next one
				if i+1 >= len(toks) { // end of list, go to first one
					if f == "" { // there isn't a first one, same level
						g.BeginIntermission(g.CreateTargetChangeLevel(g.level.Mapname))
					} else {
						g.BeginIntermission(g.CreateTargetChangeLevel(f))
					}
				} else {
					g.BeginIntermission(g.CreateTargetChangeLevel(toks[i+1]))
				}
				return
			}
			if f == "" {
				f = t
			}
		}
	}

	if g.level.Nextmap != "" { // go to a specific map
		g.BeginIntermission(g.CreateTargetChangeLevel(g.level.Nextmap))
	} else { // search for a changelevel
		ent := g.G_Find(nil, FOFS_classname, "target_changelevel")
		if ent == nil {
			// the map designer didn't include a changelevel,
			// so create a fake ent that goes back to the same level
			g.BeginIntermission(g.CreateTargetChangeLevel(g.level.Mapname))
			return
		}
		g.BeginIntermission(ent)
	}
}

// C: game/g_main.c:275 CheckDMRules
func (g *Game) CheckDMRules() {
	if g.level.Intermissiontime != 0 {
		return
	}

	if g.deathmatch.Value == 0 {
		return
	}

	//ZOID
	if g.ctfmod {
		if g.ctfOn() && g.CTFCheckRules() {
			g.EndDMLevel()
			return
		}
		if g.CTFInMatch() {
			return // no checking in match mode
		}
	}
	//ZOID

	if g.timelimit.Value != 0 {
		if g.level.Time >= g.timelimit.Value*60 {
			g.gi.Bprintf(PRINT_HIGH, "Timelimit hit.\n")
			g.EndDMLevel()
			return
		}
	}

	if g.fraglimit.Value != 0 {
		for i := 0; float32(i) < g.maxclients.Value; i++ {
			cl := &g.game.Clients[i]
			if !g.edicts[i+1].InUse {
				continue
			}

			if float32(cl.Resp.Score) >= g.fraglimit.Value {
				g.gi.Bprintf(PRINT_HIGH, "Fraglimit hit.\n")
				g.EndDMLevel()
				return
			}
		}
	}
}

// gamemapCommand builds ExitLevel's console command
// Com_sprintf(command, sizeof(command), "gamemap \"%s\"\n", level.changemap).
//
// Deviation (security, docs/review/03-go-game.md G-11): changemap comes from
// map data (target_changelevel "map", worldspawn "nextmap"), where a "\n"
// escape or a quote would end the quoted argument and the command line, and
// the rest would run as further console commands on the server. The name is
// cut at the first quote or line break; real map names never contain them.
func gamemapCommand(changemap string) string {
	if i := strings.IndexAny(changemap, "\"\n\r"); i >= 0 {
		changemap = changemap[:i]
	}
	command := fmt.Sprintf("gamemap \"%s\"\n", changemap)
	if len(command) > 255 {
		command = command[:255]
	}
	return command
}

// C: game/g_main.c:318 ExitLevel
func (g *Game) ExitLevel() {
	if g.ctfmod {
		g.ctfExitLevel()
		return
	}
	g.gi.AddCommandString(gamemapCommand(g.level.Changemap))
	g.level.Changemap = ""
	g.level.Exitintermission = 0
	g.level.Intermissiontime = 0
	g.ClientEndServerFrames()

	// clear some things before going to next level
	for i := 0; float32(i) < g.maxclients.Value; i++ {
		ent := &g.edicts[1+i]
		if !ent.InUse {
			continue
		}
		if ent.Health > ent.Client.Pers.MaxHealth {
			ent.Health = ent.Client.Pers.MaxHealth
		}
	}
}

// ctfExitLevel is the ctf fork's ExitLevel (CTFNextMap hook, different order).
// C: ctf/g_main.c:333 ExitLevel
func (g *Game) ctfExitLevel() {
	g.level.Exitintermission = 0
	g.level.Intermissiontime = 0

	if g.CTFNextMap() {
		return
	}

	g.gi.AddCommandString(gamemapCommand(g.level.Changemap))
	g.ClientEndServerFrames()

	g.level.Changemap = ""

	// clear some things before going to next level
	for i := 0; float32(i) < g.maxclients.Value; i++ {
		ent := &g.edicts[1+i]
		if !ent.InUse {
			continue
		}
		if ent.Health > ent.Client.Pers.MaxHealth {
			ent.Health = ent.Client.Pers.MaxHealth
		}
	}
}

// C: game/g_main.c:349 G_RunFrame
//
// Advances the world by 0.1 seconds
func (g *Game) G_RunFrame() {
	g.level.Framenum++
	g.level.Time = float32(float64(g.level.Framenum) * FRAMETIME)

	// choose a client for monsters to target this frame
	g.AI_SetSightClient()

	// exit intermissions

	if g.level.Exitintermission != 0 {
		g.ExitLevel()
		return
	}

	//
	// treat each object in turn
	// even the world gets a chance to think
	//
	for i := 0; i < int(g.num_edicts); i++ {
		ent := &g.edicts[i]
		if !ent.InUse {
			continue
		}

		g.level.CurrentEntity = ent

		ent.S.OldOrigin = ent.S.Origin

		// if the ground entity moved, make sure we are still on it
		if ent.Groundentity != nil && ent.Groundentity.LinkCount != ent.GroundentityLinkcount {
			ent.Groundentity = nil
			if ent.Flags&(FL_SWIM|FL_FLY) == 0 && ent.SVFlags&SVF_MONSTER != 0 {
				g.M_CheckGround(ent)
			}
		}

		if i > 0 && float32(i) <= g.maxclients.Value {
			g.ClientBeginServerFrame(ent)
			continue
		}

		g.G_RunEntity(ent)
	}

	// see if it is time to end a deathmatch
	g.CheckDMRules()

	// build the playerstate_t structures for all players
	g.ClientEndServerFrames()
}
