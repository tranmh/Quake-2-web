package perception

import (
	"quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/shared"
)

// What a player hears of where a sound comes from. The client's mixer
// renders a sound with two numbers per channel and nothing else
// (S_SpatializeOrigin): a stereo balance, from the dot product of the
// listener's right vector with the direction to the source, and a distance
// attenuation, 1 - (dist - SOUND_FULLVOLUME) * dist_mult. Quake II has no
// front/back or height cue: a source ahead and one behind on the same side
// sound alike, and only a turn of the head (a change of the right vector
// between two sounds) tells them apart. A Cue is those two numbers in the
// coarse steps a listener tells apart; a Percept never holds the position
// of a sound whose emitter is not in view.
// C: client/snd_dma.c:425 S_SpatializeOrigin, :566 S_IssuePlaysound

// Pan is the stereo balance of a sound: five steps of the lateral part
// dot = right · direction (−1: all left, +1: all right). Hard: |dot| ≥
// sin 67.5°; soft: |dot| ≥ sin 22.5°; center below. For a source level
// with the ear, HardLeft is 67.5–112.5° to the left of the view, Left
// 22.5–67.5° or 112.5–157.5° (front or back), Center within 22.5° of
// straight ahead or straight behind. A source above or below sounds more
// central than its bearing.
type Pan int8

// Stereo balance steps.
const (
	PanNone      Pan = iota // no direction: not spatialized (ATTN_NONE) or not placed
	PanHardRight            // dot ≥ sin 67.5°
	PanRight                // sin 22.5° ≤ dot < sin 67.5°
	PanCenter               // |dot| < sin 22.5°: ahead or behind
	PanLeft                 // sin 22.5° ≤ −dot < sin 67.5°
	PanHardLeft             // −dot ≥ sin 67.5°
)

// Pan thresholds: sin 22.5°, sin 67.5°.
const (
	panSoft = 0.38268343
	panHard = 0.9238795
)

// String returns the step's name.
func (p Pan) String() string {
	switch p {
	case PanHardRight:
		return "hard_right"
	case PanRight:
		return "right"
	case PanCenter:
		return "center"
	case PanLeft:
		return "left"
	case PanHardLeft:
		return "hard_left"
	}
	return "none"
}

// MarshalText encodes the step by name.
func (p Pan) MarshalText() ([]byte, error) { return []byte(p.String()), nil }

// Arcs returns the bearings relative to the view (degrees, positive to the
// left, within [-180, 360)) a source level with the ear can have to sound
// with balance p: one arc for a hard or soft side (a soft side also covers
// the hard one, since a source above or below sounds more central), two
// (ahead and behind) for the center, none without a direction.
func (p Pan) Arcs() [][2]float32 {
	switch p {
	case PanHardLeft:
		return [][2]float32{{67.5, 112.5}}
	case PanLeft:
		return [][2]float32{{22.5, 157.5}}
	case PanCenter:
		return [][2]float32{{-22.5, 22.5}, {157.5, 202.5}}
	case PanRight:
		return [][2]float32{{-157.5, -22.5}}
	case PanHardRight:
		return [][2]float32{{-112.5, -67.5}}
	}
	return nil
}

// Loudness is how loud a sound plays against its own level at full volume:
// the mixer's distance attenuation in three steps. A player knows how loud
// a familiar sound is up close, so this is a coarse distance, whose scale
// depends on the sound's attenuation (ATTN_NORM reaches farther than
// ATTN_IDLE): LoudnessRange.
type Loudness int8

// Loudness steps.
const (
	LoudNone Loudness = iota // not attenuated (ATTN_NONE) or not placed: no distance
	LoudFar                  // attenuation < 0.4
	LoudMid                  // 0.4 ≤ attenuation < 0.75
	LoudNear                 // attenuation ≥ 0.75
)

// Loudness thresholds and the middle attenuation of each step.
const (
	loudMidAt  = 0.4
	loudNearAt = 0.75
)

// String returns the step's name.
func (l Loudness) String() string {
	switch l {
	case LoudFar:
		return "far"
	case LoudMid:
		return "mid"
	case LoudNear:
		return "near"
	}
	return "none"
}

// MarshalText encodes the step by name.
func (l Loudness) MarshalText() ([]byte, error) { return []byte(l.String()), nil }

// Cue is what a player hears of where a sound comes from.
type Cue struct {
	Pan  Pan
	Loud Loudness
}

// Directional reports a cue with a direction (a spatialized, placed sound).
func (c Cue) Directional() bool { return c.Pan != PanNone }

// LoudnessRange returns the distances (from the ear, units) at which a
// sound of attenuation atten (ATTN_*; loop: true for an entity's looping
// sound) plays with loudness l, and the distance in the middle of the
// step's attenuations. ok is false for LoudNone or an ATTN_NONE sound.
func LoudnessRange(l Loudness, atten float32, loop bool) (lo, mid, hi float32, ok bool) {
	mult := soundDistMult(atten)
	if loop {
		mult = soundLoopAttenuate
	}
	if mult <= 0 {
		return 0, 0, 0, false
	}
	d := func(g float32) float32 { return soundFullVolume + (1-g)/mult }
	switch l {
	case LoudNear:
		return 0, d((1 + loudNearAt) / 2), d(loudNearAt), true
	case LoudMid:
		return d(loudNearAt), d((loudNearAt + loudMidAt) / 2), d(loudMidAt), true
	case LoudFar:
		return d(loudMidAt), d(loudMidAt / 2), d(0), true
	}
	return 0, 0, 0, false
}

// cueAt is the cue of a sound the mixer plays at pos with distance
// multiplier distMult for the listener of vision v (zero for an
// unattenuated sound, which the mixer does not spatialize).
// C: client/snd_dma.c:425 S_SpatializeOrigin
func cueAt(v *Vision, pos Vec3, distMult float32) Cue {
	if distMult == 0 {
		return Cue{}
	}
	d := shared.VectorSubtract(pos, v.Eye())
	dist := shared.VectorNormalize(&d) - soundFullVolume
	if dist < 0 {
		dist = 0
	}
	g := 1 - dist*distMult
	_, right, _ := v.Axes()
	dot := shared.DotProduct(right, d)
	var c Cue
	switch {
	case dot >= panHard:
		c.Pan = PanHardRight
	case dot >= panSoft:
		c.Pan = PanRight
	case dot > -panSoft:
		c.Pan = PanCenter
	case dot > -panHard:
		c.Pan = PanLeft
	default:
		c.Pan = PanHardLeft
	}
	switch {
	case g >= loudNearAt:
		c.Loud = LoudNear
	case g >= loudMidAt:
		c.Loud = LoudMid
	default:
		c.Loud = LoudFar
	}
	return c
}

// CueAt returns the cue a sound of attenuation atten (loop: an entity's
// looping sound) would have this frame were it played at pos: the world
// model compares it with what it heard to tell whether a position it
// believes in agrees with a sound. Like Audible, valid until the next
// Perceive of the same Perceiver.
func (p *Percept) CueAt(pos Vec3, atten float32, loop bool) Cue {
	if p.vision == nil {
		return Cue{}
	}
	mult := soundDistMult(atten)
	if loop {
		mult = soundLoopAttenuate
	}
	return cueAt(p.vision, pos, mult)
}

// flashAttenuation is the attenuation of a muzzle flash's sound.
// C: client/cl_fx.c CL_ParseMuzzleFlash (S_StartSound ... ATTN_NORM)
const flashAttenuation = q2const.ATTN_NORM
