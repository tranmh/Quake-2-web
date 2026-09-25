// Package dbtest creates throwaway Postgres databases for tests gated on
// DATABASE_URL_TEST, so that packages testing in parallel do not interfere.
package dbtest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
)

// URL creates a fresh database on the server of $DATABASE_URL_TEST and
// returns its URL; the database is dropped when the test ends. The test is
// skipped when DATABASE_URL_TEST is not set.
func URL(t testing.TB) string {
	t.Helper()
	base := os.Getenv("DATABASE_URL_TEST")
	if base == "" {
		t.Skip("DATABASE_URL_TEST not set")
	}
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, base)
	if err != nil {
		t.Fatal(err)
	}
	var b [6]byte
	rand.Read(b[:]) //nolint:errcheck
	name := "q2test_" + hex.EncodeToString(b[:])
	if _, err := conn.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		conn.Close(ctx)
		t.Fatal(err)
	}
	t.Cleanup(func() {
		conn.Exec(ctx, "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)") //nolint:errcheck
		conn.Close(ctx)
	})
	u, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/" + name
	return u.String()
}
