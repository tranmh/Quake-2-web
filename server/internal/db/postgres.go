package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"

	"quake2web/server/migrations"
)

// Postgres implements Repo on a pgx pool.
type Postgres struct {
	Pool *pgxpool.Pool
}

// Open connects to url (a pgx connection string) and pings it.
func Open(ctx context.Context, url string) (*Postgres, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, err
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return &Postgres{Pool: pool}, nil
}

// migrationLockID is the advisory lock key taken while migrating (so that
// several server replicas starting together migrate once).
const migrationLockID = 0x51325745 // "Q2WE"

func (p *Postgres) provider() (*goose.Provider, *sql.DB, error) {
	sqlDB := stdlib.OpenDBFromPool(p.Pool)
	locker, err := lock.NewPostgresSessionLocker(lock.WithLockID(migrationLockID))
	if err != nil {
		sqlDB.Close()
		return nil, nil, err
	}
	prov, err := goose.NewProvider(goose.DialectPostgres, sqlDB, migrations.FS,
		goose.WithSessionLocker(locker), goose.WithDisableGlobalRegistry(true))
	if err != nil {
		sqlDB.Close()
		return nil, nil, err
	}
	return prov, sqlDB, nil
}

// Migrate applies all pending migrations under an advisory lock.
func (p *Postgres) Migrate(ctx context.Context) error {
	prov, sqlDB, err := p.provider()
	if err != nil {
		return err
	}
	defer sqlDB.Close()
	_, err = prov.Up(ctx)
	return err
}

// MigrateDownAll rolls back every migration (tests, tooling).
func (p *Postgres) MigrateDownAll(ctx context.Context) error {
	prov, sqlDB, err := p.provider()
	if err != nil {
		return err
	}
	defer sqlDB.Close()
	_, err = prov.DownTo(ctx, 0)
	return err
}

// MigrationVersion returns the current schema version.
func (p *Postgres) MigrationVersion(ctx context.Context) (int64, error) {
	prov, sqlDB, err := p.provider()
	if err != nil {
		return 0, err
	}
	defer sqlDB.Close()
	return prov.GetDBVersion(ctx)
}

// Ping implements Repo.
func (p *Postgres) Ping(ctx context.Context) error { return p.Pool.Ping(ctx) }

// Close implements Repo.
func (p *Postgres) Close() { p.Pool.Close() }

func mapErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	var pe *pgconn.PgError
	if errors.As(err, &pe) {
		switch pe.Code {
		case "23505": // unique_violation
			return fmt.Errorf("%w: %s", ErrConflict, pe.ConstraintName)
		case "23503": // foreign_key_violation
			return fmt.Errorf("%w: %s", ErrConflict, pe.ConstraintName)
		}
	}
	return err
}

func nullID(id int64) *int64 {
	if id == 0 {
		return nil
	}
	return &id
}

func derefID(id *int64) int64 {
	if id == nil {
		return 0
	}
	return *id
}

// ---- users ----

const userCols = `id, email, display_name, password_hash, is_admin, created_at`

func scanUser(row pgx.Row) (User, error) {
	var u User
	err := row.Scan(&u.ID, &u.Email, &u.DisplayName, &u.PasswordHash, &u.IsAdmin, &u.CreatedAt)
	return u, mapErr(err)
}

// CreateUser implements Repo.
func (p *Postgres) CreateUser(ctx context.Context, u User) (User, error) {
	return scanUser(p.Pool.QueryRow(ctx,
		`INSERT INTO users (email, display_name, password_hash, is_admin) VALUES ($1, $2, $3, $4)
		 RETURNING `+userCols, u.Email, u.DisplayName, u.PasswordHash, u.IsAdmin))
}

// UserByEmail implements Repo.
func (p *Postgres) UserByEmail(ctx context.Context, email string) (User, error) {
	return scanUser(p.Pool.QueryRow(ctx, `SELECT `+userCols+` FROM users WHERE lower(email) = lower($1)`, email))
}

