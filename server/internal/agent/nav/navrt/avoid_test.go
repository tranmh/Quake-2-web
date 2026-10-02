package navrt

import (
	"math"
	"strings"
	"testing"

	"quake2web/server/internal/agent/control"
	"quake2web/server/internal/agent/nav"
	"quake2web/server/internal/agent/nav/navsim"
	"quake2web/server/internal/agent/worldmodel"
)

// cornerBox is a trigger volume inside the corner of an L on the
// synthetic slab (east along y=-64 to x=32, then north along x=32): no
// edge of the L comes near it, the pursuit's corner cut runs right
// through it.
func cornerBox() nav.Volume {
	return nav.Volume{Kind: nav.EffTrigger, Entity: 777, Pose: -1, Blocker: -1, Min: Vec3{-24, -34, 0}, Max: Vec3{-12, -22, 64}}
}

// lSim is the synthetic slab with cornerBox added and every edge but the
// L's marked blocked, the bot at rest on the L's start.
func lSim(t *testing.T, cfg Config) *simBot {
	t.Helper()
	s := newSimGraph(t, cfg, func(g *nav.Graph) { g.Volumes = append(g.Volumes, cornerBox()) })
	row := func(p Vec3) bool { return p[1] == -64 && p[0] <= 32 }
	col := func(p Vec3) bool { return p[0] == 32 }
	for i := range s.g.Edges {
		e := &s.g.Edges[i]
		a, b := s.g.Nodes[e.From].Origin, s.g.Nodes[e.To].Origin
		if e.Kind == nav.EdgeWalk && (row(a) && row(b) && b[0]-a[0] == 32 || col(a) && col(b) && b[1]-a[1] == 32) {
			if e.HasEffect(int(cornerBox().Entity)) {
				t.Fatalf("edge %s sets the corner trigger off", EdgeKey(e))
			}
			continue
		}
		s.nav.bl.Mark(i, nil, s.nav.ms, 0)
	}
	s.g.Place(s.srv, s.nodeAt(-64, -64))
	return s
}

// walkL follows the L to its end and reports whether the bot arrived and
// whether the server's player box touched cornerBox on the way.
func walkL(t *testing.T, s *simBot) (arrived, touched bool) {
	t.Helper()
	if err := s.nav.SetGoal(NodeGoal(s.nodeAt(32, 64)), s.now); err != nil {
		t.Fatal(err)
	}
	for end := s.now + 6000; s.now < end; {
		s.tick()
		st := s.srv.State()
		if box := cornerBox(); boxTouch(st.Origin(), st.Ducked(), box.Min, box.Max) {
			touched = true
		}
		if s.nav.Status().Follow == Arrived {
			return true, touched
		}
	}
	return false, touched
}

// TestSimAvoidCornerCut: the pursuit lookahead cuts the L's corner through
// a trigger no edge sets off; with the trigger avoided it keeps to the
// edges and still arrives.
func TestSimAvoidCornerCut(t *testing.T) {
	arrived, touched := walkL(t, lSim(t, Config{}))
	if !arrived || !touched {
		t.Fatalf("control run: arrived %v, touched the corner trigger %v (the test needs the cut to cross it)", arrived, touched)
	}
	s := lSim(t, Config{Avoid: []int32{cornerBox().Entity}})
	arrived, touched = walkL(t, s)
	if touched {
		t.Error("the bot cut the corner through the avoided trigger")
	}
	if !arrived {
		st := s.nav.Status()
		t.Errorf("did not arrive avoiding the trigger: %s %s %s", st.Follow, st.Cause, st.Reason)
	}
	t.Logf("arrived avoiding the trigger after %d ms", s.now)
}

