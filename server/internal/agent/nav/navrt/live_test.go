package navrt

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"quake2web/server/internal/agent/control"
	"quake2web/server/internal/agent/mapdata"
	"quake2web/server/internal/agent/nav"
	"quake2web/server/internal/agent/nav/navbuild"
	"quake2web/server/internal/agent/nav/navsim"
	"quake2web/server/internal/agent/perception"
	"quake2web/server/internal/agent/session"
	"quake2web/server/internal/agent/session/sessiontest"
	"quake2web/server/internal/agent/worldmodel"
	"quake2web/server/internal/assets/pak"
	"quake2web/server/internal/fakeclient"
	"quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/shared"
)

// liveBot is a navigator driving the bot on a lockstep server, the way
// the agent will: the session's frames go through perception into the
// world model, the driver turns the navigator's intents into usercmds.
// The bot is in god and notarget mode (client cheats, single player), so
// only the movement is tested.
type liveBot struct {
	t   testing.TB
	ctx context.Context
	fs  *pak.FS
	l   *session.Lockstep
	md  *mapdata.Map
	g   *nav.Graph
	wm  *worldmodel.World
	rd  *perception.Reader
	nav *Navigator
	drv *Driver
}

// demoMap loads the map data of a demo map (skill 1).
func demoMap(t testing.TB, fs *pak.FS, name string) *mapdata.Map {
	t.Helper()
	raw, err := fs.ReadFile("maps/" + name + ".bsp")
	if err != nil {
		t.Fatal(err)
	}
	md, err := mapdata.Load(name, raw, mapdata.Options{Skill: 1})
	if err != nil {
		t.Fatal(err)
	}
	return md
}

// demoGraph returns the skill-resolved nav graph from the shared cache
// (assets/nav), building it when missing; under the race detector a build
// takes minutes, so the test skips instead unless Q2_AGENT_LONG is set.
func demoGraph(t testing.TB, fs *pak.FS, md *mapdata.Map) *nav.Graph {
	t.Helper()
	params := nav.DefaultParams()
	dir := nav.DefaultDir()
	if raceEnabled && os.Getenv("Q2_AGENT_LONG") == "" {
		g, err := nav.ReadFile(filepath.Join(dir, nav.CacheName(md.Name, md.Checksum, params)))
		if err != nil || g.Matches(md, params) != nil {
			t.Skipf("%s: no cached nav graph in %s (run q2nav build -all, or set Q2_AGENT_LONG=1)", md.Name, dir)
		}
	}
	s := nav.NewStore(dir, navbuild.StoreBuilder(fs.ReadFile, navbuild.Config{}))
	s.Logf = t.Logf
	g, err := s.Load(context.Background(), md, params)
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func startBot(t testing.TB, name string, seed uint32, cfg Config) *liveBot {
	t.Helper()
	fs := sessiontest.DemoFS(t)
	md := demoMap(t, fs, name)
	g := demoGraph(t, fs, md)
	b := &liveBot{t: t, ctx: context.Background(), fs: fs, md: md, g: g}
	b.l = session.NewLockstep(session.LockstepConfig{FS: fs, Spec: session.Spec{Map: name, Skill: 1}, Seed: seed,
		Client: fakeclient.Options{MaxHistory: 256}})
	if err := b.l.Start(b.ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = b.l.Close() })
	c := b.l.Client()
	c.StringCmd("god")
	c.StringCmd("notarget")
	b.wm = worldmodel.New(worldmodel.Config{ReadFile: fs.ReadFile})
	b.wm.Reset(worldmodel.Level{Key: worldmodel.LevelKey{Map: name}, Map: md})
	b.rd = perception.NewReader()
	b.nav = New(g, md, cfg)
	b.drv = NewDriver(b.nav)
	b.observe()
	return b
}

func (b *liveBot) observe() {
	if in, ok := b.rd.Next(b.l.Client()); ok {
		now := b.l.GameTimeMs()
		b.wm.Update(in, now)
		b.drv.Observe(b.wm.Belief(), now)
	}
}

// step runs one server frame with the driver's commands.
func (b *liveBot) step() {
	b.t.Helper()
	if err := b.l.Step(b.ctx, b.drv.Cmd); err != nil {
		b.t.Fatal(err)
	}
	b.observe()
}

func (b *liveBot) now() int64 { return b.l.GameTimeMs() }

func (b *liveBot) origin() Vec3 { return b.l.Client().Origin() }

