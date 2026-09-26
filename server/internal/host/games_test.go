package host

import (
	"context"
	"errors"
	"io/fs"
	"strings"
	"testing"
	"time"

	"quake2web/server/internal/api"
	"quake2web/server/internal/assets/blob"
	"quake2web/server/internal/assets/manifest"
	"quake2web/server/internal/assets/pak"
	"quake2web/server/internal/auth"
	"quake2web/server/internal/db"
	"quake2web/server/internal/qcommon/crand"
	"quake2web/server/internal/sv"
	"quake2web/server/internal/sv/stubgame"
	"quake2web/server/internal/testutil"
)

func TestModeSettings(t *testing.T) {
	get := func(cv [][2]string, k string) string {
		v := ""
		for _, kv := range cv {
			if kv[0] == k {
				v = kv[1]
			}
		}
		return v
	}
	for _, tc := range []struct {
		spec      api.GameSpec
		dedicated bool
		want      map[string]string
	}{
		{api.GameSpec{Mode: "sp"}, false, map[string]string{"deathmatch": "0", "coop": "0", "maxclients": "1", "skill": "1"}},
		{api.GameSpec{Mode: "coop", Cvars: map[string]string{"skill": "3"}}, false, map[string]string{"coop": "1", "maxclients": "4", "skill": "3"}},
		{api.GameSpec{Mode: "dm", MaxPlayers: 12, Cvars: map[string]string{"fraglimit": "20", "password": "pw"}}, true,
			map[string]string{"deathmatch": "1", "maxclients": "12", "fraglimit": "20", "password": "pw", "ctf": "0"}},
		{api.GameSpec{Mode: "ctf"}, true, map[string]string{"deathmatch": "1", "ctf": "1", "maxclients": "16"}},
	} {
		ded, cv, err := ModeSettings(tc.spec)
		if err != nil {
			t.Fatalf("%+v: %v", tc.spec, err)
		}
		if ded != tc.dedicated {
			t.Errorf("%s: dedicated %v", tc.spec.Mode, ded)
		}
		for k, v := range tc.want {
			if got := get(cv, k); got != v {
				t.Errorf("%s: %s = %q, want %q (%v)", tc.spec.Mode, k, got, v, cv)
			}
		}
	}
	if _, _, err := ModeSettings(api.GameSpec{Mode: "dm", Cvars: map[string]string{"rcon_password": "x"}}); !errors.Is(err, api.ErrGameInvalid) {
		t.Errorf("forbidden cvar: %v", err)
	}
	if _, _, err := ModeSettings(api.GameSpec{Mode: "race"}); !errors.Is(err, api.ErrGameInvalid) {
		t.Errorf("bad mode: %v", err)
	}
}

func TestSanitizeName(t *testing.T) {
	for in, want := range map[string]string{
		"alice":                 "alice",
		`a\b"c;d` + "\x01":      "abcd",
		"":                      "player",
		strings.Repeat("x", 40): strings.Repeat("x", 31),
		"Jürgen the Destroyer":  "Jrgen the Destroyer",
	} {
		if got := SanitizeName(in); got != want {
			t.Errorf("SanitizeName(%q) = %q, want %q", in, got, want)
		}
	}
}

