# Decision cache on the 4-level demo

Date: 2026-11-13. Backend: live jev (`jev-1.13.0`). Machine: Intel Core Ultra 5 335, 8 threads, no GPU.

## Verdict

**The cache works and saves little money; whether it changes how well the bot plays cannot be
told from these runs.**

- **Hit rates.** `exact` reused 0.6-3.3% of requests, `approx:coarse` 13.2-39.4%. That saved
  $0.006-$0.050 per `exact` run and $0.10-$0.21 per `approx:coarse` run: $0.55 over the six cached
  runs, against $6.79 spent on all nine.
- **Play varies too much to compare.** Three runs with the same configuration (cache off) needed
  3,958 to 13,057 calls and 505 to 1,611 s of game time. Three cached runs (B1, B3, C2) spent
  3,000+ s with 20-25 deaths; B3 hit the 60-minute level watchdog with 2 of 4 levels. With 3
  seeds per mode this does not show that the cache caused the bad runs, and does not show that
  it did not.
- **Gate.** 8 of 9 runs pass the 70% model-share gate, against 1 of 3 yesterday. The one failure
  (B3, 0.686) has no API errors: the budget ladder capped its fast lane late in a long run (see
  "Why B3 failed the gate").
- **nimble-v3 is still out of reach on this CPU.** The cache removes calls, not seconds per call:
  even at C1's 39% a campaign would still take about 4.6 days (projection, below).

## What was run

9 live campaigns (demo1, demo2, demo3, demo2, victory), seeds 1-3 per mode:
`-session lockstep -skill 1 -max-deaths 25 -level-timeout 60m -sim-latency 212ms -budget-usd 2
-on-exhausted stop`, plus `-cache exact` (B) or `-cache approx:coarse` (C); A has no cache. C3 ran
with `-budget-usd 1.9` (to hold total spend under $8); it spent $0.73 and never reached the
budget ladder, so the lower limit did not act. The run ids are in "Reproduce".

## Campaign results

| Run | Cache | Seed | Outcome | Levels | Deaths | Damage | Stuck | Game time | Gate share | Gate | API errors |
|---|---|---|---|---|---|---|---|---|---|---|---|
| A1 | off | 1 | victory | 4/4 | 4 | 924 | 39 | 1,611 s | 0.742 | pass | 0 |
| A2 | off | 2 | victory | 4/4 | 1 | 585 | 20 | 527 s | 0.836 | pass | 0 |
| A3 | off | 3 | victory | 4/4 | 5 | 358 | 9 | 505 s | 0.897 | pass | 3 (529) |
| B1 | exact | 1 | victory | 4/4 | 25 | 3,445 | 99 | 3,177 s | 0.754 | pass | 0 |
| B2 | exact | 2 | victory | 4/4 | 4 | 643 | 16 | 656 s | 0.806 | pass | 0 |
| B3 | exact | 3 | failed: level watchdog in demo3 | 2/4 | 20 | 3,061 | 92 | 3,760 s | 0.686 | fail | 0 |
| C1 | approx:coarse | 1 | victory | 4/4 | 5 | 721 | 13 | 1,125 s | 0.914 | pass | 4 (timeout) |
| C2 | approx:coarse | 2 | victory | 4/4 | 22 | 3,402 | 117 | 3,300 s | 0.783 | pass | 6 (529) |
| C3 | approx:coarse | 3 | victory | 4/4 | 9 | 1,441 | 49 | 1,762 s | 0.787 | pass | 4 (timeout) |

Gate share is the lowest per-field model share of decided ticks (`gate.model_share`).

| Run | API calls | Hits | Misses | Hit rate | Fast lane | Slow lane | Cost | Saved tokens | Saved $ |
|---|---|---|---|---|---|---|---|---|---|
| A1 | 13,057 | | | | | | $0.664 | | |
| A2 | 5,022 | | | | | | $0.266 | | |
| A3 | 3,958 | | | | | | $0.212 | | |
| B1 | 25,523 | 166 | 25,523 | 0.6% | 0.8% | 0.1% | $1.384 | 178,546 | $0.007 |
| B2 | 5,727 | 131 | 5,727 | 2.2% | 2.3% | 2.0% | $0.306 | 144,441 | $0.006 |
| B3 | 28,203 | 964 | 28,203 | 3.3% | 2.2% | 6.3% | $1.535 | 1,181,332 | $0.050 |
| C1 | 6,734 | 4,383 | 6,734 | 39.4% | 42.2% | 28.9% | $0.353 | 4,925,597 | $0.207 |
| C2 | 24,153 | 3,671 | 24,153 | 13.2% | 14.4% | 9.6% | $1.341 | 4,256,940 | $0.179 |
| C3 | 13,270 | 2,028 | 13,270 | 13.3% | 13.0% | 14.2% | $0.729 | 2,361,995 | $0.099 |

