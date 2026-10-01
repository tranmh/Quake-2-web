package route

import (
	"fmt"
	"math"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"quake2web/server/internal/agent/mapdata"
	"quake2web/server/internal/qcommon/shared"
)

// Problem is one validation finding.
type Problem struct {
	// Table is the table name ("" for campaign-level problems).
	Table string
	// Where locates it: "table", "step 3 (press)", "avoid 0", "campaign".
	Where string
	Msg   string
}

func (p Problem) String() string {
	if p.Table == "" {
		return p.Where + ": " + p.Msg
	}
	return p.Table + ": " + p.Where + ": " + p.Msg
}

// Error is returned by Validate and ValidateCampaign; it lists every
// problem found.
type Error struct {
	Problems []Problem
}

func (e *Error) Error() string {
	lines := make([]string, len(e.Problems))
	for i, p := range e.Problems {
		lines[i] = p.String()
	}
	return strings.Join(lines, "\n")
}

// Resolve finds the present entity a reference names and checks every
// field it gives. An entity that is inhibited at the map's skill or freed
// by its spawn function is an error.
func Resolve(r Ref, m *mapdata.Map) (*mapdata.Entity, error) {
	var e *mapdata.Entity
	switch {
	case r.Entity != nil:
		if e = m.Entity(*r.Entity); e == nil {
			return nil, fmt.Errorf("%v: no entity #%d (the lump has %d)", r, *r.Entity, len(m.Entities))
		}
	case r.Model != "":
		for i := range m.Entities {
			if m.Entities[i].Model == r.Model {
				e = &m.Entities[i]
				if e.Present() {
					break
				}
			}
		}
		if e == nil {
			return nil, fmt.Errorf("%v: no entity uses model %s", r, r.Model)
		}
	case r.Targetname != "":
		var cands []*mapdata.Entity
		for _, c := range m.Targets(r.Targetname) {
			if r.Classname == "" || c.Classname == r.Classname {
				cands = append(cands, c)
			}
		}
		switch len(cands) {
		case 0:
			return nil, fmt.Errorf("%v: no present entity has this targetname", r)
		case 1:
			e = cands[0]
		default:
			return nil, fmt.Errorf("%v: ambiguous, %d entities match (give entity or model)", r, len(cands))
		}
	default:
		return nil, fmt.Errorf("empty entity reference")
	}
	if r.Model != "" && e.Model != r.Model {
		return nil, fmt.Errorf("%v: entity is %v (model %q)", r, e, e.Model)
	}
	if r.Classname != "" && e.Classname != r.Classname {
		return nil, fmt.Errorf("%v: entity is %v", r, e)
	}
	if r.Targetname != "" && shared.Q_stricmp(e.Targetname, r.Targetname) != 0 {
		return nil, fmt.Errorf("%v: entity is %v (targetname %q)", r, e, e.Targetname)
	}
	switch {
	case e.Inhibited:
		return nil, fmt.Errorf("%v: %v is inhibited at skill %d (spawnflags %#x)", r, e, m.Skill, e.RawSpawnflags)
	case e.Freed:
		return nil, fmt.Errorf("%v: %v is freed by its spawn function", r, e)
	}
	return e, nil
}

// Validate checks one table against the map data:
//   - the table is for this map, its arrival spawnpoint exists and its exit
//     is one of the map's target_changelevels;
//   - every referenced entity exists with the expected classname, model and
//     targetname and is present at the map's skill; kill and pickup targets
//     exist;
//   - every press / touch / shoot step (and the effects a kill, pickup or
//     ride claims) has a logic chain (targets through relays, delays,
//     counters, keys, doors, carrying movers) that reaches the claimed
//     effects, with the keys it needs picked up earlier and TRIGGERED
//     triggers enabled earlier; a claimed door opening or mover pose is
//     where that use sends the mover; directional triggers are touched
//     facing along their movedir; wait / confirm effects were caused
//     earlier;
//   - no step activates, or relies on a chain through, a single-use entity
//     an earlier step used up (a fired trigger_once, a pressed wait -1
//     button, a killed monster, a removed killtarget, ...);
//   - the last step leads to the table's exit and no step to another exit;
//   - every avoid entry is an activator of another exit.
func Validate(t *Table, m *mapdata.Map) error {
	v := newValidator(t, m, nil)
	v.run()
	if len(v.problems) > 0 {
		return &Error{Problems: v.problems}
	}
	return nil
}

