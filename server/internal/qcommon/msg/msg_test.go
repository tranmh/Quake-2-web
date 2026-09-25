package msg

import (
	"encoding/hex"
	"errors"
	"math"
	"math/rand"
	"testing"

	"quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/shared"
)

func hexOf(b *SizeBuf) string { return hex.EncodeToString(b.Bytes()) }

func TestScalarBytes(t *testing.T) {
	cases := []struct {
		name string
		fn   func(b *SizeBuf)
		want string
	}{
		{"char -1", func(b *SizeBuf) { b.MSG_WriteChar(-1) }, "ff"},
		{"byte 300", func(b *SizeBuf) { b.MSG_WriteByte(300) }, "2c"},
		{"short -2", func(b *SizeBuf) { b.MSG_WriteShort(-2) }, "feff"},
		{"short 70000", func(b *SizeBuf) { b.MSG_WriteShort(70000) }, "7011"},
		{"long", func(b *SizeBuf) { b.MSG_WriteLong(0x12345678) }, "78563412"},
		{"long min", func(b *SizeBuf) { b.MSG_WriteLong(math.MinInt32) }, "00000080"},
		{"float 1", func(b *SizeBuf) { b.MSG_WriteFloat(1) }, "0000803f"},
		{"float -0", func(b *SizeBuf) { b.MSG_WriteFloat(float32(math.Copysign(0, -1))) }, "00000080"},
		{"string", func(b *SizeBuf) { b.MSG_WriteString("hi") }, "686900"},
		{"string nul", func(b *SizeBuf) { b.MSG_WriteString("a\x00b") }, "6100"},
		{"coord 1.5", func(b *SizeBuf) { b.MSG_WriteCoord(1.5) }, "0c00"},
		{"coord -0.1", func(b *SizeBuf) { b.MSG_WriteCoord(-0.1) }, "0000"},
		{"coord -1.2", func(b *SizeBuf) { b.MSG_WriteCoord(-1.2) }, "f7ff"}, // (int)(-9.6) = -9
		{"pos", func(b *SizeBuf) { b.MSG_WritePos(shared.Vec3{1, -1, 0.125}) }, "0800f8ff0100"},
		{"angle 90", func(b *SizeBuf) { b.MSG_WriteAngle(90) }, "40"},
		{"angle -90", func(b *SizeBuf) { b.MSG_WriteAngle(-90) }, "c0"},
		{"angle 359.9", func(b *SizeBuf) { b.MSG_WriteAngle(359.9) }, "ff"},
		{"angle16 90", func(b *SizeBuf) { b.MSG_WriteAngle16(90) }, "0040"},
		{"angle16 -90", func(b *SizeBuf) { b.MSG_WriteAngle16(-90) }, "00c0"},
		{"dir up", func(b *SizeBuf) { b.MSG_WriteDir(&shared.Vec3{0, 0, 1}) }, "05"},
		{"dir nil", func(b *SizeBuf) { b.MSG_WriteDir(nil) }, "00"},
		{"dir zero", func(b *SizeBuf) { b.MSG_WriteDir(&shared.Vec3{}) }, "00"},
	}
	for _, c := range cases {
		b := NewSizeBuf(64)
		c.fn(b)
		if got := hexOf(b); got != c.want {
			t.Errorf("%s: got %s want %s", c.name, got, c.want)
		}
	}
}

