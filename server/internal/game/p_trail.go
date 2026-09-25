package game

// Port of game/p_trail.c: PLAYER TRAIL.
//
// This is a circular list containing the a list of points of where
// the player has been recently.  It is used by monsters for pursuit.
//
// .origin		the spot
// .owner		forward link
// .aiment		backward link

import "quake2web/server/internal/qcommon/shared"

// C: game/p_trail.c:39 TRAIL_LENGTH
const TRAIL_LENGTH = 8

// C: game/p_trail.c:45 NEXT
func trailNEXT(n int32) int32 { return (n + 1) & (TRAIL_LENGTH - 1) }

// C: game/p_trail.c:46 PREV
func trailPREV(n int32) int32 { return (n - 1) & (TRAIL_LENGTH - 1) }

// C: game/p_trail.c:49 PlayerTrail_Init
func (g *Game) PlayerTrail_Init() {
	if g.deathmatch.Value != 0 /* FIXME || coop */ {
		return
	}

	for n := 0; n < TRAIL_LENGTH; n++ {
		g.trail[n] = g.G_Spawn()
		g.trail[n].Classname = "player_trail"
	}

	g.trail_head = 0
	g.trail_active = true
}

// C: game/p_trail.c:67 PlayerTrail_Add
func (g *Game) PlayerTrail_Add(spot Vec3) {
	if !g.trail_active {
		return
	}

	g.trail[g.trail_head].S.Origin = spot

	g.trail[g.trail_head].Timestamp = g.level.Time

	temp := shared.VectorSubtract(spot, g.trail[trailPREV(g.trail_head)].S.Origin)
	g.trail[g.trail_head].S.Angles[1] = vectoyaw(temp)

	g.trail_head = trailNEXT(g.trail_head)
}

// C: game/p_trail.c:85 PlayerTrail_New
func (g *Game) PlayerTrail_New(spot Vec3) {
	if !g.trail_active {
		return
	}

	g.PlayerTrail_Init()
	g.PlayerTrail_Add(spot)
}

// C: game/p_trail.c:95 PlayerTrail_PickFirst
func (g *Game) PlayerTrail_PickFirst(self *Edict) *Edict {
	if !g.trail_active {
		return nil
	}

	marker := g.trail_head
	for n := TRAIL_LENGTH; n != 0; n-- {
		if g.trail[marker].Timestamp <= self.Monsterinfo.TrailTime {
			marker = trailNEXT(marker)
		} else {
			break
		}
	}

	if g.visible(self, g.trail[marker]) {
		return g.trail[marker]
	}

	if g.visible(self, g.trail[trailPREV(marker)]) {
		return g.trail[trailPREV(marker)]
	}

	return g.trail[marker]
}

// C: game/p_trail.c:124 PlayerTrail_PickNext
func (g *Game) PlayerTrail_PickNext(self *Edict) *Edict {
	if !g.trail_active {
		return nil
	}

	marker := g.trail_head
	for n := TRAIL_LENGTH; n != 0; n-- {
		if g.trail[marker].Timestamp <= self.Monsterinfo.TrailTime {
			marker = trailNEXT(marker)
		} else {
			break
		}
	}

	return g.trail[marker]
}

// C: game/p_trail.c:143 PlayerTrail_LastSpot
func (g *Game) PlayerTrail_LastSpot() *Edict {
	return g.trail[trailPREV(g.trail_head)]
}
