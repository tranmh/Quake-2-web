package msg

import (
	"quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/shared"
)

// ParseEntityBits returns the entity number and the header bits.
// C: client/cl_ents.c:202 CL_ParseEntityBits
func (m *SizeBuf) ParseEntityBits() (number int32, bits uint32) {
	total := uint32(m.MSG_ReadByte())
	if total&q2const.U_MOREBITS1 != 0 {
		b := uint32(m.MSG_ReadByte())
		total |= b << 8
	}
	if total&q2const.U_MOREBITS2 != 0 {
		b := uint32(m.MSG_ReadByte())
		total |= b << 16
	}
	if total&q2const.U_MOREBITS3 != 0 {
		b := uint32(m.MSG_ReadByte())
		total |= b << 24
	}

	if total&q2const.U_NUMBER16 != 0 {
		number = m.MSG_ReadShort()
	} else {
		number = m.MSG_ReadByte()
	}
	return number, total
}

// ParseDelta reads an entity delta; can go from either a baseline or a
// previous packet_entity.
// C: client/cl_ents.c:247 CL_ParseDelta
func (m *SizeBuf) ParseDelta(from *shared.EntityState, number int32, bits uint32) shared.EntityState {
	// set everything to the state we are delta'ing from
	to := *from

	to.OldOrigin = from.Origin
	to.Number = number

	if bits&q2const.U_MODEL != 0 {
		to.ModelIndex = m.MSG_ReadByte()
	}
	if bits&q2const.U_MODEL2 != 0 {
		to.ModelIndex2 = m.MSG_ReadByte()
	}
	if bits&q2const.U_MODEL3 != 0 {
		to.ModelIndex3 = m.MSG_ReadByte()
	}
	if bits&q2const.U_MODEL4 != 0 {
		to.ModelIndex4 = m.MSG_ReadByte()
	}

	if bits&q2const.U_FRAME8 != 0 {
		to.Frame = m.MSG_ReadByte()
	}
	if bits&q2const.U_FRAME16 != 0 {
		to.Frame = m.MSG_ReadShort()
	}

	if bits&q2const.U_SKIN8 != 0 && bits&q2const.U_SKIN16 != 0 { //used for laser colors
		to.SkinNum = m.MSG_ReadLong()
	} else if bits&q2const.U_SKIN8 != 0 {
		to.SkinNum = m.MSG_ReadByte()
	} else if bits&q2const.U_SKIN16 != 0 {
		to.SkinNum = m.MSG_ReadShort()
	}

	if bits&(q2const.U_EFFECTS8|q2const.U_EFFECTS16) == q2const.U_EFFECTS8|q2const.U_EFFECTS16 {
		to.Effects = uint32(m.MSG_ReadLong())
	} else if bits&q2const.U_EFFECTS8 != 0 {
		to.Effects = uint32(m.MSG_ReadByte())
	} else if bits&q2const.U_EFFECTS16 != 0 {
		to.Effects = uint32(m.MSG_ReadShort()) // sign-extended, as in C
	}

	if bits&(q2const.U_RENDERFX8|q2const.U_RENDERFX16) == q2const.U_RENDERFX8|q2const.U_RENDERFX16 {
		to.RenderFX = m.MSG_ReadLong()
	} else if bits&q2const.U_RENDERFX8 != 0 {
		to.RenderFX = m.MSG_ReadByte()
	} else if bits&q2const.U_RENDERFX16 != 0 {
		to.RenderFX = m.MSG_ReadShort()
	}

	if bits&q2const.U_ORIGIN1 != 0 {
		to.Origin[0] = m.MSG_ReadCoord()
	}
	if bits&q2const.U_ORIGIN2 != 0 {
		to.Origin[1] = m.MSG_ReadCoord()
	}
	if bits&q2const.U_ORIGIN3 != 0 {
		to.Origin[2] = m.MSG_ReadCoord()
	}

	if bits&q2const.U_ANGLE1 != 0 {
		to.Angles[0] = m.MSG_ReadAngle()
	}
	if bits&q2const.U_ANGLE2 != 0 {
		to.Angles[1] = m.MSG_ReadAngle()
	}
	if bits&q2const.U_ANGLE3 != 0 {
		to.Angles[2] = m.MSG_ReadAngle()
	}

	if bits&q2const.U_OLDORIGIN != 0 {
		to.OldOrigin = m.MSG_ReadPos()
	}

	if bits&q2const.U_SOUND != 0 {
		to.Sound = m.MSG_ReadByte()
	}

	if bits&q2const.U_EVENT != 0 {
		to.Event = m.MSG_ReadByte()
	} else {
		to.Event = 0
	}

	if bits&q2const.U_SOLID != 0 {
		to.Solid = m.MSG_ReadShort()
	}
	return to
}

// ReadDeltaEntity is CL_ParseEntityBits followed by CL_ParseDelta.
func (m *SizeBuf) ReadDeltaEntity(from *shared.EntityState) (to shared.EntityState, number int32, bits uint32) {
	number, bits = m.ParseEntityBits()
	return m.ParseDelta(from, number, bits), number, bits
}

