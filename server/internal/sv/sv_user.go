package sv

import (
	"fmt"

	"quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/crc"
	"quake2web/server/internal/qcommon/shared"
)

/*
============================================================

USER STRINGCMD EXECUTION

sv_client and sv_player will be valid.
============================================================
*/

// beginDemoserver. C: server/sv_user.c:36 SV_BeginDemoserver
func (s *Server) beginDemoserver() {
	name := fmt.Sprintf("demos/%s", s.SV.Name)
	var data []byte
	var err error
	if s.cfg.FS != nil {
		data, err = s.cfg.FS.ReadFile(name)
	}
	if s.cfg.FS == nil || err != nil {
		shared.Error(q2const.ERR_DROP, "Couldn't open %s\n", name)
	}
	s.SV.DemoFile = data
	s.SV.demoPos = 0
	s.SV.demoOpen = true
}

// new_f sends the first message from the server to a connected client. This
// will be sent on the initial connection and upon each server load.
// C: server/sv_user.c:55 SV_New_f
func (s *Server) new_f() {
	cl := s.client
	s.DPrintf("New() from %s\n", cl.Name)

	if cl.State != cs_connected {
		s.Printf("New not valid -- already spawned\n")
		return
	}

	// demo servers just dump the file message
	if s.SV.State == ss_demo {
		s.beginDemoserver()
		return
	}

	// serverdata needs to go over for all types of servers
	// to make sure the protocol is right, and to set the gamedir
	gamedir := s.Cvars.VariableString("gamedir")

	// send the serverdata
	m := &cl.Netchan.Message
	m.MSG_WriteByte(q2const.Svc_serverdata)
	m.MSG_WriteLong(q2const.PROTOCOL_VERSION)
	m.MSG_WriteLong(int32(s.SVS.SpawnCount))
	m.MSG_WriteByte(b2i(s.SV.AttractLoop))
	m.MSG_WriteString(gamedir)

	var playernum int
	if s.SV.State == ss_cinematic || s.SV.State == ss_pic {
		playernum = -1
	} else {
		playernum = cl.index
	}
	m.MSG_WriteShort(int32(playernum))

	// send full levelname
	m.MSG_WriteString(s.SV.ConfigStrings.Get(q2const.CS_NAME))

	// game server
	if s.SV.State == ss_game {
		// set up the entity for the client
		ent := s.edictNum(playernum + 1)
		ent.S.Number = int32(playernum + 1)
		cl.Edict = ent
		cl.LastCmd = shared.UserCmd{}

		// begin fetching configstrings
		m.MSG_WriteByte(q2const.Svc_stufftext)
		m.MSG_WriteString(fmt.Sprintf("cmd configstrings %d 0\n", s.SVS.SpawnCount))
	}
}

func b2i(b bool) int32 {
	if b {
		return 1
	}
	return 0
}

// configstrings_f. C: server/sv_user.c:125 SV_Configstrings_f
func (s *Server) configstrings_f() {
	cl := s.client
	s.DPrintf("Configstrings() from %s\n", cl.Name)

	if cl.State != cs_connected {
		s.Printf("configstrings not valid -- already spawned\n")
		return
	}

	// handle the case of a level changing while a client was connecting
	if int(shared.Atoi(s.Cmd.Argv(1))) != s.SVS.SpawnCount {
		s.Printf("SV_Configstrings_f from different level\n")
		s.new_f()
		return
	}

	start := int(shared.Atoi(s.Cmd.Argv(2)))
	// C indexes configstrings[start] for a negative start (reads before the
	// array); memory-safety fix: start at 0. (Skipping the negative slots one
	// by one let "configstrings N -2147483648" spin 2^31 iterations.)
	if start < 0 {
		start = 0
	}

	// write a packet full of data
	m := &cl.Netchan.Message
	for m.CurSize < q2const.MAX_MSGLEN/2 && start < q2const.MAX_CONFIGSTRINGS {
		if !s.SV.ConfigStrings.Empty(start) {
			m.MSG_WriteByte(q2const.Svc_configstring)
			m.MSG_WriteShort(int32(start))
			m.MSG_WriteString(s.SV.ConfigStrings.Get(start))
		}
		start++
	}

	// send next command
	if start == q2const.MAX_CONFIGSTRINGS {
		m.MSG_WriteByte(q2const.Svc_stufftext)
		m.MSG_WriteString(fmt.Sprintf("cmd baselines %d 0\n", s.SVS.SpawnCount))
	} else {
		m.MSG_WriteByte(q2const.Svc_stufftext)
		m.MSG_WriteString(fmt.Sprintf("cmd configstrings %d %d\n", s.SVS.SpawnCount, start))
	}
}

