package sv

import (
	"encoding/binary"
	"fmt"
	"strings"

	qnet "quake2web/server/internal/net"
	"quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/msg"
	"quake2web/server/internal/qcommon/shared"
)

// DropClient is called when the player is totally leaving the server, either
// willingly or unwillingly. This is NOT called if the entire server is quiting
// or crashing.
// C: server/sv_main.c:67 SV_DropClient
func (s *Server) DropClient(drop *Client) {
	// add the disconnect
	drop.Netchan.Message.MSG_WriteByte(q2const.Svc_disconnect)

	if drop.State == cs_spawned {
		// call the prog function for removing a client
		// this will remove the body, among other things
		s.ge.ClientDisconnect(drop.Edict)
	}

	if drop.Download != nil {
		drop.Download = nil
	}

	drop.State = cs_zombie // become free in a few seconds
	drop.Name = ""
}

/*
==============================================================================

CONNECTIONLESS COMMANDS

==============================================================================
*/

// statusString builds the string that is sent as heartbeats and status
// replies.
// C: server/sv_main.c:103 SV_StatusString
func (s *Server) statusString() string {
	const size = q2const.MAX_MSGLEN - 16
	status := s.Cvars.Serverinfo() + "\n"
	for i := 0; i < s.maxClients(); i++ {
		cl := &s.SVS.Clients[i]
		if cl.State == cs_connected || cl.State == cs_spawned {
			player := fmt.Sprintf("%d %d \"%s\"\n",
				cl.Edict.Client.PS.Stats[q2const.STAT_FRAGS], cl.Ping, cl.Name)
			if len(status)+len(player) >= size {
				break // can't hold any more
			}
			status += player
		}
	}
	return status
}

// svcStatus responds with all the info that qplug or qspy can see.
// C: server/sv_main.c:141 SVC_Status
func (s *Server) svcStatus() {
	qnet.OutOfBandPrint(s.netVia, s.netFrom, "print\n"+s.statusString())
}

// svcAck. C: server/sv_main.c:157 SVC_Ack
func (s *Server) svcAck() {
	s.Printf("Ping acknowledge from %s\n", s.netFrom)
}

// svcInfo responds with short info for broadcast scans.
// C: server/sv_main.c:170 SVC_Info
func (s *Server) svcInfo() {
	if s.maxclients.Value == 1 {
		return // ignore in single player
	}

	version := shared.Atoi(s.Cmd.Argv(1))

	var str string
	if version != q2const.PROTOCOL_VERSION {
		str = fmt.Sprintf("%s: wrong version\n", s.hostname.String)
	} else {
		count := 0
		for i := 0; i < s.maxClients(); i++ {
			if s.SVS.Clients[i].State >= cs_connected {
				count++
			}
		}
		str = fmt.Sprintf("%16s %8s %2d/%2d\n", s.hostname.String, s.SV.Name, count, s.maxClients())
	}
	if len(str) > 63 {
		str = str[:63] // char string[64]
	}
	qnet.OutOfBandPrint(s.netVia, s.netFrom, "info\n"+str)
}

// svcPing just responds with an acknowledgement.
// C: server/sv_main.c:204 SVC_Ping
func (s *Server) svcPing() {
	qnet.OutOfBandPrint(s.netVia, s.netFrom, "ack")
}

// svcGetChallenge returns a challenge number that can be used in a subsequent
// client_connect command.
// C: server/sv_main.c:222 SVC_GetChallenge
func (s *Server) svcGetChallenge() {
	oldest := 0
	oldestTime := 0x7fffffff

	// see if we already have a challenge for this ip
	i := 0
	for i = 0; i < MAX_CHALLENGES; i++ {
		if qnet.CompareBaseAdr(s.netFrom, s.SVS.challenges[i].adr) {
			break
		}
		if s.SVS.challenges[i].time < oldestTime {
			oldestTime = s.SVS.challenges[i].time
			oldest = i
		}
	}

	if i == MAX_CHALLENGES {
		// overwrite the oldest
		s.SVS.challenges[oldest].challenge = int(s.Rand.Rand() & 0x7fff)
		s.SVS.challenges[oldest].adr = s.netFrom
		s.SVS.challenges[oldest].time = s.Milliseconds()
		i = oldest
	}

	// send it back
	qnet.OutOfBandPrint(s.netVia, s.netFrom, fmt.Sprintf("challenge %d", s.SVS.challenges[i].challenge))
}

