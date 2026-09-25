package world

import (
	"testing"

	"quake2web/server/internal/assets/pak"
	"quake2web/server/internal/cmodel"
	"quake2web/server/internal/game"
	"quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/shared"
	"quake2web/server/internal/testutil"
)

type fixture struct {
	w      *World
	models [q2const.MAX_MODELS]*shared.CModel
	edicts []game.Edict
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	pk, err := pak.Open(testutil.DemoPak(t))
	if err != nil {
		t.Fatal(err)
	}
	defer pk.Close()
	raw, err := pk.ReadFile("maps/demo1.bsp")
	if err != nil {
		t.Fatal(err)
	}
	m, err := cmodel.LoadMapBytes("maps/demo1.bsp", raw)
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{edicts: make([]game.Edict, 64)}
	for i := range f.edicts {
		f.edicts[i].Index = i
	}
	f.models[1] = m.WorldModel()
	for i := 1; i < m.NumInlineModels(); i++ {
		f.models[i+1] = m.InlineModel("*" + itoa(i))
	}
	f.w = &World{
		CM:         cmodel.NewState(m),
		Models:     &f.models,
		WorldEdict: func() *game.Edict { return &f.edicts[0] },
	}
	f.w.ClearWorld()
	return f
}

func itoa(i int) string {
	if i < 10 {
		return string(rune('0' + i))
	}
	return itoa(i/10) + string(rune('0'+i%10))
}

// a point known to be in the open on demo1 (info_player_start + 9)
var openSpot = shared.Vec3{128, -320, 33}

func (f *fixture) box(i int, org shared.Vec3, solid int32) *game.Edict {
	e := &f.edicts[i]
	e.InUse = true
	e.Solid = solid
	e.S.Origin = org
	e.Mins = shared.Vec3{-16, -16, -24}
	e.Maxs = shared.Vec3{16, 16, 32}
	f.w.LinkEdict(e)
	return e
}

func TestAreaNodes(t *testing.T) {
	f := newFixture(t)
	if f.w.numNodes != 31 {
		t.Fatalf("numareanodes = %d, want 31 (depth 4 binary tree)", f.w.numNodes)
	}
	// children order: [0] is the upper half (mins2 = dist)
	n := &f.w.nodes[0]
	c0, c1 := &f.w.nodes[n.children[0]], &f.w.nodes[n.children[1]]
	if n.children[0] != 1 || c0.axis == -1 || c1.axis == -1 {
		t.Fatalf("unexpected tree shape")
	}
	wm := f.models[1]
	size := shared.VectorSubtract(wm.Maxs, wm.Mins)
	wantAxis := 1
	if size[0] > size[1] {
		wantAxis = 0
	}
	if n.axis != wantAxis || n.dist != float32(0.5*float64(wm.Maxs[n.axis]+wm.Mins[n.axis])) {
		t.Fatalf("root axis %d dist %v", n.axis, n.dist)
	}
}

