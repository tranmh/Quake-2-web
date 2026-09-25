package sv

import (
	"quake2web/server/internal/game"
	"quake2web/server/internal/pmove"
	"quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/cvar"
	"quake2web/server/internal/qcommon/shared"
)

// gameImport is game_import_t: the engine functions handed to the game.
// C: server/sv_game.c:334 SV_InitGameProgs (import table)
type gameImport struct {
	s *Server
}

var _ game.Import = (*gameImport)(nil)

// Unicast sends the contents of the mutlicast buffer to a single client.
// C: server/sv_game.c:34 PF_Unicast
func (g *gameImport) Unicast(ent *game.Edict, reliable bool) {
	s := g.s
	if ent == nil {
		return
	}

	p := numForEdict(ent)
	if p < 1 || p > s.maxClients() {
		return
	}

	client := &s.SVS.Clients[p-1]

	if reliable {
		client.Netchan.Message.SZ_Write(s.SV.Multicast.Bytes())
	} else {
		client.Datagram.SZ_Write(s.SV.Multicast.Bytes())
	}

	s.SV.Multicast.SZ_Clear()
}

// clip1024 mimics the char msg[1024] vsprintf buffers of the PF_*printf
// functions (overflow is a memory-safety issue: truncated).
func clip1024(text string) string {
	if len(text) > 1023 {
		return text[:1023]
	}
	return text
}

// Dprintf is debug print to server console.
// C: server/sv_game.c:62 PF_dprintf
func (g *gameImport) Dprintf(text string) {
	g.s.Printf("%s", clip1024(text))
}

// Cprintf prints to a single client.
// C: server/sv_game.c:81 PF_cprintf
func (g *gameImport) Cprintf(ent *game.Edict, level int, text string) {
	s := g.s
	n := 0
	if ent != nil {
		n = numForEdict(ent)
		if n < 1 || n > s.maxClients() {
			shared.Error(q2const.ERR_DROP, "cprintf to a non-client")
		}
	}

	text = clip1024(text)
	if ent != nil {
		s.ClientPrintf(&s.SVS.Clients[n-1], level, "%s", text)
	} else {
		s.Printf("%s", text)
	}
}

// Centerprintf centerprints to a single client.
// C: server/sv_game.c:110 PF_centerprintf
func (g *gameImport) Centerprintf(ent *game.Edict, text string) {
	s := g.s
	n := numForEdict(ent)
	if n < 1 || n > s.maxClients() {
		return // Com_Error (ERR_DROP, "centerprintf to a non-client");
	}

	s.SV.Multicast.MSG_WriteByte(q2const.Svc_centerprint)
	s.SV.Multicast.MSG_WriteString(clip1024(text))
	g.Unicast(ent, true)
}

// Error aborts the server with a game error.
// C: server/sv_game.c:134 PF_error
func (g *gameImport) Error(text string) {
	shared.Error(q2const.ERR_DROP, "Game Error: %s", clip1024(text))
}

// SetModel also sets mins and maxs for inline bmodels.
// C: server/sv_game.c:154 PF_setmodel
func (g *gameImport) SetModel(ent *game.Edict, name string) {
	s := g.s
	i := s.ModelIndex(name)

	ent.S.ModelIndex = int32(i)

	// if it is an inline model, get the size information for it
	if len(name) > 0 && name[0] == '*' {
		mod := s.CM.Map().InlineModel(name)
		ent.Mins = mod.Mins
		ent.Maxs = mod.Maxs
		s.World.LinkEdict(ent)
	}
}

// Configstring. C: server/sv_game.c:183 PF_Configstring
func (g *gameImport) Configstring(index int, val string) {
	s := g.s
	if index < 0 || index >= q2const.MAX_CONFIGSTRINGS {
		shared.Error(q2const.ERR_DROP, "configstring: bad index %d\n", index)
	}

	// change the string in sv
	s.SV.ConfigStrings.Set(index, val)

	if s.SV.State != ss_loading {
		// send the update to everyone
		s.SV.Multicast.SZ_Clear()
		s.SV.Multicast.MSG_WriteChar(q2const.Svc_configstring)
		s.SV.Multicast.MSG_WriteShort(int32(index))
		s.SV.Multicast.MSG_WriteString(val)

		s.Multicast(&shared.Vec3Origin, q2const.MULTICAST_ALL_R)
	}
}

