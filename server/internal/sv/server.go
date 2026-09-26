// Package sv ports the Quake 2 server (server/*.c). One *Server is one game
// instance: the C globals sv, svs, ge, sv_client, sv_player, the cvar and
// command registries, the collision state and the random generator all live
// on it. A Server is not safe for concurrent use; internal/host owns it from a
// single goroutine.
package sv

import (
	"fmt"
	"io"
	"strings"
	"time"

	"quake2web/server/internal/cmodel"
	"quake2web/server/internal/game"
	qnet "quake2web/server/internal/net"
	"quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/cmd"
	"quake2web/server/internal/qcommon/crand"
	"quake2web/server/internal/qcommon/cvar"
	"quake2web/server/internal/qcommon/msg"
	"quake2web/server/internal/qcommon/shared"
	"quake2web/server/internal/world"
)

// C: server/server.h:31
const MAX_MASTERS = 8

// server_state_t. C: server/server.h:33
const (
	ss_dead    = iota // no map loaded
	ss_loading        // spawning level edicts
	ss_game           // actively running
	ss_cinematic
	ss_demo
	ss_pic
)

// client_state_t. C: server/server.h:74
const (
	cs_free      = iota // can be reused for a new connection
	cs_zombie           // client has been disconnected, but don't reuse connection for a couple seconds
	cs_connected        // has been assigned to a client_t, but not in game yet
	cs_spawned          // client is fully in game
)

// C: server/server.h:94
const (
	LATENCY_COUNTS = 16
	RATE_MESSAGES  = 10
)

// MAX_CHALLENGES. C: server/server.h:151
const MAX_CHALLENGES = 1024

// redirect_t. C: server/server.h:271
const (
	RD_NONE = iota
	RD_CLIENT
	RD_PACKET
)

// SV_OUTPUTBUF_LENGTH. C: server/server.h:272
const SV_OUTPUTBUF_LENGTH = q2const.MAX_MSGLEN - 16

// MAX_TOKEN_CHARS is qcommon.h's max length of an individual token.
const MAX_TOKEN_CHARS = 128

// ServerT is C server_t: the per-level state.
// C: server/server.h:43 server_t
type ServerT struct {
	State int // precache commands are only valid during load

	AttractLoop bool // running cinematics and demos for the local system only
	LoadGame    bool // client begins should reuse existing entity

	Time     uint32 // always sv.framenum * 100 msec
	FrameNum int

	Name   string // map name, or cinematic name
	Models [q2const.MAX_MODELS]*shared.CModel

	ConfigStrings ConfigStrings
	Baselines     [q2const.MAX_EDICTS]shared.EntityState

	// the multicast buffer is used to send a message to a set of clients
	// it is only used to marshall data until SV_Multicast is called
	Multicast    msg.SizeBuf
	multicastBuf [q2const.MAX_MSGLEN]byte

	// demo server information
	DemoFile []byte // whole demo, read sequentially
	demoPos  int
	demoOpen bool
	TimeDemo bool // don't time sync
}

// ConfigStrings is C char configstrings[MAX_CONFIGSTRINGS][MAX_QPATH] kept as
// one flat array, so strcpy of a long string (CS_STATUSBAR) runs into the
// following slots exactly like in C.
type ConfigStrings struct {
	b [q2const.MAX_CONFIGSTRINGS * q2const.MAX_QPATH]byte
}

// Get returns the C string starting at slot i.
func (c *ConfigStrings) Get(i int) string {
	return cString(c.b[i*q2const.MAX_QPATH:])
}

// Empty reports configstrings[i][0] == 0.
func (c *ConfigStrings) Empty(i int) bool { return c.b[i*q2const.MAX_QPATH] == 0 }

// Set is strcpy(configstrings[i], v) (bounded by the end of the array:
// memory-safety fix).
func (c *ConfigStrings) Set(i int, v string) {
	v = cString([]byte(v))
	o := i * q2const.MAX_QPATH
	n := copy(c.b[o:], v)
	if o+n < len(c.b) {
		c.b[o+n] = 0
	} else {
		c.b[len(c.b)-1] = 0
	}
}

// SetN is strncpy(configstrings[i], v, MAX_QPATH): zero padded, no
// terminator when v is MAX_QPATH chars or longer.
func (c *ConfigStrings) SetN(i int, v string) {
	v = cString([]byte(v))
	o := i * q2const.MAX_QPATH
	slot := c.b[o : o+q2const.MAX_QPATH]
	n := copy(slot, v)
	for j := n; j < len(slot); j++ {
		slot[j] = 0
	}
}

