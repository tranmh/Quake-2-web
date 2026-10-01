package navbuild

import (
	"math"
	"sort"

	"quake2web/server/internal/agent/nav"
	"quake2web/server/internal/agent/nav/navsim"
	"quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/shared"
)

// node priorities: lower wins a dedupe cell and sorts first
const (
	prioSpawn uint8 = iota
	prioGround
	prioLadder
	prioWater
	prioMover
	prioEnd
)

type bnode struct {
	o       Vec3
	flags   nav.NodeFlags
	blocker int32
	pose    int8
	prio    uint8
	// local is the origin relative to the pose origin (mover nodes)
	local Vec3
	// ladderYaw faces the ladder (ladder nodes); ladder groups the nodes of
	// one ladder face
	ladderYaw float32
	ladder    int32
	// ledge has bit i set when direction i*45° drops off
	ledge uint8
	// spawn is the scene spawn index (spawn nodes), else -1
	spawn int
}

func (n *bnode) sup() []support {
	if n.blocker < 0 {
		return nil
	}
	return []support{{n.blocker, n.pose}}
}

// settle offsets tried around a sample: the point itself, then 8 units
// around it (so a grid point a hull's width from a wall still yields a node
// in a corridor whose walls are not on the grid)
func settleOffsets() [9][2]float32 {
	return [9][2]float32{{0, 0}, {8, 0}, {-8, 0}, {0, 8}, {0, -8}, {8, 8}, {-8, 8}, {8, -8}, {-8, -8}}
}

// settle turns a floor point into a standing node: a hull trace from 42
// above down to 6 above (standing hull, else ducked), a walkable plane, one
// idle pmove step that ends on ground. With want > 0 the ground must be
// that solid.
func (wk *worker) settle(p Vec3, want int) (bnode, bool) {
	mins, smax, dmax := navsim.StandMins(), navsim.StandMaxs(), navsim.DuckMaxs()
	for _, off := range settleOffsets() {
		start := Vec3{p[0] + off[0], p[1] + off[1], p[2] + 42}
		end := Vec3{start[0], start[1], p[2] + 6}
		ducked := false
		tr := wk.w.Trace(start, mins, smax, end, q2const.MASK_PLAYERSOLID)
		if tr.StartSolid || tr.AllSolid {
			tr = wk.w.Trace(start, mins, dmax, end, q2const.MASK_PLAYERSOLID)
			if tr.StartSolid || tr.AllSolid {
				continue
			}
			ducked = true
		}
		if tr.Fraction >= 1 || tr.Plane.Normal[2] < 0.7 || (want > 0 && tr.Ent != want) {
			continue
		}
		if ducked && wk.w.Fits(tr.EndPos, mins, smax) {
			ducked = false
		}
		r := wk.r
		r.Reset(tr.EndPos, ducked)
		c := navsim.Cmd{}
		if ducked {
			c.Up = -400
		}
		r.Step(c)
		st := r.StatePtr()
		if !st.OnGround() || (want > 0 && st.Ground != want) {
			continue
		}
		o := st.Origin()
		if dist3(o, tr.EndPos) > 4 {
			continue // slid away
		}
		n := bnode{o: o, blocker: -1, ladder: -1, spawn: -1, prio: prioGround}
		if st.Ducked() {
			n.flags |= nav.NodeCrouch
		}
		if st.WaterLevel >= 2 { // standing in deep water: pmove swims
			n.flags |= nav.NodeWater
			if st.WaterLevel < 3 {
				n.flags |= nav.NodeBreath
			}
		}
		return n, true
	}
	return bnode{}, false
}

func dist3(a, b Vec3) float32 {
	dx, dy, dz := a[0]-b[0], a[1]-b[1], a[2]-b[2]
	return float32(math.Sqrt(float64(dx*dx + dy*dy + dz*dz)))
}

func hdist(a, b Vec3) float32 {
	dx, dy := a[0]-b[0], a[1]-b[1]
	return float32(math.Sqrt(float64(dx*dx + dy*dy)))
}

// faceJob is one upward face to sample: world brushes (offset zero), static
// brush solids (offset their origin) or mover tops (local coordinates,
// sampled for every pose).
type faceJob struct {
	fc      face
	offset  Vec3
	blocker int32
}

