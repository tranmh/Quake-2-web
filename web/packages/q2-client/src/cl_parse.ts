// Port of client/cl_parse.c -- parse a message received from the server.
// Downloads (svc_download / CL_CheckOrDownloadFile / download command) are not supported: every asset is
// served over HTTP by the asset service, so the client never requests files from the game server.
import {
  CS_CDTRACK,
  CS_IMAGES,
  CS_LIGHTS,
  CS_MODELS,
  CS_PLAYERSKINS,
  CS_SOUNDS,
  DEFAULT_SOUND_PACKET_ATTENUATION,
  DEFAULT_SOUND_PACKET_VOLUME,
  ERR_DISCONNECT,
  ERR_DROP,
  MAX_CLIENTS,
  MAX_CONFIGSTRINGS,
  MAX_EDICTS,
  MAX_LIGHTSTYLES,
  MAX_MODELS,
  MAX_QPATH,
  MAX_SOUNDS,
  PRINT_CHAT,
  PROTOCOL_VERSION,
  Q_stricmp,
  SND_ATTENUATION,
  SND_ENT,
  SND_OFFSET,
  SND_POS,
  SND_VOLUME,
  EntityState,
  fr,
  svc_centerprint,
  svc_configstring,
  svc_deltapacketentities,
  svc_disconnect,
  svc_download,
  svc_frame,
  svc_inventory,
  svc_layout,
  svc_muzzleflash,
  svc_muzzleflash2,
  svc_nop,
  svc_packetentities,
  svc_playerinfo,
  svc_print,
  svc_reconnect,
  svc_serverdata,
  svc_sound,
  svc_spawnbaseline,
  svc_stufftext,
  svc_temp_entity,
} from 'q2-shared';
import { MSG_ReadByte, MSG_ReadLong, MSG_ReadPos, MSG_ReadShort, MSG_ReadString } from 'q2-protocol';
import type { ImageHandle, ModelHandle } from 'q2-ref';
import {
  ca_connected,
  ca_connecting,
  ClientInfo,
  Com_DPrintf,
  Com_Error,
  Com_Printf,
  type ClientContext,
} from './client';
import { CL_ClearState, CL_WriteDemoMessage } from './cl_main';
import { CL_ParseDelta, CL_ParseEntityBits, CL_ParseFrame } from './cl_ents';
import { CL_AddNetgraph, SCR_CenterPrint } from './cl_scrn';
import { CL_ParseInventory } from './cl_inv';
import { SCR_PlayCinematic } from './cinematic';
import { RegisterModel, RegisterPic, RegisterSkin } from './regcache';

// C: cl_parse.c:24 svc_strings
export const svc_strings: readonly (string | undefined)[] = [
  'svc_bad',
  'svc_muzzleflash',
  'svc_muzzlflash2',
  'svc_temp_entity',
  'svc_layout',
  'svc_inventory',
  'svc_nop',
  'svc_disconnect',
  'svc_reconnect',
  'svc_sound',
  'svc_print',
  'svc_stufftext',
  'svc_serverdata',
  'svc_configstring',
  'svc_spawnbaseline',
  'svc_centerprint',
  'svc_download',
  'svc_playerinfo',
  'svc_packetentities',
  'svc_deltapacketentities',
  'svc_frame',
];

const nullstate = new EntityState();

//=============================================================================

// C: cl_parse.c:53 CL_DownloadFileName (kept for parity; there is no local file system to write to)
export function CL_DownloadFileName(c: ClientContext, fn: string): string {
  if (fn.startsWith('players')) return 'baseq2/' + fn;
  return (c.cvars.variableString('game') || 'baseq2') + '/' + fn;
}

// C: cl_parse.c:69 CL_CheckOrDownloadFile
// Returns true if the file exists. Browser: assets come from the HTTP asset service, so the file is
// always considered present and no download is ever started.
export function CL_CheckOrDownloadFile(c: ClientContext, filename: string): boolean {
  if (filename.includes('..')) {
    Com_Printf(c, 'Refusing to download a path with ..\n');
    return true;
  }
  return true;
}

