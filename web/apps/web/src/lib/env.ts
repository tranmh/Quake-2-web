// Browser-visible configuration.

/** Optional Go server origin (e.g. http://localhost:8080) for direct WebSocket connections in dev. */
export const Q2_SERVER: string = (process.env.NEXT_PUBLIC_Q2_SERVER ?? '').replace(/\/+$/, '');

/** Pakset used when a game does not name one. */
export const DEFAULT_PAKSET = 'demo';

/**
 * Turns the wsUrl of POST /games/{id}/join into an absolute ws(s):// URL carrying the ticket.
 * Relative URLs resolve against NEXT_PUBLIC_Q2_SERVER when set (the Next dev proxy may not forward
 * WebSocket upgrades), otherwise against the page origin (Caddy / the dev rewrites).
 */
export function resolveWsUrl(wsUrl: string, ticket: string, pageOrigin: string): string {
  const base = Q2_SERVER || pageOrigin;
  const u = new URL(wsUrl || '/', base);
  if (u.protocol === 'http:') u.protocol = 'ws:';
  else if (u.protocol === 'https:') u.protocol = 'wss:';
  if (ticket && !u.searchParams.has('ticket')) u.searchParams.set('ticket', ticket);
  return u.toString();
}