// usage records how a single-use entity was used up.
type usage struct {
	// gone: the entity was removed (a killtarget, a fired trigger_once, a
	// killed monster, a picked-up item, a destroyed func_explosive).
	// Otherwise it still exists but no longer reacts: button_fire and
	// door_go_up ignore a wait -1 button or door at its end pose, so it
	// neither moves nor fires its targets again.
	gone bool
	// how is the past participle for messages ("fired", "pressed", ...),
	// by the step that did it ("demo2a step 1").
	how, by string
}

type validator struct {
	t        *Table
	m        *mapdata.Map
	problems []Problem
	where    string
	step     int

	inventory map[string]bool
	reached   map[int]mapdata.Reached // everything earlier steps set off
	enabled   map[int]bool
	face      *float32
	// used holds the entities used up before the current step: by earlier
	// visits of the map (a revisit restores the level as it was left) and
	// by earlier steps of this table. consumed is this table's own share,
	// pending the current step's until the step has been checked.
	used, consumed, pending map[int]usage
	exit                    *mapdata.Exit
}

// newValidator returns a validator for t on m; earlier lists what earlier
// visits of the map used up (nil for none).
func newValidator(t *Table, m *mapdata.Map, earlier map[int]usage) *validator {
	used := make(map[int]usage, len(earlier))
	for i, u := range earlier {
		used[i] = u
	}
	return &validator{
		t: t, m: m, where: "table",
		inventory: map[string]bool{}, reached: map[int]mapdata.Reached{}, enabled: map[int]bool{},
		used: used, consumed: map[int]usage{}, pending: map[int]usage{},
	}
}

func (v *validator) errorf(format string, args ...any) {
	p := Problem{Table: v.t.Name, Where: v.where, Msg: fmt.Sprintf(format, args...)}
	for _, q := range v.problems {
		if q == p {
			return // the same finding through two checks (a claimed exit and the table's exit)
		}
	}
	v.problems = append(v.problems, p)
}

func (v *validator) resolve(r *Ref, what string) *mapdata.Entity {
	if r == nil {
		v.errorf("%s needs a target", what)
		return nil
	}
	e, err := Resolve(*r, v.m)
	if err != nil {
		v.errorf("%s: %v", what, err)
		return nil
	}
	return e
}

func (v *validator) run() {
	t, m := v.t, v.m
	if t.Name == "" {
		v.errorf("missing name")
	}
	if !strings.EqualFold(t.Map, m.Name) {
		v.errorf("table is for map %q, validated against %q", t.Map, m.Name)
	}
	if _, ok := m.SpawnPoint(t.From); !ok {
		v.errorf("arrival spawnpoint %q: no info_player_start with that targetname", t.From)
	}
	for k := range m.Exits {
		if m.Exits[k].Level.Raw == t.Exit.Map {
			v.exit = &m.Exits[k]
			break
		}
	}
	if v.exit == nil {
		var have []string
		for _, x := range m.Exits {
			have = append(have, fmt.Sprintf("%q", x.Level.Raw))
		}
		v.errorf("exit %q is no target_changelevel of %s (it has %s)", t.Exit.Map, m.Name, strings.Join(have, ", "))
	}
	if len(t.Steps) == 0 {
		v.errorf("no steps")
		return
	}

	for i := range t.Steps {
		s := &t.Steps[i]
		v.step, v.where = i, fmt.Sprintf("step %d (%s)", i, s.Op)
		start, reach := v.runStep(s)
		for _, r := range reach {
			if x := m.Exit(r.Entity); x != nil && r.Response == mapdata.RespExit && x.Level.Raw != t.Exit.Map {
				v.errorf("leads to the wrong exit %q (#%d)", x.Level.Raw, x.Entity)
			}
		}
		if i == len(t.Steps)-1 {
			v.checkExit(start, reach)
		}
		for _, r := range reach {
			if _, ok := v.reached[r.Entity]; !ok {
				v.reached[r.Entity] = r
			}
			// a carry into a trigger touches it, a killtarget removes it:
			// only a use enables a TRIGGERED trigger or spawns an entity
			if r.Via.Kind == mapdata.LinkTarget && (r.Response == mapdata.RespEnable || r.Response == mapdata.RespSpawn) {
				v.enabled[r.Entity] = true
			}
			v.consumeReached(r)
		}
		for e, u := range v.pending {
			if old, ok := v.used[e]; !ok || u.gone && !old.gone {
				v.used[e] = u
			}
			v.consumed[e] = u
		}
		clear(v.pending)
	}

	for k, a := range t.Avoid {
		v.where = fmt.Sprintf("avoid %d", k)
		e := v.resolve(&a.Target, "avoid")
		if e == nil {
			continue
		}
		if a.Why == "" {
			v.errorf("missing why")
		}
		other := ""
		for _, r := range m.ReachEnabled(e.Index, v.isEnabled) {
			if x := m.Exit(r.Entity); x != nil && r.Response == mapdata.RespExit && x.Level.Raw != t.Exit.Map {
				other = x.Level.Raw
				break
			}
		}
		if other == "" {
			v.errorf("%v does not lead to another exit", e)
		}
		for i := range t.Steps {
			if s := &t.Steps[i]; s.Target != nil {
				if se, err := Resolve(*s.Target, m); err == nil && se.Index == e.Index {
					v.errorf("step %d uses %v, which the table avoids", i, e)
				}
			}
		}
	}
}

