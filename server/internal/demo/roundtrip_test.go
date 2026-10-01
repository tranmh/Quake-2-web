package demo_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"quake2web/server/internal/agent/session"
	"quake2web/server/internal/agent/session/sessiontest"
	"quake2web/server/internal/demo"
	"quake2web/server/internal/fakeclient"
	"quake2web/server/internal/host"
	"quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/shared"
	"quake2web/server/internal/testutil"
)

// frameRec is what a client reconstructed from one svc_frame.
type frameRec struct {
	ServerFrame, DeltaFrame int32
	AreaBits                [q2const.MAX_MAP_AREAS / 8]byte
	PS                      shared.PlayerState
	Ents                    []shared.EntityState
}

// captureFrames returns an OnServerMessage hook appending every parsed frame.
func captureFrames(dst *[]frameRec) func(*fakeclient.Client, []byte, []fakeclient.Span) {
	return func(c *fakeclient.Client, _ []byte, spans []fakeclient.Span) {
		for _, sp := range spans {
			if sp.Cmd == q2const.Svc_frame && c.Frame.Valid {
				*dst = append(*dst, frameRec{c.Frame.ServerFrame, c.Frame.DeltaFrame, c.Frame.AreaBits,
					c.Frame.PlayerState, c.FrameEntities(&c.Frame)})
			}
		}
	}
}

func both(hooks ...func(*fakeclient.Client, []byte, []fakeclient.Span)) func(*fakeclient.Client, []byte, []fakeclient.Span) {
	return func(c *fakeclient.Client, p []byte, s []fakeclient.Span) {
		for _, h := range hooks {
			h(c, p, s)
		}
	}
}

// recordWalk records a seeded lockstep walker run on demo1 for n frames
// and returns the files and the frames the bot saw.
func recordWalk(t *testing.T, seed uint32, n int, allBaselines bool) (demo.MemFiles, []frameRec) {
	t.Helper()
	files := demo.MemFiles{}
	rec := demo.NewRecorder(files.Create)
	rec.AllBaselines = allBaselines
	var frames []frameRec
	l := session.NewLockstep(session.LockstepConfig{
		FS: sessiontest.DemoFS(t), Spec: session.Spec{Skill: 1}, Seed: seed,
		Client: fakeclient.Options{OnServerMessage: both(rec.OnServerMessage, captureFrames(&frames))},
	})
	ctx := context.Background()
	if err := l.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	walker := sessiontest.Walker()
	for i := 0; i < n; i++ {
		if err := l.Step(ctx, walker); err != nil {
			t.Fatal(err)
		}
	}
	if err := rec.Close(); err != nil {
		t.Fatal(err)
	}
	if got := rec.Files(); len(got) != 1 || got[0] != "00-demo1.dm2" {
		t.Fatalf("recorded files %v", got)
	}
	return files, frames
}

// playback serves data as demos/t.dm2 to a fresh server, runs "demomap
// t.dm2" with a loopback viewer until the server ends the demo and returns
// the frames the viewer reconstructed.
func playback(t *testing.T, data []byte, blocks int) (*session.Lockstep, []frameRec) {
	t.Helper()
	var viewFrames []frameRec
	l := session.NewLockstep(session.LockstepConfig{
		FS:           &host.OverlayFS{Files: map[string][]byte{"demos/t.dm2": data}, Base: sessiontest.DemoFS(t)},
		StartCommand: "demomap t.dm2",
		Client:       fakeclient.Options{OnServerMessage: captureFrames(&viewFrames)},
	})
	ctx := context.Background()
	if err := l.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	var end error
	for i := 0; i < blocks+50 && end == nil; i++ {
		end = l.Step(ctx, nil)
	}
	if !errors.Is(end, fakeclient.ErrDisconnected) {
		t.Fatalf("playback did not end with a disconnect: %v", end)
	}
	return l, viewFrames
}

// diffFrame returns how a viewer frame differs from the bot's ("" if not).
func diffFrame(b, v frameRec) string {
	if v.PS.PMove.PmType != q2const.PM_FREEZE {
		return fmt.Sprintf("viewer pm_type %d, a demo freezes it", v.PS.PMove.PmType)
	}
	v.PS.PMove.PmType = b.PS.PMove.PmType // CL_ParseFrame forces PM_FREEZE for attract loops
	if b.ServerFrame != v.ServerFrame || b.DeltaFrame != v.DeltaFrame || b.AreaBits != v.AreaBits || b.PS != v.PS {
		return fmt.Sprintf("header or playerstate:\nbot    %+v\nviewer %+v", b, v)
	}
	if len(b.Ents) != len(v.Ents) {
		return fmt.Sprintf("%d entities vs %d", len(b.Ents), len(v.Ents))
	}
	for k := range b.Ents {
		if b.Ents[k] != v.Ents[k] {
			return fmt.Sprintf("entity %d: %+v vs %+v", b.Ents[k].Number, b.Ents[k], v.Ents[k])
		}
	}
	return ""
}

