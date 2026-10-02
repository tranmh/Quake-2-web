package worldmodel_test

import (
	"encoding/json"
	"testing"

	"quake2web/server/internal/agent/backend/scripted"
	"quake2web/server/internal/agent/decide"
	"quake2web/server/internal/agent/worldmodel"
)

// TestPerturbationInvariance runs worldmodel.PerturbationRun: the beliefs
// of the verbatim and the perturbed stream must be identical at every
// frame, and so must the decisions made from them: both lane states and
// the scripted policy's answers to them. A heard-only monster moved
// anywhere within its sounds' cues (and given any velocity) changes
// nothing the bot decides.
func TestPerturbationInvariance(t *testing.T) {
	proj := decide.NewProjector(decide.ProjectorConfig{})
	pol := scripted.NewPolicy(scripted.Config{})
	enemies, heard := 0, 0
	lane := func(w *worldmodel.World) (string, []decide.Enemy) {
		b := w.Belief()
		cx := decide.Context{Mode: decide.ModeObjective}
		fast := proj.Fast(b, cx)
		slow := fast
		proj.Extend(&slow, b, cx)
		d := pol.Decide(&slow, b.Time)
		j, err := json.Marshal(struct {
			Fast, Slow decide.State
			Decision   scripted.Decision
		}{fast, slow, d})
		if err != nil {
			t.Fatal(err)
		}
		return string(j), fast.Enemies
	}
	worldmodel.PerturbationRun(t, func(frame int, a, b *worldmodel.World) {
		ja, en := lane(a)
		jb, _ := lane(b)
		if ja != jb {
			t.Fatalf("frame %d: decisions differ\nverbatim:  %s\nperturbed: %s", frame, ja, jb)
		}
		for _, e := range en {
			enemies++
			if e.Heard != "" {
				heard++
			}
		}
	})
	t.Logf("%d enemy entries in the fast lane, %d of them placed by ear", enemies, heard)
	if enemies == 0 {
		t.Fatal("no enemies in the lane states: the decision comparison is blind")
	}
}
