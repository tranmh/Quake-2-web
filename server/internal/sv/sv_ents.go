package sv

import (
	"encoding/binary"

	"quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/msg"
	"quake2web/server/internal/qcommon/shared"
)

/*
=============================================================================

Encode a client frame onto the network channel

=============================================================================
*/

// emitPacketEntities writes a delta update of an entity_state_t list to the
// message.
// C: server/sv_ents.c:126 SV_EmitPacketEntities
func (s *Server) emitPacketEntities(from, to *ClientFrame, m *msg.SizeBuf) {
	var oldent, newent *shared.EntityState

	m.MSG_WriteByte(q2const.Svc_packetentities)

	fromNumEntities := 0
	if from != nil {
		fromNumEntities = from.NumEntities
	}

	ce := s.SVS.ClientEntities
	nce := s.SVS.NumClientEntities
	newindex := 0
	oldindex := 0
	for newindex < to.NumEntities || oldindex < fromNumEntities {
		var newnum, oldnum int
		if newindex >= to.NumEntities {
			newnum = 9999
		} else {
			newent = &ce[(to.FirstEntity+newindex)%nce]
			newnum = int(newent.Number)
		}

		if oldindex >= fromNumEntities {
			oldnum = 9999
		} else {
			oldent = &ce[(from.FirstEntity+oldindex)%nce]
			oldnum = int(oldent.Number)
		}

		if newnum == oldnum {
			// delta update from old position
			// because the force parm is false, this will not result
			// in any bytes being emited if the entity has not changed at all
			// note that players are always 'newentities', this updates their oldorigin always
			// and prevents warping
			m.MSG_WriteDeltaEntity(oldent, newent, false, float32(newent.Number) <= s.maxclients.Value)
			oldindex++
			newindex++
			continue
		}

		if newnum < oldnum {
			// this is a new entity, send it from the baseline
			m.MSG_WriteDeltaEntity(&s.SV.Baselines[newnum], newent, true, true)
			newindex++
			continue
		}

		if newnum > oldnum {
			// the old entity isn't present in the new message
			bits := q2const.U_REMOVE
			if oldnum >= 256 {
				bits |= q2const.U_NUMBER16 | q2const.U_MOREBITS1
			}

			m.MSG_WriteByte(int32(bits & 255))
			if bits&0x0000ff00 != 0 {
				m.MSG_WriteByte(int32((bits >> 8) & 255))
			}

			if bits&q2const.U_NUMBER16 != 0 {
				m.MSG_WriteShort(int32(oldnum))
			} else {
				m.MSG_WriteByte(int32(oldnum))
			}

			oldindex++
			continue
		}
	}

	m.MSG_WriteShort(0) // end of packetentities
}

// writePlayerstateToClient. C: server/sv_ents.c:215 SV_WritePlayerstateToClient
func writePlayerstateToClient(from, to *ClientFrame, m *msg.SizeBuf) {
	if from == nil {
		m.WriteDeltaPlayerstate(nil, &to.PS)
	} else {
		m.WriteDeltaPlayerstate(&from.PS, &to.PS)
	}
}

// writeFrameToClient. C: server/sv_ents.c:414 SV_WriteFrameToClient
func (s *Server) writeFrameToClient(client *Client, m *msg.SizeBuf) {
	var oldframe *ClientFrame
	var lastframe int

	// this is the frame we are creating
	frame := &client.Frames[s.SV.FrameNum&q2const.UPDATE_MASK]

	if client.LastFrame <= 0 {
		// client is asking for a retransmit
		oldframe = nil
		lastframe = -1
	} else if s.SV.FrameNum-client.LastFrame >= (q2const.UPDATE_BACKUP - 3) {
		// client hasn't gotten a good message through in a long time
		oldframe = nil
		lastframe = -1
	} else {
		// we have a valid message to delta from
		oldframe = &client.Frames[client.LastFrame&q2const.UPDATE_MASK]
		lastframe = client.LastFrame
	}

	m.MSG_WriteByte(q2const.Svc_frame)
	m.MSG_WriteLong(int32(s.SV.FrameNum))
	m.MSG_WriteLong(int32(lastframe))            // what we are delta'ing from
	m.MSG_WriteByte(int32(client.SurpressCount)) // rate dropped packets
	client.SurpressCount = 0

	// send over the areabits
	m.MSG_WriteByte(int32(frame.AreaBytes))
	m.SZ_Write(frame.AreaBits[:frame.AreaBytes])

	// delta encode the playerstate
	writePlayerstateToClient(oldframe, frame, m)

	// delta encode the entities
	s.emitPacketEntities(oldframe, frame, m)
}

