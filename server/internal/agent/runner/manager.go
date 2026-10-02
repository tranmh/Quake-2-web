package runner

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/prometheus/client_golang/prometheus"

	"quake2web/server/internal/agent/backend/jev"
	"quake2web/server/internal/agent/budget"
	"quake2web/server/internal/agent/campaign"
	"quake2web/server/internal/agent/route"
	"quake2web/server/internal/agent/session"
	"quake2web/server/internal/agent/trace"
	"quake2web/server/internal/api"
	"quake2web/server/internal/auth"
	"quake2web/server/internal/host"
	"quake2web/server/internal/qcommon/shared"
	"quake2web/server/internal/spectate"
)

// GameRunner creates and stops the realtime games the Manager's bots play
// in: *host.Games, the API server's instance manager.
type GameRunner interface {
	// CreateWithID starts a game with the given instance id.
	CreateWithID(ctx context.Context, id string, spec api.GameSpec) (string, error)
	// Stop stops a game.
	Stop(ctx context.Context, id string) error
	// Host is the host running the games' instances.
	Host() *host.Host
}

// Manager defaults (ManagerConfig zero values).
const (
	DefaultMaxBots         = 4
	DefaultMaxBotsPerUser  = 1
	DefaultMaxViewers      = 8
	DefaultMaxViewersTotal = 64
	DefaultMaxViewersPerIP = 4
	DefaultMaxRun          = 30 * time.Minute
	DefaultStopWait        = 15 * time.Second
	DefaultKeepPerUser     = 10
	// DefaultStartsPerUser bots per DefaultStartWindow is how often an
	// account that is not an administrator may start bots.
	DefaultStartsPerUser = 10
	DefaultStartWindow   = 10 * time.Minute
	// DefaultSkill is a bot's skill unless its spec sets one.
	DefaultSkill = 1
	// MaxSimLatency bounds a bot spec's simulated latency samples.
	MaxSimLatency = 5 * time.Second
	// spendFlush is how often the account's spend is persisted.
	spendFlush = 30 * time.Second
	// maxBotName bounds a bot's name (runes).
	maxBotName = 64
)

// Ticket scopes: the "game id" of the one-time tickets (auth.TicketService)
// of a bot's live streams.
func watchScope(id string) string     { return "bot-watch:" + id }
func decisionsScope(id string) string { return "bot-decisions:" + id }

// gameID is the instance id of a bot's game for episode ep.
func gameID(id string, ep int) string {
	if ep == 0 {
		return "bot-" + id
	}
	return fmt.Sprintf("bot-%s-%d", id, ep)
}

