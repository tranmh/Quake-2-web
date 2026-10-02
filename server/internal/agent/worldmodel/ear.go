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
// and the bot's own state alone. A monster never seen whose spawn origin
// the bot knows from the entity lump (static map knowledge) stands in at
// that origin while the sounds agree with it.

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
	// AtSpawn: the track was never seen, the bot knows where it spawns
	// (the entity lump: static map knowledge), and that spawn origin fits
	// the sounds as Agrees says: Est is the spawn origin.
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
// that holds the bearing of the last position seen or of the spawn origin
// the sounds agree with (seen is false without one), else the one farthest
// from the view (a source the bot does not
// see is likelier out of view than hidden in it), else the first.
func pickRun(runs []yawRun, seen bool, seenYaw, viewYaw float32) yawRun {
	if seen {
		k := sectorOf(seenYaw)
		for _, r := range runs {
			if r.has(k) {
				return r
			}
		}
	}
	best, bd := runs[0], float32(-1)
	for _, r := range runs {
		if d := absf(angleDiff(r.center(), viewYaw)); d > bd+0.01 {
			best, bd = r, d
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
	sp, ok := w.spawnOf(a)
	e.AtSpawn = ok && !a.PosKnown && m&(1<<sectorOf(yawTo(s.Eye, sp))) != 0 && pc.CueAt(sp, atten, loop).Loud == cue.Loud
	w.earPlace(a, viewYaw)
	a.LastUpdate = w.now
}

// spawnOf returns the spawn origin of the lump entity track a stands for
// (static map knowledge), ok false without one.
func (w *World) spawnOf(a *actor) (Vec3, bool) {
	if a.Lump < 0 || w.level.Map == nil || a.Lump >= len(w.level.Map.Entities) {
		return Vec3{}, false
	}
	return w.level.Map.Entities[a.Lump].Origin, true
}

// earPlace sets the arc and the stand-in of a's Ear from its yaw mask and
// Ear.Dist, from where the bot stands now with view yaw viewYaw. Of the
// runs the mask leaves, the one with the last position seen (or the spawn
// origin the sounds agree with: Ear.AtSpawn) is picked (pickRun).
func (w *World) earPlace(a *actor, viewYaw float32) {
	s := &w.b.Self
	runs := a.earMask.runs()
	if len(runs) == 0 {
		return
	}
	e := &a.Ear
	sp, spawn := w.spawnOf(a)
	spawn = spawn && e.AtSpawn && !a.PosKnown && a.earMask&(1<<sectorOf(yawTo(s.Eye, sp))) != 0
	prefer, preferYaw := a.PosKnown, float32(0)
	switch {
	case a.PosKnown:
		preferYaw = yawTo(s.Eye, a.Pos)
	case spawn:
		prefer, preferYaw = true, yawTo(s.Eye, sp)
	}
	r := pickRun(runs, prefer, preferYaw, viewYaw)
	e.Yaw, e.Spread, e.Ambiguous = r.center(), float32(r.n)*earBin/2, len(runs) > 1
	sy, cy := math.Sincos(float64(e.Yaw) * math.Pi / 180)
	e.Est = Vec3{s.Origin[0] + float32(cy)*e.Dist, s.Origin[1] + float32(sy)*e.Dist, s.Origin[2]}
	e.AtSpawn = spawn
	if spawn {
		e.Est = sp
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
