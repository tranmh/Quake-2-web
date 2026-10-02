package worldmodel

import (
	"encoding/json"
	"fmt"
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
	// leak is called when a rewrite keeps everything the percept may say
	// about the entities it touched (what is seen and admitted, every cue
	// of every emitter) and the percept still differs: the percept carries
	// something about an entity beyond what a player perceives of it.
	leak func(msg string)
	// mutate (tests of the test only) changes each percept the perturber
	// judges, after the filter made it: a leak planted on purpose.
	mutate  func(pc *perception.Percept, in *perception.FrameInput)
	changed int // entity states rewritten
	frames  int // frames with at least one rewrite
	retries int
	// by kind of rewrite
	hiddenMonsters, brushes, heardOnly, oldOrigins, removed, injected, injectedTracked int
	// heard-only emitters whose origin moved, those mirrored front to back
	// (the stereo cue is the same), and the monsters among the moved
	heardMoved, heardMirrored, heardMonsters int
	// heard-only states by channel (one-shot sounds, muzzle flashes, loop
	// sounds; an emitter may be heard on several) and how many moved, and
	// the monsters among them
	chanStates, chanMoved [nChannels]int
	monsterStates         int
}

// perceive is the percept the perturber judges a frame input by.
func (p *perturber) perceive(in *perception.FrameInput) *perception.Percept {
	pc := p.checker.Perceive(in)
	if p.mutate != nil {
		p.mutate(pc, in)
	}
	return pc
}

// percSig is what must not change: the entities seen and those admitted,
// and everything heard (the sounds' and flashes' cues), with the cues of
// each emitter by number to tell which rewrite changed one.
type percSig struct {
	seen, admitted []int32
	heard          []perception.Hearing
	flashes        []perception.Flash
	temp           []perception.TempEvent
	cues           map[int32]string
}

func signature(pc *perception.Percept) percSig {
	var seen []int32
	for i := range pc.Seen {
		seen = append(seen, pc.Seen[i].Num)
	}
	sig := percSig{seen: seen, admitted: pc.Admitted(), heard: pc.Heard, flashes: pc.Flashes, temp: pc.TempEnts,
		cues: map[int32]string{}}
	for i := range pc.Heard {
		h := &pc.Heard[i]
		sig.cues[cueKey(h)] += fmt.Sprintf("s%d:%v:%v:%v;", h.Index, h.Cue, h.Placed, h.Seen)
	}
	for i := range pc.Flashes {
		f := &pc.Flashes[i]
		sig.cues[f.Num] += fmt.Sprintf("f%d:%v:%v:%v;", f.Raw, f.Cue, f.Placed, f.Seen)
	}
	return sig
}

// cueKey is the key of a sound's cues in percSig.cues: its entity, or for
// a blended loop (no entity) minus its sound index.
func cueKey(h *perception.Hearing) int32 {
	if h.Num == 0 && h.Loop {
		return -h.Index
	}
	return h.Num
}

func (a percSig) equal(b percSig) bool {
	return reflect.DeepEqual(a.seen, b.seen) && reflect.DeepEqual(a.admitted, b.admitted) &&
		reflect.DeepEqual(a.heard, b.heard) && reflect.DeepEqual(a.flashes, b.flashes) && reflect.DeepEqual(a.temp, b.temp)
}

// sameCues reports whether a and b agree on all a rewrite may change only
// together with what the player perceives: the entities seen and admitted
// and the cues of every emitter. If they do and a and b still differ, the
// percept says more about some entity than that.
func (a percSig) sameCues(b percSig) bool {
	return reflect.DeepEqual(a.seen, b.seen) && reflect.DeepEqual(a.admitted, b.admitted) && reflect.DeepEqual(a.cues, b.cues)
}

// channels a heard-only emitter is heard on
const (
	chSound = iota // a one-shot sound (svc_sound) placed at it
	chFlash        // a muzzle flash
	chLoop         // a loop sound it is one of the makers of
	nChannels
)

