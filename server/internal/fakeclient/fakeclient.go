// Package fakeclient is a headless protocol-34 Quake 2 client for tests and
// the agent. It ports the protocol side of client/cl_main.c (connection
// handshake), client/cl_parse.c, client/cl_ents.c (frame / delta entity
// parsing) and client/cl_input.c CL_SendCmd over any net.Conn.
//
// Connect, WaitActive and Poll receive from the connection themselves. A
// driver that owns the datagram loop (a single-goroutine lockstep server)
// uses BeginConnect, Feed and Tick instead. The Options hooks (Clock,
// OnServerMessage, Passive, MaxHistory) and RequestFullFrame are opt-in:
// with a zero Options the client behaves, and sends, exactly as without them.
package fakeclient

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"quake2web/server/internal/net"
	"quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/cmd"
	"quake2web/server/internal/qcommon/cvar"
	"quake2web/server/internal/qcommon/msg"
	"quake2web/server/internal/qcommon/shared"
)

// Client-side sizes from client/client.h.
const (
	CMD_BACKUP         = 64   // C: client/client.h:106 CMD_BACKUP (allow a lot of command backups for very fast systems)
	MAX_PARSE_ENTITIES = 1024 // C: client/client.h:185 MAX_PARSE_ENTITIES
)

// ConnState is C connstate_t.
// C: client/client.h:198 connstate_t
type ConnState int

// Connection states.
const (
	CaUninitialized ConnState = iota
	CaDisconnected            // not talking to a server
	CaConnecting              // sending request packets to the server
	CaConnected               // netchan_t established, waiting for svc_serverdata
	CaActive                  // game views should be displayed
)

// ErrDisconnected is returned once the server dropped the client.
var ErrDisconnected = errors.New("fakeclient: disconnected")

// Options configures a Client. The zero value is the plain test client; every
// other field is an opt-in hook that leaves the protocol behavior unchanged
// unless it is set.
type Options struct {
	Qport    int    // netchan qport; 0 picks a pseudo random one
	Userinfo string // default "\name\fakeclient\skin\male/grunt\rate\25000\msg\1\hand\0\fov\90"
	NoDelta  bool   // cl_nodelta: always request uncompressed frames
	Printf   func(format string, args ...any)

	// Clock, when set, replaces Sys_Milliseconds (the client's curtime,
	// which feeds the netchan and the resend / keepalive timers), so a
	// driver with a virtual clock (a lockstep server) controls the client's
	// notion of time. nil uses the wall clock since New.
	Clock func() int

	// OnServerMessage, when set, is called for every accepted sequenced
	// server packet after CL_ParseServerMessage parsed it and before the
	// commands it stuffed are executed: the point where C calls
	// CL_WriteDemoMessage. payload is the packet without its 8 byte netchan
	// header (exactly what CL_WriteDemoMessage writes to a .dm2) and spans
	// locates every svc command in it. Both are fresh copies owned by the
	// callee. A packet whose parsing ends in a Com_Error is not reported,
	// like C, which never reaches CL_WriteDemoMessage then.
	OnServerMessage func(c *Client, payload []byte, spans []Span)

	// Passive makes a client that only parses server messages handed to
	// FeedPayload (demo blocks): stuffed text is recorded but never
	// executed, downloads are not answered and nothing is ever sent (Feed
	// refuses datagrams).
	Passive bool

	// MaxHistory bounds each event history slice (Prints, CenterPrints,
	// StuffTexts, Layouts, Sounds, TempEnts, TempEntEvents, MuzzleFlashes,
	// Downloads, OOB). A history that reaches 2*MaxHistory entries drops
	// its older half, so it always holds the newest MaxHistory entries (all
	// of them while there are fewer) and never more than 2*MaxHistory; the
	// trimming costs amortized O(1) per event. Client.Counts keeps counting
	// every event, and NewSince returns the entries recorded after an
	// earlier count. 0 keeps everything.
	MaxHistory int
}

// HistoryCounts counts the events each history slice of a Client recorded
// since New, including the ones MaxHistory discarded since. Inventory counts
// the svc_inventory messages parsed (each one replaces Client.Inventory), so
// a reader can tell a fresh inventory from an unchanged array.
type HistoryCounts struct {
	Prints, CenterPrints, StuffTexts, Layouts uint64
	Sounds, TempEnts, TempEntEvents           uint64
	MuzzleFlashes, Downloads, OOB             uint64
	Inventory                                 uint64
}

