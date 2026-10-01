package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"quake2web/server/internal/agent/mapdata"
	"quake2web/server/internal/agent/nav"
	"quake2web/server/internal/assets/pak"
	"quake2web/server/internal/bsp"
	"quake2web/server/internal/testutil"
)

// writePak writes a pak holding the given files.
func writePak(t *testing.T, files map[string][]byte) string {
	t.Helper()
	var names []string
	for n := range files {
		names = append(names, n)
	}
	data := make([]byte, 12)
	type ent struct{ pos, n int }
	var ents []ent
	for _, n := range names {
		ents = append(ents, ent{len(data), len(files[n])})
		data = append(data, files[n]...)
	}
	dir := len(data)
	for i, n := range names {
		d := make([]byte, 64)
		copy(d, n)
		binary.LittleEndian.PutUint32(d[56:], uint32(ents[i].pos))
		binary.LittleEndian.PutUint32(d[60:], uint32(ents[i].n))
		data = append(data, d...)
	}
	binary.LittleEndian.PutUint32(data[0:], 'P'|'A'<<8|'C'<<16|'K'<<24)
	binary.LittleEndian.PutUint32(data[4:], uint32(dir))
	binary.LittleEndian.PutUint32(data[8:], uint32(64*len(names)))
	path := filepath.Join(t.TempDir(), "synth.pak")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestNavCommandsSynthetic runs build, verify (with the live lockstep
// server), dump and path on a pak holding the synthetic floor map.
func TestNavCommandsSynthetic(t *testing.T) {
	pk := writePak(t, map[string][]byte{"maps/synth.bsp": bsp.Encode(bsp.SyntheticFloorMap())})
	out := t.TempDir()
	code, stdout, stderr := runQ2nav("build", "-pak", pk, "-map", "synth", "-out", out)
	if code != 0 {
		t.Fatalf("build exit %d: %s%s", code, stdout, stderr)
	}
	files, _ := filepath.Glob(filepath.Join(out, "synth-*-v1-*.json.gz"))
	if len(files) != 1 || !strings.Contains(stdout, "synth: 25 nodes") || !strings.Contains(stdout, "wrote ") {
		t.Fatalf("build output %q, files %v", stdout, files)
	}
	if code, stdout, _ = runQ2nav("build", "-pak", pk, "-all", "-out", out); code != 0 || !strings.Contains(stdout, "up to date") {
		t.Errorf("second build: %d %q", code, stdout)
	}

	code, stdout, stderr = runQ2nav("verify", "-pak", pk, "-map", "synth", "-nav", out, "-sample", "0", "-live", "12")
	if code != 0 {
		t.Fatalf("verify exit %d:\n%s%s", code, stdout, stderr)
	}
	for _, want := range []string{"resim: 168/168 edges arrive", "live: 12/12 edges reproduce on the server (12 bit-exact", "verify: ok"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("verify output lacks %q:\n%s", want, stdout)
		}
	}

	dump := filepath.Join(t.TempDir(), "synth.json")
	if code, stdout, stderr = runQ2nav("dump", "-pak", pk, "-map", "synth", "-nav", out, "-o", dump); code != 0 {
		t.Fatalf("dump exit %d: %s%s", code, stdout, stderr)
	}
	var d dumpFile
	b, err := os.ReadFile(dump)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &d); err != nil {
		t.Fatal(err)
	}
	if d.Schema != "q2nav.dump/2" || len(d.Nodes) != 25 || len(d.Edges) != 168 || len(d.Spawns) != 1 || d.EdgeKinds[1] != "walk" ||
		d.EdgeFlags[0] != "fast" || d.Edges[0][4]&int32(nav.EdgeFast) == 0 {
		t.Errorf("dump %q: %d nodes %d edges %d spawns kinds %v flags %v", d.Schema, len(d.Nodes), len(d.Edges), len(d.Spawns), d.EdgeKinds, d.EdgeFlags)
	}
	// empty lists are [] (an overlay maps over them), never null
	if bytes.Contains(b, []byte("null")) || !bytes.Contains(b, []byte(`"lasers":[]`)) || !bytes.Contains(b, []byte(`"solids":[]`)) {
		t.Errorf("dump has null lists: %s", b[:min(len(b), 400)])
	}

	if code, stdout, stderr = runQ2nav("path", "-pak", pk, "-map", "synth", "-nav", out, "-to", "64,64,24"); code != 0 || !strings.Contains(stdout, "path ") {
		t.Errorf("path exit %d: %s%s", code, stdout, stderr)
	}
	if code, _, _ = runQ2nav("path", "-pak", pk, "-map", "synth", "-nav", out, "-to", "5000,0,0"); code != 1 {
		t.Errorf("path to nowhere: exit %d", code)
	}
}

