package host

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"quake2web/server/internal/api"
	"quake2web/server/internal/assets/blob"
	"quake2web/server/internal/assets/manifest"
	"quake2web/server/internal/auth"
	"quake2web/server/internal/db"
	"quake2web/server/internal/game"
	"quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/crand"
	"quake2web/server/internal/sv"
)

// GameModule creates the game module factory of a new instance; rng is the
// instance's random generator (shared by engine and game).
type GameModule func(rng *crand.Rand) sv.GameFactory

// RealGame is the default GameModule: the internal/game port.
func RealGame(rng *crand.Rand) sv.GameFactory {
	return func(gi game.Import) game.Export { return game.New(gi, rng) }
}

// Default instance limits (GamesConfig.MaxGamesPerOwner / MaxGames).
const (
	DefaultMaxGamesPerOwner = 3
	DefaultMaxGames         = 64
)

// GamesConfig configures Games.
type GamesConfig struct {
	// Host runs the instances (default New()).
	Host *Host
	// Indexes resolves a pakset id to its merged asset index
	// (api.Catalog.PaksetIndex).
	Indexes func(ctx context.Context, paksetID string) (*manifest.Index, error)
	// Blobs holds the pakset files.
	Blobs blob.Store
	// Saves, when set, stores the saves of single player and coop instances
	// in their owner's account; nil keeps them in memory per instance.
	Saves db.SaveStore
	// Tickets issues the join tickets redeemed by the WebSocket endpoint.
	Tickets auth.TicketService
	// PublicWSURL is the base of the returned WebSocket URLs
	// ("wss://q2.example.com"); empty returns a relative URL.
	PublicWSURL string
	// Game is the game module (default RealGame).
	Game GameModule
	// Maps is the shared map cache (default a new one).
	Maps *sv.MapCache
	// IdleTimeout stops account-owned instances without players for that
	// long (default 5 minutes); games created without an owner (the server's
	// own, e.g. q2server -map) are never reaped. Single player instances
	// stop as soon as their player leaves (after an autosave).
	IdleTimeout time.Duration
	// ReapInterval is how often idle instances are looked for (default 15 s).
	ReapInterval time.Duration
	// Seed returns the random seed of a new instance (default: time based).
	Seed func() uint32
	// OnEnd, when set, is called after an instance has ended for any reason
	// (e.g. to close its games registry row).
	OnEnd func(id string)
	// Log receives diagnostics; the servers' console output is logged at
	// debug level.
	Log *slog.Logger
	// MaxGamesPerOwner bounds the running games of one account (default
	// DefaultMaxGamesPerOwner; negative: unlimited). MaxGames bounds all
	// account-owned games together (default DefaultMaxGames; negative:
	// unlimited). Games without an owner (the server's own) are exempt.
	MaxGamesPerOwner int
	MaxGames         int
}

// Games is the api.GameHost of the game server: it turns GameSpecs into
// host instances loading their data from ingested paksets, issues join
// tickets, maps save/load client commands to the owner's account saves and
// reaps idle instances.
type Games struct {
	cfg  GamesConfig
	host *Host

	mu    sync.Mutex
	games map[string]*gameMeta
	// starting counts the games being created per owner (reserved slots).
	starting map[int64]int

	stopReap chan struct{}
	reapDone chan struct{}
	closeMu  sync.Once
}

type gameMeta struct {
	id        string
	spec      api.GameSpec
	startedAt time.Time
	inst      *Instance
	saves     bool // account saves enabled (sp/coop with an owner)
	hadPlayer bool

	// capture collects console output while a client-issued command runs
	// (instance goroutine only).
	capture *strings.Builder
}

