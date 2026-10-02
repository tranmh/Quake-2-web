package spectate

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"quake2web/server/internal/fakeclient"
	"quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/msg"
	"quake2web/server/internal/qcommon/shared"
)

func TestHandshake(t *testing.T) {
	rg := newRig(t, 0)
	rg.startLevel(newSynth(42, "test", 6), 5)
	rv := rg.addViewer(64)
	c, bot := rv.c, rg.bot
	if rv.err != nil || c.State != fakeclient.CaActive {
		t.Fatalf("viewer state %d, err %v", c.State, rv.err)
	}
	if c.ServerData.AttractLoop != 1 || c.ServerData.ServerCount != 42 || c.ServerData.PlayerNum != 0 ||
		c.ServerData.GameDir != "baseq2" || c.ServerData.LevelName != "Synthetic test" {
		t.Fatalf("serverdata %+v", c.ServerData)
	}
	if c.ConfigStrings != bot.ConfigStrings {
		t.Fatal("configstrings differ from the bot's")
	}
	for i := range c.Entities {
		if c.Entities[i].HasBaseline != bot.Entities[i].HasBaseline || c.Entities[i].Baseline != bot.Entities[i].Baseline {
			t.Fatalf("baseline %d differs", i)
		}
	}
	if c.NumBaselines != 7 { // 6 movers + the sound-only entity
		t.Fatalf("%d baselines", c.NumBaselines)
	}
	// the keyframe is the bot's latest frame, with the same number
	if len(rv.order) != 1 || rv.order[0] != (frameKey{42, bot.Frame.ServerFrame}) || c.Frame.DeltaFrame != -1 {
		t.Fatalf("first frames %v (bot at %d), delta %d", rv.order, bot.Frame.ServerFrame, c.Frame.DeltaFrame)
	}
	// and the bot's delta frames apply from there
	for i := 0; i < 30; i++ {
		rg.botFrame(nil)
	}
	if n := rv.requireMatch(t); n != 31 {
		t.Fatalf("viewer holds %d frames, want 31", n)
	}
	st := rg.m.Snapshot()
	if st.Resyncs[ResyncJoin] != 1 || len(st.Resyncs) != 1 || st.Viewers != 1 {
		t.Fatalf("stats %+v", st)
	}
	// the server's commands to the bot never reach the viewer
	for _, s := range c.StuffTexts {
		if !strings.HasPrefix(s, "cmd configstrings ") && !strings.HasPrefix(s, "cmd baselines ") && !strings.HasPrefix(s, "precache ") {
			t.Fatalf("viewer got stufftext %q", s)
		}
	}
}

func TestHandshakePagesLikeTheServer(t *testing.T) {
	// many long configstrings and baselines: several pages of each
	rg := newRig(t, 0)
	s := newSynth(9, "big", 300)
	for i := 0; i < 200; i++ {
		s.cs[q2const.CS_MODELS+2+i] = fmt.Sprintf("models/monsters/thing%03d/tris.md2", i)
	}
	s.ents = s.ents[:20] // most entities are out of view: the keyframe fits
	rg.startLevel(s, 1)
	rv := rg.addViewer(64)
	if rv.c.State != fakeclient.CaActive || rv.c.ConfigStrings != rg.bot.ConfigStrings || rv.c.NumBaselines != rg.bot.NumBaselines {
		t.Fatalf("state %d, baselines %d/%d", rv.c.State, rv.c.NumBaselines, rg.bot.NumBaselines)
	}
	pages := map[string]int{}
	for _, st := range rv.c.StuffTexts {
		pages[strings.Fields(st)[0]+" "+strings.Fields(st)[1]]++
	}
	if pages["cmd configstrings"] < 3 || pages["cmd baselines"] < 3 {
		t.Fatalf("pages %v", pages)
	}
}

