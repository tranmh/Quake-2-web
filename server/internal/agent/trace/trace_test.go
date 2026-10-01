package trace

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

// update rewrites golden files (Q2_UPDATE_FIXTURES=1).
var update = os.Getenv("Q2_UPDATE_FIXTURES") == "1"

func golden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (regenerate with Q2_UPDATE_FIXTURES=1)", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("%s differs:\n--- got\n%s\n--- want\n%s", name, got, want)
	}
}

func fixedClock() func() time.Time {
	t0 := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	n := 0
	return func() time.Time {
		n++
		return t0.Add(time.Duration(n) * 250 * time.Millisecond)
	}
}

// sampleEvents is one of every event type, as a runner would publish them.
func sampleEvents() []Event {
	bearing := -42.5
	return []Event{
		{Type: TypeRunStart, Body: RunStart{Schema: Schema, Backend: "jev", ModelBackend: true, Model: "jev-1.13",
			Session: "lockstep", Maps: []string{"demo1", "demo2", "demo3"}, Skill: 1, Seed: 7, Episodes: 1,
			SimLatencyMs: 212, BudgetUSD: 2, Config: map[string]string{"max_qps": "10"}}},
		{Type: TypeEpisodeStart, Body: EpisodeStart{Seed: 7}},
		{Type: TypeLevelStart, GMs: 600, Map: "demo1", SF: 12, Body: LevelStart{Visit: 0, Gen: 1, Checksum: "1234567"}},
		{Type: TypeDecision, GMs: 1300, Map: "demo1", SF: 19, Body: Decision{Lane: "fast", Req: 1, SnapGMs: 1100,
			StateDigest: "00112233445566778899aabbccddeeff", Questions: json.RawMessage(`[{"id":"target","type":"choice"}]`),
			Response: json.RawMessage(`{"target":{"choice":"e3","confidence":0.8}}`),
			Fields: []Field{{Name: "target", Value: "e3", Source: SourceModel, Confidence: 0.8, Scripted: "e3"},
				{Name: "fire_policy", Value: "hold", Source: SourceReflex, Fallback: "neutral in line"}},
			Cmd: &UserCmd{Msec: 25, Buttons: 1, Angles: [3]int16{0, 16384, 0}, Forward: 300}, Backend: "jev",
			Model: "jev-1.13", LatencyMs: 212.5, InputTokens: 900, OutputTokens: 35, CostUSD: 0.0000378, Combat: true}},
		{Type: TypeAPICall, GMs: 1300, Map: "demo1", SF: 19, Body: APICall{Backend: "jev", Model: "jev-1.13", Lane: "fast",
			Req: 1, Status: 200, LatencyMs: 212.5, InputTokens: 900, CostUSD: 0.0000378, Combat: true}},
		{Type: TypeDamage, GMs: 1500, Map: "demo1", SF: 21, Body: Damage{Amount: 8, Health: 92, Armor: 0, Bearing: &bearing, Source: "e3"}},
		{Type: TypeKill, GMs: 2100, Map: "demo1", SF: 27, Body: Kill{Target: "e3", Class: "soldier_light", Weapon: "blaster"}},
		{Type: TypeStuck, GMs: 2500, Map: "demo1", SF: 31, Body: Stuck{Stage: "jump", Node: 41, Pos: [3]float32{128, -320, 24}}},
		{Type: TypeDeath, GMs: 3000, Map: "demo1", SF: 36, Body: Death{Cause: "gunner", Health: -12}},
		{Type: TypeReload, GMs: 4500, Map: "demo1", SF: 12, Body: Reload{Slot: "save0", Deaths: 1}},
		{Type: TypeBudget, GMs: 5000, Map: "demo1", Body: Budget{SpentUSD: 1.5, LimitUSD: 2, RateHz: 5, Reason: "75% spent"}},
		{Type: TypeError, GMs: 5100, Map: "demo1", Body: Error{Msg: "jev: 429 Too Many Requests"}},
		{Type: TypeLevelEnd, GMs: 9000, Map: "demo1", SF: 96, Body: LevelEnd{Outcome: OutcomeExit, CombatMs: 1200,
			KilledMonsters: 3, TotalMonsters: 9, FoundSecrets: 0, TotalSecrets: 2}},
		{Type: TypeEpisodeEnd, GMs: 9000, Body: EpisodeEnd{Outcome: "failed", Reason: "stopped after demo1"}},
		{Type: TypeRunEnd, GMs: 9000, Body: RunEnd{Outcome: "failed"}},
	}
}

