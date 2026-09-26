// Port of client/cl_ents.c -- entity parsing and management.
// The `#if 0` projectile code (CL_ClearProjectiles / CL_ParseProjectiles / CL_AddProjectiles) is not
// compiled in the original and is therefore not ported.
import {
  AngleVectors,
  EF_ANIM01,
  EF_ANIM23,
  EF_ANIM_ALL,
  EF_ANIM_ALLFAST,
  EF_BFG,
  EF_BLASTER,
  EF_BLUEHYPERBLASTER,
  EF_COLOR_SHELL,
  EF_DOUBLE,
  EF_FLAG1,
  EF_FLAG2,
  EF_FLIES,
  EF_GIB,
  EF_GREENGIB,
  EF_GRENADE,
  EF_HALF_DAMAGE,
  EF_HYPERBLASTER,
  EF_IONRIPPER,
  EF_PENT,
  EF_PLASMA,
  EF_POWERSCREEN,
  EF_QUAD,
  EF_ROCKET,
  EF_ROTATE,
  EF_SPHERETRANS,
  EF_SPINNINGLIGHTS,
  EF_TAGTRAIL,
  EF_TELEPORTER,
  EF_TRACKER,
  EF_TRACKERTRAIL,
  EF_TRAP,
  ERR_DROP,
  EV_OTHER_TELEPORT,
  EV_PLAYER_TELEPORT,
  LerpAngle,
  MAX_CLIENTWEAPONMODELS,
  MAX_CONFIGSTRINGS,
  MAX_QPATH,
  MAX_EDICTS,
  MAX_PARSE_ENTITIES,
  PMF_NO_PREDICTION,
  PM_DEAD,
  RF_BEAM,
  RF_DEPTHHACK,
  RF_FRAMELERP,
  RF_MINLIGHT,
  RF_SHELL_BLUE,
  RF_SHELL_DOUBLE,
  RF_SHELL_GREEN,
  RF_SHELL_HALF_DAM,
  RF_SHELL_RED,
  RF_TRANSLUCENT,
  RF_USE_DISGUISE,
  RF_VIEWERMODEL,
  RF_WEAPONMODEL,
  U_REMOVE,
  UPDATE_MASK,
  VIDREF_GL,
  anglemod,
  cInt,
  fr,
  svc_packetentities,
  svc_playerinfo,
  type EntityState,
  type PlayerState,
} from 'q2-shared';
import {
  MSG_ReadByte,
  MSG_ReadData,
  MSG_ReadLong,
  parseDelta,
  parseEntityBits,
  parsePlayerstate,
} from 'q2-protocol';
import type { EntityBits } from 'q2-protocol';
import { newRefEntity, type ImageHandle, type ModelHandle, type RefEntity } from 'q2-ref';
import { CS_PLAYERSKINS } from 'q2-shared';
import { ca_active, Com_Error, Com_Printf, type ClientContext, type ClientInfo, type Frame } from './client';
import { SCR_EndLoadingPlaque } from './cl_scrn';
import { CL_CheckPredictionError } from './cl_pred';
import { V_AddEntity, V_AddLight } from './cl_view';
import { RegisterModelSync, RegisterSkinSync } from './regcache';
import { SHOWNET, svc_strings } from './cl_parse';

// PGM: the renderer is always GL in the browser
const vidref_val = VIDREF_GL;

const bfg_lightramp = [300, 400, 600, 300, 150, 75] as const;

/** Scratch storage for cl_ents.c / cl_parse.c (C: locals and file statics), preallocated. */
export class EntsState {
  readonly bits: EntityBits = { number: 0, bits: 0 };
  /** CL_AddPacketEntities `entity_t ent` (memset once per call, reused across entities as in C) */
  readonly ent: RefEntity = newRefEntity();
  /** CL_AddViewWeapon `entity_t gun` */
  readonly gun: RefEntity = newRefEntity();
  readonly forward = new Float32Array(3);
  readonly start = new Float32Array(3);
  /** CL_ParseStartSoundPacket pos_v */
  readonly sound_pos = new Float32Array(3);
  /** CL_LoadClientinfo: newest load token per clientinfo (async loads may overlap) */
  readonly clientinfoTokens = new Map<ClientInfo, number>();
  /** flat `char configstrings[MAX_CONFIGSTRINGS][MAX_QPATH]` image (see CL_StoreConfigString) */
  readonly configstring_bytes = new Uint8Array(MAX_CONFIGSTRINGS * MAX_QPATH);
}

function clearRefEntity(e: RefEntity): void {
  e.model = null;
  e.angles.fill(0);
  e.origin.fill(0);
  e.frame = 0;
  e.oldorigin.fill(0);
  e.oldframe = 0;
  e.backlerp = 0;
  e.skinnum = 0;
  e.lightstyle = 0;
  e.alpha = 0;
  e.skin = null;
  e.flags = 0;
}

/*
=========================================================================

FRAME PARSING

=========================================================================
*/

