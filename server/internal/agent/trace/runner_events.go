package trace

// Event types the runner (package agent/runner) publishes besides the
// campaign's and the decision layer's. They are additive to the schema
// (q2bot.trace/1): consumers that do not know them skip them.
const (
	// TypeCmds carries the usercmds the bot sent during one session step
	// (one server frame): the input of an actions-mode replay.
	TypeCmds = "cmds"
	// TypeProvenance carries the per-tick provenance of the decision
	// fields over an episode: where the value the bot acted on came from
	// at every decision tick (its lane tick events' Intent.Fields, reflex
	// overrides included), with the decision layer's own counts before
	// the bot's overrides (decide.ArbiterStats Ticks) for a cross-check.
	// The request-level decision events alone cannot tell it (a held or
	// fallback value has no request of its own).
	TypeProvenance = "provenance"
)

// Cmds is the body of cmds: the usercmds of one session step, in the
// order they were sent (four of 25 ms while the client is active; steps
// without commands are not recorded).
type Cmds struct {
	// Step is the step's index in the episode, from 0 (session Steps of
	// the campaign loop; the frames a reload waits for are not steps).
	Step int64     `json:"step"`
	Cmds []StepCmd `json:"cmds"`
}

// StepCmd is one usercmd as sent, complete: the UserCmd fields and the
// light level the client reports (usercmd_t lightlevel, which the game's
// monsters read to tell whether they can see the player).
type StepCmd struct {
	UserCmd
	Light uint8 `json:"light,omitempty"`
}

// Provenance is the body of provenance: the tick counts of every decision
// field by source over the episode (Ep of the envelope).
type Provenance struct {
	// Ticks is the number of decision ticks (Intents acted on); Fields
	// count them by the source of the value acted on, as the bot's lane
	// tick events report it (a field the bot's reflexes or route
	// overrode is reflex). Without tick reports (a bot that emits none)
	// they are the decision layer's counts, Arbiter's.
	Ticks  int         `json:"ticks"`
	Fields []TickField `json:"fields"`
	// ArbiterTicks and Arbiter are the decision layer's counts of the
	// same ticks before the bot's overrides (never reflex): for every
	// field and every other source the acted-on count is at most the
	// arbiter's. Absent from recordings made before they existed.
	ArbiterTicks int         `json:"arbiter_ticks,omitempty"`
	Arbiter      []TickField `json:"arbiter,omitempty"`
}

// TickField counts the ticks of one decision field by the source of the
// value acted on (Source* names): default (nothing decided it), model,
// scripted (the fallback or the scripted backend), stale (a model answer
// held past its TTL) and reflex (a reflex overrode it).
type TickField struct {
	Name     string `json:"name"`
	Default  int    `json:"default"`
	Model    int    `json:"model"`
	Scripted int    `json:"scripted"`
	Stale    int    `json:"stale"`
	Reflex   int    `json:"reflex,omitempty"`
}

// Count adds n ticks of source (a Source* name) to the field; a source it
// does not know counts as default, so the field still counts every tick.
func (f *TickField) Count(source string, n int) {
	switch source {
	case SourceModel:
		f.Model += n
	case SourceScripted:
		f.Scripted += n
	case SourceStale:
		f.Stale += n
	case SourceReflex:
		f.Reflex += n
	default:
		f.Default += n
	}
}

// Total is the ticks the field counts.
func (f TickField) Total() int { return f.Default + f.Model + f.Scripted + f.Stale + f.Reflex }