func publishAll(b *Bus, evs []Event) []Event {
	out := make([]Event, 0, len(evs))
	for _, e := range evs {
		e.Run = ""
		out = append(out, b.Publish(e))
	}
	return out
}

func TestEventJSONGolden(t *testing.T) {
	b := NewBus("run-0001", fixedClock())
	var buf bytes.Buffer
	for _, e := range publishAll(b, sampleEvents()) {
		line, err := json.Marshal(e)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.HasPrefix(line, []byte(`{"v":1,"type":"`+e.Type+`","run":"run-0001","ep":`)) {
			t.Fatalf("envelope not first: %s", line)
		}
		buf.Write(line)
		buf.WriteByte('\n')
	}
	golden(t, "events.golden.jsonl", buf.Bytes())
}

func TestEventRoundTrip(t *testing.T) {
	b := NewBus("r", fixedClock())
	for _, e := range publishAll(b, sampleEvents()) {
		line, _ := json.Marshal(e)
		var d Event
		if err := json.Unmarshal(line, &d); err != nil {
			t.Fatal(err)
		}
		if d.Type != e.Type || d.Seq != e.Seq || d.GMs != e.GMs || d.Map != e.Map || d.SF != e.SF || d.Wall != e.Wall {
			t.Fatalf("envelope: %+v vs %+v", d, e)
		}
		// the decoded event re-encodes to the same line
		again, err := json.Marshal(d)
		if err != nil || !bytes.Equal(again, line) {
			t.Fatalf("re-encoded %s\nwant %s (%v)", again, line, err)
		}
		// and the body decodes back to the published value
		want := reflect.ValueOf(e.Body)
		got := reflect.New(want.Type())
		if err := d.DecodeBody(got.Interface()); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got.Elem().Interface(), e.Body) {
			t.Fatalf("%s body: %+v want %+v", e.Type, got.Elem().Interface(), e.Body)
		}
		// DecodeBody also works on an event built in process
		got2 := reflect.New(want.Type())
		if err := e.DecodeBody(got2.Interface()); err != nil || !reflect.DeepEqual(got2.Elem().Interface(), e.Body) {
			t.Fatalf("%s in-process body: %v", e.Type, err)
		}
	}
	// a modified envelope wins over the raw copy's values
	var d Event
	_ = json.Unmarshal([]byte(`{"v":1,"type":"kill","run":"a","ep":0,"seq":5,"wall":1,"gms":2,"lvl":0,"map":"m","sf":3,"target":"e1","class":"x"}`), &d)
	d.Seq = 9
	out, _ := json.Marshal(d)
	if !strings.Contains(string(out), `"seq":9`) || strings.Count(string(out), `"seq"`) != 1 || !strings.Contains(string(out), `"target":"e1"`) {
		t.Fatalf("re-encoded %s", out)
	}
	if _, err := json.Marshal(Event{Type: "x", Body: 3}); err == nil {
		t.Fatal("non-object body accepted")
	}
}

func TestBodiesDoNotShadowEnvelope(t *testing.T) {
	for _, e := range sampleEvents() {
		ty := reflect.TypeOf(e.Body)
		for i := 0; i < ty.NumField(); i++ {
			name := strings.Split(ty.Field(i).Tag.Get("json"), ",")[0]
			if envelopeKeys[name] {
				t.Errorf("%s.%s uses the envelope key %q", ty.Name(), ty.Field(i).Name, name)
			}
		}
	}
}