// NewGames starts the instance manager.
func NewGames(cfg GamesConfig) *Games {
	if cfg.Host == nil {
		cfg.Host = New()
	}
	if cfg.Game == nil {
		cfg.Game = RealGame
	}
	if cfg.Maps == nil {
		cfg.Maps = sv.NewMapCache()
	}
	if cfg.IdleTimeout <= 0 {
		cfg.IdleTimeout = 5 * time.Minute
	}
	if cfg.ReapInterval <= 0 {
		cfg.ReapInterval = 15 * time.Second
	}
	if cfg.Seed == nil {
		cfg.Seed = func() uint32 { return uint32(time.Now().UnixNano()) }
	}
	if cfg.Log == nil {
		cfg.Log = slog.Default()
	}
	if cfg.MaxGamesPerOwner == 0 {
		cfg.MaxGamesPerOwner = DefaultMaxGamesPerOwner
	}
	if cfg.MaxGames == 0 {
		cfg.MaxGames = DefaultMaxGames
	}
	if cfg.Tickets != nil && cfg.Host.Tickets == nil {
		cfg.Host.Tickets = cfg.Tickets
	}
	g := &Games{cfg: cfg, host: cfg.Host, games: map[string]*gameMeta{}, starting: map[int64]int{},
		stopReap: make(chan struct{}), reapDone: make(chan struct{})}
	go g.reaper()
	return g
}

// Host returns the underlying instance host (for its WebSocket handler and
// UDP).
func (g *Games) Host() *Host { return g.host }

// Handler returns the WebSocket handler (GET /ws/v1/games/{id}).
func (g *Games) Handler() http.Handler { return g.host.Handler() }

var _ api.GameHost = (*Games)(nil)

// ErrBadSpec wraps invalid GameSpec errors (the API answers 400).
var ErrBadSpec = api.ErrGameInvalid

// allowedCvars are the GameSpec cvars an owner may set; the mode decides
// deathmatch/coop/ctf/maxclients.
var allowedCvars = map[string]bool{
	"skill": true, "dmflags": true, "fraglimit": true, "timelimit": true, "capturelimit": true,
	"password": true, "spectator_password": true, "hostname": true, "maxspectators": true,
	"sv_maplist": true, "g_select_empty": true, "sv_gravity": true, "sv_airaccelerate": true,
	"filterban": true, "sv_maxvelocity": true, "instantweap": true, "coop_respawn": true,
}

// ModeSettings returns the dedicated flag and the cvars of a game mode
// (C: the menu's StartServer / "+set deathmatch 1" conventions).
func ModeSettings(spec api.GameSpec) (dedicated bool, cvars [][2]string, err error) {
	maxp := spec.MaxPlayers
	set := func(k, v string) { cvars = append(cvars, [2]string{k, v}) }
	skill := "1"
	for k, v := range spec.Cvars {
		if k == "skill" {
			skill = v
		}
	}
	switch spec.Mode {
	case "", "sp":
		set("deathmatch", "0")
		set("coop", "0")
		set("maxclients", "1")
	case "coop":
		if maxp <= 1 {
			maxp = 4
		}
		set("deathmatch", "0")
		set("coop", "1")
		set("maxclients", strconv.Itoa(maxp))
	case "dm", "ctf":
		if maxp <= 1 {
			maxp = 8
			if spec.Mode == "ctf" {
				maxp = 16
			}
		}
		if maxp > q2const.MAX_CLIENTS {
			maxp = q2const.MAX_CLIENTS
		}
		dedicated = true
		set("deathmatch", "1")
		set("coop", "0")
		if spec.Mode == "ctf" {
			// selects the ctf module, like loading ctf/game.so (game/g_main Init reads "game")
			set("game", "ctf")
			set("ctf", "1")
		} else {
			set("ctf", "0")
		}
		set("maxclients", strconv.Itoa(maxp))
	default:
		return false, nil, fmt.Errorf("%w: unknown mode %q", ErrBadSpec, spec.Mode)
	}
	set("skill", skill)
	keys := make([]string, 0, len(spec.Cvars))
	for k := range spec.Cvars {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if k == "skill" {
			continue
		}
		if !allowedCvars[k] {
			return false, nil, fmt.Errorf("%w: cvar %q may not be set", ErrBadSpec, k)
		}
		set(k, spec.Cvars[k])
	}
	return dedicated, cvars, nil
}

