// Package cmodel ports qcommon/cmodel.c: collision model loading, box and
// point traces, PVS/PHS decompression and area portals.
//
// The C globals are split in two: Map holds everything CM_LoadMap builds and
// never changes afterwards (shared read-only between game instances), and
// State holds the mutable parts (portal state, area flood numbers, the box
// hull planes CM_HeadnodeForBox rewrites, brush checkcounts and all trace
// scratch). A State is owned by one goroutine.
package cmodel

import (
	"fmt"
	"math"

	"quake2web/server/internal/bsp"
	"quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/md4"
	"quake2web/server/internal/qcommon/shared"
)

type Vec3 = shared.Vec3

// cnode is C cnode_t with the plane pointer as an index.
// C: qcommon/cmodel.c:28 cnode_t
type cnode struct {
	plane    int32
	children [2]int32 // negative numbers are leafs
}

// cbrushside is C cbrushside_t; surface -1 is nullsurface.
// C: qcommon/cmodel.c:34 cbrushside_t
type cbrushside struct {
	plane   int32
	surface int32
}

// cleaf is C cleaf_t.
// C: qcommon/cmodel.c:43 cleaf_t
type cleaf struct {
	contents       int32
	cluster        int32
	area           int32
	firstleafbrush uint16
	numleafbrushes uint16
}

// cbrush is C cbrush_t without checkcount (kept per State).
// C: qcommon/cmodel.c:51 cbrush_t
type cbrush struct {
	contents       int32
	numsides       int32
	firstbrushside int32
}

// carea is C carea_t without the flood fields (kept per State).
// C: qcommon/cmodel.c:59 carea_t
type carea struct {
	numareaportals  int32
	firstareaportal int32
}

// DIST_EPSILON is 1/32 epsilon to keep floating point happy. It is a double
// literal in C, so expressions using it are evaluated in double.
// C: qcommon/cmodel.c:974 DIST_EPSILON
const DIST_EPSILON = 0.03125

// Map is the immutable result of CM_LoadMap.
type Map struct {
	Name     string
	Checksum uint32 // Com_BlockChecksum of the file, as CM_LoadMap returns it

	numplanes int
	planes    []shared.CPlane // numplanes; box planes live in State

	numnodes int
	nodes    []cnode // numnodes + 6 (box hull)

	numleafs   int
	leafs      []cleaf // numleafs + 1 (box leaf)
	emptyleaf  int32
	solidleaf  int32
	numleafbrs int
	leafbrs    []uint16 // numleafbrushes + 1

	numbrushes int
	brushes    []cbrush // numbrushes + 1

	numbrushsides int
	brushsides    []cbrushside // numbrushsides + 6

	numtexinfo  int
	surfaces    []shared.MapSurface
	nullsurface shared.MapSurface
	badsurface  shared.MapSurface // target of brush sides with texinfo < 0

	numcmodels int
	cmodels    []shared.CModel

	numareas       int
	areas          []carea // MAX_MAP_AREAS like the C static array
	numareaportals int
	areaportals    []bsp.DAreaPortal

	numvisibility int
	visibility    []byte

	numclusters int

	entitystring string

	boxHeadnode int32
	boxPlanes   [12]shared.CPlane // template: normals/type/signbits, dist 0
}

// State is the per-instance mutable collision state.
type State struct {
	m *Map

	// NoAreas mirrors the map_noareas cvar ("0" by default).
	NoAreas bool

	checkcount int32
	brushCheck []int32 // cbrush_t.checkcount, numbrushes + 1

	boxPlanes [12]shared.CPlane // map_planes[numplanes..numplanes+11]

	portalopen  [q2const.MAX_MAP_AREAPORTALS]bool
	floodnum    [q2const.MAX_MAP_AREAS]int32
	floodvalidA [q2const.MAX_MAP_AREAS]int32
	floodvalid  int32

	// trace scratch (C file statics)
	traceStart, traceEnd Vec3
	traceMins, traceMaxs Vec3
	traceExtents         Vec3
	traceTrace           shared.Trace
	traceContents        int32
	traceIspoint         bool

	leafCount, leafMaxcount int
	leafList                []int32
	leafMins, leafMaxs      Vec3
	leafTopnode             int32
	leafBuf                 [1024]int32

	pvsrow [q2const.MAX_MAP_LEAFS / 8]byte
	phsrow [q2const.MAX_MAP_LEAFS / 8]byte

	// statistics counters, like C
	CPointcontents, CTraces, CBrushTraces int
}

/*
===============================================================================

					MAP LOADING

===============================================================================
*/

// loadSubmodels ports CMod_LoadSubmodels.
// C: qcommon/cmodel.c:137 CMod_LoadSubmodels
func (m *Map) loadSubmodels(in []bsp.DModel) error {
	count := len(in)
	if count < 1 {
		return fmt.Errorf("Map with no models")
	}
	if count > q2const.MAX_MAP_MODELS {
		return fmt.Errorf("Map has too many models")
	}
	m.numcmodels = count
	m.cmodels = make([]shared.CModel, count)
	for i := range in {
		out := &m.cmodels[i]
		for j := 0; j < 3; j++ { // spread the mins / maxs by a pixel
			out.Mins[j] = in[i].Mins[j] - 1
			out.Maxs[j] = in[i].Maxs[j] + 1
			out.Origin[j] = in[i].Origin[j]
		}
		out.Headnode = in[i].Headnode
	}
	return nil
}

// loadSurfaces ports CMod_LoadSurfaces.
// C: qcommon/cmodel.c:175 CMod_LoadSurfaces
func (m *Map) loadSurfaces(in []bsp.TexInfo) error {
	count := len(in)
	if count < 1 {
		return fmt.Errorf("Map with no surfaces")
	}
	if count > q2const.MAX_MAP_TEXINFO {
		return fmt.Errorf("Map has too many surfaces")
	}
	m.numtexinfo = count
	m.surfaces = make([]shared.MapSurface, count)
	for i := range in {
		name := in[i].TextureName()
		cname := name
		if len(cname) > 15 { // strncpy (out->c.name, in->texture, 15)
			cname = cname[:15]
		}
		rname := name
		if len(rname) > 31 {
			rname = rname[:31]
		}
		m.surfaces[i] = shared.MapSurface{
			C:     shared.CSurface{Name: cname, Flags: in[i].Flags, Value: in[i].Value},
			RName: rname,
		}
	}
	return nil
}

// loadNodes ports CMod_LoadNodes. Plane numbers are validated after the
// planes are loaded (see validate).
// C: qcommon/cmodel.c:209 CMod_LoadNodes
func (m *Map) loadNodes(in []bsp.DNode) error {
	count := len(in)
	if count < 1 {
		return fmt.Errorf("Map has no nodes")
	}
	if count > q2const.MAX_MAP_NODES {
		return fmt.Errorf("Map has too many nodes")
	}
	m.numnodes = count
	m.nodes = make([]cnode, count+6)
	for i := range in {
		m.nodes[i].plane = in[i].PlaneNum
		m.nodes[i].children = in[i].Children
	}
	return nil
}

