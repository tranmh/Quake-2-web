package cmodel

import (
	"errors"
	"math"
	"strconv"
	"strings"
	"testing"

	"quake2web/server/internal/assets/pak"
	"quake2web/server/internal/bsp"
	"quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/shared"
	"quake2web/server/internal/testutil"
)

func floorState(t testing.TB) *State {
	t.Helper()
	m, err := LoadMapBytes("synthetic", bsp.Encode(bsp.SyntheticFloorMap()))
	if err != nil {
		t.Fatal(err)
	}
	return NewState(m)
}

func expectComError(t *testing.T, name string, fn func()) {
	t.Helper()
	defer func() {
		r := recover()
		var ce shared.ComError
		if e, ok := r.(shared.ComError); ok {
			ce = e
		}
		if ce.Code != q2const.ERR_DROP {
			t.Errorf("%s: expected ERR_DROP ComError, got %v", name, r)
		}
	}()
	fn()
}

func TestSyntheticLoad(t *testing.T) {
	s := floorState(t)
	m := s.Map()
	c := m.Counts()
	if c.Planes != 12 || c.Nodes != 6 || c.Leafs != 3 || c.Brushes != 1 || c.Clusters != 1 || c.Areas != 2 {
		t.Fatalf("counts %+v", c)
	}
	w := m.WorldModel()
	if w.Mins != (Vec3{-65, -65, -17}) || w.Maxs != (Vec3{65, 65, 1}) {
		t.Errorf("world model bounds spread by a pixel: %v %v", w.Mins, w.Maxs)
	}
	if !strings.Contains(m.EntityString(), "info_player_start") {
		t.Error("entity string")
	}
	if m.LeafContents(2) != q2const.CONTENTS_SOLID || m.LeafCluster(1) != 0 || m.LeafArea(1) != 1 {
		t.Error("leaf accessors")
	}
	expectComError(t, "LeafContents", func() { m.LeafContents(3) })
	expectComError(t, "InlineModel", func() { m.InlineModel("*1") })
	expectComError(t, "InlineModel name", func() { m.InlineModel("1") })
}

func TestSyntheticPointQueries(t *testing.T) {
	s := floorState(t)
	for _, tc := range []struct {
		p    Vec3
		leaf int32
		cont int32
	}{
		{Vec3{0, 0, 8}, 1, 0},
		{Vec3{0, 0, -8}, 2, q2const.CONTENTS_SOLID},
		{Vec3{100, 0, -8}, 1, 0},
		{Vec3{0, 0, 0}, 1, 0}, // on the plane: d >= 0 is front
	} {
		if l := s.PointLeafnum(tc.p); l != tc.leaf {
			t.Errorf("PointLeafnum(%v) = %d, want %d", tc.p, l, tc.leaf)
		}
		if c := s.PointContents(tc.p, 0); c != tc.cont {
			t.Errorf("PointContents(%v) = %d, want %d", tc.p, c, tc.cont)
		}
	}
	// transformed: move the slab up by 100
	if c := s.TransformedPointContents(Vec3{0, 0, 92}, 0, Vec3{0, 0, 100}, Vec3{}); c != q2const.CONTENTS_SOLID {
		t.Errorf("TransformedPointContents = %d", c)
	}
	// rotated 90 degrees in yaw, still inside
	if c := s.TransformedPointContents(Vec3{30, 50, -8}, 0, Vec3{}, Vec3{0, 90, 0}); c != q2const.CONTENTS_SOLID {
		t.Errorf("rotated TransformedPointContents = %d", c)
	}
	var list [8]int32
	var top int32
	n := s.BoxLeafnums(Vec3{-8, -8, -8}, Vec3{8, 8, 8}, list[:], &top)
	if n != 2 || top != 0 {
		t.Errorf("BoxLeafnums = %d %v top %d", n, list[:n], top)
	}
	n = s.BoxLeafnums(Vec3{-8, -8, -8}, Vec3{8, 8, 8}, list[:1], nil)
	if n != 1 {
		t.Errorf("BoxLeafnums truncated = %d", n)
	}
}