// WriteChar ... WriteAngle write to sv.multicast.
// C: server/sv_game.c:209 PF_WriteChar ...
func (g *gameImport) WriteChar(c int)         { g.s.SV.Multicast.MSG_WriteChar(int32(c)) }
func (g *gameImport) WriteByte(c int)         { g.s.SV.Multicast.MSG_WriteByte(int32(c)) }
func (g *gameImport) WriteShort(c int)        { g.s.SV.Multicast.MSG_WriteShort(int32(c)) }
func (g *gameImport) WriteLong(c int)         { g.s.SV.Multicast.MSG_WriteLong(int32(c)) }
func (g *gameImport) WriteFloat(f float32)    { g.s.SV.Multicast.MSG_WriteFloat(f) }
func (g *gameImport) WriteString(str string)  { g.s.SV.Multicast.MSG_WriteString(str) }
func (g *gameImport) WritePosition(pos *Vec3) { g.s.SV.Multicast.MSG_WritePos(*pos) }
func (g *gameImport) WriteDir(dir *Vec3)      { g.s.SV.Multicast.MSG_WriteDir(dir) }
func (g *gameImport) WriteAngle(f float32)    { g.s.SV.Multicast.MSG_WriteAngle(f) }

// Vec3 is vec3_t.
type Vec3 = shared.Vec3

// maskBit tests bit cluster of a PVS/PHS row. C reads mask[cluster>>3] even
// for cluster -1 (one byte before the static row: undefined); treated as not
// set.
func maskBit(mask []byte, cluster int32) bool {
	if cluster < 0 || int(cluster>>3) >= len(mask) {
		return false
	}
	return mask[cluster>>3]&(1<<(cluster&7)) != 0
}

// InPVS also checks portalareas so that doors block sight.
// C: server/sv_game.c:227 PF_inPVS
func (g *gameImport) InPVS(p1, p2 *Vec3) bool {
	cm := g.s.CM
	m := cm.Map()
	leafnum := cm.PointLeafnum(*p1)
	cluster := m.LeafCluster(int(leafnum))
	area1 := m.LeafArea(int(leafnum))
	mask := cm.ClusterPVS(int(cluster))

	leafnum = cm.PointLeafnum(*p2)
	cluster = m.LeafCluster(int(leafnum))
	area2 := m.LeafArea(int(leafnum))
	if !maskBit(mask, cluster) {
		return false
	}
	if !cm.AreasConnected(int(area1), int(area2)) {
		return false // a door blocks sight
	}
	return true
}

// InPHS also checks portalareas so that doors block sound.
// C: server/sv_game.c:257 PF_inPHS
func (g *gameImport) InPHS(p1, p2 *Vec3) bool {
	cm := g.s.CM
	m := cm.Map()
	leafnum := cm.PointLeafnum(*p1)
	cluster := m.LeafCluster(int(leafnum))
	area1 := m.LeafArea(int(leafnum))
	mask := cm.ClusterPHS(int(cluster))

	leafnum = cm.PointLeafnum(*p2)
	cluster = m.LeafCluster(int(leafnum))
	area2 := m.LeafArea(int(leafnum))
	if !maskBit(mask, cluster) {
		return false // more than one bounce away
	}
	if !cm.AreasConnected(int(area1), int(area2)) {
		return false // a door blocks hearing
	}
	return true
}

// Sound. C: server/sv_game.c:280 PF_StartSound
func (g *gameImport) Sound(entity *game.Edict, channel, soundnum int, volume, attenuation, timeofs float32) {
	if entity == nil {
		return
	}
	g.s.StartSound(nil, entity, channel, soundnum, volume, attenuation, timeofs)
}