// Sprintf is Com_sprintf(configstrings[i], MAX_QPATH, ...): truncated to
// MAX_QPATH-1 characters.
func (c *ConfigStrings) Sprintf(i int, format string, args ...any) {
	v := fmt.Sprintf(format, args...)
	if len(v) > q2const.MAX_QPATH-1 {
		v = v[:q2const.MAX_QPATH-1]
	}
	c.Set(i, v)
}

// Raw returns the underlying bytes (for savegames).
func (c *ConfigStrings) Raw() []byte { return c.b[:] }

// ClientFrame is C client_frame_t.
// C: server/server.h:84 client_frame_t
type ClientFrame struct {
	AreaBytes   int
	AreaBits    [q2const.MAX_MAP_AREAS / 8]byte // portalarea visibility bits
	PS          shared.PlayerState
	NumEntities int
	FirstEntity int // into the circular sv_packet_entities[]
	SentTime    int // for ping calculations
}

// Client is C client_t.
// C: server/server.h:97 client_t
type Client struct {
	State int

	Userinfo string // name, etc

	LastFrame int            // for delta compression
	LastCmd   shared.UserCmd // for filling in big drops

	CommandMsec int // every seconds this is reset, if user commands exhaust it, assume time cheating

	FrameLatency [LATENCY_COUNTS]int
	Ping         int

	MessageSize   [RATE_MESSAGES]int // used to rate drop packets
	Rate          int
	SurpressCount int // number of messages rate supressed

	Edict        *game.Edict // EDICT_NUM(clientnum+1)
	Name         string      // extracted from userinfo, high bits masked
	MessageLevel int         // for filtering printed messages

	// The datagram is written to by sound calls, prints, temp ents, etc.
	// It can be harmlessly overflowed.
	Datagram    msg.SizeBuf
	datagramBuf [q2const.MAX_MSGLEN]byte

	Frames [q2const.UPDATE_BACKUP]ClientFrame // updates can be delta'd from here

	Download      []byte // file being downloaded
	DownloadSize  int    // total bytes (can't use EOF because of paks)
	DownloadCount int    // bytes sent

	LastMessage int // sv.framenum when packet was last received
	LastConnect int

	Challenge int // challenge of this user, randomly generated

	Netchan qnet.Netchan

	index int
}

// Index returns the client slot number (cl - svs.clients).
func (cl *Client) Index() int { return cl.index }

// challenge is C challenge_t.
// C: server/server.h:153 challenge_t
type challenge struct {
	adr       qnet.Addr
	challenge int
	time      int
}

// ServerStatic is C server_static_t: persistant server info.
// C: server/server.h:161 server_static_t
type ServerStatic struct {
	Initialized bool // sv_init has completed
	RealTime    int  // always increasing, no clamping, etc

	MapCmd string // ie: *intro.cin+base

	SpawnCount int // incremented each server start, used to check late spawns

	Clients            []Client // [maxclients->value]
	NumClientEntities  int      // maxclients->value*UPDATE_BACKUP*MAX_PACKET_ENTITIES
	NextClientEntities int      // next client_entity to use
	ClientEntities     []shared.EntityState

	LastHeartbeat int

	challenges [MAX_CHALLENGES]challenge // to prevent invalid IPs from connecting

	// serverrecord values
	DemoFile          io.WriteCloser
	DemoMulticast     msg.SizeBuf
	demoMulticastBuf  [q2const.MAX_MSGLEN]byte
	demoMulticastUsed bool
}

// FileSystem is the part of FS_* the server needs (maps, demos).
type FileSystem interface {
	ReadFile(name string) ([]byte, error)
}

// GameFactory creates the game module for a new game (Sys_GetGameAPI).
type GameFactory func(gi game.Import) game.Export

