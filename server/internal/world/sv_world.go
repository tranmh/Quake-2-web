// Package world ports server/sv_world.c: the area-node tree used to find the
// entities touching a box, and the world + entity traces built on cmodel.
package world

import (
	"math"

	"quake2web/server/internal/cmodel"
	"quake2web/server/internal/game"
	"quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/shared"
)

// C: server/sv_world.c:49
const (
	AREA_DEPTH = 4
	AREA_NODES = 32
)

// MAX_TOTAL_ENT_LEAFS bounds the leafs gathered by SV_LinkEdict.
// C: server/sv_world.c:164
const MAX_TOTAL_ENT_LEAFS = 128

// areaNode is C areanode_t. Children are indices into World.nodes (-1 = NULL).
// C: server/sv_world.c:40 areanode_t
type areaNode struct {
	axis          int // -1 = leaf node
	dist          float32
	children      [2]int
	triggerEdicts game.Link
	solidEdicts   game.Link
}

// World is the per-instance state of sv_world.c plus the server state it reads
// (sv.models, ge->edicts, sv.state).
type World struct {
	CM *cmodel.State
	// Models is sv.models (index 1 is the world model, "*N" inline models follow).
	Models *[q2const.MAX_MODELS]*shared.CModel
	// WorldEdict returns ge->edicts (edict 0).
	WorldEdict func() *game.Edict
	// Loading reports sv.state == ss_loading.
	Loading func() bool
	// Printf / DPrintf receive Com_Printf / Com_DPrintf output (may be nil).
	Printf, DPrintf func(format string, args ...any)

	nodes    [AREA_NODES]areaNode
	numNodes int

	// SV_AreaEdicts parameters (C file statics area_mins ... area_type)
	areaMins, areaMaxs Vec3
	areaList           []*game.Edict
	areaCount          int
	areaType           int

	touchlist [q2const.MAX_EDICTS]*game.Edict // SV_ClipMoveToEntities touchlist
	pctouch   [q2const.MAX_EDICTS]*game.Edict // SV_PointContents touch
}

// Vec3 is vec3_t.
type Vec3 = shared.Vec3

// ClearLink is used for new headnodes.
// C: server/sv_world.c:64 ClearLink
func ClearLink(l *game.Link) {
	l.Prev, l.Next = l, l
}

// RemoveLink unlinks l.
// C: server/sv_world.c:69 RemoveLink
func RemoveLink(l *game.Link) {
	l.Next.Prev = l.Prev
	l.Prev.Next = l.Next
}

// InsertLinkBefore links l before before.
// C: server/sv_world.c:75 InsertLinkBefore
func InsertLinkBefore(l, before *game.Link) {
	l.Next = before
	l.Prev = before.Prev
	l.Prev.Next = l
	l.Next.Prev = l
}

// createAreaNode builds a uniformly subdivided tree for the given world size.
// C: server/sv_world.c:90 SV_CreateAreaNode
func (w *World) createAreaNode(depth int, mins, maxs Vec3) int {
	idx := w.numNodes
	anode := &w.nodes[idx]
	w.numNodes++

	ClearLink(&anode.triggerEdicts)
	ClearLink(&anode.solidEdicts)

	if depth == AREA_DEPTH {
		anode.axis = -1
		anode.children[0], anode.children[1] = -1, -1
		return idx
	}

	var size Vec3
	for i := 0; i < 3; i++ {
		size[i] = maxs[i] - mins[i]
	}
	if size[0] > size[1] {
		anode.axis = 0
	} else {
		anode.axis = 1
	}

	anode.dist = float32(0.5 * float64(maxs[anode.axis]+mins[anode.axis]))
	mins1, mins2 := mins, mins
	maxs1, maxs2 := maxs, maxs

	maxs1[anode.axis] = anode.dist
	mins2[anode.axis] = anode.dist

	c0 := w.createAreaNode(depth+1, mins2, maxs2)
	c1 := w.createAreaNode(depth+1, mins1, maxs1)
	anode = &w.nodes[idx]
	anode.children[0] = c0
	anode.children[1] = c1
	return idx
}

