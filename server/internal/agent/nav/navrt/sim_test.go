package navrt

import (
	"context"
	"math"
	"strings"
	"testing"

	"quake2web/server/internal/agent/control"
	"quake2web/server/internal/agent/mapdata"
	"quake2web/server/internal/agent/nav"
	"quake2web/server/internal/agent/nav/navbuild"
	"quake2web/server/internal/agent/nav/navsim"
	"quake2web/server/internal/agent/worldmodel"
	"quake2web/server/internal/bsp"
	"quake2web/server/internal/q2const"
)

// simBot closes the loop without a server: a navsim runner stands in for
// the server (its world may hold solids the navigator does not know), the
// navigator sees the runner's exact state, and its intents are composed
// into usercmds as the driver does.
type simBot struct {
	t    testing.TB
	g    *nav.Graph
	md   *mapdata.Map
	srv  *navsim.Runner
	nav  *Navigator
	b    worldmodel.Belief
	now  int64
	last navsim.StepResult
	have bool
	in   control.MoveIntent
}

// newSim builds the synthetic floor map (a 128x128 slab, a 5x5 grid of
// nodes 32 apart) with extra solids in the server's world only.
func newSim(t testing.TB, cfg Config, extra ...navsim.Solid) *simBot {
	t.Helper()
	raw := bsp.Encode(bsp.SyntheticFloorMap())
	md, err := mapdata.Load("synthetic", raw, mapdata.Options{Skill: 1})
	if err != nil {
		t.Fatal(err)
	}
	g, _, err := navbuild.Build(context.Background(), "synthetic", raw, navbuild.Config{Workers: 1})
	if err != nil {
		t.Fatal(err)
	}
	g = g.ForSkill(1)
	w := navsim.NewWorld(md.CM)
	w.SetSolids(append(append([]navsim.Solid(nil), g.Solids...), extra...))
	return &simBot{t: t, g: g, md: md, srv: g.NewRunner(w), nav: New(g, md, cfg)}
}

// nodeAt returns the node at x, y.
func (s *simBot) nodeAt(x, y float32) nav.NodeID {
	for i, n := range s.g.Nodes {
		if n.Origin[0] == x && n.Origin[1] == y {
			return nav.NodeID(i)
		}
	}
	s.t.Fatalf("no node at %v,%v", x, y)
	return nav.NoNode
}

func (s *simBot) tick() control.MoveIntent {
	st := s.srv.State()
	in := TickInput{Self: st, Belief: &s.b, Now: s.now}
	if s.have {
		in.Last = &s.last
	}
	it := s.nav.Tick(in)
	u := control.Compose(it, it.FaceYaw, it.FacePitch, st.PM.DeltaAngles, CmdMsec)
	res := s.srv.Step(cmdOf(u, st.PM.DeltaAngles))
	s.last.Touched = append(s.last.Touched[:0], res.Touched...)
	s.last.Inside = append(s.last.Inside[:0], res.Inside...)
	s.last.Teleported, s.have = res.Teleported, true
	s.now += CmdMsec
	if s.now%FrameMsec == 0 {
		s.b.Frames++
		s.b.Time = s.now
	}
	s.in = it
	return it
}

// run follows goal for at most msec and returns the status.
func (s *simBot) run(goal Goal, msec int64) Status {
	s.t.Helper()
	if err := s.nav.SetGoal(goal, s.now); err != nil {
		s.t.Fatal(err)
	}
	end := s.now + msec
	for s.now < end {
		s.tick()
		if st := s.nav.Status(); st.Follow == Arrived || st.Follow == Failed && s.nav.final {
			return st
		}
	}
	return s.nav.Status()
}

func TestSimFollowArrives(t *testing.T) {
	s := newSim(t, Config{})
	s.g.Place(s.srv, s.nodeAt(-64, -64))
	goal := s.nodeAt(64, 64)
	st := s.run(NodeGoal(goal), 5000)
	if st.Follow != Arrived {
		t.Fatalf("%s: %s %s", st.Follow, st.Cause, st.Reason)
	}
	if o := s.srv.State().Origin(); !navsim.Near(o, s.g.Nodes[goal].Origin) {
		t.Errorf("arrived at %v", o)
	}
	// the diagonal is 181 units: at 300 u/s with the start from rest
	// well under a second and a half
	if s.now > 1500 {
		t.Errorf("took %d ms", s.now)
	}
	if st.Stucks != 0 {
		t.Errorf("%d stucks on an empty slab", st.Stucks)
	}
}

