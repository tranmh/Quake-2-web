import { describe, expect, it } from 'vitest';
import {
  K_ALT,
  K_AUX1,
  K_CTRL,
  K_ENTER,
  K_ESCAPE,
  K_F1,
  K_F12,
  K_KP_ENTER,
  K_KP_HOME,
  K_KP_SLASH,
  K_MOUSE1,
  K_MOUSE2,
  K_MOUSE3,
  K_MWHEELDOWN,
  K_MWHEELUP,
  K_PGDN,
  K_SHIFT,
  K_UPARROW,
} from 'q2-shared';
import { keyFromKeyboardEvent, keyFromMouseButton, keyFromWheel } from '../src/keymap';

describe('keymap', () => {
  it('maps KeyboardEvent.code', () => {
    expect(keyFromKeyboardEvent('KeyA')).toBe(97);
    expect(keyFromKeyboardEvent('KeyZ')).toBe(122);
    expect(keyFromKeyboardEvent('Digit0')).toBe(48);
    expect(keyFromKeyboardEvent('Digit9')).toBe(57);
    expect(keyFromKeyboardEvent('Enter')).toBe(K_ENTER);
    expect(keyFromKeyboardEvent('Escape')).toBe(K_ESCAPE);
    expect(keyFromKeyboardEvent('F1')).toBe(K_F1);
    expect(keyFromKeyboardEvent('F12')).toBe(K_F12);
    expect(keyFromKeyboardEvent('F13')).toBeNull();
    expect(keyFromKeyboardEvent('ArrowUp')).toBe(K_UPARROW);
    expect(keyFromKeyboardEvent('PageDown')).toBe(K_PGDN);
    expect(keyFromKeyboardEvent('ShiftRight')).toBe(K_SHIFT);
    expect(keyFromKeyboardEvent('ControlLeft')).toBe(K_CTRL);
    expect(keyFromKeyboardEvent('AltRight')).toBe(K_ALT);
    expect(keyFromKeyboardEvent('Numpad7')).toBe(K_KP_HOME);
    expect(keyFromKeyboardEvent('NumpadEnter')).toBe(K_KP_ENTER);
    expect(keyFromKeyboardEvent('NumpadDivide')).toBe(K_KP_SLASH);
    expect(keyFromKeyboardEvent('Backquote')).toBe(96);
    expect(keyFromKeyboardEvent('Semicolon')).toBe(59);
    expect(keyFromKeyboardEvent('Slash')).toBe(47);
    expect(keyFromKeyboardEvent('MetaLeft')).toBeNull();
    expect(keyFromKeyboardEvent('Unidentified', 'Q')).toBe(113);
  });
  it('maps mouse buttons and wheel', () => {
    expect(keyFromMouseButton(0)).toBe(K_MOUSE1);
    expect(keyFromMouseButton(2)).toBe(K_MOUSE2);
    expect(keyFromMouseButton(1)).toBe(K_MOUSE3);
    expect(keyFromMouseButton(3)).toBe(K_AUX1);
    expect(keyFromMouseButton(9)).toBeNull();
    expect(keyFromWheel(100)).toBe(K_MWHEELDOWN);
    expect(keyFromWheel(-3)).toBe(K_MWHEELUP);
    expect(keyFromWheel(0)).toBeNull();
  });
});
