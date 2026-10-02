#!/usr/bin/env python3
"""Summarizes the agent-nightly q2bot runs and checks the cross-run gates.

Usage: agent_nightly_report.py <runs dir> [--summary FILE]

<runs dir> holds one directory per matrix entry, as actions/download-artifact
extracts them: agent-<name>/{matrix.json, seed-<n>/{exit-code, q2bot-run.txt,
<run id>/run.json, ...}} (agent_nightly_seeds.sh writes them). An older
layout without seed directories (agent-<name>/{exit-code, <run id>/run.json})
reads as one seed. Prints a Markdown table (also appended to FILE, e.g.
$GITHUB_STEP_SUMMARY) and exits 1 when a gate fails:

- every required entry has a run.json for each of its seeds;
- the scripted runs completed the campaign (victory);
- every ablation (ablate-*) did measurably worse than the scripted run of the
  same seed: it completed fewer levels, or as many with more deaths;
- an entry with min_wins reached victory.pcx on at least that many of its
  seeds (the threshold and why are in docs/AGENT.md, "Nightly");
- every run of an entry with gate passed the provenance gate (run.json
  gate.passed; the matrix job's q2bot validate checks it too).

The per-run exit codes are checked by the matrix jobs themselves.
"""

import json
import os
import re
import sys

REQUIRED = ["scripted", "mock-clean", "mock-noisy", "ablate-constant", "ablate-random"]
GATE_FIELDS = ["target", "fire_policy", "mode"]


def read_int(path):
    try:
        with open(path) as f:
            return int(f.read().strip())
    except (OSError, ValueError):
        return None


def find_run(d):
    """The run.json (and its directory name) under d, or (None, None)."""
    for sub in sorted(os.listdir(d)):
        p = os.path.join(d, sub, "run.json")
        if os.path.isfile(p):
            with open(p) as f:
                return json.load(f), sub
    return None, None


def load(root):
    """{name: {"name", "matrix", "seeds": [{"seed", "exit", "run", "dir"}]}}"""
    entries = {}
    for entry in sorted(os.listdir(root)):
        d = os.path.join(root, entry)
        if not os.path.isdir(d):
            continue
        name = entry[len("agent-"):] if entry.startswith("agent-") else entry
        matrix = {}
        try:
            with open(os.path.join(d, "matrix.json")) as f:
                matrix = json.load(f)
        except (OSError, ValueError):
            pass
        seeds = []
        dirs = sorted((int(m.group(1)), s) for s in os.listdir(d) if (m := re.fullmatch(r"seed-(\d+)", s)))
        if dirs:
            for n, s in dirs:
                run, sub = find_run(os.path.join(d, s))
                seeds.append({"seed": n, "exit": read_int(os.path.join(d, s, "exit-code")), "run": run, "dir": sub})
        else:  # the older single-run layout
            run, sub = find_run(d)
            seeds.append({"seed": (run or {}).get("seed", 1), "exit": read_int(os.path.join(d, "exit-code")),
                          "run": run, "dir": sub})
        # seeds the matrix asked for that left no directory at all
        have = {s["seed"] for s in seeds}
        seeds += [{"seed": n, "exit": None, "run": None, "dir": None} for n in matrix.get("seeds", []) if n not in have]
        seeds.sort(key=lambda s: s["seed"])
        entries[name] = {"name": name, "matrix": matrix, "seeds": seeds}
    return entries


def pct(x):
    return "" if x is None else f"{100 * x:.0f}%"


def victory(run):
    """A whole-campaign run that completed reached victory.pcx."""
    return run is not None and run.get("outcome") == "completed" and "victory" in run.get("reason", "")


def demo3(run):
    """(exits, attempts) of the run's demo3 visits.

    A visit's attempts are its deaths, plus the attempt that exited or was
    still under way when the level ended (timeout, stall, exit). A visit that
    ended at the death cap (outcome death_limit) has no further attempt: the
    campaign fails the level on the death after the last allowed one, so with
    -max-deaths 25 it holds 26 deaths and 26 attempts.
    """
    exits = attempts = 0
    for ep in (run or {}).get("episodes") or []:
        for lv in ep.get("levels") or []:
            if lv.get("map") == "demo3":
                attempts += lv.get("deaths", 0) + (0 if lv.get("outcome") == "death_limit" else 1)
                exits += lv.get("outcome") in ("exit", "victory")
    return exits, attempts


def model_backend(run):
    return run.get("backend") in ("jev", "mock")


def setup(run):
    """What the run.json config section says the run was (empty for an older run.json)."""
    c = run.get("config")
    if not c:
        return ""
    parts = []
    m = c.get("mock")
    if m:
        n = m.get("noise")
        policy = {"scripted": "clean"}.get(m.get("policy"), m.get("policy") or "?")
        parts.append(f"mock {policy}" + (f" ({n['noise']:g}/{n['swap']:g}/{n['low_confidence']:g})" if n else ""))
    if c.get("sim_latency") not in (None, "", "0s"):
        parts.append(c["sim_latency"])
    if c.get("entry_commands"):
        parts.append("cheats " + ",".join(c["entry_commands"]))
    return ", ".join(parts)