func TestSyntheticTraces(t *testing.T) {
	s := floorState(t)
	// point trace straight down onto the slab
	tr := s.BoxTrace(Vec3{0, 0, 100}, Vec3{0, 0, -100}, Vec3{}, Vec3{}, 0, q2const.MASK_SOLID)
	wantFrac := float32((100 - 0.03125) / 200.0)
	if tr.Fraction != wantFrac || tr.Plane.Normal != (Vec3{0, 0, 1}) || tr.Contents != q2const.CONTENTS_SOLID {
		t.Fatalf("point trace: %+v", tr)
	}
	if tr.Surface == nil || tr.Surface.Name != "e1u1/floor1_3" || tr.StartSolid || tr.AllSolid || tr.Ent != -1 {
		t.Errorf("surface/flags: %+v %+v", tr, tr.Surface)
	}
	if math.Abs(float64(tr.EndPos[2]-0.03125)) > 1e-4 {
		t.Errorf("endpos %v", tr.EndPos)
	}
	// player box
	mins, maxs := Vec3{-16, -16, -24}, Vec3{16, 16, 32}
	tr = s.BoxTrace(Vec3{0, 0, 100}, Vec3{0, 0, -100}, mins, maxs, 0, q2const.MASK_PLAYERSOLID)
	if tr.Fraction >= 1 || math.Abs(float64(tr.EndPos[2]-24.03125)) > 1e-3 {
		t.Errorf("box trace: %+v", tr)
	}
	// miss
	tr = s.BoxTrace(Vec3{200, 0, 100}, Vec3{200, 0, -100}, mins, maxs, 0, q2const.MASK_PLAYERSOLID)
	if tr.Fraction != 1 || tr.EndPos != (Vec3{200, 0, -100}) || tr.Surface != s.Map().NullSurface() {
		t.Errorf("miss: %+v", tr)
	}
	// mask excludes solid
	tr = s.BoxTrace(Vec3{0, 0, 100}, Vec3{0, 0, -100}, Vec3{}, Vec3{}, 0, q2const.MASK_WATER)
	if tr.Fraction != 1 {
		t.Errorf("masked: %+v", tr)
	}
	// start inside
	tr = s.BoxTrace(Vec3{0, 0, -8}, Vec3{0, 0, 100}, Vec3{}, Vec3{}, 0, q2const.MASK_SOLID)
	if !tr.StartSolid || tr.AllSolid {
		t.Errorf("startsolid: %+v", tr)
	}
	// position test
	tr = s.BoxTrace(Vec3{0, 0, -8}, Vec3{0, 0, -8}, Vec3{}, Vec3{}, 0, q2const.MASK_SOLID)
	if !tr.StartSolid || !tr.AllSolid || tr.Fraction != 0 || tr.EndPos != (Vec3{0, 0, -8}) {
		t.Errorf("position test solid: %+v", tr)
	}
	tr = s.BoxTrace(Vec3{0, 0, 40}, Vec3{0, 0, 40}, mins, maxs, 0, q2const.MASK_SOLID)
	if tr.StartSolid || tr.Fraction != 1 {
		t.Errorf("position test clear: %+v", tr)
	}
}

func TestBoxHull(t *testing.T) {
	s := floorState(t)
	h := s.HeadnodeForBox(Vec3{-10, -10, -10}, Vec3{10, 10, 10})
	if h != 6 {
		t.Fatalf("box headnode %d", h)
	}
	tr := s.BoxTrace(Vec3{-100, 0, 0}, Vec3{100, 0, 0}, Vec3{}, Vec3{}, h, q2const.MASK_PLAYERSOLID)
	if tr.Fraction >= 1 || tr.Contents != q2const.CONTENTS_MONSTER || tr.Plane.Normal != (Vec3{-1, 0, 0}) {
		t.Fatalf("box trace %+v", tr)
	}
	if math.Abs(float64(tr.EndPos[0]+10.03125)) > 1e-3 {
		t.Errorf("endpos %v", tr.EndPos)
	}
	if c := s.PointContents(Vec3{0, 0, 0}, h); c != q2const.CONTENTS_MONSTER {
		t.Errorf("box contents %d", c)
	}
	if c := s.PointContents(Vec3{20, 0, 0}, h); c != 0 {
		t.Errorf("outside box contents %d", c)
	}
	// transformed trace against a box ignores rotation
	tr = s.TransformedBoxTrace(Vec3{-100, 50, 0}, Vec3{100, 50, 0}, Vec3{}, Vec3{}, h, q2const.MASK_PLAYERSOLID, Vec3{0, 50, 0}, Vec3{0, 45, 0})
	if tr.Fraction >= 1 || math.Abs(float64(tr.EndPos[0]+10.03125)) > 1e-3 || tr.EndPos[1] != 50 {
		t.Errorf("transformed box %+v", tr)
	}
}

