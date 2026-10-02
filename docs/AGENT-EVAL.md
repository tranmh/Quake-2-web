# Agent evaluation: noisy and latent models

This page measures how the agent copes with a model that is sometimes wrong (noisy) and always late (latent).
It covers two rounds of change to the decision layer and the bot that this evaluation drove:

- **Round 1**: the arbiter accumulates evidence across answers instead of trusting each answer alone.
- **Round 2**: the arbiter learns per field how far to trust the model, and the bot recovers from stalls and
  backs away from drain and melee monsters on its own.

Every number comes from a lockstep run you can repeat with the commands in [Reproduce](#reproduce). The design
is in [ADR-0006](adr/0006-ai-agent.md) and the tools are in [AGENT.md](AGENT.md).

## Read this first: what decides a run

A run is decided on demo3. An attempt that gets through kills 35–40 monsters, and almost every attempt that
fails dies before the route's first step (the ambush gunner) is done. The bot enters the level with the health
it left demo2 with, and usually only the blaster has ammo. Every death reloads the level-entry save. The useful
number is therefore **demo3 survival**: demo3 exits ÷ demo3 attempts, where attempts = deaths + 1 per visit.

Same protocol for every row (lockstep, skill 1, `-max-deaths 25 -level-timeout 60m`):

| Policy | Build | Seeds | Runs won | demo3 survival |
|---|---|---|---|---|
| scripted, 0 ms | final | 1–9 | 9/9 | 9/43 (21 %) |
| scripted, 212 ms | final | 1–9 | 9/9 | 9/89 (10 %) |
| clean mock, 212 ms | final | 1–9 | 7/9 | 7/109 (6.4 %) |
| clean mock, 212 ms | round 1 | 1–9 | 4/9 | 4/179 (2.2 %) |
| clean mock, 212 ms | latest-answer control | 1, 3–9 | 6/8 | 6/73 (8.2 %) |
| noisy mock, 212 ms | final | 1–11 | 7/11 | 7/187 (3.7 %) |
| noisy mock, 212 ms | round 1 | 1–5 | 3/5 | 3/88 (3.4 %) |
| noisy mock, 212 ms | before | 1–5 | 1/5 | 1/112 (0.9 %) |

Three things follow:

- **Latency costs the most.**
  - The scripted policy survives demo3 21 % of the time at 0 ms and 10 % at 212 ms. On its own that gap is
    not significant (two-sided Fisher p ≈ 0.1).
  - Against the 212 ms mock models it is: p ≈ 0.02 for clean and p < 0.001 for noisy.
  - A diagnostic build that decides the fast lane's fields fresh takes a 212 ms clean model from 5 % to
    about 20 % (p ≈ 0.02 pooled; see [Where latency hurts](#where-latency-hurts)).
- **The policy matters too, less clearly.** At 212 ms the clean mock survives 6.4 % and the noisy mock 3.7 %,
  against the scripted policy's 10 %. That is p ≈ 0.4 and p ≈ 0.05.
- **Fixed-seed victory gates are close to chance at these rates.**
  - A run allows up to 26 attempts on demo3, so it wins with a probability of about 1 − (1 − p)^26: 0.62 at
    p = 3.7 % and 0.82 at p = 6.4 %.
  - Gate (a) asks for 4 wins in 5 seeds and gate (b) for 3 in 3. At those odds they pass about 40 % and
    55 % of the time.
  - Whether a gate passes on its seeds says little about a change. The per-attempt counts above say more.

## Gate verdicts (final build)

| Gate | Requirement | Result | Verdict |
|---|---|---|---|
| (a) noisy mock, seeds 1–5 | ≥ 4 of 5 reach `victory.pcx`, every completed run model-driven | 4 of 5 (seed 3 fails, 26 deaths on demo3). All 5 runs are model-driven: target ≥ 0.87, fire_policy ≥ 0.85, mode ≥ 0.88; ticks on stale answers 3.7–5.0 %. Seed 1 wins only because the new stall recovery reloaded demo2 after 250 s without progress. | **met** |
| (b) clean mock, seeds 1–3 | all win, model-driven | 2 of 3 (seed 3 hits the 60 min level watchdog after 21 deaths on demo3). All 3 are model-driven (≥ 0.89 / 0.87 / 0.90). | **not met** |
| (c) scripted | `TestCampaignScripted` (seed 1); seeds 1–3 win via `q2bot` | The test passes: 1035 s of game time, 2 deaths. Seeds 1–3 win with 2, 2 and 0 deaths. | met |
| (d) ablations | constant and random fail or are measurably worse | constant fails on demo1 (26 deaths), random on demo3 (47 deaths). Scripted wins seeds 1–9. | met |
| (e) latency sweep, clean mock, seed 1 | report | 100, 212 and 400 ms win; 500 ms fails on demo3. | reported |
| (f) determinism | identical `run.json` (wall-clock fields excluded) and traces | Two noisy seed-1 runs: `run.json` equal, traces equal (48,398 events). `q2bot replay -strict` finds no divergence. | met |
| (g) tests | fairness differential test and every existing test | Green (see [Tests](#g-tests)). | met |

## Method

- **Runs.**
  - Every run plays the whole campaign (demo1 → demo2 → demo3 → demo2 → `victory.pcx`) in a lockstep
    session at skill 1.
  - Limits are `-max-deaths 25` per level and `-level-timeout 60m`. Demos are off (`-record=false`).
    One episode per run.
  - Every row on this page uses that protocol.
- **Backends.**
  - `scripted` is the rule policy. It answers at once unless `-sim-latency` is given.
  - `mock` is the real jev HTTP client (`backend/jev`) talking to the in-process fake Jev server
    (`backend/jevtest`) over loopback. Its answers come from the lane state JSON the client sends, never
    from the bot's belief.
    - `-mock-policy scripted` (**clean**) runs the scripted rules on the received state. The state carries no
      clock, so the strafe rhythm's clock is drawn from the request digest and the strafe side varies by
      request.
    - `-mock-policy noisy` (**noisy**, the default) perturbs those answers:
      - up to 30 % of the probability mass is spread at random (`-mock-noise 0.3`);
      - the top two options swap with probability 0.1 (`-mock-swap 0.1`), and a swapped answer keeps a normal
        confidence;
      - the confidence is replaced by one in [0.05, 0.3] with probability 0.1 (`-mock-lowconf 0.1`);
      - danger scores shift by N(0, 0.4) levels.
    - The noise is seeded by the run seed and the request's content, so the same request gets the same
      answer.
    - Both mock policies also inject 2 % HTTP 500s and 2 % answers missing a question.
    - The trace's `run_start` config now records the policy and the noise in effect (`mock.policy`, for
      example `noisy noise=0.3 swap=0.1 lowconf=0.1`), next to `mock.faults`. `run.json` carries neither.
  - The simulated latency is 212 ms unless stated. In lockstep an answer is due 212 ms of game time after
    its snapshot, and the simulation waits for it.
- **Tokens and cost.**
  - The fake server reports `input_tokens` = request body bytes ÷ 4. That is an estimate, not Jev's
    tokenizer.
  - Cost = input tokens × $0.042 per million, the documented Jev price. Output is free.
- **Provenance gate** (`run.json` → `gate`):
  - At least 70 % of the *decided ticks* of target, fire_policy and mode act on a model value. A tick is
    decided unless the field fell to its default. A reflex override counts against the model.
  - At most 15 % of a gate field's ticks act on a stale answer.
  - At most 15 % of API answers arrive stale.
- **Builds.**
  - *Before*: the tree at `77f4843`, plus the `-mock-*` flags.
  - *Round 1*: the arbiter with evidence accumulation (snapshot `1086a1d`).
  - *Final*: this tree.
  - *Controls*: scratch builds of the round-1 tree, plus the confirmation code, with the arbiter's parameters
    set from the environment. These are diagnostic builds, not in the tree. *Latest-answer* sets every τ
    negative, which reproduces the old arbiter: its clean seeds 1 and 3 match the before runs exactly
    (1238.4 s with 5 deaths, 927.0 s with 4). *Accumulation* disables confirmation, which is round 1. The
    *field-local* diagnostics and the keep-off A/B are described where they are used.

## What changed

### Round 1: evidence accumulation (`agent/decide/arbiter.go`)

- Before round 1, the arbiter trusted each answer alone:
  - a confidence under 0.35 forced the scripted fallback;
  - a confident wrong answer (a swap) was acted on;
  - hysteresis then held the wrong value (fire_policy 0.4 s, mode 1.5 s);
  - a value held past its answer's TTL counted as stale.
- **Weights.** Each field keeps the model's accepted answers as evidence. Each answer weighs its confidence
  (a noul's |2p−1|) × exp(−Δt/τ). τ is 300 ms for target, fire_policy and movement, 1 s for mode, 500 ms
  for danger and 1.5 s for weapon and pickup.
- **Posterior.**
  - It is the weighted mixture over the options of the field's latest answered question.
  - Options gone from the state (a dead target, a taken item) are dropped.
  - A weapon `keep` is stored as the weapon in hand.
  - Danger is the weighted mean.
- **Decision.** The posterior decides while the newest answer is within its TTL and the evidence is strong:
  weight ≥ 0.35 and top option ≥ 0.4. Otherwise the scripted fallback decides (reason `weak`, `ttl`,
  `error`, `timeout`).
- **Hysteresis and provenance.** Hysteresis applies to the posterior. A held value stays `model` while the
  fresh posterior backs it with ≥ 0.4.
- **Safety asymmetry.** Danger rises at once and falls with the evidence. At danger ≥ 3.5 the newest answer's
  `retreat` is acted on at once.
- **Scripted backend.** It takes the latest answer alone, which reproduces the old arbiter exactly.

Round 1 made the noisy model pass the provenance gate (fire_policy 0.68–0.69 → 0.85–0.87 of ticks; stale
13–16 % → about 4 %). It also made the clean model slower and worse. The latest-answer control shows it:
clean demo3 survival fell from 6/73 (8.2 %) to 4/179 (2.2 %) on the same seeds (two-sided Fisher p ≈ 0.04).
A clean model's answers are never wrong, so smoothing them only adds lag: 2–3 answers per change.

### Round 2: trust the model as far as it has earned

**Reliability-adaptive arbitration** (`decide/arbiter.go`, `ArbiterConfig.TrustBelow`, `Confirm`).

- *The estimate.* Per choice field, the arbiter counts how often the model's confident answers *blip*. A blip
  is an answer whose top option differs from the one before it while the answer after it goes back: a lone
  swap.
  - The rate is decayed over the last ~256 answers, starts from a prior of 0.05, and outlives a level
    change.
  - Low-confidence answers do not count.
- *Measured rates* in round-1 traces (clean seed 1, noisy seed 3), blips per confident answer:

  | | target | fire_policy | mode | weapon | pickup | movement |
  |---|---|---|---|---|---|---|
  | clean | 0.9 % | 1.1 % | 2.6 % | 0.1 % | 1.5 % | 7.8 % |
  | noisy | 7.4 % | 8.1 % | 9.5 % | 7.8 % | 7.2 % | 10.6 % |

  The clean movement rate is high because the clean mock's strafe side varies by request.
- *Trusted* (blip rate < 0.04). One confident answer (confidence ≥ 0.35, top ≥ 0.6) that names a new top
  option is a change point: the older evidence is dropped and the field follows the newest answer. The field
  behaves like the old arbiter, but it keeps its evidence through a missing or failed answer.
  - Replaying the estimate over final traces, the clean model is trusted on 77–100 % of answers for target,
    fire_policy, mode, weapon and pickup, and on 10–11 % for movement.
  - The noisy model is trusted on 0–1 %.
- *Untrusted.* A change needs `Confirm` (2) agreeing confident answers. This is the reviewer's confirmation
  rule: a lone swap never flips a field, and a sustained change flips after exactly 2 answers at any answer
  rate. Without confirmation, at 20 Hz, 30 earlier answers outweighed 2 new ones.
- *Unit tests* (`decide/evidence_test.go`):
  - `TestEvidenceTrust`: a consistent model is trusted after 5 answers; a model with one swap in six is not;
    low-confidence answers do not count; the estimate survives `Reset`.
  - `TestEvidenceConfirmation`.
  - `TestEvidenceSustainedChangeFlips`: exactly 2 answers.
  - The round-1 tests now run on an untrusted arbiter.

**Stall and off-graph recovery** (`bot.stallTick`, `bot.wedgeTick`).

- *Stalls.* The campaign's no-progress watchdog ends a run after 5 minutes without route progress or a kill.
  That ended about 1 run in 15. For example, a demo2 spot ends with "no path to touch #439", and the demo2b
  ledge at (50, −1568…−1820, 92) puts the bot off the nav graph.
- *What the bot does now.* It types `kill`, which reloads the level-entry save:
  - after 250 s without progress as the campaign counts it (reflex `stalled_kill`);
  - or after 60 s off the nav graph with a goal, without moving (`wedged_kill`).
- *Effect.* The attempt was lost either way; a fresh one usually gets past. Noisy seed 1 of gate (a) is such
  a run: stalled on demo2 at 481 s, reloaded, won.
- *Tests.* `TestStallKill`; `TestWedgeKill` adds the off-graph cases.

**Keep-off and low-health reflexes** (`bot/fight.go`). These are movement only; movement is not a gate field.

- *keep_off.* In a fight, a drain monster (parasite: reach 256) in view within 304 units, or a melee-only
  monster (berserker) within 160, is backed away from at once, whatever the intent's movement. The policy's
  own keep-off rule arrives a latency late, and a charging monster covers 50–100 units meanwhile.
- *low_health.* Under 25 health, an advance on an attacker in view within 600 units becomes a sidestep.
- *Provenance.* Both count as movement reflexes.
- *Tests.* `TestKeepOff`.

**Tried and dropped.** A 600 ms strafe hold (the scripted rhythm's shortest segment) in place of 400 ms:

- clean seeds 1–4: 3/28, against 3/60 without;
- noisy seeds 1–5: 3/75, against 4/73;
- scripted at 0 ms, seeds 1–3: 3/20, against 3/7, because it delays the policy's dodge-side changes.

Reverted.

## Results

Each row is one run. Columns:

- **deaths**: per level (demo1 / demo2 / demo3 / demo2 second visit);
- **demo3**: exits / attempts;
- **game**: seconds;
- **model t/f/m**: the model share of decided ticks for target / fire_policy / mode;
- **stale**: the largest share of a gate field's ticks on a stale answer;
- **gate**: model-driven;
- **tokens** and **cost**: input tokens at $0.042 per million.

### (a) Noisy mock, 212 ms, seeds 1–5

| run | outcome | deaths | demo3 | game | model t/f/m | stale | gate | calls | tokens | cost |
|---|---|---|---|---|---|---|---|---|---|---|
| before s1 | failed: 26 deaths on demo3 | 27 (1 / 0 / 26) | 0/27 | 3852 | 0.80 / 0.69 / 0.78 | 14.1 % | no | 34444 | 19.88 M | $0.83 |
| before s2 | failed: 26 deaths on demo3 | 30 (1 / 3 / 26) | 0/27 | 2900 | 0.77 / 0.68 / 0.80 | 15.7 % | no | 27580 | 15.81 M | $0.66 |
| before s3 | failed: 26 deaths on demo3 | 27 (0 / 1 / 26) | 0/27 | 3313 | 0.79 / 0.69 / 0.79 | 14.8 % | no | 30956 | 17.76 M | $0.75 |
| before s4 | victory | 6 (0 / 1 / 3 / 2) | 1/4 | 1412 | 0.78 / 0.69 / 0.77 | 13.0 % | no | 12171 | 6.81 M | $0.29 |
| before s5 | failed: 26 deaths on demo3 | 27 (0 / 1 / 26) | 0/27 | 2726 | 0.79 / 0.69 / 0.79 | 15.7 % | no | 25401 | 14.49 M | $0.61 |
| round 1 s1 | failed: 26 deaths on demo3 | 26 (0 / 0 / 26) | 0/27 | 3726 | 0.89 / 0.87 / 0.91 | 4.2 % | yes | 31807 | 17.74 M | $0.75 |
| round 1 s2 | victory | 16 (0 / 0 / 16 / 0) | 1/17 | 3212 | 0.89 / 0.85 / 0.89 | 4.2 % | yes | 27750 | 15.37 M | $0.65 |
| round 1 s3 | victory | 2 (0 / 0 / 2 / 0) | 1/3 | 890 | 0.87 / 0.85 / 0.91 | 4.8 % | yes | 7437 | 4.17 M | $0.18 |
| round 1 s4 | victory | 13 (0 / 0 / 13 / 0) | 1/14 | 2512 | 0.89 / 0.86 / 0.91 | 4.1 % | yes | 21308 | 11.95 M | $0.50 |
| round 1 s5 | failed: 26 deaths on demo3 | 26 (0 / 0 / 26) | 0/27 | 3756 | 0.88 / 0.86 / 0.90 | 4.3 % | yes | 32173 | 18.24 M | $0.77 |
| **final s1** | victory | 3 (0 / 1 / 2 / 0) | 1/3 | 1410 | 0.89 / 0.86 / 0.88 | 3.7 % | yes | 10086 | 5.56 M | $0.23 |
| **final s2** | victory | 15 (0 / 0 / 15 / 0) | 1/16 | 2547 | 0.89 / 0.86 / 0.90 | 3.9 % | yes | 21900 | 12.41 M | $0.52 |
| **final s3** | failed: 26 deaths on demo3 | 26 (0 / 0 / 26) | 0/27 | 3453 | 0.89 / 0.87 / 0.92 | 4.2 % | yes | 31321 | 17.55 M | $0.74 |
| **final s4** | victory | 4 (0 / 0 / 4 / 0) | 1/5 | 1265 | 0.87 / 0.85 / 0.91 | 5.0 % | yes | 10623 | 5.97 M | $0.25 |
| **final s5** | victory | 22 (0 / 0 / 21 / 1) | 1/22 | 3516 | 0.87 / 0.85 / 0.92 | 4.0 % | yes | 32266 | 17.95 M | $0.75 |

**Summary.**

- Victories went from 1/5 (before) to 3/5 (round 1) to 4/5 (final). demo3 survival on these seeds went from
  1/112 to 3/88 to 4/73.
- Over seeds 1–11 the final build's noisy survival is 7/187 (3.7 %), the same as round 1's within noise. The
  extra victory is the stall recovery on seed 1, plus chance.
- The final build is model-driven on every seed.
- Part of round 1's stale-share drop is relabelling, not behaviour:
  - Before, a value held by hysteresis past its answer's TTL was always stale. Now it stays `model` while the
    fresh posterior backs it with ≥ 0.4.
  - Before, low-confidence answers (10 %) went to the scripted fallback; now they are accumulated.
- Seeds 6–11 (final): victories on 6, 8 and 11; failures on 7 and 9 (26 deaths on demo3) and 10 (level
  watchdog).

### (b) Clean mock, 212 ms, seeds 1–3

| run | outcome | deaths | demo3 | game | model t/f/m | stale | gate | calls | tokens | cost |
|---|---|---|---|---|---|---|---|---|---|---|
| before s1 | victory | 5 (0 / 3 / 2 / 0) | 1/3 | 1238 | 0.89 / 0.89 / 0.93 | 3.4 % | yes | 9639 | 5.27 M | $0.22 |
| before s2 | victory | 0 (0 / 0 / 0 / 0) | 1/1 | 974 | 0.70 / 0.69 / 0.91 | 2.6 % | no | 5485 | 3.00 M | $0.13 |
| before s3 | victory | 4 (0 / 0 / 1 / 3) | 1/2 | 927 | 0.87 / 0.86 / 0.92 | 3.2 % | yes | 7275 | 3.89 M | $0.16 |
| round 1 s1 | victory | 8 (1 / 0 / 7 / 0) | 1/8 | 2394 | 0.87 / 0.87 / 0.93 | 1.5 % | yes | 18212 | 9.92 M | $0.42 |
| round 1 s2 | victory | 22 (0 / 1 / 20 / 1) | 1/21 | 4010 | 0.90 / 0.90 / 0.93 | 1.3 % | yes | 33825 | 18.77 M | $0.79 |
| round 1 s3 | failed: level watchdog (60 min) | 22 (0 / 1 / 21) | 0/22 | 3845 | 0.89 / 0.89 / 0.91 | 1.6 % | yes | 29898 | 16.55 M | $0.70 |
| **final s1** | victory | 2 (0 / 0 / 2 / 0) | 1/3 | 1426 | 0.90 / 0.90 / 0.90 | 2.8 % | yes | 11114 | 6.02 M | $0.25 |
| **final s2** | victory | 21 (0 / 0 / 21 / 0) | 1/22 | 3793 | 0.89 / 0.87 / 0.94 | 3.6 % | yes | 31921 | 17.82 M | $0.75 |
| **final s3** | failed: level watchdog (60 min) | 25 (0 / 4 / 21) | 0/22 | 4341 | 0.90 / 0.89 / 0.94 | 3.5 % | yes | 35609 | 19.58 M | $0.82 |

- The gate fails on seed 3 in round 1 and in the final build alike.
- Over the larger sample, seeds 1–9, the final build recovers most of round 1's loss:

  | | demo3 survival | victories |
  |---|---|---|
  | final | 7/109 (6.4 %) | 7/9 |
  | round 1 | 4/179 (2.2 %) | 4/9 |
  | latest-answer control | 6/73 (8.2 %) | 6/8 |

  The final build's trust rule makes a clean model decide from its newest answer again. It is level with the
  control (p ≈ 0.8). Its gain over round 1 is suggestive but, on its own, not significant (p ≈ 0.1).
- A build with trust but without the keep-off reflex and the stall recovery won all of seeds 1–3, with 8,
  11 and 15 deaths (3/31 on demo3). Over seeds 1–9 the keep-off reflex is neutral for the clean model (see
  [Keep-off A/B](#keep-off-ab)). Seed 3 is another example of how far one seed's outcome swings with a
  small change.

### (c) Scripted, 0 ms, seeds 1–3

| run | outcome | deaths | demo3 | game |
|---|---|---|---|---|
| before s1 | victory | 4 (0 / 0 / 3 / 1) | 1/4 | 1043 |
| before s2 | victory | 1 (0 / 0 / 0 / 1) | 1/1 | 584 |
| before s3 | victory | 4 (0 / 0 / 4 / 0) | 1/5 | 1313 |
| round 1 s1–s3 | identical to before | | | |
| **final s1** | victory | 2 (0 / 0 / 2 / 0) | 1/3 | 942 |
| **final s2** | victory | 2 (0 / 0 / 2 / 0) | 1/3 | 836 |
| **final s3** | victory | 0 (0 / 0 / 0 / 0) | 1/1 | 595 |

- The scripted backend takes the latest-answer path in every build. Its final runs differ from before only
  through the bot's keep-off reflex and the recoveries.
- `TestCampaignScripted` (seed 1, the campaign package's own lockstep loop) passes: victory in 1035 s of game
  time with 2 deaths, all on demo3 (round 1: 826 s, 3 deaths).
- Seeds 4–9 win too, with 3, 7, 6, 7, 0 and 10 deaths.

### (d) Ablations, seed 1

| run | outcome | deaths | game | model t/f/m |
|---|---|---|---|---|
| before constant | failed: 26 deaths on demo1 | 26 (26) | 816 | 1.00 / 1.00 / 0.86 |
| before random | failed: 26 deaths on demo3 | 26 (0 / 0 / 26) | 2587 | 0.30 / 0.98 / 0.48 |
| round 1 constant | failed: 26 deaths on demo1 | 26 (26) | 915 | 1.00 / 1.00 / 0.87 |
| round 1 random | failed: 26 deaths on demo3 | 36 (10 / 0 / 26) | 2483 | 0.69 / 0.92 / 0.51 |
| **final constant** | failed: 26 deaths on demo1 | 26 (26) | 816 | 1.00 / 1.00 / 0.86 |
| **final random** | failed: 26 deaths on demo3 | 47 (7 / 14 / 26) | 2225 | 0.71 / 0.93 / 0.57 |

- Both ablations fail, while the scripted policy wins seeds 1–9 and the noisy mock 7 of 11 seeds.
- The random ablation answers one-hot on a random pick since round 1. The old uniform probabilities are no
  evidence to an accumulating arbiter, which would have turned the ablation into the scripted fallback.

### (e) Latency sweep, clean mock, seed 1

| latency | before | round 1 | final |
|---|---|---|---|
| 100 ms | victory, 1 death, 883 s, stale 0.7 % | victory, 4 deaths, 1253 s, stale 0.4 % | victory, 6 deaths, 1546 s, stale 0.9 % |
| 212 ms | victory, 5 deaths, 1238 s, stale 3.4 % | victory, 8 deaths, 2394 s, stale 1.5 % | victory, 2 deaths, 1426 s, stale 2.8 % |
| 400 ms | victory, 1 death, 604 s, stale 2.1 % | failed: 26 deaths on demo3, 3195 s, stale 1.1 % | victory, 0 deaths, 712 s, stale 1.8 % |
| 500 ms | failed: 26 deaths on demo3, 3803 s, stale 2.3 % | failed: 26 deaths on demo3, 3529 s, stale 1.1 % | failed: 26 deaths on demo3, 3713 s, stale 2.2 % |

- Every run of the sweep is model-driven (model shares 0.87–0.97).
- One seed per latency is a single draw of a lottery. The latency effect is better measured by the scripted
  rows in [Read this first](#read-this-first-what-decides-a-run): 21 % at 0 ms against 10 % at 212 ms
  (p ≈ 0.1).

### (f) Determinism

- **Repeat runs.** Two runs of the final build (`-backend mock`, noisy, seed 1) give identical `run.json`
  once the run id, `started` and `wall_ms` are excluded. Their traces are identical event by event with the
  wall clock and run id removed: 48,398 events.
- **Replay.** `q2bot replay -trace <final noisy seed 4>/ep-000/trace.jsonl.gz -strict` finds no divergence:
  - 46,516 events replayed equal;
  - 49,840 usercmds fed and recomputed equal;
  - 10,623 responses matched, plus 3 requests still in flight when the episode ended;
  - the summary equal.
- **Earlier root cause.** Determinism broke once under machine load, in round 1. One 800 ms fast-lane attempt
  timed out on loopback. Lockstep mock runs now lift that cap (`jev.Config.UncapFast`, 30 s attempts).

### (g) Tests

- `gofmt -l` is clean and `go vet ./...` passes.
- `go test -count=1 ./...` passes over all 63 server packages, including:
  - the fairness differential test (`internal/agent/fairness`);
  - `TestCampaignScripted`;
  - the oracle and golden tests.
- `go test -race -count=1 ./internal/agent/... ./cmd/q2bot/...` passes.
- One race pass on a machine also playing four campaigns failed `jev.TestConcurrent`: wall-clock attempts
  over its 800 ms cap, in a test this change does not touch. It passed three times on a re-run under the same
  load, and in the full race pass (25 packages).
- No ported package changed.

## Controls and diagnosis

### Accumulation against the latest answer, clean mock

Seeds 1–9, gate protocol. Round 1's arbiter, against the same tree deciding from the latest answer (control):

| seed | latest answer | accumulation (round 1) |
|---|---|---|
| 1 | victory, 5 deaths (demo3 1/3) | victory, 8 (1/8) |
| 2 | (before: victory, 0 deaths, 1/1) | victory, 22 (1/21) |
| 3 | victory, 4 (1/2) | failed, watchdog (0/22) |
| 4 | victory, 3 (1/4) | victory, 17 (1/17) |
| 5 | victory, 12 (1/12) | failed, watchdog (0/22) |
| 6 | victory, 14 (1/15) | failed, watchdog (0/24) |
| 7 | victory, 14 (1/13) | failed, watchdog (0/24) |
| 8 | failed: stalled on demo2 (no demo3 attempt) | failed, watchdog (0/23) |
| 9 | failed, watchdog (0/24) | victory, 17 (1/18) |
| total (1, 3–9) | 6/73 (8.2 %) | 4/179 (2.2 %), seeds 1–9 |

The control and round 1 differ in the arbiter alone. A confirmation rule on top of accumulation, without
trust, did not help either: seeds 1–3 gave 1/70 and one victory.

### Where latency hurts

To find which decisions suffer from latency, a diagnostic build lets the local scripted fallback decide
chosen fields. The fallback answers each request as it is built, so it is at most one request interval old.
Clean mock, seeds 1–4, final tree otherwise:

| fields decided locally | victories | demo3 survival | p against none |
|---|---|---|---|
| none (final build) | 3/4 | 3/60 (5 %) | |
| slow lane (mode, weapon, pickup) | 4/4 | 4/42 (10 %) | 0.44 |
| fast lane (target, fire_policy, movement) | 4/4 | 4/19 (21 %) | 0.05 |
| movement | 4/4 | 4/20 (20 %) | 0.06 |
| target and fire_policy | 4/4 | 4/22 (18 %) | 0.08 |
| only the strafe side, when both pick a strafe | 4/4 | 4/31 (13 %) | 0.22 |
| (scripted backend, 0 ms) | 4/4 | 4/10 (40 %) | 0.006 |

- The fast lane's latency costs the most. Either movement or target and fire_policy, decided fresh, recovers
  most of it. Each row alone is marginal; together the three fast-lane rows give 12/61 against 3/60
  (p ≈ 0.02).
- About half of the movement effect is the strafe side alone. The clean mock's strafe side varies by request,
  because the lane state has no clock, while the local fallback keeps the scripted 0.6–1.2 s rhythm.
- The bot's own reflexes are what can act within a latency. The keep-off reflex is one step in that
  direction. A coherent strafe rhythm in the controller is another, but the 600 ms strafe hold tried for it
  hurt the scripted policy's dodges and was dropped.

### Keep-off A/B

Keep-off and low-health reflexes on (final) against off, same seeds, same arbiter (trust, Confirm 2, stall
recovery):

| | on | off |
|---|---|---|
| noisy, seeds 6–11 | 3/114, 3 victories | 5/87, 5 victories |
| noisy, seeds 12–19 | 6/90, 6 victories | 2/159, 2 victories |
| clean, seeds 4–9 | 5/62, 5 victories | 4/79, 4 victories |
| clean, seeds 1–3 | 2/47, 2 victories | 3/31, 3 victories (also without the stall recovery) |
| total | 16/313 (5.1 %), 16/23 | 14/356 (3.9 %), 14/23 |

The difference is within noise (p ≈ 0.6). The halves of the noisy sample point in opposite directions: seeds
12–19 alone give p ≈ 0.03 for the reflex, seeds 6–11 lean the other way. The reflex is
kept because it acts on the deaths that dominate demo3 (below) and costs nothing in provenance: movement is
not a gate field.

### Confirmation for an untrusted model

Noisy mock, Confirm 2 against no confirmation (round 1's accumulation):

| | Confirm 2 | no confirmation |
|---|---|---|
| seeds 6–11 (trust, stall recovery) | 5/87 | 3/103 |
| seeds 1–5 | 2/96 (without trust or stall recovery) | 4/60 (with trust) |
| total | 7/183 (3.8 %) | 7/163 (4.3 %) |

No measurable difference. Confirm 2 is kept for its bounded reaction lag: a change takes exactly 2 answers at
any answer rate.

## What kills the bot on demo3

Round 1 said two thirds of demo3's damage came from attackers out of view. The traces do not support that.

- **Unattributed damage.** 67 % of the damage is unattributed. The sample is the five final runs that failed
  on demo3 (noisy seeds 3, 7, 9; clean seeds 3, 7): 18,839 points.
  - 57 % of all damage carries no bearing at all. The parasite's drain does no knockback, so it shows no
    direction.
  - 44 % of all damage is unattributed while the bot fights with one or more enemies in view. The world model
    attributes a hit without a bearing only when exactly one monster is attacking.
- **The drain.** Two- and five-point hits without a bearing, the drain's damage, make 36 % of all demo3 damage.
- **At death.** Over the 120 deaths of those runs, the monsters in view within 400 units were parasites (46),
  berserkers (39), gunners (37), soldiers (25) and infantry (23).

## Cost

These are fake-server estimates (bytes ÷ 4), at $0.042 per million input tokens:

| | mean input tokens | mean cost per run | tokens per request | cost per game hour |
|---|---|---|---|---|
| noisy mock, before (seeds 1–5) | 14.95 M | $0.63 | 573 | $0.80 |
| noisy mock, round 1 (seeds 1–5) | 13.49 M | $0.57 | 560 | $0.72 |
| noisy mock, final (seeds 1–5) | 11.89 M | $0.50 | 560 | $0.74 |
| noisy mock, final (seeds 1–11) | 13.38 M | $0.56 | 562 | $0.74 |
| clean mock, before (seeds 1–3) | 4.05 M | $0.17 | 543 | $0.59 |
| clean mock, round 1 (seeds 1–3) | 15.08 M | $0.63 | 552 | $0.67 |
| clean mock, final (seeds 1–9) | 11.67 M | $0.49 | 556 | $0.69 |

- **Cost is game time.** The rate is about $0.7 per game hour at 11.7–13.4 requests per second in combat, so
  a run costs what its game time costs. A quick victory (noisy seed 4, 1265 s) costs $0.25. A run that dies 26
  times on demo3 costs about $0.75.
- **Arbitration is free.** It adds no requests.
- **Real Jev may differ.** Its tokenizer and question overhead will change the per-request figure; the plan
  estimated 1–1.5k tokens. `q2bot jev-probe` measures the real number.

## Interpretation

- **The provenance gate holds for both mock models.** Every noisy and clean run since round 1 is
  model-driven. In the final build, target, fire_policy and mode come from the model on 0.82–0.95 of decided
  ticks, with at most 5.0 % of ticks on stale answers. Round 1 brought the noisy model there: before it,
  fire_policy came from the model on only 68–69 % of ticks and 13–16 % of ticks were stale.
- **No single arbiter setting is right for both models; the arbiter now measures which one it has.**
  - Accumulation protects against a noisy model's swaps. It costs a clean model 2–3 answers of lag per
    change, and that cut its demo3 survival from 8.2 % to 2.2 %.
  - The blip-rate estimate separates the two models' fields cleanly: 0.1–2.6 % against 7–10 %. Movement is
    the exception, because the clean mock's strafe side is random.
  - With it, the clean model is back at 6.4 %, level with the latest-answer control. The noisy model is
    unchanged at 3.7 %.
- **Latency is the bot's weak point, not the arbiter.**
  - The scripted policy itself goes from 21 % to 10 % at 212 ms.
  - Fresh fast-lane decisions bring a 212 ms clean model from 5 % to about 20 %.
  - Further gains have to come from the bot's own reflexes acting inside the latency: a coherent strafe
    rhythm, keeping off drain and melee monsters, shooting back at an attacker.
- **The gates are lotteries at the current survival rates.** Gate (a) passed and gate (b) failed on its
  seed 3. Both results are within what chance gives a fixed bot on 3–5 seeds. Seed sweeps of any size should
  report per-attempt survival with its counts, not victories on a few seeds.

## Reproduce

All commands run from `server/` with the demo pak in place (`make demo`). Use a scratch `-out` directory.
`COMMON` is shared by every command:

```sh
COMMON="-session lockstep -skill 1 -max-deaths 25 -level-timeout 60m -record=false -out /tmp/agent-eval"
```

| Number | Command |
|---|---|
| (a) noisy mock, seed N | `go run ./cmd/q2bot run -backend mock -seed N $COMMON` (the default `-mock-policy noisy`; `-mock-noise 0.3 -mock-swap 0.1 -mock-lowconf 0.1` are the defaults) |
| (b) clean mock, seed N | `go run ./cmd/q2bot run -backend mock -mock-policy scripted -seed N $COMMON` |
| (c) scripted, seed N | `go run ./cmd/q2bot run -backend scripted -seed N $COMMON` (212 ms: add `-sim-latency 212ms`) |
| (c) the scripted gate test | `go test -count=1 -run 'TestCampaignScripted$' -v ./internal/agent/campaign` |
| (d) ablations | `go run ./cmd/q2bot run -backend constant -seed 1 $COMMON`, then the same with `-backend random` |
| (e) latency sweep | `go run ./cmd/q2bot run -backend mock -mock-policy scripted -seed 1 -sim-latency 100ms $COMMON`, then 212ms, 400ms and 500ms |
| (f) determinism | run (a) with seed 1 twice. Compare the two `run.json` files without `run`, `started` and `wall_ms`, and the two `ep-000/trace.jsonl.gz` files line by line without `wall` and `run`. Then `go run ./cmd/q2bot replay -trace <run>/ep-000/trace.jsonl.gz -strict`. |
| (g) tests | `go test -count=1 ./...`, then `go test -race -count=1 ./internal/agent/... ./cmd/q2bot/...` |
| per-attempt tables | the (a), (b) or (c) commands with the seeds of each row; demo3 survival is read from `run.json` → `episodes[0].levels` (the demo3 level's `deaths` and `outcome`) |

The provenance verdict is the `gate` line of the summary and `gate` in `run.json`. Add `-min-model-share 0.7`
to make `q2bot` exit 1 when the run is not model-driven, and `-require-complete` to make it exit 1 without a
victory.

To rebuild the earlier builds:

- *Before*: check out `77f4843` and apply the `runner/{config,backend}.go` and `cmd/q2bot/runcmd.go` parts of
  round 1.
- *Round 1*: check out `1086a1d`.
- *Controls*: patched builds of the round-1 tree. *Latest answer* sets every `ArbiterConfig.Tau` negative.
  The *field-local* diagnostic returns the fallback's candidate for the chosen fields in `Arbiter.candidate`.
  These patches are not in the tree.

## Caveats

- **The mock is not Jev.**
  - Its noise model (swaps at normal confidence, independent per request) is adversarial for single-answer
    arbitration, and a real model's errors may be correlated in time. The blip-rate estimate would then
    trust a model that is consistently wrong, as it should: such errors are not noise to filter.
  - Its clean policy draws the strafe side per request.
  - Its tokens are an estimate.
  - Phase 9 (`jev-probe`, then a live run) is what settles these.
- **The recoveries are heuristics.**
  - The stall recovery gives up an attempt 50 s before the campaign would give up the run. A run that would
    have progressed within those 50 s loses an attempt.
  - The off-graph recovery needs 60 s on one spot with no nav node within reach.
  - Across this evaluation's builds, the stall recovery fired in 6 distinct runs and 4 of them won. The
    off-graph recovery fired in 2, and 1 of them won.
- **Small samples.** Every comparison on this page rests on a few hundred demo3 attempts at most.
  Differences under a factor of about two are within noise. Three are clearly outside it:
  - scripted at 0 ms against the 212 ms mock models (p ≤ 0.02);
  - the pooled fresh-fast-lane diagnosis (p ≈ 0.02);
  - accumulation against the latest answer for the clean model (p ≈ 0.04).
