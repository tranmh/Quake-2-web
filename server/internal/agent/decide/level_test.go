package decide

import (
	"encoding/json"
	"strings"
	"testing"

	"quake2web/server/internal/agent/worldmodel"
)

// TestLastTick: every Tick reports the fast state it projected and the
// requests it built; a nil belief resets it.
func TestLastTick(t *testing.T) {
	p, err := NewPipeline(PipelineConfig{Backend: constBackend(fallbackKeys), Scheduler: SchedulerConfig{Mode: Lockstep}})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	if ti := p.LastTick(); len(ti.Requests) != 0 || len(ti.Fast.Enemies) != 0 {
		t.Fatalf("before the first tick: %+v", ti)
	}
	b := testBelief()
	b.Time, b.Frames = 10000, 100
	p.Tick(10000, b, testObjective())
	ti := p.LastTick()
	want := p.proj.Fast(b, Context{})
	if len(ti.Fast.Enemies) == 0 || ti.Fast.Me.Health != want.Me.Health || len(ti.Fast.Enemies) != len(want.Enemies) {
		t.Fatalf("fast state %+v, want %+v", ti.Fast, want)
	}
	if len(ti.Requests) == 0 {
		t.Fatal("no request on the first tick")
	}
	b.Time, b.Frames = 10100, 101
	p.Tick(10100, b, testObjective())
	if ti2 := p.LastTick(); len(ti2.Records) == 0 {
		t.Error("the zero-latency answers were not collected on the next tick")
	}
	p.Tick(10200, nil, nil)
	if ti := p.LastTick(); len(ti.Requests) != 0 || len(ti.Records) != 0 {
		t.Errorf("after a nil belief: %+v", ti)
	}
}

// TestSetProbes: the pipeline's projector takes the level's probes: the
// path function decides the items listed and their path distances.
func TestSetProbes(t *testing.T) {
	p, err := NewPipeline(PipelineConfig{Backend: constBackend(fallbackKeys)})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	b := testBelief()
	if len(p.proj.Project(LaneSlow, b, Context{}).Items) == 0 {
		t.Fatal("no items without a path function")
	}
	p.SetProbes(nil, func(_, _ Vec3) (float32, bool) { return 0, false })
	if items := p.proj.Project(LaneSlow, b, Context{}).Items; len(items) != 0 {
		t.Errorf("unreachable items listed: %+v", items)
	}
	p.SetProbes(fixedSpace{300, 300, 300, 300}, func(_, _ Vec3) (float32, bool) { return 77, true })
	st := p.proj.Project(LaneSlow, b, Context{})
	if len(st.Items) == 0 || st.Items[0].Path != 77 || st.Space == nil {
		t.Errorf("with the probes: items %+v space %+v", st.Items, st.Space)
	}
}

// TestObjectiveExitAndEmpty: the objective's exit flag and the owned
// weapons without ammo are encoded only when present, so the golden
// states without them are unchanged.
func TestObjectiveExitAndEmpty(t *testing.T) {
	p := testProjector()
	b := testBelief()
	obj := testObjective()
	st := p.Project(LaneSlow, b, Context{Objective: obj})
	raw, err := json.Marshal(st)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), `"exit"`) || strings.Contains(string(raw), `"empty"`) {
		t.Fatalf("exit or empty encoded without them: %s", raw)
	}
	obj.Exit = true
	b.Inventory = worldmodel.Inventory{Known: true, Items: []worldmodel.InvItem{{Index: 1, Name: "Blaster", Count: 1},
		{Index: 2, Name: "Super Shotgun", Count: 1}, {Index: 3, Name: "Shells", Count: 1}}}
	b.Self.Weapon, b.Self.Ammo = "Blaster", 0
	st = p.Project(LaneSlow, b, Context{Objective: obj})
	if st.Objective == nil || !st.Objective.Exit {
		t.Errorf("objective %+v, want exit", st.Objective)
	}
	if len(st.Me.Empty) != 1 || st.Me.Empty[0] != string(WeaponSuperShotgun) {
		t.Errorf("empty weapons %v, want the super shotgun (1 shell, 2 a shot)", st.Me.Empty)
	}
}
