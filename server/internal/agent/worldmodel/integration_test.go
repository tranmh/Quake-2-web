package worldmodel

import (
	"context"
	"math"
	"testing"

	"quake2web/server/internal/agent/mapdata"
	"quake2web/server/internal/agent/perception"
	"quake2web/server/internal/agent/session"
	"quake2web/server/internal/agent/session/sessiontest"
	"quake2web/server/internal/assets/pak"
	"quake2web/server/internal/fakeclient"
	"quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/shared"
)

// demo1Route is a path through demo1 from the start (the points a free
// walker passed, so the straight lines between them are open): the start
// corridor, the first room with the barrels and the stimpacks, down the
// ramp to the soldiers.
var demo1Route = []Vec3{
	{153, -316, 0}, {207, -290, 0}, {236, -264, 0}, {236, -222, 0}, {240, -167, 0}, {240, -103, 0},
	{240, -32, 0}, {234, -27, 0}, {213, 27, 0}, {187, 82, 0}, {160, 112, 0}, {126, 112, 0}, {79, 120, 0},
	{17, 120, 0}, {-46, 120, 0}, {-49, 106, 0}, {-60, 88, 0}, {-81, 64, 0}, {-111, 32, 0}, {-156, 16, 0},
	{-208, 16, 0}, {-270, 2, 0}, {-327, -35, 0}, {-352, -92, 0}, {-358, -152, 0}, {-357, -176, 0},
	{-348, -191, 0}, {-324, -245, 0}, {-299, -301, 0}, {-296, -354, 0}, {-296, -394, 0}, {-296, -427, 0},
	{-286, -419, 0}, {-326, -416, 0}, {-371, -388, 0}, {-419, -352, 0}, {-448, -320, 0}, {-454, -285, 0},
	{-487, -254, 0}, {-546, -246, 0}, {-605, -239, 0},
}

// scriptedWalker turns in place once (turnCmds commands, starting from the
// start yaw 135), then runs along waypoints, jumping when it makes no
// progress for a second, and at the end of the route slowly turns in place.
// After the first turn it fires a short blaster burst every 3 s when fire
// is set (the noise wakes the monsters up). It depends only on the client
// state and the command count, so a run is deterministic.
func scriptedWalker(turnCmds int, wps []Vec3, fire bool) session.CmdFunc {
	n, wp := 0, 0
	var last Vec3
	yaw := float32(135)
	return func(c *fakeclient.Client, msec int) shared.UserCmd {
		n++
		delta := c.Frame.PlayerState.PMove.DeltaAngles
		var cmd shared.UserCmd
		switch o := c.Origin(); {
		case n <= turnCmds:
			yaw = 135 + float32(n)*360/float32(turnCmds)
		case wp < len(wps):
			for wp < len(wps) && math.Hypot(float64(wps[wp][0]-o[0]), float64(wps[wp][1]-o[1])) < 24 {
				wp++
			}
			if wp == len(wps) {
				break
			}
			yaw = float32(math.Atan2(float64(wps[wp][1]-o[1]), float64(wps[wp][0]-o[0])) * 180 / math.Pi)
			cmd.ForwardMove = 300
			if n%40 == 0 {
				if math.Hypot(float64(o[0]-last[0]), float64(o[1]-last[1])) < 8 {
					cmd.UpMove = 200
				}
				last = o
			}
		default:
			yaw += 1
		}
		if fire && n > turnCmds && n%120 < 4 {
			cmd.Buttons = q2const.BUTTON_ATTACK
		}
		cmd.Angles[q2const.YAW] = int16(shared.ANGLE2SHORT(yaw) - int32(delta[q2const.YAW]))
		cmd.Angles[q2const.PITCH] = int16(-int32(delta[q2const.PITCH]))
		return cmd
	}
}

func loadLevel(t testing.TB, fs *pak.FS, name string, visit int) Level {
	t.Helper()
	raw, err := fs.ReadFile("maps/" + name + ".bsp")
	if err != nil {
		t.Fatal(err)
	}
	md, err := mapdata.Load(name, raw, mapdata.Options{Skill: 1})
	if err != nil {
		t.Fatal(err)
	}
	return Level{Key: LevelKey{Map: name, Visit: visit}, Map: md}
}

// frameRec is one frame of a lockstep run.
type frameRec struct {
	in  perception.FrameInput
	now int64
}

