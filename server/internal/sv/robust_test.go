package sv

import (
	"bytes"
	"errors"
	"fmt"
	"testing"
	"time"

	"quake2web/server/internal/game"
	"quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/crand"
	"quake2web/server/internal/sv/stubgame"
)

// mustNotPanic runs fn and fails the test (instead of crashing the test
// binary) when it panics.
func mustNotPanic(t *testing.T, what string, fn func() error) error {
	t.Helper()
	var err error
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("%s panicked: %v", what, r)
			}
		}()
		err = fn()
	}()
	return err
}

// A spawned client sending a full MAX_MSGLEN datagram whose last byte is
// clc_move made the checksum slice nm.Data[checksumIndex+1:end] start past
// its (clamped) end: runtime panic, not a Com_Error, so it escaped the
// instance recovery and killed the whole process.
func TestClcMoveAtEndOfFullPacketDoesNotPanic(t *testing.T) {
	s := newSynthServer(t, false)
	c := newTClient(t, s, "10.0.0.1", 1)
	c.connect(4242, "\\name\\p\\rate\\25000")
	c.spawn()

	payload := bytes.Repeat([]byte{q2const.Clc_nop}, q2const.MAX_MSGLEN-10-1)
	payload = append(payload, q2const.Clc_move)
	mustNotPanic(t, "clc_move at end of full packet", func() error { return c.send(payload) })
	if !s.InGame() {
		t.Fatalf("server went down")
	}
}

// "configstrings <spawncount> -2147483648" used to spin ~2^31 iterations
// (the memory-safety guard skipped negative slots one by one); 7 such
// commands per datagram hung the instance for many seconds.
func TestConfigstringsNegativeStartIsBounded(t *testing.T) {
	s := newSynthServer(t, false)
	c := newTClient(t, s, "10.0.0.2", 1)
	c.connect(4243, "\\name\\p")
	c.send(stringCmd("new"))

	var payload []byte
	for i := 0; i < MAX_STRINGCMDS-1; i++ {
		payload = append(payload, stringCmd(fmt.Sprintf("configstrings %d -2147483648", s.SVS.SpawnCount))...)
	}
	t0 := time.Now()
	mustNotPanic(t, "configstrings", func() error { return c.send(payload) })
	if d := time.Since(t0); d > time.Second {
		t.Fatalf("configstrings with negative start took %v", d)
	}
	if cl := c.slot(); cl == nil || cl.State != cs_connected {
		t.Fatalf("client state changed")
	}
}

func TestBaselinesNegativeStartIsBounded(t *testing.T) {
	s := newSynthServer(t, false)
	c := newTClient(t, s, "10.0.0.3", 1)
	c.connect(4244, "\\name\\p")
	c.send(stringCmd("new"))

	var payload []byte
	for i := 0; i < MAX_STRINGCMDS-1; i++ {
		payload = append(payload, stringCmd(fmt.Sprintf("baselines %d -2147483648", s.SVS.SpawnCount))...)
	}
	t0 := time.Now()
	mustNotPanic(t, "baselines", func() error { return c.send(payload) })
	if d := time.Since(t0); d > time.Second {
		t.Fatalf("baselines with negative start took %v", d)
	}
}

// panicGame is the stub game with Go runtime bugs: ClientCommand "boom"
// writes to a nil map; RunFrame does the same once frameBoom is set.
type panicGame struct {
	game.Export
	frameBoom *bool
}

func (g *panicGame) ClientCommand(ent *game.Edict) {
	var m map[string]int
	m["x"]++ // assignment to entry in nil map
}

func (g *panicGame) RunFrame() {
	if *g.frameBoom {
		var m map[string]int
		m["x"]++
	}
	g.Export.RunFrame()
}

// A runtime panic (not a Com_Error) anywhere below HandlePacket/Frame used to
// be re-panicked by recoverError and escape the instance goroutine, killing
// every game on the process. By default it now ends only this instance.
func TestRuntimePanicEndsOnlyTheInstance(t *testing.T) {
	boom := false
	gf := func(gi game.Import) game.Export { return &panicGame{stubgame.New()(gi), &boom} }
	s := newSynthServerWith(t, gf, crand.New(1))
	c := newTClient(t, s, "10.0.0.4", 1)
	c.connect(4245, "\\name\\p")
	c.spawn()
	c.allowInternal = true
	err := mustNotPanic(t, "game command", func() error { return c.send(stringCmd("boom")) })
	var ie *InternalError
	if !errors.As(err, &ie) || ie.ClientDropped {
		t.Fatalf("want an instance-level InternalError, got %v", err)
	}
	if !s.Killed() || s.SVS.Initialized {
		t.Fatalf("instance still running after an internal error")
	}
}

// With Config.DropClientOnPanic a panic while executing one client's message
// drops only that client; the level keeps running for the others.
func TestRuntimePanicInClientMessageDropsOnlyThatClient(t *testing.T) {
	boom := false
	gf := func(gi game.Import) game.Export { return &panicGame{stubgame.New()(gi), &boom} }
	s := newSynthServerWith(t, gf, crand.New(1))
	s.cfg.DropClientOnPanic = true
	other := newTClient(t, s, "10.0.0.5", 1)
	other.connect(4246, "\\name\\other")
	other.spawn()
	c := newTClient(t, s, "10.0.0.4", 1)
	c.connect(4245, "\\name\\p")
	c.spawn()
	c.allowInternal = true
	err := mustNotPanic(t, "game command", func() error { return c.send(stringCmd("boom")) })
	var ie *InternalError
	if !errors.As(err, &ie) || !ie.ClientDropped {
		t.Fatalf("want an InternalError with ClientDropped, got %v", err)
	}
	if s.Killed() || !s.InGame() {
		t.Fatalf("instance went down for a panic in one client's command")
	}
	if cl := c.slot(); cl == nil || cl.State != cs_zombie {
		t.Fatalf("offending client not dropped")
	}
	if cl := other.slot(); cl == nil || cl.State != cs_spawned {
		t.Fatalf("other client affected")
	}
	if _, err := s.Frame(100); err != nil || !s.InGame() {
		t.Fatalf("frame after contained panic: %v", err)
	}
	t.Logf("reported: %v", err)
}

