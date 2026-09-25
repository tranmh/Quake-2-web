// Package blob is the content-addressed blob store (ADR-0005). Blobs are
// keyed by the lowercase hex SHA-256 of their bytes; writing the same bytes
// twice is a no-op.
package blob

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// ErrNotFound is returned for unknown hashes.
var ErrNotFound = errors.New("blob: not found")

// ErrBadHash is returned for keys that are not 64 lowercase hex digits.
var ErrBadHash = errors.New("blob: bad sha256")

var hashRE = regexp.MustCompile(`^[0-9a-f]{64}$`)

// ValidHash reports whether s is a lowercase hex SHA-256.
func ValidHash(s string) bool { return hashRE.MatchString(s) }

// Sum returns the hex SHA-256 of b.
func Sum(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// Info describes a stored blob.
type Info struct {
	SHA256  string
	Size    int64
	ModTime time.Time
}

// ReadSeekCloser is what Open returns (http.ServeContent needs Seek).
type ReadSeekCloser interface {
	io.ReadSeeker
	io.Closer
}

// Store is a content-addressed blob store.
type Store interface {
	// Put streams r into the store and returns its hash and size.
	Put(ctx context.Context, r io.Reader) (Info, error)
	// PutBytes stores b.
	PutBytes(ctx context.Context, b []byte) (Info, error)
	// Open returns a reader for the blob.
	Open(ctx context.Context, sha string) (ReadSeekCloser, Info, error)
	// Stat returns blob info or ErrNotFound.
	Stat(ctx context.Context, sha string) (Info, error)
	// Delete removes a blob (missing blobs are not an error).
	Delete(ctx context.Context, sha string) error
	// Check performs a write/read/delete round trip (readiness probe).
	Check(ctx context.Context) error
}

// Localer is implemented by stores that keep blobs as local files, letting
// ingest open a pak in place instead of copying it.
type Localer interface {
	LocalPath(sha string) (string, error)
}

// Open returns a store for a URL: file:///abs/dir, a plain directory path,
// or s3://bucket/prefix (not implemented yet).
func Open(spec string) (Store, error) {
	if strings.HasPrefix(spec, "s3://") {
		return NewS3(spec)
	}
	if strings.HasPrefix(spec, "file://") {
		u, err := url.Parse(spec)
		if err != nil {
			return nil, err
		}
		return NewFileStore(u.Path)
	}
	return NewFileStore(spec)
}

// FileStore keeps blobs under dir/ab/cdef... (first two hex digits as a
// fan-out directory).
type FileStore struct {
	dir string
}

// NewFileStore creates dir if needed.
func NewFileStore(dir string) (*FileStore, error) {
	if dir == "" {
		return nil, errors.New("blob: empty directory")
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Join(abs, "tmp"), 0o755); err != nil {
		return nil, err
	}
	return &FileStore{dir: abs}, nil
}

// Dir returns the root directory.
func (s *FileStore) Dir() string { return s.dir }

func (s *FileStore) path(sha string) string {
	return filepath.Join(s.dir, sha[:2], sha[2:])
}

// LocalPath implements Localer.
func (s *FileStore) LocalPath(sha string) (string, error) {
	if !ValidHash(sha) {
		return "", ErrBadHash
	}
	p := s.path(sha)
	if _, err := os.Stat(p); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", ErrNotFound
		}
		return "", err
	}
	return p, nil
}

// Put implements Store: the data is streamed to a temp file while hashing,
// then renamed into place.
func (s *FileStore) Put(ctx context.Context, r io.Reader) (Info, error) {
	tmp, err := os.CreateTemp(filepath.Join(s.dir, "tmp"), "put-*")
	if err != nil {
		return Info{}, err
	}
	defer os.Remove(tmp.Name())
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(tmp, h), ctxReader{ctx, r})
	if err != nil {
		tmp.Close()
		return Info{}, err
	}
	if err := tmp.Close(); err != nil {
		return Info{}, err
	}
	sha := hex.EncodeToString(h.Sum(nil))
	return s.commit(tmp.Name(), sha, n)
}

// PutFile moves (renames) an already hashed local file into the store. The
// caller vouches for sha; the file is gone afterwards.
func (s *FileStore) PutFile(path, sha string, size int64) (Info, error) {
	if !ValidHash(sha) {
		return Info{}, ErrBadHash
	}
	return s.commit(path, sha, size)
}

