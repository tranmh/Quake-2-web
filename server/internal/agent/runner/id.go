package runner

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"regexp"
	"time"
)

// NewRunID returns a fresh run id: the UTC time to the second and 8 random
// hex digits from crypto/rand ("20261002T153045Z-3fa94c1e"). It is made
// once per run, outside every decision path, so it never perturbs a
// lockstep run.
func NewRunID(now time.Time) (string, error) {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("runner: run id: %w", err)
	}
	return now.UTC().Format("20060102T150405Z") + "-" + hex.EncodeToString(b[:]), nil
}

// runIDPattern is what a run id (a run directory's name) may look like:
// no path separators, no dots first.
var runIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$`)

// ValidRunID reports whether id can name a run directory.
func ValidRunID(id string) bool { return runIDPattern.MatchString(id) }