// C: cl_ents.c:202 CL_ParseEntityBits -- returns the entity number and the header bits (shared object)
export function CL_ParseEntityBits(c: ClientContext): EntityBits {
  return parseEntityBits(c.net_message, c.ents.bits);
}

// C: cl_ents.c:247 CL_ParseDelta -- can go from either a baseline or a previous packet_entity
export function CL_ParseDelta(
  c: ClientContext,
  from: EntityState,
  to: EntityState,
  number: number,
  bits: number,
): void {
  parseDelta(from, to, number, bits, c.net_message);
}

// C: cl_ents.c:327 CL_DeltaEntity -- parses deltas from the given base and adds the resulting entity
// to the current frame
export function CL_DeltaEntity(
  c: ClientContext,
  frame: Frame,
  newnum: number,
  old: EntityState,
  bits: number,
): void {
  const cl = c.cl;
  const ent = c.cl_entities[newnum]!;

  // A frame holds each entity number at most once (< MAX_EDICTS == MAX_PARSE_ENTITIES entries). A server
  // repeating numbers out of order makes every delta frame larger than its base, so num_entities -- and
  // the per-frame work -- would grow without bound (the ring then also overwrites its own base).
  if (frame.num_entities >= MAX_PARSE_ENTITIES) Com_Error(c, ERR_DROP, 'CL_DeltaEntity: too many entities');
  const state = c.cl_parse_entities[cl.parse_entities & (MAX_PARSE_ENTITIES - 1)]!;
  cl.parse_entities++;
  frame.num_entities++;

  CL_ParseDelta(c, old, state, newnum, bits);

  // some data changes will force no lerping
  const cur = ent.current;
  if (
    state.modelindex !== cur.modelindex ||
    state.modelindex2 !== cur.modelindex2 ||
    state.modelindex3 !== cur.modelindex3 ||
    state.modelindex4 !== cur.modelindex4 ||
    Math.abs(cInt(fr(state.origin[0]! - cur.origin[0]!))) > 512 ||
    Math.abs(cInt(fr(state.origin[1]! - cur.origin[1]!))) > 512 ||
    Math.abs(cInt(fr(state.origin[2]! - cur.origin[2]!))) > 512 ||
    state.event === EV_PLAYER_TELEPORT ||
    state.event === EV_OTHER_TELEPORT
  ) {
    ent.serverframe = -99;
  }

  if (ent.serverframe !== cl.frame.serverframe - 1) {
    // wasn't in last update, so initialize some things
    ent.trailcount = 1024; // for diminishing rocket / grenade trails
    // duplicate the current state so lerping doesn't hurt anything
    ent.prev.copyFrom(state);
    if (state.event === EV_OTHER_TELEPORT) {
      ent.prev.origin.set(state.origin);
      ent.lerp_origin.set(state.origin);
    } else {
      ent.prev.origin.set(state.old_origin);
      ent.lerp_origin.set(state.old_origin);
    }
  } else {
    // shuffle the last state to previous
    ent.prev.copyFrom(ent.current);
  }

  ent.serverframe = cl.frame.serverframe;
  ent.current.copyFrom(state);
}

// C: cl_ents.c:388 CL_ParsePacketEntities -- an svc_packetentities has just been parsed, deal with the
// rest of the data stream.
export function CL_ParsePacketEntities(c: ClientContext, oldframe: Frame | null, newframe: Frame): void {
  const cl = c.cl;
  const msg = c.net_message;
  const pe = c.cl_parse_entities;
  const shownet = c.cv.cl_shownet;

  newframe.parse_entities = cl.parse_entities;
  newframe.num_entities = 0;

  // delta from the entities present in oldframe
  let oldindex = 0;
  let oldstate: EntityState | null = null;
  let oldnum: number;
  const next = (): void => {
    if (oldindex >= oldframe!.num_entities) oldnum = 99999;
    else {
      oldstate = pe[(oldframe!.parse_entities + oldindex) & (MAX_PARSE_ENTITIES - 1)]!;
      oldnum = oldstate.number;
    }
  };
  if (!oldframe) oldnum = 99999;
  else next();

  while (1) {
    const eb = CL_ParseEntityBits(c);
    const newnum = eb.number;
    const bits = eb.bits;
    if (newnum >= MAX_EDICTS) Com_Error(c, ERR_DROP, 'CL_ParsePacketEntities: bad number:%i', newnum);

    if (msg.readcount > msg.cursize) Com_Error(c, ERR_DROP, 'CL_ParsePacketEntities: end of message');

    if (!newnum) break;

    // negative numbers (a signed short entity number) would index before the array in C
    if (newnum < 0) Com_Error(c, ERR_DROP, 'CL_ParsePacketEntities: bad number:%i', newnum);

    while (oldnum! < newnum) {
      // one or more entities from the old packet are unchanged
      if (shownet.value === 3) Com_Printf(c, '   unchanged: %i\n', oldnum!);
      CL_DeltaEntity(c, newframe, oldnum!, oldstate!, 0);
      oldindex++;
      next();
    }

    if (bits & U_REMOVE) {
      // the entity present in oldframe is not in the current frame
      if (shownet.value === 3) Com_Printf(c, '   remove: %i\n', newnum);
      if (oldnum! !== newnum) Com_Printf(c, 'U_REMOVE: oldnum != newnum\n');

      oldindex++;
      // (oldframe is non-NULL here unless the server sent a bogus remove; C would crash)
      if (oldframe) next();
      else oldnum = 99999;
      continue;
    }

    if (oldnum! === newnum) {
      // delta from previous state
      if (shownet.value === 3) Com_Printf(c, '   delta: %i\n', newnum);
      CL_DeltaEntity(c, newframe, newnum, oldstate!, bits);

      oldindex++;
      next();
      continue;
    }

    if (oldnum! > newnum) {
      // delta from baseline
      if (shownet.value === 3) Com_Printf(c, '   baseline: %i\n', newnum);
      CL_DeltaEntity(c, newframe, newnum, c.cl_entities[newnum]!.baseline, bits);
      continue;
    }
  }

  // any remaining entities in the old frame are copied over
  while (oldnum! !== 99999) {
    // one or more entities from the old packet are unchanged
    if (shownet.value === 3) Com_Printf(c, '   unchanged: %i\n', oldnum!);
    CL_DeltaEntity(c, newframe, oldnum!, oldstate!, 0);
    oldindex++;
    next();
  }
}

