// Package msg ports the message buffer code of qcommon/common.c (SZ_* and
// MSG_*), the player state delta writer of server/sv_ents.c and the entity /
// player state delta readers of client/cl_ents.c. Output is byte-exact with
// the C engine.
package msg

import (
	"math"
	"strings"

	"quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/shared"
)

// SizeBuf is C sizebuf_t. Invariant: MaxSize <= len(Data), CurSize <= MaxSize.
// C: qcommon/qcommon.h:75 sizebuf_t
type SizeBuf struct {
	AllowOverflow bool // if false, do a Com_Error
	Overflowed    bool // set to true if the buffer size failed
	Data          []byte
	MaxSize       int
	CurSize       int
	ReadCount     int
}

// SZ_Init resets buf to use data as storage.
// C: qcommon/common.c:876 SZ_Init
func (buf *SizeBuf) SZ_Init(data []byte) {
	*buf = SizeBuf{Data: data, MaxSize: len(data)}
}

// NewSizeBuf returns a writable buffer of maxsize bytes.
func NewSizeBuf(maxsize int) *SizeBuf {
	b := &SizeBuf{}
	b.SZ_Init(make([]byte, maxsize))
	return b
}

// NewReader returns a buffer holding data ready for MSG_Read* (cursize =
// len(data), readcount = 0).
func NewReader(data []byte) *SizeBuf {
	b := &SizeBuf{}
	b.SZ_Init(data)
	b.CurSize = len(data)
	return b
}

// Bytes returns the written part of the buffer.
func (buf *SizeBuf) Bytes() []byte { return buf.Data[:buf.CurSize] }

// SZ_Clear empties the buffer.
// C: qcommon/common.c:883 SZ_Clear
func (buf *SizeBuf) SZ_Clear() {
	buf.CurSize = 0
	buf.Overflowed = false
}

// SZ_GetSpace reserves length bytes and returns them.
// C: qcommon/common.c:889 SZ_GetSpace
func (buf *SizeBuf) SZ_GetSpace(length int) []byte {
	if buf.CurSize+length > buf.MaxSize {
		if !buf.AllowOverflow {
			shared.Error(q2const.ERR_FATAL, "SZ_GetSpace: overflow without allowoverflow set")
		}
		if length > buf.MaxSize {
			shared.Error(q2const.ERR_FATAL, "SZ_GetSpace: %d is > full buffer size", length)
		}
		// Com_Printf ("SZ_GetSpace: overflow\n");
		buf.SZ_Clear()
		buf.Overflowed = true
	}
	data := buf.Data[buf.CurSize : buf.CurSize+length]
	buf.CurSize += length
	return data
}

// SZ_Write appends data.
// C: qcommon/common.c:912 SZ_Write
func (buf *SizeBuf) SZ_Write(data []byte) {
	copy(buf.SZ_GetSpace(len(data)), data)
}

// SZ_Print appends a NUL-terminated string, overwriting a previous trailing 0.
// C: qcommon/common.c:917 SZ_Print
func (buf *SizeBuf) SZ_Print(data string) {
	data = cstr(data)
	length := len(data) + 1
	if buf.CurSize != 0 && buf.Data[buf.CurSize-1] == 0 {
		// write over trailing 0: memcpy(SZ_GetSpace(buf, len-1)-1, data, len)
		buf.SZ_GetSpace(length - 1)
		start := buf.CurSize - length
		for i := 0; i < length; i++ {
			c := byte(0)
			if i < len(data) {
				c = data[i]
			}
			// After an overflow clear C writes one byte before the buffer
			// (undefined behavior); memory-safety fix: that byte is dropped.
			if start+i >= 0 {
				buf.Data[start+i] = c
			}
		}
		return
	}
	// empty buffer, or no trailing 0
	sp := buf.SZ_GetSpace(length)
	copy(sp, data)
	sp[length-1] = 0
}

func cstr(s string) string {
	if i := strings.IndexByte(s, 0); i >= 0 {
		return s[:i]
	}
	return s
}

//
// writing functions
//

// MSG_WriteChar writes the low byte of c.
// C: qcommon/common.c:281 MSG_WriteChar
func (sb *SizeBuf) MSG_WriteChar(c int32) {
	sb.SZ_GetSpace(1)[0] = byte(c)
}

// MSG_WriteByte writes the low byte of c.
// C: qcommon/common.c:294 MSG_WriteByte
func (sb *SizeBuf) MSG_WriteByte(c int32) {
	sb.SZ_GetSpace(1)[0] = byte(c)
}

