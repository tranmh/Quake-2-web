package cache

import (
	"fmt"
	"strings"
	"time"
)

// Mode is what the cache matches requests by.
type Mode uint8

// Modes.
const (
	Off    Mode = iota // no cache
	Exact              // equal requests (the request digest)
	Approx             // similar requests (the approximate key)
)

// Granularity is how coarse the approximate key is.
type Granularity uint8

// Granularities.
const (
	Fine   Granularity = iota // 8 bearing sectors, finer bands (the default)
	Coarse                    // 4 sectors, fewer bands, fewer fields
)

// DefaultMaxEntries is a Store's size when Config.MaxEntries is 0: more
// than the requests of a whole 4-level campaign (about 10 000), so a
// campaign never evicts; an entry is about 1-2 KB.
const DefaultMaxEntries = 16384

// Markers of a reused answer (decide.Response.Cache, trace decision
// "cache").
const (
	MarkExact  = "exact"
	MarkApprox = "approx"
)

// Config configures a Store.
type Config struct {
	Mode        Mode
	Granularity Granularity // Approx only
	// TTL is the oldest answer reused, in game time (0: no limit).
	TTL time.Duration
	// MaxEntries bounds the entries kept (0: DefaultMaxEntries).
	MaxEntries int
}

// Parse reads a mode: off ("" too), exact, approx (approx:fine),
// approx:fine or approx:coarse.
func Parse(s string) (Config, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "off":
		return Config{}, nil
	case "exact":
		return Config{Mode: Exact}, nil
	case "approx", "approx:fine":
		return Config{Mode: Approx, Granularity: Fine}, nil
	case "approx:coarse":
		return Config{Mode: Approx, Granularity: Coarse}, nil
	}
	return Config{}, fmt.Errorf("cache: mode %q (off|exact|approx[:fine|coarse])", s)
}

// Enabled reports a mode other than Off.
func (c Config) Enabled() bool { return c.Mode != Off }

// String formats the mode as Parse reads it ("off", "exact",
// "approx:fine", "approx:coarse"); the TTL is not part of it.
func (c Config) String() string {
	switch c.Mode {
	case Exact:
		return "exact"
	case Approx:
		if c.Granularity == Coarse {
			return "approx:coarse"
		}
		return "approx:fine"
	}
	return "off"
}

// Marker is the marker of the answers the mode reuses ("" when off).
func (c Config) Marker() string {
	switch c.Mode {
	case Exact:
		return MarkExact
	case Approx:
		return MarkApprox
	}
	return ""
}

func (c Config) maxEntries() int {
	if c.MaxEntries > 0 {
		return c.MaxEntries
	}
	return DefaultMaxEntries
}
