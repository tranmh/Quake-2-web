// The real Effects implementation: binds the Effects interface (effects.ts) to the ports of
// client/cl_fx.c, cl_tent.c and cl_newfx.c. All state lives in one FxState instance.
import type { EntityState } from 'q2-shared';
import type { SizeBuf } from 'q2-protocol';
import type { ModelHandle, RefEntity } from 'q2-ref';
import type { CEntity, ClientContext } from './client';
import type { Effects } from './effects';
import {
  FxState,
  CL_AddDLights,
  CL_AddLightStyles,
  CL_AddParticles,
  CL_BfgParticles,
  CL_BlasterTrail,
  CL_ClearEffects,
  CL_DiminishingTrail,
  CL_EntityEvent,
  CL_FlagTrail,
  CL_FlyEffect,
  CL_IonripperTrail,
  CL_ParseMuzzleFlash,
  CL_ParseMuzzleFlash2,
  CL_RocketTrail,
  CL_RunDLights,
  CL_RunLightStyles,
  CL_SetLightstyle,
  CL_TeleporterParticles,
  CL_TrapParticles,
} from './cl_fx';
import {
  CL_AddTEnts,
  CL_ClearTEnts,
  CL_ParseTEnt,
  CL_RegisterTEntModels,
  CL_RegisterTEntSounds,
} from './cl_tent';
import { CL_BlasterTrail2, CL_TagTrail, CL_Tracker_Shell, CL_TrackerTrail } from './cl_newfx';

export class ClientEffects implements Effects {
  readonly fx = new FxState();

  get cl_mod_powerscreen(): ModelHandle | null {
    return this.fx.tent.cl_mod_powerscreen;
  }

  attach(c: ClientContext): void {
    this.fx.c = c;
  }

  // ---- cl_fx.c
  clearEffects(): void {
    CL_ClearEffects(this.fx);
  }
  setLightstyle(i: number): void {
    CL_SetLightstyle(this.fx, i);
  }
  runDLights(): void {
    CL_RunDLights(this.fx);
  }
  runLightStyles(): void {
    CL_RunLightStyles(this.fx);
  }
  parseMuzzleFlash(msg: SizeBuf): void {
    CL_ParseMuzzleFlash(this.fx, msg);
  }
  parseMuzzleFlash2(msg: SizeBuf): void {
    CL_ParseMuzzleFlash2(this.fx, msg);
  }
  entityEvent(ent: EntityState): void {
    CL_EntityEvent(this.fx, ent);
  }
  teleporterParticles(ent: EntityState): void {
    CL_TeleporterParticles(this.fx, ent);
  }
  addParticles(): void {
    CL_AddParticles(this.fx);
  }
  addDLights(): void {
    CL_AddDLights(this.fx);
  }
  addLightStyles(): void {
    CL_AddLightStyles(this.fx);
  }
  rocketTrail(start: Float32Array, end: Float32Array, old: CEntity): void {
    CL_RocketTrail(this.fx, start, end, old);
  }
  blasterTrail(start: Float32Array, end: Float32Array): void {
    CL_BlasterTrail(this.fx, start, end);
  }
  diminishingTrail(start: Float32Array, end: Float32Array, old: CEntity, flags: number): void {
    CL_DiminishingTrail(this.fx, start, end, old, flags);
  }
  flyEffect(ent: CEntity, origin: Float32Array): void {
    CL_FlyEffect(this.fx, ent, origin);
  }
  bfgParticles(ent: RefEntity): void {
    CL_BfgParticles(this.fx, ent);
  }
  trapParticles(ent: RefEntity): void {
    CL_TrapParticles(this.fx, ent);
  }
  flagTrail(start: Float32Array, end: Float32Array, color: number): void {
    CL_FlagTrail(this.fx, start, end, color);
  }
  ionripperTrail(start: Float32Array, end: Float32Array): void {
    CL_IonripperTrail(this.fx, start, end);
  }

  // ---- cl_newfx.c
  blasterTrail2(start: Float32Array, end: Float32Array): void {
    CL_BlasterTrail2(this.fx, start, end);
  }
  tagTrail(start: Float32Array, end: Float32Array, color: number): void {
    CL_TagTrail(this.fx, start, end, color);
  }
  trackerShell(origin: Float32Array): void {
    CL_Tracker_Shell(this.fx, origin);
  }
  trackerTrail(start: Float32Array, end: Float32Array, particleColor: number): void {
    CL_TrackerTrail(this.fx, start, end, particleColor);
  }

  // ---- cl_tent.c
  clearTEnts(): void {
    CL_ClearTEnts(this.fx);
  }
  registerTEntSounds(): void {
    CL_RegisterTEntSounds(this.fx);
  }
  registerTEntModels(): Promise<void> {
    return CL_RegisterTEntModels(this.fx);
  }
  parseTEnt(msg: SizeBuf): void {
    CL_ParseTEnt(this.fx, msg);
  }
  addTEnts(): void {
    CL_AddTEnts(this.fx);
  }
}

/** The ported cl_fx.c / cl_tent.c / cl_newfx.c effects (default of createClientEngine). */
export function createClientEffects(): ClientEffects {
  return new ClientEffects();
}