// runDemo1 runs the scripted walker on demo1 (seed 1, skill 1) for frames
// server frames (plus the frame the client became active with) through a
// World, and returns the frame inputs with their clock and the bot's whole
// payload stream (OnServerMessage, from the handshake on). onFrame sees
// each frame after Update.
func runDemo1(t *testing.T, frames int, onFrame func(w *World, in *perception.FrameInput)) ([]frameRec, [][]byte) {
	t.Helper()
	fs := sessiontest.DemoFS(t)
	var payloads [][]byte
	l := session.NewLockstep(session.LockstepConfig{FS: fs, Spec: session.Spec{Map: "demo1", Skill: 1}, Seed: 1,
		Client: fakeclient.Options{MaxHistory: 256, OnServerMessage: func(_ *fakeclient.Client, p []byte, _ []fakeclient.Span) {
			payloads = append(payloads, p)
		}}})
	ctx := context.Background()
	if err := l.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	w := New(Config{ReadFile: fs.ReadFile})
	w.Reset(loadLevel(t, fs, "demo1", 0))
	r := perception.NewReader()
	walk := scriptedWalker(90, demo1Route, true)
	var recs []frameRec
	for i := 0; i <= frames; i++ {
		if i > 0 { // frame 0: the one the client became active with
			if err := l.Step(ctx, walk); err != nil {
				t.Fatal(err)
			}
		}
		in, ok := r.Next(l.Client())
		if !ok {
			continue
		}
		now := l.GameTimeMs()
		w.Update(in, now)
		recs = append(recs, frameRec{in: in, now: now})
		if onFrame != nil {
			onFrame(w, &in)
		}
	}
	return recs, payloads
}

// TestDemo1Belief runs the scripted walker on demo1 for 60 s of game time:
// monsters enter the belief only once perceived, hidden ones in the packet
// stay out, the soldier that comes hunting is tracked, its hits come with
// bearings towards it, and a second run gives the same beliefs.
func TestDemo1Belief(t *testing.T) {
	const frames = 600
	var digests []string
	admittedEver := map[int32]bool{}
	lastPos := map[string]Vec3{}
	hiddenMonsterFrames, bearings := 0, 0
	recs, _ := runDemo1(t, frames, func(w *World, in *perception.FrameInput) {
		pc := w.Percept()
		b := w.Belief()
		digests = append(digests, b.Digest())
		seen, admitted := map[int32]bool{}, map[int32]bool{}
		for _, n := range pc.Admitted() {
			admittedEver[n], admitted[n] = true, true
		}
		for i := range pc.Seen {
			seen[pc.Seen[i].Num] = true
		}
		for _, tr := range b.Tracks {
			if !admittedEver[tr.Num] {
				t.Fatalf("t=%d: track %s (#%d %s) was never perceived", b.Time, tr.ID, tr.Num, tr.Class)
			}
			if tr.Visible && !seen[tr.Num] {
				t.Fatalf("t=%d: track %s visible but not seen", b.Time, tr.ID)
			}
			// only an admitted position refreshes a track (a sound without
			// a position does not)
			if tr.LastUpdate == b.Time && !admitted[tr.Num] {
				t.Fatalf("t=%d: track %s updated without being perceived", b.Time, tr.ID)
			}
			if p, ok := lastPos[tr.ID]; ok && p != tr.Pos && !admitted[tr.Num] {
				t.Fatalf("t=%d: track %s moved from %v to %v without an admitted position", b.Time, tr.ID, p, tr.Pos)
			}
			lastPos[tr.ID] = tr.Pos
		}
		for i := range in.Entities {
			e := &in.Entities[i]
			if pc.Classifier().Classify(e).Class.Kind == perception.KindMonster && !seen[e.Number] {
				hiddenMonsterFrames++
			}
		}
		// a hit with a recovered bearing points at the soldier that shot
		if n := len(b.Damage); n > 0 && b.Damage[n-1].At == b.Time && b.Damage[n-1].BearingKnown {
			d := b.Damage[n-1]
			src := b.Track(d.Source)
			if src == nil {
				t.Fatalf("t=%d: damage %+v without a source", b.Time, d)
			}
			var truth Vec3
			for i := range in.Entities {
				if in.Entities[i].Number == src.Num {
					truth = in.Entities[i].Origin
				}
			}
			yaw := float32(math.Atan2(float64(truth[1]-b.Self.Origin[1]), float64(truth[0]-b.Self.Origin[0])) * 180 / math.Pi)
			if diff := angleDiff(d.Bearing, yaw); math.Abs(float64(diff)) > 30 {
				t.Errorf("t=%d: damage bearing %.0f, attacker at %.0f", b.Time, d.Bearing, yaw)
			}
			bearings++
		}
	})
	if len(recs) < frames-10 {
		t.Fatalf("only %d frames", len(recs))
	}
	if hiddenMonsterFrames == 0 {
		t.Error("no frame had a monster in the packet but out of sight: the filter was not exercised")
	}
	if bearings == 0 {
		t.Error("no damage with a bearing in the run")
	}
	t.Logf("%d frames, %d frames with hidden monsters, %d bearings", len(recs), hiddenMonsterFrames, bearings)

	var again []string
	var last Belief
	runDemo1(t, frames, func(w *World, in *perception.FrameInput) {
		again = append(again, w.Belief().Digest())
		last = w.Snapshot()
	})
	if len(again) != len(digests) {
		t.Fatalf("second run: %d frames, first %d", len(again), len(digests))
	}
	for i := range digests {
		if digests[i] != again[i] {
			t.Fatalf("frame %d: belief digests differ between identical runs", i)
		}
	}
	var soldier *Track
	for i := range last.Tracks {
		if last.Tracks[i].Kind == "monster" {
			soldier = &last.Tracks[i]
		}
	}
	if soldier == nil || soldier.Class != "soldier" || soldier.Lump < 0 || soldier.FirstSeen == 0 {
		t.Fatalf("no tracked soldier at the end: %+v", last.Tracks)
	}
	if len(last.Items) < 3 {
		t.Fatalf("items %+v", last.Items)
	}
}

