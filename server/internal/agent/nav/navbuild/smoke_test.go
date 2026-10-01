package navbuild

import (
	"context"
	"testing"

	"quake2web/server/internal/assets/pak"
	"quake2web/server/internal/testutil"
)

func TestSmokeBuild(t *testing.T) {
	p, err := pak.Open(testutil.DemoPak(t))
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	raw, err := p.ReadFile("maps/demo1.bsp")
	if err != nil {
		t.Fatal(err)
	}
	g, rep, err := Build(context.Background(), "demo1", raw, Config{Logf: t.Logf})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%s", rep)
	t.Logf("stats %+v", g.Stats())
}
