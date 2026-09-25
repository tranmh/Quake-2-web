// Package stubgame is a tiny game module (game.Export) for server tests and
// development until the real game port is complete. It spawns worldspawn,
// inline brush models ("model" "*N") and players at info_player_start, runs
// player movement through gi.Pmove and links players into the world. It is
// deliberately not a port of game/*.c.
package stubgame

import (
	"encoding/json"
	"strings"

	"quake2web/server/internal/game"
	"quake2web/server/internal/pmove"
	"quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/shared"
)

// StatusBar is the CS_STATUSBAR layout (spans many MAX_QPATH slots).
const StatusBar = "yb -24 xv 0 hnum xv 50 pic 0 if 2 xv 100 anum xv 150 pic 2 endif " +
	"if 4 xv 200 rnum xv 250 pic 4 endif if 6 xv 296 pic 6 endif yb -50 " +
	"if 7 xv 0 pic 7 xv 26 yb -42 stat_string 8 yb -50 endif " +
	"if 9 xv 262 num 2 10 xv 296 pic 9 endif if 11 xv 148 pic 11 endif " +
	"xr -50 yt 2 num 3 14 if 17 xv 0 yb -58 string2 \"SPECTATOR MODE\" endif " +
	"if 16 xv 0 yb -68 string \"Chasing\" xv 64 stat_string 16 endif " +
	"yb -24 xv 0 hnum xv 50 pic 0 if 2 xv 100 anum xv 150 pic 2 endif " +
	"if 4 xv 200 rnum xv 250 pic 4 endif if 6 xv 296 pic 6 endif yb -50 " +
	"if 7 xv 0 pic 7 xv 26 yb -42 stat_string 8 yb -50 endif " +
	"if 9 xv 262 num 2 10 xv 296 pic 9 endif if 11 xv 148 pic 11 endif "

// Game is the stub game state.
type Game struct {
	gi game.Import

	edicts     []game.Edict
	clients    []game.GClient
	numEdicts  int
	maxClients int

	spawns    []spawnPoint
	FrameNum  int
	Mapname   string
	Connected int

	// Events records the calls made by the server (for tests).
	Events []string
}

type spawnPoint struct {
	origin shared.Vec3
	angles shared.Vec3
}

// New returns a factory usable as sv.Config.Game.
func New() func(gi game.Import) game.Export {
	return func(gi game.Import) game.Export { return &Game{gi: gi} }
}

var _ game.Export = (*Game)(nil)

func (g *Game) Init() {
	g.gi.Dprintf("==== stubgame InitGame ====\n")
	g.gi.Cvar("gamename", "baseq2", q2const.CVAR_SERVERINFO|q2const.CVAR_LATCH)
	g.gi.Cvar("sv_gravity", "800", 0)
	g.maxClients = int(g.gi.Cvar("maxclients", "4", q2const.CVAR_SERVERINFO|q2const.CVAR_LATCH).Value)
	g.edicts = make([]game.Edict, q2const.MAX_EDICTS)
	for i := range g.edicts {
		g.edicts[i].Index = i
	}
	g.clients = make([]game.GClient, g.maxClients)
	for i := range g.clients {
		g.clients[i].Index = i
	}
	g.numEdicts = g.maxClients + 1
}

func (g *Game) Shutdown() { g.Events = append(g.Events, "shutdown") }

func (g *Game) spawn() *game.Edict {
	for i := g.maxClients + 1; i < g.numEdicts; i++ {
		if !g.edicts[i].InUse {
			return g.initEdict(i)
		}
	}
	if g.numEdicts == len(g.edicts) {
		g.gi.Error("ED_Alloc: no free edicts")
	}
	g.numEdicts++
	return g.initEdict(g.numEdicts - 1)
}

func (g *Game) initEdict(i int) *game.Edict {
	e := &g.edicts[i]
	*e = game.Edict{Index: i}
	e.InUse = true
	e.S.Number = int32(i)
	return e
}

func parseVec(s string) shared.Vec3 {
	var v shared.Vec3
	for i, f := range strings.Fields(s) {
		if i < 3 {
			v[i] = float32(shared.Atof(f))
		}
	}
	return v
}