func TestTransformedRotated(t *testing.T) {
	s := floorState(t)
	// the slab rotated 90 degrees in roll becomes a wall: x,z in [-64,64], y in [-16,0]... check that a
	// rotated trace hits something and the plane normal is rotated back into world space.
	tr := s.TransformedBoxTrace(Vec3{0, 100, 0}, Vec3{0, -100, 0}, Vec3{}, Vec3{}, 0, q2const.MASK_SOLID, Vec3{}, Vec3{0, 0, 90})
	if tr.Fraction >= 1 {
		t.Fatalf("rotated trace missed: %+v", tr)
	}
	if math.Abs(float64(tr.Plane.Normal[1])) < 0.99 {
		t.Errorf("normal not rotated into world space: %v", tr.Plane.Normal)
	}
}

func TestVisAndAreas(t *testing.T) {
	s := floorState(t)
	if row := s.ClusterPVS(0); row[0] != 1 {
		t.Errorf("pvs %x", row[0])
	}
	if row := s.ClusterPHS(0); row[0] != 1 {
		t.Errorf("phs %x", row[0])
	}
	if row := s.ClusterPVS(-1); row[0] != 0 {
		t.Errorf("pvs -1 %x", row[0])
	}
	if !s.HeadnodeVisible(0, []byte{1}) || s.HeadnodeVisible(0, []byte{0}) {
		t.Error("HeadnodeVisible")
	}
	if !s.AreasConnected(1, 1) || s.AreasConnected(0, 1) {
		t.Error("AreasConnected")
	}
	s.NoAreas = true
	if !s.AreasConnected(0, 1) {
		t.Error("map_noareas")
	}
	s.NoAreas = false
	var buf [4]byte
	if n := s.WriteAreaBits(buf[:], 1); n != 1 || buf[0] != 0x02 {
		t.Errorf("WriteAreaBits(1) = %d %x", n, buf[0])
	}
	if n := s.WriteAreaBits(buf[:], 0); n != 1 || buf[0] != 0x03 {
		t.Errorf("WriteAreaBits(0) = %d %x", n, buf[0])
	}
	expectComError(t, "AreasConnected", func() { s.AreasConnected(3, 1) })
	expectComError(t, "SetAreaPortalState", func() { s.SetAreaPortalState(1, true) })
}

// portalMap builds three areas joined by two portals on top of the floor map.
func portalMap(t *testing.T) *State {
	f := bsp.SyntheticFloorMap()
	f.Areas = []bsp.DArea{{}, {NumAreaPortals: 1, FirstAreaPortal: 0}, {NumAreaPortals: 2, FirstAreaPortal: 1}, {NumAreaPortals: 1, FirstAreaPortal: 3}}
	f.AreaPortals = []bsp.DAreaPortal{{PortalNum: 1, OtherArea: 2}, {PortalNum: 1, OtherArea: 1}, {PortalNum: 2, OtherArea: 3}, {PortalNum: 2, OtherArea: 2}}
	m, err := LoadMapBytes("portals", bsp.Encode(f))
	if err != nil {
		t.Fatal(err)
	}
	return NewState(m)
}

