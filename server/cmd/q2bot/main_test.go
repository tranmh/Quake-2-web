package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"quake2web/server/internal/agent/backend/jevtest"
	"quake2web/server/internal/agent/backend/scripted"
	"quake2web/server/internal/agent/budget"
	"quake2web/server/internal/agent/metrics"
	"quake2web/server/internal/agent/runner"
	"quake2web/server/internal/testutil"
)

// q2bot runs the command with an environment of vars only.
func q2bot(vars map[string]string, args ...string) (int, string, string) {
	var out, errOut bytes.Buffer
	code := run(args, &out, &errOut, func(k string) string { return vars[k] })
	return code, out.String(), errOut.String()
}

func TestUsage(t *testing.T) {
	cases := []struct {
		args []string
		want string
	}{
		{nil, "usage: q2bot <command>"},
		{[]string{"fly"}, `unknown command "fly"`},
		{[]string{"run", "-bogus"}, "flag provided but not defined"},
		{[]string{"run", "-h"}, "usage: q2bot run"},
		{[]string{"run", "-backend", "gpt"}, `unknown backend "gpt"`},
		{[]string{"run", "-session", "remote"}, `unknown session "remote"`},
		{[]string{"run", "-trace", "every:0"}, "trace mode"},
		{[]string{"run", "-sim-latency", "soon"}, "latency"},
		{[]string{"run", "-on-exhausted", "maybe"}, "exhaustion policy"},
		{[]string{"run", "-episodes", "0"}, "-episodes 0"},
		{[]string{"run", "-skill", "4"}, "-skill 4"},
		{[]string{"run", "-skill", "-5"}, "-skill -5"},
		{[]string{"run", "-min-model-share", "2"}, "shares in 0..1"},
		{[]string{"run", "-backend", "replay"}, "needs -replay-trace"},
		{[]string{"run", "-backend", "jev"}, "needs TYPESAFE_API_KEY"},
		{[]string{"run", "-api-key", "x"}, "flag provided but not defined"},
		{[]string{"run", "extra"}, `unexpected argument "extra"`},
		{[]string{"replay"}, "-trace is required"},
		{[]string{"replay", "-trace", "x", "-mode", "fast"}, `-mode "fast"`},
		{[]string{"summarize"}, "one run directory"},
		{[]string{"validate", "a", "b"}, "one run directory"},
		{[]string{"validate", "a", "-min-model-share", "-1"}, "shares are in 0..1"},
		{[]string{"jev-probe", "-lane", "medium"}, `-lane "medium"`},
		{[]string{"jev-probe", "-n", "0"}, "-n 0"},
	}
	for _, tc := range cases {
		code, _, errOut := q2bot(nil, tc.args...)
		if code != 2 || !strings.Contains(errOut, tc.want) {
			t.Errorf("%v: exit %d, stderr %q (want 2, %q)", tc.args, code, errOut, tc.want)
		}
	}
}