// demoCached skips under the race detector when the demo graphs are not in
// the shared cache (building them there takes about half a minute each).
func demoCached(t *testing.T, names ...string) {
	t.Helper()
	if !raceEnabled || os.Getenv("Q2_AGENT_LONG") != "" {
		return
	}
	p, err := pak.Open(testutil.DemoPak(t))
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	for _, n := range names {
		raw, err := p.ReadFile("maps/" + n + ".bsp")
		if err != nil {
			t.Fatal(err)
		}
		md, err := mapdata.Load(n, raw, mapdata.Options{Skill: 1})
		if err != nil {
			t.Fatal(err)
		}
		g, err := nav.ReadFile(filepath.Join(nav.DefaultDir(), nav.CacheName(n, md.Checksum, nav.DefaultParams())))
		if err != nil || g.Matches(md, nav.DefaultParams()) != nil {
			t.Skipf("no cached %s graph under -race; run q2nav build -all or set Q2_AGENT_LONG=1", n)
		}
	}
}

// TestVerifyDemo checks the phase-2 gates on the demo maps with small
// samples: every re-simulated edge arrives, the live server reproduces the
// sampled edges, every route step is reachable.
func TestVerifyDemo(t *testing.T) {
	pk := testutil.DemoPak(t)
	demoCached(t, "demo1", "demo2", "demo3")
	routes := filepath.Join(filepath.Dir(routesDir(t)), "routes")
	for _, m := range []string{"demo1", "demo2", "demo3"} {
		code, stdout, stderr := runQ2nav("verify", "-pak", pk, "-map", m, "-sample", "200", "-live", "25", "-posed", "15", "-routes", routes)
		if code != 0 {
			t.Errorf("%s: verify exit %d:\n%s%s", m, code, stdout, stderr)
			continue
		}
		if !strings.Contains(stdout, "verify: ok") || !strings.Contains(stdout, "route ") || !strings.Contains(stdout, "live posed: 15/15") {
			t.Errorf("%s: %s", m, stdout)
		}
		t.Logf("%s", stdout)
	}
}

func TestNavUsageErrors(t *testing.T) {
	for _, args := range [][]string{
		{"build", "-pak", "x.pak"},                      // neither -map nor -all
		{"build", "-pak", "x.pak", "-map", "a", "-all"}, // both
		{"verify", "-pak", "x.pak"},                     // no -map
		{"dump", "-pak", "x.pak"},                       // no -map
		{"path", "-pak", "x.pak", "-map", "demo1"},      // no -to
		{"verify", "-map", "demo1", "-bogus"},           // bad flag
	} {
		if code, _, errOut := runQ2nav(args...); code != 2 || errOut == "" {
			t.Errorf("q2nav %q: exit %d, stderr %q; want 2 with usage", args, code, errOut)
		}
	}
}

// TestDumpDemoSolids: the demo1 dump lists the static solids the edges
// were validated with, the buttons among them, and the lasers as [].
func TestDumpDemoSolids(t *testing.T) {
	pk := testutil.DemoPak(t)
	demoCached(t, "demo1")
	out := filepath.Join(t.TempDir(), "demo1.json")
	if code, stdout, stderr := runQ2nav("dump", "-pak", pk, "-map", "demo1", "-o", out); code != 0 {
		t.Fatalf("dump exit %d: %s%s", code, stdout, stderr)
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	var d dumpFile
	if err := json.Unmarshal(b, &d); err != nil {
		t.Fatal(err)
	}
	buttons := map[int32]bool{}
	for _, id := range d.Buttons {
		buttons[id] = true
	}
	if len(d.Solids) == 0 || !buttons[591] || d.Lasers == nil || bytes.Contains(b, []byte("null")) {
		t.Errorf("%d solids, buttons %v, lasers %v", len(d.Solids), d.Buttons, d.Lasers)
	}
	for _, s := range d.Solids {
		if s.Entity == 591 && (s.Class != "func_button" || s.Model != "*34" || s.Min[0] >= s.Max[0]) {
			t.Errorf("button *34 %+v", s)
		}
	}
	// a dump to an unwritable place fails instead of reporting success
	if code, _, _ := runQ2nav("dump", "-pak", pk, "-map", "demo1", "-o", filepath.Join(t.TempDir(), "missing", "x.json")); code == 0 {
		t.Error("dump into a missing directory succeeded")
	}
}
