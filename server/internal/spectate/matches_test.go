package spectate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"slices"
	"strings"
	"testing"
	"time"

	"quake2web/server/internal/agent/session"
	"quake2web/server/internal/agent/session/sessiontest"
	"quake2web/server/internal/fakeclient"
	qnet "quake2web/server/internal/net"
	"quake2web/server/internal/q2const"
	"quake2web/server/internal/sv"
)

// liveBot is a lockstep walker bot on the demo pak with a Stream on its
// client, a Hub on the stream, a record of its frames and a digest of
// every message it received.
type liveBot struct {
	t      *testing.T
	l      *session.Lockstep
	c      *fakeclient.Client
	stream *Stream
	hub    *Hub
	frames map[frameKey]frameRec
	digest hash.Hash
	msgs   int
	walk   session.CmdFunc
}

func newLiveBot(t *testing.T, fs sv.FileSystem, seed uint32, cfg HubConfig, cheats ...string) *liveBot {
	t.Helper()
	b := &liveBot{t: t, frames: map[frameKey]frameRec{}, digest: sha256.New(), walk: sessiontest.Walker()}
	b.stream = NewStream(SinkFunc(func(c *fakeclient.Client, payload []byte, spans []fakeclient.Span) {
		b.digest.Write(payload)
		b.msgs++
		if hasFrameSpan(spans) && c.Frame.Valid {
			k, v := recordFrame(c)
			b.frames[k] = v
		}
	}))
	b.l = startBot(t, fs, seed, fakeclient.Options{MaxHistory: 256}, b.stream)
	b.c = b.l.Client()
	for _, s := range cheats {
		b.c.StringCmd(s)
	}
	b.hub = NewHub(b.stream, cfg)
	t.Cleanup(func() { _ = b.hub.Close() })
	return b
}

// latest is the key of the bot's current frame.
func (b *liveBot) latest() frameKey {
	return frameKey{b.c.ServerData.ServerCount, b.c.Frame.ServerFrame}
}

// step runs n frames. After each one it waits for every paced viewer that
// is live on the bot's level to hold the bot's frame, so viewers keep up
// with a bot running far faster than real time.
func (b *liveBot) step(n int, paced ...*liveViewer) {
	b.t.Helper()
	for i := 0; i < n; i++ {
		if err := b.l.Step(context.Background(), b.walk); err != nil {
			b.t.Fatalf("bot step: %v", err)
		}
		k := b.latest()
		if b.c.State != fakeclient.CaActive {
			continue
		}
		for _, lv := range paced {
			if _, live := lv.firstFrame(k.sc); !live {
				continue
			}
			if !eventually(5*time.Second, func() bool { return lv.has(k) }) {
				b.t.Fatalf("viewer lacks frame %v (err %v, stats %+v)", k, lv.error(), b.hub.Stats())
			}
		}
	}
}

// joinRunning steps the bot (at most max frames) until the viewer is live
// on the bot's level and returns the frames it took. Each step waits for
// the hub to drain its feed, and once the hub sent the viewer its keyframe
// the bot pauses until the viewer parsed it, so "live" is observed without
// slack.
func (b *liveBot) joinRunning(lv *liveViewer, max int) (first, botAt int32) {
	b.t.Helper()
	joins := b.hub.Stats().Resyncs[ResyncJoin]
	for i := 0; i < max; i++ {
		sc := b.latest().sc
		if f, ok := lv.firstFrame(sc); ok {
			return f, b.latest().frame
		}
		if b.hub.Stats().Resyncs[ResyncJoin] > joins {
			if eventually(5*time.Second, func() bool { _, ok := lv.firstFrame(sc); return ok }) {
				continue
			}
		}
		b.step(1)
		eventually(time.Second, func() bool { return len(b.hub.sub.ch) == 0 })
		time.Sleep(200 * time.Microsecond) // let the handshake run alongside
	}
	b.t.Fatalf("viewer not live after %d frames: %v (%+v)", max, lv.error(), b.hub.Stats())
	return 0, 0
}