// C: cl_parse.c:132 CL_Download_f -- request a download from the server (not supported)
export function CL_Download_f(c: ClientContext): void {
  if (c.cmd.argc() !== 2) {
    Com_Printf(c, 'Usage: download <filename>\n');
    return;
  }
  const filename = c.cmd.argv(1);
  if (filename.includes('..')) {
    Com_Printf(c, 'Refusing to download a path with ..\n');
    return;
  }
  Com_Printf(c, 'Downloading from the game server is not supported (assets are loaded over HTTP).\n');
}

// C: cl_parse.c:176 CL_RegisterSounds -- returns S_EndRegistration's promise (sample loading)
export function CL_RegisterSounds(c: ClientContext): Promise<void> {
  const cl = c.cl;
  c.sound.beginRegistration();
  c.fx.registerTEntSounds();
  for (let i = 1; i < MAX_SOUNDS; i++) {
    if (!cl.configstrings[CS_SOUNDS + i]) break;
    cl.sound_precache[i] = c.sound.registerSound(cl.configstrings[CS_SOUNDS + i]!);
  }
  return c.sound.endRegistration();
}

// C: cl_parse.c:200 CL_ParseDownload
// The payload is consumed exactly as in C; the data is discarded because this client never requests
// downloads (no "nextdl" is sent, so a stray download simply stops).
export function CL_ParseDownload(c: ClientContext): void {
  const msg = c.net_message;
  // read the data
  const size = MSG_ReadShort(msg);
  MSG_ReadByte(msg); // percent
  if (size === -1) {
    Com_Printf(c, 'Server does not have this file.\n');
    return;
  }
  msg.readcount += size;
  Com_Printf(c, 'Ignoring download data (downloads are not supported).\n');
}

/*
=====================================================================

  SERVER CONNECTING MESSAGES

=====================================================================
*/

// C: cl_parse.c:298 CL_ParseServerData
export function CL_ParseServerData(c: ClientContext): void {
  const cl = c.cl;
  const cls = c.cls;
  const msg = c.net_message;

  Com_DPrintf(c, 'Serverdata packet received.\n');
  //
  // wipe the client_state_t struct
  //
  CL_ClearState(c);
  c.ents.configstring_bytes.fill(0);
  cls.state = ca_connected;

  // parse protocol version number
  const i = MSG_ReadLong(msg);
  cls.serverProtocol = i;

  // BIG HACK to let demos from release work with the 3.0x patch!!! (Com_ServerState() is always 0 here)
  if (i !== PROTOCOL_VERSION)
    Com_Error(c, ERR_DROP, 'Server returned version %i, not %i', i, PROTOCOL_VERSION);

  cl.servercount = MSG_ReadLong(msg);
  cl.attractloop = MSG_ReadByte(msg) !== 0;

  // game directory
  let str = MSG_ReadString(msg);
  cl.gamedir = str.slice(0, MAX_QPATH - 1);

  // set gamedir (fs_gamedirvar->string is never NULL, so an empty gamedir always sets "game")
  const game = c.cvars.variableString('game');
  if ((str && (!game || game !== str)) || !str) c.cvars.set('game', str);

  // parse player entity number
  cl.playernum = MSG_ReadShort(msg);

  // get the full level name
  str = MSG_ReadString(msg);

  if (cl.playernum === -1) {
    // playing a cinematic or showing a pic, not a level
    SCR_PlayCinematic(c, str);
  } else {
    // seperate the printfs so the server message can have a color
    Com_Printf(
      c,
      '\n\n\x1d\x1e\x1e\x1e\x1e\x1e\x1e\x1e\x1e\x1e\x1e\x1e\x1e\x1e\x1e\x1e\x1e\x1e\x1e\x1e\x1e\x1e\x1e\x1e\x1e\x1e\x1e\x1e\x1e\x1e\x1e\x1e\x1e\x1e\x1e\x1e\x1f\n\n',
    );
    Com_Printf(c, '%c%s\n', 2, str);

    // need to prep refresh at next oportunity
    cl.refresh_prepped = false;
  }
}

