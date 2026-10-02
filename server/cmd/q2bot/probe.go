package main

import (
	"bufio"
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"quake2web/server/internal/agent/backend/jev"
	"quake2web/server/internal/agent/decide"
	"quake2web/server/internal/agent/trace"
)

// probeStates are the lane states jev-probe asks about by default: the
// decide package's golden fast and slow states (four enemies, two
// projectiles; the slow one with items, objective and events).
//
//go:embed probe_fast.json probe_slow.json
var probeStates embed.FS

// ProbeSchema names the jev-probe output format.
const ProbeSchema = "q2bot.jev-probe/1"

// probeDoc is what jev-probe writes: the requests' results and the jev
// client's recorded exchanges (request and response bodies, headers with
// the Authorization redacted).
type probeDoc struct {
	Schema    string            `json:"schema"`
	ProbedAt  string            `json:"probed_at"`
	Endpoint  string            `json:"endpoint"`
	Model     string            `json:"model"`
	Lane      string            `json:"lane"`
	Results   []probeResult     `json:"results"`
	Exchanges []json.RawMessage `json:"exchanges"`
}

type probeResult struct {
	Seq          uint64                 `json:"seq"`
	LatencyMs    float64                `json:"latency_ms"`
	Status       int                    `json:"status,omitempty"`
	Retries      int                    `json:"retries,omitempty"`
	Model        string                 `json:"model,omitempty"`
	InputTokens  int64                  `json:"input_tokens,omitempty"`
	OutputTokens int64                  `json:"output_tokens,omitempty"`
	CostUSD      float64                `json:"cost_usd,omitempty"`
	Unknown      int                    `json:"unknown_fields,omitempty"`
	Answers      map[string]probeAnswer `json:"answers,omitempty"`
	Err          string                 `json:"error,omitempty"`
}

type probeAnswer struct {
	Type          string             `json:"type"`
	Choice        string             `json:"choice,omitempty"`
	Noul          *float64           `json:"noul,omitempty"`
	Score         *float64           `json:"score,omitempty"`
	Probabilities map[string]float64 `json:"probabilities,omitempty"`
	Confidence    *float64           `json:"confidence,omitempty"`
}

// defaultProbeOut is the probe's output file: the jev package's testdata.
func defaultProbeOut() string {
	rel := filepath.Join("internal", "agent", "backend", "jev", "testdata", "live-probe.json")
	if root := repoRoot(); root != "" {
		return filepath.Join(root, "server", rel)
	}
	return rel
}