// requireComplete checks that the viewer holds every bot frame of level sc
// from its first one on, and that all its frames equal the bot's.
func (b *liveBot) requireComplete(lv *liveViewer, sc int32) int {
	b.t.Helper()
	first, ok := lv.firstFrame(sc)
	if !ok {
		b.t.Fatalf("viewer has no frame of level %d", sc)
	}
	n := 0
	for k := range b.frames {
		if k.sc == sc && k.frame >= first {
			n++
			if !lv.has(k) {
				b.t.Fatalf("viewer lacks frame %v (first %d)", k, first)
			}
		}
	}
	lv.match(b.t, b.frames, sc)
	return n
}

// TestRelayMatchesBot is the relay's gate: a lockstep walker bot on the
// demo pak and fake client viewers watching it through the hub over
// in-memory pipes. For every serverframe both hold, the viewer's
// playerstate and frame entities equal the bot's (pm_type aside: attract
// loop clients force PM_FREEZE), across late joins, level changes, a death
// reload, a slow viewer, an oversized message meeting a pending
// configstring diff and the hub closing; and watching never changes what
// the bot receives.
func TestRelayMatchesBot(t *testing.T) {
	t.Run("late_join", func(t *testing.T) {
		t.Parallel()
		b := newLiveBot(t, sessiontest.DemoFS(t), 21, HubConfig{}, "god", "notarget")
		b.step(100)
		at := b.latest()
		lv := watch(t, b.hub, 1, nil) // joins while the bot is paused
		if !eventually(5*time.Second, func() bool { _, ok := lv.firstFrame(at.sc); return ok }) {
			t.Fatalf("no keyframe: %v", lv.error())
		}
		if first, _ := lv.firstFrame(at.sc); first != at.frame {
			t.Fatalf("first viewer frame %d, bot at %d", first, at.frame)
		}
		b.step(200, lv)
		if n := b.requireComplete(lv, at.sc); n != 201 {
			t.Fatalf("%d frames compared", n)
		}
	})

	t.Run("late_join_running", func(t *testing.T) {
		t.Parallel()
		b := newLiveBot(t, sessiontest.DemoFS(t), 22, HubConfig{}, "god", "notarget")
		b.step(100)
		lv := watch(t, b.hub, 2, nil)
		first, botAt := b.joinRunning(lv, 300)
		if botAt-first > 3 {
			t.Fatalf("viewer's first frame %d, bot already at %d", first, botAt)
		}
		b.step(150, lv)
		b.requireComplete(lv, b.latest().sc)
		if st := b.hub.Stats(); st.Resyncs[ResyncJoin] != 1 || len(st.Resyncs) != 1 {
			t.Fatalf("stats %+v", st)
		}
	})

	t.Run("level_change", func(t *testing.T) {
		t.Parallel()
		b := newLiveBot(t, sessiontest.DemoFS(t), 23, HubConfig{}, "god", "notarget")
		b.step(30)
		lv := watch(t, b.hub, 3, nil)
		b.joinRunning(lv, 300)
		b.step(30, lv)
		sc1 := b.latest().sc
		b.requireComplete(lv, sc1)

		stuffed := len(lv.stuffTexts())
		gen := b.l.LevelGen()
		if err := b.l.Exec("gamemap demo2"); err != nil {
			t.Fatal(err)
		}
		if err := b.l.WaitLevel(context.Background(), gen, 0); err != nil {
			t.Fatal(err)
		}
		sc2 := b.latest().sc
		if sc2 == sc1 || b.l.MapName() != "demo2" {
			t.Fatalf("level %q sc %d", b.l.MapName(), sc2)
		}
		b.joinRunning(lv, 300)
		b.step(100, lv)
		b.requireComplete(lv, sc2)
		lv.mu.Lock()
		m := lv.maps[sc2]
		lv.mu.Unlock()
		if m != "demo2" {
			t.Fatalf("viewer on %q", m)
		}
		// the relay's own level change, then a fresh handshake up to the
		// precache (the walker may leave demo2 again later on)
		got := lv.stuffTexts()[stuffed:]
		end := slices.Index(got, fmt.Sprintf("precache %d\n", sc2))
		if len(got) < 3 || got[0] != "changing\n" || got[1] != "reconnect\n" || got[2] != fmt.Sprintf("cmd configstrings %d 0\n", sc2) || end < 0 {
			t.Fatalf("stufftexts after the level change: %q", got)
		}
		for _, s := range got[2:end] {
			if !strings.HasPrefix(s, fmt.Sprintf("cmd configstrings %d ", sc2)) && !strings.HasPrefix(s, fmt.Sprintf("cmd baselines %d ", sc2)) {
				t.Fatalf("unexpected stufftext %q in the handshake: %q", s, got)
			}
		}
		// seed 23 walks onto the demo2 lift back to demo1: the viewer
		// follows that level change too
		if sc3 := b.latest().sc; sc3 != sc2 {
			b.joinRunning(lv, 300)
			b.step(30, lv)
			b.requireComplete(lv, sc3)
			lv.mu.Lock()
			t.Logf("the viewer followed the walker on to %s", lv.maps[sc3])
			lv.mu.Unlock()
		}
	})

	t.Run("death_reload", func(t *testing.T) {
		t.Parallel()
		b := newLiveBot(t, sessiontest.DemoFS(t), 24, HubConfig{})
		b.step(60) // Cmd_Kill_f refuses within 5 s of the spawn
		lv := watch(t, b.hub, 4, nil)
		b.joinRunning(lv, 300)
		b.c.StringCmd("kill")
		for i := 0; i < 30 && b.c.Frame.PlayerState.Stats[q2const.STAT_HEALTH] > 0; i++ {
			b.step(1, lv)
		}
		if hp := b.c.Frame.PlayerState.Stats[q2const.STAT_HEALTH]; hp > 0 {
			t.Fatalf("kill: health %d", hp)
		}
		b.step(15, lv)
		sc1 := b.latest().sc
		b.requireComplete(lv, sc1)
		if err := b.l.Reload(context.Background()); err != nil {
			t.Fatal(err)
		}
		sc2 := b.latest().sc
		if sc2 == sc1 {
			t.Fatal("reload kept the servercount")
		}
		b.joinRunning(lv, 300)
		b.step(60, lv)
		b.requireComplete(lv, sc2)
		if hp := b.c.Frame.PlayerState.Stats[q2const.STAT_HEALTH]; hp != 100 {
			t.Fatalf("health after reload %d", hp)
		}
	})

	t.Run("slow_viewer", func(t *testing.T) {
		t.Parallel()
		b := newLiveBot(t, sessiontest.DemoFS(t), 25, HubConfig{QueueDepth: 8}, "god", "notarget")
		b.step(20)
		var sc *stallConn
		lv := watch(t, b.hub, 5, func(c qnet.Conn) qnet.Conn { sc = newStallConn(c); return sc })
		b.joinRunning(lv, 300)
		b.step(20, lv)
		sc.stall()
		b.step(30) // the queue fills up and drops
		if !eventually(5*time.Second, func() bool { return b.hub.Stats().Drops[DropQueue] > 0 }) {
			t.Fatalf("no drop: %+v", b.hub.Stats())
		}
		sc.unstall()
		// the queued datagrams drain; then a bot frame brings the keyframe
		var k frameKey
		caught := false
		for i := 0; i < 50 && !caught; i++ {
			b.step(1)
			k = b.latest()
			caught = eventually(200*time.Millisecond, func() bool { return lv.has(k) })
		}
		if !caught {
			t.Fatalf("viewer did not catch up: %+v", b.hub.Stats())
		}
		st := b.hub.Stats()
		if st.Resyncs[ResyncDrop] != 1 || st.Viewers != 1 {
			t.Fatalf("stats %+v", st)
		}
		b.step(50, lv)
		lv.match(t, b.frames, k.sc)
		for f := k.frame; f <= b.latest().frame; f++ {
			if !lv.has(frameKey{k.sc, f}) {
				t.Fatalf("viewer lacks frame %d after the resync", f)
			}
		}
	})

	t.Run("oversize_pending_reliable", testOversizeWithPendingReliable)

	t.Run("hub_close", func(t *testing.T) {
		t.Parallel()
		b := newLiveBot(t, sessiontest.DemoFS(t), 26, HubConfig{}, "god")
		b.step(10)
		lv := watch(t, b.hub, 6, nil)
		b.joinRunning(lv, 300)
		b.step(10, lv)
		b.hub.Close()
		if !eventually(5*time.Second, func() bool { return errors.Is(lv.error(), fakeclient.ErrDisconnected) }) {
			t.Fatalf("viewer after Close: %v", lv.error())
		}
		b.step(10) // the bot plays on
	})

	t.Run("bot_unaffected", func(t *testing.T) {
		t.Parallel()
		fs := sessiontest.DemoFS(t)
		const frames = 300
		run := func(viewers int) (string, int) {
			b := newLiveBot(t, fs, 27, HubConfig{QueueDepth: 8})
			var lvs []*liveViewer
			var third *stallConn
			paced := 0
			for i := 0; i < frames; i++ {
				if len(lvs) < viewers && i == 40*len(lvs) {
					var wrap func(qnet.Conn) qnet.Conn
					if len(lvs) == 2 { // the third viewer stalls for good later on
						wrap = func(c qnet.Conn) qnet.Conn { third = newStallConn(c); return third }
					}
					lvs = append(lvs, watch(t, b.hub, 10+len(lvs), wrap))
					paced = len(lvs)
				}
				if i == 200 && third != nil {
					third.stall()
					paced = 2
				}
				b.step(1, lvs[:paced]...)
			}
			for _, lv := range lvs[:min(len(lvs), 2)] {
				if n := b.requireComplete(lv, b.latest().sc); n < frames/2 {
					t.Fatalf("a viewer saw %d frames", n)
				}
			}
			if st := b.hub.Stats(); st.Joins != uint64(viewers) || st.Resyncs[ResyncJoin] != uint64(viewers) ||
				(viewers == 3 && st.Drops[DropQueue] == 0) {
				t.Fatalf("stats %+v", st)
			}
			return hex.EncodeToString(b.digest.Sum(nil)), b.msgs
		}
		plain := func() (string, int) { // no stream at all
			h, n := sha256.New(), 0
			l := session.NewLockstep(session.LockstepConfig{FS: fs, Spec: session.Spec{Skill: 1}, Seed: 27,
				Client: fakeclient.Options{MaxHistory: 256, OnServerMessage: func(_ *fakeclient.Client, p []byte, _ []fakeclient.Span) {
					h.Write(p)
					n++
				}}})
			if err := l.Start(context.Background()); err != nil {
				t.Fatal(err)
			}
			defer l.Close()
			walk := sessiontest.Walker()
			for i := 0; i < frames; i++ {
				if err := l.Step(context.Background(), walk); err != nil {
					t.Fatal(err)
				}
			}
			return hex.EncodeToString(h.Sum(nil)), n
		}
		d0, n0 := plain()
		d1, n1 := run(0)
		d3, n3 := run(3)
		t.Logf("bot digest %s over %d messages", d0, n0)
		if d1 != d0 || d3 != d0 || n1 != n0 || n3 != n0 {
			t.Fatalf("bot stream changed: plain %s/%d, hub %s/%d, 3 viewers %s/%d", d0, n0, d1, n1, d3, n3)
		}
	})
}
