//go:build oracle

package host

// Differential smoke test against the original C dedicated server.
//
// Run with: go test -tags oracle ./internal/host -run Oracle -v
// Skipped unless oracle/build/bin/q2ded (and baseq2/game.so, the demo pak)
// exist.
//
// The same fakeclient connects over UDP to
//   - the real C q2ded (+set deathmatch 1 +map demo1, real game.so), and
//   - the Go server running the stub game (dedicated, deathmatch 1, demo1).
//
// Both handshakes must complete (getchallenge/connect/new/configstrings/
// baselines/begin/first frame). Compared, because they come from the engine
// and not from the game:
//   - svc_serverdata: protocol, attractloop, gamedir, playernum, levelname
//     (servercount is logged: both are the first rand() of the process)
//   - the configstrings the engine writes in SV_SpawnServer: CS_MODELS+1
//     (map), CS_MODELS+2.. ("*1".."*N", the inline models), CS_MAPCHECKSUM,
//     CS_AIRACCEL, and CS_NAME (worldspawn "message", set by both games)
//   - the shape of the stufftext handshake: "cmd configstrings N 0",
//     "cmd configstrings N k"..., "cmd baselines N 0", "cmd baselines N k"...,
//     "precache N" with the same servercount N as svc_serverdata
//   - baselines structure: entity numbers differ (the stub spawns fewer
//     entities), so baselines are matched by the inline model ("*N") they
//     use: the Go set (brush entities spawned by the stub) must be a subset of
//     the C set (on demo1 DM they are equal: 11 == 11) and matched baselines
//     must have identical origins; total counts are logged.
//   - the first frame: valid, full (deltaframe -1), player entity present.
// Not compared: game configstrings (sounds, images, items, statusbar,
// lightstyles), entity numbering, monsters/items baselines, player state.

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"quake2web/server/internal/cmodel"
	"quake2web/server/internal/fakeclient"
	qnet "quake2web/server/internal/net"
	"quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/shared"
	"quake2web/server/internal/sv"
	"quake2web/server/internal/sv/stubgame"
	"quake2web/server/internal/testutil"
)

