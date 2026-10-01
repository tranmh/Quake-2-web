package decide

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
)

// The wire format is Jev's (docs.typesafe.ai, POST /v1/systemone):
//
//	request  {"model": m, "state": <state>, "questions": {id: question}}
//	question {"type": "choice", "instructions": s, "criteria": {key: desc}}
//	         {"type": "score",  "instructions": s, "criteria": [desc, ...]}
//	         {"type": "noul",   "instructions": s, "criteria": {"true": d, "false": d}}
//	response {"model": m, "answers": {id: answer}, "usage": {"input_tokens": n, "output_tokens": n}}
//	answer   {"type": "choice", "choice": key, "probabilities": {key: p}, "confidence": c}
//	         {"type": "score", "score": x, "legend": {"0": d}, "probabilities": {"0": p}, "confidence": c}
//	         {"type": "noul", "noul": p}
//
// Objects are written in the questions' option order (Go maps would sort
// them); decoding is tolerant of the variants listed at DecodeResponse.

// ErrMalformed is wrapped by decoding errors of a response or request body.
var ErrMalformed = errors.New("decide: malformed body")

// MarshalJSON encodes the question in the wire format.
func (q Question) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString(`{"type":`)
	writeJSON(&buf, string(q.Type))
	buf.WriteString(`,"instructions":`)
	writeJSON(&buf, q.Instructions)
	switch q.Type {
	case Score:
		buf.WriteString(`,"criteria":[`)
		for i, o := range q.Options {
			if i > 0 {
				buf.WriteByte(',')
			}
			writeJSON(&buf, o.Desc)
		}
		buf.WriteByte(']')
	case Choice, Noul:
		if len(q.Options) > 0 {
			buf.WriteString(`,"criteria":`)
			writeOptions(&buf, q.Options)
		}
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

func writeJSON(buf *bytes.Buffer, v any) {
	b, err := marshal(v)
	if err != nil {
		panic("decide: " + err.Error()) // strings and numbers always encode
	}
	buf.Write(b)
}

func writeOptions(buf *bytes.Buffer, opts []Option) {
	buf.WriteByte('{')
	for i, o := range opts {
		if i > 0 {
			buf.WriteByte(',')
		}
		writeJSON(buf, o.Key)
		buf.WriteByte(':')
		writeJSON(buf, o.Desc)
	}
	buf.WriteByte('}')
}

// MarshalQuestions encodes questions as the request's "questions" object,
// in order.
func MarshalQuestions(qs []Question) (json.RawMessage, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	seen := map[string]bool{}
	for i, q := range qs {
		if seen[q.ID] {
			return nil, fmt.Errorf("decide: duplicate question %q", q.ID)
		}
		seen[q.ID] = true
		if i > 0 {
			buf.WriteByte(',')
		}
		writeJSON(&buf, q.ID)
		buf.WriteByte(':')
		b, _ := q.MarshalJSON()
		buf.Write(b)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// RequestBody encodes the request body for model.
func RequestBody(model string, req *Request) ([]byte, error) {
	qs, err := MarshalQuestions(req.Questions)
	if err != nil {
		return nil, err
	}
	if !json.Valid(req.State) {
		return nil, errors.New("decide: request state is not JSON")
	}
	var buf bytes.Buffer
	buf.WriteString(`{"model":`)
	writeJSON(&buf, model)
	buf.WriteString(`,"state":`)
	buf.Write(req.State)
	buf.WriteString(`,"questions":`)
	buf.Write(qs)
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// WireRequest is a decoded request body.
type WireRequest struct {
	Model     string
	State     json.RawMessage
	Questions []Question // in the body's order
	// QuestionsJSON is the raw "questions" object.
	QuestionsJSON json.RawMessage
}

// Digest is RequestDigest of the body's state and questions: equal to
// Request.Digest of the request it encodes.
func (w *WireRequest) Digest() string {
	qs, err := MarshalQuestions(w.Questions)
	if err != nil {
		return ""
	}
	var st bytes.Buffer
	if err := json.Compact(&st, w.State); err != nil {
		return ""
	}
	return RequestDigest(st.Bytes(), qs)
}

// ParseRequestBody decodes a request body, keeping the order of the
// questions and of their criteria. Instructions or criteria given as
// structured objects are kept as their JSON text.
func ParseRequestBody(body []byte) (*WireRequest, error) {
	top, err := orderedObject(body)
	if err != nil {
		return nil, err
	}
	w := &WireRequest{}
	var haveState, haveQuestions bool
	for _, kv := range top {
		switch kv.key {
		case "model":
			if err := json.Unmarshal(kv.val, &w.Model); err != nil {
				return nil, fmt.Errorf("%w: model: %v", ErrMalformed, err)
			}
		case "state":
			w.State, haveState = kv.val, true
		case "questions":
			haveQuestions = true
			w.QuestionsJSON = kv.val
			qs, err := orderedObject(kv.val)
			if err != nil {
				return nil, fmt.Errorf("questions: %w", err)
			}
			for _, qv := range qs {
				q, err := parseQuestion(qv.key, qv.val)
				if err != nil {
					return nil, err
				}
				w.Questions = append(w.Questions, q)
			}
		}
	}
	if !haveState || !haveQuestions {
		return nil, fmt.Errorf("%w: missing state or questions", ErrMalformed)
	}
	return w, nil
}

func parseQuestion(id string, raw json.RawMessage) (Question, error) {
	fields, err := orderedObject(raw)
	if err != nil {
		return Question{}, fmt.Errorf("question %s: %w", id, err)
	}
	q := Question{ID: id}
	var criteria json.RawMessage
	for _, kv := range fields {
		switch kv.key {
		case "type":
			var t string
			if err := json.Unmarshal(kv.val, &t); err != nil {
				return q, fmt.Errorf("%w: question %s type", ErrMalformed, id)
			}
			q.Type = QuestionType(t)
		case "instructions":
			q.Instructions = textOf(kv.val)
		case "criteria":
			criteria = kv.val
		}
	}
	switch c := bytes.TrimSpace(criteria); {
	case len(c) == 0 || string(c) == "null":
	case c[0] == '{':
		opts, err := orderedObject(c)
		if err != nil {
			return q, fmt.Errorf("question %s criteria: %w", id, err)
		}
		for _, o := range opts {
			q.Options = append(q.Options, Option{Key: o.key, Desc: textOf(o.val)})
		}
	case c[0] == '[':
		var items []json.RawMessage
		if err := json.Unmarshal(c, &items); err != nil {
			return q, fmt.Errorf("%w: question %s criteria", ErrMalformed, id)
		}
		for i, it := range items {
			q.Options = append(q.Options, Option{Key: levelKey(i), Desc: textOf(it)})
		}
	default:
		return q, fmt.Errorf("%w: question %s criteria", ErrMalformed, id)
	}
	return q, nil
}

// textOf returns a JSON string's value, or the compact JSON text of any
// other value.
func textOf(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var buf bytes.Buffer
	if json.Compact(&buf, raw) == nil {
		return buf.String()
	}
	return string(raw)
}

type keyVal struct {
	key string
	val json.RawMessage
}

// orderedObject decodes a JSON object into its members in order.
func orderedObject(raw []byte) ([]keyVal, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	t, err := dec.Token()
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrMalformed, err)
	}
	if d, ok := t.(json.Delim); !ok || d != '{' {
		return nil, fmt.Errorf("%w: not a JSON object", ErrMalformed)
	}
	var out []keyVal
	for dec.More() {
		t, err := dec.Token()
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrMalformed, err)
		}
		k, _ := t.(string)
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return nil, fmt.Errorf("%w: %v", ErrMalformed, err)
		}
		out = append(out, keyVal{k, v})
	}
	if _, err := dec.Token(); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrMalformed, err)
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, fmt.Errorf("%w: trailing data", ErrMalformed)
	}
	return out, nil
}

