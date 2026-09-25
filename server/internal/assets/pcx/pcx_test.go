package pcx

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"

	"quake2web/server/internal/assets/pak"
	"quake2web/server/internal/testutil"
)

// Encode builds a minimal 8-bit version 5 RLE PCX (test helper).
func encode(w, h int, pix []byte, pal []byte) []byte {
	var hdr [HeaderSize]byte
	hdr[0], hdr[1], hdr[2], hdr[3] = 0x0a, 5, 1, 8
	binary.LittleEndian.PutUint16(hdr[8:], uint16(w-1))
	binary.LittleEndian.PutUint16(hdr[10:], uint16(h-1))
	binary.LittleEndian.PutUint16(hdr[66:], uint16(w))
	out := append([]byte(nil), hdr[:]...)
	for y := 0; y < h; y++ {
		row := pix[y*w : (y+1)*w]
		for x := 0; x < w; {
			c := row[x]
			n := 1
			for x+n < w && row[x+n] == c && n < 63 {
				n++
			}
			if n > 1 || c&0xC0 == 0xC0 {
				out = append(out, 0xC0|byte(n), c)
			} else {
				out = append(out, c)
			}
			x += n
		}
	}
	out = append(out, 0x0c)
	return append(out, pal...)
}

func testPal() []byte {
	pal := make([]byte, PaletteSize)
	for i := range pal {
		pal[i] = byte(i)
	}
	return pal
}

func TestDecodeSynthetic(t *testing.T) {
	pix := []byte{1, 1, 1, 200, 2, 3, 3, 3, 255, 255, 255, 0}
	data := encode(4, 3, pix, testPal())
	im, err := Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	if im.Width != 4 || im.Height != 3 || !bytes.Equal(im.Pix, pix) {
		t.Fatalf("got %dx%d %v", im.Width, im.Height, im.Pix)
	}
	if !bytes.Equal(im.Palette, testPal()) {
		t.Fatal("palette mismatch")
	}
}

func TestDecodeRejects(t *testing.T) {
	good := encode(2, 2, []byte{1, 2, 3, 4}, testPal())
	cases := map[string]func([]byte){
		"manufacturer": func(b []byte) { b[0] = 9 },
		"version":      func(b []byte) { b[1] = 4 },
		"encoding":     func(b []byte) { b[2] = 0 },
		"bpp":          func(b []byte) { b[3] = 4 },
		"xmax":         func(b []byte) { binary.LittleEndian.PutUint16(b[8:], 640) },
		"ymax":         func(b []byte) { binary.LittleEndian.PutUint16(b[10:], 480) },
	}
	for name, mut := range cases {
		b := append([]byte(nil), good...)
		mut(b)
		if _, err := Decode(b); !errors.Is(err, ErrBad) {
			t.Errorf("%s: err = %v, want ErrBad", name, err)
		}
	}
	// 639x479 is the largest accepted size
	b := append([]byte(nil), good...)
	binary.LittleEndian.PutUint16(b[8:], 639)
	if _, err := Decode(b); errors.Is(err, ErrBad) {
		t.Error("xmax 639 should pass the header check")
	}
	// truncated RLE stream
	short := append([]byte(nil), good[:HeaderSize+1]...)
	if _, err := Decode(short); err == nil {
		t.Error("truncated file accepted")
	}
}

func TestRunSpillsIntoNextRow(t *testing.T) {
	// a 2x2 image whose first row is one run of 3: the spill is overwritten
	// by row 2, as in C.
	var hdr [HeaderSize]byte
	hdr[0], hdr[1], hdr[2], hdr[3] = 0x0a, 5, 1, 8
	binary.LittleEndian.PutUint16(hdr[8:], 1)
	binary.LittleEndian.PutUint16(hdr[10:], 1)
	data := append(hdr[:], 0xC3, 7, 8, 9)
	data = append(data, testPal()...)
	im, err := Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(im.Pix, []byte{7, 7, 8, 9}) {
		t.Fatalf("pix = %v", im.Pix)
	}
}

func TestDemoFiles(t *testing.T) {
	p, err := pak.Open(testutil.DemoPak(t))
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	for _, c := range []struct {
		name string
		w, h int
	}{{"pics/colormap.pcx", 256, 320}, {"pics/conchars.pcx", 128, 128}} {
		raw, err := p.ReadFile(c.name)
		if err != nil {
			t.Fatal(err)
		}
		im, err := Decode(raw)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if im.Width != c.w || im.Height != c.h {
			t.Errorf("%s: %dx%d", c.name, im.Width, im.Height)
		}
	}
	raw, _ := p.ReadFile("pics/colormap.pcx")
	im, _ := Decode(raw)
	// well-known Quake II palette entries
	for i, want := range map[int][3]byte{0: {0, 0, 0}, 15: {235, 235, 235}, 208: {0, 255, 0}, 255: {159, 91, 83}} {
		got := [3]byte{im.Palette[i*3], im.Palette[i*3+1], im.Palette[i*3+2]}
		if got != want {
			t.Errorf("palette[%d] = %v, want %v", i, got, want)
		}
	}
}

func FuzzDecode(f *testing.F) {
	f.Add(encode(4, 3, []byte{1, 1, 1, 200, 2, 3, 3, 3, 255, 255, 255, 0}, testPal()))
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, data []byte) {
		im, err := Decode(data)
		if err == nil && len(im.Pix) != im.Width*im.Height {
			t.Fatal("pixel buffer size mismatch")
		}
	})
}
