package gametest

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"quake2web/server/internal/game"
	"quake2web/server/internal/testutil"
)

func fixtureScenarios(t *testing.T) []string {
	dir := filepath.Join(testutil.FixturesDir(), "game")
	m, _ := filepath.Glob(filepath.Join(dir, "*.jsonl"))
	if len(m) == 0 {
		t.Skip("game fixtures not available")
	}
	var out []string
	for _, p := range m {
		out = append(out, strings.TrimSuffix(filepath.Base(p), ".jsonl"))
	}
	return out
}

// TestFixtureDelta checks the delta reconstruction on the first frames of
// every fixture: listed edicts replace/add, freed ones disappear.
func TestFixtureDelta(t *testing.T) {
	for _, name := range fixtureScenarios(t) {
		t.Run(name, func(t *testing.T) {
			fr, err := OpenFixture(filepath.Join(testutil.FixturesDir(), "game", name+".jsonl"))
			if err != nil {
				t.Fatal(err)
			}
			defer fr.Close()
			if fr.Encoding != "delta" && fr.Encoding != "full" {
				t.Fatalf("encoding %q", fr.Encoding)
			}
			var prev map[int]map[string]any
			for i := 0; i < 50; i++ {
				f, err := fr.Next()
				if err == io.EOF {
					break
				}
				if err != nil {
					t.Fatal(err)
				}
				if f.Frame != i {
					t.Fatalf("frame %d at line %d", f.Frame, i)
				}
				if i == 0 {
					if len(f.Edicts) == 0 || f.Edicts[0]["classname"] != "worldspawn" {
						t.Fatalf("frame 0 without worldspawn")
					}
				} else {
					want := len(prev) - len(f.Freed)
					for _, n := range f.Listed {
						if _, ok := prev[n]; !ok {
							want++
						} else {
							for _, fn := range f.Freed {
								if fn == n {
									want++ // freed and re-used in the same frame
								}
							}
						}
					}
					if len(f.Edicts) != want {
						t.Fatalf("frame %d: %d edicts, want %d", i, len(f.Edicts), want)
					}
				}
				for n, r := range f.Edicts {
					if k, _ := numInt(r["n"]); k != n {
						t.Fatalf("frame %d: record %d under key %d", i, k, n)
					}
				}
				prev = f.Edicts
			}
		})
	}
}

// TestScheduleInputs checks that the input generator (schedule + random
// walks) reproduces the per-frame "inputs" echoed by the oracle.
func TestScheduleInputs(t *testing.T) {
	for _, name := range fixtureScenarios(t) {
		t.Run(name, func(t *testing.T) {
			fr, err := OpenFixture(filepath.Join(testutil.FixturesDir(), "game", name+".jsonl"))
			if err != nil {
				t.Fatal(err)
			}
			defer fr.Close()
			sc, err := ParseScenario(fr.RawHead)
			if err != nil {
				t.Fatal(err)
			}
			scPath := filepath.Join(testutil.FixturesDir(), "scenarios", "game", name+".json")
			if _, err := os.Stat(scPath); err == nil {
				sc2, err := LoadScenario(scPath)
				if err != nil {
					t.Fatal(err)
				}
				if len(sc2.Schedule) != len(sc.Schedule) || sc2.Map != sc.Map || *sc2.Frames != *sc.Frames {
					t.Fatalf("scenario file differs from the fixture header")
				}
			}
			sch := NewSchedule(sc)
			for {
				f, err := fr.Next()
				if err == io.EOF {
					break
				}
				if err != nil {
					t.Fatal(err)
				}
				var act []any
				if f.Frame > 0 {
					ins, err := sch.Inputs(f.Frame)
					if err != nil {
						t.Fatal(err)
					}
					for _, in := range ins {
						act = append(act, in.Record())
					}
				}
				if act == nil {
					act = []any{}
				}
				if d := cmpVal("inputs", f.Inputs, act); len(d) > 0 {
					t.Fatalf("frame %d: %v", f.Frame, d[0])
				}
			}
		})
	}
}

func sameKeys(t *testing.T, what string, exp map[string]any, act map[string]any) {
	t.Helper()
	for k := range exp {
		if _, ok := act[k]; !ok {
			t.Errorf("%s: our record lacks %q", what, k)
		}
	}
	for k := range act {
		if _, ok := exp[k]; !ok {
			t.Errorf("%s: our record has extra %q", what, k)
		}
	}
}

// TestRecordShapes checks that our dump records use exactly the fixture keys.
func TestRecordShapes(t *testing.T) {
	path := filepath.Join(testutil.FixturesDir(), "game", "demo1_dm4.jsonl")
	fr, err := OpenFixture(path)
	if err != nil {
		t.Skip("fixture not available")
	}
	defer fr.Close()
	f, err := fr.Next()
	if err != nil {
		t.Fatal(err)
	}
	var e game.Edict
	e.Client = &game.GClient{}
	rec := edictRecord(&e)
	sameKeys(t, "edict", f.Edicts[0], rec)
	sameKeys(t, "entity_state", f.Edicts[0]["s"].(map[string]any), rec["s"].(map[string]any))
	ps := psRecord(&e.Client.PS)
	cl := f.Clients[0].(map[string]any)
	sameKeys(t, "ps", cl["ps"].(map[string]any), ps)
	sameKeys(t, "pmove", cl["ps"].(map[string]any)["pmove"].(map[string]any), ps["pmove"].(map[string]any))
	sameKeys(t, "level", f.Level, Record{"framenum": 0, "time": 0, "killed_monsters": 0, "total_monsters": 0, "found_secrets": 0, "total_secrets": 0})
}
