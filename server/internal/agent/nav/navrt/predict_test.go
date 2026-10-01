package navrt

import (
	"math"
	"math/rand"
	"testing"

	"quake2web/server/internal/agent/control"
	"quake2web/server/internal/agent/nav/navsim"
	"quake2web/server/internal/bsp"
	"quake2web/server/internal/cmodel"
	"quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/shared"
)

func floorMap(t testing.TB) *cmodel.Map {
	t.Helper()
	m, err := cmodel.LoadMapBytes("synthetic", bsp.Encode(bsp.SyntheticFloorMap()))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// TestCmdOfRoundTrip: a usercmd turned back into a navsim command sends
// exactly the same usercmd, so replays run what the server ran.
func TestCmdOfRoundTrip(t *testing.T) {
	rng := rand.New(rand.NewSource(4))
	for i := 0; i < 20000; i++ {
		var delta [3]int16
		for k := range delta {
			delta[k] = int16(rng.Intn(65536) - 32768)
		}
		u := shared.UserCmd{Msec: uint8(1 + rng.Intn(250)), Buttons: uint8(rng.Intn(256)),
			ForwardMove: int16(rng.Intn(801) - 400), SideMove: int16(rng.Intn(801) - 400), UpMove: int16(rng.Intn(801) - 400)}
		u.Angles = control.CmdAngles(float32(rng.Float64()*720-360), float32(rng.Float64()*178-89), delta)
		if got := cmdOf(u, delta).UserCmd(delta); got != u {
			t.Fatalf("delta %v: %+v came back as %+v", delta, u, got)
		}
	}
}

// TestPredictorReplay: Predict replays exactly the sent commands after the
// acknowledged one, in order, and stops at a gap in the ring.
func TestPredictorReplay(t *testing.T) {
	w := navsim.NewWorld(floorMap(t))
	r := navsim.NewRunner(w, navsim.DefaultPhysics())
	if !r.Settle(Vec3{-40, 0, 30}, false, 2000) {
		t.Fatal("no rest")
	}
	start := r.State()
	ps := shared.PlayerState{PMove: start.PM}
	cmds := make([]shared.UserCmd, 3)
	for k := range cmds {
		cmds[k] = shared.UserCmd{Msec: 25, ForwardMove: 400, SideMove: int16(100 * k)}
		cmds[k].Angles = control.CmdAngles(float32(10*k), 0, start.PM.DeltaAngles)
	}
	want := start
	ref := navsim.NewRunner(w, navsim.DefaultPhysics())
	ref.SetState(start, false)
	for _, u := range cmds {
		ref.Step(cmdOf(u, start.PM.DeltaAngles))
	}
	want = ref.State()

	p := NewPredictor(navsim.NewRunner(w, navsim.DefaultPhysics()))
	for k, u := range cmds {
		p.Sent(100+k, u)
	}
	if got := p.Predict(&ps, 99, 103); got.PM != want.PM {
		t.Errorf("replay of 3: %+v, want %+v", got.PM, want.PM)
	}
	if got := p.Predict(&ps, 102, 103); got.PM != start.PM {
		t.Errorf("all acknowledged: %+v", got.PM)
	}
	// a stale ring entry (another sequence in the slot) ends the replay
	p.Sent(101+CmdBackup, cmds[1])
	ref.SetState(start, false)
	ref.Step(cmdOf(cmds[0], start.PM.DeltaAngles))
	if got := p.Predict(&ps, 99, 103); got.PM != ref.State().PM {
		t.Errorf("gap: %+v, want %+v", got.PM, ref.State().PM)
	}
	// a frozen player is not predicted
	ps.PMove.PmType = q2const.PM_FREEZE
	if got := p.Predict(&ps, 99, 103); got.PM != ps.PMove {
		t.Errorf("frozen: %+v", got.PM)
	}
	// Step simulates one command from a state
	st, res := p.Step(start, cmds[0])
	ref.SetState(start, false)
	ref.Step(cmdOf(cmds[0], start.PM.DeltaAngles))
	if st.PM != ref.State().PM || res == nil {
		t.Errorf("Step: %+v", st.PM)
	}
}

// TestPredictionMatchesServer drives the bot on demo1 with the navigator
// and compares, every frame, where the predictor says the frame's four
// commands take the player with the player state the server sends next:
// nearly always bit for bit.
func TestPredictionMatchesServer(t *testing.T) {
	if testing.Short() {
		t.Skip("lockstep run")
	}
	b := startBot(t, "demo1", 3, Config{})
	b.nav.cfg.Avoid = ExitTriggers(b.g, b.md)
	for _, a := range b.nav.cfg.Avoid {
		b.nav.avoid[a] = true
	}
	var pred Vec3
	have := false
	sub := 0
	b.drv.OnCmd = func(in control.MoveIntent, u shared.UserCmd, s *navsim.State) {
		sub++
		if sub%4 != 0 {
			return
		}
		st, _ := b.drv.Pred.Step(*s, u)
		pred, have = st.Origin(), true
	}
	cands := connected(b.nav, spawnNode(b.g))
	rng := rand.New(rand.NewSource(3))
	frames, exact, near := 0, 0, 0
	worst := 0.0
	for goal := 0; goal < 8; goal++ {
		target := cands[rng.Intn(len(cands))]
		if err := b.nav.SetGoal(NodeGoal(target), b.now()); err != nil {
			t.Fatal(err)
		}
		for f := 0; f < 150 && b.nav.Status().Follow != Arrived; f++ {
			have = false
			b.step()
			if !have {
				continue
			}
			got := b.origin()
			d := float64(dist3(got, pred))
			frames++
			switch {
			case d == 0:
				exact++
				near++
			case d <= 1:
				near++
			}
			worst = math.Max(worst, d)
		}
	}
	t.Logf("%d frames: %d predicted exactly, %d within a unit, worst %.2f units", frames, exact, near, worst)
	if frames < 300 {
		t.Fatalf("only %d frames", frames)
	}
	// the rest are contacts with what the bot cannot know exactly: a
	// monster beside it out of view, a barrel it pushed, a monster's box
	// a frame old
	if float64(exact) < 0.90*float64(frames) || float64(near) < 0.97*float64(frames) {
		t.Errorf("prediction off the server too often: %d exact, %d near of %d", exact, near, frames)
	}
}
