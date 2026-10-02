package runner

import (
	"bytes"
	"encoding/json"
	"math"
	"sort"
	"strings"

	"quake2web/server/internal/agent/decide"
	"quake2web/server/internal/agent/trace"
	"quake2web/server/internal/q2const"
)

// DecisionsProtocol names the decision feed's protocol: JSON text frames
// on GET /ws/v1/bots/{id}/decisions, one message each, told apart by "t":
//
//	{"t":"hello","bot":id,"backend":b,"model":m,"maps":[...]}   first
//	{"t":"decision","sf":frame,"lvl":n,"map":m,"mode":...}       one per decision tick (about 10 Hz)
//	{"t":"event","kind":k,"sf":frame,"lvl":n,"map":m,"data":{}}  level_start, level_end, death, reload, kill, damage, stuck, budget
//	{"t":"stats","kills":...,"costUsd":...}                       about once a second
//	{"t":"gap","dropped":n}                                       n trace events were lost (the viewer fell behind)
//	{"t":"bye","status":s}                                        last: the run ended with bot status s
//
// A decision carries the mode the bot executed, its route objective, its
// target (with the class and distance the bot last believed), the
// backend's latest answer to every question (options with probabilities,
// the chosen option, the confidence), the provenance of every field it
// acted on (model, scripted, stale, reflex, default) with the fallback
// reasons, the action (movement, fire policy, weapon in hand, whether it
// fired since the previous decision), the latency of the latest answer
// and the cost of the requests answered since the previous decision. Its
// sf aligns it with the video stream's frames.
const DecisionsProtocol = "q2bot.decisions/1"

// Decision feed messages.
type (
	feedHello struct {
		T       string   `json:"t"`
		Bot     string   `json:"bot"`
		Backend string   `json:"backend"`
		Model   string   `json:"model,omitempty"`
		Maps    []string `json:"maps"`
	}
	feedDecision struct {
		T          string            `json:"t"`
		SF         int32             `json:"sf"`
		Lvl        int               `json:"lvl"`
		Map        string            `json:"map"`
		Mode       string            `json:"mode"`
		Objective  string            `json:"objective"`
		Target     *feedTarget       `json:"target"`
		Questions  []feedQuestion    `json:"questions"`
		Provenance map[string]string `json:"provenance"`
		Fallback   map[string]string `json:"fallback,omitempty"`
		Action     feedAction        `json:"action"`
		LatencyMs  float64           `json:"latencyMs"`
		CostUSD    float64           `json:"costUsd"`
	}
	feedTarget struct {
		ID    string `json:"id"`
		Class string `json:"class"`
		Dist  int    `json:"dist"`
	}
	feedQuestion struct {
		ID         string       `json:"id"`
		Options    []feedOption `json:"options"`
		Chosen     string       `json:"chosen"`
		Confidence *float64     `json:"confidence,omitempty"`
	}
	feedOption struct {
		Label string  `json:"label"`
		P     float64 `json:"p"`
	}
	feedAction struct {
		Movement   string `json:"movement"`
		FirePolicy string `json:"firePolicy"`
		Weapon     string `json:"weapon"`
		Fire       bool   `json:"fire"`
	}
	feedEvent struct {
		T    string          `json:"t"`
		Kind string          `json:"kind"`
		SF   int32           `json:"sf"`
		Lvl  int             `json:"lvl"`
		Map  string          `json:"map"`
		Data json.RawMessage `json:"data"`
	}
	feedStats struct {
		T            string  `json:"t"`
		Kills        int     `json:"kills"`
		Deaths       int     `json:"deaths"`
		Decisions    int     `json:"decisions"`
		DecisionRate float64 `json:"decisionRate"`
		ModelShare   float64 `json:"modelShare"`
		StaleRate    float64 `json:"staleRate"`
		APIP50Ms     float64 `json:"apiP50Ms"`
		CostUSD      float64 `json:"costUsd"`
	}
	feedGap struct {
		T       string `json:"t"`
		Dropped uint64 `json:"dropped"`
	}
	feedBye struct {
		T      string `json:"t"`
		Status string `json:"status"`
	}
)

// feedEventKinds are the trace events the feed forwards as "event".
var feedEventKinds = map[string]bool{
	trace.TypeLevelStart: true, trace.TypeLevelEnd: true, trace.TypeDeath: true, trace.TypeReload: true,
	trace.TypeKill: true, trace.TypeDamage: true, trace.TypeStuck: true, trace.TypeBudget: true,
}

