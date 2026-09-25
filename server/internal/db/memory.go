package db

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"sync"
	"time"
)

// Memory is an in-memory Repo with the same semantics as Postgres. It is
// used by tests and by servers started without DATABASE_URL (data is lost on
// restart).
type Memory struct {
	mu        sync.Mutex
	now       func() time.Time
	nextID    int64
	users     map[int64]User
	sessions  map[string]Session
	blobs     map[string]Blob
	paks      map[int64]Pak
	pakOwners map[int64]map[int64]string
	entries   map[int64][]PakEntry
	assets    map[int64]map[string]string
	maps      map[int64][]MapRow
	paksets   map[string]Pakset
	jobs      map[int64]Job
	saves     map[int64]map[string]memSave
	settings  map[int64]memSettings
	games     map[string]Game
	bans      []Ban
}

type memSave struct {
	info SaveInfo
	blob []byte
}

type memSettings struct {
	config string
	at     time.Time
}

// NewMemory returns an empty in-memory repository.
func NewMemory() *Memory {
	return &Memory{
		now:       time.Now,
		users:     map[int64]User{},
		sessions:  map[string]Session{},
		blobs:     map[string]Blob{},
		paks:      map[int64]Pak{},
		pakOwners: map[int64]map[int64]string{},
		entries:   map[int64][]PakEntry{},
		assets:    map[int64]map[string]string{},
		maps:      map[int64][]MapRow{},
		paksets:   map[string]Pakset{},
		jobs:      map[int64]Job{},
		saves:     map[int64]map[string]memSave{},
		settings:  map[int64]memSettings{},
		games:     map[string]Game{},
	}
}

func (m *Memory) id() int64 { m.nextID++; return m.nextID }

// Ping implements Repo.
func (m *Memory) Ping(context.Context) error { return nil }

// Close implements Repo.
func (m *Memory) Close() {}

// CreateUser implements Repo.
func (m *Memory) CreateUser(_ context.Context, u User) (User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, x := range m.users {
		if strings.EqualFold(x.Email, u.Email) {
			return User{}, ErrConflict
		}
	}
	u.ID = m.id()
	u.CreatedAt = m.now()
	m.users[u.ID] = u
	return u, nil
}

// UserByEmail implements Repo.
func (m *Memory) UserByEmail(_ context.Context, email string) (User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, x := range m.users {
		if strings.EqualFold(x.Email, email) {
			return x, nil
		}
	}
	return User{}, ErrNotFound
}

// UserByID implements Repo.
func (m *Memory) UserByID(_ context.Context, id int64) (User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.users[id]
	if !ok {
		return User{}, ErrNotFound
	}
	return u, nil
}

// CreateSession implements Repo.
func (m *Memory) CreateSession(_ context.Context, s Session) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.users[s.UserID]; !ok {
		return ErrConflict
	}
	if _, ok := m.sessions[s.ID]; ok {
		return ErrConflict
	}
	m.sessions[s.ID] = s
	return nil
}

// SessionUser implements Repo.
func (m *Memory) SessionUser(_ context.Context, id string, now time.Time) (Session, User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[id]
	if !ok || !s.ExpiresAt.After(now) {
		return Session{}, User{}, ErrNotFound
	}
	u, ok := m.users[s.UserID]
	if !ok {
		return Session{}, User{}, ErrNotFound
	}
	return s, u, nil
}

// DeleteSession implements Repo.
func (m *Memory) DeleteSession(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, id)
	return nil
}

// DeleteExpiredSessions implements Repo.
func (m *Memory) DeleteExpiredSessions(_ context.Context, now time.Time) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var n int64
	for id, s := range m.sessions {
		if !s.ExpiresAt.After(now) {
			delete(m.sessions, id)
			n++
		}
	}
	return n, nil
}

// UpsertBlobs implements Repo.
func (m *Memory) UpsertBlobs(_ context.Context, blobs []Blob) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, b := range blobs {
		if _, ok := m.blobs[b.SHA256]; !ok {
			m.blobs[b.SHA256] = b
		}
	}
	return nil
}

// BlobBySHA implements Repo.
func (m *Memory) BlobBySHA(_ context.Context, sha string) (Blob, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.blobs[sha]
	if !ok {
		return Blob{}, ErrNotFound
	}
	return b, nil
}

