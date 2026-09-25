// Port of client/cl_main.c -- client main loop.
// Browser deviations:
//  * NET_* goes through a DatagramTransport created for `cls.servername`; there is one remote address.
//  * CL_RequestNextDownload's download phase is replaced by an asynchronous asset step: the map is
//    loaded through the file loader, its Com_BlockChecksum is verified against CS_MAPCHECKSUM, sounds are
//    registered and CL_PrepRefresh is awaited before "begin" is sent.
//  * Demo recording keeps the .dm2 bytes in memory and hands them to the host on `stop`; demo playback
//    (CL_PlayDemo) feeds one recorded block per 100 ms, like the original server-side ss_demo.
//  * pingservers (LAN broadcast) is unavailable; rcon only works over the current connection; setenv uses a
//    private key/value store; `cmd` OOB packets are always treated as remote.
import {
  CS_MAPCHECKSUM,
  CS_MAXCLIENTS,
  CS_MODELS,
  CS_NAME,
  CS_PLAYERSKINS,
  CVAR_ARCHIVE,
  CVAR_NOSET,
  CVAR_SERVERINFO,
  CVAR_USERINFO,
  ERR_DISCONNECT,
  MAX_CLIENTS,
  MAX_CONFIGSTRINGS,
  MAX_EDICTS,
  MAX_MSGLEN,
  NS_CLIENT,
  PROTOCOL_VERSION,
  EntityState,
  Q_stricmp,
  atoi,
  clc_stringcmd,
  fr,
  latin1ToBytes,
  svc_configstring,
  svc_serverdata,
  svc_spawnbaseline,
  svc_stufftext,
  sprintf,
} from 'q2-shared';
import {
  MSG_BeginReading,
  MSG_ReadLong,
  MSG_ReadString,
  MSG_ReadStringLine,
  MSG_WriteByte,
  MSG_WriteChar,
  MSG_WriteDeltaEntity,
  MSG_WriteLong,
  MSG_WriteShort,
  MSG_WriteString,
  Netchan_OutOfBandPrint,
  ProtocolError,
  SZ_Clear,
  SZ_Print,
  SizeBuf,
} from 'q2-protocol';
import { CMError, CollisionModel, CollisionWorld } from 'q2-pmove';
import {
  ca_active,
  ca_connected,
  ca_connecting,
  ca_disconnected,
  ca_uninitialized,
  Com_DPrintf,
  Com_Error,
  Com_Printf,
  DropError,
  Sys_Milliseconds,
  type ClientContext,
} from './client';
import { CL_InitInput, CL_SendCmd, IN_Init } from './cl_input';
import { CL_ParseClientinfo, CL_ParseServerMessage, CL_RegisterSounds } from './cl_parse';
import { CL_PredictMovement } from './cl_pred';
import { CL_PrepRefresh, V_Init } from './cl_view';
import {
  SCR_BeginLoadingPlaque,
  SCR_EndLoadingPlaque,
  SCR_Init,
  SCR_RunConsole,
  SCR_UpdateScreen,
} from './cl_scrn';
import { SCR_RunCinematic, SCR_StopCinematic } from './cinematic';
import { Con_Init } from './console';
import { Key_WriteBindings } from './keys';
import { M_AddToServerList, M_ForceMenuOff } from './menu';

interface CheatVar {
  name: string;
  value: string;
}

// C: cl_main.c:1586 cheatvars
const CHEATVARS: readonly CheatVar[] = [
  { name: 'timescale', value: '1' },
  { name: 'timedemo', value: '0' },
  { name: 'r_drawworld', value: '1' },
  { name: 'cl_testlights', value: '0' },
  { name: 'r_fullbright', value: '0' },
  { name: 'r_drawflat', value: '0' },
  { name: 'paused', value: '0' },
  { name: 'fixedtime', value: '0' },
  { name: 'sw_draworder', value: '0' },
  { name: 'gl_lightmap', value: '0' },
  { name: 'gl_saturatelighting', value: '0' },
];

/** cl_main.c file-level globals. */
export class MainState {
  /** test hook: receives every Com_Printf text */
  printHook: ((msg: string) => void) | null = null;

  // CL_Frame statics
  extratime = 0;

  // CL_FixCvarCheats
  numcheatvars = 0;

  // precache (CL_Precache_f / CL_RequestNextDownload replacement)
  precache_spawncount = 0;
  /** an asynchronous precache step is running */
  precaching = false;

  // NET_GetPacket queue (datagrams received from the transport since the last CL_ReadPackets)
  readonly packets: Uint8Array[] = [];
  /** servername the current transport was opened for */
  transportAddress = '';

  // demo playback
  demoplaying = false;
  demoname = '';
  demodata: Uint8Array | null = null;
  demopos = 0;
  demonexttime = 0;

  /** CL_Setenv_f storage (no process environment in a browser) */
  readonly env = new Map<string, string>();

  /** last reported values (engine event polling) */
  reportedState = -1;
  reportedKeyDest = -1;
  reportedIntermission = false;
}

// ======================================================================

// C: cl_main.c:109 CL_WriteDemoMessage -- dumps the current net message, prefixed by the length
export function CL_WriteDemoMessage(c: ClientContext): void {
  const cls = c.cls;
  if (!cls.demofile) return;
  // the first eight bytes are just packet sequencing stuff
  const len = c.net_message.cursize - 8;
  const out = new Uint8Array(4 + len);
  new DataView(out.buffer).setInt32(0, len, true);
  out.set(c.net_message.data.subarray(8, 8 + len), 4);
  cls.demofile.push(out);
}

function demoWriteBlock(c: ClientContext, buf: SizeBuf): void {
  const out = new Uint8Array(4 + buf.cursize);
  new DataView(out.buffer).setInt32(0, buf.cursize, true);
  out.set(buf.data.subarray(0, buf.cursize), 4);
  c.cls.demofile!.push(out);
}

