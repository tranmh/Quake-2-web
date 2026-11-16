# nimble-v3 live campaign on the 4-level demo (interrupted)

Date: 2026-11-14 to 2026-11-16. Machine: Xeon workstation, Quadro T2000 (4 GB), Ollama
0.40.4 in podman (`q2-ollama`, 1 of 33 layers on the GPU).

## Verdict

**The run did not finish, so there is no campaign result for nimble-v3.** A forced
restart of the PC for software updates (SCCM, 16.11. 03:10 local time) killed the run
after 32 h 49 min, in the third of four levels.

Until then nimble-v3 played by itself and made progress: demo1 and demo2 were finished
without a death, and demo3 was at route step 4 of 13 after one death. Every one of the
4,728 model calls returned HTTP 200.

One run on one seed, cut off in the middle, says nothing about whether nimble-v3 plays
better or worse than jev or the scripted bot.

## Setup

Run `20261114T172046Z-bb815410`, started 14.11. 18:20 local time:

- `-backend ollama -model nimble-v3 -session lockstep -skill 1 -seed 1`
- maps demo1, demo2, demo3, demo2; `-max-deaths 3 -level-timeout 60m -sim-latency 212ms`
- trace `full`, demos recorded

Lockstep waits for every answer, so the slow model costs wall time but not game time.

## How far it came

| Level | Outcome | Game time | Wall time | Deaths | Bot kills | Damage taken | Model calls |
|---|---|---|---|---|---|---|---|
| demo1 | exit to demo2 | 53.2 s | 3 h 01 min | 0 | 4 | 52 | 434 |
| demo2 (visit 0) | exit to demo3 | 81.1 s | 5 h 47 min | 0 | 9 | 77 | 815 |
| demo3 | cut off at step 4 of 13 | 318.8 s | 24 h 01 min | 1 | 23 | 181 | 3,479 |
| demo2 (visit 1) | not reached | | | | | | |
| Total | | 453.1 s | 32 h 49 min | 1 | 36 | 310 | 4,728 |

The server's own count at the level exits: 5 of 21 monsters killed on demo1, 13 of 43 on
demo2, no secrets found.

### demo3 in detail

The demo3 route has 13 steps (0 to 12). Steps 0 to 3 collect the quad, the chaingun and
two ammo boxes; step 4 is `kill monster_gunner #418`; steps 5 to 12 lead through the
laser, the blue key and the lift to the exit.

| | Attempt 1 | Attempt 2 (after reload of save0) |
|---|---|---|
| Wall time | 11 h 46 min | 13 h 15 min |
| Game time | 148.9 s | 169.9 s |
| Step 0, quad | done after 74.6 s | done after 87.3 s |
| Steps 1 to 3 | 1 and 2 done, died on step 3 | all done |
| Step 4, gunner | not reached | open for 62 s of game time (4 h 54 min wall) |
| Bot kills | 7 | 16 |
| Damage taken | 97 | 84 |
| End | died (`hit`) at [861.25 206.25 -423.875] | cut off: health 31, fighting `e33` with the shotgun |

The gunner of step 4 was not dead when the run stopped. The 16 kills of attempt 2 are
soldiers, infantry, 2 berserks and 2 parasites.

## Model use

Counted from the trace, not by `q2bot`'s gate (the run wrote no `run.json`):

- 4,728 model calls (3,768 fast lane, 960 slow lane), all HTTP 200, about 3,470 input
  tokens on average.
- 23 s median between two calls, 57 s at the 95th percentile.
- Share of the 4,501 ticks where the acting value came from the model: mode 96.7%,
  fire_policy 85.2%, target 69.0%. Target is mostly `default` otherwise, on ticks
  without a fight.
- Fire policy chosen by the model on those ticks: hold 1,783, suppress 1,118,
  fire_when_aligned 932.
- 8 `stuck` events (4 on demo1, 1 on demo2, 3 on demo3).

## For scale: the scripted bot on the same seed

Run `20261114T172021Z-f263c15c` (scripted, seed 1, same settings) reached victory in
962 s of game time with 2 deaths. It needed 96.4 s for demo1, 143.1 s for demo2 and
405.9 s for demo3 with 1 death there. This is one seed and no comparison of quality.

## What is left of the run

In `runs/20261114T172046Z-bb815410/ep-000/` (not in git):

- `trace.jsonl.gz`: the end of the gzip stream is missing, 18,627 records are readable
  (`gzip -dc` reports "unexpected end of file" and still prints them). All numbers here
  come from it.
- `log.txt`: only the first 4,096 bytes reached the disk; it ends at the reload on demo3.
- `demos/00-demo1.dm2` to `03-demo3.dm2`: the last one is cut off.
- No `run.json` and no `episode.json`, so `q2bot validate` has nothing to check.

## Limits

- A campaign cannot be continued in the middle; a new run starts at demo1 and needs well
  over 33 hours on this machine.
- The same forced restart can hit a new run. Check the Software Center for pending
  restarts before starting one.
