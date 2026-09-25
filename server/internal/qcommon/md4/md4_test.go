package md4

import (
	"encoding/binary"
	"encoding/hex"
	"strings"
	"testing"
)

// RFC 1320 appendix A.5 test suite.
var rfcVectors = []struct{ in, out string }{
	{"", "31d6cfe0d16ae931b73c59d7e0c089c0"},
	{"a", "bde52cb31de33e46245e05fbdbd6fb24"},
	{"abc", "a448017aaf21d8525fc10ae87aa6729d"},
	{"message digest", "d9130a8164549fe818874806e1c7014b"},
	{"abcdefghijklmnopqrstuvwxyz", "d79e1c308aa5bbcdeea8ed63df412da9"},
	{"ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789", "043f8582f241db351ce627e153e7f0e4"},
	{strings.Repeat("1234567890", 8), "e33b4ddc9c38f2199c3e7b164fcc0536"},
}

func TestRFC1320(t *testing.T) {
	for _, v := range rfcVectors {
		d := Sum([]byte(v.in))
		if got := hex.EncodeToString(d[:]); got != v.out {
			t.Errorf("MD4(%q) = %s, want %s", v.in, got, v.out)
		}
		want, _ := hex.DecodeString(v.out)
		x := binary.LittleEndian.Uint32(want[0:]) ^ binary.LittleEndian.Uint32(want[4:]) ^
			binary.LittleEndian.Uint32(want[8:]) ^ binary.LittleEndian.Uint32(want[12:])
		if got := Com_BlockChecksum([]byte(v.in)); got != x {
			t.Errorf("Com_BlockChecksum(%q) = %#x, want %#x", v.in, got, x)
		}
	}
}

func TestIncremental(t *testing.T) {
	data := []byte(strings.Repeat("The quick brown fox jumps over the lazy dog. ", 50))
	whole := Sum(data)
	for _, step := range []int{1, 3, 63, 64, 65, 200} {
		var c Ctx
		c.Init()
		for i := 0; i < len(data); i += step {
			e := i + step
			if e > len(data) {
				e = len(data)
			}
			c.Update(data[i:e])
		}
		if got := c.Final(); got != whole {
			t.Errorf("step %d: digest mismatch", step)
		}
	}
}

func BenchmarkChecksum(b *testing.B) {
	data := make([]byte, 1<<20)
	b.SetBytes(int64(len(data)))
	for i := 0; i < b.N; i++ {
		Com_BlockChecksum(data)
	}
}
