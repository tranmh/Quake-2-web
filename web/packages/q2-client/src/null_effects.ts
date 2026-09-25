// No-op Effects (cl_fx.c / cl_tent.c / cl_newfx.c stand-in). Draws nothing, but consumes the
// svc_temp_entity / svc_muzzleflash / svc_muzzleflash2 payloads byte-exactly like the original parsers
// so the rest of the server message stays in sync.
import {
  type EntityState,
  ERR_DROP,
  MAX_EDICTS,
  TE_BFG_BIGEXPLOSION,
  TE_BFG_EXPLOSION,
  TE_BFG_LASER,
  TE_BLASTER,
  TE_BLASTER2,
  TE_BLOOD,
  TE_BLUEHYPERBLASTER,
  TE_BOSSTPORT,
  TE_BUBBLETRAIL,
  TE_BUBBLETRAIL2,
  TE_BULLET_SPARKS,
  TE_CHAINFIST_SMOKE,
  TE_DBALL_GOAL,
  TE_DEBUGTRAIL,
  TE_ELECTRIC_SPARKS,
  TE_EXPLOSION1,
  TE_EXPLOSION1_BIG,
  TE_EXPLOSION1_NP,
  TE_EXPLOSION2,
  TE_FLASHLIGHT,
  TE_FLECHETTE,
  TE_FORCEWALL,
  TE_GRAPPLE_CABLE,
  TE_GREENBLOOD,
  TE_GRENADE_EXPLOSION,
  TE_GRENADE_EXPLOSION_WATER,
  TE_GUNSHOT,
  TE_HEATBEAM,
  TE_HEATBEAM_SPARKS,
  TE_HEATBEAM_STEAM,
  TE_LASER_SPARKS,
  TE_LIGHTNING,
  TE_MEDIC_CABLE_ATTACK,
  TE_MONSTER_HEATBEAM,
  TE_MOREBLOOD,
  TE_NUKEBLAST,
  TE_PARASITE_ATTACK,
  TE_PLAIN_EXPLOSION,
  TE_PLASMA_EXPLOSION,
  TE_RAILTRAIL,
  TE_ROCKET_EXPLOSION,
  TE_ROCKET_EXPLOSION_WATER,
  TE_SCREEN_SPARKS,
  TE_SHIELD_SPARKS,
  TE_SHOTGUN,
  TE_SPARKS,
  TE_SPLASH,
  TE_STEAM,
  TE_TELEPORT_EFFECT,
  TE_TRACKER_EXPLOSION,
  TE_TUNNEL_SPARKS,
  TE_WELDING_SPARKS,
  TE_WIDOWBEAMOUT,
  TE_WIDOWSPLASH,
} from 'q2-shared';
import {
  MSG_ReadByte,
  MSG_ReadDir,
  MSG_ReadLong,
  MSG_ReadPos,
  MSG_ReadShort,
  type SizeBuf,
} from 'q2-protocol';
import { Com_Error, type ClientContext } from './client';
import type { Effects } from './effects';

export class NullEffects implements Effects {
  readonly cl_mod_powerscreen = null;
  private c: ClientContext | null = null;
  private readonly pos = new Float32Array(3);

  attach(c: ClientContext): void {
    this.c = c;
  }

  private drop(msg: string): never {
    if (this.c) Com_Error(this.c, ERR_DROP, '%s', msg);
    throw new Error(msg);
  }

  clearEffects(): void {}
  setLightstyle(_i: number): void {}
  runDLights(): void {}
  runLightStyles(): void {}

  // C: cl_fx.c:240 CL_ParseMuzzleFlash (reads only)
  parseMuzzleFlash(msg: SizeBuf): void {
    const i = MSG_ReadShort(msg);
    if (i < 1 || i >= MAX_EDICTS) this.drop('CL_ParseMuzzleFlash: bad entity');
    MSG_ReadByte(msg); // weapon
  }

  // C: cl_fx.c:430 CL_ParseMuzzleFlash2 (reads only)
  parseMuzzleFlash2(msg: SizeBuf): void {
    const ent = MSG_ReadShort(msg);
    if (ent < 1 || ent >= MAX_EDICTS) this.drop('CL_ParseMuzzleFlash2: bad entity');
    MSG_ReadByte(msg); // flash_number
  }