// UserByID implements Repo.
func (p *Postgres) UserByID(ctx context.Context, id int64) (User, error) {
	return scanUser(p.Pool.QueryRow(ctx, `SELECT `+userCols+` FROM users WHERE id = $1`, id))
}

// ---- sessions ----

// CreateSession implements Repo.
func (p *Postgres) CreateSession(ctx context.Context, s Session) error {
	_, err := p.Pool.Exec(ctx,
		`INSERT INTO sessions (id, user_id, created_at, expires_at, user_agent, ip) VALUES ($1, $2, $3, $4, $5, $6)`,
		s.ID, s.UserID, s.CreatedAt, s.ExpiresAt, s.UserAgent, s.IP)
	return mapErr(err)
}

// SessionUser implements Repo.
func (p *Postgres) SessionUser(ctx context.Context, id string, now time.Time) (Session, User, error) {
	var s Session
	var u User
	err := p.Pool.QueryRow(ctx,
		`SELECT s.id, s.user_id, s.created_at, s.expires_at, s.user_agent, s.ip,
		        u.id, u.email, u.display_name, u.password_hash, u.is_admin, u.created_at
		   FROM sessions s JOIN users u ON u.id = s.user_id
		  WHERE s.id = $1 AND s.expires_at > $2`, id, now).
		Scan(&s.ID, &s.UserID, &s.CreatedAt, &s.ExpiresAt, &s.UserAgent, &s.IP,
			&u.ID, &u.Email, &u.DisplayName, &u.PasswordHash, &u.IsAdmin, &u.CreatedAt)
	return s, u, mapErr(err)
}

// DeleteSession implements Repo.
func (p *Postgres) DeleteSession(ctx context.Context, id string) error {
	_, err := p.Pool.Exec(ctx, `DELETE FROM sessions WHERE id = $1`, id)
	return mapErr(err)
}

// DeleteExpiredSessions implements Repo.
func (p *Postgres) DeleteExpiredSessions(ctx context.Context, now time.Time) (int64, error) {
	tag, err := p.Pool.Exec(ctx, `DELETE FROM sessions WHERE expires_at <= $1`, now)
	return tag.RowsAffected(), mapErr(err)
}

// ---- blobs ----

type querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	CopyFrom(ctx context.Context, tableName pgx.Identifier, columnNames []string, rowSrc pgx.CopyFromSource) (int64, error)
}

func upsertBlobs(ctx context.Context, q querier, blobs []Blob) error {
	if len(blobs) == 0 {
		return nil
	}
	shas := make([]string, len(blobs))
	sizes := make([]int64, len(blobs))
	types := make([]string, len(blobs))
	for i, b := range blobs {
		shas[i], sizes[i], types[i] = b.SHA256, b.Size, b.ContentType
	}
	_, err := q.Exec(ctx,
		`INSERT INTO blobs (sha256, size, content_type)
		 SELECT * FROM unnest($1::text[], $2::bigint[], $3::text[])
		 ON CONFLICT (sha256) DO NOTHING`, shas, sizes, types)
	return mapErr(err)
}

// UpsertBlobs implements Repo.
func (p *Postgres) UpsertBlobs(ctx context.Context, blobs []Blob) error {
	return upsertBlobs(ctx, p.Pool, blobs)
}

// BlobBySHA implements Repo.
func (p *Postgres) BlobBySHA(ctx context.Context, sha string) (Blob, error) {
	var b Blob
	err := p.Pool.QueryRow(ctx, `SELECT sha256, size, content_type FROM blobs WHERE sha256 = $1`, sha).
		Scan(&b.SHA256, &b.Size, &b.ContentType)
	return b, mapErr(err)
}

// AssetAccess implements Repo.
func (p *Postgres) AssetAccess(ctx context.Context, sha string, userID int64) (Blob, bool, error) {
	var b Blob
	var ok bool
	err := p.Pool.QueryRow(ctx,
		`SELECT b.sha256, b.size, b.content_type,
		        EXISTS (SELECT 1 FROM pak_assets a JOIN paks k ON k.id = a.pak_id
		                 WHERE a.sha256 = b.sha256 AND k.status = 'ready'
		                   AND (k.public OR EXISTS (SELECT 1 FROM pak_owners o
		                                             WHERE o.pak_id = k.id AND o.user_id = $2)))
		   FROM blobs b WHERE b.sha256 = $1`, sha, userID).
		Scan(&b.SHA256, &b.Size, &b.ContentType, &ok)
	return b, ok, mapErr(err)
}

