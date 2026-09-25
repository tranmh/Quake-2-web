// Package bsp parses IBSP version 38 files (qcommon/qfiles.h) into typed
// lumps. Every offset, length and count is bounds-checked against the file
// and the MAX_MAP_* design limits; malformed input yields an error, never a
// panic. The parser does not interpret cross references (plane numbers,
// children, ...); consumers such as cmodel validate those.
package bsp

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"

	"quake2web/server/internal/q2const"
)

// Lump is C lump_t.
// C: qcommon/qfiles.h:260 lump_t
type Lump struct {
	FileOfs, FileLen int32
}

// Header is C dheader_t.
// C: qcommon/qfiles.h:288 dheader_t
type Header struct {
	Ident   int32
	Version int32
	Lumps   [q2const.HEADER_LUMPS]Lump
}

// DModel is C dmodel_t (48 bytes).
// C: qcommon/qfiles.h:297 dmodel_t
type DModel struct {
	Mins, Maxs [3]float32
	Origin     [3]float32 // for sounds or lights
	Headnode   int32
	FirstFace  int32
	NumFaces   int32
}

// DVertex is C dvertex_t (12 bytes).
// C: qcommon/qfiles.h:303 dvertex_t
type DVertex struct {
	Point [3]float32
}

// DPlane is C dplane_t (20 bytes).
// C: qcommon/qfiles.h:323 dplane_t
type DPlane struct {
	Normal [3]float32
	Dist   float32
	Type   int32
}

// DNode is C dnode_t (28 bytes).
// C: qcommon/qfiles.h:389 dnode_t
type DNode struct {
	PlaneNum  int32
	Children  [2]int32 // negative numbers are -(leafs+1), not nodes
	Mins      [3]int16
	Maxs      [3]int16
	FirstFace uint16
	NumFaces  uint16
}

// TexInfo is C texinfo_t (76 bytes). Texture keeps the raw 32 bytes.
// C: qcommon/qfiles.h:399 texinfo_t
type TexInfo struct {
	Vecs        [2][4]float32
	Flags       int32
	Value       int32
	Texture     [32]byte
	NextTexInfo int32
}

// TextureName returns Texture up to the first NUL.
func (t *TexInfo) TextureName() string { return cstring(t.Texture[:]) }

// DEdge is C dedge_t (4 bytes).
// C: qcommon/qfiles.h:407 dedge_t
type DEdge struct {
	V [2]uint16
}

// DFace is C dface_t (20 bytes).
// C: qcommon/qfiles.h:422 dface_t
type DFace struct {
	PlaneNum  uint16
	Side      int16
	FirstEdge int32
	NumEdges  int16
	TexInfo   int16
	Styles    [q2const.MAXLIGHTMAPS]byte
	LightOfs  int32
}

// DLeaf is C dleaf_t (28 bytes).
// C: qcommon/qfiles.h:439 dleaf_t
type DLeaf struct {
	Contents       int32
	Cluster        int16
	Area           int16
	Mins           [3]int16
	Maxs           [3]int16
	FirstLeafFace  uint16
	NumLeafFaces   uint16
	FirstLeafBrush uint16
	NumLeafBrushes uint16
}

// DBrushSide is C dbrushside_t (4 bytes).
// C: qcommon/qfiles.h:445 dbrushside_t
type DBrushSide struct {
	PlaneNum uint16
	TexInfo  int16
}

// DBrush is C dbrush_t (12 bytes).
// C: qcommon/qfiles.h:452 dbrush_t
type DBrush struct {
	FirstSide int32
	NumSides  int32
	Contents  int32
}

// DVis is C dvis_t: the header of the visibility lump. BitOfs has
// NumClusters entries of {PVS offset, PHS offset}, relative to the lump.
// C: qcommon/qfiles.h:467 dvis_t
type DVis struct {
	NumClusters int32
	BitOfs      [][2]int32
}

// DAreaPortal is C dareaportal_t (8 bytes).
// C: qcommon/qfiles.h:476 dareaportal_t
type DAreaPortal struct {
	PortalNum int32
	OtherArea int32
}

// DArea is C darea_t (8 bytes).
// C: qcommon/qfiles.h:482 darea_t
type DArea struct {
	NumAreaPortals  int32
	FirstAreaPortal int32
}