// MSG_WriteShort writes the low 16 bits of c, little endian.
// C: qcommon/common.c:307 MSG_WriteShort
func (sb *SizeBuf) MSG_WriteShort(c int32) {
	buf := sb.SZ_GetSpace(2)
	buf[0] = byte(c & 0xff)
	buf[1] = byte(c >> 8)
}

// MSG_WriteLong writes c little endian.
// C: qcommon/common.c:321 MSG_WriteLong
func (sb *SizeBuf) MSG_WriteLong(c int32) {
	buf := sb.SZ_GetSpace(4)
	buf[0] = byte(c & 0xff)
	buf[1] = byte((c >> 8) & 0xff)
	buf[2] = byte((c >> 16) & 0xff)
	buf[3] = byte(c >> 24)
}

// MSG_WriteFloat writes the IEEE bits of f little endian.
// C: qcommon/common.c:332 MSG_WriteFloat
func (sb *SizeBuf) MSG_WriteFloat(f float32) {
	sb.MSG_WriteLong(int32(math.Float32bits(f)))
}

// MSG_WriteString writes s and a terminating 0 (s is cut at an embedded NUL,
// like C strlen).
// C: qcommon/common.c:347 MSG_WriteString
func (sb *SizeBuf) MSG_WriteString(s string) {
	s = cstr(s)
	buf := sb.SZ_GetSpace(len(s) + 1)
	copy(buf, s)
	buf[len(s)] = 0
}

// MSG_WriteCoord writes (int)(f*8) as a short (float multiply, truncation).
// C: qcommon/common.c:355 MSG_WriteCoord
func (sb *SizeBuf) MSG_WriteCoord(f float32) {
	sb.MSG_WriteShort(int32(f * 8))
}

// MSG_WritePos writes three coords.
// C: qcommon/common.c:360 MSG_WritePos
func (sb *SizeBuf) MSG_WritePos(pos shared.Vec3) {
	sb.MSG_WriteShort(int32(pos[0] * 8))
	sb.MSG_WriteShort(int32(pos[1] * 8))
	sb.MSG_WriteShort(int32(pos[2] * 8))
}

// MSG_WriteAngle writes (int)(f*256/360) & 255, evaluated in float.
// C: qcommon/common.c:367 MSG_WriteAngle
func (sb *SizeBuf) MSG_WriteAngle(f float32) {
	sb.MSG_WriteByte(int32(float32(f*256)/360) & 255)
}

// MSG_WriteAngle16 writes ANGLE2SHORT(f).
// C: qcommon/common.c:372 MSG_WriteAngle16
func (sb *SizeBuf) MSG_WriteAngle16(f float32) {
	sb.MSG_WriteShort(shared.ANGLE2SHORT(f))
}

// MSG_WriteDeltaUsercmd writes cmd delta-compressed against from.
// C: qcommon/common.c:378 MSG_WriteDeltaUsercmd
func (buf *SizeBuf) MSG_WriteDeltaUsercmd(from, cmd *shared.UserCmd) {
	var bits int32
	if cmd.Angles[0] != from.Angles[0] {
		bits |= q2const.CM_ANGLE1
	}
	if cmd.Angles[1] != from.Angles[1] {
		bits |= q2const.CM_ANGLE2
	}
	if cmd.Angles[2] != from.Angles[2] {
		bits |= q2const.CM_ANGLE3
	}
	if cmd.ForwardMove != from.ForwardMove {
		bits |= q2const.CM_FORWARD
	}
	if cmd.SideMove != from.SideMove {
		bits |= q2const.CM_SIDE
	}
	if cmd.UpMove != from.UpMove {
		bits |= q2const.CM_UP
	}
	if cmd.Buttons != from.Buttons {
		bits |= q2const.CM_BUTTONS
	}
	if cmd.Impulse != from.Impulse {
		bits |= q2const.CM_IMPULSE
	}

	buf.MSG_WriteByte(bits)

	if bits&q2const.CM_ANGLE1 != 0 {
		buf.MSG_WriteShort(int32(cmd.Angles[0]))
	}
	if bits&q2const.CM_ANGLE2 != 0 {
		buf.MSG_WriteShort(int32(cmd.Angles[1]))
	}
	if bits&q2const.CM_ANGLE3 != 0 {
		buf.MSG_WriteShort(int32(cmd.Angles[2]))
	}
	if bits&q2const.CM_FORWARD != 0 {
		buf.MSG_WriteShort(int32(cmd.ForwardMove))
	}
	if bits&q2const.CM_SIDE != 0 {
		buf.MSG_WriteShort(int32(cmd.SideMove))
	}
	if bits&q2const.CM_UP != 0 {
		buf.MSG_WriteShort(int32(cmd.UpMove))
	}
	if bits&q2const.CM_BUTTONS != 0 {
		buf.MSG_WriteByte(int32(cmd.Buttons))
	}
	if bits&q2const.CM_IMPULSE != 0 {
		buf.MSG_WriteByte(int32(cmd.Impulse))
	}

	buf.MSG_WriteByte(int32(cmd.Msec))
	buf.MSG_WriteByte(int32(cmd.LightLevel))
}

