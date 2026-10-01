package fakeclient

import (
	"quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/crc"
	"quake2web/server/internal/qcommon/msg"
	"quake2web/server/internal/qcommon/shared"
)

// SendCmd builds and sends one move packet with cmd as the newest command
// (CL_CreateCmd is replaced by the argument). While only connected it sends
// the pending reliable data / keepalive instead, like the C client. It returns
// the unreliable payload that was transmitted (nil when no move was sent). A
// passive client sends nothing.
// C: client/cl_input.c:453 CL_SendCmd
func (c *Client) SendCmd(ucmd shared.UserCmd) []byte {
	if c.opt.Passive {
		return nil
	}

	// save this command off for prediction
	i := c.Netchan.OutgoingSequence & (CMD_BACKUP - 1)
	c.cmds[i] = ucmd

	if c.State == CaDisconnected || c.State == CaConnecting {
		return nil
	}

	if c.State == CaConnected {
		c.sendConnected()
		return nil
	}

	// send a userinfo update if needed
	if c.userinfoMod {
		c.userinfoMod = false
		c.Netchan.Message.MSG_WriteByte(q2const.Clc_userinfo)
		c.Netchan.Message.MSG_WriteString(c.Userinfo)
	}

	var buf msg.SizeBuf
	buf.SZ_Init(make([]byte, 128))

	// begin a client move command
	buf.MSG_WriteByte(q2const.Clc_move)

	// save the position for a checksum byte
	checksumIndex := buf.CurSize
	buf.MSG_WriteByte(0)

	// let the server know what the last frame we
	// got was, so the next message can be delta compressed
	if c.opt.NoDelta || !c.Frame.Valid || c.demoWaiting {
		buf.MSG_WriteLong(-1) // no compression
	} else {
		buf.MSG_WriteLong(c.Frame.ServerFrame)
	}

	// send this and the previous cmds in the message, so
	// if the last packet was dropped, it can be recovered
	var nullcmd shared.UserCmd
	cmd := &c.cmds[(c.Netchan.OutgoingSequence-2)&(CMD_BACKUP-1)]
	buf.MSG_WriteDeltaUsercmd(&nullcmd, cmd)
	oldcmd := cmd

	cmd = &c.cmds[(c.Netchan.OutgoingSequence-1)&(CMD_BACKUP-1)]
	buf.MSG_WriteDeltaUsercmd(oldcmd, cmd)
	oldcmd = cmd

	cmd = &c.cmds[c.Netchan.OutgoingSequence&(CMD_BACKUP-1)]
	buf.MSG_WriteDeltaUsercmd(oldcmd, cmd)

	// calculate a checksum over the move commands
	buf.Data[checksumIndex] = crc.COM_BlockSequenceCRCByte(
		buf.Data[checksumIndex+1:buf.CurSize], int32(c.Netchan.OutgoingSequence))

	// deliver the message
	payload := append([]byte(nil), buf.Bytes()...)
	c.Netchan.Transmit(payload, c.curtime())
	return payload
}
