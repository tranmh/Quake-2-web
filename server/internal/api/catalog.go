package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"quake2web/server/internal/assets/blob"
	"quake2web/server/internal/assets/img"
	"quake2web/server/internal/assets/ingest"
	"quake2web/server/internal/assets/manifest"
	"quake2web/server/internal/assets/pak"
	"quake2web/server/internal/db"
)

// DemoPaksetID is the id of the public pakset built from the demo pak.
const DemoPaksetID = "demo"

// ErrPaksetNotReady is returned when a pakset references a pak whose ingest
// has not finished.
var ErrPaksetNotReady = errors.New("pakset: a pak is not ingested yet")

// Catalog owns pak ingest (a bounded job queue) and the per-pakset asset
// index cache.
type Catalog struct {
	repo      db.Repo
	store     blob.Store
	log       *slog.Logger
	uploadDir string
	onJob     func(status string)

	mu       sync.Mutex
	palette  *img.Palette
	mans     map[string]*manifest.PakManifest // by manifest sha256
	indexes  map[string]*CachedIndex          // by pakset id
	closed   bool
	queue    chan ingestReq
	wg       sync.WaitGroup
	baseCtx  context.Context
	cancel   context.CancelFunc
	inflight map[int64]bool
}

// CachedIndex is a merged, serialized pakset index.
type CachedIndex struct {
	key   string
	Index *manifest.Index
	JSON  []byte
	// ETag is the quoted sha256 of JSON.
	ETag string
}

type ingestReq struct {
	pak   db.Pak
	path  string // local pak file
	jobID int64
}

