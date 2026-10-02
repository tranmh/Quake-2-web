package fairness

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"quake2web/server/internal/agent/bot"
	"quake2web/server/internal/agent/decide"
	"quake2web/server/internal/agent/mapdata"
	"quake2web/server/internal/agent/nav"
	"quake2web/server/internal/agent/route"
	"quake2web/server/internal/agent/trace"
	"quake2web/server/internal/agent/worldmodel"
	"quake2web/server/internal/assets/pak"
	"quake2web/server/internal/fakeclient"
	"quake2web/server/internal/testutil"
)

// forbidden returns the packages neither this package nor its test
// binary may link: the server, the game, the collision world, the host
// and the session (whose truth is for metrics only).
func forbidden() []string {
	return []string{"internal/sv", "internal/game", "internal/world", "internal/host", "internal/agent/session"}
}

func goCommand(t *testing.T) string {
	t.Helper()
	gobin := filepath.Join(runtime.GOROOT(), "bin", "go")
	if _, err := exec.LookPath(gobin); err != nil {
		if gobin, err = exec.LookPath("go"); err != nil {
			t.Skip("go command not found")
		}
	}
	return gobin
}

// TestImports: the rebuild and its tests link none of the forbidden
// packages (go list -deps -test: the test binary's whole import graph).
func TestImports(t *testing.T) {
	out, err := exec.Command(goCommand(t), "list", "-deps", "-test", "-f", "{{.ImportPath}}", ".").Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	deps := strings.Fields(string(out))
	if len(deps) < 20 {
		t.Fatalf("go list returned %d packages", len(deps))
	}
	for _, dep := range deps {
		for _, f := range forbidden() {
			if p := "quake2web/server/" + f; dep == p || strings.HasPrefix(dep, p+"/") {
				t.Errorf("the fairness test links %s", dep)
			}
		}
	}
}

// recording is campaign.FairnessRecording (the recording's metadata, as
// TestRecordFairness writes it).
type recording struct {
	Seed   uint64 `json:"seed"`
	Map    string `json:"map"`
	Visit  int    `json:"visit"`
	Skill  int    `json:"skill"`
	Demo   string `json:"demo"`
	Trace  string `json:"trace"`
	Deaths int    `json:"deaths"`
	Ticks  int    `json:"ticks"`
}

// record runs campaign.TestRecordFairness in its own test binary (which
// links the session) and returns the directory it wrote. Q2_FAIRNESS_DIR
// names a recording to use instead.
func record(t *testing.T) string {
	t.Helper()
	if dir := os.Getenv("Q2_FAIRNESS_DIR"); dir != "" {
		return dir
	}
	dir := t.TempDir()
	cmd := exec.Command(goCommand(t), "test", "-count=1", "-run", "^TestRecordFairness$", "quake2web/server/internal/agent/campaign")
	cmd.Env = append(os.Environ(), "Q2_FAIRNESS_OUT="+dir)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("recording: %v\n%s", err, out)
	}
	if _, err := os.Stat(filepath.Join(dir, "recording.json")); err != nil {
		t.Fatalf("recording: %v\n%s", err, out)
	}
	return dir
}

// TestDifferential is the fairness differential test: a lockstep episode
// of the scripted pipeline on demo1 without cheats is recorded (.dm2 and
// trace) by another test binary; this one, which cannot reach the server,
// the game or the session, rebuilds the bot's belief and decisions from
// the recording alone, and every decision tick (intent, provenance, lane
// state digest, commands) and every usercmd must match byte for byte.
func TestDifferential(t *testing.T) {
	if testing.Short() {
		t.Skip("records a lockstep episode")
	}
	pk, err := pak.Open(testutil.DemoPak(t))
	if err != nil {
		t.Fatal(err)
	}
	fs := &pak.FS{}
	fs.AddPak(pk)
	t.Cleanup(func() { _ = fs.Close() })

	dir := record(t)
	var meta recording
	raw, err := os.ReadFile(filepath.Join(dir, "recording.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &meta); err != nil {
		t.Fatal(err)
	}
	dm2, err := os.ReadFile(filepath.Join(dir, meta.Demo))
	if err != nil {
		t.Fatal(err)
	}
	events, err := trace.ReadFile(filepath.Join(dir, meta.Trace))
	if err != nil {
		t.Fatal(err)
	}

	// the static map knowledge the campaign gave the bot
	bsp, err := fs.ReadFile("maps/" + meta.Map + ".bsp")
	if err != nil {
		t.Fatal(err)
	}
	md, err := mapdata.Load(meta.Map, bsp, mapdata.Options{Skill: meta.Skill})
	if err != nil {
		t.Fatal(err)
	}
	g, err := nav.NewStore(nav.DefaultDir(), nil).Load(context.Background(), md, nav.DefaultParams())
	if err != nil {
		t.Fatalf("nav graph (the recording run caches it): %v", err)
	}
	camp, err := route.Load(routesDir())
	if err != nil {
		t.Fatal(err)
	}
	table, err := camp.Select(meta.Map, meta.Visit)
	if err != nil {
		t.Fatal(err)
	}

	cfg := Config{
		Bot: bot.Config{ReadFile: fs.ReadFile, Seed: int64(meta.Seed)},
		NewPolicy: func() (bot.Policy, func(), error) {
			p, err := bot.NewBrain(bot.BrainConfig{Seed: meta.Seed, Mode: decide.Lockstep})
			if err != nil {
				return nil, nil, err
			}
			return p, func() { _ = p.Close() }, nil
		},
		Level:  bot.Level{Key: worldmodel.LevelKey{Map: meta.Map, Visit: meta.Visit}, Map: md, Graph: g, Route: table},
		Client: fakeclient.Options{MaxHistory: 256},
	}
	res, err := Rebuild(Recording{Demo: dm2, Events: events}, cfg)
	t.Logf("rebuilt %d blocks, %d frames, %d ticks, %d commands (recording: %d ticks, %d deaths)", res.Blocks, res.Frames, res.Ticks, res.Cmds,
		meta.Ticks, meta.Deaths)
	if err != nil {
		t.Fatal(err)
	}
	if res.Ticks != meta.Ticks || res.Ticks < 100 {
		t.Errorf("%d ticks rebuilt, the recording has %d", res.Ticks, meta.Ticks)
	}
}

// routesDir is the route tables' directory: $Q2_ROUTES_DIR, else the
// repository's fixtures/agent/routes (campaign.DefaultRoutesDir, which
// this package may not import).
func routesDir() string {
	if d := os.Getenv("Q2_ROUTES_DIR"); d != "" {
		return d
	}
	root, err := testutil.RepoRoot()
	if err != nil {
		return filepath.Join("fixtures", "agent", "routes")
	}
	return filepath.Join(root, "fixtures", "agent", "routes")
}
