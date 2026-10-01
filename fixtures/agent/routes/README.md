# Route tables

Per-visit subgoal tables for the demo campaign
(demo1 → demo2 → demo3 → demo2 again → `victory.pcx`), loaded by
`server/internal/agent/route` and checked against the demo pak with:

    cd server && go run ./cmd/q2nav plan -pak ../assets/demo/baseq2/pak0.pak

`plan` prints every exit of every map with the logic chains that fire it,
then each table with what its steps set off, and exits non-zero when a
table is invalid (see `route.Validate` / `route.ValidateCampaign` for the
rules).

## Files

- `campaign.json`: `{schema, name, skill, start, visits: [table files in
  order], terminal: {exit, kind}}`. The campaign ends when the last visit's
  exit (`victory.pcx`, a `pic`) fires.
- One table per visit: `demo1.json`, `demo2a.json` (first visit of demo2),
  `demo3.json`, `demo2b.json` (second visit, which restores demo2 as it was
  left).

## Table schema (version 1)

    {
      "schema": 1, "name": "demo2a", "map": "demo2",
      "visit": 0,                  // earlier visits of this map
      "title": "...", "from": "base1",   // arrival spawnpoint ("" = map start)
      "exit": {"map": "demo3$base2a"},   // the target_changelevel "map" value
      "steps": [ {"op": ..., ...}, ... ],
      "avoid": [ {"target": <ref>, "why": "..."} ]   // activators of other exits
    }

A `<ref>` names an entity: `{"entity": lump index, "model": "*N",
"classname": ..., "targetname": ...}`. Every given field must match, and
the entity must be present at the campaign skill (not inhibited, not freed
by its spawn function). Lump indexes count from 0 (worldspawn); they are not
edict numbers.

| op | fields | meaning |
|---|---|---|
| `goto` | `target` or `pos`, `radius` | walk there (onto a mover target) |
| `touch` | `target` (trigger_multiple/once), `yaw`, `effects` | walk into the volume; directional triggers need a facing |
| `press` | `target` (touch func_button), `effects` | walk into the button (the game never reads BUTTON_USE) |
| `shoot` | `target` (entity with health), `effects` | damage it |
| `ride` | `target` (door/plat/train), `until` | stay on it until pose `pos1`/`pos2`/`top`/`bottom`/corner |
| `wait` | `seconds`, `effects` | wait, or until effects caused earlier are seen |
| `face` | `yaw` | turn |
| `kill` | `class`, `pos` (spawn origin), `target`, `effects` | kill that monster |
| `pickup` | `class`, `pos`, `effects` | pick the item up (adds it to the inventory keys need) |
| `confirm` | `effects` | guard: effects caused earlier must be observed |

Effect kinds: `exit`, `laserOff`, `laserOn`, `doorOpen`, `moverAt` (with
`pose`), `enable`, `remove`, `wake`, `use`. A step's claimed effects must be
reached by its entity's logic chain (targets through relays, delays, doors
that fire as they open, buttons that fire on arrival, keys, counters, and
movers that carry a rider into a trigger). In particular:

- `moverAt` names the pose the use sends the mover to: `pos2` for doors,
  rotating doors and buttons, `pos2`/`bottom` for a plat (`Use_Plat` sends
  it down), `pos1` or `pos2` for a secret door, a corner on the train's run
  up to its first `wait -1` corner.
- `doorOpen` is not accepted for a START_OPEN door: using it closes it.
- A mover carrying the rider into a TRIGGERED trigger only fires it when an
  earlier step enabled it (`enable` is caused by a use, never by a carry).
- Single-use entities stay used up for the rest of the visit and, because a
  revisit restores the level as it was left, for later visits of the map:
  a fired `trigger_once`, a pressed `wait -1` button, a `wait -1` door that
  has moved, a killed monster, a picked-up item, a removed killtarget. No
  later step may activate one, claim it moves again, or rely on a chain
  through it, and nothing may refer to a removed one.
