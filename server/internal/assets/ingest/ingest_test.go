package ingest

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"image"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"testing"

	"quake2web/server/internal/assets/blob"
	"quake2web/server/internal/assets/img"
	"quake2web/server/internal/assets/manifest"
	"quake2web/server/internal/assets/pcx"
	"quake2web/server/internal/testutil"
)

func readBlob(t *testing.T, s blob.Store, sha string) []byte {
	t.Helper()
	r, _, err := s.Open(context.Background(), sha)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	b, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestIngestDemoPak(t *testing.T) {
	path := testutil.DemoPak(t)
	store, err := blob.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	res, err := PakFile(context.Background(), store, path, Options{})
	if err != nil {
		t.Fatal(err)
	}
	m := res.Manifest
	if m.NumFiles != 1106 || len(m.Entries) != 1106 {
		t.Fatalf("%d entries", len(m.Entries))
	}
	if res.Palette == nil || m.Palette == "" || m.ConvertPalette != m.Palette {
		t.Fatal("palette not taken from pics/colormap.pcx")
	}
	for _, e := range m.Entries {
		if e.Error != "" {
			t.Errorf("%s: %s", e.Path, e.Error)
		}
		if !blob.ValidHash(e.SHA256) {
			t.Errorf("%s: no hash", e.Path)
		}
	}

	// raw hashes agree with the oracle pak fixture (if generated)
	var pakFix struct {
		Files []struct {
			Name   string `json:"name"`
			SHA256 string `json:"sha256"`
		} `json:"files"`
	}
	if fx := filepath.Join(testutil.FixturesDir(), "core/pak/pak0.json"); fileExists(fx) {
		if err := testutil.ReadJSON(fx, &pakFix); err != nil {
			t.Fatal(err)
		}
		if len(pakFix.Files) != len(m.Entries) {
			t.Fatalf("fixture has %d files", len(pakFix.Files))
		}
		for i, f := range pakFix.Files {
			if m.Entries[i].Path != f.Name || m.Entries[i].SHA256 != f.SHA256 {
				t.Errorf("entry %d: %s %s, fixture %s %s", i, m.Entries[i].Path, m.Entries[i].SHA256, f.Name, f.SHA256)
			}
		}
	}

	// maps: CM_LoadMap checksums, cross-checked with the oracle fixtures
	want := map[string]uint32{"demo1": 3218560851, "demo2": 180827219, "demo3": 2948950693}
	if len(res.Maps) != 3 {
		t.Fatalf("%d maps", len(res.Maps))
	}
	for _, mi := range res.Maps {
		if mi.Checksum != want[mi.Name] {
			t.Errorf("%s checksum %d, want %d", mi.Name, mi.Checksum, want[mi.Name])
		}
		var fix struct {
			Checksum   uint32 `json:"checksum"`
			NumTexinfo int    `json:"numtexinfo"`
			NumCmodels int    `json:"numcmodels"`
		}
		if fx := filepath.Join(testutil.FixturesDir(), "core/bsp", mi.Name+".json"); fileExists(fx) {
			if err := readFirstJSON(fx, &fix); err != nil {
				t.Fatal(err)
			}
			if fix.Checksum != mi.Checksum || fix.NumTexinfo != mi.NumTexInfo || fix.NumCmodels != mi.NumInlineModels {
				t.Errorf("%s: fixture %+v, got checksum %d texinfo %d cmodels %d", mi.Name, fix, mi.Checksum, mi.NumTexInfo, mi.NumInlineModels)
			}
		}
		if mi.Sky != "unit1_" || len(mi.SkyImages) != 6 || len(mi.Textures) == 0 || mi.Message == "" {
			t.Errorf("%s: %+v", mi.Name, mi)
		}
	}

	byPath := map[string]manifest.Entry{}
	for _, e := range m.Entries {
		byPath[e.Path] = e
	}
	// colormap PNG pixel == palette entry of the index
	cm := byPath["pics/colormap.pcx"]
	if cm.PNG == nil || cm.PNG.Width != 256 || cm.PNG.Height != 320 || cm.PNG.Type != manifest.ImagePic {
		t.Fatalf("colormap png %+v", cm.PNG)
	}
	pal := res.Palette
	dec, err := png.Decode(bytes.NewReader(readBlob(t, store, cm.PNG.SHA256)))
	if err != nil {
		t.Fatal(err)
	}
	cmIm, err := pcx.Decode(readBlob(t, store, cm.SHA256))
	if err != nil {
		t.Fatal(err)
	}
	for _, xy := range [][2]int{{0, 0}, {1, 0}, {15, 0}, {100, 0}, {5, 100}, {255, 319}} {
		x, y := xy[0], xy[1]
		idx := int(cmIm.Pix[y*256+x])
		r, g, b, a := dec.At(x, y).RGBA()
		wantA := uint32(0xffff)
		if idx == 255 {
			wantA = 0
		}
		if a != wantA || (a != 0 && (byte(r>>8) != pal[idx*3] || byte(g>>8) != pal[idx*3+1] || byte(b>>8) != pal[idx*3+2])) {
			t.Errorf("colormap(%d,%d) idx %d = %d %d %d %d", x, y, idx, r>>8, g>>8, b>>8, a>>8)
		}
	}
	// known palette entries (Quake II): 208 is pure green
	if pal[208*3] != 0 || pal[208*3+1] != 255 || pal[208*3+2] != 0 {
		t.Error("palette[208] is not (0,255,0)")
	}
	// WAL metadata and PNG
	w := byPath["textures/e1u1/metal2_1.wal"]
	if w.WAL == nil || w.WAL.Width != 64 || w.PNG == nil || w.PNG.Type != manifest.ImageWall || w.PNG.HasAlpha {
		t.Fatalf("wal %+v %+v", w.WAL, w.PNG)
	}
	// MD2 skins are converted as skins (flood fill), sprites as sprites
	md := byPath["models/items/ammo/bullets/medium/tris.md2"]
	if md.MD2 == nil || md.MD2.NumFrames != 1 || md.MD2.Skins[0] != "models/items/ammo/bullets/medium/skin.pcx" {
		t.Fatalf("md2 %+v", md.MD2)
	}
	if s := byPath["models/items/ammo/bullets/medium/skin.pcx"]; s.PNG == nil || s.PNG.Type != manifest.ImageSkin {
		t.Fatalf("skin %+v", s.PNG)
	}
	if s := byPath["sprites/s_explod_0.pcx"]; s.PNG == nil || s.PNG.Type != manifest.ImageSprite {
		t.Fatalf("sprite %+v", s.PNG)
	}
	if s := byPath["sprites/s_explod.sp2"]; s.SP2 == nil || len(s.SP2.Frames) != 6 {
		t.Fatalf("sp2 %+v", s.SP2)
	}
	if s := byPath["env/unit1_rt.tga"]; s.PNG == nil || s.PNG.Type != manifest.ImageSky || s.PNG.Width != 256 {
		t.Fatalf("sky %+v", s.PNG)
	}
	if s := byPath["sound/world/amb1.wav"]; s.WAV == nil || s.WAV.LoopStart != 0 || s.WAV.Samples != 24840 {
		t.Fatalf("wav %+v", s.WAV)
	}
	// raw blob of a file equals the pak bytes
	if b := readBlob(t, store, cm.SHA256); blob.Sum(b) != cm.SHA256 || int64(len(b)) != cm.Size {
		t.Fatal("raw blob mismatch")
	}
	// palette blob
	if b := readBlob(t, store, m.Palette); !bytes.Equal(b, pal[:]) {
		t.Fatal("palette blob mismatch")
	}
	// the manifest blob decodes back
	var back manifest.PakManifest
	if err := json.Unmarshal(readBlob(t, store, res.ManifestSHA), &back); err != nil || len(back.Entries) != 1106 {
		t.Fatal("manifest blob", err)
	}
	// idempotent: second run yields the same manifest hash
	res2, err := PakFile(context.Background(), store, path, Options{})
	if err != nil || res2.ManifestSHA != res.ManifestSHA {
		t.Fatalf("not deterministic: %v", err)
	}
	idx := manifest.Merge("demo", []manifest.PakRef{{ID: 1}}, []*manifest.PakManifest{m})
	if len(idx.Files) != 1106 || len(idx.Maps) != 3 || idx.Palette == nil {
		t.Fatalf("index: %d files %d maps", len(idx.Files), len(idx.Maps))
	}
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }

// readFirstJSON decodes the first JSON value of a file.
func readFirstJSON(p string, v any) error {
	f, err := os.Open(p)
	if err != nil {
		return err
	}
	defer f.Close()
	return json.NewDecoder(f).Decode(v)
}

// buildPak assembles a pak from name → bytes (in order).
func buildPak(files [][2]string) []byte {
	var data []byte
	data = append(data, make([]byte, 12)...)
	type ent struct {
		name     string
		pos, len int
	}
	var ents []ent
	for _, f := range files {
		ents = append(ents, ent{f[0], len(data), len(f[1])})
		data = append(data, f[1]...)
	}
	dirofs := len(data)
	for _, e := range ents {
		var d [64]byte
		copy(d[:56], e.name)
		binary.LittleEndian.PutUint32(d[56:], uint32(e.pos))
		binary.LittleEndian.PutUint32(d[60:], uint32(e.len))
		data = append(data, d[:]...)
	}
	copy(data[0:], "PACK")
	binary.LittleEndian.PutUint32(data[4:], uint32(dirofs))
	binary.LittleEndian.PutUint32(data[8:], uint32(len(ents)*64))
	return data
}

func TestSyntheticPakFallbackPalette(t *testing.T) {
	// a 2x1 WAL using index 255, a broken md2 and an unknown file
	walb := make([]byte, 100+2)
	copy(walb, "t/a")
	binary.LittleEndian.PutUint32(walb[32:], 2)
	binary.LittleEndian.PutUint32(walb[36:], 1)
	binary.LittleEndian.PutUint32(walb[40:], 100)
	copy(walb[56:], "t/b")
	binary.LittleEndian.PutUint32(walb[88:], 8) // SURF_SKY
	walb[100], walb[101] = 5, 255
	pk := buildPak([][2]string{
		{"textures/t/a.wal", string(walb)},
		{"models/bad/tris.md2", "IDP2junk"},
		{"readme.txt", "hi"},
	})
	dir := t.TempDir()
	p := filepath.Join(dir, "pak1.pak")
	os.WriteFile(p, pk, 0o644)
	store, _ := blob.NewFileStore(filepath.Join(dir, "blobs"))

	// without any palette: no PNGs, raw entries still indexed
	res, err := PakFile(context.Background(), store, p, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Manifest.Entries[0].PNG != nil || res.Manifest.Palette != "" {
		t.Fatal("PNG without palette")
	}
	if e := res.Manifest.Entries[1]; e.Error == "" || e.Kind != manifest.KindMD2 {
		t.Fatalf("bad md2 not flagged: %+v", e)
	}
	if e := res.Manifest.Entries[2]; e.Kind != manifest.KindOther || e.Size != 2 {
		t.Fatalf("%+v", e)
	}
	var pal img.Palette
	for i := 0; i < 256; i++ {
		pal[i*3] = byte(i)
	}
	res, err = PakFile(context.Background(), store, p, Options{Palette: &pal})
	if err != nil {
		t.Fatal(err)
	}
	e := res.Manifest.Entries[0]
	if e.PNG == nil || !e.PNG.HasAlpha || e.WAL.Flags != 8 || e.WAL.AnimNext != "textures/t/b.wal" {
		t.Fatalf("%+v %+v", e.PNG, e.WAL)
	}
	dec, _ := png.Decode(bytes.NewReader(readBlob(t, store, e.PNG.SHA256)))
	// texel 1 is index 255: transparent with the RGB of its left neighbour (5)
	nrgba, ok := dec.(*image.NRGBA)
	if !ok {
		t.Fatalf("decoded %T", dec)
	}
	if got := nrgba.Pix[4:8]; got[0] != 5 || got[1] != 0 || got[2] != 0 || got[3] != 0 {
		t.Fatalf("texel 1 = %v, want [5 0 0 0]", got)
	}
	if got := nrgba.Pix[0:4]; got[0] != 5 || got[3] != 255 {
		t.Fatalf("texel 0 = %v", got)
	}
}

func BenchmarkIngestDemo(b *testing.B) {
	path := testutil.DemoPak(b)
	for i := 0; i < b.N; i++ {
		store, _ := blob.NewFileStore(b.TempDir())
		if _, err := PakFile(context.Background(), store, path, Options{}); err != nil {
			b.Fatal(err)
		}
	}
}