func (s *FileStore) commit(tmpPath, sha string, n int64) (Info, error) {
	dst := s.path(sha)
	if st, err := os.Stat(dst); err == nil {
		os.Remove(tmpPath)
		return Info{SHA256: sha, Size: st.Size(), ModTime: st.ModTime()}, nil
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return Info{}, err
	}
	if err := os.Chmod(tmpPath, 0o644); err != nil {
		return Info{}, err
	}
	if err := os.Rename(tmpPath, dst); err != nil {
		// cross-device: copy
		if err2 := copyFile(tmpPath, dst); err2 != nil {
			return Info{}, fmt.Errorf("blob: rename: %v; copy: %w", err, err2)
		}
		os.Remove(tmpPath)
	}
	st, err := os.Stat(dst)
	if err != nil {
		return Info{}, err
	}
	return Info{SHA256: sha, Size: n, ModTime: st.ModTime()}, nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst + ".partial"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}

// PutBytes implements Store.
func (s *FileStore) PutBytes(ctx context.Context, b []byte) (Info, error) {
	sha := Sum(b)
	dst := s.path(sha)
	if st, err := os.Stat(dst); err == nil {
		return Info{SHA256: sha, Size: st.Size(), ModTime: st.ModTime()}, nil
	}
	return s.Put(ctx, bytes.NewReader(b))
}

// Open implements Store.
func (s *FileStore) Open(_ context.Context, sha string) (ReadSeekCloser, Info, error) {
	if !ValidHash(sha) {
		return nil, Info{}, ErrBadHash
	}
	f, err := os.Open(s.path(sha))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, Info{}, ErrNotFound
		}
		return nil, Info{}, err
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, Info{}, err
	}
	return f, Info{SHA256: sha, Size: st.Size(), ModTime: st.ModTime()}, nil
}

// Stat implements Store.
func (s *FileStore) Stat(_ context.Context, sha string) (Info, error) {
	if !ValidHash(sha) {
		return Info{}, ErrBadHash
	}
	st, err := os.Stat(s.path(sha))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Info{}, ErrNotFound
		}
		return Info{}, err
	}
	return Info{SHA256: sha, Size: st.Size(), ModTime: st.ModTime()}, nil
}

// Delete implements Store.
func (s *FileStore) Delete(_ context.Context, sha string) error {
	if !ValidHash(sha) {
		return ErrBadHash
	}
	err := os.Remove(s.path(sha))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// Check implements Store.
func (s *FileStore) Check(ctx context.Context) error {
	probe := []byte(fmt.Sprintf("readyz %d", time.Now().UnixNano()))
	info, err := s.PutBytes(ctx, probe)
	if err != nil {
		return err
	}
	r, _, err := s.Open(ctx, info.SHA256)
	if err != nil {
		return err
	}
	got, err := io.ReadAll(r)
	r.Close()
	if err != nil {
		return err
	}
	if !bytes.Equal(got, probe) {
		return errors.New("blob: readback mismatch")
	}
	return s.Delete(ctx, info.SHA256)
}

type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (c ctxReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(p)
}

// S3Store is a placeholder for an s3:// driver (plan: "s3:// driver later").
type S3Store struct{ URL string }

// ErrNotImplemented is returned by every S3Store method.
var ErrNotImplemented = errors.New("blob: s3 store not implemented")

// NewS3 returns an error: the S3 driver is not implemented yet.
func NewS3(spec string) (*S3Store, error) {
	return nil, fmt.Errorf("%w (%s)", ErrNotImplemented, spec)
}

// Put implements Store.
func (*S3Store) Put(context.Context, io.Reader) (Info, error) { return Info{}, ErrNotImplemented }

// PutBytes implements Store.
func (*S3Store) PutBytes(context.Context, []byte) (Info, error) { return Info{}, ErrNotImplemented }

// Open implements Store.
func (*S3Store) Open(context.Context, string) (ReadSeekCloser, Info, error) {
	return nil, Info{}, ErrNotImplemented
}

// Stat implements Store.
func (*S3Store) Stat(context.Context, string) (Info, error) { return Info{}, ErrNotImplemented }

// Delete implements Store.
func (*S3Store) Delete(context.Context, string) error { return ErrNotImplemented }

// Check implements Store.
func (*S3Store) Check(context.Context) error { return ErrNotImplemented }

var _ Store = (*FileStore)(nil)
var _ Store = (*S3Store)(nil)
