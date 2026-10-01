// Package trace is the agent's event trace: a JSON Lines stream (gzip on
// disk) of envelopes {v,type,run,ep,seq,wall,gms,lvl,map,sf} with the
// type-specific fields of the event flattened into the same object. A Bus
// stamps events and fans them out to synchronous sinks (the trace file, the
// metrics collector) and drop-oldest subscribers (live viewers).
//
// Everything but "wall" is deterministic in a lockstep run; replay
// comparisons use Comparable, which leaves it out.
package trace

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
)

// Version is the envelope version ("v").
const Version = 1

// Schema names the trace format (RunStart.Schema).
const Schema = "q2bot.trace/1"

// Event types.
const (
	TypeRunStart     = "run_start"
	TypeEpisodeStart = "episode_start"
	TypeLevelStart   = "level_start"
	TypeLevelEnd     = "level_end"
	TypeDecision     = "decision"
	TypeAPICall      = "api_call"
	TypeDamage       = "damage"
	TypeKill         = "kill"
	TypeDeath        = "death"
	TypeReload       = "reload"
	TypeStuck        = "stuck"
	TypeBudget       = "budget"
	TypeError        = "error"
	TypeEpisodeEnd   = "episode_end"
	TypeRunEnd       = "run_end"
)

// Event is one trace record. The envelope fields come first in the JSON
// object, followed by the fields of Body.
type Event struct {
	V    int    `json:"v"`
	Type string `json:"type"`
	Run  string `json:"run"`
	Ep   int    `json:"ep"`   // episode index
	Seq  uint64 `json:"seq"`  // per run, from 1 (stamped by the Bus)
	Wall int64  `json:"wall"` // wall clock, unix ms (excluded from replay comparisons)
	GMs  int64  `json:"gms"`  // session game time, ms
	Lvl  int    `json:"lvl"`  // level index in the episode (counts level_start from 0)
	Map  string `json:"map"`
	SF   int32  `json:"sf"` // the bot's latest server frame (aligns events with the video)

	// Body holds the type-specific fields (one of the body types of this
	// package, or any value that marshals to a JSON object). A decoded
	// event has a nil Body: use DecodeBody.
	Body any `json:"-"`

	raw json.RawMessage
}

// envelope is Event without its methods (for the default encoding).
type envelope struct {
	V    int    `json:"v"`
	Type string `json:"type"`
	Run  string `json:"run"`
	Ep   int    `json:"ep"`
	Seq  uint64 `json:"seq"`
	Wall int64  `json:"wall"`
	GMs  int64  `json:"gms"`
	Lvl  int    `json:"lvl"`
	Map  string `json:"map"`
	SF   int32  `json:"sf"`
}

func (e *Event) envelope() envelope {
	return envelope{e.V, e.Type, e.Run, e.Ep, e.Seq, e.Wall, e.GMs, e.Lvl, e.Map, e.SF}
}

// envelopeKeys are the JSON keys a body may not use.
var envelopeKeys = map[string]bool{
	"v": true, "type": true, "run": true, "ep": true, "seq": true, "wall": true,
	"gms": true, "lvl": true, "map": true, "sf": true,
}

// MarshalJSON writes the envelope followed by the body's fields.
func (e Event) MarshalJSON() ([]byte, error) {
	env, err := json.Marshal(e.envelope())
	if err != nil {
		return nil, err
	}
	if e.Body == nil {
		if e.raw != nil {
			return mergeRaw(env, e.raw)
		}
		return env, nil
	}
	body, err := json.Marshal(e.Body)
	if err != nil {
		return nil, fmt.Errorf("trace: %s body: %w", e.Type, err)
	}
	if len(body) < 2 || body[0] != '{' {
		return nil, fmt.Errorf("trace: %s body is not a JSON object", e.Type)
	}
	if string(body) == "{}" {
		return env, nil
	}
	out := make([]byte, 0, len(env)+len(body))
	out = append(out, env[:len(env)-1]...)
	out = append(out, ',')
	return append(out, body[1:]...), nil
}

// mergeRaw re-encodes a decoded event: the envelope, then the raw object's
// non-envelope fields in their original order.
func mergeRaw(env, raw []byte) ([]byte, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return nil, errors.New("trace: raw event is not a JSON object")
	}
	out := append([]byte(nil), env[:len(env)-1]...)
	for dec.More() {
		t, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, _ := t.(string)
		var val json.RawMessage
		if err := dec.Decode(&val); err != nil {
			return nil, err
		}
		if envelopeKeys[key] {
			continue
		}
		k, _ := json.Marshal(key)
		out = append(out, ',')
		out = append(out, k...)
		out = append(out, ':')
		out = append(out, val...)
	}
	return append(out, '}'), nil
}

// UnmarshalJSON reads the envelope and keeps the whole object for
// DecodeBody.
func (e *Event) UnmarshalJSON(b []byte) error {
	var env envelope
	if err := json.Unmarshal(b, &env); err != nil {
		return err
	}
	*e = Event{V: env.V, Type: env.Type, Run: env.Run, Ep: env.Ep, Seq: env.Seq, Wall: env.Wall,
		GMs: env.GMs, Lvl: env.Lvl, Map: env.Map, SF: env.SF, raw: append(json.RawMessage(nil), b...)}
	return nil
}

// DecodeBody decodes the type-specific fields into v (a pointer to a body
// type). It works for decoded events and for events built with a Body.
func (e *Event) DecodeBody(v any) error {
	if e.Body != nil {
		b, err := json.Marshal(e.Body)
		if err != nil {
			return err
		}
		return json.Unmarshal(b, v)
	}
	if e.raw == nil {
		return nil
	}
	return json.Unmarshal(e.raw, v)
}