// NewCatalog starts workers ingest workers.
func NewCatalog(repo db.Repo, store blob.Store, log *slog.Logger, workers int, uploadDir string) *Catalog {
	if workers <= 0 {
		workers = 1
	}
	if log == nil {
		log = slog.Default()
	}
	if uploadDir == "" {
		if fs, ok := store.(*blob.FileStore); ok {
			uploadDir = filepath.Join(fs.Dir(), "tmp")
		} else {
			uploadDir = os.TempDir()
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	c := &Catalog{
		repo: repo, store: store, log: log, uploadDir: uploadDir,
		mans: map[string]*manifest.PakManifest{}, indexes: map[string]*CachedIndex{},
		queue: make(chan ingestReq, 64), baseCtx: ctx, cancel: cancel, inflight: map[int64]bool{},
	}
	for i := 0; i < workers; i++ {
		c.wg.Add(1)
		go c.worker()
	}
	return c
}

// UploadDir is where uploads are spooled.
func (c *Catalog) UploadDir() string { return c.uploadDir }

// Close stops the workers (running ingests are cancelled).
func (c *Catalog) Close() {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.closed = true
	close(c.queue)
	c.mu.Unlock()
	c.cancel()
	c.wg.Wait()
}

// Palette returns the fallback palette (from the first pak ingested with a
// pics/colormap.pcx), or nil.
func (c *Catalog) Palette() *img.Palette {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.palette
}

func (c *Catalog) setPalette(p *img.Palette) {
	if p == nil {
		return
	}
	c.mu.Lock()
	if c.palette == nil {
		c.palette = p
	}
	c.mu.Unlock()
}

func (c *Catalog) invalidate() {
	c.mu.Lock()
	c.indexes = map[string]*CachedIndex{}
	c.mu.Unlock()
}

func (c *Catalog) worker() {
	defer c.wg.Done()
	for req := range c.queue {
		c.runJob(c.baseCtx, req)
	}
}

// Enqueue schedules an ingest of a local pak file (which must stay in place
// until the job finishes; uploads live in the blob store).
func (c *Catalog) Enqueue(ctx context.Context, p db.Pak, path string, userID int64) (db.Job, error) {
	job, err := c.repo.CreateJob(ctx, db.Job{Kind: "ingest", PakID: p.ID, UserID: userID})
	if err != nil {
		return db.Job{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return job, errors.New("catalog closed")
	}
	select {
	case c.queue <- ingestReq{pak: p, path: path, jobID: job.ID}:
	default:
		c.repo.UpdateJob(ctx, job.ID, db.JobFailed, 0, "ingest queue full") //nolint:errcheck
		return job, errors.New("ingest queue full")
	}
	return job, nil
}

func (c *Catalog) runJob(ctx context.Context, req ingestReq) {
	c.mu.Lock()
	if c.inflight[req.pak.ID] {
		c.mu.Unlock()
		// another job is ingesting the same pak; wait for it by polling
		for i := 0; i < 600; i++ {
			time.Sleep(100 * time.Millisecond)
			c.mu.Lock()
			busy := c.inflight[req.pak.ID]
			c.mu.Unlock()
			if !busy {
				break
			}
		}
		p, err := c.repo.PakByID(ctx, req.pak.ID)
		if err == nil && p.Status == db.PakReady {
			c.repo.UpdateJob(ctx, req.jobID, db.JobDone, 1, "") //nolint:errcheck
			return
		}
		c.mu.Lock()
	}
	c.inflight[req.pak.ID] = true
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.inflight, req.pak.ID)
		c.mu.Unlock()
	}()
	err := c.IngestPak(ctx, req.pak, req.path, req.jobID)
	status := db.JobDone
	if err != nil {
		status = db.JobFailed
	}
	c.mu.Lock()
	hook := c.onJob
	c.mu.Unlock()
	if hook != nil {
		hook(status)
	}
}

// SetJobHook registers a callback run after each queued job ("done" or
// "failed"); the router uses it for the q2_ingest_jobs metric.
func (c *Catalog) SetJobHook(f func(status string)) {
	c.mu.Lock()
	c.onJob = f
	c.mu.Unlock()
}

// IngestPak ingests a pak file synchronously and records the result.
// jobID 0 skips job bookkeeping.
func (c *Catalog) IngestPak(ctx context.Context, p db.Pak, path string, jobID int64) error {
	log := c.log.With("pak_id", p.ID, "pak", p.Name, "sha256", p.SHA256)
	t0 := time.Now()
	// progress updates use a detached context so a cancelled request does
	// not abort bookkeeping
	bg := context.WithoutCancel(ctx)
	c.repo.SetPakStatus(bg, p.ID, db.PakIngesting, "") //nolint:errcheck
	if jobID != 0 {
		c.repo.UpdateJob(bg, jobID, db.JobRunning, 0, "") //nolint:errcheck
	}
	var lastTenth int
	var pmu sync.Mutex
	res, err := ingest.PakFile(ctx, c.store, path, ingest.Options{
		Palette: c.Palette(),
		Log:     log,
		Progress: func(done, total int) {
			if jobID == 0 || total == 0 {
				return
			}
			t := done * 10 / total
			pmu.Lock()
			defer pmu.Unlock()
			if t > lastTenth {
				lastTenth = t
				c.repo.UpdateJob(bg, jobID, db.JobRunning, float32(done)/float32(total)*0.95, "") //nolint:errcheck
			}
		},
	})
	fail := func(err error) error {
		log.Error("ingest failed", "err", err)
		c.repo.SetPakStatus(bg, p.ID, db.PakFailed, err.Error()) //nolint:errcheck
		if jobID != 0 {
			c.repo.UpdateJob(bg, jobID, db.JobFailed, 0, err.Error()) //nolint:errcheck
		}
		return err
	}
	if err != nil {
		return fail(err)
	}
	if res.Manifest.SHA256 != p.SHA256 {
		return fail(fmt.Errorf("pak changed on disk (sha256 %s, expected %s)", res.Manifest.SHA256, p.SHA256))
	}
	rec := db.PakIngest{
		Checksum:       res.Manifest.Checksum,
		NumFiles:       res.Manifest.NumFiles,
		ManifestSHA256: res.ManifestSHA,
	}
	for _, b := range res.Blobs {
		rec.Blobs = append(rec.Blobs, db.Blob{SHA256: b.SHA256, Size: b.Size, ContentType: b.ContentType})
		rec.Assets = append(rec.Assets, db.PakAsset{SHA256: b.SHA256, Role: b.Role})
	}
	if _, err := c.store.Stat(ctx, p.SHA256); err == nil {
		rec.Blobs = append(rec.Blobs, db.Blob{SHA256: p.SHA256, Size: p.Size, ContentType: ingest.TypePak})
		rec.Assets = append(rec.Assets, db.PakAsset{SHA256: p.SHA256, Role: "pak"})
	}
	for i, f := range res.Files {
		e := res.Manifest.Entries[i]
		rec.Entries = append(rec.Entries, db.PakEntry{Idx: i, Name: f.Name, FilePos: f.FilePos, FileLen: f.FileLen,
			SHA256: e.SHA256, Kind: e.Kind})
	}
	for _, m := range res.Maps {
		info, _ := json.Marshal(m)
		rec.Maps = append(rec.Maps, db.MapRow{Path: m.Path, Name: m.Name, SHA256: m.SHA256, Checksum: m.Checksum,
			Message: m.Message, Sky: m.Sky, Info: info})
	}
	if err := c.repo.FinishPakIngest(bg, p.ID, rec); err != nil {
		return fail(err)
	}
	if jobID != 0 {
		c.repo.UpdateJob(bg, jobID, db.JobDone, 1, "") //nolint:errcheck
	}
	c.setPalette(res.Palette)
	c.mu.Lock()
	c.mans[res.ManifestSHA] = res.Manifest
	c.mu.Unlock()
	c.invalidate()
	log.Info("ingest done", "files", res.Manifest.NumFiles, "blobs", len(res.Blobs), "maps", len(res.Maps),
		"took_ms", time.Since(t0).Milliseconds())
	return nil
}

// EnsureDemo ingests the demo pak (idempotent) as a public pak and points
// the public pakset "demo" at it.
func (c *Catalog) EnsureDemo(ctx context.Context, path string) (db.Pakset, error) {
	sha, size, err := ingest.FileSHA256(path)
	if err != nil {
		return db.Pakset{}, err
	}
	p, _, err := c.repo.CreatePak(ctx, db.Pak{SHA256: sha, Name: filepath.Base(path), Size: size, Public: true})
	if err != nil {
		return db.Pakset{}, err
	}
	if !p.Public {
		if err := c.repo.SetPakPublic(ctx, p.ID, true); err != nil {
			return db.Pakset{}, err
		}
	}
	ready := p.Status == db.PakReady
	if ready {
		if _, err := c.store.Stat(ctx, p.ManifestSHA256); err != nil {
			ready = false // blob dir was wiped
		}
	}
	if !ready {
		if err := c.IngestPak(ctx, p, path, 0); err != nil {
			return db.Pakset{}, err
		}
	} else if c.Palette() == nil {
		if pk, err := pak.Open(path); err == nil {
			if pal, err := ingest.LoadPalette(pk); err == nil {
				c.setPalette(pal)
			}
			pk.Close()
		}
	}
	ps, err := c.repo.UpsertPakset(ctx, db.Pakset{ID: DemoPaksetID, Name: "Quake II demo", Public: true, PakIDs: []int64{p.ID}})
	if err != nil {
		return db.Pakset{}, err
	}
	c.invalidate()
	return ps, nil
}

func (c *Catalog) loadManifest(ctx context.Context, sha string) (*manifest.PakManifest, error) {
	c.mu.Lock()
	m := c.mans[sha]
	c.mu.Unlock()
	if m != nil {
		return m, nil
	}
	r, _, err := c.store.Open(ctx, sha)
	if err != nil {
		return nil, fmt.Errorf("manifest %s: %w", sha, err)
	}
	defer r.Close()
	m = &manifest.PakManifest{}
	if err := json.NewDecoder(r).Decode(m); err != nil {
		return nil, fmt.Errorf("manifest %s: %w", sha, err)
	}
	c.mu.Lock()
	c.mans[sha] = m
	c.mu.Unlock()
	return m, nil
}

// Index returns the merged index of a pakset (cached until a pakset or pak
// changes).
func (c *Catalog) Index(ctx context.Context, ps db.Pakset) (*CachedIndex, error) {
	paks := make([]db.Pak, 0, len(ps.PakIDs))
	var key strings.Builder
	key.WriteString(ps.ID)
	for _, id := range ps.PakIDs {
		p, err := c.repo.PakByID(ctx, id)
		if err != nil {
			return nil, err
		}
		if p.Status != db.PakReady || p.ManifestSHA256 == "" {
			return nil, ErrPaksetNotReady
		}
		paks = append(paks, p)
		fmt.Fprintf(&key, "|%d:%s", p.ID, p.ManifestSHA256)
	}
	c.mu.Lock()
	ci := c.indexes[ps.ID]
	c.mu.Unlock()
	if ci != nil && ci.key == key.String() {
		return ci, nil
	}
	refs := make([]manifest.PakRef, len(paks))
	mans := make([]*manifest.PakManifest, len(paks))
	for i, p := range paks {
		m, err := c.loadManifest(ctx, p.ManifestSHA256)
		if err != nil {
			return nil, err
		}
		mans[i] = m
		refs[i] = manifest.PakRef{ID: p.ID, Name: p.Name, SHA256: p.SHA256, Size: p.Size, Checksum: p.Checksum, NumFiles: p.NumFiles}
	}
	idx := manifest.Merge(ps.ID, refs, mans)
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(idx); err != nil {
		return nil, err
	}
	sum := sha256.Sum256(buf.Bytes())
	ci = &CachedIndex{key: key.String(), Index: idx, JSON: buf.Bytes(), ETag: `"` + hex.EncodeToString(sum[:]) + `"`}
	c.mu.Lock()
	c.indexes[ps.ID] = ci
	c.mu.Unlock()
	return ci, nil
}

// SpoolUpload streams r into a temp file under the upload dir while hashing
// it; at most max bytes are accepted.
func (c *Catalog) SpoolUpload(r io.Reader, max int64) (path, sha string, size int64, err error) {
	if err := os.MkdirAll(c.uploadDir, 0o755); err != nil {
		return "", "", 0, err
	}
	f, err := os.CreateTemp(c.uploadDir, "upload-*.pak")
	if err != nil {
		return "", "", 0, err
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(f, h), io.LimitReader(r, max+1))
	cerr := f.Close()
	if err == nil {
		err = cerr
	}
	if err == nil && n > max {
		err = errUploadTooLarge
	}
	if err != nil {
		os.Remove(f.Name())
		return "", "", 0, err
	}
	return f.Name(), hex.EncodeToString(h.Sum(nil)), n, nil
}

var errUploadTooLarge = errors.New("upload too large")

// StorePak moves a spooled, validated pak into the blob store and returns
// a local path to ingest from.
func (c *Catalog) StorePak(ctx context.Context, tmp, sha string, size int64) (string, error) {
	if fs, ok := c.store.(*blob.FileStore); ok {
		if _, err := fs.PutFile(tmp, sha, size); err != nil {
			return "", err
		}
		return fs.LocalPath(sha)
	}
	f, err := os.Open(tmp)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := c.store.Put(ctx, f); err != nil {
		return "", err
	}
	if l, ok := c.store.(blob.Localer); ok {
		os.Remove(tmp)
		return l.LocalPath(sha)
	}
	return tmp, nil // keep the spool file as ingest source
}
