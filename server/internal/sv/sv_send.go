package sv

import (
	"encoding/binary"
	"fmt"

	"quake2web/server/internal/game"
	qnet "quake2web/server/internal/net"
	"quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/msg"
	"quake2web/server/internal/qcommon/shared"
)

/*
=============================================================================

Com_Printf redirection

=============================================================================
*/

// flushRedirect. C: server/sv_send.c:34 SV_FlushRedirect
func (s *Server) flushRedirect(target int, outputbuf string) {
	if target == RD_PACKET {
		qnet.OutOfBandPrint(s.netVia, s.netFrom, "print\n"+outputbuf)
	} else if target == RD_CLIENT {
		s.client.Netchan.Message.MSG_WriteByte(q2const.Svc_print)
		s.client.Netchan.Message.MSG_WriteByte(q2const.PRINT_HIGH)
		s.client.Netchan.Message.MSG_WriteString(outputbuf)
	}
}

/*
=============================================================================

EVENT MESSAGES

=============================================================================
*/

// ClientPrintf sends text across to be displayed if the level passes.
// C: server/sv_send.c:64 SV_ClientPrintf
func (s *Server) ClientPrintf(cl *Client, level int, format string, args ...any) {
	if level < cl.MessageLevel {
		return
	}

	str := clip1024(fmt.Sprintf(format, args...))

	cl.Netchan.Message.MSG_WriteByte(q2const.Svc_print)
	cl.Netchan.Message.MSG_WriteByte(int32(level))
	cl.Netchan.Message.MSG_WriteString(str)
}

// BroadcastPrintf sends text to all active clients.
// C: server/sv_send.c:86 SV_BroadcastPrintf
func (s *Server) BroadcastPrintf(level int, format string, args ...any) {
	str := fmt.Sprintf(format, args...)
	if len(str) > 2047 {
		str = str[:2047]
	}

	// echo to console
	if s.dedicated.Value != 0 {
		// mask off high bits
		cp := []byte(str)
		if len(cp) > 1023 {
			cp = cp[:1023]
		}
		for i := range cp {
			cp[i] &= 127
		}
		s.Printf("%s", cString(cp))
	}

	for i := 0; i < s.maxClients(); i++ {
		cl := &s.SVS.Clients[i]
		if level < cl.MessageLevel {
			continue
		}
		if cl.State != cs_spawned {
			continue
		}
		cl.Netchan.Message.MSG_WriteByte(q2const.Svc_print)
		cl.Netchan.Message.MSG_WriteByte(int32(level))
		cl.Netchan.Message.MSG_WriteString(str)
	}
}

// BroadcastCommand sends text to all active clients.
// C: server/sv_send.c:127 SV_BroadcastCommand
func (s *Server) BroadcastCommand(format string, args ...any) {
	if s.SV.State == ss_dead {
		return
	}
	str := clip1024(fmt.Sprintf(format, args...))

	s.SV.Multicast.MSG_WriteByte(q2const.Svc_stufftext)
	s.SV.Multicast.MSG_WriteString(str)
	s.Multicast(nil, q2const.MULTICAST_ALL_R)
}

// Multicast sends the contents of sv.multicast to a subset of the clients,
// then clears sv.multicast.
// C: server/sv_send.c:155 SV_Multicast
func (s *Server) Multicast(origin *Vec3, to int) {
	reliable := false
	var area1 int32
	var leafnum int32
	var cluster int32
	var mask []byte
	cm := s.CM
	m := cm.Map()

	if to != q2const.MULTICAST_ALL_R && to != q2const.MULTICAST_ALL {
		leafnum = cm.PointLeafnum(*origin)
		area1 = m.LeafArea(int(leafnum))
	} else {
		leafnum = 0 // just to avoid compiler warnings
		area1 = 0
	}

	// if doing a serverrecord, store everything
	if s.SVS.DemoFile != nil {
		s.SVS.DemoMulticast.SZ_Write(s.SV.Multicast.Bytes())
	}

	switch to {
	case q2const.MULTICAST_ALL_R, q2const.MULTICAST_ALL:
		reliable = to == q2const.MULTICAST_ALL_R
		mask = nil

	case q2const.MULTICAST_PHS_R, q2const.MULTICAST_PHS:
		reliable = to == q2const.MULTICAST_PHS_R
		leafnum = cm.PointLeafnum(*origin)
		cluster = m.LeafCluster(int(leafnum))
		mask = cm.ClusterPHS(int(cluster))

	case q2const.MULTICAST_PVS_R, q2const.MULTICAST_PVS:
		reliable = to == q2const.MULTICAST_PVS_R
		leafnum = cm.PointLeafnum(*origin)
		cluster = m.LeafCluster(int(leafnum))
		mask = cm.ClusterPVS(int(cluster))

	default:
		shared.Error(q2const.ERR_FATAL, "SV_Multicast: bad to:%d", to)
	}

	// send the data to all relevent clients
	for j := 0; j < s.maxClients(); j++ {
		client := &s.SVS.Clients[j]
		if client.State == cs_free || client.State == cs_zombie {
			continue
		}
		if client.State != cs_spawned && !reliable {
			continue
		}

		if mask != nil {
			leafnum = cm.PointLeafnum(client.Edict.S.Origin)
			cluster = m.LeafCluster(int(leafnum))
			area2 := m.LeafArea(int(leafnum))
			if !cm.AreasConnected(int(area1), int(area2)) {
				continue
			}
			if !maskBit(mask, cluster) {
				continue
			}
		}

		if reliable {
			client.Netchan.Message.SZ_Write(s.SV.Multicast.Bytes())
		} else {
			client.Datagram.SZ_Write(s.SV.Multicast.Bytes())
		}
	}

	s.SV.Multicast.SZ_Clear()
}