func TestRunConfig(t *testing.T) {
	vars := map[string]string{"TYPESAFE_API_KEY": "sk-from-env", "JEV_MODEL": "jev-1.13.0"}
	e := &env{stdout: &bytes.Buffer{}, stderr: &bytes.Buffer{}, getenv: func(k string) string { return vars[k] }}
	cfg, o, err := runConfig(e, []string{"-maps", "demo1, demo2", "-skill", "2", "-backend", "mock", "-session", "inproc",
		"-sim-latency", "80ms,150ms", "-episodes", "3", "-seed", "9", "-out", "x/runs", "-trace", "every:5", "-record=false",
		"-budget-usd", "2", "-budget-queries", "100", "-max-qps", "8", "-on-exhausted", "stop", "-account-qps", "30",
		"-require-complete", "-min-model-share", "0.7", "-max-stale-rate", "0.2", "-json", "-pak", "p.pak", "-nav-dir", "nav",
		"-cheats", "god,notarget", "-level-timeout", "5m", "-episode-timeout", "1h", "-max-deaths", "-1", "-v"})
	if err != nil {
		t.Fatal(err)
	}
	want := runner.Config{Maps: []string{"demo1", "demo2"}, Backend: "mock", Session: "inproc", Episodes: 3, Seed: 9, OutDir: "x/runs",
		Trace: runner.TraceMode(5), RequireComplete: true, MinModelShare: 0.7, MaxStaleRate: 0.2, NavDir: "nav",
		EntryCommands: []string{"god", "notarget"}, LevelTimeout: 5 * time.Minute, EpisodeTimeout: time.Hour, MaxDeaths: -1, Verbose: true,
		SimLatency: runner.Latency{Samples: []time.Duration{80 * time.Millisecond, 150 * time.Millisecond}, Set: true},
		Budget:     budget.Limits{USD: 2, Queries: 100, MaxQPS: 8, OnExhausted: budget.Stop}}
	got := cfg
	if got.Skill == nil || *got.Skill != 2 {
		t.Fatalf("skill %v", got.Skill)
	}
	if got.Jev.APIKey.Reveal() != "sk-from-env" || got.Jev.Model != "jev-1.13.0" {
		t.Fatalf("the jev config is not the environment's: %+v", got.Jev)
	}
	got.Skill, got.Jev = nil, want.Jev
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("config\n%+v\nwant\n%+v", got, want)
	}
	if o != (runOptions{pak: "p.pak", json: true, accountQPS: 30, gate: true}) {
		t.Fatalf("options %+v", o)
	}
	// the defaults
	cfg, o, err = runConfig(e, nil)
	if err != nil || cfg.Backend != "scripted" || cfg.Session != "lockstep" || cfg.Episodes != 1 || cfg.Seed != 1 || cfg.OutDir != "runs" ||
		cfg.Trace != runner.TraceFull || !cfg.Record || cfg.Skill != nil || cfg.Maps != nil || cfg.SimLatency.Set || o.gate {
		t.Fatalf("defaults %+v %+v %v", cfg, o, err)
	}
	// a campaign file names its directory
	dir := t.TempDir()
	file := filepath.Join(dir, "campaign.json")
	if err := os.WriteFile(file, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if cfg, _, _ := runConfig(e, []string{"-campaign", file}); cfg.RoutesDir != dir {
		t.Fatalf("campaign dir %q", cfg.RoutesDir)
	}
}

// TestRunConfigMock: the mock flags reach the config only when given; a
// share of 0 turns its effect off (-1 for jevtest), and bad values are
// usage errors.
func TestRunConfigMock(t *testing.T) {
	e := &env{stdout: &bytes.Buffer{}, stderr: &bytes.Buffer{}, getenv: func(string) string { return "" }}
	cfg, _, err := runConfig(e, []string{"-backend", "mock"})
	if err != nil || cfg.MockPolicy != "" || cfg.MockNoise != 0 || cfg.MockSwap != 0 || cfg.MockLowConfidence != 0 {
		t.Fatalf("defaults %+v %v", cfg, err)
	}
	cfg, _, err = runConfig(e, []string{"-backend", "mock", "-mock-policy", "scripted", "-mock-noise", "0.5", "-mock-swap", "0", "-mock-lowconf", "0.2"})
	if err != nil || cfg.MockPolicy != runner.MockPolicyScripted || cfg.MockNoise != 0.5 || cfg.MockSwap != -1 || cfg.MockLowConfidence != 0.2 {
		t.Fatalf("given %+v %v", cfg, err)
	}
	for _, args := range [][]string{{"-mock-policy", "chaos"}, {"-mock-swap", "1.5"}, {"-mock-noise", "-0.1"}} {
		if _, _, err := runConfig(e, args); !errors.Is(err, errUsage) {
			t.Errorf("%v: %v", args, err)
		}
	}
}

