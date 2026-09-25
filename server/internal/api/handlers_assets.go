package api

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"mime"
	"net/http"
	"os"
	"path"
	"strconv"
	"strings"
	"unicode/utf8"

	"quake2web/server/internal/assets/blob"
	"quake2web/server/internal/assets/manifest"
	"quake2web/server/internal/assets/pak"
	"quake2web/server/internal/db"
)

func pathInt(r *http.Request, name string) (int64, bool) {
	n, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	return n, err == nil && n > 0
}

type pakResponse struct {
	Pak   db.Pak  `json:"pak"`
	Owned bool    `json:"owned"`
	Job   *db.Job `json:"job,omitempty"`
}

// POST /api/v1/paks (multipart/form-data, field "file") → 202 {pak, job}
// for a new pak, 200 {pak} when the same content was ingested before.
func (s *server) uploadPak(w http.ResponseWriter, r *http.Request) {
	u, ok := requireUser(w, r)
	if !ok {
		return
	}
	ct, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || ct != "multipart/form-data" {
		writeError(w, http.StatusUnsupportedMediaType, "unsupported_media_type", "Content-Type must be multipart/form-data")
		return
	}
	max := s.Config.MaxUploadBytes
	r.Body = http.MaxBytesReader(w, r.Body, max+1<<20) // multipart overhead
	mr, err := r.MultipartReader()
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad_multipart", err.Error())
		return
	}
	var tmp, sha, filename string
	var size int64
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			s.uploadError(w, err)
			return
		}
		if part.FormName() != "file" || tmp != "" {
			io.Copy(io.Discard, part) //nolint:errcheck
			part.Close()
			continue
		}
		filename = path.Base(strings.ReplaceAll(part.FileName(), "\\", "/"))
		tmp, sha, size, err = s.Catalog.SpoolUpload(part, max)
		part.Close()
		if err != nil {
			s.uploadError(w, err)
			return
		}
	}
	if tmp == "" {
		writeFieldError(w, "file", "missing file part")
		return
	}
	keepTmp := false
	defer func() {
		if !keepTmp {
			os.Remove(tmp)
		}
	}()
	if filename == "" || filename == "." || filename == "/" || !utf8.ValidString(filename) || len(filename) > 128 {
		filename = "upload.pak"
	}
	// validate it is a pak (FS_LoadPackFile)
	pk, err := pak.Open(tmp)
	if err != nil {
		writeFieldError(w, "file", "not a valid pak file: "+err.Error())
		return
	}
	pk.Close()

	ctx := r.Context()
	p, created, err := s.Repo.CreatePak(ctx, db.Pak{SHA256: sha, Name: filename, Size: size})
	if err != nil {
		s.internal(w, r, err)
		return
	}
	if err := s.Repo.AddPakOwner(ctx, p.ID, u.ID, filename); err != nil {
		s.internal(w, r, err)
		return
	}
	needIngest := created || p.Status == db.PakFailed
	if !needIngest {
		if _, err := s.Store.Stat(ctx, p.ManifestSHA256); p.Status == db.PakReady && err != nil {
			needIngest = true
		}
	}
	if !needIngest {
		writeJSON(w, http.StatusOK, pakResponse{Pak: p, Owned: true})
		return
	}
	src, err := s.Catalog.StorePak(ctx, tmp, sha, size)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	keepTmp = src == tmp
	p.Status = db.PakPending
	s.Repo.SetPakStatus(ctx, p.ID, db.PakPending, "") //nolint:errcheck
	job, err := s.Catalog.Enqueue(ctx, p, src, u.ID)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "busy", err.Error())
		return
	}
	writeJSON(w, http.StatusAccepted, pakResponse{Pak: p, Owned: true, Job: &job})
}