func TestNewWaitsForTheBotsLevel(t *testing.T) {
	rg := newRig(t, 0)
	rv := rg.addViewer(64) // no level at all yet
	if rv.c.State != fakeclient.CaConnected || rv.c.LevelGen() != 0 || !rv.v.pendingNew {
		t.Fatalf("state %d gen %d pending %v", rv.c.State, rv.c.LevelGen(), rv.v.pendingNew)
	}
	s := newSynth(5, "late", 3)
	rg.srv = s
	for _, p := range s.handshake() {
		rg.botMsg(p)
	}
	if rv.c.LevelGen() != 0 {
		t.Fatal("serverdata sent before the bot's level was complete")
	}
	rg.botFrame(nil) // the bot's first frame: the level is ready
	if rv.c.State != fakeclient.CaActive {
		t.Fatalf("viewer state %d", rv.c.State)
	}
	rg.botFrame(nil)
	if n := rv.requireMatch(t); n != 2 {
		t.Fatalf("%d frames", n)
	}
}

func TestLevelChange(t *testing.T) {
	rg := newRig(t, 0)
	rg.startLevel(newSynth(42, "one", 4), 3)
	rv := rg.addViewer(64)
	rg.botFrame(nil)
	// the bot is told to change level (the relay filters these) ...
	rg.botFrame(writeStuff("changing\n"))
	rg.botMsg(func() []byte { m := msg.NewSizeBuf(64); writeStuff("reconnect\n")(m); return m.Bytes() }())
	// ... and starts the next one
	rg.startLevel(newSynth(43, "two", 8), 4)
	if rv.err != nil || rv.c.State != fakeclient.CaActive || rv.c.LevelGen() != 2 || rv.c.MapName() != "two" {
		t.Fatalf("viewer state %d gen %d map %q err %v", rv.c.State, rv.c.LevelGen(), rv.c.MapName(), rv.err)
	}
	if rv.c.ConfigStrings != rg.bot.ConfigStrings {
		t.Fatal("configstrings of the new level differ")
	}
	for i := 0; i < 10; i++ {
		rg.botFrame(nil)
	}
	rv.requireMatch(t)
	if _, ok := rv.frames[frameKey{43, rg.srv.frame}]; !ok {
		t.Fatal("viewer lacks the bot's latest frame on the new level")
	}
	var changing, reconnect int
	for _, s := range rv.c.StuffTexts {
		changing += strings.Count(s, "changing")
		reconnect += strings.Count(s, "reconnect")
	}
	if changing != 1 || reconnect != 1 {
		t.Fatalf("changing %d reconnect %d: %q", changing, reconnect, rv.c.StuffTexts)
	}
}

func TestCinematicLevel(t *testing.T) {
	rg := newRig(t, 0)
	rg.startLevel(newSynth(42, "one", 2), 2)
	rv := rg.addViewer(64)
	pic := newSynth(50, "x", 0)
	rg.srv = pic
	rg.botMsg(pic.cinematic("victory.pcx"))
	c := rv.c
	if c.LevelGen() != 2 || c.ServerData.PlayerNum != -1 || c.ServerData.LevelName != "victory.pcx" || c.State != fakeclient.CaConnected {
		t.Fatalf("viewer %+v state %d", c.ServerData, c.State)
	}
	// keepalives keep a viewer on a picture level connected
	for i := 0; i < 40; i++ {
		rg.tick(time.Second)
	}
	if rv.err != nil || rv.v.removed {
		t.Fatalf("viewer dropped on a picture level: %v", rv.err)
	}
}

