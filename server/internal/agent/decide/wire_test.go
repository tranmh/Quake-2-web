package decide

import (
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"
)

func docQuestions() []Question {
	return []Question{
		{ID: "department", Type: Choice, Instructions: "Which team should handle this?", Options: []Option{
			{"returns", "Exchanges, wrong or damaged items"}, {"shipping", "Delivery status, delays, lost packages"},
			{"billing", "Charges, invoices, payment problems"}}},
		{ID: "bug_severity", Type: Score, Instructions: "How severe is the reported issue?", Options: []Option{
			{"0", "Cosmetic; no impact to functionality"}, {"1", "Broken or degraded feature, but workaround exists"},
			{"2", "Blocking issue; no workaround exists"}}},
		{ID: "is_human_escalation", Type: Noul, Instructions: "Is the customer asking for a human agent?"},
		{ID: "is_repeat_contact", Type: Noul, Instructions: "Has the customer contacted support about this before?",
			Options: []Option{{"true", "Mentions a prior attempt"}, {"false", "No sign of any previous contact"}}},
	}
}

// TestQuestionEncoding: criteria objects keep the option order, score
// criteria are an array, a noul without criteria has none.
func TestQuestionEncoding(t *testing.T) {
	raw, err := MarshalQuestions(docQuestions())
	if err != nil {
		t.Fatal(err)
	}
	want := `{"department":{"type":"choice","instructions":"Which team should handle this?","criteria":{"returns":"Exchanges, wrong or damaged items","shipping":"Delivery status, delays, lost packages","billing":"Charges, invoices, payment problems"}},` +
		`"bug_severity":{"type":"score","instructions":"How severe is the reported issue?","criteria":["Cosmetic; no impact to functionality","Broken or degraded feature, but workaround exists","Blocking issue; no workaround exists"]},` +
		`"is_human_escalation":{"type":"noul","instructions":"Is the customer asking for a human agent?"},` +
		`"is_repeat_contact":{"type":"noul","instructions":"Has the customer contacted support about this before?","criteria":{"true":"Mentions a prior attempt","false":"No sign of any previous contact"}}}`
	if string(raw) != want {
		t.Fatalf("got  %s\nwant %s", raw, want)
	}
	if _, err := MarshalQuestions([]Question{{ID: "a"}, {ID: "a"}}); err == nil {
		t.Fatal("duplicate ids accepted")
	}
	for _, q := range docQuestions() {
		if err := q.Validate(); err != nil {
			t.Error(err)
		}
	}
	for _, q := range []Question{
		{ID: "x", Type: Choice, Instructions: "i"},
		{ID: "x", Type: Score, Instructions: "i", Options: []Option{{"0", "a"}}},
		{ID: "x", Type: Score, Instructions: "i", Options: []Option{{"1", "a"}, {"0", "b"}}},
		{ID: "x", Type: Noul, Instructions: "i", Options: []Option{{"maybe", "a"}}},
		{ID: "x", Type: "vote", Instructions: "i"},
		{ID: "x", Type: Choice, Options: []Option{{"a", ""}}},
		{ID: "x", Type: Choice, Instructions: "i", Options: []Option{{"a", ""}, {"a", ""}}},
	} {
		if q.Validate() == nil {
			t.Errorf("invalid question accepted: %+v", q)
		}
	}
}