// loadBrushes ports CMod_LoadBrushes.
// C: qcommon/cmodel.c:248 CMod_LoadBrushes
func (m *Map) loadBrushes(in []bsp.DBrush) error {
	count := len(in)
	if count > q2const.MAX_MAP_BRUSHES {
		return fmt.Errorf("Map has too many brushes")
	}
	m.numbrushes = count
	m.brushes = make([]cbrush, count+1)
	for i := range in {
		m.brushes[i] = cbrush{
			firstbrushside: in[i].FirstSide,
			numsides:       in[i].NumSides,
			contents:       in[i].Contents,
		}
	}
	return nil
}

// loadLeafs ports CMod_LoadLeafs (including the C check of the leaf count
// against MAX_MAP_PLANES).
// C: qcommon/cmodel.c:280 CMod_LoadLeafs
func (m *Map) loadLeafs(in []bsp.DLeaf) error {
	count := len(in)
	if count < 1 {
		return fmt.Errorf("Map with no leafs")
	}
	// need to save space for box planes
	if count > q2const.MAX_MAP_PLANES {
		return fmt.Errorf("Map has too many planes")
	}
	m.numleafs = count
	m.leafs = make([]cleaf, count+1)
	m.numclusters = 0
	for i := range in {
		out := &m.leafs[i]
		out.contents = in[i].Contents
		out.cluster = int32(in[i].Cluster)
		out.area = int32(in[i].Area)
		out.firstleafbrush = in[i].FirstLeafBrush
		out.numleafbrushes = in[i].NumLeafBrushes
		if int(out.cluster) >= m.numclusters {
			m.numclusters = int(out.cluster) + 1
		}
	}
	if m.leafs[0].contents != q2const.CONTENTS_SOLID {
		return fmt.Errorf("Map leaf 0 is not CONTENTS_SOLID")
	}
	m.solidleaf = 0
	m.emptyleaf = -1
	for i := 1; i < m.numleafs; i++ {
		if m.leafs[i].contents == 0 {
			m.emptyleaf = int32(i)
			break
		}
	}
	if m.emptyleaf == -1 {
		return fmt.Errorf("Map does not have an empty leaf")
	}
	return nil
}

// loadPlanes ports CMod_LoadPlanes.
// C: qcommon/cmodel.c:335 CMod_LoadPlanes
func (m *Map) loadPlanes(in []bsp.DPlane) error {
	count := len(in)
	if count < 1 {
		return fmt.Errorf("Map with no planes")
	}
	// need to save space for box planes
	if count > q2const.MAX_MAP_PLANES {
		return fmt.Errorf("Map has too many planes")
	}
	m.numplanes = count
	m.planes = make([]shared.CPlane, count)
	for i := range in {
		out := &m.planes[i]
		var bits uint8
		for j := 0; j < 3; j++ {
			out.Normal[j] = in[i].Normal[j]
			if out.Normal[j] < 0 {
				bits |= 1 << j
			}
		}
		out.Dist = in[i].Dist
		out.Type = uint8(in[i].Type)
		out.SignBits = bits
	}
	return nil
}

// loadLeafBrushes ports CMod_LoadLeafBrushes.
// C: qcommon/cmodel.c:378 CMod_LoadLeafBrushes
func (m *Map) loadLeafBrushes(in []uint16) error {
	count := len(in)
	if count < 1 {
		return fmt.Errorf("Map with no planes")
	}
	// need to save space for box planes
	if count > q2const.MAX_MAP_LEAFBRUSHES {
		return fmt.Errorf("Map has too many leafbrushes")
	}
	m.numleafbrs = count
	m.leafbrs = make([]uint16, count+1)
	copy(m.leafbrs, in)
	return nil
}

// loadBrushSides ports CMod_LoadBrushSides. Like C, the plane number and
// texinfo are read as signed shorts.
// C: qcommon/cmodel.c:408 CMod_LoadBrushSides
func (m *Map) loadBrushSides(in []bsp.DBrushSide) error {
	count := len(in)
	// need to save space for box planes
	if count > q2const.MAX_MAP_BRUSHSIDES {
		return fmt.Errorf("Map has too many planes")
	}
	m.numbrushsides = count
	m.brushsides = make([]cbrushside, count+6)
	for i := range in {
		num := int32(int16(in[i].PlaneNum)) // LittleShort
		if num < 0 || int(num) >= m.numplanes {
			return fmt.Errorf("Bad brushside plane %d", num) // C: out-of-bounds pointer
		}
		m.brushsides[i].plane = num
		j := int32(in[i].TexInfo)
		if int(j) >= m.numtexinfo {
			return fmt.Errorf("Bad brushside texinfo")
		}
		if j < 0 {
			// C points at map_surfaces[j] (before the array; qbsp writes -1
			// for bevel sides). In the 64-bit oracle that memory is zero, so
			// such sides resolve to an all-zero surface (see badsurface).
			j = -2
		}
		m.brushsides[i].surface = j
	}
	return nil
}

// loadAreas ports CMod_LoadAreas.
// C: qcommon/cmodel.c:444 CMod_LoadAreas
func (m *Map) loadAreas(in []bsp.DArea) error {
	count := len(in)
	if count > q2const.MAX_MAP_AREAS {
		return fmt.Errorf("Map has too many areas")
	}
	m.numareas = count
	m.areas = make([]carea, q2const.MAX_MAP_AREAS)
	for i := range in {
		m.areas[i].numareaportals = in[i].NumAreaPortals
		m.areas[i].firstareaportal = in[i].FirstAreaPortal
	}
	return nil
}

// loadAreaPortals ports CMod_LoadAreaPortals, keeping the C quirk of
// checking the count against MAX_MAP_AREAS.
// C: qcommon/cmodel.c:476 CMod_LoadAreaPortals
func (m *Map) loadAreaPortals(in []bsp.DAreaPortal) error {
	count := len(in)
	if count > q2const.MAX_MAP_AREAS {
		return fmt.Errorf("Map has too many areas")
	}
	m.numareaportals = count
	m.areaportals = append([]bsp.DAreaPortal(nil), in...)
	return nil
}

// loadVisibility ports CMod_LoadVisibility (the dvis_t header is decoded on
// use from the raw bytes).
// C: qcommon/cmodel.c:506 CMod_LoadVisibility
func (m *Map) loadVisibility(in []byte) error {
	m.numvisibility = len(in)
	if len(in) > q2const.MAX_MAP_VISIBILITY {
		return fmt.Errorf("Map has too large visibility lump")
	}
	m.visibility = append([]byte(nil), in...)
	return nil
}

// loadEntityString ports CMod_LoadEntityString.
// C: qcommon/cmodel.c:530 CMod_LoadEntityString
func (m *Map) loadEntityString(in []byte) error {
	if len(in) > q2const.MAX_MAP_ENTSTRING {
		return fmt.Errorf("Map has too large entity lump")
	}
	s := in
	for i, c := range s {
		if c == 0 {
			s = s[:i]
			break
		}
	}
	m.entitystring = string(s)
	return nil
}

