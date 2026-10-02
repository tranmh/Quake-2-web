# Agent evaluation: noisy and latent models

This page measures how the agent copes with a model that is sometimes wrong (noisy) and always late (latent).
It covers the change to the decision layer that this evaluation drove: the arbiter now accumulates evidence
across answers. Every number comes from a run you can repeat with the commands in
[Reproduce](#reproduce). The design is in [ADR-0006](adr/0006-ai-agent.md) and the tools are in
[AGENT.md](AGENT.md).

Before reading the tables, note this: **completion is decided by demo3's opening fight, and the arbiter does
not fix that.** A demo3 attempt by this bot survives about 2–4 % of the time. That holds for the scripted
policy at 0 ms latency (seeds 11–22), the clean mock, and the noisy mock with the new arbiter. Only the noisy
mock under the old arbiter does worse, under 1 % (see [Large sample](#large-sample-demo3-survival-per-attempt)).
How many fixed seeds reach `victory.pcx` is therefore mostly luck. The provenance gate does not depend on
luck. The arbiter change makes a noisy model pass it on every seed, where it failed on every seed before.

## Gate verdicts (final build)

| Gate | Requirement | Result | Verdict |
|---|---|---|---|
| (a) noisy mock, seeds 1–5 | ≥ 4 of 5 reach `victory.pcx`, and every completed run is model-driven | 3 of 5 victories (seeds 2, 3, 4). All 5 runs are model-driven: min share ≥ 0.84, ticks on stale answers ≤ 4.8 %. | **fails** (3 of 5) |
| (b) clean mock, seeds 1–3 | all victories, model-driven | 2 of 3 victories (seeds 1, 2). Seed 3 hit the 60 min level watchdog after 21 deaths on demo3. All 3 are model-driven. | **fails** (2 of 3) |
| (c) scripted | `TestCampaignScripted` (seed 1); seeds 1–3 victory via `q2bot` | The test passes: 826 s game time, 3 deaths. Seeds 1–3 win with 4, 1 and 4 deaths, the same runs as before the change. | passes |
| (d) ablations | constant and random fail or are measurably worse | constant fails on demo1 (26 deaths). random fails on demo3 (36 deaths). Scripted wins with 1–4 deaths. | passes |
| (e) latency sweep, clean mock, seed 1 | report | 100 ms and 212 ms win; 400 ms and 500 ms fail on demo3 (table below). | reported |
| (f) determinism | identical `run.json` (wall-clock fields excluded) and traces | Two noisy seed-1 runs give identical traces (137,952 events) and identical `run.json`. `q2bot replay -strict` finds no divergence. | passes |
| (g) tests | fairness differential test and every existing test | `go test ./...` and `go test -race ./internal/agent/... ./cmd/q2bot/...` are green (see [Tests](#g-tests)). | passes |

## Method

- **Runs.**
  - Every run plays the whole campaign (demo1 → demo2 → demo3 → demo2 → `victory.pcx`) in a lockstep
    session at skill 1.
  - Limits are `-max-deaths 25` per level and `-level-timeout 60m`. Demos are off (`-record=false`).
    One episode per run.
  - The large-sample runs use `-max-deaths 5`.
- **Backends.**
  - `scripted` is the rule policy, which answers at once.
  - `mock` is the real jev HTTP client (`backend/jev`) talking to the in-process fake Jev server
    (`backend/jevtest`) over loopback. Its answers come from the lane state JSON the client sends, never
    from the bot's belief.
    - `-mock-policy scripted` (**clean**) runs the scripted rules on the received state. The strafe rhythm's
      clock is drawn from the request digest, so the strafe side varies by request.
    - `-mock-policy noisy` (**noisy**, the default) perturbs those answers:
      - up to 30 % of the probability mass is spread at random (`-mock-noise 0.3`);
      - the top two options swap with probability 0.1 (`-mock-swap 0.1`), and a swapped answer keeps a normal
        confidence;
      - the confidence is replaced by one in [0.05, 0.3] with probability 0.1 (`-mock-lowconf 0.1`);
      - danger scores shift by N(0, 0.4) levels.
    - The noise is seeded by the run seed and the request's content, so the same request gets the same
      answer.
    - Both mock policies also inject 2 % HTTP 500s and 2 % answers missing a question.
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
- **Survival metric.** "demo3 survival" is demo3 exits ÷ demo3 attempts, where attempts = deaths + 1 per
  demo3 visit. Each level-entry reload is an attempt. This is the quantity that decides a run.
- **Builds.**
  - *Before* is the tree at `77f4843`, plus the `-mock-*` flags.
  - *After* is the final tree.
  - The final build's only change from the one that played the other "after" runs is the lifted fast-lane
    attempt cap for lockstep mock runs ([Determinism](#f-determinism)). That build reran noisy seed 1. The
    cap matters only when a loopback call takes more than 800 ms, which happened in no other run: they report
    no transport timeouts. Their results are therefore identical by construction.

## What changed

### Evidence accumulation in the arbiter (`agent/decide/arbiter.go`)

Before, the arbiter trusted each answer alone:
- a confidence under 0.35 forced the scripted fallback;
- a confident wrong answer (a swap) was acted on;
- hysteresis then held that wrong value (fire_policy 0.4 s, mode 1.5 s);
- a value held past its answer's TTL counted as stale.

Now each field keeps the model's accepted answers as evidence and decides from their accumulated posterior:

- **Weight.**
  - Each answer weighs its confidence (a noul's |2p−1|) × exp(−Δt/τ).
  - Δt is the answer's snapshot age relative to the field's newest answer. The latency itself does not weaken
    the evidence; expiry is the TTL's job.
  - τ is 300 ms for target, fire_policy and movement, 1 s for mode, 500 ms for danger, and 1.5 s for weapon
    and pickup (`ArbiterConfig.Tau`).
  - Answers older than 5 τ are dropped, and at most 24 are kept.
- **Posterior.**
  - It is the weighted mixture of the answers' probability vectors over the options of the field's question
    in the lane's latest *answered* request.
  - Options keep stable ids: track ids, item ids, mode names.
  - An option gone from that state, a dead target or a taken item, is dropped and the rest renormalized.
  - A weapon answer's `keep` is stored as the weapon in hand at the time, so evidence survives a weapon
    switch.
  - A score's value (danger) is the weighted mean level.
  - Sums run in option order, so the posterior is deterministic.
- **Decision.**
  - The posterior decides, with provenance `model`, while the field's newest answer is within its TTL and
    the evidence is strong enough.
  - Strong enough means the accumulated weight of the remaining options is ≥ 0.35 (0.2 for a noul) and, for
    a choice, the top option is ≥ 0.4.
  - Otherwise the field falls back to the scripted policy: reason `weak`, or `ttl`/`error`/`timeout` when
    every contributing answer expired.
  - A lone low-confidence answer is too weak to decide. Among confident answers it only counts for less,
    and four agreeing low-confidence answers (0.15 each, 100 ms apart) decide.
- **Hysteresis on the posterior.**
  - It is unchanged in form (`ModeDelta`, `TargetDelta`, dwell times), but is now applied to the posterior's
    probabilities.
  - A held value is `model` while the answer that set it is within its TTL, or while the fresh posterior
    still gives it ≥ 0.4. It is `stale` when only its dwell time keeps it.
- **Safety asymmetry.**
  - Danger rises at once to a fresh answer's level and falls with the accumulated evidence.
  - At danger ≥ 3.5 the newest answer's `retreat` is acted on at once.
  - Without this, accumulation delayed retreats. The newest answer said retreat at danger ≥ 3.5 on 59 and
    142 ticks of two clean runs, against 0 before.
- **Scripted backend.** The scripted backend's answers are exact, not evidence, so its arbiter decides every
  field from the latest answer (τ < 0). That path reproduces the old arbiter decision for decision: all
  21,728 decision events of clean-mock seed 1 are identical with every τ negative.
- **Unit tests** (`decide/evidence_test.go`):
  - a single swapped answer among consistent ones does not flip target, fire_policy, movement or mode, though
    the same answer flips the old arbiter;
  - a sustained change flips within 2–3 answers;
  - low-confidence answers still count;
  - vanished and dead options are dropped;
  - expiry falls back;
  - the score mean;
  - the safety asymmetry;
  - weapon keep across a switch;
  - provenance of held values;
  - bit-identical repeat of 200 random answers.

### Other changes

- **`q2bot run` flags and `runner.Config` fields:**
  - `-mock-policy scripted|noisy` (`MockPolicy`);
  - `-mock-noise`, `-mock-swap`, `-mock-lowconf` (`MockNoise`, `MockSwap`, `MockLowConfidence`), where
    0 turns an effect off.
- **Random ablation.** It now answers one-hot on its random pick: a model that is sure of a random choice.
  The old version sent uniform probabilities with confidence 1. The accumulating arbiter rightly treats those
  as no evidence, which would have turned the ablation into the scripted policy.
- **Wedge recovery** (`bot.wedgeTick`).
  - If the bot stands within 24 units for 120 s while its navigator is under way on ≥ 75 % of the frames and
    recovering ≥ 8 times (stuck reports plus repaths), it types `kill`. That counts as a death and reloads
    the level-entry save, like the existing pit recovery.
  - It fired once in the final runs. That was clean seed 2 at a demo2 spot where the navigator keeps
    re-planning without moving, and the run went on to win.
  - The trigger was a development run of clean seed 2 that stood 5 minutes wedged on a parasite below its
    view and failed the level. A 30 s threshold and a no-path variant were tried and dropped:
    - at 30 s it also killed bots that would have freed themselves;
    - on demo3's kill step the navigator legitimately reports no path while the bot waits.
- **Lockstep mock determinism.** The jev client's fast-lane attempt was capped at 800 ms of wall clock.
  Under a heavily loaded machine one loopback call took longer, and that single timeout changed a whole run
  (noisy seed 1: 3710 s versus 3726 s). `jev.Config.UncapFast` lifts the cap. The runner sets it with a
  30 s attempt timeout for lockstep mock runs only, where the timeout just guards against a hang. Realtime
  clients keep the cap.

## Results

Each row is one run. Columns:
- **deaths**: per level (demo1 / demo2 / demo3 / demo2 second visit);
- **game** and **wall**: seconds;
- **model t/f/m**: the model share of decided ticks for target / fire_policy / mode;
- **stale**: the largest share of a gate field's ticks that acted on a stale answer;
- **gate**: whether the run is model-driven;
- **tokens** and **cost**: input tokens and their cost at $0.042 per million.

### (a) Noisy mock, 212 ms, seeds 1–5

| run | outcome | deaths | game | wall | model t/f/m | stale | gate | calls | tokens | cost |
|---|---|---|---|---|---|---|---|---|---|---|
| before s1 | failed: 26 deaths on demo3 | 27 (1 / 0 / 26) | 3852 | 154 | 0.80 / 0.69 / 0.78 | 14.1 % | no | 34444 | 19.88 M | $0.83 |
| before s2 | failed: 26 deaths on demo3 | 30 (1 / 3 / 26) | 2900 | 118 | 0.77 / 0.68 / 0.80 | 15.7 % | no | 27580 | 15.81 M | $0.66 |
| before s3 | failed: 26 deaths on demo3 | 27 (0 / 1 / 26) | 3313 | 159 | 0.79 / 0.69 / 0.79 | 14.8 % | no | 30956 | 17.76 M | $0.75 |
| before s4 | victory | 6 (0 / 1 / 3 / 2) | 1412 | 63 | 0.78 / 0.69 / 0.77 | 13.0 % | no | 12171 | 6.81 M | $0.29 |
| before s5 | failed: 26 deaths on demo3 | 27 (0 / 1 / 26) | 2726 | 119 | 0.79 / 0.69 / 0.79 | 15.7 % | no | 25401 | 14.49 M | $0.61 |
| **after s1** | failed: 26 deaths on demo3 | 26 (0 / 0 / 26) | 3726 | 197 | 0.89 / 0.87 / 0.91 | 4.2 % | yes | 31807 | 17.74 M | $0.75 |
| **after s2** | victory | 16 (0 / 0 / 16 / 0) | 3212 | 233 | 0.89 / 0.85 / 0.89 | 4.2 % | yes | 27750 | 15.37 M | $0.65 |
| **after s3** | victory | 2 (0 / 0 / 2 / 0) | 890 | 64 | 0.87 / 0.85 / 0.91 | 4.8 % | yes | 7437 | 4.17 M | $0.18 |
| **after s4** | victory | 13 (0 / 0 / 13 / 0) | 2512 | 182 | 0.89 / 0.86 / 0.91 | 4.1 % | yes | 21308 | 11.95 M | $0.50 |
| **after s5** | failed: 26 deaths on demo3 | 26 (0 / 0 / 26) | 3756 | 201 | 0.88 / 0.86 / 0.90 | 4.3 % | yes | 32173 | 18.24 M | $0.77 |

**Summary.**
- Victories went from 1 of 5 to 3 of 5.
- demo3 survival went from 1/112 (0.9 %) to 3/88 (3.4 %).
- Deaths outside demo3 went from 10 to 0.
- The gate went from failed on every seed (fire_policy 0.68–0.69; 13–16 % of fire_policy ticks on stale
  answers) to passed on every seed.
- Part of the stale-share drop is relabelling, not behaviour. Before, a value held by hysteresis past its
  answer's TTL was always stale. Now it stays `model` while the fresh posterior still gives it ≥ 0.4, and is
  stale only when its dwell time alone keeps it.
- The model shares also rest on a different count. Before, low-confidence answers went to the scripted
  fallback (10 % of answers). Now they are accumulated.

Per-run figures, identical across the five seeds unless a range is given:

| | before | after |
|---|---|---|
| answer latency p50/p95 | 212/212 ms | 212/212 ms |
| fast-lane rate in combat | 11.6–12.1 QPS | 11.8–12.8 QPS |
| disagreement with the script | 18 % | 18 % |
| movement model share | 0.59–0.61 | 0.70–0.73 |

### (b) Clean mock, 212 ms, seeds 1–3

| run | outcome | deaths | game | wall | model t/f/m | stale | gate | calls | tokens | cost |
|---|---|---|---|---|---|---|---|---|---|---|
| before s1 | victory | 5 (0 / 3 / 2 / 0) | 1238 | 47 | 0.89 / 0.89 / 0.93 | 3.4 % | yes | 9639 | 5.27 M | $0.22 |
| before s2 | victory | 0 (0 / 0 / 0 / 0) | 974 | 29 | 0.70 / 0.69 / 0.91 | 2.6 % | no | 5485 | 3.00 M | $0.13 |
| before s3 | victory | 4 (0 / 0 / 1 / 3) | 927 | 32 | 0.87 / 0.86 / 0.92 | 3.2 % | yes | 7275 | 3.89 M | $0.16 |
| **after s1** | victory | 8 (1 / 0 / 7 / 0) | 2394 | 131 | 0.87 / 0.87 / 0.93 | 1.5 % | yes | 18212 | 9.92 M | $0.42 |
| **after s2** | victory | 22 (0 / 1 / 20 / 1) | 4010 | 202 | 0.90 / 0.90 / 0.93 | 1.3 % | yes | 33825 | 18.77 M | $0.79 |
| **after s3** | failed: level watchdog (60 min) | 22 (0 / 1 / 21) | 3845 | 156 | 0.89 / 0.89 / 0.91 | 1.6 % | yes | 29898 | 16.55 M | $0.70 |

On these three seeds the clean mock did worse after the change: 3/6 demo3 survival before against 2/51
after. Over seeds 11–18 the difference is within noise (see [Large sample](#large-sample-demo3-survival-per-attempt)):
- before: 2/48 (4.2 %);
- after: 1/41 (2.4 %);
- scripted at 0 ms on seeds 11–22: 3/73 (4.1 %).

The three "before" seeds were lucky draws. Accumulation does add reaction lag for a clean model: a sustained
change needs 2–3 answers, not 1, which is 100–200 ms on the fast lane and 0.2–0.5 s on the slow lane.

### (c) Scripted, 0 ms, seeds 1–3

| run | outcome | deaths | game | wall |
|---|---|---|---|---|
| before s1 | victory | 4 (0 / 0 / 3 / 1) | 1043 | 38 |
| before s2 | victory | 1 (0 / 0 / 0 / 1) | 584 | 21 |
| before s3 | victory | 4 (0 / 0 / 4 / 0) | 1313 | 42 |
| **after s1** | victory | 4 (0 / 0 / 3 / 1) | 1043 | 32 |
| **after s2** | victory | 1 (0 / 0 / 0 / 1) | 584 | 18 |
| **after s3** | victory | 4 (0 / 0 / 4 / 0) | 1313 | 41 |

The scripted runs are the same before and after. Their decision events are identical (23,106 for seed 3,
18,874 for seed 1) because the scripted backend takes the latest-answer path, and no wedge recovery fired.
`TestCampaignScripted` (seed 1, the campaign package's own lockstep loop) passes with 826 s of game time and
3 deaths (demo3 3), unchanged.

### (d) Ablations, seed 1

| run | outcome | deaths | game | wall | model t/f/m |
|---|---|---|---|---|---|
| before constant | failed: 26 deaths on demo1 | 26 (26) | 816 | 19 | 1.00 / 1.00 / 0.86 |
| before random | failed: 26 deaths on demo3 | 26 (0 / 0 / 26) | 2587 | 104 | 0.30 / 0.98 / 0.48 |
| **after constant** | failed: 26 deaths on demo1 | 26 (26) | 915 | 23 | 1.00 / 1.00 / 0.87 |
| **after random** | failed: 26 deaths on demo3 | 36 (10 / 0 / 26) | 2483 | 103 | 0.69 / 0.92 / 0.51 |

Both ablations fail, against 1–4 deaths for the scripted policy and 2–16 deaths in the winning noisy runs.
The random ablation's mode share is 0.51. Of its decided mode ticks:
- 26 % are the bot's reflex overrides, mostly an explore while the route has the bot;
- 20 % are the scripted fallback, half of them `weak`: its random one-hot picks often leave no option with
  40 % of the posterior.

### (e) Latency sweep, clean mock, seed 1

| latency | before | after |
|---|---|---|
| 100 ms | victory, 1 death, 883 s, stale 0.7 % | victory, 4 deaths, 1253 s, stale 0.4 % |
| 212 ms | victory, 5 deaths, 1238 s, stale 3.4 % | victory, 8 deaths, 2394 s, stale 1.5 % |
| 400 ms | victory, 1 death, 604 s, stale 2.1 % | failed: 26 deaths on demo3, 3195 s, stale 1.1 % |
| 500 ms | failed: 26 deaths on demo3, 3803 s, stale 2.3 % | failed: 26 deaths on demo3, 3529 s, stale 1.1 % |

Every run of the sweep is model-driven (gate passes; model shares 0.87–0.97). Taken one seed at a time the
sweep is as noisy as the rest: demo3 survival is 2/31 before and 1/59 after.

### Large sample: demo3 survival per attempt

Seeds 11–18 for the mock rows and 11–22 for the scripted rows. All runs use 212 ms latency unless stated,
`-max-deaths 5` and `-trace digest`. "After" here is the final arbiter in a development build whose wedge
recovery fired after 30 s (the final build waits 120 s).

| backend | runs | victories | demo3 survival |
|---|---|---|---|
| scripted, 0 ms | 12 | 3 | 3/73 (4.1 %) |
| scripted, 212 ms | 12 | 1 | 2/72 (2.8 %) |
| clean mock, before | 8 | 2 | 2/48 (4.2 %) |
| clean mock, after | 8 | 1 | 1/41 (2.4 %) |
| noisy mock, before | 8 | 0 | 0/56 (0 %) |
| noisy mock, after | 8 | 1 | 1/51 (2.0 %) |

Combining every full run on seeds 1–5 with the large sample:
- the noisy mock's demo3 survival rises from 1/168 (0.6 %) before to 4/139 (2.9 %) after;
- that is the range of the clean mock and of the scripted policy (2–4 %).

Two shortfalls remain:
- about two thirds of the damage taken on demo3 has no attributed source: the bot is shot from outside its
  view;
- each attempt starts from the level-entry save, usually with only the blaster.

A run with up to 25 attempts and a 60 min watchdog therefore wins with a probability of about 0.6 whichever
policy plays. "4 of 5 seeds" and "3 of 3 seeds" happen to be met about one time in three and one time in four.

### (f) Determinism

- **Repeat runs.** Two identical runs of the final build (`-backend mock`, noisy, seed 1) give identical
  `run.json` once the run id, `started` and `wall_ms` are excluded. Their traces are identical event by event
  with the wall clock and run id removed (137,952 events). An earlier pair (noisy seed 1, before the wedge
  and safety changes) was identical too (57,271 events).
- **Replay.** `q2bot replay -trace <noisy seed 3 trace> -strict` finds no divergence: 32,692 events equal,
  35,028 usercmds fed and recomputed equal, 7,437 responses matched.
- **Root cause.** Determinism had broken once under machine load: one 800 ms fast-lane attempt timed out on
  loopback. It is fixed by `UncapFast` (see [Other changes](#other-changes)). After the fix, both noisy
  seed-1 runs are identical, to each other and to the earlier run without the timeout.

### (g) Tests

- `gofmt -l` is clean and `go vet ./...` passes.
- `go test -count=1 ./...` passes over all server packages, including:
  - the fairness differential test (`internal/agent/fairness`);
  - `TestCampaignScripted`;
  - the oracle and golden tests.
- `go test -race -count=1 ./internal/agent/... ./cmd/q2bot/...` passes.
- No ported package changed.

## Cost

These are fake-server estimates (bytes ÷ 4), at $0.042 per million input tokens:

| | mean input tokens | mean cost per run | tokens per request | cost per game hour |
|---|---|---|---|---|
| noisy mock, before (5 runs) | 14.95 M | $0.63 | 573 | $0.80 |
| noisy mock, after (5 runs) | 13.49 M | $0.57 | 560 | $0.72 |
| clean mock, before (3 runs) | 4.05 M | $0.17 | 543 | $0.59 |
| clean mock, after (3 runs) | 15.08 M | $0.63 | 552 | $0.67 |

- **Cost is game time.** The rate is about $0.6–0.8 per game hour at about 12 requests per second in
  combat, so a run costs what its game time costs. A quick victory (noisy seed 3, 890 s) costs $0.18. A run
  that dies 26 times on demo3 costs about $0.75.
- **Accumulation is free.** It adds no requests.
- **Real Jev may differ.** Its tokenizer and question overhead will change the per-request figure; the plan
  estimated 1–1.5k tokens. `q2bot jev-probe` measures the real number.

## Interpretation

- **The arbiter now passes the provenance gate.**
  - Under the noisy policy the old arbiter acted on 10 % swapped answers and dropped 10 % low-confidence
    ones, and its hysteresis held the wrong values. fire_policy came from the model on only 68–69 % of ticks,
    and 13–16 % of ticks acted on stale values.
  - Accumulating confidence-weighted answers rejects isolated swaps and keeps low-confidence answers as weak
    evidence. fire_policy now comes from the model on 85–87 % of ticks, with about 4 % stale.
  - Every noisy and clean run is model-driven.
- **Noisy play improved.**
  - demo3 survival rose from about 0.6 % to about 3 %, deaths outside demo3 fell from 10 to 0, and victories
    went from 1/5 to 3/5.
  - The noisy model now plays about as well as the clean one.
- **The cost is reaction lag.**
  - A real change of mind needs 2–3 answers.
  - The safety asymmetry keeps that lag out of retreats.
  - Measured over many seeds, the lag does not hurt the clean model beyond noise.
- **Completion is the bot's fight, not the arbiter.**
  - demo3's opening survives about 3–4 % of attempts for every policy, the scripted one at 0 ms latency
    included. Gates (a) and (b) are therefore lotteries over seed choice. Passing them reliably needs better
    demo3 combat: facing attackers it cannot see, and getting a better weapon before the gunner fight.
  - Seed sweeps of any size should report per-attempt survival with its counts, not victories on 3–5 seeds.

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
| (c) scripted, seed N | `go run ./cmd/q2bot run -backend scripted -seed N $COMMON` |
| (c) the scripted gate test | `go test -count=1 -run 'TestCampaignScripted$' -v ./internal/agent/campaign` |
| (d) ablations | `go run ./cmd/q2bot run -backend constant -seed 1 $COMMON`, then the same with `-backend random` |
| (e) latency sweep | `go run ./cmd/q2bot run -backend mock -mock-policy scripted -seed 1 -sim-latency 100ms $COMMON`, then 212ms, 400ms and 500ms |
| (f) determinism | run (a) with seed 1 twice. Compare the two `run.json` files without `run`, `started` and `wall_ms`, and the two `ep-000/trace.jsonl.gz` files line by line without `wall` and `run`. Then `go run ./cmd/q2bot replay -trace <run>/ep-000/trace.jsonl.gz -strict`. |
| (g) tests | `go test -count=1 ./...`, then `go test -race -count=1 ./internal/agent/... ./cmd/q2bot/...` |
| Large sample | the (a), (b) or (c) command with `-seed 11` … `-seed 22 -max-deaths 5 -trace digest` (scripted at 212 ms: add `-sim-latency 212ms`) |

The provenance verdict is the `gate` line of the summary and `gate` in `run.json`. Add `-min-model-share 0.7`
to make `q2bot` exit 1 when the run is not model-driven, and `-require-complete` to make it exit 1 without a
victory.

The "before" rows were played by the tree at `77f4843` with only the `-mock-*` flags added. To rebuild it,
check out `77f4843` and apply the `runner/{config,backend}.go` and `cmd/q2bot/runcmd.go` parts of this
change.

## Caveats

- **The mock is not Jev.**
  - Its noise model (swaps at normal confidence, independent per request) is adversarial for single-answer
    arbitration, and a real model's errors may be correlated in time.
  - Its tokens are an estimate.
  - Phase 9 (`jev-probe`, then a live run) is what settles both.
- **The wedge recovery is a heuristic.**
  - Two demo2 spots look wedged for a while: (−870, 288, −136) on the first visit and (159, −1480, 24) on
    the second. Some runs free themselves there and some never do.
  - The 120 s threshold trades 2 minutes of game time for not killing a bot that would have freed itself.
