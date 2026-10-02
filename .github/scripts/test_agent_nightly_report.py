#!/usr/bin/env python3
"""Tests of agent_nightly_report.py on synthetic artifact trees (stdlib only).

Run: python3 .github/scripts/test_agent_nightly_report.py
"""

import io
import json
import os
import sys
import tempfile
import unittest
from contextlib import redirect_stderr, redirect_stdout

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import agent_nightly_report as report  # noqa: E402


def run_json(backend, won, demo3_deaths, levels_done=None, passed=True, policy=None, seed=1):
    """A run.json as q2bot writes it, reduced to the fields the report reads."""
    levels = [
        {"lvl": 0, "map": "demo1", "visit": 0, "outcome": "exit", "deaths": 0},
        {"lvl": 1, "map": "demo2", "visit": 0, "outcome": "exit", "deaths": 1},
        {"lvl": 2, "map": "demo3", "visit": 0, "outcome": "exit" if won else "death_limit", "deaths": demo3_deaths},
    ]
    if won:
        levels.append({"lvl": 3, "map": "demo2", "visit": 1, "outcome": "victory", "deaths": 0})
    done = levels_done if levels_done is not None else sum(lv["outcome"] in ("exit", "victory") for lv in levels)
    r = {
        "schema": "q2bot.run/1",
        "outcome": "completed" if won else "failed",
        "reason": "1 of 1 episodes to victory.pcx" if won else f"episode 0: {demo3_deaths} deaths on demo3",
        "backend": backend,
        "seed": seed,
        "game_ms": 1000000,
        "wall_ms": 30000,
        "totals": {"levels": len(levels), "levels_completed": done, "deaths": 1 + demo3_deaths},
        "api": {"latency_ms": {"p50": 212}, "cost_usd": 0.5},
        "episodes": [{"index": 0, "seed": seed, "levels": levels}],
        "config": {"backend": backend, "seed": seed, "sim_latency": "212ms" if backend == "mock" else "0s"},
    }
    if backend == "mock":
        r["config"]["mock"] = {"policy": policy or "noisy"}
        if (policy or "noisy") == "noisy":
            r["config"]["mock"]["noise"] = {"noise": 0.3, "swap": 0.1, "low_confidence": 0.1}
        r["gate"] = {"passed": passed, "model_shares": {"target": 0.9, "fire_policy": 0.86, "mode": 0.9},
                     "tick_stale_share": 0.04, "reasons": [] if passed else ["fire_policy: model share 0.650 < 0.700"]}
    return r


def good():
    """{name: (matrix or None, [(seed, run.json or None)])}: a nightly that passes every gate."""
    noisy_wins = {1, 2, 4, 5}
    return {
        "scripted": ({"name": "scripted", "seeds": [1]}, [(1, run_json("scripted", True, 2))]),
        "mock-clean": (
            {"name": "mock-clean", "seeds": list(range(1, 7)), "min_wins": 3, "gate": True},
            [(s, run_json("mock", s != 3, 3 if s != 3 else 26, policy="scripted", seed=s)) for s in range(1, 7)],
        ),
        "mock-noisy": (
            {"name": "mock-noisy", "seeds": list(range(1, 7)), "min_wins": 2, "gate": True},
            [(s, run_json("mock", s in noisy_wins, 10 if s in noisy_wins else 26, seed=s)) for s in range(1, 7)],
        ),
        "ablate-constant": ({"name": "ablate-constant", "seeds": [1]}, [(1, run_json("constant", False, 0, levels_done=0))]),
        "ablate-random": ({"name": "ablate-random", "seeds": [1]}, [(1, run_json("random", False, 26))]),
        "latency-500ms": ({"name": "latency-500ms", "seeds": [1]}, [(1, run_json("mock", False, 26, policy="scripted"))]),
    }


def write(root, entries, legacy=()):
    for name, (matrix, seeds) in entries.items():
        d = os.path.join(root, "agent-" + name)
        os.makedirs(d)
        if matrix is not None and name not in legacy:
            with open(os.path.join(d, "matrix.json"), "w") as f:
                json.dump(matrix, f)
        for seed, r in seeds:
            sd = d if name in legacy else os.path.join(d, f"seed-{seed}")
            os.makedirs(sd, exist_ok=True)
            if r is None:
                continue
            os.makedirs(os.path.join(sd, "20261002T000000Z-0123abcd"))
            with open(os.path.join(sd, "20261002T000000Z-0123abcd", "run.json"), "w") as f:
                json.dump(r, f)
            with open(os.path.join(sd, "exit-code"), "w") as f:
                f.write("0\n")