// Create implements api.GameHost.
func (g *Games) Create(ctx context.Context, spec api.GameSpec) (string, error) {
	return g.CreateWithID(ctx, "", spec)
}

// CreateWithID is Create with a chosen instance id ("" picks a random one).
func (g *Games) CreateWithID(ctx context.Context, id string, spec api.GameSpec) (string, error) {
	if spec.Pakset == "" {
		spec.Pakset = api.DemoPaksetID
	}
	if spec.Mode == "" {
		spec.Mode = "sp"
	}
	dedicated, cvars, err := ModeSettings(spec)
	if err != nil {
		return "", err
	}
	if g.cfg.Indexes == nil || g.cfg.Blobs == nil {
		return "", errors.New("host: no pakset resolver configured")
	}
	if spec.OwnerID != 0 {
		release, err := g.reserve(spec.OwnerID)
		if err != nil {
			return "", err
		}
		defer release()
	}
	idx, err := g.cfg.Indexes(ctx, spec.Pakset)
	if err != nil {
		return "", fmt.Errorf("pakset %s: %w", spec.Pakset, err)
	}
	if id == "" {
		id = randomID()
	}
	m := &gameMeta{id: id, spec: spec, startedAt: time.Now()}
	var saves sv.SaveStore
	if (spec.Mode == "sp" || spec.Mode == "coop") && spec.OwnerID != 0 && g.cfg.Saves != nil {
		saves = newAccountSaves(g.cfg.Saves, spec.OwnerID, SaveOrigin{Pakset: spec.Pakset, Mode: spec.Mode})
		m.saves = true
	} else {
		saves = sv.NewMemSaveStore()
	}
	start := "map " + spec.Map
	if spec.LoadSlot != "" {
		if !m.saves {
			return "", fmt.Errorf("%w: saves are not available for this game", ErrBadSpec)
		}
		start = "load " + ServerSlot(spec.LoadSlot)
	}
	log := g.cfg.Log.With("game", id)
	rng := crand.New(g.cfg.Seed())
	inst, err := g.host.Create(InstanceConfig{
		ID: id,
		Server: sv.Config{
			FS:        NewIndexFS(idx, g.cfg.Blobs),
			Game:      g.cfg.Game(rng),
			Maps:      g.mapCacheFor(spec),
			Saves:     saves,
			Rand:      rng,
			Dedicated: dedicated,
			Cvars:     cvars,
			Printf: func(format string, args ...any) {
				text := fmt.Sprintf(format, args...)
				if m.capture != nil {
					m.capture.WriteString(text)
				}
				if t := strings.TrimRight(text, "\n"); t != "" {
					log.Debug(t)
				}
			},
		},
		Commands: []string{start},
		ClientCommand: func(inst *Instance, s *sv.Server, cl *sv.Client, p *Player) bool {
			return g.clientCommand(m, inst, s, cl, p)
		},
		BeforeDrop: func(inst *Instance, s *sv.Server, cl *sv.Client, p *Player) {
			if m.spec.Mode == "sp" && m.saves && cl.Spawned() {
				g.autosave(m, s)
			}
		},
		ConnClosed: func(inst *Instance, p *Player) {
			if m.spec.Mode == "sp" && inst.Stats().Players == 0 {
				go g.end(id, "player left")
			}
		},
	})
	if err != nil {
		return "", err
	}
	m.inst = inst
	g.mu.Lock()
	g.games[id] = m
	g.mu.Unlock()
	go func() {
		<-inst.Done()
		g.mu.Lock()
		cur := g.games[id]
		if cur == m {
			delete(g.games, id)
		}
		g.mu.Unlock()
		if cur == m {
			log.Info("game ended", "err", inst.Err())
			if g.cfg.OnEnd != nil {
				g.cfg.OnEnd(id)
			}
		}
	}()
	log.Info("game started", "mode", spec.Mode, "map", spec.Map, "pakset", spec.Pakset, "owner", spec.OwnerID, "loadSlot", spec.LoadSlot)
	return id, nil
}

