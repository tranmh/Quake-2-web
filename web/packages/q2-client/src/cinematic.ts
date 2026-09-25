// The client's view of client/cl_cin.c (cinematics). Implemented by the cinematics module; the SCR_*
// wrappers below are called at exactly the points the original calls them. NullCinematics behaves like
// the original when the cinematic file cannot be opened.
import { MSG_WriteByte, SZ_Print } from 'q2-protocol';
import { clc_stringcmd, sprintf } from 'q2-shared';
import { ca_active, Com_Printf, type ClientContext } from './client';
import { SCR_EndLoadingPlaque } from './cl_scrn';

export interface Cinematics {
  attach(c: ClientContext): void;
  /** SCR_PlayCinematic(name): name from the serverdata levelname (e.g. "idlog.cin" or "end.pcx"). */
  play(name: string): void;
  /** SCR_RunCinematic: once per CL_Frame */
  run(): void;
  /** SCR_StopCinematic */
  stop(): void;
  /** SCR_DrawCinematic: true if a cinematic frame was drawn (the rest of the screen is skipped) */
  draw(): boolean;
}

/** No cinematic support: every file is "not found". */
export class NullCinematics implements Cinematics {
  private c: ClientContext | null = null;
  attach(c: ClientContext): void {
    this.c = c;
  }
  play(arg: string): void {
    const c = this.c!;
    c.cl.cinematicframe = 0;
    const dot = arg.indexOf('.');
    if (dot >= 0 && arg.slice(dot) === '.pcx') {
      // static pcx image
      c.cl.cinematicframe = -1;
      c.cl.cinematictime = 1;
      SCR_EndLoadingPlaque(c);
      c.cls.state = ca_active;
      Com_Printf(c, '%s not found.\n', 'pics/' + arg);
      c.cl.cinematictime = 0;
      return;
    }
    SCR_FinishCinematic(c);
    c.cl.cinematictime = 0; // done
  }
  run(): void {}
  stop(): void {
    const c = this.c!;
    c.cl.cinematictime = 0; // done
    if (c.cl.cinematicpalette_active) {
      c.re.cinematicSetPalette(null);
      c.cl.cinematicpalette_active = false;
    }
  }
  draw(): boolean {
    return false;
  }
}

// C: cl_cin.c:576 SCR_PlayCinematic
export function SCR_PlayCinematic(c: ClientContext, name: string): void {
  c.cin.play(name);
}
// C: cl_cin.c SCR_RunCinematic
export function SCR_RunCinematic(c: ClientContext): void {
  c.cin.run();
}
// C: cl_cin.c:153 SCR_StopCinematic
export function SCR_StopCinematic(c: ClientContext): void {
  c.cin.stop();
}
// C: cl_cin.c:198 SCR_FinishCinematic -- called when either the cinematic completes, or it is aborted
export function SCR_FinishCinematic(c: ClientContext): void {
  // tell the server to advance to the next map / cinematic
  MSG_WriteByte(c.cls.netchan.message, clc_stringcmd);
  SZ_Print(c.cls.netchan.message, sprintf('nextserver %i\n', c.cl.servercount));
}
// C: cl_cin.c SCR_DrawCinematic
export function SCR_DrawCinematic(c: ClientContext): boolean {
  return c.cin.draw();
}
