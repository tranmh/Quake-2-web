package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"quake2web/server/internal/db"
)

var slotRE = regexp.MustCompile(`^[A-Za-z0-9_-]{1,32}$`)

// ValidSlot reports whether a save slot name is acceptable.
func ValidSlot(s string) bool { return slotRE.MatchString(s) }

// GET /api/v1/saves → {saves:[SaveInfo...]}.
func (s *server) listSaves(w http.ResponseWriter, r *http.Request) {
	u, ok := requireUser(w, r)
	if !ok {
		return
	}
	list, err := s.Repo.ListSaves(r.Context(), u.ID)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"saves": list})
}

func (s *server) loadSave(w http.ResponseWriter, r *http.Request) ([]byte, db.SaveInfo, bool) {
	u, ok := requireUser(w, r)
	if !ok {
		return nil, db.SaveInfo{}, false
	}
	slot := r.PathValue("slot")
	if !ValidSlot(slot) {
		writeError(w, http.StatusNotFound, "not_found", "save not found")
		return nil, db.SaveInfo{}, false
	}
	blob, info, err := s.Repo.GetSave(r.Context(), u.ID, slot)
	if errors.Is(err, db.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "save not found")
		return nil, info, false
	}
	if err != nil {
		s.internal(w, r, err)
		return nil, info, false
	}
	return blob, info, true
}

// GET /api/v1/saves/{slot} → {save:SaveInfo}.
func (s *server) getSave(w http.ResponseWriter, r *http.Request) {
	_, info, ok := s.loadSave(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"save": info})
}

// GET /api/v1/saves/{slot}/data → the raw save blob.
func (s *server) getSaveData(w http.ResponseWriter, r *http.Request) {
	blob, info, ok := s.loadSave(w, r)
	if !ok {
		return
	}
	h := w.Header()
	h.Set("Content-Type", "application/octet-stream")
	h.Set("Content-Length", strconv.Itoa(len(blob)))
	h.Set("Cache-Control", "no-store")
	h.Set("Content-Disposition", `attachment; filename="`+info.Slot+`.sav"`)
	w.WriteHeader(http.StatusOK)
	w.Write(blob) //nolint:errcheck
}

// DELETE /api/v1/saves/{slot} → 204.
func (s *server) deleteSave(w http.ResponseWriter, r *http.Request) {
	u, ok := requireUser(w, r)
	if !ok {
		return
	}
	slot := r.PathValue("slot")
	err := db.ErrNotFound
	if ValidSlot(slot) {
		err = s.Repo.DeleteSave(r.Context(), u.ID, slot)
	}
	if errors.Is(err, db.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "save not found")
		return
	}
	if err != nil {
		s.internal(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// MaxConfigBytes caps the stored config.cfg text.
const MaxConfigBytes = 64 << 10

type settingsBody struct {
	Config    string     `json:"config"`
	UpdatedAt *time.Time `json:"updatedAt,omitempty"`
}

// GET /api/v1/settings → {config, updatedAt} (config "" when never saved).
func (s *server) getSettings(w http.ResponseWriter, r *http.Request) {
	u, ok := requireUser(w, r)
	if !ok {
		return
	}
	cfg, at, err := s.Repo.GetSettings(r.Context(), u.ID)
	if errors.Is(err, db.ErrNotFound) {
		writeJSON(w, http.StatusOK, settingsBody{})
		return
	}
	if err != nil {
		s.internal(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, settingsBody{Config: cfg, UpdatedAt: &at})
}

// PUT /api/v1/settings {config} → {config, updatedAt}.
func (s *server) putSettings(w http.ResponseWriter, r *http.Request) {
	u, ok := requireUser(w, r)
	if !ok {
		return
	}
	var in settingsBody
	if !decodeJSON(w, r, &in) {
		return
	}
	if len(in.Config) > MaxConfigBytes {
		writeFieldError(w, "config", "config exceeds 64 KiB")
		return
	}
	if strings.IndexByte(in.Config, 0) >= 0 {
		writeFieldError(w, "config", "config must not contain NUL bytes")
		return
	}
	if err := s.Repo.PutSettings(r.Context(), u.ID, in.Config); err != nil {
		s.internal(w, r, err)
		return
	}
	s.getSettings(w, r)
}

// ---- games ----

var gameModes = map[string]bool{"sp": true, "coop": true, "dm": true, "ctf": true}
var mapNameRE = regexp.MustCompile(`^[A-Za-z0-9_\-]{1,58}$`)
var cvarNameRE = regexp.MustCompile(`^[A-Za-z0-9_]{1,32}$`)

func (s *server) requireHost(w http.ResponseWriter) bool {
	if s.Host == nil {
		writeError(w, http.StatusServiceUnavailable, "no_game_host", "game host unavailable")
		return false
	}
	return true
}

func (s *server) hostError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrGameNotFound):
		writeError(w, http.StatusNotFound, "not_found", "game not found")
	case errors.Is(err, ErrGameFull):
		writeError(w, http.StatusConflict, "game_full", "game is full")
	case errors.Is(err, ErrGameLimit):
		writeError(w, http.StatusTooManyRequests, "too_many_games", err.Error())
	case errors.Is(err, ErrGameInvalid):
		writeError(w, http.StatusBadRequest, "invalid_game", err.Error())
	default:
		s.internal(w, r, err)
	}
}

