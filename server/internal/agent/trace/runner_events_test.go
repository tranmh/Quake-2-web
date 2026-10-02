package trace

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
)

// TestRunnerEventsRoundTrip writes the runner's event types through a file
// sink and reads them back: the bodies survive, and none of their fields
// shadows an envelope key.
func TestRunnerEventsRoundTrip(t *testing.T) {
	evs := []Event{
		{Type: TypeCmds, Ep: 1, GMs: 1300, Lvl: 0, Map: "demo1", SF: 19, Body: Cmds{Step: 12, Cmds: []UserCmd{
			{Msec: 25, Buttons: 1, Angles: [3]int16{-3, 16384, 0}, Forward: 400},
			{Msec: 25, Angles: [3]int16{-3, 16390, 0}, Side: -400, Up: 400, Impulse: 0},
		}}},
		{Type: TypeProvenance, Ep: 1, GMs: 90000, Map: "demo1", Body: Provenance{Ticks: 900, Fields: []TickField{
			{Name: "target", Default: 500, Model: 350, Scripted: 40, Stale: 10},
			{Name: "fire_policy", Default: 500, Model: 380, Scripted: 15, Stale: 4, Reflex: 1},
		}}},
	}
	for _, e := range evs {
		ty := reflect.TypeOf(e.Body)
		for i := 0; i < ty.NumField(); i++ {
			if name := strings.Split(ty.Field(i).Tag.Get("json"), ",")[0]; envelopeKeys[name] {
				t.Errorf("%s.%s uses the envelope key %q", ty.Name(), ty.Field(i).Name, name)
			}
		}
	}

	var buf bytes.Buffer
	sink := NewFileSink(nopCloser{&buf}, 0)
	bus := NewBus("run-r", fixedClock())
	bus.AddSink(sink)
	for _, e := range evs {
		bus.Publish(e)
	}
	if err := sink.Close(); err != nil {
		t.Fatal(err)
	}
	r, err := NewReader(&buf)
	if err != nil {
		t.Fatal(err)
	}
	var cmds Cmds
	var prov Provenance
	for i, want := range []any{&cmds, &prov} {
		e, err := r.Next()
		if err != nil {
			t.Fatal(err)
		}
		if e.Type != evs[i].Type || e.Ep != 1 || e.Run != "run-r" {
			t.Fatalf("event %d: %+v", i, e)
		}
		if err := e.DecodeBody(want); err != nil {
			t.Fatal(err)
		}
	}
	if !reflect.DeepEqual(cmds, evs[0].Body) || !reflect.DeepEqual(prov, evs[1].Body) {
		t.Fatalf("round trip:\n%+v\n%+v", cmds, prov)
	}
}

type nopCloser struct{ *bytes.Buffer }

func (nopCloser) Close() error { return nil }
