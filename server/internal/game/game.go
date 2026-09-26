package game

import (
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"

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

// guard is deferred by every game_export_t entry point. A Go runtime panic
// inside the game module (a nil edict or an index out of range on a
// malformed map, a tampered save or an unforeseen input, where the C game
// dereferences NULL or reads out of bounds) is turned into gi.error, which
// ends this server instance with ERR_DROP like any other game error instead
// of crashing the whole process that hosts every instance. gi.error panics
// (shared.ComError) pass through unchanged. Valid input never gets here, so
// this does not change any C-faithful behaviour.
func (g *Game) guard() {
	r := recover()
	if r == nil {
		return
	}
	if _, ok := r.(shared.ComError); ok {
		panic(r)
	}
	g.gi.Error(internalError(r))
}

// guardErr is guard for the entry points that return an error (ReadGame,
// ReadLevel, WriteGame, WriteLevel): the panic becomes the returned error.
func guardErr(errp *error) {
	r := recover()
	if r == nil {
		return
	}
	if ce, ok := r.(shared.ComError); ok {
		*errp = ce
		return
	}
	*errp = errors.New(internalError(r))
}

// internalError describes a recovered panic with the game function and
// source line that raised it.
func internalError(r any) string {
	pcs := make([]uintptr, 32)
	n := runtime.Callers(3, pcs)
	frames := runtime.CallersFrames(pcs[:n])
	for {
		f, more := frames.Next()
		if strings.HasPrefix(f.Function, "quake2web/server/internal/game.") &&
			!strings.HasSuffix(f.Function, ".guard") && !strings.HasSuffix(f.Function, ".guardErr") {
			return fmt.Sprintf("internal error: %v at %s:%d %s", r, filepath.Base(f.File), f.Line,
				strings.TrimPrefix(f.Function, "quake2web/server/internal/game."))
		}
		if !more {
			return fmt.Sprintf("internal error: %v", r)
		}
	}
}

// maxCallDepth bounds the nesting of the entity callback chains that the C
// game recurses through without any limit: a target loop (trigger_relay A
// targets B, B targets A: G_UseTargets -> use -> G_UseTargets ...) or a
// func_train on zero-length wait-0 path_corner loop (train_next -> Move_Calc
// -> Move_Done -> train_wait -> train_next ...). C overflows its stack and
// crashes (SIGSEGV); in Go a goroutine stack overflow is a fatal error that
// can not be recovered and would kill every instance of the process. A
// non-looping chain nests at most once per entity (MAX_EDICTS = 1024), so
// the limit is never reached by a map the C game can run.
const maxCallDepth = 4096

// enterCall enters one level of a recursive callback chain; past
// maxCallDepth it raises gi.error (ERR_DROP of this instance). Pair with
// defer g.leaveCall().
func (g *Game) enterCall(what string) {
	g.callDepth++
	if g.callDepth > maxCallDepth {
		g.callDepth = 0
		g.error("%s: entity loop, recursion deeper than %d", what, maxCallDepth)
	}
}

// leaveCall leaves a level entered by enterCall.
func (g *Game) leaveCall() {
	if g.callDepth > 0 {
		g.callDepth--
	}
}