// ManagerConfig configures a Manager.
type ManagerConfig struct {
	// Games runs the bots' realtime games (required).
	Games GameRunner
	// FS returns the game data the runs read, the files of the pakset the
	// games load (required; called at every Start).
	FS func(ctx context.Context) (FileSystem, error)
	// Pakset is the pakset id of the games ("": api.DemoPaksetID).
	Pakset string
	// Tickets issues and redeems the watch tickets (required for Watch).
	Tickets auth.TicketService
	// PublicWSURL is the base of the returned WebSocket URLs (""
	// relative ones), as for games.
	PublicWSURL string

	// Dir holds the run directories (required); Keep is how many ended
	// runs are kept, newest first (0: all). Of them KeepPerUser
	// (DefaultKeepPerUser) at most are one account's, so that nobody
	// evicts the others' runs by starting bots in a loop; the runs of
	// administrators, of the server and of the command line only count
	// against Keep.
	Dir         string
	Keep        int
	KeepPerUser int
	// MaxBots bounds the live bots, all owners (DefaultMaxBots);
	// MaxPerUser those of one account (DefaultMaxBotsPerUser;
	// administrators are exempt).
	MaxBots    int
	MaxPerUser int
	// StartLimiter bounds how often an account that is not an
	// administrator starts bots, keyed by its id (nil:
	// DefaultStartsPerUser per DefaultStartWindow).
	StartLimiter *auth.Limiter
	// AllowUsers lets accounts that are not administrators start bots
	// with a local backend (never jev).
	AllowUsers bool
	// MaxViewers bounds a bot's viewers of each stream (DefaultMaxViewers);
	// MaxViewersTotal the video viewers of every bot together
	// (DefaultMaxViewersTotal); MaxViewersPerIP the connections of one
	// address (RemoteIP) to each stream of a bot (DefaultMaxViewersPerIP),
	// so that one client does not take every slot.
	MaxViewers      int
	MaxViewersTotal int
	MaxViewersPerIP int
	// MaxRun is a bot's wall-clock limit (DefaultMaxRun).
	MaxRun time.Duration
	// StopWait bounds how long Stop and Close wait for a run to end
	// (DefaultStopWait).
	StopWait time.Duration

	// Jev configures the jev backend: the key, the base URL and the
	// pinned model (jev.Config; the model is also used by mock bots).
	// Without a key jev bots are refused.
	Jev jev.Config
	// BudgetUSDPerRun caps a model-backed run's spend (0: none);
	// DailyUSD the spend of every jev bot per UTC day (0: none), persisted
	// through SpendStore (nil: in memory); JevMaxQPS the requests per
	// second of every jev bot together (0: none).
	BudgetUSDPerRun float64
	DailyUSD        float64
	SpendStore      budget.SpendStore
	JevMaxQPS       float64

	// Campaign is the campaign the bots play (nil: route.Load(RoutesDir));
	// RoutesDir its directory ("": campaign.DefaultRoutesDir()); NavDir
	// the nav cache ("": nav.DefaultDir()).
	Campaign  *route.Campaign
	RoutesDir string
	NavDir    string
	// Trace is the decision events' detail (zero: TraceFull, which the
	// decision feed needs for the questions and probabilities).
	Trace TraceMode

	// RequireTickets and OriginPatterns are the WebSocket endpoints'
	// rules, as host.Host's: without RequireTickets (development) a viewer
	// without a ticket may watch a public bot; a ticket given must be
	// valid. RemoteIP names a viewer's address (nil: RemoteAddr's host).
	RequireTickets bool
	OriginPatterns []string
	RemoteIP       func(r *http.Request) string

	// Log receives the Manager's and the runs' diagnostics (nil:
	// slog.Default()).
	Log *slog.Logger
	// Now is the wall clock (nil: time.Now).
	Now func() time.Time
}

// Manager runs the API server's AI bots (it implements api.BotHost): each
// bot is a runner.Runner on an InProc session in a realtime
// single-player game of the server's own host (OwnerID 0, so the game is
// never idle-reaped and outside the per-owner game limits; a random
// password keeps players out), recorded into a run directory under Dir.
// The bot's server messages feed a spectate.Stream with a Hub for the
// live viewers (and the run's .dm2 recorder); its trace bus feeds the run
// directory, the run's metrics, the Manager's live counters and
// Prometheus metrics, and the decision feeds.
//
// Run directories written by the q2bot command line into Dir are listed
// as bots too (public, without an owner). Ended runs beyond Keep (or an
// account's beyond KeepPerUser) are deleted, oldest first.
type Manager struct {
	cfg     ManagerConfig
	log     *slog.Logger
	camp    *route.Campaign
	account *budget.Account
	metrics *ManagerMetrics
	viewers *spectate.ViewerLimit
	feeds   atomic.Int64 // decision feed connections, all bots

	mu       sync.Mutex
	bots     map[string]*managedBot // live bots (until their run has ended)
	reserved map[int64]int          // Start calls past the caps, by owner
	libs     map[int]*campaign.Library
	closed   bool
	wg       sync.WaitGroup
	pruneMu  sync.Mutex

	flushStop chan struct{}
	flushDone chan struct{}

	// newRun makes a bot's run (nil: New; tests use stand-ins).
	newRun func(cfg Config) (botRun, error)
	// feedDepth is a decision feed's subscription size.
	feedDepth int
}

