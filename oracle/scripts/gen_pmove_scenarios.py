#!/usr/bin/env python3
"""Generate pmove scenarios (input of `oracle_core pmove`) for a demo map.

Usage: gen_pmove_scenarios.py <oracle_core> <basedir> <map> <out.json> [airaccelerate]

Scenario format (consumed only by oracle_core; the resulting fixture carries every cmd and reset state):
  {"map":"demo1","airaccelerate":0,"steps":[{"cmd":UC,"in":pmove_state?}, ...]}

Start positions come from the map itself (info_player_* entities; floor/water/ladder spots found by
querying the C collision model through `oracle_core trace`). The output is derived from the pak and is therefore
written under fixtures/generated (never committed). Fully deterministic: own xorshift32 PRNG.
"""
import json
import os
import re
import struct
import subprocess
import sys
import tempfile

CONTENTS_SOLID = 1
CONTENTS_WATER = 32
MASK_WATER = 32 | 8 | 16
CONTENTS_LADDER = 0x20000000
MASK_PLAYERSOLID = 1 | 0x10000 | 2 | 0x2000000
PLAYER_MINS = [-16.0, -16.0, -24.0]
PLAYER_MAXS = [16.0, 16.0, 32.0]
PM_NORMAL, PM_SPECTATOR, PM_DEAD, PM_GIB, PM_FREEZE = range(5)
PMF_DUCKED, PMF_JUMP_HELD, PMF_ON_GROUND, PMF_TIME_WATERJUMP, PMF_TIME_LAND, PMF_TIME_TELEPORT = 1, 2, 4, 8, 16, 32


class XR:
    def __init__(self, seed):
        self.s = seed & 0xFFFFFFFF or 0x9E3779B9

    def next(self):
        x = self.s
        x ^= (x << 13) & 0xFFFFFFFF
        x ^= x >> 17
        x ^= (x << 5) & 0xFFFFFFFF
        self.s = x
        return x

    def range(self, n):
        return self.next() % n

    def uniform(self, lo, hi):
        return lo + (hi - lo) * ((self.next() >> 8) / 16777216.0)

    def choice(self, seq):
        return seq[self.range(len(seq))]