func (v *validator) isEnabled(i int) bool { return v.enabled[i] }

// checkExit checks that the last step's chain (from start) leads to the
// table's exit.
func (v *validator) checkExit(start int, reach []mapdata.Reached) {
	if v.exit == nil {
		return
	}
	for _, r := range reach {
		if r.Entity == v.exit.Entity && r.Response == mapdata.RespExit {
			v.checkConditions(r)
			v.checkChain(start, reach, r)
			return
		}
	}
	v.errorf("the last step does not lead to the exit %q (#%d %s); it reaches %s",
		v.t.Exit.Map, v.exit.Entity, v.exit.Targetname, v.describe(reach))
}

// runStep validates one step and returns the entity whose activation starts
// its chain (-1 for none) and what that chain reaches.
func (v *validator) runStep(s *Step) (int, []mapdata.Reached) {
	m := v.m
	switch s.Op {
	case OpGoto:
		if (s.Target == nil) == (s.Pos == nil) {
			v.errorf("goto needs exactly one of target or pos")
		} else if s.Target != nil {
			if e := v.resolve(s.Target, "goto"); e != nil {
				v.checkExists(e, "goto")
			}
		}
		return -1, nil

	case OpTouch:
		e := v.resolve(s.Target, "touch")
		if e == nil {
			return -1, nil
		}
		tr := m.Trigger(e.Index)
		if tr == nil || !tr.HasVolume || (tr.Classname != "trigger_multiple" && tr.Classname != "trigger_once") {
			v.errorf("%v is not a trigger_multiple / trigger_once volume", e)
			return -1, nil
		}
		v.checkActivate(e)
		if tr.NotPlayer {
			v.errorf("%v ignores players (NOT_PLAYER)", e)
		}
		if tr.Triggered && !v.enabled[e.Index] {
			v.errorf("%v is TRIGGERED and no earlier step enables it", e)
		}
		if tr.Directional() {
			yaw := s.Yaw
			if yaw == nil {
				yaw = v.face
			}
			if yaw == nil {
				v.errorf("%v is directional (movedir %v): give a yaw or face first", e, tr.Movedir)
			} else if f := forward(*yaw); shared.DotProduct(f, tr.Movedir) < 0 {
				v.errorf("%v is directional (movedir %v): yaw %g faces away", e, tr.Movedir, *yaw)
			}
		}
		if tr.Wait < 0 {
			v.consume(e.Index, true, "fired") // multi_trigger frees it
		}
		return e.Index, v.effects(s, e, true)

	case OpPress:
		e := v.resolve(s.Target, "press")
		if e == nil {
			return -1, nil
		}
		mv := m.Mover(e.Index)
		if mv == nil || mv.Kind != mapdata.MoverButton || mv.Activation&mapdata.ActTouch == 0 {
			act := "none"
			if mv != nil {
				act = mv.Activation.String()
			}
			v.errorf("%v is not a touch-activated func_button (activation %s)", e, act)
			return -1, nil
		}
		v.checkActivate(e)
		if mv.Wait < 0 {
			v.consume(e.Index, false, "pressed")
		}
		return e.Index, v.effects(s, e, true)

	case OpShoot:
		e := v.resolve(s.Target, "shoot")
		if e == nil {
			return -1, nil
		}
		mv := m.Mover(e.Index)
		if mv == nil || mv.Activation&mapdata.ActShoot == 0 {
			v.errorf("%v cannot be shot (no health / die function)", e)
			return -1, nil
		}
		v.checkActivate(e)
		switch {
		case mv.Kind == mapdata.MoverExplosive:
			v.consume(e.Index, true, "destroyed")
		case mv.Kind == mapdata.MoverButton && mv.Wait < 0:
			v.consume(e.Index, false, "pressed")
		}
		return e.Index, v.effects(s, e, true)

	case OpRide:
		e := v.resolve(s.Target, "ride")
		if e == nil {
			return -1, nil
		}
		mv := m.Mover(e.Index)
		if mv == nil || (mv.Kind != mapdata.MoverDoor && mv.Kind != mapdata.MoverPlat && mv.Kind != mapdata.MoverTrain) {
			v.errorf("%v is not a door, plat or train to ride", e)
			return -1, nil
		}
		v.checkExists(e, "ride")
		if !validPose(mv, s.Until) {
			v.errorf("%v has no pose %q (%s)", e, s.Until, poseNames(mv))
		}
		return e.Index, v.effects(s, e, false)

	case OpWait:
		if s.Seconds <= 0 && len(s.Effects) == 0 {
			v.errorf("wait needs seconds or effects")
		}
		if s.Seconds < 0 {
			v.errorf("negative seconds %g", s.Seconds)
		}
		v.claimedEarlier(s.Effects)
		return -1, nil

	case OpFace:
		if s.Yaw == nil {
			v.errorf("face needs a yaw")
		}
		v.face = s.Yaw
		return -1, nil

	case OpKill:
		mo := v.monster(s)
		if mo == nil {
			return -1, nil
		}
		e := m.Entity(mo.Entity)
		if mo.TriggerSpawn && !v.enabled[mo.Entity] {
			v.errorf("%v is trigger-spawned and no earlier step spawns it", e)
		}
		v.checkActivate(e)
		v.consume(mo.Entity, true, "killed")
		return mo.Entity, v.effects(s, e, false)

	case OpPickup:
		it := v.item(s)
		if it == nil {
			return -1, nil
		}
		e := m.Entity(it.Entity)
		v.checkActivate(e)
		v.inventory[it.Classname] = true
		v.consume(it.Entity, true, "picked up")
		return it.Entity, v.effects(s, e, false)

	case OpConfirm:
		if len(s.Effects) == 0 {
			v.errorf("confirm needs effects")
		}
		v.claimedEarlier(s.Effects)
		return -1, nil
	}
	v.errorf("unknown op %q", s.Op)
	return -1, nil
}