func TestScalarRoundTrip(t *testing.T) {
	b := NewSizeBuf(4096)
	b.MSG_WriteChar(-100)
	b.MSG_WriteByte(200)
	b.MSG_WriteShort(-12345)
	b.MSG_WriteLong(-123456789)
	b.MSG_WriteFloat(3.25)
	b.MSG_WriteString("hello")
	b.MSG_WriteString("line1\nline2")
	b.MSG_WriteCoord(-100.625)
	b.MSG_WritePos(shared.Vec3{1.125, -2.5, 4095.875})
	b.MSG_WriteAngle(45)
	b.MSG_WriteAngle16(-45)
	for i := 0; i < q2const.NUMVERTEXNORMALS; i++ {
		d := q2const.ByteDirs[i]
		b.MSG_WriteDir(&d)
	}

	r := NewReader(b.Bytes())
	if v := r.MSG_ReadChar(); v != -100 {
		t.Errorf("char %d", v)
	}
	if v := r.MSG_ReadByte(); v != 200 {
		t.Errorf("byte %d", v)
	}
	if v := r.MSG_ReadShort(); v != -12345 {
		t.Errorf("short %d", v)
	}
	if v := r.MSG_ReadLong(); v != -123456789 {
		t.Errorf("long %d", v)
	}
	if v := r.MSG_ReadFloat(); v != 3.25 {
		t.Errorf("float %v", v)
	}
	if v := r.MSG_ReadString(); v != "hello" {
		t.Errorf("string %q", v)
	}
	if v := r.MSG_ReadStringLine(); v != "line1" {
		t.Errorf("stringline %q", v)
	}
	if v := r.MSG_ReadString(); v != "line2" {
		t.Errorf("string rest %q", v)
	}
	if v := r.MSG_ReadCoord(); v != -100.625 {
		t.Errorf("coord %v", v)
	}
	if v := r.MSG_ReadPos(); v != (shared.Vec3{1.125, -2.5, 4095.875}) {
		t.Errorf("pos %v", v)
	}
	if v := r.MSG_ReadAngle(); v != 45 {
		t.Errorf("angle %v", v)
	}
	if v := r.MSG_ReadAngle16(); v != -45 {
		t.Errorf("angle16 %v", v)
	}
	for i := 0; i < q2const.NUMVERTEXNORMALS; i++ {
		if v := r.MSG_ReadDir(); v != q2const.ByteDirs[i] {
			t.Fatalf("dir %d: %v", i, v)
		}
	}
	// past the end
	if r.MSG_ReadByte() != -1 || r.MSG_ReadShort() != -1 || r.MSG_ReadLong() != -1 || r.MSG_ReadFloat() != -1 {
		t.Error("reads past the end must return -1")
	}
	if r.ReadCount != b.CurSize+1+2+4+4 {
		t.Errorf("readcount %d", r.ReadCount)
	}
}

func TestReadStringQuirks(t *testing.T) {
	// 0xff reads as signed char -1 and terminates the string
	r := NewReader([]byte{'a', 0xff, 'b', 0})
	if s := r.MSG_ReadString(); s != "a" {
		t.Errorf("got %q", s)
	}
	if s := r.MSG_ReadString(); s != "b" {
		t.Errorf("got %q", s)
	}
	// 2047 char limit, remaining chars stay unread
	long := make([]byte, 3000)
	for i := range long {
		long[i] = 'x'
	}
	r = NewReader(long)
	if s := r.MSG_ReadString(); len(s) != 2047 || r.ReadCount != 2047 {
		t.Errorf("len %d readcount %d", len(s), r.ReadCount)
	}
	// ReadData past the end fills 0xff
	r = NewReader([]byte{1})
	d := make([]byte, 3)
	r.MSG_ReadData(d)
	if d[0] != 1 || d[1] != 0xff || d[2] != 0xff {
		t.Errorf("readdata %v", d)
	}
}

func TestReadDirOutOfRange(t *testing.T) {
	for _, in := range [][]byte{{162}, {255}, {}} {
		func() {
			defer func() {
				if e, ok := recover().(shared.ComError); !ok || e.Code != q2const.ERR_DROP {
					t.Errorf("%v: expected ERR_DROP, got %v", in, e)
				}
			}()
			NewReader(in).MSG_ReadDir()
		}()
	}
}

func TestOverflow(t *testing.T) {
	b := NewSizeBuf(4)
	func() {
		defer func() {
			if e, ok := recover().(shared.ComError); !ok || e.Code != q2const.ERR_FATAL {
				t.Errorf("expected ERR_FATAL panic, got %v", e)
			}
		}()
		b.MSG_WriteLong(1)
		b.MSG_WriteByte(1)
	}()
	b = NewSizeBuf(4)
	b.AllowOverflow = true
	b.MSG_WriteShort(0x1111)
	b.MSG_WriteShort(0x2222)
	b.MSG_WriteByte(0x33)
	if !b.Overflowed || b.CurSize != 1 || b.Data[0] != 0x33 {
		t.Errorf("overflow: %+v", b)
	}
	func() {
		defer func() {
			if _, ok := recover().(shared.ComError); !ok {
				t.Error("expected panic for length > maxsize")
			}
		}()
		b.SZ_GetSpace(5)
	}()
	b.SZ_Clear()
	if b.Overflowed || b.CurSize != 0 {
		t.Error("SZ_Clear")
	}
}

