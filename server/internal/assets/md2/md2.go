// Package md2 parses and validates .md2 alias models (qcommon/qfiles.h
// dmdl_t) with the checks of ref_gl/gl_model.c Mod_LoadAliasModel. Models are
// served verbatim; this package only extracts metadata for the asset index.
package md2

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"math"

	"quake2web/server/internal/q2const"
)

// HeaderSize is sizeof(dmdl_t).
const HeaderSize = 17 * 4

// MaxLBMHeight is ref_gl/gl_local.h MAX_LBM_HEIGHT.
const MaxLBMHeight = 480

// Header is C dmdl_t.
type Header struct {
	Ident      int32
	Version    int32
	SkinWidth  int32
	SkinHeight int32
	FrameSize  int32
	NumSkins   int32
	NumXYZ     int32
	NumST      int32
	NumTris    int32
	NumGLCmds  int32
	NumFrames  int32
	OfsSkins   int32
	OfsST      int32
	OfsTris    int32
	OfsFrames  int32
	OfsGLCmds  int32
	OfsEnd     int32
}

// Frame is the header of a daliasframe_t (vertices are left in the file).
type Frame struct {
	Scale     [3]float32
	Translate [3]float32
	Name      string
}

// Model is the metadata of a validated MD2.
type Model struct {
	Header Header
	Skins  []string
	Frames []Frame
}

// ErrBad marks files Mod_LoadAliasModel / Mod_ForName would reject.
var ErrBad = errors.New("md2: bad model")

func cstring(b []byte) string {
	if i := bytes.IndexByte(b, 0); i >= 0 {
		b = b[:i]
	}
	return string(b)
}

func span(ofs, n, size int32, total int) bool {
	if ofs < 0 || n < 0 || size < 0 {
		return false
	}
	return int64(ofs)+int64(n)*int64(size) <= int64(total)
}

// Parse validates an MD2 exactly like Mod_ForName (ident) +
// Mod_LoadAliasModel (version, skin height, vertex/st/triangle/frame counts)
// and additionally bounds-checks every lump the C loader reads unchecked
// (memory-safety deviation for uploaded data).
// C: ref_gl/gl_model.c:929 Mod_LoadAliasModel
func Parse(data []byte) (*Model, error) {
	if len(data) < HeaderSize {
		return nil, fmt.Errorf("%w: short header", ErrBad)
	}
	var v [17]int32
	for i := range v {
		v[i] = int32(binary.LittleEndian.Uint32(data[i*4:]))
	}
	h := Header{v[0], v[1], v[2], v[3], v[4], v[5], v[6], v[7], v[8], v[9], v[10], v[11], v[12], v[13], v[14], v[15], v[16]}
	if h.Ident != q2const.IDALIASHEADER {
		return nil, fmt.Errorf("%w: unknown fileid", ErrBad)
	}
	if h.Version != q2const.ALIAS_VERSION {
		return nil, fmt.Errorf("%w: wrong version number (%d should be %d)", ErrBad, h.Version, q2const.ALIAS_VERSION)
	}
	switch {
	case h.SkinHeight > MaxLBMHeight:
		return nil, fmt.Errorf("%w: has a skin taller than %d", ErrBad, MaxLBMHeight)
	case h.NumXYZ <= 0:
		return nil, fmt.Errorf("%w: has no vertices", ErrBad)
	case h.NumXYZ > q2const.MAX_VERTS:
		return nil, fmt.Errorf("%w: has too many vertices", ErrBad)
	case h.NumST <= 0:
		return nil, fmt.Errorf("%w: has no st vertices", ErrBad)
	case h.NumTris <= 0:
		return nil, fmt.Errorf("%w: has no triangles", ErrBad)
	case h.NumFrames <= 0:
		return nil, fmt.Errorf("%w: has no frames", ErrBad)
	}
	// memory-safety checks (not in C)
	n := len(data)
	switch {
	case h.NumSkins < 0 || h.NumSkins > q2const.MAX_MD2SKINS:
		return nil, fmt.Errorf("%w: bad skin count %d", ErrBad, h.NumSkins)
	case !span(h.OfsSkins, h.NumSkins, q2const.MAX_SKINNAME, n):
		return nil, fmt.Errorf("%w: skins out of bounds", ErrBad)
	case !span(h.OfsST, h.NumST, 4, n):
		return nil, fmt.Errorf("%w: st out of bounds", ErrBad)
	case !span(h.OfsTris, h.NumTris, 12, n):
		return nil, fmt.Errorf("%w: triangles out of bounds", ErrBad)
	case int64(h.FrameSize) < 40+int64(h.NumXYZ)*4:
		return nil, fmt.Errorf("%w: frame size %d too small", ErrBad, h.FrameSize)
	case !span(h.OfsFrames, h.NumFrames, h.FrameSize, n):
		return nil, fmt.Errorf("%w: frames out of bounds", ErrBad)
	case !span(h.OfsGLCmds, h.NumGLCmds, 4, n):
		return nil, fmt.Errorf("%w: glcmds out of bounds", ErrBad)
	}
	m := &Model{Header: h}
	for i := int32(0); i < h.NumSkins; i++ {
		o := int(h.OfsSkins) + int(i)*q2const.MAX_SKINNAME
		m.Skins = append(m.Skins, cstring(data[o:o+q2const.MAX_SKINNAME]))
	}
	m.Frames = make([]Frame, h.NumFrames)
	for i := range m.Frames {
		o := int(h.OfsFrames) + i*int(h.FrameSize)
		f := &m.Frames[i]
		for j := 0; j < 3; j++ {
			f.Scale[j] = math.Float32frombits(binary.LittleEndian.Uint32(data[o+4*j:]))
			f.Translate[j] = math.Float32frombits(binary.LittleEndian.Uint32(data[o+12+4*j:]))
		}
		f.Name = cstring(data[o+24 : o+40])
	}
	return m, nil
}
