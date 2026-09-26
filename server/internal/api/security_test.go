package api

import (
	"context"
	"net/http"
	"strconv"
	"testing"
	"time"

	"quake2web/server/internal/assets/blob"
	"quake2web/server/internal/db"
)

func waitPakStatus(t *testing.T, c *client, id int64, want string) db.Pak {
	t.Helper()
	var p db.Pak
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
		p = decode[pakResponse](t, c.do("GET", "/api/v1/paks/"+strconv.FormatInt(id, 10), nil)).Pak
		if p.Status == want {
			return p
		}
	}
	t.Fatalf("pak %d: status %q, want %q", id, p.Status, want)
	return p
}

// A pak whose ingest never finished (server restarted mid-ingest, or the
// ingest queue was full) stayed "pending"/"ingesting" forever: uploading the
// same bytes again answered 200 without queueing a job, so that content
// could never become usable by anyone (paks are unique by hash).
func TestReuploadRecoversStuckPak(t *testing.T) {
	repo := db.NewMemory()
	e := newEnv(t, repo)
	alice := e.client()
	alice.register("alice@example.com")
	ctx := context.Background()
	for _, stuck := range []string{db.PakPending, db.PakIngesting} {
		data := buildPak(map[string][]byte{"readme.txt": []byte("hello " + stuck)}, []string{"readme.txt"})
		// the leftover row of an interrupted ingest
		p, _, err := repo.CreatePak(ctx, db.Pak{SHA256: blob.Sum(data), Name: "x.pak", Size: int64(len(data))})
		if err != nil {
			t.Fatal(err)
		}
		if err := repo.SetPakStatus(ctx, p.ID, stuck, ""); err != nil {
			t.Fatal(err)
		}
		expect(t, alice.upload("x.pak", data), http.StatusAccepted)
		waitPakStatus(t, alice, p.ID, db.PakReady)
	}
}

// Uploads were unbounded per account: one user could fill the blob store's
// disk (1 GiB per upload, any number of uploads) and take the service down.
func TestUploadQuota(t *testing.T) {
	repo := db.NewMemory()
	e := newEnv(t, repo)
	e.deps.Config.MaxUserPaks = 2
	e.srv.Config.Handler = NewRouter(e.deps)
	alice := e.client()
	alice.register("alice@example.com")
	pakN := func(i int) []byte {
		return buildPak(map[string][]byte{"f.txt": []byte("content " + strconv.Itoa(i))}, []string{"f.txt"})
	}
	expect(t, alice.upload("1.pak", pakN(1)), http.StatusAccepted)
	expect(t, alice.upload("2.pak", pakN(2)), http.StatusAccepted)
	expect(t, alice.upload("3.pak", pakN(3)), http.StatusForbidden)
	// re-uploading a pak already owned is not new storage
	expect(t, alice.upload("1.pak", pakN(1)), http.StatusOK)
	// other accounts have their own quota
	bob := e.client()
	bob.register("bob@example.com")
	expect(t, bob.upload("3.pak", pakN(3)), http.StatusAccepted)

	// byte quota
	e.deps.Config.MaxUserPaks = 100
	e.deps.Config.MaxUserStorageBytes = int64(len(pakN(1))*2 + 10)
	e.srv.Config.Handler = NewRouter(e.deps)
	carol := e.client()
	carol.register("carol@example.com")
	expect(t, carol.upload("4.pak", pakN(4)), http.StatusAccepted)
	expect(t, carol.upload("5.pak", pakN(5)), http.StatusAccepted)
	expect(t, carol.upload("6.pak", pakN(6)), http.StatusForbidden)
}

// A ban only blocked login and join: sessions created before the ban could
// still upload paks and start games.
func TestBannedSessionCannotUploadOrCreateGames(t *testing.T) {
	repo := db.NewMemory()
	e := newEnv(t, repo)
	mallory := e.client()
	u := mallory.register("mallory@example.com")
	if _, err := repo.CreateBan(context.Background(), db.Ban{UserID: u.ID, Reason: "test"}); err != nil {
		t.Fatal(err)
	}
	data := buildPak(map[string][]byte{"f.txt": []byte("banned")}, []string{"f.txt"})
	expect(t, mallory.upload("b.pak", data), http.StatusForbidden)
	expect(t, mallory.do("POST", "/api/v1/games", map[string]any{"mode": "dm", "map": "demo1"}), http.StatusForbidden)
}
