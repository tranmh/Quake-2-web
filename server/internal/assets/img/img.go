// Package img holds the palette handling and 8-bit → RGBA conversion of
// ref_gl/gl_image.c (Draw_GetPalette, GL_Upload8, R_FloodFillSkin) plus PNG
// encoding for ingest.
//
// Conversion rules (docs/ASSETS.md):
//   - The palette comes from pics/colormap.pcx (Draw_GetPalette); index 255
//     has alpha 0, every other index alpha 255.
//   - Every 8-bit image (pic, skin, wall, sprite and — with the default
//     gl_ext_palettedtexture 0 — sky) goes through GL_Upload8: index 255
//     becomes transparent and its RGB is copied from the first non-255
//     neighbour in the order up, down, left, right (else palette index 0),
//     using GL_Upload8's exact bounds tests (i > width, i < s-width, i > 0,
//     i < s-1) on the flattened buffer.
//   - Skins (it_skin) get R_FloodFillSkin on the indices first.
//   - Gamma/intensity (GL_LightScaleTexture), power-of-two resampling,
//     picmip and the scrap atlas are GL upload-time state and are not applied.
package img

import (
	"bytes"
	"errors"
	"image"
	"image/png"
)

// Palette is the 768-byte RGB palette of pics/colormap.pcx.
type Palette [768]byte

// Table is C d_8to24table: little-endian RGBA packed as r | g<<8 | b<<16 |
// a<<24, with entry 255's alpha cleared.
type Table [256]uint32

// ErrPalette is returned for palettes of the wrong size.
var ErrPalette = errors.New("img: palette must be 768 bytes")

// NewPalette copies a 768-byte palette.
func NewPalette(b []byte) (Palette, error) {
	var p Palette
	if len(b) != len(p) {
		return p, ErrPalette
	}
	copy(p[:], b)
	return p, nil
}

// Table builds d_8to24table from the palette.
// C: ref_gl/gl_image.c:1457 Draw_GetPalette
func (p *Palette) Table() Table {
	var t Table
	for i := 0; i < 256; i++ {
		r := uint32(p[i*3+0])
		g := uint32(p[i*3+1])
		b := uint32(p[i*3+2])
		t[i] = (255 << 24) + (r << 0) + (g << 8) + (b << 16)
	}
	t[255] &= 0xffffff // 255 is transparent
	return t
}

// RGBA is a non-premultiplied 8-bit RGBA image (RGB of transparent texels is
// meaningful, see GL_Upload8).
type RGBA struct {
	Width, Height int
	Pix           []byte // 4*Width*Height, rows top to bottom
}

// HasAlpha reports whether any texel has alpha != 255, the test GL_Upload32
// uses to pick the alpha texture format (image_t.has_alpha).
// C: ref_gl/gl_image.c:1000 GL_Upload32 (alpha scan)
func (m *RGBA) HasAlpha() bool {
	for i := 3; i < len(m.Pix); i += 4 {
		if m.Pix[i] != 255 {
			return true
		}
	}
	return false
}

// Upload8 expands palette indices to RGBA the way GL_Upload8 fills its
// trans[] buffer before calling GL_Upload32.
// C: ref_gl/gl_image.c:1165 GL_Upload8
func Upload8(data []byte, width, height int, t *Table) *RGBA {
	s := width * height
	out := &RGBA{Width: width, Height: height, Pix: make([]byte, 4*s)}
	for i := 0; i < s; i++ {
		p := int(data[i])
		v := t[p]
		if p == 255 {
			// transparent, so scan around for another color to avoid alpha fringes
			switch {
			case i > width && data[i-width] != 255:
				p = int(data[i-width])
			case i < s-width && data[i+width] != 255:
				p = int(data[i+width])
			case i > 0 && data[i-1] != 255:
				p = int(data[i-1])
			case i < s-1 && data[i+1] != 255:
				p = int(data[i+1])
			default:
				p = 0
			}
			// copy rgb components, keep the alpha of entry 255
			v = (v & 0xff000000) | (t[p] & 0x00ffffff)
		}
		o := out.Pix[4*i : 4*i+4]
		o[0] = byte(v)
		o[1] = byte(v >> 8)
		o[2] = byte(v >> 16)
		o[3] = byte(v >> 24)
	}
	return out
}

