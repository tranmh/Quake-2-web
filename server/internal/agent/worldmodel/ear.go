package worldmodel

import (
	"math"
	"math/bits"

	"quake2web/server/internal/agent/perception"
	"quake2web/server/internal/q2const"
)

// Hearing localization. A sound never gives a position (perception.Cue):
// only the stereo balance and the loudness the client's mixer renders.
// The world model keeps, per track, the world yaws the cues heard lately
// allow (a mask of earBins sectors) and narrows them with each new cue:
// Quake II's stereo has no front/back cue, but a turn of the bot between
// two sounds moves the arcs a cue allows, and their overlap tells front
// from back, as a player's turn of the head does. The stand-in position
// (Ear.Est) is the middle of the narrowed arc at the middle distance of
// the loudness step, from where the bot stood: a function of the cues
// and the bot's own state alone. The entity lump (static map knowledge)
// says where the monsters of each family spawn: of those spawns, the ones
// the sounds of a monster never seen agree with (their bearing in the arc,
// their loudness the one heard, not in plain view) pick the arc it is in,
// and when exactly one agrees the monster stands in at that spawn. Which
// lump entity a heard monster is never comes from its entity number: a
// player who has not seen it cannot tell which of a family's monsters it
// is.

const (
	// earBins sectors of 360/earBins degrees make up a yaw mask.
	earBins = 64
	earBin  = float32(360) / earBins
	// earMargin widens each arc a cue allows (degrees): the source's
	// height (which makes it sound more central), the listener's roll and
	// the source's and the bot's motion between two sounds.
	earMargin = 11.25
	// earMemory: a cue heard within this long (ms) of the last one narrows
	// the arcs that one left; an older one starts afresh.
	earMemory = 1500
)

// yawMask is a set of world yaw sectors: bit k covers [k, k+1)·earBin.
type yawMask uint64

// Ear is where hearing places a track out of view: what a player's ears
// tell (the cue of the last sound with a direction) and what the bot's own
// turns between sounds resolved. Never a position.
type Ear struct {
	At    int64  // when last heard with a direction (0: never)
	Sound string // what that sound was: attack, sight, pain, idle, step, death, ...
	Pan   perception.Pan
	Loud  perception.Loudness
	// Yaw is the world yaw (degrees) from the bot towards the source that
	// the sounds heard within earMemory agree on, give or take Spread.
	// Ambiguous: two arcs still fit (ahead and behind: the bot did not
	// turn between the sounds); Yaw is the one picked (see pickRun).
	Yaw       float32
	Spread    float32
	Ambiguous bool
	// Dist is the distance in the middle of the sound's loudness step
	// (perception.LoudnessRange) and Est the stand-in position: Yaw at
	// Dist from where the bot stood when it heard the sound, at the height
	// of its own origin.
	Dist float32
	Est  Vec3
	// Agrees: the last position seen (Track.Pos) fits the last sound: its
	// bearing is in the arc and it would sound as loud.
	Agrees bool
	// AtSpawn: the track was never seen and exactly one spawn origin of
	// its family on the level (the entity lump: static map knowledge)
	// fits the sounds (familySpawns, spawnFits): Est is that origin.
	AtSpawn bool
}

// arcMask returns the sectors whose centers lie within [lo, hi] (degrees,
// any range, wrapping).
func arcMask(lo, hi float32) yawMask {
	if hi-lo >= 360 {
		return ^yawMask(0)
	}
	var m yawMask
	for k := 0; k < earBins; k++ {
		c := (float32(k) + 0.5) * earBin
		d := float32(math.Mod(float64(c-lo), 360))
		if d < 0 {
			d += 360
		}
		if d <= hi-lo {
			m |= 1 << k
		}
	}
	return m
}

// cueMask returns the world yaws a source can have that sounds with
// balance pan to a listener with view yaw viewYaw (widened by earMargin).
func cueMask(pan perception.Pan, viewYaw float32) yawMask {
	var m yawMask
	for _, a := range pan.Arcs() {
		m |= arcMask(viewYaw+a[0]-earMargin, viewYaw+a[1]+earMargin)
	}
	return m
}

// sectorOf returns the sector of a world yaw.
func sectorOf(yaw float32) int {
	y := math.Mod(float64(yaw), 360)
	if y < 0 {
		y += 360
	}
	return int(float32(y)/earBin) % earBins
}

// yawRun is a circular run of set sectors: start and length.
type yawRun struct{ start, n int }

func (r yawRun) center() float32 {
	return float32(math.Mod(float64((float32(r.start)+float32(r.n)/2)*earBin), 360))
}