// TestAvoidHit checks the straight-line test against the avoided boxes:
// contact, the margin, a box the bot already touches, and a volume whose
// mover is elsewhere.
func TestAvoidHit(t *testing.T) {
	g := lineGraph(t)
	g.Volumes = []nav.Volume{
		{Kind: nav.EffTrigger, Entity: 70, Pose: -1, Blocker: -1, Min: Vec3{100, -10, 0}, Max: Vec3{120, 10, 60}},
		// an item on door 0 at its open pose (the door is closed)
		{Kind: nav.EffItem, Entity: 71, Pose: 1, Blocker: 0, Min: Vec3{100, 100, 0}, Max: Vec3{120, 120, 60}},
	}
	n := New(g, nil, Config{Avoid: []int32{70, 71}})
	n.ms.bel[0] = BlockerBelief{Status: BlockerAt, Pose: 0, Observed: true}
	z := float32(24)
	cases := []struct {
		name   string
		a, b   Vec3
		margin float32
		want   bool
	}{
		{"through", Vec3{0, 0, z}, Vec3{200, 0, z}, 0, true},
		{"short of it", Vec3{0, 0, z}, Vec3{80, 0, z}, 0, false},
		// the hull reaches 17 units (half width and the contact unit)
		{"brushing", Vec3{0, 26, z}, Vec3{200, 26, z}, 0, true},
		{"clear by 10", Vec3{0, 37, z}, Vec3{200, 37, z}, 0, false},
		{"within the margin", Vec3{0, 33, z}, Vec3{200, 33, z}, avoidMargin, true},
		{"already inside", Vec3{110, 0, z}, Vec3{200, 0, z}, avoidMargin, false},
		{"mover elsewhere", Vec3{110, 0, z}, Vec3{110, 200, z}, 0, false},
	}
	for _, c := range cases {
		if got := n.avoidHit(c.a, c.b, false, c.margin); got != c.want {
			t.Errorf("%s: avoidHit = %v, want %v", c.name, got, c.want)
		}
	}
	// the door seen open puts the item there
	n.ms.bel[0] = BlockerBelief{Status: BlockerAt, Pose: 1, Observed: true}
	if !n.avoidHit(Vec3{110, 0, z}, Vec3{110, 200, z}, false, 0) {
		t.Error("the item on the open door is not avoided")
	}
}

// TestAvoidManoeuvres: a sidestep, a back-off, a step off a monster and
// the walk back to the graph do not head into an avoided trigger.
func TestAvoidManoeuvres(t *testing.T) {
	// a trigger along the bot's left (+y) on the slab
	vol := nav.Volume{Kind: nav.EffTrigger, Entity: 778, Pose: -1, Blocker: -1, Min: Vec3{-80, 20, 0}, Max: Vec3{80, 30, 64}}
	cfg := Config{Avoid: []int32{vol.Entity}}
	s := newSimGraph(t, cfg, func(g *nav.Graph) { g.Volumes = append(g.Volumes, vol) })
	// the direction choices on a navigator without a collision model (no
	// floor checks: the slab is too small for a 0.4 s run in most
	// directions)
	n := New(s.g, nil, cfg)
	n.st = restAt(Vec3{0, -32, 24})
	east, north, south := Vec3{1, 0, 0}, Vec3{0, 1, 0}, Vec3{0, -1, 0}
	for k := 0; k < 4; k++ {
		n.stuck.man = manNone
		n.strafe(east)
		if n.stuck.man != manStrafe || n.stuck.manDir != south {
			t.Fatalf("strafe %d: man %d dir %v, want a sidestep south, away from the trigger", k, n.stuck.man, n.stuck.manDir)
		}
	}
	if n.safeDir(north, 300) || !n.safeDir(south, 300) {
		t.Error("safeDir: the back-off towards the trigger is allowed, or the one away refused")
	}
	if d, ok := n.openDir(north, 400); !ok || d[1] > 0 {
		t.Errorf("openDir towards the trigger: %v %v", d, ok)
	}
	n.SetAvoid(nil)
	if d, ok := n.openDir(north, 400); !ok || d != north {
		t.Errorf("openDir with nothing avoided: %v %v", d, ok)
	}
	// off the slab's graph north of the trigger: localization and the walk
	// back do not cross it
	s.srv.Reset(Vec3{0, 60, 24}, false)
	s.srv.Step(navsim.Cmd{})
	s.nav.st = s.srv.State()
	if id := s.nav.Localize(s.nav.st.Origin(), false); id != nav.NoNode && s.g.Nodes[id].Origin[1] < 20 {
		t.Errorf("localized across the trigger at %v", s.g.Nodes[id].Origin)
	}
	s.nav.offSince, s.nav.now = 0, 0
	if in := s.nav.walkOff(); in.Speed > 0 && in.WishDir[1] < 0 {
		t.Errorf("walking back to the graph through the trigger: %+v", in)
	}
}