// validate checks every cross reference the C code follows blindly, so that
// traces on malformed maps cannot index out of range (memory-safety fix).
func (m *Map) validate() error {
	child := func(c int32) bool {
		if c >= 0 {
			return int(c) < m.numnodes
		}
		return int(-1-c) < m.numleafs
	}
	for i := 0; i < m.numnodes; i++ {
		n := &m.nodes[i]
		if n.plane < 0 || int(n.plane) >= m.numplanes {
			return fmt.Errorf("node %d: bad plane %d", i, n.plane)
		}
		if !child(n.children[0]) || !child(n.children[1]) {
			return fmt.Errorf("node %d: bad children %v", i, n.children)
		}
	}
	if err := m.checkNodeCycles(); err != nil {
		return err
	}
	for i := 0; i < m.numleafs; i++ {
		l := &m.leafs[i]
		if int(l.firstleafbrush)+int(l.numleafbrushes) > m.numleafbrs {
			return fmt.Errorf("leaf %d: bad leafbrush range", i)
		}
	}
	for i := 0; i < m.numleafbrs; i++ {
		if int(m.leafbrs[i]) >= m.numbrushes {
			return fmt.Errorf("leafbrush %d: bad brush %d", i, m.leafbrs[i])
		}
	}
	for i := 0; i < m.numbrushes; i++ {
		b := &m.brushes[i]
		if b.numsides < 0 || b.firstbrushside < 0 || int64(b.firstbrushside)+int64(b.numsides) > int64(m.numbrushsides) {
			return fmt.Errorf("brush %d: bad side range", i)
		}
	}
	for i := 0; i < m.numcmodels; i++ {
		if !child(m.cmodels[i].Headnode) {
			return fmt.Errorf("model %d: bad headnode %d", i, m.cmodels[i].Headnode)
		}
	}
	for i := 0; i < m.numareas; i++ {
		a := &m.areas[i]
		if a.numareaportals < 0 || a.firstareaportal < 0 ||
			int64(a.firstareaportal)+int64(a.numareaportals) > int64(m.numareaportals) {
			return fmt.Errorf("area %d: bad portal range", i)
		}
	}
	for i, p := range m.areaportals {
		if p.PortalNum < 0 || p.PortalNum >= q2const.MAX_MAP_AREAPORTALS ||
			p.OtherArea < 0 || p.OtherArea >= q2const.MAX_MAP_AREAS {
			return fmt.Errorf("areaportal %d: bad portal/area %d/%d", i, p.PortalNum, p.OtherArea)
		}
	}
	return nil
}

// checkNodeCycles rejects node graphs that are not a forest: a child that
// leads back to one of its ancestors (a cycle) or a node with two parents (a
// shared subtree). On a cycle the tree walks (CM_PointLeafnum_r loops,
// CM_RecursiveHullCheck, CM_BoxLeafnums_r, CM_HeadnodeVisible recurse) never
// terminate: an endless loop or a fatal, unrecoverable Go stack overflow. A
// chain of shared subtrees makes the walks that visit both children take 2^N
// steps. Memory-safety fix for user-supplied maps; compiled BSPs are trees
// (every node has exactly one parent, the model headnodes none) and pass.
func (m *Map) checkNodeCycles() error {
	const (
		unvisited = iota
		onStack
		done
	)
	parents := make([]uint8, m.numnodes)
	for i := 0; i < m.numnodes; i++ {
		for _, c := range m.nodes[i].children {
			if c < 0 {
				continue
			}
			if parents[c] != 0 {
				return fmt.Errorf("node %d: node %d has more than one parent", i, c)
			}
			parents[c] = 1
		}
	}
	state := make([]uint8, m.numnodes)
	type frame struct {
		node int32
		next int // next child to visit (0, 1, 2 = finished)
	}
	var stack []frame
	for root := 0; root < m.numnodes; root++ {
		if state[root] != unvisited {
			continue
		}
		state[root] = onStack
		stack = append(stack[:0], frame{node: int32(root)})
		for len(stack) > 0 {
			top := &stack[len(stack)-1]
			if top.next == 2 {
				state[top.node] = done
				stack = stack[:len(stack)-1]
				continue
			}
			c := m.nodes[top.node].children[top.next]
			top.next++
			if c < 0 {
				continue // leaf
			}
			switch state[c] {
			case onStack:
				return fmt.Errorf("node %d: child %d forms a cycle", top.node, c)
			case unvisited:
				state[c] = onStack
				stack = append(stack, frame{node: c})
			}
		}
	}
	return nil
}

// LoadMap builds a Map from a parsed BSP and the raw file bytes (for the
// checksum), in the lump order of CM_LoadMap. An empty name yields the
// "no map" state C uses for cinematic servers.
// C: qcommon/cmodel.c:548 CM_LoadMap
func LoadMap(name string, f *bsp.File, raw []byte) (*Map, error) {
	if name == "" {
		return EmptyMap(), nil
	}
	m := &Map{Name: name}
	m.Checksum = md4.Com_BlockChecksum(raw)

	if f.Header.Version != q2const.BSPVERSION {
		return nil, fmt.Errorf("CMod_LoadBrushModel: %s has wrong version number (%d should be %d)",
			name, f.Header.Version, q2const.BSPVERSION)
	}

	steps := []func() error{
		func() error { return m.loadSurfaces(f.TexInfo) },
		func() error { return m.loadLeafs(f.Leafs) },
		func() error { return m.loadLeafBrushes(f.LeafBrushes) },
		func() error { return m.loadPlanes(f.Planes) },
		func() error { return m.loadBrushes(f.Brushes) },
		func() error { return m.loadBrushSides(f.BrushSides) },
		func() error { return m.loadSubmodels(f.Models) },
		func() error { return m.loadNodes(f.Nodes) },
		func() error { return m.loadAreas(f.Areas) },
		func() error { return m.loadAreaPortals(f.AreaPortals) },
		func() error { return m.loadVisibility(f.Visibility) },
		func() error { return m.loadEntityString(f.Entities) },
		m.validate,
		m.initBoxHull,
	}
	for _, step := range steps {
		if err := step(); err != nil {
			return nil, fmt.Errorf("cmodel %s: %w", name, err)
		}
	}
	return m, nil
}

// LoadMapBytes parses raw BSP bytes and loads them.
func LoadMapBytes(name string, raw []byte) (*Map, error) {
	f, err := bsp.Parse(raw)
	if err != nil {
		return nil, err
	}
	return LoadMap(name, f, raw)
}

// EmptyMap is the state after CM_LoadMap with an empty name: one leaf, one
// cluster, one area, no nodes (traces return the default trace).
// C: qcommon/cmodel.c:579 CM_LoadMap (empty name branch)
func EmptyMap() *Map {
	return &Map{
		numleafs:    1,
		leafs:       make([]cleaf, 2),
		numclusters: 1,
		numareas:    1,
		areas:       make([]carea, q2const.MAX_MAP_AREAS),
		cmodels:     make([]shared.CModel, 1),
	}
}