// AssetAccess implements Repo.
func (m *Memory) AssetAccess(_ context.Context, sha string, userID int64) (Blob, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.blobs[sha]
	if !ok {
		return Blob{}, false, ErrNotFound
	}
	for pid, set := range m.assets {
		if _, ok := set[sha]; !ok {
			continue
		}
		k := m.paks[pid]
		if k.Status != PakReady {
			continue
		}
		if k.Public {
			return b, true, nil
		}
		if _, ok := m.pakOwners[pid][userID]; ok && userID != 0 {
			return b, true, nil
		}
	}
	return b, false, nil
}

// CreatePak implements Repo.
func (m *Memory) CreatePak(_ context.Context, k Pak) (Pak, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, x := range m.paks {
		if x.SHA256 == k.SHA256 {
			return x, false, nil
		}
	}
	if k.Status == "" {
		k.Status = PakPending
	}
	k.ID = m.id()
	k.CreatedAt = m.now()
	m.paks[k.ID] = k
	return k, true, nil
}

// PakByID implements Repo.
func (m *Memory) PakByID(_ context.Context, id int64) (Pak, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	k, ok := m.paks[id]
	if !ok {
		return Pak{}, ErrNotFound
	}
	return k, nil
}

// PakBySHA implements Repo.
func (m *Memory) PakBySHA(_ context.Context, sha string) (Pak, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, x := range m.paks {
		if x.SHA256 == sha {
			return x, nil
		}
	}
	return Pak{}, ErrNotFound
}

// AddPakOwner implements Repo.
func (m *Memory) AddPakOwner(_ context.Context, pakID, userID int64, filename string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.paks[pakID]; !ok {
		return ErrConflict
	}
	if _, ok := m.users[userID]; !ok {
		return ErrConflict
	}
	if m.pakOwners[pakID] == nil {
		m.pakOwners[pakID] = map[int64]string{}
	}
	if _, ok := m.pakOwners[pakID][userID]; !ok {
		m.pakOwners[pakID][userID] = filename
	}
	return nil
}

// PakOwned implements Repo.
func (m *Memory) PakOwned(_ context.Context, pakID, userID int64) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.pakOwners[pakID][userID]
	return ok, nil
}

// ListPaks implements Repo.
func (m *Memory) ListPaks(_ context.Context, userID int64) ([]Pak, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Pak
	for id, k := range m.paks {
		_, owned := m.pakOwners[id][userID]
		if k.Public || owned {
			out = append(out, k)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// SetPakStatus implements Repo.
func (m *Memory) SetPakStatus(_ context.Context, id int64, status, errMsg string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	k, ok := m.paks[id]
	if !ok {
		return ErrNotFound
	}
	k.Status, k.Error = status, errMsg
	m.paks[id] = k
	return nil
}

// SetPakPublic implements Repo.
func (m *Memory) SetPakPublic(_ context.Context, id int64, public bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	k, ok := m.paks[id]
	if !ok {
		return ErrNotFound
	}
	k.Public = public
	m.paks[id] = k
	return nil
}

// FinishPakIngest implements Repo.
func (m *Memory) FinishPakIngest(_ context.Context, id int64, r PakIngest) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	k, ok := m.paks[id]
	if !ok {
		return ErrNotFound
	}
	for _, b := range r.Blobs {
		if _, ok := m.blobs[b.SHA256]; !ok {
			m.blobs[b.SHA256] = b
		}
	}
	m.entries[id] = append([]PakEntry(nil), r.Entries...)
	set := map[string]string{}
	for _, a := range r.Assets {
		if _, ok := set[a.SHA256]; !ok {
			set[a.SHA256] = a.Role
		}
	}
	m.assets[id] = set
	var maps []MapRow
	seen := map[string]bool{}
	for _, mr := range r.Maps {
		if seen[mr.Path] {
			continue
		}
		seen[mr.Path] = true
		mr.PakID = id
		if len(mr.Info) == 0 {
			mr.Info = json.RawMessage(`{}`)
		}
		maps = append(maps, mr)
	}
	m.maps[id] = maps
	now := m.now()
	k.Checksum, k.NumFiles, k.ManifestSHA256 = r.Checksum, r.NumFiles, r.ManifestSHA256
	k.Status, k.Error, k.IngestedAt = PakReady, "", &now
	m.paks[id] = k
	return nil
}

// PakEntries implements Repo.
func (m *Memory) PakEntries(_ context.Context, pakID int64) ([]PakEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]PakEntry(nil), m.entries[pakID]...), nil
}