// ---- paks ----

const pakCols = `id, sha256, name, size, checksum, num_files, public, status, error, manifest_sha256, created_at, ingested_at`

func scanPak(row pgx.Row) (Pak, error) {
	var k Pak
	var ck int64
	err := row.Scan(&k.ID, &k.SHA256, &k.Name, &k.Size, &ck, &k.NumFiles, &k.Public, &k.Status, &k.Error,
		&k.ManifestSHA256, &k.CreatedAt, &k.IngestedAt)
	k.Checksum = uint32(ck)
	return k, mapErr(err)
}

// CreatePak implements Repo.
func (p *Postgres) CreatePak(ctx context.Context, k Pak) (Pak, bool, error) {
	if k.Status == "" {
		k.Status = PakPending
	}
	row, err := scanPak(p.Pool.QueryRow(ctx,
		`INSERT INTO paks (sha256, name, size, public, status) VALUES ($1, $2, $3, $4, $5)
		 ON CONFLICT (sha256) DO NOTHING RETURNING `+pakCols, k.SHA256, k.Name, k.Size, k.Public, k.Status))
	if err == nil {
		return row, true, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return Pak{}, false, err
	}
	row, err = p.PakBySHA(ctx, k.SHA256)
	return row, false, err
}

// PakByID implements Repo.
func (p *Postgres) PakByID(ctx context.Context, id int64) (Pak, error) {
	return scanPak(p.Pool.QueryRow(ctx, `SELECT `+pakCols+` FROM paks WHERE id = $1`, id))
}

// PakBySHA implements Repo.
func (p *Postgres) PakBySHA(ctx context.Context, sha string) (Pak, error) {
	return scanPak(p.Pool.QueryRow(ctx, `SELECT `+pakCols+` FROM paks WHERE sha256 = $1`, sha))
}

// AddPakOwner implements Repo.
func (p *Postgres) AddPakOwner(ctx context.Context, pakID, userID int64, filename string) error {
	_, err := p.Pool.Exec(ctx,
		`INSERT INTO pak_owners (pak_id, user_id, filename) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`,
		pakID, userID, filename)
	return mapErr(err)
}

