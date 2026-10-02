package api

import (
	"errors"
	"net/http"
	"path"
	"strconv"
	"strings"
)

// ---- bots (docs/plans/0002-ai-agent.md H.5) ----

// requireBots writes 503 and returns false when the server runs no bots
// (Q2_BOTS_ENABLED off).
func (s *server) requireBots(w http.ResponseWriter) bool {
	if s.Bots == nil {
		writeError(w, http.StatusServiceUnavailable, "bots_disabled", "bots are not enabled on this server")
		return false
	}
	return true
}

// botUser is the caller as BotHost sees it (ID 0: anonymous).
func botUser(r *http.Request) BotUser {
	u, ok := currentUser(r)
	if !ok {
		return BotUser{}
	}
	return BotUser{ID: u.ID, Name: u.DisplayName, Admin: u.IsAdmin}
}

func (s *server) botError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrBotNotFound):
		writeError(w, http.StatusNotFound, "not_found", "bot not found")
	case errors.Is(err, ErrBotForbidden):
		writeError(w, http.StatusForbidden, "forbidden", err.Error())
	case errors.Is(err, ErrBotLimit):
		writeError(w, http.StatusTooManyRequests, "too_many_bots", err.Error())
	case errors.Is(err, ErrBotsDisabled):
		writeError(w, http.StatusServiceUnavailable, "bots_unavailable", err.Error())
	case errors.Is(err, ErrBotInvalid):
		writeError(w, http.StatusBadRequest, "invalid_bot", err.Error())
	case errors.Is(err, ErrBotNotLive):
		writeError(w, http.StatusConflict, "not_live", err.Error())
	default:
		s.internal(w, r, err)
	}
}

// POST /api/v1/bots BotSpec → 201 {bot:BotInfo}.
func (s *server) createBot(w http.ResponseWriter, r *http.Request) {
	u, ok := requireUser(w, r)
	if !ok || !s.requireBots(w) || s.rejectBanned(w, r, u) {
		return
	}
	var spec BotSpec
	if !decodeJSON(w, r, &spec) {
		return
	}
	info, err := s.Bots.Start(r.Context(), spec, botUser(r))
	if err != nil {
		s.botError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"bot": info})
}

// GET /api/v1/bots → {bots:[...]} (public, own, or all for an admin).
func (s *server) listBots(w http.ResponseWriter, r *http.Request) {
	if !s.requireBots(w) {
		return
	}
	list, err := s.Bots.List(r.Context(), botUser(r))
	if err != nil {
		s.botError(w, r, err)
		return
	}
	if list == nil {
		list = []BotInfo{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"bots": list})
}

// GET /api/v1/bots/{id} → {bot}.
func (s *server) getBot(w http.ResponseWriter, r *http.Request) {
	if !s.requireBots(w) {
		return
	}
	info, err := s.Bots.Get(r.Context(), r.PathValue("id"), botUser(r))
	if err != nil {
		s.botError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"bot": info})
}

// DELETE /api/v1/bots/{id} → 204 (owner or admin): stops a live bot.
func (s *server) deleteBot(w http.ResponseWriter, r *http.Request) {
	if _, ok := requireUser(w, r); !ok || !s.requireBots(w) {
		return
	}
	if err := s.Bots.Stop(r.Context(), r.PathValue("id"), botUser(r)); err != nil {
		s.botError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// POST /api/v1/bots/{id}/watch → BotWatch (409 when the bot is not live).
// Anyone may watch a public bot, so the tickets are rate-limited per
// account, or per IP for anonymous callers (429), and refused to banned
// accounts and addresses (403).
func (s *server) watchBot(w http.ResponseWriter, r *http.Request) {
	if !s.requireBots(w) {
		return
	}
	u, _ := currentUser(r) // the zero user when anonymous: a ban of the address only
	key := "bot-watch:ip:" + s.clientIP(r)
	if u.ID != 0 {
		key = "bot-watch:user:" + strconv.FormatInt(u.ID, 10)
	}
	if ok, retry := s.BotWatchLimiter.Allow(key); !ok {
		tooMany(w, retry)
		return
	}
	if s.rejectBanned(w, r, u) {
		return
	}
	bw, err := s.Bots.Watch(r.Context(), r.PathValue("id"), botUser(r))
	if err != nil {
		s.botError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, bw)
}

// GET /api/v1/bots/{id}/artifacts/{name...} → the file (allowlisted
// names; a trace is sent gzip-encoded as stored).
func (s *server) botArtifact(w http.ResponseWriter, r *http.Request) {
	if !s.requireBots(w) {
		return
	}
	name := r.PathValue("name")
	a, err := s.Bots.OpenArtifact(r.Context(), r.PathValue("id"), name, botUser(r))
	if err != nil {
		s.botError(w, r, err)
		return
	}
	defer a.Content.Close()
	h := w.Header()
	h.Set("Content-Type", a.ContentType)
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Cache-Control", "private, no-cache")
	if a.ContentEncoding != "" {
		h.Set("Content-Encoding", a.ContentEncoding)
	}
	switch a.Kind {
	case ArtifactDemo, ArtifactTrace:
		file := path.Base(a.Name)
		if a.ContentEncoding == "gzip" {
			// the browser saves the decoded body
			file = strings.TrimSuffix(file, ".gz")
		}
		h.Set("Content-Disposition", `attachment; filename="`+file+`"`)
	}
	http.ServeContent(w, r, "", a.ModTime, a.Content)
}