Total jev spend: $6.79 (A $1.14, B $3.23, C $2.42). In every cached run `cache.hits` equals the
decision events marked `cache` in the trace, and `api.calls` = `cache.misses` = the `api_call`
events.

**Where the hits come from.** In every cached run most hits fall on its longest level: B1 demo3
125 of 166, B3 demo3 960 of 964, C2 demo3 3,525 of 3,671, C3 demo3 1,656 of 2,028, and C1's final
demo2 3,954 of 4,383. That level took C1 634 s (A1: 101 s), with a 60% hit rate and reused answers
a median 69 s old (offline: 0.6 s). A hypothesis, not a finding: long stays on a level, often stuck,
repeat situations and so produce hits; reused answers could equally keep the bot in place. These
data cannot separate the two directions, and the correlation is not even consistent: C1 had the
most coarse hits and the fewest deaths, B1 had 25 deaths and the fewest exact hits.

## Offline estimate (yesterday's 3 jev runs)

`q2bot cache-sim` on the three cache-off jev runs of `jev-vs-nimble-v3.md`, 30,698 requests:

| Mode | Hits | Fast lane | Slow lane | All | In-flight duplicates | Saved $ (3 runs) |
|---|---|---|---|---|---|---|
| exact | 179 | 0.6% | 0.6% | 0.6% | 98 | $0.008 |
| approx:fine | 1,755 | 6.5% | 3.3% | 5.7% | 4,291 | $0.084 |
| approx:coarse | 2,811 | 10.3% | 5.5% | 9.2% | 5,625 | $0.136 |

**Fidelity**: how often the reused answer equals the one jev gave to that request (danger: same
level rounded).

| Question | exact | approx:fine | approx:coarse |
|---|---|---|---|
| target | 99.2% (133) | 98.5% (1,509) | 98.3% (2,403) |
| fire_policy | 95.5% (133) | 97.7% (1,509) | 96.4% (2,403) |
| movement | 100.0% (133) | 99.6% (1,509) | 99.3% (2,403) |
| mode | 100.0% (46) | 95.5% (242) | 94.8% (402) |
| weapon | 100.0% (9) | 99.4% (161) | 99.3% (269) |
| pickup | 95.7% (46) | 82.8% (227) | 83.6% (372) |
| danger | 100.0% (46) | 98.8% (242) | 97.3% (402) |
| all questions | 95.0% (179) | 93.6% (1,751) | 92.1% (2,805) |

`exact` reuses the answer to a byte-identical request, so its 95.0% is jev's own noise floor: the
same request gets a different answer 5% of the time. The approximate modes lose 1.4 and 2.9 points
against that floor, mostly on pickup (83-84% against 96%) and mode.

**Live against offline**, all lanes, by seed:

| Seed | exact offline | exact live | coarse offline | coarse live |
|---|---|---|---|---|
| 1 | 0.2% | 0.6% | 7.7% | 39.4% |
| 2 | 1.3% | 2.2% | 13.1% | 13.2% |
| 3 | 0.2% | 3.3% | 6.7% | 13.3% |

