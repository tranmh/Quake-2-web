package db

import (
	"context"
	"errors"
	"testing"
)

func TestSlotFiles(t *testing.T) {
	ctx := context.Background()
	m := NewMemory()
	u, _ := m.CreateUser(ctx, User{Email: "a@b.c", DisplayName: "a", PasswordHash: "x"})
	errNo := errors.New("no save")
	sf := &SlotFiles{Store: NewSaveStore(m), UserID: u.ID, NotFound: errNo,
		Meta: func(slot string, files map[string][]byte) SaveMeta {
			return SaveMeta{Comment: slot, Schema: len(files)}
		}}
	if _, err := sf.Read("save0", "server.ssv"); !errors.Is(err, errNo) {
		t.Fatal(err)
	}
	if err := sf.Write("save0", "server.ssv", []byte("S")); err != nil {
		t.Fatal(err)
	}
	if err := sf.Write("save0", "demo1.sav", []byte("LEVEL")); err != nil {
		t.Fatal(err)
	}
	if b, err := sf.Read("save0", "demo1.sav"); err != nil || string(b) != "LEVEL" {
		t.Fatal(b, err)
	}
	if names, _ := sf.List("save0"); len(names) != 2 || names[0] != "demo1.sav" {
		t.Fatal(names)
	}
	if _, info, err := m.GetSave(ctx, u.ID, "save0"); err != nil || info.Comment != "save0" || info.Schema != 2 {
		t.Fatal(info, err)
	}
	sf.Remove("save0", "server.ssv")
	sf.Remove("save0", "demo1.sav")
	if _, _, err := m.GetSave(ctx, u.ID, "save0"); !errors.Is(err, ErrNotFound) {
		t.Fatal("empty slot not deleted")
	}
}

func FuzzDecodeBundle(f *testing.F) {
	f.Add(EncodeBundle(map[string][]byte{"a": []byte("xy"), "game.ssv": nil}))
	f.Fuzz(func(t *testing.T, b []byte) {
		files, err := DecodeBundle(b)
		if err == nil {
			if got, err := DecodeBundle(EncodeBundle(files)); err != nil || len(got) != len(files) {
				t.Fatal("round trip")
			}
		}
	})
}
