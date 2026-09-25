package msg

import (
	"quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/shared"
)

// WriteDeltaPlayerstate is the body of SV_WritePlayerstateToClient: it
// writes svc_playerinfo and ps delta-compressed against ops (nil = zeroed
// dummy, as when the client has no valid delta frame).
// C: server/sv_ents.c:220 SV_WritePlayerstateToClient
func (msg *SizeBuf) WriteDeltaPlayerstate(from, to *shared.PlayerState) {
	var dummy shared.PlayerState
	ps := to
	ops := from
	if ops == nil {
		ops = &dummy
	}

	//
	// determine what needs to be sent
	//
	var pflags int32

	if ps.PMove.PmType != ops.PMove.PmType {
		pflags |= q2const.PS_M_TYPE
	}
	if ps.PMove.Origin != ops.PMove.Origin {
		pflags |= q2const.PS_M_ORIGIN
	}
	if ps.PMove.Velocity != ops.PMove.Velocity {
		pflags |= q2const.PS_M_VELOCITY
	}
	if ps.PMove.PmTime != ops.PMove.PmTime {
		pflags |= q2const.PS_M_TIME
	}
	if ps.PMove.PmFlags != ops.PMove.PmFlags {
		pflags |= q2const.PS_M_FLAGS
	}
	if ps.PMove.Gravity != ops.PMove.Gravity {
		pflags |= q2const.PS_M_GRAVITY
	}
	if ps.PMove.DeltaAngles != ops.PMove.DeltaAngles {
		pflags |= q2const.PS_M_DELTA_ANGLES
	}

	// float compares use C != semantics (NaN != NaN, -0 == 0)
	if vecNE(ps.ViewOffset, ops.ViewOffset) {
		pflags |= q2const.PS_VIEWOFFSET
	}
	if vecNE(ps.ViewAngles, ops.ViewAngles) {
		pflags |= q2const.PS_VIEWANGLES
	}
	if vecNE(ps.KickAngles, ops.KickAngles) {
		pflags |= q2const.PS_KICKANGLES
	}
	if ps.Blend[0] != ops.Blend[0] || ps.Blend[1] != ops.Blend[1] ||
		ps.Blend[2] != ops.Blend[2] || ps.Blend[3] != ops.Blend[3] {
		pflags |= q2const.PS_BLEND
	}
	if ps.Fov != ops.Fov {
		pflags |= q2const.PS_FOV
	}
	if ps.RDFlags != ops.RDFlags {
		pflags |= q2const.PS_RDFLAGS
	}
	if ps.GunFrame != ops.GunFrame {
		pflags |= q2const.PS_WEAPONFRAME
	}

	pflags |= q2const.PS_WEAPONINDEX

	//
	// write it
	//
	msg.MSG_WriteByte(q2const.Svc_playerinfo)
	msg.MSG_WriteShort(pflags)

	//
	// write the pmove_state_t
	//
	if pflags&q2const.PS_M_TYPE != 0 {
		msg.MSG_WriteByte(ps.PMove.PmType)
	}
	if pflags&q2const.PS_M_ORIGIN != 0 {
		msg.MSG_WriteShort(int32(ps.PMove.Origin[0]))
		msg.MSG_WriteShort(int32(ps.PMove.Origin[1]))
		msg.MSG_WriteShort(int32(ps.PMove.Origin[2]))
	}
	if pflags&q2const.PS_M_VELOCITY != 0 {
		msg.MSG_WriteShort(int32(ps.PMove.Velocity[0]))
		msg.MSG_WriteShort(int32(ps.PMove.Velocity[1]))
		msg.MSG_WriteShort(int32(ps.PMove.Velocity[2]))
	}
	if pflags&q2const.PS_M_TIME != 0 {
		msg.MSG_WriteByte(int32(ps.PMove.PmTime))
	}
	if pflags&q2const.PS_M_FLAGS != 0 {
		msg.MSG_WriteByte(int32(ps.PMove.PmFlags))
	}
	if pflags&q2const.PS_M_GRAVITY != 0 {
		msg.MSG_WriteShort(int32(ps.PMove.Gravity))
	}
	if pflags&q2const.PS_M_DELTA_ANGLES != 0 {
		msg.MSG_WriteShort(int32(ps.PMove.DeltaAngles[0]))
		msg.MSG_WriteShort(int32(ps.PMove.DeltaAngles[1]))
		msg.MSG_WriteShort(int32(ps.PMove.DeltaAngles[2]))
	}

	//
	// write the rest of the player_state_t
	//
	if pflags&q2const.PS_VIEWOFFSET != 0 {
		msg.MSG_WriteChar(int32(ps.ViewOffset[0] * 4))
		msg.MSG_WriteChar(int32(ps.ViewOffset[1] * 4))
		msg.MSG_WriteChar(int32(ps.ViewOffset[2] * 4))
	}
	if pflags&q2const.PS_VIEWANGLES != 0 {
		msg.MSG_WriteAngle16(ps.ViewAngles[0])
		msg.MSG_WriteAngle16(ps.ViewAngles[1])
		msg.MSG_WriteAngle16(ps.ViewAngles[2])
	}
	if pflags&q2const.PS_KICKANGLES != 0 {
		msg.MSG_WriteChar(int32(ps.KickAngles[0] * 4))
		msg.MSG_WriteChar(int32(ps.KickAngles[1] * 4))
		msg.MSG_WriteChar(int32(ps.KickAngles[2] * 4))
	}
	if pflags&q2const.PS_WEAPONINDEX != 0 {
		msg.MSG_WriteByte(ps.GunIndex)
	}
	if pflags&q2const.PS_WEAPONFRAME != 0 {
		msg.MSG_WriteByte(ps.GunFrame)
		msg.MSG_WriteChar(int32(ps.GunOffset[0] * 4))
		msg.MSG_WriteChar(int32(ps.GunOffset[1] * 4))
		msg.MSG_WriteChar(int32(ps.GunOffset[2] * 4))
		msg.MSG_WriteChar(int32(ps.GunAngles[0] * 4))
		msg.MSG_WriteChar(int32(ps.GunAngles[1] * 4))
		msg.MSG_WriteChar(int32(ps.GunAngles[2] * 4))
	}
	if pflags&q2const.PS_BLEND != 0 {
		msg.MSG_WriteByte(int32(ps.Blend[0] * 255))
		msg.MSG_WriteByte(int32(ps.Blend[1] * 255))
		msg.MSG_WriteByte(int32(ps.Blend[2] * 255))
		msg.MSG_WriteByte(int32(ps.Blend[3] * 255))
	}
	if pflags&q2const.PS_FOV != 0 {
		msg.MSG_WriteByte(int32(ps.Fov))
	}
	if pflags&q2const.PS_RDFLAGS != 0 {
		msg.MSG_WriteByte(ps.RDFlags)
	}

	// send stats
	var statbits uint32
	for i := 0; i < q2const.MAX_STATS; i++ {
		if ps.Stats[i] != ops.Stats[i] {
			statbits |= 1 << i
		}
	}
	msg.MSG_WriteLong(int32(statbits))
	for i := 0; i < q2const.MAX_STATS; i++ {
		if statbits&(1<<i) != 0 {
			msg.MSG_WriteShort(int32(ps.Stats[i]))
		}
	}
}

func vecNE(a, b shared.Vec3) bool {
	return a[0] != b[0] || a[1] != b[1] || a[2] != b[2]
}