// NewSince returns the entries of history s recorded after its count was
// seen, given its current count total (a HistoryCounts field), and how many
// of those MaxHistory already discarded. Entries are never lost while at
// most MaxHistory events arrive between two calls.
func NewSince[T any](s []T, total, seen uint64) (fresh []T, lost uint64) {
	if total <= seen {
		return nil, 0
	}
	n := total - seen
	if n > uint64(len(s)) {
		return s, n - uint64(len(s))
	}
	return s[len(s)-int(n):], 0
}

// Span locates one svc command of a server message payload: Cmd is the svc_*
// byte and payload[Start:End] the command including that byte (svc_frame
// spans its playerinfo and packetentities as well).
type Span struct {
	Cmd        int32
	Start, End int
}

// ServerData is what svc_serverdata carried.
type ServerData struct {
	Protocol    int32
	ServerCount int32
	AttractLoop int32
	GameDir     string
	PlayerNum   int32
	LevelName   string
}

// Frame is C frame_t.
// C: client/client.h:27 frame_t
type Frame struct {
	Valid         bool // cleared if delta parsing was invalid
	ServerFrame   int32
	ServerTime    int32 // server time the message is valid for (in msec)
	DeltaFrame    int32
	SurpressCount int32
	AreaBits      [q2const.MAX_MAP_AREAS / 8]byte // portalarea visibility bits
	AreaBytes     int                             // length byte sent by the server
	PlayerState   shared.PlayerState
	NumEntities   int
	ParseEntities int // non-masked index into cl_parse_entities array
}

// CEntity is the protocol part of C centity_t.
// C: client/client.h:43 centity_t
type CEntity struct {
	Baseline    shared.EntityState // delta from this if not from a previous frame
	HasBaseline bool
	Current     shared.EntityState
	Prev        shared.EntityState // will always be valid, but might just be a copy of current
	ServerFrame int32              // if not current, this ent isn't in the frame
}

// Download records a svc_download message.
type Download struct {
	Size    int32
	Percent int32
}

// Sound records a svc_sound message.
type Sound struct {
	Flags, SoundNum, Ent, Channel int32
	Volume, Attenuation, Ofs      float32
	Pos                           *shared.Vec3
}

// TempEnt records the fields CL_ParseTEnt reads from a svc_temp_entity. Which
// ones are set depends on Type (see parseTEnt); the rest stay zero.
type TempEnt struct {
	Type      int32
	Pos       shared.Vec3 // origin / beam or trail start
	Pos2      shared.Vec3 // beam or trail end (TE_BLUEHYPERBLASTER: the second vector C reads into "dir")
	Offset    shared.Vec3 // TE_GRAPPLE_CABLE beam offset
	Dir       shared.Vec3 // surface normal / direction (MSG_ReadDir)
	Ent       int32       // beam owner, TE_LIGHTNING source, TE_FLASHLIGHT entity, steam / widow id
	Ent2      int32       // TE_LIGHTNING destination
	Count     int32       // particle count (splash, sparks, steam)
	Color     int32       // splash color / laser sparks color / forcewall / steam color
	Magnitude int32       // TE_STEAM magnitude
	Wait      int32       // TE_STEAM sustain interval (id != -1 only)
}

// MuzzleFlash records a svc_muzzleflash (Monster false: a player weapon,
// Weapon is the MZ_* byte including MZ_SILENCED) or svc_muzzleflash2
// (Monster true: Weapon is the MZ2_* flash number).
type MuzzleFlash struct {
	Ent, Weapon int32
	Monster     bool
}

