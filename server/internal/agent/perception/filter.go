package perception

import (
	"sort"

	"quake2web/server/internal/cmodel"
	"quake2web/server/internal/fakeclient"
	"quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/shared"
)

// Mixer constants of the client's sound system.
// C: client/snd_dma.c:36 SOUND_FULLVOLUME, :38 SOUND_LOOPATTENUATE
const (
	soundFullVolume    = 80
	soundLoopAttenuate = 0.003
	beamSamples        = 8
)

// Options configure a Perceiver.
type Options struct {
	View ViewOptions // field of view (and the test-only NoOcclusion)
}

// Sighting is an entity in the field of view with a line of sight from the
// eye this frame: everything about it is admissible.
type Sighting struct {
	Num   int32
	State shared.EntityState
	Classification
	// Anim is the frame's animation when the model's MD2 is known.
	Anim FrameAnim
	// Mins and Maxs are the box around State.Origin (decoded from
	// State.Solid, the inline model bounds, or the class prior); AbsMin and
	// AbsMax the world box. Beams span State.Origin to State.OldOrigin.
	Mins, Maxs     Vec3
	AbsMin, AbsMax Vec3
	Inline         int // N of a brush model "*N", else 0
	// Aim is the first visible point of the box (the center when it is
	// visible; the eye when inside the box). Beams leave it zero.
	Aim Vec3
	// Shootable: a shot from the eye reaches Aim (glass stops shots, not
	// sight). Set for monsters, neutrals, players and barrels.
	Shootable bool
	Dist      float32 // from the eye to the box center
}

// Center returns the world box center.
func (s *Sighting) Center() Vec3 {
	return Vec3{(s.AbsMin[0] + s.AbsMax[0]) / 2, (s.AbsMin[1] + s.AbsMax[1]) / 2, (s.AbsMin[2] + s.AbsMax[2]) / 2}
}

// Hearing is an audible sound: a svc_sound event, or a loop sound of the
// frame's entities. It says what the sound is and where it seems to come
// from (Cue), never where its emitter is: the client's mixer renders a
// stereo balance and a distance attenuation, not a position. A loop sound
// is the blend of every entity of the frame making it (Num 0), except a
// brush entity's.
type Hearing struct {
	Num     int32 // emitting entity (0: the world, none, or a blended loop)
	Channel int32
	Index   int32 // CS_SOUNDS index
	Path    string
	Kind    SoundKind
	Family  string // monster family of a monster voice
	// Cue is how the mixer renders the sound for the listener: its stereo
	// balance and loudness. It is zero without a direction: an ATTN_NONE
	// sound (not spatialized), or one this frame does not place (Placed
	// false).
	Cue Cue
	// Placed: the mixer plays the sound at a position this frame holds
	// (the message's, or the emitter's origin with the emitter in the
	// packet), and it is audible there. False: the emitter is not in the
	// frame and the message carried no position; the client mixer would
	// play it at the emitter's stale origin, so the Perceiver cannot tell
	// whether it is audible: the consumer must check Percept.Audible at
	// the position it believes the emitter is at (from earlier admitted
	// data) and drop the sound otherwise. ATTN_NONE sounds are audible
	// anywhere.
	Placed bool
	// Seen: the emitter is in view this frame (Percept.Sighting has it,
	// with its position).
	Seen bool
	// Mover is set when the emitter is a brush entity of the frame (door,
	// plat, button sounds): hearing it admits the mover's pose (the
	// mover's geometry and travel are static map knowledge; see ADR-0006).
	Mover       *BrushPose
	Volume      float32
	Attenuation float32
	Loop        bool
}