// ReadDeltaPlayerstate reads a player state delta (after the svc_playerinfo
// byte) against from (nil = zeroed). The demo-playback PM_FREEZE override
// (cl.attractloop) is left to the caller.
// C: client/cl_ents.c:515 CL_ParsePlayerstate
func (m *SizeBuf) ReadDeltaPlayerstate(from *shared.PlayerState) shared.PlayerState {
	var state shared.PlayerState

	// clear to old value before delta parsing
	if from != nil {
		state = *from
	}

	flags := m.MSG_ReadShort()

	//
	// parse the pmove_state_t
	//
	if flags&q2const.PS_M_TYPE != 0 {
		state.PMove.PmType = m.MSG_ReadByte()
	}
	if flags&q2const.PS_M_ORIGIN != 0 {
		state.PMove.Origin[0] = int16(m.MSG_ReadShort())
		state.PMove.Origin[1] = int16(m.MSG_ReadShort())
		state.PMove.Origin[2] = int16(m.MSG_ReadShort())
	}
	if flags&q2const.PS_M_VELOCITY != 0 {
		state.PMove.Velocity[0] = int16(m.MSG_ReadShort())
		state.PMove.Velocity[1] = int16(m.MSG_ReadShort())
		state.PMove.Velocity[2] = int16(m.MSG_ReadShort())
	}
	if flags&q2const.PS_M_TIME != 0 {
		state.PMove.PmTime = uint8(m.MSG_ReadByte())
	}
	if flags&q2const.PS_M_FLAGS != 0 {
		state.PMove.PmFlags = uint8(m.MSG_ReadByte())
	}
	if flags&q2const.PS_M_GRAVITY != 0 {
		state.PMove.Gravity = int16(m.MSG_ReadShort())
	}
	if flags&q2const.PS_M_DELTA_ANGLES != 0 {
		state.PMove.DeltaAngles[0] = int16(m.MSG_ReadShort())
		state.PMove.DeltaAngles[1] = int16(m.MSG_ReadShort())
		state.PMove.DeltaAngles[2] = int16(m.MSG_ReadShort())
	}

	//
	// parse the rest of the player_state_t
	//
	if flags&q2const.PS_VIEWOFFSET != 0 {
		state.ViewOffset[0] = float32(float64(m.MSG_ReadChar()) * 0.25)
		state.ViewOffset[1] = float32(float64(m.MSG_ReadChar()) * 0.25)
		state.ViewOffset[2] = float32(float64(m.MSG_ReadChar()) * 0.25)
	}
	if flags&q2const.PS_VIEWANGLES != 0 {
		state.ViewAngles[0] = m.MSG_ReadAngle16()
		state.ViewAngles[1] = m.MSG_ReadAngle16()
		state.ViewAngles[2] = m.MSG_ReadAngle16()
	}
	if flags&q2const.PS_KICKANGLES != 0 {
		state.KickAngles[0] = float32(float64(m.MSG_ReadChar()) * 0.25)
		state.KickAngles[1] = float32(float64(m.MSG_ReadChar()) * 0.25)
		state.KickAngles[2] = float32(float64(m.MSG_ReadChar()) * 0.25)
	}
	if flags&q2const.PS_WEAPONINDEX != 0 {
		state.GunIndex = m.MSG_ReadByte()
	}
	if flags&q2const.PS_WEAPONFRAME != 0 {
		state.GunFrame = m.MSG_ReadByte()
		state.GunOffset[0] = float32(float64(m.MSG_ReadChar()) * 0.25)
		state.GunOffset[1] = float32(float64(m.MSG_ReadChar()) * 0.25)
		state.GunOffset[2] = float32(float64(m.MSG_ReadChar()) * 0.25)
		state.GunAngles[0] = float32(float64(m.MSG_ReadChar()) * 0.25)
		state.GunAngles[1] = float32(float64(m.MSG_ReadChar()) * 0.25)
		state.GunAngles[2] = float32(float64(m.MSG_ReadChar()) * 0.25)
	}
	if flags&q2const.PS_BLEND != 0 {
		state.Blend[0] = float32(float64(m.MSG_ReadByte()) / 255.0)
		state.Blend[1] = float32(float64(m.MSG_ReadByte()) / 255.0)
		state.Blend[2] = float32(float64(m.MSG_ReadByte()) / 255.0)
		state.Blend[3] = float32(float64(m.MSG_ReadByte()) / 255.0)
	}
	if flags&q2const.PS_FOV != 0 {
		state.Fov = float32(m.MSG_ReadByte())
	}
	if flags&q2const.PS_RDFLAGS != 0 {
		state.RDFlags = m.MSG_ReadByte()
	}

	// parse stats
	statbits := uint32(m.MSG_ReadLong())
	for i := 0; i < q2const.MAX_STATS; i++ {
		if statbits&(1<<i) != 0 {
			state.Stats[i] = int16(m.MSG_ReadShort())
		}
	}
	return state
}
