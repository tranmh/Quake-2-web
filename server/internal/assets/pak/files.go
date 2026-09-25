// Package pak ports the pak-file and search-path parts of qcommon/files.c
// (qfiles.h dpackheader_t / dpackfile_t).
package pak

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/md4"
	"quake2web/server/internal/qcommon/shared"
)

const (
	headerSize  = 12 // sizeof(dpackheader_t)
	dirEntSize  = 64 // sizeof(dpackfile_t)
	nameSize    = 56
	maxFileSize = 1 << 31
)

// ErrNotFound is returned when a file is in no pak / directory of the path.
var ErrNotFound = errors.New("pak: file not found")

// File is C packfile_t (one directory entry).
// C: qcommon/files.c:51 packfile_t
type File struct {
	Name    string
	FilePos int32
	FileLen int32
}

// Pak is C pack_t: an opened pak file with its directory.
// C: qcommon/files.c:59 pack_t
type Pak struct {
	Filename string
	Files    []File
	// Checksum is Com_BlockChecksum over the raw directory, as computed
	// (and ignored unless NO_ADDONS) by FS_LoadPackFile.
	Checksum uint32

	r      io.ReaderAt
	size   int64
	closer io.Closer
}

// Open opens a pak file from disk. The file stays open until Close.
// C: qcommon/files.c:448 FS_LoadPackFile
func Open(path string) (*Pak, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	p, err := load(path, f, st.Size())
	if err != nil {
		f.Close()
		return nil, err
	}
	p.closer = f
	return p, nil
}

// OpenBytes parses a pak held in memory.
func OpenBytes(name string, data []byte) (*Pak, error) {
	return load(name, bytes.NewReader(data), int64(len(data)))
}

// load is the body of FS_LoadPackFile with bounds checks instead of the C
// fatal errors / unchecked reads.
// C: qcommon/files.c:448 FS_LoadPackFile
func load(name string, r io.ReaderAt, size int64) (*Pak, error) {
	var hdr [headerSize]byte
	if size < headerSize {
		return nil, fmt.Errorf("%s is not a packfile", name)
	}
	if _, err := r.ReadAt(hdr[:], 0); err != nil {
		return nil, err
	}
	ident := int32(binary.LittleEndian.Uint32(hdr[0:]))
	if ident != q2const.IDPAKHEADER {
		return nil, fmt.Errorf("%s is not a packfile", name)
	}
	dirofs := int32(binary.LittleEndian.Uint32(hdr[4:]))
	dirlen := int32(binary.LittleEndian.Uint32(hdr[8:]))

	if dirofs < 0 || dirlen < 0 || int64(dirofs)+int64(dirlen) > size {
		return nil, fmt.Errorf("%s: directory out of bounds", name)
	}
	numpackfiles := int(dirlen) / dirEntSize
	if numpackfiles > q2const.MAX_FILES_IN_PACK {
		return nil, fmt.Errorf("%s has %d files", name, numpackfiles)
	}

	info := make([]byte, dirlen)
	if _, err := r.ReadAt(info, int64(dirofs)); err != nil && !(errors.Is(err, io.EOF) && dirlen == 0) {
		return nil, err
	}

	p := &Pak{
		Filename: name,
		Checksum: md4.Com_BlockChecksum(info),
		r:        r,
		size:     size,
		Files:    make([]File, numpackfiles),
	}
	for i := 0; i < numpackfiles; i++ {
		e := info[i*dirEntSize : (i+1)*dirEntSize]
		n := e[:nameSize]
		if j := bytes.IndexByte(n, 0); j >= 0 {
			n = n[:j]
		}
		p.Files[i] = File{
			Name:    string(n),
			FilePos: int32(binary.LittleEndian.Uint32(e[56:])),
			FileLen: int32(binary.LittleEndian.Uint32(e[60:])),
		}
	}
	return p, nil
}

// Close releases the underlying file (no-op for OpenBytes).
func (p *Pak) Close() error {
	if p.closer != nil {
		return p.closer.Close()
	}
	return nil
}

// List returns the directory in on-disk order.
func (p *Pak) List() []File { return p.Files }