// PakOwned implements Repo.
func (p *Postgres) PakOwned(ctx context.Context, pakID, userID int64) (bool, error) {
	var ok bool
	err := p.Pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM pak_owners WHERE pak_id = $1 AND user_id = $2)`, pakID, userID).Scan(&ok)
	return ok, mapErr(err)
}

// ListPaks implements Repo.
func (p *Postgres) ListPaks(ctx context.Context, userID int64) ([]Pak, error) {
	rows, err := p.Pool.Query(ctx,
		`SELECT `+pakCols+` FROM paks k
		  WHERE k.public OR EXISTS (SELECT 1 FROM pak_owners o WHERE o.pak_id = k.id AND o.user_id = $1)
		  ORDER BY k.id`, userID)
	if err != nil {
		return nil, mapErr(err)
	}
	defer rows.Close()
	var out []Pak
	for rows.Next() {
		k, err := scanPak(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, mapErr(rows.Err())
}

// SetPakStatus implements Repo.
func (p *Postgres) SetPakStatus(ctx context.Context, id int64, status, errMsg string) error {
	tag, err := p.Pool.Exec(ctx, `UPDATE paks SET status = $2, error = $3 WHERE id = $1`, id, status, errMsg)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return mapErr(err)
}

// SetPakPublic implements Repo.
func (p *Postgres) SetPakPublic(ctx context.Context, id int64, public bool) error {
	tag, err := p.Pool.Exec(ctx, `UPDATE paks SET public = $2 WHERE id = $1`, id, public)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return mapErr(err)
}

// FinishPakIngest implements Repo.
func (p *Postgres) FinishPakIngest(ctx context.Context, id int64, r PakIngest) error {
	return pgx.BeginFunc(ctx, p.Pool, func(tx pgx.Tx) error {
		if err := upsertBlobs(ctx, tx, r.Blobs); err != nil {
			return err
		}
		for _, q := range []string{`DELETE FROM pak_entries WHERE pak_id = $1`,
			`DELETE FROM pak_assets WHERE pak_id = $1`, `DELETE FROM maps WHERE pak_id = $1`} {
			if _, err := tx.Exec(ctx, q, id); err != nil {
				return mapErr(err)
			}
		}
		if _, err := tx.CopyFrom(ctx, pgx.Identifier{"pak_entries"},
			[]string{"pak_id", "idx", "name", "filepos", "filelen", "sha256", "kind"},
			pgx.CopyFromSlice(len(r.Entries), func(i int) ([]any, error) {
				e := r.Entries[i]
				return []any{id, e.Idx, e.Name, e.FilePos, e.FileLen, e.SHA256, e.Kind}, nil
			})); err != nil {
			return mapErr(err)
		}
		seen := map[string]bool{}
		assets := make([]PakAsset, 0, len(r.Assets))
		for _, a := range r.Assets {
			if !seen[a.SHA256] {
				seen[a.SHA256] = true
				assets = append(assets, a)
			}
		}
		if _, err := tx.CopyFrom(ctx, pgx.Identifier{"pak_assets"}, []string{"pak_id", "sha256", "role"},
			pgx.CopyFromSlice(len(assets), func(i int) ([]any, error) {
				return []any{id, assets[i].SHA256, assets[i].Role}, nil
			})); err != nil {
			return mapErr(err)
		}
		for _, m := range r.Maps {
			info := m.Info
			if len(info) == 0 {
				info = json.RawMessage(`{}`)
			}
			if _, err := tx.Exec(ctx,
				`INSERT INTO maps (pak_id, path, name, sha256, checksum, message, sky, info)
				 VALUES ($1, $2, $3, $4, $5, $6, $7, $8) ON CONFLICT (pak_id, path) DO NOTHING`,
				id, m.Path, m.Name, m.SHA256, int64(m.Checksum), m.Message, m.Sky, []byte(info)); err != nil {
				return mapErr(err)
			}
		}
		tag, err := tx.Exec(ctx,
			`UPDATE paks SET checksum = $2, num_files = $3, manifest_sha256 = $4, status = 'ready', error = '',
			        ingested_at = now() WHERE id = $1`, id, int64(r.Checksum), r.NumFiles, r.ManifestSHA256)
		if err != nil {
			return mapErr(err)
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return nil
	})
}

// PakEntries implements Repo.
func (p *Postgres) PakEntries(ctx context.Context, pakID int64) ([]PakEntry, error) {
	rows, err := p.Pool.Query(ctx,
		`SELECT idx, name, filepos, filelen, sha256, kind FROM pak_entries WHERE pak_id = $1 ORDER BY idx`, pakID)
	if err != nil {
		return nil, mapErr(err)
	}
	defer rows.Close()
	var out []PakEntry
	for rows.Next() {
		var e PakEntry
		if err := rows.Scan(&e.Idx, &e.Name, &e.FilePos, &e.FileLen, &e.SHA256, &e.Kind); err != nil {
			return nil, mapErr(err)
		}
		out = append(out, e)
	}
	return out, mapErr(rows.Err())
}

// ---- paksets ----

func (p *Postgres) paksetPaks(ctx context.Context, q pgx.Tx, ps *Pakset) error {
	var rows pgx.Rows
	var err error
	const sqlq = `SELECT pak_id FROM pakset_paks WHERE pakset_id = $1 ORDER BY position`
	if q != nil {
		rows, err = q.Query(ctx, sqlq, ps.ID)
	} else {
		rows, err = p.Pool.Query(ctx, sqlq, ps.ID)
	}
	if err != nil {
		return mapErr(err)
	}
	defer rows.Close()
	ps.PakIDs = []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return mapErr(err)
		}
		ps.PakIDs = append(ps.PakIDs, id)
	}
	return mapErr(rows.Err())
}

const paksetCols = `id, name, owner_id, public, created_at, updated_at`

func scanPakset(row pgx.Row) (Pakset, error) {
	var ps Pakset
	var owner *int64
	err := row.Scan(&ps.ID, &ps.Name, &owner, &ps.Public, &ps.CreatedAt, &ps.UpdatedAt)
	ps.OwnerID = derefID(owner)
	return ps, mapErr(err)
}

func writePaksetPaks(ctx context.Context, tx pgx.Tx, id string, pakIDs []int64) error {
	if _, err := tx.Exec(ctx, `DELETE FROM pakset_paks WHERE pakset_id = $1`, id); err != nil {
		return mapErr(err)
	}
	for i, pid := range pakIDs {
		if _, err := tx.Exec(ctx, `INSERT INTO pakset_paks (pakset_id, position, pak_id) VALUES ($1, $2, $3)`,
			id, i, pid); err != nil {
			return mapErr(err)
		}
	}
	return nil
}

// CreatePakset implements Repo.
func (p *Postgres) CreatePakset(ctx context.Context, ps Pakset) (Pakset, error) {
	var out Pakset
	err := pgx.BeginFunc(ctx, p.Pool, func(tx pgx.Tx) error {
		var err error
		out, err = scanPakset(tx.QueryRow(ctx,
			`INSERT INTO paksets (id, name, owner_id, public) VALUES ($1, $2, $3, $4) RETURNING `+paksetCols,
			ps.ID, ps.Name, nullID(ps.OwnerID), ps.Public))
		if err != nil {
			return err
		}
		if err := writePaksetPaks(ctx, tx, ps.ID, ps.PakIDs); err != nil {
			return err
		}
		out.PakIDs = append([]int64{}, ps.PakIDs...)
		return nil
	})
	return out, err
}

// UpsertPakset implements Repo.
func (p *Postgres) UpsertPakset(ctx context.Context, ps Pakset) (Pakset, error) {
	var out Pakset
	err := pgx.BeginFunc(ctx, p.Pool, func(tx pgx.Tx) error {
		var err error
		out, err = scanPakset(tx.QueryRow(ctx,
			`INSERT INTO paksets (id, name, owner_id, public) VALUES ($1, $2, $3, $4)
			 ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name, owner_id = EXCLUDED.owner_id,
			        public = EXCLUDED.public, updated_at = now()
			 RETURNING `+paksetCols, ps.ID, ps.Name, nullID(ps.OwnerID), ps.Public))
		if err != nil {
			return err
		}
		if err := writePaksetPaks(ctx, tx, ps.ID, ps.PakIDs); err != nil {
			return err
		}
		out.PakIDs = append([]int64{}, ps.PakIDs...)
		return nil
	})
	return out, err
}

// PaksetByID implements Repo.
func (p *Postgres) PaksetByID(ctx context.Context, id string) (Pakset, error) {
	ps, err := scanPakset(p.Pool.QueryRow(ctx, `SELECT `+paksetCols+` FROM paksets WHERE id = $1`, id))
	if err != nil {
		return ps, err
	}
	return ps, p.paksetPaks(ctx, nil, &ps)
}

// ListPaksets implements Repo.
func (p *Postgres) ListPaksets(ctx context.Context, userID int64) ([]Pakset, error) {
	rows, err := p.Pool.Query(ctx,
		`SELECT `+paksetCols+` FROM paksets WHERE public OR owner_id = $1 ORDER BY created_at, id`, userID)
	if err != nil {
		return nil, mapErr(err)
	}
	var out []Pakset
	for rows.Next() {
		ps, err := scanPakset(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, ps)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, mapErr(err)
	}
	for i := range out {
		if err := p.paksetPaks(ctx, nil, &out[i]); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// UpdatePakset implements Repo.
func (p *Postgres) UpdatePakset(ctx context.Context, ps Pakset) (Pakset, error) {
	var out Pakset
	err := pgx.BeginFunc(ctx, p.Pool, func(tx pgx.Tx) error {
		var err error
		out, err = scanPakset(tx.QueryRow(ctx,
			`UPDATE paksets SET name = $2, public = $3, updated_at = now() WHERE id = $1 RETURNING `+paksetCols,
			ps.ID, ps.Name, ps.Public))
		if err != nil {
			return err
		}
		if err := writePaksetPaks(ctx, tx, ps.ID, ps.PakIDs); err != nil {
			return err
		}
		out.PakIDs = append([]int64{}, ps.PakIDs...)
		return nil
	})
	return out, err
}

// DeletePakset implements Repo.
func (p *Postgres) DeletePakset(ctx context.Context, id string) error {
	tag, err := p.Pool.Exec(ctx, `DELETE FROM paksets WHERE id = $1`, id)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return mapErr(err)
}

// ---- maps ----

// MapsForPaks implements Repo.
func (p *Postgres) MapsForPaks(ctx context.Context, pakIDs []int64) ([]MapRow, error) {
	rows, err := p.Pool.Query(ctx,
		`SELECT pak_id, path, name, sha256, checksum, message, sky, info FROM maps
		  WHERE pak_id = ANY($1) ORDER BY name, pak_id`, pakIDs)
	if err != nil {
		return nil, mapErr(err)
	}
	defer rows.Close()
	var out []MapRow
	for rows.Next() {
		var m MapRow
		var ck int64
		var info []byte
		if err := rows.Scan(&m.PakID, &m.Path, &m.Name, &m.SHA256, &ck, &m.Message, &m.Sky, &info); err != nil {
			return nil, mapErr(err)
		}
		m.Checksum = uint32(ck)
		m.Info = info
		out = append(out, m)
	}
	return out, mapErr(rows.Err())
}

// ---- jobs ----

const jobCols = `id, kind, status, pak_id, user_id, progress, error, created_at, started_at, finished_at`

func scanJob(row pgx.Row) (Job, error) {
	var j Job
	var pak, user *int64
	err := row.Scan(&j.ID, &j.Kind, &j.Status, &pak, &user, &j.Progress, &j.Error, &j.CreatedAt, &j.StartedAt, &j.FinishedAt)
	j.PakID, j.UserID = derefID(pak), derefID(user)
	return j, mapErr(err)
}

// CreateJob implements Repo.
func (p *Postgres) CreateJob(ctx context.Context, j Job) (Job, error) {
	if j.Status == "" {
		j.Status = JobQueued
	}
	return scanJob(p.Pool.QueryRow(ctx,
		`INSERT INTO jobs (kind, status, pak_id, user_id) VALUES ($1, $2, $3, $4) RETURNING `+jobCols,
		j.Kind, j.Status, nullID(j.PakID), nullID(j.UserID)))
}

// UpdateJob implements Repo.
func (p *Postgres) UpdateJob(ctx context.Context, id int64, status string, progress float32, errMsg string) error {
	tag, err := p.Pool.Exec(ctx,
		`UPDATE jobs SET status = $2, progress = $3, error = $4,
		        started_at = CASE WHEN $2 = 'running' AND started_at IS NULL THEN now() ELSE started_at END,
		        finished_at = CASE WHEN $2 IN ('done', 'failed') THEN now() ELSE finished_at END
		  WHERE id = $1`, id, status, progress, errMsg)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return mapErr(err)
}

// JobByID implements Repo.
func (p *Postgres) JobByID(ctx context.Context, id int64) (Job, error) {
	return scanJob(p.Pool.QueryRow(ctx, `SELECT `+jobCols+` FROM jobs WHERE id = $1`, id))
}

// JobsForPak implements Repo.
func (p *Postgres) JobsForPak(ctx context.Context, pakID int64) ([]Job, error) {
	rows, err := p.Pool.Query(ctx, `SELECT `+jobCols+` FROM jobs WHERE pak_id = $1 ORDER BY id`, pakID)
	if err != nil {
		return nil, mapErr(err)
	}
	defer rows.Close()
	var out []Job
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, mapErr(rows.Err())
}

// ---- saves ----

// PutSave implements Repo.
func (p *Postgres) PutSave(ctx context.Context, userID int64, slot string, blob []byte, m SaveMeta) error {
	_, err := p.Pool.Exec(ctx,
		`INSERT INTO saves (user_id, slot, comment, mapcmd, mode, schema, blob, size)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		 ON CONFLICT (user_id, slot) DO UPDATE SET comment = EXCLUDED.comment, mapcmd = EXCLUDED.mapcmd,
		        mode = EXCLUDED.mode, schema = EXCLUDED.schema, blob = EXCLUDED.blob, size = EXCLUDED.size,
		        updated_at = now()`,
		userID, slot, m.Comment, m.MapCmd, m.Mode, m.Schema, blob, len(blob))
	return mapErr(err)
}

// GetSave implements Repo.
func (p *Postgres) GetSave(ctx context.Context, userID int64, slot string) ([]byte, SaveInfo, error) {
	var si SaveInfo
	var blob []byte
	err := p.Pool.QueryRow(ctx,
		`SELECT slot, comment, mapcmd, mode, schema, size, created_at, updated_at, blob
		   FROM saves WHERE user_id = $1 AND slot = $2`, userID, slot).
		Scan(&si.Slot, &si.Comment, &si.MapCmd, &si.Mode, &si.Schema, &si.Size, &si.CreatedAt, &si.UpdatedAt, &blob)
	return blob, si, mapErr(err)
}

// ListSaves implements Repo.
func (p *Postgres) ListSaves(ctx context.Context, userID int64) ([]SaveInfo, error) {
	rows, err := p.Pool.Query(ctx,
		`SELECT slot, comment, mapcmd, mode, schema, size, created_at, updated_at
		   FROM saves WHERE user_id = $1 ORDER BY slot`, userID)
	if err != nil {
		return nil, mapErr(err)
	}
	defer rows.Close()
	out := []SaveInfo{}
	for rows.Next() {
		var si SaveInfo
		if err := rows.Scan(&si.Slot, &si.Comment, &si.MapCmd, &si.Mode, &si.Schema, &si.Size, &si.CreatedAt, &si.UpdatedAt); err != nil {
			return nil, mapErr(err)
		}
		out = append(out, si)
	}
	return out, mapErr(rows.Err())
}

// DeleteSave implements Repo.
func (p *Postgres) DeleteSave(ctx context.Context, userID int64, slot string) error {
	tag, err := p.Pool.Exec(ctx, `DELETE FROM saves WHERE user_id = $1 AND slot = $2`, userID, slot)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return mapErr(err)
}

// ---- settings ----

// GetSettings implements Repo.
func (p *Postgres) GetSettings(ctx context.Context, userID int64) (string, time.Time, error) {
	var cfg string
	var at time.Time
	err := p.Pool.QueryRow(ctx, `SELECT config, updated_at FROM settings WHERE user_id = $1`, userID).Scan(&cfg, &at)
	return cfg, at, mapErr(err)
}

// PutSettings implements Repo.
func (p *Postgres) PutSettings(ctx context.Context, userID int64, config string) error {
	_, err := p.Pool.Exec(ctx,
		`INSERT INTO settings (user_id, config) VALUES ($1, $2)
		 ON CONFLICT (user_id) DO UPDATE SET config = EXCLUDED.config, updated_at = now()`, userID, config)
	return mapErr(err)
}

// ---- games ----

// InsertGame implements Repo.
func (p *Postgres) InsertGame(ctx context.Context, g Game) error {
	settings := g.Settings
	if len(settings) == 0 {
		settings = json.RawMessage(`{}`)
	}
	if g.StartedAt.IsZero() {
		g.StartedAt = time.Now()
	}
	_, err := p.Pool.Exec(ctx,
		`INSERT INTO games (id, owner_id, mode, map, pakset_id, settings, public, started_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		g.ID, nullID(g.OwnerID), g.Mode, g.Map, g.PaksetID, []byte(settings), g.Public, g.StartedAt)
	return mapErr(err)
}