func (b *builder) upFaces(headnode int32, mask int32) []face {
	f := b.geo.f
	var out []face
	for _, br := range modelBrushes(f, headnode) {
		if f.Brushes[br].Contents&mask == 0 {
			continue
		}
		out = append(out, brushFaces(f, br, func(p dplane) bool { return p.n[2] >= 0.7 }, 1)...)
	}
	return out
}

func (b *builder) buildNodes() (int, error) {
	sc := b.sc
	var jobs []faceJob
	for _, fc := range b.upFaces(b.geo.f.Models[0].Headnode, q2const.MASK_PLAYERSOLID) {
		jobs = append(jobs, faceJob{fc: fc, blocker: -1})
	}
	for _, s := range sc.statics {
		if s.Box || s.Angles != (Vec3{}) {
			continue
		}
		for _, fc := range b.upFaces(s.Headnode, q2const.MASK_PLAYERSOLID) {
			jobs = append(jobs, faceJob{fc: fc, offset: s.Origin, blocker: -1})
		}
	}
	for bi := range sc.blockers {
		if !sc.geo[bi].tops {
			continue
		}
		for _, fc := range b.upFaces(sc.geo[bi].headnode, q2const.MASK_PLAYERSOLID) {
			jobs = append(jobs, faceJob{fc: fc, blocker: int32(bi)})
		}
	}
	if c := b.cfg.Clip; c != nil {
		// sample only faces near the clip box (mover tops may move into it)
		kept := jobs[:0]
		for _, j := range jobs {
			mn, mx := j.fc.w.bounds()
			near := true
			for k := 0; k < 3; k++ {
				lo, hi := mn[k]+float64(j.offset[k]), mx[k]+float64(j.offset[k])
				if j.blocker < 0 && (hi < float64(c[0][k])-64 || lo > float64(c[1][k])+64) {
					near = false
				}
			}
			if near {
				kept = append(kept, j)
			}
		}
		jobs = kept
	}
	g := float64(b.p.Grid)
	results := make([][]bnode, len(jobs))
	err := b.parallel(len(jobs), func(wk *worker, i int) {
		j := &jobs[i]
		pts := gridPoints(j.fc.w, g, 0, 0)
		if len(pts) == 0 || j.fc.w.area() < g*g {
			c := j.fc.w.centroid()
			pts = append(pts, [2]float64{c[0], c[1]})
		}
		if j.blocker < 0 {
			wk.setWorld()
			for _, pt := range pts {
				p := Vec3{float32(pt[0]) + j.offset[0], float32(pt[1]) + j.offset[1], float32(j.fc.plane.zAt(pt[0], pt[1])) + j.offset[2]}
				if n, ok := wk.settle(p, 0); ok {
					results[i] = append(results[i], n)
				}
			}
			return
		}
		bl := &b.sc.blockers[j.blocker]
		id := b.sc.geo[j.blocker].solidID
		for k, pose := range bl.Poses {
			wk.setWorld(support{j.blocker, int8(k)})
			for _, pt := range pts {
				p := Vec3{float32(pt[0]) + pose.Origin[0], float32(pt[1]) + pose.Origin[1], float32(j.fc.plane.zAt(pt[0], pt[1])) + pose.Origin[2]}
				if n, ok := wk.settle(p, id); ok {
					n.flags |= nav.NodeMover
					n.blocker, n.pose, n.prio = j.blocker, int8(k), prioMover
					n.local = shared.VectorSubtract(n.o, pose.Origin)
					results[i] = append(results[i], n)
				}
			}
		}
	})
	if err != nil {
		return 0, err
	}
	var all []bnode
	for _, r := range results {
		all = append(all, r...)
	}
	all = append(all, b.spawnNodes()...)
	water, err := b.waterNodes()
	if err != nil {
		return 0, err
	}
	all = append(all, water...)
	all = append(all, b.ladderNodes()...)
	all = dedupe(b.clip(b.dropVoid(all)))
	if all, err = b.dropBlocked(all); err != nil {
		return 0, err
	}
	b.setNodes(all)
	return len(b.nodes), nil
}

