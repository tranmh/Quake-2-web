package worldmodel

import (
	"encoding/json"
	"math/rand"
	"reflect"
	"testing"

	"quake2web/server/internal/agent/perception"
	"quake2web/server/internal/agent/session/sessiontest"
	"quake2web/server/internal/fakeclient"
	"quake2web/server/internal/qcommon/shared"
)

// replay feeds a recorded payload stream to a Passive client and returns the
// frame inputs a Reader makes of it.
func replay(t *testing.T, payloads [][]byte) []perception.FrameInput {
	t.Helper()
	c := fakeclient.NewPassive(fakeclient.Options{MaxHistory: 256})
	r := perception.NewReader()
	var out []perception.FrameInput
	for i, p := range payloads {
		if _, err := c.FeedPayload(p); err != nil {
			t.Fatalf("payload %d: %v", i, err)
		}
		if in, ok := r.Next(c); ok {
			out = append(out, in)
		}
	}
	return out
}

func sameSet(a, b []int32) bool { return reflect.DeepEqual(a, b) }

// perturber rewrites the entities a percept did not admit.
type perturber struct {
	rng     *rand.Rand
	checker *perception.Perceiver
	changed int // entity states rewritten
	frames  int // frames with at least one rewrite
	retries int
}

func (p *perturber) randomize(e *shared.EntityState, orig shared.EntityState) {
	*e = orig
	for i := 0; i < 2; i++ {
		e.Origin[i] = orig.Origin[i] + float32(p.rng.Intn(2001)-1000)
	}
	e.Origin[2] = orig.Origin[2] + float32(p.rng.Intn(401)-200)
	e.Angles = Vec3{float32(p.rng.Intn(360)), float32(p.rng.Intn(360)), float32(p.rng.Intn(360))}
	e.Frame = int32(p.rng.Intn(200))
	e.SkinNum = orig.SkinNum ^ 1
	if e.RenderFX != 0 && orig.OldOrigin != (Vec3{}) {
		e.OldOrigin = shared.VectorAdd(e.Origin, shared.VectorSubtract(orig.OldOrigin, orig.Origin))
	}
}

// perturb returns a copy of in whose entities that the filter did not admit
// have random origins, angles, frames and skins. The new values are
// resampled until the filter still admits exactly the same entities (a
// hidden entity moved into view would be seen, which is not a leak), and
// put back if that fails. Brush entities keep their pose: an occluding
// brush is by definition visible where it occludes.
func (p *perturber) perturb(in perception.FrameInput) perception.FrameInput {
	admitted := p.checker.Perceive(&in).Admitted()
	adm := map[int32]bool{}
	for _, n := range admitted {
		adm[n] = true
	}
	out := in
	out.Entities = append([]shared.EntityState(nil), in.Entities...)
	cls := p.checker.Classifier()
	var idx []int
	for i := range out.Entities {
		e := &out.Entities[i]
		if adm[e.Number] || e.Number == in.OwnEntity() || cls.InlineModel(e.ModelIndex) != 0 {
			continue
		}
		p.randomize(e, in.Entities[i])
		idx = append(idx, i)
	}
	for attempt := 0; ; attempt++ {
		got := p.checker.Perceive(&out).Admitted()
		if sameSet(got, admitted) {
			break
		}
		p.retries++
		bad := map[int32]bool{}
		for _, n := range got {
			if !adm[n] {
				bad[n] = true
			}
		}
		for _, i := range idx {
			if bad[out.Entities[i].Number] {
				if attempt < 20 {
					p.randomize(&out.Entities[i], in.Entities[i])
				} else {
					out.Entities[i] = in.Entities[i]
				}
			}
		}
		if attempt > 21 {
			panic("perturbation does not converge")
		}
	}
	n := 0
	for _, i := range idx {
		if out.Entities[i] != in.Entities[i] {
			n++
		}
	}
	p.changed += n
	if n > 0 {
		p.frames++
	}
	return out
}

// TestPerturbationInvariance is the fairness test of the ObservationFilter:
// the bot's payload stream from a lockstep run is replayed through a
// Passive client twice, once verbatim and once with every entity the
// filter did not admit at that frame rewritten (origin, angles, frame,
// skin), and the world model's beliefs must be identical at every frame.
func TestPerturbationInvariance(t *testing.T) {
	// 60 s: the soldier that comes hunting is first seen after about 35 s
	recs, payloads := runDemo1(t, 600, nil)
	inputs := replay(t, payloads)
	if len(inputs) != len(recs) {
		t.Fatalf("replay made %d frames, the live run %d", len(inputs), len(recs))
	}
	for i := range inputs {
		if inputs[i].ServerFrame != recs[i].in.ServerFrame || !reflect.DeepEqual(inputs[i].Entities, recs[i].in.Entities) ||
			inputs[i].PlayerState != recs[i].in.PlayerState || !reflect.DeepEqual(inputs[i].Events, recs[i].in.Events) {
			t.Fatalf("frame %d: the replayed input differs from the live one", i)
		}
	}

	fs := sessiontest.DemoFS(t)
	lv := loadLevel(t, fs, "demo1", 0)
	cfg := Config{ReadFile: fs.ReadFile}
	wa, wb, wc := New(cfg), New(cfg), New(cfg)
	wa.Reset(lv)
	wb.Reset(lv)
	wc.Reset(lv) // control: admitted monsters moved, which must show
	controlDiffers := false
	table := perception.NewClassTable()
	p := &perturber{rng: rand.New(rand.NewSource(7)),
		checker: perception.NewPerceiver(lv.Map.CM, perception.NewClassifier(table), perception.NewAnimCache(fs.ReadFile), perception.Options{})}
	hiddenMonsters := 0
	for i, in := range inputs {
		pert := p.perturb(in)
		for k := range pert.Entities {
			if pert.Entities[k] != in.Entities[k] &&
				p.checker.Classifier().Classify(&in.Entities[k]).Class.Kind == perception.KindMonster {
				hiddenMonsters++
			}
		}
		wa.Update(in, recs[i].now)
		wb.Update(pert, recs[i].now)
		ctl := in
		ctl.Entities = append([]shared.EntityState(nil), in.Entities...)
		for k := range ctl.Entities {
			if s := wa.Percept().Sighting(ctl.Entities[k].Number); s != nil && s.Class.Kind == perception.KindMonster {
				ctl.Entities[k].Origin[0] += 8
			}
		}
		wc.Update(ctl, recs[i].now)
		controlDiffers = controlDiffers || wc.Belief().Digest() != wa.Belief().Digest()
		da, db := wa.Belief().Digest(), wb.Belief().Digest()
		if da != db {
			ja, _ := json.Marshal(wa.Belief())
			jb, _ := json.Marshal(wb.Belief())
			t.Fatalf("frame %d (t=%d): beliefs differ\nverbatim:  %s\nperturbed: %s", i, recs[i].now, ja, jb)
		}
	}
	t.Logf("%d frames, %d entity states rewritten in %d frames (%d of hidden monsters), %d resamples",
		len(inputs), p.changed, p.frames, hiddenMonsters, p.retries)
	if p.frames < len(inputs)/2 || hiddenMonsters == 0 {
		t.Fatalf("the perturbation did not touch enough: %d frames, %d hidden monster states", p.frames, hiddenMonsters)
	}
	if len(wa.Belief().Tracks) == 0 {
		t.Fatal("no tracks: the run perceived nothing")
	}
	if !controlDiffers {
		t.Fatal("moving the seen monsters did not change the belief: the comparison is blind")
	}
}
