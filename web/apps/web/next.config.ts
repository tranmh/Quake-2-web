// Next.js shell for the Quake 2 web port (docs/plans/0001-port-plan.md "UI").
//
// The browser always talks to the Go server through same-origin paths (/api, /assets, /ws). In production
// Caddy routes them (deploy/Caddyfile); in development these rewrites proxy them to the Go server.
//   Q2_SERVER_URL          upstream used by the rewrites (default http://localhost:8080)
//   NEXT_PUBLIC_Q2_SERVER  optional: browser-visible Go server origin; when set, game WebSockets connect
//                          there directly instead of through the dev proxy (see src/lib/env.ts)
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import type { NextConfig } from 'next';

const here = path.dirname(fileURLToPath(import.meta.url));
const upstream = (
  process.env.Q2_SERVER_URL ||
  process.env.NEXT_PUBLIC_Q2_SERVER ||
  process.env.NEXT_PUBLIC_API_URL ||
  'http://localhost:8080'
).replace(/\/+$/, '');

const nextConfig: NextConfig = {
  output: 'standalone',
  reactStrictMode: true,
  poweredByHeader: false,
  // do not generate AGENTS.md / CLAUDE.md in the app directory on `next dev`
  agentRules: false,
  // workspace root (web/): standalone tracing and Turbopack must see the linked packages
  outputFileTracingRoot: path.join(here, '../..'),
  turbopack: { root: path.join(here, '../..') },
  transpilePackages: [
    'q2-client',
    'q2-ref',
    'q2-render-gl',
    'q2-shared',
    'q2-sound',
    'q2-formats',
    'q2-protocol',
    'q2-pmove',
  ],
  // The shell must not be framed (clickjacking of Delete / upload / pointer lock); no inline-script CSP
  // because Next.js injects inline bootstrap scripts.
  async headers() {
    return [
      {
        source: '/:path*',
        headers: [
          { key: 'X-Frame-Options', value: 'DENY' },
          { key: 'Content-Security-Policy', value: "frame-ancestors 'none'" },
          { key: 'X-Content-Type-Options', value: 'nosniff' },
          { key: 'Referrer-Policy', value: 'strict-origin-when-cross-origin' },
        ],
      },
    ];
  },
  async rewrites() {
    return [
      { source: '/api/:path*', destination: `${upstream}/api/:path*` },
      { source: '/assets/:path*', destination: `${upstream}/assets/:path*` },
      { source: '/ws/:path*', destination: `${upstream}/ws/:path*` },
      { source: '/healthz', destination: `${upstream}/healthz` },
    ];
  },
};

export default nextConfig;