// demoIndex puts maps/demo1.bsp of the demo pak into a blob store and
// returns a one-file index for it.
func demoIndex(t *testing.T) (*manifest.Index, blob.Store) {
	t.Helper()
	pk, err := pak.Open(testutil.RequireFile(t, testutil.DemoPakPath()))
	if err != nil {
		t.Fatal(err)
	}
	defer pk.Close()
	raw, err := pk.ReadFile("maps/demo1.bsp")
	if err != nil {
		t.Fatal(err)
	}
	store, err := blob.NewFileStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	info, err := store.PutBytes(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	idx := &manifest.Index{Pakset: "demo", Files: map[string]*manifest.Entry{
		"maps/demo1.bsp": {Path: "maps/demo1.bsp", SHA256: info.SHA256, Size: int64(len(raw))},
	}}
	return idx, store
}

func TestIndexFS(t *testing.T) {
	idx, store := demoIndex(t)
	f := NewIndexFS(idx, store)
	b, err := f.ReadFile("MAPS/Demo1.bsp")
	if err != nil || len(b) == 0 {
		t.Fatalf("ReadFile: %d bytes, %v", len(b), err)
	}
	if _, err := f.ReadFile("maps/nope.bsp"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("missing file: %v", err)
	}
}

func TestGamesIdleReapAndSaves(t *testing.T) {
	idx, store := demoIndex(t)
	repo := db.NewMemory()
	u, err := repo.CreateUser(context.Background(), db.User{Email: "a@example.com", DisplayName: "a", PasswordHash: "x"})
	if err != nil {
		t.Fatal(err)
	}
	ended := make(chan string, 4)
	g := NewGames(GamesConfig{
		Indexes: func(_ context.Context, id string) (*manifest.Index, error) {
			if id != "demo" {
				return nil, db.ErrNotFound
			}
			return idx, nil
		},
		Blobs:        store,
		Saves:        db.NewSaveStore(repo),
		Tickets:      auth.NewTickets(0),
		Game:         func(*crand.Rand) sv.GameFactory { return stubgame.New() },
		IdleTimeout:  300 * time.Millisecond,
		ReapInterval: 50 * time.Millisecond,
		OnEnd:        func(id string) { ended <- id },
	})
	defer g.Close()
	ctx := context.Background()

	server, err := g.CreateWithID(ctx, "server", api.GameSpec{Mode: "dm", Map: "demo1"})
	if err != nil {
		t.Fatal(err)
	}
	owned, err := g.Create(ctx, api.GameSpec{OwnerID: u.ID, Mode: "coop", Map: "demo1"})
	if err != nil {
		t.Fatal(err)
	}
	info, err := g.Get(ctx, owned)
	if err != nil || info.Map != "demo1" || info.MaxPlayers != 4 || info.Pakset != "demo" {
		t.Fatalf("Get: %+v %v", info, err)
	}
	tk, url, err := g.Join(ctx, owned, api.Player{UserID: u.ID, DisplayName: "a"})
	if err != nil || tk == "" || url != "/ws/v1/games/"+owned+"?ticket="+tk {
		t.Fatalf("Join: %q %q %v", tk, url, err)
	}
	if _, _, err := g.Join(ctx, "nope", api.Player{}); !errors.Is(err, api.ErrGameNotFound) {
		t.Errorf("Join unknown: %v", err)
	}
	if _, err := g.Create(ctx, api.GameSpec{OwnerID: u.ID, Mode: "sp", Map: "demo1", Pakset: "retail"}); err == nil {
		t.Error("unknown pakset accepted")
	}

	select {
	case id := <-ended:
		if id != owned {
			t.Fatalf("ended %s, want the idle owned game %s", id, owned)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("idle game not reaped")
	}
	if _, err := g.Get(ctx, server); err != nil {
		t.Errorf("server game reaped: %v", err)
	}
	list, _ := g.List(ctx)
	if len(list) != 1 || list[0].ID != server {
		t.Errorf("List: %+v", list)
	}
	if err := g.Stop(ctx, server); err != nil {
		t.Fatal(err)
	}
	if err := g.Stop(ctx, server); !errors.Is(err, api.ErrGameNotFound) {
		t.Errorf("second Stop: %v", err)
	}
}

func TestAccountSaves(t *testing.T) {
	repo := db.NewMemory()
	ctx := context.Background()
	u, _ := repo.CreateUser(ctx, db.User{Email: "a@example.com", DisplayName: "a", PasswordHash: "x"})
	st := newAccountSaves(db.NewSaveStore(repo), u.ID, SaveOrigin{Pakset: "demo", Mode: "sp"})
	if err := st.Write("current", "server.ssv", []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	if list, _ := repo.ListSaves(ctx, u.ID); len(list) != 0 {
		t.Fatalf("current reached the database: %+v", list)
	}
	if err := st.Write("save0", "server.ssv", []byte(`{"version":1,"comment":"ENTERING x","mapcmd":"demo1"}`)); err != nil {
		t.Fatal(err)
	}
	_, info, err := repo.GetSave(ctx, u.ID, AutosaveSlot)
	if err != nil || info.Comment != "ENTERING x" || info.MapCmd != "demo1" || info.Mode != "sp" {
		t.Fatalf("autosave row: %+v %v", info, err)
	}
	names, _ := st.List("save0")
	if strings.Join(names, ",") != InstanceFile+",server.ssv" {
		t.Errorf("files %v", names)
	}
	if _, err := st.Read("quick", "server.ssv"); !errors.Is(err, sv.ErrNoSave) {
		t.Errorf("missing slot: %v", err)
	}
}