func TestParseFlagsInterleaved(t *testing.T) {
	e := &env{stdout: &bytes.Buffer{}, stderr: &bytes.Buffer{}, getenv: func(string) string { return "" }}
	fs := newFlags("x", e)
	j := fs.Bool("json", false, "")
	m := fs.Float64("min-model-share", 0, "")
	pos, err := parseFlags(fs, []string{"runs/a", "-json", "-min-model-share", "0.5", "b"})
	if err != nil || !*j || *m != 0.5 || !reflect.DeepEqual(pos, []string{"runs/a", "b"}) {
		t.Fatalf("%v %v %v %v", pos, err, *j, *m)
	}
}

func TestJevProbeNoKey(t *testing.T) {
	code, out, errOut := q2bot(map[string]string{}, "jev-probe", "-out", filepath.Join(t.TempDir(), "p.json"))
	if code != 2 || out != "" || !strings.Contains(errOut, "TYPESAFE_API_KEY is not set") || !strings.Contains(errOut, "never as a\nflag") {
		t.Fatalf("exit %d\n%s\n%s", code, out, errOut)
	}
}

// TestJevProbeMock probes the fake Jev server (loopback, the only hosts a
// test binary may reach) and checks what the probe writes.
func TestJevProbeMock(t *testing.T) {
	const key = "sk-probe-test-0123456789abcdef"
	srv := jevtest.NewServer(jevtest.Options{APIKey: key, Policy: jevtest.NewScripted(scripted.Config{Seed: 1})})
	defer srv.Close()
	out := filepath.Join(t.TempDir(), "testdata", "live-probe.json")
	vars := map[string]string{"TYPESAFE_API_KEY": key, "JEV_BASE_URL": srv.URL(), "JEV_ALLOW_CUSTOM_BASE": "1"}
	for _, lane := range []string{"fast", "slow"} {
		code, stdout, errOut := q2bot(vars, "jev-probe", "-lane", lane, "-n", "2", "-out", out)
		if code != 0 {
			t.Fatalf("%s: exit %d\n%s\n%s", lane, code, stdout, errOut)
		}
		raw, err := os.ReadFile(out)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(raw, []byte(key)) || strings.Contains(stdout+errOut, key) {
			t.Fatal("the key reached the probe's output")
		}
		var doc probeDoc
		if err := json.Unmarshal(raw, &doc); err != nil {
			t.Fatal(err)
		}
		if doc.Schema != ProbeSchema || doc.Lane != lane || len(doc.Results) != 2 || len(doc.Exchanges) != 2 || doc.Model == "" {
			t.Fatalf("%s: probe %+v", lane, doc)
		}
		for _, r := range doc.Results {
			if r.Err != "" || r.Status != 200 || len(r.Answers) < 3 || r.InputTokens == 0 {
				t.Fatalf("%s: result %+v", lane, r)
			}
		}
		var x struct {
			Request        json.RawMessage     `json:"request"`
			Response       json.RawMessage     `json:"response"`
			RequestHeaders map[string][]string `json:"request_headers"`
		}
		if err := json.Unmarshal(doc.Exchanges[0], &x); err != nil || x.Request == nil || x.Response == nil ||
			x.RequestHeaders["Authorization"][0] != "[REDACTED]" {
			t.Fatalf("%s: exchange %s (%v)", lane, doc.Exchanges[0], err)
		}
		if !strings.Contains(stdout, "target") && lane == "fast" || !strings.Contains(stdout, "mode") && lane == "slow" {
			t.Fatalf("%s: stdout lacks the answers:\n%s", lane, stdout)
		}
	}
	if srv.Count() != 4 {
		t.Fatalf("%d calls", srv.Count())
	}
}

// gameCap bounds a run's game time under -race (unless Q2_AGENT_LONG=1).
func gameCap() []string {
	if raceEnabled && os.Getenv("Q2_AGENT_LONG") != "1" {
		return []string{"-episode-timeout", "10s"}
	}
	return nil
}

func onlyRun(t *testing.T, out string) string {
	t.Helper()
	ents, err := os.ReadDir(out)
	if err != nil || len(ents) != 1 {
		t.Fatalf("run directories in %s: %v %v", out, ents, err)
	}
	return filepath.Join(out, ents[0].Name())
}

