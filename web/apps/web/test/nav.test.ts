// ?next= after login must stay on this origin (docs/review/05-ts-render-sound-app.md, open redirect).
import { describe, expect, it } from 'vitest';
import { safeNextPath } from '@/lib/nav';

const ORIGIN = 'https://q2.example';

/** Where the browser ends up for router.push(next) from a page of ORIGIN. */
function landsOn(next: string): string {
  return new URL(safeNextPath(next), `${ORIGIN}/login`).origin;
}

describe('safeNextPath', () => {
  it('keeps same-origin paths', () => {
    expect(safeNextPath('/play/abc?x=1#y')).toBe('/play/abc?x=1#y');
    expect(safeNextPath('/servers')).toBe('/servers');
  });

  it('falls back for missing or relative values', () => {
    expect(safeNextPath(null)).toBe('/servers');
    expect(safeNextPath('')).toBe('/servers');
    expect(safeNextPath('https://evil.example/')).toBe('/servers');
    expect(safeNextPath('javascript:alert(1)')).toBe('/servers');
  });

  it.each([
    '//evil.example/phish',
    '/\\evil.example/phish',
    '/\t/evil.example',
    '/%09/evil.example',
    '///evil.example',
  ])('never leaves the origin for %j', (next) => {
    expect(landsOn(next)).toBe(ORIGIN);
  });
});