// runGoal sets goal and steps until the navigator arrives, fails for good
// or the budget (ms of game time) runs out. It returns the status and the
// game time it took. Q2_NAVRT_TRACE=1 logs every status change,
// Q2_NAVRT_TRACECMD=1 every command (with the predicted state).
func (b *liveBot) runGoal(goal Goal, budget int64) (Status, int64, error) {
	start := b.now()
	if err := b.nav.SetGoal(goal, start); err != nil {
		return Status{}, 0, err
	}
	trace := os.Getenv("Q2_NAVRT_TRACE") != ""
	if os.Getenv("Q2_NAVRT_TRACECMD") != "" {
		b.drv.OnCmd = func(in control.MoveIntent, u shared.UserCmd, s *navsim.State) {
			b.t.Logf("    cmd t=%d o=%v v=%v wl=%d ground=%d flags=%#x tm=%d | wish %v sp %.0f j%v c%v up%v mf%v face %.1f/%.1f | f%d s%d u%d", b.now()-start, s.Origin(), s.Velocity(), s.WaterLevel, s.Ground, s.PM.PmFlags, s.PM.PmTime,
				in.WishDir, in.Speed, in.Jump, in.Crouch, in.SwimUp, in.MustFace, in.FaceYaw, in.FacePitch, u.ForwardMove, u.SideMove, u.UpMove)
		}
	}
	var last Status
	for b.now()-start < budget {
		b.step()
		st := b.nav.Status()
		if trace && (st.Follow != last.Follow || st.Edge != last.Edge || st.Reason != last.Reason) {
			e := ""
			if st.Edge >= 0 {
				ed := &b.g.Edges[st.Edge]
				e = fmt.Sprintf("%s %v->%v reqs %v flags %v", ed.Kind, b.g.Nodes[ed.From].Origin, b.g.Nodes[ed.To].Origin, ed.Reqs, ed.Flags)
			}
			b.t.Logf("  t=%5d %v %s %s [%s] ph %d | %s", b.now()-start, b.origin(), st.Follow, st.Cause, st.Reason, b.nav.ph, e)
			last = st
		}
		if st.Follow == Arrived || (st.Follow == Failed && b.nav.final) {
			return st, b.now() - start, nil
		}
		if b.l.Client().MapName() != b.g.Map {
			return st, b.now() - start, fmt.Errorf("left the level for %s", b.l.Client().MapName())
		}
	}
	return b.nav.Status(), b.now() - start, nil
}

// connected returns the largest strongly connected part of the graph
// that is reachable from start, over the edges the navigator's cost
// function accepts in the level as it spawns (auto doors open as the bot
// walks up, nothing else changes): from any of its nodes the bot can get
// to any other.
func connected(n *Navigator, start nav.NodeID) []nav.NodeID {
	g := n.g
	ok := func(i int) bool { _, ok := n.cost(i, &g.Edges[i]); return ok }
	fwd := make([]bool, len(g.Nodes))
	stack := []nav.NodeID{start}
	fwd[start] = true
	for len(stack) > 0 {
		u := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		lo, hi := g.OutRange(u)
		for i := lo; i < hi; i++ {
			if v := g.Edges[i].To; !fwd[v] && ok(i) {
				fwd[v] = true
				stack = append(stack, v)
			}
		}
	}
	comp := sccs(g, ok)
	size := map[int]int{}
	for i, c := range comp {
		if fwd[i] {
			size[c]++
		}
	}
	best := comp[start]
	for c, sz := range size {
		if sz > size[best] || sz == size[best] && c < best {
			best = c
		}
	}
	var out []nav.NodeID
	for i := range g.Nodes {
		if fwd[i] && comp[i] == best {
			out = append(out, nav.NodeID(i))
		}
	}
	return out
}

func spawnNode(g *nav.Graph) nav.NodeID {
	for _, s := range g.Spawns {
		if s.Targetname == "" {
			return s.Node
		}
	}
	return g.Spawns[0].Node
}

type walkResult struct {
	target   nav.NodeID
	ok       bool
	msec     int64
	budget   int64
	planned  float32
	status   Status
	from, to Vec3
	end      Vec3
	err      error
}

