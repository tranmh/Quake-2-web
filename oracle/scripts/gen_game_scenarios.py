#!/usr/bin/env python3
"""Write the committed game scenarios (fixtures/scenarios/game/*.json). Nothing here is derived from game data.

Usage: gen_game_scenarios.py <outdir>

Positions in the CTF entity additions are written as placeholders ("$dmN" = origin of the N-th
info_player_deathmatch in the map's own entity string); tools/gen-fixtures.sh materializes them
(oracle/scripts/prep_game_scenario.py) into a plain "entstring_override" before running the oracle.
"""
import json
import os
import sys


def ui(name, skin="male/grunt", hand=0, fov=90):
    return {"userinfo": "\\name\\%s\\skin\\%s\\hand\\%d\\fov\\%d" % (name, skin, hand, fov)}


def a2s(a):
    v = int(a * 65536 / 360) & 65535
    return v - 65536 if v >= 32768 else v


def cmd(pitch=0.0, yaw=0.0, fwd=0, side=0, up=0, buttons=0, msec=100, impulse=0):
    return {"msec": msec, "buttons": buttons, "angles": [a2s(pitch), a2s(yaw), 0], "forwardmove": fwd,
            "sidemove": side, "upmove": up, "impulse": impulse, "lightlevel": 128}


def base(map_, frames, cvars, clients, schedule, module="baseq2", seed=1, extra=None):
    d = {"map": map_, "module": module, "entstring_override": None, "seed": seed, "cvars": cvars,
         "clients": clients, "frames": frames, "schedule": schedule}
    if extra:
        d.update(extra)
    return d


SP = {"skill": "1", "deathmatch": "0", "coop": "0", "maxclients": "1"}