// svcDirectConnect is a connection request that did not come from the master.
// C: server/sv_main.c:262 SVC_DirectConnect
func (s *Server) svcDirectConnect() {
	adr := s.netFrom
	via := s.netVia

	s.DPrintf("SVC_DirectConnect ()\n")

	version := shared.Atoi(s.Cmd.Argv(1))
	if version != q2const.PROTOCOL_VERSION {
		qnet.OutOfBandPrint(via, adr, fmt.Sprintf("print\nServer is version %4.2f.\n", q2const.VERSION))
		s.DPrintf("    rejected connect from version %d\n", version)
		return
	}

	qport := int(shared.Atoi(s.Cmd.Argv(2)))

	challenge := int(shared.Atoi(s.Cmd.Argv(3)))

	userinfo := s.Cmd.Argv(4)
	if len(userinfo) > q2const.MAX_INFO_STRING-1 {
		userinfo = userinfo[:q2const.MAX_INFO_STRING-1]
	}

	if s.cfg.Userinfo != nil {
		userinfo = s.cfg.Userinfo(s.netFrom, userinfo)
	}

	// force the IP key/value pair so the game can filter based on ip
	var warn string
	userinfo, warn = shared.Info_SetValueForKey(userinfo, "ip", s.netFrom.String())
	if warn != "" {
		s.Printf("%s", warn)
	}

	// attractloop servers are ONLY for local clients
	if s.SV.AttractLoop {
		if !qnet.IsLocalAddress(adr) {
			s.Printf("Remote connect in attract loop.  Ignored.\n")
			qnet.OutOfBandPrint(via, adr, "print\nConnection refused.\n")
			return
		}
	}

	// see if the challenge is valid
	if !qnet.IsLocalAddress(adr) {
		i := 0
		for i = 0; i < MAX_CHALLENGES; i++ {
			if qnet.CompareBaseAdr(s.netFrom, s.SVS.challenges[i].adr) {
				if challenge == s.SVS.challenges[i].challenge {
					break // good
				}
				qnet.OutOfBandPrint(via, adr, "print\nBad challenge.\n")
				return
			}
		}
		if i == MAX_CHALLENGES {
			qnet.OutOfBandPrint(via, adr, "print\nNo challenge for address.\n")
			return
		}
	}

	var newcl *Client

	// if there is already a slot for this ip, reuse it
	for i := 0; i < s.maxClients(); i++ {
		cl := &s.SVS.Clients[i]
		if cl.State == cs_free {
			continue
		}
		if qnet.CompareBaseAdr(adr, cl.Netchan.RemoteAddress) &&
			(cl.Netchan.Qport == qport || adr.Port == cl.Netchan.RemoteAddress.Port) {
			if !qnet.IsLocalAddress(adr) && (s.SVS.RealTime-cl.LastConnect) < (int(s.svReconnectLimit.Value)*1000) {
				s.DPrintf("%s:reconnect rejected : too soon\n", adr)
				return
			}
			s.Printf("%s:reconnect\n", adr)
			newcl = cl
			break
		}
	}

	if newcl == nil {
		// find a client slot
		for i := 0; i < s.maxClients(); i++ {
			cl := &s.SVS.Clients[i]
			if cl.State == cs_free {
				newcl = cl
				break
			}
		}
		if newcl == nil {
			qnet.OutOfBandPrint(via, adr, "print\nServer is full.\n")
			s.DPrintf("Rejected a connection.\n")
			return
		}
	}

	// gotnewcl:
	// build a new connection
	// accept the new client
	// this is the only place a client_t is ever initialized
	idx := newcl.index
	*newcl = Client{index: idx}
	s.client = newcl
	edictnum := idx + 1
	ent := s.edictNum(edictnum)
	newcl.Edict = ent
	newcl.Challenge = challenge // save challenge for checksumming

	// get the game a chance to reject this connection or modify the userinfo
	ok, newUserinfo := s.ge.ClientConnect(ent, userinfo)
	userinfo = newUserinfo
	if !ok {
		if rej := shared.Info_ValueForKey(userinfo, "rejmsg"); rej != "" {
			qnet.OutOfBandPrint(via, adr, fmt.Sprintf("print\n%s\nConnection refused.\n", rej))
		} else {
			qnet.OutOfBandPrint(via, adr, "print\nConnection refused.\n")
		}
		s.DPrintf("Game rejected a connection.\n")
		return
	}

	// parse some info from the info strings
	if len(userinfo) > q2const.MAX_INFO_STRING-1 {
		userinfo = userinfo[:q2const.MAX_INFO_STRING-1]
	}
	newcl.Userinfo = userinfo
	s.UserinfoChanged(newcl)

	// send the connect packet to the client
	qnet.OutOfBandPrint(via, adr, "client_connect")

	s.netchanSetup(&newcl.Netchan, adr, via, qport)

	newcl.State = cs_connected

	newcl.Datagram.SZ_Init(newcl.datagramBuf[:])
	newcl.Datagram.AllowOverflow = true
	newcl.LastMessage = s.SVS.RealTime // don't timeout
	newcl.LastConnect = s.SVS.RealTime
}

