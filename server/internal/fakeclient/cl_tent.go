package fakeclient

import (
	"quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/msg"
	"quake2web/server/internal/qcommon/shared"
)

// parseTEnt consumes a svc_temp_entity with exactly the reads of the C client
// (effects are not simulated): the type is recorded in TempEnts and the
// values read in TempEntEvents.
// C: client/cl_tent.c:694 CL_ParseTEnt
func (c *Client) parseTEnt(m *msg.SizeBuf) {
	var te TempEnt
	te.Type = m.MSG_ReadByte()
	c.TempEnts = appendHistory(c.TempEnts, te.Type, c.opt.MaxHistory, &c.Counts.TempEnts)

	switch te.Type {
	case q2const.TE_BLOOD, q2const.TE_GUNSHOT, q2const.TE_SPARKS, q2const.TE_BULLET_SPARKS,
		q2const.TE_SCREEN_SPARKS, q2const.TE_SHIELD_SPARKS, q2const.TE_SHOTGUN,
		q2const.TE_BLASTER, q2const.TE_GREENBLOOD, q2const.TE_BLASTER2, q2const.TE_FLECHETTE,
		q2const.TE_HEATBEAM_SPARKS, q2const.TE_HEATBEAM_STEAM, q2const.TE_MOREBLOOD,
		q2const.TE_ELECTRIC_SPARKS:
		te.Pos = m.MSG_ReadPos()
		te.Dir = m.MSG_ReadDir()

	case q2const.TE_SPLASH, q2const.TE_LASER_SPARKS, q2const.TE_WELDING_SPARKS, q2const.TE_TUNNEL_SPARKS:
		te.Count = m.MSG_ReadByte()
		te.Pos = m.MSG_ReadPos()
		te.Dir = m.MSG_ReadDir()
		te.Color = m.MSG_ReadByte() // r / color

	case q2const.TE_BLUEHYPERBLASTER, q2const.TE_RAILTRAIL, q2const.TE_BUBBLETRAIL,
		q2const.TE_DEBUGTRAIL, q2const.TE_BUBBLETRAIL2,
		q2const.TE_BFG_LASER: // CL_ParseLaser
		te.Pos = m.MSG_ReadPos()
		te.Pos2 = m.MSG_ReadPos()

	case q2const.TE_EXPLOSION2, q2const.TE_GRENADE_EXPLOSION, q2const.TE_GRENADE_EXPLOSION_WATER,
		q2const.TE_PLASMA_EXPLOSION, q2const.TE_EXPLOSION1, q2const.TE_EXPLOSION1_BIG,
		q2const.TE_ROCKET_EXPLOSION, q2const.TE_ROCKET_EXPLOSION_WATER, q2const.TE_EXPLOSION1_NP,
		q2const.TE_BFG_EXPLOSION, q2const.TE_BFG_BIGEXPLOSION, q2const.TE_BOSSTPORT,
		q2const.TE_PLAIN_EXPLOSION, q2const.TE_CHAINFIST_SMOKE, q2const.TE_TRACKER_EXPLOSION,
		q2const.TE_TELEPORT_EFFECT, q2const.TE_DBALL_GOAL, q2const.TE_WIDOWSPLASH:
		te.Pos = m.MSG_ReadPos()

	case q2const.TE_PARASITE_ATTACK, q2const.TE_MEDIC_CABLE_ATTACK: // CL_ParseBeam
		te.Ent = m.MSG_ReadShort()
		te.Pos = m.MSG_ReadPos()
		te.Pos2 = m.MSG_ReadPos()

	case q2const.TE_GRAPPLE_CABLE: // CL_ParseBeam2
		te.Ent = m.MSG_ReadShort()
		te.Pos = m.MSG_ReadPos()
		te.Pos2 = m.MSG_ReadPos()
		te.Offset = m.MSG_ReadPos()

	case q2const.TE_HEATBEAM, q2const.TE_MONSTER_HEATBEAM: // CL_ParsePlayerBeam (no offset for heatbeams)
		te.Ent = m.MSG_ReadShort()
		te.Pos = m.MSG_ReadPos()
		te.Pos2 = m.MSG_ReadPos()

	case q2const.TE_LIGHTNING: // CL_ParseLightning
		te.Ent = m.MSG_ReadShort()  // srcEnt
		te.Ent2 = m.MSG_ReadShort() // destEnt
		te.Pos = m.MSG_ReadPos()
		te.Pos2 = m.MSG_ReadPos()

	case q2const.TE_FLASHLIGHT:
		te.Pos = m.MSG_ReadPos()
		te.Ent = m.MSG_ReadShort()

	case q2const.TE_FORCEWALL:
		te.Pos = m.MSG_ReadPos()
		te.Pos2 = m.MSG_ReadPos()
		te.Color = m.MSG_ReadByte()

	case q2const.TE_STEAM: // C: client/cl_tent.c:557 CL_ParseSteam
		te.Ent = m.MSG_ReadShort() // an id of -1 is an instant effect
		te.Count = m.MSG_ReadByte()
		te.Pos = m.MSG_ReadPos()
		te.Dir = m.MSG_ReadDir()
		te.Color = m.MSG_ReadByte()
		te.Magnitude = m.MSG_ReadShort()
		if te.Ent != -1 {
			te.Wait = m.MSG_ReadLong() // sustain end time / interval
		}

	case q2const.TE_WIDOWBEAMOUT: // C: client/cl_tent.c:619 CL_ParseWidow
		te.Ent = m.MSG_ReadShort()
		te.Pos = m.MSG_ReadPos()

	case q2const.TE_NUKEBLAST: // C: client/cl_tent.c:652 CL_ParseNuke
		te.Pos = m.MSG_ReadPos()

	default:
		shared.Error(q2const.ERR_DROP, "CL_ParseTEnt: bad type")
	}
	c.TempEntEvents = appendHistory(c.TempEntEvents, te, c.opt.MaxHistory, &c.Counts.TempEntEvents)
}