// effects computes the chain of e and checks the step's claimed effects
// against it.
func (v *validator) effects(s *Step, e *mapdata.Entity, required bool) []mapdata.Reached {
	reach := v.m.ReachEnabled(e.Index, v.isEnabled)
	if required && len(s.Effects) == 0 {
		v.errorf("%s must claim at least one effect (%v reaches %s)", s.Op, e, v.describe(reach))
	}
	for _, eff := range s.Effects {
		te, err := Resolve(eff.Target, v.m)
		if err != nil {
			v.errorf("effect %s: %v", eff.Kind, err)
			continue
		}
		if !v.checkExists(te, "effect "+string(eff.Kind)) {
			continue
		}
		r, ok := v.find(reach, eff, te)
		if !ok {
			v.errorf("%v does not cause %s of %v (its chain reaches %s)", e, eff.Kind, te, v.describe(reach))
			continue
		}
		if u, spent := v.used[te.Index]; spent && (eff.Kind == EffDoorOpen || eff.Kind == EffMoverAt) {
			v.errorf("effect %s: %v was already %s by %s and does not move again", eff.Kind, te, u.how, u.by)
		}
		v.checkConditions(r)
		v.checkChain(e.Index, reach, r)
	}
	return reach
}

// claimedEarlier checks effects that earlier steps must have caused.
func (v *validator) claimedEarlier(effs []Effect) {
	all := make([]mapdata.Reached, 0, len(v.reached))
	for _, r := range v.reached {
		all = append(all, r)
	}
	for _, eff := range effs {
		te, err := Resolve(eff.Target, v.m)
		if err != nil {
			v.errorf("effect %s: %v", eff.Kind, err)
			continue
		}
		if _, ok := v.find(all, eff, te); !ok {
			v.errorf("no earlier step causes %s of %v", eff.Kind, te)
		}
	}
}