// TestParseRequestBody round-trips a request body: the parsed questions
// re-encode identically and the digests agree.
func TestParseRequestBody(t *testing.T) {
	p := testProjector()
	b := testBelief()
	st := p.Project(LaneSlow, b, Context{Objective: testObjective()})
	req, err := NewRequest(3, LaneSlow, b.Time, &st, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	body, err := RequestBody("jev-1.13.0", req)
	if err != nil {
		t.Fatal(err)
	}
	w, err := ParseRequestBody(body)
	if err != nil {
		t.Fatal(err)
	}
	again, _ := MarshalQuestions(w.Questions)
	if w.Model != "jev-1.13.0" || string(again) != string(req.QuestionsJSON()) || string(w.State) != string(req.State) {
		t.Fatalf("round trip differs:\n%s\n%s", again, req.QuestionsJSON())
	}
	if w.Digest() != req.Digest() || len(req.Digest()) != 32 {
		t.Fatalf("digest %s vs %s", w.Digest(), req.Digest())
	}
	for _, bad := range []string{``, `[]`, `{"model":"m"}`, `{"model":"m","state":{},"questions":[]}`, `{"model":1,"state":{},"questions":{}}`,
		`{"model":"m","state":{},"questions":{"q":{"type":"choice","instructions":"i","criteria":7}}}`, `{"model":"m","state":{},"questions":{}} x`} {
		if _, err := ParseRequestBody([]byte(bad)); !errors.Is(err, ErrMalformed) {
			t.Errorf("%q: %v, want ErrMalformed", bad, err)
		}
	}
}

// TestDecodeDocumentedResponses decodes the docs' own examples.
func TestDecodeDocumentedResponses(t *testing.T) {
	qs := docQuestions()
	raw := `{"model":"jev-1.13.0","answers":{
		"department":{"type":"choice","choice":"returns","confidence":1.0,"probabilities":{"shipping":0.0,"returns":1.0,"billing":0.0}},
		"bug_severity":{"type":"score","score":1.43,"confidence":0.35,"legend":{"0":"Cosmetic; no impact to functionality","1":"Broken or degraded feature, but workaround exists","2":"Blocking issue; no workaround exists"},"probabilities":{"0":0.0,"1":0.57,"2":0.43}},
		"is_human_escalation":{"type":"noul","noul":0.99},
		"is_repeat_contact":{"type":"noul","noul":0.93}},
		"usage":{"input_tokens":328,"output_tokens":34}}`
	r, err := DecodeResponse([]byte(raw), qs)
	if err != nil {
		t.Fatal(err)
	}
	if r.Model != "jev-1.13.0" || r.Usage != (Usage{328, 34}) || r.Unknown != 0 || len(r.Answers) != 4 {
		t.Fatalf("response %+v", r)
	}
	d := r.Answers["department"]
	if d.Type != Choice || d.Choice != "returns" || !d.HasConfidence || d.Confidence != 1 || d.Probabilities["returns"] != 1 {
		t.Errorf("choice %+v", d)
	}
	s := r.Answers["bug_severity"]
	if s.Score != 1.43 || s.Confidence != 0.35 || s.Probabilities["1"] != 0.57 {
		t.Errorf("score %+v", s)
	}
	if n := r.Answers["is_human_escalation"]; n.Noul != 0.99 || n.HasConfidence {
		t.Errorf("noul %+v", n)
	}
}

// TestDecodeVariants: the tolerant decoder accepts minor variants and
// counts what it does not know.
func TestDecodeVariants(t *testing.T) {
	qs := docQuestions()
	for _, tc := range []struct {
		name, raw string
		check     func(t *testing.T, r *Response)
	}{
		{"array answers and pair probabilities", `{"answers":[{"id":"department","choice":"billing","probabilities":[["billing",0.7],["returns",0.3]],"confidence":"0.6"}]}`,
			func(t *testing.T, r *Response) {
				a := r.Answers["department"]
				if a.Type != Choice || a.Choice != "billing" || a.Confidence != 0.6 || a.Probabilities["returns"] != 0.3 {
					t.Errorf("%+v", a)
				}
			}},
		{"object probabilities list, no choice", `{"answers":{"department":{"probabilities":[{"option":"shipping","p":0.8},{"key":"billing","probability":0.2}]}}}`,
			func(t *testing.T, r *Response) {
				if a := r.Answers["department"]; a.Choice != "shipping" || a.HasConfidence {
					t.Errorf("%+v", a)
				}
			}},
		{"score from probabilities keyed by description", `{"answers":{"bug_severity":{"probabilities":{"Blocking issue; no workaround exists":0.5,"Cosmetic; no impact to functionality":0.5}}}}`,
			func(t *testing.T, r *Response) {
				if a := r.Answers["bug_severity"]; math.Abs(a.Score-1) > 1e-9 || a.Probabilities["2"] != 0.5 {
					t.Errorf("%+v", a)
				}
			}},
		{"noul variants", `{"answers":{"is_human_escalation":{"p":"0.8"},"is_repeat_contact":{"noul":{"true":0.3}}}}`,
			func(t *testing.T, r *Response) {
				if r.Answers["is_human_escalation"].Noul != 0.8 || r.Answers["is_repeat_contact"].Noul != 0.3 {
					t.Errorf("%+v", r.Answers)
				}
			}},
		{"bare values", `{"answers":{"department":"returns","is_human_escalation":true,"bug_severity":2}}`,
			func(t *testing.T, r *Response) {
				if r.Answers["department"].Choice != "returns" || r.Answers["is_human_escalation"].Noul != 1 || r.Answers["bug_severity"].Score != 2 {
					t.Errorf("%+v", r.Answers)
				}
			}},
		{"results key, usage variants, unknowns", `{"id":"x","results":{"department":{"answer":"returns","extra":1},"nope":{}},"usage":{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12,"cached":1}}`,
			func(t *testing.T, r *Response) {
				// unknown: top-level id, answer field extra, answer id nope, usage cached
				if r.Answers["department"].Choice != "returns" || r.Usage != (Usage{10, 2}) || r.Unknown != 4 {
					t.Errorf("answers %+v usage %+v unknown %d", r.Answers, r.Usage, r.Unknown)
				}
			}},
		{"type mismatch kept for the arbiter", `{"answers":{"department":{"type":"score","choice":"returns"}}}`,
			func(t *testing.T, r *Response) {
				if r.Answers["department"].Type != Score {
					t.Errorf("%+v", r.Answers["department"])
				}
			}},
		{"missing answers are absent", `{"answers":{}}`,
			func(t *testing.T, r *Response) {
				if len(r.Answers) != 0 {
					t.Errorf("%+v", r.Answers)
				}
			}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, err := DecodeResponse([]byte(tc.raw), qs)
			if err != nil {
				t.Fatal(err)
			}
			tc.check(t, r)
		})
	}
	for _, bad := range []string{``, `{"model":"m"}`, `{"answers":7}`, `{"answers":{`, `[1]`, `{"answers":{}} {}`} {
		if _, err := DecodeResponse([]byte(bad), qs); !errors.Is(err, ErrMalformed) {
			t.Errorf("%q: %v, want ErrMalformed", bad, err)
		}
	}
}

