package sp2

import (
	"encoding/binary"
	"errors"
	"testing"

	"quake2web/server/internal/assets/pak"
	"quake2web/server/internal/q2const"
	"quake2web/server/internal/testutil"
)

func build(n int32, frames int) []byte {
	b := make([]byte, 12+frames*FrameSize)
	le := binary.LittleEndian
	le.PutUint32(b[0:], q2const.IDSPRITEHEADER)
	le.PutUint32(b[4:], 2)
	le.PutUint32(b[8:], uint32(n))
	for i := 0; i < frames; i++ {
		o := 12 + i*FrameSize
		le.PutUint32(b[o:], 32)
		le.PutUint32(b[o+4:], 16)
		le.PutUint32(b[o+8:], 16)
		le.PutUint32(b[o+12:], 8)
		copy(b[o+16:], "sprites/f.pcx")
	}
	return b
}

func TestParse(t *testing.T) {
	s, err := Parse(build(2, 2))
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Frames) != 2 || s.Frames[1] != (Frame{32, 16, 16, 8, "sprites/f.pcx"}) {
		t.Fatalf("%+v", s.Frames)
	}
	if _, err := Parse(build(33, 0)); !errors.Is(err, ErrBad) {
		t.Error("33 frames accepted")
	}
	if s, err := Parse(build(-1, 0)); err != nil || len(s.Frames) != 0 {
		t.Error("negative frame count should load no frames")
	}
	if _, err := Parse(build(3, 2)); !errors.Is(err, ErrBad) {
		t.Error("truncated accepted")
	}
	b := build(1, 1)
	b[4] = 1
	if _, err := Parse(b); !errors.Is(err, ErrBad) {
		t.Error("bad version accepted")
	}
}

func TestDemoSprites(t *testing.T) {
	p, err := pak.Open(testutil.DemoPak(t))
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	raw, err := p.ReadFile("sprites/s_explod.sp2")
	if err != nil {
		t.Fatal(err)
	}
	s, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if s.NumFrames != 6 || s.Frames[0] != (Frame{56, 56, 28, 28, "sprites/s_explod_0.pcx"}) {
		t.Fatalf("%+v", s)
	}
}

func FuzzParse(f *testing.F) {
	f.Add(build(2, 2))
	f.Fuzz(func(t *testing.T, data []byte) { Parse(data) })
}
