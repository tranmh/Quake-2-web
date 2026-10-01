package decide

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"quake2web/server/internal/agent/trace"
	"quake2web/server/internal/agent/worldmodel"
)

// Lane is a decision lane.
type Lane uint8

// Lanes.
const (
	LaneFast Lane = iota // combat: target, fire_policy, movement
	LaneSlow             // strategy: mode, weapon, pickup, danger
)

// NumLanes is the number of lanes.
const NumLanes = 2

// String returns "fast" or "slow".
func (l Lane) String() string {
	if l == LaneSlow {
		return "slow"
	}
	return "fast"
}

// MarshalText encodes the lane by name.
func (l Lane) MarshalText() ([]byte, error) { return []byte(l.String()), nil }

// UnmarshalText decodes "fast" or "slow".
func (l *Lane) UnmarshalText(b []byte) error {
	switch string(b) {
	case "fast":
		*l = LaneFast
	case "slow":
		*l = LaneSlow
	default:
		return fmt.Errorf("decide: unknown lane %q", b)
	}
	return nil
}

// QuestionType is a Jev question primitive.
type QuestionType string

// Question types.
const (
	Noul   QuestionType = "noul"   // yes/no: answer P(true), no confidence
	Choice QuestionType = "choice" // one of named options
	Score  QuestionType = "score"  // a continuous level on an ordered scale
)

// Limits of the question primitives (Jev docs).
const (
	MaxChoiceOptions = 255
	MinScoreLevels   = 2
	MaxScoreLevels   = 10
)

// Option is a choice option, a score level or a noul criterion. Key is the
// stable vocabulary key the backend answers with (for a score level, its
// zero-based number "0", "1", ...); Desc is what it means.
type Option struct {
	Key  string
	Desc string
}

// Question is one typed question. Options are in a fixed order: the order
// of a choice's criteria object, of a score's levels (Key = level number)
// and, for a noul, the optional "true"/"false" criteria.
type Question struct {
	ID           string
	Type         QuestionType
	Instructions string
	Options      []Option
}

// Index returns the position of option key (-1 if not an option).
func (q *Question) Index(key string) int {
	for i := range q.Options {
		if q.Options[i].Key == key {
			return i
		}
	}
	return -1
}

// Validate checks the question against the primitive's documented rules.
func (q *Question) Validate() error {
	if q.ID == "" {
		return errors.New("decide: question without an id")
	}
	if q.Instructions == "" {
		return fmt.Errorf("decide: question %s without instructions", q.ID)
	}
	seen := map[string]bool{}
	for _, o := range q.Options {
		if o.Key == "" || seen[o.Key] {
			return fmt.Errorf("decide: question %s: empty or duplicate option %q", q.ID, o.Key)
		}
		seen[o.Key] = true
	}
	switch q.Type {
	case Choice:
		if len(q.Options) < 1 || len(q.Options) > MaxChoiceOptions {
			return fmt.Errorf("decide: choice %s has %d options", q.ID, len(q.Options))
		}
	case Score:
		if len(q.Options) < MinScoreLevels || len(q.Options) > MaxScoreLevels {
			return fmt.Errorf("decide: score %s has %d levels", q.ID, len(q.Options))
		}
		for i, o := range q.Options {
			if o.Key != levelKey(i) {
				return fmt.Errorf("decide: score %s level %d has key %q", q.ID, i, o.Key)
			}
		}
	case Noul:
		for _, o := range q.Options {
			if o.Key != "true" && o.Key != "false" {
				return fmt.Errorf("decide: noul %s criterion %q", q.ID, o.Key)
			}
		}
	default:
		return fmt.Errorf("decide: question %s has type %q", q.ID, q.Type)
	}
	return nil
}

// Answer is a decoded answer to one question.
type Answer struct {
	Type QuestionType
	// Choice is the chosen option key (choice).
	Choice string
	// Noul is P(true) (noul).
	Noul float64
	// Score is the level, continuous between 0 and levels-1 (score).
	Score float64
	// Probabilities by option key (choice) or level number (score).
	Probabilities map[string]float64
	// Confidence is 0..1 (choice, score); HasConfidence reports whether the
	// backend sent one (a noul never has one).
	Confidence    float64
	HasConfidence bool
}