// SpawnEntities parses the entity string.
func (g *Game) SpawnEntities(mapname, entstring, spawnpoint string) {
	g.Mapname = mapname
	g.spawns = nil
	for i := range g.edicts {
		g.edicts[i] = game.Edict{Index: i}
	}
	for i := range g.clients {
		g.clients[i] = game.GClient{Index: i}
	}
	g.numEdicts = g.maxClients + 1

	data := entstring
	first := true
	for {
		tok, rest, ok := shared.COM_Parse(data)
		data = rest
		if !ok || tok == "" {
			break
		}
		if tok != "{" {
			g.gi.Error("ED_LoadFromFile: found " + tok + " when expecting {")
		}
		kv := map[string]string{}
		for {
			key, rest, _ := shared.COM_Parse(data)
			data = rest
			if key == "}" || key == "" {
				break
			}
			val, rest, _ := shared.COM_Parse(data)
			data = rest
			kv[key] = val
		}
		cls := kv["classname"]
		// C: game/g_spawn.c:585 SpawnEntities (SPAWNFLAG_NOT_DEATHMATCH)
		if !first && g.gi.Cvar("deathmatch", "0", 0).Value != 0 && shared.Atoi(kv["spawnflags"])&0x800 != 0 {
			first = false
			continue
		}
		switch {
		case first:
			w := &g.edicts[0]
			w.InUse = true
			w.S.ModelIndex = 1 // world model is always index 1
			w.Solid = q2const.SOLID_BSP
			if m := kv["message"]; m != "" {
				g.gi.Configstring(q2const.CS_NAME, m)
			}
			g.gi.Configstring(q2const.CS_SKY, "unit1_")
			// long enough to span several configstring slots like the real
			// game's statusbar
			g.gi.Configstring(q2const.CS_STATUSBAR, StatusBar)
			g.gi.Configstring(q2const.CS_MAXCLIENTS, "4")
			g.gi.ModelIndex("players/male/tris.md2")
			g.gi.SoundIndex("player/step1.wav")
		case cls == "info_player_start" || cls == "info_player_deathmatch":
			sp := spawnPoint{origin: parseVec(kv["origin"])}
			if a, ok := kv["angle"]; ok {
				sp.angles[1] = float32(shared.Atof(a))
			}
			if cls == "info_player_start" {
				g.spawns = append([]spawnPoint{sp}, g.spawns...)
			} else {
				g.spawns = append(g.spawns, sp)
			}
		case cls == "func_areaportal": // no model is set for areaportals
		case cls == "func_explosive" && g.gi.Cvar("deathmatch", "0", 0).Value != 0:
			// auto-removed in deathmatch (g_func.c SP_func_explosive)
		case strings.HasPrefix(kv["model"], "*"):
			e := g.spawn()
			e.S.Origin = parseVec(kv["origin"])
			e.Solid = q2const.SOLID_BSP
			g.gi.SetModel(e, kv["model"])
			g.gi.LinkEntity(e)
		}
		first = false
	}
	g.Events = append(g.Events, "spawn "+mapname)
}

type saveState struct {
	FrameNum int    `json:"framenum"`
	Mapname  string `json:"mapname"`
}

func (g *Game) WriteGame(autosave bool) ([]byte, error) {
	return json.Marshal(saveState{FrameNum: g.FrameNum, Mapname: g.Mapname})
}

func (g *Game) ReadGame(data []byte) error {
	var st saveState
	if err := json.Unmarshal(data, &st); err != nil {
		return err
	}
	g.FrameNum = st.FrameNum
	return nil
}

func (g *Game) WriteLevel() ([]byte, error) {
	return json.Marshal(saveState{FrameNum: g.FrameNum, Mapname: g.Mapname})
}

func (g *Game) ReadLevel(data []byte) error {
	var st saveState
	return json.Unmarshal(data, &st)
}

func (g *Game) ClientConnect(ent *game.Edict, userinfo string) (bool, string) {
	g.Connected++
	g.Events = append(g.Events, "connect")
	return true, userinfo
}