// ClearWorld rebuilds the area nodes from the world model bounds.
// C: server/sv_world.c:135 SV_ClearWorld
func (w *World) ClearWorld() {
	w.nodes = [AREA_NODES]areaNode{}
	w.numNodes = 0
	w.createAreaNode(0, w.Models[1].Mins, w.Models[1].Maxs)
}

// UnlinkEdict removes ent from the area nodes.
// C: server/sv_world.c:149 SV_UnlinkEdict
func (w *World) UnlinkEdict(ent *game.Edict) {
	if ent.Area.Prev == nil {
		return // not linked in anywhere
	}
	RemoveLink(&ent.Area)
	ent.Area.Prev, ent.Area.Next = nil, nil
}

// LinkEdict computes the size, abs box, clusters and areas of ent and links it
// into the area node it crosses.
// C: server/sv_world.c:165 SV_LinkEdict
func (w *World) LinkEdict(ent *game.Edict) {
	var leafs [MAX_TOTAL_ENT_LEAFS]int32
	var clusters [MAX_TOTAL_ENT_LEAFS]int32
	var topnode int32

	if ent.Area.Prev != nil {
		w.UnlinkEdict(ent) // unlink from old position
	}

	if ent == w.WorldEdict() {
		return // don't add the world
	}

	if !ent.InUse {
		return
	}

	// set the size
	for i := 0; i < 3; i++ {
		ent.Size[i] = ent.Maxs[i] - ent.Mins[i]
	}

	// encode the size into the entity_state for client prediction
	if ent.Solid == q2const.SOLID_BBOX && ent.SVFlags&q2const.SVF_DEADMONSTER == 0 {
		// assume that x/y are equal and symetric
		i := int32(ent.Maxs[0] / 8)
		if i < 1 {
			i = 1
		}
		if i > 31 {
			i = 31
		}

		// z is not symetric
		j := int32((-ent.Mins[2]) / 8)
		if j < 1 {
			j = 1
		}
		if j > 31 {
			j = 31
		}

		// and z maxs can be negative...
		k := int32((ent.Maxs[2] + 32) / 8)
		if k < 1 {
			k = 1
		}
		if k > 63 {
			k = 63
		}

		ent.S.Solid = (k << 10) | (j << 5) | i
	} else if ent.Solid == q2const.SOLID_BSP {
		ent.S.Solid = 31 // a solid_bbox will never create this value
	} else {
		ent.S.Solid = 0
	}

	// set the abs box
	if ent.Solid == q2const.SOLID_BSP &&
		(ent.S.Angles[0] != 0 || ent.S.Angles[1] != 0 || ent.S.Angles[2] != 0) {
		// expand for rotation
		var max float32
		for i := 0; i < 3; i++ {
			v := float32(math.Abs(float64(ent.Mins[i])))
			if v > max {
				max = v
			}
			v = float32(math.Abs(float64(ent.Maxs[i])))
			if v > max {
				max = v
			}
		}
		for i := 0; i < 3; i++ {
			ent.AbsMin[i] = ent.S.Origin[i] - max
			ent.AbsMax[i] = ent.S.Origin[i] + max
		}
	} else {
		// normal
		for i := 0; i < 3; i++ {
			ent.AbsMin[i] = ent.S.Origin[i] + ent.Mins[i]
			ent.AbsMax[i] = ent.S.Origin[i] + ent.Maxs[i]
		}
	}

	// because movement is clipped an epsilon away from an actual edge,
	// we must fully check even when bounding boxes don't quite touch
	ent.AbsMin[0] -= 1
	ent.AbsMin[1] -= 1
	ent.AbsMin[2] -= 1
	ent.AbsMax[0] += 1
	ent.AbsMax[1] += 1
	ent.AbsMax[2] += 1

	// link to PVS leafs
	ent.NumClusters = 0
	ent.AreaNum = 0
	ent.AreaNum2 = 0

	// get all leafs, including solids
	numLeafs := w.CM.BoxLeafnums(ent.AbsMin, ent.AbsMax, leafs[:], &topnode)

	m := w.CM.Map()
	// set areas
	for i := 0; i < numLeafs; i++ {
		clusters[i] = m.LeafCluster(int(leafs[i]))
		area := m.LeafArea(int(leafs[i]))
		if area != 0 {
			// doors may legally straggle two areas,
			// but nothing should evern need more than that
			if ent.AreaNum != 0 && ent.AreaNum != area {
				if ent.AreaNum2 != 0 && ent.AreaNum2 != area && w.Loading != nil && w.Loading() {
					w.dprintf("Object touching 3 areas at %f %f %f\n",
						ent.AbsMin[0], ent.AbsMin[1], ent.AbsMin[2])
				}
				ent.AreaNum2 = area
			} else {
				ent.AreaNum = area
			}
		}
	}

	if numLeafs >= MAX_TOTAL_ENT_LEAFS {
		// assume we missed some leafs, and mark by headnode
		ent.NumClusters = -1
		ent.HeadNode = topnode
	} else {
		ent.NumClusters = 0
		for i := 0; i < numLeafs; i++ {
			if clusters[i] == -1 {
				continue // not a visible leaf
			}
			j := 0
			for j = 0; j < i; j++ {
				if clusters[j] == clusters[i] {
					break
				}
			}
			if j == i {
				if ent.NumClusters == q2const.MAX_ENT_CLUSTERS {
					// assume we missed some leafs, and mark by headnode
					ent.NumClusters = -1
					ent.HeadNode = topnode
					break
				}
				ent.ClusterNums[ent.NumClusters] = clusters[i]
				ent.NumClusters++
			}
		}
	}

	// if first time, make sure old_origin is valid
	if ent.LinkCount == 0 {
		ent.S.OldOrigin = ent.S.Origin
	}
	ent.LinkCount++

	if ent.Solid == q2const.SOLID_NOT {
		return
	}

	// find the first node that the ent's box crosses
	node := &w.nodes[0]
	for {
		if node.axis == -1 {
			break
		}
		if ent.AbsMin[node.axis] > node.dist {
			node = &w.nodes[node.children[0]]
		} else if ent.AbsMax[node.axis] < node.dist {
			node = &w.nodes[node.children[1]]
		} else {
			break // crosses the node
		}
	}

	// link it in
	ent.Area.Ent = ent
	if ent.Solid == q2const.SOLID_TRIGGER {
		InsertLinkBefore(&ent.Area, &node.triggerEdicts)
	} else {
		InsertLinkBefore(&ent.Area, &node.solidEdicts)
	}
}

