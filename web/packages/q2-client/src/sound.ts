// The client's view of the sound system: the main-thread half of client/snd_dma.c (S_* entry points
// called by the client code). Implemented elsewhere (AudioWorklet mixer); NullSound is the no-op default.
import type { ClientContext } from './client';

/** Opaque handle to a registered sound (C: struct sfx_s *). */
export interface SfxHandle {
  readonly __sfx: unique symbol;
}

/**
 * C: snd_dma.c public API (sound.h). Called at exactly the points the original client calls S_*.
 * Vectors are float32 [3] arrays; do not keep references to them after the call returns.
 */
export interface Sound {
  /**
   * Called once when the engine is created. The implementation may read client state it needs
   * (C globals used by snd_dma.c: cl.frame / cl_parse_entities for S_AddLoopSounds, cl.sound_precache,
   * cl.configstrings / cl.clientinfo for S_RegisterSexedSound, CL_GetEntitySoundOrigin from cl_ents.ts,
   * cl_paused / cls.disable_screen / cls.state).
   */
  attach(c: ClientContext): void;
  /** S_Init */
  init(): void;
  /** S_Shutdown */
  shutdown(): void;
  /** S_BeginRegistration */
  beginRegistration(): void;
  /**
   * S_RegisterSound: returns a handle immediately (S_FindName); the sample may load lazily. Names
   * starting with '*' are sexed sounds (resolved at start time). Returns null only for invalid names.
   */
  registerSound(name: string): SfxHandle | null;
  /** S_EndRegistration: frees unused sounds and loads all registered ones. */
  endRegistration(): Promise<void>;
  /**
   * S_StartSound. `origin` null = use the entity's origin (CL_GetEntitySoundOrigin) each mix;
   * entnum/entchannel as in the protocol; fvol 0..1; attenuation ATTN_*; timeofs seconds.
   */
  startSound(
    origin: Float32Array | null,
    entnum: number,
    entchannel: number,
    sfx: SfxHandle | null,
    fvol: number,
    attenuation: number,
    timeofs: number,
  ): void;
  /** S_StartLocalSound (menu/HUD sounds, "talk" etc.) */
  startLocalSound(name: string): void;
  /** S_StopAllSounds */
  stopAllSounds(): void;
  /** S_Update: called once per client frame with the listener orientation (cl.refdef.vieworg, cl.v_*). */
  update(origin: Float32Array, forward: Float32Array, right: Float32Array, up: Float32Array): void;
  /** S_RawSamples (cinematic audio) */
  rawSamples(samples: number, rate: number, width: number, channels: number, data: Uint8Array): void;
}

/** No-op sound system (keeps handles so precache bookkeeping behaves as with a real mixer). */
export class NullSound implements Sound {
  private readonly known = new Map<string, SfxHandle>();
  attach(): void {}
  init(): void {}
  shutdown(): void {}
  beginRegistration(): void {}
  registerSound(name: string): SfxHandle | null {
    if (!name) return null;
    let h = this.known.get(name);
    if (!h) {
      h = { name } as unknown as SfxHandle;
      this.known.set(name, h);
    }
    return h;
  }
  async endRegistration(): Promise<void> {}
  startSound(): void {}
  startLocalSound(): void {}
  stopAllSounds(): void {}
  update(): void {}
  rawSamples(): void {}
}
