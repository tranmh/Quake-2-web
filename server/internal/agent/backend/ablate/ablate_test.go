package ablate

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"testing"

	"quake2web/server/internal/agent/decide"
)

var (
	_ decide.DecisionBackend = (*Constant)(nil)
	_ decide.DecisionBackend = (*Random)(nil)
)

// requests returns a fast and a slow request about the decide package's
// golden lane states.
func requests(t *testing.T, seq uint64) []*decide.Request {
	t.Helper()
	var out []*decide.Request
	for i, f := range []string{"fast_state.golden.json", "slow_state.golden.json"} {
		raw, err := os.ReadFile("../../decide/testdata/" + f)
		if err != nil {
			t.Fatal(err)
		}
		var st decide.State
		if err := json.Unmarshal(raw, &st); err != nil {
			t.Fatal(err)
		}
		req, err := decide.NewRequest(seq+uint64(i), decide.Lane(i), 1000, &st, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		if len(req.Questions) < 3 {
			t.Fatalf("%s: %d questions", f, len(req.Questions))
		}
		out = append(out, req)
	}
	return out
}

// accepted runs the answers through an arbiter: every answered field must
// be taken as the backend's (no fallback reason).
func accepted(t *testing.T, req *decide.Request, resp *decide.Response) {
	t.Helper()
	a := decide.NewArbiter(decide.ArbiterConfig{})
	ap := a.Apply(decide.Result{Req: req, Resp: resp, Arrived: req.SnapTime})
	if ap.Dropped != "" || len(ap.Fields) != len(req.Questions) {
		t.Fatalf("applied %+v", ap)
	}
	for _, f := range ap.Fields {
		if f.Reason != "" || f.Source != decide.SourceModel {
			t.Fatalf("field %s: %+v", f.Field.ID(), f)
		}
	}
	// the raw wire response decodes to the same answers (traces replay)
	dec, err := decide.DecodeResponse(resp.Raw, req.Questions)
	if err != nil {
		t.Fatal(err)
	}
	for id, want := range resp.Answers {
		got := dec.Answers[id]
		if got.Choice != want.Choice || math.Abs(got.Score-want.Score) > 1e-9 {
			t.Fatalf("%s: raw decodes to %+v, answered %+v", id, got, want)
		}
	}
}

func TestConstant(t *testing.T) {
	c := NewConstant()
	for _, req := range requests(t, 1) {
		resp, err := c.Decide(context.Background(), req)
		if err != nil {
			t.Fatal(err)
		}
		if resp.Model != ConstantName || resp.Seq != req.Seq || len(resp.Answers) != len(req.Questions) {
			t.Fatalf("response %+v", resp)
		}
		for _, q := range req.Questions {
			a := resp.Answers[q.ID]
			first := q.Options[0].Key
			if a.Probabilities[first] != 1 || a.Confidence != 1 || q.Type == decide.Choice && a.Choice != first {
				t.Fatalf("%s: %+v, want the first option %q", q.ID, a, first)
			}
		}
		accepted(t, req, resp)
	}
	// configured keys win where the question offers them
	k := &Constant{Keys: map[string]string{decide.QFirePolicy: "suppress", decide.QMovement: "nope"}, Index: 99}
	req := requests(t, 1)[0]
	resp, _ := k.Decide(context.Background(), req)
	if got := resp.Answers[decide.QFirePolicy].Choice; got != "suppress" {
		t.Fatalf("fire_policy %q", got)
	}
	q := req.Question(decide.QMovement)
	if got := resp.Answers[decide.QMovement].Choice; got != q.Options[len(q.Options)-1].Key {
		t.Fatalf("movement %q: an unknown key falls back to the (clamped) index", got)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Decide(ctx, req); err == nil {
		t.Fatal("cancelled context answered")
	}
}

func TestRandom(t *testing.T) {
	r := NewRandom(7)
	counts := map[string]map[string]int{}
	for seq := uint64(1); seq <= 2000; seq += 2 {
		for _, req := range requests(t, seq) {
			resp, err := r.Decide(context.Background(), req)
			if err != nil {
				t.Fatal(err)
			}
			again, _ := NewRandom(7).Decide(context.Background(), req)
			if string(again.Raw) != string(resp.Raw) {
				t.Fatalf("request %d does not repeat", req.Seq)
			}
			if seq == 1 {
				accepted(t, req, resp)
			}
			for _, q := range req.Questions {
				a := resp.Answers[q.ID]
				sum := 0.0
				for _, o := range q.Options {
					p := a.Probabilities[o.Key]
					if math.Abs(p-1/float64(len(q.Options))) > 1e-12 {
						t.Fatalf("%s: probabilities %v are not uniform", q.ID, a.Probabilities)
					}
					sum += p
				}
				if math.Abs(sum-1) > 1e-9 || a.Confidence != 1 {
					t.Fatalf("%s: %+v", q.ID, a)
				}
				v := a.Choice
				if q.Type == decide.Score {
					v = q.Options[int(a.Score)].Key
				}
				if q.Index(v) < 0 {
					t.Fatalf("%s: answer %q is not an option", q.ID, v)
				}
				if counts[q.ID] == nil {
					counts[q.ID] = map[string]int{}
				}
				counts[q.ID][v]++
			}
		}
	}
	// every option comes up about equally often
	for _, req := range requests(t, 1) {
		for _, q := range req.Questions {
			n, total := len(q.Options), 0
			for _, c := range counts[q.ID] {
				total += c
			}
			for _, o := range q.Options {
				share := float64(counts[q.ID][o.Key]) / float64(total)
				if math.Abs(share-1/float64(n)) > 0.06 {
					t.Errorf("%s: option %s drawn %.3f of the time, want %.3f", q.ID, o.Key, share, 1/float64(n))
				}
			}
		}
	}
	// another seed draws differently
	req := requests(t, 1)[0]
	a, _ := NewRandom(7).Decide(context.Background(), req)
	diff := false
	for s := uint64(8); s < 20 && !diff; s++ {
		b, _ := NewRandom(s).Decide(context.Background(), req)
		diff = string(a.Raw) != string(b.Raw)
	}
	if !diff {
		t.Fatal("the seed does not matter")
	}
}

func TestNoul(t *testing.T) {
	q := decide.Question{ID: "dodge", Type: decide.Noul, Instructions: "dodge now?"}
	req := &decide.Request{Seq: 3, Lane: decide.LaneFast, State: json.RawMessage(`{}`), Questions: []decide.Question{q}}
	c, err := NewConstant().Decide(context.Background(), req)
	if err != nil || c.Answers["dodge"].Noul != 0 {
		t.Fatalf("constant noul %+v %v", c, err)
	}
	seen := map[float64]bool{}
	for seq := uint64(1); seq < 64; seq++ {
		req.Seq = seq
		r, err := NewRandom(1).Decide(context.Background(), req)
		if err != nil {
			t.Fatal(err)
		}
		seen[r.Answers["dodge"].Noul] = true
	}
	if !seen[0] || !seen[1] || len(seen) != 2 {
		t.Fatalf("random noul values %v", seen)
	}
}
