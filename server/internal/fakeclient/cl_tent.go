package fakeclient

import (
	"quake2web/server/internal/q2const"
	"quake2web/server/internal/qcommon/msg"
	"quake2web/server/internal/qcommon/shared"
)

// parseTEnt consumes a svc_temp_entity with exactly the reads of the C client
// (effects are not simulated; the type is recorded).
// C: client/cl_tent.c:694 CL_ParseTEnt
func (c *Client) parseTEnt(m *msg.SizeBuf) {
	typ := m.MSG_ReadByte()
	c.TempEnts = append(c.TempEnts, typ)
	pos := func(n int) {
		for i := 0; i < n; i++ {
			m.MSG_ReadPos()
		}
	}
	dir := func() { m.MSG_ReadDir() }

	switch typ {
	case q2const.TE_BLOOD, q2const.TE_GUNSHOT, q2const.TE_SPARKS, q2const.TE_BULLET_SPARKS,
		q2const.TE_SCREEN_SPARKS, q2const.TE_SHIELD_SPARKS, q2const.TE_SHOTGUN,
		q2const.TE_BLASTER, q2const.TE_GREENBLOOD, q2const.TE_BLASTER2, q2const.TE_FLECHETTE,
		q2const.TE_HEATBEAM_SPARKS, q2const.TE_HEATBEAM_STEAM, q2const.TE_MOREBLOOD,
		q2const.TE_ELECTRIC_SPARKS:
		pos(1)
		dir()

	case q2const.TE_SPLASH, q2const.TE_LASER_SPARKS, q2const.TE_WELDING_SPARKS, q2const.TE_TUNNEL_SPARKS:
		m.MSG_ReadByte() // cnt
		pos(1)
		dir()
		m.MSG_ReadByte() // r / color

	case q2const.TE_BLUEHYPERBLASTER, q2const.TE_RAILTRAIL, q2const.TE_BUBBLETRAIL,
		q2const.TE_DEBUGTRAIL, q2const.TE_BUBBLETRAIL2,
		q2const.TE_BFG_LASER: // CL_ParseLaser
		pos(2)

	case q2const.TE_EXPLOSION2, q2const.TE_GRENADE_EXPLOSION, q2const.TE_GRENADE_EXPLOSION_WATER,
		q2const.TE_PLASMA_EXPLOSION, q2const.TE_EXPLOSION1, q2const.TE_EXPLOSION1_BIG,
		q2const.TE_ROCKET_EXPLOSION, q2const.TE_ROCKET_EXPLOSION_WATER, q2const.TE_EXPLOSION1_NP,
		q2const.TE_BFG_EXPLOSION, q2const.TE_BFG_BIGEXPLOSION, q2const.TE_BOSSTPORT,
		q2const.TE_PLAIN_EXPLOSION, q2const.TE_CHAINFIST_SMOKE, q2const.TE_TRACKER_EXPLOSION,
		q2const.TE_TELEPORT_EFFECT, q2const.TE_DBALL_GOAL, q2const.TE_WIDOWSPLASH:
		pos(1)

	case q2const.TE_PARASITE_ATTACK, q2const.TE_MEDIC_CABLE_ATTACK: // CL_ParseBeam
		m.MSG_ReadShort()
		pos(2)

	case q2const.TE_GRAPPLE_CABLE: // CL_ParseBeam2
		m.MSG_ReadShort()
		pos(3)

	case q2const.TE_HEATBEAM, q2const.TE_MONSTER_HEATBEAM: // CL_ParsePlayerBeam (no offset for heatbeams)
		m.MSG_ReadShort()
		pos(2)

	case q2const.TE_LIGHTNING: // CL_ParseLightning
		m.MSG_ReadShort()
		m.MSG_ReadShort()
		pos(2)

	case q2const.TE_FLASHLIGHT:
		pos(1)
		m.MSG_ReadShort()

	case q2const.TE_FORCEWALL:
		pos(2)
		m.MSG_ReadByte()

	case q2const.TE_STEAM: // C: client/cl_tent.c:557 CL_ParseSteam
		id := m.MSG_ReadShort() // an id of -1 is an instant effect
		m.MSG_ReadByte()
		pos(1)
		dir()
		m.MSG_ReadByte()
		m.MSG_ReadShort()
		if id != -1 {
			m.MSG_ReadLong() // sustain end time / interval
		}

	case q2const.TE_WIDOWBEAMOUT: // C: client/cl_tent.c:619 CL_ParseWidow
		m.MSG_ReadShort()
		pos(1)

	case q2const.TE_NUKEBLAST: // C: client/cl_tent.c:652 CL_ParseNuke
		pos(1)

	default:
		shared.Error(q2const.ERR_DROP, "CL_ParseTEnt: bad type")
	}
}