// NewState creates the mutable collision state for m and performs the
// portal reset CM_LoadMap does (memset portalopen, FloodAreaConnections).
func NewState(m *Map) *State {
	s := &State{m: m}
	s.brushCheck = make([]int32, m.numbrushes+1)
	s.boxPlanes = m.boxPlanes
	s.ResetPortals()
	return s
}

// Map returns the immutable map the state was created for.
func (s *State) Map() *Map { return s.m }

// ResetPortals closes every portal and refloods, as CM_LoadMap does when it
// (re)loads a map for the server.
// C: qcommon/cmodel.c:558 CM_LoadMap (same map branch)
func (s *State) ResetPortals() {
	s.portalopen = [q2const.MAX_MAP_AREAPORTALS]bool{}
	s.FloodAreaConnections()
}

// InlineModel returns submodel "*N".
// C: qcommon/cmodel.c:639 CM_InlineModel
func (m *Map) InlineModel(name string) *shared.CModel {
	if name == "" || name[0] != '*' {
		shared.Error(q2const.ERR_DROP, "CM_InlineModel: bad name")
	}
	num := atoi(name[1:])
	if num < 1 || num >= m.numcmodels {
		shared.Error(q2const.ERR_DROP, "CM_InlineModel: bad number")
	}
	return &m.cmodels[num]
}

// WorldModel returns map_cmodels[0], what CM_LoadMap returns.
func (m *Map) WorldModel() *shared.CModel { return &m.cmodels[0] }

// atoi is C atoi for the digits after '*' (leading space, sign, digits).
func atoi(s string) int {
	i := 0
	for i < len(s) && (s[i] == ' ' || (s[i] >= '\t' && s[i] <= '\r')) {
		i++
	}
	neg := false
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		neg = s[i] == '-'
		i++
	}
	var n int64
	for ; i < len(s) && s[i] >= '0' && s[i] <= '9'; i++ {
		n = n*10 + int64(s[i]-'0')
		if n > 1<<40 {
			n = 1 << 40
		}
	}
	if neg {
		n = -n
	}
	return int(int32(n))
}

// NumClusters is CM_NumClusters.
// C: qcommon/cmodel.c:652 CM_NumClusters
func (m *Map) NumClusters() int { return m.numclusters }

// NumInlineModels is CM_NumInlineModels.
// C: qcommon/cmodel.c:657 CM_NumInlineModels
func (m *Map) NumInlineModels() int { return m.numcmodels }

// EntityString is CM_EntityString.
// C: qcommon/cmodel.c:662 CM_EntityString
func (m *Map) EntityString() string { return m.entitystring }

// LeafContents is CM_LeafContents.
// C: qcommon/cmodel.c:667 CM_LeafContents
func (m *Map) LeafContents(leafnum int) int32 {
	if leafnum < 0 || leafnum >= m.numleafs {
		shared.Error(q2const.ERR_DROP, "CM_LeafContents: bad number")
	}
	return m.leafs[leafnum].contents
}

// LeafCluster is CM_LeafCluster.
// C: qcommon/cmodel.c:674 CM_LeafCluster
func (m *Map) LeafCluster(leafnum int) int32 {
	if leafnum < 0 || leafnum >= m.numleafs {
		shared.Error(q2const.ERR_DROP, "CM_LeafCluster: bad number")
	}
	return m.leafs[leafnum].cluster
}

// LeafArea is CM_LeafArea.
// C: qcommon/cmodel.c:681 CM_LeafArea
func (m *Map) LeafArea(leafnum int) int32 {
	if leafnum < 0 || leafnum >= m.numleafs {
		shared.Error(q2const.ERR_DROP, "CM_LeafArea: bad number")
	}
	return m.leafs[leafnum].area
}

// Counts exposes the loaded element counts (for tests and tools).
type Counts struct {
	Planes, Nodes, Leafs, LeafBrushes, Brushes, BrushSides, TexInfo int
	Models, Areas, AreaPortals, Clusters, Visibility                int
}

// Counts returns the C num* globals.
func (m *Map) Counts() Counts {
	return Counts{
		Planes: m.numplanes, Nodes: m.numnodes, Leafs: m.numleafs, LeafBrushes: m.numleafbrs,
		Brushes: m.numbrushes, BrushSides: m.numbrushsides, TexInfo: m.numtexinfo,
		Models: m.numcmodels, Areas: m.numareas, AreaPortals: m.numareaportals,
		Clusters: m.numclusters, Visibility: m.numvisibility,
	}
}

//=======================================================================

// initBoxHull sets up the planes and nodes so that the six floats of a
// bounding box can just be stored out and get a proper clipping hull.
// C: qcommon/cmodel.c:704 CM_InitBoxHull
func (m *Map) initBoxHull() error {
	m.boxHeadnode = int32(m.numnodes)
	if m.numnodes+6 > q2const.MAX_MAP_NODES ||
		m.numbrushes+1 > q2const.MAX_MAP_BRUSHES ||
		m.numleafbrs+1 > q2const.MAX_MAP_LEAFBRUSHES ||
		m.numbrushsides+6 > q2const.MAX_MAP_BRUSHSIDES ||
		m.numplanes+12 > q2const.MAX_MAP_PLANES {
		return fmt.Errorf("Not enough room for box tree")
	}

	boxBrush := &m.brushes[m.numbrushes]
	boxBrush.numsides = 6
	boxBrush.firstbrushside = int32(m.numbrushsides)
	boxBrush.contents = q2const.CONTENTS_MONSTER

	boxLeaf := &m.leafs[m.numleafs]
	boxLeaf.contents = q2const.CONTENTS_MONSTER
	boxLeaf.firstleafbrush = uint16(m.numleafbrs)
	boxLeaf.numleafbrushes = 1

	m.leafbrs[m.numleafbrs] = uint16(m.numbrushes)

	np := int32(m.numplanes)
	for i := int32(0); i < 6; i++ {
		side := i & 1

		// brush sides
		s := &m.brushsides[int32(m.numbrushsides)+i]
		s.plane = np + i*2 + side
		s.surface = -1

		// nodes
		c := &m.nodes[m.boxHeadnode+i]
		c.plane = np + i*2
		c.children[side] = -1 - m.emptyleaf
		if i != 5 {
			c.children[side^1] = m.boxHeadnode + i + 1
		} else {
			c.children[side^1] = -1 - int32(m.numleafs)
		}

		// planes
		p := &m.boxPlanes[i*2]
		p.Type = uint8(i >> 1)
		p.SignBits = 0
		p.Normal = Vec3{}
		p.Normal[i>>1] = 1

		p = &m.boxPlanes[i*2+1]
		p.Type = uint8(3 + (i >> 1))
		p.SignBits = 0
		p.Normal = Vec3{}
		p.Normal[i>>1] = -1
	}
	return nil
}

