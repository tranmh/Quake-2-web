package game

// Port of game/m_boss3.c (monster_boss3_stand, uses the m_boss32.h frames).

import (
	. "quake2web/server/internal/q2const"
)

var (
	Use_Boss3        = defUse("Use_Boss3")
	Think_Boss3Stand = defThink("Think_Boss3Stand")
)

func init() {
	Use_Boss3.bind((*Game).Use_Boss3)
	Think_Boss3Stand.bind((*Game).Think_Boss3Stand)
	RegisterSpawn("monster_boss3_stand", (*Game).SP_monster_boss3_stand)
}

// C: game/m_boss3.c:31 Use_Boss3
func (g *Game) Use_Boss3(ent, other, activator *Edict) {
	g.gi.WriteByteC(svc_temp_entity)
	g.gi.WriteByteC(TE_BOSSTPORT)
	g.gi.WritePosition(&ent.S.Origin)
	g.gi.Multicast(&ent.S.Origin, MULTICAST_PVS)
	g.G_FreeEdict(ent)
}

// C: game/m_boss3.c:40 Think_Boss3Stand
func (g *Game) Think_Boss3Stand(ent *Edict) {
	if ent.S.Frame == boss32_FRAME_stand260 {
		ent.S.Frame = boss32_FRAME_stand201
	} else {
		ent.S.Frame++
	}
	ent.Nextthink = float32(float64(g.level.Time) + FRAMETIME)
}

// QUAKED monster_boss3_stand (1 .5 0) (-32 -32 0) (32 32 90)
//
// Just stands and cycles in one place until targeted, then teleports away.
// C: game/m_boss3.c:53 SP_monster_boss3_stand
func (g *Game) SP_monster_boss3_stand(self *Edict) {
	if g.deathmatch.Value != 0 {
		g.G_FreeEdict(self)
		return
	}

	self.Movetype = MOVETYPE_STEP
	self.Solid = SOLID_BBOX
	self.Model = "models/monsters/boss3/rider/tris.md2"
	self.S.ModelIndex = int32(g.gi.ModelIndex(self.Model))
	self.S.Frame = boss32_FRAME_stand201

	g.gi.SoundIndex("misc/bigtele.wav")

	self.Mins = Vec3{-32, -32, 0}
	self.Maxs = Vec3{32, 32, 90}

	self.Use = Use_Boss3
	self.Think = Think_Boss3Stand
	self.Nextthink = float32(float64(g.level.Time) + FRAMETIME)
	g.gi.LinkEntity(self)
}