// C: cl_ents.c:515 CL_ParsePlayerstate
export function CL_ParsePlayerstate(c: ClientContext, oldframe: Frame | null, newframe: Frame): void {
  parsePlayerstate(
    oldframe ? oldframe.playerstate : null,
    newframe.playerstate,
    c.net_message,
    c.cl.attractloop,
  );
}

// C: cl_ents.c:639 CL_FireEntityEvents
export function CL_FireEntityEvents(c: ClientContext, frame: Frame): void {
  for (let pnum = 0; pnum < frame.num_entities; pnum++) {
    const num = (frame.parse_entities + pnum) & (MAX_PARSE_ENTITIES - 1);
    const s1 = c.cl_parse_entities[num]!;
    if (s1.event) c.fx.entityEvent(s1);

    // EF_TELEPORTER acts like an event, but is not cleared each frame
    if (s1.effects & EF_TELEPORTER) c.fx.teleporterParticles(s1);
  }
}

// C: cl_ents.c:663 CL_ParseFrame
export function CL_ParseFrame(c: ClientContext): void {
  const cl = c.cl;
  const cls = c.cls;
  const msg = c.net_message;

  cl.frame.clear();

  cl.frame.serverframe = MSG_ReadLong(msg);
  cl.frame.deltaframe = MSG_ReadLong(msg);
  cl.frame.servertime = Math.imul(cl.frame.serverframe, 100);

  // BIG HACK to let old demos continue to work
  if (cls.serverProtocol !== 26) cl.surpressCount = MSG_ReadByte(msg);

  if (c.cv.cl_shownet.value === 3)
    Com_Printf(c, '   frame:%i  delta:%i\n', cl.frame.serverframe, cl.frame.deltaframe);

  // If the frame is delta compressed from data that we
  // no longer have available, we must suck up the rest of
  // the frame, but not use it, then ask for a non-compressed
  // message
  let old: Frame | null;
  if (cl.frame.deltaframe <= 0) {
    cl.frame.valid = true; // uncompressed frame
    old = null;
    cls.demowaiting = false; // we can start recording now
  } else {
    old = cl.frames[cl.frame.deltaframe & UPDATE_MASK]!;
    if (!old.valid) {
      // should never happen
      Com_Printf(c, 'Delta from invalid frame (not supposed to happen!).\n');
    }
    if (old.serverframe !== cl.frame.deltaframe) {
      // The frame that the server did the delta from
      // is too old, so we can't reconstruct it properly.
      Com_Printf(c, 'Delta frame too old.\n');
    } else if (cl.parse_entities - old.parse_entities > MAX_PARSE_ENTITIES - 128) {
      Com_Printf(c, 'Delta parse_entities too old.\n');
    } else cl.frame.valid = true; // valid delta parse
  }

  // clamp time
  if (cl.time > cl.frame.servertime) cl.time = cl.frame.servertime;
  else if (cl.time < cl.frame.servertime - 100) cl.time = cl.frame.servertime - 100;

  // read areabits (at most sizeof(areabits) bytes are kept; the C memcpy would overrun)
  const len = MSG_ReadByte(msg);
  const ab = cl.frame.areabits;
  if (len > ab.length) {
    MSG_ReadData(msg, ab, ab.length);
    msg.readcount += len - ab.length;
  } else MSG_ReadData(msg, ab, len);

  // read playerinfo
  let cmd = MSG_ReadByte(msg);
  SHOWNET(c, svc_strings[cmd]);
  if (cmd !== svc_playerinfo) Com_Error(c, ERR_DROP, 'CL_ParseFrame: not playerinfo');
  CL_ParsePlayerstate(c, old, cl.frame);

  // read packet entities
  cmd = MSG_ReadByte(msg);
  SHOWNET(c, svc_strings[cmd]);
  if (cmd !== svc_packetentities) Com_Error(c, ERR_DROP, 'CL_ParseFrame: not packetentities');
  CL_ParsePacketEntities(c, old, cl.frame);

  // save the frame off in the backup array for later delta comparisons
  cl.frames[cl.frame.serverframe & UPDATE_MASK]!.copyFrom(cl.frame);

  if (cl.frame.valid) {
    // getting a valid frame message ends the connection process
    if (cls.state !== ca_active) {
      cls.state = ca_active;
      cl.force_refdef = true;
      const o = cl.frame.playerstate.pmove.origin;
      cl.predicted_origin[0] = o[0]! * 0.125;
      cl.predicted_origin[1] = o[1]! * 0.125;
      cl.predicted_origin[2] = o[2]! * 0.125;
      cl.predicted_angles.set(cl.frame.playerstate.viewangles);
      if (cls.disable_servercount !== cl.servercount && cl.refresh_prepped) SCR_EndLoadingPlaque(c); // get rid of loading plaque
    }
    cl.sound_prepped = true; // can start mixing ambient sounds

    // fire entity events
    CL_FireEntityEvents(c, cl.frame);
    CL_CheckPredictionError(c);
  }
}

