package runner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"quake2web/server/internal/agent/backend/jev"
	"quake2web/server/internal/agent/budget"
	"quake2web/server/internal/agent/metrics"
	"quake2web/server/internal/agent/trace"
	"quake2web/server/internal/api"
	"quake2web/server/internal/spectate"
)

// BotFile is the bot.json the Manager keeps in each of its run
// directories: who started the bot, its settings and how it ended.
const BotFile = "bot.json"

// BotSchema is the schema of bot.json.
const BotSchema = "q2bot.bot/1"

// botMeta is bot.json.
type botMeta struct {
	Schema     string     `json:"schema"`
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	OwnerID    int64      `json:"owner_id"`
	OwnerAdmin bool       `json:"owner_admin,omitempty"` // started by an administrator (see ManagerConfig.KeepPerUser)
	Public     bool       `json:"public"`
	Backend    string     `json:"backend"`
	Model      string     `json:"model,omitempty"`
	Maps       []string   `json:"maps"`
	Skill      int        `json:"skill"`
	Status     string     `json:"status"`
	Reason     string     `json:"reason,omitempty"`
	StartedAt  time.Time  `json:"started_at"`
	EndedAt    *time.Time `json:"ended_at,omitempty"`
}

// botRun is a bot's run: *Runner (tests use a stand-in).
type botRun interface {
	Run(ctx context.Context) (*metrics.RunSummary, error)
	Bus() *trace.Bus
}

// managedBot is one live bot of a Manager.
type managedBot struct {
	id       string
	password string
	dir      string
	run      botRun
	stream   *spectate.Stream
	hub      *spectate.Hub
	stats    *liveStats
	feeds    int64          // decision feed connections (guarded by mu)
	ips      map[string]int // stream connections by stream and address (guarded by mu)

	// tickets are the bot's stream tickets issued and not redeemed yet,
	// oldest first (see Manager.issueTickets).
	tmu     sync.Mutex
	tickets []pendingTicket

	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{} // closed once the run has ended and bot.json is final

	mu      sync.Mutex
	meta    botMeta
	stopWhy string
}

func (b *managedBot) status() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.meta.Status
}

// markRunning: the bot entered its first level.
func (b *managedBot) markRunning() {
	b.mu.Lock()
	if b.meta.Status == api.BotStarting {
		b.meta.Status = api.BotRunning
	}
	b.mu.Unlock()
}

// stop asks the run to end (the first reason wins).
func (b *managedBot) stop(why string) {
	b.mu.Lock()
	if b.stopWhy == "" {
		b.stopWhy = why
	}
	b.mu.Unlock()
	b.cancel()
}

func (b *managedBot) snapshot() botMeta {
	b.mu.Lock()
	defer b.mu.Unlock()
	m := b.meta
	m.Maps = append([]string(nil), m.Maps...)
	return m
}

// newBot prepares a bot: its run (directory, backend), its relay and its
// bot.json. The run does not start yet.
func (m *Manager) newBot(fs FileSystem, spec api.BotSpec, u api.BotUser, name string, visits []string, skill int, lat Latency) (*managedBot, error) {
	id, err := m.newID()
	if err != nil {
		return nil, err
	}
	b := &managedBot{id: id, password: randomHex(12), dir: m.runDir(id), done: make(chan struct{})}
	b.ctx, b.cancel = context.WithCancel(context.Background())
	model := ""
	if modelBackend(spec.Backend) {
		model = m.cfg.Jev.Model
	}
	b.meta = botMeta{Schema: BotSchema, ID: id, Name: name, OwnerID: u.ID, OwnerAdmin: u.Admin, Public: spec.Public,
		Backend: spec.Backend, Model: model, Maps: visits, Skill: skill, Status: api.BotStarting, StartedAt: m.cfg.Now().UTC()}
	b.stats = &liveStats{backend: spec.Backend, metrics: m.metrics, onLevel: b.markRunning}
	logf := func(format string, args ...any) {
		m.log.Debug(strings.TrimRight(fmt.Sprintf(format, args...), "\n"), "bot", id)
	}
	b.stream = spectate.NewStream()
	b.hub = spectate.NewHub(b.stream, spectate.HubConfig{MaxViewers: m.cfg.MaxViewers, Global: m.viewers,
		Metrics: relayMetrics{m.metrics}, Logf: logf})

	rc := Config{
		FS: fs, Campaign: m.camp, Maps: visits, Skill: &skill,
		Backend: spec.Backend, Session: SessionInProc, SimLatency: lat,
		Episodes: 1, Seed: randomSeed(),
		OutDir: m.cfg.Dir, RunID: id, Trace: m.cfg.Trace, Record: true,
		Library: m.library(fs, skill), NavDir: m.cfg.NavDir,
		OnServerMessage: b.stream.OnServerMessage,
		Userinfo:        botUserinfo(name, b.password),
		NewInstance:     m.newGame(b),
		Logf:            logf,
		Now:             m.cfg.Now,
	}
	if modelBackend(spec.Backend) {
		rc.Budget = budget.Limits{USD: m.cfg.BudgetUSDPerRun}
		rc.Jev = jev.Config{Model: m.cfg.Jev.Model}
	}
	if spec.Backend == BackendJev {
		// only real spend counts against the account (mock answers are free)
		rc.Jev = m.cfg.Jev
		rc.Account = m.account
	}
	newRun := m.newRun
	if newRun == nil {
		newRun = func(c Config) (botRun, error) { return New(c) }
	}
	r, err := newRun(rc)
	if err != nil {
		_ = b.hub.Close()
		b.cancel()
		switch {
		case errors.Is(err, ErrConfig):
			return nil, fmt.Errorf("%w: %s", api.ErrBotInvalid, strings.TrimPrefix(err.Error(), ErrConfig.Error()+": "))
		case spec.Backend == BackendJev:
			m.log.Error("bots: jev backend unavailable", "err", err)
			return nil, fmt.Errorf("%w: the jev backend is misconfigured", api.ErrBotsDisabled)
		}
		return nil, err
	}
	b.run = r
	r.Bus().AddSink(b.stats)
	if err := os.MkdirAll(b.dir, 0o755); err != nil {
		b.discard()
		return nil, err
	}
	if err := metrics.WriteJSON(filepath.Join(b.dir, BotFile), b.meta); err != nil {
		b.discard()
		return nil, err
	}
	return b, nil
}