// TestMarshalResponseRoundTrip: encoded answers decode to themselves.
func TestMarshalResponseRoundTrip(t *testing.T) {
	qs := docQuestions()
	ans := map[string]Answer{
		"department":          {Type: Choice, Choice: "billing", Confidence: 0.7, HasConfidence: true, Probabilities: map[string]float64{"billing": 0.8, "returns": 0.2}},
		"bug_severity":        {Type: Score, Score: 1.25, Confidence: 0.5, HasConfidence: true, Probabilities: map[string]float64{"1": 0.75, "2": 0.25}},
		"is_human_escalation": {Type: Noul, Noul: 0.25},
		"unasked":             {Type: Choice, Choice: "x"},
	}
	raw, err := MarshalResponse("m", qs, ans, Usage{5, 1})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "unasked") || !json.Valid(raw) {
		t.Fatalf("%s", raw)
	}
	r, err := DecodeResponse(raw, qs)
	if err != nil {
		t.Fatal(err)
	}
	if r.Model != "m" || r.Usage != (Usage{5, 1}) || r.Unknown != 0 || len(r.Answers) != 3 {
		t.Fatalf("%+v", r)
	}
	for id, want := range ans {
		if id == "unasked" {
			continue
		}
		got := r.Answers[id]
		if got.Choice != want.Choice || got.Score != want.Score || got.Noul != want.Noul || got.Confidence != want.Confidence {
			t.Errorf("%s: %+v, want %+v", id, got, want)
		}
		for k, p := range want.Probabilities {
			if got.Probabilities[k] != p {
				t.Errorf("%s: p(%s) %v, want %v", id, k, got.Probabilities[k], p)
			}
		}
	}
	q := &qs[1]
	if a := OneHot(q, "2"); a.Score != 2 || a.Probabilities["2"] != 1 || a.Probabilities["0"] != 0 || a.Confidence != 1 {
		t.Errorf("one-hot score %+v", a)
	}
}
