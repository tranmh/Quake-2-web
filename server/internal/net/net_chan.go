package net

import (
	"quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/msg"
)

// Netchan is C netchan_t.
// C: qcommon/qcommon.h:565 netchan_t
type Netchan struct {
	FatalError bool

	Sock int // NS_CLIENT or NS_SERVER

	Dropped int // between last packet and previous

	LastReceived int // for timeouts
	LastSent     int // for retransmits

	RemoteAddress Addr
	Out           Sender // transport the remote address is reached through
	Qport         int    // qport value to write when transmitting

	// sequencing variables
	IncomingSequence             int
	IncomingAcknowledged         int
	IncomingReliableAcknowledged int // single bit

	IncomingReliableSequence int // single bit, maintained local

	OutgoingSequence     int
	ReliableSequence     int // single bit
	LastReliableSequence int // sequence number of last send

	// reliable staging and holding areas
	Message    msg.SizeBuf // writing buffer to send to server
	messageBuf [q2const.MAX_MSGLEN - 16]byte

	// message is copied to this buffer when it is first transfered
	ReliableLength int
	ReliableBuf    [q2const.MAX_MSGLEN - 16]byte

	// Printf receives Com_Printf output (may be nil).
	Printf func(format string, args ...any)
	// ShowPackets / ShowDrop mirror the showpackets / showdrop cvars.
	ShowPackets, ShowDrop func() bool
}

func (chan_ *Netchan) printf(format string, args ...any) {
	if chan_.Printf != nil {
		chan_.Printf(format, args...)
	}
}

// OutOfBand sends an out-of-band datagram.
// C: qcommon/net_chan.c:106 Netchan_OutOfBand
func OutOfBand(out Sender, adr Addr, data []byte) {
	send := msg.NewSizeBuf(q2const.MAX_MSGLEN)
	send.MSG_WriteLong(-1) // -1 sequence means out of band
	send.SZ_Write(data)
	if out != nil {
		_ = out.SendPacket(adr, send.Bytes())
	}
}

// OutOfBandPrint sends a text message in an out-of-band datagram.
// C: qcommon/net_chan.c:127 Netchan_OutOfBandPrint
func OutOfBandPrint(out Sender, adr Addr, text string) {
	if len(text) > q2const.MAX_MSGLEN-4-1 {
		text = text[:q2const.MAX_MSGLEN-4-1] // static char string[MAX_MSGLEN - 4]
	}
	OutOfBand(out, adr, []byte(text))
}

// Setup opens a channel to a remote system.
// C: qcommon/net_chan.c:147 Netchan_Setup
func (chan_ *Netchan) Setup(sock int, adr Addr, out Sender, qport int, curtime int) {
	printf, sp, sd := chan_.Printf, chan_.ShowPackets, chan_.ShowDrop
	*chan_ = Netchan{}
	chan_.Printf, chan_.ShowPackets, chan_.ShowDrop = printf, sp, sd

	chan_.Sock = sock
	chan_.RemoteAddress = adr
	chan_.Out = out
	chan_.Qport = qport
	chan_.LastReceived = curtime
	chan_.IncomingSequence = 0
	chan_.OutgoingSequence = 1

	chan_.Message.SZ_Init(chan_.messageBuf[:])
	chan_.Message.AllowOverflow = true
}

// CanReliable returns true if the last reliable message has acked.
// C: qcommon/net_chan.c:171 Netchan_CanReliable
func (chan_ *Netchan) CanReliable() bool {
	return chan_.ReliableLength == 0
}

// NeedReliable reports whether the next Transmit carries the reliable buffer.
// C: qcommon/net_chan.c:179 Netchan_NeedReliable
func (chan_ *Netchan) NeedReliable() bool {
	sendReliable := false

	// if the remote side dropped the last reliable message, resend it
	if chan_.IncomingAcknowledged > chan_.LastReliableSequence &&
		chan_.IncomingReliableAcknowledged != chan_.ReliableSequence {
		sendReliable = true
	}

	// if the reliable transmit buffer is empty, copy the current message out
	if chan_.ReliableLength == 0 && chan_.Message.CurSize != 0 {
		sendReliable = true
	}
	return sendReliable
}