// TestSimStuckOnUnseenObstacle: a pillar the navigator knows nothing about
// stands on the straight way (on the middle node): the bot gets stuck,
// recovers (at worst by marking the edge into the pillar blocked) and
// arrives the long way round.
func TestSimStuckOnUnseenObstacle(t *testing.T) {
	pillar := navsim.Solid{ID: 999, Box: true, Origin: Vec3{0, 0, 0}, Mins: Vec3{-12, -12, 0}, Maxs: Vec3{12, 12, 120}}
	s := newSim(t, Config{}, pillar)
	s.g.Place(s.srv, s.nodeAt(-64, 0))
	var first Status
	if err := s.nav.SetGoal(NodeGoal(s.nodeAt(64, 0)), 0); err != nil {
		t.Fatal(err)
	}
	for s.now < 20000 && s.nav.Status().Follow != Arrived && !s.nav.final {
		s.tick()
		if st := s.nav.Status(); st.Stucks == 1 && first.Stucks == 0 {
			first = st
		}
	}
	st := s.nav.Status()
	if st.Follow != Arrived {
		t.Fatalf("%s after %d ms: %s %s", st.Follow, s.now, st.Cause, st.Reason)
	}
	if first.Stucks != 1 || first.Cause != CauseWorld {
		t.Errorf("first stuck: %+v (the pillar is unknown: the world)", first)
	}
	t.Logf("arrived after %d ms, %d stucks, %d repaths, blocked %v; first: %s", s.now, st.Stucks, st.Repaths, s.nav.bl.Edges(s.now), first.Reason)
}

// TestSimEntityBlock: a visible monster on the way is an obstacle in the
// prediction world and its spot costs more, so the plan goes round it;
// pushed against it, the stuck classification names it.
func TestSimEntityBlock(t *testing.T) {
	s := newSim(t, Config{})
	s.b.Tracks = []worldmodel.Track{{ID: "e1", Class: "soldier", Kind: "monster", Pos: Vec3{0, 0, 24}, PosKnown: true,
		Mins: Vec3{-16, -16, -24}, Maxs: Vec3{16, 16, 32}, Visible: true, LastSeen: 1, Life: worldmodel.LifeAlive}}
	s.b.Frames, s.b.Time = 1, 1
	s.g.Place(s.srv, s.nodeAt(-64, 0))
	s.tick() // observe
	from, to := s.nodeAt(-64, 0), s.nodeAt(64, 0)
	p, ok := s.nav.pl.Find(from, CompileTarget(s.g, NodeGoal(to)), s.nav.cost)
	if !ok {
		t.Fatal("no path")
	}
	for _, i := range p.Edges {
		n := &s.g.Nodes[s.g.Edges[i].To]
		if boxTouch(n.Origin, false, Vec3{-24, -24, 0}, Vec3{24, 24, 56}) {
			t.Errorf("the path walks onto the monster's spot at %v", n.Origin)
		}
	}
	// pushed against it
	s.srv.Reset(Vec3{-33, 0, 24.125}, false)
	s.srv.Step(navsim.Cmd{})
	s.nav.st = s.srv.State()
	c, why := s.nav.classify(control.MoveIntent{WishDir: Vec3{1, 0, 0}, Speed: 400})
	if c != CauseEntity || !strings.Contains(why, "e1") || !strings.Contains(why, "soldier") {
		t.Errorf("classified %s: %s", c, why)
	}
}

func TestSimShootGoal(t *testing.T) {
	s := newSim(t, Config{})
	s.g.Place(s.srv, s.nodeAt(-64, 0))
	aim := Vec3{0, 0, 80}
	if err := s.nav.SetGoal(ShootGoal(4242, aim), 0); err != nil {
		t.Fatal(err)
	}
	fired := 0
	for s.now < 2000 {
		if in := s.tick(); in.Fire {
			fired++
			if !in.MustFace || math.Abs(float64(control.AngleDelta(in.FaceYaw, 0))) > 5 || in.FacePitch >= 0 {
				t.Fatalf("firing without facing the target: %+v", in)
			}
		}
	}
	if fired < 40 {
		t.Errorf("fired %d of 80 commands", fired)
	}
	// nothing reacts to the shots (entity 4242 is no button): it gives up
	for s.now < ShootTimeout+3000 && !s.nav.final {
		s.tick()
	}
	if st := s.nav.Status(); st.Follow != Failed || !s.nav.final {
		t.Errorf("after %d ms: %s %s", s.now, st.Follow, st.Reason)
	}
}

// doorBot is a navigator on lineGraph without a collision model: the
// follower's decisions on hand-made states.
func doorBot(t *testing.T) (*Navigator, *worldmodel.Belief) {
	g := lineGraph(t)
	n := New(g, nil, Config{})
	n.ms.info[0].auto, n.ms.info[0].travel = true, 1
	// door 0 hangs above the line (its path is clear of the nodes), door
	// 1 (use only) was seen open
	b := &worldmodel.Belief{Frames: 1, Movers: []worldmodel.Mover{
		{Model: "*1", Origin: Vec3{0, 0, 0}, LastUpdate: 0, Visible: true},
		{Model: "*2", Origin: Vec3{0, 0, 100}, LastUpdate: 0, Visible: true},
	}}
	return n, b
}

