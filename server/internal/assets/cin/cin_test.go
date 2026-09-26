package cin

import (
	"encoding/binary"
	"errors"
	"testing"
)

func build(frames int, withPalette bool, end bool) []byte {
	le := binary.LittleEndian
	b := make([]byte, HeaderSize+HuffTableSize)
	for i, v := range []uint32{320, 240, 22050, 2, 1} {
		le.PutUint32(b[i*4:], v)
	}
	for f := 0; f < frames; f++ {
		cmd := uint32(0)
		if f == 0 && withPalette {
			cmd = 1
		}
		b = le.AppendUint32(b, cmd)
		if cmd == 1 {
			b = append(b, make([]byte, 768)...)
		}
		b = le.AppendUint32(b, 10)
		b = append(b, make([]byte, 10)...)
		count := (f+1)*22050/14 - f*22050/14
		b = append(b, make([]byte, count*2)...)
	}
	if end {
		b = le.AppendUint32(b, 2)
	}
	return b
}

func TestParse(t *testing.T) {
	h, err := Parse(build(3, true, true))
	if err != nil {
		t.Fatal(err)
	}
	if h.Width != 320 || h.Height != 240 || h.Rate != 22050 || h.SampWidth != 2 || h.Channels != 1 || h.NumFrames != 3 || h.Palettes != 1 {
		t.Fatalf("%+v", h)
	}
	if h, err := Parse(build(2, false, false)); err != nil || h.NumFrames != 2 {
		t.Fatalf("%+v %v", h, err)
	}
	b := build(2, false, false)
	if _, err := Parse(b[:len(b)-5]); !errors.Is(err, ErrBad) {
		t.Fatal("truncated frame accepted")
	}
	if _, err := Parse(b[:100]); !errors.Is(err, ErrBad) {
		t.Fatal("short header accepted")
	}
}

func FuzzParse(f *testing.F) {
	f.Add(build(1, true, true))
	f.Fuzz(func(t *testing.T, data []byte) { Parse(data) })
}

// Huge sound parameters must not overflow the int64 read position (which
// then went negative and panicked on data[p:], crashing the ingest worker
// and with it the whole server).
func TestParseSoundSizeOverflow(t *testing.T) {
	le := binary.LittleEndian
	b := make([]byte, HeaderSize+HuffTableSize)
	le.PutUint32(b[8:], 14<<27) // rate: 2^27 samples in frame 0
	le.PutUint32(b[12:], 1<<30) // sample width
	le.PutUint32(b[16:], 1<<6)  // channels: 2^27*2^30*2^6 = 2^63
	b = le.AppendUint32(b, 0)   // command
	b = le.AppendUint32(b, 1)   // compressed size
	b = append(b, 0)            // compressed data
	b = append(b, make([]byte, 16)...)
	if _, err := Parse(b); !errors.Is(err, ErrBad) {
		t.Fatalf("err = %v, want ErrBad", err)
	}
}