var _ api.BotHost = (*Manager)(nil)

// NewManager checks cfg, loads the campaign and the account's daily
// spend, marks the runs a previous process left live as failed and
// applies the retention.
func NewManager(ctx context.Context, cfg ManagerConfig) (*Manager, error) {
	if cfg.Games == nil || cfg.FS == nil {
		return nil, errors.New("runner: manager needs Games and FS")
	}
	if cfg.Dir == "" {
		return nil, errors.New("runner: manager needs a runs directory")
	}
	if cfg.Pakset == "" {
		cfg.Pakset = api.DemoPaksetID
	}
	if cfg.MaxBots <= 0 {
		cfg.MaxBots = DefaultMaxBots
	}
	if cfg.MaxPerUser <= 0 {
		cfg.MaxPerUser = DefaultMaxBotsPerUser
	}
	if cfg.MaxViewers <= 0 {
		cfg.MaxViewers = DefaultMaxViewers
	}
	if cfg.MaxViewersTotal <= 0 {
		cfg.MaxViewersTotal = DefaultMaxViewersTotal
	}
	if cfg.MaxViewersPerIP <= 0 {
		cfg.MaxViewersPerIP = DefaultMaxViewersPerIP
	}
	if cfg.StartLimiter == nil {
		cfg.StartLimiter = auth.NewLimiter(DefaultStartsPerUser, DefaultStartWindow)
	}
	if cfg.MaxRun <= 0 {
		cfg.MaxRun = DefaultMaxRun
	}
	if cfg.StopWait <= 0 {
		cfg.StopWait = DefaultStopWait
	}
	if cfg.Keep < 0 {
		cfg.Keep = 0
	}
	if cfg.KeepPerUser <= 0 {
		cfg.KeepPerUser = DefaultKeepPerUser
	}
	if cfg.Trace == TraceDigest {
		cfg.Trace = TraceFull
	}
	if cfg.Jev.Model == "" {
		cfg.Jev.Model = jev.DefaultModel
	}
	if cfg.Log == nil {
		cfg.Log = slog.Default()
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	camp := cfg.Campaign
	if camp == nil {
		dir := cfg.RoutesDir
		if dir == "" {
			dir = campaign.DefaultRoutesDir()
		}
		c, err := route.Load(dir)
		if err != nil {
			return nil, fmt.Errorf("runner: manager campaign: %w", err)
		}
		camp = c
	}
	if err := os.MkdirAll(cfg.Dir, 0o755); err != nil {
		return nil, fmt.Errorf("runner: runs directory: %w", err)
	}
	acct, err := budget.NewAccount(ctx, budget.AccountConfig{QPS: cfg.JevMaxQPS, DailyUSD: cfg.DailyUSD, Store: cfg.SpendStore, Now: cfg.Now})
	if err != nil {
		return nil, fmt.Errorf("runner: bots' daily spend: %w", err)
	}
	m := &Manager{cfg: cfg, log: cfg.Log, camp: camp, account: acct, viewers: spectate.NewViewerLimit(cfg.MaxViewersTotal),
		bots: map[string]*managedBot{}, reserved: map[int64]int{}, libs: map[int]*campaign.Library{},
		flushStop: make(chan struct{}), flushDone: make(chan struct{}), feedDepth: DefaultFeedDepth}
	m.metrics = newManagerMetrics(func() float64 { return float64(m.liveCount()) }, func() float64 { return float64(m.viewers.InUse()) })
	m.recoverInterrupted()
	m.prune()
	go m.flushLoop()
	return m, nil
}

// RegisterMetrics registers the q2bot_* metrics.
func (m *Manager) RegisterMetrics(reg prometheus.Registerer) error {
	for _, c := range m.metrics.collectors() {
		if err := reg.Register(c); err != nil {
			return err
		}
	}
	return nil
}

// Account is the account every jev bot shares (daily spend, rate limit).
func (m *Manager) Account() *budget.Account { return m.account }

func (m *Manager) flushLoop() {
	defer close(m.flushDone)
	t := time.NewTicker(spendFlush)
	defer t.Stop()
	for {
		select {
		case <-m.flushStop:
			return
		case <-t.C:
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			if err := m.account.Flush(ctx); err != nil {
				m.log.Warn("bots: daily spend not persisted", "err", err)
			}
			cancel()
		}
	}
}

// liveCount is the number of live bots.
func (m *Manager) liveCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.bots)
}

