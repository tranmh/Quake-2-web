package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"

	"quake2web/server/internal/db"
)

// Errors returned by Service.
var (
	ErrInvalidCredentials = errors.New("auth: invalid email or password")
	ErrEmailTaken         = errors.New("auth: email already registered")
	ErrUnauthenticated    = errors.New("auth: not logged in")
	ErrBanned             = errors.New("auth: account banned")
)

// ValidationError describes a rejected registration field.
type ValidationError struct {
	Field  string
	Reason string
}

func (e *ValidationError) Error() string { return e.Field + ": " + e.Reason }

// Service implements register/login/logout/session lookup.
type Service struct {
	Repo   db.Repo
	TTL    time.Duration
	Params Params
	Now    func() time.Time
	// dummy is a hash verified for unknown emails so that login timing does
	// not reveal whether an account exists.
	dummy string
}

// NewService returns a Service with default argon2 parameters.
func NewService(repo db.Repo, ttl time.Duration) *Service {
	return NewServiceWithParams(repo, ttl, DefaultParams)
}

// NewServiceWithParams allows cheaper parameters (tests).
func NewServiceWithParams(repo db.Repo, ttl time.Duration, p Params) *Service {
	dummy, _ := HashPassword("not a password", p)
	return &Service{Repo: repo, TTL: ttl, Params: p, Now: time.Now, dummy: dummy}
}

// TokenHash is the session id stored for a cookie token.
func TokenHash(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

// NewToken returns 32 random bytes, base64url encoded.
func NewToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// NormalizeEmail trims and validates an address.
func NormalizeEmail(email string) (string, error) {
	email = strings.TrimSpace(email)
	a, err := mail.ParseAddress(email)
	if err != nil || a.Address != email || len(email) > 254 {
		return "", &ValidationError{"email", "invalid address"}
	}
	return email, nil
}

// Register creates an account.
func (s *Service) Register(ctx context.Context, email, password, displayName string) (db.User, error) {
	email, err := NormalizeEmail(email)
	if err != nil {
		return db.User{}, err
	}
	if n := utf8.RuneCountInString(password); n < 8 || len(password) > 256 {
		return db.User{}, &ValidationError{"password", "must be 8 to 256 characters"}
	}
	displayName = strings.TrimSpace(displayName)
	if displayName == "" {
		displayName = strings.SplitN(email, "@", 2)[0]
	}
	if utf8.RuneCountInString(displayName) > 32 || !utf8.ValidString(displayName) {
		return db.User{}, &ValidationError{"displayName", "must be at most 32 characters"}
	}
	for _, r := range displayName {
		if r < 0x20 || r == 0x7f {
			return db.User{}, &ValidationError{"displayName", "must not contain control characters"}
		}
	}
	hash, err := HashPassword(password, s.Params)
	if err != nil {
		return db.User{}, err
	}
	u, err := s.Repo.CreateUser(ctx, db.User{Email: email, DisplayName: displayName, PasswordHash: hash})
	if errors.Is(err, db.ErrConflict) {
		return db.User{}, ErrEmailTaken
	}
	return u, err
}

// Login verifies credentials and creates a session; the returned token is
// the cookie value.
func (s *Service) Login(ctx context.Context, email, password, userAgent, ip string) (string, db.Session, db.User, error) {
	u, err := s.Repo.UserByEmail(ctx, strings.TrimSpace(email))
	if errors.Is(err, db.ErrNotFound) {
		VerifyPassword(s.dummy, password) //nolint:errcheck // timing equalization
		return "", db.Session{}, db.User{}, ErrInvalidCredentials
	}
	if err != nil {
		return "", db.Session{}, db.User{}, err
	}
	ok, err := VerifyPassword(u.PasswordHash, password)
	if err != nil {
		return "", db.Session{}, db.User{}, fmt.Errorf("auth: user %d: %w", u.ID, err)
	}
	if !ok {
		return "", db.Session{}, db.User{}, ErrInvalidCredentials
	}
	now := s.Now()
	if ban, err := s.Repo.ActiveBan(ctx, u.ID, ip, now); err != nil {
		return "", db.Session{}, db.User{}, err
	} else if ban != nil {
		return "", db.Session{}, db.User{}, ErrBanned
	}
	token, err := NewToken()
	if err != nil {
		return "", db.Session{}, db.User{}, err
	}
	if len(userAgent) > 512 {
		userAgent = userAgent[:512]
	}
	sess := db.Session{ID: TokenHash(token), UserID: u.ID, CreatedAt: now, ExpiresAt: now.Add(s.TTL), UserAgent: userAgent, IP: ip}
	if err := s.Repo.CreateSession(ctx, sess); err != nil {
		return "", db.Session{}, db.User{}, err
	}
	return token, sess, u, nil
}

// Authenticate resolves a cookie token to its user.
func (s *Service) Authenticate(ctx context.Context, token string) (db.Session, db.User, error) {
	if token == "" || len(token) > 128 {
		return db.Session{}, db.User{}, ErrUnauthenticated
	}
	sess, u, err := s.Repo.SessionUser(ctx, TokenHash(token), s.Now())
	if errors.Is(err, db.ErrNotFound) {
		return db.Session{}, db.User{}, ErrUnauthenticated
	}
	return sess, u, err
}

// Logout deletes the session of a token.
func (s *Service) Logout(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}
	return s.Repo.DeleteSession(ctx, TokenHash(token))
}