// MSG_WriteDir writes the index of the bytedirs normal with the largest dot
// product (0 when dir is nil or nothing beats 0).
// C: qcommon/common.c:429 MSG_WriteDir
func (sb *SizeBuf) MSG_WriteDir(dir *shared.Vec3) {
	if dir == nil {
		sb.MSG_WriteByte(0)
		return
	}
	var bestd float32
	best := 0
	for i := 0; i < q2const.NUMVERTEXNORMALS; i++ {
		d := shared.DotProduct(*dir, q2const.ByteDirs[i])
		if d > bestd {
			bestd = d
			best = i
		}
	}
	sb.MSG_WriteByte(int32(best))
}

// MSG_ReadDir reads a bytedirs index; out of range raises ERR_DROP.
// C: qcommon/common.c:455 MSG_ReadDir
func (sb *SizeBuf) MSG_ReadDir() shared.Vec3 {
	b := sb.MSG_ReadByte()
	if b >= q2const.NUMVERTEXNORMALS {
		shared.Error(q2const.ERR_DROP, "MSF_ReadDir: out of range")
	}
	if b < 0 {
		// C indexes bytedirs[-1] (reads before the table) when the
		// message is exhausted; memory-safety fix: treat as a drop.
		shared.Error(q2const.ERR_DROP, "MSF_ReadDir: out of range")
	}
	return q2const.ByteDirs[b]
}

