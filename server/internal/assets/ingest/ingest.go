// Package ingest turns a pak into content-addressed blobs plus a per-pak
// manifest (docs/ASSETS.md): every directory entry is stored verbatim,
// images additionally get a PNG rendition, and models, sprites, sounds,
// cinematics and maps are parsed for index metadata.
package ingest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path"
	"runtime"
	"strings"
	"sync"

	"quake2web/server/internal/assets/blob"
	"quake2web/server/internal/assets/cin"
	"quake2web/server/internal/assets/img"
	"quake2web/server/internal/assets/manifest"
	"quake2web/server/internal/assets/md2"
	"quake2web/server/internal/assets/pak"
	"quake2web/server/internal/assets/pcx"
	"quake2web/server/internal/assets/sp2"
	"quake2web/server/internal/assets/tga"
	"quake2web/server/internal/assets/wal"
	"quake2web/server/internal/assets/wav"
)

// Content types stored alongside blobs.
const (
	TypePak      = "application/x-quake2-pak"
	TypePNG      = "image/png"
	TypeJSON     = "application/json"
	TypeOctet    = "application/octet-stream"
	TypeWAV      = "audio/wav"
	TypePalette  = "application/x-quake2-palette"
	ColormapPath = "pics/colormap.pcx"
)

// ContentType returns the served media type of a raw entry by kind.
func ContentType(kind string) string {
	switch kind {
	case manifest.KindWAV:
		return TypeWAV
	default:
		return TypeOctet
	}
}

// BlobRef records a stored blob and its role.
type BlobRef struct {
	SHA256      string
	Size        int64
	ContentType string
	Role        string // raw | png | palette | manifest
}

// Options tunes an ingest run.
type Options struct {
	// Palette is used for PNG derivation when the pak has no
	// pics/colormap.pcx of its own. Nil and no own colormap → no PNGs.
	Palette *img.Palette
	// SkipPNG disables image conversion.
	SkipPNG bool
	// Workers bounds parallel conversion (default GOMAXPROCS).
	Workers int
	Log     *slog.Logger
	// Progress is called with (done, total) entries.
	Progress func(done, total int)
}

// Result is the outcome of ingesting one pak.
type Result struct {
	Manifest    *manifest.PakManifest
	ManifestSHA string
	Blobs       []BlobRef // unique by SHA256
	Maps        []manifest.MapInfo
	Palette     *img.Palette // this pak's own palette, if any
	Files       []pak.File   // the pak directory (positions for pak_entries)
}

// FileSHA256 hashes a file on disk.
func FileSHA256(p string) (string, int64, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

// PakFile hashes and ingests a pak file on disk.
func PakFile(ctx context.Context, store blob.Store, filename string, opts Options) (*Result, error) {
	sha, size, err := FileSHA256(filename)
	if err != nil {
		return nil, err
	}
	p, err := pak.Open(filename)
	if err != nil {
		return nil, err
	}
	defer p.Close()
	return Pak(ctx, store, p, path.Base(strings.ReplaceAll(filename, "\\", "/")), sha, size, opts)
}

// LoadPalette extracts the palette of pics/colormap.pcx from a pak.
// C: ref_gl/gl_image.c:1457 Draw_GetPalette
func LoadPalette(p *pak.Pak) (*img.Palette, error) {
	raw, err := p.ReadFile(ColormapPath)
	if err != nil {
		return nil, err
	}
	return PaletteFromPCX(raw)
}

// PaletteFromPCX decodes colormap.pcx bytes and returns its palette.
func PaletteFromPCX(raw []byte) (*img.Palette, error) {
	im, err := pcx.Decode(raw)
	if err != nil {
		return nil, fmt.Errorf("Couldn't load pics/colormap.pcx: %w", err)
	}
	pal, err := img.NewPalette(im.Palette)
	if err != nil {
		return nil, err
	}
	return &pal, nil
}

// KindOf classifies a virtual path by extension.
func KindOf(name string) string {
	ext := manifest.Lower(path.Ext(name))
	switch ext {
	case ".bsp", ".md2", ".sp2", ".wal", ".pcx", ".tga", ".wav", ".cin":
		return ext[1:]
	}
	return manifest.KindOther
}

// ImageType chooses the ref_gl imagetype a PCX/TGA is converted as. The
// renderer decides by call site (Draw_FindPic → pic, R_RegisterSkin / MD2
// skins → skin, SP2 frames → sprite, R_SetSky → sky); ingest approximates
// that from the path, plus the skin names referenced by MD2s in the pak.
func ImageType(name string, kind string, skins map[string]bool) string {
	l := manifest.Lower(name)
	switch {
	case kind == manifest.KindWAL:
		return manifest.ImageWall
	case strings.HasPrefix(l, "env/"):
		return manifest.ImageSky
	case skins[l]:
		return manifest.ImageSkin
	case strings.HasPrefix(l, "pics/"):
		return manifest.ImagePic
	case strings.HasPrefix(l, "sprites/"):
		return manifest.ImageSprite
	case strings.HasPrefix(l, "models/"), strings.HasPrefix(l, "players/"):
		return manifest.ImageSkin
	}
	return manifest.ImagePic
}

type collector struct {
	mu    sync.Mutex
	blobs map[string]BlobRef
	order []string
}

func (c *collector) add(b BlobRef) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.blobs[b.SHA256]; ok {
		return
	}
	c.blobs[b.SHA256] = b
	c.order = append(c.order, b.SHA256)
}