func (g *Game) ClientBegin(ent *game.Edict) {
	g.Events = append(g.Events, "begin")
	cl := &g.clients[ent.Index-1]
	*cl = game.GClient{Index: cl.Index}
	idx := ent.Index
	g.gi.UnlinkEntity(ent) // never clear a linked edict
	*ent = game.Edict{Index: idx}
	ent.InUse = true
	ent.Client = cl
	ent.S.Number = int32(idx)
	ent.Solid = q2const.SOLID_BBOX
	ent.ClipMask = q2const.MASK_PLAYERSOLID
	ent.Mins = shared.Vec3{-16, -16, -24}
	ent.Maxs = shared.Vec3{16, 16, 32}
	ent.S.ModelIndex = 255
	ent.S.SkinNum = int32(idx - 1)

	var sp spawnPoint
	if len(g.spawns) > 0 {
		sp = g.spawns[0]
	}
	sp.origin[2] += 9
	ent.S.Origin = sp.origin
	ent.S.OldOrigin = sp.origin
	ent.S.Angles = shared.Vec3{0, sp.angles[1], 0}

	ps := &cl.PS
	ps.Fov = 90
	ps.ViewOffset[2] = 22
	ps.PMove.PmType = q2const.PM_NORMAL
	ps.PMove.Gravity = 800
	for i := 0; i < 3; i++ {
		ps.PMove.Origin[i] = int16(int32(sp.origin[i] * 8))
		ps.PMove.DeltaAngles[i] = int16(shared.ANGLE2SHORT(sp.angles[i]))
	}
	ps.ViewAngles = sp.angles
	g.gi.LinkEntity(ent)
}

func (g *Game) ClientUserinfoChanged(ent *game.Edict, userinfo string) {}

func (g *Game) ClientDisconnect(ent *game.Edict) {
	g.Events = append(g.Events, "disconnect")
	g.gi.UnlinkEntity(ent)
	ent.S.ModelIndex = 0
	ent.Solid = q2const.SOLID_NOT
	ent.InUse = false
}

func (g *Game) ClientCommand(ent *game.Edict) {
	g.Events = append(g.Events, "cmd "+g.gi.Argv(0))
}

// ClientThink moves the player like p_client.c ClientThink (movement part).
func (g *Game) ClientThink(ent *game.Edict, ucmd *shared.UserCmd) {
	cl := ent.Client
	if cl == nil {
		return
	}
	var pm pmove.PmoveT
	cl.PS.PMove.Gravity = 800
	pm.S = cl.PS.PMove
	for i := 0; i < 3; i++ {
		pm.S.Origin[i] = int16(int32(ent.S.Origin[i] * 8))
	}
	pm.Cmd = *ucmd
	pm.Trace = func(start, mins, maxs, end *shared.Vec3) shared.Trace {
		t := g.gi.Trace(start, mins, maxs, end, ent, q2const.MASK_PLAYERSOLID)
		st := shared.Trace{AllSolid: t.AllSolid, StartSolid: t.StartSolid, Fraction: t.Fraction,
			EndPos: t.EndPos, Plane: t.Plane, Surface: t.Surface, Contents: t.Contents, Ent: pmove.NoEnt}
		if t.Ent != nil {
			st.Ent = t.Ent.Index
		}
		return st
	}
	pm.PointContents = func(p shared.Vec3) int32 { return g.gi.PointContents(&p) }

	g.gi.Pmove(&pm)

	cl.PS.PMove = pm.S
	for i := 0; i < 3; i++ {
		ent.S.Origin[i] = float32(pm.S.Origin[i]) * 0.125
	}
	ent.Mins = pm.Mins
	ent.Maxs = pm.Maxs
	cl.PS.ViewAngles = pm.ViewAngles
	cl.PS.ViewOffset[2] = pm.ViewHeight
	ent.S.Angles[1] = pm.ViewAngles[1]
	g.gi.LinkEntity(ent)
}

func (g *Game) RunFrame() {
	g.FrameNum++
}

func (g *Game) ServerCommand() {
	g.Events = append(g.Events, "sv "+g.gi.Argv(1))
}

func (g *Game) Edicts() []game.Edict { return g.edicts }
func (g *Game) NumEdicts() int       { return g.numEdicts }
func (g *Game) MaxEdicts() int       { return len(g.edicts) }