func (s *server) uploadError(w http.ResponseWriter, err error) {
	var mbe *http.MaxBytesError
	if errors.Is(err, errUploadTooLarge) || errors.As(err, &mbe) {
		writeError(w, http.StatusRequestEntityTooLarge, "too_large",
			"pak exceeds the upload limit of "+strconv.FormatInt(s.Config.MaxUploadBytes, 10)+" bytes")
		return
	}
	writeError(w, http.StatusBadRequest, "bad_upload", err.Error())
}

// GET /api/v1/paks → {paks:[...]} (own + public).
func (s *server) listPaks(w http.ResponseWriter, r *http.Request) {
	paks, err := s.Repo.ListPaks(r.Context(), userID(r))
	if err != nil {
		s.internal(w, r, err)
		return
	}
	if paks == nil {
		paks = []db.Pak{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"paks": paks})
}

// visiblePak loads a pak the caller may see (public or owned).
func (s *server) visiblePak(w http.ResponseWriter, r *http.Request, id int64) (db.Pak, bool, bool) {
	p, err := s.Repo.PakByID(r.Context(), id)
	if errors.Is(err, db.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "pak not found")
		return p, false, false
	}
	if err != nil {
		s.internal(w, r, err)
		return p, false, false
	}
	owned := false
	if uid := userID(r); uid != 0 {
		owned, err = s.Repo.PakOwned(r.Context(), id, uid)
		if err != nil {
			s.internal(w, r, err)
			return p, false, false
		}
	}
	if !p.Public && !owned {
		writeError(w, http.StatusNotFound, "not_found", "pak not found")
		return p, false, false
	}
	return p, owned, true
}

// GET /api/v1/paks/{id} → {pak, owned, jobs, entries?}; ?entries=1 adds the
// directory with content hashes.
func (s *server) getPak(w http.ResponseWriter, r *http.Request) {
	id, ok := pathInt(r, "id")
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "pak not found")
		return
	}
	p, owned, ok := s.visiblePak(w, r, id)
	if !ok {
		return
	}
	jobs, err := s.Repo.JobsForPak(r.Context(), id)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	if jobs == nil {
		jobs = []db.Job{}
	}
	out := map[string]any{"pak": p, "owned": owned, "jobs": jobs}
	if r.URL.Query().Get("entries") != "" {
		entries, err := s.Repo.PakEntries(r.Context(), id)
		if err != nil {
			s.internal(w, r, err)
			return
		}
		if entries == nil {
			entries = []db.PakEntry{}
		}
		out["entries"] = entries
	}
	writeJSON(w, http.StatusOK, out)
}