func restAt(p Vec3) navsim.State {
	st := navsim.State{Ground: 0, Mins: navsim.StandMins(), Maxs: navsim.StandMaxs(), ViewHeight: 22}
	st.PM.Origin = navsim.SnapOrigin(p)
	st.PM.PmFlags = q2const.PMF_ON_GROUND
	return st
}

// TestWaitForAutoDoor: before an edge through an auto door the follower
// stops at the edge start and waits; once the door is seen open it runs
// the edge; a door that stays shut past its travel time plus a second
// gets the edge marked blocked.
func TestWaitForAutoDoor(t *testing.T) {
	for _, opens := range []bool{true, false} {
		n, b := doorBot(t)
		at := restAt(n.g.Nodes[1].Origin)
		if err := n.SetGoal(NodeGoal(3), 0); err != nil {
			t.Fatal(err)
		}
		var in control.MoveIntent
		for now := int64(0); now <= 600; now += CmdMsec {
			b.Time, b.Frames = now, b.Frames+1
			b.Movers[0].LastUpdate = now
			in = n.Tick(TickInput{Self: at, Belief: b, Now: now})
		}
		if st := n.Status(); st.Follow != Waiting || st.WaitFor != 0 || !in.Stop || in.StopAt != n.g.Nodes[1].Origin {
			t.Fatalf("door shut: %+v, intent %+v", st, in)
		}
		if opens {
			b.Movers[0].Origin = Vec3{0, 0, 100}
			b.Time, b.Frames, b.Movers[0].LastUpdate = 625, b.Frames+1, 625
			in = n.Tick(TickInput{Self: at, Belief: b, Now: 625})
			if st := n.Status(); st.Follow != Following || in.Speed == 0 || in.Stop {
				t.Errorf("door open: %+v, intent %+v", st, in)
			}
			continue
		}
		for now := int64(625); now <= 2500; now += CmdMsec {
			b.Time, b.Frames = now, b.Frames+1
			b.Movers[0].LastUpdate = now
			in = n.Tick(TickInput{Self: at, Belief: b, Now: now})
		}
		e := edgeOf(n.g, 1, 2, nav.EdgeWalk)
		if !n.bl.Active(e, 2500) {
			t.Errorf("the edge through the shut door is not blocked: %+v", n.Status())
		}
		if st := n.Status(); st.Follow != Failed || st.Cause != CauseNoPath {
			t.Errorf("no way left: %+v", st)
		}
	}
}

// TestDirectionalFacing: near a directional trigger the path sets off,
// the view turns to the trigger's direction (MustFace), from well before
// the bot enters it.
func TestDirectionalFacing(t *testing.T) {
	g := lineGraph(t)
	g.Volumes = []nav.Volume{{Kind: nav.EffTrigger, Entity: 77, Pose: -1, Blocker: -1, Min: Vec3{20, 110, 0}, Max: Vec3{36, 150, 60}}}
	n := New(g, nil, Config{})
	n.dirYaw[77] = 90
	if err := n.SetGoal(NodeGoal(4), 0); err != nil {
		t.Fatal(err)
	}
	far := n.Tick(TickInput{Self: restAt(g.Nodes[0].Origin), Now: 0})
	if far.MustFace && far.FaceYaw == 90 {
		t.Errorf("facing the trigger from 110 units: %+v", far)
	}
	st := restAt(Vec3{8, 60, 24})
	near := n.Tick(TickInput{Self: st, Now: 25})
	if !near.MustFace || near.FaceYaw != 90 || near.FacePitch != 0 {
		t.Errorf("near the trigger: %+v", near)
	}
	if near.Speed == 0 {
		t.Error("it stopped instead of walking on facing the trigger")
	}
}

// TestTouchButtonGoal: a button goal runs the touch edge facing the
// button (MustFace) and is reached as soon as the prediction touched it.
func TestTouchButtonGoal(t *testing.T) {
	g := lineGraph(t)
	n := New(g, nil, Config{})
	at := restAt(g.Nodes[2].Origin)
	if err := n.SetGoal(TouchGoal(88), 0); err != nil {
		t.Fatal(err)
	}
	in := n.Tick(TickInput{Self: at, Now: 0})
	if !in.MustFace || in.Speed == 0 || math.Abs(float64(control.AngleDelta(in.FaceYaw, 0))) > 1 {
		t.Fatalf("pressing: %+v (%+v)", in, n.Status())
	}
	in = n.Tick(TickInput{Self: at, Now: 25, Last: &navsim.StepResult{Touched: []int{88}}})
	if st := n.Status(); st.Follow != Arrived || in.Speed != 0 {
		t.Errorf("touched: %+v %+v", st, in)
	}
}