// find looks for the reach record that realizes effect eff on te.
func (v *validator) find(reach []mapdata.Reached, eff Effect, te *mapdata.Entity) (mapdata.Reached, bool) {
	m := v.m
	for _, r := range reach {
		if r.Entity != te.Index {
			continue
		}
		mv := m.Mover(te.Index)
		ok := false
		switch eff.Kind {
		case EffExit:
			ok = r.Response == mapdata.RespExit
		case EffLaserOff, EffLaserOn:
			if l := m.Laser(te.Index); l != nil && r.Response == mapdata.RespToggle {
				ok = l.StartOn == (eff.Kind == EffLaserOff)
			}
		case EffDoorOpen:
			ok = mv != nil && r.Response == mapdata.RespMove &&
				(mv.Kind == mapdata.MoverDoor || mv.Kind == mapdata.MoverDoorRotating || mv.Kind == mapdata.MoverDoorSecret)
			if ok && mv.StartOpen {
				// the spawn swapped the poses: door_go_up moves it to Pos2,
				// its closed pose
				v.errorf("effect doorOpen: %v starts open, so the use closes it (claim moverAt pos2)", te)
			}
		case EffMoverAt:
			ok = mv != nil && r.Response == mapdata.RespMove
			switch {
			case !ok:
			case !validPose(mv, eff.Pose):
				v.errorf("effect moverAt: %v has no pose %q (%s)", te, eff.Pose, poseNames(mv))
			default:
				if to := usePoses(m, mv); !slices.Contains(to, canonicalPose(mv, eff.Pose)) {
					v.errorf("effect moverAt: %v moves to %s when used, not %q", te, listOrNone(to), eff.Pose)
				}
			}
		case EffEnable:
			ok = r.Via.Kind == mapdata.LinkTarget && (r.Response == mapdata.RespEnable || r.Response == mapdata.RespSpawn)
		case EffRemove:
			ok = r.Removed
		case EffWake:
			ok = strings.HasPrefix(te.Classname, "monster_") && (r.Response == mapdata.RespWake || r.Response == mapdata.RespSpawn)
		case EffUse:
			ok = !r.Removed && !r.Disabled
		default:
			v.errorf("unknown effect kind %q", eff.Kind)
			return r, true
		}
		if ok {
			return r, true
		}
	}
	return mapdata.Reached{}, false
}

// checkConditions checks what a reached effect needs on the way: keys
// picked up earlier, and no trigger_counter.
func (v *validator) checkConditions(r mapdata.Reached) {
	for _, it := range r.Requires {
		if !v.inventory[it] {
			v.errorf("the chain to #%d passes a trigger_key that needs %s, which no earlier step picks up", r.Entity, it)
		}
	}
	if r.Counter {
		v.errorf("the chain to #%d passes a trigger_counter (it needs several uses)", r.Entity)
	}
}

// checkChain reports a used-up entity on the chain from start to r: a
// removed relay or trigger no longer exists, a spent button or door no
// longer fires its targets.
func (v *validator) checkChain(start int, reach []mapdata.Reached, r mapdata.Reached) {
	if len(v.used) == 0 {
		return
	}
	by := make(map[int]mapdata.Reached, len(reach))
	for _, x := range reach {
		by[x.Entity] = x
	}
	for from := r.Via.From; from != start; {
		if u, ok := v.used[from]; ok {
			v.errorf("the chain to %v passes %v, which was %s by %s", v.m.Entity(r.Entity), v.m.Entity(from), u.how, u.by)
			return
		}
		p, ok := by[from]
		if !ok {
			return
		}
		from = p.Via.From
	}
}

