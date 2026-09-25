// Package fakeclient is a headless protocol-34 Quake 2 client for tests. It
// ports the protocol side of client/cl_main.c (connection handshake),
// client/cl_parse.c, client/cl_ents.c (frame / delta entity parsing) and
// client/cl_input.c CL_SendCmd over any net.Conn.
package fakeclient

import (
	"context"
	"errors"
	"fmt"
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

// Options configures a Client.
type Options struct {
	Qport    int    // netchan qport; 0 picks a pseudo random one
	Userinfo string // default "\name\fakeclient\skin\male/grunt\rate\25000\msg\1\hand\0\fov\90"
	NoDelta  bool   // cl_nodelta: always request uncompressed frames
	Printf   func(format string, args ...any)
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

	Prints       []string
	CenterPrints []string
	StuffTexts   []string
	Layouts      []string
	Sounds       []Sound
	TempEnts     []int32
	Downloads    []Download
	Inventory    [q2const.MAX_ITEMS]int32
	OOB          []string // connectionless messages received
	Reliables    int      // number of reliable (stringcmd/userinfo) messages queued
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

func (c *Client) printf(format string, args ...any) {
	if c.opt.Printf != nil {
		c.opt.Printf(format, args...)
	}
}

// curtime is Sys_Milliseconds relative to the client's creation.
func (c *Client) curtime() int { return int(time.Since(c.start) / time.Millisecond) }

// Qport returns the qport the client uses.
func (c *Client) Qport() int { return c.opt.Qport }

// ---------------------------------------------------------------------------
// Connection (client/cl_main.c)

// Connect starts the handshake: getchallenge, connect, and waits until the
// server answered client_connect (the "new" command is then queued).
func (c *Client) Connect(ctx context.Context) error {
	c.State = CaConnecting
	c.connectTime = -99999 // CL_CheckForResend() will fire immediately
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
func (c *Client) readPacket(ctx context.Context, d time.Duration) (err error) {
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
	defer func() {
		if r := recover(); r != nil {
			ce, ok := r.(shared.ComError)
			if !ok {
				panic(r)
			}
			c.Disconnected = true
			c.DisconnectMsg = ce.Msg
			c.State = CaDisconnected
			if ce.Code == q2const.ERR_DISCONNECT {
				err = ErrDisconnected
			} else {
				err = fmt.Errorf("fakeclient: %s", ce.Msg)
			}
		}
	}()
	c.processPacket(data)
	c.cmd.Cbuf_Execute()
	if c.Disconnected {
		return ErrDisconnected
	}
	return nil
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
	c.parseServerMessage(m)
}

// C: client/cl_main.c:866 CL_ConnectionlessPacket
func (c *Client) connectionlessPacket(m *msg.SizeBuf) {
	m.MSG_BeginReading()
	m.MSG_ReadLong() // skip the -1
	s := m.MSG_ReadStringLine()
	c.OOB = append(c.OOB, s)
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
		c.Prints = append(c.Prints, s)
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
