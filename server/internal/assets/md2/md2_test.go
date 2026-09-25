package md2

import (
	"encoding/binary"
	"errors"
	"math"
	"testing"

	"quake2web/server/internal/assets/pak"
	"quake2web/server/internal/q2const"
	"quake2web/server/internal/testutil"
)

// build makes a valid 1-frame model with the given skins.
func build(skins ...string) []byte {
	const numXYZ, numST, numTris, numGL = 3, 3, 1, 1
	frameSize := 40 + numXYZ*4
	ofsSkins := HeaderSize
	ofsST := ofsSkins + len(skins)*64
	ofsTris := ofsST + numST*4
	ofsFrames := ofsTris + numTris*12
	ofsGL := ofsFrames + frameSize
	ofsEnd := ofsGL + numGL*4
	b := make([]byte, ofsEnd)
	v := []int32{q2const.IDALIASHEADER, 8, 64, 32, int32(frameSize), int32(len(skins)), numXYZ, numST, numTris, numGL, 1,
		int32(ofsSkins), int32(ofsST), int32(ofsTris), int32(ofsFrames), int32(ofsGL), int32(ofsEnd)}
	for i, x := range v {
		binary.LittleEndian.PutUint32(b[i*4:], uint32(x))
	}
	for i, s := range skins {
		copy(b[ofsSkins+i*64:], s)
	}
	binary.LittleEndian.PutUint32(b[ofsFrames:], math.Float32bits(0.5))
	copy(b[ofsFrames+24:], "stand01")
	return b
}

func setField(b []byte, i int, v int32) []byte {
	c := append([]byte(nil), b...)
	binary.LittleEndian.PutUint32(c[i*4:], uint32(v))
	return c
}

func TestParseSynthetic(t *testing.T) {
	m, err := Parse(build("models/a/skin.pcx", "models/a/pain.pcx"))
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Skins) != 2 || m.Skins[1] != "models/a/pain.pcx" || m.Frames[0].Name != "stand01" || m.Frames[0].Scale[0] != 0.5 {
		t.Fatalf("%+v", m)
	}
}

func TestValidation(t *testing.T) {
	good := build("s.pcx")
	cases := []struct {
		field int
		v     int32
	}{
		{0, 1},        // ident
		{1, 7},        // version
		{3, 481},      // skinheight > MAX_LBM_HEIGHT
		{6, 0},        // num_xyz <= 0
		{6, 2049},     // num_xyz > MAX_VERTS
		{7, 0},        // num_st
		{8, 0},        // num_tris
		{10, 0},       // num_frames
		{5, 33},       // num_skins > MAX_MD2SKINS
		{11, 1 << 20}, // skins out of bounds
		{14, -4},      // frames out of bounds
		{4, 8},        // framesize too small
	}
	for _, c := range cases {
		if _, err := Parse(setField(good, c.field, c.v)); !errors.Is(err, ErrBad) {
			t.Errorf("field %d = %d: err %v", c.field, c.v, err)
		}
	}
	if _, err := Parse(setField(good, 3, 480)); err != nil {
		t.Errorf("skinheight 480 must pass: %v", err)
	}
}

func TestDemoModels(t *testing.T) {
	p, err := pak.Open(testutil.DemoPak(t))
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	raw, err := p.ReadFile("models/items/ammo/bullets/medium/tris.md2")
	if err != nil {
		t.Fatal(err)
	}
	m, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	h := m.Header
	if h.SkinWidth != 136 || h.SkinHeight != 60 || h.NumXYZ != 8 || h.NumTris != 12 || h.NumFrames != 1 ||
		len(m.Skins) != 1 || m.Skins[0] != "models/items/ammo/bullets/medium/skin.pcx" {
		t.Fatalf("%+v %v", h, m.Skins)
	}
	n := 0
	for i, f := range p.List() {
		if len(f.Name) > 4 && f.Name[len(f.Name)-4:] == ".md2" {
			raw, _ := p.ReadEntry(i)
			if _, err := Parse(raw); err != nil {
				t.Errorf("%s: %v", f.Name, err)
			}
			n++
		}
	}
	if n != 104 {
		t.Errorf("%d md2 files", n)
	}
}

func FuzzParse(f *testing.F) {
	f.Add(build("s.pcx"))
	f.Fuzz(func(t *testing.T, data []byte) {
		m, err := Parse(data)
		if err == nil && len(m.Frames) != int(m.Header.NumFrames) {
			t.Fatal("frame count mismatch")
		}
	})
}