// number decodes a JSON number, a numeric string or a boolean (1/0).
func number(raw json.RawMessage) (float64, bool) {
	var f float64
	if json.Unmarshal(raw, &f) == nil {
		return f, !math.IsNaN(f) && !math.IsInf(f, 0)
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
		return f, err == nil && !math.IsNaN(f) && !math.IsInf(f, 0)
	}
	var b bool
	if json.Unmarshal(raw, &b) == nil {
		if b {
			return 1, true
		}
		return 0, true
	}
	return 0, false
}

func stringOf(raw json.RawMessage) (string, bool) {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s, true
	}
	return "", false
}

// DecodeResponse decodes a response body for the questions it answers.
// It follows the documented fields and tolerates variants, counting what
// it does not recognize in Response.Unknown:
//
//   - "answers" may be an object by question id or an array of answers
//     carrying "id" (or "question", "question_id", "name"); "results" is
//     accepted for "answers";
//   - an answer's type may be missing (the question's type is used);
//   - choice: "choice", or "answer", "label", "option"; a missing choice is
//     the most probable option;
//   - noul: "noul", or "p", "probability", "value"; a number, a numeric
//     string, a boolean or an object {"p": x} / {"true": x};
//   - score: "score", or "value", "level"; a missing score is the
//     probability-weighted mean level;
//   - probabilities: an object, an array of {option|key|label|level: k,
//     p|prob|probability|value: x}, or of [k, x] pairs; score levels may be
//     keyed by their description;
//   - confidence: "confidence" or "conf", a number or numeric string;
//   - usage: input_tokens/output_tokens or prompt_tokens/completion_tokens.
//
// Answers to unknown question ids are counted and dropped. Validation of
// the values (options, ranges) is the Arbiter's.
func DecodeResponse(raw []byte, qs []Question) (*Response, error) {
	top, err := orderedObject(raw)
	if err != nil {
		return nil, err
	}
	r := &Response{Raw: append(json.RawMessage(nil), raw...), Answers: map[string]Answer{}}
	saw := false
	for _, kv := range top {
		switch kv.key {
		case "model":
			if s, ok := stringOf(kv.val); ok {
				r.Model = s
			} else {
				r.Unknown++
			}
		case "answers", "results":
			saw = true
			if err := decodeAnswers(kv.val, qs, r); err != nil {
				return nil, err
			}
		case "usage":
			decodeUsage(kv.val, r)
		default:
			r.Unknown++
		}
	}
	if !saw {
		return nil, fmt.Errorf("%w: no answers", ErrMalformed)
	}
	return r, nil
}

