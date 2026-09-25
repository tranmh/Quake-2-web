package bsp

import (
	"encoding/binary"
	"math"

	"quake2web/server/internal/q2const"
)

// writer appends little-endian fields.
type writer struct{ b []byte }

func (w *writer) i32(v int32) { w.b = binary.LittleEndian.AppendUint32(w.b, uint32(v)) }
func (w *writer) u16(v uint16) {
	w.b = binary.LittleEndian.AppendUint16(w.b, v)
}
func (w *writer) i16(v int16)   { w.u16(uint16(v)) }
func (w *writer) f32(v float32) { w.b = binary.LittleEndian.AppendUint32(w.b, math.Float32bits(v)) }
func (w *writer) vec3(v [3]float32) {
	w.f32(v[0])
	w.f32(v[1])
	w.f32(v[2])
}
func (w *writer) short3(v [3]int16) {
	w.i16(v[0])
	w.i16(v[1])
	w.i16(v[2])
}

// Encode serializes f as an IBSP v38 file (lumps in index order, 4-byte
// aligned). Header ident/version are always written as IBSP/38. The raw
// Visibility lump is written as is (f.Vis is not re-encoded). It is the
// inverse of Parse and is used to build synthetic maps for tests and tools.
func Encode(f *File) []byte {
	var lumps [q2const.HEADER_LUMPS][]byte

	lumps[q2const.LUMP_ENTITIES] = f.Entities
	lumps[q2const.LUMP_VISIBILITY] = f.Visibility
	lumps[q2const.LUMP_LIGHTING] = f.Lighting
	lumps[q2const.LUMP_POP] = f.Pop

	var w writer
	for _, p := range f.Planes {
		w.vec3(p.Normal)
		w.f32(p.Dist)
		w.i32(p.Type)
	}
	lumps[q2const.LUMP_PLANES], w.b = w.b, nil
	for _, v := range f.Vertexes {
		w.vec3(v.Point)
	}
	lumps[q2const.LUMP_VERTEXES], w.b = w.b, nil
	for _, n := range f.Nodes {
		w.i32(n.PlaneNum)
		w.i32(n.Children[0])
		w.i32(n.Children[1])
		w.short3(n.Mins)
		w.short3(n.Maxs)
		w.u16(n.FirstFace)
		w.u16(n.NumFaces)
	}
	lumps[q2const.LUMP_NODES], w.b = w.b, nil
	for _, t := range f.TexInfo {
		for s := 0; s < 2; s++ {
			for k := 0; k < 4; k++ {
				w.f32(t.Vecs[s][k])
			}
		}
		w.i32(t.Flags)
		w.i32(t.Value)
		w.b = append(w.b, t.Texture[:]...)
		w.i32(t.NextTexInfo)
	}
	lumps[q2const.LUMP_TEXINFO], w.b = w.b, nil
	for _, fc := range f.Faces {
		w.u16(fc.PlaneNum)
		w.i16(fc.Side)
		w.i32(fc.FirstEdge)
		w.i16(fc.NumEdges)
		w.i16(fc.TexInfo)
		w.b = append(w.b, fc.Styles[:]...)
		w.i32(fc.LightOfs)
	}
	lumps[q2const.LUMP_FACES], w.b = w.b, nil
	for _, l := range f.Leafs {
		w.i32(l.Contents)
		w.i16(l.Cluster)
		w.i16(l.Area)
		w.short3(l.Mins)
		w.short3(l.Maxs)
		w.u16(l.FirstLeafFace)
		w.u16(l.NumLeafFaces)
		w.u16(l.FirstLeafBrush)
		w.u16(l.NumLeafBrushes)
	}
	lumps[q2const.LUMP_LEAFS], w.b = w.b, nil
	for _, v := range f.LeafFaces {
		w.u16(v)
	}
	lumps[q2const.LUMP_LEAFFACES], w.b = w.b, nil
	for _, v := range f.LeafBrushes {
		w.u16(v)
	}
	lumps[q2const.LUMP_LEAFBRUSHES], w.b = w.b, nil
	for _, e := range f.Edges {
		w.u16(e.V[0])
		w.u16(e.V[1])
	}
	lumps[q2const.LUMP_EDGES], w.b = w.b, nil
	for _, v := range f.SurfEdges {
		w.i32(v)
	}
	lumps[q2const.LUMP_SURFEDGES], w.b = w.b, nil
	for _, m := range f.Models {
		w.vec3(m.Mins)
		w.vec3(m.Maxs)
		w.vec3(m.Origin)
		w.i32(m.Headnode)
		w.i32(m.FirstFace)
		w.i32(m.NumFaces)
	}
	lumps[q2const.LUMP_MODELS], w.b = w.b, nil
	for _, b := range f.Brushes {
		w.i32(b.FirstSide)
		w.i32(b.NumSides)
		w.i32(b.Contents)
	}
	lumps[q2const.LUMP_BRUSHES], w.b = w.b, nil
	for _, s := range f.BrushSides {
		w.u16(s.PlaneNum)
		w.i16(s.TexInfo)
	}
	lumps[q2const.LUMP_BRUSHSIDES], w.b = w.b, nil
	for _, a := range f.Areas {
		w.i32(a.NumAreaPortals)
		w.i32(a.FirstAreaPortal)
	}
	lumps[q2const.LUMP_AREAS], w.b = w.b, nil
	for _, p := range f.AreaPortals {
		w.i32(p.PortalNum)
		w.i32(p.OtherArea)
	}
	lumps[q2const.LUMP_AREAPORTALS], w.b = w.b, nil

	out := writer{b: make([]byte, 0, headerSize)}
	out.i32(q2const.IDBSPHEADER)
	out.i32(q2const.BSPVERSION)
	ofs := headerSize
	for _, l := range lumps {
		out.i32(int32(ofs))
		out.i32(int32(len(l)))
		ofs += (len(l) + 3) &^ 3
	}
	for _, l := range lumps {
		out.b = append(out.b, l...)
		for len(out.b)%4 != 0 {
			out.b = append(out.b, 0)
		}
	}
	return out.b
}
