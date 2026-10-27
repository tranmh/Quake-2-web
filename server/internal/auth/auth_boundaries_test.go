package auth

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestTicketsTTL(t *testing.T) {
	now := time.Unix(5000, 0)
	for _, tc := range []struct {
		name string
		ttl  time.Duration
		want time.Duration
	}{
		{"zero uses default", 0, TicketTTL},
		{"negative uses default", -time.Second, TicketTTL},
		{"custom", 5 * time.Second, 5 * time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ts := NewTickets(tc.ttl)
			ts.SetClock(func() time.Time { return now })
			tk, err := ts.Issue(1, "a", "g")
			if err != nil {
				t.Fatal(err)
			}
			if !tk.ExpiresAt.Equal(now.Add(tc.want)) {
				t.Fatalf("ExpiresAt %v, want %v", tk.ExpiresAt, now.Add(tc.want))
			}
		})
	}
	if TicketTTL != 60*time.Second {
		t.Fatalf("TicketTTL %v", TicketTTL)
	}
}

func TestTicketsIssueFields(t *testing.T) {
	ts := NewTickets(0)
	a, err := ts.Issue(42, "Ranger", "game-1")
	if err != nil {
		t.Fatal(err)
	}
	b, err := ts.Issue(42, "Ranger", "game-1")
	if err != nil {
		t.Fatal(err)
	}
	if a.UserID != 42 || a.DisplayName != "Ranger" || a.GameID != "game-1" || a.Token == "" {
		t.Fatalf("%+v", a)
	}
	if a.Token == b.Token {
		t.Fatal("tokens not fresh")
	}
	if ts.Len() != 2 {
		t.Fatalf("Len %d", ts.Len())
	}
}

func TestTicketsRedeemExpiryBoundary(t *testing.T) {
	for _, tc := range []struct {
		name    string
		advance time.Duration
		ok      bool
	}{
		{"just before expiry", 10*time.Second - time.Nanosecond, true},
		{"exactly at expiry", 10 * time.Second, false},
		{"after expiry", 11 * time.Second, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ts := NewTickets(10 * time.Second)
			now := time.Unix(1000, 0)
			ts.SetClock(func() time.Time { return now })
			tk, _ := ts.Issue(1, "p", "g")
			now = now.Add(tc.advance)
			got, err := ts.Redeem(tk.Token, "g")
			if tc.ok {
				if err != nil || got.Token != tk.Token {
					t.Fatalf("got %+v, %v", got, err)
				}
			} else if !errors.Is(err, ErrTicketInvalid) {
				t.Fatalf("err %v", err)
			}
			if ts.Len() != 0 {
				t.Fatalf("ticket not consumed: Len %d", ts.Len())
			}
		})
	}
}