// Client is one fake client connection (C client_static_t + client_state_t).
type Client struct {
	conn  net.Conn
	opt   Options
	start time.Time

	cvars *cvar.Registry
	cmd   *cmd.Cmd

	State       ConnState
	Netchan     net.Netchan
	Challenge   int
	connectTime int
	Userinfo    string
	userinfoMod bool

	ServerData    ServerData
	ConfigStrings [q2const.MAX_CONFIGSTRINGS]string
	Entities      [q2const.MAX_EDICTS]CEntity
	NumBaselines  int

	Frame         Frame // cl.frame: the last parsed frame (possibly invalid)
	Frames        [q2const.UPDATE_BACKUP]Frame
	parseEntities int
	ParseEnts     [MAX_PARSE_ENTITIES]shared.EntityState

	cmds [CMD_BACKUP]shared.UserCmd

	FramesParsed  int
	ValidFrames   int
	BeginSent     bool
	Disconnected  bool
	DisconnectMsg string

	Prints        []string
	CenterPrints  []string
	StuffTexts    []string
	Layouts       []string
	Sounds        []Sound
	TempEnts      []int32
	TempEntEvents []TempEnt
	MuzzleFlashes []MuzzleFlash
	Downloads     []Download
	Inventory     [q2const.MAX_ITEMS]int32
	OOB           []string      // connectionless messages received
	Counts        HistoryCounts // events recorded into each history since New
	Reliables     int           // number of reliable (stringcmd/userinfo) messages queued

	levelGen    int    // number of svc_serverdata parsed
	demoWaiting bool   // cls.demowaiting: ask for an uncompressed frame
	wantSpans   bool   // record spans while parsing the current message
	spans       []Span // svc command spans of the current message (absolute offsets)
}

// New creates a client talking over conn.
func New(conn net.Conn, opt Options) *Client {
	c := &Client{conn: conn, opt: opt, start: time.Now()}
	if c.opt.Qport == 0 {
		c.opt.Qport = int(time.Now().UnixNano() & 0xffff)
	}
	c.Userinfo = opt.Userinfo
	if c.Userinfo == "" {
		c.Userinfo = `\name\fakeclient\skin\male/grunt\rate\25000\msg\1\hand\0\fov\90`
	}
	c.cvars = cvar.New()
	c.cmd = cmd.New(c.cvars)
	c.cmd.Printf = c.printf
	c.cmd.Init()
	c.cvars.Init(c.cmd)
	c.cmd.ForwardToServer = c.cmdForwardToServer
	c.cmd.AddCommand("cmd", c.forwardToServer_f)
	c.cmd.AddCommand("precache", c.precache_f)
	c.cmd.AddCommand("changing", c.changing_f)
	c.cmd.AddCommand("reconnect", c.reconnect_f)
	c.cmd.AddCommand("disconnect", c.disconnect_f)
	c.Netchan.Printf = c.printf
	c.State = CaDisconnected
	return c
}

// NewPassive creates a Passive client (opt.Passive is forced on) without a
// connection, for parsing recorded server messages with FeedPayload.
func NewPassive(opt Options) *Client {
	opt.Passive = true
	if opt.Qport == 0 {
		opt.Qport = 1 // never used: a passive client sends nothing
	}
	return New(nil, opt)
}

func (c *Client) printf(format string, args ...any) {
	if c.opt.Printf != nil {
		c.opt.Printf(format, args...)
	}
}

// curtime is Sys_Milliseconds relative to the client's creation, or
// Options.Clock.
func (c *Client) curtime() int {
	if c.opt.Clock != nil {
		return c.opt.Clock()
	}
	return int(time.Since(c.start) / time.Millisecond)
}

// LevelGen returns the number of svc_serverdata messages parsed so far: it
// changes exactly when a new level (map change, load, reconnect) begins.
func (c *Client) LevelGen() int { return c.levelGen }

// RequestFullFrame makes the client ask for uncompressed frames (lastframe
// -1) until the next one arrives, like cls.demowaiting after CL_Record_f.
func (c *Client) RequestFullFrame() { c.demoWaiting = true }

// WaitingFullFrame reports whether a requested uncompressed frame has not
// arrived yet.
func (c *Client) WaitingFullFrame() bool { return c.demoWaiting }

// MapName returns the client's current level: the BSP name of a game level
// ("maps/demo1.bsp" -> "demo1"), otherwise the serverdata level name (a
// cinematic or picture such as "victory.pcx"), or "" before serverdata.
func (c *Client) MapName() string {
	if m := c.ConfigStrings[q2const.CS_MODELS+1]; strings.HasPrefix(m, "maps/") && strings.HasSuffix(m, ".bsp") {
		return strings.TrimSuffix(strings.TrimPrefix(m, "maps/"), ".bsp")
	}
	return c.ServerData.LevelName
}

