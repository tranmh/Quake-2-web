package game

import (
	"quake2web/server/internal/pmove"
	"quake2web/server/internal/qcommon/cvar"
	"quake2web/server/internal/qcommon/shared"
)

// Trace is C trace_t as seen by the game: Ent is an edict pointer.
// C: game/q_shared.h:445 trace_t
type Trace struct {
	AllSolid   bool
	StartSolid bool
	Fraction   float32
	EndPos     Vec3
	Plane      shared.CPlane
	Surface    *shared.CSurface
	Contents   int32
	Ent        *Edict
}

// Import is C game_import_t: the functions the engine provides to the game.
// The server (internal/sv) implements it; tests implement it with the same
// semantics as server/sv_game.c PF_* and server/sv_world.c.
// C: game/game.h:121 game_import_t
type Import interface {
	Bprintf(printlevel int, text string)
	Dprintf(text string)
	Cprintf(ent *Edict, printlevel int, text string)
	Centerprintf(ent *Edict, text string)
	Sound(ent *Edict, channel, soundindex int, volume, attenuation, timeofs float32)
	PositionedSound(origin *Vec3, ent *Edict, channel, soundindex int, volume, attenuation, timeofs float32)

	Configstring(num int, s string)
	Error(text string) // does not return (panics with shared.ComError)

	ModelIndex(name string) int
	SoundIndex(name string) int
	ImageIndex(name string) int
	SetModel(ent *Edict, name string)

	Trace(start, mins, maxs, end *Vec3, passent *Edict, contentmask int32) Trace
	PointContents(point *Vec3) int32
	InPVS(p1, p2 *Vec3) bool
	InPHS(p1, p2 *Vec3) bool
	SetAreaPortalState(portalnum int, open bool)
	AreasConnected(area1, area2 int) bool

	LinkEntity(ent *Edict)
	UnlinkEntity(ent *Edict)
	BoxEdicts(mins, maxs *Vec3, list []*Edict, areatype int) int
	// Pmove runs the shared player movement. Entity ids in pm are edict indices.
	Pmove(pm *pmove.PmoveT)

	Multicast(origin *Vec3, to int)
	Unicast(ent *Edict, reliable bool)
	WriteChar(c int)
	// WriteByteC is gi.WriteByte (renamed: go vet stdmethods reserves WriteByte).
	WriteByteC(c int)
	WriteShort(c int)
	WriteLong(c int)
	WriteFloat(f float32)
	WriteString(s string)
	WritePosition(pos *Vec3)
	WriteDir(dir *Vec3)
	WriteAngle(f float32)

	Cvar(name, value string, flags int) *cvar.Cvar
	CvarSet(name, value string) *cvar.Cvar
	CvarForceSet(name, value string) *cvar.Cvar

	Argc() int
	Argv(n int) string
	Args() string

	AddCommandString(text string)
}

// Export is C game_export_t: the functions the game provides to the engine.
// C: game/game.h:200 game_export_t
type Export interface {
	Init()
	Shutdown()
	SpawnEntities(mapname, entstring, spawnpoint string)

	WriteGame(autosave bool) ([]byte, error)
	ReadGame(data []byte) error
	WriteLevel() ([]byte, error)
	ReadLevel(data []byte) error

	// ClientConnect returns false to reject; the (possibly modified) userinfo is
	// returned, including a "rejmsg" key on rejection, as C edits it in place.
	ClientConnect(ent *Edict, userinfo string) (ok bool, newUserinfo string)
	ClientBegin(ent *Edict)
	ClientUserinfoChanged(ent *Edict, userinfo string)
	ClientDisconnect(ent *Edict)
	ClientCommand(ent *Edict)
	ClientThink(ent *Edict, cmd *shared.UserCmd)

	RunFrame()
	ServerCommand()

	// Edicts is globals.edicts; NumEdicts is globals.num_edicts.
	Edicts() []Edict
	NumEdicts() int
	MaxEdicts() int
}