func (s *Server) netchanSetup(ch *qnet.Netchan, adr qnet.Addr, via qnet.Sender, qport int) {
	ch.Printf = s.Printf
	ch.ShowPackets = func() bool { return s.showpackets.Value != 0 }
	ch.ShowDrop = func() bool { return s.showdrop.Value != 0 }
	ch.Setup(q2const.NS_SERVER, adr, via, qport, s.Milliseconds())
}

// rconValidate. C: server/sv_main.c:410 Rcon_Validate
func (s *Server) rconValidate() bool {
	if len(s.rconPassword.String) == 0 {
		return false
	}
	if s.Cmd.Argv(1) != s.rconPassword.String {
		return false
	}
	return true
}

// svcRemoteCommand: a client issued an rcon command. Shift down the remaining
// args, redirect all printfs.
// C: server/sv_main.c:430 SVC_RemoteCommand
func (s *Server) svcRemoteCommand() {
	text := cString(s.netMsg.Data[4:s.netMsg.CurSize])
	if !s.rconValidate() {
		s.Printf("Bad rcon from %s:\n%s\n", s.netFrom, text)
	} else {
		s.Printf("Rcon from %s:\n%s\n", s.netFrom, text)
	}

	s.beginRedirect(RD_PACKET, SV_OUTPUTBUF_LENGTH)

	if !s.rconValidate() {
		s.Printf("Bad rcon_password.\n")
	} else {
		var remaining strings.Builder
		for i := 2; i < s.Cmd.Argc(); i++ {
			remaining.WriteString(s.Cmd.Argv(i))
			remaining.WriteString(" ")
		}
		s.Cmd.ExecuteString(remaining.String())
	}

	s.endRedirect()
}

func cString(b []byte) string {
	for i, c := range b {
		if c == 0 {
			return string(b[:i])
		}
	}
	return string(b)
}

// connectionlessPacket handles a packet with four leading 0xff characters.
// C: server/sv_main.c:476 SV_ConnectionlessPacket
func (s *Server) connectionlessPacket() {
	s.netMsg.MSG_BeginReading()
	s.netMsg.MSG_ReadLong() // skip the -1 marker

	str := s.netMsg.MSG_ReadStringLine()

	s.Cmd.TokenizeString(str, false)

	c := s.Cmd.Argv(0)
	s.DPrintf("Packet %s : %s\n", s.netFrom, c)

	switch c {
	case "ping":
		s.svcPing()
	case "ack":
		s.svcAck()
	case "status":
		s.svcStatus()
	case "info":
		s.svcInfo()
	case "getchallenge":
		s.svcGetChallenge()
	case "connect":
		s.svcDirectConnect()
	case "rcon":
		s.svcRemoteCommand()
	default:
		s.Printf("bad connectionless packet from %s:\n%s\n", s.netFrom, str)
	}
}

// calcPings updates the cl->ping variables.
// C: server/sv_main.c:523 SV_CalcPings
func (s *Server) calcPings() {
	for i := 0; i < s.maxClients(); i++ {
		cl := &s.SVS.Clients[i]
		if cl.State != cs_spawned {
			continue
		}

		total := 0
		count := 0
		for j := 0; j < LATENCY_COUNTS; j++ {
			if cl.FrameLatency[j] > 0 {
				count++
				total += cl.FrameLatency[j]
			}
		}
		if count == 0 {
			cl.Ping = 0
		} else {
			cl.Ping = total / count
		}

		// let the game dll know about the ping
		cl.Edict.Client.Ping = int32(cl.Ping)
	}
}

