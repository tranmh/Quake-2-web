# jev vs nimble-v3 on the 4-level demo

Date: 2026-11-12/13. Machine: Intel Core Ultra 5 335, 8 threads, 30 GB RAM, no GPU.

## Verdict

**There is no evidence that nimble-v3 is better than jev, and on this machine it cannot
be used at all.** No winner can be named on level progress, because nimble-v3 never
played a level: one decision takes about 90 s on this CPU, against the bot's 800 ms
budget for a combat decision.

A real answer needs a GPU Ollama host. The `ollama` backend added with this benchmark is
ready for that (`make bot-run BACKEND=ollama MODEL=nimble-v3 OLLAMA_URL=...`, see
`docs/AGENT.md`, "Local models via Ollama").

A side finding: **live jev played worse than the scripted bot** in these runs.

## What was compared

| | jev | nimble-v3 |
|---|---|---|
| Model | `jev-1.13.0`, TypeSafe API | Bespoke-Nimble-9B-v3, 9.0B parameters, Q8_0 |
| Served by | `https://api.typesafe.ai/v1/systemone` | Ollama 0.40.4 in Docker, `POST /v1/systemone` |
| Time per decision | 150-310 ms (probe) | 89 s median, 128 s p95 |
| Cost | about $0.45 per campaign | free |

nimble-v3 is a decision-only model: Ollama refuses `/api/chat` and `/api/generate` for it
and serves it on `/v1/systemone`, the same wire format jev uses. Both models therefore
receive the identical request body (state and questions).

## Method

The plan was 3 full campaigns per bot. At about 90 s per decision a nimble-v3 campaign
(10k-20k decisions) would take 2 to 3.5 weeks, so the nimble-v3 side was replaced by an
offline agreement test:

1. **Campaigns.** 3 scripted and 3 live jev runs of the full campaign
   (demo1, demo2, demo3, demo2, victory), seeds 1-3:
   `-session lockstep -skill 1 -max-deaths 25 -level-timeout 60m`, jev with
   `-sim-latency 212ms -budget-usd 2 -on-exhausted stop`.
2. **Replay.** 300 of jev's recorded decisions (233 fast lane, 67 slow lane), sampled
   uniformly from the 24,719 answered requests of the 3 jev runs, were sent unchanged to
   nimble-v3. All 300 returned HTTP 200; 947 answers were compared.

## Campaign results

All six runs reached victory with 4 of 4 levels and pass `q2bot validate`.
Mean (range) over 3 seeds:

| | scripted | jev (live) |
|---|---|---|
| Deaths per run | 2 (2-2) | 5.7 (4-7) |
| Damage taken | 702 (688-717) | 1023 (794-1307) |
| Game time | 913 s (825-962) | 1186 s (1135-1232) |
| Game kills | 110 (104-117) | 94 (82-102) |
| Model share (gate minimum) | n/a | 0.66 (0.52-0.83) |
| Cost per run | $0 | $0.45 ($0.35-$0.53) |

Total jev spend: $1.34.

Two of the three jev runs fail the 70% model-share gate (seed 1: 0.63, seed 3: 0.52).
A few real timeouts (6 and 4) opened the jev client's circuit breaker for 5 s of wall
clock; in lockstep, refused calls return at once, so the game ran through about 360 s
and 530 s of game time on the scripted fallback. That is a lockstep bug (a wall-clock
cooldown in compressed time), not a jev quality problem, and it is not fixed here. The
one clean jev run (seed 2, share 0.83, no errors) had 4 deaths against scripted's 2.

## How nimble-v3 decides compared with jev

Top-1 agreement on the same 300 situations (danger: same level). 95% CI by cluster
bootstrap over requests.

| Question | n | Agreement | 95% CI |
|---|---|---|---|
| movement | 233 | 96.1% | 93.6-98.3 |
| target | 233 | 94.0% | 90.6-97.0 |
| weapon | 48 | 87.5% | 77.1-95.8 |
| mode | 67 | 82.1% | 71.6-91.0 |
| danger level | 67 | 79.1% | 68.7-88.1 |
| pickup | 66 | 63.6% | 51.5-74.2 |
| fire_policy | 233 | 42.1% | 35.6-48.5 |
| all | 947 | 77.4% | 75.2-79.5 |

**Fire policy is the real difference** (rows: jev, columns: nimble-v3):

| jev \ nimble-v3 | fire_when_aligned | hold | suppress | total |
|---|---|---|---|---|
| fire_when_aligned | 61 | 57 | 78 | 196 |
| hold | | 33 | | 33 |
| suppress | | | 4 | 4 |

Other differences:

- **Pickups.** In 21 of 36 cases where jev chose an item, nimble-v3 chose none.
- **Danger.** nimble-v3 rates danger lower (mean 1.81 against 2.06; Pearson r 0.93).
- **Confidence.** Mean confidence 0.91 against jev's 0.69. Applying the arbiter's
  rejection rule to each answer alone (confidence below 0.35, or top probability below
  0.4), 1.4% of nimble-v3's answers would be rejected against 12.0% of jev's. The two
  confidence scales are not calibrated against each other, so this is not evidence that
  nimble-v3 is more often right.
- **Scripted policy as a third opinion.** Where the two models disagreed (211 answers),
  the scripted policy sided with jev 128 times, with nimble-v3 58 times and with neither
  25 times; most of that is fire policy.

## Limits

- Agreement shows where the models differ, not which one finishes more levels.
- 293 of the 300 sampled decisions are combat situations; non-combat behaviour is
  barely tested.
- Slow-lane questions have 48-67 samples each, so their intervals are wide.
- 3 seeds per bot is thin, and 2 of the 3 jev campaigns are partly scripted play.
- The sample is drawn from situations jev's play led to; nimble-v3 playing itself would
  meet different ones.

## Reproduce

```sh
# Ollama with nimble-v3
docker run -d --name q2-ollama -p 127.0.0.1:11434:11434 -v q2-ollama:/root/.ollama \
  -e OLLAMA_NUM_PARALLEL=1 -e OLLAMA_KEEP_ALIVE=24h ollama/ollama:0.40.4
docker exec q2-ollama ollama pull nimble-v3

# campaigns (jev needs TYPESAFE_API_KEY in .env)
make bot-run BACKEND=scripted
make jev-run BUDGET=2
make bot-run BACKEND=ollama MODEL=nimble-v3   # full campaign: only sensible with a GPU
```

Every request and answer of a run is in `runs/<id>/ep-000/trace.jsonl.gz` (`decision`
events with `state`, `questions`, `response`), which is what the replay re-sent.
