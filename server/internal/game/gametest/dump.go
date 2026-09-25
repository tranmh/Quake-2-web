package gametest

import (
	"quake2web/server/internal/game"
	. "quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/shared"
)

// Record is one JSON object of a dump (key -> float32 | int64 | string | nil | []any | Record).
type Record = map[string]any

// FrameDump is our side of one fixture line (oracle/src/game_dump.c).
type FrameDump struct {
	Frame     int
	Level     Record
	RandCalls int
	Inputs    []any
	Edicts    map[int]Record // every inuse edict
	Clients   []any
	Events    []any
}

type levelGetter interface{ Level() *game.LevelLocals }

func i64[T ~int | ~int32 | ~int16 | ~uint8 | ~uint32](v T) int64 { return int64(v) }

func esRecord(s *shared.EntityState) Record {
	return Record{
		"number": i64(s.Number), "origin": vecAny(s.Origin), "angles": vecAny(s.Angles),
		"old_origin": vecAny(s.OldOrigin), "modelindex": i64(s.ModelIndex), "modelindex2": i64(s.ModelIndex2),
		"modelindex3": i64(s.ModelIndex3), "modelindex4": i64(s.ModelIndex4), "frame": i64(s.Frame),
		"skinnum": i64(s.SkinNum), "effects": i64(s.Effects), "renderfx": i64(s.RenderFX),
		"solid": i64(s.Solid), "sound": i64(s.Sound), "event": i64(s.Event),
	}
}

func strOrNull(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func edictRecord(e *game.Edict) Record {
	r := Record{
		"n": int64(e.Index), "classname": strOrNull(e.Classname), "s": esRecord(&e.S),
		"solid": i64(e.Solid), "svflags": i64(e.SVFlags), "mins": vecAny(e.Mins), "maxs": vecAny(e.Maxs),
		"health": i64(e.Health), "movetype": i64(e.Movetype), "flags": i64(e.Flags), "nextthink": e.Nextthink,
		"velocity": vecAny(e.Velocity), "avelocity": vecAny(e.Avelocity),
		"groundentity": entNum(e.Groundentity), "enemy": entNum(e.Enemy), "owner": entNum(e.Owner),
		"takedamage": i64(e.Takedamage), "deadflag": i64(e.Deadflag), "waterlevel": i64(e.Waterlevel),
		"spawnflags": i64(e.Spawnflags), "think": nil, "aiflags": nil, "currentmove": nil,
	}
	if e.Think != nil {
		r["think"] = e.Think.Name()
	}
	if e.SVFlags&SVF_MONSTER != 0 {
		r["aiflags"] = i64(e.Monsterinfo.Aiflags)
		if e.Monsterinfo.Currentmove != nil {
			r["currentmove"] = e.Monsterinfo.Currentmove.Name
		}
	}
	return r
}

func short3(v [3]int16) []any { return []any{int64(v[0]), int64(v[1]), int64(v[2])} }

func psRecord(ps *shared.PlayerState) Record {
	stats := make([]any, len(ps.Stats))
	for i, s := range ps.Stats {
		stats[i] = int64(s)
	}
	pm := &ps.PMove
	return Record{
		"pmove": Record{"pm_type": i64(pm.PmType), "origin": short3(pm.Origin), "velocity": short3(pm.Velocity),
			"pm_flags": i64(pm.PmFlags), "pm_time": i64(pm.PmTime), "gravity": i64(pm.Gravity),
			"delta_angles": short3(pm.DeltaAngles)},
		"viewangles": vecAny(ps.ViewAngles), "viewoffset": vecAny(ps.ViewOffset), "kick_angles": vecAny(ps.KickAngles),
		"gunangles": vecAny(ps.GunAngles), "gunoffset": vecAny(ps.GunOffset), "gunindex": i64(ps.GunIndex),
		"gunframe": i64(ps.GunFrame), "blend": []any{ps.Blend[0], ps.Blend[1], ps.Blend[2], ps.Blend[3]},
		"fov": ps.Fov, "rdflags": i64(ps.RDFlags), "stats": stats,
	}
}

// Dump captures the current state (after the frame ran).
func (s *Server) Dump(frame int) *FrameDump {
	ge := s.E.Ge
	d := &FrameDump{Frame: frame, RandCalls: s.RandCalls(), Inputs: s.Inputs, Edicts: map[int]Record{}}
	if d.Inputs == nil {
		d.Inputs = []any{}
	}
	if lg, ok := ge.(levelGetter); ok {
		l := lg.Level()
		d.Level = Record{"framenum": i64(l.Framenum), "time": l.Time, "killed_monsters": i64(l.KilledMonsters),
			"total_monsters": i64(l.TotalMonsters), "found_secrets": i64(l.FoundSecrets), "total_secrets": i64(l.TotalSecrets)}
	}
	edicts := ge.Edicts()
	for n := 0; n < ge.NumEdicts() && n < len(edicts); n++ {
		if edicts[n].InUse {
			d.Edicts[n] = edictRecord(&edicts[n])
		}
	}
	d.Clients = make([]any, len(s.Sc.Clients))
	for i := range s.Sc.Clients {
		e := &edicts[1+i]
		cl := e.Client
		if cl == nil {
			d.Clients[i] = nil
			continue
		}
		inv := make([]any, len(cl.Pers.Inventory))
		for k, v := range cl.Pers.Inventory {
			inv[k] = int64(v)
		}
		var weapon any
		if cl.Pers.Weapon != nil && cl.Pers.Weapon.Classname != "" {
			weapon = cl.Pers.Weapon.Classname
		}
		d.Clients[i] = Record{"ps": psRecord(&cl.PS), "inventory": inv, "health": i64(e.Health),
			"score": i64(cl.Resp.Score), "weapon": weapon}
	}
	d.Events = make([]any, len(s.E.Events))
	for i, ev := range s.E.Events {
		d.Events[i] = map[string]any(ev)
	}
	return d
}