func findQuestion(qs []Question, id string) *Question {
	for i := range qs {
		if qs[i].ID == id {
			return &qs[i]
		}
	}
	return nil
}

func decodeAnswers(raw json.RawMessage, qs []Question, r *Response) error {
	switch c := bytes.TrimSpace(raw); {
	case len(c) > 0 && c[0] == '{':
		members, err := orderedObject(c)
		if err != nil {
			return err
		}
		for _, kv := range members {
			q := findQuestion(qs, kv.key)
			if q == nil {
				r.Unknown++
				continue
			}
			if a, ok := decodeAnswer(kv.val, q, r, ""); ok {
				r.Answers[q.ID] = a
			}
		}
	case len(c) > 0 && c[0] == '[':
		var items []json.RawMessage
		if err := json.Unmarshal(c, &items); err != nil {
			return fmt.Errorf("%w: answers", ErrMalformed)
		}
		for _, it := range items {
			fields, err := orderedObject(it)
			if err != nil {
				r.Unknown++
				continue
			}
			var id, idKey string
			for _, kv := range fields {
				switch kv.key {
				case "id", "question", "question_id", "name":
					if s, ok := stringOf(kv.val); ok && id == "" {
						id, idKey = s, kv.key
					}
				}
			}
			q := findQuestion(qs, id)
			if q == nil {
				r.Unknown++
				continue
			}
			if a, ok := decodeAnswer(it, q, r, idKey); ok {
				r.Answers[q.ID] = a
			}
		}
	default:
		return fmt.Errorf("%w: answers is not an object", ErrMalformed)
	}
	return nil
}