// plane resolves a plane index into map_planes, where the 12 entries past
// numplanes are this State's box planes.
func (s *State) plane(i int32) *shared.CPlane {
	if int(i) >= s.m.numplanes {
		return &s.boxPlanes[int(i)-s.m.numplanes]
	}
	return &s.m.planes[i]
}

// surface resolves a brush side surface index (-1 = nullsurface, -2 = the
// zeroed stand-in for a negative texinfo).
func (m *Map) surface(i int32) *shared.CSurface {
	if i < 0 {
		if i == -2 {
			return &m.badsurface.C
		}
		return &m.nullsurface.C
	}
	return &m.surfaces[i].C
}

// NullSurface returns &nullsurface.c.
func (m *Map) NullSurface() *shared.CSurface { return &m.nullsurface.C }

// HeadnodeForBox turns a bounding box into the small box BSP tree.
// C: qcommon/cmodel.c:775 CM_HeadnodeForBox
func (s *State) HeadnodeForBox(mins, maxs Vec3) int32 {
	bp := &s.boxPlanes
	bp[0].Dist = maxs[0]
	bp[1].Dist = -maxs[0]
	bp[2].Dist = mins[0]
	bp[3].Dist = -mins[0]
	bp[4].Dist = maxs[1]
	bp[5].Dist = -maxs[1]
	bp[6].Dist = mins[1]
	bp[7].Dist = -mins[1]
	bp[8].Dist = maxs[2]
	bp[9].Dist = -maxs[2]
	bp[10].Dist = mins[2]
	bp[11].Dist = -mins[2]
	return s.m.boxHeadnode
}

// pointLeafnum_r walks the tree from num to the leaf containing p.
// C: qcommon/cmodel.c:800 CM_PointLeafnum_r
func (s *State) pointLeafnum_r(p *Vec3, num int32) int32 {
	var d float32
	for num >= 0 {
		node := &s.m.nodes[num]
		plane := s.plane(node.plane)
		if plane.Type < 3 {
			d = p[plane.Type] - plane.Dist
		} else {
			d = shared.DotProduct(plane.Normal, *p) - plane.Dist
		}
		if d < 0 {
			num = node.children[1]
		} else {
			num = node.children[0]
		}
	}
	s.CPointcontents++ // optimize counter
	return -1 - num
}

// PointLeafnum returns the world leaf containing p.
// C: qcommon/cmodel.c:826 CM_PointLeafnum
func (s *State) PointLeafnum(p Vec3) int32 {
	if s.m.numplanes == 0 {
		return 0 // sound may call this without map loaded
	}
	return s.pointLeafnum_r(&p, 0)
}

// boxLeafnums_r fills in the list of all the leafs touched.
// C: qcommon/cmodel.c:847 CM_BoxLeafnums_r
func (s *State) boxLeafnums_r(nodenum int32) {
	for {
		if nodenum < 0 {
			if s.leafCount >= s.leafMaxcount {
				return
			}
			s.leafList[s.leafCount] = -1 - nodenum
			s.leafCount++
			return
		}

		node := &s.m.nodes[nodenum]
		plane := s.plane(node.plane)
		sd := shared.BOX_ON_PLANE_SIDE(&s.leafMins, &s.leafMaxs, plane)
		if sd == 1 {
			nodenum = node.children[0]
		} else if sd == 2 {
			nodenum = node.children[1]
		} else { // go down both
			if s.leafTopnode == -1 {
				s.leafTopnode = nodenum
			}
			s.boxLeafnums_r(node.children[0])
			nodenum = node.children[1]
		}
	}
}

// boxLeafnumsHeadnode fills list with the leafs touched by the box under
// headnode; topnode receives the first node where the box straddles.
// C: qcommon/cmodel.c:885 CM_BoxLeafnums_headnode
func (s *State) boxLeafnumsHeadnode(mins, maxs Vec3, list []int32, headnode int32, topnode *int32) int {
	s.leafList = list
	s.leafCount = 0
	s.leafMaxcount = len(list)
	s.leafMins = mins
	s.leafMaxs = maxs
	s.leafTopnode = -1

	s.boxLeafnums_r(headnode)

	if topnode != nil {
		*topnode = s.leafTopnode
	}
	s.leafList = nil
	return s.leafCount
}

// BoxLeafnums lists the world leafs touching the box (len(list) is the C
// listsize). topnode may be nil.
// C: qcommon/cmodel.c:903 CM_BoxLeafnums
func (s *State) BoxLeafnums(mins, maxs Vec3, list []int32, topnode *int32) int {
	return s.boxLeafnumsHeadnode(mins, maxs, list, s.m.cmodels[0].Headnode, topnode)
}

// PointContents returns the contents of the leaf containing p under headnode.
// C: qcommon/cmodel.c:917 CM_PointContents
func (s *State) PointContents(p Vec3, headnode int32) int32 {
	if s.m.numnodes == 0 { // map not loaded
		return 0
	}
	l := s.pointLeafnum_r(&p, headnode)
	return s.m.leafs[l].contents
}

// TransformedPointContents handles offsetting and rotation of the point for
// moving and rotating entities.
// C: qcommon/cmodel.c:937 CM_TransformedPointContents
func (s *State) TransformedPointContents(p Vec3, headnode int32, origin, angles Vec3) int32 {
	var forward, right, up Vec3

	// subtract origin offset
	pl := shared.VectorSubtract(p, origin)

	// rotate start and end into the models frame of reference
	if headnode != s.m.boxHeadnode && (angles[0] != 0 || angles[1] != 0 || angles[2] != 0) {
		shared.AngleVectors(angles, &forward, &right, &up)

		temp := pl
		pl[0] = shared.DotProduct(temp, forward)
		pl[1] = -shared.DotProduct(temp, right)
		pl[2] = shared.DotProduct(temp, up)
	}

	l := s.pointLeafnum_r(&pl, headnode)
	return s.m.leafs[l].contents
}

/*
===============================================================================

BOX TRACING

===============================================================================
*/