// C: cl_parse.c:359 CL_ParseBaseline
export function CL_ParseBaseline(c: ClientContext): void {
  const eb = CL_ParseEntityBits(c);
  const es = c.cl_entities[eb.number]!.baseline;
  CL_ParseDelta(c, nullstate, es, eb.number, eb.bits);
}

/**
 * C: cl_parse.c:380 CL_LoadClientinfo. The registrations are asynchronous; the handles are stored into
 * `ci` only when every await completed for the same level (c.clearGeneration) and no newer load of the
 * same clientinfo was started meanwhile.
 */
export async function CL_LoadClientinfo(c: ClientContext, ci: ClientInfo, s: string): Promise<void> {
  const gen = c.clearGeneration;
  const tokens = c.ents.clientinfoTokens;
  const token = (tokens.get(ci) ?? 0) + 1;
  tokens.set(ci, token);
  const stale = (): boolean => c.clearGeneration !== gen || tokens.get(ci) !== token;

  ci.cinfo = s.slice(0, MAX_QPATH - 1);

  // isolate the player's name
  ci.name = s.slice(0, MAX_QPATH - 1);
  const t = s.indexOf('\\');
  if (t >= 0) {
    ci.name = ci.name.slice(0, t);
    s = s.slice(t + 1);
  }

  let model: ModelHandle | null;
  let skin: ImageHandle | null;
  let icon: ImageHandle | null;
  let iconname: string;
  const weaponmodel: (ModelHandle | null)[] = new Array<ModelHandle | null>(ci.weaponmodel.length).fill(null);

  const noskins = !!c.cv.cl_noskins.value || !s;
  let wcount = 0;
  if (noskins) {
    const model_filename = 'players/male/tris.md2';
    const weapon_filename = 'players/male/weapon.md2';
    const skin_filename = 'players/male/grunt.pcx';
    iconname = '/players/male/grunt_i.pcx';
    model = await RegisterModel(c, model_filename);
    weaponmodel[0] = await RegisterModel(c, weapon_filename);
    wcount = 1;
    skin = await RegisterSkin(c, skin_filename);
    icon = await RegisterPic(c, iconname);
  } else {
    // isolate the model name
    let model_name = s;
    let tt = model_name.indexOf('/');
    if (tt < 0) tt = model_name.indexOf('\\');
    if (tt < 0) tt = 0;
    model_name = model_name.slice(0, tt);

    // isolate the skin name
    const skin_name = s.slice(model_name.length + 1);

    // model file
    let model_filename = `players/${model_name}/tris.md2`.slice(0, MAX_QPATH - 1);
    model = await RegisterModel(c, model_filename);
    if (!model) {
      model_name = 'male';
      model_filename = 'players/male/tris.md2';
      model = await RegisterModel(c, model_filename);
    }

    // skin file
    let skin_filename = `players/${model_name}/${skin_name}.pcx`.slice(0, MAX_QPATH - 1);
    skin = await RegisterSkin(c, skin_filename);

    // if we don't have the skin and the model wasn't male,
    // see if the male has it (this is for CTF's skins)
    if (!skin && Q_stricmp(model_name, 'male')) {
      // change model to male
      model_name = 'male';
      model_filename = 'players/male/tris.md2';
      model = await RegisterModel(c, model_filename);

      // see if the skin exists for the male model
      skin_filename = `players/${model_name}/${skin_name}.pcx`.slice(0, MAX_QPATH - 1);
      skin = await RegisterSkin(c, skin_filename);
    }

    // if we still don't have a skin, it means that the male model didn't have
    // it, so default to grunt
    if (!skin) {
      // see if the skin exists for the male model
      skin_filename = `players/${model_name}/grunt.pcx`.slice(0, MAX_QPATH - 1);
      skin = await RegisterSkin(c, skin_filename);
    }

    // weapon file
    const view = c.view;
    for (let i = 0; i < view.num_cl_weaponmodels; i++) {
      let weapon_filename = `players/${model_name}/${view.cl_weaponmodels[i]}`.slice(0, MAX_QPATH - 1);
      weaponmodel[i] = await RegisterModel(c, weapon_filename);
      if (!weaponmodel[i] && model_name === 'cyborg') {
        // try male
        weapon_filename = `players/male/${view.cl_weaponmodels[i]}`.slice(0, MAX_QPATH - 1);
        weaponmodel[i] = await RegisterModel(c, weapon_filename);
      }
      wcount = i + 1;
      if (!c.cv.cl_vwep.value) break; // only one when vwep is off
    }

    // icon file
    iconname = `/players/${model_name}/${skin_name}_i.pcx`.slice(0, MAX_QPATH - 1);
    icon = await RegisterPic(c, iconname);
  }

  if (stale()) return;

  ci.iconname = iconname;
  ci.model = model;
  ci.skin = skin;
  ci.icon = icon;
  // the noskins path memsets the weapon list; the other path overwrites only the entries it registered
  if (noskins) ci.weaponmodel.fill(null);
  for (let i = 0; i < wcount; i++) ci.weaponmodel[i] = weaponmodel[i]!;

  // must have loaded all data types to be valud
  if (!ci.skin || !ci.icon || !ci.model || !ci.weaponmodel[0]) {
    ci.skin = null;
    ci.icon = null;
    ci.model = null;
    ci.weaponmodel[0] = null;
    return;
  }
}

