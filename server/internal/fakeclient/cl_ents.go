package fakeclient

import (
	"quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/msg"
	"quake2web/server/internal/qcommon/shared"
)

func absf(f float32) float32 {
	if f < 0 {
		return -f
	}
	return f
}

// C: client/cl_ents.c:327 CL_DeltaEntity
func (c *Client) deltaEntity(m *msg.SizeBuf, frame *Frame, newnum int32, old *shared.EntityState, bits uint32) {
	ent := &c.Entities[newnum]
	slot := c.parseEntities & (MAX_PARSE_ENTITIES - 1)
	c.parseEntities++
	frame.NumEntities++

	state := m.ParseDelta(old, newnum, bits)
	c.ParseEnts[slot] = state

	// some data changes will force no lerping
	// (C abs() takes an int: the float difference is truncated)
	if state.ModelIndex != ent.Current.ModelIndex ||
		state.ModelIndex2 != ent.Current.ModelIndex2 ||
		state.ModelIndex3 != ent.Current.ModelIndex3 ||
		state.ModelIndex4 != ent.Current.ModelIndex4 ||
		absf(float32(int32(state.Origin[0]-ent.Current.Origin[0]))) > 512 ||
		absf(float32(int32(state.Origin[1]-ent.Current.Origin[1]))) > 512 ||
		absf(float32(int32(state.Origin[2]-ent.Current.Origin[2]))) > 512 ||
		state.Event == q2const.EV_PLAYER_TELEPORT ||
		state.Event == q2const.EV_OTHER_TELEPORT {
		ent.ServerFrame = -99
	}

	if ent.ServerFrame != c.Frame.ServerFrame-1 {
		// wasn't in last update, so initialize some things
		// duplicate the current state so lerping doesn't hurt anything
		ent.Prev = state
		if state.Event == q2const.EV_OTHER_TELEPORT {
			ent.Prev.Origin = state.Origin
		} else {
			ent.Prev.Origin = state.OldOrigin
		}
	} else {
		// shuffle the last state to previous
		ent.Prev = ent.Current
	}
	ent.ServerFrame = c.Frame.ServerFrame
	ent.Current = state
}

// C: client/cl_ents.c:388 CL_ParsePacketEntities
func (c *Client) parsePacketEntities(m *msg.SizeBuf, oldframe, newframe *Frame) {
	newframe.ParseEntities = c.parseEntities
	newframe.NumEntities = 0

	// delta from the entities present in oldframe
	oldindex := 0
	var oldnum int32
	var oldstate shared.EntityState
	next := func() {
		if oldindex >= oldframe.NumEntities {
			oldnum = 99999
		} else {
			oldstate = c.ParseEnts[(oldframe.ParseEntities+oldindex)&(MAX_PARSE_ENTITIES-1)]
			oldnum = oldstate.Number
		}
	}
	if oldframe == nil {
		oldnum = 99999
	} else {
		next()
	}

	for {
		newnum, bits := m.ParseEntityBits()
		// newnum < 0 (a 16 bit number with the sign bit set) indexes
		// cl_entities out of bounds in C: memory-safety check
		if newnum >= q2const.MAX_EDICTS || newnum < 0 {
			shared.Error(q2const.ERR_DROP, "CL_ParsePacketEntities: bad number:%d", newnum)
		}
		if m.ReadCount > m.CurSize {
			shared.Error(q2const.ERR_DROP, "CL_ParsePacketEntities: end of message")
		}
		if newnum == 0 {
			break
		}

		for oldnum < newnum { // one or more entities from the old packet are unchanged
			c.deltaEntity(m, newframe, oldnum, &oldstate, 0)
			oldindex++
			next()
		}

		if bits&q2const.U_REMOVE != 0 { // the entity present in oldframe is not in the current frame
			if oldnum != newnum {
				c.printf("U_REMOVE: oldnum != newnum\n")
			}
			oldindex++
			if oldframe != nil {
				next()
			}
			continue
		}

		if oldnum == newnum { // delta from previous state
			c.deltaEntity(m, newframe, newnum, &oldstate, bits)
			oldindex++
			next()
			continue
		}

		if oldnum > newnum { // delta from baseline
			base := c.Entities[newnum].Baseline
			c.deltaEntity(m, newframe, newnum, &base, bits)
			continue
		}
	}

	// any remaining entities in the old frame are copied over
	for oldnum != 99999 {
		c.deltaEntity(m, newframe, oldnum, &oldstate, 0)
		oldindex++
		next()
	}
}

// C: client/cl_ents.c:663 CL_ParseFrame
func (c *Client) parseFrame(m *msg.SizeBuf) {
	c.Frame = Frame{}

	c.Frame.ServerFrame = m.MSG_ReadLong()
	c.Frame.DeltaFrame = m.MSG_ReadLong()
	c.Frame.ServerTime = c.Frame.ServerFrame * 100
	c.Frame.SurpressCount = m.MSG_ReadByte()

	// If the frame is delta compressed from data that we
	// no longer have available, we must suck up the rest of
	// the frame, but not use it, then ask for a non-compressed
	// message
	var old *Frame
	if c.Frame.DeltaFrame <= 0 {
		c.Frame.Valid = true  // uncompressed frame
		c.demoWaiting = false // we can start recording now
	} else {
		old = &c.Frames[c.Frame.DeltaFrame&q2const.UPDATE_MASK]
		if !old.Valid { // should never happen
			c.printf("Delta from invalid frame (not supposed to happen!).\n")
		}
		if old.ServerFrame != c.Frame.DeltaFrame {
			// The frame that the server did the delta from
			// is too old, so we can't reconstruct it properly.
			c.printf("Delta frame too old.\n")
		} else if c.parseEntities-old.ParseEntities > MAX_PARSE_ENTITIES-128 {
			c.printf("Delta parse_entities too old.\n")
		} else {
			c.Frame.Valid = true // valid delta parse
		}
	}

	// read areabits
	n := int(m.MSG_ReadByte())
	c.Frame.AreaBytes = n
	if n < 0 {
		n = 0
	}
	area := make([]byte, n)
	m.MSG_ReadData(area)
	copy(c.Frame.AreaBits[:], area) // memory safety: C overruns areabits for len > 32

	// read playerinfo
	if cmd := m.MSG_ReadByte(); cmd != q2const.Svc_playerinfo {
		shared.Error(q2const.ERR_DROP, "CL_ParseFrame: not playerinfo")
	}
	// C: client/cl_ents.c:515 CL_ParsePlayerstate
	var oldps *shared.PlayerState
	if old != nil {
		oldps = &old.PlayerState
	}
	c.Frame.PlayerState = m.ReadDeltaPlayerstate(oldps)
	if c.ServerData.AttractLoop != 0 {
		c.Frame.PlayerState.PMove.PmType = q2const.PM_FREEZE // demo playback
	}

	// read packet entities
	if cmd := m.MSG_ReadByte(); cmd != q2const.Svc_packetentities {
		shared.Error(q2const.ERR_DROP, "CL_ParseFrame: not packetentities")
	}
	c.parsePacketEntities(m, old, &c.Frame)

	// save the frame off in the backup array for later delta comparisons
	c.Frames[c.Frame.ServerFrame&q2const.UPDATE_MASK] = c.Frame
	c.FramesParsed++

	if c.Frame.Valid {
		c.ValidFrames++
		// getting a valid frame message ends the connection process
		if c.State != CaActive {
			c.State = CaActive
		}
	}
}