// POST /api/v1/games GameSpec → 201 {game:GameInfo}.
func (s *server) createGame(w http.ResponseWriter, r *http.Request) {
	u, ok := requireUser(w, r)
	if !ok || !s.requireHost(w) {
		return
	}
	var spec GameSpec
	if !decodeJSON(w, r, &spec) {
		return
	}
	s.startGame(w, r, u, spec)
}

// rejectBanned writes 403 and returns true when the user (or the request's
// IP) is banned. Sessions created before a ban stay valid, so actions that
// consume server resources check it themselves.
func (s *server) rejectBanned(w http.ResponseWriter, r *http.Request, u db.User) bool {
	ban, err := s.Repo.ActiveBan(r.Context(), u.ID, s.clientIP(r), time.Now())
	if err != nil {
		s.internal(w, r, err)
		return true
	}
	if ban != nil {
		writeError(w, http.StatusForbidden, "banned", "account banned")
		return true
	}
	return false
}

// startGame validates spec, starts it on the host and registers it.
func (s *server) startGame(w http.ResponseWriter, r *http.Request, u db.User, spec GameSpec) {
	if s.rejectBanned(w, r, u) {
		return
	}
	spec.OwnerID = u.ID
	if spec.Mode == "" {
		spec.Mode = "sp"
	}
	if !gameModes[spec.Mode] {
		writeFieldError(w, "mode", "mode must be sp, coop, dm or ctf")
		return
	}
	if !mapNameRE.MatchString(spec.Map) {
		writeFieldError(w, "map", "invalid map name")
		return
	}
	spec.Name = strings.TrimSpace(spec.Name)
	if utf8.RuneCountInString(spec.Name) > 64 {
		writeFieldError(w, "name", "name must be at most 64 characters")
		return
	}
	if spec.Name == "" {
		spec.Name = u.DisplayName + "'s game"
	}
	if spec.MaxPlayers < 0 || spec.MaxPlayers > 64 {
		writeFieldError(w, "maxPlayers", "maxPlayers must be 0 to 64")
		return
	}
	if len(spec.Cvars) > 64 {
		writeFieldError(w, "cvars", "too many cvars")
		return
	}
	for k, v := range spec.Cvars {
		if !cvarNameRE.MatchString(k) || len(v) > 256 || strings.ContainsAny(v, "\"\n\r;\x00") {
			writeFieldError(w, "cvars", "invalid cvar "+k)
			return
		}
	}
	if spec.LoadSlot != "" {
		if !ValidSlot(spec.LoadSlot) {
			writeFieldError(w, "loadSlot", "invalid save slot")
			return
		}
		if spec.Mode != "sp" && spec.Mode != "coop" {
			writeFieldError(w, "loadSlot", "only single player and coop games can start from a save")
			return
		}
		if _, _, err := s.Repo.GetSave(r.Context(), u.ID, spec.LoadSlot); errors.Is(err, db.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "save not found")
			return
		} else if err != nil {
			s.internal(w, r, err)
			return
		}
	}
	// the pakset must exist, be visible and contain the map
	_, ci, ok := s.paksetIndexFor(w, r, spec.Pakset)
	if !ok {
		return
	}
	if spec.Pakset == "" {
		spec.Pakset = DemoPaksetID
	}
	if e := ci.Index.Lookup("maps/" + spec.Map + ".bsp"); e == nil {
		writeFieldError(w, "map", "map not in pakset")
		return
	}
	id, err := s.Host.Create(r.Context(), spec)
	if err != nil {
		s.hostError(w, r, err)
		return
	}
	settings, _ := json.Marshal(spec)
	if err := s.Repo.InsertGame(r.Context(), db.Game{ID: id, OwnerID: u.ID, Mode: spec.Mode, Map: spec.Map,
		PaksetID: spec.Pakset, Settings: settings, Public: spec.Public}); err != nil {
		s.Log.Warn("games registry insert failed", "game", id, "err", err)
	}
	info, err := s.Host.Get(r.Context(), id)
	if err != nil {
		info = GameInfo{ID: id, Name: spec.Name, OwnerID: u.ID, Mode: spec.Mode, Map: spec.Map, Pakset: spec.Pakset,
			Public: spec.Public, MaxPlayers: spec.MaxPlayers, StartedAt: time.Now()}
	}
	writeJSON(w, http.StatusCreated, map[string]any{"game": info})
}