// live returns a live bot.
func (m *Manager) live(id string) *managedBot {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.bots[id]
}

// visible reports whether u may see a bot of owner (public or not).
func visible(owner int64, public bool, u api.BotUser) bool {
	return public || u.Admin || (u.ID != 0 && owner == u.ID)
}

// allowedBackend reports the backends a bot may run with (a replay needs
// a recorded trace and lockstep: not for live bots).
func allowedBackend(name string) bool {
	switch name {
	case BackendScripted, BackendJev, BackendMock, BackendConstant, BackendRandom:
		return true
	}
	return false
}

// Start implements api.BotHost.
func (m *Manager) Start(ctx context.Context, spec api.BotSpec, u api.BotUser) (api.BotInfo, error) {
	spec.Backend = strings.TrimSpace(spec.Backend)
	if spec.Backend == "" {
		spec.Backend = BackendScripted
	}
	if !allowedBackend(spec.Backend) {
		return api.BotInfo{}, fmt.Errorf("%w: backend must be scripted, jev, mock, constant or random", api.ErrBotInvalid)
	}
	switch {
	case u.Admin:
	case u.ID == 0:
		return api.BotInfo{}, fmt.Errorf("%w: log in to start a bot", api.ErrBotForbidden)
	case !m.cfg.AllowUsers:
		return api.BotInfo{}, fmt.Errorf("%w: only administrators may start bots on this server", api.ErrBotForbidden)
	case spec.Backend == BackendJev:
		return api.BotInfo{}, fmt.Errorf("%w: jev bots are for administrators", api.ErrBotForbidden)
	}
	if spec.Backend == BackendJev {
		if !m.cfg.Jev.APIKey.IsSet() {
			return api.BotInfo{}, fmt.Errorf("%w: the jev backend has no API key configured", api.ErrBotsDisabled)
		}
		if spent, limit := m.account.Daily(); limit > 0 && spent >= limit {
			return api.BotInfo{}, fmt.Errorf("%w: today's bot budget ($%.2f) is spent", api.ErrBotLimit, limit)
		}
	}
	name, err := botName(spec.Name, u)
	if err != nil {
		return api.BotInfo{}, err
	}
	skill := DefaultSkill
	if spec.Skill != nil {
		skill = *spec.Skill
	}
	if skill < 0 || skill > 3 {
		return api.BotInfo{}, fmt.Errorf("%w: skill must be 0 to 3", api.ErrBotInvalid)
	}
	visits, _, err := selectVisits(m.camp, spec.Maps)
	if err != nil {
		return api.BotInfo{}, fmt.Errorf("%w: %s", api.ErrBotInvalid, strings.TrimPrefix(err.Error(), ErrConfig.Error()+": "))
	}
	lat, err := ParseLatency(spec.SimLatency)
	if err != nil {
		return api.BotInfo{}, fmt.Errorf("%w: simLatency: want a duration such as 212ms, or a comma separated list", api.ErrBotInvalid)
	}
	for _, d := range append([]time.Duration{lat.Fixed}, lat.Samples...) {
		if d > MaxSimLatency {
			return api.BotInfo{}, fmt.Errorf("%w: simLatency above %s", api.ErrBotInvalid, MaxSimLatency)
		}
	}
	if spec.Backend == BackendMock && !lat.Set {
		// the mock answers about as late as the real API by default
		lat = Latency{Fixed: DefaultModelLatency, Set: true}
	}

	release, err := m.reserve(u)
	if err != nil {
		return api.BotInfo{}, err
	}
	defer release()
	if !u.Admin {
		// each start loads a map into a new game: not in a loop
		if ok, retry := m.cfg.StartLimiter.Allow(strconv.FormatInt(u.ID, 10)); !ok {
			return api.BotInfo{}, fmt.Errorf("%w: you are starting bots too often; try again in %s", api.ErrBotLimit,
				max(retry.Round(time.Second), time.Second))
		}
	}
	fs, err := m.cfg.FS(ctx)
	if err != nil {
		m.log.Error("bots: game data unavailable", "err", err)
		return api.BotInfo{}, fmt.Errorf("%w: the game data is unavailable", api.ErrBotsDisabled)
	}
	b, err := m.newBot(fs, spec, u, name, visits, skill, lat)
	if err != nil {
		return api.BotInfo{}, err
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		b.discard()
		return api.BotInfo{}, fmt.Errorf("%w: the server is shutting down", api.ErrBotsDisabled)
	}
	m.bots[b.id] = b
	m.wg.Add(1)
	m.mu.Unlock()
	m.log.Info("bot started", "bot", b.id, "backend", b.meta.Backend, "maps", strings.Join(visits, ","), "skill", skill,
		"owner", u.ID, "public", spec.Public)
	info := m.liveInfo(b, false) // as created: starting
	go m.runBot(b)
	return info, nil
}