// Find returns the index of the first entry whose name matches filename
// case-insensitively (Q_strcasecmp), or -1.
// C: qcommon/files.c:238 FS_FOpenFile (pak branch)
func (p *Pak) Find(filename string) int {
	for i := range p.Files {
		if shared.Q_strcasecmp(p.Files[i].Name, filename) == 0 {
			return i
		}
	}
	return -1
}

// ReadEntry returns the bytes of directory entry i.
func (p *Pak) ReadEntry(i int) ([]byte, error) {
	f := p.Files[i]
	if f.FilePos < 0 || f.FileLen < 0 || int64(f.FilePos)+int64(f.FileLen) > p.size {
		return nil, fmt.Errorf("%s: entry %q out of bounds", p.Filename, f.Name)
	}
	buf := make([]byte, f.FileLen)
	if f.FileLen == 0 {
		return buf, nil
	}
	if _, err := p.r.ReadAt(buf, int64(f.FilePos)); err != nil {
		return nil, err
	}
	return buf, nil
}

// ReadFile returns the contents of filename.
func (p *Pak) ReadFile(filename string) ([]byte, error) {
	i := p.Find(filename)
	if i < 0 {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, filename)
	}
	return p.ReadEntry(i)
}

// searchPath is C searchpath_t: either a loose directory or a pak.
// C: qcommon/files.c:81 searchpath_t
type searchPath struct {
	dir string
	pak *Pak
}

// FS is the ordered search path (fs_searchpaths). Index 0 is searched first.
type FS struct {
	paths   []searchPath
	gamedir string
}

// AddPak pushes an opened pak to the head of the search path.
func (fs *FS) AddPak(p *Pak) {
	fs.paths = append([]searchPath{{pak: p}}, fs.paths...)
}

// AddDir pushes a loose directory to the head of the search path.
func (fs *FS) AddDir(dir string) {
	fs.paths = append([]searchPath{{dir: dir}}, fs.paths...)
}

// AddGameDirectory adds dir to the head of the path, then pak0.pak ..
// pak9.pak in that order, each pushed to the head, so later paks override
// earlier ones and all paks override loose files of the same directory.
// C: qcommon/files.c:513 FS_AddGameDirectory
func (fs *FS) AddGameDirectory(dir string) error {
	fs.gamedir = dir
	fs.AddDir(dir)
	for i := 0; i < 10; i++ {
		pakfile := filepath.Join(dir, fmt.Sprintf("pak%d.pak", i))
		p, err := Open(pakfile)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return err
		}
		fs.AddPak(p)
	}
	return nil
}

// Gamedir returns the last directory added by AddGameDirectory.
// C: qcommon/files.c:555 FS_Gamedir
func (fs *FS) Gamedir() string { return fs.gamedir }

// Paks returns the paks of the search path in search order.
func (fs *FS) Paks() []*Pak {
	var out []*Pak
	for _, s := range fs.paths {
		if s.pak != nil {
			out = append(out, s.pak)
		}
	}
	return out
}

// ReadFile finds filename in the search path, one element at a time.
// C: qcommon/files.c:206 FS_FOpenFile / files.c:394 FS_LoadFile
func (fs *FS) ReadFile(filename string) ([]byte, error) {
	for _, s := range fs.paths {
		if s.pak != nil {
			if i := s.pak.Find(filename); i >= 0 {
				return s.pak.ReadEntry(i)
			}
			continue
		}
		// check a file in the directory tree; reject path escapes
		if strings.Contains(filename, "..") || filepath.IsAbs(filename) {
			continue
		}
		b, err := os.ReadFile(filepath.Join(s.dir, filepath.FromSlash(filename)))
		if err != nil {
			continue
		}
		return b, nil
	}
	return nil, fmt.Errorf("%w: %s", ErrNotFound, filename)
}

// Close closes every pak in the search path.
func (fs *FS) Close() error {
	var first error
	for _, s := range fs.paths {
		if s.pak != nil {
			if err := s.pak.Close(); err != nil && first == nil {
				first = err
			}
		}
	}
	fs.paths = nil
	return first
}