func (r yawRun) has(k int) bool {
	return (k-r.start+earBins)%earBins < r.n
}

// runs returns the circular runs of m, in the order of their first sector
// from a gap.
func (m yawMask) runs() []yawRun {
	if m == 0 {
		return nil
	}
	if m == ^yawMask(0) {
		return []yawRun{{0, earBins}}
	}
	// start the scan after a clear sector so no run wraps the scan
	first := bits.TrailingZeros64(uint64(^m))
	var out []yawRun
	cur := yawRun{-1, 0}
	for i := 1; i <= earBins; i++ {
		k := (first + i) % earBins
		if m&(1<<k) != 0 {
			if cur.n == 0 {
				cur.start = k
			}
			cur.n++
			continue
		}
		if cur.n > 0 {
			out = append(out, cur)
			cur = yawRun{-1, 0}
		}
	}
	return out
}

// yawTo is the world yaw from a to b (degrees).
func yawTo(a, b Vec3) float32 {
	return float32(math.Atan2(float64(b[1]-a[1]), float64(b[0]-a[0])) * 180 / math.Pi)
}

// pickRun picks the run the stand-in goes in when several fit: the one
// that holds the bearing of the last position seen (seen is false without
// one), else the one that holds the most of the bearings of the spawns
// that fit the sounds (spawnYaws), else the one farthest from the view (a
// source the bot does not see is likelier out of view than hidden in it),
// else the first.
func pickRun(runs []yawRun, seen bool, seenYaw float32, spawnYaws []float32, viewYaw float32) yawRun {
	if seen {
		k := sectorOf(seenYaw)
		for _, r := range runs {
			if r.has(k) {
				return r
			}
		}
	}
	count := func(r yawRun) int {
		n := 0
		for _, y := range spawnYaws {
			if r.has(sectorOf(y)) {
				n++
			}
		}
		return n
	}
	best, bn, bd := runs[0], -1, float32(-1)
	for _, r := range runs {
		n, d := count(r), absf(angleDiff(r.center(), viewYaw))
		if n > bn || n == bn && d > bd+0.01 {
			best, bn, bd = r, n, d
		}
	}
	return best
}

func absf(f float32) float32 {
	if f < 0 {
		return -f
	}
	return f
}

// hearCue folds a cue with a direction of a sound of track a that the bot
// heard out of view: kind is what it was, atten its attenuation (loop: an
// entity's looping sound). It never moves Pos nor touches Vel.
func (w *World) hearCue(a *actor, cue perception.Cue, kind perception.SoundKind, atten float32, loop bool, pc *perception.Percept) {
	if !cue.Directional() {
		return
	}
	s := &w.b.Self
	viewYaw := pc.PS.ViewAngles[q2const.YAW]
	m := cueMask(cue.Pan, viewYaw)
	if a.earMask != 0 && a.Ear.At > 0 && w.now-a.Ear.At <= earMemory && a.LastSeen <= a.Ear.At && m&a.earMask != 0 {
		m &= a.earMask
	}
	a.earMask = m
	e := &a.Ear
	e.At, e.Sound, e.Pan, e.Loud = w.now, kind.String(), cue.Pan, cue.Loud
	_, mid, _, ok := perception.LoudnessRange(cue.Loud, atten, loop)
	if !ok {
		mid = 0
	}
	e.Dist = mid
	e.Agrees = a.PosKnown && m&(1<<sectorOf(yawTo(s.Eye, a.Pos))) != 0 && pc.CueAt(a.Pos, atten, loop).Loud == cue.Loud
	a.fits = a.fits[:0]
	if !a.PosKnown {
		a.fits = w.spawnFits(a, cue, atten, loop, pc)
	}
	w.earPlace(a, viewYaw)
	a.LastUpdate = w.now
}

// familySpawns returns the lump entities (static map knowledge) where the
// monsters of a voice family spawn, in lump order: the present, non-brush
// entities whose classname is one of the family's classes. It does not
// say which of them a heard monster is.
func (w *World) familySpawns(family string) []int {
	if family == "" || family == "player" || w.level.Map == nil {
		return nil
	}
	if l, ok := w.spawnsBy[family]; ok {
		return l
	}
	names := map[string]bool{}
	for _, c := range w.classes.Classes() {
		if c.Kind == perception.KindMonster && familyOf(c) == family {
			for _, cn := range c.Classnames {
				names[cn] = true
			}
		}
	}
	var out []int
	for i := range w.level.Map.Entities {
		if e := &w.level.Map.Entities[i]; e.Present() && !e.Brush() && names[e.Classname] {
			out = append(out, i)
		}
	}
	w.spawnsBy[family] = out
	return out
}