func TestSZPrint(t *testing.T) {
	b := NewSizeBuf(64)
	b.SZ_Print("abc")
	b.SZ_Print("def")
	if got := string(b.Bytes()); got != "abcdef\x00" {
		t.Errorf("got %q", got)
	}
	b.MSG_WriteByte('x')
	b.SZ_Print("g")
	if got := string(b.Bytes()); got != "abcdef\x00xg\x00" {
		t.Errorf("got %q", got)
	}
}

func randCoord(r *rand.Rand) float32 {
	return float32(r.Intn(65536)-32768) * 0.125
}

func randES(r *rand.Rand) shared.EntityState {
	var s shared.EntityState
	s.Number = int32(1 + r.Intn(1023))
	for k := 0; k < 3; k++ {
		s.Origin[k] = randCoord(r)
		s.Angles[k] = float32(r.Intn(256)-128) * (360.0 / 256)
		s.OldOrigin[k] = randCoord(r)
	}
	s.ModelIndex = int32(r.Intn(256))
	s.ModelIndex2 = int32(r.Intn(256))
	s.ModelIndex3 = int32(r.Intn(256))
	s.ModelIndex4 = int32(r.Intn(256))
	s.Frame = int32(r.Intn(32768))
	s.SkinNum = int32(r.Uint32())
	s.Effects = r.Uint32()
	s.RenderFX = int32(r.Uint32())
	s.Solid = int32(r.Intn(32768))
	s.Sound = int32(r.Intn(256))
	s.Event = int32(r.Intn(3))
	if r.Intn(2) == 0 {
		s.Frame &= 255
		s.SkinNum &= 255
		s.Effects &= 255
		s.RenderFX &= 255
	}
	return s
}

func TestDeltaEntityBytes(t *testing.T) {
	from := shared.EntityState{Number: 1}
	to := shared.EntityState{Number: 1, Origin: shared.Vec3{8, 0, 0}}
	b := NewSizeBuf(64)
	b.MSG_WriteDeltaEntity(&from, &to, false, false)
	if got := hexOf(b); got != "01014000" {
		t.Errorf("simple: %s", got)
	}
	b.SZ_Clear()
	b.MSG_WriteDeltaEntity(&from, &to, false, true)
	if got := hexOf(b); got != "81808001014000000000000000" {
		t.Errorf("newentity: %s", got)
	}
	b.SZ_Clear()
	b.MSG_WriteDeltaEntity(&from, &from, false, false)
	if b.CurSize != 0 {
		t.Errorf("unchanged, not forced: %s", hexOf(b))
	}
	b.MSG_WriteDeltaEntity(&from, &from, true, false)
	if got := hexOf(b); got != "0001" {
		t.Errorf("forced: %s", got)
	}
	b.SZ_Clear()
	big := shared.EntityState{Number: 300}
	b.MSG_WriteDeltaEntity(&big, &big, true, false)
	if got := hexOf(b); got != "80012c01" {
		t.Errorf("number16: %s", got)
	}
}

func TestDeltaEntityRoundTrip(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	b := NewSizeBuf(1024)
	for i := 0; i < 5000; i++ {
		from := randES(r)
		to := randES(r)
		if r.Intn(3) == 0 {
			to = from
			to.Origin[0] = randCoord(r)
		}
		to.Number = from.Number
		newent := r.Intn(4) == 0
		b.SZ_Clear()
		b.MSG_WriteDeltaEntity(&from, &to, true, newent)
		rd := NewReader(b.Bytes())
		got, num, bits := rd.ReadDeltaEntity(&from)
		if num != to.Number {
			t.Fatalf("case %d: number %d want %d", i, num, to.Number)
		}
		want := to
		if bits&q2const.U_OLDORIGIN == 0 {
			want.OldOrigin = from.Origin
		}
		// effects16 / renderfx16 / skin16 / frame16 are 16 bit on the wire
		// and sign-extended by the reader (C quirk).
		if bits&(q2const.U_EFFECTS8|q2const.U_EFFECTS16) == q2const.U_EFFECTS16 {
			want.Effects = uint32(int32(int16(want.Effects)))
		}
		if bits&(q2const.U_RENDERFX8|q2const.U_RENDERFX16) == q2const.U_RENDERFX16 {
			want.RenderFX = int32(int16(want.RenderFX))
		}
		// negative renderfx is "< 256" and goes out as one byte (C quirk)
		if bits&(q2const.U_RENDERFX8|q2const.U_RENDERFX16) == q2const.U_RENDERFX8 {
			want.RenderFX = int32(uint8(want.RenderFX))
		}
		if bits&(q2const.U_SKIN8|q2const.U_SKIN16) == q2const.U_SKIN16 {
			want.SkinNum = int32(int16(want.SkinNum))
		}
		if got != want {
			t.Fatalf("case %d:\n got  %+v\n want %+v\n bytes %s", i, got, want, hexOf(b))
		}
		if rd.ReadCount != rd.CurSize {
			t.Fatalf("case %d: consumed %d of %d", i, rd.ReadCount, rd.CurSize)
		}
	}
}

