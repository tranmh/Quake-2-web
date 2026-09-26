package sv

import (
	"errors"
	"testing"

	"quake2web/server/internal/qcommon/md4"
)

// mapFS serves one map name from a byte slice.
type mapFS struct{ raw []byte }

func (f mapFS) ReadFile(name string) ([]byte, error) {
	if name == "maps/x.bsp" {
		return f.raw, nil
	}
	return nil, errors.New("not found")
}

// Every distinct map ever loaded stayed in the process-wide cache forever.
func TestMapCacheIsBounded(t *testing.T) {
	c := NewMapCache()
	for i := 0; i < 3*DefaultMapCacheEntries; i++ {
		raw := append(append([]byte(nil), synthBSP...), byte(i), byte(i>>8))
		m, err := c.Load(mapFS{raw}, "maps/x.bsp")
		if err != nil {
			t.Fatal(err)
		}
		if m.Checksum != md4.Com_BlockChecksum(raw) {
			t.Fatalf("map %d: wrong map returned", i)
		}
	}
	if n := len(c.maps); n > DefaultMapCacheEntries {
		t.Fatalf("cache holds %d maps (bound %d)", n, DefaultMapCacheEntries)
	}
}

// The cache key was name + length + 64-bit FNV-1a, which is not collision
// resistant: a colliding pair for the synthetic map took 23 s of CPU to find
// (parallel rho). A user's map crafted to collide with a public one and
// loaded first would replace the collision model and entity string of every
// later game using the public map.
func TestMapCacheCollisionDoesNotPoison(t *testing.T) {
	// two 8-byte suffixes whose FNV-1a-64 states collide after synthBSP
	x := []byte{0x22, 0xcc, 0x62, 0x23, 0xa7, 0x72, 0x1b, 0xe5} // 0xe51b72a72362cc22 LE
	y := []byte{0x5d, 0x92, 0x50, 0xc7, 0xde, 0xda, 0x10, 0x2a} // 0x2a10dadec750925d LE
	evil := append(append([]byte(nil), synthBSP...), x...)
	good := append(append([]byte(nil), synthBSP...), y...)
	h1, h2 := fnv64a(evil), fnv64a(good)
	if h1 != h2 || len(evil) != len(good) {
		t.Fatalf("test vector is not an FNV-1a-64 collision: %#x %#x", h1, h2)
	}

	c := NewMapCache()
	if _, err := c.Load(mapFS{evil}, "maps/x.bsp"); err != nil {
		t.Fatal(err)
	}
	m, err := c.Load(mapFS{good}, "maps/x.bsp")
	if err != nil {
		t.Fatal(err)
	}
	if m.Checksum != md4.Com_BlockChecksum(good) {
		t.Fatalf("cache returned the colliding (attacker's) map for different bytes")
	}
}

func fnv64a(b []byte) uint64 {
	h := uint64(0xcbf29ce484222325)
	for _, c := range b {
		h ^= uint64(c)
		h *= 0x100000001b3
	}
	return h
}