func TestAreaPortals(t *testing.T) {
	s := portalMap(t)
	if s.AreasConnected(1, 2) || s.AreasConnected(2, 3) {
		t.Fatal("portals start closed")
	}
	s.SetAreaPortalState(1, true)
	if !s.AreasConnected(1, 2) || s.AreasConnected(1, 3) {
		t.Fatal("portal 1 open")
	}
	s.SetAreaPortalState(2, true)
	if !s.AreasConnected(1, 3) {
		t.Fatal("portal 2 open")
	}
	var buf [1]byte
	s.WriteAreaBits(buf[:], 3)
	if buf[0] != 0x0e {
		t.Errorf("area bits %x", buf[0])
	}
	saved := s.WritePortalState()
	s.SetAreaPortalState(1, false)
	if s.AreasConnected(1, 3) {
		t.Fatal("portal 1 closed")
	}
	s.ReadPortalState(saved)
	if !s.AreasConnected(1, 3) || !s.PortalOpen(1) {
		t.Fatal("ReadPortalState")
	}
	s.ResetPortals()
	if s.AreasConnected(1, 2) {
		t.Fatal("ResetPortals")
	}
}

func TestLoadErrors(t *testing.T) {
	cases := map[string]func(f *bsp.File){
		"leaf0":       func(f *bsp.File) { f.Leafs[0].Contents = 0 },
		"noempty":     func(f *bsp.File) { f.Leafs[1].Contents = 1 },
		"nodeplane":   func(f *bsp.File) { f.Nodes[0].PlaneNum = 99 },
		"nodechild":   func(f *bsp.File) { f.Nodes[0].Children[0] = -50 },
		"leafbrush":   func(f *bsp.File) { f.LeafBrushes[0] = 7 },
		"brushside":   func(f *bsp.File) { f.Brushes[0].NumSides = 60 },
		"sideplane":   func(f *bsp.File) { f.BrushSides[0].PlaneNum = 0x8000 },
		"sidetexinfo": func(f *bsp.File) { f.BrushSides[0].TexInfo = 5 },
		"nomodels":    func(f *bsp.File) { f.Models = nil },
		"headnode":    func(f *bsp.File) { f.Models[0].Headnode = 40 },
		"areaportals": func(f *bsp.File) { f.AreaPortals = make([]bsp.DAreaPortal, q2const.MAX_MAP_AREAS+1) },
		"arearange":   func(f *bsp.File) { f.Areas[1].NumAreaPortals = 3 },
	}
	for name, mut := range cases {
		f := bsp.SyntheticFloorMap()
		mut(f)
		if _, err := LoadMapBytes(name, bsp.Encode(f)); err == nil {
			t.Errorf("%s: expected load error", name)
		}
	}
}

func TestEmptyMap(t *testing.T) {
	s := NewState(EmptyMap())
	tr := s.BoxTrace(Vec3{}, Vec3{0, 0, 10}, Vec3{}, Vec3{}, 0, -1)
	if tr.Fraction != 1 || s.PointContents(Vec3{}, 0) != 0 || s.PointLeafnum(Vec3{}) != 0 || s.Map().NumClusters() != 1 {
		t.Errorf("empty map: %+v", tr)
	}
}

// ---- demo pak based tests ----

func loadDemo(t testing.TB, name string) (*State, []byte) {
	t.Helper()
	p, err := pak.Open(testutil.DemoPak(t))
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	raw, err := p.ReadFile("maps/" + name + ".bsp")
	if errors.Is(err, pak.ErrNotFound) {
		t.Skipf("no %s", name)
	}
	if err != nil {
		t.Fatal(err)
	}
	m, err := LoadMapBytes("maps/"+name+".bsp", raw)
	if err != nil {
		t.Fatal(err)
	}
	return NewState(m), raw
}

// entityOrigins returns the origins of all entities of classname.
func entityOrigins(ents, classname string) []Vec3 {
	var out []Vec3
	data := ents
	for {
		tok, rest, more := shared.COM_Parse(data)
		data = rest
		if !more {
			return out
		}
		if tok != "{" {
			continue
		}
		kv := map[string]string{}
		for {
			k, rest, more := shared.COM_Parse(data)
			data = rest
			if !more || k == "}" {
				break
			}
			v, rest, _ := shared.COM_Parse(data)
			data = rest
			kv[k] = v
		}
		if kv["classname"] == classname {
			var o Vec3
			for i, f := range strings.Fields(kv["origin"]) {
				if i < 3 {
					x, _ := strconv.ParseFloat(f, 32)
					o[i] = float32(x)
				}
			}
			out = append(out, o)
		}
	}
}

