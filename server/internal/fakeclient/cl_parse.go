package fakeclient

import (
	"quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/msg"
	"quake2web/server/internal/qcommon/shared"
)

// parseServerMessage parses one sequenced server packet.
// C: client/cl_parse.c:652 CL_ParseServerMessage
func (c *Client) parseServerMessage(m *msg.SizeBuf) {
	for {
		if m.ReadCount > m.CurSize {
			shared.Error(q2const.ERR_DROP, "CL_ParseServerMessage: Bad server message")
		}
		start := m.ReadCount
		cmd := m.MSG_ReadByte()
		if cmd == -1 {
			break
		}
		switch cmd {
		default:
			shared.Error(q2const.ERR_DROP, "CL_ParseServerMessage: Illegible server message\n")

		case q2const.Svc_nop:

		case q2const.Svc_disconnect:
			shared.Error(q2const.ERR_DISCONNECT, "Server disconnected\n")

		case q2const.Svc_reconnect:
			c.printf("Server disconnected, reconnecting\n")
			c.State = CaConnecting
			c.connectTime = -99999 // CL_CheckForResend() will fire immediately

		case q2const.Svc_print:
			m.MSG_ReadByte() // print level
			s := m.MSG_ReadString()
			c.Prints = appendHistory(c.Prints, s, c.opt.MaxHistory, &c.Counts.Prints)
			c.printf("%s", s)

		case q2const.Svc_centerprint:
			c.CenterPrints = appendHistory(c.CenterPrints, m.MSG_ReadString(), c.opt.MaxHistory, &c.Counts.CenterPrints)

		case q2const.Svc_stufftext:
			s := m.MSG_ReadString()
			c.StuffTexts = appendHistory(c.StuffTexts, s, c.opt.MaxHistory, &c.Counts.StuffTexts)
			if !c.opt.Passive {
				c.cmd.Cbuf_AddText(s)
			}

		case q2const.Svc_serverdata:
			c.cmd.Cbuf_Execute() // make sure any stuffed commands are done
			c.parseServerData(m)

		case q2const.Svc_configstring:
			c.parseConfigString(m)

		case q2const.Svc_sound:
			c.parseStartSoundPacket(m)

		case q2const.Svc_spawnbaseline:
			c.parseBaseline(m)

		case q2const.Svc_temp_entity:
			c.parseTEnt(m)

		case q2const.Svc_muzzleflash:
			// C: client/cl_fx.c:238 CL_ParseMuzzleFlash
			i := m.MSG_ReadShort()
			if i < 1 || i >= q2const.MAX_EDICTS {
				shared.Error(q2const.ERR_DROP, "CL_ParseMuzzleFlash: bad entity")
			}
			weapon := m.MSG_ReadByte()
			c.MuzzleFlashes = appendHistory(c.MuzzleFlashes, MuzzleFlash{Ent: i, Weapon: weapon}, c.opt.MaxHistory, &c.Counts.MuzzleFlashes)

		case q2const.Svc_muzzleflash2:
			// C: client/cl_fx.c:429 CL_ParseMuzzleFlash2
			ent := m.MSG_ReadShort()
			if ent < 1 || ent >= q2const.MAX_EDICTS {
				shared.Error(q2const.ERR_DROP, "CL_ParseMuzzleFlash2: bad entity")
			}
			flash := m.MSG_ReadByte()
			c.MuzzleFlashes = appendHistory(c.MuzzleFlashes, MuzzleFlash{Ent: ent, Weapon: flash, Monster: true}, c.opt.MaxHistory, &c.Counts.MuzzleFlashes)

		case q2const.Svc_download:
			c.parseDownload(m)

		case q2const.Svc_frame:
			c.parseFrame(m)

		case q2const.Svc_inventory:
			// C: client/cl_inv.c:29 CL_ParseInventory
			for i := range c.Inventory {
				c.Inventory[i] = m.MSG_ReadShort()
			}
			c.Counts.Inventory++

		case q2const.Svc_layout:
			c.Layouts = appendHistory(c.Layouts, m.MSG_ReadString(), c.opt.MaxHistory, &c.Counts.Layouts)

		case q2const.Svc_playerinfo, q2const.Svc_packetentities, q2const.Svc_deltapacketentities:
			shared.Error(q2const.ERR_DROP, "Out of place frame data")
		}
		if c.wantSpans {
			c.spans = append(c.spans, Span{Cmd: cmd, Start: start, End: m.ReadCount})
		}
	}
}

