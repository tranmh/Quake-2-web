package ingest

import (
	"context"
	"encoding/binary"
	"fmt"
	"testing"

	"quake2web/server/internal/assets/blob"
	"quake2web/server/internal/assets/pak"
)

// overlappingPak returns a pak whose n directory entries all alias one
// size-byte data region.
func overlappingPak(n, size int) []byte {
	data := make([]byte, 12+size)
	dirofs := len(data)
	for i := 0; i < n; i++ {
		var d [64]byte
		copy(d[:56], fmt.Sprintf("big%d.bin", i))
		binary.LittleEndian.PutUint32(d[56:], 12)
		binary.LittleEndian.PutUint32(d[60:], uint32(size))
		data = append(data, d[:]...)
	}
	copy(data, "PACK")
	binary.LittleEndian.PutUint32(data[4:], uint32(dirofs))
	binary.LittleEndian.PutUint32(data[8:], uint32(n*64))
	return data
}

// Directory entries may alias the same bytes, so a 1 GiB upload could make
// ingest read and hash up to 4096 GiB (MAX_FILES_IN_PACK entries of 1 GiB
// each), holding GOMAXPROCS such entries in memory at once: an OOM of the
// whole server, or at least an ingest queue blocked for hours. Ingest must
// refuse paks whose entries cover more bytes than the file has.
func TestIngestRejectsOverlappingEntries(t *testing.T) {
	raw := overlappingPak(64, 1<<20)
	p, err := pak.OpenBytes("evil.pak", raw)
	if err != nil {
		t.Fatal(err)
	}
	store, err := blob.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Pak(context.Background(), store, p, "evil.pak", blob.Sum(raw), int64(len(raw)), Options{}); err == nil {
		t.Fatal("ingest accepted a pak whose entries cover 64x its size")
	}
	// a normal pak (entries within the file, no aliasing) still ingests
	ok := buildPak([][2]string{{"a.txt", "hello"}, {"b.txt", "world"}})
	p2, err := pak.OpenBytes("ok.pak", ok)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Pak(context.Background(), store, p2, "ok.pak", blob.Sum(ok), int64(len(ok)), Options{}); err != nil {
		t.Fatalf("normal pak: %v", err)
	}
}
