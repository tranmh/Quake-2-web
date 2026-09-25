// Package sp2 parses .sp2 sprites (qcommon/qfiles.h dsprite_t) with the
// checks of ref_gl/gl_model.c Mod_LoadSpriteModel.
package sp2

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"

	"quake2web/server/internal/q2const"
)

// FrameSize is sizeof(dsprframe_t).
const FrameSize = 16 + q2const.MAX_SKINNAME

// Frame is C dsprframe_t.
type Frame struct {
	Width   int32  `json:"width"`
	Height  int32  `json:"height"`
	OriginX int32  `json:"originX"`
	OriginY int32  `json:"originY"`
	Name    string `json:"name"` // name of pcx file
}

// Sprite is C dsprite_t.
type Sprite struct {
	Ident     int32
	Version   int32
	NumFrames int32
	Frames    []Frame
}

// ErrBad marks files the C loader rejects.
var ErrBad = errors.New("sp2: bad sprite")

// Parse decodes a sprite.
// C: ref_gl/gl_model.c:1061 Mod_LoadSpriteModel
func Parse(data []byte) (*Sprite, error) {
	if len(data) < 12 {
		return nil, fmt.Errorf("%w: short header", ErrBad)
	}
	le := binary.LittleEndian
	s := &Sprite{
		Ident:     int32(le.Uint32(data[0:])),
		Version:   int32(le.Uint32(data[4:])),
		NumFrames: int32(le.Uint32(data[8:])),
	}
	if s.Ident != q2const.IDSPRITEHEADER {
		return nil, fmt.Errorf("%w: unknown fileid", ErrBad)
	}
	if s.Version != q2const.SPRITE_VERSION {
		return nil, fmt.Errorf("%w: wrong version number (%d should be %d)", ErrBad, s.Version, q2const.SPRITE_VERSION)
	}
	if s.NumFrames > q2const.MAX_MD2SKINS {
		return nil, fmt.Errorf("%w: too many frames (%d > %d)", ErrBad, s.NumFrames, q2const.MAX_MD2SKINS)
	}
	// C loops i < numframes, so a negative count loads no frames
	for i := 0; i < int(s.NumFrames); i++ {
		o := 12 + i*FrameSize
		if o+FrameSize > len(data) { // memory-safety (not in C)
			return nil, fmt.Errorf("%w: frame %d out of bounds", ErrBad, i)
		}
		name := data[o+16 : o+FrameSize]
		if j := bytes.IndexByte(name, 0); j >= 0 {
			name = name[:j]
		}
		s.Frames = append(s.Frames, Frame{
			Width:   int32(le.Uint32(data[o:])),
			Height:  int32(le.Uint32(data[o+4:])),
			OriginX: int32(le.Uint32(data[o+8:])),
			OriginY: int32(le.Uint32(data[o+12:])),
			Name:    string(name),
		})
	}
	return s, nil
}