// MSG_WriteDeltaEntity writes part of a packetentities message. Can delta from
// either a baseline or a previous packet_entity.
// C: qcommon/common.c:474 MSG_WriteDeltaEntity
func (msg *SizeBuf) MSG_WriteDeltaEntity(from, to *shared.EntityState, force, newentity bool) {
	if to.Number == 0 {
		shared.Error(q2const.ERR_FATAL, "Unset entity number")
	}
	if to.Number >= q2const.MAX_EDICTS {
		shared.Error(q2const.ERR_FATAL, "Entity number >= MAX_EDICTS")
	}

	// send an update
	var bits uint32

	if to.Number >= 256 {
		bits |= q2const.U_NUMBER16 // number8 is implicit otherwise
	}

	if to.Origin[0] != from.Origin[0] {
		bits |= q2const.U_ORIGIN1
	}
	if to.Origin[1] != from.Origin[1] {
		bits |= q2const.U_ORIGIN2
	}
	if to.Origin[2] != from.Origin[2] {
		bits |= q2const.U_ORIGIN3
	}

	if to.Angles[0] != from.Angles[0] {
		bits |= q2const.U_ANGLE1
	}
	if to.Angles[1] != from.Angles[1] {
		bits |= q2const.U_ANGLE2
	}
	if to.Angles[2] != from.Angles[2] {
		bits |= q2const.U_ANGLE3
	}

	if to.SkinNum != from.SkinNum {
		if uint32(to.SkinNum) < 256 {
			bits |= q2const.U_SKIN8
		} else if uint32(to.SkinNum) < 0x10000 {
			bits |= q2const.U_SKIN16
		} else {
			bits |= q2const.U_SKIN8 | q2const.U_SKIN16
		}
	}

	if to.Frame != from.Frame {
		if to.Frame < 256 {
			bits |= q2const.U_FRAME8
		} else {
			bits |= q2const.U_FRAME16
		}
	}

	if to.Effects != from.Effects {
		if to.Effects < 256 {
			bits |= q2const.U_EFFECTS8
		} else if to.Effects < 0x8000 {
			bits |= q2const.U_EFFECTS16
		} else {
			bits |= q2const.U_EFFECTS8 | q2const.U_EFFECTS16
		}
	}

	if to.RenderFX != from.RenderFX {
		if to.RenderFX < 256 {
			bits |= q2const.U_RENDERFX8
		} else if to.RenderFX < 0x8000 {
			bits |= q2const.U_RENDERFX16
		} else {
			bits |= q2const.U_RENDERFX8 | q2const.U_RENDERFX16
		}
	}

	if to.Solid != from.Solid {
		bits |= q2const.U_SOLID
	}

	// event is not delta compressed, just 0 compressed
	if to.Event != 0 {
		bits |= q2const.U_EVENT
	}

	if to.ModelIndex != from.ModelIndex {
		bits |= q2const.U_MODEL
	}
	if to.ModelIndex2 != from.ModelIndex2 {
		bits |= q2const.U_MODEL2
	}
	if to.ModelIndex3 != from.ModelIndex3 {
		bits |= q2const.U_MODEL3
	}
	if to.ModelIndex4 != from.ModelIndex4 {
		bits |= q2const.U_MODEL4
	}

	if to.Sound != from.Sound {
		bits |= q2const.U_SOUND
	}

	if newentity || (to.RenderFX&q2const.RF_BEAM != 0) {
		bits |= q2const.U_OLDORIGIN
	}

	//
	// write the message
	//
	if bits == 0 && !force {
		return // nothing to send!
	}

	//----------

	if bits&0xff000000 != 0 {
		bits |= q2const.U_MOREBITS3 | q2const.U_MOREBITS2 | q2const.U_MOREBITS1
	} else if bits&0x00ff0000 != 0 {
		bits |= q2const.U_MOREBITS2 | q2const.U_MOREBITS1
	} else if bits&0x0000ff00 != 0 {
		bits |= q2const.U_MOREBITS1
	}

	msg.MSG_WriteByte(int32(bits & 255))

	if bits&0xff000000 != 0 {
		msg.MSG_WriteByte(int32((bits >> 8) & 255))
		msg.MSG_WriteByte(int32((bits >> 16) & 255))
		msg.MSG_WriteByte(int32((bits >> 24) & 255))
	} else if bits&0x00ff0000 != 0 {
		msg.MSG_WriteByte(int32((bits >> 8) & 255))
		msg.MSG_WriteByte(int32((bits >> 16) & 255))
	} else if bits&0x0000ff00 != 0 {
		msg.MSG_WriteByte(int32((bits >> 8) & 255))
	}

	//----------

	if bits&q2const.U_NUMBER16 != 0 {
		msg.MSG_WriteShort(to.Number)
	} else {
		msg.MSG_WriteByte(to.Number)
	}

	if bits&q2const.U_MODEL != 0 {
		msg.MSG_WriteByte(to.ModelIndex)
	}
	if bits&q2const.U_MODEL2 != 0 {
		msg.MSG_WriteByte(to.ModelIndex2)
	}
	if bits&q2const.U_MODEL3 != 0 {
		msg.MSG_WriteByte(to.ModelIndex3)
	}
	if bits&q2const.U_MODEL4 != 0 {
		msg.MSG_WriteByte(to.ModelIndex4)
	}

	if bits&q2const.U_FRAME8 != 0 {
		msg.MSG_WriteByte(to.Frame)
	}
	if bits&q2const.U_FRAME16 != 0 {
		msg.MSG_WriteShort(to.Frame)
	}

	if bits&q2const.U_SKIN8 != 0 && bits&q2const.U_SKIN16 != 0 { //used for laser colors
		msg.MSG_WriteLong(to.SkinNum)
	} else if bits&q2const.U_SKIN8 != 0 {
		msg.MSG_WriteByte(to.SkinNum)
	} else if bits&q2const.U_SKIN16 != 0 {
		msg.MSG_WriteShort(to.SkinNum)
	}

	if bits&(q2const.U_EFFECTS8|q2const.U_EFFECTS16) == q2const.U_EFFECTS8|q2const.U_EFFECTS16 {
		msg.MSG_WriteLong(int32(to.Effects))
	} else if bits&q2const.U_EFFECTS8 != 0 {
		msg.MSG_WriteByte(int32(to.Effects))
	} else if bits&q2const.U_EFFECTS16 != 0 {
		msg.MSG_WriteShort(int32(to.Effects))
	}

	if bits&(q2const.U_RENDERFX8|q2const.U_RENDERFX16) == q2const.U_RENDERFX8|q2const.U_RENDERFX16 {
		msg.MSG_WriteLong(to.RenderFX)
	} else if bits&q2const.U_RENDERFX8 != 0 {
		msg.MSG_WriteByte(to.RenderFX)
	} else if bits&q2const.U_RENDERFX16 != 0 {
		msg.MSG_WriteShort(to.RenderFX)
	}

	if bits&q2const.U_ORIGIN1 != 0 {
		msg.MSG_WriteCoord(to.Origin[0])
	}
	if bits&q2const.U_ORIGIN2 != 0 {
		msg.MSG_WriteCoord(to.Origin[1])
	}
	if bits&q2const.U_ORIGIN3 != 0 {
		msg.MSG_WriteCoord(to.Origin[2])
	}

	if bits&q2const.U_ANGLE1 != 0 {
		msg.MSG_WriteAngle(to.Angles[0])
	}
	if bits&q2const.U_ANGLE2 != 0 {
		msg.MSG_WriteAngle(to.Angles[1])
	}
	if bits&q2const.U_ANGLE3 != 0 {
		msg.MSG_WriteAngle(to.Angles[2])
	}

	if bits&q2const.U_OLDORIGIN != 0 {
		msg.MSG_WriteCoord(to.OldOrigin[0])
		msg.MSG_WriteCoord(to.OldOrigin[1])
		msg.MSG_WriteCoord(to.OldOrigin[2])
	}

	if bits&q2const.U_SOUND != 0 {
		msg.MSG_WriteByte(to.Sound)
	}
	if bits&q2const.U_EVENT != 0 {
		msg.MSG_WriteByte(to.Event)
	}
	if bits&q2const.U_SOLID != 0 {
		msg.MSG_WriteShort(to.Solid)
	}
}