// EndGame implements Repo.
func (p *Postgres) EndGame(ctx context.Context, id string, at time.Time) error {
	tag, err := p.Pool.Exec(ctx, `UPDATE games SET ended_at = $2 WHERE id = $1 AND ended_at IS NULL`, id, at)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return mapErr(err)
}

// ListGames implements Repo.
func (p *Postgres) ListGames(ctx context.Context, activeOnly bool) ([]Game, error) {
	q := `SELECT id, owner_id, mode, map, pakset_id, settings, public, started_at, ended_at FROM games`
	if activeOnly {
		q += ` WHERE ended_at IS NULL`
	}
	rows, err := p.Pool.Query(ctx, q+` ORDER BY started_at, id`)
	if err != nil {
		return nil, mapErr(err)
	}
	defer rows.Close()
	var out []Game
	for rows.Next() {
		var g Game
		var owner *int64
		var settings []byte
		if err := rows.Scan(&g.ID, &owner, &g.Mode, &g.Map, &g.PaksetID, &settings, &g.Public, &g.StartedAt, &g.EndedAt); err != nil {
			return nil, mapErr(err)
		}
		g.OwnerID = derefID(owner)
		g.Settings = settings
		out = append(out, g)
	}
	return out, mapErr(rows.Err())
}

// ---- bans ----