// Config holds what a Server is created with.
type Config struct {
	FS   FileSystem
	Game GameFactory
	// Maps is an optional shared (read-only data) map cache.
	Maps *MapCache
	// Saves stores savegames; nil uses a fresh in-memory store.
	Saves SaveStore
	// Rand is the instance random generator shared with the game; nil
	// creates a fresh one (srand(1) state).
	Rand *crand.Rand
	// Printf receives the console output (Com_Printf); nil discards it.
	Printf func(format string, args ...any)
	// Clock returns Sys_Milliseconds; nil uses the wall clock since creation.
	Clock func() int
	// Now returns the wall clock for savegame comments; nil uses time.Now.
	Now func() time.Time
	// DemoCreate opens <gamedir>/demos/<name>.dm2 for serverrecord; nil
	// makes serverrecord fail with "couldn't open".
	DemoCreate func(name string) (io.WriteCloser, error)
	// Dedicated is the value of the "dedicated" cvar ("1" for a dedicated
	// server, "0" for a single player / listen style instance).
	Dedicated bool
	// Cvars are "+set name value" early commands applied at init.
	Cvars [][2]string
	// Userinfo, when set, may rewrite a client's userinfo string when it
	// connects and whenever it sends a new one (the host uses it to force
	// the account display name). addr is the client's address. Not in C.
	Userinfo func(addr qnet.Addr, userinfo string) string
	// ClientCommand, when set, is offered every client string command
	// before the engine's own table (s.Cmd holds the tokenized command);
	// returning true swallows it. Not in C.
	ClientCommand func(cl *Client) bool
}

// Server is one game instance.
type Server struct {
	cfg Config

	Cvars *cvar.Registry
	Cmd   *cmd.Cmd
	Rand  *crand.Rand

	SV  ServerT
	SVS ServerStatic

	ge game.Export
	gi *gameImport

	CM    *cmodel.State
	World world.World

	masterAdr [MAX_MASTERS]qnet.Addr

	client  *Client     // sv_client: current client
	player  *game.Edict // sv_player
	netFrom qnet.Addr   // net_from
	netVia  qnet.Sender // transport net_from came through
	netMsg  *msg.SizeBuf

	pmAirAccelerate float32 // pm_airaccelerate

	fatpvs [65536 / 8]byte // 32767 is MAX_MAP_LEAFS

	// Com_Printf redirection (Com_BeginRedirect)
	rdTarget int
	rdBuffer strings.Builder
	rdSize   int

	clockStart time.Time

	// cvars (C globals)
	svPaused, svTimedemo, svEnforcetime, timeout, zombietime    *cvar.Cvar
	rconPassword, allowDownload, allowDownloadPlayers           *cvar.Cvar
	allowDownloadModels, allowDownloadSounds, allowDownloadMaps *cvar.Cvar
	svAirAccelerate, svNoreload, maxclients, svShowclamp        *cvar.Cvar
	hostname, publicServer, svReconnectLimit, dedicated         *cvar.Cvar
	developer, hostSpeeds                                       *cvar.Cvar
	showpackets, showdrop                                       *cvar.Cvar

	// Shutdown is set once the server has been killed (killserver, ERR_DROP).
	killed bool
}

// New creates an instance with the engine cvars and operator commands
// registered, like Qcommon_Init + SV_Init. No map is loaded.
func New(cfg Config) *Server {
	s := &Server{cfg: cfg}
	if cfg.Saves == nil {
		s.cfg.Saves = NewMemSaveStore()
	}
	if cfg.Rand != nil {
		s.Rand = cfg.Rand
	} else {
		s.Rand = crand.New(1)
	}
	s.clockStart = time.Now()
	s.Cvars = cvar.New()
	s.Cvars.Printf = s.Printf
	s.Cvars.ServerState = func() int { return s.SV.State }
	s.Cmd = cmd.New(s.Cvars)
	s.Cmd.Printf = s.Printf
	if cfg.FS != nil {
		s.Cmd.LoadFile = cfg.FS.ReadFile
	}
	s.Cmd.Init()
	s.Cvars.Init(s.Cmd)

	// C: qcommon/common.c:1449 Qcommon_Init (the cvars the server reads)
	s.hostSpeeds = s.Cvars.Get("host_speeds", "0", 0)
	s.developer = s.Cvars.Get("developer", "0", 0)
	ded := "0"
	if cfg.Dedicated {
		ded = "1"
	}
	s.dedicated = s.Cvars.Get("dedicated", ded, q2const.CVAR_NOSET)
	s.Cvars.Get("version", fmt.Sprintf("%4.2f %s %s %s", q2const.VERSION, "x86_64", "Sep 25 2026", "Linux"),
		q2const.CVAR_SERVERINFO|q2const.CVAR_NOSET)
	// C: qcommon/net_chan.c:83 Netchan_Init
	s.showpackets = s.Cvars.Get("showpackets", "0", 0)
	s.showdrop = s.Cvars.Get("showdrop", "0", 0)
	s.Cvars.Get("qport", fmt.Sprintf("%d", s.Milliseconds()&0xffff), q2const.CVAR_NOSET)

	for _, kv := range cfg.Cvars {
		s.Cvars.Set(kv[0], kv[1])
	}
	s.World.Models = &s.SV.Models
	s.World.WorldEdict = s.worldEdict
	s.World.Loading = func() bool { return s.SV.State == ss_loading }
	s.World.Printf = s.Printf
	s.World.DPrintf = s.DPrintf
	s.svInit()
	s.netMsg = msg.NewSizeBuf(q2const.MAX_MSGLEN)
	return s
}

