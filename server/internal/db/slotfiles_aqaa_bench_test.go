package db

import (
	"context"
	"fmt"
	"testing"
)

var slotBenchSink interface{}

// BenchmarkSlotFilesWrite rewrites one file of a slot holding k files; each
// Write re-decodes and re-encodes the whole bundle.
func BenchmarkSlotFilesWrite(b *testing.B) {
	for _, k := range []int{1, 16, 128} {
		b.Run(fmt.Sprintf("files=%d", k), func(b *testing.B) {
			m := NewMemory()
			u, err := m.CreateUser(context.Background(), User{Email: "a@b.c", DisplayName: "a", PasswordHash: "x"})
			if err != nil {
				b.Fatal(err)
			}
			sf := &SlotFiles{Store: NewSaveStore(m), UserID: u.ID}
			data := make([]byte, 4096)
			for i := 0; i < k; i++ {
				if err := sf.Write("slot1", fmt.Sprintf("map%d.sav", i), data); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := sf.Write("slot1", "map0.sav", data); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkEncodeDecodeBundle(b *testing.B) {
	for _, k := range []int{1, 16, 128} {
		b.Run(fmt.Sprintf("files=%d", k), func(b *testing.B) {
			files := make(map[string][]byte, k)
			for i := 0; i < k; i++ {
				files[fmt.Sprintf("map%d.sav", i)] = make([]byte, 4096)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				out, err := DecodeBundle(EncodeBundle(files))
				if err != nil {
					b.Fatal(err)
				}
				slotBenchSink = out
			}
		})
	}
}