func TestDeltaEntityFatal(t *testing.T) {
	for _, n := range []int32{0, q2const.MAX_EDICTS} {
		func() {
			defer func() {
				if _, ok := recover().(shared.ComError); !ok {
					t.Errorf("number %d: expected ComError", n)
				}
			}()
			s := shared.EntityState{Number: n}
			NewSizeBuf(64).MSG_WriteDeltaEntity(&s, &s, true, false)
		}()
	}
}

func randUC(r *rand.Rand) shared.UserCmd {
	return shared.UserCmd{
		Msec: uint8(r.Intn(256)), Buttons: uint8(r.Intn(256)),
		Angles:      [3]int16{int16(r.Uint32()), int16(r.Uint32()), int16(r.Uint32())},
		ForwardMove: int16(r.Uint32()), SideMove: int16(r.Uint32()), UpMove: int16(r.Uint32()),
		Impulse: uint8(r.Intn(256)), LightLevel: uint8(r.Intn(256)),
	}
}

func TestDeltaUsercmd(t *testing.T) {
	var zero shared.UserCmd
	cmd := shared.UserCmd{Msec: 16, LightLevel: 7, ForwardMove: 400}
	b := NewSizeBuf(64)
	b.MSG_WriteDeltaUsercmd(&zero, &cmd)
	if got := hexOf(b); got != "08900110"+"07" {
		t.Errorf("bytes %s", got)
	}
	r := rand.New(rand.NewSource(2))
	for i := 0; i < 5000; i++ {
		from, to := randUC(r), randUC(r)
		if r.Intn(2) == 0 {
			to.Angles = from.Angles
			to.Buttons = from.Buttons
		}
		b.SZ_Clear()
		b.MSG_WriteDeltaUsercmd(&from, &to)
		rd := NewReader(b.Bytes())
		if got := rd.MSG_ReadDeltaUsercmd(&from); got != to || rd.ReadCount != rd.CurSize {
			t.Fatalf("case %d: got %+v want %+v", i, got, to)
		}
	}
}

func randPS(r *rand.Rand) shared.PlayerState {
	var ps shared.PlayerState
	ps.PMove.PmType = int32(r.Intn(5))
	for k := 0; k < 3; k++ {
		ps.PMove.Origin[k] = int16(r.Uint32())
		ps.PMove.Velocity[k] = int16(r.Uint32())
		ps.PMove.DeltaAngles[k] = int16(r.Uint32())
		ps.ViewAngles[k] = float32(shared.SHORT2ANGLE(int32(int16(r.Uint32()))))
		ps.ViewOffset[k] = float32(r.Intn(256)-128) * 0.25
		ps.KickAngles[k] = float32(r.Intn(256)-128) * 0.25
		ps.GunAngles[k] = float32(r.Intn(256)-128) * 0.25
		ps.GunOffset[k] = float32(r.Intn(256)-128) * 0.25
	}
	ps.PMove.PmFlags = uint8(r.Intn(256))
	ps.PMove.PmTime = uint8(r.Intn(256))
	ps.PMove.Gravity = int16(r.Uint32())
	ps.GunIndex = int32(r.Intn(256))
	ps.GunFrame = int32(r.Intn(256))
	for k := 0; k < 4; k++ {
		ps.Blend[k] = float32(float64(r.Intn(256)) / 255.0)
	}
	ps.Fov = float32(r.Intn(256))
	ps.RDFlags = int32(r.Intn(256))
	for k := range ps.Stats {
		if r.Intn(2) == 0 {
			ps.Stats[k] = int16(r.Uint32())
		}
	}
	return ps
}

