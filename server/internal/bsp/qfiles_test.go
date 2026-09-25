package bsp

import (
	"encoding/binary"
	"reflect"
	"testing"

	"quake2web/server/internal/assets/pak"
	"quake2web/server/internal/q2const"
	"quake2web/server/internal/testutil"
)

func TestRoundTripSynthetic(t *testing.T) {
	f := SyntheticFloorMap()
	data := Encode(f)
	g, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	g.Header = Header{}
	f.Header = Header{}
	// Parse returns empty (non-nil) slices for empty lumps; normalise.
	if len(g.Faces) == 0 {
		g.Faces = nil
	}
	for _, p := range []*[]byte{&g.Lighting, &g.Pop} {
		if len(*p) == 0 {
			*p = nil
		}
	}
	norm := func(x *File) {
		if len(x.Vertexes) == 0 {
			x.Vertexes = nil
		}
		if len(x.LeafFaces) == 0 {
			x.LeafFaces = nil
		}
		if len(x.Edges) == 0 {
			x.Edges = nil
		}
		if len(x.SurfEdges) == 0 {
			x.SurfEdges = nil
		}
		if len(x.AreaPortals) == 0 {
			x.AreaPortals = nil
		}
		if len(x.Faces) == 0 {
			x.Faces = nil
		}
		if len(x.Lighting) == 0 {
			x.Lighting = nil
		}
		if len(x.Pop) == 0 {
			x.Pop = nil
		}
	}
	norm(f)
	norm(g)
	if !reflect.DeepEqual(f, g) {
		t.Fatalf("round trip mismatch:\n%+v\n%+v", f, g)
	}
	if g.EntityString() == "" || g.TexInfo[0].TextureName() != "e1u1/floor1_3" {
		t.Fatal("bad strings")
	}
}

func TestParseErrors(t *testing.T) {
	good := Encode(SyntheticFloorMap())
	mut := func(fn func(b []byte) []byte) []byte {
		b := append([]byte(nil), good...)
		return fn(b)
	}
	lumpOfs := func(i int) int { return 8 + 8*i }
	cases := map[string][]byte{
		"empty":   nil,
		"short":   good[:50],
		"ident":   mut(func(b []byte) []byte { b[0] = 'X'; return b }),
		"version": mut(func(b []byte) []byte { binary.LittleEndian.PutUint32(b[4:], 37); return b }),
		"lumpofs": mut(func(b []byte) []byte {
			binary.LittleEndian.PutUint32(b[lumpOfs(q2const.LUMP_PLANES):], 0x7fffffff)
			return b
		}),
		"neglen": mut(func(b []byte) []byte {
			binary.LittleEndian.PutUint32(b[lumpOfs(q2const.LUMP_NODES)+4:], 0xfffffff0)
			return b
		}),
		"funny": mut(func(b []byte) []byte {
			o := lumpOfs(q2const.LUMP_PLANES) + 4
			binary.LittleEndian.PutUint32(b[o:], binary.LittleEndian.Uint32(b[o:])-1)
			return b
		}),
		"visheader": mut(func(b []byte) []byte {
			o := int(binary.LittleEndian.Uint32(b[lumpOfs(q2const.LUMP_VISIBILITY):]))
			binary.LittleEndian.PutUint32(b[o:], 1000)
			return b
		}),
	}
	for name, data := range cases {
		if _, err := Parse(data); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

func TestLimits(t *testing.T) {
	f := SyntheticFloorMap()
	f.Areas = make([]DArea, q2const.MAX_MAP_AREAS+1)
	if _, err := Parse(Encode(f)); err == nil {
		t.Error("expected MAX_MAP_AREAS error")
	}
}

// LoadDemoMap reads maps/<name>.bsp from the demo pak (skipping if absent).
func loadDemoMap(t testing.TB, name string) []byte {
	t.Helper()
	p, err := pak.Open(testutil.DemoPak(t))
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	b, err := p.ReadFile("maps/" + name + ".bsp")
	if err != nil {
		t.Skipf("no %s in demo pak: %v", name, err)
	}
	return b
}

func TestDemo1(t *testing.T) {
	data := loadDemoMap(t, "demo1")
	f, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("demo1: %d planes %d nodes %d leafs %d brushes %d brushsides %d texinfo %d models %d areas %d areaportals %d faces vis %d clusters",
		len(f.Planes), len(f.Nodes), len(f.Leafs), len(f.Brushes), len(f.BrushSides), len(f.TexInfo),
		len(f.Models), len(f.Areas), len(f.AreaPortals), len(f.Faces), f.Vis.NumClusters)
	if len(f.Models) < 1 || len(f.Leafs) < 2 || len(f.Nodes) < 1 {
		t.Fatal("implausible map")
	}
	if f.Leafs[0].Contents != q2const.CONTENTS_SOLID {
		t.Error("leaf 0 not solid")
	}
	// cross references of a real map are consistent
	for i, n := range f.Nodes {
		if int(n.PlaneNum) >= len(f.Planes) {
			t.Fatalf("node %d plane %d", i, n.PlaneNum)
		}
	}
	// re-encoding and re-parsing yields identical typed data
	g, err := Parse(Encode(f))
	if err != nil {
		t.Fatal(err)
	}
	g.Header, f.Header = Header{}, Header{}
	if !reflect.DeepEqual(f, g) {
		t.Error("demo1 encode/parse round trip mismatch")
	}
}

func BenchmarkParseDemo1(b *testing.B) {
	data := loadDemoMap(b, "demo1")
	b.SetBytes(int64(len(data)))
	for i := 0; i < b.N; i++ {
		if _, err := Parse(data); err != nil {
			b.Fatal(err)
		}
	}
}

func FuzzParse(f *testing.F) {
	f.Add(Encode(SyntheticFloorMap()))
	f.Add([]byte("IBSP&\x00\x00\x00"))
	f.Fuzz(func(t *testing.T, data []byte) {
		bf, err := Parse(data)
		if err != nil {
			return
		}
		_ = bf.EntityString()
		for i := range bf.TexInfo {
			_ = bf.TexInfo[i].TextureName()
		}
	})
}