// giveMsec gives all clients an allotment of milliseconds for their command
// moves every few frames. If they exceed it, assume cheating.
// C: server/sv_main.c:571 SV_GiveMsec
func (s *Server) giveMsec() {
	if s.SV.FrameNum&15 != 0 {
		return
	}
	for i := 0; i < s.maxClients(); i++ {
		cl := &s.SVS.Clients[i]
		if cl.State == cs_free {
			continue
		}
		cl.CommandMsec = 1800 // 1600 + some slop
	}
}

// readPacket is the body of the SV_ReadPackets loop for one datagram.
// C: server/sv_main.c:595 SV_ReadPackets
func (s *Server) readPacket(p qnet.Packet) {
	s.netFrom = p.From
	s.netVia = p.Via
	n := len(p.Data)
	if n > q2const.MAX_MSGLEN {
		// NET_GetPacket: "Oversize packet from %s"
		s.Printf("Oversize packet from %s\n", p.From)
		return
	}
	s.netMsg.SZ_Clear()
	copy(s.netMsg.Data, p.Data)
	s.netMsg.CurSize = n
	s.netMsg.ReadCount = 0

	// check for connectionless packet (0xffffffff) first
	if n >= 4 && binary.LittleEndian.Uint32(p.Data) == 0xffffffff {
		s.connectionlessPacket()
		return
	}

	// read the qport out of the message so we can fix up
	// stupid address translating routers
	s.netMsg.MSG_BeginReading()
	s.netMsg.MSG_ReadLong() // sequence number
	s.netMsg.MSG_ReadLong() // sequence number
	qport := int(s.netMsg.MSG_ReadShort() & 0xffff)

	// check for packets from connected clients
	for i := 0; i < s.maxClients(); i++ {
		cl := &s.SVS.Clients[i]
		if cl.State == cs_free {
			continue
		}
		if !qnet.CompareBaseAdr(s.netFrom, cl.Netchan.RemoteAddress) {
			continue
		}
		if cl.Netchan.Qport != qport {
			continue
		}
		if cl.Netchan.RemoteAddress.Port != s.netFrom.Port {
			s.Printf("SV_ReadPackets: fixing up a translated port\n")
			cl.Netchan.RemoteAddress.Port = s.netFrom.Port
			cl.Netchan.Out = s.netVia
		}

		if cl.Netchan.Process(s.netMsg, s.Milliseconds()) {
			// this is a valid, sequenced packet, so process it
			if cl.State != cs_zombie {
				cl.LastMessage = s.SVS.RealTime // don't timeout
				s.executeClientMessage(cl)
			}
		}
		break
	}
}

// checkTimeouts drops clients that sent nothing for timeout->value seconds;
// zombies become free after zombietime.
// C: server/sv_main.c:659 SV_CheckTimeouts
func (s *Server) checkTimeouts() {
	// int - float*1000 is evaluated in float, then truncated
	droppoint := int(float32(s.SVS.RealTime) - 1000*s.timeout.Value)
	zombiepoint := int(float32(s.SVS.RealTime) - 1000*s.zombietime.Value)

	for i := 0; i < s.maxClients(); i++ {
		cl := &s.SVS.Clients[i]
		// message times may be wrong across a changelevel
		if cl.LastMessage > s.SVS.RealTime {
			cl.LastMessage = s.SVS.RealTime
		}

		if cl.State == cs_zombie && cl.LastMessage < zombiepoint {
			cl.State = cs_free // can now be reused
			continue
		}
		if (cl.State == cs_connected || cl.State == cs_spawned) && cl.LastMessage < droppoint {
			s.BroadcastPrintf(q2const.PRINT_HIGH, "%s timed out\n", cl.Name)
			s.DropClient(cl)
			cl.State = cs_free // don't bother with zombie state
		}
	}
}