// mapCacheFor returns the map cache an instance of spec may use: the
// shared cache only for the system demo pakset. sv.MapCache keys by name,
// size and a non-cryptographic FNV-1a hash, so a crafted map in a user
// pakset could otherwise collide with, and replace, a public map for every
// other game (and each distinct map would stay cached forever).
func (g *Games) mapCacheFor(spec api.GameSpec) *sv.MapCache {
	if spec.Pakset == api.DemoPaksetID {
		return g.cfg.Maps
	}
	return nil
}

// autosave writes the "autosave" slot (instance goroutine).
func (g *Games) autosave(m *gameMeta, s *sv.Server) {
	if !s.InGame() {
		return
	}
	if err := s.ExecuteText("save save0\n"); err != nil {
		g.cfg.Log.Warn("autosave failed", "game", m.id, "err", err)
	}
}

// clientCommand handles the client console commands the host adds:
// "save <slot>" and "load <slot>" for the owner of a single player or coop
// game (C: the client's save/load commands issuing savegame/loadgame on
// its local server), and the single player autosave on "disconnect".
func (g *Games) clientCommand(m *gameMeta, inst *Instance, s *sv.Server, cl *sv.Client, p *Player) bool {
	switch cmd := s.Cmd.Argv(0); cmd {
	case "save", "load":
		if !m.saves {
			s.ClientPrintf(cl, q2const.PRINT_HIGH, "Saving is not available in this game.\n")
			return true
		}
		if p == nil || p.UserID != m.spec.OwnerID {
			s.ClientPrintf(cl, q2const.PRINT_HIGH, "Only the owner of this game can %s.\n", cmd)
			return true
		}
		slot := s.Cmd.Argv(1)
		if s.Cmd.Argc() != 2 || !api.ValidSlot(slot) || slot == "current" {
			s.ClientPrintf(cl, q2const.PRINT_HIGH, "usage: %s <slot> (letters, digits, - and _)\n", cmd)
			return true
		}
		if cmd == "save" && !g.saveSlotAvailable(m, slot) {
			s.ClientPrintf(cl, q2const.PRINT_HIGH, "Too many save slots (%d); overwrite or delete one first.\n", MaxSaveSlots)
			return true
		}
		// the server's own console commands have the same names
		// (C: SV_InitOperatorCommands "save"/"load")
		line := cmd + " " + ServerSlot(slot) + "\n"
		inst.Defer(func(s *sv.Server) {
			var out strings.Builder
			m.capture = &out
			err := s.ExecuteText(line)
			m.capture = nil
			if err != nil {
				g.cfg.Log.Warn("client "+cmd+" failed", "game", m.id, "slot", slot, "err", err)
			}
			if cmd == "save" && cl.InUse() {
				text := strings.TrimSpace(out.String())
				if text == "" {
					text = "Done."
				}
				s.ClientPrintf(cl, q2const.PRINT_HIGH, "%s\n", text)
			}
		})
		return true
	case "disconnect":
		if m.spec.Mode == "sp" {
			if m.saves && cl.Spawned() {
				args := s.Cmd.Args()
				g.autosave(m, s)
				// "save" tokenized its own command line; restore ours
				s.Cmd.TokenizeString(strings.TrimSpace(cmd+" "+args), false)
			}
			// the single player has left: end the game once the engine
			// has dropped the client
			inst.Defer(func(*sv.Server) { go g.end(m.id, "player left") })
		}
	}
	return false
}

