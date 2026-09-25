// Package gametest is the Go equivalent of the C oracle driver
// oracle/src/game_main.c: a headless engine side (server/sv_game.c PF_*,
// SV_FindIndex, sv_world.c through internal/world, the parts of sv_init.c /
// sv_user.c / sv_main.c the oracle runs) driving the game module through
// game.Import / game.Export, with the same event capture, and a comparator
// against the golden fixtures fixtures/generated/game/<scenario>.jsonl.
package gametest

import (
	"fmt"

	"quake2web/server/internal/cmodel"
	"quake2web/server/internal/game"
	"quake2web/server/internal/pmove"
	. "quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/cmd"
	"quake2web/server/internal/qcommon/cvar"
	"quake2web/server/internal/qcommon/msg"
	"quake2web/server/internal/qcommon/shared"
	"quake2web/server/internal/world"
)

type Vec3 = shared.Vec3

// server_state_t
// C: server/server.h:35 server_state_t
const (
	ss_dead = iota
	ss_loading
	ss_game
	ss_cinematic
	ss_demo
	ss_pic
)

// client_state_t
// C: server/server.h:81
const (
	cs_free = iota
	cs_zombie
	cs_connected
	cs_spawned
)

// client is the part of client_t the oracle flow uses.
type client struct {
	state    int
	edict    *game.Edict
	userinfo string
}

// Event is one captured game_import_t call, in the JSON shape of
// docs/FIXTURES.md ("t", ...). Values are float32 / int64 / string / nil /
// []any (vectors, float32 elements) so they compare exactly.
type Event = map[string]any

// Engine implements game.Import with the semantics of the C engine used by
// the oracle and records the calls the oracle's wrappers record.
type Engine struct {
	Cvars *cvar.Registry
	Cmd   *cmd.Cmd

	Map   *cmodel.Map
	CM    *cmodel.State
	World *world.World

	Ge game.Export

	models        [MAX_MODELS]*shared.CModel // sv.models
	Configstrings [MAX_CONFIGSTRINGS]string  // sv.configstrings
	MC            *msg.SizeBuf               // sv.multicast
	State         int                        // sv.state
	airaccelerate float32                    // pm_airaccelerate

	clients    []client
	maxclients *cvar.Cvar

	// Events captured since the last reset (in call order).
	Events []Event
	// Log receives Com_Printf-like engine output (may be nil).
	Log func(string)
}

var _ game.Import = (*Engine)(nil)

func newEngine() *Engine {
	e := &Engine{Cvars: cvar.New()}
	e.Cmd = cmd.New(e.Cvars)
	e.Cvars.ServerState = func() int { return e.State }
	e.MC = msg.NewSizeBuf(MAX_MSGLEN)
	return e
}

func (e *Engine) logf(format string, args ...any) {
	if e.Log != nil {
		e.Log(fmt.Sprintf(format, args...))
	}
}

func (e *Engine) record(ev Event) { e.Events = append(e.Events, ev) }

func (e *Engine) maxClients() int { return int(e.maxclients.Value) }

func entNum(ent *game.Edict) int64 {
	if ent == nil {
		return -1
	}
	return int64(ent.Index)
}

func vecAny(v Vec3) []any { return []any{v[0], v[1], v[2]} }

func (e *Engine) soundName(idx int) string {
	if idx >= 0 && idx < MAX_SOUNDS {
		return e.Configstrings[CS_SOUNDS+idx]
	}
	return ""
}

// ---- prints ----

func (e *Engine) printEv(kind string, ent int64, level int, text string) {
	e.record(Event{"t": "print", "kind": kind, "ent": ent, "level": int64(level), "text": text})
}

// Bprintf C: server/sv_game.c:? PF_bprintf -> SV_BroadcastPrintf (no game-visible state).
func (e *Engine) Bprintf(printlevel int, text string) {
	e.printEv("bprintf", -1, printlevel, text)
}

// Dprintf C: server/sv_game.c:62 PF_dprintf
func (e *Engine) Dprintf(text string) {
	e.printEv("dprintf", -1, 0, text)
	e.logf("%s", text)
}

// Cprintf C: server/sv_game.c:81 PF_cprintf
func (e *Engine) Cprintf(ent *game.Edict, printlevel int, text string) {
	e.printEv("cprintf", entNum(ent), printlevel, text)
	if ent != nil {
		n := ent.Index
		if n < 1 || n > e.maxClients() {
			shared.Error(ERR_DROP, "cprintf to a non-client")
		}
	}
}

// Centerprintf C: server/sv_game.c:110 PF_centerprintf
func (e *Engine) Centerprintf(ent *game.Edict, text string) {
	e.printEv("centerprintf", entNum(ent), 0, text)
	n := ent.Index
	if n < 1 || n > e.maxClients() {
		return
	}
	e.MC.MSG_WriteByte(Svc_centerprint)
	e.MC.MSG_WriteString(text)
	e.unicast(ent, true) // real PF_Unicast: not recorded
}

