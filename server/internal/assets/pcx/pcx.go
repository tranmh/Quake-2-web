// Package pcx ports the PCX loader of ref_gl/gl_image.c (LoadPCX) and the
// pcx_t header of qcommon/qfiles.h.
package pcx

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// HeaderSize is sizeof(pcx_t) up to (not including) the unbounded data byte.
const HeaderSize = 128

// PaletteSize is the size of the trailing 256-colour palette.
const PaletteSize = 768

// Header is C pcx_t (qcommon/qfiles.h:59).
type Header struct {
	Manufacturer int8
	Version      int8
	Encoding     int8
	BitsPerPixel int8
	XMin, YMin   uint16
	XMax, YMax   uint16
	HRes, VRes   uint16
	Palette16    [48]byte
	Reserved     int8
	ColorPlanes  int8
	BytesPerLine uint16
	PaletteType  uint16
}

// Image is the result of LoadPCX: 8-bit palette indices (row-major,
// Width*Height) plus the 768-byte palette taken from the last 768 bytes of
// the file.
type Image struct {
	Header  Header
	Width   int
	Height  int
	Pix     []byte
	Palette []byte // len 768
}

// ErrBad is returned for files LoadPCX rejects with "Bad pcx file".
var ErrBad = errors.New("pcx: bad pcx file")

// ErrMalformed is returned when decoding runs past the end of the file
// ("PCX file %s was malformed").
var ErrMalformed = errors.New("pcx: malformed")

// ParseHeader decodes the 128-byte header.
func ParseHeader(data []byte) (Header, error) {
	var h Header
	if len(data) < HeaderSize {
		return h, fmt.Errorf("%w: short header (%d bytes)", ErrBad, len(data))
	}
	le := binary.LittleEndian
	h.Manufacturer = int8(data[0])
	h.Version = int8(data[1])
	h.Encoding = int8(data[2])
	h.BitsPerPixel = int8(data[3])
	h.XMin = le.Uint16(data[4:])
	h.YMin = le.Uint16(data[6:])
	h.XMax = le.Uint16(data[8:])
	h.YMax = le.Uint16(data[10:])
	h.HRes = le.Uint16(data[12:])
	h.VRes = le.Uint16(data[14:])
	copy(h.Palette16[:], data[16:64])
	h.Reserved = int8(data[64])
	h.ColorPlanes = int8(data[65])
	h.BytesPerLine = le.Uint16(data[66:])
	h.PaletteType = le.Uint16(data[68:])
	return h, nil
}

// Decode parses a PCX file exactly like LoadPCX: only 8-bit RLE version 5
// images smaller than 640x480 are accepted; xmin/ymin and bytes_per_line are
// ignored (each row decodes xmax+1 pixels); runs that overflow a row spill
// into the following rows, which then overwrite them, so the visible result
// equals clipping at the end of the pixel buffer. Reading past the end of
// the file (which C does, then detects) yields ErrMalformed.
// C: ref_gl/gl_image.c:421 LoadPCX
func Decode(data []byte) (*Image, error) {
	h, err := ParseHeader(data)
	if err != nil {
		return nil, err
	}
	if h.Manufacturer != 0x0a || h.Version != 5 || h.Encoding != 1 || h.BitsPerPixel != 8 ||
		h.XMax >= 640 || h.YMax >= 480 {
		return nil, ErrBad
	}
	// memory-safety: C copies the palette from (pcx + len - 768) unchecked
	if len(data) < PaletteSize {
		return nil, fmt.Errorf("%w: file shorter than palette", ErrMalformed)
	}
	w := int(h.XMax) + 1
	hgt := int(h.YMax) + 1
	img := &Image{
		Header:  h,
		Width:   w,
		Height:  hgt,
		Pix:     make([]byte, w*hgt),
		Palette: append([]byte(nil), data[len(data)-PaletteSize:]...),
	}
	raw := HeaderSize
	out := img.Pix
	for y := 0; y < hgt; y++ {
		row := y * w
		for x := 0; x < w; {
			if raw >= len(data) {
				return nil, ErrMalformed
			}
			dataByte := data[raw]
			raw++
			runLength := 1
			if dataByte&0xC0 == 0xC0 {
				runLength = int(dataByte & 0x3F)
				if raw >= len(data) {
					return nil, ErrMalformed
				}
				dataByte = data[raw]
				raw++
			}
			for ; runLength > 0; runLength-- {
				if p := row + x; p < len(out) {
					out[p] = dataByte
				}
				x++
			}
		}
	}
	return img, nil
}