func TestDeltaPlayerstate(t *testing.T) {
	var zero shared.PlayerState
	b := NewSizeBuf(1400)
	b.WriteDeltaPlayerstate(nil, &zero)
	if got := hexOf(b); got != "11001000"+"00000000" {
		t.Errorf("zero: %s", got)
	}
	r := rand.New(rand.NewSource(3))
	for i := 0; i < 3000; i++ {
		from, to := randPS(r), randPS(r)
		if r.Intn(2) == 0 {
			to.PMove = from.PMove
			to.Blend = from.Blend
		}
		fp := &from
		if r.Intn(5) == 0 {
			fp = nil
		}
		b.SZ_Clear()
		b.WriteDeltaPlayerstate(fp, &to)
		rd := NewReader(b.Bytes())
		if rd.MSG_ReadByte() != q2const.Svc_playerinfo {
			t.Fatal("missing svc_playerinfo")
		}
		got := rd.ReadDeltaPlayerstate(fp)
		want := to
		ops := zero
		if fp != nil {
			ops = *fp
		}
		// gunoffset/gunangles only travel with a gunframe change (C behavior)
		if to.GunFrame == ops.GunFrame {
			want.GunOffset, want.GunAngles = ops.GunOffset, ops.GunAngles
		}
		// blend goes through (int)(b*255) and /255.0; quantized inputs survive
		if got != want || rd.ReadCount != rd.CurSize {
			t.Fatalf("case %d:\n got  %+v\n want %+v", i, got, want)
		}
	}
}

// mustNotPanicExceptComError runs f and fails on any panic but ComError.
func mustNotPanicExceptComError(t *testing.T, f func()) {
	defer func() {
		if e := recover(); e != nil {
			var ce shared.ComError
			if err, ok := e.(error); !ok || !errors.As(err, &ce) {
				t.Fatalf("unexpected panic: %v", e)
			}
		}
	}()
	f()
}

func FuzzReadDeltaEntity(f *testing.F) {
	f.Add([]byte{0x01, 0x01, 0x40, 0x00})
	f.Add([]byte{0x81, 0x80, 0x80, 0xff, 0x2c, 0x01})
	f.Fuzz(func(t *testing.T, data []byte) {
		mustNotPanicExceptComError(t, func() {
			var from shared.EntityState
			NewReader(data).ReadDeltaEntity(&from)
		})
	})
}

func FuzzReadDeltaPlayerstate(f *testing.F) {
	f.Add([]byte{0x00, 0x10, 0x00, 0, 0, 0, 0})
	f.Add([]byte{0xff, 0x7f, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10})
	f.Fuzz(func(t *testing.T, data []byte) {
		mustNotPanicExceptComError(t, func() {
			NewReader(data).ReadDeltaPlayerstate(nil)
		})
	})
}

func FuzzReadDeltaUsercmd(f *testing.F) {
	f.Add([]byte{0xff, 1, 2, 3, 4, 5, 6})
	f.Fuzz(func(t *testing.T, data []byte) {
		mustNotPanicExceptComError(t, func() {
			var from shared.UserCmd
			NewReader(data).MSG_ReadDeltaUsercmd(&from)
		})
	})
}

func FuzzReadString(f *testing.F) {
	f.Add([]byte("hello\x00world\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		mustNotPanicExceptComError(t, func() {
			r := NewReader(data)
			r.MSG_ReadString()
			r.MSG_ReadStringLine()
			r.MSG_ReadDir()
			r.MSG_ReadPos()
			r.MSG_ReadAngle16()
			r.MSG_ReadData(make([]byte, 8))
		})
	})
}

func BenchmarkWriteDeltaEntity(b *testing.B) {
	r := rand.New(rand.NewSource(4))
	var from, to [64]shared.EntityState
	for i := range from {
		from[i] = randES(r)
		to[i] = randES(r)
		to[i].Number = from[i].Number
	}
	buf := NewSizeBuf(1 << 16)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		k := i & 63
		if buf.CurSize > 60000 {
			buf.SZ_Clear()
		}
		buf.MSG_WriteDeltaEntity(&from[k], &to[k], false, false)
	}
}
