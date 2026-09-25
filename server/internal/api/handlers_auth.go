package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"quake2web/server/internal/auth"
	"quake2web/server/internal/db"
)

type credentials struct {
	Email       string `json:"email"`
	Password    string `json:"password"`
	DisplayName string `json:"displayName,omitempty"`
}

type meResponse struct {
	User db.User `json:"user"`
}

func (s *server) setSessionCookie(w http.ResponseWriter, token string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookie,
		Value:    token,
		Path:     "/",
		Expires:  expires,
		MaxAge:   int(time.Until(expires).Seconds()),
		HttpOnly: true,
		Secure:   s.Config.CookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
}

func (s *server) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name: SessionCookie, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: s.Config.CookieSecure, SameSite: http.SameSiteLaxMode,
	})
}

// POST /api/v1/auth/register {email,password,displayName} → 201 {user}
// (and logs the new account in).
func (s *server) register(w http.ResponseWriter, r *http.Request) {
	var in credentials
	if !decodeJSON(w, r, &in) {
		return
	}
	ip := s.clientIP(r)
	if ok, retry := s.LoginLimiterIP.Allow("register:" + ip); !ok {
		tooMany(w, retry)
		return
	}
	u, err := s.Auth.Register(r.Context(), in.Email, in.Password, in.DisplayName)
	var ve *auth.ValidationError
	switch {
	case errors.As(err, &ve):
		writeFieldError(w, ve.Field, ve.Error())
		return
	case errors.Is(err, auth.ErrEmailTaken):
		writeError(w, http.StatusConflict, "email_taken", "email already registered")
		return
	case err != nil:
		s.internal(w, r, err)
		return
	}
	token, sess, _, err := s.Auth.Login(r.Context(), in.Email, in.Password, r.UserAgent(), ip)
	if err != nil {
		s.internal(w, r, err)
		return
	}
	s.setSessionCookie(w, token, sess.ExpiresAt)
	writeJSON(w, http.StatusCreated, meResponse{User: u})
}

func tooMany(w http.ResponseWriter, retry time.Duration) {
	secs := int(retry.Seconds() + 0.999)
	if secs < 1 {
		secs = 1
	}
	w.Header().Set("Retry-After", strconv.Itoa(secs))
	writeError(w, http.StatusTooManyRequests, "rate_limited", "too many attempts, try again later")
}

// POST /api/v1/auth/login {email,password} → 200 {user} + session cookie.
func (s *server) login(w http.ResponseWriter, r *http.Request) {
	var in credentials
	if !decodeJSON(w, r, &in) {
		return
	}
	ip := s.clientIP(r)
	emailKey := "login:" + strings.ToLower(strings.TrimSpace(in.Email))
	if ok, retry := s.LoginLimiterIP.Allow("login:" + ip); !ok {
		tooMany(w, retry)
		return
	}
	if ok, retry := s.LoginLimiterEmail.Allow(emailKey); !ok {
		tooMany(w, retry)
		return
	}
	token, sess, u, err := s.Auth.Login(r.Context(), in.Email, in.Password, r.UserAgent(), ip)
	switch {
	case errors.Is(err, auth.ErrInvalidCredentials):
		writeError(w, http.StatusUnauthorized, "invalid_credentials", "invalid email or password")
		return
	case errors.Is(err, auth.ErrBanned):
		writeError(w, http.StatusForbidden, "banned", "account banned")
		return
	case err != nil:
		s.internal(w, r, err)
		return
	}
	s.LoginLimiterEmail.Reset(emailKey)
	s.setSessionCookie(w, token, sess.ExpiresAt)
	writeJSON(w, http.StatusOK, meResponse{User: u})
}

// POST /api/v1/auth/logout → 204.
func (s *server) logout(w http.ResponseWriter, r *http.Request) {
	if uc, ok := r.Context().Value(ctxUser).(*userCtx); ok {
		if err := s.Auth.Logout(r.Context(), uc.token); err != nil {
			s.internal(w, r, err)
			return
		}
	}
	s.clearSessionCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

// GET /api/v1/auth/me → 200 {user} or 401.
func (s *server) me(w http.ResponseWriter, r *http.Request) {
	u, ok := requireUser(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, meResponse{User: u})
}

func (s *server) internal(w http.ResponseWriter, r *http.Request, err error) {
	s.Log.Error("internal error", "req_id", RequestID(r.Context()), "path", r.URL.Path, "err", err)
	writeError(w, http.StatusInternalServerError, "internal", "internal server error")
}