func TestViewerInputIgnored(t *testing.T) {
	rg := newRig(t, 0)
	rg.startLevel(newSynth(42, "test", 3), 2)
	rv := rg.addViewer(64)
	before, stuffed := rg.bot.Netchan.OutgoingSequence, len(rv.c.StuffTexts)
	for _, s := range []string{"killserver", "say hello", "kill", "nextserver 42", "download maps/test.bsp", "new", "configstrings 42 0", "baselines 42 0", "rcon x status", "god"} {
		rv.c.StringCmd(s)
		rg.botFrame(nil)
	}
	rv.c.SetUserinfo(`\name\evil\rate\1`)
	rg.botFrame(nil)
	if rv.err != nil || rv.v.removed || rv.c.State != fakeclient.CaActive {
		t.Fatalf("viewer dropped: %v", rv.err)
	}
	if rg.bot.Netchan.OutgoingSequence != before || len(rg.bot.StuffTexts) != 1 {
		t.Fatal("the bot's client saw viewer input")
	}
	rv.requireMatch(t)
	if len(rv.c.StuffTexts) != stuffed { // "new" etc. while spawned are ignored
		t.Fatalf("stufftexts %q", rv.c.StuffTexts)
	}
	// a spawned viewer repeating "begin" (up to MAX_STRINGCMDS-1 a packet)
	// gets no keyframes for it: only lastframe -1 asks for one, rate-limited
	out := rg.m.Snapshot().DatagramsOut
	for i := 0; i < 2; i++ {
		for j := 0; j < maxStringCmds; j++ {
			rv.c.StringCmd("begin 42")
		}
		rg.botFrame(nil)
	}
	if st := rg.m.Snapshot(); st.Resyncs[ResyncJoin] != 1 || len(st.Resyncs) != 1 || st.DatagramsOut-out > 6 {
		t.Fatalf("repeated begin: stats %+v (%d datagrams)", st, st.DatagramsOut-out)
	}
	if rv.err != nil || rv.v.removed || rv.c.State != fakeclient.CaActive {
		t.Fatalf("viewer dropped: %v", rv.err)
	}
	rv.requireMatch(t)
}

// TestStreamOutlivesBotClient: a Stream kept across the bot's sessions is
// fed by a new client whose LevelGen starts over at 1. Its serverdata is a
// new generation all the same: the mirror takes the new client's level
// whole (no stale servercount or baselines) and viewers are sent through
// changing/reconnect and a fresh handshake.
func TestStreamOutlivesBotClient(t *testing.T) {
	rg := newRig(t, 0)
	rg.startLevel(newSynth(42, "one", 8), 3)
	rv := rg.addViewer(64)
	rg.botFrame(nil)
	if g := rg.bot.LevelGen(); g != 1 {
		t.Fatalf("first client at gen %d", g)
	}
	// the next episode: a new client on the same stream, at gen 1 again,
	// on the same map name with fewer baselines and another servercount
	rg.bot = fakeclient.NewPassive(fakeclient.Options{OnServerMessage: rg.stream.OnServerMessage})
	rg.startLevel(newSynth(77, "one", 3), 4)
	if g := rg.bot.LevelGen(); g != 1 {
		t.Fatalf("second client at gen %d", g)
	}
	l := rg.stream.Level()
	if l.Gen != 2 || l.ServerCount != 77 || !l.Ready {
		t.Fatalf("level after the client change: %+v", l)
	}
	if *l.cs != rg.bot.ConfigStrings {
		t.Fatal("configstrings differ from the new client's")
	}
	for i := range rg.bot.Entities {
		if b, ok := l.Baseline(i); ok != rg.bot.Entities[i].HasBaseline || b != rg.bot.Entities[i].Baseline {
			t.Fatalf("baseline %d is not the new client's (present %v)", i, ok)
		}
	}
	if f := rg.stream.Frame(); f == nil || f.Gen != 2 || f.ServerFrame != rg.srv.frame {
		t.Fatalf("frame %+v", f)
	}
	if rv.err != nil || rv.c.State != fakeclient.CaActive || rv.c.ServerData.ServerCount != 77 ||
		rv.c.NumBaselines != rg.bot.NumBaselines || rv.c.ConfigStrings != rg.bot.ConfigStrings {
		t.Fatalf("viewer state %d sc %d baselines %d/%d err %v", rv.c.State, rv.c.ServerData.ServerCount,
			rv.c.NumBaselines, rg.bot.NumBaselines, rv.err)
	}
	var changing, reconnect int
	for _, s := range rv.c.StuffTexts {
		changing += strings.Count(s, "changing")
		reconnect += strings.Count(s, "reconnect")
	}
	if changing != 1 || reconnect != 1 {
		t.Fatalf("changing %d reconnect %d: %q", changing, reconnect, rv.c.StuffTexts)
	}
	for i := 0; i < 10; i++ {
		rg.botFrame(nil)
	}
	rv.requireMatch(t)
	if _, ok := rv.frames[frameKey{77, rg.srv.frame}]; !ok {
		t.Fatal("viewer lacks the new client's latest frame")
	}
}