/*
==========================================================================

INTERPOLATE BETWEEN FRAMES TO GET RENDERING PARMS

==========================================================================
*/

// C: cl_ents.c:780 S_RegisterSexedModel (frame-time: uses the registration cache)
export function S_RegisterSexedModel(c: ClientContext, ent: EntityState, base: string): ModelHandle | null {
  // determine what model the client is using
  let model = '';
  const n = CS_PLAYERSKINS + ent.number - 1;
  const cs = c.cl.configstrings[n];
  if (cs) {
    const p = cs.indexOf('\\');
    if (p >= 0) {
      model = cs.slice(p + 1);
      const q = model.indexOf('/');
      if (q >= 0) model = model.slice(0, q);
    }
  }
  // if we can't figure it out, they're male
  if (!model) model = 'male';

  let mdl = RegisterModelSync(c, `players/${model}/${base.slice(1)}`);
  if (!mdl) {
    // not found, try default weapon model
    mdl = RegisterModelSync(c, `players/${model}/weapon.md2`);
    if (!mdl) {
      // no, revert to the male model
      mdl = RegisterModelSync(c, `players/male/${base.slice(1)}`);
      if (!mdl) {
        // last try, default male weapon.md2
        mdl = RegisterModelSync(c, 'players/male/weapon.md2');
      }
    }
  }
  return mdl;
}

/** `(char *)image_t` in C is the image name: reverse lookup through the registration cache. */
function skinName(c: ClientContext, h: ImageHandle | null): string | null {
  if (!h) return null;
  for (const [k, v] of c.regcache.handles) if (v === h) return k.slice(k.indexOf(':') + 1);
  return null;
}

