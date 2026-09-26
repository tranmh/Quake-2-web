package cmodel

import (
	"testing"

	"quake2web/server/internal/bsp"
	"quake2web/server/internal/qcommon/shared"
)

// Maps come from user-uploaded paks. A node whose child points back at
// itself (or an ancestor) passed validate(): CM_PointLeafnum_r then loops
// forever and CM_RecursiveHullCheck / CM_BoxLeafnums_r / HeadnodeVisible
// recurse until Go's "fatal error: stack overflow", which no recover can
// catch: one uploaded map took down the whole server process.
func TestLoadMapRejectsNodeCycles(t *testing.T) {
	for _, tc := range []struct {
		name  string
		patch func(f *bsp.File)
	}{
		{"self", func(f *bsp.File) { f.Nodes[0].Children[0] = 0 }},
		{"back edge", func(f *bsp.File) { f.Nodes[5].Children[1] = 2 }},
		{"submodel loop", func(f *bsp.File) {
			f.Nodes[3].Children[0] = 3
			f.Models = append(f.Models, bsp.DModel{Headnode: 3})
		}},
	} {
		f := bsp.SyntheticFloorMap()
		tc.patch(f)
		if _, err := LoadMapBytes("cycle", bsp.Encode(f)); err == nil {
			t.Errorf("%s: map with a node cycle was accepted", tc.name)
		}
	}
}

// A node referenced by two parents (a DAG) is not a tree either: with a
// chain of N nodes whose two children are both the next node, every walk
// that visits both sides (a trace straddling the planes, CM_BoxLeafnums_r,
// CM_HeadnodeVisible) takes 2^N steps, hanging the instance forever.
func TestLoadMapRejectsSharedSubtrees(t *testing.T) {
	f := bsp.SyntheticFloorMap()
	f.Nodes[0].Children[0] = 2 // node 2 is now reachable from node 0 and node 1
	if _, err := LoadMapBytes("dag", bsp.Encode(f)); err == nil {
		t.Errorf("map with a shared subtree was accepted")
	}

	// the exponential case itself must be rejected, not traced
	f = bsp.SyntheticFloorMap()
	const n = 64
	f.Nodes = make([]bsp.DNode, n)
	for i := range f.Nodes {
		c := int32(i + 1)
		if i == n-1 {
			c = -1 - 1 // empty leaf
		}
		f.Nodes[i] = bsp.DNode{PlaneNum: 0, Children: [2]int32{c, c},
			Mins: [3]int16{-64, -64, -16}, Maxs: [3]int16{64, 64, 0}}
	}
	if _, err := LoadMapBytes("dag64", bsp.Encode(f)); err == nil {
		t.Errorf("exponential DAG accepted")
	}
}

// Every inline model is a separate root; the synthetic map and the demo maps
// (golden tests) still load.
func TestLoadMapTreeStillLoads(t *testing.T) {
	if _, err := LoadMapBytes("tree", bsp.Encode(bsp.SyntheticFloorMap())); err != nil {
		t.Fatal(err)
	}
}

// FuzzLoadMapStructured patches fields of the synthetic map (cross
// references, contents, clusters, areas, portals, vis offsets) instead of
// mutating raw bytes, then runs every query the server makes. A hang shows
// up as a stalled fuzzer; a panic that is not a Com_Error is a bug.
func FuzzLoadMapStructured(f *testing.F) {
	f.Add([]byte{0, 0, 0, 0, 0, 0})
	f.Add([]byte{1, 5, 1, 0xfe, 0xff, 0xff, 2, 1, 0, 3, 0, 0})
	f.Add([]byte{6, 0, 0, 1, 0, 0, 7, 1, 0, 0x10, 0, 0, 8, 0, 0, 200, 0, 0})
	f.Fuzz(func(t *testing.T, patch []byte) {
		fm := bsp.SyntheticFloorMap()
		fm.Areas = append(fm.Areas, bsp.DArea{NumAreaPortals: 1, FirstAreaPortal: 0})
		fm.AreaPortals = []bsp.DAreaPortal{{PortalNum: 1, OtherArea: 1}}
		for len(patch) >= 4 {
			sel, idx := patch[0], int(patch[1])
			v := int32(int16(uint16(patch[2]) | uint16(patch[3])<<8))
			patch = patch[4:]
			switch sel % 12 {
			case 0:
				fm.Nodes[idx%len(fm.Nodes)].Children[0] = v
			case 1:
				fm.Nodes[idx%len(fm.Nodes)].Children[1] = v
			case 2:
				fm.Nodes[idx%len(fm.Nodes)].PlaneNum = v
			case 3:
				fm.Leafs[idx%len(fm.Leafs)].Cluster = int16(v)
			case 4:
				fm.Leafs[idx%len(fm.Leafs)].Area = int16(v)
			case 5:
				fm.Leafs[idx%len(fm.Leafs)].Contents = v
			case 6:
				fm.Areas[idx%len(fm.Areas)].NumAreaPortals = v
			case 7:
				fm.AreaPortals[0].PortalNum = v
			case 8:
				fm.AreaPortals[0].OtherArea = v
			case 9:
				if n := len(fm.Visibility); n > 0 {
					fm.Visibility[idx%n] = byte(v)
				}
			case 10:
				fm.Models[0].Headnode = v
			case 11:
				fm.Nodes = append(fm.Nodes, bsp.DNode{PlaneNum: v & 7, Children: [2]int32{-1, int32(idx) - 128}})
			}
		}
		m, err := LoadMapBytes("fuzz", bsp.Encode(fm))
		if err != nil {
			return
		}
		s := NewState(m)
		func() {
			defer func() {
				if r := recover(); r != nil {
					if _, ok := r.(shared.ComError); !ok {
						panic(r)
					}
				}
			}()
			s.BoxTrace(Vec3{-100, -50, 100}, Vec3{80, 30, -100}, Vec3{-16, -16, -24}, Vec3{16, 16, 32}, 0, -1)
			s.PointLeafnum(Vec3{0, 0, 10})
			var leafs [64]int32
			var top int32
			s.BoxLeafnums(Vec3{-200, -200, -200}, Vec3{200, 200, 200}, leafs[:], &top)
			s.HeadnodeVisible(0, make([]byte, 8192))
			for c := -1; c < m.NumClusters() && c < 4; c++ {
				s.ClusterPVS(c)
				s.ClusterPHS(c)
			}
			for l := 0; l < 3; l++ {
				m.LeafArea(l)
				m.LeafCluster(l)
			}
			s.SetAreaPortalState(1, true)
			var bits [32]byte
			s.WriteAreaBits(bits[:], 1)
			s.AreasConnected(0, 1)
		}()
	})
}

// A leaf cluster below -1 (from a user map) indexed visbits[cluster>>3] with
// a negative index in CM_HeadnodeVisible (SV_BuildClientFrame for any entity
// spanning too many leafs): runtime panic every server frame.
func TestHeadnodeVisibleNegativeCluster(t *testing.T) {
	fm := bsp.SyntheticFloorMap()
	fm.Leafs[1].Cluster = -26
	m, err := LoadMapBytes("negcluster", bsp.Encode(fm))
	if err != nil {
		t.Skipf("rejected at load: %v", err)
	}
	s := NewState(m)
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("HeadnodeVisible panicked: %v", r)
		}
	}()
	if s.HeadnodeVisible(0, make([]byte, 8192)) {
		t.Errorf("negative cluster reported visible")
	}
}