func TestViewerBadCommandDrops(t *testing.T) {
	rg := newRig(t, 0)
	rg.startLevel(newSynth(42, "test", 3), 2)
	rv := rg.addViewer(64)
	rv.c.Netchan.Transmit([]byte{q2const.Clc_nop, 9}, rg.ms()) // 9: no such clc
	rg.pump()
	if !rv.v.removed || !errors.Is(rv.err, fakeclient.ErrDisconnected) {
		t.Fatalf("removed %v err %v", rv.v.removed, rv.err)
	}
	if st := rg.m.Snapshot(); st.Leaves[LeaveProtocol] != 1 || st.Viewers != 0 {
		t.Fatalf("stats %+v", st)
	}
}

func TestViewerDisconnect(t *testing.T) {
	rg := newRig(t, 0)
	rg.startLevel(newSynth(42, "test", 3), 2)
	rv := rg.addViewer(64)
	rv.c.Disconnect()
	rg.pump()
	if !rv.v.removed || rg.m.Snapshot().Leaves[LeaveDisconnect] != 1 {
		t.Fatal("disconnect not handled")
	}
}

// movePayload is a clc_move with the given lastframe (usercmds omitted:
// the relay reads no further).
func movePayload(lastframe int32) []byte {
	m := msg.NewSizeBuf(16)
	m.MSG_WriteByte(q2const.Clc_move)
	m.MSG_WriteByte(0)
	m.MSG_WriteLong(lastframe)
	return m.Bytes()
}

func TestClientRequestedResync(t *testing.T) {
	rg := newRig(t, 0)
	rg.startLevel(newSynth(42, "test", 3), 2)
	rv := rg.addViewer(64)
	rg.botFrame(nil)
	rv.c.Netchan.Transmit(movePayload(-1), rg.ms())
	rg.pump()
	rg.botFrame(nil)
	if got := rg.m.Snapshot().Resyncs[ResyncClient]; got != 1 {
		t.Fatalf("client resyncs %d", got)
	}
	if rv.c.Frame.DeltaFrame != -1 {
		t.Fatal("the resync frame is not a keyframe")
	}
	// at most one a second
	for i := 0; i < 5; i++ {
		rv.c.Netchan.Transmit(movePayload(-1), rg.ms())
		rg.pump()
		rg.botFrame(nil)
	}
	if got := rg.m.Snapshot().Resyncs[ResyncClient]; got != 1 {
		t.Fatalf("client resyncs %d within a second", got)
	}
	rg.tick(time.Second)
	rv.c.Netchan.Transmit(movePayload(-1), rg.ms())
	rg.pump()
	rg.botFrame(nil)
	if got := rg.m.Snapshot().Resyncs[ResyncClient]; got != 2 {
		t.Fatalf("client resyncs %d", got)
	}
	rv.requireMatch(t)
}

