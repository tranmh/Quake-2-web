package gametest

// Tampered / corrupted save blobs: a valid game+level save of a running
// demo1 game is decompressed, a few JSON values are replaced with hostile
// ones, and the result is loaded with the real load flow (ReadGame,
// SpawnServer, ReadLevel, reconnect) and played. A Go panic is a failure; an
// error returned by ReadGame/ReadLevel is the expected outcome for a bad blob.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"

	"quake2web/server/internal/game"
)

func zstdDec(t testing.TB, b []byte) []byte {
	d, err := zstd.NewReader(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	out, err := d.DecodeAll(b, nil)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func zstdEnc(t testing.TB, b []byte) []byte {
	e, err := zstd.NewWriter(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	return e.EncodeAll(b, nil)
}

type leaf struct {
	parent any // map[string]any or []any
	key    string
	idx    int
}

func collectLeaves(v any, out *[]leaf) {
	switch x := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			switch x[k].(type) {
			case map[string]any, []any:
				collectLeaves(x[k], out)
			}
			*out = append(*out, leaf{parent: x, key: k})
		}
	case []any:
		for i := range x {
			switch x[i].(type) {
			case map[string]any, []any:
				collectLeaves(x[i], out)
			}
			*out = append(*out, leaf{parent: x, idx: i})
		}
	}
}

var hostileNumbers = []string{"-1", "0", "1", "2", "3", "7", "31", "32", "255", "256", "1023", "1024", "1025", "4096",
	"-2147483648", "2147483647", "-32768", "32767", "99999", "1e10", "0.5", "-0.5", "1e38"}

func hostileValue(r *rand.Rand, old any) any {
	names := game.RegistryNames()
	switch r.Intn(10) {
	case 0:
		return nil
	case 1:
		return "bogus"
	case 2:
		return names[r.Intn(len(names))]
	case 3:
		return []string{"soldier_move_stand", "infantry_move_death1", "medic_move_attackCable", "jorg_move_death",
			"weapon_bfg", "item_quad", "ammo_cells", "key_blue_key", ""}[r.Intn(9)]
	case 4:
		return true
	}
	switch o := old.(type) {
	case json.Number:
		return json.Number(hostileNumbers[r.Intn(len(hostileNumbers))])
	case string:
		if r.Intn(2) == 0 {
			return strings.Repeat("x", r.Intn(3000))
		}
		return names[r.Intn(len(names))]
	case bool:
		return !o
	case []any:
		if len(o) > 0 && r.Intn(2) == 0 {
			return o[:r.Intn(len(o))]
		}
		return append(o, json.Number("1"))
	}
	return json.Number(hostileNumbers[r.Intn(len(hostileNumbers))])
}

func mutateSave(t testing.TB, r *rand.Rand, blob []byte, n int) []byte {
	raw := zstdDec(t, blob)
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var root any
	if err := d.Decode(&root); err != nil {
		t.Fatal(err)
	}
	var leaves []leaf
	collectLeaves(root, &leaves)
	for i := 0; i < n; i++ {
		l := leaves[r.Intn(len(leaves))]
		switch p := l.parent.(type) {
		case map[string]any:
			if r.Intn(15) == 0 {
				delete(p, l.key)
			} else {
				p[l.key] = hostileValue(r, p[l.key])
			}
		case []any:
			p[l.idx] = hostileValue(r, p[l.idx])
		}
	}
	out, err := json.Marshal(root)
	if err != nil {
		t.Fatal(err)
	}
	return zstdEnc(t, out)
}

// saveFixture runs mode for frames with some cheats and returns the saves.
func saveFixture(t testing.TB, mode string, frames int, r *rand.Rand) (*Server, []byte, []byte) {
	s := newRobustServer(t, robustModes[mode], "")
	for c := range s.Sc.Clients {
		s.inputCommand(c, "give all")
	}
	if msg := monkey(s, r, frames, nil); classify(msg) != "" {
		t.Fatalf("fixture run: %s", msg)
	}
	gd, err := s.E.Ge.WriteGame(false)
	if err != nil {
		t.Fatal(err)
	}
	ld, err := s.E.Ge.WriteLevel()
	if err != nil {
		t.Fatal(err)
	}
	return s, gd, ld
}

// loadSave follows the real load flow into a fresh server; it returns the
// load error (nil when loaded).
func loadSave(t testing.TB, a *Server, gd, ld []byte) (*Server, error) {
	b, err := NewServer(a.Sc, needDemoPak(t), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.E.Ge.ReadGame(gd); err != nil {
		return nil, err
	}
	if err := b.SpawnServer(a.Sc.Map); err != nil {
		t.Fatal(err)
	}
	b.E.World.ClearWorld()
	b.E.Configstrings = a.E.Configstrings
	b.E.CM.ReadPortalState(a.E.CM.WritePortalState())
	if err := b.E.Ge.ReadLevel(ld); err != nil {
		return nil, err
	}
	for i := range a.Sc.Clients {
		b.reconnect(i, a.Sc.Clients[i].Userinfo)
	}
	return b, nil
}

// TestRobustSave loads mutated saves. Env Q2_SAVE_ITERS (default 12),
// Q2_SAVE_SEED.
func TestRobustSave(t *testing.T) {
	needDemoPak(t)
	iters := 12
	if v := os.Getenv("Q2_SAVE_ITERS"); v != "" {
		fmt.Sscan(v, &iters)
	}
	seed0 := int64(1)
	if v := os.Getenv("Q2_SAVE_SEED"); v != "" {
		fmt.Sscan(v, &seed0)
	}
	for _, mode := range []string{"sp", "coop", "dm", "ctf"} {
		mode := mode
		t.Run(mode, func(t *testing.T) {
			a, gd, ld := saveFixture(t, mode, 60, rand.New(rand.NewSource(99)))
			found := map[string]string{}
			var order []string
			loadErrs := 0
			for it := 0; it < iters; it++ {
				seed := seed0 + int64(it)
				r := rand.New(rand.NewSource(seed))
				mg, ml := gd, ld
				switch r.Intn(3) {
				case 0:
					mg = mutateSave(t, r, gd, 1+r.Intn(4))
				case 1:
					ml = mutateSave(t, r, ld, 1+r.Intn(6))
				default:
					mg = mutateSave(t, r, gd, 1+r.Intn(3))
					ml = mutateSave(t, r, ld, 1+r.Intn(3))
				}
				var b *Server
				var lerr error
				msg := catchPanic(func() { b, lerr = loadSave(t, a, mg, ml) })
				if msg == "" && lerr != nil {
					if strings.Contains(lerr.Error(), "internal error") {
						msg = "comerror: Game Error: " + lerr.Error()
					} else {
						loadErrs++
						continue
					}
				}
				if msg == "" {
					msg = monkey(b, r, 40, nil)
				}
				site := classify(msg)
				if site == "" {
					continue
				}
				if _, dup := found[site]; !dup {
					order = append(order, site)
					found[site] = fmt.Sprintf("seed %d: %s", seed, msg)
				}
			}
			t.Logf("%d/%d mutated saves rejected by the loader", loadErrs, iters)
			for _, site := range order {
				t.Errorf("%s\n%s", site, found[site])
			}
		})
	}
}
