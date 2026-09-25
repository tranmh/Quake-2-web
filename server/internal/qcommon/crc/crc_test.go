package crc

import "testing"

func TestCRCBlock(t *testing.T) {
	cases := []struct {
		in   string
		want uint16
	}{
		{"", 0xffff},
		{"123456789", 0x29B1},
		{"A", 0xB915},
	}
	for _, c := range cases {
		if got := CRC_Block([]byte(c.in)); got != c.want {
			t.Errorf("CRC_Block(%q) = %#04x, want %#04x", c.in, got, c.want)
		}
		var v uint16
		CRC_Init(&v)
		for i := 0; i < len(c.in); i++ {
			CRC_ProcessByte(&v, c.in[i])
		}
		if CRC_Value(v) != c.want {
			t.Errorf("incremental CRC(%q) = %#04x", c.in, v)
		}
	}
}

func TestChktbl(t *testing.T) {
	if chktbl[0] != 0x84 || chktbl[959] != 0x32 || chktbl[960] != 0 || chktbl[1023] != 0 {
		t.Fatalf("chktbl extraction wrong: %x %x %x", chktbl[0], chktbl[959], chktbl[960])
	}
	if crctable[1] != 0x1021 {
		t.Fatalf("crctable[1] = %#x", crctable[1])
	}
}

func TestBlockSequenceCRCByte(t *testing.T) {
	base := []byte("hello world, this is a test of the sequence crc byte function, longer than sixty bytes")
	// Reference computed directly from the definition.
	ref := func(b []byte, seq int32) byte {
		if len(b) > 60 {
			b = b[:60]
		}
		p := chktbl[seq%1020:]
		buf := append(append([]byte{}, b...), p[0], p[1], p[2], p[3])
		c := int(CRC_Block(buf))
		x := 0
		for _, v := range buf {
			x += int(v)
		}
		return byte((c ^ x) & 0xff)
	}
	for _, seq := range []int32{0, 1, 17, 1019, 1020, 1021, 123456789} {
		for _, n := range []int{0, 5, 60, len(base)} {
			if got, want := COM_BlockSequenceCRCByte(base[:n], seq), ref(base[:n], seq); got != want {
				t.Errorf("seq %d len %d: %#x want %#x", seq, n, got, want)
			}
		}
	}
	defer func() {
		if recover() == nil {
			t.Error("negative sequence did not panic")
		}
	}()
	COM_BlockSequenceCRCByte(base, -1)
}