// C: cl_main.c:129 CL_Stop_f -- stop recording a demo
export function CL_Stop_f(c: ClientContext): void {
  const cls = c.cls;
  if (!cls.demorecording) {
    Com_Printf(c, 'Not recording a demo.\n');
    return;
  }

  // finish up
  const end = new Uint8Array(4);
  new DataView(end.buffer).setInt32(0, -1, true);
  cls.demofile!.push(end);
  let total = 0;
  for (const b of cls.demofile!) total += b.length;
  const data = new Uint8Array(total);
  let o = 0;
  for (const b of cls.demofile!) {
    data.set(b, o);
    o += b.length;
  }
  cls.demofile = null;
  cls.demorecording = false;
  Com_Printf(c, 'Stopped demo.\n');
  c.host.onDemoRecorded?.(cls.demoname, data);
}

// C: cl_main.c:155 CL_Record_f -- record <demoname>: begins recording a demo from the current position
export function CL_Record_f(c: ClientContext): void {
  const cls = c.cls;
  const cl = c.cl;
  if (c.cmd.argc() !== 2) {
    Com_Printf(c, 'record <demoname>\n');
    return;
  }

  if (cls.demorecording) {
    Com_Printf(c, 'Already recording.\n');
    return;
  }

  if (cls.state !== ca_active) {
    Com_Printf(c, 'You must be in a level to record.\n');
    return;
  }

  //
  // open the demo file
  //
  const name = sprintf('%s/demos/%s.dm2', FS_Gamedir(c), c.cmd.argv(1));

  Com_Printf(c, 'recording to %s.\n', name);
  cls.demofile = [];
  cls.demoname = name;
  cls.demorecording = true;

  // don't start saving messages until a non-delta compressed message is received
  cls.demowaiting = true;

  //
  // write out messages to hold the startup information
  //
  const buf = new SizeBuf(MAX_MSGLEN);

  // send the serverdata
  MSG_WriteByte(buf, svc_serverdata);
  MSG_WriteLong(buf, PROTOCOL_VERSION);
  MSG_WriteLong(buf, 0x10000 + cl.servercount);
  MSG_WriteByte(buf, 1); // demos are always attract loops
  MSG_WriteString(buf, cl.gamedir);
  MSG_WriteShort(buf, cl.playernum);

  MSG_WriteString(buf, cl.configstrings[CS_NAME]!);

  // configstrings
  for (let i = 0; i < MAX_CONFIGSTRINGS; i++) {
    const cs = cl.configstrings[i]!;
    if (cs.length) {
      if (buf.cursize + cs.length + 32 > buf.maxsize) {
        // write it out
        demoWriteBlock(c, buf);
        buf.cursize = 0;
      }

      MSG_WriteByte(buf, svc_configstring);
      MSG_WriteShort(buf, i);
      MSG_WriteString(buf, cs);
    }
  }

  // baselines
  const nullstate = new EntityState();
  for (let i = 0; i < MAX_EDICTS; i++) {
    const ent = c.cl_entities[i]!.baseline;
    if (!ent.modelindex) continue;

    if (buf.cursize + 64 > buf.maxsize) {
      // write it out
      demoWriteBlock(c, buf);
      buf.cursize = 0;
    }

    MSG_WriteByte(buf, svc_spawnbaseline);
    MSG_WriteDeltaEntity(nullstate, ent, buf, true, true);
  }

  MSG_WriteByte(buf, svc_stufftext);
  MSG_WriteString(buf, 'precache\n');

  // write it to the demo file
  demoWriteBlock(c, buf);

  // the rest of the demo file will be individual frames
}

/** C: files.c FS_Gamedir */
function FS_Gamedir(c: ClientContext): string {
  const g = c.cvars.variableString('game');
  return g ? g : 'baseq2';
}

// ======================================================================

// C: cl_main.c:280 Cmd_ForwardToServer
// adds the current command line as a clc_stringcmd to the client message.
// things like godmode, noclip, etc, are commands directed to the server,
// so when they are typed in at the console, they will need to be forwarded.
export function Cmd_ForwardToServer(c: ClientContext): void {
  const cls = c.cls;
  const cmd = c.cmd.argv(0);
  if (cls.state <= ca_connected || cmd[0] === '-' || cmd[0] === '+') {
    Com_Printf(c, 'Unknown command "%s"\n', cmd);
    return;
  }

  MSG_WriteByte(cls.netchan.message, clc_stringcmd);
  SZ_Print(cls.netchan.message, cmd);
  if (c.cmd.argc() > 1) {
    SZ_Print(cls.netchan.message, ' ');
    SZ_Print(cls.netchan.message, c.cmd.args());
  }
}

// C: cl_main.c:302 CL_Setenv_f (browser: a private key/value store instead of the process environment)
export function CL_Setenv_f(c: ClientContext): void {
  const argc = c.cmd.argc();
  if (argc > 2) {
    let buffer = '';
    for (let i = 2; i < argc; i++) buffer += c.cmd.argv(i) + ' ';
    c.main.env.set(c.cmd.argv(1), buffer);
  } else if (argc === 2) {
    const env = c.main.env.get(c.cmd.argv(1));
    if (env !== undefined) Com_Printf(c, '%s=%s\n', c.cmd.argv(1), env);
    else Com_Printf(c, '%s undefined\n', c.cmd.argv(1));
  }
}

// C: cl_main.c:340 CL_ForwardToServer_f
export function CL_ForwardToServer_f(c: ClientContext): void {
  const cls = c.cls;
  if (cls.state !== ca_connected && cls.state !== ca_active) {
    Com_Printf(c, 'Can\'t "%s", not connected\n', c.cmd.argv(0));
    return;
  }

  // don't forward the first argument
  if (c.cmd.argc() > 1) {
    MSG_WriteByte(cls.netchan.message, clc_stringcmd);
    SZ_Print(cls.netchan.message, c.cmd.args());
  }
}