// appendHistory appends v to a history, counts it in *count and applies
// MaxHistory (limit <= 0: unbounded): when s already holds 2*limit
// entries, its newest limit-1 are moved to the front first, so the backing
// array never grows past 2*limit and each event costs amortized O(1).
func appendHistory[T any](s []T, v T, limit int, count *uint64) []T {
	*count++
	if limit > 0 && len(s) >= 2*limit {
		n := copy(s, s[len(s)-(limit-1):])
		clear(s[n:])
		s = s[:n]
	}
	return append(s, v)
}

// Qport returns the qport the client uses.
func (c *Client) Qport() int { return c.opt.Qport }

// ---------------------------------------------------------------------------
// Connection (client/cl_main.c)

// Connect starts the handshake: getchallenge, connect, and waits until the
// server answered client_connect (the "new" command is then queued).
func (c *Client) Connect(ctx context.Context) error {
	c.BeginConnect()
	for c.State == CaConnecting {
		if err := c.checkForResend(); err != nil {
			return err
		}
		if err := c.readPacket(ctx, 100*time.Millisecond); err != nil {
			return err
		}
	}
	if c.Disconnected {
		return ErrDisconnected
	}
	return nil
}

// BeginConnect starts connecting without waiting (the state change of
// CL_Connect_f): the next Tick sends getchallenge. A driver that delivers the
// datagrams itself then runs the handshake with Tick and Feed.
// C: client/cl_main.c:494 CL_Connect_f
func (c *Client) BeginConnect() {
	c.State = CaConnecting
	c.connectTime = -99999 // CL_CheckForResend() will fire immediately
}

// Tick runs the client's periodic connection work once: resend the
// challenge request while connecting (CL_CheckForResend) and flush reliable
// commands or send a keepalive while connected (the ca_connected branch of
// CL_SendCmd). It sends nothing once active (use SendCmd) or when passive.
func (c *Client) Tick() error {
	if c.opt.Passive {
		return nil
	}
	if err := c.checkForResend(); err != nil {
		return err
	}
	c.sendConnected()
	return nil
}

// Handshake runs Connect and then WaitActive.
func (c *Client) Handshake(ctx context.Context) error {
	if err := c.Connect(ctx); err != nil {
		return err
	}
	return c.WaitActive(ctx)
}

// WaitActive runs the client until the first valid svc_frame was parsed after
// "begin" (cls.state == ca_active).
func (c *Client) WaitActive(ctx context.Context) error {
	for c.State != CaActive {
		if c.Disconnected {
			return ErrDisconnected
		}
		if err := c.checkForResend(); err != nil {
			return err
		}
		c.sendConnected()
		if err := c.readPacket(ctx, 50*time.Millisecond); err != nil {
			return err
		}
	}
	return nil
}

// Poll receives and parses packets for up to d (keeping the connection alive
// while not yet active). It returns early only on errors.
func (c *Client) Poll(ctx context.Context, d time.Duration) error {
	deadline := time.Now().Add(d)
	for {
		left := time.Until(deadline)
		if left <= 0 {
			return nil
		}
		if err := c.checkForResend(); err != nil {
			return err
		}
		c.sendConnected()
		if err := c.readPacket(ctx, left); err != nil {
			return err
		}
	}
}

// C: client/cl_main.c:461 CL_CheckForResend
func (c *Client) checkForResend() error {
	if c.State != CaConnecting {
		return nil
	}
	if c.curtime()-c.connectTime < 3000 {
		return nil
	}
	c.connectTime = c.curtime() // for retransmit requests
	c.printf("Connecting...\n")
	net.OutOfBandPrint(net.ConnSender{C: c.conn}, net.Addr{}, "getchallenge\n")
	return nil
}

// C: client/cl_main.c:419 CL_SendConnectPacket
func (c *Client) sendConnectPacket() {
	c.userinfoMod = false
	net.OutOfBandPrint(net.ConnSender{C: c.conn}, net.Addr{}, fmt.Sprintf("connect %d %d %d \"%s\"\n",
		q2const.PROTOCOL_VERSION, c.opt.Qport, c.Challenge, c.Userinfo))
}