// TestSetAvoid: changing the avoid set replans the current goal, keeps
// the blocked marks, and undoing it brings the way back.
func TestSetAvoid(t *testing.T) {
	g := lineGraph(t)
	n := New(g, nil, Config{})
	n.ms.info[0].auto, n.ms.info[0].travel = true, 1
	at := restAt(g.Nodes[0].Origin)
	// door 1 is shut: the only way to node 3 is round by node 4, through
	// trigger #77
	if err := n.SetGoal(NodeGoal(3), 0); err != nil {
		t.Fatal(err)
	}
	n.Tick(TickInput{Self: at, Now: 0})
	via := edgeOf(g, 0, 4, nav.EdgeWalk)
	if p, _ := n.Path(); len(p.Edges) == 0 || p.Edges[0] != via {
		t.Fatalf("path %+v, want it through edge 0->4", p)
	}
	e12 := edgeOf(g, 1, 2, nav.EdgeWalk)
	n.MarkBlocked(e12, 0)
	n.SetAvoid([]int32{77})
	if got := n.Avoid(); len(got) != 1 || got[0] != 77 {
		t.Errorf("Avoid() = %v", got)
	}
	n.Tick(TickInput{Self: at, Now: 25})
	if st := n.Status(); st.Follow != Failed || st.Cause != CauseNoPath {
		t.Errorf("trigger #77 avoided: %s %s %s, want no path", st.Follow, st.Cause, st.Reason)
	}
	if !n.Blocked().Active(e12, 25) {
		t.Error("SetAvoid dropped the blocked marks")
	}
	n.SetAvoid(nil)
	n.Tick(TickInput{Self: at, Now: 50})
	if p, _ := n.Path(); n.Status().Follow != Following || len(p.Edges) == 0 || p.Edges[0] != via {
		t.Errorf("avoid set cleared: %+v, path %+v", n.Status(), p)
	}
}

// TestHazardCost: a hazard shortcut costs its damage plus HazardPenalty,
// so the dry detour wins unless it is much longer; hazard and barrel
// walks are no plain walks (no corner cut across them).
func TestHazardCost(t *testing.T) {
	g := &nav.Graph{
		Format: nav.FormatVersion, Map: "hazard", Params: nav.DefaultParams(), Skill: 1,
		Nodes: []nav.Node{
			{Origin: nav.Vec3{0, 0, 24}, Blocker: -1},
			{Origin: nav.Vec3{0, 300, 24}, Blocker: -1},
			{Origin: nav.Vec3{300, 0, 24}, Blocker: -1},
		},
		Edges: []nav.Edge{
			{From: 0, To: 1, Kind: nav.EdgeWalk, Recipe: navsim.RecipeWalk, Cost: 2},
			{From: 0, To: 2, Kind: nav.EdgeWalk, Recipe: navsim.RecipeWalk, Cost: 1, Flags: nav.EdgeHazard, Damage: 10},
			{From: 1, To: 2, Kind: nav.EdgeWalk, Recipe: navsim.RecipeWalk, Cost: 2},
		},
	}
	if err := g.Finish(); err != nil {
		t.Fatal(err)
	}
	hz := edgeOf(g, 0, 2, nav.EdgeWalk)
	n := New(g, nil, Config{})
	if c, _ := n.cost(hz, &g.Edges[hz]); math.Abs(float64(c-(1+DefaultHazardPenalty+10*DefaultDamageCost))) > 1e-5 {
		t.Errorf("hazard edge cost %v", c)
	}
	if p, ok := planTo(t, n, 0, NodeGoal(2)); !ok || len(p.Edges) != 2 {
		t.Errorf("the planner took the hazard shortcut: %+v %v", p, ok)
	}
	// the only way left: through the hazard
	n.MarkBlocked(edgeOf(g, 0, 1, nav.EdgeWalk), 0)
	if p, ok := planTo(t, n, 0, NodeGoal(2)); !ok || len(p.Edges) != 1 || p.Edges[0] != hz {
		t.Errorf("no way through the hazard when it is the only one: %+v %v", p, ok)
	}
	// a small penalty makes the shortcut worth it
	n = New(g, nil, Config{HazardPenalty: 0.5})
	if p, ok := planTo(t, n, 0, NodeGoal(2)); !ok || len(p.Edges) != 1 {
		t.Errorf("HazardPenalty 0.5: %+v %v", p, ok)
	}
	for _, f := range []nav.EdgeFlags{nav.EdgeHazard, nav.EdgePushes} {
		e := g.Edges[0]
		e.Flags = f
		if n.plainNow(&e) {
			t.Errorf("a %v walk is pursued as a plain walk", f)
		}
	}
}