// SaveOriginFile is the file of a save bundle recording the pakset and
// mode of the game that wrote it (written by internal/host).
const SaveOriginFile = "q2web.json"

// FromSaveRequest is POST /api/v1/games/from-save.
type FromSaveRequest struct {
	Slot   string `json:"slot"`
	Name   string `json:"name,omitempty"`
	Public bool   `json:"public,omitempty"`
	// MaxPlayers applies to coop saves.
	MaxPlayers int `json:"maxPlayers,omitempty"`
	// Pakset overrides the pakset recorded in the save.
	Pakset string `json:"pakset,omitempty"`
}

// MapFromMapCmd extracts the map name of a server mapcmd
// ("*base2$spawn", "intro.cin+base1") or "" when there is none.
func MapFromMapCmd(mc string) string {
	if i := strings.LastIndexByte(mc, '+'); i >= 0 {
		mc = mc[i+1:]
	}
	mc = strings.TrimPrefix(mc, "*")
	if i := strings.IndexByte(mc, '$'); i >= 0 {
		mc = mc[:i]
	}
	if strings.Contains(mc, ".") {
		return ""
	}
	return mc
}

// POST /api/v1/games/from-save {slot} → 201 {game:GameInfo}: starts a
// single player (or coop) game of the caller from one of their saves.
func (s *server) createGameFromSave(w http.ResponseWriter, r *http.Request) {
	u, ok := requireUser(w, r)
	if !ok || !s.requireHost(w) {
		return
	}
	var in FromSaveRequest
	if !decodeJSON(w, r, &in) {
		return
	}
	if !ValidSlot(in.Slot) {
		writeFieldError(w, "slot", "invalid save slot")
		return
	}
	blob, info, err := s.Repo.GetSave(r.Context(), u.ID, in.Slot)
	if errors.Is(err, db.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "save not found")
		return
	}
	if err != nil {
		s.internal(w, r, err)
		return
	}
	spec := GameSpec{Name: in.Name, Mode: info.Mode, Map: MapFromMapCmd(info.MapCmd), Pakset: in.Pakset,
		Public: in.Public, MaxPlayers: in.MaxPlayers, LoadSlot: in.Slot}
	if files, err := db.DecodeBundle(blob); err == nil {
		var origin struct {
			Pakset string `json:"pakset"`
			Mode   string `json:"mode"`
		}
		if b, ok := files[SaveOriginFile]; ok && json.Unmarshal(b, &origin) == nil {
			if spec.Pakset == "" {
				spec.Pakset = origin.Pakset
			}
			if origin.Mode != "" {
				spec.Mode = origin.Mode
			}
		}
		if _, ok := files["server.ssv"]; !ok {
			writeError(w, http.StatusConflict, "bad_save", "save is incomplete")
			return
		}
	}
	if spec.Mode == "" {
		spec.Mode = "sp"
	}
	if spec.Map == "" {
		writeError(w, http.StatusConflict, "bad_save", "save has no map")
		return
	}
	s.startGame(w, r, u, spec)
}