// Error C: server/sv_game.c:134 PF_error
func (e *Engine) Error(text string) {
	shared.Error(ERR_DROP, "Game Error: %s", text)
}

// ---- sounds ----

// Sound C: server/sv_game.c:285 PF_StartSound
func (e *Engine) Sound(ent *game.Edict, channel, soundindex int, volume, attenuation, timeofs float32) {
	e.record(Event{"t": "sound", "ent": entNum(ent), "channel": int64(channel), "sound": e.soundName(soundindex),
		"volume": volume, "attenuation": attenuation, "timeofs": timeofs})
	if ent == nil {
		return
	}
	e.startSound(nil, ent, channel, soundindex, volume, attenuation, timeofs)
}

// PositionedSound is gi.positioned_sound = SV_StartSound.
func (e *Engine) PositionedSound(origin *Vec3, ent *game.Edict, channel, soundindex int, volume, attenuation, timeofs float32) {
	var o any
	if origin != nil {
		o = vecAny(*origin)
	}
	e.record(Event{"t": "positioned_sound", "ent": entNum(ent), "channel": int64(channel), "sound": e.soundName(soundindex),
		"volume": volume, "attenuation": attenuation, "timeofs": timeofs, "origin": o})
	if ent == nil {
		return // C would crash in NUM_FOR_EDICT(NULL)
	}
	e.startSound(origin, ent, channel, soundindex, volume, attenuation, timeofs)
}

// startSound C: server/sv_send.c:272 SV_StartSound
func (e *Engine) startSound(origin *Vec3, entity *game.Edict, channel, soundindex int, volume, attenuation, timeofs float32) {
	var originV Vec3

	if volume < 0 || float64(volume) > 1.0 {
		shared.Error(ERR_FATAL, "SV_StartSound: volume = %f", volume)
	}
	if attenuation < 0 || attenuation > 4 {
		shared.Error(ERR_FATAL, "SV_StartSound: attenuation = %f", attenuation)
	}
	if timeofs < 0 || float64(timeofs) > 0.255 {
		shared.Error(ERR_FATAL, "SV_StartSound: timeofs = %f", timeofs)
	}

	ent := entity.Index

	usePHS := true
	if channel&8 != 0 { // no PHS flag
		usePHS = false
		channel &= 7
	}

	sendchan := (ent << 3) | (channel & 7)

	flags := 0
	if volume != DEFAULT_SOUND_PACKET_VOLUME {
		flags |= SND_VOLUME
	}
	if attenuation != DEFAULT_SOUND_PACKET_ATTENUATION {
		flags |= SND_ATTENUATION
	}

	// the client doesn't know that bmodels have weird origins
	// the origin can also be explicitly set
	if entity.SVFlags&SVF_NOCLIENT != 0 || entity.Solid == SOLID_BSP || origin != nil {
		flags |= SND_POS
	}

	// always send the entity number for channel overrides
	flags |= SND_ENT

	if timeofs != 0 {
		flags |= SND_OFFSET
	}

	// use the entity origin unless it is a bmodel or explicitly specified
	if origin == nil {
		origin = &originV
		if entity.Solid == SOLID_BSP {
			for i := 0; i < 3; i++ {
				originV[i] = float32(float64(entity.S.Origin[i]) + 0.5*float64(entity.Mins[i]+entity.Maxs[i]))
			}
		} else {
			originV = entity.S.Origin
		}
	}

	m := e.MC
	m.MSG_WriteByte(Svc_sound)
	m.MSG_WriteByte(int32(flags))
	m.MSG_WriteByte(int32(soundindex))

	if flags&SND_VOLUME != 0 {
		m.MSG_WriteByte(int32(volume * 255))
	}
	if flags&SND_ATTENUATION != 0 {
		m.MSG_WriteByte(int32(attenuation * 64))
	}
	if flags&SND_OFFSET != 0 {
		m.MSG_WriteByte(int32(timeofs * 1000))
	}
	if flags&SND_ENT != 0 {
		m.MSG_WriteShort(int32(sendchan))
	}
	if flags&SND_POS != 0 {
		m.MSG_WritePos(*origin)
	}

	// if the sound doesn't attenuate,send it to everyone
	// (global radio chatter, voiceovers, etc)
	if attenuation == ATTN_NONE {
		usePHS = false
	}

	if channel&CHAN_RELIABLE != 0 {
		if usePHS {
			e.multicast(origin, MULTICAST_PHS_R)
		} else {
			e.multicast(origin, MULTICAST_ALL_R)
		}
	} else {
		if usePHS {
			e.multicast(origin, MULTICAST_PHS)
		} else {
			e.multicast(origin, MULTICAST_ALL)
		}
	}
}