// discard drops a bot that was prepared but never ran. Its run is run on
// a cancelled context, which ends it at once and releases its backend
// (a mock server, say).
func (b *managedBot) discard() {
	b.cancel()
	_ = b.hub.Close()
	_, _ = runSafely(b.ctx, b.run)
	_ = os.RemoveAll(b.dir)
}

// runBot plays b's run to its end, then finalizes bot.json, the metrics
// and the retention.
func (m *Manager) runBot(b *managedBot) {
	defer m.wg.Done()
	ctx, cancel := context.WithTimeout(b.ctx, m.cfg.MaxRun)
	defer cancel()
	sum, err := runSafely(ctx, b.run)
	_ = b.hub.Close()
	limit := errors.Is(ctx.Err(), context.DeadlineExceeded)

	b.mu.Lock()
	status, reason := endStatus(sum, err, b.stopWhy, limit, m.cfg.MaxRun)
	now := m.cfg.Now().UTC()
	b.meta.Status, b.meta.Reason, b.meta.EndedAt = status, reason, &now
	meta := b.meta
	b.mu.Unlock()
	if werr := metrics.WriteJSON(filepath.Join(b.dir, BotFile), meta); werr != nil {
		m.log.Error("bots: bot.json not written", "bot", b.id, "err", werr)
	}
	m.metrics.runEnded(status)
	lvl := slogLevel(status)
	m.log.Log(context.Background(), lvl, "bot ended", "bot", b.id, "status", status, "reason", reason, "err", err)

	m.mu.Lock()
	delete(m.bots, b.id)
	m.mu.Unlock()
	close(b.done)
	b.cancel()
	m.prune()
}

// runSafely runs r, turning a panic of the run into an error: a bot must
// not take the API server down.
func runSafely(ctx context.Context, r botRun) (sum *metrics.RunSummary, err error) {
	defer func() {
		if v := recover(); v != nil {
			sum, err = nil, fmt.Errorf("runner: bot run panicked: %v", v)
			r.Bus().Close()
		}
	}()
	return r.Run(ctx)
}

// endStatus maps a run's end to a bot status and reason.
func endStatus(sum *metrics.RunSummary, err error, stopWhy string, limit bool, maxRun time.Duration) (string, string) {
	switch {
	case stopWhy != "":
		return api.BotStopped, stopWhy
	case limit:
		return api.BotStopped, fmt.Sprintf("its wall-clock limit (%s) was reached", maxRun)
	case sum == nil:
		if err == nil {
			err = errors.New("no summary")
		}
		return api.BotFailed, firstLine(err.Error())
	}
	switch sum.Outcome {
	case OutcomeCompleted:
		if err != nil && !errors.Is(err, ErrIncomplete) {
			return api.BotFailed, firstLine(err.Error())
		}
		return api.BotFinished, sum.Reason
	case OutcomeAborted:
		return api.BotStopped, sum.Reason
	}
	reason := sum.Reason
	if reason == "" && err != nil {
		reason = firstLine(err.Error())
	}
	return api.BotFailed, reason
}

func firstLine(s string) string {
	s, _, _ = strings.Cut(s, "\n")
	if len(s) > 300 {
		s = s[:300]
	}
	return s
}

// liveInfo describes a live bot.
func (m *Manager) liveInfo(b *managedBot, withSummary bool) api.BotInfo {
	meta := b.snapshot()
	info := infoOf(meta)
	info.Viewers = b.hub.Viewers()
	info.Level, info.Live = b.stats.snapshot()
	info.Artifacts = artifacts(b.dir, true)
	if withSummary {
		if raw, err := os.ReadFile(filepath.Join(b.dir, RunFile)); err == nil {
			info.Summary = raw // a progress snapshot (written after each episode)
		}
	}
	return info
}