// checkActivate reports a step activating an entity that was used up
// before.
func (v *validator) checkActivate(e *mapdata.Entity) {
	if u, ok := v.used[e.Index]; ok {
		v.errorf("%v was already %s by %s", e, u.how, u.by)
	}
}

// checkExists reports a reference to an entity that was removed before;
// it returns false then.
func (v *validator) checkExists(e *mapdata.Entity, what string) bool {
	if u, ok := v.used[e.Index]; ok && u.gone {
		v.errorf("%s: %v was already %s by %s", what, e, u.how, u.by)
		return false
	}
	return true
}

// consume records that the current step uses entity i up.
func (v *validator) consume(i int, gone bool, how string) {
	if u, ok := v.pending[i]; ok && (u.gone || !gone) {
		return
	}
	v.pending[i] = usage{gone: gone, how: how, by: fmt.Sprintf("%s step %d", v.t.Name, v.step)}
}

// consumeReached records the single-use entities a chain uses up: a
// killtarget is removed, a trigger_once (or wait -1 trigger_multiple) used
// or ridden into frees itself, a used func_explosive explodes, a wait -1
// button or non-toggle door stays at its end pose for good.
func (v *validator) consumeReached(r mapdata.Reached) {
	switch {
	case r.Removed:
		v.consume(r.Entity, true, "removed")
	case r.Disabled:
	case r.Response == mapdata.RespExplode:
		v.consume(r.Entity, true, "destroyed")
	case r.Response == mapdata.RespRelay:
		if tr := v.m.Trigger(r.Entity); tr != nil && tr.Wait < 0 &&
			(tr.Classname == "trigger_once" || tr.Classname == "trigger_multiple") {
			v.consume(r.Entity, true, "fired")
		}
	case r.Response == mapdata.RespMove:
		mv := v.m.Mover(r.Entity)
		if mv == nil || mv.Wait >= 0 || mv.Toggle {
			break
		}
		switch mv.Kind {
		case mapdata.MoverButton:
			v.consume(r.Entity, false, "pressed")
		case mapdata.MoverDoor, mapdata.MoverDoorRotating:
			v.consume(r.Entity, false, "moved")
		}
	}
}

func (v *validator) monster(s *Step) *mapdata.Monster {
	if s.Class == "" {
		v.errorf("kill needs a class")
		return nil
	}
	var te *mapdata.Entity
	if s.Target != nil {
		if te = v.resolve(s.Target, "kill"); te == nil {
			return nil
		}
	}
	if te == nil && s.Pos == nil {
		v.errorf("kill needs pos (the spawn origin) or target")
		return nil
	}
	var near []string
	for k := range v.m.Monsters {
		mo := &v.m.Monsters[k]
		if mo.Classname != s.Class {
			continue
		}
		if te != nil && mo.Entity != te.Index {
			continue
		}
		if s.Pos != nil && dist(mo.Origin, *s.Pos) > 1 {
			near = append(near, fmt.Sprintf("#%d at %v", mo.Entity, mo.Origin))
			continue
		}
		return mo
	}
	switch {
	case te == nil:
		v.errorf("no present %s spawned at %v (there are %s)", s.Class, *s.Pos, listOrNone(near))
	case s.Pos == nil:
		v.errorf("%v is not a present %s", te, s.Class)
	default:
		v.errorf("%v is not a present %s spawned at %v", te, s.Class, *s.Pos)
	}
	return nil
}

func (v *validator) item(s *Step) *mapdata.Item {
	if s.Class == "" {
		v.errorf("pickup needs a class")
		return nil
	}
	var te *mapdata.Entity
	if s.Target != nil {
		if te = v.resolve(s.Target, "pickup"); te == nil {
			return nil
		}
	}
	var near []string
	for k := range v.m.Items {
		it := &v.m.Items[k]
		if it.Classname != s.Class || (te != nil && it.Entity != te.Index) {
			continue
		}
		if s.Pos != nil && dist(it.Origin, *s.Pos) > 1 {
			near = append(near, fmt.Sprintf("#%d at %v", it.Entity, it.Origin))
			continue
		}
		return it
	}
	v.errorf("no present %s to pick up (there are %s)", s.Class, listOrNone(near))
	return nil
}

