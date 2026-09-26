// Navigation helpers shared by the shell pages.

/**
 * The page to open after logging in / registering (the ?next= parameter). Only same-origin paths are
 * accepted: "//host", "/\host" and paths with control characters (which URL parsing strips, turning
 * "/\t/host" into "//host") are protocol-relative URLs that would redirect off-site.
 */
export function safeNextPath(next: string | null | undefined, fallback = '/servers'): string {
  if (!next || next[0] !== '/') return fallback;
  if (next[1] === '/' || next[1] === '\\') return fallback;
  if (/[\u0000-\u001f\u007f]/.test(next)) return fallback;
  try {
    const base = 'http://origin.invalid';
    const u = new URL(next, base);
    if (u.origin !== base) return fallback;
    return u.pathname + u.search + u.hash;
  } catch {
    return fallback;
  }
}
