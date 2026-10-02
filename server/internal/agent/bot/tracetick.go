package bot

import (
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
		Health: bel.Self.Health, Reflexes: b.fight.reflexes, Requests: append([]uint64(nil), info.Requests...)}
	switch {
	case b.mode == ModeFight && b.fight.goal == goalNone:
		tk.Move = b.fight.move.String()
	case b.mode == ModeFight || b.mode == ModeRetreat:
		tk.Move = "nav"
	}
	d.Tick = tk
	d.Cmds = b.pending
	b.pending, b.fight.reflexes = nil, nil
	b.cfg.OnDecision(&d)
}

// traceIntent is the tick's Intent with the provenance of every field,
// the bot's overrides marked as reflex.
func (b *Bot) traceIntent() *trace.Intent {
	in := &b.intent
	ti := &trace.Intent{Mode: string(in.Mode), Target: in.Target, FirePolicy: string(in.FirePolicy), Movement: string(in.Movement),
		Weapon: string(in.Weapon), Pickup: in.Pickup, Danger: in.Danger}
	for _, f := range traceFields() {
		p := in.Provenance.Get(f)
		tf := trace.Field{Name: f.ID(), Value: in.Value(f), Source: p.Source.String(), Confidence: p.Confidence, Fallback: p.Reason}
		switch {
		case f == decide.FieldTarget && b.targetBy != "":
			tf.Value, tf.Source, tf.Fallback, tf.Confidence = b.target, trace.SourceReflex, b.targetBy, 0
		case f == decide.FieldFirePolicy && b.fireBy != "":
			tf.Value, tf.Source, tf.Fallback, tf.Confidence = string(b.firePolicy), trace.SourceReflex, b.fireBy, 0
		case f == decide.FieldWeapon && b.weaponBy != "":
			tf.Source, tf.Fallback, tf.Confidence = trace.SourceReflex, b.weaponBy, 0
			if b.weaponTo != "" {
				tf.Value = string(b.weaponTo)
			}
		}
		ti.Fields = append(ti.Fields, tf)
	}
	return ti
}