// botName returns the bot's display name: the spec's, trimmed, or a
// default from the owner's name.
func botName(n string, u api.BotUser) (string, error) {
	n = strings.TrimSpace(n)
	if utf8.RuneCountInString(n) > maxBotName {
		return "", fmt.Errorf("%w: name must be at most %d characters", api.ErrBotInvalid, maxBotName)
	}
	for _, r := range n {
		if unicode.IsControl(r) {
			return "", fmt.Errorf("%w: name must not contain control characters", api.ErrBotInvalid)
		}
	}
	if n == "" {
		if u.Name != "" {
			return u.Name + "'s bot", nil
		}
		return "bot", nil
	}
	return n, nil
}

// reserve takes a live-bot slot of u under the caps; the returned func
// gives it back (the registered bot then counts itself).
func (m *Manager) reserve(u api.BotUser) (func(), error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil, fmt.Errorf("%w: the server is shutting down", api.ErrBotsDisabled)
	}
	total, mine := 0, m.reserved[u.ID]
	for _, n := range m.reserved {
		total += n
	}
	for _, b := range m.bots {
		total++
		if b.meta.OwnerID == u.ID {
			mine++
		}
	}
	if total >= m.cfg.MaxBots {
		return nil, fmt.Errorf("%w: the server runs its maximum of %d bots", api.ErrBotLimit, m.cfg.MaxBots)
	}
	if !u.Admin && u.ID != 0 && mine >= m.cfg.MaxPerUser {
		return nil, fmt.Errorf("%w: you already run %d bots; stop one first", api.ErrBotLimit, mine)
	}
	m.reserved[u.ID]++
	return func() {
		m.mu.Lock()
		if m.reserved[u.ID]--; m.reserved[u.ID] <= 0 {
			delete(m.reserved, u.ID)
		}
		m.mu.Unlock()
	}, nil
}

// library returns the shared map data and nav graphs at skill.
func (m *Manager) library(fs FileSystem, skill int) *campaign.Library {
	m.mu.Lock()
	defer m.mu.Unlock()
	lib := m.libs[skill]
	if lib == nil {
		lib = campaign.NewLibrary(campaign.LibraryConfig{ReadFile: fs.ReadFile, Skill: skill, NavDir: m.cfg.NavDir,
			Logf: func(format string, args ...any) { m.log.Debug(strings.TrimSpace(fmt.Sprintf(format, args...))) }})
		m.libs[skill] = lib
	}
	return lib
}

// randomHex returns n random bytes in hex.
func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// randomSeed returns a random run seed.
func randomSeed() uint64 {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return binary.LittleEndian.Uint64(b[:]) >> 1
}