class ReportTest(unittest.TestCase):
    def run_report(self, entries, legacy=()):
        with tempfile.TemporaryDirectory() as root:
            write(root, entries, legacy)
            out, err = io.StringIO(), io.StringIO()
            with redirect_stdout(out), redirect_stderr(err):
                code = report.main(["agent_nightly_report.py", root])
            return code, out.getvalue() + err.getvalue()

    def test_good(self):
        code, text = self.run_report(good())
        self.assertEqual(code, 0, text)
        self.assertIn("cross-run gates: ok", text)
        self.assertIn("**mock-noisy**: 4 of 6 seeds to victory (gate: at least 2); demo3 survival 4/96 (4.2%)", text)
        self.assertIn("**mock-clean**: 5 of 6 seeds to victory (gate: at least 3)", text)
        self.assertIn("| mock-noisy s3 | failed", text)
        self.assertIn("mock noisy (0.3/0.1/0.1), 212ms", text)
        self.assertIn("mock clean, 212ms", text)

    def test_too_few_wins(self):
        e = good()
        m, seeds = e["mock-noisy"]
        e["mock-noisy"] = (m, [(s, r if s == 1 else run_json("mock", False, 26, seed=s)) for s, r in seeds])
        code, text = self.run_report(e)
        self.assertEqual(code, 1)
        self.assertIn("mock-noisy: 1 of 6 seeds reached victory.pcx, want at least 2", text)

    def test_not_model_driven(self):
        e = good()
        e["mock-clean"][1][1] = (2, run_json("mock", True, 1, passed=False, policy="scripted", seed=2))
        code, text = self.run_report(e)
        self.assertEqual(code, 1)
        self.assertIn("mock-clean seed 2: not model-driven (fire_policy: model share 0.650 < 0.700)", text)

    def test_missing_seed(self):
        e = good()
        e["mock-noisy"][1][5] = (6, None)  # a seed directory without run.json
        code, text = self.run_report(e)
        self.assertEqual(code, 1)
        self.assertIn("mock-noisy seed 6: no run.json", text)
        e = good()
        m, seeds = e["mock-noisy"]
        e["mock-noisy"] = (m, seeds[:5])  # no seed directory at all
        code, text = self.run_report(e)
        self.assertEqual(code, 1)
        self.assertIn("mock-noisy seed 6: no run.json", text)

    def test_missing_entry(self):
        e = good()
        del e["ablate-constant"]
        code, text = self.run_report(e)
        self.assertEqual(code, 1)
        self.assertIn("ablate-constant: no artifact", text)

    def test_ablation_not_worse(self):
        e = good()
        e["ablate-random"] = (e["ablate-random"][0], [(1, run_json("random", True, 1))])
        code, text = self.run_report(e)
        self.assertEqual(code, 1)
        self.assertIn("ablate-random seed 1: not worse than scripted", text)

    def test_scripted_failed(self):
        e = good()
        e["scripted"] = (e["scripted"][0], [(1, run_json("scripted", False, 26))])
        code, text = self.run_report(e)
        self.assertEqual(code, 1)
        self.assertIn("scripted seed 1: outcome failed", text)

    def test_legacy_layout(self):
        """An artifact of the single-run layout (no matrix.json, no seed directories) reads as one seed."""
        e = good()
        old = run_json("scripted", True, 2)
        del old["config"]
        e["scripted"] = (None, [(1, old)])
        code, text = self.run_report(e, legacy={"scripted", "ablate-constant", "ablate-random"})
        self.assertEqual(code, 0, text)
        self.assertIn("| scripted s1 | completed", text)

    def test_demo3(self):
        self.assertEqual(report.demo3(run_json("mock", True, 2)), (1, 3))
        # -max-deaths 25: the 26th death ends the visit at the cap, with no 27th attempt
        self.assertEqual(report.demo3(run_json("mock", False, 26)), (0, 26))
        # a visit that ended any other way (timeout, stall) counts the attempt under way
        timeout = run_json("mock", False, 3)
        timeout["episodes"][0]["levels"][2]["outcome"] = "timeout"
        self.assertEqual(report.demo3(timeout), (0, 4))
        self.assertEqual(report.demo3(None), (0, 0))
        self.assertTrue(report.victory(run_json("mock", True, 0)))
        self.assertFalse(report.victory({"outcome": "completed", "reason": "1 episodes through demo1"}))


if __name__ == "__main__":
    unittest.main()