type memSink struct {
	mu  sync.Mutex
	evs []Event
	err error
}

func (m *memSink) Write(e Event) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.evs = append(m.evs, e)
	return m.err
}

func TestBusStampsAndFansOut(t *testing.T) {
	b := NewBus("run-x", fixedClock())
	s1, s2 := &memSink{}, &memSink{err: errors.New("disk full")}
	b.AddSink(s1)
	b.AddSink(s2)
	sub := b.Subscribe(100)
	for i := 0; i < 5; i++ {
		b.Publish(Event{Type: TypeKill, Ep: 2, GMs: int64(i), Body: Kill{Target: fmt.Sprint(i)}})
	}
	b.Publish(Event{Type: TypeError, Run: "other", Body: Error{Msg: "x"}})
	if len(s1.evs) != 6 || len(s2.evs) != 6 {
		t.Fatalf("sinks got %d and %d events", len(s1.evs), len(s2.evs))
	}
	for i, e := range s1.evs {
		if e.Seq != uint64(i+1) || e.V != Version || e.Wall == 0 {
			t.Fatalf("event %d: %+v", i, e)
		}
	}
	if s1.evs[0].Run != "run-x" || s1.evs[5].Run != "other" || s1.evs[4].Ep != 2 {
		t.Fatalf("stamps %+v", s1.evs)
	}
	if b.Err() == nil || !strings.Contains(b.Err().Error(), "disk full") {
		t.Fatalf("sink error %v", b.Err())
	}
	if len(sub.C()) != 6 || sub.Dropped() != 0 {
		t.Fatalf("subscriber queue %d dropped %d", len(sub.C()), sub.Dropped())
	}
	b.Close()
	n := 0
	for range sub.C() {
		n++
	}
	if n != 6 {
		t.Fatalf("drained %d", n)
	}
	b.Publish(Event{Type: TypeKill}) // after Close: dropped, no panic
	if len(s1.evs) != 6 {
		t.Fatal("published after Close")
	}
	if s := b.Subscribe(1); s.Dropped() != 0 {
		t.Fatal("subscription after close")
	} else if _, ok := <-s.C(); ok {
		t.Fatal("subscription after Close is open")
	}
}