func TestDeltaFromUnsentFrameResyncs(t *testing.T) {
	rg := newRig(t, 0)
	rg.startLevel(newSynth(42, "test", 5), 5)
	rv := rg.addViewer(64) // keyframe of frame 5 only
	s := rg.srv
	s.advance()
	rg.botMsg(s.frameMsg(s.frame-3, nil)) // frame 6 from frame 3: the bot has it, the viewer not
	if !rg.bot.Frame.Valid {
		t.Fatal("bot frame invalid")
	}
	if got := rg.m.Snapshot().Resyncs[ResyncDelta]; got != 1 || rv.c.Frame.DeltaFrame != -1 {
		t.Fatalf("delta resyncs %d, viewer delta %d", got, rv.c.Frame.DeltaFrame)
	}
	for i := 0; i < 5; i++ {
		rg.botFrame(nil)
	}
	if n := rv.requireMatch(t); n != 7 {
		t.Fatalf("%d frames", n)
	}
}

func TestInvalidBotFrameNotForwarded(t *testing.T) {
	rg := newRig(t, 0)
	rg.startLevel(newSynth(42, "test", 3), 3)
	rv := rg.addViewer(64)
	s := rg.srv
	// a delta from a frame the bot never got (lost on the way): invalid
	s.advance()
	s.history[1000] = s.history[s.frame-1]
	rg.botMsg(s.frameMsg(1000, writePrint("still forwarded\n")))
	if rg.bot.Frame.Valid {
		t.Fatal("bot frame valid")
	}
	if !rv.c.Frame.Valid || rv.c.Frame.ServerFrame == s.frame {
		t.Fatal("the invalid frame reached the viewer")
	}
	if p := rv.c.Prints; len(p) == 0 || p[len(p)-1] != "still forwarded\n" {
		t.Fatalf("prints %q", p)
	}
	// the bot asks for an uncompressed frame; it is forwarded as is
	s.advance()
	rg.botMsg(s.frameMsg(-1, nil))
	rv.requireMatch(t)
	if _, ok := rv.frames[frameKey{42, s.frame}]; !ok {
		t.Fatal("uncompressed frame not forwarded")
	}
}

func TestSlowViewerResyncAndDisconnect(t *testing.T) {
	rg := newRig(t, 0)
	rg.startLevel(newSynth(42, "test", 4), 2)
	rv := rg.addViewer(8)
	for episode := 1; episode <= DefaultDropLimit+1; episode++ {
		rv.stalled = true
		for len(rv.q.items) < rv.q.cap {
			rg.botFrame(nil)
		}
		rg.botFrame(nil) // dropped
		rg.botFrame(nil) // waits for the queue to drain
		st := rg.m.Snapshot()
		if episode <= DefaultDropLimit {
			if st.Drops[DropQueue] != uint64(episode) {
				t.Fatalf("episode %d: queue drops %d", episode, st.Drops[DropQueue])
			}
			rv.stalled = false
			rg.pump()
			rg.botFrame(nil) // keyframe
			if st := rg.m.Snapshot(); st.Resyncs[ResyncDrop] != uint64(episode) {
				t.Fatalf("episode %d: drop resyncs %d", episode, st.Resyncs[ResyncDrop])
			}
			rg.botFrame(nil)
			rv.requireMatch(t)
			if _, ok := rv.frames[frameKey{42, rg.srv.frame}]; !ok {
				t.Fatalf("episode %d: viewer did not catch up", episode)
			}
			continue
		}
		// one drop too many within a minute
		if !rv.v.removed || st.Leaves[LeaveSlow] != 1 {
			t.Fatalf("slow viewer kept: %+v", st)
		}
	}
}

func TestFeedOverflowResyncs(t *testing.T) {
	rg := newRig(t, 2)
	rg.startLevel(newSynth(42, "test", 4), 2)
	rv := rg.addViewer(64)
	s := rg.srv
	for i := 0; i < 6; i++ { // the feed holds 3: 3 are lost
		s.advance()
		rg.feed(s.frameMsg(s.frame-1, nil))
	}
	rg.deliver()
	s.advance()
	rg.botMsg(s.frameMsg(s.frame-1, nil))
	st := rg.m.Snapshot()
	if st.Drops[DropFeed] != 3 || st.Resyncs[ResyncFeed] != 1 {
		t.Fatalf("stats %+v", st)
	}
	rg.botFrame(nil)
	rv.requireMatch(t)
	if _, ok := rv.frames[frameKey{42, s.frame}]; !ok {
		t.Fatal("viewer did not catch up")
	}
}