// reserve takes a game slot of owner for the duration of a Create
// (MaxGamesPerOwner / MaxGames); the returned func releases the
// reservation (the running game then counts itself).
func (g *Games) reserve(owner int64) (func(), error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	mine, total := g.starting[owner], 0
	for _, n := range g.starting {
		total += n
	}
	for _, m := range g.games {
		if m.spec.OwnerID == 0 || m.ended() {
			continue
		}
		total++
		if m.spec.OwnerID == owner {
			mine++
		}
	}
	if g.cfg.MaxGamesPerOwner > 0 && mine >= g.cfg.MaxGamesPerOwner {
		return nil, fmt.Errorf("%w: you already run %d games; stop one first", api.ErrGameLimit, mine)
	}
	if g.cfg.MaxGames > 0 && total >= g.cfg.MaxGames {
		return nil, fmt.Errorf("%w: the server runs its maximum of %d games", api.ErrGameLimit, total)
	}
	g.starting[owner]++
	return func() {
		g.mu.Lock()
		if g.starting[owner]--; g.starting[owner] <= 0 {
			delete(g.starting, owner)
		}
		g.mu.Unlock()
	}, nil
}

// ended reports whether the instance goroutine has finished (the games map
// entry is removed shortly after, by a background goroutine).
func (m *gameMeta) ended() bool {
	select {
	case <-m.inst.Done():
		return true
	default:
		return false
	}
}

// MaxSaveSlots bounds the named save slots of one account (the autosave
// slot does not count): each is a database row of up to a few MiB.
const MaxSaveSlots = 32

// saveSlotAvailable reports whether the owner of m may write slot: it
// exists already, or the account has fewer than MaxSaveSlots slots.
func (g *Games) saveSlotAvailable(m *gameMeta, slot string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	list, err := g.cfg.Saves.List(ctx, m.spec.OwnerID)
	if err != nil {
		return true // the save itself will report the database error
	}
	n := 0
	for _, si := range list {
		if si.Slot == slot {
			return true
		}
		if si.Slot != AutosaveSlot {
			n++
		}
	}
	return n < MaxSaveSlots
}

// meta returns a running game (nil for unknown or ended ones).
func (g *Games) meta(id string) *gameMeta {
	g.mu.Lock()
	defer g.mu.Unlock()
	if m := g.games[id]; m != nil && !m.ended() {
		return m
	}
	return nil
}

func (g *Games) info(m *gameMeta) api.GameInfo {
	st := m.inst.Stats()
	mp := st.Map
	if mp == "" {
		mp = m.spec.Map
	}
	maxp := st.MaxClients
	if maxp == 0 {
		maxp = m.spec.MaxPlayers
	}
	return api.GameInfo{ID: m.id, Name: m.spec.Name, OwnerID: m.spec.OwnerID, Mode: m.spec.Mode, Map: mp,
		Pakset: m.spec.Pakset, Public: m.spec.Public, Players: st.Players, MaxPlayers: maxp, StartedAt: m.startedAt}
}

// List implements api.GameHost.
func (g *Games) List(context.Context) ([]api.GameInfo, error) {
	g.mu.Lock()
	all := make([]*gameMeta, 0, len(g.games))
	for _, m := range g.games {
		if !m.ended() {
			all = append(all, m)
		}
	}
	g.mu.Unlock()
	out := make([]api.GameInfo, 0, len(all))
	for _, m := range all {
		out = append(out, g.info(m))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt.Before(out[j].StartedAt) })
	return out, nil
}

// Get implements api.GameHost.
func (g *Games) Get(_ context.Context, id string) (api.GameInfo, error) {
	m := g.meta(id)
	if m == nil {
		return api.GameInfo{}, api.ErrGameNotFound
	}
	return g.info(m), nil
}

// Join implements api.GameHost.
func (g *Games) Join(_ context.Context, id string, p api.Player) (string, string, error) {
	m := g.meta(id)
	if m == nil {
		return "", "", api.ErrGameNotFound
	}
	if g.cfg.Tickets == nil {
		return "", "", errors.New("host: no ticket service")
	}
	if st := m.inst.Stats(); st.MaxClients > 0 && st.Players >= st.MaxClients {
		return "", "", api.ErrGameFull
	}
	if m.spec.Mode == "sp" && m.spec.OwnerID != 0 && p.UserID != m.spec.OwnerID {
		return "", "", api.ErrGameFull // the single player slot is the owner's
	}
	tk, err := g.cfg.Tickets.Issue(p.UserID, p.DisplayName, id)
	if err != nil {
		return "", "", err
	}
	return tk.Token, strings.TrimRight(g.cfg.PublicWSURL, "/") + "/ws/v1/games/" + id + "?ticket=" + tk.Token, nil
}