// decodeAnswer decodes one answer to q (skip names the id field of an
// array answer).
func decodeAnswer(raw json.RawMessage, q *Question, r *Response, skip string) (Answer, bool) {
	fields, err := orderedObject(raw)
	if err != nil {
		// a bare value: a choice key or a number
		a := Answer{Type: q.Type}
		switch q.Type {
		case Choice:
			s, ok := stringOf(raw)
			a.Choice = s
			return a, ok
		case Noul:
			p, ok := number(raw)
			a.Noul = p
			return a, ok
		case Score:
			x, ok := number(raw)
			a.Score = x
			return a, ok
		}
		return a, false
	}
	a := Answer{Type: q.Type}
	var probs json.RawMessage
	haveValue := false
	for _, kv := range fields {
		k := kv.key
		if k == skip {
			continue
		}
		switch {
		case k == "type":
			if s, ok := stringOf(kv.val); ok {
				a.Type = QuestionType(s)
			}
		case k == "probabilities" || k == "probs" || k == "distribution":
			probs = kv.val
		case k == "confidence" || k == "conf":
			if c, ok := number(kv.val); ok {
				a.Confidence, a.HasConfidence = c, true
			}
		case k == "legend":
		case q.Type == Choice && (k == "choice" || k == "answer" || k == "label" || k == "option"):
			if s, ok := stringOf(kv.val); ok {
				a.Choice, haveValue = s, true
			}
		case q.Type == Noul && (k == "noul" || k == "p" || k == "probability" || k == "value"):
			if p, ok := noulValue(kv.val); ok {
				a.Noul, haveValue = p, true
			}
		case q.Type == Score && (k == "score" || k == "value" || k == "level"):
			if x, ok := number(kv.val); ok {
				a.Score, haveValue = x, true
			}
		default:
			r.Unknown++
		}
	}
	if probs != nil {
		a.Probabilities = decodeProbs(probs, q, r)
	}
	if !haveValue && len(a.Probabilities) > 0 {
		switch q.Type {
		case Choice:
			best := -1.0
			for _, o := range q.Options {
				if p, ok := a.Probabilities[o.Key]; ok && p > best {
					a.Choice, best = o.Key, p
				}
			}
			haveValue = best >= 0
		case Score:
			var sum, w float64
			for i := range q.Options {
				p := a.Probabilities[levelKey(i)]
				sum += p * float64(i)
				w += p
			}
			if w > 0 {
				a.Score, haveValue = sum/w, true
			}
		}
	}
	return a, haveValue
}

func noulValue(raw json.RawMessage) (float64, bool) {
	if p, ok := number(raw); ok {
		return p, true
	}
	fields, err := orderedObject(raw)
	if err != nil {
		return 0, false
	}
	for _, kv := range fields {
		if kv.key == "p" || kv.key == "true" || kv.key == "probability" {
			return number(kv.val)
		}
	}
	return 0, false
}

// decodeProbs decodes probabilities keyed by option key (or, for a score,
// level number or description).
func decodeProbs(raw json.RawMessage, q *Question, r *Response) map[string]float64 {
	out := map[string]float64{}
	key := func(k string) string {
		if q.Type == Score && q.Index(k) < 0 {
			for i, o := range q.Options {
				if o.Desc == k {
					return levelKey(i)
				}
			}
			if f, err := strconv.ParseFloat(k, 64); err == nil && f == math.Trunc(f) {
				return levelKey(int(f))
			}
		}
		return k
	}
	switch c := bytes.TrimSpace(raw); {
	case len(c) > 0 && c[0] == '{':
		members, err := orderedObject(c)
		if err != nil {
			r.Unknown++
			return nil
		}
		for _, kv := range members {
			if p, ok := number(kv.val); ok {
				out[key(kv.key)] = p
			} else {
				r.Unknown++
			}
		}
	case len(c) > 0 && c[0] == '[':
		var items []json.RawMessage
		if json.Unmarshal(c, &items) != nil {
			r.Unknown++
			return nil
		}
		for _, it := range items {
			var pair []json.RawMessage
			if json.Unmarshal(it, &pair) == nil && len(pair) == 2 {
				k, ok1 := stringOf(pair[0])
				if !ok1 {
					if f, ok := number(pair[0]); ok {
						k, ok1 = strconv.FormatFloat(f, 'f', -1, 64), true
					}
				}
				if p, ok := number(pair[1]); ok1 && ok {
					out[key(k)] = p
					continue
				}
				r.Unknown++
				continue
			}
			fields, err := orderedObject(it)
			if err != nil {
				r.Unknown++
				continue
			}
			var k string
			var p float64
			var hk, hp bool
			for _, kv := range fields {
				switch kv.key {
				case "option", "key", "label", "choice", "level", "name":
					if s, ok := stringOf(kv.val); ok {
						k, hk = s, true
					} else if f, ok := number(kv.val); ok {
						k, hk = strconv.FormatFloat(f, 'f', -1, 64), true
					}
				case "p", "prob", "probability", "value":
					p, hp = number(kv.val)
				}
			}
			if hk && hp {
				out[key(k)] = p
			} else {
				r.Unknown++
			}
		}
	default:
		r.Unknown++
		return nil
	}
	return out
}

