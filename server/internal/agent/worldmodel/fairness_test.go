package worldmodel

import (
	"encoding/json"
	"math"
	"math/rand"
	"reflect"
	"sort"
	"testing"

	"quake2web/server/internal/agent/perception"
	"quake2web/server/internal/agent/session/sessiontest"
	"quake2web/server/internal/fakeclient"
	"quake2web/server/internal/q2const"
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

// perturber rewrites what a percept did not admit.
type perturber struct {
	rng     *rand.Rand
	checker *perception.Perceiver
	changed int // entity states rewritten
	frames  int // frames with at least one rewrite
	retries int
	// by kind of rewrite
	hiddenMonsters, brushes, heardOnly, oldOrigins, removed, injected, injectedTracked int
}

// signature is what must not change: the entities seen and those admitted.
func signature(pc *perception.Percept) [2][]int32 {
	var seen []int32
	for i := range pc.Seen {
		seen = append(seen, pc.Seen[i].Num)
	}
	return [2][]int32{seen, pc.Admitted()}
}

func (p *perturber) offset(max int) float32 { return float32(p.rng.Intn(2*max+1) - max) }

// randomize rewrites a hidden entity: everything a player could learn by
// seeing it.
func (p *perturber) randomize(e *shared.EntityState, orig shared.EntityState) {
	*e = orig
	for i := 0; i < 2; i++ {
		e.Origin[i] = orig.Origin[i] + p.offset(1000)
	}
	e.Origin[2] = orig.Origin[2] + p.offset(200)
	e.Angles = Vec3{float32(p.rng.Intn(360)), float32(p.rng.Intn(360)), float32(p.rng.Intn(360))}
	e.Frame = int32(p.rng.Intn(200))
	e.SkinNum = orig.SkinNum ^ 1
	if e.RenderFX != 0 && orig.OldOrigin != (Vec3{}) {
		e.OldOrigin = shared.VectorAdd(e.Origin, shared.VectorSubtract(orig.OldOrigin, orig.Origin))
	}
}

// randomizeHeard rewrites an entity admitted only by a sound or flash at its
// origin: everything but the origin (and what makes the sound).
func (p *perturber) randomizeHeard(e *shared.EntityState, orig shared.EntityState) {
	*e = orig
	e.Angles = Vec3{float32(p.rng.Intn(360)), float32(p.rng.Intn(360)), float32(p.rng.Intn(360))}
	e.Frame = int32(p.rng.Intn(200))
	e.SkinNum = orig.SkinNum ^ 1
	e.Solid = int32(p.rng.Intn(1 << 15))
	if e.RenderFX&q2const.RF_BEAM == 0 {
		e.OldOrigin = shared.VectorAdd(orig.Origin, Vec3{p.offset(300), p.offset(300), p.offset(100)})
	}
}

// outsideFrustum reports whether the box lies wholly outside the view
// frustum of v: all its corners beyond one of the frustum planes. No line
// of sight to a point in view can touch such a box.
func outsideFrustum(v *perception.Vision, lo, hi Vec3) bool {
	fwd, right, up := v.Axes()
	fx, fy := v.Fov()
	tx, ty := math.Tan(float64(fx)*math.Pi/360), math.Tan(float64(fy)*math.Pi/360)
	eye := v.Eye()
	planes := []func(x, r, u float64) bool{
		func(x, r, u float64) bool { return x < -1 },
		func(x, r, u float64) bool { return r > x*tx+1 },
		func(x, r, u float64) bool { return -r > x*tx+1 },
		func(x, r, u float64) bool { return u > x*ty+1 },
		func(x, r, u float64) bool { return -u > x*ty+1 },
	}
	for _, out := range planes {
		all := true
		for c := 0; c < 8 && all; c++ {
			p := Vec3{lo[0], lo[1], lo[2]}
			if c&1 != 0 {
				p[0] = hi[0]
			}
			if c&2 != 0 {
				p[1] = hi[1]
			}
			if c&4 != 0 {
				p[2] = hi[2]
			}
			d := shared.VectorSubtract(p, eye)
			all = out(float64(shared.DotProduct(d, fwd)), float64(shared.DotProduct(d, right)), float64(shared.DotProduct(d, up)))
		}
		if all {
			return true
		}
	}
	return false
}

func boxesTouch(alo, ahi, blo, bhi Vec3) bool {
	for i := 0; i < 3; i++ {
		if alo[i] > bhi[i] || ahi[i] < blo[i] {
			return false
		}
	}
	return true
}

// movableBrush reports whether a brush entity's pose is outside what the
// player can perceive: its box wholly outside the view frustum and clear of
// the player's own box (whose water contents the player feels).
func (p *perturber) movableBrush(v *perception.Vision, inline int, e *shared.EntityState, own Vec3) bool {
	lo, hi, ok := v.BrushBounds(inline, e.Origin, e.Angles)
	if !ok {
		return false
	}
	return outsideFrustum(v, lo, hi) &&
		!boxesTouch(lo, hi, shared.VectorAdd(own, Vec3{-48, -48, -48}), shared.VectorAdd(own, Vec3{48, 48, 64}))
}

// rewrite kinds of an entity of the frame
const (
	keep = iota
	hidden
	heardOnly
	brush
	oldOrigin
)

// perturb returns a copy of in where everything the filter did not admit
// is rewritten:
//   - hidden entities: origin, angles, frame and skin;
//   - entities admitted only by a sound at their origin: all but the origin;
//   - seen entities whose old_origin is out of view: old_origin;
//   - brush entities wholly outside the view frustum: their pose (inside
//     it an occluding brush is by definition visible where it occludes);
//   - packet membership: half the hidden entities that no event of the
//     frame names are dropped from the packet (the PVS is hidden state).
//
// New values are resampled until the filter still sees and admits exactly
// the same entities (a hidden entity moved into view would be seen, which
// is not a leak), and put back if that fails.
func (p *perturber) perturb(in perception.FrameInput) perception.FrameInput {
	pc := p.checker.Perceive(&in)
	sig := signature(pc)
	v := pc.Vision()
	adm, seen := map[int32]bool{}, map[int32]bool{}
	for _, n := range sig[1] {
		adm[n] = true
	}
	for _, n := range sig[0] {
		seen[n] = true
	}
	out := in
	out.Entities = append([]shared.EntityState(nil), in.Entities...)
	cls := p.checker.Classifier()
	ownOrigin := Vec3{float32(in.PlayerState.PMove.Origin[0]) / 8, float32(in.PlayerState.PMove.Origin[1]) / 8,
		float32(in.PlayerState.PMove.Origin[2]) / 8}
	kind := make([]int, len(out.Entities))
	for i := range out.Entities {
		e := &out.Entities[i]
		n := cls.InlineModel(e.ModelIndex)
		switch {
		case e.Number == in.OwnEntity():
		case n != 0 && e.RenderFX&q2const.RF_BEAM == 0:
			if !adm[e.Number] && p.movableBrush(v, n, e, ownOrigin) {
				kind[i] = brush
			}
		case seen[e.Number]:
			if e.RenderFX&q2const.RF_BEAM == 0 && e.OldOrigin != (Vec3{}) && !v.SeesPoint(e.OldOrigin) {
				kind[i] = oldOrigin
			}
		case adm[e.Number]:
			kind[i] = heardOnly
		default:
			kind[i] = hidden
		}
	}
	// rewrite i once; ok false if no valid value was found (it is put back)
	rewrite := func(i int) bool {
		e, orig := &out.Entities[i], in.Entities[i]
		switch kind[i] {
		case hidden:
			p.randomize(e, orig)
		case heardOnly:
			p.randomizeHeard(e, orig)
		case brush:
			for try := 0; try < 10; try++ {
				*e = orig
				e.Origin = shared.VectorAdd(orig.Origin, Vec3{p.offset(200), p.offset(200), p.offset(100)})
				e.Angles = Vec3{orig.Angles[0], float32(p.rng.Intn(360)), orig.Angles[2]}
				if p.movableBrush(v, cls.InlineModel(e.ModelIndex), e, ownOrigin) {
					return true
				}
			}
			*e = orig
			return false
		case oldOrigin:
			for try := 0; try < 10; try++ {
				*e = orig
				e.OldOrigin = shared.VectorAdd(orig.Origin, Vec3{p.offset(150), p.offset(150), p.offset(60)})
				if !v.SeesPoint(e.OldOrigin) {
					return true
				}
			}
			*e = orig
			return false
		}
		return true
	}
	for i := range out.Entities {
		if kind[i] != keep {
			rewrite(i)
		}
	}
	for attempt := 0; ; attempt++ {
		got := signature(p.checker.Perceive(&out))
		if reflect.DeepEqual(got, sig) {
			break
		}
		p.retries++
		// put back or resample the rewrites that changed what is perceived
		bad := map[int32]bool{}
		for _, n := range got[1] {
			if !adm[n] {
				bad[n] = true
			}
		}
		for _, n := range got[0] {
			if !seen[n] {
				bad[n] = true
			}
		}
		for i := range out.Entities {
			if kind[i] == keep {
				continue
			}
			if attempt >= 20 || bad[out.Entities[i].Number] || len(bad) == 0 && kind[i] == brush {
				if attempt < 20 {
					rewrite(i)
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
	for i := range out.Entities {
		if out.Entities[i] == in.Entities[i] {
			continue
		}
		n++
		switch kind[i] {
		case hidden:
			if cls.Classify(&in.Entities[i]).Class.Kind == perception.KindMonster {
				p.hiddenMonsters++
			}
		case brush:
			p.brushes++
		case heardOnly:
			p.heardOnly++
		case oldOrigin:
			p.oldOrigins++
		}
	}

	// drop hidden entities from the packet: a sound of one would lose its
	// position, so only those no event names
	named := map[int32]bool{}
	for _, e := range in.Events.Sounds {
		named[e.Ent] = true
	}
	for _, e := range in.Events.MuzzleFlashes {
		named[e.Ent] = true
	}
	kept := make([]shared.EntityState, 0, len(out.Entities))
	for i := range out.Entities {
		if kind[i] == hidden && !named[out.Entities[i].Number] && p.rng.Intn(2) == 0 {
			p.removed++
			n++
			continue
		}
		kept = append(kept, out.Entities[i])
	}
	full := out.Entities
	out.Entities = kept
	if !reflect.DeepEqual(signature(p.checker.Perceive(&out)), sig) {
		p.retries++
		out.Entities = full
	}
	p.changed += n
	if n > 0 {
		p.frames++
	}
	return out
}

// earshot is beyond which the mixer plays a one-shot sound of attenuation
// atten at zero volume (S_SpatializeOrigin: SOUND_FULLVOLUME 80 plus
// 1/dist_mult, with a margin).
func earshot(atten float32) float32 {
	switch atten {
	case q2const.ATTN_NORM:
		return 80 + 1/0.0005 + 100
	case q2const.ATTN_IDLE:
		return 80 + 1/0.001 + 100
	case q2const.ATTN_STATIC:
		return 80 + 1/0.003 + 100
	}
	return float32(math.Inf(1))
}

// inject adds sounds and muzzle flashes without a position for entities
// not in the frame whose last perceived position (the tracks with that
// number in b) is out of earshot, and an explosion behind the player out of
// earshot: none of it may change the belief.
func (p *perturber) inject(out *perception.FrameInput, b *Belief, deathSound int32) {
	if deathSound == 0 {
		return
	}
	inFrame := map[int32]bool{}
	for i := range out.Entities {
		inFrame[out.Entities[i].Number] = true
	}
	eye := perception.Eye(&out.PlayerState)
	// the farthest attenuation each candidate is out of earshot for
	cand := map[int32]float32{}
	tracked := map[int32]bool{}
	for n := int32(2); n < q2const.MAX_EDICTS; n++ {
		if !inFrame[n] {
			cand[n] = q2const.ATTN_NORM
		}
	}
	for _, tr := range b.Tracks {
		a, ok := cand[tr.Num]
		if !ok {
			continue
		}
		tracked[tr.Num] = true
		d := dist(tr.Pos, eye)
		switch {
		case !tr.PosKnown:
		case d > earshot(q2const.ATTN_NORM):
		case d > earshot(q2const.ATTN_IDLE) && a != q2const.ATTN_STATIC:
			cand[tr.Num] = q2const.ATTN_IDLE
		case d > earshot(q2const.ATTN_STATIC):
			cand[tr.Num] = q2const.ATTN_STATIC
		default:
			delete(cand, tr.Num)
		}
	}
	nums := make([]int32, 0, len(cand))
	for n := range cand {
		nums = append(nums, n)
	}
	sort.Slice(nums, func(i, j int) bool { return nums[i] < nums[j] })
	// the tracked candidates first, then a few others
	sort.SliceStable(nums, func(i, j int) bool { return tracked[nums[i]] && !tracked[nums[j]] })
	ev := out.Events
	ev.Sounds = append([]fakeclient.Sound(nil), ev.Sounds...)
	ev.MuzzleFlashes = append([]fakeclient.MuzzleFlash(nil), ev.MuzzleFlashes...)
	ev.TempEnts = append([]fakeclient.TempEnt(nil), ev.TempEnts...)
	for k, n := range nums {
		if k >= 4 {
			break
		}
		ev.Sounds = append(ev.Sounds, fakeclient.Sound{SoundNum: deathSound, Ent: n, Channel: 2, Volume: 1, Attenuation: cand[n]})
		if cand[n] == q2const.ATTN_NORM {
			ev.MuzzleFlashes = append(ev.MuzzleFlashes, fakeclient.MuzzleFlash{Ent: n, Monster: true,
				Weapon: q2const.MZ2_SOLDIER_SHOTGUN_1})
		}
		p.injected++
		if tracked[n] {
			p.injectedTracked++
		}
	}
	var fwd Vec3
	shared.AngleVectors(out.PlayerState.ViewAngles, &fwd, nil, nil)
	ev.TempEnts = append(ev.TempEnts, fakeclient.TempEnt{Type: q2const.TE_EXPLOSION1, Pos: shared.VectorMA(eye, -3000, fwd)})
	out.Events = ev
}

// deathSoundIndex returns the CS_SOUNDS index of a monster death cry of
// the level (0 if none is precached).
func deathSoundIndex(cs *perception.ConfigStrings) int32 {
	for i := 1; i < q2const.MAX_SOUNDS; i++ {
		if k, fam := perception.ClassifySound(cs[q2const.CS_SOUNDS+i]); k == perception.SoundDeath && fam != "" {
			return int32(i)
		}
	}
	return 0
}

// TestPerturbationInvariance is the fairness test of the ObservationFilter:
// the bot's payload stream from a lockstep run is replayed through a
// Passive client twice, once verbatim and once with everything the filter
// did not admit at that frame rewritten (see perturber.perturb), plus
// sounds and flashes without a position from entities out of earshot and a
// far explosion behind the player; the world model's beliefs must be
// identical at every frame.
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
	for i, in := range inputs {
		pert := p.perturb(in)
		p.inject(&pert, wa.Belief(), deathSoundIndex(in.CS))
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
	t.Logf("%d frames, %d entity states rewritten in %d frames (%d of hidden monsters, %d heard-only, %d brushes, "+
		"%d unseen old_origins, %d dropped from the packet), %d resamples; %d sounds injected (%d for tracks out of earshot)",
		len(inputs), p.changed, p.frames, p.hiddenMonsters, p.heardOnly, p.brushes, p.oldOrigins, p.removed, p.retries,
		p.injected, p.injectedTracked)
	if p.frames < len(inputs)/2 || p.hiddenMonsters == 0 || p.brushes == 0 || p.heardOnly == 0 || p.removed == 0 ||
		p.injectedTracked == 0 {
		t.Fatalf("the perturbation did not touch enough: %d frames, %d hidden monster states, %d brushes, "+
			"%d heard-only, %d dropped, %d sounds for tracks", p.frames, p.hiddenMonsters, p.brushes, p.heardOnly,
			p.removed, p.injectedTracked)
	}
	if len(wa.Belief().Tracks) == 0 {
		t.Fatal("no tracks: the run perceived nothing")
	}
	if !controlDiffers {
		t.Fatal("moving the seen monsters did not change the belief: the comparison is blind")
	}
}

// TestInjectedSoundsAreNotIgnoredBlindly is the control of the event
// perturbation: the same death cry without a position from a track within
// earshot does change the belief.
func TestInjectedSoundsAreNotIgnoredBlindly(t *testing.T) {
	s := newSim(t)
	s.ents = []shared.EntityState{soldierAt(20, Vec3{300, 0, 24})}
	s.step()
	s.ents = nil
	s.ps.ViewAngles[q2const.YAW] = 180 // its spot out of view: not known to be empty
	s.step()
	before := s.w.Belief().Digest()
	s.ev.Sounds = []fakeclient.Sound{{SoundNum: deathSoundIndex(s.cs), Ent: 20, Volume: 1, Attenuation: q2const.ATTN_STATIC}}
	if s.step(); s.w.Belief().Digest() == before || s.track("e1").Life != LifeDying {
		t.Fatalf("a cry in earshot was ignored: %+v", s.track("e1"))
	}
}