// C: cl_main.c:361 CL_Pause_f
export function CL_Pause_f(c: ClientContext): void {
  // never pause in multiplayer
  if (c.cvars.variableValue('maxclients') > 1 || !c.cvars.serverState()) {
    c.cvars.setValue('paused', 0);
    return;
  }

  c.cvars.setValue('paused', c.cv.cl_paused.value ? 0 : 1);
}

// C: cl_main.c:378 CL_Quit_f
export function CL_Quit_f(c: ClientContext): void {
  CL_Disconnect(c);
  c.host.onQuit?.();
}

// C: cl_main.c:391 CL_Drop -- called after an ERR_DROP was thrown
export function CL_Drop(c: ClientContext): void {
  const cls = c.cls;
  if (cls.state === ca_uninitialized) return;
  if (cls.state === ca_disconnected) return;

  CL_Disconnect(c);

  // drop loading plaque unless this is the initial game start
  if (cls.disable_servercount !== -1) SCR_EndLoadingPlaque(c); // get rid of loading plaque
}

/** NET_StringToAdr + socket: make sure a transport to `address` exists. Returns false on a bad address. */
function NET_OpenTransport(c: ClientContext, address: string): boolean {
  if (!address) return false;
  if (c.transport && c.main.transportAddress === address) return true;
  NET_CloseTransport(c);
  let t;
  try {
    t = c.transportFactory(address);
  } catch {
    return false;
  }
  c.transport = t;
  c.main.transportAddress = address;
  t.onMessage = (data) => {
    if (c.transport === t) c.main.packets.push(data);
  };
  t.onClose = (reason) => {
    if (c.transport !== t) return;
    c.transport = null;
    c.main.transportAddress = '';
    Com_DPrintf(c, 'transport closed: %s\n', reason);
  };
  return true;
}

function NET_CloseTransport(c: ClientContext): void {
  const t = c.transport;
  c.transport = null;
  c.main.transportAddress = '';
  c.main.packets.length = 0;
  if (t) {
    t.onMessage = null;
    t.onClose = null;
    t.close();
  }
}

// C: cl_main.c:414 CL_SendConnectPacket -- we have gotten a challenge from the server, so try and connect.
export function CL_SendConnectPacket(c: ClientContext): void {
  const cls = c.cls;
  if (!NET_OpenTransport(c, cls.servername)) {
    Com_Printf(c, 'Bad server address\n');
    cls.connect_time = 0;
    return;
  }

  const port = c.cvars.variableValue('qport');
  c.cvars.userinfoModified = false;

  Netchan_OutOfBandPrint(
    netctx(c),
    NS_CLIENT,
    cls.servername,
    'connect %i %i %i "%s"\n',
    PROTOCOL_VERSION,
    Math.trunc(port),
    cls.challenge,
    c.cvars.userinfo(),
  );
}

function netctx(c: ClientContext) {
  return c.cls.netchan.ctx;
}

// C: cl_main.c:443 CL_CheckForResend -- resend a connect message if the last one has timed out
export function CL_CheckForResend(c: ClientContext): void {
  const cls = c.cls;
  // if the local server is running and we aren't then connect (no local server in a browser)

  // resend if we haven't gotten a reply yet
  if (cls.state !== ca_connecting) return;

  if (cls.realtime - cls.connect_time < 3000) return;

  if (!NET_OpenTransport(c, cls.servername)) {
    Com_Printf(c, 'Bad server address\n');
    cls.state = ca_disconnected;
    return;
  }

  cls.connect_time = cls.realtime; // for retransmit requests

  Com_Printf(c, 'Connecting to %s...\n', cls.servername);

  Netchan_OutOfBandPrint(netctx(c), NS_CLIENT, cls.servername, 'getchallenge\n');
}

// C: cl_main.c:486 CL_Connect_f
export function CL_Connect_f(c: ClientContext): void {
  const cls = c.cls;
  if (c.cmd.argc() !== 2) {
    Com_Printf(c, 'usage: connect <server>\n');
    return;
  }

  CL_Disconnect(c);

  const server = c.cmd.argv(1);

  CL_Disconnect(c);

  cls.state = ca_connecting;
  cls.servername = server.slice(0, 127);
  cls.connect_time = -99999; // CL_CheckForResend() will fire immediately
}

// C: cl_main.c:522 CL_Rcon_f (rcon is replaced by the admin API)
export function CL_Rcon_f(c: ClientContext): void {
  if (!c.cv.rcon_client_password.string) {
    Com_Printf(c, "You must set 'rcon_password' before\nissuing an rcon command.\n");
    return;
  }
  const cls = c.cls;
  let message = '\xff\xff\xff\xffrcon ' + c.cv.rcon_client_password.string + ' ';
  for (let i = 1; i < c.cmd.argc(); i++) message += c.cmd.argv(i) + ' ';
  if (cls.state >= ca_connected && c.transport) {
    const bytes = latin1ToBytes(message + '\0');
    c.transport.send(bytes);
    return;
  }
  Com_Printf(c, "You must either be connected,\nor set the 'rcon_address' cvar\nto issue rcon commands\n");
}

// C: cl_main.c:582 CL_ClearState
export function CL_ClearState(c: ClientContext): void {
  c.sound.stopAllSounds();
  c.fx.clearEffects();
  c.fx.clearTEnts();

  // wipe the entire cl structure
  c.cl.reset();
  for (const e of c.cl_entities) e.clear();
  c.clearGeneration++; // abandons any asynchronous precache / registration of the previous level
  c.main.precaching = false;

  SZ_Clear(c.cls.netchan.message);
}