func TestLinkEdict(t *testing.T) {
	f := newFixture(t)
	e := f.box(1, openSpot, q2const.SOLID_BBOX)

	// i = 16/8 = 2, j = 24/8 = 3, k = (32+32)/8 = 8
	if want := int32(8<<10 | 3<<5 | 2); e.S.Solid != want {
		t.Errorf("s.solid = %d, want %d", e.S.Solid, want)
	}
	for i := 0; i < 3; i++ {
		if e.AbsMin[i] != openSpot[i]+e.Mins[i]-1 || e.AbsMax[i] != openSpot[i]+e.Maxs[i]+1 {
			t.Errorf("abs box %v %v", e.AbsMin, e.AbsMax)
		}
	}
	if e.Size != (shared.Vec3{32, 32, 56}) {
		t.Errorf("size %v", e.Size)
	}
	if e.NumClusters <= 0 || e.AreaNum == 0 {
		t.Errorf("clusters %d area %d", e.NumClusters, e.AreaNum)
	}
	if e.LinkCount != 1 || e.S.OldOrigin != openSpot {
		t.Errorf("linkcount %d oldorigin %v", e.LinkCount, e.S.OldOrigin)
	}
	// the clusters must be the distinct clusters of the leafs in the box
	var leafs [MAX_TOTAL_ENT_LEAFS]int32
	n := f.w.CM.BoxLeafnums(e.AbsMin, e.AbsMax, leafs[:], nil)
	seen := map[int32]bool{}
	for _, l := range leafs[:n] {
		if c := f.w.CM.Map().LeafCluster(int(l)); c != -1 {
			seen[c] = true
		}
	}
	if len(seen) != int(e.NumClusters) {
		t.Errorf("num_clusters %d, distinct clusters %d", e.NumClusters, len(seen))
	}

	// moving: old_origin is not touched after the first link
	e.S.Origin[0] += 8
	f.w.LinkEdict(e)
	if e.LinkCount != 2 || e.S.OldOrigin != openSpot {
		t.Errorf("relink: linkcount %d oldorigin %v", e.LinkCount, e.S.OldOrigin)
	}

	// the world itself is never linked, SOLID_BSP encodes 31, dead monsters 0
	f.edicts[0].InUse = true
	f.w.LinkEdict(&f.edicts[0])
	if f.edicts[0].Area.Prev != nil || f.edicts[0].LinkCount != 0 {
		t.Errorf("world was linked")
	}
	d := f.box(2, openSpot, q2const.SOLID_BBOX)
	d.SVFlags = q2const.SVF_DEADMONSTER
	f.w.LinkEdict(d)
	if d.S.Solid != 0 {
		t.Errorf("dead monster s.solid %d", d.S.Solid)
	}
	nl := &f.edicts[3]
	nl.InUse = true
	nl.Solid = q2const.SOLID_NOT
	nl.S.Origin = openSpot
	f.w.LinkEdict(nl)
	if nl.Area.Prev != nil || nl.LinkCount != 1 || nl.NumClusters <= 0 {
		t.Errorf("SOLID_NOT: linked=%v linkcount %d clusters %d", nl.Area.Prev != nil, nl.LinkCount, nl.NumClusters)
	}
}

func names(list []*game.Edict) []int {
	var out []int
	for _, e := range list {
		out = append(out, e.Index)
	}
	return out
}

