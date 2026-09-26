#!/usr/bin/env python3
"""Write the committed synthetic CTF game scenarios (fixtures/scenarios/game/ctf_*.json, except ctf_demo1 which is
written by gen_game_scenarios.py). They run on the ctf module (oracle_game_ctf) and exercise the ctf/g_ctf.c code
paths with scripted inputs: flag take/capture/drop/return, capturelimit, grapple, techs, team switching and menus,
match mode with ready/notready/ghosts, admin/map elections and votes.

Usage: gen_ctf_scenarios.py <outdir>

Entity origins are plain coordinates in the demo1 start area (no game data is committed); the map's own entity
string is kept (entstring_append) with its monster blocks stripped. Nothing here is derived from game data.
"""
import json
import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from gen_game_scenarios import ui, cmd, rw, base  # noqa: E402

BUTTON_ATTACK = 1
BUTTON_ANY = 128

CTF = {"skill": "1", "deathmatch": "1", "coop": "0", "maxclients": "4", "ctf": "1", "cheats": "1",
       "dmflags": "0", "fraglimit": "0", "timelimit": "0", "capturelimit": "0"}


def ent(classname, origin, angle=None, **kv):
    s = '{\n"classname" "%s"\n"origin" "%s"\n' % (classname, origin)
    if angle is not None:
        s += '"angle" "%d"\n' % angle
    for k, v in kv.items():
        s += '"%s" "%s"\n' % (k, v)
    return s + "}\n"


# demo1 start area: the coop spots are a row at y=-224 facing north (angle 90)
RED_SPAWN = "32 -288 24"
BLUE_SPAWN = "168 -288 24"
START_ENTS = (ent("info_player_team1", RED_SPAWN, 90) + ent("info_player_team2", BLUE_SPAWN, 90) +
              ent("item_flag_team1", RED_SPAWN) + ent("item_flag_team2", "32 -128 24"))


def hold(sched, client, first, last, c):
    for f in range(first, last + 1):
        sched.append({"frame": f, "client": client, "cmd": c})


def scenario(frames, cvars, clients, sched, append, seed):
    return base("demo1", frames, cvars, clients, sched, module="ctf", seed=seed,
                extra={"entstring_strip": ["monster_"], "entstring_append": append})


def flags(capturelimit="0", frames=900):
    """red r0 takes the blue flag, captures it at the red base, takes it again, suicides (flag dropped), the flag
    auto-returns; blue b0 walks over a dropped red flag carried by r1..."""
    cl = [ui("r0"), ui("b0", "female/athena"), ui("r1", "male/cipher")]
    s = [{"frame": 2, "client": 0, "command": "team red"},
         {"frame": 2, "client": 1, "command": "team blue"}]
    idle = cmd()
    # r0: north to the blue flag, back south to the red flag (capture)
    hold(s, 0, 20, 29, cmd(fwd=400))
    hold(s, 0, 30, 32, idle)
    hold(s, 0, 33, 45, cmd(yaw=180, fwd=400))
    hold(s, 0, 46, 60, idle)
    s.append({"frame": 61, "client": 0, "command": "score"})
    # take it again and die with it: the flag is dropped, then auto-returns after 30 s
    hold(s, 0, 70, 79, cmd(fwd=400))
    s.append({"frame": 85, "client": 0, "command": "drop Blue Flag"})
    s.append({"frame": 90, "client": 0, "command": "kill"})
    s.append({"frame": 95, "client": 0, "command": "score"})
    hold(s, 0, 110, 111, cmd(buttons=BUTTON_ATTACK | BUTTON_ANY))
    # b0 walks over the dropped blue flag (lands near 32 -54): returned to the blue base
    hold(s, 1, 100, 115, cmd(yaw=30.2, fwd=400))
    # r1 joins red late, b0 random walks
    s.append({"frame": 150, "client": 2, "command": "team red"})
    s.append(rw(1, 4101, 160, frames + 1))
    # r0 takes the blue flag again and b0/r1 fight around it
    hold(s, 0, 420, 429, cmd(fwd=400))
    s.append(rw(0, 4100, 430, frames + 1))
    s.append(rw(2, 4102, 430, frames + 1))
    s.append({"frame": 431, "client": 0, "command": "give all"})
    s.append({"frame": 431, "client": 1, "command": "give all"})
    s.append({"frame": 431, "client": 2, "command": "give all"})
    s.append({"frame": 432, "client": 1, "command": "use Rocket Launcher"})
    s.append({"frame": 432, "client": 2, "command": "use Rocket Launcher"})
    cv = dict(CTF, capturelimit=capturelimit)
    return scenario(frames, cv, cl, s, START_ENTS, 21)