Both count in-flight duplicates as misses (C1: 1,424 fast-lane misses in flight; offline seed 1:
2,073), so that does not explain the gap. Two things the data do show: the offline runs of seeds 1
and 3 had 2,800 and 3,179 failed requests (yesterday's breaker fallbacks), which the simulation
counts as requests but stores no answer for, and seed 2, the clean run, is the one where offline
and live coarse agree (13.1% against 13.2%). C1's excess comes from its one long final level.

## Breaker fix in lockstep

Yesterday 2 of 3 jev runs failed the gate (0.63, 0.52) because an open breaker refused calls at
once and the game ran on through hundreds of seconds of scripted fallback. jev now waits out an
open breaker or a 429/529 cooldown in lockstep (`WaitWhenOpen`), so the game clock stands still.

Today: API errors are 0 in five runs, three 529s in A3, six 529s in C2 and four timeouts each in
C1 and C3. The run logs print no breaker or cooldown line (no match for
`breaker|cooldown|circuit` in any of the 9); the traces show the waits. In A3, after three 529s in
a row at 184.3 s of game time, the next call came 10.3 s of wall clock but 0.1 s of game time
later; in C2, after two 529s at 2,181.2 s, 10.3 s of wall clock and 1.0 s of game time. No run had
the five consecutive failures that open the breaker (at most four, C3's timeouts), so the
breaker-wait path itself was not exercised live; the cooldown wait was.

## Why B3 failed the gate

B3 had no API errors. Its share fell when the budget ladder capped the fast lane (5 Hz once half
the budget is spent, 2 Hz at three quarters): $1.00 was spent at 1,920 s of game time and $1.50 at
3,618 s. A fast-lane request over the cap is refused by the budget gate (6,477 decision events
with `err: "budget: fast lane capped at ..."`); the arbiter treats a refused result like an error,
and the ticks fall back to the scripted policy (fallback `error`). Model share of decided ticks
in each phase, from the trace:

| Phase (B3) | target | fire_policy |
|---|---|---|
| 10 Hz, 0-1,920 s | 0.804 | 0.913 |
| 5 Hz, 1,920-3,618 s | 0.531 | 0.471 |
| 2 Hz, 3,618-3,760 s | 0.179 | 0.179 |
| whole run | 0.686 | 0.699 |

B1 and C2 also crossed $1.00 (at 1,938 s and 2,253 s; 3,875 and 3,417 refusals; 5 Hz phase
shares 0.51/0.50 and 0.56/0.59) and passed (0.754, 0.783) because most of their ticks came before
the cap. The low share is thus a consequence of run length through the budget, not of API errors.
Cache hits count as model answers and do not lower it.

## nimble-v3 projection

A projection, not a measurement: jev's call counts × 89 s (nimble-v3's median per decision on this
CPU). Hits are answered at once, but every miss still takes 89 s, against the fast lane's 800 ms.

| Mode | Calls per run (measured) | Projected time per campaign |
|---|---|---|
| off | 3,958-13,057 (mean 7,346) | 4.1-13.4 days (mean 7.6) |
| exact | 5,727-28,203 | 5.9-29.1 days |
| approx:coarse | 6,734-24,153 | 6.9-24.9 days |

The per-mode ranges mostly reflect run length. Applied to the cache-off mean of 7,346 requests,
the live hit rates leave 7,100-7,300 calls with `exact` (7.3-7.5 days) and 4,450-6,380 with
`approx:coarse` (4.6-6.6 days). nimble-v3 has no cost, so it would never meet the budget ladder
that throttled B1, B3 and C2; its call counts would be higher than these.

## Limits

- 3 seeds per mode. Same-configuration runs differ by a factor of 3 in calls and game time, which
  swamps any effect of the cache on play.
- A cache hit changes what the bot does next, so cached and uncached runs of the same seed are
  different games; the per-seed comparison is not paired.
- Hits concentrate on one long level per run, so the hit rate says more about how long the bot
  stayed somewhere than about the cache.
- Fidelity is measured offline only; live, there is no second answer to compare a reused one with.
- The `exact` store reached its 16,384-entry cap in B1 and B3 (8,954 and 11,623 evicted), so their
  exact hit rates are slightly below an unbounded store's.
- In the three budget-capped runs the per-episode log line counts more fast-lane misses than
  `run.json` (B1 22,812 against 18,959, B3 27,371 against 20,950, C2 21,127 against 17,951);
  the gap is close to, but not equal to, the budget refusals. The tables use `run.json`.
- jev's latency is simulated (212 ms); real latency is not measured here.

## Reproduce

```sh
# campaigns (jev needs TYPESAFE_API_KEY in .env); jev-run also applies the 70% gate
make jev-run BUDGET=2                      # cache off
make jev-run BUDGET=2 CACHE=exact
make jev-run BUDGET=2 CACHE=approx:coarse

# one seed, as run here (A: no -cache flag)
server/bin/q2bot run -backend jev -session lockstep -skill 1 -max-deaths 25 -level-timeout 60m \
  -sim-latency 212ms -budget-usd 2 -on-exhausted stop -seed 1 -cache approx:coarse -out runs

# offline estimate on recorded runs (no model is asked)
server/bin/q2bot cache-sim runs/20261112T194920Z-f51e238a runs/20261112T195602Z-9eacd0c1 \
  runs/20261112T200418Z-a1709ee9
```

Runs: A1 `20261113T080856Z-d3da7bfc`, A2 `20261113T085019Z-58058e8b`, A3 `20261113T092038Z-05394158`,
B1 `20261113T082009Z-7cdd2ce9`, B2 `20261113T085407Z-3799eda1`, B3 `20261113T092406Z-b8f2e9c3`,
C1 `20261113T084338Z-d89b0fd0`, C2 `20261113T085836Z-72bab9f5`, C3 `20261113T185400Z-fb9a5a0c`.
Hits are the `decision` events with a `cache` field in `runs/<id>/ep-000/trace.jsonl.gz`; budget
refusals are `decision` events whose `err` starts with `budget:`.