// dropBlocked removes nodes that a blocker overlaps in every one of its
// states (a spot inside a sliding door's panel both closed and open): no
// edge from or to them can ever hold. A mover node's own mover is exempt.
func (b *builder) dropBlocked(all []bnode) ([]bnode, error) {
	blocked := make([]bool, len(all))
	err := b.parallel(len(all), func(wk *worker, i int) {
		n := &all[i]
		s := []navsim.Sample{{Origin: n.o, Ducked: n.crouch(), OnGround: true}}
		mins, maxs := sampleHull(&s[0], 0)
		lo, hi := shared.VectorAdd(n.o, mins), shared.VectorAdd(n.o, maxs)
		for bi := range b.sc.geo {
			g, bl := &b.sc.geo[bi], &b.sc.blockers[bi]
			if bl.Gone || int32(bi) == n.blocker || !g.solid || !boxesOverlap(lo, hi, g.umin, g.umax) {
				continue
			}
			all := true
			for k := range bl.Poses {
				if !wk.hitsPose(bi, k, s) {
					all = false
					break
				}
			}
			if all {
				blocked[i] = true
				return
			}
		}
	})
	if err != nil {
		return nil, err
	}
	out := all[:0]
	for i, n := range all {
		if !blocked[i] || n.prio == prioSpawn {
			out = append(out, n)
		}
	}
	return out, nil
}

// clip drops the nodes outside Config.Clip.
func (b *builder) clip(all []bnode) []bnode {
	c := b.cfg.Clip
	if c == nil {
		return all
	}
	out := all[:0]
	for _, n := range all {
		in := true
		for k := 0; k < 3; k++ {
			if n.o[k] < c[0][k] || n.o[k] > c[1][k] {
				in = false
			}
		}
		if in {
			out = append(out, n)
		}
	}
	return out
}

// dropVoid removes nodes outside the sealed level: their origin is in a
// leaf without a vis cluster (the tops of the outer brushes). Maps without
// vis keep everything.
func (b *builder) dropVoid(all []bnode) []bnode {
	cm := b.geo.cm
	if cm.NumClusters() == 0 {
		return all
	}
	cs := b.workers[0].w.State()
	out := all[:0]
	for _, n := range all {
		if n.prio == prioSpawn || cm.LeafCluster(int(cs.PointLeafnum(n.o))) >= 0 {
			out = append(out, n)
		}
	}
	return out
}

// dedupe sorts the candidates and keeps the first of each 16x16x8 cell
// (per mover pose); spawn nodes are always kept.
func dedupe(all []bnode) []bnode {
	sort.Slice(all, func(i, j int) bool { return lessNode(&all[i], &all[j]) })
	type key struct {
		b       int32
		p       int8
		x, y, z int32
	}
	seen := map[key]bool{}
	out := all[:0]
	for _, n := range all {
		k := key{n.blocker, n.pose, int32(math.Floor(float64(n.o[0]) / 16)), int32(math.Floor(float64(n.o[1]) / 16)), int32(math.Floor(float64(n.o[2]) / 8))}
		if seen[k] && n.prio != prioSpawn {
			continue
		}
		seen[k] = true
		out = append(out, n)
	}
	return out
}

func lessNode(a, b *bnode) bool {
	if a.prio != b.prio {
		return a.prio < b.prio
	}
	if a.blocker != b.blocker {
		return a.blocker < b.blocker
	}
	if a.pose != b.pose {
		return a.pose < b.pose
	}
	if a.spawn != b.spawn {
		return a.spawn < b.spawn
	}
	for k := 2; k >= 0; k-- {
		if a.o[k] != b.o[k] {
			return a.o[k] < b.o[k]
		}
	}
	return a.flags < b.flags
}

// setNodes installs the node list and its spatial index (32-unit cells).
func (b *builder) setNodes(nodes []bnode) {
	b.nodes = nodes
	b.cells = map[uint64][]int32{}
	for i := range nodes {
		k := cell32(nodes[i].o[0], nodes[i].o[1])
		b.cells[k] = append(b.cells[k], int32(i))
	}
}

func cellIdx(v float32) int32 { return int32(math.Floor(float64(v) / 32)) }

func cell32(x, y float32) uint64 { return uint64(uint32(cellIdx(x)))<<32 | uint64(uint32(cellIdx(y))) }

// near calls fn for every node within r horizontally of p (cell order).
func (b *builder) near(p Vec3, r float32, fn func(i int32)) {
	r2 := r * r
	for cx := cellIdx(p[0] - r); cx <= cellIdx(p[0]+r); cx++ {
		for cy := cellIdx(p[1] - r); cy <= cellIdx(p[1]+r); cy++ {
			for _, i := range b.cells[uint64(uint32(cx))<<32|uint64(uint32(cy))] {
				o := b.nodes[i].o
				dx, dy := o[0]-p[0], o[1]-p[1]
				if dx*dx+dy*dy <= r2 {
					fn(i)
				}
			}
		}
	}
}