func TestTicketsRedeemInvalid(t *testing.T) {
	ts := NewTickets(0)
	for _, tok := range []string{"", "unknown"} {
		if got, err := ts.Redeem(tok, "g"); !errors.Is(err, ErrTicketInvalid) || got != (Ticket{}) {
			t.Fatalf("%q: %+v %v", tok, got, err)
		}
	}
	// a wrong-game redeem burns the ticket; the other ticket is untouched
	a, _ := ts.Issue(1, "p", "g1")
	b, _ := ts.Issue(2, "q", "g1")
	if got, err := ts.Redeem(a.Token, "g2"); !errors.Is(err, ErrTicketInvalid) || got != (Ticket{}) {
		t.Fatalf("%+v %v", got, err)
	}
	if ts.Len() != 1 {
		t.Fatalf("Len %d", ts.Len())
	}
	if _, err := ts.Redeem(a.Token, "g1"); !errors.Is(err, ErrTicketInvalid) {
		t.Fatal("burned ticket redeemed")
	}
	if got, err := ts.Redeem(b.Token, "g1"); err != nil || got.UserID != 2 {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestTicketsIssueSweepsExpired(t *testing.T) {
	ts := NewTickets(10 * time.Second)
	now := time.Unix(1000, 0)
	ts.SetClock(func() time.Time { return now })
	ts.Issue(1, "", "g")
	ts.Issue(2, "", "g")
	now = now.Add(5 * time.Second)
	ts.Issue(3, "", "g")
	if ts.Len() != 3 {
		t.Fatalf("Len %d", ts.Len())
	}
	now = now.Add(5 * time.Second) // first two expire exactly now
	ts.Issue(4, "", "g")
	if ts.Len() != 2 {
		t.Fatalf("Len after sweep %d", ts.Len())
	}
}

func TestLimiterBoundaries(t *testing.T) {
	l := NewLimiter(3, 10*time.Second)
	now := time.Unix(100, 0)
	l.SetClock(func() time.Time { return now })
	step := func(d time.Duration) { now = now.Add(d) }

	for i := 0; i < 3; i++ {
		if ok, retry := l.Allow("k"); !ok || retry != 0 {
			t.Fatalf("event %d: %v %v", i, ok, retry)
		}
		step(2 * time.Second)
	}
	// now = 106; oldest event at 100 leaves the window at 110
	ok, retry := l.Allow("k")
	if ok || retry != 4*time.Second {
		t.Fatalf("4th: %v %v", ok, retry)
	}
	// denied events are not recorded: still the same retry later
	step(3 * time.Second)
	if ok, retry := l.Allow("k"); ok || retry != time.Second {
		t.Fatalf("denied: %v %v", ok, retry)
	}
	// at exactly 110 the event at 100 is outside the window
	step(time.Second)
	if ok, _ := l.Allow("k"); !ok {
		t.Fatal("event at window edge not expired")
	}
	// window now holds 102, 104, 110 -> full; retry until 102 leaves (112)
	if ok, retry := l.Allow("k"); ok || retry != 2*time.Second {
		t.Fatalf("full again: %v %v", ok, retry)
	}
	// other keys are independent
	if ok, _ := l.Allow("other"); !ok {
		t.Fatal("other key limited")
	}
	l.Reset("k")
	for i := 0; i < 3; i++ {
		if ok, _ := l.Allow("k"); !ok {
			t.Fatalf("after reset %d denied", i)
		}
	}
	if ok, _ := l.Allow("k"); ok {
		t.Fatal("limit not enforced after reset")
	}
	l.Reset("never-seen") // no panic
}

func TestLimiterManyKeysSweep(t *testing.T) {
	l := NewLimiter(1, time.Second)
	now := time.Unix(0, 0)
	l.SetClock(func() time.Time { return now })
	l.Allow("keep")
	for i := 0; i < 1100; i++ {
		now = now.Add(time.Millisecond)
		if ok, _ := l.Allow(fmt.Sprintf("k%d", i)); !ok {
			t.Fatalf("k%d denied", i)
		}
	}
	// sweeping old keys must not change behaviour of live keys
	if ok, _ := l.Allow("k1099"); ok {
		t.Fatal("live key forgotten by sweep")
	}
	if ok, _ := l.Allow("keep"); !ok {
		t.Fatal("expired key still limited")
	}
}

func TestHashPasswordFormat(t *testing.T) {
	if DefaultParams.Memory != 19456 || DefaultParams.Time != 2 || DefaultParams.Threads != 1 {
		t.Fatalf("%+v", DefaultParams)
	}
	h, err := HashPassword("pw", fast)
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(h, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" || parts[2] != "v=19" || parts[3] != "m=64,t=1,p=1" {
		t.Fatalf("%q", h)
	}
	salt, err := b64.DecodeString(parts[4])
	if err != nil || len(salt) != 16 {
		t.Fatalf("salt %q %v", parts[4], err)
	}
	key, err := b64.DecodeString(parts[5])
	if err != nil || len(key) != 32 {
		t.Fatalf("key %q %v", parts[5], err)
	}
	// empty and unicode passwords round-trip
	for _, pw := range []string{"", "pässwörd ✓"} {
		h, _ := HashPassword(pw, fast)
		if ok, err := VerifyPassword(h, pw); !ok || err != nil {
			t.Fatalf("%q: %v %v", pw, ok, err)
		}
		if ok, err := VerifyPassword(h, pw+"x"); ok || err != nil {
			t.Fatalf("%q wrong: %v %v", pw, ok, err)
		}
	}
}

func TestVerifyPasswordMalformed(t *testing.T) {
	good, _ := HashPassword("pw", fast)
	parts := strings.Split(good, "$")
	salt, key := parts[4], parts[5]
	mk := func(alg, ver, params, s, k string) string {
		return strings.Join([]string{"", alg, ver, params, s, k}, "$")
	}
	long := b64.EncodeToString(make([]byte, 129))
	for _, tc := range []struct {
		name, hash string
	}{
		{"empty", ""},
		{"too few parts", "$argon2id$v=19$m=64,t=1,p=1$" + salt},
		{"too many parts", good + "$extra"},
		{"argon2i", mk("argon2i", "v=19", "m=64,t=1,p=1", salt, key)},
		{"bcrypt-like", "$2b$10$abcdefghijklmnopqrstuuabcdefghijklmnopqrstuvwxyz012"},
		{"wrong version", mk("argon2id", "v=16", "m=64,t=1,p=1", salt, key)},
		{"version garbage", mk("argon2id", "x=19", "m=64,t=1,p=1", salt, key)},
		{"params garbage", mk("argon2id", "v=19", "m=64;t=1;p=1", salt, key)},
		{"memory zero", mk("argon2id", "v=19", "m=0,t=1,p=1", salt, key)},
		{"memory too large", mk("argon2id", "v=19", fmt.Sprintf("m=%d,t=1,p=1", 1<<21+1), salt, key)},
		{"time zero", mk("argon2id", "v=19", "m=64,t=0,p=1", salt, key)},
		{"time too large", mk("argon2id", "v=19", "m=64,t=17,p=1", salt, key)},
		{"threads zero", mk("argon2id", "v=19", "m=64,t=1,p=0", salt, key)},
		{"negative memory", mk("argon2id", "v=19", "m=-1,t=1,p=1", salt, key)},
		{"bad salt base64", mk("argon2id", "v=19", "m=64,t=1,p=1", "!!", key)},
		{"bad key base64", mk("argon2id", "v=19", "m=64,t=1,p=1", salt, "@@")},
		{"padded key base64", mk("argon2id", "v=19", "m=64,t=1,p=1", salt, key+"==")},
		{"empty key", mk("argon2id", "v=19", "m=64,t=1,p=1", salt, "")},
		{"key too long", mk("argon2id", "v=19", "m=64,t=1,p=1", salt, long)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ok, err := VerifyPassword(tc.hash, "pw")
			if ok || !errors.Is(err, ErrBadHash) {
				t.Fatalf("%q: %v %v", tc.hash, ok, err)
			}
		})
	}
}

func TestVerifyPasswordParamBoundaries(t *testing.T) {
	// t=16 is the largest accepted time cost and a 128-byte key the
	// longest accepted key: a well-formed hash verifies (to false).
	salt := b64.EncodeToString(make([]byte, 16))
	key := b64.EncodeToString(make([]byte, 128))
	for _, params := range []string{"m=8,t=16,p=1", "m=8,t=1,p=1"} {
		h := "$argon2id$v=19$" + params + "$" + salt + "$" + key
		if ok, err := VerifyPassword(h, "pw"); ok || err != nil {
			t.Fatalf("%s: %v %v", params, ok, err)
		}
	}
}
