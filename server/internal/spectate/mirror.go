package spectate

import (
	"encoding/binary"

	"quake2web/server/internal/fakeclient"
	"quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/msg"
	"quake2web/server/internal/qcommon/shared"
)

// LevelSnapshot is the mirrored startup state of one of the bot's level
// generations: what svc_serverdata carried and the configstrings and
// baselines the bot holds. A LevelSnapshot is immutable; the Mirror makes a
// new one (sharing the unchanged arrays) whenever something changes.
type LevelSnapshot struct {
	// Gen is the bot client's level generation (fakeclient LevelGen).
	Gen int

	// The svc_serverdata fields as the bot received them (the protocol is
	// always PROTOCOL_VERSION: the bot drops any other).
	ServerCount int32
	AttractLoop int32 // the bot's; viewers are always sent 1
	GameDir     string
	PlayerNum   int32 // -1 on a cinematic or picture level
	LevelName   string

	// Ready reports that the snapshot is complete: on a game level the bot
	// parsed its first valid frame of the generation (so it had every
	// configstring and baseline of the handshake), on a cinematic or picture
	// level (PlayerNum -1) at once.
	Ready bool

	cs   *[q2const.MAX_CONFIGSTRINGS]string
	base *baselines
}

// baselines are the entity baselines the bot received (Present) and their
// states.
type baselines struct {
	State   [q2const.MAX_EDICTS]shared.EntityState
	Present [q2const.MAX_EDICTS]bool
}

// Game reports whether the level is a game level (it has a player and
// frames), not a cinematic or picture.
func (l *LevelSnapshot) Game() bool { return l.PlayerNum >= 0 }

// ConfigString returns configstring i ("" when unset or out of range).
func (l *LevelSnapshot) ConfigString(i int) string {
	if i < 0 || i >= q2const.MAX_CONFIGSTRINGS {
		return ""
	}
	return l.cs[i]
}

// Baseline returns the baseline of entity n and whether the bot received
// one.
func (l *LevelSnapshot) Baseline(n int) (shared.EntityState, bool) {
	if n < 0 || n >= q2const.MAX_EDICTS {
		return shared.EntityState{}, false
	}
	return l.base.State[n], l.base.Present[n]
}

// FrameSnapshot is a copy of one valid frame the bot parsed: everything a
// keyframe needs. It is immutable.
type FrameSnapshot struct {
	Gen           int // level generation the frame belongs to
	ServerFrame   int32
	DeltaFrame    int32 // what the server delta-coded it from (-1: uncompressed)
	SurpressCount int32
	AreaBytes     int
	AreaBits      [q2const.MAX_MAP_AREAS / 8]byte
	PlayerState   shared.PlayerState
	Entities      []shared.EntityState // in frame order (ascending entity number)
}

// Message is one server message of the bot together with the mirrored state
// right after it. It is immutable and shared by every hub.
type Message struct {
	Seq     uint64            // 1, 2, ... per Stream
	Payload []byte            // the message without its netchan header
	Spans   []fakeclient.Span // the svc commands of Payload

	// Level is the bot's level after the message (nil before the first
	// svc_serverdata) and Frame its latest valid frame (nil while the
	// generation has none yet, possibly from an earlier message).
	Level *LevelSnapshot
	Frame *FrameSnapshot

	// FrameSpan is the index in Spans of the svc_frame this message
	// carried (the last one if there were several), -1 for none.
	// FrameValid reports whether the bot could use it: a frame delta-coded
	// from a frame the bot no longer had is not valid (the bot asks for an
	// uncompressed one) and is never forwarded.
	FrameSpan  int
	FrameValid bool
}

// Mirror maintains the LevelSnapshot and the latest frame of a bot client
// from its server messages. Update runs in the bot's goroutine right after
// the client parsed a message (fakeclient OnServerMessage) and reads the
// client's parsed state: a new generation is copied once in full, later
// configstring and baseline changes are located by the message's spans.
// A Mirror is not safe for concurrent use (Stream serializes it); the
// snapshots it returns are.
type Mirror struct {
	level *LevelSnapshot
	frame *FrameSnapshot
}

// Level returns the current level snapshot (nil before any serverdata).
func (mr *Mirror) Level() *LevelSnapshot { return mr.level }

// Frame returns the latest valid frame of the current level (nil if none).
func (mr *Mirror) Frame() *FrameSnapshot { return mr.frame }