// sendConnected is the ca_connected branch of CL_SendCmd: flush reliable
// commands (or keep alive once a second).
func (c *Client) sendConnected() {
	if c.State != CaConnected {
		return
	}
	if c.Netchan.Message.CurSize != 0 || c.curtime()-c.Netchan.LastSent > 1000 {
		c.Netchan.Transmit(nil, c.curtime())
	}
}

// readPacket waits up to d for one datagram and processes it
// (CL_ReadPackets for one packet), then executes stuffed commands.
func (c *Client) readPacket(ctx context.Context, d time.Duration) error {
	rctx, cancel := context.WithTimeout(ctx, d)
	defer cancel()
	data, rerr := c.conn.Recv(rctx)
	if rerr != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if errors.Is(rerr, context.DeadlineExceeded) {
			return nil
		}
		return rerr
	}
	return c.Feed(data)
}

// ErrPassive is returned by Feed on a Passive client, which has no
// connection to answer connectionless packets on (use FeedPayload).
var ErrPassive = errors.New("fakeclient: Feed on a passive client (use FeedPayload)")

// Feed processes one received datagram (the body of CL_ReadPackets for one
// packet) and then executes the stuffed commands, exactly what Poll does
// with each datagram it receives. It is for drivers that deliver datagrams
// themselves (a single-goroutine lockstep loop). A Com_Error raised while
// parsing disconnects the client: ErrDisconnected for svc_disconnect, any
// other drop as an error carrying its message. A Passive client refuses
// datagrams with ErrPassive.
func (c *Client) Feed(data []byte) (err error) {
	if c.opt.Passive {
		return ErrPassive
	}
	defer c.recoverComError(&err)
	c.processPacket(data)
	c.cmd.Cbuf_Execute()
	if c.Disconnected {
		return ErrDisconnected
	}
	return nil
}

// FeedPayload parses one server message that has no netchan header (a .dm2
// block, or a payload passed to OnServerMessage) on a Passive client and
// returns the spans of its svc commands. OnServerMessage, when set, is
// called for it as for a received packet. Errors are reported like Feed.
func (c *Client) FeedPayload(payload []byte) (spans []Span, err error) {
	if !c.opt.Passive {
		return nil, errors.New("fakeclient: FeedPayload needs a passive client")
	}
	defer c.recoverComError(&err)
	m := msg.NewReader(payload)
	c.parseMessage(m, 0)
	spans = c.spans
	c.spans = nil
	if c.Disconnected {
		return spans, ErrDisconnected
	}
	return spans, nil
}

// recoverComError turns a Com_Error raised while handling server input into
// a disconnect (the client side of Com_Error's longjmp). It must be deferred
// directly.
func (c *Client) recoverComError(errp *error) {
	r := recover()
	if r == nil {
		return
	}
	ce, ok := r.(shared.ComError)
	if !ok {
		panic(r)
	}
	c.wantSpans = false
	c.spans = nil
	c.Disconnected = true
	c.DisconnectMsg = ce.Msg
	c.State = CaDisconnected
	if ce.Code == q2const.ERR_DISCONNECT {
		*errp = ErrDisconnected
	} else {
		*errp = fmt.Errorf("fakeclient: %s", ce.Msg)
	}
}

// C: client/cl_main.c:987 CL_ReadPackets (body for one packet)
func (c *Client) processPacket(data []byte) {
	m := msg.NewReader(data)
	// remote command packet
	if len(data) >= 4 && data[0] == 0xff && data[1] == 0xff && data[2] == 0xff && data[3] == 0xff {
		c.connectionlessPacket(m)
		return
	}
	if c.State == CaDisconnected || c.State == CaConnecting {
		return // dump it if not connected
	}
	if len(data) < 8 {
		c.printf("Runt packet\n")
		return
	}
	if !c.Netchan.Process(m, c.curtime()) {
		return // wasn't accepted for some reason
	}
	if c.opt.OnServerMessage == nil {
		c.parseServerMessage(m)
		return
	}
	// the first eight bytes are just packet sequencing stuff
	// (C: client/cl_main.c:113 CL_WriteDemoMessage)
	c.parseMessage(m, 8)
	c.spans = nil
}