func visibleGame(g GameInfo, uid int64, admin bool) bool {
	return g.Public || (uid != 0 && g.OwnerID == uid) || admin
}

// GET /api/v1/games → {games:[...]} (public + own).
func (s *server) listGames(w http.ResponseWriter, r *http.Request) {
	if !s.requireHost(w) {
		return
	}
	list, err := s.Host.List(r.Context())
	if err != nil {
		s.internal(w, r, err)
		return
	}
	u, _ := currentUser(r)
	out := []GameInfo{}
	for _, g := range list {
		if visibleGame(g, u.ID, u.IsAdmin) {
			out = append(out, g)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"games": out})
}

func (s *server) loadGame(w http.ResponseWriter, r *http.Request) (GameInfo, bool) {
	if !s.requireHost(w) {
		return GameInfo{}, false
	}
	id := r.PathValue("id")
	g, err := s.Host.Get(r.Context(), id)
	if err != nil {
		s.hostError(w, r, err)
		return g, false
	}
	u, _ := currentUser(r)
	if !visibleGame(g, u.ID, u.IsAdmin) {
		writeError(w, http.StatusNotFound, "not_found", "game not found")
		return g, false
	}
	return g, true
}

// GET /api/v1/games/{id} → {game}.
func (s *server) getGame(w http.ResponseWriter, r *http.Request) {
	g, ok := s.loadGame(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"game": g})
}

// JoinResponse is POST /api/v1/games/{id}/join.
type JoinResponse struct {
	Ticket string `json:"ticket"`
	WSURL  string `json:"wsUrl"`
}

// POST /api/v1/games/{id}/join → {ticket, wsUrl}.
func (s *server) joinGame(w http.ResponseWriter, r *http.Request) {
	u, ok := requireUser(w, r)
	if !ok {
		return
	}
	g, ok := s.loadGame(w, r)
	if !ok {
		return
	}
	if ban, err := s.Repo.ActiveBan(r.Context(), u.ID, s.clientIP(r), time.Now()); err != nil {
		s.internal(w, r, err)
		return
	} else if ban != nil {
		writeError(w, http.StatusForbidden, "banned", "account banned")
		return
	}
	ticket, wsURL, err := s.Host.Join(r.Context(), g.ID, Player{UserID: u.ID, DisplayName: u.DisplayName})
	if err != nil {
		s.hostError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, JoinResponse{Ticket: ticket, WSURL: wsURL})
}

// DELETE /api/v1/games/{id} → 204 (owner or admin).
func (s *server) deleteGame(w http.ResponseWriter, r *http.Request) {
	u, ok := requireUser(w, r)
	if !ok {
		return
	}
	g, ok := s.loadGame(w, r)
	if !ok {
		return
	}
	if g.OwnerID != u.ID && !u.IsAdmin {
		writeError(w, http.StatusForbidden, "forbidden", "not your game")
		return
	}
	if err := s.Host.Stop(r.Context(), g.ID); err != nil {
		s.hostError(w, r, err)
		return
	}
	if err := s.Repo.EndGame(r.Context(), g.ID, time.Now()); err != nil && !errors.Is(err, db.ErrNotFound) {
		s.Log.Warn("games registry end failed", "game", g.ID, "err", err)
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- health ----

// GET /healthz → 200 "ok" (process is up).
func (s *server) healthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Write([]byte("ok\n")) //nolint:errcheck
}

// GET /readyz → 200 {"db":"ok","blobs":"ok"} or 503 with the failing check.
func (s *server) readyz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	out := map[string]string{"db": "ok", "blobs": "ok"}
	status := http.StatusOK
	// details go to the log only: this endpoint is public and errors name
	// internal hosts, users and paths
	if err := s.Repo.Ping(ctx); err != nil {
		s.Log.Error("readyz: database", "err", err)
		out["db"] = "error"
		status = http.StatusServiceUnavailable
	}
	if err := s.Store.Check(ctx); err != nil {
		s.Log.Error("readyz: blob store", "err", err)
		out["blobs"] = "error"
		status = http.StatusServiceUnavailable
	}
	writeJSON(w, status, out)
}
