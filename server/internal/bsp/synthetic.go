package bsp

import "quake2web/server/internal/q2const"

// SyntheticFloorMap returns a tiny valid map for tests and fuzz seeds: one
// solid brush (a 128x128x16 floor slab spanning x,y in [-64,64] and z in
// [-16,0]) floating in empty space, compiled into a six node BSP chain. The
// planes come in qbsp-style opposite pairs and nodes use the positive one.
//
//	leaf 0: CONTENTS_SOLID (outside, by convention), leaf 1: empty
//	(cluster 0, area 1), leaf 2: the slab (CONTENTS_SOLID, brush 0).
func SyntheticFloorMap() *File {
	f := &File{}
	type pl struct {
		n [3]float32
		d float32
		t int32
	}
	for _, p := range []pl{
		{[3]float32{0, 0, 1}, 0, 2}, {[3]float32{0, 0, -1}, 0, 2},
		{[3]float32{0, 0, 1}, -16, 2}, {[3]float32{0, 0, -1}, 16, 2},
		{[3]float32{1, 0, 0}, 64, 0}, {[3]float32{-1, 0, 0}, -64, 0},
		{[3]float32{1, 0, 0}, -64, 0}, {[3]float32{-1, 0, 0}, 64, 0},
		{[3]float32{0, 1, 0}, 64, 1}, {[3]float32{0, -1, 0}, -64, 1},
		{[3]float32{0, 1, 0}, -64, 1}, {[3]float32{0, -1, 0}, 64, 1},
	} {
		f.Planes = append(f.Planes, DPlane{Normal: p.n, Dist: p.d, Type: p.t})
	}
	const empty, slab = -1 - 1, -1 - 2
	f.Nodes = []DNode{
		{PlaneNum: 0, Children: [2]int32{empty, 1}},
		{PlaneNum: 2, Children: [2]int32{2, empty}},
		{PlaneNum: 4, Children: [2]int32{empty, 3}},
		{PlaneNum: 6, Children: [2]int32{4, empty}},
		{PlaneNum: 8, Children: [2]int32{empty, 5}},
		{PlaneNum: 10, Children: [2]int32{slab, empty}},
	}
	for i := range f.Nodes {
		f.Nodes[i].Mins = [3]int16{-64, -64, -16}
		f.Nodes[i].Maxs = [3]int16{64, 64, 0}
	}
	f.Leafs = []DLeaf{
		{Contents: q2const.CONTENTS_SOLID, Cluster: -1, Area: 0},
		{Contents: 0, Cluster: 0, Area: 1},
		{Contents: q2const.CONTENTS_SOLID, Cluster: -1, Area: 0, FirstLeafBrush: 0, NumLeafBrushes: 1},
	}
	f.LeafBrushes = []uint16{0}
	f.Brushes = []DBrush{{FirstSide: 0, NumSides: 6, Contents: q2const.CONTENTS_SOLID}}
	f.BrushSides = []DBrushSide{{0, 0}, {3, 0}, {4, 0}, {7, 0}, {8, 0}, {11, 0}}
	var tex TexInfo
	copy(tex.Texture[:], "e1u1/floor1_3")
	tex.Flags = 0
	tex.Value = 0
	tex.NextTexInfo = -1
	f.TexInfo = []TexInfo{tex}
	f.Models = []DModel{{
		Mins: [3]float32{-64, -64, -16}, Maxs: [3]float32{64, 64, 0}, Headnode: 0,
	}}
	f.Areas = []DArea{{}, {}}
	// dvis_t: numclusters=1, bitofs[0] = {12, 13}, then one PVS and one PHS byte
	f.Visibility = []byte{1, 0, 0, 0, 12, 0, 0, 0, 13, 0, 0, 0, 0x01, 0x01}
	f.Vis = DVis{NumClusters: 1, BitOfs: [][2]int32{{12, 13}}}
	f.Entities = []byte("{\n\"classname\" \"worldspawn\"\n}\n{\n\"classname\" \"info_player_start\"\n\"origin\" \"0 0 24\"\n}\n\x00")
	return f
}
