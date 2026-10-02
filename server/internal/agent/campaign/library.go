package campaign

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"quake2web/server/internal/agent/mapdata"
	"quake2web/server/internal/agent/nav"
	"quake2web/server/internal/agent/nav/navbuild"
)

// Library loads and keeps the static knowledge of the campaign's maps: the
// map data (mapdata, for the campaign's skill) and the nav graph resolved
// for that skill (from the nav cache, built when missing). Both are
// immutable, so one Library can serve many episodes and bots; it is safe
// for concurrent use.
type Library struct {
	read  func(name string) ([]byte, error)
	skill int
	store *nav.Store
	par   nav.Params

	mu     sync.Mutex
	levels map[string]*libEntry
}

type libEntry struct {
	mu sync.Mutex
	md *mapdata.Map
	g  *nav.Graph
}

// LibraryConfig configures a Library.
type LibraryConfig struct {
	// ReadFile reads game data ("maps/<name>.bsp"), e.g. the session's
	// ReadFile (required).
	ReadFile func(name string) ([]byte, error)
	// Skill is the skill the map data and graphs are resolved for.
	Skill int
	// NavDir is the nav cache directory ("": nav.DefaultDir()).
	NavDir string
	// Params are the nav build parameters (zero: nav.DefaultParams()).
	Params nav.Params
	// Workers bounds the builder's workers when a graph is missing (0:
	// GOMAXPROCS).
	Workers int
	// Logf, when set, receives cache misses and build stages.
	Logf func(format string, args ...any)
}

// NewLibrary returns an empty library.
func NewLibrary(cfg LibraryConfig) *Library {
	if cfg.NavDir == "" {
		cfg.NavDir = nav.DefaultDir()
	}
	if cfg.Params == (nav.Params{}) {
		cfg.Params = nav.DefaultParams()
	}
	st := nav.NewStore(cfg.NavDir, navbuild.StoreBuilder(cfg.ReadFile, navbuild.Config{Params: cfg.Params, Workers: cfg.Workers, Logf: cfg.Logf}))
	st.Logf = cfg.Logf
	return &Library{read: cfg.ReadFile, skill: cfg.Skill, store: st, par: cfg.Params, levels: map[string]*libEntry{}}
}

// Skill returns the skill the library resolves for.
func (l *Library) Skill() int { return l.skill }

// Map returns the map data of map name (loaded once; a failed load is
// tried again by the next call).
func (l *Library) Map(name string) (*mapdata.Map, error) {
	e := l.entry(name)
	e.mu.Lock()
	defer e.mu.Unlock()
	return l.mapLocked(e, name)
}

// Level returns the map data and the skill-resolved nav graph of map name.
// The first successful call for a map loads them (building the graph when
// the cache has none); later calls share the result. A failed load (a
// cancelled build, say) is tried again by the next call.
func (l *Library) Level(ctx context.Context, name string) (*mapdata.Map, *nav.Graph, error) {
	e := l.entry(name)
	e.mu.Lock()
	defer e.mu.Unlock()
	md, err := l.mapLocked(e, name)
	if err != nil {
		return nil, nil, err
	}
	if e.g == nil {
		g, err := l.store.Load(ctx, md, l.par)
		if err != nil {
			return nil, nil, fmt.Errorf("campaign: %s nav graph: %w", name, err)
		}
		e.g = g
	}
	return md, e.g, nil
}

func (l *Library) entry(name string) *libEntry {
	l.mu.Lock()
	defer l.mu.Unlock()
	e := l.levels[name]
	if e == nil {
		e = &libEntry{}
		l.levels[name] = e
	}
	return e
}

func (l *Library) mapLocked(e *libEntry, name string) (*mapdata.Map, error) {
	if e.md != nil {
		return e.md, nil
	}
	if l.read == nil {
		return nil, fmt.Errorf("campaign: library has no ReadFile")
	}
	raw, err := l.read("maps/" + name + ".bsp")
	if err != nil {
		return nil, fmt.Errorf("campaign: %s: %w", name, err)
	}
	md, err := mapdata.Load(name, raw, mapdata.Options{Skill: l.skill})
	if err != nil {
		return nil, fmt.Errorf("campaign: %s: %w", name, err)
	}
	e.md = md
	return md, nil
}

// DefaultRoutesDir returns the route tables' directory: $Q2_ROUTES_DIR,
// else fixtures/agent/routes of the repository the working directory is
// in (found by its server/go.mod), else that path relative to the working
// directory.
func DefaultRoutesDir() string {
	if d := os.Getenv("Q2_ROUTES_DIR"); d != "" {
		return d
	}
	rel := filepath.Join("fixtures", "agent", "routes")
	dir, err := os.Getwd()
	if err != nil {
		return rel
	}
	for {
		if st, err := os.Stat(filepath.Join(dir, "server", "go.mod")); err == nil && !st.IsDir() {
			return filepath.Join(dir, rel)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return rel
		}
		dir = parent
	}
}
