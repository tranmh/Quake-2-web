package campaign

import (
	"context"
	"os"
	"testing"

	"quake2web/server/internal/agent/bot"
	"quake2web/server/internal/agent/route"
	"quake2web/server/internal/agent/session"
	"quake2web/server/internal/agent/session/sessiontest"
	"quake2web/server/internal/agent/worldmodel"
	"quake2web/server/internal/fakeclient"
)

func TestZZJacket(t *testing.T) {
	if os.Getenv("ZZ_JACKET") == "" {
		t.Skip()
	}
	fs := sessiontest.DemoFS(t)
	lib := demoLibrary(t)
	md, g, err := lib.Level(context.Background(), "demo1")
	if err != nil {
		t.Fatal(err)
	}
	tab := &route.Table{Schema: route.SchemaVersion, Name: "jacket", Map: "demo1", Exit: route.ExitRef{Map: "demo2$base1"},
		Steps: []route.Step{{Op: route.OpPickup, Class: "item_armor_jacket", Pos: &route.Vec{-384, -544, -80}}}}
	l := session.NewLockstep(session.LockstepConfig{FS: fs, Spec: session.Spec{Map: "demo1", Skill: 1}, Seed: 1, Client: fakeclient.Options{MaxHistory: 256}})
	ctx := context.Background()
	if err := l.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	c := l.Client()
	c.StringCmd("god")
	c.StringCmd("notarget")
	b := bot.New(bot.Config{ReadFile: fs.ReadFile, Logf: t.Logf})
	if err := b.Enter(bot.Level{Key: worldmodel.LevelKey{Map: "demo1"}, Gen: c.LevelGen(), Map: md, Graph: g, Route: tab}); err != nil {
		t.Fatal(err)
	}
	for frame := 0; frame < 400 && !b.Route().Done(); frame++ {
		if err := l.Step(ctx, b.Cmd); err != nil {
			t.Fatal(err)
		}
		b.Observe(c, l.GameTimeMs())
		if frame%20 == 0 {
			st := b.Navigator().Status()
			t.Logf("f%d pos %v ducked %v nav %s edge %d step %d armor %d", frame, b.Belief().Self.Origin, b.Belief().Self.Ducked, st.Follow, st.Edge, st.Step, b.Belief().Self.Armor)
		}
	}
	t.Logf("done %v armor %d\n%s", b.Route().Done(), b.Belief().Self.Armor, b.Describe())
}
