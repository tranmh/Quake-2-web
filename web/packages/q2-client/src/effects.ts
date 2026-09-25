// The client's view of the local effects modules: client/cl_fx.c, cl_tent.c, cl_newfx.c. Implemented by
// the effects package; the methods are called at exactly the points the original client calls the
// corresponding CL_* function. NullEffects consumes temp-entity / muzzle-flash messages correctly but
// draws nothing.
import type { EntityState } from 'q2-shared';
import type { SizeBuf } from 'q2-protocol';
import type { ModelHandle, RefEntity } from 'q2-ref';
import type { CEntity, ClientContext } from './client';

export interface Effects {
  /**
   * Called once when the engine is created. Effects code reads client globals through the context
   * (c.cl.time, c.cl.frame, c.cl_entities, c.cl.configstrings, c.cl.model_draw, c.cl.v_forward, c.cl.refdef ...) and
   * adds to the scene with V_AddEntity / V_AddParticle / V_AddLight / V_AddLightStyle from cl_view.ts, and
   * plays sounds with c.sound. Do not cache c.cl sub-objects across frames; the instance is stable but
   * its contents are reset on every map change.
   */
  attach(c: ClientContext): void;

  // ---- cl_fx.c
  /** CL_ClearEffects (particles, dlights, lightstyles) -- called from CL_ClearState */
  clearEffects(): void;
  /** CL_SetLightstyle(i): cl.configstrings[CS_LIGHTS+i] changed */
  setLightstyle(i: number): void;
  /** CL_RunDLights -- once per CL_Frame after SCR_UpdateScreen */
  runDLights(): void;
  /** CL_RunLightStyles -- once per CL_Frame after SCR_UpdateScreen */
  runLightStyles(): void;
  /** CL_ParseMuzzleFlash: reads the svc_muzzleflash payload from msg */
  parseMuzzleFlash(msg: SizeBuf): void;
  /** CL_ParseMuzzleFlash2: reads the svc_muzzleflash2 payload from msg */
  parseMuzzleFlash2(msg: SizeBuf): void;
  /** CL_EntityEvent (entity_state_t.event != 0), from CL_FireEntityEvents */
  entityEvent(ent: EntityState): void;
  /** CL_TeleporterParticles (EF_TELEPORTER), from CL_FireEntityEvents */
  teleporterParticles(ent: EntityState): void;
  /** CL_AddParticles, CL_AddDLights, CL_AddLightStyles -- from CL_AddEntities */
  addParticles(): void;
  addDLights(): void;
  addLightStyles(): void;
  // trails / entity effects from CL_AddPacketEntities (start/end are float32[3], do not retain)
  rocketTrail(start: Float32Array, end: Float32Array, old: CEntity): void;
  blasterTrail(start: Float32Array, end: Float32Array): void;
  diminishingTrail(start: Float32Array, end: Float32Array, old: CEntity, flags: number): void;
  flyEffect(ent: CEntity, origin: Float32Array): void;
  bfgParticles(ent: RefEntity): void;
  trapParticles(ent: RefEntity): void;
  flagTrail(start: Float32Array, end: Float32Array, color: number): void;
  ionripperTrail(start: Float32Array, end: Float32Array): void;
  // ---- cl_newfx.c (PGM)
  blasterTrail2(start: Float32Array, end: Float32Array): void;
  tagTrail(start: Float32Array, end: Float32Array, color: number): void;
  trackerShell(origin: Float32Array): void;
  trackerTrail(start: Float32Array, end: Float32Array, particleColor: number): void;

  // ---- cl_tent.c
  /** cl_tent.c global cl_mod_powerscreen (set by registerTEntModels; used by CL_AddPacketEntities) */
  readonly cl_mod_powerscreen: ModelHandle | null;
  /** CL_ClearTEnts -- called from CL_ClearState */
  clearTEnts(): void;
  /** CL_RegisterTEntSounds -- from CL_RegisterSounds (use c.sound.registerSound) */
  registerTEntSounds(): void;
  /** CL_RegisterTEntModels -- from CL_PrepRefresh (awaited; use c.re.registerModel / registerPic) */
  registerTEntModels(): Promise<void>;
  /** CL_ParseTEnt: reads the svc_temp_entity payload from msg */
  parseTEnt(msg: SizeBuf): void;
  /** CL_AddTEnts -- from CL_AddEntities */
  addTEnts(): void;
}