def capturelimit():
    """capturelimit 1: the capture ends the level (intermission, CTFCalcScores, blinking headers, ExitLevel)."""
    cl = [ui("r0"), ui("b0", "female/athena")]
    s = [{"frame": 2, "client": 0, "command": "team red"},
         {"frame": 2, "client": 1, "command": "team blue"}]
    hold(s, 0, 20, 29, cmd(fwd=400))
    hold(s, 0, 33, 45, cmd(yaw=180, fwd=400))
    # leave the intermission (5 s + any button)
    hold(s, 1, 150, 152, cmd(buttons=BUTTON_ANY))
    return scenario(200, dict(CTF, capturelimit="1"), cl, s, START_ENTS, 22)


def grapple():
    """grapple fire/pull/hang/release, grapple a player, switch weapons while grappled."""
    cl = [ui("r0"), ui("b0", "female/athena")]
    s = [{"frame": 2, "client": 0, "command": "team red"},
         {"frame": 2, "client": 1, "command": "team blue"},
         {"frame": 3, "client": 0, "command": "use Grapple"}]
    atk = BUTTON_ATTACK | BUTTON_ANY
    # hook the wall ahead, get pulled, hang, release
    hold(s, 0, 12, 40, cmd(buttons=atk))
    hold(s, 0, 41, 45, cmd())
    # hook up at the ceiling
    hold(s, 0, 50, 70, cmd(pitch=-60.0, buttons=atk))
    hold(s, 0, 71, 75, cmd())
    # hook the floor behind, switch weapon while grappled
    hold(s, 0, 80, 90, cmd(pitch=30.0, yaw=180, buttons=atk))
    s.append({"frame": 88, "client": 0, "command": "use Blaster"})
    hold(s, 0, 91, 100, cmd(pitch=30.0, yaw=180))
    s.append({"frame": 105, "client": 0, "command": "weaplast"})
    # grapple the blue player standing to the east
    hold(s, 0, 115, 140, cmd(yaw=-90, buttons=atk))
    hold(s, 0, 141, 150, cmd())
    s.append({"frame": 151, "client": 0, "command": "give all"})
    s.append({"frame": 152, "client": 0, "command": "give Time Accel"})
    s.append({"frame": 153, "client": 0, "command": "use Grapple"})
    # hasted grapple
    hold(s, 0, 160, 200, cmd(yaw=90, pitch=-20.0, buttons=atk))
    s.append(rw(0, 4200, 201, 401))
    s.append(rw(1, 4201, 60, 401))
    return scenario(400, dict(CTF), cl, s, START_ENTS, 23)


# three spawn spots per team (SelectCTFSpawnPoint skips the two closest to other players), flags away from the
# start area
TECH_ENTS = (ent("info_player_team1", "32 -224 24", 90) + ent("info_player_team1", "96 -224 24", 90) +
             ent("info_player_team1", "32 -288 24", 90) +
             ent("info_player_team2", "168 -224 24", 90) + ent("info_player_team2", "168 -288 24", 90) +
             ent("info_player_team2", "100 -288 24", 90) +
             ent("item_flag_team1", "-392 840 -104") + ent("item_flag_team2", "480 272 -40"))


