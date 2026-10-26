//go:build integration

package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"quake2web/server/internal/db"
)

// integrationPG opens a migrated Postgres repo on $AQAA_PG_URL. It prefers a
// throwaway database; when the role cannot create one it uses the configured
// database as is (callers use unique emails then).
func integrationPG(t *testing.T) *db.Postgres {
	t.Helper()
	base := os.Getenv("AQAA_PG_URL")
	if base == "" {
		t.Skip("AQAA_PG_URL not set")
	}
	ctx := context.Background()
	target := base
	if conn, err := pgx.Connect(ctx, base); err == nil {
		var b [6]byte
		rand.Read(b[:]) //nolint:errcheck
		name := "q2it_" + hex.EncodeToString(b[:])
		if _, err := conn.Exec(ctx, "CREATE DATABASE "+name); err == nil {
			u, _ := url.Parse(base)
			u.Path = "/" + name
			target = u.String()
			t.Cleanup(func() {
				conn.Exec(ctx, "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)") //nolint:errcheck
				conn.Close(ctx)
			})
		} else {
			conn.Close(ctx)
		}
	}
	pg, err := db.Open(ctx, target)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pg.Close)
	if err := pg.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	return pg
}

func uniqEmail(prefix string) string {
	var b [4]byte
	rand.Read(b[:]) //nolint:errcheck
	return prefix + "-" + hex.EncodeToString(b[:]) + "@example.com"
}

func sessionCookie(resp *http.Response) *http.Cookie {
	for _, c := range resp.Cookies() {
		if c.Name == SessionCookie {
			return c
		}
	}
	return nil
}

func TestIntegrationAPISuitePostgres(t *testing.T) {
	if os.Getenv("AQAA_PG_URL") == "" {
		t.Skip("AQAA_PG_URL not set")
	}
	// The shared suite uses fixed emails, so it needs its own database.
	pg := integrationPG(t)
	var n int
	if err := pg.Pool.QueryRow(context.Background(), "SELECT count(*) FROM users").Scan(&n); err == nil && n > 0 {
		t.Skip("no throwaway database available and users exist")
	}
	runAPISuite(t, newEnv(t, pg))
}

func TestIntegrationAuthFlowPostgres(t *testing.T) {
	e := newEnv(t, integrationPG(t))
	email := uniqEmail("auth")

	t.Run("register sets session and me works", func(t *testing.T) {
		c := e.client()
		resp := c.do("POST", "/api/v1/auth/register", map[string]string{"email": email, "password": "password123", "displayName": "Auth"})
		if resp.StatusCode != http.StatusCreated {
			expect(t, resp, http.StatusCreated)
		}
		sc := sessionCookie(resp)
		u := decode[meResponse](t, resp).User
		if sc == nil || !sc.HttpOnly || sc.Path != "/" || sc.SameSite != http.SameSiteLaxMode || sc.Secure {
			t.Fatalf("session cookie %+v", sc)
		}
		me := decode[meResponse](t, c.do("GET", "/api/v1/auth/me", nil))
		if me.User.ID != u.ID || me.User.Email != email {
			t.Fatalf("me %+v want %+v", me.User, u)
		}
	})

	t.Run("duplicate email 409 email_taken", func(t *testing.T) {
		b := expect(t, e.client().do("POST", "/api/v1/auth/register", map[string]string{"email": email, "password": "password123", "displayName": "Dup"}), http.StatusConflict)
		if !strings.Contains(string(b), "email_taken") {
			t.Fatalf("body %s", b)
		}
	})

	t.Run("invalid fields 400", func(t *testing.T) {
		for _, body := range []map[string]string{
			{"email": "not-an-email", "password": "password123"},
			{"email": uniqEmail("short"), "password": "x"},
		} {
			expect(t, e.client().do("POST", "/api/v1/auth/register", body), http.StatusBadRequest)
		}
	})

	t.Run("login wrong password 401, good 200, logout clears", func(t *testing.T) {
		c := e.client()
		b := expect(t, c.do("POST", "/api/v1/auth/login", map[string]string{"email": email, "password": "wrongpass"}), http.StatusUnauthorized)
		if !strings.Contains(string(b), "invalid_credentials") {
			t.Fatalf("body %s", b)
		}
		expect(t, c.do("POST", "/api/v1/auth/login", map[string]string{"email": uniqEmail("nobody"), "password": "password123"}), http.StatusUnauthorized)
		resp := c.do("POST", "/api/v1/auth/login", map[string]string{"email": strings.ToUpper(email), "password": "password123"})
		if sessionCookie(resp) == nil {
			t.Fatal("no session cookie on login")
		}
		expect(t, resp, http.StatusOK)
		expect(t, c.do("GET", "/api/v1/auth/me", nil), http.StatusOK)
		resp = c.do("POST", "/api/v1/auth/logout", nil)
		sc := sessionCookie(resp)
		expect(t, resp, http.StatusNoContent)
		if sc == nil || sc.MaxAge >= 0 {
			t.Fatalf("logout cookie %+v", sc)
		}
		expect(t, c.do("GET", "/api/v1/auth/me", nil), http.StatusUnauthorized)
	})
}