/*
=============================================================================

Build a client frame structure

=============================================================================
*/

// fatPVS: the client will interpolate the view position, so we can't use a
// single PVS point.
// C: server/sv_ents.c:478 SV_FatPVS
func (s *Server) fatPVS(org Vec3) {
	var leafs [64]int32
	var mins, maxs Vec3

	for i := 0; i < 3; i++ {
		mins[i] = org[i] - 8
		maxs[i] = org[i] + 8
	}

	cm := s.CM
	m := cm.Map()
	count := cm.BoxLeafnums(mins, maxs, leafs[:], nil)
	if count < 1 {
		shared.Error(q2const.ERR_FATAL, "SV_FatPVS: count < 1")
	}
	longs := (m.NumClusters() + 31) >> 5

	// convert leafs to clusters
	for i := 0; i < count; i++ {
		leafs[i] = m.LeafCluster(int(leafs[i]))
	}

	copy(s.fatpvs[:longs<<2], cm.ClusterPVS(int(leafs[0])))
	// or in all the other leaf bits
	for i := 1; i < count; i++ {
		j := 0
		for j = 0; j < i; j++ {
			if leafs[i] == leafs[j] {
				break
			}
		}
		if j != i {
			continue // already have the cluster we want
		}
		src := cm.ClusterPVS(int(leafs[i]))
		// ((int *)fatpvs)[j] |= ((int *)src)[j] (LP64 patch: int, not long)
		for j = 0; j < longs; j++ {
			v := binary.LittleEndian.Uint32(s.fatpvs[j*4:]) | binary.LittleEndian.Uint32(src[j*4:])
			binary.LittleEndian.PutUint32(s.fatpvs[j*4:], v)
		}
	}
}