// GET /api/v1/jobs/{id} → {job} (only the job's user).
func (s *server) getJob(w http.ResponseWriter, r *http.Request) {
	u, ok := requireUser(w, r)
	if !ok {
		return
	}
	id, ok := pathInt(r, "id")
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "job not found")
		return
	}
	j, err := s.Repo.JobByID(r.Context(), id)
	if err != nil || (j.UserID != u.ID && !u.IsAdmin) {
		writeError(w, http.StatusNotFound, "not_found", "job not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"job": j})
}

// ---- paksets ----

type paksetInput struct {
	Name   string  `json:"name"`
	Paks   []int64 `json:"paks"`
	Public bool    `json:"public"`
}

func newPaksetID() string {
	var b [8]byte
	rand.Read(b[:]) //nolint:errcheck
	return hex.EncodeToString(b[:])
}

// validatePaksetInput checks name and that the caller may use every pak.
func (s *server) validatePaksetInput(w http.ResponseWriter, r *http.Request, u db.User, in *paksetInput) bool {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || utf8.RuneCountInString(in.Name) > 64 {
		writeFieldError(w, "name", "name must be 1 to 64 characters")
		return false
	}
	if len(in.Paks) == 0 || len(in.Paks) > 32 {
		writeFieldError(w, "paks", "a pakset needs 1 to 32 paks")
		return false
	}
	if in.Public && !u.IsAdmin {
		writeFieldError(w, "public", "only administrators can publish paksets")
		return false
	}
	seen := map[int64]bool{}
	for _, id := range in.Paks {
		if seen[id] {
			writeFieldError(w, "paks", "duplicate pak "+strconv.FormatInt(id, 10))
			return false
		}
		seen[id] = true
		p, err := s.Repo.PakByID(r.Context(), id)
		if err != nil {
			writeFieldError(w, "paks", "unknown pak "+strconv.FormatInt(id, 10))
			return false
		}
		owned, err := s.Repo.PakOwned(r.Context(), id, u.ID)
		if err != nil {
			s.internal(w, r, err)
			return false
		}
		if !p.Public && !owned {
			writeFieldError(w, "paks", "unknown pak "+strconv.FormatInt(id, 10))
			return false
		}
	}
	return true
}

// GET /api/v1/paksets → {paksets:[...]} (public + own).
func (s *server) listPaksets(w http.ResponseWriter, r *http.Request) {
	list, err := s.Repo.ListPaksets(r.Context(), userID(r))
	if err != nil {
		s.internal(w, r, err)
		return
	}
	if list == nil {
		list = []db.Pakset{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"paksets": list})
}

// POST /api/v1/paksets {name, paks:[ids lowest priority first], public} → 201 {pakset}.
func (s *server) createPakset(w http.ResponseWriter, r *http.Request) {
	u, ok := requireUser(w, r)
	if !ok {
		return
	}
	var in paksetInput
	if !decodeJSON(w, r, &in) || !s.validatePaksetInput(w, r, u, &in) {
		return
	}
	ps, err := s.Repo.CreatePakset(r.Context(), db.Pakset{ID: newPaksetID(), Name: in.Name, OwnerID: u.ID, Public: in.Public, PakIDs: in.Paks})
	if err != nil {
		s.internal(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"pakset": ps})
}

// visiblePakset loads a pakset that is public or owned by the caller.
func (s *server) visiblePakset(w http.ResponseWriter, r *http.Request, id string) (db.Pakset, bool) {
	ps, err := s.Repo.PaksetByID(r.Context(), id)
	if err == nil && (ps.Public || (ps.OwnerID != 0 && ps.OwnerID == userID(r))) {
		return ps, true
	}
	if err != nil && !errors.Is(err, db.ErrNotFound) {
		s.internal(w, r, err)
		return ps, false
	}
	writeError(w, http.StatusNotFound, "not_found", "pakset not found")
	return ps, false
}

// ownedPakset loads a pakset the caller may modify.
func (s *server) ownedPakset(w http.ResponseWriter, r *http.Request) (db.User, db.Pakset, bool) {
	u, ok := requireUser(w, r)
	if !ok {
		return u, db.Pakset{}, false
	}
	ps, ok := s.visiblePakset(w, r, r.PathValue("id"))
	if !ok {
		return u, ps, false
	}
	if !(ps.OwnerID == u.ID || (u.IsAdmin && ps.OwnerID != 0)) {
		writeError(w, http.StatusForbidden, "forbidden", "not your pakset")
		return u, ps, false
	}
	return u, ps, true
}

// GET /api/v1/paksets/{id} → {pakset}.
func (s *server) getPakset(w http.ResponseWriter, r *http.Request) {
	ps, ok := s.visiblePakset(w, r, r.PathValue("id"))
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"pakset": ps})
}

// PUT /api/v1/paksets/{id} {name, paks, public} → {pakset}.
func (s *server) updatePakset(w http.ResponseWriter, r *http.Request) {
	u, ps, ok := s.ownedPakset(w, r)
	if !ok {
		return
	}
	var in paksetInput
	if !decodeJSON(w, r, &in) || !s.validatePaksetInput(w, r, u, &in) {
		return
	}
	ps.Name, ps.PakIDs, ps.Public = in.Name, in.Paks, in.Public
	out, err := s.Repo.UpdatePakset(r.Context(), ps)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	s.Catalog.invalidate()
	writeJSON(w, http.StatusOK, map[string]any{"pakset": out})
}

