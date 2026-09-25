#!/usr/bin/env python3
"""Materialize a committed game scenario for the oracle.

Usage: prep_game_scenario.py <basedir> <in.json> <out.json>

If the scenario has "entstring_append", the map's own entity string (read from pak0.pak) is taken, placeholders
"$dmN" in the appended text are replaced by the origin of the N-th info_player_deathmatch entity, and the result
becomes "entstring_override" (the key "entstring_append" is removed). Other scenarios are copied unchanged.
The output is derived from game data and lives under fixtures/generated (never committed).
"""
import json
import os
import re
import struct
import sys


def entity_string(basedir, mapname):
    with open(os.path.join(basedir, "baseq2", "pak0.pak"), "rb") as f:
        _, ofs, ln = struct.unpack("<4sii", f.read(12))
        f.seek(ofs)
        for _ in range(ln // 64):
            name, pos, size = struct.unpack("<56sii", f.read(64))
            if name.split(b"\0")[0].decode() == "maps/%s.bsp" % mapname:
                f.seek(pos)
                bsp = f.read(size)
                eofs, elen = struct.unpack_from("<ii", bsp, 8)
                return bsp[eofs:eofs + elen].split(b"\0")[0].decode("latin-1")
    raise SystemExit("map %s not found" % mapname)


def main():
    basedir, inp, outp = sys.argv[1:4]
    with open(inp) as f:
        sc = json.load(f)
    app = sc.pop("entstring_append", None)
    if app is not None:
        ents = entity_string(basedir, sc["map"])
        dm = []
        for block in re.findall(r"\{([^}]*)\}", ents):
            kv = dict(re.findall(r'"([^"]*)"\s*"([^"]*)"', block))
            if kv.get("classname") == "info_player_deathmatch" and "origin" in kv:
                dm.append(kv["origin"])

        def sub(m):
            n = int(m.group(1))
            if n >= len(dm):
                raise SystemExit("scenario needs $dm%d but map has %d deathmatch spots" % (n, len(dm)))
            return dm[n]
        sc["entstring_override"] = ents + re.sub(r"\$dm(\d+)", sub, app)
    os.makedirs(os.path.dirname(os.path.abspath(outp)), exist_ok=True)
    with open(outp, "w") as f:
        json.dump(sc, f, separators=(",", ":"))
        f.write("\n")


if __name__ == "__main__":
    main()