// StartSound: each entity can have eight independant sound sources. If origin
// is nil, the origin is determined from the entity origin or the midpoint of
// the entity box for bmodels.
// C: server/sv_send.c:271 SV_StartSound
func (s *Server) StartSound(origin *Vec3, entity *game.Edict, channel, soundindex int,
	volume, attenuation, timeofs float32) {

	if volume < 0 || volume > 1.0 {
		shared.Error(q2const.ERR_FATAL, "SV_StartSound: volume = %f", volume)
	}

	if attenuation < 0 || attenuation > 4 {
		shared.Error(q2const.ERR_FATAL, "SV_StartSound: attenuation = %f", attenuation)
	}

	if timeofs < 0 || float64(timeofs) > 0.255 {
		shared.Error(q2const.ERR_FATAL, "SV_StartSound: timeofs = %f", timeofs)
	}

	ent := numForEdict(entity)

	var usePHS bool
	if channel&8 != 0 { // no PHS flag
		usePHS = false
		channel &= 7
	} else {
		usePHS = true
	}

	sendchan := (ent << 3) | (channel & 7)

	flags := 0
	if volume != q2const.DEFAULT_SOUND_PACKET_VOLUME {
		flags |= q2const.SND_VOLUME
	}
	if attenuation != q2const.DEFAULT_SOUND_PACKET_ATTENUATION {
		flags |= q2const.SND_ATTENUATION
	}

	// the client doesn't know that bmodels have weird origins
	// the origin can also be explicitly set
	if entity.SVFlags&q2const.SVF_NOCLIENT != 0 ||
		entity.Solid == q2const.SOLID_BSP ||
		origin != nil {
		flags |= q2const.SND_POS
	}

	// always send the entity number for channel overrides
	flags |= q2const.SND_ENT

	if timeofs != 0 {
		flags |= q2const.SND_OFFSET
	}

	// use the entity origin unless it is a bmodel or explicitly specified
	var originV Vec3
	if origin == nil {
		origin = &originV
		if entity.Solid == q2const.SOLID_BSP {
			for i := 0; i < 3; i++ {
				originV[i] = float32(float64(entity.S.Origin[i]) + 0.5*float64(entity.Mins[i]+entity.Maxs[i]))
			}
		} else {
			originV = entity.S.Origin
		}
	}

	mc := &s.SV.Multicast
	mc.MSG_WriteByte(q2const.Svc_sound)
	mc.MSG_WriteByte(int32(flags))
	mc.MSG_WriteByte(int32(soundindex))

	if flags&q2const.SND_VOLUME != 0 {
		mc.MSG_WriteByte(int32(volume * 255))
	}
	if flags&q2const.SND_ATTENUATION != 0 {
		mc.MSG_WriteByte(int32(attenuation * 64))
	}
	if flags&q2const.SND_OFFSET != 0 {
		mc.MSG_WriteByte(int32(timeofs * 1000))
	}

	if flags&q2const.SND_ENT != 0 {
		mc.MSG_WriteShort(int32(sendchan))
	}

	if flags&q2const.SND_POS != 0 {
		mc.MSG_WritePos(*origin)
	}

	// if the sound doesn't attenuate,send it to everyone
	// (global radio chatter, voiceovers, etc)
	if attenuation == q2const.ATTN_NONE {
		usePHS = false
	}

	if channel&q2const.CHAN_RELIABLE != 0 {
		if usePHS {
			s.Multicast(origin, q2const.MULTICAST_PHS_R)
		} else {
			s.Multicast(origin, q2const.MULTICAST_ALL_R)
		}
	} else {
		if usePHS {
			s.Multicast(origin, q2const.MULTICAST_PHS)
		} else {
			s.Multicast(origin, q2const.MULTICAST_ALL)
		}
	}
}

