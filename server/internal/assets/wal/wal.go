// Package wal parses .wal textures (qcommon/qfiles.h miptex_t) the way
// ref_gl/gl_image.c GL_LoadWal reads them.
package wal

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
)

// MipLevels is C MIPLEVELS.
const MipLevels = 4

// HeaderSize is sizeof(miptex_t).
const HeaderSize = 32 + 4 + 4 + 4*MipLevels + 32 + 4 + 4 + 4

// MipTex is C miptex_t (qcommon/qfiles.h:198).
type MipTex struct {
	Name     string
	Width    uint32
	Height   uint32
	Offsets  [MipLevels]uint32
	AnimName string // next frame in animation chain
	Flags    int32
	Contents int32
	Value    int32
}

// ErrShort is returned for truncated files.
var ErrShort = errors.New("wal: truncated")

func cstring(b []byte) string {
	if i := bytes.IndexByte(b, 0); i >= 0 {
		b = b[:i]
	}
	return string(b)
}

// ParseHeader decodes the miptex_t header.
func ParseHeader(data []byte) (*MipTex, error) {
	if len(data) < HeaderSize {
		return nil, fmt.Errorf("%w: header", ErrShort)
	}
	le := binary.LittleEndian
	m := &MipTex{
		Name:   cstring(data[0:32]),
		Width:  le.Uint32(data[32:]),
		Height: le.Uint32(data[36:]),
	}
	for i := 0; i < MipLevels; i++ {
		m.Offsets[i] = le.Uint32(data[40+4*i:])
	}
	m.AnimName = cstring(data[56:88])
	m.Flags = int32(le.Uint32(data[88:]))
	m.Contents = int32(le.Uint32(data[92:]))
	m.Value = int32(le.Uint32(data[96:]))
	return m, nil
}

// Decode returns the header and the mip level 0 indices (width*height bytes
// at offsets[0]), which is all GL_LoadWal uploads (GL builds its own mips).
// C: ref_gl/gl_image.c:1318 GL_LoadWal
func Decode(data []byte) (*MipTex, []byte, error) {
	m, err := ParseHeader(data)
	if err != nil {
		return nil, nil, err
	}
	// C: width/height/ofs are LittleLong'ed into int
	w, h, ofs := int64(int32(m.Width)), int64(int32(m.Height)), int64(int32(m.Offsets[0]))
	if w <= 0 || h <= 0 || w > 1<<14 || h > 1<<14 {
		return m, nil, fmt.Errorf("wal: bad size %dx%d", w, h)
	}
	if ofs < 0 || ofs+w*h > int64(len(data)) {
		return m, nil, fmt.Errorf("%w: mip 0 out of bounds", ErrShort)
	}
	return m, data[ofs : ofs+w*h], nil
}