// TestLandingVerdictIgnoresMonsters: the cached check from rest runs in
// the edge's validation world, so a monster standing on the end node then
// does not take the edge away for the level; the check from the bot's
// state does see it.
func TestLandingVerdictIgnoresMonsters(t *testing.T) {
	s := newSim(t, Config{})
	from, to := s.nodeAt(-64, 0), s.nodeAt(-32, 0)
	i := edgeOf(s.g, from, to, nav.EdgeWalk)
	if i < 0 {
		t.Fatal("no walk edge")
	}
	e := &s.g.Edges[i]
	s.g.Place(s.srv, from)
	s.b.Tracks = []worldmodel.Track{{ID: "e1", Class: "soldier", Kind: "monster", Pos: s.g.Nodes[to].Origin, PosKnown: true,
		Mins: Vec3{-16, -16, -24}, Maxs: Vec3{16, 16, 32}, Visible: true, LastSeen: 1, Life: worldmodel.LifeAlive}}
	s.b.Frames, s.b.Time = 1, 1
	s.tick() // observe: the soldier is in the prediction world
	if len(s.nav.tracks) != 1 {
		t.Fatalf("the soldier is not in the prediction world: %v", s.nav.tracks)
	}
	if s.nav.trial(i, e, false, false) {
		t.Error("the check from the bot's state walked through the soldier")
	}
	if !s.nav.trial(i, e, true, true) {
		t.Error("the cached check from rest failed with the soldier on the end node")
	}
	if v, ok := s.nav.landing[i]; !ok || !v {
		t.Errorf("cached verdict %v %v", v, ok)
	}
	if _, ok := s.nav.cost(i, e); !ok {
		t.Error("the edge is left out")
	}
}

// TestDirectionalFacingKeepsRecipes: near a directional trigger an
// executor's view only gets its yaw turned (its pitch stays), and the
// view of a ladder, swim or water jump step is left alone.
func TestDirectionalFacingKeepsRecipes(t *testing.T) {
	g := lineGraph(t)
	g.Volumes = []nav.Volume{{Kind: nav.EffTrigger, Entity: 77, Pose: -1, Blocker: -1, Min: Vec3{20, 110, 0}, Max: Vec3{36, 150, 60}}}
	n := New(g, nil, Config{})
	n.dirYaw[77] = 90
	if err := n.SetGoal(NodeGoal(4), 0); err != nil {
		t.Fatal(err)
	}
	n.Tick(TickInput{Self: restAt(Vec3{8, 60, 24}), Now: 0})
	exec := control.MoveIntent{WishDir: Vec3{0, 1, 0}, Speed: 400, MustFace: true, FaceYaw: 80, FacePitch: 30}
	in := exec
	n.faceDirectional(&in)
	if !in.MustFace || in.FaceYaw != 90 || in.FacePitch != 30 || in.WishDir != exec.WishDir {
		t.Errorf("executor view near the trigger: %+v", in)
	}
	cur := &g.Edges[n.path.Edges[n.cur]]
	for _, r := range []navsim.Recipe{navsim.RecipeLadder, navsim.RecipeSwim, navsim.RecipeWaterJump} {
		saved := cur.Recipe
		cur.Recipe = r
		in = exec
		n.faceDirectional(&in)
		cur.Recipe = saved
		if in != exec {
			t.Errorf("%v step: the view was changed: %+v", r, in)
		}
	}
	n.st.WaterLevel = 2
	in = exec
	n.faceDirectional(&in)
	if in != exec {
		t.Errorf("in the water: the view was changed: %+v", in)
	}
}