// C: cl_ents.c:834 CL_AddPacketEntities
export function CL_AddPacketEntities(c: ClientContext, frame: Frame): void {
  const cl = c.cl;
  const ent = c.ents.ent;

  // bonus items rotate at a fixed rate
  const autorotate = anglemod((cl.time / 10) | 0);

  // brush models can auto animate their frames
  const autoanim = ((2 * cl.time) / 1000) | 0;

  clearRefEntity(ent);

  for (let pnum = 0; pnum < frame.num_entities; pnum++) {
    const s1 = c.cl_parse_entities[(frame.parse_entities + pnum) & (MAX_PARSE_ENTITIES - 1)]!;

    const cent = c.cl_entities[s1.number]!;

    let effects = s1.effects >>> 0;
    let renderfx = s1.renderfx >>> 0;

    // set frame
    if (effects & EF_ANIM01) ent.frame = autoanim & 1;
    else if (effects & EF_ANIM23) ent.frame = 2 + (autoanim & 1);
    else if (effects & EF_ANIM_ALL) ent.frame = autoanim;
    else if (effects & EF_ANIM_ALLFAST) ent.frame = (cl.time / 100) | 0;
    else ent.frame = s1.frame;

    // quad and pent can do different things on client
    if (effects & EF_PENT) {
      effects &= ~EF_PENT;
      effects |= EF_COLOR_SHELL;
      renderfx |= RF_SHELL_RED;
    }

    if (effects & EF_QUAD) {
      effects &= ~EF_QUAD;
      effects |= EF_COLOR_SHELL;
      renderfx |= RF_SHELL_BLUE;
    }
    //======
    // PMM
    if (effects & EF_DOUBLE) {
      effects &= ~EF_DOUBLE;
      effects |= EF_COLOR_SHELL;
      renderfx |= RF_SHELL_DOUBLE;
    }

    if (effects & EF_HALF_DAMAGE) {
      effects &= ~EF_HALF_DAMAGE;
      effects |= EF_COLOR_SHELL;
      renderfx |= RF_SHELL_HALF_DAM;
    }
    // pmm
    //======
    effects >>>= 0;
    renderfx >>>= 0;

    ent.oldframe = cent.prev.frame;
    ent.backlerp = fr(1.0 - cl.lerpfrac);

    if (renderfx & (RF_FRAMELERP | RF_BEAM)) {
      // step origin discretely, because the frames
      // do the animation properly
      ent.origin.set(cent.current.origin);
      ent.oldorigin.set(cent.current.old_origin);
    } else {
      // interpolate origin
      for (let i = 0; i < 3; i++) {
        const p = cent.prev.origin[i]!;
        ent.origin[i] = ent.oldorigin[i] = fr(p + fr(cl.lerpfrac * fr(cent.current.origin[i]! - p)));
      }
    }

    // create a new entity

    // tweak the color of beams
    if (renderfx & RF_BEAM) {
      // the four beam colors are encoded in 32 bits of skinnum (hack)
      ent.alpha = fr(0.3);
      ent.skinnum = (s1.skinnum >> ((c.rand.rand() % 4) * 8)) & 0xff;
      ent.model = null;
    } else {
      // set skin
      if (s1.modelindex === 255) {
        // use custom player skin
        ent.skinnum = 0;
        const ci = cl.clientinfo[s1.skinnum & 0xff]!;
        ent.skin = ci.skin;
        ent.model = ci.model;
        if (!ent.skin || !ent.model) {
          ent.skin = cl.baseclientinfo.skin;
          ent.model = cl.baseclientinfo.model;
        }

        //============
        //PGM
        if (renderfx & RF_USE_DISGUISE) {
          const sn = skinName(c, ent.skin) ?? '';
          if (sn.startsWith('players/male')) {
            ent.skin = RegisterSkinSync(c, 'players/male/disguise.pcx');
            ent.model = RegisterModelSync(c, 'players/male/tris.md2');
          } else if (sn.startsWith('players/female')) {
            ent.skin = RegisterSkinSync(c, 'players/female/disguise.pcx');
            ent.model = RegisterModelSync(c, 'players/female/tris.md2');
          } else if (sn.startsWith('players/cyborg')) {
            ent.skin = RegisterSkinSync(c, 'players/cyborg/disguise.pcx');
            ent.model = RegisterModelSync(c, 'players/cyborg/tris.md2');
          }
        }
        //PGM
        //============
      } else {
        ent.skinnum = s1.skinnum;
        ent.skin = null;
        ent.model = cl.model_draw[s1.modelindex] ?? null;
      }
    }

    // only used for black hole model right now, FIXME: do better
    if (renderfx === RF_TRANSLUCENT) ent.alpha = fr(0.7);

    // render effects (fullbright, translucent, etc)
    if (effects & EF_COLOR_SHELL)
      ent.flags = 0; // renderfx go on color shell entity
    else ent.flags = renderfx | 0;

    // calculate angles
    if (effects & EF_ROTATE) {
      // some bonus items auto-rotate
      ent.angles[0] = 0;
      ent.angles[1] = autorotate;
      ent.angles[2] = 0;
    }
    // RAFAEL
    else if (effects & EF_SPINNINGLIGHTS) {
      ent.angles[0] = 0;
      ent.angles[1] = fr(anglemod((cl.time / 2) | 0) + s1.angles[1]!);
      ent.angles[2] = 180;
      {
        const forward = c.ents.forward;
        const start = c.ents.start;
        AngleVectors(ent.angles, forward, null, null);
        for (let k = 0; k < 3; k++) start[k] = fr(ent.origin[k]! + fr(64 * forward[k]!));
        V_AddLight(c, start, 100, 1, 0, 0);
      }
    } else {
      // interpolate angles
      for (let i = 0; i < 3; i++) {
        const a1 = cent.current.angles[i]!;
        const a2 = cent.prev.angles[i]!;
        ent.angles[i] = LerpAngle(a2, a1, cl.lerpfrac);
      }
    }

    if (s1.number === cl.playernum + 1) {
      ent.flags |= RF_VIEWERMODEL; // only draw from mirrors
      // FIXME: still pass to refresh

      if (effects & EF_FLAG1) V_AddLight(c, ent.origin, 225, 1.0, 0.1, 0.1);
      else if (effects & EF_FLAG2) V_AddLight(c, ent.origin, 225, 0.1, 0.1, 1.0);
      else if (effects & EF_TAGTRAIL)
        //PGM
        V_AddLight(c, ent.origin, 225, 1.0, 1.0, 0.0); //PGM
      else if (effects & EF_TRACKERTRAIL)
        //PGM
        V_AddLight(c, ent.origin, 225, -1.0, -1.0, -1.0); //PGM

      continue;
    }

    // if set to invisible, skip
    if (!s1.modelindex) continue;

    if (effects & EF_BFG) {
      ent.flags |= RF_TRANSLUCENT;
      ent.alpha = fr(0.3);
    }

    // RAFAEL
    if (effects & EF_PLASMA) {
      ent.flags |= RF_TRANSLUCENT;
      ent.alpha = fr(0.6);
    }

    if (effects & EF_SPHERETRANS) {
      ent.flags |= RF_TRANSLUCENT;
      // PMM - *sigh*  yet more EF overloading
      if (effects & EF_TRACKERTRAIL) ent.alpha = fr(0.6);
      else ent.alpha = fr(0.3);
    }
    //pmm

    // add to refresh list
    V_AddEntity(c, ent);

    // color shells generate a seperate entity for the main model
    if (effects & EF_COLOR_SHELL) {
      ent.flags = renderfx | RF_TRANSLUCENT | 0;
      ent.alpha = fr(0.3);
      V_AddEntity(c, ent);
    }

    ent.skin = null; // never use a custom skin on others
    ent.skinnum = 0;
    ent.flags = 0;
    ent.alpha = 0;

    // duplicate for linked models
    if (s1.modelindex2) {
      if (s1.modelindex2 === 255) {
        // custom weapon
        const ci = cl.clientinfo[s1.skinnum & 0xff]!;
        let i = s1.skinnum >> 8; // 0 is default weapon model
        if (!c.cv.cl_vwep.value || i > MAX_CLIENTWEAPONMODELS - 1) i = 0;
        // (a negative skinnum would index before the array in C)
        ent.model = ci.weaponmodel[i] ?? null;
        if (!ent.model) {
          if (i !== 0) ent.model = ci.weaponmodel[0]!;
          if (!ent.model) ent.model = cl.baseclientinfo.weaponmodel[0]!;
        }
      }
      //PGM - hack to allow translucent linked models (defender sphere's shell)
      //		set the high bit 0x80 on modelindex2 to enable translucency
      else if (s1.modelindex2 & 0x80) {
        ent.model = cl.model_draw[s1.modelindex2 & 0x7f] ?? null;
        ent.alpha = fr(0.32);
        ent.flags = RF_TRANSLUCENT;
      }
      //PGM
      else ent.model = cl.model_draw[s1.modelindex2] ?? null;
      V_AddEntity(c, ent);

      //PGM - make sure these get reset.
      ent.flags = 0;
      ent.alpha = 0;
      //PGM
    }
    if (s1.modelindex3) {
      ent.model = cl.model_draw[s1.modelindex3] ?? null;
      V_AddEntity(c, ent);
    }
    if (s1.modelindex4) {
      ent.model = cl.model_draw[s1.modelindex4] ?? null;
      V_AddEntity(c, ent);
    }

    if (effects & EF_POWERSCREEN) {
      // cl_mod_powerscreen is a cl_tent.c global (registered by CL_RegisterTEntModels)
      ent.model = c.fx.cl_mod_powerscreen;
      ent.oldframe = 0;
      ent.frame = 0;
      ent.flags |= RF_TRANSLUCENT | RF_SHELL_GREEN;
      ent.alpha = fr(0.3);
      V_AddEntity(c, ent);
    }

    // add automatic particle trails
    if (effects & ~EF_ROTATE) {
      if (effects & EF_ROCKET) {
        c.fx.rocketTrail(cent.lerp_origin, ent.origin, cent);
        V_AddLight(c, ent.origin, 200, 1, 1, 0);
      }
      // PGM - Do not reorder EF_BLASTER and EF_HYPERBLASTER.
      // EF_BLASTER | EF_TRACKER is a special case for EF_BLASTER2... Cheese!
      else if (effects & EF_BLASTER) {
        //PGM
        if (effects & EF_TRACKER) {
          // lame... problematic?
          c.fx.blasterTrail2(cent.lerp_origin, ent.origin);
          V_AddLight(c, ent.origin, 200, 0, 1, 0);
        } else {
          c.fx.blasterTrail(cent.lerp_origin, ent.origin);
          V_AddLight(c, ent.origin, 200, 1, 1, 0);
        }
        //PGM
      } else if (effects & EF_HYPERBLASTER) {
        if (effects & EF_TRACKER)
          // PGM	overloaded for blaster2.
          V_AddLight(c, ent.origin, 200, 0, 1, 0); // PGM
        // PGM
        else V_AddLight(c, ent.origin, 200, 1, 1, 0);
      } else if (effects & EF_GIB) {
        c.fx.diminishingTrail(cent.lerp_origin, ent.origin, cent, effects | 0);
      } else if (effects & EF_GRENADE) {
        c.fx.diminishingTrail(cent.lerp_origin, ent.origin, cent, effects | 0);
      } else if (effects & EF_FLIES) {
        c.fx.flyEffect(cent, ent.origin);
      } else if (effects & EF_BFG) {
        let i: number;
        if (effects & EF_ANIM_ALLFAST) {
          c.fx.bfgParticles(ent);
          i = 200;
        } else {
          // (C reads past the table for other frames)
          i = bfg_lightramp[s1.frame] ?? 0;
        }
        V_AddLight(c, ent.origin, i, 0, 1, 0);
      }
      // RAFAEL
      else if (effects & EF_TRAP) {
        ent.origin[2] = ent.origin[2]! + 32;
        c.fx.trapParticles(ent);
        const i = (c.rand.rand() % 100) + 100;
        V_AddLight(c, ent.origin, i, 1, 0.8, 0.1);
      } else if (effects & EF_FLAG1) {
        c.fx.flagTrail(cent.lerp_origin, ent.origin, 242);
        V_AddLight(c, ent.origin, 225, 1, 0.1, 0.1);
      } else if (effects & EF_FLAG2) {
        c.fx.flagTrail(cent.lerp_origin, ent.origin, 115);
        V_AddLight(c, ent.origin, 225, 0.1, 0.1, 1);
      }
      //======
      //ROGUE
      else if (effects & EF_TAGTRAIL) {
        c.fx.tagTrail(cent.lerp_origin, ent.origin, 220);
        V_AddLight(c, ent.origin, 225, 1.0, 1.0, 0.0);
      } else if (effects & EF_TRACKERTRAIL) {
        if (effects & EF_TRACKER) {
          const intensity = fr(50 + 500 * (Math.sin(cl.time / 500.0) + 1.0));
          // FIXME - check out this effect in rendition
          if (vidref_val === VIDREF_GL) V_AddLight(c, ent.origin, intensity, -1.0, -1.0, -1.0);
          else V_AddLight(c, ent.origin, fr(-1.0 * intensity), 1.0, 1.0, 1.0);
        } else {
          c.fx.trackerShell(cent.lerp_origin);
          V_AddLight(c, ent.origin, 155, -1.0, -1.0, -1.0);
        }
      } else if (effects & EF_TRACKER) {
        c.fx.trackerTrail(cent.lerp_origin, ent.origin, 0);
        // FIXME - check out this effect in rendition
        if (vidref_val === VIDREF_GL) V_AddLight(c, ent.origin, 200, -1, -1, -1);
        else V_AddLight(c, ent.origin, -200, 1, 1, 1);
      }
      //ROGUE
      //======
      // RAFAEL
      else if (effects & EF_GREENGIB) {
        c.fx.diminishingTrail(cent.lerp_origin, ent.origin, cent, effects | 0);
      }
      // RAFAEL
      else if (effects & EF_IONRIPPER) {
        c.fx.ionripperTrail(cent.lerp_origin, ent.origin);
        V_AddLight(c, ent.origin, 100, 1, 0.5, 0.5);
      }
      // RAFAEL
      else if (effects & EF_BLUEHYPERBLASTER) {
        V_AddLight(c, ent.origin, 200, 0, 0, 1);
      }
      // RAFAEL
      else if (effects & EF_PLASMA) {
        if (effects & EF_ANIM_ALLFAST) {
          c.fx.blasterTrail(cent.lerp_origin, ent.origin);
        }
        V_AddLight(c, ent.origin, 130, 1, 0.5, 0.5);
      }
    }

    cent.lerp_origin.set(ent.origin);
  }
}