// C: cl_parse.c:501 CL_ParseClientinfo -- load the skin, icon, and model for a client
// Returns the load promise (callers inside a frame ignore it; CL_PrepRefresh awaits it).
export function CL_ParseClientinfo(c: ClientContext, player: number): Promise<void> {
  const s = c.cl.configstrings[player + CS_PLAYERSKINS]!;
  const ci = c.cl.clientinfo[player]!;
  return CL_LoadClientinfo(c, ci, s).catch((e: unknown) => {
    Com_DPrintf(c, 'CL_LoadClientinfo: %s\n', String(e));
  });
}

/**
 * `strcpy(cl.configstrings[i], s)` into `char configstrings[MAX_CONFIGSTRINGS][MAX_QPATH]`: strings longer
 * than MAX_QPATH-1 run into the following slots (the server relies on this for CS_STATUSBAR, sending the
 * full string at CS_STATUSBAR and its 64-byte tails in the next slots). The flat C array is emulated
 * with a byte buffer; every slot whose C string changed is re-derived from it. The copy is clamped to
 * the end of the array (the C strcpy would write past it).
 */
export function CL_StoreConfigString(c: ClientContext, i: number, s: string): void {
  const buf = c.ents.configstring_bytes;
  const base = i * MAX_QPATH;
  const n = Math.min(s.length, buf.length - base - 1);
  for (let k = 0; k < n; k++) buf[base + k] = s.charCodeAt(k) & 255;
  buf[base + n] = 0;
  const cs = c.cl.configstrings;
  const derive = (j: number): void => {
    const o = j * MAX_QPATH;
    let e = o;
    while (e < buf.length && buf[e] !== 0) e++;
    let str = '';
    for (let k = o; k < e; k++) str += String.fromCharCode(buf[k]!);
    cs[j] = str;
  };
  const last = Math.min(MAX_CONFIGSTRINGS - 1, i + Math.floor(n / MAX_QPATH));
  for (let j = i; j <= last; j++) derive(j);
  // earlier slots whose string runs (without a NUL) into slot i see the new bytes too
  for (let j = i - 1; j >= 0; j--) {
    const o = j * MAX_QPATH;
    let hasNul = false;
    for (let k = o; k < o + MAX_QPATH; k++)
      if (buf[k] === 0) {
        hasNul = true;
        break;
      }
    if (hasNul) break;
    derive(j);
  }
}