// Transmit tries to send an unreliable message to a connection, and handles
// the transmition / retransmition of the reliable messages. A 0 length will
// still generate a packet and deal with the reliable messages. It returns the
// datagram that was sent (nil when nothing was sent).
// C: qcommon/net_chan.c:207 Netchan_Transmit
func (chan_ *Netchan) Transmit(data []byte, curtime int) []byte {
	// check for message overflow
	if chan_.Message.Overflowed {
		chan_.FatalError = true
		chan_.printf("%s:Outgoing message overflow\n", chan_.RemoteAddress)
		return nil
	}

	sendReliable := chan_.NeedReliable()

	if chan_.ReliableLength == 0 && chan_.Message.CurSize != 0 {
		copy(chan_.ReliableBuf[:], chan_.messageBuf[:chan_.Message.CurSize])
		chan_.ReliableLength = chan_.Message.CurSize
		chan_.Message.CurSize = 0
		chan_.ReliableSequence ^= 1
	}

	// write the packet header
	var sendBuf [q2const.MAX_MSGLEN]byte
	var send msg.SizeBuf
	send.SZ_Init(sendBuf[:])

	var rel uint32
	if sendReliable {
		rel = 1
	}
	w1 := (uint32(chan_.OutgoingSequence) &^ (1 << 31)) | (rel << 31)
	w2 := (uint32(chan_.IncomingSequence) &^ (1 << 31)) | (uint32(chan_.IncomingReliableSequence) << 31)

	chan_.OutgoingSequence++
	chan_.LastSent = curtime

	send.MSG_WriteLong(int32(w1))
	send.MSG_WriteLong(int32(w2))

	// send the qport if we are a client
	if chan_.Sock == q2const.NS_CLIENT {
		send.MSG_WriteShort(int32(chan_.Qport))
	}

	// copy the reliable message to the packet first
	if sendReliable {
		send.SZ_Write(chan_.ReliableBuf[:chan_.ReliableLength])
		chan_.LastReliableSequence = chan_.OutgoingSequence
	}

	// add the unreliable part if space is available
	if send.MaxSize-send.CurSize >= len(data) {
		send.SZ_Write(data)
	} else {
		chan_.printf("Netchan_Transmit: dumped unreliable\n")
	}

	// send the datagram
	out := append([]byte(nil), send.Bytes()...)
	if chan_.Out != nil {
		_ = chan_.Out.SendPacket(chan_.RemoteAddress, out)
	}

	if chan_.ShowPackets != nil && chan_.ShowPackets() {
		if sendReliable {
			chan_.printf("send %4d : s=%d reliable=%d ack=%d rack=%d\n",
				send.CurSize, chan_.OutgoingSequence-1, chan_.ReliableSequence,
				chan_.IncomingSequence, chan_.IncomingReliableSequence)
		} else {
			chan_.printf("send %4d : s=%d ack=%d rack=%d\n",
				send.CurSize, chan_.OutgoingSequence-1,
				chan_.IncomingSequence, chan_.IncomingReliableSequence)
		}
	}
	return out
}

// Process is called when the current net_message is from remote_address; it
// leaves the read cursor of m at the packet payload.
// C: qcommon/net_chan.c:298 Netchan_Process
func (chan_ *Netchan) Process(m *msg.SizeBuf, curtime int) bool {
	// get sequence numbers
	m.MSG_BeginReading()
	sequence := uint32(m.MSG_ReadLong())
	sequenceAck := uint32(m.MSG_ReadLong())

	// read the qport if we are a server
	if chan_.Sock == q2const.NS_SERVER {
		_ = m.MSG_ReadShort()
	}

	reliableMessage := sequence >> 31
	reliableAck := sequenceAck >> 31

	sequence &^= 1 << 31
	sequenceAck &^= 1 << 31

	if chan_.ShowPackets != nil && chan_.ShowPackets() {
		if reliableMessage != 0 {
			chan_.printf("recv %4d : s=%d reliable=%d ack=%d rack=%d\n",
				m.CurSize, sequence, chan_.IncomingReliableSequence^1, sequenceAck, reliableAck)
		} else {
			chan_.printf("recv %4d : s=%d ack=%d rack=%d\n",
				m.CurSize, sequence, sequenceAck, reliableAck)
		}
	}

	// discard stale or duplicated packets
	// (C compares unsigned sequence with the int incoming_sequence: unsigned)
	if sequence <= uint32(chan_.IncomingSequence) {
		if chan_.ShowDrop != nil && chan_.ShowDrop() {
			chan_.printf("%s:Out of order packet %d at %d\n",
				chan_.RemoteAddress, sequence, chan_.IncomingSequence)
		}
		return false
	}

	// dropped packets don't keep the message from being used
	chan_.Dropped = int(int32(sequence - uint32(chan_.IncomingSequence+1)))
	if chan_.Dropped > 0 {
		if chan_.ShowDrop != nil && chan_.ShowDrop() {
			chan_.printf("%s:Dropped %d packets at %d\n",
				chan_.RemoteAddress, chan_.Dropped, sequence)
		}
	}

	// if the current outgoing reliable message has been acknowledged
	// clear the buffer to make way for the next
	if int(reliableAck) == chan_.ReliableSequence {
		chan_.ReliableLength = 0 // it has been received
	}

	// if this message contains a reliable message, bump incoming_reliable_sequence
	chan_.IncomingSequence = int(sequence)
	chan_.IncomingAcknowledged = int(sequenceAck)
	chan_.IncomingReliableAcknowledged = int(reliableAck)
	if reliableMessage != 0 {
		chan_.IncomingReliableSequence ^= 1
	}

	// the message can now be read from the current message pointer
	chan_.LastReceived = curtime
	return true
}
