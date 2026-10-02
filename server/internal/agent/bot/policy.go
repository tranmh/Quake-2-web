package bot

import (
	"quake2web/server/internal/agent/decide"
	"quake2web/server/internal/agent/worldmodel"
)

// Policy is the bot's decision layer: once per server frame it turns the
// belief and the route's objective into an Intent (mode, target, fire
// policy, movement, weapon, pickup) that the bot executes with its
// reflexes until the next frame. *decide.Pipeline implements it (wave 5);
// ObjectivePolicy is the phase-4 policy.
type Policy interface {
	Tick(now int64, b *worldmodel.Belief, obj *decide.ObjectiveView) decide.Intent
}

// ObjectivePolicy always pursues the objective: the bot fights only when
// the route's current step is a kill, with the trigger held while the
// target is aligned.
type ObjectivePolicy struct{}

// Tick implements Policy.
func (ObjectivePolicy) Tick(now int64, _ *worldmodel.Belief, _ *decide.ObjectiveView) decide.Intent {
	return decide.Intent{Time: now, Mode: decide.ModeObjective, FirePolicy: decide.FireWhenAligned, Movement: decide.MoveHold}
}