// C: cl_parse.c:519 CL_ParseConfigString
export function CL_ParseConfigString(c: ClientContext): void {
  const cl = c.cl;
  const msg = c.net_message;

  const i = MSG_ReadShort(msg);
  if (i < 0 || i >= MAX_CONFIGSTRINGS) Com_Error(c, ERR_DROP, 'configstring > MAX_CONFIGSTRINGS');
  const s = MSG_ReadString(msg);
  CL_StoreConfigString(c, i, s);

  // do something apropriate
  if (i >= CS_LIGHTS && i < CS_LIGHTS + MAX_LIGHTSTYLES) c.fx.setLightstyle(i - CS_LIGHTS);
  else if (i === CS_CDTRACK) {
    // CDAudio_Play: no CD audio in the browser
  } else if (i >= CS_MODELS && i < CS_MODELS + MAX_MODELS) {
    if (cl.refresh_prepped) {
      const gen = c.clearGeneration;
      const idx = i - CS_MODELS;
      void RegisterModel(c, s).then((h) => {
        if (c.clearGeneration === gen && cl.configstrings[i] === s) cl.model_draw[idx] = h;
      });
      if (s[0] === '*') {
        if (c.cmModel) {
          try {
            cl.model_clip[idx] = c.cmModel.inlineModel(s);
          } catch (e) {
            Com_Error(c, ERR_DROP, '%s', e instanceof Error ? e.message : String(e));
          }
        }
      } else cl.model_clip[idx] = null;
    }
  } else if (i >= CS_SOUNDS && i < CS_SOUNDS + MAX_MODELS) {
    if (cl.refresh_prepped) cl.sound_precache[i - CS_SOUNDS] = c.sound.registerSound(s);
  } else if (i >= CS_IMAGES && i < CS_IMAGES + MAX_MODELS) {
    if (cl.refresh_prepped) {
      const gen = c.clearGeneration;
      const idx = i - CS_IMAGES;
      void RegisterPic(c, s).then((h) => {
        if (c.clearGeneration === gen && cl.configstrings[i] === s) cl.image_precache[idx] = h;
      });
    }
  } else if (i >= CS_PLAYERSKINS && i < CS_PLAYERSKINS + MAX_CLIENTS) {
    if (cl.refresh_prepped) void CL_ParseClientinfo(c, i - CS_PLAYERSKINS);
  }
}

/*
=====================================================================

ACTION MESSAGES

=====================================================================
*/

// C: cl_parse.c:581 CL_ParseStartSoundPacket
export function CL_ParseStartSoundPacket(c: ClientContext): void {
  const msg = c.net_message;
  const flags = MSG_ReadByte(msg);
  const sound_num = MSG_ReadByte(msg);

  let volume: number;
  if (flags & SND_VOLUME) volume = fr(MSG_ReadByte(msg) / 255.0);
  else volume = DEFAULT_SOUND_PACKET_VOLUME;

  let attenuation: number;
  if (flags & SND_ATTENUATION) attenuation = fr(MSG_ReadByte(msg) / 64.0);
  else attenuation = DEFAULT_SOUND_PACKET_ATTENUATION;

  let ofs: number;
  if (flags & SND_OFFSET) ofs = fr(MSG_ReadByte(msg) / 1000.0);
  else ofs = 0;

  let channel: number;
  let ent: number;
  if (flags & SND_ENT) {
    // entity reletive
    channel = MSG_ReadShort(msg);
    ent = channel >> 3;
    if (ent > MAX_EDICTS) Com_Error(c, ERR_DROP, 'CL_ParseStartSoundPacket: ent = %i', ent);
    channel &= 7;
  } else {
    ent = 0;
    channel = 0;
  }

  let pos: Float32Array | null;
  if (flags & SND_POS) {
    // positioned in space
    pos = c.ents.sound_pos;
    MSG_ReadPos(msg, pos);
  } // use entity number
  else pos = null;

  // sound_num == -1 (read past the end) indexes before the array in C; treated as "no sound"
  const sfx = c.cl.sound_precache[sound_num];
  if (!sfx) return;

  c.sound.startSound(pos, ent, channel, sfx, volume, attenuation, ofs);
}

