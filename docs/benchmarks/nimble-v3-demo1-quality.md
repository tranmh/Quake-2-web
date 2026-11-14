# nimble-v3 plays demo1 (quality at simulated GPU speed)

Date: 2026-11-14. Machine: Intel Core Ultra 5 335 (8 cores: 4 at 4.5-4.7 GHz, 4 at 3.4 GHz),
30 GB RAM, no GPU.

## Verdict

**nimble-v3 cleared demo1 without dying, and played more carefully than jev.**
- It killed twice as many monsters as jev (6 against 3) and took 42% less damage (28 against 48).
- It was 23% slower to the exit (61.8 s against 50.2 s of game time).
- It played better than the scripted bot on every measure in the table below.

This is one run per bot on one level with one seed. It shows that nimble-v3 can play a level
on its own, as the model making most of the decisions. It does not show that nimble-v3 is
better than jev. That needs more seeds and the other levels, above all demo3, where jev died
6 times in an earlier campaign. On a GPU those runs would take minutes, not hours (see Cost of
the run).

This is the first time nimble-v3 played a level. The earlier [jev vs nimble-v3](jev-vs-nimble-v3.md)
benchmark could only compare answers offline, at about 90 s per decision.

## Setup

All three runs used the same settings: `-session lockstep -skill 1 -seed 1 -maps demo1 -max-deaths -1`.
- `-max-deaths -1`: the first death ends the run.
- Level timeout: the default 20 min of game time.
- Recording: on.

The model runs also used `-sim-latency 212ms` and had the decision cache off.

| | scripted | jev | nimble-v3 |
|---|---|---|---|
| Decisions by | the hand-written policy | `jev-1.13.0`, TypeSafe API | Bespoke-Nimble-9B-v3 Q8_0, Ollama 0.40.4 in Docker, CPU |
| Run | `20261114T082112Z-7c73992a` | `20261114T082113Z-4254dc1f` | `20261114T084607Z-7b9ed055` |

### How a CPU run simulates a GPU

In lockstep the game clock stops while an answer is pending. Every answer counts as arriving
212 ms of game time after it was asked, however long Ollama took in real time. So nimble-v3
plays exactly as it would on hardware that answers in 212 ms.
- No answer was stale and none timed out. The `api` stale rate was 0.
- Without lockstep (realtime), the 800 ms / 2.5 s deadlines would apply, and every CPU answer
  would be dropped for the scripted fallback.

## Results (demo1, seed 1)

| | scripted | jev | nimble-v3 |
|---|---|---|---|
| Outcome | exit | exit | exit |
| Deaths | 0 | 0 | 0 |
| Game time to the exit | 96.4 s | **50.2 s** | 61.8 s |
| Time in combat | 46.9 s | 30.4 s | 37.5 s |
| Kills by the bot | **11** | 3 | 6 |
| Damage taken | 82 | 48 | **28** |
| Stuck recoveries | 2 | 2 | 2 |
| Decisions (API calls) | 711 | 388 | 524 |
| Model share: target / fire_policy / mode | 0 | 91% / 92% / 94% | 84% / 87% / 95% |
| Disagreement with the script | 0 | 48.5% | 48.0% |
| Ticks on stale answers | 0 | 1.0% | 4.6% |
| Cost | $0 | $0.020 | $0 (8.1 h of CPU) |

Both models passed the provenance gate (model share >= 0.7, stale <= 0.15). So both results are
model-driven play, not the scripted fallback.

### Route steps (game time per step)

| Step | jev | nimble-v3 |
|---|---|---|
| 0 pick up shotgun | 13.0 s | 14.1 s |
| 1-2 pick up 2× shells | 1.1 s | 0.9 s |
| 3 fight through to door *32 | 31.5 s | 39.9 s |
| 4 door *31 | 1.7 s | 1.6 s |
| 5 press button *34 | 0.5 s | 2.8 s |
| 6-7 ride the door, exit trigger | 0.4 s + exit | 0.5 s + exit |

Nearly all of the extra time was spent in the combat of step 3. There nimble-v3 kept fighting
(4 of its 6 kills are in that step) where jev moved on. It got stuck no more often than jev.

## Cost of the run

| | |
|---|---|
| Real time | 29,132 s (8.1 h) for 61.8 s of game time |
| Ollama calls | 525 (524 in the run, all HTTP 200, plus 1 still in flight when the run ended) |
| Real latency per call | p50 52.7 s, p95 77.1 s, p99 81.5 s |
| Input tokens | 1.79 M (about 3,400 per call; jev: about 1,230) |

**Threads.** By default Ollama ran the model on 2 threads (83 s per decision). The same first
decisions of this run, timed per thread count:

| Threads | Seconds per decision |
|---|---|
| 2 (default) | 83 |
| 4 | 54.5 |
| 8 | 55.8 |

The run used `nimble-v3-t4`. It is the same weights, with `PARAMETER num_thread 4` in its
Modelfile. More threads than the 4 fast cores do not help: the fast threads wait for the slow ones.

On a GPU that answers in about 212 ms, the same run would take about 2 minutes. On this CPU a
full 4-level campaign would take several days.

## Caveats

- **One seed, one level.** demo1 is the easiest level. jev's earlier campaigns lost most of
  their time and deaths in demo3.
- **Different trajectories.** Each bot's game diverges from the first decision on. So the
  numbers compare whole plays, not answers to the same situations. The offline agreement test
  in [jev vs nimble-v3](jev-vs-nimble-v3.md) does the latter.
- **Stale ticks.** nimble-v3 played 4.6% of ticks on an older model answer, against 1.0% for jev.
  Each answer counts as arriving 212 ms of game time after it was asked for both models, so
  this is not the CPU latency. The cause was not examined. nimble-v3 also asked more requests
  per second in combat (13.4 against 12.1).

## Reproduce

```sh
docker start q2-ollama        # Ollama 0.40.4 with nimble-v3 (see jev-vs-nimble-v3.md)
printf 'FROM nimble-v3\nPARAMETER num_thread 4\n' | docker exec -i q2-ollama sh -c \
  'cat > /tmp/Modelfile && ollama create nimble-v3-t4 -f /tmp/Modelfile'
server/bin/q2bot run -backend ollama -model nimble-v3-t4 -session lockstep -sim-latency 212ms \
  -skill 1 -seed 1 -maps demo1 -max-deaths -1 -cache off -out runs -v
server/bin/q2bot run -backend jev -sim-latency 212ms -budget-usd 1 -on-exhausted stop \
  -session lockstep -skill 1 -seed 1 -maps demo1 -max-deaths -1 -out runs
server/bin/q2bot run -backend scripted -session lockstep -skill 1 -seed 1 -maps demo1 -max-deaths -1 -out runs
make replay RUN=<run id>      # watch a run in the browser
```