// prepWorldFrame clears events: they only last for a single message.
// C: server/sv_main.c:698 SV_PrepWorldFrame
func (s *Server) prepWorldFrame() {
	n := s.ge.NumEdicts()
	for i := 0; i < n; i++ {
		ent := s.edictNum(i)
		// events only last for a single message
		ent.S.Event = 0
	}
}

// runGameFrame. C: server/sv_main.c:717 SV_RunGameFrame
func (s *Server) runGameFrame() {
	// we always need to bump framenum, even if we
	// don't run the world, otherwise the delta
	// compression can get confused when a client
	// has the "current" frame
	s.SV.FrameNum++
	s.SV.Time = uint32(s.SV.FrameNum * 100)

	// don't run if paused
	if s.svPaused.Value == 0 || s.maxclients.Value > 1 {
		s.ge.RunFrame()

		// never get more than one tic behind
		if int(s.SV.Time) < s.SVS.RealTime {
			if s.svShowclamp.Value != 0 {
				s.Printf("sv highclamp\n")
			}
			s.SVS.RealTime = int(s.SV.Time)
		}
	}
}

// Frame is SV_Frame driven by the instance loop: msec is the wall time since
// the previous call to Frame or HandlePacket. It returns the number of
// milliseconds until the next game frame is due (NET_Sleep).
//
// Deviation (documented in PARITY.md): C's SV_Frame runs once per host loop
// iteration (every >= 1 ms), calling rand() and SV_CheckTimeouts each time and
// sleeping in NET_Sleep until a packet arrives or the frame is due. Here
// packets are handled as they arrive (HandlePacket) and rand() plus
// SV_CheckTimeouts run exactly once per game frame, just before it, so the
// random stream only depends on the game.
// C: server/sv_main.c:752 SV_Frame
func (s *Server) Frame(msec int) (sleep int, err error) {
	defer s.recoverError(&err)

	// execute console commands (Qcommon_Frame: Cbuf_Execute before SV_Frame)
	s.Cmd.Cbuf_Execute()

	// if server is not active, do nothing
	if !s.SVS.Initialized {
		return 100, nil
	}

	s.SVS.RealTime += msec

	// move autonomous things around if enough time has passed
	if s.svTimedemo.Value == 0 && s.SVS.RealTime < int(s.SV.Time) {
		// never let the time get too far off
		if int(s.SV.Time)-s.SVS.RealTime > 100 {
			if s.svShowclamp.Value != 0 {
				s.Printf("sv lowclamp\n")
			}
			s.SVS.RealTime = int(s.SV.Time) - 100
		}
		return int(s.SV.Time) - s.SVS.RealTime, nil
	}

	// keep the random time dependent
	s.Rand.Rand()

	// check timeouts
	s.checkTimeouts()

	// update ping based on the last known frame from all clients
	s.calcPings()

	// give the clients some timeslices
	s.giveMsec()

	// let everything in the world think and move
	s.runGameFrame()

	// send messages back to the clients that had packets read this frame
	s.SendClientMessages()

	// save the entire world state if recording a serverdemo
	s.recordDemoMessage()

	// send a heartbeat to the master if needed
	s.masterHeartbeat()

	// clear teleport flags, etc for next frame
	s.prepWorldFrame()

	if !s.SVS.Initialized {
		return 100, nil
	}
	d := int(s.SV.Time) - s.SVS.RealTime
	if d < 0 {
		d = 0
	}
	return d, nil
}

// HandlePacket processes one received datagram (SV_ReadPackets for one
// packet). msec is the wall time since the previous Frame/HandlePacket call.
func (s *Server) HandlePacket(msec int, p qnet.Packet) (err error) {
	defer s.recoverError(&err)
	// Qcommon_Frame runs Cbuf_Execute before every SV_Frame / SV_ReadPackets
	s.Cmd.Cbuf_Execute()
	if !s.SVS.Initialized {
		return nil
	}
	s.SVS.RealTime += msec
	s.readPacket(p)
	return nil
}

// HEARTBEAT_SECONDS. C: server/sv_main.c:828
const HEARTBEAT_SECONDS = 300