/*
===============================================================================

FRAME UPDATES

===============================================================================
*/

// sendClientDatagram. C: server/sv_send.c:402 SV_SendClientDatagram
func (s *Server) sendClientDatagram(client *Client) bool {
	var msgBuf [q2const.MAX_MSGLEN]byte
	var m msg.SizeBuf

	s.buildClientFrame(client)

	m.SZ_Init(msgBuf[:])
	m.AllowOverflow = true

	// send over all the relevant entity_state_t
	// and the player_state_t
	s.writeFrameToClient(client, &m)

	// copy the accumulated multicast datagram
	// for this client out to the message
	// it is necessary for this to be after the WriteEntities
	// so that entity references will be current
	if client.Datagram.Overflowed {
		s.Printf("WARNING: datagram overflowed for %s\n", client.Name)
	} else {
		m.SZ_Write(client.Datagram.Bytes())
	}
	client.Datagram.SZ_Clear()

	if m.Overflowed {
		// must have room left for the packet header
		s.Printf("WARNING: msg overflowed for %s\n", client.Name)
		m.SZ_Clear()
	}

	// send the datagram
	client.Netchan.Transmit(m.Bytes(), s.Milliseconds())

	// record the size for rate estimation
	client.MessageSize[s.SV.FrameNum%RATE_MESSAGES] = m.CurSize

	return true
}

// demoCompleted. C: server/sv_send.c:447 SV_DemoCompleted
func (s *Server) demoCompleted() {
	if s.SV.demoOpen {
		s.SV.DemoFile = nil
		s.SV.demoOpen = false
	}
	s.nextserver()
}

// rateDrop returns true if the client is over its current bandwidth
// estimation and should not be sent another packet.
// C: server/sv_send.c:467 SV_RateDrop
func (s *Server) rateDrop(c *Client) bool {
	// never drop over the loopback
	if qnet.IsLocalAddress(c.Netchan.RemoteAddress) {
		return false
	}

	total := 0
	for i := 0; i < RATE_MESSAGES; i++ {
		total += c.MessageSize[i]
	}

	if total > c.Rate {
		c.SurpressCount++
		c.MessageSize[s.SV.FrameNum%RATE_MESSAGES] = 0
		return true
	}

	return false
}

// SendClientMessages. C: server/sv_send.c:497 SV_SendClientMessages
func (s *Server) SendClientMessages() {
	var msgbuf []byte
	msglen := 0

	// read the next demo message if needed
	if s.SV.State == ss_demo && s.SV.demoOpen {
		if s.svPaused.Value != 0 {
			msglen = 0
		} else {
			// get the next message
			d := s.SV.DemoFile
			if s.SV.demoPos+4 > len(d) {
				s.demoCompleted()
				return
			}
			msglen = int(int32(binary.LittleEndian.Uint32(d[s.SV.demoPos:])))
			s.SV.demoPos += 4
			if msglen == -1 {
				s.demoCompleted()
				return
			}
			if msglen > q2const.MAX_MSGLEN {
				shared.Error(q2const.ERR_DROP, "SV_SendClientMessages: msglen > MAX_MSGLEN")
			}
			if msglen < 0 || s.SV.demoPos+msglen > len(d) {
				s.demoCompleted()
				return
			}
			msgbuf = d[s.SV.demoPos : s.SV.demoPos+msglen]
			s.SV.demoPos += msglen
		}
	}

	// send a message to each connected client
	for i := 0; i < s.maxClients(); i++ {
		c := &s.SVS.Clients[i]
		if c.State == cs_free {
			continue
		}
		// if the reliable message overflowed,
		// drop the client
		if c.Netchan.Message.Overflowed {
			c.Netchan.Message.SZ_Clear()
			c.Datagram.SZ_Clear()
			s.BroadcastPrintf(q2const.PRINT_HIGH, "%s overflowed\n", c.Name)
			s.DropClient(c)
		}

		if s.SV.State == ss_cinematic || s.SV.State == ss_demo || s.SV.State == ss_pic {
			c.Netchan.Transmit(msgbuf[:msglen], s.Milliseconds())
		} else if c.State == cs_spawned {
			// don't overrun bandwidth
			if s.rateDrop(c) {
				continue
			}
			s.sendClientDatagram(c)
		} else {
			// just update reliable	if needed
			if c.Netchan.Message.CurSize != 0 || s.Milliseconds()-c.Netchan.LastSent > 1000 {
				c.Netchan.Transmit(nil, s.Milliseconds())
			}
		}
	}
}