def read_entities(basedir, mapname):
    pak = os.path.join(basedir, "baseq2", "pak0.pak")
    with open(pak, "rb") as f:
        magic, ofs, ln = struct.unpack("<4sii", f.read(12))
        f.seek(ofs)
        for _ in range(ln // 64):
            name, pos, size = struct.unpack("<56sii", f.read(64))
            if name.split(b"\0")[0].decode() == "maps/%s.bsp" % mapname:
                break
        else:
            raise SystemExit("map not in pak")
        f.seek(pos)
        bsp = f.read(size)
    eofs, elen = struct.unpack_from("<ii", bsp, 8)  # lump 0 = entities
    text = bsp[eofs:eofs + elen].split(b"\0")[0].decode("latin-1")
    ents = []
    for block in re.findall(r"\{([^}]*)\}", text):
        ents.append(dict(re.findall(r'"([^"]*)"\s*"([^"]*)"', block)))
    return ents


class Oracle:
    def __init__(self, exe, basedir, mapname):
        self.exe, self.basedir, self.map = exe, basedir, mapname

    def run(self, queries):
        with tempfile.TemporaryDirectory() as td:
            qp, op = os.path.join(td, "q.jsonl"), os.path.join(td, "r.jsonl")
            with open(qp, "w") as f:
                for q in queries:
                    f.write(json.dumps({"q": q}) + "\n")
            subprocess.run([self.exe, "--basedir", self.basedir, "-o", op, "trace", self.map, qp], check=True)
            with open(op) as f:
                return [json.loads(l)["r"] for l in f]

    def bounds(self):
        with tempfile.TemporaryDirectory() as td:
            op = os.path.join(td, "b.json")
            subprocess.run([self.exe, "--basedir", self.basedir, "-o", op, "bsp", self.map], check=True)
            with open(op) as f:
                c = json.load(f)["cmodels"][0]
            return c["mins"], c["maxs"]


def f32(v):
    return struct.unpack("<f", struct.pack("<f", v))[0]


def vec(v):
    return [f32(x) for x in v]


def find_spots(orc, rng):
    mins, maxs = orc.bounds()
    cands = [vec([rng.uniform(mins[i] + 32, maxs[i] - 32) for i in range(3)]) for _ in range(20000)]
    qs = []
    for p in cands:
        qs.append({"kind": "point", "p": p, "headnode": 0})
        qs.append({"kind": "box", "start": p, "end": p, "mins": PLAYER_MINS, "maxs": PLAYER_MAXS, "headnode": 0,
                   "mask": MASK_PLAYERSOLID})
        qs.append({"kind": "box", "start": p, "end": [p[0], p[1], p[2] - 4096], "mins": PLAYER_MINS,
                   "maxs": PLAYER_MAXS, "headnode": 0, "mask": MASK_PLAYERSOLID | MASK_WATER})
    rs = orc.run(qs)
    floor, water = [], []
    for i, p in enumerate(cands):
        pc, hull, down = rs[3 * i]["contents"], rs[3 * i + 1], rs[3 * i + 2]
        if hull["startsolid"] or hull["allsolid"]:
            continue
        if pc & MASK_WATER:
            water.append(p)
        elif not down["startsolid"] and down["fraction"] < 1 and down["plane"]["normal"][2] > 0.7 \
                and not (down["contents"] & MASK_WATER):
            floor.append(vec([p[0], p[1], down["endpos"][2] + 0.25]))
    # ladders: horizontal hull traces from floor spots that hit CONTENTS_LADDER brushes
    qs, meta = [], []
    for p in floor[:400]:
        for d in range(8):
            yaw = d * 45.0
            import math
            dx, dy = math.cos(math.radians(yaw)), math.sin(math.radians(yaw))
            qs.append({"kind": "box", "start": p, "end": vec([p[0] + dx * 256, p[1] + dy * 256, p[2]]),
                       "mins": PLAYER_MINS, "maxs": PLAYER_MAXS, "headnode": 0, "mask": MASK_PLAYERSOLID})
            meta.append((p, yaw))
    ladders = []
    for (p, yaw), r in zip(meta, orc.run(qs) if qs else []):
        if not r["startsolid"] and r["fraction"] < 1 and (r["contents"] & CONTENTS_LADDER):
            ladders.append((p, yaw))
    return floor, water, ladders


def st(origin, pm_type=PM_NORMAL, velocity=(0, 0, 0), flags=0, time=0, gravity=800, delta=(0, 0, 0)):
    return {"pm_type": pm_type, "origin": [int(round(x * 8)) for x in origin],
            "velocity": [int(v) for v in velocity], "pm_flags": flags, "pm_time": time, "gravity": gravity,
            "delta_angles": [int(d) for d in delta]}


def a2s(a):
    return int(a * 65536 / 360) & 65535


def s16(v):
    v &= 0xFFFF
    return v - 65536 if v >= 32768 else v


def cmd(msec, pitch=0.0, yaw=0.0, fwd=0, side=0, up=0, buttons=0, roll=0.0):
    return {"msec": msec, "buttons": buttons, "angles": [s16(a2s(pitch)), s16(a2s(yaw)), s16(a2s(roll))],
            "forwardmove": fwd, "sidemove": side, "upmove": up, "impulse": 0, "lightlevel": 128}


MSECS = [16, 16, 16, 33, 50, 100, 8, 1, 2, 250, 125, 11, 17, 25, 66, 200]


def gen(orc, ents, mapname, seed, airaccelerate=0):
    rng = XR(seed)
    steps = []

    def seg(state, cmds):
        for i, c in enumerate(cmds):
            s = {"cmd": c}
            if i == 0:
                s["in"] = state
            steps.append(s)

    starts = []
    for e in ents:
        if e.get("classname", "").startswith("info_player_") and "origin" in e:
            o = [float(x) for x in e["origin"].split()]
            # prefer info_player_start (single player spawn), then any other info_player_*
            starts.append((0 if e["classname"] == "info_player_start" else 1, len(starts), vec(o),
                           float(e.get("angle", "0"))))
    if not starts:
        raise SystemExit("no info_player_* in " + mapname)
    _, _, start, syaw = min(starts)
    floor, water, ladders = find_spots(orc, rng)
    print("%s: %d starts, %d floor, %d water, %d ladder spots" % (mapname, len(starts), len(floor), len(water),
                                                                  len(ladders)), file=sys.stderr)

    # 1. walking / running in 8 directions from the start spot
    for speed in (200, 400):
        for d in range(8):
            fwd = [speed, speed, 0, -speed, -speed, -speed, 0, speed][d]
            side = [0, speed, speed, speed, 0, -speed, -speed, -speed][d]
            seg(st(start), [cmd(16, 0, syaw, fwd, side) for _ in range(60)])
    # 2. strafing with turning, different msec
    seg(st(start), [cmd(MSECS[i % len(MSECS)], 0, syaw + i * 3, 400, 400 if (i // 30) % 2 else -400)
                    for i in range(300)])
    # 3. jumping (hold / release cycles) while running and standing
    seg(st(start), [cmd(16, 0, syaw, 400 if i < 150 else 0, 0, 200 if (i % 40) < 20 else 0) for i in range(300)])
    # 4. crouching, crouch-walking, standing up
    seg(st(start), [cmd(16, 0, syaw, 200 if i > 30 else 0, 0, -200 if i < 150 else 0) for i in range(250)])
    # 5. long random walk with yaw drift from the start
    yaw, pitch = syaw, 0.0
    cmds = []
    for i in range(3000):
        yaw += rng.uniform(-8, 8)
        pitch = max(-80.0, min(80.0, pitch + rng.uniform(-3, 3)))
        r = rng.range(64)
        up = 200 if r < 6 else (-200 if r < 9 else 0)
        cmds.append(cmd(rng.choice(MSECS) if rng.range(4) == 0 else 16, pitch, yaw,
                        rng.choice([400, 400, 200, 0, -200]), rng.choice([0, 0, 200, -200, 400]), up))
    seg(st(start), cmds)
    # 6. random walks from random floor spots (ledges, stairs, slopes)
    for k in range(min(12, len(floor))):
        p = floor[rng.range(len(floor))]
        yaw = rng.uniform(0, 360)
        cmds = []
        for i in range(250):
            yaw += rng.uniform(-4, 4)
            cmds.append(cmd(rng.choice([16, 16, 33, 100]), 0, yaw, 400, rng.choice([0, 0, 0, 200, -200]),
                            200 if rng.range(20) == 0 else 0))
        seg(st(p), cmds)
    # 7. falling: start in the air above floor spots with various velocities
    for k in range(min(8, len(floor))):
        p = floor[rng.range(len(floor))]
        vz = rng.choice([0, -400, 300, -1200])
        seg(st([p[0], p[1], p[2] + rng.uniform(16, 200)], velocity=(rng.range(400) - 200, rng.range(400) - 200, vz)),
            [cmd(rng.choice([16, 50, 100]), 0, rng.uniform(0, 360), rng.choice([0, 400])) for _ in range(100)])
    # 8. water: sink, swim with pitch, swim up (waterjump attempts)
    for k in range(min(10, len(water))):
        p = water[rng.range(len(water))]
        yaw = rng.uniform(0, 360)
        cmds = []
        for i in range(200):
            ph = i // 50
            pitch = [0, -45, 45, -10][ph]
            cmds.append(cmd(16 if i % 7 else 50, pitch, yaw + i, 400 if ph != 0 else 0, 0, 200 if ph in (0, 3) else 0))
        seg(st(p), cmds)
    # water entry: start above water spots and fall in
    for k in range(min(4, len(water))):
        p = water[rng.range(len(water))]
        seg(st([p[0], p[1], p[2] + 96]), [cmd(16, 30, rng.uniform(0, 360), 200, 0, 0) for _ in range(150)])
    # 9. ladders
    for k in range(min(6, len(ladders))):
        p, yaw = ladders[rng.range(len(ladders))]
        seg(st(p), [cmd(16, -45 if i < 120 else 45, yaw, 400, 0, 200 if i % 50 < 25 else 0) for i in range(200)])
    # 10. pm_type variations
    seg(st(start, PM_SPECTATOR), [cmd(rng.choice(MSECS), rng.uniform(-89, 89), rng.uniform(0, 360),
                                      rng.choice([400, -400, 0]), rng.choice([400, 0]), rng.choice([200, -200, 0]))
                                  for _ in range(200)])
    far = [start[0] + 4000, start[1], start[2]]
    seg(st(far, PM_SPECTATOR), [cmd(100, 0, 0, 400, 0, 0) for _ in range(20)])  # noclip outside the world
    for t in (PM_DEAD, PM_GIB, PM_FREEZE):
        seg(st([start[0], start[1], start[2] + 20], t, velocity=(300, -200, 100)),
            [cmd(rng.choice([16, 100]), rng.uniform(-30, 30), rng.uniform(0, 360), 400, 200, 200) for _ in range(80)])
    # 11. timers: teleport, land, waterjump; delta_angles; gravity
    seg(st(start, flags=PMF_TIME_TELEPORT, time=20), [cmd(16, 0, syaw, 400, 0, 200) for _ in range(60)])
    seg(st(start, flags=PMF_TIME_TELEPORT, time=255), [cmd(250, 0, syaw, 400, 0, 0) for _ in range(20)])
    seg(st(start, flags=PMF_TIME_LAND | PMF_ON_GROUND, time=18), [cmd(16, 0, syaw, 0, 0, 200) for _ in range(40)])
    seg(st(start, flags=PMF_TIME_WATERJUMP, time=40, velocity=(0, 0, 350)), [cmd(16, 0, syaw, 400) for _ in range(60)])
    seg(st(start, delta=(0, a2s(90) - 65536 if a2s(90) > 32767 else a2s(90), 0)),
        [cmd(16, 0, i, 400, 0, 0) for i in range(80)])
    for g in (400, 0, 1600):
        seg(st(start, gravity=g), [cmd(16, 0, syaw, 400, 0, 200 if i % 30 < 5 else 0) for i in range(90)])
    # 12. msec extremes, including 0 and 255
    seg(st(start), [cmd(m, 0, syaw + 10 * i, 400, 0, 200 if i % 3 == 0 else 0)
                    for i, m in enumerate([0, 1, 255, 250, 1, 0, 128, 64, 3, 200] * 6)])
    return {"map": mapname, "airaccelerate": airaccelerate, "steps": steps}


def main():
    exe, basedir, mapname, outp = sys.argv[1:5]
    air = float(sys.argv[5]) if len(sys.argv) > 5 else 0
    ents = read_entities(basedir, mapname)
    seed = {"demo1": 101, "demo2": 202, "demo3": 303}.get(mapname, 1) + (7 if air else 0)
    sc = gen(Oracle(exe, basedir, mapname), ents, mapname, seed, int(air) if air == int(air) else air)
    os.makedirs(os.path.dirname(os.path.abspath(outp)), exist_ok=True)
    with open(outp, "w") as f:
        f.write('{"map":%s,"airaccelerate":%s,"steps":[\n' % (json.dumps(sc["map"]), sc["airaccelerate"]))
        f.write(",\n".join(json.dumps(s, separators=(",", ":")) for s in sc["steps"]))
        f.write("\n]}\n")
    print("%s: %d steps" % (mapname, len(sc["steps"])), file=sys.stderr)


if __name__ == "__main__":
    main()