func equal(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestAreaEdictsOrder(t *testing.T) {
	f := newFixture(t)
	f.box(1, openSpot, q2const.SOLID_BBOX)
	f.box(2, openSpot, q2const.SOLID_BBOX)
	f.box(3, openSpot, q2const.SOLID_BBOX)
	trig := f.box(4, openSpot, q2const.SOLID_TRIGGER)

	list := make([]*game.Edict, 16)
	mins := shared.VectorSubtract(openSpot, shared.Vec3{64, 64, 64})
	maxs := shared.VectorAdd(openSpot, shared.Vec3{64, 64, 64})
	n := f.w.AreaEdicts(mins, maxs, list, q2const.AREA_SOLID)
	if got := names(list[:n]); !equal(got, []int{1, 2, 3}) {
		t.Fatalf("solid list %v", got)
	}
	n = f.w.AreaEdicts(mins, maxs, list, q2const.AREA_TRIGGERS)
	if got := names(list[:n]); !equal(got, []int{4}) {
		t.Fatalf("trigger list %v", got)
	}
	// relinking moves an entity to the end of its node list (FIFO)
	f.w.LinkEdict(&f.edicts[1])
	n = f.w.AreaEdicts(mins, maxs, list, q2const.AREA_SOLID)
	if got := names(list[:n]); !equal(got, []int{2, 3, 1}) {
		t.Fatalf("after relink %v", got)
	}
	// maxcount
	n = f.w.AreaEdicts(mins, maxs, list[:2], q2const.AREA_SOLID)
	if n != 2 {
		t.Fatalf("maxcount: %d", n)
	}
	// unlink
	f.w.UnlinkEdict(&f.edicts[2])
	f.w.UnlinkEdict(&f.edicts[2]) // not linked: no-op
	n = f.w.AreaEdicts(mins, maxs, list, q2const.AREA_SOLID)
	if got := names(list[:n]); !equal(got, []int{3, 1}) {
		t.Fatalf("after unlink %v", got)
	}
	// a deactivated (SOLID_NOT) entity still linked is skipped
	f.edicts[3].Solid = q2const.SOLID_NOT
	n = f.w.AreaEdicts(mins, maxs, list, q2const.AREA_SOLID)
	if got := names(list[:n]); !equal(got, []int{1}) {
		t.Fatalf("SOLID_NOT skipped: %v", got)
	}
	// not touching
	far := shared.Vec3{openSpot[0] + 1000, openSpot[1], openSpot[2]}
	if n = f.w.AreaEdicts(far, far, list, q2const.AREA_SOLID); n != 0 {
		t.Fatalf("far box found %d", n)
	}
	_ = trig
}

func TestTraceBoxEntity(t *testing.T) {
	f := newFixture(t)
	target := shared.Vec3{openSpot[0] - 60, openSpot[1], openSpot[2]}
	box := f.box(5, target, q2const.SOLID_BBOX)

	start := openSpot
	end := shared.Vec3{openSpot[0] - 120, openSpot[1], openSpot[2]}
	tr := f.w.Trace(&start, nil, nil, &end, nil, q2const.MASK_SOLID|q2const.CONTENTS_MONSTER)
	if tr.Ent != box {
		t.Fatalf("trace hit %v (fraction %v), want box", tr.Ent, tr.Fraction)
	}
	// hits the +x face of the box at x = target+16
	if want := target[0] + 16; tr.EndPos[0] < want || tr.EndPos[0] > want+1 {
		t.Errorf("endpos %v", tr.EndPos)
	}
	if tr.Plane.Normal != (shared.Vec3{1, 0, 0}) {
		t.Errorf("plane normal %v", tr.Plane.Normal)
	}
	if tr.Contents&q2const.CONTENTS_MONSTER == 0 {
		t.Errorf("contents %x", tr.Contents)
	}

	// passedict and owner rules
	tr = f.w.Trace(&start, nil, nil, &end, box, q2const.MASK_SOLID|q2const.CONTENTS_MONSTER)
	if tr.Ent == box {
		t.Errorf("passedict was hit")
	}
	shooter := &f.edicts[6]
	box.Owner = shooter
	tr = f.w.Trace(&start, nil, nil, &end, shooter, q2const.MASK_SOLID|q2const.CONTENTS_MONSTER)
	if tr.Ent == box {
		t.Errorf("own missile was hit")
	}
	box.Owner = nil
	shooter.Owner = box
	tr = f.w.Trace(&start, nil, nil, &end, shooter, q2const.MASK_SOLID|q2const.CONTENTS_MONSTER)
	if tr.Ent == box {
		t.Errorf("owner was hit")
	}
	shooter.Owner = nil

	// dead monsters are only hit with CONTENTS_DEADMONSTER
	box.SVFlags = q2const.SVF_DEADMONSTER
	f.w.LinkEdict(box)
	tr = f.w.Trace(&start, nil, nil, &end, nil, q2const.MASK_SOLID|q2const.CONTENTS_MONSTER)
	if tr.Ent == box {
		t.Errorf("dead monster hit without CONTENTS_DEADMONSTER")
	}
	box.SVFlags = 0
	f.w.LinkEdict(box)

	// starting inside the box
	in := target
	tr = f.w.Trace(&in, nil, nil, &end, nil, q2const.MASK_SOLID|q2const.CONTENTS_MONSTER)
	if !tr.StartSolid || tr.Ent != box {
		t.Errorf("start in box: startsolid %v ent %v", tr.StartSolid, tr.Ent)
	}

	// point contents include the box
	if c := f.w.PointContents(target); c&q2const.CONTENTS_MONSTER == 0 {
		t.Errorf("point contents %x", c)
	}
	if c := f.w.PointContents(openSpot); c != 0 {
		t.Errorf("open contents %x", c)
	}

	// a trace blocked by the world at once returns the world
	wall := shared.Vec3{openSpot[0], openSpot[1], -10000}
	down := shared.Vec3{openSpot[0], openSpot[1], -20000}
	tr = f.w.Trace(&wall, nil, nil, &down, nil, q2const.MASK_SOLID)
	if tr.Ent != &f.edicts[0] {
		t.Errorf("world trace ent %v", tr.Ent)
	}
}

func TestTraceBounds(t *testing.T) {
	var bmin, bmax shared.Vec3
	TraceBounds(shared.Vec3{0, 10, 5}, shared.Vec3{-1, -2, -3}, shared.Vec3{1, 2, 3}, shared.Vec3{10, 0, 5}, &bmin, &bmax)
	if bmin != (shared.Vec3{-2, -3, 1}) || bmax != (shared.Vec3{12, 13, 9}) {
		t.Fatalf("bounds %v %v", bmin, bmax)
	}
}
