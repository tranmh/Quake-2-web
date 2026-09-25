// Package db is the persistence layer: row types, the Repo interface, a
// Postgres implementation (pgx/v5 pool, goose migrations embedded from
// server/migrations) and an in-memory implementation used by tests and by
// servers started without DATABASE_URL.
package db

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// Errors returned by every Repo implementation.
var (
	ErrNotFound = errors.New("db: not found")
	ErrConflict = errors.New("db: conflict")
)

// User is a row of users.
type User struct {
	ID           int64     `json:"id"`
	Email        string    `json:"email"`
	DisplayName  string    `json:"displayName"`
	PasswordHash string    `json:"-"`
	IsAdmin      bool      `json:"isAdmin"`
	CreatedAt    time.Time `json:"createdAt"`
}

// Session is a row of sessions. ID is the hex SHA-256 of the cookie token.
type Session struct {
	ID        string
	UserID    int64
	CreatedAt time.Time
	ExpiresAt time.Time
	UserAgent string
	IP        string
}

// Blob is a row of blobs.
type Blob struct {
	SHA256      string `json:"sha256"`
	Size        int64  `json:"size"`
	ContentType string `json:"contentType"`
}

// Pak statuses.
const (
	PakPending   = "pending"
	PakIngesting = "ingesting"
	PakReady     = "ready"
	PakFailed    = "failed"
)

// Pak is a row of paks (unique by content hash; shared by all uploaders).
type Pak struct {
	ID             int64      `json:"id"`
	SHA256         string     `json:"sha256"`
	Name           string     `json:"name"`
	Size           int64      `json:"size"`
	Checksum       uint32     `json:"checksum"`
	NumFiles       int        `json:"numFiles"`
	Public         bool       `json:"public"`
	Status         string     `json:"status"`
	Error          string     `json:"error,omitempty"`
	ManifestSHA256 string     `json:"manifestSha256,omitempty"`
	CreatedAt      time.Time  `json:"createdAt"`
	IngestedAt     *time.Time `json:"ingestedAt,omitempty"`
}

// PakEntry is a row of pak_entries (one pak directory entry).
type PakEntry struct {
	Idx     int    `json:"idx"`
	Name    string `json:"name"`
	FilePos int32  `json:"filepos"`
	FileLen int32  `json:"filelen"`
	SHA256  string `json:"sha256"`
	Kind    string `json:"kind"`
}

// PakAsset is a row of pak_assets.
type PakAsset struct {
	SHA256 string
	Role   string
}

// MapRow is a row of maps.
type MapRow struct {
	PakID    int64           `json:"pakId"`
	Path     string          `json:"path"`
	Name     string          `json:"name"`
	SHA256   string          `json:"sha256"`
	Checksum uint32          `json:"checksum"`
	Message  string          `json:"message"`
	Sky      string          `json:"sky"`
	Info     json.RawMessage `json:"info,omitempty"`
}

// PakIngest is what FinishPakIngest records atomically.
type PakIngest struct {
	Checksum       uint32
	NumFiles       int
	ManifestSHA256 string
	Blobs          []Blob
	Entries        []PakEntry
	Assets         []PakAsset
	Maps           []MapRow
}

// Pakset is a row of paksets plus its ordered pak ids (lowest priority
// first). OwnerID 0 is the system (e.g. the public "demo" pakset).
type Pakset struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	OwnerID   int64     `json:"ownerId"`
	Public    bool      `json:"public"`
	PakIDs    []int64   `json:"pakIds"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Job statuses.
const (
	JobQueued  = "queued"
	JobRunning = "running"
	JobDone    = "done"
	JobFailed  = "failed"
)

// Job is a row of jobs.
type Job struct {
	ID         int64      `json:"id"`
	Kind       string     `json:"kind"`
	Status     string     `json:"status"`
	PakID      int64      `json:"pakId"`
	UserID     int64      `json:"userId"`
	Progress   float32    `json:"progress"`
	Error      string     `json:"error,omitempty"`
	CreatedAt  time.Time  `json:"createdAt"`
	StartedAt  *time.Time `json:"startedAt,omitempty"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
}

// SaveMeta is the descriptive part of a save slot.
type SaveMeta struct {
	Comment string `json:"comment"`
	MapCmd  string `json:"mapcmd"`
	Mode    string `json:"mode"`
	Schema  int    `json:"schema"`
}

