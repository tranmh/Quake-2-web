package decide

// TickInfo is what one Pipeline.Tick did besides its Intent: the fast lane
// state it projected (every tick, for the trace's state digest), the
// requests it built (by Seq, submitted or dropped) and the results it
// collected and applied. It is the input of the bot's per-tick decision
// trace event.
type TickInfo struct {
	Fast     State
	Requests []uint64
	Records  []*Record
}

// LastTick returns what the last Tick did (the zero TickInfo before the
// first one, and after a Tick on a nil belief). The slices are the
// pipeline's: valid until the next Tick.
func (p *Pipeline) LastTick() TickInfo { return p.tick }

// SetProbes replaces the projector's level-specific static knowledge: the
// SpaceProbe of the level's collision model and the PathFunc over its nav
// graph (nil: none). A bot calls it at every level entry; it must not run
// during a Tick.
func (p *Pipeline) SetProbes(space SpaceProbe, path PathFunc) { p.proj.SetProbes(space, path) }

// SetProbes replaces the projector's SpaceProbe and PathFunc (nil: none:
// no space field, straight-line item distances).
func (p *Projector) SetProbes(space SpaceProbe, path PathFunc) {
	p.cfg.Space, p.cfg.Path = space, path
}