// areaEdictsR collects the edicts of one node and recurses.
// C: server/sv_world.c:354 SV_AreaEdicts_r
func (w *World) areaEdictsR(nodeIdx int) {
	node := &w.nodes[nodeIdx]

	// touch linked edicts
	var start *game.Link
	if w.areaType == q2const.AREA_SOLID {
		start = &node.solidEdicts
	} else {
		start = &node.triggerEdicts
	}

	var next *game.Link
	for l := start.Next; l != start; l = next {
		next = l.Next
		check := l.Ent

		if check.Solid == q2const.SOLID_NOT {
			continue // deactivated
		}
		if check.AbsMin[0] > w.areaMaxs[0] ||
			check.AbsMin[1] > w.areaMaxs[1] ||
			check.AbsMin[2] > w.areaMaxs[2] ||
			check.AbsMax[0] < w.areaMins[0] ||
			check.AbsMax[1] < w.areaMins[1] ||
			check.AbsMax[2] < w.areaMins[2] {
			continue // not touching
		}

		if w.areaCount == len(w.areaList) {
			w.printf("SV_AreaEdicts: MAXCOUNT\n")
			return
		}

		w.areaList[w.areaCount] = check
		w.areaCount++
	}

	if node.axis == -1 {
		return // terminal node
	}

	// recurse down both sides
	if w.areaMaxs[node.axis] > node.dist {
		w.areaEdictsR(node.children[0])
	}
	if w.areaMins[node.axis] < node.dist {
		w.areaEdictsR(node.children[1])
	}
}

