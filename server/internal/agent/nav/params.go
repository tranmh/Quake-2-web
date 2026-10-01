package nav

import (
	"encoding/json"
	"fmt"
	"hash/fnv"

	"quake2web/server/internal/agent/nav/navsim"
)

// Params are the build settings. Everything that changes the built graph
// is here or in the versions PhysicsHash folds in; the number of build
// workers is not (the output does not depend on it).
type Params struct {
	// Grid is the ground sample spacing (world aligned).
	Grid float32 `json:"grid"`
	// WaterGrid is the swim sample spacing.
	WaterGrid float32 `json:"waterGrid"`
	// StepMsec is the simulated usercmd length.
	StepMsec int `json:"stepMsec"`
	// Gravity and AirAccelerate are sv_gravity and sv_airaccelerate.
	Gravity       int16   `json:"gravity"`
	AirAccelerate float32 `json:"airAccelerate"`
	// MaxDegree caps the out edges of a node (special edges excepted).
	MaxDegree int `json:"maxDegree"`
	// RegionRadius bounds a region around its seed node.
	RegionRadius float32 `json:"regionRadius"`
	// LedgeReach is the horizontal range of jump/drop targets from a ledge;
	// MaxDrop and MaxRise bound their height difference.
	LedgeReach float32 `json:"ledgeReach"`
	MaxDrop    float32 `json:"maxDrop"`
	MaxRise    float32 `json:"maxRise"`
}

// DefaultParams returns the settings the agent uses.
func DefaultParams() Params {
	return Params{
		Grid: 32, WaterGrid: 48, StepMsec: 25, Gravity: 800, AirAccelerate: 0,
		MaxDegree: 24, RegionRadius: 192, LedgeReach: 320, MaxDrop: 1024, MaxRise: 64,
	}
}

// Physics returns the navsim physics of these params.
func (p Params) Physics() navsim.Physics {
	ph := navsim.DefaultPhysics()
	ph.Gravity, ph.AirAccelerate, ph.StepMsec = p.Gravity, p.AirAccelerate, p.StepMsec
	return ph
}

// Validate rejects params a build cannot use.
func (p Params) Validate() error {
	switch {
	case p.Grid < 8 || p.Grid > 256:
		return fmt.Errorf("nav: grid %v not in [8, 256]", p.Grid)
	case p.WaterGrid < 8 || p.WaterGrid > 256:
		return fmt.Errorf("nav: water grid %v not in [8, 256]", p.WaterGrid)
	case p.StepMsec < 1 || p.StepMsec > 250:
		return fmt.Errorf("nav: step msec %d not in [1, 250]", p.StepMsec)
	case p.MaxDegree < 4:
		return fmt.Errorf("nav: max degree %d < 4", p.MaxDegree)
	case p.LedgeReach < 0 || p.MaxDrop < 0 || p.MaxRise < 0 || p.RegionRadius <= 0:
		return fmt.Errorf("nav: negative reach")
	}
	return nil
}

// PhysicsHash identifies everything besides the map that the built graph
// depends on: the file format, the builder and simulator versions, the
// physics and the build settings (16 hex digits of FNV-1a 64).
func (p Params) PhysicsHash() string {
	h := fnv.New64a()
	b, _ := json.Marshal(p) // fixed field order: deterministic
	fmt.Fprintf(h, "format=%d build=%d navsim=%d pmove=q2-3.19 params=%s", FormatVersion, BuildVersion, navsim.Version, b)
	return fmt.Sprintf("%016x", h.Sum64())
}

// CacheName is the cache file name of a map: <map>-<checksum hex>-v<format>-<physics hash>.json.gz.
func CacheName(mapName string, checksum uint32, p Params) string {
	return fmt.Sprintf("%s-%08x-v%d-%s.json.gz", mapName, checksum, FormatVersion, p.PhysicsHash())
}