// Update mirrors one message the client just parsed and returns it with the
// resulting state. payload and spans are retained (fakeclient hands fresh
// copies to OnServerMessage).
func (mr *Mirror) Update(c *fakeclient.Client, payload []byte, spans []fakeclient.Span) *Message {
	m := &Message{Payload: payload, Spans: spans, FrameSpan: -1}
	for i, sp := range spans {
		if sp.Cmd == q2const.Svc_frame {
			m.FrameSpan = i
		}
	}

	gen := c.LevelGen()
	switch lvl := mr.level; {
	case gen == 0:
		// no serverdata yet: nothing to mirror
	case lvl == nil || lvl.Gen != gen:
		mr.level = levelFromClient(c)
		mr.frame = nil
	default:
		mr.level = lvl.apply(c, payload, spans)
	}

	if m.FrameSpan >= 0 && mr.level != nil && c.Frame.Valid {
		m.FrameValid = true
		mr.frame = frameFromClient(c, gen)
	}
	if l := mr.level; l != nil && !l.Ready && (!l.Game() || m.FrameValid) {
		cp := *l
		cp.Ready = true
		mr.level = &cp
	}
	m.Level, m.Frame = mr.level, mr.frame
	return m
}

// levelFromClient copies the client's whole level state (a new generation).
func levelFromClient(c *fakeclient.Client) *LevelSnapshot {
	l := &LevelSnapshot{
		Gen:         c.LevelGen(),
		ServerCount: c.ServerData.ServerCount,
		AttractLoop: c.ServerData.AttractLoop,
		GameDir:     c.ServerData.GameDir,
		PlayerNum:   c.ServerData.PlayerNum,
		LevelName:   c.ServerData.LevelName,
		cs:          new([q2const.MAX_CONFIGSTRINGS]string),
		base:        new(baselines),
	}
	*l.cs = c.ConfigStrings
	for i := range c.Entities {
		if c.Entities[i].HasBaseline {
			l.base.State[i] = c.Entities[i].Baseline
			l.base.Present[i] = true
		}
	}
	return l
}

// apply returns the snapshot updated with the configstrings and baselines
// the message set (l itself when it set none).
func (l *LevelSnapshot) apply(c *fakeclient.Client, payload []byte, spans []fakeclient.Span) *LevelSnapshot {
	out := l
	for _, sp := range spans {
		switch sp.Cmd {
		case q2const.Svc_configstring:
			i, ok := configStringIndex(payload, sp)
			if !ok || out.cs[i] == c.ConfigStrings[i] {
				continue
			}
			if out == l {
				cp := *l
				out = &cp
			}
			if out.cs == l.cs {
				out.cs = new([q2const.MAX_CONFIGSTRINGS]string)
				*out.cs = *l.cs
			}
			out.cs[i] = c.ConfigStrings[i]
		case q2const.Svc_spawnbaseline:
			n, ok := baselineNumber(payload, sp)
			if !ok {
				continue
			}
			e := &c.Entities[n]
			if out.base.Present[n] == e.HasBaseline && out.base.State[n] == e.Baseline {
				continue
			}
			if out == l {
				cp := *l
				out = &cp
			}
			if out.base == l.base {
				out.base = new(baselines)
				*out.base = *l.base
			}
			out.base.State[n] = e.Baseline
			out.base.Present[n] = e.HasBaseline
		}
	}
	return out
}

// frameFromClient copies the client's current frame (c.Frame).
func frameFromClient(c *fakeclient.Client, gen int) *FrameSnapshot {
	f := &c.Frame
	return &FrameSnapshot{
		Gen:           gen,
		ServerFrame:   f.ServerFrame,
		DeltaFrame:    f.DeltaFrame,
		SurpressCount: f.SurpressCount,
		AreaBytes:     f.AreaBytes,
		AreaBits:      f.AreaBits,
		PlayerState:   f.PlayerState,
		Entities:      c.FrameEntities(f),
	}
}

// configStringIndex reads the index of a svc_configstring span.
func configStringIndex(payload []byte, sp fakeclient.Span) (int, bool) {
	if sp.Start < 0 || sp.Start+3 > sp.End || sp.End > len(payload) {
		return 0, false
	}
	i := int(int16(binary.LittleEndian.Uint16(payload[sp.Start+1:])))
	if i < 0 || i >= q2const.MAX_CONFIGSTRINGS {
		return 0, false
	}
	return i, true
}

// baselineNumber reads the entity number of a svc_spawnbaseline span
// (CL_ParseEntityBits).
func baselineNumber(payload []byte, sp fakeclient.Span) (int, bool) {
	if sp.Start < 0 || sp.Start+2 > sp.End || sp.End > len(payload) {
		return 0, false
	}
	m := msg.NewReader(payload[sp.Start+1 : sp.End])
	n, _ := m.ParseEntityBits()
	if m.ReadCount > m.CurSize || n < 0 || n >= q2const.MAX_EDICTS {
		return 0, false
	}
	return int(n), true
}
