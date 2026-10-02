#!/usr/bin/env python3
"""Summarizes the agent-nightly q2bot runs and checks the cross-run gates.

Usage: agent_nightly_report.py <runs dir> [--summary FILE]

<runs dir> holds one directory per matrix run, as actions/download-artifact
extracts them: agent-<name>/{exit-code, q2bot-run.txt, <run id>/run.json, ...}.
Prints a Markdown table (also appended to FILE, e.g. $GITHUB_STEP_SUMMARY)
and exits 1 when a gate fails:

- the scripted run completed the campaign (victory);
- every ablation (ablate-*) did measurably worse than the scripted run: it
  completed fewer levels, or as many with more deaths;
- every run that should be there has a run.json.

The per-run expectations (exit codes, -require-complete, -min-model-share)
are checked by the matrix jobs themselves; this script only compares runs.
"""

import json
import os
import sys

REQUIRED = ["scripted", "mock-clean", "mock-noisy", "ablate-constant", "ablate-random"]


def load(root):
    runs = {}
    for entry in sorted(os.listdir(root)):
        d = os.path.join(root, entry)
        if not os.path.isdir(d):
            continue
        name = entry[len("agent-"):] if entry.startswith("agent-") else entry
        rec = {"name": name, "exit": None, "run": None}
        try:
            with open(os.path.join(d, "exit-code")) as f:
                rec["exit"] = int(f.read().strip())
        except (OSError, ValueError):
            pass
        for sub in sorted(os.listdir(d)):
            p = os.path.join(d, sub, "run.json")
            if os.path.isfile(p):
                with open(p) as f:
                    rec["run"] = json.load(f)
                rec["dir"] = sub
                break
        runs[name] = rec
    return runs


def pct(x):
    return "" if x is None else f"{100 * x:.0f}%"


def row(rec):
    r = rec["run"]
    if r is None:
        return f"| {rec['name']} | missing | | | | | | | | {rec['exit']} |"
    t = r.get("totals", {})
    gate = r.get("gate") or {}
    shares = gate.get("model_shares") or {}
    share = ", ".join(f"{k} {pct(v)}" for k, v in sorted(shares.items())) if r.get("backend") in ("jev", "mock") else ""
    api = r.get("api", {})
    lat = api.get("latency_ms", {}).get("p50")
    return (
        f"| {rec['name']} | {r.get('outcome')} ({r.get('reason', '')}) "
        f"| {t.get('levels_completed', 0)}/{t.get('levels', 0)} | {t.get('deaths', 0)} "
        f"| {r.get('game_ms', 0) / 1000:.0f} s | {r.get('wall_ms', 0) / 1000:.0f} s "
        f"| {share} | {pct(gate.get('tick_stale_share')) if share else ''} "
        f"| {'' if lat is None else f'{lat:.0f} ms'} / ${api.get('cost_usd', 0):.3f} | {rec['exit']} |"
    )


def worse(a, base):
    """a did measurably worse than base: fewer levels completed, or as many with more deaths."""
    ta, tb = a.get("totals", {}), base.get("totals", {})
    la, lb = ta.get("levels_completed", 0), tb.get("levels_completed", 0)
    return la < lb or (la == lb and ta.get("deaths", 0) > tb.get("deaths", 0))


def main(argv):
    if len(argv) < 2:
        print(__doc__, file=sys.stderr)
        return 2
    root = argv[1]
    summary = argv[argv.index("--summary") + 1] if "--summary" in argv else None
    runs = load(root)
    lines = [
        "## Agent nightly (lockstep, skill 1, seed 1, 25 deaths per level, 60 min level timeout)",
        "",
        "| run | outcome | levels | deaths | game | wall | model share (gate fields) | ticks on stale | api p50 / cost | exit |",
        "|---|---|---|---|---|---|---|---|---|---|",
    ]
    order = REQUIRED + sorted(n for n in runs if n not in REQUIRED)
    lines += [row(runs[n]) for n in order if n in runs]
    errors = []
    for n in REQUIRED:
        if n not in runs or runs[n]["run"] is None:
            errors.append(f"{n}: no run.json")
    base = runs.get("scripted", {}).get("run")
    if base is not None and base.get("outcome") != "completed":
        errors.append(f"scripted: outcome {base.get('outcome')} ({base.get('reason', '')}), want completed")
    for n, rec in runs.items():
        if not n.startswith("ablate-") or rec["run"] is None or base is None:
            continue
        if not worse(rec["run"], base):
            t, tb = rec["run"]["totals"], base["totals"]
            errors.append(
                f"{n}: not worse than scripted ({t.get('levels_completed')} levels, {t.get('deaths')} deaths vs "
                f"{tb.get('levels_completed')} levels, {tb.get('deaths')} deaths)"
            )
    lines.append("")
    lines += [f"- **gate failed**: {e}" for e in errors] or ["- cross-run gates: ok (scripted completed; ablations worse)"]
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