// Outside a client's message (here the game frame) the instance state cannot
// be trusted any more: it ends only this instance, like ERR_FATAL.
func TestRuntimePanicInFrameEndsOnlyTheInstance(t *testing.T) {
	boom := false
	gf := func(gi game.Import) game.Export { return &panicGame{stubgame.New()(gi), &boom} }
	s := newSynthServerWith(t, gf, crand.New(1))
	boom = true
	var err error
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("Frame panicked: %v", r)
			}
		}()
		for i := 0; i < 50 && err == nil; i++ { // until a game frame is due
			_, err = s.Frame(100)
		}
	}()
	var ie *InternalError
	if !errors.As(err, &ie) || ie.ClientDropped {
		t.Fatalf("want an instance-level InternalError, got %v", err)
	}
	if !s.Killed() || s.SVS.Initialized {
		t.Fatalf("instance still running after an internal error")
	}
}

// ge->ReadGame reallocates the edicts, but SV_InitGame had pointed every
// client slot at the old array (C: dangling pointers). Until a client
// reconnected, "save" dereferenced the stale edict's nil client.
func TestLoadgameRepointsClientEdicts(t *testing.T) {
	rng := crand.New(1)
	gf := func(gi game.Import) game.Export { return game.New(gi, rng) }
	s := New(Config{FS: synthFS{}, Game: gf, Rand: rng, Clock: func() int { return 0 },
		Cvars: [][2]string{{"deathmatch", "0"}, {"maxclients", "1"}}})
	if err := s.ExecuteText("map synth\n"); err != nil {
		t.Fatal(err)
	}
	c := newTClient(t, s, "10.0.0.9", 1)
	c.connect(9, "\\name\\p")
	c.spawn()
	if err := s.ExecuteText("save s1\n"); err != nil {
		t.Fatal(err)
	}
	if err := s.ExecuteText("load s1\n"); err != nil {
		t.Fatal(err)
	}
	for i := range s.SVS.Clients {
		if s.SVS.Clients[i].Edict != s.edictNum(i+1) {
			t.Errorf("client %d edict points at a stale edict array", i)
		}
	}
	if err := s.ExecuteText("save s2\n"); err != nil {
		t.Fatalf("save after load: %v", err)
	}
}

// On a picture / cinematic server (e.g. the coop "victory.pcx" end screen)
// a real client never sends "begin", but a malicious one echoing the spawn
// count made the game spawn a player on the empty map: gi.error("Couldn't
// find spawn point") -> ERR_DROP, the instance shut down for everyone.
func TestBeginOnPicServerIgnored(t *testing.T) {
	for _, mode := range [][2]string{{"coop", "1"}, {"deathmatch", "1"}} {
		rng := crand.New(1)
		gf := func(gi game.Import) game.Export { return game.New(gi, rng) }
		s := New(Config{FS: synthFS{}, Game: gf, Rand: rng, Clock: func() int { return 0 }, Dedicated: true,
			Cvars: [][2]string{mode, {"maxclients", "4"}}})
		if err := s.ExecuteText("map victory.pcx\n"); err != nil {
			t.Fatal(err)
		}
		c := newTClient(t, s, "10.0.0.10", 1)
		c.connect(10, "\\name\\p")
		if err := c.send(stringCmd("new")); err != nil {
			t.Fatal(err)
		}
		err := c.send(stringCmd(fmt.Sprintf("begin %d", s.SVS.SpawnCount)))
		if err != nil || !s.SVS.Initialized {
			t.Fatalf("%v: begin on a pic server shut it down: %v", mode, err)
		}
	}
}

// SV_ExecuteUserCommand tokenizes client commands with macro expansion, so
// "say $rcon_password" broadcast the rcon password (or "password",
// "spectator_password", CTF "admin_password", ...) to every player: with
// rcon_password set, any player got full rcon. Client macros may only name
// public (CVAR_SERVERINFO) cvars; others expand to "" like unknown ones.
func TestClientMacroCannotReadPrivateCvars(t *testing.T) {
	s := newSynthServer(t, true, [2]string{"rcon_password", "hunter2"}, [2]string{"hostname", "pubhost"})
	c := newTClient(t, s, "10.0.0.11", 1)
	c.connect(11, "\\name\\p\\rate\\25000")
	c.spawn()
	c.ch.Transmit(append(stringCmd("say $rcon_password"), stringCmd("say $hostname")...), 0)
	for _, p := range c.out.take() {
		if err := c.raw(p); err != nil {
			t.Fatal(err)
		}
	}
	var got []byte
	for i := 0; i < 5; i++ {
		if _, err := s.Frame(100); err != nil {
			t.Fatal(err)
		}
		for _, p := range c.in.take() {
			got = append(got, p...)
		}
	}
	if bytes.Contains(got, []byte("hunter2")) {
		t.Errorf("rcon_password leaked to a client through $ expansion")
	}
	if !bytes.Contains(got, []byte("p: pubhost")) {
		t.Errorf("public (serverinfo) cvar no longer expands")
	}
}