// questionOrder is the order of a decision's questions (the trace's field
// order); other questions follow by id.
var questionOrder = map[string]int{"mode": 0, "target": 1, "fire_policy": 2, "movement": 3, "weapon": 4, "pickup": 5, "danger": 6}

// maxFeedOptions bounds the options a question shows.
const maxFeedOptions = 16

// feedTranslator turns a bot's trace events into decision feed messages.
// It keeps what the tick events do not carry: the backend's latest
// answers (from the request events' raw responses and questions), the
// latest lane states' route objective and enemies. It is used by one
// goroutine.
type feedTranslator struct {
	answers   map[string]feedQuestion
	objective string
	enemies   map[string]decide.Enemy
	latencyMs float64
	cost      float64
}

func newFeedTranslator() *feedTranslator {
	return &feedTranslator{answers: map[string]feedQuestion{}, enemies: map[string]decide.Enemy{}}
}

// translate returns the messages for e (none for most events).
func (f *feedTranslator) translate(e *trace.Event) []any {
	switch {
	case e.Type == trace.TypeDecision:
		var d trace.Decision
		if decodeBody(e, &d) != nil {
			return nil
		}
		switch d.Lane {
		case trace.LaneFast, trace.LaneSlow:
			f.request(&d)
		case trace.LaneTick:
			return []any{f.decision(e, &d)}
		}
	case feedEventKinds[e.Type]:
		if e.Type == trace.TypeLevelStart {
			// a new level (or a reload of it): the old answers and states
			// were about another situation
			f.answers, f.enemies, f.objective = map[string]feedQuestion{}, map[string]decide.Enemy{}, ""
		}
		return []any{feedEvent{T: "event", Kind: e.Type, SF: e.SF, Lvl: e.Lvl, Map: e.Map, Data: eventData(e)}}
	}
	return nil
}

// request takes in an answered (or failed) request.
func (f *feedTranslator) request(d *trace.Decision) {
	f.cost += d.CostUSD
	if len(d.State) > 0 {
		var st decide.State
		if json.Unmarshal(d.State, &st) == nil {
			f.enemies = map[string]decide.Enemy{}
			for _, en := range st.Enemies {
				f.enemies[en.ID] = en
			}
			if d.Lane == trace.LaneSlow {
				f.objective = ""
				if st.Objective != nil {
					f.objective = st.Objective.Desc
				}
			}
		}
	}
	if d.Err != "" || len(d.Response) == 0 {
		return
	}
	f.latencyMs = d.LatencyMs
	if d.Stale || len(d.Questions) == 0 {
		return // not applied, or no questions to read the answers with
	}
	qs, err := parseQuestions(d.Questions)
	if err != nil {
		return
	}
	resp, err := decide.DecodeResponse(d.Response, qs)
	if err != nil {
		return
	}
	for i := range qs {
		if a, ok := resp.Answers[qs[i].ID]; ok {
			f.answers[qs[i].ID] = questionView(&qs[i], &a)
		}
	}
}

// parseQuestions decodes a trace's questions object (the wire format).
func parseQuestions(raw json.RawMessage) ([]decide.Question, error) {
	var body bytes.Buffer
	body.WriteString(`{"model":"","state":{},"questions":`)
	body.Write(raw)
	body.WriteByte('}')
	w, err := decide.ParseRequestBody(body.Bytes())
	if err != nil {
		return nil, err
	}
	return w.Questions, nil
}

// questionView is one answered question for the feed.
func questionView(q *decide.Question, a *decide.Answer) feedQuestion {
	v := feedQuestion{ID: q.ID, Options: []feedOption{}}
	switch q.Type {
	case decide.Noul:
		p := clamp01(a.Noul)
		v.Options = append(v.Options, feedOption{Label: "true", P: round4(p)}, feedOption{Label: "false", P: round4(1 - p)})
		v.Chosen = "false"
		if p >= 0.5 {
			v.Chosen = "true"
		}
		c := round4(math.Abs(2*p - 1)) // a noul has no confidence: its decisiveness
		v.Confidence = &c
		return v
	case decide.Score:
		for i, o := range q.Options {
			if i >= maxFeedOptions {
				break
			}
			v.Options = append(v.Options, feedOption{Label: levelLabel(o), P: round4(a.Probabilities[o.Key])})
		}
		lvl := int(math.Round(a.Score))
		if lvl >= 0 && lvl < len(q.Options) {
			v.Chosen = levelLabel(q.Options[lvl])
		}
	default:
		for i, o := range q.Options {
			if i >= maxFeedOptions {
				break
			}
			v.Options = append(v.Options, feedOption{Label: o.Key, P: round4(a.Probabilities[o.Key])})
		}
		v.Chosen = a.Choice
	}
	if a.HasConfidence {
		c := round4(a.Confidence)
		v.Confidence = &c
	}
	return v
}