// baselines_f. C: server/sv_user.c:182 SV_Baselines_f
func (s *Server) baselines_f() {
	cl := s.client
	s.DPrintf("Baselines() from %s\n", cl.Name)

	if cl.State != cs_connected {
		s.Printf("baselines not valid -- already spawned\n")
		return
	}

	// handle the case of a level changing while a client was connecting
	if int(shared.Atoi(s.Cmd.Argv(1))) != s.SVS.SpawnCount {
		s.Printf("SV_Baselines_f from different level\n")
		s.new_f()
		return
	}

	start := int(shared.Atoi(s.Cmd.Argv(2)))
	// C indexes baselines[start] for a negative start; memory-safety fix:
	// start at 0 (see configstrings_f).
	if start < 0 {
		start = 0
	}

	var nullstate shared.EntityState

	// write a packet full of data
	m := &cl.Netchan.Message
	for m.CurSize < q2const.MAX_MSGLEN/2 && start < q2const.MAX_EDICTS {
		base := &s.SV.Baselines[start]
		if base.ModelIndex != 0 || base.Sound != 0 || base.Effects != 0 {
			m.MSG_WriteByte(q2const.Svc_spawnbaseline)
			m.MSG_WriteDeltaEntity(&nullstate, base, true, true)
		}
		start++
	}

	// send next command
	if start == q2const.MAX_EDICTS {
		m.MSG_WriteByte(q2const.Svc_stufftext)
		m.MSG_WriteString(fmt.Sprintf("precache %d\n", s.SVS.SpawnCount))
	} else {
		m.MSG_WriteByte(q2const.Svc_stufftext)
		m.MSG_WriteString(fmt.Sprintf("cmd baselines %d %d\n", s.SVS.SpawnCount, start))
	}
}

// begin_f. C: server/sv_user.c:238 SV_Begin_f
func (s *Server) begin_f() {
	s.DPrintf("Begin() from %s\n", s.client.Name)

	// handle the case of a level changing while a client was connecting
	if int(shared.Atoi(s.Cmd.Argv(1))) != s.SVS.SpawnCount {
		s.Printf("SV_Begin_f from different level\n")
		s.new_f()
		return
	}

	// Robustness fix (not in C): only a game server has a player to spawn.
	// On a cinematic / pic / demo server C calls ge->ClientBegin on the empty
	// map and the game aborts the whole server (ERR_DROP "Couldn't find spawn
	// point"); real clients never send begin there.
	if s.SV.State != ss_game {
		s.DPrintf("SV_Begin_f on a non-game server from %s\n", s.client.Name)
		return
	}

	s.client.State = cs_spawned

	// call the game begin function
	s.ge.ClientBegin(s.player)

	s.Cmd.Cbuf_InsertFromDefer()
}

// nextDownload_f. C: server/sv_user.c:262 SV_NextDownload_f
func (s *Server) nextDownload_f() {
	cl := s.client
	if cl.Download == nil {
		return
	}

	r := cl.DownloadSize - cl.DownloadCount
	if r > 1024 {
		r = 1024
	}

	m := &cl.Netchan.Message
	m.MSG_WriteByte(q2const.Svc_download)
	m.MSG_WriteShort(int32(r))

	cl.DownloadCount += r
	size := cl.DownloadSize
	if size == 0 {
		size = 1
	}
	percent := cl.DownloadCount * 100 / size
	m.MSG_WriteByte(int32(percent))
	m.SZ_Write(cl.Download[cl.DownloadCount-r : cl.DownloadCount])

	if cl.DownloadCount != cl.DownloadSize {
		return
	}

	cl.Download = nil
}

