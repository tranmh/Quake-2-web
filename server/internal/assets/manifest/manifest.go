// Package manifest defines the JSON asset index consumed by the TypeScript
// client (docs/ASSETS.md is the normative description) and the per-pak
// manifests it is merged from.
package manifest

import (
	"sort"

	"quake2web/server/internal/assets/cin"
	"quake2web/server/internal/assets/sp2"
	"quake2web/server/internal/assets/wav"
)

// Schema is the version of the index/manifest JSON layout.
const Schema = 1

// Entry kinds (lowercased file extension; everything else is "other").
const (
	KindBSP   = "bsp"
	KindMD2   = "md2"
	KindSP2   = "sp2"
	KindWAL   = "wal"
	KindPCX   = "pcx"
	KindTGA   = "tga"
	KindWAV   = "wav"
	KindCIN   = "cin"
	KindOther = "other"
)

// Image types (ref_gl imagetype_t) used for PNG derivation.
const (
	ImagePic    = "pic"
	ImageSkin   = "skin"
	ImageSprite = "sprite"
	ImageWall   = "wall"
	ImageSky    = "sky"
)

// PNG is an optional derived PNG rendition of an image entry.
type PNG struct {
	SHA256   string `json:"sha256"`
	Size     int64  `json:"size"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
	HasAlpha bool   `json:"hasAlpha"`
	Type     string `json:"type"` // imagetype used for the conversion
}

// ImageInfo describes the source image dimensions (from the raw file).
type ImageInfo struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

// WAL is the miptex_t header of a .wal.
type WAL struct {
	Name     string `json:"name"`
	Width    uint32 `json:"width"`
	Height   uint32 `json:"height"`
	Flags    int32  `json:"flags"`
	Contents int32  `json:"contents"`
	Value    int32  `json:"value"`
	AnimName string `json:"animName"`
	// AnimNext is the virtual path of the next animation frame
	// ("textures/<animName>.wal") or empty.
	AnimNext string `json:"animNext,omitempty"`
}

// MD2 is alias model metadata.
type MD2 struct {
	SkinWidth  int32    `json:"skinWidth"`
	SkinHeight int32    `json:"skinHeight"`
	NumXYZ     int32    `json:"numXyz"`
	NumST      int32    `json:"numSt"`
	NumTris    int32    `json:"numTris"`
	NumGLCmds  int32    `json:"numGlCmds"`
	NumFrames  int32    `json:"numFrames"`
	Skins      []string `json:"skins"`
	Frames     []string `json:"frames"`
}

// SP2 is sprite metadata.
type SP2 struct {
	Frames []sp2.Frame `json:"frames"`
}

// MapInfo is the per-map summary.
type MapInfo struct {
	Name     string `json:"name"` // "demo1"
	Path     string `json:"path"` // "maps/demo1.bsp"
	SHA256   string `json:"sha256"`
	Checksum uint32 `json:"checksum"` // CM_LoadMap checksum (CS_MAPCHECKSUM)
	// Message is the worldspawn "message" key (level title).
	Message   string `json:"message"`
	Sky       string `json:"sky"`
	SkyRotate string `json:"skyRotate,omitempty"`
	SkyAxis   string `json:"skyAxis,omitempty"`
	CDTrack   string `json:"cdTrack,omitempty"` // worldspawn "sounds"
	// SkyImages are the 6 env/ paths R_SetSky loads (TGA, gl_ext_palettedtexture 0).
	SkyImages       []string `json:"skyImages"`
	NumInlineModels int      `json:"numInlineModels"`
	NumTexInfo      int      `json:"numTexInfo"`
	// Textures are the unique "textures/<name>.wal" paths of texinfo, in
	// first-use order (Mod_LoadTexinfo).
	Textures []string `json:"textures"`
	// Models/Sounds/Classnames are hints gathered from the entity string.
	Models     []string `json:"models"`
	Sounds     []string `json:"sounds"`
	Classnames []string `json:"classnames"`
	Error      string   `json:"error,omitempty"`
}

// Entry is one virtual file.
type Entry struct {
	// Path is the name exactly as stored in the pak directory.
	Path string `json:"path"`
	// SHA256 and Size describe the ORIGINAL raw bytes (primary payload).
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
	Kind   string `json:"kind"`
	// Pak is the index into Index.Paks of the pak supplying this file
	// (always 0 inside a per-pak manifest).
	Pak   int         `json:"pak"`
	Image *ImageInfo  `json:"image,omitempty"`
	PNG   *PNG        `json:"png,omitempty"`
	WAL   *WAL        `json:"wal,omitempty"`
	MD2   *MD2        `json:"md2,omitempty"`
	SP2   *SP2        `json:"sp2,omitempty"`
	WAV   *wav.Info   `json:"wav,omitempty"`
	CIN   *cin.Header `json:"cin,omitempty"`
	Map   *MapInfo    `json:"map,omitempty"`
	// Error is set when the file failed validation/parsing; the raw blob
	// is still available.
	Error string `json:"error,omitempty"`
}

// PakManifest is the ingest result of a single pak.
type PakManifest struct {
	Schema   int    `json:"schema"`
	Name     string `json:"name"`
	SHA256   string `json:"sha256"`
	Size     int64  `json:"size"`
	Checksum uint32 `json:"checksum"` // Com_BlockChecksum of the directory (FS_LoadPackFile)
	NumFiles int    `json:"numFiles"`
	// Palette is the sha256 of the 768-byte palette of this pak's
	// pics/colormap.pcx, if it has one.
	Palette string `json:"palette,omitempty"`
	// ConvertPalette is the palette used for PNG derivation (own or fallback).
	ConvertPalette string  `json:"convertPalette,omitempty"`
	Entries        []Entry `json:"entries"` // pak directory order, duplicates included
}

// PakRef identifies a pak in an index.
type PakRef struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	SHA256   string `json:"sha256"`
	Size     int64  `json:"size"`
	Checksum uint32 `json:"checksum"`
	NumFiles int    `json:"numFiles"`
}

// Palette points at the exported palette.
type Palette struct {
	SHA256 string `json:"sha256"` // 768 raw bytes
	Source string `json:"source"` // virtual path it came from
}

// Index is the merged asset index of a pakset.
type Index struct {
	Schema  int      `json:"schema"`
	Pakset  string   `json:"pakset"`
	Paks    []PakRef `json:"paks"` // lowest priority first
	Palette *Palette `json:"palette,omitempty"`
	// Files maps the lowercased virtual path to the entry that wins under
	// files.c search order.
	Files map[string]*Entry `json:"files"`
	Maps  []MapInfo         `json:"maps"`
}

// Lower is ASCII lowercasing as Q_strcasecmp compares names.
func Lower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + ('a' - 'A')
		}
	}
	return string(b)
}

// Merge builds the pakset index. paks are ordered lowest priority first
// (pak0, pak1, …): a later pak overrides an earlier one, and within one pak
// the first directory entry with a given name wins (FS_FOpenFile scans the
// directory front to back).
// C: qcommon/files.c:206 FS_FOpenFile, :513 FS_AddGameDirectory
func Merge(pakset string, refs []PakRef, paks []*PakManifest) *Index {
	idx := &Index{Schema: Schema, Pakset: pakset, Paks: refs, Files: map[string]*Entry{}, Maps: []MapInfo{}}
	for i, pm := range paks {
		seen := make(map[string]bool, len(pm.Entries))
		for j := range pm.Entries {
			e := pm.Entries[j] // copy
			k := Lower(e.Path)
			if seen[k] {
				continue
			}
			seen[k] = true
			e.Pak = i
			idx.Files[k] = &e
		}
	}
	if e, ok := idx.Files["pics/colormap.pcx"]; ok && paks[e.Pak].Palette != "" {
		idx.Palette = &Palette{SHA256: paks[e.Pak].Palette, Source: "pics/colormap.pcx"}
	}
	for _, e := range idx.Files {
		if e.Map != nil {
			idx.Maps = append(idx.Maps, *e.Map)
		}
	}
	sort.Slice(idx.Maps, func(a, b int) bool { return idx.Maps[a].Name < idx.Maps[b].Name })
	return idx
}

// Lookup returns the entry for a virtual path (case-insensitive).
func (idx *Index) Lookup(path string) *Entry { return idx.Files[Lower(path)] }

// SHAs returns every blob hash referenced by the index (raw, PNG, palette).
func (idx *Index) SHAs() []string {
	set := map[string]bool{}
	for _, e := range idx.Files {
		set[e.SHA256] = true
		if e.PNG != nil {
			set[e.PNG.SHA256] = true
		}
	}
	if idx.Palette != nil {
		set[idx.Palette.SHA256] = true
	}
	out := make([]string, 0, len(set))
	for s := range set {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}