// TestRoundTripSvDemo records 20 s of a bot on demo1, plays the file back
// through the server's "demomap" to a loopback viewer and checks that the
// viewer reconstructs exactly the frames the bot saw (playerstate, area
// bits, every entity) and that the server ends the demo cleanly at its end.
func TestRoundTripSvDemo(t *testing.T) {
	files, botFrames := recordWalk(t, 42, 200, true)
	data := files["00-demo1.dm2"].Bytes()
	st, err := demo.Validate(data)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("demo: %d bytes, %d blocks (%d header), frames %d..%d", st.Bytes, st.Blocks, st.HeaderBlocks, st.FirstFrame, st.LastFrame)
	if st.Frames != len(botFrames) || st.FirstFrame != botFrames[0].ServerFrame || botFrames[0].DeltaFrame != -1 {
		t.Fatalf("demo holds %d frames from %d, the bot saw %d from %d (delta %d)",
			st.Frames, st.FirstFrame, len(botFrames), botFrames[0].ServerFrame, botFrames[0].DeltaFrame)
	}

	l, viewFrames := playback(t, data, st.Blocks)
	viewer := l.Client()
	if viewer.ServerData.AttractLoop != 1 || viewer.ServerData.ServerCount != st.ServerCount {
		t.Fatalf("viewer serverdata %+v", viewer.ServerData)
	}
	if l.Server().SVS.Initialized || l.Server().Killed() {
		t.Fatalf("server after the demo: initialized %v killed %v", l.Server().SVS.Initialized, l.Server().Killed())
	}
	if !strings.Contains(strings.Join(viewer.Prints, ""), "Server was killed.") {
		t.Errorf("viewer prints %q", viewer.Prints)
	}
	if len(viewFrames) != len(botFrames) {
		t.Fatalf("viewer got %d frames, the bot %d", len(viewFrames), len(botFrames))
	}
	for i := range botFrames {
		if d := diffFrame(botFrames[i], viewFrames[i]); d != "" {
			t.Fatalf("frame %d differs: %s", botFrames[i].ServerFrame, d)
		}
	}
	moved := shared.VectorLength(shared.VectorSubtract(
		vec(botFrames[len(botFrames)-1].PS.PMove.Origin), vec(botFrames[0].PS.PMove.Origin)))
	if moved < 64 {
		t.Errorf("the bot only moved %.0f units: a weak round trip", moved)
	}
}

// TestCHeaderLosesSoundOnlyBaselines pins why Writer.AllBaselines exists: a
// header written exactly like CL_Record_f drops the baselines of entities
// without a model, so a looping sound entity replays from a null baseline.
func TestCHeaderLosesSoundOnlyBaselines(t *testing.T) {
	files, botFrames := recordWalk(t, 42, 30, false)
	data := files["00-demo1.dm2"].Bytes()
	st, err := demo.Validate(data)
	if err != nil {
		t.Fatal(err)
	}
	_, viewFrames := playback(t, data, st.Blocks)
	if len(viewFrames) != len(botFrames) {
		t.Fatalf("viewer got %d frames, the bot %d", len(viewFrames), len(botFrames))
	}
	soundOnly := 0
	for i := range botFrames {
		b, v := botFrames[i], viewFrames[i]
		for k := range b.Ents {
			if b.Ents[k] == v.Ents[k] {
				continue
			}
			if b.Ents[k].ModelIndex != 0 || b.Ents[k].Sound == 0 {
				t.Fatalf("frame %d: entity %+v differs and is not sound-only", b.ServerFrame, b.Ents[k])
			}
			soundOnly++
		}
	}
	if soundOnly == 0 {
		t.Fatal("no sound-only entity differs: the C header behavior is not exercised")
	}
}

func vec(o [3]int16) shared.Vec3 {
	return shared.Vec3{float32(o[0]) / 8, float32(o[1]) / 8, float32(o[2]) / 8}
}