func (m *Memory) checkPaks(ids []int64) error {
	for _, id := range ids {
		if _, ok := m.paks[id]; !ok {
			return ErrConflict
		}
	}
	return nil
}

func clonePakset(ps Pakset) Pakset {
	ps.PakIDs = append([]int64{}, ps.PakIDs...)
	return ps
}

// CreatePakset implements Repo.
func (m *Memory) CreatePakset(_ context.Context, ps Pakset) (Pakset, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.paksets[ps.ID]; ok {
		return Pakset{}, ErrConflict
	}
	if err := m.checkPaks(ps.PakIDs); err != nil {
		return Pakset{}, err
	}
	ps.CreatedAt = m.now()
	ps.UpdatedAt = ps.CreatedAt
	ps = clonePakset(ps)
	m.paksets[ps.ID] = ps
	return clonePakset(ps), nil
}

// UpsertPakset implements Repo.
func (m *Memory) UpsertPakset(_ context.Context, ps Pakset) (Pakset, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.checkPaks(ps.PakIDs); err != nil {
		return Pakset{}, err
	}
	now := m.now()
	if old, ok := m.paksets[ps.ID]; ok {
		ps.CreatedAt = old.CreatedAt
	} else {
		ps.CreatedAt = now
	}
	ps.UpdatedAt = now
	ps = clonePakset(ps)
	m.paksets[ps.ID] = ps
	return clonePakset(ps), nil
}

// PaksetByID implements Repo.
func (m *Memory) PaksetByID(_ context.Context, id string) (Pakset, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	ps, ok := m.paksets[id]
	if !ok {
		return Pakset{}, ErrNotFound
	}
	return clonePakset(ps), nil
}

// ListPaksets implements Repo.
func (m *Memory) ListPaksets(_ context.Context, userID int64) ([]Pakset, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Pakset
	for _, ps := range m.paksets {
		if ps.Public || (userID != 0 && ps.OwnerID == userID) {
			out = append(out, clonePakset(ps))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.Before(out[j].CreatedAt)
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

// UpdatePakset implements Repo.
func (m *Memory) UpdatePakset(_ context.Context, ps Pakset) (Pakset, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	old, ok := m.paksets[ps.ID]
	if !ok {
		return Pakset{}, ErrNotFound
	}
	if err := m.checkPaks(ps.PakIDs); err != nil {
		return Pakset{}, err
	}
	old.Name, old.Public, old.PakIDs = ps.Name, ps.Public, append([]int64{}, ps.PakIDs...)
	old.UpdatedAt = m.now()
	m.paksets[ps.ID] = old
	return clonePakset(old), nil
}

// DeletePakset implements Repo.
func (m *Memory) DeletePakset(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.paksets[id]; !ok {
		return ErrNotFound
	}
	delete(m.paksets, id)
	return nil
}

// MapsForPaks implements Repo.
func (m *Memory) MapsForPaks(_ context.Context, pakIDs []int64) ([]MapRow, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []MapRow
	for _, id := range pakIDs {
		out = append(out, m.maps[id]...)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].PakID < out[j].PakID
	})
	return out, nil
}

// CreateJob implements Repo.
func (m *Memory) CreateJob(_ context.Context, j Job) (Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if j.Status == "" {
		j.Status = JobQueued
	}
	j.ID = m.id()
	j.CreatedAt = m.now()
	m.jobs[j.ID] = j
	return j, nil
}

// UpdateJob implements Repo.
func (m *Memory) UpdateJob(_ context.Context, id int64, status string, progress float32, errMsg string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.jobs[id]
	if !ok {
		return ErrNotFound
	}
	now := m.now()
	j.Status, j.Progress, j.Error = status, progress, errMsg
	if status == JobRunning && j.StartedAt == nil {
		j.StartedAt = &now
	}
	if status == JobDone || status == JobFailed {
		j.FinishedAt = &now
	}
	m.jobs[id] = j
	return nil
}

// JobByID implements Repo.
func (m *Memory) JobByID(_ context.Context, id int64) (Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.jobs[id]
	if !ok {
		return Job{}, ErrNotFound
	}
	return j, nil
}

// JobsForPak implements Repo.
func (m *Memory) JobsForPak(_ context.Context, pakID int64) ([]Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Job
	for _, j := range m.jobs {
		if j.PakID == pakID {
			out = append(out, j)
		}
	}
	sort.Slice(out, func(a, b int) bool { return out[a].ID < out[b].ID })
	return out, nil
}

// PutSave implements Repo.
func (m *Memory) PutSave(_ context.Context, userID int64, slot string, blob []byte, meta SaveMeta) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.users[userID]; !ok {
		return ErrConflict
	}
	if m.saves[userID] == nil {
		m.saves[userID] = map[string]memSave{}
	}
	now := m.now()
	s, ok := m.saves[userID][slot]
	if !ok {
		s.info.CreatedAt = now
	}
	s.info.Slot, s.info.SaveMeta, s.info.Size, s.info.UpdatedAt = slot, meta, len(blob), now
	s.blob = append([]byte(nil), blob...)
	m.saves[userID][slot] = s
	return nil
}