// beginDownload_f. Assets are served over HTTP (PARITY: svc_download
// replaced), so every request is answered "not found" exactly like a refused
// or missing file in C.
// C: server/sv_user.c:298 SV_BeginDownload_f
func (s *Server) beginDownload_f() {
	cl := s.client
	name := s.Cmd.Argv(1)
	s.DPrintf("Couldn't download %s to %s\n", name, cl.Name)
	cl.Download = nil

	m := &cl.Netchan.Message
	m.MSG_WriteByte(q2const.Svc_download)
	m.MSG_WriteShort(-1)
	m.MSG_WriteByte(0)
}

// disconnect_f: the client is going to disconnect, so remove the connection
// immediately.
// C: server/sv_user.c:374 SV_Disconnect_f
func (s *Server) disconnect_f() {
	s.DropClient(s.client)
}

// showServerinfo_f dumps the serverinfo info string.
// C: server/sv_user.c:388 SV_ShowServerinfo_f
func (s *Server) showServerinfo_f() {
	s.infoPrint(s.Cvars.Serverinfo())
}

// nextserver. C: server/sv_user.c:394 SV_Nextserver
func (s *Server) nextserver() {
	//ZOID, ss_pic can be nextserver'd in coop mode
	if s.SV.State == ss_game || (s.SV.State == ss_pic && s.Cvars.VariableValue("coop") == 0) {
		return // can't nextserver while playing a normal game
	}

	s.SVS.SpawnCount++ // make sure another doesn't sneak in
	v := s.Cvars.VariableString("nextserver")
	if v == "" {
		s.Cmd.Cbuf_AddText("killserver\n")
	} else {
		s.Cmd.Cbuf_AddText(v)
		s.Cmd.Cbuf_AddText("\n")
	}
	s.Cvars.Set("nextserver", "")
}

// nextserver_f: a cinematic has completed or been aborted by a client, so
// move to the next server.
// C: server/sv_user.c:421 SV_Nextserver_f
func (s *Server) nextserver_f() {
	if int(shared.Atoi(s.Cmd.Argv(1))) != s.SVS.SpawnCount {
		s.DPrintf("Nextserver() from wrong level, from %s\n", s.client.Name)
		return // leftover from last server
	}

	s.DPrintf("Nextserver() from %s\n", s.client.Name)

	s.nextserver()
}

// ucmd_t. C: server/sv_user.c:433 ucmds
type ucmd struct {
	name string
	fn   func(s *Server)
}

var ucmds = [...]ucmd{
	// auto issued
	{"new", (*Server).new_f},
	{"configstrings", (*Server).configstrings_f},
	{"baselines", (*Server).baselines_f},
	{"begin", (*Server).begin_f},

	{"nextserver", (*Server).nextserver_f},

	{"disconnect", (*Server).disconnect_f},

	// issued by hand at client consoles
	{"info", (*Server).showServerinfo_f},

	{"download", (*Server).beginDownload_f},
	{"nextdl", (*Server).nextDownload_f},
}

// executeUserCommand. C: server/sv_user.c:462 SV_ExecuteUserCommand
func (s *Server) executeUserCommand(str string) {
	// Security fix (not in C): client text is macro expanded, so
	// "say $rcon_password" disclosed any server cvar. Only public
	// (CVAR_SERVERINFO) cvars may be named; others expand to "".
	s.Cmd.MacroAllow = s.publicCvar
	s.Cmd.TokenizeString(str, true)
	s.Cmd.MacroAllow = nil
	s.player = s.client.Edict

	if s.cfg.ClientCommand != nil && s.cfg.ClientCommand(s.client) {
		return
	}

	for i := range ucmds {
		if s.Cmd.Argv(0) == ucmds[i].name {
			ucmds[i].fn(s)
			return
		}
	}

	if s.SV.State == ss_game {
		s.ge.ClientCommand(s.player)
	}
}

/*
===========================================================================

USER CMD EXECUTION

===========================================================================
*/

// publicCvar reports whether a client may expand $name: only cvars already
// published in the serverinfo string.
func (s *Server) publicCvar(name string) bool {
	v := s.Cvars.FindVar(name)
	return v != nil && v.Flags&q2const.CVAR_SERVERINFO != 0
}

// clientThink. C: server/sv_user.c:495 SV_ClientThink
func (s *Server) clientThink(cl *Client, cmd *shared.UserCmd) {
	cl.CommandMsec -= int(cmd.Msec)

	if cl.CommandMsec < 0 && s.svEnforcetime.Value != 0 {
		s.DPrintf("commandMsec underflow from %s\n", cl.Name)
		return
	}

	s.ge.ClientThink(cl.Edict, cmd)
}

