// Package cin parses the header of .cin cinematics as SCR_PlayCinematic and
// Huff1TableInit read it (client/cl_cin.c) and walks the frame stream the
// way SCR_ReadNextFrame does to count frames. The file is served verbatim;
// decoding (Huffman) happens in the TypeScript client.
package cin

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// HeaderSize is width, height, s_rate, s_width, s_channels (5 ints).
const HeaderSize = 20

// HuffTableSize is the 256 rows of 256 byte counts read by Huff1TableInit.
const HuffTableSize = 256 * 256

// MaxCompressed is sizeof(compressed) in SCR_ReadNextFrame.
const MaxCompressed = 0x20000

// Header is the cinematic header plus stream statistics.
type Header struct {
	Width     int32 `json:"width"`
	Height    int32 `json:"height"`
	Rate      int32 `json:"rate"`        // s_rate
	SampWidth int32 `json:"sampleWidth"` // s_width
	Channels  int32 `json:"channels"`    // s_channels
	NumFrames int32 `json:"numFrames"`
	// Palettes counts frames with command 1 (a new 768-byte palette).
	Palettes int32 `json:"palettes"`
}

// ErrBad is returned for truncated or malformed files.
var ErrBad = errors.New("cin: bad cinematic")

// Parse reads the header and counts frames until the end marker (command 2)
// or the end of the file.
// C: client/cl_cin.c:576 SCR_PlayCinematic, :427 SCR_ReadNextFrame
func Parse(data []byte) (*Header, error) {
	if len(data) < HeaderSize+HuffTableSize {
		return nil, fmt.Errorf("%w: short header", ErrBad)
	}
	le := binary.LittleEndian
	h := &Header{
		Width:     int32(le.Uint32(data[0:])),
		Height:    int32(le.Uint32(data[4:])),
		Rate:      int32(le.Uint32(data[8:])),
		SampWidth: int32(le.Uint32(data[12:])),
		Channels:  int32(le.Uint32(data[16:])),
	}
	if h.Rate < 0 || h.SampWidth < 0 || h.Channels < 0 { // memory-safety (not in C)
		return h, fmt.Errorf("%w: negative sound parameters", ErrBad)
	}
	p := int64(HeaderSize + HuffTableSize)
	n := int64(len(data))
	for frame := int64(0); ; frame++ {
		if p+4 > n {
			break // fread of the command fails: end of cinematic
		}
		command := int32(le.Uint32(data[p:]))
		p += 4
		if command == 2 {
			break // last frame marker
		}
		if command == 1 { // read palette
			p += 768
			h.Palettes++
		}
		if p+4 > n {
			return h, fmt.Errorf("%w: frame %d truncated", ErrBad, frame)
		}
		size := int32(le.Uint32(data[p:]))
		p += 4
		if size > MaxCompressed || size < 1 {
			return h, fmt.Errorf("%w: Bad compressed frame size", ErrBad)
		}
		p += int64(size)
		start := frame * int64(h.Rate) / 14
		end := (frame + 1) * int64(h.Rate) / 14
		count := end - start
		p += count * int64(h.SampWidth) * int64(h.Channels)
		if p > n {
			return h, fmt.Errorf("%w: frame %d truncated", ErrBad, frame)
		}
		h.NumFrames++
	}
	return h, nil
}