// levelLabel is a score level's short name: its description up to the
// first colon ("safe: no threat" is "safe"), else its number.
func levelLabel(o decide.Option) string {
	if name, _, ok := strings.Cut(o.Desc, ":"); ok && name != "" && len(name) <= 24 {
		return strings.TrimSpace(name)
	}
	return o.Key
}

func clamp01(p float64) float64 {
	if math.IsNaN(p) {
		return 0
	}
	return math.Max(0, math.Min(1, p))
}

// round4 keeps four decimals (the feed is for display).
func round4(x float64) float64 {
	if math.IsNaN(x) || math.IsInf(x, 0) {
		return 0
	}
	return math.Round(x*1e4) / 1e4
}

// roundUSD keeps a cost to a nano dollar, dropping float summation noise.
func roundUSD(x float64) float64 {
	if math.IsNaN(x) || math.IsInf(x, 0) {
		return 0
	}
	return math.Round(x*1e9) / 1e9
}

// decision builds the message of a lane tick event.
func (f *feedTranslator) decision(e *trace.Event, d *trace.Decision) feedDecision {
	msg := feedDecision{T: "decision", SF: e.SF, Lvl: e.Lvl, Map: e.Map, Objective: f.objective,
		Questions: []feedQuestion{}, Provenance: map[string]string{}, LatencyMs: round4(f.latencyMs), CostUSD: roundUSD(f.cost)}
	f.cost = 0
	if d.Intent != nil {
		msg.Mode = d.Intent.Mode
		msg.Action.FirePolicy = d.Intent.FirePolicy
		for _, fd := range d.Intent.Fields {
			msg.Provenance[fd.Name] = fd.Source
			if fd.Fallback != "" {
				if msg.Fallback == nil {
					msg.Fallback = map[string]string{}
				}
				msg.Fallback[fd.Name] = fd.Fallback
			}
			if fd.Name == "fire_policy" {
				msg.Action.FirePolicy = fd.Value
			}
		}
	}
	msg.Action.Movement = "nav"
	if tk := d.Tick; tk != nil {
		msg.Mode = tk.Mode
		msg.Action.Weapon = tk.Weapon
		if tk.Move != "" {
			msg.Action.Movement = tk.Move
		}
		if tk.Target != "" {
			t := &feedTarget{ID: tk.Target}
			if en, ok := f.enemies[tk.Target]; ok {
				t.Class, t.Dist = en.Class, en.Units
			}
			msg.Target = t
		}
	}
	for _, c := range d.Cmds {
		if c.Buttons&q2const.BUTTON_ATTACK != 0 {
			msg.Action.Fire = true
		}
	}
	ids := make([]string, 0, len(f.answers))
	for id := range f.answers {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		oi, iok := questionOrder[ids[i]]
		oj, jok := questionOrder[ids[j]]
		switch {
		case iok && jok:
			return oi < oj
		case iok != jok:
			return iok
		}
		return ids[i] < ids[j]
	})
	for _, id := range ids {
		msg.Questions = append(msg.Questions, f.answers[id])
	}
	return msg
}

// eventData returns an event's body fields as a JSON object.
func eventData(e *trace.Event) json.RawMessage {
	b, err := json.Marshal(e)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	var all map[string]json.RawMessage
	if json.Unmarshal(b, &all) != nil {
		return json.RawMessage(`{}`)
	}
	for _, k := range []string{"v", "type", "run", "ep", "seq", "wall", "gms", "lvl", "map", "sf"} {
		delete(all, k)
	}
	out, err := json.Marshal(all)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return out
}

// statsMessage is a stats message from a bot's live counters;
// decisionRate is over the interval since the previous one.
func statsMessage(c liveCounters, rate float64) feedStats {
	return feedStats{T: "stats", Kills: c.Kills, Deaths: c.Deaths, Decisions: c.Decisions, DecisionRate: round4(rate),
		ModelShare: round4(c.ModelShare), StaleRate: round4(c.StaleRate), APIP50Ms: round4(c.APIP50Ms), CostUSD: roundUSD(c.CostUSD)}
}

// marshalFeed encodes a feed message (no HTML escaping: the feed is not
// HTML).
func marshalFeed(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}