// masterHeartbeat sends a message to the master every few minutes. Masters are
// replaced by the server browser API; master_adr stays empty so nothing is
// sent, but the timing logic is kept.
// C: server/sv_main.c:829 Master_Heartbeat
func (s *Server) masterHeartbeat() {
	if s.dedicated.Value == 0 {
		return // only dedicated servers send heartbeats
	}
	if s.publicServer.Value == 0 {
		return // a private dedicated game
	}

	// check for time wraparound
	if s.SVS.LastHeartbeat > s.SVS.RealTime {
		s.SVS.LastHeartbeat = s.SVS.RealTime
	}

	if s.SVS.RealTime-s.SVS.LastHeartbeat < HEARTBEAT_SECONDS*1000 {
		return // not time to send yet
	}

	s.SVS.LastHeartbeat = s.SVS.RealTime

	// send the same string that we would give for a status OOB command
	str := s.statusString()

	// send to group master
	for i := 0; i < MAX_MASTERS; i++ {
		if s.masterAdr[i].Port != 0 {
			s.Printf("Sending heartbeat to %s\n", s.masterAdr[i])
			qnet.OutOfBandPrint(s.netVia, s.masterAdr[i], "heartbeat\n"+str)
		}
	}
}

// masterShutdown informs all masters that this server is going down.
// C: server/sv_main.c:873 Master_Shutdown
func (s *Server) masterShutdown() {
	if s.dedicated == nil || s.dedicated.Value == 0 {
		return
	}
	if s.publicServer.Value == 0 {
		return
	}
	for i := 0; i < MAX_MASTERS; i++ {
		if s.masterAdr[i].Port != 0 {
			if i > 0 {
				s.Printf("Sending heartbeat to %s\n", s.masterAdr[i])
			}
			qnet.OutOfBandPrint(s.netVia, s.masterAdr[i], "shutdown")
		}
	}
}

// UserinfoChanged pulls specific info from a newly changed userinfo string
// into a more C freindly form.
// C: server/sv_main.c:905 SV_UserinfoChanged
func (s *Server) UserinfoChanged(cl *Client) {
	// call prog code to allow overrides
	s.ge.ClientUserinfoChanged(cl.Edict, cl.Userinfo)

	// name for C code
	name := []byte(shared.Info_ValueForKey(cl.Userinfo, "name"))
	if len(name) > 31 {
		name = name[:31]
	}
	// mask off high bit
	for i := range name {
		name[i] &= 127
	}
	cl.Name = cString(name)

	// rate command
	val := shared.Info_ValueForKey(cl.Userinfo, "rate")
	if len(val) != 0 {
		i := int(shared.Atoi(val))
		cl.Rate = i
		if cl.Rate < 100 {
			cl.Rate = 100
		}
		if cl.Rate > 15000 {
			cl.Rate = 15000
		}
	} else {
		cl.Rate = 5000
	}

	// msg command
	val = shared.Info_ValueForKey(cl.Userinfo, "msg")
	if len(val) != 0 {
		cl.MessageLevel = int(shared.Atoi(val))
	}
}

// svInit is called once at instance creation.
// C: server/sv_main.c:951 SV_Init
func (s *Server) svInit() {
	s.initOperatorCommands()

	s.rconPassword = s.Cvars.Get("rcon_password", "", 0)
	s.Cvars.Get("skill", "1", 0)
	s.Cvars.Get("deathmatch", "0", q2const.CVAR_LATCH)
	s.Cvars.Get("coop", "0", q2const.CVAR_LATCH)
	s.Cvars.Get("dmflags", fmt.Sprintf("%d", q2const.DF_INSTANT_ITEMS), q2const.CVAR_SERVERINFO)
	s.Cvars.Get("fraglimit", "0", q2const.CVAR_SERVERINFO)
	s.Cvars.Get("timelimit", "0", q2const.CVAR_SERVERINFO)
	s.Cvars.Get("cheats", "0", q2const.CVAR_SERVERINFO|q2const.CVAR_LATCH)
	s.Cvars.Get("protocol", fmt.Sprintf("%d", q2const.PROTOCOL_VERSION), q2const.CVAR_SERVERINFO|q2const.CVAR_NOSET)
	s.maxclients = s.Cvars.Get("maxclients", "1", q2const.CVAR_SERVERINFO|q2const.CVAR_LATCH)
	s.hostname = s.Cvars.Get("hostname", "noname", q2const.CVAR_SERVERINFO|q2const.CVAR_ARCHIVE)
	s.timeout = s.Cvars.Get("timeout", "125", 0)
	s.zombietime = s.Cvars.Get("zombietime", "2", 0)
	s.svShowclamp = s.Cvars.Get("showclamp", "0", 0)
	s.svPaused = s.Cvars.Get("paused", "0", 0)
	s.svTimedemo = s.Cvars.Get("timedemo", "0", 0)
	s.svEnforcetime = s.Cvars.Get("sv_enforcetime", "0", 0)
	s.allowDownload = s.Cvars.Get("allow_download", "0", q2const.CVAR_ARCHIVE)
	s.allowDownloadPlayers = s.Cvars.Get("allow_download_players", "0", q2const.CVAR_ARCHIVE)
	s.allowDownloadModels = s.Cvars.Get("allow_download_models", "1", q2const.CVAR_ARCHIVE)
	s.allowDownloadSounds = s.Cvars.Get("allow_download_sounds", "1", q2const.CVAR_ARCHIVE)
	s.allowDownloadMaps = s.Cvars.Get("allow_download_maps", "1", q2const.CVAR_ARCHIVE)

	s.svNoreload = s.Cvars.Get("sv_noreload", "0", 0)

	s.svAirAccelerate = s.Cvars.Get("sv_airaccelerate", "0", q2const.CVAR_LATCH)

	s.publicServer = s.Cvars.Get("public", "0", 0)

	s.svReconnectLimit = s.Cvars.Get("sv_reconnect_limit", "3", q2const.CVAR_ARCHIVE)
}