def techs():
    """tech spawn at the deathmatch spots, give/pickup/drop, the one-tech rule, strength/resistance/haste/regen
    effects in a fight, dead drop with random velocity, tech respawn (TechThink after 60 s)."""
    cl = [ui("r0"), ui("b0", "female/athena"), ui("r1", "male/cipher"), ui("b1", "female/jezebel")]
    s = []
    for c in range(4):
        s.append({"frame": 2 + c, "client": c, "command": "team " + ("red" if c % 2 == 0 else "blue")})
    s.append({"frame": 30, "client": 0, "command": "give Power Amplifier"})
    s.append({"frame": 30, "client": 1, "command": "give Disruptor Shield"})
    s.append({"frame": 30, "client": 2, "command": "give Time Accel"})
    s.append({"frame": 30, "client": 3, "command": "give AutoDoc"})
    s.append({"frame": 31, "client": 0, "command": "give AutoDoc"})  # already has one
    s.append({"frame": 32, "client": 0, "command": "say_team I have %t, %h, %a, %w at %l, see %n"})
    for c in range(4):
        s.append({"frame": 33, "client": c, "command": "give weapons"})
        s.append({"frame": 33, "client": c, "command": "give ammo"})
    s.append({"frame": 34, "client": 0, "command": "use Railgun"})
    s.append({"frame": 34, "client": 1, "command": "use Rocket Launcher"})
    s.append({"frame": 34, "client": 2, "command": "use HyperBlaster"})
    s.append({"frame": 34, "client": 3, "command": "use Chaingun"})
    s.append({"frame": 40, "client": 3, "command": "drop tech"})
    s.append({"frame": 41, "client": 3, "command": "drop tech"})
    s.append({"frame": 42, "client": 1, "command": "id"})
    for c in range(4):
        s.append(rw(c, 4300 + c, 45, 901))
    s.append({"frame": 300, "client": 2, "command": "kill"})
    s.append({"frame": 500, "client": 0, "command": "drop tech"})
    return scenario(900, dict(CTF), cl, s, TECH_ENTS, 24)


def teams():
    """team command variants, menus (inven/invnext/invprev/invuse/putaway), join by menu, chase camera,
    observer, say_team/steam, playerlist, stats, id view, credits menu."""
    cl = [ui("r0"), ui("b0", "female/athena"), ui("x0", "male/cipher")]
    s = [{"frame": 2, "client": 0, "command": "team"},
         {"frame": 3, "client": 0, "command": "team red"},
         {"frame": 4, "client": 0, "command": "team red"},
         {"frame": 5, "client": 0, "command": "team green"},
         # b0 joins blue through the join menu (the cursor starts on "Join Red Team")
         {"frame": 6, "client": 1, "command": "invnext"},
         {"frame": 7, "client": 1, "command": "invnext"},
         {"frame": 8, "client": 1, "command": "invprev"},
         {"frame": 20, "client": 1, "command": "invuse"},
         # x0: chase camera through the menu
         {"frame": 10, "client": 2, "command": "invnext"},
         {"frame": 11, "client": 2, "command": "invnext"},
         {"frame": 13, "client": 2, "command": "invuse"},
         {"frame": 30, "client": 2, "command": "invnext"},
         {"frame": 40, "client": 2, "command": "invprev"},
         {"frame": 50, "client": 2, "command": "score"},
         {"frame": 60, "client": 2, "command": "score"},
         {"frame": 70, "client": 2, "command": "inven"},
         {"frame": 75, "client": 2, "command": "inven"},
         {"frame": 76, "client": 2, "command": "inven"},
         {"frame": 77, "client": 2, "command": "invnext"},
         {"frame": 78, "client": 2, "command": "invnext"},
         {"frame": 79, "client": 2, "command": "invuse"},  # leave chase camera
         {"frame": 90, "client": 2, "command": "inven"},
         {"frame": 91, "client": 2, "command": "invprev"},
         {"frame": 94, "client": 2, "command": "invuse"},  # credits
         {"frame": 100, "client": 2, "command": "invuse"},  # return to main
         {"frame": 110, "client": 2, "command": "putaway"},
         {"frame": 111, "client": 2, "command": "kill"},
         {"frame": 112, "client": 2, "command": "stats"},
         {"frame": 113, "client": 2, "command": "playerlist"},
         {"frame": 114, "client": 2, "command": "help"},
         {"frame": 120, "client": 0, "command": "say_team %l %L %a %h %t %w %n %x 100%"},
         {"frame": 121, "client": 1, "command": "steam \"hello %h\""},
         {"frame": 122, "client": 0, "command": "id"},
         {"frame": 130, "client": 2, "command": "team blue"},
         {"frame": 200, "client": 1, "command": "team red"},
         {"frame": 260, "client": 0, "command": "observer"},
         {"frame": 261, "client": 0, "command": "observer"},
         {"frame": 262, "client": 0, "command": "kill"},
         {"frame": 270, "client": 0, "command": "invnext"},
         {"frame": 271, "client": 0, "command": "invnext"},
         {"frame": 272, "client": 0, "command": "invuse"},  # chase camera
         {"frame": 300, "client": 0, "command": "invnext"},
         {"frame": 320, "client": 0, "command": "score"},
         {"frame": 340, "client": 0, "command": "inven"},
         {"frame": 341, "client": 0, "command": "invuse"},  # join red from the menu
         {"frame": 350, "client": 0, "command": "wave 1"},
         ]
    s.append(rw(1, 4400, 25, 400))
    s.append(rw(2, 4401, 135, 400))
    s.append(rw(0, 4402, 345, 400))
    return scenario(400, dict(CTF), cl, s, START_ENTS, 25)


