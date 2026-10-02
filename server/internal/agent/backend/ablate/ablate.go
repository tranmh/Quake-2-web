// Package ablate holds the ablation backends: decision backends that
// answer the decide lanes' questions without looking at the state, to show
// that the decisions matter (a run on them must do measurably worse than
// on the scripted policy or the model).
//
//   - Constant always answers the same option of each question: by default
//     the first one (hold fire, advance, fight, the first enemy or item,
//     danger "safe"), or a configured key.
//   - Random answers a uniformly random option, with uniform
//     probabilities (a noul: a coin), seeded by the request (its sequence
//     number and content), so a lockstep run repeats exactly.
//
// Both report full confidence so the arbiter acts on their answers
// rather than falling back to the scripted policy. With uniform
// probabilities the arbiter's hysteresis, which compares the
// probabilities of the current and the new value, keeps Random's first
// mode and target until they are lost; the other fields follow its draws.
//
// Responses carry the documented wire format (decide.MarshalResponse), so
// traces of ablation runs replay like any other.
package ablate

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"math/rand"

	"quake2web/server/internal/agent/decide"
)

// Names of the backends (and the model ids they report).
const (
	ConstantName = "constant"
	RandomName   = "random"
)

// Constant answers every question with the same option.
type Constant struct {
	// Keys picks the option by question id (an option key; a key the
	// question does not offer falls back to Index).
	Keys map[string]string
	// Index is the option's position for the other questions, clamped to
	// the options (0: the first).
	Index int
}

// NewConstant returns the constant backend answering the first option of
// every question.
func NewConstant() *Constant { return &Constant{} }

// Name implements decide.DecisionBackend.
func (c *Constant) Name() string { return ConstantName }

// Decide implements decide.DecisionBackend.
func (c *Constant) Decide(ctx context.Context, req *decide.Request) (*decide.Response, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	answers := map[string]decide.Answer{}
	for i := range req.Questions {
		q := &req.Questions[i]
		key := ""
		switch k, ok := c.Keys[q.ID]; {
		case ok && (q.Index(k) >= 0 || q.Type == decide.Noul && (k == "true" || k == "false")):
			key = k
		case len(q.Options) > 0:
			key = q.Options[min(max(c.Index, 0), len(q.Options)-1)].Key
		case q.Type == decide.Noul:
			key = "false"
		default:
			continue
		}
		answers[q.ID] = decide.OneHot(q, key)
	}
	return respond(ConstantName, req, answers)
}

// Random answers every question with a uniformly random option.
type Random struct {
	// Seed seeds the draws (with the request's Seq and digest).
	Seed uint64
}

// NewRandom returns the random backend.
func NewRandom(seed uint64) *Random { return &Random{Seed: seed} }

// Name implements decide.DecisionBackend.
func (r *Random) Name() string { return RandomName }

// Decide implements decide.DecisionBackend.
func (r *Random) Decide(ctx context.Context, req *decide.Request) (*decide.Response, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	rng := rand.New(rand.NewSource(int64(r.seed(req))))
	answers := map[string]decide.Answer{}
	for i := range req.Questions {
		q := &req.Questions[i]
		if q.Type == decide.Noul {
			// a yes/no drawn by a coin; a noul has no probabilities to
			// spread (0.5 would be no answer at all)
			key := "false"
			if rng.Intn(2) == 1 {
				key = "true"
			}
			answers[q.ID] = decide.OneHot(q, key)
			continue
		}
		n := len(q.Options)
		if n == 0 {
			continue
		}
		key := q.Options[rng.Intn(n)].Key
		a := decide.OneHot(q, key)
		for _, o := range q.Options {
			a.Probabilities[o.Key] = 1 / float64(n)
		}
		answers[q.ID] = a
	}
	return respond(RandomName, req, answers)
}

// seed is the request's draw seed: the backend seed, the Seq and the
// content digest, mixed.
func (r *Random) seed(req *decide.Request) uint64 {
	h := decide.Mix64(r.Seed ^ decide.Mix64(req.Seq+1))
	if d, err := hex.DecodeString(req.Digest()); err == nil && len(d) >= 8 {
		h = decide.Mix64(h ^ binary.LittleEndian.Uint64(d))
	}
	return h
}

func respond(model string, req *decide.Request, answers map[string]decide.Answer) (*decide.Response, error) {
	raw, err := decide.MarshalResponse(model, req.Questions, answers, decide.Usage{})
	if err != nil {
		return nil, err
	}
	return &decide.Response{Seq: req.Seq, Answers: answers, Model: model, Raw: raw}, nil
}