// PositionedSound is SV_StartSound.
func (g *gameImport) PositionedSound(origin *Vec3, entity *game.Edict, channel, soundnum int, volume, attenuation, timeofs float32) {
	g.s.StartSound(origin, entity, channel, soundnum, volume, attenuation, timeofs)
}

func (g *gameImport) Bprintf(printlevel int, text string) {
	g.s.BroadcastPrintf(printlevel, "%s", text)
}

func (g *gameImport) ModelIndex(name string) int { return g.s.ModelIndex(name) }
func (g *gameImport) SoundIndex(name string) int { return g.s.SoundIndex(name) }
func (g *gameImport) ImageIndex(name string) int { return g.s.ImageIndex(name) }

func (g *gameImport) Trace(start, mins, maxs, end *Vec3, passent *game.Edict, contentmask int32) game.Trace {
	return g.s.World.Trace(start, mins, maxs, end, passent, contentmask)
}

func (g *gameImport) PointContents(point *Vec3) int32 { return g.s.World.PointContents(*point) }

func (g *gameImport) SetAreaPortalState(portalnum int, open bool) {
	g.s.CM.SetAreaPortalState(portalnum, open)
}

func (g *gameImport) AreasConnected(area1, area2 int) bool {
	return g.s.CM.AreasConnected(area1, area2)
}

func (g *gameImport) LinkEntity(ent *game.Edict)   { g.s.World.LinkEdict(ent) }
func (g *gameImport) UnlinkEntity(ent *game.Edict) { g.s.World.UnlinkEdict(ent) }

func (g *gameImport) BoxEdicts(mins, maxs *Vec3, list []*game.Edict, areatype int) int {
	return g.s.World.AreaEdicts(*mins, *maxs, list, areatype)
}

// Pmove is qcommon Pmove with pm_airaccelerate set by SV_SpawnServer.
// C: qcommon/pmove.c:1238 Pmove
func (g *gameImport) Pmove(pm *pmove.PmoveT) { pmove.Pmove(pm, g.s.pmAirAccelerate) }

func (g *gameImport) Multicast(origin *Vec3, to int) { g.s.Multicast(origin, to) }

func (g *gameImport) Cvar(name, value string, flags int) *cvar.Cvar {
	return g.s.Cvars.Get(name, value, flags)
}
func (g *gameImport) CvarSet(name, value string) *cvar.Cvar { return g.s.Cvars.Set(name, value) }
func (g *gameImport) CvarForceSet(name, value string) *cvar.Cvar {
	return g.s.Cvars.ForceSet(name, value)
}

func (g *gameImport) Argc() int         { return g.s.Cmd.Argc() }
func (g *gameImport) Argv(n int) string { return g.s.Cmd.Argv(n) }
func (g *gameImport) Args() string      { return g.s.Cmd.Args() }

func (g *gameImport) AddCommandString(text string) { g.s.Cmd.Cbuf_AddText(text) }

// shutdownGameProgs is called when either the entire server is being killed,
// or it is changing to a different game directory.
// C: server/sv_game.c:299 SV_ShutdownGameProgs
func (s *Server) shutdownGameProgs() {
	if s.ge == nil {
		return
	}
	s.ge.Shutdown()
	s.ge = nil
	s.gi = nil
}

// initGameProgs inits the game subsystem for a new map.
// C: server/sv_game.c:318 SV_InitGameProgs
func (s *Server) initGameProgs() {
	// unload anything we have now
	if s.ge != nil {
		s.shutdownGameProgs()
	}

	// load a new game dll
	s.gi = &gameImport{s: s}
	if s.cfg.Game != nil {
		s.ge = s.cfg.Game(s.gi)
	}
	if s.ge == nil {
		shared.Error(q2const.ERR_DROP, "failed to load game DLL")
	}

	s.ge.Init()
}