// C: cl_parse.c:641 SHOWNET
export function SHOWNET(c: ClientContext, s: string | undefined): void {
  if (c.cv.cl_shownet.value >= 2) Com_Printf(c, '%3i:%s\n', c.net_message.readcount - 1, s ?? '(null)');
}

// C: cl_parse.c:652 CL_ParseServerMessage
export function CL_ParseServerMessage(c: ClientContext): void {
  const msg = c.net_message;
  const cls = c.cls;
  const shownet = c.cv.cl_shownet;

  //
  // if recording demos, copy the message out
  //
  if (shownet.value === 1) Com_Printf(c, '%i ', msg.cursize);
  else if (shownet.value >= 2) Com_Printf(c, '------------------\n');

  //
  // parse the message
  //
  while (1) {
    if (msg.readcount > msg.cursize) {
      Com_Error(c, ERR_DROP, 'CL_ParseServerMessage: Bad server message');
    }

    const cmd = MSG_ReadByte(msg);

    if (cmd === -1) {
      SHOWNET(c, 'END OF MESSAGE');
      break;
    }

    if (shownet.value >= 2) {
      if (!svc_strings[cmd]) Com_Printf(c, '%3i:BAD CMD %i\n', msg.readcount - 1, cmd);
      else SHOWNET(c, svc_strings[cmd]);
    }

    // other commands
    switch (cmd) {
      default:
        Com_Error(c, ERR_DROP, 'CL_ParseServerMessage: Illegible server message\n');
        break;

      case svc_nop:
        break;

      case svc_disconnect:
        Com_Error(c, ERR_DISCONNECT, 'Server disconnected\n');
        break;

      case svc_reconnect:
        Com_Printf(c, 'Server disconnected, reconnecting\n');
        // (no download to close)
        cls.state = ca_connecting;
        cls.connect_time = -99999; // CL_CheckForResend() will fire immediately
        break;

      case svc_print: {
        const i = MSG_ReadByte(msg);
        if (i === PRINT_CHAT) {
          c.sound.startLocalSound('misc/talk.wav');
          c.con.ormask = 128;
        }
        Com_Printf(c, '%s', MSG_ReadString(msg));
        c.con.ormask = 0;
        break;
      }

      case svc_centerprint:
        SCR_CenterPrint(c, MSG_ReadString(msg));
        break;

      case svc_stufftext: {
        const s = MSG_ReadString(msg);
        Com_DPrintf(c, 'stufftext: %s\n', s);
        c.cmd.cbufAddText(s);
        break;
      }

      case svc_serverdata:
        c.cmd.cbufExecute(); // make sure any stuffed commands are done
        CL_ParseServerData(c);
        break;

      case svc_configstring:
        CL_ParseConfigString(c);
        break;

      case svc_sound:
        CL_ParseStartSoundPacket(c);
        break;

      case svc_spawnbaseline:
        CL_ParseBaseline(c);
        break;

      case svc_temp_entity:
        c.fx.parseTEnt(msg);
        break;

      case svc_muzzleflash:
        c.fx.parseMuzzleFlash(msg);
        break;

      case svc_muzzleflash2:
        c.fx.parseMuzzleFlash2(msg);
        break;

      case svc_download:
        CL_ParseDownload(c);
        break;

      case svc_frame:
        CL_ParseFrame(c);
        break;

      case svc_inventory:
        CL_ParseInventory(c);
        break;

      case svc_layout: {
        const s = MSG_ReadString(msg);
        c.cl.layout = s.slice(0, 1023);
        break;
      }

      case svc_playerinfo:
      case svc_packetentities:
      case svc_deltapacketentities:
        Com_Error(c, ERR_DROP, 'Out of place frame data');
        break;
    }
  }

  CL_AddNetgraph(c);

  //
  // we don't know if it is ok to save a demo message until
  // after we have parsed the frame
  //
  if (cls.demorecording && !cls.demowaiting) CL_WriteDemoMessage(c);
}