func decodeUsage(raw json.RawMessage, r *Response) {
	fields, err := orderedObject(raw)
	if err != nil {
		r.Unknown++
		return
	}
	for _, kv := range fields {
		n, ok := number(kv.val)
		if !ok {
			r.Unknown++
			continue
		}
		switch kv.key {
		case "input_tokens", "prompt_tokens", "inputTokens":
			r.Usage.InputTokens = int64(n)
		case "output_tokens", "completion_tokens", "outputTokens":
			r.Usage.OutputTokens = int64(n)
		case "total_tokens":
		default:
			r.Unknown++
		}
	}
}

// MarshalResponse encodes answers in the documented response format, in
// the questions' order (answers to other ids are left out). Choice
// probabilities list every option; a score's legend and probabilities
// list every level.
func MarshalResponse(model string, qs []Question, answers map[string]Answer, usage Usage) (json.RawMessage, error) {
	var buf bytes.Buffer
	buf.WriteString(`{"model":`)
	writeJSON(&buf, model)
	buf.WriteString(`,"answers":{`)
	n := 0
	for i := range qs {
		q := &qs[i]
		a, ok := answers[q.ID]
		if !ok {
			continue
		}
		if n > 0 {
			buf.WriteByte(',')
		}
		n++
		writeJSON(&buf, q.ID)
		buf.WriteByte(':')
		if err := writeAnswer(&buf, q, &a); err != nil {
			return nil, err
		}
	}
	buf.WriteString(`},"usage":{"input_tokens":`)
	writeJSON(&buf, usage.InputTokens)
	buf.WriteString(`,"output_tokens":`)
	writeJSON(&buf, usage.OutputTokens)
	buf.WriteString(`}}`)
	return buf.Bytes(), nil
}

func finite(f float64) float64 {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return 0
	}
	return f
}

func writeAnswer(buf *bytes.Buffer, q *Question, a *Answer) error {
	switch q.Type {
	case Noul:
		buf.WriteString(`{"type":"noul","noul":`)
		writeJSON(buf, finite(a.Noul))
		buf.WriteByte('}')
	case Choice:
		buf.WriteString(`{"type":"choice","choice":`)
		writeJSON(buf, a.Choice)
		buf.WriteString(`,"confidence":`)
		writeJSON(buf, finite(a.Confidence))
		buf.WriteString(`,"probabilities":{`)
		for i, o := range q.Options {
			if i > 0 {
				buf.WriteByte(',')
			}
			writeJSON(buf, o.Key)
			buf.WriteByte(':')
			writeJSON(buf, finite(a.Probabilities[o.Key]))
		}
		buf.WriteString(`}}`)
	case Score:
		buf.WriteString(`{"type":"score","score":`)
		writeJSON(buf, finite(a.Score))
		buf.WriteString(`,"confidence":`)
		writeJSON(buf, finite(a.Confidence))
		buf.WriteString(`,"legend":`)
		writeOptions(buf, q.Options)
		buf.WriteString(`,"probabilities":{`)
		for i, o := range q.Options {
			if i > 0 {
				buf.WriteByte(',')
			}
			writeJSON(buf, o.Key)
			buf.WriteByte(':')
			writeJSON(buf, finite(a.Probabilities[o.Key]))
		}
		buf.WriteString(`}}`)
	default:
		return fmt.Errorf("decide: question %s has type %q", q.ID, q.Type)
	}
	return nil
}

// OneHot returns a confident answer for option key of q (for a score, the
// level key): probability 1 on it, confidence 1.
func OneHot(q *Question, key string) Answer {
	a := Answer{Type: q.Type, Probabilities: map[string]float64{}, Confidence: 1, HasConfidence: true}
	for _, o := range q.Options {
		a.Probabilities[o.Key] = 0
	}
	a.Probabilities[key] = 1
	switch q.Type {
	case Choice:
		a.Choice = key
	case Score:
		if i := q.Index(key); i >= 0 {
			a.Score = float64(i)
		}
	case Noul:
		a.HasConfidence, a.Confidence = false, 0
		if key == "true" {
			a.Noul = 1
		}
	}
	return a
}