// C: cl_ents.c:1293 CL_AddViewWeapon
export function CL_AddViewWeapon(c: ClientContext, ps: PlayerState, ops: PlayerState): void {
  const cl = c.cl;
  // allow the gun to be completely removed
  if (!c.cv.cl_gun.value) return;

  // don't draw gun if in wide angle view
  if (ps.fov > 90) return;

  const gun = c.ents.gun;
  clearRefEntity(gun);

  if (c.view.gun_model)
    gun.model = c.view.gun_model; // development tool
  else gun.model = cl.model_draw[ps.gunindex] ?? null;
  if (!gun.model) return;

  // set up gun position
  const lerp = cl.lerpfrac;
  for (let i = 0; i < 3; i++) {
    gun.origin[i] = fr(
      fr(cl.refdef.vieworg[i]! + ops.gunoffset[i]!) + fr(lerp * fr(ps.gunoffset[i]! - ops.gunoffset[i]!)),
    );
    gun.angles[i] = fr(cl.refdef.viewangles[i]! + LerpAngle(ops.gunangles[i]!, ps.gunangles[i]!, lerp));
  }

  if (c.view.gun_frame) {
    gun.frame = c.view.gun_frame; // development tool
    gun.oldframe = c.view.gun_frame; // development tool
  } else {
    gun.frame = ps.gunframe;
    if (gun.frame === 0)
      gun.oldframe = 0; // just changed weapons, don't lerp from old
    else gun.oldframe = ops.gunframe;
  }

  gun.flags = RF_MINLIGHT | RF_DEPTHHACK | RF_WEAPONMODEL;
  gun.backlerp = fr(1.0 - lerp);
  gun.oldorigin.set(gun.origin); // don't lerp at all
  V_AddEntity(c, gun);
}