// Pak ingests an opened pak. pakSHA/size describe the pak file itself.
func Pak(ctx context.Context, store blob.Store, p *pak.Pak, name, pakSHA string, size int64, opts Options) (*Result, error) {
	log := opts.Log
	if log == nil {
		log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	workers := opts.Workers
	if workers <= 0 {
		workers = runtime.GOMAXPROCS(0)
	}
	files := p.List()
	pm := &manifest.PakManifest{
		Schema:   manifest.Schema,
		Name:     name,
		SHA256:   pakSHA,
		Size:     size,
		Checksum: p.Checksum,
		NumFiles: len(files),
		Entries:  make([]manifest.Entry, len(files)),
	}
	res := &Result{Manifest: pm, Files: files}
	col := &collector{blobs: map[string]BlobRef{}}

	// palette: own colormap first, then the fallback
	convPal := opts.Palette
	if i := p.Find(ColormapPath); i >= 0 {
		raw, err := p.ReadEntry(i)
		if err == nil {
			if pal, err := PaletteFromPCX(raw); err == nil {
				res.Palette = pal
				convPal = pal
				info, err := store.PutBytes(ctx, pal[:])
				if err != nil {
					return nil, err
				}
				pm.Palette = info.SHA256
				col.add(BlobRef{SHA256: info.SHA256, Size: info.Size, ContentType: TypePalette, Role: "palette"})
			} else {
				log.Warn("ingest: bad colormap", "pak", name, "err", err)
			}
		}
	}
	var table *img.Table
	if convPal != nil && !opts.SkipPNG {
		t := convPal.Table()
		table = &t
		pm.ConvertPalette = blob.Sum(convPal[:])
	}

	// first pass: MD2 skin names (for flood-fill classification)
	skins := map[string]bool{}
	for i, f := range files {
		if KindOf(f.Name) != manifest.KindMD2 {
			continue
		}
		raw, err := p.ReadEntry(i)
		if err != nil {
			continue
		}
		if m, err := md2.Parse(raw); err == nil {
			for _, s := range m.Skins {
				skins[manifest.Lower(s)] = true
			}
		}
	}

	jobs := make(chan int)
	var wg sync.WaitGroup
	var firstErr error
	var errOnce sync.Once
	var doneMu sync.Mutex
	done := 0
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				if err := ingestEntry(ctx, store, p, i, table, skins, pm, col); err != nil {
					errOnce.Do(func() { firstErr = err; cancel() })
				}
				if opts.Progress != nil {
					doneMu.Lock()
					done++
					d := done
					doneMu.Unlock()
					opts.Progress(d, len(files))
				}
			}
		}()
	}
feed:
	for i := range files {
		select {
		case jobs <- i:
		case <-ctx.Done():
			break feed
		}
	}
	close(jobs)
	wg.Wait()
	if firstErr != nil {
		return nil, firstErr
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	for i := range pm.Entries {
		if m := pm.Entries[i].Map; m != nil {
			res.Maps = append(res.Maps, *m)
		}
	}

	mj, err := json.Marshal(pm)
	if err != nil {
		return nil, err
	}
	info, err := store.PutBytes(ctx, mj)
	if err != nil {
		return nil, err
	}
	res.ManifestSHA = info.SHA256
	col.add(BlobRef{SHA256: info.SHA256, Size: info.Size, ContentType: TypeJSON, Role: "manifest"})
	for _, s := range col.order {
		res.Blobs = append(res.Blobs, col.blobs[s])
	}
	return res, nil
}