func TestIntegrationLoginRateLimitPerEmail(t *testing.T) {
	e := newEnv(t, integrationPG(t))
	email := uniqEmail("rl")
	e.client().register(email)
	c := e.client()
	// Default email limiter: 10 per 10 minutes, keyed on the trimmed lower-cased email.
	for i := 0; i < 10; i++ {
		addr := email
		if i%2 == 1 {
			addr = "  " + strings.ToUpper(email) + " "
		}
		expect(t, c.do("POST", "/api/v1/auth/login", map[string]string{"email": addr, "password": "wrongpass"}), http.StatusUnauthorized)
	}
	resp := c.do("POST", "/api/v1/auth/login", map[string]string{"email": email, "password": "password123"})
	ra := resp.Header.Get("Retry-After")
	b := expect(t, resp, http.StatusTooManyRequests)
	if secs, err := strconv.Atoi(ra); err != nil || secs < 1 || !strings.Contains(string(b), "rate_limited") {
		t.Fatalf("Retry-After %q body %s", ra, b)
	}
}

func TestIntegrationPaksAndAssetsPostgres(t *testing.T) {
	e := newEnv(t, integrationPG(t))
	anon, alice, bob := e.client(), e.client(), e.client()
	alice.register(uniqEmail("alice"))
	bob.register(uniqEmail("bob"))

	expect(t, anon.upload("x.pak", []byte("PACK")), http.StatusUnauthorized)
	expect(t, alice.do("POST", "/api/v1/paks", []byte(`{}`), "Content-Type", "application/json"), http.StatusUnsupportedMediaType)
	expect(t, alice.upload("bad.pak", []byte("not a pak at all")), http.StatusBadRequest)

	content := []byte("unique content " + uniqEmail("c"))
	pak := buildPak(map[string][]byte{"docs/readme.txt": content}, []string{"docs/readme.txt"})
	type upResp struct {
		Pak struct {
			ID int64 `json:"id"`
		} `json:"pak"`
	}
	resp := alice.upload("mine.pak", pak)
	if resp.StatusCode != http.StatusAccepted {
		expect(t, resp, http.StatusAccepted)
	}
	up := decode[upResp](t, resp)
	if up.Pak.ID == 0 {
		t.Fatal("no pak id")
	}
	id := strconv.FormatInt(up.Pak.ID, 10)

	expect(t, alice.do("GET", "/api/v1/paks/"+id, nil), http.StatusOK)
	list := decode[struct {
		Paks []struct {
			ID int64 `json:"id"`
		} `json:"paks"`
	}](t, alice.do("GET", "/api/v1/paks", nil))
	found := false
	for _, p := range list.Paks {
		found = found || p.ID == up.Pak.ID
	}
	if !found {
		t.Fatalf("own pak %d not listed: %+v", up.Pak.ID, list.Paks)
	}
	expect(t, alice.do("GET", "/api/v1/paks/999999999", nil), http.StatusNotFound)

	// asset lookups
	expect(t, anon.do("GET", "/assets/nothex", nil), http.StatusNotFound)
	expect(t, anon.do("GET", "/assets/"+strings.Repeat("0", 64), nil), http.StatusNotFound)
}