// CreateBan implements Repo.
func (p *Postgres) CreateBan(ctx context.Context, b Ban) (Ban, error) {
	var out Ban
	var user, by *int64
	err := p.Pool.QueryRow(ctx,
		`INSERT INTO bans (user_id, ip, reason, created_by, expires_at) VALUES ($1, $2, $3, $4, $5)
		 RETURNING id, user_id, ip, reason, created_by, created_at, expires_at`,
		nullID(b.UserID), b.IP, b.Reason, nullID(b.CreatedBy), b.ExpiresAt).
		Scan(&out.ID, &user, &out.IP, &out.Reason, &by, &out.CreatedAt, &out.ExpiresAt)
	out.UserID, out.CreatedBy = derefID(user), derefID(by)
	return out, mapErr(err)
}

// ActiveBan implements Repo.
func (p *Postgres) ActiveBan(ctx context.Context, userID int64, ip string, now time.Time) (*Ban, error) {
	var out Ban
	var user, by *int64
	err := p.Pool.QueryRow(ctx,
		`SELECT id, user_id, ip, reason, created_by, created_at, expires_at FROM bans
		  WHERE (($1 <> 0 AND user_id = $1) OR ($2 <> '' AND ip = $2))
		    AND (expires_at IS NULL OR expires_at > $3)
		  ORDER BY id LIMIT 1`, userID, strings.TrimSpace(ip), now).
		Scan(&out.ID, &user, &out.IP, &out.Reason, &by, &out.CreatedAt, &out.ExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, mapErr(err)
	}
	out.UserID, out.CreatedBy = derefID(user), derefID(by)
	return &out, nil
}

var _ Repo = (*Postgres)(nil)

// ---- bot spend ----

// BotSpend implements Repo.
func (p *Postgres) BotSpend(ctx context.Context, day string) (float64, error) {
	if err := checkBotSpend(day, 0); err != nil {
		return 0, err
	}
	var usd float64
	err := p.Pool.QueryRow(ctx, `SELECT usd FROM bot_spend WHERE day = $1::date`, day).Scan(&usd)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	return usd, mapErr(err)
}

// AddBotSpend implements Repo.
func (p *Postgres) AddBotSpend(ctx context.Context, day string, usd float64) (float64, error) {
	if err := checkBotSpend(day, usd); err != nil {
		return 0, err
	}
	var total float64
	err := p.Pool.QueryRow(ctx,
		`INSERT INTO bot_spend (day, usd) VALUES ($1::date, $2)
		 ON CONFLICT (day) DO UPDATE SET usd = bot_spend.usd + EXCLUDED.usd, updated_at = now()
		 RETURNING usd`, day, usd).Scan(&total)
	return total, mapErr(err)
}