func runProbe(e *env, args []string) error {
	fs := newFlags("jev-probe", e)
	lane := fs.String("lane", "fast", "the lane whose state and questions are sent: fast or slow")
	n := fs.Int("n", 1, "requests to send (one after another)")
	out := fs.String("out", defaultProbeOut(), "where the exchange is written (the key redacted)")
	statePath := fs.String("state", "", "a lane state JSON to ask about (default: the decide package's golden state of the lane)")
	timeout := fs.Duration("timeout", 30*time.Second, "the probe's deadline")
	pos, err := parseFlags(fs, args)
	if err != nil {
		return err
	}
	switch {
	case len(pos) > 0:
		return fmt.Errorf("%w: unexpected argument %q", errUsage, pos[0])
	case *lane != "fast" && *lane != "slow":
		return fmt.Errorf("%w: -lane %q (fast|slow)", errUsage, *lane)
	case *n < 1 || *n > 100:
		return fmt.Errorf("%w: -n %d (1..100)", errUsage, *n)
	}
	if e.getenv(jev.EnvAPIKey) == "" {
		fmt.Fprintf(e.stderr, `q2bot jev-probe: %s is not set.

The probe sends %d real request(s) to the Jev API (about a thousand input
tokens each: well under a cent at $0.042 per million) and records the
exchange for the fixtures. Set the key in the environment only (never as a
flag), then run it again:

    export %s=...
    cd server && go run ./cmd/q2bot jev-probe

It writes %s (the key redacted). %s, %s and %s
select another endpoint or model.
`, jev.EnvAPIKey, *n, jev.EnvAPIKey, *out, jev.EnvBaseURL, jev.EnvAllowCustomBase, jev.EnvModel)
		return errNoKey
	}

	var st decide.State
	var raw []byte
	if *statePath != "" {
		raw, err = os.ReadFile(*statePath)
	} else {
		raw, err = probeStates.ReadFile("probe_" + *lane + ".json")
	}
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, &st); err != nil {
		return fmt.Errorf("lane state: %w", err)
	}
	l := decide.LaneFast
	if *lane == "slow" {
		l = decide.LaneSlow
	}

	cfg := jev.FromEnv(e.getenv)
	var rec lockedBuffer
	cfg.Recorder = &rec
	cfg.RatePerSec = -1
	cfg.Logf = func(format string, args ...any) { fmt.Fprintf(e.stderr, "jev: "+format+"\n", args...) }
	c, err := jev.New(cfg)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	doc := probeDoc{Schema: ProbeSchema, ProbedAt: time.Now().UTC().Format(time.RFC3339), Endpoint: c.Endpoint(), Model: c.Model(), Lane: *lane}
	fmt.Fprintf(e.stdout, "jev-probe: %s, model %s, %s lane\n", c.Endpoint(), c.Model(), *lane)
	ok := 0
	for i := 0; i < *n; i++ {
		req, err := decide.NewRequest(uint64(i+1), l, 0, &st, nil, 0)
		if err != nil {
			return err
		}
		t0 := time.Now()
		resp, err := c.Decide(ctx, req)
		res := probeResult{Seq: req.Seq, LatencyMs: float64(time.Since(t0)) / float64(time.Millisecond)}
		if err != nil {
			res.Err = err.Error()
			var je *jev.Error
			if errors.As(err, &je) {
				res.Status = je.Status
			}
			fmt.Fprintf(e.stdout, "  request %d: %.0f ms: %v\n", req.Seq, res.LatencyMs, err)
		} else {
			ok++
			res.Status, res.Retries, res.Model, res.CostUSD, res.Unknown = resp.Status, resp.Retries, resp.Model, resp.CostUSD, resp.Unknown
			res.InputTokens, res.OutputTokens = resp.Usage.InputTokens, resp.Usage.OutputTokens
			res.Answers = map[string]probeAnswer{}
			fmt.Fprintf(e.stdout, "  request %d: %.0f ms, status %d, model %s, %d input / %d output tokens, $%.8f, %d unknown fields\n",
				req.Seq, res.LatencyMs, res.Status, res.Model, res.InputTokens, res.OutputTokens, res.CostUSD, res.Unknown)
			for _, q := range req.Questions {
				a, present := resp.Answers[q.ID]
				if !present {
					fmt.Fprintf(e.stdout, "    %-11s (no answer)\n", q.ID)
					continue
				}
				pa := probeAnswer{Type: string(a.Type), Choice: a.Choice, Probabilities: a.Probabilities}
				switch q.Type {
				case decide.Noul:
					v := a.Noul
					pa.Noul = &v
				case decide.Score:
					v := a.Score
					pa.Score = &v
				}
				if a.HasConfidence {
					v := a.Confidence
					pa.Confidence = &v
				}
				res.Answers[q.ID] = pa
				fmt.Fprintf(e.stdout, "    %-11s %s\n", q.ID, describeAnswer(q, a))
			}
		}
		doc.Results = append(doc.Results, res)
	}
	sc := bufio.NewScanner(bytes.NewReader(rec.Bytes()))
	sc.Buffer(make([]byte, 1<<20), 16<<20)
	for sc.Scan() {
		if line := bytes.TrimSpace(sc.Bytes()); len(line) > 0 && json.Valid(line) {
			doc.Exchanges = append(doc.Exchanges, append(json.RawMessage(nil), line...))
		}
	}
	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	// the recorder redacts the Authorization header; this catches a key
	// a server echoed anywhere else
	b = []byte(trace.Redact(string(b), cfg.APIKey))
	if err := writeFileAtomic(*out, append(b, '\n')); err != nil {
		return err
	}
	fmt.Fprintf(e.stdout, "wrote %s (%d exchanges, the key redacted)\n", *out, len(doc.Exchanges))
	if ok == 0 {
		return errFailed
	}
	return nil
}

// describeAnswer formats an answer for the terminal.
func describeAnswer(q decide.Question, a decide.Answer) string {
	conf := ""
	if a.HasConfidence {
		conf = fmt.Sprintf(" confidence %.2f", a.Confidence)
	}
	switch q.Type {
	case decide.Noul:
		return fmt.Sprintf("noul %.3f", a.Noul)
	case decide.Score:
		return fmt.Sprintf("score %.2f%s %v", a.Score, conf, a.Probabilities)
	}
	return fmt.Sprintf("%s%s %v", a.Choice, conf, a.Probabilities)
}

// writeFileAtomic writes data to path through a temporary file (the
// directory is made when missing).
func writeFileAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// lockedBuffer is a Recorder buffer (the jev client writes from its
// calls).
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) Bytes() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]byte(nil), b.buf.Bytes()...)
}