// newID returns a fresh run id whose directory does not exist.
func (m *Manager) newID() (string, error) {
	for i := 0; i < 8; i++ {
		id, err := NewRunID(m.cfg.Now())
		if err != nil {
			return "", err
		}
		if _, err := os.Lstat(m.runDir(id)); errors.Is(err, os.ErrNotExist) && m.live(id) == nil {
			return id, nil
		}
	}
	return "", errors.New("runner: no free run id")
}

// botUserinfo is the bot client's userinfo: fakeclient's defaults with
// the bot's name and the game's password.
func botUserinfo(name, password string) string {
	ui := `\name\fakeclient\skin\male/grunt\rate\25000\msg\1\hand\0\fov\90`
	if nu, warn := shared.Info_SetValueForKey(ui, "name", host.SanitizeName(name)); warn == "" {
		ui = nu
	}
	if nu, warn := shared.Info_SetValueForKey(ui, "password", password); warn == "" {
		ui = nu
	}
	return ui
}

// newGame returns the runner's NewInstance for b: a realtime
// single-player game of the server's host, without an owner, with a
// random password only the bot knows.
func (m *Manager) newGame(b *managedBot) func(ep int, spec session.Spec, seed uint32) (*host.Instance, func(), error) {
	return func(ep int, spec session.Spec, _ uint32) (*host.Instance, func(), error) {
		gs, err := spec.GameSpec()
		if err != nil {
			return nil, nil, err
		}
		gs.Name, gs.Pakset, gs.Public = "bot "+b.meta.Name, m.cfg.Pakset, false
		gs.Cvars["password"] = b.password
		id := gameID(b.id, ep)
		ctx, cancel := context.WithTimeout(b.ctx, 30*time.Second)
		defer cancel()
		if _, err := m.cfg.Games.CreateWithID(ctx, id, gs); err != nil {
			return nil, nil, fmt.Errorf("runner: bot game: %w", err)
		}
		inst, err := m.cfg.Games.Host().Get(id)
		if err != nil {
			_ = m.cfg.Games.Stop(context.Background(), id)
			return nil, nil, fmt.Errorf("runner: bot game: %w", err)
		}
		return inst, func() { _ = m.cfg.Games.Stop(context.Background(), id) }, nil
	}
}

// List implements api.BotHost.
func (m *Manager) List(_ context.Context, u api.BotUser) ([]api.BotInfo, error) {
	m.mu.Lock()
	live := make([]*managedBot, 0, len(m.bots))
	for _, b := range m.bots {
		live = append(live, b)
	}
	m.mu.Unlock()
	out := []api.BotInfo{}
	seen := map[string]bool{}
	for _, b := range live {
		seen[b.id] = true
		if visible(b.meta.OwnerID, b.meta.Public, u) {
			out = append(out, m.liveInfo(b, false))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt.After(out[j].StartedAt) })
	ended, err := m.scanRuns()
	if err != nil {
		return nil, err
	}
	for _, r := range ended {
		if seen[r.id] || !visible(r.meta.OwnerID, r.meta.Public, u) {
			continue
		}
		out = append(out, m.endedInfo(r, false))
	}
	return out, nil
}

// Get implements api.BotHost.
func (m *Manager) Get(_ context.Context, id string, u api.BotUser) (api.BotInfo, error) {
	if b := m.live(id); b != nil {
		if !visible(b.meta.OwnerID, b.meta.Public, u) {
			return api.BotInfo{}, api.ErrBotNotFound
		}
		return m.liveInfo(b, true), nil
	}
	r, ok := m.readRun(id)
	if !ok || !visible(r.meta.OwnerID, r.meta.Public, u) {
		return api.BotInfo{}, api.ErrBotNotFound
	}
	return m.endedInfo(r, true), nil
}