// SaveInfo describes a stored save (without its blob).
type SaveInfo struct {
	Slot string `json:"slot"`
	SaveMeta
	Size      int       `json:"size"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Game is a row of games.
type Game struct {
	ID        string          `json:"id"`
	OwnerID   int64           `json:"ownerId"`
	Mode      string          `json:"mode"`
	Map       string          `json:"map"`
	PaksetID  string          `json:"pakset"`
	Settings  json.RawMessage `json:"settings,omitempty"`
	Public    bool            `json:"public"`
	StartedAt time.Time       `json:"startedAt"`
	EndedAt   *time.Time      `json:"endedAt,omitempty"`
}

// Ban is a row of bans (UserID 0 = IP ban).
type Ban struct {
	ID        int64      `json:"id"`
	UserID    int64      `json:"userId"`
	IP        string     `json:"ip"`
	Reason    string     `json:"reason"`
	CreatedBy int64      `json:"createdBy"`
	CreatedAt time.Time  `json:"createdAt"`
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
}

// SaveStore is the savegame persistence the game server uses (slot names
// are the C save directory names, e.g. "save0", "current"). Get/Delete
// return ErrNotFound for unknown slots.
type SaveStore interface {
	Put(ctx context.Context, userID int64, slot string, blob []byte, meta SaveMeta) error
	Get(ctx context.Context, userID int64, slot string) ([]byte, SaveInfo, error)
	List(ctx context.Context, userID int64) ([]SaveInfo, error)
	Delete(ctx context.Context, userID int64, slot string) error
}

// Repo is the complete persistence interface.
type Repo interface {
	Ping(ctx context.Context) error
	Close()

	CreateUser(ctx context.Context, u User) (User, error)
	UserByEmail(ctx context.Context, email string) (User, error)
	UserByID(ctx context.Context, id int64) (User, error)

	CreateSession(ctx context.Context, s Session) error
	SessionUser(ctx context.Context, id string, now time.Time) (Session, User, error)
	DeleteSession(ctx context.Context, id string) error
	DeleteExpiredSessions(ctx context.Context, now time.Time) (int64, error)

	UpsertBlobs(ctx context.Context, blobs []Blob) error
	BlobBySHA(ctx context.Context, sha string) (Blob, error)
	// AssetAccess returns the blob and whether userID (0 = anonymous) may
	// read it: it belongs to a public pak, or to a pak the user owns
	// (ADR-0005: same content hash).
	AssetAccess(ctx context.Context, sha string, userID int64) (Blob, bool, error)

	// CreatePak inserts a pak or returns the existing row with the same
	// sha256 (created=false).
	CreatePak(ctx context.Context, p Pak) (Pak, bool, error)
	PakByID(ctx context.Context, id int64) (Pak, error)
	PakBySHA(ctx context.Context, sha string) (Pak, error)
	AddPakOwner(ctx context.Context, pakID, userID int64, filename string) error
	PakOwned(ctx context.Context, pakID, userID int64) (bool, error)
	// ListPaks returns the paks the user owns plus public paks.
	ListPaks(ctx context.Context, userID int64) ([]Pak, error)
	SetPakStatus(ctx context.Context, id int64, status, errMsg string) error
	SetPakPublic(ctx context.Context, id int64, public bool) error
	FinishPakIngest(ctx context.Context, id int64, r PakIngest) error
	PakEntries(ctx context.Context, pakID int64) ([]PakEntry, error)

	CreatePakset(ctx context.Context, ps Pakset) (Pakset, error)
	// UpsertPakset creates or replaces (name, public, paks) a pakset.
	UpsertPakset(ctx context.Context, ps Pakset) (Pakset, error)
	PaksetByID(ctx context.Context, id string) (Pakset, error)
	ListPaksets(ctx context.Context, userID int64) ([]Pakset, error)
	UpdatePakset(ctx context.Context, ps Pakset) (Pakset, error)
	DeletePakset(ctx context.Context, id string) error

	MapsForPaks(ctx context.Context, pakIDs []int64) ([]MapRow, error)

	CreateJob(ctx context.Context, j Job) (Job, error)
	UpdateJob(ctx context.Context, id int64, status string, progress float32, errMsg string) error
	JobByID(ctx context.Context, id int64) (Job, error)
	JobsForPak(ctx context.Context, pakID int64) ([]Job, error)

	PutSave(ctx context.Context, userID int64, slot string, blob []byte, meta SaveMeta) error
	GetSave(ctx context.Context, userID int64, slot string) ([]byte, SaveInfo, error)
	ListSaves(ctx context.Context, userID int64) ([]SaveInfo, error)
	DeleteSave(ctx context.Context, userID int64, slot string) error

	GetSettings(ctx context.Context, userID int64) (string, time.Time, error)
	PutSettings(ctx context.Context, userID int64, config string) error

	InsertGame(ctx context.Context, g Game) error
	EndGame(ctx context.Context, id string, at time.Time) error
	ListGames(ctx context.Context, activeOnly bool) ([]Game, error)

	CreateBan(ctx context.Context, b Ban) (Ban, error)
	// ActiveBan returns the first unexpired ban matching the user or IP.
	ActiveBan(ctx context.Context, userID int64, ip string, now time.Time) (*Ban, error)
}

// NewSaveStore adapts a Repo to SaveStore.
func NewSaveStore(r Repo) SaveStore { return saveStore{r} }

type saveStore struct{ r Repo }

func (s saveStore) Put(ctx context.Context, userID int64, slot string, blob []byte, meta SaveMeta) error {
	return s.r.PutSave(ctx, userID, slot, blob, meta)
}
func (s saveStore) Get(ctx context.Context, userID int64, slot string) ([]byte, SaveInfo, error) {
	return s.r.GetSave(ctx, userID, slot)
}
func (s saveStore) List(ctx context.Context, userID int64) ([]SaveInfo, error) {
	return s.r.ListSaves(ctx, userID)
}
func (s saveStore) Delete(ctx context.Context, userID int64, slot string) error {
	return s.r.DeleteSave(ctx, userID, slot)
}
