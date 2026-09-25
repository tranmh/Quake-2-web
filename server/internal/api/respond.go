package api

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
)

// ErrorBody is the JSON error envelope: {"error":{"code":"...","message":"..."}}.
type ErrorBody struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Field   string `json:"field,omitempty"`
	} `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if w.Header().Get("Cache-Control") == "" {
		w.Header().Set("Cache-Control", "no-store")
	}
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.Encode(v) //nolint:errcheck
}

func writeError(w http.ResponseWriter, status int, code, msg string) {
	var b ErrorBody
	b.Error.Code, b.Error.Message = code, msg
	writeJSON(w, status, b)
}

func writeFieldError(w http.ResponseWriter, field, msg string) {
	var b ErrorBody
	b.Error.Code, b.Error.Message, b.Error.Field = "invalid", msg, field
	writeJSON(w, http.StatusBadRequest, b)
}

const maxJSONBody = 1 << 20

// decodeJSON reads a JSON request body (Content-Type must be
// application/json — part of the CSRF defence) into v. It writes the error
// response itself and returns false on failure.
func decodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	ct, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || ct != "application/json" {
		writeError(w, http.StatusUnsupportedMediaType, "unsupported_media_type", "Content-Type must be application/json")
		return false
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxJSONBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			writeError(w, http.StatusRequestEntityTooLarge, "too_large", "request body too large")
			return false
		}
		writeError(w, http.StatusBadRequest, "bad_json", "malformed JSON: "+strings.TrimPrefix(err.Error(), "json: "))
		return false
	}
	if dec.More() {
		writeError(w, http.StatusBadRequest, "bad_json", "trailing data after JSON value")
		return false
	}
	io.Copy(io.Discard, r.Body) //nolint:errcheck
	return true
}
