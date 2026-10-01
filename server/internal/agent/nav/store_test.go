package nav_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"quake2web/server/internal/agent/mapdata"
	"quake2web/server/internal/agent/nav"
	"quake2web/server/internal/bsp"
	"quake2web/server/internal/q2const"
)

func synthMap(t testing.TB) *mapdata.Map {
	t.Helper()
	md, err := mapdata.Load("synthetic", bsp.Encode(bsp.SyntheticFloorMap()), mapdata.Options{Skill: 1})
	if err != nil {
		t.Fatal(err)
	}
	return md
}

// counting returns a build function that counts its calls.
func counting(t testing.TB, n *atomic.Int32) nav.BuildFunc {
	return func(ctx context.Context, md *mapdata.Map, p nav.Params) (*nav.Graph, error) {
		n.Add(1)
		return testGraph(t), nil
	}
}

func TestStoreBuildsOnceAndCaches(t *testing.T) {
	dir := t.TempDir()
	md := synthMap(t)
	p := nav.DefaultParams()
	var n atomic.Int32
	s := nav.NewStore(dir, counting(t, &n))
	g, err := s.Load(context.Background(), md, p)
	if err != nil {
		t.Fatal(err)
	}
	if n.Load() != 1 || g.Skill != 1 || g.Map != "synthetic" || g.Checksum != md.Checksum || g.PhysicsHash != p.PhysicsHash() {
		t.Fatalf("built %d times, graph %q %x skill %d", n.Load(), g.Map, g.Checksum, g.Skill)
	}
	path := s.Path(md, p)
	if filepath.Dir(path) != dir || filepath.Base(path) != nav.CacheName("synthetic", md.Checksum, p) {
		t.Errorf("path %s", path)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("cache file not written: %v", err)
	}
	// memory, then a fresh store reading the file
	if _, err := s.Load(context.Background(), md, p); err != nil || n.Load() != 1 {
		t.Fatalf("second load built again (%d) %v", n.Load(), err)
	}
	s2 := nav.NewStore(dir, counting(t, &n))
	g2, err := s2.Load(context.Background(), md, p)
	if err != nil || n.Load() != 1 {
		t.Fatalf("fresh store rebuilt (%d) %v", n.Load(), err)
	}
	if len(g2.Edges) != len(g.Edges) || g2.Skill != 1 {
		t.Errorf("cached graph differs: %d vs %d edges", len(g2.Edges), len(g.Edges))
	}
	// another skill of the same file: no rebuild, other resolution
	md0, _ := mapdata.Load("synthetic", bsp.Encode(bsp.SyntheticFloorMap()), mapdata.Options{Skill: 0})
	g0, err := s2.Load(context.Background(), md0, p)
	if err != nil || n.Load() != 1 || g0.Skill != 0 {
		t.Errorf("skill 0: %v built %d skill %d", err, n.Load(), g0.Skill)
	}
	// no tmp files left behind
	ents, _ := os.ReadDir(dir)
	if len(ents) != 1 {
		t.Errorf("cache dir holds %d files", len(ents))
	}
}

func TestStoreRebuildsCorruptAndStale(t *testing.T) {
	dir := t.TempDir()
	md := synthMap(t)
	p := nav.DefaultParams()
	var n atomic.Int32
	var logs []string
	s := nav.NewStore(dir, counting(t, &n))
	s.Logf = func(f string, a ...any) { logs = append(logs, f) }
	path := s.Path(md, p)
	if err := os.WriteFile(path, []byte("not a graph"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Load(context.Background(), md, p); err != nil || n.Load() != 1 {
		t.Fatalf("corrupt file: %v, built %d", err, n.Load())
	}
	if g, err := nav.ReadFile(path); err != nil || g.Matches(md, p) != nil {
		t.Fatalf("the rebuilt file is not valid: %v", err)
	}
	if len(logs) == 0 {
		t.Error("corrupt file not logged")
	}

	// a valid graph of another map version under this name is rebuilt too
	stale := testGraph(t)
	stale.Map, stale.Checksum, stale.PhysicsHash, stale.Params = "synthetic", md.Checksum+1, p.PhysicsHash(), p
	if err := stale.WriteFile(path); err != nil {
		t.Fatal(err)
	}
	s2 := nav.NewStore(dir, counting(t, &n))
	if _, err := s2.Load(context.Background(), md, p); err != nil || n.Load() != 2 {
		t.Fatalf("stale file: %v, built %d", err, n.Load())
	}
	if err := stale.Matches(md, p); !errors.Is(err, nav.ErrMismatch) {
		t.Errorf("Matches %v", err)
	}
}

func TestStoreSingleFlight(t *testing.T) {
	md := synthMap(t)
	release := make(chan struct{})
	var n atomic.Int32
	s := nav.NewStore(t.TempDir(), func(ctx context.Context, md *mapdata.Map, p nav.Params) (*nav.Graph, error) {
		n.Add(1)
		<-release
		return testGraph(t), nil
	})
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.Load(context.Background(), md, nav.DefaultParams())
			errs <- err
		}()
	}
	time.Sleep(20 * time.Millisecond)
	close(release)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if n.Load() != 1 {
		t.Fatalf("%d builds for 8 concurrent loads", n.Load())
	}
}