def match():
    """admin election won by vote, admin menu -> match mode, ready/notready, match start with ghost codes, match
    timer configstrings, ghost/stats commands, match end, intermission and CTFNextMap back to match setup."""
    cl = [ui("r0"), ui("b0", "female/athena"), ui("x0", "male/cipher")]
    s = [{"frame": 2, "client": 0, "command": "team red"},
         {"frame": 2, "client": 1, "command": "team blue"},
         {"frame": 10, "client": 0, "command": "admin"},
         {"frame": 11, "client": 0, "command": "yes"},  # can't vote for yourself
         {"frame": 12, "client": 2, "command": "yes"},  # 1 vote needed: r0 becomes admin
         {"frame": 13, "client": 2, "command": "yes"},  # no election
         {"frame": 20, "client": 0, "command": "admin"},  # admin menu
         {"frame": 21, "client": 0, "command": "invnext"},
         {"frame": 22, "client": 0, "command": "invuse"},  # switch to match mode
         {"frame": 30, "client": 0, "command": "ready"},  # no team yet
         {"frame": 31, "client": 0, "command": "team red"},
         {"frame": 31, "client": 1, "command": "team blue"},
         {"frame": 32, "client": 0, "command": "ready"},
         {"frame": 33, "client": 0, "command": "ready"},
         {"frame": 34, "client": 0, "command": "stats"},
         {"frame": 34, "client": 1, "command": "playerlist"},
         {"frame": 35, "client": 1, "command": "ready"},
         {"frame": 40, "client": 1, "command": "notready"},
         {"frame": 41, "client": 1, "command": "notready"},
         {"frame": 42, "client": 1, "command": "ready"},
         {"frame": 60, "client": 2, "command": "ghost 12345"},
         {"frame": 61, "client": 2, "command": "ghost"},
         {"frame": 62, "client": 2, "command": "team red"},  # can't change teams in a match
         {"frame": 63, "client": 2, "command": "inven"},
         {"frame": 64, "client": 2, "command": "invuse"},  # match is locked
         {"frame": 70, "client": 0, "command": "stats"},
         {"frame": 71, "client": 0, "command": "give all"},
         {"frame": 72, "client": 0, "command": "use Rocket Launcher"},
         {"frame": 200, "client": 1, "command": "stats"},
         {"frame": 201, "client": 0, "command": "boot 3"},
         {"frame": 202, "client": 0, "command": "boot 9"},
         ]
    s.append(rw(0, 4500, 75, 520))
    s.append(rw(1, 4501, 75, 520))
    hold(s, 0, 530, 535, cmd(buttons=BUTTON_ANY))
    # admin settings menu: change the match length and weapons stay, apply
    s.append({"frame": 560, "client": 0, "command": "admin"})
    s.append({"frame": 561, "client": 0, "command": "invuse"})  # Settings (the cursor starts on Apply)
    s.append({"frame": 562, "client": 0, "command": "invnext"})  # Cancel
    s.append({"frame": 563, "client": 0, "command": "invnext"})  # Match Len
    s.append({"frame": 564, "client": 0, "command": "invuse"})
    for f in range(565, 568):
        s.append({"frame": f, "client": 0, "command": "invnext"})
    s.append({"frame": 568, "client": 0, "command": "invuse"})  # Weapons Stay
    for f in range(569, 574):
        s.append({"frame": f, "client": 0, "command": "invnext"})
    s.append({"frame": 575, "client": 0, "command": "invuse"})  # Apply
    s.append({"frame": 580, "client": 0, "command": "admin"})
    s.append({"frame": 581, "client": 0, "command": "invnext"})
    s.append({"frame": 582, "client": 0, "command": "invuse"})  # Force start match
    return scenario(600, dict(CTF, competition="1", matchstarttime="3", matchtime="0.5", matchsetuptime="1"),
                    cl, s, START_ENTS, 26)


