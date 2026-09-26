package host

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"time"

	"quake2web/server/internal/assets/blob"
	"quake2web/server/internal/assets/manifest"
)

// IndexFS is an sv.FileSystem over an ingested pakset: names resolve through
// the merged asset index (files.c search order, case-insensitive like
// FS_FOpenFile) and the bytes come from the content-addressed blob store,
// so instances never need a local game directory.
type IndexFS struct {
	Index *manifest.Index
	Store blob.Store
	// Timeout bounds each blob read (default 30 s).
	Timeout time.Duration
}

// NewIndexFS returns the file system of a pakset index.
func NewIndexFS(idx *manifest.Index, store blob.Store) *IndexFS {
	return &IndexFS{Index: idx, Store: store}
}

// ReadFile implements sv.FileSystem.
func (f *IndexFS) ReadFile(name string) ([]byte, error) {
	if f == nil || f.Index == nil {
		return nil, fs.ErrNotExist
	}
	e := f.Index.Lookup(name)
	if e == nil {
		return nil, fmt.Errorf("%s: %w", name, fs.ErrNotExist)
	}
	t := f.Timeout
	if t <= 0 {
		t = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), t)
	defer cancel()
	r, _, err := f.Store.Open(ctx, e.SHA256)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	defer r.Close()
	b, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	if int64(len(b)) != e.Size {
		return nil, fmt.Errorf("%s: blob %s has %d bytes, want %d", name, e.SHA256, len(b), e.Size)
	}
	return b, nil
}