// ---- configstrings / indexes ----

// Configstring C: server/sv_game.c:183 PF_Configstring
func (e *Engine) Configstring(index int, val string) {
	e.record(Event{"t": "configstring", "index": int64(index), "value": val})
	if index < 0 || index >= MAX_CONFIGSTRINGS {
		shared.Error(ERR_DROP, "configstring: bad index %d\n", index)
	}

	// change the string in sv
	e.Configstrings[index] = val

	if e.State != ss_loading {
		// send the update to everyone
		e.MC.SZ_Clear()
		e.MC.MSG_WriteChar(Svc_configstring)
		e.MC.MSG_WriteShort(int32(index))
		e.MC.MSG_WriteString(val)
		e.multicast(&shared.Vec3Origin, MULTICAST_ALL_R)
	}
}

// findIndex C: server/sv_init.c:32 SV_FindIndex
func (e *Engine) findIndex(name string, start, max int, create bool) int {
	if name == "" {
		return 0
	}

	i := 1
	for ; i < max && e.Configstrings[start+i] != ""; i++ {
		if e.Configstrings[start+i] == name {
			return i
		}
	}

	if !create {
		return 0
	}

	if i == max {
		shared.Error(ERR_DROP, "*Index: overflow")
	}

	s := name
	if len(s) > MAX_QPATH {
		s = s[:MAX_QPATH] // strncpy of sizeof(configstring)
	}
	e.Configstrings[start+i] = s

	if e.State != ss_loading {
		// send the update to everyone
		e.MC.SZ_Clear()
		e.MC.MSG_WriteChar(Svc_configstring)
		e.MC.MSG_WriteShort(int32(start + i))
		e.MC.MSG_WriteString(name)
		e.multicast(&shared.Vec3Origin, MULTICAST_ALL_R)
	}

	return i
}

// ModelIndex C: server/sv_init.c:64 SV_ModelIndex
func (e *Engine) ModelIndex(name string) int { return e.findIndex(name, CS_MODELS, MAX_MODELS, true) }

// SoundIndex C: server/sv_init.c:69 SV_SoundIndex
func (e *Engine) SoundIndex(name string) int { return e.findIndex(name, CS_SOUNDS, MAX_SOUNDS, true) }

// ImageIndex C: server/sv_init.c:74 SV_ImageIndex
func (e *Engine) ImageIndex(name string) int { return e.findIndex(name, CS_IMAGES, MAX_IMAGES, true) }

// SetModel C: server/sv_game.c:161 PF_setmodel
func (e *Engine) SetModel(ent *game.Edict, name string) {
	i := e.ModelIndex(name)
	ent.S.ModelIndex = int32(i)

	// if it is an inline model, get the size information for it
	if len(name) > 0 && name[0] == '*' {
		mod := e.Map.InlineModel(name)
		ent.Mins = mod.Mins
		ent.Maxs = mod.Maxs
		e.World.LinkEdict(ent)
	}
}

// ---- collision ----

// Trace is SV_Trace.
func (e *Engine) Trace(start, mins, maxs, end *Vec3, passent *game.Edict, contentmask int32) game.Trace {
	return e.World.Trace(start, mins, maxs, end, passent, contentmask)
}

// PointContents is SV_PointContents.
func (e *Engine) PointContents(point *Vec3) int32 { return e.World.PointContents(*point) }

func maskBit(mask []byte, cluster int32) bool {
	if cluster < 0 {
		// C reads mask[-1] (undefined); treated as not visible.
		return false
	}
	return mask[cluster>>3]&(1<<(uint(cluster)&7)) != 0
}

func (e *Engine) inVis(p1, p2 *Vec3, phs bool) bool {
	leafnum := e.CM.PointLeafnum(*p1)
	cluster := e.Map.LeafCluster(int(leafnum))
	area1 := e.Map.LeafArea(int(leafnum))
	var mask []byte
	if phs {
		mask = e.CM.ClusterPHS(int(cluster))
	} else {
		mask = e.CM.ClusterPVS(int(cluster))
	}

	leafnum = e.CM.PointLeafnum(*p2)
	cluster = e.Map.LeafCluster(int(leafnum))
	area2 := e.Map.LeafArea(int(leafnum))
	if !maskBit(mask, cluster) {
		return false
	}
	if !e.CM.AreasConnected(int(area1), int(area2)) {
		return false // a door blocks sight / hearing
	}
	return true
}

// InPVS C: server/sv_game.c:226 PF_inPVS
func (e *Engine) InPVS(p1, p2 *Vec3) bool { return e.inVis(p1, p2, false) }

