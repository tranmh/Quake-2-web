package spectate

import (
	"bytes"
	"context"
	"testing"
	"time"

	"quake2web/server/internal/agent/session"
	"quake2web/server/internal/agent/session/sessiontest"
	"quake2web/server/internal/demo"
	"quake2web/server/internal/fakeclient"
	"quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/msg"
)

// checkMirror compares the mirrored state with the client's own.
func checkMirror(t *testing.T, s *Stream, c *fakeclient.Client) {
	t.Helper()
	l := s.Level()
	if l == nil {
		if c.LevelGen() != 0 {
			t.Fatal("no level mirrored")
		}
		return
	}
	sd := c.ServerData
	if l.Gen != c.LevelGen() || l.ServerCount != sd.ServerCount || l.GameDir != sd.GameDir || l.PlayerNum != sd.PlayerNum ||
		l.LevelName != sd.LevelName || l.AttractLoop != sd.AttractLoop {
		t.Fatalf("level %+v, client %+v gen %d", l, sd, c.LevelGen())
	}
	if *l.cs != c.ConfigStrings {
		t.Fatal("configstrings differ")
	}
	for i := range c.Entities {
		if b, ok := l.Baseline(i); ok != c.Entities[i].HasBaseline || b != c.Entities[i].Baseline {
			t.Fatalf("baseline %d differs", i)
		}
	}
	if !l.Ready && (c.State == fakeclient.CaActive || !l.Game()) {
		t.Fatalf("not ready with client state %d", c.State)
	}
	if f := s.Frame(); f != nil && c.Frame.Valid && f.ServerFrame == c.Frame.ServerFrame {
		if f.Gen != l.Gen || f.PlayerState != c.Frame.PlayerState || f.AreaBits != c.Frame.AreaBits ||
			f.AreaBytes != c.Frame.AreaBytes || f.DeltaFrame != c.Frame.DeltaFrame || !equalEntities(f.Entities, c.FrameEntities(&c.Frame)) {
			t.Fatalf("frame %d differs", f.ServerFrame)
		}
	}
}

// TestMirrorFollowsClient checks the mirror against the bot client's state
// after every message of a run with a level change and a reload.
func TestMirrorFollowsClient(t *testing.T) {
	t.Parallel()
	fs := sessiontest.DemoFS(t)
	var s *Stream
	msgs := 0
	s = NewStream(SinkFunc(func(c *fakeclient.Client, _ []byte, _ []fakeclient.Span) {
		msgs++
		checkMirror(t, s, c)
	}))
	l := startBot(t, fs, 31, fakeclient.Options{MaxHistory: 64}, s)
	walk := sessiontest.Walker()
	step := func(n int) {
		for i := 0; i < n; i++ {
			if err := l.Step(context.Background(), walk); err != nil {
				t.Fatal(err)
			}
		}
	}
	step(50)
	gen := l.LevelGen()
	if err := l.Exec("gamemap demo2"); err != nil {
		t.Fatal(err)
	}
	if err := l.WaitLevel(context.Background(), gen, 0); err != nil {
		t.Fatal(err)
	}
	step(30)
	if err := l.Reload(context.Background()); err != nil {
		t.Fatal(err)
	}
	step(30)
	if lvl := s.Level(); lvl.Gen != 3 || !lvl.Ready || lvl.ConfigString(q2const.CS_MODELS+1) != "maps/demo2.bsp" {
		t.Fatalf("final level %+v", lvl)
	}
	if msgs < 100 {
		t.Fatalf("%d messages", msgs)
	}
}

func TestMirrorSnapshotsAreImmutable(t *testing.T) {
	rg := newRig(t, 0)
	rg.startLevel(newSynth(42, "imm", 3), 2)
	before := rg.stream.Level()
	frame := rg.stream.Frame()
	rg.botFrame(writeConfigString(q2const.CS_LIGHTS+3, "zzz"))
	after := rg.stream.Level()
	if before.ConfigString(q2const.CS_LIGHTS+3) != "" || after.ConfigString(q2const.CS_LIGHTS+3) != "zzz" {
		t.Fatal("configstring update leaked into the old snapshot")
	}
	if before.base != after.base {
		t.Fatal("unchanged baselines were copied")
	}
	if rg.stream.Frame() == frame || frame.ServerFrame == rg.stream.Frame().ServerFrame {
		t.Fatal("frame not replaced")
	}
	// a message that changes nothing keeps the snapshot
	l := rg.stream.Level()
	m := msg.NewSizeBuf(64)
	writePrint("x\n")(m)
	rg.botMsg(m.Bytes())
	if rg.stream.Level() != l {
		t.Fatal("snapshot replaced without a change")
	}
}

func TestStreamNeverBlocks(t *testing.T) {
	rg := newRig(t, 0)
	rg.startLevel(newSynth(42, "nb", 3), 1)
	stuck := rg.stream.subscribe(4) // a hub that stopped reading
	var worst time.Duration
	for i := 0; i < 2000; i++ {
		s := rg.srv
		s.advance()
		p := s.frameMsg(s.frame-1, nil)
		t0 := time.Now()
		rg.feed(p)
		if d := time.Since(t0); d > worst {
			worst = d
		}
		for len(rg.sub.ch) > 0 { // the rig's own feed is read
			<-rg.sub.ch
		}
	}
	if worst > 100*time.Millisecond {
		t.Fatalf("a message took %v", worst)
	}
	rg.stream.mu.Lock()
	lost := stuck.lost
	rg.stream.mu.Unlock()
	if len(stuck.ch) != cap(stuck.ch) || lost != 2000-uint64(cap(stuck.ch))+1 {
		t.Fatalf("stuck feed holds %d, lost %d", len(stuck.ch), lost)
	}
	rg.stream.unsubscribe(stuck)
}

// TestRecorderIsASink plugs the existing demo.Recorder into a Stream: it
// must record exactly what it records on the client hook directly.
func TestRecorderIsASink(t *testing.T) {
	t.Parallel()
	fs := sessiontest.DemoFS(t)
	record := func(viaStream bool) demo.MemFiles {
		files := demo.MemFiles{}
		rec := demo.NewRecorder(files.Create)
		opt := fakeclient.Options{OnServerMessage: rec.OnServerMessage}
		var hub *Hub
		if viaStream {
			s := NewStream(rec)
			opt.OnServerMessage = s.OnServerMessage
			hub = NewHub(s, HubConfig{})
			defer hub.Close()
		}
		l := session.NewLockstep(session.LockstepConfig{FS: fs, Spec: session.Spec{Skill: 1}, Seed: 32, Client: opt})
		if err := l.Start(context.Background()); err != nil {
			t.Fatal(err)
		}
		defer l.Close()
		walk := sessiontest.Walker()
		for i := 0; i < 100; i++ {
			if err := l.Step(context.Background(), walk); err != nil {
				t.Fatal(err)
			}
		}
		if err := rec.Close(); err != nil {
			t.Fatal(err)
		}
		return files
	}
	direct, streamed := record(false), record(true)
	if len(direct) != 1 || len(streamed) != 1 {
		t.Fatalf("files %d / %d", len(direct), len(streamed))
	}
	for name, b := range direct {
		if !bytes.Equal(streamed[name].Bytes(), b.Bytes()) {
			t.Fatalf("%s differs through the stream", name)
		}
		if st, err := demo.Validate(b.Bytes()); err != nil || st.Frames < 90 {
			t.Fatalf("validate: %+v %v", st, err)
		}
	}
}