// C: cl_main.c:605 CL_Disconnect
// Goes from a connected state to full screen console state. Sends a disconnect message to the server.
// This is also called on Com_Error, so it shouldn't cause any errors.
export function CL_Disconnect(c: ClientContext): void {
  const cls = c.cls;
  const cl = c.cl;
  if (cls.state === ca_disconnected) return;

  if (c.cv.cl_timedemo && c.cv.cl_timedemo.value) {
    const time = Sys_Milliseconds(c) - cl.timedemo_start;
    if (time > 0)
      Com_Printf(
        c,
        '%i frames, %3.1f seconds: %3.1f fps\n',
        cl.timedemo_frames,
        time / 1000.0,
        (cl.timedemo_frames * 1000.0) / time,
      );
  }

  cl.refdef.blend[0] = cl.refdef.blend[1] = cl.refdef.blend[2] = 0;
  c.re.cinematicSetPalette(null);

  M_ForceMenuOff(c);

  cls.connect_time = 0;

  SCR_StopCinematic(c);

  if (cls.demorecording) CL_Stop_f(c);

  // send a disconnect message to the server
  if (!c.main.demoplaying && c.transport && cls.state >= ca_connected) {
    const final = latin1ToBytes(String.fromCharCode(clc_stringcmd) + 'disconnect');
    cls.netchan.transmit(final.length, final);
    cls.netchan.transmit(final.length, final);
    cls.netchan.transmit(final.length, final);
  }

  CL_ClearState(c);

  c.main.demoplaying = false;
  c.main.demodata = null;
  c.main.precaching = false;
  NET_CloseTransport(c);

  cls.state = ca_disconnected;
}

// C: cl_main.c:653 CL_Disconnect_f
export function CL_Disconnect_f(c: ClientContext): void {
  Com_Error(c, 1 /* ERR_DROP */, 'Disconnected from server');
}

// C: cl_main.c:720 CL_Changing_f -- just sent as a hint to the client that they should drop to full console
export function CL_Changing_f(c: ClientContext): void {
  //ZOID
  //if we are downloading, we don't change!  This so we don't suddenly stop downloading a map
  if (c.cls.download) return;

  SCR_BeginLoadingPlaque(c);
  c.cls.state = ca_connected; // not active anymore, but not disconnected
  Com_Printf(c, '\nChanging map...\n');
}

// C: cl_main.c:739 CL_Reconnect_f -- the server is changing levels
export function CL_Reconnect_f(c: ClientContext): void {
  const cls = c.cls;
  //ZOID
  //if we are downloading, we don't change!  This so we don't suddenly stop downloading a map
  if (cls.download) return;

  c.sound.stopAllSounds();
  if (cls.state === ca_connected) {
    Com_Printf(c, 'reconnecting...\n');
    cls.state = ca_connected;
    MSG_WriteChar(cls.netchan.message, clc_stringcmd);
    MSG_WriteString(cls.netchan.message, 'new');
    return;
  }

  if (cls.servername) {
    if (cls.state >= ca_connected) {
      const name = cls.servername;
      CL_Disconnect(c);
      cls.servername = name;
      cls.connect_time = cls.realtime - 1500;
    } else cls.connect_time = -99999; // fire immediately

    cls.state = ca_connecting;
    Com_Printf(c, 'reconnecting...\n');
  }
}

// C: cl_main.c:773 CL_ParseStatusMessage -- handle a reply from a ping
export function CL_ParseStatusMessage(c: ClientContext): void {
  const s = MSG_ReadString(c.net_message);
  Com_Printf(c, '%s\n', s);
  M_AddToServerList(c, c.net_from, s);
}

// C: cl_main.c:789 CL_PingServers_f (LAN broadcast is impossible from a browser)
export function CL_PingServers_f(c: ClientContext): void {
  Com_Printf(c, 'pingservers is not available in the browser client; use the server browser.\n');
}

// C: cl_main.c:855 CL_Skins_f -- load or download any custom player skins and models
export function CL_Skins_f(c: ClientContext): void {
  for (let i = 0; i < MAX_CLIENTS; i++) {
    if (!c.cl.configstrings[CS_PLAYERSKINS + i]) continue;
    Com_Printf(c, 'client %i: %s\n', i, c.cl.configstrings[CS_PLAYERSKINS + i]!);
    SCR_UpdateScreen(c);
    CL_ParseClientinfo(c, i);
  }
}

// C: cl_main.c:876 CL_ConnectionlessPacket -- responses to broadcasts, etc
export function CL_ConnectionlessPacket(c: ClientContext): void {
  const cls = c.cls;
  const msg = c.net_message;
  MSG_BeginReading(msg);
  MSG_ReadLong(msg); // skip the -1

  const s = MSG_ReadStringLine(msg);

  c.cmd.tokenizeString(s, false);

  const cmd = c.cmd.argv(0);

  Com_Printf(c, '%s: %s\n', c.net_from, cmd);

  // server connection
  if (cmd === 'client_connect') {
    if (cls.state === ca_connected) {
      Com_Printf(c, 'Dup connect received.  Ignored.\n');
      return;
    }
    cls.netchan.setup(NS_CLIENT, c.net_from, cls.quakePort);
    MSG_WriteChar(cls.netchan.message, clc_stringcmd);
    MSG_WriteString(cls.netchan.message, 'new');
    cls.state = ca_connected;
    return;
  }

  // server responding to a status broadcast
  if (cmd === 'info') {
    CL_ParseStatusMessage(c);
    return;
  }

  // remote command from gui front end
  if (cmd === 'cmd') {
    // C: only accepted from a local address; a remote server can never be local here
    Com_Printf(c, 'Command packet from remote host.  Ignored.\n');
    return;
  }
  // print command from somewhere
  if (cmd === 'print') {
    const p = MSG_ReadString(msg);
    Com_Printf(c, '%s', p);
    return;
  }

  // ping from somewhere
  if (cmd === 'ping') {
    Netchan_OutOfBandPrint(netctx(c), NS_CLIENT, c.net_from, 'ack');
    return;
  }

  // challenge from the server we are connecting to
  if (cmd === 'challenge') {
    cls.challenge = atoi(c.cmd.argv(1));
    CL_SendConnectPacket(c);
    return;
  }

  // echo request from server
  if (cmd === 'echo') {
    Netchan_OutOfBandPrint(netctx(c), NS_CLIENT, c.net_from, '%s', c.cmd.argv(1));
    return;
  }

  Com_Printf(c, 'Unknown command.\n');
}