// clipBoxToBrush clips the move p1->p2 of the box against one brush.
// C: qcommon/cmodel.c:989 CM_ClipBoxToBrush
func (s *State) clipBoxToBrush(mins, maxs, p1, p2 *Vec3, trace *shared.Trace, brush *cbrush) {
	var clipplane *shared.CPlane
	var leadside *cbrushside
	var dist, d1, d2, f float32
	var ofs Vec3

	enterfrac := float32(-1)
	leavefrac := float32(1)

	if brush.numsides == 0 {
		return
	}

	s.CBrushTraces++

	getout := false
	startout := false

	for i := int32(0); i < brush.numsides; i++ {
		side := &s.m.brushsides[brush.firstbrushside+i]
		plane := s.plane(side.plane)

		// FIXME: special case for axial

		if !s.traceIspoint { // general box case
			// push the plane out apropriately for mins/maxs
			for j := 0; j < 3; j++ {
				if plane.Normal[j] < 0 {
					ofs[j] = maxs[j]
				} else {
					ofs[j] = mins[j]
				}
			}
			dist = shared.DotProduct(ofs, plane.Normal)
			dist = plane.Dist - dist
		} else { // special point case
			dist = plane.Dist
		}

		d1 = shared.DotProduct(*p1, plane.Normal) - dist
		d2 = shared.DotProduct(*p2, plane.Normal) - dist

		if d2 > 0 {
			getout = true // endpoint is not in solid
		}
		if d1 > 0 {
			startout = true
		}

		// if completely in front of face, no intersection
		if d1 > 0 && d2 >= d1 {
			return
		}

		if d1 <= 0 && d2 <= 0 {
			continue
		}

		// crosses face
		if d1 > d2 { // enter
			f = float32((float64(d1) - DIST_EPSILON) / float64(d1-d2))
			if f > enterfrac {
				enterfrac = f
				clipplane = plane
				leadside = side
			}
		} else { // leave
			f = float32((float64(d1) + DIST_EPSILON) / float64(d1-d2))
			if f < leavefrac {
				leavefrac = f
			}
		}
	}

	if !startout { // original point was inside brush
		trace.StartSolid = true
		if !getout {
			trace.AllSolid = true
		}
		return
	}
	if enterfrac < leavefrac {
		if enterfrac > -1 && enterfrac < trace.Fraction {
			if enterfrac < 0 {
				enterfrac = 0
			}
			trace.Fraction = enterfrac
			trace.Plane = *clipplane
			trace.Surface = s.m.surface(leadside.surface)
			trace.Contents = brush.contents
		}
	}
}

// testBoxInBrush tests whether the box at p1 is inside the brush.
// C: qcommon/cmodel.c:1103 CM_TestBoxInBrush
func (s *State) testBoxInBrush(mins, maxs, p1 *Vec3, trace *shared.Trace, brush *cbrush) {
	var ofs Vec3
	if brush.numsides == 0 {
		return
	}
	for i := int32(0); i < brush.numsides; i++ {
		side := &s.m.brushsides[brush.firstbrushside+i]
		plane := s.plane(side.plane)

		// general box case
		for j := 0; j < 3; j++ {
			if plane.Normal[j] < 0 {
				ofs[j] = maxs[j]
			} else {
				ofs[j] = mins[j]
			}
		}
		dist := shared.DotProduct(ofs, plane.Normal)
		dist = plane.Dist - dist

		d1 := shared.DotProduct(*p1, plane.Normal) - dist

		// if completely in front of face, no intersection
		if d1 > 0 {
			return
		}
	}

	// inside this brush
	trace.StartSolid = true
	trace.AllSolid = true
	trace.Fraction = 0
	trace.Contents = brush.contents
}

// traceToLeaf traces the line against all brushes in the leaf.
// C: qcommon/cmodel.c:1158 CM_TraceToLeaf
func (s *State) traceToLeaf(leafnum int32) {
	leaf := &s.m.leafs[leafnum]
	if leaf.contents&s.traceContents == 0 {
		return
	}
	// trace line against all brushes in the leaf
	for k := 0; k < int(leaf.numleafbrushes); k++ {
		brushnum := s.m.leafbrs[int(leaf.firstleafbrush)+k]
		b := &s.m.brushes[brushnum]
		if s.brushCheck[brushnum] == s.checkcount {
			continue // already checked this brush in another leaf
		}
		s.brushCheck[brushnum] = s.checkcount

		if b.contents&s.traceContents == 0 {
			continue
		}
		s.clipBoxToBrush(&s.traceMins, &s.traceMaxs, &s.traceStart, &s.traceEnd, &s.traceTrace, b)
		if s.traceTrace.Fraction == 0 {
			return
		}
	}
}

// testInLeaf tests the box against all brushes in the leaf.
// C: qcommon/cmodel.c:1192 CM_TestInLeaf
func (s *State) testInLeaf(leafnum int32) {
	leaf := &s.m.leafs[leafnum]
	if leaf.contents&s.traceContents == 0 {
		return
	}
	// trace line against all brushes in the leaf
	for k := 0; k < int(leaf.numleafbrushes); k++ {
		brushnum := s.m.leafbrs[int(leaf.firstleafbrush)+k]
		b := &s.m.brushes[brushnum]
		if s.brushCheck[brushnum] == s.checkcount {
			continue // already checked this brush in another leaf
		}
		s.brushCheck[brushnum] = s.checkcount

		if b.contents&s.traceContents == 0 {
			continue
		}
		s.testBoxInBrush(&s.traceMins, &s.traceMaxs, &s.traceStart, &s.traceTrace, b)
		if s.traceTrace.Fraction == 0 {
			return
		}
	}
}

// recursiveHullCheck sweeps p1->p2 (fractions p1f..p2f) through node num.
// C: qcommon/cmodel.c:1227 CM_RecursiveHullCheck
func (s *State) recursiveHullCheck(num int32, p1f, p2f float32, p1, p2 Vec3) {
	var t1, t2, offset, frac, frac2, idist, midf float32
	var mid Vec3
	var side int32

	if s.traceTrace.Fraction <= p1f {
		return // already hit something nearer
	}

	// if < 0, we are in a leaf node
	if num < 0 {
		s.traceToLeaf(-1 - num)
		return
	}

	// find the point distances to the seperating plane
	// and the offset for the size of the box
	node := &s.m.nodes[num]
	plane := s.plane(node.plane)

	if plane.Type < 3 {
		t1 = p1[plane.Type] - plane.Dist
		t2 = p2[plane.Type] - plane.Dist
		offset = s.traceExtents[plane.Type]
	} else {
		t1 = shared.DotProduct(plane.Normal, p1) - plane.Dist
		t2 = shared.DotProduct(plane.Normal, p2) - plane.Dist
		if s.traceIspoint {
			offset = 0
		} else {
			// fabs() returns double: the sum is formed in double
			offset = float32(math.Abs(float64(s.traceExtents[0]*plane.Normal[0])) +
				math.Abs(float64(s.traceExtents[1]*plane.Normal[1])) +
				math.Abs(float64(s.traceExtents[2]*plane.Normal[2])))
		}
	}

	// see which sides we need to consider
	if t1 >= offset && t2 >= offset {
		s.recursiveHullCheck(node.children[0], p1f, p2f, p1, p2)
		return
	}
	if t1 < -offset && t2 < -offset {
		s.recursiveHullCheck(node.children[1], p1f, p2f, p1, p2)
		return
	}

	// put the crosspoint DIST_EPSILON pixels on the near side
	if t1 < t2 {
		idist = float32(1.0 / float64(t1-t2))
		side = 1
		frac2 = float32((float64(t1+offset) + DIST_EPSILON) * float64(idist))
		frac = float32((float64(t1-offset) + DIST_EPSILON) * float64(idist))
	} else if t1 > t2 {
		idist = float32(1.0 / float64(t1-t2))
		side = 0
		frac2 = float32((float64(t1-offset) - DIST_EPSILON) * float64(idist))
		frac = float32((float64(t1+offset) + DIST_EPSILON) * float64(idist))
	} else {
		side = 0
		frac = 1
		frac2 = 0
	}

	// move up to the node
	if frac < 0 {
		frac = 0
	}
	if frac > 1 {
		frac = 1
	}

	midf = p1f + float32((p2f-p1f)*frac)
	for i := 0; i < 3; i++ {
		mid[i] = p1[i] + float32(frac*(p2[i]-p1[i]))
	}

	s.recursiveHullCheck(node.children[side], p1f, midf, p1, mid)

	// go past the node
	if frac2 < 0 {
		frac2 = 0
	}
	if frac2 > 1 {
		frac2 = 1
	}

	midf = p1f + float32((p2f-p1f)*frac2)
	for i := 0; i < 3; i++ {
		mid[i] = p1[i] + float32(frac2*(p2[i]-p1[i]))
	}

	s.recursiveHullCheck(node.children[side^1], midf, p2f, mid, p2)
}