// MAX_STRINGCMDS. C: server/sv_user.c:511
const MAX_STRINGCMDS = 8

// executeClientMessage parses the current net_message for the given client.
// C: server/sv_user.c:519 SV_ExecuteClientMessage
func (s *Server) executeClientMessage(cl *Client) {
	nm := s.netMsg
	s.client = cl
	s.player = s.client.Edict

	// only allow one move command
	moveIssued := false
	stringCmdCount := 0

	for {
		if nm.ReadCount > nm.CurSize {
			s.Printf("SV_ReadClientMessage: badread\n")
			s.DropClient(cl)
			return
		}

		c := nm.MSG_ReadByte()
		if c == -1 {
			break
		}

		switch c {
		default:
			s.Printf("SV_ReadClientMessage: unknown command char\n")
			s.DropClient(cl)
			return

		case q2const.Clc_nop:

		case q2const.Clc_userinfo:
			ui := nm.MSG_ReadString()
			if len(ui) > q2const.MAX_INFO_STRING-1 {
				ui = ui[:q2const.MAX_INFO_STRING-1]
			}
			if s.cfg.Userinfo != nil {
				ui = s.cfg.Userinfo(cl.Netchan.RemoteAddress, ui)
			}
			cl.Userinfo = ui
			s.UserinfoChanged(cl)

		case q2const.Clc_move:
			if moveIssued {
				return // someone is trying to cheat...
			}

			moveIssued = true
			checksumIndex := nm.ReadCount
			checksum := int(nm.MSG_ReadByte())
			lastframe := int(nm.MSG_ReadLong())
			if lastframe != cl.LastFrame {
				cl.LastFrame = lastframe
				if cl.LastFrame > 0 {
					cl.FrameLatency[cl.LastFrame&(LATENCY_COUNTS-1)] =
						s.SVS.RealTime - cl.Frames[cl.LastFrame&q2const.UPDATE_MASK].SentTime
				}
			}

			var nullcmd shared.UserCmd
			oldest := nm.MSG_ReadDeltaUsercmd(&nullcmd)
			oldcmd := nm.MSG_ReadDeltaUsercmd(&oldest)
			newcmd := nm.MSG_ReadDeltaUsercmd(&oldcmd)

			if cl.State != cs_spawned {
				cl.LastFrame = -1
				break
			}

			// if the checksum fails, ignore the rest of the packet
			// C reads stale net_message bytes when the usercmds ran past the
			// end of the packet; the buffer is reused the same way here, only
			// bounded by its size (memory-safety)
			// (a clc_move in the last byte of a full MAX_MSGLEN datagram puts
			// the start past the clamped end as well: clamp both)
			end := nm.ReadCount
			if end > len(nm.Data) {
				end = len(nm.Data)
			}
			begin := checksumIndex + 1
			if begin > end {
				begin = end
			}
			calculatedChecksum := int(crc.COM_BlockSequenceCRCByte(
				nm.Data[begin:end],
				int32(cl.Netchan.IncomingSequence)))

			if calculatedChecksum != checksum {
				s.DPrintf("Failed command checksum for %s (%d != %d)/%d\n",
					cl.Name, calculatedChecksum, checksum,
					cl.Netchan.IncomingSequence)
				return
			}

			if s.svPaused.Value == 0 {
				netDrop := cl.Netchan.Dropped
				if netDrop < 20 {
					for netDrop > 2 {
						s.clientThink(cl, &cl.LastCmd)
						netDrop--
					}
					if netDrop > 1 {
						s.clientThink(cl, &oldest)
					}
					if netDrop > 0 {
						s.clientThink(cl, &oldcmd)
					}
				}
				s.clientThink(cl, &newcmd)
			}

			cl.LastCmd = newcmd

		case q2const.Clc_stringcmd:
			str := nm.MSG_ReadString()

			// malicious users may try using too many string commands
			stringCmdCount++
			if stringCmdCount < MAX_STRINGCMDS {
				s.executeUserCommand(str)
			}

			if cl.State == cs_zombie {
				return // disconnect command
			}
		}
	}
}
