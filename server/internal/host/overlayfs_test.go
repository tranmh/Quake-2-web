package host

import (
	"errors"
	"io/fs"
	"testing"
)

type mapFS map[string]string

func (m mapFS) ReadFile(name string) ([]byte, error) {
	if s, ok := m[name]; ok {
		return []byte(s), nil
	}
	return nil, fs.ErrNotExist
}

func TestOverlayFS(t *testing.T) {
	o := &OverlayFS{
		Files: map[string][]byte{"demos/t.dm2": []byte("demo"), "maps/demo1.bsp": []byte("over"), "B.txt": []byte("upper"), "b.TXT": []byte("mixed")},
		Base:  mapFS{"maps/demo1.bsp": "base", "pics/x.pcx": "pic"},
	}
	for name, want := range map[string]string{
		"demos/t.dm2":    "demo",
		"DEMOS/T.DM2":    "demo", // case-insensitive like a pak lookup
		"maps/demo1.bsp": "over", // the overlay shadows the base
		"pics/x.pcx":     "pic",  // falls through to the base
		"b.txt":          "upper",
	} {
		got, err := o.ReadFile(name)
		if err != nil || string(got) != want {
			t.Errorf("%s: %q %v, want %q", name, got, err, want)
		}
	}
	if _, err := o.ReadFile("missing"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("missing: %v", err)
	}
	b, _ := o.ReadFile("demos/t.dm2")
	b[0] = 'X'
	if string(o.Files["demos/t.dm2"]) != "demo" {
		t.Error("ReadFile returned the overlay's own bytes")
	}
	noBase := &OverlayFS{}
	if _, err := noBase.ReadFile("x"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("no base: %v", err)
	}
	var nilFS *OverlayFS
	if _, err := nilFS.ReadFile("x"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("nil overlay: %v", err)
	}
}