// C: cl_ents.c:1352 CL_CalcViewValues -- sets cl.refdef view values
export function CL_CalcViewValues(c: ClientContext): void {
  const cl = c.cl;

  // find the previous frame to interpolate from
  const ps = cl.frame.playerstate;
  const i0 = (cl.frame.serverframe - 1) & UPDATE_MASK;
  let oldframe = cl.frames[i0]!;
  if (oldframe.serverframe !== cl.frame.serverframe - 1 || !oldframe.valid) oldframe = cl.frame; // previous frame was dropped or involid
  let ops = oldframe.playerstate;

  // see if the player entity was teleported this frame
  const po = ps.pmove.origin;
  const oo = ops.pmove.origin;
  if (
    Math.abs(oo[0]! - po[0]!) > 256 * 8 ||
    Math.abs(oo[1]! - po[1]!) > 256 * 8 ||
    Math.abs(oo[2]! - po[2]!) > 256 * 8
  )
    ops = ps; // don't interpolate

  const lerp = cl.lerpfrac;
  const vieworg = cl.refdef.vieworg;

  // calculate the origin
  if (c.cv.cl_predict.value && !(cl.frame.playerstate.pmove.pm_flags & PMF_NO_PREDICTION)) {
    // use predicted values
    const backlerp = fr(1.0 - lerp);
    for (let i = 0; i < 3; i++) {
      vieworg[i] = fr(
        fr(
          fr(cl.predicted_origin[i]! + ops.viewoffset[i]!) +
            fr(cl.lerpfrac * fr(ps.viewoffset[i]! - ops.viewoffset[i]!)),
        ) - fr(backlerp * cl.prediction_error[i]!),
      );
    }

    // smooth out stair climbing
    const delta = (c.cls.realtime - cl.predicted_step_time) >>> 0;
    if (delta < 100) vieworg[2] = vieworg[2]! - fr(cl.predicted_step * fr(100 - delta)) * 0.01;
  } else {
    // just use interpolated values
    for (let i = 0; i < 3; i++)
      vieworg[i] =
        oo[i]! * 0.125 +
        ops.viewoffset[i]! +
        lerp * (po[i]! * 0.125 + ps.viewoffset[i]! - (oo[i]! * 0.125 + ops.viewoffset[i]!));
  }

  // if not running a demo or on a locked frame, add the local angle movement
  const va = cl.refdef.viewangles;
  if (cl.frame.playerstate.pmove.pm_type < PM_DEAD) {
    // use predicted values
    for (let i = 0; i < 3; i++) va[i] = cl.predicted_angles[i]!;
  } else {
    // just use interpolated values
    for (let i = 0; i < 3; i++) va[i] = LerpAngle(ops.viewangles[i]!, ps.viewangles[i]!, lerp);
  }

  for (let i = 0; i < 3; i++) va[i] = fr(va[i]! + LerpAngle(ops.kick_angles[i]!, ps.kick_angles[i]!, lerp));

  AngleVectors(va, cl.v_forward, cl.v_right, cl.v_up);

  // interpolate field of view
  cl.refdef.fov_x = fr(ops.fov + fr(lerp * fr(ps.fov - ops.fov)));

  // don't interpolate blend color
  for (let i = 0; i < 4; i++) cl.refdef.blend[i] = ps.blend[i]!;

  // add the weapon
  CL_AddViewWeapon(c, ps, ops);
}

