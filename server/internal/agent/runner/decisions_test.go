package runner

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"quake2web/server/internal/agent/decide"
	"quake2web/server/internal/agent/trace"
	"quake2web/server/internal/q2const"
)

var updateGolden = flag.Bool("update", false, "rewrite the golden files of the decision feed")

// feedRequest is a request decision event about st with made-up
// answers: every choice prefers its second option (its first when alone),
// a score sits at 1.3, a noul at 0.8.
func feedRequest(t *testing.T, seq uint64, lane decide.Lane, st *decide.State, latencyMs, cost float64) trace.Decision {
	t.Helper()
	req, err := decide.NewRequest(seq, lane, int64(seq)*100, st, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	ans := map[string]decide.Answer{}
	for _, q := range req.Questions {
		a := decide.Answer{Type: q.Type, Probabilities: map[string]float64{}, Confidence: 0.75, HasConfidence: true}
		switch q.Type {
		case decide.Choice:
			pick := 0
			if len(q.Options) > 1 {
				pick = 1
			}
			for i, o := range q.Options {
				p := 0.1 / float64(len(q.Options))
				if i == pick {
					p += 0.9
				}
				a.Probabilities[o.Key] = p
			}
			a.Choice = q.Options[pick].Key
		case decide.Score:
			a.Score = 1.3
			for i, o := range q.Options {
				a.Probabilities[o.Key] = map[int]float64{0: 0.2, 1: 0.6, 2: 0.2}[i]
			}
		case decide.Noul:
			a.Noul, a.HasConfidence = 0.8, false
		}
		ans[q.ID] = a
	}
	raw, err := decide.MarshalResponse("jev-1.13.0", req.Questions, ans, decide.Usage{InputTokens: 900})
	if err != nil {
		t.Fatal(err)
	}
	return trace.Decision{Lane: lane.String(), Req: seq, SnapGMs: req.SnapTime, State: req.State, Questions: req.QuestionsJSON(),
		Response: raw, Backend: "jev", Model: "jev-1.13.0", LatencyMs: latencyMs, InputTokens: 900, CostUSD: cost}
}

// feedEvents is a level of a bot's trace as its bus carries it.
func feedEvents(t *testing.T) []trace.Event {
	t.Helper()
	slowState := &decide.State{Mode: "objective",
		Me:        decide.Me{HP: "ok", Health: 80, Weapon: "shotgun", Ammo: "ok", Weapons: []string{"blaster", "shotgun", "machinegun"}},
		Enemies:   []decide.Enemy{{ID: "e7", Class: "soldier_shotgun", Bearing: 12, Dist: "mid", Units: 410, Visible: true, Shootable: true, Aim: "near", State: "attacking", Threat: "med"}},
		Items:     []decide.ItemView{{ID: "i3", Class: "item_health", Gives: "health+10", Bearing: -40, Path: 220}},
		Objective: &decide.Objective{Kind: "press", Desc: "press button *34", Bearing: 30, Path: 512, Next: 25},
		Level:     &decide.LevelInfo{Map: "demo1"}}
	fastState := &decide.State{
		Me: decide.Me{HP: "ok", Health: 80, Weapon: "shotgun", Ammo: "ok"},
		Enemies: []decide.Enemy{
			{ID: "e7", Class: "soldier_shotgun", Bearing: 10, Dist: "mid", Units: 398, Visible: true, Shootable: true, Aim: "on", State: "attacking", Threat: "med", Current: true},
			{ID: "e9", Class: "infantry", Bearing: -60, Dist: "far", Units: 820, Visible: false, Aim: "off", State: "alert", Threat: "high"},
		},
		Space: &decide.Space{Front: "open", Back: "tight", Left: "open", Right: "blocked"}}
	stale := feedRequest(t, 4, decide.LaneFast, fastState, 640, 0.00003)
	stale.Stale = true
	failed := trace.Decision{Lane: "fast", Req: 5, Backend: "jev", LatencyMs: 800, Err: "jev: timeout", Timeout: true}
	tick := func(n int, mode, target, move string, attack bool, fields ...trace.Field) trace.Decision {
		d := trace.Decision{Lane: trace.LaneTick, Intent: &trace.Intent{Mode: mode, Target: target, FirePolicy: "fire_when_aligned", Movement: move,
			Fields: fields}, Tick: &trace.Tick{N: n, Mode: mode, Step: 2, Target: target, Move: move, Weapon: "Shotgun", Health: 80}}
		d.Cmds = []trace.UserCmd{{Msec: 25}, {Msec: 25}}
		if attack {
			d.Cmds[1].Buttons = q2const.BUTTON_ATTACK
		}
		return d
	}
	fields := []trace.Field{
		{Name: "mode", Value: "fight", Source: trace.SourceModel, Confidence: 0.75},
		{Name: "target", Value: "e7", Source: trace.SourceModel, Confidence: 0.75},
		{Name: "fire_policy", Value: "fire_when_aligned", Source: trace.SourceScripted, Fallback: "low_confidence"},
		{Name: "movement", Value: "strafe_left", Source: trace.SourceModel, Confidence: 0.75},
		{Name: "weapon", Value: "keep", Source: trace.SourceDefault, Fallback: "not_asked"},
		{Name: "pickup", Value: "none", Source: trace.SourceStale, Fallback: "ttl"},
		{Name: "danger", Value: "1.3", Source: trace.SourceModel},
	}
	ev := func(typ string, gms int64, sf int32, lvl int, body any) trace.Event {
		return trace.Event{V: 1, Type: typ, Run: "run-1", GMs: gms, Lvl: lvl, Map: "demo1", SF: sf, Body: body}
	}
	return []trace.Event{
		ev(trace.TypeRunStart, 0, 0, 0, trace.RunStart{Schema: trace.Schema, Backend: "jev"}),
		ev(trace.TypeLevelStart, 100, 3, 0, trace.LevelStart{Visit: 0, Gen: 1, Checksum: "bfd75753"}),
		ev(trace.TypeDecision, 200, 4, 0, feedRequest(t, 1, decide.LaneSlow, slowState, 183, 0.00004)),
		ev(trace.TypeAPICall, 200, 4, 0, trace.APICall{Backend: "jev", Lane: "slow", Req: 1, Status: 200, LatencyMs: 183}),
		ev(trace.TypeDecision, 300, 5, 0, feedRequest(t, 2, decide.LaneFast, fastState, 121, 0.00002)),
		ev(trace.TypeDecision, 300, 5, 0, tick(0, "fight", "e7", "strafe_left", true, fields...)),
		ev(trace.TypeCmds, 300, 5, 0, trace.Cmds{Step: 3}),
		// a stale answer is not applied; a failed request has no answer
		ev(trace.TypeDecision, 400, 6, 0, stale),
		ev(trace.TypeDecision, 400, 6, 0, failed),
		ev(trace.TypeDecision, 400, 6, 0, tick(1, "fight", "e9", "", false, fields[:2]...)),
		ev(trace.TypeDamage, 450, 6, 0, trace.Damage{Amount: 12, Health: 68, Source: "e9"}),
		ev(trace.TypeKill, 500, 7, 0, trace.Kill{Target: "e7", Class: "soldier_shotgun", Weapon: "Shotgun"}),
		ev(trace.TypeStuck, 520, 7, 0, trace.Stuck{Stage: "jump", Node: 17, Pos: [3]float32{1, 2, 3}}),
		ev(trace.TypeBudget, 540, 7, 0, trace.Budget{SpentUSD: 0.5, LimitUSD: 1, RateHz: 5}),
		ev(trace.TypeDeath, 600, 8, 0, trace.Death{Cause: "infantry", Health: -4}),
		ev(trace.TypeReload, 2100, 1, 0, trace.Reload{Slot: "save0", Deaths: 1}),
		// a level start (here: the reload's) forgets the old answers
		ev(trace.TypeLevelStart, 2200, 2, 0, trace.LevelStart{Visit: 0, Gen: 2}),
		ev(trace.TypeDecision, 2300, 3, 0, tick(0, "objective", "", "", false)),
		ev(trace.TypeLevelEnd, 9000, 90, 0, trace.LevelEnd{Outcome: trace.OutcomeExit, CombatMs: 1200, KilledMonsters: 3, TotalMonsters: 9}),
		ev(trace.TypeEpisodeEnd, 9000, 90, 0, trace.EpisodeEnd{Outcome: "completed"}),
	}
}

// TestFeedTranslationGolden pins the decision feed's messages
// (q2bot.decisions/1) for a level of trace events; -update rewrites it.
func TestFeedTranslationGolden(t *testing.T) {
	tr := newFeedTranslator()
	var out bytes.Buffer
	for _, e := range feedEvents(t) {
		e := e
		for _, msg := range tr.translate(&e) {
			b, err := marshalFeed(msg)
			if err != nil {
				t.Fatal(err)
			}
			out.Write(b)
			out.WriteByte('\n')
		}
	}
	// the decoded events of a trace file translate the same
	tr2 := newFeedTranslator()
	var out2 bytes.Buffer
	for _, e := range feedEvents(t) {
		b, _ := json.Marshal(e)
		var d trace.Event
		if err := json.Unmarshal(b, &d); err != nil {
			t.Fatal(err)
		}
		for _, msg := range tr2.translate(&d) {
			b, _ := marshalFeed(msg)
			out2.Write(b)
			out2.WriteByte('\n')
		}
	}
	if out.String() != out2.String() {
		t.Errorf("decoded events translate differently:\n%s\nvs\n%s", out2.String(), out.String())
	}
	golden := filepath.Join("testdata", "decisions.golden.jsonl")
	if *updateGolden {
		if err := os.WriteFile(golden, out.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("%v (run with -update to create it)", err)
	}
	if out.String() != string(want) {
		t.Fatalf("feed differs from %s (-update rewrites it):\n%s", golden, out.String())
	}

	// spot checks of the contract
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	var first map[string]any
	for _, l := range lines {
		var m map[string]any
		if err := json.Unmarshal([]byte(l), &m); err != nil {
			t.Fatal(err)
		}
		if m["t"] == "decision" {
			first = m
			break
		}
	}
	if first == nil {
		t.Fatal("no decision")
	}
	target := first["target"].(map[string]any)
	action := first["action"].(map[string]any)
	prov := first["provenance"].(map[string]any)
	if target["id"] != "e7" || target["class"] != "soldier_shotgun" || target["dist"] != 398.0 ||
		first["objective"] != "press button *34" || first["mode"] != "fight" || first["latencyMs"] != 121.0 ||
		action["movement"] != "strafe_left" || action["firePolicy"] != "fire_when_aligned" || action["weapon"] != "Shotgun" ||
		action["fire"] != true || prov["fire_policy"] != "scripted" || prov["target"] != "model" {
		t.Fatalf("first decision %v", first)
	}
	qs := first["questions"].([]any)
	var ids []string
	for _, q := range qs {
		ids = append(ids, q.(map[string]any)["id"].(string))
	}
	if strings.Join(ids, ",") != "mode,target,fire_policy,movement,weapon,pickup,danger" {
		t.Fatalf("question order %v", ids)
	}
	danger := qs[6].(map[string]any)
	if danger["chosen"] != "low" || danger["options"].([]any)[0].(map[string]any)["label"] != "safe" {
		t.Fatalf("danger %v", danger)
	}
}