const (
	floodFillFIFOSize = 0x1000
	floodFillFIFOMask = floodFillFIFOSize - 1
)

// FloodFillSkin fills the background of a skin (the colour of texel 0,
// flood-connected from the top-left corner) with the colour of an adjacent
// non-background texel, so mipmapping doesn't produce haloes. It modifies
// skin in place. The FIFO is 4096 entries and wraps silently, exactly as C.
// C: ref_gl/gl_image.c:761 R_FloodFillSkin
func FloodFillSkin(skin []byte, skinwidth, skinheight int, t *Table) {
	if len(skin) == 0 || skinwidth <= 0 || skinheight <= 0 {
		return
	}
	fillcolor := skin[0] // assume this is the pixel to fill
	type floodfill struct{ x, y int16 }
	var fifo [floodFillFIFOSize]floodfill
	inpt, outpt := 0, 0

	filledcolor := 0
	// attempt to find opaque black
	for i := 0; i < 256; i++ {
		if t[i] == (255 << 0) { // alpha 1.0
			filledcolor = i
			break
		}
	}

	// can't fill to filled color or to transparent color (used as visited marker)
	if int(fillcolor) == filledcolor || fillcolor == 255 {
		return
	}

	fifo[inpt] = floodfill{0, 0}
	inpt = (inpt + 1) & floodFillFIFOMask

	for outpt != inpt {
		x, y := int(fifo[outpt].x), int(fifo[outpt].y)
		fdc := filledcolor
		pos := x + skinwidth*y
		outpt = (outpt + 1) & floodFillFIFOMask

		step := func(off, dx, dy int) {
			if skin[pos+off] == fillcolor {
				skin[pos+off] = 255
				fifo[inpt] = floodfill{int16(x + dx), int16(y + dy)}
				inpt = (inpt + 1) & floodFillFIFOMask
			} else if skin[pos+off] != 255 {
				fdc = int(skin[pos+off])
			}
		}
		if x > 0 {
			step(-1, -1, 0)
		}
		if x < skinwidth-1 {
			step(1, 1, 0)
		}
		if y > 0 {
			step(-skinwidth, 0, -1)
		}
		if y < skinheight-1 {
			step(skinwidth, 0, 1)
		}
		skin[x+skinwidth*y] = byte(fdc)
	}
}

// Image returns the RGBA as an *image.NRGBA sharing Pix.
func (m *RGBA) Image() *image.NRGBA {
	return &image.NRGBA{Pix: m.Pix, Stride: 4 * m.Width, Rect: image.Rect(0, 0, m.Width, m.Height)}
}

// EncodePNG encodes the RGBA losslessly as a non-premultiplied PNG. Images
// without transparency are written as 8-bit RGB (the encoder drops the alpha
// channel when every texel is opaque).
func (m *RGBA) EncodePNG() ([]byte, error) {
	var buf bytes.Buffer
	enc := png.Encoder{CompressionLevel: png.DefaultCompression}
	if err := enc.Encode(&buf, m.Image()); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// PalettePNG encodes the palette as a 16x16 RGB PNG (index = y*16+x), handy
// for tools; the canonical export is the raw 768 bytes.
func (p *Palette) PalettePNG() ([]byte, error) {
	m := &RGBA{Width: 16, Height: 16, Pix: make([]byte, 4*256)}
	for i := 0; i < 256; i++ {
		m.Pix[4*i+0] = p[i*3+0]
		m.Pix[4*i+1] = p[i*3+1]
		m.Pix[4*i+2] = p[i*3+2]
		m.Pix[4*i+3] = 255
	}
	return m.EncodePNG()
}