// spawnNodes settles every spawn point like PutClientInServer (origin + 9
// + 1, then pmove) with the movers in their spawn state.
func (b *builder) spawnNodes() []bnode {
	wk := b.workers[0]
	sol := append(append([]navsim.Solid(nil), b.sc.statics...), b.sc.spawnSolids()...)
	wk.w.SetSolids(sol)
	defer wk.setWorld()
	var out []bnode
	for i, s := range b.sc.spawns {
		o := s.origin
		o[2] += 10
		if !wk.r.Settle(o, false, 3000) {
			continue
		}
		st := wk.r.StatePtr()
		n := bnode{o: st.Origin(), flags: nav.NodeSpawn, blocker: -1, ladder: -1, spawn: i, prio: prioSpawn}
		if bi, ok := b.sc.blockerByID[st.Ground]; ok {
			bl := &b.sc.blockers[bi]
			n.flags |= nav.NodeMover
			n.blocker, n.pose = bi, bl.Spawn
			n.local = shared.VectorSubtract(n.o, bl.Poses[bl.Spawn].Origin)
		}
		out = append(out, n)
	}
	return out
}

// waterNodes samples the water brushes on a WaterGrid lattice: positions
// where the hull fits and the player swims (water level >= 2, not standing).
func (b *builder) waterNodes() ([]bnode, error) {
	f := b.geo.f
	var brushes []int
	for _, br := range modelBrushes(f, f.Models[0].Headnode) {
		c := f.Brushes[br].Contents
		if c&q2const.CONTENTS_WATER != 0 && c&(q2const.CONTENTS_LAVA|q2const.CONTENTS_SLIME) == 0 {
			brushes = append(brushes, br)
		}
	}
	g := float64(b.p.WaterGrid)
	results := make([][]bnode, len(brushes))
	err := b.parallel(len(brushes), func(wk *worker, i int) {
		faces := brushFaces(f, brushes[i], func(dplane) bool { return true }, 0)
		if len(faces) == 0 {
			return
		}
		mn, mx := faces[0].w.bounds()
		for _, fc := range faces[1:] {
			a, c := fc.w.bounds()
			for k := 0; k < 3; k++ {
				mn[k], mx[k] = math.Min(mn[k], a[k]), math.Max(mx[k], c[k])
			}
		}
		wk.setWorld()
		zs := []float64{mx[2] - 8}
		for z := math.Ceil((mn[2]+24)/g) * g; z < mx[2]-8-g/2; z += g {
			zs = append(zs, z)
		}
		for x := math.Ceil(mn[0]/g) * g; x <= mx[0]; x += g {
			for y := math.Ceil(mn[1]/g) * g; y <= mx[1]; y += g {
				for _, z := range zs {
					p := Vec3{float32(x), float32(y), float32(z)}
					if !wk.w.Fits(p, navsim.StandMins(), navsim.StandMaxs()) {
						continue
					}
					st := navsim.State{Mins: navsim.StandMins(), Maxs: navsim.StandMaxs(), ViewHeight: 22}
					st.PM.Origin = navsim.SnapOrigin(p)
					wk.w.Categorize(&st)
					if st.WaterLevel < 2 || st.OnGround() || st.WaterType&q2const.CONTENTS_WATER == 0 {
						continue
					}
					n := bnode{o: st.Origin(), flags: nav.NodeWater, blocker: -1, ladder: -1, spawn: -1, prio: prioWater}
					if st.WaterLevel < 3 {
						n.flags |= nav.NodeBreath
					}
					results[i] = append(results[i], n)
				}
			}
		}
	})
	var out []bnode
	for _, r := range results {
		out = append(out, r...)
	}
	return out, err
}