// TestGate is the CLI gate: a scripted lockstep run of demo1, then
// validate, a strict replay and summarize all succeed.
func TestGate(t *testing.T) {
	t.Parallel()
	pak := testutil.DemoPak(t)
	out := t.TempDir()
	args := append([]string{"run", "-backend", "scripted", "-session", "lockstep", "-maps", "demo1", "-out", out, "-pak", pak, "-json"}, gameCap()...)
	code, stdout, errOut := q2bot(nil, args...)
	if code != 0 {
		t.Fatalf("run: exit %d\n%s", code, errOut)
	}
	var s metrics.RunSummary
	if err := json.Unmarshal([]byte(stdout), &s); err != nil || s.Schema != metrics.Schema {
		t.Fatalf("-json output: %v\n%s", err, stdout)
	}
	dir := onlyRun(t, out)
	if filepath.Base(dir) != s.Run {
		t.Fatalf("run dir %s, run %s", dir, s.Run)
	}
	before, _ := os.ReadFile(filepath.Join(dir, runner.RunFile))

	if code, stdout, errOut = q2bot(nil, "validate", dir); code != 0 || !strings.Contains(stdout, ": valid") {
		t.Fatalf("validate: exit %d\n%s\n%s", code, stdout, errOut)
	}
	if code, stdout, errOut = q2bot(nil, "replay", "-strict", "-pak", pak, "-trace", filepath.Join(dir, "ep-000", runner.TraceFile)); code != 0 ||
		!strings.Contains(stdout, "no divergence") {
		t.Fatalf("replay: exit %d\n%s\n%s", code, stdout, errOut)
	}
	if code, stdout, errOut = q2bot(nil, "summarize", dir); code != 0 || !strings.Contains(stdout, "run "+s.Run) {
		t.Fatalf("summarize: exit %d\n%s\n%s", code, stdout, errOut)
	}
	after, _ := os.ReadFile(filepath.Join(dir, runner.RunFile))
	if !bytes.Equal(before, after) {
		t.Fatal("summarize changed run.json")
	}
	summarizeTruncated(t, dir, before)
	// a run that does not complete fails -require-complete
	out2 := t.TempDir()
	code, _, errOut = q2bot(nil, "run", "-maps", "demo1", "-out", out2, "-pak", pak, "-record=false", "-episode-timeout", "3s", "-require-complete")
	if code != 1 || !strings.Contains(errOut, "did not complete") {
		t.Fatalf("-require-complete: exit %d\n%s", code, errOut)
	}
	// a damaged run does not validate
	if err := os.WriteFile(filepath.Join(onlyRun(t, out2), runner.RunFile), []byte(`{"schema":"other"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, stdout, _ = q2bot(nil, "validate", onlyRun(t, out2)); code != 1 || !strings.Contains(stdout, "INVALID") {
		t.Fatalf("validate a damaged run: exit %d\n%s", code, stdout)
	}
}

// TestMockGate runs the mock backend: its run.json carries the provenance,
// and -min-model-share decides the exit status of run and validate.
func TestMockGate(t *testing.T) {
	t.Parallel()
	pak := testutil.DemoPak(t)
	out := t.TempDir()
	limit := []string{"-episode-timeout", "30s"}
	if c := gameCap(); c != nil {
		limit = c
	}
	args := append([]string{"run", "-backend", "mock", "-maps", "demo1", "-out", out, "-pak", pak, "-min-model-share", "0.99"}, limit...)
	code, stdout, errOut := q2bot(nil, args...)
	if code != 1 || !strings.Contains(errOut, "provenance gate not met") || !strings.Contains(stdout, "model jev-1.13.0") {
		t.Fatalf("run: exit %d\n%s\n%s", code, stdout, errOut)
	}
	dir := onlyRun(t, out)
	var s metrics.RunSummary
	raw, _ := os.ReadFile(filepath.Join(dir, runner.RunFile))
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatal(err)
	}
	if s.Gate == nil || !s.Gate.ModelBackend || s.Gate.MinModelShare != 0.99 || s.Ticks == nil || s.API.OK == 0 || s.API.InputTokens == 0 {
		t.Fatalf("run.json %+v", s)
	}
	if code, stdout, _ = q2bot(nil, "validate", dir, "-min-model-share", "0.01", "-max-stale-rate", "1"); code != 0 || !strings.Contains(stdout, "gate") {
		t.Fatalf("validate at 1%%: exit %d\n%s", code, stdout)
	}
	if code, stdout, _ = q2bot(nil, "validate", dir, "-min-model-share", "0.99"); code != 1 || !strings.Contains(stdout, "NOT met") {
		t.Fatalf("validate at 99%%: exit %d\n%s", code, stdout)
	}
}

// summarizeTruncated copies run dir with its trace cut short: summarize
// exits 1 and keeps the good run.json, -force rewrites it, and a run
// without run.json (a crash) gets the partial one.
func summarizeTruncated(t *testing.T, dir string, good []byte) {
	t.Helper()
	cp := filepath.Join(t.TempDir(), filepath.Base(dir))
	if err := os.MkdirAll(filepath.Join(cp, "ep-000"), 0o755); err != nil {
		t.Fatal(err)
	}
	tr, err := os.ReadFile(filepath.Join(dir, "ep-000", runner.TraceFile))
	if err != nil {
		t.Fatal(err)
	}
	runJSON := filepath.Join(cp, runner.RunFile)
	if err := os.WriteFile(filepath.Join(cp, "ep-000", runner.TraceFile), tr[:len(tr)/2], 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(runJSON, good, 0o644); err != nil {
		t.Fatal(err)
	}
	code, _, errOut := q2bot(nil, "summarize", cp)
	if got, _ := os.ReadFile(runJSON); code != 1 || !bytes.Equal(got, good) || !strings.Contains(errOut, "left as it is") {
		t.Fatalf("summarize a truncated trace: exit %d, run.json kept %v\n%s", code, bytes.Equal(got, good), errOut)
	}
	if code, _, errOut = q2bot(nil, "summarize", "-force", cp); code != 1 {
		t.Fatalf("summarize -force: exit %d\n%s", code, errOut)
	}
	var s metrics.RunSummary
	if got, _ := os.ReadFile(runJSON); bytes.Equal(got, good) || json.Unmarshal(got, &s) != nil || s.Outcome != metrics.OutcomeIncomplete {
		t.Fatalf("summarize -force kept run.json, or wrote %+v", s)
	}
	if err := os.Remove(runJSON); err != nil {
		t.Fatal(err)
	}
	if code, _, errOut = q2bot(nil, "summarize", cp); code != 1 {
		t.Fatalf("summarize without run.json: exit %d\n%s", code, errOut)
	}
	if _, err := os.Stat(runJSON); err != nil {
		t.Fatalf("a crashed run gets no run.json: %v", err)
	}
}

// TestRunConfigRejected: maps the campaign does not start with, or does
// not visit in that order, are usage errors (exit 2) and make no run
// directory.
func TestRunConfigRejected(t *testing.T) {
	pak := testutil.DemoPak(t)
	for _, tc := range []struct{ maps, want string }{
		{"demo2", "must include the campaign's start"},
		{"demo1,demo3", "not among the campaign's first visits"},
	} {
		out := t.TempDir()
		code, _, errOut := q2bot(nil, "run", "-maps", tc.maps, "-out", out, "-pak", pak)
		if code != 2 || !strings.Contains(errOut, tc.want) || !strings.Contains(errOut, "usage: q2bot run") {
			t.Errorf("-maps %s: exit %d\n%s", tc.maps, code, errOut)
		}
		if ents, _ := os.ReadDir(out); len(ents) != 0 {
			t.Errorf("-maps %s made %v", tc.maps, ents)
		}
	}
}