// ingestEntry stores entry i and fills pm.Entries[i]. Only storage errors
// are returned; parse errors are recorded in the entry.
func ingestEntry(ctx context.Context, store blob.Store, p *pak.Pak, i int, table *img.Table,
	skins map[string]bool, pm *manifest.PakManifest, col *collector) error {
	f := p.Files[i]
	e := &pm.Entries[i]
	e.Path = f.Name
	e.Kind = KindOf(f.Name)
	raw, err := p.ReadEntry(i)
	if err != nil {
		e.Error = err.Error()
		e.Size = int64(f.FileLen)
		return nil
	}
	info, err := store.PutBytes(ctx, raw)
	if err != nil {
		return err
	}
	e.SHA256, e.Size = info.SHA256, info.Size
	col.add(BlobRef{SHA256: info.SHA256, Size: info.Size, ContentType: ContentType(e.Kind), Role: "raw"})

	setErr := func(err error) {
		if err != nil && e.Error == "" {
			e.Error = err.Error()
		}
	}
	var rgba *img.RGBA
	imgType := ""
	switch e.Kind {
	case manifest.KindPCX:
		im, err := pcx.Decode(raw)
		if err != nil {
			setErr(err)
			break
		}
		e.Image = &manifest.ImageInfo{Width: im.Width, Height: im.Height}
		if table != nil {
			imgType = ImageType(f.Name, e.Kind, skins)
			pix := im.Pix
			if imgType == manifest.ImageSkin {
				pix = append([]byte(nil), pix...)
				// C: GL_LoadPic → R_FloodFillSkin for it_skin, bits 8
				img.FloodFillSkin(pix, im.Width, im.Height, table)
			}
			rgba = img.Upload8(pix, im.Width, im.Height, table)
		}
	case manifest.KindWAL:
		mt, pix, err := wal.Decode(raw)
		if mt != nil {
			e.WAL = &manifest.WAL{
				Name: mt.Name, Width: mt.Width, Height: mt.Height,
				Flags: mt.Flags, Contents: mt.Contents, Value: mt.Value, AnimName: mt.AnimName,
			}
			if mt.AnimName != "" {
				// C: gl_model.c Mod_LoadTexinfo / cmodel: "textures/%s.wal"
				e.WAL.AnimNext = "textures/" + mt.AnimName + ".wal"
			}
		}
		if err != nil {
			setErr(err)
			break
		}
		w, h := int(mt.Width), int(mt.Height)
		e.Image = &manifest.ImageInfo{Width: w, Height: h}
		if table != nil {
			imgType = manifest.ImageWall
			rgba = img.Upload8(pix, w, h, table)
		}
	case manifest.KindTGA:
		im, err := tga.Decode(raw)
		if err != nil {
			setErr(err)
			break
		}
		e.Image = &manifest.ImageInfo{Width: im.Width, Height: im.Height}
		if table != nil { // 32-bit: GL_Upload32 as is
			imgType = ImageType(f.Name, e.Kind, skins)
			rgba = &img.RGBA{Width: im.Width, Height: im.Height, Pix: im.Pix}
		}
	case manifest.KindMD2:
		m, err := md2.Parse(raw)
		if err != nil {
			setErr(err)
			break
		}
		d := &manifest.MD2{
			SkinWidth: m.Header.SkinWidth, SkinHeight: m.Header.SkinHeight,
			NumXYZ: m.Header.NumXYZ, NumST: m.Header.NumST, NumTris: m.Header.NumTris,
			NumGLCmds: m.Header.NumGLCmds, NumFrames: m.Header.NumFrames,
			Skins: m.Skins, Frames: make([]string, len(m.Frames)),
		}
		if d.Skins == nil {
			d.Skins = []string{}
		}
		for k := range m.Frames {
			d.Frames[k] = m.Frames[k].Name
		}
		e.MD2 = d
	case manifest.KindSP2:
		s, err := sp2.Parse(raw)
		if err != nil {
			setErr(err)
			break
		}
		fr := s.Frames
		if fr == nil {
			fr = []sp2.Frame{}
		}
		e.SP2 = &manifest.SP2{Frames: fr}
	case manifest.KindWAV:
		wi, err := wav.GetWavinfo(f.Name, raw)
		setErr(err)
		if err == nil || wi.Rate != 0 {
			e.WAV = &wi
		}
	case manifest.KindCIN:
		h, err := cin.Parse(raw)
		setErr(err)
		if h != nil {
			e.CIN = h
		}
	case manifest.KindBSP:
		mi := MapInfo(f.Name, raw)
		mi.SHA256 = e.SHA256
		e.Map = mi
		if mi.Error != "" {
			setErr(errors.New(mi.Error))
		}
	}
	if rgba != nil {
		b, err := rgba.EncodePNG()
		if err != nil {
			return err
		}
		pi, err := store.PutBytes(ctx, b)
		if err != nil {
			return err
		}
		col.add(BlobRef{SHA256: pi.SHA256, Size: pi.Size, ContentType: TypePNG, Role: "png"})
		e.PNG = &manifest.PNG{
			SHA256: pi.SHA256, Size: pi.Size, Width: rgba.Width, Height: rgba.Height,
			HasAlpha: rgba.HasAlpha(), Type: imgType,
		}
	}
	return nil
}