func TestStoreCancel(t *testing.T) {
	md := synthMap(t)
	var cancelled atomic.Bool
	started := make(chan struct{})
	s := nav.NewStore("", func(ctx context.Context, md *mapdata.Map, p nav.Params) (*nav.Graph, error) {
		close(started)
		<-ctx.Done()
		cancelled.Store(true)
		return nil, ctx.Err()
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error)
	go func() {
		_, err := s.Load(ctx, md, nav.DefaultParams())
		done <- err
	}()
	<-started
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("Load after cancel: %v", err)
	}
	// the abandoned build stops
	for i := 0; i < 200 && !cancelled.Load(); i++ {
		time.Sleep(5 * time.Millisecond)
	}
	if !cancelled.Load() {
		t.Fatal("the build was not cancelled when its only caller gave up")
	}

	// no builder and no file
	if _, err := nav.NewStore(t.TempDir(), nil).Load(context.Background(), md, nav.DefaultParams()); !errors.Is(err, nav.ErrNoBuilder) {
		t.Errorf("no builder: %v", err)
	}
	if _, err := nav.NewStore("", nil).Load(context.Background(), nil, nav.DefaultParams()); err == nil {
		t.Error("nil map accepted")
	}
	bad := nav.DefaultParams()
	bad.Grid = 0
	if _, err := nav.NewStore("", nil).Load(context.Background(), md, bad); err == nil {
		t.Error("bad params accepted")
	}
}

func TestDefaultDir(t *testing.T) {
	d := nav.DefaultDir()
	if filepath.Base(d) != "nav" || filepath.Base(filepath.Dir(d)) != "assets" {
		t.Errorf("DefaultDir %s", d)
	}
}

func TestWorldHelpers(t *testing.T) {
	md := synthMap(t)
	g := testGraph(t)
	// the door blocks the way at pos1 only
	g.Blockers[0].Headnode = md.CM.WorldModel().Headnode
	w := g.NewWorld(md.CM, nil)
	if len(w.Solids()) != 1 || w.Solids()[0].ID != 30 {
		t.Fatalf("static solids %+v", w.Solids())
	}
	g.SetWorld(w, g.SpawnPoses())
	if len(w.Solids()) != 3 { // barrel box, door at pos1, plat at bottom (the laser never)
		t.Fatalf("spawn world solids %d", len(w.Solids()))
	}
	poses := g.EdgePoses(&g.Edges[3]) // a spawn-world touch edge from a plain node
	if poses[0] != 0 || poses[1] != 1 || poses[2] != 0 {
		t.Errorf("spawn world poses %v", poses)
	}
	poses = g.EdgePoses(&g.Edges[4]) // from the plat top
	if poses[0] != -1 || poses[1] != 0 || poses[2] != -1 {
		t.Errorf("support poses %v", poses)
	}
	if s := g.BlockerSolid(1, 1); s.ID != 11 || s.Origin != (nav.Vec3{0, 0, -100}) {
		t.Errorf("blocker solid %+v", s)
	}
	r := g.NewRunner(w)
	if len(r.Volumes) != 2 || len(r.Pushes) != 1 || len(r.Teleports) != 1 || r.Phys.StepMsec != 25 {
		t.Errorf("runner %+v", r)
	}
	// holding a ladder node faces the ladder; a crouch node ducks
	if c := nav.HoldCmd(&nav.Node{Flags: nav.NodeLadder, Yaw: 90}); c.Yaw != 90 || c.Forward != 0 {
		t.Errorf("ladder hold %+v", c)
	}
	if c := nav.HoldCmd(&nav.Node{Flags: nav.NodeCrouch}); c.Up >= 0 {
		t.Errorf("crouch hold %+v", c)
	}
	// Place on the synthetic floor (node 0 at the slab top)
	g.SetWorld(w, nil)
	g.Place(r, 0)
	if st := r.State(); !st.OnGround() || st.Origin() != g.Nodes[0].Origin || st.PM.PmFlags&q2const.PMF_ON_GROUND == 0 {
		t.Errorf("placed state %+v", st)
	}
}
