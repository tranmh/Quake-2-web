// Package wav ports the RIFF chunk walker and GetWavinfo of
// client/snd_mem.c. Sounds are served verbatim; the parsed info goes into the
// asset index.
package wav

import (
	"errors"
	"fmt"
)

// Info is C wavinfo_t (client/snd_loc.h:92).
type Info struct {
	Rate      int32 `json:"rate"`
	Width     int32 `json:"width"` // bytes per sample
	Channels  int32 `json:"channels"`
	LoopStart int32 `json:"loopStart"` // -1 when there is no cue chunk
	Samples   int32 `json:"samples"`
	DataOfs   int32 `json:"dataOfs"` // chunk starts this many bytes from file start
}

// Errors mirroring GetWavinfo's messages. For the "Missing"/"PCM only" cases
// C returns a zeroed wavinfo_t (S_LoadSound then fails because rate is 0).
var (
	ErrNoRIFF      = errors.New("Missing RIFF/WAVE chunks")
	ErrNoFmt       = errors.New("Missing fmt chunk")
	ErrNotPCM      = errors.New("Microsoft PCM format only")
	ErrNoData      = errors.New("Missing data chunk")
	ErrLoopLength  = errors.New("has a bad loop length")
	ErrTruncated   = errors.New("wav: truncated")
	ErrZeroWidth   = errors.New("wav: zero sample width")
	errOutOfBounds = errors.New("oob")
)

// parser holds the C file-scope iff state (data_p, iff_end, last_chunk,
// iff_data, iff_chunk_len). data_p == -1 is the C NULL.
type parser struct {
	wav         []byte
	dataP       int64
	iffEnd      int64
	lastChunk   int64
	iffData     int64
	iffChunkLen int32
	outOfBounds bool
}

func (p *parser) byteAt(i int64) byte {
	if i < 0 || i >= int64(len(p.wav)) {
		p.outOfBounds = true
		return 0
	}
	return p.wav[i]
}

// C: client/snd_mem.c:190 GetLittleShort
func (p *parser) getLittleShort() int16 {
	v := uint16(p.byteAt(p.dataP)) | uint16(p.byteAt(p.dataP+1))<<8
	p.dataP += 2
	return int16(v)
}

// C: client/snd_mem.c:199 GetLittleLong
func (p *parser) getLittleLong() int32 {
	v := uint32(p.byteAt(p.dataP)) | uint32(p.byteAt(p.dataP+1))<<8 |
		uint32(p.byteAt(p.dataP+2))<<16 | uint32(p.byteAt(p.dataP+3))<<24
	p.dataP += 4
	return int32(v)
}

func (p *parser) match(at int64, name string) bool {
	for i := 0; i < 4; i++ {
		if at+int64(i) >= int64(len(p.wav)) || at+int64(i) < 0 {
			return false
		}
		if p.wav[at+int64(i)] != name[i] {
			return false
		}
	}
	return true
}

// findNextChunk walks chunks from last_chunk. A chunk header that does not
// fit in the buffer ends the search (C would read past the end).
// C: client/snd_mem.c:210 FindNextChunk
func (p *parser) findNextChunk(name string) {
	for {
		p.dataP = p.lastChunk
		if p.dataP >= p.iffEnd { // didn't find the chunk
			p.dataP = -1
			return
		}
		if p.dataP+8 > int64(len(p.wav)) { // memory-safety (not in C)
			p.dataP = -1
			return
		}
		p.dataP += 4
		p.iffChunkLen = p.getLittleLong()
		if p.iffChunkLen < 0 {
			p.dataP = -1
			return
		}
		p.dataP -= 8
		p.lastChunk = p.dataP + 8 + ((int64(p.iffChunkLen) + 1) &^ 1)
		if p.match(p.dataP, name) {
			return
		}
	}
}

// C: client/snd_mem.c:237 FindChunk
func (p *parser) findChunk(name string) {
	p.lastChunk = p.iffData
	p.findNextChunk(name)
}

// GetWavinfo parses the RIFF header of a .wav. It returns the same fields
// as C; on the "Missing"/"PCM" paths it returns a zero Info (LoopStart 0)
// together with the error, as C returns memset-zero info.
// C: client/snd_mem.c:267 GetWavinfo
func GetWavinfo(name string, wav []byte) (Info, error) {
	var info Info
	if len(wav) == 0 {
		return info, ErrNoRIFF
	}
	p := &parser{wav: wav, iffData: 0, iffEnd: int64(len(wav))}

	// find "RIFF" chunk
	p.findChunk("RIFF")
	if !(p.dataP >= 0 && p.match(p.dataP+8, "WAVE")) {
		return Info{}, ErrNoRIFF
	}

	// get "fmt " chunk
	p.iffData = p.dataP + 12

	p.findChunk("fmt ")
	if p.dataP < 0 {
		return Info{}, ErrNoFmt
	}
	p.dataP += 8
	format := p.getLittleShort()
	if format != 1 {
		if p.outOfBounds {
			return Info{}, ErrTruncated
		}
		return Info{}, ErrNotPCM
	}

	info.Channels = int32(p.getLittleShort())
	info.Rate = p.getLittleLong()
	p.dataP += 4 + 2
	info.Width = int32(p.getLittleShort() / 8)

	// get cue chunk
	p.findChunk("cue ")
	if p.dataP >= 0 {
		p.dataP += 32
		info.LoopStart = p.getLittleLong()

		// if the next chunk is a LIST chunk, look for a cue length marker
		p.findNextChunk("LIST")
		if p.dataP >= 0 {
			if p.match(p.dataP+28, "mark") {
				// this is not a proper parse, but it works with cooledit...
				p.dataP += 24
				i := p.getLittleLong() // samples in loop
				info.Samples = info.LoopStart + i
			}
		}
	} else {
		info.LoopStart = -1
	}

	// find data chunk
	p.findChunk("data")
	if p.dataP < 0 {
		return info, ErrNoData
	}

	p.dataP += 4
	datalen := p.getLittleLong()
	if p.outOfBounds {
		return info, ErrTruncated
	}
	if info.Width == 0 { // C: integer division by zero
		return info, ErrZeroWidth
	}
	samples := datalen / info.Width

	if info.Samples != 0 {
		if samples < info.Samples {
			return info, fmt.Errorf("Sound %s %w", name, ErrLoopLength)
		}
	} else {
		info.Samples = samples
	}

	info.DataOfs = int32(p.dataP)
	return info, nil
}