/** C: net_udp.c NET_GetPacket -- next queued datagram into net_message */
function NET_GetPacket(c: ClientContext): boolean {
  const msg = c.net_message;
  for (;;) {
    const d = c.main.packets.shift();
    if (!d) return false;
    if (d.length > msg.maxsize) {
      Com_Printf(c, 'Oversize packet from %s\n', c.main.transportAddress);
      continue;
    }
    msg.data.set(d);
    msg.cursize = d.length;
    msg.readcount = 0;
    c.net_from = c.main.transportAddress;
    return true;
  }
}

// C: cl_main.c:976 CL_DumpPackets
export function CL_DumpPackets(c: ClientContext): void {
  while (NET_GetPacket(c)) Com_Printf(c, 'dumnping a packet\n');
}

// C: cl_main.c:991 CL_ReadPackets
export function CL_ReadPackets(c: ClientContext): void {
  const cls = c.cls;
  if (c.main.demoplaying) {
    CL_ReadDemoPackets(c);
  } else {
    while (NET_GetPacket(c)) {
      const msg = c.net_message;
      //
      // remote command packet
      //
      if (
        msg.cursize >= 4 &&
        msg.data[0] === 0xff &&
        msg.data[1] === 0xff &&
        msg.data[2] === 0xff &&
        msg.data[3] === 0xff
      ) {
        CL_ConnectionlessPacket(c);
        continue;
      }

      if (cls.state === ca_disconnected || cls.state === ca_connecting) continue; // dump it if not connected

      if (msg.cursize < 8) {
        Com_Printf(c, '%s: Runt packet\n', c.net_from);
        continue;
      }

      //
      // packet from server
      //
      if (c.net_from !== cls.netchan.remote_address) {
        Com_DPrintf(c, '%s:sequenced packet without connection\n', c.net_from);
        continue;
      }
      if (!cls.netchan.process(msg)) continue; // wasn't accepted for some reason
      CL_ParseServerMessage(c);
    }
  }

  //
  // check timeout
  //
  if (cls.state >= ca_connected && cls.realtime - cls.netchan.last_received > c.cv.cl_timeout.value * 1000) {
    if (++c.cl.timeoutcount > 5) {
      // timeoutcount saves debugger
      Com_Printf(c, '\nServer connection timed out.\n');
      CL_Disconnect(c);
      return;
    }
  } else c.cl.timeoutcount = 0;
}

/**
 * Demo playback (replaces the server's ss_demo state): starts playing recorded .dm2 bytes. Each block
 * is parsed as one server message; one block is delivered per 100 ms (one server frame).
 */
export function CL_PlayDemo(c: ClientContext, name: string, data: Uint8Array): void {
  CL_Disconnect(c);
  const m = c.main;
  m.demoplaying = true;
  m.demoname = name;
  m.demodata = data;
  m.demopos = 0;
  m.demonexttime = c.cls.realtime;
  c.cls.servername = 'demo:' + name;
  c.cls.netchan.last_received = c.curtime;
  SCR_BeginLoadingPlaque(c);
  c.cls.state = ca_connected;
}

function CL_ReadDemoPackets(c: ClientContext): void {
  const m = c.main;
  const cls = c.cls;
  // a synchronous load in the original: hold the stream while the asynchronous precache runs
  if (m.precaching) {
    cls.netchan.last_received = c.curtime;
    return;
  }
  while (m.demoplaying && m.demonexttime <= cls.realtime) {
    const d = m.demodata!;
    const msg = c.net_message;
    if (m.demopos + 4 > d.length) {
      demoFinished(c);
      return;
    }
    const len = new DataView(d.buffer, d.byteOffset + m.demopos, 4).getInt32(0, true);
    m.demopos += 4;
    if (len === -1) {
      demoFinished(c);
      return;
    }
    if (len < 0 || len > MAX_MSGLEN || m.demopos + len > d.length) {
      Com_Error(c, 1 /* ERR_DROP */, 'SV_SendClientMessages: msglen > MAX_MSGLEN');
    }
    msg.data.set(d.subarray(m.demopos, m.demopos + len));
    msg.cursize = len;
    msg.readcount = 0;
    m.demopos += len;
    m.demonexttime += 100;
    cls.netchan.last_received = c.curtime;
    CL_ParseServerMessage(c);
    if (m.precaching) return;
  }
}

function demoFinished(c: ClientContext): void {
  // C: SV_DemoCompleted -> killserver -> svc_disconnect: Com_Error (ERR_DISCONNECT, "Server disconnected\n")
  Com_Printf(c, 'Demo finished.\n');
  Com_Error(c, ERR_DISCONNECT, 'Server disconnected\n');
}

//=============================================================================

// C: cl_main.c:1058 CL_FixUpGender
export function CL_FixUpGender(c: ClientContext): void {
  const cv = c.cv;
  if (cv.gender_auto.value) {
    if (cv.gender.modified) {
      // was set directly, don't override the user
      cv.gender.modified = false;
      return;
    }

    let sk = cv.skin.string.slice(0, 79);
    const p = sk.indexOf('/');
    if (p >= 0) sk = sk.slice(0, p);
    if (Q_stricmp(sk, 'male') === 0 || Q_stricmp(sk, 'cyborg') === 0) c.cvars.set('gender', 'male');
    else if (Q_stricmp(sk, 'female') === 0 || Q_stricmp(sk, 'crackhor') === 0)
      c.cvars.set('gender', 'female');
    else c.cvars.set('gender', 'none');
    cv.gender.modified = false;
  }
}

