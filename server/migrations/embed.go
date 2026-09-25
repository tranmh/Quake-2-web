// Package migrations embeds the goose SQL migrations (run on startup by
// internal/db.Migrate under a Postgres advisory lock).
package migrations

import "embed"

// FS holds the *.sql migration files.
//
//go:embed *.sql
var FS embed.FS
