package blob

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFileStore(t *testing.T) {
	ctx := context.Background()
	s, err := Open("file://" + t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	info, err := s.PutBytes(ctx, []byte("hello"))
	if err != nil {
		t.Fatal(err)
	}
	if info.SHA256 != "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824" || info.Size != 5 {
		t.Fatalf("%+v", info)
	}
	again, err := s.Put(ctx, strings.NewReader("hello"))
	if err != nil || again.SHA256 != info.SHA256 {
		t.Fatal(err)
	}
	r, st, err := s.Open(ctx, info.SHA256)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(r)
	r.Close()
	if !bytes.Equal(b, []byte("hello")) || st.Size != 5 {
		t.Fatal("readback")
	}
	if _, err := s.Stat(ctx, strings.Repeat("0", 64)); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, _, err := s.Open(ctx, "../etc/passwd"); !errors.Is(err, ErrBadHash) {
		t.Fatal(err)
	}
	if err := s.Check(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(ctx, info.SHA256); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Stat(ctx, info.SHA256); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	// PutFile moves a hashed file into place
	fs := s.(*FileStore)
	tmp := filepath.Join(t.TempDir(), "x")
	os.WriteFile(tmp, []byte("hello"), 0o600)
	if _, err := fs.PutFile(tmp, info.SHA256, 5); err != nil {
		t.Fatal(err)
	}
	if p, err := fs.LocalPath(info.SHA256); err != nil || !strings.HasPrefix(p, fs.Dir()) {
		t.Fatal(p, err)
	}
}

func TestS3Stub(t *testing.T) {
	if _, err := Open("s3://bucket/prefix"); !errors.Is(err, ErrNotImplemented) {
		t.Fatal(err)
	}
}
