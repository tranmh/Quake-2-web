// Package testutil holds helpers shared by unit and golden tests: repository
// root detection, fixture and demo pak location (docs/FIXTURES.md), a JSON
// Lines reader and bit-exact float comparison.
package testutil

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"testing"
)

// RepoRoot returns the repository root (the directory holding server/ and
// Quake-2/), found by walking up from the working directory.
func RepoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if st, err := os.Stat(filepath.Join(dir, "server", "go.mod")); err == nil && !st.IsDir() {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("testutil: repository root not found")
		}
		dir = parent
	}
}

// FixturesDir returns $Q2_FIXTURES or <repo>/fixtures/generated.
func FixturesDir() string {
	if v := os.Getenv("Q2_FIXTURES"); v != "" {
		return v
	}
	root, err := RepoRoot()
	if err != nil {
		return ""
	}
	return filepath.Join(root, "fixtures", "generated")
}

// BaseDir returns $Q2_BASEDIR or <repo>/assets/demo.
func BaseDir() string {
	if v := os.Getenv("Q2_BASEDIR"); v != "" {
		return v
	}
	root, err := RepoRoot()
	if err != nil {
		return ""
	}
	return filepath.Join(root, "assets", "demo")
}

// DemoPakPath returns the path of baseq2/pak0.pak under BaseDir.
func DemoPakPath() string {
	return filepath.Join(BaseDir(), "baseq2", "pak0.pak")
}

// RequireFile skips the test when path does not exist and returns path.
func RequireFile(t testing.TB, path string) string {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Skipf("missing %s (%v); skipping", path, err)
	}
	return path
}

// Fixture returns the path of a fixture relative to FixturesDir, skipping the
// test when it is absent.
func Fixture(t testing.TB, rel string) string {
	t.Helper()
	return RequireFile(t, filepath.Join(FixturesDir(), rel))
}

// DemoPak returns the demo pak path, skipping when absent.
func DemoPak(t testing.TB) string {
	t.Helper()
	return RequireFile(t, DemoPakPath())
}

// ReadJSONL calls fn for every non-empty line of a JSON Lines file with its
// 1-based line number. Iteration stops at the first error returned by fn.
func ReadJSONL(path string, fn func(lineNo int, line []byte) error) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<30)
	n := 0
	for sc.Scan() {
		n++
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		if err := fn(n, line); err != nil {
			return fmt.Errorf("%s:%d: %w", path, n, err)
		}
	}
	return sc.Err()
}

// ReadJSON decodes a whole JSON file into v.
func ReadJSON(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

// F32 narrows a %.9g-printed C float parsed as float64, like Go float32(v).
func F32(v float64) float32 { return float32(v) }

// Vec3 narrows a JSON array of 3 numbers.
func Vec3(v []float64) [3]float32 {
	var o [3]float32
	for i := 0; i < 3 && i < len(v); i++ {
		o[i] = float32(v[i])
	}
	return o
}

// Short3 converts a JSON array of 3 integers to int16.
func Short3(v []int64) [3]int16 {
	var o [3]int16
	for i := 0; i < 3 && i < len(v); i++ {
		o[i] = int16(v[i])
	}
	return o
}

// SameF32 reports bitwise equality (distinguishing -0 and +0, NaN payloads).
func SameF32(a, b float32) bool { return math.Float32bits(a) == math.Float32bits(b) }

// SameVec3 reports bitwise equality of all components.
func SameVec3(a, b [3]float32) bool {
	return SameF32(a[0], b[0]) && SameF32(a[1], b[1]) && SameF32(a[2], b[2])
}

// FmtF32 formats a float32 with its bit pattern for mismatch reports.
func FmtF32(f float32) string {
	return fmt.Sprintf("%.9g(0x%08x)", f, math.Float32bits(f))
}

// FmtVec3 formats a vector with bit patterns.
func FmtVec3(v [3]float32) string {
	return fmt.Sprintf("[%s %s %s]", FmtF32(v[0]), FmtF32(v[1]), FmtF32(v[2]))
}

// Hex decodes a lowercase hex string, failing the test on error.
func Hex(t testing.TB, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatalf("bad hex %q: %v", s, err)
	}
	return b
}

// Latin1 converts a string decoded from JSON (where the oracle escaped bytes
// >= 0x7f as \u00XX) back to the original byte string.
func Latin1(s string) string {
	b := make([]byte, 0, len(s))
	for _, r := range s {
		b = append(b, byte(r))
	}
	return string(b)
}