// buildClientFrame decides which entities are going to be visible to the
// client, and copies off the playerstat and areabits.
// C: server/sv_ents.c:527 SV_BuildClientFrame
func (s *Server) buildClientFrame(client *Client) {
	clent := client.Edict
	if clent.Client == nil {
		return // not in game yet
	}

	cm := s.CM
	m := cm.Map()

	// this is the frame we are creating
	frame := &client.Frames[s.SV.FrameNum&q2const.UPDATE_MASK]

	frame.SentTime = s.SVS.RealTime // save it for ping calc later

	// find the client's PVS
	var org Vec3
	for i := 0; i < 3; i++ {
		org[i] = float32(float64(clent.Client.PS.PMove.Origin[i])*0.125 + float64(clent.Client.PS.ViewOffset[i]))
	}

	leafnum := cm.PointLeafnum(org)
	clientarea := m.LeafArea(int(leafnum))
	clientcluster := m.LeafCluster(int(leafnum))

	// calculate the visible areas
	frame.AreaBytes = cm.WriteAreaBits(frame.AreaBits[:], int(clientarea))

	// grab the current player_state_t
	frame.PS = clent.Client.PS

	s.fatPVS(org)
	clientphs := cm.ClusterPHS(int(clientcluster))

	// build up the list of visible entities
	frame.NumEntities = 0
	frame.FirstEntity = s.SVS.NextClientEntities

	n := s.ge.NumEdicts()
	for e := 1; e < n; e++ {
		ent := s.edictNum(e)

		// ignore ents without visible models
		if ent.SVFlags&q2const.SVF_NOCLIENT != 0 {
			continue
		}

		// ignore ents without visible models unless they have an effect
		if ent.S.ModelIndex == 0 && ent.S.Effects == 0 && ent.S.Sound == 0 && ent.S.Event == 0 {
			continue
		}

		// ignore if not touching a PV leaf
		if ent != clent {
			// check area
			if !cm.AreasConnected(int(clientarea), int(ent.AreaNum)) {
				// doors can legally straddle two areas, so
				// we may need to check another one
				if ent.AreaNum2 == 0 || !cm.AreasConnected(int(clientarea), int(ent.AreaNum2)) {
					continue // blocked by a door
				}
			}

			// beams just check one point for PHS
			if ent.S.RenderFX&q2const.RF_BEAM != 0 {
				l := ent.ClusterNums[0]
				if !maskBit(clientphs, l) {
					continue
				}
			} else {
				// FIXME: if an ent has a model and a sound, but isn't
				// in the PVS, only the PHS, clear the model
				bitvector := s.fatpvs[:]

				if ent.NumClusters == -1 {
					// too many leafs for individual check, go by headnode
					if !cm.HeadnodeVisible(ent.HeadNode, bitvector) {
						continue
					}
				} else {
					// check individual leafs
					i := int32(0)
					for i = 0; i < ent.NumClusters; i++ {
						l := ent.ClusterNums[i]
						if maskBit(bitvector, l) {
							break
						}
					}
					if i == ent.NumClusters {
						continue // not visible
					}
				}

				if ent.S.ModelIndex == 0 {
					// don't send sounds if they will be attenuated away
					delta := shared.VectorSubtract(org, ent.S.Origin)
					l := shared.VectorLength(delta)
					if l > 400 {
						continue
					}
				}
			}
		}

		// add it to the circular client_entities array
		state := &s.SVS.ClientEntities[s.SVS.NextClientEntities%s.SVS.NumClientEntities]
		if ent.S.Number != int32(e) {
			s.DPrintf("FIXING ENT->S.NUMBER!!!\n")
			ent.S.Number = int32(e)
		}
		*state = ent.S

		// don't mark players missiles as solid
		if ent.Owner == client.Edict {
			state.Solid = 0
		}

		s.SVS.NextClientEntities++
		frame.NumEntities++
	}
}

// recordDemoMessage saves everything in the world out without deltas. Used
// for recording footage for merged or assembled demos.
// C: server/sv_ents.c:677 SV_RecordDemoMessage
func (s *Server) recordDemoMessage() {
	if s.SVS.DemoFile == nil {
		return
	}

	var nostate shared.EntityState
	buf := msg.NewSizeBuf(32768)

	// write a frame message that doesn't contain a player_state_t
	buf.MSG_WriteByte(q2const.Svc_frame)
	buf.MSG_WriteLong(int32(s.SV.FrameNum))

	buf.MSG_WriteByte(q2const.Svc_packetentities)

	n := s.ge.NumEdicts()
	for e := 1; e < n; e++ {
		ent := s.edictNum(e)
		// ignore ents without visible models unless they have an effect
		if ent.InUse &&
			ent.S.Number != 0 &&
			(ent.S.ModelIndex != 0 || ent.S.Effects != 0 || ent.S.Sound != 0 || ent.S.Event != 0) &&
			ent.SVFlags&q2const.SVF_NOCLIENT == 0 {
			buf.MSG_WriteDeltaEntity(&nostate, &ent.S, false, true)
		}
	}

	buf.MSG_WriteShort(0) // end of packetentities

	// now add the accumulated multicast information
	buf.SZ_Write(s.SVS.DemoMulticast.Bytes())
	s.SVS.DemoMulticast.SZ_Clear()

	// now write the entire message to the file, prefixed by the length
	s.writeDemoBlock(buf.Bytes())
}

func (s *Server) writeDemoBlock(b []byte) {
	var l [4]byte
	binary.LittleEndian.PutUint32(l[:], uint32(len(b)))
	if _, err := s.SVS.DemoFile.Write(l[:]); err != nil {
		s.Printf("demo write failed: %v\n", err)
		return
	}
	_, _ = s.SVS.DemoFile.Write(b)
}
