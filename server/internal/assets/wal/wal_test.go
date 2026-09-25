package wal

import (
	"encoding/binary"
	"testing"

	"quake2web/server/internal/assets/pak"
	"quake2web/server/internal/testutil"
)

func build(name string, w, h uint32, anim string, flags, contents, value int32) []byte {
	b := make([]byte, HeaderSize)
	copy(b[0:32], name)
	le := binary.LittleEndian
	le.PutUint32(b[32:], w)
	le.PutUint32(b[36:], h)
	ofs := uint32(HeaderSize)
	for i := 0; i < MipLevels; i++ {
		le.PutUint32(b[40+4*i:], ofs)
		ofs += (w >> i) * (h >> i)
	}
	copy(b[56:88], anim)
	le.PutUint32(b[88:], uint32(flags))
	le.PutUint32(b[92:], uint32(contents))
	le.PutUint32(b[96:], uint32(value))
	for i := uint32(0); i < ofs-HeaderSize; i++ {
		b = append(b, byte(i))
	}
	return b
}

func TestDecodeSynthetic(t *testing.T) {
	data := build("e1u1/water", 16, 8, "e1u1/water2", 0x20, 32, 7)
	m, pix, err := Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	if m.Name != "e1u1/water" || m.Width != 16 || m.Height != 8 || m.AnimName != "e1u1/water2" ||
		m.Flags != 0x20 || m.Contents != 32 || m.Value != 7 {
		t.Fatalf("header %+v", m)
	}
	if len(pix) != 128 || pix[5] != 5 {
		t.Fatalf("pix len %d", len(pix))
	}
	if _, _, err := Decode(data[:HeaderSize+10]); err == nil {
		t.Fatal("truncated mip accepted")
	}
	if _, _, err := Decode(data[:50]); err == nil {
		t.Fatal("truncated header accepted")
	}
}

func TestDemoWAL(t *testing.T) {
	p, err := pak.Open(testutil.DemoPak(t))
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	raw, err := p.ReadFile("textures/e1u1/metal2_1.wal")
	if err != nil {
		t.Fatal(err)
	}
	m, pix, err := Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	if m.Name != "e1u1/metal2_1" || m.Width != 64 || m.Height != 128 || m.Offsets != [4]uint32{100, 8292, 10340, 10852} {
		t.Fatalf("%+v", m)
	}
	if len(pix) != 64*128 {
		t.Fatal(len(pix))
	}
}

func FuzzDecode(f *testing.F) {
	f.Add(build("a", 4, 4, "", 0, 0, 0))
	f.Fuzz(func(t *testing.T, data []byte) {
		m, pix, err := Decode(data)
		if err == nil && len(pix) != int(m.Width*m.Height) {
			t.Fatal("size mismatch")
		}
	})
}