// DELETE /api/v1/paksets/{id} → 204.
func (s *server) deletePakset(w http.ResponseWriter, r *http.Request) {
	_, ps, ok := s.ownedPakset(w, r)
	if !ok {
		return
	}
	if err := s.Repo.DeletePakset(r.Context(), ps.ID); err != nil {
		s.internal(w, r, err)
		return
	}
	s.Catalog.invalidate()
	w.WriteHeader(http.StatusNoContent)
}

// paksetIndexFor resolves ?pakset= / {id} (default "demo") to its index.
func (s *server) paksetIndexFor(w http.ResponseWriter, r *http.Request, id string) (db.Pakset, *CachedIndex, bool) {
	if id == "" {
		id = DemoPaksetID
	}
	ps, ok := s.visiblePakset(w, r, id)
	if !ok {
		return ps, nil, false
	}
	ci, err := s.Catalog.Index(r.Context(), ps)
	if errors.Is(err, ErrPaksetNotReady) {
		writeError(w, http.StatusConflict, "not_ready", "a pak of this pakset is still being ingested")
		return ps, nil, false
	}
	if err != nil {
		s.internal(w, r, err)
		return ps, nil, false
	}
	return ps, ci, true
}

// GET /api/v1/paksets/{id}/index → the asset index JSON (docs/ASSETS.md),
// with a strong ETag; revalidate with If-None-Match.
func (s *server) paksetIndex(w http.ResponseWriter, r *http.Request) {
	ps, ci, ok := s.paksetIndexFor(w, r, r.PathValue("id"))
	if !ok {
		return
	}
	h := w.Header()
	h.Set("ETag", ci.ETag)
	if ps.Public {
		h.Set("Cache-Control", "public, no-cache")
	} else {
		h.Set("Cache-Control", "private, no-cache")
	}
	h.Set("Content-Type", "application/json; charset=utf-8")
	h.Set("X-Content-Type-Options", "nosniff")
	if match := r.Header.Get("If-None-Match"); match != "" && etagMatch(match, ci.ETag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	h.Set("Content-Length", strconv.Itoa(len(ci.JSON)))
	w.WriteHeader(http.StatusOK)
	if r.Method != http.MethodHead {
		w.Write(ci.JSON) //nolint:errcheck
	}
}

func etagMatch(header, etag string) bool {
	for _, t := range strings.Split(header, ",") {
		t = strings.TrimSpace(t)
		t = strings.TrimPrefix(t, "W/")
		if t == "*" || t == etag {
			return true
		}
	}
	return false
}

// MapSummary is an element of GET /api/v1/maps.
type MapSummary struct {
	Name     string `json:"name"`
	Path     string `json:"path"`
	SHA256   string `json:"sha256"`
	Checksum uint32 `json:"checksum"`
	Message  string `json:"message"`
	Sky      string `json:"sky"`
	Pak      int64  `json:"pakId"`
}

// GET /api/v1/maps?pakset=demo → {pakset, maps:[...]}.
func (s *server) listMaps(w http.ResponseWriter, r *http.Request) {
	ps, ci, ok := s.paksetIndexFor(w, r, r.URL.Query().Get("pakset"))
	if !ok {
		return
	}
	out := make([]MapSummary, 0, len(ci.Index.Maps))
	for _, m := range ci.Index.Maps {
		e := ci.Index.Lookup(m.Path)
		var pakID int64
		if e != nil {
			pakID = ci.Index.Paks[e.Pak].ID
		}
		out = append(out, MapSummary{Name: m.Name, Path: m.Path, SHA256: m.SHA256, Checksum: m.Checksum,
			Message: m.Message, Sky: m.Sky, Pak: pakID})
	}
	writeJSON(w, http.StatusOK, map[string]any{"pakset": ps.ID, "maps": out})
}

// MapManifest is GET /api/v1/maps/{name}/manifest: the map summary plus the
// resolved index entries of everything the map references.
type MapManifest struct {
	Pakset  string                     `json:"pakset"`
	Map     manifest.MapInfo           `json:"map"`
	Palette *manifest.Palette          `json:"palette,omitempty"`
	Files   map[string]*manifest.Entry `json:"files"`
	Missing []string                   `json:"missing"`
}

// GET /api/v1/maps/{name}/manifest?pakset=demo
func (s *server) mapManifest(w http.ResponseWriter, r *http.Request) {
	ps, ci, ok := s.paksetIndexFor(w, r, r.URL.Query().Get("pakset"))
	if !ok {
		return
	}
	name := r.PathValue("name")
	e := ci.Index.Lookup("maps/" + name + ".bsp")
	if e == nil || e.Map == nil {
		writeError(w, http.StatusNotFound, "not_found", "map not found in pakset")
		return
	}
	mm := MapManifest{Pakset: ps.ID, Map: *e.Map, Palette: ci.Index.Palette, Files: map[string]*manifest.Entry{}, Missing: []string{}}
	add := func(p string) {
		k := manifest.Lower(p)
		if _, ok := mm.Files[k]; ok {
			return
		}
		if x := ci.Index.Lookup(p); x != nil {
			mm.Files[k] = x
		} else {
			mm.Missing = append(mm.Missing, p)
		}
	}
	add(e.Path)
	for _, t := range e.Map.Textures {
		add(t)
		// follow WAL animation chains
		for i, cur := 0, ci.Index.Lookup(t); cur != nil && cur.WAL != nil && cur.WAL.AnimNext != "" && i < 64; i++ {
			next := cur.WAL.AnimNext
			if _, ok := mm.Files[manifest.Lower(next)]; ok {
				break
			}
			add(next)
			cur = ci.Index.Lookup(next)
		}
	}
	for _, p := range e.Map.SkyImages {
		add(p)
	}
	for _, p := range e.Map.Models {
		add(p)
	}
	for _, p := range e.Map.Sounds {
		add(p)
	}
	writeJSON(w, http.StatusOK, mm)
}

// GET|HEAD /assets/{sha256}: content-addressed blob with Range support.
// Entitlement (ADR-0005): blobs of public paks are served to everyone
// (Cache-Control public); others only to owners of a pak containing the
// same hash (Cache-Control private).
func (s *server) asset(w http.ResponseWriter, r *http.Request) {
	sha := r.PathValue("sha256")
	if !blob.ValidHash(sha) {
		writeError(w, http.StatusNotFound, "not_found", "asset not found")
		return
	}
	b, allowed, err := s.Repo.AssetAccess(r.Context(), sha, userID(r))
	if errors.Is(err, db.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "asset not found")
		return
	}
	if err != nil {
		s.internal(w, r, err)
		return
	}
	if !allowed {
		if userID(r) == 0 {
			writeError(w, http.StatusUnauthorized, "unauthenticated", "login required for this asset")
		} else {
			writeError(w, http.StatusForbidden, "forbidden", "you do not own a pak containing this asset")
		}
		return
	}
	f, info, err := s.Store.Open(r.Context(), sha)
	if errors.Is(err, blob.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "asset blob missing")
		return
	}
	if err != nil {
		s.internal(w, r, err)
		return
	}
	defer f.Close()
	h := w.Header()
	h.Set("ETag", `"`+sha+`"`)
	if userID(r) == 0 || s.isPublicAsset(r, sha) {
		h.Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		h.Set("Cache-Control", "private, max-age=31536000, immutable")
		h.Add("Vary", "Cookie")
	}
	ctype := b.ContentType
	if ctype == "" {
		ctype = "application/octet-stream"
	}
	h.Set("Content-Type", ctype)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "default-src 'none'; sandbox")
	http.ServeContent(w, r, "", info.ModTime, f)
}

func (s *server) isPublicAsset(r *http.Request, sha string) bool {
	_, ok, err := s.Repo.AssetAccess(r.Context(), sha, 0)
	return err == nil && ok
}