// spawnFits returns the spawns of the family of track a (never seen) that
// a sound with cue cue (attenuation atten, loop: a looping sound) could
// come from: the bearing from the eye in the arc the sounds allow
// (a.earMask), the loudness step the one heard, and not in plain view (a
// monster there would be seen). The spawns of the monsters seen on the
// level are left out: the bot knows where those are, or that they are
// dead. Nothing here depends on the entity number of the sound.
func (w *World) spawnFits(a *actor, cue perception.Cue, atten float32, loop bool, pc *perception.Percept) []int {
	spawns := w.familySpawns(a.family)
	if len(spawns) == 0 {
		return nil
	}
	known := map[int]bool{}
	for _, o := range w.actors {
		if o.LastSeen > 0 && o.Lump >= 0 {
			known[o.Lump] = true
		}
	}
	eye := w.b.Self.Eye
	v := pc.Vision()
	var out []int
	for _, l := range spawns {
		sp := w.level.Map.Entities[l].Origin
		if known[l] || a.earMask&(1<<sectorOf(yawTo(eye, sp))) == 0 || pc.CueAt(sp, atten, loop).Loud != cue.Loud {
			continue
		}
		if v != nil && v.SeesPoint(sp) {
			continue
		}
		out = append(out, l)
	}
	return out
}

// earPlace sets the arc and the stand-in of a's Ear from its yaw mask and
// Ear.Dist, from where the bot stands now with view yaw viewYaw. Of the
// runs the mask leaves, the one with the last position seen, else the one
// with the most spawns that fit the sounds (a.fits), is picked (pickRun);
// when exactly one spawn fits, it is the stand-in.
func (w *World) earPlace(a *actor, viewYaw float32) {
	s := &w.b.Self
	runs := a.earMask.runs()
	if len(runs) == 0 {
		return
	}
	e := &a.Ear
	// the arc may have narrowed since the spawns were judged (feelHit)
	keep := a.fits[:0]
	var yaws []float32
	for _, l := range a.fits {
		if y := yawTo(s.Eye, w.level.Map.Entities[l].Origin); a.earMask&(1<<sectorOf(y)) != 0 {
			keep = append(keep, l)
			yaws = append(yaws, y)
		}
	}
	a.fits = keep
	var seenYaw float32
	if a.PosKnown {
		seenYaw = yawTo(s.Eye, a.Pos)
	}
	r := pickRun(runs, a.PosKnown, seenYaw, yaws, viewYaw)
	e.Yaw, e.Spread, e.Ambiguous = r.center(), float32(r.n)*earBin/2, len(runs) > 1
	sy, cy := math.Sincos(float64(e.Yaw) * math.Pi / 180)
	e.Est = Vec3{s.Origin[0] + float32(cy)*e.Dist, s.Origin[1] + float32(sy)*e.Dist, s.Origin[2]}
	e.AtSpawn = !a.PosKnown && len(a.fits) == 1 && r.has(sectorOf(yaws[0]))
	if e.AtSpawn {
		e.Est = w.level.Map.Entities[a.fits[0]].Origin
	}
}

// hitArc is the half width (degrees) of the arc a hit's recovered bearing
// leaves an attacker placed by ear (the bearing is good to about 30°:
// worldmodel.TestDemo1Belief).
const hitArc = 30

// feelHit narrows the arc of the track id placed by ear alone with the
// bearing (world yaw) of a hit attributed to it: the view kick is the bot's
// own state, as a player feels which side a hit came from. viewYaw is the
// bot's view yaw.
func (w *World) feelHit(id string, bearing, viewYaw float32) {
	for _, a := range w.actors {
		if a.ID != id {
			continue
		}
		if a.Visible || a.LocSeen || a.earMask == 0 || w.now-a.Ear.At > earMemory {
			return
		}
		if m := a.earMask & arcMask(bearing-hitArc, bearing+hitArc); m != 0 {
			a.earMask = m
			w.earPlace(a, viewYaw)
			a.locate()
		}
		return
	}
}

// locate sets a track's Loc: where the bot believes it is now.
func (a *actor) locate() {
	switch {
	case a.Visible:
		a.Loc, a.LocKnown, a.LocSeen = a.Pos, true, true
	case a.Ear.At > 0 && a.Ear.At >= a.LastSeen && !(a.PosKnown && a.Ear.Agrees):
		a.Loc, a.LocKnown, a.LocSeen = a.Ear.Est, true, false
	case a.PosKnown:
		a.Loc, a.LocKnown, a.LocSeen = a.Pos, true, true
	default:
		a.Loc, a.LocKnown, a.LocSeen = Vec3{}, false, false
	}
}