// ladderNodes places hanging positions in front of the vertical sides of
// ladder brushes, every 32 units of height, where the hull fits and pmove's
// ladder test (a 1-unit forward trace hitting CONTENTS_LADDER) succeeds.
// Ladder brushes are in the world model or in static brush entities (the
// demo2 and demo3 ladders are func_walls); the trace reports a static
// solid's brush contents like the world's, so pmove climbs both.
func (b *builder) ladderNodes() []bnode {
	f := b.geo.f
	wk := b.workers[0]
	wk.setWorld()
	type source struct {
		headnode int32
		offset   Vec3
	}
	srcs := []source{{headnode: f.Models[0].Headnode}}
	for _, s := range b.sc.statics {
		if !s.Box && s.Angles == (Vec3{}) {
			srcs = append(srcs, source{s.Headnode, s.Origin})
		}
	}
	var out []bnode
	ladder := int32(0)
	for _, src := range srcs {
		for _, br := range modelBrushes(f, src.headnode) {
			if f.Brushes[br].Contents&q2const.CONTENTS_LADDER == 0 {
				continue
			}
			for _, fc := range brushFaces(f, br, func(p dplane) bool { return math.Abs(p.n[2]) < 0.1 }, 16) {
				n := fc.plane.n
				c := fc.w.centroid()
				mn, mx := fc.w.bounds()
				off := 16*(math.Abs(n[0])+math.Abs(n[1])) + 0.5
				yaw, _ := navsim.YawTo(Vec3{float32(n[0]), float32(n[1]), 0}, Vec3{})
				o := src.offset
				found := false
				for z := mn[2] + 24 + 8; z <= mx[2]+24; z += 32 {
					p := Vec3{float32(c[0]+n[0]*off) + o[0], float32(c[1]+n[1]*off) + o[1], float32(z) + o[2]}
					if !wk.w.Fits(p, navsim.StandMins(), navsim.StandMaxs()) {
						continue
					}
					st := navsim.State{Mins: navsim.StandMins(), Maxs: navsim.StandMaxs(), ViewHeight: 22}
					st.PM.Origin = navsim.SnapOrigin(p)
					if !wk.w.OnLadder(&st, yaw) {
						continue
					}
					out = append(out, bnode{o: st.Origin(), flags: nav.NodeLadder, blocker: -1, spawn: -1, prio: prioLadder, ladderYaw: yaw, ladder: ladder})
					found = true
				}
				if found {
					ladder++
				}
			}
		}
	}
	return out
}

// findLedges marks, for every standing node, the directions in which the
// floor drops away within 32 units (no walkable neighbor there, the hull
// can move there, and there is no floor within 24 units below).
func (b *builder) findLedges() (int, error) {
	marks := make([]uint8, len(b.nodes))
	err := b.parallel(len(b.nodes), func(wk *worker, i int) {
		n := &b.nodes[i]
		if n.flags&(nav.NodeWater|nav.NodeLadder) != 0 {
			return
		}
		var have uint8
		b.near(n.o, 56, func(j int32) {
			m := &b.nodes[j]
			if j == int32(i) || abs32(m.o[2]-n.o[2]) > 24 || m.flags&(nav.NodeWater|nav.NodeLadder) != 0 {
				return
			}
			if y, ok := navsim.YawTo(n.o, m.o); ok {
				have |= 1 << (uint(math.Floor(float64(y)/45+0.5)) % 8)
			}
		})
		wk.setWorld(n.sup()...)
		maxs := navsim.StandMaxs()
		if n.flags&nav.NodeCrouch != 0 {
			maxs = navsim.DuckMaxs()
		}
		for d := 0; d < 8; d++ {
			if have&(1<<uint(d)) != 0 {
				continue
			}
			a := float64(d) * math.Pi / 4
			p := Vec3{n.o[0] + float32(32*math.Cos(a)), n.o[1] + float32(32*math.Sin(a)), n.o[2]}
			if tr := wk.w.Trace(n.o, navsim.StandMins(), maxs, p, q2const.MASK_PLAYERSOLID); tr.Fraction < 1 || tr.StartSolid {
				continue
			}
			down := p
			down[2] -= 24
			if tr := wk.w.Trace(p, navsim.StandMins(), maxs, down, q2const.MASK_PLAYERSOLID); tr.Fraction < 1 {
				continue
			}
			marks[i] |= 1 << uint(d)
		}
	})
	if err != nil {
		return 0, err
	}
	count := 0
	for i := range b.nodes {
		b.nodes[i].ledge = marks[i]
		if marks[i] != 0 {
			b.nodes[i].flags |= nav.NodeLedge
			count++
		}
	}
	return count, nil
}