// C: cl_ents.c:1438 CL_AddEntities -- emits all entities, particles, and lights to the refresh
export function CL_AddEntities(c: ClientContext): void {
  const cl = c.cl;
  if (c.cls.state !== ca_active) return;

  if (cl.time > cl.frame.servertime) {
    if (c.cv.cl_showclamp.value) Com_Printf(c, 'high clamp %i\n', cl.time - cl.frame.servertime);
    cl.time = cl.frame.servertime;
    cl.lerpfrac = 1.0;
  } else if (cl.time < cl.frame.servertime - 100) {
    if (c.cv.cl_showclamp.value) Com_Printf(c, 'low clamp %i\n', cl.frame.servertime - 100 - cl.time);
    cl.time = cl.frame.servertime - 100;
    cl.lerpfrac = 0;
  } else cl.lerpfrac = fr(1.0 - (cl.frame.servertime - cl.time) * 0.01);

  if (c.cv.cl_timedemo.value) cl.lerpfrac = 1.0;

  CL_CalcViewValues(c);
  // PMM - moved this here so the heat beam has the right values for the vieworg, and can lock the beam to the gun
  CL_AddPacketEntities(c, cl.frame);
  c.fx.addTEnts();
  c.fx.addParticles();
  c.fx.addDLights();
  c.fx.addLightStyles();
}

// C: cl_ents.c:1490 CL_GetEntitySoundOrigin -- called to get the sound spatialization origin
export function CL_GetEntitySoundOrigin(c: ClientContext, ent: number, org: Float32Array): void {
  if (ent < 0 || ent >= MAX_EDICTS) Com_Error(c, ERR_DROP, 'CL_GetEntitySoundOrigin: bad ent');
  const old = c.cl_entities[ent]!;
  org.set(old.lerp_origin);
  // FIXME: bmodel issues...
}
