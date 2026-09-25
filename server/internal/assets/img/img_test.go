package img

import (
	"bytes"
	"image/png"
	"testing"

	"quake2web/server/internal/assets/pak"
	"quake2web/server/internal/assets/pcx"
	"quake2web/server/internal/testutil"
)

func rampPalette() *Palette {
	var p Palette
	for i := 0; i < 256; i++ {
		p[i*3], p[i*3+1], p[i*3+2] = byte(i), byte(255-i), byte(i/2)
	}
	return &p
}

func TestTable(t *testing.T) {
	tb := rampPalette().Table()
	if tb[1] != 0xff000000|1|254<<8|0<<16 {
		t.Fatalf("%08x", tb[1])
	}
	if tb[255]>>24 != 0 {
		t.Fatal("index 255 must be transparent")
	}
}

func TestUpload8Fill(t *testing.T) {
	tb := rampPalette().Table()
	// 3x3 with transparent texels; check the up/down/left/right order and
	// the strict "i > width" test of GL_Upload8.
	data := []byte{
		10, 255, 12,
		255, 255, 255,
		40, 50, 255,
	}
	m := Upload8(data, 3, 3, &tb)
	px := func(i int) [4]byte { return [4]byte{m.Pix[4*i], m.Pix[4*i+1], m.Pix[4*i+2], m.Pix[4*i+3]} }
	col := func(p int, a byte) [4]byte { return [4]byte{byte(p), byte(255 - p), byte(p / 2), a} }
	want := map[int][4]byte{
		0: col(10, 255),
		1: col(10, 0), // i=1: up fails (i>width false), down 255, left 10
		3: col(40, 0), // i=3: i>width false (3>3), down = 40
		4: col(50, 0), // up 255 (i-width=1), down 50
		5: col(12, 0), // i=5: up = data[2] = 12
		8: col(50, 0), // i=8: up 255, down n/a, left 50
	}
	for i, w := range want {
		if px(i) != w {
			t.Errorf("texel %d = %v, want %v", i, px(i), w)
		}
	}
	if !m.HasAlpha() {
		t.Error("HasAlpha")
	}
	// all transparent → palette index 0 rgb
	m = Upload8([]byte{255}, 1, 1, &tb)
	if px(0) != col(0, 0) {
		t.Errorf("isolated 255 → %v", px(0))
	}
}

func TestFloodFillSkin(t *testing.T) {
	tb := rampPalette().Table()
	// background 7 connected from the corner; island of 3 in the middle;
	// a separate 7 region enclosed by 3 must stay 7.
	skin := []byte{
		7, 7, 7, 7,
		7, 3, 3, 7,
		7, 3, 7, 3,
		7, 7, 3, 9,
	}
	FloodFillSkin(skin, 4, 4, &tb)
	for i, v := range skin {
		if v == 255 {
			t.Fatalf("visited marker left at %d", i)
		}
	}
	if skin[10] != 7 {
		t.Errorf("enclosed region changed: %v", skin)
	}
	if skin[0] == 7 || skin[3] == 7 || skin[12] == 7 {
		t.Errorf("background not filled: %v", skin)
	}
	// fill colour 0 (no opaque "black" match) or 255 → untouched
	s2 := []byte{0, 0, 1, 1}
	FloodFillSkin(s2, 2, 2, &tb)
	if !bytes.Equal(s2, []byte{0, 0, 1, 1}) {
		t.Errorf("fill of filledcolor must be a no-op: %v", s2)
	}
}

func TestPNGRoundTrip(t *testing.T) {
	tb := rampPalette().Table()
	m := Upload8([]byte{1, 255, 3, 4}, 2, 2, &tb)
	b, err := m.EncodePNG()
	if err != nil {
		t.Fatal(err)
	}
	im, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	if im.Bounds().Dx() != 2 || im.Bounds().Dy() != 2 {
		t.Fatal(im.Bounds())
	}
}

// TestDemoConchars converts pics/conchars.pcx with the demo palette and
// checks pixels against the palette.
func TestDemoConchars(t *testing.T) {
	p, err := pak.Open(testutil.DemoPak(t))
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	raw, _ := p.ReadFile("pics/colormap.pcx")
	cm, err := pcx.Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	pal, _ := NewPalette(cm.Palette)
	tb := pal.Table()
	raw, _ = p.ReadFile("pics/conchars.pcx")
	im, err := pcx.Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	m := Upload8(im.Pix, im.Width, im.Height, &tb)
	b, _ := m.EncodePNG()
	dec, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	if dec.Bounds().Dx() != 128 || dec.Bounds().Dy() != 128 {
		t.Fatal(dec.Bounds())
	}
	opaque, transparent := 0, 0
	for i, idx := range im.Pix {
		x, y := i%128, i/128
		r, g, bb, a := dec.At(x, y).RGBA()
		if idx != 255 {
			if byte(r>>8) != pal[int(idx)*3] || byte(g>>8) != pal[int(idx)*3+1] || byte(bb>>8) != pal[int(idx)*3+2] || a != 0xffff {
				t.Fatalf("(%d,%d) index %d: got %d %d %d %d", x, y, idx, r>>8, g>>8, bb>>8, a>>8)
			}
			opaque++
		} else {
			if m.Pix[4*i+3] != 0 {
				t.Fatalf("(%d,%d) index 255 not transparent", x, y)
			}
			transparent++
		}
	}
	if opaque == 0 || transparent == 0 {
		t.Fatalf("opaque %d transparent %d", opaque, transparent)
	}
}

func FuzzUpload8(f *testing.F) {
	f.Add([]byte{1, 255, 3, 255, 255, 6}, uint8(3))
	f.Fuzz(func(t *testing.T, data []byte, w uint8) {
		width := int(w%16) + 1
		h := len(data) / width
		if h == 0 {
			return
		}
		tb := rampPalette().Table()
		pix := append([]byte(nil), data[:width*h]...)
		FloodFillSkin(pix, width, h, &tb)
		m := Upload8(pix, width, h, &tb)
		if len(m.Pix) != 4*width*h {
			t.Fatal("size")
		}
	})
}