//======================================================================

// BoxTrace sweeps the box mins/maxs from start to end through headnode.
// C: qcommon/cmodel.c:1350 CM_BoxTrace
func (s *State) BoxTrace(start, end, mins, maxs Vec3, headnode int32, brushmask int32) shared.Trace {
	s.checkcount++ // for multi-check avoidance

	s.CTraces++ // for statistics, may be zeroed

	// fill in a default trace
	s.traceTrace = shared.Trace{Fraction: 1, Surface: &s.m.nullsurface.C, Ent: -1}

	if s.m.numnodes == 0 { // map not loaded
		return s.traceTrace
	}

	s.traceContents = brushmask
	s.traceStart = start
	s.traceEnd = end
	s.traceMins = mins
	s.traceMaxs = maxs

	// check for position test special case
	if start[0] == end[0] && start[1] == end[1] && start[2] == end[2] {
		var topnode int32

		c1 := shared.VectorAdd(start, mins)
		c2 := shared.VectorAdd(start, maxs)
		for i := 0; i < 3; i++ {
			c1[i] -= 1
			c2[i] += 1
		}

		numleafs := s.boxLeafnumsHeadnode(c1, c2, s.leafBuf[:], headnode, &topnode)
		for i := 0; i < numleafs; i++ {
			s.testInLeaf(s.leafBuf[i])
			if s.traceTrace.AllSolid {
				break
			}
		}
		s.traceTrace.EndPos = start
		return s.traceTrace
	}

	// check for point special case
	if mins[0] == 0 && mins[1] == 0 && mins[2] == 0 &&
		maxs[0] == 0 && maxs[1] == 0 && maxs[2] == 0 {
		s.traceIspoint = true
		s.traceExtents = Vec3{}
	} else {
		s.traceIspoint = false
		for i := 0; i < 3; i++ {
			if -mins[i] > maxs[i] {
				s.traceExtents[i] = -mins[i]
			} else {
				s.traceExtents[i] = maxs[i]
			}
		}
	}

	// general sweeping through world
	s.recursiveHullCheck(headnode, 0, 1, start, end)

	if s.traceTrace.Fraction == 1 {
		s.traceTrace.EndPos = end
	} else {
		for i := 0; i < 3; i++ {
			s.traceTrace.EndPos[i] = start[i] + float32(s.traceTrace.Fraction*(end[i]-start[i]))
		}
	}
	return s.traceTrace
}

// TransformedBoxTrace handles offsetting and rotation of the end points for
// moving and rotating entities.
// C: qcommon/cmodel.c:1451 CM_TransformedBoxTrace
func (s *State) TransformedBoxTrace(start, end, mins, maxs Vec3, headnode int32, brushmask int32, origin, angles Vec3) shared.Trace {
	var forward, right, up Vec3

	// subtract origin offset
	startL := shared.VectorSubtract(start, origin)
	endL := shared.VectorSubtract(end, origin)

	// rotate start and end into the models frame of reference
	rotated := headnode != s.m.boxHeadnode && (angles[0] != 0 || angles[1] != 0 || angles[2] != 0)

	if rotated {
		shared.AngleVectors(angles, &forward, &right, &up)

		temp := startL
		startL[0] = shared.DotProduct(temp, forward)
		startL[1] = -shared.DotProduct(temp, right)
		startL[2] = shared.DotProduct(temp, up)

		temp = endL
		endL[0] = shared.DotProduct(temp, forward)
		endL[1] = -shared.DotProduct(temp, right)
		endL[2] = shared.DotProduct(temp, up)
	}

	// sweep the box through the model
	trace := s.BoxTrace(startL, endL, mins, maxs, headnode, brushmask)

	if rotated && trace.Fraction != 1.0 {
		// FIXME: figure out how to do this with existing angles
		a := shared.VectorNegate(angles)
		shared.AngleVectors(a, &forward, &right, &up)

		temp := trace.Plane.Normal
		trace.Plane.Normal[0] = shared.DotProduct(temp, forward)
		trace.Plane.Normal[1] = -shared.DotProduct(temp, right)
		trace.Plane.Normal[2] = shared.DotProduct(temp, up)
	}

	trace.EndPos[0] = start[0] + float32(trace.Fraction*(end[0]-start[0]))
	trace.EndPos[1] = start[1] + float32(trace.Fraction*(end[1]-start[1]))
	trace.EndPos[2] = start[2] + float32(trace.Fraction*(end[2]-start[2]))

	return trace
}

/*
===============================================================================

PVS / PHS

===============================================================================
*/

// visByte reads map_visibility[i]; bytes past the lump read as 0 like the
// zeroed tail of the C static buffer.
func (m *Map) visByte(i int) byte {
	if i < 0 || i >= len(m.visibility) {
		return 0
	}
	return m.visibility[i]
}

// bitofs reads map_vis->bitofs[cluster][which] from the raw lump.
func (m *Map) bitofs(cluster int, which int) int {
	o := 4 + cluster*8 + which*4
	return int(int32(uint32(m.visByte(o)) | uint32(m.visByte(o+1))<<8 |
		uint32(m.visByte(o+2))<<16 | uint32(m.visByte(o+3))<<24))
}

// decompressVis expands the run-length compressed row at offset in.
// C: qcommon/cmodel.c:1530 CM_DecompressVis
func (m *Map) decompressVis(in int, out []byte) {
	row := (m.numclusters + 7) >> 3
	outP := 0

	if m.numvisibility == 0 { // no vis info, so make all visible
		for ; row > 0; row-- {
			out[outP] = 0xff
			outP++
		}
		return
	}

	for {
		if in < 0 || in >= len(m.visibility) {
			// C would read past the lump (zeros forever): stop safely
			for outP < row {
				out[outP] = 0
				outP++
			}
			return
		}
		if b := m.visByte(in); b != 0 {
			out[outP] = b
			outP++
			in++
		} else {
			c := int(m.visByte(in + 1))
			in += 2
			if outP+c > row {
				c = row - outP
			}
			for ; c > 0; c-- {
				out[outP] = 0
				outP++
			}
		}
		if outP >= row {
			break
		}
	}
}