// testOversizeWithPendingReliable (a TestRelayMatchesBot case): a viewer due for a resync with a
// configstring diff pending meets a bot message filling a whole datagram.
// Neither the diff nor any part of the message may be lost: the diff goes
// alone first, the keyframe and the rest follow in as many datagrams as
// they need.
func testOversizeWithPendingReliable(t *testing.T) {
	rg := newRig(t, 0)
	rg.startLevel(newSynth(42, "test", 12), 2)
	rv := rg.addViewer(8)
	rv.stalled = true
	for len(rv.q.items) < rv.q.cap {
		rg.botFrame(nil)
	}
	// this configstring rides in a datagram that is dropped
	const idx = q2const.CS_SOUNDS + 60
	rg.botFrame(writeConfigString(idx, "misc/new.wav"))
	if rv.v.known[idx] != "" || rv.v.resync != ResyncDrop {
		t.Fatalf("known %q resync %q", rv.v.known[idx], rv.v.resync)
	}
	rv.stalled = false
	rg.pump()

	// a bot message as large as a datagram can be
	var prints []string
	big := func(m *msg.SizeBuf) {
		for i := 0; m.CurSize < maxDatagram-60; i++ {
			s := fmt.Sprintf("line %02d %s\n", i, strings.Repeat("x", 30))
			prints = append(prints, s)
			writePrint(s)(m)
		}
	}
	s := rg.srv
	s.advance()
	payload := s.frameMsg(s.frame-1, big)
	if len(payload) < maxDatagram-60 || len(payload) > maxDatagram {
		t.Fatalf("payload %d bytes", len(payload))
	}
	out := rg.m.Snapshot().DatagramsOut
	rg.botMsg(payload)
	if got := rg.m.Snapshot().DatagramsOut - out; got < 3 {
		t.Fatalf("%d datagrams: want the reliable diff alone plus a split forward", got)
	}
	if rv.c.ConfigStrings[idx] != "misc/new.wav" {
		t.Fatal("configstring lost")
	}
	if got := rv.c.Prints[len(rv.c.Prints)-len(prints):]; strings.Join(got, "") != strings.Join(prints, "") {
		t.Fatal("prints lost")
	}
	if _, ok := rv.frames[frameKey{42, s.frame}]; !ok || rv.c.Frame.DeltaFrame != -1 {
		t.Fatal("keyframe lost")
	}
	if st := rg.m.Snapshot(); st.Resyncs[ResyncDrop] != 1 || st.Drops[DropSpan] != 0 {
		t.Fatalf("stats %+v", st)
	}
	rg.botFrame(nil)
	rv.requireMatch(t)
}

// TestKeyframeTooLargeRetries: a keyframe that fits no datagram is not
// sent; the viewer stays due for one and gets it once it fits.
func TestKeyframeTooLargeRetries(t *testing.T) {
	rg := newRig(t, 0)
	s := newSynth(42, "crowd", 120)
	rg.startLevel(s, 2)
	rv := rg.addViewer(64)
	if rv.c.State == fakeclient.CaActive || rv.v.resync != ResyncJoin || rg.m.Snapshot().Drops[DropSpan] != 1 {
		t.Fatalf("state %d resync %q stats %+v", rv.c.State, rv.v.resync, rg.m.Snapshot())
	}
	s.ents = s.ents[:20] // the crowd leaves
	rg.botFrame(nil)
	if rv.c.State != fakeclient.CaActive {
		t.Fatal("no keyframe once it fits")
	}
	rg.botFrame(nil)
	rv.requireMatch(t)
}