// describe lists what a chain reaches, for error messages.
func (v *validator) describe(reach []mapdata.Reached) string {
	if len(reach) == 0 {
		return "nothing"
	}
	var parts []string
	for _, r := range reach {
		e := v.m.Entity(r.Entity)
		what := r.Response.String()
		switch {
		case r.Removed:
			what = "removed"
		case r.Disabled:
			what = "disabled"
		}
		parts = append(parts, fmt.Sprintf("%v [%s]", e, what))
	}
	sort.Strings(parts)
	return strings.Join(parts, ", ")
}

// ValidateCampaign validates every table of the campaign with the map data
// load returns, plus the campaign as a whole: the first visit starts the
// first map without a spawnpoint, every exit leads to the next visit's map
// and arrival spawnpoint, the last exit is the terminal one, visit indexes
// count earlier visits of the same map, and a revisit (which restores the
// level as it was left) neither activates nor relies on entities an
// earlier visit used up, nor refers to ones it removed.
func ValidateCampaign(c *Campaign, load func(name string) (*mapdata.Map, error)) error {
	var problems []Problem
	add := func(table, where, format string, args ...any) {
		problems = append(problems, Problem{Table: table, Where: where, Msg: fmt.Sprintf(format, args...)})
	}
	if len(c.Tables) == 0 {
		add("", "campaign", "no visits")
		return &Error{Problems: problems}
	}
	if len(c.Tables) != len(c.Visits) {
		add("", "campaign", "%d tables loaded for %d visits", len(c.Tables), len(c.Visits))
	}
	if !validKind(c.Terminal.Kind) {
		add("", "campaign", "unknown terminal kind %q", c.Terminal.Kind)
	}
	if k := mapdata.ParseLevelString(c.Terminal.Exit).Kind.String(); k != c.Terminal.Kind {
		add("", "campaign", "terminal exit %q is a %s, not a %s", c.Terminal.Exit, k, c.Terminal.Kind)
	}

	seen := map[string]int{}
	used := map[string]map[int]usage{} // map -> what its visits used up so far
	for i, t := range c.Tables {
		if i < len(c.Visits) {
			if stem := strings.TrimSuffix(filepath.Base(c.Visits[i]), filepath.Ext(c.Visits[i])); t.Name != stem {
				add(t.Name, "table", "name %q differs from its file %s", t.Name, c.Visits[i])
			}
		}
		key := strings.ToLower(t.Map)
		if t.Visit != seen[key] {
			add(t.Name, "table", "visit %d, but it is visit %d of %s in the campaign", t.Visit, seen[key], t.Map)
		}
		seen[key]++
		if i == 0 {
			if !strings.EqualFold(t.Map, c.Start) || t.From != "" {
				add(t.Name, "table", "the first visit must start %s without a spawnpoint (has %s$%s)", c.Start, t.Map, t.From)
			}
		} else {
			prev := c.Tables[i-1]
			ls := mapdata.ParseLevelString(prev.Exit.Map)
			if ls.Kind != mapdata.ExitLevel || !strings.EqualFold(ls.Map, t.Map) || ls.Spawnpoint != t.From {
				add(t.Name, "table", "previous visit %s exits to %q, but this visit arrives at %s$%s",
					prev.Name, prev.Exit.Map, t.Map, t.From)
			}
		}
		if i == len(c.Tables)-1 && t.Exit.Map != c.Terminal.Exit {
			add(t.Name, "table", "the last visit exits to %q, the campaign ends at %q", t.Exit.Map, c.Terminal.Exit)
		}

		m, err := load(t.Map)
		if err != nil {
			add(t.Name, "table", "loading %s: %v", t.Map, err)
			continue
		}
		// a revisit restores the level as it was left (SV_ReadLevelFile)
		v := newValidator(t, m, used[key])
		v.run()
		problems = append(problems, v.problems...)
		if used[key] == nil {
			used[key] = map[int]usage{}
		}
		for e, u := range v.consumed {
			if old, ok := used[key][e]; !ok || u.gone && !old.gone {
				used[key][e] = u
			}
		}
	}
	if len(problems) > 0 {
		return &Error{Problems: problems}
	}
	return nil
}