// finalMessage is used by SV_Shutdown to send a final message to all
// connected clients before the server goes down.
// C: server/sv_main.c:1002 SV_FinalMessage
func (s *Server) finalMessage(message string, reconnect bool) {
	m := msg.NewSizeBuf(q2const.MAX_MSGLEN)
	m.MSG_WriteByte(q2const.Svc_print)
	m.MSG_WriteByte(q2const.PRINT_HIGH)
	m.MSG_WriteString(message)

	if reconnect {
		m.MSG_WriteByte(q2const.Svc_reconnect)
	} else {
		m.MSG_WriteByte(q2const.Svc_disconnect)
	}

	// send it twice
	// stagger the packets to crutch operating system limited buffers
	for i := range s.SVS.Clients {
		cl := &s.SVS.Clients[i]
		if cl.State >= cs_connected {
			cl.Netchan.Transmit(m.Bytes(), s.Milliseconds())
		}
	}
	for i := range s.SVS.Clients {
		cl := &s.SVS.Clients[i]
		if cl.State >= cs_connected {
			cl.Netchan.Transmit(m.Bytes(), s.Milliseconds())
		}
	}
}

// svShutdown is called when each game quits.
// C: server/sv_main.c:1041 SV_Shutdown
func (s *Server) svShutdown(finalmsg string, reconnect bool) {
	if s.SVS.Clients != nil {
		s.finalMessage(finalmsg, reconnect)
	}

	s.masterShutdown()
	s.shutdownGameProgs()

	// free current level
	s.SV = ServerT{}

	// free server static data
	if s.SVS.DemoFile != nil {
		_ = s.SVS.DemoFile.Close()
	}
	s.SVS = ServerStatic{}
	s.client = nil
	s.player = nil
}

// Shutdown kills the server like "killserver" (clients get a disconnect).
func (s *Server) Shutdown(finalmsg string) {
	if s.SVS.Initialized {
		s.svShutdown(finalmsg, false)
	}
	s.killed = true
}

// infoPrint is Info_Print.
// C: qcommon/common.c:1003 Info_Print
func (s *Server) infoPrint(str string) {
	if strings.HasPrefix(str, "\\") {
		str = str[1:]
	}
	for len(str) > 0 {
		i := strings.IndexByte(str, '\\')
		var key string
		if i < 0 {
			key, str = str, ""
		} else {
			key, str = str[:i], str[i:]
		}
		if len(key) < 20 {
			key += strings.Repeat(" ", 20-len(key))
		}
		s.Printf("%s", key)

		if len(str) == 0 {
			s.Printf("MISSING VALUE\n")
			return
		}

		str = str[1:]
		i = strings.IndexByte(str, '\\')
		var value string
		if i < 0 {
			value, str = str, ""
		} else {
			value, str = str[:i], str[i+1:]
		}
		s.Printf("%s\n", value)
	}
}