// TestFellOffPlainWalk: a bot that comes down from a flight well below
// the plain walk it was on relocalizes at once (it fell off a ledge);
// standing that low without a flight (a path that starts up a ramp) is no
// fall, and a short hop is none either.
func TestFellOffPlainWalk(t *testing.T) {
	air := func(p Vec3) navsim.State {
		st := restAt(p)
		st.Ground, st.PM.PmFlags = -1, 0
		return st
	}
	for _, c := range []struct {
		name   string
		flight int     // commands in the air before landing
		dz     float32 // landing height relative to the walk
		fell   bool
	}{
		{"fell 64 units", 8, -64, true},
		{"hop", 2, -64, false},
		{"flight onto the walk's level", 8, -10, false},
		{"low without a flight", 0, -64, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			g := lineGraph(t)
			n := New(g, nil, Config{})
			n.ms.bel[0] = BlockerBelief{Status: BlockerAt, Pose: 1, Observed: true}
			n.ms.bel[1] = BlockerBelief{Status: BlockerAt, Pose: 1, Observed: true}
			if err := n.SetGoal(NodeGoal(3), 0); err != nil {
				t.Fatal(err)
			}
			now := int64(0)
			n.Tick(TickInput{Self: restAt(g.Nodes[0].Origin), Now: now})
			for k := 0; k < c.flight; k++ {
				now += CmdMsec
				n.Tick(TickInput{Self: air(Vec3{4 + float32(k), 0, 24 + c.dz*float32(k)/float32(c.flight)}), Now: now})
			}
			now += CmdMsec
			n.Tick(TickInput{Self: restAt(Vec3{4 + float32(c.flight), 0, 24 + c.dz}), Now: now})
			fell := n.replan && strings.HasPrefix(n.replanWhy, "fell off")
			if fell != c.fell {
				t.Errorf("relocalized for a fall: %v (%q), want %v", fell, n.replanWhy, c.fell)
			}
		})
	}
}

// TestDropTouchDownOnTheWay: a drop that touches down on a lip for a
// command or two and falls on to its end is not a wrong landing (the
// builder's simulation goes through such touch-downs too); one that
// settles on the lip is, and gets the edge marked blocked.
func TestDropTouchDownOnTheWay(t *testing.T) {
	air := func(p Vec3) navsim.State {
		st := restAt(p)
		st.Ground, st.PM.PmFlags = -1, 0
		return st
	}
	for _, c := range []struct {
		name  string
		onLip int // commands on the lip
		wrong bool
	}{{"touch-down", 2, false}, {"settled", landSettle, true}} {
		t.Run(c.name, func(t *testing.T) {
			g := &nav.Graph{
				Format: nav.FormatVersion, Map: "drop", Params: nav.DefaultParams(), Skill: 1,
				Nodes: []nav.Node{{Origin: nav.Vec3{0, 0, 24}, Blocker: -1}, {Origin: nav.Vec3{64, 0, -100}, Blocker: -1}},
				Edges: []nav.Edge{{From: 0, To: 1, Kind: nav.EdgeDrop, Recipe: navsim.RecipeWalk, Cost: 1}},
			}
			if err := g.Finish(); err != nil {
				t.Fatal(err)
			}
			n := New(g, nil, Config{})
			if err := n.SetGoal(NodeGoal(1), 0); err != nil {
				t.Fatal(err)
			}
			now := int64(0)
			tick := func(st navsim.State) {
				n.Tick(TickInput{Self: st, Now: now})
				now += CmdMsec
			}
			tick(restAt(g.Nodes[0].Origin))
			for k := 0; k < 6; k++ {
				tick(air(Vec3{8 + 4*float32(k), 0, 24 - 5*float32(k)}))
			}
			for k := 0; k < c.onLip; k++ {
				tick(restAt(Vec3{40, 0, -6}))
			}
			if got := n.Blocked().Active(0, now); got != c.wrong {
				t.Fatalf("after %d commands on the lip: drop marked blocked %v, want %v (%+v)", c.onLip, got, c.wrong, n.Status())
			}
			if c.wrong {
				return
			}
			for k := 0; k < 6; k++ {
				tick(air(Vec3{44 + 3*float32(k), 0, -6 - 15*float32(k)}))
			}
			tick(restAt(g.Nodes[1].Origin))
			if st := n.Status(); st.Follow != Arrived {
				t.Errorf("landed on the drop's end: %s %s %s", st.Follow, st.Cause, st.Reason)
			}
		})
	}
}