// cued returns the entities of frame in that a sound or flash placed this
// frame names (heard at their origin or with them in the packet: only a
// cue admitted), and the audible makers of the loop sounds heard (the
// blend of all the frame's entities with that sound; one out of earshot
// adds nothing to it and is hidden), with the channels each is heard on.
func cued(pc *perception.Percept, in *perception.FrameInput) map[int32][nChannels]bool {
	out := map[int32][nChannels]bool{}
	set := func(n int32, ch int) {
		c := out[n]
		c[ch] = true
		out[n] = c
	}
	loops := map[int32]bool{}
	for i := range pc.Heard {
		switch h := &pc.Heard[i]; {
		case h.Num == 0 && h.Loop:
			loops[h.Index] = true
		case h.Placed && h.Num > 0:
			set(h.Num, chSound)
		}
	}
	for i := range pc.Flashes {
		if f := &pc.Flashes[i]; f.Placed {
			set(f.Num, chFlash)
		}
	}
	own := in.OwnEntity()
	for i := range in.Entities {
		// a loop plays like a one-shot ATTN_STATIC sound at full volume
		// (SOUND_LOOPATTENUATE is ATTN_STATIC's distance multiplier)
		if e := &in.Entities[i]; e.Sound != 0 && e.Number != own && loops[e.Sound] && pc.Audible(e.Origin, 1, q2const.ATTN_STATIC) {
			set(e.Number, chLoop)
		}
	}
	return out
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

// randomizeHeard rewrites an entity that is only heard (a sound or flash
// placed at its origin, not in view): everything but what makes the sound,
// its origin included. The origin moves anywhere (the caller keeps a draw
// only when every cue of the frame stays the same: the same stereo balance
// and loudness step), so its velocity is arbitrary too; a third of the
// draws mirror it front to back about the listener (the stereo mixer
// renders both alike). It reports whether the draw is a mirror image.
func (p *perturber) randomizeHeard(e *shared.EntityState, orig shared.EntityState, v *perception.Vision) bool {
	*e = orig
	e.Angles = Vec3{float32(p.rng.Intn(360)), float32(p.rng.Intn(360)), float32(p.rng.Intn(360))}
	e.Frame = int32(p.rng.Intn(200))
	e.SkinNum = orig.SkinNum ^ 1
	e.Solid = int32(p.rng.Intn(1 << 15))
	mirror := p.rng.Intn(3) == 0
	if mirror {
		fwd, _, _ := v.Axes()
		d := shared.VectorSubtract(orig.Origin, v.Eye())
		e.Origin = shared.VectorMA(orig.Origin, -2*shared.DotProduct(d, fwd), fwd)
		e.Origin = shared.VectorAdd(e.Origin, Vec3{p.offset(40), p.offset(40), p.offset(16)})
	} else {
		e.Origin = shared.VectorAdd(orig.Origin, Vec3{p.offset(400), p.offset(400), p.offset(64)})
	}
	if e.RenderFX&q2const.RF_BEAM == 0 {
		e.OldOrigin = shared.VectorAdd(e.Origin, Vec3{p.offset(300), p.offset(300), p.offset(100)})
	}
	return mirror
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
//   - entities only heard (a sound or flash placed at their origin, not in
//     view): everything but what makes the sound, the origin included,
//     anywhere it keeps the same cues (stereo balance and loudness step),
//     often mirrored front to back;
//   - seen entities whose old_origin is out of view: old_origin;
//   - brush entities wholly outside the view frustum: their pose (inside
//     it an occluding brush is by definition visible where it occludes);
//   - packet membership: half the hidden entities that no event of the
//     frame names are dropped from the packet (the PVS is hidden state).
//
// New values are resampled until the filter still sees and admits exactly
// the same entities and hears exactly the same (a hidden entity moved into
// view would be seen, a heard one moved off its cue heard differently,
// which is not a leak), and put back if that fails.
func (p *perturber) perturb(in perception.FrameInput) perception.FrameInput {
	pc := p.perceive(&in)
	sig := signature(pc)
	heard := cued(pc, &in)
	v := pc.Vision()
	adm, seen := map[int32]bool{}, map[int32]bool{}
	for _, n := range sig.admitted {
		adm[n] = true
	}
	for _, n := range sig.seen {
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
			kind[i] = keep // a mover a sound gave the pose of
		case heard[e.Number] != [nChannels]bool{}:
			kind[i] = heardOnly
		default:
			kind[i] = hidden
		}
	}
	// rewrite i once; ok false if no valid value was found (it is put back)
	mirrored := make([]bool, len(out.Entities))
	rewrite := func(i int) bool {
		e, orig := &out.Entities[i], in.Entities[i]
		switch kind[i] {
		case hidden:
			p.randomize(e, orig)
		case heardOnly:
			mirrored[i] = p.randomizeHeard(e, orig, v)
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
		got := signature(p.perceive(&out))
		if got.equal(sig) {
			break
		}
		if got.sameCues(sig) {
			p.leak(fmt.Sprintf("frame %d: rewriting what was not admitted kept what is seen and admitted and every cue, "+
				"yet the percept changed: it says more about an entity than a player perceives\nverbatim:  %+v %+v %+v\nperturbed: %+v %+v %+v",
				in.ServerFrame, sig.heard, sig.flashes, sig.temp, got.heard, got.flashes, got.temp))
			out.Entities = append(out.Entities[:0:0], in.Entities...)
			break
		}
		p.retries++
		// put back or resample the rewrites that changed what is perceived
		// (a blended loop's cue, key minus its sound, names all its makers)
		bad := map[int32]bool{}
		badLoop := map[int32]bool{}
		mark := func(n int32) {
			if n < 0 {
				badLoop[-n] = true
			} else {
				bad[n] = true
			}
		}
		for _, n := range got.admitted {
			if !adm[n] {
				bad[n] = true
			}
		}
		for _, n := range got.seen {
			if !seen[n] {
				bad[n] = true
			}
		}
		for n, c := range got.cues {
			if sig.cues[n] != c {
				mark(n)
			}
		}
		for n, c := range sig.cues {
			if got.cues[n] != c {
				mark(n)
			}
		}
		for i := range out.Entities {
			if kind[i] == keep {
				continue
			}
			e := &out.Entities[i]
			if attempt >= 20 || bad[e.Number] || badLoop[e.Sound] && e.Sound != 0 || len(bad)+len(badLoop) == 0 && kind[i] == brush {
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
	for i := range out.Entities {
		if kind[i] != heardOnly {
			continue
		}
		moved := out.Entities[i].Origin != in.Entities[i].Origin
		for ch, on := range heard[in.Entities[i].Number] {
			if on {
				p.chanStates[ch]++
				if moved {
					p.chanMoved[ch]++
				}
			}
		}
		if c := cls.Classify(&in.Entities[i]).Class; c != nil && c.Kind == perception.KindMonster {
			p.monsterStates++
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
			if out.Entities[i].Origin != in.Entities[i].Origin {
				p.heardMoved++
				if mirrored[i] {
					p.heardMirrored++
				}
				if c := cls.Classify(&in.Entities[i]).Class; c != nil && c.Kind == perception.KindMonster {
					p.heardMonsters++
				}
			}
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
	if got := signature(p.perceive(&out)); !got.equal(sig) {
		// the dropped entities are neither seen, admitted nor heard: no
		// legitimate part of the percept depends on them
		p.leak(fmt.Sprintf("frame %d: dropping hidden entities no event names from the packet changed the percept\nverbatim:  %+v\nperturbed: %+v",
			in.ServerFrame, sig, got))
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
// not in the frame whose believed location (Loc of the tracks with that
// number in b: where the world model gates such a sound) is out of earshot, and an explosion behind the player out of
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
		d := dist(tr.Loc, eye)
		switch {
		case !tr.LocKnown:
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

// PerturbationRun is the fairness test of the ObservationFilter: the bot's
// payload stream from a lockstep run is replayed through a Passive client
// twice, once verbatim and once with everything the filter did not admit
// at that frame rewritten (see perturber.perturb: hidden entities, the
// origins of the entities only heard within what keeps their cues, and
// more), plus sounds and flashes without a position from entities out of
// earshot and a far explosion behind the player; the world model's
// beliefs must be identical at every frame. each (optional) is called
// after every frame with the verbatim and the perturbed world: the
// decisions made from them must be identical too
// (TestPerturbationInvariance in package worldmodel_test checks the lane
// states and the scripted policy's answers).
func PerturbationRun(t *testing.T, each func(frame int, verbatim, perturbed *World)) {
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
	wa, wb, wc, wd := New(cfg), New(cfg), New(cfg), New(cfg)
	wa.Reset(lv)
	wb.Reset(lv)
	wc.Reset(lv) // control: admitted monsters moved, which must show
	wd.Reset(lv) // control: heard-only emitters moved to the other ear, which must show
	controlDiffers, earControlDiffers := false, false
	earFrames := 0 // track-frames placed by ear alone
	table := perception.NewClassTable()
	p := &perturber{rng: rand.New(rand.NewSource(7)), leak: func(msg string) { t.Fatal(msg) },
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
		// the other ear: a heard-only emitter mirrored left to right about
		// the listener changes its stereo balance
		ear := in
		ear.Entities = append([]shared.EntityState(nil), in.Entities...)
		heardNow := cued(wa.Percept(), &in)
		var right Vec3
		shared.AngleVectors(in.PlayerState.ViewAngles, nil, &right, nil)
		eye := perception.Eye(&in.PlayerState)
		for k := range ear.Entities {
			if e := &ear.Entities[k]; heardNow[e.Number] != [nChannels]bool{} && wa.Percept().Sighting(e.Number) == nil {
				d := shared.VectorSubtract(e.Origin, eye)
				e.Origin = shared.VectorMA(e.Origin, -2*shared.DotProduct(d, right), right)
			}
		}
		wd.Update(ear, recs[i].now)
		earControlDiffers = earControlDiffers || wd.Belief().Digest() != wa.Belief().Digest()
		da, db := wa.Belief().Digest(), wb.Belief().Digest()
		if da != db {
			ja, _ := json.Marshal(wa.Belief())
			jb, _ := json.Marshal(wb.Belief())
			t.Fatalf("frame %d (t=%d): beliefs differ\nverbatim:  %s\nperturbed: %s", i, recs[i].now, ja, jb)
		}
		for _, tr := range wa.Belief().Tracks {
			if tr.LocKnown && !tr.LocSeen {
				earFrames++
			}
		}
		if each != nil {
			each(i, wa, wb)
		}
	}
	t.Logf("%d frames, %d entity states rewritten in %d frames (%d of hidden monsters, %d heard-only of which %d moved "+
		"(%d monsters) and %d mirrored front to back, %d brushes, %d unseen old_origins, %d dropped from the packet), %d resamples; "+
		"%d sounds injected (%d for tracks out of earshot); %d track-frames placed by ear",
		len(inputs), p.changed, p.frames, p.hiddenMonsters, p.heardOnly, p.heardMoved, p.heardMonsters, p.heardMirrored,
		p.brushes,
		p.oldOrigins, p.removed, p.retries, p.injected, p.injectedTracked, earFrames)
	t.Logf("heard-only states moved by channel: sounds %d/%d, flashes %d/%d, loops %d/%d; monsters %d/%d",
		p.chanMoved[chSound], p.chanStates[chSound], p.chanMoved[chFlash], p.chanStates[chFlash],
		p.chanMoved[chLoop], p.chanStates[chLoop], p.heardMonsters, p.monsterStates)
	// nearly every heard-only state must move, on each channel heard in
	// the run (one-shot sounds and loops always are), and so must nearly
	// every heard-only monster's
	short := p.chanStates[chSound] == 0 || p.chanStates[chLoop] == 0 || p.monsterStates == 0 ||
		p.heardMonsters*10 < p.monsterStates*9
	for ch := range p.chanStates {
		short = short || p.chanMoved[ch]*10 < p.chanStates[ch]*9
	}
	if short || p.frames < len(inputs)/2 || p.hiddenMonsters == 0 || p.brushes == 0 || p.heardOnly == 0 || p.removed == 0 ||
		p.injectedTracked == 0 || p.heardMirrored == 0 || earFrames == 0 {
		t.Fatalf("the perturbation did not touch enough: %d frames, %d hidden monster states, %d brushes, "+
			"%d heard-only (%d moved, %d mirrored; by channel %v of %v; monsters %d of %d), %d dropped, %d sounds for tracks, "+
			"%d track-frames by ear", p.frames, p.hiddenMonsters, p.brushes, p.heardOnly, p.heardMoved, p.heardMirrored,
			p.chanMoved, p.chanStates, p.heardMonsters, p.monsterStates, p.removed, p.injectedTracked, earFrames)
	}
	if len(wa.Belief().Tracks) == 0 {
		t.Fatal("no tracks: the run perceived nothing")
	}
	if !controlDiffers {
		t.Fatal("moving the seen monsters did not change the belief: the comparison is blind")
	}
	if !earControlDiffers {
		t.Fatal("moving the heard monsters to the other ear did not change the belief: hearing is ignored")
	}
}

// TestPerturberCatchesPerceptLeaks is the negative check of the strict
// rule of perturber.perturb: a percept that carries anything about a
// heard-only entity beyond its cue (here planted on purpose after the
// filter: the exact origin of the emitter of a sound or a muzzle flash,
// or how many entities make a blended loop) is reported as a leak,
// although every cue stays the same and so no belief could differ.
func TestPerturberCatchesPerceptLeaks(t *testing.T) {
	origin := func(in *perception.FrameInput, num int32) float32 {
		for i := range in.Entities {
			if in.Entities[i].Number == num {
				return in.Entities[i].Origin[0]
			}
		}
		return 0
	}
	for _, tc := range []struct {
		name   string
		mutate func(pc *perception.Percept, in *perception.FrameInput)
	}{
		{"sound origin", func(pc *perception.Percept, in *perception.FrameInput) {
			for i := range pc.Heard {
				if h := &pc.Heard[i]; h.Num > 0 && !h.Loop && !h.Seen {
					h.Volume = origin(in, h.Num)
				}
			}
		}},
		{"flash origin", func(pc *perception.Percept, in *perception.FrameInput) {
			for i := range pc.Flashes {
				if f := &pc.Flashes[i]; !f.Seen {
					f.Volume = origin(in, f.Num)
				}
			}
		}},
		{"loop makers", func(pc *perception.Percept, in *perception.FrameInput) {
			for i := range pc.Heard {
				if h := &pc.Heard[i]; h.Loop && h.Num == 0 {
					for k := range in.Entities {
						if in.Entities[k].Sound == h.Index {
							h.Channel++
						}
					}
				}
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newSim(t)
			s.cs[q2const.CS_SOUNDS+sLoop] = "world/amb10.wav"
			leaks := 0
			p := &perturber{rng: rand.New(rand.NewSource(3)), mutate: tc.mutate, leak: func(string) { leaks++ },
				checker: perception.NewPerceiver(floorCM(t), perception.NewClassifier(perception.NewClassTable()), nil, perception.Options{})}
			for frame := 0; frame < 20; frame++ {
				s.ps.ViewAngles[q2const.YAW] = float32(frame * 20)
				s.ents = []shared.EntityState{soldierAt(30, Vec3{-300, 200, 24}), soldierAt(31, Vec3{100, -500, 24}),
					{Number: 40, Sound: sLoop, Origin: Vec3{0, 250, 40}},  // a speaker in earshot
					{Number: 41, Sound: sLoop, Origin: Vec3{0, 4000, 40}}} // and one far out of it
				s.ev = perception.Events{Sounds: []fakeclient.Sound{{SoundNum: sSight, Ent: 30, Volume: 1, Attenuation: 1}},
					MuzzleFlashes: []fakeclient.MuzzleFlash{{Ent: 31, Monster: true, Weapon: q2const.MZ2_SOLDIER_MACHINEGUN_1}}}
				p.perturb(s.input())
				s.step()
			}
			if leaks == 0 {
				t.Fatal("a leak planted in the percept was not reported")
			}
		})
	}
	// and the filter itself leaks nothing on the same frames
	s := newSim(t)
	s.cs[q2const.CS_SOUNDS+sLoop] = "world/amb10.wav"
	p := &perturber{rng: rand.New(rand.NewSource(3)), leak: func(msg string) { t.Fatal(msg) },
		checker: perception.NewPerceiver(floorCM(t), perception.NewClassifier(perception.NewClassTable()), nil, perception.Options{})}
	for frame := 0; frame < 20; frame++ {
		s.ps.ViewAngles[q2const.YAW] = float32(frame * 20)
		s.ents = []shared.EntityState{soldierAt(30, Vec3{-300, 200, 24}), soldierAt(31, Vec3{100, -500, 24}),
			{Number: 40, Sound: sLoop, Origin: Vec3{0, 250, 40}}, {Number: 41, Sound: sLoop, Origin: Vec3{0, 4000, 40}}}
		s.ev = perception.Events{Sounds: []fakeclient.Sound{{SoundNum: sSight, Ent: 30, Volume: 1, Attenuation: 1}},
			MuzzleFlashes: []fakeclient.MuzzleFlash{{Ent: 31, Monster: true, Weapon: q2const.MZ2_SOLDIER_MACHINEGUN_1}}}
		p.perturb(s.input())
		s.step()
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

// TestHeardOnlyOriginsInvariance is the hearing half of the perturbation
// test on a synthetic fight with many heard monsters: four soldiers move
// at random around a turning player, out of view as often as in it, crying
// out and firing at random; two of them and a speaker also make the same
// loop sound. A second world gets the same frames with the origin (and so
// the velocity) of every entity only heard redrawn at random wherever
// every cue of the frame stays the same (often mirrored front to back),
// and its angles, frame and box rewritten too: its belief must equal the
// first world's at every frame. A draw that keeps every cue and what is
// seen and admitted must keep the whole percept: anything else in it would
// tell the bot more about a heard entity than its cue. Nearly every heard
// state must move, on each channel (sounds, flashes, loops). A control
// world that moves the heard soldiers to the other ear must differ.
func TestHeardOnlyOriginsInvariance(t *testing.T) {
	a, b, c := newSim(t), newSim(t), newSim(t)
	for _, s := range []*sim{a, b, c} {
		s.cs[q2const.CS_SOUNDS+sLoop] = "world/amb10.wav"
	}
	checker := perception.NewPerceiver(floorCM(t), perception.NewClassifier(perception.NewClassTable()), nil, perception.Options{})
	rng := rand.New(rand.NewSource(11))
	p := &perturber{rng: rand.New(rand.NewSource(12)), checker: checker}
	type mon struct {
		num      int32
		pos, vel Vec3
	}
	mons := []mon{{num: 30}, {num: 31}, {num: 32}, {num: 33}}
	for i := range mons {
		mons[i].pos = Vec3{p.offset(900), p.offset(900), 24}
	}
	speaker := shared.EntityState{Number: 40, Sound: sLoop, Origin: Vec3{p.offset(600), p.offset(600), 40}}
	moved, mirrored, nHeard, earFrames, differs := 0, 0, 0, 0, false
	var chStates, chMoved [nChannels]int
	for frame := 0; frame < 400; frame++ {
		yaw := float32(frame) * 4 // a slow turn: turns tell front from back
		var ents []shared.EntityState
		var ev perception.Events
		for i := range mons {
			m := &mons[i]
			if frame%15 == 0 {
				m.vel = Vec3{float32(rng.Intn(401) - 200), float32(rng.Intn(401) - 200), 0}
			}
			m.pos = shared.VectorMA(m.pos, 0.1, m.vel)
			for k := 0; k < 2; k++ {
				m.pos[k] = max(-1200, min(1200, m.pos[k]))
			}
			e := soldierAt(m.num, m.pos)
			if m.num >= 32 {
				e.Sound = sLoop
			}
			ents = append(ents, e)
			switch r := rng.Intn(10); {
			case r < 2:
				ev.Sounds = append(ev.Sounds, fakeclient.Sound{SoundNum: sSight, Ent: m.num, Volume: 1, Attenuation: float32(1 + rng.Intn(2))})
			case r < 4:
				ev.MuzzleFlashes = append(ev.MuzzleFlashes, fakeclient.MuzzleFlash{Ent: m.num, Monster: true, Weapon: q2const.MZ2_SOLDIER_MACHINEGUN_1})
			}
		}
		ents = append(ents, speaker)
		for _, s := range []*sim{a, b, c} {
			s.ps.ViewAngles[q2const.YAW] = yaw
			s.ents, s.ev = append([]shared.EntityState(nil), ents...), ev
		}
		in := a.input()
		pc := checker.Perceive(&in)
		sig := signature(pc)
		heard := cued(pc, &in)
		v := pc.Vision()
		var right Vec3
		shared.AngleVectors(a.ps.ViewAngles, nil, &right, nil)
		for i := range b.ents {
			e := &b.ents[i]
			chans := heard[e.Number]
			if chans == [nChannels]bool{} || pc.Sighting(e.Number) != nil {
				continue
			}
			nHeard++
			orig := *e
			ok := false
			for try := 0; try < 60 && !ok; try++ {
				mir := p.randomizeHeard(e, orig, v)
				if orig.ModelIndex != 0 {
					e.Solid = solidStd // a box the class allows, so a draw that comes into view is told apart
				}
				pin := b.input()
				got := signature(checker.Perceive(&pin))
				ok = got.equal(sig)
				if !ok && got.sameCues(sig) {
					t.Fatalf("frame %d: moving heard-only entity %d kept every cue and what is seen, yet the percept changed: "+
						"it says more about it than its cue\nverbatim:  %+v %+v\nperturbed: %+v %+v", frame, e.Number, sig.heard, sig.flashes, got.heard, got.flashes)
				}
				if ok && e.Origin != orig.Origin {
					moved++
					if mir {
						mirrored++
					}
				}
			}
			if !ok {
				*e = orig
			}
			for ch, on := range chans {
				if on {
					chStates[ch]++
					if e.Origin != orig.Origin {
						chMoved[ch]++
					}
				}
			}
			// the control: the other ear
			ce := &c.ents[i]
			d := shared.VectorSubtract(ce.Origin, v.Eye())
			ce.Origin = shared.VectorMA(ce.Origin, -2*shared.DotProduct(d, right), right)
		}
		if got := b.input(); !signature(checker.Perceive(&got)).equal(sig) {
			t.Fatalf("frame %d: the perturbation changed the percept", frame)
		}
		a.step()
		b.step()
		c.step()
		if da, db := a.w.Belief().Digest(), b.w.Belief().Digest(); da != db {
			ja, _ := json.Marshal(a.w.Belief().Tracks)
			jb, _ := json.Marshal(b.w.Belief().Tracks)
			t.Fatalf("frame %d: beliefs differ\nverbatim:  %s\nperturbed: %s", frame, ja, jb)
		}
		differs = differs || c.w.Belief().Digest() != a.w.Belief().Digest()
		for _, tr := range a.w.Belief().Tracks {
			if tr.LocKnown && !tr.LocSeen {
				earFrames++
			}
		}
	}
	t.Logf("%d heard-only states, %d moved (%d mirrored front to back); by channel: sounds %d/%d, flashes %d/%d, loops %d/%d; "+
		"%d track-frames placed by ear", nHeard, moved, mirrored, chMoved[chSound], chStates[chSound], chMoved[chFlash],
		chStates[chFlash], chMoved[chLoop], chStates[chLoop], earFrames)
	short := moved*100 < nHeard*95 || mirrored == 0 || earFrames < 100
	for ch := range chStates {
		short = short || chStates[ch] < 50 || chMoved[ch]*100 < chStates[ch]*90
	}
	if short {
		t.Fatalf("the perturbation did not touch enough: %d heard-only, %d moved, %d mirrored, %d by ear; by channel %v of %v",
			nHeard, moved, mirrored, earFrames, chMoved, chStates)
	}
	if !differs {
		t.Fatal("moving the heard soldiers to the other ear did not change the belief: hearing is ignored")
	}
}
