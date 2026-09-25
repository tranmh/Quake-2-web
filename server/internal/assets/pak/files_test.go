package pak

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"quake2web/server/internal/testutil"
)

// buildPak assembles a pak in memory from name/content pairs.
func buildPak(entries [][2]string) []byte {
	var body bytes.Buffer
	type ent struct {
		name     string
		pos, len int
	}
	var ents []ent
	pos := headerSize
	for _, e := range entries {
		ents = append(ents, ent{e[0], pos, len(e[1])})
		body.WriteString(e[1])
		pos += len(e[1])
	}
	var out bytes.Buffer
	hdr := make([]byte, headerSize)
	binary.LittleEndian.PutUint32(hdr[0:], 'P'|'A'<<8|'C'<<16|'K'<<24)
	binary.LittleEndian.PutUint32(hdr[4:], uint32(pos))
	binary.LittleEndian.PutUint32(hdr[8:], uint32(len(ents)*dirEntSize))
	out.Write(hdr)
	out.Write(body.Bytes())
	for _, e := range ents {
		d := make([]byte, dirEntSize)
		copy(d, e.name)
		binary.LittleEndian.PutUint32(d[56:], uint32(e.pos))
		binary.LittleEndian.PutUint32(d[60:], uint32(e.len))
		out.Write(d)
	}
	return out.Bytes()
}

func TestOpenBytes(t *testing.T) {
	data := buildPak([][2]string{{"maps/a.bsp", "AAAA"}, {"Pics/B.pcx", "bb"}, {"maps/a.bsp", "dup"}, {"empty", ""}})
	p, err := OpenBytes("test.pak", data)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.List()) != 4 {
		t.Fatalf("got %d files", len(p.List()))
	}
	for _, tc := range []struct{ name, want string }{
		{"maps/a.bsp", "AAAA"}, // first match wins inside one pak
		{"MAPS/A.BSP", "AAAA"},
		{"pics/b.pcx", "bb"},
		{"empty", ""},
	} {
		b, err := p.ReadFile(tc.name)
		if err != nil || string(b) != tc.want {
			t.Errorf("ReadFile(%q) = %q, %v; want %q", tc.name, b, err, tc.want)
		}
	}
	if _, err := p.ReadFile("nope"); err == nil {
		t.Error("expected not found")
	}
}

func TestOpenBytesErrors(t *testing.T) {
	good := buildPak([][2]string{{"a", "x"}})
	cases := map[string][]byte{
		"short": []byte("PACK"),
		"ident": append([]byte("KCAP"), good[4:]...),
		"dirofs": func() []byte {
			b := append([]byte(nil), good...)
			binary.LittleEndian.PutUint32(b[4:], 1<<30)
			return b
		}(),
		"neglen": func() []byte {
			b := append([]byte(nil), good...)
			binary.LittleEndian.PutUint32(b[8:], 0xffffffff)
			return b
		}(),
	}
	for name, data := range cases {
		if _, err := OpenBytes(name, data); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
	// entry out of bounds is reported at read time
	b := append([]byte(nil), good...)
	binary.LittleEndian.PutUint32(b[len(b)-4:], 1000)
	p, err := OpenBytes("oob", b)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.ReadFile("a"); err == nil {
		t.Error("expected out of bounds error")
	}
}

func TestSearchOrder(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "pak0.pak"), buildPak([][2]string{{"x.txt", "pak0"}, {"only0", "0"}}), 0o644)
	os.WriteFile(filepath.Join(dir, "pak1.pak"), buildPak([][2]string{{"x.txt", "pak1"}}), 0o644)
	os.WriteFile(filepath.Join(dir, "loose.txt"), []byte("loose"), 0o644)
	os.WriteFile(filepath.Join(dir, "x.txt"), []byte("dir"), 0o644)
	var fs FS
	if err := fs.AddGameDirectory(dir); err != nil {
		t.Fatal(err)
	}
	defer fs.Close()
	for _, tc := range []struct{ name, want string }{
		{"x.txt", "pak1"}, // later pak overrides earlier pak and loose files
		{"only0", "0"},
		{"loose.txt", "loose"},
	} {
		b, err := fs.ReadFile(tc.name)
		if err != nil || string(b) != tc.want {
			t.Errorf("ReadFile(%q) = %q, %v; want %q", tc.name, b, err, tc.want)
		}
	}
	if _, err := fs.ReadFile("../etc/passwd"); err == nil {
		t.Error("path escape must fail")
	}
	if len(fs.Paks()) != 2 {
		t.Errorf("paks = %d", len(fs.Paks()))
	}
}

func TestDemoPak(t *testing.T) {
	path := testutil.DemoPak(t)
	p, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	files := p.List()
	if len(files) == 0 {
		t.Fatal("empty pak")
	}
	var hasDemo1, hasColormap bool
	for _, f := range files {
		switch strings.ToLower(f.Name) {
		case "maps/demo1.bsp":
			hasDemo1 = true
		case "pics/colormap.pcx":
			hasColormap = true
		}
	}
	if !hasDemo1 || !hasColormap {
		t.Fatalf("demo pak lacks demo1.bsp (%v) or colormap.pcx (%v)", hasDemo1, hasColormap)
	}
	b, err := p.ReadFile("maps/demo1.bsp")
	if err != nil {
		t.Fatal(err)
	}
	if string(b[:4]) != "IBSP" {
		t.Fatalf("bad bsp ident %q", b[:4])
	}
	t.Logf("%d files, directory checksum %#x", len(files), p.Checksum)
}

func FuzzOpenBytes(f *testing.F) {
	f.Add(buildPak([][2]string{{"a", "x"}, {"b/c", "yy"}}))
	f.Add([]byte("PACK\x0c\x00\x00\x00\x40\x00\x00\x00"))
	f.Fuzz(func(t *testing.T, data []byte) {
		p, err := OpenBytes("fuzz", data)
		if err != nil {
			return
		}
		for i := range p.List() {
			p.ReadEntry(i)
		}
	})
}