//============================================================

//
// reading functions
//

// MSG_BeginReading rewinds the read cursor.
// C: qcommon/common.c:675 MSG_BeginReading
func (msg *SizeBuf) MSG_BeginReading() {
	msg.ReadCount = 0
}

// MSG_ReadChar returns a signed byte, or -1 if no more characters are available.
// C: qcommon/common.c:681 MSG_ReadChar
func (m *SizeBuf) MSG_ReadChar() int32 {
	var c int32
	if m.ReadCount+1 > m.CurSize {
		c = -1
	} else {
		c = int32(int8(m.Data[m.ReadCount]))
	}
	m.ReadCount++
	return c
}

// MSG_ReadByte returns an unsigned byte, or -1 past the end.
// C: qcommon/common.c:694 MSG_ReadByte
func (m *SizeBuf) MSG_ReadByte() int32 {
	var c int32
	if m.ReadCount+1 > m.CurSize {
		c = -1
	} else {
		c = int32(m.Data[m.ReadCount])
	}
	m.ReadCount++
	return c
}

// MSG_ReadShort returns a signed little-endian short, or -1 past the end.
// C: qcommon/common.c:707 MSG_ReadShort
func (m *SizeBuf) MSG_ReadShort() int32 {
	var c int32
	if m.ReadCount+2 > m.CurSize {
		c = -1
	} else {
		c = int32(int16(uint16(m.Data[m.ReadCount]) | uint16(m.Data[m.ReadCount+1])<<8))
	}
	m.ReadCount += 2
	return c
}

// MSG_ReadLong returns a little-endian int, or -1 past the end.
// C: qcommon/common.c:722 MSG_ReadLong
func (m *SizeBuf) MSG_ReadLong() int32 {
	var c int32
	if m.ReadCount+4 > m.CurSize {
		c = -1
	} else {
		c = int32(uint32(m.Data[m.ReadCount]) | uint32(m.Data[m.ReadCount+1])<<8 |
			uint32(m.Data[m.ReadCount+2])<<16 | uint32(m.Data[m.ReadCount+3])<<24)
	}
	m.ReadCount += 4
	return c
}

// MSG_ReadFloat returns a little-endian IEEE float, or -1 past the end.
// C: qcommon/common.c:739 MSG_ReadFloat
func (m *SizeBuf) MSG_ReadFloat() float32 {
	var f float32
	if m.ReadCount+4 > m.CurSize {
		f = -1
	} else {
		f = math.Float32frombits(uint32(m.Data[m.ReadCount]) | uint32(m.Data[m.ReadCount+1])<<8 |
			uint32(m.Data[m.ReadCount+2])<<16 | uint32(m.Data[m.ReadCount+3])<<24)
	}
	m.ReadCount += 4
	return f
}