  entityEvent(_ent: EntityState): void {}
  teleporterParticles(): void {}
  addParticles(): void {}
  addDLights(): void {}
  addLightStyles(): void {}
  rocketTrail(): void {}
  blasterTrail(): void {}
  diminishingTrail(): void {}
  flyEffect(): void {}
  bfgParticles(): void {}
  trapParticles(): void {}
  flagTrail(): void {}
  ionripperTrail(): void {}
  blasterTrail2(): void {}
  tagTrail(): void {}
  trackerShell(): void {}
  trackerTrail(): void {}
  clearTEnts(): void {}
  registerTEntSounds(): void {}
  async registerTEntModels(): Promise<void> {}
  addTEnts(): void {}

  // C: cl_tent.c:694 CL_ParseTEnt (reads only)
  parseTEnt(msg: SizeBuf): void {
    const p = this.pos;
    const pos = (): void => MSG_ReadPos(msg, p);
    const dir = (): void => MSG_ReadDir(msg, p);
    const type = MSG_ReadByte(msg);
    switch (type) {
      case TE_BLOOD: // bullet hitting flesh
      case TE_GUNSHOT: // bullet hitting wall
      case TE_SPARKS:
      case TE_BULLET_SPARKS:
      case TE_SCREEN_SPARKS:
      case TE_SHIELD_SPARKS:
      case TE_SHOTGUN: // bullet hitting wall
      case TE_BLASTER: // blaster hitting wall
      case TE_GREENBLOOD:
      case TE_BLASTER2: // green blaster hitting wall
      case TE_FLECHETTE: // flechette
      case TE_HEATBEAM_SPARKS:
      case TE_HEATBEAM_STEAM:
      case TE_MOREBLOOD:
      case TE_ELECTRIC_SPARKS:
        pos();
        dir();
        break;

      case TE_SPLASH: // bullet hitting water
      case TE_LASER_SPARKS:
      case TE_WELDING_SPARKS:
      case TE_TUNNEL_SPARKS:
        MSG_ReadByte(msg); // cnt
        pos();
        dir();
        MSG_ReadByte(msg); // r / color
        break;

      case TE_BLUEHYPERBLASTER:
      case TE_RAILTRAIL: // railgun effect
      case TE_BUBBLETRAIL:
      case TE_DEBUGTRAIL:
      case TE_BUBBLETRAIL2:
      case TE_BFG_LASER: // CL_ParseLaser
        pos();
        pos();
        break;

      case TE_EXPLOSION2:
      case TE_GRENADE_EXPLOSION:
      case TE_GRENADE_EXPLOSION_WATER:
      case TE_PLASMA_EXPLOSION:
      case TE_EXPLOSION1:
      case TE_EXPLOSION1_BIG:
      case TE_ROCKET_EXPLOSION:
      case TE_ROCKET_EXPLOSION_WATER:
      case TE_EXPLOSION1_NP:
      case TE_BFG_EXPLOSION:
      case TE_BFG_BIGEXPLOSION:
      case TE_BOSSTPORT: // boss teleporting to station
      case TE_PLAIN_EXPLOSION:
      case TE_CHAINFIST_SMOKE:
      case TE_TRACKER_EXPLOSION:
      case TE_TELEPORT_EFFECT:
      case TE_DBALL_GOAL:
      case TE_WIDOWSPLASH:
      case TE_NUKEBLAST: // CL_ParseNuke
        pos();
        break;

      case TE_PARASITE_ATTACK: // CL_ParseBeam
      case TE_MEDIC_CABLE_ATTACK:
      case TE_HEATBEAM: // CL_ParsePlayerBeam (heatbeam offsets are implied)
      case TE_MONSTER_HEATBEAM:
        MSG_ReadShort(msg);
        pos();
        pos();
        break;

      case TE_GRAPPLE_CABLE: // CL_ParseBeam2
        MSG_ReadShort(msg);
        pos();
        pos();
        pos();
        break;

      case TE_LIGHTNING: // CL_ParseLightning
        MSG_ReadShort(msg);
        MSG_ReadShort(msg);
        pos();
        pos();
        break;

      case TE_FLASHLIGHT:
        pos();
        MSG_ReadShort(msg);
        break;

      case TE_FORCEWALL:
        pos();
        pos();
        MSG_ReadByte(msg);
        break;

      case TE_STEAM: {
        // CL_ParseSteam: both the sustain and the "no free sustain" paths read the interval long
        const id = MSG_ReadShort(msg); // an id of -1 is an instant effect
        MSG_ReadByte(msg);
        pos();
        dir();
        MSG_ReadByte(msg);
        MSG_ReadShort(msg);
        if (id !== -1) MSG_ReadLong(msg);
        break;
      }

      case TE_WIDOWBEAMOUT: // CL_ParseWidow
        MSG_ReadShort(msg);
        pos();
        break;

      default:
        this.drop('CL_ParseTEnt: bad type');
    }
  }
}
