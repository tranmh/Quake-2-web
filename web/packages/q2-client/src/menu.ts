// Replacement for client/menu.c: the menus are React components (q2-ui-kit). These functions keep the
// engine-side effects of the original entry points (key_dest / paused handling) and notify the host.
import { key_game, key_menu, type ClientContext } from './client';
import { Key_ClearStates } from './keys';

// C: menu.c M_ForceMenuOff
export function M_ForceMenuOff(c: ClientContext): void {
  c.cls.key_dest = key_game;
  Key_ClearStates(c);
  c.cvars.set('paused', '0');
  c.host.onMenu?.('off');
}

// C: menu.c M_Menu_Main_f (M_PushMenu)
export function M_Menu_Main_f(c: ClientContext): void {
  // if this menu is already present, M_PushMenu drops back to that level; the UI handles the stack
  if (c.cvars.variableValue('maxclients') === 1 && c.cvars.serverState()) c.cvars.set('paused', '1');
  c.cls.key_dest = key_menu;
  c.host.onMenu?.('main');
}

// C: menu.c M_Keydown -- keys go to the UI while key_dest == key_menu
export function M_Keydown(c: ClientContext, key: number): void {
  c.host.onMenu?.('key', key);
}

// C: menu.c M_Draw -- the React overlay draws the menu; nothing is drawn in the canvas
export function M_Draw(_c: ClientContext): void {}

// C: menu.c M_AddToServerList -- server browser is an HTTP API; status replies are only printed
export function M_AddToServerList(_c: ClientContext, _adr: string, _info: string): void {}