// C: common.c Info_Print
export function Info_Print(c: ClientContext, s: string): void {
  let i = 0;
  if (s[i] === '\\') i++;
  while (i < s.length) {
    let key = '';
    while (i < s.length && s[i] !== '\\') key += s[i++];
    if (key.length < 20) key = key + ' '.repeat(20 - key.length);
    Com_Printf(c, '%s', key);

    if (i >= s.length) {
      Com_Printf(c, 'MISSING VALUE\n');
      return;
    }

    let value = '';
    i++;
    while (i < s.length && s[i] !== '\\') value += s[i++];
    if (i < s.length) i++;
    Com_Printf(c, '%s\n', value);
  }
}

// C: cl_main.c:1087 CL_Userinfo_f
export function CL_Userinfo_f(c: ClientContext): void {
  Com_Printf(c, 'User info settings:\n');
  Info_Print(c, c.cvars.userinfo());
}

// C: cl_main.c:1100 CL_Snd_Restart_f -- restart the sound subsystem so it can pick up new parameters
export function CL_Snd_Restart_f(c: ClientContext): void {
  c.sound.shutdown();
  c.sound.init();
  CL_RegisterSounds(c).catch((e: unknown) => Com_HandleError(c, e));
}

/**
 * C: cmodel.c:548 CM_LoadMap(name, clientload=true, &checksum) through the async loader. Reuses the
 * loaded map when the name is unchanged (the clientload shortcut).
 */
export async function CM_LoadMap(c: ClientContext, name: string): Promise<number> {
  if (c.cmModel && c.cmModel.name === name && name) {
    c.cm!.resetPortals();
    return c.cmModel.checksum >>> 0;
  }
  if (!name) {
    c.cmModel = CollisionModel.empty();
    c.cm = new CollisionWorld(c.cmModel);
    return 0;
  }
  const data = await c.loadFile(name);
  if (!data) Com_Error(c, 1 /* ERR_DROP */, "Couldn't load %s", name);
  const model = CollisionModel.load(name, data);
  c.cmModel = model;
  c.cm = new CollisionWorld(model);
  return model.checksum >>> 0;
}

// C: cl_main.c:1123 CL_RequestNextDownload (browser replacement)
// Downloads are replaced by HTTP assets: load and verify the map, register sounds, prepare the refresh
// (awaiting every registration) and send "begin".
export function CL_RequestNextDownload(c: ClientContext): void {
  if (c.cls.state !== ca_connected) return;
  void precache(c, true);
}

async function precache(c: ClientContext, fullSequence: boolean): Promise<void> {
  const cl = c.cl;
  const gen = c.clearGeneration;
  const mapname = cl.configstrings[CS_MODELS + 1]!;
  c.main.precaching = true;
  const stale = () => c.clearGeneration !== gen;
  try {
    c.host.onLoading?.({ active: true, mapname, stage: 'map', progress: 0 });
    const map_checksum = await CM_LoadMap(c, mapname);
    if (stale()) return;
    if (fullSequence && map_checksum !== atoi(cl.configstrings[CS_MAPCHECKSUM]!) >>> 0) {
      Com_Error(
        c,
        1 /* ERR_DROP */,
        "Local map version differs from server: %i != '%s'\n",
        map_checksum | 0,
        cl.configstrings[CS_MAPCHECKSUM]!,
      );
    }
    //ZOID
    c.host.onLoading?.({ active: true, mapname, stage: 'sounds', progress: 0.1 });
    await CL_RegisterSounds(c);
    if (stale()) return;
    await CL_PrepRefresh(c);
    if (stale()) return;

    if (fullSequence) {
      MSG_WriteByte(c.cls.netchan.message, clc_stringcmd);
      MSG_WriteString(c.cls.netchan.message, sprintf('begin %i\n', c.main.precache_spawncount));
    }
  } catch (e) {
    if (!stale()) Com_HandleError(c, e);
  } finally {
    if (!stale()) c.main.precaching = false;
  }
}

// C: cl_main.c:1426 CL_Precache_f -- the server will send this command right before allowing the client
// into the server
export function CL_Precache_f(c: ClientContext): void {
  //Yet another hack to let old demos work
  //the old precache sequence
  if (c.cmd.argc() < 2) {
    void precache(c, false);
    return;
  }

  c.main.precache_spawncount = atoi(c.cmd.argv(1));
  CL_RequestNextDownload(c);
}