def row(name, s):
    r = s["run"]
    label = f"{name} s{s['seed']}"
    if r is None:
        return f"| {label} | missing | | | | | | | | | {s['exit']} |"
    t = r.get("totals", {})
    gate = r.get("gate") or {}
    shares = gate.get("model_shares") or {}
    share = ", ".join(f"{k} {pct(v)}" for k, v in sorted(shares.items())) if model_backend(r) else ""
    api = r.get("api", {})
    lat = api.get("latency_ms", {}).get("p50")
    ex, at = demo3(r)
    return (
        f"| {label} | {r.get('outcome')} ({r.get('reason', '')}) | {setup(r)} "
        f"| {t.get('levels_completed', 0)}/{t.get('levels', 0)} | {t.get('deaths', 0)} | {ex}/{at} "
        f"| {r.get('game_ms', 0) / 1000:.0f} s | {r.get('wall_ms', 0) / 1000:.0f} s "
        f"| {share} | {pct(gate.get('tick_stale_share')) if share else ''} "
        f"| {'' if lat is None else f'{lat:.0f} ms'} / ${api.get('cost_usd', 0):.3f} | {s['exit']} |"
    )


def aggregate(name, e):
    """The entry's seeds together: victories, pooled demo3 survival, the lowest gate-field share."""
    runs = [s["run"] for s in e["seeds"] if s["run"] is not None]
    wins = sum(victory(r) for r in runs)
    ex = sum(demo3(r)[0] for r in runs)
    at = sum(demo3(r)[1] for r in runs)
    surv = f"{ex}/{at} ({100 * ex / at:.1f}%)" if at else "no demo3 attempt"
    shares = [v for r in runs if model_backend(r) for k, v in ((r.get("gate") or {}).get("model_shares") or {}).items()
              if k in GATE_FIELDS]
    low = f", lowest gate-field model share {pct(min(shares))}" if shares else ""
    need = e["matrix"].get("min_wins", 0)
    gate = f" (gate: at least {need})" if need else ""
    return f"- **{name}**: {wins} of {len(e['seeds'])} seeds to victory{gate}; demo3 survival {surv}{low}"


def worse(a, base):
    """a did measurably worse than base: fewer levels completed, or as many with more deaths."""
    ta, tb = a.get("totals", {}), base.get("totals", {})
    la, lb = ta.get("levels_completed", 0), tb.get("levels_completed", 0)
    return la < lb or (la == lb and ta.get("deaths", 0) > tb.get("deaths", 0))


def check(entries):
    errors = []
    for n in REQUIRED:
        if n not in entries:
            errors.append(f"{n}: no artifact")
    for n, e in entries.items():
        for s in e["seeds"]:
            if s["run"] is None and (n in REQUIRED or e["matrix"]):
                errors.append(f"{n} seed {s['seed']}: no run.json")
    scripted = {s["seed"]: s["run"] for s in entries.get("scripted", {}).get("seeds", []) if s["run"] is not None}
    for seed, r in sorted(scripted.items()):
        if r.get("outcome") != "completed":
            errors.append(f"scripted seed {seed}: outcome {r.get('outcome')} ({r.get('reason', '')}), want completed")
    for n, e in entries.items():
        m = e["matrix"]
        runs = [s for s in e["seeds"] if s["run"] is not None]
        if n.startswith("ablate-"):
            for s in runs:
                base = scripted.get(s["seed"])
                if base is not None and not worse(s["run"], base):
                    t, tb = s["run"].get("totals", {}), base.get("totals", {})
                    errors.append(
                        f"{n} seed {s['seed']}: not worse than scripted ({t.get('levels_completed')} levels, "
                        f"{t.get('deaths')} deaths vs {tb.get('levels_completed')} levels, {tb.get('deaths')} deaths)"
                    )
        need = m.get("min_wins", 0)
        if need:
            wins = sum(victory(s["run"]) for s in runs)
            if wins < need:
                errors.append(f"{n}: {wins} of {len(e['seeds'])} seeds reached victory.pcx, want at least {need}")
        if m.get("gate"):
            for s in runs:
                g = s["run"].get("gate") or {}
                if not g.get("passed"):
                    why = "; ".join(g.get("reasons") or []) or "no gate in run.json"
                    errors.append(f"{n} seed {s['seed']}: not model-driven ({why})")
    return errors


def main(argv):
    if len(argv) < 2:
        print(__doc__, file=sys.stderr)
        return 2
    root = argv[1]
    summary = argv[argv.index("--summary") + 1] if "--summary" in argv else None
    entries = load(root)
    lines = [
        "## Agent nightly (lockstep, skill 1, 25 deaths per level, 60 min level timeout)",
        "",
        "| run | outcome | setup | levels | deaths | demo3 | game | wall | model share (gate fields) | ticks on stale "
        "| api p50 / cost | exit |",
        "|---|---|---|---|---|---|---|---|---|---|---|---|",
    ]
    order = REQUIRED + sorted(n for n in entries if n not in REQUIRED)
    for n in order:
        if n in entries:
            lines += [row(n, s) for s in entries[n]["seeds"]]
    lines.append("")
    lines += [aggregate(n, entries[n]) for n in order if n in entries and len(entries[n]["seeds"]) > 1]
    errors = check(entries)
    lines.append("")
    lines += [f"- **gate failed**: {e}" for e in errors] or [
        "- cross-run gates: ok (scripted completed; ablations worse; seed thresholds met; gated runs model-driven)"
    ]
    text = "\n".join(lines) + "\n"
    sys.stdout.write(text)
    if summary:
        with open(summary, "a") as f:
            f.write(text)
    for e in errors:
        print(f"::error::{e}", file=sys.stderr)
    return 1 if errors else 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