// walkRandom walks the bot to count random nodes of the spawn's connected
// part of the graph, one after the other, each within the time budget the
// gate allows (planned seconds x 1.5 + 5 s).
func walkRandom(t *testing.T, name string, count int, seed int64) []walkResult {
	b := startBot(t, name, uint32(seed), Config{})
	b.nav.cfg.Avoid = ExitTriggers(b.g, b.md)
	for _, a := range b.nav.cfg.Avoid {
		b.nav.avoid[a] = true
	}
	start := spawnNode(b.g)
	cands := connected(b.nav, start)
	t.Logf("%s: %d of %d nodes in the largest strongly connected part reachable from the spawn, avoiding %v", name, len(cands), len(b.g.Nodes), b.nav.cfg.Avoid)
	rng := rand.New(rand.NewSource(seed))
	var res []walkResult
	for k := 0; k < count; k++ {
		from := b.origin()
		var target nav.NodeID
		for {
			target = cands[rng.Intn(len(cands))]
			if dist3(b.g.Nodes[target].Origin, from) > 160 {
				break
			}
		}
		here := b.nav.Localize(from, b.l.Client().Frame.PlayerState.PMove.PmFlags&q2const.PMF_DUCKED != 0)
		if here == nav.NoNode {
			here = b.g.Localize(from, 256) // the navigator walks back to the graph
		}
		goal := NodeGoal(target)
		// the budget comes from the plan in the level as it spawns
		// (without the edges the navigator marked blocked meanwhile)
		b.nav.now = b.now()
		marks := b.nav.bl
		b.nav.bl = NewBlocked()
		p, ok := b.nav.pl.Find(here, CompileTarget(b.g, goal), b.nav.cost)
		b.nav.bl = marks
		r := walkResult{target: target, from: from, to: b.g.Nodes[target].Origin}
		if !ok {
			r.err = fmt.Errorf("no plan from node %d", here)
			res = append(res, r)
			continue
		}
		r.planned = p.Cost
		r.budget = int64(p.Cost*1.5*1000) + 5000
		st, ms, err := b.runGoal(goal, r.budget)
		r.status, r.msec, r.err, r.end = st, ms, err, b.origin()
		r.ok = st.Follow == Arrived && err == nil
		res = append(res, r)
		if err != nil {
			break
		}
	}
	return res
}

func report(t *testing.T, name string, res []walkResult, need int) {
	pass := 0
	var fails []string
	for i, r := range res {
		if r.ok {
			pass++
			continue
		}
		fails = append(fails, fmt.Sprintf("  #%d node %d %v (from %v): %s after %.1fs of %.1fs budget (planned %.1fs), ended at %v: %s %s; %s (repaths %d, stucks %d) %v",
			i, r.target, r.to, r.from, r.status.Follow, float64(r.msec)/1000, float64(r.budget)/1000, r.planned, r.end,
			r.status.Cause, r.status.Reason, statusEdge(r.status), r.status.Repaths, r.status.Stucks, r.err))
	}
	t.Logf("%s: %d/%d arrivals", name, pass, len(res))
	if len(fails) > 0 {
		t.Logf("%s failures:\n%s", name, strings.Join(fails, "\n"))
	}
	if pass < need {
		t.Errorf("%s: %d/%d arrivals, the gate needs %d", name, pass, len(res), need)
	}
}

func statusEdge(s Status) string {
	if s.Edge < 0 {
		return "no edge"
	}
	return fmt.Sprintf("edge %d (step %d)", s.Edge, s.Step)
}

// TestGateWalkRandomNodes is the phase-3 gate: on demo1 the bot walks to
// 50 random nodes of the spawn's connected part of the graph and must
// arrive at 48 within the time budget; on demo2 and demo3 at 18 of 20.
func TestGateWalkRandomNodes(t *testing.T) {
	if testing.Short() {
		t.Skip("lockstep gate (seconds of wall time)")
	}
	for _, c := range []struct {
		name        string
		count, need int
	}{{"demo1", 50, 48}, {"demo2", 20, 18}, {"demo3", 20, 18}} {
		t.Run(c.name, func(t *testing.T) {
			res := walkRandom(t, c.name, c.count, 7)
			report(t, c.name, res, c.need)
		})
	}
}