// ClusterPVS returns the decompressed PVS row of cluster (cluster -1 gives
// an all-zero row). The returned slice is the State's MAX_MAP_LEAFS/8 byte
// buffer, reused by the next call; only the first (numclusters+7)>>3 bytes
// are meaningful.
// C: qcommon/cmodel.c:1575 CM_ClusterPVS
func (s *State) ClusterPVS(cluster int) []byte {
	if cluster == -1 {
		row := (s.m.numclusters + 7) >> 3
		for i := 0; i < row; i++ {
			s.pvsrow[i] = 0
		}
	} else {
		s.m.decompressVis(s.m.bitofs(cluster, q2const.DVIS_PVS), s.pvsrow[:])
	}
	return s.pvsrow[:]
}

// ClusterPHS is ClusterPVS for the PHS.
// C: qcommon/cmodel.c:1584 CM_ClusterPHS
func (s *State) ClusterPHS(cluster int) []byte {
	if cluster == -1 {
		row := (s.m.numclusters + 7) >> 3
		for i := 0; i < row; i++ {
			s.phsrow[i] = 0
		}
	} else {
		s.m.decompressVis(s.m.bitofs(cluster, q2const.DVIS_PHS), s.phsrow[:])
	}
	return s.phsrow[:]
}

/*
===============================================================================

AREAPORTALS

===============================================================================
*/

// floodArea_r floods floodnum through open portals.
// C: qcommon/cmodel.c:1602 FloodArea_r
func (s *State) floodArea_r(area int32, floodnum int32) {
	if s.floodvalidA[area] == s.floodvalid {
		if s.floodnum[area] == floodnum {
			return
		}
		shared.Error(q2const.ERR_DROP, "FloodArea_r: reflooded")
	}

	s.floodnum[area] = floodnum
	s.floodvalidA[area] = s.floodvalid
	a := &s.m.areas[area]
	for i := int32(0); i < a.numareaportals; i++ {
		p := &s.m.areaportals[a.firstareaportal+i]
		if s.portalopen[p.PortalNum] {
			s.floodArea_r(p.OtherArea, floodnum)
		}
	}
}

// FloodAreaConnections recomputes the area flood numbers.
// C: qcommon/cmodel.c:1631 FloodAreaConnections
func (s *State) FloodAreaConnections() {
	// all current floods are now invalid
	s.floodvalid++
	var floodnum int32

	// area 0 is not used
	for i := 1; i < s.m.numareas; i++ {
		if s.floodvalidA[i] == s.floodvalid {
			continue // already flooded into
		}
		floodnum++
		s.floodArea_r(int32(i), floodnum)
	}
}

// SetAreaPortalState opens or closes a portal and refloods.
// C: qcommon/cmodel.c:1653 CM_SetAreaPortalState
func (s *State) SetAreaPortalState(portalnum int, open bool) {
	if portalnum > s.m.numareaportals {
		shared.Error(q2const.ERR_DROP, "areaportal > numareaportals")
	}
	if portalnum < 0 || portalnum >= q2const.MAX_MAP_AREAPORTALS {
		shared.Error(q2const.ERR_DROP, "areaportal out of range") // C: out-of-bounds write
	}
	s.portalopen[portalnum] = open
	s.FloodAreaConnections()
}

// AreasConnected reports whether the two areas share a flood.
// C: qcommon/cmodel.c:1662 CM_AreasConnected
func (s *State) AreasConnected(area1, area2 int) bool {
	if s.NoAreas {
		return true
	}
	if area1 > s.m.numareas || area2 > s.m.numareas {
		shared.Error(q2const.ERR_DROP, "area > numareas")
	}
	if area1 < 0 || area2 < 0 || area1 >= q2const.MAX_MAP_AREAS || area2 >= q2const.MAX_MAP_AREAS {
		shared.Error(q2const.ERR_DROP, "area out of range") // C: out-of-bounds read
	}
	return s.floodnum[area1] == s.floodnum[area2]
}

// WriteAreaBits writes a bit vector of all the areas that are in the same
// flood as area into buffer and returns the byte count.
// C: qcommon/cmodel.c:1686 CM_WriteAreaBits
func (s *State) WriteAreaBits(buffer []byte, area int) int {
	bytes := (s.m.numareas + 7) >> 3

	if s.NoAreas { // for debugging, send everything
		for i := 0; i < bytes; i++ {
			buffer[i] = 255
		}
	} else {
		for i := 0; i < bytes; i++ {
			buffer[i] = 0
		}
		if area < 0 || area >= q2const.MAX_MAP_AREAS {
			shared.Error(q2const.ERR_DROP, "CM_WriteAreaBits: bad area") // C: out-of-bounds read
		}
		floodnum := s.floodnum[area]
		for i := 0; i < s.m.numareas; i++ {
			if s.floodnum[i] == floodnum || area == 0 {
				buffer[i>>3] |= 1 << (i & 7)
			}
		}
	}
	return bytes
}

// WritePortalState returns the portal state as saved in a savegame
// (MAX_MAP_AREAPORTALS qboolean ints, native little-endian).
// C: qcommon/cmodel.c:1721 CM_WritePortalState
func (s *State) WritePortalState() []byte {
	out := make([]byte, 4*q2const.MAX_MAP_AREAPORTALS)
	for i, o := range s.portalopen {
		if o {
			out[i*4] = 1
		}
	}
	return out
}

// ReadPortalState restores the portal state and recalculates the area
// connections.
// C: qcommon/cmodel.c:1734 CM_ReadPortalState
func (s *State) ReadPortalState(data []byte) {
	for i := range s.portalopen {
		o := i * 4
		var v uint32
		if o+4 <= len(data) {
			v = uint32(data[o]) | uint32(data[o+1])<<8 | uint32(data[o+2])<<16 | uint32(data[o+3])<<24
		}
		s.portalopen[i] = v != 0
	}
	s.FloodAreaConnections()
}

// PortalOpen reports the state of one portal.
func (s *State) PortalOpen(portalnum int) bool { return s.portalopen[portalnum] }

// HeadnodeVisible returns true if any leaf under headnode has a cluster
// that is potentially visible.
// C: qcommon/cmodel.c:1748 CM_HeadnodeVisible
func (s *State) HeadnodeVisible(nodenum int32, visbits []byte) bool {
	if nodenum < 0 {
		leafnum := -1 - nodenum
		cluster := s.m.leafs[leafnum].cluster
		if cluster == -1 {
			return false
		}
		// memory-safety fix: other negative clusters (malformed map) would
		// index before visbits in C; not visible
		if cluster >= 0 && int(cluster>>3) < len(visbits) && visbits[cluster>>3]&(1<<(cluster&7)) != 0 {
			return true
		}
		return false
	}

	node := &s.m.nodes[nodenum]
	if s.HeadnodeVisible(node.children[0], visbits) {
		return true
	}
	return s.HeadnodeVisible(node.children[1], visbits)
}
