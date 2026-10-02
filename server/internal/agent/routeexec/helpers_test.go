package routeexec

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"quake2web/server/internal/agent/mapdata"
	"quake2web/server/internal/agent/nav"
	"quake2web/server/internal/agent/nav/navbuild"
	"quake2web/server/internal/agent/nav/navrt"
	"quake2web/server/internal/agent/route"
	"quake2web/server/internal/agent/worldmodel"
	"quake2web/server/internal/assets/pak"
	"quake2web/server/internal/testutil"
)

// level is a demo map's static data (skill 1).
type level struct {
	md *mapdata.Map
	g  *nav.Graph
}

// demo loads demo map name: its map data and its nav graph from the
// shared cache (assets/nav), built when missing; it skips without the
// demo pak, and under the race detector without the cache (a build takes
// minutes there) unless Q2_AGENT_LONG is set.
func demo(t testing.TB, name string) level {
	t.Helper()
	pk, err := pak.Open(testutil.DemoPak(t))
	if err != nil {
		t.Fatal(err)
	}
	fs := &pak.FS{}
	fs.AddPak(pk)
	t.Cleanup(func() { _ = fs.Close() })
	raw, err := fs.ReadFile("maps/" + name + ".bsp")
	if err != nil {
		t.Fatal(err)
	}
	md, err := mapdata.Load(name, raw, mapdata.Options{Skill: 1})
	if err != nil {
		t.Fatal(err)
	}
	params, dir := nav.DefaultParams(), nav.DefaultDir()
	if raceEnabled && os.Getenv("Q2_AGENT_LONG") == "" {
		g, err := nav.ReadFile(filepath.Join(dir, nav.CacheName(md.Name, md.Checksum, params)))
		if err != nil || g.Matches(md, params) != nil {
			t.Skipf("%s: no cached nav graph in %s (run q2nav build -all, or set Q2_AGENT_LONG=1)", name, dir)
		}
	}
	g, err := nav.NewStore(dir, navbuild.StoreBuilder(fs.ReadFile, navbuild.Config{})).Load(context.Background(), md, params)
	if err != nil {
		t.Fatal(err)
	}
	return level{md: md, g: g}
}

// fakeNav is a Navigator whose status the test sets; it records the
// goals, clears, retries and assumptions. Its map state is a real one
// over the level (fed with Update by the test).
type fakeNav struct {
	ms      *navrt.MapState
	goals   []navrt.Goal
	has     bool
	status  navrt.Status
	path    navrt.Path
	cur     int
	avoid   []int32
	clears  int
	retries int
	assumed map[int32]int
	refuse  bool
}

func newFakeNav(lv level) *fakeNav {
	return &fakeNav{ms: navrt.NewMapState(lv.g, lv.md), assumed: map[int32]int{}, status: navrt.Status{Edge: -1, WaitFor: -1}}
}

func (f *fakeNav) SetGoal(g navrt.Goal, now int64) error {
	if f.refuse {
		return navrt.ErrNoTarget
	}
	f.goals = append(f.goals, g)
	f.has = true
	f.status = navrt.Status{Follow: navrt.Following, Edge: -1, WaitFor: -1}
	return nil
}
func (f *fakeNav) ClearGoal() {
	f.has = false
	f.clears++
	f.status = navrt.Status{Follow: navrt.Idle, Edge: -1, WaitFor: -1}
}
func (f *fakeNav) Status() navrt.Status      { return f.status }
func (f *fakeNav) Retry()                    { f.retries++ }
func (f *fakeNav) SetAvoid(ents []int32)     { f.avoid = append([]int32(nil), ents...) }
func (f *fakeNav) Path() (navrt.Path, int)   { return f.path, f.cur }
func (f *fakeNav) MapState() *navrt.MapState { return f.ms }
func (f *fakeNav) last() navrt.Goal          { return f.goals[len(f.goals)-1] }
func (f *fakeNav) arrive()                   { f.status.Follow = navrt.Arrived }
func (f *fakeNav) follow(remaining float32) {
	f.status = navrt.Status{Follow: navrt.Following, Remaining: remaining}
}
func (f *fakeNav) fail(c navrt.Cause, why string) {
	f.status = navrt.Status{Follow: navrt.Failed, Cause: c, Reason: why}
}
func (f *fakeNav) Assume(b int32, pose int, now int64) bool {
	f.assumed[b] = pose
	return f.ms.Assume(b, pose, now)
}

// harness runs an executor on a level with a fake navigator and a belief
// the test edits; tick advances the clock by a frame and updates.
type harness struct {
	t   *testing.T
	lv  level
	nav *fakeNav
	x   *Executor
	b   worldmodel.Belief
	now int64
	d   Directive
}

func newHarness(t *testing.T, lv level, steps []route.Step, avoid ...route.Avoid) *harness {
	t.Helper()
	tab := &route.Table{Schema: route.SchemaVersion, Name: "test", Map: lv.md.Name, Steps: steps, Avoid: avoid}
	h := &harness{t: t, lv: lv, nav: newFakeNav(lv), now: 1000}
	x, err := New(tab, lv.md, lv.g, h.nav, Config{Logf: t.Logf})
	if err != nil {
		t.Fatal(err)
	}
	h.x = x
	h.b.Self.Health = 100
	x.Start(h.now)
	return h
}

// tick runs n frames (100 ms each) and returns the last directive.
func (h *harness) tick(n int) Directive {
	for i := 0; i < n; i++ {
		h.now += 100
		h.b.Time = h.now
		h.b.Frames++
		h.nav.ms.Update(&h.b, h.now)
		h.d = h.x.Update(h.now, &h.b)
	}
	return h.d
}

func (h *harness) wantStep(i int, st Status) {
	h.t.Helper()
	if h.x.Index() != i || (i < len(h.x.Steps()) && h.x.Current().Status != st) {
		h.t.Fatalf("at step %d (%s), want step %d %s", h.x.Index(), h.x.Current().Status, i, st)
	}
}

func ref(ent int) *route.Ref { return &route.Ref{Entity: &ent} }

func modelRef(m string) *route.Ref { return &route.Ref{Model: m} }

func vec(x, y, z float32) *route.Vec { return &route.Vec{x, y, z} }

func yaw(y float32) *float32 { return &y }