// C: cl_main.c:1453 CL_InitLocal
export function CL_InitLocal(c: ClientContext): void {
  const cls = c.cls;
  const cv = c.cv;
  const get = (n: string, v: string, f: number) => c.cvars.get(n, v, f);
  cls.state = ca_disconnected;
  cls.realtime = Sys_Milliseconds(c);

  // cmd.c calls Cmd_ForwardToServer (cl_main.c) for unknown commands
  c.cmd.forwardToServer = () => Cmd_ForwardToServer(c);

  CL_InitInput(c);

  cv.adr = [];
  for (let i = 0; i <= 8; i++) cv.adr.push(get('adr' + i, '', CVAR_ARCHIVE));

  //
  // register our variables
  //
  cv.cl_stereo_separation = get('cl_stereo_separation', '0.4', CVAR_ARCHIVE);
  cv.cl_stereo = get('cl_stereo', '0', 0);

  cv.cl_add_blend = get('cl_blend', '1', 0);
  cv.cl_add_lights = get('cl_lights', '1', 0);
  cv.cl_add_particles = get('cl_particles', '1', 0);
  cv.cl_add_entities = get('cl_entities', '1', 0);
  cv.cl_gun = get('cl_gun', '1', 0);
  cv.cl_footsteps = get('cl_footsteps', '1', 0);
  cv.cl_noskins = get('cl_noskins', '0', 0);
  cv.cl_autoskins = get('cl_autoskins', '0', 0);
  cv.cl_predict = get('cl_predict', '1', 0);
  //	cl_minfps = Cvar_Get ("cl_minfps", "5", 0);
  cv.cl_maxfps = get('cl_maxfps', '90', 0);

  cv.cl_upspeed = get('cl_upspeed', '200', 0);
  cv.cl_forwardspeed = get('cl_forwardspeed', '200', 0);
  cv.cl_sidespeed = get('cl_sidespeed', '200', 0);
  cv.cl_yawspeed = get('cl_yawspeed', '140', 0);
  cv.cl_pitchspeed = get('cl_pitchspeed', '150', 0);
  cv.cl_anglespeedkey = get('cl_anglespeedkey', '1.5', 0);

  cv.cl_run = get('cl_run', '0', CVAR_ARCHIVE);
  cv.freelook = get('freelook', '0', CVAR_ARCHIVE);
  cv.lookspring = get('lookspring', '0', CVAR_ARCHIVE);
  cv.lookstrafe = get('lookstrafe', '0', CVAR_ARCHIVE);
  cv.sensitivity = get('sensitivity', '3', CVAR_ARCHIVE);

  cv.m_pitch = get('m_pitch', '0.022', CVAR_ARCHIVE);
  cv.m_yaw = get('m_yaw', '0.022', 0);
  cv.m_forward = get('m_forward', '1', 0);
  cv.m_side = get('m_side', '1', 0);

  cv.cl_shownet = get('cl_shownet', '0', 0);
  cv.cl_showmiss = get('cl_showmiss', '0', 0);
  cv.cl_showclamp = get('showclamp', '0', 0);
  cv.cl_timeout = get('cl_timeout', '120', 0);
  cv.cl_paused = get('paused', '0', 0);
  cv.cl_timedemo = get('timedemo', '0', 0);

  cv.rcon_client_password = get('rcon_password', '', 0);
  cv.rcon_address = get('rcon_address', '', 0);

  cv.cl_lightlevel = get('r_lightlevel', '0', 0);

  //
  // userinfo
  //
  cv.info_password = get('password', '', CVAR_USERINFO);
  cv.info_spectator = get('spectator', '0', CVAR_USERINFO);
  cv.name = get('name', 'unnamed', CVAR_USERINFO | CVAR_ARCHIVE);
  cv.skin = get('skin', 'male/grunt', CVAR_USERINFO | CVAR_ARCHIVE);
  cv.rate = get('rate', '25000', CVAR_USERINFO | CVAR_ARCHIVE); // FIXME
  cv.msg = get('msg', '1', CVAR_USERINFO | CVAR_ARCHIVE);
  cv.hand = get('hand', '0', CVAR_USERINFO | CVAR_ARCHIVE);
  cv.fov = get('fov', '90', CVAR_USERINFO | CVAR_ARCHIVE);
  cv.gender = get('gender', 'male', CVAR_USERINFO | CVAR_ARCHIVE);
  cv.gender_auto = get('gender_auto', '1', CVAR_ARCHIVE);
  cv.gender.modified = false; // clear this so we know when user sets it manually

  cv.cl_vwep = get('cl_vwep', '1', CVAR_ARCHIVE);

  //
  // register our commands
  //
  const add = (name: string, fn: (c: ClientContext) => void) => c.cmd.addCommand(name, () => fn(c));
  add('cmd', CL_ForwardToServer_f);
  add('pause', CL_Pause_f);
  add('pingservers', CL_PingServers_f);
  add('skins', CL_Skins_f);

  add('userinfo', CL_Userinfo_f);
  add('snd_restart', CL_Snd_Restart_f);

  add('changing', CL_Changing_f);
  add('disconnect', CL_Disconnect_f);
  add('record', CL_Record_f);
  add('stop', CL_Stop_f);

  add('quit', CL_Quit_f);

  add('connect', CL_Connect_f);
  add('reconnect', CL_Reconnect_f);

  add('rcon', CL_Rcon_f);

  // 	Cmd_AddCommand ("packet", CL_Packet_f); // this is dangerous to leave in

  add('setenv', CL_Setenv_f);

  add('precache', CL_Precache_f);

  add('download', CL_Download_f_);

  //
  // forward to server commands
  //
  // the only thing this does is allow command completion
  // to work -- all unknown commands are automatically
  // forwarded to the server
  for (const n of [
    'wave',
    'inven',
    'kill',
    'use',
    'drop',
    'say',
    'say_team',
    'info',
    'prog',
    'give',
    'god',
    'notarget',
    'noclip',
    'invuse',
    'invprev',
    'invnext',
    'invdrop',
    'weapnext',
    'weapprev',
  ])
    c.cmd.addCommand(n, null);
}

function CL_Download_f_(c: ClientContext): void {
  Com_Printf(c, 'Downloading is not supported; assets are served over HTTP.\n');
}

// C: cl_main.c:1567 CL_WriteConfiguration -- key bindings and archived cvars (config.cfg text)
export function CL_WriteConfiguration(c: ClientContext): string {
  if (c.cls.state === ca_uninitialized) return '';
  let f = '// generated by quake, do not modify\n';
  f += Key_WriteBindings(c);
  f += c.cvars.writeVariables();
  return f;
}

// C: cl_main.c:1613 CL_FixCvarCheats
export function CL_FixCvarCheats(c: ClientContext): void {
  const cl = c.cl;
  if (cl.configstrings[CS_MAXCLIENTS] === '1' || !cl.configstrings[CS_MAXCLIENTS]) return; // single player can cheat

  // find all the cvars if we haven't done it yet
  if (!c.main.numcheatvars) {
    for (const v of CHEATVARS) c.cvars.get(v.name, v.value, 0);
    c.main.numcheatvars = CHEATVARS.length;
  }

  // make sure they are all set to the proper values
  for (const v of CHEATVARS) {
    if (c.cvars.variableString(v.name) !== v.value) c.cvars.set(v.name, v.value);
  }
}