// File is a parsed BSP. Raw byte lumps alias the input buffer.
type File struct {
	Header      Header
	Entities    []byte // raw entity string lump (may contain a trailing NUL)
	Planes      []DPlane
	Vertexes    []DVertex
	Visibility  []byte // raw lump, starts with the dvis_t header
	Vis         DVis   // parsed header (zero when the lump is empty)
	Nodes       []DNode
	TexInfo     []TexInfo
	Faces       []DFace
	Lighting    []byte
	Leafs       []DLeaf
	LeafFaces   []uint16
	LeafBrushes []uint16
	Edges       []DEdge
	SurfEdges   []int32
	Models      []DModel
	Brushes     []DBrush
	BrushSides  []DBrushSide
	Pop         []byte
	Areas       []DArea
	AreaPortals []DAreaPortal
}

// EntityString returns the entity lump up to the first NUL.
func (f *File) EntityString() string { return cstring(f.Entities) }

func cstring(b []byte) string {
	if i := bytes.IndexByte(b, 0); i >= 0 {
		b = b[:i]
	}
	return string(b)
}

var lumpNames = [q2const.HEADER_LUMPS]string{
	"entities", "planes", "vertexes", "visibility", "nodes", "texinfo", "faces",
	"lighting", "leafs", "leaffaces", "leafbrushes", "edges", "surfedges",
	"models", "brushes", "brushsides", "pop", "areas", "areaportals",
}

// lumpSpec gives the element size and maximum element count of each lump
// (qfiles.h MAX_MAP_*). A zero max means unbounded by the format.
var lumpSpec = [q2const.HEADER_LUMPS]struct{ size, max int }{
	q2const.LUMP_ENTITIES:    {1, q2const.MAX_MAP_ENTSTRING},
	q2const.LUMP_PLANES:      {20, q2const.MAX_MAP_PLANES},
	q2const.LUMP_VERTEXES:    {12, q2const.MAX_MAP_VERTS},
	q2const.LUMP_VISIBILITY:  {1, q2const.MAX_MAP_VISIBILITY},
	q2const.LUMP_NODES:       {28, q2const.MAX_MAP_NODES},
	q2const.LUMP_TEXINFO:     {76, q2const.MAX_MAP_TEXINFO},
	q2const.LUMP_FACES:       {20, q2const.MAX_MAP_FACES},
	q2const.LUMP_LIGHTING:    {1, q2const.MAX_MAP_LIGHTING},
	q2const.LUMP_LEAFS:       {28, q2const.MAX_MAP_LEAFS},
	q2const.LUMP_LEAFFACES:   {2, q2const.MAX_MAP_LEAFFACES},
	q2const.LUMP_LEAFBRUSHES: {2, q2const.MAX_MAP_LEAFBRUSHES},
	q2const.LUMP_EDGES:       {4, q2const.MAX_MAP_EDGES},
	q2const.LUMP_SURFEDGES:   {4, q2const.MAX_MAP_SURFEDGES},
	q2const.LUMP_MODELS:      {48, q2const.MAX_MAP_MODELS},
	q2const.LUMP_BRUSHES:     {12, q2const.MAX_MAP_BRUSHES},
	q2const.LUMP_BRUSHSIDES:  {4, q2const.MAX_MAP_BRUSHSIDES},
	q2const.LUMP_POP:         {1, 0},
	q2const.LUMP_AREAS:       {8, q2const.MAX_MAP_AREAS},
	q2const.LUMP_AREAPORTALS: {8, q2const.MAX_MAP_AREAPORTALS},
}

const headerSize = 8 + 8*q2const.HEADER_LUMPS

// reader decodes little-endian fields sequentially.
type reader struct {
	b []byte
	o int
}

func (r *reader) i32() int32 {
	v := int32(binary.LittleEndian.Uint32(r.b[r.o:]))
	r.o += 4
	return v
}
func (r *reader) u16() uint16 {
	v := binary.LittleEndian.Uint16(r.b[r.o:])
	r.o += 2
	return v
}
func (r *reader) i16() int16 { return int16(r.u16()) }
func (r *reader) f32() float32 {
	v := math.Float32frombits(binary.LittleEndian.Uint32(r.b[r.o:]))
	r.o += 4
	return v
}
func (r *reader) vec3() (v [3]float32) {
	v[0], v[1], v[2] = r.f32(), r.f32(), r.f32()
	return
}
func (r *reader) short3() (v [3]int16) {
	v[0], v[1], v[2] = r.i16(), r.i16(), r.i16()
	return
}