// parseMessage runs CL_ParseServerMessage recording the svc command spans
// and then reports the message to OnServerMessage. base is the offset of the
// payload in m (the netchan header length).
func (c *Client) parseMessage(m *msg.SizeBuf, base int) {
	c.wantSpans = true
	c.spans = nil
	c.parseServerMessage(m)
	c.wantSpans = false
	for i := range c.spans {
		c.spans[i].Start -= base
		c.spans[i].End -= base
	}
	if c.opt.OnServerMessage != nil {
		payload := append([]byte(nil), m.Data[base:m.CurSize]...)
		c.opt.OnServerMessage(c, payload, append([]Span(nil), c.spans...))
	}
}

// C: client/cl_main.c:866 CL_ConnectionlessPacket
func (c *Client) connectionlessPacket(m *msg.SizeBuf) {
	m.MSG_BeginReading()
	m.MSG_ReadLong() // skip the -1
	s := m.MSG_ReadStringLine()
	c.OOB = appendHistory(c.OOB, s, c.opt.MaxHistory, &c.Counts.OOB)
	c.cmd.TokenizeString(s, false)
	cmdName := c.cmd.Argv(0)
	c.printf("OOB: %s\n", cmdName)

	switch cmdName {
	case "client_connect":
		if c.State == CaConnected {
			c.printf("Dup connect received.  Ignored.\n")
			return
		}
		c.Netchan.Setup(q2const.NS_CLIENT, net.Addr{}, net.ConnSender{C: c.conn}, c.opt.Qport, c.curtime())
		c.Netchan.Message.MSG_WriteChar(q2const.Clc_stringcmd)
		c.Netchan.Message.MSG_WriteString("new")
		c.State = CaConnected
	case "info":
	case "cmd":
		// remote command packets are only accepted from the local host
		c.printf("Command packet from remote host.  Ignored.\n")
	case "print":
		s := m.MSG_ReadString()
		c.Prints = appendHistory(c.Prints, s, c.opt.MaxHistory, &c.Counts.Prints)
		c.printf("%s", s)
		if c.State == CaConnecting && c.Challenge != 0 {
			// a rejected connect ("Server is full.", "Bad challenge." ...)
			c.Disconnected = true
			c.DisconnectMsg = s
		}
	case "ping":
		net.OutOfBandPrint(net.ConnSender{C: c.conn}, net.Addr{}, "ack")
	case "challenge":
		c.Challenge = int(shared.Atoi(c.cmd.Argv(1)))
		c.sendConnectPacket()
	case "echo":
		net.OutOfBandPrint(net.ConnSender{C: c.conn}, net.Addr{}, c.cmd.Argv(1))
	default:
		c.printf("Unknown command.\n")
	}
}

// ---------------------------------------------------------------------------
// Console commands the server may stuff

// C: client/cl_main.c:284 Cmd_ForwardToServer
func (c *Client) cmdForwardToServer() {
	name := c.cmd.Argv(0)
	if c.State <= CaConnected || name[0] == '-' || name[0] == '+' {
		c.printf("Unknown command \"%s\"\n", name)
		return
	}
	c.Netchan.Message.MSG_WriteByte(q2const.Clc_stringcmd)
	c.Netchan.Message.SZ_Print(name)
	if c.cmd.Argc() > 1 {
		c.Netchan.Message.SZ_Print(" ")
		c.Netchan.Message.SZ_Print(c.cmd.Args())
	}
	c.Reliables++
}

// C: client/cl_main.c:345 CL_ForwardToServer_f
func (c *Client) forwardToServer_f() {
	if c.State != CaConnected && c.State != CaActive {
		c.printf("Can't \"%s\", not connected\n", c.cmd.Argv(0))
		return
	}
	// don't forward the first argument
	if c.cmd.Argc() > 1 {
		c.Netchan.Message.MSG_WriteByte(q2const.Clc_stringcmd)
		c.Netchan.Message.SZ_Print(c.cmd.Args())
		c.Reliables++
	}
}

// precache_f is CL_Precache_f followed by CL_RequestNextDownload with
// downloading disabled: it answers "begin <spawncount>".
// C: client/cl_main.c:1375 CL_Precache_f, client/cl_main.c:1367
func (c *Client) precache_f() {
	if c.cmd.Argc() < 2 {
		return // old demo precache sequence: nothing to send
	}
	spawncount := shared.Atoi(c.cmd.Argv(1))
	c.Netchan.Message.MSG_WriteByte(q2const.Clc_stringcmd)
	c.Netchan.Message.MSG_WriteString(fmt.Sprintf("begin %d\n", spawncount))
	c.BeginSent = true
	c.Reliables++
}