// TestKickModelMatchesGame compares the bot's prediction of its own view
// kick (run pitch/roll, bob, fall) with the kick_angles the server sends,
// on a run without firing: they agree within the wire quantization, so
// what remains on a damage frame is the damage kick.
func TestKickModelMatchesGame(t *testing.T) {
	fs := sessiontest.DemoFS(t)
	l := session.NewLockstep(session.LockstepConfig{FS: fs, Spec: session.Spec{Map: "demo1", Skill: 1}, Seed: 1})
	ctx := context.Background()
	if err := l.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	lv := loadLevel(t, fs, "demo1", 0)
	vis := perception.NewVision(lv.Map.CM, perception.ViewOptions{})
	r := perception.NewReader()
	walk := scriptedWalker(90, demo1Route, false)
	var k kickModel
	checked, moving := 0, 0
	var maxKick float64
	for i := 0; i < 250; i++ {
		if err := l.Step(ctx, walk); err != nil {
			t.Fatal(err)
		}
		in, ok := r.Next(l.Client())
		if !ok {
			continue
		}
		ps := &in.PlayerState
		eye := perception.Eye(ps)
		vis.Begin(eye, ps.ViewAngles, ps.Fov, in.AreaBits[:in.AreaBytes], nil)
		origin := Vec3{float32(ps.PMove.Origin[0]) / 8, float32(ps.PMove.Origin[1]) / 8, float32(ps.PMove.Origin[2]) / 8}
		wl, _ := waterlevel(vis, origin, ps.PMove.PmFlags&q2const.PMF_DUCKED != 0)
		pitch, roll := k.predict(ps, wl, in.ServerFrame)
		if ps.Stats[q2const.STAT_FLASHES] != 0 || i < 2 {
			continue
		}
		if math.Abs(float64(ps.KickAngles[q2const.PITCH]-pitch)) > 0.3 || math.Abs(float64(ps.KickAngles[q2const.ROLL]-roll)) > 0.3 {
			t.Errorf("frame %d: kick %v, predicted pitch %.3f roll %.3f (vel %v)", in.ServerFrame, ps.KickAngles, pitch, roll, ps.PMove.Velocity)
		}
		checked++
		maxKick = math.Max(maxKick, math.Max(math.Abs(float64(ps.KickAngles[q2const.PITCH])), math.Abs(float64(ps.KickAngles[q2const.ROLL]))))
		if ps.PMove.Velocity != [3]int16{} {
			moving++
		}
	}
	if checked < 200 || moving < 50 || maxKick < 1 {
		t.Fatalf("checked %d frames, %d moving, largest kick %.2f", checked, moving, maxKick)
	}
	t.Logf("%d frames checked (%d moving), largest kick %.2f degrees", checked, moving, maxKick)
}
