// Package tga ports the Targa loader of ref_gl/gl_image.c (LoadTGA): only
// uncompressed (type 2) and run-length encoded (type 10) 24/32-bit true
// colour images without colour map are supported.
package tga

import (
	"encoding/binary"
	"errors"
)

// HeaderSize is the size of the fixed Targa header.
const HeaderSize = 18

// Header is C TargaHeader.
type Header struct {
	IDLength       uint8
	ColormapType   uint8
	ImageType      uint8
	ColormapIndex  uint16
	ColormapLength uint16
	ColormapSize   uint8
	XOrigin        uint16
	YOrigin        uint16
	Width          uint16
	Height         uint16
	PixelSize      uint8
	Attributes     uint8
}

// Image is the decoded picture: RGBA, rows top to bottom (LoadTGA always
// flips, ignoring the origin bit of the attributes byte).
type Image struct {
	Header Header
	Width  int
	Height int
	Pix    []byte
}

var (
	// ErrType is LoadTGA's "Only type 2 and 10 targa RGB images supported".
	ErrType = errors.New("LoadTGA: Only type 2 and 10 targa RGB images supported")
	// ErrDepth is LoadTGA's "Only 32 or 24 bit images supported (no colormaps)".
	ErrDepth = errors.New("LoadTGA: Only 32 or 24 bit images supported (no colormaps)")
	// ErrShort is returned where C would read past the end of the buffer.
	ErrShort = errors.New("tga: truncated")
	// ErrTooLarge is returned for images wider or taller than MaxDimension.
	ErrTooLarge = errors.New("tga: image too large")
)

// MaxDimension bounds width and height (memory-safety deviation for
// untrusted uploads: C LoadTGA has no limit, but a crafted RLE header could
// otherwise make a few MiB of input allocate gigabytes; ref_gl resamples
// every upload to at most 256x256 anyway).
const MaxDimension = 4096

// Decode parses a Targa file.
// C: ref_gl/gl_image.c:539 LoadTGA
func Decode(data []byte) (*Image, error) {
	if len(data) < HeaderSize {
		return nil, ErrShort
	}
	le := binary.LittleEndian
	h := Header{
		IDLength:       data[0],
		ColormapType:   data[1],
		ImageType:      data[2],
		ColormapIndex:  le.Uint16(data[3:]),
		ColormapLength: le.Uint16(data[5:]),
		ColormapSize:   data[7],
		XOrigin:        le.Uint16(data[8:]),
		YOrigin:        le.Uint16(data[10:]),
		Width:          le.Uint16(data[12:]),
		Height:         le.Uint16(data[14:]),
		PixelSize:      data[16],
		Attributes:     data[17],
	}
	if h.ImageType != 2 && h.ImageType != 10 {
		return nil, ErrType
	}
	if h.ColormapType != 0 || (h.PixelSize != 32 && h.PixelSize != 24) {
		return nil, ErrDepth
	}
	columns, rows := int(h.Width), int(h.Height)
	if columns > MaxDimension || rows > MaxDimension { // memory-safety (not in C)
		return nil, ErrTooLarge
	}
	// memory-safety (not in C): refuse sizes the data cannot possibly cover
	// before allocating (an RLE packet yields at most 128 pixels)
	if n := columns * rows; (h.ImageType == 2 && n*int(h.PixelSize/8) > len(data)) || n > 128*len(data) {
		return nil, ErrShort
	}
	img := &Image{Header: h, Width: columns, Height: rows, Pix: make([]byte, columns*rows*4)}
	p := HeaderSize + int(h.IDLength) // skip TARGA image comment
	bpp := int(h.PixelSize) / 8

	// readPixel returns r,g,b,a of the next source pixel
	readPixel := func() (r, g, b, a byte, ok bool) {
		if p+bpp > len(data) {
			return 0, 0, 0, 0, false
		}
		b, g, r = data[p], data[p+1], data[p+2]
		a = 255
		if bpp == 4 {
			a = data[p+3]
		}
		p += bpp
		return r, g, b, a, true
	}

	if h.ImageType == 2 { // Uncompressed, RGB images
		for row := rows - 1; row >= 0; row-- {
			o := row * columns * 4
			for column := 0; column < columns; column++ {
				r, g, b, a, ok := readPixel()
				if !ok {
					return nil, ErrShort
				}
				img.Pix[o], img.Pix[o+1], img.Pix[o+2], img.Pix[o+3] = r, g, b, a
				o += 4
			}
		}
		return img, nil
	}

	// Runlength encoded RGB images
	for row := rows - 1; row >= 0; row-- {
		o := row * columns * 4
	columns:
		for column := 0; column < columns; {
			if p >= len(data) {
				return nil, ErrShort
			}
			packetHeader := data[p]
			p++
			packetSize := 1 + int(packetHeader&0x7f)
			var r, g, b, a byte
			rle := packetHeader&0x80 != 0
			if rle { // run-length packet
				var ok bool
				if r, g, b, a, ok = readPixel(); !ok {
					return nil, ErrShort
				}
			}
			for j := 0; j < packetSize; j++ {
				if !rle { // non run-length packet
					var ok bool
					if r, g, b, a, ok = readPixel(); !ok {
						return nil, ErrShort
					}
				}
				img.Pix[o], img.Pix[o+1], img.Pix[o+2], img.Pix[o+3] = r, g, b, a
				o += 4
				column++
				if column == columns { // run spans across rows
					column = 0
					if row > 0 {
						row--
					} else {
						break columns // goto breakOut
					}
					o = row * columns * 4
				}
			}
		}
	}
	return img, nil
}