func TestTimeoutAndKeepalive(t *testing.T) {
	rg := newRig(t, 0)
	rg.startLevel(newSynth(42, "test", 2), 2)
	rv := rg.addViewer(64)
	// the bot pauses; the relay keeps the viewer's connection alive and the
	// viewer, sending moves, stays
	out := rg.m.Snapshot().DatagramsOut
	for i := 0; i < 40; i++ {
		rg.tick(1100 * time.Millisecond) // a keepalive is due after more than a second
		rv.c.SendCmd(shared.UserCmd{Msec: 100})
		rg.pump()
	}
	if rv.v.removed || rg.m.Snapshot().DatagramsOut-out < 35 {
		t.Fatalf("removed %v, %d keepalives", rv.v.removed, rg.m.Snapshot().DatagramsOut-out)
	}
	// a silent viewer times out
	rg.tick(DefaultTimeout + time.Second)
	if !rv.v.removed || rg.m.Snapshot().Leaves[LeaveTimeout] != 1 {
		t.Fatal("no timeout")
	}
}

func TestCloseDisconnectsViewers(t *testing.T) {
	rg := newRig(t, 0)
	rg.startLevel(newSynth(42, "test", 2), 2)
	a, b := rg.addViewer(64), rg.addViewer(64)
	rg.r.closeAll()
	if n := len(a.q.items); n != 3 {
		t.Fatalf("%d final datagrams", n)
	}
	rg.pump()
	for _, rv := range []*rigViewer{a, b} {
		if !errors.Is(rv.err, fakeclient.ErrDisconnected) {
			t.Fatalf("viewer err %v", rv.err)
		}
	}
	if st := rg.m.Snapshot(); st.Leaves[LeaveClosed] != 2 || st.Viewers != 0 {
		t.Fatalf("stats %+v", st)
	}
}

func TestConnectRefusals(t *testing.T) {
	rg := newRig(t, 0)
	rg.startLevel(newSynth(42, "test", 2), 1)
	rv := &rigViewer{rg: rg, q: &sliceQueue{cap: 16}}
	rv.v = &viewer{id: 9, out: rv.q}
	rg.r.join(rv.v)
	oob := func(s string) string {
		rv.q.items = nil
		rg.r.packet(rv.v, append([]byte{0xff, 0xff, 0xff, 0xff}, s...))
		if len(rv.q.items) != 1 {
			return ""
		}
		return string(bytes.TrimRight(rv.q.items[0][4:], "\x00"))
	}
	if got := oob("connect 34 1 4321 \"\\name\\x\"\n"); got != "print\nNo challenge for address.\n" {
		t.Fatalf("%q", got)
	}
	if got := oob("getchallenge\n"); got != "challenge 4321" {
		t.Fatalf("%q", got)
	}
	if got := oob("connect 33 1 4321 \"\"\n"); got != "print\nServer is version 3.19.\n" {
		t.Fatalf("%q", got)
	}
	if got := oob("connect 34 1 1234 \"\"\n"); got != "print\nBad challenge.\n" {
		t.Fatalf("%q", got)
	}
	if got := oob("status\n"); got != "" {
		t.Fatalf("status answered %q", got)
	}
	if got := oob("connect 34 7 4321 \"\"\n"); got != "client_connect" || rv.v.state != vsConnected || rv.v.ch.Qport != 7 {
		t.Fatalf("%q state %d", got, rv.v.state)
	}
	// sequenced packets with another qport are not this viewer's
	before := rv.v.ch.IncomingSequence
	pkt := []byte{5, 0, 0, 0, 0, 0, 0, 0, 8, 0, q2const.Clc_nop}
	rg.r.packet(rv.v, pkt)
	if rv.v.ch.IncomingSequence != before {
		t.Fatal("packet with a foreign qport accepted")
	}
	pkt[8] = 7
	rg.r.packet(rv.v, pkt)
	if rv.v.ch.IncomingSequence != 5 {
		t.Fatal("packet not accepted")
	}
}