def sp_walk():
    """Scripted single-player walk: move/turn, `give all`, cycle and fire weapons, jump/crouch."""
    s = []
    yaw = 0.0
    f = 1
    for _ in range(20):                       # stand still
        s.append({"frame": f, "client": 0, "cmd": cmd(0, yaw)}); f += 1
    for i in range(80):                       # walk forward, gently turning (item pickups near the start)
        yaw += 1.5 if (i // 20) % 2 == 0 else -1.5
        s.append({"frame": f, "client": 0, "cmd": cmd(0, yaw, 400, 0, 0)}); f += 1
    s.append({"frame": f, "client": 0, "command": "give all"})
    weapons = ["Blaster", "Shotgun", "Super Shotgun", "Machinegun", "Chaingun", "Grenade Launcher",
               "Rocket Launcher", "HyperBlaster", "Railgun", "BFG10K"]
    for w in weapons:                         # switch, then fire for a while while strafing
        s.append({"frame": f, "client": 0, "command": "use " + w})
        for i in range(30):
            pitch = -10.0 + (i % 10)
            s.append({"frame": f, "client": 0,
                      "cmd": cmd(pitch, yaw + (i - 15) * 2, 0, 200 if (i // 10) % 2 else -200, 0,
                                 1 if i >= 8 else 0)})
            f += 1
    s.append({"frame": f, "client": 0, "command": "weapnext"})
    s.append({"frame": f + 1, "client": 0, "command": "weapprev"})
    s.append({"frame": f + 2, "client": 0, "command": "invnext"})
    s.append({"frame": f + 3, "client": 0, "command": "inven"})
    s.append({"frame": f + 4, "client": 0, "command": "inven"})
    while f <= 600:                           # run around: jumps, crouches, turns
        i = f
        up = 200 if i % 25 < 3 else (-200 if i % 60 > 50 else 0)
        yaw += 6 if (i // 40) % 2 else -4
        s.append({"frame": f, "client": 0, "cmd": cmd(0, yaw, 400, 0, up, 1 if i % 50 < 5 else 0)}); f += 1
    return base("demo1", 600, dict(SP), [ui("player")], s)


def rw(client, seed, start=1, until=None):
    d = {"seed": seed, "until": until} if until else {"seed": seed}
    return {"frame": start, "client": client, "random_walk": d}


def dm4():
    cl = [ui("p0"), ui("p1", "female/athena"), ui("p2", "male/cipher"), ui("p3", "female/jezebel")]
    s = []
    for c in range(4):
        s.append({"frame": 3 + c, "client": c, "command": "give all"})
        s.append(rw(c, 1000 + c, 10, 601))
    guns = ["Rocket Launcher", "Railgun", "Shotgun", "HyperBlaster", "Chaingun", "Grenade Launcher", "Machinegun"]
    for k, f in enumerate(range(12, 600, 45)):
        for c in range(4):
            s.append({"frame": f + c, "client": c, "command": "use " + guns[(k + c) % len(guns)]})
    s.append({"frame": 300, "client": 1, "command": "kill"})
    s.append({"frame": 305, "client": 2, "command": "say hello"})
    s.append({"frame": 400, "client": 0, "command": "score"})
    return base("demo1", 600, {"skill": "1", "deathmatch": "1", "coop": "0", "maxclients": "4", "cheats": "1",
                               "dmflags": "0", "fraglimit": "0", "timelimit": "0"}, cl, s, seed=4)


def coop2():
    cl = [ui("c0"), ui("c1", "female/athena")]
    s = [rw(0, 2000, 5, 601), rw(1, 2001, 5, 601), {"frame": 200, "client": 0, "command": "give shotgun"},
         {"frame": 201, "client": 0, "command": "use shotgun"}]
    return base("demo1", 600, {"skill": "1", "deathmatch": "0", "coop": "1", "maxclients": "2"}, cl, s, seed=2)


CTF_APPEND = (
    '{\n"classname" "info_player_team1"\n"origin" "$dm0"\n}\n'
    '{\n"classname" "info_player_team1"\n"origin" "$dm1"\n}\n'
    '{\n"classname" "info_player_team2"\n"origin" "$dm2"\n}\n'
    '{\n"classname" "info_player_team2"\n"origin" "$dm3"\n}\n'
    '{\n"classname" "item_flag_team1"\n"origin" "$dm4"\n}\n'
    '{\n"classname" "item_flag_team2"\n"origin" "$dm5"\n}\n')


def ctf4():
    cl = [ui("r0"), ui("b0", "female/athena"), ui("r1", "male/cipher"), ui("b1", "female/jezebel")]
    s = []
    for c in range(4):
        s.append({"frame": 2, "client": c, "command": "team " + ("red" if c % 2 == 0 else "blue")})
        s.append(rw(c, 3000 + c, 6, 601))
    s.append({"frame": 100, "client": 0, "command": "give all"})
    s.append({"frame": 101, "client": 0, "command": "use Rocket Launcher"})
    s.append({"frame": 250, "client": 3, "command": "team red"})
    s.append({"frame": 400, "client": 1, "command": "kill"})
    return base("demo1", 600, {"skill": "1", "deathmatch": "1", "coop": "0", "maxclients": "4", "ctf": "1",
                               "cheats": "1", "dmflags": "0", "fraglimit": "0", "timelimit": "0",
                               "capturelimit": "0"},
                cl, s, module="ctf", seed=5, extra={"entstring_append": CTF_APPEND})


def main():
    out = sys.argv[1]
    os.makedirs(out, exist_ok=True)
    sc = {
        "demo1_sp_idle": base("demo1", 300, dict(SP), [ui("player")], []),
        "demo1_sp_walk": sp_walk(),
        "demo1_sp_random": base("demo1", 1200, dict(SP), [ui("player")], [rw(0, 7, 1, 1201)], seed=7),
        "demo2_sp_random": base("demo2", 600, dict(SP, skill="2"), [ui("player")], [rw(0, 8, 1, 601)], seed=8),
        "demo3_sp_random": base("demo3", 600, dict(SP, skill="0"), [ui("player", "female/athena", 1)],
                                [rw(0, 9, 1, 601)], seed=9),
        "demo1_dm4": dm4(),
        "demo1_coop2": coop2(),
        "ctf_demo1": ctf4(),
    }
    for name, d in sc.items():
        with open(os.path.join(out, name + ".json"), "w") as f:
            # one schedule entry per line keeps diffs readable
            sched = d.pop("schedule")
            body = json.dumps(d, separators=(",", ":"))[:-1]
            f.write(body + ',\n"schedule":[\n' + ",\n".join(json.dumps(e, separators=(",", ":")) for e in sched)
                    + "\n]}\n")


if __name__ == "__main__":
    main()
