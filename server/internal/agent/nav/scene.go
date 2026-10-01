package nav

import (
	"encoding/json"
	"fmt"
	"hash/fnv"

	"quake2web/server/internal/agent/mapdata"
	"quake2web/server/internal/qcommon/shared"
)

// SceneDigest identifies what the builder takes from the map data of one
// skill besides the BSP collision model: the movers with their poses, the
// triggers, lasers, spawn points, items and the parsed entities (16 hex
// digits of FNV-1a 64 over their JSON). A graph records the digest of each
// skill it was built for (Graph.Scene), so a change in how mapdata derives
// them makes Graph.Matches fail and the graph is rebuilt, without a manual
// BuildVersion bump.
func SceneDigest(md *mapdata.Map) string {
	type ent struct {
		Index                    int
		Classname, Model         string
		Origin, Angles           shared.Vec3
		Spawnflags               int32
		Target, Targetname, Team string
		Killtarget, Deathtarget  string
		Wait, Delay, Speed       float32
		Lip, Health, Dmg         int32
		Inhibited, Freed         bool
	}
	ents := make([]ent, len(md.Entities))
	for i := range md.Entities {
		e := &md.Entities[i]
		ents[i] = ent{e.Index, e.Classname, e.Model, e.Origin, e.Angles, e.Spawnflags, e.Target, e.Targetname, e.Team,
			e.Killtarget, e.Deathtarget, e.Wait, e.Delay, e.Speed, e.Lip, e.Health, e.Dmg, e.Inhibited, e.Freed}
	}
	b, err := json.Marshal(struct {
		Skill    int
		Entities []ent
		Movers   []mapdata.Mover
		Triggers []mapdata.Trigger
		Lasers   []mapdata.Laser
		Spawns   []mapdata.Spawn
		Items    []mapdata.Item
	}{md.Skill, ents, md.Movers, md.Triggers, md.Lasers, md.Spawns, md.Items})
	if err != nil {
		return "unencodable: " + err.Error()
	}
	h := fnv.New64a()
	_, _ = h.Write(b)
	return fmt.Sprintf("%016x", h.Sum64())
}

// singlePlayer reports whether md was loaded with the single-player
// options the builder uses (navbuild loads every skill that way).
func singlePlayer(md *mapdata.Map) bool {
	return md.Options == (mapdata.Options{Skill: md.Options.Skill})
}
