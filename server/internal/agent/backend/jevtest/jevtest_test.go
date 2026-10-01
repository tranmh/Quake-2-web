package jevtest_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"testing"
	"time"

	"quake2web/server/internal/agent/backend/jev"
	"quake2web/server/internal/agent/backend/jevtest"
	"quake2web/server/internal/agent/backend/scripted"
	"quake2web/server/internal/agent/decide"
	"quake2web/server/internal/agent/trace"
)

const key = "sk-jevtest"

// states returns varied lane states (enemies at different ranges and
// threats, items, damage).
func states() []*decide.State {
	var out []*decide.State
	classes := []string{"soldier", "gunner", "berserk", "infantry", "parasite"}
	threats := []string{"low", "med", "high"}
	for i := 0; i < 40; i++ {
		st := &decide.State{
			Me: decide.Me{HP: []string{"critical", "low", "ok", "full"}[i%4], Health: []int{15, 40, 80, 100}[i%4], Weapon: []string{"shotgun", "machinegun", "blaster"}[i%3],
				Ammo: []string{"ok", "low", "inf"}[i%3], Weapons: []string{"blaster", "shotgun", "machinegun", "rocket_launcher"}, DamageLast1s: (i % 5) * 7},
			Enemies: []decide.Enemy{},
			Space:   &decide.Space{Front: "open", Back: []string{"open", "blocked"}[i%2], Left: "open", Right: []string{"tight", "blocked"}[i%2]},
		}
		for j := 0; j < i%4; j++ {
			u := 120 + 230*((i+j)%6)
			dist := "far"
			if u < 250 {
				dist = "close"
			} else if u < 700 {
				dist = "mid"
			}
			st.Enemies = append(st.Enemies, decide.Enemy{ID: fmt.Sprintf("e%d", j+1), Class: classes[(i+j)%5], Bearing: (i*37+j*50)%360 - 180,
				Dist: dist, Units: u, Visible: (i+j)%3 != 0, Shootable: (i+j)%4 != 0, Aim: "near", State: []string{"idle", "alert", "attacking"}[(i+j)%3],
				Threat: threats[(i*j+i)%3], Current: j == 0 && i%2 == 0})
		}
		if i%3 == 0 {
			st.Incoming = []decide.Incoming{{Kind: "rocket", Bearing: 10, ETA: []string{"imminent", "soon"}[i%2], Dodge: "left"}}
		}
		if i%2 == 1 {
			st.Items = []decide.ItemView{{ID: "i1", Class: "item_health", Gives: "health+10", Path: 200 + i*20}, {ID: "i2", Class: "weapon_chaingun", Gives: "weapon:chaingun", Path: 500}}
			st.Objective = &decide.Objective{Kind: "touch", Desc: "exit", Path: 900}
		}
		out = append(out, st)
	}
	return out
}

