package bot

import (
	"cmp"

	"quake2web/server/internal/agent/decide"
	"quake2web/server/internal/agent/trace"
	"quake2web/server/internal/agent/worldmodel"
)

// traceFields returns the decision fields in the order of a tick event's
// Intent.Fields.
func traceFields() [7]decide.Field {
	return [...]decide.Field{decide.FieldMode, decide.FieldTarget, decide.FieldFirePolicy, decide.FieldMovement,
		decide.FieldWeapon, decide.FieldPickup, decide.FieldDanger}
}

// traceTick emits the tick's decision events (Config.OnDecision): the
// requests the Policy collected (TraceRequests), then the tick event.
func (b *Bot) traceTick(bel *worldmodel.Belief) {
	defer func() { b.ticks++ }()
	if b.cfg.OnDecision == nil {
		return
	}
	var info decide.TickInfo
	rep, reports := b.policy.(tickReporter)
	if reports {
		info = rep.LastTick()
	}
	if b.cfg.TraceRequests {
		for _, r := range info.Records {
			d := r.Decision(decide.RecordOptions{State: b.cfg.TraceState, Questions: b.cfg.TraceState})
			b.cfg.OnDecision(&d)
		}
	}
	d := trace.Decision{Lane: trace.LaneTick, SnapGMs: b.now, Intent: b.traceIntent()}
	if reports {
		d.StateDigest, _ = trace.Digest(&info.Fast)
		if b.cfg.TraceState {
			if raw, err := decide.EncodeState(&info.Fast, 1<<20); err == nil {
				d.State = raw
			}
		}
	}
	step := -1
	if b.exec != nil {
		step = b.exec.Index()
	}
	tk := &trace.Tick{N: b.ticks, Mode: string(b.mode), Step: step, Target: b.target, Weapon: bel.Self.Weapon, Switch: b.switchCmd,
		Health: bel.Self.Health, Reflexes: b.fight.reflexes, Requests: append([]uint64(nil), info.Requests...), DroppedCmds: b.dropped}
	switch {
	case b.mode == ModeFight && b.fight.goal == goalNone:
		tk.Move = b.fight.move.String()
	case b.mode == ModeFight || b.mode == ModeRetreat:
		tk.Move = "nav"
	}
	d.Tick = tk
	d.Cmds = b.pending
	b.pending, b.dropped, b.fight.reflexes = nil, 0, nil
	b.cfg.OnDecision(&d)
}

// traceIntent is the tick's Intent with the provenance of every field:
// the value acted on, the bot's overrides marked as reflex with the
// reason, and a value the bot had no use for this tick (a target it did
// not engage, a movement outside a fight: the navigator moves it) as
// default with the reason (see trace.Intent). Neither credits the
// decision's source.
func (b *Bot) traceIntent() *trace.Intent {
	in := &b.intent
	ti := &trace.Intent{Mode: string(in.Mode), Target: in.Target, FirePolicy: string(in.FirePolicy), Movement: string(in.Movement),
		Weapon: string(in.Weapon), Pickup: in.Pickup, Danger: in.Danger}
	move := decide.Movement(b.fight.move.String())
	if b.mode != ModeFight {
		move = movedByNav
	}
	acted := decide.Intent{Mode: actedMode(b.mode), Target: b.target, FirePolicy: b.firePolicy, Movement: move, Weapon: b.weaponTo}
	for _, f := range traceFields() {
		p := in.Provenance.Get(f)
		tf := trace.Field{Name: f.ID(), Value: in.Value(f), Source: p.Source.String(), Confidence: p.Confidence, Fallback: p.Reason}
		by, unused := "", ""
		switch f {
		case decide.FieldMode:
			by = b.modeBy
		case decide.FieldTarget:
			by = b.targetBy
			if by == "" && b.target != in.Target {
				switch {
				case b.target == "" && (in.Mode != decide.ModeFight || b.modeBy == ""):
					// the intent's mode is not a fight and the target is
					// not in view to shoot back at
					unused = notEngaged
				default:
					// kept from the fight the intent asked for
					by = cmp.Or(b.modeBy, "override")
				}
			}
		case decide.FieldFirePolicy:
			by = b.fireBy
		case decide.FieldMovement:
			if b.mode == ModeFight {
				by = b.moveBy
			} else {
				unused = notFighting
			}
		case decide.FieldWeapon:
			by = b.weaponBy
			// the weapon acted on (the bot's own choice for the range when
			// no decision chose one: the source stays the default's)
			tf.Value = acted.Value(f)
		}
		switch {
		case by != "":
			tf.Value, tf.Source, tf.Fallback, tf.Confidence = acted.Value(f), trace.SourceReflex, by, 0
		case unused != "":
			tf.Value, tf.Source, tf.Fallback, tf.Confidence = acted.Value(f), trace.SourceDefault, unused, 0
		}
		ti.Fields = append(ti.Fields, tf)
	}
	return ti
}

// Reasons of a tick field the bot had no use for (source default), and
// the movement value outside a fight.
const (
	notEngaged  = "not_engaged"  // target: not fought nor shot at
	notFighting = "not_fighting" // movement: the navigator moves the bot
	movedByNav  = decide.Movement("nav")
)