// C: client/cl_main.c:591 CL_Changing_f
func (c *Client) changing_f() {
	c.State = CaConnected // not active anymore, but not disconnected
	c.BeginSent = false
	c.printf("\nChanging map...\n")
}

// C: client/cl_main.c:611 CL_Reconnect_f
func (c *Client) reconnect_f() {
	if c.State == CaConnected {
		c.printf("reconnecting...\n")
		c.BeginSent = false
		c.Netchan.Message.MSG_WriteChar(q2const.Clc_stringcmd)
		c.Netchan.Message.MSG_WriteString("new")
		c.Reliables++
		return
	}
	if c.State >= CaConnected {
		c.Disconnect()
		c.Disconnected = false
		c.connectTime = c.curtime() - 1500
	} else {
		c.connectTime = -99999 // fire immediately
	}
	c.State = CaConnecting
	c.printf("reconnecting...\n")
}

func (c *Client) disconnect_f() {
	c.Disconnect()
	c.Disconnected = true
}

// Disconnect sends the final "disconnect" three times and clears the state.
// C: client/cl_main.c:510 CL_Disconnect
func (c *Client) Disconnect() {
	if c.State == CaDisconnected {
		return
	}
	c.connectTime = 0
	final := append([]byte{q2const.Clc_stringcmd}, "disconnect"...)
	c.Netchan.Transmit(final, c.curtime())
	c.Netchan.Transmit(final, c.curtime())
	c.Netchan.Transmit(final, c.curtime())
	c.clearState()
	c.State = CaDisconnected
}

// C: client/cl_main.c:486 CL_ClearState (protocol part)
func (c *Client) clearState() {
	c.ServerData = ServerData{}
	c.ConfigStrings = [q2const.MAX_CONFIGSTRINGS]string{}
	c.Entities = [q2const.MAX_EDICTS]CEntity{}
	c.NumBaselines = 0
	c.Frame = Frame{}
	c.Frames = [q2const.UPDATE_BACKUP]Frame{}
	c.parseEntities = 0
	c.cmds = [CMD_BACKUP]shared.UserCmd{}
	// clear out the netchan's pending reliable data
	c.Netchan.Message.SZ_Clear()
}

// StringCmd queues a clc_stringcmd on the reliable stream (like typing a
// forwarded command at the console). It is sent with the next packet.
func (c *Client) StringCmd(s string) {
	c.Netchan.Message.MSG_WriteByte(q2const.Clc_stringcmd)
	c.Netchan.Message.MSG_WriteString(s)
	c.Reliables++
}

// SetUserinfo replaces the userinfo; it is sent as clc_userinfo with the next
// SendCmd while active (userinfo_modified).
func (c *Client) SetUserinfo(s string) {
	c.Userinfo = s
	c.userinfoMod = true
}

// Exec runs console text through the client's command buffer.
func (c *Client) Exec(text string) {
	c.cmd.Cbuf_AddText(text)
	c.cmd.Cbuf_Execute()
}

// ConfigStringIndexed returns the configstrings in [base, base+n) that are set.
func (c *Client) ConfigStringIndexed(base, n int) map[int]string {
	out := map[int]string{}
	for i := base; i < base+n && i < q2const.MAX_CONFIGSTRINGS; i++ {
		if c.ConfigStrings[i] != "" {
			out[i] = c.ConfigStrings[i]
		}
	}
	return out
}

// FrameEntities returns the entity states of frame f (copied out of the ring).
func (c *Client) FrameEntities(f *Frame) []shared.EntityState {
	out := make([]shared.EntityState, f.NumEntities)
	for i := range out {
		out[i] = c.ParseEnts[(f.ParseEntities+i)&(MAX_PARSE_ENTITIES-1)]
	}
	return out
}

// Origin returns the player's pmove origin of the current frame in units.
func (c *Client) Origin() shared.Vec3 {
	o := c.Frame.PlayerState.PMove.Origin
	return shared.Vec3{float32(o[0]) * 0.125, float32(o[1]) * 0.125, float32(o[2]) * 0.125}
}
