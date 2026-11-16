# tev1:0.8b plays the 4-level demo (quality at simulated GPU speed)

Date: 2026-11-14/15. Machine: Intel Core Ultra 5 335 (8 cores: 4 at 4.5-4.7 GHz, 4 at 3.4 GHz),
30 GB RAM, no GPU.

## Verdict

**tev1:0.8b fails the campaign.**
- It clears demo1 and demo2, then dies 26 times on demo3, which ends the run (`death_limit`).
- It never gets past the second step of the demo3 route (quad damage, then chaingun).
- With the same seed and settings, jev and the scripted bot finish the whole campaign. They
  die 4-7 times and 2 times.

tev1:0.8b plays demo1 well: it exits with no deaths, at a speed between jev and the scripted bot.
From demo2 on it falls behind. It dies 8 times on demo2, where jev dies once and the scripted
bot not at all.

This is one run with one seed. The gap on demo3 is large: 26 deaths without progress, against
3-6 deaths and a clear for jev. So more seeds would be unlikely to reverse the ranking.

## Setup

These are the evaluation settings of [AGENT-EVAL.md](../AGENT-EVAL.md), the same as the earlier
campaign benchmarks:

`-session lockstep -sim-latency 212ms -skill 1 -seed 1 -max-deaths 25 -level-timeout 60m -cache off`

The game clock stops while an answer is pending. Every answer counts as arriving 212 ms of game
time after it was asked, however long Ollama took. So tev1 plays as it would on hardware that
answers in 212 ms. See [nimble-v3 plays demo1](nimble-v3-demo1-quality.md) for details.

| | Run | Model |
|---|---|---|
| tev1:0.8b | `20261114T173548Z-469d5f88` | `tev1-0.8b-t4`: tev1:0.8b (772.85M parameters, Q8_0), Ollama 0.40.4, `num_thread 4` |
| jev (A) | `20261112T194920Z-f51e238a` | `jev-1.13.0`. This run was hit by the breaker bug (2,800 refused calls). It failed the model-share gate. |
| jev (B) | `20261113T080856Z-d3da7bfc` | `jev-1.13.0`, after the breaker fix. It passed the gate. |
| scripted | `20261112T194755Z-e142c234` | the hand-written policy |

## Results (seed 1)

| | tev1:0.8b | jev (A) | jev (B) | scripted |
|---|---|---|---|---|
| Outcome | **failed on demo3** | victory | victory | victory |
| Levels cleared | 2 of 4 | 4 | 4 | 4 |
| Deaths | 34 | 7 | 4 | 2 |
| Game time | 1,783 s (until the failure) | 1,232 s | 1,611 s | 962 s |
| Kills by the bot | 102 | 132 | 115 | 108 |
| Damage taken | 1,056 | 1,307 | 924 | 701 |
| Stuck recoveries | 45 | 47 | 39 | 27 |
| Decisions | 17,121 | 11,006 | 13,057 | 6,817 |
| Model share: target / fire_policy / mode | 84% / 90% / 72% | 66% / 69% / 63% | 90% / 93% / 74% | 0 |
| Cost | $0 (19.6 h of CPU) | $0.46 | $0.66 | $0 |

### Per level: game time, deaths

| Level | tev1:0.8b | jev (A) | jev (B) | scripted |
|---|---|---|---|---|
| demo1 | 82.2 s, 0 | 61.5 s, 0 | 55.7 s, 0 | 96.4 s, 0 |
| demo2 | 447.4 s, **8** | 194.1 s, 1 | 146.3 s, 1 | 143.1 s, 0 |
| demo3 | 1,249.6 s, **26 (death limit)** | 923.1 s, 6 | 1,302.4 s, 3 | 405.9 s, 1 |
| demo2 (return) | not reached | 47.6 s, 0 | 100.6 s, 0 | 311.2 s, 1 |

### How tev1 dies

| | Hit by a monster | Cause unknown | Fall |
|---|---|---|---|
| demo2 (8) | 7 | 1 | 0 |
| demo3 (26) | 7 | 18 | 1 |

On demo3, 19 of tev1's 26 deaths came without a recorded hit (cause `unknown` or `fall`). These
are probably hazards of the level rather than combat. The cause was not examined further. All of
jev's deaths in both runs were hits (demo3: 6 and 3).

tev1's answers were often below the confidence threshold for movement and danger. The scripted
fallback made those decisions instead. tev1's model share was 34% for movement and 46% for
danger, against more than 70% for target, fire_policy and mode. So tev1 still passes the
provenance gate, which checks only target, fire_policy and mode. But its movement was mostly the
script's.

## Cost of the run

| | |
|---|---|
| Real time | 70,444 s (19.6 h) for 1,783 s of game time |
| Ollama calls | 17,122 (17,121 in the run, all HTTP 200; 1 still in flight when the run ended) |
| Real latency per call | p50 4.65 s, p95 6.13 s, p99 6.26 s |
| Input tokens | 30.2 M (about 1,760 per call) |

**Threads.** The first 11 decisions of demo1, seed 1, timed per thread count (the model load is
left out):

| Threads | Seconds per decision |
|---|---|
| 2 (Ollama default) | 6.1 |
| 4 | 4.1 |
| 6 | 4.6 |
| 8 | 4.5 (3.3-5.2) |

As with nimble-v3, 4 threads (the fast cores) are best. tev1:0.8b is about 13 times faster than
nimble-v3 (54.5 s) on this CPU. It is still about 5 times over the bot's 800 ms budget for a
realtime combat decision.

## Comparison with nimble-v3

nimble-v3 played only demo1, under the stricter rule that the first death ends the run. See
[nimble-v3 plays demo1](nimble-v3-demo1-quality.md).

| demo1 | nimble-v3 | tev1:0.8b |
|---|---|---|
| Deaths | 0 | 0 |
| Game time | 61.8 s | 82.2 s |
| Damage taken | 28 | 61 |
| Kills by the bot | 6 | 13 |
| Seconds per decision on this CPU | 54.5 | 4.1 |

On demo1 both models clear the level without dying. nimble-v3 is faster and takes less damage.
Ollama's published accuracy figures point the same way: 75.7% for nimble-v3 against 63.5% for
tev1:0.8b, on 13 human-labelled datasets. demo1 does not separate the models well. The
separating levels, demo2 and demo3, have not been played by nimble-v3. On this CPU that would
take several days.

## Reproduce

```sh
docker exec q2-ollama ollama pull tev1:0.8b
printf 'FROM tev1:0.8b\nPARAMETER num_thread 4\n' | docker exec -i q2-ollama sh -c \
  'cat > /tmp/Modelfile && ollama create tev1-0.8b-t4 -f /tmp/Modelfile'
server/bin/q2bot run -backend ollama -model tev1-0.8b-t4 -session lockstep -sim-latency 212ms \
  -skill 1 -seed 1 -max-deaths 25 -level-timeout 60m -cache off -out runs -v
make replay RUN=20261114T173548Z-469d5f88      # watch it in the browser
```