// sccs labels the strongly connected components of the graph over the
// edges ok accepts (Tarjan, iterative).
func sccs(g *nav.Graph, ok func(i int) bool) []int {
	n := len(g.Nodes)
	index := make([]int, n)
	low := make([]int, n)
	on := make([]bool, n)
	comp := make([]int, n)
	for i := range index {
		index[i] = -1
		comp[i] = -1
	}
	var stack []int
	next, ncomp := 0, 0
	type frame struct{ v, e, hi int }
	for root := 0; root < n; root++ {
		if index[root] >= 0 {
			continue
		}
		lo, hi := g.OutRange(nav.NodeID(root))
		call := []frame{{root, lo, hi}}
		index[root], low[root] = next, next
		next++
		stack = append(stack, root)
		on[root] = true
		for len(call) > 0 {
			f := &call[len(call)-1]
			if f.e < f.hi {
				i := f.e
				f.e++
				if !ok(i) {
					continue
				}
				w := int(g.Edges[i].To)
				if index[w] < 0 {
					index[w], low[w] = next, next
					next++
					stack = append(stack, w)
					on[w] = true
					lo, hi := g.OutRange(nav.NodeID(w))
					call = append(call, frame{w, lo, hi})
				} else if on[w] {
					low[f.v] = min(low[f.v], index[w])
				}
				continue
			}
			v := f.v
			if low[v] == index[v] {
				for {
					w := stack[len(stack)-1]
					stack = stack[:len(stack)-1]
					on[w] = false
					comp[w] = ncomp
					if w == v {
						break
					}
				}
				ncomp++
			}
			call = call[:len(call)-1]
			if len(call) > 0 {
				p := call[len(call)-1].v
				low[p] = min(low[p], low[v])
			}
		}
	}
	return comp
}

// TestRouteSmokeDemo1Car walks from the demo1 spawn into the elevator car
// *31 (the first leg of the demo1 route; pressing its button *34 is the
// route executor's job): the bot must end standing on the car, at its top
// pose, with the exits avoided.
func TestRouteSmokeDemo1Car(t *testing.T) {
	if testing.Short() {
		t.Skip("lockstep run")
	}
	b := startBot(t, "demo1", 1, Config{})
	avoid := ExitTriggers(b.g, b.md)
	b.nav.cfg.Avoid = avoid
	for _, a := range avoid {
		b.nav.avoid[a] = true
	}
	car := b.md.ByModel("*31")
	if car == nil {
		t.Fatal("no *31 in demo1")
	}
	bl := b.g.BlockerOf(car.Index)
	if bl < 0 {
		t.Fatal("*31 is not a blocker")
	}
	nodes := b.g.MoverNodes(bl, 0)
	if len(nodes) == 0 {
		t.Fatal("no nodes on *31")
	}
	goal := NodeGoal(nodes...)
	b.nav.now = b.now()
	p, ok := b.nav.pl.Find(spawnNode(b.g), CompileTarget(b.g, goal), b.nav.cost)
	if !ok {
		t.Fatal("no path from the spawn to the car")
	}
	budget := int64(p.Cost*1.5*1000) + 5000
	st, ms, err := b.runGoal(goal, budget)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("car reached in %.1fs (planned %.1fs, budget %.1fs): %s, %d repaths, %d stucks", float64(ms)/1000, p.Cost, float64(budget)/1000, st.Follow, st.Repaths, st.Stucks)
	if st.Follow != Arrived {
		t.Fatalf("did not reach the car: %s %s %s at %v", st.Follow, st.Cause, st.Reason, b.origin())
	}
	// standing on the car: the ground under the bot is the car's brush
	for i := 0; i < 5; i++ {
		b.step()
	}
	o := b.origin()
	lo, hi := b.nav.ms.Sweep(bl)
	if o[0] < lo[0] || o[0] > hi[0] || o[1] < lo[1] || o[1] > hi[1] {
		t.Fatalf("the bot at %v is not over the car (%v..%v)", o, lo, hi)
	}
	pm := b.l.Client().Frame.PlayerState.PMove
	if pm.PmFlags&q2const.PMF_ON_GROUND == 0 {
		t.Fatalf("not standing (flags %#x)", pm.PmFlags)
	}
	if bb := b.nav.ms.Belief(bl); bb.Status != BlockerAt || bb.Pose != 0 {
		t.Errorf("the car is believed %s at pose %d, want resting at its top", bb.Status, bb.Pose)
	}
	if b.l.Client().MapName() != "demo1" {
		t.Errorf("left demo1")
	}
}

// TestWalkSeedsLong repeats the random walk with other seeds, 50 targets
// per map and seed (about a minute of wall time; Q2_AGENT_LONG=1). With
// notarget the monsters stand still, so a few of them parked in one-wide
// corridors block every way through; it needs 90%.
func TestWalkSeedsLong(t *testing.T) {
	if os.Getenv("Q2_AGENT_LONG") == "" {
		t.Skip("long: set Q2_AGENT_LONG=1")
	}
	for _, name := range []string{"demo1", "demo2", "demo3"} {
		for seed := int64(1); seed <= 5; seed++ {
			t.Run(fmt.Sprintf("%s-%d", name, seed), func(t *testing.T) {
				report(t, name, walkRandom(t, name, 50, seed), 45)
			})
		}
	}
}