// GetSave implements Repo.
func (m *Memory) GetSave(_ context.Context, userID int64, slot string) ([]byte, SaveInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.saves[userID][slot]
	if !ok {
		return nil, SaveInfo{}, ErrNotFound
	}
	return append([]byte(nil), s.blob...), s.info, nil
}

// ListSaves implements Repo.
func (m *Memory) ListSaves(_ context.Context, userID int64) ([]SaveInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []SaveInfo{}
	for _, s := range m.saves[userID] {
		out = append(out, s.info)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Slot < out[j].Slot })
	return out, nil
}

// DeleteSave implements Repo.
func (m *Memory) DeleteSave(_ context.Context, userID int64, slot string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.saves[userID][slot]; !ok {
		return ErrNotFound
	}
	delete(m.saves[userID], slot)
	return nil
}

// GetSettings implements Repo.
func (m *Memory) GetSettings(_ context.Context, userID int64) (string, time.Time, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.settings[userID]
	if !ok {
		return "", time.Time{}, ErrNotFound
	}
	return s.config, s.at, nil
}

// PutSettings implements Repo.
func (m *Memory) PutSettings(_ context.Context, userID int64, config string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.users[userID]; !ok {
		return ErrConflict
	}
	m.settings[userID] = memSettings{config, m.now()}
	return nil
}

// InsertGame implements Repo.
func (m *Memory) InsertGame(_ context.Context, g Game) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.games[g.ID]; ok {
		return ErrConflict
	}
	if g.StartedAt.IsZero() {
		g.StartedAt = m.now()
	}
	if len(g.Settings) == 0 {
		g.Settings = json.RawMessage(`{}`)
	}
	m.games[g.ID] = g
	return nil
}

// EndGame implements Repo.
func (m *Memory) EndGame(_ context.Context, id string, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	g, ok := m.games[id]
	if !ok || g.EndedAt != nil {
		return ErrNotFound
	}
	g.EndedAt = &at
	m.games[id] = g
	return nil
}

// ListGames implements Repo.
func (m *Memory) ListGames(_ context.Context, activeOnly bool) ([]Game, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Game
	for _, g := range m.games {
		if activeOnly && g.EndedAt != nil {
			continue
		}
		out = append(out, g)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].StartedAt.Equal(out[j].StartedAt) {
			return out[i].StartedAt.Before(out[j].StartedAt)
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

// CreateBan implements Repo.
func (m *Memory) CreateBan(_ context.Context, b Ban) (Ban, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if b.UserID == 0 && b.IP == "" {
		return Ban{}, ErrConflict
	}
	b.ID = m.id()
	b.CreatedAt = m.now()
	m.bans = append(m.bans, b)
	return b, nil
}

// ActiveBan implements Repo.
func (m *Memory) ActiveBan(_ context.Context, userID int64, ip string, now time.Time) (*Ban, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	ip = strings.TrimSpace(ip)
	for _, b := range m.bans {
		if !((userID != 0 && b.UserID == userID) || (ip != "" && b.IP == ip)) {
			continue
		}
		if b.ExpiresAt != nil && !b.ExpiresAt.After(now) {
			continue
		}
		bb := b
		return &bb, nil
	}
	return nil, nil
}

var _ Repo = (*Memory)(nil)
