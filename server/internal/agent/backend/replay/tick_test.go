package replay_test

import (
	"testing"
	"time"

	"quake2web/server/internal/agent/backend/replay"
	"quake2web/server/internal/agent/backend/scripted"
	"quake2web/server/internal/agent/decide"
	"quake2web/server/internal/agent/trace"
)

// TestSkipsTickEvents: the bot's decision tick events (lane tick: no Req,
// many per episode) sit between the request events of a trace; replay
// ignores them and still replays every request.
func TestSkipsTickEvents(t *testing.T) {
	want, events := run(t, scripted.New(scripted.Config{Seed: 1}), decide.FixedLatency(100*time.Millisecond), 0)
	var mixed []trace.Event
	for i, e := range events {
		mixed = append(mixed, e)
		tick := trace.Decision{Lane: trace.LaneTick, Intent: &trace.Intent{Mode: "objective", FirePolicy: "hold", Movement: "hold"},
			Tick: &trace.Tick{N: i, Mode: "objective"}, Cmds: []trace.UserCmd{{Msec: 25, Seq: 10 + i, Ack: 9 + i}}}
		mixed = append(mixed, trace.Event{Type: trace.TypeDecision, GMs: e.GMs, Body: tick})
	}
	// the bodies as they come back from a trace file
	for i := range mixed {
		b, err := trace.Comparable(mixed[i])
		if err != nil {
			t.Fatal(err)
		}
		if mixed[i], err = decodeEvent(b); err != nil {
			t.Fatal(err)
		}
	}
	rb, err := replay.New(mixed, replay.Options{Strict: true})
	if err != nil {
		t.Fatalf("tick events in the trace: %v", err)
	}
	if rb.Len() != len(events) {
		t.Fatalf("%d requests recorded, %d in the trace", rb.Len(), len(events))
	}
	got, _ := run(t, rb, rb, 0)
	if len(got) != len(want) || rb.Divergence() != nil {
		t.Fatalf("replay with tick events: %d/%d intents, divergence %v", len(got), len(want), rb.Divergence())
	}
}

func decodeEvent(b []byte) (trace.Event, error) {
	var e trace.Event
	err := e.UnmarshalJSON(b)
	return e, err
}