func infoOf(meta botMeta) api.BotInfo {
	info := api.BotInfo{ID: meta.ID, Name: meta.Name, OwnerID: meta.OwnerID, Status: meta.Status, Reason: meta.Reason,
		Backend: meta.Backend, Model: meta.Model, Maps: meta.Maps, Skill: meta.Skill, Public: meta.Public,
		StartedAt: meta.StartedAt, EndedAt: meta.EndedAt, Artifacts: []api.BotArtifactInfo{}}
	if info.Maps == nil {
		info.Maps = []string{}
	}
	return info
}

// gateFieldSet are the decision fields of the model share (the run's
// provenance gate fields).
var gateFieldSet = map[string]bool{"target": true, "fire_policy": true, "mode": true}

// latencyWindow is how many answered requests apiP50Ms looks back on.
const latencyWindow = 128

// liveStats is the bus sink of a live bot's counters (BotInfo.Live, the
// decision feed's stats) and Prometheus metrics. It runs under the bus
// lock in the bot's goroutines, so it only counts.
type liveStats struct {
	backend string
	metrics *ManagerMetrics
	onLevel func()

	mu        sync.Mutex
	level     *api.BotLevel
	kills     int
	deaths    int
	decisions int // requests answered or failed
	ticks     int
	gateModel int // gate field ticks acted on the model's answer
	gateTotal int
	answered  int // requests with an answer
	stale     int // of them, arrived after their TTL
	cost      float64
	lat       [latencyWindow]float64
	latN      int
}

// Write implements trace.Sink.
func (s *liveStats) Write(e trace.Event) error {
	var d *trace.Decision
	if e.Type == trace.TypeDecision {
		var dd trace.Decision
		if decodeBody(&e, &dd) == nil {
			d = &dd
		}
	}
	s.metrics.event(s.backend, &e, d)
	first := false
	s.mu.Lock()
	switch e.Type {
	case trace.TypeLevelStart:
		var ls trace.LevelStart
		_ = decodeBody(&e, &ls)
		first = s.level == nil
		s.level = &api.BotLevel{Map: e.Map, Visit: ls.Visit}
	case trace.TypeKill:
		s.kills++
	case trace.TypeDeath:
		s.deaths++
	case trace.TypeDecision:
		if d == nil {
			break
		}
		switch d.Lane {
		case trace.LaneFast, trace.LaneSlow:
			s.decisions++
			s.cost += d.CostUSD
			if d.Err == "" && len(d.Response) > 0 {
				s.answered++
				if d.Stale {
					s.stale++
				}
				s.lat[s.latN%latencyWindow] = d.LatencyMs
				s.latN++
			}
		case trace.LaneTick:
			s.ticks++
			if d.Intent != nil {
				for _, f := range d.Intent.Fields {
					if gateFieldSet[f.Name] {
						s.gateTotal++
						if f.Source == trace.SourceModel {
							s.gateModel++
						}
					}
				}
			}
		}
	}
	s.mu.Unlock()
	if first && s.onLevel != nil {
		s.onLevel()
	}
	return nil
}

// liveCounters are a snapshot of liveStats.
type liveCounters struct {
	Kills, Deaths, Decisions int
	ModelShare, StaleRate    float64
	APIP50Ms, CostUSD        float64
}

func (s *liveStats) counters() liveCounters {
	s.mu.Lock()
	defer s.mu.Unlock()
	c := liveCounters{Kills: s.kills, Deaths: s.deaths, Decisions: s.decisions, CostUSD: s.cost}
	if s.gateTotal > 0 {
		c.ModelShare = float64(s.gateModel) / float64(s.gateTotal)
	}
	if s.answered > 0 {
		c.StaleRate = float64(s.stale) / float64(s.answered)
	}
	if n := min(s.latN, latencyWindow); n > 0 {
		v := append([]float64(nil), s.lat[:n]...)
		sort.Float64s(v)
		c.APIP50Ms = v[(n-1)/2]
	}
	return c
}

// snapshot returns BotInfo's Level and Live.
func (s *liveStats) snapshot() (*api.BotLevel, *api.BotLiveStats) {
	c := s.counters()
	s.mu.Lock()
	var lvl *api.BotLevel
	if s.level != nil {
		l := *s.level
		lvl = &l
	}
	s.mu.Unlock()
	return lvl, &api.BotLiveStats{Kills: c.Kills, Deaths: c.Deaths, Decisions: c.Decisions, CostUSD: roundUSD(c.CostUSD), ModelShare: round4(c.ModelShare)}
}

// decodeBody decodes e's body into v, without a JSON round trip when the
// body already has v's type (the events of a live bus).
func decodeBody[T any](e *trace.Event, v *T) error {
	switch b := e.Body.(type) {
	case T:
		*v = b
		return nil
	case *T:
		if b != nil {
			*v = *b
			return nil
		}
	}
	return e.DecodeBody(v)
}
