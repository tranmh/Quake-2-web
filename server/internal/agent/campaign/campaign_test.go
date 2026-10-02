package campaign

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"quake2web/server/internal/agent/nav/navrt"
	"quake2web/server/internal/agent/session"
	"quake2web/server/internal/agent/trace"
	"quake2web/server/internal/fakeclient"
	"quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/msg"
)

// fakeSession is a session without a server: its client is a passive
// fakeclient that the test feeds server messages (svc_serverdata of
// cinematics and pictures, which never make a client active). Step
// advances the clock and runs the script.
type fakeSession struct {
	c       *fakeclient.Client
	clock   int64
	steps   int
	script  func(f *fakeSession)
	reloads int
	cmds    []string
}

func newFakeSession() *fakeSession {
	f := &fakeSession{c: fakeclient.NewPassive(fakeclient.Options{})}
	// a passive client has no outgoing buffer: give it one to queue the
	// string commands in (nothing is sent)
	f.c.Netchan.Message.SZ_Init(make([]byte, 4096))
	return f
}

// serverdata feeds an svc_serverdata.
func (f *fakeSession) serverdata(t testing.TB, count int32, player int16, level string) {
	t.Helper()
	sb := msg.NewSizeBuf(1400)
	sb.MSG_WriteByte(q2const.Svc_serverdata)
	sb.MSG_WriteLong(q2const.PROTOCOL_VERSION)
	sb.MSG_WriteLong(count)
	sb.MSG_WriteByte(0)
	sb.MSG_WriteString("baseq2")
	sb.MSG_WriteShort(int32(player))
	sb.MSG_WriteString(level)
	if _, err := f.c.FeedPayload(sb.Bytes()); err != nil {
		t.Fatal(err)
	}
}

// sent returns the string commands the client queued so far.
func (f *fakeSession) sent() []string {
	r := msg.NewReader(f.c.Netchan.Message.Bytes())
	var out []string
	for r.ReadCount < r.CurSize {
		if r.MSG_ReadByte() != q2const.Clc_stringcmd {
			break
		}
		out = append(out, r.MSG_ReadString())
	}
	return out
}

func (f *fakeSession) Start(context.Context) error { return nil }
func (f *fakeSession) Step(ctx context.Context, _ session.CmdFunc) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	f.clock += session.FrameMsec
	f.steps++
	if f.script != nil {
		f.script(f)
	}
	return nil
}
func (f *fakeSession) WaitActive(context.Context, int) error     { return session.ErrNotActive }
func (f *fakeSession) WaitLevel(context.Context, int, int) error { return session.ErrNotActive }
func (f *fakeSession) Client() *fakeclient.Client                { return f.c }
func (f *fakeSession) Reload(context.Context) error              { f.reloads++; return errors.New("fake: no saves") }
func (f *fakeSession) Exec(text string) error                    { f.cmds = append(f.cmds, text); return nil }
func (f *fakeSession) GameTimeMs() int64                         { return f.clock }
func (f *fakeSession) Truth() (session.Truth, error) {
	return session.Truth{}, errors.New("fake: no game")
}
func (f *fakeSession) ReadFile(string) ([]byte, error) { return nil, os.ErrNotExist }
func (f *fakeSession) MapName() string                 { return f.c.MapName() }
func (f *fakeSession) LevelGen() int                   { return f.c.LevelGen() }
func (f *fakeSession) Close() error                    { return nil }

var _ session.Session = (*fakeSession)(nil)

// fakeConfig is a Config over the checked-in campaign with a library
// that never loads anything (pictures and cinematics need no map).
func fakeConfig(t *testing.T, f *fakeSession) (Config, *[]trace.Event) {
	t.Helper()
	var events []trace.Event
	bus := trace.NewBus("fake", func() time.Time { return time.Unix(0, 0) })
	bus.AddSink(sinkFunc(func(e trace.Event) error { events = append(events, e); return nil }))
	return Config{Campaign: demoCampaign(t), Library: NewLibrary(LibraryConfig{ReadFile: f.ReadFile, Skill: 1}), Bus: bus,
		Logf: t.Logf}, &events
}

func TestRunVictoryPicture(t *testing.T) {
	f := newFakeSession()
	f.serverdata(t, 3, -1, "victory.pcx")
	cfg, events := fakeConfig(t, f)
	res, err := Run(context.Background(), f, cfg)
	if err != nil || res.Outcome != OutcomeCompleted || !res.Victory || !strings.Contains(res.Reason, "victory.pcx") {
		t.Fatalf("result %+v, %v", res, err)
	}
	if n := countEvents(*events, trace.TypeEpisodeEnd); n != 1 || (*events)[0].Type != trace.TypeEpisodeStart {
		t.Fatalf("events %v", *events)
	}
	if res.Summary.Outcome != OutcomeCompleted {
		t.Errorf("summary outcome %q", res.Summary.Outcome)
	}
}