func handshakeUDP(t *testing.T, addr string) *fakeclient.Client {
	t.Helper()
	conn, err := qnet.DialUDP(addr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	c := fakeclient.New(conn, fakeclient.Options{})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := c.Handshake(ctx); err != nil {
		t.Fatalf("handshake with %s: %v", addr, err)
	}
	return c
}

func inlineBaselineModels(c *fakeclient.Client) map[string]int {
	out := map[string]int{}
	for i := range c.Entities {
		e := &c.Entities[i]
		if !e.HasBaseline {
			continue
		}
		mi := int(e.Baseline.ModelIndex)
		if mi <= 0 || mi >= q2const.MAX_MODELS {
			continue
		}
		name := c.ConfigStrings[q2const.CS_MODELS+mi]
		if strings.HasPrefix(name, "*") {
			out[name]++
		}
	}
	return out
}

func inlineBaselines(c *fakeclient.Client) map[string]shared.EntityState {
	out := map[string]shared.EntityState{}
	for i := range c.Entities {
		e := &c.Entities[i]
		mi := int(e.Baseline.ModelIndex)
		if !e.HasBaseline || mi <= 0 || mi >= q2const.MAX_MODELS {
			continue
		}
		if name := c.ConfigStrings[q2const.CS_MODELS+mi]; strings.HasPrefix(name, "*") {
			out[name] = e.Baseline
		}
	}
	return out
}

// modelClasses maps "*N" to the classname of the entity using it on demo1.
func modelClasses(t *testing.T) map[string]string {
	raw, err := demoFS(t).ReadFile("maps/demo1.bsp")
	if err != nil {
		t.Fatal(err)
	}
	m, err := cmodel.LoadMapBytes("maps/demo1.bsp", raw)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	data := m.EntityString()
	kv := map[string]string{}
	for {
		tok, rest, ok := shared.COM_Parse(data)
		data = rest
		if !ok || tok == "" {
			break
		}
		switch tok {
		case "{":
			kv = map[string]string{}
		case "}":
			if strings.HasPrefix(kv["model"], "*") {
				out[kv["model"]] = kv["classname"]
			}
		default:
			val, rest, _ := shared.COM_Parse(data)
			data = rest
			kv[tok] = val
		}
	}
	return out
}

var stuffRe = regexp.MustCompile(`^(cmd configstrings|cmd baselines|precache) (-?\d+)( \d+)?\n$`)

func stuffShape(t *testing.T, c *fakeclient.Client) []string {
	t.Helper()
	var out []string
	for _, s := range c.StuffTexts {
		m := stuffRe.FindStringSubmatch(s)
		if m == nil {
			continue
		}
		if m[2] != fmt.Sprint(c.ServerData.ServerCount) {
			t.Errorf("stufftext %q: servercount %s != %d", s, m[2], c.ServerData.ServerCount)
		}
		kind := m[1]
		if m[3] == " 0" {
			kind += " 0"
		} else if m[3] != "" {
			kind += " k"
		}
		if len(out) == 0 || out[len(out)-1] != kind || !strings.HasSuffix(kind, " k") {
			out = append(out, kind)
		}
	}
	return out
}

func TestOracleHandshakeDiff(t *testing.T) {
	cAddr := testutil.StartQ2Ded(t, "+set", "deathmatch", "1", "+map", "demo1")

	h := New()
	_, err := h.Create(InstanceConfig{
		ID: "go",
		Server: sv.Config{
			FS:        demoFS(t),
			Game:      stubgame.New(),
			Dedicated: true,
			Cvars:     [][2]string{{"deathmatch", "1"}},
		},
		Commands: []string{"map demo1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.Shutdown)
	l, err := qnet.ListenUDP("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	go func() { _ = h.ServeUDP(l, "go") }()
	goAddr := l.LocalAddr().String()

	cc := handshakeUDP(t, cAddr)
	gc := handshakeUDP(t, goAddr)

	// svc_serverdata
	cs, gs := cc.ServerData, gc.ServerData
	t.Logf("C  serverdata %+v", cs)
	t.Logf("Go serverdata %+v", gs)
	if cs.Protocol != gs.Protocol || cs.AttractLoop != gs.AttractLoop || cs.GameDir != gs.GameDir ||
		cs.PlayerNum != gs.PlayerNum || cs.LevelName != gs.LevelName {
		t.Errorf("serverdata differs")
	}
	if cs.ServerCount != gs.ServerCount {
		t.Logf("note: servercount differs (C %d, Go %d)", cs.ServerCount, gs.ServerCount)
	}

	// engine configstrings
	for _, i := range []int{q2const.CS_NAME, q2const.CS_MODELS + 1, q2const.CS_MAPCHECKSUM, q2const.CS_AIRACCEL} {
		if cc.ConfigStrings[i] != gc.ConfigStrings[i] {
			t.Errorf("configstring %d: C %q Go %q", i, cc.ConfigStrings[i], gc.ConfigStrings[i])
		}
	}
	nInline := 0
	for i := q2const.CS_MODELS + 2; i < q2const.CS_MODELS+q2const.MAX_MODELS; i++ {
		want := cc.ConfigStrings[i]
		if !strings.HasPrefix(want, "*") {
			break
		}
		nInline++
		if gc.ConfigStrings[i] != want {
			t.Errorf("configstring %d: C %q Go %q", i, want, gc.ConfigStrings[i])
		}
	}
	if nInline == 0 {
		t.Errorf("no inline model configstrings from C")
	}
	if g := gc.ConfigStrings[q2const.CS_MODELS+2+nInline]; strings.HasPrefix(g, "*") {
		t.Errorf("Go has more inline models: %q", g)
	}
	t.Logf("world model configstrings identical: map + %d inline models, checksum %s",
		nInline, cc.ConfigStrings[q2const.CS_MAPCHECKSUM])

	// stufftext handshake shape
	cShape, gShape := stuffShape(t, cc), stuffShape(t, gc)
	t.Logf("C  stufftext shape %v", cShape)
	t.Logf("Go stufftext shape %v", gShape)
	valid := func(shape []string) bool {
		s := strings.Join(shape, ",")
		return regexp.MustCompile(`^cmd configstrings 0(,cmd configstrings k)*,cmd baselines 0(,cmd baselines k)*,precache$`).MatchString(s)
	}
	if !valid(cShape) || !valid(gShape) {
		t.Errorf("unexpected handshake shape")
	}

	// baselines
	cm, gm := inlineBaselineModels(cc), inlineBaselineModels(gc)
	var extra []string
	for name := range gm {
		if cm[name] == 0 {
			extra = append(extra, name)
		}
	}
	sort.Strings(extra)
	t.Logf("baselines: C %d total, %d with inline models; Go %d total, %d with inline models",
		cc.NumBaselines, len(cm), gc.NumBaselines, len(gm))
	if len(extra) > 0 {
		t.Errorf("Go baselines use inline models the C server has no baseline for: %v", extra)
	}
	if len(gm) == 0 {
		t.Errorf("Go server sent no inline model baselines")
	}
	// the brush entities' baseline origins come from the same entity string
	// (func_train origins are computed by the game from path_corners)
	classes := modelClasses(t)
	cb, gb := inlineBaselines(cc), inlineBaselines(gc)
	same := 0
	for name, g := range gb {
		c, ok := cb[name]
		if !ok || cm[name] != 1 || gm[name] != 1 || classes[name] == "func_train" {
			continue
		}
		if c.Origin != g.Origin {
			t.Errorf("baseline %s origin: C %v Go %v", name, c.Origin, g.Origin)
		} else {
			same++
		}
	}
	t.Logf("%d inline-model baselines with identical origins", same)

	// first frame
	for name, c := range map[string]*fakeclient.Client{"C": cc, "Go": gc} {
		if !c.Frame.Valid {
			t.Errorf("%s: frame not valid", name)
		}
		found := false
		for _, e := range c.FrameEntities(&c.Frame) {
			if e.Number == c.ServerData.PlayerNum+1 {
				found = true
			}
		}
		if !found {
			t.Errorf("%s: player entity not in frame", name)
		}
	}
	cc.Disconnect()
	gc.Disconnect()
}