// AreaEdicts fills list (maxcount = len(list)) with the edicts whose abs box
// touches mins/maxs and returns their number. areatype is AREA_SOLID or
// AREA_TRIGGERS.
// C: server/sv_world.c:408 SV_AreaEdicts
func (w *World) AreaEdicts(mins, maxs Vec3, list []*game.Edict, areatype int) int {
	w.areaMins = mins
	w.areaMaxs = maxs
	w.areaList = list
	w.areaCount = 0
	w.areaType = areatype

	w.areaEdictsR(0)

	w.areaList = nil
	return w.areaCount
}

// PointContents returns the contents at p from the world and all solid
// entities.
// C: server/sv_world.c:431 SV_PointContents
func (w *World) PointContents(p Vec3) int32 {
	touch := w.pctouch[:]

	// get base contents from world
	contents := w.CM.PointContents(p, w.Models[1].Headnode)

	// or in contents from all the other entities
	num := w.AreaEdicts(p, p, touch, q2const.AREA_SOLID)

	for i := 0; i < num; i++ {
		hit := touch[i]

		// might intersect, so do an exact clip
		headnode := w.HullForEntity(hit)
		// C computes angles (vec3_origin for non-BSP) but then passes
		// hit->s.angles; kept.
		c2 := w.CM.TransformedPointContents(p, headnode, hit.S.Origin, hit.S.Angles)

		contents |= c2
	}
	return contents
}

// moveClip is C moveclip_t.
// C: server/sv_world.c:465 moveclip_t
type moveClip struct {
	boxmins, boxmaxs Vec3 // enclose the test object along entire move
	mins, maxs       Vec3 // size of the moving object
	mins2, maxs2     Vec3 // size when clipping against mosnters
	start, end       Vec3
	trace            game.Trace
	passedict        *game.Edict
	contentmask      int32
}

// HullForEntity returns a headnode that can be used for testing or clipping an
// object of mins/maxs size.
// C: server/sv_world.c:488 SV_HullForEntity
func (w *World) HullForEntity(ent *game.Edict) int32 {
	// decide which clipping hull to use, based on the size
	if ent.Solid == q2const.SOLID_BSP {
		// explicit hulls in the BSP model
		var model *shared.CModel
		if ent.S.ModelIndex >= 0 && int(ent.S.ModelIndex) < len(w.Models) {
			model = w.Models[ent.S.ModelIndex]
		}
		if model == nil {
			shared.Error(q2const.ERR_FATAL, "MOVETYPE_PUSH with a non bsp model")
		}
		return model.Headnode
	}

	// create a temp hull from bounding box sizes
	return w.CM.HeadnodeForBox(ent.Mins, ent.Maxs)
}

// convTrace turns a cmodel trace into a game trace (ent is set by the caller).
func convTrace(t shared.Trace) game.Trace {
	return game.Trace{
		AllSolid:   t.AllSolid,
		StartSolid: t.StartSolid,
		Fraction:   t.Fraction,
		EndPos:     t.EndPos,
		Plane:      t.Plane,
		Surface:    t.Surface,
		Contents:   t.Contents,
	}
}

