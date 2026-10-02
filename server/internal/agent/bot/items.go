package bot

import (
	"quake2web/server/internal/agent/mapdata"
	"quake2web/server/internal/agent/worldmodel"
)

// itemTriggerSpawn is the item spawnflag that keeps an item hidden until
// something uses it (C: game/g_items.c ITEM_TRIGGER_SPAWN).
const itemTriggerSpawn = 1

// seedItems adds the level's item spawns from the map data to the level
// memory the world model just entered, as items known from an earlier
// attempt are (worldmodel.LevelMemory.Items): static map knowledge, the
// way a player who knows the level knows where the weapons and the health
// lie. The world model restores them at the level's first frame as
// remembered items (worldmodel.Item.Remembered) and believes them there
// until it sees their spot empty; the decision layer may then pick them
// up. Items hidden until triggered (ITEM_TRIGGER_SPAWN) and the ones the
// memory already holds are left out.
func (b *Bot) seedItems(md *mapdata.Map) {
	mem := b.world.Memory()
	if mem == nil || mem.Key.Map == "" || md == nil {
		return
	}
	for _, it := range md.Items {
		e := md.Entity(it.Entity)
		if e == nil || e.Spawnflags&itemTriggerSpawn != 0 {
			continue
		}
		cls := b.classes.ForClassname(it.Classname)
		if len(cls) == 0 {
			continue
		}
		name := cls[0].Name
		known := false
		for _, k := range mem.Items {
			if k.Class == name && dist3(k.Pos, it.Origin) < 16 {
				known = true
				break
			}
		}
		if !known {
			mem.Items = append(mem.Items, worldmodel.KnownItem{Class: name, Pos: it.Origin, Lump: it.Entity})
		}
	}
}
