package tga

import (
	"bytes"
	"encoding/binary"
	"errors"
	"runtime"
	"testing"

	"quake2web/server/internal/assets/pak"
	"quake2web/server/internal/testutil"
)

func header(typ, bpp uint8, w, h uint16) []byte {
	b := make([]byte, HeaderSize)
	b[2] = typ
	binary.LittleEndian.PutUint16(b[12:], w)
	binary.LittleEndian.PutUint16(b[14:], h)
	b[16] = bpp
	return b
}

func TestUncompressed24(t *testing.T) {
	// 2x2, stored bottom-up, BGR
	data := append(header(2, 24, 2, 2),
		1, 2, 3, 4, 5, 6, // bottom row
		7, 8, 9, 10, 11, 12) // top row
	im, err := Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{9, 8, 7, 255, 12, 11, 10, 255, 3, 2, 1, 255, 6, 5, 4, 255}
	if !bytes.Equal(im.Pix, want) {
		t.Fatalf("pix %v", im.Pix)
	}
}

func TestRLE32SpansRows(t *testing.T) {
	// 2x2, one run packet of 3 pixels spanning rows then one raw pixel
	data := append(header(10, 32, 2, 2), 0x82, 1, 2, 3, 4, 0x00, 5, 6, 7, 8)
	im, err := Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{3, 2, 1, 4, 7, 6, 5, 8, 3, 2, 1, 4, 3, 2, 1, 4}
	if !bytes.Equal(im.Pix, want) {
		t.Fatalf("pix %v", im.Pix)
	}
}

func TestRejects(t *testing.T) {
	if _, err := Decode(header(1, 24, 1, 1)); !errors.Is(err, ErrType) {
		t.Error(err)
	}
	if _, err := Decode(header(2, 16, 1, 1)); !errors.Is(err, ErrDepth) {
		t.Error(err)
	}
	h := header(2, 24, 1, 1)
	h[1] = 1
	if _, err := Decode(h); !errors.Is(err, ErrDepth) {
		t.Error(err)
	}
	if _, err := Decode(header(2, 24, 2, 2)); !errors.Is(err, ErrShort) {
		t.Error(err)
	}
}

func TestDemoSky(t *testing.T) {
	p, err := pak.Open(testutil.DemoPak(t))
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	raw, err := p.ReadFile("env/unit1_rt.tga")
	if err != nil {
		t.Fatal(err)
	}
	im, err := Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	if im.Width != 256 || im.Height != 256 {
		t.Fatal(im.Width, im.Height)
	}
	// top-left output pixel is the first pixel of the last stored row
	o := HeaderSize + 255*256*3
	if im.Pix[0] != raw[o+2] || im.Pix[1] != raw[o+1] || im.Pix[2] != raw[o] || im.Pix[3] != 255 {
		t.Fatal("flip/BGR mismatch")
	}
}

func FuzzDecode(f *testing.F) {
	f.Add(append(header(10, 32, 2, 2), 0x82, 1, 2, 3, 4, 0x00, 5, 6, 7, 8))
	f.Add(append(header(2, 24, 1, 1), 1, 2, 3))
	f.Fuzz(func(t *testing.T, data []byte) {
		im, err := Decode(data)
		if err == nil && len(im.Pix) != 4*im.Width*im.Height {
			t.Fatal("size mismatch")
		}
	})
}

// A small RLE file must not make Decode allocate gigabytes: the old bound
// (pixels <= 128*len(data)) let a 2 MiB entry claim 16384x16384 and
// allocate 1 GiB of RGBA (65535x65535 from 34 MiB: 16 GiB), which, with
// ingest workers running in parallel, OOM-kills the whole server.
func TestRLEHugeDimensionsRejectedWithoutAllocation(t *testing.T) {
	data := append(header(10, 24, 16384, 16384), make([]byte, 2200000)...)
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	_, err := Decode(data)
	runtime.ReadMemStats(&after)
	if err == nil {
		t.Fatal("16384x16384 RLE image accepted")
	}
	if got := after.TotalAlloc - before.TotalAlloc; got > 256<<20 {
		t.Fatalf("Decode allocated %d MiB for a %d KiB input", got>>20, len(data)>>10)
	}
}