//============================================================================

// C: cl_main.c:1646 CL_SendCommand
export function CL_SendCommand(c: ClientContext): void {
  // get new key events (C: Sys_SendKeyEvents stamps sys_frame_time; browser events are delivered as
  // they happen through the engine)
  c.input.sys_frame_time = Sys_Milliseconds(c) >>> 0;

  // process console commands
  c.cmd.cbufExecute();

  // fix any cheating cvars
  CL_FixCvarCheats(c);

  // send intentions now
  CL_SendCmd(c);

  // resend a connection request if necessary
  CL_CheckForResend(c);
}

// C: cl_main.c:1674 CL_Frame
export function CL_Frame(c: ClientContext, msec: number): void {
  const cls = c.cls;
  const cl = c.cl;
  const m = c.main;

  m.extratime += msec;

  if (!c.cv.cl_timedemo.value) {
    if (cls.state === ca_connected && m.extratime < 100) return; // don't flood packets out while connecting
    if (m.extratime < 1000 / c.cv.cl_maxfps.value) return; // framerate is too high
  }

  // let the mouse activate or deactivate (IN_Frame: pointer lock is managed by the host)

  // decide the simulation time
  cls.frametime = fr(m.extratime / 1000.0);
  cl.time += m.extratime;
  cls.realtime = c.curtime;

  m.extratime = 0;
  if (cls.frametime > 1.0 / 5) cls.frametime = fr(1.0 / 5);

  // if in the debugger last frame, don't timeout
  if (msec > 5000) cls.netchan.last_received = Sys_Milliseconds(c);

  // keep the netchan cvars current (C reads them directly)
  const ctx = cls.netchan.ctx;
  ctx.qport = c.cv.qport.value;
  ctx.showpackets = c.cv.showpackets.value;
  ctx.showdrop = c.cv.showdrop.value;

  // fetch results from server
  CL_ReadPackets(c);

  // send a new command message to the server
  CL_SendCommand(c);

  // predict all unacknowledged movements
  CL_PredictMovement(c);

  // allow rendering DLL change (VID_CheckChanges: no renderer switching in the browser)
  if (!cl.refresh_prepped && cls.state === ca_active && !m.precaching) {
    CL_PrepRefresh(c).catch((e: unknown) => Com_HandleError(c, e));
  }

  // update the screen
  SCR_UpdateScreen(c);

  // update audio
  c.sound.update(cl.refdef.vieworg, cl.v_forward, cl.v_right, cl.v_up);

  // advance local effects for next frame
  c.fx.runDLights();
  c.fx.runLightStyles();
  SCR_RunCinematic(c);
  SCR_RunConsole(c);

  cls.framecount++;
}

// C: common.c:195 Com_Error, the part that runs after the longjmp back to the frame loop
export function Com_HandleError(c: ClientContext, e: unknown): void {
  if (e instanceof DropError) {
    if (e.code === ERR_DISCONNECT) {
      CL_Drop(c);
      return;
    }
    Com_Printf(c, '********************\nERROR: %s\n********************\n', e.message);
    c.host.onError?.(e.message);
    CL_Drop(c);
    return;
  }
  // an internal exception: report like ERR_DROP so the client stays usable
  const msg = e instanceof Error ? e.message : String(e);
  Com_Printf(c, '********************\nERROR: %s\n********************\n', msg);
  c.host.onError?.(msg);
  CL_Drop(c);
  // ProtocolError (SZ_/MSG_ Com_Error) and CMError (cmodel Com_Error) are ERR_DROPs of the original;
  // anything else is an engine bug worth a stack trace
  if (!(e instanceof ProtocolError) && !(e instanceof CMError)) console.error(e);
}

//============================================================================

// C: common.c:1420 Qcommon_Init (the parts a client needs) + net_chan.c Netchan_Init
export function Qcommon_InitCvars(c: ClientContext): void {
  const cv = c.cv;
  const get = (n: string, v: string, f: number) => c.cvars.get(n, v, f);
  cv.host_speeds = get('host_speeds', '0', 0);
  get('log_stats', '0', 0);
  cv.developer = get('developer', '0', 0);
  cv.timescale = get('timescale', '1', 0);
  cv.fixedtime = get('fixedtime', '0', 0);
  get('logfile', '0', 0);
  cv.showtrace = get('showtrace', '0', 0);
  get('dedicated', '0', CVAR_NOSET);
  get('version', '3.19 x86 Nov 30 1997 browser', CVAR_SERVERINFO | CVAR_NOSET);
  // Netchan_Init
  const port = Sys_Milliseconds(c) & 0xffff;
  cv.showpackets = get('showpackets', '0', 0);
  cv.showdrop = get('showdrop', '0', 0);
  cv.qport = get('qport', String(port), CVAR_NOSET);
  // vid cvars read by the client
  cv.vid_fullscreen = get('vid_fullscreen', '0', CVAR_ARCHIVE);
  cv.vid_gamma = get('vid_gamma', '1', CVAR_ARCHIVE);
}

// C: cl_main.c:1784 CL_Init
export function CL_Init(c: ClientContext): void {
  // all archived variables will now be loaded

  Con_Init(c);
  c.sound.init();
  V_Init(c);

  // net_message.data = net_message_buffer (preallocated in the context)

  SCR_Init(c);
  c.cls.disable_screen = 1; // don't draw yet

  CL_InitLocal(c);
  IN_Init(c);

  //	Cbuf_AddText ("exec autoexec.cfg\n");
  // FS_ExecAutoexec
  c.cmd.cbufAddText('exec autoexec.cfg\n');
  c.cmd.cbufExecute();
}

// C: cl_main.c:1826 CL_Shutdown
export function CL_Shutdown(c: ClientContext): string {
  const cfg = CL_WriteConfiguration(c);
  c.sound.shutdown();
  return cfg;
}
