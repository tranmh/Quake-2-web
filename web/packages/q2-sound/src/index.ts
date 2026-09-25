// q2-sound: port of client/snd_dma.c + client/snd_mix.c (+ S_LoadSound of snd_mem.c).
//
// Architecture (decision): the ENTIRE mixer core of snd_dma.c/snd_mix.c -- channels, S_PickChannel, the
// playsound queue with s_beginofs drift, S_IssuePlaysound, paintedtime/soundtime, S_Spatialize,
// S_AddLoopSounds, raw samples, the paint buffer and the transfer into a 16 bit stereo DMA ring -- runs
// in one SoundCore instance, hosted inside an AudioWorkletProcessor in the browser (worklet.ts) or
// in-process in node (createNullSound). Channel ownership therefore always stays consistent with
// paintedtime, exactly as in the single threaded original. The main thread (MainSound, implementing the
// q2-client Sound interface) keeps the sfx registry (S_FindName / S_RegisterSound / S_AliasName /
// S_RegisterSexedSound / S_EndRegistration), loads and resamples WAVs (q2-formats) at dma.speed, owns
// the cvars and console commands, and forwards every S_* call as an ordered binary command. Everything
// the C mixer read from client globals (cl.playernum, cls.state, cl_paused, cl.sound_prepped,
// cls.disable_screen, cl.frame entity sounds, CL_GetEntitySoundOrigin for dynamically sourced sounds,
// cvar values) is captured at S_Update time and sent with the update command, so the core sees the
// same values the C code would read during S_Update/S_Update_.
//
// Deviations (TODO-IMPROVE / async):
// - FS_LoadFile / FS_FOpenFile are asynchronous: a sound not yet resident when S_StartSound is called
//   is started when its data arrives (C blocks on the disk); sexed-sound existence checks likewise.
//   A failed load is retried at the next registration instead of on every S_LoadSound.
// - S_PaintChannelFrom8 follows the id386 assembly of the retail x86 builds (scaletable row vol>>3);
//   the portable C `vol>>11` (silent 8 bit sounds) is available with portableC8bit.
// - dma: 16 bit stereo, 0x10000 byte ring, submission_chunk 1, speed from s_khz like snd_win.c.
export * from './snd_core';
export * from './protocol';
export * from './host';
export * from './ring';
export * from './snd_dma';
export * from './offline';
export * from './web';
export * from './worklet_types';

import { MainSound } from './snd_dma';
import { OfflineBackend, type OfflineBackendOptions } from './offline';
import { WebAudioBackend, type WebBackendOptions } from './web';

/** The browser sound system (AudioWorklet mixer). */
export function createWebSound(opts: WebBackendOptions = {}): MainSound & { backend: WebAudioBackend } {
  return new MainSound({ backend: new WebAudioBackend(opts) }) as MainSound & { backend: WebAudioBackend };
}

/**
 * Sound system for node / headless use: the same mixer runs in-process against a clock-driven fake DMA
 * device; nothing is played. `backend.core` exposes the mixer for inspection.
 */
export function createNullSound(opts: OfflineBackendOptions = {}): MainSound & { backend: OfflineBackend } {
  return new MainSound({ backend: new OfflineBackend(opts) }) as MainSound & { backend: OfflineBackend };
}