// Flash is an audible or visible muzzle flash of another entity. Its sound
// plays at Volume with ATTN_NORM (C: client/cl_fx.c CL_ParseMuzzleFlash).
// Weapon is WeaponNone for the non-weapon flashes (MZ_LOGIN, MZ_LOGOUT,
// MZ_RESPAWN, MZ_ITEMRESPAWN): no attack.
type Flash struct {
	Num      int32
	Monster  bool  // svc_muzzleflash2 (MZ2_*), else a player's (MZ_*)
	Raw      int32 // the MZ_ / MZ2_ byte
	Weapon   Weapon
	Silenced bool
	Volume   float32 // 1, or 0.2 when silenced
	// Cue is how the mixer renders the flash's sound (zero when it is not
	// placed, or seen but out of earshot).
	Cue Cue
	// Placed: the shooter is in the frame, and the flash is audible at its
	// origin or seen. False: as for a Hearing without a position, the
	// consumer must check Percept.Audible at the position it believes the
	// shooter is at.
	Placed bool
	// Seen: the shooter is in view this frame (the flash's light with it).
	Seen bool
}

// TempEvent is an admitted temp entity: an explosion (or other loud effect)
// heard, or an impact, trail or beam seen. One heard but not seen keeps
// only its Type of the TempEnt (no position) and its Cue.
type TempEvent struct {
	fakeclient.TempEnt
	Heard bool
	Seen  bool
	Cue   Cue // how its sound plays (when Heard)
}

// Percept is one frame as the player perceives it. It never holds an
// entity that is neither visible nor audible.
type Percept struct {
	Level       *LevelStatic
	CS          *ConfigStrings
	ServerFrame int32
	ServerTime  int32
	// PS is the own player state (always known to the player: view, HUD
	// stats, weapon).
	PS  shared.PlayerState
	Eye Vec3
	// Own is the own entity's state in this frame (HasOwn false if absent):
	// the client hears its own events (footsteps, falls).
	Own    shared.EntityState
	HasOwn bool
	// OwnFlashes counts the own weapon's muzzle flashes this frame.
	OwnFlashes int

	Seen     []Sighting // by entity number
	Heard    []Hearing  // svc_sound in arrival order, then loop sounds by number
	Flashes  []Flash
	TempEnts []TempEvent

	Prints       []string
	CenterPrints []string
	Layouts      []string
	Inventory    [q2const.MAX_ITEMS]int32
	InventorySeq uint64
	Lost         uint64

	vision *Vision
	cls    *Classifier
}

// Vision returns the frame's vision (valid until the next Perceive of the
// same Perceiver). The world model uses it to check whether a remembered
// position is in view now.
func (p *Percept) Vision() *Vision { return p.vision }

// Classifier returns the level's classifier.
func (p *Percept) Classifier() *Classifier { return p.cls }

// Audible reports whether the client's mixer plays a one-shot sound at pos
// with volume (0..1, as in Hearing.Volume) and attenuation (ATTN_*) above
// zero volume this frame. It is the gate for the sounds and flashes whose
// position the frame does not carry: pos must come from what the consumer
// admitted earlier. Like Vision, it is valid until the next Perceive of the
// same Perceiver.
// C: client/snd_dma.c:425 S_SpatializeOrigin, :566 S_IssuePlaysound
func (p *Percept) Audible(pos Vec3, volume, attenuation float32) bool {
	if p.vision == nil {
		return false
	}
	return audible(p.vision, pos, volume*255, soundDistMult(attenuation))
}

// Sighting returns the sighting of entity num (nil if not seen).
func (p *Percept) Sighting(num int32) *Sighting {
	i := sort.Search(len(p.Seen), func(i int) bool { return p.Seen[i].Num >= num })
	if i < len(p.Seen) && p.Seen[i].Num == num {
		return &p.Seen[i]
	}
	return nil
}