// clipMoveToEntities clips the move against every solid entity in its box.
// C: server/sv_world.c:517 SV_ClipMoveToEntities
func (w *World) clipMoveToEntities(clip *moveClip) {
	// the touch list is reused: SV_Trace is not reentrant (neither is C's
	// SV_AreaEdicts with its file statics)
	touchlist := w.touchlist[:]
	num := w.AreaEdicts(clip.boxmins, clip.boxmaxs, touchlist, q2const.AREA_SOLID)

	// be careful, it is possible to have an entity in this
	// list removed before we get to it (killtriggered)
	for i := 0; i < num; i++ {
		touch := touchlist[i]
		if touch.Solid == q2const.SOLID_NOT {
			continue
		}
		if touch == clip.passedict {
			continue
		}
		if clip.trace.AllSolid {
			return
		}
		if clip.passedict != nil {
			if touch.Owner == clip.passedict {
				continue // don't clip against own missiles
			}
			if clip.passedict.Owner == touch {
				continue // don't clip against owner
			}
		}

		if clip.contentmask&q2const.CONTENTS_DEADMONSTER == 0 &&
			touch.SVFlags&q2const.SVF_DEADMONSTER != 0 {
			continue
		}

		// might intersect, so do an exact clip
		headnode := w.HullForEntity(touch)
		angles := touch.S.Angles
		if touch.Solid != q2const.SOLID_BSP {
			angles = shared.Vec3Origin // boxes don't rotate
		}

		var st shared.Trace
		if touch.SVFlags&q2const.SVF_MONSTER != 0 {
			st = w.CM.TransformedBoxTrace(clip.start, clip.end,
				clip.mins2, clip.maxs2, headnode, clip.contentmask,
				touch.S.Origin, angles)
		} else {
			st = w.CM.TransformedBoxTrace(clip.start, clip.end,
				clip.mins, clip.maxs, headnode, clip.contentmask,
				touch.S.Origin, angles)
		}
		trace := convTrace(st)

		if trace.AllSolid || trace.StartSolid || trace.Fraction < clip.trace.Fraction {
			trace.Ent = touch
			if clip.trace.StartSolid {
				clip.trace = trace
				clip.trace.StartSolid = true
			} else {
				clip.trace = trace
			}
		} else if trace.StartSolid {
			clip.trace.StartSolid = true
		}
	}
}

// TraceBounds computes the box enclosing the whole move.
// C: server/sv_world.c:589 SV_TraceBounds
func TraceBounds(start, mins, maxs, end Vec3, boxmins, boxmaxs *Vec3) {
	for i := 0; i < 3; i++ {
		if end[i] > start[i] {
			boxmins[i] = start[i] + mins[i] - 1
			boxmaxs[i] = end[i] + maxs[i] + 1
		} else {
			boxmins[i] = end[i] + mins[i] - 1
			boxmaxs[i] = start[i] + maxs[i] + 1
		}
	}
}

// Trace moves the given mins/maxs volume through the world from start to end.
// Passedict and edicts owned by passedict are explicitly not checked. nil mins
// or maxs mean vec3_origin.
// C: server/sv_world.c:624 SV_Trace
func (w *World) Trace(start, mins, maxs, end *Vec3, passedict *game.Edict, contentmask int32) game.Trace {
	if mins == nil {
		mins = &shared.Vec3Origin
	}
	if maxs == nil {
		maxs = &shared.Vec3Origin
	}

	var clip moveClip

	// clip to world
	clip.trace = convTrace(w.CM.BoxTrace(*start, *end, *mins, *maxs, 0, contentmask))
	clip.trace.Ent = w.WorldEdict()
	if clip.trace.Fraction == 0 {
		return clip.trace // blocked by the world
	}

	clip.contentmask = contentmask
	clip.start = *start
	clip.end = *end
	clip.mins = *mins
	clip.maxs = *maxs
	clip.passedict = passedict

	clip.mins2 = *mins
	clip.maxs2 = *maxs

	// create the bounding box of the entire move
	TraceBounds(*start, clip.mins2, clip.maxs2, *end, &clip.boxmins, &clip.boxmaxs)

	// clip to other solid entities
	w.clipMoveToEntities(&clip)

	return clip.trace
}

func (w *World) printf(format string, args ...any) {
	if w.Printf != nil {
		w.Printf(format, args...)
	}
}

func (w *World) dprintf(format string, args ...any) {
	if w.DPrintf != nil {
		w.DPrintf(format, args...)
	}
}
