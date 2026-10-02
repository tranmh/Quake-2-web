package db

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"quake2web/server/internal/db/dbtest"
)

func sha(c byte) string { return strings.Repeat(string(c), 64) }

// runRepoSuite exercises every Repo method; it must pass on Memory and on
// Postgres alike.
func runRepoSuite(t *testing.T, r Repo) {
	ctx := context.Background()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := r.Ping(ctx); err != nil {
		t.Fatal(err)
	}

	// users
	u, err := r.CreateUser(ctx, User{Email: "A@x.io", DisplayName: "A", PasswordHash: "h"})
	must(err)
	if _, err := r.CreateUser(ctx, User{Email: "a@X.io", DisplayName: "dup", PasswordHash: "h"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate email: %v", err)
	}
	u2, err := r.CreateUser(ctx, User{Email: "b@x.io", DisplayName: "B", PasswordHash: "h"})
	must(err)
	if got, err := r.UserByEmail(ctx, "a@x.IO"); err != nil || got.ID != u.ID {
		t.Fatal(err)
	}
	if _, err := r.UserByID(ctx, 999999); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}

	// sessions
	now := time.Now().UTC().Truncate(time.Microsecond)
	must(r.CreateSession(ctx, Session{ID: "s1", UserID: u.ID, CreatedAt: now, ExpiresAt: now.Add(time.Hour)}))
	must(r.CreateSession(ctx, Session{ID: "s2", UserID: u.ID, CreatedAt: now, ExpiresAt: now.Add(-time.Hour)}))
	if s, su, err := r.SessionUser(ctx, "s1", now); err != nil || s.UserID != u.ID || su.Email != "A@x.io" {
		t.Fatal(err)
	}
	if _, _, err := r.SessionUser(ctx, "s2", now); !errors.Is(err, ErrNotFound) {
		t.Fatal("expired session returned")
	}
	if n, err := r.DeleteExpiredSessions(ctx, now); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	must(r.DeleteSession(ctx, "s1"))
	if _, _, err := r.SessionUser(ctx, "s1", now); !errors.Is(err, ErrNotFound) {
		t.Fatal("deleted session returned")
	}

	// paks, ingest, entitlement
	pub, created, err := r.CreatePak(ctx, Pak{SHA256: sha('a'), Name: "pak0.pak", Size: 10, Public: true})
	must(err)
	if !created || pub.Status != PakPending {
		t.Fatalf("%+v", pub)
	}
	again, created, err := r.CreatePak(ctx, Pak{SHA256: sha('a'), Name: "other.pak", Size: 10})
	must(err)
	if created || again.ID != pub.ID {
		t.Fatal("pak not deduplicated by sha")
	}
	priv, _, err := r.CreatePak(ctx, Pak{SHA256: sha('b'), Name: "pak1.pak", Size: 20})
	must(err)
	must(r.AddPakOwner(ctx, priv.ID, u.ID, "pak1.pak"))
	must(r.AddPakOwner(ctx, priv.ID, u.ID, "again.pak")) // idempotent
	if ok, _ := r.PakOwned(ctx, priv.ID, u.ID); !ok {
		t.Fatal("owner")
	}
	if ok, _ := r.PakOwned(ctx, priv.ID, u2.ID); ok {
		t.Fatal("not owner")
	}
	must(r.SetPakStatus(ctx, priv.ID, PakIngesting, ""))
	info, _ := json.Marshal(map[string]any{"name": "m1"})
	for _, p := range []Pak{pub, priv} {
		c := p.SHA256[0]
		must(r.FinishPakIngest(ctx, p.ID, PakIngest{
			Checksum: 0xdeadbeef, NumFiles: 2, ManifestSHA256: sha('e'),
			Blobs:   []Blob{{sha(c + 2), 5, "image/png"}, {sha('5'), 3, "application/octet-stream"}, {sha('e'), 9, "application/json"}},
			Entries: []PakEntry{{0, "maps/m1.bsp", 12, 5, sha(c + 2), "bsp"}, {1, "shared.wav", 17, 3, sha('5'), "wav"}},
			Assets:  []PakAsset{{sha(c + 2), "raw"}, {sha('5'), "raw"}, {sha('5'), "raw"}, {sha('e'), "manifest"}},
			Maps:    []MapRow{{Path: "maps/m1.bsp", Name: "m1", SHA256: sha(c + 2), Checksum: 0xfffffffe, Message: "M", Sky: "unit1_", Info: info}},
		}))
	}
	// re-ingest replaces rows
	must(r.FinishPakIngest(ctx, priv.ID, PakIngest{Checksum: 1, NumFiles: 2, ManifestSHA256: sha('e'),
		Blobs:   []Blob{{sha('f'), 5, "image/png"}, {sha('5'), 3, "x"}},
		Entries: []PakEntry{{0, "maps/m1.bsp", 12, 5, sha('f'), "bsp"}, {1, "shared.wav", 17, 3, sha('5'), "wav"}},
		Assets:  []PakAsset{{sha('f'), "raw"}, {sha('5'), "raw"}},
		Maps:    []MapRow{{Path: "maps/m1.bsp", Name: "m1", SHA256: sha('f'), Checksum: 7}},
	}))
	p, err := r.PakByID(ctx, priv.ID)
	must(err)
	if p.Status != PakReady || p.Checksum != 1 || p.NumFiles != 2 || p.IngestedAt == nil {
		t.Fatalf("%+v", p)
	}
	if p2, err := r.PakBySHA(ctx, sha('b')); err != nil || p2.ID != priv.ID {
		t.Fatal(err)
	}
	ents, err := r.PakEntries(ctx, priv.ID)
	must(err)
	if len(ents) != 2 || ents[0].SHA256 != sha('f') || ents[1].FilePos != 17 {
		t.Fatalf("%+v", ents)
	}
	if b, err := r.BlobBySHA(ctx, sha('5')); err != nil || b.Size != 3 || b.ContentType != "application/octet-stream" {
		t.Fatal(b, err) // first content type wins
	}
	check := func(s string, uid int64, want bool) {
		t.Helper()
		_, ok, err := r.AssetAccess(ctx, s, uid)
		if err != nil || ok != want {
			t.Fatalf("AssetAccess(%s, %d) = %v, %v; want %v", s[:1], uid, ok, err, want)
		}
	}
	check(sha('c'), 0, true)  // public pak asset
	check(sha('5'), 0, true)  // in both paks
	check(sha('f'), 0, false) // private
	check(sha('f'), u2.ID, false)
	check(sha('f'), u.ID, true)
	if _, _, err := r.AssetAccess(ctx, sha('9'), 0); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	list, err := r.ListPaks(ctx, u2.ID)
	must(err)
	if len(list) != 1 || list[0].ID != pub.ID {
		t.Fatalf("u2 sees %d paks", len(list))
	}
	list, _ = r.ListPaks(ctx, u.ID)
	if len(list) != 2 {
		t.Fatal(len(list))
	}
	maps, err := r.MapsForPaks(ctx, []int64{pub.ID, priv.ID})
	must(err)
	if len(maps) != 2 || maps[0].PakID != pub.ID || maps[0].Checksum != 0xfffffffe || maps[1].Checksum != 7 {
		t.Fatalf("%+v", maps)
	}
	must(r.SetPakPublic(ctx, priv.ID, true))
	check(sha('f'), 0, true)
	must(r.SetPakPublic(ctx, priv.ID, false))
	if err := r.SetPakStatus(ctx, 999999, PakFailed, "x"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}

	// paksets
	ps, err := r.CreatePakset(ctx, Pakset{ID: "set1", Name: "Set", OwnerID: u.ID, PakIDs: []int64{pub.ID, priv.ID}})
	must(err)
	if len(ps.PakIDs) != 2 {
		t.Fatal(ps)
	}
	if _, err := r.CreatePakset(ctx, Pakset{ID: "set1", Name: "dup", OwnerID: u.ID}); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	if _, err := r.CreatePakset(ctx, Pakset{ID: "bad", Name: "x", PakIDs: []int64{999999}}); !errors.Is(err, ErrConflict) {
		t.Fatal("unknown pak accepted", err)
	}
	demo, err := r.UpsertPakset(ctx, Pakset{ID: "demo", Name: "Demo", Public: true, PakIDs: []int64{pub.ID}})
	must(err)
	demo, err = r.UpsertPakset(ctx, Pakset{ID: "demo", Name: "Demo 2", Public: true, PakIDs: []int64{pub.ID}})
	must(err)
	if got, err := r.PaksetByID(ctx, "demo"); err != nil || got.Name != "Demo 2" || got.OwnerID != 0 || len(got.PakIDs) != 1 {
		t.Fatal(got, err)
	}
	sets, _ := r.ListPaksets(ctx, u2.ID)
	if len(sets) != 1 || sets[0].ID != "demo" {
		t.Fatalf("u2 sees %+v", sets)
	}
	sets, _ = r.ListPaksets(ctx, u.ID)
	if len(sets) != 2 {
		t.Fatal(len(sets))
	}
	ps.Name, ps.PakIDs = "Renamed", []int64{priv.ID}
	if got, err := r.UpdatePakset(ctx, ps); err != nil || got.Name != "Renamed" || len(got.PakIDs) != 1 {
		t.Fatal(got, err)
	}
	must(r.DeletePakset(ctx, "set1"))
	if err := r.DeletePakset(ctx, "set1"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	_ = demo

	// jobs
	j, err := r.CreateJob(ctx, Job{Kind: "ingest", PakID: priv.ID, UserID: u.ID})
	must(err)
	must(r.UpdateJob(ctx, j.ID, JobRunning, 0.5, ""))
	must(r.UpdateJob(ctx, j.ID, JobDone, 1, ""))
	j, err = r.JobByID(ctx, j.ID)
	must(err)
	if j.Status != JobDone || j.Progress != 1 || j.StartedAt == nil || j.FinishedAt == nil {
		t.Fatalf("%+v", j)
	}
	if js, _ := r.JobsForPak(ctx, priv.ID); len(js) != 1 {
		t.Fatal(js)
	}

	// saves via the SaveStore adapter
	ss := NewSaveStore(r)
	must(ss.Put(ctx, u.ID, "save0", []byte{1, 2, 3}, SaveMeta{Comment: "base1", MapCmd: "base1", Mode: "sp", Schema: 1}))
	must(ss.Put(ctx, u.ID, "current", []byte{9}, SaveMeta{}))
	must(ss.Put(ctx, u.ID, "save0", []byte{4, 5}, SaveMeta{Comment: "base2", Schema: 2}))
	blob, si, err := ss.Get(ctx, u.ID, "save0")
	must(err)
	if string(blob) != "\x04\x05" || si.Comment != "base2" || si.Size != 2 || si.Schema != 2 || si.CreatedAt.IsZero() {
		t.Fatalf("%+v %v", si, blob)
	}
	saves, _ := ss.List(ctx, u.ID)
	if len(saves) != 2 || saves[0].Slot != "current" {
		t.Fatalf("%+v", saves)
	}
	if other, _ := ss.List(ctx, u2.ID); len(other) != 0 {
		t.Fatal("saves leaked across users")
	}
	must(ss.Delete(ctx, u.ID, "save0"))
	if err := ss.Delete(ctx, u.ID, "save0"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if _, _, err := ss.Get(ctx, u.ID, "save0"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}

	// settings
	if _, _, err := r.GetSettings(ctx, u.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	must(r.PutSettings(ctx, u.ID, "bind w +forward\n"))
	must(r.PutSettings(ctx, u.ID, "bind w +back\n"))
	if cfg, at, err := r.GetSettings(ctx, u.ID); err != nil || cfg != "bind w +back\n" || at.IsZero() {
		t.Fatal(cfg, err)
	}

	// games
	must(r.InsertGame(ctx, Game{ID: "g1", OwnerID: u.ID, Mode: "dm", Map: "demo1", PaksetID: "demo", Public: true}))
	must(r.InsertGame(ctx, Game{ID: "g2", Mode: "sp", Map: "demo2", Settings: json.RawMessage(`{"skill":"1"}`)}))
	if err := r.InsertGame(ctx, Game{ID: "g1", Mode: "dm", Map: "x"}); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	must(r.EndGame(ctx, "g1", time.Now()))
	if err := r.EndGame(ctx, "g1", time.Now()); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	if gs, _ := r.ListGames(ctx, true); len(gs) != 1 || gs[0].ID != "g2" {
		t.Fatalf("%+v", gs)
	}
	if gs, _ := r.ListGames(ctx, false); len(gs) != 2 {
		t.Fatal(gs)
	}

	// bans
	if b, err := r.ActiveBan(ctx, u2.ID, "9.9.9.9", now); err != nil || b != nil {
		t.Fatal(b, err)
	}
	past := now.Add(-time.Minute)
	_, err = r.CreateBan(ctx, Ban{IP: "9.9.9.9", Reason: "old", ExpiresAt: &past})
	must(err)
	if b, _ := r.ActiveBan(ctx, 0, "9.9.9.9", now); b != nil {
		t.Fatal("expired ban active")
	}
	_, err = r.CreateBan(ctx, Ban{UserID: u2.ID, Reason: "cheat", CreatedBy: u.ID})
	must(err)
	if b, err := r.ActiveBan(ctx, u2.ID, "", now); err != nil || b == nil || b.Reason != "cheat" || b.CreatedBy != u.ID {
		t.Fatal(b, err)
	}

	runBotSpendSuite(t, r)
}

// runBotSpendSuite checks the bots' daily spend: per day, additive,
// atomic under concurrent adds, and validated.
func runBotSpendSuite(t *testing.T, r Repo) {
	ctx := context.Background()
	if v, err := r.BotSpend(ctx, "2026-10-02"); err != nil || v != 0 {
		t.Fatalf("unknown day: %v %v", v, err)
	}
	if v, err := r.AddBotSpend(ctx, "2026-10-02", 0.125); err != nil || v != 0.125 {
		t.Fatalf("first add: %v %v", v, err)
	}
	if v, err := r.AddBotSpend(ctx, "2026-10-02", 0.25); err != nil || v != 0.375 {
		t.Fatalf("second add: %v %v", v, err)
	}
	if v, err := r.AddBotSpend(ctx, "2026-10-03", 0); err != nil || v != 0 {
		t.Fatalf("zero add: %v %v", v, err)
	}
	if v, err := r.BotSpend(ctx, "2026-10-02"); err != nil || v != 0.375 {
		t.Fatalf("day total: %v %v", v, err)
	}
	if v, err := r.BotSpend(ctx, "2026-10-01"); err != nil || v != 0 {
		t.Fatalf("other day: %v %v", v, err)
	}
	// concurrent adds (several q2server processes, or bots flushing at
	// once) never lose an update
	const n = 16
	errs := make(chan error, n)
	for i := 0; i < n; i++ {
		go func() { _, err := r.AddBotSpend(ctx, "2026-10-04", 0.5); errs <- err }()
	}
	for i := 0; i < n; i++ {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	if v, err := r.BotSpend(ctx, "2026-10-04"); err != nil || v != n*0.5 {
		t.Fatalf("concurrent total: %v %v", v, err)
	}
	for _, day := range []string{"", "2026-13-01", "2026-1-2", "02.10.2026", "2026-10-02; DROP TABLE users"} {
		if _, err := r.BotSpend(ctx, day); err == nil {
			t.Errorf("BotSpend(%q) accepted", day)
		}
		if _, err := r.AddBotSpend(ctx, day, 1); err == nil {
			t.Errorf("AddBotSpend(%q) accepted", day)
		}
	}
	for _, usd := range []float64{-0.01, math.NaN(), math.Inf(1)} {
		if _, err := r.AddBotSpend(ctx, "2026-10-02", usd); err == nil {
			t.Errorf("AddBotSpend(%v) accepted", usd)
		}
	}
	// the adapter is the agent/budget.SpendStore shape
	st := BotSpendStore(r)
	if v, err := st.AddSpend(ctx, "2026-10-02", 0.625); err != nil || v != 1 {
		t.Fatalf("store add: %v %v", v, err)
	}
	if v, err := st.LoadSpend(ctx, "2026-10-02"); err != nil || v != 1 {
		t.Fatalf("store load: %v %v", v, err)
	}
}

func TestMemoryRepo(t *testing.T) { runRepoSuite(t, NewMemory()) }

// TestPostgresRepo runs migrations up/down/up and the suite against
// DATABASE_URL_TEST (a throwaway database; it is wiped).
func TestPostgresRepo(t *testing.T) {
	url := dbtest.URL(t)
	ctx := context.Background()
	pg, err := Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pg.Close()
	if err := pg.MigrateDownAll(ctx); err != nil {
		t.Fatal("down (initial):", err)
	}
	if err := pg.Migrate(ctx); err != nil {
		t.Fatal("up:", err)
	}
	if v, err := pg.MigrationVersion(ctx); err != nil || v != 2 {
		t.Fatal(v, err)
	}
	if err := pg.MigrateDownAll(ctx); err != nil {
		t.Fatal("down:", err)
	}
	var n int
	if err := pg.Pool.QueryRow(ctx, `SELECT count(*) FROM information_schema.tables WHERE table_schema='public' AND table_name IN ('users', 'bot_spend')`).Scan(&n); err != nil || n != 0 {
		t.Fatal("tables survived down", n, err)
	}
	if err := pg.Migrate(ctx); err != nil {
		t.Fatal("up again:", err)
	}
	// concurrent migrate calls serialize on the advisory lock
	errs := make(chan error, 3)
	for i := 0; i < 3; i++ {
		go func() { errs <- pg.Migrate(ctx) }()
	}
	for i := 0; i < 3; i++ {
		if err := <-errs; err != nil {
			t.Fatal("concurrent migrate:", err)
		}
	}
	runRepoSuite(t, pg)
}
