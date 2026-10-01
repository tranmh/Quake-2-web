package host

import (
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"quake2web/server/internal/sv"
)

// OverlayFS is an sv.FileSystem that serves in-memory Files in front of a
// Base file system, e.g. a recorded demo as "demos/<name>.dm2" over the
// pakset for "demomap". Names are matched like FS_FOpenFile in a pak:
// exactly first, then case-insensitively (the lexically smallest key wins
// when several fold to the same name). The returned bytes are a copy.
type OverlayFS struct {
	Files map[string][]byte
	Base  sv.FileSystem
}

var _ sv.FileSystem = (*OverlayFS)(nil)

// ReadFile implements sv.FileSystem.
func (o *OverlayFS) ReadFile(name string) ([]byte, error) {
	if o == nil {
		return nil, fmt.Errorf("%s: %w", name, fs.ErrNotExist)
	}
	if b, ok := o.Files[name]; ok {
		return append([]byte(nil), b...), nil
	}
	var keys []string
	for k := range o.Files {
		if strings.EqualFold(k, name) {
			keys = append(keys, k)
		}
	}
	if len(keys) > 0 {
		sort.Strings(keys)
		return append([]byte(nil), o.Files[keys[0]]...), nil
	}
	if o.Base == nil {
		return nil, fmt.Errorf("%s: %w", name, fs.ErrNotExist)
	}
	return o.Base.ReadFile(name)
}