// InPHS C: server/sv_game.c:257 PF_inPHS
func (e *Engine) InPHS(p1, p2 *Vec3) bool { return e.inVis(p1, p2, true) }

// SetAreaPortalState is CM_SetAreaPortalState.
func (e *Engine) SetAreaPortalState(portalnum int, open bool) {
	e.CM.SetAreaPortalState(portalnum, open)
}

// AreasConnected is CM_AreasConnected.
func (e *Engine) AreasConnected(area1, area2 int) bool { return e.CM.AreasConnected(area1, area2) }

// LinkEntity is SV_LinkEdict.
func (e *Engine) LinkEntity(ent *game.Edict) { e.World.LinkEdict(ent) }

// UnlinkEntity is SV_UnlinkEdict.
func (e *Engine) UnlinkEntity(ent *game.Edict) { e.World.UnlinkEdict(ent) }

// BoxEdicts is SV_AreaEdicts (maxcount = len(list)).
func (e *Engine) BoxEdicts(mins, maxs *Vec3, list []*game.Edict, areatype int) int {
	return e.World.AreaEdicts(*mins, *maxs, list, areatype)
}

// Pmove is PF_Pmove -> Pmove with pm_airaccelerate.
func (e *Engine) Pmove(pm *pmove.PmoveT) { pmove.Pmove(pm, e.airaccelerate) }

// ---- network messages ----

func (e *Engine) bytesHex() string { return fmt.Sprintf("%x", e.MC.Bytes()) }

// Multicast is gi.multicast = SV_Multicast (recorded).
func (e *Engine) Multicast(origin *Vec3, to int) {
	var o any
	if origin != nil {
		o = vecAny(*origin)
	}
	e.record(Event{"t": "multicast", "origin": o, "to": int64(to), "bytes": e.bytesHex()})
	e.multicast(origin, to)
}

// multicast C: server/sv_send.c:161 SV_Multicast. Nothing is ever sent by
// the oracle; the observable effects are the PVS lookup (none) and the clear.
func (e *Engine) multicast(origin *Vec3, to int) {
	switch to {
	case MULTICAST_ALL, MULTICAST_ALL_R, MULTICAST_PHS, MULTICAST_PHS_R, MULTICAST_PVS, MULTICAST_PVS_R:
	default:
		shared.Error(ERR_FATAL, "SV_Multicast: bad to:%d", to)
	}
	e.MC.SZ_Clear()
}

// Unicast is gi.unicast = PF_Unicast (recorded).
func (e *Engine) Unicast(ent *game.Edict, reliable bool) {
	r := int64(0)
	if reliable {
		r = 1
	}
	e.record(Event{"t": "unicast", "ent": entNum(ent), "reliable": r, "bytes": e.bytesHex()})
	e.unicast(ent, reliable)
}

// unicast C: server/sv_game.c:34 PF_Unicast
func (e *Engine) unicast(ent *game.Edict, reliable bool) {
	if ent == nil {
		return
	}
	p := ent.Index
	if p < 1 || p > e.maxClients() {
		return
	}
	e.MC.SZ_Clear()
}

func (e *Engine) WriteChar(c int)         { e.MC.MSG_WriteChar(int32(c)) }
func (e *Engine) WriteByteC(c int)        { e.MC.MSG_WriteByte(int32(c)) }
func (e *Engine) WriteShort(c int)        { e.MC.MSG_WriteShort(int32(c)) }
func (e *Engine) WriteLong(c int)         { e.MC.MSG_WriteLong(int32(c)) }
func (e *Engine) WriteFloat(f float32)    { e.MC.MSG_WriteFloat(f) }
func (e *Engine) WriteString(s string)    { e.MC.MSG_WriteString(s) }
func (e *Engine) WritePosition(pos *Vec3) { e.MC.MSG_WritePos(*pos) }
func (e *Engine) WriteDir(dir *Vec3)      { e.MC.MSG_WriteDir(dir) }
func (e *Engine) WriteAngle(f float32)    { e.MC.MSG_WriteAngle(f) }

// ---- cvars / commands ----

func (e *Engine) Cvar(name, value string, flags int) *cvar.Cvar {
	return e.Cvars.Get(name, value, flags)
}
func (e *Engine) CvarSet(name, value string) *cvar.Cvar      { return e.Cvars.Set(name, value) }
func (e *Engine) CvarForceSet(name, value string) *cvar.Cvar { return e.Cvars.ForceSet(name, value) }

func (e *Engine) Argc() int         { return e.Cmd.Argc() }
func (e *Engine) Argv(n int) string { return e.Cmd.Argv(n) }
func (e *Engine) Args() string      { return e.Cmd.Args() }

// AddCommandString is recorded and never executed (no gamemap etc).
func (e *Engine) AddCommandString(text string) {
	e.record(Event{"t": "cmd", "text": text})
}
