package manifest

import "testing"

func TestMergeOverride(t *testing.T) {
	pak0 := &PakManifest{Name: "pak0.pak", Palette: "p0", Entries: []Entry{
		{Path: "pics/colormap.pcx", SHA256: "c0"},
		{Path: "maps/a.bsp", SHA256: "a0", Map: &MapInfo{Name: "a", Path: "maps/a.bsp"}},
		{Path: "Sound/X.wav", SHA256: "x0"},
		{Path: "sound/x.wav", SHA256: "x0dup"}, // second entry in the same pak loses
	}}
	pak1 := &PakManifest{Name: "pak1.pak", Entries: []Entry{
		{Path: "sound/x.WAV", SHA256: "x1"},
		{Path: "maps/b.bsp", SHA256: "b1", Map: &MapInfo{Name: "b", Path: "maps/b.bsp"}},
	}}
	idx := Merge("set", []PakRef{{ID: 1}, {ID: 2}}, []*PakManifest{pak0, pak1})
	if e := idx.Lookup("SOUND/x.wav"); e == nil || e.SHA256 != "x1" || e.Pak != 1 {
		t.Fatalf("override: %+v", e)
	}
	if len(idx.Files) != 4 {
		t.Fatalf("%d files", len(idx.Files))
	}
	if idx.Palette == nil || idx.Palette.SHA256 != "p0" {
		t.Fatal("palette")
	}
	if len(idx.Maps) != 2 || idx.Maps[0].Name != "a" || idx.Maps[1].Name != "b" {
		t.Fatalf("maps %+v", idx.Maps)
	}
	// first-in-pak wins
	idx = Merge("set", []PakRef{{ID: 1}}, []*PakManifest{pak0})
	if e := idx.Lookup("sound/x.wav"); e.SHA256 != "x0" {
		t.Fatal(e.SHA256)
	}
	if len(idx.SHAs()) != 4 {
		t.Fatal(idx.SHAs())
	}
}