def vote():
    """elections: warp list/unknown level, map election voted no/timeout, match request from the join menu,
    admin_password, warp as admin (forcemap intermission)."""
    cl = [ui("r0"), ui("b0", "female/athena"), ui("x0", "male/cipher"), ui("x1", "female/jezebel")]
    s = [{"frame": 2, "client": 0, "command": "team red"},
         {"frame": 2, "client": 1, "command": "team blue"},
         {"frame": 2, "client": 3, "command": "team blue"},
         {"frame": 5, "client": 0, "command": "warp"},
         {"frame": 6, "client": 0, "command": "warp base1"},
         {"frame": 7, "client": 0, "command": "warp q2ctf2"},
         {"frame": 8, "client": 1, "command": "warp q2ctf3"},  # election in progress
         {"frame": 9, "client": 1, "command": "no"},
         {"frame": 10, "client": 1, "command": "no"},
         {"frame": 11, "client": 2, "command": "no"},
         # the election times out after 20 s; x0 still has the join menu open
         {"frame": 241, "client": 2, "command": "invprev"},
         {"frame": 250, "client": 2, "command": "invuse"},  # request match
         {"frame": 251, "client": 0, "command": "yes"},
         {"frame": 252, "client": 1, "command": "yes"},
         {"frame": 260, "client": 3, "command": "admin wrong"},
         {"frame": 261, "client": 3, "command": "admin secret"},
         {"frame": 262, "client": 3, "command": "admin"},
         {"frame": 263, "client": 3, "command": "invnext"},
         {"frame": 264, "client": 3, "command": "invuse"},  # force start match
         {"frame": 300, "client": 3, "command": "warp q2ctf5"},
         ]
    s.append(rw(0, 4600, 20, 300))
    s.append(rw(1, 4601, 20, 300))
    return scenario(360, dict(CTF, competition="1", admin_password="secret", electpercentage="50"),
                    cl, s, START_ENTS, 27)


def write(out, name, d):
    with open(os.path.join(out, name + ".json"), "w") as f:
        sched = d.pop("schedule")
        body = json.dumps(d, separators=(",", ":"))[:-1]
        f.write(body + ',\n"schedule":[\n' + ",\n".join(json.dumps(e, separators=(",", ":")) for e in sched)
                + "\n]}\n")


def main():
    out = sys.argv[1]
    os.makedirs(out, exist_ok=True)
    sc = {
        "ctf_flags": flags(),
        "ctf_capturelimit": capturelimit(),
        "ctf_grapple": grapple(),
        "ctf_techs": techs(),
        "ctf_teams": teams(),
        "ctf_match": match(),
        "ctf_vote": vote(),
    }
    for name, d in sc.items():
        write(out, name, d)


if __name__ == "__main__":
    main()
