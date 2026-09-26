package auth

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"quake2web/server/internal/db"
)

var fast = Params{Memory: 64, Time: 1, Threads: 1, SaltLen: 16, KeyLen: 32}

func TestPasswordHash(t *testing.T) {
	h, err := HashPassword("correct horse", fast)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(h, "$argon2id$v=19$m=64,t=1,p=1$") {
		t.Fatal(h)
	}
	if ok, err := VerifyPassword(h, "correct horse"); !ok || err != nil {
		t.Fatal("verify failed", err)
	}
	if ok, _ := VerifyPassword(h, "wrong"); ok {
		t.Fatal("wrong password accepted")
	}
	h2, _ := HashPassword("correct horse", fast)
	if h == h2 {
		t.Fatal("salt not random")
	}
	for _, bad := range []string{"", "$argon2i$v=19$m=64,t=1,p=1$AA$AA", "$argon2id$v=19$m=0,t=1,p=1$AA$AA", "$argon2id$v=19$m=64,t=1,p=1$!$AA"} {
		if _, err := VerifyPassword(bad, "x"); !errors.Is(err, ErrBadHash) {
			t.Errorf("%q: %v", bad, err)
		}
	}
	// default params produce a verifiable hash
	h, _ = HashPassword("pw", DefaultParams)
	if ok, _ := VerifyPassword(h, "pw"); !ok {
		t.Fatal("default params")
	}
}

func TestServiceFlow(t *testing.T) {
	ctx := context.Background()
	repo := db.NewMemory()
	s := NewServiceWithParams(repo, time.Hour, fast)
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	s.Now = func() time.Time { return now }

	var ve *ValidationError
	if _, err := s.Register(ctx, "not-an-email", "password1", ""); !errors.As(err, &ve) || ve.Field != "email" {
		t.Fatal(err)
	}
	if _, err := s.Register(ctx, "a@b.c", "short", ""); !errors.As(err, &ve) || ve.Field != "password" {
		t.Fatal(err)
	}
	u, err := s.Register(ctx, "Player@Example.com", "password1", "")
	if err != nil {
		t.Fatal(err)
	}
	if u.DisplayName != "Player" {
		t.Fatal(u.DisplayName)
	}
	if _, err := s.Register(ctx, "player@example.com", "password2", "x"); !errors.Is(err, ErrEmailTaken) {
		t.Fatal(err)
	}
	if _, _, _, err := s.Login(ctx, "player@example.com", "nope", "", ""); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatal(err)
	}
	if _, _, _, err := s.Login(ctx, "ghost@example.com", "password1", "", ""); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatal(err)
	}
	tok, sess, lu, err := s.Login(ctx, "PLAYER@example.com", "password1", "ua", "1.2.3.4")
	if err != nil || lu.ID != u.ID || sess.ID != TokenHash(tok) || sess.ID == tok {
		t.Fatal(err)
	}
	if _, au, err := s.Authenticate(ctx, tok); err != nil || au.ID != u.ID {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Hour)
	if _, _, err := s.Authenticate(ctx, tok); !errors.Is(err, ErrUnauthenticated) {
		t.Fatal("expired session accepted")
	}
	now = now.Add(-2 * time.Hour)
	if err := s.Logout(ctx, tok); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Authenticate(ctx, tok); !errors.Is(err, ErrUnauthenticated) {
		t.Fatal("logged-out session accepted")
	}
	// bans block login
	repo.CreateBan(ctx, db.Ban{UserID: u.ID, Reason: "test"})
	if _, _, _, err := s.Login(ctx, "player@example.com", "password1", "", ""); !errors.Is(err, ErrBanned) {
		t.Fatal(err)
	}
}

func TestLimiter(t *testing.T) {
	l := NewLimiter(2, time.Minute)
	now := time.Unix(0, 0)
	l.SetClock(func() time.Time { return now })
	if ok, _ := l.Allow("k"); !ok {
		t.Fatal()
	}
	if ok, _ := l.Allow("k"); !ok {
		t.Fatal()
	}
	ok, retry := l.Allow("k")
	if ok || retry != time.Minute {
		t.Fatal(ok, retry)
	}
	if ok, _ := l.Allow("other"); !ok {
		t.Fatal("keys not independent")
	}
	now = now.Add(61 * time.Second)
	if ok, _ := l.Allow("k"); !ok {
		t.Fatal("window did not slide")
	}
	l.Reset("k")
	l.Allow("k")
	if ok, _ := l.Allow("k"); !ok {
		t.Fatal("reset")
	}
}

func TestTickets(t *testing.T) {
	ts := NewTickets(0)
	now := time.Unix(1000, 0)
	ts.SetClock(func() time.Time { return now })
	tk, err := ts.Issue(7, "Player", "g1")
	if err != nil || tk.ExpiresAt != now.Add(60*time.Second) {
		t.Fatal(err, tk)
	}
	if _, err := ts.Redeem(tk.Token, "g2"); !errors.Is(err, ErrTicketInvalid) {
		t.Fatal("wrong game accepted")
	}
	if _, err := ts.Redeem(tk.Token, "g1"); !errors.Is(err, ErrTicketInvalid) {
		t.Fatal("ticket reusable after failed redeem")
	}
	tk, _ = ts.Issue(7, "Player", "g1")
	got, err := ts.Redeem(tk.Token, "g1")
	if err != nil || got.UserID != 7 || got.DisplayName != "Player" {
		t.Fatal(err, got)
	}
	if _, err := ts.Redeem(tk.Token, "g1"); !errors.Is(err, ErrTicketInvalid) {
		t.Fatal("one-time ticket reused")
	}
	tk, _ = ts.Issue(7, "Player", "g1")
	now = now.Add(61 * time.Second)
	if _, err := ts.Redeem(tk.Token, "g1"); !errors.Is(err, ErrTicketInvalid) {
		t.Fatal("expired ticket accepted")
	}
	ts.Issue(1, "", "g")
	if ts.Len() != 1 {
		t.Fatal(ts.Len())
	}
}

// Password hashing is bounded: when every slot is busy a login waits and
// gives up with its context instead of starting another argon2 run.
func TestHashingSlotsBounded(t *testing.T) {
	s := NewServiceWithParams(db.NewMemory(), time.Hour, fast)
	if cap(s.hashSem) < 2 {
		t.Fatalf("hash slots %d", cap(s.hashSem))
	}
	for i := 0; i < cap(s.hashSem); i++ {
		s.hashSem <- struct{}{}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, _, _, err := s.Login(ctx, "nobody@example.com", "password123", "", ""); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("login with all slots busy: %v", err)
	}
}
