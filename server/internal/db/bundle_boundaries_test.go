package db

import (
	"bytes"
	"errors"
	"testing"
)

func TestBundleRoundTripAndLayout(t *testing.T) {
	empty := EncodeBundle(nil)
	if !bytes.Equal(empty, []byte("Q2SB\x01\x00\x00\x00\x00")) {
		t.Fatalf("% x", empty)
	}
	if got, err := DecodeBundle(empty); err != nil || len(got) != 0 {
		t.Fatalf("%v %v", got, err)
	}
	files := map[string][]byte{"b.sav": {1, 2, 3}, "a.sv2": {}, "save/x": []byte("hi")}
	enc := EncodeBundle(files)
	// files are sorted by name: "a.sv2" comes first
	if !bytes.Equal(enc[9:11], []byte{5, 0}) || string(enc[11:16]) != "a.sv2" {
		t.Fatalf("% x", enc)
	}
	if !bytes.Equal(enc, EncodeBundle(files)) {
		t.Fatal("encoding not deterministic")
	}
	got, err := DecodeBundle(enc)
	if err != nil || len(got) != 3 {
		t.Fatalf("%v %v", got, err)
	}
	for n, d := range files {
		if !bytes.Equal(got[n], d) {
			t.Fatalf("%s: % x", n, got[n])
		}
	}
}

func TestDecodeBundleMalformed(t *testing.T) {
	good := EncodeBundle(map[string][]byte{"f": []byte("data")})
	for _, tc := range []struct {
		name string
		b    []byte
	}{
		{"nil", nil},
		{"bad magic", append([]byte("Q2SX\x01"), good[5:]...)},
		{"wrong version", append([]byte("Q2SB\x02"), good[5:]...)},
		{"missing count", []byte("Q2SB\x01\x00\x00")},
		{"count too large", append([]byte("Q2SB\x01\x02\x00\x00\x00"), good[9:]...)},
		{"truncated name length", good[:10]},
		{"truncated name", good[:12]},
		{"truncated data", good[:len(good)-1]},
		{"trailing data", append(append([]byte(nil), good...), 0)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got, err := DecodeBundle(tc.b); !errors.Is(err, ErrBadBundle) || got != nil {
				t.Fatalf("%v %v", got, err)
			}
		})
	}
}