// Stop implements api.GameHost.
func (g *Games) Stop(_ context.Context, id string) error {
	m := g.meta(id)
	if m == nil {
		return api.ErrGameNotFound
	}
	m.inst.Stop()
	return nil
}

func (g *Games) end(id, why string) {
	if m := g.meta(id); m != nil {
		g.cfg.Log.Info("stopping game", "game", id, "reason", why)
		m.inst.Stop()
	}
}

func (g *Games) reaper() {
	defer close(g.reapDone)
	t := time.NewTicker(g.cfg.ReapInterval)
	defer t.Stop()
	for {
		select {
		case <-g.stopReap:
			return
		case now := <-t.C:
			g.reap(now)
		}
	}
}

func (g *Games) reap(now time.Time) {
	g.mu.Lock()
	all := make([]*gameMeta, 0, len(g.games))
	for _, m := range g.games {
		all = append(all, m)
	}
	g.mu.Unlock()
	for _, m := range all {
		st := m.inst.Stats()
		if st.Players > 0 || m.spec.OwnerID == 0 { // server-created games stay up
			continue
		}
		since := m.startedAt
		if st.LastActive.After(since) {
			since = st.LastActive
		}
		if now.Sub(since) >= g.cfg.IdleTimeout {
			g.end(m.id, "idle")
		}
	}
}

// Close autosaves single player games, stops every instance and the reaper.
func (g *Games) Close() {
	g.closeMu.Do(func() {
		close(g.stopReap)
		<-g.reapDone
		g.mu.Lock()
		all := make([]*gameMeta, 0, len(g.games))
		for _, m := range g.games {
			all = append(all, m)
		}
		g.mu.Unlock()
		for _, m := range all {
			if m.spec.Mode == "sp" && m.saves {
				_ = m.inst.Do(func(s *sv.Server) {
					for k := range s.SVS.Clients {
						if s.SVS.Clients[k].Spawned() {
							g.autosave(m, s)
							break
						}
					}
				})
			}
		}
		g.host.Shutdown()
	})
}

// RegisterMetrics registers q2_instances, q2_players,
// q2_tick_duration_seconds and q2_tick_overruns_total. Call it before the
// first instance is created.
func (g *Games) RegisterMetrics(reg prometheus.Registerer) error {
	count := func(players bool) float64 {
		g.mu.Lock()
		defer g.mu.Unlock()
		if !players {
			return float64(len(g.games))
		}
		n := 0
		for _, m := range g.games {
			n += m.inst.Stats().Players
		}
		return float64(n)
	}
	tick := prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "q2_tick_duration_seconds",
		Help:    "Duration of one server frame (SV_Frame) of a game instance.",
		Buckets: []float64{0.0005, 0.001, 0.002, 0.005, 0.01, 0.02, 0.05, 0.1, 0.25},
	})
	overruns := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "q2_tick_overruns_total",
		Help: "Server frames that took longer than the 100 ms frame time.",
	})
	for _, c := range []prometheus.Collector{
		prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: "q2_instances", Help: "Running game instances."},
			func() float64 { return count(false) }),
		prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: "q2_players", Help: "Connected players over all instances."},
			func() float64 { return count(true) }),
		tick, overruns,
	} {
		if err := reg.Register(c); err != nil {
			return err
		}
	}
	prev := g.host.TickObserver
	g.host.TickObserver = func(d time.Duration) {
		tick.Observe(d.Seconds())
		if d > 100*time.Millisecond {
			overruns.Inc()
		}
		if prev != nil {
			prev(d)
		}
	}
	return nil
}