// Parse decodes an IBSP v38 file. The returned File aliases data.
func Parse(data []byte) (*File, error) {
	if len(data) < headerSize {
		return nil, fmt.Errorf("bsp: file too short (%d bytes)", len(data))
	}
	f := &File{}
	r := &reader{b: data}
	f.Header.Ident = r.i32()
	f.Header.Version = r.i32()
	for i := range f.Header.Lumps {
		f.Header.Lumps[i].FileOfs = r.i32()
		f.Header.Lumps[i].FileLen = r.i32()
	}
	if f.Header.Ident != q2const.IDBSPHEADER {
		return nil, fmt.Errorf("bsp: bad ident %#x", uint32(f.Header.Ident))
	}
	if f.Header.Version != q2const.BSPVERSION {
		return nil, fmt.Errorf("bsp: wrong version number (%d should be %d)", f.Header.Version, q2const.BSPVERSION)
	}

	var lumps [q2const.HEADER_LUMPS][]byte
	for i, l := range f.Header.Lumps {
		spec := lumpSpec[i]
		if l.FileOfs < 0 || l.FileLen < 0 || int64(l.FileOfs)+int64(l.FileLen) > int64(len(data)) {
			return nil, fmt.Errorf("bsp: lump %s out of bounds (ofs %d len %d, file %d)", lumpNames[i], l.FileOfs, l.FileLen, len(data))
		}
		if int(l.FileLen)%spec.size != 0 {
			return nil, fmt.Errorf("bsp: funny lump size for %s (%d)", lumpNames[i], l.FileLen)
		}
		if spec.max > 0 && int(l.FileLen)/spec.size > spec.max {
			return nil, fmt.Errorf("bsp: lump %s has %d elements (max %d)", lumpNames[i], int(l.FileLen)/spec.size, spec.max)
		}
		lumps[i] = data[l.FileOfs : l.FileOfs+l.FileLen]
	}

	f.Entities = lumps[q2const.LUMP_ENTITIES]
	f.Visibility = lumps[q2const.LUMP_VISIBILITY]
	f.Lighting = lumps[q2const.LUMP_LIGHTING]
	f.Pop = lumps[q2const.LUMP_POP]

	{
		b := lumps[q2const.LUMP_PLANES]
		r := &reader{b: b}
		f.Planes = make([]DPlane, len(b)/20)
		for i := range f.Planes {
			p := &f.Planes[i]
			p.Normal = r.vec3()
			p.Dist = r.f32()
			p.Type = r.i32()
		}
	}
	{
		b := lumps[q2const.LUMP_VERTEXES]
		r := &reader{b: b}
		f.Vertexes = make([]DVertex, len(b)/12)
		for i := range f.Vertexes {
			f.Vertexes[i].Point = r.vec3()
		}
	}
	{
		b := lumps[q2const.LUMP_NODES]
		r := &reader{b: b}
		f.Nodes = make([]DNode, len(b)/28)
		for i := range f.Nodes {
			n := &f.Nodes[i]
			n.PlaneNum = r.i32()
			n.Children[0] = r.i32()
			n.Children[1] = r.i32()
			n.Mins = r.short3()
			n.Maxs = r.short3()
			n.FirstFace = r.u16()
			n.NumFaces = r.u16()
		}
	}
	{
		b := lumps[q2const.LUMP_TEXINFO]
		r := &reader{b: b}
		f.TexInfo = make([]TexInfo, len(b)/76)
		for i := range f.TexInfo {
			t := &f.TexInfo[i]
			for s := 0; s < 2; s++ {
				for k := 0; k < 4; k++ {
					t.Vecs[s][k] = r.f32()
				}
			}
			t.Flags = r.i32()
			t.Value = r.i32()
			copy(t.Texture[:], b[r.o:r.o+32])
			r.o += 32
			t.NextTexInfo = r.i32()
		}
	}
	{
		b := lumps[q2const.LUMP_FACES]
		r := &reader{b: b}
		f.Faces = make([]DFace, len(b)/20)
		for i := range f.Faces {
			fc := &f.Faces[i]
			fc.PlaneNum = r.u16()
			fc.Side = r.i16()
			fc.FirstEdge = r.i32()
			fc.NumEdges = r.i16()
			fc.TexInfo = r.i16()
			copy(fc.Styles[:], b[r.o:r.o+4])
			r.o += 4
			fc.LightOfs = r.i32()
		}
	}
	{
		b := lumps[q2const.LUMP_LEAFS]
		r := &reader{b: b}
		f.Leafs = make([]DLeaf, len(b)/28)
		for i := range f.Leafs {
			l := &f.Leafs[i]
			l.Contents = r.i32()
			l.Cluster = r.i16()
			l.Area = r.i16()
			l.Mins = r.short3()
			l.Maxs = r.short3()
			l.FirstLeafFace = r.u16()
			l.NumLeafFaces = r.u16()
			l.FirstLeafBrush = r.u16()
			l.NumLeafBrushes = r.u16()
		}
	}
	f.LeafFaces = u16s(lumps[q2const.LUMP_LEAFFACES])
	f.LeafBrushes = u16s(lumps[q2const.LUMP_LEAFBRUSHES])
	{
		b := lumps[q2const.LUMP_EDGES]
		r := &reader{b: b}
		f.Edges = make([]DEdge, len(b)/4)
		for i := range f.Edges {
			f.Edges[i].V[0] = r.u16()
			f.Edges[i].V[1] = r.u16()
		}
	}
	{
		b := lumps[q2const.LUMP_SURFEDGES]
		r := &reader{b: b}
		f.SurfEdges = make([]int32, len(b)/4)
		for i := range f.SurfEdges {
			f.SurfEdges[i] = r.i32()
		}
	}
	{
		b := lumps[q2const.LUMP_MODELS]
		r := &reader{b: b}
		f.Models = make([]DModel, len(b)/48)
		for i := range f.Models {
			m := &f.Models[i]
			m.Mins = r.vec3()
			m.Maxs = r.vec3()
			m.Origin = r.vec3()
			m.Headnode = r.i32()
			m.FirstFace = r.i32()
			m.NumFaces = r.i32()
		}
	}
	{
		b := lumps[q2const.LUMP_BRUSHES]
		r := &reader{b: b}
		f.Brushes = make([]DBrush, len(b)/12)
		for i := range f.Brushes {
			br := &f.Brushes[i]
			br.FirstSide = r.i32()
			br.NumSides = r.i32()
			br.Contents = r.i32()
		}
	}
	{
		b := lumps[q2const.LUMP_BRUSHSIDES]
		r := &reader{b: b}
		f.BrushSides = make([]DBrushSide, len(b)/4)
		for i := range f.BrushSides {
			f.BrushSides[i].PlaneNum = r.u16()
			f.BrushSides[i].TexInfo = r.i16()
		}
	}
	{
		b := lumps[q2const.LUMP_AREAS]
		r := &reader{b: b}
		f.Areas = make([]DArea, len(b)/8)
		for i := range f.Areas {
			f.Areas[i].NumAreaPortals = r.i32()
			f.Areas[i].FirstAreaPortal = r.i32()
		}
	}
	{
		b := lumps[q2const.LUMP_AREAPORTALS]
		r := &reader{b: b}
		f.AreaPortals = make([]DAreaPortal, len(b)/8)
		for i := range f.AreaPortals {
			f.AreaPortals[i].PortalNum = r.i32()
			f.AreaPortals[i].OtherArea = r.i32()
		}
	}

	// visibility header: numclusters then bitofs[numclusters][2]
	if v := f.Visibility; len(v) > 0 {
		if len(v) < 4 {
			return nil, fmt.Errorf("bsp: visibility lump too short (%d)", len(v))
		}
		n := int32(binary.LittleEndian.Uint32(v))
		if n < 0 || int64(4+8*int64(n)) > int64(len(v)) {
			return nil, fmt.Errorf("bsp: visibility header claims %d clusters in %d bytes", n, len(v))
		}
		f.Vis.NumClusters = n
		f.Vis.BitOfs = make([][2]int32, n)
		r := &reader{b: v, o: 4}
		for i := range f.Vis.BitOfs {
			f.Vis.BitOfs[i][0] = r.i32()
			f.Vis.BitOfs[i][1] = r.i32()
		}
	}
	return f, nil
}

func u16s(b []byte) []uint16 {
	out := make([]uint16, len(b)/2)
	for i := range out {
		out[i] = binary.LittleEndian.Uint16(b[i*2:])
	}
	return out
}
