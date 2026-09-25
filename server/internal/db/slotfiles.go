package db

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"
)

// SlotFiles adapts a per-user SaveStore to the file-oriented save interface
// of the game server (internal/sv SaveStore: Read/Write/Remove/List of named
// files such as server.ssv, game.ssv, <map>.sav, <map>.sv2 inside a slot).
// All files of one slot are kept in a single saves row as a bundle (see
// EncodeBundle). Each Write/Remove is a read-modify-write of the bundle,
// serialized by the adapter's mutex; use one SlotFiles per user.
type SlotFiles struct {
	Store  SaveStore
	UserID int64
	// NotFound is returned by Read for a missing file (set it to
	// sv.ErrNoSave); default ErrNotFound.
	NotFound error
	// Meta, when set, supplies the slot metadata stored with each write.
	Meta func(slot string, files map[string][]byte) SaveMeta
	// Timeout bounds each database call (default 10s).
	Timeout time.Duration

	mu sync.Mutex
}

func (s *SlotFiles) ctx() (context.Context, context.CancelFunc) {
	t := s.Timeout
	if t <= 0 {
		t = 10 * time.Second
	}
	return context.WithTimeout(context.Background(), t)
}

func (s *SlotFiles) notFound() error {
	if s.NotFound != nil {
		return s.NotFound
	}
	return ErrNotFound
}

func (s *SlotFiles) load(ctx context.Context, slot string) (map[string][]byte, SaveMeta, error) {
	blob, info, err := s.Store.Get(ctx, s.UserID, slot)
	if errors.Is(err, ErrNotFound) {
		return map[string][]byte{}, SaveMeta{}, nil
	}
	if err != nil {
		return nil, SaveMeta{}, err
	}
	files, err := DecodeBundle(blob)
	return files, info.SaveMeta, err
}

func (s *SlotFiles) store(ctx context.Context, slot string, files map[string][]byte, meta SaveMeta) error {
	if len(files) == 0 {
		err := s.Store.Delete(ctx, s.UserID, slot)
		if errors.Is(err, ErrNotFound) {
			return nil
		}
		return err
	}
	if s.Meta != nil {
		meta = s.Meta(slot, files)
	}
	return s.Store.Put(ctx, s.UserID, slot, EncodeBundle(files), meta)
}

// Read returns one file of a slot.
func (s *SlotFiles) Read(slot, name string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx, cancel := s.ctx()
	defer cancel()
	files, _, err := s.load(ctx, slot)
	if err != nil {
		return nil, err
	}
	b, ok := files[name]
	if !ok {
		return nil, s.notFound()
	}
	return b, nil
}

// Write stores one file of a slot.
func (s *SlotFiles) Write(slot, name string, data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx, cancel := s.ctx()
	defer cancel()
	files, meta, err := s.load(ctx, slot)
	if err != nil {
		return err
	}
	files[name] = append([]byte(nil), data...)
	return s.store(ctx, slot, files, meta)
}

// Remove deletes one file of a slot (the row goes away with its last file).
func (s *SlotFiles) Remove(slot, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx, cancel := s.ctx()
	defer cancel()
	files, meta, err := s.load(ctx, slot)
	if err != nil {
		return err
	}
	if _, ok := files[name]; !ok {
		return nil
	}
	delete(files, name)
	return s.store(ctx, slot, files, meta)
}

// List returns the sorted file names of a slot.
func (s *SlotFiles) List(slot string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ctx, cancel := s.ctx()
	defer cancel()
	files, _, err := s.load(ctx, slot)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(files))
	for n := range files {
		out = append(out, n)
	}
	sort.Strings(out)
	return out, nil
}

// bundleMagic starts a save bundle ("Q2SB", version 1).
var bundleMagic = []byte("Q2SB\x01")

// EncodeBundle serializes named files: "Q2SB" 0x01, u32 count, then per
// file (sorted by name) u16 name length, name, u32 data length, data; all
// little-endian. This is the body of GET /api/v1/saves/{slot}/data.
func EncodeBundle(files map[string][]byte) []byte {
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	var b bytes.Buffer
	b.Write(bundleMagic)
	binary.Write(&b, binary.LittleEndian, uint32(len(names))) //nolint:errcheck
	for _, n := range names {
		binary.Write(&b, binary.LittleEndian, uint16(len(n))) //nolint:errcheck
		b.WriteString(n)
		binary.Write(&b, binary.LittleEndian, uint32(len(files[n]))) //nolint:errcheck
		b.Write(files[n])
	}
	return b.Bytes()
}

// ErrBadBundle is returned by DecodeBundle for malformed data.
var ErrBadBundle = errors.New("db: malformed save bundle")

// DecodeBundle parses EncodeBundle output.
func DecodeBundle(b []byte) (map[string][]byte, error) {
	if !bytes.HasPrefix(b, bundleMagic) || len(b) < len(bundleMagic)+4 {
		return nil, ErrBadBundle
	}
	p := len(bundleMagic)
	n := binary.LittleEndian.Uint32(b[p:])
	p += 4
	files := make(map[string][]byte)
	for i := uint32(0); i < n; i++ {
		if p+2 > len(b) {
			return nil, ErrBadBundle
		}
		nl := int(binary.LittleEndian.Uint16(b[p:]))
		p += 2
		if p+nl+4 > len(b) {
			return nil, ErrBadBundle
		}
		name := string(b[p : p+nl])
		p += nl
		dl := int(binary.LittleEndian.Uint32(b[p:]))
		p += 4
		if dl < 0 || p+dl > len(b) {
			return nil, ErrBadBundle
		}
		files[name] = append([]byte(nil), b[p:p+dl]...)
		p += dl
	}
	if p != len(b) {
		return nil, fmt.Errorf("%w: trailing data", ErrBadBundle)
	}
	return files, nil
}
