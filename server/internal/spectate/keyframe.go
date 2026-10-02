package spectate

import (
	"fmt"

	"quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/msg"
	"quake2web/server/internal/qcommon/shared"
)

// maxKeyframe bounds the size of an encoded keyframe: the frame header,
// the largest playerstate and MAX_EDICTS entities with every field set.
const maxKeyframe = 64 << 10

// AppendKeyframe appends frame f encoded as an uncompressed svc_frame to dst:
// what SV_WriteFrameToClient sends a client that asks for a retransmit
// (lastframe -1). The serverframe number is f's own and the deltaframe -1,
// the playerstate is delta-coded from a zeroed state and every entity from
// its baseline in l (a zero state when the bot received none, like the
// client's). Because the msg quantizers are idempotent (a coordinate, angle,
// offset or blend read from the wire writes back the same bytes), a client
// parsing the keyframe ends up with exactly the bot's parsed frame, so the
// bot's next delta frame applies on it.
//
// The entities must have numbers in [1, MAX_EDICTS) in ascending order, as
// in every frame a client parses. An area bits length beyond MAX_MAP_AREAS/8
// (which C would have overrun on parse) is clamped.
func AppendKeyframe(dst []byte, f *FrameSnapshot, l *LevelSnapshot) ([]byte, error) {
	var buf msg.SizeBuf
	buf.SZ_Init(make([]byte, maxKeyframe))
	if err := writeKeyframe(&buf, f, l); err != nil {
		return dst, err
	}
	return append(dst, buf.Bytes()...), nil
}

// writeKeyframe writes the keyframe into m (see AppendKeyframe), which must
// hold maxKeyframe bytes.
// C: server/sv_ents.c:414 SV_WriteFrameToClient (oldframe == NULL)
func writeKeyframe(m *msg.SizeBuf, f *FrameSnapshot, l *LevelSnapshot) error {
	prev := int32(0)
	for i := range f.Entities {
		n := f.Entities[i].Number
		if n <= prev || n >= q2const.MAX_EDICTS {
			return fmt.Errorf("spectate: keyframe entity %d: number %d out of order or range", i, n)
		}
		prev = n
	}
	if len(f.Entities) > q2const.MAX_EDICTS {
		return fmt.Errorf("spectate: keyframe with %d entities", len(f.Entities))
	}

	m.MSG_WriteByte(q2const.Svc_frame)
	m.MSG_WriteLong(f.ServerFrame)
	m.MSG_WriteLong(-1) // what we are delta'ing from: nothing
	m.MSG_WriteByte(f.SurpressCount)

	// send over the areabits
	n := f.AreaBytes
	if n < 0 {
		n = 0
	}
	if n > len(f.AreaBits) {
		n = len(f.AreaBits)
	}
	m.MSG_WriteByte(int32(n))
	m.SZ_Write(f.AreaBits[:n])

	// delta encode the playerstate
	writeKeyPlayerstate(m, &f.PlayerState)

	// delta encode the entities: with no old frame every entity is new
	// and sent from its baseline
	// C: server/sv_ents.c:126 SV_EmitPacketEntities (from == NULL)
	m.MSG_WriteByte(q2const.Svc_packetentities)
	var null shared.EntityState
	for i := range f.Entities {
		e := &f.Entities[i]
		base := &null
		if l != nil {
			base = &l.base.State[e.Number]
		}
		m.MSG_WriteDeltaEntity(base, e, true, true)
	}
	m.MSG_WriteShort(0) // end of packetentities
	return nil
}

// writeKeyPlayerstate writes ps delta-coded from a zeroed state, like
// SV_WritePlayerstateToClient for a client without a delta frame, with one
// difference: PS_WEAPONFRAME also carries gunoffset and gunangles but is
// only sent when gunframe differs, so a client keeps stale offsets while
// the gunframe stays put. A bot holding gunframe 0 with such offsets would
// lose them on a keyframe; the flag is then forced (from a dummy gunframe),
// which writes exactly the bot's values.
// C: server/sv_ents.c:215 SV_WritePlayerstateToClient (from == NULL)
func writeKeyPlayerstate(m *msg.SizeBuf, ps *shared.PlayerState) {
	var from shared.PlayerState
	if ps.GunFrame == 0 && (ps.GunOffset != (shared.Vec3{}) || ps.GunAngles != (shared.Vec3{})) {
		from.GunFrame = 1
	}
	m.WriteDeltaPlayerstate(&from, ps)
}