func newClient(t *testing.T, url string, rec io.Writer) *jev.Client {
	t.Helper()
	c, err := jev.New(jev.Config{BaseURL: url, APIKey: trace.NewSecret(key), AllowCustomBase: true, Recorder: rec, RatePerSec: -1})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// TestScriptedMatchesBackend: the wire policy parses the lane state and
// answers exactly like the scripted backend does with the typed state.
func TestScriptedMatchesBackend(t *testing.T) {
	pol := jevtest.NewScripted(scripted.Config{Seed: 4})
	pol.Now = func(*jevtest.Call) int64 { return 777 }
	srv := jevtest.NewServer(jevtest.Options{Policy: pol, APIKey: key})
	defer srv.Close()
	c := newClient(t, srv.URL(), nil)
	sb := scripted.New(scripted.Config{Seed: 4})
	for i, st := range states() {
		for _, lane := range []decide.Lane{decide.LaneFast, decide.LaneSlow} {
			req, err := decide.NewRequest(uint64(i), lane, 777, st, nil, 0)
			if err != nil {
				t.Fatal(err)
			}
			got, err := c.Decide(context.Background(), req)
			if err != nil {
				t.Fatal(err)
			}
			want, _ := sb.Decide(context.Background(), req)
			for _, q := range req.Questions {
				g, w := got.Answers[q.ID], want.Answers[q.ID]
				if g.Choice != w.Choice || g.Score != w.Score {
					t.Fatalf("state %d %s %s: wire %+v, backend %+v", i, lane, q.ID, g, w)
				}
			}
		}
	}
}

// TestNoisy: deterministic per request, model-like disagreement overall.
func TestNoisy(t *testing.T) {
	base := jevtest.NewScripted(scripted.Config{})
	base.Now = func(*jevtest.Call) int64 { return 0 }
	noisy := &jevtest.Noisy{Base: base, Seed: 11}
	var calls []*jevtest.Call
	for i, st := range states() {
		for _, lane := range []decide.Lane{decide.LaneFast, decide.LaneSlow} {
			req, _ := decide.NewRequest(uint64(i), lane, 0, st, nil, 0)
			calls = append(calls, &jevtest.Call{State: req.State, Questions: req.Questions, Digest: req.Digest()})
		}
	}
	changed, low, total := 0, 0, 0
	for _, c := range calls {
		a, err := noisy.Answer(c)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := noisy.Answer(c)
		if fmt.Sprint(a.Answers) != fmt.Sprint(b.Answers) {
			t.Fatal("same request, different noise")
		}
		r, _ := base.Answer(c)
		for _, q := range c.Questions {
			ans := a.Answers[q.ID]
			sum := 0.0
			for _, p := range ans.Probabilities {
				if p < 0 || p > 1 {
					t.Fatalf("probability %v", p)
				}
				sum += p
			}
			if q.Type == decide.Choice {
				total++
				if math.Abs(sum-1) > 1e-3 || q.Index(ans.Choice) < 0 || !ans.HasConfidence {
					t.Fatalf("%s: %+v", q.ID, ans)
				}
				if ans.Choice != r.Answers[q.ID].Choice {
					changed++
				}
				if ans.Confidence < 0.35 {
					low++
				}
			}
			if q.Type == decide.Score && (ans.Score < 0 || ans.Score > 4) {
				t.Fatalf("score %+v", ans)
			}
		}
	}
	if changed == 0 || changed > total/3 || low == 0 || low > total/3 {
		t.Fatalf("%d of %d choices changed, %d unconfident", changed, total, low)
	}
	t.Logf("%d of %d choices disagree with the base policy, %d below 0.35 confidence", changed, total, low)
	// another seed draws other noise; a retry of the same content does not
	other := &jevtest.Noisy{Base: base, Seed: 12}
	retry := *calls[0]
	retry.Attempt, retry.N = 1, 99
	a, _ := noisy.Answer(calls[0])
	b, _ := other.Answer(calls[0])
	c, _ := noisy.Answer(&retry)
	if fmt.Sprint(a.Answers) == fmt.Sprint(b.Answers) || fmt.Sprint(a.Answers) != fmt.Sprint(c.Answers) {
		t.Fatal("noise must depend on the seed and the content only")
	}
}

// TestRecorded replays the jev client's own recording, matched by digest.
func TestRecorded(t *testing.T) {
	noisy := &jevtest.Noisy{Base: jevtest.NewScripted(scripted.Config{}), Seed: 2}
	src := jevtest.NewServer(jevtest.Options{Policy: noisy, APIKey: key, Faults: jevtest.Faults{Sequence: []jevtest.Fault{jevtest.FaultNone, jevtest.FaultServer}}})
	defer src.Close()
	var rec bytes.Buffer
	c := newClient(t, src.URL(), &rec)
	var reqs []*decide.Request
	want := map[uint64]string{}
	for i, st := range states()[:6] {
		req, _ := decide.NewRequest(uint64(i+1), decide.LaneSlow, 0, st, nil, 0)
		reqs = append(reqs, req)
		resp, err := c.Decide(context.Background(), req)
		if err != nil {
			t.Fatal(err)
		}
		want[req.Seq] = fmt.Sprint(resp.Answers)
	}
	// the 500 of the second request (retried) is recorded but not replayed
	r, err := jevtest.LoadRecorded(bytes.NewReader(rec.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if r.Len() != 6 {
		t.Fatalf("%d recorded responses", r.Len())
	}
	dst := jevtest.NewServer(jevtest.Options{Policy: r, APIKey: key})
	defer dst.Close()
	c2 := newClient(t, dst.URL(), nil)
	for i := len(reqs) - 1; i >= 0; i-- { // another order: matched by digest
		resp, err := c2.Decide(context.Background(), reqs[i])
		if err != nil {
			t.Fatal(err)
		}
		if got := fmt.Sprint(resp.Answers); got != want[reqs[i].Seq] {
			t.Fatalf("request %d: replayed %s, recorded %s", reqs[i].Seq, got, want[reqs[i].Seq])
		}
	}
	if _, err := c2.Decide(context.Background(), reqs[0]); jev.ClassOf(err) != jev.ClassServer {
		t.Fatalf("exhausted: %v", err)
	}
	// bare response lines replay in order
	bare := `{"model":"m","answers":{"mode":{"type":"choice","choice":"explore","confidence":0.9,"probabilities":{"explore":1}}}}` + "\n"
	r2, err := jevtest.LoadRecorded(strings.NewReader(bare + `{"response":` + strings.TrimSpace(bare) + "}\n"))
	if err != nil || r2.Len() != 2 {
		t.Fatalf("bare lines: %v", err)
	}
	if _, err := jevtest.LoadRecorded(strings.NewReader("{\"x\":1}\n")); err == nil {
		t.Fatal("a line without a response accepted")
	}
}

func post(t *testing.T, url, auth, body string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	return resp
}

func TestFaultsAndValidation(t *testing.T) {
	st := states()[5]
	req, _ := decide.NewRequest(1, decide.LaneFast, 0, st, nil, 0)
	body, _ := decide.RequestBody("jev-1.13.0", req)
	srv := jevtest.NewServer(jevtest.Options{APIKey: key, Faults: jevtest.Faults{RetryAfter: 1500 * time.Millisecond,
		Sequence: []jevtest.Fault{jevtest.FaultRateLimit, jevtest.FaultOverload, jevtest.FaultServer, jevtest.FaultNone}}})
	defer srv.Close()
	url := srv.URL() + jevtest.Path
	for i, want := range []int{429, 529, 500, 200} {
		resp := post(t, url, "Bearer "+key, string(body))
		if resp.StatusCode != want {
			t.Fatalf("call %d: status %d, want %d", i, resp.StatusCode, want)
		}
		if want == 429 || want == 529 {
			if resp.Header.Get("Retry-After") != "2" || resp.Header.Get("Retry-After-Ms") != "1500" {
				t.Fatalf("retry headers %v", resp.Header)
			}
		}
	}
	for _, tc := range []struct {
		name, url, auth, body string
		want                  int
	}{
		{"no key", url, "", string(body), 401},
		{"wrong key", url, "Bearer nope", string(body), 401},
		{"not bearer", url, key, string(body), 401},
		{"wrong path", srv.URL() + "/v1/other", "Bearer " + key, string(body), 404},
		{"malformed", url, "Bearer " + key, `{"model":`, 400},
		{"no model", url, "Bearer " + key, `{"state":{},"questions":{"q":{"type":"noul","instructions":"i"}}}`, 422},
		{"no questions", url, "Bearer " + key, `{"model":"m","state":{},"questions":{}}`, 422},
		{"one score level", url, "Bearer " + key, `{"model":"m","state":{},"questions":{"q":{"type":"score","instructions":"i","criteria":["a"]}}}`, 422},
		{"bad type", url, "Bearer " + key, `{"model":"m","state":{},"questions":{"q":{"type":"vote","instructions":"i"}}}`, 422},
	} {
		if resp := post(t, tc.url, tc.auth, tc.body); resp.StatusCode != tc.want {
			t.Errorf("%s: %d, want %d", tc.name, resp.StatusCode, tc.want)
		}
	}
	getReq, _ := http.NewRequest(http.MethodGet, url, nil)
	if resp, err := http.DefaultClient.Do(getReq); err != nil || resp.StatusCode != 405 {
		t.Errorf("GET: %v %v", resp, err)
	} else {
		resp.Body.Close()
	}
	if srv.Count() != 4 || srv.Hits() != 14 {
		t.Fatalf("calls %d hits %d", srv.Count(), srv.Hits())
	}
	if calls := srv.Calls(); calls[0].Fault != jevtest.FaultRateLimit || calls[3].Fault != jevtest.FaultNone || calls[1].Attempt != 1 {
		t.Fatalf("calls %+v", calls[:2])
	}

	// latency and hangs
	slow := jevtest.NewServer(jevtest.Options{APIKey: key, Faults: jevtest.Faults{Latency: []time.Duration{40 * time.Millisecond}, Sequence: []jevtest.Fault{jevtest.FaultNone, jevtest.FaultHang}}})
	defer slow.Close()
	start := time.Now()
	if resp := post(t, slow.URL()+jevtest.Path, "Bearer "+key, string(body)); resp.StatusCode != 200 || time.Since(start) < 40*time.Millisecond {
		t.Fatalf("latency: %d after %v", resp.StatusCode, time.Since(start))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	hreq, _ := http.NewRequestWithContext(ctx, http.MethodPost, slow.URL()+jevtest.Path, bytes.NewReader(body))
	hreq.Header.Set("Authorization", "Bearer "+key)
	if _, err := http.DefaultClient.Do(hreq); err == nil {
		t.Fatal("a hang answered")
	}
}

// TestRandomFaultsDeterministic: the random faults of a request depend on
// its content and attempt only.
func TestRandomFaultsDeterministic(t *testing.T) {
	faults := jevtest.Faults{Seed: 5, RateLimit: 0.2, Server: 0.2, Malformed: 0.1, Missing: 0.1}
	run := func(order []int) map[string]jevtest.Fault {
		srv := jevtest.NewServer(jevtest.Options{APIKey: key, Faults: faults})
		defer srv.Close()
		var bodies []string
		for i, st := range states()[:20] {
			req, _ := decide.NewRequest(uint64(i), decide.LaneFast, 0, st, nil, 0)
			b, _ := decide.RequestBody("jev-1.13.0", req)
			bodies = append(bodies, string(b))
		}
		for _, i := range order {
			post(t, srv.URL()+jevtest.Path, "Bearer "+key, bodies[i])
		}
		out := map[string]jevtest.Fault{}
		for _, c := range srv.Calls() {
			out[c.Digest] = c.Fault
		}
		return out
	}
	fwd, rev := make([]int, 20), make([]int, 20)
	for i := range fwd {
		fwd[i], rev[i] = i, 19-i
	}
	a, b := run(fwd), run(rev)
	kinds := map[jevtest.Fault]int{}
	for d, f := range a {
		if b[d] != f {
			t.Fatalf("request %s: fault %v vs %v", d, f, b[d])
		}
		kinds[f]++
	}
	if len(kinds) < 3 {
		t.Fatalf("fault mix %v", kinds)
	}
}
