package gametest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"quake2web/server/internal/testutil"
)

// dumpToFixture turns one of our dumps into the decoded-fixture form so two
// runs can be compared with CompareFrame.
func dumpToFixture(t *testing.T, d *FrameDump) *FixtureFrame {
	t.Helper()
	enc := func(v any) any {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		var out any
		if err := decodeNumber(b, &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	ff := &FixtureFrame{Frame: d.Frame, RandCalls: d.RandCalls, Edicts: map[int]map[string]any{}}
	ff.Level, _ = enc(d.Level).(map[string]any)
	ff.Inputs, _ = enc(d.Inputs).([]any)
	ff.Clients, _ = enc(d.Clients).([]any)
	ff.Events, _ = enc(d.Events).([]any)
	for n, r := range d.Edicts {
		ff.Edicts[n] = enc(r).(map[string]any)
	}
	return ff
}

// TestSaveRoundTrip: run demo1_dm4 to frame N, save game+level, load them
// into a fresh engine+game following the real load flow, then feed the same
// inputs to both for M frames: every frame dump must be identical.
func TestSaveRoundTrip(t *testing.T) {
	if testing.Short() {
		t.Skip("short")
	}
	const scenario = "demo1_dm4"
	const saveAt, more = 150, 100
	base := testutil.BaseDir()
	if _, err := os.Stat(filepath.Join(base, "baseq2", "pak0.pak")); err != nil {
		t.Skip("demo pak not available")
	}
	scPath := filepath.Join(testutil.FixturesDir(), "scenarios", "game", scenario+".json")
	sc, err := LoadScenario(scPath)
	if err != nil {
		t.Skip("scenario not available")
	}

	a, err := NewServer(sc, base, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.Start(); err != nil {
		t.Fatal(err)
	}
	a.EndFrame()
	for f := 1; f <= saveAt; f++ {
		if err := a.Frame(f); err != nil {
			t.Fatal(err)
		}
		a.EndFrame()
	}

	gameData, err := a.E.Ge.WriteGame(false)
	if err != nil {
		t.Fatal(err)
	}
	levelData, err := a.E.Ge.WriteLevel()
	if err != nil {
		t.Fatal(err)
	}

	// fresh engine + game: SV_InitGame + ge->Init, ReadGame, SV_SpawnServer
	b, err := NewServer(sc, base, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.E.Ge.ReadGame(gameData); err != nil {
		t.Fatal(err)
	}
	if err := b.SpawnServer(sc.Map); err != nil {
		t.Fatal(err)
	}
	// SV_ReadLevelFile: configstrings, portal state, ge->ReadLevel
	b.E.World.ClearWorld()
	b.E.Configstrings = a.E.Configstrings
	b.E.CM.ReadPortalState(a.E.CM.WritePortalState())
	if err := b.E.Ge.ReadLevel(levelData); err != nil {
		t.Fatal(err)
	}
	*b.Rng = *a.Rng
	// the reconnect: clients are spawned again and connected
	edicts := b.E.Ge.Edicts()
	for i := range sc.Clients {
		b.E.clients[i] = a.E.clients[i]
		b.E.clients[i].edict = &edicts[i+1]
		if cl := edicts[i+1].Client; cl != nil {
			cl.Pers.Connected = true
		}
	}
	// same input generator state
	sch := *a.sched
	sch.walks = nil
	for _, w := range a.sched.walks {
		wc := *w
		sch.walks = append(sch.walks, &wc)
	}
	b.sched = &sch

	for f := saveAt + 1; f <= saveAt+more; f++ {
		if err := a.Frame(f); err != nil {
			t.Fatal(err)
		}
		da := a.Dump(f)
		if err := b.Frame(f); err != nil {
			t.Fatal(err)
		}
		db := b.Dump(f)
		if diffs := CompareFrame(dumpToFixture(t, da), db, 20); len(diffs) > 0 {
			t.Fatalf("frame %d (saved at %d) differs after load:\n%v", f, saveAt, diffs)
		}
		a.EndFrame()
		b.EndFrame()
	}
}