func TestRunWrongPicture(t *testing.T) {
	f := newFakeSession()
	f.serverdata(t, 3, -1, "credits.pcx")
	cfg, _ := fakeConfig(t, f)
	res, err := Run(context.Background(), f, cfg)
	if err != nil || res.Outcome != OutcomeFailed || res.Victory {
		t.Fatalf("result %+v, %v", res, err)
	}
}

// TestRunCinematic: a cinematic is answered with "nextserver
// <spawncount>" (again every 5 s while it plays on), and the campaign
// goes on with what comes next.
func TestRunCinematic(t *testing.T) {
	f := newFakeSession()
	f.serverdata(t, 7, -1, "end.cin")
	var sent []string
	f.script = func(f *fakeSession) {
		if f.steps == 80 {
			sent = f.sent() // (a serverdata clears the client's state)
			f.serverdata(t, 9, -1, "victory.pcx")
		}
	}
	cfg, _ := fakeConfig(t, f)
	res, err := Run(context.Background(), f, cfg)
	if err != nil || !res.Victory {
		t.Fatalf("result %+v, %v", res, err)
	}
	n := 0
	for _, s := range sent {
		if s == "nextserver 7" {
			n++
		} else {
			t.Errorf("unexpected command %q", s)
		}
	}
	if n != 2 { // at once, and once more after 5 s
		t.Errorf("%d nextserver commands in 8 s, want 2", n)
	}
}

func TestRunAbortAndEpisodeWatchdog(t *testing.T) {
	t.Run("abort", func(t *testing.T) {
		f := newFakeSession()
		f.serverdata(t, 7, -1, "loop.cin")
		ctx, cancel := context.WithCancel(context.Background())
		f.script = func(f *fakeSession) {
			if f.steps == 10 {
				cancel()
			}
		}
		cfg, events := fakeConfig(t, f)
		res, err := Run(ctx, f, cfg)
		if err != nil || res.Outcome != OutcomeAborted {
			t.Fatalf("result %+v, %v", res, err)
		}
		if last := (*events)[len(*events)-1]; last.Type != trace.TypeEpisodeEnd {
			t.Errorf("last event %s", last.Type)
		}
	})
	t.Run("watchdog", func(t *testing.T) {
		f := newFakeSession()
		f.serverdata(t, 7, -1, "loop.cin")
		cfg, _ := fakeConfig(t, f)
		cfg.EpisodeTimeout = 2 * time.Second
		res, err := Run(context.Background(), f, cfg)
		if err != nil || res.Outcome != OutcomeFailed || !strings.Contains(res.Reason, "episode watchdog") || f.clock > 2200 {
			t.Fatalf("result %+v, %v after %d ms", res, err, f.clock)
		}
	})
}

func TestHelpers(t *testing.T) {
	for _, c := range []struct {
		st   navrt.Status
		want string
	}{
		{navrt.Status{Level: 1}, "jump"}, {navrt.Status{Level: 1, Cause: navrt.CauseEntity}, "strafe"},
		{navrt.Status{Level: 2}, "strafe"}, {navrt.Status{Level: 2, Cause: navrt.CauseEntity}, "jump"},
		{navrt.Status{Level: 3}, "backoff"}, {navrt.Status{Level: 4}, "repath"}, {navrt.Status{Level: 5}, "block"},
		{navrt.Status{Level: 6}, "report"},
	} {
		if got := stuckStage(c.st); got != c.want {
			t.Errorf("stage of level %d cause %s: %q, want %q", c.st.Level, c.st.Cause, got, c.want)
		}
	}
	if cheatReply("god") != "godmode" || cheatReply("notarget") != "notarget" || cheatReply("kill") != "" {
		t.Error("cheat replies")
	}
	var cfg Config
	cfg.MaxDeaths = -1
	cfg.defaults()
	if cfg.MaxDeaths != 0 || cfg.LevelTimeout != DefaultLevelTimeout || cfg.ExploreAfter != DefaultExploreAfter || cfg.Now == nil {
		t.Errorf("defaults %+v", cfg)
	}
	t.Setenv("Q2_ROUTES_DIR", "/x/routes")
	if d := DefaultRoutesDir(); d != "/x/routes" {
		t.Errorf("routes dir %q", d)
	}
}