func validKind(k string) bool {
	switch k {
	case "level", "cin", "demo", "pic":
		return true
	}
	return false
}

// validPose reports whether pose names a rest pose of the mover ("" is
// "pos2").
func validPose(mv *mapdata.Mover, pose string) bool {
	switch pose {
	case "", "pos1", "pos2":
		return mv.Kind != mapdata.MoverTrain
	case "top", "bottom":
		return mv.Kind == mapdata.MoverPlat
	}
	if mv.Kind == mapdata.MoverTrain {
		for _, p := range mv.Path {
			if shared.Q_stricmp(p.Targetname, pose) == 0 {
				return true
			}
		}
	}
	return false
}

// canonicalPose maps the pose aliases to Pos1 / Pos2: "" means "pos2", a
// plat's "top" is "pos1" and its "bottom" "pos2". Train corners stay as
// they are.
func canonicalPose(mv *mapdata.Mover, pose string) string {
	switch {
	case mv.Kind == mapdata.MoverTrain:
		return pose
	case pose == "", pose == "bottom":
		return "pos2"
	case pose == "top":
		return "pos1"
	}
	return pose
}

// usePoses lists the poses (canonical names) a use sends the mover to from
// its spawn state: door_go_up and button_fire move a door, rotating door,
// water or button to Pos2 (a START_OPEN door's closed pose); a secret door
// slides to Pos1 and on to Pos2; Use_Plat sends a waiting plat down to
// Pos2; train_use runs a train from its first corner along its path,
// stopping at the corners on the way and staying at the first one with
// wait -1 (a START_ON train ignores the use, or stops for a TOGGLE one).
// C: game/g_func.c:1633 train_use
func usePoses(m *mapdata.Map, mv *mapdata.Mover) []string {
	switch mv.Kind {
	case mapdata.MoverDoor, mapdata.MoverDoorRotating, mapdata.MoverButton, mapdata.MoverPlat:
		return []string{"pos2"}
	case mapdata.MoverDoorSecret:
		return []string{"pos1", "pos2"}
	case mapdata.MoverTrain:
		if mv.Activation&mapdata.ActAuto != 0 || len(mv.Path) < 2 {
			return nil
		}
		// the corner index train_next goes to after corner k
		next := func(k int) int {
			if k+1 < len(mv.Path) {
				return k + 1
			}
			// the path closes on a corner already on it (spTrain stops there)
			if ts := m.Targets(m.Entity(mv.Path[k].Corner).Target); len(ts) > 0 {
				for j, p := range mv.Path {
					if p.Corner == ts[0].Index {
						return j
					}
				}
			}
			return -1
		}
		var out []string
		for k, n := 1, 0; k >= 0 && n < len(mv.Path); k, n = next(k), n+1 {
			p := mv.Path[k]
			if p.Teleport {
				continue // train_next jumps through it to the next corner
			}
			out = append(out, p.Targetname)
			if p.Wait < 0 {
				break
			}
		}
		return out
	}
	return nil
}

func poseNames(mv *mapdata.Mover) string {
	switch mv.Kind {
	case mapdata.MoverPlat:
		return "poses: pos1/top, pos2/bottom"
	case mapdata.MoverTrain:
		var names []string
		for _, p := range mv.Path {
			names = append(names, p.Targetname)
		}
		return "corners: " + strings.Join(names, ", ")
	}
	return "poses: pos1, pos2"
}

// forward is the AngleVectors forward vector of a yaw in degrees.
func forward(yaw float32) mapdata.Vec3 {
	var f mapdata.Vec3
	shared.AngleVectors(mapdata.Vec3{0, yaw, 0}, &f, nil, nil)
	return f
}

func dist(a mapdata.Vec3, b Vec) float64 {
	dx, dy, dz := float64(a[0]-b[0]), float64(a[1]-b[1]), float64(a[2]-b[2])
	return math.Sqrt(dx*dx + dy*dy + dz*dz)
}

func listOrNone(s []string) string {
	if len(s) == 0 {
		return "none"
	}
	return strings.Join(s, ", ")
}