// Milliseconds is Sys_Milliseconds / curtime.
func (s *Server) Milliseconds() int {
	if s.cfg.Clock != nil {
		return s.cfg.Clock()
	}
	return int(time.Since(s.clockStart) / time.Millisecond)
}

// Game returns the loaded game module (ge), or nil.
func (s *Server) Game() game.Export { return s.ge }

// Killed reports whether the server has been shut down (no game running).
func (s *Server) Killed() bool { return s.killed }

func (s *Server) now() time.Time {
	if s.cfg.Now != nil {
		return s.cfg.Now()
	}
	return time.Now()
}

// Printf is Com_Printf (with rcon redirection).
// C: qcommon/common.c:109 Com_Printf
func (s *Server) Printf(format string, args ...any) {
	text := fmt.Sprintf(format, args...)
	if s.rdTarget != RD_NONE {
		if len(text)+s.rdBuffer.Len() > s.rdSize-1 {
			s.flushRedirect(s.rdTarget, s.rdBuffer.String())
			s.rdBuffer.Reset()
		}
		s.rdBuffer.WriteString(text)
		return
	}
	if s.cfg.Printf != nil {
		s.cfg.Printf("%s", text)
	}
}

// DPrintf is Com_DPrintf.
// C: qcommon/common.c:155 Com_DPrintf
func (s *Server) DPrintf(format string, args ...any) {
	if s.developer == nil || s.developer.Value == 0 {
		return
	}
	s.Printf(format, args...)
}

// beginRedirect is Com_BeginRedirect.
// C: qcommon/common.c:84 Com_BeginRedirect
func (s *Server) beginRedirect(target int, size int) {
	s.rdTarget = target
	s.rdSize = size
	s.rdBuffer.Reset()
}

// endRedirect is Com_EndRedirect.
// C: qcommon/common.c:96 Com_EndRedirect
func (s *Server) endRedirect() {
	s.flushRedirect(s.rdTarget, s.rdBuffer.String())
	s.rdTarget = RD_NONE
	s.rdBuffer.Reset()
}

// edictNum is EDICT_NUM.
func (s *Server) edictNum(n int) *game.Edict {
	return &s.ge.Edicts()[n]
}

// numForEdict is NUM_FOR_EDICT.
func numForEdict(e *game.Edict) int { return e.Index }

func (s *Server) maxClients() int { return int(s.maxclients.Value) }

// ExecuteText runs console text for this instance (Cbuf_AddText +
// Cbuf_Execute), with Com_Error recovery like Qcommon_Frame.
func (s *Server) ExecuteText(text string) (err error) {
	defer s.recoverError(&err)
	s.Cmd.Cbuf_AddText(text)
	s.Cmd.Cbuf_Execute()
	return nil
}

// recoverError handles a Com_Error panic at a frame boundary like
// Qcommon_Frame's setjmp: ERR_DROP (and ERR_FATAL, which exits the C
// program) shut the server down; ERR_DISCONNECT only affects a client.
// C: qcommon/common.c:175 Com_Error
func (s *Server) recoverError(errp *error) {
	r := recover()
	if r == nil {
		return
	}
	ce, ok := r.(shared.ComError)
	if !ok {
		panic(r)
	}
	*errp = ce
	switch ce.Code {
	case q2const.ERR_DISCONNECT:
		// CL_Drop only; the server keeps running
		*errp = nil
	case q2const.ERR_DROP:
		s.Printf("********************\nERROR: %s\n********************\n", ce.Msg)
		s.svShutdown(fmt.Sprintf("Server crashed: %s\n", ce.Msg), false)
		s.killed = true
	default:
		s.Printf("Error: %s\n", ce.Msg)
		s.svShutdown(fmt.Sprintf("Server fatal crashed: %s\n", ce.Msg), false)
		s.killed = true
	}
}

// Spawned reports whether the client is fully in game (cs_spawned).
func (cl *Client) Spawned() bool { return cl.State == cs_spawned }

// InUse reports whether the slot holds a connecting or spawned client
// (cs_connected or cs_spawned).
func (cl *Client) InUse() bool { return cl.State >= cs_connected }

// InGame reports whether a level is running (sv.state == ss_game).
func (s *Server) InGame() bool { return s.SVS.Initialized && s.SV.State == ss_game }