// C: client/cl_parse.c:298 CL_ParseServerData
func (c *Client) parseServerData(m *msg.SizeBuf) {
	// wipe the client_state_t struct
	c.clearState()
	c.State = CaConnected
	c.levelGen++

	// parse protocol version number
	i := m.MSG_ReadLong()
	c.ServerData.Protocol = i
	if i != q2const.PROTOCOL_VERSION {
		shared.Error(q2const.ERR_DROP, "Server returned version %d, not %d", i, q2const.PROTOCOL_VERSION)
	}
	c.ServerData.ServerCount = m.MSG_ReadLong()
	c.ServerData.AttractLoop = m.MSG_ReadByte()
	c.ServerData.GameDir = m.MSG_ReadString()
	c.ServerData.PlayerNum = m.MSG_ReadShort()
	c.ServerData.LevelName = m.MSG_ReadString()
}

// C: client/cl_parse.c:519 CL_ParseConfigString
func (c *Client) parseConfigString(m *msg.SizeBuf) {
	i := m.MSG_ReadShort()
	if i < 0 || i >= q2const.MAX_CONFIGSTRINGS {
		shared.Error(q2const.ERR_DROP, "configstring > MAX_CONFIGSTRINGS")
	}
	c.ConfigStrings[i] = m.MSG_ReadString()
}

// C: client/cl_parse.c:359 CL_ParseBaseline
func (c *Client) parseBaseline(m *msg.SizeBuf) {
	var nullstate shared.EntityState
	newnum, bits := m.ParseEntityBits()
	if newnum < 0 || newnum >= q2const.MAX_EDICTS {
		shared.Error(q2const.ERR_DROP, "CL_ParseBaseline: bad number %d", newnum)
	}
	e := &c.Entities[newnum]
	e.Baseline = m.ParseDelta(&nullstate, newnum, bits)
	if !e.HasBaseline {
		e.HasBaseline = true
		c.NumBaselines++
	}
}

// C: client/cl_parse.c:581 CL_ParseStartSoundPacket
func (c *Client) parseStartSoundPacket(m *msg.SizeBuf) {
	var s Sound
	s.Flags = m.MSG_ReadByte()
	s.SoundNum = m.MSG_ReadByte()
	s.Volume = q2const.DEFAULT_SOUND_PACKET_VOLUME
	if s.Flags&q2const.SND_VOLUME != 0 {
		s.Volume = float32(float64(m.MSG_ReadByte()) / 255.0)
	}
	s.Attenuation = q2const.DEFAULT_SOUND_PACKET_ATTENUATION
	if s.Flags&q2const.SND_ATTENUATION != 0 {
		s.Attenuation = float32(float64(m.MSG_ReadByte()) / 64.0)
	}
	if s.Flags&q2const.SND_OFFSET != 0 {
		s.Ofs = float32(float64(m.MSG_ReadByte()) / 1000.0)
	}
	if s.Flags&q2const.SND_ENT != 0 { // entity reletive
		ch := m.MSG_ReadShort()
		s.Ent = ch >> 3
		if s.Ent > q2const.MAX_EDICTS {
			shared.Error(q2const.ERR_DROP, "CL_ParseStartSoundPacket: ent = %d", s.Ent)
		}
		s.Channel = ch & 7
	}
	if s.Flags&q2const.SND_POS != 0 { // positioned in space
		p := m.MSG_ReadPos()
		s.Pos = &p
	}
	c.Sounds = appendHistory(c.Sounds, s, c.opt.MaxHistory, &c.Counts.Sounds)
}

// parseDownload records the message; data is skipped (assets come over HTTP).
// A passive client never asks for the next block.
// C: client/cl_parse.c:200 CL_ParseDownload
func (c *Client) parseDownload(m *msg.SizeBuf) {
	size := m.MSG_ReadShort()
	percent := m.MSG_ReadByte()
	c.Downloads = appendHistory(c.Downloads, Download{Size: size, Percent: percent}, c.opt.MaxHistory, &c.Counts.Downloads)
	if size < 0 { // -1: not found (other negatives: memory safety)
		c.printf("Server does not have this file.\n")
		return
	}
	m.ReadCount += int(size)
	if percent != 100 && !c.opt.Passive {
		// request next block
		c.Netchan.Message.MSG_WriteByte(q2const.Clc_stringcmd)
		c.Netchan.Message.SZ_Print("nextdl")
	}
}
