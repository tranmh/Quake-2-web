package wav

import (
	"encoding/binary"
	"errors"
	"testing"

	"quake2web/server/internal/assets/pak"
	"quake2web/server/internal/testutil"
)

type chunk struct {
	id   string
	body []byte
}

func riff(chunks ...chunk) []byte {
	var body []byte
	body = append(body, "WAVE"...)
	for _, c := range chunks {
		body = append(body, c.id...)
		body = binary.LittleEndian.AppendUint32(body, uint32(len(c.body)))
		body = append(body, c.body...)
		if len(c.body)&1 == 1 {
			body = append(body, 0)
		}
	}
	out := append([]byte("RIFF"), binary.LittleEndian.AppendUint32(nil, uint32(len(body)))...)
	return append(out, body...)
}

func fmtChunk(format, channels uint16, rate uint32, bits uint16) chunk {
	b := binary.LittleEndian.AppendUint16(nil, format)
	b = binary.LittleEndian.AppendUint16(b, channels)
	b = binary.LittleEndian.AppendUint32(b, rate)
	b = binary.LittleEndian.AppendUint32(b, rate*uint32(channels)*uint32(bits/8))
	b = binary.LittleEndian.AppendUint16(b, channels*bits/8)
	b = binary.LittleEndian.AppendUint16(b, bits)
	return chunk{"fmt ", b}
}

func cue(loop uint32) chunk {
	b := make([]byte, 28)
	binary.LittleEndian.PutUint32(b[0:], 1)
	binary.LittleEndian.PutUint32(b[24:], loop) // data_p += 32 from the chunk id
	return chunk{"cue ", b}
}

func markList(n uint32) chunk {
	b := make([]byte, 24)
	copy(b[0:], "adtl")
	copy(b[20:], "mark") // data_p + 28 from the chunk id
	b = binary.LittleEndian.AppendUint32(b[:16], n)
	b = append(b, "mark"...)
	return chunk{"LIST", b}
}

func TestBasic(t *testing.T) {
	w := riff(fmtChunk(1, 1, 22050, 16), chunk{"data", make([]byte, 100)})
	info, err := GetWavinfo("x", w)
	if err != nil {
		t.Fatal(err)
	}
	if info != (Info{Rate: 22050, Width: 2, Channels: 1, LoopStart: -1, Samples: 50, DataOfs: 44}) {
		t.Fatalf("%+v", info)
	}
}

func TestCueAndMark(t *testing.T) {
	w := riff(fmtChunk(1, 1, 11025, 8), chunk{"data", make([]byte, 100)}, cue(10), markList(40))
	info, err := GetWavinfo("x", w)
	if err != nil {
		t.Fatal(err)
	}
	if info.LoopStart != 10 || info.Samples != 50 || info.Width != 1 {
		t.Fatalf("%+v", info)
	}
	// loop longer than the data: Com_Error(ERR_DROP)
	w = riff(fmtChunk(1, 1, 11025, 8), chunk{"data", make([]byte, 20)}, cue(10), markList(40))
	if _, err := GetWavinfo("x", w); !errors.Is(err, ErrLoopLength) {
		t.Fatalf("err = %v", err)
	}
}

func TestErrors(t *testing.T) {
	if _, err := GetWavinfo("x", []byte("RIFX")); !errors.Is(err, ErrNoRIFF) {
		t.Error(err)
	}
	if _, err := GetWavinfo("x", riff(chunk{"data", nil})); !errors.Is(err, ErrNoFmt) {
		t.Error(err)
	}
	if _, err := GetWavinfo("x", riff(fmtChunk(3, 1, 22050, 32), chunk{"data", nil})); !errors.Is(err, ErrNotPCM) {
		t.Error(err)
	}
	if _, err := GetWavinfo("x", riff(fmtChunk(1, 1, 22050, 16))); !errors.Is(err, ErrNoData) {
		t.Error(err)
	}
	if _, err := GetWavinfo("x", riff(fmtChunk(1, 1, 22050, 4), chunk{"data", make([]byte, 4)})); !errors.Is(err, ErrZeroWidth) {
		t.Error(err)
	}
}

func TestDemoSounds(t *testing.T) {
	p, err := pak.Open(testutil.DemoPak(t))
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	raw, err := p.ReadFile("sound/world/amb1.wav")
	if err != nil {
		t.Fatal(err)
	}
	info, err := GetWavinfo("world/amb1.wav", raw)
	if err != nil {
		t.Fatal(err)
	}
	// cue chunk at offset 49724 with sample offset 0; the LIST chunk after it
	// is "adtl/ltxt", not "mark", so samples come from the data chunk.
	if info != (Info{Rate: 22050, Width: 2, Channels: 1, LoopStart: 0, Samples: 24840, DataOfs: 44}) {
		t.Fatalf("%+v", info)
	}
	for i, f := range p.List() {
		n := len(f.Name)
		if n > 4 && (f.Name[n-4:] == ".wav" || f.Name[n-4:] == ".WAV") {
			raw, _ := p.ReadEntry(i)
			if info, err := GetWavinfo(f.Name, raw); err != nil || info.Rate == 0 {
				t.Errorf("%s: %v %+v", f.Name, err, info)
			}
		}
	}
}

func FuzzGetWavinfo(f *testing.F) {
	f.Add(riff(fmtChunk(1, 1, 11025, 8), chunk{"data", make([]byte, 100)}, cue(10), markList(40)))
	f.Fuzz(func(t *testing.T, data []byte) {
		info, err := GetWavinfo("f", data)
		if err == nil && (info.DataOfs < 0 || int(info.DataOfs) > len(data)) {
			t.Fatalf("dataofs %d out of range", info.DataOfs)
		}
	})
}
