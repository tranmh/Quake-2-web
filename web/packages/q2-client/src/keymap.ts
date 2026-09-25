// Browser input → Quake 2 key numbers (client/keys.h K_*). Replaces the platform key translation of
// in_*.c / vid_*.c (e.g. linux rw_x11.c XLateKey).
//
// Mapping is by KeyboardEvent.code (the physical key), not by the produced character: in the original
// the key number of a printable key IS its unshifted ASCII code (letters lowercase), and the shifted
// character used for console typing is derived inside Key_Event through the keyshift[] table while
// K_SHIFT is held. Mapping by physical key keeps bindings layout-independent exactly like the original
// scancode tables (US layout positions).
import {
  K_ALT,
  K_AUX1,
  K_AUX2,
  K_BACKSPACE,
  K_CTRL,
  K_DEL,
  K_DOWNARROW,
  K_END,
  K_ENTER,
  K_ESCAPE,
  K_F1,
  K_HOME,
  K_INS,
  K_KP_5,
  K_KP_DEL,
  K_KP_DOWNARROW,
  K_KP_END,
  K_KP_ENTER,
  K_KP_HOME,
  K_KP_INS,
  K_KP_LEFTARROW,
  K_KP_MINUS,
  K_KP_PGDN,
  K_KP_PGUP,
  K_KP_PLUS,
  K_KP_RIGHTARROW,
  K_KP_SLASH,
  K_KP_UPARROW,
  K_LEFTARROW,
  K_MOUSE1,
  K_MOUSE2,
  K_MOUSE3,
  K_MWHEELDOWN,
  K_MWHEELUP,
  K_PAUSE,
  K_PGDN,
  K_PGUP,
  K_RIGHTARROW,
  K_SHIFT,
  K_SPACE,
  K_TAB,
  K_UPARROW,
} from 'q2-shared';

const CODE_MAP: Record<string, number> = {
  Space: K_SPACE,
  Enter: K_ENTER,
  Escape: K_ESCAPE,
  Tab: K_TAB,
  Backspace: K_BACKSPACE,
  ArrowUp: K_UPARROW,
  ArrowDown: K_DOWNARROW,
  ArrowLeft: K_LEFTARROW,
  ArrowRight: K_RIGHTARROW,
  Insert: K_INS,
  Delete: K_DEL,
  Home: K_HOME,
  End: K_END,
  PageUp: K_PGUP,
  PageDown: K_PGDN,
  ShiftLeft: K_SHIFT,
  ShiftRight: K_SHIFT,
  ControlLeft: K_CTRL,
  ControlRight: K_CTRL,
  AltLeft: K_ALT,
  AltRight: K_ALT,
  Pause: K_PAUSE,
  Numpad7: K_KP_HOME,
  Numpad8: K_KP_UPARROW,
  Numpad9: K_KP_PGUP,
  Numpad4: K_KP_LEFTARROW,
  Numpad5: K_KP_5,
  Numpad6: K_KP_RIGHTARROW,
  Numpad1: K_KP_END,
  Numpad2: K_KP_DOWNARROW,
  Numpad3: K_KP_PGDN,
  Numpad0: K_KP_INS,
  NumpadDecimal: K_KP_DEL,
  NumpadEnter: K_KP_ENTER,
  NumpadDivide: K_KP_SLASH,
  NumpadSubtract: K_KP_MINUS,
  NumpadAdd: K_KP_PLUS,
  NumpadMultiply: 42, // '*' (the original maps it to its ASCII char)
  Backquote: 96, // '`' console key
  Minus: 45,
  Equal: 61,
  BracketLeft: 91,
  BracketRight: 93,
  Backslash: 92,
  Semicolon: 59,
  Quote: 39,
  Comma: 44,
  Period: 46,
  Slash: 47,
  IntlBackslash: 92,
};

/**
 * KeyboardEvent.code (+ optional .key used only as a fallback for unknown codes) → K_* number, or null
 * if the key has no Quake 2 equivalent.
 */
export function keyFromKeyboardEvent(code: string, key?: string): number | null {
  const m = CODE_MAP[code];
  if (m !== undefined) return m;
  if (code.length === 4 && code.startsWith('Key')) {
    const ch = code.charCodeAt(3);
    if (ch >= 65 && ch <= 90) return ch + 32; // 'a'..'z'
  }
  if (code.length === 6 && code.startsWith('Digit')) {
    const ch = code.charCodeAt(5);
    if (ch >= 48 && ch <= 57) return ch;
  }
  const f = /^F([1-9]|1[0-2])$/.exec(code);
  if (f) return K_F1 + Number(f[1]) - 1;
  // fallback: a single printable ASCII character produced by an unknown physical key
  if (key && key.length === 1) {
    let ch = key.charCodeAt(0);
    if (ch >= 65 && ch <= 90) ch += 32;
    if (ch > 32 && ch < 127) return ch;
  }
  return null;
}

/**
 * MouseEvent.button → K_*: 0 (left) → K_MOUSE1, 2 (right) → K_MOUSE2, 1 (middle) → K_MOUSE3.
 * 3.19 has no MOUSE4/5 keys; the browser back/forward buttons (3, 4) map to K_AUX1/K_AUX2 (bindable).
 */
export function keyFromMouseButton(button: number): number | null {
  switch (button) {
    case 0:
      return K_MOUSE1;
    case 2:
      return K_MOUSE2;
    case 1:
      return K_MOUSE3;
    case 3:
      return K_AUX1;
    case 4:
      return K_AUX2;
    default:
      return null;
  }
}

/** WheelEvent.deltaY → K_MWHEELDOWN (positive, scrolling down) / K_MWHEELUP (negative); 0 → null. */
export function keyFromWheel(deltaY: number): number | null {
  if (deltaY > 0) return K_MWHEELDOWN;
  if (deltaY < 0) return K_MWHEELUP;
  return null;
}