// Usage is the token usage of a request.
type Usage struct {
	InputTokens  int64
	OutputTokens int64
}

// OptionIndex maps a question id to its option keys in order.
type OptionIndex map[string][]string

// Has reports whether key is an option of question id.
func (x OptionIndex) Has(id, key string) bool {
	for _, k := range x[id] {
		if k == key {
			return true
		}
	}
	return false
}

// Request is one question set about one lane state. A Request is immutable
// once built: backends and the scheduler share it across goroutines.
type Request struct {
	Seq      uint64
	Lane     Lane
	SnapTime int64 // the belief's time (ms of the session clock)
	// State is the encoded lane state, as sent to the backend.
	State       json.RawMessage
	Questions   []Question
	OptionIndex OptionIndex
	// View is the typed lane state State encodes (never serialized).
	View *State `json:"-"`
	// Snapshot is the belief the state was projected from: an immutable
	// copy (never serialized, never modified).
	Snapshot *worldmodel.Belief `json:"-"`
}

// NewRequest encodes st (trimmed within maxBytes, see FitState) and asks
// the lane's questions about the trimmed state, which becomes View. snap
// may be nil.
func NewRequest(seq uint64, lane Lane, snapTime int64, st *State, snap *worldmodel.Belief, maxBytes int) (*Request, error) {
	fit, raw, err := FitState(*st, maxBytes)
	if err != nil {
		return nil, err
	}
	qs := Questions(lane, &fit)
	idx := OptionIndex{}
	for _, q := range qs {
		keys := make([]string, len(q.Options))
		for i, o := range q.Options {
			keys[i] = o.Key
		}
		idx[q.ID] = keys
	}
	return &Request{Seq: seq, Lane: lane, SnapTime: snapTime, State: raw, Questions: qs, OptionIndex: idx, View: &fit, Snapshot: snap}, nil
}

// Question returns the question with id (nil if not asked).
func (r *Request) Question(id string) *Question {
	for i := range r.Questions {
		if r.Questions[i].ID == id {
			return &r.Questions[i]
		}
	}
	return nil
}

// QuestionsJSON returns the questions as the backend receives them (the
// "questions" object of the request body).
func (r *Request) QuestionsJSON() json.RawMessage {
	b, err := MarshalQuestions(r.Questions)
	if err != nil {
		panic("decide: questions not encodable: " + err.Error())
	}
	return b
}

// Digest identifies the request's content (state and questions, not its
// Seq): 32 hex characters (trace.Digest of {"questions", "state"}).
func (r *Request) Digest() string {
	return RequestDigest(r.State, r.QuestionsJSON())
}

// RequestDigest is Request.Digest for an encoded state and questions
// object (as recorded in a trace).
func RequestDigest(state, questions json.RawMessage) string {
	d, err := trace.Digest(struct {
		Questions json.RawMessage `json:"questions"`
		State     json.RawMessage `json:"state"`
	}{questions, state})
	if err != nil {
		return ""
	}
	return d
}

// Response is a backend's answers to a Request.
type Response struct {
	Seq     uint64
	Answers map[string]Answer
	// Model is the model id the backend reports (e.g. "jev-1.13.0").
	Model   string
	Latency time.Duration
	Usage   Usage
	CostUSD float64
	// Raw is the backend's response body (the documented wire format).
	Raw json.RawMessage
	// Unknown counts fields and answers the tolerant decoder did not
	// recognize.
	Unknown int
	// Status is the HTTP status of a remote backend's answer (0 for a
	// local backend); Retries the attempts it took beyond the first.
	Status  int
	Retries int
}

// DecisionBackend answers question sets. Decide must not modify the
// request, must honor ctx, and must be safe for concurrent use (the
// realtime scheduler runs several calls at once).
type DecisionBackend interface {
	Name() string
	Decide(ctx context.Context, req *Request) (*Response, error)
}

// levelKey is the key of score level i.
func levelKey(i int) string { return fmt.Sprint(i) }