func TestDemo1(t *testing.T) {
	s, _ := loadDemo(t, "demo1")
	m := s.Map()
	t.Logf("demo1 checksum %d counts %+v", m.Checksum, m.Counts())
	starts := entityOrigins(m.EntityString(), "info_player_start")
	if len(starts) == 0 {
		t.Fatal("no info_player_start")
	}
	mins, maxs := Vec3{-16, -16, -24}, Vec3{16, 16, 32}
	for _, st := range starts {
		if c := s.PointContents(st, 0); c&q2const.CONTENTS_SOLID != 0 {
			t.Errorf("start %v in solid (%d)", st, c)
		}
		tr := s.BoxTrace(st, st, mins, maxs, 0, q2const.MASK_PLAYERSOLID)
		if tr.StartSolid {
			t.Errorf("player box at start %v is in solid", st)
		}
		down := st
		down[2] -= 1000
		tr = s.BoxTrace(st, down, mins, maxs, 0, q2const.MASK_PLAYERSOLID)
		if tr.Fraction >= 1 || tr.Plane.Normal[2] < 0.7 || tr.StartSolid {
			t.Errorf("trace down from %v: %+v", st, tr)
		}
		t.Logf("start %v floor at %v surface %q", st, tr.EndPos, tr.Surface.Name)
	}
	// PVS of the start cluster contains itself
	leaf := s.PointLeafnum(starts[0])
	cl := m.LeafCluster(int(leaf))
	if cl >= 0 {
		row := s.ClusterPVS(int(cl))
		if row[cl>>3]&(1<<(cl&7)) == 0 {
			t.Error("cluster not in own PVS")
		}
	}
	for i := 1; i < m.NumInlineModels(); i++ {
		mod := m.InlineModel("*" + strconv.Itoa(i))
		if mod.Headnode < 0 {
			t.Errorf("model %d headnode %d", i, mod.Headnode)
		}
	}
}

func BenchmarkBoxTrace(b *testing.B) {
	s, _ := loadDemo(b, "demo1")
	starts := entityOrigins(s.Map().EntityString(), "info_player_start")
	if len(starts) == 0 {
		b.Skip("no start")
	}
	st := starts[0]
	mins, maxs := Vec3{-16, -16, -24}, Vec3{16, 16, 32}
	ends := []Vec3{
		{st[0] + 512, st[1], st[2]}, {st[0] - 512, st[1] + 300, st[2]},
		{st[0], st[1] - 512, st[2] - 100}, {st[0], st[1], st[2] - 1000},
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.BoxTrace(st, ends[i&3], mins, maxs, 0, q2const.MASK_PLAYERSOLID)
	}
}

func BenchmarkPointTrace(b *testing.B) {
	s, _ := loadDemo(b, "demo1")
	starts := entityOrigins(s.Map().EntityString(), "info_player_start")
	if len(starts) == 0 {
		b.Skip("no start")
	}
	st := starts[0]
	end := Vec3{st[0] + 1000, st[1] + 700, st[2] - 50}
	for i := 0; i < b.N; i++ {
		s.BoxTrace(st, end, Vec3{}, Vec3{}, 0, q2const.MASK_SHOT)
	}
}

func FuzzLoadMap(f *testing.F) {
	f.Add(bsp.Encode(bsp.SyntheticFloorMap()))
	f.Fuzz(func(t *testing.T, data []byte) {
		m, err := LoadMapBytes("fuzz", data)
		if err != nil {
			return
		}
		s := NewState(m)
		s.BoxTrace(Vec3{-100, -50, 100}, Vec3{80, 30, -100}, Vec3{-16, -16, -24}, Vec3{16, 16, 32}, 0, -1)
		s.BoxTrace(Vec3{1, 2, 3}, Vec3{1, 2, 3}, Vec3{-16, -16, -24}, Vec3{16, 16, 32}, 0, -1)
		s.PointContents(Vec3{0, 0, 0}, 0)
		for c := -1; c < m.NumClusters() && c < 4; c++ {
			s.ClusterPVS(c)
			s.ClusterPHS(c)
		}
	})
}