// MSG_ReadString reads up to 2047 chars until 0, end of data or a 0xff byte
// (signed char -1 is indistinguishable from end of data, as in C).
// C: qcommon/common.c:764 MSG_ReadString
func (m *SizeBuf) MSG_ReadString() string {
	var s [2048]byte
	l := 0
	for {
		c := m.MSG_ReadChar()
		if c == -1 || c == 0 {
			break
		}
		s[l] = byte(c)
		l++
		if l >= len(s)-1 {
			break
		}
	}
	return string(s[:l])
}

// MSG_ReadStringLine is ReadString that also stops at '\n'.
// C: qcommon/common.c:784 MSG_ReadStringLine
func (m *SizeBuf) MSG_ReadStringLine() string {
	var s [2048]byte
	l := 0
	for {
		c := m.MSG_ReadChar()
		if c == -1 || c == 0 || c == '\n' {
			break
		}
		s[l] = byte(c)
		l++
		if l >= len(s)-1 {
			break
		}
	}
	return string(s[:l])
}

// MSG_ReadCoord returns short * (1.0/8).
// C: qcommon/common.c:804 MSG_ReadCoord
func (m *SizeBuf) MSG_ReadCoord() float32 {
	return float32(float64(m.MSG_ReadShort()) * (1.0 / 8))
}

// MSG_ReadPos reads three coords.
// C: qcommon/common.c:809 MSG_ReadPos
func (m *SizeBuf) MSG_ReadPos() shared.Vec3 {
	var pos shared.Vec3
	pos[0] = float32(float64(m.MSG_ReadShort()) * (1.0 / 8))
	pos[1] = float32(float64(m.MSG_ReadShort()) * (1.0 / 8))
	pos[2] = float32(float64(m.MSG_ReadShort()) * (1.0 / 8))
	return pos
}

// MSG_ReadAngle returns char * (360.0/256).
// C: qcommon/common.c:816 MSG_ReadAngle
func (m *SizeBuf) MSG_ReadAngle() float32 {
	return float32(float64(m.MSG_ReadChar()) * (360.0 / 256))
}

// MSG_ReadAngle16 returns SHORT2ANGLE(short).
// C: qcommon/common.c:821 MSG_ReadAngle16
func (m *SizeBuf) MSG_ReadAngle16() float32 {
	return float32(shared.SHORT2ANGLE(m.MSG_ReadShort()))
}

// MSG_ReadDeltaUsercmd reads a usercmd delta-compressed against from.
// C: qcommon/common.c:826 MSG_ReadDeltaUsercmd
func (m *SizeBuf) MSG_ReadDeltaUsercmd(from *shared.UserCmd) shared.UserCmd {
	move := *from

	bits := m.MSG_ReadByte()

	// read current angles
	if bits&q2const.CM_ANGLE1 != 0 {
		move.Angles[0] = int16(m.MSG_ReadShort())
	}
	if bits&q2const.CM_ANGLE2 != 0 {
		move.Angles[1] = int16(m.MSG_ReadShort())
	}
	if bits&q2const.CM_ANGLE3 != 0 {
		move.Angles[2] = int16(m.MSG_ReadShort())
	}

	// read movement
	if bits&q2const.CM_FORWARD != 0 {
		move.ForwardMove = int16(m.MSG_ReadShort())
	}
	if bits&q2const.CM_SIDE != 0 {
		move.SideMove = int16(m.MSG_ReadShort())
	}
	if bits&q2const.CM_UP != 0 {
		move.UpMove = int16(m.MSG_ReadShort())
	}

	// read buttons
	if bits&q2const.CM_BUTTONS != 0 {
		move.Buttons = uint8(m.MSG_ReadByte())
	}
	if bits&q2const.CM_IMPULSE != 0 {
		move.Impulse = uint8(m.MSG_ReadByte())
	}

	// read time to run command
	move.Msec = uint8(m.MSG_ReadByte())

	// read the light level
	move.LightLevel = uint8(m.MSG_ReadByte())
	return move
}

// MSG_ReadData fills data byte by byte (past the end each byte becomes 0xff).
// C: qcommon/common.c:865 MSG_ReadData
func (m *SizeBuf) MSG_ReadData(data []byte) {
	for i := range data {
		data[i] = byte(m.MSG_ReadByte())
	}
}