// Admitted returns the entity numbers whose state entered the percept this
// frame (seen, or a mover whose pose a sound of it gave), sorted. An
// entity only heard is not admitted: only its sound's Cue is. The fairness
// tests perturb every other entity, and the heard ones' origins within
// what keeps their cues.
func (p *Percept) Admitted() []int32 {
	var out []int32
	for i := range p.Seen {
		out = append(out, p.Seen[i].Num)
	}
	for i := range p.Heard {
		if h := &p.Heard[i]; h.Mover != nil {
			out = append(out, h.Num)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	n := 0
	for i, v := range out {
		if i == 0 || v != out[n-1] {
			out[n] = v
			n++
		}
	}
	return out[:n]
}

// Perceiver is the ObservationFilter of one level. Perceive depends only on
// its FrameInput (and the static level data): the Perceiver keeps no memory
// between frames, which is what makes the fairness tests meaningful.
type Perceiver struct {
	cls    *Classifier
	anims  *AnimCache
	vision *Vision
}

// NewPerceiver returns a Perceiver for a level with collision map cm,
// classifying with cls and reading animations from anims (nil: none).
// Without a map the Perceiver sees nothing and only hears (see
// ViewOptions.NoOcclusion for the test-only alternative).
func NewPerceiver(cm *cmodel.Map, cls *Classifier, anims *AnimCache, opt Options) *Perceiver {
	return &Perceiver{cls: cls, anims: anims, vision: NewVision(cm, opt.View)}
}

// Vision returns the perceiver's vision.
func (p *Perceiver) Vision() *Vision { return p.vision }

// Classifier returns the perceiver's classifier.
func (p *Perceiver) Classifier() *Classifier { return p.cls }

// Eye returns the eye position of a player state: the pmove origin plus
// the view offset (what SV_BuildClientFrame and the renderer use).
func Eye(ps *shared.PlayerState) Vec3 {
	var e Vec3
	for i := 0; i < 3; i++ {
		e[i] = float32(float64(ps.PMove.Origin[i])*0.125 + float64(ps.ViewOffset[i]))
	}
	return e
}

// Perceive filters one frame.
func (p *Perceiver) Perceive(in *FrameInput) *Percept {
	p.cls.Update(in.CS)
	own := in.OwnEntity()
	pc := &Percept{
		Level: in.Level, CS: in.CS,
		ServerFrame: in.ServerFrame, ServerTime: in.ServerTime,
		PS: in.PlayerState, Eye: Eye(&in.PlayerState),
		Prints: in.Events.Prints, CenterPrints: in.Events.CenterPrints, Layouts: in.Events.Layouts,
		Inventory: in.Inventory, InventorySeq: in.InventorySeq, Lost: in.Events.Lost,
		vision: p.vision, cls: p.cls,
	}

	// brush entities occlude
	var brushes []BrushPose
	for i := range in.Entities {
		e := &in.Entities[i]
		if n := p.cls.InlineModel(e.ModelIndex); n != 0 && e.RenderFX&q2const.RF_BEAM == 0 {
			brushes = append(brushes, BrushPose{Num: e.Number, Inline: n, Origin: e.Origin, Angles: e.Angles})
		}
	}
	p.vision.Begin(pc.Eye, in.PlayerState.ViewAngles, in.PlayerState.Fov, in.AreaBits[:min(max(in.AreaBytes, 0), len(in.AreaBits))], brushes)

	byNum := make(map[int32]*shared.EntityState, len(in.Entities))
	for i := range in.Entities {
		e := &in.Entities[i]
		byNum[e.Number] = e
		if e.Number == own {
			pc.Own, pc.HasOwn = *e, true
			continue
		}
		if s, ok := p.sight(e); ok {
			pc.Seen = append(pc.Seen, s)
		}
	}
	sort.Slice(pc.Seen, func(i, j int) bool { return pc.Seen[i].Num < pc.Seen[j].Num })

	p.hear(pc, in, byNum, own)
	p.flashes(pc, in, byNum, own)
	p.tempEnts(pc, in)
	return pc
}

// box returns the entity's box around its origin.
func (p *Perceiver) box(e *shared.EntityState, c *Class, inline int) (mins, maxs Vec3) {
	if inline != 0 {
		lo, hi, ok := p.vision.BrushBounds(inline, e.Origin, e.Angles)
		if ok {
			return shared.VectorSubtract(lo, e.Origin), shared.VectorSubtract(hi, e.Origin)
		}
	}
	if lo, hi, ok := DecodeSolid(e.Solid); ok {
		return lo, hi
	}
	if c != nil && (c.Mins != (Vec3{}) || c.Maxs != (Vec3{})) {
		mins, maxs = c.Mins, c.Maxs
		if c.Kind == KindMonster || c.Kind == KindNeutral {
			maxs[2] = min(maxs[2], -8) // not solid: a corpse lies flat (C: *_dead)
		}
		return mins, maxs
	}
	return Vec3{-4, -4, -4}, Vec3{4, 4, 4}
}

// sight decides whether entity e is visible and describes it.
func (p *Perceiver) sight(e *shared.EntityState) (Sighting, bool) {
	cl := p.cls.Classify(e)
	if cl.Class == nil || cl.Class.Kind == KindSpeaker || (e.ModelIndex == 0 && e.RenderFX&q2const.RF_BEAM == 0) {
		return Sighting{}, false
	}
	s := Sighting{Num: e.Number, State: *e, Classification: cl}
	v := p.vision
	if cl.Class.Kind == KindBeam {
		if !v.SeesSegment(e.Origin, e.OldOrigin, beamSamples, -1) {
			return Sighting{}, false
		}
		for i := 0; i < 3; i++ {
			s.AbsMin[i], s.AbsMax[i] = min(e.Origin[i], e.OldOrigin[i]), max(e.Origin[i], e.OldOrigin[i])
		}
		s.Mins, s.Maxs = shared.VectorSubtract(s.AbsMin, e.Origin), shared.VectorSubtract(s.AbsMax, e.Origin)
	} else {
		s.Inline = p.cls.InlineModel(e.ModelIndex)
		s.Mins, s.Maxs = p.box(e, cl.Class, s.Inline)
		s.AbsMin, s.AbsMax = shared.VectorAdd(e.Origin, s.Mins), shared.VectorAdd(e.Origin, s.Maxs)
		ignore := int32(-1)
		if s.Inline != 0 {
			ignore = e.Number
		}
		aim, ok := v.VisiblePoint(s.AbsMin, s.AbsMax, ignore)
		if !ok {
			return Sighting{}, false
		}
		s.Aim = aim
	}
	c := s.Center()
	s.Dist = shared.VectorLength(shared.VectorSubtract(c, v.Eye()))
	switch cl.Class.Kind {
	case KindMonster, KindNeutral, KindPlayer, KindBarrel:
		// towards the visible part: a shot at a hidden center would ask
		// about geometry the player cannot see
		s.Shootable = v.Shootable(s.Aim, -1)
	}
	switch cl.Class.Kind {
	case KindMonster, KindNeutral, KindPlayer:
		if m := p.anims.Model(cl.Model); m != nil {
			s.Anim = m.At(e.Frame)
		}
	}
	return s, true
}

// audible reports whether the client's mixer plays a sound at pos with
// master volume master (0..255) and distance multiplier distMult with a
// non-zero volume on either channel, for the listener of vision v.
// C: client/snd_dma.c:425 S_SpatializeOrigin
func audible(v *Vision, pos Vec3, master, distMult float32) bool {
	d := shared.VectorSubtract(pos, v.Eye())
	dist := shared.VectorNormalize(&d) - soundFullVolume
	if dist < 0 {
		dist = 0
	}
	dist *= distMult
	if distMult == 0 {
		return int32(master) > 0
	}
	_, right, _ := v.Axes()
	dot := shared.DotProduct(right, d)
	r, l := 0.5*(1+dot), 0.5*(1-dot)
	return int32(master*(1-dist)*r) > 0 || int32(master*(1-dist)*l) > 0
}

// soundDistMult is the channel distance multiplier of a one-shot sound.
// C: client/snd_dma.c:566 S_IssuePlaysound
func soundDistMult(atten float32) float32 {
	if atten == q2const.ATTN_STATIC {
		return atten * 0.001
	}
	return atten * 0.0005
}

func (p *Perceiver) soundName(index int32, cs *ConfigStrings) string {
	if cs == nil || index <= 0 || index >= q2const.MAX_SOUNDS {
		return ""
	}
	return cs[q2const.CS_SOUNDS+int(index)]
}

func (p *Perceiver) brushPose(e *shared.EntityState) *BrushPose {
	if e == nil {
		return nil
	}
	n := p.cls.InlineModel(e.ModelIndex)
	if n == 0 {
		return nil
	}
	return &BrushPose{Num: e.Number, Inline: n, Origin: e.Origin, Angles: e.Angles}
}

// hear admits the audible svc_sound events and loop sounds, with their
// cues and without their positions.
func (p *Perceiver) hear(pc *Percept, in *FrameInput, byNum map[int32]*shared.EntityState, own int32) {
	for _, s := range in.Events.Sounds {
		if s.Ent == own && own != 0 {
			continue // the player's own noises
		}
		path := p.soundName(s.SoundNum, in.CS)
		kind, fam := ClassifySound(path)
		h := Hearing{Num: s.Ent, Channel: s.Channel, Index: s.SoundNum, Path: path, Kind: kind, Family: fam,
			Volume: s.Volume, Attenuation: s.Attenuation}
		e := byNum[s.Ent]
		if s.Ent == 0 {
			e = nil
		}
		var pos Vec3
		switch {
		case s.Pos != nil:
			pos, h.Placed = *s.Pos, true
		case e != nil:
			pos, h.Placed = e.Origin, true
		}
		if h.Placed {
			mult := soundDistMult(s.Attenuation)
			if !audible(p.vision, pos, s.Volume*255, mult) {
				continue
			}
			h.Cue = cueAt(p.vision, pos, mult)
		}
		h.Seen = s.Ent != 0 && pc.Sighting(s.Ent) != nil
		h.Mover = p.brushPose(e)
		pc.Heard = append(pc.Heard, h)
	}
	// Loop sounds. The mixer sums every entity of the frame that has the
	// same loop sound into one channel, adding up their spatialized left
	// and right volumes: a player hears one blended loop per sound, not how
	// many entities make it nor which. So a loop is one Hearing per sound
	// index, with the cue of the sum, no entity number and no Seen. A brush
	// entity's loop (a door or plat under way) is the stated exception: it
	// keeps its entity, whose pose it admits.
	// C: client/snd_dma.c S_AddLoopSounds
	type loopSum struct {
		l, r  float32
		n     int
		first Vec3 // the origin of the first audible entity
	}
	sums := map[int32]*loopSum{}
	var order []int32
	for i := range in.Entities {
		e := &in.Entities[i]
		if e.Sound == 0 || e.Number == own {
			continue
		}
		if !audible(p.vision, e.Origin, 255, soundLoopAttenuate) {
			continue
		}
		if mv := p.brushPose(e); mv != nil {
			path := p.soundName(e.Sound, in.CS)
			kind, fam := ClassifySound(path)
			pc.Heard = append(pc.Heard, Hearing{Num: e.Number, Index: e.Sound, Path: path, Kind: kind, Family: fam,
				Cue: cueAt(p.vision, e.Origin, soundLoopAttenuate), Placed: true, Seen: pc.Sighting(e.Number) != nil,
				Mover: mv, Volume: 1, Attenuation: q2const.ATTN_STATIC, Loop: true})
			continue
		}
		ls := sums[e.Sound]
		if ls == nil {
			ls = &loopSum{first: e.Origin}
			sums[e.Sound] = ls
			order = append(order, e.Sound)
		}
		l, r := spatialize(p.vision, e.Origin, soundLoopAttenuate)
		ls.l, ls.r, ls.n = ls.l+l, ls.r+r, ls.n+1
	}
	sort.Slice(order, func(i, j int) bool { return order[i] < order[j] })
	for _, idx := range order {
		ls := sums[idx]
		cue := cueAt(p.vision, ls.first, soundLoopAttenuate)
		if ls.n > 1 {
			cue = cueOf(min(ls.l, 1), min(ls.r, 1))
		}
		path := p.soundName(idx, in.CS)
		kind, fam := ClassifySound(path)
		pc.Heard = append(pc.Heard, Hearing{Index: idx, Path: path, Kind: kind, Family: fam, Cue: cue, Placed: true,
			Volume: 1, Attenuation: q2const.ATTN_STATIC, Loop: true})
	}
}

// flashes admits the audible or visible muzzle flashes of other entities.
// C: client/cl_fx.c:238 CL_ParseMuzzleFlash, :429 CL_ParseMuzzleFlash2
func (p *Perceiver) flashes(pc *Percept, in *FrameInput, byNum map[int32]*shared.EntityState, own int32) {
	for _, f := range in.Events.MuzzleFlashes {
		if !f.Monster && f.Ent == own {
			pc.OwnFlashes++
			continue
		}
		fl := Flash{Num: f.Ent, Monster: f.Monster, Raw: f.Weapon, Volume: 1}
		if f.Monster {
			fl.Weapon = MonsterFlashWeapon(f.Weapon)
		} else {
			fl.Silenced = f.Weapon&q2const.MZ_SILENCED != 0
			fl.Weapon = PlayerFlashWeapon(f.Weapon &^ q2const.MZ_SILENCED)
			if fl.Silenced {
				fl.Volume = 0.2
			}
		}
		if e := byNum[f.Ent]; e != nil {
			mult := soundDistMult(flashAttenuation)
			fl.Placed, fl.Seen = true, pc.Sighting(f.Ent) != nil
			if audible(p.vision, e.Origin, fl.Volume*255, mult) {
				fl.Cue = cueAt(p.vision, e.Origin, mult)
			} else if !fl.Seen {
				continue
			}
		}
		pc.Flashes = append(pc.Flashes, fl)
	}
}

// tempEnts admits loud temp entities when audible and the others when seen.
// C: client/cl_tent.c:694 CL_ParseTEnt (the effects that start a sound)
func (p *Perceiver) tempEnts(pc *Percept, in *FrameInput) {
	for _, t := range in.Events.TempEnts {
		ev := TempEvent{TempEnt: t}
		switch t.Type {
		case q2const.TE_EXPLOSION1, q2const.TE_EXPLOSION2, q2const.TE_ROCKET_EXPLOSION, q2const.TE_ROCKET_EXPLOSION_WATER,
			q2const.TE_GRENADE_EXPLOSION, q2const.TE_GRENADE_EXPLOSION_WATER, q2const.TE_EXPLOSION1_BIG,
			q2const.TE_EXPLOSION1_NP, q2const.TE_PLASMA_EXPLOSION, q2const.TE_BFG_BIGEXPLOSION,
			q2const.TE_PLAIN_EXPLOSION, q2const.TE_TRACKER_EXPLOSION, q2const.TE_NUKEBLAST, q2const.TE_BOSSTPORT,
			q2const.TE_BLASTER, q2const.TE_BLASTER2, q2const.TE_FLECHETTE, q2const.TE_RAILTRAIL:
			if mult := soundDistMult(q2const.ATTN_NORM); audible(p.vision, t.Pos, 255, mult) {
				ev.Heard, ev.Cue = true, cueAt(p.vision, t.Pos, mult)
			}
		}
		switch t.Type {
		case q2const.TE_RAILTRAIL, q2const.TE_BUBBLETRAIL, q2const.TE_BUBBLETRAIL2, q2const.TE_DEBUGTRAIL,
			q2const.TE_BLUEHYPERBLASTER, q2const.TE_BFG_LASER, q2const.TE_PARASITE_ATTACK,
			q2const.TE_MEDIC_CABLE_ATTACK, q2const.TE_GRAPPLE_CABLE, q2const.TE_LIGHTNING, q2const.TE_HEATBEAM,
			q2const.TE_MONSTER_HEATBEAM, q2const.TE_FORCEWALL:
			ev.Seen = p.vision.SeesSegment(t.Pos, t.Pos2, beamSamples, -1)
		default:
			ev.Seen = p.vision.SeesPoint(t.Pos)
		}
		if ev.Heard && !ev.Seen {
			ev.TempEnt = fakeclient.TempEnt{Type: t.Type} // heard: what it was, not where
		}
		if ev.Heard || ev.Seen {
			pc.TempEnts = append(pc.TempEnts, ev)
		}
	}
}
