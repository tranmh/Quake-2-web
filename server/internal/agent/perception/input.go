package perception

import (
	"quake2web/server/internal/fakeclient"
	"quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/shared"
)

// Vec3 is the game's vec3_t.
type Vec3 = shared.Vec3

// ConfigStrings is a copy of the client's configstrings. FrameInputs of a
// Reader share one copy until a configstring changes; a copy is never
// modified after it was handed out.
type ConfigStrings [q2const.MAX_CONFIGSTRINGS]string

// LevelStatic is what the client received once for the current level
// (svc_serverdata and the baselines). It is shared by all FrameInputs of the
// level and never modified.
type LevelStatic struct {
	// Gen is the client's level generation (fakeclient LevelGen): it changes
	// exactly when a new level, load or reconnect began.
	Gen         int
	MapName     string // fakeclient MapName ("demo1", "victory.pcx")
	PlayerNum   int32  // svc_serverdata playernum; the own entity is PlayerNum+1
	ServerCount int32
	// Baselines are the svc_spawnbaseline states, in entity number order.
	// They carry each entity's spawn state (model and origin), which is map
	// knowledge: the entity lump has the same information. The world model
	// uses them only to tie lump entities to entity numbers.
	Baselines []shared.EntityState
}

// Events are the server events that arrived since the previous FrameInput,
// in arrival order. Lost counts events the client's MaxHistory discarded
// before they could be read (0 unless the reader fell far behind).
type Events struct {
	Prints        []string
	CenterPrints  []string
	Layouts       []string
	Sounds        []fakeclient.Sound
	TempEnts      []fakeclient.TempEnt
	MuzzleFlashes []fakeclient.MuzzleFlash
	Lost          uint64
}

// FrameInput is everything the client received for one server frame. It is
// a self-contained value (no pointers into the client): Entities, Events and
// the arrays are copies, Level and CS are shared immutable snapshots.
type FrameInput struct {
	Level *LevelStatic
	CS    *ConfigStrings

	ServerFrame int32
	ServerTime  int32 // ms; ServerFrame*100
	PlayerState shared.PlayerState
	AreaBits    [q2const.MAX_MAP_AREAS / 8]byte
	AreaBytes   int

	// Entities are the frame's packet entities in entity number order, the
	// player's own entity included. The server already dropped what is
	// outside the PVS; what remains is NOT what the player sees: that is
	// the Perceiver's job.
	Entities []shared.EntityState

	Events Events

	// Inventory is the last svc_inventory the client parsed and
	// InventorySeq the number of svc_inventory messages so far
	// (fakeclient Counts.Inventory): a change means a fresh inventory.
	Inventory    [q2const.MAX_ITEMS]int32
	InventorySeq uint64
}

// OwnEntity returns the entity number of the player (PlayerNum+1).
func (in *FrameInput) OwnEntity() int32 {
	if in.Level == nil {
		return 0
	}
	return in.Level.PlayerNum + 1
}

// ConfigString returns configstring i ("" when out of range or unset).
func (in *FrameInput) ConfigString(i int) string {
	if in.CS == nil || i < 0 || i >= len(in.CS) {
		return ""
	}
	return in.CS[i]
}

// Reader builds FrameInputs from a fakeclient.Client. Call Next after every
// batch of datagrams (or payloads) handed to the client; it reports each new
// valid server frame once. A Reader is not safe for concurrent use.
type Reader struct {
	seen      fakeclient.HistoryCounts
	lastGen   int
	lastFrame int32
	started   bool
	level     *LevelStatic
	cs        *ConfigStrings
}

// NewReader returns a Reader that reports the client's next valid frame.
// Events recorded before the reader's first frame are included in it.
func NewReader() *Reader { return &Reader{} }

// Next returns the FrameInput of the client's latest frame when that frame
// is valid, the client is active and the frame was not reported before
// (ok false otherwise). Events that arrived since the previous reported
// frame, even across a level change, belong to the returned input.
func (r *Reader) Next(c *fakeclient.Client) (in FrameInput, ok bool) {
	if c.State != fakeclient.CaActive || !c.Frame.Valid {
		return FrameInput{}, false
	}
	gen := c.LevelGen()
	if r.started && gen == r.lastGen && c.Frame.ServerFrame == r.lastFrame {
		return FrameInput{}, false
	}
	if r.level == nil || r.level.Gen != gen {
		r.level = levelStatic(c, gen)
	}
	if r.cs == nil || *r.cs != c.ConfigStrings {
		cs := ConfigStrings(c.ConfigStrings)
		r.cs = &cs
	}
	r.started, r.lastGen, r.lastFrame = true, gen, c.Frame.ServerFrame

	in = FrameInput{
		Level:        r.level,
		CS:           r.cs,
		ServerFrame:  c.Frame.ServerFrame,
		ServerTime:   c.Frame.ServerTime,
		PlayerState:  c.Frame.PlayerState,
		AreaBits:     c.Frame.AreaBits,
		AreaBytes:    c.Frame.AreaBytes,
		Entities:     c.FrameEntities(&c.Frame),
		Inventory:    c.Inventory,
		InventorySeq: c.Counts.Inventory,
	}
	in.Events = r.events(c)
	return in, true
}

// events copies the history entries recorded since the previous call.
func (r *Reader) events(c *fakeclient.Client) Events {
	var ev Events
	ev.Prints = fresh(c.Prints, c.Counts.Prints, &r.seen.Prints, &ev.Lost)
	ev.CenterPrints = fresh(c.CenterPrints, c.Counts.CenterPrints, &r.seen.CenterPrints, &ev.Lost)
	ev.Layouts = fresh(c.Layouts, c.Counts.Layouts, &r.seen.Layouts, &ev.Lost)
	ev.Sounds = fresh(c.Sounds, c.Counts.Sounds, &r.seen.Sounds, &ev.Lost)
	ev.TempEnts = fresh(c.TempEntEvents, c.Counts.TempEntEvents, &r.seen.TempEntEvents, &ev.Lost)
	ev.MuzzleFlashes = fresh(c.MuzzleFlashes, c.Counts.MuzzleFlashes, &r.seen.MuzzleFlashes, &ev.Lost)
	return ev
}

// fresh returns a copy of the entries of history s after *seen and advances
// *seen to total (the client trims its histories in place, so the entries
// must be copied out).
func fresh[T any](s []T, total uint64, seen *uint64, lost *uint64) []T {
	n, l := fakeclient.NewSince(s, total, *seen)
	*seen = total
	*lost += l
	if len(n) == 0 {
		return nil
	}
	return append([]T(nil), n...)
}

func levelStatic(c *fakeclient.Client, gen int) *LevelStatic {
	ls := &LevelStatic{
		Gen:         gen,
		MapName:     c.MapName(),
		PlayerNum:   c.ServerData.PlayerNum,
		ServerCount: c.ServerData.ServerCount,
	}
	for i := range c.Entities {
		if c.Entities[i].HasBaseline {
			b := c.Entities[i].Baseline
			b.Number = int32(i)
			ls.Baselines = append(ls.Baselines, b)
		}
	}
	return ls
}