func TestBusDropOldest(t *testing.T) {
	b := NewBus("r", nil)
	slow := b.Subscribe(3)
	fast := b.Subscribe(64)
	done := make(chan struct{})
	go func() {
		for i := 1; i <= 10; i++ {
			b.Publish(Event{Type: TypeKill, GMs: int64(i)})
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Publish blocked on a full subscriber")
	}
	var got []int64
	for len(slow.C()) > 0 {
		got = append(got, (<-slow.C()).GMs)
	}
	if fmt.Sprint(got) != "[8 9 10]" || slow.Dropped() != 7 {
		t.Fatalf("slow subscriber kept %v, dropped %d", got, slow.Dropped())
	}
	if len(fast.C()) != 10 || fast.Dropped() != 0 {
		t.Fatalf("fast subscriber %d dropped %d", len(fast.C()), fast.Dropped())
	}
	slow.Close()
	slow.Close() // idempotent
	if _, ok := <-slow.C(); ok {
		t.Fatal("closed subscription still open")
	}
	b.Publish(Event{Type: TypeKill}) // the closed subscriber is gone
	if len(fast.C()) != 11 {
		t.Fatal("fan-out stopped after an unsubscribe")
	}
}

func TestBusConcurrent(t *testing.T) {
	b := NewBus("r", nil)
	sink := &memSink{}
	b.AddSink(sink)
	sub := b.Subscribe(16)
	var readers sync.WaitGroup
	readers.Add(1)
	read := 0
	go func() {
		defer readers.Done()
		for range sub.C() {
			read++
		}
	}()
	var pubs sync.WaitGroup
	for p := 0; p < 4; p++ {
		pubs.Add(1)
		go func() {
			defer pubs.Done()
			for i := 0; i < 500; i++ {
				b.Publish(Event{Type: TypeDecision})
			}
		}()
	}
	pubs.Wait()
	b.Close()
	readers.Wait()
	if len(sink.evs) != 2000 {
		t.Fatalf("sink got %d", len(sink.evs))
	}
	for i, e := range sink.evs {
		if e.Seq != uint64(i+1) {
			t.Fatalf("sink order: event %d has seq %d", i, e.Seq)
		}
	}
	if uint64(read)+sub.Dropped() != 2000 {
		t.Fatalf("read %d + dropped %d != 2000", read, sub.Dropped())
	}
}

func TestFileSinkRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trace.jsonl.gz")
	f, err := CreateFile(path, 20*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	b := NewBus("r", fixedClock())
	b.AddSink(f)
	pub := publishAll(b, sampleEvents())

	// the periodic flush makes the events readable before Close (a crash
	// loses at most one flush interval)
	deadline := time.Now().Add(5 * time.Second)
	var partial []Event
	for time.Now().Before(deadline) {
		partial, _ = ReadFile(path)
		if len(partial) == len(pub) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(partial) != len(pub) {
		t.Fatalf("after the flush interval %d of %d events are on disk", len(partial), len(pub))
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal("second close:", err)
	}
	if err := f.Write(Event{Type: TypeKill}); err == nil {
		t.Fatal("write after close")
	}
	got, err := ReadFile(path)
	if err != nil || len(got) != len(pub) || f.Events() != len(pub) {
		t.Fatalf("read %d events (%v), wrote %d", len(got), err, f.Events())
	}
	for i := range pub {
		a, _ := Comparable(pub[i])
		g, _ := Comparable(got[i])
		if !bytes.Equal(a, g) {
			t.Fatalf("event %d:\n%s\n%s", i, a, g)
		}
	}

	// a plain (uncompressed) JSONL trace reads too
	var plain bytes.Buffer
	for _, e := range pub {
		line, _ := json.Marshal(e)
		plain.Write(line)
		plain.WriteString("\n\n")
	}
	r, err := NewReader(&plain)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for {
		if _, err := r.Next(); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		n++
	}
	if n != len(pub) {
		t.Fatalf("plain: %d events", n)
	}

	// a bad line is reported with its number
	r, _ = NewReader(strings.NewReader("{\"v\":1}\nnot json\n"))
	_, _ = r.Next()
	if _, err := r.Next(); err == nil || !strings.Contains(err.Error(), "line 2") {
		t.Fatalf("bad line: %v", err)
	}
}

// bufCloser is an in-memory file for a FileSink.
type bufCloser struct{ bytes.Buffer }

func (*bufCloser) Close() error { return nil }

// readAll reads events until the first error (io.EOF included).
func readAll(t *testing.T, data []byte) ([]Event, error) {
	t.Helper()
	r, err := NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	var out []Event
	for {
		e, err := r.Next()
		if err != nil {
			return out, err
		}
		out = append(out, e)
	}
}

// TestTruncatedTrace: a trace cut anywhere (inside a line, at a line end,
// inside the gzip trailer, or after a flush of a sink that never closed)
// yields its complete events in order and then io.ErrUnexpectedEOF, never
// a JSON syntax error.
func TestTruncatedTrace(t *testing.T) {
	const n = 20000
	var file bufCloser
	f := NewFileSink(&file, 0)
	b := NewBus("r", fixedClock())
	b.AddSink(f)
	flushedAt := 0 // file size after the flush following event n/2
	for i := 0; i < n; i++ {
		b.Publish(Event{Type: TypeKill, GMs: int64(i) * 100, Map: "demo1",
			Body: Kill{Target: fmt.Sprintf("e%d", i*7919%1000), Class: "soldier_light", Weapon: "blaster"}})
		if i == n/2 {
			if err := f.Flush(); err != nil {
				t.Fatal(err)
			}
			flushedAt = file.Len()
		}
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	full := file.Bytes()

	check := func(name string, data []byte, minEvents, maxEvents int) {
		t.Helper()
		evs, err := readAll(t, data)
		if !errors.Is(err, io.ErrUnexpectedEOF) {
			t.Fatalf("%s: %d events, then %v; want io.ErrUnexpectedEOF", name, len(evs), err)
		}
		if len(evs) < minEvents || len(evs) > maxEvents {
			t.Fatalf("%s: %d complete events, want %d..%d", name, len(evs), minEvents, maxEvents)
		}
		for i, e := range evs {
			if e.Seq != uint64(i+1) || e.GMs != int64(i)*100 {
				t.Fatalf("%s: event %d is seq %d gms %d", name, i, e.Seq, e.GMs)
			}
		}
	}
	check("gzip cut at 1/3", full[:len(full)/3], 1, n-1)
	check("gzip cut at 1/2", full[:len(full)/2], 1, n-1)
	check("gzip cut after a flush", full[:flushedAt+3], n/2+1, n-1) // everything flushed is complete
	check("gzip trailer cut", full[:len(full)-4], n, n)             // every line, no checksum

	// a crash after a flush: the sink's file holds the flushed stream with
	// no final block or trailer
	var crashed bufCloser
	cf := NewFileSink(&crashed, 0)
	cb := NewBus("r", fixedClock())
	cb.AddSink(cf)
	for i := 0; i < 50; i++ {
		cb.Publish(Event{Type: TypeKill, GMs: int64(i) * 100, Body: Kill{Target: "e1"}})
	}
	if err := cf.Flush(); err != nil {
		t.Fatal(err)
	}
	check("unclosed sink after flush", append([]byte(nil), crashed.Bytes()...), 50, 50)

	// plain JSONL cut inside a line, and at a line end (no event lost: a
	// clean EOF)
	var plain bytes.Buffer
	for i := 0; i < 3; i++ {
		line, _ := json.Marshal(Event{V: 1, Type: TypeKill, Seq: uint64(i + 1), GMs: int64(i) * 100})
		plain.Write(line)
		plain.WriteByte('\n')
	}
	cut := plain.Bytes()[:plain.Len()-10]
	check("plain cut inside a line", cut, 2, 2)
	if evs, err := readAll(t, plain.Bytes()); !errors.Is(err, io.EOF) || len(evs) != 3 {
		t.Fatalf("plain: %d events, %v", len(evs), err)
	}
	// an unterminated last line that parses is an event; a corrupt
	// terminated line stays a syntax error
	if evs, err := readAll(t, bytes.TrimSuffix(plain.Bytes(), []byte("\n"))); !errors.Is(err, io.EOF) || len(evs) != 3 {
		t.Fatalf("unterminated last line: %d events, %v", len(evs), err)
	}
	corrupt := append(append([]byte(nil), plain.Bytes()...), "{\"v\":1,\n"...)
	if _, err := readAll(t, corrupt); err == nil || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) {
		t.Fatalf("corrupt terminated line: %v", err)
	}

	// ReadFile returns the complete events with the error
	path := filepath.Join(t.TempDir(), "cut.jsonl.gz")
	if err := os.WriteFile(path, full[:len(full)/2], 0o644); err != nil {
		t.Fatal(err)
	}
	if evs, err := ReadFile(path); !errors.Is(err, io.ErrUnexpectedEOF) || len(evs) == 0 {
		t.Fatalf("ReadFile: %d events, %v", len(evs), err)
	}
}

func TestDigestAndComparable(t *testing.T) {
	a, err := Canonical(json.RawMessage(`{ "b": [1, 2.50, {"y": "<x>", "x": null}], "a": true }`))
	if err != nil {
		t.Fatal(err)
	}
	if string(a) != `{"a":true,"b":[1,2.50,{"x":null,"y":"<x>"}]}` {
		t.Fatalf("canonical %s", a)
	}
	type s struct {
		B []any `json:"b"`
		A bool  `json:"a"`
	}
	d1, _ := Digest(json.RawMessage(`{"a":true,"b":[1]}`))
	d2, _ := Digest(s{A: true, B: []any{1}})
	d3, _ := Digest(map[string]any{"b": []int{1}, "a": true})
	if d1 != d2 || d1 != d3 || len(d1) != 32 {
		t.Fatalf("digests %s %s %s", d1, d2, d3)
	}
	// pinned: sha256("{\"a\":true,\"b\":[1]}")[:16]
	if d1 != "c8e300c84b051af86354bfc1765bc2fa" {
		t.Fatalf("digest %s", d1)
	}
	if d4, _ := Digest(map[string]any{"a": false, "b": []int{1}}); d4 == d1 {
		t.Fatal("different values, same digest")
	}
	if _, err := Canonical(json.RawMessage(`{`)); err == nil {
		t.Fatal("bad JSON accepted")
	}

	e1 := Event{Type: TypeKill, Seq: 3, Wall: 111, GMs: 5, Body: Kill{Target: "e1"}}
	e2 := e1
	e2.Wall = 999
	c1, _ := Comparable(e1)
	c2, _ := Comparable(e2)
	if !bytes.Equal(c1, c2) || bytes.Contains(c1, []byte("wall")) {
		t.Fatalf("comparable %s / %s", c1, c2)
	}
	e2.GMs = 6
	if c3, _ := Comparable(e2); bytes.Equal(c1, c3) {
		t.Fatal("game time ignored")
	}
}

type creds struct {
	Name string
	Key  Secret
	key  Secret
}

func TestSecretNeverPrints(t *testing.T) {
	const raw = "sk-live-0123456789"
	s := NewSecret(raw)
	if s.Reveal() != raw || !s.IsSet() || NewSecret("").IsSet() || (Secret{}).Reveal() != "" {
		t.Fatal("reveal")
	}
	c := creds{Name: "jev", Key: s, key: s}
	var outs []string
	for _, f := range []string{"%v", "%s", "%q", "%x", "%d", "%+v", "%#v", "%10s"} {
		outs = append(outs, fmt.Sprintf(f, s), fmt.Sprintf(f, c), fmt.Sprintf(f, &c))
	}
	outs = append(outs, fmt.Sprint(s), fmt.Sprintln(c), s.String(), s.GoString())
	j, _ := json.Marshal(c)
	outs = append(outs, string(j))
	txt, _ := s.MarshalText()
	outs = append(outs, string(txt))
	var logbuf bytes.Buffer
	slog.New(slog.NewJSONHandler(&logbuf, nil)).Info("call", "key", s, "creds", c)
	slog.New(slog.NewTextHandler(&logbuf, nil)).Info("call", "key", s)
	outs = append(outs, logbuf.String())
	for _, o := range outs {
		if strings.Contains(o, raw) || strings.Contains(o, "0123456789") {
			t.Fatalf("secret leaked: %s", o)
		}
	}
	if !strings.Contains(fmt.Sprint(s), Redacted) || string(j) != `{"Name":"jev","Key":"[REDACTED]"}` {
		t.Fatalf("redacted forms: %s / %s", fmt.Sprint(s), j)
	}
	if fmt.Sprint(Secret{}) != "" {
		t.Fatal("empty secret prints")
	}

	msg := "POST /v1/systemone: 401 (key " + raw + " invalid)"
	if got := Redact(msg, s, Secret{}); strings.Contains(got, raw) || !strings.Contains(got, Redacted) {
		t.Fatalf("Redact: %s", got)
	}
	h := http.Header{}
	h.Set("Authorization", "Bearer "+raw)
	h.Set("Content-Type", "application/json")
	h.Add("Cookie", "a=b")
	r := RedactHeader(h)
	if r.Get("Authorization") != Redacted || r.Get("Cookie") != Redacted || r.Get("Content-Type") != "application/json" {
		t.Fatalf("RedactHeader %v", r)
	}
	if h.Get("Authorization") != "Bearer "+raw {
		t.Fatal("RedactHeader modified its input")
	}
}