// Stop implements api.BotHost.
func (m *Manager) Stop(ctx context.Context, id string, u api.BotUser) error {
	b := m.live(id)
	if b == nil {
		r, ok := m.readRun(id)
		switch {
		case !ok || !visible(r.meta.OwnerID, r.meta.Public, u):
			return api.ErrBotNotFound
		case !u.Admin && (u.ID == 0 || r.meta.OwnerID != u.ID):
			return fmt.Errorf("%w: not your bot", api.ErrBotForbidden)
		}
		return nil // it has ended already
	}
	if !visible(b.meta.OwnerID, b.meta.Public, u) {
		return api.ErrBotNotFound
	}
	if !u.Admin && (u.ID == 0 || b.meta.OwnerID != u.ID) {
		return fmt.Errorf("%w: not your bot", api.ErrBotForbidden)
	}
	why := "stopped by its owner"
	if u.ID != b.meta.OwnerID {
		why = "stopped by an administrator"
	}
	b.stop(why)
	return m.wait(ctx, b)
}

// wait waits for b's run to end, at most StopWait: a bot told to stop
// ends on its own, so a slow end is no error (the bot still reads as
// live until then). Only ctx ending first is.
func (m *Manager) wait(ctx context.Context, b *managedBot) error {
	t := time.NewTimer(m.cfg.StopWait)
	defer t.Stop()
	select {
	case <-b.done:
	case <-t.C:
		m.log.Warn("bots: bot still ending after its stop", "bot", b.id, "waited", m.cfg.StopWait)
	case <-ctx.Done():
		return ctx.Err()
	}
	return nil
}

// Watch implements api.BotHost. A bot's outstanding tickets are bounded:
// asking for more revokes the oldest (see issueTickets).
func (m *Manager) Watch(_ context.Context, id string, u api.BotUser) (api.BotWatch, error) {
	b := m.live(id)
	if b == nil {
		if r, ok := m.readRun(id); ok && visible(r.meta.OwnerID, r.meta.Public, u) {
			return api.BotWatch{}, api.ErrBotNotLive
		}
		return api.BotWatch{}, api.ErrBotNotFound
	}
	if !visible(b.meta.OwnerID, b.meta.Public, u) {
		return api.BotWatch{}, api.ErrBotNotFound
	}
	if !api.BotLive(b.status()) {
		return api.BotWatch{}, api.ErrBotNotLive
	}
	tks, err := m.issueTickets(b, u, watchScope(id), decisionsScope(id))
	if err != nil {
		return api.BotWatch{}, err
	}
	wt, dt := tks[0], tks[1]
	base := strings.TrimRight(m.cfg.PublicWSURL, "/") + "/ws/v1/bots/" + id
	return api.BotWatch{Ticket: wt.Token, WSURL: base + "/watch?ticket=" + wt.Token, DecisionsTicket: dt.Token,
		DecisionsURL: base + "/decisions?ticket=" + dt.Token, Pakset: m.cfg.Pakset}, nil
}

// Close stops every bot (they end as stopped and write their run
// directories) and waits for them, then persists the daily spend. Call it
// before closing the games the bots play in. It is idempotent.
func (m *Manager) Close() {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	m.closed = true
	live := make([]*managedBot, 0, len(m.bots))
	for _, b := range m.bots {
		live = append(live, b)
	}
	m.mu.Unlock()
	for _, b := range live {
		b.stop("the server shut down")
	}
	done := make(chan struct{})
	go func() { m.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(m.cfg.StopWait):
		m.log.Warn("bots: runs still ending at shutdown", "bots", len(live))
	}
	close(m.flushStop)
	<-m.flushDone
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := m.account.Flush(ctx); err != nil {
		m.log.Warn("bots: daily spend not persisted", "err", err)
	}
}

var _ trace.Sink = (*liveStats)(nil)

// slogLevel is the log level of a bot's end.
func slogLevel(status string) slog.Level {
	if status == api.BotFailed {
		return slog.LevelWarn
	}
	return slog.LevelInfo
}

// Dir is the runs directory.
func (m *Manager) Dir() string { return m.cfg.Dir }