// Fixtures under fixtures/agent (regenerated with Q2_UPDATE_FIXTURES=1).
const (
	// fixtureName: 10 s of a seeded walker on demo1 as the Recorder writes it.
	fixtureName = "demo1-walker-10s.dm2"
	// recordHeaderName: what CL_Record_f writes ("record" and "stop" at
	// once) for a client that has played fixtureName up to its first frame:
	// the reference for the TS client's CL_Record_f (header parity).
	recordHeaderName = "demo1-walker-10s.record-header.dm2"
)

// recordHeaderAfterFirstFrame plays a demo up to its first frame on a
// passive client and returns what CL_Record_f and CL_Stop_f would write.
func recordHeaderAfterFirstFrame(t *testing.T, data []byte) []byte {
	t.Helper()
	p := fakeclient.NewPassive(fakeclient.Options{})
	r := demo.NewReader(bytes.NewReader(data))
	for p.State != fakeclient.CaActive {
		b, err := r.Next()
		if err != nil {
			t.Fatalf("no frame in the demo: %v", err)
		}
		if _, err := p.FeedPayload(b); err != nil {
			t.Fatal(err)
		}
	}
	var buf bytes.Buffer
	w := demo.NewWriter(&buf) // exactly CL_Record_f: no AllBaselines
	if err := w.Begin(demo.HeaderFromClient(p)); err != nil {
		t.Fatal(err)
	}
	if err := w.End(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// TestFixtureDemo1Walker regenerates the fixtures and checks they are
// byte-identical to the committed files: lockstep recording is
// deterministic. Q2_UPDATE_FIXTURES=1 rewrites them.
func TestFixtureDemo1Walker(t *testing.T) {
	files, _ := recordWalk(t, 1, 100, true)
	data := files["00-demo1.dm2"].Bytes()
	st, err := demo.Validate(data)
	if err != nil {
		t.Fatal(err)
	}
	if st.Frames < 100 {
		t.Fatalf("fixture holds %d frames", st.Frames)
	}
	header := recordHeaderAfterFirstFrame(t, data)
	root, err := testutil.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "fixtures", "agent")
	for name, got := range map[string][]byte{fixtureName: data, recordHeaderName: header} {
		path := filepath.Join(dir, name)
		if os.Getenv("Q2_UPDATE_FIXTURES") == "1" {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, got, 0o644); err != nil {
				t.Fatal(err)
			}
			t.Logf("wrote %s: %d bytes", path, len(got))
		}
		want, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%v (regenerate with Q2_UPDATE_FIXTURES=1)", err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("regenerated %s differs from the fixture (%d vs %d bytes)", name, len(got), len(want))
		}
	}
}

// TestRecorderAcrossLevels: a level change and a death reload each start a
// new file, and every file validates on its own.
func TestRecorderAcrossLevels(t *testing.T) {
	files := demo.MemFiles{}
	rec := demo.NewRecorder(files.Create)
	l := session.NewLockstep(session.LockstepConfig{
		FS: sessiontest.DemoFS(t), Spec: session.Spec{Skill: 1}, Seed: 11,
		Client: fakeclient.Options{OnServerMessage: rec.OnServerMessage},
	})
	ctx := context.Background()
	if err := l.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	walk := func(n int) {
		t.Helper()
		w := sessiontest.Walker()
		for i := 0; i < n; i++ {
			if err := l.Step(ctx, w); err != nil {
				t.Fatal(err)
			}
		}
	}
	walk(20)
	gen := l.LevelGen()
	if err := l.Exec("gamemap demo2"); err != nil {
		t.Fatal(err)
	}
	if err := l.WaitLevel(ctx, gen, 0); err != nil {
		t.Fatal(err)
	}
	// stand still (walking on demo2 may take the lift back to demo1) until
	// Cmd_Kill_f accepts a suicide, 5 s after the spawn
	for i := 0; i < 55; i++ {
		if err := l.Step(ctx, nil); err != nil {
			t.Fatal(err)
		}
	}
	l.Client().StringCmd("kill")
	walk(10)
	if err := l.Reload(ctx); err != nil {
		t.Fatal(err)
	}
	walk(20)
	if err := rec.Close(); err != nil {
		t.Fatal(err)
	}
	want := []string{"00-demo1.dm2", "01-demo2.dm2", "02-demo2.dm2"}
	if got := rec.Files(); strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("files %v, want %v", got, want)
	}
	for _, name := range want {
		st, err := demo.Validate(files[name].Bytes())
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !strings.HasSuffix(st.Map, strings.TrimSuffix(name[3:], ".dm2")+".bsp") || st.Frames < 10 {
			t.Fatalf("%s: %+v", name, st)
		}
	}
}
